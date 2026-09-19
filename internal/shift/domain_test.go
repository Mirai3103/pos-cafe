package shift_test

import (
	"math"
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
	// opening + cash payments - cash voids - completed cash refunds
	// + pay ins - pay outs (ADR-046).
	got, err := shift.ComputeExpectedCash(500_000, 850_000, 100_000, 50_000, 100_000, 150_000)
	require.NoError(t, err)
	assert.Equal(t, int64(1_150_000), got)

	// No payments, voids, refunds, or movements yet: the Opening Float.
	got, err = shift.ComputeExpectedCash(500_000, 0, 0, 0, 0, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(500_000), got)

	// A Cash Payment Void removes exactly its source's amount from the drawer.
	got, err = shift.ComputeExpectedCash(500_000, 850_000, 850_000, 0, 0, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(500_000), got)

	// Completed Cash Refunds have left the drawer.
	got, err = shift.ComputeExpectedCash(500_000, 850_000, 0, 100_000, 0, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(1_250_000), got)

	// Cash Movements still apply on top of the payment terms.
	got, err = shift.ComputeExpectedCash(500_000, 0, 0, 0, 100_000, 150_000)
	require.NoError(t, err)
	assert.Equal(t, int64(450_000), got)

	// Sustained Pay Outs can legitimately drive the figure negative, so the
	// guard is symmetric rather than one-sided.
	got, err = shift.ComputeExpectedCash(100_000, 0, 0, 0, 0, 500_000)
	require.NoError(t, err)
	assert.Equal(t, int64(-400_000), got)
}

func TestComputeExpectedCashRejectsTotalsOutsideTheBound(t *testing.T) {
	// A total outside the canonical bound is corrupt data even when the
	// arithmetic does not wrap. Both directions are failures.
	_, err := shift.ComputeExpectedCash(shift.MaxAmountVND, shift.MaxAmountVND, 0, 0, 0, 0)
	require.ErrorIs(t, err, shift.ErrExpectedCashOutOfRange)

	_, err = shift.ComputeExpectedCash(0, 0, 0, 0, 0, shift.MaxAmountVND+1)
	require.ErrorIs(t, err, shift.ErrExpectedCashOutOfRange)
}

