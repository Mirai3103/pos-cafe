package sales

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
)

// refundFingerprint is the credential-free idempotency input. It carries the
// normalized business fields and the UUID-ordered allocation collections, and
// deliberately has no Manager Approval field, so rotating the approver or
// their PIN leaves the fingerprint byte-identical.
type refundFingerprint struct {
	CheckID               uuid.UUID                         `json:"check_id"`
	Method                string                            `json:"method"`
	AdjustmentAllocations []RefundAdjustmentAllocationInput `json:"adjustment_allocations"`
	PaymentAllocations    []RefundPaymentAllocationInput    `json:"payment_allocations"`
	Reason                string                            `json:"reason"`
	Note                  *string                           `json:"note"`
}

// refundFingerprintFor builds the fingerprint from the normalized command and
// the normalized note. It normalizes defensively so a caller cannot make a
// reordered request hash differently.
func refundFingerprintFor(cmd RecordRefundCommand, note *string) refundFingerprint {
	normalized := NormalizeRefundAllocations(cmd)
	return refundFingerprint{
		CheckID:               normalized.CheckID,
		Method:                normalized.Method,
		AdjustmentAllocations: normalized.AdjustmentAllocations,
		PaymentAllocations:    normalized.PaymentAllocations,
		Reason:                normalized.Reason,
		Note:                  note,
	}
}

// RecordRefundHandler records real money returned through the original Payment
// method. A live Refund consumes the active Session's corrected excess and
// returns the updated Service Session; a post-sale Refund consumes a
// Completed Sale's POST_SALE correction capacity and returns the sale's
// additive correction history without rewriting any closed row.
type RecordRefundHandler struct{ runner *Runner }

// NewRecordRefundHandler creates a RecordRefundHandler.
func NewRecordRefundHandler(runner *Runner) *RecordRefundHandler {
	return &RecordRefundHandler{runner: runner}
}

// Handle executes the Refund.
//
// The command is validated and its note normalized BEFORE the mutation begins,
// so a malformed request never claims its idempotency key. The command
// requires the initiator's sales.operate plus one inline Manager Approval for
// sales.operate; self-approval is permitted and the approver is recorded
// separately. Success answers 201; the route layer maps domain errors through
// ErrorResponse.
func (h *RecordRefundHandler) Handle(ctx context.Context, actor Actor,
	cmd RecordRefundCommand,
) (int, RefundResult, error) {
	note := NormalizeRefundNote(cmd.Note)
	cmd = NormalizeRefundAllocations(cmd)
	if err := ValidateRecordRefundCommand(cmd, note); err != nil {
		return 0, RefundResult{}, err
	}

	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpRecordRefund,
		Fingerprint: refundFingerprintFor(cmd, note),
		Required:    []string{CapSalesOperate},
		Approval: &ApprovalSpec{
			ApproverLoginCode:  cmd.ManagerApproval.ApproverLoginCode,
			ManagerPIN:         cmd.ManagerApproval.ManagerPIN,
			RequiredCapability: CapSalesOperate,
		},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, RefundResult, AuditRecord, error) {
			result, audit, err := applyRecordRefund(ctx, mc.Queries, actor, mc.Approver, cmd, note)
			if err != nil {
				return 0, RefundResult{}, AuditRecord{}, err
			}
			return http.StatusCreated, result, audit, nil
		})
}

