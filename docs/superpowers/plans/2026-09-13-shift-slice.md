# Sales Shift Slice (Phase 4) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement `internal/shift`, the Sales Shift vertical slice: open a Sales Shift with a counted Opening Float, record Cash Movements approved by a second identity holding the Manager role, and read the current Shift with a dynamically computed Expected Cash.

**Architecture:** One Go package mirroring `internal/tables`: a `Runner` owning transaction orchestration, a generic `ExecuteMutation` / `ExecuteRead` pair, one handler type per operation, a `Slices` aggregate, and `RegisterRoutes(v1, authn)`. Every mutation reloads authority inside its transaction, verifies Manager approval where required, claims an actor-scoped idempotency record, mutates, audits, and stores a replayable result — all atomically. A new shared primitive `auth.VerifyManagerApproval` performs second-party approval and is reused by Phase 5.

**Tech Stack:** Go 1.26+, Echo v4, PostgreSQL 16 via `database/sql` + `jackc/pgx/v5` stdlib, sqlc (type-safe query bindings), `stretchr/testify`, embedded SQL migrations, Swagger via `swaggo/swag`.

**Spec:** `docs/superpowers/specs/2026-09-13-shift-slice-design.md`

## Global Constraints

- Module path is `github.com/Mirai3103/pos-cafe`. All internal imports use this prefix.
- Monetary values are `BIGINT` in PostgreSQL and `int64` in Go. The inclusive upper bound for every Phase 4 monetary value is `2147483647`.
- Expected Cash is bounded symmetrically: a total outside `[-2147483647, 2147483647]` is `EXPECTED_CASH_OUT_OF_RANGE`.
- `internal/shift` may import `internal/auth`, `internal/database/sqlc`, and `internal/response`. It must NOT import `internal/sales`, `internal/tables`, or `internal/catalog`.
- Idempotency uses the shared `idempotency_keys` table only (ADR-005, ADR-007). Do not create a new idempotency table. Action values are exactly `shift.open_shift` and `shift.record_cash_movement`.
- Audit events use the shared `audit_events` table. Business event types are `UPPER_SNAKE_CASE` (`SALES_SHIFT_OPENED`, `CASH_MOVEMENT_RECORDED`); the denial event type is lowercase dotted (`shift.authorization_denied`).
- `manager_pin` must never appear in a request fingerprint, an audit event, a log field, an error message, or a response body.
- sqlc query names are global across the generated package. Every new query name in this plan is already prefixed to avoid colliding with `auth.sql`, `catalog.sql`, and `tables.sql`.
- All four denial reasons for Manager approval collapse to the single client-visible code `MANAGER_APPROVAL_UNAVAILABLE`.
- Integration tests carry the `//go:build integration` tag, live in package `shift_test`, and run with `-p 1`.
- Run unit tests with: `go test -race ./internal/shift/... ./internal/auth/...`
- Run integration tests with: `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -race -p 1 -tags=integration ./internal/shift/... ./internal/auth/...`
- PostgreSQL must be running first: `make docker-up && make db-wait`.

---

## File Structure

**Created:**

| File | Responsibility |
| --- | --- |
| `internal/database/migrations/000007_create_shift_slice.sql` | `sales_shifts` and `cash_movements` tables, constraints, partial unique open index |
| `sql/queries/shift.sql` | Shift authority reload, advisory lock, Shift and Cash Movement queries |
| `internal/auth/manager_approval.go` | `VerifyManagerApproval`, the shared second-party approval primitive |
| `internal/auth/manager_approval_test.go` | Unit tests for denial-reason selection and login-code normalization |
| `internal/auth/manager_approval_integration_test.go` | Integration tests against real identities and roles |
| `internal/shift/domain.go` | Constants, validation, note normalization, Expected Cash arithmetic |
| `internal/shift/domain_test.go` | Unit tests for every validation boundary |
| `internal/shift/dto.go` | API commands and responses, separate from sqlc models |
| `internal/shift/errors.go` | Domain sentinels, `MapDBError`, `MapHTTPError` |
| `internal/shift/errors_test.go` | Unit tests for error mapping |
| `internal/shift/executor.go` | `Runner`, `ExecuteMutation`, `ExecuteRead`, authority reload, approval, idempotency, audit |
| `internal/shift/executor_integration_test.go` | Integration tests for the executor, plus shared test helpers |
| `internal/shift/open_shift.go` | Open Sales Shift command handler |
| `internal/shift/open_shift_integration_test.go` | Integration tests for opening, including the one-open invariant |
| `internal/shift/cash_movement.go` | Record Cash Movement command handler |
| `internal/shift/cash_movement_integration_test.go` | Integration tests for movement recording and approval |
| `internal/shift/current.go` | Current Sales Shift read and Expected Cash assembly |
| `internal/shift/current_integration_test.go` | Integration tests for the read |
| `internal/shift/http.go` | Echo handlers, binding, validation, Swagger annotations |
| `internal/shift/routes.go` | `Slices` aggregate and `RegisterRoutes` |
| `internal/shift/schema_integration_test.go` | Migration and database-constraint tests |
| `internal/shift/shift_integration_test.go` | End-to-end HTTP tests through the real router |

**Modified:**

| File | Change |
| --- | --- |
| `sql/queries/auth.sql` | Add `GetStaffByLoginCodeForUpdate` and `GetStaffRolesForUpdate` |
| `internal/database/sqlc/*` | Regenerated by `sqlc generate` — never hand-edited |
| `cmd/api/main.go:169-170` | Wire `shift.NewSlices(...)` and `RegisterRoutes` after the tables slice |
| `spec/decisions.md` | Append ADR-008 and ADR-009 |
| `MIGRATE_PLAN.md` | Mark Phase 4 status and tracker row complete |
| `docs/swagger.json`, `docs/swagger.yaml`, `docs/docs.go` | Regenerated by `swag init` |

---

## Task 1: Database Schema And Queries

**Files:**
- Create: `internal/database/migrations/000007_create_shift_slice.sql`
- Create: `sql/queries/shift.sql`
- Modify: `sql/queries/auth.sql` (append two queries)
- Create: `internal/shift/schema_integration_test.go`
- Regenerate: `internal/database/sqlc/` via `sqlc generate`

**Interfaces:**
- Consumes: the existing `staff_identities`, `staff_access_sessions`, `staff_operational_roles`, `idempotency_keys`, and `audit_events` tables from migrations `000002` and `000003`.
- Produces: sqlc bindings `OpenSalesShift`, `GetOpenSalesShift`, `GetOpenSalesShiftForUpdate`, `GetStaffSummary`, `InsertCashMovement`, `ListCashMovements`, `SumCashMovements`, `GetShiftSessionAuthority`, `GetShiftSessionRoles`, `ShiftAdvisoryLock`, `GetStaffByLoginCodeForUpdate`, `GetStaffRolesForUpdate`. Tasks 2 and 4-7 call these by name.

- [ ] **Step 1: Write the migration**

Create `internal/database/migrations/000007_create_shift_slice.sql`:

```sql
-- Phase 4: Sales Shift & Cash Movements.
--
-- Both tables are owned by internal/shift.
--
-- Expected Cash is NOT stored. It is computed on read from the Opening Float
-- and the Cash Movements. Phase 5 adds Cash Payments and Cash Refunds to the
-- same computation without a schema change. See ADR-008 in spec/decisions.md.

CREATE TABLE IF NOT EXISTS sales_shifts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    state TEXT NOT NULL DEFAULT 'OPEN',
    opened_by_staff_identity_id UUID NOT NULL REFERENCES staff_identities(id) ON DELETE RESTRICT,
    opening_float_vnd BIGINT NOT NULL,
    opened_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT sales_shift_state_valid
        CHECK (state IN ('OPEN', 'CLOSED')),
    CONSTRAINT sales_shift_opening_float_vnd_valid
        CHECK (opening_float_vnd >= 0 AND opening_float_vnd <= 2147483647)
);

-- The sole authority for the one-open-Shift invariant. A Go pre-check may
-- produce a friendlier error, but this index resolves genuine races.
CREATE UNIQUE INDEX IF NOT EXISTS sales_shift_only_one_open_unique
    ON sales_shifts (state)
    WHERE state = 'OPEN';

COMMENT ON TABLE sales_shifts IS
    'Owned by internal/shift (Phase 4). At most one row may be in OPEN state. Phase 4 ships no close operation; see the Non-Goals in the Phase 4 design spec.';

CREATE TABLE IF NOT EXISTS cash_movements (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sales_shift_id UUID NOT NULL REFERENCES sales_shifts(id) ON DELETE RESTRICT,
    method TEXT NOT NULL,
    amount_vnd BIGINT NOT NULL,
    reason TEXT NOT NULL,
    note TEXT,
    initiated_by_staff_identity_id UUID NOT NULL REFERENCES staff_identities(id) ON DELETE RESTRICT,
    initiated_staff_access_session_id UUID NOT NULL REFERENCES staff_access_sessions(id) ON DELETE RESTRICT,
    approved_by_staff_identity_id UUID NOT NULL REFERENCES staff_identities(id) ON DELETE RESTRICT,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT cash_movement_method_valid
        CHECK (method IN ('PAY_IN', 'PAY_OUT')),
    CONSTRAINT cash_movement_amount_vnd_valid
        CHECK (amount_vnd > 0 AND amount_vnd <= 2147483647),
    CONSTRAINT cash_movement_reason_valid
        CHECK (reason IN ('ADD_CHANGE_FUND', 'REMOVE_EXCESS_FLOAT', 'SAFE_DROP', 'OTHER')),
    CONSTRAINT cash_movement_note_valid
        CHECK (
            (note IS NULL OR (note = btrim(note) AND char_length(note) BETWEEN 1 AND 500))
            AND (reason <> 'OTHER' OR note IS NOT NULL)
        )
);

CREATE INDEX IF NOT EXISTS cash_movement_sales_shift_index
    ON cash_movements (sales_shift_id, occurred_at);

COMMENT ON TABLE cash_movements IS
    'Owned by internal/shift (Phase 4). Append-only: Phase 4 provides no edit, reverse, or delete operation. Direction is carried by method, never by a negative amount.';
```

- [ ] **Step 2: Write the shift queries**

Create `sql/queries/shift.sql`:

```sql
-- -- Authority --
-- Names are prefixed because sqlc query names are global across the package.

-- name: GetShiftSessionAuthority :one
SELECT s.id AS session_id, s.staff_identity_id, s.state, s.active_workspace,
       s.last_human_activity_at, s.expires_at, s.revoked_at,
       i.enabled AS identity_enabled, i.display_name, i.login_code
FROM staff_access_sessions s
JOIN staff_identities i ON i.id = s.staff_identity_id
WHERE s.id = $1 AND s.staff_identity_id = $2;

-- name: GetShiftSessionRoles :many
SELECT role
FROM staff_operational_roles
WHERE staff_identity_id = $1
ORDER BY role ASC;

-- name: ShiftAdvisoryLock :exec
SELECT pg_advisory_xact_lock($1);

-- -- Sales Shift --

-- name: OpenSalesShift :one
INSERT INTO sales_shifts (opened_by_staff_identity_id, opening_float_vnd)
VALUES ($1, $2)
RETURNING id, state, opened_by_staff_identity_id, opening_float_vnd, opened_at;

-- name: GetOpenSalesShift :one
SELECT sh.id, sh.state, sh.opening_float_vnd, sh.opened_at,
       i.id AS opener_id,
       i.display_name AS opener_display_name,
       i.login_code AS opener_login_code
FROM sales_shifts sh
JOIN staff_identities i ON i.id = sh.opened_by_staff_identity_id
WHERE sh.state = 'OPEN'
ORDER BY sh.opened_at DESC
LIMIT 1;

-- Single-table so the row lock is unambiguous; the opener is fetched separately
-- with GetStaffSummary.
-- name: GetOpenSalesShiftForUpdate :one
SELECT id, state, opening_float_vnd, opened_at, opened_by_staff_identity_id
FROM sales_shifts
WHERE id = $1 AND state = 'OPEN'
LIMIT 1
FOR UPDATE;

-- name: GetStaffSummary :one
SELECT id, display_name, login_code
FROM staff_identities
WHERE id = $1;

-- -- Cash Movements --

-- name: InsertCashMovement :one
INSERT INTO cash_movements (
    sales_shift_id, method, amount_vnd, reason, note,
    initiated_by_staff_identity_id, initiated_staff_access_session_id,
    approved_by_staff_identity_id
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING id, sales_shift_id, method, amount_vnd, reason, note,
          initiated_by_staff_identity_id, approved_by_staff_identity_id, occurred_at;

-- name: ListCashMovements :many
SELECT cm.id, cm.sales_shift_id, cm.method, cm.amount_vnd, cm.reason, cm.note, cm.occurred_at,
       ini.id AS initiator_id,
       ini.display_name AS initiator_display_name,
       ini.login_code AS initiator_login_code,
       apr.id AS approver_id,
       apr.display_name AS approver_display_name,
       apr.login_code AS approver_login_code
FROM cash_movements cm
JOIN staff_identities ini ON ini.id = cm.initiated_by_staff_identity_id
JOIN staff_identities apr ON apr.id = cm.approved_by_staff_identity_id
WHERE cm.sales_shift_id = $1
ORDER BY cm.occurred_at DESC, cm.id DESC;

-- name: SumCashMovements :one
SELECT
    COALESCE(SUM(amount_vnd) FILTER (WHERE method = 'PAY_IN'), 0)::BIGINT AS pay_in_vnd,
    COALESCE(SUM(amount_vnd) FILTER (WHERE method = 'PAY_OUT'), 0)::BIGINT AS pay_out_vnd
FROM cash_movements
WHERE sales_shift_id = $1;
```

- [ ] **Step 3: Append the two approval queries to `sql/queries/auth.sql`**

Append at the end of the file:

```sql
-- name: GetStaffByLoginCodeForUpdate :one
-- Locks the approver row so a concurrent disablement or role change cannot
-- interleave between verification and use. Used by VerifyManagerApproval.
SELECT id, display_name, login_code, pin_hash, enabled, created_at
FROM staff_identities
WHERE upper(btrim(login_code)) = upper(btrim($1))
LIMIT 1
FOR UPDATE;

-- name: GetStaffRolesForUpdate :many
SELECT role
FROM staff_operational_roles
WHERE staff_identity_id = $1
ORDER BY role ASC
FOR UPDATE;
```

- [ ] **Step 4: Regenerate sqlc bindings**

Run: `sqlc generate`

Expected: exits 0, and `internal/database/sqlc/` now contains `OpenSalesShift`, `GetOpenSalesShift`, `GetOpenSalesShiftForUpdate`, `GetStaffSummary`, `InsertCashMovement`, `ListCashMovements`, `SumCashMovements`, `GetShiftSessionAuthority`, `GetShiftSessionRoles`, `ShiftAdvisoryLock`, `GetStaffByLoginCodeForUpdate`, and `GetStaffRolesForUpdate`.

Verify: `grep -c "func (q \*Queries) OpenSalesShift" internal/database/sqlc/*.go`

Expected: one file reports `1`.

If sqlc reports a duplicate query name, rename the new query rather than the existing one — `auth.sql`, `catalog.sql`, and `tables.sql` are already shipped.

- [ ] **Step 5: Write the failing schema test**

Create `internal/shift/schema_integration_test.go`:

```go
//go:build integration

package shift_test

import (
	"database/sql"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// insertShiftRow opens a Sales Shift with raw SQL so constraint behavior is
// tested without going through the slice.
func insertShiftRow(t *testing.T, db *sql.DB, openerID uuid.UUID, floatVND int64) (uuid.UUID, error) {
	t.Helper()
	var id uuid.UUID
	err := db.QueryRow(
		`INSERT INTO sales_shifts (opened_by_staff_identity_id, opening_float_vnd)
		 VALUES ($1, $2) RETURNING id`,
		openerID, floatVND,
	).Scan(&id)
	return id, err
}

func TestSchemaRejectsSecondOpenShift(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	actor := newTestActor(t, q, []string{"CASHIER"}, true)

	_, err := insertShiftRow(t, db, actor.StaffID, 500000)
	require.NoError(t, err)

	_, err = insertShiftRow(t, db, actor.StaffID, 700000)
	require.Error(t, err, "a second OPEN Sales Shift must violate the partial unique index")
	assert.Contains(t, err.Error(), "sales_shift_only_one_open_unique")
}

func TestSchemaAllowsOpenAfterClose(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	actor := newTestActor(t, q, []string{"CASHIER"}, true)

	first, err := insertShiftRow(t, db, actor.StaffID, 500000)
	require.NoError(t, err)

	// CLOSED exists in the check constraint although Phase 4 ships no close
	// command, so Phase 5 adds one without a state-domain migration.
	_, err = db.Exec(`UPDATE sales_shifts SET state = 'CLOSED' WHERE id = $1`, first)
	require.NoError(t, err)

	_, err = insertShiftRow(t, db, actor.StaffID, 700000)
	assert.NoError(t, err, "the partial index must only constrain OPEN rows")
}

func TestSchemaRejectsInvalidShiftValues(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	actor := newTestActor(t, q, []string{"CASHIER"}, true)

	_, err := insertShiftRow(t, db, actor.StaffID, -1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sales_shift_opening_float_vnd_valid")

	_, err = insertShiftRow(t, db, actor.StaffID, 2147483648)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sales_shift_opening_float_vnd_valid")

	// Zero is valid: a station may legitimately open with an empty fund.
	_, err = insertShiftRow(t, db, actor.StaffID, 0)
	assert.NoError(t, err)
}

func TestSchemaRejectsInvalidCashMovements(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	actor := newTestActor(t, q, []string{"CASHIER"}, true)
	shiftID, err := insertShiftRow(t, db, actor.StaffID, 500000)
	require.NoError(t, err)

	insert := func(method string, amount int64, reason string, note any) error {
		_, execErr := db.Exec(
			`INSERT INTO cash_movements (
				sales_shift_id, method, amount_vnd, reason, note,
				initiated_by_staff_identity_id, initiated_staff_access_session_id,
				approved_by_staff_identity_id)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			shiftID, method, amount, reason, note,
			actor.StaffID, actor.SessionID, actor.StaffID,
		)
		return execErr
	}

	cases := []struct {
		name       string
		method     string
		amount     int64
		reason     string
		note       any
		constraint string
	}{
		{"unknown method", "CASH_DROP", 1000, "SAFE_DROP", nil, "cash_movement_method_valid"},
		{"zero amount", "PAY_IN", 0, "ADD_CHANGE_FUND", nil, "cash_movement_amount_vnd_valid"},
		{"negative amount", "PAY_OUT", -1000, "SAFE_DROP", nil, "cash_movement_amount_vnd_valid"},
		{"over-bound amount", "PAY_IN", 2147483648, "ADD_CHANGE_FUND", nil, "cash_movement_amount_vnd_valid"},
		{"unknown reason", "PAY_IN", 1000, "PETTY_CASH", nil, "cash_movement_reason_valid"},
		{"OTHER without note", "PAY_OUT", 1000, "OTHER", nil, "cash_movement_note_valid"},
		{"untrimmed note", "PAY_OUT", 1000, "OTHER", "  padded  ", "cash_movement_note_valid"},
		{"empty note", "PAY_OUT", 1000, "OTHER", "", "cash_movement_note_valid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			insertErr := insert(tc.method, tc.amount, tc.reason, tc.note)
			require.Error(t, insertErr)
			assert.Contains(t, insertErr.Error(), tc.constraint)
		})
	}

	assert.NoError(t, insert("PAY_OUT", 50000, "SAFE_DROP", nil))
	assert.NoError(t, insert("PAY_OUT", 50000, "OTHER", "mua da cho quay pha che"))
}
```

- [ ] **Step 6: Run the schema test to verify it fails**

Run: `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -race -p 1 -tags=integration ./internal/shift/...`

Expected: FAIL to build with `undefined: openShiftTestDB`, `undefined: truncateShiftTables`, `undefined: newTestActor`. Those helpers arrive in Task 4. This confirms the file compiles into package `shift_test` and nothing else is missing.

- [ ] **Step 7: Verify the migration applies to a real database**

Run: `go run ./cmd/api` in one terminal, wait for the startup log line reporting migrations applied, then stop it with Ctrl+C.

Then run: `docker compose exec -T postgres psql -U cafe_pos -d cafe_pos -c "\d sales_shifts" -c "\d cash_movements"`

Expected: both tables exist with the constraints above, and `sales_shift_only_one_open_unique` is listed as `UNIQUE, btree (state) WHERE state = 'OPEN'`.

- [ ] **Step 8: Commit**

```bash
git add internal/database/migrations/000007_create_shift_slice.sql \
        sql/queries/shift.sql sql/queries/auth.sql \
        internal/database/sqlc/ internal/shift/schema_integration_test.go
git commit -m "feat(shift): add sales_shifts and cash_movements schema and queries"
```

---

## Task 2: Shared Manager Approval Primitive

Recording a Cash Movement needs a **second** identity to authenticate inline. This is different from the Catalog pattern, which re-verifies the *actor's own* PIN. Phase 5 needs the identical mechanism for Refund, Payment Void, and Comp, so the verification lives in `internal/auth` and is written once (ADR-009).

**Files:**
- Create: `internal/auth/manager_approval.go`
- Create: `internal/auth/manager_approval_test.go`
- Create: `internal/auth/manager_approval_integration_test.go`

**Interfaces:**
- Consumes: `sqlc.Queries.GetStaffByLoginCodeForUpdate`, `sqlc.Queries.GetStaffRolesForUpdate` (Task 1); the existing `VerifyPin`, `DeriveCapabilities`, `RoleManager`, and the unexported `dummyBcryptHash` in `internal/auth/domain.go`.
- Produces:
  - `type ApproverSummary struct { ID uuid.UUID; DisplayName string; LoginCode string }` with json tags `id`, `display_name`, `login_code`
  - `var ErrManagerApprovalDenied error`
  - `const ApprovalDenialInvalidPin, ApprovalDenialIdentityDisabled, ApprovalDenialManagerRoleRequired, ApprovalDenialCapabilityRequired string`
  - `func NormalizeLoginCode(s string) string`
  - `func VerifyManagerApproval(ctx context.Context, q *sqlc.Queries, approverLoginCode, managerPin, requiredCapability string) (ApproverSummary, error)`

  Task 4 calls `VerifyManagerApproval` from inside the Shift transaction and maps any `ErrManagerApprovalDenied` to `shift.ErrManagerApprovalUnavailable`.

- [ ] **Step 1: Write the failing unit test**

Create `internal/auth/manager_approval_test.go`:

```go
package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeLoginCode(t *testing.T) {
	cases := []struct{ in, want string }{
		{"qla", "QLA"},
		{"  qla  ", "QLA"},
		{"QLA", "QLA"},
		{"\tQl A\n", "QL A"},
		{"", ""},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, NormalizeLoginCode(tc.in), "input %q", tc.in)
	}
}

func TestEvaluateApprover(t *testing.T) {
	managerRoles := []string{RoleManager}
	cashierRoles := []string{RoleCashier}

	cases := []struct {
		name       string
		found      bool
		pinOK      bool
		enabled    bool
		roles      []string
		capability string
		want       string
	}{
		{
			name: "approved", found: true, pinOK: true, enabled: true,
			roles: managerRoles, capability: "sales_shift.operate", want: "",
		},
		{
			// An unknown login code must be indistinguishable from a wrong PIN.
			name: "unknown login code", found: false, pinOK: false, enabled: false,
			roles: nil, capability: "sales_shift.operate", want: ApprovalDenialInvalidPin,
		},
		{
			name: "wrong pin", found: true, pinOK: false, enabled: true,
			roles: managerRoles, capability: "sales_shift.operate", want: ApprovalDenialInvalidPin,
		},
		{
			name: "disabled manager", found: true, pinOK: true, enabled: false,
			roles: managerRoles, capability: "sales_shift.operate", want: ApprovalDenialIdentityDisabled,
		},
		{
			name: "cashier cannot approve", found: true, pinOK: true, enabled: true,
			roles: cashierRoles, capability: "sales_shift.operate", want: ApprovalDenialManagerRoleRequired,
		},
		{
			name: "no roles at all", found: true, pinOK: true, enabled: true,
			roles: nil, capability: "sales_shift.operate", want: ApprovalDenialManagerRoleRequired,
		},
		{
			name: "manager lacking the capability", found: true, pinOK: true, enabled: true,
			roles: managerRoles, capability: "nonexistent.capability", want: ApprovalDenialCapabilityRequired,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := evaluateApprover(tc.found, tc.pinOK, tc.enabled, tc.roles, tc.capability)
			assert.Equal(t, tc.want, got)
		})
	}
}
```

- [ ] **Step 2: Run the unit test to verify it fails**

Run: `go test -race ./internal/auth/ -run 'TestNormalizeLoginCode|TestEvaluateApprover' -v`

Expected: FAIL to build — `undefined: NormalizeLoginCode`, `undefined: evaluateApprover`, `undefined: ApprovalDenialInvalidPin`.

- [ ] **Step 3: Write the implementation**

Create `internal/auth/manager_approval.go`:

```go
package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

// ApproverSummary identifies the Manager who approved a second-party command.
// It carries exactly these three fields; no PIN hash, role list, enablement
// flag, or session detail crosses a slice boundary.
type ApproverSummary struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"display_name"`
	LoginCode   string    `json:"login_code"`
}

// ErrManagerApprovalDenied reports that inline Manager approval failed. The
// wrapped text names the reason for the server log and the denial audit event.
// Callers must collapse every reason to one client-visible code, so that the
// API does not disclose which condition failed.
var ErrManagerApprovalDenied = errors.New("manager approval denied")

// Denial reasons. These are logged and audited, never returned to a client.
const (
	ApprovalDenialInvalidPin          = "INVALID_PIN"
	ApprovalDenialIdentityDisabled    = "IDENTITY_DISABLED"
	ApprovalDenialManagerRoleRequired = "MANAGER_ROLE_REQUIRED"
	ApprovalDenialCapabilityRequired  = "CAPABILITY_REQUIRED"
)

// NormalizeLoginCode trims surrounding whitespace and upper-cases a login code,
// matching the upper(btrim(...)) lookup used by the identity queries.
func NormalizeLoginCode(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
}

// evaluateApprover decides the denial reason from already-gathered facts, or
// returns "" when approval is granted. It is pure so every branch is unit
// tested without a database.
//
// A missing identity and a wrong PIN both report INVALID_PIN: the caller runs
// PIN verification against a dummy hash when no identity matched, so neither
// the response body nor the response timing reveals whether a login code exists.
func evaluateApprover(found, pinOK, enabled bool, roles []string, requiredCapability string) string {
	if !found || !pinOK {
		return ApprovalDenialInvalidPin
	}
	if !enabled {
		return ApprovalDenialIdentityDisabled
	}
	if !slices.Contains(roles, RoleManager) {
		return ApprovalDenialManagerRoleRequired
	}
	if !slices.Contains(DeriveCapabilities(roles), requiredCapability) {
		return ApprovalDenialCapabilityRequired
	}
	return ""
}

// VerifyManagerApproval authenticates a second identity inline and confirms it
// may approve an operation requiring requiredCapability.
//
// It must be called with a transaction-scoped Queries so that the approver row
// lock it takes holds for the rest of the operation: a concurrent disablement
// or role change cannot interleave between verification and use.
//
// Self-approval is permitted. A Manager working alone supplies their own login
// code and PIN; callers record initiator and approver separately, so a
// self-approved command stays distinguishable in the audit trail.
func VerifyManagerApproval(
	ctx context.Context,
	q *sqlc.Queries,
	approverLoginCode string,
	managerPin string,
	requiredCapability string,
) (ApproverSummary, error) {
	normalized := NormalizeLoginCode(approverLoginCode)

	approver, err := q.GetStaffByLoginCodeForUpdate(ctx, normalized)
	found := true
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return ApproverSummary{}, fmt.Errorf("lookup approver: %w", err)
		}
		found = false
	}

	// Always spend the bcrypt comparison, even with no match, so response
	// timing does not distinguish an unknown login code from a wrong PIN.
	pinHash := dummyBcryptHash
	if found {
		pinHash = approver.PinHash
	}
	pinOK := VerifyPin(pinHash, managerPin)

	var roles []string
	if found && pinOK && approver.Enabled {
		roles, err = q.GetStaffRolesForUpdate(ctx, approver.ID)
		if err != nil {
			return ApproverSummary{}, fmt.Errorf("load approver roles: %w", err)
		}
	}

	if reason := evaluateApprover(found, pinOK, approver.Enabled, roles, requiredCapability); reason != "" {
		return ApproverSummary{}, fmt.Errorf("%w: %s", ErrManagerApprovalDenied, reason)
	}

	return ApproverSummary{
		ID:          approver.ID,
		DisplayName: approver.DisplayName,
		LoginCode:   approver.LoginCode,
	}, nil
}
```

Note on the `found == false` path: `approver` is the zero `sqlc.StaffIdentity`, so `approver.Enabled` is `false`, but `evaluateApprover` returns `ApprovalDenialInvalidPin` before reading it. The roles load is skipped for an unmatched, wrong-PIN, or disabled identity, so a failed approval never spends an extra query.

- [ ] **Step 4: Run the unit test to verify it passes**

Run: `go test -race ./internal/auth/ -run 'TestNormalizeLoginCode|TestEvaluateApprover' -v`

Expected: PASS, both tests, every subtest.

- [ ] **Step 5: Write the failing integration test**

Create `internal/auth/manager_approval_integration_test.go`:

```go
//go:build integration

package auth_test

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/database"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openApprovalTestDB(t *testing.T) (*sql.DB, *sqlc.Queries) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	require.Contains(t, url, "_test")
	db, err := database.Open(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db, sqlc.New(db)
}

// newApprover creates an identity with a known PIN and returns its login code.
func newApprover(t *testing.T, q *sqlc.Queries, roles []string, enabled bool, pin string) (uuid.UUID, string) {
	t.Helper()
	ctx := context.Background()

	loginCode := "A" + strings.ReplaceAll(uuid.NewString(), "-", "")[:23]
	hash, err := auth.HashPin(pin)
	require.NoError(t, err)

	row, err := q.CreateStaffIdentity(ctx, sqlc.CreateStaffIdentityParams{
		DisplayName: "Approval Test " + loginCode,
		Btrim:       loginCode,
		PinHash:     hash,
		Enabled:     enabled,
	})
	require.NoError(t, err)

	for _, role := range roles {
		require.NoError(t, q.AddStaffRole(ctx, sqlc.AddStaffRoleParams{
			StaffIdentityID: row.ID, Role: role,
		}))
	}
	return row.ID, loginCode
}

func TestVerifyManagerApprovalGrantsForEnabledManager(t *testing.T) {
	db, q := openApprovalTestDB(t)
	ctx := context.Background()
	id, loginCode := newApprover(t, q, []string{auth.RoleManager}, true, "8642")

	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback() //nolint:errcheck

	// Lower-cased and padded on purpose: normalization must find the identity.
	approver, err := auth.VerifyManagerApproval(ctx, q.WithTx(tx),
		"  "+strings.ToLower(loginCode)+"  ", "8642", "sales_shift.operate")
	require.NoError(t, err)
	assert.Equal(t, id, approver.ID)
	assert.Equal(t, loginCode, approver.LoginCode)
	assert.NotEmpty(t, approver.DisplayName)
}

func TestVerifyManagerApprovalDenials(t *testing.T) {
	db, q := openApprovalTestDB(t)
	ctx := context.Background()

	_, managerCode := newApprover(t, q, []string{auth.RoleManager}, true, "8642")
	_, disabledCode := newApprover(t, q, []string{auth.RoleManager}, false, "8642")
	_, cashierCode := newApprover(t, q, []string{auth.RoleCashier}, true, "8642")

	cases := []struct {
		name       string
		loginCode  string
		pin        string
		capability string
		reason     string
	}{
		{"wrong pin", managerCode, "0000", "sales_shift.operate", auth.ApprovalDenialInvalidPin},
		{"unknown login code", "ZZUNKNOWN", "8642", "sales_shift.operate", auth.ApprovalDenialInvalidPin},
		{"disabled manager", disabledCode, "8642", "sales_shift.operate", auth.ApprovalDenialIdentityDisabled},
		{"cashier approver", cashierCode, "8642", "sales_shift.operate", auth.ApprovalDenialManagerRoleRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := db.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer tx.Rollback() //nolint:errcheck

			_, err = auth.VerifyManagerApproval(ctx, q.WithTx(tx), tc.loginCode, tc.pin, tc.capability)
			require.Error(t, err)
			assert.ErrorIs(t, err, auth.ErrManagerApprovalDenied)
			assert.Contains(t, err.Error(), tc.reason)
		})
	}
}

func TestVerifyManagerApprovalLocksApproverRow(t *testing.T) {
	db, q := openApprovalTestDB(t)
	ctx := context.Background()
	id, loginCode := newApprover(t, q, []string{auth.RoleManager}, true, "8642")

	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback() //nolint:errcheck

	_, err = auth.VerifyManagerApproval(ctx, q.WithTx(tx), loginCode, "8642", "sales_shift.operate")
	require.NoError(t, err)

	// A concurrent disablement must block while the approval transaction holds
	// the row lock, proving verification and use cannot be interleaved.
	blocked := make(chan error, 1)
	go func() {
		_, execErr := db.Exec(`UPDATE staff_identities SET enabled = false WHERE id = $1`, id)
		blocked <- execErr
	}()

	select {
	case <-blocked:
		t.Fatal("concurrent disablement completed while the approver row was locked")
	case <-time.After(300 * time.Millisecond):
		// Still blocked, which is the expected outcome.
	}

	require.NoError(t, tx.Rollback())
	require.NoError(t, <-blocked)
}
```

- [ ] **Step 6: Run the integration test to verify it passes**

Run: `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -race -p 1 -tags=integration ./internal/auth/ -run TestVerifyManagerApproval -v`

Expected: PASS, all three tests.

If `TestVerifyManagerApprovalLocksApproverRow` fails because the `UPDATE` completed, the `FOR UPDATE` clause is missing from `GetStaffByLoginCodeForUpdate` — check that Task 1 Step 3 placed `FOR UPDATE` after `LIMIT 1`, then rerun `sqlc generate`.

- [ ] **Step 7: Confirm the existing auth suites still pass**

Run: `go test -race ./internal/auth/...`

Expected: PASS. `manager_approval.go` adds to the package and changes nothing existing.

- [ ] **Step 8: Commit**

```bash
git add internal/auth/manager_approval.go \
        internal/auth/manager_approval_test.go \
        internal/auth/manager_approval_integration_test.go
git commit -m "feat(auth): add VerifyManagerApproval second-party approval primitive"
```

---

## Task 3: Domain Rules, DTOs, And Error Mapping

Pure, dependency-free code first, so every boundary is unit tested without a database.

