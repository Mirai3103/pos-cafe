//go:build integration

package database_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/database"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("skipping integration test: TEST_DATABASE_URL is not set")
	}

	// Same safety guard as the slice integration tests: never touch a database
	// whose name does not end in _test.
	if !strings.Contains(dbURL, "_test?") && !strings.HasSuffix(dbURL, "_test") {
		t.Fatalf("SAFETY VIOLATION: TEST_DATABASE_URL must target a database name ending with '_test'. Got: %s", dbURL)
	}

	db, err := database.Open(context.Background(), dbURL)
	require.NoError(t, err)

	_, err = db.ExecContext(context.Background(), "TRUNCATE TABLE menu_categories CASCADE")
	require.NoError(t, err)

	t.Cleanup(func() {
		_, cleanErr := db.ExecContext(context.Background(), "TRUNCATE TABLE menu_categories CASCADE")
		assert.NoError(t, cleanErr)
		assert.NoError(t, db.Close())
	})

	return db
}

func TestWithTxCommitsOnSuccess(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	err := database.WithTx(ctx, db, func(q *sqlc.Queries) error {
		for _, name := range []string{"Espresso Bar", "Cold Brew"} {
			if _, err := q.CreateMenuCategory(ctx, sqlc.CreateMenuCategoryParams{
				Name:           name,
				NormalizedName: strings.ToLower(name),
			}); err != nil {
				return err
			}
		}
		return nil
	})
	require.NoError(t, err)

	rows, err := sqlc.New(db).ListMenuCategories(ctx)
	require.NoError(t, err)
	assert.Len(t, rows, 2)
}

func TestWithTxRollsBackOnError(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	sentinel := errors.New("business rule violated")

	// The first insert succeeds, so a missing rollback would leave it behind.
	err := database.WithTx(ctx, db, func(q *sqlc.Queries) error {
		if _, err := q.CreateMenuCategory(ctx, sqlc.CreateMenuCategoryParams{
			Name:           "Espresso Bar",
			NormalizedName: "espresso bar",
		}); err != nil {
			return err
		}
		return sentinel
	})
	require.ErrorIs(t, err, sentinel)

	rows, err := sqlc.New(db).ListMenuCategories(ctx)
	require.NoError(t, err)
	assert.Empty(t, rows, "the successful insert must have been rolled back")
}

func TestWithTxRollsBackOnPanic(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	assert.Panics(t, func() {
		_ = database.WithTx(ctx, db, func(q *sqlc.Queries) error {
			if _, err := q.CreateMenuCategory(ctx, sqlc.CreateMenuCategoryParams{
				Name:           "Espresso Bar",
				NormalizedName: "espresso bar",
			}); err != nil {
				return err
			}
			panic("handler blew up")
		})
	}, "the panic must propagate to the caller, not be swallowed")

	rows, err := sqlc.New(db).ListMenuCategories(ctx)
	require.NoError(t, err)
	assert.Empty(t, rows, "the panic must not leave the write committed")
}
