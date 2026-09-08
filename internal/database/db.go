package database

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"
	"time"

	// Registers the "pgx" driver with database/sql; imported for side effects only.
	_ "github.com/jackc/pgx/v5/stdlib"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Open creates and configures a PostgreSQL connection pool with embedded migrations.
func Open(ctx context.Context, databaseURL string) (*sql.DB, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open postgres database: %w", err)
	}

	// Configure connection pool for production
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(time.Hour)
	db.SetConnMaxIdleTime(15 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping postgres database: %w", err)
	}

	if err := runMigrations(ctx, db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	return db, nil
}

const migrationAdvisoryLockID = 7142981

func runMigrations(ctx context.Context, db *sql.DB) error {
	// 1. Acquire PostgreSQL advisory lock to ensure single-instance migration execution
	if _, err := db.ExecContext(ctx, "SELECT pg_advisory_lock($1)", migrationAdvisoryLockID); err != nil {
		return fmt.Errorf("acquire migration advisory lock: %w", err)
	}
	defer func() {
		if _, err := db.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", migrationAdvisoryLockID); err != nil {
			slog.Error("failed to release migration advisory lock", "error", err)
		}
	}()

	// 2. Ensure schema_migrations table exists
	const createSchemaMigrationsTable = `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version VARCHAR(255) PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`
	if _, err := db.ExecContext(ctx, createSchemaMigrationsTable); err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}

	// 3. Query already applied migrations
	rows, err := db.QueryContext(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return fmt.Errorf("query applied migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[string]bool)
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return fmt.Errorf("scan applied migration version: %w", err)
		}
		applied[version] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate applied migrations: %w", err)
	}

	// 4. Read embedded migration directory and sort files
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	// 5. Apply each pending migration inside a transaction
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		version := entry.Name()
		if applied[version] {
			continue
		}

		content, err := migrationFS.ReadFile("migrations/" + version)
		if err != nil {
			return fmt.Errorf("read migration file %s: %w", version, err)
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin transaction for migration %s: %w", version, err)
		}

		if _, err := tx.ExecContext(ctx, string(content)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("execute migration %s: %w", version, err)
		}

		if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", version); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %s: %w", version, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", version, err)
		}

		slog.Info("applied database migration", "version", version)
	}

	return nil
}
