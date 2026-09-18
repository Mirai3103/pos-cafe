package sales

import (
	"math"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The live Check equation, design section 6.1:
//
//	charge             = base charge - live adjustments
//	valid payment      = original payments - voided payments
//	effective received = valid payment - completed live refunds
//	balance            = max(charge - effective received, 0)
//	pending refund     = max(effective received - charge, 0)
//
// Its inputs are persisted sums, so every rejection is a defect rather than a
// business state: ErrFinancialInvariantViolated surfaces as a logged 500.
func TestComputeCheckFinancialsBaseCharge(t *testing.T) {
	got, err := ComputeCheckFinancials(CheckFinancialInputs{BaseChargeVND: 85_000})
	require.NoError(t, err)
	assert.Equal(t, CheckFinancials{
		ChargeVND:            85_000,
		ValidPaymentVND:      0,
		EffectiveReceivedVND: 0,
		BalanceVND:           85_000,
		PendingRefundVND:     0,
	}, got)
}

func TestComputeCheckFinancialsAdjustedCharge(t *testing.T) {
	got, err := ComputeCheckFinancials(CheckFinancialInputs{
		BaseChargeVND:     85_000,
		LiveAdjustmentVND: 25_000,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(60_000), got.ChargeVND,
		"a live adjustment reduces the charge without touching the base")
	assert.Equal(t, int64(60_000), got.BalanceVND)
}

func TestComputeCheckFinancialsValidPaymentSubtractsVoids(t *testing.T) {
	got, err := ComputeCheckFinancials(CheckFinancialInputs{
		BaseChargeVND:      85_000,
		OriginalPaymentVND: 60_000,
		VoidedPaymentVND:   10_000,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(50_000), got.ValidPaymentVND)
	assert.Equal(t, int64(50_000), got.EffectiveReceivedVND)
	assert.Equal(t, int64(35_000), got.BalanceVND)
	assert.Equal(t, int64(0), got.PendingRefundVND)
}

func TestComputeCheckFinancialsCompletedRefundReducesEffectiveReceipt(t *testing.T) {
	got, err := ComputeCheckFinancials(CheckFinancialInputs{
		BaseChargeVND:      85_000,
		OriginalPaymentVND: 85_000,
		CompletedRefundVND: 25_000,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(85_000), got.ValidPaymentVND)
	assert.Equal(t, int64(60_000), got.EffectiveReceivedVND)
	assert.Equal(t, int64(25_000), got.BalanceVND)
	assert.Equal(t, int64(0), got.PendingRefundVND)
}

// A pending Manual QR Refund is not completed money movement, so it is not an
// input: effective receipt still exceeds the charge and the excess stays owed
// back until staff confirm the outbound transfer.
func TestComputeCheckFinancialsPendingRefundDoesNotReduceReceipt(t *testing.T) {
	got, err := ComputeCheckFinancials(CheckFinancialInputs{
		BaseChargeVND:      85_000,
		LiveAdjustmentVND:  35_000,
		OriginalPaymentVND: 85_000,
		CompletedRefundVND: 0,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(50_000), got.ChargeVND)
	assert.Equal(t, int64(85_000), got.EffectiveReceivedVND)
	assert.Equal(t, int64(0), got.BalanceVND)
	assert.Equal(t, int64(35_000), got.PendingRefundVND)
}

// A completed Refund that resolves the excess clears the pending obligation
// and leaves the Check settled with a zero balance.
func TestComputeCheckFinancialsCompletedRefundResolvesPendingRefund(t *testing.T) {
	got, err := ComputeCheckFinancials(CheckFinancialInputs{
		BaseChargeVND:      85_000,
		LiveAdjustmentVND:  35_000,
		OriginalPaymentVND: 85_000,
		CompletedRefundVND: 35_000,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(0), got.BalanceVND)
	assert.Equal(t, int64(0), got.PendingRefundVND)
}

// A zero-charge Check whose money has not left yet owes the whole receipt.
func TestComputeCheckFinancialsZeroChargeOwesWholeReceipt(t *testing.T) {
	got, err := ComputeCheckFinancials(CheckFinancialInputs{
		BaseChargeVND:      25_000,
		LiveAdjustmentVND:  25_000,
		OriginalPaymentVND: 25_000,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(0), got.ChargeVND)
	assert.Equal(t, int64(25_000), got.EffectiveReceivedVND)
	assert.Equal(t, int64(0), got.BalanceVND)
	assert.Equal(t, int64(25_000), got.PendingRefundVND)
}

func TestComputeCheckFinancialsRejectsNegativeEffectiveReceipt(t *testing.T) {
	_, err := ComputeCheckFinancials(CheckFinancialInputs{
		BaseChargeVND:      85_000,
		OriginalPaymentVND: 10_000,
		CompletedRefundVND: 20_000,
	})
	require.ErrorIs(t, err, ErrFinancialInvariantViolated,
		"more money cannot have been refunded than was validly received")
}

func TestComputeCheckFinancialsRejectsUnderflowingInputs(t *testing.T) {
	cases := map[string]CheckFinancialInputs{
		"live adjustments above the base charge": {
			BaseChargeVND:     25_000,
			LiveAdjustmentVND: 30_000,
		},
		"voids above the original payments": {
			BaseChargeVND:      25_000,
			OriginalPaymentVND: 10_000,
			VoidedPaymentVND:   15_000,
		},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ComputeCheckFinancials(in)
			require.ErrorIs(t, err, ErrFinancialInvariantViolated)
		})
	}
}

func TestComputeCheckFinancialsRejectsNegativeInputs(t *testing.T) {
	cases := map[string]CheckFinancialInputs{
		"negative base charge":      {BaseChargeVND: -1},
		"negative live adjustment":  {BaseChargeVND: 1, LiveAdjustmentVND: -1},
		"negative original payment": {BaseChargeVND: 1, OriginalPaymentVND: -1},
		"negative voided payment":   {BaseChargeVND: 1, OriginalPaymentVND: 1, VoidedPaymentVND: -1},
		"negative completed refund": {BaseChargeVND: 1, OriginalPaymentVND: 1, CompletedRefundVND: -1},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ComputeCheckFinancials(in)
			require.ErrorIs(t, err, ErrFinancialInvariantViolated)
		})
	}
}

// int64's own bounds are not a business ceiling, but the guarded subtractions
// must not wrap at them either.
func TestComputeCheckFinancialsBoundaryAmounts(t *testing.T) {
	t.Run("the maximum amount survives both subtractions", func(t *testing.T) {
		got, err := ComputeCheckFinancials(CheckFinancialInputs{
			BaseChargeVND:      math.MaxInt64,
			OriginalPaymentVND: math.MaxInt64,
		})
		require.NoError(t, err)
		assert.Equal(t, int64(math.MaxInt64), got.ChargeVND)
		assert.Equal(t, int64(math.MaxInt64), got.EffectiveReceivedVND)
		assert.Equal(t, int64(0), got.BalanceVND)
		assert.Equal(t, int64(0), got.PendingRefundVND)
	})

	t.Run("a zero charge keeps the maximum receipt owed", func(t *testing.T) {
		got, err := ComputeCheckFinancials(CheckFinancialInputs{
			BaseChargeVND:      0,
			OriginalPaymentVND: math.MaxInt64,
		})
		require.NoError(t, err)
		assert.Equal(t, int64(math.MaxInt64), got.PendingRefundVND)
	})
}

// Remaining capacity is a source amount less every Refund allocation against
// it: pending Manual QR intents reserve capacity exactly as completed Refunds
// do, and the two snapshot modes differ only in post-sale membership.
func TestRemainingRefundableVND(t *testing.T) {
	t.Run("every allocation reserves capacity", func(t *testing.T) {
		remaining, err := remainingRefundableVND(25_000, []sourceAllocation{
			{AmountVND: 10_000},
			{AmountVND: 5_000},
		}, SnapshotLive)
		require.NoError(t, err)
		assert.Equal(t, int64(10_000), remaining)
	})

	t.Run("no allocations leaves the whole amount", func(t *testing.T) {
		remaining, err := remainingRefundableVND(25_000, nil, SnapshotLive)
		require.NoError(t, err)
		assert.Equal(t, int64(25_000), remaining)
	})

	t.Run("the core snapshot ignores post-sale allocations", func(t *testing.T) {
		postSale := uuid.New()
		allocations := []sourceAllocation{
			{AmountVND: 10_000},
			{AmountVND: 5_000, RefundCompletedSaleID: uuid.NullUUID{UUID: postSale, Valid: true}},
		}

		live, err := remainingRefundableVND(25_000, allocations, SnapshotLive)
		require.NoError(t, err)
		assert.Equal(t, int64(10_000), live,
			"a post-sale Refund consumed the live source's capacity")

		core, err := remainingRefundableVND(25_000, allocations, SnapshotCompletedSaleCore)
		require.NoError(t, err)
		assert.Equal(t, int64(15_000), core,
			"historical capacity excludes additive post-sale Refunds")
	})

	t.Run("allocations above the source amount are an invariant failure", func(t *testing.T) {
		_, err := remainingRefundableVND(25_000, []sourceAllocation{
			{AmountVND: 20_000},
			{AmountVND: 10_000},
		}, SnapshotLive)
		require.ErrorIs(t, err, ErrFinancialInvariantViolated)
	})
}
