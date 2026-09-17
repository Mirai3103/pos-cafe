# Preparation Corrections & Recovery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add typed, idempotent Preparation alerts, Waste, Remake, Remake priority, and atomic Manager-authorized state correction to the Phase 6A queue without changing customer charges.

**Architecture:** Keep `internal/sales` as the creator of original Preparation Units and owner of Service Session closure. Grow `internal/preparation` with explicit correction fact tables and handlers using its existing transaction executor. Every exceptional state change writes the current unit, immutable typed fact, transition history, audit evidence, and replay result atomically; Remake and correction lock owning Sessions before work rows so they serialize with closure.

**Tech Stack:** Go 1.27.1, Echo v4, PostgreSQL 17, `database/sql` with pgx stdlib, sqlc v2, testify, bcrypt through `internal/auth`, Swaggo/OpenAPI 2.0

**Spec:** `docs/superpowers/specs/2026-09-17-preparation-corrections-design.md`

## Global Constraints

- Phase 6B writes only `WASTE` alerts. Declare `CANCELLATION` and `CHANGE` as valid alert kinds for Phase 6C, but add no Cancellation/change command, Refund, Comp, Payment Void, charge mutation, or pending-refund closure rule.
- `internal/sales` remains the creator of original units at Submit. `internal/preparation` creates only linked Remake units and never writes Orders, Order Items, Checks, Charge Allocations, Payments, or Completed Sales.
- A Remake copies the source unit's immutable preparation snapshot, uses the next unit number of the same Order Item, has `priority = REMAKE`, and changes no customer charge.
- Waste, Remake, and alert acknowledgment require `preparation.operate`. State Correction additionally requires the current actor's Manager role and fresh own PIN.
- Manager PIN verification occurs inside the mutation transaction before idempotency replay. PIN values never enter fingerprints, responses, database facts, audits, or logs.
- State Correction accepts 1 through 50 non-zero unique unit ids and is all-or-nothing. Do not reuse Phase 6A's per-unit savepoints.
- Remake locks Service Session, Order Item, Waste, and source unit in that order. State Correction locks unique Service Sessions in UUID order, then units in UUID order. This preserves Session-first serialization with closure.
- Waste uses the existing unit lock. Acknowledgment uses an alert lock. Expected unique-constraint races map by named constraint only; unknown PostgreSQL errors remain infrastructure failures.
- Every state change writes `preparation_unit_transitions`. Dedicated fact tables explain why; audit rows explain who invoked the action. Never derive Completed Sale history from audit JSON.
- Queue reads stay in one read-only `REPEATABLE READ` transaction and expose no financial or credential data.
- Every response collection is allocated as a non-nil slice. Optional notes are trimmed; a present note is 1 through 500 Unicode code points, and `OTHER` requires one.
- Run `sqlc generate` after SQL or migration changes; never hand-edit `internal/database/sqlc/*.go`.
- Run `swag init -g cmd/api/main.go -o docs` after annotation changes; never hand-edit generated Swagger files.
- Integration tests require PostgreSQL and `TEST_DATABASE_URL=postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable`.
- Commit steps below are implementation checkpoints. Execute them only when commit authorization covers the implementation session.

## File Map

| File | Responsibility |
| --- | --- |
| `internal/database/migrations/000013_add_preparation_corrections.sql` | Priority/link columns, alerts, Waste, Remake, correction facts, and legal transition pairs |
| `sql/queries/preparation.sql` | Correction locks/writes, alert/history reads, Remake allocation, and queue ordering |
| `sql/queries/auth.sql` | Lock the current actor identity by id for self-PIN verification |
| `sql/queries/sales.sql` | Project Remake metadata into live and Completed Sale unit reads |
| `internal/database/sqlc/{models,preparation.sql,auth.sql,sales.sql,querier}.go` | Generated schema and query bindings |
| `internal/preparation/domain.go` | Operation, event, kind, priority, reason, and correction-state rules |
| `internal/preparation/dto.go` | Command, fact, unit, alert, history, and batch response contracts |
| `internal/preparation/errors.go` | Stable Phase 6B domain-to-HTTP mappings |
| `internal/preparation/executor.go` | Current Manager own-PIN gate and shared audit-batch helper |
| `internal/preparation/waste.go` | Waste command and its two business audits |
| `internal/preparation/alerts.go` | Alert acknowledgment command |
| `internal/preparation/remake.go` | Linked Remake creation and Session/Order Item lock protocol |
| `internal/preparation/correct_state.go` | Atomic 1-50 unit reverse correction command |
| `internal/preparation/advance.go` | Unit response mapper extended with priority/link metadata |
| `internal/preparation/queue.go` | Active/alert-retained units, active alerts, and recent correction history |
| `internal/preparation/routes.go`, `internal/preparation/http.go` | Echo wiring, boundary validation, and Swagger annotations |
| `internal/preparation/*_test.go` | Pure validation, schema, command, queue, HTTP, rollback, and concurrency coverage |
| `internal/sales/dto.go`, `internal/sales/projection.go` | Live Service Session Remake metadata projection |
| `internal/sales/session_close_integration_test.go` | Closure and Completed Sale regression coverage |
| `docs/{docs.go,swagger.json,swagger.yaml}` | Generated OpenAPI artifacts |
| `spec/decisions.md` | ADR-036 through ADR-039 |
| `MIGRATE_PLAN.md` | Mark 6B complete while preserving the 6C boundary |

---

### Task 1: Correction Schema And Generated SQL Foundation

**Files:**
- Create: `internal/database/migrations/000013_add_preparation_corrections.sql`
- Modify: `sql/queries/preparation.sql`
- Modify: `sql/queries/auth.sql`
- Modify: `sql/queries/sales.sql`
- Regenerate: `internal/database/sqlc/models.go`
- Regenerate: `internal/database/sqlc/preparation.sql.go`
- Regenerate: `internal/database/sqlc/auth.sql.go`
- Regenerate: `internal/database/sqlc/sales.sql.go`
- Regenerate: `internal/database/sqlc/querier.go`
- Modify: `internal/preparation/schema_integration_test.go`

**Interfaces:**
- Adds `PreparationUnit.Priority string` and `RemakeOfPreparationUnitID uuid.NullUUID`.
- Adds generated models for `PreparationAlert`, `PreparationWaste`, `PreparationRemake`, and `PreparationStateCorrection`.
- Adds locking/writing queries named in Steps 4 and 5.
- Adds `InsertPreparationAuditEventsBatch` with aligned event-type/detail arrays.
- Changes `GetPreparationUnit`, `LockPreparationUnit`, `ListActivePreparationUnits`, and `ListSessionPreparationUnits` to include priority/link fields.

