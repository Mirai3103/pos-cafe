# Sales Shift Closure And Reconciliation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a server-enforced blind-count Sales Shift reconciliation workflow that safely closes a Shift, preserves immutable evidence, and exposes Manager-only closed-Shift history.

**Architecture:** Phase 07 remains in `internal/shift`. A new `CLOSING` state and normalized reconciliation, attempt, closure, and discrepancy facts form the durable workflow; the Shift row is the writer gate. `internal/sales` makes one targeted Session Start locking change so an active Session cannot appear after Shift closure validates blockers.

**Tech Stack:** Go 1.27, Echo v4, PostgreSQL 16 through `database/sql`/pgx, sqlc v1.31.1, embedded SQL migrations, testify, Swagger.

**Spec:** `docs/superpowers/specs/2026-09-18-shift-closure-reconciliation-design.md`

## Global Constraints

- Follow ADR-048: reload identity, session, roles, and capabilities inside every domain transaction before idempotency replay.
- Use the existing Shift `ExecuteMutation`/`ExecuteRead` executors; do not create a shared executor or a new reconciliation package.
- All financial and commercial facts are append-only; no command edits an attempt, closure, discrepancy, Payment, Refund, or Cash Movement.
- Money is whole VND in `BIGINT`; validate non-negative observed values and use checked arithmetic for every aggregate and signed difference.
- A Shift state is only `OPEN`, `CLOSING`, or `CLOSED`; no route restores `CLOSING` to `OPEN` or reopens `CLOSED`.
- At most one `OPEN` or `CLOSING` Shift may exist.
- Before the initial count commits, no Shift API or error exposes aggregate Expected Cash or aggregate source totals. A response may echo a value supplied by its actor.
- `CLOSING` and closed snapshots must never be recalculated from unrestricted live facts.
- Closure blockers are global and ordered: unsettled Check, pending Refund, unresolved correction, active Service Session.
- `CASH_COUNT_DIFFERENCE` is valid only for Cash; `QR_OBSERVATION_DIFFERENCE` only for QR dimensions; `UNEXPLAINED` and `OTHER` are valid for all dimensions; `OTHER` requires a 1-500 rune trimmed note.
- A nonzero Cash discrepancy requires two Cash Counts; either nonzero QR discrepancy requires two full QR Observations; one fresh Manager Approval covers the entire final discrepancy set.
- Approver login and PIN are request-only: exclude them from fingerprints, records, stored responses, Audit Events, logs, and Swagger examples containing real secrets.
- All arrays serialize as `[]`, never `null`; all database and HTTP reads use the established snapshot/authorization conventions.
- Do not hand-edit `internal/database/sqlc`; run `make sqlc` after changing `sql/queries`.
- Use `make test` for unit/race checks and `make test-integration` for PostgreSQL integration/race checks. Start PostgreSQL with `make docker-up` when needed.

---

## File Structure

| File | Responsibility |
| --- | --- |
| `internal/database/migrations/000015_add_shift_closure_reconciliation.sql` | States, active-Shift index, append-only Phase 07 tables, constraints, and indexes. |
| `sql/queries/shift.sql` | Shift lock/state, reconciliation persistence/read, global blockers, closure/history queries. |
| `sql/queries/sales.sql` | Keep Session Start's open-Shift shared lock available to the Sales slice. |
| `internal/shift/domain.go` | States, operation names, discrepancy enums, reason/attempt validation, checked differences. |
| `internal/shift/dto.go` | Commands and redacted/open, closing, preview, and closed-history DTOs. |
| `internal/shift/reconciliation.go` | Start Reconciliation and shared snapshot/blocker loading. |
| `internal/shift/attempts.go` | Append Cash Count and QR Observation commands. |
| `internal/shift/close.go` | Exact/discrepant Final Close, latest-evidence and approval checks. |
| `internal/shift/current.go` | Redacted `OPEN` read and frozen `CLOSING` projection. |
| `internal/shift/history.go` | `audit.inspect` list/detail reads and cursor codec. |
| `internal/shift/http.go`, `routes.go`, `errors.go` | Route bindings, input validation, Swagger, and typed error mapping. |
| `internal/sales/service_number.go` | Replace non-locking open-Shift lookup for Session Start with `FOR SHARE`. |
| `internal/shift/*_integration_test.go` | Schema, command, HTTP, idempotency, and race coverage. |
| `internal/sales/*_integration_test.go` | Session Start versus reconciliation-start race coverage. |

## Checkpoint 7A: Persistence And Gate

### Task 1: Define the Phase 07 domain vocabulary and redacted DTO boundary

**Files:**
- Modify: `internal/shift/domain.go`
- Modify: `internal/shift/dto.go`
- Modify: `internal/shift/domain_test.go`
- Modify: `internal/shift/shift_integration_test.go`

