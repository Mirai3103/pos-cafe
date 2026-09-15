//go:build integration

package database_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/database"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()

	_, err := databaseTestDB.ExecContext(context.Background(), "TRUNCATE TABLE menu_categories CASCADE")
	require.NoError(t, err)

	t.Cleanup(func() {
		_, cleanErr := databaseTestDB.ExecContext(context.Background(), "TRUNCATE TABLE menu_categories CASCADE")
		assert.NoError(t, cleanErr)
	})

	return databaseTestDB
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
