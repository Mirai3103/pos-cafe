//go:build integration

package auth_test

import (
	"database/sql"
	"os"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/testdb"
	"github.com/stretchr/testify/require"
)

// authTestDB is the single pool every auth integration test shares, bound by
// TestMain before any test runs.
var authTestDB *sql.DB

// TestMain provisions this package's isolated clone of the migrated test
// template and binds one shared pool for the whole package. The clone is
// already migrated, so tests never run migrations themselves.
func TestMain(m *testing.M) {
	os.Exit(testdb.Run(m, "auth", func(db *sql.DB) {
		authTestDB = db
	}))
}

// TestAuthPackageUsesSharedDatabase pins the harness contract: every test in
// the package reaches the same pool, so the package opens one database view
// rather than one per test.
func TestAuthPackageUsesSharedDatabase(t *testing.T) {
	first, _ := openApprovalTestDB(t)
	second, _ := openApprovalTestDB(t)
	require.Same(t, first, second)
}
