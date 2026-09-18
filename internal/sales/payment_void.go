package sales

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// voidPaymentFingerprint is the normalized, credential-free business input a
// Payment Void stands for: the Payment, the reason from the Void catalog, and
// the normalized note. The approver login code and PIN are deliberately
// absent, so rotating credentials cannot change the idempotency key
// (spec §10, §13).
type voidPaymentFingerprint struct {
	PaymentID uuid.UUID `json:"payment_id"`
	Reason    string    `json:"reason"`
	Note      *string   `json:"note"`
}

// voidPaymentFingerprintFor builds the fingerprint from the already-validated
// command and the already-normalized note.
func voidPaymentFingerprintFor(cmd VoidPaymentCommand, note *string) voidPaymentFingerprint {
	return voidPaymentFingerprint{PaymentID: cmd.PaymentID, Reason: cmd.Reason, Note: note}
}

// constraintPaymentVoidPaymentUnique is the one-Void-per-Payment constraint.
// A concurrent second Void serializes behind the Payment lock and then fails
// here, and the whole transaction — claim included — rolls back.
const constraintPaymentVoidPaymentUnique = "payment_void_payment_unique"

// mapVoidPaymentDBError maps exactly the named unique constraint to the typed
// condition it represents: one whole Void admits one Payment. Every other
// PostgreSQL error is returned untouched.
func mapVoidPaymentDBError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" &&
		pgErr.ConstraintName == constraintPaymentVoidPaymentUnique {
		return fmt.Errorf("%w: the payment already carries a void", ErrPaymentAlreadyVoided)
	}
	return err
}

// VoidPaymentHandler declares one whole Payment incorrect through one
// Manager-approved append-only reversal. The source Payment is never edited or
// deleted; the Void records its whole applied amount, and the owning Check
// reopens when the remaining valid coverage no longer covers its charge. The
// command is a pre-close command: the Service Session must still be ACTIVE and
// the original Payment Shift must still be the currently open one (spec §10).
type VoidPaymentHandler struct{ runner *Runner }

// NewVoidPaymentHandler creates a VoidPaymentHandler.
func NewVoidPaymentHandler(runner *Runner) *VoidPaymentHandler {
	return &VoidPaymentHandler{runner: runner}
}

// Handle executes the Void.
//
// The command is validated and its note normalized BEFORE the mutation
// begins, so a malformed request never claims its idempotency key. It requires
// the initiator's current sales.operate plus one inline Manager Approval for
// sales.operate; self-approval is permitted and the approver is recorded
// separately. Success answers 201 with the complete Service Session
// projection; the route layer maps domain errors through MapHTTPError.
func (h *VoidPaymentHandler) Handle(ctx context.Context, actor Actor,
	cmd VoidPaymentCommand,
) (int, ServiceSessionResponse, error) {
	var zero ServiceSessionResponse
	note := NormalizeVoidPaymentNote(cmd.Note)
	if err := ValidateVoidPaymentCommand(cmd, note); err != nil {
		return 0, zero, err
	}

	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpVoidPayment,
		Fingerprint: voidPaymentFingerprintFor(cmd, note),
		Required:    []string{CapSalesOperate},
		Approval: &ApprovalSpec{
			ApproverLoginCode:  cmd.ManagerApproval.ApproverLoginCode,
			ManagerPIN:         cmd.ManagerApproval.ManagerPIN,
			RequiredCapability: CapSalesOperate,
		},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, ServiceSessionResponse, AuditRecord, error) {
			result, audit, err := applyVoidPayment(ctx, h.runner, mc.Queries, actor,
				mc.Approver, cmd, note)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			return http.StatusCreated, result, audit, nil
		})
}

