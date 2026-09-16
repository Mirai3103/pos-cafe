# Preparation Queue Reads & Bulk Transitions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a privacy-safe active Preparation Queue and idempotent partial-success bulk transitions to the existing `internal/preparation` slice.

**Architecture:** Keep Submit as the synchronous creator of Preparation Units. Extend `internal/preparation` with a repeatable-read queue projection and one shared transition helper; the bulk command wraps each selected unit in a PostgreSQL savepoint while one outer mutation transaction owns authority, idempotency, auditing, and commit.

**Tech Stack:** Go 1.27.1, Echo v4, PostgreSQL 17, `database/sql` with pgx stdlib, sqlc v2, testify, Swaggo/OpenAPI 2.0

**Spec:** `docs/superpowers/specs/2026-09-16-preparation-queue-transitions-design.md`

## Global Constraints

- `internal/sales` continues to create Preparation Units directly in Submit; do not add a Watermill publisher or consumer.
- `internal/preparation` may read `orders`, `order_items`, `table_assignments`, and `tables` through its own SQL but must not write them or import `internal/sales` outside `_test.go` files.
- Queue reads use a read-only `REPEATABLE READ` transaction and require current `preparation.operate` authority.
- Queue responses contain only `observed_at` and `units`; do not add alert or correction placeholders.
- Queue units expose no prices, allocations, Checks, Payments, balances, Sales Shift money, or staff credentials.
- Active states are exactly `QUEUED`, `IN_PREPARATION`, and `READY`; order them by `queued_at, id`.
- Bulk requests accept 1 through 50 ids, select duplicate ids once at first occurrence, lock unique ids in UUID order, and return outcomes in first-selection order.
- Only `UNIT_NOT_FOUND` and `INVALID_TRANSITION` become per-unit failures. Every unexpected error rolls back the complete outer transaction.
- Current authority is checked before idempotency replay. Same-input replay returns the exact stored response.
- Do not add SSE, WebSockets, a notifier abstraction, a shared cross-slice executor, or a new dependency.
- Run `sqlc generate` after query or migration changes; never hand-edit `internal/database/sqlc/*.go`.
- Run `swag init -g cmd/api/main.go -o docs` after Swagger annotation changes; never hand-edit generated Swagger files.
- Integration tests require PostgreSQL and `TEST_DATABASE_URL=postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable`.
- Commit steps are conditional: execute them only when the user explicitly requests commits.

## File Map

| File | Responsibility |
| --- | --- |
| `internal/database/migrations/000012_add_preparation_queue_fields.sql` | Add and backfill `preparation_units.in_preparation_at` |
| `sql/queries/preparation.sql` | Preparation locks, transitions, observed time, active units, and current Table names |
| `internal/database/sqlc/models.go` | Generated nullable timestamp field |
| `internal/database/sqlc/preparation.sql.go` | Generated Preparation query methods and row types |
| `internal/database/sqlc/querier.go` | Generated query interface additions |
| `internal/preparation/domain.go` | Operation/outcome constants and existing transition graph |
| `internal/preparation/dto.go` | Single-unit, queue, and bulk request/response contracts |
| `internal/preparation/executor.go` | Read executor, denial audit, mutation savepoint primitive |
| `internal/preparation/advance.go` | Shared single-unit transition helper and existing handler |
| `internal/preparation/queue.go` | Active queue projection and row mapping |
| `internal/preparation/bulk_advance.go` | Bulk normalization, partial outcomes, savepoint orchestration, batch audits |
| `internal/preparation/routes.go` | Wire queue, single advance, and bulk advance handlers |
| `internal/preparation/http.go` | Echo handlers, validation, and Swagger annotations |
| `internal/preparation/schema_integration_test.go` | Migration/schema contract |
| `internal/preparation/executor_integration_test.go` | Read transaction and denial behavior |
| `internal/preparation/queue_integration_test.go` | Queue ordering, projection, privacy inputs, and current facts |
| `internal/preparation/bulk_advance_test.go` | Pure bulk normalization and validation tests |
| `internal/preparation/bulk_advance_integration_test.go` | Partial success, idempotency, audit, rollback, and concurrency |
| `internal/preparation/preparation_integration_test.go` | Authenticated HTTP surface and JSON privacy contract |
| `internal/preparation/env_integration_test.go` | Shared Phase 6A fixtures and direct-handler helpers |
| `internal/preparation/advance_integration_test.go` | Single-transition timestamp regression coverage |
| `docs/docs.go`, `docs/swagger.json`, `docs/swagger.yaml` | Generated OpenAPI artifacts |
| `spec/decisions.md` | ADR-032 through ADR-035 |
| `MIGRATE_PLAN.md` | Mark 6A complete and preserve 6B/6C boundaries |

---

### Task 1: Schema And Generated Query Foundation

**Files:**
- Create: `internal/database/migrations/000012_add_preparation_queue_fields.sql`
- Modify: `sql/queries/preparation.sql`
- Modify: `internal/preparation/advance.go`
- Regenerate: `internal/database/sqlc/models.go`
- Regenerate: `internal/database/sqlc/preparation.sql.go`
- Regenerate: `internal/database/sqlc/querier.go`
- Create: `internal/preparation/schema_integration_test.go`

**Interfaces:**
- Produces: nullable `preparation_units.in_preparation_at`
- Produces: `GetPreparationCurrentTime(ctx) (time.Time, error)`
- Produces: `ListActivePreparationUnits(ctx) ([]sqlc.ListActivePreparationUnitsRow, error)`
- Produces: `ListCurrentPreparationTables(ctx, []uuid.UUID) ([]sqlc.ListCurrentPreparationTablesRow, error)`
- Changes: `SetPreparationUnitStateParams` gains `OccurredAt time.Time`
- Changes: `GetPreparationUnit` and `LockPreparationUnit` include `InPreparationAt sql.NullTime`

- [ ] **Step 1: Write the failing schema integration test**

Create `internal/preparation/schema_integration_test.go`:

```go
//go:build integration

package preparation_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/preparation"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestPreparationQueueSchema(t *testing.T) {
	db, _ := openPrepTestDB(t)

	var dataType, nullable string
	err := db.QueryRow(`
		SELECT data_type, is_nullable
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = 'preparation_units'
		  AND column_name = 'in_preparation_at'`).Scan(&dataType, &nullable)
	require.NoError(t, err)
	require.Equal(t, "timestamp with time zone", dataType)
	require.Equal(t, "YES", nullable)
}

func TestPreparationQueueMigrationBackfillsFirstStart(t *testing.T) {
	env := newPrepEnv(t)
	units := env.SubmittedUnits(t, 2)
	unit := units[0]
	before := time.Now()
	_, _, err := env.Advance(t, unit.ID, preparation.StateInPreparation)
	after := time.Now()
	require.NoError(t, err)
	var initiallyWritten time.Time
	require.NoError(t, env.DB.QueryRow(
		`SELECT in_preparation_at FROM preparation_units WHERE id = $1`, unit.ID,
	).Scan(&initiallyWritten))
	require.False(t, initiallyWritten.Before(before))
	require.False(t, initiallyWritten.After(after))

	earlier := before.Add(-2 * time.Hour)
	later := before.Add(-time.Hour)
	_, err = env.DB.Exec(`
		UPDATE preparation_unit_transitions
		SET occurred_at = $2
		WHERE preparation_unit_id = $1`, unit.ID, later)
	require.NoError(t, err)
	_, err = env.DB.Exec(`
		INSERT INTO preparation_unit_transitions (
			preparation_unit_id, prior_state, resulting_state,
			actor_staff_identity_id, staff_access_session_id, occurred_at
		) VALUES ($1, 'QUEUED', 'IN_PREPARATION', $2, $3, $4)`,
		unit.ID, env.barista.StaffID, env.barista.SessionID, earlier)
	require.NoError(t, err)

	_, err = env.DB.Exec(
		`UPDATE preparation_units SET in_preparation_at = NULL WHERE id = ANY($1)`,
		pq.Array([]uuid.UUID{unit.ID, units[1].ID}),
	)
	require.NoError(t, err)
	migration, err := os.ReadFile(filepath.Join(
		"..", "database", "migrations", "000012_add_preparation_queue_fields.sql",
	))
	require.NoError(t, err)
	_, err = env.DB.Exec(string(migration))
	require.NoError(t, err)

	var got, want time.Time
	require.NoError(t, env.DB.QueryRow(
		`SELECT in_preparation_at FROM preparation_units WHERE id = $1`, unit.ID,
	).Scan(&got))
	require.NoError(t, env.DB.QueryRow(`
		SELECT min(occurred_at)
		FROM preparation_unit_transitions
		WHERE preparation_unit_id = $1 AND resulting_state = 'IN_PREPARATION'`, unit.ID,
	).Scan(&want))
	require.Equal(t, want, got)
	var neverStartedIsNull bool
	require.NoError(t, env.DB.QueryRow(
		`SELECT in_preparation_at IS NULL FROM preparation_units WHERE id = $1`, units[1].ID,
	).Scan(&neverStartedIsNull))
	require.True(t, neverStartedIsNull)
}
```

- [ ] **Step 2: Run the schema test to verify it fails**

Run in PowerShell:

```powershell
$env:TEST_DATABASE_URL = "postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable"
go test -count=1 -tags=integration ./internal/preparation -run TestPreparationQueue -v
```

Expected: FAIL because `in_preparation_at` and the migration file are absent. After Task 1 generation, the backfill test executes the actual migration file rather than a copied CTE.

- [ ] **Step 3: Add the additive migration with historical backfill**

Create `internal/database/migrations/000012_add_preparation_queue_fields.sql`:

```sql
-- Phase 6A: server-authoritative Preparation Queue aging.
ALTER TABLE preparation_units
    ADD COLUMN IF NOT EXISTS in_preparation_at TIMESTAMPTZ;

WITH first_start AS (
    SELECT DISTINCT ON (preparation_unit_id)
           preparation_unit_id,
           occurred_at
    FROM preparation_unit_transitions
    WHERE resulting_state = 'IN_PREPARATION'
    ORDER BY preparation_unit_id, occurred_at, id
)
UPDATE preparation_units AS pu
SET in_preparation_at = first_start.occurred_at
FROM first_start
WHERE pu.id = first_start.preparation_unit_id
  AND pu.in_preparation_at IS NULL;
```

