# Sales Session & Order Draft (Phase 5A) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `internal/sales` up to, but not including, its commercial boundary — Service Session lifecycle, Table assignments, and the Order Draft — as the first of four Phase 5 sub-phases.

**Architecture:** One vertical slice package organized by behavior, following `internal/shift` exactly: a `Runner` owning transaction orchestration, per-operation handler types, a `Slices` aggregate, and `RegisterRoutes(v1, authn)`. Every mutation runs in one transaction that reloads authority, claims an actor-scoped idempotency key, locks its rows, mutates, audits, and stores a replayable result. The slice reads Catalog, Shift, and Tables data through its own sqlc queries and imports no slice but `internal/auth`.

**Tech Stack:** Go 1.26+, Echo v4, PostgreSQL via `jackc/pgx/v5` (stdlib `database/sql` driver), sqlc, `google/uuid`, `stretchr/testify`, swag/OpenAPI 2.0.

**Spec:** [`docs/superpowers/specs/2026-09-13-sales-session-draft-design.md`](../specs/2026-09-13-sales-session-draft-design.md)

## Global Constraints

Every task's requirements implicitly include this section.

- **Package boundary.** `internal/sales` imports `internal/auth`, `internal/database/sqlc`, `internal/response`, `internal/httpvalidator`. It MUST NOT import `internal/catalog`, `internal/tables`, or `internal/shift`. Test files may import them.
- **Capability.** Every operation requires `sales.operate`. It already exists in `auth.DeriveCapabilities` for `MANAGER` and `CASHIER`. Do not modify the capability table.
- **Idempotency.** Use the shared `idempotency_keys` table (ADR-005, ADR-007). Do not create a new idempotency table. Action names are fully qualified and fit `VARCHAR(50)`.
- **Audit.** Use the shared `audit_events` table. Business events are `UPPER_SNAKE_CASE`; the denial event is `sales.authorization_denied`.
- **Money.** No monetary column is written in 5A. `price_vnd` appears in the projection only, read from Catalog.
- **Quantity bound.** 1 to 9,999 inclusive, enforced in Go and by a database `CHECK`.
- **Preparation Note bound.** Trimmed, 1 to 200 Unicode code points, or `NULL`.
- **Service Number format.** `S` plus five zero-padded digits (`S00001`), unique within a Sales Shift, allocated under a transaction-scoped advisory lock.
- **Session state domain.** `('ACTIVE', 'CLOSED')`. 5A writes only `ACTIVE`.
- **Empty collections** serialize as `[]`, never `null`. `sqlc.yaml` already sets `emit_empty_slices: true`.
- **Integration tests** carry `//go:build integration`, live in package `sales_test`, and run with `-p 1`.
- **Every task ends with a commit.** Run `make fmt` before committing.

---

## File Structure

**New files in `internal/sales/`:**

| File | Responsibility |
| --- | --- |
| `domain.go` | Capability, operation names, audit event names, state constants, bounds, and pure validation and formatting functions |
| `domain_test.go` | Unit tests for the pure functions |
| `errors.go` | Domain error sentinels, `MapDBError`, `MapHTTPError` |
| `errors_test.go` | Unit tests for error mapping |
| `executor.go` | `Runner`, `Actor`, `MutationSpec`, `MutationContext`, `AuditRecord`, `ExecuteMutation`, `ExecuteRead`, authority reload, denial auditing |
| `dto.go` | Command structs and response structs |
| `dto_test.go` | Unit tests for JSON serialization shape |
| `projection.go` | Assembling `ServiceSessionResponse` from query rows |
| `service_number.go` | Shift-scoped Service Number allocation |
| `catalog_resolution.go` | Effective Modifier Group resolution, default options, option validation |
| `session_start.go` | Open Takeaway and Open Dine-in handlers |
| `table_assignments.go` | Set Service Session Tables handler |
| `draft_add.go` | Add draft item handler |
| `draft_edit.go` | Quantity, Size, Note, Modifier handlers and the composition merge helper |
| `draft_remove.go` | Remove draft item handler |
| `reads.go` | Get Service Session and List active Service Sessions handlers |
| `http.go` | Echo handlers, binding, validation helpers, Swagger annotations |
| `routes.go` | `Slices` aggregate and `RegisterRoutes` |

**New files elsewhere:**

| File | Responsibility |
| --- | --- |
| `internal/database/migrations/000008_create_sales_draft_slice.sql` | Schema |
| `sql/queries/sales.sql` | Every sqlc query the slice uses |
| `internal/sales/*_integration_test.go` | Integration suites, one per behavior area |

**Modified files:**

| File | Change |
| --- | --- |
| `cmd/api/main.go` | Wire `sales.NewSlices(...).RegisterRoutes(v1, authSlices.Middleware)` |
| `spec/decisions.md` | Append ADR-010, ADR-011, ADR-012 |
| `MIGRATE_PLAN.md` | Add sub-phase spec links under Phase 5 |

---

## Task 1: Database Schema

**Files:**
- Create: `internal/database/migrations/000008_create_sales_draft_slice.sql`
- Test: `internal/sales/schema_integration_test.go`

**Interfaces:**
- Consumes: `service_sessions` and `table_assignments` from migration `000006`; `sales_shifts` from `000007`; `menu_items`, `menu_item_sizes`, `modifier_options` from `000003`.
- Produces: tables `order_drafts`, `order_draft_items`, `order_draft_item_modifier_options`; columns `service_sessions.sales_shift_id` and `service_sessions.sequence`; indexes `service_session_number_per_shift_unique`, `service_session_sequence_per_shift_unique`, `order_draft_editable_per_session_unique`, `order_draft_item_composition_unique`.

- [ ] **Step 1: Write the migration**

Create `internal/database/migrations/000008_create_sales_draft_slice.sql`:

```sql
-- Phase 5A: Service Session & Order Draft.
--
-- internal/sales takes business ownership of service_sessions and
-- table_assignments, which migration 000006 provisioned early so the Tables
-- overview read could ship complete in Phase 3 (ADR-006). This migration
-- completes service_sessions, corrects a state domain Phase 3 guessed, and
-- creates the Order Draft tables.

-- service_sessions gains a NOT NULL column with no default, which is only
-- safe on an empty table. Phase 3 never writes the table through its API, so
-- in any real deployment it is empty. Fail loudly rather than backfilling a
-- fabricated Sales Shift.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM service_sessions) THEN
        RAISE EXCEPTION
            'service_sessions must be empty before Phase 5A adds sales_shift_id NOT NULL';
    END IF;
END $$;

ALTER TABLE service_sessions
    ADD COLUMN IF NOT EXISTS sales_shift_id UUID NOT NULL
        REFERENCES sales_shifts(id) ON DELETE RESTRICT,
    ADD COLUMN IF NOT EXISTS sequence INTEGER NOT NULL;

-- The canonical SERVICE_SESSION_STATES is ['ACTIVE', 'CLOSED']. Phase 3
-- guessed ('ACTIVE', 'COMPLETED', 'CANCELLED') for a domain it never wrote:
-- COMPLETED belongs to the separate Completed Sale entity, and no canonical
-- path cancels a Service Session.
ALTER TABLE service_sessions DROP CONSTRAINT IF EXISTS service_session_state_valid;
ALTER TABLE service_sessions ADD CONSTRAINT service_session_state_valid
    CHECK (state IN ('ACTIVE', 'CLOSED'));

ALTER TABLE service_sessions ADD CONSTRAINT service_session_sequence_valid
    CHECK (sequence BETWEEN 1 AND 99999);

-- A Service Number is an operational label, unique among the Sessions of one
-- Sales Shift rather than for all time. See ADR-011.
DROP INDEX IF EXISTS service_session_service_number_unique;

CREATE UNIQUE INDEX IF NOT EXISTS service_session_number_per_shift_unique
    ON service_sessions (sales_shift_id, service_number);

CREATE UNIQUE INDEX IF NOT EXISTS service_session_sequence_per_shift_unique
    ON service_sessions (sales_shift_id, sequence);

CREATE INDEX IF NOT EXISTS service_session_active_index
    ON service_sessions (state, created_at ASC, id ASC);

COMMENT ON TABLE service_sessions IS
    'Owned by internal/sales (Phase 5A).';
COMMENT ON TABLE table_assignments IS
    'Owned by internal/sales (Phase 5A). Read by internal/tables for the overview.';

-- One editable Order Draft per Service Session.
CREATE TABLE IF NOT EXISTS order_drafts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    service_session_id UUID NOT NULL REFERENCES service_sessions(id) ON DELETE RESTRICT,
    state TEXT NOT NULL DEFAULT 'EDITABLE',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT order_draft_state_valid
        CHECK (state IN ('EDITABLE', 'COMMITTED'))
);

-- 5A never writes COMMITTED. The state exists so the draft lock query's
-- state = 'EDITABLE' filter is meaningful rather than vacuous, and so 5B adds
-- Commit without a state-domain migration.
CREATE UNIQUE INDEX IF NOT EXISTS order_draft_editable_per_session_unique
    ON order_drafts (service_session_id)
    WHERE state = 'EDITABLE';

CREATE INDEX IF NOT EXISTS order_draft_service_session_index
    ON order_drafts (service_session_id);

COMMENT ON TABLE order_drafts IS
    'Owned by internal/sales (Phase 5A). Commit, which writes COMMITTED, lands in 5B.';

CREATE TABLE IF NOT EXISTS order_draft_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_draft_id UUID NOT NULL REFERENCES order_drafts(id) ON DELETE CASCADE,
    menu_item_id UUID NOT NULL REFERENCES menu_items(id) ON DELETE RESTRICT,
    size_id UUID REFERENCES menu_item_sizes(id) ON DELETE RESTRICT,
    quantity INTEGER NOT NULL DEFAULT 1,
    preparation_note TEXT,
    modifier_key TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- A plain unique index treats two NULLs as distinct, which would let the
    -- same composition exist twice whenever Size or note is absent. These
    -- generated columns make the composition key total.
    size_key TEXT GENERATED ALWAYS AS (coalesce(size_id::text, '')) STORED,
    note_key TEXT GENERATED ALWAYS AS (coalesce(preparation_note, '')) STORED,
    CONSTRAINT order_draft_item_quantity_valid
        CHECK (quantity BETWEEN 1 AND 9999),
    CONSTRAINT order_draft_item_note_valid
        CHECK (
            preparation_note IS NULL
            OR (char_length(preparation_note) BETWEEN 1 AND 200
                AND preparation_note = btrim(preparation_note))
        )
);

CREATE UNIQUE INDEX IF NOT EXISTS order_draft_item_composition_unique
    ON order_draft_items (order_draft_id, menu_item_id, size_key, note_key, modifier_key);

CREATE INDEX IF NOT EXISTS order_draft_item_draft_index
    ON order_draft_items (order_draft_id, created_at ASC, id ASC);

COMMENT ON COLUMN order_draft_items.modifier_key IS
    'Derived: sorted, comma-joined modifier_option_id list. Exists only to make the composition index possible; order_draft_item_modifier_options is authoritative.';

CREATE TABLE IF NOT EXISTS order_draft_item_modifier_options (
    order_draft_item_id UUID NOT NULL
        REFERENCES order_draft_items(id) ON DELETE CASCADE,
    modifier_option_id UUID NOT NULL
        REFERENCES modifier_options(id) ON DELETE RESTRICT,
    PRIMARY KEY (order_draft_item_id, modifier_option_id)
);
```

- [ ] **Step 2: Write the failing schema test**

Create `internal/sales/schema_integration_test.go`:

```go
//go:build integration

package sales_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestServiceSessionStateDomain proves Phase 3's guessed state domain is gone.
func TestServiceSessionStateDomain(t *testing.T) {
	db, _ := openSalesTestDB(t)
	ctx := context.Background()

	var clause string
	err := db.QueryRowContext(ctx, `
		SELECT pg_get_constraintdef(oid)
		FROM pg_constraint
		WHERE conname = 'service_session_state_valid'`).Scan(&clause)
	require.NoError(t, err)

	assert.Contains(t, clause, "ACTIVE")
	assert.Contains(t, clause, "CLOSED")
	assert.NotContains(t, clause, "COMPLETED")
	assert.NotContains(t, clause, "CANCELLED")
}

// TestServiceNumberIsShiftScoped proves the global unique index is gone and
// the Shift-scoped one replaced it (ADR-011).
func TestServiceNumberIsShiftScoped(t *testing.T) {
	db, _ := openSalesTestDB(t)
	ctx := context.Background()

	var globalCount int
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT count(*) FROM pg_indexes
		WHERE indexname = 'service_session_service_number_unique'`).Scan(&globalCount))
	assert.Equal(t, 0, globalCount, "the global Service Number index must be dropped")

	var def string
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT indexdef FROM pg_indexes
		WHERE indexname = 'service_session_number_per_shift_unique'`).Scan(&def))
	assert.Contains(t, def, "sales_shift_id")
	assert.Contains(t, def, "service_number")
}

// TestCompositionIndexTreatsNullsAsEqual is the whole reason size_key and
// note_key exist: a plain unique index would allow this duplicate.
func TestCompositionIndexTreatsNullsAsEqual(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	ctx := context.Background()

	fx := seedSalesFixture(t, q)
	draftID := fx.DraftID

	_, err := db.ExecContext(ctx, `
		INSERT INTO order_draft_items (order_draft_id, menu_item_id, size_id, preparation_note, modifier_key)
		VALUES ($1, $2, NULL, NULL, '')`, draftID, fx.MenuItemID)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `
		INSERT INTO order_draft_items (order_draft_id, menu_item_id, size_id, preparation_note, modifier_key)
		VALUES ($1, $2, NULL, NULL, '')`, draftID, fx.MenuItemID)
	require.Error(t, err, "a second NULL-Size, NULL-note row of the same composition must violate the unique index")
}

// TestQuantityBound proves the 9,999 ceiling from the spec is enforced by the
// database, not only by Go.
func TestQuantityBound(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	ctx := context.Background()

	fx := seedSalesFixture(t, q)

	_, err := db.ExecContext(ctx, `
		INSERT INTO order_draft_items (order_draft_id, menu_item_id, quantity, modifier_key)
		VALUES ($1, $2, 10000, '')`, fx.DraftID, fx.MenuItemID)
	require.Error(t, err)

	_, err = db.ExecContext(ctx, `
		INSERT INTO order_draft_items (order_draft_id, menu_item_id, quantity, modifier_key)
		VALUES ($1, $2, 0, '')`, fx.DraftID, fx.MenuItemID)
	require.Error(t, err)

	_, err = db.ExecContext(ctx, `
		INSERT INTO order_draft_items (order_draft_id, menu_item_id, quantity, modifier_key)
		VALUES ($1, $2, 9999, '')`, fx.DraftID, fx.MenuItemID)
	require.NoError(t, err)
}

// TestOneEditableDraftPerSession proves the partial unique index.
func TestOneEditableDraftPerSession(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	ctx := context.Background()

	fx := seedSalesFixture(t, q)

	_, err := db.ExecContext(ctx, `
		INSERT INTO order_drafts (service_session_id) VALUES ($1)`, fx.ServiceSessionID)
	require.Error(t, err, "a second EDITABLE draft for one Session must be rejected")

	// A COMMITTED draft alongside an EDITABLE one is allowed; the index is partial.
	_, err = db.ExecContext(ctx, `
		INSERT INTO order_drafts (service_session_id, state) VALUES ($1, 'COMMITTED')`,
		fx.ServiceSessionID)
	require.NoError(t, err)
}
```

- [ ] **Step 3: Write the test harness**

Create `internal/sales/testmain_integration_test.go`. This is shared by every integration suite in the package.

```go
//go:build integration

package sales_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync/atomic"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/database"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

var loginCodeCounter atomic.Int64

// testLoginCode returns a login code unique within this test binary.
func testLoginCode(prefix string) string {
	return fmt.Sprintf("%s%d", prefix, loginCodeCounter.Add(1))
}

// openSalesTestDB connects to the integration database and runs migrations.
// Mirrors openShiftTestDB in internal/shift.
func openSalesTestDB(t *testing.T) (*sql.DB, *sqlc.Queries) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	db, err := database.Open(dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, database.Migrate(db))
	return db, sqlc.New(db)
}

// truncateSalesTables clears every table this slice writes, plus the shared
// tables its fixtures seed. Order matters only for readability; CASCADE does
// the work.
func truncateSalesTables(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`
		TRUNCATE order_draft_item_modifier_options, order_draft_items, order_drafts,
		         table_assignments, service_sessions, cash_movements, sales_shifts,
		         tables, modifier_group_default_options, item_modifier_group_exclusions,
		         item_modifier_groups, category_modifier_groups, modifier_options,
		         modifier_groups, menu_item_sizes, menu_items, menu_categories,
		         idempotency_keys, audit_events, staff_access_sessions,
		         staff_operational_roles, staff_identities
		RESTART IDENTITY CASCADE`)
	require.NoError(t, err)
}

// salesFixture is the minimal world a Sales test needs: an open Shift, a
// Service Session with its draft, and one priced Menu Item.
type salesFixture struct {
	StaffID          uuid.UUID
	SalesShiftID     uuid.UUID
	ServiceSessionID uuid.UUID
	DraftID          uuid.UUID
	MenuCategoryID   uuid.UUID
	MenuItemID       uuid.UUID
}

// seedSalesFixture inserts the fixture with raw SQL rather than through the
// Sales API, because Task 1 has no API yet and later tasks still need a world
// that exists before the command under test runs.
func seedSalesFixture(t *testing.T, q *sqlc.Queries) salesFixture {
	t.Helper()
	ctx := context.Background()
	var fx salesFixture

	staff, err := q.CreateStaffIdentity(ctx, sqlc.CreateStaffIdentityParams{
		DisplayName: "Fixture " + testLoginCode("F"),
		Btrim:       testLoginCode("F"),
		PinHash:     "$2a$10$abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKLMNOPQR",
		Enabled:     true,
	})
	require.NoError(t, err)
	fx.StaffID = staff.ID

	shift, err := q.OpenSalesShift(ctx, sqlc.OpenSalesShiftParams{
		OpenedByStaffIdentityID: fx.StaffID,
		OpeningFloatVnd:         100000,
	})
	require.NoError(t, err)
	fx.SalesShiftID = shift.ID

	cat, err := q.CreateCategory(ctx, sqlc.CreateCategoryParams{
		Name:           "Cà phê",
		NormalizedName: "cà phê",
	})
	require.NoError(t, err)
	fx.MenuCategoryID = cat.ID

	item, err := q.CreateMenuItem(ctx, sqlc.CreateMenuItemParams{
		MenuCategoryID: fx.MenuCategoryID,
		Name:           "Cà phê sữa",
		NormalizedName: "cà phê sữa",
		PriceVnd:       sql.NullInt64{Int64: 25000, Valid: true},
		Available:      true,
	})
	require.NoError(t, err)
	fx.MenuItemID = item.ID

	// service_sessions and order_drafts have no sqlc writer until Task 6, so
	// the fixture inserts them directly.
	require.NoError(t, q.DB().QueryRowContext(ctx, `
		INSERT INTO service_sessions
		    (service_number, sequence, service_mode, state, created_by_staff_identity_id, sales_shift_id)
		VALUES ('S00001', 1, 'TAKEAWAY', 'ACTIVE', $1, $2)
		RETURNING id`, fx.StaffID, fx.SalesShiftID).Scan(&fx.ServiceSessionID))

	require.NoError(t, q.DB().QueryRowContext(ctx, `
		INSERT INTO order_drafts (service_session_id) VALUES ($1) RETURNING id`,
		fx.ServiceSessionID).Scan(&fx.DraftID))

	return fx
}
```

> **Note for the implementer:** `sqlc.Queries` does not expose the underlying
> `*sql.DB`. If `q.DB()` does not exist in this repo's generated code, change
> `seedSalesFixture` to take `(t *testing.T, db *sql.DB, q *sqlc.Queries)` and
> use `db.QueryRowContext` directly. Check `internal/database/sqlc/db.go`
> before writing the file and pick whichever matches. Do the same for
> `q.CreateCategory` and `q.CreateMenuItem`: confirm the exact generated
> parameter struct names in `internal/database/sqlc/catalog.sql.go` and adjust
> the field names to match. Do not guess.

- [ ] **Step 4: Run the tests to verify they fail**

Run: `make docker-up && make db-wait`
Run: `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -p 1 -tags=integration ./internal/sales/... -run 'TestServiceSession|TestComposition|TestQuantity|TestOneEditable' -v`

Expected: FAIL — the migration has not been applied yet on a pre-existing test database, or the package does not compile.

- [ ] **Step 5: Run the tests to verify they pass**

The migration runs automatically through `database.Migrate(db)` in the harness.

Run: `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -p 1 -tags=integration ./internal/sales/... -v`

Expected: PASS, all five tests.

If the test database already holds `service_sessions` rows from an earlier Phase 3 run, the migration's guard raises. Drop and recreate the test database: `docker compose exec -T postgres psql -U cafe_pos -c 'DROP DATABASE cafe_pos_test' -c 'CREATE DATABASE cafe_pos_test'`, then rerun.

- [ ] **Step 6: Commit**

```bash
make fmt
git add internal/database/migrations/000008_create_sales_draft_slice.sql internal/sales/
git commit -m "feat(sales): add Phase 5A schema for Service Session and Order Draft

Takes ownership of service_sessions and table_assignments, adds
sales_shift_id and sequence, corrects the state domain Phase 3 guessed to
('ACTIVE','CLOSED'), replaces the global Service Number index with a
Shift-scoped one (ADR-011), and creates the Order Draft tables.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

## Task 2: Domain Primitives

Pure functions with no database and no Echo. Everything here is unit-testable.

**Files:**
- Create: `internal/sales/domain.go`
- Test: `internal/sales/domain_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `const CapSalesOperate = "sales.operate"`
  - Operation names `OpStartTakeawaySession`, `OpStartDineInSession`, `OpSetSessionTables`, `OpAddDraftItem`, `OpSetDraftItemQuantity`, `OpSetDraftItemSize`, `OpSetDraftItemNote`, `OpSetDraftItemModifiers`, `OpRemoveDraftItem`, `OpGetServiceSession`, `OpListServiceSessions`
  - Audit event names `EventServiceSessionStarted`, `EventDineInServiceSessionStarted`, `EventTableAssignmentCreated`, `EventTableAssignmentReleased`, `EventDraftItemAdded`, `EventDraftItemQuantitySet`, `EventDraftItemSizeSet`, `EventDraftItemNoteSet`, `EventDraftItemModifiersSet`, `EventDraftItemRemoved`, `EventDraftItemsMerged`, `EventAuthorizationDenied`
  - `const StateActive, StateClosed, DraftStateEditable, DraftStateCommitted, ModeTakeaway, ModeDineIn string`
  - `const MinQuantity, MaxQuantity int32`, `MaxPreparationNoteLength int`, `MaxServiceSequence int32`
  - `func NormalizePreparationNote(v *string) (sql.NullString, error)`
  - `func ValidateQuantity(q int32) error`
  - `func ModifierKeyFor(ids []uuid.UUID) string`
  - `func SortedUUIDs(ids []uuid.UUID) []uuid.UUID`
  - `func HasDuplicateUUIDs(ids []uuid.UUID) bool`
  - `func FormatServiceNumber(seq int32) (string, error)`

- [ ] **Step 1: Write the failing tests**

Create `internal/sales/domain_test.go`:

```go
package sales

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func strPtr(s string) *string { return &s }

func TestNormalizePreparationNote(t *testing.T) {
	t.Run("nil stays null", func(t *testing.T) {
		got, err := NormalizePreparationNote(nil)
		require.NoError(t, err)
		assert.False(t, got.Valid)
	})

	t.Run("whitespace only becomes null", func(t *testing.T) {
		got, err := NormalizePreparationNote(strPtr("   \t\n "))
		require.NoError(t, err)
		assert.False(t, got.Valid)
	})

	t.Run("surrounding whitespace is trimmed", func(t *testing.T) {
		got, err := NormalizePreparationNote(strPtr("  ít đá  "))
		require.NoError(t, err)
		require.True(t, got.Valid)
		assert.Equal(t, "ít đá", got.String)
	})

	t.Run("200 code points is accepted", func(t *testing.T) {
		got, err := NormalizePreparationNote(strPtr(strings.Repeat("á", 200)))
		require.NoError(t, err)
		assert.True(t, got.Valid)
	})

	t.Run("201 code points is rejected", func(t *testing.T) {
		_, err := NormalizePreparationNote(strPtr(strings.Repeat("á", 201)))
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidPreparationNote)
	})

	t.Run("length is counted in code points not bytes", func(t *testing.T) {
		// 100 three-byte characters is 300 bytes but only 100 code points, so
		// a byte-based check would wrongly reject it.
		got, err := NormalizePreparationNote(strPtr(strings.Repeat("ế", 100)))
		require.NoError(t, err)
		assert.True(t, got.Valid)
	})
}

func TestValidateQuantity(t *testing.T) {
	assert.Error(t, ValidateQuantity(0))
	assert.NoError(t, ValidateQuantity(1))
	assert.NoError(t, ValidateQuantity(9999))
	assert.Error(t, ValidateQuantity(10000))
	assert.Error(t, ValidateQuantity(-1))
}

func TestModifierKeyFor(t *testing.T) {
	a := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	b := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	t.Run("empty set is the empty string", func(t *testing.T) {
		assert.Equal(t, "", ModifierKeyFor(nil))
		assert.Equal(t, "", ModifierKeyFor([]uuid.UUID{}))
	})

	t.Run("input order does not change the key", func(t *testing.T) {
		assert.Equal(t, ModifierKeyFor([]uuid.UUID{a, b}), ModifierKeyFor([]uuid.UUID{b, a}))
	})

	t.Run("key is sorted and comma joined", func(t *testing.T) {
		assert.Equal(t, a.String()+","+b.String(), ModifierKeyFor([]uuid.UUID{b, a}))
	})
}

func TestHasDuplicateUUIDs(t *testing.T) {
	a := uuid.New()
	b := uuid.New()
	assert.False(t, HasDuplicateUUIDs([]uuid.UUID{a, b}))
	assert.True(t, HasDuplicateUUIDs([]uuid.UUID{a, b, a}))
	assert.False(t, HasDuplicateUUIDs(nil))
}

func TestFormatServiceNumber(t *testing.T) {
	t.Run("pads to six characters", func(t *testing.T) {
		got, err := FormatServiceNumber(1)
		require.NoError(t, err)
		assert.Equal(t, "S00001", got)
	})

	t.Run("upper bound", func(t *testing.T) {
		got, err := FormatServiceNumber(99999)
		require.NoError(t, err)
		assert.Equal(t, "S99999", got)
	})

	t.Run("overflow is an error not a longer string", func(t *testing.T) {
		// The database CHECK is ^[A-Z0-9]{6}$, so a seventh character would
		// fail at insert time with an unmapped 23514 instead of here.
		_, err := FormatServiceNumber(100000)
		require.Error(t, err)
	})

	t.Run("zero and negative are errors", func(t *testing.T) {
		_, err := FormatServiceNumber(0)
		require.Error(t, err)
		_, err = FormatServiceNumber(-1)
		require.Error(t, err)
	})

	t.Run("every formatted value matches the database pattern", func(t *testing.T) {
		for _, seq := range []int32{1, 9, 10, 999, 1000, 99999} {
			got, err := FormatServiceNumber(seq)
			require.NoError(t, err)
			assert.Len(t, got, 6)
			assert.Regexp(t, `^[A-Z0-9]{6}$`, got)
		}
	})
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/sales/ -run 'TestNormalize|TestValidateQuantity|TestModifierKey|TestHasDuplicate|TestFormatService' -v`
Expected: FAIL — `undefined: NormalizePreparationNote` and the rest.

- [ ] **Step 3: Write the implementation**

Create `internal/sales/domain.go`:

```go
// Package sales implements the Sales vertical slice. Phase 5A covers the
// Service Session lifecycle up to its commercial boundary: opening a Takeaway
// or Dine-in Session, maintaining Table assignments, and building the Order
// Draft. Commit, Payment, Submit, and closure land in 5B, 5C, and 5D.
package sales

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

// CapSalesOperate is the capability every Sales operation requires. It is
// already derived for MANAGER and CASHIER by auth.DeriveCapabilities.
const CapSalesOperate = "sales.operate"

// Idempotency action names, stored in idempotency_keys.action (VARCHAR(50)).
const (
	OpStartTakeawaySession  = "sales.start_takeaway_session"
	OpStartDineInSession    = "sales.start_dine_in_session"
	OpSetSessionTables      = "sales.set_session_tables"
	OpAddDraftItem          = "sales.add_draft_item"
	OpSetDraftItemQuantity  = "sales.set_draft_item_quantity"
	OpSetDraftItemSize      = "sales.set_draft_item_size"
	OpSetDraftItemNote      = "sales.set_draft_item_note"
	OpSetDraftItemModifiers = "sales.set_draft_item_modifiers"
	OpRemoveDraftItem       = "sales.remove_draft_item"
	OpGetServiceSession     = "sales.get_service_session"
	OpListServiceSessions   = "sales.list_service_sessions"
)

// Audit event types. Business events are UPPER_SNAKE_CASE and the denial event
// is lowercase dotted, matching internal/tables and internal/shift.
const (
	EventServiceSessionStarted       = "SERVICE_SESSION_STARTED"
	EventDineInServiceSessionStarted = "DINE_IN_SERVICE_SESSION_STARTED"
	EventTableAssignmentCreated      = "TABLE_ASSIGNMENT_CREATED"
	EventTableAssignmentReleased     = "TABLE_ASSIGNMENT_RELEASED"
	EventDraftItemAdded              = "ORDER_DRAFT_ITEM_ADDED"
	EventDraftItemQuantitySet        = "ORDER_DRAFT_ITEM_QUANTITY_SET"
	EventDraftItemSizeSet            = "ORDER_DRAFT_ITEM_SIZE_SET"
	EventDraftItemNoteSet            = "ORDER_DRAFT_ITEM_NOTE_SET"
	EventDraftItemModifiersSet       = "ORDER_DRAFT_ITEM_MODIFIERS_SET"
	EventDraftItemRemoved            = "ORDER_DRAFT_ITEM_REMOVED"
	// EventDraftItemsMerged has no canonical counterpart. A composition edit
	// that absorbs one row into another changes a visible quantity that no
	// command asked for; without its own event the audit trail cannot explain
	// the change.
	EventDraftItemsMerged    = "ORDER_DRAFT_ITEMS_MERGED"
	EventAuthorizationDenied = "sales.authorization_denied"
)

// Service Session states. 5A writes only StateActive; 5D writes StateClosed.
const (
	StateActive = "ACTIVE"
	StateClosed = "CLOSED"
)

// Order Draft states. 5A writes only DraftStateEditable; 5B writes
// DraftStateCommitted.
const (
	DraftStateEditable  = "EDITABLE"
	DraftStateCommitted = "COMMITTED"
)

// Service modes.
const (
	ModeTakeaway = "TAKEAWAY"
	ModeDineIn   = "DINE_IN"
)

// Quantity bounds. The canonical maximum is floor(MAX_SAFE_INTEGER /
// MAX_MENU_PRICE_VND), an artifact of JavaScript integer precision rather than
// a business rule. Go computes line totals in int64 and has no such hazard, so
// this is a deliberate data-entry guard instead. See the spec, section 6.7.
const (
	MinQuantity int32 = 1
	MaxQuantity int32 = 9999
)

// MaxPreparationNoteLength is counted in Unicode code points so Go agrees with
// the database char_length check.
const MaxPreparationNoteLength = 200

// MaxServiceSequence keeps a formatted Service Number at six characters, which
// the service_session_number_valid check constraint requires.
const MaxServiceSequence int32 = 99999

// NormalizePreparationNote trims a note, converts an empty result to SQL NULL,
// and rejects anything longer than MaxPreparationNoteLength code points.
func NormalizePreparationNote(v *string) (sql.NullString, error) {
	if v == nil {
		return sql.NullString{}, nil
	}
	trimmed := strings.TrimSpace(*v)
	if trimmed == "" {
		return sql.NullString{}, nil
	}
	if utf8.RuneCountInString(trimmed) > MaxPreparationNoteLength {
		return sql.NullString{}, fmt.Errorf("%w: preparation_note exceeds %d characters",
			ErrInvalidPreparationNote, MaxPreparationNoteLength)
	}
	return sql.NullString{String: trimmed, Valid: true}, nil
}

// ValidateQuantity enforces the draft item quantity bounds.
func ValidateQuantity(q int32) error {
	if q < MinQuantity || q > MaxQuantity {
		return fmt.Errorf("%w: quantity must be between %d and %d",
			ErrInvalidQuantity, MinQuantity, MaxQuantity)
	}
	return nil
}

// SortedUUIDs returns a sorted copy, leaving the caller's slice untouched.
// Selection order carries meaning for audit events and assignment sequence, so
// callers must never sort in place.
func SortedUUIDs(ids []uuid.UUID) []uuid.UUID {
	out := make([]uuid.UUID, len(ids))
	copy(out, ids)
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

// HasDuplicateUUIDs reports whether ids contains the same value twice.
func HasDuplicateUUIDs(ids []uuid.UUID) bool {
	seen := make(map[uuid.UUID]struct{}, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			return true
		}
		seen[id] = struct{}{}
	}
	return false
}

// ModifierKeyFor builds the denormalized composition key: the option ids
// sorted and comma joined. The empty set is the empty string.
func ModifierKeyFor(ids []uuid.UUID) string {
	if len(ids) == 0 {
		return ""
	}
	sorted := SortedUUIDs(ids)
	parts := make([]string, len(sorted))
	for i, id := range sorted {
		parts[i] = id.String()
	}
	return strings.Join(parts, ",")
}

// FormatServiceNumber renders a Shift-scoped sequence as the operational
// label. It errors rather than producing a seventh character, so an overflow
// surfaces here instead of as an unmapped 23514 at insert time.
func FormatServiceNumber(seq int32) (string, error) {
	if seq < 1 || seq > MaxServiceSequence {
		return "", fmt.Errorf("%w: service sequence %d is outside 1..%d",
			ErrServiceSequenceExhausted, seq, MaxServiceSequence)
	}
	return fmt.Sprintf("S%05d", seq), nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

The tests reference `ErrInvalidPreparationNote`, `ErrInvalidQuantity`, and `ErrServiceSequenceExhausted`, which Task 3 defines. Add a temporary stub at the bottom of `domain.go` so this task compiles on its own, and delete it in Task 3:

```go
// Temporary: removed in Task 3 when errors.go defines these for real.
var (
	ErrInvalidPreparationNote   = errors.New("invalid preparation note")
	ErrInvalidQuantity          = errors.New("invalid quantity")
	ErrServiceSequenceExhausted = errors.New("service sequence exhausted")
)
```

Add `"errors"` to the import block for now.

Run: `go test ./internal/sales/ -v`
Expected: PASS, every subtest.

- [ ] **Step 5: Commit**

```bash
make fmt
git add internal/sales/domain.go internal/sales/domain_test.go
git commit -m "feat(sales): add Phase 5A domain primitives

Capability, operation and audit event names, state constants, and the pure
validation and formatting functions: preparation note normalization,
quantity bounds, composition modifier key, and Shift-scoped Service Number
formatting.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

## Task 3: Error Sentinels And Mapping

**Files:**
- Create: `internal/sales/errors.go`
- Modify: `internal/sales/domain.go` (delete the temporary `var` block from Task 2 and drop the `errors` import)
- Test: `internal/sales/errors_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: every sentinel below, plus `func MapDBError(err error) error` and `func MapHTTPError(err error) error`.

The spec's error set (section 11) maps one-to-one onto these sentinels.

- [ ] **Step 1: Write the failing tests**

Create `internal/sales/errors_test.go`:

```go
package sales

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func codedFrom(t *testing.T, err error) *response.CodedError {
	t.Helper()
	var coded *response.CodedError
	require.ErrorAs(t, MapHTTPError(err), &coded)
	return coded
}

func TestMapHTTPErrorCodes(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"open shift required", ErrOpenShiftRequired, http.StatusConflict, "OPEN_SALES_SHIFT_REQUIRED"},
		{"session not found", ErrServiceSessionNotFound, http.StatusNotFound, "SERVICE_SESSION_NOT_FOUND"},
		{"session closed", ErrServiceSessionClosed, http.StatusConflict, "SERVICE_SESSION_ALREADY_CLOSED"},
		{"draft not found", ErrEditableDraftNotFound, http.StatusConflict, "EDITABLE_DRAFT_NOT_FOUND"},
		{"draft item not found", ErrDraftItemNotFound, http.StatusNotFound, "DRAFT_ITEM_NOT_FOUND"},
		{"menu item not found", ErrMenuItemNotFound, http.StatusNotFound, "MENU_ITEM_NOT_FOUND"},
		{"menu item unavailable", ErrMenuItemUnavailable, http.StatusConflict, "MENU_ITEM_UNAVAILABLE"},
		{"menu item retired", ErrMenuItemRetired, http.StatusConflict, "MENU_ITEM_RETIRED"},
		{"size not found", ErrSizeNotFound, http.StatusNotFound, "SIZE_NOT_FOUND"},
		{"size unavailable", ErrSizeUnavailable, http.StatusConflict, "SIZE_UNAVAILABLE"},
		{"size retired", ErrSizeRetired, http.StatusConflict, "SIZE_RETIRED"},
		{"option not found", ErrModifierOptionNotFound, http.StatusNotFound, "MODIFIER_OPTION_NOT_FOUND"},
		{"option unavailable", ErrModifierOptionUnavailable, http.StatusConflict, "MODIFIER_OPTION_UNAVAILABLE"},
		{"option retired", ErrModifierOptionRetired, http.StatusConflict, "MODIFIER_OPTION_RETIRED"},
		{"bad note", ErrInvalidPreparationNote, http.StatusBadRequest, "INVALID_PREPARATION_NOTE"},
		{"bad quantity", ErrInvalidQuantity, http.StatusBadRequest, "INVALID_INPUT"},
		{"table selection required", ErrTableSelectionRequired, http.StatusBadRequest, "DINE_IN_TABLE_SELECTION_REQUIRED"},
		{"table selection duplicate", ErrTableSelectionDuplicate, http.StatusBadRequest, "DINE_IN_TABLE_SELECTION_DUPLICATE"},
		{"table not found", ErrTableNotFound, http.StatusNotFound, "DINE_IN_TABLE_NOT_FOUND"},
		{"table unavailable", ErrTableUnavailable, http.StatusConflict, "DINE_IN_TABLE_UNAVAILABLE"},
		{"takeaway has no tables", ErrTakeawayTablesNotAvailable, http.StatusConflict, "TAKEAWAY_TABLE_ASSIGNMENT_NOT_AVAILABLE"},
		{"request conflict", ErrRequestConflict, http.StatusConflict, "REQUEST_CONFLICT"},
		{"stored result", ErrInvalidStoredResult, http.StatusInternalServerError, "INVALID_STORED_RESULT"},
		{"forbidden", ErrForbidden, http.StatusForbidden, "NOT_AUTHORIZED"},
		{"unauthorized", ErrUnauthorized, http.StatusUnauthorized, "NOT_AUTHORIZED"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			coded := codedFrom(t, tc.err)
			assert.Equal(t, tc.status, coded.Status)
			assert.Equal(t, tc.code, coded.Code)
		})
	}
}

// Both denial sentinels collapse to one client-visible code, so the API never
// discloses whether an identity exists, is disabled, or merely lacks a role.
func TestDenialsShareOneClientCode(t *testing.T) {
	assert.Equal(t, "NOT_AUTHORIZED", codedFrom(t, ErrForbidden).Code)
	assert.Equal(t, "NOT_AUTHORIZED", codedFrom(t, ErrUnauthorized).Code)
}

// A wrapped PostgreSQL detail is server-side evidence and must not reach the
// client message.
func TestWrappedDetailStaysServerSide(t *testing.T) {
	err := fmt.Errorf("%w: %s", ErrOpenShiftRequired, "Key (sales_shift_id)=(...) is not present")
	coded := codedFrom(t, err)
	assert.NotContains(t, coded.Message, "sales_shift_id")
	assert.Equal(t, ErrOpenShiftRequired.Error(), coded.Message)
}

func TestMapDBError(t *testing.T) {
	t.Run("service session FK means no open shift", func(t *testing.T) {
		pgErr := &pgconn.PgError{
			Code:           "23503",
			ConstraintName: serviceSessionSalesShiftFK,
			Detail:         "Key (sales_shift_id)=(x) is not present in table \"sales_shifts\".",
		}
		assert.ErrorIs(t, MapDBError(pgErr), ErrOpenShiftRequired)
	})

	t.Run("an unrelated FK is a defect not a business state", func(t *testing.T) {
		pgErr := &pgconn.PgError{Code: "23503", ConstraintName: "some_other_fkey"}
		mapped := MapDBError(pgErr)
		assert.NotErrorIs(t, mapped, ErrOpenShiftRequired)
		assert.Equal(t, pgErr, mapped)
	})

	t.Run("check violation is never mapped", func(t *testing.T) {
		pgErr := &pgconn.PgError{Code: "23514", ConstraintName: "order_draft_item_quantity_valid"}
		assert.Equal(t, pgErr, MapDBError(pgErr))
	})

	t.Run("nil stays nil", func(t *testing.T) {
		assert.NoError(t, MapDBError(nil))
	})
}

func TestMapHTTPErrorPassesThroughCodedAndUnknown(t *testing.T) {
	coded := response.NewCodedError(http.StatusTeapot, "TEAPOT", "teapot", nil)
	assert.Equal(t, error(coded), MapHTTPError(coded))

	unknown := errors.New("boom")
	assert.Equal(t, unknown, MapHTTPError(unknown))

	assert.NoError(t, MapHTTPError(nil))
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/sales/ -run 'TestMap|TestDenials|TestWrapped' -v`
Expected: FAIL — `undefined: ErrOpenShiftRequired` and the rest.

> **Note for the implementer:** confirm `response.CodedError`'s exported field
> names in `internal/response/`. The test above assumes `Status`, `Code`, and
> `Message`. If they differ, fix the test to match the real type rather than
> changing the type.

- [ ] **Step 3: Write the implementation**

Create `internal/sales/errors.go`:

```go
package sales

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrOpenShiftRequired      = errors.New("an open sales shift is required")
	ErrServiceSessionNotFound = errors.New("service session not found")
	ErrServiceSessionClosed   = errors.New("service session is already closed")
	ErrEditableDraftNotFound  = errors.New("no editable order draft for this service session")
	ErrDraftItemNotFound      = errors.New("draft item not found")

	ErrMenuItemNotFound    = errors.New("menu item not found")
	ErrMenuItemUnavailable = errors.New("menu item is unavailable")
	ErrMenuItemRetired     = errors.New("menu item is retired")

	ErrSizeNotFound    = errors.New("size not found for this menu item")
	ErrSizeUnavailable = errors.New("size is unavailable")
	ErrSizeRetired     = errors.New("size is retired")

	ErrModifierOptionNotFound    = errors.New("modifier option not found for this menu item")
	ErrModifierOptionUnavailable = errors.New("modifier option is unavailable")
	ErrModifierOptionRetired     = errors.New("modifier option is retired")

	ErrInvalidPreparationNote = errors.New("invalid preparation note")
	ErrInvalidQuantity        = errors.New("invalid quantity")

	ErrTableSelectionRequired     = errors.New("at least one table is required for a dine-in session")
	ErrTableSelectionDuplicate    = errors.New("the same table was selected twice")
	ErrTableNotFound              = errors.New("table not found")
	ErrTableUnavailable           = errors.New("table is unavailable")
	ErrTakeawayTablesNotAvailable = errors.New("a takeaway session cannot be assigned tables")

	ErrRequestConflict     = errors.New("request conflict")
	ErrInvalidStoredResult = errors.New("invalid stored result")
	ErrForbidden           = errors.New("forbidden")
	ErrUnauthorized        = errors.New("unauthorized")

	// ErrServiceSequenceExhausted cannot occur in normal operation: a Shift
	// would need 99,999 Service Sessions. It exists so FormatServiceNumber has
	// a typed failure instead of emitting a seventh character that the
	// database check would reject as an unmapped 23514.
	ErrServiceSequenceExhausted = errors.New("service number sequence exhausted for this shift")
)

// serviceSessionSalesShiftFK is the auto-generated name of the only foreign
// key whose violation means "no open Shift to attach to". service_sessions has
// another FK (the creating identity) whose violation is a defect, not this
// business state.
const serviceSessionSalesShiftFK = "service_sessions_sales_shift_id_fkey"

// MapDBError maps PostgreSQL driver and database errors to domain sentinels
// inside the Sales boundary, so an expected constraint failure never becomes
// an accidental generic 500.
func MapDBError(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		msg := pgErr.Detail
		if msg == "" {
			msg = pgErr.Message
		}
		if pgErr.Code == "23503" && pgErr.ConstraintName == serviceSessionSalesShiftFK {
			return fmt.Errorf("%w: %s", ErrOpenShiftRequired, msg)
		}
		// Every other code is deliberately unmapped:
		//
		//   23505 on the composition index is absorbed by the add path's
		//   upsert and never reaches here.
		//   23505 on service_session_number_per_shift_unique means the
		//   advisory-locked allocation path has a defect.
		//   23514 means Go validation and the database disagree.
		//
		// All three are defects, not business states, and must surface as
		// logged 500s.
	}
	return err
}

// MapHTTPError maps Sales domain errors and input validation errors to
// *response.CodedError.
//
// Note that sql.ErrNoRows is not mapped here. Unlike Shift, where a missing
// row has exactly one meaning, Sales reads several different rows and each
// caller decides which sentinel a miss means. A bare sql.ErrNoRows reaching
// this function is a defect in the caller.
func MapHTTPError(err error) error {
	if err == nil {
		return nil
	}
	var codedErr *response.CodedError
	if errors.As(err, &codedErr) {
		return err
	}

	// The client message is always the stable sentinel text, never the wrapped
	// PostgreSQL detail, which must stay server-side only.
	coded := func(status int, code string, sentinel error) error {
		return response.NewCodedError(status, code, sentinel.Error(), err)
	}

	switch {
	case errors.Is(err, ErrOpenShiftRequired):
		return coded(http.StatusConflict, "OPEN_SALES_SHIFT_REQUIRED", ErrOpenShiftRequired)
	case errors.Is(err, ErrServiceSessionNotFound):
		return coded(http.StatusNotFound, "SERVICE_SESSION_NOT_FOUND", ErrServiceSessionNotFound)
	case errors.Is(err, ErrServiceSessionClosed):
		return coded(http.StatusConflict, "SERVICE_SESSION_ALREADY_CLOSED", ErrServiceSessionClosed)
	case errors.Is(err, ErrEditableDraftNotFound):
		return coded(http.StatusConflict, "EDITABLE_DRAFT_NOT_FOUND", ErrEditableDraftNotFound)
	case errors.Is(err, ErrDraftItemNotFound):
		return coded(http.StatusNotFound, "DRAFT_ITEM_NOT_FOUND", ErrDraftItemNotFound)

	case errors.Is(err, ErrMenuItemNotFound):
		return coded(http.StatusNotFound, "MENU_ITEM_NOT_FOUND", ErrMenuItemNotFound)
	case errors.Is(err, ErrMenuItemUnavailable):
		return coded(http.StatusConflict, "MENU_ITEM_UNAVAILABLE", ErrMenuItemUnavailable)
	case errors.Is(err, ErrMenuItemRetired):
		return coded(http.StatusConflict, "MENU_ITEM_RETIRED", ErrMenuItemRetired)

	case errors.Is(err, ErrSizeNotFound):
		return coded(http.StatusNotFound, "SIZE_NOT_FOUND", ErrSizeNotFound)
	case errors.Is(err, ErrSizeUnavailable):
		return coded(http.StatusConflict, "SIZE_UNAVAILABLE", ErrSizeUnavailable)
	case errors.Is(err, ErrSizeRetired):
		return coded(http.StatusConflict, "SIZE_RETIRED", ErrSizeRetired)

	case errors.Is(err, ErrModifierOptionNotFound):
		return coded(http.StatusNotFound, "MODIFIER_OPTION_NOT_FOUND", ErrModifierOptionNotFound)
	case errors.Is(err, ErrModifierOptionUnavailable):
		return coded(http.StatusConflict, "MODIFIER_OPTION_UNAVAILABLE", ErrModifierOptionUnavailable)
	case errors.Is(err, ErrModifierOptionRetired):
		return coded(http.StatusConflict, "MODIFIER_OPTION_RETIRED", ErrModifierOptionRetired)

	case errors.Is(err, ErrInvalidPreparationNote):
		return response.NewCodedError(http.StatusBadRequest, "INVALID_PREPARATION_NOTE", err.Error(), err)

	case errors.Is(err, ErrTableSelectionRequired):
		return coded(http.StatusBadRequest, "DINE_IN_TABLE_SELECTION_REQUIRED", ErrTableSelectionRequired)
	case errors.Is(err, ErrTableSelectionDuplicate):
		return coded(http.StatusBadRequest, "DINE_IN_TABLE_SELECTION_DUPLICATE", ErrTableSelectionDuplicate)
	case errors.Is(err, ErrTableNotFound):
		return coded(http.StatusNotFound, "DINE_IN_TABLE_NOT_FOUND", ErrTableNotFound)
	case errors.Is(err, ErrTableUnavailable):
		// The wrapped message names the Table so staff know which one to free;
		// it contains no data the caller could not already see.
		return response.NewCodedError(http.StatusConflict, "DINE_IN_TABLE_UNAVAILABLE", err.Error(), err)
	case errors.Is(err, ErrTakeawayTablesNotAvailable):
		return coded(http.StatusConflict, "TAKEAWAY_TABLE_ASSIGNMENT_NOT_AVAILABLE", ErrTakeawayTablesNotAvailable)

	case errors.Is(err, ErrRequestConflict):
		return response.NewCodedError(http.StatusConflict, "REQUEST_CONFLICT", err.Error(), err)
	case errors.Is(err, ErrInvalidStoredResult), errors.Is(err, ErrServiceSequenceExhausted):
		return response.NewCodedError(http.StatusInternalServerError, "INVALID_STORED_RESULT",
			"an unexpected error occurred", err)

	// Both denial sentinels collapse to one code so the API never discloses
	// which condition failed. The reason is in the server log and the audit
	// event.
	case errors.Is(err, ErrForbidden):
		return response.NewCodedError(http.StatusForbidden, "NOT_AUTHORIZED", "not authorized", err)
	case errors.Is(err, ErrUnauthorized):
		return response.NewCodedError(http.StatusUnauthorized, "NOT_AUTHORIZED", "not authorized", err)

	// ErrInvalidQuantity is wrapped in response.ErrInvalid by its caller, so
	// it lands on the generic validation code alongside every other bad field.
	case errors.Is(err, response.ErrInvalid), errors.Is(err, ErrInvalidQuantity):
		return response.NewCodedError(http.StatusBadRequest, "INVALID_INPUT", err.Error(), err)
	default:
		return err
	}
}

// unused keeps the sql import honest if a future edit drops the only use.
var _ = sql.ErrNoRows
```

- [ ] **Step 4: Delete the temporary stub from Task 2**

Remove the `// Temporary: removed in Task 3` `var` block from `internal/sales/domain.go` and drop `"errors"` from its imports.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/sales/ -v`
Expected: PASS, every test from Tasks 2 and 3.

Run: `go vet ./internal/sales/`
Expected: no output.

- [ ] **Step 6: Commit**

```bash
make fmt
git add internal/sales/errors.go internal/sales/errors_test.go internal/sales/domain.go
git commit -m "feat(sales): add Phase 5A error sentinels and mapping

Maps the spec's error subset to stable API codes, collapses both denial
sentinels to NOT_AUTHORIZED, and maps only the sales_shift_id foreign key
violation, leaving 23505 and 23514 as logged defects.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

## Task 4: Transaction Executor

A near-copy of `internal/shift/executor.go` minus second-party approval. ADR-007 requires each slice to own its executor; do not import Shift's.

**Files:**
- Create: `internal/sales/executor.go`
- Create: `sql/queries/sales.sql`
- Test: `internal/sales/executor_integration_test.go`

**Interfaces:**
- Consumes: `auth.DeriveCapabilities`, `auth.GetInactivityTimeout`, `auth.SessionStateLocked`; the shared `GetIdempotencyRecord`, `ClaimIdempotencyRecord`, `StoreIdempotencyResult`, `InsertAuditEvent` queries.
- Produces:
  - `type Actor struct { StaffID, SessionID uuid.UUID }`
  - `type MutationSpec struct { RequestID uuid.UUID; Operation string; Fingerprint any; Required []string }`
  - `type MutationContext struct { Queries *sqlc.Queries }`
  - `type AuditRecord struct { EventType string; Details any }`
  - `type Runner`, `func NewRunner(db *sql.DB, queries *sqlc.Queries) *Runner`
  - `func ExecuteMutation[T any](ctx, r, actor, spec, fn func(MutationContext) (int, T, AuditRecord, error)) (int, T, error)`
  - `func ExecuteRead[T any](ctx, r, actor, operation, requiredCapability string, fn func(*sqlc.Queries) (T, error)) (T, error)`
  - `func (r *Runner) AdvisoryLock(ctx context.Context, q *sqlc.Queries, key int64) error`
  - `func IDToLockKey(id uuid.UUID) int64`

- [ ] **Step 1: Write the executor's SQL queries**

Create `sql/queries/sales.sql`:

```sql
-- Queries for internal/sales (Phase 5A).
--
-- Authority, role, and advisory-lock queries are slice-local by ADR-007: the
-- shared table is shared, the helper logic is not.

-- name: GetSalesSessionAuthority :one
SELECT s.id AS session_id, s.staff_identity_id, s.state, s.active_workspace,
       s.last_human_activity_at, s.expires_at, s.revoked_at,
       i.enabled AS identity_enabled, i.display_name, i.login_code
FROM staff_access_sessions s
JOIN staff_identities i ON i.id = s.staff_identity_id
WHERE s.id = $1 AND s.staff_identity_id = $2;

-- name: GetSalesSessionRoles :many
SELECT role
FROM staff_operational_roles
WHERE staff_identity_id = $1
ORDER BY role ASC;

-- name: SalesAdvisoryLock :exec
SELECT pg_advisory_xact_lock($1);
```

- [ ] **Step 2: Regenerate sqlc bindings**

Run: `make sqlc`
Expected: `internal/database/sqlc/sales.sql.go` is created with `GetSalesSessionAuthority`, `GetSalesSessionRoles`, and `SalesAdvisoryLock`.

- [ ] **Step 3: Write the failing integration test**

Create `internal/sales/executor_integration_test.go`:

```go
//go:build integration

package sales_test

import (
	"context"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// probeResult is the trivial payload the executor tests round-trip through
// idempotency storage.
type probeResult struct {
	Value string `json:"value"`
}

type probeFingerprint struct {
	Value string `json:"value"`
}

// runProbe executes a minimal mutation through the real executor.
func runProbe(t *testing.T, runner *sales.Runner, actor sales.Actor,
	requestID uuid.UUID, value string, calls *int,
) (int, probeResult, error) {
	t.Helper()
	return sales.ExecuteMutation(context.Background(), runner, actor, sales.MutationSpec{
		RequestID:   requestID,
		Operation:   sales.OpStartTakeawaySession,
		Fingerprint: probeFingerprint{Value: value},
		Required:    []string{sales.CapSalesOperate},
	}, func(mc sales.MutationContext) (int, probeResult, sales.AuditRecord, error) {
		*calls++
		return 201, probeResult{Value: value}, sales.AuditRecord{
			EventType: sales.EventServiceSessionStarted,
			Details:   map[string]string{"value": value},
		}, nil
	})
}

func TestExecutorReplayReturnsStoredResult(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	requestID := uuid.New()
	calls := 0

	status, first, err := runProbe(t, runner, actor, requestID, "one", &calls)
	require.NoError(t, err)
	assert.Equal(t, 201, status)
	assert.Equal(t, 1, calls)

	status, second, err := runProbe(t, runner, actor, requestID, "one", &calls)
	require.NoError(t, err)
	assert.Equal(t, 201, status)
	assert.Equal(t, first, second)
	assert.Equal(t, 1, calls, "a replay must not run the mutation body again")
}

func TestExecutorRejectsReusedRequestIDWithDifferentPayload(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	requestID := uuid.New()
	calls := 0

	_, _, err := runProbe(t, runner, actor, requestID, "one", &calls)
	require.NoError(t, err)

	_, _, err = runProbe(t, runner, actor, requestID, "two", &calls)
	require.ErrorIs(t, err, sales.ErrRequestConflict)
	assert.Equal(t, 1, calls)
}

func TestExecutorDeniesMissingCapability(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"BARISTA"})
	calls := 0

	_, _, err := runProbe(t, runner, actor, uuid.New(), "one", &calls)
	require.ErrorIs(t, err, sales.ErrForbidden)
	assert.Equal(t, 0, calls)

	assertAuditEvent(t, db, sales.EventAuthorizationDenied, 1)
}

// Authority is reloaded inside the transaction and before the idempotency
// replay, so a revoked session cannot replay an earlier success.
func TestExecutorDeniesReplayAfterSessionRevoked(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	requestID := uuid.New()
	calls := 0

	_, _, err := runProbe(t, runner, actor, requestID, "one", &calls)
	require.NoError(t, err)

	_, err = db.Exec(`UPDATE staff_access_sessions SET revoked_at = now() WHERE id = $1`,
		actor.SessionID)
	require.NoError(t, err)

	_, _, err = runProbe(t, runner, actor, requestID, "one", &calls)
	require.ErrorIs(t, err, sales.ErrUnauthorized)
}

