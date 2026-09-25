//go:build integration

package catalog_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/catalog"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func attachCategoryGroupDirect(t *testing.T, db *sql.DB, catID, groupID uuid.UUID) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO category_modifier_groups (menu_category_id, modifier_group_id) VALUES ($1, $2)`, catID, groupID)
	require.NoError(t, err)
}

func attachItemGroupDirect(t *testing.T, db *sql.DB, itemID, groupID uuid.UUID) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO item_modifier_groups (menu_item_id, modifier_group_id) VALUES ($1, $2)`, itemID, groupID)
	require.NoError(t, err)
}

func excludeItemGroupDirect(t *testing.T, db *sql.DB, itemID, groupID uuid.UUID) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO item_modifier_group_exclusions (menu_item_id, modifier_group_id) VALUES ($1, $2)`, itemID, groupID)
	require.NoError(t, err)
}

// idsFrom runs a one-argument query returning a single uuid column.
func idsFrom(t *testing.T, db *sql.DB, query string, arg uuid.UUID) []uuid.UUID {
	t.Helper()
	rows, err := db.Query(query, arg)
	require.NoError(t, err)
	defer rows.Close()
	out := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		require.NoError(t, rows.Scan(&id))
		out = append(out, id)
	}
	require.NoError(t, rows.Err())
	return out
}

const exclusionsOfItem = `SELECT modifier_group_id FROM item_modifier_group_exclusions WHERE menu_item_id = $1 ORDER BY modifier_group_id`

func TestMoveItemCategory(t *testing.T) {
	db, q := openExecutorTestDB(t)
	handler := catalog.NewMoveItemCategoryHandler(catalog.NewRunner(db, q))
	ctx := context.Background()
	price := int64(30000)

	t.Run("moves and drops exclusions the new category does not provide", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		actor := managerActor(t, db, q)
		from := createTestCategoryDirect(t, db, "Coffee")
		to := createTestCategoryDirect(t, db, "Tea")
		sugar := createTestModifierGroupDirect(t, db, "Sugar", 0, 1, false)
		ice := createTestModifierGroupDirect(t, db, "Ice", 0, 1, false)
		attachCategoryGroupDirect(t, db, from, sugar)
		attachCategoryGroupDirect(t, db, from, ice)
		attachCategoryGroupDirect(t, db, to, ice)
		item := createTestItemDirect(t, db, from, "Latte", &price, false)
		excludeItemGroupDirect(t, db, item, sugar)
		excludeItemGroupDirect(t, db, item, ice)

		status, res, err := handler.Handle(ctx, actor, catalog.MoveItemCategoryCommand{RequestID: uuid.New(), ItemID: item, CategoryID: to})
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.Equal(t, to, res.CategoryID)
		assert.Equal(t, []uuid.UUID{sugar}, res.RemovedExclusionGroupIDs)
		assert.Equal(t, []uuid.UUID{ice}, idsFrom(t, db, exclusionsOfItem, item), "the exclusion the new category provides stays")

		var details []byte
		require.NoError(t, db.QueryRow(`SELECT details FROM audit_events WHERE event_type = $1`, catalog.EventItemCategoryChanged).Scan(&details))
		var audit struct {
			From    uuid.UUID   `json:"from_category_id"`
			To      uuid.UUID   `json:"to_category_id"`
			Removed []uuid.UUID `json:"removed_exclusion_group_ids"`
		}
		require.NoError(t, json.Unmarshal(details, &audit))
		assert.Equal(t, from, audit.From)
		assert.Equal(t, to, audit.To)
		assert.Equal(t, []uuid.UUID{sugar}, audit.Removed)
	})

	t.Run("moving to the current category is a no-op without audit", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		cat := createTestCategoryDirect(t, db, "Coffee")
		item := createTestItemDirect(t, db, cat, "Latte", &price, false)
		_, res, err := handler.Handle(ctx, managerActor(t, db, q), catalog.MoveItemCategoryCommand{RequestID: uuid.New(), ItemID: item, CategoryID: cat})
		require.NoError(t, err)
		assert.Equal(t, cat, res.CategoryID)
		assert.Empty(t, res.RemovedExclusionGroupIDs)
		assert.Equal(t, 0, auditCount(t, db, catalog.EventItemCategoryChanged))
	})

	t.Run("a name taken in the target is CATALOG_NAME_CONFLICT", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		from := createTestCategoryDirect(t, db, "Coffee")
		to := createTestCategoryDirect(t, db, "Tea")
		item := createTestItemDirect(t, db, from, "Latte", &price, false)
		createTestItemDirect(t, db, to, "latte", &price, false)
		_, _, err := handler.Handle(ctx, managerActor(t, db, q), catalog.MoveItemCategoryCommand{RequestID: uuid.New(), ItemID: item, CategoryID: to})
		assert.True(t, errors.Is(err, catalog.ErrNameConflict), "got %v", err)
	})

	t.Run("retired target is ErrEntityRetired", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		from := createTestCategoryDirect(t, db, "Coffee")
		to := createTestCategoryDirect(t, db, "Tea")
		retireCategoryDirect(t, db, to)
		item := createTestItemDirect(t, db, from, "Latte", &price, false)
		_, _, err := handler.Handle(ctx, managerActor(t, db, q), catalog.MoveItemCategoryCommand{RequestID: uuid.New(), ItemID: item, CategoryID: to})
		assert.True(t, errors.Is(err, catalog.ErrEntityRetired))
	})

	t.Run("unknown target is ErrNotFound", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		from := createTestCategoryDirect(t, db, "Coffee")
		item := createTestItemDirect(t, db, from, "Latte", &price, false)
		_, _, err := handler.Handle(ctx, managerActor(t, db, q), catalog.MoveItemCategoryCommand{RequestID: uuid.New(), ItemID: item, CategoryID: uuid.New()})
		assert.True(t, errors.Is(err, catalog.ErrNotFound))
	})
}
