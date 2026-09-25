//go:build integration

package catalog_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCatalogDisplayFieldConstraints(t *testing.T) {
	db, _ := openExecutorTestDB(t)
	ctx := context.Background()
	cleanCategoryTestTables(t, db)
	catID := createTestCategoryDirect(t, db, "Coffee")
	price := int64(30000)
	itemA := createTestItemDirect(t, db, catID, "Americano", &price, false)
	itemB := createTestItemDirect(t, db, catID, "Bac xiu", &price, false)

	t.Run("badge outside the enum is rejected", func(t *testing.T) {
		_, err := db.ExecContext(ctx, `UPDATE menu_items SET badge = 'FAVORITE' WHERE id = $1`, itemA)
		require.Error(t, err)
	})

	t.Run("code requires its normalized form", func(t *testing.T) {
		_, err := db.ExecContext(ctx, `UPDATE menu_items SET code = 'X', normalized_code = NULL WHERE id = $1`, itemA)
		require.Error(t, err)
	})

	t.Run("active codes are unique", func(t *testing.T) {
		_, err := db.ExecContext(ctx, `UPDATE menu_items SET code = 'CF', normalized_code = 'cf' WHERE id = $1`, itemA)
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, `UPDATE menu_items SET code = 'cf', normalized_code = 'cf' WHERE id = $1`, itemB)
		var pgErr *pgconn.PgError
		require.ErrorAs(t, err, &pgErr)
		assert.Equal(t, "menu_items_active_code_key", pgErr.ConstraintName)
	})

	t.Run("a retired item's code can be reused", func(t *testing.T) {
		retired := createTestItemDirect(t, db, catID, "Old coffee", &price, true)
		_, err := db.ExecContext(ctx, `UPDATE menu_items SET code = 'cf', normalized_code = 'cf' WHERE id = $1`, retired)
		require.NoError(t, err)
	})

	t.Run("description is capped at 300 characters", func(t *testing.T) {
		_, err := db.ExecContext(ctx, `UPDATE menu_items SET description = $2 WHERE id = $1`, itemA, strings.Repeat("a", 301))
		require.Error(t, err)
	})

	t.Run("image key must be a content hash", func(t *testing.T) {
		_, err := db.ExecContext(ctx, `UPDATE menu_items SET image_key = 'x.png' WHERE id = $1`, itemA)
		require.Error(t, err)
		_, err = db.ExecContext(ctx, `UPDATE menu_items SET image_key = $2 WHERE id = $1`, itemA, strings.Repeat("a", 64)+".webp")
		require.NoError(t, err)
	})

	t.Run("category icon and order are checked", func(t *testing.T) {
		_, err := db.ExecContext(ctx, `UPDATE menu_categories SET icon = 'Coffee Cup' WHERE id = $1`, catID)
		require.Error(t, err)
		_, err = db.ExecContext(ctx, `UPDATE menu_categories SET display_order = 10000 WHERE id = $1`, catID)
		require.Error(t, err)
		_, err = db.ExecContext(ctx, `UPDATE menu_categories SET icon = 'cup-soda', display_order = 3 WHERE id = $1`, catID)
		require.NoError(t, err)
	})

	t.Run("categories list by display order, then name", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		zeta := createTestCategoryDirect(t, db, "Zeta")
		alpha := createTestCategoryDirect(t, db, "Alpha")
		_, err := db.ExecContext(ctx,
			`UPDATE menu_categories SET display_order = CASE WHEN id = $1 THEN 1 ELSE 2 END`, zeta)
		require.NoError(t, err)

		cats, err := sqlc.New(db).ListMenuCategories(ctx)
		require.NoError(t, err)
		require.Len(t, cats, 2)
		assert.Equal(t, zeta, cats[0].ID)
		assert.Equal(t, alpha, cats[1].ID)
		assert.Equal(t, int32(1), cats[0].DisplayOrder)
	})
}
