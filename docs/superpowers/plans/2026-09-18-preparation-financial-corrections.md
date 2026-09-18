# Preparation Financial Corrections Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement Phase 6.C Cancellation/Change, Comp, Refund, Manual QR Refund confirmation, Payment Void, correction-aware projections and closure, and current-Shift reconciliation.

**Architecture:** Keep command ownership in the existing vertical slices: Preparation atomically owns Cancel/Change plus its narrow live Check adjustment, Sales owns all other financial corrections and their projections, and Shift owns cross-slice reconciliation reads through Shift SQL. Every mutation remains one PostgreSQL transaction through the owning executor, uses append-only source facts plus explicit current-state updates, follows the Check-before-Session lock protocol, and uses no event-driven consistency path.

**Tech Stack:** Go 1.27.0/toolchain 1.27.1, Echo v4, PostgreSQL, `database/sql`, sqlc v1.31.1, `google/uuid`, `testify`, swaggo.

**Spec:** `docs/superpowers/specs/2026-09-18-preparation-financial-corrections-design.md`

## Global Constraints

- Use Go `1.27.0` with toolchain `go1.27.1`; add no dependency and do not change `go.mod` or `go.sum`.
- Use parameterized SQL in `sql/queries/*.sql`; never manually edit `internal/database/sqlc/*`.
- Use `make sqlc` after query changes and `make swagger` after annotation changes.
- Store all VND values in guarded `int64` arithmetic; request-caused range failures map to `422`, persisted invariant failures to `500`.
- Preserve package ownership: Preparation may write Charge Adjustment and Check rows only inside Cancel/Change; Shift imports neither Sales nor Preparation.
- Use one owning-package transaction per command; do not use Watermill, triggers, views, stored procedures, or a new orchestration package.
- Acquire rows in this order: Checks by UUID, Service Sessions by UUID, current/original Sales Shift, Payments then Charge Adjustments by UUID, Preparation facts then units by UUID.
- Accept PostgreSQL `40P01` only for Cancel/active Comp versus Submit under ADR-031; do not add an in-transaction retry.
- Exclude approver login codes and PINs from fingerprints, stored results, business facts, audit details, and logs.
- Return all collection fields as non-null arrays and expose no correction money through the Preparation Queue.
- Do not add Manual QR excess, partial Payment Void, post-close Payment Void, post-Shift correction, Shift Close, frontend work, arbitrary discounts, or mutable Completed Sale snapshots.
- Integration tests use the existing `//go:build integration` package clone harness and must be independently runnable.

## File Structure

- Create `internal/database/migrations/000014_add_preparation_financial_corrections.sql`: all eight append-only Phase 6.C tables, indexes, constraints, and the `QUEUED -> CANCELLED` transition pair.
- Modify `sql/queries/preparation.sql`: Cancellation ownership resolution, locks, writes, financial verification, settlement, and audit queries.
- Modify `sql/queries/sales.sql`: correction locks/writes, refundable-capacity reads, projection reads, closure reads, and Completed Sale history.
- Modify `sql/queries/shift.sql`: current-Shift financial aggregates and Refund summaries owned by Shift.
- Regenerate `internal/database/sqlc/{models.go,preparation.sql.go,sales.sql.go,shift.sql.go,querier.go}` from those SQL sources.
- Create `internal/sales/financials.go`: guarded Check financial equations and correction projection loaders shared only inside Sales.
- Create `internal/preparation/cancel.go`: atomic Cancellation/Change command.
- Create `internal/sales/comp.go`: live and post-sale Comp command.
- Create `internal/sales/refund.go`: Refund recording and Manual QR completion commands.
- Create `internal/sales/payment_void.go`: whole-Payment Void command and Check reopening.
- Modify existing package `domain.go`, `dto.go`, `errors.go`, `routes.go`, `http.go`, projection, closure, Completed Sale, restructuring, and Shift current-read files rather than creating parallel frameworks.
- Add source-matched unit tests and focused integration/concurrency test files named after each new source file.
- Regenerate `docs/{docs.go,swagger.json,swagger.yaml}` only in the final contract task.

---

### Task 1: Persist Correction Facts And Generate Query APIs

**Files:**
- Create: `internal/database/migrations/000014_add_preparation_financial_corrections.sql`
- Modify: `sql/queries/preparation.sql`
- Modify: `sql/queries/sales.sql`
- Modify: `sql/queries/shift.sql`
- Modify: `internal/preparation/schema_integration_test.go`
- Modify: `internal/sales/schema_integration_test.go`
- Modify: `internal/preparation/env_integration_test.go:87-105`
- Modify: `internal/sales/testmain_integration_test.go:71-89`
- Modify: `internal/shift/executor_integration_test.go:30-39`
- Regenerate: `internal/database/sqlc/models.go`
- Regenerate: `internal/database/sqlc/preparation.sql.go`
- Regenerate: `internal/database/sqlc/sales.sql.go`
- Regenerate: `internal/database/sqlc/shift.sql.go`
- Regenerate: `internal/database/sqlc/querier.go`

**Interfaces:**
- Produces tables: `charge_adjustments`, `preparation_cancellations`, `sales_comps`, `payment_voids`, `refunds`, `refund_payment_allocations`, `refund_adjustment_allocations`, `refund_completions`.
- Produces sqlc query groups named `ResolveCancellationUnits`, `LockPreparationChecksForCancellation`, `LockPreparationSessionsForCancellation`, `LockOpenSalesShiftForCancellation`, `LockPreparationUnitsForCancellation`, `GetCancellationReplacementOrder`, `GetPreparationCheckFinancials`, `InsertChargeAdjustment`, `UpdateAdjustedCheckCharge`, `InsertPreparationCancellation`, `SettleAdjustedCheck`, `ResolveCompSource`, `LockWasteForComp`, `LockPaymentForVoid`, `LockPaymentsForRefund`, `LockChargeAdjustmentsForRefund`, `InsertSalesComp`, `InsertPaymentVoid`, `InsertRefund`, `InsertRefundPaymentAllocations`, `InsertRefundAdjustmentAllocations`, `InsertRefundCompletion`, `ListCheckChargeAdjustments`, `ListCheckRefunds`, `ListRefundPaymentAllocations`, `ListRefundAdjustmentAllocations`, `ListCompletedSalePostSaleCorrections`, `GetShiftReconciliationTotals`, and `ListShiftRefunds`.
- Consumes only existing primary keys and ownership links from migrations `000007` through `000013`.

- [ ] **Step 1: Write failing schema contract tests**

Add `TestPreparationFinancialCorrectionsSchema` and `TestSalesFinancialCorrectionsSchema` using `information_schema`, `pg_constraint`, and `pg_indexes`. Pin every table, FK delete action, named check/unique constraint, required index, and the added transition pair. Add direct insert cases that reject invalid kind/source, scope/Completed Sale, reason/note, non-positive amount, duplicate Cancellation/Comp/Void/completion, and allocation pairs.

```go
func TestPreparationFinancialCorrectionsSchema(t *testing.T) {
	db, _ := openPrepTestDB(t)
	var tables int
	require.NoError(t, db.QueryRow(`
		SELECT count(*) FROM information_schema.tables
		WHERE table_schema = 'public'
		  AND table_name IN ('charge_adjustments', 'preparation_cancellations')`).Scan(&tables))
	require.Equal(t, 2, tables)
	require.Contains(t, constraintDef(t, db, "preparation_unit_transition_states_valid"),
		"'QUEUED'::text, 'CANCELLED'::text")
}

func TestSalesFinancialCorrectionsSchema(t *testing.T) {
	db, _ := openSalesTestDB(t)
	var tables int
	require.NoError(t, db.QueryRow(`
		SELECT count(*) FROM information_schema.tables
		WHERE table_schema = 'public'
		  AND table_name IN ('sales_comps', 'payment_voids', 'refunds',
		                     'refund_payment_allocations',
		                     'refund_adjustment_allocations', 'refund_completions')`).Scan(&tables))
	require.Equal(t, 6, tables)
}
```

