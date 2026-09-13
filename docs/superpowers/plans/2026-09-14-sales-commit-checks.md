# Commit, Checks & Charge Allocations (Phase 5B) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the commercial boundary of `internal/sales` — Commit, immutable Committed Items, Checks, and Charge Allocations — plus the two Order Draft targeting commands, as the second of four Phase 5 sub-phases.

**Architecture:** Extends the existing `internal/sales` package in place, reusing 5A's `Runner`, `ExecuteMutation`, `LockEditableDraft` query, and projection assembly. Commit runs in one transaction that locks the draft, revalidates every draft item against current Catalog state, freezes prices and names into `committed_items`, resolves a target Check from the draft's `check_target`, writes one Charge Allocation per item, and flips the draft to `COMMITTED`.

**Tech Stack:** Go 1.26+, Echo v4, PostgreSQL via `jackc/pgx/v5` (stdlib `database/sql` driver), sqlc, `google/uuid`, `stretchr/testify`, swag/OpenAPI 2.0.

**Spec:** [`docs/superpowers/specs/2026-09-14-sales-commit-checks-design.md`](../specs/2026-09-14-sales-commit-checks-design.md)

## Global Constraints

Every task's requirements implicitly include this section.

- **Package boundary.** `internal/sales` imports `internal/auth`, `internal/database/sqlc`, `internal/response`, `internal/httpvalidator`. It MUST NOT import `internal/catalog`, `internal/tables`, or `internal/shift`. Test files may import them.
- **Capability.** Every 5B operation requires `sales.operate`. Do not modify the capability table. No 5B operation requires Manager Approval.
- **Idempotency.** Use the shared `idempotency_keys` table (ADR-005, ADR-007). Action names: `sales.commit_order_draft`, `sales.start_new_order_draft`, `sales.set_order_draft_check_target`.
- **Audit.** Use the shared `audit_events` table. Business events are `UPPER_SNAKE_CASE`.
- **Money.** All monetary columns are `BIGINT`, mapped to Go `int64`. Constraints assert positivity only — never a `MAX_SAFE_INTEGER`-derived ceiling (ADR-013). Go guards against `int64` overflow explicitly, because Go wraps silently.
- **Quantity bound.** 1 to 9,999 inclusive, reusing 5A's `MinQuantity`/`MaxQuantity`, enforced in Go and by database `CHECK` on `committed_items` and `charge_allocations`.
- **Check state domain.** `('OPEN', 'SETTLED', 'MERGED')` from migration `000009` (ADR-014). 5B writes only `OPEN`. No settlement column is created.
- **Lock order.** Sales rows before Catalog rows, always. Catalog rows at Commit are locked `FOR SHARE`, not `FOR UPDATE` (ADR-015).
- **Empty collections** serialize as `[]`, never `null`. `sqlc.yaml` already sets `emit_empty_slices: true`.
- **Integration tests** carry `//go:build integration`, live in package `sales_test`, and run with `-p 1`.
- **Every task ends with a commit.** Run `make fmt` before committing.

---

## File Structure

**New files in `internal/sales/`:**

| File | Responsibility |
| --- | --- |
| `commit.go` | Commit handler, revalidation, pricing, snapshot building, Check targeting |
| `commit_test.go` | Unit tests for pricing, overflow guards, group rule evaluation |
| `draft_rounds.go` | Start new Order Draft and Set Check target handlers |
| `commit_integration_test.go` | Commit success and rejection suites |
| `check_targeting_integration_test.go` | `CURRENT_UNPAID` / `NEW_CHECK` suites, seeded per spec §1 |
| `draft_rounds_integration_test.go` | Round lifecycle suites |

**Modified files in `internal/sales/`:**

| File | Change |
| --- | --- |
| `domain.go` | New operation names, event names, Check states, check targets, pricing helpers |
| `domain_test.go` | Unit tests for the new pure functions |
| `errors.go` | New sentinels and their HTTP status mapping |
| `errors_test.go` | Mapping tests for every new code |
| `dto.go` | Commit/round commands; `CheckResponse`, `ChargeAllocationResponse`, `CommittedModifierResponse`; `Checks` retyped; `CheckTarget` on the draft |
| `dto_test.go` | Serialization tests for the new shapes |
| `projection.go` | Check assembly with the stored-charge invariant verification |
| `http.go` | Three Echo handlers with Swagger annotations |
| `routes.go` | Three handlers on `Slices`, three routes |
| `catalog_resolution.go` | Batched effective-group resolution for Commit |
| `catalog_resolution_integration_test.go` | Extended ADR-012 consistency test |
| `schema_integration_test.go` | Assertions for the four new/changed tables |

**New/modified files elsewhere:**

| File | Change |
| --- | --- |
| `internal/database/migrations/000009_create_sales_commit_slice.sql` | Schema (new) |
| `sql/queries/sales.sql` | Commit, Check, and round queries appended |
| `spec/decisions.md` | Append ADR-013, ADR-014, ADR-015 |
| `MIGRATE_PLAN.md` | 5B spec and plan links in the Phase 5 sub-phase table |

---

## Task 1: Database Schema

**Files:**
- Create: `internal/database/migrations/000009_create_sales_commit_slice.sql`
- Modify: `internal/sales/schema_integration_test.go`

**Interfaces:**
- Consumes: 5A's `order_drafts`, `order_draft_items`, `service_sessions`; Catalog's `menu_items`, `menu_item_sizes`, `modifier_groups`, `modifier_options`.
- Produces: tables `checks`, `committed_items`, `committed_item_modifier_options`, `charge_allocations`; column `order_drafts.check_target`.

- [ ] **Step 1: Write the failing schema test**

Append to `internal/sales/schema_integration_test.go`:

```go
func TestCommitSchema(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	t.Run("order_drafts carries check_target defaulting to CURRENT_UNPAID", func(t *testing.T) {
		var def string
		err := db.QueryRowContext(ctx, `
			SELECT column_default FROM information_schema.columns
			WHERE table_name = 'order_drafts' AND column_name = 'check_target'`).Scan(&def)
		require.NoError(t, err)
		require.Contains(t, def, "CURRENT_UNPAID")
	})

	t.Run("checks state domain admits all three canonical values", func(t *testing.T) {
		var clause string
		err := db.QueryRowContext(ctx, `
			SELECT pg_get_constraintdef(oid) FROM pg_constraint
			WHERE conname = 'check_state_valid'`).Scan(&clause)
		require.NoError(t, err)
		require.Contains(t, clause, "OPEN")
		require.Contains(t, clause, "SETTLED")
		require.Contains(t, clause, "MERGED")
	})

	t.Run("no settlement column exists until 5C", func(t *testing.T) {
		var n int
		err := db.QueryRowContext(ctx, `
			SELECT count(*) FROM information_schema.columns
			WHERE table_name = 'checks'
			  AND column_name IN ('settled_at', 'merged_into_check_id',
			                      'settled_by_staff_identity_id')`).Scan(&n)
		require.NoError(t, err)
		require.Equal(t, 0, n)
	})

	t.Run("committed_items enforces total equals quantity times unit price", func(t *testing.T) {
		var clause string
		err := db.QueryRowContext(ctx, `
			SELECT pg_get_constraintdef(oid) FROM pg_constraint
			WHERE conname = 'committed_item_total_vnd_valid'`).Scan(&clause)
		require.NoError(t, err)
		require.Contains(t, clause, "quantity")
		require.Contains(t, clause, "unit_price_vnd")
	})

	t.Run("charge_allocations is unique per committed item and check", func(t *testing.T) {
		var n int
		err := db.QueryRowContext(ctx, `
			SELECT count(*) FROM pg_indexes
			WHERE indexname = 'charge_allocation_item_check_unique'`).Scan(&n)
		require.NoError(t, err)
		require.Equal(t, 1, n)
	})
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -tags integration ./internal/sales/ -run TestCommitSchema -v -p 1`
Expected: FAIL — `check_target` column and the new tables do not exist.

- [ ] **Step 3: Write the migration**

Create `internal/database/migrations/000009_create_sales_commit_slice.sql`:

```sql
-- Phase 5B: Commit, Committed Items, Checks & Charge Allocations.
--
-- Commit is the commercial boundary: it revalidates an Order Draft, freezes
-- prices and names into immutable Committed Items, and places their charges
-- in a Check. Payments and Check restructuring belong to 5C; Submit, Orders,
-- and closure belong to 5D.

-- The Check target belongs to the draft, not the Session, so it resets to the
-- canonical default every time a new draft opens.
ALTER TABLE order_drafts
    ADD COLUMN IF NOT EXISTS check_target TEXT NOT NULL DEFAULT 'CURRENT_UNPAID';

ALTER TABLE order_drafts ADD CONSTRAINT order_draft_check_target_valid
    CHECK (check_target IN ('CURRENT_UNPAID', 'NEW_CHECK'));

-- The state domain ships complete even though 5B writes only OPEN, so the
-- CURRENT_UNPAID target query's state filter is meaningful rather than
-- vacuous and 5C adds columns without rewriting this constraint. See ADR-014.
CREATE TABLE IF NOT EXISTS checks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    service_session_id UUID NOT NULL REFERENCES service_sessions(id) ON DELETE RESTRICT,
    state TEXT NOT NULL DEFAULT 'OPEN',
    charge_vnd BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT check_state_valid CHECK (state IN ('OPEN', 'SETTLED', 'MERGED')),
    CONSTRAINT check_charge_vnd_valid CHECK (charge_vnd >= 0)
);

CREATE INDEX IF NOT EXISTS check_service_session_index
    ON checks (service_session_id);

-- Serves the CURRENT_UNPAID lookup directly.
CREATE INDEX IF NOT EXISTS check_open_per_session_index
    ON checks (service_session_id, created_at DESC, id DESC)
    WHERE state = 'OPEN';

-- An immutable commercial snapshot. The name columns are copies, not
-- references: a Menu Item renamed or retired tomorrow must not rewrite what a
-- customer was charged for today. menu_item_id is retained as a reporting
-- link. There is deliberately no size_id, matching the canonical schema.
CREATE TABLE IF NOT EXISTS committed_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_draft_id UUID NOT NULL REFERENCES order_drafts(id) ON DELETE RESTRICT,
    source_draft_item_id UUID NOT NULL REFERENCES order_draft_items(id) ON DELETE RESTRICT,
    menu_item_id UUID NOT NULL REFERENCES menu_items(id) ON DELETE RESTRICT,
    category_name TEXT NOT NULL,
    item_name TEXT NOT NULL,
    size_name TEXT,
    quantity INTEGER NOT NULL,
    unit_price_vnd BIGINT NOT NULL,
    total_vnd BIGINT NOT NULL,
    preparation_note TEXT,
    committed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT committed_item_quantity_valid
        CHECK (quantity BETWEEN 1 AND 9999),
    CONSTRAINT committed_item_unit_price_vnd_valid
        CHECK (unit_price_vnd > 0),
    CONSTRAINT committed_item_total_vnd_valid
        CHECK (total_vnd > 0 AND total_vnd = quantity::BIGINT * unit_price_vnd)
);

-- Committing one draft item twice is unrepresentable. The EDITABLE ->
-- COMMITTED draft transition is the primary guard; this index is the
-- database-level backstop that turns a logic error into a constraint
-- violation rather than a duplicate charge.
CREATE UNIQUE INDEX IF NOT EXISTS committed_item_source_draft_item_unique
    ON committed_items (source_draft_item_id);

CREATE INDEX IF NOT EXISTS committed_item_order_draft_index
    ON committed_items (order_draft_id);

CREATE TABLE IF NOT EXISTS committed_item_modifier_options (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    committed_item_id UUID NOT NULL REFERENCES committed_items(id) ON DELETE CASCADE,
    modifier_group_id UUID NOT NULL REFERENCES modifier_groups(id) ON DELETE RESTRICT,
    modifier_group_name TEXT NOT NULL,
    modifier_option_id UUID NOT NULL REFERENCES modifier_options(id) ON DELETE RESTRICT,
    modifier_option_name TEXT NOT NULL,
    surcharge_vnd BIGINT NOT NULL,
    CONSTRAINT committed_item_modifier_option_surcharge_vnd_valid
        CHECK (surcharge_vnd >= 0)
);

CREATE UNIQUE INDEX IF NOT EXISTS committed_item_modifier_option_unique
    ON committed_item_modifier_options (committed_item_id, modifier_option_id);

CREATE INDEX IF NOT EXISTS committed_item_modifier_option_item_index
    ON committed_item_modifier_options (committed_item_id);

-- A row states that a given quantity of one Committed Item is charged to one
-- Check. 5B always writes exactly one per item at full quantity; 5C's Split
-- reduces one and inserts another against a different Check, which is a data
-- change rather than a schema change.
CREATE TABLE IF NOT EXISTS charge_allocations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    committed_item_id UUID NOT NULL REFERENCES committed_items(id) ON DELETE CASCADE,
    check_id UUID NOT NULL REFERENCES checks(id) ON DELETE RESTRICT,
    quantity INTEGER NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT charge_allocation_quantity_valid
        CHECK (quantity BETWEEN 1 AND 9999)
);

CREATE UNIQUE INDEX IF NOT EXISTS charge_allocation_item_check_unique
    ON charge_allocations (committed_item_id, check_id);

CREATE INDEX IF NOT EXISTS charge_allocation_check_index
    ON charge_allocations (check_id);

COMMENT ON TABLE checks IS
    'Owned by internal/sales. 5B writes only OPEN; 5C adds settlement.';
COMMENT ON TABLE committed_items IS
    'Owned by internal/sales. Immutable after insert: no phase updates a row.';
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test -tags integration ./internal/sales/ -run TestCommitSchema -v -p 1`
Expected: PASS (all five subtests).

- [ ] **Step 5: Commit**

```bash
make fmt
git add internal/database/migrations/000009_create_sales_commit_slice.sql internal/sales/schema_integration_test.go
git commit -m "feat(sales): add Phase 5B schema for Commit, Checks and Charge Allocations"
```

---

## Task 2: sqlc Queries

**Files:**
- Modify: `sql/queries/sales.sql`
- Generated: `internal/database/sqlc/*.go` (via `make sqlc`)

**Interfaces:**
- Produces: `LockDraftItemsForCommit`, `ListDraftItemOptionsForCommit`, `ListEffectiveModifierGroupsForCommit`, `LockMenuItemSizesForCommit`, `LockCurrentOpenCheck`, `InsertCheck`, `RaiseCheckCharge`, `MarkOrderDraftCommitted`, `InsertCommittedItem`, `InsertCommittedItemModifierOption`, `InsertChargeAllocation`, `SetOrderDraftCheckTarget`, `FindBlockingDraft`, `ListSessionChecks`, `ListCheckAllocations`, `ListCommittedItemModifiers`.

- [ ] **Step 1: Append the Commit-side read and lock queries**

Append to `sql/queries/sales.sql`:

```sql
-- name: LockDraftItemsForCommit :many
-- Every draft item with the Catalog facts Commit revalidates against, locked
-- so the rows cannot change between validation and write. Ordered by
-- (created_at, id), which also fixes the order of the Committed Items.
SELECT di.id, di.menu_item_id, di.size_id, di.quantity, di.preparation_note,
       mi.name AS item_name, mi.price_vnd AS item_price_vnd,
       mi.available AS item_available,
       (mi.retired_at IS NOT NULL) AS item_retired,
       mc.name AS category_name
FROM order_draft_items di
JOIN menu_items mi ON mi.id = di.menu_item_id
JOIN menu_categories mc ON mc.id = mi.category_id
WHERE di.order_draft_id = $1
ORDER BY di.created_at ASC, di.id ASC
FOR UPDATE OF di;

-- name: ListDraftItemOptionsForCommit :many
-- The selected Options of the given draft items, with the Group facts the
-- Commit rules need.
SELECT dio.order_draft_item_id, o.id AS option_id, o.name AS option_name,
       o.surcharge_vnd, o.available,
       (o.retired_at IS NOT NULL) AS option_retired,
       g.id AS group_id, g.name AS group_name,
       (g.retired_at IS NOT NULL) AS group_retired
FROM order_draft_item_modifier_options dio
JOIN modifier_options o ON o.id = dio.modifier_option_id
JOIN modifier_groups g ON g.id = o.modifier_group_id
WHERE dio.order_draft_item_id = ANY(sqlc.arg(draft_item_ids)::uuid[])
ORDER BY g.name ASC, o.name ASC, o.id ASC;

-- name: ListEffectiveModifierGroupsForCommit :many
-- (inherited - exclusions) + direct, for a SET of Menu Items, returning the
-- selection rules Commit enforces.
--
-- This is the second expression of the algebra ListEffectiveModifierGroupIDs
-- already encodes. TestSalesResolutionMatchesCatalog pins both to
-- catalog.EffectiveGroupIDs over shared fixtures so they cannot drift. The
-- single-item query is left alone: the draft path does not need min/max and
-- should not pay for them. See ADR-012.
WITH targets AS (
    SELECT id, category_id FROM menu_items
    WHERE id = ANY(sqlc.arg(menu_item_ids)::uuid[])
),
inherited AS (
    SELECT t.id AS menu_item_id, cmg.modifier_group_id
    FROM targets t
    JOIN category_modifier_groups cmg ON cmg.menu_category_id = t.category_id
    WHERE NOT EXISTS (
        SELECT 1 FROM item_modifier_group_exclusions ex
        WHERE ex.menu_item_id = t.id
          AND ex.modifier_group_id = cmg.modifier_group_id
    )
),
direct AS (
    SELECT t.id AS menu_item_id, img.modifier_group_id
    FROM targets t
    JOIN item_modifier_groups img ON img.menu_item_id = t.id
),
effective AS (
    SELECT menu_item_id, modifier_group_id FROM inherited
    UNION
    SELECT menu_item_id, modifier_group_id FROM direct
)
SELECT e.menu_item_id, e.modifier_group_id, g.name AS group_name,
       g.min_selections, g.max_selections,
       (g.retired_at IS NOT NULL) AS group_retired
FROM effective e
JOIN modifier_groups g ON g.id = e.modifier_group_id
ORDER BY e.menu_item_id ASC, e.modifier_group_id ASC;

-- name: LockMenuItemSizesForCommit :many
-- Locked FOR SHARE: Commit only reads these rows and must merely prevent a
-- retirement or availability change landing mid-transaction. FOR UPDATE would
-- serialize two cashiers committing orders that share a popular item, on the
-- busiest path in the system, for no correctness gain. internal/catalog's
-- mutations take FOR UPDATE and are still excluded. See ADR-015.
SELECT id, menu_item_id, name, price_vnd, available,
       (retired_at IS NOT NULL) AS size_retired
FROM menu_item_sizes
WHERE id = ANY(sqlc.arg(size_ids)::uuid[])
ORDER BY id ASC
FOR SHARE;
```

- [ ] **Step 2: Append the Commit-side write queries**

```sql
-- name: LockCurrentOpenCheck :one
-- The Session's most recent OPEN Check, for the CURRENT_UNPAID target.
SELECT id, charge_vnd
FROM checks
WHERE service_session_id = $1 AND state = 'OPEN'
ORDER BY created_at DESC, id DESC
LIMIT 1
FOR UPDATE;

-- name: InsertCheck :one
INSERT INTO checks (service_session_id, created_at)
VALUES ($1, $2)
RETURNING id, charge_vnd;

-- name: RaiseCheckCharge :exec
UPDATE checks SET charge_vnd = $2 WHERE id = $1;

-- name: InsertCommittedItem :one
INSERT INTO committed_items (
    order_draft_id, source_draft_item_id, menu_item_id, category_name,
    item_name, size_name, quantity, unit_price_vnd, total_vnd,
    preparation_note, committed_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING id;

-- name: InsertCommittedItemModifierOption :exec
INSERT INTO committed_item_modifier_options (
    committed_item_id, modifier_group_id, modifier_group_name,
    modifier_option_id, modifier_option_name, surcharge_vnd
) VALUES ($1, $2, $3, $4, $5, $6);

-- name: InsertChargeAllocation :exec
INSERT INTO charge_allocations (committed_item_id, check_id, quantity, created_at)
VALUES ($1, $2, $3, $4);

-- name: MarkOrderDraftCommitted :exec
UPDATE order_drafts SET state = 'COMMITTED' WHERE id = $1;

-- name: SetOrderDraftCheckTarget :exec
UPDATE order_drafts SET check_target = $2 WHERE id = $1;

-- name: GetOrderDraftCheckTarget :one
SELECT check_target FROM order_drafts WHERE id = $1;
```

- [ ] **Step 3: Append the round-lifecycle and projection queries**

```sql
-- name: FindBlockingDraft :one
-- A draft that prevents a new one opening: EDITABLE, or COMMITTED without a
-- corresponding Order.
--
-- 5B has no orders table, so the second clause matches every COMMITTED draft
-- and a Session that has committed once cannot open another draft. That dead
-- end is deliberate and disappears when 5D adds the orders join here: the
-- rule exists to stop staff stacking rounds ahead of the kitchen, and
-- relaxing it now would ship a rule no phase wants. See the spec's accepted
-- consequences.
SELECT id
FROM order_drafts
WHERE service_session_id = $1
  AND state IN ('EDITABLE', 'COMMITTED')
ORDER BY created_at ASC, id ASC
LIMIT 1
FOR UPDATE;

-- name: InsertOrderDraftForSession :one
INSERT INTO order_drafts (service_session_id, created_at)
VALUES ($1, $2)
RETURNING id, state, check_target;

-- name: ListSessionChecks :many
SELECT id, state, charge_vnd, created_at
FROM checks
WHERE service_session_id = $1
ORDER BY created_at ASC, id ASC;

-- name: ListCheckAllocations :many
SELECT ca.id, ca.quantity AS allocated_quantity, ca.created_at,
       ci.id AS committed_item_id, ci.menu_item_id, ci.category_name,
       ci.item_name, ci.size_name, ci.quantity AS committed_quantity,
       ci.unit_price_vnd, ci.total_vnd AS committed_total_vnd,
       ci.preparation_note
FROM charge_allocations ca
JOIN committed_items ci ON ci.id = ca.committed_item_id
WHERE ca.check_id = $1
ORDER BY ci.committed_at ASC, ci.id ASC;

-- name: ListCommittedItemModifiers :many
SELECT committed_item_id, modifier_group_id, modifier_group_name,
       modifier_option_id, modifier_option_name, surcharge_vnd
FROM committed_item_modifier_options
WHERE committed_item_id = ANY(sqlc.arg(committed_item_ids)::uuid[])
ORDER BY modifier_group_name ASC, modifier_option_name ASC;
```

- [ ] **Step 4: Regenerate and verify it compiles**

Run: `make sqlc && go build ./...`
Expected: generation succeeds and the package builds. If sqlc reports an unknown column, check it against migration `000009` rather than editing the query to match a guess.

- [ ] **Step 5: Commit**

```bash
make fmt
git add sql/queries/sales.sql internal/database/sqlc/
git commit -m "feat(sales): add sqlc queries for Commit, Checks and round lifecycle"
```

---

## Task 3: Domain Constants And Pricing

**Files:**
- Modify: `internal/sales/domain.go`
- Test: `internal/sales/domain_test.go`

**Interfaces:**
- Produces: `OpCommitOrderDraft`, `OpStartNewOrderDraft`, `OpSetOrderDraftCheckTarget`; `EventOrderDraftCommitted`, `EventOrderDraftStarted`, `EventOrderDraftCheckTargetSet`; `CheckStateOpen`/`Settled`/`Merged`; `CheckTargetCurrentUnpaid`/`CheckTargetNewCheck`; `ValidateCheckTarget(string) error`; `LineTotal(quantity int32, unitPriceVND int64) (int64, error)`; `AddCharge(total, delta int64) (int64, error)`.

- [ ] **Step 1: Write the failing unit tests**

Append to `internal/sales/domain_test.go`:

```go
func TestLineTotal(t *testing.T) {
	t.Run("multiplies quantity by unit price", func(t *testing.T) {
		got, err := sales.LineTotal(3, 25_000)
		require.NoError(t, err)
		require.Equal(t, int64(75_000), got)
	})

	t.Run("rejects a non-positive unit price", func(t *testing.T) {
		_, err := sales.LineTotal(1, 0)
		require.ErrorIs(t, err, sales.ErrLineTotalOutOfRange)
	})

	t.Run("rejects a multiplication that would overflow int64", func(t *testing.T) {
		_, err := sales.LineTotal(9999, math.MaxInt64/2)
		require.ErrorIs(t, err, sales.ErrLineTotalOutOfRange)
	})

	t.Run("accepts the largest realistic line", func(t *testing.T) {
		got, err := sales.LineTotal(9999, 2_147_483_647)
		require.NoError(t, err)
		require.Equal(t, int64(9999)*2_147_483_647, got)
	})
}

func TestAddCharge(t *testing.T) {
	t.Run("accumulates", func(t *testing.T) {
		got, err := sales.AddCharge(50_000, 25_000)
		require.NoError(t, err)
		require.Equal(t, int64(75_000), got)
	})

	t.Run("rejects an accumulation that would overflow int64", func(t *testing.T) {
		_, err := sales.AddCharge(math.MaxInt64-10, 100)
		require.ErrorIs(t, err, sales.ErrCheckChargeOutOfRange)
	})

	t.Run("rejects a negative result", func(t *testing.T) {
		_, err := sales.AddCharge(10, -100)
		require.ErrorIs(t, err, sales.ErrCheckChargeOutOfRange)
	})
}

func TestValidateCheckTarget(t *testing.T) {
	require.NoError(t, sales.ValidateCheckTarget("CURRENT_UNPAID"))
	require.NoError(t, sales.ValidateCheckTarget("NEW_CHECK"))
	require.Error(t, sales.ValidateCheckTarget("PAID"))
	require.Error(t, sales.ValidateCheckTarget(""))
}
```

Add `"math"` to that file's imports.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/sales/ -run 'TestLineTotal|TestAddCharge|TestValidateCheckTarget' -v`
Expected: FAIL — `LineTotal`, `AddCharge`, `ValidateCheckTarget` undefined.

- [ ] **Step 3: Add the constants and functions**

Append to the operation-name block in `internal/sales/domain.go`:

```go
	OpCommitOrderDraft         = "sales.commit_order_draft"
	OpStartNewOrderDraft       = "sales.start_new_order_draft"
	OpSetOrderDraftCheckTarget = "sales.set_order_draft_check_target"
```

Append to the event-name block:

```go
	// EventOrderDraftCommitted drops the canonical TAKEAWAY_CHECKOUT_ prefix.
	// Commit is mode-agnostic — the canonical source runs one handler for
	// Dine-in too — and 5A already dropped that prefix throughout.
	EventOrderDraftCommitted      = "ORDER_DRAFT_COMMITTED"
	EventOrderDraftStarted        = "ORDER_DRAFT_STARTED"
	EventOrderDraftCheckTargetSet = "ORDER_DRAFT_CHECK_TARGET_SET"
```

Append to `internal/sales/domain.go`:

```go
// Check states. 5B writes only CheckStateOpen; 5C writes the other two.
// The domain ships complete so the CURRENT_UNPAID target query's state filter
// is meaningful rather than vacuous. See ADR-014.
const (
	CheckStateOpen    = "OPEN"
	CheckStateSettled = "SETTLED"
	CheckStateMerged  = "MERGED"
)

// Order Draft Check targets. The target belongs to the draft, not the
// Session, and resets to CheckTargetCurrentUnpaid when a new draft opens.
const (
	CheckTargetCurrentUnpaid = "CURRENT_UNPAID"
	CheckTargetNewCheck      = "NEW_CHECK"
)

// ValidateCheckTarget rejects a target outside the domain.
func ValidateCheckTarget(target string) error {
	switch target {
	case CheckTargetCurrentUnpaid, CheckTargetNewCheck:
		return nil
	default:
		return fmt.Errorf("%w: check_target must be %s or %s",
			ErrInvalidCheckTarget, CheckTargetCurrentUnpaid, CheckTargetNewCheck)
	}
}

// LineTotal computes quantity * unitPriceVND with an explicit overflow guard.
//
// The canonical implementation guards against Number.MAX_SAFE_INTEGER because
// JavaScript loses integer precision beyond it. Go has no such limit, but its
// integer arithmetic wraps silently, so money arithmetic that does not check
// can produce a negative total without failing. The bound is int64's, not a
// business ceiling. See ADR-013.
func LineTotal(quantity int32, unitPriceVND int64) (int64, error) {
	if quantity < MinQuantity || quantity > MaxQuantity {
		return 0, fmt.Errorf("%w: quantity %d out of range", ErrLineTotalOutOfRange, quantity)
	}
	if unitPriceVND <= 0 {
		return 0, fmt.Errorf("%w: unit price %d is not positive", ErrLineTotalOutOfRange, unitPriceVND)
	}
	if unitPriceVND > math.MaxInt64/int64(quantity) {
		return 0, fmt.Errorf("%w: %d x %d overflows", ErrLineTotalOutOfRange, quantity, unitPriceVND)
	}
	return int64(quantity) * unitPriceVND, nil
}

// AddCharge accumulates a Check's charge with an explicit overflow guard.
func AddCharge(totalVND, deltaVND int64) (int64, error) {
	if deltaVND > 0 && totalVND > math.MaxInt64-deltaVND {
		return 0, fmt.Errorf("%w: %d + %d overflows", ErrCheckChargeOutOfRange, totalVND, deltaVND)
	}
	sum := totalVND + deltaVND
	if sum < 0 {
		return 0, fmt.Errorf("%w: %d is negative", ErrCheckChargeOutOfRange, sum)
	}
	return sum, nil
}
```

