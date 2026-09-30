package sales

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

// checkoutRecoveryFingerprint is the idempotency input of both Phase 08
// commands. The note is the normalized one, so differently padded replays
// match.
type checkoutRecoveryFingerprint struct {
	ServiceSessionID uuid.UUID `json:"service_session_id"`
	Reason           string    `json:"reason"`
	Note             *string   `json:"note"`
}

// lockOpenShiftForRecovery takes the open Sales Shift FOR SHARE first, and
// requires one, because both commands write rows the Shift reconciles. Payment,
// Refund, Void and Comp lock Check, then Session, then Shift; shared locks
// cannot deadlock against those.
func lockOpenShiftForRecovery(ctx context.Context, q *sqlc.Queries) (uuid.UUID, error) {
	shiftID, err := lockOpenSalesShift(ctx, q)
	if err != nil {
		return uuid.Nil, err
	}
	if shiftID == uuid.Nil {
		return uuid.Nil, fmt.Errorf("%w: no open sales shift", ErrOpenShiftRequired)
	}
	return shiftID, nil
}

// lockRecoverableSession locks the Session and applies the preconditions both
// commands share: it exists, is ACTIVE, and has no Order (ADR-066).
func lockRecoverableSession(ctx context.Context, q *sqlc.Queries, sessionID uuid.UUID) error {
	session, err := q.LockServiceSessionForSubmission(ctx, sessionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: %s", ErrServiceSessionNotFound, sessionID)
		}
		return fmt.Errorf("lock service session: %w", err)
	}
	if session.State != StateActive {
		return fmt.Errorf("%w: %s", ErrServiceSessionClosed, sessionID)
	}
	hasOrder, err := q.SessionHasOrder(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("check session order: %w", err)
	}
	if hasOrder {
		return fmt.Errorf("%w: %s", ErrSessionHasOrder, sessionID)
	}
	return nil
}

// CancelAwaitingSubmissionHandler withdraws the charges of paid, unsubmitted
// Committed Items so the existing Refund can return the money (spec §5.1).
type CancelAwaitingSubmissionHandler struct{ runner *Runner }

// NewCancelAwaitingSubmissionHandler creates a CancelAwaitingSubmissionHandler.
func NewCancelAwaitingSubmissionHandler(runner *Runner) *CancelAwaitingSubmissionHandler {
	return &CancelAwaitingSubmissionHandler{runner: runner}
}

