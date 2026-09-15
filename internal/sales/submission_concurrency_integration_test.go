//go:build integration

package sales_test

import (
	"context"
	"sync"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
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
	// Both take Check locks in ascending (created_at, id) order, so they
	// serialize instead of deadlocking.
	env := newSalesEnv(t)
	session := env.commitDineInDraftWithQuantity(t, 1)
	checkID := env.soleCheckID(t, session.ID)
	charge := env.checkCharge(t, checkID)

	var wg sync.WaitGroup
	wg.Add(2)
	var submitErr, payErr error
	go func() { defer wg.Done(); _, _, submitErr = env.TrySubmit(t, session.ID) }()
	go func() { defer wg.Done(); _, _, payErr = env.payCash(t, checkID, charge, charge) }()
	wg.Wait()

	require.NoError(t, submitErr, "dine-in Submit does not require settlement")
	require.NoError(t, payErr)
}
