//go:build integration

package sales_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/catalog"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resolutionFixture is one Menu Item and the three association sets that
// decide its effective Modifier Groups.
type resolutionFixture struct {
	MenuItemID uuid.UUID
	Inherited  []uuid.UUID
	Excluded   []uuid.UUID
	Direct     []uuid.UUID
}

// TestSalesResolutionMatchesCatalog is ADR-012's safety net. Sales expresses
// (inherited - exclusions) + direct in SQL twice — once per Menu Item for the
// draft path and once batched for Commit — and Catalog expresses it in Go; if
// any of the three ever disagree, this fails in CI rather than in a cafe.
func TestSalesResolutionMatchesCatalog(t *testing.T) {
	db, q := openSalesTestDB(t)
	ctx := context.Background()

	cases := []struct {
		name  string
		build func(t *testing.T, db *sql.DB, q *sqlc.Queries) resolutionFixture
	}{
		{"no groups at all", buildNoGroups},
		{"inherited only", buildInheritedOnly},
		{"direct only", buildDirectOnly},
		{"inherited and excluded", buildInheritedAndExcluded},
		{"excluded and directly attached", buildExcludedAndDirect},
		{"retired group still resolves as a member", buildRetiredGroup},
		{"inherited and directly attached (dedupes)", buildInheritedAndDirectSameGroup},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			truncateSalesTables(t, db)
			fx := tc.build(t, db, q)

			fromCatalog := catalog.EffectiveGroupIDs(fx.Inherited, fx.Excluded, fx.Direct)

			perItem, err := q.ListEffectiveModifierGroupIDs(ctx, fx.MenuItemID)
			require.NoError(t, err)

			batchRows, err := q.ListEffectiveModifierGroupsForCommit(ctx,
				[]uuid.UUID{fx.MenuItemID})
			require.NoError(t, err)
			batched := make([]uuid.UUID, 0, len(batchRows))
			for _, row := range batchRows {
				batched = append(batched, row.ModifierGroupID)
			}

			// ElementsMatch treats nil and empty as equal, so
			// EffectiveGroupIDs' nil-for-empty convention needs no normalizing
			// here.
			require.ElementsMatch(t, fromCatalog, perItem,
				"the single-item query must match catalog.EffectiveGroupIDs")
			require.ElementsMatch(t, fromCatalog, batched,
				"the batched commit query must match catalog.EffectiveGroupIDs")
		})
	}
}

// TestBatchedResolutionDoesNotLeakGroupsAcrossItems pins the batched commit
// query to per-Item scope: Coffee's inherited Topping group must resolve under
// Coffee alone, Tea (no groups at all) must stay empty, and no row may name a
// Menu Item outside the requested batch.
func TestBatchedResolutionDoesNotLeakGroupsAcrossItems(t *testing.T) {
	env := newSalesEnv(t)
	ctx := context.Background()

	rows, err := env.Queries.ListEffectiveModifierGroupsForCommit(ctx,
		[]uuid.UUID{env.CoffeeID, env.TeaID})
	require.NoError(t, err)

	requested := []uuid.UUID{env.CoffeeID, env.TeaID}
	byItem := make(map[uuid.UUID][]uuid.UUID)
	for _, row := range rows {
		require.Contains(t, requested, row.MenuItemID,
			"the batched query must only resolve the requested Menu Items")
		byItem[row.MenuItemID] = append(byItem[row.MenuItemID], row.ModifierGroupID)
	}

	coffeeOnly, err := env.Queries.ListEffectiveModifierGroupIDs(ctx, env.CoffeeID)
	require.NoError(t, err)
	teaOnly, err := env.Queries.ListEffectiveModifierGroupIDs(ctx, env.TeaID)
	require.NoError(t, err)

	require.ElementsMatch(t, coffeeOnly, byItem[env.CoffeeID],
		"the batched set for Coffee must match its single-item set")
	require.ElementsMatch(t, teaOnly, byItem[env.TeaID],
		"the batched set for Tea must match its single-item set")
}

// A Group that is both inherited and directly attached appears once.
func TestEffectiveGroupsDeduplicate(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	ctx := context.Background()

	fx := buildInheritedAndDirectSameGroup(t, db, q)

	got, err := q.ListEffectiveModifierGroupIDs(ctx, fx.MenuItemID)
	require.NoError(t, err)
	assert.Len(t, got, 1)
}

// Defaults exclude unavailable options, retired options, and every option of a
// retired Group.
func TestDefaultOptionsFilterUnselectable(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	ctx := context.Background()

	fx, wantOptionID := buildDefaultsFixture(t, db, q)

	groups, err := q.ListEffectiveModifierGroupIDs(ctx, fx.MenuItemID)
	require.NoError(t, err)

	got, err := q.ListDefaultModifierOptionIDs(ctx, groups)
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{wantOptionID}, got)
}

// -- Fixtures --
//
// The builders create their entities through the shared sqlc writers and their
// association rows through raw SQL: the association tables have no writer in
// this slice (task brief, note for the implementer).