- [ ] **Step 1: Write failing migration contract tests**

Extend `internal/preparation/schema_integration_test.go` with tests that query `information_schema` and `pg_get_constraintdef`:

- `TestPreparationCorrectionsSchema` verifies all four tables, foreign keys, required actor/session/timestamp columns, and the two new unit columns.
- `TestPreparationCorrectionsSchemaBackfillsStandardPriority` creates a real submitted unit, drops the four 6B tables and two new unit columns to emulate the pre-`000013` schema, runs the migration text, and asserts the existing unit receives `STANDARD` plus a null Remake link. Register a `t.Cleanup` that reruns the idempotent migration so a failed assertion cannot leave the package clone's schema downgraded for later tests; do not mark this schema-changing test parallel.
- `TestPreparationCorrectionsSchemaEnforcesPriorityLinkPair` rejects `STANDARD + link`, `REMAKE + null`, and unknown priority.
- `TestPreparationCorrectionsSchemaEnforcesAlertAcknowledgmentTuple` accepts all-null or all-present acknowledgment evidence and rejects partial tuples.
- `TestPreparationCorrectionsSchemaEnforcesReasonsAndNotes` rejects unknown reasons, blank/over-500 notes, and `OTHER` without a note. It directly inserts all three alert kinds and proves `CANCELLATION|CHANGE` accept only `CUSTOMER_REQUEST|ORDER_ENTRY_ERROR|ITEM_UNAVAILABLE|OTHER`, while `WASTE` accepts only its Waste catalog.
- `TestPreparationCorrectionsSchemaExtendsTransitionPairs` accepts the two Waste and three reverse-correction pairs while still rejecting skipped, exceptional-terminal, and `QUEUED -> CANCELLED` pairs.
- `TestPreparationCorrectionsSchemaEnforcesOneWasteAndOneRemake` verifies unique source-unit Waste, unique Waste Remake, and unique replacement unit.

Use the real migration file, as the Phase 6A backfill test does, rather than duplicating its SQL in the test.

- [ ] **Step 2: Run the schema tests to verify they fail**

```powershell
$env:TEST_DATABASE_URL = "postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable"
go test -count=1 -tags=integration ./internal/preparation -run 'TestPreparationCorrectionsSchema' -v
```

Expected: FAIL because migration `000013` and its objects do not exist.

- [ ] **Step 3: Add migration `000013_add_preparation_corrections.sql`**

Implement the approved schema with explicit, stable constraint names:

- Add `preparation_units.priority TEXT NOT NULL DEFAULT 'STANDARD'`.
- Add nullable self-reference `remake_of_preparation_unit_id ... ON DELETE RESTRICT`.
- Add `preparation_unit_priority_valid` and `preparation_unit_priority_link_valid`.
- Create `preparation_alerts` with `kind`, reason/note, creation evidence, and the all-null/all-present acknowledgment tuple.
- Create `preparation_wastes`, unique on `preparation_unit_id`, with prior state restricted to `IN_PREPARATION|READY`.
- Create `preparation_remakes`, unique on both `waste_id` and replacement `preparation_unit_id`.
- Create append-only `preparation_state_corrections` with the three approved reverse pairs.
- Add indexes `preparation_alert_active_index` and `preparation_state_correction_unit_index` using the exact sort columns from the spec.
- Replace `preparation_unit_transition_states_valid` with the existing three forward pairs plus two Waste pairs and three correction pairs. Do not add Cancellation yet.

Keep `CANCELLATION|CHANGE|WASTE` in the alert-kind check from the first migration. The reason check must be kind-aware: enforce the approved Waste catalog for `WASTE`, and enforce `CUSTOMER_REQUEST|ORDER_ENTRY_ERROR|ITEM_UNAVAILABLE|OTHER` for the two reserved Phase 6C kinds without adding writers for them.

- [ ] **Step 4: Add mutation and lock queries to `sql/queries/preparation.sql`**

Add these generated operations with selected columns explicit, never `SELECT *`:

```text
LockPreparationServiceSessions   -- unique ids, ORDER BY id, FOR UPDATE
LockPreparationOrderItem        -- id plus owning Session, FOR UPDATE
LockPreparationWaste            -- Waste plus source unit/session/order-item ids, FOR UPDATE OF waste and unit
LockPreparationAlert            -- alert acknowledgment evidence, FOR UPDATE
ListPreparationUnitsForCorrection -- selected ids with Session ids, no lock
LockPreparationUnitsForCorrection -- sorted ids, ORDER BY unit id, FOR UPDATE
SetPreparationUnitCorrectedState -- clears in_preparation_at only for target QUEUED
InsertPreparationWaste
InsertPreparationAlert
AcknowledgePreparationAlert
InsertPreparationRemakeUnit
InsertPreparationRemake
GetNextPreparationUnitNumber
InsertPreparationStateCorrection
InsertPreparationAuditEventsBatch
ListActivePreparationAlerts
ListRecentPreparationCorrections
```

`InsertPreparationAuditEventsBatch` takes aligned `event_types text[]` and `details_batch text[]`, plus one actor, session, and timestamp. Validate equal non-zero lengths in Go, then pair the arrays with PostgreSQL's multi-array `unnest(event_types, details_batch)`; its null padding makes any caller drift fail the target columns' `NOT NULL` constraints rather than silently truncating one array. Leave the existing Catalog `InsertAuditEventsBatch` unchanged because its callers intentionally write several events of one type.

Change `ListActivePreparationUnits` so it:

- Includes active units and terminal units with an unacknowledged alert. Use `EXISTS`, not a direct alert join, so multiple active alerts for one unit cannot duplicate the unit row.
- Projects `priority` and `remake_of_preparation_unit_id`.
- Counts every physical unit, including Remakes and terminal units.
- Orders active Remakes first, active Standard units second, then alert-retained terminal units; each lane uses `queued_at, id`.

Implement `ListRecentPreparationCorrections` as one set-based `UNION ALL` over Waste and Remake facts for active Sessions, newest first by occurrence/creation time and id, `LIMIT 50`. Return a discriminator and all nullable columns needed for explicit Go mapping; do not query per history row.

- [ ] **Step 5: Add self-PIN and Sales projection queries**

In `sql/queries/auth.sql`, add:

```sql
-- name: GetStaffByIDForUpdate :one
SELECT id, display_name, login_code, pin_hash, enabled, created_at
FROM staff_identities
WHERE id = $1
FOR UPDATE;
```

