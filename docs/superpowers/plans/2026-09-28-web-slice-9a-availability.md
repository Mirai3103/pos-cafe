# Web Slice 9a — Availability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give Manager, Cashier, and Barista a "Món tạm hết" tab under `/settings` that toggles Menu Item, Size, and Modifier Option availability one at a time or restores everything at once through a new atomic Go batch command, and keep every POS's menu fresh when availability changes.

**Architecture:** Go gains `POST /catalog/availability/batch`, a single `ExecuteMutation` command that locks rows in a deadlock-free global order and writes one audit event. The web `/settings` route becomes a layout with per-tab capability guards; its only tab in 9a reads `GET /catalog/menu/availability`, turns it into a pure view model, and issues the existing single PATCH commands or the new batch. The POS sellable menu polls every 30 s and is invalidated globally whenever a mutation fails with a stale-menu code.

**Tech Stack:** Go 1.x, Echo, sqlc, PostgreSQL integration tests (`-tags=integration`), swag; React 19, TanStack Router + Query, orval, Tailwind, lucide-react, `bun test` with `react-dom/server` `renderToString`.

**Spec:** [`docs/superpowers/specs/2026-09-28-web-slice-9a-availability-design.md`](../specs/2026-09-28-web-slice-9a-availability-design.md)

## Global Constraints

- All user-facing copy is Vietnamese, verbatim from the spec: "Món tạm hết", "Tổng danh mục", "Đang còn hàng", "Tạm hết hàng", "Khôi phục tất cả còn hàng", "Chỉ xem món tạm hết", "Topping", "Tất cả", "Còn hàng", "Tạm hết", "Không tìm thấy món hoặc topping phù hợp", "Xóa bộ lọc", "Chưa có món nào trong thực đơn", "Danh sách đã thay đổi, vui lòng kiểm tra lại", "Không bán được".
- Every mutation carries a `request_id` generated once per user intent (`newRequestId()` from `@/lib/command`) and kept across retries of the same intent.
- Touch targets are at least 48px (`min-h-[48px]`), per `design-system/pos-cafe/DESIGN.md`.
- Batch size: 1 to 200 entries. Kinds: exactly `item`, `size`, `modifier_option`.
- Operation name `catalog.availability.set_batch`; audit event type `catalog.availability.batch_changed`.
- Poll intervals: availability tab `15_000` ms; POS sellable menu `30_000` ms.
- Web tests: pure logic and `renderToString` component checks only. No end-to-end, no browser integration tests (slice sequence §3). Go tests: PostgreSQL template integration tests plus plain unit tests.
- The takeaway and dine-in POS paths must behave exactly as before.
- Go commands run from the repo root; web commands run from `web/`.
- Integration tests need PostgreSQL: `make docker-up` once, then `make test-integration-fast` or the `go test -tags=integration` command given per task.

---

## File map

| File | Responsibility |
| --- | --- |
| `internal/catalog/domain.go` | batch kinds, `AvailabilityChange`, `NormalizeAvailabilityChanges`, op/event names |
| `internal/catalog/dto.go` | batch request, command, result, response |
| `internal/catalog/availability_batch.go` (new) | `SetAvailabilityBatchHandler`, lock planning, per-entry apply |
| `internal/catalog/http.go`, `routes.go` | handler and route |
| `internal/catalog/availability_batch_integration_test.go` (new) | command tests |
| `spec/decisions.md` | ADR-055, ADR-056 |
| `web/src/lib/search.ts` (new) | `normalizeVietnamese`, `getAcronym`, `matchesSearch` moved from POS |
| `web/src/lib/menu-staleness.ts` (new) | `MENU_STALE_CODES`, `isMenuStaleError` |
| `web/src/lib/query-client.ts` | invalidate sellable menu on stale-menu errors |
| `web/src/lib/guards.ts` | `requireAnyCapability` |
| `web/src/lib/error-messages.ts` | `ENTITY_RETIRED`, `CATALOG_NOT_FOUND` |
| `web/src/components/feedback/error-toast.tsx` (new) | generic bottom toast (KDS re-exports it) |
| `web/src/components/layout/nav-items.ts` (new) | `NAV_ITEMS`, `visibleNavItems` |
| `web/src/components/layout/pos-header.tsx` | capability-filtered nav |
| `web/src/features/settings/lib/tabs.ts` (new) | `SETTINGS_TABS`, `SETTINGS_CAPABILITIES`, `permittedTabs`, `firstPermittedTab` |
| `web/src/features/settings/lib/availability.ts` (new) | view model, filters, optimistic patch, restore changes, intent id |
| `web/src/features/settings/api/use-availability.ts` (new) | query, single toggle, restore |
| `web/src/features/settings/components/*` (new) | layout, toggle, stats, item card, toppings, restore dialog, board, view |
| `web/src/routes/_app/settings.tsx`, `settings/index.tsx`, `settings/availability.tsx` | nested routes |
| `web/src/features/pos/api/use-pos.ts` | sellable poll |
| `web/src/features/pos/utils/search.ts` | re-export moved helpers |

---

### Task 1: Batch validation and ordering (Go, pure)

**Files:**
- Modify: `internal/catalog/domain.go`
- Test: `internal/catalog/domain_test.go`

**Interfaces:**
- Produces:
  - `const AvailabilityKindItem = "item"`, `AvailabilityKindSize = "size"`, `AvailabilityKindModifierOption = "modifier_option"`
  - `const MaxAvailabilityBatchSize = 200`
  - `const OpAvailabilitySetBatch = "catalog.availability.set_batch"`
  - `const EventAvailabilityBatchChanged = "catalog.availability.batch_changed"`
  - `type AvailabilityChange struct { Kind string; ID uuid.UUID; Available bool }` (json `kind`, `id`, `available`)
  - `func NormalizeAvailabilityChanges(changes []AvailabilityChange) ([]AvailabilityChange, error)` — validates and returns a new slice sorted by `(Kind, ID.String())`; never mutates the input.

- [ ] **Step 1: Write the failing test**

Append to `internal/catalog/domain_test.go`:

```go
func TestNormalizeAvailabilityChanges(t *testing.T) {
	t.Parallel()

	a := uuid.MustParse("00000000-0000-0000-0000-00000000000a")
	b := uuid.MustParse("00000000-0000-0000-0000-00000000000b")

	t.Run("sorts by kind then id without mutating input", func(t *testing.T) {
		t.Parallel()
		in := []catalog.AvailabilityChange{
			{Kind: catalog.AvailabilityKindSize, ID: b, Available: true},
			{Kind: catalog.AvailabilityKindItem, ID: b, Available: false},
			{Kind: catalog.AvailabilityKindItem, ID: a, Available: true},
			{Kind: catalog.AvailabilityKindModifierOption, ID: a, Available: true},
		}
		out, err := catalog.NormalizeAvailabilityChanges(in)
		assert.NoError(t, err)
		assert.Equal(t, []catalog.AvailabilityChange{
			{Kind: catalog.AvailabilityKindItem, ID: a, Available: true},
			{Kind: catalog.AvailabilityKindItem, ID: b, Available: false},
			{Kind: catalog.AvailabilityKindModifierOption, ID: a, Available: true},
			{Kind: catalog.AvailabilityKindSize, ID: b, Available: true},
		}, out)
		assert.Equal(t, catalog.AvailabilityKindSize, in[0].Kind, "input must not be reordered")
	})

	t.Run("same kind and id on different kinds is not a duplicate", func(t *testing.T) {
		t.Parallel()
		_, err := catalog.NormalizeAvailabilityChanges([]catalog.AvailabilityChange{
			{Kind: catalog.AvailabilityKindItem, ID: a},
			{Kind: catalog.AvailabilityKindSize, ID: a},
		})
		assert.NoError(t, err)
	})

	tooMany := make([]catalog.AvailabilityChange, catalog.MaxAvailabilityBatchSize+1)
	for i := range tooMany {
		tooMany[i] = catalog.AvailabilityChange{Kind: catalog.AvailabilityKindItem, ID: uuid.New()}
	}

	invalid := []struct {
		name    string
		changes []catalog.AvailabilityChange
	}{
		{name: "empty", changes: nil},
		{name: "over the limit", changes: tooMany},
		{name: "unknown kind", changes: []catalog.AvailabilityChange{{Kind: "category", ID: a}}},
		{name: "nil id", changes: []catalog.AvailabilityChange{{Kind: catalog.AvailabilityKindItem, ID: uuid.Nil}}},
		{name: "duplicate", changes: []catalog.AvailabilityChange{
			{Kind: catalog.AvailabilityKindItem, ID: a, Available: true},
			{Kind: catalog.AvailabilityKindItem, ID: a, Available: false},
		}},
	}
	for _, tt := range invalid {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := catalog.NormalizeAvailabilityChanges(tt.changes)
			assert.Error(t, err)
		})
	}

	t.Run("exactly the limit is accepted", func(t *testing.T) {
		t.Parallel()
		_, err := catalog.NormalizeAvailabilityChanges(tooMany[:catalog.MaxAvailabilityBatchSize])
		assert.NoError(t, err)
	})
}
```

Also add two lines inside the existing `TestCatalogConstants`:

```go
	assert.Equal(t, "catalog.availability.set_batch", catalog.OpAvailabilitySetBatch)
	assert.Equal(t, "catalog.availability.batch_changed", catalog.EventAvailabilityBatchChanged)
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/catalog/ -run 'TestNormalizeAvailabilityChanges|TestCatalogConstants'`
Expected: build failure, `undefined: catalog.AvailabilityChange`.

- [ ] **Step 3: Write minimal implementation**

In `internal/catalog/domain.go`, add `OpAvailabilitySetBatch = "catalog.availability.set_batch"` to the `const` block that holds `OpModifierOptionSetAvailability`, and `EventAvailabilityBatchChanged = "catalog.availability.batch_changed"` to the block that holds `EventModifierOptionAvailabilityChanged`. Then append:

```go
// Availability batch entry kinds.
const (
	AvailabilityKindItem           = "item"
	AvailabilityKindSize           = "size"
	AvailabilityKindModifierOption = "modifier_option"
)

// MaxAvailabilityBatchSize bounds one batch availability command.
const MaxAvailabilityBatchSize = 200

// AvailabilityChange is one entry of a batch availability command.
type AvailabilityChange struct {
	Kind      string    `json:"kind"`
	ID        uuid.UUID `json:"id"`
	Available bool      `json:"available"`
}

// NormalizeAvailabilityChanges validates a batch and returns a copy sorted by
// (kind, id), so that the same set of changes in any order fingerprints alike.
func NormalizeAvailabilityChanges(changes []AvailabilityChange) ([]AvailabilityChange, error) {
	if len(changes) == 0 {
		return nil, fmt.Errorf("changes must not be empty")
	}
	if len(changes) > MaxAvailabilityBatchSize {
		return nil, fmt.Errorf("changes must hold at most %d entries", MaxAvailabilityBatchSize)
	}
	seen := make(map[string]struct{}, len(changes))
	out := make([]AvailabilityChange, len(changes))
	for i, c := range changes {
		switch c.Kind {
		case AvailabilityKindItem, AvailabilityKindSize, AvailabilityKindModifierOption:
		default:
			return nil, fmt.Errorf("unknown availability kind %q", c.Kind)
		}
		if c.ID == uuid.Nil {
			return nil, fmt.Errorf("change %d has no id", i)
		}
		key := c.Kind + ":" + c.ID.String()
		if _, dup := seen[key]; dup {
			return nil, fmt.Errorf("duplicate change for %s %s", c.Kind, c.ID)
		}
		seen[key] = struct{}{}
		out[i] = c
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].ID.String() < out[j].ID.String()
	})
	return out, nil
}
```

`fmt`, `sort`, and `uuid` are already imported by `domain.go`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/catalog/ -run 'TestNormalizeAvailabilityChanges|TestCatalogConstants'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/catalog/domain.go internal/catalog/domain_test.go
git commit -m "feat(catalog): validate and order availability batch changes"
```

---

### Task 2: Batch availability command (Go)

**Files:**
- Modify: `internal/catalog/dto.go`
- Create: `internal/catalog/availability_batch.go`
- Test: `internal/catalog/availability_batch_integration_test.go`

**Interfaces:**
- Consumes: Task 1's `AvailabilityChange`, `NormalizeAvailabilityChanges`, kind/op/event constants; existing `ExecuteMutation`, `MutationSpec`, `AuditRecord`, `lockSizeWithParentCheck`, `lockModifierOptionWithParentCheck`, `MapDBError`, `ErrEntityRetired`, `response.ErrInvalid`; sqlc `GetMenuItemForUpdate`, `GetMenuItemSizeByID`, `GetModifierOptionByID`, `SetMenuItemAvailability`, `SetMenuItemSizeAvailability`, `SetModifierOptionAvailability`.
- Produces:
  - `type SetAvailabilityBatchRequest struct { RequestID uuid.UUID; Changes []AvailabilityChange }` (json `request_id`, `changes`)
  - `type SetAvailabilityBatchCommand struct { RequestID uuid.UUID; Changes []AvailabilityChange }`
  - `type AvailabilityBatchResult struct { Kind string; ID uuid.UUID; Available bool; Changed bool }` (json `kind`, `id`, `available`, `changed`)
  - `type AvailabilityBatchResponse struct { Results []AvailabilityBatchResult }` (json `results`)
  - `func NewSetAvailabilityBatchHandler(runner *Runner) *SetAvailabilityBatchHandler`
  - `func (h *SetAvailabilityBatchHandler) Handle(ctx context.Context, actor Actor, cmd SetAvailabilityBatchCommand) (int, AvailabilityBatchResponse, error)`

- [ ] **Step 1: Write the failing tests**

Create `internal/catalog/availability_batch_integration_test.go`. The helpers `openExecutorTestDB`, `cleanCategoryTestTables`, `createCatalogTestIdentity`, `createTestCategoryDirect`, `createTestItemDirect`, `createTestSizeDirect`, `createTestModifierGroupDirect`, `createTestModifierOptionDirect`, and `countAuthorizationDenials` already exist in this test package.

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `TEST_DATABASE_URL="postgres://postgres:postgres@localhost:5432/cafe_pos_test?sslmode=disable" go test -count=1 -tags=integration ./internal/catalog/ -run TestSetAvailabilityBatch`
(Use the `TEST_DATABASE_URL` value defined at the top of the `Makefile` if it differs.)
Expected: build failure, `undefined: catalog.NewSetAvailabilityBatchHandler`.

- [ ] **Step 3: Add the DTOs**

Append to `internal/catalog/dto.go`, next to `SetAvailabilityRequest`:

```go
// SetAvailabilityBatchRequest carries the request ID and a batch of availability changes.
type SetAvailabilityBatchRequest struct {
	RequestID uuid.UUID            `json:"request_id"`
	Changes   []AvailabilityChange `json:"changes"`
}