- [ ] **Step 2: Run the schema tests to verify they fail**

Run: `go test -count=1 -tags=integration ./internal/preparation ./internal/sales -run FinancialCorrectionsSchema`

Expected: FAIL because migration `000014` and the Phase 6.C tables do not exist.

- [ ] **Step 3: Add the rerunnable migration**

Define the approved columns and named constraints. Keep source records immutable and alter only the transition-pair constraint on an existing table.

```sql
CREATE TABLE IF NOT EXISTS charge_adjustments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    kind TEXT NOT NULL,
    scope TEXT NOT NULL,
    preparation_unit_id UUID NOT NULL REFERENCES preparation_units(id) ON DELETE RESTRICT,
    preparation_waste_id UUID REFERENCES preparation_wastes(id) ON DELETE RESTRICT,
    charge_allocation_id UUID NOT NULL REFERENCES charge_allocations(id) ON DELETE RESTRICT,
    check_id UUID NOT NULL REFERENCES checks(id) ON DELETE RESTRICT,
    completed_sale_id UUID REFERENCES completed_sales(id) ON DELETE RESTRICT,
    sales_shift_id UUID NOT NULL REFERENCES sales_shifts(id) ON DELETE RESTRICT,
    amount_vnd BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT charge_adjustment_kind_source_valid CHECK (
        (kind = 'CANCELLATION' AND preparation_waste_id IS NULL) OR
        (kind = 'COMP' AND preparation_waste_id IS NOT NULL)
    ),
    CONSTRAINT charge_adjustment_scope_valid CHECK (
        (scope = 'LIVE_CHECK' AND completed_sale_id IS NULL) OR
        (scope = 'POST_SALE' AND completed_sale_id IS NOT NULL)
    ),
    CONSTRAINT charge_adjustment_amount_positive CHECK (amount_vnd > 0),
    CONSTRAINT charge_adjustment_kind_unit_unique UNIQUE (kind, preparation_unit_id)
);

CREATE TABLE IF NOT EXISTS preparation_cancellations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    preparation_unit_id UUID NOT NULL REFERENCES preparation_units(id) ON DELETE RESTRICT,
    kind TEXT NOT NULL,
    charge_adjustment_id UUID REFERENCES charge_adjustments(id) ON DELETE RESTRICT,
    replacement_order_id UUID REFERENCES orders(id) ON DELETE RESTRICT,
    reason TEXT NOT NULL,
    note TEXT,
    actor_staff_identity_id UUID NOT NULL REFERENCES staff_identities(id) ON DELETE RESTRICT,
    staff_access_session_id UUID NOT NULL REFERENCES staff_access_sessions(id) ON DELETE RESTRICT,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT preparation_cancellation_unit_unique UNIQUE (preparation_unit_id),
    CONSTRAINT preparation_cancellation_adjustment_unique UNIQUE (charge_adjustment_id),
    CONSTRAINT preparation_cancellation_kind_replacement_valid CHECK (
        (kind = 'CANCELLATION' AND replacement_order_id IS NULL) OR
        (kind = 'CHANGE' AND replacement_order_id IS NOT NULL)
    ),
    CONSTRAINT preparation_cancellation_reason_valid CHECK (
        reason IN ('CUSTOMER_REQUEST', 'ORDER_ENTRY_ERROR', 'ITEM_UNAVAILABLE', 'OTHER')
    ),
    CONSTRAINT preparation_cancellation_note_valid CHECK (
        note IS NULL OR char_length(note) BETWEEN 1 AND 500
    ),
    CONSTRAINT preparation_cancellation_other_note_valid CHECK (
        reason <> 'OTHER' OR (note IS NOT NULL AND btrim(note) <> '')
    )
);
```

Add the remaining six tables with the exact columns from spec sections 5.3 through 5.7. Give every FK an explicit stable name and `ON DELETE RESTRICT`. Name unique constraints `sales_comp_waste_unique`, `sales_comp_adjustment_unique`, `payment_void_payment_unique`, `refund_payment_allocation_pair_unique`, `refund_adjustment_allocation_pair_unique`, and `refund_completion_refund_unique`; name positive checks with the table name plus `_amount_positive`. Add indexes for Check, Completed Sale, Shift, source unit/Waste, Payment allocation, Adjustment allocation, and `(created_at, id)` ordering. Rebuild `preparation_unit_transition_states_valid` with all existing pairs plus `('QUEUED', 'CANCELLED')`.

- [ ] **Step 4: Add parameterized query APIs in lock order**

Use non-locking ownership resolution first and deterministic SQL ordering on every multi-row lock.

```sql
-- name: LockPaymentsForRefund :many
SELECT p.id, p.check_id, p.sales_shift_id, p.method, p.applied_amount_vnd
FROM payments AS p
WHERE p.id = ANY(sqlc.arg(payment_ids)::uuid[])
ORDER BY p.id
FOR UPDATE;

-- name: LockChargeAdjustmentsForRefund :many
SELECT ca.id, ca.scope, ca.check_id, ca.completed_sale_id, ca.amount_vnd
FROM charge_adjustments AS ca
WHERE ca.id = ANY(sqlc.arg(charge_adjustment_ids)::uuid[])
ORDER BY ca.id
FOR UPDATE;

-- name: InsertRefundCompletion :one
INSERT INTO refund_completions (
    refund_id, transaction_reference, completed_by_staff_identity_id,
    staff_access_session_id, completed_at
) VALUES ($1, $2, $3, $4, $5)
RETURNING id, refund_id, transaction_reference,
          completed_by_staff_identity_id, staff_access_session_id, completed_at;
```

`ResolveCancellationUnits` must map each standard unit to the immutable per-unit price and the Charge Allocation range ordered by `(charge_allocations.created_at, charge_allocations.id)`; Remakes return null allocation/Check/price. Capacity queries must include pending Refund allocations, while effective receipt sums include only completed Refunds.

- [ ] **Step 5: Regenerate sqlc and fix query typing at the source**

Run: `make sqlc`

Expected: PASS; generated methods appear in `sqlc.Querier`, nullable UUIDs use `uuid.NullUUID`, and no generated file is hand-edited.

- [ ] **Step 6: Add all Phase 6.C tables to package truncation helpers**

Use dependency order so explicit cleanup remains readable even though `CASCADE` is present.

```sql
TRUNCATE refund_completions, refund_adjustment_allocations,
         refund_payment_allocations, refunds, payment_voids, sales_comps,
         preparation_cancellations, charge_adjustments
RESTART IDENTITY CASCADE;
```

- [ ] **Step 7: Run schema and package compile verification**

Run: `go test ./internal/preparation ./internal/sales ./internal/shift`

Run: `go test -count=1 -tags=integration ./internal/preparation ./internal/sales -run FinancialCorrectionsSchema`

Expected: PASS.

- [ ] **Step 8: Commit the database contract**

```bash
git add internal/database/migrations/000014_add_preparation_financial_corrections.sql sql/queries internal/database/sqlc internal/preparation/schema_integration_test.go internal/sales/schema_integration_test.go internal/preparation/env_integration_test.go internal/sales/testmain_integration_test.go internal/shift/executor_integration_test.go
git commit -m "feat(database): add financial correction facts"
```

### Task 2: Add Optional Manager Approval To Sales Mutations

**Files:**
- Modify: `internal/sales/executor.go:25-39,215-329`
- Modify: `internal/sales/dto.go`
- Modify: `internal/sales/dto_test.go`
- Modify: `internal/sales/executor_integration_test.go`

**Interfaces:**
- Produces `ManagerApprovalInput`, `ApprovalSpec`, `MutationSpec.Approval`, and `MutationContext.Approver` with the same semantics as `internal/shift/executor.go`.
- Consumes `auth.VerifyManagerApproval(ctx, q, loginCode, pin, capability)`.
- Existing Sales callers continue passing no approval and require no edits beyond compilation.

