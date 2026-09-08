package httpvalidator_test

import (
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/httpvalidator"
	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type sampleStruct struct {
	Name  string `validate:"required,min=2,max=10"`
	Age   int    `validate:"gte=18,lte=65"`
	Email string `validate:"omitempty,email"`
}

func TestValidator(t *testing.T) {
	t.Parallel()
	v := httpvalidator.New()

	t.Run("valid struct", func(t *testing.T) {
		t.Parallel()
		s := sampleStruct{Name: "Alice", Age: 25}
		err := v.Validate(&s)
		require.NoError(t, err)
	})

	t.Run("required field missing", func(t *testing.T) {
		t.Parallel()
		s := sampleStruct{Age: 25}
		err := v.Validate(&s)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "field 'name' is required")
	})

	t.Run("min characters failed", func(t *testing.T) {
		t.Parallel()
		s := sampleStruct{Name: "A", Age: 25}
		err := v.Validate(&s)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be at least 2 characters")
	})

	t.Run("max characters failed", func(t *testing.T) {
		t.Parallel()
		s := sampleStruct{Name: "Supercalifragilistic", Age: 25}
		err := v.Validate(&s)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be at most 10 characters")
	})

	t.Run("gte failed", func(t *testing.T) {
		t.Parallel()
		s := sampleStruct{Name: "Bob", Age: 16}
		err := v.Validate(&s)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be greater than or equal to 18")
	})

	t.Run("lte failed", func(t *testing.T) {
		t.Parallel()
		s := sampleStruct{Name: "Bob", Age: 70}
		err := v.Validate(&s)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be less than or equal to 65")
	})
}

func TestValidatorNonStructInput(t *testing.T) {
	t.Parallel()

	// A non-struct argument is a programming error, not a client error, so the
	// library's own *InvalidValidationError must surface unchanged rather than
	// being reformatted into a field message.
	err := httpvalidator.New().Validate("not a struct")

	require.Error(t, err)
	var invalid *validator.InvalidValidationError
	assert.ErrorAs(t, err, &invalid)
}