// applyRecordRefund is the mutation body, ordered so each step's failure
// leaves the transaction — approval, claim, and stored result included — to
// the executor's rollback. The lock order is the spec §11.1 protocol: Check,
// Service Session, current open Shift, selected Payments by UUID, then
// selected Charge Adjustments by UUID.
//
// Both capacities are verified independently and against every existing
// Refund allocation, including pending Manual QR intents: a Refund allocates
// against corrected Charge Adjustments and original Payments in equal sums, so
// no allocation can be spent twice and the live pending Refund can never go
// negative.
func applyRecordRefund(ctx context.Context, q *sqlc.Queries, actor Actor,
	approver *auth.ApproverSummary, cmd RecordRefundCommand, note *string,
) (RefundResult, AuditRecord, error) {
	if approver == nil {
		// The executor always supplies an approver for a spec with Approval;
		// a nil here is a wiring defect, not a client condition.
		return RefundResult{}, AuditRecord{}, fmt.Errorf(
			"record refund: executor supplied no approver for a manager-approved command")
	}

	// Validation proved the sums agree; re-derive the total with the guarded
	// arithmetic and refuse a stored-input disagreement as a defect.
	amountVND, err := sumRefundPaymentAllocationAmounts(cmd.PaymentAllocations)
	if err != nil {
		return RefundResult{}, AuditRecord{}, err
	}
	adjustmentTotalVND, err := sumRefundAdjustmentAllocationAmounts(cmd.AdjustmentAllocations)
	if err != nil {
		return RefundResult{}, AuditRecord{}, err
	}
	if amountVND != adjustmentTotalVND {
		return RefundResult{}, AuditRecord{}, fmt.Errorf(
			"%w: refund allocations sum to %d and %d after validation",
			ErrFinancialInvariantViolated, amountVND, adjustmentTotalVND)
	}

	// 1. Lock the Check. Unlike a Payment, a live Refund may serve an OPEN or
	// a SETTLED Check, and a post-sale Refund serves a closed Session's, so
	// the shared OPEN-only precondition does not apply.
	checkRow, err := q.LockCheckForPayment(ctx, cmd.CheckID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RefundResult{}, AuditRecord{}, fmt.Errorf("%w: check %s", ErrCheckNotFound, cmd.CheckID)
		}
		return RefundResult{}, AuditRecord{}, fmt.Errorf("lock refund check: %w", err)
	}

	// 2. Lock the Service Session, which decides the live/post-sale scope
	// structurally: closure takes the same lock, so a Refund observes exactly
	// one state rather than racing it.
	sessionRow, err := q.LockServiceSessionForUpdate(ctx, checkRow.ServiceSessionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RefundResult{}, AuditRecord{}, fmt.Errorf(
				"%w: session %s", ErrServiceSessionNotFound, checkRow.ServiceSessionID)
		}
		return RefundResult{}, AuditRecord{}, fmt.Errorf("lock refund service session: %w", err)
	}

	// 3. Require the current open Sales Shift.
	shiftID, err := lockOpenSalesShift(ctx, q)
	if err != nil {
		return RefundResult{}, AuditRecord{}, err
	}
	if shiftID == uuid.Nil {
		return RefundResult{}, AuditRecord{}, fmt.Errorf("%w: no sales shift is open",
			ErrOpenShiftRequired)
	}

	var saleID uuid.UUID
	isPostSale := false
	switch sessionRow.State {
	case StateActive:
		if checkRow.State != CheckStateOpen && checkRow.State != CheckStateSettled {
			return RefundResult{}, AuditRecord{}, fmt.Errorf(
				"%w: check %s is %s", ErrCheckNotOpen, checkRow.ID, checkRow.State)
		}
	case StateClosed:
		saleID, err = q.FindCompletedSaleByServiceSession(ctx, checkRow.ServiceSessionID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return RefundResult{}, AuditRecord{}, fmt.Errorf(
					"%w: closed session %s carries no completed sale",
					ErrFinancialInvariantViolated, checkRow.ServiceSessionID)
			}
			return RefundResult{}, AuditRecord{}, fmt.Errorf("find completed sale: %w", err)
		}
		isPostSale = true
	default:
		return RefundResult{}, AuditRecord{}, fmt.Errorf(
			"%w: session %s is %s",
			ErrFinancialInvariantViolated, checkRow.ServiceSessionID, sessionRow.State)
	}

	// 4. Lock the selected Payments and Charge Adjustments, each ascending
	// UUID, after the Check, Session, and Shift.
	paymentIDs := make([]uuid.UUID, len(cmd.PaymentAllocations))
	paymentAmounts := make([]int64, len(cmd.PaymentAllocations))
	for i, allocation := range cmd.PaymentAllocations {
		paymentIDs[i] = allocation.PaymentID
		paymentAmounts[i] = allocation.AmountVND
	}
	adjustmentIDs := make([]uuid.UUID, len(cmd.AdjustmentAllocations))
	adjustmentAmounts := make([]int64, len(cmd.AdjustmentAllocations))
	for i, allocation := range cmd.AdjustmentAllocations {
		adjustmentIDs[i] = allocation.ChargeAdjustmentID
		adjustmentAmounts[i] = allocation.AmountVND
	}

	lockedPayments, err := q.LockPaymentsForRefund(ctx, paymentIDs)
	if err != nil {
		return RefundResult{}, AuditRecord{}, fmt.Errorf("lock refund payments: %w", err)
	}
	if len(lockedPayments) != len(paymentIDs) {
		return RefundResult{}, AuditRecord{}, fmt.Errorf(
			"%w: a selected payment does not exist", ErrRefundAllocationInvalid)
	}
	lockedAdjustments, err := q.LockChargeAdjustmentsForRefund(ctx, adjustmentIDs)
	if err != nil {
		return RefundResult{}, AuditRecord{}, fmt.Errorf("lock refund charge adjustments: %w", err)
	}
	if len(lockedAdjustments) != len(adjustmentIDs) {
		return RefundResult{}, AuditRecord{}, fmt.Errorf(
			"%w: a selected charge adjustment does not exist", ErrRefundAllocationInvalid)
	}

	// Revalidate the Payment sources: same Check, same method, not voided.
	checkPayments, err := q.ListCheckPayments(ctx, cmd.CheckID)
	if err != nil {
		return RefundResult{}, AuditRecord{}, fmt.Errorf("load check payments: %w", err)
	}
	voidedPayments := make(map[uuid.UUID]bool, len(checkPayments))
	for _, payment := range checkPayments {
		if payment.VoidID.Valid {
			voidedPayments[payment.ID] = true
		}
	}
	for _, payment := range lockedPayments {
		if payment.CheckID != cmd.CheckID {
			return RefundResult{}, AuditRecord{}, fmt.Errorf(
				"%w: payment %s belongs to check %s",
				ErrRefundAllocationInvalid, payment.ID, payment.CheckID)
		}
		if payment.Method != cmd.Method {
			return RefundResult{}, AuditRecord{}, fmt.Errorf(
				"%w: payment %s is %s", ErrRefundMethodMismatch, payment.ID, payment.Method)
		}
		if voidedPayments[payment.ID] {
			return RefundResult{}, AuditRecord{}, fmt.Errorf(
				"%w: payment %s is voided", ErrRefundExceedsPaymentCapacity, payment.ID)
		}
	}

	// Revalidate the Charge Adjustment sources: same Check and the scope the
	// locked Session selected. Live and post-sale sources never mix.
	for _, adjustment := range lockedAdjustments {
		if adjustment.CheckID != cmd.CheckID {
			return RefundResult{}, AuditRecord{}, fmt.Errorf(
				"%w: charge adjustment %s belongs to check %s",
				ErrRefundAllocationInvalid, adjustment.ID, adjustment.CheckID)
		}
		if isPostSale {
			if adjustment.Scope != CompScopePostSale || !adjustment.CompletedSaleID.Valid ||
				adjustment.CompletedSaleID.UUID != saleID {
				return RefundResult{}, AuditRecord{}, fmt.Errorf(
					"%w: charge adjustment %s is not a post-sale correction of sale %s",
					ErrRefundAllocationInvalid, adjustment.ID, saleID)
			}
			continue
		}
		if adjustment.Scope != CompScopeLiveCheck || adjustment.CompletedSaleID.Valid {
			return RefundResult{}, AuditRecord{}, fmt.Errorf(
				"%w: charge adjustment %s is not a live correction of check %s",
				ErrRefundAllocationInvalid, adjustment.ID, cmd.CheckID)
		}
	}

	// 5. Bound the pending obligation cumulatively, before any insertion. The
	// request may only spend what the Check or Completed Sale still owes back
	// after every existing PENDING Refund, each of which reserved its amount
	// when it was recorded. A completed Refund already reduced the obligation
	// through the corrected financials, so it is not reserved a second time;
	// this gate reports obligation overruns in every scope, so a set of
	// intents can never promise more than can ever be paid back.
	availableVND, target, err := loadPendingRefundHeadroom(ctx, q, cmd.CheckID, saleID,
		isPostSale, checkRow.ChargeVnd)
	if err != nil {
		return RefundResult{}, AuditRecord{}, err
	}
	if amountVND > availableVND {
		return RefundResult{}, AuditRecord{}, fmt.Errorf(
			"%w: refund %d exceeds %s pending refund headroom %d",
			ErrRefundExceedsPendingRefund, amountVND, target, availableVND)
	}

	// 6. Sum every existing allocation, including pending Manual QR intents,
	// and verify each source's remaining capacity independently.
	if err := assertRefundCapacity(ctx, q, cmd, lockedPayments, lockedAdjustments); err != nil {
		return RefundResult{}, AuditRecord{}, err
	}

	// 7. Insert the Refund and both allocation sets.
	occurredAt := time.Now()
	refund, err := q.InsertRefund(ctx, sqlc.InsertRefundParams{
		CheckID:                   cmd.CheckID,
		CompletedSaleID:           uuid.NullUUID{UUID: saleID, Valid: isPostSale},
		SalesShiftID:              shiftID,
		Method:                    cmd.Method,
		AmountVnd:                 amountVND,
		Reason:                    cmd.Reason,
		Note:                      nullString(note),
		ActorStaffIdentityID:      actor.StaffID,
		StaffAccessSessionID:      actor.SessionID,
		ApprovedByStaffIdentityID: approver.ID,
		CreatedAt:                 occurredAt,
	})
	if err != nil {
		return RefundResult{}, AuditRecord{}, fmt.Errorf("insert refund: %w", err)
	}
	if err := q.InsertRefundPaymentAllocations(ctx, sqlc.InsertRefundPaymentAllocationsParams{
		RefundID:   refund.ID,
		PaymentIds: paymentIDs,
		Amounts:    paymentAmounts,
	}); err != nil {
		return RefundResult{}, AuditRecord{}, fmt.Errorf("insert refund payment allocations: %w", err)
	}
	if err := q.InsertRefundAdjustmentAllocations(ctx, sqlc.InsertRefundAdjustmentAllocationsParams{
		RefundID:            refund.ID,
		ChargeAdjustmentIds: adjustmentIDs,
		Amounts:             adjustmentAmounts,
	}); err != nil {
		return RefundResult{}, AuditRecord{}, fmt.Errorf("insert refund adjustment allocations: %w", err)
	}

	// 8. A Cash Refund completes in the same transaction: staff recorded the
	// money leaving. A Manual QR Refund stays pending until confirmation.
	var completion *sqlc.RefundCompletion
	if cmd.Method == RefundMethodCash {
		inserted, err := q.InsertRefundCompletion(ctx, sqlc.InsertRefundCompletionParams{
			RefundID:                   refund.ID,
			TransactionReference:       sql.NullString{},
			CompletedByStaffIdentityID: actor.StaffID,
			StaffAccessSessionID:       actor.SessionID,
			CompletedAt:                occurredAt,
		})
		if err != nil {
			return RefundResult{}, AuditRecord{}, fmt.Errorf("insert refund completion: %w", err)
		}
		completion = &inserted
	}

	paymentRows, err := q.ListRefundPaymentAllocations(ctx, refund.ID)
	if err != nil {
		return RefundResult{}, AuditRecord{}, fmt.Errorf("load refund payment allocations: %w", err)
	}
	adjustmentRows, err := q.ListRefundAdjustmentAllocations(ctx, refund.ID)
	if err != nil {
		return RefundResult{}, AuditRecord{}, fmt.Errorf("load refund adjustment allocations: %w", err)
	}
	refundResponse := refundResponseFromFact(refund, paymentRows, adjustmentRows, completion)

	result := RefundResult{Scope: CompScopeLiveCheck, Refund: refundResponse}
	if isPostSale {
		history, err := loadPostSaleCorrectionHistory(ctx, q, saleID, &refundResponse)
		if err != nil {
			return RefundResult{}, AuditRecord{}, err
		}
		result.Scope = CompScopePostSale
		result.CompletedSaleID = &saleID
		result.PostSaleCorrections = history
	} else {
		session, err := LoadServiceSession(ctx, q, checkRow.ServiceSessionID)
		if err != nil {
			return RefundResult{}, AuditRecord{}, err
		}
		result.ServiceSession = &session
	}

	audit := AuditRecord{
		EventType: EventRefundRecorded,
		Details: refundRecordedAudit{
			RefundID:                  refund.ID,
			CheckID:                   refund.CheckID,
			CompletedSaleID:           nullUUIDPtr(refund.CompletedSaleID),
			SalesShiftID:              refund.SalesShiftID,
			Method:                    refund.Method,
			AmountVND:                 refund.AmountVnd,
			PaymentAllocations:        refundPaymentAllocationAudits(cmd.PaymentAllocations),
			AdjustmentAllocations:     refundAdjustmentAllocationAudits(cmd.AdjustmentAllocations),
			Reason:                    refund.Reason,
			Note:                      note,
			ActorStaffIdentityID:      actor.StaffID,
			ApprovedByStaffIdentityID: approver.ID,
		},
	}

	if completion != nil {
		if err := writeRefundAudit(ctx, q, actor, occurredAt, EventRefundCompleted,
			refundCompletedAudit{
				RefundID:                      refund.ID,
				CheckID:                       refund.CheckID,
				CompletedSaleID:               nullUUIDPtr(refund.CompletedSaleID),
				SalesShiftID:                  refund.SalesShiftID,
				Method:                        refund.Method,
				AmountVND:                     refund.AmountVnd,
				CompletedByStaffIdentityID:    completion.CompletedByStaffIdentityID,
				CompletedStaffAccessSessionID: completion.StaffAccessSessionID,
				CompletedAt:                   completion.CompletedAt,
			}); err != nil {
			return RefundResult{}, AuditRecord{}, err
		}
	}

	return result, audit, nil
}

