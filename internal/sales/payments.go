package sales

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
)

// lockedCheck is a Check acquired under the uniform 5C lock protocol, with
// the parent state its caller needs to evaluate preconditions.
type lockedCheck struct {
	ID                  uuid.UUID
	State               string
	ChargeVND           int64
	ServiceSessionID    uuid.UUID
	ServiceSessionState string
	// SalesShiftID is the Shift open at the moment of the lock, which is not
	// necessarily the one the Check's Session was opened in. See ADR-019.
	SalesShiftID uuid.UUID
}

// checkPreconditions reports the first failing precondition shared by every
// command that operates on a Check, in the precedence §6.2 documents: the
// Check's own state, then its Session, then the Shift.
//
// Existence is the caller's concern, because a Check id that locks nothing
// means a different failure to each command. Evaluating the rest in one place
// is what keeps two commands from reporting different codes for one state.
func checkPreconditions(c lockedCheck) error {
	if c.State != CheckStateOpen {
		return fmt.Errorf("%w: check %s is %s", ErrCheckNotOpen, c.ID, c.State)
	}
	if c.ServiceSessionState != StateActive {
		return fmt.Errorf("%w: session %s", ErrServiceSessionClosed, c.ServiceSessionID)
	}
	if c.SalesShiftID == uuid.Nil {
		return fmt.Errorf("%w: check %s", ErrOpenShiftRequired, c.ID)
	}
	return nil
}

// lockOpenSalesShift takes the open Sales Shift FOR SHARE and returns its id,
// or uuid.Nil when no Shift is open.
//
// The Shift is read directly rather than through the Check's Session: the Shift
// in which money reached the cashier is an independent fact, and a Session
// opened in one Shift can be paid in the next. Deriving it from the Session
// would answer the reconciliation question wrongly. See ADR-019.
func lockOpenSalesShift(ctx context.Context, q *sqlc.Queries) (uuid.UUID, error) {
	id, err := q.LockOpenSalesShiftForShare(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return uuid.Nil, nil
		}
		return uuid.Nil, fmt.Errorf("lock open sales shift: %w", err)
	}
	return id, nil
}

// lockCheckForMutation takes the Check FOR UPDATE and then its Session
// FOR UPDATE, then reports the first failing precondition.
//
// The Session lock is exclusive rather than FOR SHARE because the command does
// not stop at reading the Session to evaluate a precondition: inside the same
// READ COMMITTED transaction it rebuilds the whole Service Session read model
// through LoadServiceSession, which reads a Check's header and its payments in
// several statements. A sibling Check's commit landing between them is observed
// half-applied and trips the settlement invariant, rolling back a valid
// Payment. See ADR-023.
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
	shiftID, err := lockOpenSalesShift(ctx, q)
	if err != nil {
		return zero, err
	}
	check := lockedCheck{
		ID:                  row.ID,
		State:               row.State,
		ChargeVND:           row.ChargeVnd,
		ServiceSessionID:    row.ServiceSessionID,
		ServiceSessionState: row.ServiceSessionState,
		SalesShiftID:        shiftID,
	}
	if err := checkPreconditions(check); err != nil {
		return zero, err
	}
	return check, nil
}

// assertChargeMatchesAllocations verifies a Check's stored charge against the
// live sum of its allocations.
//
// The comparison is 5B's invariant: stored charge_vnd is a denormalization, and
// a disagreement is a defect rather than a business state, so it fails the
// request with a logged 500. Every command that rewrites a charge runs this
// first — a value that has drifted must not be built on, or the drift is
// propagated into a second Check before the read path ever sees it.
func assertChargeMatchesAllocations(ctx context.Context, q *sqlc.Queries, checkID uuid.UUID,
	storedChargeVND int64,
) error {
	allocatedVND, err := q.SumCheckAllocatedCharge(ctx, checkID)
	if err != nil {
		return fmt.Errorf("sum check allocated charge: %w", err)
	}
	if allocatedVND != storedChargeVND {
		slog.Error("check charge does not match its allocations",
			"check_id", checkID,
			"stored_charge_vnd", storedChargeVND,
			"allocated_vnd", allocatedVND)
		return fmt.Errorf("%w: check %s stored %d, allocated %d",
			ErrChargeInvariantViolated, checkID, storedChargeVND, allocatedVND)
	}
	return nil
}

