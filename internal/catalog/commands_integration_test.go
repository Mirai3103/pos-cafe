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
	"github.com/Mirai3103/pos-cafe/internal/response"
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
			modifier_groups,
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

	t.Run("EmptyNameRejected", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createTestIdentity(t, db, q, []string{auth.RoleManager}, true)
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewCreateCategoryHandler(runner)

		status, _, err := handler.Handle(ctx, actor, catalog.CreateCategoryCommand{
			RequestID: uuid.New(),
			Name:      "   ",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrInvalidCategoryConfiguration), "expected ErrInvalidCategoryConfiguration, got: %v", err)
		assert.Equal(t, 0, status)

		// Verify nothing was persisted
		var count int
		err = db.QueryRowContext(ctx, `SELECT count(*) FROM menu_categories`).Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 0, count, "whitespace-only name must not create a category")
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

	t.Run("EmptyNameRejected", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createTestIdentity(t, db, q, []string{auth.RoleManager}, true)
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		createHandler := catalog.NewCreateCategoryHandler(runner)
		renameHandler := catalog.NewRenameCategoryHandler(runner)

		_, created, err := createHandler.Handle(ctx, actor, catalog.CreateCategoryCommand{
			RequestID: uuid.New(),
			Name:      "Cold Drinks",
		})
		require.NoError(t, err)

		status, _, err := renameHandler.Handle(ctx, actor, catalog.RenameCategoryCommand{
			RequestID:  uuid.New(),
			CategoryID: created.ID,
			Name:       "   ",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrInvalidCategoryConfiguration), "expected ErrInvalidCategoryConfiguration, got: %v", err)
		assert.Equal(t, 0, status)

		// Verify the category name is unchanged
		var name string
		err = db.QueryRowContext(ctx, `SELECT name FROM menu_categories WHERE id = $1`, created.ID).Scan(&name)
		require.NoError(t, err)
		assert.Equal(t, "Cold Drinks", name, "whitespace-only rename must not change the category")
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

	t.Run("EmptyNameRejected", func(t *testing.T) {
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

		price := int64(45000)
		status, _, err := itemHandler.Handle(ctx, actor, catalog.CreateItemCommand{
			RequestID:  uuid.New(),
			CategoryID: cat.ID,
			Name:       "   ",
			PriceVND:   &price,
			ManagerPIN: manager.PIN,
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrInvalidPricingConfiguration), "expected ErrInvalidPricingConfiguration, got: %v", err)
		assert.Equal(t, 0, status)

		// Verify no item was persisted
		var itemCount int
		err = db.QueryRowContext(ctx, `SELECT count(*) FROM menu_items`).Scan(&itemCount)
		require.NoError(t, err)
		assert.Equal(t, 0, itemCount, "whitespace-only name must not create an item")
	})

	t.Run("MissingCategoryIDRejected", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		// Valid manager identity with correct PIN: auth must pass so the
		// request reaches the handler's own validation, proving enforcement
		// lives in CreateItemHandler and not the HTTP transport layer.
		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		itemHandler := catalog.NewCreateItemHandler(runner)

		price := int64(45000)
		status, _, err := itemHandler.Handle(ctx, actor, catalog.CreateItemCommand{
			RequestID:  uuid.New(),
			CategoryID: uuid.Nil,
			Name:       "Orphan Drink",
			PriceVND:   &price,
			ManagerPIN: manager.PIN,
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, response.ErrInvalid), "expected ErrInvalid, got: %v", err)
		assert.Equal(t, 0, status)

		// Verify no item was persisted
		var itemCount int
		err = db.QueryRowContext(ctx, `SELECT count(*) FROM menu_items`).Scan(&itemCount)
		require.NoError(t, err)
		assert.Equal(t, 0, itemCount, "nil category_id must not create an item")
	})
}