**Interfaces:**
- Produces `StateClosing`, `DiscrepancyDimension`, `DiscrepancyReason`, `ValidateDiscrepancyReason`, `ComputeDifference`, `OpenCurrentShiftResponse`, `ReconciliationPreview`, `ClosingShiftResponse`, and `ClosedShiftDetailResponse` for later handlers.
- Changes `CashMovementResult` to contain only `Movement CashMovementResponse`.

- [ ] **Step 1: Write failing domain and JSON boundary tests**

Add table-driven `TestValidateDiscrepancyReason` cases for valid Cash/QR pairings, invalid cross-dimension pairings, `OTHER` with trimmed note, and non-`OTHER` with a note. Add `TestComputeDifference` cases for zero, excess, shortage, and `math.MinInt64 - 1` overflow. Replace the existing current-read HTTP assertion with a raw JSON assertion that an `OPEN` Shift contains only `id`, `state`, `opened_at`, and `opener`, and that Cash Movement JSON contains no `expected_cash_vnd`.

```go
cases := []struct {
    dimension string
    reason    string
    note      *string
    wantErr   bool
}{
    {DimensionCash, ReasonCashCountDifference, nil, false},
    {DimensionManualQRReceived, ReasonCashCountDifference, nil, true},
    {DimensionManualQRRefunded, ReasonQRObservationDifference, nil, false},
    {DimensionCash, ReasonOther, strPtr("drawer seal broken"), false},
}
```

- [ ] **Step 2: Run focused tests to verify failure**

Run: `go test ./internal/shift -run 'Test(ValidateDiscrepancyReason|ComputeDifference)'`

Expected: FAIL because the Phase 07 constants, validators, and difference helper do not exist.

- [ ] **Step 3: Add constants, validators, arithmetic, and DTO types**

In `domain.go`, add these stable identifiers and use `subtractAmount`-style checked arithmetic for the signed difference:

```go
const (
    StateOpen    = "OPEN"
    StateClosing = "CLOSING"
    StateClosed  = "CLOSED"

    DimensionCash              = "CASH"
    DimensionManualQRReceived  = "MANUAL_QR_RECEIVED"
    DimensionManualQRRefunded  = "MANUAL_QR_REFUNDED"
    ReasonCashCountDifference  = "CASH_COUNT_DIFFERENCE"
    ReasonQRObservationDifference = "QR_OBSERVATION_DIFFERENCE"
    ReasonUnexplained          = "UNEXPLAINED"
    ReasonOther                = "OTHER"
)

func ComputeDifference(observed, expected int64) (int64, error) {
    if (expected < 0 && observed > math.MaxInt64+expected) ||
        (expected > 0 && observed < math.MinInt64+expected) {
        return 0, ErrExpectedCashOutOfRange
    }
    return observed - expected, nil
}
```

Split `SalesShiftResponse` into reusable Shift metadata and separate `OpenCurrentShiftResponse`, so opening a Shift can continue returning its own Opening Float while the current read cannot accidentally embed it. Define preview entries with pointer `ObservedVND` and `DifferenceVND`; initialize every collection in response constructors with `make(..., 0)`.

- [ ] **Step 4: Remove the OPEN-money response contract**

Change `CashMovementResult` to:

```go
type CashMovementResult struct {
    Movement CashMovementResponse `json:"movement"`
}
```

Update the existing Cash Movement handler only enough to stop loading totals and Expected Cash after insert. Keep its Shift `FOR UPDATE`, approval, audit, and idempotency behavior unchanged.

- [ ] **Step 5: Run focused unit and HTTP tests**

Run: `go test ./internal/shift -run 'Test(ValidateDiscrepancyReason|ComputeDifference|ShiftHTTPHappyPath|ShiftHTTPSerializesEmptyListsAsArrays)'`

Expected: PASS after updating the old assertions to the redacted contract.

- [ ] **Step 6: Commit the domain boundary**

```bash
git add internal/shift/domain.go internal/shift/dto.go internal/shift/domain_test.go internal/shift/shift_integration_test.go internal/shift/cash_movement.go
git commit -m "feat(shift): define reconciliation domain boundary"
```

### Task 2: Add the durable reconciliation schema and generated SQL bindings

**Files:**
- Create: `internal/database/migrations/000015_add_shift_closure_reconciliation.sql`
- Modify: `sql/queries/shift.sql`
- Modify: `internal/database/sqlc/*.go` via `make sqlc`
- Modify: `internal/shift/schema_integration_test.go`
- Modify: `internal/shift/executor_integration_test.go`