Reuse existing `GetStaffRolesForUpdate` after locking the identity. In `sql/queries/sales.sql`, add `priority` and `remake_of_preparation_unit_id` to `ListSessionPreparationUnits`; this one query feeds both live Service Session and Completed Sale projections.

- [ ] **Step 6: Regenerate sqlc and inspect only generated outputs**

```powershell
sqlc generate
gofmt -w internal/database/sqlc
git diff -- internal/database/sqlc/models.go internal/database/sqlc/preparation.sql.go internal/database/sqlc/auth.sql.go internal/database/sqlc/sales.sql.go internal/database/sqlc/querier.go
```

Expected: generated methods and nullable types match the Interfaces block. Fix SQL aliases if sqlc emits anonymous names; never edit generated code.

- [ ] **Step 7: Run schema and compile checks**

```powershell
$env:TEST_DATABASE_URL = "postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable"
go test -count=1 -tags=integration ./internal/preparation -run 'TestPreparationCorrectionsSchema' -v
go test ./internal/preparation ./internal/sales
go vet ./internal/preparation ./internal/sales
```

Expected: schema tests PASS; Go packages may require temporary mapper additions if generated row fields changed. Add only the minimal compile-preserving field mapping that Task 2 tests will pin.

- [ ] **Step 8: Commit the schema checkpoint**

```powershell
git add internal/database/migrations/000013_add_preparation_corrections.sql sql/queries/preparation.sql sql/queries/auth.sql sql/queries/sales.sql internal/database/sqlc internal/preparation/schema_integration_test.go
git commit -m "feat(preparation): add correction schema"
```

---

### Task 2: Domain Rules, DTOs, And Stable Errors

**Files:**
- Modify: `internal/preparation/domain.go`
- Modify: `internal/preparation/domain_test.go`
- Modify: `internal/preparation/dto.go`
- Modify: `internal/preparation/errors.go`
- Modify: `internal/preparation/errors_test.go`
- Modify: `internal/preparation/advance.go`

**Interfaces:**
- Produces operation/event constants from spec Sections 7 and 11.
- Produces `NormalizeCorrectionNote`, operation-specific reason validators, and `RequiredPriorState`.
- Adds priority/link metadata to `UnitResponse` and `QueueUnitResponse`.
- Produces all four command/result contracts plus queue alert/history contracts.

- [ ] **Step 1: Write failing pure domain tests**

Add table-driven tests for:

- Waste reasons: `PREPARATION_ERROR`, `QUALITY_FAILURE`, `CUSTOMER_REQUEST`, `OTHER`.
- Remake reasons: `PREPARATION_ERROR`, `QUALITY_FAILURE`, `OTHER`.
- Correction reasons: `STATE_RECORDED_IN_ERROR`, `OTHER`.
- Note trimming, nil/blank normalization, 500-rune acceptance, 501-rune rejection, and required `OTHER` note.
- `RequiredPriorState`: `QUEUED <- IN_PREPARATION`, `IN_PREPARATION <- READY`, `READY <- FULFILLED`; reject every other target.
- Correction selection rejects zero ids, more than 50 ids, duplicates, and `uuid.Nil` rather than silently deduplicating.
- A marshaled correction fingerprint contains ids, target, reason, and normalized note but no `manager_pin` or PIN value.

- [ ] **Step 2: Write failing error mapping tests**

Extend `errors_test.go` for stable status/code pairs:

```text
ErrAlertNotFound               404 PREPARATION_ALERT_NOT_FOUND
ErrWasteNotFound               404 PREPARATION_WASTE_NOT_FOUND
ErrInvalidReason               400 INVALID_PREPARATION_REASON
ErrInvalidNote                 400 INVALID_PREPARATION_NOTE
ErrAlertAlreadyAcknowledged    409 PREPARATION_ALERT_ALREADY_ACKNOWLEDGED
ErrWasteAlreadyRemade          409 PREPARATION_WASTE_ALREADY_REMADE
ErrServiceSessionClosed        409 SERVICE_SESSION_ALREADY_CLOSED
ErrInvalidManagerPIN           403 NOT_AUTHORIZED
```

Retain existing mappings. Missing or malformed PIN shape is a boundary error through `response.ErrInvalid` as `400 INVALID_INPUT`; only a well-formed PIN that fails current self-authentication maps to the collapsed `403 NOT_AUTHORIZED` response.

- [ ] **Step 3: Run unit tests to verify they fail**

```powershell
go test ./internal/preparation -run 'Test.*Reason|Test.*Note|TestRequiredPriorState|TestValidateCorrectState|TestErrorResponse' -v
```

Expected: compile/test FAIL because the Phase 6B contract does not exist.

- [ ] **Step 4: Add constants and pure validators**

In `domain.go`, group constants by operation, event, alert kind, priority, and reason. Use `utf8.RuneCountInString` for the 500-character boundary. Normalize before validation and before building fingerprints.

Add pure helpers only where multiple handlers/tests need them. Keep command orchestration in command files rather than building a generic correction framework.

- [ ] **Step 5: Add explicit DTOs**

Add these named contracts:

```text
AcknowledgeAlertCommand / AlertResponse
WasteUnitCommand / WasteResponse
RemakeUnitCommand / RemakeResponse
CorrectStateCommand / CorrectStateOutcome / CorrectStateResponse
QueueAlertResponse / QueueCorrectionResponse
```

`CorrectStateCommand.ManagerPIN` is request-only. Fingerprint structs remain package-private in command files and deliberately omit credentials. Allocate queue/result collections before return.

- [ ] **Step 6: Extend unit mappers and errors**

Map `priority` and nullable Remake link in `loadUnit` and `queueUnitFromRow`. Extend `ErrorResponse` using `errors.Is`; never inspect error strings. Preserve wrapped database detail only for server logs.

- [ ] **Step 7: Run focused and package tests**

```powershell
gofmt -w internal/preparation
go test ./internal/preparation -v
go vet ./internal/preparation
```

Expected: all PASS.

- [ ] **Step 8: Commit the contract checkpoint**

```powershell
git add internal/preparation/domain.go internal/preparation/domain_test.go internal/preparation/dto.go internal/preparation/errors.go internal/preparation/errors_test.go internal/preparation/advance.go
git commit -m "feat(preparation): define correction contracts"
```

---

### Task 3: Current Manager Self-PIN And Audit Batch Support

**Files:**
- Modify: `internal/preparation/executor.go`
- Modify: `internal/preparation/executor_integration_test.go`
- Modify: `internal/preparation/env_integration_test.go`

