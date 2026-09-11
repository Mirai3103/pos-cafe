//go:build integration

package catalog_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/catalog"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type seededCatalog struct {
	CatCoffee uuid.UUID
	CatTea    uuid.UUID
	CatEmpty  uuid.UUID
	CatUnsell uuid.UUID

	ItemEspressoDirect     uuid.UUID // available, direct price 30000
	ItemAmericanoSized     uuid.UUID // available, size small available (25000), size large available (35000)
	ItemLattePartAvailSize uuid.UUID // available, size small available (30000), size large unavailable (40000)
	ItemAllSizesUnavail    uuid.UUID // available, but all sizes unavailable
	ItemDirectUnavail      uuid.UUID // unavailable direct item
	ItemRetired            uuid.UUID // retired item
	ItemRequiredModAvail   uuid.UUID // has required group with available options
	ItemRequiredModUnavail uuid.UUID // has required group with all options unavailable
	ItemExcludedGroup      uuid.UUID // has category group excluded
	ItemDirectAndInherited uuid.UUID // has direct and category group, deduplicated
	ItemRetiredReqGroup    uuid.UUID // has required group, but the group is retired

	GroupCategorySugar  uuid.UUID // min: 1, max: 1 (attached to CatCoffee)
	GroupExtraEspresso  uuid.UUID // min: 0, max: 2
	GroupRequiredIce    uuid.UUID // min: 1, max: 1
	GroupUnavailOptions uuid.UUID // min: 1, max: 1, all options unavailable
	GroupRetired        uuid.UUID // retired group (min: 1, max: 1)

	OptSugarNormal  uuid.UUID // available, surcharge 0
	OptSugarLess    uuid.UUID // available, surcharge 0
	OptSugarRetired uuid.UUID // retired, surcharge 0
	OptExtraShot    uuid.UUID // available, surcharge 10000
	OptSyrup        uuid.UUID // unavailable, surcharge 5000
	OptIceNormal    uuid.UUID // available, surcharge 0
	OptIceNone      uuid.UUID // unavailable, surcharge 0
	OptIceRetired   uuid.UUID // retired
	OptUnavail1     uuid.UUID // unavailable
	OptRetiredGrp1  uuid.UUID // in retired group
}

