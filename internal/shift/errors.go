package shift

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrShiftAlreadyOpen    = errors.New("a sales shift is already open")
	ErrOpenShiftRequired   = errors.New("an open sales shift is required")
	ErrShiftAlreadyClosing = errors.New("a sales shift is already closing")
	// ErrSalesShiftNotFound covers a named Shift that does not exist at all,
	// which the start route maps to 404 (spec 12: unknown Shift or attempt id).
	ErrSalesShiftNotFound = errors.New("sales shift not found")
	// ErrReconciliationNotStarted is defined now for the attempt and close
	// routes (Task 5) whose target Shift carries no reconciliation snapshot.
	ErrReconciliationNotStarted   = errors.New("shift reconciliation not started")
	ErrUnsettledCheck             = errors.New("the shift has unsettled checks")
	ErrPendingRefund              = errors.New("the shift has pending refunds")
	ErrUnresolvedCorrection       = errors.New("the shift has unresolved corrections")
	ErrActiveServiceSession       = errors.New("the shift has active service sessions")
	ErrManagerApprovalUnavailable = errors.New("manager approval unavailable")
	ErrRequestConflict            = errors.New("request conflict")
	ErrExpectedCashOutOfRange     = errors.New("expected cash out of range")
	ErrForbidden                  = errors.New("forbidden")
	ErrUnauthorized               = errors.New("unauthorized")
	ErrInvalidStoredResult        = errors.New("invalid stored result")
)

// errReconciliationCalculationFailed is private by design (spec 12): before
// the initial count commits, an Expected Cash range failure and every other
// calculation failure surface as the generic SHIFT_RECONCILIATION_CALCULATION_FAILED
// code exposing no values or operands. The mutation wraps the computation
// error in this sentinel, so the client sees only the stable message while the
// cause stays in the server-side error chain and log.
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
// name (spec 12): any other unique violation is a defect, not a business
// state, and passes through unmapped.
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

// MapHTTPError maps Shift domain errors and input validation errors to
// *response.CodedError.
func MapHTTPError(err error) error {
	if err == nil {
		return nil
	}
	var codedErr *response.CodedError
	if errors.As(err, &codedErr) {
		return err
	}
	switch {
	// The calculation-failed case precedes the range case deliberately: the
	// start mutation wraps every computation failure in the private sentinel,
	// and the client message must stay generic (spec 12).
	case errors.Is(err, errReconciliationCalculationFailed):
		return response.NewCodedError(http.StatusInternalServerError,
			"SHIFT_RECONCILIATION_CALCULATION_FAILED",
			"shift reconciliation calculation failed", err)
	case errors.Is(err, ErrShiftAlreadyOpen):
		// The client message is the stable sentinel text, never the wrapped
		// PostgreSQL detail, which must stay server-side only.
		return response.NewCodedError(http.StatusConflict, "SALES_SHIFT_ALREADY_OPEN", ErrShiftAlreadyOpen.Error(), err)
	case errors.Is(err, ErrOpenShiftRequired):
		return response.NewCodedError(http.StatusConflict, "OPEN_SALES_SHIFT_REQUIRED", ErrOpenShiftRequired.Error(), err)
	case errors.Is(err, ErrShiftAlreadyClosing):
		return response.NewCodedError(http.StatusConflict, "SALES_SHIFT_ALREADY_CLOSING", ErrShiftAlreadyClosing.Error(), err)
	case errors.Is(err, ErrSalesShiftNotFound):
		return response.NewCodedError(http.StatusNotFound, "SALES_SHIFT_NOT_FOUND", ErrSalesShiftNotFound.Error(), err)
	case errors.Is(err, ErrReconciliationNotStarted):
		return response.NewCodedError(http.StatusConflict, "SHIFT_RECONCILIATION_NOT_STARTED", ErrReconciliationNotStarted.Error(), err)
	case errors.Is(err, ErrUnsettledCheck):
		return response.NewCodedError(http.StatusConflict, "SHIFT_UNSETTLED_CHECK", ErrUnsettledCheck.Error(), err)
	case errors.Is(err, ErrPendingRefund):
		return response.NewCodedError(http.StatusConflict, "SHIFT_PENDING_REFUND", ErrPendingRefund.Error(), err)
	case errors.Is(err, ErrUnresolvedCorrection):
		return response.NewCodedError(http.StatusConflict, "SHIFT_UNRESOLVED_CORRECTION", ErrUnresolvedCorrection.Error(), err)
	case errors.Is(err, ErrActiveServiceSession):
		return response.NewCodedError(http.StatusConflict, "SHIFT_ACTIVE_SERVICE_SESSION", ErrActiveServiceSession.Error(), err)
	case errors.Is(err, ErrManagerApprovalUnavailable):
		// Every denial reason collapses here so the API never discloses which
		// condition failed. The reason is in the server log and audit event.
		return response.NewCodedError(http.StatusForbidden, "MANAGER_APPROVAL_UNAVAILABLE",
			"manager approval could not be confirmed with this login code and PIN", err)
	case errors.Is(err, ErrRequestConflict):
		return response.NewCodedError(http.StatusConflict, "REQUEST_CONFLICT", err.Error(), err)
	case errors.Is(err, ErrExpectedCashOutOfRange):
		return response.NewCodedError(http.StatusBadRequest, "EXPECTED_CASH_OUT_OF_RANGE", err.Error(), err)
	case errors.Is(err, ErrForbidden):
		return response.NewCodedError(http.StatusForbidden, "FORBIDDEN", err.Error(), err)
	case errors.Is(err, ErrUnauthorized):
		return response.NewCodedError(http.StatusUnauthorized, "UNAUTHORIZED", err.Error(), err)
	case errors.Is(err, ErrInvalidStoredResult):
		return response.NewCodedError(http.StatusInternalServerError, "INVALID_STORED_RESULT",
			"an unexpected error occurred", err)
	case errors.Is(err, response.ErrInvalid):
		return response.NewCodedError(http.StatusBadRequest, "INVALID_INPUT", err.Error(), err)
	default:
		return err
	}
}
