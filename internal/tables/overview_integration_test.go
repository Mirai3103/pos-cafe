//go:build integration

package tables_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/tables"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedServiceSession inserts a Sales-owned service_sessions row directly.
// Phase 3 never writes these through its API; Phase 5 owns that behavior.
func seedServiceSession(t *testing.T, db *sql.DB, staffID uuid.UUID, serviceNumber, state string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	require.NoError(t, db.QueryRow(
		`INSERT INTO service_sessions (service_number, service_mode, state, created_by_staff_identity_id)
		 VALUES ($1, 'DINE_IN', $2, $3) RETURNING id`,
		serviceNumber, state, staffID).Scan(&id))
	return id
}

func seedAssignment(t *testing.T, db *sql.DB, tableID, sessionID, staffID uuid.UUID, sequence int, released bool) {
	t.Helper()
	if released {
		_, err := db.Exec(
			`INSERT INTO table_assignments
			   (table_id, service_session_id, assigned_by_staff_identity_id, sequence,
			    released_at, released_by_staff_identity_id)
			 VALUES ($1, $2, $3, $4, now(), $3)`,
			tableID, sessionID, staffID, sequence)
		require.NoError(t, err)
		return
	}
	_, err := db.Exec(
		`INSERT INTO table_assignments
		   (table_id, service_session_id, assigned_by_staff_identity_id, sequence)
		 VALUES ($1, $2, $3, $4)`,
		tableID, sessionID, staffID, sequence)
	require.NoError(t, err)
}

func findRow(rows []tables.TableOverviewRow, id uuid.UUID) *tables.TableOverviewRow {
	for i := range rows {
		if rows[i].ID == id {
			return &rows[i]
		}
	}
	return nil
}

// randomServiceNumber returns a value matching ^[A-Z0-9]{6}$.
func randomServiceNumber() string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	raw := uuid.New()
	out := make([]byte, 6)
	for i := range out {
		out[i] = alphabet[int(raw[i])%len(alphabet)]
	}
	return string(out)
}

func TestOverviewReportsOccupancy(t *testing.T) {
	db, q := openTablesTestDB(t)
	runner := tables.NewRunner(db, q)
	create := tables.NewCreateTableHandler(runner)
	overview := tables.NewOverviewHandler(runner)
	ctx := context.Background()

	manager := newTestActor(t, q, []string{"MANAGER"}, true)
	cashier := newTestActor(t, q, []string{"CASHIER"}, true)

	_, shared, err := create.Handle(ctx, manager.actor(), tables.CreateTableCommand{
		RequestID: uuid.New(), Name: uniqueTableName("Ban ghep"),
	})
	require.NoError(t, err)
	_, empty, err := create.Handle(ctx, manager.actor(), tables.CreateTableCommand{
		RequestID: uuid.New(), Name: uniqueTableName("Ban trong"),
	})
	require.NoError(t, err)

	t.Run("both Tables start unoccupied", func(t *testing.T) {
		rows, err := overview.Handle(ctx, cashier.actor())
		require.NoError(t, err)
		require.NotNil(t, findRow(rows, shared.ID))
		assert.Empty(t, findRow(rows, shared.ID).CurrentServiceSessions)
		assert.Empty(t, findRow(rows, empty.ID).CurrentServiceSessions)
	})

	numberOne := randomServiceNumber()
	numberTwo := randomServiceNumber()
	require.NotEqual(t, numberOne, numberTwo)
	sessionOne := seedServiceSession(t, db, manager.StaffID, numberOne, "ACTIVE")
	sessionTwo := seedServiceSession(t, db, manager.StaffID, numberTwo, "ACTIVE")
	seedAssignment(t, db, shared.ID, sessionOne, manager.StaffID, 1, false)
	seedAssignment(t, db, shared.ID, sessionTwo, manager.StaffID, 2, false)

	t.Run("reports shared occupancy in assignment order", func(t *testing.T) {
		rows, err := overview.Handle(ctx, cashier.actor())
		require.NoError(t, err)
		row := findRow(rows, shared.ID)
		require.NotNil(t, row)
		require.Len(t, row.CurrentServiceSessions, 2)
		assert.Equal(t, sessionOne, row.CurrentServiceSessions[0].ServiceSessionID)
		assert.Equal(t, numberOne, row.CurrentServiceSessions[0].ServiceNumber)
		assert.Equal(t, sessionTwo, row.CurrentServiceSessions[1].ServiceSessionID)

		assert.Empty(t, findRow(rows, empty.ID).CurrentServiceSessions)
	})

	t.Run("occupants expose exactly two fields", func(t *testing.T) {
		rows, err := overview.Handle(ctx, cashier.actor())
		require.NoError(t, err)
		row := findRow(rows, shared.ID)
		require.NotNil(t, row)

		raw, err := json.Marshal(row.CurrentServiceSessions[0])
		require.NoError(t, err)
		var fields map[string]any
		require.NoError(t, json.Unmarshal(raw, &fields))
		assert.Len(t, fields, 2)
		assert.Contains(t, fields, "service_session_id")
		assert.Contains(t, fields, "service_number")
	})

	t.Run("excludes released assignments", func(t *testing.T) {
		_, released, err := create.Handle(ctx, manager.actor(), tables.CreateTableCommand{
			RequestID: uuid.New(), Name: uniqueTableName("Ban released"),
		})
		require.NoError(t, err)
		session := seedServiceSession(t, db, manager.StaffID, randomServiceNumber(), "ACTIVE")
		seedAssignment(t, db, released.ID, session, manager.StaffID, 1, true)

		rows, err := overview.Handle(ctx, cashier.actor())
		require.NoError(t, err)
		assert.Empty(t, findRow(rows, released.ID).CurrentServiceSessions)
	})

	t.Run("excludes non-active sessions", func(t *testing.T) {
		_, done, err := create.Handle(ctx, manager.actor(), tables.CreateTableCommand{
			RequestID: uuid.New(), Name: uniqueTableName("Ban done"),
		})
		require.NoError(t, err)
		session := seedServiceSession(t, db, manager.StaffID, randomServiceNumber(), "COMPLETED")
		seedAssignment(t, db, done.ID, session, manager.StaffID, 1, false)

		rows, err := overview.Handle(ctx, cashier.actor())
		require.NoError(t, err)
		assert.Empty(t, findRow(rows, done.ID).CurrentServiceSessions)
	})

	t.Run("an unavailable Table keeps its occupants", func(t *testing.T) {
		setAvail := tables.NewSetTableAvailabilityHandler(runner)
		_, _, err := setAvail.Handle(ctx, manager.actor(), tables.SetTableAvailabilityCommand{
			RequestID: uuid.New(), TableID: shared.ID, Available: boolPtr(false),
		})
		require.NoError(t, err)

		rows, err := overview.Handle(ctx, cashier.actor())
		require.NoError(t, err)
		row := findRow(rows, shared.ID)
		require.NotNil(t, row)
		assert.False(t, row.Available)
		assert.Len(t, row.CurrentServiceSessions, 2,
			"Availability and occupancy are independent")
	})
}