func TestExecutorDeniesReplayAfterIdentityDisabled(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	requestID := uuid.New()
	calls := 0

	_, _, err := runProbe(t, runner, actor, requestID, "one", &calls)
	require.NoError(t, err)

	_, err = db.Exec(`UPDATE staff_identities SET enabled = false WHERE id = $1`, actor.StaffID)
	require.NoError(t, err)

	_, _, err = runProbe(t, runner, actor, requestID, "one", &calls)
	require.ErrorIs(t, err, sales.ErrForbidden)
}

// Concurrent duplicates run the mutation exactly once.
func TestExecutorConcurrentDuplicatesRunOnce(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	requestID := uuid.New()
	calls := 0

	start := make(chan struct{})
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			_, _, err := runProbe(t, runner, actor, requestID, "one", &calls)
			errs <- err
		}()
	}
	close(start)
	require.NoError(t, <-errs)
	require.NoError(t, <-errs)
	assert.Equal(t, 1, calls)
}

// A business audit event is written for a success and none for a replay.
func TestExecutorAuditsOnceAcrossReplay(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	requestID := uuid.New()
	calls := 0

	_, _, err := runProbe(t, runner, actor, requestID, "one", &calls)
	require.NoError(t, err)
	assertAuditEvent(t, db, sales.EventServiceSessionStarted, 1)

	_, _, err = runProbe(t, runner, actor, requestID, "one", &calls)
	require.NoError(t, err)
	assertAuditEvent(t, db, sales.EventServiceSessionStarted, 1)
}

// An audit failure must roll back the business mutation AND the idempotency
// claim, so the request can be retried rather than being permanently stuck
// replaying a result that was never committed.
func TestExecutorAuditFailureRollsBackTheClaim(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	requestID := uuid.New()

	// A channel cannot be marshalled, so the audit insert step fails after the
	// claim and after the mutation body ran.
	_, _, err := sales.ExecuteMutation(context.Background(), runner, actor, sales.MutationSpec{
		RequestID:   requestID,
		Operation:   sales.OpStartTakeawaySession,
		Fingerprint: probeFingerprint{Value: "one"},
		Required:    []string{sales.CapSalesOperate},
	}, func(mc sales.MutationContext) (int, probeResult, sales.AuditRecord, error) {
		return 201, probeResult{Value: "one"}, sales.AuditRecord{
			EventType: sales.EventServiceSessionStarted,
			Details:   make(chan int),
		}, nil
	})
	require.Error(t, err)

	var claims int
	require.NoError(t, db.QueryRow(
		`SELECT count(*) FROM idempotency_keys WHERE key = $1`, requestID).Scan(&claims))
	assert.Equal(t, 0, claims, "the idempotency claim must roll back with the mutation")

	assertAuditEvent(t, db, sales.EventServiceSessionStarted, 0)

	// The same request_id is therefore reusable.
	calls := 0
	_, _, err = runProbe(t, runner, actor, requestID, "one", &calls)
	require.NoError(t, err)
	assert.Equal(t, 1, calls)
}
```

- [ ] **Step 4: Extend the test harness**

Append to `internal/sales/testmain_integration_test.go`:

```go
// seedActor creates an enabled identity with the given roles and an active
// access session, returning the Actor the executor expects.
func seedActor(t *testing.T, q *sqlc.Queries, roles []string) sales.Actor {
	t.Helper()
	ctx := context.Background()

	code := testLoginCode("A")
	hash, err := auth.HashPin("1234")
	require.NoError(t, err)

	identity, err := q.CreateStaffIdentity(ctx, sqlc.CreateStaffIdentityParams{
		DisplayName: "Actor " + code,
		Btrim:       code,
		PinHash:     hash,
		Enabled:     true,
	})
	require.NoError(t, err)

	for _, role := range roles {
		require.NoError(t, q.AddStaffRole(ctx, sqlc.AddStaffRoleParams{
			StaffIdentityID: identity.ID,
			Role:            role,
		}))
	}

	session := createTestSession(t, q, identity.ID)
	return sales.Actor{StaffID: identity.ID, SessionID: session}
}

// assertAuditEvent asserts the exact number of audit_events rows of a type.
func assertAuditEvent(t *testing.T, db *sql.DB, eventType string, want int) {
	t.Helper()
	var got int
	require.NoError(t, db.QueryRow(
		`SELECT count(*) FROM audit_events WHERE event_type = $1`, eventType).Scan(&got))
	assert.Equal(t, want, got, "audit_events rows of type %s", eventType)
}
```

> **Note for the implementer:** `createTestSession` does not exist yet. Copy
> the session-creation helper from `internal/shift`'s integration harness
> (search for where it inserts into `staff_access_sessions` with an
> `expires_at` in the future and `last_human_activity_at` set to now) rather
> than inventing one. The session must be `active`, unexpired, and recently
> active, or `reloadAuthority` will deny every test. Add the `auth`,
> `sales`, and `assert` imports.

- [ ] **Step 5: Run the tests to verify they fail**

Run: `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -p 1 -tags=integration ./internal/sales/ -run TestExecutor -v`
Expected: FAIL — `undefined: sales.NewRunner`.

- [ ] **Step 6: Write the implementation**

Create `internal/sales/executor.go`. This mirrors `internal/shift/executor.go`; read that file first and keep the structure identical so a reviewer can diff the two.

```go
package sales

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

// Actor identifies the authenticated staff member executing an operation.
type Actor struct {
	StaffID   uuid.UUID
	SessionID uuid.UUID
}

// MutationSpec carries request-level metadata for a mutation command.
//
// No Sales operation in 5A takes a PIN or any other secret, so unlike Shift's
// spec there is no approval field and no secret can reach the fingerprint.
type MutationSpec struct {
	RequestID   uuid.UUID
	Operation   string
	Fingerprint any
	Required    []string
}

// MutationContext carries per-execution facts the mutation body needs.
type MutationContext struct {
	Queries *sqlc.Queries
}

// AuditRecord describes the audit event to insert after a successful mutation.
// A zero EventType writes no business event.
type AuditRecord struct {
	EventType string
	Details   any
}

// committedDenial carries a resolved authorization denial: err is the original
// denial reason the client must see, and committed reports whether its audit
// event was durably written.
type committedDenial struct {
	err       error
	committed bool
}

func (e *committedDenial) Error() string { return e.err.Error() }
func (e *committedDenial) Unwrap() error { return e.err }

// Runner holds the database connection and generated queries.
type Runner struct {
	db      *sql.DB
	queries *sqlc.Queries
}

// NewRunner creates a Runner.
func NewRunner(db *sql.DB, queries *sqlc.Queries) *Runner {
	return &Runner{db: db, queries: queries}
}

// IDToLockKey converts a UUID to a stable int64 for advisory locking.
func IDToLockKey(id uuid.UUID) int64 {
	//nolint:gosec // G115: bit-cast first 8 bytes of UUID to int64 for advisory lock key
	return int64(binary.BigEndian.Uint64(id[:8]))
}

// fpHash computes SHA-256 of the JSON-encoded business fingerprint.
func fpHash(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("marshal mutation fingerprint: %w", err)
	}
	h := sha256.Sum256(b)
	return fmt.Sprintf("%x", h), nil
}

// reloadAuthority loads the current session, roles, and capabilities inside the
// transaction, so authority removed mid-session takes effect immediately.
func reloadAuthority(ctx context.Context, q *sqlc.Queries, actor Actor) (
	sqlc.GetSalesSessionAuthorityRow, []string, error,
) {
	authRow, err := q.GetSalesSessionAuthority(ctx, sqlc.GetSalesSessionAuthorityParams{
		ID:              actor.SessionID,
		StaffIdentityID: actor.StaffID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return authRow, nil, fmt.Errorf("%w: session not found or identity mismatch", ErrUnauthorized)
		}
		return authRow, nil, fmt.Errorf("reload session authority: %w", err)
	}

	if authRow.State == auth.SessionStateLocked {
		return authRow, nil, fmt.Errorf("%w: session locked", ErrUnauthorized)
	}
	if authRow.RevokedAt.Valid {
		return authRow, nil, fmt.Errorf("%w: session revoked", ErrUnauthorized)
	}
	if time.Now().After(authRow.ExpiresAt) {
		return authRow, nil, fmt.Errorf("%w: session expired", ErrUnauthorized)
	}
	if !authRow.IdentityEnabled {
		return authRow, nil, fmt.Errorf("%w: identity disabled", ErrForbidden)
	}
	workspace := ""
	if authRow.ActiveWorkspace.Valid {
		workspace = authRow.ActiveWorkspace.String
	}
	if time.Since(authRow.LastHumanActivityAt) >= auth.GetInactivityTimeout(workspace) {
		return authRow, nil, fmt.Errorf("%w: session inactive", ErrUnauthorized)
	}

	roles, err := q.GetSalesSessionRoles(ctx, authRow.StaffIdentityID)
	if err != nil {
		return authRow, nil, fmt.Errorf("reload session roles: %w", err)
	}

	return authRow, auth.DeriveCapabilities(roles), nil
}

// verifyCapabilities checks that all required capabilities are present.
func verifyCapabilities(required []string, available []string) error {
	avail := make(map[string]struct{}, len(available))
	for _, c := range available {
		avail[c] = struct{}{}
	}
	for _, need := range required {
		if _, ok := avail[need]; !ok {
			return fmt.Errorf("%w: missing capability %q", ErrForbidden, need)
		}
	}
	return nil
}

// recordDenial writes a denial audit event and returns the resolved outcome.
//
// The session id is attributed only when authority confirms that exact session
// row still exists, because audit_events.session_id carries its own foreign
// key: naming a session this transaction could not find would fail the audit
// insert itself.
func recordDenial(ctx context.Context, q *sqlc.Queries, actor Actor,
	authority sqlc.GetSalesSessionAuthorityRow, operation string, denialErr error,
) *committedDenial {
	details, _ := json.Marshal(map[string]string{
		"operation": operation,
		"reason":    denialErr.Error(),
	})
	sessionID := uuid.NullUUID{}
	if authority.SessionID == actor.SessionID {
		sessionID = uuid.NullUUID{UUID: actor.SessionID, Valid: true}
	}
	if _, err := q.InsertAuditEvent(ctx, sqlc.InsertAuditEventParams{
		EventType:  EventAuthorizationDenied,
		ActorID:    uuid.NullUUID{UUID: actor.StaffID, Valid: true},
		SessionID:  sessionID,
		Details:    details,
		OccurredAt: time.Now(),
	}); err != nil {
		slog.Error("insert denial audit event",
			"operation", operation,
			"reason", denialErr.Error(),
			"staff_identity_id", actor.StaffID,
			"error", err)
		return &committedDenial{err: denialErr, committed: false}
	}
	slog.Warn("sales authorization denied",
		"operation", operation,
		"reason", denialErr.Error(),
		"staff_identity_id", actor.StaffID)
	return &committedDenial{err: denialErr, committed: true}
}

// finishDenial commits the denial's audit event when it was written and always
// returns the original denial reason.
func finishDenial(tx *sql.Tx, outcome *committedDenial) error {
	if outcome.committed {
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit authorization denial: %w", err)
		}
	}
	return outcome.err
}

func isSecurityDenial(err error) bool {
	return errors.Is(err, ErrUnauthorized) || errors.Is(err, ErrForbidden)
}

// AdvisoryLock takes a transaction-scoped advisory lock on the given key.
// Exposed so Service Number allocation can serialize on the Sales Shift id.
func (r *Runner) AdvisoryLock(ctx context.Context, q *sqlc.Queries, key int64) error {
	if err := q.SalesAdvisoryLock(ctx, key); err != nil {
		return fmt.Errorf("advisory lock: %w", err)
	}
	return nil
}

// ExecuteMutation runs a mutation inside one transaction with authorization,
// idempotency, and audit. On success it returns the HTTP status and result.
//
// The order of steps is load-bearing. Authority is reloaded before the
// idempotency replay, so an actor whose session was revoked or whose role was
// removed cannot replay an earlier success. The open-Sales-Shift precondition
// deliberately lives inside fn, which runs after the claim, so a replay of a
// request that succeeded during a Shift still returns its stored result after
// that Shift closes.
func ExecuteMutation[T any](ctx context.Context, r *Runner, actor Actor,
	spec MutationSpec,
	fn func(MutationContext) (int, T, AuditRecord, error),
) (int, T, error) {
	var zero T

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, zero, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	q := r.queries.WithTx(tx)

	// 1. Reload current authority inside the transaction.
	authRow, caps, err := reloadAuthority(ctx, q, actor)
	if err != nil {
		if isSecurityDenial(err) {
			return 0, zero, finishDenial(tx, recordDenial(ctx, q, actor, authRow, spec.Operation, err))
		}
		return 0, zero, err
	}

	// 2. Verify capabilities.
	if err := verifyCapabilities(spec.Required, caps); err != nil {
		return 0, zero, finishDenial(tx, recordDenial(ctx, q, actor, authRow, spec.Operation, err))
	}

	// 3. Fingerprint the normalized business input.
	reqHash, err := fpHash(spec.Fingerprint)
	if err != nil {
		return 0, zero, err
	}

	// 4. Advisory lock to serialize concurrent duplicates of this request.
	if err := r.AdvisoryLock(ctx, q, IDToLockKey(actor.StaffID)^IDToLockKey(spec.RequestID)); err != nil {
		return 0, zero, err
	}

	// 5. Look for an existing record, now that the lock is held.
	existing, err := q.GetIdempotencyRecord(ctx, sqlc.GetIdempotencyRecordParams{
		ActorID: actor.StaffID,
		Key:     spec.RequestID,
	})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, zero, fmt.Errorf("lookup idempotency: %w", err)
	}
	if err == nil {
		if existing.Action != spec.Operation || existing.RequestHash != reqHash {
			return 0, zero, fmt.Errorf("%w: request_id reused for a different change", ErrRequestConflict)
		}
		var result T
		if err := json.Unmarshal(existing.ResponseBody, &result); err != nil {
			return 0, zero, fmt.Errorf("%w: stored response body is corrupted", ErrInvalidStoredResult)
		}
		return int(existing.ResponseCode), result, nil
	}

	// 6. Claim the request before any business mutation.
	claimed, err := q.ClaimIdempotencyRecord(ctx, sqlc.ClaimIdempotencyRecordParams{
		Key:          spec.RequestID,
		ActorID:      actor.StaffID,
		Action:       spec.Operation,
		RequestHash:  reqHash,
		ResponseCode: 0,
		ResponseBody: json.RawMessage("null"),
	})
	if err != nil {
		return 0, zero, fmt.Errorf("claim idempotency request: %w", err)
	}
	if claimed.Action != spec.Operation || claimed.RequestHash != reqHash {
		return 0, zero, fmt.Errorf("%w: request was claimed concurrently", ErrRequestConflict)
	}

	// 7. Run the mutation.
	resultCode, result, audit, err := fn(MutationContext{Queries: q})
	if err != nil {
		return 0, zero, err
	}

	// 8. Write the business audit event, when there is one.
	if audit.EventType != "" {
		detailsBytes, err := json.Marshal(audit.Details)
		if err != nil {
			return 0, zero, fmt.Errorf("marshal audit details: %w", err)
		}
		if _, err := q.InsertAuditEvent(ctx, sqlc.InsertAuditEventParams{
			EventType:  audit.EventType,
			ActorID:    uuid.NullUUID{UUID: actor.StaffID, Valid: true},
			SessionID:  uuid.NullUUID{UUID: actor.SessionID, Valid: true},
			Details:    detailsBytes,
			OccurredAt: time.Now(),
		}); err != nil {
			return 0, zero, fmt.Errorf("insert audit event for %q: %w", spec.Operation, err)
		}
	}

	// 9. Store the replayable result.
	bodyBytes, err := json.Marshal(result)
	if err != nil {
		return 0, zero, fmt.Errorf("marshal response body: %w", err)
	}
	if err := q.StoreIdempotencyResult(ctx, sqlc.StoreIdempotencyResultParams{
		ActorID:      actor.StaffID,
		Key:          spec.RequestID,
		ResponseCode: int32(resultCode), //nolint:gosec // G115: HTTP status (100-599) fits int32
		ResponseBody: bodyBytes,
	}); err != nil {
		return 0, zero, fmt.Errorf("store idempotent result: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, zero, fmt.Errorf("commit transaction: %w", err)
	}
	return resultCode, result, nil
}

// ExecuteRead runs a read inside a read-only repeatable-read transaction so the
// capability check and every query observe one snapshot.
func ExecuteRead[T any](ctx context.Context, r *Runner, actor Actor,
	operation string, requiredCapability string,
	fn func(*sqlc.Queries) (T, error),
) (T, error) {
	var zero T

	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return zero, fmt.Errorf("begin read transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	q := r.queries.WithTx(tx)

	authRow, caps, err := reloadAuthority(ctx, q, actor)
	if err != nil {
		if isSecurityDenial(err) {
			return zero, r.auditReadDenial(ctx, actor, authRow, operation, err)
		}
		return zero, err
	}
	if err := verifyCapabilities([]string{requiredCapability}, caps); err != nil {
		return zero, r.auditReadDenial(ctx, actor, authRow, operation, err)
	}

	result, err := fn(q)
	if err != nil {
		return zero, err
	}
	if err := tx.Commit(); err != nil {
		return zero, fmt.Errorf("commit read transaction: %w", err)
	}
	return result, nil
}

// auditReadDenial records a read-path denial in a short separate read-write
// transaction, since ExecuteRead's own transaction is read-only.
func (r *Runner) auditReadDenial(ctx context.Context, actor Actor,
	authority sqlc.GetSalesSessionAuthorityRow, operation string, denialErr error,
) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		slog.Error("begin read denial audit transaction", "operation", operation, "error", err)
		return denialErr
	}
	defer tx.Rollback() //nolint:errcheck

	outcome := recordDenial(ctx, r.queries.WithTx(tx), actor, authority, operation, denialErr)
	if outcome.committed {
		if err := tx.Commit(); err != nil {
			slog.Error("commit read denial audit transaction", "operation", operation, "error", err)
		}
	}
	return denialErr
}
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -p 1 -race -tags=integration ./internal/sales/ -run TestExecutor -v`
Expected: PASS, all seven tests.

- [ ] **Step 8: Verify the slice boundary**

Run: `go list -deps ./internal/sales/ | grep 'pos-cafe/internal'`
Expected: `internal/auth`, `internal/database/sqlc`, `internal/response` and their own dependencies. **`internal/catalog`, `internal/tables`, and `internal/shift` must not appear.**

- [ ] **Step 9: Commit**

```bash
make fmt
git add internal/sales/executor.go internal/sales/executor_integration_test.go sql/queries/sales.sql internal/database/sqlc/
git commit -m "feat(sales): add Phase 5A transaction executor

Reloads authority inside the transaction and before idempotent replay,
claims an actor-scoped request against the shared idempotency_keys table,
and audits both successes and denials. Slice-local per ADR-007.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

## Task 5: DTOs, Projection, And The Service Session Read

The projection is assembled once and returned by every mutation, so it must exist before any command.

**Files:**
- Create: `internal/sales/dto.go`, `internal/sales/projection.go`, `internal/sales/reads.go`, `internal/sales/http.go`, `internal/sales/routes.go`
- Modify: `sql/queries/sales.sql`, `cmd/api/main.go`
- Test: `internal/sales/dto_test.go`, `internal/sales/projection_integration_test.go`

**Interfaces:**
- Consumes: `Runner`, `ExecuteRead`, `Actor`, `OpGetServiceSession`, `CapSalesOperate`.
- Produces:
  - `type ServiceSessionResponse`, `OrderDraftResponse`, `DraftItemResponse`, `SelectedModifierOptionResponse`, `SessionTableResponse`
  - `func LoadServiceSession(ctx context.Context, q *sqlc.Queries, sessionID uuid.UUID) (ServiceSessionResponse, error)`
  - `type GetServiceSessionHandler`, `func NewGetServiceSessionHandler(runner *Runner) *GetServiceSessionHandler`, `func (h *GetServiceSessionHandler) Handle(ctx, actor, sessionID) (ServiceSessionResponse, error)`
  - `type Slices`, `func NewSlices(db *sql.DB, queries *sqlc.Queries) *Slices`, `func (s *Slices) RegisterRoutes(v1 *echo.Group, authn *auth.Middleware)`

- [ ] **Step 1: Add the projection queries**

Append to `sql/queries/sales.sql`:

```sql
-- name: GetServiceSession :one
SELECT id, service_number, sequence, service_mode, state, sales_shift_id,
       created_by_staff_identity_id, created_at
FROM service_sessions
WHERE id = $1;

-- name: ListServiceSessionTables :many
-- Current assignments only. Released rows are history, not occupancy.
SELECT t.id, t.name, a.sequence
FROM table_assignments a
JOIN tables t ON t.id = a.table_id
WHERE a.service_session_id = $1 AND a.released_at IS NULL
ORDER BY a.sequence ASC;

-- name: GetEditableDraft :one
SELECT id, service_session_id, state, created_at
FROM order_drafts
WHERE service_session_id = $1 AND state = 'EDITABLE';

-- name: ListDraftItems :many
-- price_vnd is the Size price when a Size is chosen and the Item price
-- otherwise, matching the canonical Menu Price rule. available is read live
-- rather than snapshotted: a draft is a live proposal, and an item that became
-- unavailable while the customer was deciding must show as such.
SELECT di.id, di.menu_item_id, mi.name AS menu_item_name,
       di.size_id, s.name AS size_name,
       COALESCE(s.price_vnd, mi.price_vnd) AS price_vnd,
       di.quantity, di.preparation_note, di.modifier_key, di.created_at,
       (mi.available AND mi.retired_at IS NULL
        AND (di.size_id IS NULL OR (s.available AND s.retired_at IS NULL))) AS available
FROM order_draft_items di
JOIN menu_items mi ON mi.id = di.menu_item_id
LEFT JOIN menu_item_sizes s ON s.id = di.size_id
WHERE di.order_draft_id = $1
ORDER BY di.created_at ASC, di.id ASC;

-- name: ListDraftItemModifierOptions :many
-- Ordered by Group then Option name, which is the order the projection emits.
SELECT m.order_draft_item_id, o.id AS option_id, o.name AS option_name,
       o.surcharge_vnd, g.id AS group_id, g.name AS group_name
FROM order_draft_item_modifier_options m
JOIN modifier_options o ON o.id = m.modifier_option_id
JOIN modifier_groups g ON g.id = o.modifier_group_id
JOIN order_draft_items di ON di.id = m.order_draft_item_id
WHERE di.order_draft_id = $1
ORDER BY g.name ASC, o.name ASC, o.id ASC;

-- name: ListActiveServiceSessions :many
SELECT id, service_number, sequence, service_mode, state, sales_shift_id,
       created_by_staff_identity_id, created_at
FROM service_sessions
WHERE state = 'ACTIVE'
ORDER BY created_at ASC, id ASC;
```

Run: `make sqlc`

- [ ] **Step 2: Write the failing DTO test**

Create `internal/sales/dto_test.go`:

```go
package sales

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The projection ships in its final shape from 5A. Fields owned by 5B and 5D
// are present and empty rather than absent, so integrators never face a
// breaking change when those sub-phases land.
func TestServiceSessionResponseShipsFinalShape(t *testing.T) {
	resp := ServiceSessionResponse{
		ID:            uuid.New(),
		ServiceNumber: "S00001",
		ServiceMode:   ModeTakeaway,
		State:         StateActive,
		SalesShiftID:  uuid.New(),
	}

	raw, err := json.Marshal(resp)
	require.NoError(t, err)

	var decoded map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &decoded))

	for _, field := range []string{
		"id", "service_number", "service_mode", "state", "customer_identity_id",
		"sales_shift_id", "tables", "created_at", "draft", "checks", "orders",
		"preparation_units",
	} {
		assert.Contains(t, decoded, field, "field %q must be in the 5A contract", field)
	}

	// Phase 6 concerns are omitted entirely, not stubbed: no Phase 5 sub-phase
	// will ever fill them.
	assert.NotContains(t, decoded, "preparation_alerts")
	assert.NotContains(t, decoded, "preparation_corrections")
}

// Empty collections serialize as [], never null, so clients can iterate
// without a nil check.
func TestEmptyCollectionsSerializeAsArrays(t *testing.T) {
	raw, err := json.Marshal(ServiceSessionResponse{})
	require.NoError(t, err)

	body := string(raw)
	assert.Contains(t, body, `"tables":[]`)
	assert.Contains(t, body, `"checks":[]`)
	assert.Contains(t, body, `"orders":[]`)
	assert.Contains(t, body, `"preparation_units":[]`)
}

// Every Service Session in 5A is anonymous; the field is a constant null.
func TestCustomerIdentityIsAlwaysNull(t *testing.T) {
	raw, err := json.Marshal(ServiceSessionResponse{})
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"customer_identity_id":null`)
}

func TestDraftItemResponseNullables(t *testing.T) {
	raw, err := json.Marshal(DraftItemResponse{})
	require.NoError(t, err)

	body := string(raw)
	assert.Contains(t, body, `"price_vnd":null`)
	assert.Contains(t, body, `"size_id":null`)
	assert.Contains(t, body, `"size_name":null`)
	assert.Contains(t, body, `"preparation_note":null`)
	assert.Contains(t, body, `"selected_modifier_options":[]`)
}

