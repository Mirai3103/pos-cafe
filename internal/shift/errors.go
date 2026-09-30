package shift

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/Mirai3103/pos-cafe/internal/platform/command"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrShiftAlreadyOpen    = errors.New("a sales shift is already open")
	ErrOpenShiftRequired   = errors.New("an open sales shift is required")
	ErrShiftAlreadyClosing = errors.New("a sales shift is already closing")
	// ErrShiftAlreadyClosed marks a mutation whose target Shift has finished
	// its lifecycle, a 409 lifecycle conflict. The attempt routes reach it
	// when an append races a committed close; Final Close reports it to the
	// loser of two concurrent closes.
	ErrShiftAlreadyClosed = errors.New("a sales shift is already closed")
	// ErrSalesShiftNotFound covers a named Shift that does not exist at all,
	// which the routes map to 404.
	ErrSalesShiftNotFound = errors.New("sales shift not found")
	// ErrReconciliationNotStarted marks an attempt or close whose target Shift
	// carries no reconciliation snapshot.
	ErrReconciliationNotStarted = errors.New("shift reconciliation not started")
	ErrUnsettledCheck           = errors.New("the shift has unsettled checks")
	ErrPendingRefund            = errors.New("the shift has pending refunds")
	ErrUnresolvedCorrection     = errors.New("the shift has unresolved corrections")
	ErrActiveServiceSession     = errors.New("the shift has active service sessions")
	ErrAwaitingSubmission       = errors.New("the shift has service sessions awaiting submission")
	// ErrReconciliationStale marks a close whose submitted final attempt ids
	// are no longer the latest rows of their ledgers: a later attempt landed
	// and the client must refresh and resubmit.
	ErrReconciliationStale = errors.New("shift reconciliation evidence is stale")
	// ErrReconciliationSourceChanged marks a close whose reloaded live source
	// totals disagree with the frozen snapshot — an uncoordinated writer or
	// corrupt state.
	ErrReconciliationSourceChanged = errors.New("shift reconciliation source totals changed")
	// ErrCashRecountRequired marks a nonzero Cash difference whose final Cash
	// Count is still the blind sequence 1.
	ErrCashRecountRequired = errors.New("a cash recount is required")
	// ErrQRRecheckRequired marks a nonzero Manual QR difference whose final
	// QR Observation is still sequence 1.
	ErrQRRecheckRequired = errors.New("a manual QR observation recheck is required")
	// ErrDiscrepancyReasonRequired marks a nonzero closure dimension without
	// exactly one reason entry.
	ErrDiscrepancyReasonRequired = errors.New("a discrepancy reason is required")
	// ErrDiscrepancyReasonUnexpected marks a reason entry a close cannot use:
	// an entry on a dimension whose server-derived difference is zero, or a
	// reason-to-dimension pairing the catalog forbids.
	ErrDiscrepancyReasonUnexpected = errors.New("a discrepancy reason is unexpected")
	// ErrReconciliationAttemptNotFound marks a submitted final attempt id that
	// does not exist in the target reconciliation's ledger; it maps to 404.
	ErrReconciliationAttemptNotFound = errors.New("shift reconciliation attempt not found")
	ErrManagerApprovalUnavailable    = errors.New("manager approval unavailable")
	// ErrExpectedCashOutOfRange marks guarded-arithmetic overflow in the
	// Expected Cash and difference equations. It has no direct HTTP mapping:
	// every pre-reveal producer is wrapped in the private calculation-failed
	// sentinel, and a post-reveal escape is corrupt state that surfaces as the
	// generic 500. Mapping it to a client-facing status would leak the wrapped
	// operands in the message.
	ErrExpectedCashOutOfRange = errors.New("expected cash out of range")

	ErrRequestConflict     = command.ErrRequestConflict
	ErrForbidden           = command.ErrForbidden
	ErrUnauthorized        = command.ErrUnauthorized
	ErrInvalidStoredResult = command.ErrInvalidStoredResult
)

