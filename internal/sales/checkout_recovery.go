package sales

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/platform/database/sqlc"
	"github.com/google/uuid"
)

// checkoutRecoveryFingerprint is the idempotency input of both checkout
// recovery commands, Cancel Awaiting Submission and Abandon Checkout. The note
// is the normalized one, so differently padded replays match.
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
// Committed Items so the existing Refund can return the money.
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
			out, audit, err := applyCancelAwaitingSubmission(ctx, mc.Queries, actor, cmd, note)
			if err != nil {
				return 0, ServiceSessionResponse{}, AuditRecord{}, err
			}
			return http.StatusOK, out, audit, nil
		})
}

// awaitingSubmissionTarget is what Cancel Awaiting Submission holds locked
// once its preconditions are revalidated: the open Shift, the Session's
// submittable draft, and every Check of the Session.
type awaitingSubmissionTarget struct {
	shiftID uuid.UUID
	draftID uuid.UUID
	checks  []sqlc.LockSessionChecksForRecoveryRow
}

// chargeWithdrawal is the outcome of withdrawing a draft's Charge Allocations:
// the charge removed from each Check, the appended Charge Adjustments, and
// their total.
type chargeWithdrawal struct {
	removedByCheck map[uuid.UUID]int64
	adjustmentIDs  []uuid.UUID
	withdrawnVND   int64
}

// applyCancelAwaitingSubmission is the Cancel Awaiting Submission mutation
// body: it locks the target, withdraws every Charge Allocation of the draft,
// lowers and possibly settles each affected Check, and cancels the draft.
func applyCancelAwaitingSubmission(ctx context.Context, q *sqlc.Queries, actor Actor,
	cmd CheckoutRecoveryCommand, note *string,
) (ServiceSessionResponse, AuditRecord, error) {
	var zero ServiceSessionResponse
	target, err := lockAwaitingSubmission(ctx, q, cmd.ServiceSessionID)
	if err != nil {
		return zero, AuditRecord{}, err
	}

	occurredAt, err := q.GetSalesOccurredAt(ctx)
	if err != nil {
		return zero, AuditRecord{}, fmt.Errorf("read occurrence time: %w", err)
	}
	withdrawal, err := insertWithdrawalAdjustments(ctx, q, target, occurredAt)
	if err != nil {
		return zero, AuditRecord{}, err
	}
	for _, check := range target.checks {
		removed, ok := withdrawal.removedByCheck[check.ID]
		if !ok {
			continue
		}
		if err := lowerWithdrawnCheckCharge(ctx, q, actor, target.shiftID, occurredAt,
			check, removed); err != nil {
			return zero, AuditRecord{}, err
		}
	}

	if err := q.MarkOrderDraftCancelled(ctx, target.draftID); err != nil {
		return zero, AuditRecord{}, fmt.Errorf("mark draft cancelled: %w", err)
	}

	out, err := LoadServiceSession(ctx, q, cmd.ServiceSessionID)
	if err != nil {
		return zero, AuditRecord{}, err
	}
	return out, awaitingSubmissionCancelledAudit(cmd, note, target.draftID, withdrawal), nil
}