**Interfaces:**
- Produces sqlc methods `LockSalesShiftForReconciliation`, `InsertShiftReconciliation`, `InsertShiftCashCount`, `InsertShiftQRObservation`, `GetGlobalShiftClosureBlockers`, `GetReconciliationSnapshot`, `GetLatestReconciliationEvidence`, `InsertShiftClosure`, `InsertShiftDiscrepancies`, `ListClosedShiftSummaries`, and `GetClosedShiftDetail`.
- The generated `sqlc.Queries` methods are consumed by Tasks 4-8.

- [ ] **Step 1: Write migration-schema tests before creating the migration**

Add integration tests that attempt each invalid database state directly: two active Shifts, a duplicate reconciliation, duplicate count sequence, negative Cash Count, negative QR observation, a closure with exact differences plus approver, a nonzero difference without approver, a zero discrepancy row, an invalid reason/dimension pair, and a non-`OTHER` note.

```go
_, err := db.Exec(`INSERT INTO shift_discrepancies (
    shift_closure_id, dimension, expected_vnd, observed_vnd,
    difference_vnd, reason, note
) VALUES ($1, 'CASH', 100, 90, -10, 'QR_OBSERVATION_DIFFERENCE', NULL)`, closureID)
require.Error(t, err)
```

- [ ] **Step 2: Run schema tests to verify failure**

Run: `make test-integration-fast TEST_DATABASE_URL="$TEST_DATABASE_URL"` with `-run TestShiftClosureSchema` appended to the underlying `go test` command.

Expected: FAIL because the tables and constraints do not exist.

- [ ] **Step 3: Create migration `000015_add_shift_closure_reconciliation.sql`**

Alter the Shift state check, drop `sales_shift_only_one_open_unique`, and recreate it as:

```sql
CREATE UNIQUE INDEX sales_shift_only_one_active_unique
    ON sales_shifts ((true))
    WHERE state IN ('OPEN', 'CLOSING');
```

Create `shift_reconciliations`, `shift_cash_counts`, `shift_qr_observations`, `shift_closures`, and `shift_discrepancies` exactly as section 5 of the approved spec requires. Use named checks for every enum, amount bound, sequence, reason/note, approver/difference, and difference equation. Add the ordered history index:

```sql
CREATE INDEX shift_closures_closed_at_id_desc_index
    ON shift_closures (closed_at DESC, id DESC);
```

Use `ON DELETE RESTRICT` for every evidence foreign key. Do not use triggers, views, or JSON columns.

- [ ] **Step 4: Add all Phase 07 SQL queries**

In `sql/queries/shift.sql`, write named, parameterized queries for the new data model. Make the global correction CTE distinct from `GetShiftReconciliationTotals`: it must aggregate all LIVE_CHECK adjusted Checks and all POST_SALE adjustment capacity without a Shift id filter. Return all blocker booleans/counts/amounts in one `GetGlobalShiftClosureBlockers` row so Go chooses precedence without a race between separate reads.

Use an insert pattern that returns the database timestamp and id:

```sql
-- name: InsertShiftCashCount :one
INSERT INTO shift_cash_counts (
    reconciliation_id, sequence, counted_cash_vnd,
    counted_by_staff_identity_id, counted_staff_access_session_id
) VALUES ($1, $2, $3, $4, $5)
RETURNING id, sequence, counted_cash_vnd, counted_at;
```

- [ ] **Step 5: Regenerate sqlc and compile**

Run: `make sqlc && go test ./internal/shift -run '^$'`

Expected: sqlc exits 0 and the Shift package compiles against the generated query signatures.

- [ ] **Step 6: Run migration schema tests**

Run: `TEST_DATABASE_URL="$TEST_DATABASE_URL" go test -count=1 -tags=integration ./internal/shift -run TestShiftClosureSchema`

Expected: PASS; each direct invalid insert is rejected by its named database constraint.

- [ ] **Step 7: Commit persistence**

```bash
git add internal/database/migrations/000015_add_shift_closure_reconciliation.sql sql/queries/shift.sql internal/database/sqlc internal/shift/schema_integration_test.go internal/shift/executor_integration_test.go
git commit -m "feat(shift): add reconciliation persistence"
```

### Task 3: Gate Session Start with the open Shift shared lock

**Files:**
- Modify: `internal/sales/service_number.go`
- Modify: `internal/sales/session_start.go`
- Modify: `internal/sales/sales_integration_test.go`
- Modify: `internal/sales/session_start_integration_test.go` or create it if this behavior has no focused file

**Interfaces:**
- Changes `requireOpenSalesShift(ctx, q)` to `lockOpenSalesShift(ctx, q)` returning `(uuid.UUID, error)`.
- Consumes generated `LockOpenSalesShiftForShare(ctx)` and leaves `allocateServiceNumber` unchanged.

- [ ] **Step 1: Write the Session Start lock test**