// resolveVoidPaymentCheckID resolves the Payment's Check before the mutation
// transaction takes its locks. No non-locking Payment-by-id read exists, so
// LockPaymentForVoid runs on the Runner's pooled handle outside the mutation:
// the statement's implicit transaction releases its row lock as soon as the
// row is scanned, and the mutation transaction still acquires the Check first
// (spec §11.1). The Payment's Check attribution is immutable, so the resolved
// Check cannot go stale, and the mutation re-locks the Payment under the Check
// lock and revalidates it.
func resolveVoidPaymentCheckID(ctx context.Context, runner *Runner,
	paymentID uuid.UUID,
) (uuid.UUID, error) {
	row, err := runner.queries.LockPaymentForVoid(ctx, paymentID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return uuid.Nil, fmt.Errorf("%w: %s", ErrPaymentNotFound, paymentID)
		}
		return uuid.Nil, fmt.Errorf("resolve void payment: %w", err)
	}
	return row.CheckID, nil
}

// applyVoidPayment is the mutation body, ordered so each step's failure leaves
// the transaction — approval, claim, and stored result included — to the
// executor's rollback. The lock order is the spec §11.1 protocol: the Check,
// its Service Session, the current open Shift, then the Payment.
//
// The Void is always whole: its amount is frozen from the locked Payment's
// applied amount, never a request value. Any Refund allocation — pending or
// completed, live or post-sale — refuses the whole command.
func applyVoidPayment(ctx context.Context, runner *Runner, q *sqlc.Queries, actor Actor,
	approver *auth.ApproverSummary, cmd VoidPaymentCommand, note *string,
) (ServiceSessionResponse, AuditRecord, error) {
	var zero ServiceSessionResponse
	if approver == nil {
		// The executor always supplies an approver for a spec with Approval;
		// a nil here is a wiring defect, not a client condition.
		return zero, AuditRecord{}, fmt.Errorf(
			"void payment: executor supplied no approver for a manager-approved command")
	}

	// 1. Pre-resolve the Payment's owning Check without the common lock order;
	// the mutation revalidates the attribution after the Check lock.
	checkID, err := resolveVoidPaymentCheckID(ctx, runner, cmd.PaymentID)
	if err != nil {
		return zero, AuditRecord{}, err
	}

	// 2. Lock order: Check, Service Session, current Shift, Payment.
	checkRow, err := q.LockCheckForPayment(ctx, checkID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return zero, AuditRecord{}, fmt.Errorf("%w: check %s", ErrCheckNotFound, checkID)
		}
		return zero, AuditRecord{}, fmt.Errorf("lock void check: %w", err)
	}
	sessionRow, err := q.LockServiceSessionForUpdate(ctx, checkRow.ServiceSessionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return zero, AuditRecord{}, fmt.Errorf("%w: session %s",
				ErrServiceSessionNotFound, checkRow.ServiceSessionID)
		}
		return zero, AuditRecord{}, fmt.Errorf("lock void service session: %w", err)
	}
	shiftID, err := lockOpenSalesShift(ctx, q)
	if err != nil {
		return zero, AuditRecord{}, err
	}
	paymentRow, err := q.LockPaymentForVoid(ctx, cmd.PaymentID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return zero, AuditRecord{}, fmt.Errorf(
				"%w: payment %s is absent from check %s after locking it",
				ErrPaymentNotFound, cmd.PaymentID, checkID)
		}
		return zero, AuditRecord{}, fmt.Errorf("lock void payment: %w", err)
	}

	// 3. Revalidate every pre-resolved fact under the locks. A disagreement is
	// a stored defect, never a client condition, because the Check attribution
	// is immutable.
	if paymentRow.CheckID != checkID {
		return zero, AuditRecord{}, fmt.Errorf(
			"%w: payment %s references check %s after locking %s",
			ErrFinancialInvariantViolated, paymentRow.ID, paymentRow.CheckID, checkID)
	}
	if sessionRow.State != StateActive {
		return zero, AuditRecord{}, fmt.Errorf(
			"%w: session %s is %s", ErrServiceSessionClosed,
			checkRow.ServiceSessionID, sessionRow.State)
	}
	if checkRow.State != CheckStateOpen && checkRow.State != CheckStateSettled {
		return zero, AuditRecord{}, fmt.Errorf(
			"%w: check %s is %s", ErrCheckNotOpen, checkID, checkRow.State)
	}
	if shiftID == uuid.Nil {
		return zero, AuditRecord{}, fmt.Errorf(
			"%w: no sales shift is open, so payment %s's shift cannot still be open",
			ErrPaymentVoidShiftClosed, paymentRow.ID)
	}
	if paymentRow.SalesShiftID != shiftID {
		return zero, AuditRecord{}, fmt.Errorf(
			"%w: payment %s was received in shift %s, not the open shift %s",
			ErrPaymentVoidShiftClosed, paymentRow.ID, paymentRow.SalesShiftID, shiftID)
	}

	// 4. Revalidate the Payment's own facts. The Check lock serializes
	// concurrent Voids, so a committed Void seen here is a duplicate rather
	// than a race; the unique constraint remains the backstop.
	checkPayments, err := q.ListCheckPayments(ctx, checkID)
	if err != nil {
		return zero, AuditRecord{}, fmt.Errorf("load check payments: %w", err)
	}
	found := false
	for _, payment := range checkPayments {
		if payment.ID != paymentRow.ID {
			continue
		}
		found = true
		if payment.VoidID.Valid {
			return zero, AuditRecord{}, fmt.Errorf("%w: %s", ErrPaymentAlreadyVoided, paymentRow.ID)
		}
		break
	}
	if !found {
		return zero, AuditRecord{}, fmt.Errorf(
			"%w: payment %s is absent from check %s", ErrFinancialInvariantViolated,
			paymentRow.ID, checkID)
	}
	allocations, err := q.ListPaymentRefundAllocations(ctx, []uuid.UUID{paymentRow.ID})
	if err != nil {
		return zero, AuditRecord{}, fmt.Errorf("load payment refund allocations: %w", err)
	}
	if len(allocations) > 0 {
		return zero, AuditRecord{}, fmt.Errorf("%w: %s", ErrPaymentHasRefund, paymentRow.ID)
	}

	// 5. Append the whole Void. The amount is the frozen applied amount.
	occurredAt, err := q.GetSalesOccurredAt(ctx)
	if err != nil {
		return zero, AuditRecord{}, fmt.Errorf("read void time: %w", err)
	}
	void, err := q.InsertPaymentVoid(ctx, sqlc.InsertPaymentVoidParams{
		PaymentID:                 paymentRow.ID,
		SalesShiftID:              paymentRow.SalesShiftID,
		AmountVnd:                 paymentRow.AppliedAmountVnd,
		Reason:                    cmd.Reason,
		Note:                      nullString(note),
		ActorStaffIdentityID:      actor.StaffID,
		StaffAccessSessionID:      actor.SessionID,
		ApprovedByStaffIdentityID: approver.ID,
		OccurredAt:                occurredAt,
	})
	if err != nil {
		return zero, AuditRecord{}, fmt.Errorf("insert payment void: %w", mapVoidPaymentDBError(err))
	}

	// 6. Recompute the corrected financials without the source Payment. A
	// SETTLED Check whose remaining valid coverage no longer covers the charge
	// reopens, clearing all four evidence columns in the same statement;
	// otherwise it stays exactly as it was.
	balanceVND, err := checkBalance(ctx, q, checkID, checkRow.ChargeVnd)
	if err != nil {
		return zero, AuditRecord{}, err
	}
	if checkRow.State == CheckStateSettled && balanceVND > 0 {
		if err := q.ReopenCheckAfterPaymentVoid(ctx, checkID); err != nil {
			return zero, AuditRecord{}, fmt.Errorf("reopen check after payment void: %w", err)
		}
		if err := writeVoidAudit(ctx, q, actor, occurredAt, EventCheckReopenedAfterPaymentVoid,
			checkReopenedAfterPaymentVoidAudit{
				CheckID:          checkID,
				PaymentVoidID:    void.ID,
				PaymentID:        paymentRow.ID,
				ServiceSessionID: checkRow.ServiceSessionID,
				SalesShiftID:     paymentRow.SalesShiftID,
				PriorState:       CheckStateSettled,
				ResultingState:   CheckStateOpen,
				ChargeVND:        checkRow.ChargeVnd,
				BalanceVND:       balanceVND,
				VoidedAmountVND:  paymentRow.AppliedAmountVnd,
			}); err != nil {
			return zero, AuditRecord{}, err
		}
	}

	// 7. Project the full Service Session the route returns.
	session, err := LoadServiceSession(ctx, q, checkRow.ServiceSessionID)
	if err != nil {
		return zero, AuditRecord{}, err
	}
	return session, AuditRecord{
		EventType: EventPaymentVoided,
		Details: paymentVoidedAudit{
			PaymentVoidID:             void.ID,
			PaymentID:                 paymentRow.ID,
			CheckID:                   checkID,
			ServiceSessionID:          checkRow.ServiceSessionID,
			SalesShiftID:              paymentRow.SalesShiftID,
			Method:                    paymentRow.Method,
			AmountVND:                 paymentRow.AppliedAmountVnd,
			Reason:                    cmd.Reason,
			Note:                      note,
			ActorStaffIdentityID:      actor.StaffID,
			ApprovedByStaffIdentityID: approver.ID,
		},
	}, nil
}