Add `"math"` to `domain.go`'s imports.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/sales/ -run 'TestLineTotal|TestAddCharge|TestValidateCheckTarget' -v`
Expected: PASS. (Compilation requires Task 4's sentinels; if running this task standalone, add the three sentinels first — Task 4 then only adds the mapping and its tests.)

- [ ] **Step 5: Commit**

```bash
make fmt
git add internal/sales/domain.go internal/sales/domain_test.go
git commit -m "feat(sales): add Commit domain constants and overflow-guarded money arithmetic"
```

---

## Task 4: Error Sentinels And HTTP Mapping

**Files:**
- Modify: `internal/sales/errors.go`
- Test: `internal/sales/errors_test.go`

**Interfaces:**
- Produces: `ErrEmptyDraft`, `ErrCommitMenuItemUnavailable`, `ErrCommitMenuItemRetired`, `ErrCommitSizeRequired`, `ErrCommitSizeInvalid`, `ErrCommitSizeUnavailable`, `ErrCommitSizeRetired`, `ErrCommitModifierOptionInvalid`, `ErrCommitModifierOptionUnavailable`, `ErrCommitModifierOptionRetired`, `ErrCommitModifierGroupInvalid`, `ErrCommitModifierGroupRetired`, `ErrLineTotalOutOfRange`, `ErrCheckChargeOutOfRange`, `ErrNewOrderDraftNotAvailable`, `ErrInvalidCheckTarget`, `ErrChargeInvariantViolated`.

- [ ] **Step 1: Write the failing mapping tests**

Append to `internal/sales/errors_test.go`:

```go
func TestMapHTTPErrorCommitCodes(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{sales.ErrEmptyDraft, http.StatusConflict, "EMPTY_DRAFT"},
		{sales.ErrCommitMenuItemUnavailable, http.StatusConflict, "COMMIT_MENU_ITEM_UNAVAILABLE"},
		{sales.ErrCommitMenuItemRetired, http.StatusConflict, "COMMIT_MENU_ITEM_RETIRED"},
		{sales.ErrCommitSizeRequired, http.StatusConflict, "COMMIT_SIZE_REQUIRED"},
		{sales.ErrCommitSizeInvalid, http.StatusConflict, "COMMIT_SIZE_INVALID"},
		{sales.ErrCommitSizeUnavailable, http.StatusConflict, "COMMIT_SIZE_UNAVAILABLE"},
		{sales.ErrCommitSizeRetired, http.StatusConflict, "COMMIT_SIZE_RETIRED"},
		{sales.ErrCommitModifierOptionInvalid, http.StatusConflict, "COMMIT_MODIFIER_OPTION_INVALID"},
		{sales.ErrCommitModifierOptionUnavailable, http.StatusConflict, "COMMIT_MODIFIER_OPTION_UNAVAILABLE"},
		{sales.ErrCommitModifierOptionRetired, http.StatusConflict, "COMMIT_MODIFIER_OPTION_RETIRED"},
		{sales.ErrCommitModifierGroupInvalid, http.StatusConflict, "COMMIT_MODIFIER_GROUP_INVALID"},
		{sales.ErrCommitModifierGroupRetired, http.StatusConflict, "COMMIT_MODIFIER_GROUP_RETIRED"},
		{sales.ErrNewOrderDraftNotAvailable, http.StatusConflict, "NEW_ORDER_DRAFT_NOT_AVAILABLE"},
		{sales.ErrLineTotalOutOfRange, http.StatusUnprocessableEntity, "LINE_TOTAL_OUT_OF_RANGE"},
		{sales.ErrCheckChargeOutOfRange, http.StatusUnprocessableEntity, "CHECK_CHARGE_OUT_OF_RANGE"},
		{sales.ErrInvalidCheckTarget, http.StatusUnprocessableEntity, "INVALID_CHECK_TARGET"},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			status, code, _ := sales.MapHTTPError(fmt.Errorf("wrapped: %w", tc.err))
			require.Equal(t, tc.status, status)
			require.Equal(t, tc.code, code)
		})
	}
}

// The stored-charge invariant is a defect detector, never a business state:
// it must not reach the client as a recognizable code.
func TestChargeInvariantViolationIsNotAClientCode(t *testing.T) {
	status, _, _ := sales.MapHTTPError(fmt.Errorf("wrapped: %w", sales.ErrChargeInvariantViolated))
	require.Equal(t, http.StatusInternalServerError, status)
}
```

Match the existing `MapHTTPError` signature in `errors.go`; if it returns a struct rather than a triple, adapt these assertions to that shape rather than changing the function.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/sales/ -run 'TestMapHTTPErrorCommitCodes|TestChargeInvariant' -v`
Expected: FAIL — the sentinels are undefined.

- [ ] **Step 3: Add the sentinels and mapping**

Append to the sentinel block in `internal/sales/errors.go`:

```go
	ErrEmptyDraft = errors.New("order draft has no items")

	ErrCommitMenuItemUnavailable = errors.New("menu item is unavailable at commit")
	ErrCommitMenuItemRetired     = errors.New("menu item is retired at commit")

	ErrCommitSizeRequired    = errors.New("a size must be chosen before commit")
	ErrCommitSizeInvalid     = errors.New("size is not valid for this menu item")
	ErrCommitSizeUnavailable = errors.New("size is unavailable at commit")
	ErrCommitSizeRetired     = errors.New("size is retired at commit")

	ErrCommitModifierOptionInvalid     = errors.New("modifier option is not selectable for this menu item")
	ErrCommitModifierOptionUnavailable = errors.New("modifier option is unavailable at commit")
	ErrCommitModifierOptionRetired     = errors.New("modifier option is retired at commit")
	ErrCommitModifierGroupInvalid      = errors.New("modifier group selection rules are not satisfied")
	ErrCommitModifierGroupRetired      = errors.New("a required modifier group is retired")

	ErrLineTotalOutOfRange   = errors.New("line total out of range")
	ErrCheckChargeOutOfRange = errors.New("check charge out of range")

	ErrNewOrderDraftNotAvailable = errors.New("a new order draft cannot be started yet")
	ErrInvalidCheckTarget        = errors.New("invalid check target")

	// ErrChargeInvariantViolated reports that a Check's stored charge_vnd
	// disagrees with the sum of its allocations. That is a defect, not a
	// business state, so it is deliberately absent from MapHTTPError and
	// surfaces as a 500 with the Check id logged.
	ErrChargeInvariantViolated = errors.New("check charge does not match its allocations")
```

Append these cases to `MapHTTPError`, following the existing `coded(...)` style:

```go
	case errors.Is(err, ErrEmptyDraft):
		return coded(http.StatusConflict, "EMPTY_DRAFT", ErrEmptyDraft)
	case errors.Is(err, ErrCommitMenuItemRetired):
		return coded(http.StatusConflict, "COMMIT_MENU_ITEM_RETIRED", ErrCommitMenuItemRetired)
	case errors.Is(err, ErrCommitMenuItemUnavailable):
		return coded(http.StatusConflict, "COMMIT_MENU_ITEM_UNAVAILABLE", ErrCommitMenuItemUnavailable)
	case errors.Is(err, ErrCommitSizeRequired):
		return coded(http.StatusConflict, "COMMIT_SIZE_REQUIRED", ErrCommitSizeRequired)
	case errors.Is(err, ErrCommitSizeInvalid):
		return coded(http.StatusConflict, "COMMIT_SIZE_INVALID", ErrCommitSizeInvalid)
	case errors.Is(err, ErrCommitSizeRetired):
		return coded(http.StatusConflict, "COMMIT_SIZE_RETIRED", ErrCommitSizeRetired)
	case errors.Is(err, ErrCommitSizeUnavailable):
		return coded(http.StatusConflict, "COMMIT_SIZE_UNAVAILABLE", ErrCommitSizeUnavailable)
	case errors.Is(err, ErrCommitModifierOptionInvalid):
		return coded(http.StatusConflict, "COMMIT_MODIFIER_OPTION_INVALID", ErrCommitModifierOptionInvalid)
	case errors.Is(err, ErrCommitModifierOptionRetired):
		return coded(http.StatusConflict, "COMMIT_MODIFIER_OPTION_RETIRED", ErrCommitModifierOptionRetired)
	case errors.Is(err, ErrCommitModifierOptionUnavailable):
		return coded(http.StatusConflict, "COMMIT_MODIFIER_OPTION_UNAVAILABLE", ErrCommitModifierOptionUnavailable)
	case errors.Is(err, ErrCommitModifierGroupRetired):
		return coded(http.StatusConflict, "COMMIT_MODIFIER_GROUP_RETIRED", ErrCommitModifierGroupRetired)
	case errors.Is(err, ErrCommitModifierGroupInvalid):
		return coded(http.StatusConflict, "COMMIT_MODIFIER_GROUP_INVALID", ErrCommitModifierGroupInvalid)
	case errors.Is(err, ErrNewOrderDraftNotAvailable):
		return coded(http.StatusConflict, "NEW_ORDER_DRAFT_NOT_AVAILABLE", ErrNewOrderDraftNotAvailable)
	case errors.Is(err, ErrLineTotalOutOfRange):
		return coded(http.StatusUnprocessableEntity, "LINE_TOTAL_OUT_OF_RANGE", ErrLineTotalOutOfRange)
	case errors.Is(err, ErrCheckChargeOutOfRange):
		return coded(http.StatusUnprocessableEntity, "CHECK_CHARGE_OUT_OF_RANGE", ErrCheckChargeOutOfRange)
	case errors.Is(err, ErrInvalidCheckTarget):
		return coded(http.StatusUnprocessableEntity, "INVALID_CHECK_TARGET", ErrInvalidCheckTarget)
```

Retirement is mapped before availability in each pair, matching 5A's rule that a permanent state must not report "try again later".

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/sales/ -run 'TestMapHTTPErrorCommitCodes|TestChargeInvariant' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
make fmt
git add internal/sales/errors.go internal/sales/errors_test.go
git commit -m "feat(sales): add Commit error sentinels and HTTP status mapping"
```

---

## Task 5: DTOs

**Files:**
- Modify: `internal/sales/dto.go`
- Test: `internal/sales/dto_test.go`

**Interfaces:**
- Consumes: nothing from Tasks 1–4 at the type level.
- Produces: `CommitOrderDraftCommand{RequestID uuid.UUID; ServiceSessionID uuid.UUID}`, `StartNewOrderDraftCommand{RequestID; ServiceSessionID}`, `SetCheckTargetCommand{RequestID; ServiceSessionID; CheckTarget string}`; `CheckResponse`, `ChargeAllocationResponse`, `CommittedModifierResponse`; `OrderDraftResponse.CheckTarget`; `ServiceSessionResponse.Checks []CheckResponse`.

- [ ] **Step 1: Write the failing serialization tests**

Append to `internal/sales/dto_test.go`:

```go
func TestCheckResponseSerialization(t *testing.T) {
	check := sales.CheckResponse{
		ID:              uuid.New(),
		State:           "OPEN",
		ChargeVND:       85_000,
		TotalAppliedVND: 0,
		BalanceVND:      85_000,
		Payments:        make([]struct{}, 0),
		Allocations:     make([]sales.ChargeAllocationResponse, 0),
	}
	b, err := json.Marshal(check)
	require.NoError(t, err)
	require.Contains(t, string(b), `"payments":[]`)
	require.Contains(t, string(b), `"allocations":[]`)
	require.Contains(t, string(b), `"total_applied_vnd":0`)
	// pending_refund_vnd is omitted, not stubbed: Refund is outside Phase 5.
	require.NotContains(t, string(b), "pending_refund_vnd")
}

func TestChargeAllocationSerializationMarksUnsubmitted(t *testing.T) {
	alloc := sales.ChargeAllocationResponse{
		ID:                uuid.New(),
		CommittedItemID:   uuid.New(),
		Modifiers:         make([]sales.CommittedModifierResponse, 0),
		AllocatedQuantity: 2,
		AmountVND:         85_000,
	}
	b, err := json.Marshal(alloc)
	require.NoError(t, err)
	require.Contains(t, string(b), `"submitted":false`)
	require.Contains(t, string(b), `"modifiers":[]`)
}

func TestOrderDraftCarriesCheckTarget(t *testing.T) {
	draft := sales.OrderDraftResponse{
		ID:          uuid.New(),
		State:       "EDITABLE",
		CheckTarget: "CURRENT_UNPAID",
		Items:       make([]sales.DraftItemResponse, 0),
	}
	b, err := json.Marshal(draft)
	require.NoError(t, err)
	require.Contains(t, string(b), `"check_target":"CURRENT_UNPAID"`)
}

func TestServiceSessionSerializesNullDraftAfterCommit(t *testing.T) {
	session := sales.ServiceSessionResponse{
		Tables:           make([]sales.SessionTableResponse, 0),
		Checks:           make([]sales.CheckResponse, 0),
		Orders:           make([]struct{}, 0),
		PreparationUnits: make([]struct{}, 0),
		Draft:            nil,
	}
	b, err := json.Marshal(session)
	require.NoError(t, err)
	require.Contains(t, string(b), `"draft":null`)
	require.Contains(t, string(b), `"checks":[]`)
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/sales/ -run 'TestCheckResponse|TestChargeAllocation|TestOrderDraftCarries|TestServiceSessionSerializesNull' -v`
Expected: FAIL — the types and the `CheckTarget` field are undefined.

- [ ] **Step 3: Add the commands and response types**

Append to the command section of `internal/sales/dto.go`:

```go
// CommitOrderDraftCommand fixes the draft's prices into a Check.
type CommitOrderDraftCommand struct {
	RequestID        uuid.UUID `json:"request_id"`
	ServiceSessionID uuid.UUID `json:"-"`
}

// StartNewOrderDraftCommand opens the Session's next Order Draft.
type StartNewOrderDraftCommand struct {
	RequestID        uuid.UUID `json:"request_id"`
	ServiceSessionID uuid.UUID `json:"-"`
}

// SetCheckTargetCommand steers where the next Commit's charges land.
type SetCheckTargetCommand struct {
	RequestID        uuid.UUID `json:"request_id"`
	ServiceSessionID uuid.UUID `json:"-"`
	CheckTarget      string    `json:"check_target"`
}
```

Append to the response section:

```go
// CommittedModifierResponse is one frozen Modifier Option on a Committed Item.
// Names and surcharge are snapshots: a later rename in Catalog must not
// rewrite what a customer was charged for.
type CommittedModifierResponse struct {
	GroupID      uuid.UUID `json:"group_id"`
	GroupName    string    `json:"group_name"`
	OptionID     uuid.UUID `json:"option_id"`
	OptionName   string    `json:"option_name"`
	SurchargeVND int64     `json:"surcharge_vnd"`
}

// ChargeAllocationResponse is one Committed Item's charge against one Check.
//
// Submitted is a constant false in 5B and is filled by 5D, which introduces
// the orders table this flag is derived from.
type ChargeAllocationResponse struct {
	ID                 uuid.UUID                   `json:"id"`
	CommittedItemID    uuid.UUID                   `json:"committed_item_id"`
	MenuItemID         uuid.UUID                   `json:"menu_item_id"`
	CategoryName       string                      `json:"category_name"`
	Name               string                      `json:"name"`
	SizeName           *string                     `json:"size_name"`
	PreparationNote    *string                     `json:"preparation_note"`
	Modifiers          []CommittedModifierResponse `json:"modifiers"`
	CommittedQuantity  int32                       `json:"committed_quantity"`
	CommittedTotalVND  int64                       `json:"committed_total_vnd"`
	AllocatedQuantity  int32                       `json:"allocated_quantity"`
	AmountVND          int64                       `json:"amount_vnd"`
	CreatedAt          time.Time                   `json:"created_at"`
	Submitted          bool                        `json:"submitted"`
}

// CheckResponse is a grouping of charges awaiting settlement.
//
// TotalAppliedVND is the sum of the Check's Payments and is therefore always
// zero in 5B; BalanceVND is ChargeVND minus it. Both ship in their final shape
// and are filled by 5C, following the precedent ADR-008 set for
// expected_cash_vnd. PendingRefundVND is deliberately absent: Refund is
// outside Phase 5 entirely.
type CheckResponse struct {
	ID              uuid.UUID                  `json:"id"`
	State           string                     `json:"state"`
	ChargeVND       int64                      `json:"charge_vnd"`
	TotalAppliedVND int64                      `json:"total_applied_vnd"`
	BalanceVND      int64                      `json:"balance_vnd"`
	CreatedAt       time.Time                  `json:"created_at"`
	// Filled by 5C.
	Payments    []struct{}                 `json:"payments"`
	Allocations []ChargeAllocationResponse `json:"allocations"`
}
```

Add `CheckTarget` to `OrderDraftResponse`:

```go
// OrderDraftResponse is the editable Order Draft.
//
// CheckTarget belongs to the draft, not the Session: it resets to
// CURRENT_UNPAID every time a new draft opens, so a cashier who directed one
// round to a new Check does not silently direct the next one there too.
type OrderDraftResponse struct {
	ID          uuid.UUID           `json:"id"`
	State       string              `json:"state"`
	CheckTarget string              `json:"check_target"`
	Items       []DraftItemResponse `json:"items"`
}
```

Retype `Checks` on `ServiceSessionResponse` and update its doc comment:

```go
	// Filled from 5B; payments within each Check are filled by 5C.
	Checks []CheckResponse `json:"checks"`
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/sales/ -run 'TestCheckResponse|TestChargeAllocation|TestOrderDraftCarries|TestServiceSessionSerializesNull' -v`
Expected: PASS. `go build ./...` will fail until Task 6 updates `projection.go`'s `newServiceSessionResponse`; that is expected and fixed there.

- [ ] **Step 5: Commit**

```bash
make fmt
git add internal/sales/dto.go internal/sales/dto_test.go
git commit -m "feat(sales): add Check and Charge Allocation DTOs and the draft check target"
```

---

## Task 6: Check Projection

**Files:**
- Modify: `internal/sales/projection.go`
- Test: `internal/sales/projection_integration_test.go`

**Interfaces:**
- Consumes: Task 2's `ListSessionChecks`, `ListCheckAllocations`, `ListCommittedItemModifiers`, `GetOrderDraftCheckTarget`; Task 5's response types; Task 4's `ErrChargeInvariantViolated`.
- Produces: `loadChecks(ctx, q, sessionID) ([]CheckResponse, error)`, wired into `LoadServiceSession`.

- [ ] **Step 1: Write the failing integration test**

Append to `internal/sales/projection_integration_test.go`:

```go
func TestProjectionRejectsCorruptedCheckCharge(t *testing.T) {
	env := newSalesEnv(t)
	session := env.commitOneItemSession(t) // helper added in Task 8

	_, err := env.DB.Exec(
		`UPDATE checks SET charge_vnd = charge_vnd + 1 WHERE service_session_id = $1`,
		session.ID)
	require.NoError(t, err)

	_, err = env.GetSession(t, session.ID)
	require.ErrorIs(t, err, sales.ErrChargeInvariantViolated)
}
```

This test depends on Task 8's `commitOneItemSession` helper. Implement Task 6's production code now and enable this test at the end of Task 8; the remaining steps here are verified through the existing projection suite plus a direct SQL fixture.

- [ ] **Step 2: Run the existing projection suite to confirm the starting state**

Run: `go test -tags integration ./internal/sales/ -run TestProjection -v -p 1`
Expected: PASS — the suite still describes a Session with no Checks.

- [ ] **Step 3: Implement Check assembly**

Update `newServiceSessionResponse` in `internal/sales/projection.go`:

```go
func newServiceSessionResponse() ServiceSessionResponse {
	return ServiceSessionResponse{
		Tables:           make([]SessionTableResponse, 0),
		Checks:           make([]CheckResponse, 0),
		Orders:           make([]struct{}, 0),
		PreparationUnits: make([]struct{}, 0),
	}
}
```

Append to `internal/sales/projection.go`:

```go
// loadChecks assembles every Check of a Service Session with its allocations.
//
// The Check's stored charge_vnd is a denormalization of the sum over its
// allocations, in the same spirit as 5A's modifier_key: derived, never
// authoritative. It is stored rather than always derived because 5C freezes
// the charge of a settled or merged Check, at which point the live sum stops
// being the right answer. Every read therefore recomputes and compares, and a
// mismatch fails the read rather than serving a wrong total.
func loadChecks(ctx context.Context, q *sqlc.Queries, sessionID uuid.UUID) (
	[]CheckResponse, error,
) {
	checkRows, err := q.ListSessionChecks(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("load session checks: %w", err)
	}

	out := make([]CheckResponse, 0, len(checkRows))
	for _, row := range checkRows {
		allocations, allocatedVND, err := loadCheckAllocations(ctx, q, row.ID)
		if err != nil {
			return nil, err
		}
		if allocatedVND != row.ChargeVND {
			slog.Error("check charge does not match its allocations",
				"check_id", row.ID,
				"stored_charge_vnd", row.ChargeVND,
				"allocated_vnd", allocatedVND)
			return nil, fmt.Errorf("%w: check %s", ErrChargeInvariantViolated, row.ID)
		}

		// No Payment exists before 5C, so applied is zero and the balance is
		// the whole charge. Both ship in their final shape.
		out = append(out, CheckResponse{
			ID:              row.ID,
			State:           row.State,
			ChargeVND:       row.ChargeVND,
			TotalAppliedVND: 0,
			BalanceVND:      row.ChargeVND,
			CreatedAt:       row.CreatedAt,
			Payments:        make([]struct{}, 0),
			Allocations:     allocations,
		})
	}
	return out, nil
}

// loadCheckAllocations returns one Check's allocations and their summed amount.
func loadCheckAllocations(ctx context.Context, q *sqlc.Queries, checkID uuid.UUID) (
	[]ChargeAllocationResponse, int64, error,
) {
	rows, err := q.ListCheckAllocations(ctx, checkID)
	if err != nil {
		return nil, 0, fmt.Errorf("load check allocations: %w", err)
	}

	itemIDs := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		itemIDs = append(itemIDs, row.CommittedItemID)
	}
	modifiers, err := loadCommittedModifiers(ctx, q, itemIDs)
	if err != nil {
		return nil, 0, err
	}

	out := make([]ChargeAllocationResponse, 0, len(rows))
	var totalVND int64
	for _, row := range rows {
		amountVND, err := LineTotal(row.AllocatedQuantity, row.UnitPriceVnd)
		if err != nil {
			return nil, 0, err
		}
		totalVND, err = AddCharge(totalVND, amountVND)
		if err != nil {
			return nil, 0, err
		}

		mods := modifiers[row.CommittedItemID]
		if mods == nil {
			mods = make([]CommittedModifierResponse, 0)
		}
		out = append(out, ChargeAllocationResponse{
			ID:                row.ID,
			CommittedItemID:   row.CommittedItemID,
			MenuItemID:        row.MenuItemID,
			CategoryName:      row.CategoryName,
			Name:              row.ItemName,
			SizeName:          nullStringPtr(row.SizeName),
			PreparationNote:   nullStringPtr(row.PreparationNote),
			Modifiers:         mods,
			CommittedQuantity: row.CommittedQuantity,
			CommittedTotalVND: row.CommittedTotalVnd,
			AllocatedQuantity: row.AllocatedQuantity,
			AmountVND:         amountVND,
			CreatedAt:         row.CreatedAt,
			// Filled by 5D, which introduces the orders table this is
			// derived from.
			Submitted: false,
		})
	}
	return out, totalVND, nil
}

// loadCommittedModifiers groups frozen modifier snapshots by Committed Item.
// The query orders by (group name, option name), so presentation order comes
// from the read rather than from insert order — the table carries no ordering
// column, exactly as 5A's selected options do not.
func loadCommittedModifiers(ctx context.Context, q *sqlc.Queries, itemIDs []uuid.UUID) (
	map[uuid.UUID][]CommittedModifierResponse, error,
) {
	out := make(map[uuid.UUID][]CommittedModifierResponse, len(itemIDs))
	if len(itemIDs) == 0 {
		return out, nil
	}
	rows, err := q.ListCommittedItemModifiers(ctx, itemIDs)
	if err != nil {
		return nil, fmt.Errorf("load committed item modifiers: %w", err)
	}
	for _, row := range rows {
		out[row.CommittedItemID] = append(out[row.CommittedItemID], CommittedModifierResponse{
			GroupID:      row.ModifierGroupID,
			GroupName:    row.ModifierGroupName,
			OptionID:     row.ModifierOptionID,
			OptionName:   row.ModifierOptionName,
			SurchargeVND: row.SurchargeVnd,
		})
	}
	return out, nil
}
```

Add `"log/slog"` to `projection.go`'s imports. If `nullStringPtr` does not already exist in the package, add it next to `nullUUID` in `draft_add.go`:

```go
// nullStringPtr converts a nullable text column to an optional string.
func nullStringPtr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	return &v.String
}
```

In `LoadServiceSession`, set the draft's `CheckTarget` where the draft is assembled, and call `loadChecks` before returning:

```go
	checks, err := loadChecks(ctx, q, sessionID)
	if err != nil {
		return out, err
	}
	out.Checks = checks
```

Populate `CheckTarget` from the draft row already loaded there — the `LockEditableDraft` and draft-read queries return it once Task 2's `check_target` column exists; if the projection's draft query does not select it, add `d.check_target` to that query's select list rather than issuing a second read.

- [ ] **Step 4: Run the projection suite to verify nothing regressed**

Run: `go build ./... && go test -tags integration ./internal/sales/ -run TestProjection -v -p 1`
Expected: build succeeds; PASS — a Session with no Checks still projects `checks: []`, and the draft now carries `check_target`.

- [ ] **Step 5: Commit**

```bash
make fmt
git add internal/sales/projection.go internal/sales/draft_add.go internal/sales/projection_integration_test.go
git commit -m "feat(sales): project Checks with allocations and verify the stored charge invariant"
```

---

## Task 7: Commit Revalidation And Snapshot Building

**Files:**
- Create: `internal/sales/commit.go`
- Test: `internal/sales/commit_test.go`

**Interfaces:**
- Consumes: Task 3's `LineTotal`/`AddCharge`, Task 4's sentinels.
- Produces: `type commitCandidate struct`, `type effectiveGroup struct`, `type selectedOption struct`, `type itemSnapshot struct`, `func buildSnapshot(c commitCandidate) (itemSnapshot, error)`.

This task builds the revalidation and pricing as a pure function over plain structs, so every rule is unit-testable without a database. Task 8 loads rows into these structs and persists the result.

- [ ] **Step 1: Write the failing unit tests**

Create `internal/sales/commit_test.go`:

```go
package sales_test

import (
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func candidate() sales.CommitCandidate {
	return sales.CommitCandidate{
		DraftItemID:  uuid.New(),
		MenuItemID:   uuid.New(),
		ItemName:     "Cà phê sữa",
		CategoryName: "Cà phê",
		Quantity:     2,
		ItemPriceVND: ptrInt64(25_000),
	}
}

func ptrInt64(v int64) *int64 { return &v }

func TestBuildSnapshotPricesADirectlyPricedItem(t *testing.T) {
	got, err := sales.BuildSnapshot(candidate())
	require.NoError(t, err)
	require.Equal(t, int64(25_000), got.UnitPriceVND)
	require.Equal(t, int64(50_000), got.TotalVND)
	require.Nil(t, got.SizeName)
}

func TestBuildSnapshotAddsSurchargesToTheBasePrice(t *testing.T) {
	groupID := uuid.New()
	c := candidate()
	c.EffectiveGroups = []sales.EffectiveGroup{
		{GroupID: groupID, MinSelections: 0, MaxSelections: 2},
	}
	c.Selected = []sales.SelectedOption{
		{GroupID: groupID, OptionID: uuid.New(), OptionName: "Trân châu", SurchargeVND: 5_000},
		{GroupID: groupID, OptionID: uuid.New(), OptionName: "Thạch", SurchargeVND: 3_000},
	}
	got, err := sales.BuildSnapshot(c)
	require.NoError(t, err)
	require.Equal(t, int64(33_000), got.UnitPriceVND)
	require.Equal(t, int64(66_000), got.TotalVND)
	require.Len(t, got.Modifiers, 2)
}

func TestBuildSnapshotRequiresASizeForASizedItem(t *testing.T) {
	c := candidate()
	c.ItemPriceVND = nil // sized items carry no own price
	_, err := sales.BuildSnapshot(c)
	require.ErrorIs(t, err, sales.ErrCommitSizeRequired)
	require.Contains(t, err.Error(), "Cà phê sữa")
}

func TestBuildSnapshotRejectsASizeOnADirectlyPricedItem(t *testing.T) {
	c := candidate()
	sizeID := uuid.New()
	c.SizeID = &sizeID
	c.Size = &sales.CommitSize{
		ID: sizeID, MenuItemID: c.MenuItemID, Name: "Lớn", PriceVND: 30_000, Available: true,
	}
	_, err := sales.BuildSnapshot(c)
	require.ErrorIs(t, err, sales.ErrCommitSizeInvalid)
}

func TestBuildSnapshotRejectsAForeignSize(t *testing.T) {
	c := candidate()
	c.ItemPriceVND = nil
	sizeID := uuid.New()
	c.SizeID = &sizeID
	c.Size = &sales.CommitSize{
		ID: sizeID, MenuItemID: uuid.New(), Name: "Lớn", PriceVND: 30_000, Available: true,
	}
	_, err := sales.BuildSnapshot(c)
	require.ErrorIs(t, err, sales.ErrCommitSizeInvalid)
}

func TestBuildSnapshotRejectsARetiredSizeBeforeAnUnavailableOne(t *testing.T) {
	c := candidate()
	c.ItemPriceVND = nil
	sizeID := uuid.New()
	c.SizeID = &sizeID
	c.Size = &sales.CommitSize{
		ID: sizeID, MenuItemID: c.MenuItemID, Name: "Lớn",
		PriceVND: 30_000, Available: false, Retired: true,
	}
	_, err := sales.BuildSnapshot(c)
	require.ErrorIs(t, err, sales.ErrCommitSizeRetired)
}

func TestBuildSnapshotRejectsAnOptionOutsideTheEffectiveGroups(t *testing.T) {
	c := candidate()
	c.EffectiveGroups = []sales.EffectiveGroup{
		{GroupID: uuid.New(), MinSelections: 0, MaxSelections: 1},
	}
	c.Selected = []sales.SelectedOption{
		{GroupID: uuid.New(), OptionID: uuid.New(), OptionName: "Lạ", SurchargeVND: 0},
	}
	_, err := sales.BuildSnapshot(c)
	require.ErrorIs(t, err, sales.ErrCommitModifierOptionInvalid)
}

func TestBuildSnapshotEnforcesGroupSelectionCounts(t *testing.T) {
	groupID := uuid.New()

	t.Run("below the minimum", func(t *testing.T) {
		c := candidate()
		c.EffectiveGroups = []sales.EffectiveGroup{
			{GroupID: groupID, MinSelections: 1, MaxSelections: 1},
		}
		_, err := sales.BuildSnapshot(c)
		require.ErrorIs(t, err, sales.ErrCommitModifierGroupInvalid)
	})

	t.Run("above the maximum", func(t *testing.T) {
		c := candidate()
		c.EffectiveGroups = []sales.EffectiveGroup{
			{GroupID: groupID, MinSelections: 0, MaxSelections: 1},
		}
		c.Selected = []sales.SelectedOption{
			{GroupID: groupID, OptionID: uuid.New(), OptionName: "A"},
			{GroupID: groupID, OptionID: uuid.New(), OptionName: "B"},
		}
		_, err := sales.BuildSnapshot(c)
		require.ErrorIs(t, err, sales.ErrCommitModifierGroupInvalid)
	})

	t.Run("exactly at both bounds", func(t *testing.T) {
		c := candidate()
		c.EffectiveGroups = []sales.EffectiveGroup{
			{GroupID: groupID, MinSelections: 1, MaxSelections: 2},
		}
		c.Selected = []sales.SelectedOption{
			{GroupID: groupID, OptionID: uuid.New(), OptionName: "A"},
		}
		_, err := sales.BuildSnapshot(c)
		require.NoError(t, err)
	})
}

func TestBuildSnapshotSkipsARetiredGroupThatRequiresNothing(t *testing.T) {
	c := candidate()
	c.EffectiveGroups = []sales.EffectiveGroup{
		{GroupID: uuid.New(), MinSelections: 0, MaxSelections: 1, Retired: true},
	}
	_, err := sales.BuildSnapshot(c)
	require.NoError(t, err)
}

func TestBuildSnapshotRejectsARetiredGroupThatRequiresASelection(t *testing.T) {
	c := candidate()
	c.EffectiveGroups = []sales.EffectiveGroup{
		{GroupID: uuid.New(), MinSelections: 1, MaxSelections: 1, Retired: true},
	}
	_, err := sales.BuildSnapshot(c)
	require.ErrorIs(t, err, sales.ErrCommitModifierGroupRetired)
}

func TestBuildSnapshotRejectsARetiredOptionBeforeAnUnavailableOne(t *testing.T) {
	groupID := uuid.New()
	c := candidate()
	c.EffectiveGroups = []sales.EffectiveGroup{
		{GroupID: groupID, MinSelections: 0, MaxSelections: 1},
	}
	c.Selected = []sales.SelectedOption{
		{GroupID: groupID, OptionID: uuid.New(), OptionName: "A", Available: false, Retired: true},
	}
	_, err := sales.BuildSnapshot(c)
	require.ErrorIs(t, err, sales.ErrCommitModifierOptionRetired)
}
```

Note: `SelectedOption.Available` defaults to `false` in these fixtures, so `BuildSnapshot` must treat availability as explicitly supplied by the loader. Task 8 sets it from the query row; the passing tests above set it where it matters.

Adjust the fixtures that expect success to set `Available: true` on every selected option.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/sales/ -run TestBuildSnapshot -v`
Expected: FAIL — `BuildSnapshot` and its types are undefined.

- [ ] **Step 3: Implement the types and the pure builder**

Create `internal/sales/commit.go`:

```go
package sales

import (
	"fmt"

	"github.com/google/uuid"
)

// EffectiveGroup is one Modifier Group that applies to a Menu Item, with the
// selection rules Commit enforces. 5A's draft validation deliberately ignores
// these counts: a draft is a proposal and tolerates incompleteness. Commit is
// where completeness matters, because it is where price is fixed.
type EffectiveGroup struct {
	GroupID       uuid.UUID
	GroupName     string
	MinSelections int32
	MaxSelections int32
	Retired       bool
}

// SelectedOption is one Modifier Option chosen on a draft item.
type SelectedOption struct {
	GroupID      uuid.UUID
	GroupName    string
	OptionID     uuid.UUID
	OptionName   string
	SurchargeVND int64
	Available    bool
	Retired      bool
	GroupRetired bool
}

// CommitSize is the Size a draft item selected, as it stands now.
type CommitSize struct {
	ID         uuid.UUID
	MenuItemID uuid.UUID
	Name       string
	PriceVND   int64
	Available  bool
	Retired    bool
}

// CommitCandidate is one draft item with everything Commit revalidates it
// against, loaded and locked by the handler.
//
// ItemPriceVND is nil exactly when the Menu Item is priced through its Sizes.
type CommitCandidate struct {
	DraftItemID     uuid.UUID
	MenuItemID      uuid.UUID
	ItemName        string
	CategoryName    string
	Quantity        int32
	ItemPriceVND    *int64
	PreparationNote *string
	SizeID          *uuid.UUID
	Size            *CommitSize
	EffectiveGroups []EffectiveGroup
	Selected        []SelectedOption
}

// ItemSnapshot is the immutable commercial record Commit persists.
type ItemSnapshot struct {
	SourceDraftItemID uuid.UUID
	MenuItemID        uuid.UUID
	CategoryName      string
	ItemName          string
	SizeName          *string
	Quantity          int32
	UnitPriceVND      int64
	TotalVND          int64
	PreparationNote   *string
	Modifiers         []CommittedModifierResponse
}

// BuildSnapshot revalidates one draft item and freezes it into a snapshot.
//
// The rule order is load-bearing and reproduces the canonical source: within
// each pair, retirement is reported before unavailability, because retirement
// is permanent and must not tell staff to try again later. Every error names
// the offending item, since a cashier told only that "an item is unavailable"
// cannot act on it.
//
// The canonical duplicate-option check is not migrated: 5A's
// order_draft_item_modifier_options primary key on (draft item, option) makes
// a duplicate unrepresentable, and a check that cannot fire is noise.
func BuildSnapshot(c CommitCandidate) (ItemSnapshot, error) {
	var zero ItemSnapshot

	basePriceVND, sizeName, err := resolveBasePrice(c)
	if err != nil {
		return zero, err
	}

	if err := validateSelections(c); err != nil {
		return zero, err
	}
	if err := validateGroupRules(c); err != nil {
		return zero, err
	}

	unitPriceVND := basePriceVND
	modifiers := make([]CommittedModifierResponse, 0, len(c.Selected))
	for _, opt := range c.Selected {
		unitPriceVND, err = AddCharge(unitPriceVND, opt.SurchargeVND)
		if err != nil {
			return zero, err
		}
		modifiers = append(modifiers, CommittedModifierResponse{
			GroupID:      opt.GroupID,
			GroupName:    opt.GroupName,
			OptionID:     opt.OptionID,
			OptionName:   opt.OptionName,
			SurchargeVND: opt.SurchargeVND,
		})
	}

	totalVND, err := LineTotal(c.Quantity, unitPriceVND)
	if err != nil {
		return zero, err
	}

	return ItemSnapshot{
		SourceDraftItemID: c.DraftItemID,
		MenuItemID:        c.MenuItemID,
		CategoryName:      c.CategoryName,
		ItemName:          c.ItemName,
		SizeName:          sizeName,
		Quantity:          c.Quantity,
		UnitPriceVND:      unitPriceVND,
		TotalVND:          totalVND,
		PreparationNote:   c.PreparationNote,
		Modifiers:         modifiers,
	}, nil
}

// resolveBasePrice applies the Size rules and returns the pre-surcharge price.
func resolveBasePrice(c CommitCandidate) (int64, *string, error) {
	sized := c.ItemPriceVND == nil

	if sized && c.SizeID == nil {
		return 0, nil, fmt.Errorf("%w: %s", ErrCommitSizeRequired, c.ItemName)
	}
	if !sized && c.SizeID != nil {
		return 0, nil, fmt.Errorf("%w: %s is priced directly", ErrCommitSizeInvalid, c.ItemName)
	}
	if !sized {
		return *c.ItemPriceVND, nil, nil
	}

	size := c.Size
	if size == nil || size.MenuItemID != c.MenuItemID {
		return 0, nil, fmt.Errorf("%w: %s", ErrCommitSizeInvalid, c.ItemName)
	}
	if size.Retired {
		return 0, nil, fmt.Errorf("%w: %s", ErrCommitSizeRetired, c.ItemName)
	}
	if !size.Available {
		return 0, nil, fmt.Errorf("%w: %s", ErrCommitSizeUnavailable, c.ItemName)
	}
	name := size.Name
	return size.PriceVND, &name, nil
}

// validateSelections rejects options that are no longer selectable.
func validateSelections(c CommitCandidate) error {
	effective := make(map[uuid.UUID]struct{}, len(c.EffectiveGroups))
	for _, g := range c.EffectiveGroups {
		effective[g.GroupID] = struct{}{}
	}
	for _, opt := range c.Selected {
		// A selection valid when it was made becomes invalid if the Item's
		// Category attachments changed since.
		if _, ok := effective[opt.GroupID]; !ok {
			return fmt.Errorf("%w: %s", ErrCommitModifierOptionInvalid, c.ItemName)
		}
		if opt.Retired || opt.GroupRetired {
			return fmt.Errorf("%w: %s", ErrCommitModifierOptionRetired, c.ItemName)
		}
		if !opt.Available {
			return fmt.Errorf("%w: %s", ErrCommitModifierOptionUnavailable, c.ItemName)
		}
	}
	return nil
}

// validateGroupRules enforces each effective Group's selection counts.
func validateGroupRules(c CommitCandidate) error {
	counts := make(map[uuid.UUID]int32, len(c.EffectiveGroups))
	for _, opt := range c.Selected {
		counts[opt.GroupID]++
	}
	for _, g := range c.EffectiveGroups {
		if g.Retired {
			// A retired Group that requires nothing is simply skipped; one
			// that requires a selection can no longer be satisfied.
			if g.MinSelections > 0 {
				return fmt.Errorf("%w: %s", ErrCommitModifierGroupRetired, c.ItemName)
			}
			continue
		}
		n := counts[g.GroupID]
		if n < g.MinSelections || n > g.MaxSelections {
			return fmt.Errorf("%w: %s", ErrCommitModifierGroupInvalid, c.ItemName)
		}
	}
	return nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/sales/ -run TestBuildSnapshot -v`
Expected: PASS (all eleven cases).

- [ ] **Step 5: Commit**

```bash
make fmt
git add internal/sales/commit.go internal/sales/commit_test.go
git commit -m "feat(sales): add Commit revalidation and price freezing as a pure builder"
```

---

## Task 8: Commit Handler

**Files:**
- Modify: `internal/sales/commit.go`
- Test: `internal/sales/commit_integration_test.go`

**Interfaces:**
- Consumes: Task 7's `BuildSnapshot`/`CommitCandidate`; Task 2's queries; Task 5's `CommitOrderDraftCommand`; 5A's `lockEditableDraft`, `ExecuteMutation`, `LoadServiceSession`.
- Produces: `CommitOrderDraftHandler`, `NewCommitOrderDraftHandler(runner *Runner) *CommitOrderDraftHandler`, `Handle(ctx, actor, cmd) (int, ServiceSessionResponse, error)`; test helper `commitOneItemSession`.

- [ ] **Step 1: Write the failing integration tests**

Create `internal/sales/commit_integration_test.go` (package `sales_test`, build tag `integration`), following the fixture helpers the 5A suites already use:

```go
func TestCommitFreezesPricesAndOpensACheck(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartTakeaway(t)
	env.AddDraftItem(t, session.ID, env.CoffeeID, nil)

	got := env.Commit(t, session.ID)

	require.Nil(t, got.Draft, "the draft is committed, so the session has no editable draft")
	require.Len(t, got.Checks, 1)
	check := got.Checks[0]
	require.Equal(t, "OPEN", check.State)
	require.Len(t, check.Allocations, 1)
	require.Equal(t, check.Allocations[0].AmountVND, check.ChargeVND)
	require.Equal(t, check.ChargeVND, check.BalanceVND)
	require.Zero(t, check.TotalAppliedVND)
	require.False(t, check.Allocations[0].Submitted)
	require.Empty(t, check.Payments)
}

func TestCommitIsModeAgnostic(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartDineIn(t, env.TableID)
	env.AddDraftItem(t, session.ID, env.CoffeeID, nil)

	got := env.Commit(t, session.ID)

	require.Len(t, got.Checks, 1)
	require.Len(t, got.Checks[0].Allocations, 1)
}

func TestCommitRejectsAnEmptyDraft(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartTakeaway(t)

	_, err := env.TryCommit(t, session.ID)

	require.ErrorIs(t, err, sales.ErrEmptyDraft)
	env.RequireNoChecks(t, session.ID)
	env.RequireDraftState(t, session.ID, "EDITABLE")
}

func TestCommitReportsRetiredBeforeUnavailable(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartTakeaway(t)
	env.AddDraftItem(t, session.ID, env.CoffeeID, nil)
	env.AddDraftItem(t, session.ID, env.TeaID, nil)
	env.SetMenuItemAvailable(t, env.CoffeeID, false)
	env.RetireMenuItem(t, env.TeaID)

	_, err := env.TryCommit(t, session.ID)

	require.ErrorIs(t, err, sales.ErrCommitMenuItemRetired)
	env.RequireNoChecks(t, session.ID)
}

func TestCommitRejectionsLeaveTheDraftEditable(t *testing.T) {
	cases := []struct {
		name    string
		arrange func(t *testing.T, env *salesEnv, sessionID uuid.UUID)
		want    error
	}{
		{
			name: "unavailable menu item",
			arrange: func(t *testing.T, env *salesEnv, sessionID uuid.UUID) {
				env.AddDraftItem(t, sessionID, env.CoffeeID, nil)
				env.SetMenuItemAvailable(t, env.CoffeeID, false)
			},
			want: sales.ErrCommitMenuItemUnavailable,
		},
		{
			name: "sized item with no size",
			arrange: func(t *testing.T, env *salesEnv, sessionID uuid.UUID) {
				env.AddDraftItem(t, sessionID, env.SizedItemID, nil)
			},
			want: sales.ErrCommitSizeRequired,
		},
		{
			name: "retired size",
			arrange: func(t *testing.T, env *salesEnv, sessionID uuid.UUID) {
				env.AddDraftItem(t, sessionID, env.SizedItemID, &env.LargeSizeID)
				env.RetireSize(t, env.LargeSizeID)
			},
			want: sales.ErrCommitSizeRetired,
		},
		{
			name: "unavailable size",
			arrange: func(t *testing.T, env *salesEnv, sessionID uuid.UUID) {
				env.AddDraftItem(t, sessionID, env.SizedItemID, &env.LargeSizeID)
				env.SetSizeAvailable(t, env.LargeSizeID, false)
			},
			want: sales.ErrCommitSizeUnavailable,
		},
		{
			name: "option whose group stopped being effective",
			arrange: func(t *testing.T, env *salesEnv, sessionID uuid.UUID) {
				env.AddDraftItemWithOptions(t, sessionID, env.CoffeeID, env.ToppingOptionID)
				env.ExcludeGroupFromItem(t, env.CoffeeID, env.ToppingGroupID)
			},
			want: sales.ErrCommitModifierOptionInvalid,
		},
		{
			name: "retired option",
			arrange: func(t *testing.T, env *salesEnv, sessionID uuid.UUID) {
				env.AddDraftItemWithOptions(t, sessionID, env.CoffeeID, env.ToppingOptionID)
				env.RetireOption(t, env.ToppingOptionID)
			},
			want: sales.ErrCommitModifierOptionRetired,
		},
		{
			name: "unavailable option",
			arrange: func(t *testing.T, env *salesEnv, sessionID uuid.UUID) {
				env.AddDraftItemWithOptions(t, sessionID, env.CoffeeID, env.ToppingOptionID)
				env.SetOptionAvailable(t, env.ToppingOptionID, false)
			},
			want: sales.ErrCommitModifierOptionUnavailable,
		},
		{
			name: "unsatisfied required group",
			arrange: func(t *testing.T, env *salesEnv, sessionID uuid.UUID) {
				env.SetGroupSelectionBounds(t, env.ToppingGroupID, 1, 1)
				env.AddDraftItem(t, sessionID, env.CoffeeID, nil)
			},
			want: sales.ErrCommitModifierGroupInvalid,
		},
		{
			name: "retired group that requires a selection",
			arrange: func(t *testing.T, env *salesEnv, sessionID uuid.UUID) {
				env.SetGroupSelectionBounds(t, env.ToppingGroupID, 1, 1)
				env.AddDraftItem(t, sessionID, env.CoffeeID, nil)
				env.RetireGroup(t, env.ToppingGroupID)
			},
			want: sales.ErrCommitModifierGroupRetired,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newSalesEnv(t)
			session := env.StartTakeaway(t)
			tc.arrange(t, env, session.ID)

			_, err := env.TryCommit(t, session.ID)

			require.ErrorIs(t, err, tc.want)
			env.RequireNoChecks(t, session.ID)
			env.RequireNoCommittedItems(t, session.ID)
			env.RequireDraftState(t, session.ID, "EDITABLE")
		})
	}
}

func TestCommittedSnapshotSurvivesCatalogChanges(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartTakeaway(t)
	env.AddDraftItemWithOptions(t, session.ID, env.CoffeeID, env.ToppingOptionID)
	before := env.Commit(t, session.ID)

	env.RenameMenuItem(t, env.CoffeeID, "Tên mới")
	env.RenameOption(t, env.ToppingOptionID, "Topping mới")
	env.SetMenuItemAvailable(t, env.CoffeeID, false)

	after := env.GetSessionOK(t, session.ID)
	require.Equal(t, before.Checks[0].ChargeVND, after.Checks[0].ChargeVND)
	require.Equal(t, before.Checks[0].Allocations[0].Name, after.Checks[0].Allocations[0].Name)
	require.Equal(t,
		before.Checks[0].Allocations[0].Modifiers[0].OptionName,
		after.Checks[0].Allocations[0].Modifiers[0].OptionName)
}

func TestCommitIsIdempotent(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartTakeaway(t)
	env.AddDraftItem(t, session.ID, env.CoffeeID, nil)
	requestID := uuid.New()

	first := env.CommitWithRequestID(t, session.ID, requestID)
	second := env.CommitWithRequestID(t, session.ID, requestID)

	require.Equal(t, first.Checks[0].ID, second.Checks[0].ID)
	env.RequireCheckCount(t, session.ID, 1)
}

func TestCommitDeniedForBarista(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartTakeaway(t)
	env.AddDraftItem(t, session.ID, env.CoffeeID, nil)

	_, err := env.AsBarista().TryCommit(t, session.ID)

	require.ErrorIs(t, err, sales.ErrForbidden)
	env.RequireNoChecks(t, session.ID)
}

func TestCommitRequiresAnOpenShift(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartTakeaway(t)
	env.AddDraftItem(t, session.ID, env.CoffeeID, nil)
	env.CloseShift(t)

	_, err := env.TryCommit(t, session.ID)

	require.ErrorIs(t, err, sales.ErrEditableDraftNotFound)
}
```

Add the fixture helpers this suite needs to the existing shared test support file, mirroring the helpers 5A's suites already define. Add `commitOneItemSession` there too, since Task 6's projection test consumes it:

```go
// commitOneItemSession opens a Takeaway Session, adds one item, and commits.
func (e *salesEnv) commitOneItemSession(t *testing.T) sales.ServiceSessionResponse {
	t.Helper()
	session := e.StartTakeaway(t)
	e.AddDraftItem(t, session.ID, e.CoffeeID, nil)
	return e.Commit(t, session.ID)
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -tags integration ./internal/sales/ -run TestCommit -v -p 1`
Expected: FAIL — the handler does not exist.

- [ ] **Step 3: Implement the handler**

Append to `internal/sales/commit.go`:

```go
type commitFingerprint struct {
	ServiceSessionID uuid.UUID `json:"service_session_id"`
}

type commitAudit struct {
	ServiceSessionID   uuid.UUID `json:"service_session_id"`
	OrderDraftID       uuid.UUID `json:"order_draft_id"`
	CheckID            uuid.UUID `json:"check_id"`
	CommittedAmountVND int64     `json:"committed_amount_vnd"`
	CommittedItemCount int       `json:"committed_item_count"`
}

// CommitOrderDraftHandler fixes an Order Draft's prices into a Check.
type CommitOrderDraftHandler struct{ runner *Runner }

// NewCommitOrderDraftHandler creates a new CommitOrderDraftHandler.
func NewCommitOrderDraftHandler(runner *Runner) *CommitOrderDraftHandler {
	return &CommitOrderDraftHandler{runner: runner}
}

// Handle revalidates the draft, freezes its items, and charges a Check.
//
// Any failure aborts the whole Commit: there is no partial commit, and the
// draft stays EDITABLE and editable so staff can fix what was rejected.
func (h *CommitOrderDraftHandler) Handle(ctx context.Context, actor Actor,
	cmd CommitOrderDraftCommand,
) (int, ServiceSessionResponse, error) {
	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpCommitOrderDraft,
		Fingerprint: commitFingerprint{ServiceSessionID: cmd.ServiceSessionID},
		Required:    []string{CapSalesOperate},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, ServiceSessionResponse, AuditRecord, error) {
			var zero ServiceSessionResponse
			q := mc.Queries

			draft, err := lockEditableDraft(ctx, q, cmd.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}

			candidates, err := loadCommitCandidates(ctx, q, draft.OrderDraftID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}

			committedAt := time.Now()
			snapshots := make([]ItemSnapshot, 0, len(candidates))
			var committedAmountVND int64
			for _, candidate := range candidates {
				snapshot, err := BuildSnapshot(candidate)
				if err != nil {
					return 0, zero, AuditRecord{}, err
				}
				committedAmountVND, err = AddCharge(committedAmountVND, snapshot.TotalVND)
				if err != nil {
					return 0, zero, AuditRecord{}, err
				}
				snapshots = append(snapshots, snapshot)
			}

			target, err := q.GetOrderDraftCheckTarget(ctx, draft.OrderDraftID)
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("load draft check target: %w", err)
			}
			checkID, chargeVND, err := resolveTargetCheck(ctx, q,
				cmd.ServiceSessionID, target, committedAt)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			chargeVND, err = AddCharge(chargeVND, committedAmountVND)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}

			if err := persistSnapshots(ctx, q, draft.OrderDraftID, checkID,
				snapshots, committedAt); err != nil {
				return 0, zero, AuditRecord{}, err
			}
			if err := q.RaiseCheckCharge(ctx, sqlc.RaiseCheckChargeParams{
				ID: checkID, ChargeVnd: chargeVND,
			}); err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("raise check charge: %w", err)
			}
			if err := q.MarkOrderDraftCommitted(ctx, draft.OrderDraftID); err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("mark draft committed: %w", err)
			}

			result, err := LoadServiceSession(ctx, q, cmd.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}

			return 200, result, AuditRecord{
				EventType: EventOrderDraftCommitted,
				Details: commitAudit{
					ServiceSessionID:   cmd.ServiceSessionID,
					OrderDraftID:       draft.OrderDraftID,
					CheckID:            checkID,
					CommittedAmountVND: committedAmountVND,
					CommittedItemCount: len(snapshots),
				},
			}, nil
		})
}
```

- [ ] **Step 4: Implement the loaders and writers**

Append to `internal/sales/commit.go`:

```go
// loadCommitCandidates reads and locks everything Commit revalidates against.
//
// Lock order is Sales rows before Catalog rows, as 5A established. The draft
// items are locked FOR UPDATE by the query; the Catalog rows are locked FOR
// SHARE, which blocks internal/catalog's FOR UPDATE mutations without
// serializing two concurrent Commits that share a menu item. See ADR-015.
func loadCommitCandidates(ctx context.Context, q *sqlc.Queries, draftID uuid.UUID) (
	[]CommitCandidate, error,
) {
	itemRows, err := q.LockDraftItemsForCommit(ctx, draftID)
	if err != nil {
		return nil, fmt.Errorf("lock draft items for commit: %w", err)
	}
	if len(itemRows) == 0 {
		return nil, fmt.Errorf("%w: draft %s", ErrEmptyDraft, draftID)
	}

	// Whole-draft checks run before any per-item rule, and retirement is
	// reported before unavailability across the entire draft. The precedence
	// is observable — a draft holding one of each reports the retired item
	// regardless of draft order — so it is reproduced rather than tidied into
	// per-item ordering.
	for _, row := range itemRows {
		if row.ItemRetired {
			return nil, fmt.Errorf("%w: %s", ErrCommitMenuItemRetired, row.ItemName)
		}
	}
	for _, row := range itemRows {
		if !row.ItemAvailable {
			return nil, fmt.Errorf("%w: %s", ErrCommitMenuItemUnavailable, row.ItemName)
		}
	}

	draftItemIDs := make([]uuid.UUID, 0, len(itemRows))
	menuItemIDs := make([]uuid.UUID, 0, len(itemRows))
	sizeIDs := make([]uuid.UUID, 0, len(itemRows))
	for _, row := range itemRows {
		draftItemIDs = append(draftItemIDs, row.ID)
		menuItemIDs = append(menuItemIDs, row.MenuItemID)
		if row.SizeID.Valid {
			sizeIDs = append(sizeIDs, row.SizeID.UUID)
		}
	}

	sizes, err := loadCommitSizes(ctx, q, sizeIDs)
	if err != nil {
		return nil, err
	}
	groups, err := loadEffectiveGroups(ctx, q, menuItemIDs)
	if err != nil {
		return nil, err
	}
	selected, err := loadSelectedOptions(ctx, q, draftItemIDs)
	if err != nil {
		return nil, err
	}

	out := make([]CommitCandidate, 0, len(itemRows))
	for _, row := range itemRows {
		candidate := CommitCandidate{
			DraftItemID:     row.ID,
			MenuItemID:      row.MenuItemID,
			ItemName:        row.ItemName,
			CategoryName:    row.CategoryName,
			Quantity:        row.Quantity,
			PreparationNote: nullStringPtr(row.PreparationNote),
			EffectiveGroups: groups[row.MenuItemID],
			Selected:        selected[row.ID],
		}
		if row.ItemPriceVnd.Valid {
			price := row.ItemPriceVnd.Int64
			candidate.ItemPriceVND = &price
		}
		if row.SizeID.Valid {
			sizeID := row.SizeID.UUID
			candidate.SizeID = &sizeID
			if size, ok := sizes[sizeID]; ok {
				candidate.Size = &size
			}
		}
		out = append(out, candidate)
	}
	return out, nil
}