// ---------- Phase 6C: Confirm Manual QR Refund ----------

// OpConfirmQRRefund is the idempotency action name stored in
// idempotency_keys.action (VARCHAR(50)).
const OpConfirmQRRefund = "sales.confirm_qr_refund"

// EventManualQRRefundCompleted records that staff confirmed the outbound
// transfer of an approved Manual QR Refund (spec §15).
const EventManualQRRefundCompleted = "MANUAL_QR_REFUND_COMPLETED"

// ConfirmManualQRRefundCommand confirms that an approved Manual QR Refund's
// outbound transfer occurred. RefundID is json:"-": it comes from the path,
// never the body. TransactionReference is the optional outbound bank reference,
// trimmed and bounded to MaxTransactionReferenceLength characters (spec §9.2).
type ConfirmManualQRRefundCommand struct {
	RequestID            uuid.UUID `json:"request_id"`
	RefundID             uuid.UUID `json:"-"`
	TransactionReference *string   `json:"transaction_reference"`
}

// NormalizeRefundReference trims surrounding whitespace from an optional
// confirmation reference, collapses a blank reference to nil, and bounds a
// present one to MaxTransactionReferenceLength code points. Callers normalize
// BEFORE building the fingerprint, so replays of differently padded input stay
// equal.
func NormalizeRefundReference(ref *string) (*string, error) {
	if ref == nil {
		return nil, nil
	}
	trimmed := strings.TrimSpace(*ref)
	if trimmed == "" {
		return nil, nil
	}
	if runes := utf8.RuneCountInString(trimmed); runes > MaxTransactionReferenceLength {
		return nil, fmt.Errorf(
			"%w: transaction_reference is %d characters, maximum is %d",
			response.ErrInvalid, runes, MaxTransactionReferenceLength)
	}
	return &trimmed, nil
}

// confirmManualQRRefundFingerprint is the credential-free idempotency input of
// one confirmation: the Refund it attests and the normalized outbound
// reference. Confirmation carries no Manager Approval, so no credential can
// shape the hash.
type confirmManualQRRefundFingerprint struct {
	RefundID             uuid.UUID `json:"refund_id"`
	TransactionReference *string   `json:"transaction_reference"`
}

func confirmManualQRRefundFingerprintFor(refundID uuid.UUID,
	reference *string,
) confirmManualQRRefundFingerprint {
	return confirmManualQRRefundFingerprint{RefundID: refundID, TransactionReference: reference}
}

// ConfirmManualQRRefundHandler completes an approved Manual QR Refund once
// staff confirm the outbound transfer. The Manager Approval that authorized the
// Refund is not repeated: current sales.operate authority plus the recorded
// intent are enough, because confirmation attests that the approved money
// movement occurred rather than approving a new one (spec §9.2).
type ConfirmManualQRRefundHandler struct{ runner *Runner }

// NewConfirmManualQRRefundHandler creates a ConfirmManualQRRefundHandler.
func NewConfirmManualQRRefundHandler(runner *Runner) *ConfirmManualQRRefundHandler {
	return &ConfirmManualQRRefundHandler{runner: runner}
}

