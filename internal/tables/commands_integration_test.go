//go:build integration

package tables_test

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/tables"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func uniqueTableName(prefix string) string {
	return prefix + " " + uuid.NewString()[:8]
}

func countAuditEvents(t *testing.T, db *sql.DB, eventType string, tableID uuid.UUID) int {
	t.Helper()
	var n int
	err := db.QueryRow(
		`SELECT count(*) FROM audit_events
		 WHERE event_type = $1 AND details->>'table_id' = $2`,
		eventType, tableID.String()).Scan(&n)
	require.NoError(t, err)
	return n
}

func TestCreateTable(t *testing.T) {
	db, q := openTablesTestDB(t)
	runner := tables.NewRunner(db, q)
	handler := tables.NewCreateTableHandler(runner)
	ctx := context.Background()
	manager := newTestActor(t, q, []string{"MANAGER"}, true)

	t.Run("creates a Table and audits it", func(t *testing.T) {
		name := uniqueTableName("Ban")
		code, res, err := handler.Handle(ctx, manager.actor(), tables.CreateTableCommand{
			RequestID: uuid.New(),
			Name:      "  " + name + "  ",
		})
		require.NoError(t, err)
		assert.Equal(t, 201, code)
		assert.Equal(t, name, res.Name, "name must be trimmed")
		assert.True(t, res.Available, "a new Table is available by default")
		assert.NotEqual(t, uuid.Nil, res.ID)
		assert.Equal(t, 1, countAuditEvents(t, db, tables.EventTableCreated, res.ID))
	})

	t.Run("collapses internal whitespace", func(t *testing.T) {
		suffix := uuid.NewString()[:8]
		_, res, err := handler.Handle(ctx, manager.actor(), tables.CreateTableCommand{
			RequestID: uuid.New(),
			Name:      "Ban    ghep " + suffix,
		})
		require.NoError(t, err)
		assert.Equal(t, "Ban ghep "+suffix, res.Name)
	})

	t.Run("rejects an empty name", func(t *testing.T) {
		_, _, err := handler.Handle(ctx, manager.actor(), tables.CreateTableCommand{
			RequestID: uuid.New(),
			Name:      "   ",
		})
		require.Error(t, err)
	})

	t.Run("rejects a name over 60 runes", func(t *testing.T) {
		_, _, err := handler.Handle(ctx, manager.actor(), tables.CreateTableCommand{
			RequestID: uuid.New(),
			Name:      strings.Repeat("a", 61),
		})
		require.Error(t, err)
	})

	t.Run("rejects a duplicate normalized name", func(t *testing.T) {
		name := uniqueTableName("Ban dup")
		_, _, err := handler.Handle(ctx, manager.actor(), tables.CreateTableCommand{
			RequestID: uuid.New(), Name: name,
		})
		require.NoError(t, err)

		_, _, err = handler.Handle(ctx, manager.actor(), tables.CreateTableCommand{
			RequestID: uuid.New(), Name: strings.ToUpper(name),
		})
		require.ErrorIs(t, err, tables.ErrNameConflict)
	})

	t.Run("a cashier cannot create", func(t *testing.T) {
		cashier := newTestActor(t, q, []string{"CASHIER"}, true)
		_, _, err := handler.Handle(ctx, cashier.actor(), tables.CreateTableCommand{
			RequestID: uuid.New(), Name: uniqueTableName("Ban cashier"),
		})
		require.ErrorIs(t, err, tables.ErrForbidden)
	})
}

func TestCreateTableConcurrentSameName(t *testing.T) {
	db, q := openTablesTestDB(t)
	runner := tables.NewRunner(db, q)
	handler := tables.NewCreateTableHandler(runner)
	ctx := context.Background()
	manager := newTestActor(t, q, []string{"MANAGER"}, true)

	name := uniqueTableName("Ban race")
	const goroutines = 4
	errs := make([]error, goroutines)
	var start sync.WaitGroup
	var done sync.WaitGroup
	start.Add(1)

	for i := range goroutines {
		done.Add(1)
		go func() {
			defer done.Done()
			start.Wait()
			_, _, errs[i] = handler.Handle(ctx, manager.actor(), tables.CreateTableCommand{
				RequestID: uuid.New(), // distinct requests, same name
				Name:      name,
			})
		}()
	}
	start.Done()
	done.Wait()

	successes := 0
	for _, err := range errs {
		if err == nil {
			successes++
			continue
		}
		require.ErrorIs(t, err, tables.ErrNameConflict)
	}
	assert.Equal(t, 1, successes, "exactly one concurrent create may succeed")
}

func TestRenameTable(t *testing.T) {
	db, q := openTablesTestDB(t)
	runner := tables.NewRunner(db, q)
	create := tables.NewCreateTableHandler(runner)
	rename := tables.NewRenameTableHandler(runner)
	ctx := context.Background()
	manager := newTestActor(t, q, []string{"MANAGER"}, true)

	_, original, err := create.Handle(ctx, manager.actor(), tables.CreateTableCommand{
		RequestID: uuid.New(), Name: uniqueTableName("Ban old"),
	})
	require.NoError(t, err)

	t.Run("preserves identity and availability", func(t *testing.T) {
		newName := uniqueTableName("Ban new")
		code, res, err := rename.Handle(ctx, manager.actor(), tables.RenameTableCommand{
			RequestID: uuid.New(), TableID: original.ID, Name: newName,
		})
		require.NoError(t, err)
		assert.Equal(t, 200, code)
		assert.Equal(t, original.ID, res.ID, "rename must preserve identity")
		assert.Equal(t, newName, res.Name)
		assert.Equal(t, original.Available, res.Available)
		assert.Equal(t, 1, countAuditEvents(t, db, tables.EventTableRenamed, res.ID))
	})

	t.Run("rejects a missing Table", func(t *testing.T) {
		_, _, err := rename.Handle(ctx, manager.actor(), tables.RenameTableCommand{
			RequestID: uuid.New(), TableID: uuid.New(), Name: uniqueTableName("Ban ghost"),
		})
		require.ErrorIs(t, err, tables.ErrTableNotFound)
	})

	t.Run("rejects a conflicting name", func(t *testing.T) {
		occupied := uniqueTableName("Ban taken")
		_, _, err := create.Handle(ctx, manager.actor(), tables.CreateTableCommand{
			RequestID: uuid.New(), Name: occupied,
		})
		require.NoError(t, err)

		_, _, err = rename.Handle(ctx, manager.actor(), tables.RenameTableCommand{
			RequestID: uuid.New(), TableID: original.ID, Name: occupied,
		})
		require.ErrorIs(t, err, tables.ErrNameConflict)
	})
}