// SetAvailabilityBatchCommand carries the parameters for one batch availability change.
type SetAvailabilityBatchCommand struct {
	RequestID uuid.UUID            `json:"request_id"`
	Changes   []AvailabilityChange `json:"changes"`
}

// AvailabilityBatchResult reports the outcome for one requested entry.
type AvailabilityBatchResult struct {
	Kind      string    `json:"kind"`
	ID        uuid.UUID `json:"id"`
	Available bool      `json:"available"`
	Changed   bool      `json:"changed"`
}

// AvailabilityBatchResponse lists every requested entry in (kind, id) order.
type AvailabilityBatchResponse struct {
	Results []AvailabilityBatchResult `json:"results"`
}
```

- [ ] **Step 4: Write the handler**

Create `internal/catalog/availability_batch.go`:

```go
package catalog

import (
	"context"
	"fmt"
	"sort"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
)

type availabilityBatchFingerprint struct {
	Changes []AvailabilityChange `json:"changes"`
}

type availabilityBatchAuditChange struct {
	Kind         string    `json:"kind"`
	ID           uuid.UUID `json:"id"`
	OldAvailable bool      `json:"old_available"`
	NewAvailable bool      `json:"new_available"`
}

type availabilityBatchChangedAuditDetails struct {
	Changes []availabilityBatchAuditChange `json:"changes"`
}

// SetAvailabilityBatchHandler applies many availability changes atomically.
type SetAvailabilityBatchHandler struct {
	runner *Runner
}

// NewSetAvailabilityBatchHandler creates a new SetAvailabilityBatchHandler.
func NewSetAvailabilityBatchHandler(runner *Runner) *SetAvailabilityBatchHandler {
	return &SetAvailabilityBatchHandler{runner: runner}
}

// Handle executes the batch availability command. Every entry succeeds or the
// whole batch rolls back; one audit event lists the entries that changed.
func (h *SetAvailabilityBatchHandler) Handle(ctx context.Context, actor Actor, cmd SetAvailabilityBatchCommand) (int, AvailabilityBatchResponse, error) {
	changes, err := NormalizeAvailabilityChanges(cmd.Changes)
	if err != nil {
		return 0, AvailabilityBatchResponse{}, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error())
	}

	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpAvailabilitySetBatch,
		Fingerprint: availabilityBatchFingerprint{Changes: changes},
		Required:    []string{CapManageAvailability},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec, func(q *sqlc.Queries) (int, AvailabilityBatchResponse, AuditRecord, error) {
		steps, err := planAvailabilityLocks(ctx, q, changes)
		if err != nil {
			return 0, AvailabilityBatchResponse{}, AuditRecord{}, err
		}

		previous := make(map[string]bool, len(steps))
		for _, step := range steps {
			old, err := applyAvailabilityChange(ctx, q, step.change)
			if err != nil {
				return 0, AvailabilityBatchResponse{}, AuditRecord{}, err
			}
			previous[availabilityKey(step.change)] = old
		}

		res := AvailabilityBatchResponse{Results: make([]AvailabilityBatchResult, 0, len(changes))}
		var audited []availabilityBatchAuditChange
		for _, c := range changes {
			old := previous[availabilityKey(c)]
			changed := old != c.Available
			res.Results = append(res.Results, AvailabilityBatchResult{
				Kind: c.Kind, ID: c.ID, Available: c.Available, Changed: changed,
			})
			if changed {
				audited = append(audited, availabilityBatchAuditChange{
					Kind: c.Kind, ID: c.ID, OldAvailable: old, NewAvailable: c.Available,
				})
			}
		}

		if len(audited) == 0 {
			return 200, res, AuditRecord{}, nil
		}
		return 200, res, AuditRecord{
			EventType: EventAvailabilityBatchChanged,
			Details:   availabilityBatchChangedAuditDetails{Changes: audited},
		}, nil
	})
}

func availabilityKey(c AvailabilityChange) string {
	return c.Kind + ":" + c.ID.String()
}

// availabilityLockStep places one change in the global lock order.
type availabilityLockStep struct {
	change     AvailabilityChange
	treeRank   int       // 0: Menu Item tree, 1: Modifier Group tree
	parentID   uuid.UUID // the Menu Item or Modifier Group that is locked first
	childRank  int       // 0: the parent itself, 1: a child of it
}

// planAvailabilityLocks resolves each entry's parent without locking, then
// orders the entries so that every batch locks rows in one global order:
// Menu Items by id, each followed by its Sizes; then Modifier Groups by id,
// each followed by its Options. The lock helpers take the parent before the
// child, so ordering by (kind, id) alone could deadlock crossed batches.
func planAvailabilityLocks(ctx context.Context, q *sqlc.Queries, changes []AvailabilityChange) ([]availabilityLockStep, error) {
	steps := make([]availabilityLockStep, 0, len(changes))
	for _, c := range changes {
		step := availabilityLockStep{change: c}
		switch c.Kind {
		case AvailabilityKindItem:
			step.treeRank, step.parentID, step.childRank = 0, c.ID, 0
		case AvailabilityKindSize:
			row, err := q.GetMenuItemSizeByID(ctx, c.ID)
			if err != nil {
				return nil, MapDBError(err)
			}
			step.treeRank, step.parentID, step.childRank = 0, row.MenuItemID, 1
		case AvailabilityKindModifierOption:
			row, err := q.GetModifierOptionByID(ctx, c.ID)
			if err != nil {
				return nil, MapDBError(err)
			}
			step.treeRank, step.parentID, step.childRank = 1, row.ModifierGroupID, 1
		}
		steps = append(steps, step)
	}
	sort.Slice(steps, func(i, j int) bool {
		a, b := steps[i], steps[j]
		if a.treeRank != b.treeRank {
			return a.treeRank < b.treeRank
		}
		if a.parentID != b.parentID {
			return a.parentID.String() < b.parentID.String()
		}
		if a.childRank != b.childRank {
			return a.childRank < b.childRank
		}
		return a.change.ID.String() < b.change.ID.String()
	})
	return steps, nil
}