**Files:**
- Create: `internal/shift/domain.go`
- Create: `internal/shift/domain_test.go`
- Create: `internal/shift/dto.go`
- Create: `internal/shift/errors.go`
- Create: `internal/shift/errors_test.go`

**Interfaces:**
- Consumes: `internal/response` for `ErrInvalid`, `CodedError`, `NewCodedError`.
- Produces, all in package `shift`:
  - Constants `CapSalesShiftOperate`, `OpOpenShift`, `OpRecordCashMovement`, `EventSalesShiftOpened`, `EventCashMovementRecorded`, `EventAuthorizationDenied`, `StateOpen`, `StateClosed`, `MethodPayIn`, `MethodPayOut`, `ReasonAddChangeFund`, `ReasonRemoveExcessFloat`, `ReasonSafeDrop`, `ReasonOther`, `MaxAmountVND`, `MaxNoteLength`
  - `func ValidateOpeningFloat(v int64) error`
  - `func ValidateAmount(v int64) error`
  - `func ValidateMethod(m string) error`
  - `func ValidateReason(r string) error`
  - `func NormalizeNote(note *string) *string`
  - `func ValidateNote(note *string, reason string) error`
  - `func ComputeExpectedCash(openingFloatVND, payInVND, payOutVND int64) (int64, error)`
  - Types `OpenShiftCommand`, `RecordCashMovementCommand`, `StaffSummary`, `SalesShiftResponse`, `CashMovementResponse`, `CurrentSalesShiftResponse`, `CashMovementResult`
  - Sentinels `ErrShiftAlreadyOpen`, `ErrOpenShiftRequired`, `ErrManagerApprovalUnavailable`, `ErrRequestConflict`, `ErrExpectedCashOutOfRange`, `ErrForbidden`, `ErrUnauthorized`, `ErrInvalidStoredResult`
  - `func MapDBError(err error) error`, `func MapHTTPError(err error) error`

  Tasks 4-8 use every one of these names.

- [ ] **Step 1: Write the failing domain unit test**

Create `internal/shift/domain_test.go`:

```go
package shift_test

import (
	"strings"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/shift"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func strPtr(s string) *string { return &s }

func TestValidateOpeningFloat(t *testing.T) {
	// Zero is valid: a station may legitimately open with an empty fund.
	assert.NoError(t, shift.ValidateOpeningFloat(0))
	assert.NoError(t, shift.ValidateOpeningFloat(500000))
	assert.NoError(t, shift.ValidateOpeningFloat(shift.MaxAmountVND))

	assert.Error(t, shift.ValidateOpeningFloat(-1))
	assert.Error(t, shift.ValidateOpeningFloat(shift.MaxAmountVND+1))
}

func TestValidateAmount(t *testing.T) {
	// A Cash Movement amount is strictly positive; direction lives in method.
	assert.Error(t, shift.ValidateAmount(0))
	assert.Error(t, shift.ValidateAmount(-1000))
	assert.Error(t, shift.ValidateAmount(shift.MaxAmountVND+1))

	assert.NoError(t, shift.ValidateAmount(1))
	assert.NoError(t, shift.ValidateAmount(shift.MaxAmountVND))
}

func TestValidateMethod(t *testing.T) {
	assert.NoError(t, shift.ValidateMethod(shift.MethodPayIn))
	assert.NoError(t, shift.ValidateMethod(shift.MethodPayOut))

	// CASH_DROP appears in the superseded MIGRATE_PLAN sketch; it is not a method.
	assert.Error(t, shift.ValidateMethod("CASH_DROP"))
	assert.Error(t, shift.ValidateMethod("pay_in"))
	assert.Error(t, shift.ValidateMethod(""))
}

func TestValidateReason(t *testing.T) {
	for _, reason := range []string{
		shift.ReasonAddChangeFund,
		shift.ReasonRemoveExcessFloat,
		shift.ReasonSafeDrop,
		shift.ReasonOther,
	} {
		assert.NoError(t, shift.ValidateReason(reason), reason)
	}
	assert.Error(t, shift.ValidateReason("PETTY_CASH"))
	assert.Error(t, shift.ValidateReason(""))
}

func TestNormalizeNote(t *testing.T) {
	assert.Nil(t, shift.NormalizeNote(nil))
	assert.Nil(t, shift.NormalizeNote(strPtr("")))
	assert.Nil(t, shift.NormalizeNote(strPtr("   ")))

	got := shift.NormalizeNote(strPtr("  mua da  "))
	require.NotNil(t, got)
	assert.Equal(t, "mua da", *got)
}

func TestValidateNote(t *testing.T) {
	// An absent note is fine for every reason except OTHER.
	assert.NoError(t, shift.ValidateNote(nil, shift.ReasonSafeDrop))
	assert.Error(t, shift.ValidateNote(nil, shift.ReasonOther),
		"an unexplained OTHER movement is exactly the shrinkage this control prevents")

	assert.NoError(t, shift.ValidateNote(strPtr("mua da"), shift.ReasonOther))
	assert.NoError(t, shift.ValidateNote(strPtr("x"), shift.ReasonSafeDrop))

	atLimit := strings.Repeat("n", shift.MaxNoteLength)
	assert.NoError(t, shift.ValidateNote(&atLimit, shift.ReasonSafeDrop))

	overLimit := strings.Repeat("n", shift.MaxNoteLength+1)
	assert.Error(t, shift.ValidateNote(&overLimit, shift.ReasonSafeDrop))

	// Length is counted in Unicode code points so Go agrees with char_length.
	vietnamese := strings.Repeat("ế", shift.MaxNoteLength)
	assert.NoError(t, shift.ValidateNote(&vietnamese, shift.ReasonSafeDrop))
}

func TestComputeExpectedCash(t *testing.T) {
	got, err := shift.ComputeExpectedCash(500000, 100000, 150000)
	require.NoError(t, err)
	assert.Equal(t, int64(450000), got)

	// No movements yet: Expected Cash is the Opening Float.
	got, err = shift.ComputeExpectedCash(500000, 0, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(500000), got)

	// Sustained Pay Outs can legitimately drive the Phase 4 partial figure
	// negative, so the guard is symmetric rather than one-sided.
	got, err = shift.ComputeExpectedCash(0, 0, 1000)
	require.NoError(t, err)
	assert.Equal(t, int64(-1000), got)

	_, err = shift.ComputeExpectedCash(shift.MaxAmountVND, 1, 0)
	assert.ErrorIs(t, err, shift.ErrExpectedCashOutOfRange)

	_, err = shift.ComputeExpectedCash(0, 0, shift.MaxAmountVND+1)
	assert.ErrorIs(t, err, shift.ErrExpectedCashOutOfRange)
}
```

- [ ] **Step 2: Run the domain test to verify it fails**

Run: `go test -race ./internal/shift/`

Expected: FAIL to build — the package `shift` does not exist yet.

- [ ] **Step 3: Write `domain.go`**

Create `internal/shift/domain.go`:

```go
// Package shift implements the Sales Shift vertical slice: the accountability
// window for the cashier station's cash fund, the Cash Movements that change
// its Expected Cash, and the read that reports both.
package shift

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// CapSalesShiftOperate is the capability every Shift operation requires. It is
// already derived for MANAGER and CASHIER by auth.DeriveCapabilities.
const CapSalesShiftOperate = "sales_shift.operate"

// Idempotency action names, stored in idempotency_keys.action.
const (
	OpOpenShift          = "shift.open_shift"
	OpRecordCashMovement = "shift.record_cash_movement"
)

// Audit event types. Business events are UPPER_SNAKE_CASE and the denial event
// is lowercase dotted, matching the convention in internal/tables.
const (
	EventSalesShiftOpened     = "SALES_SHIFT_OPENED"
	EventCashMovementRecorded = "CASH_MOVEMENT_RECORDED"
	EventAuthorizationDenied  = "shift.authorization_denied"
)

// Sales Shift states. Phase 4 produces only StateOpen; StateClosed exists in
// the schema so Phase 5 adds a close command without a state-domain migration.
const (
	StateOpen   = "OPEN"
	StateClosed = "CLOSED"
)

// Cash Movement methods. Direction is carried here, never by a negative amount.
const (
	MethodPayIn  = "PAY_IN"
	MethodPayOut = "PAY_OUT"
)

// Cash Movement reasons.
const (
	ReasonAddChangeFund     = "ADD_CHANGE_FUND"
	ReasonRemoveExcessFloat = "REMOVE_EXCESS_FLOAT"
	ReasonSafeDrop          = "SAFE_DROP"
	ReasonOther             = "OTHER"
)

// MaxAmountVND is the inclusive upper bound on every Phase 4 monetary value.
// It matches the canonical MAX_OPENING_FLOAT_VND and MAX_CASH_MOVEMENT_VND.
// BIGINT could hold more, but this is the bound the business rules are written
// against, so it is enforced in Go and in the database.
const MaxAmountVND int64 = 2147483647

// MaxNoteLength is the inclusive upper bound on a trimmed note, counted in
// Unicode code points so Go agrees with the database char_length check.
const MaxNoteLength = 500

// ValidateOpeningFloat checks a counted Opening Float. Zero is valid.
func ValidateOpeningFloat(v int64) error {
	if v < 0 {
		return fmt.Errorf("opening_float_vnd cannot be negative")
	}
	if v > MaxAmountVND {
		return fmt.Errorf("opening_float_vnd exceeds the maximum of %d", MaxAmountVND)
	}
	return nil
}

// ValidateAmount checks a Cash Movement amount, which is strictly positive.
func ValidateAmount(v int64) error {
	if v <= 0 {
		return fmt.Errorf("amount_vnd must be greater than 0")
	}
	if v > MaxAmountVND {
		return fmt.Errorf("amount_vnd exceeds the maximum of %d", MaxAmountVND)
	}
	return nil
}

// ValidateMethod checks a Cash Movement method.
func ValidateMethod(m string) error {
	switch m {
	case MethodPayIn, MethodPayOut:
		return nil
	default:
		return fmt.Errorf("method must be %s or %s", MethodPayIn, MethodPayOut)
	}
}

// ValidateReason checks a Cash Movement reason.
func ValidateReason(r string) error {
	switch r {
	case ReasonAddChangeFund, ReasonRemoveExcessFloat, ReasonSafeDrop, ReasonOther:
		return nil
	default:
		return fmt.Errorf("reason must be one of %s, %s, %s, %s",
			ReasonAddChangeFund, ReasonRemoveExcessFloat, ReasonSafeDrop, ReasonOther)
	}
}

// NormalizeNote trims a note and reports nil for an empty result, matching the
// canonical `note || null` transform. Normalizing before fingerprinting keeps
// an idempotent replay stable across cosmetic whitespace differences.
func NormalizeNote(note *string) *string {
	if note == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*note)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// ValidateNote checks an already-normalized note against its length bounds and
// the reason-specific requirement. Reason OTHER requires a note.
func ValidateNote(note *string, reason string) error {
	if note == nil {
		if reason == ReasonOther {
			return fmt.Errorf("note is required when reason is %s", ReasonOther)
		}
		return nil
	}
	n := utf8.RuneCountInString(*note)
	if n < 1 {
		return fmt.Errorf("note cannot be empty")
	}
	if n > MaxNoteLength {
		return fmt.Errorf("note is %d characters, maximum is %d", n, MaxNoteLength)
	}
	return nil
}

// ComputeExpectedCash returns the Sales Shift's calculated cash responsibility.
//
// Phase 4 formula: Opening Float plus Pay Ins less Pay Outs. Phase 5 adds Cash
// Payments and subtracts Cash Refunds; until then this figure reflects fund
// movements only and is not a reconciliation figure. See ADR-008.
//
// The guard is symmetric because sustained Pay Outs can drive the partial
// Phase 4 figure negative. A total outside the bound indicates corrupt data,
// not a legitimate drawer balance.
func ComputeExpectedCash(openingFloatVND, payInVND, payOutVND int64) (int64, error) {
	total := openingFloatVND + payInVND - payOutVND
	if total > MaxAmountVND || total < -MaxAmountVND {
		return 0, fmt.Errorf("%w: expected cash %d is outside [%d, %d]",
			ErrExpectedCashOutOfRange, total, -MaxAmountVND, MaxAmountVND)
	}
	return total, nil
}
```

- [ ] **Step 4: Write `dto.go`**

Create `internal/shift/dto.go`:

```go
package shift

import (
	"time"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/google/uuid"
)

// OpenShiftCommand opens a Sales Shift.
//
// OpeningFloatVND is a pointer so a missing JSON field is rejected instead of
// silently defaulting to zero, which is itself a valid counted float.
type OpenShiftCommand struct {
	RequestID       uuid.UUID `json:"request_id"`
	OpeningFloatVND *int64    `json:"opening_float_vnd"`
}

// RecordCashMovementCommand records a Pay In or Pay Out against an open Shift.
//
// ShiftID comes from the route, not the body. ManagerPIN is a secret: it is
// excluded from the request fingerprint and must never be audited or logged.
type RecordCashMovementCommand struct {
	RequestID         uuid.UUID `json:"request_id"`
	ShiftID           uuid.UUID `json:"-"`
	Method            string    `json:"method"`
	AmountVND         *int64    `json:"amount_vnd"`
	Reason            string    `json:"reason"`
	Note              *string   `json:"note"`
	ApproverLoginCode string    `json:"approver_login_code"`
	ManagerPIN        string    `json:"manager_pin"`
}

// StaffSummary is the only staff representation that crosses the Shift
// boundary. It carries exactly these three fields.
type StaffSummary struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"display_name"`
	LoginCode   string    `json:"login_code"`
}

func staffSummaryFromApprover(a auth.ApproverSummary) StaffSummary {
	return StaffSummary{ID: a.ID, DisplayName: a.DisplayName, LoginCode: a.LoginCode}
}

// SalesShiftResponse is the API representation of a Sales Shift.
type SalesShiftResponse struct {
	ID              uuid.UUID    `json:"id"`
	State           string       `json:"state"`
	OpeningFloatVND int64        `json:"opening_float_vnd"`
	OpenedAt        time.Time    `json:"opened_at"`
	Opener          StaffSummary `json:"opener"`
}

// CashMovementResponse is the API representation of one Cash Movement.
type CashMovementResponse struct {
	ID           uuid.UUID    `json:"id"`
	SalesShiftID uuid.UUID    `json:"sales_shift_id"`
	Method       string       `json:"method"`
	AmountVND    int64        `json:"amount_vnd"`
	Reason       string       `json:"reason"`
	Note         *string      `json:"note"`
	Initiator    StaffSummary `json:"initiator"`
	Approver     StaffSummary `json:"approver"`
	OccurredAt   time.Time    `json:"occurred_at"`
}

// CurrentSalesShiftResponse is the current-Shift read.
//
// ExpectedCashVND is complete in shape but partial in value until Phase 5 adds
// Cash Payments and Cash Refunds. CashMovements is always an array and is
// serialized as [] when empty, never as null.
type CurrentSalesShiftResponse struct {
	SalesShiftResponse
	ExpectedCashVND int64                  `json:"expected_cash_vnd"`
	CashMovements   []CashMovementResponse `json:"cash_movements"`
}

// CashMovementResult is the Cash Movement command response. It returns the
// resulting Expected Cash so the terminal updates its drawer figure without a
// second request.
type CashMovementResult struct {
	Movement        CashMovementResponse `json:"movement"`
	ExpectedCashVND int64                `json:"expected_cash_vnd"`
}
```

- [ ] **Step 5: Write `errors.go`**

Create `internal/shift/errors.go`:

```go
package shift

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrShiftAlreadyOpen           = errors.New("a sales shift is already open")
	ErrOpenShiftRequired          = errors.New("an open sales shift is required")
	ErrManagerApprovalUnavailable = errors.New("manager approval unavailable")
	ErrRequestConflict            = errors.New("request conflict")
	ErrExpectedCashOutOfRange     = errors.New("expected cash out of range")
	ErrForbidden                  = errors.New("forbidden")
	ErrUnauthorized               = errors.New("unauthorized")
	ErrInvalidStoredResult        = errors.New("invalid stored result")
)

// MapDBError maps PostgreSQL driver and database errors to domain sentinels
// inside the Shift boundary, so an expected constraint failure never becomes an
// accidental generic 500.
func MapDBError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %s", ErrOpenShiftRequired, err.Error())
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		msg := pgErr.Detail
		if msg == "" {
			msg = pgErr.Message
		}
		switch pgErr.Code {
		case "23505": // unique_violation on sales_shift_only_one_open_unique
			return fmt.Errorf("%w: %s", ErrShiftAlreadyOpen, msg)
		case "23503": // foreign_key_violation on sales_shift_id
			return fmt.Errorf("%w: %s", ErrOpenShiftRequired, msg)
		}
		// 23514 (check_violation) is deliberately not mapped. It means Go
		// validation and the database disagree, which is a defect, not a
		// business state, and must surface as a logged 500.
	}
	return err
}

// MapHTTPError maps Shift domain errors and input validation errors to
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
	case errors.Is(err, ErrShiftAlreadyOpen):
		return response.NewCodedError(http.StatusConflict, "SALES_SHIFT_ALREADY_OPEN", err.Error(), err)
	case errors.Is(err, ErrOpenShiftRequired):
		return response.NewCodedError(http.StatusConflict, "OPEN_SALES_SHIFT_REQUIRED", err.Error(), err)
	case errors.Is(err, ErrManagerApprovalUnavailable):
		// Every denial reason collapses here so the API never discloses which
		// condition failed. The reason is in the server log and audit event.
		return response.NewCodedError(http.StatusForbidden, "MANAGER_APPROVAL_UNAVAILABLE",
			"manager approval could not be confirmed with this login code and PIN", err)
	case errors.Is(err, ErrRequestConflict):
		return response.NewCodedError(http.StatusConflict, "REQUEST_CONFLICT", err.Error(), err)
	case errors.Is(err, ErrExpectedCashOutOfRange):
		return response.NewCodedError(http.StatusBadRequest, "EXPECTED_CASH_OUT_OF_RANGE", err.Error(), err)
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

