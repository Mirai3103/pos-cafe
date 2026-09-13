package shift_test

import (
	"strings"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/shift"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func strPtr(s string) *string { return &s }

func TestValidateOpeningFloat(t *testing.T) {
	// Zero is valid: a station may legitimately open with an empty fund.
	assert.NoError(t, shift.ValidateOpeningFloat(0))
	assert.NoError(t, shift.ValidateOpeningFloat(500000))
	assert.NoError(t, shift.ValidateOpeningFloat(shift.MaxAmountVND))

	assert.Error(t, shift.ValidateOpeningFloat(-1))
	assert.Error(t, shift.ValidateOpeningFloat(shift.MaxAmountVND+1))
}

func TestValidateAmount(t *testing.T) {
	// A Cash Movement amount is strictly positive; direction lives in method.
	assert.Error(t, shift.ValidateAmount(0))
	assert.Error(t, shift.ValidateAmount(-1000))
	assert.Error(t, shift.ValidateAmount(shift.MaxAmountVND+1))

	assert.NoError(t, shift.ValidateAmount(1))
	assert.NoError(t, shift.ValidateAmount(shift.MaxAmountVND))
}

func TestValidateMethod(t *testing.T) {
	assert.NoError(t, shift.ValidateMethod(shift.MethodPayIn))
	assert.NoError(t, shift.ValidateMethod(shift.MethodPayOut))

	// CASH_DROP appears in the superseded MIGRATE_PLAN sketch; it is not a method.
	assert.Error(t, shift.ValidateMethod("CASH_DROP"))
	assert.Error(t, shift.ValidateMethod("pay_in"))
	assert.Error(t, shift.ValidateMethod(""))
}

func TestValidateReason(t *testing.T) {
	for _, reason := range []string{
		shift.ReasonAddChangeFund,
		shift.ReasonRemoveExcessFloat,
		shift.ReasonSafeDrop,
		shift.ReasonOther,
	} {
		assert.NoError(t, shift.ValidateReason(reason), reason)
	}
	assert.Error(t, shift.ValidateReason("PETTY_CASH"))
	assert.Error(t, shift.ValidateReason(""))
}

func TestNormalizeNote(t *testing.T) {
	assert.Nil(t, shift.NormalizeNote(nil))
	assert.Nil(t, shift.NormalizeNote(strPtr("")))
	assert.Nil(t, shift.NormalizeNote(strPtr("   ")))

	got := shift.NormalizeNote(strPtr("  mua da  "))
	require.NotNil(t, got)
	assert.Equal(t, "mua da", *got)
}

func TestValidateNote(t *testing.T) {
	// An absent note is fine for every reason except OTHER.
	assert.NoError(t, shift.ValidateNote(nil, shift.ReasonSafeDrop))
	assert.Error(t, shift.ValidateNote(nil, shift.ReasonOther),
		"an unexplained OTHER movement is exactly the shrinkage this control prevents")

	assert.NoError(t, shift.ValidateNote(strPtr("mua da"), shift.ReasonOther))
	assert.NoError(t, shift.ValidateNote(strPtr("x"), shift.ReasonSafeDrop))

	atLimit := strings.Repeat("n", shift.MaxNoteLength)
	assert.NoError(t, shift.ValidateNote(&atLimit, shift.ReasonSafeDrop))

	overLimit := strings.Repeat("n", shift.MaxNoteLength+1)
	assert.Error(t, shift.ValidateNote(&overLimit, shift.ReasonSafeDrop))

	// Length is counted in Unicode code points so Go agrees with char_length.
	vietnamese := strings.Repeat("ế", shift.MaxNoteLength)
	assert.NoError(t, shift.ValidateNote(&vietnamese, shift.ReasonSafeDrop))
}

func TestComputeExpectedCash(t *testing.T) {
	got, err := shift.ComputeExpectedCash(500000, 100000, 150000)
	require.NoError(t, err)
	assert.Equal(t, int64(450000), got)

	// No movements yet: Expected Cash is the Opening Float.
	got, err = shift.ComputeExpectedCash(500000, 0, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(500000), got)

	// Sustained Pay Outs can legitimately drive the Phase 4 partial figure
	// negative, so the guard is symmetric rather than one-sided.
	got, err = shift.ComputeExpectedCash(0, 0, 1000)
	require.NoError(t, err)
	assert.Equal(t, int64(-1000), got)

	_, err = shift.ComputeExpectedCash(shift.MaxAmountVND, 1, 0)
	assert.ErrorIs(t, err, shift.ErrExpectedCashOutOfRange)

	_, err = shift.ComputeExpectedCash(0, 0, shift.MaxAmountVND+1)
	assert.ErrorIs(t, err, shift.ErrExpectedCashOutOfRange)
}
