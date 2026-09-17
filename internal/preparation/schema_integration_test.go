//go:build integration

package preparation_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/preparation"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// constraintDef reads one constraint's decompiled definition by name and
// fails the test when the constraint does not exist.
func constraintDef(t *testing.T, db *sql.DB, name string) string {
	t.Helper()
	var clause string
	err := db.QueryRow(`
		SELECT pg_get_constraintdef(oid)
		FROM pg_constraint
		WHERE conname = $1`, name).Scan(&clause)
	require.NoError(t, err, "constraint %s must exist", name)
	return clause
}

// indexDef reads one index's decompiled definition by name and fails the test
// when the index does not exist.
func indexDef(t *testing.T, db *sql.DB, name string) string {
	t.Helper()
	var def string
	err := db.QueryRow(`
		SELECT indexdef
		FROM pg_indexes
		WHERE indexname = $1`, name).Scan(&def)
	require.NoError(t, err, "index %s must exist", name)
	return def
}

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

// TestPreparationCorrectionsSchema proves migration 000013 created the four
// Phase 6B correction tables, their foreign keys and evidence columns, the two
// new Preparation Unit columns, and the two spec indexes.
func TestPreparationCorrectionsSchema(t *testing.T) {
	db, _ := openPrepTestDB(t)

	t.Run("all four correction tables exist", func(t *testing.T) {
		var n int
		require.NoError(t, db.QueryRow(`
			SELECT count(*) FROM information_schema.tables
			WHERE table_schema = 'public'
			  AND table_name IN ('preparation_alerts', 'preparation_wastes',
			                     'preparation_remakes', 'preparation_state_corrections')`).
			Scan(&n))
		require.Equal(t, 4, n)
	})

	t.Run("preparation_units carries the two new columns", func(t *testing.T) {
		var priorityType, priorityNullable string
		require.NoError(t, db.QueryRow(`
			SELECT data_type, is_nullable
			FROM information_schema.columns
			WHERE table_schema = 'public'
			  AND table_name = 'preparation_units'
			  AND column_name = 'priority'`).Scan(&priorityType, &priorityNullable))
		require.Equal(t, "text", priorityType)
		require.Equal(t, "NO", priorityNullable)

		var priorityDefault string
		require.NoError(t, db.QueryRow(`
			SELECT column_default
			FROM information_schema.columns
			WHERE table_schema = 'public'
			  AND table_name = 'preparation_units'
			  AND column_name = 'priority'`).Scan(&priorityDefault))
		require.Contains(t, priorityDefault, "STANDARD")

		var linkType, linkNullable string
		require.NoError(t, db.QueryRow(`
			SELECT data_type, is_nullable
			FROM information_schema.columns
			WHERE table_schema = 'public'
			  AND table_name = 'preparation_units'
			  AND column_name = 'remake_of_preparation_unit_id'`).
			Scan(&linkType, &linkNullable))
		require.Equal(t, "uuid", linkType)
		require.Equal(t, "YES", linkNullable)
	})

	t.Run("every correction fact carries its foreign keys", func(t *testing.T) {
		foreignKeys := []struct {
			name       string
			references string
		}{
			{"preparation_units_remake_of_preparation_unit_id_fkey", "REFERENCES preparation_units(id) ON DELETE RESTRICT"},
			{"preparation_alerts_preparation_unit_id_fkey", "REFERENCES preparation_units(id) ON DELETE RESTRICT"},
			{"preparation_alerts_created_by_staff_identity_id_fkey", "REFERENCES staff_identities(id) ON DELETE RESTRICT"},
			{"preparation_alerts_created_staff_access_session_id_fkey", "REFERENCES staff_access_sessions(id) ON DELETE RESTRICT"},
			{"preparation_alerts_acknowledged_by_staff_identity_id_fkey", "REFERENCES staff_identities(id) ON DELETE RESTRICT"},
			{"preparation_alerts_acknowledged_staff_access_session_id_fkey", "REFERENCES staff_access_sessions(id) ON DELETE RESTRICT"},
			{"preparation_wastes_preparation_unit_id_fkey", "REFERENCES preparation_units(id) ON DELETE RESTRICT"},
			{"preparation_wastes_actor_staff_identity_id_fkey", "REFERENCES staff_identities(id) ON DELETE RESTRICT"},
			{"preparation_wastes_staff_access_session_id_fkey", "REFERENCES staff_access_sessions(id) ON DELETE RESTRICT"},
			{"preparation_remakes_waste_id_fkey", "REFERENCES preparation_wastes(id) ON DELETE RESTRICT"},
			{"preparation_remakes_preparation_unit_id_fkey", "REFERENCES preparation_units(id) ON DELETE RESTRICT"},
			{"preparation_remakes_actor_staff_identity_id_fkey", "REFERENCES staff_identities(id) ON DELETE RESTRICT"},
			{"preparation_remakes_staff_access_session_id_fkey", "REFERENCES staff_access_sessions(id) ON DELETE RESTRICT"},
			{"preparation_state_corrections_preparation_unit_id_fkey", "REFERENCES preparation_units(id) ON DELETE RESTRICT"},
			{"preparation_state_corrections_actor_staff_identity_id_fkey", "REFERENCES staff_identities(id) ON DELETE RESTRICT"},
			{"preparation_state_corrections_staff_access_session_id_fkey", "REFERENCES staff_access_sessions(id) ON DELETE RESTRICT"},
		}
		for _, fk := range foreignKeys {
			clause := constraintDef(t, db, fk.name)
			require.Contains(t, clause, fk.references, "constraint %s", fk.name)
		}
	})

	t.Run("every correction fact carries actor, session, and timestamp evidence", func(t *testing.T) {
		type column struct {
			name     string
			nullable string
		}
		for _, tc := range []struct {
			table   string
			columns []column
		}{
			{
				table: "preparation_alerts",
				columns: []column{
					{"kind", "NO"}, {"reason", "NO"}, {"note", "YES"},
					{"created_by_staff_identity_id", "NO"},
					{"created_staff_access_session_id", "NO"}, {"created_at", "NO"},
					{"acknowledged_by_staff_identity_id", "YES"},
					{"acknowledged_staff_access_session_id", "YES"},
					{"acknowledged_at", "YES"},
				},
			},
			{
				table: "preparation_wastes",
				columns: []column{
					{"preparation_unit_id", "NO"}, {"prior_state", "NO"},
					{"reason", "NO"}, {"note", "YES"},
					{"actor_staff_identity_id", "NO"},
					{"staff_access_session_id", "NO"}, {"occurred_at", "NO"},
				},
			},
			{
				table: "preparation_remakes",
				columns: []column{
					{"waste_id", "NO"}, {"preparation_unit_id", "NO"},
					{"reason", "NO"}, {"note", "YES"},
					{"actor_staff_identity_id", "NO"},
					{"staff_access_session_id", "NO"}, {"created_at", "NO"},
				},
			},
			{
				table: "preparation_state_corrections",
				columns: []column{
					{"preparation_unit_id", "NO"}, {"prior_state", "NO"},
					{"resulting_state", "NO"}, {"reason", "NO"}, {"note", "YES"},
					{"actor_staff_identity_id", "NO"},
					{"staff_access_session_id", "NO"}, {"occurred_at", "NO"},
				},
			},
		} {
			for _, col := range tc.columns {
				var nullable string
				require.NoError(t, db.QueryRow(`
					SELECT is_nullable
					FROM information_schema.columns
					WHERE table_schema = 'public'
					  AND table_name = $1 AND column_name = $2`,
					tc.table, col.name).Scan(&nullable),
					"%s.%s must exist", tc.table, col.name)
				require.Equal(t, col.nullable, nullable, "%s.%s nullability", tc.table, col.name)
			}
		}
	})

	t.Run("the two spec indexes use the exact sort columns", func(t *testing.T) {
		require.Contains(t, indexDef(t, db, "preparation_alert_active_index"),
			"(acknowledged_at, created_at, id)")
		require.Contains(t, indexDef(t, db, "preparation_state_correction_unit_index"),
			"(preparation_unit_id, occurred_at, id)")
	})

	t.Run("alerts stay multi-event per unit", func(t *testing.T) {
		var unique int
		require.NoError(t, db.QueryRow(`
			SELECT count(*)
			FROM pg_index
			JOIN pg_class AS table_class ON table_class.oid = pg_index.indrelid
			WHERE table_class.relname = 'preparation_alerts'
			  AND pg_index.indisunique
			  AND NOT pg_index.indisprimary`).
			Scan(&unique))
		require.Equal(t, 0, unique,
			"one unit may accumulate several alerts over its history")
	})
}

