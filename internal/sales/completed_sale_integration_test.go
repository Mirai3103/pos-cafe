//go:build integration

package sales_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/preparation"
	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCompletedSaleReads(t *testing.T) {
	env := newSalesEnv(t)

	t.Run("reads by id and by service session agree", func(t *testing.T) {
		session := readyToClose(t, env, 2)
		closed := env.Close(t, session.ID)

		byID, _, err := env.GetCompletedSale(t, closed.ID)
		require.NoError(t, err)
		bySession, _, err := env.GetCompletedSaleBySession(t, session.ID)
		require.NoError(t, err)

		require.Equal(t, closed.ID, byID.ID)
		require.Equal(t, closed.ID, bySession.ID)
		require.Equal(t, byID, bySession)
	})

	t.Run("preparation history is in occurrence order", func(t *testing.T) {
		session := readyToClose(t, env, 1)
		sale := env.Close(t, session.ID)

		require.Len(t, sale.PreparationHistory, 3)
		require.Equal(t, sales.UnitStateQueued, sale.PreparationHistory[0].PriorState)
		require.Equal(t, sales.UnitStateInPreparation, sale.PreparationHistory[0].ResultingState)
		require.Equal(t, sales.UnitStateReady, sale.PreparationHistory[2].PriorState)
		require.Equal(t, sales.UnitStateFulfilled, sale.PreparationHistory[2].ResultingState)
		for _, transition := range sale.PreparationHistory {
			require.NotEqual(t, uuid.Nil, transition.ActorStaffID)
			require.NotEqual(t, uuid.Nil, transition.StaffSessionID)
		}
	})

	t.Run("an unknown sale is not found", func(t *testing.T) {
		_, _, err := env.GetCompletedSale(t, uuid.New())
		require.ErrorIs(t, err, sales.ErrCompletedSaleNotFound)
	})

	t.Run("an open session has no completed sale", func(t *testing.T) {
		session := env.StartTakeaway(t)
		_, _, err := env.GetCompletedSaleBySession(t, session.ID)
		require.ErrorIs(t, err, sales.ErrCompletedSaleNotFound)
	})
}

// completedSaleCore is the immutable core of a Completed Sale: the facts
// closure fixed. Marshaling it to JSON pins the wire bytes, so any post-sale
// correction that rewrote one of these fields would change the comparison.
type completedSaleCore struct {
	Checks             []sales.CompletedSaleCheckResponse    `json:"checks"`
	Orders             []sales.OrderResponse                 `json:"orders"`
	PreparationUnits   []sales.PreparationUnitResponse       `json:"preparation_units"`
	PreparationHistory []sales.PreparationTransitionResponse `json:"preparation_history"`
}

