//go:build integration

package catalog_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/catalog"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	directGroupsOfItem = `SELECT modifier_group_id FROM item_modifier_groups WHERE menu_item_id = $1 ORDER BY modifier_group_id`
	groupsOfCategory   = `SELECT modifier_group_id FROM category_modifier_groups WHERE menu_category_id = $1 ORDER BY modifier_group_id`
)

func TestReplaceItemModifierGroups(t *testing.T) {
	db, q := openExecutorTestDB(t)
	handler := catalog.NewReplaceItemModifierGroupsHandler(catalog.NewRunner(db, q))
	ctx := context.Background()
	price := int64(30000)

	t.Run("replaces direct and excluded sets in one audit event", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		cat := createTestCategoryDirect(t, db, "Coffee")
		sugar := createTestModifierGroupDirect(t, db, "Sugar", 0, 1, false)
		ice := createTestModifierGroupDirect(t, db, "Ice", 0, 1, false)
		topping := createTestModifierGroupDirect(t, db, "Topping", 0, 1, false)
		cheese := createTestModifierGroupDirect(t, db, "Cheese", 0, 1, false)
		attachCategoryGroupDirect(t, db, cat, sugar)
		attachCategoryGroupDirect(t, db, cat, ice)
		item := createTestItemDirect(t, db, cat, "Latte", &price, false)
		attachItemGroupDirect(t, db, item, topping)
		excludeItemGroupDirect(t, db, item, sugar)

		status, res, err := handler.Handle(ctx, managerActor(t, db, q), catalog.ReplaceItemModifierGroupsCommand{
			RequestID: uuid.New(), ItemID: item, DirectGroupIDs: []uuid.UUID{cheese}, ExcludedGroupIDs: []uuid.UUID{ice},
		})
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.Equal(t, []uuid.UUID{cheese}, res.DirectGroupIDs)
		assert.Equal(t, []uuid.UUID{cheese}, idsFrom(t, db, directGroupsOfItem, item))
		assert.Equal(t, []uuid.UUID{ice}, idsFrom(t, db, exclusionsOfItem, item))
		assert.Equal(t, 1, auditCount(t, db, catalog.EventItemModifierGroupsReplaced))
	})

	t.Run("an identical set is a no-op without audit", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		cat := createTestCategoryDirect(t, db, "Coffee")
		topping := createTestModifierGroupDirect(t, db, "Topping", 0, 1, false)
		item := createTestItemDirect(t, db, cat, "Latte", &price, false)
		attachItemGroupDirect(t, db, item, topping)
		_, _, err := handler.Handle(ctx, managerActor(t, db, q), catalog.ReplaceItemModifierGroupsCommand{
			RequestID: uuid.New(), ItemID: item, DirectGroupIDs: []uuid.UUID{topping},
		})
		require.NoError(t, err)
		assert.Equal(t, 0, auditCount(t, db, catalog.EventItemModifierGroupsReplaced))
	})

	t.Run("excluding a group the category does not provide is INVALID_INHERITANCE", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		cat := createTestCategoryDirect(t, db, "Coffee")
		other := createTestModifierGroupDirect(t, db, "Other", 0, 1, false)
		item := createTestItemDirect(t, db, cat, "Latte", &price, false)
		_, _, err := handler.Handle(ctx, managerActor(t, db, q), catalog.ReplaceItemModifierGroupsCommand{
			RequestID: uuid.New(), ItemID: item, ExcludedGroupIDs: []uuid.UUID{other},
		})
		assert.True(t, errors.Is(err, catalog.ErrInvalidInheritance))
	})

	t.Run("a group both direct and excluded is INVALID_INHERITANCE", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		cat := createTestCategoryDirect(t, db, "Coffee")
		sugar := createTestModifierGroupDirect(t, db, "Sugar", 0, 1, false)
		attachCategoryGroupDirect(t, db, cat, sugar)
		item := createTestItemDirect(t, db, cat, "Latte", &price, false)
		_, _, err := handler.Handle(ctx, managerActor(t, db, q), catalog.ReplaceItemModifierGroupsCommand{
			RequestID: uuid.New(), ItemID: item, DirectGroupIDs: []uuid.UUID{sugar}, ExcludedGroupIDs: []uuid.UUID{sugar},
		})
		assert.True(t, errors.Is(err, catalog.ErrInvalidInheritance))
	})

	t.Run("adding a retired group is ErrEntityRetired; keeping one is fine", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		cat := createTestCategoryDirect(t, db, "Coffee")
		retired := createTestModifierGroupDirect(t, db, "Old", 0, 1, true)
		kept := createTestModifierGroupDirect(t, db, "Kept", 0, 1, true)
		item := createTestItemDirect(t, db, cat, "Latte", &price, false)
		attachItemGroupDirect(t, db, item, kept)

		_, _, err := handler.Handle(ctx, managerActor(t, db, q), catalog.ReplaceItemModifierGroupsCommand{
			RequestID: uuid.New(), ItemID: item, DirectGroupIDs: []uuid.UUID{kept, retired},
		})
		assert.True(t, errors.Is(err, catalog.ErrEntityRetired))

		_, _, err = handler.Handle(ctx, managerActor(t, db, q), catalog.ReplaceItemModifierGroupsCommand{
			RequestID: uuid.New(), ItemID: item, DirectGroupIDs: []uuid.UUID{kept},
		})
		require.NoError(t, err)
	})

	t.Run("unknown group is ErrNotFound; duplicates are INVALID_INPUT", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		cat := createTestCategoryDirect(t, db, "Coffee")
		g := createTestModifierGroupDirect(t, db, "G", 0, 1, false)
		item := createTestItemDirect(t, db, cat, "Latte", &price, false)
		_, _, err := handler.Handle(ctx, managerActor(t, db, q), catalog.ReplaceItemModifierGroupsCommand{
			RequestID: uuid.New(), ItemID: item, DirectGroupIDs: []uuid.UUID{uuid.New()},
		})
		assert.True(t, errors.Is(err, catalog.ErrNotFound))
		_, _, err = handler.Handle(ctx, managerActor(t, db, q), catalog.ReplaceItemModifierGroupsCommand{
			RequestID: uuid.New(), ItemID: item, DirectGroupIDs: []uuid.UUID{g, g},
		})
		assert.Error(t, err)
	})
}

