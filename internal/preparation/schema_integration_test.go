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
	// The emulation drops 6B objects with CASCADE, which also removes any
	// later migration's object that references them, and rerunning 000013
	// rebuilds the transition constraint without 000014's pair. Cleanup
	// therefore replays both migrations in order, leaving the package clone in
	// its fully migrated state for the tests that follow.
	laterMigration, err := os.ReadFile(filepath.Join(
		"..", "database", "migrations", "000014_add_preparation_financial_corrections.sql",
	))
	require.NoError(t, err)

	t.Cleanup(func() {
		_, err := env.DB.Exec(string(migration))
		require.NoError(t, err)
		_, err = env.DB.Exec(string(laterMigration))
		require.NoError(t, err)
	})

	// All later-migration tables are dropped too. A CASCADE of only the 6B
	// tables would leave charge_adjustments alive with its Waste foreign key
	// amputated, and CREATE TABLE IF NOT EXISTS cannot repair that.
	_, err = env.DB.Exec(`
		DROP TABLE IF EXISTS refund_adjustment_allocations, sales_comps,
		                      preparation_cancellations, charge_adjustments,
		                      preparation_state_corrections,
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
// constraint admits the two Waste, three reverse-correction, and one
// Cancellation pairs alongside the three forward pairs, and still rejects
// skipped and exceptional-terminal pairs.
func TestPreparationCorrectionsSchemaExtendsTransitionPairs(t *testing.T) {
	env := newPrepEnv(t)
	unit := env.SubmittedUnits(t, 1)[0]
	actor := env.BaristaActor()

	t.Run("the constraint declares all nine pairs", func(t *testing.T) {
		clause := constraintDef(t, env.DB, "preparation_unit_transition_states_valid")
		for _, state := range []string{
			"QUEUED", "IN_PREPARATION", "READY", "FULFILLED", "WASTED", "CANCELLED",
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

	t.Run("the nine legal pairs are accepted", func(t *testing.T) {
		for _, pair := range [][2]string{
			{"QUEUED", "IN_PREPARATION"}, {"IN_PREPARATION", "READY"},
			{"READY", "FULFILLED"}, {"IN_PREPARATION", "WASTED"},
			{"READY", "WASTED"}, {"IN_PREPARATION", "QUEUED"},
			{"READY", "IN_PREPARATION"}, {"FULFILLED", "READY"},
			{"QUEUED", "CANCELLED"},
		} {
			require.NoError(t, insertTransition(pair[0], pair[1]),
				"%s -> %s must be accepted", pair[0], pair[1])
		}
	})

	t.Run("skipped and exceptional pairs are rejected", func(t *testing.T) {
		for _, pair := range [][2]string{
			{"QUEUED", "READY"}, {"QUEUED", "FULFILLED"},
			{"IN_PREPARATION", "FULFILLED"}, {"QUEUED", "WASTED"},
			{"FULFILLED", "WASTED"},
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

// --- Phase 6C: Cancellation persistence ---

// cancellationFixture resolves the immutable financial target of one submitted
// unit: the Check it was charged to, its first original Charge Allocation, the
// committed per-unit price, and the ids the Cancellation inserts need. A
// Completed Sale row for the still-active Session is inserted directly because
// the schema contract must exercise the POST_SALE pairing without a closure
// command.
type cancellationFixture struct {
	Env             *prepEnv
	UnitIDs         []uuid.UUID
	CheckID         uuid.UUID
	AllocationID    uuid.UUID
	OrderID         uuid.UUID
	CompletedSaleID uuid.UUID
	PriceVND        int64
}

func newCancellationFixture(t *testing.T, units int32) cancellationFixture {
	t.Helper()
	env := newPrepEnv(t)
	submitted := env.SubmittedUnits(t, units)
	require.NotEmpty(t, submitted)

	fixture := cancellationFixture{Env: env}
	for _, unit := range submitted {
		fixture.UnitIDs = append(fixture.UnitIDs, unit.ID)
	}
	first := fixture.UnitIDs[0]
	require.NoError(t, env.DB.QueryRow(`
		SELECT ca.check_id, ca.id, ci.unit_price_vnd
		FROM preparation_units pu
		JOIN order_items oi ON oi.id = pu.order_item_id
		JOIN charge_allocations ca ON ca.committed_item_id = oi.committed_item_id
		JOIN committed_items ci ON ci.id = ca.committed_item_id
		WHERE pu.id = $1
		ORDER BY ca.created_at ASC, ca.id ASC
		LIMIT 1`, first).
		Scan(&fixture.CheckID, &fixture.AllocationID, &fixture.PriceVND))
	require.NoError(t, env.DB.QueryRow(`
		SELECT o.id
		FROM preparation_units pu
		JOIN order_items oi ON oi.id = pu.order_item_id
		JOIN orders o ON o.id = oi.order_id
		WHERE pu.id = $1`, first).Scan(&fixture.OrderID))
	actor := env.ManagerActor()
	require.NoError(t, env.DB.QueryRow(`
		INSERT INTO completed_sales (service_session_id, completed_by_staff_identity_id,
		                             completed_staff_access_session_id)
		VALUES ($1, $2, $3) RETURNING id`,
		env.SessionIDForUnit(t, first), actor.StaffID, actor.SessionID).
		Scan(&fixture.CompletedSaleID))
	return fixture
}

// insertChargeAdjustment writes one raw Charge Adjustment row and returns the
// error untouched, so a rejection case proves the database refuses it.
func insertChargeAdjustment(t *testing.T, env *prepEnv, fixture cancellationFixture,
	kind, scope string, wasteID, completedSaleID any, amountVND int64,
) (uuid.UUID, error) {
	t.Helper()
	var id uuid.UUID
	err := env.DB.QueryRow(`
		INSERT INTO charge_adjustments (kind, scope, preparation_unit_id,
			preparation_waste_id, charge_allocation_id, check_id,
			completed_sale_id, sales_shift_id, amount_vnd)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id`,
		kind, scope, fixture.UnitIDs[0], wasteID, fixture.AllocationID,
		fixture.CheckID, completedSaleID, env.ShiftID, amountVND).Scan(&id)
	return id, err
}

// insertPreparationCancellation writes one raw Cancellation fact row.
func insertPreparationCancellation(t *testing.T, env *prepEnv, unitID uuid.UUID,
	kind string, adjustmentID, replacementOrderID any, reason string, note any,
) (uuid.UUID, error) {
	t.Helper()
	actor := env.ManagerActor()
	var id uuid.UUID
	err := env.DB.QueryRow(`
		INSERT INTO preparation_cancellations (preparation_unit_id, kind,
			charge_adjustment_id, replacement_order_id, reason, note,
			actor_staff_identity_id, staff_access_session_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id`,
		unitID, kind, adjustmentID, replacementOrderID, reason, note,
		actor.StaffID, actor.SessionID).Scan(&id)
	return id, err
}

// insertWasteForUnit records one Waste fact directly, for fixtures that need a
// valid Comp source.
func insertWasteForUnit(t *testing.T, env *prepEnv, unitID uuid.UUID) uuid.UUID {
	t.Helper()
	actor := env.ManagerActor()
	var id uuid.UUID
	require.NoError(t, env.DB.QueryRow(`
		INSERT INTO preparation_wastes (preparation_unit_id, prior_state, reason,
			actor_staff_identity_id, staff_access_session_id)
		VALUES ($1, 'READY', 'QUALITY_FAILURE', $2, $3)
		RETURNING id`, unitID, actor.StaffID, actor.SessionID).Scan(&id))
	return id
}

// TestPreparationFinancialCorrectionsSchema proves migration 000014 created
// the Phase 6C Cancellation facts, their foreign keys, named constraints, and
// indexes, extended the Preparation Unit transition graph with exactly
// QUEUED -> CANCELLED, and that the database rejects every invalid fact shape.
func TestPreparationFinancialCorrectionsSchema(t *testing.T) {
	db, _ := openPrepTestDB(t)

	t.Run("both Cancellation fact tables exist", func(t *testing.T) {
		var tables int
		require.NoError(t, db.QueryRow(`
			SELECT count(*) FROM information_schema.tables
			WHERE table_schema = 'public'
			  AND table_name IN ('charge_adjustments', 'preparation_cancellations')`).
			Scan(&tables))
		require.Equal(t, 2, tables)
	})

	t.Run("every foreign key restricts deletion", func(t *testing.T) {
		foreignKeys := []struct {
			name       string
			references string
		}{
			{"charge_adjustments_preparation_unit_id_fkey", "REFERENCES preparation_units(id) ON DELETE RESTRICT"},
			{"charge_adjustments_preparation_waste_id_fkey", "REFERENCES preparation_wastes(id) ON DELETE RESTRICT"},
			{"charge_adjustments_charge_allocation_id_fkey", "REFERENCES charge_allocations(id) ON DELETE RESTRICT"},
			{"charge_adjustments_check_id_fkey", "REFERENCES checks(id) ON DELETE RESTRICT"},
			{"charge_adjustments_completed_sale_id_fkey", "REFERENCES completed_sales(id) ON DELETE RESTRICT"},
			{"charge_adjustments_sales_shift_id_fkey", "REFERENCES sales_shifts(id) ON DELETE RESTRICT"},
			{"preparation_cancellations_preparation_unit_id_fkey", "REFERENCES preparation_units(id) ON DELETE RESTRICT"},
			{"preparation_cancellations_charge_adjustment_id_fkey", "REFERENCES charge_adjustments(id) ON DELETE RESTRICT"},
			{"preparation_cancellations_replacement_order_id_fkey", "REFERENCES orders(id) ON DELETE RESTRICT"},
			{"preparation_cancellations_actor_staff_identity_id_fkey", "REFERENCES staff_identities(id) ON DELETE RESTRICT"},
			{"preparation_cancellations_staff_access_session_id_fkey", "REFERENCES staff_access_sessions(id) ON DELETE RESTRICT"},
		}
		for _, fk := range foreignKeys {
			clause := constraintDef(t, db, fk.name)
			require.Contains(t, clause, fk.references, "constraint %s", fk.name)
		}
	})

	t.Run("the named check and unique constraints exist", func(t *testing.T) {
		require.Contains(t, constraintDef(t, db, "charge_adjustment_kind_source_valid"),
			"preparation_waste_id")
		require.Contains(t, constraintDef(t, db, "charge_adjustment_scope_valid"),
			"completed_sale_id")
		require.Contains(t, constraintDef(t, db, "charge_adjustment_amount_positive"),
			"amount_vnd > 0")
		require.Contains(t, constraintDef(t, db, "charge_adjustment_kind_unit_unique"),
			"UNIQUE (kind, preparation_unit_id)")
		require.Contains(t, constraintDef(t, db, "preparation_cancellation_unit_unique"),
			"UNIQUE (preparation_unit_id)")
		require.Contains(t, constraintDef(t, db, "preparation_cancellation_adjustment_unique"),
			"UNIQUE (charge_adjustment_id)")
		require.Contains(t, constraintDef(t, db, "preparation_cancellation_kind_replacement_valid"),
			"replacement_order_id")
		reason := constraintDef(t, db, "preparation_cancellation_reason_valid")
		for _, allowed := range []string{
			"CUSTOMER_REQUEST", "ORDER_ENTRY_ERROR", "ITEM_UNAVAILABLE", "OTHER",
		} {
			require.Contains(t, reason, allowed)
		}
		require.Contains(t, constraintDef(t, db, "preparation_cancellation_note_valid"), "500")
		require.Contains(t, constraintDef(t, db, "preparation_cancellation_other_note_valid"),
			"btrim")
	})

	t.Run("the required indexes exist", func(t *testing.T) {
		require.Contains(t, indexDef(t, db, "charge_adjustment_check_index"),
			"(check_id, created_at, id)")
		require.Contains(t, indexDef(t, db, "charge_adjustment_completed_sale_index"),
			"(completed_sale_id, created_at, id)")
		require.Contains(t, indexDef(t, db, "charge_adjustment_shift_index"),
			"(sales_shift_id, created_at, id)")
		require.Contains(t, indexDef(t, db, "charge_adjustment_unit_index"),
			"(preparation_unit_id, created_at, id)")
		require.Contains(t, indexDef(t, db, "charge_adjustment_waste_index"),
			"(preparation_waste_id, created_at, id)")
		require.Contains(t, indexDef(t, db, "preparation_cancellation_occurred_index"),
			"(occurred_at, id)")
	})

	t.Run("the transition constraint declares the QUEUED to CANCELLED pair", func(t *testing.T) {
		clause := strings.Join(strings.Fields(
			constraintDef(t, db, "preparation_unit_transition_states_valid")), " ")
		require.Contains(t, clause,
			"(prior_state = 'QUEUED'::text) AND (resulting_state = 'CANCELLED'::text)")
	})

	t.Run("the database accepts the new pair and rejects undeclared ones", func(t *testing.T) {
		fixture := newCancellationFixture(t, 1)
		actor := fixture.Env.ManagerActor()
		insertTransition := func(prior, resulting string) error {
			_, err := fixture.Env.DB.Exec(`
				INSERT INTO preparation_unit_transitions (
					preparation_unit_id, prior_state, resulting_state,
					actor_staff_identity_id, staff_access_session_id, occurred_at
				) VALUES ($1, $2, $3, $4, $5, now())`,
				fixture.UnitIDs[0], prior, resulting, actor.StaffID, actor.SessionID)
			return err
		}
		require.NoError(t, insertTransition("QUEUED", "CANCELLED"),
			"QUEUED -> CANCELLED is the new accepted pair")
		err := insertTransition("READY", "CANCELLED")
		require.Error(t, err, "READY -> CANCELLED is still undeclared")
		require.Contains(t, err.Error(), "preparation_unit_transition_states_valid")
	})

	t.Run("charge adjustment kind, scope, and amount pairings are enforced", func(t *testing.T) {
		fixture := newCancellationFixture(t, 1)
		env := fixture.Env

		for _, tc := range []struct {
			name       string
			kind       string
			scope      string
			waste      any
			sale       any
			amount     int64
			constraint string
		}{
			{"unknown kind", "DISCOUNT", "LIVE_CHECK", nil, nil, 1000,
				"charge_adjustment_kind_source_valid"},
			{"Cancellation with a Waste", "CANCELLATION", "LIVE_CHECK", uuid.New(), nil, 1000,
				"charge_adjustment_kind_source_valid"},
			{"Comp without a Waste", "COMP", "LIVE_CHECK", nil, nil, 1000,
				"charge_adjustment_kind_source_valid"},
			{"unknown scope", "CANCELLATION", "SALE", nil, nil, 1000,
				"charge_adjustment_scope_valid"},
			{"live scope with a Completed Sale", "CANCELLATION", "LIVE_CHECK", nil,
				fixture.CompletedSaleID, 1000, "charge_adjustment_scope_valid"},
			{"post-sale scope without a Completed Sale", "CANCELLATION", "POST_SALE", nil, nil,
				1000, "charge_adjustment_scope_valid"},
			{"zero amount", "CANCELLATION", "LIVE_CHECK", nil, nil, 0,
				"charge_adjustment_amount_positive"},
		} {
			_, err := insertChargeAdjustment(t, env, fixture, tc.kind, tc.scope,
				tc.waste, tc.sale, tc.amount)
			require.Error(t, err, tc.name)
			require.Contains(t, err.Error(), tc.constraint, tc.name)
		}

		_, err := insertChargeAdjustment(t, env, fixture, "CANCELLATION", "LIVE_CHECK",
			nil, nil, fixture.PriceVND)
		require.NoError(t, err, "a live Cancellation adjustment is the accepted shape")
		_, err = insertChargeAdjustment(t, env, fixture, "CANCELLATION", "LIVE_CHECK",
			nil, nil, fixture.PriceVND)
		require.Error(t, err, "a second adjustment for one unit and kind must be rejected")
		require.Contains(t, err.Error(), "charge_adjustment_kind_unit_unique")
	})

	t.Run("a Comp sourced adjustment is accepted on a Wasted unit", func(t *testing.T) {
		fixture := newCancellationFixture(t, 1)
		wasteID := insertWasteForUnit(t, fixture.Env, fixture.UnitIDs[0])
		_, err := insertChargeAdjustment(t, fixture.Env, fixture, "COMP", "LIVE_CHECK",
			wasteID, nil, fixture.PriceVND)
		require.NoError(t, err, "COMP requires a Waste and accepts a live scope")
	})

	t.Run("Cancellation facts enforce kind, reason, note, and uniqueness", func(t *testing.T) {
		fixture := newCancellationFixture(t, 2)
		env := fixture.Env
		first, second := fixture.UnitIDs[0], fixture.UnitIDs[1]

		adjustmentID, err := insertChargeAdjustment(t, env, fixture,
			"CANCELLATION", "LIVE_CHECK", nil, nil, fixture.PriceVND)
		require.NoError(t, err)

		_, err = insertPreparationCancellation(t, env, first, "CHANGE", nil, nil,
			"CUSTOMER_REQUEST", nil)
		require.Error(t, err, "CHANGE without a replacement Order must be rejected")
		require.Contains(t, err.Error(), "preparation_cancellation_kind_replacement_valid")

		_, err = insertPreparationCancellation(t, env, first, "CANCELLATION", nil,
			fixture.OrderID, "CUSTOMER_REQUEST", nil)
		require.Error(t, err, "CANCELLATION with a replacement Order must be rejected")
		require.Contains(t, err.Error(), "preparation_cancellation_kind_replacement_valid")

		_, err = insertPreparationCancellation(t, env, first, "CANCELLATION", nil, nil,
			"STATE_RECORDED_IN_ERROR", nil)
		require.Error(t, err, "a State Correction reason is not a Cancellation reason")
		require.Contains(t, err.Error(), "preparation_cancellation_reason_valid")

		_, err = insertPreparationCancellation(t, env, first, "CANCELLATION", nil, nil,
			"OTHER", nil)
		require.Error(t, err, "OTHER without a note must be rejected")
		require.Contains(t, err.Error(), "preparation_cancellation_other_note_valid")

		_, err = insertPreparationCancellation(t, env, first, "CANCELLATION", nil, nil,
			"CUSTOMER_REQUEST", "")
		require.Error(t, err, "a blank note must be rejected")
		require.Contains(t, err.Error(), "preparation_cancellation_note_valid")

		_, err = insertPreparationCancellation(t, env, first, "CANCELLATION", nil, nil,
			"CUSTOMER_REQUEST", strings.Repeat("a", 501))
		require.Error(t, err, "a note over 500 characters must be rejected")
		require.Contains(t, err.Error(), "preparation_cancellation_note_valid")

		_, err = insertPreparationCancellation(t, env, first, "CANCELLATION", adjustmentID,
			nil, "CUSTOMER_REQUEST", nil)
		require.NoError(t, err, "one Cancellation per unit is the accepted shape")

		_, err = insertPreparationCancellation(t, env, first, "CHANGE", nil,
			fixture.OrderID, "CUSTOMER_REQUEST", nil)
		require.Error(t, err, "a second Cancellation for one unit must be rejected")
		require.Contains(t, err.Error(), "preparation_cancellation_unit_unique")

		_, err = insertPreparationCancellation(t, env, second, "CANCELLATION", adjustmentID,
			nil, "CUSTOMER_REQUEST", nil)
		require.Error(t, err, "a second Cancellation naming one adjustment must be rejected")
		require.Contains(t, err.Error(), "preparation_cancellation_adjustment_unique")

		_, err = insertPreparationCancellation(t, env, second, "CANCELLATION", nil,
			nil, "CUSTOMER_REQUEST", strings.Repeat("a", 500))
		require.NoError(t, err, "a note of exactly 500 characters is accepted")
	})
}
