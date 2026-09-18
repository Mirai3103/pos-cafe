package sales

import (
	"fmt"

	"github.com/google/uuid"
)

// SnapshotMode selects which structural boundary a Check projection belongs
// to. Membership is decided by completed_sale_id, never by a clock: a
// correction that observed a closed Service Session carries its Completed Sale
// id, and one that observed an active Session does not, so the two never mix
// regardless of application or database time.
type SnapshotMode uint8

const (
	// SnapshotLive is the live Check projection: the current facts and every
	// Refund allocation that has consumed refundable capacity, post-sale
	// included, because that capacity is spent.
	SnapshotLive SnapshotMode = iota
	// SnapshotCompletedSaleCore is the immutable Completed Sale projection:
	// LIVE_CHECK adjustments and live Refunds only, and post-sale Refund
	// allocations never reduce historical refundable capacity.
	SnapshotCompletedSaleCore
)

// CheckFinancialInputs are the persisted sums the live Check equation is
// derived from. They are evidence, never authority: the caller reads them from
// append-only facts and the equation decides what must be true. Design section
// 6.1.
type CheckFinancialInputs struct {
	BaseChargeVND      int64
	LiveAdjustmentVND  int64
	OriginalPaymentVND int64
	VoidedPaymentVND   int64
	CompletedRefundVND int64
}

// CheckFinancials is the live financial truth of one Check.
type CheckFinancials struct {
	// ChargeVND is the base charge less live adjustments.
	ChargeVND int64
	// ValidPaymentVND is the original Payment sum less voided Payments.
	ValidPaymentVND int64
	// EffectiveReceivedVND is valid receipt less completed live Refunds. Money
	// that has not moved — a pending Manual QR Refund — is not subtracted.
	EffectiveReceivedVND int64
	// BalanceVND is what the customer still owes.
	BalanceVND int64
	// PendingRefundVND is money the system still owes back: valid receipt in
	// excess of the corrected charge.
	PendingRefundVND int64
}

// ComputeCheckFinancials derives the live Check equation:
//
//	charge             = base charge - live adjustments
//	valid payment      = original payments - voided payments
//	effective received = valid payment - completed live refunds
//	balance            = max(charge - effective received, 0)
//	pending refund     = max(effective received - charge, 0)
//
// Every input is a persisted sum, so an underflow or a negative value is a
// corrupt stored result rather than a client conflict, and is rejected with
// ErrFinancialInvariantViolated.
func ComputeCheckFinancials(in CheckFinancialInputs) (CheckFinancials, error) {
	var zero CheckFinancials

	switch {
	case in.BaseChargeVND < 0:
		return zero, fmt.Errorf("%w: base charge %d is negative",
			ErrFinancialInvariantViolated, in.BaseChargeVND)
	case in.LiveAdjustmentVND < 0:
		return zero, fmt.Errorf("%w: live adjustments %d are negative",
			ErrFinancialInvariantViolated, in.LiveAdjustmentVND)
	case in.LiveAdjustmentVND > in.BaseChargeVND:
		return zero, fmt.Errorf("%w: live adjustments %d exceed base charge %d",
			ErrFinancialInvariantViolated, in.LiveAdjustmentVND, in.BaseChargeVND)
	case in.OriginalPaymentVND < 0:
		return zero, fmt.Errorf("%w: original payments %d are negative",
			ErrFinancialInvariantViolated, in.OriginalPaymentVND)
	case in.VoidedPaymentVND < 0:
		return zero, fmt.Errorf("%w: voided payments %d are negative",
			ErrFinancialInvariantViolated, in.VoidedPaymentVND)
	case in.VoidedPaymentVND > in.OriginalPaymentVND:
		return zero, fmt.Errorf("%w: voided payments %d exceed original payments %d",
			ErrFinancialInvariantViolated, in.VoidedPaymentVND, in.OriginalPaymentVND)
	case in.CompletedRefundVND < 0:
		return zero, fmt.Errorf("%w: completed refunds %d are negative",
			ErrFinancialInvariantViolated, in.CompletedRefundVND)
	}

	chargeVND := in.BaseChargeVND - in.LiveAdjustmentVND
	validPaymentVND := in.OriginalPaymentVND - in.VoidedPaymentVND
	if in.CompletedRefundVND > validPaymentVND {
		return zero, fmt.Errorf("%w: completed refunds %d exceed valid receipt %d",
			ErrFinancialInvariantViolated, in.CompletedRefundVND, validPaymentVND)
	}
	effectiveReceivedVND := validPaymentVND - in.CompletedRefundVND

	out := CheckFinancials{
		ChargeVND:            chargeVND,
		ValidPaymentVND:      validPaymentVND,
		EffectiveReceivedVND: effectiveReceivedVND,
	}
	if chargeVND > effectiveReceivedVND {
		out.BalanceVND = chargeVND - effectiveReceivedVND
	} else {
		out.PendingRefundVND = effectiveReceivedVND - chargeVND
	}
	return out, nil
}

// sourceAllocation is one Refund allocation against a Payment or a Charge
// Adjustment. A completed-sale allocation carries its Completed Sale id,
// which is what the structural snapshot rule filters on; a pending or live
// allocation leaves it null.
type sourceAllocation struct {
	AmountVND             int64
	RefundCompletedSaleID uuid.NullUUID
}

// remainingRefundableVND is one source's unallocated capacity: its amount less
// every Refund allocation against it. Pending Manual QR intents reserve
// capacity exactly as completed Refunds do. In the Completed Sale core
// snapshot, allocations belonging to post-sale Refunds are additive history
// and never consume the historical source.
func remainingRefundableVND(amountVND int64, allocations []sourceAllocation,
	mode SnapshotMode,
) (int64, error) {
	if amountVND < 0 {
		return 0, fmt.Errorf("%w: source amount %d is negative",
			ErrFinancialInvariantViolated, amountVND)
	}

	var consumedVND int64
	for _, allocation := range allocations {
		if mode == SnapshotCompletedSaleCore && allocation.RefundCompletedSaleID.Valid {
			continue
		}
		next, err := AddCharge(consumedVND, allocation.AmountVND)
		if err != nil {
			return 0, fmt.Errorf("%w: refund allocation sum: %v",
				ErrFinancialInvariantViolated, err)
		}
		consumedVND = next
	}
	if consumedVND > amountVND {
		return 0, fmt.Errorf("%w: refund allocations %d exceed source amount %d",
			ErrFinancialInvariantViolated, consumedVND, amountVND)
	}
	return amountVND - consumedVND, nil
}
