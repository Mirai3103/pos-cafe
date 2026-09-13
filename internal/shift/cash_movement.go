package shift

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
)

// cashMovementFingerprint is the idempotency fingerprint for a Cash Movement.
//
// ManagerPIN is deliberately absent. Including a secret would make the
// idempotency key sensitive to it and would store a PIN-derived value at rest.
// ApproverLoginCode is not a secret and is included, so re-using a request_id
// with a different approver is a conflict rather than a silent replay.
type cashMovementFingerprint struct {
	SalesShiftID      uuid.UUID `json:"sales_shift_id"`
	Method            string    `json:"method"`
	AmountVND         int64     `json:"amount_vnd"`
	Reason            string    `json:"reason"`
	Note              *string   `json:"note"`
	ApproverLoginCode string    `json:"approver_login_code"`
}

type cashMovementAuditDetails struct {
	CashMovementID           uuid.UUID `json:"cash_movement_id"`
	SalesShiftID             uuid.UUID `json:"sales_shift_id"`
	Method                   string    `json:"method"`
	AmountVND                int64     `json:"amount_vnd"`
	Reason                   string    `json:"reason"`
	Note                     *string   `json:"note"`
	InitiatorStaffIdentityID uuid.UUID `json:"initiator_staff_identity_id"`
	ApproverStaffIdentityID  uuid.UUID `json:"approver_staff_identity_id"`
}

// RecordCashMovementHandler records a Pay In or Pay Out.
type RecordCashMovementHandler struct{ runner *Runner }

// NewRecordCashMovementHandler creates a new RecordCashMovementHandler.
func NewRecordCashMovementHandler(runner *Runner) *RecordCashMovementHandler {
	return &RecordCashMovementHandler{runner: runner}
}

// Handle records a Cash Movement against an open Sales Shift and returns the
// resulting Expected Cash, so the terminal updates its drawer figure without a
// second request.
//
// Cash Movements are append-only: Phase 4 provides no edit, reverse, or delete.
func (h *RecordCashMovementHandler) Handle(ctx context.Context, actor Actor, cmd RecordCashMovementCommand) (int, CashMovementResult, error) {
	note := NormalizeNote(cmd.Note)
	amount := *cmd.AmountVND
	approverLoginCode := auth.NormalizeLoginCode(cmd.ApproverLoginCode)

	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpRecordCashMovement,
		Fingerprint: cashMovementFingerprint{
			SalesShiftID:      cmd.ShiftID,
			Method:            cmd.Method,
			AmountVND:         amount,
			Reason:            cmd.Reason,
			Note:              note,
			ApproverLoginCode: approverLoginCode,
		},
		Required: []string{CapSalesShiftOperate},
		Approval: &ApprovalSpec{
			ApproverLoginCode:  approverLoginCode,
			ManagerPIN:         cmd.ManagerPIN,
			RequiredCapability: CapSalesShiftOperate,
		},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, CashMovementResult, AuditRecord, error) {
			var zero CashMovementResult

			if err := ValidateMethod(cmd.Method); err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error())
			}
			if err := ValidateReason(cmd.Reason); err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error())
			}
			if err := ValidateAmount(amount); err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error())
			}
			if err := ValidateNote(note, cmd.Reason); err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error())
			}

			// Lock the Shift row so a concurrent state change cannot be missed.
			// A missing or non-OPEN Shift maps to OPEN_SALES_SHIFT_REQUIRED.
			openShift, err := mc.Queries.GetOpenSalesShiftForUpdate(ctx, cmd.ShiftID)
			if err != nil {
				return 0, zero, AuditRecord{}, MapDBError(err)
			}

			initiator, err := mc.Queries.GetStaffSummary(ctx, actor.StaffID)
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("load initiator summary: %w", err)
			}

			noteArg := sql.NullString{}
			if note != nil {
				noteArg = sql.NullString{String: *note, Valid: true}
			}

			inserted, err := mc.Queries.InsertCashMovement(ctx, sqlc.InsertCashMovementParams{
				SalesShiftID:                  openShift.ID,
				Method:                        cmd.Method,
				AmountVnd:                     amount,
				Reason:                        cmd.Reason,
				Note:                          noteArg,
				InitiatedByStaffIdentityID:    actor.StaffID,
				InitiatedStaffAccessSessionID: actor.SessionID,
				ApprovedByStaffIdentityID:     mc.Approver.ID,
			})
			if err != nil {
				return 0, zero, AuditRecord{}, MapDBError(err)
			}

			sums, err := mc.Queries.SumCashMovements(ctx, openShift.ID)
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("sum cash movements: %w", err)
			}
			expected, err := ComputeExpectedCash(openShift.OpeningFloatVnd, sums.PayInVnd, sums.PayOutVnd)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}

			movement := CashMovementResponse{
				ID:           inserted.ID,
				SalesShiftID: openShift.ID,
				Method:       cmd.Method,
				AmountVND:    amount,
				Reason:       cmd.Reason,
				Note:         note,
				Initiator: StaffSummary{
					ID:          initiator.ID,
					DisplayName: initiator.DisplayName,
					LoginCode:   initiator.LoginCode,
				},
				Approver:   staffSummaryFromApprover(*mc.Approver),
				OccurredAt: inserted.OccurredAt,
			}

			return 201, CashMovementResult{
				Movement:        movement,
				ExpectedCashVND: expected,
			}, AuditRecord{
				EventType: EventCashMovementRecorded,
				Details: cashMovementAuditDetails{
					CashMovementID:           movement.ID,
					SalesShiftID:             openShift.ID,
					Method:                   movement.Method,
					AmountVND:                movement.AmountVND,
					Reason:                   movement.Reason,
					Note:                     movement.Note,
					InitiatorStaffIdentityID: initiator.ID,
					ApproverStaffIdentityID:  mc.Approver.ID,
				},
			}, nil
		})
}