- [ ] **Step 4: Replace the Preparation SQL with timestamp-aware and queue queries**

Keep the ADR-024 header in `sql/queries/preparation.sql`. Add `in_preparation_at` to both existing unit selects and replace the state update with:

```sql
-- name: SetPreparationUnitState :exec
UPDATE preparation_units
SET state = sqlc.arg(state),
    in_preparation_at = CASE
        WHEN sqlc.arg(state)::text = 'IN_PREPARATION'
            THEN sqlc.arg(occurred_at)::timestamptz
        ELSE in_preparation_at
    END
WHERE id = sqlc.arg(id);
```

Append these reads:

```sql
-- name: GetPreparationCurrentTime :one
SELECT clock_timestamp()::timestamptz AS current_time;

-- name: ListActivePreparationUnits :many
WITH unit_counts AS (
    SELECT order_item_id, count(*)::integer AS unit_count
    FROM preparation_units
    GROUP BY order_item_id
)
SELECT pu.id,
       pu.order_item_id,
       pu.unit_number,
       pu.state,
       pu.service_number,
       pu.category_name,
       pu.item_name,
       pu.size_name,
       pu.modifiers,
       pu.preparation_note,
       pu.queued_at,
       pu.in_preparation_at,
       o.service_session_id,
       uc.unit_count AS order_item_unit_count
FROM preparation_units AS pu
JOIN order_items AS oi ON oi.id = pu.order_item_id
JOIN orders AS o ON o.id = oi.order_id
JOIN unit_counts AS uc ON uc.order_item_id = pu.order_item_id
WHERE pu.state IN ('QUEUED', 'IN_PREPARATION', 'READY')
ORDER BY pu.queued_at, pu.id;

-- name: ListCurrentPreparationTables :many
SELECT ta.service_session_id, t.name
FROM table_assignments AS ta
JOIN tables AS t ON t.id = ta.table_id
WHERE ta.service_session_id = ANY(sqlc.arg(service_session_ids)::uuid[])
  AND ta.released_at IS NULL
ORDER BY ta.service_session_id, ta.sequence, ta.id;
```

- [ ] **Step 5: Pass the occurrence time from the existing single-unit handler**

Before extracting the helper in Task 2, keep the existing handler correct against the new query signature by changing its update to:

```go
occurredAt, err := q.GetPreparationCurrentTime(ctx)
if err != nil {
	return 0, zero, AuditRecord{}, fmt.Errorf("read preparation occurrence time: %w", err)
}
if err := q.SetPreparationUnitState(ctx, sqlc.SetPreparationUnitStateParams{
	ID: unit.ID,
	State: cmd.TargetState,
	OccurredAt: occurredAt,
}); err != nil {
	return 0, zero, AuditRecord{}, fmt.Errorf("set preparation unit state: %w", err)
}
```

Reuse that same `occurredAt` for `InsertPreparationUnitTransition`. This prevents the keyed generated parameter from silently using `time.Time{}` between Tasks 1 and 2.

- [ ] **Step 6: Regenerate sqlc and inspect the generated API**

Run:

```powershell
sqlc generate
gofmt -w internal/database/sqlc
git diff -- internal/database/sqlc/models.go internal/database/sqlc/preparation.sql.go internal/database/sqlc/querier.go
```

Expected generated fields and methods must match the Interfaces block. If sqlc chooses an anonymous parameter name instead of `ServiceSessionIds`, correct the SQL with `sqlc.arg(service_session_ids)` and regenerate; do not rename generated code manually.

- [ ] **Step 7: Run the schema and compile checks**

Run:

```powershell
$env:TEST_DATABASE_URL = "postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable"
go test -count=1 -tags=integration ./internal/preparation -run TestPreparationQueue -v
go test ./internal/preparation
go vet ./internal/preparation
```

Expected: all PASS.

- [ ] **Step 8: Commit if explicitly requested**

```powershell
git add internal/database/migrations/000012_add_preparation_queue_fields.sql sql/queries/preparation.sql internal/database/sqlc internal/preparation/advance.go internal/preparation/schema_integration_test.go
git commit -m "feat(preparation): add queue schema and queries"
```

---

### Task 2: One Shared Timestamp-Aware Transition

**Files:**
- Modify: `internal/preparation/dto.go`
- Modify: `internal/preparation/advance.go`
- Modify: `internal/preparation/advance_integration_test.go`

**Interfaces:**
- Produces: `UnitResponse.InPreparationAt *time.Time`
- Produces: `transitionOutcome{Unit UnitResponse, Audit AuditRecord}`
- Produces: `applyAdvance(ctx context.Context, q *sqlc.Queries, actor Actor, unitID uuid.UUID, target string) (transitionOutcome, error)`
- Consumes: timestamp-aware `SetPreparationUnitState` from Task 1

- [ ] **Step 1: Add the failing start-timestamp regression test**

In the full-chain subtest in `internal/preparation/advance_integration_test.go`, replace the loop with explicit assertions:

```go
started, _, err := env.Advance(t, unit.ID, preparation.StateInPreparation)
require.NoError(t, err)
require.NotNil(t, started.InPreparationAt)
startedAt := *started.InPreparationAt

ready, _, err := env.Advance(t, unit.ID, preparation.StateReady)
require.NoError(t, err)
require.NotNil(t, ready.InPreparationAt)
require.Equal(t, startedAt, *ready.InPreparationAt)

fulfilled, _, err := env.Advance(t, unit.ID, preparation.StateFulfilled)
require.NoError(t, err)
require.NotNil(t, fulfilled.InPreparationAt)
require.Equal(t, startedAt, *fulfilled.InPreparationAt)
require.Equal(t, preparation.StateFulfilled, fulfilled.State)
```

- [ ] **Step 2: Run the targeted test to verify it fails**

Run:

```powershell
$env:TEST_DATABASE_URL = "postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable"
go test -count=1 -tags=integration ./internal/preparation -run 'TestAdvanceUnit/the_full_chain_reaches_fulfilled' -v
```

Expected: compile FAIL because `UnitResponse.InPreparationAt` does not exist.

- [ ] **Step 3: Extend the unit DTO and mapper**

Add to `UnitResponse` in `internal/preparation/dto.go`:

```go
InPreparationAt *time.Time `json:"in_preparation_at"`
```

In `loadUnit`, map the generated nullable timestamp explicitly:

```go
var inPreparationAt *time.Time
if row.InPreparationAt.Valid {
	value := row.InPreparationAt.Time
	inPreparationAt = &value
}
```

Set `InPreparationAt: inPreparationAt` in the returned `UnitResponse`.

- [ ] **Step 4: Extract the shared transition helper**

In `internal/preparation/advance.go`, add:

```go
type transitionOutcome struct {
	Unit  UnitResponse
	Audit AuditRecord
}

func applyAdvance(ctx context.Context, q *sqlc.Queries, actor Actor,
	unitID uuid.UUID, target string,
) (transitionOutcome, error) {
	var zero transitionOutcome
	if !IsAdvanceTarget(target) {
		return zero, fmt.Errorf("%w: %q is not an advance target", ErrInvalidTransition, target)
	}

	unit, err := q.LockPreparationUnit(ctx, unitID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return zero, fmt.Errorf("%w: %s", ErrUnitNotFound, unitID)
		}
		return zero, fmt.Errorf("lock preparation unit: %w", err)
	}
	if !IsLegalAdvance(unit.State, target) {
		return zero, fmt.Errorf("%w: %s cannot advance to %s", ErrInvalidTransition, unit.State, target)
	}

	occurredAt, err := q.GetPreparationCurrentTime(ctx)
	if err != nil {
		return zero, fmt.Errorf("read preparation occurrence time: %w", err)
	}
	if err := q.SetPreparationUnitState(ctx, sqlc.SetPreparationUnitStateParams{
		ID: unit.ID, State: target, OccurredAt: occurredAt,
	}); err != nil {
		return zero, fmt.Errorf("set preparation unit state: %w", err)
	}
	if err := q.InsertPreparationUnitTransition(ctx, sqlc.InsertPreparationUnitTransitionParams{
		PreparationUnitID: unit.ID,
		PriorState: unit.State,
		ResultingState: target,
		ActorStaffIdentityID: actor.StaffID,
		StaffAccessSessionID: actor.SessionID,
		OccurredAt: occurredAt,
	}); err != nil {
		return zero, fmt.Errorf("insert preparation unit transition: %w", err)
	}

	out, err := loadUnit(ctx, q, unit.ID)
	if err != nil {
		return zero, err
	}
	return transitionOutcome{
		Unit: out,
		Audit: AuditRecord{
			EventType: EventPreparationUnitAdvanced,
			Details: map[string]any{
				"preparation_unit_id": unit.ID,
				"prior_state": unit.State,
				"resulting_state": target,
			},
		},
	}, nil
}
```

Replace the body-specific transition logic in `AdvanceUnitHandler.Handle` with:

```go
outcome, err := applyAdvance(ctx, mc.Queries, actor, cmd.UnitID, cmd.TargetState)
if err != nil {
	return 0, UnitResponse{}, AuditRecord{}, err
}
return http.StatusOK, outcome.Unit, outcome.Audit, nil
```

- [ ] **Step 5: Run the Preparation regression suite**

Run:

```powershell
gofmt -w internal/preparation
$env:TEST_DATABASE_URL = "postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable"
go test -count=1 -tags=integration ./internal/preparation -run 'TestAdvanceUnit|TestConcurrentAdvance' -v
go test ./internal/preparation
```

Expected: all PASS; the timestamp remains unchanged after `READY` and `FULFILLED`.

- [ ] **Step 6: Commit if explicitly requested**

```powershell
git add internal/preparation/dto.go internal/preparation/advance.go internal/preparation/advance_integration_test.go
git commit -m "refactor(preparation): share timestamp-aware transition"
```

---