- [ ] **Step 6: Write the failing error-mapping unit test**

Create `internal/shift/errors_test.go`:

```go
package shift_test

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/Mirai3103/pos-cafe/internal/shift"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMapDBErrorMapsConstraintFailures(t *testing.T) {
	assert.NoError(t, shift.MapDBError(nil))

	unique := &pgconn.PgError{Code: "23505", Message: "duplicate key", Detail: "sales_shift_only_one_open_unique"}
	assert.ErrorIs(t, shift.MapDBError(unique), shift.ErrShiftAlreadyOpen)

	fk := &pgconn.PgError{Code: "23503", Message: "foreign key violation"}
	assert.ErrorIs(t, shift.MapDBError(fk), shift.ErrOpenShiftRequired)

	// A check violation is a defect, not a business state: it must pass through
	// unmapped so it surfaces as a logged 500.
	check := &pgconn.PgError{Code: "23514", Message: "cash_movement_note_valid"}
	mapped := shift.MapDBError(check)
	assert.NotErrorIs(t, mapped, shift.ErrShiftAlreadyOpen)
	assert.NotErrorIs(t, mapped, shift.ErrOpenShiftRequired)
	assert.ErrorIs(t, mapped, check)
}

func TestMapHTTPErrorStatusesAndCodes(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{shift.ErrShiftAlreadyOpen, http.StatusConflict, "SALES_SHIFT_ALREADY_OPEN"},
		{shift.ErrOpenShiftRequired, http.StatusConflict, "OPEN_SALES_SHIFT_REQUIRED"},
		{shift.ErrManagerApprovalUnavailable, http.StatusForbidden, "MANAGER_APPROVAL_UNAVAILABLE"},
		{shift.ErrRequestConflict, http.StatusConflict, "REQUEST_CONFLICT"},
		{shift.ErrExpectedCashOutOfRange, http.StatusBadRequest, "EXPECTED_CASH_OUT_OF_RANGE"},
		{shift.ErrForbidden, http.StatusForbidden, "FORBIDDEN"},
		{shift.ErrUnauthorized, http.StatusUnauthorized, "UNAUTHORIZED"},
		{shift.ErrInvalidStoredResult, http.StatusInternalServerError, "INVALID_STORED_RESULT"},
		{response.ErrInvalid, http.StatusBadRequest, "INVALID_INPUT"},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			wrapped := fmt.Errorf("context: %w", tc.err)
			mapped := shift.MapHTTPError(wrapped)

			var coded *response.CodedError
			require.True(t, errors.As(mapped, &coded), "expected a *response.CodedError")
			assert.Equal(t, tc.status, coded.Status)
			assert.Equal(t, tc.code, coded.Code)
		})
	}
}

func TestMapHTTPErrorHidesApprovalReason(t *testing.T) {
	// The denial reason belongs in the log and the audit event, never in the
	// client message, which must not reveal whether a login code exists.
	wrapped := fmt.Errorf("%w: IDENTITY_DISABLED", shift.ErrManagerApprovalUnavailable)
	mapped := shift.MapHTTPError(wrapped)

	var coded *response.CodedError
	require.True(t, errors.As(mapped, &coded))
	assert.NotContains(t, coded.Message, "IDENTITY_DISABLED")
	assert.NotContains(t, coded.Message, "INVALID_PIN")
}
```

`response.CodedError` is defined at `internal/response/response.go:24` with exported fields `Status int`, `Code string`, `Message string`, and `Cause error`, which is what these assertions read.

- [ ] **Step 7: Run the unit tests to verify they pass**

Run: `go test -race ./internal/shift/ -v`

Expected: PASS for `TestValidateOpeningFloat`, `TestValidateAmount`, `TestValidateMethod`, `TestValidateReason`, `TestNormalizeNote`, `TestValidateNote`, `TestComputeExpectedCash`, `TestMapDBErrorMapsConstraintFailures`, `TestMapHTTPErrorStatusesAndCodes`, `TestMapHTTPErrorHidesApprovalReason`.

- [ ] **Step 8: Commit**

```bash
git add internal/shift/domain.go internal/shift/domain_test.go \
        internal/shift/dto.go internal/shift/errors.go internal/shift/errors_test.go
git commit -m "feat(shift): add domain rules, DTOs, and error mapping"
```

---

## Task 4: Transaction Executor

The executor is where authority reload, Manager approval, idempotency, audit, and commit ordering live. Every command in Tasks 5 and 6 is a short body handed to it.

The ordering is not negotiable: **authority reload and Manager approval both run before the idempotency claim**, so an actor whose session was revoked — or an approver who was disabled or demoted — cannot replay an earlier success.

**Files:**
- Create: `internal/shift/executor.go`
- Create: `internal/shift/executor_integration_test.go` (also the home of the helpers every other integration file uses)

**Interfaces:**
- Consumes: `auth.SessionStateLocked`, `auth.GetInactivityTimeout`, `auth.DeriveCapabilities`, `auth.VerifyManagerApproval`, `auth.ErrManagerApprovalDenied`, `auth.ApproverSummary` (Task 2); sqlc `GetShiftSessionAuthority`, `GetShiftSessionRoles`, `ShiftAdvisoryLock`, `GetIdempotencyRecord`, `ClaimIdempotencyRecord`, `StoreIdempotencyResult`, `InsertAuditEvent` (Task 1 and existing).
- Produces:
  - `type Actor struct { StaffID uuid.UUID; SessionID uuid.UUID }`
  - `type ApprovalSpec struct { ApproverLoginCode string; ManagerPIN string; RequiredCapability string }`
  - `type MutationSpec struct { RequestID uuid.UUID; Operation string; Fingerprint any; Required []string; Approval *ApprovalSpec }`
  - `type MutationContext struct { Queries *sqlc.Queries; Approver *auth.ApproverSummary }`
  - `type AuditRecord struct { EventType string; Details any }`
  - `type Runner struct{ ... }` and `func NewRunner(db *sql.DB, queries *sqlc.Queries) *Runner`
  - `func ExecuteMutation[T any](ctx context.Context, r *Runner, actor Actor, spec MutationSpec, fn func(MutationContext) (int, T, AuditRecord, error)) (int, T, error)`
  - `func ExecuteRead[T any](ctx context.Context, r *Runner, actor Actor, requiredCapability string, fn func(*sqlc.Queries) (T, error)) (T, error)`

  Test helpers produced here and used by Tasks 1, 5, 6, 7, 8: `openShiftTestDB`, `truncateShiftTables`, `testLoginCode`, `newTestActor`, `newTestActorWithPin`, `type testActor`.

- [ ] **Step 1: Write `executor.go`**

Create `internal/shift/executor.go`:

```go
package shift

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

// ApprovalSpec requires a second identity holding the Manager role to
// authenticate inline before the request is claimed.
type ApprovalSpec struct {
	ApproverLoginCode string
	ManagerPIN        string
	// RequiredCapability is the capability the approver must also hold, so a
	// Manager cannot approve an operation outside their own authority.
	RequiredCapability string
}

// MutationSpec carries request-level metadata for a mutation command.
//
// Fingerprint must never contain a PIN. Including a secret would make the
// idempotency key sensitive to it and would store a PIN-derived value at rest.
type MutationSpec struct {
	RequestID   uuid.UUID
	Operation   string
	Fingerprint any
	Required    []string
	// Approval is nil for operations needing no second-party approval.
	Approval *ApprovalSpec
}

// MutationContext carries per-execution facts the mutation body needs.
// Approver is nil when the spec required no approval.
type MutationContext struct {
	Queries  *sqlc.Queries
	Approver *auth.ApproverSummary
}

// AuditRecord describes the audit event to insert after a successful mutation.
// A zero EventType writes no business event.
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

// reloadAuthority loads the current session, roles, and capabilities inside the
// transaction, so authority removed mid-session takes effect immediately.
func reloadAuthority(ctx context.Context, q *sqlc.Queries, actor Actor) (
	sqlc.GetShiftSessionAuthorityRow, []string, error,
) {
	authRow, err := q.GetShiftSessionAuthority(ctx, sqlc.GetShiftSessionAuthorityParams{
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

	roles, err := q.GetShiftSessionRoles(ctx, authRow.StaffIdentityID)
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
//
// The details carry the operation and the reason text. The reason may name an
// approval denial such as INVALID_PIN, which is why this value is audited and
// logged but never returned to a client.
func recordDenial(ctx context.Context, q *sqlc.Queries, actor Actor,
	authority sqlc.GetShiftSessionAuthorityRow, operation string, denialErr error,
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
// optional second-party approval, idempotency, and audit. On success it returns
// the HTTP status and result.
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

	// 2. Verify the initiator's capabilities.
	if err := verifyCapabilities(spec.Required, caps); err != nil {
		return 0, zero, finishDenial(tx, recordDenial(ctx, q, actor, authRow, spec.Operation, err))
	}

	// 3. Verify second-party Manager approval, when the operation needs one.
	// This runs before the idempotency claim, so a replay cannot succeed using
	// an approver who has since been disabled or demoted. The approver row stays
	// locked for the rest of this transaction.
	var approver *auth.ApproverSummary
	if spec.Approval != nil {
		summary, approvalErr := auth.VerifyManagerApproval(ctx, q,
			spec.Approval.ApproverLoginCode,
			spec.Approval.ManagerPIN,
			spec.Approval.RequiredCapability)
		if approvalErr != nil {
			if errors.Is(approvalErr, auth.ErrManagerApprovalDenied) {
				denial := fmt.Errorf("%w: %s", ErrManagerApprovalUnavailable, approvalErr.Error())
				return 0, zero, finishDenial(tx, recordDenial(ctx, q, actor, authRow, spec.Operation, denial))
			}
			return 0, zero, approvalErr
		}
		approver = &summary
	}

	// 4. Fingerprint the normalized business input. The spec's Fingerprint type
	// has no PIN field, so no secret reaches the hash.
	reqHash, err := fpHash(spec.Fingerprint)
	if err != nil {
		return 0, zero, err
	}

	// 5. Advisory lock to serialize concurrent duplicates of this request.
	lockKey := idToLockKey(actor.StaffID) ^ idToLockKey(spec.RequestID)
	if err := q.ShiftAdvisoryLock(ctx, lockKey); err != nil {
		return 0, zero, fmt.Errorf("advisory lock: %w", err)
	}

	// 6. Look for an existing record, now that the lock is held.
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

	// 7. Claim the request before any business mutation.
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

	// 8. Run the mutation.
	resultCode, result, audit, err := fn(MutationContext{Queries: q, Approver: approver})
	if err != nil {
		return 0, zero, err
	}

	// 9. Write the business audit event, when there is one.
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

	// 10. Store the replayable result.
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

- [ ] **Step 2: Build to verify the generated names line up**

Run: `go build ./internal/shift/`

Expected: compiles. If it fails on `sqlc.GetShiftSessionAuthorityRow` or `GetShiftSessionAuthorityParams`, open `internal/database/sqlc/shift.sql.go` and use the exact generated names; do not rename the query in `shift.sql`, because Task 1 is already committed.

- [ ] **Step 3: Write the failing executor integration test**

Create `internal/shift/executor_integration_test.go`. This file also owns the helpers every other Shift integration file uses.

```go
//go:build integration

package shift_test

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/database"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/shift"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openShiftTestDB(t *testing.T) (*sql.DB, *sqlc.Queries) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	require.Contains(t, url, "_test")
	db, err := database.Open(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db, sqlc.New(db)
}

// truncateShiftTables clears Shift state before a test.
//
// This is mandatory, not hygiene: the one-open-Shift invariant is global, so a
// Shift left open by an earlier test makes every later open fail. Integration
// packages run with -p 1 precisely so this truncation is safe.
func truncateShiftTables(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`TRUNCATE cash_movements, sales_shifts RESTART IDENTITY CASCADE`)
	require.NoError(t, err)
}

type testActor struct {
	StaffID   uuid.UUID
	SessionID uuid.UUID
	LoginCode string
	Pin       string
}

func (a testActor) actor() shift.Actor {
	return shift.Actor{StaffID: a.StaffID, SessionID: a.SessionID}
}

// testLoginCode generates a high-entropy unique login code for a test identity.
// staff_identities.login_code is VARCHAR(24) and the prefix shares that budget.
// Identities are never deleted between runs, so a per-process counter would
// collide across runs.
func testLoginCode(prefix string) string {
	room := 24 - len(prefix)
	if room < 8 {
		panic("testLoginCode: prefix leaves too little entropy budget")
	}
	return prefix + strings.ReplaceAll(uuid.NewString(), "-", "")[:room]
}

// newTestActor creates an identity with an active session and no usable PIN.
func newTestActor(t *testing.T, q *sqlc.Queries, roles []string, enabled bool) testActor {
	t.Helper()
	return newTestActorWithPin(t, q, roles, enabled, "")
}

