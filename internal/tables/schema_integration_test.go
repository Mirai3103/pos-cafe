//go:build integration

package tables_test

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/database"
	"github.com/stretchr/testify/require"
)

func openSchemaTestDB(t *testing.T) *sql.DB {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	require.Contains(t, url, "_test")
	db, err := database.Open(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

// seedClosedSalesShift inserts a Sales Shift in the CLOSED state so Service
// Session fixtures can satisfy the sales_shift_id NOT NULL column (Phase 5A)
// without tripping the one-open-Shift invariant.
func seedClosedSalesShift(t *testing.T, db *sql.DB, staffID any) string {
	t.Helper()
	var id string
	require.NoError(t, db.QueryRow(
		`INSERT INTO sales_shifts (state, opened_by_staff_identity_id, opening_float_vnd)
		 VALUES ('CLOSED', $1, 0) RETURNING id`, staffID).Scan(&id))
	return id
}

func TestTablesSchemaExists(t *testing.T) {
	db := openSchemaTestDB(t)
	ctx := context.Background()

	for _, name := range []string{"tables", "service_sessions", "table_assignments"} {
		var exists bool
		err := db.QueryRowContext(ctx,
			`SELECT EXISTS (SELECT 1 FROM information_schema.tables
			 WHERE table_schema = 'public' AND table_name = $1)`, name).Scan(&exists)
		require.NoError(t, err)
		require.True(t, exists, "table %q must exist", name)
	}
}

func TestTableNameConstraints(t *testing.T) {
	db := openSchemaTestDB(t)
	ctx := context.Background()

	// Untrimmed name is rejected.
	_, err := db.ExecContext(ctx,
		`INSERT INTO tables (name, normalized_name) VALUES (' Ban 1 ', ' ban 1 ')`)
	require.Error(t, err, "untrimmed name must violate table_name_valid")

	// normalized_name must equal lower(name).
	_, err = db.ExecContext(ctx,
		`INSERT INTO tables (name, normalized_name) VALUES ('Ban 1', 'BAN 1')`)
	require.Error(t, err, "mismatched normalized_name must be rejected")

	// Empty name is rejected.
	_, err = db.ExecContext(ctx,
		`INSERT INTO tables (name, normalized_name) VALUES ('', '')`)
	require.Error(t, err, "empty name must be rejected")

	// A valid row inserts, and a duplicate normalized name is rejected.
	var id string
	err = db.QueryRowContext(ctx,
		`INSERT INTO tables (name, normalized_name) VALUES ('Ban Schema A', 'ban schema a')
		 RETURNING id`).Scan(&id)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM tables WHERE id = $1`, id) })

	_, err = db.ExecContext(ctx,
		`INSERT INTO tables (name, normalized_name) VALUES ('Ban Schema A', 'ban schema a')`)
	require.Error(t, err, "duplicate normalized_name must be rejected")
}

func TestTableAssignmentReleaseEvidenceConstraint(t *testing.T) {
	db := openSchemaTestDB(t)
	ctx := context.Background()

	var staffID string
	err := db.QueryRowContext(ctx,
		`INSERT INTO staff_identities (display_name, login_code, pin_hash, enabled)
		 VALUES ('Schema Actor', $1, '', true) RETURNING id`, testLoginCode("SCH")).Scan(&staffID)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM staff_identities WHERE id = $1`, staffID) })

	// Cleanup is registered before the inserts so an abort mid-test still
	// removes whatever rows were created; the unique table name must stay
	// reusable on the next run.
	var tableID, shiftID, sessionID string
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM table_assignments WHERE table_id = $1`, tableID)
		_, _ = db.ExecContext(ctx, `DELETE FROM service_sessions WHERE id = $1`, sessionID)
		_, _ = db.ExecContext(ctx, `DELETE FROM sales_shifts WHERE id = $1`, shiftID)
		_, _ = db.ExecContext(ctx, `DELETE FROM tables WHERE id = $1`, tableID)
	})

	shiftID = seedClosedSalesShift(t, db, staffID)

	err = db.QueryRowContext(ctx,
		`INSERT INTO tables (name, normalized_name) VALUES ('Ban Schema B', 'ban schema b')
		 RETURNING id`).Scan(&tableID)
	require.NoError(t, err)

	err = db.QueryRowContext(ctx,
		`INSERT INTO service_sessions (service_number, sequence, service_mode, state, created_by_staff_identity_id, sales_shift_id)
		 VALUES ($1, 1, 'DINE_IN', 'ACTIVE', $2, $3) RETURNING id`,
		randomServiceNumber(), staffID, shiftID).Scan(&sessionID)
	require.NoError(t, err)

	// released_at without released_by violates the evidence check.
	_, err = db.ExecContext(ctx,
		`INSERT INTO table_assignments
		   (table_id, service_session_id, assigned_by_staff_identity_id, sequence, released_at)
		 VALUES ($1, $2, $3, 1, now())`, tableID, sessionID, staffID)
	require.Error(t, err, "released_at without released_by must be rejected")

	// A current assignment inserts.
	_, err = db.ExecContext(ctx,
		`INSERT INTO table_assignments
		   (table_id, service_session_id, assigned_by_staff_identity_id, sequence)
		 VALUES ($1, $2, $3, 1)`, tableID, sessionID, staffID)
	require.NoError(t, err)

	// A second current assignment for the same pair violates the partial unique index.
	_, err = db.ExecContext(ctx,
		`INSERT INTO table_assignments
		   (table_id, service_session_id, assigned_by_staff_identity_id, sequence)
		 VALUES ($1, $2, $3, 2)`, tableID, sessionID, staffID)
	require.Error(t, err, "duplicate current assignment must be rejected")
}

func TestServiceSessionNumberConstraint(t *testing.T) {
	db := openSchemaTestDB(t)
	ctx := context.Background()

	var staffID string
	err := db.QueryRowContext(ctx,
		`INSERT INTO staff_identities (display_name, login_code, pin_hash, enabled)
		 VALUES ('Schema Actor 2', $1, '', true) RETURNING id`, testLoginCode("SCH")).Scan(&staffID)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM staff_identities WHERE id = $1`, staffID) })

	shiftID := seedClosedSalesShift(t, db, staffID)
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM sales_shifts WHERE id = $1`, shiftID) })

	// Lowercase and wrong-length service numbers are rejected.
	_, err = db.ExecContext(ctx,
		`INSERT INTO service_sessions (service_number, sequence, created_by_staff_identity_id, sales_shift_id)
		 VALUES ('abc123', 1, $1, $2)`, staffID, shiftID)
	require.Error(t, err, "lowercase service_number must be rejected")

	_, err = db.ExecContext(ctx,
		`INSERT INTO service_sessions (service_number, sequence, created_by_staff_identity_id, sales_shift_id)
		 VALUES ('AB12', 1, $1, $2)`, staffID, shiftID)
	require.Error(t, err, "short service_number must be rejected")

	// An invalid state is rejected.
	_, err = db.ExecContext(ctx,
		`INSERT INTO service_sessions (service_number, sequence, state, created_by_staff_identity_id, sales_shift_id)
		 VALUES ($1, 1, 'PENDING', $2, $3)`, randomServiceNumber(), staffID, shiftID)
	require.Error(t, err, "unknown state must be rejected")
}
