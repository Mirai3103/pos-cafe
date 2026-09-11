//go:build integration

package catalog_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/catalog"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestSizeDirect(t *testing.T, db *sql.DB, itemID uuid.UUID, name string, price int64, retired bool) uuid.UUID {
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
		INSERT INTO menu_item_sizes (menu_item_id, name, normalized_name, price_vnd, available, retired_at, retirement_reason, retirement_note)
		VALUES ($1, $2, $3, $4, true, $5, $6, $7)
		RETURNING id
	`, itemID, display, key, price, retiredAt, reason, note).Scan(&id)
	require.NoError(t, err)
	return id
}

// ============================================================================
// Rename Item Tests
// ============================================================================

func TestRenameItem(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()

	t.Run("Success_Trimming_InternalSpace_Audit_Timestamp", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRenameItemHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		price := int64(45000)
		itemID := createTestItemDirect(t, db, catID, "Caramel Macchiato", &price, false)

		var origCreatedAt, origUpdatedAt time.Time
		err := db.QueryRowContext(ctx, `SELECT created_at, updated_at FROM menu_items WHERE id = $1`, itemID).Scan(&origCreatedAt, &origUpdatedAt)
		require.NoError(t, err)

		reqID := uuid.New()
		inputName := "   Iced   Caramel   Macchiato   "
		expectedDisplay := "Iced   Caramel   Macchiato"
		expectedKey := "iced   caramel   macchiato"

		cmd := catalog.RenameItemCommand{
			RequestID: reqID,
			ItemID:    itemID,
			Name:      inputName,
		}

		status, res, err := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.Equal(t, itemID, res.ID)
		assert.Equal(t, catID, res.CategoryID)
		assert.Equal(t, expectedDisplay, res.Name)
		require.NotNil(t, res.PriceVND)
		assert.Equal(t, price, *res.PriceVND)
		assert.True(t, res.Available)

		// Verify database row
		var dbName, dbNormalized string
		var dbUpdatedAt time.Time
		err = db.QueryRowContext(ctx, `SELECT name, normalized_name, updated_at FROM menu_items WHERE id = $1`, itemID).Scan(&dbName, &dbNormalized, &dbUpdatedAt)
		require.NoError(t, err)
		assert.Equal(t, expectedDisplay, dbName)
		assert.Equal(t, expectedKey, dbNormalized)
		assert.False(t, dbUpdatedAt.Before(origUpdatedAt), "updated_at should be updated")

		// Verify audit event
		var eventType string
		var actorID, sessionID uuid.UUID
		var detailsJSON []byte
		err = db.QueryRowContext(ctx, `
			SELECT event_type, actor_id, session_id, details
			FROM audit_events
			WHERE event_type = 'catalog.item.renamed' AND actor_id = $1
		`, actor.StaffID).Scan(&eventType, &actorID, &sessionID, &detailsJSON)
		require.NoError(t, err)
		assert.Equal(t, "catalog.item.renamed", eventType)
		assert.Equal(t, actor.StaffID, actorID)
		assert.Equal(t, actor.SessionID, sessionID)

		var details map[string]any
		err = json.Unmarshal(detailsJSON, &details)
		require.NoError(t, err)
		assert.Equal(t, itemID.String(), details["item_id"])
		assert.Equal(t, "Caramel Macchiato", details["old_name"])
		assert.Equal(t, expectedDisplay, details["new_name"])
	})

	t.Run("Success_SizedItem", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRenameItemHandler(runner)

		catID := createTestCategoryDirect(t, db, "Tea")
		itemID := createTestItemDirect(t, db, catID, "Milk Tea", nil, false)
		sizeS := createTestSizeDirect(t, db, itemID, "Small", 30000, false)
		sizeL := createTestSizeDirect(t, db, itemID, "Large", 40000, false)

		status, res, err := handler.Handle(ctx, actor, catalog.RenameItemCommand{
			RequestID: uuid.New(),
			ItemID:    itemID,
			Name:      "Signature Milk Tea",
		})
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.Equal(t, "Signature Milk Tea", res.Name)
		assert.Nil(t, res.PriceVND)
		require.Len(t, res.Sizes, 2)
		assert.Equal(t, sizeL, res.Sizes[0].ID) // ordered by normalized_name: "large" < "small"
		assert.Equal(t, sizeS, res.Sizes[1].ID)
	})

	t.Run("SameName_Auditing", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRenameItemHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		price := int64(35000)
		itemID := createTestItemDirect(t, db, catID, "Americano", &price, false)

		status, res, err := handler.Handle(ctx, actor, catalog.RenameItemCommand{
			RequestID: uuid.New(),
			ItemID:    itemID,
			Name:      "   Americano   ",
		})
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.Equal(t, "Americano", res.Name)

		var detailsJSON []byte
		err = db.QueryRowContext(ctx, `
			SELECT details FROM audit_events
			WHERE event_type = 'catalog.item.renamed' AND actor_id = $1
		`, actor.StaffID).Scan(&detailsJSON)
		require.NoError(t, err)

		var details map[string]any
		err = json.Unmarshal(detailsJSON, &details)
		require.NoError(t, err)
		assert.Equal(t, "Americano", details["old_name"])
		assert.Equal(t, "Americano", details["new_name"])
	})

	t.Run("ScopedConflict_SameCategory", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRenameItemHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		price := int64(30000)
		_ = createTestItemDirect(t, db, catID, "Latte", &price, false)
		item2 := createTestItemDirect(t, db, catID, "Mocha", &price, false)

		status, _, err := handler.Handle(ctx, actor, catalog.RenameItemCommand{
			RequestID: uuid.New(),
			ItemID:    item2,
			Name:      "   lAtTe   ",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrNameConflict), "expected ErrNameConflict, got: %v", err)
		assert.Equal(t, 0, status)
	})

	t.Run("ScopedConflict_DifferentCategory_Allowed", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRenameItemHandler(runner)

		cat1 := createTestCategoryDirect(t, db, "Hot Coffee")
		cat2 := createTestCategoryDirect(t, db, "Cold Coffee")
		price := int64(30000)
		_ = createTestItemDirect(t, db, cat1, "Americano", &price, false)
		item2 := createTestItemDirect(t, db, cat2, "Espresso", &price, false)

		// Rename item2 in cat2 to "Americano" - allowed since category is different
		status, res, err := handler.Handle(ctx, actor, catalog.RenameItemCommand{
			RequestID: uuid.New(),
			ItemID:    item2,
			Name:      "Americano",
		})
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.Equal(t, "Americano", res.Name)
	})

	t.Run("TargetRetirement", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRenameItemHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		price := int64(30000)
		retiredItem := createTestItemDirect(t, db, catID, "Old Brew", &price, true)

		status, _, err := handler.Handle(ctx, actor, catalog.RenameItemCommand{
			RequestID: uuid.New(),
			ItemID:    retiredItem,
			Name:      "New Brew",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrEntityRetired), "expected ErrEntityRetired, got: %v", err)
		assert.Equal(t, 0, status)
	})

	t.Run("NotFound", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRenameItemHandler(runner)

		status, _, err := handler.Handle(ctx, actor, catalog.RenameItemCommand{
			RequestID: uuid.New(),
			ItemID:    uuid.New(),
			Name:      "Ghost Item",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrNotFound), "expected ErrNotFound, got: %v", err)
		assert.Equal(t, 0, status)
	})

	t.Run("Replay", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRenameItemHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		price := int64(30000)
		itemID := createTestItemDirect(t, db, catID, "Original", &price, false)

		cmd := catalog.RenameItemCommand{
			RequestID: uuid.New(),
			ItemID:    itemID,
			Name:      "Renamed",
		}

		status1, res1, err1 := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err1)
		assert.Equal(t, 200, status1)

		status2, res2, err2 := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err2)
		assert.Equal(t, 200, status2)
		assert.Equal(t, res1, res2)

		var auditCount int
		err := db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE event_type = 'catalog.item.renamed'`).Scan(&auditCount)
		require.NoError(t, err)
		assert.Equal(t, 1, auditCount)
	})

	t.Run("Forbidden_MissingCapabilities", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		catID := createTestCategoryDirect(t, db, "Coffee")
		price := int64(30000)
		itemID := createTestItemDirect(t, db, catID, "Original", &price, false)

		cashier := createCatalogTestIdentity(t, db, q, []string{auth.RoleCashier}, true, "5678")
		handler := catalog.NewRenameItemHandler(runner)

		status, _, err := handler.Handle(ctx, catalog.Actor{StaffID: cashier.StaffID, SessionID: cashier.SessionID}, catalog.RenameItemCommand{
			RequestID: uuid.New(),
			ItemID:    itemID,
			Name:      "Hacked Name",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrForbidden))
		assert.Equal(t, 0, status)
		assert.Equal(t, 1, countAuthorizationDenials(t, db, "catalog.item.rename"))
	})
}