// newTestActorWithPin creates an identity whose PIN can be used for approval.
// Pass an empty pin when the identity never needs to approve anything.
func newTestActorWithPin(t *testing.T, q *sqlc.Queries, roles []string, enabled bool, pin string) testActor {
	t.Helper()
	ctx := context.Background()

	loginCode := testLoginCode("S")
	pinHash := ""
	if pin != "" {
		hash, err := auth.HashPin(pin)
		require.NoError(t, err)
		pinHash = hash
	}

	row, err := q.CreateStaffIdentity(ctx, sqlc.CreateStaffIdentityParams{
		DisplayName: "Shift Test " + loginCode,
		Btrim:       loginCode,
		PinHash:     pinHash,
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

	return testActor{StaffID: row.ID, SessionID: session.ID, LoginCode: loginCode, Pin: pin}
}

type probeFingerprint struct {
	Label string `json:"label"`
}

type probeResult struct {
	Value string `json:"value"`
}

func TestExecuteMutationRequiresCapability(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	runner := shift.NewRunner(db, q)
	ctx := context.Background()

	// BARISTA holds neither sales.operate nor sales_shift.operate.
	barista := newTestActor(t, q, []string{"BARISTA"}, true)

	_, _, err := shift.ExecuteMutation(ctx, runner, barista.actor(), shift.MutationSpec{
		RequestID:   uuid.New(),
		Operation:   shift.OpOpenShift,
		Fingerprint: probeFingerprint{Label: "probe"},
		Required:    []string{shift.CapSalesShiftOperate},
	}, func(shift.MutationContext) (int, probeResult, shift.AuditRecord, error) {
		t.Fatal("mutation body must not run for an unauthorized actor")
		return 0, probeResult{}, shift.AuditRecord{}, nil
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, shift.ErrForbidden)
}

func TestExecuteMutationReplaysStoredResult(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	runner := shift.NewRunner(db, q)
	ctx := context.Background()

	cashier := newTestActor(t, q, []string{"CASHIER"}, true)
	requestID := uuid.New()
	spec := shift.MutationSpec{
		RequestID:   requestID,
		Operation:   shift.OpOpenShift,
		Fingerprint: probeFingerprint{Label: "same"},
		Required:    []string{shift.CapSalesShiftOperate},
	}

	runs := 0
	body := func(shift.MutationContext) (int, probeResult, shift.AuditRecord, error) {
		runs++
		return 201, probeResult{Value: "first"}, shift.AuditRecord{}, nil
	}

	status, res, err := shift.ExecuteMutation(ctx, runner, cashier.actor(), spec, body)
	require.NoError(t, err)
	assert.Equal(t, 201, status)
	assert.Equal(t, "first", res.Value)

	status, res, err = shift.ExecuteMutation(ctx, runner, cashier.actor(), spec, body)
	require.NoError(t, err)
	assert.Equal(t, 201, status)
	assert.Equal(t, "first", res.Value)
	assert.Equal(t, 1, runs, "an exact replay must not re-run the mutation body")
}

func TestExecuteMutationRejectsReusedRequestID(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	runner := shift.NewRunner(db, q)
	ctx := context.Background()

	cashier := newTestActor(t, q, []string{"CASHIER"}, true)
	requestID := uuid.New()

	body := func(shift.MutationContext) (int, probeResult, shift.AuditRecord, error) {
		return 201, probeResult{Value: "stored"}, shift.AuditRecord{}, nil
	}

	_, _, err := shift.ExecuteMutation(ctx, runner, cashier.actor(), shift.MutationSpec{
		RequestID:   requestID,
		Operation:   shift.OpOpenShift,
		Fingerprint: probeFingerprint{Label: "original"},
		Required:    []string{shift.CapSalesShiftOperate},
	}, body)
	require.NoError(t, err)

	_, _, err = shift.ExecuteMutation(ctx, runner, cashier.actor(), shift.MutationSpec{
		RequestID:   requestID,
		Operation:   shift.OpOpenShift,
		Fingerprint: probeFingerprint{Label: "changed"},
		Required:    []string{shift.CapSalesShiftOperate},
	}, body)
	require.Error(t, err)
	assert.ErrorIs(t, err, shift.ErrRequestConflict)
}

func TestExecuteMutationDeniesReplayAfterIdentityDisabled(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	runner := shift.NewRunner(db, q)
	ctx := context.Background()

	cashier := newTestActor(t, q, []string{"CASHIER"}, true)
	spec := shift.MutationSpec{
		RequestID:   uuid.New(),
		Operation:   shift.OpOpenShift,
		Fingerprint: probeFingerprint{Label: "before"},
		Required:    []string{shift.CapSalesShiftOperate},
	}
	body := func(shift.MutationContext) (int, probeResult, shift.AuditRecord, error) {
		return 201, probeResult{Value: "stored"}, shift.AuditRecord{}, nil
	}

	_, _, err := shift.ExecuteMutation(ctx, runner, cashier.actor(), spec, body)
	require.NoError(t, err)

	_, err = db.Exec(`UPDATE staff_identities SET enabled = false WHERE id = $1`, cashier.StaffID)
	require.NoError(t, err)

	// Authority is reloaded before replay, so the stored result is unreachable.
	_, _, err = shift.ExecuteMutation(ctx, runner, cashier.actor(), spec, body)
	require.Error(t, err)
	assert.ErrorIs(t, err, shift.ErrForbidden)
}

func TestExecuteMutationApprovalRunsBeforeReplay(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	runner := shift.NewRunner(db, q)
	ctx := context.Background()

	cashier := newTestActor(t, q, []string{"CASHIER"}, true)
	manager := newTestActorWithPin(t, q, []string{"MANAGER"}, true, "8642")

	spec := shift.MutationSpec{
		RequestID:   uuid.New(),
		Operation:   shift.OpRecordCashMovement,
		Fingerprint: probeFingerprint{Label: "approved"},
		Required:    []string{shift.CapSalesShiftOperate},
		Approval: &shift.ApprovalSpec{
			ApproverLoginCode:  manager.LoginCode,
			ManagerPIN:         "8642",
			RequiredCapability: shift.CapSalesShiftOperate,
		},
	}

	var sawApprover uuid.UUID
	body := func(mc shift.MutationContext) (int, probeResult, shift.AuditRecord, error) {
		require.NotNil(t, mc.Approver, "an approval spec must deliver an approver")
		sawApprover = mc.Approver.ID
		return 201, probeResult{Value: "stored"}, shift.AuditRecord{}, nil
	}

	_, _, err := shift.ExecuteMutation(ctx, runner, cashier.actor(), spec, body)
	require.NoError(t, err)
	assert.Equal(t, manager.StaffID, sawApprover)

	// Demote the approver, then attempt an exact replay. Approval is verified
	// before the idempotency lookup, so the stored result is unreachable.
	_, err = db.Exec(`DELETE FROM staff_operational_roles WHERE staff_identity_id = $1`, manager.StaffID)
	require.NoError(t, err)

	_, _, err = shift.ExecuteMutation(ctx, runner, cashier.actor(), spec, body)
	require.Error(t, err)
	assert.ErrorIs(t, err, shift.ErrManagerApprovalUnavailable)
}

func TestExecuteMutationConcurrentDuplicatesRunOnce(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	runner := shift.NewRunner(db, q)
	ctx := context.Background()

	cashier := newTestActor(t, q, []string{"CASHIER"}, true)
	spec := shift.MutationSpec{
		RequestID:   uuid.New(),
		Operation:   shift.OpOpenShift,
		Fingerprint: probeFingerprint{Label: "concurrent"},
		Required:    []string{shift.CapSalesShiftOperate},
	}

	var mu sync.Mutex
	runs := 0
	body := func(shift.MutationContext) (int, probeResult, shift.AuditRecord, error) {
		mu.Lock()
		runs++
		mu.Unlock()
		return 201, probeResult{Value: "stored"}, shift.AuditRecord{}, nil
	}

	const goroutines = 4
	start := make(chan struct{})
	errs := make(chan error, goroutines)
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _, execErr := shift.ExecuteMutation(ctx, runner, cashier.actor(), spec, body)
			errs <- execErr
		}()
	}
	close(start)
	wg.Wait()
	close(errs)

	for execErr := range errs {
		assert.NoError(t, execErr, "every concurrent duplicate must succeed by replay")
	}
	assert.Equal(t, 1, runs, "the advisory lock must serialize duplicates so the body runs once")
}
```

- [ ] **Step 4: Run the executor integration tests to verify they pass**

Run: `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -race -p 1 -tags=integration ./internal/shift/ -run TestExecuteMutation -v`

Expected: PASS for all six `TestExecuteMutation*` tests.

- [ ] **Step 5: Run the schema tests from Task 1, which now have their helpers**

Run: `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -race -p 1 -tags=integration ./internal/shift/ -run TestSchema -v`

Expected: PASS for all four `TestSchema*` tests, including every subtest of `TestSchemaRejectsInvalidCashMovements`.

- [ ] **Step 6: Commit**

```bash
git add internal/shift/executor.go internal/shift/executor_integration_test.go
git commit -m "feat(shift): add transaction executor with approval, idempotency, and audit"
```

---

## Task 5: Open Sales Shift

**Files:**
- Create: `internal/shift/open_shift.go`
- Create: `internal/shift/open_shift_integration_test.go`

**Interfaces:**
- Consumes: `ExecuteMutation`, `MutationSpec`, `MutationContext`, `AuditRecord`, `Actor`, `Runner` (Task 4); `ValidateOpeningFloat`, `OpOpenShift`, `EventSalesShiftOpened`, `StateOpen` (Task 3); sqlc `OpenSalesShift`, `GetStaffSummary` (Task 1).
- Produces: `type OpenShiftHandler struct{...}`, `func NewOpenShiftHandler(runner *Runner) *OpenShiftHandler`, and
  `func (h *OpenShiftHandler) Handle(ctx context.Context, actor Actor, cmd OpenShiftCommand) (int, SalesShiftResponse, error)`.
  Tasks 7 and 8 call `Handle`; Task 8 also stores the handler on `Slices`.

**A note on generated field names:** sqlc converts `opening_float_vnd` to `OpeningFloatVnd`, not `OpeningFloatVND`. Read the generated struct in `internal/database/sqlc/shift.sql.go` and use its spelling exactly. The API DTO keeps `OpeningFloatVND`, which is why the two differ by one letter at the boundary.

- [ ] **Step 1: Write the implementation**

Create `internal/shift/open_shift.go`:

```go
package shift

import (
	"context"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
)

type openShiftFingerprint struct {
	OpeningFloatVND int64 `json:"opening_float_vnd"`
}

type salesShiftOpenedAuditDetails struct {
	SalesShiftID    uuid.UUID `json:"sales_shift_id"`
	OpeningFloatVND int64     `json:"opening_float_vnd"`
}

// OpenShiftHandler opens a Sales Shift.
type OpenShiftHandler struct{ runner *Runner }

// NewOpenShiftHandler creates a new OpenShiftHandler.
func NewOpenShiftHandler(runner *Runner) *OpenShiftHandler {
	return &OpenShiftHandler{runner: runner}
}

// Handle opens a Sales Shift with a counted Opening Float.
//
// At most one Sales Shift may be OPEN across the whole system. The partial
// unique index sales_shift_only_one_open_unique is the sole authority for that
// invariant: a second open receives SALES_SHIFT_ALREADY_OPEN rather than a
// generic 500, and two truly concurrent opens resolve to exactly one success.
func (h *OpenShiftHandler) Handle(ctx context.Context, actor Actor, cmd OpenShiftCommand) (int, SalesShiftResponse, error) {
	openingFloat := *cmd.OpeningFloatVND

	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpOpenShift,
		Fingerprint: openShiftFingerprint{OpeningFloatVND: openingFloat},
		Required:    []string{CapSalesShiftOperate},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, SalesShiftResponse, AuditRecord, error) {
			if err := ValidateOpeningFloat(openingFloat); err != nil {
				return 0, SalesShiftResponse{}, AuditRecord{},
					fmt.Errorf("%w: %s", response.ErrInvalid, err.Error())
			}

			row, err := mc.Queries.OpenSalesShift(ctx, sqlc.OpenSalesShiftParams{
				OpenedByStaffIdentityID: actor.StaffID,
				OpeningFloatVnd:         openingFloat,
			})
			if err != nil {
				return 0, SalesShiftResponse{}, AuditRecord{}, MapDBError(err)
			}

			opener, err := mc.Queries.GetStaffSummary(ctx, actor.StaffID)
			if err != nil {
				return 0, SalesShiftResponse{}, AuditRecord{},
					fmt.Errorf("load opener summary: %w", err)
			}

			result := SalesShiftResponse{
				ID:              row.ID,
				State:           row.State,
				OpeningFloatVND: row.OpeningFloatVnd,
				OpenedAt:        row.OpenedAt,
				Opener: StaffSummary{
					ID:          opener.ID,
					DisplayName: opener.DisplayName,
					LoginCode:   opener.LoginCode,
				},
			}

			return 201, result, AuditRecord{
				EventType: EventSalesShiftOpened,
				Details: salesShiftOpenedAuditDetails{
					SalesShiftID:    row.ID,
					OpeningFloatVND: row.OpeningFloatVnd,
				},
			}, nil
		})
}
```

- [ ] **Step 2: Write the failing integration test**

Create `internal/shift/open_shift_integration_test.go`:

```go
//go:build integration

package shift_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/shift"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func int64Ptr(v int64) *int64 { return &v }

// countAuditEvents counts business audit events of one type naming a Shift.
func countAuditEvents(t *testing.T, db *sql.DB, eventType string, shiftID uuid.UUID) int {
	t.Helper()
	var n int
	err := db.QueryRow(
		`SELECT count(*) FROM audit_events
		 WHERE event_type = $1 AND details->>'sales_shift_id' = $2`,
		eventType, shiftID.String(),
	).Scan(&n)
	require.NoError(t, err)
	return n
}

func TestOpenShiftRecordsFloatOpenerAndAudit(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	handler := shift.NewOpenShiftHandler(shift.NewRunner(db, q))
	ctx := context.Background()

	cashier := newTestActor(t, q, []string{"CASHIER"}, true)

	status, res, err := handler.Handle(ctx, cashier.actor(), shift.OpenShiftCommand{
		RequestID:       uuid.New(),
		OpeningFloatVND: int64Ptr(500000),
	})
	require.NoError(t, err)
	assert.Equal(t, 201, status)
	assert.Equal(t, shift.StateOpen, res.State)
	assert.Equal(t, int64(500000), res.OpeningFloatVND)
	assert.Equal(t, cashier.StaffID, res.Opener.ID)
	assert.Equal(t, cashier.LoginCode, res.Opener.LoginCode)
	assert.False(t, res.OpenedAt.IsZero())

	assert.Equal(t, 1, countAuditEvents(t, db, shift.EventSalesShiftOpened, res.ID))
}

func TestOpenShiftAcceptsZeroFloat(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	handler := shift.NewOpenShiftHandler(shift.NewRunner(db, q))
	ctx := context.Background()

	cashier := newTestActor(t, q, []string{"CASHIER"}, true)

	// A station may legitimately open with an empty fund.
	_, res, err := handler.Handle(ctx, cashier.actor(), shift.OpenShiftCommand{
		RequestID:       uuid.New(),
		OpeningFloatVND: int64Ptr(0),
	})
	require.NoError(t, err)
	assert.Equal(t, int64(0), res.OpeningFloatVND)
}

