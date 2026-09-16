package preparation_test

import (
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/preparation"
	"github.com/stretchr/testify/require"
)

func TestNextState(t *testing.T) {
	cases := []struct {
		from string
		next string
		ok   bool
	}{
		{preparation.StateQueued, preparation.StateInPreparation, true},
		{preparation.StateInPreparation, preparation.StateReady, true},
		{preparation.StateReady, preparation.StateFulfilled, true},
		{preparation.StateFulfilled, "", false},
		{preparation.StateCancelled, "", false},
		{preparation.StateWasted, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.from, func(t *testing.T) {
			next, ok := preparation.NextState(tc.from)
			require.Equal(t, tc.ok, ok)
			require.Equal(t, tc.next, next)
		})
	}
}

func TestIsLegalAdvance(t *testing.T) {
	require.True(t, preparation.IsLegalAdvance(preparation.StateQueued, preparation.StateInPreparation))
	require.True(t, preparation.IsLegalAdvance(preparation.StateInPreparation, preparation.StateReady))
	require.True(t, preparation.IsLegalAdvance(preparation.StateReady, preparation.StateFulfilled))

	// Skipping a step is not an advance.
	require.False(t, preparation.IsLegalAdvance(preparation.StateQueued, preparation.StateReady))
	require.False(t, preparation.IsLegalAdvance(preparation.StateQueued, preparation.StateFulfilled))
	// Going backwards is a State Correction, which is Phase 6.
	require.False(t, preparation.IsLegalAdvance(preparation.StateReady, preparation.StateInPreparation))
	// Terminal states are terminal.
	require.False(t, preparation.IsLegalAdvance(preparation.StateFulfilled, preparation.StateFulfilled))
	// Cancellation and Waste are Phase 6 commands, not advances.
	require.False(t, preparation.IsLegalAdvance(preparation.StateQueued, preparation.StateCancelled))
	require.False(t, preparation.IsLegalAdvance(preparation.StateInPreparation, preparation.StateWasted))
}

func TestIsAdvanceTarget(t *testing.T) {
	for _, s := range []string{preparation.StateInPreparation, preparation.StateReady, preparation.StateFulfilled} {
		require.True(t, preparation.IsAdvanceTarget(s), s)
	}
	for _, s := range []string{preparation.StateQueued, preparation.StateCancelled, preparation.StateWasted, "", "BREWING"} {
		require.False(t, preparation.IsAdvanceTarget(s), s)
	}
}
