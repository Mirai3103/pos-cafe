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

// ============================================================================
// Reprice Item Tests
// ============================================================================

func TestRepriceItem(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()

	t.Run("Success_Audit_Timestamp_SecretExclusion", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRepriceItemHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		oldPrice := int64(35000)
		newPrice := int64(45000)
		itemID := createTestItemDirect(t, db, catID, "Americano", &oldPrice, false)

		var origCreatedAt, origUpdatedAt time.Time
		err := db.QueryRowContext(ctx, `SELECT created_at, updated_at FROM menu_items WHERE id = $1`, itemID).Scan(&origCreatedAt, &origUpdatedAt)
		require.NoError(t, err)

		reqID := uuid.New()
		cmd := catalog.RepriceItemCommand{
			RequestID:  reqID,
			ItemID:     itemID,
			PriceVND:   newPrice,
			ManagerPIN: "1234",
		}

		status, res, err := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.Equal(t, itemID, res.ID)
		assert.Equal(t, catID, res.CategoryID)
		assert.Equal(t, "Americano", res.Name)
		require.NotNil(t, res.PriceVND)
		assert.Equal(t, newPrice, *res.PriceVND)
		assert.True(t, res.Available)

		// Verify database row
		var dbPrice int64
		var dbUpdatedAt time.Time
		err = db.QueryRowContext(ctx, `SELECT price_vnd, updated_at FROM menu_items WHERE id = $1`, itemID).Scan(&dbPrice, &dbUpdatedAt)
		require.NoError(t, err)
		assert.Equal(t, newPrice, dbPrice)
		assert.False(t, dbUpdatedAt.Before(origUpdatedAt), "updated_at should be updated")

		// Verify audit event
		var eventType string
		var actorID, sessionID uuid.UUID
		var detailsJSON []byte
		err = db.QueryRowContext(ctx, `
			SELECT event_type, actor_id, session_id, details
			FROM audit_events
			WHERE event_type = 'catalog.item.repriced' AND actor_id = $1
		`, actor.StaffID).Scan(&eventType, &actorID, &sessionID, &detailsJSON)
		require.NoError(t, err)
		assert.Equal(t, "catalog.item.repriced", eventType)
		assert.Equal(t, actor.StaffID, actorID)
		assert.Equal(t, actor.SessionID, sessionID)

		var details map[string]any
		err = json.Unmarshal(detailsJSON, &details)
		require.NoError(t, err)
		assert.Equal(t, itemID.String(), details["item_id"])
		assert.Equal(t, float64(oldPrice), details["old_price_vnd"])
		assert.Equal(t, float64(newPrice), details["new_price_vnd"])

		// Secret exclusion: PIN must never appear in audit details or mutation record
		assert.False(t, strings.Contains(string(detailsJSON), "1234"), "audit details must never record manager PIN")

		var responseBody []byte
		err = db.QueryRowContext(ctx, `SELECT response_body FROM catalog_mutation_requests WHERE request_id = $1`, reqID).Scan(&responseBody)
		require.NoError(t, err)
		assert.False(t, strings.Contains(string(responseBody), "1234"), "idempotent response must never record manager PIN")
	})

	t.Run("DirectItemOnly_RejectsSizedItem", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRepriceItemHandler(runner)

		catID := createTestCategoryDirect(t, db, "Tea")
		itemID := createTestItemDirect(t, db, catID, "Milk Tea", nil, false)
		_ = createTestSizeDirect(t, db, itemID, "Small", 30000, false)
		_ = createTestSizeDirect(t, db, itemID, "Large", 40000, false)

		status, _, err := handler.Handle(ctx, actor, catalog.RepriceItemCommand{
			RequestID:  uuid.New(),
			ItemID:     itemID,
			PriceVND:   50000,
			ManagerPIN: "1234",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrInvalidPricingConfiguration), "expected ErrInvalidPricingConfiguration, got: %v", err)
		assert.Equal(t, 0, status)
	})

	t.Run("PriceBounds", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRepriceItemHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		price := int64(35000)
		itemID := createTestItemDirect(t, db, catID, "Espresso", &price, false)

		t.Run("ZeroPrice_Rejected", func(t *testing.T) {
			status, _, err := handler.Handle(ctx, actor, catalog.RepriceItemCommand{
				RequestID:  uuid.New(),
				ItemID:     itemID,
				PriceVND:   0,
				ManagerPIN: "1234",
			})
			require.Error(t, err)
			assert.True(t, errors.Is(err, catalog.ErrInvalidPricingConfiguration))
			assert.Equal(t, 0, status)
		})

		t.Run("NegativePrice_Rejected", func(t *testing.T) {
			status, _, err := handler.Handle(ctx, actor, catalog.RepriceItemCommand{
				RequestID:  uuid.New(),
				ItemID:     itemID,
				PriceVND:   -500,
				ManagerPIN: "1234",
			})
			require.Error(t, err)
			assert.True(t, errors.Is(err, catalog.ErrInvalidPricingConfiguration))
			assert.Equal(t, 0, status)
		})

		t.Run("ExceedsMax_Rejected", func(t *testing.T) {
			status, _, err := handler.Handle(ctx, actor, catalog.RepriceItemCommand{
				RequestID:  uuid.New(),
				ItemID:     itemID,
				PriceVND:   2_147_483_648,
				ManagerPIN: "1234",
			})
			require.Error(t, err)
			assert.True(t, errors.Is(err, catalog.ErrInvalidPricingConfiguration))
			assert.Equal(t, 0, status)
		})

		t.Run("ExactMin_Allowed", func(t *testing.T) {
			status, res, err := handler.Handle(ctx, actor, catalog.RepriceItemCommand{
				RequestID:  uuid.New(),
				ItemID:     itemID,
				PriceVND:   1,
				ManagerPIN: "1234",
			})
			require.NoError(t, err)
			assert.Equal(t, 200, status)
			require.NotNil(t, res.PriceVND)
			assert.Equal(t, int64(1), *res.PriceVND)
		})

		t.Run("ExactMax_Allowed", func(t *testing.T) {
			status, res, err := handler.Handle(ctx, actor, catalog.RepriceItemCommand{
				RequestID:  uuid.New(),
				ItemID:     itemID,
				PriceVND:   2_147_483_647,
				ManagerPIN: "1234",
			})
			require.NoError(t, err)
			assert.Equal(t, 200, status)
			require.NotNil(t, res.PriceVND)
			assert.Equal(t, int64(2_147_483_647), *res.PriceVND)
		})
	})

	t.Run("SamePrice_Auditing", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRepriceItemHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		price := int64(35000)
		itemID := createTestItemDirect(t, db, catID, "Americano", &price, false)

		status, res, err := handler.Handle(ctx, actor, catalog.RepriceItemCommand{
			RequestID:  uuid.New(),
			ItemID:     itemID,
			PriceVND:   price,
			ManagerPIN: "1234",
		})
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		require.NotNil(t, res.PriceVND)
		assert.Equal(t, price, *res.PriceVND)

		var detailsJSON []byte
		err = db.QueryRowContext(ctx, `
			SELECT details FROM audit_events
			WHERE event_type = 'catalog.item.repriced' AND actor_id = $1
		`, actor.StaffID).Scan(&detailsJSON)
		require.NoError(t, err)

		var details map[string]any
		err = json.Unmarshal(detailsJSON, &details)
		require.NoError(t, err)
		assert.Equal(t, float64(price), details["old_price_vnd"])
		assert.Equal(t, float64(price), details["new_price_vnd"])
	})

	t.Run("TargetRetirement", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRepriceItemHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		price := int64(35000)
		retiredItem := createTestItemDirect(t, db, catID, "Old Brew", &price, true)

		status, _, err := handler.Handle(ctx, actor, catalog.RepriceItemCommand{
			RequestID:  uuid.New(),
			ItemID:     retiredItem,
			PriceVND:   40000,
			ManagerPIN: "1234",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrEntityRetired), "expected ErrEntityRetired, got: %v", err)
		assert.Equal(t, 0, status)
	})

	t.Run("NotFound", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRepriceItemHandler(runner)

		status, _, err := handler.Handle(ctx, actor, catalog.RepriceItemCommand{
			RequestID:  uuid.New(),
			ItemID:     uuid.New(),
			PriceVND:   40000,
			ManagerPIN: "1234",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrNotFound), "expected ErrNotFound, got: %v", err)
		assert.Equal(t, 0, status)
	})

	t.Run("Forbidden_MissingCapabilities", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		catID := createTestCategoryDirect(t, db, "Coffee")
		price := int64(35000)
		itemID := createTestItemDirect(t, db, catID, "Americano", &price, false)

		cashier := createCatalogTestIdentity(t, db, q, []string{auth.RoleCashier}, true, "5678")
		handler := catalog.NewRepriceItemHandler(runner)

		status, _, err := handler.Handle(ctx, catalog.Actor{StaffID: cashier.StaffID, SessionID: cashier.SessionID}, catalog.RepriceItemCommand{
			RequestID:  uuid.New(),
			ItemID:     itemID,
			PriceVND:   40000,
			ManagerPIN: "5678",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrForbidden))
		assert.Equal(t, 0, status)
		assert.Equal(t, 1, countAuthorizationDenials(t, db, "catalog.item.reprice"))
	})

	t.Run("InvalidManagerPIN", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRepriceItemHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		price := int64(35000)
		itemID := createTestItemDirect(t, db, catID, "Americano", &price, false)

		status, _, err := handler.Handle(ctx, actor, catalog.RepriceItemCommand{
			RequestID:  uuid.New(),
			ItemID:     itemID,
			PriceVND:   40000,
			ManagerPIN: "9999", // wrong PIN
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrInvalidManagerPin))
		assert.Equal(t, 0, status)
		assert.Equal(t, 1, countAuthorizationDenials(t, db, "catalog.item.reprice"))
	})

	t.Run("PINBeforeReplay", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRepriceItemHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		price := int64(35000)
		itemID := createTestItemDirect(t, db, catID, "Americano", &price, false)

		reqID := uuid.New()
		cmd := catalog.RepriceItemCommand{
			RequestID:  reqID,
			ItemID:     itemID,
			PriceVND:   40000,
			ManagerPIN: "1234",
		}

		status1, res1, err1 := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err1)
		assert.Equal(t, 200, status1)

		// Replay with WRONG PIN must fail before replay can be returned
		wrongPinCmd := cmd
		wrongPinCmd.ManagerPIN = "9999"
		statusBad, _, errBad := handler.Handle(ctx, actor, wrongPinCmd)
		require.Error(t, errBad)
		assert.True(t, errors.Is(errBad, catalog.ErrInvalidManagerPin))
		assert.Equal(t, 0, statusBad)

		// Replay with CORRECT PIN must succeed with exact response
		status2, res2, err2 := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err2)
		assert.Equal(t, 200, status2)
		assert.Equal(t, res1, res2)

		var auditCount int
		err := db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE event_type = 'catalog.item.repriced'`).Scan(&auditCount)
		require.NoError(t, err)
		assert.Equal(t, 1, auditCount)
	})
}

// ============================================================================
// Reprice Size Tests
// ============================================================================

func TestRepriceSize(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()

	t.Run("Success_Audit_Timestamp_SecretExclusion", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRepriceSizeHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		itemID := createTestItemDirect(t, db, catID, "Latte", nil, false)
		oldPrice := int64(30000)
		newPrice := int64(38000)
		sizeID := createTestSizeDirect(t, db, itemID, "Regular", oldPrice, false)

		var origCreatedAt, origUpdatedAt time.Time
		err := db.QueryRowContext(ctx, `SELECT created_at, updated_at FROM menu_item_sizes WHERE id = $1`, sizeID).Scan(&origCreatedAt, &origUpdatedAt)
		require.NoError(t, err)

		reqID := uuid.New()
		cmd := catalog.RepriceSizeCommand{
			RequestID:  reqID,
			SizeID:     sizeID,
			PriceVND:   newPrice,
			ManagerPIN: "1234",
		}

		status, res, err := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.Equal(t, sizeID, res.ID)
		assert.Equal(t, "Regular", res.Name)
		assert.Equal(t, newPrice, res.PriceVND)
		assert.True(t, res.Available)

		// Verify database row
		var dbPrice int64
		var dbUpdatedAt time.Time
		err = db.QueryRowContext(ctx, `SELECT price_vnd, updated_at FROM menu_item_sizes WHERE id = $1`, sizeID).Scan(&dbPrice, &dbUpdatedAt)
		require.NoError(t, err)
		assert.Equal(t, newPrice, dbPrice)
		assert.False(t, dbUpdatedAt.Before(origUpdatedAt), "updated_at should be updated")

		// Verify audit event
		var eventType string
		var actorID, sessionID uuid.UUID
		var detailsJSON []byte
		err = db.QueryRowContext(ctx, `
			SELECT event_type, actor_id, session_id, details
			FROM audit_events
			WHERE event_type = 'catalog.size.repriced' AND actor_id = $1
		`, actor.StaffID).Scan(&eventType, &actorID, &sessionID, &detailsJSON)
		require.NoError(t, err)
		assert.Equal(t, "catalog.size.repriced", eventType)
		assert.Equal(t, actor.StaffID, actorID)
		assert.Equal(t, actor.SessionID, sessionID)

		var details map[string]any
		err = json.Unmarshal(detailsJSON, &details)
		require.NoError(t, err)
		assert.Equal(t, sizeID.String(), details["size_id"])
		assert.Equal(t, itemID.String(), details["menu_item_id"])
		assert.Equal(t, float64(oldPrice), details["old_price_vnd"])
		assert.Equal(t, float64(newPrice), details["new_price_vnd"])
		assert.False(t, strings.Contains(string(detailsJSON), "1234"), "audit details must never record manager PIN")
	})

	t.Run("PriceBounds", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRepriceSizeHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		itemID := createTestItemDirect(t, db, catID, "Latte", nil, false)
		sizeID := createTestSizeDirect(t, db, itemID, "Regular", 30000, false)

		t.Run("ZeroPrice_Rejected", func(t *testing.T) {
			status, _, err := handler.Handle(ctx, actor, catalog.RepriceSizeCommand{
				RequestID:  uuid.New(),
				SizeID:     sizeID,
				PriceVND:   0,
				ManagerPIN: "1234",
			})
			require.Error(t, err)
			assert.True(t, errors.Is(err, catalog.ErrInvalidPricingConfiguration))
			assert.Equal(t, 0, status)
		})

		t.Run("ExceedsMax_Rejected", func(t *testing.T) {
			status, _, err := handler.Handle(ctx, actor, catalog.RepriceSizeCommand{
				RequestID:  uuid.New(),
				SizeID:     sizeID,
				PriceVND:   2_147_483_648,
				ManagerPIN: "1234",
			})
			require.Error(t, err)
			assert.True(t, errors.Is(err, catalog.ErrInvalidPricingConfiguration))
			assert.Equal(t, 0, status)
		})

		t.Run("ExactMin_Allowed", func(t *testing.T) {
			status, res, err := handler.Handle(ctx, actor, catalog.RepriceSizeCommand{
				RequestID:  uuid.New(),
				SizeID:     sizeID,
				PriceVND:   1,
				ManagerPIN: "1234",
			})
			require.NoError(t, err)
			assert.Equal(t, 200, status)
			assert.Equal(t, int64(1), res.PriceVND)
		})

		t.Run("ExactMax_Allowed", func(t *testing.T) {
			status, res, err := handler.Handle(ctx, actor, catalog.RepriceSizeCommand{
				RequestID:  uuid.New(),
				SizeID:     sizeID,
				PriceVND:   2_147_483_647,
				ManagerPIN: "1234",
			})
			require.NoError(t, err)
			assert.Equal(t, 200, status)
			assert.Equal(t, int64(2_147_483_647), res.PriceVND)
		})
	})

	t.Run("SamePrice_Auditing", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRepriceSizeHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		itemID := createTestItemDirect(t, db, catID, "Latte", nil, false)
		price := int64(30000)
		sizeID := createTestSizeDirect(t, db, itemID, "Regular", price, false)

		status, res, err := handler.Handle(ctx, actor, catalog.RepriceSizeCommand{
			RequestID:  uuid.New(),
			SizeID:     sizeID,
			PriceVND:   price,
			ManagerPIN: "1234",
		})
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.Equal(t, price, res.PriceVND)

		var detailsJSON []byte
		err = db.QueryRowContext(ctx, `
			SELECT details FROM audit_events
			WHERE event_type = 'catalog.size.repriced' AND actor_id = $1
		`, actor.StaffID).Scan(&detailsJSON)
		require.NoError(t, err)

		var details map[string]any
		err = json.Unmarshal(detailsJSON, &details)
		require.NoError(t, err)
		assert.Equal(t, float64(price), details["old_price_vnd"])
		assert.Equal(t, float64(price), details["new_price_vnd"])
	})

	t.Run("ParentRetirement", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRepriceSizeHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		retiredItem := createTestItemDirect(t, db, catID, "Retired Latte", nil, true)
		activeSize := createTestSizeDirect(t, db, retiredItem, "Active Size", 30000, false)

		status, _, err := handler.Handle(ctx, actor, catalog.RepriceSizeCommand{
			RequestID:  uuid.New(),
			SizeID:     activeSize,
			PriceVND:   35000,
			ManagerPIN: "1234",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrEntityRetired), "expected ErrEntityRetired, got: %v", err)
		assert.Equal(t, 0, status)
	})

	t.Run("TargetRetirement", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRepriceSizeHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		activeItem := createTestItemDirect(t, db, catID, "Active Latte", nil, false)
		retiredSize := createTestSizeDirect(t, db, activeItem, "Retired Size", 30000, true)

		status, _, err := handler.Handle(ctx, actor, catalog.RepriceSizeCommand{
			RequestID:  uuid.New(),
			SizeID:     retiredSize,
			PriceVND:   35000,
			ManagerPIN: "1234",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrEntityRetired), "expected ErrEntityRetired, got: %v", err)
		assert.Equal(t, 0, status)
	})

	t.Run("NotFound", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRepriceSizeHandler(runner)

		status, _, err := handler.Handle(ctx, actor, catalog.RepriceSizeCommand{
			RequestID:  uuid.New(),
			SizeID:     uuid.New(),
			PriceVND:   35000,
			ManagerPIN: "1234",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrNotFound), "expected ErrNotFound, got: %v", err)
		assert.Equal(t, 0, status)
	})

	t.Run("Forbidden_MissingCapabilities", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		catID := createTestCategoryDirect(t, db, "Coffee")
		itemID := createTestItemDirect(t, db, catID, "Latte", nil, false)
		sizeID := createTestSizeDirect(t, db, itemID, "Regular", 30000, false)

		cashier := createCatalogTestIdentity(t, db, q, []string{auth.RoleCashier}, true, "5678")
		handler := catalog.NewRepriceSizeHandler(runner)

		status, _, err := handler.Handle(ctx, catalog.Actor{StaffID: cashier.StaffID, SessionID: cashier.SessionID}, catalog.RepriceSizeCommand{
			RequestID:  uuid.New(),
			SizeID:     sizeID,
			PriceVND:   35000,
			ManagerPIN: "5678",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrForbidden))
		assert.Equal(t, 0, status)
		assert.Equal(t, 1, countAuthorizationDenials(t, db, "catalog.size.reprice"))
	})

	t.Run("InvalidManagerPIN", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRepriceSizeHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		itemID := createTestItemDirect(t, db, catID, "Latte", nil, false)
		sizeID := createTestSizeDirect(t, db, itemID, "Regular", 30000, false)

		status, _, err := handler.Handle(ctx, actor, catalog.RepriceSizeCommand{
			RequestID:  uuid.New(),
			SizeID:     sizeID,
			PriceVND:   35000,
			ManagerPIN: "9999",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrInvalidManagerPin))
		assert.Equal(t, 0, status)
		assert.Equal(t, 1, countAuthorizationDenials(t, db, "catalog.size.reprice"))
	})

	t.Run("PINBeforeReplay", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRepriceSizeHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		itemID := createTestItemDirect(t, db, catID, "Latte", nil, false)
		sizeID := createTestSizeDirect(t, db, itemID, "Regular", 30000, false)

		reqID := uuid.New()
		cmd := catalog.RepriceSizeCommand{
			RequestID:  reqID,
			SizeID:     sizeID,
			PriceVND:   35000,
			ManagerPIN: "1234",
		}

		status1, res1, err1 := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err1)
		assert.Equal(t, 200, status1)

		// Replay with WRONG PIN must fail before replay can be returned
		wrongPinCmd := cmd
		wrongPinCmd.ManagerPIN = "9999"
		statusBad, _, errBad := handler.Handle(ctx, actor, wrongPinCmd)
		require.Error(t, errBad)
		assert.True(t, errors.Is(errBad, catalog.ErrInvalidManagerPin))
		assert.Equal(t, 0, statusBad)

		// Replay with CORRECT PIN must succeed with exact response
		status2, res2, err2 := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err2)
		assert.Equal(t, 200, status2)
		assert.Equal(t, res1, res2)

		var auditCount int
		err := db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE event_type = 'catalog.size.repriced'`).Scan(&auditCount)
		require.NoError(t, err)
		assert.Equal(t, 1, auditCount)
	})
}