### Task 3: Authorized Repeatable-Read Executor

**Files:**
- Modify: `internal/preparation/domain.go`
- Modify: `internal/preparation/executor.go`
- Create: `internal/preparation/executor_integration_test.go`
- Modify: `internal/preparation/env_integration_test.go`

**Interfaces:**
- Produces: `OpReadActiveQueue = "preparation.read_active_queue"`
- Produces: `ExecuteRead[T any](ctx context.Context, r *Runner, actor Actor, operation, requiredCapability string, fn func(*sqlc.Queries) (T, error)) (T, error)`
- Produces: `(*Runner).auditReadDenial(...)` as best-effort evidence that never replaces the original denial
- Consumes: existing `reloadAuthority`, `verifyCapabilities`, `recordDenial`, and `finishDenial`

- [ ] **Step 1: Add fixture support for denial-audit assertions**

Append to `internal/preparation/env_integration_test.go`:

```go
func (e *prepEnv) CountAuditEvents(t *testing.T, eventType string) int {
	t.Helper()
	var n int
	require.NoError(t, e.DB.QueryRow(
		`SELECT count(*) FROM audit_events WHERE event_type = $1`, eventType,
	).Scan(&n))
	return n
}
```

- [ ] **Step 2: Write failing read-executor integration tests**

Create `internal/preparation/executor_integration_test.go`:

```go
//go:build integration

package preparation_test

import (
	"context"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/preparation"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestExecuteReadRequiresCapabilityAndAuditsDenial(t *testing.T) {
	env := newPrepEnv(t)

	_, err := preparation.ExecuteRead(
		context.Background(), env.PreparationRunner, env.CashierActor(),
		preparation.OpReadActiveQueue, preparation.CapPreparationOperate,
		func(*sqlc.Queries) (int, error) {
			t.Fatal("read body must not run for a cashier")
			return 0, nil
		},
	)
	require.ErrorIs(t, err, preparation.ErrForbidden)
	require.Equal(t, 1, env.CountAuditEvents(t, preparation.EventAuthorizationDenied))
}

func TestExecuteReadUsesReadOnlyTransaction(t *testing.T) {
	env := newPrepEnv(t)
	unit := env.SubmittedUnits(t, 1)[0]

	_, err := preparation.ExecuteRead(
		context.Background(), env.PreparationRunner, env.BaristaActor(),
		preparation.OpReadActiveQueue, preparation.CapPreparationOperate,
		func(q *sqlc.Queries) (int, error) {
			err := q.SetPreparationUnitState(context.Background(), sqlc.SetPreparationUnitStateParams{
				ID: unit.ID,
				State: preparation.StateInPreparation,
				OccurredAt: unit.QueuedAt,
			})
			return 0, err
		},
	)
	require.Error(t, err)
	require.Equal(t, preparation.StateQueued, env.UnitState(t, unit.ID))
}

func TestExecuteReadReloadsCurrentAuthority(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*prepEnv) error
		want   error
	}{
		{"locked", func(e *prepEnv) error {
			_, err := e.DB.Exec(`UPDATE staff_access_sessions SET state = 'locked' WHERE id = $1`, e.barista.SessionID)
			return err
		}, preparation.ErrUnauthorized},
		{"expired", func(e *prepEnv) error {
			_, err := e.DB.Exec(`UPDATE staff_access_sessions SET expires_at = now() - interval '1 minute' WHERE id = $1`, e.barista.SessionID)
			return err
		}, preparation.ErrUnauthorized},
		{"revoked", func(e *prepEnv) error {
			_, err := e.DB.Exec(`UPDATE staff_access_sessions SET revoked_at = now() WHERE id = $1`, e.barista.SessionID)
			return err
		}, preparation.ErrUnauthorized},
		{"disabled", func(e *prepEnv) error {
			_, err := e.DB.Exec(`UPDATE staff_identities SET enabled = false WHERE id = $1`, e.barista.StaffID)
			return err
		}, preparation.ErrForbidden},
		{"role revoked", func(e *prepEnv) error {
			_, err := e.DB.Exec(`DELETE FROM staff_identity_roles WHERE staff_identity_id = $1`, e.barista.StaffID)
			return err
		}, preparation.ErrForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newPrepEnv(t)
			require.NoError(t, tt.mutate(env))
			_, err := preparation.ExecuteRead(
				context.Background(), env.PreparationRunner, env.BaristaActor(),
				preparation.OpReadActiveQueue, preparation.CapPreparationOperate,
				func(*sqlc.Queries) (int, error) {
					t.Fatal("read body must not run after authority is removed")
					return 0, nil
				},
			)
			require.ErrorIs(t, err, tt.want)
			require.Equal(t, 1, env.CountAuditEvents(t, preparation.EventAuthorizationDenied))
		})
	}
}

func TestExecuteReadKeepsOneRepeatableSnapshot(t *testing.T) {
	env := newPrepEnv(t)
	unit := env.SubmittedUnits(t, 1)[0]
	var sessionID uuid.UUID
	require.NoError(t, env.DB.QueryRow(`
		SELECT o.service_session_id
		FROM preparation_units pu
		JOIN order_items oi ON oi.id = pu.order_item_id
		JOIN orders o ON o.id = oi.order_id
		WHERE pu.id = $1`, unit.ID).Scan(&sessionID))
	startUpdate := make(chan struct{})
	updated := make(chan error, 1)
	go func() {
		<-startUpdate
		_, err := env.DB.Exec(`UPDATE tables SET name = 'Bàn 2' WHERE id = $1`, env.TableID)
		updated <- err
	}()

	name, err := preparation.ExecuteRead(
		context.Background(), env.PreparationRunner, env.BaristaActor(),
		preparation.OpReadActiveQueue, preparation.CapPreparationOperate,
		func(q *sqlc.Queries) (string, error) {
			if _, err := q.GetPreparationCurrentTime(context.Background()); err != nil {
				return "", err
			}
			close(startUpdate)
			if err := <-updated; err != nil {
				return "", err
			}
			rows, err := q.ListCurrentPreparationTables(
				context.Background(), []uuid.UUID{sessionID},
			)
			if err != nil {
				return "", err
			}
			return rows[0].Name, nil
		},
	)
	require.NoError(t, err)
	require.Equal(t, "Bàn 1", name)

	var currentName string
	require.NoError(t, env.DB.QueryRow(`SELECT name FROM tables WHERE id = $1`, env.TableID).
		Scan(&currentName))
	require.Equal(t, "Bàn 2", currentName)
}
```

Add this failure-path test; it proves missing denial evidence never changes the client-visible authorization result:

```go
func TestExecuteReadAuditFailurePreservesDenial(t *testing.T) {
	env := newPrepEnv(t)
	_, err := env.DB.Exec(`
		CREATE FUNCTION fail_preparation_denial_audit() RETURNS trigger AS $$
		BEGIN
			IF NEW.event_type = 'preparation.authorization_denied' THEN
				RAISE EXCEPTION 'forced denial audit failure';
			END IF;
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql;
		CREATE TRIGGER fail_preparation_denial_audit
		BEFORE INSERT ON audit_events
		FOR EACH ROW EXECUTE FUNCTION fail_preparation_denial_audit();`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = env.DB.Exec(`DROP TRIGGER IF EXISTS fail_preparation_denial_audit ON audit_events`)
		_, _ = env.DB.Exec(`DROP FUNCTION IF EXISTS fail_preparation_denial_audit()`)
	})

	_, err = preparation.ExecuteRead(
		context.Background(), env.PreparationRunner, env.CashierActor(),
		preparation.OpReadActiveQueue, preparation.CapPreparationOperate,
		func(*sqlc.Queries) (int, error) {
			t.Fatal("denied read body must not run")
			return 0, nil
		},
	)
	require.ErrorIs(t, err, preparation.ErrForbidden)
}
```

Also expose the existing Barista fixture:

```go
func (e *prepEnv) BaristaActor() preparation.Actor { return e.barista }
```

- [ ] **Step 3: Run the tests to verify they fail**

Run:

```powershell
$env:TEST_DATABASE_URL = "postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable"
go test -count=1 -tags=integration ./internal/preparation -run TestExecuteRead -v
```

Expected: compile FAIL because `OpReadActiveQueue` and `ExecuteRead` do not exist.

- [ ] **Step 4: Add the operation constant and read executor**

Add to `internal/preparation/domain.go`:

```go
const OpReadActiveQueue = "preparation.read_active_queue"
```

Add to `internal/preparation/executor.go`:

```go
func (r *Runner) auditReadDenial(ctx context.Context, actor Actor,
	authority sqlc.GetSalesSessionAuthorityRow, operation string, denialErr error,
) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		slog.Error("begin read-denial audit", "operation", operation, "error", err)
		return
	}
	defer tx.Rollback() //nolint:errcheck
	outcome := recordDenial(ctx, r.queries.WithTx(tx), actor, authority, operation, denialErr)
	if !outcome.committed {
		return
	}
	if err := tx.Commit(); err != nil {
		slog.Error("commit read-denial audit", "operation", operation, "error", err)
	}
}

func ExecuteRead[T any](ctx context.Context, r *Runner, actor Actor,
	operation, requiredCapability string,
	fn func(*sqlc.Queries) (T, error),
) (T, error) {
	var zero T
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{
		ReadOnly: true,
		Isolation: sql.LevelRepeatableRead,
	})
	if err != nil {
		return zero, fmt.Errorf("begin read transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	q := r.queries.WithTx(tx)
	authority, caps, err := reloadAuthority(ctx, q, actor)
	if err != nil {
		if isSecurityDenial(err) {
			_ = tx.Rollback()
			r.auditReadDenial(ctx, actor, authority, operation, err)
		}
		return zero, err
	}
	if err := verifyCapabilities([]string{requiredCapability}, caps); err != nil {
		_ = tx.Rollback()
		r.auditReadDenial(ctx, actor, authority, operation, err)
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

- [ ] **Step 5: Run read-executor and regression tests**

Run:

```powershell
gofmt -w internal/preparation
$env:TEST_DATABASE_URL = "postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable"
go test -count=1 -tags=integration ./internal/preparation -run TestExecuteRead -v
go test ./internal/preparation
go vet ./internal/preparation
```

Expected: all PASS.

- [ ] **Step 6: Commit if explicitly requested**

```powershell
git add internal/preparation/domain.go internal/preparation/executor.go internal/preparation/executor_integration_test.go internal/preparation/env_integration_test.go
git commit -m "feat(preparation): add authorized read executor"
```

---

### Task 4: Active Preparation Queue Projection

**Files:**
- Modify: `internal/preparation/dto.go`
- Create: `internal/preparation/queue.go`
- Create: `internal/preparation/queue_integration_test.go`
- Modify: `internal/preparation/env_integration_test.go`

**Interfaces:**
- Produces: `QueueUnitResponse`
- Produces: `QueueResponse{ObservedAt time.Time, Units []QueueUnitResponse}`
- Produces: `ActiveQueueHandler`, `NewActiveQueueHandler(runner *Runner)`, and `Handle(ctx, actor) (QueueResponse, error)`
- Consumes: Task 1 queue queries and Task 3 `ExecuteRead`

- [ ] **Step 1: Add fixture helpers for queue facts**

Append these helpers to `internal/preparation/env_integration_test.go`:

```go
func (e *prepEnv) SessionIDForUnit(t *testing.T, unitID uuid.UUID) uuid.UUID {
	t.Helper()
	var sessionID uuid.UUID
	require.NoError(t, e.DB.QueryRow(`
		SELECT o.service_session_id
		FROM preparation_units pu
		JOIN order_items oi ON oi.id = pu.order_item_id
		JOIN orders o ON o.id = oi.order_id
		WHERE pu.id = $1`, unitID).Scan(&sessionID))
	return sessionID
}

func (e *prepEnv) SeedTable(t *testing.T, name string) uuid.UUID {
	t.Helper()
	return seedTable(t, e.DB, name)
}

func (e *prepEnv) SetTables(t *testing.T, sessionID uuid.UUID, tableIDs ...uuid.UUID) {
	t.Helper()
	_, _, err := sales.NewSetSessionTablesHandler(e.SalesRunner).Handle(
		context.Background(), e.salesActor(e.manager), sales.SetSessionTablesCommand{
			RequestID: uuid.New(), ServiceSessionID: sessionID, TableIDs: tableIDs,
		},
	)
	require.NoError(t, err)
}
```

- [ ] **Step 2: Write failing queue integration tests**

Create `internal/preparation/queue_integration_test.go` with these tests:

```go
//go:build integration

package preparation_test

import (
	"context"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/preparation"
	"github.com/stretchr/testify/require"
)

func TestActiveQueueReturnsEmptySliceAndDatabaseTime(t *testing.T) {
	env := newPrepEnv(t)
	before := time.Now()
	got, err := preparation.NewActiveQueueHandler(env.PreparationRunner).
		Handle(context.Background(), env.BaristaActor())
	after := time.Now()
	require.NoError(t, err)
	require.NotNil(t, got.Units)
	require.Empty(t, got.Units)
	require.False(t, got.ObservedAt.Before(before))
	require.False(t, got.ObservedAt.After(after))
}

func TestActiveQueueOrdersFIFOAndExcludesTerminalUnits(t *testing.T) {
	env := newPrepEnv(t)
	units := env.SubmittedUnits(t, 3)
	_, _, err := env.Advance(t, units[0].ID, preparation.StateInPreparation)
	require.NoError(t, err)
	_, _, err = env.Advance(t, units[0].ID, preparation.StateReady)
	require.NoError(t, err)
	_, _, err = env.Advance(t, units[0].ID, preparation.StateFulfilled)
	require.NoError(t, err)

	got, err := preparation.NewActiveQueueHandler(env.PreparationRunner).
		Handle(context.Background(), env.BaristaActor())
	require.NoError(t, err)
	require.Len(t, got.Units, 2)
	require.Equal(t, units[1].ID, got.Units[0].ID)
	require.Equal(t, units[2].ID, got.Units[1].ID)
	require.Equal(t, int32(3), got.Units[0].OrderItemUnitCount)
}

func TestActiveQueueProjectsCurrentTables(t *testing.T) {
	env := newPrepEnv(t)
	unit := env.SubmittedUnits(t, 1)[0]
	sessionID := env.SessionIDForUnit(t, unit.ID)
	secondTable := env.SeedTable(t, "Bàn 2")
	env.SetTables(t, sessionID, secondTable)

	got, err := preparation.NewActiveQueueHandler(env.PreparationRunner).
		Handle(context.Background(), env.BaristaActor())
	require.NoError(t, err)
	require.Equal(t, []string{"Bàn 2"}, got.Units[0].TableNames)
}

func TestActiveQueueDeniesCashier(t *testing.T) {
	env := newPrepEnv(t)
	_, err := preparation.NewActiveQueueHandler(env.PreparationRunner).
		Handle(context.Background(), env.CashierActor())
	require.ErrorIs(t, err, preparation.ErrForbidden)
}
```

Add these remaining cases before implementation:

- `TestActiveQueueUsesIDAsEqualTimestampTieBreak`: submit three units, set all three `queued_at` values to one timestamp with direct SQL, sort their ids with `bytes.Compare`, and assert the queue ids match that order.
- `TestActiveQueueIncludesEveryActiveStateAndStartTime`: leave one unit `QUEUED`, move one to `IN_PREPARATION`, move one to `READY`, and assert all three appear; the latter two retain non-nil equal-to-transition `in_preparation_at` values.
- `TestActiveQueueTakeawayHasEmptyTables`: add `SubmittedTakeawayUnits` beside `SubmittedUnits`, using the real Start Takeaway, Add Item, Commit, Cash Payment, and Submit handlers; assert `table_names` is a non-nil empty slice.
- Extend `TestActiveQueueProjectsCurrentTables` to read once before reassignment and once after, proving `Bàn 1` changes to `Bàn 2` without updating the Preparation Unit snapshot row.

- [ ] **Step 3: Run the queue tests to verify they fail**

Run:

```powershell
$env:TEST_DATABASE_URL = "postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable"
go test -count=1 -tags=integration ./internal/preparation -run TestActiveQueue -v
```

Expected: compile FAIL because queue DTOs and `ActiveQueueHandler` do not exist.

- [ ] **Step 4: Add explicit queue DTOs**

Add to `internal/preparation/dto.go`:

```go
type QueueUnitResponse struct {
	ID                 uuid.UUID              `json:"id"`
	OrderItemID        uuid.UUID              `json:"order_item_id"`
	OrderItemUnitCount int32                  `json:"order_item_unit_count"`
	UnitNumber         int32                  `json:"unit_number"`
	State              string                 `json:"state"`
	ServiceNumber      string                 `json:"service_number"`
	TableNames         []string               `json:"table_names"`
	CategoryName       string                 `json:"category_name"`
	ItemName           string                 `json:"item_name"`
	SizeName           *string                `json:"size_name"`
	Modifiers          []UnitModifierResponse `json:"modifiers"`
	PreparationNote    *string                `json:"preparation_note"`
	QueuedAt           time.Time              `json:"queued_at"`
	InPreparationAt    *time.Time             `json:"in_preparation_at"`
}

type QueueResponse struct {
	ObservedAt time.Time           `json:"observed_at"`
	Units      []QueueUnitResponse `json:"units"`
}
```

- [ ] **Step 5: Implement the set-based projection**

Create `internal/preparation/queue.go`. Use one helper to decode modifiers for both `queue.go` and `advance.go`:

```go
func decodeModifiers(raw json.RawMessage) ([]UnitModifierResponse, error) {
	modifiers := make([]UnitModifierResponse, 0)
	if len(raw) == 0 {
		return modifiers, nil
	}
	if err := json.Unmarshal(raw, &modifiers); err != nil {
		return nil, fmt.Errorf("decode preparation unit modifiers: %w", err)
	}
	return modifiers, nil
}
```

Implement the handler with this shape:

```go
type ActiveQueueHandler struct{ runner *Runner }

func NewActiveQueueHandler(runner *Runner) *ActiveQueueHandler {
	return &ActiveQueueHandler{runner: runner}
}

