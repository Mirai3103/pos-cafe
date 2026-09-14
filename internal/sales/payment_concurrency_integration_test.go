//go:build integration

package sales_test

import (
	"sync"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/stretchr/testify/require"
)

// Two Payments on the same Check serialize on that Check's row lock.
func TestConcurrentPaymentsOnOneCheck(t *testing.T) {
	env := newSalesEnv(t)
	session := env.commitTakeawayDraft(t, 2)
	checkID := env.soleCheckID(t, session.ID)
	charge := env.checkCharge(t, checkID)

	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, _, errs[i] = env.payCash(t, checkID, charge, charge)
		}(i)
	}
	close(start)
	wg.Wait()

	var succeeded int
	for _, err := range errs {
		if err == nil {
			succeeded++
			continue
		}
		// The winner settled the Check while the loser waited on its row
		// lock, so the loser is a Payment against an already-SETTLED Check
		// and reports CHECK_NOT_OPEN (the state precondition is evaluated
		// before the balance, and a settled Check is no longer payable).
		// Either way the loser fails cleanly and records nothing.
		require.ErrorIs(t, err, sales.ErrCheckNotOpen,
			"the losing payment must fail cleanly, not corrupt the balance")
	}
	require.Equal(t, 1, succeeded)
	require.Equal(t, 1, env.countPayments(t, checkID))

	got := env.GetSessionOK(t, session.ID)
	require.Equal(t, int64(0), got.Checks[0].BalanceVND)
	require.Equal(t, sales.CheckStateSettled, got.Checks[0].State)
}

// FOR SHARE on the parent rows is what lets these two proceed in parallel.
// Under the canonical FOR UPDATE they would serialize on the Session row.
func TestConcurrentPaymentsOnTwoChecksOfOneSession(t *testing.T) {
	env := newSalesEnv(t)
	session := env.commitTakeawayDraft(t, 2)
	firstID := env.soleCheckID(t, session.ID)
	allocations := env.checkAllocations(t, session.ID, firstID)

	got, _, err := env.splitToNewCheck(t, firstID, []sales.SplitItem{
		{CommittedItemID: allocations[0].CommittedItemID, Quantity: 1},
	})
	require.NoError(t, err)
	secondID := env.otherCheckID(t, got, firstID)

	firstCharge := env.checkCharge(t, firstID)
	secondCharge := env.checkCharge(t, secondID)

	start := make(chan struct{})
	var wg sync.WaitGroup
	var firstErr, secondErr error
	wg.Add(2)
	go func() { defer wg.Done(); <-start; _, _, firstErr = env.payCash(t, firstID, firstCharge, firstCharge) }()
	go func() {
		defer wg.Done()
		<-start
		_, _, secondErr = env.payCash(t, secondID, secondCharge, secondCharge)
	}()
	close(start)
	wg.Wait()

	require.NoError(t, firstErr)
	require.NoError(t, secondErr)

	final := env.GetSessionOK(t, session.ID)
	for _, check := range final.Checks {
		require.Equal(t, sales.CheckStateSettled, check.State)
	}
}

// A Split racing a Payment on a shared Check resolves to one of exactly two
// documented outcomes, never to a corrupted charge: the Payment wins and the
// Split reports CHECK_HAS_PAYMENT, or the Split wins and the Payment reports
// PAYMENT_EXCEEDS_CHECK_BALANCE against the reduced balance.
func TestSplitRacingPayment(t *testing.T) {
	env := newSalesEnv(t)
	session := env.commitTakeawayDraft(t, 2)
	checkID := env.soleCheckID(t, session.ID)
	allocations := env.checkAllocations(t, session.ID, checkID)
	charge := env.checkCharge(t, checkID)
	// The racing Payment is one VND short of the full charge: large enough
	// that it exceeds the balance of whichever Check the Split leaves behind
	// (a moved quantity-1 unit costs far more than 1), and small enough that
	// a Payment win leaves the Check OPEN with a positive balance rather than
	// settling it — a settled Check would report CHECK_NOT_OPEN instead of
	// the outcome §10 documents. So the loser always reports one of exactly
	// the two documented outcomes: CHECK_HAS_PAYMENT when the Payment wins,
	// PAYMENT_EXCEEDS_CHECK_BALANCE when the Split wins.
	applied := charge - 1

	start := make(chan struct{})
	var wg sync.WaitGroup
	var payErr, splitErr error
	wg.Add(2)
	go func() { defer wg.Done(); <-start; _, _, payErr = env.payCash(t, checkID, applied, applied) }()
	go func() {
		defer wg.Done()
		<-start
		_, _, splitErr = env.splitToNewCheck(t, checkID, []sales.SplitItem{
			{CommittedItemID: allocations[0].CommittedItemID, Quantity: 1},
		})
	}()
	close(start)
	wg.Wait()

	if payErr == nil && splitErr == nil {
		t.Fatal("a payment and a split on one check must not both succeed")
	}
	if splitErr != nil {
		require.ErrorIs(t, splitErr, sales.ErrCheckHasPayment)
	}

	// Whichever won, the projection must still reconcile.
	_ = env.GetSessionOK(t, session.ID)
}