// seedResolutionItem creates a Menu Category and one available Menu Item in
// it, returning both ids.
func seedResolutionItem(t *testing.T, q *sqlc.Queries, categoryName, itemName string) (uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	cat, err := q.CreateMenuCategory(ctx, sqlc.CreateMenuCategoryParams{
		Name:           categoryName,
		NormalizedName: categoryName,
	})
	require.NoError(t, err)
	item, err := q.CreateMenuItem(ctx, sqlc.CreateMenuItemParams{
		CategoryID:     cat.ID,
		Name:           itemName,
		NormalizedName: itemName,
		PriceVnd:       sql.NullInt64{Int64: 25000, Valid: true},
		Available:      true,
	})
	require.NoError(t, err)
	return cat.ID, item.ID
}

// seedResolutionGroup creates an active Modifier Group.
func seedResolutionGroup(t *testing.T, q *sqlc.Queries, name string) uuid.UUID {
	t.Helper()
	group, err := q.CreateModifierGroup(context.Background(), sqlc.CreateModifierGroupParams{
		Name: name, NormalizedName: name, MinSelections: 0, MaxSelections: 5,
	})
	require.NoError(t, err)
	return group.ID
}

// seedResolutionOption creates a Modifier Option with the given availability.
func seedResolutionOption(t *testing.T, q *sqlc.Queries, groupID uuid.UUID, name string, available bool) uuid.UUID {
	t.Helper()
	option, err := q.CreateModifierOption(context.Background(), sqlc.CreateModifierOptionParams{
		ModifierGroupID: groupID, Name: name, NormalizedName: name,
		SurchargeVnd: 0, Available: available,
	})
	require.NoError(t, err)
	return option.ID
}

// retireFixtureGroup marks a Modifier Group retired the way the Catalog
// command would.
func retireFixtureGroup(t *testing.T, q *sqlc.Queries, id uuid.UUID) {
	t.Helper()
	_, err := q.RetireModifierGroup(context.Background(), sqlc.RetireModifierGroupParams{
		ID:               id,
		RetiredAt:        sql.NullTime{Time: time.Now(), Valid: true},
		RetirementReason: sql.NullString{String: "OTHER", Valid: true},
		RetirementNote:   sql.NullString{String: "fixture", Valid: true},
	})
	require.NoError(t, err)
}

// retireFixtureOption marks a Modifier Option retired the way the Catalog
// command would.
func retireFixtureOption(t *testing.T, q *sqlc.Queries, id uuid.UUID) {
	t.Helper()
	_, err := q.RetireModifierOption(context.Background(), sqlc.RetireModifierOptionParams{
		ID:               id,
		RetiredAt:        sql.NullTime{Time: time.Now(), Valid: true},
		RetirementReason: sql.NullString{String: "OTHER", Valid: true},
		RetirementNote:   sql.NullString{String: "fixture", Valid: true},
	})
	require.NoError(t, err)
}

// attachInheritedGroup links a Group to a Category (category_modifier_groups).
func attachInheritedGroup(t *testing.T, db *sql.DB, categoryID, groupID uuid.UUID) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO category_modifier_groups
		(menu_category_id, modifier_group_id) VALUES ($1, $2)`, categoryID, groupID)
	require.NoError(t, err)
}

// attachDirectGroup links a Group to a Menu Item (item_modifier_groups).
func attachDirectGroup(t *testing.T, db *sql.DB, itemID, groupID uuid.UUID) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO item_modifier_groups
		(menu_item_id, modifier_group_id) VALUES ($1, $2)`, itemID, groupID)
	require.NoError(t, err)
}

// excludeInheritedGroup records an Item's exclusion of a Category Group
// (item_modifier_group_exclusions).
func excludeInheritedGroup(t *testing.T, db *sql.DB, itemID, groupID uuid.UUID) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO item_modifier_group_exclusions
		(menu_item_id, modifier_group_id) VALUES ($1, $2)`, itemID, groupID)
	require.NoError(t, err)
}

// declareGroupDefault marks an Option as a Group's declared default
// (modifier_group_default_options).
func declareGroupDefault(t *testing.T, db *sql.DB, groupID, optionID uuid.UUID) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO modifier_group_default_options
		(modifier_group_id, modifier_option_id) VALUES ($1, $2)`, groupID, optionID)
	require.NoError(t, err)
}

// buildNoGroups is a Menu Item whose Category has no default Groups and that
// attaches nothing itself.
func buildNoGroups(t *testing.T, db *sql.DB, q *sqlc.Queries) resolutionFixture {
	_, itemID := seedResolutionItem(t, q, "resolve no groups", "Nước chanh")
	return resolutionFixture{MenuItemID: itemID}
}

// buildInheritedOnly has one Group through the Category alone.
func buildInheritedOnly(t *testing.T, db *sql.DB, q *sqlc.Queries) resolutionFixture {
	catID, itemID := seedResolutionItem(t, q, "resolve inherited only", "Trà đá")
	groupID := seedResolutionGroup(t, q, "Trà inherited only")
	attachInheritedGroup(t, db, catID, groupID)
	return resolutionFixture{MenuItemID: itemID, Inherited: []uuid.UUID{groupID}}
}

