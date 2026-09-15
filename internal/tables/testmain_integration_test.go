//go:build integration

package tables_test

import (
	"database/sql"
	"os"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/testdb"
	"github.com/stretchr/testify/require"
)

// tablesTestDB is the single pool every Tables integration test shares, bound
// by TestMain before any test runs.
var tablesTestDB *sql.DB

// TestMain provisions this package's isolated clone of the migrated test
// template and binds one shared pool for the whole package. The clone is
// already migrated, so tests never run migrations themselves.
func TestMain(m *testing.M) {
	os.Exit(testdb.Run(m, "tables", func(db *sql.DB) {
		tablesTestDB = db
	}))
}

// TestTablesPackageUsesSharedDatabase pins the harness contract: every test in
// the package reaches the same pool, so the package opens one database view
// rather than one per test.
func TestTablesPackageUsesSharedDatabase(t *testing.T) {
	first, _ := openTablesTestDB(t)
	second := openSchemaTestDB(t)
	require.Same(t, first, second)
}