**Interfaces:**
- Changes `MutationSpec` with `ManagerPIN string` and `RequireManagerPIN bool`.
- Changes `reloadAuthority` to return current roles as well as capabilities.
- Produces `verifyCurrentManagerPIN(ctx, q, actor, pin, requiredCapability) error`.
- Produces package-private `writePreparationAudits(ctx, q, actor, occurredAt, []AuditRecord) error`.

- [ ] **Step 1: Add fixture support for known PINs and current-role mutation**

Change `seedActor` support in `env_integration_test.go` so the Manager fixture retains its known plaintext test PIN without placing it on production `Actor`. Add helpers to rotate the Manager PIN, replace roles, disable identity, count audit events by unit/type, and inspect idempotency claims.

- [ ] **Step 2: Write failing executor integration tests**

Add:

- `TestExecuteMutationRequiresCurrentManagerAndOwnPINBeforeReplay`.
- `TestExecuteMutationWrongManagerPINWritesDenialWithoutPIN`.
- `TestExecuteMutationManagerPINRotationControlsReplay`.
- `TestExecuteMutationRemovedManagerRoleOrCapabilityDeniesReplay`.
- `TestExecuteMutationManagerIdentityAndRolesRemainLocked`.
- `TestWritePreparationAuditsIsAtomic` using a trigger that fails the second batch row.

The replay test must execute once, rotate the actor's PIN, prove the old PIN is denied, and prove the new PIN returns the exact stored response without rerunning the callback.

- [ ] **Step 3: Run tests to verify they fail**

```powershell
$env:TEST_DATABASE_URL = "postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable"
go test -count=1 -tags=integration ./internal/preparation -run 'TestExecuteMutation.*Manager|TestWritePreparationAudits' -v
```

Expected: compile FAIL because `MutationSpec` has no Manager gate or audit-batch helper.

- [ ] **Step 4: Implement self re-authentication before fingerprint/replay**

Preserve executor ordering:

1. Reload session authority and current roles.
2. Verify required capability.
3. If required, lock actor identity by id, lock roles, re-check enabled + Manager + capability, and bcrypt-verify the supplied PIN.
4. Fingerprint the credential-free business input.
5. Advisory lock, replay/conflict lookup, claim, callback, audit, stored result, commit.

Map every expected Manager/PIN failure to `ErrForbidden` (or a dedicated sentinel included by `isSecurityDenial`) so denial evidence is committed and the client receives the collapsed `NOT_AUTHORIZED` response. Never include the attempted PIN in wrapped errors.

- [ ] **Step 5: Add one batch audit writer**

`writePreparationAudits` marshals each details payload, builds equal-length event/details arrays, and calls generated `InsertPreparationAuditEventsBatch` with one actor, session, and timestamp. Empty input is a no-op. Waste uses it for two different event types; correction uses it for one event type per unit. The existing executor still writes a single non-zero `AuditRecord` for commands needing one event.

- [ ] **Step 6: Run executor and regression tests**

```powershell
gofmt -w internal/preparation
$env:TEST_DATABASE_URL = "postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable"
go test -count=1 -tags=integration ./internal/preparation -run 'TestExecuteMutation|TestWritePreparationAudits' -v
go test ./internal/preparation
go vet ./internal/preparation
```

Expected: all PASS; existing advance and bulk commands still use the executor with no PIN requirement.

- [ ] **Step 7: Commit the executor checkpoint**

```powershell
git add internal/preparation/executor.go internal/preparation/executor_integration_test.go internal/preparation/env_integration_test.go
git commit -m "feat(preparation): verify manager self authorization"
```

---

### Task 4: Atomic Waste And Alert Creation

**Files:**
- Create: `internal/preparation/waste.go`
- Create: `internal/preparation/waste_integration_test.go`
- Modify: `internal/preparation/env_integration_test.go`

**Interfaces:**
- Produces `WasteUnitHandler`, `NewWasteUnitHandler`, and `Handle(ctx, actor, WasteUnitCommand)`.
- Uses `OpWasteUnit`, `PREPARATION_UNIT_WASTED`, and `PREPARATION_ALERT_CREATED`.

- [ ] **Step 1: Add direct-handler Waste fixture and fact readers**

Add `Waste`, `WasteWithRequestID`, `CountWastes`, `CountAlerts`, and unit transition/audit detail readers to `prepEnv`. Helpers must call exported handlers, not reproduce production SQL.

- [ ] **Step 2: Write failing Waste integration tests**

Cover:

- Success from `IN_PREPARATION` and `READY` with trimmed note.
- Rejection from `QUEUED`, `FULFILLED`, `CANCELLED`, and `WASTED`.
- Exact response fields and HTTP `201`.
- One Waste fact, `WASTED` current state, one typed transition, one active alert, two audits, and one stored result sharing one occurrence time.
- Same-input replay creates no duplicate fact, transition, alert, or audit.
- Conflicting request-id reuse returns `ErrRequestConflict`.
- Current capability is checked before replay.
- Forced second-audit failure and forced result-storage failure each roll back all writes and the idempotency claim.
- Two concurrent Waste requests for one unit produce one success and one lifecycle conflict, never a `500`.
- `TestAdvanceAgainstWaste` uses a lock barrier on one `IN_PREPARATION` unit and proves exactly one of advance-to-`READY` or Waste commits; the loser receives `ErrInvalidTransition`, and the final fact/transition/audit counts match the winner only.

- [ ] **Step 3: Run tests to verify they fail**

```powershell
$env:TEST_DATABASE_URL = "postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable"
go test -count=1 -tags=integration ./internal/preparation -run 'TestWaste|TestConcurrentWaste|TestAdvanceAgainstWaste' -v
```

Expected: compile FAIL because the handler is absent.

- [ ] **Step 4: Implement `WasteUnitHandler`**

Normalize and validate before `ExecuteMutation`. Use a fingerprint of unit id, reason, and normalized note. Inside the callback:

1. Lock the unit and map missing to `ErrUnitNotFound`.
2. Require `IN_PREPARATION|READY`.
3. Choose one `occurredAt`.
4. Insert Waste fact.
5. Set unit to `WASTED` without changing `in_preparation_at`.
6. Insert typed transition.
7. Insert unacknowledged `WASTE` alert.
8. Write both business audits through `writePreparationAudits`.
9. Load and return the complete Waste/Alert result with a zero executor `AuditRecord`.

Map only the named unique-Waste constraint to the expected transition conflict. Unknown constraint errors remain wrapped infrastructure errors.

- [ ] **Step 5: Run Waste and package tests**

```powershell
gofmt -w internal/preparation
$env:TEST_DATABASE_URL = "postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable"
go test -count=1 -race -tags=integration ./internal/preparation -run 'TestWaste|TestConcurrentWaste|TestAdvanceAgainstWaste' -v
go test ./internal/preparation
```