func (h *ActiveQueueHandler) Handle(ctx context.Context, actor Actor) (QueueResponse, error) {
	return ExecuteRead(ctx, h.runner, actor, OpReadActiveQueue, CapPreparationOperate,
		func(q *sqlc.Queries) (QueueResponse, error) {
			observedAt, err := q.GetPreparationCurrentTime(ctx)
			if err != nil {
				return QueueResponse{}, fmt.Errorf("read preparation observed time: %w", err)
			}
			rows, err := q.ListActivePreparationUnits(ctx)
			if err != nil {
				return QueueResponse{}, fmt.Errorf("list active preparation units: %w", err)
			}

			sessionIDs := make([]uuid.UUID, 0, len(rows))
			seenSessions := make(map[uuid.UUID]struct{}, len(rows))
			for _, row := range rows {
				if row.QueuedAt.After(observedAt) {
					observedAt = row.QueuedAt
				}
				if row.InPreparationAt.Valid && row.InPreparationAt.Time.After(observedAt) {
					observedAt = row.InPreparationAt.Time
				}
				if _, ok := seenSessions[row.ServiceSessionID]; !ok {
					seenSessions[row.ServiceSessionID] = struct{}{}
					sessionIDs = append(sessionIDs, row.ServiceSessionID)
				}
			}
			tablesBySession := make(map[uuid.UUID][]string, len(sessionIDs))
			if len(sessionIDs) > 0 {
				tableRows, err := q.ListCurrentPreparationTables(ctx, sessionIDs)
				if err != nil {
					return QueueResponse{}, fmt.Errorf("list current preparation tables: %w", err)
				}
				for _, row := range tableRows {
					tablesBySession[row.ServiceSessionID] = append(
						tablesBySession[row.ServiceSessionID], row.Name,
					)
				}
			}

			units := make([]QueueUnitResponse, 0, len(rows))
			for _, row := range rows {
				unit, err := queueUnitFromRow(row, tablesBySession[row.ServiceSessionID])
				if err != nil {
					return QueueResponse{}, err
				}
				units = append(units, unit)
			}
			return QueueResponse{ObservedAt: observedAt, Units: units}, nil
		})
}
```

Implement `queueUnitFromRow` explicitly; do not use reflection or JSON round-tripping:

```go
func queueUnitFromRow(row sqlc.ListActivePreparationUnitsRow,
	tableNames []string,
) (QueueUnitResponse, error) {
	modifiers, err := decodeModifiers(row.Modifiers)
	if err != nil {
		return QueueUnitResponse{}, err
	}
	tables := append([]string{}, tableNames...)
	var sizeName, preparationNote *string
	if row.SizeName.Valid {
		value := row.SizeName.String
		sizeName = &value
	}
	if row.PreparationNote.Valid {
		value := row.PreparationNote.String
		preparationNote = &value
	}
	var inPreparationAt *time.Time
	if row.InPreparationAt.Valid {
		value := row.InPreparationAt.Time
		inPreparationAt = &value
	}
	return QueueUnitResponse{
		ID: row.ID,
		OrderItemID: row.OrderItemID,
		OrderItemUnitCount: row.OrderItemUnitCount,
		UnitNumber: row.UnitNumber,
		State: row.State,
		ServiceNumber: row.ServiceNumber,
		TableNames: tables,
		CategoryName: row.CategoryName,
		ItemName: row.ItemName,
		SizeName: sizeName,
		Modifiers: modifiers,
		PreparationNote: preparationNote,
		QueuedAt: row.QueuedAt,
		InPreparationAt: inPreparationAt,
	}, nil
}
```

- [ ] **Step 6: Reuse modifier decoding in single-unit projection**

Replace the local `json.Unmarshal` block in `loadUnit` with:

```go
mods, err := decodeModifiers(row.Modifiers)
if err != nil {
	return UnitResponse{}, err
}
```

- [ ] **Step 7: Run queue and existing transition tests**

Run:

```powershell
gofmt -w internal/preparation
$env:TEST_DATABASE_URL = "postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable"
go test -count=1 -tags=integration ./internal/preparation -run 'TestActiveQueue|TestAdvanceUnit' -v
go test ./internal/preparation
```

Expected: all PASS.

- [ ] **Step 8: Commit if explicitly requested**

```powershell
git add internal/preparation/dto.go internal/preparation/queue.go internal/preparation/queue_integration_test.go internal/preparation/env_integration_test.go internal/preparation/advance.go
git commit -m "feat(preparation): add active queue projection"
```

---

### Task 5: Bulk Contract, Normalization, And Validation

**Files:**
- Modify: `internal/preparation/domain.go`
- Modify: `internal/preparation/dto.go`
- Create: `internal/preparation/bulk_advance.go`
- Create: `internal/preparation/bulk_advance_test.go`

**Interfaces:**
- Produces: `OpBulkAdvance = "preparation.bulk_advance"`
- Produces: `BulkAdvanceCommand`, `BulkAdvanceResponse`, and `BulkAdvanceOutcome`
- Produces: `normalizeBulkSelection(ids []uuid.UUID) []uuid.UUID`
- Produces: `sortedBulkSelection(ids []uuid.UUID) []uuid.UUID`
- Produces: `validateBulkAdvance(cmd BulkAdvanceCommand) error`
- Produces: `bulkAdvanceFingerprint{PreparationUnitIDs []uuid.UUID, TargetState string}`
- Consumes: `response.ErrInvalid` for top-level HTTP 400 validation

- [ ] **Step 1: Write the failing normalization and validation unit tests**

Create `internal/preparation/bulk_advance_test.go`:

```go
package preparation

import (
	"errors"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeBulkSelectionKeepsFirstOccurrence(t *testing.T) {
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	assert.Equal(t, []uuid.UUID{b, a, c}, normalizeBulkSelection(
		[]uuid.UUID{b, a, b, c, a},
	))
}

func TestSortedBulkSelectionUsesUUIDByteOrderWithoutMutatingInput(t *testing.T) {
	a := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	b := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	c := uuid.MustParse("10000000-0000-0000-0000-000000000000")
	input := []uuid.UUID{c, a, b}

	require.Equal(t, []uuid.UUID{b, a, c}, sortedBulkSelection(input))
	require.Equal(t, []uuid.UUID{c, a, b}, input)
}

func TestValidateBulkAdvance(t *testing.T) {
	ids50 := make([]uuid.UUID, 50)
	for i := range ids50 {
		ids50[i] = uuid.New()
	}

	tests := []struct {
		name string
		cmd  BulkAdvanceCommand
		want error
	}{
		{"one id", BulkAdvanceCommand{RequestID: uuid.New(), PreparationUnitIDs: ids50[:1], TargetState: StateReady}, nil},
		{"fifty ids", BulkAdvanceCommand{RequestID: uuid.New(), PreparationUnitIDs: ids50, TargetState: StateReady}, nil},
		{"zero request id", BulkAdvanceCommand{PreparationUnitIDs: ids50[:1], TargetState: StateReady}, response.ErrInvalid},
		{"empty ids", BulkAdvanceCommand{RequestID: uuid.New(), TargetState: StateReady}, response.ErrInvalid},
		{"fifty one ids", BulkAdvanceCommand{RequestID: uuid.New(), PreparationUnitIDs: append(ids50, uuid.New()), TargetState: StateReady}, response.ErrInvalid},
		{"zero unit id", BulkAdvanceCommand{RequestID: uuid.New(), PreparationUnitIDs: []uuid.UUID{uuid.Nil}, TargetState: StateReady}, response.ErrInvalid},
		{"invalid target", BulkAdvanceCommand{RequestID: uuid.New(), PreparationUnitIDs: ids50[:1], TargetState: StateQueued}, response.ErrInvalid},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBulkAdvance(tt.cmd)
			if tt.want == nil {
				require.NoError(t, err)
				return
			}
			require.True(t, errors.Is(err, tt.want), err)
		})
	}
}
```

- [ ] **Step 2: Run the unit tests to verify they fail**

Run:

```powershell
go test ./internal/preparation -run 'TestNormalizeBulkSelection|TestValidateBulkAdvance' -v
```

Expected: compile FAIL because the bulk contract and helpers do not exist.

- [ ] **Step 3: Add operation and outcome constants**

Add to `internal/preparation/domain.go`:

```go
const (
	OpBulkAdvance = "preparation.bulk_advance"

	BulkStatusAdvanced = "ADVANCED"
	BulkStatusFailed   = "FAILED"

	BulkCodeUnitNotFound      = "UNIT_NOT_FOUND"
	BulkCodeInvalidTransition = "INVALID_TRANSITION"
)
```

- [ ] **Step 4: Add the bulk DTOs**

Add to `internal/preparation/dto.go`:

```go
type BulkAdvanceCommand struct {
	RequestID          uuid.UUID   `json:"request_id"`
	PreparationUnitIDs []uuid.UUID `json:"preparation_unit_ids"`
	TargetState        string      `json:"target_state"`
}

type BulkAdvanceOutcome struct {
	PreparationUnitID uuid.UUID     `json:"preparation_unit_id"`
	Status            string        `json:"status"`
	Unit              *UnitResponse `json:"unit,omitempty"`
	Code              string        `json:"code,omitempty"`
}

type BulkAdvanceResponse struct {
	TargetState string               `json:"target_state"`
	Outcomes    []BulkAdvanceOutcome `json:"outcomes"`
}
```

- [ ] **Step 5: Add pure validation and first-occurrence normalization**

Start `internal/preparation/bulk_advance.go` with:

```go
type bulkAdvanceFingerprint struct {
	PreparationUnitIDs []uuid.UUID `json:"preparation_unit_ids"`
	TargetState        string      `json:"target_state"`
}

func normalizeBulkSelection(ids []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(ids))
	normalized := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		normalized = append(normalized, id)
	}
	return normalized
}

func sortedBulkSelection(ids []uuid.UUID) []uuid.UUID {
	sorted := append([]uuid.UUID(nil), ids...)
	slices.SortFunc(sorted, func(a, b uuid.UUID) int {
		return bytes.Compare(a[:], b[:])
	})
	return sorted
}

func validateBulkAdvance(cmd BulkAdvanceCommand) error {
	if cmd.RequestID == uuid.Nil {
		return fmt.Errorf("%w: request_id is required", response.ErrInvalid)
	}
	if len(cmd.PreparationUnitIDs) < 1 || len(cmd.PreparationUnitIDs) > 50 {
		return fmt.Errorf("%w: preparation_unit_ids must contain 1 through 50 ids", response.ErrInvalid)
	}
	for _, id := range cmd.PreparationUnitIDs {
		if id == uuid.Nil {
			return fmt.Errorf("%w: preparation_unit_ids must not contain a zero UUID", response.ErrInvalid)
		}
	}
	if !IsAdvanceTarget(cmd.TargetState) {
		return fmt.Errorf("%w: target_state must be IN_PREPARATION, READY, or FULFILLED", response.ErrInvalid)
	}
	return nil
}
```

Validation applies to the submitted list before duplicate normalization, so a request cannot bypass the 50-id boundary by repeating ids. The normalized first-occurrence list, not the raw list or UUID-sorted lock list, becomes the idempotency fingerprint.

- [ ] **Step 6: Run unit tests**

Run:

```powershell
gofmt -w internal/preparation
go test ./internal/preparation -run 'TestNormalizeBulkSelection|TestValidateBulkAdvance' -v
go test ./internal/preparation
```

Expected: all PASS.

- [ ] **Step 7: Commit if explicitly requested**

```powershell
git add internal/preparation/domain.go internal/preparation/dto.go internal/preparation/bulk_advance.go internal/preparation/bulk_advance_test.go
git commit -m "feat(preparation): define bulk advance contract"
```

---

### Task 6: Savepoint-Based Bulk Advance

**Files:**
- Modify: `internal/preparation/executor.go`
- Modify: `internal/preparation/bulk_advance.go`
- Create: `internal/preparation/bulk_advance_integration_test.go`
- Modify: `internal/preparation/env_integration_test.go`

**Interfaces:**
- Changes: `MutationContext` privately retains its `*sql.Tx`
- Produces: `MutationContext.withUnitSavepoint(ctx, fn) error`
- Produces: `BulkAdvanceHandler`, `NewBulkAdvanceHandler(runner *Runner)`, and `Handle(ctx, actor, cmd) (int, BulkAdvanceResponse, error)`
- Produces: `writeAdvanceAudits(ctx, q, actor, audits) error`
- Consumes: Task 2 `applyAdvance`, Task 5 normalization, and generated `InsertAuditEventsBatch`

- [ ] **Step 1: Add bulk fixture and assertion helpers**

Append to `internal/preparation/env_integration_test.go`:

```go
func (e *prepEnv) BulkAdvance(t *testing.T, cmd preparation.BulkAdvanceCommand) (int, preparation.BulkAdvanceResponse, error) {
	t.Helper()
	return preparation.NewBulkAdvanceHandler(e.PreparationRunner).
		Handle(context.Background(), e.BaristaActor(), cmd)
}