// Handle executes the confirmation.
//
// The command is validated and its reference normalized BEFORE the mutation
// begins, so a malformed request never claims its idempotency key. The command
// requires the initiator's current sales.operate and no second approval.
// Success answers 200; the route layer maps domain errors through
// ErrorResponse.
func (h *ConfirmManualQRRefundHandler) Handle(ctx context.Context, actor Actor,
	cmd ConfirmManualQRRefundCommand,
) (int, RefundResult, error) {
	if cmd.RequestID == uuid.Nil {
		return 0, RefundResult{}, fmt.Errorf("%w: request_id is required", response.ErrInvalid)
	}
	if cmd.RefundID == uuid.Nil {
		return 0, RefundResult{}, fmt.Errorf("%w: refund_id is required", response.ErrInvalid)
	}
	reference, err := NormalizeRefundReference(cmd.TransactionReference)
	if err != nil {
		return 0, RefundResult{}, err
	}

	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpConfirmQRRefund,
		Fingerprint: confirmManualQRRefundFingerprintFor(cmd.RefundID, reference),
		Required:    []string{CapSalesOperate},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, RefundResult, AuditRecord, error) {
			result, audit, err := applyConfirmManualQRRefund(ctx, h.runner, mc.Queries,
				actor, cmd, reference)
			if err != nil {
				return 0, RefundResult{}, AuditRecord{}, err
			}
			return http.StatusOK, result, audit, nil
		})
}

// resolveRefundCheckID resolves the Refund's Check before the mutation
// transaction takes its locks. LockRefundForConfirmation is the generated
// refund-by-id read and it takes the Refund row lock, so this pre-resolution
// runs on the Runner's pooled handle outside the mutation: the statement's
// implicit transaction releases that lock as soon as the row is scanned, and
// the mutation transaction still acquires the Check first (spec §11.1). The
// Refund row is append-only, so the resolved Check cannot go stale, and the
// mutation re-locks the row under the Check lock and revalidates it.
func resolveRefundCheckID(ctx context.Context, runner *Runner, refundID uuid.UUID) (uuid.UUID, error) {
	row, err := runner.queries.LockRefundForConfirmation(ctx, refundID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return uuid.Nil, fmt.Errorf("%w: %s", ErrRefundNotFound, refundID)
		}
		return uuid.Nil, fmt.Errorf("resolve refund check: %w", err)
	}
	return row.CheckID, nil
}

// applyConfirmManualQRRefund is the mutation body. The lock order is the §11.1
// protocol: the Check, then its Session, then the current open Shift, then the
// Refund row itself. After the locks it re-derives the obligation the
// completion is about to resolve, so a Refund recorded against an obligation
// that later shrank — a concurrent Void or an out-of-band completion — cannot
// move money it no longer has, and a completed Refund can never make a Check's
// balance positive (spec §6.2). It never edits the Refund row or its
// allocations.
func applyConfirmManualQRRefund(ctx context.Context, runner *Runner, q *sqlc.Queries,
	actor Actor, cmd ConfirmManualQRRefundCommand, reference *string,
) (RefundResult, AuditRecord, error) {
	checkID, err := resolveRefundCheckID(ctx, runner, cmd.RefundID)
	if err != nil {
		return RefundResult{}, AuditRecord{}, err
	}

	checkRow, err := q.LockCheckForPayment(ctx, checkID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RefundResult{}, AuditRecord{}, fmt.Errorf(
				"%w: refund %s references check %s",
				ErrFinancialInvariantViolated, cmd.RefundID, checkID)
		}
		return RefundResult{}, AuditRecord{}, fmt.Errorf("lock confirmation check: %w", err)
	}
	if _, err := q.LockServiceSessionForUpdate(ctx, checkRow.ServiceSessionID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RefundResult{}, AuditRecord{}, fmt.Errorf(
				"%w: session %s", ErrServiceSessionNotFound, checkRow.ServiceSessionID)
		}
		return RefundResult{}, AuditRecord{}, fmt.Errorf("lock confirmation service session: %w", err)
	}
	shiftID, err := lockOpenSalesShift(ctx, q)
	if err != nil {
		return RefundResult{}, AuditRecord{}, err
	}
	if shiftID == uuid.Nil {
		return RefundResult{}, AuditRecord{}, fmt.Errorf("%w: no sales shift is open",
			ErrOpenShiftRequired)
	}
	refundRow, err := q.LockRefundForConfirmation(ctx, cmd.RefundID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RefundResult{}, AuditRecord{}, fmt.Errorf("%w: %s", ErrRefundNotFound, cmd.RefundID)
		}
		return RefundResult{}, AuditRecord{}, fmt.Errorf("lock refund for confirmation: %w", err)
	}

	// Revalidate the locked Refund. The Check attribution is immutable, so a
	// disagreement is a stored defect rather than a client condition.
	if refundRow.CheckID != checkID {
		return RefundResult{}, AuditRecord{}, fmt.Errorf(
			"%w: refund %s references check %s after locking %s",
			ErrFinancialInvariantViolated, refundRow.ID, refundRow.CheckID, checkID)
	}
	if refundRow.Method != RefundMethodManualQR {
		return RefundResult{}, AuditRecord{}, fmt.Errorf(
			"%w: refund %s is %s", ErrRefundMethodMismatch, refundRow.ID, refundRow.Method)
	}
	if refundRow.CompletionID.Valid {
		return RefundResult{}, AuditRecord{}, fmt.Errorf(
			"%w: refund %s", ErrRefundAlreadyCompleted, refundRow.ID)
	}
	if refundRow.SalesShiftID != shiftID {
		return RefundResult{}, AuditRecord{}, fmt.Errorf(
			"%w: refund %s was issued in shift %s, not the open shift %s",
			ErrOpenShiftRequired, refundRow.ID, refundRow.SalesShiftID, shiftID)
	}

	// Re-derive the obligation under the Check lock before appending anything.
	if err := assertRefundConfirmationHeadroom(ctx, q, refundRow, checkRow.ChargeVnd); err != nil {
		return RefundResult{}, AuditRecord{}, err
	}

	completedAt, err := q.GetSalesOccurredAt(ctx)
	if err != nil {
		return RefundResult{}, AuditRecord{}, fmt.Errorf("read confirmation time: %w", err)
	}
	completion, err := q.InsertRefundCompletion(ctx, sqlc.InsertRefundCompletionParams{
		RefundID:                   refundRow.ID,
		TransactionReference:       nullString(reference),
		CompletedByStaffIdentityID: actor.StaffID,
		StaffAccessSessionID:       actor.SessionID,
		CompletedAt:                completedAt,
	})
	if err != nil {
		return RefundResult{}, AuditRecord{}, fmt.Errorf("insert refund completion: %w", err)
	}

	refundResponse, err := loadConfirmedRefundResponse(ctx, q, refundRow, completion)
	if err != nil {
		return RefundResult{}, AuditRecord{}, err
	}
	result := RefundResult{Scope: CompScopeLiveCheck, Refund: refundResponse}
	if refundRow.CompletedSaleID.Valid {
		saleID := refundRow.CompletedSaleID.UUID
		history, err := loadPostSaleCorrectionHistory(ctx, q, saleID, &refundResponse)
		if err != nil {
			return RefundResult{}, AuditRecord{}, err
		}
		result.Scope = CompScopePostSale
		result.CompletedSaleID = &saleID
		result.PostSaleCorrections = history
	} else {
		session, err := LoadServiceSession(ctx, q, checkRow.ServiceSessionID)
		if err != nil {
			return RefundResult{}, AuditRecord{}, err
		}
		result.ServiceSession = &session
	}

	audit := AuditRecord{
		EventType: EventManualQRRefundCompleted,
		Details: refundCompletedAudit{
			RefundID:                      refundRow.ID,
			CheckID:                       refundRow.CheckID,
			CompletedSaleID:               nullUUIDPtr(refundRow.CompletedSaleID),
			SalesShiftID:                  refundRow.SalesShiftID,
			Method:                        refundRow.Method,
			AmountVND:                     refundRow.AmountVnd,
			CompletedByStaffIdentityID:    completion.CompletedByStaffIdentityID,
			CompletedStaffAccessSessionID: completion.StaffAccessSessionID,
			CompletedAt:                   completion.CompletedAt,
		},
	}
	return result, audit, nil
}

