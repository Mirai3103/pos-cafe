//go:build integration

package database_test

import (
	"database/sql"
	"os"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/testdb"
	"github.com/stretchr/testify/require"
)

// databaseTestDB is the single pool every database integration test shares,
// bound by TestMain before any test runs.
var databaseTestDB *sql.DB

// TestMain provisions this package's isolated clone of the migrated test
// template and binds one shared pool for the whole package. The clone is
// already migrated, so tests never run migrations themselves.
func TestMain(m *testing.M) {
	os.Exit(testdb.Run(m, "database", func(db *sql.DB) {
		databaseTestDB = db
	}))
}

// TestDatabasePackageUsesSharedDatabase pins the harness contract: every test in
// the package reaches the same pool, so the package opens one database view
// rather than one per test.
func TestDatabasePackageUsesSharedDatabase(t *testing.T) {
	first := setupTestDB(t)
	second := setupTestDB(t)
	require.Same(t, first, second)
}