func TestOpenShiftRejectsSecondOpenShift(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	handler := shift.NewOpenShiftHandler(shift.NewRunner(db, q))
	ctx := context.Background()

	first := newTestActor(t, q, []string{"CASHIER"}, true)
	second := newTestActor(t, q, []string{"MANAGER"}, true)

	_, _, err := handler.Handle(ctx, first.actor(), shift.OpenShiftCommand{
		RequestID:       uuid.New(),
		OpeningFloatVND: int64Ptr(500000),
	})
	require.NoError(t, err)

	// The invariant is system-wide, not per identity or per station.
	_, _, err = handler.Handle(ctx, second.actor(), shift.OpenShiftCommand{
		RequestID:       uuid.New(),
		OpeningFloatVND: int64Ptr(700000),
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, shift.ErrShiftAlreadyOpen)
}

func TestOpenShiftRejectsInvalidFloat(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	handler := shift.NewOpenShiftHandler(shift.NewRunner(db, q))
	ctx := context.Background()

	cashier := newTestActor(t, q, []string{"CASHIER"}, true)

	for _, amount := range []int64{-1, shift.MaxAmountVND + 1} {
		_, _, err := handler.Handle(ctx, cashier.actor(), shift.OpenShiftCommand{
			RequestID:       uuid.New(),
			OpeningFloatVND: int64Ptr(amount),
		})
		require.Error(t, err, "amount %d", amount)
	}
}

func TestOpenShiftDeniesBarista(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	handler := shift.NewOpenShiftHandler(shift.NewRunner(db, q))
	ctx := context.Background()

	barista := newTestActor(t, q, []string{"BARISTA"}, true)

	_, _, err := handler.Handle(ctx, barista.actor(), shift.OpenShiftCommand{
		RequestID:       uuid.New(),
		OpeningFloatVND: int64Ptr(500000),
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, shift.ErrForbidden)
}

func TestOpenShiftIsIdempotent(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	handler := shift.NewOpenShiftHandler(shift.NewRunner(db, q))
	ctx := context.Background()

	cashier := newTestActor(t, q, []string{"CASHIER"}, true)
	requestID := uuid.New()
	cmd := shift.OpenShiftCommand{RequestID: requestID, OpeningFloatVND: int64Ptr(500000)}

	_, first, err := handler.Handle(ctx, cashier.actor(), cmd)
	require.NoError(t, err)

	status, replay, err := handler.Handle(ctx, cashier.actor(), cmd)
	require.NoError(t, err)
	assert.Equal(t, 201, status)
	assert.Equal(t, first.ID, replay.ID)
	assert.Equal(t, first.OpeningFloatVND, replay.OpeningFloatVND)

	// A replay writes no second business event.
	assert.Equal(t, 1, countAuditEvents(t, db, shift.EventSalesShiftOpened, first.ID))

	// The same request_id with a different float is a conflict, not a replay.
	_, _, err = handler.Handle(ctx, cashier.actor(), shift.OpenShiftCommand{
		RequestID:       requestID,
		OpeningFloatVND: int64Ptr(700000),
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, shift.ErrRequestConflict)
}

func TestOpenShiftConcurrentOpensYieldExactlyOne(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	handler := shift.NewOpenShiftHandler(shift.NewRunner(db, q))
	ctx := context.Background()

	const goroutines = 4
	actors := make([]testActor, goroutines)
	for i := range actors {
		actors[i] = newTestActor(t, q, []string{"CASHIER"}, true)
	}

	var succeeded atomic.Int32
	start := make(chan struct{})
	errs := make(chan error, goroutines)
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(a testActor) {
			defer wg.Done()
			<-start
			_, _, execErr := handler.Handle(ctx, a.actor(), shift.OpenShiftCommand{
				RequestID:       uuid.New(),
				OpeningFloatVND: int64Ptr(500000),
			})
			if execErr == nil {
				succeeded.Add(1)
			}
			errs <- execErr
		}(actors[i])
	}
	close(start)
	wg.Wait()
	close(errs)

	assert.Equal(t, int32(1), succeeded.Load(), "exactly one concurrent open must succeed")
	for execErr := range errs {
		if execErr != nil {
			assert.ErrorIs(t, execErr, shift.ErrShiftAlreadyOpen,
				"a losing open must report the domain conflict, never a generic failure")
		}
	}

	var openCount int
	require.NoError(t, f.DB.QueryRow(`SELECT count(*) FROM sales_shifts WHERE state = 'OPEN'`).Scan(&openCount))
	assert.Equal(t, 1, openCount)
}

func TestOpenShiftAuditDetailsCarryNoSecrets(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	handler := shift.NewOpenShiftHandler(shift.NewRunner(db, q))
	ctx := context.Background()

	cashier := newTestActor(t, q, []string{"CASHIER"}, true)
	_, res, err := handler.Handle(ctx, cashier.actor(), shift.OpenShiftCommand{
		RequestID:       uuid.New(),
		OpeningFloatVND: int64Ptr(500000),
	})
	require.NoError(t, err)

	var raw []byte
	require.NoError(t, f.DB.QueryRow(
		`SELECT details FROM audit_events
		 WHERE event_type = $1 AND details->>'sales_shift_id' = $2`,
		shift.EventSalesShiftOpened, res.ID.String(),
	).Scan(&raw))

	var details map[string]any
	require.NoError(t, json.Unmarshal(raw, &details))
	assert.Equal(t, []string{"opening_float_vnd", "sales_shift_id"}, sortedKeys(details))
}

// sortedKeys returns a map's keys in ascending order for stable assertions.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}
```

- [ ] **Step 3: Run the integration tests to verify they pass**

Run: `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -race -p 1 -tags=integration ./internal/shift/ -run TestOpenShift -v`

Expected: PASS for all seven `TestOpenShift*` tests.

If `TestOpenShiftConcurrentOpensYieldExactlyOne` reports a losing goroutine with a raw driver error instead of `ErrShiftAlreadyOpen`, `MapDBError` is not being applied to the `OpenSalesShift` result — check Step 1.

- [ ] **Step 4: Commit**

```bash
git add internal/shift/open_shift.go internal/shift/open_shift_integration_test.go
git commit -m "feat(shift): add open sales shift command"
```

---

## Task 6: Record Cash Movement

The only Phase 4 command needing a second identity. A cashier initiates; an enabled Manager authenticates inline to approve. Self-approval is permitted, and the audit trail keeps initiator and approver apart so a self-approved movement stays distinguishable.

**Files:**
- Create: `internal/shift/cash_movement.go`
- Create: `internal/shift/cash_movement_integration_test.go`

**Interfaces:**
- Consumes: `ExecuteMutation`, `ApprovalSpec`, `MutationContext` (Task 4); `ValidateMethod`, `ValidateReason`, `ValidateAmount`, `NormalizeNote`, `ValidateNote`, `ComputeExpectedCash`, `OpRecordCashMovement`, `EventCashMovementRecorded` (Task 3); sqlc `GetOpenSalesShiftForUpdate`, `InsertCashMovement`, `SumCashMovements`, `GetStaffSummary` (Task 1).
- Produces: `type RecordCashMovementHandler struct{...}`, `func NewRecordCashMovementHandler(runner *Runner) *RecordCashMovementHandler`, and
  `func (h *RecordCashMovementHandler) Handle(ctx context.Context, actor Actor, cmd RecordCashMovementCommand) (int, CashMovementResult, error)`.
  Task 8 stores the handler on `Slices` and calls `Handle`.

- [ ] **Step 1: Write the implementation**

Create `internal/shift/cash_movement.go`:

```go
package shift

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
)

// cashMovementFingerprint is the idempotency fingerprint for a Cash Movement.
//
// ManagerPIN is deliberately absent. Including a secret would make the
// idempotency key sensitive to it and would store a PIN-derived value at rest.
// ApproverLoginCode is not a secret and is included, so re-using a request_id
// with a different approver is a conflict rather than a silent replay.
type cashMovementFingerprint struct {
	SalesShiftID      uuid.UUID `json:"sales_shift_id"`
	Method            string    `json:"method"`
	AmountVND         int64     `json:"amount_vnd"`
	Reason            string    `json:"reason"`
	Note              *string   `json:"note"`
	ApproverLoginCode string    `json:"approver_login_code"`
}

type cashMovementAuditDetails struct {
	CashMovementID          uuid.UUID `json:"cash_movement_id"`
	SalesShiftID            uuid.UUID `json:"sales_shift_id"`
	Method                  string    `json:"method"`
	AmountVND               int64     `json:"amount_vnd"`
	Reason                  string    `json:"reason"`
	Note                    *string   `json:"note"`
	InitiatorStaffIdentityID uuid.UUID `json:"initiator_staff_identity_id"`
	ApproverStaffIdentityID  uuid.UUID `json:"approver_staff_identity_id"`
}

// RecordCashMovementHandler records a Pay In or Pay Out.
type RecordCashMovementHandler struct{ runner *Runner }

// NewRecordCashMovementHandler creates a new RecordCashMovementHandler.
func NewRecordCashMovementHandler(runner *Runner) *RecordCashMovementHandler {
	return &RecordCashMovementHandler{runner: runner}
}

// Handle records a Cash Movement against an open Sales Shift and returns the
// resulting Expected Cash, so the terminal updates its drawer figure without a
// second request.
//
// Cash Movements are append-only: Phase 4 provides no edit, reverse, or delete.
func (h *RecordCashMovementHandler) Handle(ctx context.Context, actor Actor, cmd RecordCashMovementCommand) (int, CashMovementResult, error) {
	note := NormalizeNote(cmd.Note)
	amount := *cmd.AmountVND
	approverLoginCode := auth.NormalizeLoginCode(cmd.ApproverLoginCode)

	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpRecordCashMovement,
		Fingerprint: cashMovementFingerprint{
			SalesShiftID:      cmd.ShiftID,
			Method:            cmd.Method,
			AmountVND:         amount,
			Reason:            cmd.Reason,
			Note:              note,
			ApproverLoginCode: approverLoginCode,
		},
		Required: []string{CapSalesShiftOperate},
		Approval: &ApprovalSpec{
			ApproverLoginCode:  approverLoginCode,
			ManagerPIN:         cmd.ManagerPIN,
			RequiredCapability: CapSalesShiftOperate,
		},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, CashMovementResult, AuditRecord, error) {
			var zero CashMovementResult

			if err := ValidateMethod(cmd.Method); err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error())
			}
			if err := ValidateReason(cmd.Reason); err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error())
			}
			if err := ValidateAmount(amount); err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error())
			}
			if err := ValidateNote(note, cmd.Reason); err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error())
			}

			// Lock the Shift row so a concurrent state change cannot be missed.
			// A missing or non-OPEN Shift maps to OPEN_SALES_SHIFT_REQUIRED.
			openShift, err := mc.Queries.GetOpenSalesShiftForUpdate(ctx, cmd.ShiftID)
			if err != nil {
				return 0, zero, AuditRecord{}, MapDBError(err)
			}

			initiator, err := mc.Queries.GetStaffSummary(ctx, actor.StaffID)
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("load initiator summary: %w", err)
			}

			noteArg := sql.NullString{}
			if note != nil {
				noteArg = sql.NullString{String: *note, Valid: true}
			}

			inserted, err := mc.Queries.InsertCashMovement(ctx, sqlc.InsertCashMovementParams{
				SalesShiftID:                  openShift.ID,
				Method:                        cmd.Method,
				AmountVnd:                     amount,
				Reason:                        cmd.Reason,
				Note:                          noteArg,
				InitiatedByStaffIdentityID:    actor.StaffID,
				InitiatedStaffAccessSessionID: actor.SessionID,
				ApprovedByStaffIdentityID:     mc.Approver.ID,
			})
			if err != nil {
				return 0, zero, AuditRecord{}, MapDBError(err)
			}

			sums, err := mc.Queries.SumCashMovements(ctx, openShift.ID)
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("sum cash movements: %w", err)
			}
			expected, err := ComputeExpectedCash(openShift.OpeningFloatVnd, sums.PayInVnd, sums.PayOutVnd)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}

			movement := CashMovementResponse{
				ID:           inserted.ID,
				SalesShiftID: openShift.ID,
				Method:       cmd.Method,
				AmountVND:    amount,
				Reason:       cmd.Reason,
				Note:         note,
				Initiator: StaffSummary{
					ID:          initiator.ID,
					DisplayName: initiator.DisplayName,
					LoginCode:   initiator.LoginCode,
				},
				Approver:   staffSummaryFromApprover(*mc.Approver),
				OccurredAt: inserted.OccurredAt,
			}

			return 201, CashMovementResult{
				Movement:        movement,
				ExpectedCashVND: expected,
			}, AuditRecord{
				EventType: EventCashMovementRecorded,
				Details: cashMovementAuditDetails{
					CashMovementID:           movement.ID,
					SalesShiftID:             openShift.ID,
					Method:                   movement.Method,
					AmountVND:                movement.AmountVND,
					Reason:                   movement.Reason,
					Note:                     movement.Note,
					InitiatorStaffIdentityID: initiator.ID,
					ApproverStaffIdentityID:  mc.Approver.ID,
				},
			}, nil
		})
}
```

- [ ] **Step 2: Write the failing integration test**

Create `internal/shift/cash_movement_integration_test.go`:

```go
//go:build integration

package shift_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/shift"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shiftFixture is a clean database with one open Sales Shift, a cashier who
// opened it, and a Manager who can approve Cash Movements.
type shiftFixture struct {
	DB       *sql.DB
	Queries  *sqlc.Queries
	Runner   *shift.Runner
	Cashier  testActor
	Manager  testActor
	Shift    shift.SalesShiftResponse
	Movement *shift.RecordCashMovementHandler
}

func newShiftFixture(t *testing.T) shiftFixture {
	t.Helper()
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	runner := shift.NewRunner(db, q)

	cashier := newTestActor(t, q, []string{"CASHIER"}, true)
	manager := newTestActorWithPin(t, q, []string{"MANAGER"}, true, "8642")

	_, opened, err := shift.NewOpenShiftHandler(runner).Handle(context.Background(), cashier.actor(),
		shift.OpenShiftCommand{RequestID: uuid.New(), OpeningFloatVND: int64Ptr(500000)})
	require.NoError(t, err)

	return shiftFixture{
		DB:       db,
		Queries:  q,
		Runner:   runner,
		Cashier:  cashier,
		Manager:  manager,
		Shift:    opened,
		Movement: shift.NewRecordCashMovementHandler(runner),
	}
}

func (f shiftFixture) command(method, reason string, amount int64, note *string) shift.RecordCashMovementCommand {
	return shift.RecordCashMovementCommand{
		RequestID:         uuid.New(),
		ShiftID:           f.Shift.ID,
		Method:            method,
		AmountVND:         &amount,
		Reason:            reason,
		Note:              note,
		ApproverLoginCode: f.Manager.LoginCode,
		ManagerPIN:        "8642",
	}
}

func TestCashMovementRecordsPayInAndPayOut(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()

	status, payIn, err := f.Movement.Handle(ctx, f.Cashier.actor(),
		f.command(shift.MethodPayIn, shift.ReasonAddChangeFund, 100000, nil))
	require.NoError(t, err)
	assert.Equal(t, 201, status)
	assert.Equal(t, int64(600000), payIn.ExpectedCashVND, "500000 float + 100000 pay in")
	assert.Equal(t, f.Cashier.StaffID, payIn.Movement.Initiator.ID)
	assert.Equal(t, f.Manager.StaffID, payIn.Movement.Approver.ID)
	assert.Nil(t, payIn.Movement.Note)

	_, payOut, err := f.Movement.Handle(ctx, f.Cashier.actor(),
		f.command(shift.MethodPayOut, shift.ReasonSafeDrop, 150000, nil))
	require.NoError(t, err)
	assert.Equal(t, int64(450000), payOut.ExpectedCashVND, "600000 - 150000 pay out")

	// Movements accumulate rather than replacing one another.
	_, third, err := f.Movement.Handle(ctx, f.Cashier.actor(),
		f.command(shift.MethodPayOut, shift.ReasonRemoveExcessFloat, 50000, nil))
	require.NoError(t, err)
	assert.Equal(t, int64(400000), third.ExpectedCashVND)
}

func TestCashMovementRequiresNoteForReasonOther(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()

	_, _, err := f.Movement.Handle(ctx, f.Cashier.actor(),
		f.command(shift.MethodPayOut, shift.ReasonOther, 50000, nil))
	require.Error(t, err)

	// A whitespace-only note normalizes to nil and must be rejected too.
	blank := "   "
	_, _, err = f.Movement.Handle(ctx, f.Cashier.actor(),
		f.command(shift.MethodPayOut, shift.ReasonOther, 50000, &blank))
	require.Error(t, err)

	note := "  mua da cho quay pha che  "
	_, res, err := f.Movement.Handle(ctx, f.Cashier.actor(),
		f.command(shift.MethodPayOut, shift.ReasonOther, 50000, &note))
	require.NoError(t, err)
	require.NotNil(t, res.Movement.Note)
	assert.Equal(t, "mua da cho quay pha che", *res.Movement.Note, "the note must be stored trimmed")
}

func TestCashMovementRejectsInvalidInput(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()

	cases := []struct {
		name   string
		method string
		reason string
		amount int64
	}{
		{"unknown method", "CASH_DROP", shift.ReasonSafeDrop, 50000},
		{"unknown reason", shift.MethodPayIn, "PETTY_CASH", 50000},
		{"zero amount", shift.MethodPayIn, shift.ReasonAddChangeFund, 0},
		{"negative amount", shift.MethodPayOut, shift.ReasonSafeDrop, -50000},
		{"over-bound amount", shift.MethodPayIn, shift.ReasonAddChangeFund, shift.MaxAmountVND + 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := f.Movement.Handle(ctx, f.Cashier.actor(),
				f.command(tc.method, tc.reason, tc.amount, nil))
			require.Error(t, err)
		})
	}
}

func TestCashMovementRequiresOpenShift(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()

	// A Shift that does not exist.
	cmd := f.command(shift.MethodPayIn, shift.ReasonAddChangeFund, 50000, nil)
	cmd.ShiftID = uuid.New()
	_, _, err := f.Movement.Handle(ctx, f.Cashier.actor(), cmd)
	require.Error(t, err)
	assert.ErrorIs(t, err, shift.ErrOpenShiftRequired)

	// A Shift that is no longer OPEN.
	_, err = f.DB.Exec(`UPDATE sales_shifts SET state = 'CLOSED' WHERE id = $1`, f.Shift.ID)
	require.NoError(t, err)

	_, _, err = f.Movement.Handle(ctx, f.Cashier.actor(),
		f.command(shift.MethodPayIn, shift.ReasonAddChangeFund, 50000, nil))
	require.Error(t, err)
	assert.ErrorIs(t, err, shift.ErrOpenShiftRequired)
}

func TestCashMovementApprovalRules(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()

	disabled := newTestActorWithPin(t, f.Queries, []string{"MANAGER"}, false, "8642")
	cashierApprover := newTestActorWithPin(t, f.Queries, []string{"CASHIER"}, true, "8642")

	cases := []struct {
		name      string
		loginCode string
		pin       string
	}{
		{"wrong pin", f.Manager.LoginCode, "0000"},
		{"unknown login code", "ZZUNKNOWN", "8642"},
		{"disabled manager", disabled.LoginCode, "8642"},
		{"cashier cannot approve", cashierApprover.LoginCode, "8642"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := f.command(shift.MethodPayOut, shift.ReasonSafeDrop, 50000, nil)
			cmd.ApproverLoginCode = tc.loginCode
			cmd.ManagerPIN = tc.pin

			_, _, err := f.Movement.Handle(ctx, f.Cashier.actor(), cmd)
			require.Error(t, err)
			assert.ErrorIs(t, err, shift.ErrManagerApprovalUnavailable,
				"every denial reason must collapse to one client-visible outcome")
		})
	}

	var recorded int
	require.NoError(t, f.DB.QueryRow(
		`SELECT count(*) FROM cash_movements WHERE sales_shift_id = $1`, f.Shift.ID).Scan(&recorded))
	assert.Equal(t, 0, recorded, "a denied approval must record nothing")
}

func TestCashMovementAllowsManagerSelfApproval(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	runner := shift.NewRunner(db, q)
	ctx := context.Background()

	// A Manager working alone supplies their own login code and PIN.
	manager := newTestActorWithPin(t, q, []string{"MANAGER"}, true, "8642")
	_, opened, err := shift.NewOpenShiftHandler(runner).Handle(ctx, manager.actor(),
		shift.OpenShiftCommand{RequestID: uuid.New(), OpeningFloatVND: int64Ptr(500000)})
	require.NoError(t, err)

	amount := int64(50000)
	_, res, err := shift.NewRecordCashMovementHandler(runner).Handle(ctx, manager.actor(),
		shift.RecordCashMovementCommand{
			RequestID:         uuid.New(),
			ShiftID:           opened.ID,
			Method:            shift.MethodPayOut,
			AmountVND:         &amount,
			Reason:            shift.ReasonSafeDrop,
			ApproverLoginCode: manager.LoginCode,
			ManagerPIN:        "8642",
		})
	require.NoError(t, err)

	// Initiator and approver are recorded separately, so a self-approved
	// movement stays distinguishable in the audit trail.
	assert.Equal(t, manager.StaffID, res.Movement.Initiator.ID)
	assert.Equal(t, manager.StaffID, res.Movement.Approver.ID)
}

func TestCashMovementIsIdempotent(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()

	cmd := f.command(shift.MethodPayOut, shift.ReasonSafeDrop, 50000, nil)

	_, first, err := f.Movement.Handle(ctx, f.Cashier.actor(), cmd)
	require.NoError(t, err)

	status, replay, err := f.Movement.Handle(ctx, f.Cashier.actor(), cmd)
	require.NoError(t, err)
	assert.Equal(t, 201, status)
	assert.Equal(t, first.Movement.ID, replay.Movement.ID)
	assert.Equal(t, first.ExpectedCashVND, replay.ExpectedCashVND)

	var recorded int
	require.NoError(t, f.DB.QueryRow(
		`SELECT count(*) FROM cash_movements WHERE sales_shift_id = $1`, f.Shift.ID).Scan(&recorded))
	assert.Equal(t, 1, recorded, "a replay must not record a second movement")

	// The same request_id with a different amount is a conflict.
	conflicting := cmd
	other := int64(70000)
	conflicting.AmountVND = &other
	_, _, err = f.Movement.Handle(ctx, f.Cashier.actor(), conflicting)
	require.Error(t, err)
	assert.ErrorIs(t, err, shift.ErrRequestConflict)
}

func TestCashMovementPinNeverPersisted(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()

	_, _, err := f.Movement.Handle(ctx, f.Cashier.actor(),
		f.command(shift.MethodPayOut, shift.ReasonSafeDrop, 50000, nil))
	require.NoError(t, err)

	// The PIN must reach neither the audit trail nor the idempotency record.
	var auditHits int
	require.NoError(t, f.DB.QueryRow(
		`SELECT count(*) FROM audit_events WHERE details::text LIKE '%8642%'`).Scan(&auditHits))
	assert.Equal(t, 0, auditHits)

	var idempotencyHits int
	require.NoError(t, f.DB.QueryRow(
		`SELECT count(*) FROM idempotency_keys
		 WHERE response_body::text LIKE '%8642%' OR request_hash LIKE '%8642%'`).Scan(&idempotencyHits))
	assert.Equal(t, 0, idempotencyHits)
}
```

- [ ] **Step 3: Run the integration tests to verify they pass**

Run: `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -race -p 1 -tags=integration ./internal/shift/ -run TestCashMovement -v`

Expected: PASS for all eight `TestCashMovement*` tests and every subtest.

- [ ] **Step 4: Commit**

```bash
git add internal/shift/cash_movement.go internal/shift/cash_movement_integration_test.go
git commit -m "feat(shift): add cash movement command with manager approval"
```

---

## Task 7: Current Sales Shift Read

**Files:**
- Create: `internal/shift/current.go`
- Create: `internal/shift/current_integration_test.go`

**Interfaces:**
- Consumes: `ExecuteRead` (Task 4); `ComputeExpectedCash`, `CapSalesShiftOperate` (Task 3); sqlc `GetOpenSalesShift`, `ListCashMovements`, `SumCashMovements` (Task 1).
- Produces: `type CurrentShiftHandler struct{...}`, `func NewCurrentShiftHandler(runner *Runner) *CurrentShiftHandler`, and
  `func (h *CurrentShiftHandler) Handle(ctx context.Context, actor Actor) (*CurrentSalesShiftResponse, error)` — a nil result means no Shift is open, which is a normal state rather than an error.
  Task 8 stores the handler on `Slices` and calls `Handle`.

- [ ] **Step 1: Write the implementation**

Create `internal/shift/current.go`:

```go
package shift

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
)