func loadCommitSizes(ctx context.Context, q *sqlc.Queries, sizeIDs []uuid.UUID) (
	map[uuid.UUID]CommitSize, error,
) {
	out := make(map[uuid.UUID]CommitSize, len(sizeIDs))
	if len(sizeIDs) == 0 {
		return out, nil
	}
	rows, err := q.LockMenuItemSizesForCommit(ctx, sizeIDs)
	if err != nil {
		return nil, fmt.Errorf("lock menu item sizes for commit: %w", err)
	}
	for _, row := range rows {
		out[row.ID] = CommitSize{
			ID:         row.ID,
			MenuItemID: row.MenuItemID,
			Name:       row.Name,
			PriceVND:   row.PriceVnd,
			Available:  row.Available,
			Retired:    row.SizeRetired,
		}
	}
	return out, nil
}

func loadEffectiveGroups(ctx context.Context, q *sqlc.Queries, menuItemIDs []uuid.UUID) (
	map[uuid.UUID][]EffectiveGroup, error,
) {
	rows, err := q.ListEffectiveModifierGroupsForCommit(ctx, menuItemIDs)
	if err != nil {
		return nil, fmt.Errorf("resolve effective modifier groups: %w", err)
	}
	out := make(map[uuid.UUID][]EffectiveGroup, len(menuItemIDs))
	for _, row := range rows {
		out[row.MenuItemID] = append(out[row.MenuItemID], EffectiveGroup{
			GroupID:       row.ModifierGroupID,
			GroupName:     row.GroupName,
			MinSelections: row.MinSelections,
			MaxSelections: row.MaxSelections,
			Retired:       row.GroupRetired,
		})
	}
	return out, nil
}