// Absent and empty modifier_option_ids are different requests: absent means
// "apply the menu's defaults", empty means "the customer declined every
// option". Go collapses both to a nil slice unless the field is a pointer.
func TestAddDraftItemCommandDistinguishesAbsentFromEmpty(t *testing.T) {
	var absent AddDraftItemCommand
	require.NoError(t, json.Unmarshal([]byte(`{"menu_item_id":"`+uuid.Nil.String()+`"}`), &absent))
	assert.Nil(t, absent.ModifierOptionIDs, "absent must stay nil")

	var empty AddDraftItemCommand
	require.NoError(t, json.Unmarshal(
		[]byte(`{"menu_item_id":"`+uuid.Nil.String()+`","modifier_option_ids":[]}`), &empty))
	require.NotNil(t, empty.ModifierOptionIDs, "empty must be a non-nil pointer to an empty slice")
	assert.Empty(t, *empty.ModifierOptionIDs)
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/sales/ -run 'TestServiceSessionResponse|TestEmptyCollections|TestCustomerIdentity|TestDraftItemResponse|TestAddDraftItemCommand' -v`
Expected: FAIL — `undefined: ServiceSessionResponse`.

- [ ] **Step 4: Write the DTOs**

Create `internal/sales/dto.go`:

```go
package sales

import (
	"time"

	"github.com/google/uuid"
)

// ---------- Commands ----------

// StartTakeawaySessionCommand opens an anonymous Takeaway Service Session.
type StartTakeawaySessionCommand struct {
	RequestID uuid.UUID `json:"request_id"`
}

// StartDineInSessionCommand opens a Dine-in Session against one or more Tables.
type StartDineInSessionCommand struct {
	RequestID uuid.UUID   `json:"request_id"`
	TableIDs  []uuid.UUID `json:"table_ids"`
}

// SetSessionTablesCommand replaces a Dine-in Session's current Table set.
// An empty TableIDs releases every assignment and is permitted.
type SetSessionTablesCommand struct {
	RequestID        uuid.UUID   `json:"request_id"`
	ServiceSessionID uuid.UUID   `json:"-"`
	TableIDs         []uuid.UUID `json:"table_ids"`
}

// AddDraftItemCommand adds one unit of a configured Menu Item to the draft.
//
// ModifierOptionIDs is a pointer because absent and empty mean different
// things: absent applies the menu's default options, empty applies none.
// A non-pointer slice would collapse both to nil.
type AddDraftItemCommand struct {
	RequestID        uuid.UUID    `json:"request_id"`
	ServiceSessionID uuid.UUID    `json:"-"`
	MenuItemID       uuid.UUID    `json:"menu_item_id"`
	SizeID           *uuid.UUID   `json:"size_id"`
	PreparationNote  *string      `json:"preparation_note"`
	ModifierOptionIDs *[]uuid.UUID `json:"modifier_option_ids"`
}

// SetDraftItemQuantityCommand sets an absolute quantity. Zero is rejected;
// removal is its own command.
type SetDraftItemQuantityCommand struct {
	RequestID        uuid.UUID `json:"request_id"`
	ServiceSessionID uuid.UUID `json:"-"`
	DraftItemID      uuid.UUID `json:"-"`
	Quantity         *int32    `json:"quantity"`
}

// SetDraftItemSizeCommand sets or clears the Size. A nil SizeID clears it.
type SetDraftItemSizeCommand struct {
	RequestID        uuid.UUID  `json:"request_id"`
	ServiceSessionID uuid.UUID  `json:"-"`
	DraftItemID      uuid.UUID  `json:"-"`
	SizeID           *uuid.UUID `json:"size_id"`
}

// SetDraftItemNoteCommand sets or clears the Preparation Note.
type SetDraftItemNoteCommand struct {
	RequestID        uuid.UUID `json:"request_id"`
	ServiceSessionID uuid.UUID `json:"-"`
	DraftItemID      uuid.UUID `json:"-"`
	PreparationNote  *string   `json:"preparation_note"`
}

// SetDraftItemModifiersCommand replaces the selected options. Unlike the add
// command this list is required; an empty list is taken literally and no
// defaults are applied.
type SetDraftItemModifiersCommand struct {
	RequestID         uuid.UUID   `json:"request_id"`
	ServiceSessionID  uuid.UUID   `json:"-"`
	DraftItemID       uuid.UUID   `json:"-"`
	ModifierOptionIDs []uuid.UUID `json:"modifier_option_ids"`
}

// RemoveDraftItemCommand removes a draft item outright.
type RemoveDraftItemCommand struct {
	RequestID        uuid.UUID `json:"request_id"`
	ServiceSessionID uuid.UUID `json:"-"`
	DraftItemID      uuid.UUID `json:"-"`
}

// ---------- Responses ----------

// SessionTableResponse is a Table currently assigned to a Service Session.
type SessionTableResponse struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// SelectedModifierOptionResponse is one chosen Modifier Option on a draft item.
type SelectedModifierOptionResponse struct {
	ID           uuid.UUID `json:"id"`
	GroupID      uuid.UUID `json:"group_id"`
	GroupName    string    `json:"group_name"`
	Name         string    `json:"name"`
	SurchargeVND int64     `json:"surcharge_vnd"`
}

// DraftItemResponse is one line of the Order Draft.
//
// PriceVND is nil when the Menu Item is priced through a required Size that
// has not been chosen yet. A draft tolerates that incompleteness; Commit does
// not.
type DraftItemResponse struct {
	ID                      uuid.UUID                        `json:"id"`
	MenuItemID              uuid.UUID                        `json:"menu_item_id"`
	Name                    string                           `json:"name"`
	PriceVND                *int64                           `json:"price_vnd"`
	SizeID                  *uuid.UUID                       `json:"size_id"`
	SizeName                *string                          `json:"size_name"`
	Quantity                int32                            `json:"quantity"`
	Available               bool                             `json:"available"`
	PreparationNote         *string                          `json:"preparation_note"`
	SelectedModifierOptions []SelectedModifierOptionResponse `json:"selected_modifier_options"`
}

// OrderDraftResponse is the editable Order Draft.
type OrderDraftResponse struct {
	ID    uuid.UUID           `json:"id"`
	State string              `json:"state"`
	Items []DraftItemResponse `json:"items"`
}

// ServiceSessionResponse is the one projection every Sales operation returns.
//
// It ships in its final shape from 5A. Checks, Orders, and PreparationUnits
// are always present and empty until 5B, 5C, and 5D fill them, so the contract
// never breaks. Preparation alerts and corrections are Phase 6 concerns and are
// omitted entirely rather than stubbed.
//
// CustomerIdentityID is a constant null: every opening-day Service Session is
// anonymous, and the field exists so a future Loyalty Program does not reshape
// the contract.
//
// SalesShiftID has no canonical counterpart. ADR-011 makes the Service Number
// unique only within its Shift, so a client storing or printing one needs the
// Shift alongside it.
type ServiceSessionResponse struct {
	ID                 uuid.UUID              `json:"id"`
	ServiceNumber      string                 `json:"service_number"`
	ServiceMode        string                 `json:"service_mode"`
	State              string                 `json:"state"`
	CustomerIdentityID *uuid.UUID             `json:"customer_identity_id"`
	SalesShiftID       uuid.UUID              `json:"sales_shift_id"`
	Tables             []SessionTableResponse `json:"tables"`
	CreatedAt          time.Time              `json:"created_at"`
	Draft              *OrderDraftResponse    `json:"draft"`

	// Filled by 5B and 5C.
	Checks []struct{} `json:"checks"`
	// Filled by 5D.
	Orders []struct{} `json:"orders"`
	// Filled by 5D.
	PreparationUnits []struct{} `json:"preparation_units"`
}
```

> **Note for the implementer:** `emit_empty_slices` only affects sqlc-generated
> types, not these hand-written ones. A nil `[]SessionTableResponse` marshals
> as `null`. `LoadServiceSession` in Step 5 must initialize every slice with
> `make(...)`, and `TestEmptyCollectionsSerializeAsArrays` will fail on a
> zero-valued struct unless you give these fields non-nil defaults. Resolve
> this by constructing the response through a `newServiceSessionResponse()`
> constructor that initializes all four slices, and change the test to call it.
> Do not add `omitempty`: that would drop the field entirely.

- [ ] **Step 5: Write the projection**

Create `internal/sales/projection.go`:

```go
package sales

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

// newServiceSessionResponse returns a response with every collection
// initialized, so the JSON contract never emits null for an array.
func newServiceSessionResponse() ServiceSessionResponse {
	return ServiceSessionResponse{
		Tables:           make([]SessionTableResponse, 0),
		Checks:           make([]struct{}, 0),
		Orders:           make([]struct{}, 0),
		PreparationUnits: make([]struct{}, 0),
	}
}

// LoadServiceSession assembles the full projection for one Service Session.
//
// Every mutation returns this, so a client never needs a follow-up read and a
// composition merge that changed an id the client was holding is immediately
// visible. Callers must run it inside the same transaction as their mutation.
func LoadServiceSession(ctx context.Context, q *sqlc.Queries, sessionID uuid.UUID) (
	ServiceSessionResponse, error,
) {
	out := newServiceSessionResponse()

	session, err := q.GetServiceSession(ctx, sessionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return out, fmt.Errorf("%w: %s", ErrServiceSessionNotFound, sessionID)
		}
		return out, fmt.Errorf("load service session: %w", err)
	}

	out.ID = session.ID
	out.ServiceNumber = session.ServiceNumber
	out.ServiceMode = session.ServiceMode
	out.State = session.State
	out.SalesShiftID = session.SalesShiftID
	out.CreatedAt = session.CreatedAt

	tableRows, err := q.ListServiceSessionTables(ctx, sessionID)
	if err != nil {
		return out, fmt.Errorf("load session tables: %w", err)
	}
	for _, row := range tableRows {
		out.Tables = append(out.Tables, SessionTableResponse{ID: row.ID, Name: row.Name})
	}

	draft, err := q.GetEditableDraft(ctx, sessionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// 5A always creates a draft with its Session, so this is only
			// reachable once 5B can commit one without a successor.
			return out, nil
		}
		return out, fmt.Errorf("load editable draft: %w", err)
	}

	items, err := loadDraftItems(ctx, q, draft.ID)
	if err != nil {
		return out, err
	}
	out.Draft = &OrderDraftResponse{ID: draft.ID, State: draft.State, Items: items}

	return out, nil
}

// loadDraftItems reads the draft's items and their selected options in two
// queries rather than one per item.
func loadDraftItems(ctx context.Context, q *sqlc.Queries, draftID uuid.UUID) (
	[]DraftItemResponse, error,
) {
	itemRows, err := q.ListDraftItems(ctx, draftID)
	if err != nil {
		return nil, fmt.Errorf("load draft items: %w", err)
	}

	optionRows, err := q.ListDraftItemModifierOptions(ctx, draftID)
	if err != nil {
		return nil, fmt.Errorf("load draft item modifier options: %w", err)
	}

	// The query already orders by group name then option name, so appending in
	// scan order preserves the projection's ordering per item.
	byItem := make(map[uuid.UUID][]SelectedModifierOptionResponse, len(itemRows))
	for _, row := range optionRows {
		byItem[row.OrderDraftItemID] = append(byItem[row.OrderDraftItemID],
			SelectedModifierOptionResponse{
				ID:           row.OptionID,
				GroupID:      row.GroupID,
				GroupName:    row.GroupName,
				Name:         row.OptionName,
				SurchargeVND: row.SurchargeVnd,
			})
	}

	items := make([]DraftItemResponse, 0, len(itemRows))
	for _, row := range itemRows {
		options := byItem[row.ID]
		if options == nil {
			options = make([]SelectedModifierOptionResponse, 0)
		}
		item := DraftItemResponse{
			ID:                      row.ID,
			MenuItemID:              row.MenuItemID,
			Name:                    row.MenuItemName,
			Quantity:                row.Quantity,
			Available:               row.Available,
			SelectedModifierOptions: options,
		}
		if row.PriceVnd.Valid {
			v := row.PriceVnd.Int64
			item.PriceVND = &v
		}
		if row.SizeID.Valid {
			id := row.SizeID.UUID
			item.SizeID = &id
		}
		if row.SizeName.Valid {
			name := row.SizeName.String
			item.SizeName = &name
		}
		if row.PreparationNote.Valid {
			note := row.PreparationNote.String
			item.PreparationNote = &note
		}
		items = append(items, item)
	}
	return items, nil
}
```

> **Note for the implementer:** the generated row field names above
> (`PriceVnd`, `SizeID`, `SurchargeVnd`, `OrderDraftItemID`) are sqlc's
> conventions, but confirm each against `internal/database/sqlc/sales.sql.go`
> after `make sqlc` and fix any mismatch. In particular, sqlc's nullable types
> depend on the driver: with `sql_package: "database/sql"` a nullable UUID is
> `uuid.NullUUID` and a nullable BIGINT is `sql.NullInt64`. The `COALESCE`
> expression's inferred nullability may need an explicit cast in the query
> (`COALESCE(s.price_vnd, mi.price_vnd)::bigint`) to produce the type you want.

- [ ] **Step 6: Write the read handler, routes, and HTTP layer**

Create `internal/sales/reads.go`:

```go
package sales

import (
	"context"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

// GetServiceSessionHandler serves the single Service Session read.
type GetServiceSessionHandler struct{ runner *Runner }

// NewGetServiceSessionHandler creates a new GetServiceSessionHandler.
func NewGetServiceSessionHandler(runner *Runner) *GetServiceSessionHandler {
	return &GetServiceSessionHandler{runner: runner}
}

// Handle returns one Service Session with its Tables and Order Draft.
//
// The read carries no open-Shift requirement: staff must be able to inspect a
// Session after its Shift closes.
func (h *GetServiceSessionHandler) Handle(ctx context.Context, actor Actor, sessionID uuid.UUID) (
	ServiceSessionResponse, error,
) {
	return ExecuteRead(ctx, h.runner, actor, OpGetServiceSession, CapSalesOperate,
		func(q *sqlc.Queries) (ServiceSessionResponse, error) {
			return LoadServiceSession(ctx, q, sessionID)
		})
}
```

Create `internal/sales/http.go` with the helpers copied from `internal/shift/http.go` (`getActor`, `parseUUIDParam`, `bindBody`, `checkRequestID`, `sendResult`, `sendError`) and this handler:

```go
// handleGetServiceSession returns one Service Session.
//
//	@Summary		Get a Service Session
//	@Description	Returns one Service Session with its current Tables and Order Draft. checks is filled by Phase 5B, orders and preparation_units by Phase 5D; each is an empty array until then.
//	@Tags			sales
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string	true	"Service Session ID"
//	@Success		200	{object}	response.APIResponse{data=ServiceSessionResponse}
//	@Failure		401	{object}	response.APIResponse
//	@Failure		403	{object}	response.APIResponse
//	@Failure		404	{object}	response.APIResponse
//	@Router			/sales/service-sessions/{id} [get]
func (s *Slices) handleGetServiceSession(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	sessionID, err := parseUUIDParam(c, "id")
	if err != nil {
		return sendError(c, err)
	}
	result, err := s.GetServiceSession.Handle(c.Request().Context(), actor, sessionID)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, http.StatusOK, result)
}
```

Create `internal/sales/routes.go`:

```go
package sales

import (
	"database/sql"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/labstack/echo/v4"
)

// Slices aggregates the Sales handlers.
type Slices struct {
	Runner *Runner

	GetServiceSession *GetServiceSessionHandler
}

// NewSlices wires every Sales handler onto a shared Runner.
func NewSlices(db *sql.DB, queries *sqlc.Queries) *Slices {
	runner := NewRunner(db, queries)
	return &Slices{
		Runner:            runner,
		GetServiceSession: NewGetServiceSessionHandler(runner),
	}
}

// RegisterRoutes mounts the Sales routes under /sales on the provided group.
//
// Routes are mounted directly on v1 rather than a /sales sub-group carrying
// RequireAuth, because any echo.Group holding group-level middleware also
// auto-registers two echo_route_not_found catch-all routes, which would make
// the router expose more than the Sales routes. This matches the reasoning
// already recorded in internal/tables/routes.go and internal/shift/routes.go.
func (s *Slices) RegisterRoutes(v1 *echo.Group, authn *auth.Middleware) {
	v1.GET("/sales/service-sessions/:id", s.handleGetServiceSession,
		authn.RequireAuth(), authn.RequireCapability(CapSalesOperate))
}
```

Modify `cmd/api/main.go`: alongside the existing slice wiring, add

```go
salesSlices := sales.NewSlices(db, queries)
salesSlices.RegisterRoutes(v1, authSlices.Middleware)
```

> **Note for the implementer:** match the exact variable names already used in
> `cmd/api/main.go` for the database handle, queries, `v1` group, and auth
> slices. Read the surrounding lines before editing.

- [ ] **Step 7: Write the failing projection integration test**

Create `internal/sales/projection_integration_test.go`:

```go
//go:build integration

package sales_test

import (
	"context"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetServiceSessionReturnsSessionWithEmptyDraft(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	fx := seedSalesFixture(t, q)

	got, err := sales.NewGetServiceSessionHandler(runner).
		Handle(context.Background(), actor, fx.ServiceSessionID)
	require.NoError(t, err)

	assert.Equal(t, fx.ServiceSessionID, got.ID)
	assert.Equal(t, "S00001", got.ServiceNumber)
	assert.Equal(t, sales.ModeTakeaway, got.ServiceMode)
	assert.Equal(t, sales.StateActive, got.State)
	assert.Equal(t, fx.SalesShiftID, got.SalesShiftID)
	assert.Nil(t, got.CustomerIdentityID)
	assert.Empty(t, got.Tables)
	require.NotNil(t, got.Draft)
	assert.Equal(t, sales.DraftStateEditable, got.Draft.State)
	assert.Empty(t, got.Draft.Items)
	assert.Empty(t, got.Checks)
	assert.Empty(t, got.Orders)
	assert.Empty(t, got.PreparationUnits)
}

func TestGetServiceSessionNotFound(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})

	_, err := sales.NewGetServiceSessionHandler(runner).
		Handle(context.Background(), actor, uuid.New())
	require.ErrorIs(t, err, sales.ErrServiceSessionNotFound)
}

// A BARISTA holds no sales.operate and is denied on the read path, with the
// denial audited.
func TestGetServiceSessionDeniesBarista(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"BARISTA"})
	fx := seedSalesFixture(t, q)

	_, err := sales.NewGetServiceSessionHandler(runner).
		Handle(context.Background(), actor, fx.ServiceSessionID)
	require.ErrorIs(t, err, sales.ErrForbidden)

	assertAuditEvent(t, db, sales.EventAuthorizationDenied, 1)
}

// The projection reads a draft item's price and availability live from
// Catalog, so a Size price overrides the Item price.
func TestDraftItemProjectionUsesSizePriceWhenSized(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)
	ctx := context.Background()

	actor := seedActor(t, q, []string{"CASHIER"})
	fx := seedSalesFixture(t, q)
	sizeID := seedSize(t, db, fx.MenuItemID, "Lớn", 32000)

	_, err := db.ExecContext(ctx, `
		INSERT INTO order_draft_items (order_draft_id, menu_item_id, size_id, modifier_key)
		VALUES ($1, $2, $3, '')`, fx.DraftID, fx.MenuItemID, sizeID)
	require.NoError(t, err)

	got, err := sales.NewGetServiceSessionHandler(runner).
		Handle(ctx, actor, fx.ServiceSessionID)
	require.NoError(t, err)

	require.Len(t, got.Draft.Items, 1)
	item := got.Draft.Items[0]
	require.NotNil(t, item.PriceVND)
	assert.Equal(t, int64(32000), *item.PriceVND, "the Size price must override the Item price")
	require.NotNil(t, item.SizeName)
	assert.Equal(t, "Lớn", *item.SizeName)
	assert.True(t, item.Available)
}

// An item that became unavailable after being added stays in the draft and is
// projected as unavailable. The draft is a live proposal, not a snapshot.
func TestDraftItemProjectionReportsLiveAvailability(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)
	ctx := context.Background()

	actor := seedActor(t, q, []string{"CASHIER"})
	fx := seedSalesFixture(t, q)

	_, err := db.ExecContext(ctx, `
		INSERT INTO order_draft_items (order_draft_id, menu_item_id, modifier_key)
		VALUES ($1, $2, '')`, fx.DraftID, fx.MenuItemID)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `UPDATE menu_items SET available = false WHERE id = $1`,
		fx.MenuItemID)
	require.NoError(t, err)

	got, err := sales.NewGetServiceSessionHandler(runner).
		Handle(ctx, actor, fx.ServiceSessionID)
	require.NoError(t, err)

	require.Len(t, got.Draft.Items, 1)
	assert.False(t, got.Draft.Items[0].Available)
}
```

Append the `seedSize` helper to the harness:

```go
// seedSize adds a Size to a Menu Item and returns its id.
func seedSize(t *testing.T, db *sql.DB, menuItemID uuid.UUID, name string, priceVND int64) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	require.NoError(t, db.QueryRow(`
		INSERT INTO menu_item_sizes (menu_item_id, name, normalized_name, price_vnd, available)
		VALUES ($1, $2, lower($2), $3, true) RETURNING id`,
		menuItemID, name, priceVND).Scan(&id))
	return id
}
```

- [ ] **Step 8: Run the tests to verify they pass**

Run: `go test ./internal/sales/ -v`
Run: `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -p 1 -race -tags=integration ./internal/sales/ -v`
Expected: PASS.

Run: `go build ./...`
Expected: no errors, confirming `cmd/api/main.go` wiring compiles.

- [ ] **Step 9: Commit**

```bash
make fmt
git add internal/sales/ sql/queries/sales.sql internal/database/sqlc/ cmd/api/main.go
git commit -m "feat(sales): add Service Session projection and read

Ships the projection in its final 5A shape with checks, orders, and
preparation_units present and empty, price and availability read live from
Catalog, and the first Sales route wired into the API.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

## Task 6: Service Number Allocation And Open Takeaway Session

**Files:**
- Create: `internal/sales/service_number.go`, `internal/sales/session_start.go`
- Modify: `sql/queries/sales.sql`, `internal/sales/routes.go`, `internal/sales/http.go`
- Test: `internal/sales/session_start_integration_test.go`

**Interfaces:**
- Consumes: `ExecuteMutation`, `LoadServiceSession`, `FormatServiceNumber`, `IDToLockKey`, `Runner.AdvisoryLock`.
- Produces:
  - `func allocateServiceNumber(ctx context.Context, r *Runner, q *sqlc.Queries, shiftID uuid.UUID) (int32, string, error)`
  - `func requireOpenSalesShift(ctx context.Context, q *sqlc.Queries) (uuid.UUID, error)`
  - `func insertSessionWithDraft(ctx, q, actor, mode string, shiftID uuid.UUID, seq int32, number string) (uuid.UUID, error)`
  - `type StartTakeawaySessionHandler`, `func NewStartTakeawaySessionHandler(runner *Runner) *StartTakeawaySessionHandler`, `func (h *StartTakeawaySessionHandler) Handle(ctx, actor, cmd StartTakeawaySessionCommand) (int, ServiceSessionResponse, error)`

- [ ] **Step 1: Add the queries**

Append to `sql/queries/sales.sql`:

```sql
-- name: GetOpenSalesShiftID :one
-- Sales reads the Shift-owned table through its own query rather than
-- importing internal/shift, per ADR-006's precedent.
SELECT id FROM sales_shifts WHERE state = 'OPEN' LIMIT 1;

-- name: GetNextServiceSequence :one
-- Callers MUST hold the advisory lock on the Sales Shift before running this.
-- Without it two concurrent opens read the same maximum and one loses to the
-- unique index.
SELECT COALESCE(MAX(sequence), 0)::int + 1 AS next_sequence
FROM service_sessions
WHERE sales_shift_id = $1;

-- name: InsertServiceSession :one
INSERT INTO service_sessions
    (service_number, sequence, service_mode, state,
     created_by_staff_identity_id, sales_shift_id)
VALUES ($1, $2, $3, 'ACTIVE', $4, $5)
RETURNING id, service_number, sequence, service_mode, state, sales_shift_id, created_at;

-- name: InsertOrderDraft :one
INSERT INTO order_drafts (service_session_id) VALUES ($1)
RETURNING id, service_session_id, state, created_at;
```

Run: `make sqlc`

- [ ] **Step 2: Write the failing integration test**

Create `internal/sales/session_start_integration_test.go`:

```go
//go:build integration

package sales_test

import (
	"context"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startTakeaway is the shorthand every start test uses.
func startTakeaway(t *testing.T, runner *sales.Runner, actor sales.Actor, requestID uuid.UUID) (
	int, sales.ServiceSessionResponse, error,
) {
	t.Helper()
	return sales.NewStartTakeawaySessionHandler(runner).Handle(
		context.Background(), actor,
		sales.StartTakeawaySessionCommand{RequestID: requestID})
}

func TestStartTakeawaySessionAllocatesSequentialNumbers(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)

	status, first, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)
	assert.Equal(t, 201, status)
	assert.Equal(t, "S00001", first.ServiceNumber)
	assert.Equal(t, sales.ModeTakeaway, first.ServiceMode)
	assert.Equal(t, sales.StateActive, first.State)
	require.NotNil(t, first.Draft, "opening a Session must create its editable draft")
	assert.Empty(t, first.Draft.Items)
	assert.Empty(t, first.Tables)

	_, second, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)
	assert.Equal(t, "S00002", second.ServiceNumber)

	assertAuditEvent(t, db, sales.EventServiceSessionStarted, 2)
}

func TestStartTakeawaySessionRequiresOpenShift(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	// Deliberately no Shift.

	_, _, err := startTakeaway(t, runner, actor, uuid.New())
	require.ErrorIs(t, err, sales.ErrOpenShiftRequired)

	var sessions int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM service_sessions`).Scan(&sessions))
	assert.Equal(t, 0, sessions, "a rejected open must create nothing")
}

// Service Numbers restart per Shift, which is the whole point of ADR-011.
func TestServiceNumberRestartsInANewShift(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)
	ctx := context.Background()

	actor := seedActor(t, q, []string{"CASHIER"})
	firstShift := seedOpenShift(t, q, actor.StaffID)

	_, first, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)
	assert.Equal(t, "S00001", first.ServiceNumber)

	// Close the first Shift and open a second. Phase 4 ships no close command,
	// so the test drives the state directly.
	_, err = db.ExecContext(ctx, `UPDATE sales_shifts SET state = 'CLOSED' WHERE id = $1`, firstShift)
	require.NoError(t, err)
	secondShift := seedOpenShift(t, q, actor.StaffID)

	_, second, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)
	assert.Equal(t, "S00001", second.ServiceNumber, "numbering restarts within each Shift")
	assert.Equal(t, secondShift, second.SalesShiftID)
}

// The advisory lock serializes allocation, so concurrent opens produce a
// gapless sequence with no duplicates and no retry loop.
func TestConcurrentStartsAllocateGaplessSequence(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)

	const n = 8
	start := make(chan struct{})
	numbers := make(chan string, n)
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			<-start
			_, resp, err := startTakeaway(t, runner, actor, uuid.New())
			errs <- err
			numbers <- resp.ServiceNumber
		}()
	}
	close(start)

	seen := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		require.NoError(t, <-errs)
		num := <-numbers
		assert.False(t, seen[num], "duplicate Service Number %s", num)
		seen[num] = true
	}
	for i := 1; i <= n; i++ {
		expected, err := sales.FormatServiceNumber(int32(i))
		require.NoError(t, err)
		assert.True(t, seen[expected], "sequence must be gapless; %s is missing", expected)
	}
}

func TestStartTakeawaySessionIsIdempotent(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)

	requestID := uuid.New()
	_, first, err := startTakeaway(t, runner, actor, requestID)
	require.NoError(t, err)

	_, replay, err := startTakeaway(t, runner, actor, requestID)
	require.NoError(t, err)
	assert.Equal(t, first.ID, replay.ID, "a replay must not open a second Session")

	var sessions int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM service_sessions`).Scan(&sessions))
	assert.Equal(t, 1, sessions)
}

// A replay must survive its Shift closing: the open-Shift precondition guards
// new work only, and runs after the idempotency claim.
func TestReplaySurvivesShiftClosing(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	shiftID := seedOpenShift(t, q, actor.StaffID)

	requestID := uuid.New()
	_, first, err := startTakeaway(t, runner, actor, requestID)
	require.NoError(t, err)

	_, err = db.Exec(`UPDATE sales_shifts SET state = 'CLOSED' WHERE id = $1`, shiftID)
	require.NoError(t, err)

	_, replay, err := startTakeaway(t, runner, actor, requestID)
	require.NoError(t, err, "a replay must return its stored result after the Shift closed")
	assert.Equal(t, first.ID, replay.ID)
}
```

Append the `seedOpenShift` helper to the harness:

```go
// seedOpenShift opens a Sales Shift and returns its id.
func seedOpenShift(t *testing.T, q *sqlc.Queries, openedBy uuid.UUID) uuid.UUID {
	t.Helper()
	shift, err := q.OpenSalesShift(context.Background(), sqlc.OpenSalesShiftParams{
		OpenedByStaffIdentityID: openedBy,
		OpeningFloatVnd:         100000,
	})
	require.NoError(t, err)
	return shift.ID
}
```

Remove the Shift creation from `seedSalesFixture` and have it call `seedOpenShift` instead, so there is one way to open a Shift in tests.

- [ ] **Step 3: Run the tests to verify they fail**

Run: `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -p 1 -tags=integration ./internal/sales/ -run 'TestStart|TestServiceNumber|TestConcurrentStarts|TestReplaySurvives' -v`
Expected: FAIL — `undefined: sales.NewStartTakeawaySessionHandler`.

- [ ] **Step 4: Write the Service Number allocator**

Create `internal/sales/service_number.go`:

```go
package sales

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

// requireOpenSalesShift returns the open Sales Shift's id, or
// ErrOpenShiftRequired when none is open.
//
// It runs inside the mutation body, after the idempotency claim, so that a
// replay of a request that succeeded during a Shift still returns its stored
// result once that Shift has closed. The precondition guards new work only.
func requireOpenSalesShift(ctx context.Context, q *sqlc.Queries) (uuid.UUID, error) {
	id, err := q.GetOpenSalesShiftID(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return uuid.Nil, ErrOpenShiftRequired
		}
		return uuid.Nil, fmt.Errorf("load open sales shift: %w", err)
	}
	return id, nil
}

// allocateServiceNumber reserves the next Service Number within a Sales Shift.
//
// The canonical implementation derived the number from six hexadecimal
// characters of the Session UUID and retried on collision up to five times,
// against a permanently global unique index. That has an unrecoverable
// exhaustion mode in the system's most frequent operation. ADR-011 replaces it
// with a Shift-scoped sequence serialized by a transaction-scoped advisory
// lock, so there is no retry loop and no exhaustion.
//
// The advisory lock is mandatory: GetNextServiceSequence reads a maximum, and
// two concurrent readers without the lock compute the same next value.
func allocateServiceNumber(ctx context.Context, r *Runner, q *sqlc.Queries, shiftID uuid.UUID) (
	int32, string, error,
) {
	if err := r.AdvisoryLock(ctx, q, IDToLockKey(shiftID)); err != nil {
		return 0, "", err
	}
	next, err := q.GetNextServiceSequence(ctx, shiftID)
	if err != nil {
		return 0, "", fmt.Errorf("compute next service sequence: %w", err)
	}
	number, err := FormatServiceNumber(next)
	if err != nil {
		return 0, "", err
	}
	return next, number, nil
}

// insertSessionWithDraft creates a Service Session and its editable Order
// Draft. Opening a Session always creates its draft, so the projection's draft
// is never null in 5A.
func insertSessionWithDraft(ctx context.Context, q *sqlc.Queries, actor Actor,
	mode string, shiftID uuid.UUID, sequence int32, number string,
) (uuid.UUID, error) {
	session, err := q.InsertServiceSession(ctx, sqlc.InsertServiceSessionParams{
		ServiceNumber:            number,
		Sequence:                 sequence,
		ServiceMode:              mode,
		CreatedByStaffIdentityID: actor.StaffID,
		SalesShiftID:             shiftID,
	})
	if err != nil {
		return uuid.Nil, MapDBError(err)
	}
	if _, err := q.InsertOrderDraft(ctx, session.ID); err != nil {
		return uuid.Nil, fmt.Errorf("create order draft: %w", err)
	}
	return session.ID, nil
}
```

- [ ] **Step 5: Write the start handler**

Create `internal/sales/session_start.go`:

```go
package sales

import (
	"context"

	"github.com/google/uuid"
)

// startTakeawayFingerprint carries no business input: a Takeaway Session has
// nothing to configure. The request_id alone distinguishes two opens, which is
// correct — two deliberate opens are two Sessions.
type startTakeawayFingerprint struct {
	Mode string `json:"mode"`
}

type serviceSessionStartedAudit struct {
	ServiceSessionID uuid.UUID `json:"service_session_id"`
	ServiceNumber    string    `json:"service_number"`
	ServiceMode      string    `json:"service_mode"`
	SalesShiftID     uuid.UUID `json:"sales_shift_id"`
}

// StartTakeawaySessionHandler opens an anonymous Takeaway Service Session.
type StartTakeawaySessionHandler struct{ runner *Runner }

// NewStartTakeawaySessionHandler creates a new StartTakeawaySessionHandler.
func NewStartTakeawaySessionHandler(runner *Runner) *StartTakeawaySessionHandler {
	return &StartTakeawaySessionHandler{runner: runner}
}

// Handle opens a Takeaway Service Session with its editable Order Draft.
func (h *StartTakeawaySessionHandler) Handle(ctx context.Context, actor Actor,
	cmd StartTakeawaySessionCommand,
) (int, ServiceSessionResponse, error) {
	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpStartTakeawaySession,
		Fingerprint: startTakeawayFingerprint{Mode: ModeTakeaway},
		Required:    []string{CapSalesOperate},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, ServiceSessionResponse, AuditRecord, error) {
			shiftID, err := requireOpenSalesShift(ctx, mc.Queries)
			if err != nil {
				return 0, ServiceSessionResponse{}, AuditRecord{}, err
			}

			sequence, number, err := allocateServiceNumber(ctx, h.runner, mc.Queries, shiftID)
			if err != nil {
				return 0, ServiceSessionResponse{}, AuditRecord{}, err
			}

			sessionID, err := insertSessionWithDraft(ctx, mc.Queries, actor,
				ModeTakeaway, shiftID, sequence, number)
			if err != nil {
				return 0, ServiceSessionResponse{}, AuditRecord{}, err
			}

			result, err := LoadServiceSession(ctx, mc.Queries, sessionID)
			if err != nil {
				return 0, ServiceSessionResponse{}, AuditRecord{}, err
			}

			return 201, result, AuditRecord{
				EventType: EventServiceSessionStarted,
				Details: serviceSessionStartedAudit{
					ServiceSessionID: sessionID,
					ServiceNumber:    number,
					ServiceMode:      ModeTakeaway,
					SalesShiftID:     shiftID,
				},
			}, nil
		})
}
```

- [ ] **Step 6: Add the route and HTTP handler**

Add to `Slices`: `StartTakeaway *StartTakeawaySessionHandler`, wired in `NewSlices`. Add to `RegisterRoutes`:

```go
v1.POST("/sales/service-sessions/takeaway", s.handleStartTakeaway,
	authn.RequireAuth(), authn.RequireCapability(CapSalesOperate))
```

Add to `http.go`:

```go
// handleStartTakeaway opens a Takeaway Service Session.
//
//	@Summary		Open a Takeaway Service Session
//	@Description	Opens an anonymous Takeaway Service Session with an empty editable Order Draft. Requires an open Sales Shift. The Service Number is sequential within that Shift (ADR-011).
//	@Tags			sales
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		StartTakeawaySessionCommand	true	"Request"
//	@Success		201		{object}	response.APIResponse{data=ServiceSessionResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Router			/sales/service-sessions/takeaway [post]
func (s *Slices) handleStartTakeaway(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	cmd, err := bindBody[StartTakeawaySessionCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(cmd.RequestID); err != nil {
		return sendError(c, err)
	}
	status, result, err := s.StartTakeaway.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, result)
}
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -p 1 -race -tags=integration ./internal/sales/ -v`
Expected: PASS, including `TestConcurrentStartsAllocateGaplessSequence`.

If the concurrency test reports duplicates, the advisory lock is being taken after the sequence read, or on the wrong key. Re-read Step 4.

- [ ] **Step 8: Commit**

```bash
make fmt
git add internal/sales/ sql/queries/sales.sql internal/database/sqlc/
git commit -m "feat(sales): open Takeaway Service Sessions with Shift-scoped numbers

Allocates Service Numbers sequentially within a Sales Shift under a
transaction-scoped advisory lock, replacing the canonical UUID-prefix retry
loop and its exhaustion failure mode (ADR-011). The open-Shift precondition
runs after the idempotency claim so a replay survives its Shift closing.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

## Task 7: Open Dine-in Session

**Files:**
- Modify: `internal/sales/session_start.go`, `internal/sales/routes.go`, `internal/sales/http.go`, `sql/queries/sales.sql`
- Create: `internal/sales/table_assignments.go`
- Test: `internal/sales/dine_in_integration_test.go`

**Interfaces:**
- Consumes: everything from Task 6.
- Produces:
  - `func lockAndValidateTables(ctx context.Context, q *sqlc.Queries, tableIDs []uuid.UUID) error`
  - `func assignTables(ctx, q, actor, sessionID uuid.UUID, tableIDs []uuid.UUID, startSequence int32) ([]tableAssignmentAudit, error)`
  - `type StartDineInSessionHandler`, `func NewStartDineInSessionHandler(runner *Runner) *StartDineInSessionHandler`, `func (h *StartDineInSessionHandler) Handle(ctx, actor, cmd StartDineInSessionCommand) (int, ServiceSessionResponse, error)`

- [ ] **Step 1: Add the queries**

Append to `sql/queries/sales.sql`:

```sql
-- name: LockTablesForAssignment :many
-- Locks the selected Tables in id order so two concurrent assignments over
-- overlapping sets cannot deadlock against each other. The caller must sort
-- the ids before calling.
SELECT id, name, available
FROM tables
WHERE id = ANY(sqlc.arg(table_ids)::uuid[])
ORDER BY id ASC
FOR UPDATE;

-- name: InsertTableAssignment :one
INSERT INTO table_assignments
    (table_id, service_session_id, assigned_by_staff_identity_id, sequence)
VALUES ($1, $2, $3, $4)
RETURNING id, table_id, service_session_id, sequence, assigned_at;

-- name: GetHighestAssignmentSequence :one
-- Includes released assignments, so a released sequence number is never
-- reused and the audit trail stays unambiguous.
SELECT COALESCE(MAX(sequence), -1)::int AS highest
FROM table_assignments
WHERE service_session_id = $1;
```

Run: `make sqlc`

- [ ] **Step 2: Write the failing integration test**

Create `internal/sales/dine_in_integration_test.go`:

```go
//go:build integration

package sales_test

import (
	"context"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func startDineIn(t *testing.T, runner *sales.Runner, actor sales.Actor,
	requestID uuid.UUID, tableIDs ...uuid.UUID,
) (int, sales.ServiceSessionResponse, error) {
	t.Helper()
	return sales.NewStartDineInSessionHandler(runner).Handle(
		context.Background(), actor,
		sales.StartDineInSessionCommand{RequestID: requestID, TableIDs: tableIDs})
}

func TestStartDineInAssignsTablesInSelectionOrder(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	t1 := seedTable(t, db, "Bàn 1")
	t2 := seedTable(t, db, "Bàn 2")

	// Pass the Tables in reverse id order to prove selection order, not id
	// order, drives the assignment sequence.
	status, resp, err := startDineIn(t, runner, actor, uuid.New(), t2, t1)
	require.NoError(t, err)
	assert.Equal(t, 201, status)
	assert.Equal(t, sales.ModeDineIn, resp.ServiceMode)
	require.Len(t, resp.Tables, 2)
	assert.Equal(t, t2, resp.Tables[0].ID, "tables are projected in assignment sequence")
	assert.Equal(t, t1, resp.Tables[1].ID)

	assertAuditEvent(t, db, sales.EventDineInServiceSessionStarted, 1)
	assertAuditEvent(t, db, sales.EventTableAssignmentCreated, 2)
}

func TestStartDineInRejectsBadSelections(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	t1 := seedTable(t, db, "Bàn 1")

	t.Run("empty selection", func(t *testing.T) {
		_, _, err := startDineIn(t, runner, actor, uuid.New())
		require.ErrorIs(t, err, sales.ErrTableSelectionRequired)
	})

	t.Run("duplicate id", func(t *testing.T) {
		_, _, err := startDineIn(t, runner, actor, uuid.New(), t1, t1)
		require.ErrorIs(t, err, sales.ErrTableSelectionDuplicate)
	})

	t.Run("unknown table", func(t *testing.T) {
		_, _, err := startDineIn(t, runner, actor, uuid.New(), uuid.New())
		require.ErrorIs(t, err, sales.ErrTableNotFound)
	})

	t.Run("unavailable table", func(t *testing.T) {
		unavailable := seedTable(t, db, "Bàn hỏng")
		_, err := db.Exec(`UPDATE tables SET available = false WHERE id = $1`, unavailable)
		require.NoError(t, err)

		_, _, err = startDineIn(t, runner, actor, uuid.New(), unavailable)
		require.ErrorIs(t, err, sales.ErrTableUnavailable)
	})

	var sessions int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM service_sessions`).Scan(&sessions))
	assert.Equal(t, 0, sessions, "every rejected open must create nothing")
}

// CONTEXT.md: a Table "may be associated with one or more active Service
// Sessions". Exclusive occupancy is not a rule of this system.
func TestTwoActiveSessionsMayShareATable(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	t1 := seedTable(t, db, "Bàn 1")

	_, first, err := startDineIn(t, runner, actor, uuid.New(), t1)
	require.NoError(t, err)

	_, second, err := startDineIn(t, runner, actor, uuid.New(), t1)
	require.NoError(t, err, "a second Session may occupy the same Table")
	assert.NotEqual(t, first.ID, second.ID)
}

// Table id order must not change the idempotency fingerprint: selection order
// is presentation, not a different request.
func TestDineInFingerprintIgnoresTableOrder(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	t1 := seedTable(t, db, "Bàn 1")
	t2 := seedTable(t, db, "Bàn 2")

	requestID := uuid.New()
	_, first, err := startDineIn(t, runner, actor, requestID, t1, t2)
	require.NoError(t, err)

	_, replay, err := startDineIn(t, runner, actor, requestID, t2, t1)
	require.NoError(t, err, "the same Table set in a different order is the same request")
	assert.Equal(t, first.ID, replay.ID)
}
```

Append the `seedTable` helper to the harness:

```go
// seedTable creates a Table and returns its id.
func seedTable(t *testing.T, db *sql.DB, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	require.NoError(t, db.QueryRow(`
		INSERT INTO tables (name, normalized_name, available)
		VALUES ($1, lower($1), true) RETURNING id`, name).Scan(&id))
	return id
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -p 1 -tags=integration ./internal/sales/ -run 'TestStartDineIn|TestTwoActive|TestDineInFingerprint' -v`
Expected: FAIL — `undefined: sales.NewStartDineInSessionHandler`.

- [ ] **Step 4: Write the table assignment helpers**

Create `internal/sales/table_assignments.go`:

```go
package sales

import (
	"context"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

// tableAssignmentAudit is one TABLE_ASSIGNMENT_CREATED or
// TABLE_ASSIGNMENT_RELEASED event's details. The names and shape match the
// canonical source, because internal/tables already reads these rows and its
// overview behavior must not shift.
type tableAssignmentAudit struct {
	TableAssignmentID uuid.UUID `json:"table_assignment_id"`
	TableID           uuid.UUID `json:"table_id"`
	ServiceSessionID  uuid.UUID `json:"service_session_id"`
}

// lockAndValidateTables locks every selected Table and rejects one that does
// not exist or is unavailable.
//
// A Table already occupied by another active Service Session is deliberately
// NOT rejected: CONTEXT.md allows a Table to carry more than one active
// Session, and internal/tables already models occupancy as a list.
//
// The lock is taken in sorted id order so two concurrent assignments over
// overlapping Table sets cannot deadlock.
func lockAndValidateTables(ctx context.Context, q *sqlc.Queries, tableIDs []uuid.UUID) error {
	if len(tableIDs) == 0 {
		return nil
	}
	rows, err := q.LockTablesForAssignment(ctx, SortedUUIDs(tableIDs))
	if err != nil {
		return fmt.Errorf("lock tables: %w", err)
	}

	byID := make(map[uuid.UUID]sqlc.LockTablesForAssignmentRow, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}
	// Iterate the caller's order so the error names the first Table the staff
	// member selected, not the lowest id.
	for _, id := range tableIDs {
		row, ok := byID[id]
		if !ok {
			return fmt.Errorf("%w: %s", ErrTableNotFound, id)
		}
		if !row.Available {
			return fmt.Errorf("%w: %s", ErrTableUnavailable, row.Name)
		}
	}
	return nil
}

// assignTables inserts one assignment per Table, numbering them from
// startSequence in the caller's selection order, and returns the audit details
// for each.
func assignTables(ctx context.Context, q *sqlc.Queries, actor Actor,
	sessionID uuid.UUID, tableIDs []uuid.UUID, startSequence int32,
) ([]tableAssignmentAudit, error) {
	out := make([]tableAssignmentAudit, 0, len(tableIDs))
	seq := startSequence
	for _, tableID := range tableIDs {
		row, err := q.InsertTableAssignment(ctx, sqlc.InsertTableAssignmentParams{
			TableID:                   tableID,
			ServiceSessionID:          sessionID,
			AssignedByStaffIdentityID: actor.StaffID,
			Sequence:                  seq,
		})
		if err != nil {
			return nil, fmt.Errorf("insert table assignment: %w", err)
		}
		out = append(out, tableAssignmentAudit{
			TableAssignmentID: row.ID,
			TableID:           tableID,
			ServiceSessionID:  sessionID,
		})
		seq++
	}
	return out, nil
}

// nextAssignmentSequence returns the next sequence for a Session, continuing
// past released assignments so a number is never reused.
func nextAssignmentSequence(ctx context.Context, q *sqlc.Queries, sessionID uuid.UUID) (int32, error) {
	highest, err := q.GetHighestAssignmentSequence(ctx, sessionID)
	if err != nil {
		return 0, fmt.Errorf("load highest assignment sequence: %w", err)
	}
	return highest + 1, nil
}
```

- [ ] **Step 5: Write the dine-in handler**

Append to `internal/sales/session_start.go`:

```go
// startDineInFingerprint sorts the Table ids: selection order does not change
// the target Table set, so it must not change the idempotency fingerprint. The
// unsorted order is kept for assignment sequence and audit events, which do
// care about the order staff selected Tables in.
type startDineInFingerprint struct {
	Mode     string      `json:"mode"`
	TableIDs []uuid.UUID `json:"table_ids"`
}

type dineInSessionStartedAudit struct {
	serviceSessionStartedAudit
	TableIDs []uuid.UUID `json:"table_ids"`
}

// StartDineInSessionHandler opens a Dine-in Service Session against Tables.
type StartDineInSessionHandler struct{ runner *Runner }

// NewStartDineInSessionHandler creates a new StartDineInSessionHandler.
func NewStartDineInSessionHandler(runner *Runner) *StartDineInSessionHandler {
	return &StartDineInSessionHandler{runner: runner}
}

// Handle opens a Dine-in Service Session and assigns its Tables.
//
// Selection validation happens before the transaction because it needs no
// database state and a bad selection should not consume a request_id.
func (h *StartDineInSessionHandler) Handle(ctx context.Context, actor Actor,
	cmd StartDineInSessionCommand,
) (int, ServiceSessionResponse, error) {
	if len(cmd.TableIDs) == 0 {
		return 0, ServiceSessionResponse{}, ErrTableSelectionRequired
	}
	if HasDuplicateUUIDs(cmd.TableIDs) {
		return 0, ServiceSessionResponse{}, ErrTableSelectionDuplicate
	}

	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpStartDineInSession,
		Fingerprint: startDineInFingerprint{
			Mode:     ModeDineIn,
			TableIDs: SortedUUIDs(cmd.TableIDs),
		},
		Required: []string{CapSalesOperate},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, ServiceSessionResponse, AuditRecord, error) {
			var zero ServiceSessionResponse

			shiftID, err := requireOpenSalesShift(ctx, mc.Queries)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			if err := lockAndValidateTables(ctx, mc.Queries, cmd.TableIDs); err != nil {
				return 0, zero, AuditRecord{}, err
			}

			sequence, number, err := allocateServiceNumber(ctx, h.runner, mc.Queries, shiftID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}

			sessionID, err := insertSessionWithDraft(ctx, mc.Queries, actor,
				ModeDineIn, shiftID, sequence, number)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}

			assignments, err := assignTables(ctx, mc.Queries, actor, sessionID, cmd.TableIDs, 0)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			if err := writeAssignmentAudits(ctx, mc.Queries, actor,
				EventTableAssignmentCreated, assignments); err != nil {
				return 0, zero, AuditRecord{}, err
			}

			result, err := LoadServiceSession(ctx, mc.Queries, sessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}

			return 201, result, AuditRecord{
				EventType: EventDineInServiceSessionStarted,
				Details: dineInSessionStartedAudit{
					serviceSessionStartedAudit: serviceSessionStartedAudit{
						ServiceSessionID: sessionID,
						ServiceNumber:    number,
						ServiceMode:      ModeDineIn,
						SalesShiftID:     shiftID,
					},
					TableIDs: cmd.TableIDs,
				},
			}, nil
		})
}
```

Add `writeAssignmentAudits` to `table_assignments.go`. The executor writes a single `AuditRecord`, but Table assignment produces one event per Table, so these are inserted directly:

```go
// writeAssignmentAudits inserts one audit event per Table assignment change.
// The executor's AuditRecord holds the single Session-level event; these are
// the per-Table events alongside it, inserted in the same transaction so an
// audit failure still rolls the whole mutation back.
func writeAssignmentAudits(ctx context.Context, q *sqlc.Queries, actor Actor,
	eventType string, entries []tableAssignmentAudit,
) error {
	for _, entry := range entries {
		details, err := json.Marshal(entry)
		if err != nil {
			return fmt.Errorf("marshal table assignment audit: %w", err)
		}
		if _, err := q.InsertAuditEvent(ctx, sqlc.InsertAuditEventParams{
			EventType:  eventType,
			ActorID:    uuid.NullUUID{UUID: actor.StaffID, Valid: true},
			SessionID:  uuid.NullUUID{UUID: actor.SessionID, Valid: true},
			Details:    details,
			OccurredAt: time.Now(),
		}); err != nil {
			return fmt.Errorf("insert %s audit event: %w", eventType, err)
		}
	}
	return nil
}
```

Add `encoding/json` and `time` to that file's imports.

- [ ] **Step 6: Add the route and HTTP handler**

Add `StartDineIn *StartDineInSessionHandler` to `Slices`, wire it in `NewSlices`, and register:

```go
v1.POST("/sales/service-sessions/dine-in", s.handleStartDineIn,
	authn.RequireAuth(), authn.RequireCapability(CapSalesOperate))
```

Add `handleStartDineIn` to `http.go`, identical in shape to `handleStartTakeaway` but binding `StartDineInSessionCommand`, with this annotation block:

```go
//	@Summary		Open a Dine-in Service Session
//	@Description	Opens a Dine-in Service Session assigned to one or more Tables. A Table may carry more than one active Service Session. Requires an open Sales Shift.
//	@Tags			sales
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		StartDineInSessionCommand	true	"Request"
//	@Success		201		{object}	response.APIResponse{data=ServiceSessionResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Router			/sales/service-sessions/dine-in [post]
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -p 1 -race -tags=integration ./internal/sales/ -v`
Expected: PASS.

- [ ] **Step 8: Verify the Tables slice still agrees**

Run: `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -p 1 -tags=integration ./internal/tables/ -v`
Expected: PASS unchanged. The Tables overview reads the rows Sales now writes.

- [ ] **Step 9: Commit**

```bash
make fmt
git add internal/sales/ sql/queries/sales.sql internal/database/sqlc/
git commit -m "feat(sales): open Dine-in Service Sessions with Table assignments

Locks Tables in sorted id order to avoid deadlock, validates existence and
availability without rejecting shared occupancy, and keeps selection order
for assignment sequence while sorting it out of the idempotency fingerprint.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

## Task 8: Set Service Session Tables

**Files:**
- Modify: `internal/sales/table_assignments.go`, `internal/sales/routes.go`, `internal/sales/http.go`, `sql/queries/sales.sql`
- Test: `internal/sales/set_tables_integration_test.go`

**Interfaces:**
- Consumes: `lockAndValidateTables`, `assignTables`, `nextAssignmentSequence`, `writeAssignmentAudits`.
- Produces: `type SetSessionTablesHandler`, `func NewSetSessionTablesHandler(runner *Runner) *SetSessionTablesHandler`, `func (h *SetSessionTablesHandler) Handle(ctx, actor, cmd SetSessionTablesCommand) (int, ServiceSessionResponse, error)`

- [ ] **Step 1: Add the queries**

Append to `sql/queries/sales.sql`:

```sql
-- name: LockServiceSessionForUpdate :one
SELECT id, service_mode, state, sales_shift_id
FROM service_sessions
WHERE id = $1
FOR UPDATE;

-- name: LockCurrentTableAssignments :many
SELECT id, table_id, sequence
FROM table_assignments
WHERE service_session_id = $1 AND released_at IS NULL
ORDER BY sequence ASC
FOR UPDATE;

-- name: ReleaseTableAssignment :exec
-- released_at and released_by_staff_identity_id must be set together; the
-- table_assignment_release_evidence_valid constraint from migration 000006
-- rejects one without the other.
UPDATE table_assignments
SET released_at = now(), released_by_staff_identity_id = $2
WHERE id = $1;
```

Run: `make sqlc`

- [ ] **Step 2: Write the failing integration test**

Create `internal/sales/set_tables_integration_test.go`:

```go
//go:build integration

package sales_test

import (
	"context"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setTables(t *testing.T, runner *sales.Runner, actor sales.Actor,
	sessionID uuid.UUID, requestID uuid.UUID, tableIDs ...uuid.UUID,
) (sales.ServiceSessionResponse, error) {
	t.Helper()
	if tableIDs == nil {
		tableIDs = []uuid.UUID{}
	}
	_, resp, err := sales.NewSetSessionTablesHandler(runner).Handle(
		context.Background(), actor, sales.SetSessionTablesCommand{
			RequestID:        requestID,
			ServiceSessionID: sessionID,
			TableIDs:         tableIDs,
		})
	return resp, err
}

func TestSetTablesAddsReleasesAndKeeps(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	t1 := seedTable(t, db, "Bàn 1")
	t2 := seedTable(t, db, "Bàn 2")
	t3 := seedTable(t, db, "Bàn 3")

	_, session, err := startDineIn(t, runner, actor, uuid.New(), t1, t2)
	require.NoError(t, err)

	// Keep t2, release t1, add t3.
	resp, err := setTables(t, runner, actor, session.ID, uuid.New(), t2, t3)
	require.NoError(t, err)

	ids := []uuid.UUID{}
	for _, tbl := range resp.Tables {
		ids = append(ids, tbl.ID)
	}
	assert.ElementsMatch(t, []uuid.UUID{t2, t3}, ids)

	// The released row keeps both pieces of release evidence.
	var releasedAt, releasedBy any
	require.NoError(t, db.QueryRow(`
		SELECT released_at, released_by_staff_identity_id
		FROM table_assignments
		WHERE service_session_id = $1 AND table_id = $2`,
		session.ID, t1).Scan(&releasedAt, &releasedBy))
	assert.NotNil(t, releasedAt)
	assert.NotNil(t, releasedBy)

	assertAuditEvent(t, db, sales.EventTableAssignmentReleased, 1)
	// Two from the dine-in open, one from the add.
	assertAuditEvent(t, db, sales.EventTableAssignmentCreated, 3)
}

// Sequence continues past released assignments, so a number is never reused.
func TestSetTablesNeverReusesASequence(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	t1 := seedTable(t, db, "Bàn 1")
	t2 := seedTable(t, db, "Bàn 2")

	_, session, err := startDineIn(t, runner, actor, uuid.New(), t1)
	require.NoError(t, err)

	_, err = setTables(t, runner, actor, session.ID, uuid.New(), t2)
	require.NoError(t, err)

	var sequences []int32
	rows, err := db.Query(`
		SELECT sequence FROM table_assignments
		WHERE service_session_id = $1 ORDER BY sequence ASC`, session.ID)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var seq int32
		require.NoError(t, rows.Scan(&seq))
		sequences = append(sequences, seq)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []int32{0, 1}, sequences)
}

// Releasing every Table is permitted. A Dine-in party that has left its table
// but not yet paid is a real situation; the canonical source rejects an empty
// selection only when opening a Session.
func TestSetTablesAcceptsAnEmptySelection(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	t1 := seedTable(t, db, "Bàn 1")

	_, session, err := startDineIn(t, runner, actor, uuid.New(), t1)
	require.NoError(t, err)

	resp, err := setTables(t, runner, actor, session.ID, uuid.New())
	require.NoError(t, err)
	assert.Empty(t, resp.Tables)
}

func TestSetTablesRejectsTakeawaySession(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	t1 := seedTable(t, db, "Bàn 1")

	_, session, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)

	_, err = setTables(t, runner, actor, session.ID, uuid.New(), t1)
	require.ErrorIs(t, err, sales.ErrTakeawayTablesNotAvailable)
}

func TestSetTablesRejectsClosedSession(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	t1 := seedTable(t, db, "Bàn 1")
	t2 := seedTable(t, db, "Bàn 2")

	_, session, err := startDineIn(t, runner, actor, uuid.New(), t1)
	require.NoError(t, err)

	// 5D owns closure; the test drives the state directly.
	_, err = db.Exec(`UPDATE service_sessions SET state = 'CLOSED' WHERE id = $1`, session.ID)
	require.NoError(t, err)

	_, err = setTables(t, runner, actor, session.ID, uuid.New(), t2)
	require.ErrorIs(t, err, sales.ErrServiceSessionClosed)
}

// An already-assigned Table that has since been marked unavailable must not
// block an unrelated change to the same Session: only newly added Tables are
// validated.
func TestSetTablesValidatesOnlyAdditions(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	t1 := seedTable(t, db, "Bàn 1")
	t2 := seedTable(t, db, "Bàn 2")

	_, session, err := startDineIn(t, runner, actor, uuid.New(), t1)
	require.NoError(t, err)

	_, err = db.Exec(`UPDATE tables SET available = false WHERE id = $1`, t1)
	require.NoError(t, err)

	resp, err := setTables(t, runner, actor, session.ID, uuid.New(), t1, t2)
	require.NoError(t, err, "keeping an already-assigned Table must not revalidate it")
	assert.Len(t, resp.Tables, 2)
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -p 1 -tags=integration ./internal/sales/ -run TestSetTables -v`
Expected: FAIL — `undefined: sales.NewSetSessionTablesHandler`.

- [ ] **Step 4: Write the handler**

Append to `internal/sales/table_assignments.go`:

```go
type setSessionTablesFingerprint struct {
	ServiceSessionID uuid.UUID   `json:"service_session_id"`
	TableIDs         []uuid.UUID `json:"table_ids"`
}

// SetSessionTablesHandler replaces a Dine-in Session's Table set.
type SetSessionTablesHandler struct{ runner *Runner }

// NewSetSessionTablesHandler creates a new SetSessionTablesHandler.
func NewSetSessionTablesHandler(runner *Runner) *SetSessionTablesHandler {
	return &SetSessionTablesHandler{runner: runner}
}

// Handle computes the difference between the Session's current and desired
// Table sets, releases what is gone, and assigns what is new.
//
// Only additions are validated for availability. A Table that is already
// assigned and has since been marked unavailable must not block an unrelated
// change to the same Session.
func (h *SetSessionTablesHandler) Handle(ctx context.Context, actor Actor,
	cmd SetSessionTablesCommand,
) (int, ServiceSessionResponse, error) {
	if HasDuplicateUUIDs(cmd.TableIDs) {
		return 0, ServiceSessionResponse{}, ErrTableSelectionDuplicate
	}

	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpSetSessionTables,
		Fingerprint: setSessionTablesFingerprint{
			ServiceSessionID: cmd.ServiceSessionID,
			TableIDs:         SortedUUIDs(cmd.TableIDs),
		},
		Required: []string{CapSalesOperate},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, ServiceSessionResponse, AuditRecord, error) {
			var zero ServiceSessionResponse
			q := mc.Queries

			session, err := q.LockServiceSessionForUpdate(ctx, cmd.ServiceSessionID)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return 0, zero, AuditRecord{},
						fmt.Errorf("%w: %s", ErrServiceSessionNotFound, cmd.ServiceSessionID)
				}
				return 0, zero, AuditRecord{}, fmt.Errorf("lock service session: %w", err)
			}
			if session.ServiceMode != ModeDineIn {
				return 0, zero, AuditRecord{}, ErrTakeawayTablesNotAvailable
			}
			if session.State != StateActive {
				return 0, zero, AuditRecord{}, ErrServiceSessionClosed
			}

			current, err := q.LockCurrentTableAssignments(ctx, cmd.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("lock current assignments: %w", err)
			}

			desired := make(map[uuid.UUID]struct{}, len(cmd.TableIDs))
			for _, id := range cmd.TableIDs {
				desired[id] = struct{}{}
			}
			assignedNow := make(map[uuid.UUID]struct{}, len(current))
			var toRelease []sqlc.LockCurrentTableAssignmentsRow
			for _, row := range current {
				assignedNow[row.TableID] = struct{}{}
				if _, keep := desired[row.TableID]; !keep {
					toRelease = append(toRelease, row)
				}
			}
			var toAdd []uuid.UUID
			for _, id := range cmd.TableIDs {
				if _, already := assignedNow[id]; !already {
					toAdd = append(toAdd, id)
				}
			}

			if err := lockAndValidateTables(ctx, q, toAdd); err != nil {
				return 0, zero, AuditRecord{}, err
			}

			released := make([]tableAssignmentAudit, 0, len(toRelease))
			for _, row := range toRelease {
				if err := q.ReleaseTableAssignment(ctx, sqlc.ReleaseTableAssignmentParams{
					ID:                        row.ID,
					ReleasedByStaffIdentityID: uuid.NullUUID{UUID: actor.StaffID, Valid: true},
				}); err != nil {
					return 0, zero, AuditRecord{}, fmt.Errorf("release table assignment: %w", err)
				}
				released = append(released, tableAssignmentAudit{
					TableAssignmentID: row.ID,
					TableID:           row.TableID,
					ServiceSessionID:  cmd.ServiceSessionID,
				})
			}
			if err := writeAssignmentAudits(ctx, q, actor, EventTableAssignmentReleased, released); err != nil {
				return 0, zero, AuditRecord{}, err
			}

			if len(toAdd) > 0 {
				startSeq, err := nextAssignmentSequence(ctx, q, cmd.ServiceSessionID)
				if err != nil {
					return 0, zero, AuditRecord{}, err
				}
				added, err := assignTables(ctx, q, actor, cmd.ServiceSessionID, toAdd, startSeq)
				if err != nil {
					return 0, zero, AuditRecord{}, err
				}
				if err := writeAssignmentAudits(ctx, q, actor, EventTableAssignmentCreated, added); err != nil {
					return 0, zero, AuditRecord{}, err
				}
			}

			result, err := LoadServiceSession(ctx, q, cmd.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			// The per-Table events above are the whole audit trail for this
			// command; there is no meaningful Session-level event to add.
			return 200, result, AuditRecord{}, nil
		})
}
```

Add `context`, `database/sql`, and `errors` to the file's imports.

> **Note for the implementer:** this handler does not call
> `requireOpenSalesShift`. The `LockServiceSessionForUpdate` row already
> carries `sales_shift_id`, and the Session's own `ACTIVE` state is the
> operative precondition for changing its Tables. If you decide the open-Shift
> check belongs here too, add it after the Session lock and add a test — do not
> add it silently.

- [ ] **Step 5: Add the route and HTTP handler**

Add `SetSessionTables *SetSessionTablesHandler` to `Slices`, wire it, and register:

```go
v1.PUT("/sales/service-sessions/:id/tables", s.handleSetSessionTables,
	authn.RequireAuth(), authn.RequireCapability(CapSalesOperate))
