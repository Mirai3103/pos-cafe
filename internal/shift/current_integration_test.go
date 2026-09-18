//go:build integration

package shift_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/shift"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCurrentShiftReturnsNilWhenNoneOpen(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	ctx := context.Background()

	cashier := newTestActor(t, q, []string{"CASHIER"}, true)

	res, err := shift.NewCurrentShiftHandler(shift.NewRunner(db, q)).Handle(ctx, cashier.actor())
	require.NoError(t, err, "no open Shift is a normal state, not an error")
	assert.Nil(t, res)
}

// TestCurrentShiftReturnsRedactedOpenShape asserts the OPEN read's redacted
// contract: id, state, opened_at, and opener only. The Opening Float, Expected
// Cash, Cash Movements, and Refunds must not appear before the blind initial
// count commits (spec 4.1); the raw-JSON key allowlist is asserted by the HTTP
// tests.
func TestCurrentShiftReturnsRedactedOpenShape(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()

	res, err := shift.NewCurrentShiftHandler(f.Runner).Handle(ctx, f.Cashier.actor())
	require.NoError(t, err)
	require.NotNil(t, res)

	assert.Equal(t, f.Shift.ID, res.ID)
	assert.Equal(t, shift.StateOpen, res.State)
	assert.False(t, res.OpenedAt.IsZero())
	assert.Equal(t, f.Cashier.StaffID, res.Opener.ID)
}

func TestCurrentShiftIgnoresClosedShifts(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()

	_, err := f.DB.Exec(`UPDATE sales_shifts SET state = 'CLOSED' WHERE id = $1`, f.Shift.ID)
	require.NoError(t, err)

	res, err := shift.NewCurrentShiftHandler(f.Runner).Handle(ctx, f.Cashier.actor())
	require.NoError(t, err)
	assert.Nil(t, res)
}

// countDenialAuditEvents counts shift.authorization_denied events attributed
// to a staff identity. Its transaction is separate from the read that denies,
// since ExecuteRead's own transaction is read-only.
func countDenialAuditEvents(t *testing.T, db *sql.DB, staffID uuid.UUID) int {
	t.Helper()
	var n int
	err := db.QueryRow(
		`SELECT count(*) FROM audit_events WHERE event_type = $1 AND actor_id = $2`,
		shift.EventAuthorizationDenied, staffID,
	).Scan(&n)
	require.NoError(t, err)
	return n
}

func TestCurrentShiftDeniesBarista(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	ctx := context.Background()

	barista := newTestActor(t, q, []string{"BARISTA"}, true)

	_, err := shift.NewCurrentShiftHandler(shift.NewRunner(db, q)).Handle(ctx, barista.actor())
	require.Error(t, err)
	assert.ErrorIs(t, err, shift.ErrForbidden)

	// The read's own transaction is read-only and cannot itself audit the
	// denial, so ExecuteRead must record it in a separate transaction.
	assert.Equal(t, 1, countDenialAuditEvents(t, db, barista.StaffID))
}

func TestCurrentShiftDeniesRevokedSession(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()

	_, err := f.DB.Exec(`UPDATE staff_access_sessions SET revoked_at = now() WHERE id = $1`, f.Cashier.SessionID)
	require.NoError(t, err)

	_, err = shift.NewCurrentShiftHandler(f.Runner).Handle(ctx, f.Cashier.actor())
	require.Error(t, err)
	assert.ErrorIs(t, err, shift.ErrUnauthorized)
	assert.Equal(t, 1, countDenialAuditEvents(t, f.DB, f.Cashier.StaffID))
}

// TestMovementRecordsAgainstItsOwnShift guards the Shift-scoped insert: a
// movement recorded against the open Shift carries exactly that Shift's id.
func TestMovementRecordsAgainstItsOwnShift(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()

	_, res, err := f.Movement.Handle(ctx, f.Cashier.actor(),
		f.command(shift.MethodPayIn, shift.ReasonAddChangeFund, 100000, nil))
	require.NoError(t, err)
	assert.Equal(t, f.Shift.ID, res.Movement.SalesShiftID)
	assert.NotEqual(t, uuid.Nil, res.Movement.SalesShiftID)
}
