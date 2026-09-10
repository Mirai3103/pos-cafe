package catalog_test

import (
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/catalog"
	"github.com/stretchr/testify/assert"
)

func TestCatalogErrorsAreSentinels(t *testing.T) {
	t.Parallel()

	errors := map[string]error{
		"ErrNotFound":                      catalog.ErrNotFound,
		"ErrNameConflict":                  catalog.ErrNameConflict,
		"ErrRequestConflict":               catalog.ErrRequestConflict,
		"ErrInvalidPricingConfiguration":   catalog.ErrInvalidPricingConfiguration,
		"ErrInvalidModifierConfiguration":  catalog.ErrInvalidModifierConfiguration,
		"ErrInvalidInheritance":            catalog.ErrInvalidInheritance,
		"ErrEntityRetired":                 catalog.ErrEntityRetired,
		"ErrInvalidManagerPin":             catalog.ErrInvalidManagerPin,
		"ErrForbidden":                     catalog.ErrForbidden,
		"ErrUnauthorized":                  catalog.ErrUnauthorized,
		"ErrInvalidStoredResult":           catalog.ErrInvalidStoredResult,
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