// ============================================================================
// Reprice Modifier Option Tests
// ============================================================================

func TestRepriceModifierOption(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()

	t.Run("Success_Audit_Timestamp_SecretExclusion", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRepriceModifierOptionHandler(runner)

		groupID := createTestModifierGroupDirect(t, db, "Toppings", 0, 3, false)
		oldSurcharge := int64(5000)
		newSurcharge := int64(8000)
		optID := createTestModifierOptionDirect(t, db, groupID, "Pudding", oldSurcharge, true, false)

		var origCreatedAt, origUpdatedAt time.Time
		err := db.QueryRowContext(ctx, `SELECT created_at, updated_at FROM modifier_options WHERE id = $1`, optID).Scan(&origCreatedAt, &origUpdatedAt)
		require.NoError(t, err)

		reqID := uuid.New()
		cmd := catalog.RepriceModifierOptionCommand{
			RequestID:    reqID,
			OptionID:     optID,
			SurchargeVND: newSurcharge,
			ManagerPIN:   "1234",
		}

		status, res, err := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.Equal(t, optID, res.ID)
		assert.Equal(t, groupID, res.ModifierGroupID)
		assert.Equal(t, "Pudding", res.Name)
		assert.Equal(t, newSurcharge, res.SurchargeVND)
		assert.True(t, res.Available)

		// Verify database row
		var dbSurcharge int64
		var dbUpdatedAt time.Time
		err = db.QueryRowContext(ctx, `SELECT surcharge_vnd, updated_at FROM modifier_options WHERE id = $1`, optID).Scan(&dbSurcharge, &dbUpdatedAt)
		require.NoError(t, err)
		assert.Equal(t, newSurcharge, dbSurcharge)
		assert.False(t, dbUpdatedAt.Before(origUpdatedAt), "updated_at should be updated")

		// Verify audit event
		var eventType string
		var actorID, sessionID uuid.UUID
		var detailsJSON []byte
		err = db.QueryRowContext(ctx, `
			SELECT event_type, actor_id, session_id, details
			FROM audit_events
			WHERE event_type = 'catalog.modifier_option.repriced' AND actor_id = $1
		`, actor.StaffID).Scan(&eventType, &actorID, &sessionID, &detailsJSON)
		require.NoError(t, err)
		assert.Equal(t, "catalog.modifier_option.repriced", eventType)
		assert.Equal(t, actor.StaffID, actorID)
		assert.Equal(t, actor.SessionID, sessionID)

		var details map[string]any
		err = json.Unmarshal(detailsJSON, &details)
		require.NoError(t, err)
		assert.Equal(t, optID.String(), details["option_id"])
		assert.Equal(t, groupID.String(), details["modifier_group_id"])
		assert.Equal(t, float64(oldSurcharge), details["old_surcharge_vnd"])
		assert.Equal(t, float64(newSurcharge), details["new_surcharge_vnd"])
		assert.False(t, strings.Contains(string(detailsJSON), "1234"), "audit details must never record manager PIN")
	})

	t.Run("SurchargeBounds", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRepriceModifierOptionHandler(runner)

		groupID := createTestModifierGroupDirect(t, db, "Ice", 0, 1, false)
		optID := createTestModifierOptionDirect(t, db, groupID, "Less Ice", 5000, true, false)

		t.Run("NegativeSurcharge_Rejected", func(t *testing.T) {
			status, _, err := handler.Handle(ctx, actor, catalog.RepriceModifierOptionCommand{
				RequestID:    uuid.New(),
				OptionID:     optID,
				SurchargeVND: -1,
				ManagerPIN:   "1234",
			})
			require.Error(t, err)
			assert.True(t, errors.Is(err, catalog.ErrInvalidModifierConfiguration))
			assert.Equal(t, 0, status)
		})

		t.Run("ExceedsMax_Rejected", func(t *testing.T) {
			status, _, err := handler.Handle(ctx, actor, catalog.RepriceModifierOptionCommand{
				RequestID:    uuid.New(),
				OptionID:     optID,
				SurchargeVND: 2_147_483_648,
				ManagerPIN:   "1234",
			})
			require.Error(t, err)
			assert.True(t, errors.Is(err, catalog.ErrInvalidModifierConfiguration))
			assert.Equal(t, 0, status)
		})

		t.Run("ZeroSurcharge_Allowed", func(t *testing.T) {
			status, res, err := handler.Handle(ctx, actor, catalog.RepriceModifierOptionCommand{
				RequestID:    uuid.New(),
				OptionID:     optID,
				SurchargeVND: 0,
				ManagerPIN:   "1234",
			})
			require.NoError(t, err)
			assert.Equal(t, 200, status)
			assert.Equal(t, int64(0), res.SurchargeVND)
		})

		t.Run("ExactMax_Allowed", func(t *testing.T) {
			status, res, err := handler.Handle(ctx, actor, catalog.RepriceModifierOptionCommand{
				RequestID:    uuid.New(),
				OptionID:     optID,
				SurchargeVND: 2_147_483_647,
				ManagerPIN:   "1234",
			})
			require.NoError(t, err)
			assert.Equal(t, 200, status)
			assert.Equal(t, int64(2_147_483_647), res.SurchargeVND)
		})
	})

	t.Run("SamePrice_Auditing", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRepriceModifierOptionHandler(runner)

		groupID := createTestModifierGroupDirect(t, db, "Ice", 0, 1, false)
		surcharge := int64(5000)
		optID := createTestModifierOptionDirect(t, db, groupID, "Regular Ice", surcharge, true, false)

		status, res, err := handler.Handle(ctx, actor, catalog.RepriceModifierOptionCommand{
			RequestID:    uuid.New(),
			OptionID:     optID,
			SurchargeVND: surcharge,
			ManagerPIN:   "1234",
		})
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.Equal(t, surcharge, res.SurchargeVND)

		var detailsJSON []byte
		err = db.QueryRowContext(ctx, `
			SELECT details FROM audit_events
			WHERE event_type = 'catalog.modifier_option.repriced' AND actor_id = $1
		`, actor.StaffID).Scan(&detailsJSON)
		require.NoError(t, err)

		var details map[string]any
		err = json.Unmarshal(detailsJSON, &details)
		require.NoError(t, err)
		assert.Equal(t, float64(surcharge), details["old_surcharge_vnd"])
		assert.Equal(t, float64(surcharge), details["new_surcharge_vnd"])
	})

	t.Run("ParentRetirement", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRepriceModifierOptionHandler(runner)

		retiredGroup := createTestModifierGroupDirect(t, db, "Retired Group", 0, 1, true)
		activeOpt := createTestModifierOptionDirect(t, db, retiredGroup, "Active Option", 5000, true, false)

		status, _, err := handler.Handle(ctx, actor, catalog.RepriceModifierOptionCommand{
			RequestID:    uuid.New(),
			OptionID:     activeOpt,
			SurchargeVND: 6000,
			ManagerPIN:   "1234",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrEntityRetired), "expected ErrEntityRetired, got: %v", err)
		assert.Equal(t, 0, status)
	})

	t.Run("TargetRetirement", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRepriceModifierOptionHandler(runner)

		activeGroup := createTestModifierGroupDirect(t, db, "Active Group", 0, 1, false)
		retiredOpt := createTestModifierOptionDirect(t, db, activeGroup, "Retired Option", 5000, true, true)

		status, _, err := handler.Handle(ctx, actor, catalog.RepriceModifierOptionCommand{
			RequestID:    uuid.New(),
			OptionID:     retiredOpt,
			SurchargeVND: 6000,
			ManagerPIN:   "1234",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrEntityRetired), "expected ErrEntityRetired, got: %v", err)
		assert.Equal(t, 0, status)
	})

	t.Run("NotFound", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRepriceModifierOptionHandler(runner)

		status, _, err := handler.Handle(ctx, actor, catalog.RepriceModifierOptionCommand{
			RequestID:    uuid.New(),
			OptionID:     uuid.New(),
			SurchargeVND: 6000,
			ManagerPIN:   "1234",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrNotFound), "expected ErrNotFound, got: %v", err)
		assert.Equal(t, 0, status)
	})

	t.Run("Forbidden_MissingCapabilities", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		groupID := createTestModifierGroupDirect(t, db, "Ice", 0, 1, false)
		optID := createTestModifierOptionDirect(t, db, groupID, "Original Option", 5000, true, false)

		cashier := createCatalogTestIdentity(t, db, q, []string{auth.RoleCashier}, true, "5678")
		handler := catalog.NewRepriceModifierOptionHandler(runner)

		status, _, err := handler.Handle(ctx, catalog.Actor{StaffID: cashier.StaffID, SessionID: cashier.SessionID}, catalog.RepriceModifierOptionCommand{
			RequestID:    uuid.New(),
			OptionID:     optID,
			SurchargeVND: 6000,
			ManagerPIN:   "5678",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrForbidden))
		assert.Equal(t, 0, status)
		assert.Equal(t, 1, countAuthorizationDenials(t, db, "catalog.modifier_option.reprice"))
	})

	t.Run("InvalidManagerPIN", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRepriceModifierOptionHandler(runner)

		groupID := createTestModifierGroupDirect(t, db, "Ice", 0, 1, false)
		optID := createTestModifierOptionDirect(t, db, groupID, "Original Option", 5000, true, false)

		status, _, err := handler.Handle(ctx, actor, catalog.RepriceModifierOptionCommand{
			RequestID:    uuid.New(),
			OptionID:     optID,
			SurchargeVND: 6000,
			ManagerPIN:   "9999",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrInvalidManagerPin))
		assert.Equal(t, 0, status)
		assert.Equal(t, 1, countAuthorizationDenials(t, db, "catalog.modifier_option.reprice"))
	})

	t.Run("PINBeforeReplay", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRepriceModifierOptionHandler(runner)

		groupID := createTestModifierGroupDirect(t, db, "Ice", 0, 1, false)
		optID := createTestModifierOptionDirect(t, db, groupID, "Original Option", 5000, true, false)

		reqID := uuid.New()
		cmd := catalog.RepriceModifierOptionCommand{
			RequestID:    reqID,
			OptionID:     optID,
			SurchargeVND: 6000,
			ManagerPIN:   "1234",
		}

		status1, res1, err1 := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err1)
		assert.Equal(t, 200, status1)

		// Replay with WRONG PIN must fail before replay can be returned
		wrongPinCmd := cmd
		wrongPinCmd.ManagerPIN = "9999"
		statusBad, _, errBad := handler.Handle(ctx, actor, wrongPinCmd)
		require.Error(t, errBad)
		assert.True(t, errors.Is(errBad, catalog.ErrInvalidManagerPin))
		assert.Equal(t, 0, statusBad)

		// Replay with CORRECT PIN must succeed with exact response
		status2, res2, err2 := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err2)
		assert.Equal(t, 200, status2)
		assert.Equal(t, res1, res2)

		var auditCount int
		err := db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE event_type = 'catalog.modifier_option.repriced'`).Scan(&auditCount)
		require.NoError(t, err)
		assert.Equal(t, 1, auditCount)
	})
}

// ============================================================================
// Set Availability Item Tests
// ============================================================================

func TestSetAvailabilityItem(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()

	t.Run("Success_StateChange_Audit_Timestamp", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		barista := createCatalogTestIdentity(t, db, q, []string{auth.RoleBarista}, true, "1234")
		actor := catalog.Actor{StaffID: barista.StaffID, SessionID: barista.SessionID}
		handler := catalog.NewSetItemAvailabilityHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		price := int64(45000)
		itemID := createTestItemDirect(t, db, catID, "Americano", &price, false)

		var origCreatedAt, origUpdatedAt time.Time
		err := db.QueryRowContext(ctx, `SELECT created_at, updated_at FROM menu_items WHERE id = $1`, itemID).Scan(&origCreatedAt, &origUpdatedAt)
		require.NoError(t, err)

		time.Sleep(10 * time.Millisecond)

		reqID := uuid.New()
		cmd := catalog.SetItemAvailabilityCommand{
			RequestID: reqID,
			ItemID:    itemID,
			Available: false,
		}

		status, res, err := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.Equal(t, itemID, res.ID)
		assert.Equal(t, catID, res.CategoryID)
		assert.Equal(t, "Americano", res.Name)
		require.NotNil(t, res.PriceVND)
		assert.Equal(t, price, *res.PriceVND)
		assert.False(t, res.Available)

		// Verify database row
		var dbAvailable bool
		var dbUpdatedAt time.Time
		err = db.QueryRowContext(ctx, `SELECT available, updated_at FROM menu_items WHERE id = $1`, itemID).Scan(&dbAvailable, &dbUpdatedAt)
		require.NoError(t, err)
		assert.False(t, dbAvailable)
		assert.True(t, dbUpdatedAt.After(origUpdatedAt), "updated_at should be updated")

		// Verify audit event
		var eventType string
		var actorID, sessionID uuid.UUID
		var detailsJSON []byte
		err = db.QueryRowContext(ctx, `
			SELECT event_type, actor_id, session_id, details
			FROM audit_events
			WHERE event_type = 'catalog.item.availability_changed' AND actor_id = $1
		`, actor.StaffID).Scan(&eventType, &actorID, &sessionID, &detailsJSON)
		require.NoError(t, err)
		assert.Equal(t, "catalog.item.availability_changed", eventType)
		assert.Equal(t, actor.StaffID, actorID)
		assert.Equal(t, actor.SessionID, sessionID)

		var details map[string]any
		err = json.Unmarshal(detailsJSON, &details)
		require.NoError(t, err)
		assert.Equal(t, itemID.String(), details["item_id"])
		assert.Equal(t, true, details["old_available"])
		assert.Equal(t, false, details["new_available"])

		// Toggle back with Cashier role
		cashier := createCatalogTestIdentity(t, db, q, []string{auth.RoleCashier}, true, "5678")
		cashierActor := catalog.Actor{StaffID: cashier.StaffID, SessionID: cashier.SessionID}

		reqID2 := uuid.New()
		status2, res2, err2 := handler.Handle(ctx, cashierActor, catalog.SetItemAvailabilityCommand{
			RequestID: reqID2,
			ItemID:    itemID,
			Available: true,
		})
		require.NoError(t, err2)
		assert.Equal(t, 200, status2)
		assert.True(t, res2.Available)

		err = db.QueryRowContext(ctx, `SELECT available FROM menu_items WHERE id = $1`, itemID).Scan(&dbAvailable)
		require.NoError(t, err)
		assert.True(t, dbAvailable)

		var count int
		err = db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE event_type = 'catalog.item.availability_changed'`).Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 2, count)
	})

	t.Run("Success_SizedItem", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		barista := createCatalogTestIdentity(t, db, q, []string{auth.RoleBarista}, true, "1234")
		actor := catalog.Actor{StaffID: barista.StaffID, SessionID: barista.SessionID}
		handler := catalog.NewSetItemAvailabilityHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		itemID := createTestItemDirect(t, db, catID, "Milk Tea", nil, false)
		_ = createTestSizeDirect(t, db, itemID, "Regular", 30000, false)
		_ = createTestSizeDirect(t, db, itemID, "Large", 40000, false)

		status, res, err := handler.Handle(ctx, actor, catalog.SetItemAvailabilityCommand{
			RequestID: uuid.New(),
			ItemID:    itemID,
			Available: false,
		})
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.Equal(t, itemID, res.ID)
		assert.Nil(t, res.PriceVND)
		assert.False(t, res.Available)
		assert.Len(t, res.Sizes, 2)

		var dbAvailable bool
		err = db.QueryRowContext(ctx, `SELECT available FROM menu_items WHERE id = $1`, itemID).Scan(&dbAvailable)
		require.NoError(t, err)
		assert.False(t, dbAvailable)
	})

	t.Run("SameState_NoOp_NoTimestampOrAuditEventChange", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		cashier := createCatalogTestIdentity(t, db, q, []string{auth.RoleCashier}, true, "1234")
		actor := catalog.Actor{StaffID: cashier.StaffID, SessionID: cashier.SessionID}
		handler := catalog.NewSetItemAvailabilityHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		price := int64(45000)
		itemID := createTestItemDirect(t, db, catID, "Americano", &price, false) // created with available = true

		var origUpdatedAt time.Time
		err := db.QueryRowContext(ctx, `SELECT updated_at FROM menu_items WHERE id = $1`, itemID).Scan(&origUpdatedAt)
		require.NoError(t, err)

		time.Sleep(10 * time.Millisecond)

		reqID := uuid.New()
		cmd := catalog.SetItemAvailabilityCommand{
			RequestID: reqID,
			ItemID:    itemID,
			Available: true, // same state!
		}

		status, res, err := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.True(t, res.Available)

		// Verify database row timestamp was NOT updated
		var dbUpdatedAt time.Time
		err = db.QueryRowContext(ctx, `SELECT updated_at FROM menu_items WHERE id = $1`, itemID).Scan(&dbUpdatedAt)
		require.NoError(t, err)
		assert.True(t, dbUpdatedAt.Equal(origUpdatedAt), "updated_at must NOT change on same-state no-op")

		// Verify NO business audit event was emitted
		var auditCount int
		err = db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE event_type = 'catalog.item.availability_changed'`).Scan(&auditCount)
		require.NoError(t, err)
		assert.Equal(t, 0, auditCount, "no audit event should be emitted on same-state no-op")

		// Replay of same-state call succeeds with exact response and still no audit event
		statusReplay, resReplay, errReplay := handler.Handle(ctx, actor, cmd)
		require.NoError(t, errReplay)
		assert.Equal(t, 200, statusReplay)
		assert.Equal(t, res, resReplay)

		err = db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE event_type = 'catalog.item.availability_changed'`).Scan(&auditCount)
		require.NoError(t, err)
		assert.Equal(t, 0, auditCount)
	})

	t.Run("Authority_CashierAndBarista", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		catID := createTestCategoryDirect(t, db, "Coffee")
		price := int64(45000)
		itemID := createTestItemDirect(t, db, catID, "Americano", &price, false)

		handler := catalog.NewSetItemAvailabilityHandler(runner)

		// Cashier succeeds
		cashier := createCatalogTestIdentity(t, db, q, []string{auth.RoleCashier}, true, "1234")
		status1, res1, err1 := handler.Handle(ctx, catalog.Actor{StaffID: cashier.StaffID, SessionID: cashier.SessionID}, catalog.SetItemAvailabilityCommand{
			RequestID: uuid.New(),
			ItemID:    itemID,
			Available: false,
		})
		require.NoError(t, err1)
		assert.Equal(t, 200, status1)
		assert.False(t, res1.Available)

		// Barista succeeds
		barista := createCatalogTestIdentity(t, db, q, []string{auth.RoleBarista}, true, "5678")
		status2, res2, err2 := handler.Handle(ctx, catalog.Actor{StaffID: barista.StaffID, SessionID: barista.SessionID}, catalog.SetItemAvailabilityCommand{
			RequestID: uuid.New(),
			ItemID:    itemID,
			Available: true,
		})
		require.NoError(t, err2)
		assert.Equal(t, 200, status2)
		assert.True(t, res2.Available)
	})

	t.Run("Forbidden_MissingCapabilities", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		catID := createTestCategoryDirect(t, db, "Coffee")
		price := int64(45000)
		itemID := createTestItemDirect(t, db, catID, "Americano", &price, false)

		staffWithoutRole := createCatalogTestIdentity(t, db, q, []string{}, true, "1234")
		handler := catalog.NewSetItemAvailabilityHandler(runner)

		status, _, err := handler.Handle(ctx, catalog.Actor{StaffID: staffWithoutRole.StaffID, SessionID: staffWithoutRole.SessionID}, catalog.SetItemAvailabilityCommand{
			RequestID: uuid.New(),
			ItemID:    itemID,
			Available: false,
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrForbidden))
		assert.Equal(t, 0, status)
		assert.Equal(t, 1, countAuthorizationDenials(t, db, "catalog.item.set_availability"))
	})

	t.Run("TargetRetirement", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		catID := createTestCategoryDirect(t, db, "Coffee")
		price := int64(45000)
		retiredItem := createTestItemDirect(t, db, catID, "Old Brew", &price, true)

		barista := createCatalogTestIdentity(t, db, q, []string{auth.RoleBarista}, true, "1234")
		actor := catalog.Actor{StaffID: barista.StaffID, SessionID: barista.SessionID}
		handler := catalog.NewSetItemAvailabilityHandler(runner)

		status, _, err := handler.Handle(ctx, actor, catalog.SetItemAvailabilityCommand{
			RequestID: uuid.New(),
			ItemID:    retiredItem,
			Available: false,
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrEntityRetired))
		assert.Equal(t, 0, status)
	})

	t.Run("NotFound", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		barista := createCatalogTestIdentity(t, db, q, []string{auth.RoleBarista}, true, "1234")
		actor := catalog.Actor{StaffID: barista.StaffID, SessionID: barista.SessionID}
		handler := catalog.NewSetItemAvailabilityHandler(runner)

		status, _, err := handler.Handle(ctx, actor, catalog.SetItemAvailabilityCommand{
			RequestID: uuid.New(),
			ItemID:    uuid.New(),
			Available: false,
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrNotFound))
		assert.Equal(t, 0, status)
	})

	t.Run("Replay", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		barista := createCatalogTestIdentity(t, db, q, []string{auth.RoleBarista}, true, "1234")
		actor := catalog.Actor{StaffID: barista.StaffID, SessionID: barista.SessionID}
		handler := catalog.NewSetItemAvailabilityHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		price := int64(45000)
		itemID := createTestItemDirect(t, db, catID, "Americano", &price, false)

		reqID := uuid.New()
		cmd := catalog.SetItemAvailabilityCommand{
			RequestID: reqID,
			ItemID:    itemID,
			Available: false,
		}

		status1, res1, err1 := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err1)
		assert.Equal(t, 200, status1)

		// Exact replay returns identical result
		status2, res2, err2 := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err2)
		assert.Equal(t, 200, status2)
		assert.Equal(t, res1, res2)

		var auditCount int
		err := db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE event_type = 'catalog.item.availability_changed'`).Scan(&auditCount)
		require.NoError(t, err)
		assert.Equal(t, 1, auditCount)

		// Conflicting replay with different availability value returns ErrRequestConflict
		conflictCmd := cmd
		conflictCmd.Available = true
		statusConflict, _, errConflict := handler.Handle(ctx, actor, conflictCmd)
		require.Error(t, errConflict)
		assert.True(t, errors.Is(errConflict, catalog.ErrRequestConflict))
		assert.Equal(t, 0, statusConflict)
	})
}

// ============================================================================
// Set Availability Size Tests
// ============================================================================

func TestSetAvailabilitySize(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()

	t.Run("Success_StateChange_Audit_Timestamp", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		barista := createCatalogTestIdentity(t, db, q, []string{auth.RoleBarista}, true, "1234")
		actor := catalog.Actor{StaffID: barista.StaffID, SessionID: barista.SessionID}
		handler := catalog.NewSetSizeAvailabilityHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		itemID := createTestItemDirect(t, db, catID, "Latte", nil, false)
		sizeID := createTestSizeDirect(t, db, itemID, "Regular", 35000, false)

		var origCreatedAt, origUpdatedAt time.Time
		err := db.QueryRowContext(ctx, `SELECT created_at, updated_at FROM menu_item_sizes WHERE id = $1`, sizeID).Scan(&origCreatedAt, &origUpdatedAt)
		require.NoError(t, err)

		time.Sleep(10 * time.Millisecond)

		reqID := uuid.New()
		cmd := catalog.SetSizeAvailabilityCommand{
			RequestID: reqID,
			SizeID:    sizeID,
			Available: false,
		}

		status, res, err := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.Equal(t, sizeID, res.ID)
		assert.Equal(t, "Regular", res.Name)
		assert.Equal(t, int64(35000), res.PriceVND)
		assert.False(t, res.Available)

		// Verify database row
		var dbAvailable bool
		var dbUpdatedAt time.Time
		err = db.QueryRowContext(ctx, `SELECT available, updated_at FROM menu_item_sizes WHERE id = $1`, sizeID).Scan(&dbAvailable, &dbUpdatedAt)
		require.NoError(t, err)
		assert.False(t, dbAvailable)
		assert.True(t, dbUpdatedAt.After(origUpdatedAt), "updated_at should be updated")

		// Verify audit event
		var eventType string
		var actorID, sessionID uuid.UUID
		var detailsJSON []byte
		err = db.QueryRowContext(ctx, `
			SELECT event_type, actor_id, session_id, details
			FROM audit_events
			WHERE event_type = 'catalog.size.availability_changed' AND actor_id = $1
		`, actor.StaffID).Scan(&eventType, &actorID, &sessionID, &detailsJSON)
		require.NoError(t, err)
		assert.Equal(t, "catalog.size.availability_changed", eventType)
		assert.Equal(t, actor.StaffID, actorID)
		assert.Equal(t, actor.SessionID, sessionID)

		var details map[string]any
		err = json.Unmarshal(detailsJSON, &details)
		require.NoError(t, err)
		assert.Equal(t, sizeID.String(), details["size_id"])
		assert.Equal(t, itemID.String(), details["menu_item_id"])
		assert.Equal(t, true, details["old_available"])
		assert.Equal(t, false, details["new_available"])

		// Toggle back with Cashier role
		cashier := createCatalogTestIdentity(t, db, q, []string{auth.RoleCashier}, true, "5678")
		cashierActor := catalog.Actor{StaffID: cashier.StaffID, SessionID: cashier.SessionID}

		reqID2 := uuid.New()
		status2, res2, err2 := handler.Handle(ctx, cashierActor, catalog.SetSizeAvailabilityCommand{
			RequestID: reqID2,
			SizeID:    sizeID,
			Available: true,
		})
		require.NoError(t, err2)
		assert.Equal(t, 200, status2)
		assert.True(t, res2.Available)

		err = db.QueryRowContext(ctx, `SELECT available FROM menu_item_sizes WHERE id = $1`, sizeID).Scan(&dbAvailable)
		require.NoError(t, err)
		assert.True(t, dbAvailable)

		var count int
		err = db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE event_type = 'catalog.size.availability_changed'`).Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 2, count)
	})

	t.Run("SameState_NoOp_NoTimestampOrAuditEventChange", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		cashier := createCatalogTestIdentity(t, db, q, []string{auth.RoleCashier}, true, "1234")
		actor := catalog.Actor{StaffID: cashier.StaffID, SessionID: cashier.SessionID}
		handler := catalog.NewSetSizeAvailabilityHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		itemID := createTestItemDirect(t, db, catID, "Latte", nil, false)
		sizeID := createTestSizeDirect(t, db, itemID, "Regular", 35000, false) // created with available = true

		var origUpdatedAt time.Time
		err := db.QueryRowContext(ctx, `SELECT updated_at FROM menu_item_sizes WHERE id = $1`, sizeID).Scan(&origUpdatedAt)
		require.NoError(t, err)

		time.Sleep(10 * time.Millisecond)

		reqID := uuid.New()
		cmd := catalog.SetSizeAvailabilityCommand{
			RequestID: reqID,
			SizeID:    sizeID,
			Available: true, // same state!
		}

		status, res, err := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.True(t, res.Available)

		var dbUpdatedAt time.Time
		err = db.QueryRowContext(ctx, `SELECT updated_at FROM menu_item_sizes WHERE id = $1`, sizeID).Scan(&dbUpdatedAt)
		require.NoError(t, err)
		assert.True(t, dbUpdatedAt.Equal(origUpdatedAt), "updated_at must NOT change on same-state no-op")

		var auditCount int
		err = db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE event_type = 'catalog.size.availability_changed'`).Scan(&auditCount)
		require.NoError(t, err)
		assert.Equal(t, 0, auditCount, "no audit event should be emitted on same-state no-op")

		statusReplay, resReplay, errReplay := handler.Handle(ctx, actor, cmd)
		require.NoError(t, errReplay)
		assert.Equal(t, 200, statusReplay)
		assert.Equal(t, res, resReplay)

		err = db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE event_type = 'catalog.size.availability_changed'`).Scan(&auditCount)
		require.NoError(t, err)
		assert.Equal(t, 0, auditCount)
	})

	t.Run("Authority_CashierAndBarista", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		catID := createTestCategoryDirect(t, db, "Coffee")
		itemID := createTestItemDirect(t, db, catID, "Latte", nil, false)
		sizeID := createTestSizeDirect(t, db, itemID, "Regular", 35000, false)

		handler := catalog.NewSetSizeAvailabilityHandler(runner)

		// Cashier succeeds
		cashier := createCatalogTestIdentity(t, db, q, []string{auth.RoleCashier}, true, "1234")
		status1, res1, err1 := handler.Handle(ctx, catalog.Actor{StaffID: cashier.StaffID, SessionID: cashier.SessionID}, catalog.SetSizeAvailabilityCommand{
			RequestID: uuid.New(),
			SizeID:    sizeID,
			Available: false,
		})
		require.NoError(t, err1)
		assert.Equal(t, 200, status1)
		assert.False(t, res1.Available)

		// Barista succeeds
		barista := createCatalogTestIdentity(t, db, q, []string{auth.RoleBarista}, true, "5678")
		status2, res2, err2 := handler.Handle(ctx, catalog.Actor{StaffID: barista.StaffID, SessionID: barista.SessionID}, catalog.SetSizeAvailabilityCommand{
			RequestID: uuid.New(),
			SizeID:    sizeID,
			Available: true,
		})
		require.NoError(t, err2)
		assert.Equal(t, 200, status2)
		assert.True(t, res2.Available)
	})

	t.Run("Forbidden_MissingCapabilities", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		catID := createTestCategoryDirect(t, db, "Coffee")
		itemID := createTestItemDirect(t, db, catID, "Latte", nil, false)
		sizeID := createTestSizeDirect(t, db, itemID, "Regular", 35000, false)

		staffWithoutRole := createCatalogTestIdentity(t, db, q, []string{}, true, "1234")
		handler := catalog.NewSetSizeAvailabilityHandler(runner)

		status, _, err := handler.Handle(ctx, catalog.Actor{StaffID: staffWithoutRole.StaffID, SessionID: staffWithoutRole.SessionID}, catalog.SetSizeAvailabilityCommand{
			RequestID: uuid.New(),
			SizeID:    sizeID,
			Available: false,
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrForbidden))
		assert.Equal(t, 0, status)
		assert.Equal(t, 1, countAuthorizationDenials(t, db, "catalog.size.set_availability"))
	})

	t.Run("ParentRetirement", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		catID := createTestCategoryDirect(t, db, "Coffee")
		retiredItem := createTestItemDirect(t, db, catID, "Retired Latte", nil, true)
		sizeID := createTestSizeDirect(t, db, retiredItem, "Regular", 35000, false)

		barista := createCatalogTestIdentity(t, db, q, []string{auth.RoleBarista}, true, "1234")
		actor := catalog.Actor{StaffID: barista.StaffID, SessionID: barista.SessionID}
		handler := catalog.NewSetSizeAvailabilityHandler(runner)

		status, _, err := handler.Handle(ctx, actor, catalog.SetSizeAvailabilityCommand{
			RequestID: uuid.New(),
			SizeID:    sizeID,
			Available: false,
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrEntityRetired))
		assert.Equal(t, 0, status)
	})

	t.Run("TargetRetirement", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		catID := createTestCategoryDirect(t, db, "Coffee")
		activeItem := createTestItemDirect(t, db, catID, "Active Latte", nil, false)
		retiredSize := createTestSizeDirect(t, db, activeItem, "Regular", 35000, true)

		barista := createCatalogTestIdentity(t, db, q, []string{auth.RoleBarista}, true, "1234")
		actor := catalog.Actor{StaffID: barista.StaffID, SessionID: barista.SessionID}
		handler := catalog.NewSetSizeAvailabilityHandler(runner)

		status, _, err := handler.Handle(ctx, actor, catalog.SetSizeAvailabilityCommand{
			RequestID: uuid.New(),
			SizeID:    retiredSize,
			Available: false,
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrEntityRetired))
		assert.Equal(t, 0, status)
	})

	t.Run("NotFound", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		barista := createCatalogTestIdentity(t, db, q, []string{auth.RoleBarista}, true, "1234")
		actor := catalog.Actor{StaffID: barista.StaffID, SessionID: barista.SessionID}
		handler := catalog.NewSetSizeAvailabilityHandler(runner)

		status, _, err := handler.Handle(ctx, actor, catalog.SetSizeAvailabilityCommand{
			RequestID: uuid.New(),
			SizeID:    uuid.New(),
			Available: false,
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrNotFound))
		assert.Equal(t, 0, status)
	})

	t.Run("Replay", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		barista := createCatalogTestIdentity(t, db, q, []string{auth.RoleBarista}, true, "1234")
		actor := catalog.Actor{StaffID: barista.StaffID, SessionID: barista.SessionID}
		handler := catalog.NewSetSizeAvailabilityHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		itemID := createTestItemDirect(t, db, catID, "Latte", nil, false)
		sizeID := createTestSizeDirect(t, db, itemID, "Regular", 35000, false)

		reqID := uuid.New()
		cmd := catalog.SetSizeAvailabilityCommand{
			RequestID: reqID,
			SizeID:    sizeID,
			Available: false,
		}

		status1, res1, err1 := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err1)
		assert.Equal(t, 200, status1)

		// Exact replay returns identical result
		status2, res2, err2 := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err2)
		assert.Equal(t, 200, status2)
		assert.Equal(t, res1, res2)

		var auditCount int
		err := db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE event_type = 'catalog.size.availability_changed'`).Scan(&auditCount)
		require.NoError(t, err)
		assert.Equal(t, 1, auditCount)

		// Conflicting replay with different availability value returns ErrRequestConflict
		conflictCmd := cmd
		conflictCmd.Available = true
		statusConflict, _, errConflict := handler.Handle(ctx, actor, conflictCmd)
		require.Error(t, errConflict)
		assert.True(t, errors.Is(errConflict, catalog.ErrRequestConflict))
		assert.Equal(t, 0, statusConflict)
	})
}

// ============================================================================
// Set Availability Modifier Option Tests
// ============================================================================

func TestSetAvailabilityModifierOption(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()

	t.Run("Success_StateChange_Audit_Timestamp", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		barista := createCatalogTestIdentity(t, db, q, []string{auth.RoleBarista}, true, "1234")
		actor := catalog.Actor{StaffID: barista.StaffID, SessionID: barista.SessionID}
		handler := catalog.NewSetModifierOptionAvailabilityHandler(runner)

		groupID := createTestModifierGroupDirect(t, db, "Sugar Level", 0, 1, false)
		optID := createTestModifierOptionDirect(t, db, groupID, "Less Sugar", 0, true, false)

		var origCreatedAt, origUpdatedAt time.Time
		err := db.QueryRowContext(ctx, `SELECT created_at, updated_at FROM modifier_options WHERE id = $1`, optID).Scan(&origCreatedAt, &origUpdatedAt)
		require.NoError(t, err)

		time.Sleep(10 * time.Millisecond)

		reqID := uuid.New()
		cmd := catalog.SetModifierOptionAvailabilityCommand{
			RequestID: reqID,
			OptionID:  optID,
			Available: false,
		}

		status, res, err := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.Equal(t, optID, res.ID)
		assert.Equal(t, groupID, res.ModifierGroupID)
		assert.Equal(t, "Less Sugar", res.Name)
		assert.Equal(t, int64(0), res.SurchargeVND)
		assert.False(t, res.Available)

		// Verify database row
		var dbAvailable bool
		var dbUpdatedAt time.Time
		err = db.QueryRowContext(ctx, `SELECT available, updated_at FROM modifier_options WHERE id = $1`, optID).Scan(&dbAvailable, &dbUpdatedAt)
		require.NoError(t, err)
		assert.False(t, dbAvailable)
		assert.True(t, dbUpdatedAt.After(origUpdatedAt), "updated_at should be updated")

		// Verify audit event
		var eventType string
		var actorID, sessionID uuid.UUID
		var detailsJSON []byte
		err = db.QueryRowContext(ctx, `
			SELECT event_type, actor_id, session_id, details
			FROM audit_events
			WHERE event_type = 'catalog.modifier_option.availability_changed' AND actor_id = $1
		`, actor.StaffID).Scan(&eventType, &actorID, &sessionID, &detailsJSON)
		require.NoError(t, err)
		assert.Equal(t, "catalog.modifier_option.availability_changed", eventType)
		assert.Equal(t, actor.StaffID, actorID)
		assert.Equal(t, actor.SessionID, sessionID)

		var details map[string]any
		err = json.Unmarshal(detailsJSON, &details)
		require.NoError(t, err)
		assert.Equal(t, optID.String(), details["option_id"])
		assert.Equal(t, groupID.String(), details["modifier_group_id"])
		assert.Equal(t, true, details["old_available"])
		assert.Equal(t, false, details["new_available"])

		// Toggle back with Cashier role
		cashier := createCatalogTestIdentity(t, db, q, []string{auth.RoleCashier}, true, "5678")
		cashierActor := catalog.Actor{StaffID: cashier.StaffID, SessionID: cashier.SessionID}

		reqID2 := uuid.New()
		status2, res2, err2 := handler.Handle(ctx, cashierActor, catalog.SetModifierOptionAvailabilityCommand{
			RequestID: reqID2,
			OptionID:  optID,
			Available: true,
		})
		require.NoError(t, err2)
		assert.Equal(t, 200, status2)
		assert.True(t, res2.Available)

		err = db.QueryRowContext(ctx, `SELECT available FROM modifier_options WHERE id = $1`, optID).Scan(&dbAvailable)
		require.NoError(t, err)
		assert.True(t, dbAvailable)

		var count int
		err = db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE event_type = 'catalog.modifier_option.availability_changed'`).Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 2, count)
	})

	t.Run("SameState_NoOp_NoTimestampOrAuditEventChange", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		cashier := createCatalogTestIdentity(t, db, q, []string{auth.RoleCashier}, true, "1234")
		actor := catalog.Actor{StaffID: cashier.StaffID, SessionID: cashier.SessionID}
		handler := catalog.NewSetModifierOptionAvailabilityHandler(runner)

		groupID := createTestModifierGroupDirect(t, db, "Sugar Level", 0, 1, false)
		optID := createTestModifierOptionDirect(t, db, groupID, "Less Sugar", 0, true, false) // created with available = true

		var origUpdatedAt time.Time
		err := db.QueryRowContext(ctx, `SELECT updated_at FROM modifier_options WHERE id = $1`, optID).Scan(&origUpdatedAt)
		require.NoError(t, err)

		time.Sleep(10 * time.Millisecond)

		reqID := uuid.New()
		cmd := catalog.SetModifierOptionAvailabilityCommand{
			RequestID: reqID,
			OptionID:  optID,
			Available: true, // same state!
		}

		status, res, err := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.True(t, res.Available)

		var dbUpdatedAt time.Time
		err = db.QueryRowContext(ctx, `SELECT updated_at FROM modifier_options WHERE id = $1`, optID).Scan(&dbUpdatedAt)
		require.NoError(t, err)
		assert.True(t, dbUpdatedAt.Equal(origUpdatedAt), "updated_at must NOT change on same-state no-op")

		var auditCount int
		err = db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE event_type = 'catalog.modifier_option.availability_changed'`).Scan(&auditCount)
		require.NoError(t, err)
		assert.Equal(t, 0, auditCount, "no audit event should be emitted on same-state no-op")

		statusReplay, resReplay, errReplay := handler.Handle(ctx, actor, cmd)
		require.NoError(t, errReplay)
		assert.Equal(t, 200, statusReplay)
		assert.Equal(t, res, resReplay)

		err = db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE event_type = 'catalog.modifier_option.availability_changed'`).Scan(&auditCount)
		require.NoError(t, err)
		assert.Equal(t, 0, auditCount)
	})

	t.Run("Authority_CashierAndBarista", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		groupID := createTestModifierGroupDirect(t, db, "Sugar Level", 0, 1, false)
		optID := createTestModifierOptionDirect(t, db, groupID, "Less Sugar", 0, true, false)

		handler := catalog.NewSetModifierOptionAvailabilityHandler(runner)

		// Cashier succeeds
		cashier := createCatalogTestIdentity(t, db, q, []string{auth.RoleCashier}, true, "1234")
		status1, res1, err1 := handler.Handle(ctx, catalog.Actor{StaffID: cashier.StaffID, SessionID: cashier.SessionID}, catalog.SetModifierOptionAvailabilityCommand{
			RequestID: uuid.New(),
			OptionID:  optID,
			Available: false,
		})
		require.NoError(t, err1)
		assert.Equal(t, 200, status1)
		assert.False(t, res1.Available)

		// Barista succeeds
		barista := createCatalogTestIdentity(t, db, q, []string{auth.RoleBarista}, true, "5678")
		status2, res2, err2 := handler.Handle(ctx, catalog.Actor{StaffID: barista.StaffID, SessionID: barista.SessionID}, catalog.SetModifierOptionAvailabilityCommand{
			RequestID: uuid.New(),
			OptionID:  optID,
			Available: true,
		})
		require.NoError(t, err2)
		assert.Equal(t, 200, status2)
		assert.True(t, res2.Available)
	})

	t.Run("Forbidden_MissingCapabilities", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		groupID := createTestModifierGroupDirect(t, db, "Sugar Level", 0, 1, false)
		optID := createTestModifierOptionDirect(t, db, groupID, "Less Sugar", 0, true, false)

		staffWithoutRole := createCatalogTestIdentity(t, db, q, []string{}, true, "1234")
		handler := catalog.NewSetModifierOptionAvailabilityHandler(runner)

		status, _, err := handler.Handle(ctx, catalog.Actor{StaffID: staffWithoutRole.StaffID, SessionID: staffWithoutRole.SessionID}, catalog.SetModifierOptionAvailabilityCommand{
			RequestID: uuid.New(),
			OptionID:  optID,
			Available: false,
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrForbidden))
		assert.Equal(t, 0, status)
		assert.Equal(t, 1, countAuthorizationDenials(t, db, "catalog.modifier_option.set_availability"))
	})

	t.Run("ParentRetirement", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		retiredGroup := createTestModifierGroupDirect(t, db, "Retired Sugar", 0, 1, true)
		optID := createTestModifierOptionDirect(t, db, retiredGroup, "Less Sugar", 0, true, false)

		barista := createCatalogTestIdentity(t, db, q, []string{auth.RoleBarista}, true, "1234")
		actor := catalog.Actor{StaffID: barista.StaffID, SessionID: barista.SessionID}
		handler := catalog.NewSetModifierOptionAvailabilityHandler(runner)

		status, _, err := handler.Handle(ctx, actor, catalog.SetModifierOptionAvailabilityCommand{
			RequestID: uuid.New(),
			OptionID:  optID,
			Available: false,
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrEntityRetired))
		assert.Equal(t, 0, status)
	})

	t.Run("TargetRetirement", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		activeGroup := createTestModifierGroupDirect(t, db, "Active Sugar", 0, 1, false)
		retiredOpt := createTestModifierOptionDirect(t, db, activeGroup, "Retired Sugar Opt", 0, true, true)

		barista := createCatalogTestIdentity(t, db, q, []string{auth.RoleBarista}, true, "1234")
		actor := catalog.Actor{StaffID: barista.StaffID, SessionID: barista.SessionID}
		handler := catalog.NewSetModifierOptionAvailabilityHandler(runner)

		status, _, err := handler.Handle(ctx, actor, catalog.SetModifierOptionAvailabilityCommand{
			RequestID: uuid.New(),
			OptionID:  retiredOpt,
			Available: false,
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrEntityRetired))
		assert.Equal(t, 0, status)
	})

	t.Run("NotFound", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		barista := createCatalogTestIdentity(t, db, q, []string{auth.RoleBarista}, true, "1234")
		actor := catalog.Actor{StaffID: barista.StaffID, SessionID: barista.SessionID}
		handler := catalog.NewSetModifierOptionAvailabilityHandler(runner)

		status, _, err := handler.Handle(ctx, actor, catalog.SetModifierOptionAvailabilityCommand{
			RequestID: uuid.New(),
			OptionID:  uuid.New(),
			Available: false,
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrNotFound))
		assert.Equal(t, 0, status)
	})

	t.Run("Replay", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		barista := createCatalogTestIdentity(t, db, q, []string{auth.RoleBarista}, true, "1234")
		actor := catalog.Actor{StaffID: barista.StaffID, SessionID: barista.SessionID}
		handler := catalog.NewSetModifierOptionAvailabilityHandler(runner)

		groupID := createTestModifierGroupDirect(t, db, "Sugar Level", 0, 1, false)
		optID := createTestModifierOptionDirect(t, db, groupID, "Less Sugar", 0, true, false)

		reqID := uuid.New()
		cmd := catalog.SetModifierOptionAvailabilityCommand{
			RequestID: reqID,
			OptionID:  optID,
			Available: false,
		}

		status1, res1, err1 := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err1)
		assert.Equal(t, 200, status1)

		// Exact replay returns identical result
		status2, res2, err2 := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err2)
		assert.Equal(t, 200, status2)
		assert.Equal(t, res1, res2)

		var auditCount int
		err := db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE event_type = 'catalog.modifier_option.availability_changed'`).Scan(&auditCount)
		require.NoError(t, err)
		assert.Equal(t, 1, auditCount)

		// Conflicting replay with different availability value returns ErrRequestConflict
		conflictCmd := cmd
		conflictCmd.Available = true
		statusConflict, _, errConflict := handler.Handle(ctx, actor, conflictCmd)
		require.Error(t, errConflict)
		assert.True(t, errors.Is(errConflict, catalog.ErrRequestConflict))
		assert.Equal(t, 0, statusConflict)
	})
}

// ============================================================================
// Retire Item Tests
// ============================================================================

func TestRetireItem(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()

	t.Run("Success_NO_LONGER_OFFERED", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRetireItemHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		price := int64(35000)
		itemID := createTestItemDirect(t, db, catID, "Espresso", &price, false)

		var origCreatedAt, origUpdatedAt time.Time
		err := db.QueryRowContext(ctx, `SELECT created_at, updated_at FROM menu_items WHERE id = $1`, itemID).Scan(&origCreatedAt, &origUpdatedAt)
		require.NoError(t, err)

		reqID := uuid.New()
		cmd := catalog.RetireItemCommand{
			RequestID: reqID,
			ItemID:    itemID,
			Reason:    "NO_LONGER_OFFERED",
		}

		status, res, err := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.Equal(t, itemID, res.ID)
		assert.Equal(t, catID, res.CategoryID)
		assert.Equal(t, "Espresso", res.Name)
		require.NotNil(t, res.PriceVND)
		assert.Equal(t, price, *res.PriceVND)

		// Verify database row
		var retiredAt sql.NullTime
		var reason, note sql.NullString
		var dbUpdatedAt time.Time
		err = db.QueryRowContext(ctx, `SELECT retired_at, retirement_reason, retirement_note, updated_at FROM menu_items WHERE id = $1`, itemID).
			Scan(&retiredAt, &reason, &note, &dbUpdatedAt)
		require.NoError(t, err)
		assert.True(t, retiredAt.Valid, "retired_at should be set")
		assert.True(t, reason.Valid)
		assert.Equal(t, "NO_LONGER_OFFERED", reason.String)
		assert.False(t, note.Valid, "retirement_note should be null when not provided")
		assert.False(t, dbUpdatedAt.Before(origUpdatedAt), "updated_at should be updated")

		// Verify audit event
		var eventType string
		var actorID, sessionID uuid.UUID
		var detailsJSON []byte
		err = db.QueryRowContext(ctx, `
			SELECT event_type, actor_id, session_id, details
			FROM audit_events
			WHERE event_type = 'catalog.item.retired'
		`).Scan(&eventType, &actorID, &sessionID, &detailsJSON)
		require.NoError(t, err)
		assert.Equal(t, "catalog.item.retired", eventType)
		assert.Equal(t, manager.StaffID, actorID)
		assert.Equal(t, manager.SessionID, sessionID)

		var details map[string]any
		err = json.Unmarshal(detailsJSON, &details)
		require.NoError(t, err)
		assert.Equal(t, itemID.String(), details["item_id"])
		assert.Equal(t, "NO_LONGER_OFFERED", details["reason"])
		// Secret-free check: no PIN or credentials in audit details
		assert.NotContains(t, details, "manager_pin")
		assert.NotContains(t, details, "pin")
	})

	t.Run("Success_MENU_RESTRUCTURE_SizedItem", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRetireItemHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		itemID := createTestItemDirect(t, db, catID, "Americano", nil, false)
		size1ID := createTestSizeDirect(t, db, itemID, "Regular", 30000, false)
		size2ID := createTestSizeDirect(t, db, itemID, "Large", 40000, false)

		reqID := uuid.New()
		cmd := catalog.RetireItemCommand{
			RequestID: reqID,
			ItemID:    itemID,
			Reason:    "MENU_RESTRUCTURE",
		}

		status, res, err := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.Equal(t, itemID, res.ID)
		assert.Nil(t, res.PriceVND)
		assert.Len(t, res.Sizes, 2)
		sizeIDs := []uuid.UUID{res.Sizes[0].ID, res.Sizes[1].ID}
		assert.Contains(t, sizeIDs, size1ID)
		assert.Contains(t, sizeIDs, size2ID)

		// Child sizes in DB must NOT be deleted
		var sizeCount int
		err = db.QueryRowContext(ctx, `SELECT count(*) FROM menu_item_sizes WHERE menu_item_id = $1`, itemID).Scan(&sizeCount)
		require.NoError(t, err)
		assert.Equal(t, 2, sizeCount)
	})

	t.Run("Success_OTHER_TrimmedNote", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRetireItemHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		price := int64(50000)
		itemID := createTestItemDirect(t, db, catID, "Cold Brew", &price, false)

		rawNote := "   Supplier discontinued single-origin beans   "
		expectedNote := "Supplier discontinued single-origin beans"

		reqID := uuid.New()
		cmd := catalog.RetireItemCommand{
			RequestID: reqID,
			ItemID:    itemID,
			Reason:    "OTHER",
			Note:      rawNote,
		}

		status, res, err := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.Equal(t, itemID, res.ID)

		// Verify database note is trimmed
		var reason, note sql.NullString
		err = db.QueryRowContext(ctx, `SELECT retirement_reason, retirement_note FROM menu_items WHERE id = $1`, itemID).
			Scan(&reason, &note)
		require.NoError(t, err)
		assert.Equal(t, "OTHER", reason.String)
		assert.True(t, note.Valid)
		assert.Equal(t, expectedNote, note.String)

		// Verify audit details has trimmed note
		var detailsJSON []byte
		err = db.QueryRowContext(ctx, `SELECT details FROM audit_events WHERE event_type = 'catalog.item.retired'`).
			Scan(&detailsJSON)
		require.NoError(t, err)

		var details map[string]any
		err = json.Unmarshal(detailsJSON, &details)
		require.NoError(t, err)
		assert.Equal(t, expectedNote, details["note"])
	})

	t.Run("Invalid_OTHER_MissingOrBlankNote", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRetireItemHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		price := int64(50000)
		itemID := createTestItemDirect(t, db, catID, "Cold Brew", &price, false)

		// Empty note
		status1, _, err1 := handler.Handle(ctx, actor, catalog.RetireItemCommand{
			RequestID: uuid.New(),
			ItemID:    itemID,
			Reason:    "OTHER",
			Note:      "",
		})
		require.Error(t, err1)
		assert.Equal(t, 0, status1)

		// Blank whitespace note
		status2, _, err2 := handler.Handle(ctx, actor, catalog.RetireItemCommand{
			RequestID: uuid.New(),
			ItemID:    itemID,
			Reason:    "OTHER",
			Note:      "     ",
		})
		require.Error(t, err2)
		assert.Equal(t, 0, status2)
	})

	t.Run("Invalid_OTHER_NoteExceeds500", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRetireItemHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		price := int64(50000)
		itemID := createTestItemDirect(t, db, catID, "Cold Brew", &price, false)

		status, _, err := handler.Handle(ctx, actor, catalog.RetireItemCommand{
			RequestID: uuid.New(),
			ItemID:    itemID,
			Reason:    "OTHER",
			Note:      strings.Repeat("x", 501),
		})
		require.Error(t, err)
		assert.Equal(t, 0, status)
	})

	t.Run("Invalid_Reason", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRetireItemHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		price := int64(50000)
		itemID := createTestItemDirect(t, db, catID, "Cold Brew", &price, false)

		// Unknown reason
		status1, _, err1 := handler.Handle(ctx, actor, catalog.RetireItemCommand{
			RequestID: uuid.New(),
			ItemID:    itemID,
			Reason:    "DISCONTINUED",
		})
		require.Error(t, err1)
		assert.Equal(t, 0, status1)

		// Empty reason
		status2, _, err2 := handler.Handle(ctx, actor, catalog.RetireItemCommand{
			RequestID: uuid.New(),
			ItemID:    itemID,
			Reason:    "",
		})
		require.Error(t, err2)
		assert.Equal(t, 0, status2)
	})

	t.Run("TargetRetirement_PermanentConflict", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRetireItemHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		price := int64(50000)
		itemID := createTestItemDirect(t, db, catID, "Cold Brew", &price, false)

		// First retirement succeeds
		status1, _, err1 := handler.Handle(ctx, actor, catalog.RetireItemCommand{
			RequestID: uuid.New(),
			ItemID:    itemID,
			Reason:    "NO_LONGER_OFFERED",
		})
		require.NoError(t, err1)
		assert.Equal(t, 200, status1)

		// Second retirement with a new request ID returns ErrEntityRetired (permanent conflict)
		status2, _, err2 := handler.Handle(ctx, actor, catalog.RetireItemCommand{
			RequestID: uuid.New(),
			ItemID:    itemID,
			Reason:    "MENU_RESTRUCTURE",
		})
		require.Error(t, err2)
		assert.True(t, errors.Is(err2, catalog.ErrEntityRetired), "expected ErrEntityRetired, got: %v", err2)
		assert.Equal(t, 0, status2)
	})

	t.Run("Replay", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRetireItemHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		price := int64(50000)
		itemID := createTestItemDirect(t, db, catID, "Cold Brew", &price, false)

		reqID := uuid.New()
		cmd := catalog.RetireItemCommand{
			RequestID: reqID,
			ItemID:    itemID,
			Reason:    "NO_LONGER_OFFERED",
		}

		status1, res1, err1 := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err1)
		assert.Equal(t, 200, status1)

		// Exact replay returns identical result
		status2, res2, err2 := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err2)
		assert.Equal(t, 200, status2)
		assert.Equal(t, res1, res2)

		var auditCount int
		err := db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE event_type = 'catalog.item.retired'`).Scan(&auditCount)
		require.NoError(t, err)
		assert.Equal(t, 1, auditCount)

		// Conflicting replay with different reason returns ErrRequestConflict
		conflictCmd := cmd
		conflictCmd.Reason = "MENU_RESTRUCTURE"
		statusConflict, _, errConflict := handler.Handle(ctx, actor, conflictCmd)
		require.Error(t, errConflict)
		assert.True(t, errors.Is(errConflict, catalog.ErrRequestConflict))
		assert.Equal(t, 0, statusConflict)
	})

	t.Run("RetainedAssignmentRows", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRetireItemHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		price := int64(45000)
		itemID := createTestItemDirect(t, db, catID, "Cappuccino", &price, false)
		groupID := createTestModifierGroupDirect(t, db, "Milk Type", 0, 1, false)

		// Attach group to item directly
		_, err := db.ExecContext(ctx, `INSERT INTO item_modifier_groups (menu_item_id, modifier_group_id) VALUES ($1, $2)`, itemID, groupID)
		require.NoError(t, err)

		// Retire item
		status, _, err := handler.Handle(ctx, actor, catalog.RetireItemCommand{
			RequestID: uuid.New(),
			ItemID:    itemID,
			Reason:    "MENU_RESTRUCTURE",
		})
		require.NoError(t, err)
		assert.Equal(t, 200, status)

		// Verify assignment row in item_modifier_groups is still present
		var count int
		err = db.QueryRowContext(ctx, `SELECT count(*) FROM item_modifier_groups WHERE menu_item_id = $1 AND modifier_group_id = $2`, itemID, groupID).Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 1, count, "assignment row must not be deleted on item retirement")
	})

	t.Run("NotFound", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRetireItemHandler(runner)

		status, _, err := handler.Handle(ctx, actor, catalog.RetireItemCommand{
			RequestID: uuid.New(),
			ItemID:    uuid.New(),
			Reason:    "NO_LONGER_OFFERED",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrNotFound), "expected ErrNotFound, got: %v", err)
		assert.Equal(t, 0, status)
	})

	t.Run("Unauthorized_Forbidden", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		cashier := createCatalogTestIdentity(t, db, q, []string{auth.RoleCashier}, true, "1234")
		actor := catalog.Actor{StaffID: cashier.StaffID, SessionID: cashier.SessionID}
		handler := catalog.NewRetireItemHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		price := int64(30000)
		itemID := createTestItemDirect(t, db, catID, "Filter Coffee", &price, false)

		status, _, err := handler.Handle(ctx, actor, catalog.RetireItemCommand{
			RequestID: uuid.New(),
			ItemID:    itemID,
			Reason:    "NO_LONGER_OFFERED",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrForbidden), "expected ErrForbidden, got: %v", err)
		assert.Equal(t, 0, status)
	})
}

