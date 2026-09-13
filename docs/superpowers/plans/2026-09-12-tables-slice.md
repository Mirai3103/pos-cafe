# Tables Slice (Phase 3) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `internal/tables`, delivering one authorized overview read reporting Table occupancy plus three idempotent, audited commands (create, rename, set Availability).

**Architecture:** One Go package following the `internal/catalog` vertical-slice pattern exactly: a `Runner` owning transaction orchestration, generic `ExecuteMutation`/`ExecuteRead` helpers, per-operation handler types, a `Slices` aggregate, and `RegisterRoutes(v1, authn)`. Every mutation reloads authority inside its transaction, claims an actor-scoped idempotency key in the shared `idempotency_keys` table, locks the Table row, mutates, writes one audit event, and commits atomically. Migration `000006` also provisions two Sales-owned tables (`service_sessions`, `table_assignments`) so the canonical overview read ships complete in Phase 3.

**Tech Stack:** Go 1.26+, Echo v4, PostgreSQL via `database/sql` + pgx stdlib, sqlc-generated queries, `stretchr/testify`, `swaggo/swag` for OpenAPI 2.0.

**Spec:** `docs/superpowers/specs/2026-09-12-tables-slice-design.md`

## Global Constraints

- Go module path is `github.com/Mirai3103/pos-cafe`. All internal imports use this prefix.
- `internal/tables` MUST NOT import `internal/sales`. It MAY import `internal/auth` for `auth.DeriveCapabilities`, `auth.SessionStateLocked`, and `auth.GetInactivityTimeout`, as `internal/catalog` already does.
- `internal/tables` MUST NOT import or call `auth.ExecuteWithIdempotency` or any other auth-owned idempotency helper. It owns its own executor.
- Idempotency uses the shared `idempotency_keys` table from migration `000002`. Do NOT create a new idempotency table.
- Audit events use the shared `audit_events` table from migration `000003`.
- Capability strings, verbatim: `sales.operate` (overview read), `tables.administer` (all three commands).
- Idempotency `action` values, verbatim: `tables.create_table`, `tables.rename_table`, `tables.set_table_availability`. Column is `VARCHAR(50)`; all three fit.
- Audit event types, verbatim: `TABLE_CREATED`, `TABLE_RENAMED`, `TABLE_AVAILABILITY_CHANGED`, `tables.authorization_denied`.
- Table name: trimmed, internal whitespace runs collapsed to one space, 1–60 **Unicode code points** (use `utf8.RuneCountInString`, never `len`). Uniqueness key is `strings.ToLower` of that result.
- No Tables command verifies a Manager PIN. No Tables entity has Retirement, delete, capacity, or display order.
- Money is irrelevant to this slice; no price columns exist here.
- Integration tests carry the `//go:build integration` tag and run with `-p 1`.
- All HTTP responses use the existing `{success,data,error}` envelope via `internal/response`.

---

### Task 1: Migration and schema constraints

**Files:**
- Create: `internal/database/migrations/000006_create_tables_slice.sql`
- Test: `internal/tables/schema_integration_test.go`

**Interfaces:**
- Consumes: nothing (first task).
- Produces: tables `tables`, `service_sessions`, `table_assignments` with the exact column names used by every later task.

- [ ] **Step 1: Write the failing test**

Create `internal/tables/schema_integration_test.go`:

```go
//go:build integration

package tables_test

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/database"
	"github.com/stretchr/testify/require"
)

func openSchemaTestDB(t *testing.T) *sql.DB {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	require.Contains(t, url, "_test")
	db, err := database.Open(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

func TestTablesSchemaExists(t *testing.T) {
	db := openSchemaTestDB(t)
	ctx := context.Background()

	for _, name := range []string{"tables", "service_sessions", "table_assignments"} {
		var exists bool
		err := db.QueryRowContext(ctx,
			`SELECT EXISTS (SELECT 1 FROM information_schema.tables
			 WHERE table_schema = 'public' AND table_name = $1)`, name).Scan(&exists)
		require.NoError(t, err)
		require.True(t, exists, "table %q must exist", name)
	}
}

func TestTableNameConstraints(t *testing.T) {
	db := openSchemaTestDB(t)
	ctx := context.Background()

	// Untrimmed name is rejected.
	_, err := db.ExecContext(ctx,
		`INSERT INTO tables (name, normalized_name) VALUES (' Ban 1 ', ' ban 1 ')`)
	require.Error(t, err, "untrimmed name must violate table_name_valid")

	// normalized_name must equal lower(name).
	_, err = db.ExecContext(ctx,
		`INSERT INTO tables (name, normalized_name) VALUES ('Ban 1', 'BAN 1')`)
	require.Error(t, err, "mismatched normalized_name must be rejected")

	// Empty name is rejected.
	_, err = db.ExecContext(ctx,
		`INSERT INTO tables (name, normalized_name) VALUES ('', '')`)
	require.Error(t, err, "empty name must be rejected")

	// A valid row inserts, and a duplicate normalized name is rejected.
	var id string
	err = db.QueryRowContext(ctx,
		`INSERT INTO tables (name, normalized_name) VALUES ('Ban Schema A', 'ban schema a')
		 RETURNING id`).Scan(&id)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM tables WHERE id = $1`, id) })

	_, err = db.ExecContext(ctx,
		`INSERT INTO tables (name, normalized_name) VALUES ('Ban Schema A', 'ban schema a')`)
	require.Error(t, err, "duplicate normalized_name must be rejected")
}

func TestTableAssignmentReleaseEvidenceConstraint(t *testing.T) {
	db := openSchemaTestDB(t)
	ctx := context.Background()

	var staffID string
	err := db.QueryRowContext(ctx,
		`INSERT INTO staff_identities (display_name, login_code, pin_hash, enabled)
		 VALUES ('Schema Actor', 'SCHEMA1', '', true) RETURNING id`).Scan(&staffID)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM staff_identities WHERE id = $1`, staffID) })

	var tableID string
	err = db.QueryRowContext(ctx,
		`INSERT INTO tables (name, normalized_name) VALUES ('Ban Schema B', 'ban schema b')
		 RETURNING id`).Scan(&tableID)
	require.NoError(t, err)

	var sessionID string
	err = db.QueryRowContext(ctx,
		`INSERT INTO service_sessions (service_number, service_mode, state, created_by_staff_identity_id)
		 VALUES ('SCH001', 'DINE_IN', 'ACTIVE', $1) RETURNING id`, staffID).Scan(&sessionID)
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM table_assignments WHERE table_id = $1`, tableID)
		_, _ = db.ExecContext(ctx, `DELETE FROM service_sessions WHERE id = $1`, sessionID)
		_, _ = db.ExecContext(ctx, `DELETE FROM tables WHERE id = $1`, tableID)
	})

	// released_at without released_by violates the evidence check.
	_, err = db.ExecContext(ctx,
		`INSERT INTO table_assignments
		   (table_id, service_session_id, assigned_by_staff_identity_id, sequence, released_at)
		 VALUES ($1, $2, $3, 1, now())`, tableID, sessionID, staffID)
	require.Error(t, err, "released_at without released_by must be rejected")

	// A current assignment inserts.
	_, err = db.ExecContext(ctx,
		`INSERT INTO table_assignments
		   (table_id, service_session_id, assigned_by_staff_identity_id, sequence)
		 VALUES ($1, $2, $3, 1)`, tableID, sessionID, staffID)
	require.NoError(t, err)

	// A second current assignment for the same pair violates the partial unique index.
	_, err = db.ExecContext(ctx,
		`INSERT INTO table_assignments
		   (table_id, service_session_id, assigned_by_staff_identity_id, sequence)
		 VALUES ($1, $2, $3, 2)`, tableID, sessionID, staffID)
	require.Error(t, err, "duplicate current assignment must be rejected")
}