func (e *prepEnv) UnitAuditCount(t *testing.T, unitID uuid.UUID) int {
	t.Helper()
	var n int
	require.NoError(t, e.DB.QueryRow(`
		SELECT count(*)
		FROM audit_events
		WHERE event_type = $1
		  AND details->>'preparation_unit_id' = $2`,
		preparation.EventPreparationUnitAdvanced, unitID.String(),
	).Scan(&n))
	return n
}
```

- [ ] **Step 2: Write failing all-success and mixed-outcome integration tests**

Create `internal/preparation/bulk_advance_integration_test.go` and begin with:

```go
//go:build integration

package preparation_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/preparation"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBulkAdvanceAllSuccess(t *testing.T) {
	env := newPrepEnv(t)
	units := env.SubmittedUnits(t, 3)
	ids := []uuid.UUID{units[2].ID, units[0].ID, units[1].ID}

	status, got, err := env.BulkAdvance(t, preparation.BulkAdvanceCommand{
		RequestID: uuid.New(), PreparationUnitIDs: ids,
		TargetState: preparation.StateInPreparation,
	})
	require.NoError(t, err)
	require.Equal(t, 200, status)
	require.Len(t, got.Outcomes, 3)
	for i, outcome := range got.Outcomes {
		require.Equal(t, ids[i], outcome.PreparationUnitID)
		require.Equal(t, preparation.BulkStatusAdvanced, outcome.Status)
		require.NotNil(t, outcome.Unit)
		require.NotNil(t, outcome.Unit.InPreparationAt)
		require.Empty(t, outcome.Code)
		require.Equal(t, 1, env.CountTransitions(t, ids[i]))
		require.Equal(t, 1, env.UnitAuditCount(t, ids[i]))
	}
}

func TestBulkAdvancePreservesValidUnitAmongFailures(t *testing.T) {
	env := newPrepEnv(t)
	units := env.SubmittedUnits(t, 2)
	_, _, err := env.Advance(t, units[0].ID, preparation.StateInPreparation)
	require.NoError(t, err)
	missing := uuid.New()

	_, got, err := env.BulkAdvance(t, preparation.BulkAdvanceCommand{
		RequestID: uuid.New(),
		PreparationUnitIDs: []uuid.UUID{units[0].ID, missing, units[1].ID},
		TargetState: preparation.StateInPreparation,
	})
	require.NoError(t, err)
	require.Equal(t, []string{
		preparation.BulkCodeInvalidTransition,
		preparation.BulkCodeUnitNotFound,
		"",
	}, []string{got.Outcomes[0].Code, got.Outcomes[1].Code, got.Outcomes[2].Code})
	require.Equal(t, preparation.BulkStatusAdvanced, got.Outcomes[2].Status)
	require.Equal(t, preparation.StateInPreparation, env.UnitState(t, units[1].ID))
	require.Equal(t, 0, env.UnitAuditCount(t, missing))
}
```

- [ ] **Step 3: Add complete behavior coverage before implementation**

Add these tests to the same file:

- `TestBulkAdvanceAllFailuresReturns200`: one missing and one stale unit produce ordered `FAILED` outcomes; capture the stale unit's baseline transition/audit counts before the bulk call and assert the command adds zero to both.
- `TestBulkAdvanceDeduplicatesAtFirstOccurrence`: input `[B, A, B]` returns exactly `[B, A]`, each moved once.
- `TestBulkAdvanceAcceptsFiftyUnits`: all 50 unique ids advance; Task 5 already covers 51 as invalid without DB work.
- `TestBulkAdvanceRestoresFirstSelectionOrder`: use deliberately reverse-sorted UUIDs and assert response order remains the submitted first-occurrence order.
- `TestBulkAdvanceReplayIsExact`: marshal the first response, replay the same command, assert byte-equivalent JSON and unchanged transition/audit counts.
- `TestBulkAdvanceRejectsConflictingReplay`: reuse the request id with a different target or normalized id list and assert `ErrRequestConflict`.
- `TestBulkAdvanceRevalidatesAuthorityBeforeReplay`: execute successfully, set the actor session's `revoked_at`, replay, assert `ErrUnauthorized` and unchanged counts.
- `TestBulkAdvanceUnexpectedStoreFailureRollsBackEverything`: install the trigger below, run an all-success command, assert every unit is still `QUEUED`, every transition/audit count is zero, and no idempotency claim remains.
- `TestBulkAdvanceAuditFailureRollsBackEverything`: install a trigger that raises on `audit_events.event_type = 'PREPARATION_UNIT_ADVANCED'`, run an all-success command, and assert all unit states, transitions, audits, and the idempotency claim rolled back.
- `TestBulkAdvanceAuthorityDenialsAreAudited`: table-test locked, expired, revoked, disabled, and role-revoked Barista actors by calling `BulkAdvanceHandler` directly; assert `ErrUnauthorized` or `ErrForbidden`, no callback effects, and exactly one `preparation.authorization_denied` row whose `details->>'operation'` is `preparation.bulk_advance`.
- `TestOverlappingBulkAdvanceDoesNotDeadlockOrDoubleApply`: run two commands concurrently with ids `[A, B]` and `[B, A]`, wait with a five-second timeout, and assert each unit has exactly one transition and one audit; one response advances each unit and the competing outcome is `INVALID_TRANSITION`.

For the forced rollback test, create a transaction-local test trigger on the cloned database:

```go
_, err := env.DB.Exec(`
	CREATE OR REPLACE FUNCTION fail_preparation_result_store() RETURNS trigger AS $$
	BEGIN
		IF NEW.response_code <> 0 AND NEW.action = 'preparation.bulk_advance' THEN
			RAISE EXCEPTION 'forced idempotency result failure';
		END IF;
		RETURN NEW;
	END;
	$$ LANGUAGE plpgsql;
	CREATE TRIGGER fail_preparation_result_store
	BEFORE UPDATE ON idempotency_keys
	FOR EACH ROW EXECUTE FUNCTION fail_preparation_result_store();`)
require.NoError(t, err)
```

Register cleanup with `t.Cleanup` to drop the trigger and function. Query `idempotency_keys` by actor and request id to prove the failed outer transaction also removed its claim.

For the audit-failure variant, use a separate trigger function:

```sql
CREATE OR REPLACE FUNCTION fail_preparation_advance_audit() RETURNS trigger AS $$
BEGIN
    IF NEW.event_type = 'PREPARATION_UNIT_ADVANCED' THEN
        RAISE EXCEPTION 'forced preparation audit failure';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER fail_preparation_advance_audit
BEFORE INSERT ON audit_events
FOR EACH ROW EXECUTE FUNCTION fail_preparation_advance_audit();
```

- [ ] **Step 4: Run targeted tests to verify they fail**

Run:

```powershell
$env:TEST_DATABASE_URL = "postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable"
go test -count=1 -tags=integration ./internal/preparation -run TestBulkAdvance -v
```

Expected: compile FAIL because `BulkAdvanceHandler` is not implemented.

- [ ] **Step 5: Expose only a package-private fixed savepoint operation**

Change `MutationContext` in `internal/preparation/executor.go`:

```go
type MutationContext struct {
	Queries *sqlc.Queries
	tx      *sql.Tx
}

func (mc MutationContext) withUnitSavepoint(ctx context.Context,
	fn func(*sqlc.Queries) error,
) error {
	if _, err := mc.tx.ExecContext(ctx, "SAVEPOINT preparation_unit"); err != nil {
		return fmt.Errorf("create preparation unit savepoint: %w", err)
	}
	if err := fn(mc.Queries); err != nil {
		if _, rollbackErr := mc.tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT preparation_unit"); rollbackErr != nil {
			return fmt.Errorf("rollback preparation unit savepoint after callback failure: %w", rollbackErr)
		}
		if _, releaseErr := mc.tx.ExecContext(ctx, "RELEASE SAVEPOINT preparation_unit"); releaseErr != nil {
			return fmt.Errorf("release rolled-back preparation unit savepoint: %w", releaseErr)
		}
		return err
	}
	if _, err := mc.tx.ExecContext(ctx, "RELEASE SAVEPOINT preparation_unit"); err != nil {
		return fmt.Errorf("release preparation unit savepoint: %w", err)
	}
	return nil
}
```

Change the executor callback construction to:

```go
resultCode, result, audit, err := fn(MutationContext{Queries: q, tx: tx})
```

The savepoint SQL is constant and package-private. Do not accept a savepoint name or SQL from `BulkAdvanceCommand`.

- [ ] **Step 6: Implement deterministic lock order and result restoration**

Complete `internal/preparation/bulk_advance.go`:

```go
type BulkAdvanceHandler struct{ runner *Runner }

func NewBulkAdvanceHandler(runner *Runner) *BulkAdvanceHandler {
	return &BulkAdvanceHandler{runner: runner}
}