// buildDirectOnly attaches one Group to the Item itself.
func buildDirectOnly(t *testing.T, db *sql.DB, q *sqlc.Queries) resolutionFixture {
	_, itemID := seedResolutionItem(t, q, "resolve direct only", "Bạc xỉu")
	groupID := seedResolutionGroup(t, q, "Đường direct only")
	attachDirectGroup(t, db, itemID, groupID)
	return resolutionFixture{MenuItemID: itemID, Direct: []uuid.UUID{groupID}}
}

// buildInheritedAndExcluded: the Category provides two Groups and the Item
// excludes one of them, leaving the other effective.
func buildInheritedAndExcluded(t *testing.T, db *sql.DB, q *sqlc.Queries) resolutionFixture {
	catID, itemID := seedResolutionItem(t, q, "resolve excluded", "Cà phê trứng")
	kept := seedResolutionGroup(t, q, "Trứng kept")
	dropped := seedResolutionGroup(t, q, "Trứng dropped")
	attachInheritedGroup(t, db, catID, kept)
	attachInheritedGroup(t, db, catID, dropped)
	excludeInheritedGroup(t, db, itemID, dropped)
	return resolutionFixture{
		MenuItemID: itemID,
		Inherited:  []uuid.UUID{kept, dropped},
		Excluded:   []uuid.UUID{dropped},
	}
}

// buildExcludedAndDirect: the Item both excludes a Category Group and attaches
// it directly — direct attachment survives the exclusion.
func buildExcludedAndDirect(t *testing.T, db *sql.DB, q *sqlc.Queries) resolutionFixture {
	catID, itemID := seedResolutionItem(t, q, "resolve ex plus direct", "Sữa cỏ bò")
	groupID := seedResolutionGroup(t, q, "Cỏ bò both")
	attachInheritedGroup(t, db, catID, groupID)
	excludeInheritedGroup(t, db, itemID, groupID)
	attachDirectGroup(t, db, itemID, groupID)
	return resolutionFixture{
		MenuItemID: itemID,
		Inherited:  []uuid.UUID{groupID},
		Excluded:   []uuid.UUID{groupID},
		Direct:     []uuid.UUID{groupID},
	}
}

// buildRetiredGroup: a retired Group still resolves as a member. Resolution is
// structural; selectability is validateModifierOptions' job.
func buildRetiredGroup(t *testing.T, db *sql.DB, q *sqlc.Queries) resolutionFixture {
	_, itemID := seedResolutionItem(t, q, "resolve retired group", "Chè bắp")
	groupID := seedResolutionGroup(t, q, "Bắp retired")
	retireFixtureGroup(t, q, groupID)
	attachDirectGroup(t, db, itemID, groupID)
	return resolutionFixture{MenuItemID: itemID, Direct: []uuid.UUID{groupID}}
}

// buildInheritedAndDirectSameGroup attaches one Group both ways; the UNION in
// the query must deduplicate it.
func buildInheritedAndDirectSameGroup(t *testing.T, db *sql.DB, q *sqlc.Queries) resolutionFixture {
	catID, itemID := seedResolutionItem(t, q, "resolve dedupe", "Cà phê đen")
	groupID := seedResolutionGroup(t, q, "Đen dedupe")
	attachInheritedGroup(t, db, catID, groupID)
	attachDirectGroup(t, db, itemID, groupID)
	return resolutionFixture{
		MenuItemID: itemID,
		Inherited:  []uuid.UUID{groupID},
		Direct:     []uuid.UUID{groupID},
	}
}

// buildDefaultsFixture declares four default options on one effective Group: a
// selectable one (returned as the want id), an unavailable one, a retired one,
// and an available one that belongs to a second, retired Group.
func buildDefaultsFixture(t *testing.T, db *sql.DB, q *sqlc.Queries) (resolutionFixture, uuid.UUID) {
	t.Helper()
	_, itemID := seedResolutionItem(t, q, "resolve defaults", "Cà phê sữa đá")
	groupID := seedResolutionGroup(t, q, "Sữa đá defaults")

	wantOptionID := seedResolutionOption(t, q, groupID, "Sữa đặc", true)
	unavailable := seedResolutionOption(t, q, groupID, "Sữa tươi", false)
	retiredOption := seedResolutionOption(t, q, groupID, "Kem trái", true)
	retireFixtureOption(t, q, retiredOption)
	retiredGroup := seedResolutionGroup(t, q, "Sữa đá retired group")
	stray := seedResolutionOption(t, q, retiredGroup, "Trân châu", true)
	retireFixtureGroup(t, q, retiredGroup)

	for _, id := range []uuid.UUID{wantOptionID, unavailable, retiredOption, stray} {
		declareGroupDefault(t, db, groupID, id)
	}
	attachDirectGroup(t, db, itemID, groupID)
	return resolutionFixture{MenuItemID: itemID, Direct: []uuid.UUID{groupID}}, wantOptionID
}
