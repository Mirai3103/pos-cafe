package sales_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// eligible builds a Service Session projection that satisfies all four
// closure conditions. Each test below breaks exactly one of them, so a
// failing assertion names the condition that actually regressed.
func eligible() sales.ServiceSessionResponse {
	itemID := uuid.New()
	return sales.ServiceSessionResponse{
		Checks: []sales.CheckResponse{{
			ID:    uuid.New(),
			State: sales.CheckStateSettled,
			Allocations: []sales.ChargeAllocationResponse{{
				ID: uuid.New(), CommittedItemID: itemID, Submitted: true,
			}},
		}},
		Orders:           []sales.OrderResponse{{ID: uuid.New()}},
		PreparationUnits: []sales.PreparationUnitResponse{{ID: uuid.New(), State: sales.UnitStateFulfilled}},
	}
}

func TestEvaluateClosureReadinessEligible(t *testing.T) {
	got := sales.EvaluateClosureReadiness(eligible())
	require.True(t, got.Eligible)
	require.NoError(t, got.Err())
}

func TestEvaluateClosureReadinessUnsettledCheck(t *testing.T) {
	s := eligible()
	s.Checks[0].State = sales.CheckStateOpen
	got := sales.EvaluateClosureReadiness(s)
	require.False(t, got.Eligible)
	require.Len(t, got.UnsettledCheckIDs, 1)
	require.ErrorIs(t, got.Err(), sales.ErrCheckNotSettledForClosure)
}

func TestEvaluateClosureReadinessMergedCheckIsNotUnsettled(t *testing.T) {
	// A merged Check was absorbed into another and owes nothing.
	s := eligible()
	s.Checks = append(s.Checks, sales.CheckResponse{ID: uuid.New(), State: sales.CheckStateMerged})
	got := sales.EvaluateClosureReadiness(s)
	require.True(t, got.Eligible)
}

func TestEvaluateClosureReadinessAllChecksMerged(t *testing.T) {
	// A Session whose every Check was merged away has no surviving Check and
	// has settled nothing, so it is not eligible even though nothing is OPEN.
	s := eligible()
	s.Checks = []sales.CheckResponse{{ID: uuid.New(), State: sales.CheckStateMerged}}
	got := sales.EvaluateClosureReadiness(s)
	require.False(t, got.Eligible)
	require.ErrorIs(t, got.Err(), sales.ErrCheckNotSettledForClosure)
}

func TestEvaluateClosureReadinessNoChecks(t *testing.T) {
	s := eligible()
	s.Checks = nil
	got := sales.EvaluateClosureReadiness(s)
	require.False(t, got.Eligible)
	require.ErrorIs(t, got.Err(), sales.ErrCheckNotSettledForClosure)
}

func TestClosureReadinessPendingRefund(t *testing.T) {
	// A fully settled Check that still owes money back must hold the Session
	// open: Check state records covered customer debt, while the pending Refund
	// is a separate obligation. The Check id travels with the verdict so the
	// API can say which Check holds the Session.
	s := eligible()
	s.Checks[0].PendingRefundVND = 25_000

	got := sales.EvaluateClosureReadiness(s)
	require.False(t, got.Eligible, "money owed back must block closure")
	require.False(t, got.AllRefundsResolved)
	require.Equal(t, []uuid.UUID{s.Checks[0].ID}, got.PendingRefundCheckIDs)
	require.ErrorIs(t, got.Err(), sales.ErrPendingRefundForClosure)
}

func TestClosureReadinessNoPendingRefundIsEmpty(t *testing.T) {
	got := sales.EvaluateClosureReadiness(eligible())
	require.True(t, got.AllRefundsResolved)
	require.NotNil(t, got.PendingRefundCheckIDs, "the collection must be non-null")
	require.Empty(t, got.PendingRefundCheckIDs)
}

func TestClosurePendingRefundErrorMapping(t *testing.T) {
	var coded *response.CodedError
	require.ErrorAs(t, sales.MapHTTPError(
		fmt.Errorf("wrapped: %w", sales.ErrPendingRefundForClosure)), &coded)
	require.Equal(t, http.StatusConflict, coded.Status)
	require.Equal(t, "PENDING_REFUND_FOR_CLOSURE", coded.Code)
}

