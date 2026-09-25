//go:build integration

package catalog_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/catalog"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetAvailabilityBatch(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	handler := catalog.NewSetAvailabilityBatchHandler(runner)
	ctx := context.Background()

	batchAuditCount := func(t *testing.T) int {
		t.Helper()
		var n int
		require.NoError(t, db.QueryRowContext(ctx,
			`SELECT count(*) FROM audit_events WHERE event_type = 'catalog.availability.batch_changed'`).Scan(&n))
		return n
	}
	available := func(t *testing.T, table string, id uuid.UUID) bool {
		t.Helper()
		var v bool
		require.NoError(t, db.QueryRowContext(ctx, `SELECT available FROM `+table+` WHERE id = $1`, id).Scan(&v))
		return v
	}
	barista := func(t *testing.T) catalog.Actor {
		t.Helper()
		ident := createCatalogTestIdentity(t, db, q, []string{auth.RoleBarista}, true, "1234")
		return catalog.Actor{StaffID: ident.StaffID, SessionID: ident.SessionID}
	}

	t.Run("MixedKinds_OneAuditEvent", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		actor := barista(t)

		catID := createTestCategoryDirect(t, db, "Coffee")
		price := int64(45000)
		itemA := createTestItemDirect(t, db, catID, "Americano", &price, false)
		itemB := createTestItemDirect(t, db, catID, "Milk Tea", nil, false)
		sizeL := createTestSizeDirect(t, db, itemB, "Large", 40000, false)
		groupID := createTestModifierGroupDirect(t, db, "Topping", 0, 1, false)
		optX := createTestModifierOptionDirect(t, db, groupID, "Pearl", 5000, true, false)

		status, res, err := handler.Handle(ctx, actor, catalog.SetAvailabilityBatchCommand{
			RequestID: uuid.New(),
			Changes: []catalog.AvailabilityChange{
				{Kind: catalog.AvailabilityKindSize, ID: sizeL, Available: false},
				{Kind: catalog.AvailabilityKindItem, ID: itemA, Available: false},
				{Kind: catalog.AvailabilityKindModifierOption, ID: optX, Available: false},
				{Kind: catalog.AvailabilityKindItem, ID: itemB, Available: true}, // already true: no-op
			},
		})
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		require.Len(t, res.Results, 4)
		// Results come back sorted by (kind, id).
		assert.Equal(t, catalog.AvailabilityKindItem, res.Results[0].Kind)
		assert.Equal(t, catalog.AvailabilityKindItem, res.Results[1].Kind)
		assert.Equal(t, catalog.AvailabilityKindModifierOption, res.Results[2].Kind)
		assert.Equal(t, catalog.AvailabilityKindSize, res.Results[3].Kind)

		changed := map[uuid.UUID]bool{}
		for _, r := range res.Results {
			changed[r.ID] = r.Changed
		}
		assert.True(t, changed[itemA])
		assert.False(t, changed[itemB])
		assert.True(t, changed[sizeL])
		assert.True(t, changed[optX])

		assert.False(t, available(t, "menu_items", itemA))
		assert.True(t, available(t, "menu_items", itemB))
		assert.False(t, available(t, "menu_item_sizes", sizeL))
		assert.False(t, available(t, "modifier_options", optX))

		assert.Equal(t, 1, batchAuditCount(t))
		var detailsJSON []byte
		require.NoError(t, db.QueryRowContext(ctx,
			`SELECT details FROM audit_events WHERE event_type = 'catalog.availability.batch_changed'`).Scan(&detailsJSON))
		var details struct {
			Changes []struct {
				Kind         string    `json:"kind"`
				ID           uuid.UUID `json:"id"`
				OldAvailable bool      `json:"old_available"`
				NewAvailable bool      `json:"new_available"`
			} `json:"changes"`
		}
		require.NoError(t, json.Unmarshal(detailsJSON, &details))
		require.Len(t, details.Changes, 3, "only entries that changed are audited")
		for _, c := range details.Changes {
			assert.True(t, c.OldAvailable)
			assert.False(t, c.NewAvailable)
			assert.NotEqual(t, itemB, c.ID)
		}
	})

	t.Run("AllNoOp_NoAuditEvent", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		actor := barista(t)
		catID := createTestCategoryDirect(t, db, "Coffee")
		price := int64(45000)
		itemA := createTestItemDirect(t, db, catID, "Americano", &price, false)

		_, res, err := handler.Handle(ctx, actor, catalog.SetAvailabilityBatchCommand{
			RequestID: uuid.New(),
			Changes:   []catalog.AvailabilityChange{{Kind: catalog.AvailabilityKindItem, ID: itemA, Available: true}},
		})
		require.NoError(t, err)
		require.Len(t, res.Results, 1)
		assert.False(t, res.Results[0].Changed)
		assert.Equal(t, 0, batchAuditCount(t))
	})

	t.Run("RetiredEntry_RollsBackWholeBatch", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		actor := barista(t)
		catID := createTestCategoryDirect(t, db, "Coffee")
		price := int64(45000)
		itemA := createTestItemDirect(t, db, catID, "Americano", &price, false)
		retired := createTestItemDirect(t, db, catID, "Old Brew", &price, true)

		status, _, err := handler.Handle(ctx, actor, catalog.SetAvailabilityBatchCommand{
			RequestID: uuid.New(),
			Changes: []catalog.AvailabilityChange{
				{Kind: catalog.AvailabilityKindItem, ID: itemA, Available: false},
				{Kind: catalog.AvailabilityKindItem, ID: retired, Available: false},
			},
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrEntityRetired))
		assert.Equal(t, 0, status)
		assert.True(t, available(t, "menu_items", itemA), "the whole batch must roll back")
		assert.Equal(t, 0, batchAuditCount(t))
	})

	t.Run("NotFound", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		actor := barista(t)
		_, _, err := handler.Handle(ctx, actor, catalog.SetAvailabilityBatchCommand{
			RequestID: uuid.New(),
			Changes:   []catalog.AvailabilityChange{{Kind: catalog.AvailabilityKindSize, ID: uuid.New(), Available: true}},
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrNotFound))
	})

	t.Run("InvalidInput", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		actor := barista(t)
		_, _, err := handler.Handle(ctx, actor, catalog.SetAvailabilityBatchCommand{RequestID: uuid.New()})
		require.Error(t, err)
		assert.True(t, errors.Is(err, response.ErrInvalid))
	})

	t.Run("Replay_Reorder_Conflict", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		actor := barista(t)
		catID := createTestCategoryDirect(t, db, "Coffee")
		price := int64(45000)
		itemA := createTestItemDirect(t, db, catID, "Americano", &price, false)
		itemB := createTestItemDirect(t, db, catID, "Latte", &price, false)

		reqID := uuid.New()
		cmd := catalog.SetAvailabilityBatchCommand{
			RequestID: reqID,
			Changes: []catalog.AvailabilityChange{
				{Kind: catalog.AvailabilityKindItem, ID: itemA, Available: false},
				{Kind: catalog.AvailabilityKindItem, ID: itemB, Available: false},
			},
		}
		_, first, err := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err)

		_, replay, err := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		assert.Equal(t, first, replay)

		reordered := catalog.SetAvailabilityBatchCommand{
			RequestID: reqID,
			Changes:   []catalog.AvailabilityChange{cmd.Changes[1], cmd.Changes[0]},
		}
		_, again, err := handler.Handle(ctx, actor, reordered)
		require.NoError(t, err)
		assert.Equal(t, first, again)
		assert.Equal(t, 1, batchAuditCount(t))

		conflict := catalog.SetAvailabilityBatchCommand{
			RequestID: reqID,
			Changes:   []catalog.AvailabilityChange{{Kind: catalog.AvailabilityKindItem, ID: itemA, Available: true}},
		}
		_, _, err = handler.Handle(ctx, actor, conflict)
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrRequestConflict))
	})

	t.Run("Forbidden_MissingCapability", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		catID := createTestCategoryDirect(t, db, "Coffee")
		price := int64(45000)
		itemA := createTestItemDirect(t, db, catID, "Americano", &price, false)
		nobody := createCatalogTestIdentity(t, db, q, []string{}, true, "1234")

		_, _, err := handler.Handle(ctx, catalog.Actor{StaffID: nobody.StaffID, SessionID: nobody.SessionID}, catalog.SetAvailabilityBatchCommand{
			RequestID: uuid.New(),
			Changes:   []catalog.AvailabilityChange{{Kind: catalog.AvailabilityKindItem, ID: itemA, Available: false}},
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrForbidden))
		assert.Equal(t, 1, countAuthorizationDenials(t, db, "catalog.availability.set_batch"))
	})

	t.Run("ConcurrentCrossedBatches_DoNotDeadlock", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		catID := createTestCategoryDirect(t, db, "Coffee")
		itemX := createTestItemDirect(t, db, catID, "Item X", nil, false)
		itemY := createTestItemDirect(t, db, catID, "Item Y", nil, false)
		sizeX := createTestSizeDirect(t, db, itemX, "Regular", 30000, false)
		sizeY := createTestSizeDirect(t, db, itemY, "Regular", 30000, false)
		actorA, actorB := barista(t), barista(t)

		for i := 0; i < 10; i++ {
			next := i%2 == 0
			var wg sync.WaitGroup
			errs := make([]error, 2)
			wg.Add(2)
			go func() {
				defer wg.Done()
				_, _, errs[0] = handler.Handle(ctx, actorA, catalog.SetAvailabilityBatchCommand{
					RequestID: uuid.New(),
					Changes: []catalog.AvailabilityChange{
						{Kind: catalog.AvailabilityKindItem, ID: itemX, Available: !next},
						{Kind: catalog.AvailabilityKindSize, ID: sizeY, Available: next},
					},
				})
			}()
			go func() {
				defer wg.Done()
				_, _, errs[1] = handler.Handle(ctx, actorB, catalog.SetAvailabilityBatchCommand{
					RequestID: uuid.New(),
					Changes: []catalog.AvailabilityChange{
						{Kind: catalog.AvailabilityKindItem, ID: itemY, Available: !next},
						{Kind: catalog.AvailabilityKindSize, ID: sizeX, Available: next},
					},
				})
			}()
			wg.Wait()
			require.NoError(t, errs[0])
			require.NoError(t, errs[1])
		}
	})
}
