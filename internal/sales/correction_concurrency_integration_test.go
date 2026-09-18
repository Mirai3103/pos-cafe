//go:build integration

package sales_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCorrectionRaces pins the Sales correction concurrency contract of design
// spec §11.2. Each subtest releases two commands simultaneously at their shared
// lock boundary, accepts only the explicitly documented outcomes, and proves
// afterwards that no hang, duplicate capacity, negative balance, or partial
// fact survived. Only the active-Comp-versus-Submit pairing may surface
// PostgreSQL 40P01 (ADR-031); every other pairing treats that abort as a
// failure.
func TestCorrectionRaces(t *testing.T) {
	t.Run("comp versus closure", func(t *testing.T) {
		env := newCompEnv(t)
		session, _, wasteID, checkID := env.paidTakeawayWastedUnit(t)

		compCmd := env.compCommand(t, wasteID, sales.CompReasonCafeError, nil)
		closeCmd := sales.CloseServiceSessionCommand{
			RequestID:        uuid.New(),
			ServiceSessionID: session.ID,
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		var compErr, closeErr error
		var compResult sales.CompResult
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, compResult, compErr = sales.NewCompWasteHandler(env.Runner).
				Handle(context.Background(), env.Actor, compCmd)
		}()
		go func() {
			defer wg.Done()
			<-start
			_, _, closeErr = sales.NewCloseServiceSessionHandler(env.Runner).
				Handle(context.Background(), env.Actor, closeCmd)
		}()
		close(start)
		requireCorrectionRaceResolved(t, &wg)

		require.False(t, correctionRaceDeadlock(compErr))
		require.False(t, correctionRaceDeadlock(closeErr))
		require.NoError(t, compErr,
			"Comp always commits: live before closure or post-sale after it")

		switch compResult.Scope {
		case sales.CompScopeLiveCheck:
			// Comp owned the Session first: the pending Refund it created is
			// exactly what closure refuses.
			require.ErrorIs(t, closeErr, sales.ErrPendingRefundForClosure)
			_, _, saleErr := env.GetCompletedSaleBySession(t, session.ID)
			require.ErrorIs(t, saleErr, sales.ErrCompletedSaleNotFound,
				"the refused closure recorded no Completed Sale")
			after := env.compCheck(t, checkID)
			assert.EqualValues(t, 0, after.ChargeVND)
			assert.Equal(t, sales.CheckStateSettled, after.State)
			assert.EqualValues(t, 25000, env.projectedCheck(t, session.ID, checkID).PendingRefundVND)
		case sales.CompScopePostSale:
			// Closure owned the Session first: the Comp appended post-sale
			// history without rewriting the closed snapshot.
			require.NoError(t, closeErr)
			require.NotNil(t, compResult.CompletedSaleID)
			sale, _, err := env.GetCompletedSaleBySession(t, session.ID)
			require.NoError(t, err)
			assert.Equal(t, sale.ID, *compResult.CompletedSaleID)
			require.Len(t, sale.PostSaleCorrections, 1)
			assert.EqualValues(t, 25000, sale.PostSaleCorrections[0].OutstandingRefundVND)
			require.Len(t, sale.Checks, 1)
			assert.EqualValues(t, 25000, sale.Checks[0].ChargeVND,
				"the snapshot keeps its pre-correction charge")
			assert.EqualValues(t, 25000, env.compCheck(t, checkID).ChargeVND,
				"a post-sale Comp writes no stored charge update")
		default:
			t.Fatalf("unexpected Comp scope %q", compResult.Scope)
		}

		assert.Equal(t, 1, env.compCount(t, wasteID), "exactly one Comp fact")
		assert.Equal(t, 1, env.adjustmentCountForWaste(t, wasteID), "exactly one adjustment")
	})

	t.Run("active comp versus submit", func(t *testing.T) {
		env := newCompEnv(t)
		session := env.commitDineInDraftWithQuantity(t, 1)
		session = env.Submit(t, session.ID)
		require.Len(t, session.PreparationUnits, 1)
		unit := session.PreparationUnits[0]
		env.advanceToReady(t, unit.ID)
		wasteID := env.wasteUnit(t, unit.ID)
		checkID := env.soleCheckID(t, session.ID)
		env.commitPendingRound(t, session.ID)
		chargeBefore := env.checkCharge(t, checkID)
		require.EqualValues(t, 50000, chargeBefore)

		compRequestID := uuid.New()
		compCmd := env.compCommand(t, wasteID, sales.CompReasonCafeError, nil)
		compCmd.RequestID = compRequestID
		submitRequestID := uuid.New()
		submitCmd := sales.SubmitOrderCommand{
			RequestID:        submitRequestID,
			ServiceSessionID: session.ID,
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		var compErr, submitErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, _, compErr = sales.NewCompWasteHandler(env.Runner).
				Handle(context.Background(), env.Actor, compCmd)
		}()
		go func() {
			defer wg.Done()
			<-start
			_, _, submitErr = sales.NewSubmitOrderHandler(env.Runner).
				Handle(context.Background(), env.Actor, submitCmd)
		}()
		close(start)
		requireCorrectionRaceResolved(t, &wg)

		// Comp locks Check then Session; Submit locks Session, Draft, then
		// Checks, so this is ADR-031's AB-BA window. No business rejection is
		// documented for the pairing and at least one side must stand.
		if compErr != nil {
			require.True(t, correctionRaceDeadlock(compErr),
				"the only accepted Comp failure here is the 40P01 abort: %v", compErr)
		}
		if submitErr != nil {
			require.True(t, correctionRaceDeadlock(submitErr),
				"the only accepted Submit failure here is the 40P01 abort: %v", submitErr)
		}
		require.True(t, compErr == nil || submitErr == nil,
			"the aborted side must leave the other standing: comp=%v submit=%v", compErr, submitErr)

		if submitErr == nil {
			assert.Equal(t, 2, env.countOrdersForSession(t, session.ID))
		} else {
			assert.Equal(t, 1, env.countOrdersForSession(t, session.ID),
				"the aborted Submit left no Order behind")
		}
		if compErr == nil {
			assert.Equal(t, 1, env.compCount(t, wasteID))
			assert.Equal(t, 1, env.adjustmentCountForWaste(t, wasteID))
		} else {
			assert.Equal(t, 0, env.compCount(t, wasteID),
				"the aborted Comp left no fact or adjustment")
			assert.Equal(t, 0, env.adjustmentCountForWaste(t, wasteID))
		}

		expectedCharge := chargeBefore
		if compErr == nil {
			expectedCharge -= 25000
		}
		assert.EqualValues(t, expectedCharge, env.checkCharge(t, checkID),
			"the stored charge reflects exactly the winners' writes")
		assert.Equal(t, compErr == nil, env.idempotencyClaimCount(t, env.Actor, compRequestID) == 1,
			"the aborted Comp rolls its claim back")
		assert.Equal(t, submitErr == nil, env.idempotencyClaimCount(t, env.Actor, submitRequestID) == 1,
			"the aborted Submit rolls its claim back")
	})

	t.Run("two comps for one waste", func(t *testing.T) {
		env := newCompEnv(t)
		session, _, wasteID := env.liveWastedUnit(t)
		checkID := env.soleCheckID(t, session.ID)
		chargeBefore := env.checkCharge(t, checkID)

		firstRequestID := uuid.New()
		firstCmd := env.compCommand(t, wasteID, sales.CompReasonCafeError, nil)
		firstCmd.RequestID = firstRequestID
		secondRequestID := uuid.New()
		secondCmd := env.compCommand(t, wasteID, sales.CompReasonQualityFailure, nil)
		secondCmd.RequestID = secondRequestID

		start := make(chan struct{})
		var wg sync.WaitGroup
		var firstErr, secondErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, _, firstErr = sales.NewCompWasteHandler(env.Runner).
				Handle(context.Background(), env.Actor, firstCmd)
		}()
		go func() {
			defer wg.Done()
			<-start
			_, _, secondErr = sales.NewCompWasteHandler(env.Runner).
				Handle(context.Background(), env.Actor, secondCmd)
		}()
		close(start)
		requireCorrectionRaceResolved(t, &wg)

		require.False(t, correctionRaceDeadlock(firstErr))
		require.False(t, correctionRaceDeadlock(secondErr))
		require.True(t, firstErr == nil || secondErr == nil,
			"the Waste admits exactly one Comp: first=%v second=%v", firstErr, secondErr)
		loserErr := firstErr
		loserRequestID := firstRequestID
		if firstErr == nil {
			loserErr = secondErr
			loserRequestID = secondRequestID
		}
		require.ErrorIs(t, loserErr, sales.ErrWasteAlreadyComped)

		assert.Equal(t, 1, env.compCount(t, wasteID), "one Comp fact, never two")
		assert.Equal(t, 1, env.adjustmentCountForWaste(t, wasteID))
		assert.EqualValues(t, chargeBefore-25000, env.checkCharge(t, checkID),
			"the charge is reduced once, never twice")
		assert.Equal(t, 0, env.idempotencyClaimCount(t, env.Actor, loserRequestID),
			"the losing Comp rolls its claim back")
	})

	t.Run("overlapping refunds on one adjustment", func(t *testing.T) {
		env := newRefundEnv(t)
		_, _, wasteID, checkID := env.paidTakeawayWastedUnitWithMethod(t, sales.PaymentMethodCash)
		comp := env.compOK(t, env.compCommand(t, wasteID, sales.CompReasonCafeError, nil))
		paymentID := env.solePaymentIDForCheck(t, checkID)

		firstRequestID := uuid.New()
		firstCmd := env.refundCommand(checkID, sales.RefundMethodCash, paymentID,
			comp.Comp.ChargeAdjustmentID, 25000)
		firstCmd.RequestID = firstRequestID
		secondRequestID := uuid.New()
		secondCmd := env.refundCommand(checkID, sales.RefundMethodCash, paymentID,
			comp.Comp.ChargeAdjustmentID, 25000)
		secondCmd.RequestID = secondRequestID

		start := make(chan struct{})
		var wg sync.WaitGroup
		var firstErr, secondErr error
		var firstResult, secondResult sales.RefundResult
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, firstResult, firstErr = sales.NewRecordRefundHandler(env.Runner).
				Handle(context.Background(), env.Actor, firstCmd)
		}()
		go func() {
			defer wg.Done()
			<-start
			_, secondResult, secondErr = sales.NewRecordRefundHandler(env.Runner).
				Handle(context.Background(), env.Actor, secondCmd)
		}()
		close(start)
		requireCorrectionRaceResolved(t, &wg)

		// The Check lock serializes the two Refunds; the loser must fail on the
		// capacity one of the two sources no longer has.
		require.False(t, correctionRaceDeadlock(firstErr))
		require.False(t, correctionRaceDeadlock(secondErr))
		require.True(t, firstErr == nil || secondErr == nil,
			"the sources admit exactly one Refund: first=%v second=%v", firstErr, secondErr)
		loserErr := firstErr
		loserRequestID := firstRequestID
		winner := firstResult
		if firstErr == nil {
			loserErr = secondErr
			loserRequestID = secondRequestID
			winner = firstResult
		} else {
			winner = secondResult
		}
		require.True(t,
			errors.Is(loserErr, sales.ErrRefundExceedsPendingRefund) ||
				errors.Is(loserErr, sales.ErrRefundExceedsAdjustmentCapacity) ||
				errors.Is(loserErr, sales.ErrRefundExceedsPaymentCapacity),
			"the losing Refund must fail on exhausted capacity, got %v", loserErr)

		assert.Equal(t, 1, env.refundCount(t, checkID))
		assert.Equal(t, 1, env.refundCompletionCount(t, winner.Refund.ID),
			"a Cash Refund completes in its own transaction")
		paymentVND, adjustmentVND := env.refundAllocationSums(t, winner.Refund.ID)
		assert.EqualValues(t, 25000, paymentVND)
		assert.EqualValues(t, 25000, adjustmentVND)
		projected := env.projectedCheck(t, env.findSessionIDForCheck(t, checkID), checkID)
		assert.EqualValues(t, 0, projected.ChargeVND)
		assert.EqualValues(t, 0, projected.PendingRefundVND, "the obligation is fully resolved")
		assert.EqualValues(t, 0, projected.BalanceVND)
		assert.Equal(t, 0, env.idempotencyClaimCount(t, env.Actor, loserRequestID),
			"the losing Refund rolls its claim back")
	})

	t.Run("overlapping refunds on one payment", func(t *testing.T) {
		env := newRefundEnv(t)
		_, checkID, compA, compB, paymentIDs := env.paidTwoUnitTwoComp(t)
		sharedPaymentID := paymentIDs[0]

		firstRequestID := uuid.New()
		firstCmd := env.refundCommand(checkID, sales.RefundMethodCash, sharedPaymentID,
			compA.Comp.ChargeAdjustmentID, 25000)
		firstCmd.RequestID = firstRequestID
		secondRequestID := uuid.New()
		secondCmd := env.refundCommand(checkID, sales.RefundMethodCash, sharedPaymentID,
			compB.Comp.ChargeAdjustmentID, 25000)
		secondCmd.RequestID = secondRequestID

		start := make(chan struct{})
		var wg sync.WaitGroup
		var firstErr, secondErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, _, firstErr = sales.NewRecordRefundHandler(env.Runner).
				Handle(context.Background(), env.Actor, firstCmd)
		}()
		go func() {
			defer wg.Done()
			<-start
			_, _, secondErr = sales.NewRecordRefundHandler(env.Runner).
				Handle(context.Background(), env.Actor, secondCmd)
		}()
		close(start)
		requireCorrectionRaceResolved(t, &wg)

		// Both Refunds name the same Payment with the same amount, so the
		// Payment's remaining capacity admits exactly one of them.
		require.False(t, correctionRaceDeadlock(firstErr))
		require.False(t, correctionRaceDeadlock(secondErr))
		require.True(t, firstErr == nil || secondErr == nil,
			"one Payment admits one Refund's allocation: first=%v second=%v", firstErr, secondErr)
		loserErr := firstErr
		loserRequestID := firstRequestID
		if firstErr == nil {
			loserErr = secondErr
			loserRequestID = secondRequestID
		}
		require.ErrorIs(t, loserErr, sales.ErrRefundExceedsPaymentCapacity,
			"the adjustment headroom remains, so the Payment's capacity is the binding one")

		assert.Equal(t, 1, env.refundCount(t, checkID))
		var allocatedToShared int64
		require.NoError(t, env.DB.QueryRow(`
			SELECT coalesce(sum(amount_vnd), 0)
			FROM refund_payment_allocations
			WHERE payment_id = $1`, sharedPaymentID).Scan(&allocatedToShared))
		assert.EqualValues(t, 25000, allocatedToShared,
			"the shared Payment's capacity is spent exactly once")
		var allocatedToOther int64
		require.NoError(t, env.DB.QueryRow(`
			SELECT coalesce(sum(amount_vnd), 0)
			FROM refund_payment_allocations
			WHERE payment_id = $1`, paymentIDs[1]).Scan(&allocatedToOther))
		assert.EqualValues(t, 0, allocatedToOther)
		projected := env.projectedCheck(t, env.findSessionIDForCheck(t, checkID), checkID)
		assert.EqualValues(t, 25000, projected.PendingRefundVND,
			"the unrecovered second capacity stays owed back")
		assert.EqualValues(t, 0, projected.BalanceVND)
		assert.Equal(t, 0, env.idempotencyClaimCount(t, env.Actor, loserRequestID))
	})

	t.Run("refund versus payment void", func(t *testing.T) {
		env := newVoidEnv(t)
		_, _, wasteID, checkID := env.paidTakeawayWastedUnitWithMethod(t, sales.PaymentMethodCash)
		comp := env.compOK(t, env.compCommand(t, wasteID, sales.CompReasonCafeError, nil))
		paymentID := env.solePaymentIDForCheck(t, checkID)

		refundRequestID := uuid.New()
		refundCmd := env.refundCommand(checkID, sales.RefundMethodCash, paymentID,
			comp.Comp.ChargeAdjustmentID, 25000)
		refundCmd.RequestID = refundRequestID
		voidRequestID := uuid.New()
		voidCmd := env.voidCommand(paymentID, sales.VoidReasonWrongAmount, nil)
		voidCmd.RequestID = voidRequestID

		start := make(chan struct{})
		var wg sync.WaitGroup
		var refundErr, voidErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, _, refundErr = sales.NewRecordRefundHandler(env.Runner).
				Handle(context.Background(), env.Actor, refundCmd)
		}()
		go func() {
			defer wg.Done()
			<-start
			_, _, voidErr = sales.NewVoidPaymentHandler(env.Runner).
				Handle(context.Background(), env.Actor, voidCmd)
		}()
		close(start)
		requireCorrectionRaceResolved(t, &wg)

		// Both commands lock Check, Session, Shift, then the Payment, so they
		// serialize: either the Refund allocation wins and Void refuses a
		// refunded Payment, or Void wins and the Refund finds no capacity.
		require.False(t, correctionRaceDeadlock(refundErr))
		require.False(t, correctionRaceDeadlock(voidErr))
		require.True(t, refundErr == nil || voidErr == nil,
			"exactly one of the two commands must stand: refund=%v void=%v", refundErr, voidErr)

		switch {
		case refundErr == nil:
			require.ErrorIs(t, voidErr, sales.ErrPaymentHasRefund)
			assert.Equal(t, 1, env.refundCount(t, checkID))
			assert.Equal(t, 0, env.voidCount(t, paymentID))
		default:
			require.NoError(t, voidErr)
			require.True(t,
				errors.Is(refundErr, sales.ErrRefundExceedsPaymentCapacity) ||
					errors.Is(refundErr, sales.ErrRefundExceedsPendingRefund),
				"the losing Refund must fail on a voided Payment's capacity, got %v", refundErr)
			assert.Equal(t, 0, env.refundCount(t, checkID))
			assert.Equal(t, 1, env.voidCount(t, paymentID))
		}

		projected := env.projectedCheck(t, env.findSessionIDForCheck(t, checkID), checkID)
		assert.GreaterOrEqual(t, projected.BalanceVND, int64(0))
		assert.GreaterOrEqual(t, projected.PendingRefundVND, int64(0))
		if voidErr == nil && refundErr != nil {
			assert.Equal(t, sales.CheckStateSettled, projected.State,
				"a zero charge with nothing received stays settled")
		}
		assert.Equal(t, refundErr == nil,
			env.idempotencyClaimCount(t, env.Actor, refundRequestID) == 1,
			"the losing Refund rolls its claim back")
		assert.Equal(t, voidErr == nil,
			env.idempotencyClaimCount(t, env.Actor, voidRequestID) == 1,
			"the losing Void rolls its claim back")
	})

	t.Run("void versus replacement payment", func(t *testing.T) {
		env := newVoidEnv(t)
		session := env.StartTakeaway(t)
		env.AddDraftItem(t, session.ID, env.CoffeeID, nil)
		session = env.Commit(t, session.ID)
		checkID := env.soleCheckID(t, session.ID)
		charge := env.checkCharge(t, checkID)
		require.EqualValues(t, 25000, charge)

		_, firstStatus, err := env.payCash(t, checkID, 10000, 10000)
		require.NoError(t, err)
		require.Equal(t, 200, firstStatus)
		_, secondStatus, err := env.payCash(t, checkID, 15000, 15000)
		require.NoError(t, err)
		require.Equal(t, 200, secondStatus)
		payments := env.paymentIDsForCheck(t, checkID)
		require.Len(t, payments, 2)
		voidedPaymentID := payments[0]

		voidRequestID := uuid.New()
		voidCmd := env.voidCommand(voidedPaymentID, sales.VoidReasonWrongAmount, nil)
		voidCmd.RequestID = voidRequestID
		replacementRequestID := uuid.New()
		replacementCmd := sales.PayCashCommand{
			RequestID:        replacementRequestID,
			CheckID:          checkID,
			AppliedAmountVND: 10000,
			CashTenderedVND:  10000,
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		var voidErr, replacementErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, _, voidErr = sales.NewVoidPaymentHandler(env.Runner).
				Handle(context.Background(), env.Actor, voidCmd)
		}()
		go func() {
			defer wg.Done()
			<-start
			_, _, replacementErr = sales.NewPayCashHandler(env.Runner).
				Handle(context.Background(), env.Actor, replacementCmd)
		}()
		close(start)
		requireCorrectionRaceResolved(t, &wg)

		// The Check lock serializes the Void and the replacement receipt. The
		// Void always stands; the replacement stands when it follows the Void
		// (the Check is open again) and is rejected when it runs first against
		// the still-settled Check.
		require.NoError(t, voidErr)
		require.False(t, correctionRaceDeadlock(replacementErr))
		projected := env.projectedCheck(t, session.ID, checkID)
		if replacementErr == nil {
			assert.Equal(t, sales.CheckStateSettled, projected.State)
			assert.EqualValues(t, 0, projected.BalanceVND)
			assert.EqualValues(t, charge, projected.EffectiveReceivedVND)
			assert.Equal(t, 2, env.countValidPayments(t, checkID))
		} else {
			require.ErrorIs(t, replacementErr, sales.ErrCheckNotOpen,
				"the replacement receipt raced behind the still-settled Check")
			assert.Equal(t, sales.CheckStateOpen, projected.State,
				"the Void reopened the Check, so the replacement is still needed")
			assert.EqualValues(t, 10000, projected.BalanceVND)
			assert.EqualValues(t, 15000, projected.EffectiveReceivedVND)
			assert.Equal(t, 1, env.countValidPayments(t, checkID))
		}
		assert.Equal(t, 1, env.voidCount(t, voidedPaymentID))
		assert.Equal(t, 1, env.idempotencyClaimCount(t, env.Actor, voidRequestID))
		assert.Equal(t, replacementErr == nil,
			env.idempotencyClaimCount(t, env.Actor, replacementRequestID) == 1)
	})
}

