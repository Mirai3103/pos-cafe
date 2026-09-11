//go:build integration

package catalog_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/catalog"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func cleanCategoryTestTables(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `
		TRUNCATE TABLE 
			menu_categories, 
			catalog_mutation_requests, 
			audit_events 
		CASCADE;
	`)
	require.NoError(t, err)
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