- [ ] **Step 1: Write failing executor approval tests**

Add table-driven cases for valid Manager, self-approval, wrong PIN, disabled/demoted approver, removed capability, replay after approval invalidation, and no secret in `idempotency_keys` or `audit_events`.

```go
func TestSalesExecutorApprovalRunsBeforeReplay(t *testing.T) {
	// First execution succeeds with a valid Manager approval.
	// Disable the approver, replay the same request, and require ErrForbidden.
	// Assert the mutation callback still ran exactly once.
}
```

- [ ] **Step 2: Run the focused test to verify it fails**

Run: `go test -count=1 -tags=integration ./internal/sales -run SalesExecutorApproval`

Expected: FAIL because Sales `MutationSpec` has no approval contract.

- [ ] **Step 3: Implement the executor contract**

```go
type ManagerApprovalInput struct {
	ApproverLoginCode string `json:"approver_login_code"`
	ManagerPIN string `json:"manager_pin"`
}

type ApprovalSpec struct {
	ApproverLoginCode string
	ManagerPIN string
	RequiredCapability string
}

type MutationSpec struct {
	RequestID uuid.UUID
	Operation string
	Fingerprint any
	Required []string
	Approval *ApprovalSpec
}

type MutationContext struct {
	Queries *sqlc.Queries
	Approver *auth.ApproverSummary
}
```

After initiator capability verification and before fingerprinting, normalize the login code, call `auth.VerifyManagerApproval`, commit security-denial evidence on expected denial, and pass a pointer to the verified summary into the callback. Never add either credential to `Fingerprint`.

- [ ] **Step 4: Run existing and new executor tests**

Run: `go test ./internal/sales -run 'TestExecuteMutation|TestSalesExecutorApproval'`

Run: `go test -count=1 -tags=integration ./internal/sales -run 'TestExecutor|TestSalesExecutorApproval'`

Expected: PASS, including existing no-approval mutations.

- [ ] **Step 5: Commit the executor extension**

```bash
git add internal/sales/executor.go internal/sales/dto.go internal/sales/dto_test.go internal/sales/executor_integration_test.go
git commit -m "feat(sales): support manager-approved mutations"
```

### Task 3: Build Correction-Aware Check Financials And Projections

**Files:**
- Create: `internal/sales/financials.go`
- Create: `internal/sales/financials_test.go`
- Modify: `internal/sales/domain.go:236-317`
- Modify: `internal/sales/dto.go:281-315`
- Modify: `internal/sales/projection.go:15-24,184-289`
- Modify: `internal/sales/projection_integration_test.go`
- Modify: `internal/sales/dto_test.go`
- Modify: `internal/sales/payments.go:134-181`

**Interfaces:**
- Produces `ComputeCheckFinancials(CheckFinancialInputs) (CheckFinancials, error)`.
- Produces DTOs `ChargeAdjustmentResponse`, `PaymentVoidResponse`, `RefundAllocationResponse`, `RefundCompletionResponse`, and `RefundResponse`.
- Extends `CheckResponse` and `PaymentResponse` exactly as required by spec section 12.1.
- Produces `SnapshotMode`, `loadChecksForSnapshot(ctx, q, sessionID, mode) ([]CheckResponse, error)`, and a compatibility wrapper `loadChecks(ctx, q, sessionID) ([]CheckResponse, error)`. `SnapshotLive` includes current live facts and `SnapshotCompletedSaleCore` excludes post-sale facts structurally.

- [ ] **Step 1: Write failing arithmetic and JSON contract tests**

Cover base charge, adjusted charge, valid payments, completed Refunds, pending Manual QR behavior, voided Payments, negative effective receipt, overflow/underflow, invariant mismatch, and non-null empty collections.

```go
type CheckFinancialInputs struct {
	BaseChargeVND int64
	LiveAdjustmentVND int64
	OriginalPaymentVND int64
	VoidedPaymentVND int64
	CompletedRefundVND int64
}

type CheckFinancials struct {
	ChargeVND int64
	ValidPaymentVND int64
	EffectiveReceivedVND int64
	BalanceVND int64
	PendingRefundVND int64
}

type SnapshotMode uint8

const (
	SnapshotLive SnapshotMode = iota
	SnapshotCompletedSaleCore
)
```

- [ ] **Step 2: Run unit tests to verify they fail**

Run: `go test ./internal/sales -run 'TestComputeCheckFinancials|TestCorrectionDTO'`

Expected: FAIL because the financial equation and DTO fields do not exist.

- [ ] **Step 3: Implement guarded equations**

Compute in this order and reject every impossible persisted result with `ErrFinancialInvariantViolated`:

```text
charge = base charge - live adjustments
valid payment = original payments - voided payments
effective received = valid payment - completed live refunds
balance = max(charge - effective received, 0)
pending refund = max(effective received - charge, 0)
```

Keep `total_applied_vnd` as the original Payment sum. Do not subtract pending Manual QR Refunds from effective receipt.

- [ ] **Step 4: Extend DTOs and projection loaders**

```go
type CheckResponse struct {
	ID uuid.UUID `json:"id"`
	State string `json:"state"`
	BaseChargeVND int64 `json:"base_charge_vnd"`
	ChargeVND int64 `json:"charge_vnd"`
	TotalAppliedVND int64 `json:"total_applied_vnd"`
	TotalVoidedVND int64 `json:"total_voided_vnd"`
	TotalRefundedVND int64 `json:"total_refunded_vnd"`
	EffectiveReceivedVND int64 `json:"effective_received_vnd"`
	BalanceVND int64 `json:"balance_vnd"`
	PendingRefundVND int64 `json:"pending_refund_vnd"`
	MergedIntoCheckID *uuid.UUID `json:"merged_into_check_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	Payments []PaymentResponse `json:"payments"`
	Allocations []ChargeAllocationResponse `json:"allocations"`
	ChargeAdjustments []ChargeAdjustmentResponse `json:"charge_adjustments"`
	Refunds []RefundResponse `json:"refunds"`
}
```

Initialize all four collections before loading. Add nullable Void evidence and `remaining_refundable_vnd` to each Payment. Load Refund allocations and completion evidence in stable `(created_at, id)` order.

- [ ] **Step 5: Replace old allocation-only charge assertions**

Update `assertChargeMatchesAllocations` and all callers so the invariant is base allocations minus `LIVE_CHECK` adjustments. Ensure merged Checks remain exempt only where the existing code already permits them.

- [ ] **Step 6: Run focused projection tests**

Run: `go test ./internal/sales -run 'TestComputeCheckFinancials|TestCorrectionDTO|TestEvaluateClosureReadiness'`

Run: `go test -count=1 -tags=integration ./internal/sales -run 'TestProjection'`

Expected: PASS with `[]`, never `null`, for adjustments and Refunds.

- [ ] **Step 7: Commit financial foundations**

```bash
git add internal/sales/financials.go internal/sales/financials_test.go internal/sales/domain.go internal/sales/dto.go internal/sales/projection.go internal/sales/projection_integration_test.go internal/sales/dto_test.go internal/sales/payments.go
git commit -m "feat(sales): project corrected check financials"
```

### Task 4: Implement Atomic Cancellation And Change

**Files:**
- Create: `internal/preparation/cancel.go`
- Create: `internal/preparation/cancel_test.go`
- Create: `internal/preparation/cancel_integration_test.go`
- Modify: `internal/preparation/domain.go:98-289`
- Modify: `internal/preparation/dto.go:115-235`
- Modify: `internal/preparation/errors.go:54-112`
- Modify: `internal/preparation/routes.go:11-66`
- Modify: `internal/preparation/http.go`
- Modify: `internal/preparation/preparation_integration_test.go`
- Modify: `internal/preparation/swagger_test.go`

**Interfaces:**
- Produces `CancelUnitsCommand`, `CancellationOutcome`, `CancelUnitsResponse`, `CancelUnitsHandler`, and `NewCancelUnitsHandler`.
- Uses `QueueAlertResponse` for the route's alert array.
- Defines `CapSalesOperate = "sales.operate"` in `internal/preparation/domain.go`; Preparation has no such constant today and must add it so the Cancel route's capability middleware can reference it without importing `internal/sales`.
- Uses `OpCancelUnits = "preparation.cancel_units"`, capability `CapSalesOperate`, and no Manager Approval.
- Does not import `internal/sales`.

- [ ] **Step 1: Write failing validation tests**

Cover 0, 1, 50, and 51 ids; zero and duplicate ids; kind/replacement pairing; reason catalogs; trimmed notes; `OTHER`; request-order output; and a UUID-sorted fingerprint independent of input order.

```go
type CancelUnitsCommand struct {
	RequestID uuid.UUID `json:"request_id"`
	PreparationUnitIDs []uuid.UUID `json:"preparation_unit_ids"`
	Kind string `json:"kind"`
	ReplacementOrderID *uuid.UUID `json:"replacement_order_id"`
	Reason string `json:"reason"`
	Note *string `json:"note"`
}

