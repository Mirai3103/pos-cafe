package catalog_test

import (
	"database/sql"
	"errors"
	"net/http"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/catalog"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
)

func TestCatalogErrorsAreSentinels(t *testing.T) {
	t.Parallel()

	errors := map[string]error{
		"ErrNotFound":                     catalog.ErrNotFound,
		"ErrNameConflict":                 catalog.ErrNameConflict,
		"ErrRequestConflict":              catalog.ErrRequestConflict,
		"ErrInvalidPricingConfiguration":  catalog.ErrInvalidPricingConfiguration,
		"ErrInvalidModifierConfiguration": catalog.ErrInvalidModifierConfiguration,
		"ErrInvalidInheritance":           catalog.ErrInvalidInheritance,
		"ErrEntityRetired":                catalog.ErrEntityRetired,
		"ErrInvalidManagerPin":            catalog.ErrInvalidManagerPin,
		"ErrForbidden":                    catalog.ErrForbidden,
		"ErrUnauthorized":                 catalog.ErrUnauthorized,
		"ErrInvalidStoredResult":          catalog.ErrInvalidStoredResult,
	}

	for name, err := range errors {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Error(t, err)
			assert.NotEmpty(t, err.Error())
		})
	}
}

func TestCatalogErrorsAreDistinct(t *testing.T) {
	t.Parallel()

	all := []error{
		catalog.ErrNotFound,
		catalog.ErrNameConflict,
		catalog.ErrRequestConflict,
		catalog.ErrInvalidPricingConfiguration,
		catalog.ErrInvalidModifierConfiguration,
		catalog.ErrInvalidInheritance,
		catalog.ErrEntityRetired,
		catalog.ErrInvalidManagerPin,
		catalog.ErrForbidden,
		catalog.ErrUnauthorized,
		catalog.ErrInvalidStoredResult,
	}

	seen := make(map[error]bool, len(all))
	for _, err := range all {
		assert.False(t, seen[err], "duplicate error sentinel: %v", err)
		seen[err] = true
	}
}

func TestMapDBError(t *testing.T) {
	t.Parallel()

	assert.Nil(t, catalog.MapDBError(nil))

	// sql.ErrNoRows -> ErrNotFound
	err := catalog.MapDBError(sql.ErrNoRows)
	assert.True(t, errors.Is(err, catalog.ErrNotFound))

	// Code 23505 -> ErrNameConflict
	err = catalog.MapDBError(&pgconn.PgError{Code: "23505", Detail: "key already exists"})
	assert.True(t, errors.Is(err, catalog.ErrNameConflict))

	// Code 23503 -> ErrNotFound
	err = catalog.MapDBError(&pgconn.PgError{Code: "23503", Detail: "foreign key missing"})
	assert.True(t, errors.Is(err, catalog.ErrNotFound))

	// Other error -> unchanged
	otherErr := errors.New("something else")
	assert.Equal(t, otherErr, catalog.MapDBError(otherErr))
}

func TestMapHTTPError(t *testing.T) {
	t.Parallel()

	assert.Nil(t, catalog.MapHTTPError(nil))

	existingCoded := response.NewCodedError(http.StatusTeapot, "TEAPOT", "short and stout", nil)
	assert.Equal(t, existingCoded, catalog.MapHTTPError(existingCoded))

	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{
			name:       "ErrNotFound",
			err:        catalog.ErrNotFound,
			wantStatus: http.StatusNotFound,
			wantCode:   "CATALOG_NOT_FOUND",
		},
		{
			name:       "ErrNameConflict",
			err:        catalog.ErrNameConflict,
			wantStatus: http.StatusConflict,
			wantCode:   "CATALOG_NAME_CONFLICT",
		},
		{
			name:       "ErrRequestConflict",
			err:        catalog.ErrRequestConflict,
			wantStatus: http.StatusConflict,
			wantCode:   "REQUEST_CONFLICT",
		},
		{
			name:       "ErrInvalidPricingConfiguration",
			err:        catalog.ErrInvalidPricingConfiguration,
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_PRICING_CONFIGURATION",
		},
		{
			name:       "ErrInvalidModifierConfiguration",
			err:        catalog.ErrInvalidModifierConfiguration,
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_MODIFIER_CONFIGURATION",
		},
		{
			name:       "ErrInvalidInheritance",
			err:        catalog.ErrInvalidInheritance,
			wantStatus: http.StatusConflict,
			wantCode:   "INVALID_INHERITANCE",
		},
		{
			name:       "ErrEntityRetired",
			err:        catalog.ErrEntityRetired,
			wantStatus: http.StatusConflict,
			wantCode:   "ENTITY_RETIRED",
		},
		{
			name:       "ErrInvalidManagerPin",
			err:        catalog.ErrInvalidManagerPin,
			wantStatus: http.StatusForbidden,
			wantCode:   "INVALID_MANAGER_PIN",
		},
		{
			name:       "ErrForbidden",
			err:        catalog.ErrForbidden,
			wantStatus: http.StatusForbidden,
			wantCode:   "FORBIDDEN",
		},
		{
			name:       "ErrUnauthorized",
			err:        catalog.ErrUnauthorized,
			wantStatus: http.StatusUnauthorized,
			wantCode:   "UNAUTHORIZED",
		},
		{
			name:       "ErrInvalidStoredResult",
			err:        catalog.ErrInvalidStoredResult,
			wantStatus: http.StatusInternalServerError,
			wantCode:   "INVALID_STORED_RESULT",
		},
		{
			name:       "response.ErrInvalid",
			err:        response.ErrInvalid,
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_INPUT",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mapped := catalog.MapHTTPError(tt.err)
			var coded *response.CodedError
			assert.True(t, errors.As(mapped, &coded))
			assert.Equal(t, tt.wantStatus, coded.Status)
			assert.Equal(t, tt.wantCode, coded.Code)
		})
	}

	unmapped := errors.New("generic unmapped error")
	assert.Equal(t, unmapped, catalog.MapHTTPError(unmapped))
}

