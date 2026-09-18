//go:build integration

package sales_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/preparation"
	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// readyToClose runs the complete path: commit, pay, submit, fulfill.
func readyToClose(t *testing.T, env *salesEnv, quantity int32) sales.ServiceSessionResponse {
	t.Helper()
	session := env.commitDineInDraftWithQuantity(t, quantity)
	checkID := env.soleCheckID(t, session.ID)
	charge := env.checkCharge(t, checkID)
	_, _, err := env.payCash(t, checkID, charge, charge)
	require.NoError(t, err)
	env.Submit(t, session.ID)
	env.FulfillAll(t, session.ID)
	return session
}

func TestCloseServiceSession(t *testing.T) {
	env := newSalesEnv(t)
	ctx := context.Background()

	t.Run("an eligible session closes into a completed sale", func(t *testing.T) {
		session := readyToClose(t, env, 2)

		sale, status, err := env.TryClose(t, session.ID)
		require.NoError(t, err)
		require.Equal(t, 201, status)

		require.Equal(t, sales.CompletedSaleStateCompleted, sale.State)
		require.Equal(t, session.ID, sale.ServiceSessionID)
		require.Equal(t, sales.StateClosed, sale.ServiceSessionState)
		require.Equal(t, env.Actor.StaffID, sale.CompletedByStaffID)
		require.NotEmpty(t, sale.CompletedByName)
		require.Len(t, sale.Checks, 1)
		require.Equal(t, int64(0), sale.Checks[0].BalanceVND)
		require.Len(t, sale.Orders, 1)
		require.Len(t, sale.PreparationUnits, 2)
		require.Len(t, sale.PreparationHistory, 6, "three advances per unit")

		var state string
		require.NoError(t, env.DB.QueryRowContext(ctx,
			`SELECT state FROM service_sessions WHERE id = $1`, session.ID).Scan(&state))
		require.Equal(t, sales.StateClosed, state)
	})

	t.Run("closing releases every held table assignment", func(t *testing.T) {
		session := readyToClose(t, env, 1)

		env.Close(t, session.ID)

		var held int
		require.NoError(t, env.DB.QueryRowContext(ctx, `
			SELECT count(*) FROM table_assignments
			WHERE service_session_id = $1 AND released_at IS NULL`, session.ID).Scan(&held))
		require.Equal(t, 0, held)
	})

	t.Run("closing twice returns the same completed sale", func(t *testing.T) {
		session := readyToClose(t, env, 1)

		first := env.Close(t, session.ID)
		second := env.Close(t, session.ID)
		require.Equal(t, first.ID, second.ID)

		var n int
		require.NoError(t, env.DB.QueryRowContext(ctx,
			`SELECT count(*) FROM completed_sales WHERE service_session_id = $1`,
			session.ID).Scan(&n))
		require.Equal(t, 1, n)
	})

	t.Run("a replayed request id returns the stored result", func(t *testing.T) {
		session := readyToClose(t, env, 1)
		requestID := uuid.New()

		first, _, err := env.CloseWithRequestID(t, requestID, session.ID)
		require.NoError(t, err)
		second, _, err := env.CloseWithRequestID(t, requestID, session.ID)
		require.NoError(t, err)
		require.Equal(t, first.ID, second.ID)
	})

	t.Run("an unknown session is not found", func(t *testing.T) {
		_, _, err := env.TryClose(t, uuid.New())
		require.ErrorIs(t, err, sales.ErrServiceSessionNotFound)
	})

	t.Run("a barista cannot close", func(t *testing.T) {
		session := readyToClose(t, env, 1)
		_, _, err := env.CloseAs(t, env.BaristaActor(), session.ID)
		require.ErrorIs(t, err, sales.ErrForbidden)
	})
}