func TestReplaceCategoryModifierGroups(t *testing.T) {
	db, q := openExecutorTestDB(t)
	handler := catalog.NewReplaceCategoryModifierGroupsHandler(catalog.NewRunner(db, q))
	ctx := context.Background()
	price := int64(30000)

	t.Run("removing a group deletes its exclusions in the category", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		cat := createTestCategoryDirect(t, db, "Coffee")
		sugar := createTestModifierGroupDirect(t, db, "Sugar", 0, 1, false)
		ice := createTestModifierGroupDirect(t, db, "Ice", 0, 1, false)
		topping := createTestModifierGroupDirect(t, db, "Topping", 0, 1, false)
		attachCategoryGroupDirect(t, db, cat, sugar)
		attachCategoryGroupDirect(t, db, cat, ice)
		latte := createTestItemDirect(t, db, cat, "Latte", &price, false)
		mocha := createTestItemDirect(t, db, cat, "Mocha", &price, false)
		excludeItemGroupDirect(t, db, latte, sugar)
		excludeItemGroupDirect(t, db, mocha, ice)

		status, res, err := handler.Handle(ctx, managerActor(t, db, q), catalog.ReplaceCategoryModifierGroupsCommand{
			RequestID: uuid.New(), CategoryID: cat, GroupIDs: []uuid.UUID{ice, topping},
		})
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.ElementsMatch(t, []uuid.UUID{ice, topping}, idsFrom(t, db, groupsOfCategory, cat))
		assert.Equal(t, []catalog.ExclusionRef{{ItemID: latte, ModifierGroupID: sugar}}, res.RemovedExclusions)
		assert.Empty(t, idsFrom(t, db, exclusionsOfItem, latte))
		assert.Equal(t, []uuid.UUID{ice}, idsFrom(t, db, exclusionsOfItem, mocha), "exclusions of kept groups stay")
		assert.Equal(t, 1, auditCount(t, db, catalog.EventCategoryModifierGroupsReplaced))
	})

	t.Run("retired category is ErrEntityRetired", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		cat := createTestCategoryDirect(t, db, "Coffee")
		retireCategoryDirect(t, db, cat)
		_, _, err := handler.Handle(ctx, managerActor(t, db, q), catalog.ReplaceCategoryModifierGroupsCommand{RequestID: uuid.New(), CategoryID: cat})
		assert.True(t, errors.Is(err, catalog.ErrEntityRetired))
	})
}