// TestPreparationCorrectionsSchemaBackfillsStandardPriority emulates the
// pre-000013 schema, reruns the real migration, and proves an existing unit
// lands on STANDARD without a Remake link. The cleanup reruns the idempotent
// migration so a failed assertion cannot leave this package's clone downgraded.
func TestPreparationCorrectionsSchemaBackfillsStandardPriority(t *testing.T) {
	env := newPrepEnv(t)
	unit := env.SubmittedUnits(t, 1)[0]

	migration, err := os.ReadFile(filepath.Join(
		"..", "database", "migrations", "000013_add_preparation_corrections.sql",
	))
	require.NoError(t, err)

	t.Cleanup(func() {
		_, err := env.DB.Exec(string(migration))
		require.NoError(t, err)
	})

	_, err = env.DB.Exec(`
		DROP TABLE IF EXISTS preparation_state_corrections,
		                      preparation_remakes, preparation_wastes,
		                      preparation_alerts CASCADE;
		ALTER TABLE preparation_units DROP COLUMN IF EXISTS remake_of_preparation_unit_id;
		ALTER TABLE preparation_units DROP COLUMN IF EXISTS priority;`)
	require.NoError(t, err)

	_, err = env.DB.Exec(string(migration))
	require.NoError(t, err)

	var priority string
	var remakeOf uuid.NullUUID
	require.NoError(t, env.DB.QueryRow(
		`SELECT priority, remake_of_preparation_unit_id
		 FROM preparation_units WHERE id = $1`, unit.ID,
	).Scan(&priority, &remakeOf))
	require.Equal(t, "STANDARD", priority, "existing units backfill to STANDARD")
	require.False(t, remakeOf.Valid, "existing units carry no Remake link")
}

