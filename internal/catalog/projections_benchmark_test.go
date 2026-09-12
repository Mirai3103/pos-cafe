//go:build integration

package catalog_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/catalog"
	"github.com/Mirai3103/pos-cafe/internal/database"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func seedBenchmarkCatalog(b *testing.B, db *sql.DB) (catalog.Actor, *catalog.Runner) {
	b.Helper()
	ctx := context.Background()

	cleanCategoryTestTablesBenchmark(b, db)

	tx, err := db.BeginTx(ctx, nil)
	require.NoError(b, err)
	defer tx.Rollback() //nolint:errcheck

	q := sqlc.New(tx)

	// Create manager identity
	loginCode := testLoginCode("bench")
	staff, err := q.CreateStaffIdentity(ctx, sqlc.CreateStaffIdentityParams{
		DisplayName: "Benchmark Manager",
		Btrim:       loginCode,
		PinHash:     "",
		Enabled:     true,
	})
	require.NoError(b, err)

	err = q.AddStaffRole(ctx, sqlc.AddStaffRoleParams{
		StaffIdentityID: staff.ID,
		Role:            auth.RoleManager,
	})
	require.NoError(b, err)

	sessionID := uuid.New()
	tokenHash := fmt.Sprintf("tok_%s", sessionID.String()[:8])
	_, err = q.CreateStaffSession(ctx, sqlc.CreateStaffSessionParams{
		TokenHash:           tokenHash,
		StaffIdentityID:     staff.ID,
		State:               auth.SessionStateActive,
		ActiveWorkspace:     sql.NullString{String: auth.WorkspaceManager, Valid: true},
		LastAuthenticatedAt: time.Now(),
		LastHumanActivityAt: time.Now(),
		ExpiresAt:           time.Now().Add(12 * time.Hour),
	})
	require.NoError(b, err)

	var actualSessionID uuid.UUID
	err = tx.QueryRowContext(ctx, `SELECT id FROM staff_access_sessions WHERE token_hash = $1`, tokenHash).Scan(&actualSessionID)
	require.NoError(b, err)

	actor := catalog.Actor{StaffID: staff.ID, SessionID: actualSessionID}

	// 1. Seed 20 Categories
	const numCategories = 20
	categoryIDs := make([]uuid.UUID, numCategories)
	for i := 0; i < numCategories; i++ {
		cat, err := q.CreateMenuCategory(ctx, sqlc.CreateMenuCategoryParams{
			Name:           fmt.Sprintf("Category %02d", i+1),
			NormalizedName: fmt.Sprintf("category %02d", i+1),
		})
		require.NoError(b, err)
		categoryIDs[i] = cat.ID
	}

	// 2. Seed 20 Modifier Groups, each with 10 Options (total 20 groups, 200 options)
	const numGroups = 20
	const optionsPerGroup = 10
	groupIDs := make([]uuid.UUID, numGroups)
	for g := 0; g < numGroups; g++ {
		minSel := int32(0)
		if g%3 == 0 {
			minSel = 1
		}
		grp, err := q.CreateModifierGroup(ctx, sqlc.CreateModifierGroupParams{
			Name:           fmt.Sprintf("Modifier Group %02d", g+1),
			NormalizedName: fmt.Sprintf("modifier group %02d", g+1),
			MinSelections:  minSel,
			MaxSelections:  3,
		})
		require.NoError(b, err)
		groupIDs[g] = grp.ID

		var firstOptID uuid.UUID
		for o := 0; o < optionsPerGroup; o++ {
			opt, err := q.CreateModifierOption(ctx, sqlc.CreateModifierOptionParams{
				ModifierGroupID: grp.ID,
				Name:            fmt.Sprintf("Option %02d-%02d", g+1, o+1),
				NormalizedName:  fmt.Sprintf("option %02d-%02d", g+1, o+1),
				SurchargeVnd:    int64(o * 5000),
				Available:       true,
			})
			require.NoError(b, err)
			if o == 0 {
				firstOptID = opt.ID
			}
		}

		// Set a default option for groups with minSelections > 0
		if minSel > 0 && firstOptID != uuid.Nil {
			err = q.CreateModifierGroupDefaultOption(ctx, sqlc.CreateModifierGroupDefaultOptionParams{
				ModifierGroupID:  grp.ID,
				ModifierOptionID: firstOptID,
			})
			require.NoError(b, err)
		}
	}

	// Attach category modifier groups (group 0 and group 1 to all categories)
	for _, catID := range categoryIDs {
		err = q.CreateCategoryModifierGroup(ctx, sqlc.CreateCategoryModifierGroupParams{
			MenuCategoryID:  catID,
			ModifierGroupID: groupIDs[0],
		})
		require.NoError(b, err)
		err = q.CreateCategoryModifierGroup(ctx, sqlc.CreateCategoryModifierGroupParams{
			MenuCategoryID:  catID,
			ModifierGroupID: groupIDs[1],
		})
		require.NoError(b, err)
	}

	// 3. Seed 200 Items (10 items per category)
	// 5 items sized with 4 sizes each -> 100 sized items * 4 sizes = 400 sizes!
	// 5 items direct priced -> 100 direct items
	// Total items = 200, total sizes = 400.
	const itemsPerCategory = 10
	for cIdx, catID := range categoryIDs {
		for i := 0; i < itemsPerCategory; i++ {
			itemName := fmt.Sprintf("Item C%02d-%02d", cIdx+1, i+1)
			normName := fmt.Sprintf("item c%02d-%02d", cIdx+1, i+1)

			if i < 5 {
				// Sized item
				item, err := q.CreateMenuItem(ctx, sqlc.CreateMenuItemParams{
					CategoryID:     catID,
					Name:           itemName,
					NormalizedName: normName,
					PriceVnd:       sql.NullInt64{},
					Available:      true,
				})
				require.NoError(b, err)

				sizeNames := []string{"Small", "Medium", "Large", "Extra Large"}
				for sIdx, sName := range sizeNames {
					_, err = q.CreateMenuItemSize(ctx, sqlc.CreateMenuItemSizeParams{
						MenuItemID:     item.ID,
						Name:           sName,
						NormalizedName: fmt.Sprintf("size %s", sName),
						PriceVnd:       int64(25000 + sIdx*10000),
						Available:      true,
					})
					require.NoError(b, err)
				}

				// Direct modifier group attachment
				err = q.CreateItemModifierGroup(ctx, sqlc.CreateItemModifierGroupParams{
					MenuItemID:      item.ID,
					ModifierGroupID: groupIDs[2],
				})
				require.NoError(b, err)
			} else {
				// Direct priced item
				item, err := q.CreateMenuItem(ctx, sqlc.CreateMenuItemParams{
					CategoryID:     catID,
					Name:           itemName,
					NormalizedName: normName,
					PriceVnd:       sql.NullInt64{Int64: int64(30000 + i*5000), Valid: true},
					Available:      true,
				})
				require.NoError(b, err)

				// For some items, exclude inherited group
				if i == 6 {
					err = q.CreateItemModifierGroupExclusion(ctx, sqlc.CreateItemModifierGroupExclusionParams{
						MenuItemID:      item.ID,
						ModifierGroupID: groupIDs[1],
					})
					require.NoError(b, err)
				}
			}
		}
	}

	err = tx.Commit()
	require.NoError(b, err)

	return actor, catalog.NewRunner(db, sqlc.New(db))
}