// errReconciliationCalculationFailed is private by design: before the initial
// count commits, an Expected Cash range failure and every other calculation
// failure surface as the generic SHIFT_RECONCILIATION_CALCULATION_FAILED code
// exposing no values or operands. The mutation wraps the computation error in
// this sentinel, so the client sees only the stable message while the cause
// stays in the server-side error chain and log.
var errReconciliationCalculationFailed = errors.New("shift reconciliation calculation failed")

// salesShiftOnlyOneActiveUnique is the sole authority for the one-active-Shift
// invariant: a constant-key partial unique index over every OPEN or CLOSING
// row. It replaced the OPEN-only sales_shift_only_one_open_unique index in
// migration 000015, which also introduced CLOSING.
const salesShiftOnlyOneActiveUnique = "sales_shift_only_one_active_unique"

// shiftReconciliationSalesShiftUnique guards the one-immutable-snapshot-per-
// Shift invariant on shift_reconciliations. Start detects a double start from
// the Shift's CLOSING state under lock; this mapping is the defensive backstop
// for any path that could reach the insert without that lock.
const shiftReconciliationSalesShiftUnique = "shift_reconciliation_sales_shift_unique"

// cashMovementSalesShiftFK is the auto-generated name of the only foreign key
// whose violation means "no open Shift to attach to". InsertCashMovement has
// three other FK columns (the initiator, its session, and the approver) whose
// violation is a defect, not this business state.
const cashMovementSalesShiftFK = "cash_movements_sales_shift_id_fkey"

// MapDBError maps PostgreSQL driver and database errors to domain sentinels
// inside the Shift boundary, so an expected constraint failure never becomes an
// accidental generic 500. Unique violations are narrowed by known constraint
// name: any other unique violation is a defect, not a business state, and
// passes through unmapped.
func MapDBError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %s", ErrOpenShiftRequired, err.Error())
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		msg := pgErr.Detail
		if msg == "" {
			msg = pgErr.Message
		}
		switch pgErr.Code {
		case "23505": // unique_violation, by constraint name
			switch pgErr.ConstraintName {
			case salesShiftOnlyOneActiveUnique:
				return fmt.Errorf("%w: %s", ErrShiftAlreadyOpen, msg)
			case shiftReconciliationSalesShiftUnique:
				return fmt.Errorf("%w: %s", ErrShiftAlreadyClosing, msg)
			}
		case "23503": // foreign_key_violation
			if pgErr.ConstraintName == cashMovementSalesShiftFK {
				return fmt.Errorf("%w: %s", ErrOpenShiftRequired, msg)
			}
			// Any other FK (initiator, initiator session, approver) failing
			// means Go's own authorization/approval checks disagree with the
			// database, which is a defect, not this business state.
		}
		// 23514 (check_violation) is deliberately not mapped. It means Go
		// validation and the database disagree, which is a defect, not a
		// business state, and must surface as a logged 500.
	}
	return err
}

// sentinelRule maps target to status and code with the sentinel's own fixed
// text as the client message, never the wrapped detail: MapDBError embeds
// PostgreSQL constraint values in the chain for server-side logging only.
func sentinelRule(target error, status int, code string) response.ErrorRule {
	return response.ErrorRule{Target: target, Spec: response.ErrorSpec{
		Status: status, Code: code, Message: target.Error(),
	}}
}

