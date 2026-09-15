//go:build integration

package testdb

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// provisionTimeout bounds the whole test body, mirroring the budget Run
	// grants its own provisioning step.
	provisionTimeout = 2 * time.Minute
	// teardownTimeout bounds the cleanup safety net, mirroring the budget Run
	// grants its own teardown. A DROP DATABASE blocked behind an open session
	// must fail the test instead of hanging until the binary timeout.
	teardownTimeout = 30 * time.Second
)

func TestProvisionCreatesMigratedIsolatedClones(t *testing.T) {
	rawURL := os.Getenv("TEST_DATABASE_URL")
	require.NotEmpty(t, rawURL)

	ctx, cancel := context.WithTimeout(context.Background(), provisionTimeout)
	defer cancel()

	base, err := parseBaseConfig(rawURL)
	require.NoError(t, err)

	// The test's own window onto the live catalog. It has to outlive
	// instance.Close, which shuts down the instance's maintenance pool, so it is
	// opened here and registered first in order to be closed last.
	maintenance, err := sql.Open("pgx", base.maintenanceDSN)
	require.NoError(t, err)
	require.NoError(t, maintenance.PingContext(ctx))
	t.Cleanup(func() {
		require.NoError(t, maintenance.Close())
	})

	// The harness never connects to the base test database and this test does not
	// create it -- the cluster's init script does. Require it up front so a
	// missing base fails here as an honest setup error rather than after Close,
	// under a message that blames Close for dropping something it never touched.
	baseCountBefore, err := countDatabase(ctx, maintenance, base.baseName)
	require.NoError(t, err)
	require.Equal(t, 1, baseCountBefore, "the base test database %s must exist before the harness runs", base.baseName)

	firstSuffix, err := newSuffix()
	require.NoError(t, err)
	first, err := provision(ctx, rawURL, "sales", firstSuffix)
	require.NoError(t, err)
	firstClosed := false
	closeOnCleanup(t, first, &firstClosed)

	secondSuffix, err := newSuffix()
	require.NoError(t, err)
	second, err := provision(ctx, rawURL, "sales", secondSuffix)
	require.NoError(t, err)
	secondClosed := false
	closeOnCleanup(t, second, &secondClosed)

	// Each pool must be wired to its own clone. Were they ever cross-wired, every
	// isolation assertion below would still pass while proving nothing: the probe
	// would land in the other clone, so second.db would still report no table and
	// zero rows, and each Close would still drop its own cfg.cloneName. Asserting
	// the linkage directly is what stops this test from certifying a wiring bug.
	// require, not assert: if the wiring is wrong, nothing after it is meaningful.
	var firstDBName string
	require.NoError(t, first.db.QueryRowContext(ctx, `SELECT current_database()`).Scan(&firstDBName))
	require.Equal(t, first.cfg.cloneName, firstDBName, "the package pool is not connected to its own clone")

	var secondDBName string
	require.NoError(t, second.db.QueryRowContext(ctx, `SELECT current_database()`).Scan(&secondDBName))
	require.Equal(t, second.cfg.cloneName, secondDBName, "the package pool is not connected to its own clone")

	// Both clones must carry the same, non-empty migration history. A clone
	// built from template0 rather than the migrated template would be empty, so
	// reading only one side would leave that regression undetectable.
	var firstMigrations, secondMigrations int
	require.NoError(t, first.db.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&firstMigrations))
	require.NoError(t, second.db.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&secondMigrations))
	assert.Positive(t, firstMigrations)
	assert.Positive(t, secondMigrations)
	assert.Equal(t, firstMigrations, secondMigrations, "clones were not migrated to the same version")

	// Schema isolation: an object created in the first clone must be invisible
	// in the second.
	_, err = first.db.ExecContext(ctx, `CREATE TABLE clone_isolation_probe (id integer PRIMARY KEY)`)
	require.NoError(t, err)
	_, err = first.db.ExecContext(ctx, `INSERT INTO clone_isolation_probe (id) VALUES (1)`)
	require.NoError(t, err)

	var probeInSecond bool
	require.NoError(t, second.db.QueryRowContext(ctx,
		`SELECT to_regclass('public.clone_isolation_probe') IS NOT NULL`).Scan(&probeInSecond))
	assert.False(t, probeInSecond, "probe table created in the first clone is visible in the second clone")

	// Data isolation: give the second clone its own copy of the probe and prove
	// the row written to the first clone did not travel with it. IF NOT EXISTS
	// keeps this assertion reachable even when the isolation check above failed.
	_, err = second.db.ExecContext(ctx,
		`CREATE TABLE IF NOT EXISTS clone_isolation_probe (id integer PRIMARY KEY)`)
	require.NoError(t, err)

	var firstRows, secondRows int
	require.NoError(t, first.db.QueryRowContext(ctx, `SELECT count(*) FROM clone_isolation_probe`).Scan(&firstRows))
	require.NoError(t, second.db.QueryRowContext(ctx, `SELECT count(*) FROM clone_isolation_probe`).Scan(&secondRows))
	assert.Equal(t, 1, firstRows, "the row inserted into the first clone did not persist there")
	assert.Zero(t, secondRows, "row inserted into the first clone is visible in the second clone")

	// Teardown is asserted, not assumed: Close must report success, remove
	// exactly its own clone, and leave the template and base databases intact.
	require.NoError(t, first.Close(ctx))
	firstClosed = true
	assertCloneDropped(ctx, t, maintenance, first.cfg)

	require.NoError(t, second.Close(ctx))
	secondClosed = true
	assertCloneDropped(ctx, t, maintenance, second.cfg)
}