// TestCompletedSaleCoreRemainsImmutable closes a sale carrying every pre-close
// correction kind — Cancellation, Comp, Refund, and Payment Void — snapshots
// its core bytes, then appends a post-sale Comp and Refund and proves the core
// is byte-identical while only post_sale_corrections changed. It also proves
// the additive history is real business data: no zero UUID stands in for an
// unselected column, and a later correction exposes the sale's earlier ones.
func TestCompletedSaleCoreRemainsImmutable(t *testing.T) {
	env := newRefundEnv(t)
	ctx := context.Background()

	// Four charged units: one is cancelled, one is comped, and two are wasted
	// but left uncorrected as the post-sale Comp sources.
	session := env.commitDineInDraftWithQuantity(t, 4)
	checkID := env.soleCheckID(t, session.ID)
	session = env.Submit(t, session.ID)
	require.Len(t, session.PreparationUnits, 4)
	units := session.PreparationUnits

	// The wasted units are prepared first; unit zero stays QUEUED so the
	// Cancellation below is admissible.
	wasteIDs := make([]uuid.UUID, 0, 3)
	for _, unit := range units[1:] {
		wasteIDs = append(wasteIDs, env.wasteUnitAfterAdvance(t, unit.ID))
	}

	// Four Payments cover the base charge before any correction, so the
	// corrections create the excess the pre-close Refund resolves.
	for range 4 {
		_, status, err := env.payCash(t, checkID, 25000, 25000)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, status)
	}
	payments := env.paymentIDsForCheck(t, checkID)
	require.Len(t, payments, 4)

	// Pre-close Cancellation of the QUEUED unit: a LIVE_CHECK adjustment.
	_, cancelled, err := preparation.NewCancelUnitsHandler(
		preparation.NewRunner(env.DB, env.Queries)).
		Handle(ctx, prepActor(env.Actor), preparation.CancelUnitsCommand{
			RequestID:          uuid.New(),
			PreparationUnitIDs: []uuid.UUID{units[0].ID},
			Kind:               preparation.CancelKindCancellation,
			Reason:             preparation.ReasonCustomerRequest,
		})
	require.NoError(t, err)
	require.Len(t, cancelled.Outcomes, 1)
	require.NotNil(t, cancelled.Outcomes[0].ChargeAdjustmentID)

	// Pre-close Comp on the first wasted unit: a second LIVE_CHECK adjustment.
	preCloseComp := env.compOK(t, env.compCommand(t, wasteIDs[0], sales.CompReasonCafeError, nil))
	require.Equal(t, sales.CompScopeLiveCheck, preCloseComp.Scope)

	_, _, err = sales.NewVoidPaymentHandler(env.Runner).
		Handle(ctx, env.Actor, sales.VoidPaymentCommand{
			RequestID: uuid.New(),
			PaymentID: payments[0],
			Reason:    sales.VoidReasonWrongAmount,
			ManagerApproval: sales.ManagerApprovalInput{
				ApproverLoginCode: env.managerCode,
				ManagerPIN:        "1234",
			},
		})
	require.NoError(t, err)

	preCloseRefund := env.refundOK(t, sales.RecordRefundCommand{
		RequestID: uuid.New(),
		CheckID:   checkID,
		Method:    sales.RefundMethodCash,
		AdjustmentAllocations: []sales.RefundAdjustmentAllocationInput{
			{ChargeAdjustmentID: *cancelled.Outcomes[0].ChargeAdjustmentID, AmountVND: 25000},
		},
		PaymentAllocations: []sales.RefundPaymentAllocationInput{
			{PaymentID: payments[1], AmountVND: 25000},
		},
		Reason: sales.RefundReasonCustomerRequest,
		ManagerApproval: sales.ManagerApprovalInput{
			ApproverLoginCode: env.managerCode,
			ManagerPIN:        "1234",
		},
	})
	require.Equal(t, sales.RefundStateCompleted, preCloseRefund.Refund.State)
	require.Equal(t, sales.CompScopeLiveCheck, preCloseRefund.Scope)

	settled := env.compCheck(t, checkID)
	require.Equal(t, sales.CheckStateSettled, settled.State)
	require.EqualValues(t, 50000, settled.ChargeVND)

	sale := env.Close(t, session.ID)

	beforeCore, err := json.Marshal(completedSaleCore{
		Checks:             sale.Checks,
		Orders:             sale.Orders,
		PreparationUnits:   sale.PreparationUnits,
		PreparationHistory: sale.PreparationHistory,
	})
	require.NoError(t, err)
	beforeResponse, err := json.Marshal(sale)
	require.NoError(t, err)

	// Post-sale corrections: two Comps and one Refund, none of which may
	// rewrite the core snapshot.
	firstComp := env.compOK(t, env.compCommand(t, wasteIDs[1], sales.CompReasonCafeError, nil))
	require.Equal(t, sales.CompScopePostSale, firstComp.Scope)

	secondComp := env.compOK(t, env.compCommand(t, wasteIDs[2], sales.CompReasonCafeError, nil))
	require.Equal(t, sales.CompScopePostSale, secondComp.Scope)
	require.Len(t, secondComp.PostSaleCorrections, 2,
		"a later correction exposes the sale's earlier post-sale history")
	requireNoZeroCorrectionIDs(t, secondComp.PostSaleCorrections)

	findEntry := func(corrections []sales.PostSaleCorrectionResponse,
		adjustmentID uuid.UUID,
	) *sales.PostSaleCorrectionResponse {
		for i := range corrections {
			if corrections[i].Adjustment.ID == adjustmentID {
				return &corrections[i]
			}
		}
		return nil
	}
	require.NotNil(t, findEntry(secondComp.PostSaleCorrections,
		firstComp.Comp.ChargeAdjustmentID), "the earlier Comp is in the later response")

	postSaleRefund := env.refundOK(t, env.refundCommand(checkID, sales.RefundMethodCash,
		payments[2], firstComp.Comp.ChargeAdjustmentID, 25000))
	require.Equal(t, sales.CompScopePostSale, postSaleRefund.Scope)
	require.Len(t, postSaleRefund.PostSaleCorrections, 2,
		"a post-sale Refund exposes the sale's whole correction history")
	requireNoZeroCorrectionIDs(t, postSaleRefund.PostSaleCorrections)

	refundedEntry := findEntry(postSaleRefund.PostSaleCorrections,
		firstComp.Comp.ChargeAdjustmentID)
	require.NotNil(t, refundedEntry)
	require.Len(t, refundedEntry.Refunds, 1)
	require.Equal(t, postSaleRefund.Refund.ID, refundedEntry.Refunds[0].ID)
	require.Equal(t, sales.RefundStateCompleted, refundedEntry.Refunds[0].State)
	require.Zero(t, refundedEntry.Adjustment.RemainingRefundableVND)
	require.Zero(t, refundedEntry.OutstandingRefundVND)

	untouchedEntry := findEntry(postSaleRefund.PostSaleCorrections,
		secondComp.Comp.ChargeAdjustmentID)
	require.NotNil(t, untouchedEntry)
	require.Empty(t, untouchedEntry.Refunds)
	require.EqualValues(t, 25000, untouchedEntry.Adjustment.RemainingRefundableVND)
	require.EqualValues(t, 25000, untouchedEntry.OutstandingRefundVND)

	after, status, err := env.GetCompletedSale(t, sale.ID)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)

	afterCore, err := json.Marshal(completedSaleCore{
		Checks:             after.Checks,
		Orders:             after.Orders,
		PreparationUnits:   after.PreparationUnits,
		PreparationHistory: after.PreparationHistory,
	})
	require.NoError(t, err)
	require.JSONEq(t, string(beforeCore), string(afterCore),
		"post-sale corrections must not rewrite the immutable Completed Sale core")

	afterResponse, err := json.Marshal(after)
	require.NoError(t, err)
	beforeFields := map[string]json.RawMessage{}
	afterFields := map[string]json.RawMessage{}
	require.NoError(t, json.Unmarshal(beforeResponse, &beforeFields))
	require.NoError(t, json.Unmarshal(afterResponse, &afterFields))
	for field, value := range beforeFields {
		if field == "post_sale_corrections" {
			continue
		}
		require.JSONEq(t, string(value), string(afterFields[field]),
			"only post_sale_corrections may change after a post-sale correction")
	}
	require.Contains(t, afterFields, "post_sale_corrections",
		"the Completed Sale carries its additive history collection")
	require.NotEqual(t, "null", strings.TrimSpace(string(afterFields["post_sale_corrections"])))

	var wire struct {
		PostSaleCorrections []sales.PostSaleCorrectionResponse `json:"post_sale_corrections"`
	}
	require.NoError(t, json.Unmarshal(afterResponse, &wire))
	require.Len(t, wire.PostSaleCorrections, 2)
	requireNoZeroCorrectionIDs(t, wire.PostSaleCorrections)
}