Open a Shift, start a transaction that locks it `FOR UPDATE`, launch Takeaway Session Start, wait until PostgreSQL reports the worker blocked, release the lock, then assert the Session starts only while the Shift remains `OPEN`. In a second subtest, update the locked Shift to `CLOSING` before release and assert `OPEN_SALES_SHIFT_REQUIRED` with no inserted Session.

- [ ] **Step 2: Run the focused test to verify failure**

Run: `TEST_DATABASE_URL="$TEST_DATABASE_URL" go test -count=1 -tags=integration ./internal/sales -run TestSessionStartLocksOpenShift`

Expected: FAIL because Session Start's current `GetOpenSalesShiftID` does not block on the Shift row.

- [ ] **Step 3: Replace the non-locking lookup**

Implement:

```go
func lockOpenSalesShift(ctx context.Context, q *sqlc.Queries) (uuid.UUID, error) {
    id, err := q.LockOpenSalesShiftForShare(ctx)
    if errors.Is(err, sql.ErrNoRows) {
        return uuid.Nil, ErrOpenShiftRequired
    }
    if err != nil {
        return uuid.Nil, fmt.Errorf("lock open sales shift: %w", err)
    }
    return id, nil
}
```

Call it before table validation/allocation in both Takeaway and Dine-in handlers. Update the generated query predicate to accept only `OPEN`; `CLOSING` must not start a Session.

- [ ] **Step 4: Run Sales tests**

Run: `TEST_DATABASE_URL="$TEST_DATABASE_URL" go test -count=1 -race -tags=integration ./internal/sales -run 'Test(SessionStartLocksOpenShift|SalesHTTPHappyPath|SalesHTTPDenies)'`

Expected: PASS.

- [ ] **Step 5: Commit Session Start gate**

```bash
git add internal/sales/service_number.go internal/sales/session_start.go internal/sales/*session_start*_integration_test.go sql/queries/sales.sql internal/database/sqlc
git commit -m "fix(sales): lock shift while starting session"
```

## Checkpoint 7B: Reconciliation And Closure

### Task 4: Implement Reconciliation Start and frozen snapshot loading

**Files:**
- Create: `internal/shift/reconciliation.go`
- Create: `internal/shift/reconciliation_integration_test.go`
- Modify: `internal/shift/errors.go`
- Modify: `internal/shift/routes.go`
- Modify: `internal/shift/http.go`

**Interfaces:**
- Produces `StartReconciliationHandler`, `NewStartReconciliationHandler`, and `StartReconciliationCommand`.
- Produces `loadClosureBlockers(ctx, q)` and `loadReconciliationSnapshot(ctx, q, shiftID)` for Task 6.

- [ ] **Step 1: Write failing start-command tests**

Cover: initial count persists with sequence 1, Shift becomes `CLOSING`, source snapshot equals seeded Payment/Void/Refund/Movement facts, `OPEN` current read becomes revealable only after success, and each blocker returns its ordered 409 code with no reconciliation row. Add an injected Expected Cash range error test asserting the public error is `SHIFT_RECONCILIATION_CALCULATION_FAILED` and has no amount text.

- [ ] **Step 2: Run the start tests to verify failure**

Run: `TEST_DATABASE_URL="$TEST_DATABASE_URL" go test -count=1 -tags=integration ./internal/shift -run TestStartReconciliation`

Expected: FAIL because the handler and route do not exist.

- [ ] **Step 3: Implement the handler around one Shift executor transaction**

Use the existing mutation sequence. The mutation body must lock the named row, collect blockers, reject in precedence order, load Shift-scoped totals plus Cash Movement sums, compute Expected Cash, insert the snapshot and count sequence 1, update state, and return the fully loaded `ClosingShiftResponse`.

```go
type StartReconciliationCommand struct {
    RequestID       uuid.UUID `json:"request_id"`
    ShiftID         uuid.UUID `json:"-"`
    CountedCashVND  *int64    `json:"counted_cash_vnd"`
}

func (h *StartReconciliationHandler) Handle(ctx context.Context, actor Actor, cmd StartReconciliationCommand) (int, ClosingShiftResponse, error)
```

Map computation failure to a private wrapped sentinel and expose the generic public code in `MapHTTPError`; do not return the original `ComputeExpectedCash` message.

- [ ] **Step 4: Bind and document the start route**

Add `POST /shifts/:shift_id/reconciliation` with `sales_shift.operate`. Validate UUID, nonzero `request_id`, and presence/non-negative value of `counted_cash_vnd` before calling the handler. Add Swagger annotations with `201`, `400`, `403`, and ordered `409` outcomes.

- [ ] **Step 5: Run handler and HTTP tests**

