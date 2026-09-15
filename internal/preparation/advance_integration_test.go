//go:build integration

package preparation_test

import (
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/preparation"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAdvanceUnit(t *testing.T) {
	env := newPrepEnv(t)

	t.Run("the full chain reaches fulfilled", func(t *testing.T) {
		unit := env.SubmittedUnits(t, 1)[0]
		require.Equal(t, preparation.StateQueued, unit.State)

		for _, target := range []string{
			preparation.StateInPreparation,
			preparation.StateReady,
			preparation.StateFulfilled,
		} {
			got, _, err := env.Advance(t, unit.ID, target)
			require.NoError(t, err)
			require.Equal(t, target, got.State)
		}
		require.Equal(t, preparation.StateFulfilled, env.UnitState(t, unit.ID))
		require.Equal(t, 3, env.CountTransitions(t, unit.ID),
			"one transition row per advance")
	})

	t.Run("skipping a step is refused", func(t *testing.T) {
		unit := env.SubmittedUnits(t, 1)[0]

		_, _, err := env.Advance(t, unit.ID, preparation.StateFulfilled)
		require.ErrorIs(t, err, preparation.ErrInvalidTransition)
		require.Equal(t, preparation.StateQueued, env.UnitState(t, unit.ID))
		require.Equal(t, 0, env.CountTransitions(t, unit.ID))
	})

	t.Run("going backwards is refused", func(t *testing.T) {
		unit := env.SubmittedUnits(t, 1)[0]
		_, _, err := env.Advance(t, unit.ID, preparation.StateInPreparation)
		require.NoError(t, err)

		_, _, err = env.Advance(t, unit.ID, preparation.StateQueued)
		require.ErrorIs(t, err, preparation.ErrInvalidTransition)
	})

	t.Run("a fulfilled unit cannot advance", func(t *testing.T) {
		unit := env.SubmittedUnits(t, 1)[0]
		for _, target := range []string{
			preparation.StateInPreparation,
			preparation.StateReady,
			preparation.StateFulfilled,
		} {
			_, _, err := env.Advance(t, unit.ID, target)
			require.NoError(t, err)
		}
		_, _, err := env.Advance(t, unit.ID, preparation.StateFulfilled)
		require.ErrorIs(t, err, preparation.ErrInvalidTransition)
	})

	t.Run("cancelled and wasted are not advance targets", func(t *testing.T) {
		unit := env.SubmittedUnits(t, 1)[0]
		for _, target := range []string{preparation.StateCancelled, preparation.StateWasted} {
			_, _, err := env.Advance(t, unit.ID, target)
			require.ErrorIs(t, err, preparation.ErrInvalidTransition)
		}
	})

	t.Run("an unknown unit is not found", func(t *testing.T) {
		_, _, err := env.Advance(t, uuid.New(), preparation.StateInPreparation)
		require.ErrorIs(t, err, preparation.ErrUnitNotFound)
	})

	t.Run("units of one item advance independently", func(t *testing.T) {
		units := env.SubmittedUnits(t, 3)
		require.Len(t, units, 3)

		_, _, err := env.Advance(t, units[0].ID, preparation.StateInPreparation)
		require.NoError(t, err)

		require.Equal(t, preparation.StateInPreparation, env.UnitState(t, units[0].ID))
		require.Equal(t, preparation.StateQueued, env.UnitState(t, units[1].ID))
		require.Equal(t, preparation.StateQueued, env.UnitState(t, units[2].ID))
	})

	t.Run("a replayed request id returns the stored result", func(t *testing.T) {
		unit := env.SubmittedUnits(t, 1)[0]
		requestID := uuid.New()

		first, _, err := env.AdvanceWithRequestID(t, requestID, unit.ID, preparation.StateInPreparation)
		require.NoError(t, err)
		second, _, err := env.AdvanceWithRequestID(t, requestID, unit.ID, preparation.StateInPreparation)
		require.NoError(t, err)

		require.Equal(t, first.State, second.State)
		require.Equal(t, 1, env.CountTransitions(t, unit.ID), "a replay records nothing further")
	})

	t.Run("a cashier cannot advance", func(t *testing.T) {
		unit := env.SubmittedUnits(t, 1)[0]
		_, _, err := env.AdvanceAs(t, env.CashierActor(), unit.ID, preparation.StateInPreparation)
		require.ErrorIs(t, err, preparation.ErrForbidden)
	})
}