type CancellationOutcome struct {
	CancellationID uuid.UUID `json:"cancellation_id"`
	PreparationUnitID uuid.UUID `json:"preparation_unit_id"`
	PriorState string `json:"prior_state"`
	ResultingState string `json:"resulting_state"`
	ChargeAdjustmentID *uuid.UUID `json:"charge_adjustment_id"`
	ChargeRemovedVND int64 `json:"charge_removed_vnd"`
	OccurredAt time.Time `json:"occurred_at"`
}

type CancelUnitsResponse struct {
	Outcomes []CancellationOutcome `json:"outcomes"`
	Alerts []QueueAlertResponse `json:"alerts"`
}
```

- [ ] **Step 2: Run unit tests to verify they fail**

Run: `go test ./internal/preparation -run 'TestValidateCancelUnits|TestCancelFingerprint'`

Expected: FAIL because the command does not exist.

- [ ] **Step 3: Implement validation and credential-free fingerprinting**

Normalize note and a sorted copy of ids before constructing:

```go
type cancelUnitsFingerprint struct {
	PreparationUnitIDs []uuid.UUID `json:"preparation_unit_ids"`
	Kind string `json:"kind"`
	ReplacementOrderID *uuid.UUID `json:"replacement_order_id"`
	Reason string `json:"reason"`
	Note *string `json:"note"`
}
```

Use stable sentinels for every error listed in spec section 14 and map malformed input to `400`, unknown resources to `404`, stale/closed/conflicting state to `409`, request-caused range errors to `422`, and invariants to `500`.

- [ ] **Step 4: Write failing atomic workflow integration tests**

Cover unpaid charged unit, fully paid unit, partial payment reduced to zero, valid/invalid Change replacement, Remake Cancellation, 50-unit success, stale/missing/cross-Session rollback, replay, authorization, audit rows, alert acknowledgment, and injected failures at adjustment/fact/alert/audit/result storage.

```go
func TestCancelUnitsFullyPaidCreatesPendingRefund(t *testing.T) {
	// Submit and pay one unit, cancel it, assert SETTLED plus pending_refund_vnd.
	// Assert one adjustment, cancellation, transition, alert, and each audit type.
}
```

- [ ] **Step 5: Implement the transaction in common lock order**

Pre-resolve ownership, sort Check ids, lock Checks, lock the one Session, lock the open Shift, lock source units, then revalidate all rows. For Change, require a different later submitted Order in the same active Session. Use one database timestamp and no savepoints.

```go
func (h *CancelUnitsHandler) Handle(
	ctx context.Context,
	actor Actor,
	cmd CancelUnitsCommand,
) (int, CancelUnitsResponse, error)
```

For each charged standard unit, insert a live `CANCELLATION` adjustment for the immutable unit price. Aggregate per Check, verify `stored charge = base allocations - existing live adjustments`, update each Check once, settle when corrected balance is zero, then append unit state, transition, Cancellation, alert, and audit facts. Preserve response order with an input-index map.

- [ ] **Step 6: Wire route, handler, and Swagger annotations**

Register `POST /preparation/units/cancel` before parameterized unit routes with `sales.operate` middleware. Return `200` through `sendResult` and document `200/400/401/403/404/409/422/500`.

```go
v1.POST("/preparation/units/cancel", s.handleCancelUnits,
	authn.RequireAuth(), authn.RequireCapability(CapSalesOperate))
```

- [ ] **Step 7: Run unit, integration, HTTP, and queue privacy tests**

Run: `go test ./internal/preparation`

Run: `go test -count=1 -tags=integration ./internal/preparation -run 'TestCancelUnits|TestPreparationHTTPCancel|TestActiveQueue'`

Expected: PASS; queue output contains Cancellation/Change alerts but none of `charge_vnd`, `payment`, `refund`, or `adjustment`.

- [ ] **Step 8: Commit Cancellation and Change**

```bash
git add internal/preparation/cancel.go internal/preparation/cancel_test.go internal/preparation/cancel_integration_test.go internal/preparation/domain.go internal/preparation/dto.go internal/preparation/errors.go internal/preparation/routes.go internal/preparation/http.go internal/preparation/preparation_integration_test.go internal/preparation/swagger_test.go
git commit -m "feat(preparation): add cancellation and change"
```

### Task 5: Block Split And Merge After Live Adjustments

**Files:**
- Modify: `internal/sales/check_restructuring.go:94-106,167-368,417-459`
- Modify: `internal/sales/check_restructuring_test.go`
- Modify: `internal/sales/check_restructuring_integration_test.go`
- Modify: `internal/sales/errors.go`

**Interfaces:**
- Produces `assertNoLiveChargeAdjustments(ctx, q, checkIDs) error`.
- Produces `ErrCheckHasChargeAdjustment`, mapped to `CHECK_HAS_CHARGE_ADJUSTMENT` and `409`.
- Consumes `CountLiveChargeAdjustmentsForChecks` generated in Task 1.

- [ ] **Step 1: Write failing restructuring tests**

Add Split and Merge cases with an otherwise eligible open/unpaid Check carrying one live adjustment. Assert no allocation or Check is rewritten.

```go
func TestSplitAndMergeRejectLiveChargeAdjustment(t *testing.T) {
	// Seed one live adjustment, then exercise both handlers.
	// Require ErrCheckHasChargeAdjustment and unchanged allocations.
}
```

- [ ] **Step 2: Run focused tests to verify they fail**

Run: `go test -count=1 -tags=integration ./internal/sales -run 'TestSplitAndMergeRejectLiveChargeAdjustment'`

Expected: FAIL because restructuring currently checks Payments only.

- [ ] **Step 3: Add the guard after Check locks and before rewrites**

```go
func assertNoLiveChargeAdjustments(ctx context.Context, q *sqlc.Queries, ids []uuid.UUID) error {
	n, err := q.CountLiveChargeAdjustmentsForChecks(ctx, ids)
	if err != nil {
		return fmt.Errorf("count live charge adjustments: %w", err)
	}
	if n != 0 {
		return ErrCheckHasChargeAdjustment
	}
	return nil
}
```

Invoke it in both handlers immediately after `lockChecks`, before any charge/allocation mutation.

- [ ] **Step 4: Run restructuring regression tests**

Run: `go test ./internal/sales -run 'TestValidateSplitItems|TestNormalizeSplitItems'`

Run: `go test -count=1 -tags=integration ./internal/sales -run 'TestSplitCheck|TestMergeChecks|TestSplitAndMergeRejectLiveChargeAdjustment'`

Expected: PASS.

- [ ] **Step 5: Commit the restructuring guard**

```bash
git add internal/sales/check_restructuring.go internal/sales/check_restructuring_test.go internal/sales/check_restructuring_integration_test.go internal/sales/errors.go
git commit -m "fix(sales): protect adjusted checks from restructuring"
```

### Task 6: Implement Live And Post-Sale Comp

**Files:**
- Create: `internal/sales/comp.go`
- Create: `internal/sales/comp_test.go`
- Create: `internal/sales/comp_integration_test.go`
- Modify: `internal/sales/domain.go`
- Modify: `internal/sales/dto.go`
- Modify: `internal/sales/errors.go`
- Modify: `internal/sales/routes.go:11-120`
- Modify: `internal/sales/http.go`
- Modify: `internal/sales/sales_integration_test.go`

**Interfaces:**
- Produces `CompWasteCommand`, `CompResponse`, `PostSaleCorrectionResponse`, `CompResult`, `CompWasteHandler`, and `NewCompWasteHandler`.
- Uses `OpCompWaste = "sales.comp_waste"`, Manager Approval for `sales.operate`, and scopes `LIVE_CHECK` or `POST_SALE`.
- Live result carries `ServiceSession *ServiceSessionResponse`; post-sale result carries `CompletedSaleID *uuid.UUID`, `OutstandingPostSaleRefundVND *int64`, and no Service Session.

- [ ] **Step 1: Write failing validation and result-shape tests**

```go
type CompWasteCommand struct {
	RequestID uuid.UUID `json:"request_id"`
	WasteID uuid.UUID `json:"-"`
	Reason string `json:"reason"`
	Note *string `json:"note"`
	ManagerApproval ManagerApprovalInput `json:"manager_approval"`
}