```

`handleSetSessionTables` binds the body, then sets `cmd.ServiceSessionID` from the `:id` path parameter, since it is `json:"-"`:

```go
//	@Summary		Set a Service Session's Tables
//	@Description	Replaces a Dine-in Session's current Table set. Tables no longer listed are released, preserving history. An empty table_ids releases every Table and is permitted. Rejected for a Takeaway Session.
//	@Tags			sales
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string					true	"Service Session ID"
//	@Param			request	body		SetSessionTablesCommand	true	"Request"
//	@Success		200		{object}	response.APIResponse{data=ServiceSessionResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Router			/sales/service-sessions/{id}/tables [put]
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -p 1 -race -tags=integration ./internal/sales/ ./internal/tables/ -v`
Expected: PASS in both packages.

- [ ] **Step 7: Commit**

```bash
make fmt
git add internal/sales/ sql/queries/sales.sql internal/database/sqlc/
git commit -m "feat(sales): set a Service Session's Tables

Differences current against desired assignments, releases with both pieces
of evidence the Phase 3 constraint requires, continues the sequence past
released rows, and validates only additions.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

## Task 9: Catalog Resolution Without A Catalog Import

Implements ADR-012. The set algebra lives in SQL, once, and a test pins it to `catalog.EffectiveGroupIDs`.

**Files:**
- Create: `internal/sales/catalog_resolution.go`
- Modify: `sql/queries/sales.sql`
- Test: `internal/sales/catalog_resolution_integration_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks except error sentinels.
- Produces:
  - `func effectiveGroupIDs(ctx context.Context, q *sqlc.Queries, menuItemID uuid.UUID) ([]uuid.UUID, error)`
  - `func defaultOptionIDs(ctx context.Context, q *sqlc.Queries, menuItemID uuid.UUID) ([]uuid.UUID, error)`
  - `func validateModifierOptions(ctx context.Context, q *sqlc.Queries, menuItemID uuid.UUID, optionIDs []uuid.UUID) error`

- [ ] **Step 1: Add the queries**

Append to `sql/queries/sales.sql`:

```sql
-- name: ListEffectiveModifierGroupIDs :many
-- (inherited - exclusions) + direct, for one Menu Item.
--
-- internal/catalog computes the same set in the exported pure function
-- EffectiveGroupIDs. Sales does not import it: MIGRATE_PLAN section 4.1
-- forbids importing another slice, and ADR-006 established reading another
-- slice's tables through one's own query. Expressing the algebra once in SQL
-- keeps the duplication to one function against one query, which
-- TestSalesResolutionMatchesCatalog pins together. See ADR-012.
WITH target AS (
    SELECT id, category_id FROM menu_items WHERE id = $1
),
inherited AS (
    SELECT cmg.modifier_group_id
    FROM category_modifier_groups cmg
    JOIN target ON target.category_id = cmg.menu_category_id
    WHERE NOT EXISTS (
        SELECT 1
        FROM item_modifier_group_exclusions ex
        WHERE ex.menu_item_id = (SELECT id FROM target)
          AND ex.modifier_group_id = cmg.modifier_group_id
    )
),
direct AS (
    SELECT img.modifier_group_id
    FROM item_modifier_groups img
    JOIN target ON target.id = img.menu_item_id
)
SELECT modifier_group_id
FROM (
    SELECT modifier_group_id FROM inherited
    UNION
    SELECT modifier_group_id FROM direct
) g
ORDER BY modifier_group_id ASC;

-- name: ListDefaultModifierOptionIDs :many
-- Declared defaults of the given Groups, filtered to what is currently
-- selectable. A retired Group's defaults never apply.
SELECT DISTINCT o.id
FROM modifier_group_default_options d
JOIN modifier_options o ON o.id = d.modifier_option_id
JOIN modifier_groups g ON g.id = o.modifier_group_id
WHERE d.modifier_group_id = ANY(sqlc.arg(group_ids)::uuid[])
  AND o.available
  AND o.retired_at IS NULL
  AND g.retired_at IS NULL
ORDER BY o.id ASC;

-- name: ListModifierOptionsForValidation :many
SELECT o.id, o.modifier_group_id, o.available,
       (o.retired_at IS NOT NULL) AS option_retired,
       (g.retired_at IS NOT NULL) AS group_retired
FROM modifier_options o
JOIN modifier_groups g ON g.id = o.modifier_group_id
WHERE o.id = ANY(sqlc.arg(option_ids)::uuid[]);
```

Run: `make sqlc`

- [ ] **Step 2: Write the failing consistency test**

Create `internal/sales/catalog_resolution_integration_test.go`. This is the only test file in the slice that imports `internal/catalog`, and it is allowed to: only production code is constrained.

```go
//go:build integration

package sales_test

import (
	"context"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/catalog"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resolutionFixture is one Menu Item and the three association sets that
// decide its effective Modifier Groups.
type resolutionFixture struct {
	MenuItemID uuid.UUID
	Inherited  []uuid.UUID
	Excluded   []uuid.UUID
	Direct     []uuid.UUID
}

// TestSalesResolutionMatchesCatalog is ADR-012's safety net. Sales expresses
// (inherited - exclusions) + direct in SQL and Catalog expresses it in Go; if
// the two ever disagree, this fails in CI rather than in a cafe.
func TestSalesResolutionMatchesCatalog(t *testing.T) {
	db, q := openSalesTestDB(t)
	ctx := context.Background()

	cases := []struct {
		name  string
		build func(t *testing.T, q *sqlc.Queries) resolutionFixture
	}{
		{"no groups at all", buildNoGroups},
		{"inherited only", buildInheritedOnly},
		{"direct only", buildDirectOnly},
		{"inherited and excluded", buildInheritedAndExcluded},
		{"excluded and directly attached", buildExcludedAndDirect},
		{"retired group still resolves as a member", buildRetiredGroup},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			truncateSalesTables(t, db)
			fx := tc.build(t, q)

			fromSQL, err := q.ListEffectiveModifierGroupIDs(ctx, fx.MenuItemID)
			require.NoError(t, err)

			fromGo := catalog.EffectiveGroupIDs(fx.Inherited, fx.Excluded, fx.Direct)

			assert.Equal(t, fromGo, fromSQL,
				"sales SQL resolution must equal catalog.EffectiveGroupIDs")
		})
	}
}

// A Group that is both inherited and directly attached appears once.
func TestEffectiveGroupsDeduplicate(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	ctx := context.Background()

	fx := buildInheritedAndDirectSameGroup(t, q)

	got, err := q.ListEffectiveModifierGroupIDs(ctx, fx.MenuItemID)
	require.NoError(t, err)
	assert.Len(t, got, 1)
}

// Defaults exclude unavailable options, retired options, and every option of a
// retired Group.
func TestDefaultOptionsFilterUnselectable(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	ctx := context.Background()

	fx, wantOptionID := buildDefaultsFixture(t, db, q)

	groups, err := q.ListEffectiveModifierGroupIDs(ctx, fx.MenuItemID)
	require.NoError(t, err)

	got, err := q.ListDefaultModifierOptionIDs(ctx, groups)
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{wantOptionID}, got)
}
```

> **Note for the implementer:** write the six `build*` fixture functions and
> `buildDefaultsFixture` in this file. Each inserts a Menu Category, a Menu
> Item, the Modifier Groups and Options it needs, and the association rows for
> its case, then returns the ids it used so the test can hand the same three
> sets to `catalog.EffectiveGroupIDs`. Use raw SQL inserts through `db` for the
> association tables; they have no sqlc writer in this slice. Keep each builder
> under about twenty lines — they are fixtures, not logic.
>
> `buildDefaultsFixture` must create four default options on one Group: one
> selectable (returned as `wantOptionID`), one unavailable, one retired, and
> one belonging to a second, retired Group.

- [ ] **Step 3: Run the tests to verify they fail**

Run: `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -p 1 -tags=integration ./internal/sales/ -run 'TestSalesResolution|TestEffectiveGroups|TestDefaultOptions' -v`
Expected: FAIL — the builders do not exist yet, then FAIL on the assertions until the queries are right.

- [ ] **Step 4: Write the Go wrappers**

Create `internal/sales/catalog_resolution.go`:

```go
package sales