// ============================================================================
// Rename Size Tests
// ============================================================================

func TestRenameSize(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()

	t.Run("Success_Trimming_InternalSpace_Audit_Timestamp", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRenameSizeHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		itemID := createTestItemDirect(t, db, catID, "Latte", nil, false)
		sizeID := createTestSizeDirect(t, db, itemID, "Medium", 35000, false)

		var origUpdatedAt time.Time
		err := db.QueryRowContext(ctx, `SELECT updated_at FROM menu_item_sizes WHERE id = $1`, sizeID).Scan(&origUpdatedAt)
		require.NoError(t, err)

		reqID := uuid.New()
		inputName := "   Extra   Large   "
		expectedDisplay := "Extra   Large"
		expectedKey := "extra   large"

		cmd := catalog.RenameSizeCommand{
			RequestID: reqID,
			SizeID:    sizeID,
			Name:      inputName,
		}

		status, res, err := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.Equal(t, sizeID, res.ID)
		assert.Equal(t, expectedDisplay, res.Name)
		assert.Equal(t, int64(35000), res.PriceVND)
		assert.True(t, res.Available)

		// Verify database row
		var dbName, dbNormalized string
		var dbUpdatedAt time.Time
		err = db.QueryRowContext(ctx, `SELECT name, normalized_name, updated_at FROM menu_item_sizes WHERE id = $1`, sizeID).Scan(&dbName, &dbNormalized, &dbUpdatedAt)
		require.NoError(t, err)
		assert.Equal(t, expectedDisplay, dbName)
		assert.Equal(t, expectedKey, dbNormalized)
		assert.False(t, dbUpdatedAt.Before(origUpdatedAt), "updated_at should be updated")

		// Verify audit event
		var eventType string
		var actorID, sessionID uuid.UUID
		var detailsJSON []byte
		err = db.QueryRowContext(ctx, `
			SELECT event_type, actor_id, session_id, details
			FROM audit_events
			WHERE event_type = 'catalog.size.renamed' AND actor_id = $1
		`, actor.StaffID).Scan(&eventType, &actorID, &sessionID, &detailsJSON)
		require.NoError(t, err)
		assert.Equal(t, "catalog.size.renamed", eventType)
		assert.Equal(t, actor.StaffID, actorID)
		assert.Equal(t, actor.SessionID, sessionID)

		var details map[string]any
		err = json.Unmarshal(detailsJSON, &details)
		require.NoError(t, err)
		assert.Equal(t, sizeID.String(), details["size_id"])
		assert.Equal(t, itemID.String(), details["menu_item_id"])
		assert.Equal(t, "Medium", details["old_name"])
		assert.Equal(t, expectedDisplay, details["new_name"])
	})

	t.Run("SameName_Auditing", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRenameSizeHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		itemID := createTestItemDirect(t, db, catID, "Latte", nil, false)
		sizeID := createTestSizeDirect(t, db, itemID, "Regular", 35000, false)

		status, res, err := handler.Handle(ctx, actor, catalog.RenameSizeCommand{
			RequestID: uuid.New(),
			SizeID:    sizeID,
			Name:      "   Regular   ",
		})
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.Equal(t, "Regular", res.Name)

		var detailsJSON []byte
		err = db.QueryRowContext(ctx, `
			SELECT details FROM audit_events
			WHERE event_type = 'catalog.size.renamed' AND actor_id = $1
		`, actor.StaffID).Scan(&detailsJSON)
		require.NoError(t, err)

		var details map[string]any
		err = json.Unmarshal(detailsJSON, &details)
		require.NoError(t, err)
		assert.Equal(t, "Regular", details["old_name"])
		assert.Equal(t, "Regular", details["new_name"])
	})

	t.Run("ScopedConflict_SameItem", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRenameSizeHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		itemID := createTestItemDirect(t, db, catID, "Latte", nil, false)
		_ = createTestSizeDirect(t, db, itemID, "Small", 30000, false)
		size2 := createTestSizeDirect(t, db, itemID, "Medium", 35000, false)

		status, _, err := handler.Handle(ctx, actor, catalog.RenameSizeCommand{
			RequestID: uuid.New(),
			SizeID:    size2,
			Name:      "   sMaLl   ",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrNameConflict), "expected ErrNameConflict, got: %v", err)
		assert.Equal(t, 0, status)
	})

	t.Run("ScopedConflict_DifferentItem_Allowed", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRenameSizeHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		item1 := createTestItemDirect(t, db, catID, "Latte", nil, false)
		item2 := createTestItemDirect(t, db, catID, "Cappuccino", nil, false)
		_ = createTestSizeDirect(t, db, item1, "Small", 30000, false)
		size2 := createTestSizeDirect(t, db, item2, "Mini", 32000, false)

		// Rename size in item2 to "Small" - allowed because scoped to item
		status, res, err := handler.Handle(ctx, actor, catalog.RenameSizeCommand{
			RequestID: uuid.New(),
			SizeID:    size2,
			Name:      "Small",
		})
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.Equal(t, "Small", res.Name)
	})

	t.Run("ParentRetirement", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRenameSizeHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		retiredItem := createTestItemDirect(t, db, catID, "Retired Latte", nil, true)
		activeSize := createTestSizeDirect(t, db, retiredItem, "Regular", 35000, false)

		status, _, err := handler.Handle(ctx, actor, catalog.RenameSizeCommand{
			RequestID: uuid.New(),
			SizeID:    activeSize,
			Name:      "Large",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrEntityRetired), "expected ErrEntityRetired, got: %v", err)
		assert.Equal(t, 0, status)
	})

	t.Run("TargetRetirement", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRenameSizeHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		activeItem := createTestItemDirect(t, db, catID, "Active Latte", nil, false)
		retiredSize := createTestSizeDirect(t, db, activeItem, "Retired Size", 35000, true)

		status, _, err := handler.Handle(ctx, actor, catalog.RenameSizeCommand{
			RequestID: uuid.New(),
			SizeID:    retiredSize,
			Name:      "New Size",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrEntityRetired), "expected ErrEntityRetired, got: %v", err)
		assert.Equal(t, 0, status)
	})

	t.Run("NotFound", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRenameSizeHandler(runner)

		status, _, err := handler.Handle(ctx, actor, catalog.RenameSizeCommand{
			RequestID: uuid.New(),
			SizeID:    uuid.New(),
			Name:      "Ghost Size",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrNotFound), "expected ErrNotFound, got: %v", err)
		assert.Equal(t, 0, status)
	})

	t.Run("Replay", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRenameSizeHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		itemID := createTestItemDirect(t, db, catID, "Latte", nil, false)
		sizeID := createTestSizeDirect(t, db, itemID, "Medium", 35000, false)

		cmd := catalog.RenameSizeCommand{
			RequestID: uuid.New(),
			SizeID:    sizeID,
			Name:      "Large",
		}

		status1, res1, err1 := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err1)
		assert.Equal(t, 200, status1)

		status2, res2, err2 := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err2)
		assert.Equal(t, 200, status2)
		assert.Equal(t, res1, res2)

		var auditCount int
		err := db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE event_type = 'catalog.size.renamed'`).Scan(&auditCount)
		require.NoError(t, err)
		assert.Equal(t, 1, auditCount)
	})

	t.Run("Forbidden_MissingCapabilities", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		catID := createTestCategoryDirect(t, db, "Coffee")
		itemID := createTestItemDirect(t, db, catID, "Latte", nil, false)
		sizeID := createTestSizeDirect(t, db, itemID, "Medium", 35000, false)

		cashier := createCatalogTestIdentity(t, db, q, []string{auth.RoleCashier}, true, "5678")
		handler := catalog.NewRenameSizeHandler(runner)

		status, _, err := handler.Handle(ctx, catalog.Actor{StaffID: cashier.StaffID, SessionID: cashier.SessionID}, catalog.RenameSizeCommand{
			RequestID: uuid.New(),
			SizeID:    sizeID,
			Name:      "Hacked Size",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrForbidden))
		assert.Equal(t, 0, status)
		assert.Equal(t, 1, countAuthorizationDenials(t, db, "catalog.size.rename"))
	})
}

// ============================================================================
// Rename Modifier Group Tests
// ============================================================================

func TestRenameModifierGroup(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()

	t.Run("Success_Trimming_InternalSpace_Audit_Timestamp", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRenameModifierGroupHandler(runner)

		groupID := createTestModifierGroupDirect(t, db, "Ice", 0, 1, false)
		optID := createTestModifierOptionDirect(t, db, groupID, "Normal Ice", 0, true, false)

		var origUpdatedAt time.Time
		err := db.QueryRowContext(ctx, `SELECT updated_at FROM modifier_groups WHERE id = $1`, groupID).Scan(&origUpdatedAt)
		require.NoError(t, err)

		reqID := uuid.New()
		inputName := "   Ice   Level   "
		expectedDisplay := "Ice   Level"
		expectedKey := "ice   level"

		cmd := catalog.RenameModifierGroupCommand{
			RequestID: reqID,
			GroupID:   groupID,
			Name:      inputName,
		}

		status, res, err := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.Equal(t, groupID, res.ID)
		assert.Equal(t, expectedDisplay, res.Name)
		assert.Equal(t, int32(0), res.MinSelections)
		assert.Equal(t, int32(1), res.MaxSelections)
		require.Len(t, res.Options, 1)
		assert.Equal(t, optID, res.Options[0].ID)

		// Verify database row
		var dbName, dbNormalized string
		var dbUpdatedAt time.Time
		err = db.QueryRowContext(ctx, `SELECT name, normalized_name, updated_at FROM modifier_groups WHERE id = $1`, groupID).Scan(&dbName, &dbNormalized, &dbUpdatedAt)
		require.NoError(t, err)
		assert.Equal(t, expectedDisplay, dbName)
		assert.Equal(t, expectedKey, dbNormalized)
		assert.False(t, dbUpdatedAt.Before(origUpdatedAt), "updated_at should be updated")

		// Verify audit event
		var eventType string
		var actorID, sessionID uuid.UUID
		var detailsJSON []byte
		err = db.QueryRowContext(ctx, `
			SELECT event_type, actor_id, session_id, details
			FROM audit_events
			WHERE event_type = 'catalog.modifier_group.renamed' AND actor_id = $1
		`, actor.StaffID).Scan(&eventType, &actorID, &sessionID, &detailsJSON)
		require.NoError(t, err)
		assert.Equal(t, "catalog.modifier_group.renamed", eventType)
		assert.Equal(t, actor.StaffID, actorID)
		assert.Equal(t, actor.SessionID, sessionID)

		var details map[string]any
		err = json.Unmarshal(detailsJSON, &details)
		require.NoError(t, err)
		assert.Equal(t, groupID.String(), details["group_id"])
		assert.Equal(t, "Ice", details["old_name"])
		assert.Equal(t, expectedDisplay, details["new_name"])
	})

	t.Run("SameName_Auditing", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRenameModifierGroupHandler(runner)

		groupID := createTestModifierGroupDirect(t, db, "Sweetness", 0, 1, false)

		status, res, err := handler.Handle(ctx, actor, catalog.RenameModifierGroupCommand{
			RequestID: uuid.New(),
			GroupID:   groupID,
			Name:      "   Sweetness   ",
		})
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.Equal(t, "Sweetness", res.Name)

		var detailsJSON []byte
		err = db.QueryRowContext(ctx, `
			SELECT details FROM audit_events
			WHERE event_type = 'catalog.modifier_group.renamed' AND actor_id = $1
		`, actor.StaffID).Scan(&detailsJSON)
		require.NoError(t, err)

		var details map[string]any
		err = json.Unmarshal(detailsJSON, &details)
		require.NoError(t, err)
		assert.Equal(t, "Sweetness", details["old_name"])
		assert.Equal(t, "Sweetness", details["new_name"])
	})

	t.Run("GlobalConflict", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRenameModifierGroupHandler(runner)

		_ = createTestModifierGroupDirect(t, db, "Ice", 0, 1, false)
		group2 := createTestModifierGroupDirect(t, db, "Sugar", 0, 1, false)

		status, _, err := handler.Handle(ctx, actor, catalog.RenameModifierGroupCommand{
			RequestID: uuid.New(),
			GroupID:   group2,
			Name:      "   iCe   ",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrNameConflict), "expected ErrNameConflict, got: %v", err)
		assert.Equal(t, 0, status)
	})

	t.Run("TargetRetirement", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRenameModifierGroupHandler(runner)

		retiredGroup := createTestModifierGroupDirect(t, db, "Old Group", 0, 1, true)

		status, _, err := handler.Handle(ctx, actor, catalog.RenameModifierGroupCommand{
			RequestID: uuid.New(),
			GroupID:   retiredGroup,
			Name:      "New Group",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrEntityRetired), "expected ErrEntityRetired, got: %v", err)
		assert.Equal(t, 0, status)
	})

	t.Run("NotFound", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRenameModifierGroupHandler(runner)

		status, _, err := handler.Handle(ctx, actor, catalog.RenameModifierGroupCommand{
			RequestID: uuid.New(),
			GroupID:   uuid.New(),
			Name:      "Ghost Group",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrNotFound), "expected ErrNotFound, got: %v", err)
		assert.Equal(t, 0, status)
	})

	t.Run("Replay", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRenameModifierGroupHandler(runner)

		groupID := createTestModifierGroupDirect(t, db, "Original Group", 0, 1, false)

		cmd := catalog.RenameModifierGroupCommand{
			RequestID: uuid.New(),
			GroupID:   groupID,
			Name:      "Renamed Group",
		}

		status1, res1, err1 := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err1)
		assert.Equal(t, 200, status1)

		status2, res2, err2 := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err2)
		assert.Equal(t, 200, status2)
		assert.Equal(t, res1, res2)

		var auditCount int
		err := db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE event_type = 'catalog.modifier_group.renamed'`).Scan(&auditCount)
		require.NoError(t, err)
		assert.Equal(t, 1, auditCount)
	})

	t.Run("Forbidden_MissingCapabilities", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		groupID := createTestModifierGroupDirect(t, db, "Original Group", 0, 1, false)

		cashier := createCatalogTestIdentity(t, db, q, []string{auth.RoleCashier}, true, "5678")
		handler := catalog.NewRenameModifierGroupHandler(runner)

		status, _, err := handler.Handle(ctx, catalog.Actor{StaffID: cashier.StaffID, SessionID: cashier.SessionID}, catalog.RenameModifierGroupCommand{
			RequestID: uuid.New(),
			GroupID:   groupID,
			Name:      "Hacked Group",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrForbidden))
		assert.Equal(t, 0, status)
		assert.Equal(t, 1, countAuthorizationDenials(t, db, "catalog.modifier_group.rename"))
	})
}