func loadSelectedOptions(ctx context.Context, q *sqlc.Queries, draftItemIDs []uuid.UUID) (
	map[uuid.UUID][]SelectedOption, error,
) {
	rows, err := q.ListDraftItemOptionsForCommit(ctx, draftItemIDs)
	if err != nil {
		return nil, fmt.Errorf("load selected modifier options: %w", err)
	}
	out := make(map[uuid.UUID][]SelectedOption, len(draftItemIDs))
	for _, row := range rows {
		out[row.OrderDraftItemID] = append(out[row.OrderDraftItemID], SelectedOption{
			GroupID:      row.GroupID,
			GroupName:    row.GroupName,
			OptionID:     row.OptionID,
			OptionName:   row.OptionName,
			SurchargeVND: row.SurchargeVnd,
			Available:    row.Available,
			Retired:      row.OptionRetired,
			GroupRetired: row.GroupRetired,
		})
	}
	return out, nil
}

// persistSnapshots writes the Committed Items, their frozen modifiers, and one
// full-quantity Charge Allocation each.
func persistSnapshots(ctx context.Context, q *sqlc.Queries, draftID, checkID uuid.UUID,
	snapshots []ItemSnapshot, committedAt time.Time,
) error {
	for _, snapshot := range snapshots {
		itemID, err := q.InsertCommittedItem(ctx, sqlc.InsertCommittedItemParams{
			OrderDraftID:      draftID,
			SourceDraftItemID: snapshot.SourceDraftItemID,
			MenuItemID:        snapshot.MenuItemID,
			CategoryName:      snapshot.CategoryName,
			ItemName:          snapshot.ItemName,
			SizeName:          nullString(snapshot.SizeName),
			Quantity:          snapshot.Quantity,
			UnitPriceVnd:      snapshot.UnitPriceVND,
			TotalVnd:          snapshot.TotalVND,
			PreparationNote:   nullString(snapshot.PreparationNote),
			CommittedAt:       committedAt,
		})
		if err != nil {
			return fmt.Errorf("insert committed item: %w", err)
		}

		for _, modifier := range snapshot.Modifiers {
			if err := q.InsertCommittedItemModifierOption(ctx,
				sqlc.InsertCommittedItemModifierOptionParams{
					CommittedItemID:    itemID,
					ModifierGroupID:    modifier.GroupID,
					ModifierGroupName:  modifier.GroupName,
					ModifierOptionID:   modifier.OptionID,
					ModifierOptionName: modifier.OptionName,
					SurchargeVnd:       modifier.SurchargeVND,
				}); err != nil {
				return fmt.Errorf("insert committed item modifier option: %w", err)
			}
		}

		// 5B always allocates the full committed quantity to one Check. 5C's
		// Split is what makes a partial allocation possible.
		if err := q.InsertChargeAllocation(ctx, sqlc.InsertChargeAllocationParams{
			CommittedItemID: itemID,
			CheckID:         checkID,
			Quantity:        snapshot.Quantity,
			CreatedAt:       committedAt,
		}); err != nil {
			return fmt.Errorf("insert charge allocation: %w", err)
		}
	}
	return nil
}