import (
	"context"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

// effectiveGroupIDs returns the Modifier Groups that apply to a Menu Item:
// those inherited from its Menu Category and not excluded by it, plus those
// attached directly.
func effectiveGroupIDs(ctx context.Context, q *sqlc.Queries, menuItemID uuid.UUID) (
	[]uuid.UUID, error,
) {
	ids, err := q.ListEffectiveModifierGroupIDs(ctx, menuItemID)
	if err != nil {
		return nil, fmt.Errorf("resolve effective modifier groups: %w", err)
	}
	return ids, nil
}

// defaultOptionIDs returns the declared default options of a Menu Item's
// effective Groups, filtered to those currently selectable.
//
// These apply only when an add request omits modifier_option_ids entirely. An
// explicitly empty list means the customer declined every option and is taken
// literally.
func defaultOptionIDs(ctx context.Context, q *sqlc.Queries, menuItemID uuid.UUID) (
	[]uuid.UUID, error,
) {
	groups, err := effectiveGroupIDs(ctx, q, menuItemID)
	if err != nil {
		return nil, err
	}
	if len(groups) == 0 {
		return []uuid.UUID{}, nil
	}
	ids, err := q.ListDefaultModifierOptionIDs(ctx, groups)
	if err != nil {
		return nil, fmt.Errorf("resolve default modifier options: %w", err)
	}
	return ids, nil
}

// validateModifierOptions rejects any option that is not currently selectable
// for this Menu Item.
//
// An option belonging to a Group that is not effective for the Item returns
// ErrModifierOptionNotFound rather than a distinct code, matching the
// canonical source: from the caller's position the option is not selectable
// here, and the reason is not the caller's business.
//
// Note the check order. Retirement is permanent and unavailability is
// temporary, so a retired option must report retirement even though it is also
// unavailable; reporting "try again later" for something that will never
// return would be wrong.
func validateModifierOptions(ctx context.Context, q *sqlc.Queries,
	menuItemID uuid.UUID, optionIDs []uuid.UUID,
) error {
	if len(optionIDs) == 0 {
		return nil
	}
	if HasDuplicateUUIDs(optionIDs) {
		return fmt.Errorf("%w: the same modifier option was selected twice", ErrModifierOptionNotFound)
	}

	groups, err := effectiveGroupIDs(ctx, q, menuItemID)
	if err != nil {
		return err
	}
	effective := make(map[uuid.UUID]struct{}, len(groups))
	for _, id := range groups {
		effective[id] = struct{}{}
	}

	rows, err := q.ListModifierOptionsForValidation(ctx, optionIDs)
	if err != nil {
		return fmt.Errorf("load modifier options: %w", err)
	}
	byID := make(map[uuid.UUID]sqlc.ListModifierOptionsForValidationRow, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}

	for _, id := range optionIDs {
		row, ok := byID[id]
		if !ok {
			return fmt.Errorf("%w: %s", ErrModifierOptionNotFound, id)
		}
		if _, applies := effective[row.ModifierGroupID]; !applies {
			return fmt.Errorf("%w: %s is not offered for this menu item", ErrModifierOptionNotFound, id)
		}
		if row.OptionRetired || row.GroupRetired {
			return fmt.Errorf("%w: %s", ErrModifierOptionRetired, id)
		}
		if !row.Available {
			return fmt.Errorf("%w: %s", ErrModifierOptionUnavailable, id)
		}
	}
	return nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -p 1 -race -tags=integration ./internal/sales/ -v`
Expected: PASS.

- [ ] **Step 6: Re-verify the production boundary**

Run: `go list -deps ./internal/sales/ | grep 'pos-cafe/internal/catalog'`
Expected: **no output.** The catalog import exists only in the test file, which `go list -deps` on the package does not include.

- [ ] **Step 7: Commit**

```bash
make fmt
git add internal/sales/ sql/queries/sales.sql internal/database/sqlc/
git commit -m "feat(sales): resolve effective Modifier Groups through Sales' own SQL

Expresses (inherited - exclusions) + direct once, in SQL, rather than
importing catalog.EffectiveGroupIDs or copying it into Go. An integration
test pins the two together over shared fixtures (ADR-012).

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

## Task 10: Add Order Draft Item

**Files:**
- Create: `internal/sales/draft_add.go`
- Modify: `sql/queries/sales.sql`, `internal/sales/routes.go`, `internal/sales/http.go`
- Test: `internal/sales/draft_add_integration_test.go`

**Interfaces:**
- Consumes: `effectiveGroupIDs`, `defaultOptionIDs`, `validateModifierOptions`, `NormalizePreparationNote`, `ModifierKeyFor`, `ValidateQuantity`, `LoadServiceSession`.
- Produces:
  - `func lockEditableDraft(ctx context.Context, q *sqlc.Queries, sessionID uuid.UUID) (sqlc.LockEditableDraftRow, error)`
  - `func replaceDraftItemOptions(ctx context.Context, q *sqlc.Queries, itemID uuid.UUID, optionIDs []uuid.UUID) error`
  - `type AddDraftItemHandler`, `func NewAddDraftItemHandler(runner *Runner) *AddDraftItemHandler`, `func (h *AddDraftItemHandler) Handle(ctx, actor, cmd AddDraftItemCommand) (int, ServiceSessionResponse, error)`

- [ ] **Step 1: Add the queries**

Append to `sql/queries/sales.sql`:

```sql
-- name: LockEditableDraft :one
-- Checks every precondition and takes the lock in one statement, so there is
-- no window between the check and the write.
--
-- No match means the Session is missing, closed, its draft already committed,
-- or its Sales Shift closed. The caller reports EDITABLE_DRAFT_NOT_FOUND for
-- all four: the remedy is the same, and distinguishing them would leak state
-- about Sessions the caller did not ask about.
SELECT d.id AS order_draft_id, s.id AS service_session_id, s.service_mode
FROM service_sessions s
JOIN order_drafts d ON d.service_session_id = s.id
JOIN sales_shifts sh ON sh.id = s.sales_shift_id
WHERE s.id = $1
  AND s.state = 'ACTIVE'
  AND d.state = 'EDITABLE'
  AND sh.state = 'OPEN'
FOR UPDATE OF d, s;

-- name: LockMenuItemForDraft :one
-- Locked FOR UPDATE so the Item cannot be retired between validation and
-- write. Sales rows are always locked before Catalog rows, and
-- internal/catalog never locks Sales rows, so no deadlock cycle exists.
SELECT id, category_id, name, price_vnd, available, retired_at
FROM menu_items
WHERE id = $1
FOR UPDATE;

-- name: GetMenuItemSizeForDraft :one
SELECT id, menu_item_id, name, price_vnd, available, retired_at
FROM menu_item_sizes
WHERE id = $1;

-- name: FindDraftItemByComposition :one
SELECT id, quantity
FROM order_draft_items
WHERE order_draft_id = $1
  AND menu_item_id = $2
  AND size_key = COALESCE(sqlc.narg(size_id)::uuid::text, '')
  AND note_key = COALESCE(sqlc.narg(preparation_note)::text, '')
  AND modifier_key = $3;

-- name: InsertDraftItem :one
INSERT INTO order_draft_items
    (order_draft_id, menu_item_id, size_id, preparation_note, modifier_key, quantity)
VALUES ($1, $2, $3, $4, $5, 1)
RETURNING id, quantity;

-- name: SetDraftItemQuantity :one
UPDATE order_draft_items
SET quantity = $2
WHERE id = $1
RETURNING id, quantity;

-- name: DeleteDraftItemModifierOptions :exec
DELETE FROM order_draft_item_modifier_options WHERE order_draft_item_id = $1;

-- name: InsertDraftItemModifierOption :exec
INSERT INTO order_draft_item_modifier_options (order_draft_item_id, modifier_option_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;
```

Run: `make sqlc`

> **Note for the implementer:** `FindDraftItemByComposition` compares against
> the generated `size_key` and `note_key` columns rather than against `size_id`
> and `preparation_note`, because `NULL = NULL` is not true in SQL and the
> whole point of those columns is to make the comparison total. Confirm sqlc
> generates the nullable parameters you expect; if `sqlc.narg` with the double
> cast is awkward, pass `size_key` and `note_key` as plain `text` parameters
> computed in Go instead, which is simpler and equally correct. Prefer that if
> the generated signature looks strange.

- [ ] **Step 2: Write the failing integration test**

Create `internal/sales/draft_add_integration_test.go`:

```go
//go:build integration

package sales_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func addItem(t *testing.T, runner *sales.Runner, actor sales.Actor,
	cmd sales.AddDraftItemCommand,
) (sales.ServiceSessionResponse, error) {
	t.Helper()
	if cmd.RequestID == uuid.Nil {
		cmd.RequestID = uuid.New()
	}
	_, resp, err := sales.NewAddDraftItemHandler(runner).Handle(context.Background(), actor, cmd)
	return resp, err
}

// Adding the same composition twice yields one row with quantity 2. The
// command takes no quantity parameter; it always adds one unit.
func TestAddSameCompositionIncrementsQuantity(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	_, session, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)
	itemID := seedMenuItem(t, db, "Cà phê sữa", 25000)

	cmd := sales.AddDraftItemCommand{ServiceSessionID: session.ID, MenuItemID: itemID}
	_, err = addItem(t, runner, actor, cmd)
	require.NoError(t, err)

	resp, err := addItem(t, runner, actor, cmd)
	require.NoError(t, err)

	require.Len(t, resp.Draft.Items, 1, "the same composition must not create a second line")
	assert.Equal(t, int32(2), resp.Draft.Items[0].Quantity)
	assertAuditEvent(t, db, sales.EventDraftItemAdded, 2)
}

// Two compositions that differ in any component are two lines.
func TestAddDifferentCompositionsAreSeparateLines(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	_, session, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)
	itemID := seedMenuItem(t, db, "Cà phê sữa", 25000)

	_, err = addItem(t, runner, actor, sales.AddDraftItemCommand{
		ServiceSessionID: session.ID, MenuItemID: itemID})
	require.NoError(t, err)

	note := "ít đường"
	resp, err := addItem(t, runner, actor, sales.AddDraftItemCommand{
		ServiceSessionID: session.ID, MenuItemID: itemID, PreparationNote: &note})
	require.NoError(t, err)

	assert.Len(t, resp.Draft.Items, 2, "a different note is a different composition")
}

// Absent modifier_option_ids applies the menu's defaults; an explicitly empty
// list applies none. The two are different requests, not an idempotency
// conflict.
func TestAddAppliesDefaultsOnlyWhenOptionsAbsent(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	_, session, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)

	itemID, defaultOptionID := seedItemWithDefaultOption(t, db)

	withDefaults, err := addItem(t, runner, actor, sales.AddDraftItemCommand{
		ServiceSessionID: session.ID, MenuItemID: itemID})
	require.NoError(t, err)
	require.Len(t, withDefaults.Draft.Items, 1)
	require.Len(t, withDefaults.Draft.Items[0].SelectedModifierOptions, 1)
	assert.Equal(t, defaultOptionID, withDefaults.Draft.Items[0].SelectedModifierOptions[0].ID)

	empty := []uuid.UUID{}
	withNone, err := addItem(t, runner, actor, sales.AddDraftItemCommand{
		ServiceSessionID:  session.ID,
		MenuItemID:        itemID,
		ModifierOptionIDs: &empty,
	})
	require.NoError(t, err)
	assert.Len(t, withNone.Draft.Items, 2,
		"an explicit empty selection is a different composition from the defaults")
}

// A draft tolerates incompleteness: an item whose Menu Item requires a Size is
// accepted with no Size. Commit, in 5B, is where completeness is checked.
func TestAddAcceptsIncompleteConfiguration(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	_, session, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)

	// A sized item has a NULL price_vnd of its own.
	itemID := seedSizedMenuItem(t, db, "Trà sữa")
	seedSize(t, db, itemID, "Lớn", 40000)

	resp, err := addItem(t, runner, actor, sales.AddDraftItemCommand{
		ServiceSessionID: session.ID, MenuItemID: itemID})
	require.NoError(t, err, "a draft holds an item with no Size chosen yet")
	require.Len(t, resp.Draft.Items, 1)
	assert.Nil(t, resp.Draft.Items[0].PriceVND, "an unsized sized-item has no price yet")
}

func TestAddRejectsUnselectableCatalogEntities(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	_, session, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)

	t.Run("unknown menu item", func(t *testing.T) {
		_, err := addItem(t, runner, actor, sales.AddDraftItemCommand{
			ServiceSessionID: session.ID, MenuItemID: uuid.New()})
		require.ErrorIs(t, err, sales.ErrMenuItemNotFound)
	})

	t.Run("unavailable menu item", func(t *testing.T) {
		itemID := seedMenuItem(t, db, "Hết hàng", 20000)
		_, err := db.Exec(`UPDATE menu_items SET available = false WHERE id = $1`, itemID)
		require.NoError(t, err)

		_, err = addItem(t, runner, actor, sales.AddDraftItemCommand{
			ServiceSessionID: session.ID, MenuItemID: itemID})
		require.ErrorIs(t, err, sales.ErrMenuItemUnavailable)
	})

	t.Run("retired menu item", func(t *testing.T) {
		itemID := seedMenuItem(t, db, "Ngừng bán", 20000)
		_, err := db.Exec(`
			UPDATE menu_items SET retired_at = now(), retirement_reason = 'DISCONTINUED'
			WHERE id = $1`, itemID)
		require.NoError(t, err)

		_, err = addItem(t, runner, actor, sales.AddDraftItemCommand{
			ServiceSessionID: session.ID, MenuItemID: itemID})
		require.ErrorIs(t, err, sales.ErrMenuItemRetired)
	})

	t.Run("size belonging to another item", func(t *testing.T) {
		itemA := seedMenuItem(t, db, "Món A", 20000)
		itemB := seedSizedMenuItem(t, db, "Món B")
		foreignSize := seedSize(t, db, itemB, "Lớn", 30000)

		_, err := addItem(t, runner, actor, sales.AddDraftItemCommand{
			ServiceSessionID: session.ID, MenuItemID: itemA, SizeID: &foreignSize})
		require.ErrorIs(t, err, sales.ErrSizeNotFound)
	})
}

// An option whose Group the Item excludes is not selectable here, and reports
// not-found rather than a distinct code.
func TestAddRejectsOptionFromExcludedGroup(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	_, session, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)

	excludingItem, inheritingItem, optionID := seedExclusionFixture(t, db)

	_, err = addItem(t, runner, actor, sales.AddDraftItemCommand{
		ServiceSessionID:  session.ID,
		MenuItemID:        excludingItem,
		ModifierOptionIDs: &[]uuid.UUID{optionID},
	})
	require.ErrorIs(t, err, sales.ErrModifierOptionNotFound)

	_, err = addItem(t, runner, actor, sales.AddDraftItemCommand{
		ServiceSessionID:  session.ID,
		MenuItemID:        inheritingItem,
		ModifierOptionIDs: &[]uuid.UUID{optionID},
	})
	require.NoError(t, err, "the same option is selectable for an item that inherits the group")
}

func TestAddRejectsOverlongPreparationNote(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	_, session, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)
	itemID := seedMenuItem(t, db, "Cà phê sữa", 25000)

	long := strings.Repeat("á", 201)
	_, err = addItem(t, runner, actor, sales.AddDraftItemCommand{
		ServiceSessionID: session.ID, MenuItemID: itemID, PreparationNote: &long})
	require.ErrorIs(t, err, sales.ErrInvalidPreparationNote)
}

// Draft edits stop working once the Session's Shift closes.
func TestAddRejectedAfterShiftCloses(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	shiftID := seedOpenShift(t, q, actor.StaffID)
	_, session, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)
	itemID := seedMenuItem(t, db, "Cà phê sữa", 25000)

	_, err = db.Exec(`UPDATE sales_shifts SET state = 'CLOSED' WHERE id = $1`, shiftID)
	require.NoError(t, err)

	_, err = addItem(t, runner, actor, sales.AddDraftItemCommand{
		ServiceSessionID: session.ID, MenuItemID: itemID})
	require.ErrorIs(t, err, sales.ErrEditableDraftNotFound)
}

// modifier_key is derived, never authoritative. It must always equal the
// sorted join of the row's actual option rows.
func TestModifierKeyMatchesStoredOptions(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	_, session, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)

	itemID, optionA, optionB := seedItemWithTwoOptions(t, db)

	_, err = addItem(t, runner, actor, sales.AddDraftItemCommand{
		ServiceSessionID:  session.ID,
		MenuItemID:        itemID,
		ModifierOptionIDs: &[]uuid.UUID{optionB, optionA},
	})
	require.NoError(t, err)

	assertModifierKeyIntegrity(t, db)
}

// A replay returns the projection as it stood when the mutation committed, not
// a fresh read. Idempotency exists so a retried request reproduces its original
// outcome; a client wanting current state issues the read.
func TestReplayReturnsTheCommittedSnapshotNotFreshState(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	_, session, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)
	menuItemID := seedMenuItem(t, db, "Cà phê sữa", 25000)

	cmd := sales.AddDraftItemCommand{
		RequestID:        uuid.New(),
		ServiceSessionID: session.ID,
		MenuItemID:       menuItemID,
	}
	first, err := addItem(t, runner, actor, cmd)
	require.NoError(t, err)
	require.True(t, first.Draft.Items[0].Available)

	_, err = db.Exec(`UPDATE menu_items SET available = false WHERE id = $1`, menuItemID)
	require.NoError(t, err)

	replay, err := addItem(t, runner, actor, cmd)
	require.NoError(t, err)
	assert.True(t, replay.Draft.Items[0].Available,
		"a replay reports the availability captured at commit time")

	fresh, err := sales.NewGetServiceSessionHandler(runner).
		Handle(context.Background(), actor, session.ID)
	require.NoError(t, err)
	assert.False(t, fresh.Draft.Items[0].Available,
		"a fresh read reports current availability")
}
```

Append these helpers to the harness. They are used by Tasks 10 through 13.

```go
// seedMenuItem creates a directly priced Menu Item in a fresh Category.
func seedMenuItem(t *testing.T, db *sql.DB, name string, priceVND int64) uuid.UUID {
	t.Helper()
	categoryID := seedMenuCategory(t, db, "Cat "+name)
	var id uuid.UUID
	require.NoError(t, db.QueryRow(`
		INSERT INTO menu_items (category_id, name, normalized_name, price_vnd, available)
		VALUES ($1, $2, lower($2), $3, true) RETURNING id`,
		categoryID, name, priceVND).Scan(&id))
	return id
}

// seedSizedMenuItem creates a Menu Item priced only through its Sizes, so its
// own price_vnd is NULL.
func seedSizedMenuItem(t *testing.T, db *sql.DB, name string) uuid.UUID {
	t.Helper()
	categoryID := seedMenuCategory(t, db, "Cat "+name)
	var id uuid.UUID
	require.NoError(t, db.QueryRow(`
		INSERT INTO menu_items (category_id, name, normalized_name, price_vnd, available)
		VALUES ($1, $2, lower($2), NULL, true) RETURNING id`,
		categoryID, name).Scan(&id))
	return id
}

func seedMenuCategory(t *testing.T, db *sql.DB, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	require.NoError(t, db.QueryRow(`
		INSERT INTO menu_categories (name, normalized_name)
		VALUES ($1, lower($1)) RETURNING id`, name).Scan(&id))
	return id
}

// assertModifierKeyIntegrity proves the denormalized key never drifts from the
// authoritative option rows.
func assertModifierKeyIntegrity(t *testing.T, db *sql.DB) {
	t.Helper()
	var drifted int
	require.NoError(t, db.QueryRow(`
		SELECT count(*)
		FROM order_draft_items di
		WHERE di.modifier_key <> COALESCE((
		    SELECT string_agg(m.modifier_option_id::text, ',' ORDER BY m.modifier_option_id::text)
		    FROM order_draft_item_modifier_options m
		    WHERE m.order_draft_item_id = di.id
		), '')`).Scan(&drifted))
	assert.Equal(t, 0, drifted, "modifier_key must equal the sorted join of the stored options")
}
```

> **Note for the implementer:** write `seedItemWithDefaultOption`,
> `seedExclusionFixture`, and `seedItemWithTwoOptions` alongside them. Each
> returns the ids its test needs. `seedExclusionFixture` creates one Category
> with a default Group, two Items in it, and an
> `item_modifier_group_exclusions` row for the first.
>
> `assertModifierKeyIntegrity` sorts by `modifier_option_id::text`, matching
> `ModifierKeyFor`, which sorts by `uuid.String()`. Do not change one without
> the other.

- [ ] **Step 3: Run the tests to verify they fail**

Run: `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -p 1 -tags=integration ./internal/sales/ -run TestAdd -v`
Expected: FAIL — `undefined: sales.NewAddDraftItemHandler`.

- [ ] **Step 4: Write the implementation**

Create `internal/sales/draft_add.go`:

```go
package sales

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

// addDraftItemFingerprint sorts the option ids: selection order does not
// change the chosen set. ModifierOptionIDs stays a pointer so an absent list
// and an empty one hash differently — they are different requests.
type addDraftItemFingerprint struct {
	ServiceSessionID  uuid.UUID    `json:"service_session_id"`
	MenuItemID        uuid.UUID    `json:"menu_item_id"`
	SizeID            *uuid.UUID   `json:"size_id"`
	PreparationNote   *string      `json:"preparation_note"`
	ModifierOptionIDs *[]uuid.UUID `json:"modifier_option_ids"`
}

type draftItemAudit struct {
	ServiceSessionID uuid.UUID   `json:"service_session_id"`
	OrderDraftID     uuid.UUID   `json:"order_draft_id"`
	DraftItemID      uuid.UUID   `json:"draft_item_id"`
	MenuItemID       uuid.UUID   `json:"menu_item_id"`
	Quantity         int32       `json:"quantity"`
	ModifierOptionIDs []uuid.UUID `json:"modifier_option_ids"`
}

// lockEditableDraft acquires the Session's editable draft, checking every
// precondition in the same statement that takes the lock.
func lockEditableDraft(ctx context.Context, q *sqlc.Queries, sessionID uuid.UUID) (
	sqlc.LockEditableDraftRow, error,
) {
	row, err := q.LockEditableDraft(ctx, sessionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return row, fmt.Errorf("%w: %s", ErrEditableDraftNotFound, sessionID)
		}
		return row, fmt.Errorf("lock editable draft: %w", err)
	}
	return row, nil
}

// requireSellableMenuItem locks the Menu Item and rejects it if it is not
// currently choosable.
//
// Retirement is checked before availability: retirement is permanent and
// availability is temporary, so a retired item must not report "try again
// later".
func requireSellableMenuItem(ctx context.Context, q *sqlc.Queries, menuItemID uuid.UUID) (
	sqlc.LockMenuItemForDraftRow, error,
) {
	item, err := q.LockMenuItemForDraft(ctx, menuItemID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return item, fmt.Errorf("%w: %s", ErrMenuItemNotFound, menuItemID)
		}
		return item, fmt.Errorf("lock menu item: %w", err)
	}
	if item.RetiredAt.Valid {
		return item, fmt.Errorf("%w: %s", ErrMenuItemRetired, item.Name)
	}
	if !item.Available {
		return item, fmt.Errorf("%w: %s", ErrMenuItemUnavailable, item.Name)
	}
	return item, nil
}

// requireSizeForItem rejects a Size that does not belong to the Item or is not
// currently choosable. A Size from another Menu Item reports not-found: it is
// not a Size of this item at all.
func requireSizeForItem(ctx context.Context, q *sqlc.Queries,
	menuItemID, sizeID uuid.UUID,
) error {
	size, err := q.GetMenuItemSizeForDraft(ctx, sizeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: %s", ErrSizeNotFound, sizeID)
		}
		return fmt.Errorf("load menu item size: %w", err)
	}
	if size.MenuItemID != menuItemID {
		return fmt.Errorf("%w: %s does not belong to this menu item", ErrSizeNotFound, sizeID)
	}
	if size.RetiredAt.Valid {
		return fmt.Errorf("%w: %s", ErrSizeRetired, size.Name)
	}
	if !size.Available {
		return fmt.Errorf("%w: %s", ErrSizeUnavailable, size.Name)
	}
	return nil
}

// replaceDraftItemOptions rewrites a draft item's selected options.
func replaceDraftItemOptions(ctx context.Context, q *sqlc.Queries,
	itemID uuid.UUID, optionIDs []uuid.UUID,
) error {
	if err := q.DeleteDraftItemModifierOptions(ctx, itemID); err != nil {
		return fmt.Errorf("clear draft item modifier options: %w", err)
	}
	for _, optionID := range optionIDs {
		if err := q.InsertDraftItemModifierOption(ctx, sqlc.InsertDraftItemModifierOptionParams{
			OrderDraftItemID: itemID,
			ModifierOptionID: optionID,
		}); err != nil {
			return fmt.Errorf("insert draft item modifier option: %w", err)
		}
	}
	return nil
}

// AddDraftItemHandler adds one unit of a configured Menu Item to the draft.
type AddDraftItemHandler struct{ runner *Runner }

// NewAddDraftItemHandler creates a new AddDraftItemHandler.
func NewAddDraftItemHandler(runner *Runner) *AddDraftItemHandler {
	return &AddDraftItemHandler{runner: runner}
}

// Handle adds one unit, merging into an existing line of the same composition.
//
// Validation is deliberately shallow: it checks only that what has been chosen
// is currently choosable, not that the configuration is complete. A sized item
// with no Size and a required Group with no selection are both valid draft
// states. Commit, in 5B, is where completeness is enforced, because that is
// where price is fixed.
func (h *AddDraftItemHandler) Handle(ctx context.Context, actor Actor,
	cmd AddDraftItemCommand,
) (int, ServiceSessionResponse, error) {
	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpAddDraftItem,
		Fingerprint: addDraftItemFingerprint{
			ServiceSessionID:  cmd.ServiceSessionID,
			MenuItemID:        cmd.MenuItemID,
			SizeID:            cmd.SizeID,
			PreparationNote:   trimmedForFingerprint(cmd.PreparationNote),
			ModifierOptionIDs: sortedOptionPointer(cmd.ModifierOptionIDs),
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

			item, err := requireSellableMenuItem(ctx, q, cmd.MenuItemID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			if cmd.SizeID != nil {
				if err := requireSizeForItem(ctx, q, item.ID, *cmd.SizeID); err != nil {
					return 0, zero, AuditRecord{}, err
				}
			}

			// Absent means "use the menu's defaults"; empty means "the
			// customer declined every option".
			var optionIDs []uuid.UUID
			if cmd.ModifierOptionIDs == nil {
				optionIDs, err = defaultOptionIDs(ctx, q, item.ID)
				if err != nil {
					return 0, zero, AuditRecord{}, err
				}
			} else {
				optionIDs = *cmd.ModifierOptionIDs
			}
			if err := validateModifierOptions(ctx, q, item.ID, optionIDs); err != nil {
				return 0, zero, AuditRecord{}, err
			}

			note, err := NormalizePreparationNote(cmd.PreparationNote)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			modifierKey := ModifierKeyFor(optionIDs)

			itemID, quantity, err := upsertDraftItem(ctx, q, draft.OrderDraftID,
				cmd.MenuItemID, cmd.SizeID, note, modifierKey, optionIDs)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}

			result, err := LoadServiceSession(ctx, q, cmd.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}

			return 200, result, AuditRecord{
				EventType: EventDraftItemAdded,
				Details: draftItemAudit{
					ServiceSessionID:  cmd.ServiceSessionID,
					OrderDraftID:      draft.OrderDraftID,
					DraftItemID:       itemID,
					MenuItemID:        cmd.MenuItemID,
					Quantity:          quantity,
					ModifierOptionIDs: SortedUUIDs(optionIDs),
				},
			}, nil
		})
}

// upsertDraftItem increments an existing line of the same composition or
// inserts a new one.
//
// This is a lookup-then-write rather than an ON CONFLICT upsert, because the
// increment must be validated against MaxQuantity: an upsert's DO UPDATE would
// hit the database check constraint and surface as an unmapped 500 instead of
// a business error. The caller already holds the draft row lock, so no other
// transaction can insert a competing composition in between.
func upsertDraftItem(ctx context.Context, q *sqlc.Queries, draftID, menuItemID uuid.UUID,
	sizeID *uuid.UUID, note sql.NullString, modifierKey string, optionIDs []uuid.UUID,
) (uuid.UUID, int32, error) {
	existing, err := findDraftItemByComposition(ctx, q, draftID, menuItemID, sizeID, note, modifierKey)
	if err != nil {
		return uuid.Nil, 0, err
	}
	if existing != nil {
		next := existing.Quantity + 1
		if err := ValidateQuantity(next); err != nil {
			return uuid.Nil, 0, err
		}
		row, err := q.SetDraftItemQuantity(ctx, sqlc.SetDraftItemQuantityParams{
			ID: existing.ID, Quantity: next,
		})
		if err != nil {
			return uuid.Nil, 0, fmt.Errorf("increment draft item quantity: %w", err)
		}
		return row.ID, row.Quantity, nil
	}

	row, err := q.InsertDraftItem(ctx, sqlc.InsertDraftItemParams{
		OrderDraftID:    draftID,
		MenuItemID:      menuItemID,
		SizeID:          nullUUID(sizeID),
		PreparationNote: note,
		ModifierKey:     modifierKey,
	})
	if err != nil {
		return uuid.Nil, 0, fmt.Errorf("insert draft item: %w", err)
	}
	if err := replaceDraftItemOptions(ctx, q, row.ID, optionIDs); err != nil {
		return uuid.Nil, 0, err
	}
	return row.ID, row.Quantity, nil
}
```

> **Note for the implementer:** write these four small helpers in the same
> file, or in `domain.go` if they are pure:
>
> - `findDraftItemByComposition(...) (*sqlc.FindDraftItemByCompositionRow, error)`
>   — calls the query and converts `sql.ErrNoRows` to `(nil, nil)`.
> - `nullUUID(*uuid.UUID) uuid.NullUUID`
> - `trimmedForFingerprint(*string) *string` — returns a pointer to the trimmed
>   note, or nil when it trims to empty, so a replay differing only in
>   surrounding whitespace is a replay rather than a conflict.
> - `sortedOptionPointer(*[]uuid.UUID) *[]uuid.UUID` — returns nil for nil, and
>   a pointer to the sorted copy otherwise, preserving the absent-versus-empty
>   distinction through the fingerprint.

- [ ] **Step 5: Add the route and HTTP handler**

Add `AddDraftItem *AddDraftItemHandler` to `Slices`, wire it, and register:

```go
v1.POST("/sales/service-sessions/:id/draft/items", s.handleAddDraftItem,
	authn.RequireAuth(), authn.RequireCapability(CapSalesOperate))
```

`handleAddDraftItem` binds the body and sets `cmd.ServiceSessionID` from `:id`:

```go
//	@Summary		Add an Order Draft item
//	@Description	Adds one unit of a configured Menu Item, merging into an existing line of the same composition. Omit modifier_option_ids to apply the menu's default options; send an empty array to apply none. The draft accepts an incomplete configuration; completeness is checked at Commit in Phase 5B.
//	@Tags			sales
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string				true	"Service Session ID"
//	@Param			request	body		AddDraftItemCommand	true	"Request"
//	@Success		200		{object}	response.APIResponse{data=ServiceSessionResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Router			/sales/service-sessions/{id}/draft/items [post]
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -p 1 -race -tags=integration ./internal/sales/ -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
make fmt
git add internal/sales/ sql/queries/sales.sql internal/database/sqlc/
git commit -m "feat(sales): add Order Draft items with composition merging

Adds one unit per call, incrementing an existing line of the same
composition. Applies menu defaults only when modifier_option_ids is absent,
and takes an explicit empty list literally. Validates only what was chosen;
completeness belongs to Commit in 5B.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

## Task 11: Set Quantity And Remove Draft Item

The two draft commands that cannot trigger a composition merge, so they land before the merge helper.

**Files:**
- Create: `internal/sales/draft_edit.go`, `internal/sales/draft_remove.go`
- Modify: `sql/queries/sales.sql`, `internal/sales/routes.go`, `internal/sales/http.go`
- Test: `internal/sales/draft_quantity_integration_test.go`

**Interfaces:**
- Consumes: `lockEditableDraft`, `ValidateQuantity`, `LoadServiceSession`.
- Produces:
  - `func lockDraftItem(ctx context.Context, q *sqlc.Queries, draftID, itemID uuid.UUID) (sqlc.LockDraftItemRow, error)`
  - `type SetDraftItemQuantityHandler`, `NewSetDraftItemQuantityHandler`, `Handle(ctx, actor, cmd SetDraftItemQuantityCommand) (int, ServiceSessionResponse, error)`
  - `type RemoveDraftItemHandler`, `NewRemoveDraftItemHandler`, `Handle(ctx, actor, cmd RemoveDraftItemCommand) (int, ServiceSessionResponse, error)`

- [ ] **Step 1: Add the queries**

Append to `sql/queries/sales.sql`:

```sql
-- name: LockDraftItem :one
-- Scoped to the draft, so a caller cannot reach an item of another Session by
-- guessing its id.
SELECT id, order_draft_id, menu_item_id, size_id, quantity, preparation_note, modifier_key
FROM order_draft_items
WHERE id = $1 AND order_draft_id = $2
FOR UPDATE;

-- name: ListDraftItemOptionIDs :many
SELECT modifier_option_id
FROM order_draft_item_modifier_options
WHERE order_draft_item_id = $1
ORDER BY modifier_option_id ASC;

-- name: DeleteDraftItem :exec
DELETE FROM order_draft_items WHERE id = $1;
```

Run: `make sqlc`

- [ ] **Step 2: Write the failing integration test**

Create `internal/sales/draft_quantity_integration_test.go`:

```go
//go:build integration

package sales_test

import (
	"context"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// draftWithOneItem opens a Session and adds one item, returning both ids.
func draftWithOneItem(t *testing.T, runner *sales.Runner, actor sales.Actor,
	menuItemID uuid.UUID,
) (sessionID uuid.UUID, draftItemID uuid.UUID) {
	t.Helper()
	_, session, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)

	resp, err := addItem(t, runner, actor, sales.AddDraftItemCommand{
		ServiceSessionID: session.ID, MenuItemID: menuItemID})
	require.NoError(t, err)
	require.Len(t, resp.Draft.Items, 1)
	return session.ID, resp.Draft.Items[0].ID
}

func setQuantity(t *testing.T, runner *sales.Runner, actor sales.Actor,
	sessionID, itemID uuid.UUID, quantity int32,
) (sales.ServiceSessionResponse, error) {
	t.Helper()
	_, resp, err := sales.NewSetDraftItemQuantityHandler(runner).Handle(
		context.Background(), actor, sales.SetDraftItemQuantityCommand{
			RequestID:        uuid.New(),
			ServiceSessionID: sessionID,
			DraftItemID:      itemID,
			Quantity:         &quantity,
		})
	return resp, err
}

func TestSetDraftItemQuantity(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	menuItemID := seedMenuItem(t, db, "Cà phê sữa", 25000)
	sessionID, itemID := draftWithOneItem(t, runner, actor, menuItemID)

	resp, err := setQuantity(t, runner, actor, sessionID, itemID, 7)
	require.NoError(t, err)
	require.Len(t, resp.Draft.Items, 1)
	assert.Equal(t, int32(7), resp.Draft.Items[0].Quantity)

	assertAuditEvent(t, db, sales.EventDraftItemQuantitySet, 1)
}

func TestSetDraftItemQuantityBounds(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	menuItemID := seedMenuItem(t, db, "Cà phê sữa", 25000)
	sessionID, itemID := draftWithOneItem(t, runner, actor, menuItemID)

	// Zero is rejected rather than treated as removal: removal is its own
	// command with its own audit event.
	_, err := setQuantity(t, runner, actor, sessionID, itemID, 0)
	require.ErrorIs(t, err, sales.ErrInvalidQuantity)

	_, err = setQuantity(t, runner, actor, sessionID, itemID, 10000)
	require.ErrorIs(t, err, sales.ErrInvalidQuantity)

	_, err = setQuantity(t, runner, actor, sessionID, itemID, 9999)
	require.NoError(t, err)
}

// An item id belonging to another Session's draft is not reachable.
func TestSetDraftItemQuantityRejectsForeignItem(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	menuItemID := seedMenuItem(t, db, "Cà phê sữa", 25000)

	sessionA, _ := draftWithOneItem(t, runner, actor, menuItemID)
	_, foreignItemID := draftWithOneItem(t, runner, actor, menuItemID)

	_, err := setQuantity(t, runner, actor, sessionA, foreignItemID, 3)
	require.ErrorIs(t, err, sales.ErrDraftItemNotFound)
}

func TestRemoveDraftItem(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	menuItemID := seedMenuItem(t, db, "Cà phê sữa", 25000)
	sessionID, itemID := draftWithOneItem(t, runner, actor, menuItemID)

	_, resp, err := sales.NewRemoveDraftItemHandler(runner).Handle(
		context.Background(), actor, sales.RemoveDraftItemCommand{
			RequestID:        uuid.New(),
			ServiceSessionID: sessionID,
			DraftItemID:      itemID,
		})
	require.NoError(t, err)
	assert.Empty(t, resp.Draft.Items)

	assertAuditEvent(t, db, sales.EventDraftItemRemoved, 1)

	// The cascade cleared the option rows too.
	var options int
	require.NoError(t, db.QueryRow(
		`SELECT count(*) FROM order_draft_item_modifier_options`).Scan(&options))
	assert.Equal(t, 0, options)
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -p 1 -tags=integration ./internal/sales/ -run 'TestSetDraftItemQuantity|TestRemoveDraftItem' -v`
Expected: FAIL — `undefined: sales.NewSetDraftItemQuantityHandler`.

- [ ] **Step 4: Write the implementation**

Create `internal/sales/draft_edit.go`:

```go
package sales

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

// lockDraftItem acquires one draft item, scoped to its draft so a caller
// cannot reach another Session's item by guessing an id.
func lockDraftItem(ctx context.Context, q *sqlc.Queries, draftID, itemID uuid.UUID) (
	sqlc.LockDraftItemRow, error,
) {
	row, err := q.LockDraftItem(ctx, sqlc.LockDraftItemParams{
		ID: itemID, OrderDraftID: draftID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return row, fmt.Errorf("%w: %s", ErrDraftItemNotFound, itemID)
		}
		return row, fmt.Errorf("lock draft item: %w", err)
	}
	return row, nil
}

type setQuantityFingerprint struct {
	ServiceSessionID uuid.UUID `json:"service_session_id"`
	DraftItemID      uuid.UUID `json:"draft_item_id"`
	Quantity         int32     `json:"quantity"`
}

// SetDraftItemQuantityHandler sets an absolute quantity on a draft item.
type SetDraftItemQuantityHandler struct{ runner *Runner }

// NewSetDraftItemQuantityHandler creates a new SetDraftItemQuantityHandler.
func NewSetDraftItemQuantityHandler(runner *Runner) *SetDraftItemQuantityHandler {
	return &SetDraftItemQuantityHandler{runner: runner}
}

// Handle sets the quantity. Zero is rejected rather than treated as removal.
//
// Quantity is not part of the composition, so this command can never trigger a
// merge.
func (h *SetDraftItemQuantityHandler) Handle(ctx context.Context, actor Actor,
	cmd SetDraftItemQuantityCommand,
) (int, ServiceSessionResponse, error) {
	var zero ServiceSessionResponse
	if cmd.Quantity == nil {
		return 0, zero, fmt.Errorf("%w: quantity is required", ErrInvalidQuantity)
	}
	quantity := *cmd.Quantity
	if err := ValidateQuantity(quantity); err != nil {
		return 0, zero, err
	}

	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpSetDraftItemQuantity,
		Fingerprint: setQuantityFingerprint{
			ServiceSessionID: cmd.ServiceSessionID,
			DraftItemID:      cmd.DraftItemID,
			Quantity:         quantity,
		},
		Required: []string{CapSalesOperate},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, ServiceSessionResponse, AuditRecord, error) {
			q := mc.Queries

			draft, err := lockEditableDraft(ctx, q, cmd.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			item, err := lockDraftItem(ctx, q, draft.OrderDraftID, cmd.DraftItemID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			if _, err := q.SetDraftItemQuantity(ctx, sqlc.SetDraftItemQuantityParams{
				ID: item.ID, Quantity: quantity,
			}); err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("set draft item quantity: %w", err)
			}

			result, err := LoadServiceSession(ctx, q, cmd.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			return 200, result, AuditRecord{
				EventType: EventDraftItemQuantitySet,
				Details: draftItemAudit{
					ServiceSessionID: cmd.ServiceSessionID,
					OrderDraftID:     draft.OrderDraftID,
					DraftItemID:      item.ID,
					MenuItemID:       item.MenuItemID,
					Quantity:         quantity,
				},
			}, nil
		})
}
```

Create `internal/sales/draft_remove.go` with `RemoveDraftItemHandler`, following the same shape: lock the draft, lock the item, call `q.DeleteDraftItem`, reload the projection, and emit `EventDraftItemRemoved` with the same `draftItemAudit` details (carrying the removed quantity). The `ON DELETE CASCADE` on `order_draft_item_modifier_options` clears the option rows.

- [ ] **Step 5: Add the routes and HTTP handlers**

Register:

```go
v1.PATCH("/sales/service-sessions/:id/draft/items/:item_id/quantity", s.handleSetDraftItemQuantity,
	authn.RequireAuth(), authn.RequireCapability(CapSalesOperate))
v1.DELETE("/sales/service-sessions/:id/draft/items/:item_id", s.handleRemoveDraftItem,
	authn.RequireAuth(), authn.RequireCapability(CapSalesOperate))
```

Both handlers parse `:id` and `:item_id` and set the `json:"-"` command fields. `DELETE` returns 200 with the projection rather than 204, so a client sees the resulting draft without a follow-up read.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -p 1 -race -tags=integration ./internal/sales/ -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
make fmt
git add internal/sales/ sql/queries/sales.sql internal/database/sqlc/
git commit -m "feat(sales): set draft item quantity and remove draft items

Quantity is absolute and bounded 1..9999; zero is rejected because removal
is its own command. Draft items are locked scoped to their draft so one
Session cannot reach another's items.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

## Task 12: Composition Edits And Merging

The riskiest task in the plan. A composition edit can make one draft item identical to another, and the two must merge.

**Files:**
- Modify: `internal/sales/draft_edit.go`, `sql/queries/sales.sql`, `internal/sales/routes.go`, `internal/sales/http.go`
- Test: `internal/sales/draft_merge_integration_test.go`

**Interfaces:**
- Consumes: `lockEditableDraft`, `lockDraftItem`, `findDraftItemByComposition`, `replaceDraftItemOptions`, `validateModifierOptions`, `requireSizeForItem`, `NormalizePreparationNote`, `ModifierKeyFor`, `ValidateQuantity`.
- Produces:
  - `func applyCompositionChange(ctx, q, actor, draftID uuid.UUID, item sqlc.LockDraftItemRow, next composition) (mergeOutcome, error)`
  - `type SetDraftItemSizeHandler`, `type SetDraftItemNoteHandler`, `type SetDraftItemModifiersHandler`, each with its `New...` constructor and `Handle`.

- [ ] **Step 1: Add the query**

Append to `sql/queries/sales.sql`:

```sql
-- name: UpdateDraftItemComposition :exec
UPDATE order_draft_items
SET size_id = $2, preparation_note = $3, modifier_key = $4
WHERE id = $1;
```

Run: `make sqlc`

- [ ] **Step 2: Write the failing merge tests**

Create `internal/sales/draft_merge_integration_test.go`:

```go
//go:build integration

package sales_test

import (
	"context"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setSize(t *testing.T, runner *sales.Runner, actor sales.Actor,
	sessionID, itemID uuid.UUID, sizeID *uuid.UUID,
) (sales.ServiceSessionResponse, error) {
	t.Helper()
	_, resp, err := sales.NewSetDraftItemSizeHandler(runner).Handle(
		context.Background(), actor, sales.SetDraftItemSizeCommand{
			RequestID: uuid.New(), ServiceSessionID: sessionID,
			DraftItemID: itemID, SizeID: sizeID,
		})
	return resp, err
}

func setNote(t *testing.T, runner *sales.Runner, actor sales.Actor,
	sessionID, itemID uuid.UUID, note *string,
) (sales.ServiceSessionResponse, error) {
	t.Helper()
	_, resp, err := sales.NewSetDraftItemNoteHandler(runner).Handle(
		context.Background(), actor, sales.SetDraftItemNoteCommand{
			RequestID: uuid.New(), ServiceSessionID: sessionID,
			DraftItemID: itemID, PreparationNote: note,
		})
	return resp, err
}

func setModifiers(t *testing.T, runner *sales.Runner, actor sales.Actor,
	sessionID, itemID uuid.UUID, optionIDs []uuid.UUID,
) (sales.ServiceSessionResponse, error) {
	t.Helper()
	if optionIDs == nil {
		optionIDs = []uuid.UUID{}
	}
	_, resp, err := sales.NewSetDraftItemModifiersHandler(runner).Handle(
		context.Background(), actor, sales.SetDraftItemModifiersCommand{
			RequestID: uuid.New(), ServiceSessionID: sessionID,
			DraftItemID: itemID, ModifierOptionIDs: optionIDs,
		})
	return resp, err
}

// Editing the Size into an existing composition merges the two rows.
func TestSetSizeMergesIntoExistingComposition(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	_, session, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)

	itemID := seedSizedMenuItem(t, db, "Trà sữa")
	small := seedSize(t, db, itemID, "Nhỏ", 30000)
	large := seedSize(t, db, itemID, "Lớn", 40000)

	// Two lines: one Large, one Small.
	_, err = addItem(t, runner, actor, sales.AddDraftItemCommand{
		ServiceSessionID: session.ID, MenuItemID: itemID, SizeID: &large})
	require.NoError(t, err)
	resp, err := addItem(t, runner, actor, sales.AddDraftItemCommand{
		ServiceSessionID: session.ID, MenuItemID: itemID, SizeID: &small})
	require.NoError(t, err)
	require.Len(t, resp.Draft.Items, 2)

	var smallItemID, largeItemID uuid.UUID
	for _, it := range resp.Draft.Items {
		if *it.SizeID == small {
			smallItemID = it.ID
		} else {
			largeItemID = it.ID
		}
	}
	require.NotEqual(t, uuid.Nil, smallItemID)

	// Change the Small line to Large: it now matches the Large line.
	merged, err := setSize(t, runner, actor, session.ID, smallItemID, &large)
	require.NoError(t, err)

	require.Len(t, merged.Draft.Items, 1, "the two rows must merge into one")
	assert.Equal(t, largeItemID, merged.Draft.Items[0].ID,
		"the pre-existing row survives and the edited row is deleted")
	assert.Equal(t, int32(2), merged.Draft.Items[0].Quantity, "quantities are summed")

	assertAuditEvent(t, db, sales.EventDraftItemsMerged, 1)
	assertModifierKeyIntegrity(t, db)
}

// The same merge happens through the Preparation Note path.
func TestSetNoteMergesIntoExistingComposition(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	_, session, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)
	menuItemID := seedMenuItem(t, db, "Cà phê sữa", 25000)

	note := "ít đá"
	_, err = addItem(t, runner, actor, sales.AddDraftItemCommand{
		ServiceSessionID: session.ID, MenuItemID: menuItemID, PreparationNote: &note})
	require.NoError(t, err)
	resp, err := addItem(t, runner, actor, sales.AddDraftItemCommand{
		ServiceSessionID: session.ID, MenuItemID: menuItemID})
	require.NoError(t, err)
	require.Len(t, resp.Draft.Items, 2)

	var noteless uuid.UUID
	for _, it := range resp.Draft.Items {
		if it.PreparationNote == nil {
			noteless = it.ID
		}
	}

	merged, err := setNote(t, runner, actor, session.ID, noteless, &note)
	require.NoError(t, err)
	require.Len(t, merged.Draft.Items, 1)
	assert.Equal(t, int32(2), merged.Draft.Items[0].Quantity)
	assertAuditEvent(t, db, sales.EventDraftItemsMerged, 1)
}

// And through the Modifier Options path.
func TestSetModifiersMergesIntoExistingComposition(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	_, session, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)

	menuItemID, optionA, optionB := seedItemWithTwoOptions(t, db)

	_, err = addItem(t, runner, actor, sales.AddDraftItemCommand{
		ServiceSessionID: session.ID, MenuItemID: menuItemID,
		ModifierOptionIDs: &[]uuid.UUID{optionA}})
	require.NoError(t, err)
	resp, err := addItem(t, runner, actor, sales.AddDraftItemCommand{
		ServiceSessionID: session.ID, MenuItemID: menuItemID,
		ModifierOptionIDs: &[]uuid.UUID{optionB}})
	require.NoError(t, err)
	require.Len(t, resp.Draft.Items, 2)

	var withB uuid.UUID
	for _, it := range resp.Draft.Items {
		if len(it.SelectedModifierOptions) == 1 && it.SelectedModifierOptions[0].ID == optionB {
			withB = it.ID
		}
	}
	require.NotEqual(t, uuid.Nil, withB)

	merged, err := setModifiers(t, runner, actor, session.ID, withB, []uuid.UUID{optionA})
	require.NoError(t, err)
	require.Len(t, merged.Draft.Items, 1)
	assert.Equal(t, int32(2), merged.Draft.Items[0].Quantity)
	assertAuditEvent(t, db, sales.EventDraftItemsMerged, 1)
	assertModifierKeyIntegrity(t, db)
}

// A composition edit that collides with nothing is a plain update, with no
// merge event.
func TestCompositionEditWithoutCollisionIsAPlainUpdate(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	_, session, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)
	menuItemID := seedMenuItem(t, db, "Cà phê sữa", 25000)

	resp, err := addItem(t, runner, actor, sales.AddDraftItemCommand{
		ServiceSessionID: session.ID, MenuItemID: menuItemID})
	require.NoError(t, err)
	itemID := resp.Draft.Items[0].ID

	note := "nhiều đá"
	updated, err := setNote(t, runner, actor, session.ID, itemID, &note)
	require.NoError(t, err)

	require.Len(t, updated.Draft.Items, 1)
	assert.Equal(t, itemID, updated.Draft.Items[0].ID, "the row keeps its identity")
	require.NotNil(t, updated.Draft.Items[0].PreparationNote)
	assert.Equal(t, note, *updated.Draft.Items[0].PreparationNote)

	assertAuditEvent(t, db, sales.EventDraftItemsMerged, 0)
	assertAuditEvent(t, db, sales.EventDraftItemNoteSet, 1)
}

