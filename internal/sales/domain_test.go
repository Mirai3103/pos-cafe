package sales

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func strPtr(s string) *string { return &s }

func TestNormalizePreparationNote(t *testing.T) {
	t.Run("nil stays null", func(t *testing.T) {
		got, err := NormalizePreparationNote(nil)
		require.NoError(t, err)
		assert.False(t, got.Valid)
	})

	t.Run("whitespace only becomes null", func(t *testing.T) {
		got, err := NormalizePreparationNote(strPtr("   \t\n "))
		require.NoError(t, err)
		assert.False(t, got.Valid)
	})

	t.Run("surrounding whitespace is trimmed", func(t *testing.T) {
		got, err := NormalizePreparationNote(strPtr("  ít đá  "))
		require.NoError(t, err)
		require.True(t, got.Valid)
		assert.Equal(t, "ít đá", got.String)
	})

	t.Run("200 code points is accepted", func(t *testing.T) {
		got, err := NormalizePreparationNote(strPtr(strings.Repeat("á", 200)))
		require.NoError(t, err)
		assert.True(t, got.Valid)
	})

	t.Run("201 code points is rejected", func(t *testing.T) {
		_, err := NormalizePreparationNote(strPtr(strings.Repeat("á", 201)))
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidPreparationNote)
	})

	t.Run("length is counted in code points not bytes", func(t *testing.T) {
		// 100 three-byte characters is 300 bytes but only 100 code points, so
		// a byte-based check would wrongly reject it.
		got, err := NormalizePreparationNote(strPtr(strings.Repeat("ế", 100)))
		require.NoError(t, err)
		assert.True(t, got.Valid)
	})
}

func TestValidateQuantity(t *testing.T) {
	assert.Error(t, ValidateQuantity(0))
	assert.NoError(t, ValidateQuantity(1))
	assert.NoError(t, ValidateQuantity(9999))
	assert.Error(t, ValidateQuantity(10000))
	assert.Error(t, ValidateQuantity(-1))
}

func TestModifierKeyFor(t *testing.T) {
	a := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	b := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	t.Run("empty set is the empty string", func(t *testing.T) {
		assert.Equal(t, "", ModifierKeyFor(nil))
		assert.Equal(t, "", ModifierKeyFor([]uuid.UUID{}))
	})

	t.Run("input order does not change the key", func(t *testing.T) {
		assert.Equal(t, ModifierKeyFor([]uuid.UUID{a, b}), ModifierKeyFor([]uuid.UUID{b, a}))
	})

	t.Run("key is sorted and comma joined", func(t *testing.T) {
		assert.Equal(t, a.String()+","+b.String(), ModifierKeyFor([]uuid.UUID{b, a}))
	})
}

func TestHasDuplicateUUIDs(t *testing.T) {
	a := uuid.New()
	b := uuid.New()
	assert.False(t, HasDuplicateUUIDs([]uuid.UUID{a, b}))
	assert.True(t, HasDuplicateUUIDs([]uuid.UUID{a, b, a}))
	assert.False(t, HasDuplicateUUIDs(nil))
}

func TestFormatServiceNumber(t *testing.T) {
	t.Run("pads to six characters", func(t *testing.T) {
		got, err := FormatServiceNumber(1)
		require.NoError(t, err)
		assert.Equal(t, "S00001", got)
	})

	t.Run("upper bound", func(t *testing.T) {
		got, err := FormatServiceNumber(99999)
		require.NoError(t, err)
		assert.Equal(t, "S99999", got)
	})

	t.Run("overflow is an error not a longer string", func(t *testing.T) {
		// The database CHECK is ^[A-Z0-9]{6}$, so a seventh character would
		// fail at insert time with an unmapped 23514 instead of here.
		_, err := FormatServiceNumber(100000)
		require.Error(t, err)
	})

	t.Run("zero and negative are errors", func(t *testing.T) {
		_, err := FormatServiceNumber(0)
		require.Error(t, err)
		_, err = FormatServiceNumber(-1)
		require.Error(t, err)
	})

	t.Run("every formatted value matches the database pattern", func(t *testing.T) {
		for _, seq := range []int32{1, 9, 10, 999, 1000, 99999} {
			got, err := FormatServiceNumber(seq)
			require.NoError(t, err)
			assert.Len(t, got, 6)
			assert.Regexp(t, `^[A-Z0-9]{6}$`, got)
		}
	})
}
