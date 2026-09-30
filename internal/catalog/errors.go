package catalog

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
	ErrNotFound                     = errors.New("catalog entity not found")
	ErrNameConflict                 = errors.New("catalog name conflict")
	ErrCodeConflict                 = errors.New("catalog code conflict")
	ErrInvalidPricingConfiguration  = errors.New("invalid pricing configuration")
	ErrInvalidModifierConfiguration = errors.New("invalid modifier configuration")
	ErrInvalidCategoryConfiguration = errors.New("invalid category configuration")
	ErrInvalidRetirement            = errors.New("invalid retirement configuration")
	ErrInvalidInheritance           = errors.New("invalid inheritance")
	ErrEntityRetired                = errors.New("entity retired")
	ErrInvalidManagerPin            = errors.New("invalid manager pin")

	ErrRequestConflict     = command.ErrRequestConflict
	ErrForbidden           = command.ErrForbidden
	ErrUnauthorized        = command.ErrUnauthorized
	ErrInvalidStoredResult = command.ErrInvalidStoredResult
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
			if pgErr.ConstraintName == "menu_items_active_code_key" {
				return fmt.Errorf("%w: %s", ErrCodeConflict, pgErr.Detail)
			}
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

// httpErrors is the Catalog HTTP error mapping. Every rule answers with the
// error's own text except a corrupted stored result, whose text would echo
// internal replay state.
var httpErrors = response.ErrorMapper{
	{Target: ErrNotFound, Spec: response.ErrorSpec{Status: http.StatusNotFound, Code: "CATALOG_NOT_FOUND"}},
	{Target: ErrCodeConflict, Spec: response.ErrorSpec{Status: http.StatusConflict, Code: "CATALOG_CODE_CONFLICT"}},
	{Target: ErrNameConflict, Spec: response.ErrorSpec{Status: http.StatusConflict, Code: "CATALOG_NAME_CONFLICT"}},
	{Target: ErrRequestConflict, Spec: response.ErrorSpec{Status: http.StatusConflict, Code: "REQUEST_CONFLICT"}},
	{Target: ErrInvalidPricingConfiguration, Spec: response.ErrorSpec{Status: http.StatusBadRequest, Code: "INVALID_PRICING_CONFIGURATION"}},
	{Target: ErrInvalidModifierConfiguration, Spec: response.ErrorSpec{Status: http.StatusBadRequest, Code: "INVALID_MODIFIER_CONFIGURATION"}},
	{Target: ErrInvalidCategoryConfiguration, Spec: response.ErrorSpec{Status: http.StatusBadRequest, Code: "INVALID_CATEGORY_CONFIGURATION"}},
	{Target: ErrInvalidRetirement, Spec: response.ErrorSpec{Status: http.StatusBadRequest, Code: "INVALID_RETIREMENT"}},
	{Target: ErrInvalidInheritance, Spec: response.ErrorSpec{Status: http.StatusConflict, Code: "INVALID_INHERITANCE"}},
	{Target: ErrEntityRetired, Spec: response.ErrorSpec{Status: http.StatusConflict, Code: "ENTITY_RETIRED"}},
	{Target: ErrInvalidManagerPin, Spec: response.ErrorSpec{Status: http.StatusForbidden, Code: "INVALID_MANAGER_PIN"}},
	{Target: ErrForbidden, Spec: response.ErrorSpec{Status: http.StatusForbidden, Code: "FORBIDDEN"}},
	{Target: ErrUnauthorized, Spec: response.ErrorSpec{Status: http.StatusUnauthorized, Code: "UNAUTHORIZED"}},
	{Target: ErrInvalidStoredResult, Spec: response.ErrorSpec{
		Status: http.StatusInternalServerError, Code: "INVALID_STORED_RESULT", Message: "an unexpected error occurred",
	}},
	{Target: ErrInvalidImage, Spec: response.ErrorSpec{Status: http.StatusBadRequest, Code: "INVALID_IMAGE"}},
	{Target: ErrImageTooLarge, Spec: response.ErrorSpec{Status: http.StatusRequestEntityTooLarge, Code: "IMAGE_TOO_LARGE"}},
	{Target: response.ErrInvalid, Spec: response.ErrorSpec{Status: http.StatusBadRequest, Code: "INVALID_INPUT"}},
}

// MapHTTPError maps catalog domain errors and input validation errors to
// *response.CodedError.
func MapHTTPError(err error) error {
	return httpErrors.Map(err)
}