func TestCloseServiceSessionRejections(t *testing.T) {
	env := newSalesEnv(t)

	// Each subtest satisfies every higher-priority condition, so the ordering
	// in ClosureReadiness.Err is genuinely exercised rather than shadowed.

	t.Run("an unsettled check is refused first", func(t *testing.T) {
		session := env.commitDineInDraftWithQuantity(t, 1)
		env.Submit(t, session.ID)
		env.FulfillAll(t, session.ID)

		_, _, err := env.TryClose(t, session.ID)
		require.ErrorIs(t, err, sales.ErrCheckNotSettledForClosure)
	})

	t.Run("unsubmitted work is refused once checks are settled", func(t *testing.T) {
		session := env.commitDineInDraftWithQuantity(t, 1)
		checkID := env.soleCheckID(t, session.ID)
		charge := env.checkCharge(t, checkID)
		_, _, err := env.payCash(t, checkID, charge, charge)
		require.NoError(t, err)

		_, _, err = env.TryClose(t, session.ID)
		require.ErrorIs(t, err, sales.ErrUnsubmittedWorkForClosure)
	})

	t.Run("nonterminal preparation is refused last", func(t *testing.T) {
		session := env.commitDineInDraftWithQuantity(t, 1)
		checkID := env.soleCheckID(t, session.ID)
		charge := env.checkCharge(t, checkID)
		_, _, err := env.payCash(t, checkID, charge, charge)
		require.NoError(t, err)
		env.Submit(t, session.ID)

		_, _, err = env.TryClose(t, session.ID)
		require.ErrorIs(t, err, sales.ErrUnfulfilledPreparationForClosure)
	})

	t.Run("a session with no order at all is refused", func(t *testing.T) {
		// A Session that has never committed has no Check either, so the
		// money condition fires first — this asserts the ordering holds even
		// when the missing Order is the more obvious problem.
		session := env.StartDineIn(t, env.TableID)

		_, _, err := env.TryClose(t, session.ID)
		require.ErrorIs(t, err, sales.ErrCheckNotSettledForClosure)
	})

	t.Run("a refused closure records nothing", func(t *testing.T) {
		session := env.commitDineInDraftWithQuantity(t, 1)

		_, _, err := env.TryClose(t, session.ID)
		require.Error(t, err)

		got := env.GetSessionOK(t, session.ID)
		require.Equal(t, sales.StateActive, got.State)
	})
}

// settledDineInOwedBack pays one Dine-in unit in full with the given method,
// submits it, wastes it, and Comps the Waste: the Check stays SETTLED and owes
// its whole paid amount back. Payment and Refund methods must match, so the
// method decides how the test resolves the obligation.
func settledDineInOwedBack(t *testing.T, env *refundEnv, method string) (
	sales.ServiceSessionResponse, uuid.UUID, sales.CompResult,
) {
	t.Helper()
	session := env.commitDineInDraftWithQuantity(t, 1)
	checkID := env.soleCheckID(t, session.ID)
	charge := env.checkCharge(t, checkID)

	var status int
	var err error
	switch method {
	case sales.PaymentMethodManualQR:
		_, status, err = env.payManualQR(t, checkID, charge, true, nil)
	default:
		_, status, err = env.payCash(t, checkID, charge, charge)
	}
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)

	session = env.Submit(t, session.ID)
	require.Len(t, session.PreparationUnits, 1)
	unit := session.PreparationUnits[0]
	env.advanceToReady(t, unit.ID)
	wasteID := env.wasteUnit(t, unit.ID)

	comp := env.compOK(t, env.compCommand(t, wasteID, sales.CompReasonCafeError, nil))
	return session, checkID, comp
}

