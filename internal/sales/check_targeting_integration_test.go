//go:build integration

package sales_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCurrentUnpaidReusesTheOpenCheck(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartTakeaway(t)
	env.AddDraftItem(t, session.ID, env.CoffeeID, nil)
	first := env.Commit(t, session.ID)
	require.Len(t, first.Checks, 1)

	// 5B cannot open a second draft through the API, so seed one directly.
	env.SeedEditableDraft(t, session.ID, "CURRENT_UNPAID")
	env.AddDraftItem(t, session.ID, env.CoffeeID, nil)
	second := env.Commit(t, session.ID)

	require.Len(t, second.Checks, 1, "the charge joined the existing check")
	require.Equal(t, first.Checks[0].ID, second.Checks[0].ID)
	require.Equal(t, 2*first.Checks[0].ChargeVND, second.Checks[0].ChargeVND)
	require.Len(t, second.Checks[0].Allocations, 2)
}

func TestNewCheckAlwaysOpensAnotherCheck(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartTakeaway(t)
	env.AddDraftItem(t, session.ID, env.CoffeeID, nil)
	first := env.Commit(t, session.ID)

	env.SeedEditableDraft(t, session.ID, "NEW_CHECK")
	env.AddDraftItem(t, session.ID, env.CoffeeID, nil)
	second := env.Commit(t, session.ID)

	require.Len(t, second.Checks, 2)
	require.NotEqual(t, second.Checks[0].ID, second.Checks[1].ID)
	for _, check := range second.Checks {
		require.Equal(t, first.Checks[0].ChargeVND, check.ChargeVND)
		require.Len(t, check.Allocations, 1)
	}
}

func TestFirstCommitOpensACheckRegardlessOfTarget(t *testing.T) {
	for _, target := range []string{"CURRENT_UNPAID", "NEW_CHECK"} {
		t.Run(target, func(t *testing.T) {
			env := newSalesEnv(t)
			session := env.StartTakeaway(t)
			env.SetCheckTarget(t, session.ID, target)
			env.AddDraftItem(t, session.ID, env.CoffeeID, nil)

			got := env.Commit(t, session.ID)

			require.Len(t, got.Checks, 1)
			require.Equal(t, "OPEN", got.Checks[0].State)
		})
	}
}

// A new Check is never observable at zero: it is raised inside the same
// transaction that creates it.
func TestNewCheckIsNeverObservableAtZero(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartTakeaway(t)
	env.AddDraftItem(t, session.ID, env.CoffeeID, nil)

	got := env.Commit(t, session.ID)

	require.Positive(t, got.Checks[0].ChargeVND)
}
