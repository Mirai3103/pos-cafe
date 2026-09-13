//go:build integration

package sales_test

import (
	"context"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setTables(t *testing.T, runner *sales.Runner, actor sales.Actor,
	sessionID uuid.UUID, requestID uuid.UUID, tableIDs ...uuid.UUID,
) (sales.ServiceSessionResponse, error) {
	t.Helper()
	if tableIDs == nil {
		tableIDs = []uuid.UUID{}
	}
	_, resp, err := sales.NewSetSessionTablesHandler(runner).Handle(
		context.Background(), actor, sales.SetSessionTablesCommand{
			RequestID:        requestID,
			ServiceSessionID: sessionID,
			TableIDs:         tableIDs,
		})
	return resp, err
}

func TestSetTablesAddsReleasesAndKeeps(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	t1 := seedTable(t, db, "Bàn 1")
	t2 := seedTable(t, db, "Bàn 2")
	t3 := seedTable(t, db, "Bàn 3")

	_, session, err := startDineIn(t, runner, actor, uuid.New(), t1, t2)
	require.NoError(t, err)

	// Keep t2, release t1, add t3.
	resp, err := setTables(t, runner, actor, session.ID, uuid.New(), t2, t3)
	require.NoError(t, err)

	ids := []uuid.UUID{}
	for _, tbl := range resp.Tables {
		ids = append(ids, tbl.ID)
	}
	assert.ElementsMatch(t, []uuid.UUID{t2, t3}, ids)

	// The released row keeps both pieces of release evidence.
	var releasedAt, releasedBy any
	require.NoError(t, db.QueryRow(`
		SELECT released_at, released_by_staff_identity_id
		FROM table_assignments
		WHERE service_session_id = $1 AND table_id = $2`,
		session.ID, t1).Scan(&releasedAt, &releasedBy))
	assert.NotNil(t, releasedAt)
	assert.NotNil(t, releasedBy)

	assertAuditEvent(t, db, sales.EventTableAssignmentReleased, 1)
	// Two from the dine-in open, one from the add.
	assertAuditEvent(t, db, sales.EventTableAssignmentCreated, 3)
}

// Sequence continues past released assignments, so a number is never reused.
func TestSetTablesNeverReusesASequence(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	t1 := seedTable(t, db, "Bàn 1")
	t2 := seedTable(t, db, "Bàn 2")

	_, session, err := startDineIn(t, runner, actor, uuid.New(), t1)
	require.NoError(t, err)

	_, err = setTables(t, runner, actor, session.ID, uuid.New(), t2)
	require.NoError(t, err)

	var sequences []int32
	rows, err := db.Query(`
		SELECT sequence FROM table_assignments
		WHERE service_session_id = $1 ORDER BY sequence ASC`, session.ID)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var seq int32
		require.NoError(t, rows.Scan(&seq))
		sequences = append(sequences, seq)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []int32{0, 1}, sequences)
}

// Releasing every Table is permitted. A Dine-in party that has left its table
// but not yet paid is a real situation; the canonical source rejects an empty
// selection only when opening a Session.
func TestSetTablesAcceptsAnEmptySelection(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	t1 := seedTable(t, db, "Bàn 1")

	_, session, err := startDineIn(t, runner, actor, uuid.New(), t1)
	require.NoError(t, err)

	resp, err := setTables(t, runner, actor, session.ID, uuid.New())
	require.NoError(t, err)
	assert.Empty(t, resp.Tables)
}

func TestSetTablesRejectsTakeawaySession(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	t1 := seedTable(t, db, "Bàn 1")

	_, session, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)

	_, err = setTables(t, runner, actor, session.ID, uuid.New(), t1)
	require.ErrorIs(t, err, sales.ErrTakeawayTablesNotAvailable)
}