// lockAwaitingSubmission takes the Shift, Session, draft and Check locks in
// that order and confirms the Session is Awaiting Submission.
func lockAwaitingSubmission(ctx context.Context, q *sqlc.Queries, sessionID uuid.UUID) (
	awaitingSubmissionTarget, error,
) {
	shiftID, err := lockOpenShiftForRecovery(ctx, q)
	if err != nil {
		return awaitingSubmissionTarget{}, err
	}
	if err := lockRecoverableSession(ctx, q, sessionID); err != nil {
		return awaitingSubmissionTarget{}, err
	}
	draftID, err := q.LockSubmittableDraft(ctx, sessionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return awaitingSubmissionTarget{}, fmt.Errorf(
				"%w: %s", ErrNothingAwaitingSubmission, sessionID)
		}
		return awaitingSubmissionTarget{}, fmt.Errorf("lock submittable draft: %w", err)
	}
	checks, err := q.LockSessionChecksForRecovery(ctx, sessionID)
	if err != nil {
		return awaitingSubmissionTarget{}, fmt.Errorf("lock session checks: %w", err)
	}

	// Awaiting Submission is judged on the locked facts, through the same
	// derivation the projection uses, so the two cannot disagree.
	before, err := LoadServiceSession(ctx, q, sessionID)
	if err != nil {
		return awaitingSubmissionTarget{}, err
	}
	if !before.AwaitingSubmission {
		return awaitingSubmissionTarget{}, fmt.Errorf(
			"%w: %s", ErrNothingAwaitingSubmission, sessionID)
	}
	return awaitingSubmissionTarget{shiftID: shiftID, draftID: draftID, checks: checks}, nil
}

// insertWithdrawalAdjustments appends one withdrawal Charge Adjustment per
// withdrawable Charge Allocation of the draft and totals what it removes, per
// Check and overall.
func insertWithdrawalAdjustments(ctx context.Context, q *sqlc.Queries,
	target awaitingSubmissionTarget, occurredAt time.Time,
) (chargeWithdrawal, error) {
	allocations, err := q.ListWithdrawableAllocations(ctx, target.draftID)
	if err != nil {
		return chargeWithdrawal{}, fmt.Errorf("list withdrawable allocations: %w", err)
	}

	out := chargeWithdrawal{
		removedByCheck: make(map[uuid.UUID]int64),
		adjustmentIDs:  make([]uuid.UUID, 0, len(allocations)),
	}
	for _, allocation := range allocations {
		adjustment, err := q.InsertChargeAdjustment(ctx, sqlc.InsertChargeAdjustmentParams{
			Kind:               ChargeAdjustmentKindWithdrawal,
			Scope:              CompScopeLiveCheck,
			ChargeAllocationID: allocation.ID,
			CheckID:            allocation.CheckID,
			SalesShiftID:       target.shiftID,
			AmountVnd:          allocation.AmountVnd,
			CreatedAt:          occurredAt,
		})
		if err != nil {
			return chargeWithdrawal{}, fmt.Errorf("insert withdrawal adjustment: %w", err)
		}
		out.adjustmentIDs = append(out.adjustmentIDs, adjustment.ID)
		checkTotal, err := AddCharge(out.removedByCheck[allocation.CheckID], allocation.AmountVnd)
		if err != nil {
			return chargeWithdrawal{}, err
		}
		out.removedByCheck[allocation.CheckID] = checkTotal
		if out.withdrawnVND, err = AddCharge(out.withdrawnVND, allocation.AmountVnd); err != nil {
			return chargeWithdrawal{}, err
		}
	}
	return out, nil
}

// lowerWithdrawnCheckCharge lowers one Check's stored charge by what the
// withdrawal removed from it, and settles an OPEN Check whose remaining
// balance the existing payments now cover.
func lowerWithdrawnCheckCharge(ctx context.Context, q *sqlc.Queries, actor Actor,
	shiftID uuid.UUID, occurredAt time.Time,
	check sqlc.LockSessionChecksForRecoveryRow, removed int64,
) error {
	if removed > check.ChargeVnd {
		return fmt.Errorf(
			"%w: withdrawal %d exceeds stored charge %d of check %s",
			ErrChargeInvariantViolated, removed, check.ChargeVnd, check.ID)
	}
	newChargeVND := check.ChargeVnd - removed
	if err := q.UpdateAdjustedCheckCharge(ctx, sqlc.UpdateAdjustedCheckChargeParams{
		ChargeVnd: newChargeVND, ID: check.ID,
	}); err != nil {
		return fmt.Errorf("update adjusted check charge: %w", err)
	}
	if check.State != CheckStateOpen {
		return nil
	}
	balanceVND, err := checkBalance(ctx, q, check.ID, newChargeVND)
	if err != nil {
		return err
	}
	if !SettlesCheck(balanceVND) {
		return nil
	}
	return settleWithdrawnCheck(ctx, q, actor, shiftID, occurredAt, check, newChargeVND)
}

