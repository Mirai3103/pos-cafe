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

func cleanCategoryTestTables(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `
		TRUNCATE TABLE 
			menu_item_sizes,
			menu_items,
			menu_categories, 
			catalog_mutation_requests, 
			audit_events 
		CASCADE;
	`)
	require.NoError(t, err)
}

type catalogTestIdentity struct {
	StaffID   uuid.UUID
	SessionID uuid.UUID
	PIN       string
}

func createCatalogTestIdentity(t *testing.T, db *sql.DB, q *sqlc.Queries, roles []string, enabled bool, pin string) catalogTestIdentity {
	t.Helper()
	ident := createTestIdentity(t, db, q, roles, enabled)
	if pin != "" {
		pinHash, err := auth.HashPin(pin)
		require.NoError(t, err)
		err = q.UpdateStaffPin(context.Background(), sqlc.UpdateStaffPinParams{
			ID:      ident.StaffID,
			PinHash: pinHash,
		})
		require.NoError(t, err)
	}
	return catalogTestIdentity{
		StaffID:   ident.StaffID,
		SessionID: ident.SessionID,
		PIN:       pin,
	}
}

func TestCreateCategory(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()

	t.Run("Success_Trimming_InternalSpace_UUID_Audit", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createTestIdentity(t, db, q, []string{auth.RoleManager}, true)
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewCreateCategoryHandler(runner)

		reqID := uuid.New()
		inputName := "   Special   Milk   Tea   "
		expectedDisplayName := "Special   Milk   Tea"
		expectedNormalizedKey := "special   milk   tea"

		cmd := catalog.CreateCategoryCommand{
			RequestID: reqID,
			Name:      inputName,
		}

		status, res, err := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		assert.Equal(t, 201, status)
		assert.NotEqual(t, uuid.Nil, res.ID)
		assert.Equal(t, expectedDisplayName, res.Name)

		// Verify database row
		var dbName, dbNormalized string
		err = db.QueryRowContext(ctx, `SELECT name, normalized_name FROM menu_categories WHERE id = $1`, res.ID).Scan(&dbName, &dbNormalized)
		require.NoError(t, err)
		assert.Equal(t, expectedDisplayName, dbName, "surrounding whitespace must be trimmed and internal whitespace preserved")
		assert.Equal(t, expectedNormalizedKey, dbNormalized, "normalized_name must be lowercase")

		// Verify Audit Event
		var eventType string
		var actorID, sessionID uuid.UUID
		var detailsJSON []byte
		err = db.QueryRowContext(ctx, `
			SELECT event_type, actor_id, session_id, details
			FROM audit_events
			WHERE event_type = 'catalog.category.created' AND actor_id = $1
		`, actor.StaffID).Scan(&eventType, &actorID, &sessionID, &detailsJSON)
		require.NoError(t, err)
		assert.Equal(t, "catalog.category.created", eventType)
		assert.Equal(t, actor.StaffID, actorID)
		assert.Equal(t, actor.SessionID, sessionID)

		var details map[string]any
		err = json.Unmarshal(detailsJSON, &details)
		require.NoError(t, err)
		assert.Equal(t, res.ID.String(), details["category_id"])
		assert.Equal(t, expectedDisplayName, details["name"])
	})

	t.Run("CaseInsensitiveConflict", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createTestIdentity(t, db, q, []string{auth.RoleManager}, true)
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewCreateCategoryHandler(runner)

		// First creation
		status1, res1, err1 := handler.Handle(ctx, actor, catalog.CreateCategoryCommand{
			RequestID: uuid.New(),
			Name:      "Beverages",
		})
		require.NoError(t, err1)
		assert.Equal(t, 201, status1)
		assert.NotEmpty(t, res1.ID)

		// Conflicting creation with different casing and whitespace
		status2, _, err2 := handler.Handle(ctx, actor, catalog.CreateCategoryCommand{
			RequestID: uuid.New(),
			Name:      "   bEvErAgEs   ",
		})
		require.Error(t, err2)
		assert.True(t, errors.Is(err2, catalog.ErrNameConflict), "expected ErrNameConflict, got: %v", err2)
		assert.Equal(t, 0, status2)

		// Verify only 1 category exists in database
		var count int
		err := db.QueryRowContext(ctx, `SELECT count(*) FROM menu_categories`).Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 1, count)
	})

	t.Run("ExactReplay", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createTestIdentity(t, db, q, []string{auth.RoleManager}, true)
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewCreateCategoryHandler(runner)

		reqID := uuid.New()
		cmd := catalog.CreateCategoryCommand{
			RequestID: reqID,
			Name:      "Bakery",
		}

		// First execution
		status1, res1, err1 := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err1)
		assert.Equal(t, 201, status1)

		// Replay with identical request
		status2, res2, err2 := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err2)
		assert.Equal(t, 201, status2)
		assert.Equal(t, res1, res2)

		// Verify database count does not increase
		var catCount, auditCount int
		err := db.QueryRowContext(ctx, `SELECT count(*) FROM menu_categories`).Scan(&catCount)
		require.NoError(t, err)
		assert.Equal(t, 1, catCount)

		err = db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE event_type = 'catalog.category.created'`).Scan(&auditCount)
		require.NoError(t, err)
		assert.Equal(t, 1, auditCount, "replay must not duplicate audit event")
	})

	t.Run("ForbiddenWithoutAdministerStructure", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		// Cashier has no catalog.administer_structure capability
		cashier := createTestIdentity(t, db, q, []string{auth.RoleCashier}, true)
		actor := catalog.Actor{StaffID: cashier.StaffID, SessionID: cashier.SessionID}
		handler := catalog.NewCreateCategoryHandler(runner)

		_, _, err := handler.Handle(ctx, actor, catalog.CreateCategoryCommand{
			RequestID: uuid.New(),
			Name:      "Restricted Category",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrForbidden), "expected ErrForbidden, got: %v", err)

		// Verify denial audit event was recorded
		assert.Equal(t, 1, countAuthorizationDenials(t, db, "catalog.category.create"))
	})
}

func TestRenameCategory(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()

	t.Run("Success_Trimming_InternalSpace_Audit", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createTestIdentity(t, db, q, []string{auth.RoleManager}, true)
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		createHandler := catalog.NewCreateCategoryHandler(runner)
		renameHandler := catalog.NewRenameCategoryHandler(runner)

		// Create category first
		_, created, err := createHandler.Handle(ctx, actor, catalog.CreateCategoryCommand{
			RequestID: uuid.New(),
			Name:      "Cold Drinks",
		})
		require.NoError(t, err)

		// Rename with whitespace and internal space preservation
		renameReqID := uuid.New()
		inputName := "   Cold   &   Iced   Drinks   "
		expectedDisplayName := "Cold   &   Iced   Drinks"
		expectedNormalizedKey := "cold   &   iced   drinks"

		renameCmd := catalog.RenameCategoryCommand{
			RequestID:  renameReqID,
			CategoryID: created.ID,
			Name:       inputName,
		}

		status, renamed, err := renameHandler.Handle(ctx, actor, renameCmd)
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.Equal(t, created.ID, renamed.ID)
		assert.Equal(t, expectedDisplayName, renamed.Name)

		// Verify database row
		var dbName, dbNormalized string
		err = db.QueryRowContext(ctx, `SELECT name, normalized_name FROM menu_categories WHERE id = $1`, created.ID).Scan(&dbName, &dbNormalized)
		require.NoError(t, err)
		assert.Equal(t, expectedDisplayName, dbName)
		assert.Equal(t, expectedNormalizedKey, dbNormalized)

		// Verify Audit Event
		var eventType string
		var actorID, sessionID uuid.UUID
		var detailsJSON []byte
		err = db.QueryRowContext(ctx, `
			SELECT event_type, actor_id, session_id, details
			FROM audit_events
			WHERE event_type = 'catalog.category.renamed' AND actor_id = $1
		`, actor.StaffID).Scan(&eventType, &actorID, &sessionID, &detailsJSON)
		require.NoError(t, err)
		assert.Equal(t, "catalog.category.renamed", eventType)
		assert.Equal(t, actor.StaffID, actorID)
		assert.Equal(t, actor.SessionID, sessionID)

		var details map[string]any
		err = json.Unmarshal(detailsJSON, &details)
		require.NoError(t, err)
		assert.Equal(t, created.ID.String(), details["category_id"])
		assert.Equal(t, "Cold Drinks", details["old_name"])
		assert.Equal(t, expectedDisplayName, details["new_name"])
	})

	t.Run("CaseInsensitiveConflict", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createTestIdentity(t, db, q, []string{auth.RoleManager}, true)
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		createHandler := catalog.NewCreateCategoryHandler(runner)
		renameHandler := catalog.NewRenameCategoryHandler(runner)

		// Create category A and category B
		_, catA, err := createHandler.Handle(ctx, actor, catalog.CreateCategoryCommand{
			RequestID: uuid.New(),
			Name:      "Hot Drinks",
		})
		require.NoError(t, err)
		assert.NotEmpty(t, catA.ID)

		_, catB, err := createHandler.Handle(ctx, actor, catalog.CreateCategoryCommand{
			RequestID: uuid.New(),
			Name:      "Cold Drinks",
		})
		require.NoError(t, err)

		// Rename category B to match category A case-insensitively
		status, _, err := renameHandler.Handle(ctx, actor, catalog.RenameCategoryCommand{
			RequestID:  uuid.New(),
			CategoryID: catB.ID,
			Name:       "   hOt dRiNkS   ",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrNameConflict), "expected ErrNameConflict, got: %v", err)
		assert.Equal(t, 0, status)

		// Verify category B's name in DB is unchanged
		var bName string
		err = db.QueryRowContext(ctx, `SELECT name FROM menu_categories WHERE id = $1`, catB.ID).Scan(&bName)
		require.NoError(t, err)
		assert.Equal(t, "Cold Drinks", bName)
	})

	t.Run("ExactReplay", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createTestIdentity(t, db, q, []string{auth.RoleManager}, true)
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		createHandler := catalog.NewCreateCategoryHandler(runner)
		renameHandler := catalog.NewRenameCategoryHandler(runner)

		_, created, err := createHandler.Handle(ctx, actor, catalog.CreateCategoryCommand{
			RequestID: uuid.New(),
			Name:      "Dessert",
		})
		require.NoError(t, err)

		renameCmd := catalog.RenameCategoryCommand{
			RequestID:  uuid.New(),
			CategoryID: created.ID,
			Name:       "Special Desserts",
		}

		// First execution
		status1, res1, err1 := renameHandler.Handle(ctx, actor, renameCmd)
		require.NoError(t, err1)
		assert.Equal(t, 200, status1)

		// Replay
		status2, res2, err2 := renameHandler.Handle(ctx, actor, renameCmd)
		require.NoError(t, err2)
		assert.Equal(t, 200, status2)
		assert.Equal(t, res1, res2)

		// Verify audit event count
		var auditCount int
		err = db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE event_type = 'catalog.category.renamed'`).Scan(&auditCount)
		require.NoError(t, err)
		assert.Equal(t, 1, auditCount, "replay must not duplicate audit event")
	})

	t.Run("MissingCategory", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createTestIdentity(t, db, q, []string{auth.RoleManager}, true)
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		renameHandler := catalog.NewRenameCategoryHandler(runner)

		nonExistentID := uuid.New()
		status, _, err := renameHandler.Handle(ctx, actor, catalog.RenameCategoryCommand{
			RequestID:  uuid.New(),
			CategoryID: nonExistentID,
			Name:       "Ghost Category",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrNotFound), "expected ErrNotFound, got: %v", err)
		assert.Equal(t, 0, status)
	})

	t.Run("ForbiddenWithoutAdministerStructure", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createTestIdentity(t, db, q, []string{auth.RoleManager}, true)
		createHandler := catalog.NewCreateCategoryHandler(runner)
		_, created, err := createHandler.Handle(ctx, catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}, catalog.CreateCategoryCommand{
			RequestID: uuid.New(),
			Name:      "Management Only",
		})
		require.NoError(t, err)

		cashier := createTestIdentity(t, db, q, []string{auth.RoleCashier}, true)
		renameHandler := catalog.NewRenameCategoryHandler(runner)

		_, _, err = renameHandler.Handle(ctx, catalog.Actor{StaffID: cashier.StaffID, SessionID: cashier.SessionID}, catalog.RenameCategoryCommand{
			RequestID:  uuid.New(),
			CategoryID: created.ID,
			Name:       "Hacked Category",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrForbidden), "expected ErrForbidden, got: %v", err)

		// Verify denial audit event was recorded
		assert.Equal(t, 1, countAuthorizationDenials(t, db, "catalog.category.rename"))
	})
}