func TestServiceSessionNumberConstraint(t *testing.T) {
	db := openSchemaTestDB(t)
	ctx := context.Background()

	var staffID string
	err := db.QueryRowContext(ctx,
		`INSERT INTO staff_identities (display_name, login_code, pin_hash, enabled)
		 VALUES ('Schema Actor 2', 'SCHEMA2', '', true) RETURNING id`).Scan(&staffID)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM staff_identities WHERE id = $1`, staffID) })

	// Lowercase and wrong-length service numbers are rejected.
	_, err = db.ExecContext(ctx,
		`INSERT INTO service_sessions (service_number, created_by_staff_identity_id)
		 VALUES ('abc123', $1)`, staffID)
	require.Error(t, err, "lowercase service_number must be rejected")

	_, err = db.ExecContext(ctx,
		`INSERT INTO service_sessions (service_number, created_by_staff_identity_id)
		 VALUES ('AB12', $1)`, staffID)
	require.Error(t, err, "short service_number must be rejected")

	// An invalid state is rejected.
	_, err = db.ExecContext(ctx,
		`INSERT INTO service_sessions (service_number, state, created_by_staff_identity_id)
		 VALUES ('SCH002', 'PENDING', $1)`, staffID)
	require.Error(t, err, "unknown state must be rejected")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -p 1 -tags=integration ./internal/tables/... -run TestTablesSchemaExists -v`

Expected: FAIL — the `tables` table does not exist.

- [ ] **Step 3: Write the migration**

Create `internal/database/migrations/000006_create_tables_slice.sql`:

```sql
-- Phase 3: Tables & Floor Layout.
--
-- `tables` is owned by internal/tables.
--
-- `service_sessions` and `table_assignments` are provisioned here so that the
-- canonical Table overview read (which reports the active Service Sessions
-- occupying each Table) can ship complete in Phase 3. Business ownership of
-- both belongs to internal/sales in Phase 5; Phase 3 only reads them.
-- See ADR-006 in spec/decisions.md.

CREATE TABLE IF NOT EXISTS tables (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    normalized_name TEXT NOT NULL,
    available BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT table_name_valid
        CHECK (char_length(name) BETWEEN 1 AND 60 AND name = btrim(name)),
    CONSTRAINT table_normalized_name_valid
        CHECK (normalized_name = lower(name))
);

CREATE UNIQUE INDEX IF NOT EXISTS table_normalized_name_unique
    ON tables (normalized_name);

CREATE INDEX IF NOT EXISTS idx_tables_created_at
    ON tables (created_at ASC, id ASC);

-- Owned by internal/sales (Phase 5). Provisioned early for the Tables overview.
-- Phase 5 adds: ALTER TABLE service_sessions
--   ADD COLUMN sales_shift_id UUID NOT NULL REFERENCES sales_shifts(id);
CREATE TABLE IF NOT EXISTS service_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    service_number TEXT NOT NULL,
    service_mode TEXT NOT NULL DEFAULT 'TAKEAWAY',
    state TEXT NOT NULL DEFAULT 'ACTIVE',
    created_by_staff_identity_id UUID NOT NULL REFERENCES staff_identities(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT service_session_number_valid
        CHECK (service_number ~ '^[A-Z0-9]{6}$'),
    CONSTRAINT service_session_mode_valid
        CHECK (service_mode IN ('DINE_IN', 'TAKEAWAY')),
    CONSTRAINT service_session_state_valid
        CHECK (state IN ('ACTIVE', 'COMPLETED', 'CANCELLED'))
);

CREATE UNIQUE INDEX IF NOT EXISTS service_session_service_number_unique
    ON service_sessions (service_number);

COMMENT ON TABLE service_sessions IS
    'Owned by internal/sales (Phase 5). Provisioned in Phase 3 for the Tables overview read; sales_shift_id is added in Phase 5.';

-- Owned by internal/sales (Phase 5). Provisioned early for the Tables overview.
CREATE TABLE IF NOT EXISTS table_assignments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    table_id UUID NOT NULL REFERENCES tables(id) ON DELETE RESTRICT,
    service_session_id UUID NOT NULL REFERENCES service_sessions(id) ON DELETE RESTRICT,
    assigned_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    assigned_by_staff_identity_id UUID NOT NULL REFERENCES staff_identities(id) ON DELETE RESTRICT,
    sequence INTEGER NOT NULL,
    released_at TIMESTAMPTZ,
    released_by_staff_identity_id UUID REFERENCES staff_identities(id) ON DELETE RESTRICT,
    CONSTRAINT table_assignment_release_evidence_valid
        CHECK (
            (released_at IS NULL AND released_by_staff_identity_id IS NULL)
            OR (released_at IS NOT NULL AND released_by_staff_identity_id IS NOT NULL)
        )
);

CREATE UNIQUE INDEX IF NOT EXISTS table_assignment_current_unique
    ON table_assignments (table_id, service_session_id)
    WHERE released_at IS NULL;

CREATE INDEX IF NOT EXISTS table_assignment_table_index
    ON table_assignments (table_id);

CREATE INDEX IF NOT EXISTS table_assignment_service_session_index
    ON table_assignments (service_session_id);

COMMENT ON TABLE table_assignments IS
    'Owned by internal/sales (Phase 5). Provisioned in Phase 3 for the Tables overview read.';
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -p 1 -tags=integration ./internal/tables/... -v`

Expected: PASS — all four schema tests.

If the test database already ran earlier migrations, drop and recreate it first: `make docker-down && make docker-up`.

- [ ] **Step 5: Commit**

```bash
git add internal/database/migrations/000006_create_tables_slice.sql internal/tables/schema_integration_test.go
git commit -m "feat(tables): add Phase 3 schema migration

Creates tables, plus service_sessions and table_assignments provisioned
early for the overview read per ADR-006."
```

---

### Task 2: SQL queries and sqlc generation

**Files:**
- Create: `sql/queries/tables.sql`
- Modify: generated `internal/database/sqlc/*.go` (via `sqlc generate`, do not hand-edit)

**Interfaces:**
- Consumes: Task 1 schema.
- Produces: generated methods on `*sqlc.Queries` used by every later task:
  - `GetTablesSessionAuthority(ctx, GetTablesSessionAuthorityParams{ID, StaffIdentityID}) (GetTablesSessionAuthorityRow, error)` — row fields `SessionID, StaffIdentityID, State, ActiveWorkspace, LastHumanActivityAt, ExpiresAt, RevokedAt, IdentityEnabled, PinHash`
  - `GetTablesSessionRoles(ctx, uuid.UUID) ([]string, error)`
  - `TablesAdvisoryLock(ctx, int64) error`
  - `GetIdempotencyRecord(ctx, GetIdempotencyRecordParams{ActorID, Key}) (GetIdempotencyRecordRow, error)` — row fields `Key, ActorID, Action, RequestHash, ResponseCode, ResponseBody, CreatedAt`
  - `ClaimIdempotencyRecord(ctx, ClaimIdempotencyRecordParams{Key, ActorID, Action, RequestHash, ResponseCode, ResponseBody}) (ClaimIdempotencyRecordRow, error)` — same row fields
  - `StoreIdempotencyResult(ctx, StoreIdempotencyResultParams{ActorID, Key, ResponseCode, ResponseBody}) error`
  - `CreateTable(ctx, CreateTableParams{Name, NormalizedName}) (Table, error)`
  - `GetTableForUpdate(ctx, uuid.UUID) (Table, error)`
  - `RenameTable(ctx, RenameTableParams{ID, Name, NormalizedName}) (Table, error)`
  - `SetTableAvailability(ctx, SetTableAvailabilityParams{ID, Available}) (Table, error)`
  - `ListTables(ctx) ([]Table, error)`
  - `ListCurrentTableOccupants(ctx) ([]ListCurrentTableOccupantsRow, error)` — row fields `TableID, ServiceSessionID, ServiceNumber`

  The `Table` model struct has fields `ID, Name, NormalizedName, Available, CreatedAt, UpdatedAt`.

- [ ] **Step 1: Write the queries file**

Create `sql/queries/tables.sql`:

```sql
-- -- Authority --

-- name: GetTablesSessionAuthority :one
SELECT s.id AS session_id, s.staff_identity_id, s.state, s.active_workspace,
       s.last_human_activity_at, s.expires_at, s.revoked_at,
       i.enabled AS identity_enabled, i.pin_hash
FROM staff_access_sessions s
JOIN staff_identities i ON i.id = s.staff_identity_id
WHERE s.id = $1 AND s.staff_identity_id = $2;

-- name: GetTablesSessionRoles :many
SELECT role
FROM staff_operational_roles
WHERE staff_identity_id = $1
ORDER BY role ASC;

-- name: TablesAdvisoryLock :exec
SELECT pg_advisory_xact_lock($1);

-- -- Shared idempotency (ADR-005) --

-- name: GetIdempotencyRecord :one
SELECT key, actor_id, action, request_hash, response_code, response_body, created_at
FROM idempotency_keys
WHERE actor_id = $1 AND key = $2
LIMIT 1;

-- name: ClaimIdempotencyRecord :one
INSERT INTO idempotency_keys (key, actor_id, action, request_hash, response_code, response_body)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (actor_id, key) DO UPDATE
    SET response_code = idempotency_keys.response_code,
        response_body = idempotency_keys.response_body
RETURNING key, actor_id, action, request_hash, response_code, response_body, created_at;

-- name: StoreIdempotencyResult :exec
UPDATE idempotency_keys
SET response_code = $3, response_body = $4
WHERE actor_id = $1 AND key = $2;

-- -- Tables --

-- name: CreateTable :one
INSERT INTO tables (name, normalized_name)
VALUES ($1, $2)
RETURNING id, name, normalized_name, available, created_at, updated_at;

-- name: GetTableForUpdate :one
SELECT id, name, normalized_name, available, created_at, updated_at
FROM tables
WHERE id = $1
FOR UPDATE;

-- name: RenameTable :one
UPDATE tables
SET name = $2, normalized_name = $3, updated_at = now()
WHERE id = $1
RETURNING id, name, normalized_name, available, created_at, updated_at;

-- name: SetTableAvailability :one
UPDATE tables
SET available = $2, updated_at = now()
WHERE id = $1
RETURNING id, name, normalized_name, available, created_at, updated_at;

-- name: ListTables :many
SELECT id, name, normalized_name, available, created_at, updated_at
FROM tables
ORDER BY created_at ASC, id ASC;

-- -- Occupancy (read-only view of Sales-owned tables) --

-- name: ListCurrentTableOccupants :many
SELECT ta.table_id, ss.id AS service_session_id, ss.service_number
FROM table_assignments ta
JOIN service_sessions ss ON ss.id = ta.service_session_id
WHERE ta.released_at IS NULL AND ss.state = 'ACTIVE'
ORDER BY ta.assigned_at ASC, ta.id ASC;
```

- [ ] **Step 2: Regenerate sqlc bindings**

Run: `make sqlc`

Expected: `internal/database/sqlc/tables.sql.go` is created, and `models.go` and `querier.go` gain the `Table`, `ServiceSession`, and `TableAssignment` structs plus the new `Querier` methods.

- [ ] **Step 3: Verify it compiles**

Run: `go build ./...`

Expected: no output, exit 0.

- [ ] **Step 4: Verify the generated names match the Interfaces block**

Run: `grep -n "func (q \*Queries) \(GetTablesSessionAuthority\|ListCurrentTableOccupants\|ClaimIdempotencyRecord\|GetTableForUpdate\)" internal/database/sqlc/tables.sql.go`

Expected: all four methods listed. If sqlc named a parameter struct field differently than this plan's Interfaces block states, treat the generated name as authoritative and use it consistently in every later task.

- [ ] **Step 5: Commit**

```bash
git add sql/queries/tables.sql internal/database/sqlc/
git commit -m "feat(tables): add SQL queries and regenerate sqlc bindings"
```

---

### Task 3: Domain normalization and validation

**Files:**
- Create: `internal/tables/domain.go`
- Test: `internal/tables/domain_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces:
  - `func NormalizeTableName(s string) (display, key string)`
  - `func ValidateTableName(display string) error`
  - Capability constants `CapSalesOperate`, `CapTablesAdminister`
  - Operation constants `OpCreateTable`, `OpRenameTable`, `OpSetTableAvailability`
  - Audit event constants `EventTableCreated`, `EventTableRenamed`, `EventTableAvailabilityChanged`, `EventAuthorizationDenied`
  - `MaxTableNameLength = 60`

- [ ] **Step 1: Write the failing test**

Create `internal/tables/domain_test.go`:

```go
package tables_test

import (
	"strings"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/tables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeTableName(t *testing.T) {
	cases := []struct {
		name        string
		in          string
		wantDisplay string
		wantKey     string
	}{
		{"trims surrounding whitespace", "  Ban 1  ", "Ban 1", "ban 1"},
		{"collapses internal whitespace", "Ban    1", "Ban 1", "ban 1"},
		{"collapses tabs and newlines", "Ban\t\n 1", "Ban 1", "ban 1"},
		{"lowercases Vietnamese diacritics", "BÀN GHÉP", "BÀN GHÉP", "bàn ghép"},
		{"preserves single internal spaces", "Ban ghep so 1", "Ban ghep so 1", "ban ghep so 1"},
		{"empty stays empty", "   ", "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			display, key := tables.NormalizeTableName(tc.in)
			assert.Equal(t, tc.wantDisplay, display)
			assert.Equal(t, tc.wantKey, key)
		})
	}
}

func TestNormalizeTableNameKeyMatchesPostgresLower(t *testing.T) {
	// The database CHECK is normalized_name = lower(name), so the key must be
	// exactly the lowercase of the display form, not of the raw input.
	display, key := tables.NormalizeTableName("  Bàn  Ghép  ")
	assert.Equal(t, "Bàn Ghép", display)
	assert.Equal(t, strings.ToLower(display), key)
}

func TestValidateTableName(t *testing.T) {
	t.Run("accepts a one-rune name", func(t *testing.T) {
		require.NoError(t, tables.ValidateTableName("A"))
	})

	t.Run("accepts exactly 60 runes", func(t *testing.T) {
		require.NoError(t, tables.ValidateTableName(strings.Repeat("à", 60)))
	})

	t.Run("rejects empty", func(t *testing.T) {
		require.Error(t, tables.ValidateTableName(""))
	})

	t.Run("rejects 61 runes", func(t *testing.T) {
		require.Error(t, tables.ValidateTableName(strings.Repeat("a", 61)))
	})

	t.Run("counts runes not bytes", func(t *testing.T) {
		// 60 Vietnamese runes are 120 bytes; a byte-based check would reject this.
		name := strings.Repeat("à", 60)
		require.Greater(t, len(name), 60)
		require.NoError(t, tables.ValidateTableName(name))
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tables/... -run "TestNormalizeTableName|TestValidateTableName" -v`

Expected: FAIL — package `internal/tables` does not exist.

- [ ] **Step 3: Write the implementation**

Create `internal/tables/domain.go`:

```go
// Package tables implements the Tables vertical slice: the Table entity,
// its naming and Availability rules, and a read reporting current occupancy.
package tables

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Capabilities required for tables operations.
const (
	CapSalesOperate     = "sales.operate"
	CapTablesAdminister = "tables.administer"
)

// Idempotency action names. Stored in idempotency_keys.action (VARCHAR(50)).
const (
	OpCreateTable          = "tables.create_table"
	OpRenameTable          = "tables.rename_table"
	OpSetTableAvailability = "tables.set_table_availability"
)

// Audit event types.
const (
	EventTableCreated             = "TABLE_CREATED"
	EventTableRenamed             = "TABLE_RENAMED"
	EventTableAvailabilityChanged = "TABLE_AVAILABILITY_CHANGED"
	EventAuthorizationDenied      = "tables.authorization_denied"
)

// MaxTableNameLength is the inclusive upper bound on a normalized Table name,
// counted in Unicode code points to agree with the database char_length check.
const MaxTableNameLength = 60

// NormalizeTableName trims surrounding whitespace, collapses each run of
// internal whitespace to a single space, and returns the display form plus a
// Unicode-lowercase uniqueness key.
//
// This differs from catalog.NormalizeName, which preserves internal
// whitespace. Table names collapse it because "Ban  1" and "Ban 1" name the
// same physical location.
func NormalizeTableName(s string) (display, key string) {
	display = strings.Join(strings.Fields(s), " ")
	key = strings.ToLower(display)
	return display, key
}

// ValidateTableName checks a normalized display name against the canonical
// length bounds. Length is measured in Unicode code points, never bytes.
func ValidateTableName(display string) error {
	n := utf8.RuneCountInString(display)
	if n < 1 {
		return fmt.Errorf("table name cannot be empty")
	}
	if n > MaxTableNameLength {
		return fmt.Errorf("table name is %d characters, maximum is %d", n, MaxTableNameLength)
	}
	return nil
}
```

Note: `strings.Fields` splits on any run of Unicode whitespace and drops empty fields, so joining with a single space performs both the trim and the collapse in one pass.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race ./internal/tables/... -run "TestNormalizeTableName|TestValidateTableName" -v`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tables/domain.go internal/tables/domain_test.go
git commit -m "feat(tables): add name normalization and validation

Table names collapse internal whitespace, unlike catalog names, and length
is measured in runes to agree with the database char_length check."
```

---

### Task 4: Domain errors and HTTP mapping

**Files:**
- Create: `internal/tables/errors.go`
- Test: `internal/tables/errors_test.go`

**Interfaces:**
- Consumes: Task 3 package.
- Produces:
  - Sentinels `ErrTableNotFound`, `ErrNameConflict`, `ErrRequestConflict`, `ErrForbidden`, `ErrUnauthorized`, `ErrInvalidStoredResult`
  - `func MapDBError(err error) error`
  - `func MapHTTPError(err error) error`

- [ ] **Step 1: Write the failing test**

Create `internal/tables/errors_test.go`:

```go
package tables_test

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/Mirai3103/pos-cafe/internal/tables"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMapDBError(t *testing.T) {
	t.Run("nil stays nil", func(t *testing.T) {
		require.NoError(t, tables.MapDBError(nil))
	})

	t.Run("no rows becomes not found", func(t *testing.T) {
		err := tables.MapDBError(sql.ErrNoRows)
		require.ErrorIs(t, err, tables.ErrTableNotFound)
	})

	t.Run("unique violation becomes name conflict", func(t *testing.T) {
		err := tables.MapDBError(&pgconn.PgError{Code: "23505", Message: "duplicate key"})
		require.ErrorIs(t, err, tables.ErrNameConflict)
	})

	t.Run("foreign key violation becomes not found", func(t *testing.T) {
		err := tables.MapDBError(&pgconn.PgError{Code: "23503", Message: "fk violation"})
		require.ErrorIs(t, err, tables.ErrTableNotFound)
	})

	t.Run("unknown error passes through", func(t *testing.T) {
		sentinel := errors.New("boom")
		require.ErrorIs(t, tables.MapDBError(sentinel), sentinel)
	})
}

func TestMapHTTPError(t *testing.T) {
	cases := []struct {
		name     string
		in       error
		wantCode string
		wantHTTP int
	}{
		{"not found", tables.ErrTableNotFound, "TABLE_NOT_FOUND", http.StatusNotFound},
		{"name conflict", tables.ErrNameConflict, "TABLE_NAME_CONFLICT", http.StatusConflict},
		{"request conflict", tables.ErrRequestConflict, "REQUEST_CONFLICT", http.StatusConflict},
		{"forbidden", tables.ErrForbidden, "FORBIDDEN", http.StatusForbidden},
		{"unauthorized", tables.ErrUnauthorized, "UNAUTHORIZED", http.StatusUnauthorized},
		{"invalid stored result", tables.ErrInvalidStoredResult, "INVALID_STORED_RESULT", http.StatusInternalServerError},
		{"invalid input", response.ErrInvalid, "INVALID_INPUT", http.StatusBadRequest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mapped := tables.MapHTTPError(fmt.Errorf("%w: context", tc.in))
			var coded *response.CodedError
			require.ErrorAs(t, mapped, &coded)
			assert.Equal(t, tc.wantCode, coded.Code)
			assert.Equal(t, tc.wantHTTP, coded.Status)
		})
	}

	t.Run("nil stays nil", func(t *testing.T) {
		require.NoError(t, tables.MapHTTPError(nil))
	})

	t.Run("stored result error hides detail from the client", func(t *testing.T) {
		mapped := tables.MapHTTPError(fmt.Errorf("%w: raw body 0xdeadbeef", tables.ErrInvalidStoredResult))
		var coded *response.CodedError
		require.ErrorAs(t, mapped, &coded)
		assert.NotContains(t, coded.Message, "0xdeadbeef")
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tables/... -run "TestMapDBError|TestMapHTTPError" -v`

Expected: FAIL — `tables.ErrTableNotFound` undefined.

If `response.CodedError` exposes its fields under different names than `Code`, `Status`, and `Message`, read `internal/response` and adjust the assertions; the sentinel-to-code mapping is what matters.

- [ ] **Step 3: Write the implementation**

Create `internal/tables/errors.go`:

```go
package tables

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrTableNotFound       = errors.New("table not found")
	ErrNameConflict        = errors.New("table name conflict")
	ErrRequestConflict     = errors.New("request conflict")
	ErrForbidden           = errors.New("forbidden")
	ErrUnauthorized        = errors.New("unauthorized")
	ErrInvalidStoredResult = errors.New("invalid stored result")
)

// MapDBError maps PostgreSQL driver and database errors to domain sentinels.
func MapDBError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %s", ErrTableNotFound, err.Error())
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		msg := pgErr.Detail
		if msg == "" {
			msg = pgErr.Message
		}
		switch pgErr.Code {
		case "23505": // unique_violation
			return fmt.Errorf("%w: %s", ErrNameConflict, msg)
		case "23503": // foreign_key_violation
			return fmt.Errorf("%w: %s", ErrTableNotFound, msg)
		}
	}
	return err
}

// MapHTTPError maps tables domain errors and input validation errors to
// *response.CodedError.
func MapHTTPError(err error) error {
	if err == nil {
		return nil
	}
	var codedErr *response.CodedError
	if errors.As(err, &codedErr) {
		return err
	}
	switch {
	case errors.Is(err, ErrTableNotFound):
		return response.NewCodedError(http.StatusNotFound, "TABLE_NOT_FOUND", err.Error(), err)
	case errors.Is(err, ErrNameConflict):
		return response.NewCodedError(http.StatusConflict, "TABLE_NAME_CONFLICT", err.Error(), err)
	case errors.Is(err, ErrRequestConflict):
		return response.NewCodedError(http.StatusConflict, "REQUEST_CONFLICT", err.Error(), err)
	case errors.Is(err, ErrForbidden):
		return response.NewCodedError(http.StatusForbidden, "FORBIDDEN", err.Error(), err)
	case errors.Is(err, ErrUnauthorized):
		return response.NewCodedError(http.StatusUnauthorized, "UNAUTHORIZED", err.Error(), err)
	case errors.Is(err, ErrInvalidStoredResult):
		return response.NewCodedError(http.StatusInternalServerError, "INVALID_STORED_RESULT",
			"an unexpected error occurred", err)
	case errors.Is(err, response.ErrInvalid):
		return response.NewCodedError(http.StatusBadRequest, "INVALID_INPUT", err.Error(), err)
	default:
		return err
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race ./internal/tables/... -run "TestMapDBError|TestMapHTTPError" -v`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tables/errors.go internal/tables/errors_test.go
git commit -m "feat(tables): add domain errors and HTTP mapping"
```

---

### Task 5: Transactional executor

**Files:**
- Create: `internal/tables/executor.go`
- Test: `internal/tables/executor_integration_test.go`

**Interfaces:**
- Consumes: Task 2 generated queries, Task 3 constants, Task 4 sentinels.
- Produces:
  - `type Actor struct { StaffID, SessionID uuid.UUID }`
  - `type MutationSpec struct { RequestID uuid.UUID; Operation string; Fingerprint any; Required []string }`
  - `type AuditRecord struct { EventType string; Details any }`
  - `type Runner struct` with `func NewRunner(db *sql.DB, queries *sqlc.Queries) *Runner`
  - `func ExecuteMutation[T any](ctx context.Context, r *Runner, actor Actor, spec MutationSpec, fn func(*sqlc.Queries) (int, T, AuditRecord, error)) (int, T, error)`
  - `func ExecuteRead[T any](ctx context.Context, r *Runner, actor Actor, requiredCapability string, fn func(*sqlc.Queries) (T, error)) (T, error)`

  `MutationSpec` has no `ManagerPIN` or `RequireManagerPIN` field; no Tables command is price-sensitive.

- [ ] **Step 1: Write the failing test**

Create `internal/tables/executor_integration_test.go`:

```go
//go:build integration

package tables_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/database"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/tables"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openTablesTestDB(t *testing.T) (*sql.DB, *sqlc.Queries) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	require.Contains(t, url, "_test")
	db, err := database.Open(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db, sqlc.New(db)
}

type testActor struct {
	StaffID   uuid.UUID
	SessionID uuid.UUID
}

func (a testActor) actor() tables.Actor {
	return tables.Actor{StaffID: a.StaffID, SessionID: a.SessionID}
}

var loginCounter atomic.Int64

func newTestActor(t *testing.T, q *sqlc.Queries, roles []string, enabled bool) testActor {
	t.Helper()
	ctx := context.Background()

	loginCode := fmt.Sprintf("T%06d", loginCounter.Add(1))
	row, err := q.CreateStaffIdentity(ctx, sqlc.CreateStaffIdentityParams{
		DisplayName: "Tables Test " + loginCode,
		Btrim:       loginCode,
		PinHash:     "",
		Enabled:     enabled,
	})
	require.NoError(t, err)

	for _, role := range roles {
		require.NoError(t, q.AddStaffRole(ctx, sqlc.AddStaffRoleParams{
			StaffIdentityID: row.ID,
			Role:            role,
		}))
	}

	session, err := q.CreateStaffSession(ctx, sqlc.CreateStaffSessionParams{
		TokenHash:           "tok_" + uuid.NewString()[:16],
		StaffIdentityID:     row.ID,
		State:               auth.SessionStateActive,
		LastHumanActivityAt: time.Now(),
		ExpiresAt:           time.Now().Add(8 * time.Hour),
	})
	require.NoError(t, err)

	return testActor{StaffID: row.ID, SessionID: session.ID}
}

type probeFingerprint struct {
	Name string `json:"name"`
}

type probeResult struct {
	Value string `json:"value"`
}

func TestExecuteMutationRequiresCapability(t *testing.T) {
	db, q := openTablesTestDB(t)
	runner := tables.NewRunner(db, q)
	ctx := context.Background()

	// BARISTA holds neither sales.operate nor tables.administer.
	barista := newTestActor(t, q, []string{"BARISTA"}, true)

	_, _, err := tables.ExecuteMutation(ctx, runner, barista.actor(), tables.MutationSpec{
		RequestID:   uuid.New(),
		Operation:   tables.OpCreateTable,
		Fingerprint: probeFingerprint{Name: "probe"},
		Required:    []string{tables.CapTablesAdminister},
	}, func(*sqlc.Queries) (int, probeResult, tables.AuditRecord, error) {
		t.Fatal("mutation body must not run for an unauthorized actor")
		return 0, probeResult{}, tables.AuditRecord{}, nil
	})
	require.ErrorIs(t, err, tables.ErrForbidden)
}

func TestExecuteMutationReplaysExactRequest(t *testing.T) {
	db, q := openTablesTestDB(t)
	runner := tables.NewRunner(db, q)
	ctx := context.Background()

	manager := newTestActor(t, q, []string{"MANAGER"}, true)
	requestID := uuid.New()
	spec := tables.MutationSpec{
		RequestID:   requestID,
		Operation:   tables.OpCreateTable,
		Fingerprint: probeFingerprint{Name: "replay probe"},
		Required:    []string{tables.CapTablesAdminister},
	}

	var runs atomic.Int32
	body := func(*sqlc.Queries) (int, probeResult, tables.AuditRecord, error) {
		runs.Add(1)
		return 201, probeResult{Value: "first"}, tables.AuditRecord{}, nil
	}

	code, first, err := tables.ExecuteMutation(ctx, runner, manager.actor(), spec, body)
	require.NoError(t, err)
	assert.Equal(t, 201, code)
	assert.Equal(t, "first", first.Value)

	code, second, err := tables.ExecuteMutation(ctx, runner, manager.actor(), spec, body)
	require.NoError(t, err)
	assert.Equal(t, 201, code)
	assert.Equal(t, "first", second.Value, "replay must return the stored result")
	assert.Equal(t, int32(1), runs.Load(), "mutation body must run exactly once")
}

func TestExecuteMutationRejectsConflictingReuse(t *testing.T) {
	db, q := openTablesTestDB(t)
	runner := tables.NewRunner(db, q)
	ctx := context.Background()

	manager := newTestActor(t, q, []string{"MANAGER"}, true)
	requestID := uuid.New()

	body := func(*sqlc.Queries) (int, probeResult, tables.AuditRecord, error) {
		return 201, probeResult{Value: "ok"}, tables.AuditRecord{}, nil
	}

	_, _, err := tables.ExecuteMutation(ctx, runner, manager.actor(), tables.MutationSpec{
		RequestID:   requestID,
		Operation:   tables.OpCreateTable,
		Fingerprint: probeFingerprint{Name: "original"},
		Required:    []string{tables.CapTablesAdminister},
	}, body)
	require.NoError(t, err)

	_, _, err = tables.ExecuteMutation(ctx, runner, manager.actor(), tables.MutationSpec{
		RequestID:   requestID,
		Operation:   tables.OpCreateTable,
		Fingerprint: probeFingerprint{Name: "different"},
		Required:    []string{tables.CapTablesAdminister},
	}, body)
	require.ErrorIs(t, err, tables.ErrRequestConflict)
}

func TestExecuteMutationRunsOnceUnderConcurrency(t *testing.T) {
	db, q := openTablesTestDB(t)
	runner := tables.NewRunner(db, q)
	ctx := context.Background()

	manager := newTestActor(t, q, []string{"MANAGER"}, true)
	spec := tables.MutationSpec{
		RequestID:   uuid.New(),
		Operation:   tables.OpCreateTable,
		Fingerprint: probeFingerprint{Name: "concurrent"},
		Required:    []string{tables.CapTablesAdminister},
	}

	var runs atomic.Int32
	var start sync.WaitGroup
	var done sync.WaitGroup
	start.Add(1)

	const goroutines = 4
	errs := make([]error, goroutines)
	for i := range goroutines {
		done.Add(1)
		go func() {
			defer done.Done()
			start.Wait()
			_, _, errs[i] = tables.ExecuteMutation(ctx, runner, manager.actor(), spec,
				func(*sqlc.Queries) (int, probeResult, tables.AuditRecord, error) {
					runs.Add(1)
					return 201, probeResult{Value: "once"}, tables.AuditRecord{}, nil
				})
		}()
	}
	start.Done()
	done.Wait()

	for _, err := range errs {
		require.NoError(t, err)
	}
	assert.Equal(t, int32(1), runs.Load(), "concurrent duplicates must execute the body once")
}

func TestExecuteMutationDoesNotCacheFailures(t *testing.T) {
	db, q := openTablesTestDB(t)
	runner := tables.NewRunner(db, q)
	ctx := context.Background()

	manager := newTestActor(t, q, []string{"MANAGER"}, true)
	spec := tables.MutationSpec{
		RequestID:   uuid.New(),
		Operation:   tables.OpCreateTable,
		Fingerprint: probeFingerprint{Name: "fails then succeeds"},
		Required:    []string{tables.CapTablesAdminister},
	}

	boom := fmt.Errorf("%w: deliberate", tables.ErrTableNotFound)
	_, _, err := tables.ExecuteMutation(ctx, runner, manager.actor(), spec,
		func(*sqlc.Queries) (int, probeResult, tables.AuditRecord, error) {
			return 0, probeResult{}, tables.AuditRecord{}, boom
		})
	require.ErrorIs(t, err, tables.ErrTableNotFound)

	// The failed claim rolled back, so the same request_id is reusable.
	code, res, err := tables.ExecuteMutation(ctx, runner, manager.actor(), spec,
		func(*sqlc.Queries) (int, probeResult, tables.AuditRecord, error) {
			return 201, probeResult{Value: "retried"}, tables.AuditRecord{}, nil
		})
	require.NoError(t, err)
	assert.Equal(t, 201, code)
	assert.Equal(t, "retried", res.Value)
}

func TestExecuteReadRequiresCapability(t *testing.T) {
	db, q := openTablesTestDB(t)
	runner := tables.NewRunner(db, q)
	ctx := context.Background()

	barista := newTestActor(t, q, []string{"BARISTA"}, true)
	_, err := tables.ExecuteRead(ctx, runner, barista.actor(), tables.CapSalesOperate,
		func(*sqlc.Queries) (int, error) {
			t.Fatal("read body must not run without sales.operate")
			return 0, nil
		})
	require.ErrorIs(t, err, tables.ErrForbidden)

	cashier := newTestActor(t, q, []string{"CASHIER"}, true)
	got, err := tables.ExecuteRead(ctx, runner, cashier.actor(), tables.CapSalesOperate,
		func(*sqlc.Queries) (int, error) { return 7, nil })
	require.NoError(t, err)
	assert.Equal(t, 7, got)
}

func TestExecuteMutationRollsBackOnAuditFailure(t *testing.T) {
	db, q := openTablesTestDB(t)
	runner := tables.NewRunner(db, q)
	ctx := context.Background()

	manager := newTestActor(t, q, []string{"MANAGER"}, true)
	requestID := uuid.New()
	spec := tables.MutationSpec{
		RequestID:   requestID,
		Operation:   tables.OpCreateTable,
		Fingerprint: probeFingerprint{Name: "audit failure"},
		Required:    []string{tables.CapTablesAdminister},
	}

	// A channel cannot be JSON-encoded, so writing the audit event fails after
	// the mutation body has already run.
	_, _, err := tables.ExecuteMutation(ctx, runner, manager.actor(), spec,
		func(*sqlc.Queries) (int, probeResult, tables.AuditRecord, error) {
			return 201, probeResult{Value: "doomed"}, tables.AuditRecord{
				EventType: tables.EventTableCreated,
				Details:   make(chan int),
			}, nil
		})
	require.Error(t, err, "an unwritable audit event must fail the operation")

	// The whole transaction rolled back, so the idempotency claim is gone too.
	var claims int
	require.NoError(t, db.QueryRow(
		`SELECT count(*) FROM idempotency_keys WHERE actor_id = $1 AND key = $2`,
		manager.StaffID, requestID).Scan(&claims))
	assert.Equal(t, 0, claims, "a failed audit write must roll back the idempotency claim")
}

func TestExecuteReadUsesReadOnlyTransaction(t *testing.T) {
	db, q := openTablesTestDB(t)
	runner := tables.NewRunner(db, q)
	ctx := context.Background()

	manager := newTestActor(t, q, []string{"MANAGER"}, true)

	// A write attempted inside the read transaction must be refused, proving
	// the read runs read-only rather than in a default read-write transaction.
	_, err := tables.ExecuteRead(ctx, runner, manager.actor(), tables.CapSalesOperate,
		func(q *sqlc.Queries) (int, error) {
			_, err := q.CreateTable(ctx, sqlc.CreateTableParams{
				Name:           "Ban readonly probe",
				NormalizedName: "ban readonly probe",
			})
			return 0, err
		})
	require.Error(t, err, "a write inside ExecuteRead must be refused")
}

func TestExecuteMutationDeniesDisabledIdentity(t *testing.T) {
	db, q := openTablesTestDB(t)
	runner := tables.NewRunner(db, q)
	ctx := context.Background()

	manager := newTestActor(t, q, []string{"MANAGER"}, true)
	require.NoError(t, q.SetStaffEnabled(ctx, sqlc.SetStaffEnabledParams{
		ID:      manager.StaffID,
		Enabled: false,
	}))

	_, _, err := tables.ExecuteMutation(ctx, runner, manager.actor(), tables.MutationSpec{
		RequestID:   uuid.New(),
		Operation:   tables.OpCreateTable,
		Fingerprint: probeFingerprint{Name: "disabled"},
		Required:    []string{tables.CapTablesAdminister},
	}, func(*sqlc.Queries) (int, probeResult, tables.AuditRecord, error) {
		t.Fatal("mutation body must not run for a disabled identity")
		return 0, probeResult{}, tables.AuditRecord{}, nil
	})
	require.ErrorIs(t, err, tables.ErrForbidden)
}
```

If `CreateStaffIdentity`, `CreateStaffSession`, or `SetStaffEnabled` have different generated parameter field names, read `internal/catalog/executor_integration_test.go` — it builds the same fixtures — and copy its exact calls.

- [ ] **Step 2: Run test to verify it fails**

Run: `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -p 1 -tags=integration ./internal/tables/... -run TestExecute -v`

Expected: FAIL — `tables.NewRunner` undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/tables/executor.go`:

```go
package tables

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
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
// Tables has no price-sensitive command, so there is no Manager PIN field.
type MutationSpec struct {
	RequestID   uuid.UUID
	Operation   string
	Fingerprint any
	Required    []string
}

// AuditRecord describes the audit event to insert after a successful mutation.
// A zero EventType means no business event is written, which is how a
// same-state Availability no-op reports itself.
type AuditRecord struct {
	EventType string
	Details   any
}

// committedDenial signals that a denial audit event was written and must be
// committed even though the operation itself failed.
type committedDenial struct {
	err error
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

// idToLockKey converts a UUID to a stable int64 for advisory locking.
func idToLockKey(id uuid.UUID) int64 {
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

// reloadAuthority loads the current session, roles, and capabilities inside
// the transaction, so authority removed mid-session takes effect immediately.
func reloadAuthority(ctx context.Context, q *sqlc.Queries, actor Actor) (
	sqlc.GetTablesSessionAuthorityRow, []string, error,
) {
	authRow, err := q.GetTablesSessionAuthority(ctx, sqlc.GetTablesSessionAuthorityParams{
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

	roles, err := q.GetTablesSessionRoles(ctx, authRow.StaffIdentityID)
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

// recordDenial writes a denial audit event and returns an outcome that must be
// committed so the security evidence survives the failed operation.
func recordDenial(ctx context.Context, q *sqlc.Queries, actor Actor,
	authority sqlc.GetTablesSessionAuthorityRow, operation string, denialErr error,
) error {
	details, _ := json.Marshal(map[string]string{
		"operation": operation,
		"reason":    denialErr.Error(),
	})
	actorID := uuid.NullUUID{}
	sessionID := uuid.NullUUID{}
	if authority.StaffIdentityID == actor.StaffID {
		actorID = uuid.NullUUID{UUID: actor.StaffID, Valid: true}
	}
	if authority.SessionID == actor.SessionID {
		sessionID = uuid.NullUUID{UUID: actor.SessionID, Valid: true}
	}
	if _, err := q.InsertAuditEvent(ctx, sqlc.InsertAuditEventParams{
		EventType:  EventAuthorizationDenied,
		ActorID:    actorID,
		SessionID:  sessionID,
		Details:    details,
		OccurredAt: time.Now(),
	}); err != nil {
		return fmt.Errorf("insert denial audit event: %w", err)
	}
	return &committedDenial{err: denialErr}
}

func finishDenial(tx *sql.Tx, outcome error) error {
	var denial *committedDenial
	if !errors.As(outcome, &denial) {
		return outcome
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit authorization denial: %w", err)
	}
	return denial.err
}

func isSecurityDenial(err error) bool {
	return errors.Is(err, ErrUnauthorized) || errors.Is(err, ErrForbidden)
}

// ExecuteMutation runs a mutation inside one transaction with authorization,
// idempotency, and audit. On success it returns the HTTP status and result.
func ExecuteMutation[T any](ctx context.Context, r *Runner, actor Actor,
	spec MutationSpec,
	fn func(*sqlc.Queries) (int, T, AuditRecord, error),
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
	lockKey := idToLockKey(actor.StaffID) ^ idToLockKey(spec.RequestID)
	if err := q.TablesAdvisoryLock(ctx, lockKey); err != nil {
		return 0, zero, fmt.Errorf("advisory lock: %w", err)
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
	resultCode, result, audit, err := fn(q)
	if err != nil {
		return 0, zero, err
	}

	// 8. Write the business audit event, when there is one. A same-state
	// Availability no-op returns a zero EventType and writes none.
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

// ExecuteRead runs a read inside a read-only repeatable-read transaction so
// the capability check and every query observe one snapshot.
func ExecuteRead[T any](ctx context.Context, r *Runner, actor Actor,
	requiredCapability string,
	fn func(*sqlc.Queries) (T, error),
) (T, error) {
	var zero T

	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return zero, fmt.Errorf("begin read transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	q := r.queries.WithTx(tx)

	if _, caps, err := reloadAuthority(ctx, q, actor); err != nil {
		return zero, err
	} else if err := verifyCapabilities([]string{requiredCapability}, caps); err != nil {
		return zero, err
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
```

The `if _, caps, err := ...; err != nil { } else if ... { }` form keeps `caps` scoped to the check. If the linter objects to that shape, split it into two statements with `caps` declared normally.

- [ ] **Step 4: Run tests to verify they pass**

Run: `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -race -p 1 -tags=integration ./internal/tables/... -run TestExecute -v`

Expected: PASS — all nine executor tests.

- [ ] **Step 5: Commit**

```bash
git add internal/tables/executor.go internal/tables/executor_integration_test.go
git commit -m "feat(tables): add transactional executor

Reloads authority inside the transaction before idempotent replay, claims
actor-scoped keys in the shared idempotency_keys table per ADR-005, and
commits denial audit evidence."
```

---

### Task 6: DTOs and the three commands

**Files:**
- Create: `internal/tables/dto.go`
- Create: `internal/tables/commands.go`
- Test: `internal/tables/commands_integration_test.go`

**Interfaces:**
- Consumes: Tasks 2–5.
- Produces:
  - `type CreateTableCommand struct { RequestID uuid.UUID \`json:"request_id"\`; Name string \`json:"name"\` }`
  - `type RenameTableCommand struct { RequestID uuid.UUID; TableID uuid.UUID; Name string }`
  - `type SetTableAvailabilityCommand struct { RequestID uuid.UUID; TableID uuid.UUID; Available bool }`
  - `type TableResponse struct { ID uuid.UUID \`json:"id"\`; Name string \`json:"name"\`; Available bool \`json:"available"\` }`
  - `CreateTableHandler`, `RenameTableHandler`, `SetTableAvailabilityHandler`, each with `NewXHandler(runner *Runner) *XHandler` and `Handle(ctx, actor Actor, cmd XCommand) (int, TableResponse, error)`

- [ ] **Step 1: Write the failing test**

Create `internal/tables/commands_integration_test.go`:

```go
//go:build integration

package tables_test

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/tables"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func uniqueTableName(prefix string) string {
	return prefix + " " + uuid.NewString()[:8]
}

func countAuditEvents(t *testing.T, db *sql.DB, eventType string, tableID uuid.UUID) int {
	t.Helper()
	var n int
	err := db.QueryRow(
		`SELECT count(*) FROM audit_events
		 WHERE event_type = $1 AND details->>'table_id' = $2`,
		eventType, tableID.String()).Scan(&n)
	require.NoError(t, err)
	return n
}

func TestCreateTable(t *testing.T) {
	db, q := openTablesTestDB(t)
	runner := tables.NewRunner(db, q)
	handler := tables.NewCreateTableHandler(runner)
	ctx := context.Background()
	manager := newTestActor(t, q, []string{"MANAGER"}, true)

	t.Run("creates a Table and audits it", func(t *testing.T) {
		name := uniqueTableName("Ban")
		code, res, err := handler.Handle(ctx, manager.actor(), tables.CreateTableCommand{
			RequestID: uuid.New(),
			Name:      "  " + name + "  ",
		})
		require.NoError(t, err)
		assert.Equal(t, 201, code)
		assert.Equal(t, name, res.Name, "name must be trimmed")
		assert.True(t, res.Available, "a new Table is available by default")
		assert.NotEqual(t, uuid.Nil, res.ID)
		assert.Equal(t, 1, countAuditEvents(t, db, tables.EventTableCreated, res.ID))
	})

	t.Run("collapses internal whitespace", func(t *testing.T) {
		suffix := uuid.NewString()[:8]
		_, res, err := handler.Handle(ctx, manager.actor(), tables.CreateTableCommand{
			RequestID: uuid.New(),
			Name:      "Ban    ghep " + suffix,
		})
		require.NoError(t, err)
		assert.Equal(t, "Ban ghep "+suffix, res.Name)
	})

	t.Run("rejects an empty name", func(t *testing.T) {
		_, _, err := handler.Handle(ctx, manager.actor(), tables.CreateTableCommand{
			RequestID: uuid.New(),
			Name:      "   ",
		})
		require.Error(t, err)
	})

	t.Run("rejects a name over 60 runes", func(t *testing.T) {
		_, _, err := handler.Handle(ctx, manager.actor(), tables.CreateTableCommand{
			RequestID: uuid.New(),
			Name:      strings.Repeat("a", 61),
		})
		require.Error(t, err)
	})

	t.Run("rejects a duplicate normalized name", func(t *testing.T) {
		name := uniqueTableName("Ban dup")
		_, _, err := handler.Handle(ctx, manager.actor(), tables.CreateTableCommand{
			RequestID: uuid.New(), Name: name,
		})
		require.NoError(t, err)

		_, _, err = handler.Handle(ctx, manager.actor(), tables.CreateTableCommand{
			RequestID: uuid.New(), Name: strings.ToUpper(name),
		})
		require.ErrorIs(t, err, tables.ErrNameConflict)
	})

	t.Run("a cashier cannot create", func(t *testing.T) {
		cashier := newTestActor(t, q, []string{"CASHIER"}, true)
		_, _, err := handler.Handle(ctx, cashier.actor(), tables.CreateTableCommand{
			RequestID: uuid.New(), Name: uniqueTableName("Ban cashier"),
		})
		require.ErrorIs(t, err, tables.ErrForbidden)
	})
}

func TestCreateTableConcurrentSameName(t *testing.T) {
	db, q := openTablesTestDB(t)
	runner := tables.NewRunner(db, q)
	handler := tables.NewCreateTableHandler(runner)
	ctx := context.Background()
	manager := newTestActor(t, q, []string{"MANAGER"}, true)

	name := uniqueTableName("Ban race")
	const goroutines = 4
	errs := make([]error, goroutines)
	var start sync.WaitGroup
	var done sync.WaitGroup
	start.Add(1)

	for i := range goroutines {
		done.Add(1)
		go func() {
			defer done.Done()
			start.Wait()
			_, _, errs[i] = handler.Handle(ctx, manager.actor(), tables.CreateTableCommand{
				RequestID: uuid.New(), // distinct requests, same name
				Name:      name,
			})
		}()
	}
	start.Done()
	done.Wait()

	successes := 0
	for _, err := range errs {
		if err == nil {
			successes++
			continue
		}
		require.ErrorIs(t, err, tables.ErrNameConflict)
	}
	assert.Equal(t, 1, successes, "exactly one concurrent create may succeed")
}

func TestRenameTable(t *testing.T) {
	db, q := openTablesTestDB(t)
	runner := tables.NewRunner(db, q)
	create := tables.NewCreateTableHandler(runner)
	rename := tables.NewRenameTableHandler(runner)
	ctx := context.Background()
	manager := newTestActor(t, q, []string{"MANAGER"}, true)

	_, original, err := create.Handle(ctx, manager.actor(), tables.CreateTableCommand{
		RequestID: uuid.New(), Name: uniqueTableName("Ban old"),
	})
	require.NoError(t, err)

	t.Run("preserves identity and availability", func(t *testing.T) {
		newName := uniqueTableName("Ban new")
		code, res, err := rename.Handle(ctx, manager.actor(), tables.RenameTableCommand{
			RequestID: uuid.New(), TableID: original.ID, Name: newName,
		})
		require.NoError(t, err)
		assert.Equal(t, 200, code)
		assert.Equal(t, original.ID, res.ID, "rename must preserve identity")
		assert.Equal(t, newName, res.Name)
		assert.Equal(t, original.Available, res.Available)
		assert.Equal(t, 1, countAuditEvents(t, db, tables.EventTableRenamed, res.ID))
	})

	t.Run("rejects a missing Table", func(t *testing.T) {
		_, _, err := rename.Handle(ctx, manager.actor(), tables.RenameTableCommand{
			RequestID: uuid.New(), TableID: uuid.New(), Name: uniqueTableName("Ban ghost"),
		})
		require.ErrorIs(t, err, tables.ErrTableNotFound)
	})

	t.Run("rejects a conflicting name", func(t *testing.T) {
		occupied := uniqueTableName("Ban taken")
		_, _, err := create.Handle(ctx, manager.actor(), tables.CreateTableCommand{
			RequestID: uuid.New(), Name: occupied,
		})
		require.NoError(t, err)

		_, _, err = rename.Handle(ctx, manager.actor(), tables.RenameTableCommand{
			RequestID: uuid.New(), TableID: original.ID, Name: occupied,
		})
		require.ErrorIs(t, err, tables.ErrNameConflict)
	})
}

func TestSetTableAvailability(t *testing.T) {
	db, q := openTablesTestDB(t)
	runner := tables.NewRunner(db, q)
	create := tables.NewCreateTableHandler(runner)
	setAvail := tables.NewSetTableAvailabilityHandler(runner)
	ctx := context.Background()
	manager := newTestActor(t, q, []string{"MANAGER"}, true)

	_, created, err := create.Handle(ctx, manager.actor(), tables.CreateTableCommand{
		RequestID: uuid.New(), Name: uniqueTableName("Ban avail"),
	})
	require.NoError(t, err)

	readUpdatedAt := func() string {
		var ts string
		require.NoError(t, db.QueryRow(
			`SELECT updated_at::text FROM tables WHERE id = $1`, created.ID).Scan(&ts))
		return ts
	}

	t.Run("turns availability off and audits it", func(t *testing.T) {
		code, res, err := setAvail.Handle(ctx, manager.actor(), tables.SetTableAvailabilityCommand{
			RequestID: uuid.New(), TableID: created.ID, Available: false,
		})
		require.NoError(t, err)
		assert.Equal(t, 200, code)
		assert.False(t, res.Available)
		assert.Equal(t, 1, countAuditEvents(t, db, tables.EventTableAvailabilityChanged, created.ID))
	})

	t.Run("a same-state request is a no-op", func(t *testing.T) {
		before := readUpdatedAt()

		code, res, err := setAvail.Handle(ctx, manager.actor(), tables.SetTableAvailabilityCommand{
			RequestID: uuid.New(), TableID: created.ID, Available: false,
		})
		require.NoError(t, err)
		assert.Equal(t, 200, code)
		assert.False(t, res.Available)
		assert.Equal(t, before, readUpdatedAt(), "a no-op must not touch updated_at")
		assert.Equal(t, 1, countAuditEvents(t, db, tables.EventTableAvailabilityChanged, created.ID),
			"a no-op must not write a second audit event")
	})

	t.Run("restores availability without deleting the record", func(t *testing.T) {
		_, res, err := setAvail.Handle(ctx, manager.actor(), tables.SetTableAvailabilityCommand{
			RequestID: uuid.New(), TableID: created.ID, Available: true,
		})
		require.NoError(t, err)
		assert.True(t, res.Available)
		assert.Equal(t, created.ID, res.ID)
		assert.Equal(t, 2, countAuditEvents(t, db, tables.EventTableAvailabilityChanged, created.ID))
	})

	t.Run("rejects a missing Table", func(t *testing.T) {
		_, _, err := setAvail.Handle(ctx, manager.actor(), tables.SetTableAvailabilityCommand{
			RequestID: uuid.New(), TableID: uuid.New(), Available: false,
		})
		require.ErrorIs(t, err, tables.ErrTableNotFound)
	})
}

func TestRenameDeniedAfterRoleRemoval(t *testing.T) {
	db, q := openTablesTestDB(t)
	runner := tables.NewRunner(db, q)
	create := tables.NewCreateTableHandler(runner)
	rename := tables.NewRenameTableHandler(runner)
	ctx := context.Background()
	manager := newTestActor(t, q, []string{"MANAGER"}, true)

	originalName := uniqueTableName("Ban authority")
	_, created, err := create.Handle(ctx, manager.actor(), tables.CreateTableCommand{
		RequestID: uuid.New(), Name: originalName,
	})
	require.NoError(t, err)

	// Strip the role mid-session.
	require.NoError(t, q.ClearStaffRoles(ctx, manager.StaffID))

	_, _, err = rename.Handle(ctx, manager.actor(), tables.RenameTableCommand{
		RequestID: uuid.New(), TableID: created.ID, Name: uniqueTableName("Ban renamed"),
	})
	require.ErrorIs(t, err, tables.ErrForbidden)

	var currentName string
	require.NoError(t, db.QueryRow(`SELECT name FROM tables WHERE id = $1`, created.ID).Scan(&currentName))
	assert.Equal(t, originalName, currentName, "a denied rename must change nothing")
}

func TestReplayDeniedAfterRoleRemoval(t *testing.T) {
	db, q := openTablesTestDB(t)
	runner := tables.NewRunner(db, q)
	create := tables.NewCreateTableHandler(runner)
	ctx := context.Background()
	manager := newTestActor(t, q, []string{"MANAGER"}, true)

	requestID := uuid.New()
	cmd := tables.CreateTableCommand{RequestID: requestID, Name: uniqueTableName("Ban replay")}
	_, _, err := create.Handle(ctx, manager.actor(), cmd)
	require.NoError(t, err)

	require.NoError(t, q.ClearStaffRoles(ctx, manager.StaffID))

	// Authority is checked before replay, so the same request is now refused.
	_, _, err = create.Handle(ctx, manager.actor(), cmd)
	require.ErrorIs(t, err, tables.ErrForbidden)
}
```

If `ClearStaffRoles` has a different generated name or signature, check `internal/database/sqlc/auth.sql.go` and use the real one; `sql/queries/auth.sql` defines `Add/ClearStaffRoles`.

- [ ] **Step 2: Run test to verify it fails**

Run: `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -p 1 -tags=integration ./internal/tables/... -run "TestCreateTable|TestRenameTable|TestSetTableAvailability" -v`

Expected: FAIL — `tables.NewCreateTableHandler` undefined.

- [ ] **Step 3: Write the DTOs**

Create `internal/tables/dto.go`:

```go
package tables

import "github.com/google/uuid"

// CreateTableCommand creates a new Table.
type CreateTableCommand struct {
	RequestID uuid.UUID `json:"request_id"`
	Name      string    `json:"name"`
}

// RenameTableCommand renames an existing Table. TableID comes from the route.
type RenameTableCommand struct {
	RequestID uuid.UUID `json:"request_id"`
	TableID   uuid.UUID `json:"-"`
	Name      string    `json:"name"`
}

// SetTableAvailabilityCommand changes a Table's Availability.
// TableID comes from the route.
type SetTableAvailabilityCommand struct {
	RequestID uuid.UUID `json:"request_id"`
	TableID   uuid.UUID `json:"-"`
	Available bool      `json:"available"`
}

// TableResponse is the API representation of a Table. It is the result of all
// three commands and the base of each overview row.
type TableResponse struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Available bool      `json:"available"`
}

// TableOccupant identifies one active Service Session seated at a Table.
// It carries exactly these two fields; no other Sales detail crosses the
// Tables boundary.
type TableOccupant struct {
	ServiceSessionID uuid.UUID `json:"service_session_id"`
	ServiceNumber    string    `json:"service_number"`
}

// TableOverviewRow is one Table plus its current occupants.
type TableOverviewRow struct {
	TableResponse
	CurrentServiceSessions []TableOccupant `json:"current_service_sessions"`
}
```

`TableID` is tagged `json:"-"` so the route parameter cannot be overridden by a body field, and so Swagger does not advertise it as a body property.

- [ ] **Step 4: Write the command handlers**

Create `internal/tables/commands.go`:

```go
package tables

import (
	"context"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
)

type createTableFingerprint struct {
	Name string `json:"name"`
}

type renameTableFingerprint struct {
	TableID uuid.UUID `json:"table_id"`
	Name    string    `json:"name"`
}

type setAvailabilityFingerprint struct {
	TableID   uuid.UUID `json:"table_id"`
	Available bool      `json:"available"`
}

type tableCreatedAuditDetails struct {
	TableID uuid.UUID `json:"table_id"`
	Name    string    `json:"name"`
}

type tableRenamedAuditDetails struct {
	TableID    uuid.UUID `json:"table_id"`
	BeforeName string    `json:"before_name"`
	AfterName  string    `json:"after_name"`
}

type tableAvailabilityAuditDetails struct {
	TableID         uuid.UUID `json:"table_id"`
	BeforeAvailable bool      `json:"before_available"`
	AfterAvailable  bool      `json:"after_available"`
}

func toTableResponse(row sqlc.Table) TableResponse {
	return TableResponse{ID: row.ID, Name: row.Name, Available: row.Available}
}

// CreateTableHandler handles Table creation.
type CreateTableHandler struct{ runner *Runner }

// NewCreateTableHandler creates a new CreateTableHandler.
func NewCreateTableHandler(runner *Runner) *CreateTableHandler {
	return &CreateTableHandler{runner: runner}
}

// Handle executes the Table creation command.
func (h *CreateTableHandler) Handle(ctx context.Context, actor Actor, cmd CreateTableCommand) (int, TableResponse, error) {
	display, key := NormalizeTableName(cmd.Name)
	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpCreateTable,
		Fingerprint: createTableFingerprint{Name: display},
		Required:    []string{CapTablesAdminister},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(q *sqlc.Queries) (int, TableResponse, AuditRecord, error) {
			if err := ValidateTableName(display); err != nil {
				return 0, TableResponse{}, AuditRecord{}, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error())
			}

			row, err := q.CreateTable(ctx, sqlc.CreateTableParams{
				Name:           display,
				NormalizedName: key,
			})
			if err != nil {
				return 0, TableResponse{}, AuditRecord{}, MapDBError(err)
			}

			return 201, toTableResponse(row), AuditRecord{
				EventType: EventTableCreated,
				Details:   tableCreatedAuditDetails{TableID: row.ID, Name: row.Name},
			}, nil
		})
}

// RenameTableHandler handles Table renaming.
type RenameTableHandler struct{ runner *Runner }

// NewRenameTableHandler creates a new RenameTableHandler.
func NewRenameTableHandler(runner *Runner) *RenameTableHandler {
	return &RenameTableHandler{runner: runner}
}

// Handle executes the Table rename command.
func (h *RenameTableHandler) Handle(ctx context.Context, actor Actor, cmd RenameTableCommand) (int, TableResponse, error) {
	display, key := NormalizeTableName(cmd.Name)
	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpRenameTable,
		Fingerprint: renameTableFingerprint{TableID: cmd.TableID, Name: display},
		Required:    []string{CapTablesAdminister},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(q *sqlc.Queries) (int, TableResponse, AuditRecord, error) {
			if err := ValidateTableName(display); err != nil {
				return 0, TableResponse{}, AuditRecord{}, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error())
			}

			before, err := q.GetTableForUpdate(ctx, cmd.TableID)
			if err != nil {
				return 0, TableResponse{}, AuditRecord{}, MapDBError(err)
			}

			after, err := q.RenameTable(ctx, sqlc.RenameTableParams{
				ID:             cmd.TableID,
				Name:           display,
				NormalizedName: key,
			})
			if err != nil {
				return 0, TableResponse{}, AuditRecord{}, MapDBError(err)
			}

			return 200, toTableResponse(after), AuditRecord{
				EventType: EventTableRenamed,
				Details: tableRenamedAuditDetails{
					TableID:    after.ID,
					BeforeName: before.Name,
					AfterName:  after.Name,
				},
			}, nil
		})
}

// SetTableAvailabilityHandler handles Table Availability changes.
type SetTableAvailabilityHandler struct{ runner *Runner }

// NewSetTableAvailabilityHandler creates a new SetTableAvailabilityHandler.
func NewSetTableAvailabilityHandler(runner *Runner) *SetTableAvailabilityHandler {
	return &SetTableAvailabilityHandler{runner: runner}
}

// Handle executes the Availability command. Setting the current value again is
// a successful no-op: it returns current state, leaves updated_at alone, and
// writes no audit event.
func (h *SetTableAvailabilityHandler) Handle(ctx context.Context, actor Actor, cmd SetTableAvailabilityCommand) (int, TableResponse, error) {
	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpSetTableAvailability,
		Fingerprint: setAvailabilityFingerprint{
			TableID:   cmd.TableID,
			Available: cmd.Available,
		},
		Required: []string{CapTablesAdminister},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(q *sqlc.Queries) (int, TableResponse, AuditRecord, error) {
			before, err := q.GetTableForUpdate(ctx, cmd.TableID)
			if err != nil {
				return 0, TableResponse{}, AuditRecord{}, MapDBError(err)
			}

			if before.Available == cmd.Available {
				// Same-state no-op: no row change, no audit event.
				return 200, toTableResponse(before), AuditRecord{}, nil
			}

			after, err := q.SetTableAvailability(ctx, sqlc.SetTableAvailabilityParams{
				ID:        cmd.TableID,
				Available: cmd.Available,
			})
			if err != nil {
				return 0, TableResponse{}, AuditRecord{}, MapDBError(err)
			}

			return 200, toTableResponse(after), AuditRecord{
				EventType: EventTableAvailabilityChanged,
				Details: tableAvailabilityAuditDetails{
					TableID:         after.ID,
					BeforeAvailable: before.Available,
					AfterAvailable:  after.Available,
				},
			}, nil
		})
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -race -p 1 -tags=integration ./internal/tables/... -v`

Expected: PASS — every test so far.

- [ ] **Step 6: Commit**

```bash
git add internal/tables/dto.go internal/tables/commands.go internal/tables/commands_integration_test.go
git commit -m "feat(tables): add create, rename, and set-availability commands

A same-state Availability request is a successful no-op that leaves
updated_at untouched and writes no audit event."
```

---

### Task 7: Overview projection with occupancy

**Files:**
- Create: `internal/tables/overview.go`
- Test: `internal/tables/overview_integration_test.go`

**Interfaces:**
- Consumes: Tasks 2–6, including `TableOverviewRow` and `TableOccupant` from `dto.go`.
- Produces: `OverviewHandler` with `NewOverviewHandler(runner *Runner) *OverviewHandler` and `Handle(ctx context.Context, actor Actor) ([]TableOverviewRow, error)`.

- [ ] **Step 1: Write the failing test**

Create `internal/tables/overview_integration_test.go`:

```go
//go:build integration

package tables_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/tables"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedServiceSession inserts a Sales-owned service_sessions row directly.
// Phase 3 never writes these through its API; Phase 5 owns that behavior.
func seedServiceSession(t *testing.T, db *sql.DB, staffID uuid.UUID, serviceNumber, state string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	require.NoError(t, db.QueryRow(
		`INSERT INTO service_sessions (service_number, service_mode, state, created_by_staff_identity_id)
		 VALUES ($1, 'DINE_IN', $2, $3) RETURNING id`,
		serviceNumber, state, staffID).Scan(&id))
	return id
}

func seedAssignment(t *testing.T, db *sql.DB, tableID, sessionID, staffID uuid.UUID, sequence int, released bool) {
	t.Helper()
	if released {
		_, err := db.Exec(
			`INSERT INTO table_assignments
			   (table_id, service_session_id, assigned_by_staff_identity_id, sequence,
			    released_at, released_by_staff_identity_id)
			 VALUES ($1, $2, $3, $4, now(), $3)`,
			tableID, sessionID, staffID, sequence)
		require.NoError(t, err)
		return
	}
	_, err := db.Exec(
		`INSERT INTO table_assignments
		   (table_id, service_session_id, assigned_by_staff_identity_id, sequence)
		 VALUES ($1, $2, $3, $4)`,
		tableID, sessionID, staffID, sequence)
	require.NoError(t, err)
}

func findRow(rows []tables.TableOverviewRow, id uuid.UUID) *tables.TableOverviewRow {
	for i := range rows {
		if rows[i].ID == id {
			return &rows[i]
		}
	}
	return nil
}

// randomServiceNumber returns a value matching ^[A-Z0-9]{6}$.
func randomServiceNumber() string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	raw := uuid.New()
	out := make([]byte, 6)
	for i := range out {
		out[i] = alphabet[int(raw[i])%len(alphabet)]
	}
	return string(out)
}

func TestOverviewReportsOccupancy(t *testing.T) {
	db, q := openTablesTestDB(t)
	runner := tables.NewRunner(db, q)
	create := tables.NewCreateTableHandler(runner)
	overview := tables.NewOverviewHandler(runner)
	ctx := context.Background()

	manager := newTestActor(t, q, []string{"MANAGER"}, true)
	cashier := newTestActor(t, q, []string{"CASHIER"}, true)

	_, shared, err := create.Handle(ctx, manager.actor(), tables.CreateTableCommand{
		RequestID: uuid.New(), Name: uniqueTableName("Ban ghep"),
	})
	require.NoError(t, err)
	_, empty, err := create.Handle(ctx, manager.actor(), tables.CreateTableCommand{
		RequestID: uuid.New(), Name: uniqueTableName("Ban trong"),
	})
	require.NoError(t, err)

	t.Run("both Tables start unoccupied", func(t *testing.T) {
		rows, err := overview.Handle(ctx, cashier.actor())
		require.NoError(t, err)
		require.NotNil(t, findRow(rows, shared.ID))
		assert.Empty(t, findRow(rows, shared.ID).CurrentServiceSessions)
		assert.Empty(t, findRow(rows, empty.ID).CurrentServiceSessions)
	})

	numberOne := randomServiceNumber()
	numberTwo := randomServiceNumber()
	require.NotEqual(t, numberOne, numberTwo)
	sessionOne := seedServiceSession(t, db, manager.StaffID, numberOne, "ACTIVE")
	sessionTwo := seedServiceSession(t, db, manager.StaffID, numberTwo, "ACTIVE")
	seedAssignment(t, db, shared.ID, sessionOne, manager.StaffID, 1, false)
	seedAssignment(t, db, shared.ID, sessionTwo, manager.StaffID, 2, false)

	t.Run("reports shared occupancy in assignment order", func(t *testing.T) {
		rows, err := overview.Handle(ctx, cashier.actor())
		require.NoError(t, err)
		row := findRow(rows, shared.ID)
		require.NotNil(t, row)
		require.Len(t, row.CurrentServiceSessions, 2)
		assert.Equal(t, sessionOne, row.CurrentServiceSessions[0].ServiceSessionID)
		assert.Equal(t, numberOne, row.CurrentServiceSessions[0].ServiceNumber)
		assert.Equal(t, sessionTwo, row.CurrentServiceSessions[1].ServiceSessionID)

		assert.Empty(t, findRow(rows, empty.ID).CurrentServiceSessions)
	})

	t.Run("occupants expose exactly two fields", func(t *testing.T) {
		rows, err := overview.Handle(ctx, cashier.actor())
		require.NoError(t, err)
		row := findRow(rows, shared.ID)
		require.NotNil(t, row)

		raw, err := json.Marshal(row.CurrentServiceSessions[0])
		require.NoError(t, err)
		var fields map[string]any
		require.NoError(t, json.Unmarshal(raw, &fields))
		assert.Len(t, fields, 2)
		assert.Contains(t, fields, "service_session_id")
		assert.Contains(t, fields, "service_number")
	})

	t.Run("excludes released assignments", func(t *testing.T) {
		_, released, err := create.Handle(ctx, manager.actor(), tables.CreateTableCommand{
			RequestID: uuid.New(), Name: uniqueTableName("Ban released"),
		})
		require.NoError(t, err)
		session := seedServiceSession(t, db, manager.StaffID, randomServiceNumber(), "ACTIVE")
		seedAssignment(t, db, released.ID, session, manager.StaffID, 1, true)

		rows, err := overview.Handle(ctx, cashier.actor())
		require.NoError(t, err)
		assert.Empty(t, findRow(rows, released.ID).CurrentServiceSessions)
	})

	t.Run("excludes non-active sessions", func(t *testing.T) {
		_, done, err := create.Handle(ctx, manager.actor(), tables.CreateTableCommand{
			RequestID: uuid.New(), Name: uniqueTableName("Ban done"),
		})
		require.NoError(t, err)
		session := seedServiceSession(t, db, manager.StaffID, randomServiceNumber(), "COMPLETED")
		seedAssignment(t, db, done.ID, session, manager.StaffID, 1, false)

		rows, err := overview.Handle(ctx, cashier.actor())
		require.NoError(t, err)
		assert.Empty(t, findRow(rows, done.ID).CurrentServiceSessions)
	})

	t.Run("an unavailable Table keeps its occupants", func(t *testing.T) {
		setAvail := tables.NewSetTableAvailabilityHandler(runner)
		_, _, err := setAvail.Handle(ctx, manager.actor(), tables.SetTableAvailabilityCommand{
			RequestID: uuid.New(), TableID: shared.ID, Available: false,
		})
		require.NoError(t, err)

		rows, err := overview.Handle(ctx, cashier.actor())
		require.NoError(t, err)
		row := findRow(rows, shared.ID)
		require.NotNil(t, row)
		assert.False(t, row.Available)
		assert.Len(t, row.CurrentServiceSessions, 2,
			"Availability and occupancy are independent")
	})
}

func TestOverviewSerializesEmptyOccupancyAsArray(t *testing.T) {
	db, q := openTablesTestDB(t)
	runner := tables.NewRunner(db, q)
	create := tables.NewCreateTableHandler(runner)
	overview := tables.NewOverviewHandler(runner)
	ctx := context.Background()
	manager := newTestActor(t, q, []string{"MANAGER"}, true)

	_, created, err := create.Handle(ctx, manager.actor(), tables.CreateTableCommand{
		RequestID: uuid.New(), Name: uniqueTableName("Ban json"),
	})
	require.NoError(t, err)

	rows, err := overview.Handle(ctx, manager.actor())
	require.NoError(t, err)
	row := findRow(rows, created.ID)
	require.NotNil(t, row)

	raw, err := json.Marshal(row)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"current_service_sessions":[]`,
		"empty occupancy must serialize as [] and never null")
}

func TestOverviewDeniedWithoutSalesOperate(t *testing.T) {
	db, q := openTablesTestDB(t)
	runner := tables.NewRunner(db, q)
	overview := tables.NewOverviewHandler(runner)
	ctx := context.Background()

	barista := newTestActor(t, q, []string{"BARISTA"}, true)
	_, err := overview.Handle(ctx, barista.actor())
	require.ErrorIs(t, err, tables.ErrForbidden)
}

func TestOverviewOrdersTablesByCreation(t *testing.T) {
	db, q := openTablesTestDB(t)
	runner := tables.NewRunner(db, q)
	create := tables.NewCreateTableHandler(runner)
	overview := tables.NewOverviewHandler(runner)
	ctx := context.Background()
	manager := newTestActor(t, q, []string{"MANAGER"}, true)

	_, first, err := create.Handle(ctx, manager.actor(), tables.CreateTableCommand{
		RequestID: uuid.New(), Name: uniqueTableName("Ban order 1"),
	})
	require.NoError(t, err)
	_, second, err := create.Handle(ctx, manager.actor(), tables.CreateTableCommand{
		RequestID: uuid.New(), Name: uniqueTableName("Ban order 2"),
	})
	require.NoError(t, err)

	rows, err := overview.Handle(ctx, manager.actor())
	require.NoError(t, err)

	firstIndex, secondIndex := -1, -1
	for i := range rows {
		switch rows[i].ID {
		case first.ID:
			firstIndex = i
		case second.ID:
			secondIndex = i
		}
	}
	require.NotEqual(t, -1, firstIndex)
	require.NotEqual(t, -1, secondIndex)
	assert.Less(t, firstIndex, secondIndex, "older Tables come first")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -p 1 -tags=integration ./internal/tables/... -run TestOverview -v`

Expected: FAIL — `tables.NewOverviewHandler` undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/tables/overview.go`:

```go
package tables

import (
	"context"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

// OverviewHandler serves the Table overview read.
type OverviewHandler struct{ runner *Runner }

// NewOverviewHandler creates a new OverviewHandler.
func NewOverviewHandler(runner *Runner) *OverviewHandler {
	return &OverviewHandler{runner: runner}
}

// Handle returns every Table with the active Service Sessions occupying it.
//
// Occupancy is read from the Sales-owned service_sessions and
// table_assignments tables through this slice's own query. The Tables slice
// never imports internal/sales.
func (h *OverviewHandler) Handle(ctx context.Context, actor Actor) ([]TableOverviewRow, error) {
	return ExecuteRead(ctx, h.runner, actor, CapSalesOperate,
		func(q *sqlc.Queries) ([]TableOverviewRow, error) {
			tableRows, err := q.ListTables(ctx)
			if err != nil {
				return nil, fmt.Errorf("list tables: %w", err)
			}

			occupantRows, err := q.ListCurrentTableOccupants(ctx)
			if err != nil {
				return nil, fmt.Errorf("list current table occupants: %w", err)
			}

			// The query already orders by (assigned_at, id), so appending in
			// scan order preserves assignment order per Table.
			byTable := make(map[uuid.UUID][]TableOccupant, len(tableRows))
			for _, occ := range occupantRows {
				byTable[occ.TableID] = append(byTable[occ.TableID], TableOccupant{
					ServiceSessionID: occ.ServiceSessionID,
					ServiceNumber:    occ.ServiceNumber,
				})
			}

			out := make([]TableOverviewRow, 0, len(tableRows))
			for _, row := range tableRows {
				occupants := byTable[row.ID]
				if occupants == nil {
					// Serialize as [] rather than null.
					occupants = []TableOccupant{}
				}
				out = append(out, TableOverviewRow{
					TableResponse:          toTableResponse(row),
					CurrentServiceSessions: occupants,
				})
			}
			return out, nil
		})
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -race -p 1 -tags=integration ./internal/tables/... -run TestOverview -v`

Expected: PASS — all four overview tests.

- [ ] **Step 5: Commit**

```bash
git add internal/tables/overview.go internal/tables/overview_integration_test.go
git commit -m "feat(tables): add overview read with current occupancy

Reads Sales-owned tables through this slice's own query rather than
importing internal/sales. Empty occupancy serializes as []."
```

---

### Task 8: HTTP handlers, routes, and app wiring

**Files:**
- Create: `internal/tables/http.go`
- Create: `internal/tables/routes.go`
- Modify: `cmd/api/main.go` (after the `catalogSlices.RegisterRoutes(v1, authSlices.Middleware)` line, around line 166)
- Test: `internal/tables/routes_test.go`

**Interfaces:**
- Consumes: Tasks 3–7.
- Produces:
  - `type Slices struct` with `CreateTable`, `RenameTable`, `SetTableAvailability`, `Overview` handler fields and a `Runner`
  - `func NewSlices(db *sql.DB, queries *sqlc.Queries) *Slices`
  - `func (s *Slices) RegisterRoutes(v1 *echo.Group, authn *auth.Middleware)`

- [ ] **Step 1: Write the failing test**

Create `internal/tables/routes_test.go`:

```go
package tables_test

import (
	"net/http"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/tables"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegisterRoutes(t *testing.T) {
	e := echo.New()
	v1 := e.Group("/api/v1")

	slices := tables.NewSlices(nil, nil)
	slices.RegisterRoutes(v1, &auth.Middleware{})

	registered := make(map[string]bool)
	for _, r := range e.Routes() {
		registered[r.Method+" "+r.Path] = true
	}

	expected := []string{
		http.MethodGet + " /api/v1/tables/overview",
		http.MethodPost + " /api/v1/tables",
		http.MethodPatch + " /api/v1/tables/:table_id/name",
		http.MethodPatch + " /api/v1/tables/:table_id/availability",
	}
	for _, route := range expected {
		assert.True(t, registered[route], "route %q must be registered", route)
	}

	assert.Len(t, e.Routes(), len(expected),
		"tables must register exactly four routes and no generic CRUD")
}

func TestNewSlicesWiresEveryHandler(t *testing.T) {
	slices := tables.NewSlices(nil, nil)
	require.NotNil(t, slices.Runner)
	require.NotNil(t, slices.Overview)
	require.NotNil(t, slices.CreateTable)
	require.NotNil(t, slices.RenameTable)
	require.NotNil(t, slices.SetTableAvailability)
}
```

If constructing `&auth.Middleware{}` directly is not possible (unexported fields with required state), read `internal/catalog/routes_test.go` and mirror however it builds a middleware for the same assertion.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tables/... -run "TestRegisterRoutes|TestNewSlicesWiresEveryHandler" -v`

Expected: FAIL — `tables.NewSlices` undefined.

- [ ] **Step 3: Write the HTTP handlers**

Create `internal/tables/http.go`:

```go
package tables

import (
	"fmt"
	"net/http"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

func getActor(c echo.Context) (Actor, error) {
	claims := auth.GetStaff(c)
	if claims == nil {
		return Actor{}, fmt.Errorf("%w: unauthorized", response.ErrUnauthorized)
	}
	return Actor{StaffID: claims.StaffID, SessionID: claims.SessionID}, nil
}

func parseUUIDParam(c echo.Context, name string) (uuid.UUID, error) {
	val := c.Param(name)
	id, err := uuid.Parse(val)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: invalid %s UUID: %s", response.ErrInvalid, name, val)
	}
	return id, nil
}

func bindBody[T any](c echo.Context) (T, error) {
	var body T
	if err := c.Bind(&body); err != nil {
		return body, fmt.Errorf("%w: invalid request body: %s", response.ErrInvalid, err.Error())
	}
	return body, nil
}

func checkRequestID(id uuid.UUID) error {
	if id == uuid.Nil {
		return fmt.Errorf("%w: request_id is required", response.ErrInvalid)
	}
	return nil
}

func sendResult[T any](c echo.Context, status int, data T) error {
	if status == http.StatusCreated {
		return response.Created(c, data)
	}
	return response.OK(c, data)
}

func sendError(c echo.Context, err error) error {
	return response.Error(c, MapHTTPError(err))
}

// handleGetOverview returns every Table with its current occupancy.
//
//	@Summary		Table overview
//	@Description	Lists every Table with the active Service Sessions occupying it
//	@Tags			tables
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	response.APIResponse{data=[]TableOverviewRow}
//	@Failure		401	{object}	response.APIResponse
//	@Failure		403	{object}	response.APIResponse
//	@Router			/tables/overview [get]
func (s *Slices) handleGetOverview(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	rows, err := s.Overview.Handle(c.Request().Context(), actor)
	if err != nil {
		return sendError(c, err)
	}
	return response.OK(c, rows)
}

// handleCreateTable creates a Table.
//
//	@Summary		Create a Table
//	@Description	Creates a Table with a unique name
//	@Tags			tables
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		CreateTableCommand	true	"Table to create"
//	@Success		201		{object}	response.APIResponse{data=TableResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Router			/tables [post]
func (s *Slices) handleCreateTable(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	cmd, err := bindBody[CreateTableCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(cmd.RequestID); err != nil {
		return sendError(c, err)
	}
	status, res, err := s.CreateTable.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
}

// handleRenameTable renames a Table.
//
//	@Summary		Rename a Table
//	@Description	Renames a Table, preserving its identity and history
//	@Tags			tables
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			table_id	path		string						true	"Table ID"
//	@Param			request		body		RenameTableCommand	true	"New name"
//	@Success		200			{object}	response.APIResponse{data=TableResponse}
//	@Failure		400			{object}	response.APIResponse
//	@Failure		401			{object}	response.APIResponse
//	@Failure		403			{object}	response.APIResponse
//	@Failure		404			{object}	response.APIResponse
//	@Failure		409			{object}	response.APIResponse
//	@Router			/tables/{table_id}/name [patch]
func (s *Slices) handleRenameTable(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	tableID, err := parseUUIDParam(c, "table_id")
	if err != nil {
		return sendError(c, err)
	}
	cmd, err := bindBody[RenameTableCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(cmd.RequestID); err != nil {
		return sendError(c, err)
	}
	cmd.TableID = tableID

	status, res, err := s.RenameTable.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
}

// handleSetTableAvailability changes a Table's Availability.
//
//	@Summary		Set Table Availability
//	@Description	Sets whether a Table is eligible for new work. Setting the current value again is a successful no-op.
//	@Tags			tables
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			table_id	path		string									true	"Table ID"
//	@Param			request		body		SetTableAvailabilityCommand	true	"Availability to set"
//	@Success		200			{object}	response.APIResponse{data=TableResponse}
//	@Failure		400			{object}	response.APIResponse
//	@Failure		401			{object}	response.APIResponse
//	@Failure		403			{object}	response.APIResponse
//	@Failure		404			{object}	response.APIResponse
//	@Router			/tables/{table_id}/availability [patch]
func (s *Slices) handleSetTableAvailability(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	tableID, err := parseUUIDParam(c, "table_id")
	if err != nil {
		return sendError(c, err)
	}
	cmd, err := bindBody[SetTableAvailabilityCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(cmd.RequestID); err != nil {
		return sendError(c, err)
	}
	cmd.TableID = tableID

	status, res, err := s.SetTableAvailability.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
}
```

The Swagger envelope type is `response.APIResponse` (fields `Success`, `Data`, `Error`), and swag resolves model names unqualified within the same package — both match `internal/catalog/http.go`. Do not write `response.Envelope`; no such type exists.

- [ ] **Step 4: Write the routes**

Create `internal/tables/routes.go`:

```go
package tables

import (
	"database/sql"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/labstack/echo/v4"
)

// Slices aggregates the Tables handlers.
type Slices struct {
	Runner *Runner

	Overview             *OverviewHandler
	CreateTable          *CreateTableHandler
	RenameTable          *RenameTableHandler
	SetTableAvailability *SetTableAvailabilityHandler
}

// NewSlices wires every Tables handler onto a shared Runner.
func NewSlices(db *sql.DB, queries *sqlc.Queries) *Slices {
	runner := NewRunner(db, queries)
	return &Slices{
		Runner:               runner,
		Overview:             NewOverviewHandler(runner),
		CreateTable:          NewCreateTableHandler(runner),
		RenameTable:          NewRenameTableHandler(runner),
		SetTableAvailability: NewSetTableAvailabilityHandler(runner),
	}
}

// RegisterRoutes mounts the Tables routes under /tables on the provided group.
func (s *Slices) RegisterRoutes(v1 *echo.Group, authn *auth.Middleware) {
	g := v1.Group("/tables", authn.RequireAuth())

	g.GET("/overview", s.handleGetOverview, authn.RequireCapability(CapSalesOperate))
	g.POST("", s.handleCreateTable, authn.RequireCapability(CapTablesAdminister))
	g.PATCH("/:table_id/name", s.handleRenameTable, authn.RequireCapability(CapTablesAdminister))
	g.PATCH("/:table_id/availability", s.handleSetTableAvailability, authn.RequireCapability(CapTablesAdminister))
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test -race ./internal/tables/... -run "TestRegisterRoutes|TestNewSlicesWiresEveryHandler" -v`

Expected: PASS.

If `g.POST("", ...)` registers as `/api/v1/tables` with a trailing-slash mismatch, use `v1.POST("/tables", ...)` outside the group and attach `authn.RequireAuth()` and `authn.RequireCapability(CapTablesAdminister)` explicitly.

- [ ] **Step 6: Wire into the application**

In `cmd/api/main.go`, directly after the existing line `catalogSlices.RegisterRoutes(v1, authSlices.Middleware)`, add:

```go
	tablesSlices := tables.NewSlices(db, queries)
	tablesSlices.RegisterRoutes(v1, authSlices.Middleware)
```

Add `"github.com/Mirai3103/pos-cafe/internal/tables"` to the import block.

- [ ] **Step 7: Verify the application builds and routes are live**

Run: `go build ./... && go vet ./...`

Expected: no output, exit 0.

- [ ] **Step 8: Commit**

```bash
git add internal/tables/http.go internal/tables/routes.go internal/tables/routes_test.go cmd/api/main.go
git commit -m "feat(tables): add HTTP handlers, routes, and app wiring

Four routes only: overview read plus create, rename, and set-availability."
```

---

### Task 9: End-to-end HTTP tests

**Files:**
- Create: `internal/tables/tables_integration_test.go`

**Interfaces:**
- Consumes: every earlier task. Produces nothing new.

- [ ] **Step 1: Write the failing test**

Create `internal/tables/tables_integration_test.go`:

```go
//go:build integration

package tables_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/tables"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type envelope struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// newTestServer builds an Echo server with auth and tables routes mounted the
// same way cmd/api/main.go mounts them.
func newTestServer(t *testing.T) (*echo.Echo, *sqlc.Queries) {
	t.Helper()
	db, q := openTablesTestDB(t)

	e := echo.New()
	v1 := e.Group("/api/v1")

	authSlices := auth.NewSlices(db, q)
	authSlices.RegisterRoutes(v1)

	tablesSlices := tables.NewSlices(db, q)
	tablesSlices.RegisterRoutes(v1, authSlices.Middleware)

	return e, q
}

// signIn creates an identity with the given roles and returns a bearer token.
// It uses the real sign-in endpoint so the middleware path is exercised.
func signIn(t *testing.T, e *echo.Echo, q *sqlc.Queries, roles []string) string {
	t.Helper()
	ctx := context.Background()

	loginCode := fmt.Sprintf("H%06d", loginCounter.Add(1))
	pin := "2468"
	hash, err := auth.HashPin(pin)
	require.NoError(t, err)

	row, err := q.CreateStaffIdentity(ctx, sqlc.CreateStaffIdentityParams{
		DisplayName: "HTTP Test " + loginCode,
		Btrim:       loginCode,
		PinHash:     hash,
		Enabled:     true,
	})
	require.NoError(t, err)
	for _, role := range roles {
		require.NoError(t, q.AddStaffRole(ctx, sqlc.AddStaffRoleParams{
			StaffIdentityID: row.ID, Role: role,
		}))
	}

	body, _ := json.Marshal(map[string]string{"login_code": loginCode, "pin": pin})
	rec := doRequest(t, e, http.MethodPost, "/api/v1/auth/sign-in", "", body)
	require.Equal(t, http.StatusOK, rec.Code, "sign-in failed: %s", rec.Body.String())

	var env envelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	var payload struct {
		Token string `json:"token"`
	}
	require.NoError(t, json.Unmarshal(env.Data, &payload))
	require.NotEmpty(t, payload.Token)
	return payload.Token
}

func doRequest(t *testing.T, e *echo.Echo, method, path, token string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestTablesHTTPHappyPath(t *testing.T) {
	e, q := newTestServer(t)
	token := signIn(t, e, q, []string{"MANAGER"})

	name := uniqueTableName("HTTP Ban")
	body, _ := json.Marshal(map[string]any{"request_id": uuid.New(), "name": name})
	rec := doRequest(t, e, http.MethodPost, "/api/v1/tables", token, body)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var env envelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	assert.True(t, env.Success)

	var created tables.TableResponse
	require.NoError(t, json.Unmarshal(env.Data, &created))
	assert.Equal(t, name, created.Name)
	assert.True(t, created.Available)

	// Rename via the route parameter.
	newName := uniqueTableName("HTTP Ban renamed")
	body, _ = json.Marshal(map[string]any{"request_id": uuid.New(), "name": newName})
	rec = doRequest(t, e, http.MethodPatch,
		"/api/v1/tables/"+created.ID.String()+"/name", token, body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// Set availability.
	body, _ = json.Marshal(map[string]any{"request_id": uuid.New(), "available": false})
	rec = doRequest(t, e, http.MethodPatch,
		"/api/v1/tables/"+created.ID.String()+"/availability", token, body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// Overview includes the Table with an empty occupancy array.
	rec = doRequest(t, e, http.MethodGet, "/api/v1/tables/overview", token, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"current_service_sessions":[]`)
}

func TestTablesHTTPRejectsUnauthenticated(t *testing.T) {
	e, _ := newTestServer(t)

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/tables/overview"},
		{http.MethodPost, "/api/v1/tables"},
		{http.MethodPatch, "/api/v1/tables/" + uuid.NewString() + "/name"},
		{http.MethodPatch, "/api/v1/tables/" + uuid.NewString() + "/availability"},
	} {
		rec := doRequest(t, e, tc.method, tc.path, "", []byte(`{}`))
		assert.Equal(t, http.StatusUnauthorized, rec.Code, "%s %s", tc.method, tc.path)
	}
}

func TestTablesHTTPCapabilityMapping(t *testing.T) {
	e, q := newTestServer(t)

	t.Run("a barista cannot read the overview", func(t *testing.T) {
		token := signIn(t, e, q, []string{"BARISTA"})
		rec := doRequest(t, e, http.MethodGet, "/api/v1/tables/overview", token, nil)
		assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	})

	t.Run("a cashier reads the overview but cannot create", func(t *testing.T) {
		token := signIn(t, e, q, []string{"CASHIER"})

		rec := doRequest(t, e, http.MethodGet, "/api/v1/tables/overview", token, nil)
		assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		body, _ := json.Marshal(map[string]any{
			"request_id": uuid.New(), "name": uniqueTableName("Cashier Ban"),
		})
		rec = doRequest(t, e, http.MethodPost, "/api/v1/tables", token, body)
		assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	})
}

func TestTablesHTTPValidation(t *testing.T) {
	e, q := newTestServer(t)
	token := signIn(t, e, q, []string{"MANAGER"})

	t.Run("rejects an invalid table_id", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"request_id": uuid.New(), "name": "Ban"})
		rec := doRequest(t, e, http.MethodPatch, "/api/v1/tables/not-a-uuid/name", token, body)
		assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())

		var env envelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
		require.NotNil(t, env.Error)
		assert.Equal(t, "INVALID_INPUT", env.Error.Code)
	})

	t.Run("rejects a missing request_id", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"name": uniqueTableName("No request id")})
		rec := doRequest(t, e, http.MethodPost, "/api/v1/tables", token, body)
		assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	})

	t.Run("reports a name conflict as 409 TABLE_NAME_CONFLICT", func(t *testing.T) {
		name := uniqueTableName("Conflict Ban")
		body, _ := json.Marshal(map[string]any{"request_id": uuid.New(), "name": name})
		rec := doRequest(t, e, http.MethodPost, "/api/v1/tables", token, body)
		require.Equal(t, http.StatusCreated, rec.Code)

		body, _ = json.Marshal(map[string]any{"request_id": uuid.New(), "name": name})
		rec = doRequest(t, e, http.MethodPost, "/api/v1/tables", token, body)
		require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())

		var env envelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
		require.NotNil(t, env.Error)
		assert.Equal(t, "TABLE_NAME_CONFLICT", env.Error.Code)
	})

	t.Run("reports a missing Table as 404", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"request_id": uuid.New(), "name": uniqueTableName("Ghost Ban"),
		})
		rec := doRequest(t, e, http.MethodPatch,
			"/api/v1/tables/"+uuid.NewString()+"/name", token, body)
		assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	})

	t.Run("reports a conflicting request_id reuse as 409 REQUEST_CONFLICT", func(t *testing.T) {
		requestID := uuid.New()
		body, _ := json.Marshal(map[string]any{
			"request_id": requestID, "name": uniqueTableName("Idem Ban"),
		})
		rec := doRequest(t, e, http.MethodPost, "/api/v1/tables", token, body)
		require.Equal(t, http.StatusCreated, rec.Code)

		body, _ = json.Marshal(map[string]any{
			"request_id": requestID, "name": uniqueTableName("Idem Ban different"),
		})
		rec = doRequest(t, e, http.MethodPost, "/api/v1/tables", token, body)
		require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())

		var env envelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
		require.NotNil(t, env.Error)
		assert.Equal(t, "REQUEST_CONFLICT", env.Error.Code)
	})
}

func TestTablesHTTPReplayReturnsStoredResponse(t *testing.T) {
	e, q := newTestServer(t)
	token := signIn(t, e, q, []string{"MANAGER"})

	requestID := uuid.New()
	name := uniqueTableName("Replay Ban")
	body, _ := json.Marshal(map[string]any{"request_id": requestID, "name": name})

	first := doRequest(t, e, http.MethodPost, "/api/v1/tables", token, body)
	require.Equal(t, http.StatusCreated, first.Code, first.Body.String())

	second := doRequest(t, e, http.MethodPost, "/api/v1/tables", token, body)
	require.Equal(t, http.StatusCreated, second.Code, second.Body.String())
	assert.JSONEq(t, first.Body.String(), second.Body.String(),
		"an exact replay must return the stored response verbatim")
}
```

The sign-in route path, request field names, and token field name must match `internal/auth/routes.go` and its DTOs. Read them and adjust `signIn` if they differ; the assertions about status codes and error codes are the point of the test.

- [ ] **Step 2: Run test to verify it fails**

Run: `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -p 1 -tags=integration ./internal/tables/... -run TestTablesHTTP -v`

Expected: FAIL initially if any helper signature is wrong; fix the helpers until the assertions run.

- [ ] **Step 3: Make the tests pass**

No new production code should be required. If a test fails on behavior rather than on a helper signature, fix the production code in the file that owns that behavior — `http.go` for status and envelope handling, `errors.go` for error codes, `routes.go` for route or capability wiring.

- [ ] **Step 4: Run the full suite**

Run: `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -race -p 1 -tags=integration ./internal/tables/... -v`

Expected: PASS — every Tables test.

- [ ] **Step 5: Commit**

```bash
git add internal/tables/tables_integration_test.go
git commit -m "test(tables): add end-to-end HTTP tests

Covers capability mapping, validation, error codes, and idempotent replay
through the real Echo stack."
```

---

### Task 10: ADRs, Swagger, and full verification

**Files:**
- Modify: `spec/decisions.md` (append after ADR-005)
- Modify: `MIGRATE_PLAN.md` (Phase 3 status line and tracker row only)
- Modify: `docs/docs.go`, `docs/swagger.json`, `docs/swagger.yaml` (regenerated, do not hand-edit)

**Interfaces:**
- Consumes: every earlier task. Produces nothing new.

- [ ] **Step 1: Append the ADRs**

Append to `spec/decisions.md`:

```markdown
---

## ADR-006: Provision sớm schema Sales cho Phase 3 (`service_sessions`, `table_assignments`)
- **Ngày quyết định:** 2026-09-12
- **Trạng thái:** Accepted
- **Bối cảnh:** Theo `CONTEXT.md`, một Bàn "may be associated with one or more active Service Sessions". Read `overview` canonical của Tables phải trả về các Service Session đang chiếm bàn, tức là phụ thuộc vào `table_assignments` và `service_sessions` — hai bảng thuộc quyền sở hữu nghiệp vụ của `internal/sales` (Phase 5). Nếu chờ Phase 5, Phase 3 sẽ ship một contract API thiếu field và phải breaking change về sau.
- **Quyết định:**
  - Migration `000006` của Phase 3 tạo luôn `service_sessions` và `table_assignments`, kèm `COMMENT ON TABLE` ghi rõ quyền sở hữu thuộc `internal/sales` (Phase 5).
  - `service_sessions` lược bỏ **duy nhất** cột `sales_shift_id` vì `sales_shifts` là bảng của Phase 4. Phase 5 bổ sung bằng `ALTER TABLE service_sessions ADD COLUMN sales_shift_id UUID NOT NULL REFERENCES sales_shifts(id)`.
  - `internal/tables` **không** import `internal/sales`. Nó đọc occupancy qua query sqlc của riêng nó (`ListCurrentTableOccupants`), đúng nguyên tắc "dùng queries hoặc interface, đừng import struct" của MIGRATE_PLAN §4.1.
  - Phase 3 chỉ **đọc** hai bảng này, không ghi qua API. Test integration seed trực tiếp bằng SQL.
- **Hệ quả:**
  - Contract công khai của Tables hoàn chỉnh và ổn định ngay từ Phase 3; frontend không phải chịu breaking change khi Phase 5 lên.
  - Đổi lại, Phase 5 phải thực hiện đúng một thao tác `ALTER TABLE` bổ sung thay vì `CREATE TABLE`.

---

## ADR-007: Reaffirming the Shared `idempotency_keys` Table

* **Decision Date:** 2026-09-12
* **Status:** Accepted
* **Context:** ADR-005 mandated that all slices share a single `idempotency_keys` table. However, Phase 2 (Catalog) created a dedicated `catalog_mutation_requests` table, contradicting this decision and reintroducing the exact schema bloat anti-pattern that ADR-005 aimed to eliminate.
* **Decision:**
* Starting from Phase 3 onwards, all slices must write idempotency records to the shared `idempotency_keys` table, using fully qualified action names for the `action` column (e.g., `tables.create_table`, `tables.rename_table`, `tables.set_table_availability`).
* `catalog_mutation_requests` is acknowledged as a **historical exception**, not a precedent. Catalog will not be retrofitted during Phase 3; any cleanup will be handled as a separate task.
* Each slice continues to own its respective executor. Sharing the **database table** does not imply sharing the **helper logic**: `internal/tables` must not import idempotency helpers from `internal/auth`.


* **Consequences:**
* Prevents schema bloat by avoiding a new table for every slice.
* Preserves vertical slice boundaries at the code layer while consolidating the storage layer.

- [ ] **Step 2: Update the roadmap status only**

In `MIGRATE_PLAN.md`, change the Phase 3 heading from:

```markdown
### Phase 3: Tables & Floor Layout (`internal/tables`)
```

to:

```markdown
### Phase 3: Tables & Floor Layout (`internal/tables`) (✅ COMPLETED)
*Approved Design Spec:* [`docs/superpowers/specs/2026-09-12-tables-slice-design.md`](docs/superpowers/specs/2026-09-12-tables-slice-design.md)
*Implementation Plan:* [`docs/superpowers/plans/2026-09-12-tables-slice.md`](docs/superpowers/plans/2026-09-12-tables-slice.md)

> The checklist below predates the canonical source review and is superseded by the spec above. It is kept only as a record of the original sketch.
```

In the "Migration Progress Tracker" table, change the Catalog row's `1 / 5` to `5 / 5` and its status to `✅ DONE`, and change the Tables row to:

```markdown
| **3. Tables & Layout** | ✅ DONE | 4 | 4 / 4 | 2026-09-12 |
```

Do not rewrite the Phase 3 task detail. The spec is the design authority; this file tracks status only.

- [ ] **Step 3: Regenerate Swagger**

Run: `make swagger`

Expected: `docs/swagger.json` gains the four `/tables` paths and the `tables.TableResponse`, `tables.TableOverviewRow`, and `tables.TableOccupant` definitions.

- [ ] **Step 4: Verify Swagger covers all four operations**

Run: `grep -c '"/tables' docs/swagger.json`

Expected: at least `4`.

Run: `grep -o '"tables\.[A-Za-z]*"' docs/swagger.json | sort -u`

Expected: includes `"tables.TableResponse"`, `"tables.TableOverviewRow"`, and `"tables.TableOccupant"`.

- [ ] **Step 5: Run the complete verification suite**

Run each and confirm it passes before moving on:

```bash
go build ./...
make fmt
go vet ./...
make lint
make test
make test-integration
```

Expected: all exit 0. `make test-integration` must show the Auth and Catalog packages still passing alongside Tables.

If `make lint` flags the `internal/tables` package for duplicated helpers shared with `internal/catalog` (`getActor`, `parseUUIDParam`, `bindBody`), that duplication is deliberate: each slice owns its own HTTP helpers so the slices stay independent. Add a targeted `//nolint:dupl` with that reason rather than extracting a shared package.

- [ ] **Step 6: Commit**

```bash
git add spec/decisions.md MIGRATE_PLAN.md docs/
git commit -m "docs(tables): add ADR-006 and ADR-007, regenerate Swagger

ADR-006 records early provisioning of the Sales schema for the overview
read. ADR-007 reaffirms the shared idempotency_keys table and records the
Phase 2 catalog table as a historical exception."
```

---

## Verification Checklist

Run after Task 10. Every line must pass before the branch is considered done.

```bash
go build ./...
go vet ./...
make lint
make test
make test-integration
```

Then confirm each acceptance criterion from spec section 13:

1. `grep -c 'g\.\(GET\|POST\|PATCH\|PUT\|DELETE\)' internal/tables/routes.go` returns `4`.
2. `grep -E 'capacity|display_order|is_active' internal/database/migrations/000006_create_tables_slice.sql` returns nothing.
3. `grep -c 'COMMENT ON TABLE' internal/database/migrations/000006_create_tables_slice.sql` returns `2`.
4. `grep -rn 'CREATE TABLE.*mutation_requests' internal/database/migrations/000006_create_tables_slice.sql` returns nothing, and `grep -c idempotency_keys sql/queries/tables.sql` is at least `3`.
5. The `TestSetTableAvailability` no-op subtests pass.
6. `TestReplayDeniedAfterRoleRemoval` passes.
7. `TestOverviewSerializesEmptyOccupancyAsArray` and the two-field subtest pass.
8. `TestNormalizeTableName` and `TestCreateTableConcurrentSameName` pass.
9. The "an unavailable Table keeps its occupants" subtest passes.
10. `TestTablesHTTPValidation` passes with no 500 responses.
11. `make test-integration` shows Auth and Catalog packages passing.
12. `grep -c '"/tables' docs/swagger.json` is at least `4`.
13. `grep -c 'ADR-00[67]' spec/decisions.md` returns at least `2`.
14. `grep -rn 'internal/sales' internal/tables/` returns nothing.
15. `grep -rn 'auth.ExecuteWithIdempotency' internal/tables/` returns nothing.
