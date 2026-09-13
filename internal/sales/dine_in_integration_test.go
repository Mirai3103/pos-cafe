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

func startDineIn(t *testing.T, runner *sales.Runner, actor sales.Actor,
	requestID uuid.UUID, tableIDs ...uuid.UUID,
) (int, sales.ServiceSessionResponse, error) {
	t.Helper()
	return sales.NewStartDineInSessionHandler(runner).Handle(
		context.Background(), actor,
		sales.StartDineInSessionCommand{RequestID: requestID, TableIDs: tableIDs})
}

func TestStartDineInAssignsTablesInSelectionOrder(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	t1 := seedTable(t, db, "Bàn 1")
	t2 := seedTable(t, db, "Bàn 2")

	// Pass the Tables in reverse id order to prove selection order, not id
	// order, drives the assignment sequence.
	status, resp, err := startDineIn(t, runner, actor, uuid.New(), t2, t1)
	require.NoError(t, err)
	assert.Equal(t, 201, status)
	assert.Equal(t, sales.ModeDineIn, resp.ServiceMode)
	require.Len(t, resp.Tables, 2)
	assert.Equal(t, t2, resp.Tables[0].ID, "tables are projected in assignment sequence")
	assert.Equal(t, t1, resp.Tables[1].ID)

	assertAuditEvent(t, db, sales.EventDineInServiceSessionStarted, 1)
	assertAuditEvent(t, db, sales.EventTableAssignmentCreated, 2)
}

func TestStartDineInRejectsBadSelections(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	t1 := seedTable(t, db, "Bàn 1")

	t.Run("empty selection", func(t *testing.T) {
		_, _, err := startDineIn(t, runner, actor, uuid.New())
		require.ErrorIs(t, err, sales.ErrTableSelectionRequired)
	})

	t.Run("duplicate id", func(t *testing.T) {
		_, _, err := startDineIn(t, runner, actor, uuid.New(), t1, t1)
		require.ErrorIs(t, err, sales.ErrTableSelectionDuplicate)
	})

	t.Run("unknown table", func(t *testing.T) {
		_, _, err := startDineIn(t, runner, actor, uuid.New(), uuid.New())
		require.ErrorIs(t, err, sales.ErrTableNotFound)
	})

	t.Run("unavailable table", func(t *testing.T) {
		unavailable := seedTable(t, db, "Bàn hỏng")
		_, err := db.Exec(`UPDATE tables SET available = false WHERE id = $1`, unavailable)
		require.NoError(t, err)

		_, _, err = startDineIn(t, runner, actor, uuid.New(), unavailable)
		require.ErrorIs(t, err, sales.ErrTableUnavailable)
	})

	var sessions int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM service_sessions`).Scan(&sessions))
	assert.Equal(t, 0, sessions, "every rejected open must create nothing")
}

// CONTEXT.md: a Table "may be associated with one or more active Service
// Sessions". Exclusive occupancy is not a rule of this system.
func TestTwoActiveSessionsMayShareATable(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	t1 := seedTable(t, db, "Bàn 1")

	_, first, err := startDineIn(t, runner, actor, uuid.New(), t1)
	require.NoError(t, err)

	_, second, err := startDineIn(t, runner, actor, uuid.New(), t1)
	require.NoError(t, err, "a second Session may occupy the same Table")
	assert.NotEqual(t, first.ID, second.ID)
}

// Table id order must not change the idempotency fingerprint: selection order
// is presentation, not a different request.
func TestDineInFingerprintIgnoresTableOrder(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	t1 := seedTable(t, db, "Bàn 1")
	t2 := seedTable(t, db, "Bàn 2")

	requestID := uuid.New()
	_, first, err := startDineIn(t, runner, actor, requestID, t1, t2)
	require.NoError(t, err)

	_, replay, err := startDineIn(t, runner, actor, requestID, t2, t1)
	require.NoError(t, err, "the same Table set in a different order is the same request")
	assert.Equal(t, first.ID, replay.ID)
}
