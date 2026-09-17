package testdb

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFinalExitCode(t *testing.T) {
	tests := []struct {
		name       string
		testCode   int
		cleanupErr error
		want       int
	}{
		{"success", 0, nil, 0},
		{"cleanup failure fails successful run", 0, errors.New("drop failed"), 1},
		{"test failure remains authoritative", 2, errors.New("drop failed"), 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, finalExitCode(tt.testCode, tt.cleanupErr))
		})
	}
}

// TestInstanceCloseOnlyDropsCloneItCreated pins the teardown gate: Close may
// drop exactly the clone this invocation created, and nothing else. A closed
// pool fails every statement, so a nil error proves no DROP was attempted.
func TestInstanceCloseOnlyDropsCloneItCreated(t *testing.T) {
	closedPool := func() *sql.DB {
		t.Helper()
		pool, err := sql.Open("pgx", "postgres://user:pass@127.0.0.1:1/cafe_pos_test_sales_0123456789ab?sslmode=disable")
		require.NoError(t, err)
		require.NoError(t, pool.Close())
		return pool
	}

	notCreated := &instance{
		maintenance:  closedPool(),
		createdClone: false,
		cfg:          config{cloneName: "cafe_pos_test_sales_0123456789ab"},
	}
	require.NoError(t, notCreated.Close(context.Background()),
		"teardown must not drop a clone this invocation did not create")

	// Positive control: with the gate open the same closed pool is reached and
	// the attempted DROP fails, which proves the assertion above can detect a drop.
	created := &instance{
		maintenance:  closedPool(),
		createdClone: true,
		cfg:          config{cloneName: "cafe_pos_test_sales_0123456789ab"},
	}
	require.Error(t, created.Close(context.Background()),
		"a clone this invocation created must be dropped on teardown")
}

// stubExecDB is a dbtx whose ExecContext answers with a canned error and whose
// read methods are never reached by dropDatabase.
type stubExecDB struct{ err error }

func (s stubExecDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, s.err
}

func (s stubExecDB) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, errors.New("stubExecDB: QueryContext is not used by dropDatabase")
}

func (s stubExecDB) QueryRowContext(context.Context, string, ...any) *sql.Row {
	return nil
}

// TestDropDatabaseTreatsAlreadyDroppedAsSuccess pins the stale-clone drop
// tolerance: the Cleanup scanner and a finishing package's own Close can race
// to drop the same clone — the scanner observes zero sessions while the owner
// commits its DROP — and whoever drops second receives SQLSTATE 3D000. The
// database no longer existing is exactly the end state both sides want, so
// dropDatabase must answer nil for it while every other failure still fails.
func TestDropDatabaseTreatsAlreadyDroppedAsSuccess(t *testing.T) {
	name := "cafe_pos_test_sales_0123456789ab"

	alreadyDropped := &pgconn.PgError{
		Code:    "3D000",
		Message: `database "cafe_pos_test_sales_0123456789ab" does not exist`,
	}
	require.NoError(t, dropDatabase(context.Background(), stubExecDB{err: alreadyDropped}, name),
		"an already-dropped clone is the desired end state")

	permissionDenied := &pgconn.PgError{Code: "42501", Message: "permission denied"}
	require.ErrorIs(t, dropDatabase(context.Background(), stubExecDB{err: permissionDenied}, name),
		permissionDenied, "every other PostgreSQL failure still fails")

	require.Error(t, dropDatabase(context.Background(), stubExecDB{err: errors.New("connection refused")}, name),
		"non-PostgreSQL failures still fail")
}
