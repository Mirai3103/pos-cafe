package catalog

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrNotFound                     = errors.New("catalog entity not found")
	ErrNameConflict                 = errors.New("catalog name conflict")
	ErrRequestConflict              = errors.New("request conflict")
	ErrInvalidPricingConfiguration  = errors.New("invalid pricing configuration")
	ErrInvalidModifierConfiguration = errors.New("invalid modifier configuration")
	ErrInvalidRetirement            = errors.New("invalid retirement configuration")
	ErrInvalidInheritance           = errors.New("invalid inheritance")
	ErrEntityRetired                = errors.New("entity retired")
	ErrInvalidManagerPin            = errors.New("invalid manager pin")
	ErrForbidden                    = errors.New("forbidden")
	ErrUnauthorized                 = errors.New("unauthorized")
	ErrInvalidStoredResult          = errors.New("invalid stored result")
)

// MapDBError maps PostgreSQL driver and database errors to domain sentinels.
func MapDBError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %s", ErrNotFound, err.Error())
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			msg := pgErr.Detail
			if msg == "" {
				msg = pgErr.Message
			}
			return fmt.Errorf("%w: %s", ErrNameConflict, msg)
		case "23503": // foreign_key_violation
			msg := pgErr.Detail
			if msg == "" {
				msg = pgErr.Message
			}
			return fmt.Errorf("%w: %s", ErrNotFound, msg)
		}
	}
	return err
}

// MapHTTPError maps catalog domain errors and input validation errors to *response.CodedError.
func MapHTTPError(err error) error {
	if err == nil {
		return nil
	}
	var codedErr *response.CodedError
	if errors.As(err, &codedErr) {
		return err
	}
	switch {
	case errors.Is(err, ErrNotFound):
		return response.NewCodedError(http.StatusNotFound, "CATALOG_NOT_FOUND", err.Error(), err)
	case errors.Is(err, ErrNameConflict):
		return response.NewCodedError(http.StatusConflict, "CATALOG_NAME_CONFLICT", err.Error(), err)
	case errors.Is(err, ErrRequestConflict):
		return response.NewCodedError(http.StatusConflict, "REQUEST_CONFLICT", err.Error(), err)
	case errors.Is(err, ErrInvalidPricingConfiguration):
		return response.NewCodedError(http.StatusBadRequest, "INVALID_PRICING_CONFIGURATION", err.Error(), err)
	case errors.Is(err, ErrInvalidModifierConfiguration):
		return response.NewCodedError(http.StatusBadRequest, "INVALID_MODIFIER_CONFIGURATION", err.Error(), err)
	case errors.Is(err, ErrInvalidRetirement):
		return response.NewCodedError(http.StatusBadRequest, "INVALID_RETIREMENT", err.Error(), err)
	case errors.Is(err, ErrInvalidInheritance):
		return response.NewCodedError(http.StatusConflict, "INVALID_INHERITANCE", err.Error(), err)
	case errors.Is(err, ErrEntityRetired):
		return response.NewCodedError(http.StatusConflict, "ENTITY_RETIRED", err.Error(), err)
	case errors.Is(err, ErrInvalidManagerPin):
		return response.NewCodedError(http.StatusForbidden, "INVALID_MANAGER_PIN", err.Error(), err)
	case errors.Is(err, ErrForbidden):
		return response.NewCodedError(http.StatusForbidden, "FORBIDDEN", err.Error(), err)
	case errors.Is(err, ErrUnauthorized):
		return response.NewCodedError(http.StatusUnauthorized, "UNAUTHORIZED", err.Error(), err)
	case errors.Is(err, ErrInvalidStoredResult):
		return response.NewCodedError(http.StatusInternalServerError, "INVALID_STORED_RESULT", "an unexpected error occurred", err)
	case errors.Is(err, response.ErrInvalid):
		return response.NewCodedError(http.StatusBadRequest, "INVALID_INPUT", err.Error(), err)
	default:
		return err
	}
}