Expected: all PASS.

- [ ] **Step 6: Commit Waste**

```powershell
git add internal/preparation/waste.go internal/preparation/waste_integration_test.go internal/preparation/env_integration_test.go
git commit -m "feat(preparation): record wasted units"
```

---

### Task 5: Alert Acknowledgment

**Files:**
- Create: `internal/preparation/alerts.go`
- Create: `internal/preparation/alerts_integration_test.go`
- Modify: `internal/preparation/env_integration_test.go`

**Interfaces:**
- Produces `AcknowledgeAlertHandler`, `NewAcknowledgeAlertHandler`, and `Handle(ctx, actor, AcknowledgeAlertCommand)`.

- [ ] **Step 1: Write failing acknowledgment integration tests**

Cover:

- Manager and Barista acknowledgment fills actor, session, and timestamp together and returns `200`.
- Unknown alert is `ErrAlertNotFound`.
- A separately requested already-acknowledged alert is `ErrAlertAlreadyAcknowledged`.
- Exact replay returns original identity/time with one audit.
- Acknowledgment changes no unit state, Waste, charge, or closure readiness.
- An alert remains acknowledgeable after its Session closes.
- Two concurrent requests result in one acknowledgment and one expected conflict.
- Forced audit/result-storage failure restores all-null acknowledgment evidence.

- [ ] **Step 2: Run tests to verify they fail**

```powershell
$env:TEST_DATABASE_URL = "postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable"
go test -count=1 -tags=integration ./internal/preparation -run 'TestAcknowledge|TestConcurrentAcknowledge' -v
```

Expected: compile FAIL because the handler is absent.

- [ ] **Step 3: Implement the acknowledgment command**

Fingerprint only the path alert id. Inside `ExecuteMutation`, lock the alert, distinguish missing from already acknowledged, set all evidence fields with one timestamp, reload the response, and return one `PREPARATION_ALERT_ACKNOWLEDGED` audit. Do not lock or update the Preparation Unit or Session.

- [ ] **Step 4: Run focused and package tests**

```powershell
gofmt -w internal/preparation
$env:TEST_DATABASE_URL = "postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable"
go test -count=1 -tags=integration ./internal/preparation -run 'TestAcknowledge|TestConcurrentAcknowledge' -v
go test ./internal/preparation
```

Expected: all PASS.

- [ ] **Step 5: Commit acknowledgment**

```powershell
git add internal/preparation/alerts.go internal/preparation/alerts_integration_test.go internal/preparation/env_integration_test.go
git commit -m "feat(preparation): acknowledge active alerts"
```

---

### Task 6: Linked Remake Creation

**Files:**
- Create: `internal/preparation/remake.go`
- Create: `internal/preparation/remake_integration_test.go`
- Modify: `internal/preparation/env_integration_test.go`

**Interfaces:**
- Produces `RemakeUnitHandler`, `NewRemakeUnitHandler`, and `Handle(ctx, actor, RemakeUnitCommand)`.

- [ ] **Step 1: Add charge/snapshot/closure fixture assertions**

Add helpers that read an Order Item's Preparation Units, the owning Session state, Check charge/allocation/payment counts, and invoke Sales closure. Keep charge baseline assertions in tests before and after Remake.

- [ ] **Step 2: Write failing Remake integration tests**

Cover:

- A Wasted source produces one new `QUEUED` unit with next number, copied category/item/size/modifiers/note/service number, fresh queue time, `REMAKE` priority, and source link.
- Remake fact carries reason, normalized note, actor/session, and creation time.
- No Order Item, allocation, Check charge, Payment, Refund, or Comp changes.
- Source Waste alert stays active.
- Unknown Waste, closed Session, and already-remade Waste return typed errors.
- A Wasted Remake can produce the next chain link.
- Exact replay creates no duplicate unit/fact/audit; conflicting reuse fails.
- Two concurrent requests for one Waste produce one Remake.
- Concurrent Remakes from two Wastes of one Order Item receive distinct sequential unit numbers.
- Forced audit/result-storage failure rolls back the new unit and fact.

- [ ] **Step 3: Run tests to verify they fail**

```powershell
$env:TEST_DATABASE_URL = "postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable"
go test -count=1 -tags=integration ./internal/preparation -run 'TestRemake|TestConcurrentRemake' -v
```

Expected: compile FAIL because the handler is absent.

- [ ] **Step 4: Implement Session-first Remake locking**

Normalize reason/note before the executor. Resolve immutable source ids, then inside the mutation lock exactly:

1. Owning Service Session.
2. Owning Order Item.
3. Waste and source Preparation Unit.

Revalidate the Session is `ACTIVE`, Waste still exists, and no Remake exists. Allocate `max(unit_number)+1` while holding the Order Item lock. Insert the copied replacement, insert the Remake fact, load `UnitResponse`, and return `201` with one `PREPARATION_REMAKE_CREATED` audit.

Map only `preparation_remake_waste_unique` to `ErrWasteAlreadyRemade`. A closed Session is a conflict even on a first execution; exact replay still returns before callback after current authority verification.

- [ ] **Step 5: Add Remake-versus-closure concurrency test**

Name the test `TestRemakeAgainstClosure`. Use a test-only trigger or lock barrier to hold one side after the Session lock. Assert either:

- Closure commits first and Remake returns `ErrServiceSessionClosed`, or
- Remake commits first and closure returns `ErrUnfulfilledPreparationForClosure`.

Never accept both successful closure and a new queued Remake.

- [ ] **Step 6: Run focused race tests and package tests**

```powershell
gofmt -w internal/preparation
$env:TEST_DATABASE_URL = "postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable"
go test -count=1 -race -tags=integration ./internal/preparation -run 'TestRemake|TestConcurrentRemake' -v
go test ./internal/preparation
```

Expected: all PASS.

- [ ] **Step 7: Commit Remake**

```powershell
git add internal/preparation/remake.go internal/preparation/remake_integration_test.go internal/preparation/env_integration_test.go
git commit -m "feat(preparation): create linked remakes"
```

---

### Task 7: Atomic State Correction

**Files:**
- Create: `internal/preparation/correct_state.go`
- Create: `internal/preparation/correct_state_test.go`
- Create: `internal/preparation/correct_state_integration_test.go`
- Modify: `internal/preparation/env_integration_test.go`

**Interfaces:**
- Produces `CorrectStateHandler`, `NewCorrectStateHandler`, and `Handle(ctx, actor, CorrectStateCommand)`.
- Produces `correctStateFingerprint` with no Manager PIN.