func TestCreateModifierGroup(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()

	t.Run("Success_WithDefaults_Audit_Normalization", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewCreateModifierGroupHandler(runner)

		reqID := uuid.New()
		inputName := "   Ice   Level   "
		expectedDisplayName := "Ice   Level"
		expectedNormalizedKey := "ice   level"

		cmd := catalog.CreateModifierGroupCommand{
			RequestID:     reqID,
			Name:          inputName,
			MinSelections: 1,
			MaxSelections: 1,
			Options: []catalog.CreateModifierOptionInput{
				{Name: "   No   Ice   ", SurchargeVND: 0},
				{Name: "Less Ice", SurchargeVND: 0},
				{Name: "   Regular   Ice   ", SurchargeVND: 0},
				{Name: "Extra Ice", SurchargeVND: 2000},
			},
			DefaultOptionNames: []string{"   rEgUlAr   iCe   "},
			ManagerPIN:         manager.PIN,
		}

		status, res, err := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		assert.Equal(t, 201, status)
		assert.NotEqual(t, uuid.Nil, res.ID)
		assert.Equal(t, expectedDisplayName, res.Name)
		assert.Equal(t, int32(1), res.MinSelections)
		assert.Equal(t, int32(1), res.MaxSelections)
		require.Len(t, res.Options, 4)

		assert.Equal(t, "No   Ice", res.Options[0].Name)
		assert.Equal(t, int64(0), res.Options[0].SurchargeVND)
		assert.True(t, res.Options[0].Available)
		assert.Equal(t, res.ID, res.Options[0].ModifierGroupID)

		assert.Equal(t, "Less Ice", res.Options[1].Name)
		assert.Equal(t, int64(0), res.Options[1].SurchargeVND)

		assert.Equal(t, "Regular   Ice", res.Options[2].Name)
		assert.Equal(t, int64(0), res.Options[2].SurchargeVND)

		assert.Equal(t, "Extra Ice", res.Options[3].Name)
		assert.Equal(t, int64(2000), res.Options[3].SurchargeVND)

		require.Len(t, res.DefaultOptionIDs, 1)
		assert.Equal(t, res.Options[2].ID, res.DefaultOptionIDs[0])

		// Verify database row in modifier_groups
		var dbName, dbNorm string
		var dbMin, dbMax int32
		err = db.QueryRowContext(ctx, `
			SELECT name, normalized_name, min_selections, max_selections
			FROM modifier_groups
			WHERE id = $1
		`, res.ID).Scan(&dbName, &dbNorm, &dbMin, &dbMax)
		require.NoError(t, err)
		assert.Equal(t, expectedDisplayName, dbName)
		assert.Equal(t, expectedNormalizedKey, dbNorm)
		assert.Equal(t, int32(1), dbMin)
		assert.Equal(t, int32(1), dbMax)

		// Verify database rows in modifier_options
		var optCount int
		err = db.QueryRowContext(ctx, `SELECT count(*) FROM modifier_options WHERE modifier_group_id = $1`, res.ID).Scan(&optCount)
		require.NoError(t, err)
		assert.Equal(t, 4, optCount)

		// Verify database rows in modifier_group_default_options
		var defOptID uuid.UUID
		err = db.QueryRowContext(ctx, `
			SELECT modifier_option_id
			FROM modifier_group_default_options
			WHERE modifier_group_id = $1
		`, res.ID).Scan(&defOptID)
		require.NoError(t, err)
		assert.Equal(t, res.Options[2].ID, defOptID)

		// Verify audit event
		var eventType string
		var actorID, sessionID uuid.UUID
		var detailsJSON []byte
		err = db.QueryRowContext(ctx, `
			SELECT event_type, actor_id, session_id, details
			FROM audit_events
			WHERE event_type = 'catalog.modifier_group.created' AND actor_id = $1
		`, actor.StaffID).Scan(&eventType, &actorID, &sessionID, &detailsJSON)
		require.NoError(t, err)
		assert.Equal(t, "catalog.modifier_group.created", eventType)
		assert.Equal(t, actor.StaffID, actorID)
		assert.Equal(t, actor.SessionID, sessionID)

		var details map[string]any
		err = json.Unmarshal(detailsJSON, &details)
		require.NoError(t, err)
		assert.Equal(t, res.ID.String(), details["group_id"])
		assert.Equal(t, expectedDisplayName, details["name"])
		assert.Equal(t, float64(1), details["min_selections"])
		assert.Equal(t, float64(1), details["max_selections"])
		require.NotNil(t, details["options"])
		optsList, ok := details["options"].([]any)
		require.True(t, ok)
		assert.Len(t, optsList, 4)

		require.NotNil(t, details["default_option_ids"])
		defsList, ok := details["default_option_ids"].([]any)
		require.True(t, ok)
		assert.Len(t, defsList, 1)
		assert.Equal(t, res.Options[2].ID.String(), defsList[0])

		// Generic PIN secrecy check: audit details must NEVER contain manager PIN
		assert.False(t, strings.Contains(string(detailsJSON), manager.PIN), "audit details must never leak manager PIN")
	})

	t.Run("Success_NoDefaults", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewCreateModifierGroupHandler(runner)

		cmd := catalog.CreateModifierGroupCommand{
			RequestID:     uuid.New(),
			Name:          "Toppings",
			MinSelections: 0,
			MaxSelections: 2,
			Options: []catalog.CreateModifierOptionInput{
				{Name: "Pearls", SurchargeVND: 5000},
				{Name: "Pudding", SurchargeVND: 6000},
			},
			ManagerPIN: manager.PIN,
		}

		status, res, err := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		assert.Equal(t, 201, status)
		assert.NotEqual(t, uuid.Nil, res.ID)
		assert.Equal(t, "Toppings", res.Name)
		assert.Equal(t, int32(0), res.MinSelections)
		assert.Equal(t, int32(2), res.MaxSelections)
		assert.Empty(t, res.DefaultOptionIDs)

		var defCount int
		err = db.QueryRowContext(ctx, `SELECT count(*) FROM modifier_group_default_options WHERE modifier_group_id = $1`, res.ID).Scan(&defCount)
		require.NoError(t, err)
		assert.Equal(t, 0, defCount)
	})

	t.Run("Reject_NoOptions", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewCreateModifierGroupHandler(runner)

		cmd := catalog.CreateModifierGroupCommand{
			RequestID:     uuid.New(),
			Name:          "No Option Group",
			MinSelections: 0,
			MaxSelections: 1,
			Options:       []catalog.CreateModifierOptionInput{},
			ManagerPIN:    manager.PIN,
		}

		status, _, err := handler.Handle(ctx, actor, cmd)
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrInvalidModifierConfiguration), "expected ErrInvalidModifierConfiguration, got: %v", err)
		assert.Equal(t, 0, status)
	})

	t.Run("Reject_DuplicateGroupName", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewCreateModifierGroupHandler(runner)

		status1, _, err1 := handler.Handle(ctx, actor, catalog.CreateModifierGroupCommand{
			RequestID:     uuid.New(),
			Name:          "Sweetness",
			MinSelections: 1,
			MaxSelections: 1,
			Options: []catalog.CreateModifierOptionInput{
				{Name: "100%", SurchargeVND: 0},
			},
			ManagerPIN: manager.PIN,
		})
		require.NoError(t, err1)
		assert.Equal(t, 201, status1)

		// Second group with whitespace/case variation
		status2, _, err2 := handler.Handle(ctx, actor, catalog.CreateModifierGroupCommand{
			RequestID:     uuid.New(),
			Name:          "   sWeEtNeSs   ",
			MinSelections: 1,
			MaxSelections: 1,
			Options: []catalog.CreateModifierOptionInput{
				{Name: "50%", SurchargeVND: 0},
			},
			ManagerPIN: manager.PIN,
		})
		require.Error(t, err2)
		assert.True(t, errors.Is(err2, catalog.ErrNameConflict), "expected ErrNameConflict, got: %v", err2)
		assert.Equal(t, 0, status2)
	})

	t.Run("Reject_DuplicateOptionNames", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewCreateModifierGroupHandler(runner)

		cmd := catalog.CreateModifierGroupCommand{
			RequestID:     uuid.New(),
			Name:          "Toppings",
			MinSelections: 1,
			MaxSelections: 2,
			Options: []catalog.CreateModifierOptionInput{
				{Name: "Pearls", SurchargeVND: 5000},
				{Name: "   pEaRlS   ", SurchargeVND: 6000},
			},
			ManagerPIN: manager.PIN,
		}

		status, _, err := handler.Handle(ctx, actor, cmd)
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrNameConflict), "expected ErrNameConflict, got: %v", err)
		assert.Equal(t, 0, status)
	})

	t.Run("Reject_NegativeSurcharge", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewCreateModifierGroupHandler(runner)

		cmd := catalog.CreateModifierGroupCommand{
			RequestID:     uuid.New(),
			Name:          "Toppings",
			MinSelections: 0,
			MaxSelections: 1,
			Options: []catalog.CreateModifierOptionInput{
				{Name: "Pearls", SurchargeVND: -1},
			},
			ManagerPIN: manager.PIN,
		}

		status, _, err := handler.Handle(ctx, actor, cmd)
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrInvalidModifierConfiguration), "expected ErrInvalidModifierConfiguration, got: %v", err)
		assert.Equal(t, 0, status)
	})

	t.Run("Reject_SurchargeAboveMax", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewCreateModifierGroupHandler(runner)

		cmd := catalog.CreateModifierGroupCommand{
			RequestID:     uuid.New(),
			Name:          "Toppings",
			MinSelections: 0,
			MaxSelections: 1,
			Options: []catalog.CreateModifierOptionInput{
				{Name: "Pearls", SurchargeVND: 2_147_483_648},
			},
			ManagerPIN: manager.PIN,
		}

		status, _, err := handler.Handle(ctx, actor, cmd)
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrInvalidModifierConfiguration), "expected ErrInvalidModifierConfiguration, got: %v", err)
		assert.Equal(t, 0, status)
	})

	t.Run("Reject_InvalidMinMaxBounds", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewCreateModifierGroupHandler(runner)

		cases := []struct {
			name string
			min  int32
			max  int32
		}{
			{"Min_Negative", -1, 1},
			{"Max_Zero", 0, 0},
			{"Min_GreaterThan_Max", 3, 2},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				cmd := catalog.CreateModifierGroupCommand{
					RequestID:     uuid.New(),
					Name:          "Bounds Test Group",
					MinSelections: tc.min,
					MaxSelections: tc.max,
					Options: []catalog.CreateModifierOptionInput{
						{Name: "Opt1", SurchargeVND: 0},
						{Name: "Opt2", SurchargeVND: 0},
						{Name: "Opt3", SurchargeVND: 0},
					},
					ManagerPIN: manager.PIN,
				}

				status, _, err := handler.Handle(ctx, actor, cmd)
				require.Error(t, err)
				assert.True(t, errors.Is(err, catalog.ErrInvalidModifierConfiguration), "expected ErrInvalidModifierConfiguration, got: %v", err)
				assert.Equal(t, 0, status)
			})
		}
	})

	t.Run("Reject_MaxSelectionsExceedsOptionCount", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewCreateModifierGroupHandler(runner)

		cmd := catalog.CreateModifierGroupCommand{
			RequestID:     uuid.New(),
			Name:          "Small Group",
			MinSelections: 1,
			MaxSelections: 3, // 3 > 2 options
			Options: []catalog.CreateModifierOptionInput{
				{Name: "Opt1", SurchargeVND: 0},
				{Name: "Opt2", SurchargeVND: 0},
			},
			ManagerPIN: manager.PIN,
		}

		status, _, err := handler.Handle(ctx, actor, cmd)
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrInvalidModifierConfiguration), "expected ErrInvalidModifierConfiguration, got: %v", err)
		assert.Equal(t, 0, status)
	})

	t.Run("Reject_DefaultOptionNotFound", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewCreateModifierGroupHandler(runner)

		cmd := catalog.CreateModifierGroupCommand{
			RequestID:     uuid.New(),
			Name:          "Group",
			MinSelections: 1,
			MaxSelections: 1,
			Options: []catalog.CreateModifierOptionInput{
				{Name: "Opt1", SurchargeVND: 0},
			},
			DefaultOptionNames: []string{"NonExistent"},
			ManagerPIN:         manager.PIN,
		}

		status, _, err := handler.Handle(ctx, actor, cmd)
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrInvalidModifierConfiguration), "expected ErrInvalidModifierConfiguration, got: %v", err)
		assert.Equal(t, 0, status)
	})

	t.Run("Reject_DuplicateDefaultOptionNames", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewCreateModifierGroupHandler(runner)

		cmd := catalog.CreateModifierGroupCommand{
			RequestID:     uuid.New(),
			Name:          "Group",
			MinSelections: 1,
			MaxSelections: 2,
			Options: []catalog.CreateModifierOptionInput{
				{Name: "Opt1", SurchargeVND: 0},
				{Name: "Opt2", SurchargeVND: 0},
			},
			DefaultOptionNames: []string{"Opt1", "   oPt1   "},
			ManagerPIN:         manager.PIN,
		}

		status, _, err := handler.Handle(ctx, actor, cmd)
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrInvalidModifierConfiguration), "expected ErrInvalidModifierConfiguration, got: %v", err)
		assert.Equal(t, 0, status)
	})

	t.Run("Reject_DefaultOptionCardinalityTooLow", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewCreateModifierGroupHandler(runner)

		cmd := catalog.CreateModifierGroupCommand{
			RequestID:     uuid.New(),
			Name:          "Group",
			MinSelections: 2,
			MaxSelections: 3,
			Options: []catalog.CreateModifierOptionInput{
				{Name: "Opt1", SurchargeVND: 0},
				{Name: "Opt2", SurchargeVND: 0},
				{Name: "Opt3", SurchargeVND: 0},
			},
			DefaultOptionNames: []string{"Opt1"}, // 1 default < min 2
			ManagerPIN:         manager.PIN,
		}

		status, _, err := handler.Handle(ctx, actor, cmd)
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrInvalidModifierConfiguration), "expected ErrInvalidModifierConfiguration, got: %v", err)
		assert.Equal(t, 0, status)
	})

	t.Run("Reject_DefaultOptionCardinalityTooHigh", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewCreateModifierGroupHandler(runner)

		cmd := catalog.CreateModifierGroupCommand{
			RequestID:     uuid.New(),
			Name:          "Group",
			MinSelections: 1,
			MaxSelections: 2,
			Options: []catalog.CreateModifierOptionInput{
				{Name: "Opt1", SurchargeVND: 0},
				{Name: "Opt2", SurchargeVND: 0},
				{Name: "Opt3", SurchargeVND: 0},
			},
			DefaultOptionNames: []string{"Opt1", "Opt2", "Opt3"}, // 3 defaults > max 2
			ManagerPIN:         manager.PIN,
		}

		status, _, err := handler.Handle(ctx, actor, cmd)
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrInvalidModifierConfiguration), "expected ErrInvalidModifierConfiguration, got: %v", err)
		assert.Equal(t, 0, status)
	})

	t.Run("Reject_EmptyOptionName", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewCreateModifierGroupHandler(runner)

		cmd := catalog.CreateModifierGroupCommand{
			RequestID:     uuid.New(),
			Name:          "Group",
			MinSelections: 0,
			MaxSelections: 1,
			Options: []catalog.CreateModifierOptionInput{
				{Name: "   ", SurchargeVND: 0},
			},
			ManagerPIN: manager.PIN,
		}

		status, _, err := handler.Handle(ctx, actor, cmd)
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrInvalidModifierConfiguration), "expected ErrInvalidModifierConfiguration, got: %v", err)
		assert.Equal(t, 0, status)
	})

	t.Run("Reject_EmptyGroupName", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewCreateModifierGroupHandler(runner)

		cmd := catalog.CreateModifierGroupCommand{
			RequestID:     uuid.New(),
			Name:          "   ",
			MinSelections: 0,
			MaxSelections: 1,
			Options: []catalog.CreateModifierOptionInput{
				{Name: "Opt1", SurchargeVND: 0},
			},
			ManagerPIN: manager.PIN,
		}

		status, _, err := handler.Handle(ctx, actor, cmd)
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrInvalidModifierConfiguration), "expected ErrInvalidModifierConfiguration, got: %v", err)
		assert.Equal(t, 0, status)
	})

	t.Run("FreshManagerPIN", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewCreateModifierGroupHandler(runner)

		cmd := catalog.CreateModifierGroupCommand{
			RequestID:     uuid.New(),
			Name:          "Group",
			MinSelections: 0,
			MaxSelections: 1,
			Options: []catalog.CreateModifierOptionInput{
				{Name: "Opt1", SurchargeVND: 0},
			},
			ManagerPIN: "9999", // Invalid PIN
		}

		status, _, err := handler.Handle(ctx, actor, cmd)
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrInvalidManagerPin), "expected ErrInvalidManagerPin, got: %v", err)
		assert.Equal(t, 0, status)
	})

	t.Run("Forbidden_MissingCapabilities", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		cashier := createCatalogTestIdentity(t, db, q, []string{auth.RoleCashier}, true, "1234")
		actor := catalog.Actor{StaffID: cashier.StaffID, SessionID: cashier.SessionID}
		handler := catalog.NewCreateModifierGroupHandler(runner)

		cmd := catalog.CreateModifierGroupCommand{
			RequestID:     uuid.New(),
			Name:          "Group",
			MinSelections: 0,
			MaxSelections: 1,
			Options: []catalog.CreateModifierOptionInput{
				{Name: "Opt1", SurchargeVND: 0},
			},
			ManagerPIN: cashier.PIN,
		}

		status, _, err := handler.Handle(ctx, actor, cmd)
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrForbidden), "expected ErrForbidden, got: %v", err)
		assert.Equal(t, 0, status)
	})

	t.Run("ExactReplay_And_PINRequiredOnReplay", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewCreateModifierGroupHandler(runner)

		reqID := uuid.New()
		cmd := catalog.CreateModifierGroupCommand{
			RequestID:     reqID,
			Name:          "Sugar Level",
			MinSelections: 1,
			MaxSelections: 1,
			Options: []catalog.CreateModifierOptionInput{
				{Name: "100%", SurchargeVND: 0},
				{Name: "50%", SurchargeVND: 0},
			},
			DefaultOptionNames: []string{"100%"},
			ManagerPIN:         manager.PIN,
		}

		// Initial creation
		status1, res1, err1 := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err1)
		assert.Equal(t, 201, status1)
		assert.NotEqual(t, uuid.Nil, res1.ID)

		// Exact replay with valid PIN
		status2, res2, err2 := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err2)
		assert.Equal(t, 201, status2)
		assert.Equal(t, res1.ID, res2.ID)
		assert.Equal(t, res1.Name, res2.Name)

		// Replay with wrong PIN must fail
		badPINCmd := cmd
		badPINCmd.ManagerPIN = "9999"
		status3, _, err3 := handler.Handle(ctx, actor, badPINCmd)
		require.Error(t, err3)
		assert.True(t, errors.Is(err3, catalog.ErrInvalidManagerPin), "expected ErrInvalidManagerPin on replay, got: %v", err3)
		assert.Equal(t, 0, status3)

		// Conflicting replay with different options
		conflictCmd := cmd
		conflictCmd.Options = []catalog.CreateModifierOptionInput{
			{Name: "Different", SurchargeVND: 1000},
		}
		status4, _, err4 := handler.Handle(ctx, actor, conflictCmd)
		require.Error(t, err4)
		assert.True(t, errors.Is(err4, catalog.ErrRequestConflict), "expected ErrRequestConflict on changed payload, got: %v", err4)
		assert.Equal(t, 0, status4)

		// Verify only 1 audit event was committed
		var auditCount int
		err := db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE event_type = 'catalog.modifier_group.created'`).Scan(&auditCount)
		require.NoError(t, err)
		assert.Equal(t, 1, auditCount, "idempotent replay must not duplicate audit events")
	})

	t.Run("AtomicRollback_OnFailure", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewCreateModifierGroupHandler(runner)

		// Install a trigger on modifier_options that aborts if option name is FAIL_OPTION
		_, err := db.ExecContext(ctx, `
			CREATE OR REPLACE FUNCTION fail_option_test() RETURNS trigger AS $$
			BEGIN
				IF NEW.name = 'FAIL_OPTION' THEN
					RAISE EXCEPTION 'forced child option insert failure';
				END IF;
				RETURN NEW;
			END;
			$$ LANGUAGE plpgsql;

			DROP TRIGGER IF EXISTS trg_fail_option_test ON modifier_options;
			CREATE TRIGGER trg_fail_option_test
			BEFORE INSERT ON modifier_options
			FOR EACH ROW EXECUTE FUNCTION fail_option_test();
		`)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = db.ExecContext(context.Background(), `
				DROP TRIGGER IF EXISTS trg_fail_option_test ON modifier_options;
				DROP FUNCTION IF EXISTS fail_option_test();
			`)
		})

		reqID := uuid.New()
		cmd := catalog.CreateModifierGroupCommand{
			RequestID:     reqID,
			Name:          "Failed Parent Group",
			MinSelections: 0,
			MaxSelections: 2,
			Options: []catalog.CreateModifierOptionInput{
				{Name: "Good Option", SurchargeVND: 0},
				{Name: "FAIL_OPTION", SurchargeVND: 1000},
			},
			ManagerPIN: manager.PIN,
		}

		status, _, err := handler.Handle(ctx, actor, cmd)
		require.Error(t, err)
		assert.Equal(t, 0, status)

		// Assert parent group was rolled back
		var groupCount int
		err = db.QueryRowContext(ctx, `SELECT count(*) FROM modifier_groups WHERE name = 'Failed Parent Group'`).Scan(&groupCount)
		require.NoError(t, err)
		assert.Equal(t, 0, groupCount, "parent modifier group must be rolled back when child option fails")

		// Assert no options were committed
		var optCount int
		err = db.QueryRowContext(ctx, `SELECT count(*) FROM modifier_options`).Scan(&optCount)
		require.NoError(t, err)
		assert.Equal(t, 0, optCount, "no child options should be committed")

		// Assert no default options were committed
		var defCount int
		err = db.QueryRowContext(ctx, `SELECT count(*) FROM modifier_group_default_options`).Scan(&defCount)
		require.NoError(t, err)
		assert.Equal(t, 0, defCount, "no default options should be committed")

		// Assert no audit events were committed
		var auditCount int
		err = db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE event_type = 'catalog.modifier_group.created'`).Scan(&auditCount)
		require.NoError(t, err)
		assert.Equal(t, 0, auditCount, "no audit events should be committed on rollback")

		// Assert idempotency request was rolled back (can retry cleanly)
		var reqCount int
		err = db.QueryRowContext(ctx, `SELECT count(*) FROM catalog_mutation_requests WHERE request_id = $1`, reqID).Scan(&reqCount)
		require.NoError(t, err)
		assert.Equal(t, 0, reqCount, "idempotency claim must roll back on failure")
	})
}