// Handle executes Cancel Awaiting Submission.
func (h *CancelAwaitingSubmissionHandler) Handle(ctx context.Context, actor Actor,
	cmd CheckoutRecoveryCommand,
) (int, ServiceSessionResponse, error) {
	note := normalizeCorrectionNote(cmd.Note)
	if err := ValidateCheckoutRecoveryCommand(cmd, note); err != nil {
		return 0, ServiceSessionResponse{}, err
	}
	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpCancelAwaitingSubmission,
		Fingerprint: checkoutRecoveryFingerprint{cmd.ServiceSessionID, cmd.Reason, note},
		Required:    []string{CapSalesOperate},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, ServiceSessionResponse, AuditRecord, error) {
			var zero ServiceSessionResponse
			q := mc.Queries

			shiftID, err := lockOpenShiftForRecovery(ctx, q)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			if err := lockRecoverableSession(ctx, q, cmd.ServiceSessionID); err != nil {
				return 0, zero, AuditRecord{}, err
			}
			draftID, err := q.LockSubmittableDraft(ctx, cmd.ServiceSessionID)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return 0, zero, AuditRecord{}, fmt.Errorf(
						"%w: %s", ErrNothingAwaitingSubmission, cmd.ServiceSessionID)
				}
				return 0, zero, AuditRecord{}, fmt.Errorf("lock submittable draft: %w", err)
			}
			checks, err := q.LockSessionChecksForRecovery(ctx, cmd.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("lock session checks: %w", err)
			}

			// Awaiting Submission is judged on the locked facts, through the
			// same derivation the projection uses, so the two cannot disagree.
			before, err := LoadServiceSession(ctx, q, cmd.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			if !before.AwaitingSubmission {
				return 0, zero, AuditRecord{}, fmt.Errorf(
					"%w: %s", ErrNothingAwaitingSubmission, cmd.ServiceSessionID)
			}

			occurredAt, err := q.GetSalesOccurredAt(ctx)
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("read occurrence time: %w", err)
			}
			allocations, err := q.ListWithdrawableAllocations(ctx, draftID)
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("list withdrawable allocations: %w", err)
			}

			removedByCheck := make(map[uuid.UUID]int64)
			adjustmentIDs := make([]uuid.UUID, 0, len(allocations))
			var withdrawnVND int64
			for _, allocation := range allocations {
				adjustment, err := q.InsertChargeAdjustment(ctx, sqlc.InsertChargeAdjustmentParams{
					Kind:               ChargeAdjustmentKindWithdrawal,
					Scope:              CompScopeLiveCheck,
					ChargeAllocationID: allocation.ID,
					CheckID:            allocation.CheckID,
					SalesShiftID:       shiftID,
					AmountVnd:          allocation.AmountVnd,
					CreatedAt:          occurredAt,
				})
				if err != nil {
					return 0, zero, AuditRecord{}, fmt.Errorf("insert withdrawal adjustment: %w", err)
				}
				adjustmentIDs = append(adjustmentIDs, adjustment.ID)
				checkTotal, err := AddCharge(removedByCheck[allocation.CheckID], allocation.AmountVnd)
				if err != nil {
					return 0, zero, AuditRecord{}, err
				}
				removedByCheck[allocation.CheckID] = checkTotal
				if withdrawnVND, err = AddCharge(withdrawnVND, allocation.AmountVnd); err != nil {
					return 0, zero, AuditRecord{}, err
				}
			}

			for _, check := range checks {
				removed, ok := removedByCheck[check.ID]
				if !ok {
					continue
				}
				if removed > check.ChargeVnd {
					return 0, zero, AuditRecord{}, fmt.Errorf(
						"%w: withdrawal %d exceeds stored charge %d of check %s",
						ErrChargeInvariantViolated, removed, check.ChargeVnd, check.ID)
				}
				newChargeVND := check.ChargeVnd - removed
				if err := q.UpdateAdjustedCheckCharge(ctx, sqlc.UpdateAdjustedCheckChargeParams{
					ChargeVnd: newChargeVND, ID: check.ID,
				}); err != nil {
					return 0, zero, AuditRecord{}, fmt.Errorf("update adjusted check charge: %w", err)
				}
				if check.State != CheckStateOpen {
					continue
				}
				balanceVND, err := checkBalance(ctx, q, check.ID, newChargeVND)
				if err != nil {
					return 0, zero, AuditRecord{}, err
				}
				if !SettlesCheck(balanceVND) {
					continue
				}
				if err := q.SettleCheck(ctx, sqlc.SettleCheckParams{
					ID:                          check.ID,
					SettledAt:                   sql.NullTime{Time: occurredAt, Valid: true},
					SettledByStaffIdentityID:    uuid.NullUUID{UUID: actor.StaffID, Valid: true},
					SettledDuringSalesShiftID:   uuid.NullUUID{UUID: shiftID, Valid: true},
					SettledStaffAccessSessionID: uuid.NullUUID{UUID: actor.SessionID, Valid: true},
				}); err != nil {
					return 0, zero, AuditRecord{}, fmt.Errorf("settle withdrawn check: %w", err)
				}
				if err := writeSalesAudit(ctx, q, actor, occurredAt, EventCheckSettled,
					map[string]any{
						"check_id":          check.ID,
						"sales_shift_id":    shiftID,
						"charge_before_vnd": check.ChargeVnd,
						"charge_after_vnd":  newChargeVND,
					}); err != nil {
					return 0, zero, AuditRecord{}, err
				}
			}

			if err := q.MarkOrderDraftCancelled(ctx, draftID); err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("mark draft cancelled: %w", err)
			}

			out, err := LoadServiceSession(ctx, q, cmd.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			return http.StatusOK, out, AuditRecord{
				EventType: EventAwaitingSubmissionCancelled,
				Details: map[string]any{
					"service_session_id":    cmd.ServiceSessionID,
					"order_draft_id":        draftID,
					"charge_adjustment_ids": adjustmentIDs,
					"withdrawn_vnd":         withdrawnVND,
					"reason":                cmd.Reason,
					"note":                  note,
				},
			}, nil
		})
}