Run: `TEST_DATABASE_URL="$TEST_DATABASE_URL" go test -count=1 -race -tags=integration ./internal/shift -run 'Test(StartReconciliation|ShiftHTTP.*Reconciliation)'`

Expected: PASS.

- [ ] **Step 6: Commit start workflow**

```bash
git add internal/shift/reconciliation.go internal/shift/reconciliation_integration_test.go internal/shift/http.go internal/shift/routes.go internal/shift/errors.go internal/shift/dto.go
git commit -m "feat(shift): start blind reconciliation"
```

### Task 5: Implement append-only Cash Count and QR Observation commands

**Files:**
- Create: `internal/shift/attempts.go`
- Create: `internal/shift/attempts_integration_test.go`
- Modify: `internal/shift/http.go`
- Modify: `internal/shift/routes.go`
- Modify: `internal/shift/dto.go`

**Interfaces:**
- Produces `RecordCashCountHandler.Handle(ctx, actor, RecordCashCountCommand)` and `RecordQRObservationHandler.Handle(ctx, actor, RecordQRObservationCommand)`.
- Produces `latestPreview(snapshot ReconciliationResponse) ReconciliationPreview` consumed by Task 6 and current read.

- [ ] **Step 1: Write failing attempt tests**

Assert that a second Cash Count increments only the Cash sequence, a QR Observation stores received/refunded together including `0`, different staff/session identities persist per attempt, an `OPEN` or `CLOSED` Shift rejects attempts, exact replay inserts no duplicate attempt/audit event, and a same request id with a different amount returns `REQUEST_CONFLICT`.

- [ ] **Step 2: Run the failing attempt tests**

Run: `TEST_DATABASE_URL="$TEST_DATABASE_URL" go test -count=1 -tags=integration ./internal/shift -run 'Test(RecordCashCount|RecordQRObservation)'`

Expected: FAIL because the attempt handlers do not exist.

- [ ] **Step 3: Implement the two handlers**

Each handler locks `CLOSING` Shift `FOR UPDATE`, loads its reconciliation, obtains `MAX(sequence) + 1` through a query under that lock, inserts exactly one append-only record, writes one audit event, and returns the new attempt plus a preview built from latest evidence.

```go
type RecordQRObservationCommand struct {
    RequestID           uuid.UUID `json:"request_id"`
    ShiftID             uuid.UUID `json:"-"`
    ObservedReceivedVND *int64    `json:"observed_received_vnd"`
    ObservedRefundedVND *int64    `json:"observed_refunded_vnd"`
}
```

Never accept one QR field without the other. Do not write a Manager Approval or discrepancy row in either attempt command.

- [ ] **Step 4: Bind the two routes**

Add:

```text
POST /shifts/:shift_id/reconciliation/cash-counts
POST /shifts/:shift_id/reconciliation/qr-observations
```

Use pointer-backed fields and reject omitted values before a domain transaction. Return `201` and the appended attempt plus full preview.

- [ ] **Step 5: Run focused tests**

Run: `TEST_DATABASE_URL="$TEST_DATABASE_URL" go test -count=1 -race -tags=integration ./internal/shift -run 'Test(RecordCashCount|RecordQRObservation|ShiftHTTP.*Observation)'`

Expected: PASS.

- [ ] **Step 6: Commit attempt ledgers**

```bash
git add internal/shift/attempts.go internal/shift/attempts_integration_test.go internal/shift/dto.go internal/shift/http.go internal/shift/routes.go
git commit -m "feat(shift): record reconciliation attempts"
```

### Task 6: Implement exact and Manager-approved discrepant Shift closure

**Files:**
- Create: `internal/shift/close.go`
- Create: `internal/shift/close_integration_test.go`
- Modify: `internal/shift/domain.go`
- Modify: `internal/shift/dto.go`
- Modify: `internal/shift/errors.go`
- Modify: `internal/shift/http.go`
- Modify: `internal/shift/routes.go`

**Interfaces:**
- Produces `CloseShiftHandler.Handle(ctx, actor, CloseShiftCommand) (int, ClosedShiftDetailResponse, error)`.
- Consumes `loadClosureBlockers`, frozen snapshot loaders, and preview/evidence DTOs from Tasks 4-5.

- [ ] **Step 1: Write failing closure tests**

Create named subtests for exact first-attempt close; Cash shortage after recount; QR-received difference after second QR Observation; QR-refunded difference; all three differences; missing reason; unexpected reason; missing/invalid Manager approval; self-approval; stale non-latest evidence; changed live source total; replay; and second independent close.

For a discrepant close, assert the final closure has three signed values, exactly the nonzero discrepancy rows, the correct reason pairing, one approver identity, and no balancing Payment/Refund/Cash Movement.

- [ ] **Step 2: Run the failing close tests**