// assertChargesMatchAllocations runs the charge invariant over a set of locked
// Checks, in the order the caller listed them so the reported failure is
// reproducible.
func assertChargesMatchAllocations(ctx context.Context, q *sqlc.Queries, ids []uuid.UUID,
	locked map[uuid.UUID]lockedCheck,
) error {
	for _, id := range ids {
		if err := assertChargeMatchesAllocations(ctx, q, id, locked[id].ChargeVND); err != nil {
			return err
		}
	}
	return nil
}

// checkBalance verifies the stored charge against the live allocation sum and
// returns what is still owed.
func checkBalance(ctx context.Context, q *sqlc.Queries, checkID uuid.UUID,
	storedChargeVND int64,
) (int64, error) {
	if err := assertChargeMatchesAllocations(ctx, q, checkID, storedChargeVND); err != nil {
		return 0, err
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

type manualQRPaymentAudit struct {
	PaymentID                uuid.UUID `json:"payment_id"`
	CheckID                  uuid.UUID `json:"check_id"`
	SalesShiftID             uuid.UUID `json:"sales_shift_id"`
	AppliedAmountVND         int64     `json:"applied_amount_vnd"`
	ReceiptObservedInBankApp bool      `json:"receipt_observed_in_bank_app"`
	TransactionReference     *string   `json:"transaction_reference"`
}

type payManualQRFingerprint struct {
	CheckID              uuid.UUID `json:"check_id"`
	AppliedAmountVND     int64     `json:"applied_amount_vnd"`
	TransactionReference *string   `json:"transaction_reference"`
}

// PayManualQRFingerprintFor exposes the normalized business fingerprint.
//
// The reference is trimmed first, so a request that differs only in
// surrounding whitespace is the same request. The receipt attestation is not
// part of the fingerprint: it is required to be true, so it cannot vary
// between two requests that both succeed.
func PayManualQRFingerprintFor(cmd PayManualQRCommand) any {
	normalized, err := ValidateTransactionReference(cmd.TransactionReference)
	if err != nil {
		normalized = cmd.TransactionReference
	}
	return payManualQRFingerprint{
		CheckID:              cmd.CheckID,
		AppliedAmountVND:     cmd.AppliedAmountVND,
		TransactionReference: normalized,
	}
}

// PayManualQRHandler records a Manual QR Payment against a Check.
type PayManualQRHandler struct{ runner *Runner }

// NewPayManualQRHandler creates a new PayManualQRHandler.
func NewPayManualQRHandler(runner *Runner) *PayManualQRHandler {
	return &PayManualQRHandler{runner: runner}
}

// Handle applies a confirmed bank transfer to a Check.
func (h *PayManualQRHandler) Handle(ctx context.Context, actor Actor,
	cmd PayManualQRCommand,
) (int, ServiceSessionResponse, error) {
	var zero ServiceSessionResponse
	if cmd.AppliedAmountVND <= 0 {
		return 0, zero, fmt.Errorf("%w: applied_amount_vnd must be positive", response.ErrInvalid)
	}
	reference, err := ValidateTransactionReference(cmd.TransactionReference)
	if err != nil {
		return 0, zero, err
	}
	// A policy, not a field shape: no Payment exists before a staff member
	// has seen the money arrive, so this gets its own code rather than
	// landing on the generic validation one.
	if !cmd.ReceiptObservedInBankApp {
		return 0, zero, fmt.Errorf("%w: check %s", ErrManualQRReceiptRequired, cmd.CheckID)
	}

	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpPayManualQR,
		Fingerprint: PayManualQRFingerprintFor(cmd),
		Required:    []string{CapSalesOperate},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, ServiceSessionResponse, AuditRecord, error) {
			result, audit, err := recordPayment(ctx, mc.Queries, actor, cmd.CheckID, paymentInput{
				Method:               PaymentMethodManualQR,
				AppliedAmountVND:     cmd.AppliedAmountVND,
				TransactionReference: reference,
				AuditEvent:           EventManualQRPaymentRecorded,
				AuditDetails: func(paymentID uuid.UUID, check lockedCheck) any {
					return manualQRPaymentAudit{
						PaymentID:                paymentID,
						CheckID:                  check.ID,
						SalesShiftID:             check.SalesShiftID,
						AppliedAmountVND:         cmd.AppliedAmountVND,
						ReceiptObservedInBankApp: true,
						TransactionReference:     reference,
					}
				},
			})
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			return 200, result, audit, nil
		})
}