func seedTestCatalog(t *testing.T, db *sql.DB, q *sqlc.Queries) seededCatalog {
	t.Helper()
	ctx := context.Background()

	// 1. Categories
	catCoffee, err := q.CreateMenuCategory(ctx, sqlc.CreateMenuCategoryParams{
		Name: "Coffee", NormalizedName: "coffee",
	})
	require.NoError(t, err)

	catTea, err := q.CreateMenuCategory(ctx, sqlc.CreateMenuCategoryParams{
		Name: "Tea", NormalizedName: "tea",
	})
	require.NoError(t, err)

	catEmpty, err := q.CreateMenuCategory(ctx, sqlc.CreateMenuCategoryParams{
		Name: "Empty Category", NormalizedName: "empty category",
	})
	require.NoError(t, err)

	catUnsell, err := q.CreateMenuCategory(ctx, sqlc.CreateMenuCategoryParams{
		Name: "Z Unsellable", NormalizedName: "z unsellable",
	})
	require.NoError(t, err)

	// 2. Modifier Groups
	grpSugar, err := q.CreateModifierGroup(ctx, sqlc.CreateModifierGroupParams{
		Name: "Sugar Level", NormalizedName: "sugar level", MinSelections: 1, MaxSelections: 1,
	})
	require.NoError(t, err)

	grpExtra, err := q.CreateModifierGroup(ctx, sqlc.CreateModifierGroupParams{
		Name: "Add-ons", NormalizedName: "add-ons", MinSelections: 0, MaxSelections: 2,
	})
	require.NoError(t, err)

	grpIce, err := q.CreateModifierGroup(ctx, sqlc.CreateModifierGroupParams{
		Name: "Ice Level", NormalizedName: "ice level", MinSelections: 1, MaxSelections: 1,
	})
	require.NoError(t, err)

	grpUnavail, err := q.CreateModifierGroup(ctx, sqlc.CreateModifierGroupParams{
		Name: "Temp Unavailable Mod", NormalizedName: "temp unavailable mod", MinSelections: 1, MaxSelections: 1,
	})
	require.NoError(t, err)

	grpRetired, err := q.CreateModifierGroup(ctx, sqlc.CreateModifierGroupParams{
		Name: "Old Toppings", NormalizedName: "old toppings", MinSelections: 1, MaxSelections: 1,
	})
	require.NoError(t, err)
	now := time.Now()
	_, err = q.RetireModifierGroup(ctx, sqlc.RetireModifierGroupParams{
		ID: grpRetired.ID, RetiredAt: sql.NullTime{Time: now, Valid: true},
		RetirementReason: sql.NullString{String: "NO_LONGER_OFFERED", Valid: true},
		RetirementNote:   sql.NullString{},
	})
	require.NoError(t, err)

	// 3. Modifier Options
	optSugarNormal, err := q.CreateModifierOption(ctx, sqlc.CreateModifierOptionParams{
		ModifierGroupID: grpSugar.ID, Name: "100% Sugar", NormalizedName: "100% sugar", SurchargeVnd: 0, Available: true,
	})
	require.NoError(t, err)

	optSugarLess, err := q.CreateModifierOption(ctx, sqlc.CreateModifierOptionParams{
		ModifierGroupID: grpSugar.ID, Name: "50% Sugar", NormalizedName: "50% sugar", SurchargeVnd: 0, Available: true,
	})
	require.NoError(t, err)

	optSugarRetired, err := q.CreateModifierOption(ctx, sqlc.CreateModifierOptionParams{
		ModifierGroupID: grpSugar.ID, Name: "Extra Sweet", NormalizedName: "extra sweet", SurchargeVnd: 0, Available: true,
	})
	require.NoError(t, err)
	_, err = q.RetireModifierOption(ctx, sqlc.RetireModifierOptionParams{
		ID: optSugarRetired.ID, RetiredAt: sql.NullTime{Time: now, Valid: true},
		RetirementReason: sql.NullString{String: "MENU_RESTRUCTURE", Valid: true},
		RetirementNote:   sql.NullString{},
	})
	require.NoError(t, err)

	// Set Sugar defaults: 100% Sugar and Extra Sweet (retired)
	err = q.CreateModifierGroupDefaultOption(ctx, sqlc.CreateModifierGroupDefaultOptionParams{
		ModifierGroupID: grpSugar.ID, ModifierOptionID: optSugarNormal.ID,
	})
	require.NoError(t, err)
	err = q.CreateModifierGroupDefaultOption(ctx, sqlc.CreateModifierGroupDefaultOptionParams{
		ModifierGroupID: grpSugar.ID, ModifierOptionID: optSugarRetired.ID,
	})
	require.NoError(t, err)

	optExtraShot, err := q.CreateModifierOption(ctx, sqlc.CreateModifierOptionParams{
		ModifierGroupID: grpExtra.ID, Name: "Extra Shot", NormalizedName: "extra shot", SurchargeVnd: 10000, Available: true,
	})
	require.NoError(t, err)

	optSyrup, err := q.CreateModifierOption(ctx, sqlc.CreateModifierOptionParams{
		ModifierGroupID: grpExtra.ID, Name: "Caramel Syrup", NormalizedName: "caramel syrup", SurchargeVnd: 5000, Available: false,
	})
	require.NoError(t, err)

	optIceNormal, err := q.CreateModifierOption(ctx, sqlc.CreateModifierOptionParams{
		ModifierGroupID: grpIce.ID, Name: "Normal Ice", NormalizedName: "normal ice", SurchargeVnd: 0, Available: true,
	})
	require.NoError(t, err)

	optIceNone, err := q.CreateModifierOption(ctx, sqlc.CreateModifierOptionParams{
		ModifierGroupID: grpIce.ID, Name: "No Ice", NormalizedName: "no ice", SurchargeVnd: 0, Available: false,
	})
	require.NoError(t, err)

	optIceRetired, err := q.CreateModifierOption(ctx, sqlc.CreateModifierOptionParams{
		ModifierGroupID: grpIce.ID, Name: "Warm", NormalizedName: "warm", SurchargeVnd: 0, Available: true,
	})
	require.NoError(t, err)
	_, err = q.RetireModifierOption(ctx, sqlc.RetireModifierOptionParams{
		ID: optIceRetired.ID, RetiredAt: sql.NullTime{Time: now, Valid: true},
		RetirementReason: sql.NullString{String: "OTHER", Valid: true},
		RetirementNote:   sql.NullString{String: "Seasonal only", Valid: true},
	})
	require.NoError(t, err)

	optUnavail1, err := q.CreateModifierOption(ctx, sqlc.CreateModifierOptionParams{
		ModifierGroupID: grpUnavail.ID, Name: "Unavailable Choice", NormalizedName: "unavailable choice", SurchargeVnd: 2000, Available: false,
	})
	require.NoError(t, err)

	optRetiredGrp1, err := q.CreateModifierOption(ctx, sqlc.CreateModifierOptionParams{
		ModifierGroupID: grpRetired.ID, Name: "Boba", NormalizedName: "boba", SurchargeVnd: 5000, Available: true,
	})
	require.NoError(t, err)

	// Attach Category Sugar to CatCoffee
	err = q.CreateCategoryModifierGroup(ctx, sqlc.CreateCategoryModifierGroupParams{
		MenuCategoryID: catCoffee.ID, ModifierGroupID: grpSugar.ID,
	})
	require.NoError(t, err)

	// 4. Menu Items
	// Item 1: Espresso (direct priced, 30000, available)
	itemEspresso, err := q.CreateMenuItem(ctx, sqlc.CreateMenuItemParams{
		CategoryID: catCoffee.ID, Name: "Espresso", NormalizedName: "espresso",
		PriceVnd: sql.NullInt64{Int64: 30000, Valid: true}, Available: true,
	})
	require.NoError(t, err)
	// Attach Add-ons directly to Espresso
	err = q.CreateItemModifierGroup(ctx, sqlc.CreateItemModifierGroupParams{
		MenuItemID: itemEspresso.ID, ModifierGroupID: grpExtra.ID,
	})
	require.NoError(t, err)

	// Item 2: Americano (sized, available)
	itemAmericano, err := q.CreateMenuItem(ctx, sqlc.CreateMenuItemParams{
		CategoryID: catCoffee.ID, Name: "Americano", NormalizedName: "americano",
		PriceVnd: sql.NullInt64{}, Available: true,
	})
	require.NoError(t, err)
	// Sizes for Americano: Small (25000, avail), Large (35000, avail)
	_, err = q.CreateMenuItemSize(ctx, sqlc.CreateMenuItemSizeParams{
		MenuItemID: itemAmericano.ID, Name: "Small", NormalizedName: "small", PriceVnd: 25000, Available: true,
	})
	require.NoError(t, err)
	_, err = q.CreateMenuItemSize(ctx, sqlc.CreateMenuItemSizeParams{
		MenuItemID: itemAmericano.ID, Name: "Large", NormalizedName: "large", PriceVnd: 35000, Available: true,
	})
	require.NoError(t, err)

	// Item 3: Latte (sized, one size available, one unavailable, one retired)
	itemLatte, err := q.CreateMenuItem(ctx, sqlc.CreateMenuItemParams{
		CategoryID: catCoffee.ID, Name: "Latte", NormalizedName: "latte",
		PriceVnd: sql.NullInt64{}, Available: true,
	})
	require.NoError(t, err)
	_, err = q.CreateMenuItemSize(ctx, sqlc.CreateMenuItemSizeParams{
		MenuItemID: itemLatte.ID, Name: "Regular", NormalizedName: "regular", PriceVnd: 30000, Available: true,
	})
	require.NoError(t, err)
	_, err = q.CreateMenuItemSize(ctx, sqlc.CreateMenuItemSizeParams{
		MenuItemID: itemLatte.ID, Name: "Jumbo", NormalizedName: "jumbo", PriceVnd: 45000, Available: false,
	})
	require.NoError(t, err)
	sizeRetired, err := q.CreateMenuItemSize(ctx, sqlc.CreateMenuItemSizeParams{
		MenuItemID: itemLatte.ID, Name: "Mini", NormalizedName: "mini", PriceVnd: 20000, Available: true,
	})
	require.NoError(t, err)
	_, err = q.RetireMenuItemSize(ctx, sqlc.RetireMenuItemSizeParams{
		ID: sizeRetired.ID, RetiredAt: sql.NullTime{Time: now, Valid: true},
		RetirementReason: sql.NullString{String: "NO_LONGER_OFFERED", Valid: true},
		RetirementNote:   sql.NullString{},
	})
	require.NoError(t, err)

	// Item 4: All Sizes Unavailable (sized item in CatUnsell)
	itemAllUnavail, err := q.CreateMenuItem(ctx, sqlc.CreateMenuItemParams{
		CategoryID: catUnsell.ID, Name: "Cappuccino Out of Stock", NormalizedName: "cappuccino out of stock",
		PriceVnd: sql.NullInt64{}, Available: true,
	})
	require.NoError(t, err)
	_, err = q.CreateMenuItemSize(ctx, sqlc.CreateMenuItemSizeParams{
		MenuItemID: itemAllUnavail.ID, Name: "Standard", NormalizedName: "standard", PriceVnd: 35000, Available: false,
	})
	require.NoError(t, err)

	// Item 5: Direct Item Unavailable (in CatUnsell)
	itemDirectUnavail, err := q.CreateMenuItem(ctx, sqlc.CreateMenuItemParams{
		CategoryID: catUnsell.ID, Name: "Seasonal Brew", NormalizedName: "seasonal brew",
		PriceVnd: sql.NullInt64{Int64: 40000, Valid: true}, Available: false,
	})
	require.NoError(t, err)

	// Item 6: Retired Item (in CatCoffee)
	itemRetired, err := q.CreateMenuItem(ctx, sqlc.CreateMenuItemParams{
		CategoryID: catCoffee.ID, Name: "Old Mocha", NormalizedName: "old mocha",
		PriceVnd: sql.NullInt64{Int64: 38000, Valid: true}, Available: true,
	})
	require.NoError(t, err)
	_, err = q.RetireMenuItem(ctx, sqlc.RetireMenuItemParams{
		ID: itemRetired.ID, RetiredAt: sql.NullTime{Time: now, Valid: true},
		RetirementReason: sql.NullString{String: "NO_LONGER_OFFERED", Valid: true},
		RetirementNote:   sql.NullString{},
	})
	require.NoError(t, err)

	// Item 7: Item with Required Modifier Group Available (in CatTea)
	itemReqAvail, err := q.CreateMenuItem(ctx, sqlc.CreateMenuItemParams{
		CategoryID: catTea.ID, Name: "Iced Peach Tea", NormalizedName: "iced peach tea",
		PriceVnd: sql.NullInt64{Int64: 32000, Valid: true}, Available: true,
	})
	require.NoError(t, err)
	err = q.CreateItemModifierGroup(ctx, sqlc.CreateItemModifierGroupParams{
		MenuItemID: itemReqAvail.ID, ModifierGroupID: grpIce.ID,
	})
	require.NoError(t, err)

	// Item 8: Item with Required Modifier Group Unavailable (in CatUnsell)
	itemReqUnavail, err := q.CreateMenuItem(ctx, sqlc.CreateMenuItemParams{
		CategoryID: catUnsell.ID, Name: "Special Herbal Tea", NormalizedName: "special herbal tea",
		PriceVnd: sql.NullInt64{Int64: 35000, Valid: true}, Available: true,
	})
	require.NoError(t, err)
	err = q.CreateItemModifierGroup(ctx, sqlc.CreateItemModifierGroupParams{
		MenuItemID: itemReqUnavail.ID, ModifierGroupID: grpUnavail.ID,
	})
	require.NoError(t, err)

	// Item 9: Item with Category Group Excluded (in CatCoffee)
	itemExcluded, err := q.CreateMenuItem(ctx, sqlc.CreateMenuItemParams{
		CategoryID: catCoffee.ID, Name: "Black Coffee", NormalizedName: "black coffee",
		PriceVnd: sql.NullInt64{Int64: 28000, Valid: true}, Available: true,
	})
	require.NoError(t, err)
	// Exclude inherited Sugar Level
	err = q.CreateItemModifierGroupExclusion(ctx, sqlc.CreateItemModifierGroupExclusionParams{
		MenuItemID: itemExcluded.ID, ModifierGroupID: grpSugar.ID,
	})
	require.NoError(t, err)

	// Item 10: Item with Direct and Inherited group deduplicated (in CatCoffee)
	itemDedup, err := q.CreateMenuItem(ctx, sqlc.CreateMenuItemParams{
		CategoryID: catCoffee.ID, Name: "Sweet Coffee", NormalizedName: "sweet coffee",
		PriceVnd: sql.NullInt64{Int64: 30000, Valid: true}, Available: true,
	})
	require.NoError(t, err)
	// Attach Sugar directly to item (which is already inherited from CatCoffee)
	err = q.CreateItemModifierGroup(ctx, sqlc.CreateItemModifierGroupParams{
		MenuItemID: itemDedup.ID, ModifierGroupID: grpSugar.ID,
	})
	require.NoError(t, err)

	// Item 11: Item with Retired Required Group (in CatTea)
	itemRetiredReqGrp, err := q.CreateMenuItem(ctx, sqlc.CreateMenuItemParams{
		CategoryID: catTea.ID, Name: "Classic Green Tea", NormalizedName: "classic green tea",
		PriceVnd: sql.NullInt64{Int64: 25000, Valid: true}, Available: true,
	})
	require.NoError(t, err)
	err = q.CreateItemModifierGroup(ctx, sqlc.CreateItemModifierGroupParams{
		MenuItemID: itemRetiredReqGrp.ID, ModifierGroupID: grpRetired.ID,
	})
	require.NoError(t, err)

	return seededCatalog{
		CatCoffee: catCoffee.ID,
		CatTea:    catTea.ID,
		CatEmpty:  catEmpty.ID,
		CatUnsell: catUnsell.ID,

		ItemEspressoDirect:     itemEspresso.ID,
		ItemAmericanoSized:     itemAmericano.ID,
		ItemLattePartAvailSize: itemLatte.ID,
		ItemAllSizesUnavail:    itemAllUnavail.ID,
		ItemDirectUnavail:      itemDirectUnavail.ID,
		ItemRetired:            itemRetired.ID,
		ItemRequiredModAvail:   itemReqAvail.ID,
		ItemRequiredModUnavail: itemReqUnavail.ID,
		ItemExcludedGroup:      itemExcluded.ID,
		ItemDirectAndInherited: itemDedup.ID,
		ItemRetiredReqGroup:    itemRetiredReqGrp.ID,

		GroupCategorySugar:  grpSugar.ID,
		GroupExtraEspresso:  grpExtra.ID,
		GroupRequiredIce:    grpIce.ID,
		GroupUnavailOptions: grpUnavail.ID,
		GroupRetired:        grpRetired.ID,

		OptSugarNormal:  optSugarNormal.ID,
		OptSugarLess:    optSugarLess.ID,
		OptSugarRetired: optSugarRetired.ID,
		OptExtraShot:    optExtraShot.ID,
		OptSyrup:        optSyrup.ID,
		OptIceNormal:    optIceNormal.ID,
		OptIceNone:      optIceNone.ID,
		OptIceRetired:   optIceRetired.ID,
		OptUnavail1:     optUnavail1.ID,
		OptRetiredGrp1:  optRetiredGrp1.ID,
	}
}