// assertRefundConfirmationHeadroom re-derives, under the Check lock, the
// obligation this completion is about to resolve, and refuses an amount above
// it. The live branch uses the corrected Check financials with pending intents
// still excluded; the post-sale branch uses the Completed Sale's outstanding
// correction amount. A refund can therefore never promise more than its source
// still owes back, even if a concurrent Void or completion shrank the
// obligation after the Refund was recorded (spec §6.2).
func assertRefundConfirmationHeadroom(ctx context.Context, q *sqlc.Queries,
	refund sqlc.LockRefundForConfirmationRow, storedChargeVND int64,
) error {
	if refund.CompletedSaleID.Valid {
		outstandingVND, err := loadOutstandingPostSaleRefundVND(ctx, q, refund.CompletedSaleID.UUID)
		if err != nil {
			return err
		}
		if refund.AmountVnd > outstandingVND {
			return fmt.Errorf(
				"%w: refund %s amount %d exceeds sale %s outstanding %d",
				ErrRefundExceedsPendingRefund, refund.ID, refund.AmountVnd,
				refund.CompletedSaleID.UUID, outstandingVND)
		}
		return nil
	}

	pendingVND, err := loadCheckPendingRefundVND(ctx, q, refund.CheckID, storedChargeVND)
	if err != nil {
		return err
	}
	if refund.AmountVnd > pendingVND {
		return fmt.Errorf(
			"%w: refund %s amount %d exceeds check %s pending refund %d",
			ErrRefundExceedsPendingRefund, refund.ID, refund.AmountVnd, refund.CheckID, pendingVND)
	}
	return nil
}

// loadConfirmedRefundResponse rebuilds the response for the Refund the
// confirmation just completed, from the locked Refund row and the completion it
// appended. The Refund row's reason, note, and identities live on the source
// scope's read — the live Check's Refunds or the Completed Sale's additive
// history — so the response carries the same shape Record Refund returns, now
// with derived state COMPLETED.
func loadConfirmedRefundResponse(ctx context.Context, q *sqlc.Queries,
	refund sqlc.LockRefundForConfirmationRow, completion sqlc.RefundCompletion,
) (RefundResponse, error) {
	fact := sqlc.Refund{
		ID:              refund.ID,
		CheckID:         refund.CheckID,
		CompletedSaleID: refund.CompletedSaleID,
		SalesShiftID:    refund.SalesShiftID,
		Method:          refund.Method,
		AmountVnd:       refund.AmountVnd,
		CreatedAt:       refund.CreatedAt,
	}

	if refund.CompletedSaleID.Valid {
		rows, err := q.ListCompletedSalePostSaleCorrections(ctx, refund.CompletedSaleID.UUID)
		if err != nil {
			return RefundResponse{}, fmt.Errorf("load post-sale corrections: %w", err)
		}
		found := false
		for _, row := range rows {
			if !row.EntryKind.Valid || row.EntryKind.String != postSaleEntryKindRefund ||
				!row.RefundID.Valid || row.RefundID.UUID != refund.ID {
				continue
			}
			if !row.Reason.Valid || !row.ActorStaffIdentityID.Valid ||
				!row.ApprovedByStaffIdentityID.Valid {
				return RefundResponse{}, fmt.Errorf(
					"%w: post-sale refund %s is missing its recorded reason or identities",
					ErrFinancialInvariantViolated, refund.ID)
			}
			fact.Reason = row.Reason.String
			fact.Note = row.Note
			fact.ActorStaffIdentityID = row.ActorStaffIdentityID.UUID
			fact.ApprovedByStaffIdentityID = row.ApprovedByStaffIdentityID.UUID
			found = true
			break
		}
		if !found {
			return RefundResponse{}, fmt.Errorf(
				"%w: post-sale refund %s is absent from sale %s's history",
				ErrFinancialInvariantViolated, refund.ID, refund.CompletedSaleID.UUID)
		}
	} else {
		rows, err := q.ListCheckRefunds(ctx, refund.CheckID)
		if err != nil {
			return RefundResponse{}, fmt.Errorf("load check refunds: %w", err)
		}
		found := false
		for _, row := range rows {
			if row.ID != refund.ID {
				continue
			}
			fact.Reason = row.Reason
			fact.Note = row.Note
			fact.ActorStaffIdentityID = row.ActorStaffIdentityID
			fact.StaffAccessSessionID = row.StaffAccessSessionID
			fact.ApprovedByStaffIdentityID = row.ApprovedByStaffIdentityID
			found = true
			break
		}
		if !found {
			return RefundResponse{}, fmt.Errorf(
				"%w: live refund %s is absent from check %s's refunds",
				ErrFinancialInvariantViolated, refund.ID, refund.CheckID)
		}
	}

	paymentRows, err := q.ListRefundPaymentAllocations(ctx, refund.ID)
	if err != nil {
		return RefundResponse{}, fmt.Errorf("load refund payment allocations: %w", err)
	}
	adjustmentRows, err := q.ListRefundAdjustmentAllocations(ctx, refund.ID)
	if err != nil {
		return RefundResponse{}, fmt.Errorf("load refund adjustment allocations: %w", err)
	}
	return refundResponseFromFact(fact, paymentRows, adjustmentRows, &completion), nil
}

