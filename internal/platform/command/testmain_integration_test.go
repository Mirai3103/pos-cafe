//go:build integration

package command_test

import (
	"database/sql"
	"os"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/testdb"
)

// commandTestDB is the single pool every command integration test shares,
// bound by TestMain before any test runs.
var commandTestDB *sql.DB

// TestMain provisions this package's isolated clone of the migrated test
// template and binds one shared pool for the whole package.
func TestMain(m *testing.M) {
	os.Exit(testdb.Run(m, "platform_command", func(db *sql.DB) {
		commandTestDB = db
	}))
}