func TestSellableMenu(t *testing.T) {
	db, q := openExecutorTestDB(t)
	cleanCategoryTestTables(t, db)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()

	seed := seedTestCatalog(t, db, q)

	manager := createTestIdentity(t, db, q, []string{auth.RoleManager}, true)
	actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}

	handler := catalog.NewSellableMenuHandler(runner)

	t.Run("Capability_Check_Forbidden", func(t *testing.T) {
		barista := createTestIdentity(t, db, q, []string{auth.RoleBarista}, true)
		baristaActor := catalog.Actor{StaffID: barista.StaffID, SessionID: barista.SessionID}
		_, err := handler.Handle(ctx, baristaActor)
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrForbidden), "Barista lacks catalog.view_prices")
	})

	t.Run("Hierarchy_Sellability_And_Omissions", func(t *testing.T) {
		res, err := handler.Handle(ctx, actor)
		require.NoError(t, err)

		// 1. Categories check:
		// CatEmpty (no items) must be omitted.
		// CatUnsell (only unsellable items) must be omitted.
		// Only Coffee and Tea should be present!
		catNames := make([]string, len(res.Categories))
		for i, c := range res.Categories {
			catNames[i] = c.Name
		}
		assert.Equal(t, []string{"Coffee", "Tea"}, catNames, "Categories must be ordered by normalized name and empty/unsellable omitted")

		// 2. Items in Coffee category:
		// Americano, Black Coffee, Espresso, Latte, Sweet Coffee (Old Mocha is retired -> omitted)
		coffeeCat := res.Categories[0]
		assert.Equal(t, seed.CatCoffee, coffeeCat.ID)
		itemNames := make([]string, len(coffeeCat.Items))
		for i, item := range coffeeCat.Items {
			itemNames[i] = item.Name
		}
		assert.Equal(t, []string{"Americano", "Black Coffee", "Espresso", "Latte", "Sweet Coffee"}, itemNames)

		// 3. Direct pricing vs Sized pricing
		// Espresso: direct price 30000, sizes empty
		var espresso *catalog.SellableItemResponse
		var americano *catalog.SellableItemResponse
		var latte *catalog.SellableItemResponse
		var blackCoffee *catalog.SellableItemResponse
		var sweetCoffee *catalog.SellableItemResponse
		for i := range coffeeCat.Items {
			switch coffeeCat.Items[i].Name {
			case "Espresso":
				espresso = &coffeeCat.Items[i]
			case "Americano":
				americano = &coffeeCat.Items[i]
			case "Latte":
				latte = &coffeeCat.Items[i]
			case "Black Coffee":
				blackCoffee = &coffeeCat.Items[i]
			case "Sweet Coffee":
				sweetCoffee = &coffeeCat.Items[i]
			}
		}

		require.NotNil(t, espresso)
		require.NotNil(t, espresso.PriceVND)
		assert.Equal(t, int64(30000), *espresso.PriceVND)
		assert.Empty(t, espresso.Sizes)

		require.NotNil(t, americano)
		assert.Nil(t, americano.PriceVND)
		require.Len(t, americano.Sizes, 2)
		assert.Equal(t, "Large", americano.Sizes[0].Name) // L < S in normalized name
		assert.Equal(t, int64(35000), americano.Sizes[0].PriceVND)
		assert.Equal(t, "Small", americano.Sizes[1].Name)
		assert.Equal(t, int64(25000), americano.Sizes[1].PriceVND)

		// Latte: one size available (Regular), one unavailable (Jumbo), one retired (Mini)
		// Only Regular should be in sellable sizes!
		require.NotNil(t, latte)
		assert.Nil(t, latte.PriceVND)
		require.Len(t, latte.Sizes, 1)
		assert.Equal(t, "Regular", latte.Sizes[0].Name)
		assert.Equal(t, int64(30000), latte.Sizes[0].PriceVND)

		// 4. Inherited vs Direct vs Deduplicated vs Excluded Modifier Groups
		// Espresso has Sugar Level (inherited from Coffee) and Add-ons (direct)
		require.Len(t, espresso.ModifierGroups, 2)
		assert.Equal(t, "Add-ons", espresso.ModifierGroups[0].Name) // A < S
		assert.Equal(t, "Sugar Level", espresso.ModifierGroups[1].Name)

		// Add-ons options: Extra Shot (avail, 10000), Caramel Syrup (unavailable -> omitted)
		require.Len(t, espresso.ModifierGroups[0].Options, 1)
		assert.Equal(t, "Extra Shot", espresso.ModifierGroups[0].Options[0].Name)
		assert.Equal(t, int64(10000), espresso.ModifierGroups[0].Options[0].SurchargeVND)

		// Sugar Level options: 100% Sugar (avail, 0), 50% Sugar (avail, 0), Extra Sweet (retired -> omitted)
		require.Len(t, espresso.ModifierGroups[1].Options, 2)
		assert.Equal(t, "100% Sugar", espresso.ModifierGroups[1].Options[0].Name)
		assert.Equal(t, "50% Sugar", espresso.ModifierGroups[1].Options[1].Name)

		// DefaultOptionIDs: Sugar Level had defaults: 100% Sugar and Extra Sweet (retired).
		// Only 100% Sugar is valid and available -> only 100% Sugar in DefaultOptionIDs!
		assert.Equal(t, []uuid.UUID{seed.OptSugarNormal}, espresso.ModifierGroups[1].DefaultOptionIDs)

		// Black Coffee: excluded Sugar Level -> ModifierGroups should be empty!
		require.NotNil(t, blackCoffee)
		assert.Empty(t, blackCoffee.ModifierGroups)

		// Sweet Coffee: direct assignment of Sugar Level + inherited Sugar Level -> deduplicated to 1 group!
		require.NotNil(t, sweetCoffee)
		require.Len(t, sweetCoffee.ModifierGroups, 1)
		assert.Equal(t, "Sugar Level", sweetCoffee.ModifierGroups[0].Name)

		// 5. Items in Tea category:
		// Classic Green Tea (had retired required group -> group omitted, item is sellable)
		// Iced Peach Tea (required group Ice Level with Normal Ice available -> sellable)
		teaCat := res.Categories[1]
		assert.Equal(t, seed.CatTea, teaCat.ID)
		teaItemNames := make([]string, len(teaCat.Items))
		for i, item := range teaCat.Items {
			teaItemNames[i] = item.Name
		}
		assert.Equal(t, []string{"Classic Green Tea", "Iced Peach Tea"}, teaItemNames)
	})
}