func createTestCategoryDirect(t *testing.T, db *sql.DB, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	display, key := catalog.NormalizeName(name)
	err := db.QueryRow(`INSERT INTO menu_categories (name, normalized_name) VALUES ($1, $2) RETURNING id`, display, key).Scan(&id)
	require.NoError(t, err)
	return id
}

func createTestItemDirect(t *testing.T, db *sql.DB, categoryID uuid.UUID, name string, price *int64, retired bool) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	display, key := catalog.NormalizeName(name)
	var retiredAt *time.Time
	var reason, note *string
	if retired {
		now := time.Now()
		retiredAt = &now
		r := "NO_LONGER_OFFERED"
		reason = &r
	}
	err := db.QueryRow(`
		INSERT INTO menu_items (category_id, name, normalized_name, price_vnd, available, retired_at, retirement_reason, retirement_note)
		VALUES ($1, $2, $3, $4, true, $5, $6, $7)
		RETURNING id
	`, categoryID, display, key, price, retiredAt, reason, note).Scan(&id)
	require.NoError(t, err)
	return id
}

func createTestModifierGroupDirect(t *testing.T, db *sql.DB, name string, minSel, maxSel int32, retired bool) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	display, key := catalog.NormalizeName(name)
	var retiredAt *time.Time
	var reason, note *string
	if retired {
		now := time.Now()
		retiredAt = &now
		r := "NO_LONGER_OFFERED"
		reason = &r
	}
	err := db.QueryRow(`
		INSERT INTO modifier_groups (name, normalized_name, min_selections, max_selections, retired_at, retirement_reason, retirement_note)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id
	`, display, key, minSel, maxSel, retiredAt, reason, note).Scan(&id)
	require.NoError(t, err)
	return id
}