Run: `TEST_DATABASE_URL="$TEST_DATABASE_URL" go test -count=1 -tags=integration ./internal/shift -run TestCloseShift`

Expected: FAIL because final close is unavailable.

- [ ] **Step 3: Implement final Close under the target Shift lock**

Define the request so all client-selected data is evidence/reason metadata, never amounts:

```go
type CloseShiftCommand struct {
    RequestID           uuid.UUID              `json:"request_id"`
    ShiftID             uuid.UUID              `json:"-"`
    FinalCashCountID    uuid.UUID              `json:"final_cash_count_id"`
    FinalQRObservationID uuid.UUID             `json:"final_qr_observation_id"`
    Discrepancies       []CloseDiscrepancyInput `json:"discrepancies"`
    ApproverLoginCode   string                 `json:"approver_login_code"`
    ManagerPIN          string                 `json:"manager_pin"`
}
```

Use two `MutationSpec` operation names: exact is selected only when the request has no discrepancy inputs; discrepant requires `auth.ManagerApproval` before replay. Under `FOR UPDATE`, re-run blockers, compare current totals with frozen totals, require latest evidence ids, compute differences, enforce recount/recheck and exact dimension/reason set, insert closure and rows, set `CLOSED`, and return immutable detail.

- [ ] **Step 4: Bind `POST /shifts/:shift_id/close` and map every typed outcome**

Validate both evidence UUIDs, non-null discrepancy array, reason/note shapes, and approval pair presence before dispatch. Map lifecycle/blocker/stale/recount errors to stable `409` codes; map bad body/reason to `400`; preserve collapse of Manager denial to `403`.

- [ ] **Step 5: Run closure integration and race tests**

Run: `TEST_DATABASE_URL="$TEST_DATABASE_URL" go test -count=1 -race -tags=integration ./internal/shift -run 'TestCloseShift|Test.*Concurrent.*Close'`

Expected: PASS, with one independent closer winning and the loser receiving `SALES_SHIFT_ALREADY_CLOSED`.

- [ ] **Step 6: Commit closure**

```bash
git add internal/shift/close.go internal/shift/close_integration_test.go internal/shift/domain.go internal/shift/dto.go internal/shift/errors.go internal/shift/http.go internal/shift/routes.go
git commit -m "feat(shift): close reconciled shifts"
```

## Checkpoint 7C: Inspection And Hardening

### Task 7: Replace current-Shift projection with redacted OPEN and frozen CLOSING reads

**Files:**
- Modify: `internal/shift/current.go`
- Modify: `internal/shift/dto.go`
- Modify: `internal/shift/current_integration_test.go`
- Modify: `internal/shift/shift_integration_test.go`

**Interfaces:**
- Changes `CurrentShiftHandler.Handle` to return `*CurrentShiftResponse`, a tagged DTO containing either `OpenCurrentShiftResponse` or `ClosingShiftResponse`.
- Consumes reconciliation/attempt loaders from Tasks 4-5.

- [ ] **Step 1: Write failing current-read tests**

Assert raw JSON for `OPEN` omits every money/history key, including `opening_float_vnd`, `expected_cash_vnd`, `cash_movements`, and `refunds`. Assert a `CLOSING` read returns frozen totals plus attempts in ascending sequence. Seed a post-start Payment/Refund mutation attempt and assert it rejects; seed raw live data after start only in a corruption test and assert current read keeps the frozen projection.

- [ ] **Step 2: Run current-read tests to verify failure**

Run: `TEST_DATABASE_URL="$TEST_DATABASE_URL" go test -count=1 -tags=integration ./internal/shift -run 'TestCurrentShift(RedactsOpen|ReturnsClosingSnapshot)'`

Expected: FAIL because `CurrentSalesShiftResponse` always exposes money.

- [ ] **Step 3: Implement state-dispatched projection**

Read one active Shift in `REPEATABLE READ`. For `OPEN`, project `OpenCurrentShiftResponse` only. For `CLOSING`, load only `shift_reconciliations`, attempts, and preview. For no active Shift, return `nil`. Never call `GetShiftReconciliationTotals` in the `CLOSING` projection.

- [ ] **Step 4: Update current route tests and run them**

Run: `TEST_DATABASE_URL="$TEST_DATABASE_URL" go test -count=1 -race -tags=integration ./internal/shift -run 'TestCurrentShift|TestShiftHTTP(HappyPath|SerializesEmptyListsAsArrays)'`

Expected: PASS with revised endpoint JSON assertions.

- [ ] **Step 5: Commit current read projection**

```bash
git add internal/shift/current.go internal/shift/dto.go internal/shift/current_integration_test.go internal/shift/shift_integration_test.go
git commit -m "feat(shift): project active reconciliation state"
```