// assertRefundCapacity sums every existing Refund allocation against each
// selected source — pending Manual QR intents included, because they reserve
// capacity before money moves — and refuses any requested allocation beyond
// the source's remainder. Adjustment capacities are verified before Payment
// capacities so the reported conflict is stable when both are exhausted.
func assertRefundCapacity(ctx context.Context, q *sqlc.Queries, cmd RecordRefundCommand,
	lockedPayments []sqlc.LockPaymentsForRefundRow,
	lockedAdjustments []sqlc.LockChargeAdjustmentsForRefundRow,
) error {
	adjustmentAllocations, err := q.ListAdjustmentRefundAllocations(ctx,
		refundAdjustmentIDs(cmd.AdjustmentAllocations))
	if err != nil {
		return fmt.Errorf("load adjustment refund allocations: %w", err)
	}
	consumedAdjustment := make(map[uuid.UUID]int64, len(adjustmentAllocations))
	for _, allocation := range adjustmentAllocations {
		next, err := AddCharge(consumedAdjustment[allocation.ChargeAdjustmentID], allocation.AmountVnd)
		if err != nil {
			return fmt.Errorf("%w: adjustment refund allocations: %v",
				ErrFinancialInvariantViolated, err)
		}
		consumedAdjustment[allocation.ChargeAdjustmentID] = next
	}
	requestedAdjustment := make(map[uuid.UUID]int64, len(cmd.AdjustmentAllocations))
	for _, allocation := range cmd.AdjustmentAllocations {
		next, err := AddCharge(requestedAdjustment[allocation.ChargeAdjustmentID], allocation.AmountVND)
		if err != nil {
			return fmt.Errorf("%w: adjustment refund request: %v",
				ErrFinancialInvariantViolated, err)
		}
		requestedAdjustment[allocation.ChargeAdjustmentID] = next
	}
	for _, adjustment := range lockedAdjustments {
		consumed := consumedAdjustment[adjustment.ID]
		if consumed > adjustment.AmountVnd {
			return fmt.Errorf(
				"%w: charge adjustment %s amount %d is below its refund allocations %d",
				ErrFinancialInvariantViolated, adjustment.ID, adjustment.AmountVnd, consumed)
		}
		if requested := requestedAdjustment[adjustment.ID]; requested > adjustment.AmountVnd-consumed {
			return fmt.Errorf(
				"%w: adjustment %s needs %d but only %d remains",
				ErrRefundExceedsAdjustmentCapacity, adjustment.ID, requested,
				adjustment.AmountVnd-consumed)
		}
	}

	paymentAllocations, err := q.ListPaymentRefundAllocations(ctx, refundPaymentIDs(cmd.PaymentAllocations))
	if err != nil {
		return fmt.Errorf("load payment refund allocations: %w", err)
	}
	consumedPayment := make(map[uuid.UUID]int64, len(paymentAllocations))
	for _, allocation := range paymentAllocations {
		next, err := AddCharge(consumedPayment[allocation.PaymentID], allocation.AmountVnd)
		if err != nil {
			return fmt.Errorf("%w: payment refund allocations: %v",
				ErrFinancialInvariantViolated, err)
		}
		consumedPayment[allocation.PaymentID] = next
	}
	requestedPayment := make(map[uuid.UUID]int64, len(cmd.PaymentAllocations))
	for _, allocation := range cmd.PaymentAllocations {
		next, err := AddCharge(requestedPayment[allocation.PaymentID], allocation.AmountVND)
		if err != nil {
			return fmt.Errorf("%w: payment refund request: %v", ErrFinancialInvariantViolated, err)
		}
		requestedPayment[allocation.PaymentID] = next
	}
	for _, payment := range lockedPayments {
		consumed := consumedPayment[payment.ID]
		if consumed > payment.AppliedAmountVnd {
			return fmt.Errorf(
				"%w: payment %s applied %d is below its refund allocations %d",
				ErrFinancialInvariantViolated, payment.ID, payment.AppliedAmountVnd, consumed)
		}
		if requested := requestedPayment[payment.ID]; requested > payment.AppliedAmountVnd-consumed {
			return fmt.Errorf(
				"%w: payment %s needs %d but only %d remains",
				ErrRefundExceedsPaymentCapacity, payment.ID, requested,
				payment.AppliedAmountVnd-consumed)
		}
	}
	return nil
}

func refundPaymentIDs(allocs []RefundPaymentAllocationInput) []uuid.UUID {
	ids := make([]uuid.UUID, len(allocs))
	for i, allocation := range allocs {
		ids[i] = allocation.PaymentID
	}
	return ids
}

func refundAdjustmentIDs(allocs []RefundAdjustmentAllocationInput) []uuid.UUID {
	ids := make([]uuid.UUID, len(allocs))
	for i, allocation := range allocs {
		ids[i] = allocation.ChargeAdjustmentID
	}
	return ids
}

// loadCheckPendingRefundVND derives money the Check still owes back. The
// stored charge is verified against its allocations and adjustments first, so
// a drifted Check refuses the command rather than building on the drift; the
// receipt side is then valid Payments less completed live Refunds. Pending
// Manual QR Refunds have not moved money and do not reduce the obligation.
func loadCheckPendingRefundVND(ctx context.Context, q *sqlc.Queries, checkID uuid.UUID,
	storedChargeVND int64,
) (int64, error) {
	if err := assertChargeMatchesAllocations(ctx, q, checkID, storedChargeVND); err != nil {
		return 0, err
	}

	paymentRows, err := q.ListCheckPayments(ctx, checkID)
	if err != nil {
		return 0, fmt.Errorf("load check payments: %w", err)
	}
	originalVND, voidedVND, err := sumPaymentAmounts(paymentRows)
	if err != nil {
		return 0, fmt.Errorf("check %s: %w", checkID, err)
	}
	refundRows, err := q.ListCheckRefunds(ctx, checkID)
	if err != nil {
		return 0, fmt.Errorf("load check refunds: %w", err)
	}
	completedRefundVND, err := sumCompletedRefundsVND(refundRows)
	if err != nil {
		return 0, fmt.Errorf("check %s: %w", checkID, err)
	}

	// The assertion above proved stored charge = base allocations - live
	// adjustments, so the charge term is the stored value.
	financials, err := ComputeCheckFinancials(CheckFinancialInputs{
		BaseChargeVND:      storedChargeVND,
		LiveAdjustmentVND:  0,
		OriginalPaymentVND: originalVND,
		VoidedPaymentVND:   voidedVND,
		CompletedRefundVND: completedRefundVND,
	})
	if err != nil {
		return 0, err
	}
	return financials.PendingRefundVND, nil
}

// loadPendingRefundHeadroom is the cumulative pending-obligation bound: the
// money the live Check or Completed Sale still owes back, less every existing
// PENDING Refund whose amount is already promised. It returns the available
// amount and a human-readable target for the refusal message.
func loadPendingRefundHeadroom(ctx context.Context, q *sqlc.Queries, checkID, saleID uuid.UUID,
	isPostSale bool, storedChargeVND int64,
) (int64, string, error) {
	if isPostSale {
		outstandingVND, err := loadOutstandingPostSaleRefundVND(ctx, q, saleID)
		if err != nil {
			return 0, "", err
		}
		reservedVND, err := loadPendingPostSaleRefundVND(ctx, q, saleID)
		if err != nil {
			return 0, "", err
		}
		target := fmt.Sprintf("sale %s", saleID)
		availableVND, err := subtractPendingReservation(outstandingVND, reservedVND, target)
		return availableVND, target, err
	}

	pendingVND, err := loadCheckPendingRefundVND(ctx, q, checkID, storedChargeVND)
	if err != nil {
		return 0, "", err
	}
	reservedVND, err := loadPendingLiveRefundVND(ctx, q, checkID)
	if err != nil {
		return 0, "", err
	}
	target := fmt.Sprintf("check %s", checkID)
	availableVND, err := subtractPendingReservation(pendingVND, reservedVND, target)
	return availableVND, target, err
}

