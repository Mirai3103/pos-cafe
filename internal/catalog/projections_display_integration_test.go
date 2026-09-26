//go:build integration

package catalog_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/catalog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProjectionsCarryDisplayFields(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()
	cleanCategoryTestTables(t, db)

	manager := managerActor(t, db, q)
	baristaIdent := createCatalogTestIdentity(t, db, q, []string{auth.RoleBarista}, true, "")
	barista := catalog.Actor{StaffID: baristaIdent.StaffID, SessionID: baristaIdent.SessionID}

	tea := createTestCategoryDirect(t, db, "Tea")
	coffee := createTestCategoryDirect(t, db, "Coffee")
	_, err := db.Exec(`UPDATE menu_categories SET display_order = 1, icon = 'coffee' WHERE id = $1`, coffee)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE menu_categories SET display_order = 2 WHERE id = $1`, tea)
	require.NoError(t, err)

	price := int64(35000)
	croissant := createTestItemDirect(t, db, coffee, "Croissant", &price, false)
	key := strings.Repeat("a", 64) + ".webp"
	_, err = db.Exec(`UPDATE menu_items SET code = 'CB', normalized_code = 'cb', badge = 'HOT', description = 'Bơ tỏi', image_key = $2 WHERE id = $1`, croissant, key)
	require.NoError(t, err)
	latte := createTestItemDirect(t, db, tea, "Latte", nil, false)
	createTestSizeDirect(t, db, latte, "Size L", 45000, false)
	createTestSizeDirect(t, db, latte, "Size M", 39000, false)
	createTestSizeDirect(t, db, latte, "Size S", 20000, true) // retired: ignored for the card price

	group := createTestModifierGroupDirect(t, db, "Topping", 0, 1, false)
	createTestModifierOptionDirect(t, db, group, "Pearl", 5000, true, false)
	attachCategoryGroupDirect(t, db, tea, group)

	t.Run("management menu", func(t *testing.T) {
		res, err := catalog.NewManagementMenuHandler(runner).Handle(ctx, manager)
		require.NoError(t, err)
		require.Len(t, res.Categories, 2)
		assert.Equal(t, coffee, res.Categories[0].ID, "display_order first")
		assert.Equal(t, "coffee", *res.Categories[0].Icon)
		assert.Equal(t, int32(1), res.Categories[0].DisplayOrder)
		item := res.Categories[0].Items[0]
		assert.Equal(t, "CB", *item.Code)
		assert.Equal(t, "HOT", *item.Badge)
		assert.Equal(t, "Bơ tỏi", *item.Description)
		assert.Equal(t, "/media/catalog/"+key, *item.ImageURL)
	})

	t.Run("sellable menu", func(t *testing.T) {
		res, err := catalog.NewSellableMenuHandler(runner).Handle(ctx, manager)
		require.NoError(t, err)
		require.Equal(t, coffee, res.Categories[0].ID)
		assert.Equal(t, "coffee", *res.Categories[0].Icon)
		item := res.Categories[0].Items[0]
		assert.Equal(t, "CB", *item.Code)
		assert.Equal(t, "HOT", *item.Badge)
		assert.Equal(t, "/media/catalog/"+key, *item.ImageURL)
	})

	t.Run("availability menu shows prices to a manager", func(t *testing.T) {
		res, err := catalog.NewAvailabilityMenuHandler(runner).Handle(ctx, manager)
		require.NoError(t, err)
		require.Len(t, res.Categories, 2)
		c := res.Categories[0].Items[0]
		assert.Equal(t, "CB", *c.Code)
		assert.Equal(t, "/media/catalog/"+key, *c.ImageURL)
		require.NotNil(t, c.PriceVND)
		assert.Equal(t, int64(35000), *c.PriceVND)

		l := res.Categories[1].Items[0]
		require.NotNil(t, l.PriceVND)
		assert.Equal(t, int64(39000), *l.PriceVND, "lowest non-retired size")
		require.NotNil(t, l.ModifierGroups[0].Options[0].SurchargeVND)
		assert.Equal(t, int64(5000), *l.ModifierGroups[0].Options[0].SurchargeVND)
	})

	t.Run("availability menu hides prices from a barista", func(t *testing.T) {
		res, err := catalog.NewAvailabilityMenuHandler(runner).Handle(ctx, barista)
		require.NoError(t, err)
		assert.Nil(t, res.Categories[0].Items[0].PriceVND)
		assert.Nil(t, res.Categories[1].Items[0].PriceVND)
		assert.Nil(t, res.Categories[1].Items[0].ModifierGroups[0].Options[0].SurchargeVND)
		assert.Equal(t, "CB", *res.Categories[0].Items[0].Code, "non-price fields stay")
	})
}