- [ ] **Step 1: Write failing pure fingerprint/order tests**

Assert request order is preserved in the fingerprint and response, a separate UUID-sorted copy drives locks, input is not mutated, duplicates are rejected, and marshaled fingerprints contain no credential field/value.

- [ ] **Step 2: Write failing correction integration tests**

Cover:

- All three legal pairs and exact response fields.
- One timestamp across a batch; one fact, transition, and audit per selected unit.
- Correction to `QUEUED` clears `in_preparation_at`; targets `IN_PREPARATION|READY` preserve it.
- One and fifty units succeed; zero, 51, duplicate, and nil ids fail at the boundary.
- Missing unit, stale state, exceptional terminal state, skipped reverse step, mixed owning Sessions with one closed, and closed Session reject the entire batch with no writes.
- Manager with correct own PIN succeeds; Barista, Cashier, wrong PIN, disabled actor, removed Manager role, and removed capability fail.
- Exact replay requires current authority/current PIN and returns the original response with no duplicate writes.
- PIN rotation permits replay only with the new PIN.
- Forced audit or result-storage failure rolls back all units, facts, transitions, audits, and claim.
- Overlapping correction batches lock deterministically and remain atomic.

- [ ] **Step 3: Run tests to verify they fail**

```powershell
go test ./internal/preparation -run 'TestCorrectStateFingerprint|TestValidateCorrectState' -v
$env:TEST_DATABASE_URL = "postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable"
go test -count=1 -tags=integration ./internal/preparation -run 'TestCorrectState|TestOverlappingCorrectState' -v
```

Expected: compile FAIL because correction orchestration is absent.

- [ ] **Step 4: Implement all-or-nothing correction**

Validate/normalize before calling `ExecuteMutation`. Configure `MutationSpec` with `RequireManagerPIN`, the supplied credential, `preparation.operate`, and a credential-free normalized fingerprint.

Inside the callback:

1. Resolve every unit and owning Session without locks; reject missing ids.
2. Deduplicate Session ids and lock Sessions in UUID byte order.
3. Require every Session `ACTIVE`.
4. Lock units in UUID byte order.
5. Revalidate every current state against `RequiredPriorState(target)`.
6. Choose one timestamp.
7. For each locked unit, update current state, insert reverse transition, insert correction fact, and queue an audit record.
8. Batch-write all audits.
9. Return outcomes in original request order with a zero executor audit.

Do not use `MutationContext.withUnitSavepoint`; any unit or infrastructure failure aborts the complete command.

- [ ] **Step 5: Add correction-versus-advance and correction-versus-closure races**

Test explicit lock barriers. Advance versus correction may have one winner, but the loser must receive a lifecycle conflict and never move from an unvalidated state. Name the closure race `TestCorrectionAgainstClosure`; start from an otherwise closure-ready Session whose selected unit is `FULFILLED`, and race closure against correction to `READY`. The only accepted outcomes are closed-before-correction rejection or correction-before-closure `ErrUnfulfilledPreparationForClosure`.

- [ ] **Step 6: Run focused race and regression tests**

```powershell
gofmt -w internal/preparation
$env:TEST_DATABASE_URL = "postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable"
go test -count=1 -race -tags=integration ./internal/preparation -run 'TestCorrectState|TestOverlappingCorrectState|TestAdvanceAgainstCorrection|TestCorrectionAgainstClosure' -v
go test ./internal/preparation
go vet ./internal/preparation
```

Expected: all PASS.

- [ ] **Step 7: Commit State Correction**

```powershell
git add internal/preparation/correct_state.go internal/preparation/correct_state_test.go internal/preparation/correct_state_integration_test.go internal/preparation/env_integration_test.go
git commit -m "feat(preparation): correct unit states atomically"
```

---

### Task 8: Queue Alerts, Priority, And Correction History

**Files:**
- Modify: `internal/preparation/queue.go`
- Modify: `internal/preparation/queue_integration_test.go`
- Modify: `internal/preparation/dto.go`
- Modify: `internal/preparation/preparation_integration_test.go`

**Interfaces:**
- Changes `QueueResponse` to `{ObservedAt, Units, Alerts, Corrections}`.
- Projects active alerts oldest first and recent Waste/Remake facts newest first.

- [ ] **Step 1: Write failing queue projection tests**

Add tests for:

- Empty `units`, `alerts`, and `corrections` are non-nil and JSON `[]`.
- Active Remakes precede active Standard units; each lane remains FIFO with id tie-break.
- Alert-retained Wasted units follow all active work even when their priority is `REMAKE`.
- A Wasted unit and active alert remain visible until acknowledgment, then both disappear.
- Active alerts are oldest first, include projected unit identity and `waste_id`, and carry null acknowledgment fields.
- Two active alerts on one terminal unit produce two alert rows but exactly one queue-unit row; reserved `CANCELLATION|CHANGE` alerts have null `waste_id`, while only the `WASTE` alert resolves it.
- Acknowledged alerts never appear.
- Waste/Remake history is newest first, capped at 50, set based, and includes only active Sessions.
- Remake/source ids and unit numbers map correctly.
- The read remains one repeatable snapshot under a concurrent acknowledgment/Remake.

- [ ] **Step 2: Extend raw HTTP privacy and exact-key tests before implementation**

Update `TestPreparationHTTPQueueUsesEmptyArrays` and `TestPreparationHTTPQueuePrivacyContract`:

- Require all three arrays.
- Add `priority` and `remake_of_preparation_unit_id` to allowed unit keys.
- Assert alert/history key sets.
- Continue recursively rejecting `price`, `allocation`, `check`, `payment`, `balance`, `sales_shift`, `pin`, `pin_hash`, and `token_hash` fragments.

- [ ] **Step 3: Run queue tests to verify they fail**

```powershell
$env:TEST_DATABASE_URL = "postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable"
go test -count=1 -tags=integration ./internal/preparation -run 'TestActiveQueue|TestPreparationHTTPQueue' -v
```

Expected: compile/assertion FAIL because queue alerts/history are not mapped.

- [ ] **Step 4: Extend the queue mapper**

Allocate all collections before queries. In the existing `ExecuteRead` callback, read observed time, units, tables, active alerts, and recent corrections through generated set-based queries. Build maps only for projection joins; do not issue per-row queries.

Use explicit mapper functions for nullable fields and discriminator-specific history rows. Treat an unknown discriminator or malformed nullable combination as an internal error rather than emitting a partial contract.

- [ ] **Step 5: Run queue, HTTP, and package tests**

