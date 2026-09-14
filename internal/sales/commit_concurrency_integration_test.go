//go:build integration

package sales_test

import (
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestConcurrentCommitsOfOneDraftCommitOnce(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartTakeaway(t)
	env.AddDraftItem(t, session.ID, env.CoffeeID, nil)

	start := make(chan struct{})
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			_, err := env.TryCommitWithRequestID(t, session.ID, uuid.New())
			results <- err
		}()
	}
	close(start)

	var succeeded, rejected int
	for i := 0; i < 2; i++ {
		if err := <-results; err == nil {
			succeeded++
		} else {
			require.ErrorIs(t, err, sales.ErrEditableDraftNotFound)
			rejected++
		}
	}
	require.Equal(t, 1, succeeded)
	require.Equal(t, 1, rejected)
	env.RequireCheckCount(t, session.ID, 1)
}

func TestConcurrentDuplicateCommitRequestsExecuteOnce(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartTakeaway(t)
	env.AddDraftItem(t, session.ID, env.CoffeeID, nil)
	requestID := uuid.New()

	start := make(chan struct{})
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			_, err := env.TryCommitWithRequestID(t, session.ID, requestID)
			results <- err
		}()
	}
	close(start)
	for i := 0; i < 2; i++ {
		require.NoError(t, <-results)
	}

	env.RequireCheckCount(t, session.ID, 1)
	env.RequireCommittedItemCount(t, session.ID, 1)
}

// FOR SHARE on Catalog rows means two Sessions committing orders that share a
// popular menu item proceed in parallel rather than serializing. See ADR-015.
func TestConcurrentCommitsSharingAMenuItemBothSucceed(t *testing.T) {
	env := newSalesEnv(t)
	first := env.StartTakeaway(t)
	second := env.StartTakeaway(t)
	env.AddDraftItem(t, first.ID, env.CoffeeID, nil)
	env.AddDraftItem(t, second.ID, env.CoffeeID, nil)

	start := make(chan struct{})
	results := make(chan error, 2)
	for _, sessionID := range []uuid.UUID{first.ID, second.ID} {
		go func(id uuid.UUID) {
			<-start
			_, err := env.TryCommit(t, id)
			results <- err
		}(sessionID)
	}
	close(start)
	for i := 0; i < 2; i++ {
		require.NoError(t, <-results)
	}

	env.RequireCheckCount(t, first.ID, 1)
	env.RequireCheckCount(t, second.ID, 1)
}

func TestCommitReplayConflictsOnADifferentPayload(t *testing.T) {
	env := newSalesEnv(t)
	first := env.StartTakeaway(t)
	second := env.StartTakeaway(t)
	env.AddDraftItem(t, first.ID, env.CoffeeID, nil)
	env.AddDraftItem(t, second.ID, env.CoffeeID, nil)
	requestID := uuid.New()

	env.CommitWithRequestID(t, first.ID, requestID)
	_, err := env.TryCommitWithRequestID(t, second.ID, requestID)

	require.ErrorIs(t, err, sales.ErrRequestConflict)
}

func TestCommitReplayDeniedAfterIdentityDisabled(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartTakeaway(t)
	env.AddDraftItem(t, session.ID, env.CoffeeID, nil)
	requestID := uuid.New()
	env.CommitWithRequestID(t, session.ID, requestID)

	env.DisableActorIdentity(t)

	_, err := env.TryCommitWithRequestID(t, session.ID, requestID)
	require.ErrorIs(t, err, sales.ErrForbidden)
}

// No 5B code path writes SETTLED or MERGED.
func TestNoCheckLeavesTheOpenState(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartTakeaway(t)
	env.AddDraftItem(t, session.ID, env.CoffeeID, nil)
	env.Commit(t, session.ID)

	var n int
	require.NoError(t, env.DB.QueryRow(
		`SELECT count(*) FROM checks WHERE state <> 'OPEN'`).Scan(&n))
	require.Zero(t, n)
}