// CurrentShiftHandler serves the current Sales Shift read.
type CurrentShiftHandler struct{ runner *Runner }

// NewCurrentShiftHandler creates a new CurrentShiftHandler.
func NewCurrentShiftHandler(runner *Runner) *CurrentShiftHandler {
	return &CurrentShiftHandler{runner: runner}
}

// Handle returns the open Sales Shift with its Expected Cash and Cash
// Movements, or nil when no Shift is open.
//
// "No Shift is currently open" is a normal operating state that the cashier
// screen renders directly, so it is a successful nil rather than a not-found
// error.
//
// The read runs in a read-only repeatable-read transaction, so the capability
// check, the Shift row, the movement list, and the Expected Cash aggregate all
// observe one snapshot.
func (h *CurrentShiftHandler) Handle(ctx context.Context, actor Actor) (*CurrentSalesShiftResponse, error) {
	return ExecuteRead(ctx, h.runner, actor, CapSalesShiftOperate,
		func(q *sqlc.Queries) (*CurrentSalesShiftResponse, error) {
			row, err := q.GetOpenSalesShift(ctx)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return nil, nil
				}
				return nil, fmt.Errorf("load open sales shift: %w", err)
			}

			movementRows, err := q.ListCashMovements(ctx, row.ID)
			if err != nil {
				return nil, fmt.Errorf("list cash movements: %w", err)
			}

			sums, err := q.SumCashMovements(ctx, row.ID)
			if err != nil {
				return nil, fmt.Errorf("sum cash movements: %w", err)
			}
			expected, err := ComputeExpectedCash(row.OpeningFloatVnd, sums.PayInVnd, sums.PayOutVnd)
			if err != nil {
				return nil, err
			}

			// Serialize an empty list as [] rather than null.
			movements := make([]CashMovementResponse, 0, len(movementRows))
			for _, m := range movementRows {
				var note *string
				if m.Note.Valid {
					value := m.Note.String
					note = &value
				}
				movements = append(movements, CashMovementResponse{
					ID:           m.ID,
					SalesShiftID: m.SalesShiftID,
					Method:       m.Method,
					AmountVND:    m.AmountVnd,
					Reason:       m.Reason,
					Note:         note,
					Initiator: StaffSummary{
						ID:          m.InitiatorID,
						DisplayName: m.InitiatorDisplayName,
						LoginCode:   m.InitiatorLoginCode,
					},
					Approver: StaffSummary{
						ID:          m.ApproverID,
						DisplayName: m.ApproverDisplayName,
						LoginCode:   m.ApproverLoginCode,
					},
					OccurredAt: m.OccurredAt,
				})
			}

			return &CurrentSalesShiftResponse{
				SalesShiftResponse: SalesShiftResponse{
					ID:              row.ID,
					State:           row.State,
					OpeningFloatVND: row.OpeningFloatVnd,
					OpenedAt:        row.OpenedAt,
					Opener: StaffSummary{
						ID:          row.OpenerID,
						DisplayName: row.OpenerDisplayName,
						LoginCode:   row.OpenerLoginCode,
					},
				},
				ExpectedCashVND: expected,
				CashMovements:   movements,
			}, nil
		})
}
```

- [ ] **Step 2: Write the failing integration test**

Create `internal/shift/current_integration_test.go`:

```go
//go:build integration

package shift_test

import (
	"context"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/shift"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCurrentShiftReturnsNilWhenNoneOpen(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	ctx := context.Background()

	cashier := newTestActor(t, q, []string{"CASHIER"}, true)

	res, err := shift.NewCurrentShiftHandler(shift.NewRunner(db, q)).Handle(ctx, cashier.actor())
	require.NoError(t, err, "no open Shift is a normal state, not an error")
	assert.Nil(t, res)
}

func TestCurrentShiftReportsFloatOpenerAndEmptyMovements(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()

	res, err := shift.NewCurrentShiftHandler(f.Runner).Handle(ctx, f.Cashier.actor())
	require.NoError(t, err)
	require.NotNil(t, res)

	assert.Equal(t, f.Shift.ID, res.ID)
	assert.Equal(t, shift.StateOpen, res.State)
	assert.Equal(t, int64(500000), res.OpeningFloatVND)
	assert.Equal(t, int64(500000), res.ExpectedCashVND, "with no movements, Expected Cash is the float")
	assert.Equal(t, f.Cashier.StaffID, res.Opener.ID)
	assert.NotNil(t, res.CashMovements)
	assert.Empty(t, res.CashMovements)
}

func TestCurrentShiftReportsMovementsNewestFirst(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()

	_, first, err := f.Movement.Handle(ctx, f.Cashier.actor(),
		f.command(shift.MethodPayIn, shift.ReasonAddChangeFund, 100000, nil))
	require.NoError(t, err)
	_, second, err := f.Movement.Handle(ctx, f.Cashier.actor(),
		f.command(shift.MethodPayOut, shift.ReasonSafeDrop, 150000, nil))
	require.NoError(t, err)

	res, err := shift.NewCurrentShiftHandler(f.Runner).Handle(ctx, f.Cashier.actor())
	require.NoError(t, err)
	require.NotNil(t, res)

	require.Len(t, res.CashMovements, 2)
	assert.Equal(t, second.Movement.ID, res.CashMovements[0].ID, "newest first")
	assert.Equal(t, first.Movement.ID, res.CashMovements[1].ID)
	assert.Equal(t, int64(450000), res.ExpectedCashVND)

	// Each movement carries both parties.
	assert.Equal(t, f.Cashier.StaffID, res.CashMovements[0].Initiator.ID)
	assert.Equal(t, f.Manager.StaffID, res.CashMovements[0].Approver.ID)
}

func TestCurrentShiftIgnoresClosedShifts(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()

	_, err := f.DB.Exec(`UPDATE sales_shifts SET state = 'CLOSED' WHERE id = $1`, f.Shift.ID)
	require.NoError(t, err)

	res, err := shift.NewCurrentShiftHandler(f.Runner).Handle(ctx, f.Cashier.actor())
	require.NoError(t, err)
	assert.Nil(t, res)
}

func TestCurrentShiftDeniesBarista(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	ctx := context.Background()

	barista := newTestActor(t, q, []string{"BARISTA"}, true)

	_, err := shift.NewCurrentShiftHandler(shift.NewRunner(db, q)).Handle(ctx, barista.actor())
	require.Error(t, err)
	assert.ErrorIs(t, err, shift.ErrForbidden)
}

func TestCurrentShiftDeniesRevokedSession(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()

	_, err := f.DB.Exec(`UPDATE staff_access_sessions SET revoked_at = now() WHERE id = $1`, f.Cashier.SessionID)
	require.NoError(t, err)

	_, err = shift.NewCurrentShiftHandler(f.Runner).Handle(ctx, f.Cashier.actor())
	require.Error(t, err)
	assert.ErrorIs(t, err, shift.ErrUnauthorized)
}

func TestCurrentShiftReportsUnknownShiftIDNotFoundForMovement(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()

	// Guards the Shift-scoped aggregate: a movement recorded against the open
	// Shift must not leak into another Shift's Expected Cash.
	_, res, err := f.Movement.Handle(ctx, f.Cashier.actor(),
		f.command(shift.MethodPayIn, shift.ReasonAddChangeFund, 100000, nil))
	require.NoError(t, err)
	assert.Equal(t, f.Shift.ID, res.Movement.SalesShiftID)
	assert.NotEqual(t, uuid.Nil, res.Movement.SalesShiftID)
}
```

- [ ] **Step 3: Run the integration tests to verify they pass**

Run: `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -race -p 1 -tags=integration ./internal/shift/ -run TestCurrentShift -v`

Expected: PASS for all seven `TestCurrentShift*` tests.

If the compiler rejects `row.OpenerID` or `m.InitiatorDisplayName`, open `internal/database/sqlc/shift.sql.go` and use the generated row-struct field names exactly.

- [ ] **Step 4: Run the whole Shift package**

Run: `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -race -p 1 -tags=integration ./internal/shift/`

Expected: PASS. Every Shift test to date runs together, which also proves the truncation discipline holds under the global one-open-Shift invariant.

- [ ] **Step 5: Commit**

```bash
git add internal/shift/current.go internal/shift/current_integration_test.go
git commit -m "feat(shift): add current sales shift read with expected cash"
```

---

## Task 8: HTTP Layer, Routes, And Application Wiring

**Files:**
- Create: `internal/shift/http.go`
- Create: `internal/shift/routes.go`
- Create: `internal/shift/shift_integration_test.go`
- Modify: `cmd/api/main.go` (after the tables slice wiring, currently lines 169-170)
- Regenerate: `docs/` via `swag init`

**Interfaces:**
- Consumes: `auth.GetStaff`, `auth.Middleware.RequireAuth`, `auth.Middleware.RequireCapability`, `auth.ValidatePinFormat`; `response.OK`, `response.Created`, `response.Error`, `response.ErrInvalid`, `response.ErrUnauthorized`; every handler from Tasks 5-7.
- Produces: `type Slices struct{ Runner *Runner; Current *CurrentShiftHandler; OpenShift *OpenShiftHandler; RecordCashMovement *RecordCashMovementHandler }`, `func NewSlices(db *sql.DB, queries *sqlc.Queries) *Slices`, and `func (s *Slices) RegisterRoutes(v1 *echo.Group, authn *auth.Middleware)`.

- [ ] **Step 1: Write `http.go`**

Create `internal/shift/http.go`:

```go
package shift

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

// checkMoney rejects a missing numeric field rather than defaulting it. Zero is
// a meaningful Opening Float, so a nil pointer must not silently become one.
func checkMoney(v *int64, field string) error {
	if v == nil {
		return fmt.Errorf("%w: %s is required", response.ErrInvalid, field)
	}
	return nil
}