// settleWithdrawnCheck settles a Check the withdrawal left fully covered and
// audits the settlement with its charge before and after.
func settleWithdrawnCheck(ctx context.Context, q *sqlc.Queries, actor Actor,
	shiftID uuid.UUID, occurredAt time.Time,
	check sqlc.LockSessionChecksForRecoveryRow, newChargeVND int64,
) error {
	if err := q.SettleCheck(ctx, sqlc.SettleCheckParams{
		ID:                          check.ID,
		SettledAt:                   sql.NullTime{Time: occurredAt, Valid: true},
		SettledByStaffIdentityID:    uuid.NullUUID{UUID: actor.StaffID, Valid: true},
		SettledDuringSalesShiftID:   uuid.NullUUID{UUID: shiftID, Valid: true},
		SettledStaffAccessSessionID: uuid.NullUUID{UUID: actor.SessionID, Valid: true},
	}); err != nil {
		return fmt.Errorf("settle withdrawn check: %w", err)
	}
	return writeSalesAudit(ctx, q, actor, occurredAt, EventCheckSettled,
		map[string]any{
			"check_id":          check.ID,
			"sales_shift_id":    shiftID,
			"charge_before_vnd": check.ChargeVnd,
			"charge_after_vnd":  newChargeVND,
		})
}

// awaitingSubmissionCancelledAudit builds the command's audit record.
func awaitingSubmissionCancelledAudit(cmd CheckoutRecoveryCommand, note *string,
	draftID uuid.UUID, withdrawal chargeWithdrawal,
) AuditRecord {
	return AuditRecord{
		EventType: EventAwaitingSubmissionCancelled,
		Details: map[string]any{
			"service_session_id":    cmd.ServiceSessionID,
			"order_draft_id":        draftID,
			"charge_adjustment_ids": withdrawal.adjustmentIDs,
			"withdrawn_vnd":         withdrawal.withdrawnVND,
			"reason":                cmd.Reason,
			"note":                  note,
		},
	}
}

// AbandonCheckoutHandler ends a Session that has no Order and holds no money
// as an Abandoned Checkout. It creates no Order, Preparation Unit, or
// Completed Sale.
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
			out, audit, err := applyAbandonCheckout(ctx, mc.Queries, actor, cmd, note)
			if err != nil {
				return 0, ServiceSessionResponse{}, AuditRecord{}, err
			}
			return http.StatusOK, out, audit, nil
		})
}

// abandonedCheckout is what recording an Abandoned Checkout produced: its
// row, the Checks it abandoned, and the Tables it released.
type abandonedCheckout struct {
	abandonedID      uuid.UUID
	checkIDs         []uuid.UUID
	releasedTableIDs []uuid.UUID
}

// applyAbandonCheckout is the Abandon Checkout mutation body: it locks the
// Session and proves it holds no money, then records the Abandoned Checkout
// and ends the Session.
func applyAbandonCheckout(ctx context.Context, q *sqlc.Queries, actor Actor,
	cmd CheckoutRecoveryCommand, note *string,
) (ServiceSessionResponse, AuditRecord, error) {
	var zero ServiceSessionResponse
	shiftID, err := lockAbandonableSession(ctx, q, cmd.ServiceSessionID)
	if err != nil {
		return zero, AuditRecord{}, err
	}

	occurredAt, err := q.GetSalesOccurredAt(ctx)
	if err != nil {
		return zero, AuditRecord{}, fmt.Errorf("read occurrence time: %w", err)
	}
	abandoned, err := recordAbandonedCheckout(ctx, q, actor, cmd, note, shiftID, occurredAt)
	if err != nil {
		return zero, AuditRecord{}, err
	}

	out, err := LoadServiceSession(ctx, q, cmd.ServiceSessionID)
	if err != nil {
		return zero, AuditRecord{}, err
	}
	return out, checkoutAbandonedAudit(cmd, note, abandoned), nil
}