func createTestModifierOptionDirect(t *testing.T, db *sql.DB, groupID uuid.UUID, name string, surcharge int64, available bool, retired bool) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	display, key := catalog.NormalizeName(name)
	var retiredAt *time.Time
	var reason, note *string
	if retired {
		now := time.Now()
		retiredAt = &now
		r := "NO_LONGER_OFFERED"
		reason = &r
	}
	err := db.QueryRow(`
		INSERT INTO modifier_options (modifier_group_id, name, normalized_name, surcharge_vnd, available, retired_at, retirement_reason, retirement_note)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id
	`, groupID, display, key, surcharge, available, retiredAt, reason, note).Scan(&id)
	require.NoError(t, err)
	return id
}

func TestAttachItemModifierGroup(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewAttachItemModifierGroupHandler(runner)

		catID := createTestCategoryDirect(t, db, "Drinks")
		price := int64(30000)
		itemID := createTestItemDirect(t, db, catID, "Milk Tea", &price, false)
		groupID := createTestModifierGroupDirect(t, db, "Sweetness", 0, 1, false)

		reqID := uuid.New()
		cmd := catalog.AttachItemModifierGroupCommand{
			RequestID:       reqID,
			ItemID:          itemID,
			ModifierGroupID: groupID,
		}

		status, res, err := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.Equal(t, itemID, res.ItemID)
		assert.Equal(t, groupID, res.ModifierGroupID)

		// Verify database row in item_modifier_groups
		var count int
		err = db.QueryRowContext(ctx, `SELECT count(*) FROM item_modifier_groups WHERE menu_item_id = $1 AND modifier_group_id = $2`, itemID, groupID).Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 1, count)

		// Verify audit event
		var eventType string
		var detailsJSON []byte
		err = db.QueryRowContext(ctx, `SELECT event_type, details FROM audit_events WHERE event_type = $1`, catalog.EventItemModifierGroupAttached).Scan(&eventType, &detailsJSON)
		require.NoError(t, err)
		assert.Equal(t, catalog.EventItemModifierGroupAttached, eventType)

		var details map[string]any
		err = json.Unmarshal(detailsJSON, &details)
		require.NoError(t, err)
		assert.Equal(t, itemID.String(), details["item_id"])
		assert.Equal(t, groupID.String(), details["modifier_group_id"])
	})

	t.Run("Reject_ItemNotFound", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewAttachItemModifierGroupHandler(runner)
		groupID := createTestModifierGroupDirect(t, db, "Sweetness", 0, 1, false)

		cmd := catalog.AttachItemModifierGroupCommand{
			RequestID:       uuid.New(),
			ItemID:          uuid.New(),
			ModifierGroupID: groupID,
		}
		status, _, err := handler.Handle(ctx, actor, cmd)
		require.Error(t, err)
		assert.ErrorIs(t, err, catalog.ErrNotFound)
		assert.Equal(t, 0, status)
	})

	t.Run("Reject_ModifierGroupNotFound", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewAttachItemModifierGroupHandler(runner)

		catID := createTestCategoryDirect(t, db, "Drinks")
		price := int64(30000)
		itemID := createTestItemDirect(t, db, catID, "Milk Tea", &price, false)

		cmd := catalog.AttachItemModifierGroupCommand{
			RequestID:       uuid.New(),
			ItemID:          itemID,
			ModifierGroupID: uuid.New(),
		}
		status, _, err := handler.Handle(ctx, actor, cmd)
		require.Error(t, err)
		assert.ErrorIs(t, err, catalog.ErrNotFound)
		assert.Equal(t, 0, status)
	})

	t.Run("Reject_RetiredItem", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewAttachItemModifierGroupHandler(runner)

		catID := createTestCategoryDirect(t, db, "Drinks")
		price := int64(30000)
		itemID := createTestItemDirect(t, db, catID, "Retired Tea", &price, true)
		groupID := createTestModifierGroupDirect(t, db, "Sweetness", 0, 1, false)

		cmd := catalog.AttachItemModifierGroupCommand{
			RequestID:       uuid.New(),
			ItemID:          itemID,
			ModifierGroupID: groupID,
		}
		status, _, err := handler.Handle(ctx, actor, cmd)
		require.Error(t, err)
		assert.ErrorIs(t, err, catalog.ErrEntityRetired)
		assert.Equal(t, 0, status)
	})

	t.Run("Reject_RetiredModifierGroup", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewAttachItemModifierGroupHandler(runner)

		catID := createTestCategoryDirect(t, db, "Drinks")
		price := int64(30000)
		itemID := createTestItemDirect(t, db, catID, "Milk Tea", &price, false)
		groupID := createTestModifierGroupDirect(t, db, "Retired Group", 0, 1, true)

		cmd := catalog.AttachItemModifierGroupCommand{
			RequestID:       uuid.New(),
			ItemID:          itemID,
			ModifierGroupID: groupID,
		}
		status, _, err := handler.Handle(ctx, actor, cmd)
		require.Error(t, err)
		assert.ErrorIs(t, err, catalog.ErrEntityRetired)
		assert.Equal(t, 0, status)
	})

	t.Run("Reject_DuplicateAttachment", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewAttachItemModifierGroupHandler(runner)

		catID := createTestCategoryDirect(t, db, "Drinks")
		price := int64(30000)
		itemID := createTestItemDirect(t, db, catID, "Milk Tea", &price, false)
		groupID := createTestModifierGroupDirect(t, db, "Sweetness", 0, 1, false)

		cmd1 := catalog.AttachItemModifierGroupCommand{
			RequestID:       uuid.New(),
			ItemID:          itemID,
			ModifierGroupID: groupID,
		}
		status1, _, err := handler.Handle(ctx, actor, cmd1)
		require.NoError(t, err)
		assert.Equal(t, 200, status1)

		// Second attempt with new request ID must fail with conflict
		cmd2 := catalog.AttachItemModifierGroupCommand{
			RequestID:       uuid.New(),
			ItemID:          itemID,
			ModifierGroupID: groupID,
		}
		status2, _, err := handler.Handle(ctx, actor, cmd2)
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrNameConflict), "duplicate attachment should return conflict error, got: %v", err)
		assert.Equal(t, 0, status2)
	})

	t.Run("ExactReplay_And_Conflict", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewAttachItemModifierGroupHandler(runner)

		catID := createTestCategoryDirect(t, db, "Drinks")
		price := int64(30000)
		itemID := createTestItemDirect(t, db, catID, "Milk Tea", &price, false)
		groupID := createTestModifierGroupDirect(t, db, "Sweetness", 0, 1, false)
		groupID2 := createTestModifierGroupDirect(t, db, "Ice", 0, 1, false)

		reqID := uuid.New()
		cmd := catalog.AttachItemModifierGroupCommand{
			RequestID:       reqID,
			ItemID:          itemID,
			ModifierGroupID: groupID,
		}
		status1, res1, err := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		assert.Equal(t, 200, status1)

		// Exact replay
		status2, res2, err := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		assert.Equal(t, 200, status2)
		assert.Equal(t, res1, res2)

		// Verify audit event not duplicated
		var auditCount int
		err = db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE event_type = $1`, catalog.EventItemModifierGroupAttached).Scan(&auditCount)
		require.NoError(t, err)
		assert.Equal(t, 1, auditCount)

		// Replay with different group -> ErrRequestConflict
		conflictCmd := catalog.AttachItemModifierGroupCommand{
			RequestID:       reqID,
			ItemID:          itemID,
			ModifierGroupID: groupID2,
		}
		_, _, err = handler.Handle(ctx, actor, conflictCmd)
		require.Error(t, err)
		assert.ErrorIs(t, err, catalog.ErrRequestConflict)
	})

	t.Run("Forbidden_MissingCapabilities", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		cashier := createCatalogTestIdentity(t, db, q, []string{auth.RoleCashier}, true, "1234")
		actor := catalog.Actor{StaffID: cashier.StaffID, SessionID: cashier.SessionID}
		handler := catalog.NewAttachItemModifierGroupHandler(runner)

		catID := createTestCategoryDirect(t, db, "Drinks")
		price := int64(30000)
		itemID := createTestItemDirect(t, db, catID, "Milk Tea", &price, false)
		groupID := createTestModifierGroupDirect(t, db, "Sweetness", 0, 1, false)

		cmd := catalog.AttachItemModifierGroupCommand{
			RequestID:       uuid.New(),
			ItemID:          itemID,
			ModifierGroupID: groupID,
		}
		_, _, err := handler.Handle(ctx, actor, cmd)
		require.Error(t, err)
		assert.ErrorIs(t, err, catalog.ErrForbidden)
	})
}

func TestAttachCategoryModifierGroup(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewAttachCategoryModifierGroupHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		groupID := createTestModifierGroupDirect(t, db, "Sugar Level", 0, 1, false)

		reqID := uuid.New()
		cmd := catalog.AttachCategoryModifierGroupCommand{
			RequestID:       reqID,
			CategoryID:      catID,
			ModifierGroupID: groupID,
		}

		status, res, err := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.Equal(t, catID, res.CategoryID)
		assert.Equal(t, groupID, res.ModifierGroupID)

		// Verify database row in category_modifier_groups
		var count int
		err = db.QueryRowContext(ctx, `SELECT count(*) FROM category_modifier_groups WHERE menu_category_id = $1 AND modifier_group_id = $2`, catID, groupID).Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 1, count)

		// Verify audit event
		var eventType string
		var detailsJSON []byte
		err = db.QueryRowContext(ctx, `SELECT event_type, details FROM audit_events WHERE event_type = $1`, catalog.EventCategoryModifierGroupAttached).Scan(&eventType, &detailsJSON)
		require.NoError(t, err)
		assert.Equal(t, catalog.EventCategoryModifierGroupAttached, eventType)

		var details map[string]any
		err = json.Unmarshal(detailsJSON, &details)
		require.NoError(t, err)
		assert.Equal(t, catID.String(), details["category_id"])
		assert.Equal(t, groupID.String(), details["modifier_group_id"])
	})

	t.Run("Reject_CategoryNotFound", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewAttachCategoryModifierGroupHandler(runner)
		groupID := createTestModifierGroupDirect(t, db, "Sugar Level", 0, 1, false)

		cmd := catalog.AttachCategoryModifierGroupCommand{
			RequestID:       uuid.New(),
			CategoryID:      uuid.New(),
			ModifierGroupID: groupID,
		}
		status, _, err := handler.Handle(ctx, actor, cmd)
		require.Error(t, err)
		assert.ErrorIs(t, err, catalog.ErrNotFound)
		assert.Equal(t, 0, status)
	})

	t.Run("Reject_ModifierGroupNotFound", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewAttachCategoryModifierGroupHandler(runner)
		catID := createTestCategoryDirect(t, db, "Coffee")

		cmd := catalog.AttachCategoryModifierGroupCommand{
			RequestID:       uuid.New(),
			CategoryID:      catID,
			ModifierGroupID: uuid.New(),
		}
		status, _, err := handler.Handle(ctx, actor, cmd)
		require.Error(t, err)
		assert.ErrorIs(t, err, catalog.ErrNotFound)
		assert.Equal(t, 0, status)
	})

	t.Run("Reject_RetiredModifierGroup", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewAttachCategoryModifierGroupHandler(runner)
		catID := createTestCategoryDirect(t, db, "Coffee")
		groupID := createTestModifierGroupDirect(t, db, "Retired Group", 0, 1, true)

		cmd := catalog.AttachCategoryModifierGroupCommand{
			RequestID:       uuid.New(),
			CategoryID:      catID,
			ModifierGroupID: groupID,
		}
		status, _, err := handler.Handle(ctx, actor, cmd)
		require.Error(t, err)
		assert.ErrorIs(t, err, catalog.ErrEntityRetired)
		assert.Equal(t, 0, status)
	})

	t.Run("Reject_DuplicateAttachment", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewAttachCategoryModifierGroupHandler(runner)
		catID := createTestCategoryDirect(t, db, "Coffee")
		groupID := createTestModifierGroupDirect(t, db, "Sugar Level", 0, 1, false)

		cmd1 := catalog.AttachCategoryModifierGroupCommand{
			RequestID:       uuid.New(),
			CategoryID:      catID,
			ModifierGroupID: groupID,
		}
		status1, _, err := handler.Handle(ctx, actor, cmd1)
		require.NoError(t, err)
		assert.Equal(t, 200, status1)

		cmd2 := catalog.AttachCategoryModifierGroupCommand{
			RequestID:       uuid.New(),
			CategoryID:      catID,
			ModifierGroupID: groupID,
		}
		status2, _, err := handler.Handle(ctx, actor, cmd2)
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrNameConflict), "duplicate attachment should return conflict error, got: %v", err)
		assert.Equal(t, 0, status2)
	})

	t.Run("ExactReplay_And_Conflict", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewAttachCategoryModifierGroupHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		groupID := createTestModifierGroupDirect(t, db, "Sugar Level", 0, 1, false)
		groupID2 := createTestModifierGroupDirect(t, db, "Milk Choice", 0, 1, false)

		reqID := uuid.New()
		cmd := catalog.AttachCategoryModifierGroupCommand{
			RequestID:       reqID,
			CategoryID:      catID,
			ModifierGroupID: groupID,
		}
		status1, res1, err := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		assert.Equal(t, 200, status1)

		// Exact replay
		status2, res2, err := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		assert.Equal(t, 200, status2)
		assert.Equal(t, res1, res2)

		// Audit not duplicated
		var auditCount int
		err = db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE event_type = $1`, catalog.EventCategoryModifierGroupAttached).Scan(&auditCount)
		require.NoError(t, err)
		assert.Equal(t, 1, auditCount)

		// Conflict
		conflictCmd := catalog.AttachCategoryModifierGroupCommand{
			RequestID:       reqID,
			CategoryID:      catID,
			ModifierGroupID: groupID2,
		}
		_, _, err = handler.Handle(ctx, actor, conflictCmd)
		require.Error(t, err)
		assert.ErrorIs(t, err, catalog.ErrRequestConflict)
	})

	t.Run("Forbidden_MissingCapabilities", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		cashier := createCatalogTestIdentity(t, db, q, []string{auth.RoleCashier}, true, "1234")
		actor := catalog.Actor{StaffID: cashier.StaffID, SessionID: cashier.SessionID}
		handler := catalog.NewAttachCategoryModifierGroupHandler(runner)
		catID := createTestCategoryDirect(t, db, "Coffee")
		groupID := createTestModifierGroupDirect(t, db, "Sugar Level", 0, 1, false)

		cmd := catalog.AttachCategoryModifierGroupCommand{
			RequestID:       uuid.New(),
			CategoryID:      catID,
			ModifierGroupID: groupID,
		}
		_, _, err := handler.Handle(ctx, actor, cmd)
		require.Error(t, err)
		assert.ErrorIs(t, err, catalog.ErrForbidden)
	})
}