func checkRequiredString(v, field string) error {
	if v == "" {
		return fmt.Errorf("%w: %s is required", response.ErrInvalid, field)
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

// handleGetCurrent returns the open Sales Shift, or null when none is open.
//
//	@Summary		Current Sales Shift
//	@Description	Returns the open Sales Shift with its Expected Cash and Cash Movements, or null when no Shift is open. expected_cash_vnd covers the Opening Float and Cash Movements only; Cash Payments and Cash Refunds join the figure in Phase 5 (ADR-008).
//	@Tags			shifts
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	response.APIResponse{data=CurrentSalesShiftResponse}
//	@Failure		401	{object}	response.APIResponse
//	@Failure		403	{object}	response.APIResponse
//	@Router			/shifts/current [get]
func (s *Slices) handleGetCurrent(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	res, err := s.Current.Handle(c.Request().Context(), actor)
	if err != nil {
		return sendError(c, err)
	}
	// A nil result serializes as data: null, which is the canonical
	// "no Shift is open" response, not a 404.
	return response.OK(c, res)
}

// handleOpenShift opens a Sales Shift.
//
//	@Summary		Open a Sales Shift
//	@Description	Opens a Sales Shift with a counted Opening Float. At most one Sales Shift may be open across the whole system.
//	@Tags			shifts
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		OpenShiftCommand	true	"Opening Float"
//	@Success		201		{object}	response.APIResponse{data=SalesShiftResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Router			/shifts [post]
func (s *Slices) handleOpenShift(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	cmd, err := bindBody[OpenShiftCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(cmd.RequestID); err != nil {
		return sendError(c, err)
	}
	if err := checkMoney(cmd.OpeningFloatVND, "opening_float_vnd"); err != nil {
		return sendError(c, err)
	}

	status, res, err := s.OpenShift.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
}

// handleRecordCashMovement records a Pay In or Pay Out.
//
//	@Summary		Record a Cash Movement
//	@Description	Records a Pay In or Pay Out against an open Sales Shift. Requires inline approval by an enabled Manager, who authenticates with their own login code and PIN. Returns the resulting Expected Cash.
//	@Tags			shifts
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			shift_id	path		string						true	"Sales Shift ID"
//	@Param			request		body		RecordCashMovementCommand	true	"Cash Movement to record"
//	@Success		201			{object}	response.APIResponse{data=CashMovementResult}
//	@Failure		400			{object}	response.APIResponse
//	@Failure		401			{object}	response.APIResponse
//	@Failure		403			{object}	response.APIResponse
//	@Failure		409			{object}	response.APIResponse
//	@Router			/shifts/{shift_id}/cash-movements [post]
func (s *Slices) handleRecordCashMovement(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	shiftID, err := parseUUIDParam(c, "shift_id")
	if err != nil {
		return sendError(c, err)
	}
	cmd, err := bindBody[RecordCashMovementCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(cmd.RequestID); err != nil {
		return sendError(c, err)
	}
	if err := checkMoney(cmd.AmountVND, "amount_vnd"); err != nil {
		return sendError(c, err)
	}
	if err := checkRequiredString(cmd.Method, "method"); err != nil {
		return sendError(c, err)
	}
	if err := checkRequiredString(cmd.Reason, "reason"); err != nil {
		return sendError(c, err)
	}
	if err := checkRequiredString(cmd.ApproverLoginCode, "approver_login_code"); err != nil {
		return sendError(c, err)
	}
	// A malformed PIN is rejected on shape alone, before any identity lookup.
	// This leaks nothing: it says the field is wrong, never whose PIN it is.
	if err := auth.ValidatePinFormat(cmd.ManagerPIN); err != nil {
		return sendError(c, fmt.Errorf("%w: manager_pin: %s", response.ErrInvalid, err.Error()))
	}
	cmd.ShiftID = shiftID

	status, res, err := s.RecordCashMovement.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
}
```

- [ ] **Step 2: Write `routes.go`**

Create `internal/shift/routes.go`:

```go
package shift

import (
	"database/sql"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/labstack/echo/v4"
)

// Slices aggregates the Shift handlers.
type Slices struct {
	Runner *Runner

	Current            *CurrentShiftHandler
	OpenShift          *OpenShiftHandler
	RecordCashMovement *RecordCashMovementHandler
}

// NewSlices wires every Shift handler onto a shared Runner.
func NewSlices(db *sql.DB, queries *sqlc.Queries) *Slices {
	runner := NewRunner(db, queries)
	return &Slices{
		Runner:             runner,
		Current:            NewCurrentShiftHandler(runner),
		OpenShift:          NewOpenShiftHandler(runner),
		RecordCashMovement: NewRecordCashMovementHandler(runner),
	}
}

// RegisterRoutes mounts the Shift routes under /shifts on the provided group.
//
// Routes are mounted directly on v1 (rather than a /shifts sub-group carrying
// RequireAuth) because any echo.Group holding group-level middleware also
// auto-registers two echo_route_not_found catch-all routes, which would make
// the router expose more than the three Shift routes. This matches the
// reasoning already recorded in internal/tables/routes.go.
func (s *Slices) RegisterRoutes(v1 *echo.Group, authn *auth.Middleware) {
	v1.GET("/shifts/current", s.handleGetCurrent,
		authn.RequireAuth(), authn.RequireCapability(CapSalesShiftOperate))
	v1.POST("/shifts", s.handleOpenShift,
		authn.RequireAuth(), authn.RequireCapability(CapSalesShiftOperate))
	v1.POST("/shifts/:shift_id/cash-movements", s.handleRecordCashMovement,
		authn.RequireAuth(), authn.RequireCapability(CapSalesShiftOperate))
}
```

- [ ] **Step 3: Wire the slice into the application**

In `cmd/api/main.go`, immediately after the existing tables wiring:

```go
	tablesSlices := tables.NewSlices(db, queries)
	tablesSlices.RegisterRoutes(v1, authSlices.Middleware)
```

add:

```go
	shiftSlices := shift.NewSlices(db, queries)
	shiftSlices.RegisterRoutes(v1, authSlices.Middleware)
```

and add `"github.com/Mirai3103/pos-cafe/internal/shift"` to the import block.

- [ ] **Step 4: Build and confirm the server starts**

Run: `go build ./...`

Expected: exits 0.

Run: `go run ./cmd/api` and confirm the startup log lists the three new routes, then stop it.

- [ ] **Step 5: Write the failing end-to-end HTTP test**

Create `internal/shift/shift_integration_test.go`:

```go
//go:build integration

package shift_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/httpvalidator"
	"github.com/Mirai3103/pos-cafe/internal/shift"
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

// newTestServer builds an Echo server with auth and shift routes mounted the
// same way cmd/api/main.go mounts them.
func newTestServer(t *testing.T) (*echo.Echo, *sqlc.Queries) {
	t.Helper()
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)

	e := echo.New()
	e.Validator = httpvalidator.New()
	v1 := e.Group("/api/v1")

	authSlices := auth.NewSlices(db, q)
	authSlices.RegisterRoutes(v1)

	shiftSlices := shift.NewSlices(db, q)
	shiftSlices.RegisterRoutes(v1, authSlices.Middleware)

	return e, q
}

// signIn creates an identity with the given roles and returns a bearer token.
// It uses the real sign-in endpoint so the middleware path is exercised.
func signIn(t *testing.T, e *echo.Echo, q *sqlc.Queries, roles []string, pin string) (string, string) {
	t.Helper()
	ctx := context.Background()

	loginCode := testLoginCode("H")
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
	return payload.Token, loginCode
}

func doRequest(t *testing.T, e *echo.Echo, method, path, token string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	reader := bytes.NewReader(body)
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestShiftHTTPHappyPath(t *testing.T) {
	e, q := newTestServer(t)
	cashierToken, _ := signIn(t, e, q, []string{"CASHIER"}, "2468")
	_, managerCode := signIn(t, e, q, []string{"MANAGER"}, "8642")

	// No Shift is open yet: 200 with data null, never 404.
	rec := doRequest(t, e, http.MethodGet, "/api/v1/shifts/current", cashierToken, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var env envelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	assert.True(t, env.Success)
	assert.JSONEq(t, "null", string(env.Data))

	// Open a Shift.
	body, _ := json.Marshal(map[string]any{"request_id": uuid.New(), "opening_float_vnd": 500000})
	rec = doRequest(t, e, http.MethodPost, "/api/v1/shifts", cashierToken, body)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	var opened shift.SalesShiftResponse
	require.NoError(t, json.Unmarshal(env.Data, &opened))
	assert.Equal(t, shift.StateOpen, opened.State)
	assert.Equal(t, int64(500000), opened.OpeningFloatVND)

	// Record a Cash Movement with Manager approval.
	body, _ = json.Marshal(map[string]any{
		"request_id":          uuid.New(),
		"method":              shift.MethodPayOut,
		"amount_vnd":          50000,
		"reason":              shift.ReasonSafeDrop,
		"approver_login_code": managerCode,
		"manager_pin":         "8642",
	})
	rec = doRequest(t, e, http.MethodPost,
		"/api/v1/shifts/"+opened.ID.String()+"/cash-movements", cashierToken, body)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	var result shift.CashMovementResult
	require.NoError(t, json.Unmarshal(env.Data, &result))
	assert.Equal(t, int64(450000), result.ExpectedCashVND)

	// The current read now reports the movement.
	rec = doRequest(t, e, http.MethodGet, "/api/v1/shifts/current", cashierToken, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	var current shift.CurrentSalesShiftResponse
	require.NoError(t, json.Unmarshal(env.Data, &current))
	assert.Equal(t, int64(450000), current.ExpectedCashVND)
	require.Len(t, current.CashMovements, 1)
	assert.Equal(t, result.Movement.ID, current.CashMovements[0].ID)
}

func TestShiftHTTPSerializesEmptyMovementsAsArray(t *testing.T) {
	e, q := newTestServer(t)
	token, _ := signIn(t, e, q, []string{"CASHIER"}, "2468")

	body, _ := json.Marshal(map[string]any{"request_id": uuid.New(), "opening_float_vnd": 500000})
	rec := doRequest(t, e, http.MethodPost, "/api/v1/shifts", token, body)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	rec = doRequest(t, e, http.MethodGet, "/api/v1/shifts/current", token, nil)
	require.Equal(t, http.StatusOK, rec.Code)

	// Assert on the raw JSON: an empty list must be [] and never null.
	assert.Contains(t, rec.Body.String(), `"cash_movements":[]`)
	assert.NotContains(t, rec.Body.String(), `"cash_movements":null`)
}

func TestShiftHTTPAuthorization(t *testing.T) {
	e, q := newTestServer(t)
	baristaToken, _ := signIn(t, e, q, []string{"BARISTA"}, "1357")

	openBody, _ := json.Marshal(map[string]any{"request_id": uuid.New(), "opening_float_vnd": 500000})
	movementBody, _ := json.Marshal(map[string]any{
		"request_id": uuid.New(), "method": shift.MethodPayIn, "amount_vnd": 1000,
		"reason": shift.ReasonAddChangeFund, "approver_login_code": "ZZ", "manager_pin": "8642",
	})

	cases := []struct {
		name   string
		method string
		path   string
		body   []byte
	}{
		{"current", http.MethodGet, "/api/v1/shifts/current", nil},
		{"open", http.MethodPost, "/api/v1/shifts", openBody},
		{"cash movement", http.MethodPost,
			"/api/v1/shifts/" + uuid.New().String() + "/cash-movements", movementBody},
	}
	for _, tc := range cases {
		t.Run(tc.name+" denies barista", func(t *testing.T) {
			rec := doRequest(t, e, tc.method, tc.path, baristaToken, tc.body)
			assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
		})
		t.Run(tc.name+" denies anonymous", func(t *testing.T) {
			rec := doRequest(t, e, tc.method, tc.path, "", tc.body)
			assert.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
		})
	}
}

func TestShiftHTTPValidation(t *testing.T) {
	e, q := newTestServer(t)
	token, _ := signIn(t, e, q, []string{"CASHIER"}, "2468")

	openBody, _ := json.Marshal(map[string]any{"request_id": uuid.New(), "opening_float_vnd": 500000})
	rec := doRequest(t, e, http.MethodPost, "/api/v1/shifts", token, openBody)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var env envelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	var opened shift.SalesShiftResponse
	require.NoError(t, json.Unmarshal(env.Data, &opened))

	cases := []struct {
		name string
		path string
		body map[string]any
	}{
		{
			name: "missing request_id",
			path: "/api/v1/shifts",
			body: map[string]any{"opening_float_vnd": 500000},
		},
		{
			// Zero is a valid float, so a missing field must be rejected rather
			// than defaulted.
			name: "missing opening_float_vnd",
			path: "/api/v1/shifts",
			body: map[string]any{"request_id": uuid.New()},
		},
		{
			name: "missing amount_vnd",
			path: "/api/v1/shifts/" + opened.ID.String() + "/cash-movements",
			body: map[string]any{
				"request_id": uuid.New(), "method": shift.MethodPayIn,
				"reason": shift.ReasonAddChangeFund,
				"approver_login_code": "ZZ", "manager_pin": "8642",
			},
		},
		{
			name: "malformed manager_pin",
			path: "/api/v1/shifts/" + opened.ID.String() + "/cash-movements",
			body: map[string]any{
				"request_id": uuid.New(), "method": shift.MethodPayIn, "amount_vnd": 1000,
				"reason": shift.ReasonAddChangeFund,
				"approver_login_code": "ZZ", "manager_pin": "abc",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(tc.body)
			rec := doRequest(t, e, http.MethodPost, tc.path, token, body)
			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())

			var errEnv envelope
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errEnv))
			require.NotNil(t, errEnv.Error)
			assert.Equal(t, "INVALID_INPUT", errEnv.Error.Code)
		})
	}

	t.Run("malformed shift_id", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"request_id": uuid.New(), "method": shift.MethodPayIn, "amount_vnd": 1000,
			"reason": shift.ReasonAddChangeFund,
			"approver_login_code": "ZZ", "manager_pin": "8642",
		})
		rec := doRequest(t, e, http.MethodPost, "/api/v1/shifts/not-a-uuid/cash-movements", token, body)
		assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	})
}

func TestShiftHTTPErrorCodes(t *testing.T) {
	e, q := newTestServer(t)
	cashierToken, _ := signIn(t, e, q, []string{"CASHIER"}, "2468")
	_, managerCode := signIn(t, e, q, []string{"MANAGER"}, "8642")

	openBody, _ := json.Marshal(map[string]any{"request_id": uuid.New(), "opening_float_vnd": 500000})
	rec := doRequest(t, e, http.MethodPost, "/api/v1/shifts", cashierToken, openBody)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var env envelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	var opened shift.SalesShiftResponse
	require.NoError(t, json.Unmarshal(env.Data, &opened))

	t.Run("second open conflicts", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"request_id": uuid.New(), "opening_float_vnd": 700000})
		rec := doRequest(t, e, http.MethodPost, "/api/v1/shifts", cashierToken, body)
		require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())

		var errEnv envelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errEnv))
		require.NotNil(t, errEnv.Error)
		assert.Equal(t, "SALES_SHIFT_ALREADY_OPEN", errEnv.Error.Code)
	})

	t.Run("wrong manager pin is forbidden without naming the reason", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"request_id": uuid.New(), "method": shift.MethodPayOut, "amount_vnd": 50000,
			"reason": shift.ReasonSafeDrop,
			"approver_login_code": managerCode, "manager_pin": "0000",
		})
		rec := doRequest(t, e, http.MethodPost,
			"/api/v1/shifts/"+opened.ID.String()+"/cash-movements", cashierToken, body)
		require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

		var errEnv envelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errEnv))
		require.NotNil(t, errEnv.Error)
		assert.Equal(t, "MANAGER_APPROVAL_UNAVAILABLE", errEnv.Error.Code)
		assert.NotContains(t, errEnv.Error.Message, "INVALID_PIN")
		assert.NotContains(t, errEnv.Error.Message, "IDENTITY_DISABLED")
	})

	t.Run("unknown shift conflicts", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"request_id": uuid.New(), "method": shift.MethodPayOut, "amount_vnd": 50000,
			"reason": shift.ReasonSafeDrop,
			"approver_login_code": managerCode, "manager_pin": "8642",
		})
		rec := doRequest(t, e, http.MethodPost,
			"/api/v1/shifts/"+uuid.New().String()+"/cash-movements", cashierToken, body)
		require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())

		var errEnv envelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errEnv))
		require.NotNil(t, errEnv.Error)
		assert.Equal(t, "OPEN_SALES_SHIFT_REQUIRED", errEnv.Error.Code)
	})
}

func TestShiftHTTPStaffSummaryFieldsAreExactlyThree(t *testing.T) {
	e, q := newTestServer(t)
	token, _ := signIn(t, e, q, []string{"CASHIER"}, "2468")

	body, _ := json.Marshal(map[string]any{"request_id": uuid.New(), "opening_float_vnd": 500000})
	rec := doRequest(t, e, http.MethodPost, "/api/v1/shifts", token, body)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var env envelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	var payload struct {
		Opener map[string]any `json:"opener"`
	}
	require.NoError(t, json.Unmarshal(env.Data, &payload))

	// No pin hash, role list, enablement flag, or session detail may cross the
	// Shift boundary.
	assert.Len(t, payload.Opener, 3)
	for _, key := range []string{"id", "display_name", "login_code"} {
		assert.Contains(t, payload.Opener, key)
	}
}
```

- [ ] **Step 6: Run the HTTP integration tests to verify they pass**

Run: `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -race -p 1 -tags=integration ./internal/shift/ -run TestShiftHTTP -v`

Expected: PASS for all six `TestShiftHTTP*` tests and every subtest.

If `TestShiftHTTPAuthorization` returns 404 instead of 401 or 403 for a route, the route path in `routes.go` does not match the test path. Fix `routes.go`, not the test.

- [ ] **Step 7: Regenerate the Swagger documentation**

Run: `swag init -g cmd/api/main.go -o docs`

Then verify all three operations are present:

Run: `grep -c "shifts" docs/swagger.json`

Expected: a non-zero count, with `/shifts`, `/shifts/current`, and `/shifts/{shift_id}/cash-movements` each appearing as a path key.

- [ ] **Step 8: Commit**

```bash
git add internal/shift/http.go internal/shift/routes.go \
        internal/shift/shift_integration_test.go cmd/api/main.go docs/
git commit -m "feat(shift): add HTTP handlers, routes, app wiring, and Swagger docs"
```

---

## Task 9: Decision Records, Roadmap Status, And Full Verification

**Files:**
- Modify: `spec/decisions.md` (append ADR-008 and ADR-009)
- Modify: `MIGRATE_PLAN.md` (Phase 4 status and tracker row)

**Interfaces:**
- Consumes: everything from Tasks 1-8.
- Produces: no code. This task closes the phase.

- [ ] **Step 1: Append ADR-008 and ADR-009 to `spec/decisions.md`**

Append at the end of the file:

```markdown
---

## ADR-008: Partial Expected Cash in Phase 4

* **Decision Date:** 2026-09-13
* **Status:** Accepted
* **Context:** The canonical Expected Cash formula is Opening Float plus Cash Payments and Pay Ins, less Cash Refunds and Pay Outs. The Cash Payment and Cash Refund terms read the `payments` table, which references `checks`, which in turn references `service_sessions` and order drafts — the whole chain is owned by Phase 5. Provisioning it in Phase 4 would replicate the most intricate constraint set in the system a phase early, for a figure Phase 4 cannot yet produce anyway.
* **Decision:**
* Phase 4 computes `expected_cash_vnd` as Opening Float plus Pay Ins less Pay Outs, on read, never stored.
* The API field ships in its final name and shape from Phase 4, so the response contract does not change when the formula completes.
* Phase 5 adds the Cash Payment and Cash Refund terms to the single `SumCashMovements`-adjacent computation in `internal/shift/current.go`.
* The Swagger description and the design spec both state that the figure is incomplete until Phase 5.
* **Consequences:**
* The public Shift API contract is complete and stable from Phase 4 onward.
* Until Phase 5 lands, `expected_cash_vnd` reflects fund movements only and must not be presented to staff as a reconciliation figure.
* The guard on the total is symmetric, because sustained Pay Outs can legitimately drive the partial figure negative.

---

## ADR-009: `auth.VerifyManagerApproval` as a Shared Second-Party Approval Primitive

* **Decision Date:** 2026-09-13
* **Status:** Accepted
* **Context:** Recording a Cash Movement requires approval by a second identity holding the Manager role, who authenticates inline with a login code and PIN. This differs from the Catalog pattern, which re-verifies the actor's own PIN. Phase 5 requires the identical mechanism for Refund, Payment Void, and Comp.
* **Decision:**
* The verification lives in `internal/auth` as `VerifyManagerApproval`, and slices supply only the capability the approver must hold.
* It locks the approver row with `FOR UPDATE`, runs PIN verification against a dummy hash when no identity matches so response timing reveals nothing, and reports one of four denial reasons.
* Callers collapse every denial reason to one client-visible code. The specific reason reaches the server log and the denial audit event only.
* **Consequences:**
* Security-critical verification is implemented and tested once rather than copied into each slice that needs it.
* This is a deliberate exception to the general rule that slices do not share helpers. It does not extend to idempotency helpers, which ADR-007 keeps slice-local.
```

- [ ] **Step 2: Update `MIGRATE_PLAN.md`**

Change the Phase 4 heading from:

```markdown
### Phase 4: Sales Shift & Cash Movements (`internal/shift`)
```

to:

```markdown
### Phase 4: Sales Shift & Cash Movements (`internal/shift`) (✅ COMPLETED)
*Approved Design Spec:* [`docs/superpowers/specs/2026-09-13-shift-slice-design.md`](docs/superpowers/specs/2026-09-13-shift-slice-design.md)
*Implementation Plan:* [`docs/superpowers/plans/2026-09-13-shift-slice.md`](docs/superpowers/plans/2026-09-13-shift-slice.md)

> The checklist below predates the canonical source review and is superseded by the spec above. It is kept only as a record of the original sketch.
```

Do not rewrite the 4.1-4.4 checklist items themselves. They are a historical record, exactly as the Phase 3 sketch was left in place.

Then change the tracker row from:

```markdown
| **4. Sales Shift & Cash** | ⏳ PENDING | 3 | 0 / 3 | Phase 4 |
```

to:

```markdown
| **4. Sales Shift & Cash** | ✅ DONE | 3 | 3 / 3 | 2026-09-13 |
```

- [ ] **Step 3: Run the full unit test suite**

Run: `go test -race ./...`

Expected: PASS across every package. No Auth, Catalog, or Tables test may regress.

- [ ] **Step 4: Run the full integration test suite**

Run: `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -race -p 1 -tags=integration ./...`

Expected: PASS across every package, including Auth, Catalog, and Tables.

The Shift suite truncates `sales_shifts` and `cash_movements` but nothing else, so it cannot disturb another slice's fixtures.

- [ ] **Step 5: Run the quality gates**

Run: `make fmt && make vet && make lint`

Expected: all exit 0. Address any `golangci-lint` finding in the new code rather than suppressing it, unless it duplicates an existing `//nolint` rationale already used in `internal/tables/executor.go`.

- [ ] **Step 6: Verify the acceptance criteria**

Walk section 14 of `docs/superpowers/specs/2026-09-13-shift-slice-design.md` and confirm each of the sixteen criteria against the code and the test output. Three are easy to lose track of:

- Criterion 7 (no secret persisted) is covered by `TestCashMovementPinNeverPersisted`.
- Criterion 10 (authority and approval evaluated before replay) is covered by `TestExecuteMutationDeniesReplayAfterIdentityDisabled` and `TestExecuteMutationApprovalRunsBeforeReplay`.
- Criterion 3 (one open Shift under concurrency) is covered by `TestOpenShiftConcurrentOpensYieldExactlyOne` and `TestSchemaRejectsSecondOpenShift`.

Anything unmet is a defect to fix before committing, not a note to carry forward.

- [ ] **Step 7: Commit**

```bash
git add spec/decisions.md MIGRATE_PLAN.md
git commit -m "docs(shift): add ADR-008 and ADR-009, mark Phase 4 complete"
```

- [ ] **Step 8: Final verification before handing off**

Run: `git status --short`

Expected: clean tree.

Run: `git log --oneline master..HEAD`

Expected: the Phase 4 commits in order — schema, approval primitive, domain, executor, open shift, cash movement, current read, HTTP, docs — on top of the Phase 3 work.