// TestPreparationCorrectionsSchemaEnforcesPriorityLinkPair proves the database
// accepts exactly STANDARD plus a null link and REMAKE plus a source link.
func TestPreparationCorrectionsSchemaEnforcesPriorityLinkPair(t *testing.T) {
	env := newPrepEnv(t)
	units := env.SubmittedUnits(t, 2)

	_, err := env.DB.Exec(`
		UPDATE preparation_units
		SET priority = 'REMAKE', remake_of_preparation_unit_id = $2
		WHERE id = $1`, units[0].ID, units[1].ID)
	require.NoError(t, err, "REMAKE plus a source link is the accepted pair")

	_, err = env.DB.Exec(`
		UPDATE preparation_units SET priority = 'STANDARD' WHERE id = $1`, units[0].ID)
	require.Error(t, err, "STANDARD plus a Remake link must be rejected")
	require.Contains(t, err.Error(), "preparation_unit_priority_link_valid")

	_, err = env.DB.Exec(`
		UPDATE preparation_units
		SET priority = 'REMAKE', remake_of_preparation_unit_id = NULL
		WHERE id = $1`, units[0].ID)
	require.Error(t, err, "REMAKE without a Remake link must be rejected")
	require.Contains(t, err.Error(), "preparation_unit_priority_link_valid")

	_, err = env.DB.Exec(`
		UPDATE preparation_units SET priority = 'RUSH' WHERE id = $1`, units[0].ID)
	require.Error(t, err, "an unknown priority must be rejected")
	// 'RUSH' violates both sibling checks at once and PostgreSQL does not
	// promise which one it reports, so either name proves the rejection.
	require.True(t,
		strings.Contains(err.Error(), "preparation_unit_priority_valid") ||
			strings.Contains(err.Error(), "preparation_unit_priority_link_valid"),
		"unknown priority must hit a priority check, got: %v", err)
}

// TestPreparationCorrectionsSchemaEnforcesAlertAcknowledgmentTuple proves the
// database accepts all-null or all-present acknowledgment evidence and rejects
// every partial tuple.
func TestPreparationCorrectionsSchemaEnforcesAlertAcknowledgmentTuple(t *testing.T) {
	env := newPrepEnv(t)
	unit := env.SubmittedUnits(t, 1)[0]
	actor := env.BaristaActor()

	insertAlert := func(ackBy, ackSession, ackAt any) error {
		_, err := env.DB.Exec(`
			INSERT INTO preparation_alerts (
				preparation_unit_id, kind, reason,
				created_by_staff_identity_id, created_staff_access_session_id, created_at,
				acknowledged_by_staff_identity_id,
				acknowledged_staff_access_session_id, acknowledged_at
			) VALUES ($1, 'WASTE', 'QUALITY_FAILURE', $2, $3, now(), $4, $5, $6)`,
			unit.ID, actor.StaffID, actor.SessionID, ackBy, ackSession, ackAt)
		return err
	}

	require.NoError(t, insertAlert(nil, nil, nil), "an unacknowledged alert is accepted")
	require.NoError(t, insertAlert(actor.StaffID, actor.SessionID, time.Now()),
		"a fully acknowledged alert is accepted")

	for _, partial := range []struct {
		name            string
		by, session, at any
	}{
		{"identity only", actor.StaffID, nil, nil},
		{"session only", nil, actor.SessionID, nil},
		{"time only", nil, nil, time.Now()},
		{"identity and session", actor.StaffID, actor.SessionID, nil},
		{"identity and time", actor.StaffID, nil, time.Now()},
		{"session and time", nil, actor.SessionID, time.Now()},
	} {
		err := insertAlert(partial.by, partial.session, partial.at)
		require.Error(t, err, "partial acknowledgment %q must be rejected", partial.name)
		require.Contains(t, err.Error(), "preparation_alert_acknowledgment_tuple_valid")
	}
}