```powershell
gofmt -w internal/preparation
$env:TEST_DATABASE_URL = "postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable"
go test -count=1 -tags=integration ./internal/preparation -run 'TestActiveQueue|TestPreparationHTTPQueue' -v
go test ./internal/preparation
```

Expected: all PASS.

- [ ] **Step 6: Commit queue recovery projection**

```powershell
git add internal/preparation/queue.go internal/preparation/queue_integration_test.go internal/preparation/dto.go internal/preparation/preparation_integration_test.go
git commit -m "feat(preparation): project recovery queue"
```

---

### Task 9: Sales And Completed Sale Remake Projection

**Files:**
- Modify: `internal/sales/dto.go`
- Modify: `internal/sales/projection.go`
- Modify: `internal/sales/dto_test.go`
- Modify: `internal/sales/projection_integration_test.go`
- Modify: `internal/sales/session_close_integration_test.go`
- Modify: `internal/sales/submit_integration_test.go`

**Interfaces:**
- Changes `PreparationUnitResponse` with `Priority string` and `RemakeOfPreparationUnitID *uuid.UUID`.
- Reuses `ListSessionPreparationUnits` for both live and Completed Sale projection.

- [ ] **Step 1: Write failing Sales projection tests**

Add tests proving:

- `TestPreparationRemakeProjection`: original submitted units project `STANDARD` and null source link, while a Remake appears in the live Service Session with `REMAKE` and exact source id.
- Creating a Remake changes no Check charge, allocation quantity, or payment.
- `TestCloseServiceSessionWithRemake`: a Wasted source plus fulfilled Remake allows closure, while an active Remake blocks closure through existing `EvaluateClosureReadiness`.
- `TestCompletedSalePreparationRecoveryHistory`: Completed Sale contains both units, their priority/link metadata, and forward/Waste/reverse-correction transition history in occurrence order.
- Empty and populated JSON contracts do not leak Preparation alerts, PIN, or financial fields into unit objects.

- [ ] **Step 2: Run Sales tests to verify they fail**

```powershell
$env:TEST_DATABASE_URL = "postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable"
go test -count=1 -tags=integration ./internal/sales -run 'TestPreparationRemakeProjection|TestCloseServiceSessionWithRemake|TestCompletedSalePreparationRecoveryHistory' -v
```

Expected: compile/assertion FAIL because `PreparationUnitResponse` lacks metadata.

- [ ] **Step 3: Map additive unit metadata**

Extend `PreparationUnitResponse` and `loadPreparationUnits`. Preserve non-nil modifiers and existing ordering. Do not add alerts or correction-history collections to Sales projections; only unit metadata and the already-existing typed transition history cross this boundary.

- [ ] **Step 4: Run Sales and cross-slice regression tests**

```powershell
gofmt -w internal/sales
$env:TEST_DATABASE_URL = "postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable"
go test -count=1 -tags=integration ./internal/sales ./internal/preparation -run 'TestPreparationRemakeProjection|TestCloseServiceSessionWithRemake|TestCompletedSalePreparationRecoveryHistory|TestSubmit|TestActiveQueue' -v
go test ./internal/sales ./internal/preparation
```

Expected: all PASS; closure policy itself needs no code change because `WASTED` is already terminal and a Remake is a real unit.

- [ ] **Step 5: Commit Sales projection support**

```powershell
git add internal/sales/dto.go internal/sales/projection.go internal/sales/dto_test.go internal/sales/projection_integration_test.go internal/sales/session_close_integration_test.go internal/sales/submit_integration_test.go
git commit -m "feat(sales): project preparation remakes"
```

---

### Task 10: HTTP Routes, Validation, Authorization, And Swagger

**Files:**
- Modify: `internal/preparation/routes.go`
- Modify: `internal/preparation/http.go`
- Modify: `internal/preparation/preparation_integration_test.go`
- Create: `internal/preparation/swagger_test.go`
- Regenerate: `docs/docs.go`
- Regenerate: `docs/swagger.json`
- Regenerate: `docs/swagger.yaml`

**Interfaces:**
- Adds the four mutation routes from spec Section 4.3.
- Keeps middleware and transactional authorization checks on every route.

- [ ] **Step 1: Write failing HTTP route tests**

Extend the authenticated test server and add subtests for each route:

- Manager and Barista may Waste, Remake, and acknowledge; Cashier gets `403`; anonymous gets `401`.
- Only Manager with own current PIN may correct; Barista/Cashier/wrong PIN get `403`.
- Malformed path UUIDs, JSON, request ids, enums, notes, PIN shape, duplicate ids, and batch bounds get `400 INVALID_INPUT` without database writes.
- Unknown unit/alert/Waste gets the operation-specific `404` code.
- Lifecycle conflicts and request-id conflicts return stable `409` codes.
- Success envelopes and `201|200` statuses decode into exact response DTOs.
- Replay with a rotated PIN follows the executor contract.

- [ ] **Step 2: Run HTTP tests to verify they fail**

```powershell
$env:TEST_DATABASE_URL = "postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable"
go test -count=1 -tags=integration ./internal/preparation -run 'TestPreparationHTTP.*(Waste|Remake|Acknowledge|Correct|Authorization|Validation)' -v
```

Expected: FAIL because handlers/routes are not wired.

- [ ] **Step 3: Wire slices and routes**

Add four handler fields to `Slices`, construct them from the shared `Runner`, and register:

```text
POST /preparation/alerts/:alert_id/acknowledge
POST /preparation/units/:unit_id/waste
POST /preparation/wastes/:waste_id/remake
POST /preparation/units/correct-state
```

Apply `RequireAuth` and `RequireCapability(CapPreparationOperate)` to all. Do not add a middleware-only Manager gate; the current-role and PIN checks belong inside the transaction.

- [ ] **Step 4: Add boundary validation and Swagger annotations**

Use `parseUUIDParam`, generic `bindBody`, `checkRequestID`, `auth.ValidatePinFormat`, and domain validators. Normalize reasons/notes before handler execution. Ensure validation errors wrap `response.ErrInvalid`, while business errors flow through `sendError` and `preparation.ErrorResponse`.

Document security, request/response DTOs, and `400|401|403|404|409|500` outcomes for every route. Update the advance description so Waste/Remake/State Correction are no longer described as future work; Cancellation remains Phase 6C.

- [ ] **Step 5: Add an automated generated Swagger contract test**

Create `internal/preparation/swagger_test.go` in package `preparation_test`. Load `../../docs/swagger.json`, decode it, and assert all four Phase 6B paths expose the expected POST operation, BearerAuth, success status, `400|401|403|404|409|500` responses, and request body schema. Recursively inspect response definitions to prove no response field contains `manager_pin`, `pin`, or `pin_hash`.

