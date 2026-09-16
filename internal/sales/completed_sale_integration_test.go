//go:build integration

package sales_test

import (
	"testing"

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