func TestReplaceGroupAssignments(t *testing.T) {
	db, q := openExecutorTestDB(t)
	handler := catalog.NewReplaceGroupAssignmentsHandler(catalog.NewRunner(db, q))
	ctx := context.Background()
	price := int64(30000)
	const itemsWithGroup = `SELECT menu_item_id FROM item_modifier_groups WHERE modifier_group_id = $1 ORDER BY menu_item_id`
	const categoriesWithGroup = `SELECT menu_category_id FROM category_modifier_groups WHERE modifier_group_id = $1 ORDER BY menu_category_id`

	t.Run("sets exactly the items and categories, one audit event", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		coffee := createTestCategoryDirect(t, db, "Coffee")
		tea := createTestCategoryDirect(t, db, "Tea")
		topping := createTestModifierGroupDirect(t, db, "Topping", 0, 3, false)
		latte := createTestItemDirect(t, db, coffee, "Latte", &price, false)
		mocha := createTestItemDirect(t, db, coffee, "Mocha", &price, false)
		peach := createTestItemDirect(t, db, tea, "Peach tea", &price, false)
		attachItemGroupDirect(t, db, latte, topping)
		attachCategoryGroupDirect(t, db, coffee, topping)
		excludeItemGroupDirect(t, db, mocha, topping)

		status, res, err := handler.Handle(ctx, managerActor(t, db, q), catalog.ReplaceGroupAssignmentsCommand{
			RequestID: uuid.New(), GroupID: topping, ItemIDs: []uuid.UUID{peach}, CategoryIDs: []uuid.UUID{tea},
		})
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.Equal(t, []uuid.UUID{peach}, idsFrom(t, db, itemsWithGroup, topping))
		assert.Equal(t, []uuid.UUID{tea}, idsFrom(t, db, categoriesWithGroup, topping))
		assert.Equal(t, []catalog.ExclusionRef{{ItemID: mocha, ModifierGroupID: topping}}, res.RemovedExclusions,
			"coffee no longer provides the group, so mocha's exclusion goes")
		assert.Equal(t, 1, auditCount(t, db, catalog.EventModifierGroupAssignmentsReplaced))
	})

	t.Run("adding an item that excludes the group is INVALID_INHERITANCE", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		coffee := createTestCategoryDirect(t, db, "Coffee")
		sugar := createTestModifierGroupDirect(t, db, "Sugar", 0, 1, false)
		attachCategoryGroupDirect(t, db, coffee, sugar)
		latte := createTestItemDirect(t, db, coffee, "Latte", &price, false)
		excludeItemGroupDirect(t, db, latte, sugar)
		_, _, err := handler.Handle(ctx, managerActor(t, db, q), catalog.ReplaceGroupAssignmentsCommand{
			RequestID: uuid.New(), GroupID: sugar, ItemIDs: []uuid.UUID{latte}, CategoryIDs: []uuid.UUID{coffee},
		})
		assert.True(t, errors.Is(err, catalog.ErrInvalidInheritance))
	})

	t.Run("adding a retired item is ErrEntityRetired; unknown is ErrNotFound", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		coffee := createTestCategoryDirect(t, db, "Coffee")
		g := createTestModifierGroupDirect(t, db, "G", 0, 1, false)
		retired := createTestItemDirect(t, db, coffee, "Old", &price, true)
		_, _, err := handler.Handle(ctx, managerActor(t, db, q), catalog.ReplaceGroupAssignmentsCommand{
			RequestID: uuid.New(), GroupID: g, ItemIDs: []uuid.UUID{retired},
		})
		assert.True(t, errors.Is(err, catalog.ErrEntityRetired))
		_, _, err = handler.Handle(ctx, managerActor(t, db, q), catalog.ReplaceGroupAssignmentsCommand{
			RequestID: uuid.New(), GroupID: g, CategoryIDs: []uuid.UUID{uuid.New()},
		})
		assert.True(t, errors.Is(err, catalog.ErrNotFound))
	})

	t.Run("retired group is ErrEntityRetired", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		g := createTestModifierGroupDirect(t, db, "Old", 0, 1, true)
		_, _, err := handler.Handle(ctx, managerActor(t, db, q), catalog.ReplaceGroupAssignmentsCommand{RequestID: uuid.New(), GroupID: g})
		assert.True(t, errors.Is(err, catalog.ErrEntityRetired))
	})
}