func TestManagementMenu(t *testing.T) {
	db, q := openExecutorTestDB(t)
	cleanCategoryTestTables(t, db)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()

	seed := seedTestCatalog(t, db, q)

	manager := createTestIdentity(t, db, q, []string{auth.RoleManager}, true)
	actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}

	handler := catalog.NewManagementMenuHandler(runner)

	t.Run("Capability_Check_Forbidden", func(t *testing.T) {
		barista := createTestIdentity(t, db, q, []string{auth.RoleBarista}, true)
		baristaActor := catalog.Actor{StaffID: barista.StaffID, SessionID: barista.SessionID}
		_, err := handler.Handle(ctx, baristaActor)
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrForbidden))
	})

	t.Run("All_Categories_Items_Retirement_And_Assignments", func(t *testing.T) {
		res, err := handler.Handle(ctx, actor)
		require.NoError(t, err)

		// 1. All 4 categories must be present, including Empty Category!
		catNames := make([]string, len(res.Categories))
		for i, c := range res.Categories {
			catNames[i] = c.Name
		}
		assert.Equal(t, []string{"Coffee", "Empty Category", "Tea", "Z Unsellable"}, catNames)

		// 2. Coffee category check
		coffeeCat := res.Categories[0]
		assert.Equal(t, []uuid.UUID{seed.GroupCategorySugar}, coffeeCat.ModifierGroupIDs)

		// Verify retired item is present in Coffee category
		var retiredItem *catalog.ManagementItemResponse
		for i := range coffeeCat.Items {
			if coffeeCat.Items[i].Name == "Old Mocha" {
				retiredItem = &coffeeCat.Items[i]
				break
			}
		}
		require.NotNil(t, retiredItem)
		assert.True(t, retiredItem.Retired)
		assert.NotNil(t, retiredItem.RetiredAt)
		require.NotNil(t, retiredItem.RetirementReason)
		assert.Equal(t, "NO_LONGER_OFFERED", *retiredItem.RetirementReason)

		// Verify Latte sizes include retired size
		var latte *catalog.ManagementItemResponse
		for i := range coffeeCat.Items {
			if coffeeCat.Items[i].Name == "Latte" {
				latte = &coffeeCat.Items[i]
				break
			}
		}
		require.NotNil(t, latte)
		require.Len(t, latte.Sizes, 3) // Jumbo, Mini, Regular (sorted by normalized name)
		var miniSize *catalog.ManagementSizeResponse
		for i := range latte.Sizes {
			if latte.Sizes[i].Name == "Mini" {
				miniSize = &latte.Sizes[i]
				break
			}
		}
		require.NotNil(t, miniSize)
		assert.True(t, miniSize.Retired)
		assert.NotNil(t, miniSize.RetiredAt)

		// Verify exclusions in Black Coffee
		var blackCoffee *catalog.ManagementItemResponse
		for i := range coffeeCat.Items {
			if coffeeCat.Items[i].Name == "Black Coffee" {
				blackCoffee = &coffeeCat.Items[i]
				break
			}
		}
		require.NotNil(t, blackCoffee)
		assert.Equal(t, []uuid.UUID{seed.GroupCategorySugar}, blackCoffee.ExcludedModifierGroupIDs)

		// Verify Espresso direct modifier groups
		var espresso *catalog.ManagementItemResponse
		for i := range coffeeCat.Items {
			if coffeeCat.Items[i].Name == "Espresso" {
				espresso = &coffeeCat.Items[i]
				break
			}
		}
		require.NotNil(t, espresso)
		assert.Equal(t, []uuid.UUID{seed.GroupExtraEspresso}, espresso.DirectModifierGroupIDs)
		// Effective modifier groups should include both Add-ons and Sugar Level
		require.Len(t, espresso.ModifierGroups, 2)

		// Verify Sugar Level defaults in Espresso are populated and sorted
		var sugarGrp *catalog.ManagementModifierGroupResponse
		for i := range espresso.ModifierGroups {
			if espresso.ModifierGroups[i].Name == "Sugar Level" {
				sugarGrp = &espresso.ModifierGroups[i]
				break
			}
		}
		require.NotNil(t, sugarGrp)
		require.Len(t, sugarGrp.DefaultOptionIDs, 2)
		expectedDefaults := []uuid.UUID{seed.OptSugarNormal, seed.OptSugarRetired}
		if expectedDefaults[0].String() > expectedDefaults[1].String() {
			expectedDefaults[0], expectedDefaults[1] = expectedDefaults[1], expectedDefaults[0]
		}
		assert.Equal(t, expectedDefaults, sugarGrp.DefaultOptionIDs, "DefaultOptionIDs must be deterministically sorted by UUID string")
	})
}

