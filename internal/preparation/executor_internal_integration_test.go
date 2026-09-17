//go:build integration

// Internal (in-package) integration test: writePreparationAudits is
// package-private and has no exported caller until Tasks 4-7 wire their
// handlers to it, so its batch atomicity cannot be exercised through any
// exported path. The file joins the package under test — the same approach
// domain_test.go takes for the package-private fingerprint — and receives the
// shared clone pool from the external test main through SetInternalTestDB.
package preparation

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// internalTestDB holds the shared integration pool the external test main
// binds once, before any test runs. It is the same pool the external fixtures
// use, so this file never opens its own database.
var internalTestDB *sql.DB

// SetInternalTestDB hands the package's shared integration pool to the
// in-package integration tests, which cannot reach the external test
// package's unexported fixtures. Only the external test main calls it.
func SetInternalTestDB(db *sql.DB) { internalTestDB = db }

func TestWritePreparationAuditsIsAtomic(t *testing.T) {
	require.NotNil(t, internalTestDB, "the external test main must share the package pool")
	ctx := context.Background()
	q := sqlc.New(internalTestDB)

	// A minimal enabled identity and active session satisfy audit_events'
	// actor and session foreign keys; the batch writer itself never touches
	// the PIN hash.
	pinHash, err := auth.HashPin("1234")
	require.NoError(t, err)
	code := "T" + strings.ReplaceAll(uuid.NewString(), "-", "")[:23]
	identity, err := q.CreateStaffIdentity(ctx, sqlc.CreateStaffIdentityParams{
		DisplayName: "Audit batch probe " + code,
		Btrim:       code,
		PinHash:     pinHash,
		Enabled:     true,
	})
	require.NoError(t, err)
	session, err := q.CreateStaffSession(ctx, sqlc.CreateStaffSessionParams{
		TokenHash:           "tok_" + uuid.NewString()[:16],
		StaffIdentityID:     identity.ID,
		State:               auth.SessionStateActive,
		LastHumanActivityAt: time.Now(),
		ExpiresAt:           time.Now().Add(time.Hour),
	})
	require.NoError(t, err)
	actor := Actor{StaffID: identity.ID, SessionID: session.ID}

	// Probe event types are unique to this test file; the run marker in every
	// details payload isolates one subtest's rows from the others and from
	// earlier runs.
	const (
		probeFirst  = "preparation.audit_batch_probe_first"
		probeSecond = "preparation.audit_batch_probe_second"
	)
	newRunID := func() string { return uuid.NewString()[:8] }
	countRun := func(t *testing.T, runID string, eventTypes ...string) int {
		t.Helper()
		var n int
		require.NoError(t, internalTestDB.QueryRow(`
			SELECT count(*) FROM audit_events
			WHERE event_type = ANY($1) AND details->>'run' = $2`,
			eventTypes, runID).Scan(&n))
		return n
	}

	t.Run("empty input is a no-op", func(t *testing.T) {
		runID := newRunID()
		require.NoError(t, writePreparationAudits(ctx, q, actor, time.Now(), nil))
		require.NoError(t, writePreparationAudits(ctx, q, actor, time.Now(), []AuditRecord{}))
		require.Equal(t, 0, countRun(t, runID, probeFirst, probeSecond))
	})

	t.Run("one batch persists both event types with one actor, session, and time", func(t *testing.T) {
		runID := newRunID()
		occurredAt := time.Now()

		err := writePreparationAudits(ctx, q, actor, occurredAt, []AuditRecord{
			{EventType: probeFirst, Details: map[string]any{"run": runID}},
			{EventType: probeSecond, Details: map[string]any{"run": runID}},
		})
		require.NoError(t, err)
		require.Equal(t, 2, countRun(t, runID, probeFirst, probeSecond))

		// One actor, one session, and one shared occurrence time across the
		// whole batch, whatever each row's event type is.
		var actors, sessions, stamps int
		require.NoError(t, internalTestDB.QueryRow(`
			SELECT count(DISTINCT actor_id), count(DISTINCT session_id),
			       count(DISTINCT occurred_at)
			FROM audit_events
			WHERE event_type = ANY($1) AND details->>'run' = $2`,
			[]string{probeFirst, probeSecond}, runID).Scan(&actors, &sessions, &stamps))
		require.Equal(t, 1, actors)
		require.Equal(t, 1, sessions)
		require.Equal(t, 1, stamps)
	})

	t.Run("second-row failure persists nothing", func(t *testing.T) {
		runID := newRunID()

		// The trigger fails the batch's second row — the record carrying the
		// force_fail marker — exactly like the package's other forced-failure
		// triggers.
		_, err := internalTestDB.Exec(`
			CREATE FUNCTION fail_second_preparation_audit_row() RETURNS trigger AS $$
			BEGIN
				IF NEW.details->>'force_fail' = 'true' THEN
					RAISE EXCEPTION 'forced second batch row failure';
				END IF;
				RETURN NEW;
			END;
			$$ LANGUAGE plpgsql;
			CREATE TRIGGER fail_second_preparation_audit_row
			BEFORE INSERT ON audit_events
			FOR EACH ROW EXECUTE FUNCTION fail_second_preparation_audit_row();`)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = internalTestDB.Exec(`DROP TRIGGER IF EXISTS fail_second_preparation_audit_row ON audit_events`)
			_, _ = internalTestDB.Exec(`DROP FUNCTION IF EXISTS fail_second_preparation_audit_row()`)
		})

		err = writePreparationAudits(ctx, q, actor, time.Now(), []AuditRecord{
			{EventType: probeFirst, Details: map[string]any{"run": runID}},
			{EventType: probeSecond, Details: map[string]any{"run": runID, "force_fail": "true"}},
		})
		require.Error(t, err, "the forced second-row failure must surface to the caller")

		// The batch is one statement, so it is all-or-nothing: the good first
		// row is rolled back together with the failed second row.
		require.Equal(t, 0, countRun(t, runID, probeFirst, probeSecond))
	})
}
