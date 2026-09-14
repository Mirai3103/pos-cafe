package sales

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
)

// lockedCheck is a Check acquired under the uniform 5C lock protocol, with
// the parent state its caller needs to evaluate preconditions.
type lockedCheck struct {
	ID               uuid.UUID
	State            string
	ChargeVND        int64
	ServiceSessionID uuid.UUID
	SalesShiftID     uuid.UUID
}

// lockCheckForMutation takes the Check FOR UPDATE and its parents FOR SHARE,
// then reports the first failing precondition.
//
// The lock query and the precondition evaluation are deliberately separate.
// The canonical source folds every condition into one WHERE clause and reports
// a single code for an empty result; a cashier told which of the four is wrong
// can act, and told only "check not open" cannot.
func lockCheckForMutation(ctx context.Context, q *sqlc.Queries, checkID uuid.UUID) (
	lockedCheck, error,
) {
	var zero lockedCheck
	row, err := q.LockCheckForPayment(ctx, checkID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return zero, fmt.Errorf("%w: check %s", ErrCheckNotFound, checkID)
		}
		return zero, fmt.Errorf("lock check for mutation: %w", err)
	}
	if row.ServiceSessionState != SessionStateActive {
		return zero, fmt.Errorf("%w: session %s", ErrServiceSessionClosed, row.ServiceSessionID)
	}
	if row.SalesShiftState != ShiftStateOpen {
		return zero, fmt.Errorf("%w: check %s", ErrOpenShiftRequired, checkID)
	}
	if row.State != CheckStateOpen {
		return zero, fmt.Errorf("%w: check %s is %s", ErrCheckNotOpen, checkID, row.State)
	}
	return lockedCheck{
		ID:               row.ID,
		State:            row.State,
		ChargeVND:        row.ChargeVnd,
		ServiceSessionID: row.ServiceSessionID,
		SalesShiftID:     row.SalesShiftID,
	}, nil
}

// checkBalance verifies the stored charge against the live allocation sum and
// returns what is still owed.
//
// The charge comparison is 5B's invariant: stored charge_vnd is a
// denormalization, and a disagreement is a defect rather than a business
// state, so it fails the request with a logged 500.
func checkBalance(ctx context.Context, q *sqlc.Queries, checkID uuid.UUID,
	storedChargeVND int64,
) (int64, error) {
	_, allocatedVND, err := loadCheckAllocations(ctx, q, checkID)
	if err != nil {
		return 0, err
	}
	if allocatedVND != storedChargeVND {
		return 0, fmt.Errorf("%w: check %s stored %d, allocated %d",
			ErrChargeInvariantViolated, checkID, storedChargeVND, allocatedVND)
	}
	appliedVND, err := q.SumCheckPayments(ctx, checkID)
	if err != nil {
		return 0, fmt.Errorf("sum check payments: %w", err)
	}
	return SubtractCharge(storedChargeVND, appliedVND)
}

// paymentInput is the method-specific part of one Payment.
type paymentInput struct {
	Method               string
	AppliedAmountVND     int64
	CashTenderedVND      *int64
	ChangeDueVND         *int64
	TransactionReference *string
	AuditEvent           string
	AuditDetails         func(paymentID uuid.UUID, check lockedCheck) any
}

// recordPayment is the one execution path both Payment commands take.
//
// Settlement is a consequence, not a command: when this Payment brings the
// balance to zero the same transaction settles the Check and emits a second
// audit event. See ADR-017.
func recordPayment(ctx context.Context, q *sqlc.Queries, actor Actor,
	checkID uuid.UUID, in paymentInput,
) (ServiceSessionResponse, AuditRecord, error) {
	var zero ServiceSessionResponse

	check, err := lockCheckForMutation(ctx, q, checkID)
	if err != nil {
		return zero, AuditRecord{}, err
	}

	balanceVND, err := checkBalance(ctx, q, check.ID, check.ChargeVND)
	if err != nil {
		return zero, AuditRecord{}, err
	}
	if in.AppliedAmountVND > balanceVND {
		return zero, AuditRecord{}, fmt.Errorf("%w: applying %d to a balance of %d",
			ErrPaymentExceedsBalance, in.AppliedAmountVND, balanceVND)
	}

	receivedAt := time.Now()
	paymentID, err := q.InsertPayment(ctx, sqlc.InsertPaymentParams{
		CheckID:              check.ID,
		SalesShiftID:         check.SalesShiftID,
		ActorStaffIdentityID: actor.StaffID,
		StaffAccessSessionID: actor.SessionID,
		AppliedAmountVnd:     in.AppliedAmountVND,
		Method:               in.Method,
		CashTenderedVnd:      nullInt64(in.CashTenderedVND),
		ChangeDueVnd:         nullInt64(in.ChangeDueVND),
		TransactionReference: nullString(in.TransactionReference),
		ReceivedAt:           receivedAt,
	})
	if err != nil {
		return zero, AuditRecord{}, fmt.Errorf("insert payment: %w", err)
	}

	remainingVND, err := SubtractCharge(balanceVND, in.AppliedAmountVND)
	if err != nil {
		return zero, AuditRecord{}, err
	}
	if SettlesCheck(remainingVND) {
		if err := q.SettleCheck(ctx, sqlc.SettleCheckParams{
			ID:                          check.ID,
			SettledAt:                   sql.NullTime{Time: receivedAt, Valid: true},
			SettledByStaffIdentityID:    uuid.NullUUID{UUID: actor.StaffID, Valid: true},
			SettledDuringSalesShiftID:   uuid.NullUUID{UUID: check.SalesShiftID, Valid: true},
			SettledStaffAccessSessionID: uuid.NullUUID{UUID: actor.SessionID, Valid: true},
		}); err != nil {
			return zero, AuditRecord{}, fmt.Errorf("settle check: %w", err)
		}
		// Payment and settlement are two distinct facts. ExecuteMutation
		// writes the one it is handed, so the settlement event is written
		// here, inside the same transaction.
		settledDetails, err := marshalAuditDetails(checkSettledAudit{
			CheckID:      check.ID,
			PaymentID:    paymentID,
			SalesShiftID: check.SalesShiftID,
		})
		if err != nil {
			return zero, AuditRecord{}, err
		}
		if _, err := q.InsertAuditEvent(ctx, sqlc.InsertAuditEventParams{
			EventType:  EventCheckSettled,
			ActorID:    uuid.NullUUID{UUID: actor.StaffID, Valid: true},
			SessionID:  uuid.NullUUID{UUID: actor.SessionID, Valid: true},
			Details:    settledDetails,
			OccurredAt: receivedAt,
		}); err != nil {
			return zero, AuditRecord{}, fmt.Errorf("insert check settled audit event: %w", err)
		}
	}

	result, err := LoadServiceSession(ctx, q, check.ServiceSessionID)
	if err != nil {
		return zero, AuditRecord{}, err
	}
	return result, AuditRecord{
		EventType: in.AuditEvent,
		Details:   in.AuditDetails(paymentID, check),
	}, nil
}