// nullString converts an optional string to the nullable text type the
// generated queries write.
func nullString(v *string) sql.NullString {
	if v == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *v, Valid: true}
}
```

Add `"context"`, `"database/sql"`, `"time"`, and the `sqlc` import to `commit.go`.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test -tags integration ./internal/sales/ -run TestCommit -v -p 1`
Expected: PASS. `resolveTargetCheck` arrives in Task 9 — implement it there and stub it here as the `NEW_CHECK` path only if this task is executed standalone; otherwise execute Task 9 before running this step.

- [ ] **Step 6: Enable Task 6's invariant test**

Run: `go test -tags integration ./internal/sales/ -run TestProjectionRejectsCorruptedCheckCharge -v -p 1`
Expected: PASS — the corrupted charge fails the read.

- [ ] **Step 7: Commit**

```bash
make fmt
git add internal/sales/commit.go internal/sales/commit_integration_test.go internal/sales/projection_integration_test.go
git commit -m "feat(sales): add the Commit handler with full draft revalidation"
```

---

## Task 9: Check Targeting

**Files:**
- Modify: `internal/sales/commit.go`
- Test: `internal/sales/check_targeting_integration_test.go`

**Interfaces:**
- Consumes: Task 2's `LockCurrentOpenCheck`, `InsertCheck`.
- Produces: `resolveTargetCheck(ctx, q, sessionID uuid.UUID, target string, at time.Time) (uuid.UUID, int64, error)`.

Per the spec's accepted consequences, a Session can commit only once in 5B, so the reuse branch is unreachable through the API. These tests seed a second draft directly, exactly as Phase 3's tests seeded `service_sessions`, so the path 5D depends on ships tested rather than merely written.

- [ ] **Step 1: Write the failing integration tests**

Create `internal/sales/check_targeting_integration_test.go`:

```go
func TestCurrentUnpaidReusesTheOpenCheck(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartTakeaway(t)
	env.AddDraftItem(t, session.ID, env.CoffeeID, nil)
	first := env.Commit(t, session.ID)
	require.Len(t, first.Checks, 1)

	// 5B cannot open a second draft through the API, so seed one directly.
	env.SeedEditableDraft(t, session.ID, "CURRENT_UNPAID")
	env.AddDraftItem(t, session.ID, env.CoffeeID, nil)
	second := env.Commit(t, session.ID)

	require.Len(t, second.Checks, 1, "the charge joined the existing check")
	require.Equal(t, first.Checks[0].ID, second.Checks[0].ID)
	require.Equal(t, 2*first.Checks[0].ChargeVND, second.Checks[0].ChargeVND)
	require.Len(t, second.Checks[0].Allocations, 2)
}

func TestNewCheckAlwaysOpensAnotherCheck(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartTakeaway(t)
	env.AddDraftItem(t, session.ID, env.CoffeeID, nil)
	first := env.Commit(t, session.ID)

	env.SeedEditableDraft(t, session.ID, "NEW_CHECK")
	env.AddDraftItem(t, session.ID, env.CoffeeID, nil)
	second := env.Commit(t, session.ID)

	require.Len(t, second.Checks, 2)
	require.NotEqual(t, second.Checks[0].ID, second.Checks[1].ID)
	for _, check := range second.Checks {
		require.Equal(t, first.Checks[0].ChargeVND, check.ChargeVND)
		require.Len(t, check.Allocations, 1)
	}
}

func TestFirstCommitOpensACheckRegardlessOfTarget(t *testing.T) {
	for _, target := range []string{"CURRENT_UNPAID", "NEW_CHECK"} {
		t.Run(target, func(t *testing.T) {
			env := newSalesEnv(t)
			session := env.StartTakeaway(t)
			env.SetCheckTarget(t, session.ID, target)
			env.AddDraftItem(t, session.ID, env.CoffeeID, nil)

			got := env.Commit(t, session.ID)

			require.Len(t, got.Checks, 1)
			require.Equal(t, "OPEN", got.Checks[0].State)
		})
	}
}

// A new Check is never observable at zero: it is raised inside the same
// transaction that creates it.
func TestNewCheckIsNeverObservableAtZero(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartTakeaway(t)
	env.AddDraftItem(t, session.ID, env.CoffeeID, nil)

	got := env.Commit(t, session.ID)

	require.Positive(t, got.Checks[0].ChargeVND)
}
```

Add `SeedEditableDraft` and `SetCheckTarget` to the shared test support file:

```go
// SeedEditableDraft inserts an EDITABLE draft directly. 5B's
// START_NEW_ORDER_DRAFT refuses while a COMMITTED draft has no Order, which
// no phase before 5D can produce, so the reuse path is reachable only this
// way. See the spec's accepted consequences.
func (e *salesEnv) SeedEditableDraft(t *testing.T, sessionID uuid.UUID, target string) {
	t.Helper()
	_, err := e.DB.Exec(
		`INSERT INTO order_drafts (service_session_id, state, check_target)
		 VALUES ($1, 'EDITABLE', $2)`, sessionID, target)
	require.NoError(t, err)
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -tags integration ./internal/sales/ -run 'TestCurrentUnpaid|TestNewCheck|TestFirstCommit' -v -p 1`
Expected: FAIL — `resolveTargetCheck` is undefined.

- [ ] **Step 3: Implement target resolution**

Append to `internal/sales/commit.go`:

```go
// resolveTargetCheck returns the Check the Commit's charges join, creating one
// when needed, and its current charge.
//
// CURRENT_UNPAID reuses the Session's most recent OPEN Check; NEW_CHECK always
// opens one. The name is canonical and "unpaid" is vacuous in 5B, where no
// Check can be paid — the state = 'OPEN' filter is what gives it meaning from
// 5C, when it must skip settled Checks and reuse only one still awaiting
// money.
func resolveTargetCheck(ctx context.Context, q *sqlc.Queries, sessionID uuid.UUID,
	target string, at time.Time,
) (uuid.UUID, int64, error) {
	if target == CheckTargetCurrentUnpaid {
		existing, err := q.LockCurrentOpenCheck(ctx, sessionID)
		if err == nil {
			return existing.ID, existing.ChargeVnd, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return uuid.Nil, 0, fmt.Errorf("lock current open check: %w", err)
		}
	}

	created, err := q.InsertCheck(ctx, sqlc.InsertCheckParams{
		ServiceSessionID: sessionID,
		CreatedAt:        at,
	})
	if err != nil {
		return uuid.Nil, 0, fmt.Errorf("insert check: %w", err)
	}
	return created.ID, created.ChargeVnd, nil
}
```

Add `"errors"` to `commit.go`'s imports.

The canonical `CHECK_CREATION_FAILED` is deliberately not implemented: it guards an `INSERT ... RETURNING` yielding no row, which is a programming defect rather than a business state — the category 5A §11 already declared not migrated.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test -tags integration ./internal/sales/ -run 'TestCurrentUnpaid|TestNewCheck|TestFirstCommit' -v -p 1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
make fmt
git add internal/sales/commit.go internal/sales/check_targeting_integration_test.go
git commit -m "feat(sales): resolve the Commit target check from the draft's check target"
```

---

## Task 10: Start New Order Draft

**Files:**
- Create: `internal/sales/draft_rounds.go`
- Test: `internal/sales/draft_rounds_integration_test.go`

**Interfaces:**
- Consumes: Task 2's `FindBlockingDraft`, `InsertOrderDraftForSession`, `LockServiceSessionForUpdate`, `GetOpenSalesShiftID`; Task 5's `StartNewOrderDraftCommand`.
- Produces: `StartNewOrderDraftHandler`, `NewStartNewOrderDraftHandler(runner *Runner) *StartNewOrderDraftHandler`.

- [ ] **Step 1: Write the failing integration tests**

Create `internal/sales/draft_rounds_integration_test.go`:

```go
func TestStartNewOrderDraftRejectedWhileADraftIsEditable(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartTakeaway(t)

	_, err := env.TryStartNewDraft(t, session.ID)

	require.ErrorIs(t, err, sales.ErrNewOrderDraftNotAvailable)
}

// In 5B every COMMITTED draft blocks, because the orders table 5D introduces
// does not exist yet. The rule exists to stop staff stacking rounds ahead of
// the kitchen; relaxing it now would ship a rule no phase wants.
func TestStartNewOrderDraftRejectedAfterCommitUntilSubmitExists(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartTakeaway(t)
	env.AddDraftItem(t, session.ID, env.CoffeeID, nil)
	env.Commit(t, session.ID)

	_, err := env.TryStartNewDraft(t, session.ID)

	require.ErrorIs(t, err, sales.ErrNewOrderDraftNotAvailable)
}

func TestStartNewOrderDraftRejectedForAnUnknownSession(t *testing.T) {
	env := newSalesEnv(t)

	_, err := env.TryStartNewDraft(t, uuid.New())

	require.ErrorIs(t, err, sales.ErrServiceSessionNotFound)
}

func TestStartNewOrderDraftDeniedForBarista(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartTakeaway(t)

	_, err := env.AsBarista().TryStartNewDraft(t, session.ID)

	require.ErrorIs(t, err, sales.ErrForbidden)
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -tags integration ./internal/sales/ -run TestStartNewOrderDraft -v -p 1`
Expected: FAIL — the handler does not exist.

- [ ] **Step 3: Implement the handler**

Create `internal/sales/draft_rounds.go`:

```go
package sales

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

type sessionScopedFingerprint struct {
	ServiceSessionID uuid.UUID `json:"service_session_id"`
}

type orderDraftStartedAudit struct {
	ServiceSessionID uuid.UUID `json:"service_session_id"`
	OrderDraftID     uuid.UUID `json:"order_draft_id"`
}

// StartNewOrderDraftHandler opens the Session's next Order Draft.
//
// This is a Session round-lifecycle operation, not a draft-item operation: it
// locks the Session rather than a draft, and decides whether a new round may
// begin at all. It is grouped with the Check-target command for that reason.
type StartNewOrderDraftHandler struct{ runner *Runner }

// NewStartNewOrderDraftHandler creates a new StartNewOrderDraftHandler.
func NewStartNewOrderDraftHandler(runner *Runner) *StartNewOrderDraftHandler {
	return &StartNewOrderDraftHandler{runner: runner}
}

// Handle opens a new EDITABLE draft when no blocking draft stands in the way.
func (h *StartNewOrderDraftHandler) Handle(ctx context.Context, actor Actor,
	cmd StartNewOrderDraftCommand,
) (int, ServiceSessionResponse, error) {
	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpStartNewOrderDraft,
		Fingerprint: sessionScopedFingerprint{ServiceSessionID: cmd.ServiceSessionID},
		Required:    []string{CapSalesOperate},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, ServiceSessionResponse, AuditRecord, error) {
			var zero ServiceSessionResponse
			q := mc.Queries

			shiftID, err := q.GetOpenSalesShiftID(ctx)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return 0, zero, AuditRecord{}, fmt.Errorf(
						"%w: no open sales shift", ErrOpenShiftRequired)
				}
				return 0, zero, AuditRecord{}, fmt.Errorf("load open sales shift: %w", err)
			}

			session, err := q.LockServiceSessionForUpdate(ctx, cmd.ServiceSessionID)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return 0, zero, AuditRecord{}, fmt.Errorf(
						"%w: %s", ErrServiceSessionNotFound, cmd.ServiceSessionID)
				}
				return 0, zero, AuditRecord{}, fmt.Errorf("lock service session: %w", err)
			}
			if session.State != StateActive {
				return 0, zero, AuditRecord{}, fmt.Errorf(
					"%w: %s", ErrServiceSessionClosed, cmd.ServiceSessionID)
			}
			if session.SalesShiftID != shiftID {
				return 0, zero, AuditRecord{}, fmt.Errorf(
					"%w: session belongs to another shift", ErrOpenShiftRequired)
			}

			if _, err := q.FindBlockingDraft(ctx, cmd.ServiceSessionID); err == nil {
				return 0, zero, AuditRecord{}, fmt.Errorf(
					"%w: %s", ErrNewOrderDraftNotAvailable, cmd.ServiceSessionID)
			} else if !errors.Is(err, sql.ErrNoRows) {
				return 0, zero, AuditRecord{}, fmt.Errorf("find blocking draft: %w", err)
			}

			// The new draft takes check_target's column default, so a cashier
			// who directed one round to a new Check does not silently direct
			// the next one there too.
			draft, err := q.InsertOrderDraftForSession(ctx,
				sqlc.InsertOrderDraftForSessionParams{
					ServiceSessionID: cmd.ServiceSessionID,
					CreatedAt:        time.Now(),
				})
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("insert order draft: %w", err)
			}

			result, err := LoadServiceSession(ctx, q, cmd.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}

			return 200, result, AuditRecord{
				EventType: EventOrderDraftStarted,
				Details: orderDraftStartedAudit{
					ServiceSessionID: cmd.ServiceSessionID,
					OrderDraftID:     draft.ID,
				},
			}, nil
		})
}
```

Confirm `LockServiceSessionForUpdate`'s row exposes `State` and `SalesShiftID`; if it does not, extend that query's select list rather than issuing a second read.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test -tags integration ./internal/sales/ -run TestStartNewOrderDraft -v -p 1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
make fmt
git add internal/sales/draft_rounds.go internal/sales/draft_rounds_integration_test.go
git commit -m "feat(sales): add the start-new-order-draft round lifecycle command"
```

---

## Task 11: Set Check Target

**Files:**
- Modify: `internal/sales/draft_rounds.go`
- Test: `internal/sales/draft_rounds_integration_test.go`

**Interfaces:**
- Consumes: Task 2's `SetOrderDraftCheckTarget`; 5A's `lockEditableDraft`; Task 3's `ValidateCheckTarget`.
- Produces: `SetCheckTargetHandler`, `NewSetCheckTargetHandler(runner *Runner) *SetCheckTargetHandler`.

- [ ] **Step 1: Write the failing integration tests**

Append to `internal/sales/draft_rounds_integration_test.go`:

```go
func TestSetCheckTargetUpdatesTheDraft(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartTakeaway(t)

	got := env.SetCheckTarget(t, session.ID, "NEW_CHECK")

	require.Equal(t, "NEW_CHECK", got.Draft.CheckTarget)
}

func TestCheckTargetDefaultsToCurrentUnpaid(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartTakeaway(t)

	require.Equal(t, "CURRENT_UNPAID", session.Draft.CheckTarget)
}

// The target belongs to the draft, not the Session.
func TestCheckTargetResetsWhenANewDraftOpens(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartTakeaway(t)
	env.SetCheckTarget(t, session.ID, "NEW_CHECK")
	env.AddDraftItem(t, session.ID, env.CoffeeID, nil)
	env.Commit(t, session.ID)

	env.SeedEditableDraft(t, session.ID, "CURRENT_UNPAID")

	got := env.GetSessionOK(t, session.ID)
	require.Equal(t, "CURRENT_UNPAID", got.Draft.CheckTarget)
}

func TestSetCheckTargetRejectedOnceCommitted(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartTakeaway(t)
	env.AddDraftItem(t, session.ID, env.CoffeeID, nil)
	env.Commit(t, session.ID)

	_, err := env.TrySetCheckTarget(t, session.ID, "NEW_CHECK")

	require.ErrorIs(t, err, sales.ErrEditableDraftNotFound)
}

func TestSetCheckTargetRejectsAnUnknownValue(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartTakeaway(t)

	_, err := env.TrySetCheckTarget(t, session.ID, "PAID")

	require.ErrorIs(t, err, sales.ErrInvalidCheckTarget)
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -tags integration ./internal/sales/ -run 'TestSetCheckTarget|TestCheckTarget' -v -p 1`
Expected: FAIL — the handler does not exist.

- [ ] **Step 3: Implement the handler**

Append to `internal/sales/draft_rounds.go`:

```go
type setCheckTargetFingerprint struct {
	ServiceSessionID uuid.UUID `json:"service_session_id"`
	CheckTarget      string    `json:"check_target"`
}

type checkTargetAudit struct {
	ServiceSessionID uuid.UUID `json:"service_session_id"`
	OrderDraftID     uuid.UUID `json:"order_draft_id"`
	CheckTarget      string    `json:"check_target"`
}

// SetCheckTargetHandler steers where the next Commit's charges land.
type SetCheckTargetHandler struct{ runner *Runner }

// NewSetCheckTargetHandler creates a new SetCheckTargetHandler.
func NewSetCheckTargetHandler(runner *Runner) *SetCheckTargetHandler {
	return &SetCheckTargetHandler{runner: runner}
}

// Handle writes the draft's Check target.
//
// It acquires the draft through the same lock the six 5A draft commands use,
// so it carries the same preconditions: ACTIVE Session, EDITABLE draft, OPEN
// Shift. Setting a target on a committed draft is not a thing that can happen.
func (h *SetCheckTargetHandler) Handle(ctx context.Context, actor Actor,
	cmd SetCheckTargetCommand,
) (int, ServiceSessionResponse, error) {
	if err := ValidateCheckTarget(cmd.CheckTarget); err != nil {
		return 0, ServiceSessionResponse{}, err
	}

	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpSetOrderDraftCheckTarget,
		Fingerprint: setCheckTargetFingerprint{
			ServiceSessionID: cmd.ServiceSessionID,
			CheckTarget:      cmd.CheckTarget,
		},
		Required: []string{CapSalesOperate},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, ServiceSessionResponse, AuditRecord, error) {
			var zero ServiceSessionResponse
			q := mc.Queries

			draft, err := lockEditableDraft(ctx, q, cmd.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}

			if err := q.SetOrderDraftCheckTarget(ctx, sqlc.SetOrderDraftCheckTargetParams{
				ID:          draft.OrderDraftID,
				CheckTarget: cmd.CheckTarget,
			}); err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("set order draft check target: %w", err)
			}

			result, err := LoadServiceSession(ctx, q, cmd.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}

			return 200, result, AuditRecord{
				EventType: EventOrderDraftCheckTargetSet,
				Details: checkTargetAudit{
					ServiceSessionID: cmd.ServiceSessionID,
					OrderDraftID:     draft.OrderDraftID,
					CheckTarget:      cmd.CheckTarget,
				},
			}, nil
		})
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test -tags integration ./internal/sales/ -run 'TestSetCheckTarget|TestCheckTarget' -v -p 1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
make fmt
git add internal/sales/draft_rounds.go internal/sales/draft_rounds_integration_test.go
git commit -m "feat(sales): add the set-check-target command"
```

---

## Task 12: HTTP Handlers, Routes And Swagger

**Files:**
- Modify: `internal/sales/http.go`, `internal/sales/routes.go`
- Test: `internal/sales/routes_test.go`

**Interfaces:**
- Consumes: Tasks 8, 10, 11 handlers.
- Produces: `Slices.CommitDraft`, `Slices.StartNewDraft`, `Slices.SetCheckTarget`; routes `POST /sales/service-sessions/:id/draft/commit`, `POST /sales/service-sessions/:id/draft`, `PUT /sales/service-sessions/:id/draft/check-target`.

- [ ] **Step 1: Write the failing route tests**

Append to `internal/sales/routes_test.go`, matching the table the existing route test already uses:

```go
func TestCommitRoutesRequireSalesOperate(t *testing.T) {
	routes := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/v1/sales/service-sessions/:id/draft/commit"},
		{http.MethodPost, "/api/v1/sales/service-sessions/:id/draft"},
		{http.MethodPut, "/api/v1/sales/service-sessions/:id/draft/check-target"},
	}
	registered := registeredSalesRoutes(t)
	for _, r := range routes {
		t.Run(r.method+" "+r.path, func(t *testing.T) {
			require.Contains(t, registered, r.method+" "+r.path)
		})
	}
}

func TestSalesExposesFourteenOperations(t *testing.T) {
	require.Len(t, registeredSalesRoutes(t), 14)
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/sales/ -run 'TestCommitRoutes|TestSalesExposesFourteen' -v`
Expected: FAIL — eleven routes are registered, not fourteen.

- [ ] **Step 3: Add the Echo handlers**

Append to `internal/sales/http.go`:

```go
// handleCommitDraft godoc
//
//	@Summary		Commit the Order Draft
//	@Description	Revalidates the draft, freezes prices into immutable Committed Items, and charges a Check. The response's checks[].payments and checks[].total_applied_vnd are filled by Phase 5C; allocations[].submitted is filled by Phase 5D.
//	@Tags			sales
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string						true	"Service Session ID"
//	@Param			body	body		CommitOrderDraftCommand		true	"Commit request"
//	@Success		200		{object}	response.Envelope{data=ServiceSessionResponse}
//	@Failure		401		{object}	response.Envelope
//	@Failure		403		{object}	response.Envelope
//	@Failure		409		{object}	response.Envelope
//	@Failure		422		{object}	response.Envelope
//	@Router			/sales/service-sessions/{id}/draft/commit [post]
func (s *Slices) handleCommitDraft(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return err
	}
	sessionID, err := parseUUIDParam(c, "id")
	if err != nil {
		return err
	}
	body, err := bindBody[CommitOrderDraftCommand](c)
	if err != nil {
		return err
	}
	if err := checkRequestID(body.RequestID); err != nil {
		return err
	}
	body.ServiceSessionID = sessionID

	status, result, err := s.CommitDraft.Handle(c.Request().Context(), actor, body)
	if err != nil {
		return err
	}
	return sendResult(c, status, result)
}

// handleStartNewDraft godoc
//
//	@Summary		Start a new Order Draft
//	@Description	Opens the Service Session's next Order Draft. Rejected while the Session holds an editable draft, or a committed draft with no Order — in Phase 5B the latter blocks every committed draft, because Submit arrives in 5D.
//	@Tags			sales
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string						true	"Service Session ID"
//	@Param			body	body		StartNewOrderDraftCommand	true	"Start request"
//	@Success		200		{object}	response.Envelope{data=ServiceSessionResponse}
//	@Failure		401		{object}	response.Envelope
//	@Failure		403		{object}	response.Envelope
//	@Failure		404		{object}	response.Envelope
//	@Failure		409		{object}	response.Envelope
//	@Router			/sales/service-sessions/{id}/draft [post]
func (s *Slices) handleStartNewDraft(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return err
	}
	sessionID, err := parseUUIDParam(c, "id")
	if err != nil {
		return err
	}
	body, err := bindBody[StartNewOrderDraftCommand](c)
	if err != nil {
		return err
	}
	if err := checkRequestID(body.RequestID); err != nil {
		return err
	}
	body.ServiceSessionID = sessionID

	status, result, err := s.StartNewDraft.Handle(c.Request().Context(), actor, body)
	if err != nil {
		return err
	}
	return sendResult(c, status, result)
}

// handleSetCheckTarget godoc
//
//	@Summary		Set the Order Draft's Check target
//	@Description	Steers where the next Commit's charges land. CURRENT_UNPAID reuses the Session's most recent open Check; NEW_CHECK always opens one. The target belongs to the draft and resets when a new draft opens.
//	@Tags			sales
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string					true	"Service Session ID"
//	@Param			body	body		SetCheckTargetCommand	true	"Check target request"
//	@Success		200		{object}	response.Envelope{data=ServiceSessionResponse}
//	@Failure		401		{object}	response.Envelope
//	@Failure		403		{object}	response.Envelope
//	@Failure		409		{object}	response.Envelope
//	@Failure		422		{object}	response.Envelope
//	@Router			/sales/service-sessions/{id}/draft/check-target [put]
func (s *Slices) handleSetCheckTarget(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return err
	}
	sessionID, err := parseUUIDParam(c, "id")
	if err != nil {
		return err
	}
	body, err := bindBody[SetCheckTargetCommand](c)
	if err != nil {
		return err
	}
	if err := checkRequestID(body.RequestID); err != nil {
		return err
	}
	body.ServiceSessionID = sessionID

	status, result, err := s.SetCheckTarget.Handle(c.Request().Context(), actor, body)
	if err != nil {
		return err
	}
	return sendResult(c, status, result)
}
```

- [ ] **Step 4: Wire the handlers and routes**

Add to the `Slices` struct in `internal/sales/routes.go`:

```go
	CommitDraft    *CommitOrderDraftHandler
	StartNewDraft  *StartNewOrderDraftHandler
	SetCheckTarget *SetCheckTargetHandler
```

Add to `NewSlices`:

```go
		CommitDraft:    NewCommitOrderDraftHandler(runner),
		StartNewDraft:  NewStartNewOrderDraftHandler(runner),
		SetCheckTarget: NewSetCheckTargetHandler(runner),
```

Add to `RegisterRoutes`:

```go
	v1.POST("/sales/service-sessions/:id/draft/commit", s.handleCommitDraft,
		authn.RequireAuth(), authn.RequireCapability(CapSalesOperate))
	v1.POST("/sales/service-sessions/:id/draft", s.handleStartNewDraft,
		authn.RequireAuth(), authn.RequireCapability(CapSalesOperate))
	v1.PUT("/sales/service-sessions/:id/draft/check-target", s.handleSetCheckTarget,
		authn.RequireAuth(), authn.RequireCapability(CapSalesOperate))
```

- [ ] **Step 5: Run the tests and regenerate Swagger**

Run: `go test ./internal/sales/ -run 'TestCommitRoutes|TestSalesExposesFourteen' -v && make swagger && go build ./...`
Expected: PASS; Swagger regenerates with fourteen Sales operations.

- [ ] **Step 6: Commit**

```bash
make fmt
git add internal/sales/http.go internal/sales/routes.go internal/sales/routes_test.go docs/
git commit -m "feat(sales): expose Commit, start-new-draft and set-check-target over HTTP"
```

---

## Task 13: Catalog Resolution Consistency

**Files:**
- Modify: `internal/sales/catalog_resolution_integration_test.go`

**Interfaces:**
- Consumes: Task 2's `ListEffectiveModifierGroupsForCommit`; the existing `ListEffectiveModifierGroupIDs`; `catalog.EffectiveGroupIDs`.

Task 2 introduced a second expression of the effective-group set algebra. ADR-012 exists to contain exactly that drift, so the existing consistency test must pin all three rather than two.

- [ ] **Step 1: Extend the consistency test**

Modify `TestSalesResolutionMatchesCatalog` in `internal/sales/catalog_resolution_integration_test.go` so each fixture Menu Item is checked three ways:

```go
	for _, itemID := range fixtureMenuItemIDs {
		t.Run(itemID.String(), func(t *testing.T) {
			fromCatalog := catalog.EffectiveGroupIDs(catalogInputsFor(t, itemID))

			perItem, err := queries.ListEffectiveModifierGroupIDs(ctx, itemID)
			require.NoError(t, err)

			batchRows, err := queries.ListEffectiveModifierGroupsForCommit(ctx,
				[]uuid.UUID{itemID})
			require.NoError(t, err)
			batched := make([]uuid.UUID, 0, len(batchRows))
			for _, row := range batchRows {
				batched = append(batched, row.ModifierGroupID)
			}

			require.ElementsMatch(t, fromCatalog, perItem,
				"the single-item query must match catalog.EffectiveGroupIDs")
			require.ElementsMatch(t, fromCatalog, batched,
				"the batched commit query must match catalog.EffectiveGroupIDs")
		})
	}
```

Confirm the fixture set still covers a Group both inherited and excluded, a Group both excluded and directly attached, an Item whose Category has no default Groups, and a retired Group. Add any that are missing.

- [ ] **Step 2: Add a batching-specific case**

Append a case proving the batched query does not leak groups across items:

```go
func TestBatchedResolutionDoesNotLeakGroupsAcrossItems(t *testing.T) {
	env := newSalesEnv(t)
	ctx := context.Background()

	rows, err := env.Queries.ListEffectiveModifierGroupsForCommit(ctx,
		[]uuid.UUID{env.CoffeeID, env.TeaID})
	require.NoError(t, err)

	byItem := make(map[uuid.UUID][]uuid.UUID)
	for _, row := range rows {
		byItem[row.MenuItemID] = append(byItem[row.MenuItemID], row.ModifierGroupID)
	}

	coffeeOnly, err := env.Queries.ListEffectiveModifierGroupIDs(ctx, env.CoffeeID)
	require.NoError(t, err)
	teaOnly, err := env.Queries.ListEffectiveModifierGroupIDs(ctx, env.TeaID)
	require.NoError(t, err)

	require.ElementsMatch(t, coffeeOnly, byItem[env.CoffeeID])
	require.ElementsMatch(t, teaOnly, byItem[env.TeaID])
}
```

- [ ] **Step 3: Run the tests to verify they pass**

Run: `go test -tags integration ./internal/sales/ -run 'TestSalesResolutionMatchesCatalog|TestBatchedResolution' -v -p 1`
Expected: PASS. A failure here means the batched query and the pure function disagree — fix the query, never the assertion.

- [ ] **Step 4: Commit**

```bash
make fmt
git add internal/sales/catalog_resolution_integration_test.go
git commit -m "test(sales): pin the batched commit resolution to catalog.EffectiveGroupIDs"
```

---

## Task 14: Concurrency And Idempotency

**Files:**
- Create: `internal/sales/commit_concurrency_integration_test.go`

**Interfaces:**
- Consumes: every handler from Tasks 8–11.

- [ ] **Step 1: Write the concurrency tests**

Create `internal/sales/commit_concurrency_integration_test.go`:

```go
func TestConcurrentCommitsOfOneDraftCommitOnce(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartTakeaway(t)
	env.AddDraftItem(t, session.ID, env.CoffeeID, nil)

	start := make(chan struct{})
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			_, err := env.TryCommitWithRequestID(t, session.ID, uuid.New())
			results <- err
		}()
	}
	close(start)

	var succeeded, rejected int
	for i := 0; i < 2; i++ {
		if err := <-results; err == nil {
			succeeded++
		} else {
			require.ErrorIs(t, err, sales.ErrEditableDraftNotFound)
			rejected++
		}
	}
	require.Equal(t, 1, succeeded)
	require.Equal(t, 1, rejected)
	env.RequireCheckCount(t, session.ID, 1)
}

func TestConcurrentDuplicateCommitRequestsExecuteOnce(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartTakeaway(t)
	env.AddDraftItem(t, session.ID, env.CoffeeID, nil)
	requestID := uuid.New()

	start := make(chan struct{})
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			_, err := env.TryCommitWithRequestID(t, session.ID, requestID)
			results <- err
		}()
	}
	close(start)
	for i := 0; i < 2; i++ {
		require.NoError(t, <-results)
	}

	env.RequireCheckCount(t, session.ID, 1)
	env.RequireCommittedItemCount(t, session.ID, 1)
}

// FOR SHARE on Catalog rows means two Sessions committing orders that share a
// popular menu item proceed in parallel rather than serializing. See ADR-015.
func TestConcurrentCommitsSharingAMenuItemBothSucceed(t *testing.T) {
	env := newSalesEnv(t)
	first := env.StartTakeaway(t)
	second := env.StartTakeaway(t)
	env.AddDraftItem(t, first.ID, env.CoffeeID, nil)
	env.AddDraftItem(t, second.ID, env.CoffeeID, nil)

	start := make(chan struct{})
	results := make(chan error, 2)
	for _, sessionID := range []uuid.UUID{first.ID, second.ID} {
		go func(id uuid.UUID) {
			<-start
			_, err := env.TryCommit(t, id)
			results <- err
		}(sessionID)
	}
	close(start)
	for i := 0; i < 2; i++ {
		require.NoError(t, <-results)
	}

	env.RequireCheckCount(t, first.ID, 1)
	env.RequireCheckCount(t, second.ID, 1)
}

func TestCommitReplayConflictsOnADifferentPayload(t *testing.T) {
	env := newSalesEnv(t)
	first := env.StartTakeaway(t)
	second := env.StartTakeaway(t)
	env.AddDraftItem(t, first.ID, env.CoffeeID, nil)
	env.AddDraftItem(t, second.ID, env.CoffeeID, nil)
	requestID := uuid.New()

	env.CommitWithRequestID(t, first.ID, requestID)
	_, err := env.TryCommitWithRequestID(t, second.ID, requestID)

	require.ErrorIs(t, err, sales.ErrRequestConflict)
}

func TestCommitReplayDeniedAfterIdentityDisabled(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartTakeaway(t)
	env.AddDraftItem(t, session.ID, env.CoffeeID, nil)
	requestID := uuid.New()
	env.CommitWithRequestID(t, session.ID, requestID)

	env.DisableActorIdentity(t)

	_, err := env.TryCommitWithRequestID(t, session.ID, requestID)
	require.ErrorIs(t, err, sales.ErrForbidden)
}

// No 5B code path writes SETTLED or MERGED.
func TestNoCheckLeavesTheOpenState(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartTakeaway(t)
	env.AddDraftItem(t, session.ID, env.CoffeeID, nil)
	env.Commit(t, session.ID)

	var n int
	require.NoError(t, env.DB.QueryRow(
		`SELECT count(*) FROM checks WHERE state <> 'OPEN'`).Scan(&n))
	require.Zero(t, n)
}
```

- [ ] **Step 2: Run the tests**

Run: `go test -tags integration ./internal/sales/ -run 'TestConcurrent|TestCommitReplay|TestNoCheckLeaves' -v -p 1 -race`
Expected: PASS. A deadlock here means the lock order of Task 8 was not followed — fix the order rather than adding a retry.

- [ ] **Step 3: Run the whole suite**

Run: `make test && go test -tags integration ./... -p 1`
Expected: every Auth, Catalog, Tables, Shift, and 5A Sales suite still passes.

- [ ] **Step 4: Commit**

```bash
make fmt
git add internal/sales/commit_concurrency_integration_test.go
git commit -m "test(sales): cover Commit concurrency, idempotency and the OPEN-only invariant"
```

---

## Task 15: Decision Records And Roadmap

**Files:**
- Modify: `spec/decisions.md`, `MIGRATE_PLAN.md`

- [ ] **Step 1: Append the three ADRs**

Append to `spec/decisions.md`, following the existing format. Copy ADR-013, ADR-014, and ADR-015 verbatim from §14 of the spec — they are already written there in full and must not be paraphrased into a second, diverging version.

- [ ] **Step 2: Update the Phase 5 sub-phase table**

In `MIGRATE_PLAN.md`, change the 5B row to:

```markdown
| **5B** — Commit, Committed Items, Checks, Charge Allocations, Order Draft targeting | [spec](docs/superpowers/specs/2026-09-14-sales-commit-checks-design.md) / [plan](docs/superpowers/plans/2026-09-14-sales-commit-checks.md) | ✅ COMPLETED (2026-09-14) |
```

Leave the Phase 5 detail and the tracker row untouched: the tracker row stays `⏳ PENDING` until 5D lands.

- [ ] **Step 3: Verify the links resolve**

Run: `ls docs/superpowers/specs/2026-09-14-sales-commit-checks-design.md docs/superpowers/plans/2026-09-14-sales-commit-checks.md`
Expected: both paths exist.

- [ ] **Step 4: Commit**

```bash
git add spec/decisions.md MIGRATE_PLAN.md
git commit -m "docs: record ADR-013..015 and mark Phase 5B complete in the roadmap"
```

---

## Self-Review

**Spec coverage.** Every spec section maps to a task: §5.1–5.5 → Task 1; §7 → Tasks 2 and 13; §6.3 → Task 3; §11 → Task 4; §9.1 → Tasks 5 and 6; §5.6 → Task 6; §6.2 → Task 7; §6.1, §6.5, §10 → Task 8; §6.4 → Task 9; §6.6 → Task 10; §6.7 → Task 11; §9 → Task 12; §12 audit events → Tasks 8, 10, 11; §13 → Tasks 7–14; §14 → Task 15.

**Known cross-task dependency.** Task 8's handler calls `resolveTargetCheck`, which Task 9 implements. Execute Tasks 8 and 9 in order; Task 8's Step 5 notes this explicitly. Similarly, Task 6's invariant test consumes Task 8's fixture helper and is enabled in Task 8 Step 6, and Task 3's tests compile against Task 4's sentinels.

**Type consistency.** `CommitCandidate`, `EffectiveGroup`, `SelectedOption`, `CommitSize`, and `ItemSnapshot` are exported from Task 7 and consumed unchanged in Task 8. `CheckResponse`, `ChargeAllocationResponse`, and `CommittedModifierResponse` are defined in Task 5 and consumed in Tasks 6 and 7. `LineTotal` and `AddCharge` are defined in Task 3 and consumed in Tasks 6 and 7. Handler constructors follow `New<Name>Handler(runner *Runner)` throughout.

**Naming note for the executor.** sqlc lower-cases `VND` to `Vnd` in generated field names (`ChargeVnd`, `UnitPriceVnd`, `SurchargeVnd`) while the hand-written DTOs use `VND` (`ChargeVND`). Both spellings appear in this plan deliberately; do not "fix" one to match the other.
