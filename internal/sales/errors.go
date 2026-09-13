package sales

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrOpenShiftRequired      = errors.New("an open sales shift is required")
	ErrServiceSessionNotFound = errors.New("service session not found")
	ErrServiceSessionClosed   = errors.New("service session is already closed")
	ErrEditableDraftNotFound  = errors.New("no editable order draft for this service session")
	ErrDraftItemNotFound      = errors.New("draft item not found")

	ErrMenuItemNotFound    = errors.New("menu item not found")
	ErrMenuItemUnavailable = errors.New("menu item is unavailable")
	ErrMenuItemRetired     = errors.New("menu item is retired")

	ErrSizeNotFound    = errors.New("size not found for this menu item")
	ErrSizeUnavailable = errors.New("size is unavailable")
	ErrSizeRetired     = errors.New("size is retired")

	ErrModifierOptionNotFound    = errors.New("modifier option not found for this menu item")
	ErrModifierOptionUnavailable = errors.New("modifier option is unavailable")
	ErrModifierOptionRetired     = errors.New("modifier option is retired")

	ErrInvalidPreparationNote = errors.New("invalid preparation note")
	ErrInvalidQuantity        = errors.New("invalid quantity")

	ErrLineTotalOutOfRange   = errors.New("line total out of range")
	ErrCheckChargeOutOfRange = errors.New("check charge out of range")
	ErrInvalidCheckTarget    = errors.New("invalid check target")

	ErrTableSelectionRequired     = errors.New("at least one table is required for a dine-in session")
	ErrTableSelectionDuplicate    = errors.New("the same table was selected twice")
	ErrTableNotFound              = errors.New("table not found")
	ErrTableUnavailable           = errors.New("table is unavailable")
	ErrTakeawayTablesNotAvailable = errors.New("a takeaway session cannot be assigned tables")

	ErrRequestConflict     = errors.New("request conflict")
	ErrInvalidStoredResult = errors.New("invalid stored result")
	ErrForbidden           = errors.New("forbidden")
	ErrUnauthorized        = errors.New("unauthorized")

	// ErrServiceSequenceExhausted cannot occur in normal operation: a Shift
	// would need 99,999 Service Sessions. It exists so FormatServiceNumber has
	// a typed failure instead of emitting a seventh character that the
	// database check would reject as an unmapped 23514.
	ErrServiceSequenceExhausted = errors.New("service number sequence exhausted for this shift")
)

// serviceSessionSalesShiftFK is the auto-generated name of the only foreign
// key whose violation means "no open Shift to attach to". service_sessions has
// another FK (the creating identity) whose violation is a defect, not this
// business state.
const serviceSessionSalesShiftFK = "service_sessions_sales_shift_id_fkey"

// MapDBError maps PostgreSQL driver and database errors to domain sentinels
// inside the Sales boundary, so an expected constraint failure never becomes
// an accidental generic 500.
func MapDBError(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		msg := pgErr.Detail
		if msg == "" {
			msg = pgErr.Message
		}
		if pgErr.Code == "23503" && pgErr.ConstraintName == serviceSessionSalesShiftFK {
			return fmt.Errorf("%w: %s", ErrOpenShiftRequired, msg)
		}
		// Every other code is deliberately unmapped:
		//
		//   23505 on the composition index is absorbed by the add path's
		//   upsert and never reaches here.
		//   23505 on service_session_number_per_shift_unique means the
		//   advisory-locked allocation path has a defect.
		//   23514 means Go validation and the database disagree.
		//
		// All three are defects, not business states, and must surface as
		// logged 500s.
	}
	return err
}

