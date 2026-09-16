//go:build integration

package preparation_test

import (
	"bytes"
	"context"
	"sort"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/preparation"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestActiveQueueReturnsEmptySliceAndDatabaseTime(t *testing.T) {
	env := newPrepEnv(t)
	// observed_at is read from PostgreSQL, so its bounds must come from the
	// same clock. Bounding it with the host time.Now() fails whenever the
	// Docker VM clock lags the host under suite load; SELECT now() through
	// env.DB makes the comparison deterministic.
	var before, after time.Time
	require.NoError(t, env.DB.QueryRow(`SELECT now()`).Scan(&before))
	got, err := preparation.NewActiveQueueHandler(env.PreparationRunner).
		Handle(context.Background(), env.BaristaActor())
	require.NoError(t, err)
	require.NoError(t, env.DB.QueryRow(`SELECT now()`).Scan(&after))
	require.NotNil(t, got.Units)
	require.Empty(t, got.Units)
	require.False(t, got.ObservedAt.Before(before))
	require.False(t, got.ObservedAt.After(after))
}

func TestActiveQueueOrdersFIFOAndExcludesTerminalUnits(t *testing.T) {
	env := newPrepEnv(t)
	units := env.SubmittedUnits(t, 3)
	_, _, err := env.Advance(t, units[0].ID, preparation.StateInPreparation)
	require.NoError(t, err)
	_, _, err = env.Advance(t, units[0].ID, preparation.StateReady)
	require.NoError(t, err)
	_, _, err = env.Advance(t, units[0].ID, preparation.StateFulfilled)
	require.NoError(t, err)

	got, err := preparation.NewActiveQueueHandler(env.PreparationRunner).
		Handle(context.Background(), env.BaristaActor())
	require.NoError(t, err)
	require.Len(t, got.Units, 2)
	require.Equal(t, units[1].ID, got.Units[0].ID)
	require.Equal(t, units[2].ID, got.Units[1].ID)
	require.Equal(t, int32(3), got.Units[0].OrderItemUnitCount)
}

func TestActiveQueueProjectsCurrentTables(t *testing.T) {
	env := newPrepEnv(t)
	unit := env.SubmittedUnits(t, 1)[0]
	sessionID := env.SessionIDForUnit(t, unit.ID)
	secondTable := env.SeedTable(t, "Bàn 2")

	// Before any reassignment the Session's current unreleased assignment is
	// the table it opened against.
	before, err := preparation.NewActiveQueueHandler(env.PreparationRunner).
		Handle(context.Background(), env.BaristaActor())
	require.NoError(t, err)
	require.Len(t, before.Units, 1)
	require.Equal(t, []string{"Bàn 1"}, before.Units[0].TableNames)

	// SetTables writes only table_assignments; the Preparation Unit snapshot
	// row is untouched, so the after-read proves the projection is computed
	// at read time from current assignments, not frozen on the unit.
	env.SetTables(t, sessionID, secondTable)

	after, err := preparation.NewActiveQueueHandler(env.PreparationRunner).
		Handle(context.Background(), env.BaristaActor())
	require.NoError(t, err)
	require.Len(t, after.Units, 1)
	require.Equal(t, []string{"Bàn 2"}, after.Units[0].TableNames)
}

func TestActiveQueueDeniesCashier(t *testing.T) {
	env := newPrepEnv(t)
	_, err := preparation.NewActiveQueueHandler(env.PreparationRunner).
		Handle(context.Background(), env.CashierActor())
	require.ErrorIs(t, err, preparation.ErrForbidden)
}

func TestActiveQueueUsesIDAsEqualTimestampTieBreak(t *testing.T) {
	env := newPrepEnv(t)
	units := env.SubmittedUnits(t, 3)

	// One timestamp for all three rows: the queued_at ordering can no longer
	// separate them, so the stable tie-break must be the unit id.
	tiedAt := time.Now()
	for _, unit := range units {
		_, err := env.DB.Exec(
			`UPDATE preparation_units SET queued_at = $1 WHERE id = $2`, tiedAt, unit.ID)
		require.NoError(t, err)
	}

	ids := []uuid.UUID{units[0].ID, units[1].ID, units[2].ID}
	sort.Slice(ids, func(i, j int) bool { return bytes.Compare(ids[i][:], ids[j][:]) < 0 })

	got, err := preparation.NewActiveQueueHandler(env.PreparationRunner).
		Handle(context.Background(), env.BaristaActor())
	require.NoError(t, err)
	require.Len(t, got.Units, 3)
	gotIDs := []uuid.UUID{got.Units[0].ID, got.Units[1].ID, got.Units[2].ID}
	require.Equal(t, ids, gotIDs)
}

func TestActiveQueueIncludesEveryActiveStateAndStartTime(t *testing.T) {
	env := newPrepEnv(t)
	units := env.SubmittedUnits(t, 3)

	started, _, err := env.Advance(t, units[1].ID, preparation.StateInPreparation)
	require.NoError(t, err)
	readyStarted, _, err := env.Advance(t, units[2].ID, preparation.StateInPreparation)
	require.NoError(t, err)
	_, _, err = env.Advance(t, units[2].ID, preparation.StateReady)
	require.NoError(t, err)

	got, err := preparation.NewActiveQueueHandler(env.PreparationRunner).
		Handle(context.Background(), env.BaristaActor())
	require.NoError(t, err)
	require.Len(t, got.Units, 3)

	byID := make(map[uuid.UUID]preparation.QueueUnitResponse, len(got.Units))
	for _, unit := range got.Units {
		byID[unit.ID] = unit
	}

	require.Equal(t, preparation.StateQueued, byID[units[0].ID].State)
	require.Nil(t, byID[units[0].ID].InPreparationAt)

	require.Equal(t, preparation.StateInPreparation, byID[units[1].ID].State)
	require.NotNil(t, byID[units[1].ID].InPreparationAt)
	require.Equal(t, *started.InPreparationAt, *byID[units[1].ID].InPreparationAt)

	require.Equal(t, preparation.StateReady, byID[units[2].ID].State)
	require.NotNil(t, byID[units[2].ID].InPreparationAt)
	// The start time is stamped once at the IN_PREPARATION transition and
	// retained unchanged through READY.
	require.Equal(t, *readyStarted.InPreparationAt, *byID[units[2].ID].InPreparationAt)
}

func TestActiveQueueTakeawayHasEmptyTables(t *testing.T) {
	env := newPrepEnv(t)
	unit := env.SubmittedTakeawayUnits(t, 1)[0]

	got, err := preparation.NewActiveQueueHandler(env.PreparationRunner).
		Handle(context.Background(), env.BaristaActor())
	require.NoError(t, err)
	require.Len(t, got.Units, 1)
	require.Equal(t, unit.ID, got.Units[0].ID)
	// An empty collection serializes as [], never null: TableNames must be
	// non-nil even with no table to name.
	require.NotNil(t, got.Units[0].TableNames)
	require.Empty(t, got.Units[0].TableNames)
}