### Task 8: Add Manager-only closed Shift list and detail reads

**Files:**
- Create: `internal/shift/history.go`
- Create: `internal/shift/history_integration_test.go`
- Modify: `internal/shift/dto.go`
- Modify: `internal/shift/http.go`
- Modify: `internal/shift/routes.go`
- Modify: `internal/shift/errors.go`

**Interfaces:**
- Produces `ListClosedShiftsHandler`, `GetClosedShiftHandler`, `DecodeClosedShiftCursor`, and `EncodeClosedShiftCursor`.
- Route middleware requires `authn.RequireCapability("audit.inspect")`.

- [ ] **Step 1: Write failing history tests**

Seed three closed snapshots with two identical `closed_at` values. Assert Manager gets descending `(closed_at, id)` pages with no duplicate/missing result; Cashier gets `403`; an `OPEN` or `CLOSING` id is `404`; missing range, range over 31 days, malformed cursor, and cursor with a different range are `400`.

- [ ] **Step 2: Run focused history tests to verify failure**

Run: `TEST_DATABASE_URL="$TEST_DATABASE_URL" go test -count=1 -tags=integration ./internal/shift -run 'Test(ListClosedShifts|GetClosedShift)'`

Expected: FAIL because history handlers and routes do not exist.

- [ ] **Step 3: Implement opaque range-bound cursor and handlers**

Use a versioned JSON payload encoded with `base64.RawURLEncoding`; reject decode failure, version mismatch, and normalized-range mismatch with `response.ErrInvalid`.

```go
type closedShiftCursor struct {
    Version    int       `json:"v"`
    ClosedAt   time.Time `json:"closed_at"`
    ID         uuid.UUID `json:"id"`
    ClosedFrom time.Time `json:"closed_from"`
    ClosedTo   time.Time `json:"closed_to"`
}
```

Handlers use `ExecuteRead(..., CapAuditInspect, ...)`, load summaries/details solely from closure tables and Staff summary joins, and initialize discrepancies/counts/observations as empty slices.

- [ ] **Step 4: Bind history HTTP routes**

Register the list route before `GET /shifts/:shift_id` so `/current` remains unambiguous. Parse RFC 3339 range parameters with inclusive lower and exclusive upper bounds; default limit 50, maximum 100. Return `ClosedShiftSummaryResponse` list with the next cursor and `ClosedShiftDetailResponse` for id.

- [ ] **Step 5: Run history HTTP and integration tests**

Run: `TEST_DATABASE_URL="$TEST_DATABASE_URL" go test -count=1 -race -tags=integration ./internal/shift -run 'Test(ListClosedShifts|GetClosedShift|ShiftHTTP.*History)'`

Expected: PASS.

- [ ] **Step 6: Commit closed history**

```bash
git add internal/shift/history.go internal/shift/history_integration_test.go internal/shift/dto.go internal/shift/http.go internal/shift/routes.go internal/shift/errors.go
git commit -m "feat(shift): inspect closed shift history"
```

### Task 9: Complete failure-injection, cross-slice concurrency, and authorization coverage

**Files:**
- Create: `internal/shift/reconciliation_concurrency_integration_test.go`
- Create: `internal/shift/reconciliation_failure_integration_test.go`
- Modify: `internal/sales/session_start_integration_test.go`
- Modify: `internal/shift/executor_integration_test.go`
- Modify: `internal/shift/shift_integration_test.go`

**Interfaces:**
- Consumes all Phase 07 public handlers and SQL fixtures.
- Produces no production interfaces; it proves the transaction/locking specification.

- [ ] **Step 1: Write deterministic lock-orchestration helpers**

Copy the established Sales concurrency approach: take a known database row lock in one transaction, start the handler in a goroutine, poll `pg_stat_activity`/lock wait evidence with a test deadline, release the lock, collect exactly one result. Do not use `time.Sleep` to create a race.

```go
func waitForBlockedQuery(t *testing.T, db *sql.DB, applicationName string) {
    t.Helper()
    require.Eventually(t, func() bool {
        var waiting bool
        err := db.QueryRow(`SELECT EXISTS (...)`).Scan(&waiting)
        return err == nil && waiting
    }, 5*time.Second, 10*time.Millisecond)
}
```

- [ ] **Step 2: Add failing race and rollback cases**

Cover: Session Start versus Start Reconciliation; Payment/Refund/Void/Comp/Cash Movement versus Start; Commit and Service Session closure versus Start; two starts; append attempt versus close; two closes; and Open Shift versus final close. For injected failures after each durable write, assert no partial reconciliation, attempt, closure, discrepancy, audit success event, or idempotency result exists.

- [ ] **Step 3: Run tests to verify the missing cases fail or expose gaps**