// TestPreparationCorrectionsSchemaEnforcesReasonsAndNotes proves the alert
// reason check is kind-aware and that note length and OTHER-note rules hold at
// the database boundary.
func TestPreparationCorrectionsSchemaEnforcesReasonsAndNotes(t *testing.T) {
	env := newPrepEnv(t)
	unit := env.SubmittedUnits(t, 1)[0]
	actor := env.BaristaActor()

	insertAlert := func(kind, reason string, note any) error {
		_, err := env.DB.Exec(`
			INSERT INTO preparation_alerts (
				preparation_unit_id, kind, reason, note,
				created_by_staff_identity_id, created_staff_access_session_id, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, now())`,
			unit.ID, kind, reason, note, actor.StaffID, actor.SessionID)
		return err
	}

	t.Run("reserved Cancellation kinds use the Cancellation reason catalog", func(t *testing.T) {
		for _, kind := range []string{"CANCELLATION", "CHANGE"} {
			for _, reason := range []string{
				"CUSTOMER_REQUEST", "ORDER_ENTRY_ERROR", "ITEM_UNAVAILABLE",
			} {
				require.NoError(t, insertAlert(kind, reason, nil),
					"%s must accept %s", kind, reason)
			}
			for _, reason := range []string{
				"PREPARATION_ERROR", "QUALITY_FAILURE", "STATE_RECORDED_IN_ERROR",
			} {
				err := insertAlert(kind, reason, nil)
				require.Error(t, err, "%s must reject %s", kind, reason)
				require.Contains(t, err.Error(), "preparation_alert_reason_valid")
			}
		}
	})

	t.Run("WASTE uses only the Waste reason catalog", func(t *testing.T) {
		for _, reason := range []string{
			"PREPARATION_ERROR", "QUALITY_FAILURE", "CUSTOMER_REQUEST",
		} {
			require.NoError(t, insertAlert("WASTE", reason, nil),
				"WASTE must accept %s", reason)
		}
		for _, reason := range []string{
			"ORDER_ENTRY_ERROR", "ITEM_UNAVAILABLE", "STATE_RECORDED_IN_ERROR",
		} {
			err := insertAlert("WASTE", reason, nil)
			require.Error(t, err, "WASTE must reject %s", reason)
			require.Contains(t, err.Error(), "preparation_alert_reason_valid")
		}
	})

	t.Run("notes are bounded and OTHER requires one", func(t *testing.T) {
		for _, kind := range []string{"CANCELLATION", "CHANGE", "WASTE"} {
			err := insertAlert(kind, "OTHER", nil)
			require.Error(t, err, "%s OTHER without a note must be rejected", kind)
			require.Contains(t, err.Error(), "preparation_alert_other_note_valid")
		}

		err := insertAlert("WASTE", "QUALITY_FAILURE", "")
		require.Error(t, err, "a blank note must be rejected")
		require.Contains(t, err.Error(), "preparation_alert_note_valid")

		err = insertAlert("WASTE", "QUALITY_FAILURE", strings.Repeat("a", 501))
		require.Error(t, err, "a note over 500 characters must be rejected")
		require.Contains(t, err.Error(), "preparation_alert_note_valid")

		require.NoError(t, insertAlert("WASTE", "QUALITY_FAILURE", strings.Repeat("a", 500)),
			"a note of exactly 500 characters is accepted")
	})
}