// ============================================================================
// Rename Modifier Option Tests
// ============================================================================

func TestRenameModifierOption(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()

	t.Run("Success_Trimming_InternalSpace_Audit_Timestamp", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRenameModifierOptionHandler(runner)

		groupID := createTestModifierGroupDirect(t, db, "Sweetness", 0, 1, false)
		optID := createTestModifierOptionDirect(t, db, groupID, "Less Sweet", 0, true, false)

		var origUpdatedAt time.Time
		err := db.QueryRowContext(ctx, `SELECT updated_at FROM modifier_options WHERE id = $1`, optID).Scan(&origUpdatedAt)
		require.NoError(t, err)

		reqID := uuid.New()
		inputName := "   Half   Sweet   "
		expectedDisplay := "Half   Sweet"
		expectedKey := "half   sweet"

		cmd := catalog.RenameModifierOptionCommand{
			RequestID: reqID,
			OptionID:  optID,
			Name:      inputName,
		}

		status, res, err := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.Equal(t, optID, res.ID)
		assert.Equal(t, groupID, res.ModifierGroupID)
		assert.Equal(t, expectedDisplay, res.Name)
		assert.Equal(t, int64(0), res.SurchargeVND)
		assert.True(t, res.Available)

		// Verify database row
		var dbName, dbNormalized string
		var dbUpdatedAt time.Time
		err = db.QueryRowContext(ctx, `SELECT name, normalized_name, updated_at FROM modifier_options WHERE id = $1`, optID).Scan(&dbName, &dbNormalized, &dbUpdatedAt)
		require.NoError(t, err)
		assert.Equal(t, expectedDisplay, dbName)
		assert.Equal(t, expectedKey, dbNormalized)
		assert.False(t, dbUpdatedAt.Before(origUpdatedAt), "updated_at should be updated")

		// Verify audit event
		var eventType string
		var actorID, sessionID uuid.UUID
		var detailsJSON []byte
		err = db.QueryRowContext(ctx, `
			SELECT event_type, actor_id, session_id, details
			FROM audit_events
			WHERE event_type = 'catalog.modifier_option.renamed' AND actor_id = $1
		`, actor.StaffID).Scan(&eventType, &actorID, &sessionID, &detailsJSON)
		require.NoError(t, err)
		assert.Equal(t, "catalog.modifier_option.renamed", eventType)
		assert.Equal(t, actor.StaffID, actorID)
		assert.Equal(t, actor.SessionID, sessionID)

		var details map[string]any
		err = json.Unmarshal(detailsJSON, &details)
		require.NoError(t, err)
		assert.Equal(t, optID.String(), details["option_id"])
		assert.Equal(t, groupID.String(), details["modifier_group_id"])
		assert.Equal(t, "Less Sweet", details["old_name"])
		assert.Equal(t, expectedDisplay, details["new_name"])
	})

	t.Run("SameName_Auditing", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRenameModifierOptionHandler(runner)

		groupID := createTestModifierGroupDirect(t, db, "Ice", 0, 1, false)
		optID := createTestModifierOptionDirect(t, db, groupID, "No Ice", 0, true, false)

		status, res, err := handler.Handle(ctx, actor, catalog.RenameModifierOptionCommand{
			RequestID: uuid.New(),
			OptionID:  optID,
			Name:      "   No Ice   ",
		})
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.Equal(t, "No Ice", res.Name)

		var detailsJSON []byte
		err = db.QueryRowContext(ctx, `
			SELECT details FROM audit_events
			WHERE event_type = 'catalog.modifier_option.renamed' AND actor_id = $1
		`, actor.StaffID).Scan(&detailsJSON)
		require.NoError(t, err)

		var details map[string]any
		err = json.Unmarshal(detailsJSON, &details)
		require.NoError(t, err)
		assert.Equal(t, "No Ice", details["old_name"])
		assert.Equal(t, "No Ice", details["new_name"])
	})

	t.Run("ScopedConflict_SameGroup", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRenameModifierOptionHandler(runner)

		groupID := createTestModifierGroupDirect(t, db, "Ice", 0, 1, false)
		_ = createTestModifierOptionDirect(t, db, groupID, "No Ice", 0, true, false)
		opt2 := createTestModifierOptionDirect(t, db, groupID, "Extra Ice", 0, true, false)

		status, _, err := handler.Handle(ctx, actor, catalog.RenameModifierOptionCommand{
			RequestID: uuid.New(),
			OptionID:  opt2,
			Name:      "   nO iCe   ",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrNameConflict), "expected ErrNameConflict, got: %v", err)
		assert.Equal(t, 0, status)
	})

	t.Run("ScopedConflict_DifferentGroup_Allowed", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRenameModifierOptionHandler(runner)

		group1 := createTestModifierGroupDirect(t, db, "Sugar", 0, 1, false)
		group2 := createTestModifierGroupDirect(t, db, "Ice", 0, 1, false)
		_ = createTestModifierOptionDirect(t, db, group1, "Standard", 0, true, false)
		opt2 := createTestModifierOptionDirect(t, db, group2, "Default", 0, true, false)

		// Rename opt2 in group2 to "Standard" - allowed because scoped to group
		status, res, err := handler.Handle(ctx, actor, catalog.RenameModifierOptionCommand{
			RequestID: uuid.New(),
			OptionID:  opt2,
			Name:      "Standard",
		})
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.Equal(t, "Standard", res.Name)
	})

	t.Run("ParentRetirement", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRenameModifierOptionHandler(runner)

		retiredGroup := createTestModifierGroupDirect(t, db, "Retired Group", 0, 1, true)
		activeOpt := createTestModifierOptionDirect(t, db, retiredGroup, "Active Option", 0, true, false)

		status, _, err := handler.Handle(ctx, actor, catalog.RenameModifierOptionCommand{
			RequestID: uuid.New(),
			OptionID:  activeOpt,
			Name:      "New Option Name",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrEntityRetired), "expected ErrEntityRetired, got: %v", err)
		assert.Equal(t, 0, status)
	})

	t.Run("TargetRetirement", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRenameModifierOptionHandler(runner)

		activeGroup := createTestModifierGroupDirect(t, db, "Active Group", 0, 1, false)
		retiredOpt := createTestModifierOptionDirect(t, db, activeGroup, "Retired Option", 0, true, true)

		status, _, err := handler.Handle(ctx, actor, catalog.RenameModifierOptionCommand{
			RequestID: uuid.New(),
			OptionID:  retiredOpt,
			Name:      "New Option Name",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrEntityRetired), "expected ErrEntityRetired, got: %v", err)
		assert.Equal(t, 0, status)
	})

	t.Run("NotFound", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRenameModifierOptionHandler(runner)

		status, _, err := handler.Handle(ctx, actor, catalog.RenameModifierOptionCommand{
			RequestID: uuid.New(),
			OptionID:  uuid.New(),
			Name:      "Ghost Option",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrNotFound), "expected ErrNotFound, got: %v", err)
		assert.Equal(t, 0, status)
	})

	t.Run("Replay", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRenameModifierOptionHandler(runner)

		groupID := createTestModifierGroupDirect(t, db, "Ice", 0, 1, false)
		optID := createTestModifierOptionDirect(t, db, groupID, "Original Option", 0, true, false)

		cmd := catalog.RenameModifierOptionCommand{
			RequestID: uuid.New(),
			OptionID:  optID,
			Name:      "Renamed Option",
		}

		status1, res1, err1 := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err1)
		assert.Equal(t, 200, status1)

		status2, res2, err2 := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err2)
		assert.Equal(t, 200, status2)
		assert.Equal(t, res1, res2)

		var auditCount int
		err := db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE event_type = 'catalog.modifier_option.renamed'`).Scan(&auditCount)
		require.NoError(t, err)
		assert.Equal(t, 1, auditCount)
	})

	t.Run("Forbidden_MissingCapabilities", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		groupID := createTestModifierGroupDirect(t, db, "Ice", 0, 1, false)
		optID := createTestModifierOptionDirect(t, db, groupID, "Original Option", 0, true, false)

		cashier := createCatalogTestIdentity(t, db, q, []string{auth.RoleCashier}, true, "5678")
		handler := catalog.NewRenameModifierOptionHandler(runner)

		status, _, err := handler.Handle(ctx, catalog.Actor{StaffID: cashier.StaffID, SessionID: cashier.SessionID}, catalog.RenameModifierOptionCommand{
			RequestID: uuid.New(),
			OptionID:  optID,
			Name:      "Hacked Option",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrForbidden))
		assert.Equal(t, 0, status)
		assert.Equal(t, 1, countAuthorizationDenials(t, db, "catalog.modifier_option.rename"))
	})
}
