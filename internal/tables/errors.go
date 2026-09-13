package tables

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrTableNotFound       = errors.New("table not found")
	ErrNameConflict        = errors.New("table name conflict")
	ErrRequestConflict     = errors.New("request conflict")
	ErrForbidden           = errors.New("forbidden")
	ErrUnauthorized        = errors.New("unauthorized")
	ErrInvalidStoredResult = errors.New("invalid stored result")
)

// MapDBError maps PostgreSQL driver and database errors to domain sentinels.
func MapDBError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %s", ErrTableNotFound, err.Error())
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		msg := pgErr.Detail
		if msg == "" {
			msg = pgErr.Message
		}
		switch pgErr.Code {
		case "23505": // unique_violation
			return fmt.Errorf("%w: %s", ErrNameConflict, msg)
		case "23503": // foreign_key_violation
			return fmt.Errorf("%w: %s", ErrTableNotFound, msg)
		}
	}
	return err
}

// MapHTTPError maps tables domain errors and input validation errors to
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
	case errors.Is(err, ErrTableNotFound):
		return response.NewCodedError(http.StatusNotFound, "TABLE_NOT_FOUND", err.Error(), err)
	case errors.Is(err, ErrNameConflict):
		return response.NewCodedError(http.StatusConflict, "TABLE_NAME_CONFLICT", err.Error(), err)
	case errors.Is(err, ErrRequestConflict):
		return response.NewCodedError(http.StatusConflict, "REQUEST_CONFLICT", err.Error(), err)
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