func TestValidateDiscrepancyReason(t *testing.T) {
	// The discrepancy reason catalog (spec section 2): CASH_COUNT_DIFFERENCE
	// is valid only for CASH, QR_OBSERVATION_DIFFERENCE only for the two
	// Manual QR dimensions, UNEXPLAINED and OTHER for every dimension. OTHER
	// requires a note; every other reason forbids one.
	cases := []struct {
		name      string
		dimension string
		reason    string
		note      *string
		wantErr   bool
	}{
		{name: "cash count difference on cash",
			dimension: shift.DimensionCash, reason: shift.ReasonCashCountDifference},
		{name: "cash count difference on QR received is cross-dimension",
			dimension: shift.DimensionManualQRReceived, reason: shift.ReasonCashCountDifference, wantErr: true},
		{name: "cash count difference on QR refunded is cross-dimension",
			dimension: shift.DimensionManualQRRefunded, reason: shift.ReasonCashCountDifference, wantErr: true},
		{name: "QR observation difference on QR received",
			dimension: shift.DimensionManualQRReceived, reason: shift.ReasonQRObservationDifference},
		{name: "QR observation difference on QR refunded",
			dimension: shift.DimensionManualQRRefunded, reason: shift.ReasonQRObservationDifference},
		{name: "QR observation difference on cash is cross-dimension",
			dimension: shift.DimensionCash, reason: shift.ReasonQRObservationDifference, wantErr: true},
		{name: "unexplained is valid on every dimension",
			dimension: shift.DimensionCash, reason: shift.ReasonUnexplained},
		{name: "unexplained is valid on the second QR dimension",
			dimension: shift.DimensionManualQRRefunded, reason: shift.ReasonUnexplained},
		{name: "other with trimmed note",
			dimension: shift.DimensionCash, reason: shift.ReasonOther, note: strPtr("drawer seal broken")},
		{name: "other without note",
			dimension: shift.DimensionCash, reason: shift.ReasonOther, wantErr: true},
		{name: "non-other reason with a note",
			dimension: shift.DimensionCash, reason: shift.ReasonCashCountDifference,
			note: strPtr("spilled during the count"), wantErr: true},
		{name: "unexplained with a note",
			dimension: shift.DimensionManualQRReceived, reason: shift.ReasonUnexplained,
			note: strPtr("recounted twice"), wantErr: true},
		{name: "unknown reason",
			dimension: shift.DimensionCash, reason: "MYSTERY", wantErr: true},
		{name: "unknown dimension",
			dimension: "BAGS", reason: shift.ReasonUnexplained, wantErr: true},
		{name: "other note at the length limit",
			dimension: shift.DimensionManualQRRefunded, reason: shift.ReasonOther,
			note: strPtr(strings.Repeat("n", shift.MaxNoteLength))},
		{name: "other note over the length limit",
			dimension: shift.DimensionManualQRRefunded, reason: shift.ReasonOther,
			note: strPtr(strings.Repeat("n", shift.MaxNoteLength+1)), wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := shift.ValidateDiscrepancyReason(tc.dimension, tc.reason, tc.note)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestComputeDifference(t *testing.T) {
	// Difference is always observed - expected: a positive value is an excess,
	// a negative value a shortage (spec section 2).
	got, err := shift.ComputeDifference(100_000, 100_000)
	require.NoError(t, err)
	assert.Equal(t, int64(0), got, "an exact count has no difference")

	got, err = shift.ComputeDifference(110_000, 100_000)
	require.NoError(t, err)
	assert.Equal(t, int64(10_000), got, "an excess is positive")

	got, err = shift.ComputeDifference(90_000, 100_000)
	require.NoError(t, err)
	assert.Equal(t, int64(-10_000), got, "a shortage is negative")

	// observed - expected == MinInt64 - 1 wraps silently, so the guard must
	// reject it before the wrapped value can masquerade as a difference.
	_, err = shift.ComputeDifference(math.MinInt64, 1)
	require.ErrorIs(t, err, shift.ErrExpectedCashOutOfRange)

	// The mirror edge wraps in the other direction.
	_, err = shift.ComputeDifference(math.MaxInt64, -1)
	require.ErrorIs(t, err, shift.ErrExpectedCashOutOfRange)

	// The extreme in-range difference is representable and accepted.
	got, err = shift.ComputeDifference(math.MaxInt64, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(math.MaxInt64), got)
}

func TestComputeExpectedCashGuardsEveryArithmeticEdge(t *testing.T) {
	// Go int64 arithmetic wraps silently, so the guards must reject a wrapped
	// intermediate before the final bound check can be fooled by it.
	cases := []struct {
		name        string
		opening     int64
		cashPayment int64
		void        int64
		refund      int64
		payIn       int64
		payOut      int64
	}{
		{"opening plus cash payment overflows", math.MaxInt64, 1, 0, 0, 0, 0},
		{"opening plus cash payment underflows", math.MinInt64, -1, 0, 0, 0, 0},
		{"cash payment void underflows", 0, math.MinInt64, 1, 0, 0, 0},
		{"cash payment void overflows", 0, math.MaxInt64, -1, 0, 0, 0},
		{"cash refund underflows", 0, math.MinInt64, 0, 1, 0, 0},
		{"cash refund overflows", 0, math.MaxInt64, 0, -1, 0, 0},
		{"pay in overflows", math.MaxInt64 - 5, 5, 0, 0, 1, 0},
		{"pay in underflows", math.MinInt64 + 5, -5, 0, 0, -1, 0},
		{"pay out underflows", math.MinInt64 + 5, -5, 0, 0, 0, 1},
		{"pay out overflows", math.MaxInt64 - 5, 5, 0, 0, 0, -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := shift.ComputeExpectedCash(
				tc.opening, tc.cashPayment, tc.void, tc.refund, tc.payIn, tc.payOut)
			require.ErrorIs(t, err, shift.ErrExpectedCashOutOfRange)
		})
	}
}
