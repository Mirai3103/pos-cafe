//go:build integration

package shift_test

import (
	"database/sql"
	"os"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/testdb"
	"github.com/stretchr/testify/require"
)

// shiftTestDB is the single pool every Shift integration test shares, bound by
// TestMain before any test runs.
var shiftTestDB *sql.DB

// TestMain provisions this package's isolated clone of the migrated test
// template and binds one shared pool for the whole package. The clone is
// already migrated, so tests never run migrations themselves.
func TestMain(m *testing.M) {
	os.Exit(testdb.Run(m, "shift", func(db *sql.DB) {
		shiftTestDB = db
	}))
}

// TestShiftPackageUsesSharedDatabase pins the harness contract: every test in
// the package reaches the same pool, so the package opens one database view
// rather than one per test.
func TestShiftPackageUsesSharedDatabase(t *testing.T) {
	first, _ := openShiftTestDB(t)
	second, _ := openShiftTestDB(t)
	require.Same(t, first, second)
}