// MapHTTPError maps Sales domain errors and input validation errors to
// *response.CodedError.
//
// Note that sql.ErrNoRows is not mapped here. Unlike Shift, where a missing
// row has exactly one meaning, Sales reads several different rows and each
// caller decides which sentinel a miss means. A bare sql.ErrNoRows reaching
// this function is a defect in the caller.
func MapHTTPError(err error) error {
	if err == nil {
		return nil
	}
	var codedErr *response.CodedError
	if errors.As(err, &codedErr) {
		return err
	}

	// The client message is always the stable sentinel text, never the wrapped
	// PostgreSQL detail, which must stay server-side only.
	coded := func(status int, code string, sentinel error) error {
		return response.NewCodedError(status, code, sentinel.Error(), err)
	}

	switch {
	case errors.Is(err, ErrOpenShiftRequired):
		return coded(http.StatusConflict, "OPEN_SALES_SHIFT_REQUIRED", ErrOpenShiftRequired)
	case errors.Is(err, ErrServiceSessionNotFound):
		return coded(http.StatusNotFound, "SERVICE_SESSION_NOT_FOUND", ErrServiceSessionNotFound)
	case errors.Is(err, ErrServiceSessionClosed):
		return coded(http.StatusConflict, "SERVICE_SESSION_ALREADY_CLOSED", ErrServiceSessionClosed)
	case errors.Is(err, ErrEditableDraftNotFound):
		return coded(http.StatusConflict, "EDITABLE_DRAFT_NOT_FOUND", ErrEditableDraftNotFound)
	case errors.Is(err, ErrDraftItemNotFound):
		return coded(http.StatusNotFound, "DRAFT_ITEM_NOT_FOUND", ErrDraftItemNotFound)

	case errors.Is(err, ErrMenuItemNotFound):
		return coded(http.StatusNotFound, "MENU_ITEM_NOT_FOUND", ErrMenuItemNotFound)
	case errors.Is(err, ErrMenuItemUnavailable):
		return coded(http.StatusConflict, "MENU_ITEM_UNAVAILABLE", ErrMenuItemUnavailable)
	case errors.Is(err, ErrMenuItemRetired):
		return coded(http.StatusConflict, "MENU_ITEM_RETIRED", ErrMenuItemRetired)

	case errors.Is(err, ErrSizeNotFound):
		return coded(http.StatusNotFound, "SIZE_NOT_FOUND", ErrSizeNotFound)
	case errors.Is(err, ErrSizeUnavailable):
		return coded(http.StatusConflict, "SIZE_UNAVAILABLE", ErrSizeUnavailable)
	case errors.Is(err, ErrSizeRetired):
		return coded(http.StatusConflict, "SIZE_RETIRED", ErrSizeRetired)

	case errors.Is(err, ErrModifierOptionNotFound):
		return coded(http.StatusNotFound, "MODIFIER_OPTION_NOT_FOUND", ErrModifierOptionNotFound)
	case errors.Is(err, ErrModifierOptionUnavailable):
		return coded(http.StatusConflict, "MODIFIER_OPTION_UNAVAILABLE", ErrModifierOptionUnavailable)
	case errors.Is(err, ErrModifierOptionRetired):
		return coded(http.StatusConflict, "MODIFIER_OPTION_RETIRED", ErrModifierOptionRetired)

	case errors.Is(err, ErrInvalidPreparationNote):
		return response.NewCodedError(http.StatusBadRequest, "INVALID_PREPARATION_NOTE", err.Error(), err)

	case errors.Is(err, ErrTableSelectionRequired):
		return coded(http.StatusBadRequest, "DINE_IN_TABLE_SELECTION_REQUIRED", ErrTableSelectionRequired)
	case errors.Is(err, ErrTableSelectionDuplicate):
		return coded(http.StatusBadRequest, "DINE_IN_TABLE_SELECTION_DUPLICATE", ErrTableSelectionDuplicate)
	case errors.Is(err, ErrTableNotFound):
		return coded(http.StatusNotFound, "DINE_IN_TABLE_NOT_FOUND", ErrTableNotFound)
	case errors.Is(err, ErrTableUnavailable):
		// The wrapped message names the Table so staff know which one to free;
		// it contains no data the caller could not already see.
		return response.NewCodedError(http.StatusConflict, "DINE_IN_TABLE_UNAVAILABLE", err.Error(), err)
	case errors.Is(err, ErrTakeawayTablesNotAvailable):
		return coded(http.StatusConflict, "TAKEAWAY_TABLE_ASSIGNMENT_NOT_AVAILABLE", ErrTakeawayTablesNotAvailable)

	case errors.Is(err, ErrRequestConflict):
		return response.NewCodedError(http.StatusConflict, "REQUEST_CONFLICT", err.Error(), err)
	case errors.Is(err, ErrInvalidStoredResult):
		return response.NewCodedError(http.StatusInternalServerError, "INVALID_STORED_RESULT",
			"an unexpected error occurred", err)
	case errors.Is(err, ErrServiceSequenceExhausted):
		return response.NewCodedError(http.StatusInternalServerError, "SERVICE_SEQUENCE_EXHAUSTED",
			"an unexpected error occurred", err)

	// Both denial sentinels collapse to one code so the API never discloses
	// which condition failed. The reason is in the server log and the audit
	// event.
	case errors.Is(err, ErrForbidden):
		return response.NewCodedError(http.StatusForbidden, "NOT_AUTHORIZED", "not authorized", err)
	case errors.Is(err, ErrUnauthorized):
		return response.NewCodedError(http.StatusUnauthorized, "NOT_AUTHORIZED", "not authorized", err)

	// ErrInvalidQuantity is wrapped in response.ErrInvalid by its caller, so
	// it lands on the generic validation code alongside every other bad field.
	case errors.Is(err, response.ErrInvalid), errors.Is(err, ErrInvalidQuantity):
		return response.NewCodedError(http.StatusBadRequest, "INVALID_INPUT", err.Error(), err)
	default:
		return err
	}
}