// closeOnCleanup registers a safety net that tears an instance down when the
// test body aborts before reaching its explicit close, so an early require
// failure cannot leak a clone. Close is not idempotent: called twice it fails on
// the clone it already dropped, so the caller's flag gates the second attempt.
// The net deliberately does not cover a Close that returns nil without dropping
// anything -- the flag is set the moment Close succeeds, so the net is skipped --
// but that case is not silent: the catalog assertions after each close are what
// catch it.
func closeOnCleanup(t *testing.T, inst *instance, closed *bool) {
	t.Helper()
	t.Cleanup(func() {
		if *closed {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), teardownTimeout)
		defer cancel()
		require.NoError(t, inst.Close(ctx))
	})
}

// assertCloneDropped proves from the live catalog that cfg's clone is gone and
// that neither the shared template nor the base test database was collateral
// damage. The survivor checks are what make a Close wired to the wrong database
// name fail loudly instead of self-healing on the next provision.
func assertCloneDropped(ctx context.Context, t *testing.T, maintenance *sql.DB, cfg config) {
	t.Helper()

	cloneCount, err := countDatabase(ctx, maintenance, cfg.cloneName)
	require.NoError(t, err)
	assert.Zero(t, cloneCount, "clone database %s survived Close", cfg.cloneName)

	templateCount, err := countDatabase(ctx, maintenance, cfg.templateName)
	require.NoError(t, err)
	assert.Equal(t, 1, templateCount, "Close dropped the persistent template database %s", cfg.templateName)

	baseCount, err := countDatabase(ctx, maintenance, cfg.baseName)
	require.NoError(t, err)
	assert.Equal(t, 1, baseCount, "Close dropped the base test database %s", cfg.baseName)
}

// countDatabase counts catalog entries carrying exactly this name.
func countDatabase(ctx context.Context, maintenance *sql.DB, name string) (int, error) {
	var count int
	if err := maintenance.QueryRowContext(ctx,
		`SELECT count(*) FROM pg_database WHERE datname = $1`, name,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count database %s: %w", name, err)
	}
	return count, nil
}

// TestCleanupSkipsActiveClone proves the coordination between Cleanup and a
// running harness: while a clone's package pool is open, the clone has live
// sessions and Cleanup must leave it — and the template and base databases —
// untouched without reporting an error. Dropping an in-use clone would erase a
// running package's database mid-run.
func TestCleanupSkipsActiveClone(t *testing.T) {
	rawURL := os.Getenv("TEST_DATABASE_URL")
	require.NotEmpty(t, rawURL)

	ctx, cancel := context.WithTimeout(context.Background(), provisionTimeout)
	defer cancel()

	suffix, err := newSuffix()
	require.NoError(t, err)
	inst, err := provision(ctx, rawURL, "sales", suffix)
	require.NoError(t, err)
	closed := false
	closeOnCleanup(t, inst, &closed)

	require.NoError(t, Cleanup(ctx, rawURL))

	// The instance's maintenance pool is closed by Close, so the catalog is
	// observed through the test's own window.
	maintenance, err := sql.Open("pgx", inst.cfg.maintenanceDSN)
	require.NoError(t, err)
	require.NoError(t, maintenance.PingContext(ctx))
	t.Cleanup(func() { require.NoError(t, maintenance.Close()) })

	cloneCount, err := countDatabase(ctx, maintenance, inst.cfg.cloneName)
	require.NoError(t, err)
	assert.Equal(t, 1, cloneCount, "Cleanup dropped the active clone %s", inst.cfg.cloneName)

	templateCount, err := countDatabase(ctx, maintenance, inst.cfg.templateName)
	require.NoError(t, err)
	assert.Equal(t, 1, templateCount, "Cleanup dropped the persistent template database")

	baseCount, err := countDatabase(ctx, maintenance, inst.cfg.baseName)
	require.NoError(t, err)
	assert.Equal(t, 1, baseCount, "Cleanup dropped the base test database")

	require.NoError(t, inst.Close(ctx))
	closed = true
	assertCloneDropped(ctx, t, maintenance, inst.cfg)
}