func TestSetTablesRejectsClosedSession(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	t1 := seedTable(t, db, "Bàn 1")
	t2 := seedTable(t, db, "Bàn 2")

	_, session, err := startDineIn(t, runner, actor, uuid.New(), t1)
	require.NoError(t, err)

	// 5D owns closure; the test drives the state directly.
	_, err = db.Exec(`UPDATE service_sessions SET state = 'CLOSED' WHERE id = $1`, session.ID)
	require.NoError(t, err)

	_, err = setTables(t, runner, actor, session.ID, uuid.New(), t2)
	require.ErrorIs(t, err, sales.ErrServiceSessionClosed)
}

// An already-assigned Table that has since been marked unavailable must not
// block an unrelated change to the same Session: only newly added Tables are
// validated.
func TestSetTablesValidatesOnlyAdditions(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	t1 := seedTable(t, db, "Bàn 1")
	t2 := seedTable(t, db, "Bàn 2")

	_, session, err := startDineIn(t, runner, actor, uuid.New(), t1)
	require.NoError(t, err)

	_, err = db.Exec(`UPDATE tables SET available = false WHERE id = $1`, t1)
	require.NoError(t, err)

	resp, err := setTables(t, runner, actor, session.ID, uuid.New(), t1, t2)
	require.NoError(t, err, "keeping an already-assigned Table must not revalidate it")
	assert.Len(t, resp.Tables, 2)
}

// Every 5A mutation requires the Session's OWN Sales Shift to be OPEN. The
// check reads the state through the Session's sales_shift_id, matching the
// draft-lock query's sh.id = s.sales_shift_id AND sh.state = 'OPEN' semantics,
// rather than asking whether some open Shift exists.
func TestSetTablesRequiresTheSessionOwnShiftOpen(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	shiftID := seedOpenShift(t, q, actor.StaffID)
	t1 := seedTable(t, db, "Bàn 1")
	t2 := seedTable(t, db, "Bàn 2")

	// Seed the Dine-in Session and its assignment with raw SQL so the world
	// exists before the command under test runs.
	var sessionID uuid.UUID
	require.NoError(t, db.QueryRow(`
		INSERT INTO service_sessions
		    (service_number, sequence, service_mode, state, created_by_staff_identity_id, sales_shift_id)
		VALUES ('S00001', 1, 'DINE_IN', 'ACTIVE', $1, $2)
		RETURNING id`, actor.StaffID, shiftID).Scan(&sessionID))
	var assignmentID uuid.UUID
	require.NoError(t, db.QueryRow(`
		INSERT INTO table_assignments
		    (table_id, service_session_id, assigned_by_staff_identity_id, sequence)
		VALUES ($1, $2, $3, 0) RETURNING id`,
		t1, sessionID, actor.StaffID).Scan(&assignmentID))

	// 5A ships no Shift close command, so the test drives the state directly.
	_, err := db.Exec(`UPDATE sales_shifts SET state = 'CLOSED' WHERE id = $1`, shiftID)
	require.NoError(t, err)

	// Requesting t2 alone would release t1 and add t2 if the Shift check were
	// missing, so a silent no-op rejection cannot pass this test.
	_, err = setTables(t, runner, actor, sessionID, uuid.New(), t2)
	require.ErrorIs(t, err, sales.ErrOpenShiftRequired)

	var assignments, released int
	require.NoError(t, db.QueryRow(`
		SELECT count(*) FROM table_assignments
		WHERE service_session_id = $1 AND released_at IS NULL`, sessionID).Scan(&assignments))
	require.NoError(t, db.QueryRow(`
		SELECT count(*) FROM table_assignments
		WHERE service_session_id = $1 AND released_at IS NOT NULL`, sessionID).Scan(&released))
	assert.Equal(t, 1, assignments, "a rejected set must leave the current assignment in place")
	assert.Equal(t, 0, released, "a rejected set must not release anything")

	assertAuditEvent(t, db, sales.EventTableAssignmentReleased, 0)
	assertAuditEvent(t, db, sales.EventTableAssignmentCreated, 0)
}