func TestExcludeInheritedModifierGroup(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewExcludeInheritedModifierGroupHandler(runner)

		catID := createTestCategoryDirect(t, db, "Tea")
		price := int64(25000)
		itemID := createTestItemDirect(t, db, catID, "Green Tea", &price, false)
		groupID := createTestModifierGroupDirect(t, db, "Ice Level", 0, 1, false)

		// Attach group to category
		attachCatHandler := catalog.NewAttachCategoryModifierGroupHandler(runner)
		_, _, err := attachCatHandler.Handle(ctx, actor, catalog.AttachCategoryModifierGroupCommand{
			RequestID:       uuid.New(),
			CategoryID:      catID,
			ModifierGroupID: groupID,
		})
		require.NoError(t, err)

		// Exclude from item
		reqID := uuid.New()
		cmd := catalog.ExcludeInheritedModifierGroupCommand{
			RequestID:       reqID,
			ItemID:          itemID,
			ModifierGroupID: groupID,
		}
		status, res, err := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.Equal(t, itemID, res.ItemID)
		assert.Equal(t, groupID, res.ModifierGroupID)

		// Verify database row in item_modifier_group_exclusions
		var count int
		err = db.QueryRowContext(ctx, `SELECT count(*) FROM item_modifier_group_exclusions WHERE menu_item_id = $1 AND modifier_group_id = $2`, itemID, groupID).Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 1, count)

		// Verify audit event
		var eventType string
		var detailsJSON []byte
		err = db.QueryRowContext(ctx, `SELECT event_type, details FROM audit_events WHERE event_type = $1`, catalog.EventItemInheritedModifierGroupExcluded).Scan(&eventType, &detailsJSON)
		require.NoError(t, err)
		assert.Equal(t, catalog.EventItemInheritedModifierGroupExcluded, eventType)

		var details map[string]any
		err = json.Unmarshal(detailsJSON, &details)
		require.NoError(t, err)
		assert.Equal(t, itemID.String(), details["item_id"])
		assert.Equal(t, groupID.String(), details["modifier_group_id"])
	})

	t.Run("Reject_ItemNotFound", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewExcludeInheritedModifierGroupHandler(runner)
		groupID := createTestModifierGroupDirect(t, db, "Ice", 0, 1, false)

		cmd := catalog.ExcludeInheritedModifierGroupCommand{
			RequestID:       uuid.New(),
			ItemID:          uuid.New(),
			ModifierGroupID: groupID,
		}
		status, _, err := handler.Handle(ctx, actor, cmd)
		require.Error(t, err)
		assert.ErrorIs(t, err, catalog.ErrNotFound)
		assert.Equal(t, 0, status)
	})

	t.Run("Reject_ModifierGroupNotFound", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewExcludeInheritedModifierGroupHandler(runner)
		catID := createTestCategoryDirect(t, db, "Tea")
		price := int64(25000)
		itemID := createTestItemDirect(t, db, catID, "Green Tea", &price, false)

		cmd := catalog.ExcludeInheritedModifierGroupCommand{
			RequestID:       uuid.New(),
			ItemID:          itemID,
			ModifierGroupID: uuid.New(),
		}
		status, _, err := handler.Handle(ctx, actor, cmd)
		require.Error(t, err)
		assert.ErrorIs(t, err, catalog.ErrNotFound)
		assert.Equal(t, 0, status)
	})

	t.Run("Reject_RetiredItem", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewExcludeInheritedModifierGroupHandler(runner)
		catID := createTestCategoryDirect(t, db, "Tea")
		price := int64(25000)
		itemID := createTestItemDirect(t, db, catID, "Retired Green Tea", &price, true)
		groupID := createTestModifierGroupDirect(t, db, "Ice", 0, 1, false)

		cmd := catalog.ExcludeInheritedModifierGroupCommand{
			RequestID:       uuid.New(),
			ItemID:          itemID,
			ModifierGroupID: groupID,
		}
		status, _, err := handler.Handle(ctx, actor, cmd)
		require.Error(t, err)
		assert.ErrorIs(t, err, catalog.ErrEntityRetired)
		assert.Equal(t, 0, status)
	})

	t.Run("Reject_RetiredModifierGroup", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewExcludeInheritedModifierGroupHandler(runner)
		catID := createTestCategoryDirect(t, db, "Tea")
		price := int64(25000)
		itemID := createTestItemDirect(t, db, catID, "Green Tea", &price, false)
		groupID := createTestModifierGroupDirect(t, db, "Retired Ice", 0, 1, true)

		cmd := catalog.ExcludeInheritedModifierGroupCommand{
			RequestID:       uuid.New(),
			ItemID:          itemID,
			ModifierGroupID: groupID,
		}
		status, _, err := handler.Handle(ctx, actor, cmd)
		require.Error(t, err)
		assert.ErrorIs(t, err, catalog.ErrEntityRetired)
		assert.Equal(t, 0, status)
	})

	t.Run("Reject_NotAssignedToCategory", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewExcludeInheritedModifierGroupHandler(runner)

		catID := createTestCategoryDirect(t, db, "Tea")
		price := int64(25000)
		itemID := createTestItemDirect(t, db, catID, "Green Tea", &price, false)
		groupID := createTestModifierGroupDirect(t, db, "Topping", 0, 2, false)
		// Group is NOT assigned to category "Tea"

		cmd := catalog.ExcludeInheritedModifierGroupCommand{
			RequestID:       uuid.New(),
			ItemID:          itemID,
			ModifierGroupID: groupID,
		}
		status, _, err := handler.Handle(ctx, actor, cmd)
		require.Error(t, err)
		assert.ErrorIs(t, err, catalog.ErrInvalidInheritance)
		assert.Equal(t, 0, status)
	})

	t.Run("Reject_DuplicateExclusion", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewExcludeInheritedModifierGroupHandler(runner)

		catID := createTestCategoryDirect(t, db, "Tea")
		price := int64(25000)
		itemID := createTestItemDirect(t, db, catID, "Green Tea", &price, false)
		groupID := createTestModifierGroupDirect(t, db, "Ice Level", 0, 1, false)

		attachCatHandler := catalog.NewAttachCategoryModifierGroupHandler(runner)
		_, _, err := attachCatHandler.Handle(ctx, actor, catalog.AttachCategoryModifierGroupCommand{
			RequestID:       uuid.New(),
			CategoryID:      catID,
			ModifierGroupID: groupID,
		})
		require.NoError(t, err)

		cmd1 := catalog.ExcludeInheritedModifierGroupCommand{
			RequestID:       uuid.New(),
			ItemID:          itemID,
			ModifierGroupID: groupID,
		}
		status1, _, err := handler.Handle(ctx, actor, cmd1)
		require.NoError(t, err)
		assert.Equal(t, 200, status1)

		// Second exclusion with different request ID fails
		cmd2 := catalog.ExcludeInheritedModifierGroupCommand{
			RequestID:       uuid.New(),
			ItemID:          itemID,
			ModifierGroupID: groupID,
		}
		status2, _, err := handler.Handle(ctx, actor, cmd2)
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrNameConflict), "duplicate exclusion should return conflict error, got: %v", err)
		assert.Equal(t, 0, status2)
	})

	t.Run("DirectAssignmentSurvivesExclusion_And_Deduplication", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		excludeHandler := catalog.NewExcludeInheritedModifierGroupHandler(runner)
		attachItemHandler := catalog.NewAttachItemModifierGroupHandler(runner)
		attachCatHandler := catalog.NewAttachCategoryModifierGroupHandler(runner)

		catID := createTestCategoryDirect(t, db, "Tea")
		price := int64(25000)
		itemID := createTestItemDirect(t, db, catID, "Green Tea", &price, false)
		groupID := createTestModifierGroupDirect(t, db, "Ice Level", 0, 1, false)

		// 1. Attach group to category
		_, _, err := attachCatHandler.Handle(ctx, actor, catalog.AttachCategoryModifierGroupCommand{
			RequestID:       uuid.New(),
			CategoryID:      catID,
			ModifierGroupID: groupID,
		})
		require.NoError(t, err)

		// 2. Attach group directly to item
		_, _, err = attachItemHandler.Handle(ctx, actor, catalog.AttachItemModifierGroupCommand{
			RequestID:       uuid.New(),
			ItemID:          itemID,
			ModifierGroupID: groupID,
		})
		require.NoError(t, err)

		// Assert deduplication before exclusion: (inherited + direct) -> 1 instance
		effectiveBefore := catalog.EffectiveGroupIDs([]uuid.UUID{groupID}, nil, []uuid.UUID{groupID})
		assert.Equal(t, []uuid.UUID{groupID}, effectiveBefore, "inherited and direct group should deduplicate to 1")

		// 3. Exclude inherited group from item
		_, _, err = excludeHandler.Handle(ctx, actor, catalog.ExcludeInheritedModifierGroupCommand{
			RequestID:       uuid.New(),
			ItemID:          itemID,
			ModifierGroupID: groupID,
		})
		require.NoError(t, err)

		// Verify direct assignment still exists in DB
		var directCount int
		err = db.QueryRowContext(ctx, `SELECT count(*) FROM item_modifier_groups WHERE menu_item_id = $1 AND modifier_group_id = $2`, itemID, groupID).Scan(&directCount)
		require.NoError(t, err)
		assert.Equal(t, 1, directCount, "direct assignment must survive exclusion")

		// Verify exclusion exists in DB
		var exclusionCount int
		err = db.QueryRowContext(ctx, `SELECT count(*) FROM item_modifier_group_exclusions WHERE menu_item_id = $1 AND modifier_group_id = $2`, itemID, groupID).Scan(&exclusionCount)
		require.NoError(t, err)
		assert.Equal(t, 1, exclusionCount, "exclusion must be recorded")

		// Assert direct assignment survives exclusion in EffectiveGroupIDs computation
		effectiveAfter := catalog.EffectiveGroupIDs([]uuid.UUID{groupID}, []uuid.UUID{groupID}, []uuid.UUID{groupID})
		assert.Equal(t, []uuid.UUID{groupID}, effectiveAfter, "direct assignment must survive category exclusion in effective groups")
	})

	t.Run("ExactReplay_And_Conflict", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewExcludeInheritedModifierGroupHandler(runner)

		catID := createTestCategoryDirect(t, db, "Tea")
		price := int64(25000)
		itemID := createTestItemDirect(t, db, catID, "Green Tea", &price, false)
		groupID := createTestModifierGroupDirect(t, db, "Ice Level", 0, 1, false)
		groupID2 := createTestModifierGroupDirect(t, db, "Sugar Level", 0, 1, false)

		attachCatHandler := catalog.NewAttachCategoryModifierGroupHandler(runner)
		_, _, err := attachCatHandler.Handle(ctx, actor, catalog.AttachCategoryModifierGroupCommand{
			RequestID:       uuid.New(),
			CategoryID:      catID,
			ModifierGroupID: groupID,
		})
		require.NoError(t, err)

		reqID := uuid.New()
		cmd := catalog.ExcludeInheritedModifierGroupCommand{
			RequestID:       reqID,
			ItemID:          itemID,
			ModifierGroupID: groupID,
		}
		status1, res1, err := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		assert.Equal(t, 200, status1)

		// Replay
		status2, res2, err := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		assert.Equal(t, 200, status2)
		assert.Equal(t, res1, res2)

		// Audit not duplicated
		var auditCount int
		err = db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE event_type = $1`, catalog.EventItemInheritedModifierGroupExcluded).Scan(&auditCount)
		require.NoError(t, err)
		assert.Equal(t, 1, auditCount)

		// Conflict
		conflictCmd := catalog.ExcludeInheritedModifierGroupCommand{
			RequestID:       reqID,
			ItemID:          itemID,
			ModifierGroupID: groupID2,
		}
		_, _, err = handler.Handle(ctx, actor, conflictCmd)
		require.Error(t, err)
		assert.ErrorIs(t, err, catalog.ErrRequestConflict)
	})

	t.Run("Forbidden_MissingCapabilities", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		cashier := createCatalogTestIdentity(t, db, q, []string{auth.RoleCashier}, true, "1234")
		actor := catalog.Actor{StaffID: cashier.StaffID, SessionID: cashier.SessionID}
		handler := catalog.NewExcludeInheritedModifierGroupHandler(runner)

		catID := createTestCategoryDirect(t, db, "Tea")
		price := int64(25000)
		itemID := createTestItemDirect(t, db, catID, "Green Tea", &price, false)
		groupID := createTestModifierGroupDirect(t, db, "Ice Level", 0, 1, false)

		cmd := catalog.ExcludeInheritedModifierGroupCommand{
			RequestID:       uuid.New(),
			ItemID:          itemID,
			ModifierGroupID: groupID,
		}
		_, _, err := handler.Handle(ctx, actor, cmd)
		require.Error(t, err)
		assert.ErrorIs(t, err, catalog.ErrForbidden)
	})
}

func TestSetModifierGroupDefaults(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()

	t.Run("Success_WithDefaults", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewSetModifierGroupDefaultsHandler(runner)

		groupID := createTestModifierGroupDirect(t, db, "Topping", 1, 3, false)
		opt1 := createTestModifierOptionDirect(t, db, groupID, "Boba", 5000, true, false)
		opt2 := createTestModifierOptionDirect(t, db, groupID, "Pudding", 7000, true, false)
		opt3 := createTestModifierOptionDirect(t, db, groupID, "Jelly", 5000, true, false)
		_ = opt3

		// Set initial defaults
		reqID := uuid.New()
		cmd := catalog.SetModifierGroupDefaultsCommand{
			RequestID: reqID,
			GroupID:   groupID,
			OptionIDs: []uuid.UUID{opt1, opt2},
		}

		status, res, err := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.Equal(t, groupID, res.GroupID)
		assert.ElementsMatch(t, []uuid.UUID{opt1, opt2}, res.OptionIDs)

		// Verify database rows in modifier_group_default_options
		rows, err := db.QueryContext(ctx, `SELECT modifier_option_id FROM modifier_group_default_options WHERE modifier_group_id = $1`, groupID)
		require.NoError(t, err)
		defer rows.Close()
		var dbOpts []uuid.UUID
		for rows.Next() {
			var oid uuid.UUID
			require.NoError(t, rows.Scan(&oid))
			dbOpts = append(dbOpts, oid)
		}
		assert.ElementsMatch(t, []uuid.UUID{opt1, opt2}, dbOpts)

		// Verify audit event
		var eventType string
		var detailsJSON []byte
		err = db.QueryRowContext(ctx, `SELECT event_type, details FROM audit_events WHERE event_type = $1`, catalog.EventModifierGroupDefaultsChanged).Scan(&eventType, &detailsJSON)
		require.NoError(t, err)
		assert.Equal(t, catalog.EventModifierGroupDefaultsChanged, eventType)

		var details map[string]any
		err = json.Unmarshal(detailsJSON, &details)
		require.NoError(t, err)
		assert.Equal(t, groupID.String(), details["group_id"])
		optSlice, ok := details["option_ids"].([]any)
		require.True(t, ok)
		assert.Len(t, optSlice, 2)
	})

	t.Run("Success_EmptyDefaults_WhenMinZero", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewSetModifierGroupDefaultsHandler(runner)

		groupID := createTestModifierGroupDirect(t, db, "Ice Level", 0, 2, false)
		opt1 := createTestModifierOptionDirect(t, db, groupID, "Regular Ice", 0, true, false)

		// First set a default
		_, _, err := handler.Handle(ctx, actor, catalog.SetModifierGroupDefaultsCommand{
			RequestID: uuid.New(),
			GroupID:   groupID,
			OptionIDs: []uuid.UUID{opt1},
		})
		require.NoError(t, err)

		// Now replace with empty defaults
		cmd := catalog.SetModifierGroupDefaultsCommand{
			RequestID: uuid.New(),
			GroupID:   groupID,
			OptionIDs: []uuid.UUID{},
		}
		status, res, err := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.Equal(t, groupID, res.GroupID)
		assert.Empty(t, res.OptionIDs)

		// Verify 0 rows in modifier_group_default_options
		var count int
		err = db.QueryRowContext(ctx, `SELECT count(*) FROM modifier_group_default_options WHERE modifier_group_id = $1`, groupID).Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 0, count)
	})

	t.Run("Reject_GroupNotFound", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewSetModifierGroupDefaultsHandler(runner)

		cmd := catalog.SetModifierGroupDefaultsCommand{
			RequestID: uuid.New(),
			GroupID:   uuid.New(),
			OptionIDs: []uuid.UUID{uuid.New()},
		}
		status, _, err := handler.Handle(ctx, actor, cmd)
		require.Error(t, err)
		assert.ErrorIs(t, err, catalog.ErrNotFound)
		assert.Equal(t, 0, status)
	})

	t.Run("Reject_RetiredGroup", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewSetModifierGroupDefaultsHandler(runner)

		groupID := createTestModifierGroupDirect(t, db, "Retired Group", 0, 2, true)
		opt1 := createTestModifierOptionDirect(t, db, groupID, "Option", 0, true, false)

		cmd := catalog.SetModifierGroupDefaultsCommand{
			RequestID: uuid.New(),
			GroupID:   groupID,
			OptionIDs: []uuid.UUID{opt1},
		}
		status, _, err := handler.Handle(ctx, actor, cmd)
		require.Error(t, err)
		assert.ErrorIs(t, err, catalog.ErrEntityRetired)
		assert.Equal(t, 0, status)
	})

	t.Run("Reject_CardinalityTooLow", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewSetModifierGroupDefaultsHandler(runner)

		groupID := createTestModifierGroupDirect(t, db, "Size Choice", 2, 3, false)
		opt1 := createTestModifierOptionDirect(t, db, groupID, "M", 0, true, false)

		cmd := catalog.SetModifierGroupDefaultsCommand{
			RequestID: uuid.New(),
			GroupID:   groupID,
			OptionIDs: []uuid.UUID{opt1}, // 1 < min(2)
		}
		status, _, err := handler.Handle(ctx, actor, cmd)
		require.Error(t, err)
		assert.ErrorIs(t, err, catalog.ErrInvalidModifierConfiguration)
		assert.Equal(t, 0, status)
	})

	t.Run("Reject_CardinalityTooHigh", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewSetModifierGroupDefaultsHandler(runner)

		groupID := createTestModifierGroupDirect(t, db, "Ice Choice", 0, 1, false)
		opt1 := createTestModifierOptionDirect(t, db, groupID, "No Ice", 0, true, false)
		opt2 := createTestModifierOptionDirect(t, db, groupID, "Regular Ice", 0, true, false)

		cmd := catalog.SetModifierGroupDefaultsCommand{
			RequestID: uuid.New(),
			GroupID:   groupID,
			OptionIDs: []uuid.UUID{opt1, opt2}, // 2 > max(1)
		}
		status, _, err := handler.Handle(ctx, actor, cmd)
		require.Error(t, err)
		assert.ErrorIs(t, err, catalog.ErrInvalidModifierConfiguration)
		assert.Equal(t, 0, status)
	})

	t.Run("Reject_DuplicateOptionIDs", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewSetModifierGroupDefaultsHandler(runner)

		groupID := createTestModifierGroupDirect(t, db, "Topping", 1, 3, false)
		opt1 := createTestModifierOptionDirect(t, db, groupID, "Boba", 5000, true, false)

		cmd := catalog.SetModifierGroupDefaultsCommand{
			RequestID: uuid.New(),
			GroupID:   groupID,
			OptionIDs: []uuid.UUID{opt1, opt1}, // duplicate
		}
		status, _, err := handler.Handle(ctx, actor, cmd)
		require.Error(t, err)
		assert.ErrorIs(t, err, catalog.ErrInvalidModifierConfiguration)
		assert.Equal(t, 0, status)
	})

	t.Run("Reject_OptionNotBelongingToGroup", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewSetModifierGroupDefaultsHandler(runner)

		groupID1 := createTestModifierGroupDirect(t, db, "Group 1", 1, 2, false)
		_ = createTestModifierOptionDirect(t, db, groupID1, "G1 Opt", 0, true, false)

		groupID2 := createTestModifierGroupDirect(t, db, "Group 2", 1, 2, false)
		optFromOtherGroup := createTestModifierOptionDirect(t, db, groupID2, "G2 Opt", 0, true, false)

		cmd := catalog.SetModifierGroupDefaultsCommand{
			RequestID: uuid.New(),
			GroupID:   groupID1,
			OptionIDs: []uuid.UUID{optFromOtherGroup},
		}
		status, _, err := handler.Handle(ctx, actor, cmd)
		require.Error(t, err)
		assert.ErrorIs(t, err, catalog.ErrInvalidModifierConfiguration)
		assert.Equal(t, 0, status)
	})

	t.Run("Reject_UnavailableDefaultOption", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewSetModifierGroupDefaultsHandler(runner)

		groupID := createTestModifierGroupDirect(t, db, "Topping", 1, 2, false)
		unavailOpt := createTestModifierOptionDirect(t, db, groupID, "Out of Stock Boba", 5000, false, false)

		cmd := catalog.SetModifierGroupDefaultsCommand{
			RequestID: uuid.New(),
			GroupID:   groupID,
			OptionIDs: []uuid.UUID{unavailOpt},
		}
		status, _, err := handler.Handle(ctx, actor, cmd)
		require.Error(t, err)
		assert.ErrorIs(t, err, catalog.ErrInvalidModifierConfiguration)
		assert.Equal(t, 0, status)
	})

	t.Run("Reject_RetiredDefaultOption", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewSetModifierGroupDefaultsHandler(runner)

		groupID := createTestModifierGroupDirect(t, db, "Topping", 1, 2, false)
		retiredOpt := createTestModifierOptionDirect(t, db, groupID, "Discontinued Boba", 5000, true, true)

		cmd := catalog.SetModifierGroupDefaultsCommand{
			RequestID: uuid.New(),
			GroupID:   groupID,
			OptionIDs: []uuid.UUID{retiredOpt},
		}
		status, _, err := handler.Handle(ctx, actor, cmd)
		require.Error(t, err)
		assert.ErrorIs(t, err, catalog.ErrEntityRetired)
		assert.Equal(t, 0, status)
	})

	t.Run("AtomicReplacement_OnFailure", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewSetModifierGroupDefaultsHandler(runner)

		groupID := createTestModifierGroupDirect(t, db, "Topping", 1, 2, false)
		opt1 := createTestModifierOptionDirect(t, db, groupID, "Boba", 5000, true, false)
		opt2Unavailable := createTestModifierOptionDirect(t, db, groupID, "Unavailable Jelly", 5000, false, false)

		// Set good initial default
		_, _, err := handler.Handle(ctx, actor, catalog.SetModifierGroupDefaultsCommand{
			RequestID: uuid.New(),
			GroupID:   groupID,
			OptionIDs: []uuid.UUID{opt1},
		})
		require.NoError(t, err)

		// Attempt replacement with an unavailable option (fails)
		reqID := uuid.New()
		cmd := catalog.SetModifierGroupDefaultsCommand{
			RequestID: reqID,
			GroupID:   groupID,
			OptionIDs: []uuid.UUID{opt2Unavailable},
		}
		status, _, err := handler.Handle(ctx, actor, cmd)
		require.Error(t, err)
		assert.Equal(t, 0, status)

		// Assert initial default is still present in database
		var currentDefaults []uuid.UUID
		rows, err := db.QueryContext(ctx, `SELECT modifier_option_id FROM modifier_group_default_options WHERE modifier_group_id = $1`, groupID)
		require.NoError(t, err)
		defer rows.Close()
		for rows.Next() {
			var oid uuid.UUID
			require.NoError(t, rows.Scan(&oid))
			currentDefaults = append(currentDefaults, oid)
		}
		assert.Equal(t, []uuid.UUID{opt1}, currentDefaults, "original defaults must remain intact after failed replacement")
	})

	t.Run("ExactReplay_And_Conflict", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewSetModifierGroupDefaultsHandler(runner)

		groupID := createTestModifierGroupDirect(t, db, "Topping", 1, 2, false)
		opt1 := createTestModifierOptionDirect(t, db, groupID, "Boba", 5000, true, false)
		opt2 := createTestModifierOptionDirect(t, db, groupID, "Jelly", 5000, true, false)

		reqID := uuid.New()
		cmd := catalog.SetModifierGroupDefaultsCommand{
			RequestID: reqID,
			GroupID:   groupID,
			OptionIDs: []uuid.UUID{opt1},
		}
		status1, res1, err := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		assert.Equal(t, 200, status1)

		// Exact replay
		status2, res2, err := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		assert.Equal(t, 200, status2)
		assert.Equal(t, res1, res2)

		// Audit not duplicated
		var auditCount int
		err = db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE event_type = $1`, catalog.EventModifierGroupDefaultsChanged).Scan(&auditCount)
		require.NoError(t, err)
		assert.Equal(t, 1, auditCount)

		// Conflict replay with different option
		conflictCmd := catalog.SetModifierGroupDefaultsCommand{
			RequestID: reqID,
			GroupID:   groupID,
			OptionIDs: []uuid.UUID{opt2},
		}
		_, _, err = handler.Handle(ctx, actor, conflictCmd)
		require.Error(t, err)
		assert.ErrorIs(t, err, catalog.ErrRequestConflict)
	})

	t.Run("Forbidden_MissingCapabilities", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		cashier := createCatalogTestIdentity(t, db, q, []string{auth.RoleCashier}, true, "1234")
		actor := catalog.Actor{StaffID: cashier.StaffID, SessionID: cashier.SessionID}
		handler := catalog.NewSetModifierGroupDefaultsHandler(runner)

		groupID := createTestModifierGroupDirect(t, db, "Topping", 1, 2, false)
		opt1 := createTestModifierOptionDirect(t, db, groupID, "Boba", 5000, true, false)

		cmd := catalog.SetModifierGroupDefaultsCommand{
			RequestID: uuid.New(),
			GroupID:   groupID,
			OptionIDs: []uuid.UUID{opt1},
		}
		_, _, err := handler.Handle(ctx, actor, cmd)
		require.Error(t, err)
		assert.ErrorIs(t, err, catalog.ErrForbidden)
	})
}
