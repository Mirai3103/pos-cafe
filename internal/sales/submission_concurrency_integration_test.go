//go:build integration

package sales_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func TestConcurrentSubmit(t *testing.T) {
	env := newSalesEnv(t)
	ctx := context.Background()
	session := env.commitDineInDraftWithQuantity(t, 1)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, errs[i] = env.TrySubmit(t, session.ID)
		}(i)
	}
	wg.Wait()

	succeeded := 0
	for _, err := range errs {
		if err == nil {
			succeeded++
		} else {
			require.ErrorIs(t, err, sales.ErrNothingToSubmit)
		}
	}
	require.Equal(t, 1, succeeded, "exactly one Submit creates the Order")

	var n int
	require.NoError(t, env.DB.QueryRowContext(ctx,
		`SELECT count(*) FROM orders WHERE service_session_id = $1`, session.ID).Scan(&n))
	require.Equal(t, 1, n, "UNIQUE (order_draft_id) is the backstop")
}

func TestConcurrentClose(t *testing.T) {
	env := newSalesEnv(t)
	ctx := context.Background()
	session := readyToClose(t, env, 1)

	var wg sync.WaitGroup
	sales := make([]uuid.UUID, 2)
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got, _, err := env.TryClose(t, session.ID)
			sales[i], errs[i] = got.ID, err
		}(i)
	}
	wg.Wait()

	for _, err := range errs {
		require.NoError(t, err, "closing twice is a duplicate, not a mistake")
	}
	require.Equal(t, sales[0], sales[1])

	var n int
	require.NoError(t, env.DB.QueryRowContext(ctx,
		`SELECT count(*) FROM completed_sales WHERE service_session_id = $1`,
		session.ID).Scan(&n))
	require.Equal(t, 1, n)
}

func TestSubmitAgainstConcurrentPayment(t *testing.T) {
	// This test encodes ADR-031's contract. Both sides take Check locks in
	// ascending (created_at, id) order, so they serialize when the
	// interleaving allows it; but Submit's Session-then-Checks order runs
	// opposite to Payment's Check-then-Session, so the race can also trip the
	// Session-lock AB-BA, aborting one side with a clean 40P01 whose rollback
	// discards its idempotency claim — a client retry absorbs it. The test
	// accepts both interleavings and asserts no corruption either way.
	env := newSalesEnv(t)
	ctx := context.Background()
	session := env.commitDineInDraftWithQuantity(t, 1)
	checkID := env.soleCheckID(t, session.ID)
	charge := env.checkCharge(t, checkID)

	var wg sync.WaitGroup
	wg.Add(2)
	var submitErr, payErr error
	go func() { defer wg.Done(); _, _, submitErr = env.TrySubmit(t, session.ID) }()
	go func() { defer wg.Done(); _, _, payErr = env.payCash(t, checkID, charge, charge) }()
	wg.Wait()

	// A failing side must be exactly the recorded deadlock abort. A domain
	// sentinel (ErrNothingToSubmit, ErrCheckNotSettledForSubmission, ...) or
	// an unexpected 500 carries no PgError, so the 40P01 match doubles as the
	// "not a business rejection" assertion: matching one of those here means
	// a logic bug, not the window.
	isDeadlock := func(err error) bool {
		var pgErr *pgconn.PgError
		return errors.As(err, &pgErr) && pgErr.Code == "40P01"
	}
	if submitErr != nil {
		require.True(t, isDeadlock(submitErr), "submit failed outside the ADR-031 window: %v", submitErr)
	}
	if payErr != nil {
		require.True(t, isDeadlock(payErr), "payment failed outside the ADR-031 window: %v", payErr)
	}
	require.True(t, submitErr == nil || payErr == nil,
		"the abort must leave at least one side standing: submit=%v, pay=%v", submitErr, payErr)

	// No corruption: the Order exists exactly when Submit stood.
	var orderCount int
	require.NoError(t, env.DB.QueryRowContext(ctx,
		`SELECT count(*) FROM orders WHERE service_session_id = $1`, session.ID).Scan(&orderCount))
	switch {
	case submitErr == nil:
		// Submit landed; Payment may or may not have.
		require.Equal(t, 1, orderCount)
		if payErr == nil {
			require.Equal(t, 1, env.countPayments(t, checkID))
		}
	case payErr == nil:
		// Payment won the race and Submit was the aborted side: nothing it
		// attempted may remain, and the landed Payment must be intact.
		require.Equal(t, 0, orderCount)
		require.Equal(t, 1, env.countPayments(t, checkID))
		check := env.findCheck(t, env.GetSessionOK(t, session.ID), checkID)
		require.Equal(t, sales.CheckStateSettled, check.State)
	}
}