type checkSettledAudit struct {
	CheckID      uuid.UUID `json:"check_id"`
	PaymentID    uuid.UUID `json:"payment_id"`
	SalesShiftID uuid.UUID `json:"sales_shift_id"`
}

type cashPaymentAudit struct {
	PaymentID        uuid.UUID `json:"payment_id"`
	CheckID          uuid.UUID `json:"check_id"`
	SalesShiftID     uuid.UUID `json:"sales_shift_id"`
	AppliedAmountVND int64     `json:"applied_amount_vnd"`
	CashTenderedVND  int64     `json:"cash_tendered_vnd"`
	ChangeDueVND     int64     `json:"change_due_vnd"`
}

type payCashFingerprint struct {
	CheckID          uuid.UUID `json:"check_id"`
	AppliedAmountVND int64     `json:"applied_amount_vnd"`
	CashTenderedVND  int64     `json:"cash_tendered_vnd"`
}

// PayCashFingerprintFor exposes the normalized business fingerprint so tests
// can pin its stability.
func PayCashFingerprintFor(cmd PayCashCommand) any {
	return payCashFingerprint{
		CheckID:          cmd.CheckID,
		AppliedAmountVND: cmd.AppliedAmountVND,
		CashTenderedVND:  cmd.CashTenderedVND,
	}
}

// ValidateCashAmounts rejects amounts that cannot describe a cash payment.
// These are field-shape rules, so they map to INVALID_INPUT rather than a
// domain code (ADR-018).
func ValidateCashAmounts(appliedVND, tenderedVND int64) error {
	if appliedVND <= 0 {
		return fmt.Errorf("%w: applied_amount_vnd must be positive", response.ErrInvalid)
	}
	if tenderedVND <= 0 {
		return fmt.Errorf("%w: cash_tendered_vnd must be positive", response.ErrInvalid)
	}
	return nil
}

// PayCashHandler records a Cash Payment against a Check.
type PayCashHandler struct{ runner *Runner }

// NewPayCashHandler creates a new PayCashHandler.
func NewPayCashHandler(runner *Runner) *PayCashHandler {
	return &PayCashHandler{runner: runner}
}

// Handle applies cash to a Check and settles it when nothing is left owed.
func (h *PayCashHandler) Handle(ctx context.Context, actor Actor, cmd PayCashCommand) (
	int, ServiceSessionResponse, error,
) {
	var zero ServiceSessionResponse
	if err := ValidateCashAmounts(cmd.AppliedAmountVND, cmd.CashTenderedVND); err != nil {
		return 0, zero, err
	}
	changeDueVND, err := ChangeDue(cmd.CashTenderedVND, cmd.AppliedAmountVND)
	if err != nil {
		return 0, zero, err
	}

	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpPayCash,
		Fingerprint: PayCashFingerprintFor(cmd),
		Required:    []string{CapSalesOperate},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, ServiceSessionResponse, AuditRecord, error) {
			tendered, change := cmd.CashTenderedVND, changeDueVND
			result, audit, err := recordPayment(ctx, mc.Queries, actor, cmd.CheckID, paymentInput{
				Method:           PaymentMethodCash,
				AppliedAmountVND: cmd.AppliedAmountVND,
				CashTenderedVND:  &tendered,
				ChangeDueVND:     &change,
				AuditEvent:       EventCashPaymentRecorded,
				AuditDetails: func(paymentID uuid.UUID, check lockedCheck) any {
					return cashPaymentAudit{
						PaymentID:        paymentID,
						CheckID:          check.ID,
						SalesShiftID:     check.SalesShiftID,
						AppliedAmountVND: cmd.AppliedAmountVND,
						CashTenderedVND:  cmd.CashTenderedVND,
						ChangeDueVND:     changeDueVND,
					}
				},
			})
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			return 200, result, audit, nil
		})
}

// nullInt64 converts an optional int64 to its sql.NullInt64 form.
func nullInt64(v *int64) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *v, Valid: true}
}

// marshalAuditDetails encodes audit details for an event written inside a
// mutation body rather than by ExecuteMutation.
func marshalAuditDetails(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal audit details: %w", err)
	}
	return b, nil
}
