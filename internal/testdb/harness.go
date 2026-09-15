package testdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/database"
	"github.com/lib/pq"
)

// templateLockID is the session-level advisory lock key that serializes
// template preparation across concurrently running integration test processes.
const templateLockID int64 = 7142982

// instance holds the resources provisioned for one integration test package.
type instance struct {
	db          *sql.DB
	maintenance *sql.DB
	cfg         config
	// createdClone records that this invocation created cfg.cloneName, so that
	// teardown never drops a database this invocation did not create.
	createdClone bool
}

// Run provisions an isolated clone of the migrated test template database for an
// integration test package, binds the shared package pool, runs the package
// tests, and tears the clone down. It returns the process exit code for the
// caller's TestMain; it never calls os.Exit itself.
func Run(m *testing.M, packageName string, bind func(*sql.DB)) int {
	rawURL := os.Getenv("TEST_DATABASE_URL")
	suffix, err := newSuffix()
	if err != nil {
		fmt.Fprintf(os.Stderr, "integration database setup for %s: %v\n", packageName, sanitizeError(err, rawURL))
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	inst, err := provision(ctx, rawURL, packageName, suffix)
	cancel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "integration database setup for %s: %v\n", packageName, sanitizeError(err, rawURL))
		return 1
	}
	bind(inst.db)
	testCode := m.Run()
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cleanupCancel()
	cleanupErr := inst.Close(cleanupCtx)
	if cleanupErr != nil {
		fmt.Fprintf(os.Stderr, "integration database cleanup for %s: %v\n", packageName, sanitizeError(cleanupErr, rawURL))
	}
	return finalExitCode(testCode, cleanupErr)
}

func finalExitCode(testCode int, cleanupErr error) int {
	if testCode != 0 {
		return testCode
	}
	if cleanupErr != nil {
		return 1
	}
	return 0
}

// provision migrates the shared template database under a connection-bound
// advisory lock and clones it into a uniquely named database for one package.
func provision(ctx context.Context, rawURL, packageName, suffix string) (*instance, error) {
	cfg, err := parseConfig(rawURL, packageName, suffix)
	if err != nil {
		return nil, fmt.Errorf("configure integration database for %s: %w", packageName, err)
	}

	maintenance, err := sql.Open("pgx", cfg.maintenanceDSN)
	if err != nil {
		return nil, fmt.Errorf("open maintenance pool for %s: %w", packageName, err)
	}
	if err := maintenance.PingContext(ctx); err != nil {
		_ = maintenance.Close()
		return nil, fmt.Errorf("connect to maintenance database for %s: %w", packageName, err)
	}

	inst := &instance{maintenance: maintenance, cfg: cfg}

	lockErr := withTemplateLock(ctx, maintenance, func(conn *sql.Conn) error {
		if err := ensureTemplate(ctx, conn, cfg); err != nil {
			return err
		}
		if err := migrateTemplate(ctx, cfg.templateDSN); err != nil {
			return err
		}
		if err := createClone(ctx, conn, cfg); err != nil {
			return err
		}
		inst.createdClone = true
		return nil
	})
	if lockErr != nil {
		lockErr = errors.Join(lockErr, inst.Close(ctx))
		return nil, fmt.Errorf("prepare template database for %s: %w", packageName, lockErr)
	}

	pool, err := openClonePool(ctx, cfg.cloneDSN)
	if err != nil {
		err = errors.Join(err, inst.Close(ctx))
		return nil, fmt.Errorf("open package pool for %s: %w", packageName, err)
	}
	inst.db = pool

	return inst, nil
}

// withTemplateLock reserves exactly one maintenance connection, acquires the
// template advisory lock on it, invokes fn on that same connection, and releases
// the lock on it before closing it, so the session-level lock can never migrate
// between pooled sessions. Callback, unlock, and close errors are joined.
func withTemplateLock(ctx context.Context, maintenance *sql.DB, fn func(*sql.Conn) error) (err error) {
	conn, err := maintenance.Conn(ctx)
	if err != nil {
		return fmt.Errorf("reserve maintenance connection for template lock: %w", err)
	}

	locked := false
	defer func() {
		var unlockErr error
		if locked {
			if _, unlockErr = conn.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", templateLockID); unlockErr != nil {
				unlockErr = fmt.Errorf("release template advisory lock: %w", unlockErr)
			}
		}
		closeErr := conn.Close()
		if closeErr != nil {
			closeErr = fmt.Errorf("close maintenance connection: %w", closeErr)
		}
		err = errors.Join(err, unlockErr, closeErr)
	}()

	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", templateLockID); err != nil {
		return fmt.Errorf("acquire template advisory lock: %w", err)
	}
	locked = true

	return fn(conn)
}