func (h *BulkAdvanceHandler) Handle(ctx context.Context, actor Actor,
	cmd BulkAdvanceCommand,
) (int, BulkAdvanceResponse, error) {
	if err := validateBulkAdvance(cmd); err != nil {
		return 0, BulkAdvanceResponse{}, err
	}
	selected := normalizeBulkSelection(cmd.PreparationUnitIDs)
	fingerprintIDs := append([]uuid.UUID(nil), selected...)
	lockOrder := sortedBulkSelection(selected)

	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpBulkAdvance,
		Fingerprint: bulkAdvanceFingerprint{
			PreparationUnitIDs: fingerprintIDs,
			TargetState: cmd.TargetState,
		},
		Required: []string{CapPreparationOperate},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, BulkAdvanceResponse, AuditRecord, error) {
			byID := make(map[uuid.UUID]BulkAdvanceOutcome, len(selected))
			audits := make([]AuditRecord, 0, len(selected))
			for _, unitID := range lockOrder {
				var transition transitionOutcome
				err := mc.withUnitSavepoint(ctx, func(q *sqlc.Queries) error {
					var err error
					transition, err = applyAdvance(ctx, q, actor, unitID, cmd.TargetState)
					return err
				})
				if err == nil {
					unit := transition.Unit
					byID[unitID] = BulkAdvanceOutcome{
						PreparationUnitID: unitID,
						Status: BulkStatusAdvanced,
						Unit: &unit,
					}
					audits = append(audits, transition.Audit)
					continue
				}
				switch {
				case errors.Is(err, ErrUnitNotFound):
					byID[unitID] = failedBulkOutcome(unitID, BulkCodeUnitNotFound)
				case errors.Is(err, ErrInvalidTransition):
					byID[unitID] = failedBulkOutcome(unitID, BulkCodeInvalidTransition)
				default:
					return 0, BulkAdvanceResponse{}, AuditRecord{}, err
				}
			}

			if err := writeAdvanceAudits(ctx, mc.Queries, actor, audits); err != nil {
				return 0, BulkAdvanceResponse{}, AuditRecord{}, err
			}
			outcomes := make([]BulkAdvanceOutcome, 0, len(selected))
			for _, id := range selected {
				outcomes = append(outcomes, byID[id])
			}
			return http.StatusOK, BulkAdvanceResponse{
				TargetState: cmd.TargetState,
				Outcomes: outcomes,
			}, AuditRecord{}, nil
		})
}
```

Add the small constructor; leaving `Unit` nil makes `omitempty` remove it:

```go
func failedBulkOutcome(unitID uuid.UUID, code string) BulkAdvanceOutcome {
	return BulkAdvanceOutcome{
		PreparationUnitID: unitID,
		Status: BulkStatusFailed,
		Code: code,
	}
}
```

- [ ] **Step 7: Batch one audit row per successful unit**

Add to `internal/preparation/bulk_advance.go`:

```go
func writeAdvanceAudits(ctx context.Context, q *sqlc.Queries, actor Actor,
	audits []AuditRecord,
) error {
	if len(audits) == 0 {
		return nil
	}
	detailsBatch := make([]string, len(audits))
	for i, audit := range audits {
		if audit.EventType != EventPreparationUnitAdvanced {
			return fmt.Errorf("unexpected bulk audit type %q", audit.EventType)
		}
		details, err := json.Marshal(audit.Details)
		if err != nil {
			return fmt.Errorf("marshal preparation advance audit: %w", err)
		}
		detailsBatch[i] = string(details)
	}
	if err := q.InsertAuditEventsBatch(ctx, sqlc.InsertAuditEventsBatchParams{
		EventType: EventPreparationUnitAdvanced,
		ActorID: actor.StaffID,
		SessionID: actor.SessionID,
		OccurredAt: time.Now(),
		DetailsBatch: detailsBatch,
	}); err != nil {
		return fmt.Errorf("insert preparation advance audits: %w", err)
	}
	return nil
}
```

The executor receives a zero `AuditRecord` because the per-unit events above are the complete business audit trail. There is no extra batch-summary event in the approved response or event contract.

- [ ] **Step 8: Run bulk integration and concurrency tests**

Run:

```powershell
gofmt -w internal/preparation
$env:TEST_DATABASE_URL = "postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable"
go test -count=1 -tags=integration ./internal/preparation -run 'TestBulkAdvance|TestOverlappingBulkAdvance' -v
go test -count=1 -race -tags=integration ./internal/preparation -run 'TestOverlappingBulkAdvance|TestConcurrentAdvance' -v
go test ./internal/preparation
```

Expected: all PASS, no timeout, and no race report.

- [ ] **Step 9: Commit if explicitly requested**

```powershell
git add internal/preparation/executor.go internal/preparation/bulk_advance.go internal/preparation/bulk_advance_integration_test.go internal/preparation/env_integration_test.go
git commit -m "feat(preparation): add savepoint bulk advance"
```

---

### Task 7: HTTP Routes, Authorization, Privacy, And Swagger

**Files:**
- Modify: `internal/preparation/routes.go`
- Modify: `internal/preparation/http.go`
- Create: `internal/preparation/preparation_integration_test.go`
- Regenerate: `docs/docs.go`
- Regenerate: `docs/swagger.json`
- Regenerate: `docs/swagger.yaml`

**Interfaces:**
- Produces: `GET /api/v1/preparation/queue`
- Produces: `POST /api/v1/preparation/units/advance-many`
- Changes: `Slices` wires `ActiveQueue`, `AdvanceUnit`, and `BulkAdvance`
- Consumes: existing Bearer middleware plus handler-level authority checks

- [ ] **Step 1: Write failing route and authorization tests**

Create `internal/preparation/preparation_integration_test.go` with a real Echo server and the same `envelope`, `doPreparationRequest`, and real `/api/v1/auth/sign-in` helper shape as `internal/tables/tables_integration_test.go`. Mount routes without truncating so populated tests can mount them over `newPrepEnv`:

```go
func mountPreparationTestServer(db *sql.DB, q *sqlc.Queries) *echo.Echo {
	e := echo.New()
	e.Validator = httpvalidator.New()
	v1 := e.Group("/api/v1")
	authSlices := auth.NewSlices(db, q)
	authSlices.RegisterRoutes(v1)
	preparation.NewSlices(db, q).RegisterRoutes(v1, authSlices.Middleware)
	return e
}

func newPreparationTestServer(t *testing.T) (*echo.Echo, *sqlc.Queries) {
	t.Helper()
	db, q := openPrepTestDB(t)
	truncatePrepTables(t, db)
	return mountPreparationTestServer(db, q), q
}
```

`signInPreparation` must create a unique enabled identity, assign the requested roles, call the real sign-in endpoint, decode `data.token`, and return it. Do not construct or hash a bearer token directly.

Add this table test:

```go
func TestPreparationHTTPAuthorization(t *testing.T) {
	e, q := newPreparationTestServer(t)
	barista := signInPreparation(t, e, q, []string{"BARISTA"})
	manager := signInPreparation(t, e, q, []string{"MANAGER"})
	cashier := signInPreparation(t, e, q, []string{"CASHIER"})

	bulkBody, err := json.Marshal(preparation.BulkAdvanceCommand{
		RequestID: uuid.New(),
		PreparationUnitIDs: []uuid.UUID{uuid.New()},
		TargetState: preparation.StateInPreparation,
	})
	require.NoError(t, err)

	for _, tc := range []struct{ name, method, path string; body []byte }{
		{"queue", http.MethodGet, "/api/v1/preparation/queue", nil},
		{"bulk", http.MethodPost, "/api/v1/preparation/units/advance-many", bulkBody},
	} {
		t.Run(tc.name+" allows barista", func(t *testing.T) {
			rec := doPreparationRequest(t, e, tc.method, tc.path, barista, tc.body)
			assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		})
		t.Run(tc.name+" allows manager", func(t *testing.T) {
			rec := doPreparationRequest(t, e, tc.method, tc.path, manager, tc.body)
			assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		})
		t.Run(tc.name+" denies cashier", func(t *testing.T) {
			rec := doPreparationRequest(t, e, tc.method, tc.path, cashier, tc.body)
			assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
		})
		t.Run(tc.name+" denies anonymous", func(t *testing.T) {
			rec := doPreparationRequest(t, e, tc.method, tc.path, "", tc.body)
			assert.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
		})
	}
}
```

The nonexistent bulk id is intentional: an authorized request completes with HTTP 200 and one `UNIT_NOT_FOUND` outcome.

- [ ] **Step 2: Add HTTP validation and response-contract tests**

Add:

- `TestPreparationHTTPBulkValidation`: missing request id, malformed UUID in the array, empty ids, 51 ids, and invalid target each return 400; conflicting request-id reuse returns 409.
- `TestPreparationHTTPQueueUsesEmptyArrays`: the raw empty response contains `"units":[]`, not `null`.
- `TestPreparationHTTPQueuePrivacyContract`: submit a populated unit through fixtures, GET the queue, decode the unit into `map[string]json.RawMessage`, and assert its keys are exactly:

```go
[]string{
	"id", "order_item_id", "order_item_unit_count", "unit_number",
	"state", "service_number", "table_names", "category_name", "item_name",
	"size_name", "modifiers", "preparation_note", "queued_at", "in_preparation_at",
}
```

Also recursively assert the raw queue response contains none of these case-insensitive fragments: `price`, `allocation`, `check`, `payment`, `balance`, `sales_shift`, `pin_hash`, `token_hash`.

- [ ] **Step 3: Run route tests to verify they fail**

Run:

```powershell
$env:TEST_DATABASE_URL = "postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable"
go test -count=1 -tags=integration ./internal/preparation -run TestPreparationHTTP -v
```

Expected: FAIL with 404 responses because the queue and bulk routes are not registered.

- [ ] **Step 4: Wire handlers and register static routes**

Update `internal/preparation/routes.go`:

```go
type Slices struct {
	Runner *Runner

	ActiveQueue *ActiveQueueHandler
	AdvanceUnit *AdvanceUnitHandler
	BulkAdvance *BulkAdvanceHandler
}