// ============================================================================
// Retire Size Tests
// ============================================================================

func TestRetireSize(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()

	t.Run("Success_NO_LONGER_OFFERED", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRetireSizeHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		itemID := createTestItemDirect(t, db, catID, "Latte", nil, false)
		sizeID := createTestSizeDirect(t, db, itemID, "Small", 35000, false)

		var origUpdatedAt time.Time
		err := db.QueryRowContext(ctx, `SELECT updated_at FROM menu_item_sizes WHERE id = $1`, sizeID).Scan(&origUpdatedAt)
		require.NoError(t, err)

		reqID := uuid.New()
		cmd := catalog.RetireSizeCommand{
			RequestID: reqID,
			SizeID:    sizeID,
			Reason:    "NO_LONGER_OFFERED",
		}

		status, res, err := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.Equal(t, sizeID, res.ID)
		assert.Equal(t, "Small", res.Name)
		assert.Equal(t, int64(35000), res.PriceVND)

		// Verify database row
		var retiredAt sql.NullTime
		var reason, note sql.NullString
		var dbUpdatedAt time.Time
		err = db.QueryRowContext(ctx, `SELECT retired_at, retirement_reason, retirement_note, updated_at FROM menu_item_sizes WHERE id = $1`, sizeID).
			Scan(&retiredAt, &reason, &note, &dbUpdatedAt)
		require.NoError(t, err)
		assert.True(t, retiredAt.Valid)
		assert.Equal(t, "NO_LONGER_OFFERED", reason.String)
		assert.False(t, note.Valid)
		assert.False(t, dbUpdatedAt.Before(origUpdatedAt))

		// Verify audit event
		var eventType string
		var detailsJSON []byte
		err = db.QueryRowContext(ctx, `SELECT event_type, details FROM audit_events WHERE event_type = 'catalog.size.retired'`).
			Scan(&eventType, &detailsJSON)
		require.NoError(t, err)
		assert.Equal(t, "catalog.size.retired", eventType)

		var details map[string]any
		err = json.Unmarshal(detailsJSON, &details)
		require.NoError(t, err)
		assert.Equal(t, sizeID.String(), details["size_id"])
		assert.Equal(t, itemID.String(), details["menu_item_id"])
		assert.Equal(t, "NO_LONGER_OFFERED", details["reason"])
	})

	t.Run("Success_OTHER_TrimmedNote", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRetireSizeHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		itemID := createTestItemDirect(t, db, catID, "Latte", nil, false)
		sizeID := createTestSizeDirect(t, db, itemID, "Jumbo", 60000, false)

		reqID := uuid.New()
		cmd := catalog.RetireSizeCommand{
			RequestID: reqID,
			SizeID:    sizeID,
			Reason:    "OTHER",
			Note:      "   Cups no longer manufactured   ",
		}

		status, res, err := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.Equal(t, sizeID, res.ID)

		var note sql.NullString
		err = db.QueryRowContext(ctx, `SELECT retirement_note FROM menu_item_sizes WHERE id = $1`, sizeID).Scan(&note)
		require.NoError(t, err)
		assert.True(t, note.Valid)
		assert.Equal(t, "Cups no longer manufactured", note.String)
	})

	t.Run("Invalid_OTHER_MissingOrBlankNote", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRetireSizeHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		itemID := createTestItemDirect(t, db, catID, "Latte", nil, false)
		sizeID := createTestSizeDirect(t, db, itemID, "Small", 35000, false)

		status, _, err := handler.Handle(ctx, actor, catalog.RetireSizeCommand{
			RequestID: uuid.New(),
			SizeID:    sizeID,
			Reason:    "OTHER",
			Note:      "",
		})
		require.Error(t, err)
		assert.Equal(t, 0, status)
	})

	t.Run("ParentRetirement", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRetireSizeHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		// Parent item is retired
		retiredItemID := createTestItemDirect(t, db, catID, "Retired Latte", nil, true)
		sizeID := createTestSizeDirect(t, db, retiredItemID, "Regular", 40000, false)

		status, _, err := handler.Handle(ctx, actor, catalog.RetireSizeCommand{
			RequestID: uuid.New(),
			SizeID:    sizeID,
			Reason:    "NO_LONGER_OFFERED",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrEntityRetired), "expected ErrEntityRetired when parent item is retired, got: %v", err)
		assert.Equal(t, 0, status)
	})

	t.Run("TargetRetirement_PermanentConflict", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRetireSizeHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		itemID := createTestItemDirect(t, db, catID, "Latte", nil, false)
		sizeID := createTestSizeDirect(t, db, itemID, "Regular", 40000, false)

		// First retirement succeeds
		status1, _, err1 := handler.Handle(ctx, actor, catalog.RetireSizeCommand{
			RequestID: uuid.New(),
			SizeID:    sizeID,
			Reason:    "NO_LONGER_OFFERED",
		})
		require.NoError(t, err1)
		assert.Equal(t, 200, status1)

		// Second retirement returns ErrEntityRetired
		status2, _, err2 := handler.Handle(ctx, actor, catalog.RetireSizeCommand{
			RequestID: uuid.New(),
			SizeID:    sizeID,
			Reason:    "MENU_RESTRUCTURE",
		})
		require.Error(t, err2)
		assert.True(t, errors.Is(err2, catalog.ErrEntityRetired), "expected ErrEntityRetired on already retired size, got: %v", err2)
		assert.Equal(t, 0, status2)
	})

	t.Run("Replay", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRetireSizeHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		itemID := createTestItemDirect(t, db, catID, "Latte", nil, false)
		sizeID := createTestSizeDirect(t, db, itemID, "Regular", 40000, false)

		reqID := uuid.New()
		cmd := catalog.RetireSizeCommand{
			RequestID: reqID,
			SizeID:    sizeID,
			Reason:    "NO_LONGER_OFFERED",
		}

		status1, res1, err1 := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err1)
		assert.Equal(t, 200, status1)

		// Exact replay
		status2, res2, err2 := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err2)
		assert.Equal(t, 200, status2)
		assert.Equal(t, res1, res2)

		var auditCount int
		err := db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE event_type = 'catalog.size.retired'`).Scan(&auditCount)
		require.NoError(t, err)
		assert.Equal(t, 1, auditCount)
	})

	t.Run("NotFound", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRetireSizeHandler(runner)

		status, _, err := handler.Handle(ctx, actor, catalog.RetireSizeCommand{
			RequestID: uuid.New(),
			SizeID:    uuid.New(),
			Reason:    "NO_LONGER_OFFERED",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrNotFound))
		assert.Equal(t, 0, status)
	})

	t.Run("Unauthorized_Forbidden", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		cashier := createCatalogTestIdentity(t, db, q, []string{auth.RoleCashier}, true, "1234")
		actor := catalog.Actor{StaffID: cashier.StaffID, SessionID: cashier.SessionID}
		handler := catalog.NewRetireSizeHandler(runner)

		catID := createTestCategoryDirect(t, db, "Coffee")
		itemID := createTestItemDirect(t, db, catID, "Latte", nil, false)
		sizeID := createTestSizeDirect(t, db, itemID, "Regular", 40000, false)

		status, _, err := handler.Handle(ctx, actor, catalog.RetireSizeCommand{
			RequestID: uuid.New(),
			SizeID:    sizeID,
			Reason:    "NO_LONGER_OFFERED",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrForbidden))
		assert.Equal(t, 0, status)
	})
}

// ============================================================================
// Retire Modifier Group Tests
// ============================================================================

func TestRetireModifierGroup(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()

	t.Run("Success_MENU_RESTRUCTURE", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRetireModifierGroupHandler(runner)

		groupID := createTestModifierGroupDirect(t, db, "Ice Level", 0, 1, false)
		opt1ID := createTestModifierOptionDirect(t, db, groupID, "No Ice", 0, true, false)
		opt2ID := createTestModifierOptionDirect(t, db, groupID, "Normal Ice", 0, true, false)
		_, err := db.ExecContext(ctx, `INSERT INTO modifier_group_default_options (modifier_group_id, modifier_option_id) VALUES ($1, $2)`, groupID, opt2ID)
		require.NoError(t, err)

		var origUpdatedAt time.Time
		err = db.QueryRowContext(ctx, `SELECT updated_at FROM modifier_groups WHERE id = $1`, groupID).Scan(&origUpdatedAt)
		require.NoError(t, err)

		reqID := uuid.New()
		cmd := catalog.RetireModifierGroupCommand{
			RequestID: reqID,
			GroupID:   groupID,
			Reason:    "MENU_RESTRUCTURE",
		}

		status, res, err := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.Equal(t, groupID, res.ID)
		assert.Equal(t, "Ice Level", res.Name)
		assert.Len(t, res.Options, 2)
		assert.Equal(t, []uuid.UUID{opt2ID}, res.DefaultOptionIDs)

		// Verify database row
		var retiredAt sql.NullTime
		var reason, note sql.NullString
		var dbUpdatedAt time.Time
		err = db.QueryRowContext(ctx, `SELECT retired_at, retirement_reason, retirement_note, updated_at FROM modifier_groups WHERE id = $1`, groupID).
			Scan(&retiredAt, &reason, &note, &dbUpdatedAt)
		require.NoError(t, err)
		assert.True(t, retiredAt.Valid)
		assert.Equal(t, "MENU_RESTRUCTURE", reason.String)
		assert.False(t, note.Valid)
		assert.False(t, dbUpdatedAt.Before(origUpdatedAt))

		// Verify audit event
		var eventType string
		var detailsJSON []byte
		err = db.QueryRowContext(ctx, `SELECT event_type, details FROM audit_events WHERE event_type = 'catalog.modifier_group.retired'`).
			Scan(&eventType, &detailsJSON)
		require.NoError(t, err)
		assert.Equal(t, "catalog.modifier_group.retired", eventType)

		var details map[string]any
		err = json.Unmarshal(detailsJSON, &details)
		require.NoError(t, err)
		assert.Equal(t, groupID.String(), details["group_id"])
		assert.Equal(t, "MENU_RESTRUCTURE", details["reason"])

		_ = opt1ID
	})

	t.Run("Success_OTHER_TrimmedNote", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRetireModifierGroupHandler(runner)

		groupID := createTestModifierGroupDirect(t, db, "Syrup Choice", 0, 1, false)

		reqID := uuid.New()
		cmd := catalog.RetireModifierGroupCommand{
			RequestID: reqID,
			GroupID:   groupID,
			Reason:    "OTHER",
			Note:      "   Replaced by advanced syrup bar   ",
		}

		status, res, err := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.Equal(t, groupID, res.ID)

		var note sql.NullString
		err = db.QueryRowContext(ctx, `SELECT retirement_note FROM modifier_groups WHERE id = $1`, groupID).Scan(&note)
		require.NoError(t, err)
		assert.True(t, note.Valid)
		assert.Equal(t, "Replaced by advanced syrup bar", note.String)
	})

	t.Run("Invalid_OTHER_MissingOrBlankNote", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRetireModifierGroupHandler(runner)

		groupID := createTestModifierGroupDirect(t, db, "Syrup Choice", 0, 1, false)

		status, _, err := handler.Handle(ctx, actor, catalog.RetireModifierGroupCommand{
			RequestID: uuid.New(),
			GroupID:   groupID,
			Reason:    "OTHER",
			Note:      "",
		})
		require.Error(t, err)
		assert.Equal(t, 0, status)
	})

	t.Run("RetainedAssignmentRows", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRetireModifierGroupHandler(runner)

		catID := createTestCategoryDirect(t, db, "Tea")
		price := int64(30000)
		itemID := createTestItemDirect(t, db, catID, "Green Tea", &price, false)
		groupID := createTestModifierGroupDirect(t, db, "Sweetness", 0, 1, false)
		optID := createTestModifierOptionDirect(t, db, groupID, "50%", 0, true, false)

		// Link to category, item, default options
		_, err := db.ExecContext(ctx, `INSERT INTO category_modifier_groups (menu_category_id, modifier_group_id) VALUES ($1, $2)`, catID, groupID)
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, `INSERT INTO item_modifier_groups (menu_item_id, modifier_group_id) VALUES ($1, $2)`, itemID, groupID)
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, `INSERT INTO modifier_group_default_options (modifier_group_id, modifier_option_id) VALUES ($1, $2)`, groupID, optID)
		require.NoError(t, err)

		// Retire modifier group
		status, _, err := handler.Handle(ctx, actor, catalog.RetireModifierGroupCommand{
			RequestID: uuid.New(),
			GroupID:   groupID,
			Reason:    "MENU_RESTRUCTURE",
		})
		require.NoError(t, err)
		assert.Equal(t, 200, status)

		// Verify ALL assignment rows are retained
		var catGroupCount, itemGroupCount, defOptCount, optCount int
		err = db.QueryRowContext(ctx, `SELECT count(*) FROM category_modifier_groups WHERE modifier_group_id = $1`, groupID).Scan(&catGroupCount)
		require.NoError(t, err)
		assert.Equal(t, 1, catGroupCount, "category_modifier_groups row retained")

		err = db.QueryRowContext(ctx, `SELECT count(*) FROM item_modifier_groups WHERE modifier_group_id = $1`, groupID).Scan(&itemGroupCount)
		require.NoError(t, err)
		assert.Equal(t, 1, itemGroupCount, "item_modifier_groups row retained")

		err = db.QueryRowContext(ctx, `SELECT count(*) FROM modifier_group_default_options WHERE modifier_group_id = $1`, groupID).Scan(&defOptCount)
		require.NoError(t, err)
		assert.Equal(t, 1, defOptCount, "modifier_group_default_options row retained")

		err = db.QueryRowContext(ctx, `SELECT count(*) FROM modifier_options WHERE modifier_group_id = $1`, groupID).Scan(&optCount)
		require.NoError(t, err)
		assert.Equal(t, 1, optCount, "modifier_options row retained")
	})

	t.Run("TargetRetirement_PermanentConflict", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRetireModifierGroupHandler(runner)

		groupID := createTestModifierGroupDirect(t, db, "Ice", 0, 1, false)

		status1, _, err1 := handler.Handle(ctx, actor, catalog.RetireModifierGroupCommand{
			RequestID: uuid.New(),
			GroupID:   groupID,
			Reason:    "NO_LONGER_OFFERED",
		})
		require.NoError(t, err1)
		assert.Equal(t, 200, status1)

		// Second request returns ErrEntityRetired
		status2, _, err2 := handler.Handle(ctx, actor, catalog.RetireModifierGroupCommand{
			RequestID: uuid.New(),
			GroupID:   groupID,
			Reason:    "MENU_RESTRUCTURE",
		})
		require.Error(t, err2)
		assert.True(t, errors.Is(err2, catalog.ErrEntityRetired))
		assert.Equal(t, 0, status2)
	})

	t.Run("Replay", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRetireModifierGroupHandler(runner)

		groupID := createTestModifierGroupDirect(t, db, "Ice", 0, 1, false)

		reqID := uuid.New()
		cmd := catalog.RetireModifierGroupCommand{
			RequestID: reqID,
			GroupID:   groupID,
			Reason:    "NO_LONGER_OFFERED",
		}

		status1, res1, err1 := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err1)
		assert.Equal(t, 200, status1)

		status2, res2, err2 := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err2)
		assert.Equal(t, 200, status2)
		assert.Equal(t, res1, res2)

		var auditCount int
		err := db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE event_type = 'catalog.modifier_group.retired'`).Scan(&auditCount)
		require.NoError(t, err)
		assert.Equal(t, 1, auditCount)
	})

	t.Run("NotFound", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRetireModifierGroupHandler(runner)

		status, _, err := handler.Handle(ctx, actor, catalog.RetireModifierGroupCommand{
			RequestID: uuid.New(),
			GroupID:   uuid.New(),
			Reason:    "NO_LONGER_OFFERED",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrNotFound))
		assert.Equal(t, 0, status)
	})

	t.Run("Unauthorized_Forbidden", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		cashier := createCatalogTestIdentity(t, db, q, []string{auth.RoleCashier}, true, "1234")
		actor := catalog.Actor{StaffID: cashier.StaffID, SessionID: cashier.SessionID}
		handler := catalog.NewRetireModifierGroupHandler(runner)

		groupID := createTestModifierGroupDirect(t, db, "Ice", 0, 1, false)

		status, _, err := handler.Handle(ctx, actor, catalog.RetireModifierGroupCommand{
			RequestID: uuid.New(),
			GroupID:   groupID,
			Reason:    "NO_LONGER_OFFERED",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrForbidden))
		assert.Equal(t, 0, status)
	})
}

