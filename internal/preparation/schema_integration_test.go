//go:build integration

package preparation_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/preparation"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestPreparationQueueSchema(t *testing.T) {
	db, _ := openPrepTestDB(t)

	var dataType, nullable string
	err := db.QueryRow(`
		SELECT data_type, is_nullable
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = 'preparation_units'
		  AND column_name = 'in_preparation_at'`).Scan(&dataType, &nullable)
	require.NoError(t, err)
	require.Equal(t, "timestamp with time zone", dataType)
	require.Equal(t, "YES", nullable)
}

func TestPreparationQueueMigrationBackfillsFirstStart(t *testing.T) {
	env := newPrepEnv(t)
	units := env.SubmittedUnits(t, 2)
	unit := units[0]
	before := time.Now()
	_, _, err := env.Advance(t, unit.ID, preparation.StateInPreparation)
	after := time.Now()
	require.NoError(t, err)
	var initiallyWritten time.Time
	require.NoError(t, env.DB.QueryRow(
		`SELECT in_preparation_at FROM preparation_units WHERE id = $1`, unit.ID,
	).Scan(&initiallyWritten))
	require.False(t, initiallyWritten.Before(before))
	require.False(t, initiallyWritten.After(after))

	earlier := before.Add(-2 * time.Hour)
	later := before.Add(-time.Hour)
	_, err = env.DB.Exec(`
		UPDATE preparation_unit_transitions
		SET occurred_at = $2
		WHERE preparation_unit_id = $1`, unit.ID, later)
	require.NoError(t, err)
	_, err = env.DB.Exec(`
		INSERT INTO preparation_unit_transitions (
			preparation_unit_id, prior_state, resulting_state,
			actor_staff_identity_id, staff_access_session_id, occurred_at
		) VALUES ($1, 'QUEUED', 'IN_PREPARATION', $2, $3, $4)`,
		unit.ID, env.barista.StaffID, env.barista.SessionID, earlier)
	require.NoError(t, err)

	_, err = env.DB.Exec(
		`UPDATE preparation_units SET in_preparation_at = NULL WHERE id = ANY($1)`,
		pq.Array([]uuid.UUID{unit.ID, units[1].ID}),
	)
	require.NoError(t, err)
	migration, err := os.ReadFile(filepath.Join(
		"..", "database", "migrations", "000012_add_preparation_queue_fields.sql",
	))
	require.NoError(t, err)
	_, err = env.DB.Exec(string(migration))
	require.NoError(t, err)

	var got, want time.Time
	require.NoError(t, env.DB.QueryRow(
		`SELECT in_preparation_at FROM preparation_units WHERE id = $1`, unit.ID,
	).Scan(&got))
	require.NoError(t, env.DB.QueryRow(`
		SELECT min(occurred_at)
		FROM preparation_unit_transitions
		WHERE preparation_unit_id = $1 AND resulting_state = 'IN_PREPARATION'`, unit.ID,
	).Scan(&want))
	require.Equal(t, want, got)
	var neverStartedIsNull bool
	require.NoError(t, env.DB.QueryRow(
		`SELECT in_preparation_at IS NULL FROM preparation_units WHERE id = $1`, units[1].ID,
	).Scan(&neverStartedIsNull))
	require.True(t, neverStartedIsNull)
}