// TestPreparationCorrectionsSchemaExtendsTransitionPairs proves the transition
// constraint admits the two Waste and three reverse-correction pairs alongside
// the three forward pairs, and still rejects skipped, exceptional-terminal, and
// Cancellation pairs.
func TestPreparationCorrectionsSchemaExtendsTransitionPairs(t *testing.T) {
	env := newPrepEnv(t)
	unit := env.SubmittedUnits(t, 1)[0]
	actor := env.BaristaActor()

	t.Run("the constraint declares all eight pairs", func(t *testing.T) {
		clause := constraintDef(t, env.DB, "preparation_unit_transition_states_valid")
		for _, state := range []string{
			"QUEUED", "IN_PREPARATION", "READY", "FULFILLED", "WASTED",
		} {
			require.Contains(t, clause, state)
		}
	})

	insertTransition := func(prior, resulting string) error {
		_, err := env.DB.Exec(`
			INSERT INTO preparation_unit_transitions (
				preparation_unit_id, prior_state, resulting_state,
				actor_staff_identity_id, staff_access_session_id, occurred_at
			) VALUES ($1, $2, $3, $4, $5, now())`,
			unit.ID, prior, resulting, actor.StaffID, actor.SessionID)
		return err
	}

	t.Run("the eight legal pairs are accepted", func(t *testing.T) {
		for _, pair := range [][2]string{
			{"QUEUED", "IN_PREPARATION"}, {"IN_PREPARATION", "READY"},
			{"READY", "FULFILLED"}, {"IN_PREPARATION", "WASTED"},
			{"READY", "WASTED"}, {"IN_PREPARATION", "QUEUED"},
			{"READY", "IN_PREPARATION"}, {"FULFILLED", "READY"},
		} {
			require.NoError(t, insertTransition(pair[0], pair[1]),
				"%s -> %s must be accepted", pair[0], pair[1])
		}
	})

	t.Run("skipped and exceptional pairs are rejected", func(t *testing.T) {
		for _, pair := range [][2]string{
			{"QUEUED", "READY"}, {"QUEUED", "FULFILLED"},
			{"IN_PREPARATION", "FULFILLED"}, {"QUEUED", "WASTED"},
			{"FULFILLED", "WASTED"}, {"QUEUED", "CANCELLED"},
			{"READY", "CANCELLED"}, {"IN_PREPARATION", "CANCELLED"},
			{"FULFILLED", "CANCELLED"}, {"WASTED", "QUEUED"},
			{"CANCELLED", "QUEUED"}, {"CANCELLED", "WASTED"},
		} {
			err := insertTransition(pair[0], pair[1])
			require.Error(t, err, "%s -> %s must be rejected", pair[0], pair[1])
			require.Contains(t, err.Error(), "preparation_unit_transition_states_valid")
		}
	})
}

// TestPreparationCorrectionsSchemaEnforcesOneWasteAndOneRemake proves the
// unique facts: one Waste per source unit, one Remake per Waste, and one
// Remake per replacement unit.
func TestPreparationCorrectionsSchemaEnforcesOneWasteAndOneRemake(t *testing.T) {
	env := newPrepEnv(t)
	units := env.SubmittedUnits(t, 2)
	actor := env.BaristaActor()

	insertWaste := func(unitID uuid.UUID) (uuid.UUID, error) {
		var wasteID uuid.UUID
		err := env.DB.QueryRow(`
			INSERT INTO preparation_wastes (
				preparation_unit_id, prior_state, reason,
				actor_staff_identity_id, staff_access_session_id, occurred_at
			) VALUES ($1, 'READY', 'QUALITY_FAILURE', $2, $3, now())
			RETURNING id`, unitID, actor.StaffID, actor.SessionID).Scan(&wasteID)
		return wasteID, err
	}

	wasteOne, err := insertWaste(units[0].ID)
	require.NoError(t, err)
	wasteTwo, err := insertWaste(units[1].ID)
	require.NoError(t, err)

	_, err = env.DB.Exec(`
		INSERT INTO preparation_wastes (
			preparation_unit_id, prior_state, reason,
			actor_staff_identity_id, staff_access_session_id, occurred_at
		) VALUES ($1, 'IN_PREPARATION', 'PREPARATION_ERROR', $2, $3, now())`,
		units[0].ID, actor.StaffID, actor.SessionID)
	require.Error(t, err, "a second Waste for one source unit must be rejected")
	require.Contains(t, err.Error(), "preparation_waste_unit_unique")

	insertRemake := func(wasteID, unitID uuid.UUID) error {
		_, err := env.DB.Exec(`
			INSERT INTO preparation_remakes (
				waste_id, preparation_unit_id, reason,
				actor_staff_identity_id, staff_access_session_id, created_at
			) VALUES ($1, $2, 'PREPARATION_ERROR', $3, $4, now())`,
			wasteID, unitID, actor.StaffID, actor.SessionID)
		return err
	}

	require.NoError(t, insertRemake(wasteOne, units[1].ID),
		"the first Remake of a Waste is accepted")

	err = insertRemake(wasteOne, units[0].ID)
	require.Error(t, err, "a second Remake of one Waste must be rejected")
	require.Contains(t, err.Error(), "preparation_remake_waste_unique")

	err = insertRemake(wasteTwo, units[1].ID)
	require.Error(t, err, "a second Remake reusing one replacement unit must be rejected")
	require.Contains(t, err.Error(), "preparation_remake_unit_unique")
}