func NewSlices(db *sql.DB, queries *sqlc.Queries) *Slices {
	runner := NewRunner(db, queries)
	return &Slices{
		Runner: runner,
		ActiveQueue: NewActiveQueueHandler(runner),
		AdvanceUnit: NewAdvanceUnitHandler(runner),
		BulkAdvance: NewBulkAdvanceHandler(runner),
	}
}

func (s *Slices) RegisterRoutes(v1 *echo.Group, authn *auth.Middleware) {
	v1.GET("/preparation/queue", s.handleActiveQueue,
		authn.RequireAuth(), authn.RequireCapability(CapPreparationOperate))
	v1.POST("/preparation/units/advance-many", s.handleBulkAdvance,
		authn.RequireAuth(), authn.RequireCapability(CapPreparationOperate))
	v1.POST("/preparation/units/:unit_id/advance", s.handleAdvanceUnit,
		authn.RequireAuth(), authn.RequireCapability(CapPreparationOperate))
}
```

- [ ] **Step 5: Add HTTP adapters and Swagger annotations**

Add to `internal/preparation/http.go`:

```go
// handleActiveQueue godoc
//
//	@Summary		Read the active Preparation Queue
//	@Description	Returns active units in FIFO order with PostgreSQL observed time and current Table names. The projection contains no financial data.
//	@Tags			preparation
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	response.APIResponse{data=QueueResponse}
//	@Failure		401	{object}	response.APIResponse
//	@Failure		403	{object}	response.APIResponse
//	@Failure		500	{object}	response.APIResponse
//	@Router			/preparation/queue [get]
func (s *Slices) handleActiveQueue(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	result, err := s.ActiveQueue.Handle(c.Request().Context(), actor)
	if err != nil {
		return sendError(c, err)
	}
	return response.OK(c, result)
}

// handleBulkAdvance godoc
//
//	@Summary		Advance selected Preparation Units
//	@Description	Advances 1 through 50 selected units. Missing or stale units are per-unit failures; unexpected failures roll back the request.
//	@Tags			preparation
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			body	body		BulkAdvanceCommand	true	"Bulk advance request"
//	@Success		200		{object}	response.APIResponse{data=BulkAdvanceResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Failure		500		{object}	response.APIResponse
//	@Router			/preparation/units/advance-many [post]
func (s *Slices) handleBulkAdvance(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	body, err := bindBody[BulkAdvanceCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	status, result, err := s.BulkAdvance.Handle(c.Request().Context(), actor, body)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, result)
}
```

Update the existing single-unit Swagger description to say the exceptional operations are Phase 6B/6C, add its new `in_preparation_at` response field through `UnitResponse`, and add a documented 500 response if absent.

- [ ] **Step 6: Regenerate and inspect Swagger**

Run:

```powershell
swag init -g cmd/api/main.go -o docs
git diff -- docs/docs.go docs/swagger.json docs/swagger.yaml
```

Expected: both new paths exist with Bearer security, queue/bulk schemas, response schemas, and the documented failure statuses. Reject any generator output that exposes unexported fingerprints or internal audit types.

- [ ] **Step 7: Run HTTP and package tests**

Run:

```powershell
gofmt -w internal/preparation
$env:TEST_DATABASE_URL = "postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable"
go test -count=1 -tags=integration ./internal/preparation -run 'TestPreparationHTTP|TestActiveQueue|TestBulkAdvance' -v
go test ./internal/preparation
go vet ./internal/preparation
```

Expected: all PASS.

- [ ] **Step 8: Commit if explicitly requested**

```powershell
git add internal/preparation/routes.go internal/preparation/http.go internal/preparation/preparation_integration_test.go docs/docs.go docs/swagger.json docs/swagger.yaml
git commit -m "feat(preparation): expose queue and bulk routes"
```

---

### Task 8: Decision Records, Roadmap, And Full Verification

**Files:**
- Modify: `spec/decisions.md`
- Modify: `MIGRATE_PLAN.md`
- Verify: all Phase 6A files

**Interfaces:**
- Documents: ADR-032 through ADR-035
- Documents: 6A complete; 6B and 6C remain explicitly deferred
- Verifies: Submit-to-fulfillment and Service Session closure regression path

- [ ] **Step 1: Add the approved decision records**

Append four entries to `spec/decisions.md`, matching the file's existing ADR format:

```markdown
## ADR-032: Phase 6 is decomposed

* **Decision Date:** 2026-09-16
* **Status:** Accepted
* **Context:** Preparation spans ordinary queue work, exceptional corrections, and Cancellation's unresolved Refund/Comp dependencies.
* **Decision:**
* ship 6A queue and ordinary transitions, 6B exceptional preparation workflows, and 6C cancellation with its financial dependencies.
* **Consequences:**
* ordinary operational work remains independent from correction and refund policy; Phase 6 is not complete when 6A ships.

---

## ADR-033: Submit remains the queue creation boundary

* **Decision Date:** 2026-09-16
* **Status:** Accepted
* **Context:** Phase 5D already creates Preparation Units atomically with Submit.
* **Decision:**
* do not publish or consume `order.submitted` through Watermill for queue creation.
* **Consequences:**
* a committed Submit is immediately queue-visible with no duplicate delivery path; Sales remains the sole creator while Preparation owns transitions.

---

## ADR-034: Bulk advance uses per-unit savepoints

* **Decision Date:** 2026-09-16
* **Status:** Accepted
* **Context:** stale or missing selected units are normal operational outcomes, while audit, database, and idempotency failures are not.
* **Decision:**
* verify authority and idempotency once, lock unique units in UUID order, and process each unit in a fixed package-private PostgreSQL savepoint.
* **Consequences:**
* missing and stale units become typed outcomes; infrastructure failures abort every successful unit in the outer transaction.

---

## ADR-035: Launch queue freshness uses polling

* **Decision Date:** 2026-09-16
* **Status:** Accepted
* **Context:** the staff-LAN client needs fresh shared queue state but no Phase 6A behavior requires server push.
* **Decision:**
* expose a side-effect-free GET with PostgreSQL `observed_at`; let Phase 7 poll and invalidate after local mutations.
* **Consequences:**
* Phase 6A adds no SSE, WebSockets, notifier seam, long-lived connections, or background fan-out.
```

- [ ] **Step 2: Update the migration roadmap**

In `MIGRATE_PLAN.md`:

- Mark every implemented Phase 6A queue, timestamp, shared transition, bulk, authorization, audit, idempotency, HTTP, and test checkbox complete.
- Leave Alerts, acknowledgment, Waste, Remake, priority, and State Correction under 6B unchecked.
- Leave Cancellation/change, Refund/Comp, pending-refund closure policy, and financial integration under 6C unchecked.
- Keep Watermill delivery out of 6A and state that synchronous Submit remains authoritative per ADR-033.
- Do not mark Phase 6 as a whole complete.

- [ ] **Step 3: Verify generated files are current**

Hash generated outputs, rerun both generators, and require byte-for-byte stability:

```powershell
$generated = @(
	Get-ChildItem -File "internal/database/sqlc/*.go"
	Get-Item "docs/docs.go", "docs/swagger.json", "docs/swagger.yaml"
)
$before = $generated | Get-FileHash -Algorithm SHA256 | ForEach-Object { "$($_.Path)=$($_.Hash)" }
sqlc generate
swag init -g cmd/api/main.go -o docs
$after = $generated | Get-FileHash -Algorithm SHA256 | ForEach-Object { "$($_.Path)=$($_.Hash)" }
$drift = Compare-Object $before $after
if ($drift) { $drift; throw "generated files were stale" }
```

Expected: no comparison output. If generated files change, inspect and include the legitimate output, then rerun this step until stable.

- [ ] **Step 4: Run formatting, build, vet, and lint**

Run:

```powershell
make fmt
go build ./...
go vet ./...
golangci-lint run ./...
```

Expected: every command exits 0.

- [ ] **Step 5: Run the complete unit suite with race detection**

Run:

```powershell
go test -count=1 -race ./...
```

Expected: PASS with no race report.

- [ ] **Step 6: Run the complete PostgreSQL integration suite with race detection**

Ensure PostgreSQL is healthy, then run:

```powershell
docker compose up -d
$env:TEST_DATABASE_URL = "postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable"
go test -count=1 -race -tags=integration ./...
```

Expected: all packages PASS; the existing Sales Submit, fulfillment, Completed Sale, and Service Session closure tests remain green.

- [ ] **Step 7: Run focused concurrency verification once more**

Run:

```powershell
$env:TEST_DATABASE_URL = "postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable"
go test -count=10 -race -tags=integration ./internal/preparation -run 'TestConcurrentAdvance|TestOverlappingBulkAdvance' -v
```

Expected: ten consecutive PASS runs, no deadlock timeout, no duplicate state step, and no race report.

- [ ] **Step 8: Inspect scope and whitespace**

Run:

```powershell
git status --short
git diff --check
git diff --stat
git diff -- internal/preparation sql/queries/preparation.sql internal/database/migrations/000012_add_preparation_queue_fields.sql spec/decisions.md MIGRATE_PLAN.md
$untracked = @(git ls-files --others --exclude-standard)
foreach ($file in $untracked) {
	$issues = & git diff --no-index --check -- NUL $file 2>&1
	if ($issues) { $issues; throw "whitespace error in $file" }
}
```

Expected: no whitespace errors in tracked or untracked files; no Watermill consumer, push transport, 6B/6C persistence, or unrelated refactor appears in the diff.

- [ ] **Step 9: Commit documentation if explicitly requested**

```powershell
git add spec/decisions.md MIGRATE_PLAN.md
git commit -m "docs(preparation): record Phase 6A decisions"
```

- [ ] **Step 10: Request final code review before integration**

Invoke `superpowers:requesting-code-review` and ask the reviewer to compare the implementation against all twelve acceptance criteria in the approved spec, with special attention to:

- read-only repeatable-read consistency and current-authority denials;
- UUID lock ordering versus first-selection response ordering;
- savepoint cleanup and expected-error classification;
- exact replay with no duplicate transitions or audits;
- outer rollback on audit/idempotency storage failure;
- privacy of queue and Swagger schemas;
- explicit preservation of the Phase 6B/6C boundary.
