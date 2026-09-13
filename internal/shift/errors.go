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
	ErrShiftAlreadyOpen           = errors.New("a sales shift is already open")
	ErrOpenShiftRequired          = errors.New("an open sales shift is required")
	ErrManagerApprovalUnavailable = errors.New("manager approval unavailable")
	ErrRequestConflict            = errors.New("request conflict")
	ErrExpectedCashOutOfRange     = errors.New("expected cash out of range")
	ErrForbidden                  = errors.New("forbidden")
	ErrUnauthorized               = errors.New("unauthorized")
	ErrInvalidStoredResult        = errors.New("invalid stored result")
)

// cashMovementSalesShiftFK is the auto-generated name of the only foreign key
// whose violation means "no open Shift to attach to". InsertCashMovement has
// three other FK columns (the initiator, its session, and the approver) whose
// violation is a defect, not this business state.
const cashMovementSalesShiftFK = "cash_movements_sales_shift_id_fkey"

// MapDBError maps PostgreSQL driver and database errors to domain sentinels
// inside the Shift boundary, so an expected constraint failure never becomes an
// accidental generic 500.
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
		case "23505": // unique_violation on sales_shift_only_one_open_unique
			return fmt.Errorf("%w: %s", ErrShiftAlreadyOpen, msg)
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
	case errors.Is(err, ErrShiftAlreadyOpen):
		// The client message is the stable sentinel text, never the wrapped
		// PostgreSQL detail, which must stay server-side only.
		return response.NewCodedError(http.StatusConflict, "SALES_SHIFT_ALREADY_OPEN", ErrShiftAlreadyOpen.Error(), err)
	case errors.Is(err, ErrOpenShiftRequired):
		return response.NewCodedError(http.StatusConflict, "OPEN_SALES_SHIFT_REQUIRED", ErrOpenShiftRequired.Error(), err)
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