// ensureTemplate creates the template database from template0 when it is absent.
func ensureTemplate(ctx context.Context, conn *sql.Conn, cfg config) error {
	var exists bool
	if err := conn.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`, cfg.templateName,
	).Scan(&exists); err != nil {
		return fmt.Errorf("look up template database %s: %w", cfg.templateName, err)
	}
	if exists {
		return nil
	}
	statement := fmt.Sprintf("CREATE DATABASE %s TEMPLATE template0", pq.QuoteIdentifier(cfg.templateName))
	if _, err := conn.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("create template database %s: %w", cfg.templateName, err)
	}
	return nil
}

// migrateTemplate applies all pending embedded migrations to the template
// database and closes the pool, so no connection remains attached to the
// template when it is cloned.
func migrateTemplate(ctx context.Context, templateDSN string) error {
	template, err := database.Open(ctx, templateDSN)
	if err != nil {
		return fmt.Errorf("migrate template database: %w", err)
	}
	if err := template.Close(); err != nil {
		return fmt.Errorf("close template database pool: %w", err)
	}
	return nil
}

// createClone clones the migrated template into this invocation's unique
// package database. A name collision fails setup instead of dropping anything.
func createClone(ctx context.Context, conn *sql.Conn, cfg config) error {
	statement := fmt.Sprintf("CREATE DATABASE %s TEMPLATE %s",
		pq.QuoteIdentifier(cfg.cloneName), pq.QuoteIdentifier(cfg.templateName))
	if _, err := conn.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("create clone database %s: %w", cfg.cloneName, err)
	}
	return nil
}

// openClonePool opens the shared package pool for the already-migrated clone.
func openClonePool(ctx context.Context, cloneDSN string) (*sql.DB, error) {
	pool, err := sql.Open("pgx", cloneDSN)
	if err != nil {
		return nil, fmt.Errorf("open clone pool: %w", err)
	}
	pool.SetMaxOpenConns(10)
	pool.SetMaxIdleConns(5)
	pool.SetConnMaxLifetime(5 * time.Minute)
	pool.SetConnMaxIdleTime(time.Minute)
	if err := pool.PingContext(ctx); err != nil {
		_ = pool.Close()
		return nil, fmt.Errorf("ping clone database: %w", err)
	}
	return pool, nil
}

// Close closes the package pool, drops the clone this invocation created, and
// closes the maintenance pool, joining every teardown error.
func (i *instance) Close(ctx context.Context) error {
	var failures []error
	if i.db != nil {
		if err := i.db.Close(); err != nil {
			failures = append(failures, fmt.Errorf("close package pool for %s: %w", i.cfg.cloneName, err))
		}
	}
	if i.createdClone && i.maintenance != nil {
		if err := dropDatabase(ctx, i.maintenance, i.cfg.cloneName); err != nil {
			failures = append(failures, fmt.Errorf("drop clone database %s: %w", i.cfg.cloneName, err))
		}
	}
	if i.maintenance != nil {
		if err := i.maintenance.Close(); err != nil {
			failures = append(failures, fmt.Errorf("close maintenance pool: %w", err))
		}
	}
	return errors.Join(failures...)
}

// dropDatabase drops one database whose generated name every caller has already
// validated. PostgreSQL does not accept identifiers as query parameters, so the
// name is re-checked against the identifier allowlist and quoted with
// pq.QuoteIdentifier immediately before it is interpolated.
func dropDatabase(ctx context.Context, maintenance *sql.DB, name string) error {
	if !identifierPattern.MatchString(name) {
		return fmt.Errorf("refuse to drop database with unsafe name %q", name)
	}
	statement := fmt.Sprintf("DROP DATABASE %s WITH (FORCE)", pq.QuoteIdentifier(name))
	//nolint:gosec // G701 cannot see that name is allowlist-validated and quoted on the line above.
	if _, err := maintenance.ExecContext(ctx, statement); err != nil {
		return err
	}
	return nil
}

// Cleanup removes ephemeral clone databases left behind by interrupted test
// runs. Only databases matching the harness-owned base/package/suffix
// convention with no attached session are dropped; the base and template
// databases are never candidates.
func Cleanup(ctx context.Context, rawURL string) error {
	return sanitizeError(cleanupStaleClones(ctx, rawURL), rawURL)
}

func cleanupStaleClones(ctx context.Context, rawURL string) (err error) {
	base, err := parseBaseConfig(rawURL)
	if err != nil {
		return err
	}

	maintenance, err := sql.Open("pgx", base.maintenanceDSN)
	if err != nil {
		return fmt.Errorf("open maintenance pool: %w", err)
	}
	defer func() {
		closeErr := maintenance.Close()
		if closeErr != nil {
			closeErr = fmt.Errorf("close maintenance pool: %w", closeErr)
		}
		err = errors.Join(err, closeErr)
	}()

	if err := maintenance.PingContext(ctx); err != nil {
		return fmt.Errorf("connect to maintenance database: %w", err)
	}

	names, err := listDatabases(ctx, maintenance)
	if err != nil {
		return err
	}

	var failures []error
	for _, name := range names {
		if !isEphemeralClone(base.baseName, name) {
			continue
		}
		active, err := activeSessions(ctx, maintenance, name)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if active > 0 {
			continue
		}
		if err := dropDatabase(ctx, maintenance, name); err != nil {
			failures = append(failures, fmt.Errorf("drop stale clone database %s: %w", name, err))
		}
	}

	return errors.Join(failures...)
}

// listDatabases collects every database name before any further query runs on
// the same pool.
func listDatabases(ctx context.Context, maintenance *sql.DB) ([]string, error) {
	rows, err := maintenance.QueryContext(ctx, `SELECT datname FROM pg_database`)
	if err != nil {
		return nil, fmt.Errorf("list databases: %w", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan database name: %w", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate databases: %w", err)
	}
	return names, nil
}

// activeSessions counts connections attached to one database.
func activeSessions(ctx context.Context, maintenance *sql.DB, name string) (int, error) {
	var count int
	if err := maintenance.QueryRowContext(ctx,
		`SELECT count(*) FROM pg_stat_activity WHERE datname = $1`, name,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count sessions for database %s: %w", name, err)
	}
	return count, nil
}