Run: `TEST_DATABASE_URL="$TEST_DATABASE_URL" go test -count=1 -race -tags=integration ./internal/shift ./internal/sales -run 'Test.*(Reconciliation|Shift).*Concurrent|TestReconciliationRollback'`

Expected: any uncovered behavior fails before production fixes; retain each test as the regression specification.

- [ ] **Step 4: Make only the minimal fixes exposed by these tests**

Keep all writer coordination on the Shift row. If a test shows a command can mutate after `CLOSING`, tighten that command's existing `sh.state = 'OPEN'` query predicate or its Shift lock path; do not add a second global lock or a cross-package import. Keep Commit protected by its active-Session/Session-closure serialization rather than adding an unnecessary Shift lock.

- [ ] **Step 5: Run the full Phase 07 integration suites**

Run: `TEST_DATABASE_URL="$TEST_DATABASE_URL" go test -count=1 -race -tags=integration ./internal/shift ./internal/sales`

Expected: PASS.

- [ ] **Step 6: Commit hardening tests and fixes**

```bash
git add internal/shift/*reconciliation*_integration_test.go internal/sales/*session_start*_integration_test.go internal/shift internal/sales
git commit -m "test(shift): harden reconciliation concurrency"
```

### Task 10: Regenerate Swagger, update phase documentation, and verify the release

**Files:**
- Modify: `internal/shift/http.go`
- Modify: `docs/docs.go` via `make swagger`
- Modify: `docs/swagger.json` via `make swagger`
- Modify: `docs/swagger.yaml` via `make swagger`
- Modify: `ROADMAP.md`
- Modify: `docs/backlog/README.md`
- Modify: `docs/backlog/phase-07-close-and-reconcile-sales-shift.md`

**Interfaces:**
- Documents the implemented HTTP contract; does not create new runtime interfaces.

- [ ] **Step 1: Write Swagger and documentation assertions**

Add/extend HTTP tests that exercise every new route and assert the operation's request validation/error code. Add a lightweight test or checked-in review assertion that generated Swagger contains `/shifts/{shift_id}/reconciliation`, both attempt routes, `/shifts/{shift_id}/close`, and history routes.

- [ ] **Step 2: Update HTTP annotations and stale comments**

Document every request/response DTO, all `400`/`403`/`404`/`409` outcomes, `audit.inspect` history access, and the secret-free Manager Approval shape. Remove comments saying Refund or Shift Close is unimplemented or deferred to Phase 5.

- [ ] **Step 3: Generate API documentation and format source**

Run: `make fmt && make swagger`

Expected: generated docs contain every route and `git diff --check` has no output.

- [ ] **Step 4: Run the complete verification suite**

Run: `make test-all && make vet && make build`

Expected: all unit tests, PostgreSQL integration tests with race detection, vet, and API build exit 0.

- [ ] **Step 5: Mark the phase delivered only after green verification**

Update `ROADMAP.md` Phase 07 from Remaining to Delivered, set the backlog table and ticket status to `completed`, and replace every checklist item in the Phase 07 ticket with `[x]`. Do not change Phase 08 status.

- [ ] **Step 6: Re-run final documentation and repository checks**

Run: `git diff --check && git status --short && make test-all && make build`

Expected: no whitespace errors, only intended Phase 07 files changed, and all commands exit 0.

- [ ] **Step 7: Commit release documentation**

```bash
git add internal/shift/http.go docs/docs.go docs/swagger.json docs/swagger.yaml ROADMAP.md docs/backlog/README.md docs/backlog/phase-07-close-and-reconcile-sales-shift.md
git commit -m "docs(shift): complete Phase 07 closure"
```

---

## Plan Self-Review

### Specification coverage

- Blind initial count, `CLOSING`, and aggregate redaction: Tasks 1, 4, and 7.
- Normalized snapshot/attempt/closure/discrepancy persistence: Task 2.
- Global blockers and exact precedence/equation: Tasks 2 and 4.
- Session Start race: Task 3; financial and Session/Commit races: Task 9.
- Recount/recheck, three dimensions, reasons, and Manager Approval: Tasks 5 and 6.
- Immutable closure, replay, and open-after-close: Task 6.
- Manager-only date-range cursor history: Task 8.
- Error secrecy, audit, Swagger, and all acceptance verification: Tasks 4, 6, 9, and 10.

### Placeholder scan

The tasks name concrete files, handlers, query contracts, commands, test commands, error codes, and commit commands. No deferred implementation markers are present.

### Type consistency

Tasks 4-8 use the DTO and domain vocabulary introduced in Task 1. Tasks 4-6 consume the generated query layer from Task 2. Task 8 consumes `ClosedShiftDetailResponse` introduced in Task 1 and completed by Tasks 4-6.