// lockAbandonableSession takes the Shift, Session and Check locks in that
// order and refuses a Session that still holds money, returning the open
// Shift.
func lockAbandonableSession(ctx context.Context, q *sqlc.Queries, sessionID uuid.UUID) (
	uuid.UUID, error,
) {
	shiftID, err := lockOpenShiftForRecovery(ctx, q)
	if err != nil {
		return uuid.Nil, err
	}
	if err := lockRecoverableSession(ctx, q, sessionID); err != nil {
		return uuid.Nil, err
	}
	// The Check locks make the money read below final: a Payment or Refund
	// that commits first is seen, and one that comes later waits and then
	// finds the Session ABANDONED.
	if _, err := q.LockSessionChecksForRecovery(ctx, sessionID); err != nil {
		return uuid.Nil, fmt.Errorf("lock session checks: %w", err)
	}
	money, err := q.GetSessionHeldMoney(ctx, sessionID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("read session money: %w", err)
	}
	if money.PendingRefundCount > 0 || money.ValidPaymentVnd != money.CompletedRefundVnd {
		return uuid.Nil, fmt.Errorf(
			"%w: received %d, returned %d, %d refund(s) pending",
			ErrPaymentRequiresRefund, money.ValidPaymentVnd,
			money.CompletedRefundVnd, money.PendingRefundCount)
	}
	return shiftID, nil
}

// recordAbandonedCheckout appends the Abandoned Checkout, cancels the
// Session's drafts, abandons its Checks, releases its held Tables, and marks
// the Session ABANDONED.
func recordAbandonedCheckout(ctx context.Context, q *sqlc.Queries, actor Actor,
	cmd CheckoutRecoveryCommand, note *string, shiftID uuid.UUID, occurredAt time.Time,
) (abandonedCheckout, error) {
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
		return abandonedCheckout{}, fmt.Errorf("insert abandoned checkout: %w", err)
	}
	if err := q.CancelSessionDrafts(ctx, cmd.ServiceSessionID); err != nil {
		return abandonedCheckout{}, fmt.Errorf("cancel session drafts: %w", err)
	}
	checkIDs, err := q.AbandonSessionChecks(ctx, cmd.ServiceSessionID)
	if err != nil {
		return abandonedCheckout{}, fmt.Errorf("abandon session checks: %w", err)
	}
	releasedTableIDs, err := releaseHeldTableAssignments(ctx, q, actor,
		cmd.ServiceSessionID, occurredAt)
	if err != nil {
		return abandonedCheckout{}, err
	}
	if err := q.AbandonServiceSession(ctx, cmd.ServiceSessionID); err != nil {
		return abandonedCheckout{}, fmt.Errorf("abandon service session: %w", err)
	}
	return abandonedCheckout{
		abandonedID:      abandonedID,
		checkIDs:         checkIDs,
		releasedTableIDs: releasedTableIDs,
	}, nil
}

// checkoutAbandonedAudit builds the command's audit record.
func checkoutAbandonedAudit(cmd CheckoutRecoveryCommand, note *string,
	abandoned abandonedCheckout,
) AuditRecord {
	return AuditRecord{
		EventType: EventCheckoutAbandoned,
		Details: map[string]any{
			"service_session_id":    cmd.ServiceSessionID,
			"abandoned_checkout_id": abandoned.abandonedID,
			"check_ids":             abandoned.checkIDs,
			"released_table_ids":    abandoned.releasedTableIDs,
			"reason":                cmd.Reason,
			"note":                  note,
		},
	}
}