// httpErrors is the Shift HTTP error mapping. Rules are tried in order.
var httpErrors = response.ErrorMapper{
	// The calculation-failed rule precedes every other one deliberately: the
	// start mutation wraps each computation failure, including an Expected
	// Cash range failure, in the private sentinel, and the client message must
	// stay generic.
	sentinelRule(errReconciliationCalculationFailed, http.StatusInternalServerError,
		"SHIFT_RECONCILIATION_CALCULATION_FAILED"),
	sentinelRule(ErrShiftAlreadyOpen, http.StatusConflict, "SALES_SHIFT_ALREADY_OPEN"),
	sentinelRule(ErrOpenShiftRequired, http.StatusConflict, "OPEN_SALES_SHIFT_REQUIRED"),
	sentinelRule(ErrShiftAlreadyClosing, http.StatusConflict, "SALES_SHIFT_ALREADY_CLOSING"),
	sentinelRule(ErrShiftAlreadyClosed, http.StatusConflict, "SALES_SHIFT_ALREADY_CLOSED"),
	sentinelRule(ErrSalesShiftNotFound, http.StatusNotFound, "SALES_SHIFT_NOT_FOUND"),
	sentinelRule(ErrReconciliationNotStarted, http.StatusConflict, "SHIFT_RECONCILIATION_NOT_STARTED"),
	sentinelRule(ErrUnsettledCheck, http.StatusConflict, "SHIFT_UNSETTLED_CHECK"),
	sentinelRule(ErrPendingRefund, http.StatusConflict, "SHIFT_PENDING_REFUND"),
	sentinelRule(ErrUnresolvedCorrection, http.StatusConflict, "SHIFT_UNRESOLVED_CORRECTION"),
	sentinelRule(ErrAwaitingSubmission, http.StatusConflict, "SHIFT_AWAITING_SUBMISSION"),
	sentinelRule(ErrActiveServiceSession, http.StatusConflict, "SHIFT_ACTIVE_SERVICE_SESSION"),
	sentinelRule(ErrReconciliationStale, http.StatusConflict, "SHIFT_RECONCILIATION_STALE"),
	sentinelRule(ErrReconciliationSourceChanged, http.StatusConflict, "SHIFT_RECONCILIATION_SOURCE_CHANGED"),
	sentinelRule(ErrCashRecountRequired, http.StatusConflict, "SHIFT_CASH_RECOUNT_REQUIRED"),
	sentinelRule(ErrQRRecheckRequired, http.StatusConflict, "SHIFT_QR_RECHECK_REQUIRED"),
	sentinelRule(ErrDiscrepancyReasonRequired, http.StatusConflict, "SHIFT_DISCREPANCY_REASON_REQUIRED"),
	sentinelRule(ErrDiscrepancyReasonUnexpected, http.StatusConflict, "SHIFT_DISCREPANCY_REASON_UNEXPECTED"),
	sentinelRule(ErrReconciliationAttemptNotFound, http.StatusNotFound, "SHIFT_RECONCILIATION_ATTEMPT_NOT_FOUND"),
	// Every approval denial reason collapses here so the API never discloses
	// which condition failed. The reason is in the server log and audit event.
	{Target: ErrManagerApprovalUnavailable, Spec: response.ErrorSpec{
		Status: http.StatusForbidden, Code: "MANAGER_APPROVAL_UNAVAILABLE",
		Message: "manager approval could not be confirmed with this login code and PIN",
	}},
	{Target: ErrRequestConflict, Spec: response.ErrorSpec{Status: http.StatusConflict, Code: "REQUEST_CONFLICT"}},
	{Target: ErrForbidden, Spec: response.ErrorSpec{Status: http.StatusForbidden, Code: "FORBIDDEN"}},
	{Target: ErrUnauthorized, Spec: response.ErrorSpec{Status: http.StatusUnauthorized, Code: "UNAUTHORIZED"}},
	{Target: ErrInvalidStoredResult, Spec: response.ErrorSpec{
		Status: http.StatusInternalServerError, Code: "INVALID_STORED_RESULT", Message: "an unexpected error occurred",
	}},
	{Target: response.ErrInvalid, Spec: response.ErrorSpec{Status: http.StatusBadRequest, Code: "INVALID_INPUT"}},
}

// MapHTTPError maps Shift domain errors and input validation errors to
// *response.CodedError.
func MapHTTPError(err error) error {
	return httpErrors.Map(err)
}