type CompResult struct {
	Scope string `json:"scope"`
	Comp CompResponse `json:"comp"`
	ServiceSession *ServiceSessionResponse `json:"service_session,omitempty"`
	CompletedSaleID *uuid.UUID `json:"completed_sale_id,omitempty"`
	OutstandingPostSaleRefundVND *int64 `json:"outstanding_post_sale_refund_vnd,omitempty"`
	PostSaleCorrections []PostSaleCorrectionResponse `json:"post_sale_corrections,omitempty"`
}
```

Define `PostSaleCorrectionResponse` in this task with `Adjustment ChargeAdjustmentResponse`, `Comp CompResponse`, `Refunds []RefundResponse`, and `OutstandingRefundVND int64`. Assert exactly one discriminator branch serializes, the post-sale history list is non-null in that branch, and no credential field appears.

- [ ] **Step 2: Run focused unit tests to verify they fail**

Run: `go test ./internal/sales -run 'TestCompValidation|TestCompResultJSON'`

Expected: FAIL because Comp contracts do not exist.

- [ ] **Step 3: Write failing integration tests**

Cover active charged Waste, duplicate Comp, Wasted Remake, paid live Comp pending Refund, open Check settled by Comp, closed-sale Comp, missing open current Shift, Manager approval denial/replay, audit details, and injected rollback failures.

- [ ] **Step 4: Implement Comp with structural scope selection**

Resolve source ids without locks, then lock Check, Session, current Shift, Waste, and unit. Revalidate `WASTED`, standard priority, allocation ownership, and Session state. If active, insert `LIVE_CHECK`, update/verify charge, apply settlement, insert Comp and audits, and load the Service Session. If closed, resolve its Completed Sale, insert `POST_SALE` without changing Check/session/sale core rows, insert Comp and audits, and return outstanding amount.

```go
func (h *CompWasteHandler) Handle(
	ctx context.Context,
	actor Actor,
	cmd CompWasteCommand,
) (int, CompResult, error)
```

- [ ] **Step 5: Wire HTTP and authorization**

Register `POST /sales/wastes/:waste_id/comp`; return `201`; annotate Bearer auth and all documented status codes.

- [ ] **Step 6: Run focused tests**

Run: `go test ./internal/sales -run 'TestComp'`

Run: `go test -count=1 -tags=integration ./internal/sales -run 'TestComp|TestSalesHTTPComp'`

Expected: PASS.

- [ ] **Step 7: Commit Comp**

```bash
git add internal/sales/comp.go internal/sales/comp_test.go internal/sales/comp_integration_test.go internal/sales/domain.go internal/sales/dto.go internal/sales/errors.go internal/sales/routes.go internal/sales/http.go internal/sales/sales_integration_test.go
git commit -m "feat(sales): add waste comp corrections"
```

### Task 7: Record Cash And Manual QR Refunds

**Files:**
- Create: `internal/sales/refund.go`
- Create: `internal/sales/refund_test.go`
- Create: `internal/sales/refund_integration_test.go`
- Modify: `internal/sales/domain.go`
- Modify: `internal/sales/dto.go`
- Modify: `internal/sales/errors.go`
- Modify: `internal/sales/routes.go`
- Modify: `internal/sales/http.go`
- Modify: `internal/sales/sales_integration_test.go`

**Interfaces:**
- Produces `RecordRefundCommand`, `RefundAdjustmentAllocationInput`, `RefundPaymentAllocationInput`, `RefundResult`, `RecordRefundHandler`, and `NewRecordRefundHandler`.
- Uses `OpRecordRefund = "sales.record_refund"` and one Manager Approval.
- Normalizes both allocation collections by UUID for fingerprinting and locking; response collections use the same stable order.

- [ ] **Step 1: Write failing allocation validation tests**

Cover empty lists, zero ids, duplicate ids, non-positive amounts, guarded sum overflow, unequal sums, invalid method/reason/note, and normalization of reordered allocations.

```go
type RecordRefundCommand struct {
	RequestID uuid.UUID `json:"request_id"`
	CheckID uuid.UUID `json:"check_id"`
	Method string `json:"method"`
	AdjustmentAllocations []RefundAdjustmentAllocationInput `json:"adjustment_allocations"`
	PaymentAllocations []RefundPaymentAllocationInput `json:"payment_allocations"`
	Reason string `json:"reason"`
	Note *string `json:"note"`
	ManagerApproval ManagerApprovalInput `json:"manager_approval"`
}