// applyAvailabilityChange locks one entity, refuses a retired one, writes the
// new state unless it is already current, and returns the previous state.
func applyAvailabilityChange(ctx context.Context, q *sqlc.Queries, c AvailabilityChange) (bool, error) {
	switch c.Kind {
	case AvailabilityKindItem:
		existing, err := q.GetMenuItemForUpdate(ctx, c.ID)
		if err != nil {
			return false, MapDBError(err)
		}
		if existing.RetiredAt.Valid {
			return false, ErrEntityRetired
		}
		if existing.Available != c.Available {
			if _, err := q.SetMenuItemAvailability(ctx, sqlc.SetMenuItemAvailabilityParams{ID: c.ID, Available: c.Available}); err != nil {
				return false, MapDBError(err)
			}
		}
		return existing.Available, nil
	case AvailabilityKindSize:
		existing, err := lockSizeWithParentCheck(ctx, q, c.ID)
		if err != nil {
			return false, err
		}
		if existing.Available != c.Available {
			if _, err := q.SetMenuItemSizeAvailability(ctx, sqlc.SetMenuItemSizeAvailabilityParams{ID: c.ID, Available: c.Available}); err != nil {
				return false, MapDBError(err)
			}
		}
		return existing.Available, nil
	case AvailabilityKindModifierOption:
		existing, err := lockModifierOptionWithParentCheck(ctx, q, c.ID)
		if err != nil {
			return false, err
		}
		if existing.Available != c.Available {
			if _, err := q.SetModifierOptionAvailability(ctx, sqlc.SetModifierOptionAvailabilityParams{ID: c.ID, Available: c.Available}); err != nil {
				return false, MapDBError(err)
			}
		}
		return existing.Available, nil
	}
	return false, fmt.Errorf("%w: unknown availability kind %q", response.ErrInvalid, c.Kind)
}
```

Check against the generated code before running: `internal/database/sqlc/catalog.sql.go` must show `GetMenuItemSizeByID` returning a row with `MenuItemID`, `GetModifierOptionByID` returning a row with `ModifierGroupID`, and the three `Set…Availability` functions returning `(row, error)`. If one returns only `error`, drop the `_ ,` from that call.

- [ ] **Step 5: Run tests to verify they pass**

Run: `gofmt -w internal/catalog && go vet ./internal/catalog/ && TEST_DATABASE_URL="<value from Makefile>" go test -count=1 -race -tags=integration ./internal/catalog/ -run TestSetAvailabilityBatch`
Expected: all subtests PASS, including `ConcurrentCrossedBatches_DoNotDeadlock`.

- [ ] **Step 6: Commit**

```bash
git add internal/catalog/dto.go internal/catalog/availability_batch.go internal/catalog/availability_batch_integration_test.go
git commit -m "feat(catalog): atomic batch availability command"
```

---

### Task 3: HTTP route, Swagger, generated client, ADR-055

**Files:**
- Modify: `internal/catalog/http.go`, `internal/catalog/routes.go`, `spec/decisions.md`
- Test: `internal/catalog/routes_test.go`
- Regenerate: `docs/docs.go`, `docs/swagger.json`, `docs/swagger.yaml`, `web/src/api/generated/**`

**Interfaces:**
- Consumes: Task 2's handler and DTOs.
- Produces: `POST /api/v1/catalog/availability/batch`; orval exports `usePostCatalogAvailabilityBatch` in `web/src/api/generated/endpoints/catalog/catalog.ts` with variables `{ data: CatalogSetAvailabilityBatchRequest }`, and models `CatalogAvailabilityChange`, `CatalogSetAvailabilityBatchRequest`, `CatalogAvailabilityBatchResponse`, `CatalogAvailabilityBatchResult`.

- [ ] **Step 1: Write the failing route test**

Append to `internal/catalog/routes_test.go`:

```go
func TestAvailabilityBatchRoute(t *testing.T) {
	tc := setupHTTPTest(t)
	path := "/api/v1/catalog/availability/batch"

	t.Run("rejects unauthenticated", func(t *testing.T) {
		rec := doJSONRequest(t, tc.e, http.MethodPost, path, "", catalog.SetAvailabilityBatchRequest{RequestID: uuid.New()})
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("requires request_id", func(t *testing.T) {
		rec := doJSONRequest(t, tc.e, http.MethodPost, path, tc.baristaToken, map[string]any{
			"changes": []map[string]any{{"kind": "item", "id": uuid.New(), "available": true}},
		})
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("empty batch is INVALID_INPUT", func(t *testing.T) {
		rec := doJSONRequest(t, tc.e, http.MethodPost, path, tc.baristaToken, catalog.SetAvailabilityBatchRequest{RequestID: uuid.New()})
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		var res response.APIResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
		assert.Equal(t, "INVALID_INPUT", res.Error.Code)
	})

	t.Run("unknown entity is CATALOG_NOT_FOUND for a cashier", func(t *testing.T) {
		rec := doJSONRequest(t, tc.e, http.MethodPost, path, tc.cashierToken, catalog.SetAvailabilityBatchRequest{
			RequestID: uuid.New(),
			Changes:   []catalog.AvailabilityChange{{Kind: catalog.AvailabilityKindItem, ID: uuid.New(), Available: true}},
		})
		assert.Equal(t, http.StatusNotFound, rec.Code)
		var res response.APIResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
		assert.Equal(t, "CATALOG_NOT_FOUND", res.Error.Code)
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `TEST_DATABASE_URL="<value from Makefile>" go test -count=1 -tags=integration ./internal/catalog/ -run TestAvailabilityBatchRoute`
Expected: FAIL, 404 instead of the asserted codes (route not registered). The unauthenticated case may already pass because the group requires auth.

- [ ] **Step 3: Register the handler and route**

In `internal/catalog/routes.go`: add the field `SetAvailabilityBatch *SetAvailabilityBatchHandler` to `Slices` after `SetModifierOptionAvailability`, initialise it in `NewSlices` with `SetAvailabilityBatch: NewSetAvailabilityBatchHandler(runner),`, and register, next to the other availability routes:

```go
	catalog.POST("/availability/batch", s.handleSetAvailabilityBatch, authn.RequireCapability(CapManageAvailability))
```

In `internal/catalog/http.go`, after `handleSetModifierOptionAvailability`:

```go
// handleSetAvailabilityBatch godoc
//
//	@Summary		Cập nhật trạng thái khả dụng hàng loạt
//	@Description	Bật hoặc tắt nhiều món, kích cỡ và tùy chọn trong một giao dịch. Thành công toàn bộ hoặc không thay đổi gì. Yêu cầu quyền catalog.manage_availability.
//	@Tags			Catalog
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		SetAvailabilityBatchRequest	true	"Danh sách thay đổi (1 đến 200 mục)"
//	@Success		200		{object}	response.APIResponse{data=AvailabilityBatchResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Failure		500		{object}	response.APIResponse
//	@Router			/catalog/availability/batch [post]
func (s *Slices) handleSetAvailabilityBatch(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	req, err := bindBody[SetAvailabilityBatchRequest](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(req.RequestID); err != nil {
		return sendError(c, err)
	}
	status, res, err := s.SetAvailabilityBatch.Handle(c.Request().Context(), actor, SetAvailabilityBatchCommand{
		RequestID: req.RequestID,
		Changes:   req.Changes,
	})
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `TEST_DATABASE_URL="<value from Makefile>" go test -count=1 -tags=integration ./internal/catalog/ -run 'TestAvailabilityBatchRoute|TestCatalogRoutes'`
Expected: PASS.

- [ ] **Step 5: Regenerate Swagger and the web client**

Run: `make swagger && cd web && bun run codegen && bunx tsc -b`
Expected: `docs/swagger.yaml` contains `/catalog/availability/batch`; `web/src/api/generated/endpoints/catalog/catalog.ts` exports `usePostCatalogAvailabilityBatch`; `tsc` passes. If `swag` is missing, install it with `go install github.com/swaggo/swag/cmd/swag@latest`.

- [ ] **Step 6: Record ADR-055**

Append to `spec/decisions.md`, following the ADR-054 layout:

```markdown
## ADR-055: Bulk availability is one atomic, explicit-list command with one audit event

* **Decision Date:** 2026-09-28
* **Status:** Accepted
* **Context:** Web slice 9a's "Khôi phục tất cả còn hàng" must turn many Menu Items, Sizes, and Modifier Options back on at once. The catalog had only single-entity availability commands, each writing one audit event.
* **Decision:**
* `POST /catalog/availability/batch` takes an explicit list of `{kind, id, available}` (1 to 200 entries) and applies it inside one `ExecuteMutation` transaction: all entries succeed or none do. A retired entity is `409 ENTITY_RETIRED`; an unknown one is `404 CATALOG_NOT_FOUND`.
* Same-state entries are no-ops, as in the single commands. One `catalog.availability.batch_changed` audit event lists only the entries that changed; a batch that changes nothing writes none.
* The fingerprint is the list sorted by `(kind, id)`. Rows are locked in one global order (Menu Items then their Sizes, then Modifier Groups then their Options) so that crossed concurrent batches cannot deadlock.
* **Rejected:** a server-scoped "restore all", which would turn back on an entity another terminal marked unavailable after the operator last saw the list; and one audit event per entity, which would require `ExecuteMutation` to accept several `AuditRecord`s for one caller.
* **Consequences:** The client must send the list it displayed. One user intent produces one audit event, consistent with ADR-048.
```

- [ ] **Step 7: Commit**

```bash
git add internal/catalog/http.go internal/catalog/routes.go internal/catalog/routes_test.go docs/ web/src/api/generated spec/decisions.md
git commit -m "feat(catalog): expose batch availability route, regenerate client"
```

---

### Task 4: Shared search, stale-menu invalidation, POS polling, error messages

**Files:**
- Create: `web/src/lib/search.ts`, `web/src/lib/search.test.ts`, `web/src/lib/menu-staleness.ts`, `web/src/lib/menu-staleness.test.ts`
- Modify: `web/src/features/pos/utils/search.ts`, `web/src/lib/query-client.ts`, `web/src/features/pos/api/use-pos.ts`, `web/src/lib/error-messages.ts`, `web/src/lib/error-messages.test.ts`

**Interfaces:**
- Produces:
  - `normalizeVietnamese(text: string): string`, `getAcronym(text: string): string`, `matchesSearch(query: string, itemName: string, categoryName?: string): boolean` from `@/lib/search`
  - `MENU_STALE_CODES: ReadonlySet<string>`, `isMenuStaleError(error: unknown): boolean` from `@/lib/menu-staleness`
  - `SELLABLE_MENU_POLL_MS = 30_000` from `@/features/pos/api/use-pos`

- [ ] **Step 1: Write the failing tests**

`web/src/lib/search.test.ts`:

```ts
import { describe, expect, it } from "bun:test";
import { matchesSearch, normalizeVietnamese } from "./search";

describe("lib/search", () => {
  it("strips diacritics and d-stroke", () => {
    expect(normalizeVietnamese("  Bạc  Xỉu Đá ")).toBe("bac xiu da");
  });

  it("matches without diacritics and by cafe acronym", () => {
    expect(matchesSearch("ca phe sua", "Cà phê sữa đá")).toBe(true);
    expect(matchesSearch("cfsd", "Cà phê sữa đá")).toBe(true);
    expect(matchesSearch("tra", "Cà phê sữa đá")).toBe(false);
  });
});
```

`web/src/lib/menu-staleness.test.ts`:

```ts
import { describe, expect, it } from "bun:test";
import { ApiError } from "./unwrap";
import { MENU_STALE_CODES, isMenuStaleError } from "./menu-staleness";

describe("isMenuStaleError", () => {
  it("recognises draft and commit availability refusals", () => {
    for (const code of [
      "MENU_ITEM_UNAVAILABLE",
      "SIZE_RETIRED",
      "MODIFIER_OPTION_UNAVAILABLE",
      "COMMIT_MENU_ITEM_UNAVAILABLE",
      "COMMIT_MODIFIER_GROUP_RETIRED",
    ]) {
      expect(isMenuStaleError(new ApiError(409, code, "x"))).toBe(true);
    }
  });

  it("ignores unrelated failures", () => {
    expect(isMenuStaleError(new ApiError(409, "EMPTY_DRAFT", "x"))).toBe(false);
    expect(isMenuStaleError(new Error("network"))).toBe(false);
  });

  it("holds exactly thirteen codes", () => {
    expect(MENU_STALE_CODES.size).toBe(13);
  });
});
```

Append to `web/src/lib/error-messages.test.ts` (inside the file's existing top-level `describe`, or as a new one if it has none):

```ts
describe("catalog availability messages", () => {
  it("translates retirement and missing entities", () => {
    expect(messageForError(new ApiError(409, "ENTITY_RETIRED", "x"))).toBe("Mục này đã ngừng kinh doanh.");
    expect(messageForError(new ApiError(404, "CATALOG_NOT_FOUND", "x"))).toBe("Không tìm thấy mục trong thực đơn.");
  });
});
```

Add the imports `messageForError` and `ApiError` if the file does not already have them.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd web && bun test src/lib`
Expected: FAIL — `Cannot find module './search'`, `'./menu-staleness'`, and the two message assertions.

- [ ] **Step 3: Move the search helpers**

Create `web/src/lib/search.ts` holding `normalizeVietnamese`, `getAcronym`, and `matchesSearch`, cut **verbatim** (bodies and doc comments) from `web/src/features/pos/utils/search.ts`. In `web/src/features/pos/utils/search.ts`, delete those three functions and add at the top:

```ts
import { matchesSearch } from "@/lib/search";

export { normalizeVietnamese, getAcronym, matchesSearch } from "@/lib/search";
```

`filterSellableItems` stays in the POS file and keeps calling `matchesSearch`. The existing `search.test.ts` keeps importing from `./search` unchanged.

- [ ] **Step 4: Add stale-menu detection and wire it**

`web/src/lib/menu-staleness.ts`:

```ts
import { ApiError } from "./unwrap";

/**
 * Refusals that mean this terminal's sellable menu is out of date: another
 * terminal marked something unavailable or retired it.
 */
export const MENU_STALE_CODES: ReadonlySet<string> = new Set([
  "MENU_ITEM_UNAVAILABLE",
  "MENU_ITEM_RETIRED",
  "SIZE_UNAVAILABLE",
  "SIZE_RETIRED",
  "MODIFIER_OPTION_UNAVAILABLE",
  "MODIFIER_OPTION_RETIRED",
  "COMMIT_MENU_ITEM_UNAVAILABLE",
  "COMMIT_MENU_ITEM_RETIRED",
  "COMMIT_SIZE_UNAVAILABLE",
  "COMMIT_SIZE_RETIRED",
  "COMMIT_MODIFIER_OPTION_UNAVAILABLE",
  "COMMIT_MODIFIER_OPTION_RETIRED",
  "COMMIT_MODIFIER_GROUP_RETIRED",
]);