func TestClosureReadinessPrecedence(t *testing.T) {
	// Staff fix what they are told about first, so the order decides which of
	// several outstanding problems they are sent to resolve: unsettled money,
	// then money owed back, then unsubmitted work, then the missing Order,
	// then the bar.
	s := eligible()
	s.Checks[0].State = sales.CheckStateOpen
	s.Checks[0].PendingRefundVND = 25_000
	s.Checks[0].Allocations[0].Submitted = false
	s.Orders = nil
	s.PreparationUnits[0].State = sales.UnitStateQueued

	got := sales.EvaluateClosureReadiness(s)
	require.ErrorIs(t, got.Err(), sales.ErrCheckNotSettledForClosure)
	require.Len(t, got.UnsettledCheckIDs, 1)
	require.Equal(t, []uuid.UUID{s.Checks[0].ID}, got.PendingRefundCheckIDs,
		"a pending Refund is still reported even while an unsettled Check outranks it")

	s.Checks[0].State = sales.CheckStateSettled
	require.ErrorIs(t, sales.EvaluateClosureReadiness(s).Err(), sales.ErrPendingRefundForClosure)

	s.Checks[0].PendingRefundVND = 0
	require.ErrorIs(t, sales.EvaluateClosureReadiness(s).Err(), sales.ErrUnsubmittedWorkForClosure)

	s.Checks[0].Allocations[0].Submitted = true
	require.ErrorIs(t, sales.EvaluateClosureReadiness(s).Err(), sales.ErrOrderRequiredForClosure)

	s.Orders = []sales.OrderResponse{{ID: uuid.New()}}
	require.ErrorIs(t, sales.EvaluateClosureReadiness(s).Err(), sales.ErrUnfulfilledPreparationForClosure)

	s.PreparationUnits[0].State = sales.UnitStateFulfilled
	require.NoError(t, sales.EvaluateClosureReadiness(s).Err(), "a fully resolved Session may close")
}

func TestEvaluateClosureReadinessUnsubmittedWork(t *testing.T) {
	s := eligible()
	s.Checks[0].Allocations[0].Submitted = false
	got := sales.EvaluateClosureReadiness(s)
	require.False(t, got.Eligible)
	require.Len(t, got.UnsubmittedCommittedItemIDs, 1)
	require.ErrorIs(t, got.Err(), sales.ErrUnsubmittedWorkForClosure)
}

func TestEvaluateClosureReadinessNoOrder(t *testing.T) {
	s := eligible()
	s.Orders = nil
	got := sales.EvaluateClosureReadiness(s)
	require.False(t, got.Eligible)
	require.ErrorIs(t, got.Err(), sales.ErrOrderRequiredForClosure)
}

func TestEvaluateClosureReadinessNonterminalUnit(t *testing.T) {
	for _, state := range []string{sales.UnitStateQueued, sales.UnitStateInPreparation, sales.UnitStateReady} {
		t.Run(state, func(t *testing.T) {
			s := eligible()
			s.PreparationUnits[0].State = state
			got := sales.EvaluateClosureReadiness(s)
			require.False(t, got.Eligible)
			require.Len(t, got.NonterminalUnitIDs, 1)
			require.ErrorIs(t, got.Err(), sales.ErrUnfulfilledPreparationForClosure)
		})
	}
}

func TestEvaluateClosureReadinessCancelledAndWastedAreTerminal(t *testing.T) {
	for _, state := range []string{sales.UnitStateCancelled, sales.UnitStateWasted} {
		t.Run(state, func(t *testing.T) {
			s := eligible()
			s.PreparationUnits[0].State = state
			require.True(t, sales.EvaluateClosureReadiness(s).Eligible)
		})
	}
}

func TestEvaluateClosureReadinessRejectionOrder(t *testing.T) {
	// Staff fix what they are told about first, so the order decides which of
	// several outstanding problems they are sent to resolve: money, then
	// work, then the bar.
	s := eligible()
	s.Checks[0].State = sales.CheckStateOpen
	s.Checks[0].Allocations[0].Submitted = false
	s.Orders = nil
	s.PreparationUnits[0].State = sales.UnitStateQueued

	got := sales.EvaluateClosureReadiness(s)
	require.ErrorIs(t, got.Err(), sales.ErrCheckNotSettledForClosure)

	s.Checks[0].State = sales.CheckStateSettled
	require.ErrorIs(t, sales.EvaluateClosureReadiness(s).Err(), sales.ErrUnsubmittedWorkForClosure)

	s.Checks[0].Allocations[0].Submitted = true
	require.ErrorIs(t, sales.EvaluateClosureReadiness(s).Err(), sales.ErrOrderRequiredForClosure)

	s.Orders = []sales.OrderResponse{{ID: uuid.New()}}
	require.ErrorIs(t, sales.EvaluateClosureReadiness(s).Err(), sales.ErrUnfulfilledPreparationForClosure)
}