type RefundResult struct {
	Scope string `json:"scope"`
	Refund RefundResponse `json:"refund"`
	ServiceSession *ServiceSessionResponse `json:"service_session,omitempty"`
	CompletedSaleID *uuid.UUID `json:"completed_sale_id,omitempty"`
	PostSaleCorrections []PostSaleCorrectionResponse `json:"post_sale_corrections,omitempty"`
}
```

- [ ] **Step 2: Run unit tests to verify they fail**

Run: `go test ./internal/sales -run 'TestValidateRefund|TestNormalizeRefundAllocations'`

Expected: FAIL because Refund validation does not exist.

- [ ] **Step 3: Write failing live and post-sale integration tests**

Cover immediate Cash completion, pending Manual QR intent, mixed-method rejection, wrong Check/sale/scope, voided Payment, exhausted Adjustment capacity, exhausted Payment capacity, pending allocation reservation, partial Refund, post-sale Refund, replay, authority/approval, and atomic failure rollback.

- [ ] **Step 4: Implement the dual-capacity transaction**

Lock Check, Session, open current Shift, selected Payments by UUID, then selected Adjustments by UUID. Revalidate method, Check, scope, Completed Sale, no Void, and remaining capacities including pending allocations. For live scope, reject amounts above `pending_refund_vnd`. Insert one Refund and both allocation sets; insert completion in the same transaction only for Cash.

```go
func (h *RecordRefundHandler) Handle(
	ctx context.Context,
	actor Actor,
	cmd RecordRefundCommand,
) (int, RefundResult, error)
```

Write `REFUND_RECORDED` and, for Cash, `REFUND_COMPLETED`; return `201`. Load the live Service Session or post-sale correction history only after all writes.

- [ ] **Step 5: Wire `POST /sales/refunds`**

Add handler parsing and Swagger annotations for `201/400/401/403/404/409/422/500`. Ensure approval credentials never appear in the result or error message.

- [ ] **Step 6: Run focused Refund tests**

Run: `go test ./internal/sales -run 'TestValidateRefund|TestNormalizeRefundAllocations'`

Run: `go test -count=1 -tags=integration ./internal/sales -run 'TestRecordRefund|TestSalesHTTPRefund'`

Expected: PASS.

- [ ] **Step 7: Commit Refund recording**

```bash
git add internal/sales/refund.go internal/sales/refund_test.go internal/sales/refund_integration_test.go internal/sales/domain.go internal/sales/dto.go internal/sales/errors.go internal/sales/routes.go internal/sales/http.go internal/sales/sales_integration_test.go
git commit -m "feat(sales): record allocated refunds"
```

### Task 8: Confirm Manual QR Refund Completion

**Files:**
- Modify: `internal/sales/refund.go`
- Modify: `internal/sales/refund_test.go`
- Modify: `internal/sales/refund_integration_test.go`
- Modify: `internal/sales/routes.go`
- Modify: `internal/sales/http.go`
- Modify: `internal/sales/sales_integration_test.go`

**Interfaces:**
- Produces `ConfirmManualQRRefundCommand`, `ConfirmManualQRRefundHandler`, and `NewConfirmManualQRRefundHandler`.
- Uses `OpConfirmQRRefund = "sales.confirm_qr_refund"`, current `sales.operate`, and no new Manager Approval.
- Reuses `RefundResult` and returns derived state `COMPLETED` with `200`.

- [ ] **Step 1: Write failing confirmation tests**

Cover reference trimming and 100-rune bound, Cash rejection, wrong Shift, already completed conflict, authority replay, exact replay, completion audit, and effective-receipt projection changing only after completion.

```go
type ConfirmManualQRRefundCommand struct {
	RequestID uuid.UUID `json:"request_id"`
	RefundID uuid.UUID `json:"-"`
	TransactionReference *string `json:"transaction_reference"`
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -count=1 -tags=integration ./internal/sales -run 'TestConfirmManualQRRefund'`

Expected: FAIL because confirmation is not implemented.

- [ ] **Step 3: Implement append-only completion**

Lock the Refund and current Shift, verify `MANUAL_QR`, no completion, and same Shift. Append one completion with the confirmer identity/session and database timestamp. Write `MANUAL_QR_REFUND_COMPLETED`; never edit the Refund row or its allocations.

- [ ] **Step 4: Wire the confirmation route**

Register `POST /sales/refunds/:refund_id/confirm` and document `200/400/401/403/404/409/500`.

- [ ] **Step 5: Run focused tests**

Run: `go test ./internal/sales -run 'TestNormalizeRefundReference'`

Run: `go test -count=1 -tags=integration ./internal/sales -run 'TestConfirmManualQRRefund|TestSalesHTTPConfirmRefund'`

Expected: PASS.

- [ ] **Step 6: Commit Manual QR completion**

```bash
git add internal/sales/refund.go internal/sales/refund_test.go internal/sales/refund_integration_test.go internal/sales/routes.go internal/sales/http.go internal/sales/sales_integration_test.go
git commit -m "feat(sales): confirm manual QR refunds"
```

### Task 9: Void Whole Payments And Reopen Checks

**Files:**
- Create: `internal/sales/payment_void.go`
- Create: `internal/sales/payment_void_test.go`
- Create: `internal/sales/payment_void_integration_test.go`
- Modify: `internal/sales/domain.go`
- Modify: `internal/sales/dto.go`
- Modify: `internal/sales/errors.go`
- Modify: `internal/sales/routes.go`
- Modify: `internal/sales/http.go`
- Modify: `internal/sales/sales_integration_test.go`

**Interfaces:**
- Produces `VoidPaymentCommand`, `VoidPaymentHandler`, and `NewVoidPaymentHandler`.
- Uses `OpVoidPayment = "sales.void_payment"` and one Manager Approval.
- Returns `ServiceSessionResponse` with `201`; Void evidence is visible under the source `PaymentResponse`.

- [ ] **Step 1: Write failing validation and integration tests**

Cover all reason/note cases; Cash and QR; reopened versus still-covered Check; already voided; any pending/completed Refund allocation; merged Check; closed Session; original Shift closed/not current; source immutability; replay; approval; audits; and rollback injection.

```go
type VoidPaymentCommand struct {
	RequestID uuid.UUID `json:"request_id"`
	PaymentID uuid.UUID `json:"-"`
	Reason string `json:"reason"`
	Note *string `json:"note"`
	ManagerApproval ManagerApprovalInput `json:"manager_approval"`
}
```

- [ ] **Step 2: Run focused tests to verify they fail**

Run: `go test -count=1 -tags=integration ./internal/sales -run 'TestVoidPayment'`

Expected: FAIL because Payment Void does not exist.

- [ ] **Step 3: Implement Void in common lock order**

Pre-resolve Payment ownership; lock Check, active Session, original/current Shift, then Payment. Reject merged Check, existing Void, or any Refund allocation. Insert a whole Void with frozen `applied_amount_vnd`, recompute financials, and if balance becomes positive atomically set `OPEN` while clearing all four settlement-evidence columns.

```go
func (h *VoidPaymentHandler) Handle(
	ctx context.Context,
	actor Actor,
	cmd VoidPaymentCommand,
) (int, ServiceSessionResponse, error)
```

Write `PAYMENT_VOIDED` and conditionally `CHECK_REOPENED_AFTER_PAYMENT_VOID`. Never update or delete `payments`.

- [ ] **Step 4: Wire `POST /sales/payments/:payment_id/void`**

Document `201/400/401/403/404/409/500` and map `PAYMENT_ALREADY_VOIDED`, `PAYMENT_HAS_REFUND`, and `PAYMENT_VOID_SHIFT_CLOSED` without string matching.

- [ ] **Step 5: Run Payment Void tests**

Run: `go test ./internal/sales -run 'TestVoidPaymentValidation'`

Run: `go test -count=1 -tags=integration ./internal/sales -run 'TestVoidPayment|TestSalesHTTPVoidPayment'`

Expected: PASS.

- [ ] **Step 6: Commit Payment Void**

```bash
git add internal/sales/payment_void.go internal/sales/payment_void_test.go internal/sales/payment_void_integration_test.go internal/sales/domain.go internal/sales/dto.go internal/sales/errors.go internal/sales/routes.go internal/sales/http.go internal/sales/sales_integration_test.go
git commit -m "feat(sales): add whole payment voids"
```

### Task 10: Enforce Pending Refund At Service Session Closure

**Files:**
- Modify: `internal/sales/closure.go:5-87`
- Modify: `internal/sales/closure_test.go`
- Modify: `internal/sales/session_close.go`
- Modify: `internal/sales/session_close_integration_test.go`
- Modify: `internal/sales/errors.go`

**Interfaces:**
- Extends `ClosureReadiness` with `AllRefundsResolved bool` and `PendingRefundCheckIDs []uuid.UUID`.
- Produces `ErrPendingRefundForClosure`, mapped to `PENDING_REFUND_FOR_CLOSURE` and `409`.
- Consumes each Check's `PendingRefundVND` from Task 3.

- [ ] **Step 1: Write failing precedence tests**

Pin unsettled plus pending Refund, pending Refund plus unsubmitted work, and fully ready cases.

```go
func TestClosureReadinessPrecedence(t *testing.T) {
	// Require unsettled, then pending Refund, then unsubmitted work,
	// then missing Order, then nonterminal Preparation Unit.
}
```

- [ ] **Step 2: Run unit tests to verify they fail**

Run: `go test ./internal/sales -run 'TestClosureReadiness'`

Expected: FAIL because closure ignores pending Refunds.

- [ ] **Step 3: Implement the exact readiness order**

```go
func (r ClosureReadiness) Err() error {
	switch {
	case !r.AllChecksSettled:
		return ErrCheckNotSettledForClosure
	case !r.AllRefundsResolved:
		return ErrPendingRefundForClosure
	case len(r.UnsubmittedCommittedItemIDs) > 0:
		return ErrUnsubmittedWorkForClosure
	case !r.HasOrder:
		return ErrOrderRequiredForClosure
	case !r.AllPreparationDone:
		return ErrUnfulfilledPreparationForClosure
	default:
		return nil
	}
}
```

Initialize `PendingRefundCheckIDs` as an empty slice and set `AllRefundsResolved` from its length. The closure transaction already holds the Session lock; retain that serialization.

- [ ] **Step 4: Add closure integration cases**

Assert a settled Check with pending Refund rejects, Cash Refund resolution permits closure, Manual QR confirmation permits closure, and no Completed Sale row/idempotent success result is written on rejection.

- [ ] **Step 5: Run closure tests**

Run: `go test ./internal/sales -run 'TestClosureReadiness'`

Run: `go test -count=1 -tags=integration ./internal/sales -run 'TestCloseServiceSession'`

Expected: PASS.

- [ ] **Step 6: Commit closure policy**

```bash
git add internal/sales/closure.go internal/sales/closure_test.go internal/sales/session_close.go internal/sales/session_close_integration_test.go internal/sales/errors.go
git commit -m "feat(sales): block closure on pending refunds"
```

### Task 11: Preserve Completed Sale Core And Add Post-Sale History

**Files:**
- Modify: `internal/sales/dto.go:384-428`
- Modify: `internal/sales/completed_sale_reads.go:14-103`
- Modify: `internal/sales/completed_sale_integration_test.go`
- Modify: `internal/sales/projection_integration_test.go`

**Interfaces:**
- Reuses `PostSaleCorrectionResponse` from Task 6 and produces `CompletedSaleResponse.PostSaleCorrections`.
- Extends `CompletedSaleCheckResponse` with `BaseChargeVND`, `TotalVoidedVND`, `TotalRefundedVND`, `EffectiveReceivedVND`, `PendingRefundVND`, `ChargeAdjustments`, and `Refunds`, matching the structurally filtered core `CheckResponse`.
- Uses `SnapshotCompletedSaleCore` from Task 3 for core Check loading.
- Loads additive history only by `completed_sale_id`, ordered by occurrence then id.

- [ ] **Step 1: Write a failing immutable-core regression test**

Close a sale with pre-close Cancellation/Comp/Refund/Void facts, marshal its core fields, add post-sale Comp and Refund, reload, and compare core bytes. Assert only `post_sale_corrections` changes.

```go
type completedSaleCore struct {
	Checks []sales.CompletedSaleCheckResponse `json:"checks"`
	Orders []sales.OrderResponse `json:"orders"`
	PreparationUnits []sales.PreparationUnitResponse `json:"preparation_units"`
	PreparationHistory []sales.PreparationTransitionResponse `json:"preparation_history"`
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -count=1 -tags=integration ./internal/sales -run 'TestCompletedSaleCoreRemainsImmutable'`

Expected: FAIL because unrestricted current loaders do not separate post-sale facts and no history collection exists.

- [ ] **Step 3: Add structural snapshot DTOs and loaders**

```go
type CompletedSaleCheckResponse struct {
	ID uuid.UUID `json:"id"`
	State string `json:"state"`
	BaseChargeVND int64 `json:"base_charge_vnd"`
	ChargeVND int64 `json:"charge_vnd"`
	TotalAppliedVND int64 `json:"total_applied_vnd"`
	TotalVoidedVND int64 `json:"total_voided_vnd"`
	TotalRefundedVND int64 `json:"total_refunded_vnd"`
	EffectiveReceivedVND int64 `json:"effective_received_vnd"`
	BalanceVND int64 `json:"balance_vnd"`
	PendingRefundVND int64 `json:"pending_refund_vnd"`
	Payments []PaymentResponse `json:"payments"`
	Allocations []ChargeAllocationResponse `json:"allocations"`
	ChargeAdjustments []ChargeAdjustmentResponse `json:"charge_adjustments"`
	Refunds []RefundResponse `json:"refunds"`
}
```

Initialize `PostSaleCorrections` to `[]`. Core loaders include only `LIVE_CHECK` adjustments and Refunds with null `completed_sale_id`; Payment refundable capacity in core ignores post-sale Refund allocations. History loaders include only rows carrying this Completed Sale id. Do not compare `created_at` to `completed_at`.

- [ ] **Step 4: Run Completed Sale tests**

Run: `go test -count=1 -tags=integration ./internal/sales -run 'TestCompletedSale'`

Expected: PASS, including byte-equivalent core and deterministic additive history.

- [ ] **Step 5: Commit Completed Sale history**

```bash
git add internal/sales/dto.go internal/sales/completed_sale_reads.go internal/sales/completed_sale_integration_test.go internal/sales/projection_integration_test.go
git commit -m "feat(sales): add immutable post-sale correction history"
```

### Task 12: Complete Current-Shift Reconciliation

**Files:**
- Modify: `internal/shift/domain.go:139-192`
- Modify: `internal/shift/domain_test.go`
- Modify: `internal/shift/dto.go:68-86`
- Modify: `internal/shift/current.go:20-109`
- Modify: `internal/shift/cash_movement.go:128-141`
- Modify: `sql/queries/shift.sql`
- Modify: `internal/shift/expected_cash_integration_test.go`
- Modify: `internal/shift/current_integration_test.go`
- Modify: `internal/shift/cash_movement_integration_test.go`
- Modify: `internal/shift/shift_integration_test.go`
- Regenerate: `internal/database/sqlc/shift.sql.go`
- Regenerate: `internal/database/sqlc/querier.go`

**Interfaces:**
- Changes `ComputeExpectedCash` to accept `cashPaymentVoidVND` and `cashRefundVND`.
- Replaces the `SumCashPaymentsForShift` query with `GetShiftReconciliationTotals`, returning all nine reconciliation scalars plus the Expected Cash inputs, and adds `ListShiftRefunds`.
- Extends `CurrentSalesShiftResponse` with all nine scalar reconciliation fields and `Refunds []RefundSummaryResponse`.
- Produces `RefundSummaryResponse`.
- Consumes `GetShiftReconciliationTotals` from both `current.go` and `cash_movement.go`; both call sites rebuild Expected Cash through the new six-argument `ComputeExpectedCash`.

- [ ] **Step 1: Write failing Expected Cash unit tests**

```go
func ComputeExpectedCash(
	openingFloatVND int64,
	cashPaymentVND int64,
	cashPaymentVoidVND int64,
	cashRefundVND int64,
	payInVND int64,
	payOutVND int64,
) (int64, error)
```

Cover `opening + cash payments - cash voids - completed cash refunds + pay ins - pay outs`, negative valid totals, and every overflow/underflow edge.

- [ ] **Step 2: Run unit tests to verify they fail**

Run: `go test ./internal/shift -run 'TestComputeExpectedCash'`

Expected: FAIL (compile error) because the new six-argument formula does not exist yet.

- [ ] **Step 3: Replace the reconciliation query and update both call sites**

In `sql/queries/shift.sql`, replace `SumCashPaymentsForShift` with `GetShiftReconciliationTotals` returning `cash_payment_vnd`, `cash_payment_void_vnd`, `cash_refund_vnd`, `manual_qr_payment_vnd`, `manual_qr_payment_void_vnd`, `manual_qr_refund_vnd`, `pending_manual_qr_refund_vnd`, `pending_refund_vnd`, and `unresolved_post_sale_adjustment_vnd`, plus `ListShiftRefunds` ordered by `(created_at, id)` with no credential columns. Run `make sqlc`, then update `cash_movement.go` and `current.go` to call it and the new formula.

- [ ] **Step 4: Extend current-Shift DTO and loader**

```go
type CurrentSalesShiftResponse struct {
	SalesShiftResponse
	ExpectedCashVND int64 `json:"expected_cash_vnd"`
	CashPaymentVND int64 `json:"cash_payment_vnd"`
	CashPaymentVoidVND int64 `json:"cash_payment_void_vnd"`
	CashRefundVND int64 `json:"cash_refund_vnd"`
	ManualQRPaymentVND int64 `json:"manual_qr_payment_vnd"`
	ManualQRPaymentVoidVND int64 `json:"manual_qr_payment_void_vnd"`
	ManualQRRefundVND int64 `json:"manual_qr_refund_vnd"`
	PendingManualQRRefundVND int64 `json:"pending_manual_qr_refund_vnd"`
	PendingRefundVND int64 `json:"pending_refund_vnd"`
	UnresolvedPostSaleAdjustmentVND int64 `json:"unresolved_post_sale_adjustment_vnd"`
	CashMovements []CashMovementResponse `json:"cash_movements"`
	Refunds []RefundSummaryResponse `json:"refunds"`
}
```

Run all reads inside the existing read-only repeatable-read transaction. Initialize `Refunds` to an empty slice and derive state from completion existence.

- [ ] **Step 5: Add reconciliation integration tests**

Cover Cash Payment/Void, completed Cash Refund, QR Payment/Void, pending/completed QR Refund, live pending Refund, unresolved post-sale adjustment, cross-Shift exclusion, stable Refund ordering, and `[]` serialization.

- [ ] **Step 6: Run Shift tests**

Run: `go test ./internal/shift`

Run: `go test -count=1 -tags=integration ./internal/shift -run 'TestExpectedCash|TestCurrentShift|TestCashMovement|TestShiftHTTP'`

Expected: PASS; no Shift Close route exists.

- [ ] **Step 7: Commit Shift reconciliation**

```bash
git add internal/shift/domain.go internal/shift/domain_test.go internal/shift/dto.go internal/shift/current.go internal/shift/cash_movement.go sql/queries/shift.sql internal/database/sqlc/shift.sql.go internal/database/sqlc/querier.go internal/shift/expected_cash_integration_test.go internal/shift/current_integration_test.go internal/shift/cash_movement_integration_test.go internal/shift/shift_integration_test.go
git commit -m "feat(shift): reconcile refunds and payment voids"
```

### Task 13: Pin Concurrency, Public Contracts, Decisions, And Full Regression

**Files:**
- Create: `internal/preparation/cancel_concurrency_integration_test.go`
- Create: `internal/sales/correction_concurrency_integration_test.go`
- Modify: `internal/preparation/preparation_integration_test.go`
- Modify: `internal/sales/sales_integration_test.go`
- Modify: `internal/preparation/swagger_test.go`
- Modify: `internal/sales/routes_test.go`
- Regenerate: `docs/docs.go`
- Regenerate: `docs/swagger.json`
- Regenerate: `docs/swagger.yaml`
- Modify: `spec/decisions.md`
- Modify: `MIGRATE_PLAN.md`

**Interfaces:**
- Verifies all five operation routes and stable error/status contracts.
- Records ADR-040 through ADR-046 exactly as approved in spec section 17.
- Marks Phase 6.C complete only after every verification command passes.

- [ ] **Step 1: Write deterministic Cancellation race tests**

Synchronize goroutines at lock boundaries and assert explicit accepted outcomes for Cancel versus advance, Waste, Payment, Split/Merge, Submit, and closure.

```go
func TestCancelRaces(t *testing.T) {
	// Each subtest uses channels to hold the first transaction after its first lock.
	// Accept one business winner, or PostgreSQL 40P01 only for Cancel versus Submit.
	// Assert no partial adjustment, fact, alert, audit, or idempotency result survives.
}
```

- [ ] **Step 2: Write deterministic Sales correction race tests**

Cover Comp versus closure, active Comp versus Submit, two Comps for one Waste, overlapping Refunds on one Adjustment, overlapping Refunds on one Payment, Refund versus Void, and Void versus replacement Payment. Assert no hang, duplicate capacity, negative balance, or partial fact.

- [ ] **Step 3: Run focused race suites**

Run: `go test -count=1 -race -tags=integration ./internal/preparation -run 'TestCancelRaces'`

Run: `go test -count=1 -race -tags=integration ./internal/sales -run 'TestCorrectionRaces'`

Expected: PASS. Only Submit pairings may surface an asserted `40P01` outcome.

- [ ] **Step 4: Complete HTTP authorization, replay, and privacy matrices**

Add all five routes to anonymous/Barista denial inventories. Exercise Cashier/Manager Cancellation, Manager-approved Comp/Refund/Void, confirmation without second approval, malformed UUID/body, every stable error mapping, changed-payload conflict, current-authority replay denial, credential rotation, and no credential persistence/response/log evidence.

- [ ] **Step 5: Regenerate and verify Swagger**

Run: `make swagger`

Assert generated paths include:

```text
/preparation/units/cancel
/sales/wastes/{waste_id}/comp
/sales/refunds
/sales/refunds/{refund_id}/confirm
/sales/payments/{payment_id}/void
```

Run: `go test ./internal/preparation ./internal/sales -run 'TestSwagger|TestRoutes'`

Expected: PASS with Bearer security and documented success/failure codes.

- [ ] **Step 6: Add ADR-040 through ADR-046**

Copy the approved decision titles and consequences from spec section 17 into `spec/decisions.md` verbatim:

```text
ADR-040: Cancellation is one cross-slice PostgreSQL consistency boundary.
ADR-041: Charge reduction is append-only and has live versus post-sale scope.
ADR-042: Refund consumes two independently locked capacities.
ADR-043: Refund completion is an append-only fact.
ADR-044: A Check remains settled while Refund is pending.
ADR-045: Payment Void is whole, append-only, and open-original-Shift only.
ADR-046: Expected Cash uses valid Cash Payments less completed Cash Refunds.
```

Do not describe Shift Close as implemented. Update `MIGRATE_PLAN.md` Phase 6.C status and checklist only after the next step is green.

- [ ] **Step 7: Run formatting, generation-drift, build, vet, and lint gates**

Run: `gofmt -w internal/preparation internal/sales internal/shift`

Run: `make sqlc`

Run: `make swagger`

Run: `git diff --check -- internal/database/sqlc docs/docs.go docs/swagger.json docs/swagger.yaml`

Run: `go build ./...`

Run: `go vet ./...`

Run: `make lint`

Expected: `git diff --check` reports no whitespace errors; the remaining commands exit `0`.

- [ ] **Step 8: Run complete unit and integration regression suites**

Run: `go test -race ./...`

Run: `make test-integration`

Expected: PASS for Auth, Catalog, Tables, Shift, Sales 5A-5D, Preparation 6A-6C, failure injection, and concurrency.

- [ ] **Step 9: Inspect final changes before committing**

Run: `git status --short`

Run: `git diff --check`

Run: `git diff --stat`

Expected: only Phase 6.C implementation, generated contracts, approved ADRs, roadmap status, this plan, and its approved spec are present; whitespace check is clean.

- [ ] **Step 10: Commit public contracts and Phase completion**

```bash
git add internal/preparation/cancel_concurrency_integration_test.go internal/sales/correction_concurrency_integration_test.go internal/preparation/preparation_integration_test.go internal/sales/sales_integration_test.go internal/preparation/swagger_test.go internal/sales/routes_test.go docs/docs.go docs/swagger.json docs/swagger.yaml spec/decisions.md MIGRATE_PLAN.md docs/superpowers/specs/2026-09-18-preparation-financial-corrections-design.md docs/superpowers/plans/2026-09-18-preparation-financial-corrections.md
git commit -m "feat: complete preparation financial corrections"
```