func TestAvailabilityMenu(t *testing.T) {
	db, q := openExecutorTestDB(t)
	cleanCategoryTestTables(t, db)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()

	seedTestCatalog(t, db, q)

	// Barista HAS catalog.manage_availability
	barista := createTestIdentity(t, db, q, []string{auth.RoleBarista}, true)
	actor := catalog.Actor{StaffID: barista.StaffID, SessionID: barista.SessionID}

	handler := catalog.NewAvailabilityMenuHandler(runner)

	t.Run("Authorized_And_Omit_Retired_Keep_Empty", func(t *testing.T) {
		res, err := handler.Handle(ctx, actor)
		require.NoError(t, err)

		// 1. All categories except those that only had retired items (if any).
		// Here Empty Category should be present!
		catNames := make([]string, len(res.Categories))
		for i, c := range res.Categories {
			catNames[i] = c.Name
		}
		assert.Contains(t, catNames, "Empty Category")
		assert.Equal(t, []string{"Coffee", "Empty Category", "Tea", "Z Unsellable"}, catNames)

		// 2. Coffee category:
		// Retired item "Old Mocha" must be omitted!
		coffeeCat := res.Categories[0]
		for _, item := range coffeeCat.Items {
			assert.NotEqual(t, "Old Mocha", item.Name, "retired item must be omitted from availability menu")
		}

		// Latte: retired size "Mini" must be omitted, but unavailable size "Jumbo" must be present!
		var latte *catalog.AvailabilityItemResponse
		for i := range coffeeCat.Items {
			if coffeeCat.Items[i].Name == "Latte" {
				latte = &coffeeCat.Items[i]
				break
			}
		}
		require.NotNil(t, latte)
		require.Len(t, latte.Sizes, 2, "only Jumbo (unavail) and Regular (avail) should be present; Mini (retired) omitted")
		assert.Equal(t, "Jumbo", latte.Sizes[0].Name)
		assert.False(t, latte.Sizes[0].Available)
		assert.Equal(t, "Regular", latte.Sizes[1].Name)
		assert.True(t, latte.Sizes[1].Available)

		// Sugar Level options: Extra Sweet is retired -> omitted!
		require.NotEmpty(t, latte.ModifierGroups)
		sugarGrp := latte.ModifierGroups[0]
		require.Len(t, sugarGrp.Options, 2)
		assert.Equal(t, "100% Sugar", sugarGrp.Options[0].Name)
		assert.Equal(t, "50% Sugar", sugarGrp.Options[1].Name)
	})
}