func TestSetTableAvailability(t *testing.T) {
	db, q := openTablesTestDB(t)
	runner := tables.NewRunner(db, q)
	create := tables.NewCreateTableHandler(runner)
	setAvail := tables.NewSetTableAvailabilityHandler(runner)
	ctx := context.Background()
	manager := newTestActor(t, q, []string{"MANAGER"}, true)

	_, created, err := create.Handle(ctx, manager.actor(), tables.CreateTableCommand{
		RequestID: uuid.New(), Name: uniqueTableName("Ban avail"),
	})
	require.NoError(t, err)

	readUpdatedAt := func() string {
		var ts string
		require.NoError(t, db.QueryRow(
			`SELECT updated_at::text FROM tables WHERE id = $1`, created.ID).Scan(&ts))
		return ts
	}

	t.Run("turns availability off and audits it", func(t *testing.T) {
		code, res, err := setAvail.Handle(ctx, manager.actor(), tables.SetTableAvailabilityCommand{
			RequestID: uuid.New(), TableID: created.ID, Available: false,
		})
		require.NoError(t, err)
		assert.Equal(t, 200, code)
		assert.False(t, res.Available)
		assert.Equal(t, 1, countAuditEvents(t, db, tables.EventTableAvailabilityChanged, created.ID))
	})

	t.Run("a same-state request is a no-op", func(t *testing.T) {
		before := readUpdatedAt()

		code, res, err := setAvail.Handle(ctx, manager.actor(), tables.SetTableAvailabilityCommand{
			RequestID: uuid.New(), TableID: created.ID, Available: false,
		})
		require.NoError(t, err)
		assert.Equal(t, 200, code)
		assert.False(t, res.Available)
		assert.Equal(t, before, readUpdatedAt(), "a no-op must not touch updated_at")
		assert.Equal(t, 1, countAuditEvents(t, db, tables.EventTableAvailabilityChanged, created.ID),
			"a no-op must not write a second audit event")
	})

	t.Run("restores availability without deleting the record", func(t *testing.T) {
		_, res, err := setAvail.Handle(ctx, manager.actor(), tables.SetTableAvailabilityCommand{
			RequestID: uuid.New(), TableID: created.ID, Available: true,
		})
		require.NoError(t, err)
		assert.True(t, res.Available)
		assert.Equal(t, created.ID, res.ID)
		assert.Equal(t, 2, countAuditEvents(t, db, tables.EventTableAvailabilityChanged, created.ID))
	})

	t.Run("rejects a missing Table", func(t *testing.T) {
		_, _, err := setAvail.Handle(ctx, manager.actor(), tables.SetTableAvailabilityCommand{
			RequestID: uuid.New(), TableID: uuid.New(), Available: false,
		})
		require.ErrorIs(t, err, tables.ErrTableNotFound)
	})
}

func TestRenameDeniedAfterRoleRemoval(t *testing.T) {
	db, q := openTablesTestDB(t)
	runner := tables.NewRunner(db, q)
	create := tables.NewCreateTableHandler(runner)
	rename := tables.NewRenameTableHandler(runner)
	ctx := context.Background()
	manager := newTestActor(t, q, []string{"MANAGER"}, true)

	originalName := uniqueTableName("Ban authority")
	_, created, err := create.Handle(ctx, manager.actor(), tables.CreateTableCommand{
		RequestID: uuid.New(), Name: originalName,
	})
	require.NoError(t, err)

	// Strip the role mid-session.
	require.NoError(t, q.ClearStaffRoles(ctx, manager.StaffID))

	_, _, err = rename.Handle(ctx, manager.actor(), tables.RenameTableCommand{
		RequestID: uuid.New(), TableID: created.ID, Name: uniqueTableName("Ban renamed"),
	})
	require.ErrorIs(t, err, tables.ErrForbidden)

	var currentName string
	require.NoError(t, db.QueryRow(`SELECT name FROM tables WHERE id = $1`, created.ID).Scan(&currentName))
	assert.Equal(t, originalName, currentName, "a denied rename must change nothing")
}

func TestReplayDeniedAfterRoleRemoval(t *testing.T) {
	db, q := openTablesTestDB(t)
	runner := tables.NewRunner(db, q)
	create := tables.NewCreateTableHandler(runner)
	ctx := context.Background()
	manager := newTestActor(t, q, []string{"MANAGER"}, true)

	requestID := uuid.New()
	cmd := tables.CreateTableCommand{RequestID: requestID, Name: uniqueTableName("Ban replay")}
	_, _, err := create.Handle(ctx, manager.actor(), cmd)
	require.NoError(t, err)

	require.NoError(t, q.ClearStaffRoles(ctx, manager.StaffID))

	// Authority is checked before replay, so the same request is now refused.
	_, _, err = create.Handle(ctx, manager.actor(), cmd)
	require.ErrorIs(t, err, tables.ErrForbidden)
}
