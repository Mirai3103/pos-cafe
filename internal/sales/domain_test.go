package sales_test

import (
	"math"
	"strings"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func strPtr(s string) *string { return &s }

func TestNormalizePreparationNote(t *testing.T) {
	t.Run("nil stays null", func(t *testing.T) {
		got, err := sales.NormalizePreparationNote(nil)
		require.NoError(t, err)
		assert.False(t, got.Valid)
	})

	t.Run("whitespace only becomes null", func(t *testing.T) {
		got, err := sales.NormalizePreparationNote(strPtr("   \t\n "))
		require.NoError(t, err)
		assert.False(t, got.Valid)
	})

	t.Run("surrounding whitespace is trimmed", func(t *testing.T) {
		got, err := sales.NormalizePreparationNote(strPtr("  ít đá  "))
		require.NoError(t, err)
		require.True(t, got.Valid)
		assert.Equal(t, "ít đá", got.String)
	})

	t.Run("200 code points is accepted", func(t *testing.T) {
		got, err := sales.NormalizePreparationNote(strPtr(strings.Repeat("á", 200)))
		require.NoError(t, err)
		assert.True(t, got.Valid)
	})

	t.Run("201 code points is rejected", func(t *testing.T) {
		_, err := sales.NormalizePreparationNote(strPtr(strings.Repeat("á", 201)))
		require.Error(t, err)
		assert.ErrorIs(t, err, sales.ErrInvalidPreparationNote)
	})

	t.Run("length is counted in code points not bytes", func(t *testing.T) {
		// 100 three-byte characters is 300 bytes but only 100 code points, so
		// a byte-based check would wrongly reject it.
		got, err := sales.NormalizePreparationNote(strPtr(strings.Repeat("ế", 100)))
		require.NoError(t, err)
		assert.True(t, got.Valid)
	})
}

func TestValidateQuantity(t *testing.T) {
	assert.Error(t, sales.ValidateQuantity(0))
	assert.NoError(t, sales.ValidateQuantity(1))
	assert.NoError(t, sales.ValidateQuantity(9999))
	assert.Error(t, sales.ValidateQuantity(10000))
	assert.Error(t, sales.ValidateQuantity(-1))
}

func TestModifierKeyFor(t *testing.T) {
	a := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	b := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	t.Run("empty set is the empty string", func(t *testing.T) {
		assert.Equal(t, "", sales.ModifierKeyFor(nil))
		assert.Equal(t, "", sales.ModifierKeyFor([]uuid.UUID{}))
	})

	t.Run("input order does not change the key", func(t *testing.T) {
		assert.Equal(t, sales.ModifierKeyFor([]uuid.UUID{a, b}), sales.ModifierKeyFor([]uuid.UUID{b, a}))
	})

	t.Run("key is sorted and comma joined", func(t *testing.T) {
		assert.Equal(t, a.String()+","+b.String(), sales.ModifierKeyFor([]uuid.UUID{b, a}))
	})
}

func TestHasDuplicateUUIDs(t *testing.T) {
	a := uuid.New()
	b := uuid.New()
	assert.False(t, sales.HasDuplicateUUIDs([]uuid.UUID{a, b}))
	assert.True(t, sales.HasDuplicateUUIDs([]uuid.UUID{a, b, a}))
	assert.False(t, sales.HasDuplicateUUIDs(nil))
}

func TestFormatServiceNumber(t *testing.T) {
	t.Run("pads to six characters", func(t *testing.T) {
		got, err := sales.FormatServiceNumber(1)
		require.NoError(t, err)
		assert.Equal(t, "S00001", got)
	})

	t.Run("upper bound", func(t *testing.T) {
		got, err := sales.FormatServiceNumber(99999)
		require.NoError(t, err)
		assert.Equal(t, "S99999", got)
	})

	t.Run("overflow is an error not a longer string", func(t *testing.T) {
		// The database CHECK is ^[A-Z0-9]{6}$, so a seventh character would
		// fail at insert time with an unmapped 23514 instead of here.
		_, err := sales.FormatServiceNumber(100000)
		require.Error(t, err)
	})

	t.Run("zero and negative are errors", func(t *testing.T) {
		_, err := sales.FormatServiceNumber(0)
		require.Error(t, err)
		_, err = sales.FormatServiceNumber(-1)
		require.Error(t, err)
	})

	t.Run("every formatted value matches the database pattern", func(t *testing.T) {
		for _, seq := range []int32{1, 9, 10, 999, 1000, 99999} {
			got, err := sales.FormatServiceNumber(seq)
			require.NoError(t, err)
			assert.Len(t, got, 6)
			assert.Regexp(t, `^[A-Z0-9]{6}$`, got)
		}
	})
}

func TestLineTotal(t *testing.T) {
	t.Run("multiplies quantity by unit price", func(t *testing.T) {
		got, err := sales.LineTotal(3, 25_000)
		require.NoError(t, err)
		require.Equal(t, int64(75_000), got)
	})

	t.Run("rejects a non-positive unit price", func(t *testing.T) {
		_, err := sales.LineTotal(1, 0)
		require.ErrorIs(t, err, sales.ErrLineTotalOutOfRange)
	})

	t.Run("rejects a multiplication that would overflow int64", func(t *testing.T) {
		_, err := sales.LineTotal(9999, math.MaxInt64/2)
		require.ErrorIs(t, err, sales.ErrLineTotalOutOfRange)
	})

	t.Run("accepts the largest realistic line", func(t *testing.T) {
		got, err := sales.LineTotal(9999, 2_147_483_647)
		require.NoError(t, err)
		require.Equal(t, int64(9999)*2_147_483_647, got)
	})
}

func TestAddCharge(t *testing.T) {
	t.Run("accumulates", func(t *testing.T) {
		got, err := sales.AddCharge(50_000, 25_000)
		require.NoError(t, err)
		require.Equal(t, int64(75_000), got)
	})

	t.Run("rejects an accumulation that would overflow int64", func(t *testing.T) {
		_, err := sales.AddCharge(math.MaxInt64-10, 100)
		require.ErrorIs(t, err, sales.ErrCheckChargeOutOfRange)
	})

	t.Run("rejects an accumulation that would underflow int64", func(t *testing.T) {
		_, err := sales.AddCharge(math.MinInt64, -1)
		require.ErrorIs(t, err, sales.ErrCheckChargeOutOfRange)
	})

	t.Run("rejects a negative result", func(t *testing.T) {
		_, err := sales.AddCharge(10, -100)
		require.ErrorIs(t, err, sales.ErrCheckChargeOutOfRange)
	})
}

func TestValidateCheckTarget(t *testing.T) {
	require.NoError(t, sales.ValidateCheckTarget("CURRENT_UNPAID"))
	require.NoError(t, sales.ValidateCheckTarget("NEW_CHECK"))
	require.Error(t, sales.ValidateCheckTarget("PAID"))
	require.Error(t, sales.ValidateCheckTarget(""))
}