func TestModifierGroups(t *testing.T) {
	db, q := openExecutorTestDB(t)
	cleanCategoryTestTables(t, db)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()

	seed := seedTestCatalog(t, db, q)

	manager := createTestIdentity(t, db, q, []string{auth.RoleManager}, true)
	actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}

	handler := catalog.NewModifierGroupsHandler(runner)

	t.Run("Capability_Check_Forbidden", func(t *testing.T) {
		barista := createTestIdentity(t, db, q, []string{auth.RoleBarista}, true)
		baristaActor := catalog.Actor{StaffID: barista.StaffID, SessionID: barista.SessionID}
		_, err := handler.Handle(ctx, baristaActor)
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrForbidden))
	})

	t.Run("Full_View_Including_Retired", func(t *testing.T) {
		groups, err := handler.Handle(ctx, actor)
		require.NoError(t, err)

		// All 5 groups must be returned (including retired Old Toppings)
		groupNames := make([]string, len(groups))
		for i, g := range groups {
			groupNames[i] = g.Name
		}
		assert.Equal(t, []string{"Add-ons", "Ice Level", "Old Toppings", "Sugar Level", "Temp Unavailable Mod"}, groupNames)

		// Check retired group Old Toppings
		var retiredGrp *catalog.ModifierGroupManagementResponse
		for i := range groups {
			if groups[i].Name == "Old Toppings" {
				retiredGrp = &groups[i]
				break
			}
		}
		require.NotNil(t, retiredGrp)
		assert.True(t, retiredGrp.Retired)
		assert.NotNil(t, retiredGrp.RetiredAt)
		require.Len(t, retiredGrp.Options, 1)
		assert.Equal(t, "Boba", retiredGrp.Options[0].Name)

		// Check Sugar Level defaults and retired options
		var sugarGrp *catalog.ModifierGroupManagementResponse
		for i := range groups {
			if groups[i].Name == "Sugar Level" {
				sugarGrp = &groups[i]
				break
			}
		}
		require.NotNil(t, sugarGrp)
		assert.Len(t, sugarGrp.Options, 3) // 100% Sugar, 50% Sugar, Extra Sweet (retired)
		assert.Contains(t, sugarGrp.DefaultOptionIDs, seed.OptSugarNormal)
		assert.Contains(t, sugarGrp.DefaultOptionIDs, seed.OptSugarRetired)
	})
}