func TestOverviewSerializesEmptyOccupancyAsArray(t *testing.T) {
	db, q := openTablesTestDB(t)
	runner := tables.NewRunner(db, q)
	create := tables.NewCreateTableHandler(runner)
	overview := tables.NewOverviewHandler(runner)
	ctx := context.Background()
	manager := newTestActor(t, q, []string{"MANAGER"}, true)

	_, created, err := create.Handle(ctx, manager.actor(), tables.CreateTableCommand{
		RequestID: uuid.New(), Name: uniqueTableName("Ban json"),
	})
	require.NoError(t, err)

	rows, err := overview.Handle(ctx, manager.actor())
	require.NoError(t, err)
	row := findRow(rows, created.ID)
	require.NotNil(t, row)

	raw, err := json.Marshal(row)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"current_service_sessions":[]`,
		"empty occupancy must serialize as [] and never null")
}

func TestOverviewDeniedWithoutSalesOperate(t *testing.T) {
	db, q := openTablesTestDB(t)
	runner := tables.NewRunner(db, q)
	overview := tables.NewOverviewHandler(runner)
	ctx := context.Background()

	barista := newTestActor(t, q, []string{"BARISTA"}, true)
	_, err := overview.Handle(ctx, barista.actor())
	require.ErrorIs(t, err, tables.ErrForbidden)
}

func TestOverviewOrdersTablesByCreation(t *testing.T) {
	db, q := openTablesTestDB(t)
	runner := tables.NewRunner(db, q)
	create := tables.NewCreateTableHandler(runner)
	overview := tables.NewOverviewHandler(runner)
	ctx := context.Background()
	manager := newTestActor(t, q, []string{"MANAGER"}, true)

	_, first, err := create.Handle(ctx, manager.actor(), tables.CreateTableCommand{
		RequestID: uuid.New(), Name: uniqueTableName("Ban order 1"),
	})
	require.NoError(t, err)
	_, second, err := create.Handle(ctx, manager.actor(), tables.CreateTableCommand{
		RequestID: uuid.New(), Name: uniqueTableName("Ban order 2"),
	})
	require.NoError(t, err)

	rows, err := overview.Handle(ctx, manager.actor())
	require.NoError(t, err)

	firstIndex, secondIndex := -1, -1
	for i := range rows {
		switch rows[i].ID {
		case first.ID:
			firstIndex = i
		case second.ID:
			secondIndex = i
		}
	}
	require.NotEqual(t, -1, firstIndex)
	require.NotEqual(t, -1, secondIndex)
	assert.Less(t, firstIndex, secondIndex, "older Tables come first")
}