// subtractPendingReservation derives the available obligation. A negative
// result means already-recorded pending intents promise more than the source
// owes, which no client request can cause: it is a stored invariant failure,
// not a conflict.
func subtractPendingReservation(obligationVND, reservedVND int64, target string) (int64, error) {
	availableVND, err := SubtractCharge(obligationVND, reservedVND)
	if err != nil {
		return 0, fmt.Errorf("%w: %s pending refund is below its reservations: %v",
			ErrFinancialInvariantViolated, target, err)
	}
	return availableVND, nil
}

// loadPendingLiveRefundVND sums every PENDING live Refund of one Check: money
// already promised back that has not moved. The query excludes post-sale
// Refunds structurally and exposes completion evidence, so a completed Refund
// — already subtracted by the corrected financials — is never counted twice.
func loadPendingLiveRefundVND(ctx context.Context, q *sqlc.Queries, checkID uuid.UUID) (int64, error) {
	rows, err := q.ListCheckRefunds(ctx, checkID)
	if err != nil {
		return 0, fmt.Errorf("load check refunds: %w", err)
	}
	var reservedVND int64
	for _, row := range rows {
		if row.CompletionID.Valid {
			continue
		}
		next, err := AddCharge(reservedVND, row.AmountVnd)
		if err != nil {
			return 0, fmt.Errorf("%w: pending live refunds: %v",
				ErrFinancialInvariantViolated, err)
		}
		reservedVND = next
	}
	return reservedVND, nil
}

// loadPendingPostSaleRefundVND sums every PENDING post-sale Refund of one
// Completed Sale from the same additive history the post-sale result projects.
func loadPendingPostSaleRefundVND(ctx context.Context, q *sqlc.Queries,
	saleID uuid.UUID,
) (int64, error) {
	rows, err := q.ListCompletedSalePostSaleCorrections(ctx, saleID)
	if err != nil {
		return 0, fmt.Errorf("load post-sale corrections: %w", err)
	}
	var reservedVND int64
	for _, row := range rows {
		if !row.EntryKind.Valid || row.EntryKind.String != postSaleEntryKindRefund ||
			row.CompletedAt.Valid {
			continue
		}
		if !row.AmountVnd.Valid || row.AmountVnd.Int64 < 0 {
			return 0, fmt.Errorf("%w: pending post-sale refund amount %v is not a sum",
				ErrFinancialInvariantViolated, row.AmountVnd)
		}
		next, err := AddCharge(reservedVND, row.AmountVnd.Int64)
		if err != nil {
			return 0, fmt.Errorf("%w: pending post-sale refunds: %v",
				ErrFinancialInvariantViolated, err)
		}
		reservedVND = next
	}
	return reservedVND, nil
}

// refundResponseFromFact assembles the Refund result from the stored fact, its
// allocation rows, and its nullable completion evidence.
func refundResponseFromFact(refund sqlc.Refund,
	paymentRows []sqlc.ListRefundPaymentAllocationsRow,
	adjustmentRows []sqlc.ListRefundAdjustmentAllocationsRow,
	completion *sqlc.RefundCompletion,
) RefundResponse {
	out := RefundResponse{
		ID:                        refund.ID,
		CheckID:                   refund.CheckID,
		CompletedSaleID:           nullUUIDPtr(refund.CompletedSaleID),
		SalesShiftID:              refund.SalesShiftID,
		Method:                    refund.Method,
		AmountVND:                 refund.AmountVnd,
		State:                     RefundStatePending,
		Reason:                    refund.Reason,
		Note:                      nullStringPtr(refund.Note),
		ActorStaffIdentityID:      refund.ActorStaffIdentityID,
		ApprovedByStaffIdentityID: refund.ApprovedByStaffIdentityID,
		CreatedAt:                 refund.CreatedAt,
		PaymentAllocations:        make([]RefundAllocationResponse, 0, len(paymentRows)),
		AdjustmentAllocations:     make([]RefundAllocationResponse, 0, len(adjustmentRows)),
	}
	for _, allocation := range paymentRows {
		out.PaymentAllocations = append(out.PaymentAllocations,
			RefundAllocationResponse{ID: allocation.PaymentID, AmountVND: allocation.AmountVnd})
	}
	for _, allocation := range adjustmentRows {
		out.AdjustmentAllocations = append(out.AdjustmentAllocations,
			RefundAllocationResponse{
				ID:        allocation.ChargeAdjustmentID,
				AmountVND: allocation.AmountVnd,
			})
	}
	if completion != nil {
		out.State = RefundStateCompleted
		out.Completion = &RefundCompletionResponse{
			ID:                            completion.ID,
			TransactionReference:          nullStringPtr(completion.TransactionReference),
			CompletedByStaffIdentityID:    completion.CompletedByStaffIdentityID,
			CompletedStaffAccessSessionID: completion.StaffAccessSessionID,
			CompletedAt:                   completion.CompletedAt,
		}
	}
	return out
}