func TestProjectionSecurity_FieldSeparation(t *testing.T) {
	db, q := openExecutorTestDB(t)
	cleanCategoryTestTables(t, db)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()

	seedTestCatalog(t, db, q)

	manager := createTestIdentity(t, db, q, []string{auth.RoleManager}, true)
	actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}

	availHandler := catalog.NewAvailabilityMenuHandler(runner)
	sellHandler := catalog.NewSellableMenuHandler(runner)
	mgmtHandler := catalog.NewManagementMenuHandler(runner)

	availRes, err := availHandler.Handle(ctx, actor)
	require.NoError(t, err)

	sellRes, err := sellHandler.Handle(ctx, actor)
	require.NoError(t, err)

	mgmtRes, err := mgmtHandler.Handle(ctx, actor)
	require.NoError(t, err)

	// Step 2 Requirement:
	// Marshal each DTO and assert the availability JSON contains none of:
	// price_vnd, surcharge_vnd, retired, default_option_ids, or excluded_modifier_group_ids.
	availJSON, err := json.Marshal(availRes)
	require.NoError(t, err)
	availStr := string(availJSON)

	forbiddenFields := []string{
		"\"price_vnd\"",
		"\"surcharge_vnd\"",
		"\"retired\"",
		"\"default_option_ids\"",
		"\"excluded_modifier_group_ids\"",
	}

	for _, field := range forbiddenFields {
		assert.False(t, strings.Contains(availStr, field), "availability JSON must not contain %s", field)
	}

	// Conversely, sellable JSON contains price_vnd and surcharge_vnd, but no retired
	sellJSON, err := json.Marshal(sellRes)
	require.NoError(t, err)
	sellStr := string(sellJSON)
	assert.Contains(t, sellStr, "\"price_vnd\"")
	assert.Contains(t, sellStr, "\"surcharge_vnd\"")
	assert.False(t, strings.Contains(sellStr, "\"retired\""))

	// Management JSON contains all fields
	mgmtJSON, err := json.Marshal(mgmtRes)
	require.NoError(t, err)
	mgmtStr := string(mgmtJSON)
	assert.Contains(t, mgmtStr, "\"price_vnd\"")
	assert.Contains(t, mgmtStr, "\"surcharge_vnd\"")
	assert.Contains(t, mgmtStr, "\"retired\"")
	assert.Contains(t, mgmtStr, "\"default_option_ids\"")
	assert.Contains(t, mgmtStr, "\"excluded_modifier_group_ids\"")
}
