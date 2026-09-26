//go:build integration

package catalog_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/catalog"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testManagerPIN = "123456"

func strPtr(s string) *string { return &s }

func managerActor(t *testing.T, db *sql.DB, q *sqlc.Queries) catalog.Actor {
	t.Helper()
	ident := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, testManagerPIN)
	return catalog.Actor{StaffID: ident.StaffID, SessionID: ident.SessionID}
}

func cashierActor(t *testing.T, db *sql.DB, q *sqlc.Queries) catalog.Actor {
	t.Helper()
	ident := createCatalogTestIdentity(t, db, q, []string{auth.RoleCashier}, true, "")
	return catalog.Actor{StaffID: ident.StaffID, SessionID: ident.SessionID}
}

func retireCategoryDirect(t *testing.T, db *sql.DB, id uuid.UUID) {
	t.Helper()
	_, err := db.Exec(`UPDATE menu_categories SET retired_at = now(), retirement_reason = 'NO_LONGER_OFFERED' WHERE id = $1`, id)
	require.NoError(t, err)
}

func auditCount(t *testing.T, db *sql.DB, eventType string) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM audit_events WHERE event_type = $1`, eventType).Scan(&n))
	return n
}

func TestSetItemDetails(t *testing.T) {
	db, q := openExecutorTestDB(t)
	handler := catalog.NewSetItemDetailsHandler(catalog.NewRunner(db, q))
	ctx := context.Background()
	price := int64(29000)

	t.Run("sets then clears the three fields, one audit event each", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		actor := managerActor(t, db, q)
		cat := createTestCategoryDirect(t, db, "Coffee")
		item := createTestItemDirect(t, db, cat, "Ca phe sua da", &price, false)

		status, res, err := handler.Handle(ctx, actor, catalog.SetItemDetailsCommand{
			RequestID: uuid.New(), ItemID: item,
			Code: strPtr("  CFSD "), Badge: strPtr(catalog.BadgeBestSeller), Description: strPtr("  Phin truyền thống "),
		})
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		require.NotNil(t, res.Code)
		assert.Equal(t, "CFSD", *res.Code)
		assert.Equal(t, catalog.BadgeBestSeller, *res.Badge)
		assert.Equal(t, "Phin truyền thống", *res.Description)

		var key string
		require.NoError(t, db.QueryRow(`SELECT normalized_code FROM menu_items WHERE id = $1`, item).Scan(&key))
		assert.Equal(t, "cfsd", key)

		_, res, err = handler.Handle(ctx, actor, catalog.SetItemDetailsCommand{RequestID: uuid.New(), ItemID: item})
		require.NoError(t, err)
		assert.Nil(t, res.Code)
		assert.Nil(t, res.Badge)
		assert.Nil(t, res.Description)
		assert.Equal(t, 2, auditCount(t, db, catalog.EventItemDetailsChanged))
	})

	t.Run("a code used by another active item is ErrCodeConflict", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		actor := managerActor(t, db, q)
		cat := createTestCategoryDirect(t, db, "Coffee")
		a := createTestItemDirect(t, db, cat, "A", &price, false)
		b := createTestItemDirect(t, db, cat, "B", &price, false)
		_, _, err := handler.Handle(ctx, actor, catalog.SetItemDetailsCommand{RequestID: uuid.New(), ItemID: a, Code: strPtr("cf")})
		require.NoError(t, err)
		_, _, err = handler.Handle(ctx, actor, catalog.SetItemDetailsCommand{RequestID: uuid.New(), ItemID: b, Code: strPtr("CF")})
		assert.True(t, errors.Is(err, catalog.ErrCodeConflict), "got %v", err)

		_, _, err = handler.Handle(ctx, actor, catalog.SetItemDetailsCommand{RequestID: uuid.New(), ItemID: a, Code: strPtr("cf"), Badge: strPtr(catalog.BadgeHot)})
		require.NoError(t, err, "an item may keep its own code")
	})

	t.Run("a retired item's code can be reused", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		actor := managerActor(t, db, q)
		cat := createTestCategoryDirect(t, db, "Coffee")
		old := createTestItemDirect(t, db, cat, "Old", &price, true)
		_, err := db.Exec(`UPDATE menu_items SET code = 'cf', normalized_code = 'cf' WHERE id = $1`, old)
		require.NoError(t, err)
		item := createTestItemDirect(t, db, cat, "New", &price, false)
		_, _, err = handler.Handle(ctx, actor, catalog.SetItemDetailsCommand{RequestID: uuid.New(), ItemID: item, Code: strPtr("cf")})
		require.NoError(t, err)
	})

	t.Run("invalid values are INVALID_INPUT", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		actor := managerActor(t, db, q)
		cat := createTestCategoryDirect(t, db, "Coffee")
		item := createTestItemDirect(t, db, cat, "A", &price, false)
		for name, cmd := range map[string]catalog.SetItemDetailsCommand{
			"code with accents": {Code: strPtr("cà phê")},
			"code too long":     {Code: strPtr("abcdefghijklm")},
			"unknown badge":     {Badge: strPtr("FAVORITE")},
			"description 301":   {Description: strPtr(strings.Repeat("a", 301))},
		} {
			cmd.RequestID, cmd.ItemID = uuid.New(), item
			_, _, err := handler.Handle(ctx, actor, cmd)
			assert.True(t, errors.Is(err, response.ErrInvalid), "%s: got %v", name, err)
		}
	})

	t.Run("retired item is ErrEntityRetired", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		actor := managerActor(t, db, q)
		cat := createTestCategoryDirect(t, db, "Coffee")
		item := createTestItemDirect(t, db, cat, "A", &price, true)
		_, _, err := handler.Handle(ctx, actor, catalog.SetItemDetailsCommand{RequestID: uuid.New(), ItemID: item, Badge: strPtr(catalog.BadgeNew)})
		assert.True(t, errors.Is(err, catalog.ErrEntityRetired))
	})

	t.Run("cashier is forbidden", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		cat := createTestCategoryDirect(t, db, "Coffee")
		item := createTestItemDirect(t, db, cat, "A", &price, false)
		_, _, err := handler.Handle(ctx, cashierActor(t, db, q), catalog.SetItemDetailsCommand{RequestID: uuid.New(), ItemID: item})
		assert.True(t, errors.Is(err, catalog.ErrForbidden))
	})

	t.Run("replay returns the stored result; a changed body conflicts", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		actor := managerActor(t, db, q)
		cat := createTestCategoryDirect(t, db, "Coffee")
		item := createTestItemDirect(t, db, cat, "A", &price, false)
		cmd := catalog.SetItemDetailsCommand{RequestID: uuid.New(), ItemID: item, Code: strPtr("aa")}
		_, first, err := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		_, second, err := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		assert.Equal(t, first, second)
		assert.Equal(t, 1, auditCount(t, db, catalog.EventItemDetailsChanged))

		cmd.Code = strPtr("bb")
		_, _, err = handler.Handle(ctx, actor, cmd)
		assert.True(t, errors.Is(err, catalog.ErrRequestConflict))
	})
}

func TestSetCategoryDetails(t *testing.T) {
	db, q := openExecutorTestDB(t)
	handler := catalog.NewSetCategoryDetailsHandler(catalog.NewRunner(db, q))
	ctx := context.Background()

	t.Run("sets icon and order with an audit event", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		cat := createTestCategoryDirect(t, db, "Coffee")
		status, res, err := handler.Handle(ctx, managerActor(t, db, q), catalog.SetCategoryDetailsCommand{
			RequestID: uuid.New(), CategoryID: cat, Icon: strPtr("coffee"), DisplayOrder: 3,
		})
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.Equal(t, "coffee", *res.Icon)
		assert.Equal(t, int32(3), res.DisplayOrder)
		assert.Equal(t, 1, auditCount(t, db, catalog.EventCategoryDetailsChanged))
	})

	t.Run("invalid icon and order are INVALID_INPUT", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		actor := managerActor(t, db, q)
		cat := createTestCategoryDirect(t, db, "Coffee")
		_, _, err := handler.Handle(ctx, actor, catalog.SetCategoryDetailsCommand{RequestID: uuid.New(), CategoryID: cat, Icon: strPtr("Coffee Cup")})
		assert.True(t, errors.Is(err, response.ErrInvalid))
		_, _, err = handler.Handle(ctx, actor, catalog.SetCategoryDetailsCommand{RequestID: uuid.New(), CategoryID: cat, DisplayOrder: 10000})
		assert.True(t, errors.Is(err, response.ErrInvalid))
	})

	t.Run("retired category is ErrEntityRetired", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		cat := createTestCategoryDirect(t, db, "Coffee")
		retireCategoryDirect(t, db, cat)
		_, _, err := handler.Handle(ctx, managerActor(t, db, q), catalog.SetCategoryDetailsCommand{RequestID: uuid.New(), CategoryID: cat})
		assert.True(t, errors.Is(err, catalog.ErrEntityRetired))
	})
}