// ============================================================================
// Retire Modifier Option Tests
// ============================================================================

func TestRetireModifierOption(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()

	t.Run("Success_NO_LONGER_OFFERED", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRetireModifierOptionHandler(runner)

		groupID := createTestModifierGroupDirect(t, db, "Toppings", 0, 3, false)
		optID := createTestModifierOptionDirect(t, db, groupID, "Grass Jelly", 5000, true, false)

		var origUpdatedAt time.Time
		err := db.QueryRowContext(ctx, `SELECT updated_at FROM modifier_options WHERE id = $1`, optID).Scan(&origUpdatedAt)
		require.NoError(t, err)

		reqID := uuid.New()
		cmd := catalog.RetireModifierOptionCommand{
			RequestID: reqID,
			OptionID:  optID,
			Reason:    "NO_LONGER_OFFERED",
		}

		status, res, err := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.Equal(t, optID, res.ID)
		assert.Equal(t, groupID, res.ModifierGroupID)
		assert.Equal(t, "Grass Jelly", res.Name)
		assert.Equal(t, int64(5000), res.SurchargeVND)

		// Verify database row
		var retiredAt sql.NullTime
		var reason, note sql.NullString
		var dbUpdatedAt time.Time
		err = db.QueryRowContext(ctx, `SELECT retired_at, retirement_reason, retirement_note, updated_at FROM modifier_options WHERE id = $1`, optID).
			Scan(&retiredAt, &reason, &note, &dbUpdatedAt)
		require.NoError(t, err)
		assert.True(t, retiredAt.Valid)
		assert.Equal(t, "NO_LONGER_OFFERED", reason.String)
		assert.False(t, note.Valid)
		assert.False(t, dbUpdatedAt.Before(origUpdatedAt))

		// Verify audit event
		var eventType string
		var detailsJSON []byte
		err = db.QueryRowContext(ctx, `SELECT event_type, details FROM audit_events WHERE event_type = 'catalog.modifier_option.retired'`).
			Scan(&eventType, &detailsJSON)
		require.NoError(t, err)
		assert.Equal(t, "catalog.modifier_option.retired", eventType)

		var details map[string]any
		err = json.Unmarshal(detailsJSON, &details)
		require.NoError(t, err)
		assert.Equal(t, optID.String(), details["option_id"])
		assert.Equal(t, groupID.String(), details["modifier_group_id"])
		assert.Equal(t, "NO_LONGER_OFFERED", details["reason"])
	})

	t.Run("Success_OTHER_TrimmedNote", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRetireModifierOptionHandler(runner)

		groupID := createTestModifierGroupDirect(t, db, "Toppings", 0, 3, false)
		optID := createTestModifierOptionDirect(t, db, groupID, "Egg Pudding", 8000, true, false)

		reqID := uuid.New()
		cmd := catalog.RetireModifierOptionCommand{
			RequestID: reqID,
			OptionID:  optID,
			Reason:    "OTHER",
			Note:      "   Ingredient supplier contract expired   ",
		}

		status, res, err := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.Equal(t, optID, res.ID)

		var note sql.NullString
		err = db.QueryRowContext(ctx, `SELECT retirement_note FROM modifier_options WHERE id = $1`, optID).Scan(&note)
		require.NoError(t, err)
		assert.True(t, note.Valid)
		assert.Equal(t, "Ingredient supplier contract expired", note.String)
	})

	t.Run("Invalid_OTHER_MissingOrBlankNote", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRetireModifierOptionHandler(runner)

		groupID := createTestModifierGroupDirect(t, db, "Toppings", 0, 3, false)
		optID := createTestModifierOptionDirect(t, db, groupID, "Aloe Vera", 5000, true, false)

		status, _, err := handler.Handle(ctx, actor, catalog.RetireModifierOptionCommand{
			RequestID: uuid.New(),
			OptionID:  optID,
			Reason:    "OTHER",
			Note:      "",
		})
		require.Error(t, err)
		assert.Equal(t, 0, status)
	})

	t.Run("ParentRetirement", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRetireModifierOptionHandler(runner)

		// Parent modifier group is retired
		retiredGroupID := createTestModifierGroupDirect(t, db, "Retired Group", 0, 1, true)
		optID := createTestModifierOptionDirect(t, db, retiredGroupID, "Opt 1", 0, true, false)

		status, _, err := handler.Handle(ctx, actor, catalog.RetireModifierOptionCommand{
			RequestID: uuid.New(),
			OptionID:  optID,
			Reason:    "NO_LONGER_OFFERED",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrEntityRetired), "expected ErrEntityRetired when parent modifier group is retired, got: %v", err)
		assert.Equal(t, 0, status)
	})

	t.Run("TargetRetirement_PermanentConflict", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRetireModifierOptionHandler(runner)

		groupID := createTestModifierGroupDirect(t, db, "Toppings", 0, 3, false)
		optID := createTestModifierOptionDirect(t, db, groupID, "Pudding", 5000, true, false)

		status1, _, err1 := handler.Handle(ctx, actor, catalog.RetireModifierOptionCommand{
			RequestID: uuid.New(),
			OptionID:  optID,
			Reason:    "NO_LONGER_OFFERED",
		})
		require.NoError(t, err1)
		assert.Equal(t, 200, status1)

		// Second request returns ErrEntityRetired
		status2, _, err2 := handler.Handle(ctx, actor, catalog.RetireModifierOptionCommand{
			RequestID: uuid.New(),
			OptionID:  optID,
			Reason:    "MENU_RESTRUCTURE",
		})
		require.Error(t, err2)
		assert.True(t, errors.Is(err2, catalog.ErrEntityRetired))
		assert.Equal(t, 0, status2)
	})

	t.Run("Replay", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRetireModifierOptionHandler(runner)

		groupID := createTestModifierGroupDirect(t, db, "Toppings", 0, 3, false)
		optID := createTestModifierOptionDirect(t, db, groupID, "Pudding", 5000, true, false)

		reqID := uuid.New()
		cmd := catalog.RetireModifierOptionCommand{
			RequestID: reqID,
			OptionID:  optID,
			Reason:    "NO_LONGER_OFFERED",
		}

		status1, res1, err1 := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err1)
		assert.Equal(t, 200, status1)

		status2, res2, err2 := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err2)
		assert.Equal(t, 200, status2)
		assert.Equal(t, res1, res2)

		var auditCount int
		err := db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE event_type = 'catalog.modifier_option.retired'`).Scan(&auditCount)
		require.NoError(t, err)
		assert.Equal(t, 1, auditCount)
	})

	t.Run("NotFound", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		manager := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, "1234")
		actor := catalog.Actor{StaffID: manager.StaffID, SessionID: manager.SessionID}
		handler := catalog.NewRetireModifierOptionHandler(runner)

		status, _, err := handler.Handle(ctx, actor, catalog.RetireModifierOptionCommand{
			RequestID: uuid.New(),
			OptionID:  uuid.New(),
			Reason:    "NO_LONGER_OFFERED",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrNotFound))
		assert.Equal(t, 0, status)
	})

	t.Run("Unauthorized_Forbidden", func(t *testing.T) {
		cleanCategoryTestTables(t, db)

		cashier := createCatalogTestIdentity(t, db, q, []string{auth.RoleCashier}, true, "1234")
		actor := catalog.Actor{StaffID: cashier.StaffID, SessionID: cashier.SessionID}
		handler := catalog.NewRetireModifierOptionHandler(runner)

		groupID := createTestModifierGroupDirect(t, db, "Toppings", 0, 3, false)
		optID := createTestModifierOptionDirect(t, db, groupID, "Pudding", 5000, true, false)

		status, _, err := handler.Handle(ctx, actor, catalog.RetireModifierOptionCommand{
			RequestID: uuid.New(),
			OptionID:  optID,
			Reason:    "NO_LONGER_OFFERED",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrForbidden))
		assert.Equal(t, 0, status)
	})
}
