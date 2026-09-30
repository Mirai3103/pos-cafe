package tables

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
	ErrTableNotFound = errors.New("table not found")
	ErrNameConflict  = errors.New("table name conflict")

	ErrRequestConflict     = command.ErrRequestConflict
	ErrForbidden           = command.ErrForbidden
	ErrUnauthorized        = command.ErrUnauthorized
	ErrInvalidStoredResult = command.ErrInvalidStoredResult
)

// MapDBError maps PostgreSQL driver and database errors to domain sentinels.
// The wrapped text keeps the driver's detail for diagnostics; httpErrors
// replaces it with a stable message before anything reaches a client.
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

// httpErrors is the Tables HTTP error mapping. Not-found and name-conflict
// errors come from MapDBError and carry PostgreSQL detail such as the
// conflicting key value, so they answer with their sentinel's fixed text.
var httpErrors = response.ErrorMapper{
	{Target: ErrTableNotFound, Spec: response.ErrorSpec{
		Status: http.StatusNotFound, Code: "TABLE_NOT_FOUND", Message: ErrTableNotFound.Error(),
	}},
	{Target: ErrNameConflict, Spec: response.ErrorSpec{
		Status: http.StatusConflict, Code: "TABLE_NAME_CONFLICT", Message: ErrNameConflict.Error(),
	}},
	{Target: ErrRequestConflict, Spec: response.ErrorSpec{Status: http.StatusConflict, Code: "REQUEST_CONFLICT"}},
	{Target: ErrForbidden, Spec: response.ErrorSpec{Status: http.StatusForbidden, Code: "FORBIDDEN"}},
	{Target: ErrUnauthorized, Spec: response.ErrorSpec{Status: http.StatusUnauthorized, Code: "UNAUTHORIZED"}},
	{Target: ErrInvalidStoredResult, Spec: response.ErrorSpec{
		Status: http.StatusInternalServerError, Code: "INVALID_STORED_RESULT", Message: "an unexpected error occurred",
	}},
	{Target: response.ErrInvalid, Spec: response.ErrorSpec{Status: http.StatusBadRequest, Code: "INVALID_INPUT"}},
}

// MapHTTPError maps tables domain errors and input validation errors to
// *response.CodedError.
func MapHTTPError(err error) error {
	return httpErrors.Map(err)
}