export function isMenuStaleError(error: unknown): boolean {
  return error instanceof ApiError && MENU_STALE_CODES.has(error.code);
}
```

In `web/src/lib/query-client.ts`, add imports:

```ts
import { getGetCatalogMenuSellableQueryKey } from "@/api/generated/endpoints/catalog/catalog";
import { isMenuStaleError } from "./menu-staleness";
```

annotate the export as `export const queryClient: QueryClient = new QueryClient({` (the explicit type keeps TypeScript from rejecting the self-reference below), and replace the `mutationCache` line:

```ts
  mutationCache: new MutationCache({
    onError: (error) => {
      void resolveForbidden(error);
      // A refusal for an unavailable or retired entry means this terminal's
      // menu is stale; refresh it now rather than at the next poll.
      if (isMenuStaleError(error)) {
        void queryClient.invalidateQueries({ queryKey: getGetCatalogMenuSellableQueryKey() });
      }
    },
  }),
```

(`queryClient` is referenced only when the callback runs, after construction, so the self-reference is safe.)

In `web/src/features/pos/api/use-pos.ts`, add above `useSellableMenu`:

```ts
/** Another terminal may mark items unavailable; there is no push channel. */
export const SELLABLE_MENU_POLL_MS = 30_000;
```

and add `refetchInterval: SELLABLE_MENU_POLL_MS,` next to `staleTime: 60_000` in `useSellableMenu`.

In `web/src/lib/error-messages.ts`, add to `ERROR_MESSAGES`:

```ts
  ENTITY_RETIRED: "Mục này đã ngừng kinh doanh.",
  CATALOG_NOT_FOUND: "Không tìm thấy mục trong thực đơn.",
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd web && bun test src/lib src/features/pos && bunx tsc -b && bun run lint`
Expected: PASS, including the unchanged POS `search.test.ts`.

- [ ] **Step 6: Commit**

```bash
git add web/src/lib web/src/features/pos/utils/search.ts web/src/features/pos/api/use-pos.ts
git commit -m "feat(web): share search helpers, refresh a stale POS menu"
```

---

### Task 5: Settings tabs, guard, navigation, nested routes, ADR-056

**Files:**
- Create: `web/src/features/settings/lib/tabs.ts`, `web/src/features/settings/lib/tabs.test.ts`, `web/src/lib/guards.test.ts`, `web/src/components/layout/nav-items.ts`, `web/src/components/layout/nav-items.test.ts`, `web/src/routes/_app/settings/index.tsx`, `web/src/routes/_app/settings/availability.tsx`, `web/src/features/settings/components/settings-layout.tsx`
- Modify: `web/src/lib/guards.ts`, `web/src/components/layout/pos-header.tsx`, `web/src/routes/_app/settings.tsx`, `spec/decisions.md`
- Delete: `web/src/features/settings/components/settings-view.tsx`
- Regenerate: `web/src/routeTree.gen.ts`

**Interfaces:**
- Produces:
  - `requireAnyCapability(capabilities: readonly string[]): void` from `@/lib/guards`
  - `interface SettingsTab { to: "/settings/availability"; label: string; icon: LucideIcon; capability: string }`
  - `SETTINGS_TABS: readonly SettingsTab[]`, `SETTINGS_CAPABILITIES: readonly string[]`, `permittedTabs(held: readonly string[]): SettingsTab[]`, `firstPermittedTab(held: readonly string[]): SettingsTab | null`
  - `NAV_ITEMS`, `visibleNavItems(held: readonly string[]): NavItem[]` from `@/components/layout/nav-items`
  - `SettingsLayout` component; the route file `settings/availability.tsx` renders `AvailabilityView` from Task 8 — until then it renders `null` (see Step 5).

- [ ] **Step 1: Write the failing tests**

`web/src/lib/guards.test.ts`:

```ts
import { beforeEach, describe, expect, it } from "bun:test";
import { isRedirect } from "@tanstack/react-router";
import { useSessionStore } from "@/stores/use-session-store";
import { requireAnyCapability, requireCapability } from "./guards";

function thrown(fn: () => void): unknown {
  try {
    fn();
  } catch (e) {
    return e;
  }
  return undefined;
}

function redirectTarget(e: unknown): string | undefined {
  return isRedirect(e) ? (e.options.to as string) : undefined;
}

describe("requireAnyCapability", () => {
  beforeEach(() => {
    useSessionStore.setState({ state: "authenticated", capabilities: ["catalog.manage_availability"] });
  });

  it("passes when any listed capability is held", () => {
    expect(thrown(() => requireAnyCapability(["staff.administer", "catalog.manage_availability"]))).toBeUndefined();
  });

  it("redirects to /no-access when none is held", () => {
    expect(redirectTarget(thrown(() => requireAnyCapability(["staff.administer"])))).toBe("/no-access");
  });

  it("does not evaluate capabilities while locked", () => {
    useSessionStore.setState({ state: "locked", capabilities: [] });
    expect(thrown(() => requireAnyCapability(["staff.administer"]))).toBeUndefined();
  });

  it("sends a signed-out session to login", () => {
    useSessionStore.setState({ state: "signed_out", capabilities: [] });
    expect(redirectTarget(thrown(() => requireAnyCapability(["x"])))).toBe("/auth/login");
  });

  it("keeps requireCapability behaving as before", () => {
    expect(thrown(() => requireCapability("catalog.manage_availability"))).toBeUndefined();
    expect(redirectTarget(thrown(() => requireCapability("staff.administer")))).toBe("/no-access");
  });
});
```

`web/src/features/settings/lib/tabs.test.ts`:

```ts
import { describe, expect, it } from "bun:test";
import { SETTINGS_CAPABILITIES, firstPermittedTab, permittedTabs } from "./tabs";

describe("settings tabs", () => {
  it("lists availability for anyone who manages availability", () => {
    expect(firstPermittedTab(["catalog.manage_availability"])?.to).toBe("/settings/availability");
    expect(permittedTabs(["catalog.manage_availability"]).map((t) => t.label)).toEqual(["Món tạm hết"]);
  });

  it("has no tab for a session without a settings capability", () => {
    expect(firstPermittedTab(["sales.operate"])).toBeNull();
  });

  it("exposes every tab capability for the layout guard", () => {
    expect(SETTINGS_CAPABILITIES).toEqual(["catalog.manage_availability"]);
  });
});
```

`web/src/components/layout/nav-items.test.ts`:

```ts
import { describe, expect, it } from "bun:test";
import { visibleNavItems } from "./nav-items";

const MANAGER = [
  "catalog.view_prices", "catalog.manage_availability", "catalog.administer_structure", "catalog.change_price",
  "sales.operate", "sales_shift.operate", "preparation.operate", "staff.administer", "audit.inspect", "tables.administer",
];
const CASHIER = ["catalog.view_prices", "catalog.manage_availability", "sales.operate", "sales_shift.operate"];
const BARISTA = ["catalog.manage_availability", "preparation.operate"];

const labels = (held: string[]) => visibleNavItems(held).map((i) => i.label);

describe("visibleNavItems", () => {
  it("shows everything to a manager", () => {
    expect(labels(MANAGER)).toEqual(["Bán hàng", "Sơ đồ bàn", "Bếp KDS", "Ca làm việc", "Lịch sử", "Cài đặt"]);
  });

  it("hides the kitchen from a cashier", () => {
    expect(labels(CASHIER)).toEqual(["Bán hàng", "Sơ đồ bàn", "Ca làm việc", "Lịch sử", "Cài đặt"]);
  });

  it("gives a barista the kitchen and settings; history stays unguarded until slice 8", () => {
    expect(labels(BARISTA)).toEqual(["Bếp KDS", "Lịch sử", "Cài đặt"]);
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd web && bun test src/lib/guards.test.ts src/features/settings src/components/layout`
Expected: FAIL — `requireAnyCapability` is not exported; `./tabs` and `./nav-items` do not exist.

- [ ] **Step 3: Implement guard, tabs, nav items**

In `web/src/lib/guards.ts`, replace `requireCapability` with:

```ts
export function requireAnyCapability(capabilities: readonly string[]): void {
  requireAuthenticated();
  const { state, capabilities: held } = useSessionStore.getState();
  // Capability is not evaluated while locked: the overlay is showing and the
  // operator has not yet proven they are still there.
  if (state === "authenticated" && !capabilities.some((c) => held.includes(c))) {
    // The miss target must be loop-free: `/` itself requires `sales.operate`,
    // so redirecting there would re-run this same failing guard forever.
    // `/no-access` is authentication-only, so the redirect always settles.
    throw redirect({ to: "/no-access" });
  }
}

export function requireCapability(capability: string): void {
  requireAnyCapability([capability]);
}
```

`web/src/features/settings/lib/tabs.ts`:

```ts
import { PackageX, type LucideIcon } from "lucide-react";

export interface SettingsTab {
  to: "/settings/availability";
  label: string;
  icon: LucideIcon;
  capability: string;
}

/** Tabs that exist. A tab not yet built is absent, never a placeholder. */
export const SETTINGS_TABS: readonly SettingsTab[] = [
  {
    to: "/settings/availability",
    label: "Món tạm hết",
    icon: PackageX,
    capability: "catalog.manage_availability",
  },
];

export const SETTINGS_CAPABILITIES: readonly string[] = SETTINGS_TABS.map((t) => t.capability);

export function permittedTabs(held: readonly string[]): SettingsTab[] {
  return SETTINGS_TABS.filter((t) => held.includes(t.capability));
}

export function firstPermittedTab(held: readonly string[]): SettingsTab | null {
  return permittedTabs(held)[0] ?? null;
}
```

`web/src/components/layout/nav-items.ts`:

```ts
import { ChefHat, Clock, Grid2X2, Receipt, Settings, ShoppingCart, type LucideIcon } from "lucide-react";
import { SETTINGS_CAPABILITIES } from "@/features/settings/lib/tabs";

export interface NavItem {
  to: "/" | "/tables" | "/kds" | "/shift" | "/history" | "/settings";
  label: string;
  icon: LucideIcon;
  /** Shown when the session holds any of these; absent means always shown. */
  capabilities?: readonly string[];
}

export const NAV_ITEMS: readonly NavItem[] = [
  { to: "/", label: "Bán hàng", icon: ShoppingCart, capabilities: ["sales.operate"] },
  { to: "/tables", label: "Sơ đồ bàn", icon: Grid2X2, capabilities: ["sales.operate"] },
  { to: "/kds", label: "Bếp KDS", icon: ChefHat, capabilities: ["preparation.operate"] },
  { to: "/shift", label: "Ca làm việc", icon: Clock, capabilities: ["sales_shift.operate"] },
  { to: "/history", label: "Lịch sử", icon: Receipt },
  { to: "/settings", label: "Cài đặt", icon: Settings, capabilities: SETTINGS_CAPABILITIES },
];

export function visibleNavItems(held: readonly string[]): NavItem[] {
  return NAV_ITEMS.filter((item) => !item.capabilities || item.capabilities.some((c) => held.includes(c)));
}
```

In `web/src/components/layout/pos-header.tsx`: delete the local `navItems` array and the now-unused icon imports (`ShoppingCart`, `Grid2X2`, `ChefHat`, `Receipt`, `Settings`; keep `Clock`, which the clock still uses), add `import { visibleNavItems } from "./nav-items";`, read `const capabilities = useSessionStore((s) => s.capabilities);` next to `displayName`, and change `navItems.map(` to `visibleNavItems(capabilities).map(`. Change the active test to `const isActive = item.to === "/" ? currentPath === "/" : currentPath.startsWith(item.to);` so "Cài đặt" stays highlighted on `/settings/availability`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd web && bun test src/lib/guards.test.ts src/features/settings src/components/layout`
Expected: PASS.

- [ ] **Step 5: Nested routes and layout**

`web/src/features/settings/components/settings-layout.tsx`:

```tsx
import type { ReactElement, ReactNode } from "react";
import { Link, Outlet } from "@tanstack/react-router";
import { useSessionStore } from "@/stores/use-session-store";
import { permittedTabs } from "../lib/tabs";

export interface SettingsLayoutProps {
  /** Rendered after a tab's label, keyed by the tab's route. */
  counters?: Partial<Record<string, ReactNode>>;
}

export function SettingsLayout({ counters = {} }: SettingsLayoutProps): ReactElement {
  const capabilities = useSessionStore((s) => s.capabilities);
  const tabs = permittedTabs(capabilities);

  return (
    <div className="flex h-full w-full flex-col bg-background">
      <nav role="tablist" aria-label="Cài đặt" className="flex items-center gap-2 border-b border-border bg-card px-4 py-2">
        {tabs.map((tab) => {
          const Icon = tab.icon;
          return (
            <Link
              key={tab.to}
              to={tab.to}
              role="tab"
              className="flex min-h-[48px] items-center gap-2 rounded-xl px-4 text-sm font-semibold text-muted-foreground transition hover:bg-muted hover:text-foreground"
              activeProps={{ className: "bg-primary text-primary-foreground hover:bg-primary hover:text-primary-foreground", "aria-selected": true }}
            >
              <Icon className="h-4 w-4" />
              <span>{tab.label}</span>
              {counters[tab.to]}
            </Link>
          );
        })}
      </nav>
      <main className="min-h-0 flex-1 overflow-y-auto">
        <Outlet />
      </main>
    </div>
  );
}
```

Replace `web/src/routes/_app/settings.tsx`:

```tsx
import { createFileRoute } from "@tanstack/react-router";
import { SettingsLayout } from "@/features/settings/components/settings-layout";
import { SETTINGS_CAPABILITIES } from "@/features/settings/lib/tabs";
import { requireAnyCapability } from "@/lib/guards";

export const Route = createFileRoute("/_app/settings")({
  beforeLoad: () => requireAnyCapability(SETTINGS_CAPABILITIES),
  component: () => <SettingsLayout />,
});
```

`web/src/routes/_app/settings/index.tsx`:

```tsx
import { createFileRoute, redirect } from "@tanstack/react-router";
import { firstPermittedTab } from "@/features/settings/lib/tabs";
import { useSessionStore } from "@/stores/use-session-store";

export const Route = createFileRoute("/_app/settings/")({
  beforeLoad: () => {
    const tab = firstPermittedTab(useSessionStore.getState().capabilities);
    throw redirect({ to: tab ? tab.to : "/no-access" });
  },
});
```

`web/src/routes/_app/settings/availability.tsx` (Task 8 swaps the component):

```tsx
import { createFileRoute } from "@tanstack/react-router";
import { requireCapability } from "@/lib/guards";

export const Route = createFileRoute("/_app/settings/availability")({
  beforeLoad: () => requireCapability("catalog.manage_availability"),
  component: () => null,
});
```

Delete `web/src/features/settings/components/settings-view.tsx`.

Regenerate the route tree: `cd web && bunx vite build` (the TanStack router plugin rewrites `src/routeTree.gen.ts`). Then restore the tracked placeholder if Vite removed it: `git checkout -- dist/.gitkeep 2>/dev/null || true`.

- [ ] **Step 6: Record ADR-056**

Append to `spec/decisions.md`:

```markdown
## ADR-056: /settings is guarded per tab, not by staff.administer

* **Decision Date:** 2026-09-28
* **Status:** Accepted
* **Context:** ADR-054 recorded `/settings` as requiring `staff.administer`, a Manager-only capability. Web slice 9a puts availability — operational work Cashier and Barista hold `catalog.manage_availability` for — under `/settings`, as the prototype does.
* **Decision:**
* `/settings` is a layout route guarded by `requireAnyCapability` over every tab's capability. Each tab is a child route guarded by its own capability; `/settings` itself redirects to the first tab the session may open. `web/src/features/settings/lib/tabs.ts` is the single source for all three.
* The header hides every navigation entry whose capabilities the session lacks. `/history` stays visible until slice 8 guards it.
* This supersedes ADR-054's line "`/settings` requires `staff.administer`"; ADR-054's `/no-access` rule is unchanged.
* **Consequences:** A Barista reaches "Món tạm hết" from the header. Slices 9b and 9c add tabs by appending to `SETTINGS_TABS`, without touching the layout guard.
```

- [ ] **Step 7: Verify and commit**

Run: `cd web && bun test && bunx tsc -b && bun run lint`
Expected: PASS.

```bash
git add web/src spec/decisions.md
git commit -m "feat(web): per-tab settings routes and capability-filtered nav"
```

---

### Task 6: Availability view model (pure)

**Files:**
- Create: `web/src/features/settings/lib/availability.ts`, `web/src/features/settings/lib/availability.test.ts`

**Interfaces:**
- Consumes: `matchesSearch` from `@/lib/search`; `newRequestId` from `@/lib/command`; generated `CatalogAvailabilityMenuResponse`, `CatalogAvailabilityChange`.
- Produces (all exported):

```ts
type AvailabilityKind = "item" | "size" | "modifier_option";
interface AvailabilityRef { kind: AvailabilityKind; id: string; name: string }
interface AvailabilitySizeView { id: string; name: string; available: boolean }
interface AvailabilityItemView { id: string; name: string; categoryId: string; categoryName: string; available: boolean; sizes: AvailabilitySizeView[]; blockedBy: string[] }
interface AvailabilityOptionView { id: string; name: string; groupId: string; groupName: string; available: boolean }
interface AvailabilityGroupView { id: string; name: string; options: AvailabilityOptionView[] }
interface AvailabilityStats { total: number; available: number; unavailable: number }
interface AvailabilityView { categories: { id: string; name: string }[]; items: AvailabilityItemView[]; groups: AvailabilityGroupView[]; stats: AvailabilityStats; unavailableRefs: AvailabilityRef[] }
interface AvailabilityFilter { query: string; scope: string; onlyUnavailable: boolean }
const SIZE_BLOCK_LABEL = "Kích cỡ"; const ALL_SCOPE = "all"; const TOPPINGS_SCOPE = "toppings";
const EMPTY_FILTER: AvailabilityFilter;
function toAvailabilityView(menu: CatalogAvailabilityMenuResponse | null | undefined): AvailabilityView;
function blockedMessage(blockedBy: string[]): string | null;
function isItemFullyAvailable(item: AvailabilityItemView): boolean;
function filterItems(items: AvailabilityItemView[], filter: AvailabilityFilter): AvailabilityItemView[];
function filterGroups(groups: AvailabilityGroupView[], filter: AvailabilityFilter): AvailabilityGroupView[];
function applyAvailability(menu: CatalogAvailabilityMenuResponse, kind: AvailabilityKind, id: string, available: boolean): CatalogAvailabilityMenuResponse;
function toRestoreChanges(refs: AvailabilityRef[]): CatalogAvailabilityChange[];
function refsKey(refs: AvailabilityRef[]): string;
interface Intent { key: string; id: string }
function nextIntent(prev: Intent | null, key: string): Intent;
```

- [ ] **Step 1: Write the failing test**

`web/src/features/settings/lib/availability.test.ts`:

```ts
import { describe, expect, it } from "bun:test";
import type { CatalogAvailabilityMenuResponse } from "@/api/generated/models";
import {
  ALL_SCOPE,
  EMPTY_FILTER,
  TOPPINGS_SCOPE,
  applyAvailability,
  blockedMessage,
  filterGroups,
  filterItems,
  nextIntent,
  refsKey,
  toAvailabilityView,
  toRestoreChanges,
} from "./availability";

const sugar = (a100: boolean, a50: boolean) => ({
  id: "g-sugar",
  name: "Mức đường",
  min_selections: 1,
  max_selections: 1,
  options: [
    { id: "o-100", name: "100% đường", available: a100 },
    { id: "o-50", name: "50% đường", available: a50 },
  ],
});

const topping = {
  id: "g-top",
  name: "Topping thêm",
  min_selections: 0,
  max_selections: 3,
  options: [{ id: "o-pearl", name: "Trân châu trắng", available: false }],
};

const menu: CatalogAvailabilityMenuResponse = {
  categories: [
    {
      id: "c-coffee",
      name: "Cà phê",
      items: [
        {
          id: "i-den",
          name: "Cà phê đen",
          category_id: "c-coffee",
          available: true,
          sizes: [
            { id: "s-den-s", name: "Size S", available: true },
            { id: "s-den-l", name: "Size L", available: false },
          ],
          modifier_groups: [sugar(true, true), topping],
        },
        {
          id: "i-sua",
          name: "Cà phê sữa đá",
          category_id: "c-coffee",
          available: false,
          modifier_groups: [sugar(true, true), topping],
        },
      ],
    },
    {
      id: "c-cake",
      name: "Bánh ngọt",
      items: [{ id: "i-croissant", name: "Croissant bơ tỏi", category_id: "c-cake", available: true }],
    },
  ],
};

describe("toAvailabilityView", () => {
  const view = toAvailabilityView(menu);

  it("flattens items with their category", () => {
    expect(view.items.map((i) => i.id)).toEqual(["i-den", "i-sua", "i-croissant"]);
    expect(view.items[2].categoryName).toBe("Bánh ngọt");
    expect(view.categories).toEqual([
      { id: "c-coffee", name: "Cà phê" },
      { id: "c-cake", name: "Bánh ngọt" },
    ]);
  });

  it("deduplicates modifier groups and options repeated under each item", () => {
    expect(view.groups.map((g) => g.id)).toEqual(["g-sugar", "g-top"]);
    expect(view.groups[0].options.map((o) => o.id)).toEqual(["o-100", "o-50"]);
    expect(view.groups[1].options[0].groupName).toBe("Topping thêm");
  });

  it("counts every item, size, and option once", () => {
    // 3 items + 2 sizes + 3 options = 8; off: i-sua, s-den-l, o-pearl
    expect(view.stats).toEqual({ total: 8, available: 5, unavailable: 3 });
    expect(view.unavailableRefs).toEqual([
      { kind: "size", id: "s-den-l", name: "Cà phê đen (Size L)" },
      { kind: "item", id: "i-sua", name: "Cà phê sữa đá" },
      { kind: "modifier_option", id: "o-pearl", name: "Trân châu trắng (Topping thêm)" },
    ]);
  });

  it("is empty for a missing menu", () => {
    expect(toAvailabilityView(null).stats).toEqual({ total: 0, available: 0, unavailable: 0 });
  });
});

describe("blockedBy", () => {
  it("flags a required group with fewer available options than its minimum", () => {
    const blocked = toAvailabilityView({
      categories: [{ id: "c", name: "C", items: [{ id: "i", name: "I", available: true, modifier_groups: [sugar(false, false)] }] }],
    });
    expect(blocked.items[0].blockedBy).toEqual(["Mức đường"]);
  });

  it("flags a sized item whose sizes are all off", () => {
    const blocked = toAvailabilityView({
      categories: [{ id: "c", name: "C", items: [{ id: "i", name: "I", available: true, sizes: [{ id: "s", name: "S", available: false }] }] }],
    });
    expect(blocked.items[0].blockedBy).toEqual(["Kích cỡ"]);
  });

  it("does not flag an optional group", () => {
    expect(toAvailabilityView(menu).items[0].blockedBy).toEqual([]);
  });

  it("formats the warning", () => {
    expect(blockedMessage([])).toBeNull();
    expect(blockedMessage(["Mức đường"])).toBe("Không bán được: hết tùy chọn bắt buộc (Mức đường)");
    expect(blockedMessage(["Kích cỡ", "Mức đường", "Đá"])).toBe(
      "Không bán được: hết kích cỡ; hết tùy chọn bắt buộc (Mức đường, Đá)",
    );
  });
});

describe("filters", () => {
  const view = toAvailabilityView(menu);

  it("returns everything with the empty filter", () => {
    expect(filterItems(view.items, EMPTY_FILTER)).toHaveLength(3);
    expect(filterGroups(view.groups, EMPTY_FILTER)).toHaveLength(2);
  });

  it("searches without diacritics across item, size, and category names", () => {
    expect(filterItems(view.items, { ...EMPTY_FILTER, query: "ca phe sua" }).map((i) => i.id)).toEqual(["i-sua"]);
    expect(filterItems(view.items, { ...EMPTY_FILTER, query: "banh" }).map((i) => i.id)).toEqual(["i-croissant"]);
  });

  it("scopes to one category, hiding toppings", () => {
    const f = { ...EMPTY_FILTER, scope: "c-cake" };
    expect(filterItems(view.items, f).map((i) => i.id)).toEqual(["i-croissant"]);
    expect(filterGroups(view.groups, f)).toEqual([]);
  });

  it("scopes to toppings, hiding items", () => {
    const f = { ...EMPTY_FILTER, scope: TOPPINGS_SCOPE };
    expect(filterItems(view.items, f)).toEqual([]);
    expect(filterGroups(view.groups, f)).toHaveLength(2);
  });

  it("keeps only items or options with something off", () => {
    const f = { ...EMPTY_FILTER, scope: ALL_SCOPE, onlyUnavailable: true };
    expect(filterItems(view.items, f).map((i) => i.id)).toEqual(["i-den", "i-sua"]);
    expect(filterGroups(view.groups, f).map((g) => g.id)).toEqual(["g-top"]);
  });
});

describe("applyAvailability", () => {
  it("patches every occurrence of an option without mutating the input", () => {
    const next = applyAvailability(menu, "modifier_option", "o-pearl", true);
    const options = next.categories!.flatMap((c) => c.items ?? []).flatMap((i) => i.modifier_groups ?? []).flatMap((g) => g.options ?? []);
    expect(options.filter((o) => o.id === "o-pearl").every((o) => o.available)).toBe(true);
    expect(menu.categories![0].items![0].modifier_groups![1].options![0].available).toBe(false);
  });

  it("patches an item and a size", () => {
    const next = applyAvailability(applyAvailability(menu, "item", "i-sua", true), "size", "s-den-l", true);
    const view = toAvailabilityView(next);
    expect(view.unavailableRefs.map((r) => r.id)).toEqual(["o-pearl"]);
  });
});

describe("restore helpers", () => {
  const refs = toAvailabilityView(menu).unavailableRefs;

  it("turns every ref on", () => {
    expect(toRestoreChanges(refs)).toEqual([
      { kind: "size", id: "s-den-l", available: true },
      { kind: "item", id: "i-sua", available: true },
      { kind: "modifier_option", id: "o-pearl", available: true },
    ]);
  });

  it("keeps the intent id while the list is unchanged and renews it when the list changes", () => {
    const first = nextIntent(null, refsKey(refs));
    expect(nextIntent(first, refsKey(refs))).toBe(first);
    const changed = nextIntent(first, refsKey(refs.slice(1)));
    expect(changed.id).not.toBe(first.id);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && bun test src/features/settings/lib/availability.test.ts`
Expected: FAIL — `Cannot find module './availability'`.

- [ ] **Step 3: Implement**

`web/src/features/settings/lib/availability.ts`:

```ts
import type {
  CatalogAvailabilityChange,
  CatalogAvailabilityMenuResponse,
} from "@/api/generated/models";
import { newRequestId } from "@/lib/command";
import { matchesSearch } from "@/lib/search";

export type AvailabilityKind = "item" | "size" | "modifier_option";

export interface AvailabilityRef {
  kind: AvailabilityKind;
  id: string;
  name: string;
}

export interface AvailabilitySizeView {
  id: string;
  name: string;
  available: boolean;
}

export interface AvailabilityItemView {
  id: string;
  name: string;
  categoryId: string;
  categoryName: string;
  available: boolean;
  sizes: AvailabilitySizeView[];
  /** Why an item that is itself on still cannot be sold (mirrors catalog.IsSellable). */
  blockedBy: string[];
}

export interface AvailabilityOptionView {
  id: string;
  name: string;
  groupId: string;
  groupName: string;
  available: boolean;
}

export interface AvailabilityGroupView {
  id: string;
  name: string;
  options: AvailabilityOptionView[];
}

export interface AvailabilityStats {
  total: number;
  available: number;
  unavailable: number;
}

export interface AvailabilityView {
  categories: { id: string; name: string }[];
  items: AvailabilityItemView[];
  groups: AvailabilityGroupView[];
  stats: AvailabilityStats;
  unavailableRefs: AvailabilityRef[];
}

export interface AvailabilityFilter {
  query: string;
  /** ALL_SCOPE, TOPPINGS_SCOPE, or a category id. */
  scope: string;
  onlyUnavailable: boolean;
}

export const SIZE_BLOCK_LABEL = "Kích cỡ";
export const ALL_SCOPE = "all";
export const TOPPINGS_SCOPE = "toppings";
export const EMPTY_FILTER: AvailabilityFilter = { query: "", scope: ALL_SCOPE, onlyUnavailable: false };

export function toAvailabilityView(menu: CatalogAvailabilityMenuResponse | null | undefined): AvailabilityView {
  const categories: { id: string; name: string }[] = [];
  const items: AvailabilityItemView[] = [];
  const groups = new Map<string, AvailabilityGroupView>();
  const seenOptions = new Set<string>();

  for (const cat of menu?.categories ?? []) {
    const categoryId = cat.id ?? "";
    const categoryName = cat.name ?? "";
    categories.push({ id: categoryId, name: categoryName });

    for (const item of cat.items ?? []) {
      const sizes = (item.sizes ?? []).map((s) => ({
        id: s.id ?? "",
        name: s.name ?? "",
        available: s.available ?? false,
      }));
      const blockedBy: string[] = [];
      if (sizes.length > 0 && !sizes.some((s) => s.available)) blockedBy.push(SIZE_BLOCK_LABEL);

      for (const g of item.modifier_groups ?? []) {
        const options = g.options ?? [];
        const availableCount = options.filter((o) => o.available).length;
        if ((g.min_selections ?? 0) > availableCount) blockedBy.push(g.name ?? "");

        const groupId = g.id ?? "";
        let group = groups.get(groupId);
        if (!group) {
          group = { id: groupId, name: g.name ?? "", options: [] };
          groups.set(groupId, group);
        }
        for (const o of options) {
          const optionId = o.id ?? "";
          if (seenOptions.has(optionId)) continue;
          seenOptions.add(optionId);
          group.options.push({
            id: optionId,
            name: o.name ?? "",
            groupId,
            groupName: group.name,
            available: o.available ?? false,
          });
        }
      }

      items.push({
        id: item.id ?? "",
        name: item.name ?? "",
        categoryId,
        categoryName,
        available: item.available ?? false,
        sizes,
        blockedBy,
      });
    }
  }

  const groupList = [...groups.values()].filter((g) => g.options.length > 0);
  const unavailableRefs: AvailabilityRef[] = [];
  let total = 0;
  for (const item of items) {
    for (const size of item.sizes) {
      total += 1;
      if (!size.available) unavailableRefs.push({ kind: "size", id: size.id, name: `${item.name} (${size.name})` });
    }
    total += 1;
    if (!item.available) unavailableRefs.push({ kind: "item", id: item.id, name: item.name });
  }
  for (const group of groupList) {
    for (const option of group.options) {
      total += 1;
      if (!option.available) {
        unavailableRefs.push({ kind: "modifier_option", id: option.id, name: `${option.name} (${group.name})` });
      }
    }
  }

  return {
    categories,
    items,
    groups: groupList,
    stats: { total, available: total - unavailableRefs.length, unavailable: unavailableRefs.length },
    unavailableRefs,
  };
}

export function blockedMessage(blockedBy: string[]): string | null {
  if (blockedBy.length === 0) return null;
  const parts: string[] = [];
  if (blockedBy.includes(SIZE_BLOCK_LABEL)) parts.push("hết kích cỡ");
  const groupNames = blockedBy.filter((b) => b !== SIZE_BLOCK_LABEL);
  if (groupNames.length > 0) parts.push(`hết tùy chọn bắt buộc (${groupNames.join(", ")})`);
  return `Không bán được: ${parts.join("; ")}`;
}

export function isItemFullyAvailable(item: AvailabilityItemView): boolean {
  return item.available && item.sizes.every((s) => s.available);
}

export function filterItems(items: AvailabilityItemView[], filter: AvailabilityFilter): AvailabilityItemView[] {
  if (filter.scope === TOPPINGS_SCOPE) return [];
  return items.filter((item) => {
    if (filter.scope !== ALL_SCOPE && item.categoryId !== filter.scope) return false;
    if (filter.onlyUnavailable && isItemFullyAvailable(item)) return false;
    if (!filter.query.trim()) return true;
    return (
      matchesSearch(filter.query, item.name, item.categoryName) ||
      item.sizes.some((s) => matchesSearch(filter.query, s.name))
    );
  });
}

export function filterGroups(groups: AvailabilityGroupView[], filter: AvailabilityFilter): AvailabilityGroupView[] {
  if (filter.scope !== ALL_SCOPE && filter.scope !== TOPPINGS_SCOPE) return [];
  return groups
    .map((g) => ({
      ...g,
      options: g.options.filter((o) => {
        if (filter.onlyUnavailable && o.available) return false;
        return matchesSearch(filter.query, o.name, g.name);
      }),
    }))
    .filter((g) => g.options.length > 0);
}

/** Optimistic patch: sets one entity's availability everywhere it appears. */
export function applyAvailability(
  menu: CatalogAvailabilityMenuResponse,
  kind: AvailabilityKind,
  id: string,
  available: boolean,
): CatalogAvailabilityMenuResponse {
  return {
    ...menu,
    categories: menu.categories?.map((cat) => ({
      ...cat,
      items: cat.items?.map((item) => ({
        ...item,
        available: kind === "item" && item.id === id ? available : item.available,
        sizes: item.sizes?.map((s) => (kind === "size" && s.id === id ? { ...s, available } : s)),
        modifier_groups: item.modifier_groups?.map((g) => ({
          ...g,
          options: g.options?.map((o) => (kind === "modifier_option" && o.id === id ? { ...o, available } : o)),
        })),
      })),
    })),
  };
}

export function toRestoreChanges(refs: AvailabilityRef[]): CatalogAvailabilityChange[] {
  return refs.map((r) => ({ kind: r.kind, id: r.id, available: true }));
}

export function refsKey(refs: AvailabilityRef[]): string {
  return refs.map((r) => `${r.kind}:${r.id}`).join(",");
}

export interface Intent {
  key: string;
  id: string;
}

/**
 * One request_id per restore intent: kept while the list the operator sees is
 * unchanged, renewed when it changes, because a different list is a
 * different command and would otherwise be refused as REQUEST_CONFLICT.
 */
export function nextIntent(prev: Intent | null, key: string): Intent {
  if (prev && prev.key === key) return prev;
  return { key, id: newRequestId() };
}
```

If the generated `CatalogAvailabilityChange.kind` is typed as a string-literal enum rather than `string`, the `kind: r.kind` assignment still type-checks because `AvailabilityKind` uses the same three values.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd web && bun test src/features/settings/lib && bunx tsc -b`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/features/settings/lib/availability.ts web/src/features/settings/lib/availability.test.ts
git commit -m "feat(web): availability view model and filters"
```

---

### Task 7: Availability data hooks

**Files:**
- Create: `web/src/features/settings/api/use-availability.ts`, `web/src/features/settings/api/use-availability.test.ts`

**Interfaces:**
- Consumes: Task 3's `usePostCatalogAvailabilityBatch`; Task 6's `applyAvailability`, `toRestoreChanges`, `AvailabilityKind`, `AvailabilityRef`; generated `useGetCatalogMenuAvailability`, `getGetCatalogMenuAvailabilityQueryKey`, `getGetCatalogMenuSellableQueryKey`, `usePatchCatalogItemsItemIdAvailability`, `usePatchCatalogSizesSizeIdAvailability`, `usePatchCatalogModifierOptionsOptionIdAvailability`, `GetCatalogMenuAvailability200`.
- Produces:
  - `AVAILABILITY_POLL_MS = 15_000`
  - `useAvailabilityMenu(): UseQueryResult<CatalogAvailabilityMenuResponse, ApiError>`
  - `useSetAvailability(): { setAvailability(kind: AvailabilityKind, id: string, available: boolean): Promise<void> }`
  - `useRestoreAvailability(): { isPending: boolean; restore(refs: AvailabilityRef[], requestId: string): Promise<void>; refresh(): Promise<void> }`
  - `classifyRestoreFailure(err: unknown): "stale" | "retry"`
  - `STALE_RESTORE_MESSAGE = "Danh sách đã thay đổi, vui lòng kiểm tra lại"`

- [ ] **Step 1: Write the failing test**

`web/src/features/settings/api/use-availability.test.ts`:

```ts
import { describe, expect, it } from "bun:test";
import { ApiError } from "@/lib/unwrap";
import {
  AVAILABILITY_POLL_MS,
  STALE_RESTORE_MESSAGE,
  classifyRestoreFailure,
  useAvailabilityMenu,
  useRestoreAvailability,
  useSetAvailability,
} from "./use-availability";

describe("classifyRestoreFailure", () => {
  it("treats a changed or missing entity as a stale list", () => {
    expect(classifyRestoreFailure(new ApiError(409, "ENTITY_RETIRED", "x"))).toBe("stale");
    expect(classifyRestoreFailure(new ApiError(409, "REQUEST_CONFLICT", "x"))).toBe("stale");
    expect(classifyRestoreFailure(new ApiError(404, "CATALOG_NOT_FOUND", "x"))).toBe("stale");
  });

  it("keeps the same intent for anything else", () => {
    expect(classifyRestoreFailure(new ApiError(500, "INTERNAL_ERROR", "x"))).toBe("retry");
    expect(classifyRestoreFailure(new Error("network"))).toBe("retry");
  });
});

describe("availability hooks", () => {
  it("poll every 15 seconds and export their hooks", () => {
    expect(AVAILABILITY_POLL_MS).toBe(15_000);
    expect(STALE_RESTORE_MESSAGE).toBe("Danh sách đã thay đổi, vui lòng kiểm tra lại");
    expect(typeof useAvailabilityMenu).toBe("function");
    expect(typeof useSetAvailability).toBe("function");
    expect(typeof useRestoreAvailability).toBe("function");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && bun test src/features/settings/api`
Expected: FAIL — `Cannot find module './use-availability'`.

- [ ] **Step 3: Implement**

`web/src/features/settings/api/use-availability.ts`:

```ts
import { useQueryClient, type UseQueryResult } from "@tanstack/react-query";
import {
  getGetCatalogMenuAvailabilityQueryKey,
  getGetCatalogMenuSellableQueryKey,
  useGetCatalogMenuAvailability,
  usePatchCatalogItemsItemIdAvailability,
  usePatchCatalogModifierOptionsOptionIdAvailability,
  usePatchCatalogSizesSizeIdAvailability,
  usePostCatalogAvailabilityBatch,
} from "@/api/generated/endpoints/catalog/catalog";
import type {
  CatalogAvailabilityMenuResponse,
  GetCatalogMenuAvailability200,
} from "@/api/generated/models";
import { newRequestId } from "@/lib/command";
import { ApiError, unwrap } from "@/lib/unwrap";
import {
  applyAvailability,
  toRestoreChanges,
  type AvailabilityKind,
  type AvailabilityRef,
} from "../lib/availability";

/** Another terminal may change availability; there is no push channel. */
export const AVAILABILITY_POLL_MS = 15_000;

export const STALE_RESTORE_MESSAGE = "Danh sách đã thay đổi, vui lòng kiểm tra lại";

export function useAvailabilityMenu(): UseQueryResult<CatalogAvailabilityMenuResponse, ApiError> {
  return useGetCatalogMenuAvailability<CatalogAvailabilityMenuResponse, ApiError>({
    query: { select: unwrap, refetchInterval: AVAILABILITY_POLL_MS },
  });
}

function useInvalidateMenus() {
  const queryClient = useQueryClient();
  return () =>
    Promise.all([
      queryClient.invalidateQueries({ queryKey: getGetCatalogMenuAvailabilityQueryKey() }),
      queryClient.invalidateQueries({ queryKey: getGetCatalogMenuSellableQueryKey() }),
    ]).then(() => undefined);
}

/** One toggle is one intent: a fresh request_id, optimistic, rolled back on refusal. */
export function useSetAvailability() {
  const queryClient = useQueryClient();
  const invalidateMenus = useInvalidateMenus();
  const item = usePatchCatalogItemsItemIdAvailability();
  const size = usePatchCatalogSizesSizeIdAvailability();
  const option = usePatchCatalogModifierOptionsOptionIdAvailability();
  const key = getGetCatalogMenuAvailabilityQueryKey();

  return {
    setAvailability: async (kind: AvailabilityKind, id: string, available: boolean) => {
      const data = { request_id: newRequestId(), available };
      await queryClient.cancelQueries({ queryKey: key });
      const previous = queryClient.getQueryData<GetCatalogMenuAvailability200>(key);
      queryClient.setQueryData<GetCatalogMenuAvailability200>(key, (old) =>
        old?.data ? { ...old, data: applyAvailability(old.data, kind, id, available) } : old,
      );
      try {
        if (kind === "item") unwrap(await item.mutateAsync({ itemId: id, data }));
        else if (kind === "size") unwrap(await size.mutateAsync({ sizeId: id, data }));
        else unwrap(await option.mutateAsync({ optionId: id, data }));
      } catch (err) {
        queryClient.setQueryData(key, previous);
        throw err;
      } finally {
        void invalidateMenus();
      }
    },
  };
}

export function classifyRestoreFailure(err: unknown): "stale" | "retry" {
  if (err instanceof ApiError && (err.status === 404 || err.status === 409)) return "stale";
  return "retry";
}

export function useRestoreAvailability() {
  const queryClient = useQueryClient();
  const invalidateMenus = useInvalidateMenus();
  const batch = usePostCatalogAvailabilityBatch();

  return {
    isPending: batch.isPending,
    restore: async (refs: AvailabilityRef[], requestId: string) => {
      unwrap(await batch.mutateAsync({ data: { request_id: requestId, changes: toRestoreChanges(refs) } }));
      await invalidateMenus();
    },
    refresh: () => queryClient.refetchQueries({ queryKey: getGetCatalogMenuAvailabilityQueryKey() }),
  };
}
```

Check the generated mutation variable names in `web/src/api/generated/endpoints/catalog/catalog.ts` (search for `usePatchCatalogSizesSizeIdAvailability`): if orval named the path parameter differently from `itemId` / `sizeId` / `optionId`, match it exactly.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd web && bun test src/features/settings && bunx tsc -b`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/features/settings/api
git commit -m "feat(web): availability query, toggle, and restore hooks"
```

---

### Task 8: Availability tab UI

**Files:**
- Create: `web/src/components/feedback/error-toast.tsx`, and under `web/src/features/settings/components/`: `availability-toggle.tsx`, `availability-stats.tsx`, `availability-item-card.tsx`, `availability-toppings.tsx`, `restore-availability-dialog.tsx`, `availability-board.tsx`, `availability-view.tsx`, `availability-counter.tsx`, with tests `availability-item-card.test.tsx`, `availability-board.test.tsx`, `restore-availability-dialog.test.tsx`, `availability-stats.test.tsx`
- Modify: `web/src/features/kds/components/kds-error-toast.tsx`, `web/src/routes/_app/settings.tsx`, `web/src/routes/_app/settings/availability.tsx`

**Interfaces:**
- Consumes: Tasks 5–7.
- Produces:
  - `ErrorToast({ message: string | null; onDismiss: () => void })`
  - `AvailabilityToggle({ checked: boolean; label: string; onChange: (next: boolean) => void; size?: "lg" | "chip"; muted?: boolean })`
  - `AvailabilityStats({ stats: AvailabilityStats; onRestore: () => void })`
  - `AvailabilityItemCard({ item: AvailabilityItemView; onToggle: (kind: AvailabilityKind, id: string, next: boolean) => void })`
  - `AvailabilityToppings({ groups: AvailabilityGroupView[]; onToggle: (kind: AvailabilityKind, id: string, next: boolean) => void })`
  - `RestorePanel({ refs: AvailabilityRef[]; error: string | null; isPending: boolean; onConfirm: () => void; onClose: () => void })` and container `RestoreAvailabilityDialog({ open: boolean; refs: AvailabilityRef[]; onClose: () => void })`
  - `AvailabilityBoard({ view: AvailabilityView; filter: AvailabilityFilter; onFilterChange: (f: AvailabilityFilter) => void; onToggle: …; onRestore: () => void })`
  - `AvailabilityView()` (container), `AvailabilityCounter()`, `TabCounter({ count: number })`

- [ ] **Step 1: Write the failing component tests**

`web/src/features/settings/components/availability-item-card.test.tsx`:

```tsx
import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { AvailabilityItemCard } from "./availability-item-card";
import type { AvailabilityItemView } from "../lib/availability";

const base: AvailabilityItemView = {
  id: "i",
  name: "Cà phê đen",
  categoryId: "c",
  categoryName: "Cà phê",
  available: true,
  sizes: [
    { id: "s", name: "Size S", available: true },
    { id: "l", name: "Size L", available: false },
  ],
  blockedBy: [],
};

describe("AvailabilityItemCard", () => {
  it("shows name, category, state, and one chip per size", () => {
    const html = renderToString(<AvailabilityItemCard item={base} onToggle={() => {}} />);
    expect(html).toContain("Cà phê đen");
    expect(html).toContain("Cà phê");
    expect(html).toContain("Còn hàng");
    expect(html).toContain("Size S");
    expect(html).toContain("Size L");
    expect(html).toContain('aria-checked="false"');
  });

  it("marks an item that is off", () => {
    const html = renderToString(<AvailabilityItemCard item={{ ...base, available: false }} onToggle={() => {}} />);
    expect(html).toContain("Tạm hết");
  });

  it("warns when a required group has run out", () => {
    const html = renderToString(<AvailabilityItemCard item={{ ...base, blockedBy: ["Mức đường"] }} onToggle={() => {}} />);
    expect(html).toContain("Không bán được: hết tùy chọn bắt buộc (Mức đường)");
  });
});
```

`web/src/features/settings/components/availability-stats.test.tsx`:

```tsx
import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { AvailabilityStats } from "./availability-stats";
import { TabCounter } from "./availability-counter";

describe("AvailabilityStats", () => {
  it("shows the counts and the restore count", () => {
    const html = renderToString(<AvailabilityStats stats={{ total: 8, available: 5, unavailable: 3 }} onRestore={() => {}} />);
    expect(html).toContain("Tổng danh mục");
    expect(html).toContain("Đang còn hàng");
    expect(html).toContain("Tạm hết hàng");
    expect(html).toContain("Khôi phục tất cả còn hàng (3)");
  });

  it("disables restore when nothing is off", () => {
    const html = renderToString(<AvailabilityStats stats={{ total: 8, available: 8, unavailable: 0 }} onRestore={() => {}} />);
    expect(html).toMatch(/<button[^>]*disabled[^>]*>[\s\S]*Khôi phục tất cả còn hàng \(0\)/);
  });
});

describe("TabCounter", () => {
  it("renders the count, and nothing at zero", () => {
    expect(renderToString(<TabCounter count={2} />)).toContain("2 tạm hết");
    expect(renderToString(<TabCounter count={0} />)).toBe("");
  });
});
```

`web/src/features/settings/components/restore-availability-dialog.test.tsx`:

```tsx
import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { RestorePanel } from "./restore-availability-dialog";

const refs = [
  { kind: "item" as const, id: "i", name: "Cà phê sữa đá" },
  { kind: "size" as const, id: "s", name: "Cà phê đen (Size L)" },
];

describe("RestorePanel", () => {
  it("lists every entry it will restore", () => {
    const html = renderToString(<RestorePanel refs={refs} error={null} isPending={false} onConfirm={() => {}} onClose={() => {}} />);
    expect(html).toContain("Cà phê sữa đá");
    expect(html).toContain("Cà phê đen (Size L)");
    expect(html).toContain("Khôi phục 2 mục");
  });

  it("shows the error inline", () => {
    const html = renderToString(
      <RestorePanel refs={refs} error="Danh sách đã thay đổi, vui lòng kiểm tra lại" isPending={false} onConfirm={() => {}} onClose={() => {}} />,
    );
    expect(html).toContain("Danh sách đã thay đổi, vui lòng kiểm tra lại");
  });
});
```

`web/src/features/settings/components/availability-board.test.tsx`:

```tsx
import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { AvailabilityBoard } from "./availability-board";
import { EMPTY_FILTER, TOPPINGS_SCOPE, toAvailabilityView } from "../lib/availability";

const view = toAvailabilityView({
  categories: [
    {
      id: "c",
      name: "Cà phê",
      items: [
        {
          id: "i",
          name: "Cà phê đen",
          available: true,
          modifier_groups: [
            { id: "g", name: "Topping thêm", min_selections: 0, options: [{ id: "o", name: "Trân châu trắng", available: false }] },
          ],
        },
      ],
    },
  ],
});

const render = (filter = EMPTY_FILTER, v = view) =>
  renderToString(<AvailabilityBoard view={v} filter={filter} onFilterChange={() => {}} onToggle={() => {}} onRestore={() => {}} />);

describe("AvailabilityBoard", () => {
  it("renders category pills, the topping pill, items, and toppings", () => {
    const html = render();
    expect(html).toContain("Tất cả");
    expect(html).toContain("Topping");
    expect(html).toContain("Cà phê đen");
    expect(html).toContain("Trân châu trắng");
    expect(html).toContain("Chỉ xem món tạm hết");
  });

  it("shows only toppings under the topping pill", () => {
    const html = render({ ...EMPTY_FILTER, scope: TOPPINGS_SCOPE });
    expect(html).not.toContain("Cà phê đen");
    expect(html).toContain("Trân châu trắng");
  });

  it("shows the no-match state with a reset", () => {
    const html = render({ ...EMPTY_FILTER, query: "khong co mon nay" });
    expect(html).toContain("Không tìm thấy món hoặc topping phù hợp");
    expect(html).toContain("Xóa bộ lọc");
  });

  it("shows the empty-menu state", () => {
    expect(render(EMPTY_FILTER, toAvailabilityView(null))).toContain("Chưa có món nào trong thực đơn");
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd web && bun test src/features/settings/components`
Expected: FAIL — modules not found.

- [ ] **Step 3: Implement the leaf components**

`web/src/components/feedback/error-toast.tsx` — move the body of `KdsErrorToast` here unchanged, renamed:

```tsx
import { AlertCircle } from "lucide-react";

export function ErrorToast({ message, onDismiss }: { message: string | null; onDismiss: () => void }) {
  if (!message) return null;
  return (
    <div
      role="alert"
      className="fixed bottom-4 left-1/2 -translate-x-1/2 z-50 flex items-center gap-2 rounded-xl bg-destructive text-destructive-foreground px-4 py-2.5 text-xs font-bold shadow-lg animate-in fade-in slide-in-from-bottom-2"
    >
      <AlertCircle className="h-4 w-4 shrink-0" />
      <span>{message}</span>
      <button
        type="button"
        onClick={onDismiss}
        className="ml-2 text-destructive-foreground/80 hover:text-destructive-foreground underline min-h-[48px] px-2 flex items-center"
      >
        Đóng
      </button>
    </div>
  );
}
```

Replace the contents of `web/src/features/kds/components/kds-error-toast.tsx` with:

```tsx
export { ErrorToast as KdsErrorToast } from "@/components/feedback/error-toast";
```

`availability-toggle.tsx`:

```tsx
import type { ReactElement } from "react";
import { cn } from "@/lib/utils";

export interface AvailabilityToggleProps {
  checked: boolean;
  label: string;
  onChange: (next: boolean) => void;
  size?: "lg" | "chip";
  /** Dimmed but operable, e.g. a Size whose item is off. */
  muted?: boolean;
}

export function AvailabilityToggle({ checked, label, onChange, size = "lg", muted = false }: AvailabilityToggleProps): ReactElement {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      onClick={() => onChange(!checked)}
      className={cn(
        "flex min-h-[48px] items-center gap-2 rounded-xl border px-3 text-xs font-bold transition active:scale-95",
        checked
          ? "border-emerald-300 bg-emerald-50 text-emerald-700 dark:border-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-400"
          : "border-rose-300 bg-rose-50 text-rose-700 dark:border-rose-800 dark:bg-rose-950/40 dark:text-rose-400",
        size === "lg" && "min-w-[120px] justify-center",
        muted && "opacity-50",
      )}
    >
      <span className={cn("h-2.5 w-2.5 rounded-full", checked ? "bg-emerald-500" : "bg-rose-500")} />
      <span>{size === "lg" ? (checked ? "Còn hàng" : "Tạm hết") : label}</span>
    </button>
  );
}
```

`availability-stats.tsx`:

```tsx
import type { ReactElement } from "react";
import { AlertOctagon, CheckCircle2, Coffee, RefreshCw } from "lucide-react";
import type { AvailabilityStats as Stats } from "../lib/availability";

function StatCard({ label, value, icon: Icon }: { label: string; value: number; icon: typeof Coffee }) {
  return (
    <div className="flex items-center justify-between rounded-2xl border border-border bg-card p-4">
      <div>
        <span className="text-xs font-semibold text-muted-foreground">{label}</span>
        <div className="text-2xl font-bold font-mono text-foreground">{value}</div>
      </div>
      <Icon className="h-6 w-6 text-muted-foreground" />
    </div>
  );
}

export function AvailabilityStats({ stats, onRestore }: { stats: Stats; onRestore: () => void }): ReactElement {
  return (
    <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
      <StatCard label="Tổng danh mục" value={stats.total} icon={Coffee} />
      <StatCard label="Đang còn hàng" value={stats.available} icon={CheckCircle2} />
      <StatCard label="Tạm hết hàng" value={stats.unavailable} icon={AlertOctagon} />
      <div className="flex flex-col justify-center gap-2 rounded-2xl border border-border bg-card p-4">
        <span className="text-xs font-semibold text-muted-foreground">Thao tác nhanh</span>
        <button
          type="button"
          onClick={onRestore}
          disabled={stats.unavailable === 0}
          className="flex min-h-[48px] items-center justify-center gap-2 rounded-xl bg-primary px-3 text-xs font-bold text-primary-foreground transition disabled:cursor-not-allowed disabled:opacity-50"
        >
          <RefreshCw className="h-4 w-4" />
          <span>Khôi phục tất cả còn hàng ({stats.unavailable})</span>
        </button>
      </div>
    </div>
  );
}
```

`availability-item-card.tsx`:

```tsx
import type { ReactElement } from "react";
import { AlertTriangle } from "lucide-react";
import { cn } from "@/lib/utils";
import { AvailabilityToggle } from "./availability-toggle";
import { blockedMessage, type AvailabilityItemView, type AvailabilityKind } from "../lib/availability";

export interface AvailabilityItemCardProps {
  item: AvailabilityItemView;
  onToggle: (kind: AvailabilityKind, id: string, next: boolean) => void;
}

export function AvailabilityItemCard({ item, onToggle }: AvailabilityItemCardProps): ReactElement {
  const warning = item.available ? blockedMessage(item.blockedBy) : null;
  return (
    <div
      className={cn(
        "flex flex-col gap-3 rounded-2xl border p-4",
        item.available ? "border-border bg-card" : "border-rose-300 bg-rose-50/40 dark:border-rose-800 dark:bg-rose-950/20",
      )}
    >
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <h4 className="truncate text-sm font-bold text-foreground">{item.name}</h4>
          <span className="text-xs text-muted-foreground">{item.categoryName}</span>
        </div>
        <AvailabilityToggle checked={item.available} label={item.name} onChange={(next) => onToggle("item", item.id, next)} />
      </div>
      {item.sizes.length > 0 && (
        <div className="flex flex-wrap gap-2">
          {item.sizes.map((size) => (
            <AvailabilityToggle
              key={size.id}
              size="chip"
              checked={size.available}
              label={size.name}
              muted={!item.available}
              onChange={(next) => onToggle("size", size.id, next)}
            />
          ))}
        </div>
      )}
      {warning && (
        <p className="flex items-center gap-1.5 text-xs font-semibold text-amber-700 dark:text-amber-400">
          <AlertTriangle className="h-3.5 w-3.5 shrink-0" />
          <span>{warning}</span>
        </p>
      )}
    </div>
  );
}
```

`availability-toppings.tsx`:

```tsx
import type { ReactElement } from "react";
import { AvailabilityToggle } from "./availability-toggle";
import type { AvailabilityGroupView, AvailabilityKind } from "../lib/availability";

export interface AvailabilityToppingsProps {
  groups: AvailabilityGroupView[];
  onToggle: (kind: AvailabilityKind, id: string, next: boolean) => void;
}

export function AvailabilityToppings({ groups, onToggle }: AvailabilityToppingsProps): ReactElement | null {
  if (groups.length === 0) return null;
  return (
    <section className="flex flex-col gap-4">
      <h3 className="text-sm font-bold text-foreground">Topping</h3>
      {groups.map((group) => (
        <div key={group.id} className="rounded-2xl border border-border bg-card p-4">
          <h4 className="mb-3 text-xs font-bold uppercase tracking-wider text-muted-foreground">{group.name}</h4>
          <div className="flex flex-wrap gap-2">
            {group.options.map((option) => (
              <AvailabilityToggle
                key={option.id}
                size="chip"
                checked={option.available}
                label={option.name}
                onChange={(next) => onToggle("modifier_option", option.id, next)}
              />
            ))}
          </div>
        </div>
      ))}
    </section>
  );
}
```

`availability-counter.tsx`:

```tsx
import type { ReactElement } from "react";
import { useAvailabilityMenu } from "../api/use-availability";
import { toAvailabilityView } from "../lib/availability";

export function TabCounter({ count }: { count: number }): ReactElement | null {
  if (count === 0) return null;
  return <span className="rounded-full bg-rose-600 px-2 py-0.5 font-mono text-2xs font-bold text-white">{count} tạm hết</span>;
}

/** Shares the tab's query cache, so it costs no extra request. */
export function AvailabilityCounter(): ReactElement | null {
  const { data } = useAvailabilityMenu();
  return <TabCounter count={toAvailabilityView(data).stats.unavailable} />;
}
```

- [ ] **Step 4: Implement the restore dialog, board, and view**

`restore-availability-dialog.tsx`:

```tsx
import { useEffect, useRef, useState, type ReactElement } from "react";
import { RefreshCw, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { messageForError } from "@/lib/error-messages";
import { playErrorBuzz, playSuccessChirp } from "@/lib/sound";
import { STALE_RESTORE_MESSAGE, classifyRestoreFailure, useRestoreAvailability } from "../api/use-availability";
import { nextIntent, refsKey, type AvailabilityRef, type Intent } from "../lib/availability";

export interface RestorePanelProps {
  refs: AvailabilityRef[];
  error: string | null;
  isPending: boolean;
  onConfirm: () => void;
  onClose: () => void;
}

export function RestorePanel({ refs, error, isPending, onConfirm, onClose }: RestorePanelProps): ReactElement {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/50 p-4 backdrop-blur-xs">
      <div role="dialog" aria-modal="true" aria-labelledby="restore-title" className="flex w-full max-w-md flex-col gap-4 rounded-3xl border border-border bg-card p-6 shadow-2xl">
        <div className="flex items-center justify-between border-b border-border pb-3">
          <h2 id="restore-title" className="text-base font-bold text-foreground">Khôi phục tất cả còn hàng</h2>
          <button type="button" aria-label="Đóng" onClick={onClose} disabled={isPending} className="flex h-12 w-12 items-center justify-center rounded-xl text-muted-foreground hover:bg-muted">
            <X className="h-5 w-5" />
          </button>
        </div>
        <ul className="max-h-72 space-y-1 overflow-y-auto text-sm">
          {refs.map((ref) => (
            <li key={`${ref.kind}:${ref.id}`} className="rounded-lg bg-muted px-3 py-2 font-medium text-foreground">
              {ref.name}
            </li>
          ))}
        </ul>
        {error && <p role="alert" className="text-xs font-semibold text-destructive">{error}</p>}
        <div className="flex justify-end gap-2 border-t border-border pt-3">
          <button type="button" onClick={onClose} disabled={isPending} className="h-12 min-h-[48px] rounded-xl border border-border bg-card px-4 text-xs font-bold text-foreground hover:bg-muted">
            Hủy bỏ
          </button>
          <Button onClick={onConfirm} disabled={isPending || refs.length === 0} className="h-12 min-h-[48px] gap-2 rounded-xl px-6 font-bold">
            <RefreshCw className="h-4 w-4" />
            {isPending ? "Đang khôi phục..." : `Khôi phục ${refs.length} mục`}
          </Button>
        </div>
      </div>
    </div>
  );
}

export interface RestoreAvailabilityDialogProps {
  open: boolean;
  refs: AvailabilityRef[];
  onClose: () => void;
}

export function RestoreAvailabilityDialog({ open, refs, onClose }: RestoreAvailabilityDialogProps): ReactElement | null {
  const { restore, refresh, isPending } = useRestoreAvailability();
  const [error, setError] = useState<string | null>(null);
  const intent = useRef<Intent | null>(null);

  useEffect(() => {
    if (!open) {
      intent.current = null;
      setError(null);
    }
  }, [open]);

  if (!open) return null;
  // Same list, same request_id across retries; a changed list is a new intent.
  intent.current = nextIntent(intent.current, refsKey(refs));

  async function handleConfirm() {
    if (isPending || !intent.current) return;
    setError(null);
    try {
      await restore(refs, intent.current.id);
      playSuccessChirp();
      onClose();
    } catch (err) {
      playErrorBuzz();
      if (classifyRestoreFailure(err) === "stale") {
        setError(STALE_RESTORE_MESSAGE);
        await refresh();
      } else {
        setError(messageForError(err));
      }
    }
  }

  return <RestorePanel refs={refs} error={error} isPending={isPending} onConfirm={handleConfirm} onClose={onClose} />;
}
```

A stale refusal refreshes `refs`, which changes `refsKey`, which renews the intent on the next render: the operator confirms the fresh list as a new command.

`availability-board.tsx`:

```tsx
import type { ReactElement } from "react";
import { Filter, Inbox, LayoutGrid, RotateCcw, Search, Sparkles } from "lucide-react";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";
import { AvailabilityItemCard } from "./availability-item-card";
import { AvailabilityStats } from "./availability-stats";
import { AvailabilityToppings } from "./availability-toppings";
import {
  ALL_SCOPE,
  EMPTY_FILTER,
  TOPPINGS_SCOPE,
  filterGroups,
  filterItems,
  type AvailabilityFilter,
  type AvailabilityKind,
  type AvailabilityView,
} from "../lib/availability";

export interface AvailabilityBoardProps {
  view: AvailabilityView;
  filter: AvailabilityFilter;
  onFilterChange: (filter: AvailabilityFilter) => void;
  onToggle: (kind: AvailabilityKind, id: string, next: boolean) => void;
  onRestore: () => void;
}

function Pill({ active, label, icon: Icon, onClick }: { active: boolean; label: string; icon?: typeof LayoutGrid; onClick: () => void }) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      onClick={onClick}
      className={cn(
        "flex min-h-[48px] items-center gap-2 rounded-xl border px-4 text-xs font-bold transition",
        active ? "border-primary bg-primary text-primary-foreground" : "border-border bg-card text-muted-foreground hover:bg-muted",
      )}
    >
      {Icon && <Icon className="h-4 w-4" />}
      <span>{label}</span>
    </button>
  );
}

export function AvailabilityBoard({ view, filter, onFilterChange, onToggle, onRestore }: AvailabilityBoardProps): ReactElement {
  if (view.items.length === 0 && view.groups.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center gap-2 p-12 text-center text-muted-foreground">
        <Inbox className="h-8 w-8" />
        <p className="text-sm font-semibold">Chưa có món nào trong thực đơn</p>
      </div>
    );
  }

  const items = filterItems(view.items, filter);
  const groups = filterGroups(view.groups, filter);
  const set = (patch: Partial<AvailabilityFilter>) => onFilterChange({ ...filter, ...patch });

  return (
    <div className="flex flex-col gap-4 p-4">
      <AvailabilityStats stats={view.stats} onRestore={onRestore} />

      <div className="flex flex-col gap-3 sm:flex-row">
        <div className="relative flex-1">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={filter.query}
            onChange={(e) => set({ query: e.target.value })}
            placeholder="Tìm theo tên món, kích cỡ, topping hoặc danh mục"
            className="h-12 rounded-xl pl-9 text-sm"
          />
        </div>
        <button
          type="button"
          aria-pressed={filter.onlyUnavailable}
          onClick={() => set({ onlyUnavailable: !filter.onlyUnavailable })}
          className={cn(
            "flex min-h-[48px] items-center gap-2 rounded-xl border px-4 text-xs font-bold transition",
            filter.onlyUnavailable ? "border-rose-300 bg-rose-50 text-rose-700" : "border-border bg-card text-muted-foreground hover:bg-muted",
          )}
        >
          <Filter className="h-4 w-4" />
          <span>Chỉ xem món tạm hết</span>
        </button>
      </div>

      <div role="tablist" aria-label="Danh mục" className="flex flex-wrap gap-2">
        <Pill active={filter.scope === ALL_SCOPE} label="Tất cả" icon={LayoutGrid} onClick={() => set({ scope: ALL_SCOPE })} />
        {view.categories.map((cat) => (
          <Pill key={cat.id} active={filter.scope === cat.id} label={cat.name} onClick={() => set({ scope: cat.id })} />
        ))}
        {view.groups.length > 0 && (
          <Pill active={filter.scope === TOPPINGS_SCOPE} label="Topping" icon={Sparkles} onClick={() => set({ scope: TOPPINGS_SCOPE })} />
        )}
      </div>

      {items.length === 0 && groups.length === 0 ? (
        <div className="flex flex-col items-center justify-center gap-3 p-12 text-center text-muted-foreground">
          <Inbox className="h-8 w-8" />
          <p className="text-sm font-semibold">Không tìm thấy món hoặc topping phù hợp</p>
          <button
            type="button"
            onClick={() => onFilterChange(EMPTY_FILTER)}
            className="flex min-h-[48px] items-center gap-2 rounded-xl border border-border bg-card px-4 text-xs font-bold text-foreground hover:bg-muted"
          >
            <RotateCcw className="h-4 w-4" />
            <span>Xóa bộ lọc</span>
          </button>
        </div>
      ) : (
        <>
          {items.length > 0 && (
            <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
              {items.map((item) => (
                <AvailabilityItemCard key={item.id} item={item} onToggle={onToggle} />
              ))}
            </div>
          )}
          <AvailabilityToppings groups={groups} onToggle={onToggle} />
        </>
      )}
    </div>
  );
}
```

`availability-view.tsx`:

```tsx
import { useState, type ReactElement } from "react";
import { Button } from "@/components/ui/button";
import { ErrorToast } from "@/components/feedback/error-toast";
import { messageForError } from "@/lib/error-messages";
import { playErrorBuzz, playTapChirp } from "@/lib/sound";
import { useAvailabilityMenu, useSetAvailability } from "../api/use-availability";
import { AvailabilityBoard } from "./availability-board";
import { RestoreAvailabilityDialog } from "./restore-availability-dialog";
import { EMPTY_FILTER, toAvailabilityView, type AvailabilityKind } from "../lib/availability";

export function AvailabilityView(): ReactElement {
  const menu = useAvailabilityMenu();
  const { setAvailability } = useSetAvailability();
  const [filter, setFilter] = useState(EMPTY_FILTER);
  const [error, setError] = useState<string | null>(null);
  const [restoreOpen, setRestoreOpen] = useState(false);

  if (menu.isPending) {
    return (
      <div className="grid grid-cols-1 gap-3 p-4 md:grid-cols-2 xl:grid-cols-3">
        {Array.from({ length: 6 }, (_, i) => (
          <div key={i} className="h-32 animate-pulse rounded-2xl bg-muted" />
        ))}
      </div>
    );
  }

  if (menu.isError) {
    return (
      <div className="flex flex-col items-center gap-3 p-12 text-center">
        <p role="alert" className="text-sm font-semibold text-destructive">{messageForError(menu.error)}</p>
        <Button onClick={() => void menu.refetch()} className="h-12 min-h-[48px] rounded-xl px-6">Thử lại</Button>
      </div>
    );
  }

  const view = toAvailabilityView(menu.data);

  const handleToggle = (kind: AvailabilityKind, id: string, next: boolean) => {
    playTapChirp();
    setError(null);
    setAvailability(kind, id, next).catch((err: unknown) => {
      playErrorBuzz();
      setError(messageForError(err));
    });
  };

  return (
    <>
      <AvailabilityBoard
        view={view}
        filter={filter}
        onFilterChange={setFilter}
        onToggle={handleToggle}
        onRestore={() => setRestoreOpen(true)}
      />
      <RestoreAvailabilityDialog open={restoreOpen} refs={view.unavailableRefs} onClose={() => setRestoreOpen(false)} />
      <ErrorToast message={error} onDismiss={() => setError(null)} />
    </>
  );
}
```

Check `@/lib/sound` exports `playTapChirp`, `playErrorBuzz`, and `playSuccessChirp` (the POS header and `use-dine-in.ts` already import them).

- [ ] **Step 5: Wire the routes**

`web/src/routes/_app/settings/availability.tsx`: import `AvailabilityView` from `@/features/settings/components/availability-view` and set `component: AvailabilityView`.

`web/src/routes/_app/settings.tsx`: import `AvailabilityCounter` from `@/features/settings/components/availability-counter` and render the layout with `counters={{ "/settings/availability": <AvailabilityCounter /> }}`.

- [ ] **Step 6: Run tests to verify they pass**

Run: `cd web && bun test && bunx tsc -b && bun run lint`
Expected: PASS. Every file under `features/settings/components/` stays under ~300 lines (slice sequence §6).

- [ ] **Step 7: Commit**

```bash
git add web/src
git commit -m "feat(web): availability tab with toggles and restore-all"
```

---

### Task 9: Full verification, roadmap, UAT handoff

**Files:**
- Modify: `ROADMAP.md`

- [ ] **Step 1: Run the full suites**

Run from the repo root:
`make fmt && make vet && make test && TEST_DATABASE_URL="<value from Makefile>" go test -count=1 -race -tags=integration ./internal/catalog/...`
then `cd web && bun test && bunx tsc -b && bun run lint && bun run build`, then `git checkout -- web/dist/.gitkeep 2>/dev/null || true`.
Expected: everything passes. Report any failure with its output; do not proceed past a failure.

- [ ] **Step 2: Update the roadmap**

In `ROADMAP.md`, after the sentence that introduces slice 7, add:

```markdown
[Slice 9a, availability](docs/superpowers/specs/2026-09-28-web-slice-9a-availability-design.md)
splits slice 9 into 9a (availability), 9b (catalog structure), and 9c (staff), and
ships the "Món tạm hết" tab with an atomic batch availability command (ADR-055)
and per-tab settings guards (ADR-056).
```

Also update "Fifty-two architecture decisions are recorded." to the actual count after ADR-056 (`grep -c '^## ADR-' spec/decisions.md`).

- [ ] **Step 3: Commit**

```bash
git add ROADMAP.md
git commit -m "docs: roadmap entry for web slice 9a"
```

- [ ] **Step 4: Hand over the UAT script and stop**

Hand the operator this script (adapted to the dev seed's menu) and wait for confirmation. Do not self-certify.

1. `make docker-up`, run the API, `make dev-seed`, `cd web && bun run dev`. Sign in as Manager: "Cài đặt" lands on `/settings/availability`; the stats match the seeded menu.
2. Turn "Cà phê sữa đá" off: a POS open in another tab stops offering it within 30 seconds. Turn it on: it returns.
3. Turn "Size L" of "Cà phê đen" off: the POS offers Size S and Size M only.
4. Turn off all four options of "Mức đường": every coffee and tea shows "Không bán được: hết tùy chọn bắt buộc (Mức đường)", and the POS no longer offers them. Turn them back on.
5. Search "ca phe sua" and "cfsd": both find "Cà phê sữa đá". "Chỉ xem món tạm hết" shows only what is off. The "Topping" pill shows only toppings.
6. Turn three entries off, press "Khôi phục tất cả còn hàng (3)": the dialog lists exactly those three; confirming turns all three on.
7. Sign in as a Barista: the header shows "Bếp KDS", "Lịch sử", "Cài đặt"; toggling works. As a Cashier: "Cài đặt" is present and toggling works; "Bếp KDS" is hidden.
8. Add an item to a takeaway draft, turn that item off from another tab, press pay: the POS shows the Vietnamese error and its menu grid drops the item at once.
9. A takeaway sale and a dine-in sale run end to end exactly as before.