// TestCloseServiceSessionPendingRefund proves closure is what enforces money
// owed back: a settled Check carrying a pending Refund holds the Session open
// until the Refund is completed — Cash immediately, Manual QR only by
// confirmation — and a refused closure records neither a Completed Sale nor an
// idempotent success result.
func TestCloseServiceSessionPendingRefund(t *testing.T) {
	env := newRefundEnv(t)
	ctx := context.Background()

	t.Run("a settled check with a pending refund is refused", func(t *testing.T) {
		session, checkID, comp := settledDineInOwedBack(t, env, sales.PaymentMethodCash)

		readiness := sales.EvaluateClosureReadiness(env.GetSessionOK(t, session.ID))
		require.False(t, readiness.Eligible)
		require.False(t, readiness.AllRefundsResolved)
		require.Equal(t, []uuid.UUID{checkID}, readiness.PendingRefundCheckIDs)
		require.ErrorIs(t, readiness.Err(), sales.ErrPendingRefundForClosure)

		requestID := uuid.New()
		_, status, err := env.CloseWithRequestID(t, requestID, session.ID)
		require.ErrorIs(t, err, sales.ErrPendingRefundForClosure)
		require.Equal(t, http.StatusConflict, status)

		got := env.GetSessionOK(t, session.ID)
		require.Equal(t, sales.StateActive, got.State, "a refused closure records nothing")
		require.EqualValues(t, 25_000, env.findCheck(t, got, checkID).PendingRefundVND)

		var completedSales int
		require.NoError(t, env.DB.QueryRowContext(ctx,
			`SELECT count(*) FROM completed_sales WHERE service_session_id = $1`,
			session.ID).Scan(&completedSales))
		require.Zero(t, completedSales, "a refused closure writes no Completed Sale")
		require.Zero(t, env.idempotencyClaimCount(t, env.Actor, requestID),
			"a refused closure stores no idempotent success result")

		// The refusal left no claim behind: resolving the Refund and reusing
		// the same request id closes normally.
		env.refundOK(t, env.refundCommand(checkID, sales.RefundMethodCash,
			env.solePaymentIDForCheck(t, checkID), comp.Comp.ChargeAdjustmentID, 25_000))
		sale, status, err := env.CloseWithRequestID(t, requestID, session.ID)
		require.NoError(t, err)
		require.Equal(t, http.StatusCreated, status)
		require.Equal(t, session.ID, sale.ServiceSessionID)
	})

	t.Run("a completed cash refund permits closure", func(t *testing.T) {
		session, checkID, comp := settledDineInOwedBack(t, env, sales.PaymentMethodCash)
		env.refundOK(t, env.refundCommand(checkID, sales.RefundMethodCash,
			env.solePaymentIDForCheck(t, checkID), comp.Comp.ChargeAdjustmentID, 25_000))

		got := env.GetSessionOK(t, session.ID)
		require.True(t, sales.EvaluateClosureReadiness(got).AllRefundsResolved)
		require.Zero(t, env.findCheck(t, got, checkID).PendingRefundVND)

		sale, status, err := env.TryClose(t, session.ID)
		require.NoError(t, err)
		require.Equal(t, http.StatusCreated, status)
		require.Equal(t, session.ID, sale.ServiceSessionID)
	})

	t.Run("a confirmed manual QR refund permits closure", func(t *testing.T) {
		session, checkID, comp := settledDineInOwedBack(t, env, sales.PaymentMethodManualQR)
		pending := env.refundOK(t, env.refundCommand(checkID, sales.RefundMethodManualQR,
			env.solePaymentIDForCheck(t, checkID), comp.Comp.ChargeAdjustmentID, 25_000))
		require.Equal(t, sales.RefundStatePending, pending.Refund.State)

		got := env.GetSessionOK(t, session.ID)
		require.False(t, sales.EvaluateClosureReadiness(got).AllRefundsResolved)
		_, status, err := env.TryClose(t, session.ID)
		require.ErrorIs(t, err, sales.ErrPendingRefundForClosure)
		require.Equal(t, http.StatusConflict, status)

		env.confirmOK(t, env.confirmCommand(pending.Refund.ID, nil))

		got = env.GetSessionOK(t, session.ID)
		require.True(t, sales.EvaluateClosureReadiness(got).AllRefundsResolved)
		require.Zero(t, env.findCheck(t, got, checkID).PendingRefundVND)

		sale, status, err := env.TryClose(t, session.ID)
		require.NoError(t, err)
		require.Equal(t, http.StatusCreated, status)
		require.Equal(t, session.ID, sale.ServiceSessionID)
	})
}

// TestCloseServiceSessionWithRemake proves the closure policy already handles
// the Phase 6B Remake without a code change: a Remake is a real Preparation
// Unit in the same Session, so its state decides readiness exactly like any
// original unit's, and a WASTED source is already terminal.
func TestCloseServiceSessionWithRemake(t *testing.T) {
	env := newSalesEnv(t)
	prep := newPrepHandlers(env)

	t.Run("a wasted source plus a fulfilled remake allows closure", func(t *testing.T) {
		submitted := env.committedDineInUnits(t, 1)
		source := submitted.PreparationUnits[0]

		prep.advancePrepUnit(t, source.ID, preparation.StateInPreparation)
		wasteID := prep.wastePrepUnit(t, source.ID)
		remake := prep.remakePrepUnit(t, wasteID)
		prep.fulfillPrepUnit(t, remake.Unit.ID)

		readiness := sales.EvaluateClosureReadiness(env.GetSessionOK(t, submitted.ID))
		require.True(t, readiness.Eligible,
			"a WASTED source is terminal and a fulfilled remake is done: %v", readiness)
		require.Empty(t, readiness.NonterminalUnitIDs)

		sale, status, err := env.TryClose(t, submitted.ID)
		require.NoError(t, err)
		require.Equal(t, 201, status)
		require.Len(t, sale.PreparationUnits, 2,
			"the Completed Sale carries the wasted source and its remake")
	})

	t.Run("an active remake blocks closure", func(t *testing.T) {
		submitted := env.committedDineInUnits(t, 1)
		source := submitted.PreparationUnits[0]

		prep.advancePrepUnit(t, source.ID, preparation.StateInPreparation)
		wasteID := prep.wastePrepUnit(t, source.ID)
		remake := prep.remakePrepUnit(t, wasteID)

		readiness := sales.EvaluateClosureReadiness(env.GetSessionOK(t, submitted.ID))
		require.False(t, readiness.Eligible,
			"an unfulfilled remake is real preparation work")
		require.Equal(t, []uuid.UUID{remake.Unit.ID}, readiness.NonterminalUnitIDs,
			"the remake alone holds the Session open, not its wasted source")

		_, _, err := env.TryClose(t, submitted.ID)
		require.ErrorIs(t, err, sales.ErrUnfulfilledPreparationForClosure)

		got := env.GetSessionOK(t, submitted.ID)
		require.Equal(t, sales.StateActive, got.State, "the refusal records nothing")
	})
}