// requireNoZeroCorrectionIDs proves a post-sale history entry carries real
// business ids in every field, including the ones the pre-Task-11 loader left
// as zero placeholders: the adjustment's allocation, shift, and sale ids.
func requireNoZeroCorrectionIDs(t *testing.T, corrections []sales.PostSaleCorrectionResponse) {
	t.Helper()
	for _, entry := range corrections {
		require.NotEqual(t, uuid.Nil, entry.Adjustment.ID)
		require.NotEqual(t, uuid.Nil, entry.Adjustment.ChargeAllocationID)
		require.NotEqual(t, uuid.Nil, entry.Adjustment.SalesShiftID)
		require.NotEqual(t, uuid.Nil, entry.Adjustment.PreparationUnitID)
		require.NotNil(t, entry.Adjustment.CompletedSaleID)
		require.NotEqual(t, uuid.Nil, *entry.Adjustment.CompletedSaleID)

		require.NotEqual(t, uuid.Nil, entry.Comp.ID)
		require.NotEqual(t, uuid.Nil, entry.Comp.ChargeAdjustmentID)
		require.NotEqual(t, uuid.Nil, entry.Comp.WasteID)
		require.NotEqual(t, uuid.Nil, entry.Comp.PreparationUnitID)
		require.NotEqual(t, uuid.Nil, entry.Comp.ActorStaffIdentityID)
		require.NotEqual(t, uuid.Nil, entry.Comp.ApprovedByStaffIdentityID)

		require.NotNil(t, entry.Refunds)
		for _, refund := range entry.Refunds {
			require.NotEqual(t, uuid.Nil, refund.ID)
			require.NotEqual(t, uuid.Nil, refund.CheckID)
			require.NotEqual(t, uuid.Nil, refund.SalesShiftID)
			require.NotNil(t, refund.CompletedSaleID)
			require.NotEqual(t, uuid.Nil, *refund.CompletedSaleID)
			require.NotEqual(t, uuid.Nil, refund.ActorStaffIdentityID)
			require.NotEqual(t, uuid.Nil, refund.ApprovedByStaffIdentityID)
			require.NotNil(t, refund.PaymentAllocations)
			require.NotNil(t, refund.AdjustmentAllocations)
			for _, allocation := range refund.PaymentAllocations {
				require.NotEqual(t, uuid.Nil, allocation.ID)
			}
			for _, allocation := range refund.AdjustmentAllocations {
				require.NotEqual(t, uuid.Nil, allocation.ID)
			}
			require.NotNil(t, refund.Completion)
			require.NotEqual(t, uuid.Nil, refund.Completion.ID)
			require.NotEqual(t, uuid.Nil, refund.Completion.CompletedByStaffIdentityID)
			require.NotEqual(t, uuid.Nil, refund.Completion.CompletedStaffAccessSessionID)
		}
	}
}