// commitPendingRound leaves one more COMMITTED draft in the Session, awaiting
// Submit, so an active-Comp-versus-Submit race has real Submit work to do.
func (e *compEnv) commitPendingRound(t *testing.T, sessionID uuid.UUID) {
	t.Helper()
	_, err := e.TryStartNewDraft(t, sessionID)
	require.NoError(t, err)
	e.AddDraftItem(t, sessionID, e.CoffeeID, nil)
	e.Commit(t, sessionID)
}

// findSessionIDForCheck resolves the Service Session that owns a Check.
func (e *salesEnv) findSessionIDForCheck(t *testing.T, checkID uuid.UUID) uuid.UUID {
	t.Helper()
	var sessionID uuid.UUID
	require.NoError(t, e.DB.QueryRow(
		`SELECT service_session_id FROM checks WHERE id = $1`, checkID).Scan(&sessionID))
	return sessionID
}

// countOrdersForSession counts a Session's submitted Orders.
func (e *salesEnv) countOrdersForSession(t *testing.T, sessionID uuid.UUID) int {
	t.Helper()
	var n int
	require.NoError(t, e.DB.QueryRow(
		`SELECT count(*) FROM orders WHERE service_session_id = $1`, sessionID).Scan(&n))
	return n
}

// countValidPayments counts a Check's Payments that carry no Void.
func (e *salesEnv) countValidPayments(t *testing.T, checkID uuid.UUID) int {
	t.Helper()
	var n int
	require.NoError(t, e.DB.QueryRow(`
		SELECT count(*)
		FROM payments AS p
		WHERE p.check_id = $1
		  AND NOT EXISTS (SELECT 1 FROM payment_voids AS v WHERE v.payment_id = p.id)`,
		checkID).Scan(&n))
	return n
}

// correctionRaceDeadlock reports whether err is PostgreSQL's retryable 40P01
// abort, the only failure ADR-031 accepts for a Submit pairing.
func correctionRaceDeadlock(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "40P01"
}

// requireCorrectionRaceResolved waits for a synchronized race with a hard
// upper bound, so a blocked pair fails the test rather than hanging it.
func requireCorrectionRaceResolved(t *testing.T, wg *sync.WaitGroup) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the synchronized race never resolved: a transaction is still blocked")
	}
}