- [ ] **Step 6: Regenerate and inspect Swagger**

```powershell
swag init -g cmd/api/main.go -o docs
git diff -- docs/docs.go docs/swagger.json docs/swagger.yaml
```

Expected: four paths appear with BearerAuth and documented response schemas; no PIN appears in a response schema.

- [ ] **Step 7: Run HTTP, Swagger, and package tests**

```powershell
gofmt -w internal/preparation
$env:TEST_DATABASE_URL = "postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable"
go test ./internal/preparation -run TestPreparationSwaggerCorrectionRoutes -v
go test -count=1 -tags=integration ./internal/preparation -run 'TestPreparationHTTP' -v
go test ./internal/preparation
go vet ./internal/preparation
```

Expected: all PASS.

- [ ] **Step 8: Commit the HTTP surface**

```powershell
git add internal/preparation/routes.go internal/preparation/http.go internal/preparation/preparation_integration_test.go internal/preparation/swagger_test.go docs/docs.go docs/swagger.json docs/swagger.yaml
git commit -m "feat(preparation): expose correction routes"
```

---

### Task 11: End-To-End Races, Decisions, Roadmap, And Full Verification

**Files:**
- Modify: `internal/preparation/corrections_e2e_integration_test.go` or the focused command integration files
- Modify: `internal/sales/session_close_integration_test.go`
- Modify: `spec/decisions.md`
- Modify: `MIGRATE_PLAN.md`
- Verify: all Phase 6B files and generated artifacts

- [ ] **Step 1: Add end-to-end recovery workflows**

Cover through real handlers:

- Submit -> advance -> Waste -> acknowledge -> Remake -> fulfill -> close -> read Completed Sale.
- Submit -> advance -> correct backward -> re-advance -> close.
- Wasted Remake -> chained Remake -> fulfill -> close.

Assert facts, queue visibility/order, unchanged money, terminal readiness, metadata, and complete typed history at each boundary.

- [ ] **Step 2: Run the complete focused race matrix**

```powershell
$env:TEST_DATABASE_URL = "postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable"
go test -count=1 -race -tags=integration ./internal/preparation -run 'TestConcurrentAdvance|TestOverlappingBulkAdvance|TestConcurrentWaste|TestAdvanceAgainstWaste|TestConcurrentAcknowledge|TestConcurrentRemake|TestOverlappingCorrectState|TestAdvanceAgainstCorrection|TestRemakeAgainstClosure|TestCorrectionAgainstClosure' -v
```

Expected: all PASS with no race report, deadlock, partial batch, duplicate unit number, or unexpected `500`.

- [ ] **Step 3: Record ADR-036 through ADR-039**

Append the approved decisions from spec Section 13 to `spec/decisions.md` using the existing Context/Decision/Consequences format:

- ADR-036: typed correction facts and typed transition history.
- ADR-037: linked Remake as the only launch priority.
- ADR-038: atomic one-step reverse correction with Manager self re-authentication.
- ADR-039: acknowledgment controls terminal-unit queue visibility only.

- [ ] **Step 4: Update the Phase 6 roadmap**

In `MIGRATE_PLAN.md`:

- Link the approved 6B design and this implementation plan.
- Mark each implemented 6B schema, command, queue, authorization, projection, HTTP, concurrency, and test item complete.
- Change the 6B row to completed with the actual implementation date.
- Leave 6C pending and explicitly keep Cancellation/change, Refund, Comp, Payment Void, charge adjustment, and pending-refund closure policy there.
- Update the Phase 6 progress count without marking Phase 6 as a whole complete.

- [ ] **Step 5: Run generation-drift and formatting checks**

```powershell
sqlc generate
swag init -g cmd/api/main.go -o docs
gofmt -w internal/preparation internal/sales internal/database/sqlc
git diff --check
git status --short
```

Expected: regeneration introduces no unexpected diff; `git diff --check` is silent.

- [ ] **Step 6: Run all quality gates**

```powershell
go build ./...
go vet ./...
golangci-lint run ./...
go test -race ./...
$env:TEST_DATABASE_URL = "postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable"
go test -count=1 -race -tags=integration ./...
```

Expected: every command exits 0.

- [ ] **Step 7: Inspect the final boundary**

```powershell
git diff --stat
git diff -- internal/preparation internal/sales sql/queries internal/database/migrations spec/decisions.md MIGRATE_PLAN.md
git grep -n -E 'manager_pin|pin_hash' -- internal/preparation sql/queries/preparation.sql
git grep -n -E 'refund|comp|CANCELLED' -- internal/preparation
```

Expected:

- `manager_pin` appears only in request DTO/binding and executor input, never in fingerprint structs, audits, SQL facts, responses, or logs.
- No Phase 6B command writes `CANCELLED`, Refund, Comp, Payment Void, or charge data.
- The only `CANCELLED` references are complete-domain state/alert visibility declarations and explicit tests proving no 6B writer exists.
- No event bus consumer, SSE, WebSocket, general rush priority, repository abstraction, or unrelated executor refactor appears.

- [ ] **Step 8: Commit decisions and final regressions**

```powershell
git add internal/preparation internal/sales internal/database/migrations internal/database/sqlc sql/queries docs spec/decisions.md MIGRATE_PLAN.md
git commit -m "docs(preparation): complete Phase 6B recovery"
```

## Verification Checklist

- [ ] Migration backfills `STANDARD`, enforces priority/link pairing, typed fact integrity, and exactly the eight Phase 6B transition pairs.
- [ ] Waste atomically writes fact, state, transition, active alert, two audits, and replay result.
- [ ] Acknowledgment records complete evidence and only controls active alert/terminal-unit visibility.
- [ ] Remake copies the immutable snapshot, allocates a unique next number, links its source, receives priority, and changes no charge.
- [ ] State Correction is Manager-only, own-PIN-gated before replay, one-step, 1-50 units, deterministic, and atomic.
- [ ] PIN rotation/revocation tests prove current authorization controls replay without putting credentials in fingerprints or persistence.
- [ ] Queue returns non-null units/alerts/corrections from one repeatable snapshot with deterministic ordering and no financial leakage.
- [ ] Service Session and Completed Sale project priority/link metadata and complete typed transition history.
- [ ] Remake/correction serialize with closure; unit-level races have one valid winner and typed loser outcomes.
- [ ] Cancellation/change writers and all financial correction work remain in Phase 6C.
- [ ] sqlc and Swagger artifacts regenerate cleanly.
- [ ] Build, vet, lint, unit, integration, and focused race suites pass.