// audit detail shapes. Each carries stable business ids and financial meaning
// and never a Manager PIN, PIN hash, or login credential (spec §15).

type paymentVoidedAudit struct {
	PaymentVoidID             uuid.UUID `json:"payment_void_id"`
	PaymentID                 uuid.UUID `json:"payment_id"`
	CheckID                   uuid.UUID `json:"check_id"`
	ServiceSessionID          uuid.UUID `json:"service_session_id"`
	SalesShiftID              uuid.UUID `json:"sales_shift_id"`
	Method                    string    `json:"method"`
	AmountVND                 int64     `json:"amount_vnd"`
	Reason                    string    `json:"reason"`
	Note                      *string   `json:"note,omitempty"`
	ActorStaffIdentityID      uuid.UUID `json:"actor_staff_identity_id"`
	ApprovedByStaffIdentityID uuid.UUID `json:"approved_by_staff_identity_id"`
}

type checkReopenedAfterPaymentVoidAudit struct {
	CheckID          uuid.UUID `json:"check_id"`
	PaymentVoidID    uuid.UUID `json:"payment_void_id"`
	PaymentID        uuid.UUID `json:"payment_id"`
	ServiceSessionID uuid.UUID `json:"service_session_id"`
	SalesShiftID     uuid.UUID `json:"sales_shift_id"`
	PriorState       string    `json:"prior_state"`
	ResultingState   string    `json:"resulting_state"`
	ChargeVND        int64     `json:"charge_vnd"`
	BalanceVND       int64     `json:"balance_vnd"`
	VoidedAmountVND  int64     `json:"voided_amount_vnd"`
}

// writeVoidAudit inserts one Payment Void business event inside the mutation
// transaction, on the same occurrence instant as the facts it describes.
func writeVoidAudit(ctx context.Context, q *sqlc.Queries, actor Actor,
	occurredAt time.Time, eventType string, details any,
) error {
	raw, err := marshalAuditDetails(details)
	if err != nil {
		return err
	}
	if _, err := q.InsertAuditEvent(ctx, sqlc.InsertAuditEventParams{
		EventType:  eventType,
		ActorID:    uuid.NullUUID{UUID: actor.StaffID, Valid: true},
		SessionID:  uuid.NullUUID{UUID: actor.SessionID, Valid: true},
		Details:    raw,
		OccurredAt: occurredAt,
	}); err != nil {
		return fmt.Errorf("insert %s audit event: %w", eventType, err)
	}
	return nil
}