func TestCreateItem(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()

	t.Run("Success_DirectPrice_Audit_Normalization", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		catHandler := catalog.NewCreateCategoryHandler(runner)
		itemHandler := catalog.NewCreateItemHandler(runner)

		_, cat, err := catHandler.Handle(ctx, actor, catalog.CreateCategoryCommand{
			RequestID: uuid.New(),
			Name:      "Beverages",
		})
		require.NoError(t, err)

		reqID := uuid.New()
		price := int64(45000)
		inputName := "   Espresso   Blend   "
		expectedDisplayName := "Espresso   Blend"
		expectedNormalizedKey := "espresso   blend"

		cmd := catalog.CreateItemCommand{
			RequestID:  reqID,
			CategoryID: cat.ID,
			Name:       inputName,
			PriceVND:   &price,
			ManagerPIN: manager.PIN,
		}

		status, res, err := itemHandler.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		assert.Equal(t, 201, status)
		assert.NotEqual(t, uuid.Nil, res.ID)
		assert.Equal(t, cat.ID, res.CategoryID)
		assert.Equal(t, expectedDisplayName, res.Name)
		require.NotNil(t, res.PriceVND)
		assert.Equal(t, price, *res.PriceVND)
		assert.True(t, res.Available)
		assert.Empty(t, res.Sizes)

		// Verify database row in menu_items
		var dbName, dbNorm string
		var dbPrice sql.NullInt64
		var dbAvail bool
		err = db.QueryRowContext(ctx, `
			SELECT name, normalized_name, price_vnd, available
			FROM menu_items
			WHERE id = $1
		`, res.ID).Scan(&dbName, &dbNorm, &dbPrice, &dbAvail)
		require.NoError(t, err)
		assert.Equal(t, expectedDisplayName, dbName)
		assert.Equal(t, expectedNormalizedKey, dbNorm)
		require.True(t, dbPrice.Valid)
		assert.Equal(t, price, dbPrice.Int64)
		assert.True(t, dbAvail)

		// Verify audit event
		var eventType string
		var actorID, sessionID uuid.UUID
		var detailsJSON []byte
		err = db.QueryRowContext(ctx, `
			SELECT event_type, actor_id, session_id, details
			FROM audit_events
			WHERE event_type = 'catalog.item.created' AND actor_id = $1
		`, actor.StaffID).Scan(&eventType, &actorID, &sessionID, &detailsJSON)
		require.NoError(t, err)
		assert.Equal(t, "catalog.item.created", eventType)
		assert.Equal(t, actor.StaffID, actorID)
		assert.Equal(t, actor.SessionID, sessionID)

		var details map[string]any
		err = json.Unmarshal(detailsJSON, &details)
		require.NoError(t, err)
		assert.Equal(t, res.ID.String(), details["item_id"])
		assert.Equal(t, cat.ID.String(), details["category_id"])
		assert.Equal(t, expectedDisplayName, details["name"])
		assert.Equal(t, float64(price), details["price_vnd"])
		assert.False(t, strings.Contains(string(detailsJSON), manager.PIN), "audit details must never record manager PIN")
	})

	t.Run("Success_SizedItem_MultipleAbsoluteSizes_Audit", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		catHandler := catalog.NewCreateCategoryHandler(runner)
		itemHandler := catalog.NewCreateItemHandler(runner)

		_, cat, err := catHandler.Handle(ctx, actor, catalog.CreateCategoryCommand{
			RequestID: uuid.New(),
			Name:      "Tea Selection",
		})
		require.NoError(t, err)

		reqID := uuid.New()
		cmd := catalog.CreateItemCommand{
			RequestID:  reqID,
			CategoryID: cat.ID,
			Name:       "   Matcha   Latte   ",
			PriceVND:   nil,
			Sizes: []catalog.CreateSizeInput{
				{Name: "   Small   ", PriceVND: 35000},
				{Name: "Medium", PriceVND: 45000},
				{Name: "Large", PriceVND: 55000},
			},
			ManagerPIN: manager.PIN,
		}

		status, res, err := itemHandler.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		assert.Equal(t, 201, status)
		assert.NotEqual(t, uuid.Nil, res.ID)
		assert.Equal(t, cat.ID, res.CategoryID)
		assert.Equal(t, "Matcha   Latte", res.Name)
		assert.Nil(t, res.PriceVND)
		assert.True(t, res.Available)
		require.Len(t, res.Sizes, 3)

		expectedSizes := []struct {
			name  string
			price int64
		}{
			{name: "Small", price: 35000},
			{name: "Medium", price: 45000},
			{name: "Large", price: 55000},
		}
		for i, exp := range expectedSizes {
			assert.NotEqual(t, uuid.Nil, res.Sizes[i].ID)
			assert.Equal(t, exp.name, res.Sizes[i].Name)
			assert.Equal(t, exp.price, res.Sizes[i].PriceVND)
			assert.True(t, res.Sizes[i].Available)
		}

		// Verify database row in menu_items (price_vnd must be null)
		var dbPrice sql.NullInt64
		err = db.QueryRowContext(ctx, `SELECT price_vnd FROM menu_items WHERE id = $1`, res.ID).Scan(&dbPrice)
		require.NoError(t, err)
		assert.False(t, dbPrice.Valid, "sized item must have NULL price_vnd in menu_items")

		// Verify database rows in menu_item_sizes
		rows, err := db.QueryContext(ctx, `
			SELECT name, normalized_name, price_vnd, available
			FROM menu_item_sizes
			WHERE menu_item_id = $1
			ORDER BY price_vnd ASC
		`, res.ID)
		require.NoError(t, err)
		defer rows.Close()

		var count int
		for rows.Next() {
			var sName, sNorm string
			var sPrice int64
			var sAvail bool
			err := rows.Scan(&sName, &sNorm, &sPrice, &sAvail)
			require.NoError(t, err)
			assert.Equal(t, expectedSizes[count].name, sName)
			assert.Equal(t, strings.ToLower(expectedSizes[count].name), sNorm)
			assert.Equal(t, expectedSizes[count].price, sPrice)
			assert.True(t, sAvail)
			count++
		}
		require.NoError(t, rows.Err())
		assert.Equal(t, 3, count)

		// Verify audit event
		var eventType string
		var detailsJSON []byte
		err = db.QueryRowContext(ctx, `
			SELECT event_type, details
			FROM audit_events
			WHERE event_type = 'catalog.item.created' AND actor_id = $1
		`, actor.StaffID).Scan(&eventType, &detailsJSON)
		require.NoError(t, err)
		assert.Equal(t, "catalog.item.created", eventType)

		var details map[string]any
		err = json.Unmarshal(detailsJSON, &details)
		require.NoError(t, err)
		assert.Equal(t, res.ID.String(), details["item_id"])
		assert.Nil(t, details["price_vnd"])
		sizesRaw, ok := details["sizes"].([]any)
		require.True(t, ok)
		assert.Len(t, sizesRaw, 3)
		assert.False(t, strings.Contains(string(detailsJSON), manager.PIN), "audit details must never record manager PIN")
	})

	t.Run("InvalidPricingConfiguration_NeitherNorBoth", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		catHandler := catalog.NewCreateCategoryHandler(runner)
		itemHandler := catalog.NewCreateItemHandler(runner)

		_, cat, err := catHandler.Handle(ctx, actor, catalog.CreateCategoryCommand{
			RequestID: uuid.New(),
			Name:      "Beverages",
		})
		require.NoError(t, err)

		price := int64(30000)

		// Neither direct price nor sizes
		status1, _, err1 := itemHandler.Handle(ctx, actor, catalog.CreateItemCommand{
			RequestID:  uuid.New(),
			CategoryID: cat.ID,
			Name:       "No Price Item",
			PriceVND:   nil,
			Sizes:      nil,
			ManagerPIN: manager.PIN,
		})
		require.Error(t, err1)
		assert.True(t, errors.Is(err1, catalog.ErrInvalidPricingConfiguration), "expected ErrInvalidPricingConfiguration for neither, got: %v", err1)
		assert.Equal(t, 0, status1)

		// Empty sizes slice
		status2, _, err2 := itemHandler.Handle(ctx, actor, catalog.CreateItemCommand{
			RequestID:  uuid.New(),
			CategoryID: cat.ID,
			Name:       "Empty Sizes Item",
			PriceVND:   nil,
			Sizes:      []catalog.CreateSizeInput{},
			ManagerPIN: manager.PIN,
		})
		require.Error(t, err2)
		assert.True(t, errors.Is(err2, catalog.ErrInvalidPricingConfiguration), "expected ErrInvalidPricingConfiguration for empty sizes, got: %v", err2)
		assert.Equal(t, 0, status2)

		// Both direct price and sizes
		status3, _, err3 := itemHandler.Handle(ctx, actor, catalog.CreateItemCommand{
			RequestID:  uuid.New(),
			CategoryID: cat.ID,
			Name:       "Both Pricing Item",
			PriceVND:   &price,
			Sizes: []catalog.CreateSizeInput{
				{Name: "Standard", PriceVND: 30000},
			},
			ManagerPIN: manager.PIN,
		})
		require.Error(t, err3)
		assert.True(t, errors.Is(err3, catalog.ErrInvalidPricingConfiguration), "expected ErrInvalidPricingConfiguration for both, got: %v", err3)
		assert.Equal(t, 0, status3)

		// Confirm nothing was inserted into database
		var itemCount int
		err = db.QueryRowContext(ctx, `SELECT count(*) FROM menu_items`).Scan(&itemCount)
		require.NoError(t, err)
		assert.Equal(t, 0, itemCount)
	})

	t.Run("PriceBounds", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		catHandler := catalog.NewCreateCategoryHandler(runner)
		itemHandler := catalog.NewCreateItemHandler(runner)

		_, cat, err := catHandler.Handle(ctx, actor, catalog.CreateCategoryCommand{
			RequestID: uuid.New(),
			Name:      "Beverages",
		})
		require.NoError(t, err)

		testCases := []struct {
			name     string
			priceVND *int64
			sizes    []catalog.CreateSizeInput
		}{
			{
				name:     "Direct price zero",
				priceVND: func() *int64 { v := int64(0); return &v }(),
			},
			{
				name:     "Direct price negative",
				priceVND: func() *int64 { v := int64(-1000); return &v }(),
			},
			{
				name:     "Direct price above max",
				priceVND: func() *int64 { v := int64(2_147_483_648); return &v }(),
			},
			{
				name:  "Size price zero",
				sizes: []catalog.CreateSizeInput{{Name: "Small", PriceVND: 0}},
			},
			{
				name:  "Size price negative",
				sizes: []catalog.CreateSizeInput{{Name: "Small", PriceVND: -500}},
			},
			{
				name:  "Size price above max",
				sizes: []catalog.CreateSizeInput{{Name: "Small", PriceVND: 2_147_483_648}},
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				status, _, err := itemHandler.Handle(ctx, actor, catalog.CreateItemCommand{
					RequestID:  uuid.New(),
					CategoryID: cat.ID,
					Name:       "Test Item",
					PriceVND:   tc.priceVND,
					Sizes:      tc.sizes,
					ManagerPIN: manager.PIN,
				})
				require.Error(t, err)
				assert.True(t, errors.Is(err, catalog.ErrInvalidPricingConfiguration),
					"expected ErrInvalidPricingConfiguration for %s, got: %v", tc.name, err)
				assert.Equal(t, 0, status)
			})
		}
	})

	t.Run("DuplicateNormalizedSizeNames", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		catHandler := catalog.NewCreateCategoryHandler(runner)
		itemHandler := catalog.NewCreateItemHandler(runner)

		_, cat, err := catHandler.Handle(ctx, actor, catalog.CreateCategoryCommand{
			RequestID: uuid.New(),
			Name:      "Beverages",
		})
		require.NoError(t, err)

		status, _, err := itemHandler.Handle(ctx, actor, catalog.CreateItemCommand{
			RequestID:  uuid.New(),
			CategoryID: cat.ID,
			Name:       "Smoothie",
			Sizes: []catalog.CreateSizeInput{
				{Name: "Regular", PriceVND: 30000},
				{Name: "   rEgUlAr   ", PriceVND: 35000},
			},
			ManagerPIN: manager.PIN,
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrNameConflict), "expected ErrNameConflict for duplicate normalized size names, got: %v", err)
		assert.Equal(t, 0, status)

		// Verify 0 items and 0 sizes in database
		var count int
		err = db.QueryRowContext(ctx, `SELECT count(*) FROM menu_items`).Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 0, count)
	})

	t.Run("ItemNameConflictWithinCategory", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		catHandler := catalog.NewCreateCategoryHandler(runner)
		itemHandler := catalog.NewCreateItemHandler(runner)

		_, catA, err := catHandler.Handle(ctx, actor, catalog.CreateCategoryCommand{
			RequestID: uuid.New(),
			Name:      "Coffee",
		})
		require.NoError(t, err)

		_, catB, err := catHandler.Handle(ctx, actor, catalog.CreateCategoryCommand{
			RequestID: uuid.New(),
			Name:      "Specials",
		})
		require.NoError(t, err)

		price := int64(40000)

		// Create first item in catA
		status1, res1, err1 := itemHandler.Handle(ctx, actor, catalog.CreateItemCommand{
			RequestID:  uuid.New(),
			CategoryID: catA.ID,
			Name:       "Americano",
			PriceVND:   &price,
			ManagerPIN: manager.PIN,
		})
		require.NoError(t, err1)
		assert.Equal(t, 201, status1)
		assert.NotEqual(t, uuid.Nil, res1.ID)

		// Conflicting item with same normalized name in same category catA
		status2, _, err2 := itemHandler.Handle(ctx, actor, catalog.CreateItemCommand{
			RequestID:  uuid.New(),
			CategoryID: catA.ID,
			Name:       "   aMeRiCaNo   ",
			PriceVND:   &price,
			ManagerPIN: manager.PIN,
		})
		require.Error(t, err2)
		assert.True(t, errors.Is(err2, catalog.ErrNameConflict), "expected ErrNameConflict, got: %v", err2)
		assert.Equal(t, 0, status2)

		// Same item name in different category catB must succeed!
		status3, res3, err3 := itemHandler.Handle(ctx, actor, catalog.CreateItemCommand{
			RequestID:  uuid.New(),
			CategoryID: catB.ID,
			Name:       "Americano",
			PriceVND:   &price,
			ManagerPIN: manager.PIN,
		})
		require.NoError(t, err3)
		assert.Equal(t, 201, status3)
		assert.NotEqual(t, uuid.Nil, res3.ID)
		assert.Equal(t, catB.ID, res3.CategoryID)
	})

	t.Run("MissingCategory", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		itemHandler := catalog.NewCreateItemHandler(runner)

		price := int64(25000)
		status, _, err := itemHandler.Handle(ctx, actor, catalog.CreateItemCommand{
			RequestID:  uuid.New(),
			CategoryID: uuid.New(), // Non-existent category
			Name:       "Ghost Drink",
			PriceVND:   &price,
			ManagerPIN: manager.PIN,
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrNotFound), "expected ErrNotFound, got: %v", err)
		assert.Equal(t, 0, status)
	})

	t.Run("CategoryLock", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		catHandler := catalog.NewCreateCategoryHandler(runner)
		itemHandler := catalog.NewCreateItemHandler(runner)

		_, cat, err := catHandler.Handle(ctx, actor, catalog.CreateCategoryCommand{
			RequestID: uuid.New(),
			Name:      "Lock Target Category",
		})
		require.NoError(t, err)

		// Acquire row lock in a separate transaction
		tx, err := db.BeginTx(ctx, nil)
		require.NoError(t, err)
		defer tx.Rollback()
		_, err = tx.ExecContext(ctx, `SELECT id FROM menu_categories WHERE id = $1 FOR UPDATE`, cat.ID)
		require.NoError(t, err)

		price := int64(30000)
		reqID := uuid.New()
		cmd := catalog.CreateItemCommand{
			RequestID:  reqID,
			CategoryID: cat.ID,
			Name:       "Contended Item",
			PriceVND:   &price,
			ManagerPIN: manager.PIN,
		}

		// Attempt handle with short timeout — must block and fail due to lock
		timeoutCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()

		_, _, err = itemHandler.Handle(timeoutCtx, actor, cmd)
		require.Error(t, err, "must fail or time out waiting for locked category row")

		// Release the lock
		_ = tx.Rollback()

		// Now executing with normal context must succeed
		status, res, err := itemHandler.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		assert.Equal(t, 201, status)
		assert.NotEqual(t, uuid.Nil, res.ID)
	})

	t.Run("FreshManagerPIN", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		catHandler := catalog.NewCreateCategoryHandler(runner)
		itemHandler := catalog.NewCreateItemHandler(runner)

		_, cat, err := catHandler.Handle(ctx, actor, catalog.CreateCategoryCommand{
			RequestID: uuid.New(),
			Name:      "Beverages",
		})
		require.NoError(t, err)

		price := int64(50000)

		// Attempt with incorrect manager PIN
		status1, _, err1 := itemHandler.Handle(ctx, actor, catalog.CreateItemCommand{
			RequestID:  uuid.New(),
			CategoryID: cat.ID,
			Name:       "PIN Test Item",
			PriceVND:   &price,
			ManagerPIN: "999999", // Wrong PIN
		})
		require.Error(t, err1)
		assert.True(t, errors.Is(err1, catalog.ErrInvalidManagerPin), "expected ErrInvalidManagerPin, got: %v", err1)
		assert.Equal(t, 0, status1)

		// Verify denial audit event recorded
		assert.Equal(t, 1, countAuthorizationDenials(t, db, "catalog.item.create"))

		// Attempt by an actor without change_price capability (Cashier)
		cashier := createCatalogTestIdentity(t, db, q, []string{auth.RoleCashier}, true, "5678")
		cashierActor := catalog.Actor{StaffID: cashier.StaffID, SessionID: cashier.SessionID}

		status2, _, err2 := itemHandler.Handle(ctx, cashierActor, catalog.CreateItemCommand{
			RequestID:  uuid.New(),
			CategoryID: cat.ID,
			Name:       "Cashier Item",
			PriceVND:   &price,
			ManagerPIN: cashier.PIN,
		})
		require.Error(t, err2)
		assert.True(t, errors.Is(err2, catalog.ErrForbidden), "expected ErrForbidden, got: %v", err2)
		assert.Equal(t, 0, status2)
	})

	t.Run("ExactReplay_And_PINRequiredOnReplay", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		catHandler := catalog.NewCreateCategoryHandler(runner)
		itemHandler := catalog.NewCreateItemHandler(runner)

		_, cat, err := catHandler.Handle(ctx, actor, catalog.CreateCategoryCommand{
			RequestID: uuid.New(),
			Name:      "Beverages",
		})
		require.NoError(t, err)

		price := int64(38000)
		reqID := uuid.New()
		cmd := catalog.CreateItemCommand{
			RequestID:  reqID,
			CategoryID: cat.ID,
			Name:       "Iced Americano",
			PriceVND:   &price,
			ManagerPIN: manager.PIN,
		}

		// First execution
		status1, res1, err1 := itemHandler.Handle(ctx, actor, cmd)
		require.NoError(t, err1)
		assert.Equal(t, 201, status1)

		// Exact replay with same PIN
		status2, res2, err2 := itemHandler.Handle(ctx, actor, cmd)
		require.NoError(t, err2)
		assert.Equal(t, 201, status2)
		assert.Equal(t, res1, res2)

		// Verify database counts
		var itemCount, auditCount int
		err = db.QueryRowContext(ctx, `SELECT count(*) FROM menu_items WHERE id = $1`, res1.ID).Scan(&itemCount)
		require.NoError(t, err)
		assert.Equal(t, 1, itemCount)

		err = db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE event_type = 'catalog.item.created'`).Scan(&auditCount)
		require.NoError(t, err)
		assert.Equal(t, 1, auditCount, "replay must not duplicate audit events")

		// Replay with incorrect PIN must fail fresh PIN verification before replay!
		cmdWrongPIN := cmd
		cmdWrongPIN.ManagerPIN = "000000"
		status3, _, err3 := itemHandler.Handle(ctx, actor, cmdWrongPIN)
		require.Error(t, err3)
		assert.True(t, errors.Is(err3, catalog.ErrInvalidManagerPin), "replay must verify fresh manager PIN")
		assert.Equal(t, 0, status3)
	})

	t.Run("RollbackWhenChildInsertFails", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		catHandler := catalog.NewCreateCategoryHandler(runner)
		itemHandler := catalog.NewCreateItemHandler(runner)

		_, cat, err := catHandler.Handle(ctx, actor, catalog.CreateCategoryCommand{
			RequestID: uuid.New(),
			Name:      "Rollback Category",
		})
		require.NoError(t, err)

		// Install a trigger on menu_item_sizes that aborts if size name is FAIL_CHILD
		_, err = db.ExecContext(ctx, `
			CREATE OR REPLACE FUNCTION fail_size_test() RETURNS trigger AS $$
			BEGIN
				IF NEW.name = 'FAIL_CHILD' THEN
					RAISE EXCEPTION 'forced child insert failure';
				END IF;
				RETURN NEW;
			END;
			$$ LANGUAGE plpgsql;

			DROP TRIGGER IF EXISTS trg_fail_size_test ON menu_item_sizes;
			CREATE TRIGGER trg_fail_size_test
			BEFORE INSERT ON menu_item_sizes
			FOR EACH ROW EXECUTE FUNCTION fail_size_test();
		`)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = db.ExecContext(context.Background(), `
				DROP TRIGGER IF EXISTS trg_fail_size_test ON menu_item_sizes;
				DROP FUNCTION IF EXISTS fail_size_test();
			`)
		})

		reqID := uuid.New()
		cmd := catalog.CreateItemCommand{
			RequestID:  reqID,
			CategoryID: cat.ID,
			Name:       "Failed Parent Item",
			Sizes: []catalog.CreateSizeInput{
				{Name: "Good Size", PriceVND: 30000},
				{Name: "FAIL_CHILD", PriceVND: 40000},
			},
			ManagerPIN: manager.PIN,
		}

		status, _, err := itemHandler.Handle(ctx, actor, cmd)
		require.Error(t, err)
		assert.Equal(t, 0, status)

		// Assert parent item was rolled back
		var itemCount int
		err = db.QueryRowContext(ctx, `SELECT count(*) FROM menu_items WHERE category_id = $1`, cat.ID).Scan(&itemCount)
		require.NoError(t, err)
		assert.Equal(t, 0, itemCount, "parent item must be rolled back when child size fails")

		// Assert no sizes were committed
		var sizeCount int
		err = db.QueryRowContext(ctx, `SELECT count(*) FROM menu_item_sizes`).Scan(&sizeCount)
		require.NoError(t, err)
		assert.Equal(t, 0, sizeCount, "no child sizes should be committed")

		// Assert no audit events were committed
		var auditCount int
		err = db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE event_type = 'catalog.item.created'`).Scan(&auditCount)
		require.NoError(t, err)
		assert.Equal(t, 0, auditCount, "no audit events should be committed on rollback")

		// Assert idempotency request was rolled back (can retry cleanly)
		var reqCount int
		err = db.QueryRowContext(ctx, `SELECT count(*) FROM catalog_mutation_requests WHERE request_id = $1`, reqID).Scan(&reqCount)
		require.NoError(t, err)
		assert.Equal(t, 0, reqCount, "idempotency claim must roll back on failure")
	})
}
