package testdb

import (
	"context"
	"database/sql"
	"errors"
	"testing"

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