// loadPostSaleCorrectionHistory projects the Completed Sale's additive
// correction history for the post-sale Refund result. Every POST_SALE Comp
// correction carries its adjustment, its Comp fact, the Refund that this
// mutation recorded against it (if any), the correction amount whose capacity
// is still unallocated, and the amount whose money has not actually moved: a
// pending Manual QR intent reserves capacity but leaves the obligation
// outstanding until its completion exists.
//
// The correction-list query deliberately projects a union row shape, so two
// Charge Adjustment columns it does not select — charge_allocation_id and
// sales_shift_id — stay zero here; the Completed Sale read enriched in Task 11
// fills them from its own dedicated loaders.
func loadPostSaleCorrectionHistory(ctx context.Context, q *sqlc.Queries, saleID uuid.UUID,
	newRefund *RefundResponse,
) ([]PostSaleCorrectionResponse, error) {
	rows, err := q.ListCompletedSalePostSaleCorrections(ctx, saleID)
	if err != nil {
		return nil, fmt.Errorf("load post-sale corrections: %w", err)
	}

	adjustmentIDs := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		if row.EntryKind.Valid && row.EntryKind.String == postSaleEntryKindComp &&
			row.ChargeAdjustmentID.Valid {
			adjustmentIDs = append(adjustmentIDs, row.ChargeAdjustmentID.UUID)
		}
	}
	allocations, err := q.ListAdjustmentRefundAllocations(ctx, adjustmentIDs)
	if err != nil {
		return nil, fmt.Errorf("load post-sale refund allocations: %w", err)
	}
	allocated := make(map[uuid.UUID]int64, len(adjustmentIDs))
	completed := make(map[uuid.UUID]int64, len(adjustmentIDs))
	for _, allocation := range allocations {
		next, err := AddCharge(allocated[allocation.ChargeAdjustmentID], allocation.AmountVnd)
		if err != nil {
			return nil, fmt.Errorf("%w: post-sale refund allocations: %v",
				ErrFinancialInvariantViolated, err)
		}
		allocated[allocation.ChargeAdjustmentID] = next
		if !allocation.RefundCompleted {
			continue
		}
		next, err = AddCharge(completed[allocation.ChargeAdjustmentID], allocation.AmountVnd)
		if err != nil {
			return nil, fmt.Errorf("%w: completed post-sale refunds: %v",
				ErrFinancialInvariantViolated, err)
		}
		completed[allocation.ChargeAdjustmentID] = next
	}

	out := make([]PostSaleCorrectionResponse, 0, len(adjustmentIDs))
	for _, row := range rows {
		if !row.EntryKind.Valid || row.EntryKind.String != postSaleEntryKindComp ||
			!row.ChargeAdjustmentID.Valid || !row.AmountVnd.Valid {
			continue
		}
		adjustmentID := row.ChargeAdjustmentID.UUID
		remainingVND := row.AmountVnd.Int64 - allocated[adjustmentID]
		outstandingVND := row.AmountVnd.Int64 - completed[adjustmentID]
		if remainingVND < 0 || outstandingVND < 0 {
			return nil, fmt.Errorf(
				"%w: post-sale adjustment %s amount %d is below its refund allocations",
				ErrFinancialInvariantViolated, adjustmentID, row.AmountVnd.Int64)
		}

		adjustment := ChargeAdjustmentResponse{
			ID:                     adjustmentID,
			Kind:                   ChargeAdjustmentKindComp,
			Scope:                  CompScopePostSale,
			PreparationUnitID:      row.PreparationUnitID.UUID,
			ChargeAllocationID:     uuid.Nil,
			CompletedSaleID:        &saleID,
			SalesShiftID:           uuid.Nil,
			AmountVND:              row.AmountVnd.Int64,
			RemainingRefundableVND: remainingVND,
			CreatedAt:              row.OccurredAt.Time,
		}
		if row.PreparationWasteID.Valid {
			wasteID := row.PreparationWasteID.UUID
			adjustment.PreparationWasteID = &wasteID
		}

		entry := PostSaleCorrectionResponse{
			Adjustment: adjustment,
			Comp: CompResponse{
				ID:                        row.SalesCompID.UUID,
				WasteID:                   row.PreparationWasteID.UUID,
				PreparationUnitID:         row.PreparationUnitID.UUID,
				ChargeAdjustmentID:        adjustmentID,
				AmountVND:                 row.AmountVnd.Int64,
				Reason:                    row.Reason.String,
				Note:                      nullStringPtr(row.Note),
				ActorStaffIdentityID:      row.ActorStaffIdentityID.UUID,
				ApprovedByStaffIdentityID: row.ApprovedByStaffIdentityID.UUID,
				OccurredAt:                row.OccurredAt.Time,
			},
			Refunds:              make([]RefundResponse, 0),
			OutstandingRefundVND: outstandingVND,
		}
		if newRefund != nil && refundCoversAdjustment(*newRefund, adjustmentID) {
			entry.Refunds = append(entry.Refunds, *newRefund)
		}
		out = append(out, entry)
	}
	return out, nil
}

// refundCoversAdjustment reports whether the Refund allocated against the
// Charge Adjustment, so the history entry lists exactly the Refunds this
// mutation can prove touched it.
func refundCoversAdjustment(refund RefundResponse, adjustmentID uuid.UUID) bool {
	for _, allocation := range refund.AdjustmentAllocations {
		if allocation.ID == adjustmentID {
			return true
		}
	}
	return false
}

// audit detail shapes. Each carries stable business ids and financial meaning,
// and never a Manager PIN, PIN hash, or login credential (spec §15).

type refundPaymentAllocationAudit struct {
	PaymentID uuid.UUID `json:"payment_id"`
	AmountVND int64     `json:"amount_vnd"`
}

type refundAdjustmentAllocationAudit struct {
	ChargeAdjustmentID uuid.UUID `json:"charge_adjustment_id"`
	AmountVND          int64     `json:"amount_vnd"`
}

type refundRecordedAudit struct {
	RefundID                  uuid.UUID                         `json:"refund_id"`
	CheckID                   uuid.UUID                         `json:"check_id"`
	CompletedSaleID           *uuid.UUID                        `json:"completed_sale_id,omitempty"`
	SalesShiftID              uuid.UUID                         `json:"sales_shift_id"`
	Method                    string                            `json:"method"`
	AmountVND                 int64                             `json:"amount_vnd"`
	PaymentAllocations        []refundPaymentAllocationAudit    `json:"payment_allocations"`
	AdjustmentAllocations     []refundAdjustmentAllocationAudit `json:"adjustment_allocations"`
	Reason                    string                            `json:"reason"`
	Note                      *string                           `json:"note,omitempty"`
	ActorStaffIdentityID      uuid.UUID                         `json:"actor_staff_identity_id"`
	ApprovedByStaffIdentityID uuid.UUID                         `json:"approved_by_staff_identity_id"`
}

type refundCompletedAudit struct {
	RefundID                      uuid.UUID  `json:"refund_id"`
	CheckID                       uuid.UUID  `json:"check_id"`
	CompletedSaleID               *uuid.UUID `json:"completed_sale_id,omitempty"`
	SalesShiftID                  uuid.UUID  `json:"sales_shift_id"`
	Method                        string     `json:"method"`
	AmountVND                     int64      `json:"amount_vnd"`
	CompletedByStaffIdentityID    uuid.UUID  `json:"completed_by_staff_identity_id"`
	CompletedStaffAccessSessionID uuid.UUID  `json:"completed_staff_access_session_id"`
	CompletedAt                   time.Time  `json:"completed_at"`
}

func refundPaymentAllocationAudits(allocs []RefundPaymentAllocationInput) []refundPaymentAllocationAudit {
	out := make([]refundPaymentAllocationAudit, 0, len(allocs))
	for _, allocation := range allocs {
		out = append(out, refundPaymentAllocationAudit{
			PaymentID: allocation.PaymentID,
			AmountVND: allocation.AmountVND,
		})
	}
	return out
}

func refundAdjustmentAllocationAudits(allocs []RefundAdjustmentAllocationInput) []refundAdjustmentAllocationAudit {
	out := make([]refundAdjustmentAllocationAudit, 0, len(allocs))
	for _, allocation := range allocs {
		out = append(out, refundAdjustmentAllocationAudit{
			ChargeAdjustmentID: allocation.ChargeAdjustmentID,
			AmountVND:          allocation.AmountVND,
		})
	}
	return out
}

// writeRefundAudit inserts one Refund business event inside the mutation
// transaction, on an explicit occurrence instant.
func writeRefundAudit(ctx context.Context, q *sqlc.Queries, actor Actor,
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