func cleanCategoryTestTablesBenchmark(b *testing.B, db *sql.DB) {
	b.Helper()
	_, err := db.ExecContext(context.Background(), `
		TRUNCATE TABLE 
			menu_item_sizes,
			menu_items,
			menu_categories, 
			modifier_groups,
			catalog_mutation_requests, 
			audit_events 
		CASCADE;
	`)
	require.NoError(b, err)
}

func BenchmarkCatalogProjections(b *testing.B) {
	url := os.Getenv("TEST_DATABASE_URL")
	require.Contains(b, url, "_test")
	db, err := database.Open(context.Background(), url)
	require.NoError(b, err)
	defer db.Close()

	actor, runner := seedBenchmarkCatalog(b, db)
	ctx := context.Background()

	sellableHandler := catalog.NewSellableMenuHandler(runner)
	managementHandler := catalog.NewManagementMenuHandler(runner)
	availabilityHandler := catalog.NewAvailabilityMenuHandler(runner)
	modifierGroupsHandler := catalog.NewModifierGroupsHandler(runner)

	b.Run("SellableMenu", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			res, err := sellableHandler.Handle(ctx, actor)
			if err != nil {
				b.Fatalf("SellableMenu: %v", err)
			}
			if len(res.Categories) != 20 {
				b.Fatalf("expected 20 categories, got %d", len(res.Categories))
			}
		}
	})

	b.Run("ManagementMenu", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			res, err := managementHandler.Handle(ctx, actor)
			if err != nil {
				b.Fatalf("ManagementMenu: %v", err)
			}
			if len(res.Categories) != 20 {
				b.Fatalf("expected 20 categories, got %d", len(res.Categories))
			}
		}
	})

	b.Run("AvailabilityMenu", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			res, err := availabilityHandler.Handle(ctx, actor)
			if err != nil {
				b.Fatalf("AvailabilityMenu: %v", err)
			}
			if len(res.Categories) != 20 {
				b.Fatalf("expected 20 categories, got %d", len(res.Categories))
			}
		}
	})

	b.Run("ModifierGroups", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			groups, err := modifierGroupsHandler.Handle(ctx, actor)
			if err != nil {
				b.Fatalf("ModifierGroups: %v", err)
			}
			if len(groups) != 20 {
				b.Fatalf("expected 20 groups, got %d", len(groups))
			}
		}
	})
}