// AbandonCheckoutHandler ends a Session that has no Order and holds no money
// as an Abandoned Checkout (spec §5.2). It creates no Order, Preparation Unit,
// or Completed Sale.
type AbandonCheckoutHandler struct{ runner *Runner }

// NewAbandonCheckoutHandler creates an AbandonCheckoutHandler.
func NewAbandonCheckoutHandler(runner *Runner) *AbandonCheckoutHandler {
	return &AbandonCheckoutHandler{runner: runner}
}

// Handle executes Abandon Checkout.
func (h *AbandonCheckoutHandler) Handle(ctx context.Context, actor Actor,
	cmd CheckoutRecoveryCommand,
) (int, ServiceSessionResponse, error) {
	note := normalizeCorrectionNote(cmd.Note)
	if err := ValidateCheckoutRecoveryCommand(cmd, note); err != nil {
		return 0, ServiceSessionResponse{}, err
	}
	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpAbandonCheckout,
		Fingerprint: checkoutRecoveryFingerprint{cmd.ServiceSessionID, cmd.Reason, note},
		Required:    []string{CapSalesOperate},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, ServiceSessionResponse, AuditRecord, error) {
			var zero ServiceSessionResponse
			q := mc.Queries

			shiftID, err := lockOpenShiftForRecovery(ctx, q)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			if err := lockRecoverableSession(ctx, q, cmd.ServiceSessionID); err != nil {
				return 0, zero, AuditRecord{}, err
			}
			// The Check locks make the money read below final: a Payment or
			// Refund that commits first is seen, and one that comes later
			// waits and then finds the Session ABANDONED.
			if _, err := q.LockSessionChecksForRecovery(ctx, cmd.ServiceSessionID); err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("lock session checks: %w", err)
			}
			money, err := q.GetSessionHeldMoney(ctx, cmd.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("read session money: %w", err)
			}
			if money.PendingRefundCount > 0 || money.ValidPaymentVnd != money.CompletedRefundVnd {
				return 0, zero, AuditRecord{}, fmt.Errorf(
					"%w: received %d, returned %d, %d refund(s) pending",
					ErrPaymentRequiresRefund, money.ValidPaymentVnd,
					money.CompletedRefundVnd, money.PendingRefundCount)
			}

			occurredAt, err := q.GetSalesOccurredAt(ctx)
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("read occurrence time: %w", err)
			}
			abandonedID, err := q.InsertAbandonedCheckout(ctx, sqlc.InsertAbandonedCheckoutParams{
				ServiceSessionID:     cmd.ServiceSessionID,
				SalesShiftID:         shiftID,
				Reason:               cmd.Reason,
				Note:                 nullString(note),
				ActorStaffIdentityID: actor.StaffID,
				StaffAccessSessionID: actor.SessionID,
				OccurredAt:           occurredAt,
			})
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("insert abandoned checkout: %w", err)
			}
			if err := q.CancelSessionDrafts(ctx, cmd.ServiceSessionID); err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("cancel session drafts: %w", err)
			}
			checkIDs, err := q.AbandonSessionChecks(ctx, cmd.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("abandon session checks: %w", err)
			}
			releasedTableIDs, err := releaseHeldTableAssignments(ctx, q, actor,
				cmd.ServiceSessionID, occurredAt)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			if err := q.AbandonServiceSession(ctx, cmd.ServiceSessionID); err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("abandon service session: %w", err)
			}

			out, err := LoadServiceSession(ctx, q, cmd.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			return http.StatusOK, out, AuditRecord{
				EventType: EventCheckoutAbandoned,
				Details: map[string]any{
					"service_session_id":    cmd.ServiceSessionID,
					"abandoned_checkout_id": abandonedID,
					"check_ids":             checkIDs,
					"released_table_ids":    releasedTableIDs,
					"reason":                cmd.Reason,
					"note":                  note,
				},
			}, nil
		})
}