// A merge whose summed quantity would exceed the bound is rejected outright
// rather than silently clamped.
func TestMergeRejectsQuantityOverflow(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	_, session, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)
	menuItemID := seedMenuItem(t, db, "Cà phê sữa", 25000)

	note := "ít đá"
	first, err := addItem(t, runner, actor, sales.AddDraftItemCommand{
		ServiceSessionID: session.ID, MenuItemID: menuItemID, PreparationNote: &note})
	require.NoError(t, err)
	second, err := addItem(t, runner, actor, sales.AddDraftItemCommand{
		ServiceSessionID: session.ID, MenuItemID: menuItemID})
	require.NoError(t, err)
	require.Len(t, second.Draft.Items, 2)

	var noted, noteless uuid.UUID
	for _, it := range second.Draft.Items {
		if it.PreparationNote == nil {
			noteless = it.ID
		} else {
			noted = it.ID
		}
	}
	_ = first

	_, err = setQuantity(t, runner, actor, session.ID, noted, 9000)
	require.NoError(t, err)
	_, err = setQuantity(t, runner, actor, session.ID, noteless, 1500)
	require.NoError(t, err)

	_, err = setNote(t, runner, actor, session.ID, noteless, &note)
	require.ErrorIs(t, err, sales.ErrInvalidQuantity)

	// Nothing changed: both rows survive with their quantities.
	resp, err := sales.NewGetServiceSessionHandler(runner).
		Handle(context.Background(), actor, session.ID)
	require.NoError(t, err)
	assert.Len(t, resp.Draft.Items, 2)
}

// Clearing the Size is permitted; the draft tolerates incompleteness.
func TestSetSizeAcceptsNullToClear(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	_, session, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)

	menuItemID := seedSizedMenuItem(t, db, "Trà sữa")
	large := seedSize(t, db, menuItemID, "Lớn", 40000)

	resp, err := addItem(t, runner, actor, sales.AddDraftItemCommand{
		ServiceSessionID: session.ID, MenuItemID: menuItemID, SizeID: &large})
	require.NoError(t, err)

	cleared, err := setSize(t, runner, actor, session.ID, resp.Draft.Items[0].ID, nil)
	require.NoError(t, err)
	assert.Nil(t, cleared.Draft.Items[0].SizeID)
	assert.Nil(t, cleared.Draft.Items[0].PriceVND)
}

// Unlike add, the modifiers command takes an empty list literally and applies
// no defaults.
func TestSetModifiersAppliesNoDefaults(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	_, session, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)

	menuItemID, defaultOptionID := seedItemWithDefaultOption(t, db)

	resp, err := addItem(t, runner, actor, sales.AddDraftItemCommand{
		ServiceSessionID: session.ID, MenuItemID: menuItemID})
	require.NoError(t, err)
	require.Len(t, resp.Draft.Items[0].SelectedModifierOptions, 1)
	_ = defaultOptionID

	cleared, err := setModifiers(t, runner, actor, session.ID, resp.Draft.Items[0].ID, nil)
	require.NoError(t, err)
	assert.Empty(t, cleared.Draft.Items[0].SelectedModifierOptions,
		"an empty list must be taken literally, not replaced by defaults")
	assertModifierKeyIntegrity(t, db)
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -p 1 -tags=integration ./internal/sales/ -run 'TestSetSize|TestSetNote|TestSetModifiers|TestComposition|TestMergeRejects' -v`
Expected: FAIL — `undefined: sales.NewSetDraftItemSizeHandler`.

- [ ] **Step 4: Write the merge helper**

Append to `internal/sales/draft_edit.go`:

```go
// composition is a draft item's identity: two items with the same composition
// are the same line.
type composition struct {
	SizeID      uuid.NullUUID
	Note        sql.NullString
	ModifierKey string
	OptionIDs   []uuid.UUID
}

// mergeOutcome reports what applyCompositionChange did, so the caller can emit
// the right audit events.
type mergeOutcome struct {
	// SurvivingItemID is the edited row when nothing collided, or the
	// pre-existing row when the two merged.
	SurvivingItemID uuid.UUID
	Quantity        int32
	Merged          bool
	AbsorbedItemID  uuid.UUID
	AbsorbedQty     int32
}

// applyCompositionChange moves a draft item to a new composition, merging it
// into an existing line when one already has that composition.
//
// This is the behavior most easily lost in a port: it is reached from three
// different commands and produces a result none of their names suggests — the
// edited item's id disappears and another row's quantity grows. Every caller
// returns the reloaded projection so the client sees it immediately.
//
// The caller must already hold the draft row lock, so no competing row can
// appear between the lookup and the write.
func applyCompositionChange(ctx context.Context, q *sqlc.Queries,
	draftID uuid.UUID, item sqlc.LockDraftItemRow, next composition,
) (mergeOutcome, error) {
	existing, err := findDraftItemByCompositionExcluding(ctx, q, draftID,
		item.MenuItemID, next, item.ID)
	if err != nil {
		return mergeOutcome{}, err
	}

	if existing != nil {
		total := existing.Quantity + item.Quantity
		// Reject rather than clamp: silently capping a quantity would charge
		// the customer for fewer units than staff entered.
		if err := ValidateQuantity(total); err != nil {
			return mergeOutcome{}, err
		}
		if _, err := q.SetDraftItemQuantity(ctx, sqlc.SetDraftItemQuantityParams{
			ID: existing.ID, Quantity: total,
		}); err != nil {
			return mergeOutcome{}, fmt.Errorf("merge draft item quantity: %w", err)
		}
		// The absorbed row's option rows cascade away. The survivor's are
		// already correct: the two rows agreed on composition by definition.
		if err := q.DeleteDraftItem(ctx, item.ID); err != nil {
			return mergeOutcome{}, fmt.Errorf("delete absorbed draft item: %w", err)
		}
		return mergeOutcome{
			SurvivingItemID: existing.ID,
			Quantity:        total,
			Merged:          true,
			AbsorbedItemID:  item.ID,
			AbsorbedQty:     item.Quantity,
		}, nil
	}

	if err := q.UpdateDraftItemComposition(ctx, sqlc.UpdateDraftItemCompositionParams{
		ID:              item.ID,
		SizeID:          next.SizeID,
		PreparationNote: next.Note,
		ModifierKey:     next.ModifierKey,
	}); err != nil {
		return mergeOutcome{}, fmt.Errorf("update draft item composition: %w", err)
	}
	if err := replaceDraftItemOptions(ctx, q, item.ID, next.OptionIDs); err != nil {
		return mergeOutcome{}, err
	}
	return mergeOutcome{SurvivingItemID: item.ID, Quantity: item.Quantity}, nil
}

type draftItemsMergedAudit struct {
	ServiceSessionID uuid.UUID `json:"service_session_id"`
	OrderDraftID     uuid.UUID `json:"order_draft_id"`
	SurvivingItemID  uuid.UUID `json:"surviving_draft_item_id"`
	AbsorbedItemID   uuid.UUID `json:"absorbed_draft_item_id"`
	AbsorbedQuantity int32     `json:"absorbed_quantity"`
	ResultingQuantity int32    `json:"resulting_quantity"`
}

// writeMergeAudit records a merge alongside the command's own event, because a
// quantity changed that no command asked to change.
func writeMergeAudit(ctx context.Context, q *sqlc.Queries, actor Actor,
	sessionID, draftID uuid.UUID, outcome mergeOutcome,
) error {
	if !outcome.Merged {
		return nil
	}
	details, err := json.Marshal(draftItemsMergedAudit{
		ServiceSessionID:  sessionID,
		OrderDraftID:      draftID,
		SurvivingItemID:   outcome.SurvivingItemID,
		AbsorbedItemID:    outcome.AbsorbedItemID,
		AbsorbedQuantity:  outcome.AbsorbedQty,
		ResultingQuantity: outcome.Quantity,
	})
	if err != nil {
		return fmt.Errorf("marshal merge audit: %w", err)
	}
	if _, err := q.InsertAuditEvent(ctx, sqlc.InsertAuditEventParams{
		EventType:  EventDraftItemsMerged,
		ActorID:    uuid.NullUUID{UUID: actor.StaffID, Valid: true},
		SessionID:  uuid.NullUUID{UUID: actor.SessionID, Valid: true},
		Details:    details,
		OccurredAt: time.Now(),
	}); err != nil {
		return fmt.Errorf("insert merge audit event: %w", err)
	}
	return nil
}
```

Add `encoding/json` and `time` to the file's imports.

> **Note for the implementer:** `findDraftItemByCompositionExcluding` is
> `findDraftItemByComposition` plus an `id <> $n` clause, so it never matches
> the row being edited. Add a `FindDraftItemByCompositionExcluding` query
> alongside the existing one rather than overloading it with a nullable
> exclusion parameter.

- [ ] **Step 5: Write the three handlers**

All three follow one shape: lock the draft, lock the item, build the next
composition from the current one with a single component replaced, call
`applyCompositionChange`, write the merge audit when one happened, reload the
projection. Write `SetDraftItemSizeHandler` in full first; the other two are
the same function with a different component and audit event.

Append to `internal/sales/draft_edit.go`:

```go
type setSizeFingerprint struct {
	ServiceSessionID uuid.UUID  `json:"service_session_id"`
	DraftItemID      uuid.UUID  `json:"draft_item_id"`
	SizeID           *uuid.UUID `json:"size_id"`
}

// SetDraftItemSizeHandler sets or clears a draft item's Size.
type SetDraftItemSizeHandler struct{ runner *Runner }

// NewSetDraftItemSizeHandler creates a new SetDraftItemSizeHandler.
func NewSetDraftItemSizeHandler(runner *Runner) *SetDraftItemSizeHandler {
	return &SetDraftItemSizeHandler{runner: runner}
}

// Handle changes the Size, merging the item into another line when that makes
// the two compositions identical.
//
// A nil SizeID clears the Size and is permitted: the draft tolerates an
// incomplete configuration, and Commit in 5B is where a required Size is
// enforced.
func (h *SetDraftItemSizeHandler) Handle(ctx context.Context, actor Actor,
	cmd SetDraftItemSizeCommand,
) (int, ServiceSessionResponse, error) {
	var zero ServiceSessionResponse

	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpSetDraftItemSize,
		Fingerprint: setSizeFingerprint{
			ServiceSessionID: cmd.ServiceSessionID,
			DraftItemID:      cmd.DraftItemID,
			SizeID:           cmd.SizeID,
		},
		Required: []string{CapSalesOperate},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, ServiceSessionResponse, AuditRecord, error) {
			q := mc.Queries

			draft, err := lockEditableDraft(ctx, q, cmd.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			item, err := lockDraftItem(ctx, q, draft.OrderDraftID, cmd.DraftItemID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			if cmd.SizeID != nil {
				if err := requireSizeForItem(ctx, q, item.MenuItemID, *cmd.SizeID); err != nil {
					return 0, zero, AuditRecord{}, err
				}
			}

			// Carry the untouched components forward. The option ids come from
			// the authoritative rows, not from modifier_key, which is derived.
			optionIDs, err := q.ListDraftItemOptionIDs(ctx, item.ID)
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("load draft item options: %w", err)
			}

			next := composition{
				SizeID:      nullUUID(cmd.SizeID),
				Note:        item.PreparationNote,
				ModifierKey: item.ModifierKey,
				OptionIDs:   optionIDs,
			}

			outcome, err := applyCompositionChange(ctx, q, draft.OrderDraftID, item, next)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			if err := writeMergeAudit(ctx, q, actor,
				cmd.ServiceSessionID, draft.OrderDraftID, outcome); err != nil {
				return 0, zero, AuditRecord{}, err
			}

			result, err := LoadServiceSession(ctx, q, cmd.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			return 200, result, AuditRecord{
				EventType: EventDraftItemSizeSet,
				Details: draftItemAudit{
					ServiceSessionID: cmd.ServiceSessionID,
					OrderDraftID:     draft.OrderDraftID,
					DraftItemID:      outcome.SurvivingItemID,
					MenuItemID:       item.MenuItemID,
					Quantity:         outcome.Quantity,
				},
			}, nil
		})
}
```

`SetDraftItemNoteHandler` is the same function with three changes: its
fingerprint carries `PreparationNote *string` passed through
`trimmedForFingerprint`; it calls `NormalizePreparationNote(cmd.PreparationNote)`
in place of the Size validation and uses the result as `next.Note`, keeping
`next.SizeID = item.SizeID`; and it emits `EventDraftItemNoteSet`.

`SetDraftItemModifiersHandler` differs more, so note each point:

- Its fingerprint carries `ModifierOptionIDs []uuid.UUID` sorted with
  `SortedUUIDs`. It is a plain slice, not a pointer: unlike the add command,
  this list is required and an empty one is meaningful on its own.
- It calls `validateModifierOptions(ctx, q, item.MenuItemID, cmd.ModifierOptionIDs)`
  and applies **no** defaults. An empty list means the customer declined every
  option and is taken literally.
- `next` keeps `item.SizeID` and `item.PreparationNote`, sets
  `ModifierKey: ModifierKeyFor(cmd.ModifierOptionIDs)`, and
  `OptionIDs: cmd.ModifierOptionIDs`.
- It emits `EventDraftItemModifiersSet`.

- [ ] **Step 6: Add the routes and HTTP handlers**

```go
v1.PATCH("/sales/service-sessions/:id/draft/items/:item_id/size", s.handleSetDraftItemSize,
	authn.RequireAuth(), authn.RequireCapability(CapSalesOperate))
v1.PATCH("/sales/service-sessions/:id/draft/items/:item_id/preparation-note", s.handleSetDraftItemNote,
	authn.RequireAuth(), authn.RequireCapability(CapSalesOperate))
v1.PATCH("/sales/service-sessions/:id/draft/items/:item_id/modifiers", s.handleSetDraftItemModifiers,
	authn.RequireAuth(), authn.RequireCapability(CapSalesOperate))
```

Each Swagger `@Description` must state that the edit may merge the item into
another line of the same composition, so an integrator is not surprised when
the id they held disappears.

- [ ] **Step 7: Run the tests to verify they pass**

Run: `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -p 1 -race -tags=integration ./internal/sales/ -v`
Expected: PASS, all three merge paths.

- [ ] **Step 8: Commit**

```bash
make fmt
git add internal/sales/ sql/queries/sales.sql internal/database/sqlc/
git commit -m "feat(sales): composition edits with draft item merging

Setting Size, Preparation Note, or Modifier Options can make one draft item
identical to another; the two merge, summing quantities and deleting the
edited row. Each merge writes its own audit event, since a quantity changed
that no command asked to change. An overflowing merge is rejected, not
clamped.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

## Task 13: List Active Service Sessions

Without this read, a cashier who opens a Session has no way to find it again
short of remembering its id.

**Files:**
- Modify: `internal/sales/reads.go`, `internal/sales/routes.go`, `internal/sales/http.go`
- Test: `internal/sales/list_sessions_integration_test.go`

**Interfaces:**
- Consumes: `ExecuteRead`, `LoadServiceSession`, `ListActiveServiceSessions` (added in Task 5).
- Produces: `type ListActiveSessionsHandler`, `NewListActiveSessionsHandler`, `Handle(ctx, actor) ([]ServiceSessionResponse, error)`

- [ ] **Step 1: Write the failing test**

Create `internal/sales/list_sessions_integration_test.go`:

```go
//go:build integration

package sales_test

import (
	"context"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListActiveSessionsOrdersByCreation(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)

	_, first, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)
	_, second, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)

	got, err := sales.NewListActiveSessionsHandler(runner).Handle(context.Background(), actor)
	require.NoError(t, err)

	require.Len(t, got, 2)
	assert.Equal(t, first.ID, got[0].ID)
	assert.Equal(t, second.ID, got[1].ID)
}

func TestListActiveSessionsExcludesClosed(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)

	_, session, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)

	// 5D owns closure; the test drives the state directly.
	_, err = db.Exec(`UPDATE service_sessions SET state = 'CLOSED' WHERE id = $1`, session.ID)
	require.NoError(t, err)

	got, err := sales.NewListActiveSessionsHandler(runner).Handle(context.Background(), actor)
	require.NoError(t, err)
	assert.Empty(t, got)
}

// Each listed Session carries its full projection, including its draft, so the
// cashier's open-tabs screen needs one request rather than one per Session.
func TestListActiveSessionsIncludesDrafts(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	menuItemID := seedMenuItem(t, db, "Cà phê sữa", 25000)

	_, session, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)
	_, err = addItem(t, runner, actor, sales.AddDraftItemCommand{
		ServiceSessionID: session.ID, MenuItemID: menuItemID})
	require.NoError(t, err)

	got, err := sales.NewListActiveSessionsHandler(runner).Handle(context.Background(), actor)
	require.NoError(t, err)

	require.Len(t, got, 1)
	require.NotNil(t, got[0].Draft)
	assert.Len(t, got[0].Draft.Items, 1)
}

func TestListActiveSessionsDeniesBarista(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"BARISTA"})

	_, err := sales.NewListActiveSessionsHandler(runner).Handle(context.Background(), actor)
	require.ErrorIs(t, err, sales.ErrForbidden)
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -p 1 -tags=integration ./internal/sales/ -run TestListActiveSessions -v`
Expected: FAIL — `undefined: sales.NewListActiveSessionsHandler`.

- [ ] **Step 3: Write the implementation**

Append to `internal/sales/reads.go`:

```go
// ListActiveSessionsHandler serves the cashier's open-Sessions list.
type ListActiveSessionsHandler struct{ runner *Runner }

// NewListActiveSessionsHandler creates a new ListActiveSessionsHandler.
func NewListActiveSessionsHandler(runner *Runner) *ListActiveSessionsHandler {
	return &ListActiveSessionsHandler{runner: runner}
}

// Handle returns every ACTIVE Service Session with its full projection.
//
// Each Session carries its draft, so the open-tabs screen needs one request
// rather than one per Session. A single cafe has a handful of open Sessions at
// a time, so the per-Session queries are bounded in practice; if that ever
// stops being true, the fix is a batched projection query, not pagination that
// would hide open tabs from staff.
func (h *ListActiveSessionsHandler) Handle(ctx context.Context, actor Actor) (
	[]ServiceSessionResponse, error,
) {
	return ExecuteRead(ctx, h.runner, actor, OpListServiceSessions, CapSalesOperate,
		func(q *sqlc.Queries) ([]ServiceSessionResponse, error) {
			rows, err := q.ListActiveServiceSessions(ctx)
			if err != nil {
				return nil, fmt.Errorf("list active service sessions: %w", err)
			}
			out := make([]ServiceSessionResponse, 0, len(rows))
			for _, row := range rows {
				session, err := LoadServiceSession(ctx, q, row.ID)
				if err != nil {
					return nil, err
				}
				out = append(out, session)
			}
			return out, nil
		})
}
```

Add `fmt` to the file's imports.

- [ ] **Step 4: Add the route and HTTP handler**

```go
v1.GET("/sales/service-sessions", s.handleListActiveSessions,
	authn.RequireAuth(), authn.RequireCapability(CapSalesOperate))
```

```go
//	@Summary		List active Service Sessions
//	@Description	Returns every ACTIVE Service Session with its full projection, ordered by creation. This is the cashier's open-tabs view.
//	@Tags			sales
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	response.APIResponse{data=[]ServiceSessionResponse}
//	@Failure		401	{object}	response.APIResponse
//	@Failure		403	{object}	response.APIResponse
//	@Router			/sales/service-sessions [get]
```

> **Note for the implementer:** register this route **before**
> `/sales/service-sessions/:id` if Echo's router shows any ambiguity. Echo
> normally prefers static segments over parameters, so both orders work, but
> confirm with `TestRouteRegistration` in Task 14 rather than assuming.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -p 1 -race -tags=integration ./internal/sales/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
make fmt
git add internal/sales/
git commit -m "feat(sales): list active Service Sessions

The cashier's open-tabs read. Each Session carries its full projection so
the screen needs one request rather than one per Session.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

## Task 14: HTTP Suite, Decision Records, And Full Verification

**Files:**
- Create: `internal/sales/routes_test.go`, `internal/sales/sales_integration_test.go`
- Modify: `spec/decisions.md`, `MIGRATE_PLAN.md`, `docs/` (regenerated)

- [ ] **Step 1: Write the route registration test**

Create `internal/sales/routes_test.go` (unit, no build tag), modeled on
`internal/shift/routes_test.go`:

```go
package sales

import (
	"fmt"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRouteRegistration pins the exact router surface.
//
// The set comparison is deliberate rather than a length check: mounting on a
// sub-group with group-level middleware would silently add two
// echo_route_not_found catch-all routes, which is exactly the mistake the
// comment in RegisterRoutes warns about.
func TestRouteRegistration(t *testing.T) {
	want := map[string]bool{
		"GET /api/v1/sales/service-sessions":                                              true,
		"GET /api/v1/sales/service-sessions/:id":                                          true,
		"POST /api/v1/sales/service-sessions/takeaway":                                    true,
		"POST /api/v1/sales/service-sessions/dine-in":                                     true,
		"PUT /api/v1/sales/service-sessions/:id/tables":                                   true,
		"POST /api/v1/sales/service-sessions/:id/draft/items":                             true,
		"PATCH /api/v1/sales/service-sessions/:id/draft/items/:item_id/quantity":          true,
		"PATCH /api/v1/sales/service-sessions/:id/draft/items/:item_id/size":              true,
		"PATCH /api/v1/sales/service-sessions/:id/draft/items/:item_id/preparation-note":  true,
		"PATCH /api/v1/sales/service-sessions/:id/draft/items/:item_id/modifiers":         true,
		"DELETE /api/v1/sales/service-sessions/:id/draft/items/:item_id":                  true,
	}

	e := echo.New()
	v1 := e.Group("/api/v1")

	// NewSlices needs no working database to register routes: the Runner only
	// stores the handle.
	slices := NewSlices(nil, nil)
	slices.RegisterRoutes(v1, &auth.Middleware{})

	got := make(map[string]bool)
	for _, route := range e.Routes() {
		got[fmt.Sprintf("%s %s", route.Method, route.Path)] = true
	}

	for route := range want {
		assert.True(t, got[route], "route %q must be registered", route)
	}
	require.Equal(t, len(want), len(got),
		"the router must expose exactly the Sales routes and nothing else; got %v", got)
}
```

> **Note for the implementer:** `auth.Middleware` may not be constructible as a
> bare `&auth.Middleware{}`. Read `internal/shift/routes_test.go` and use
> whatever it does to obtain a middleware value for this test; if it builds one
> through `auth.NewSlices`, do the same here.

- [ ] **Step 2: Write the end-to-end HTTP suite**

Create `internal/sales/sales_integration_test.go`, modeled on
`internal/shift/shift_integration_test.go`. It must cover:

- A `newTestServer` helper mounting auth and sales routes exactly as
  `cmd/api/main.go` does, and a `signIn` helper that uses the real sign-in
  endpoint so the middleware path is exercised.
- One full happy path over HTTP: sign in as `CASHIER`, open a Shift through the
  Shift API, open a Takeaway Session, add an item, set its quantity, set a note,
  remove it, and read the Session back. Assert the envelope shape and status at
  each step.
- Every route denying a `BARISTA` with 403 and code `NOT_AUTHORIZED`.
- An unauthenticated request to every route returning 401.
- A malformed UUID in `:id` and in `:item_id` returning 400.
- A missing `request_id` returning 400.
- `DELETE` returning 200 with the projection body, not 204.
- Empty collections serialized as `[]` in the raw JSON, checked as a string
  rather than after unmarshalling, since unmarshalling hides the difference.
- An add request with `"modifier_option_ids": []` and one omitting the field
  entirely producing different drafts, proving the distinction survives JSON
  decoding over the wire.

- [ ] **Step 3: Run the whole suite**

Run: `make check`
Expected: `fmt`, `vet`, `lint`, and unit tests all pass.

Run: `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -race -p 1 -tags=integration ./...`
Expected: PASS across every package. Auth, Catalog, Tables, and Shift suites
must be unchanged.

Run: `go list -deps ./internal/sales/ | grep -E 'pos-cafe/internal/(catalog|tables|shift)'`
Expected: **no output.**

- [ ] **Step 4: Regenerate Swagger**

Run: `make swagger`
Run: `go build ./...`

Open `/swagger/index.html` against a running server and confirm all eleven
Sales operations appear with Bearer security and that the `checks`, `orders`,
and `preparation_units` descriptions name the sub-phase that fills them.

- [ ] **Step 5: Append the decision records**

Add ADR-010, ADR-011, and ADR-012 to `spec/decisions.md`, using the exact text
from section 14 of the spec and matching the formatting of ADR-008 and ADR-009.

- [ ] **Step 6: Update the roadmap**

In `MIGRATE_PLAN.md`, under the Phase 5 heading, add the sub-phase table and a
link to this spec and plan, matching how Phases 2, 3, and 4 record theirs. Add
the note that the Phase 5 checklist below it predates the canonical source
review and is superseded.

Mark the **5A** sub-phase complete. Do **not** mark the Phase 5 tracker row
complete: that happens when 5D lands.

- [ ] **Step 7: Commit**

```bash
make fmt
git add internal/sales/ spec/decisions.md MIGRATE_PLAN.md docs/
git commit -m "feat(sales): complete Phase 5A with HTTP suite and decision records

Adds route registration and end-to-end HTTP coverage, records ADR-010
(Phase 5 decomposition), ADR-011 (Shift-scoped Service Numbers), and
ADR-012 (Catalog resolution through Sales' own SQL), and links the
sub-phase specs from the roadmap.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

- [ ] **Step 8: Verify against the spec's acceptance criteria**

Walk section 15 of the spec and confirm each of the sixteen criteria. Any that
fails is a bug in this plan's execution, not a criterion to relax.
