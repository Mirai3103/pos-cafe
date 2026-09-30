# Phase 08 Checkout Recovery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let staff see paid-but-unsubmitted work, withdraw it so the existing Refund can return the money, and end an unsubmitted checkout as an auditable Abandoned Checkout.

**Architecture:** Awaiting Submission is derived in the Service Session projection, not stored. Two new `internal/sales` commands, Cancel Awaiting Submission and Abandon Checkout, run on the shared `ExecuteMutation` pipeline. A new `WITHDRAWAL` Charge Adjustment kind feeds the unchanged Refund. Service Session closure and Sales Shift closure gain an Awaiting Submission blocker.

**Tech Stack:** Go, Echo, PostgreSQL, sqlc (`make sqlc`), swag (`make swagger`), testify, integration tests behind `-tags=integration`.

**Spec:** [`docs/superpowers/specs/2026-10-02-phase-08-checkout-recovery-design.md`](../specs/2026-10-02-phase-08-checkout-recovery-design.md)

## Global Constraints

- Money is whole VND in `int64` / `BIGINT`; use `AddCharge` for sums.
- Every mutation goes through `ExecuteMutation`: request id, idempotency, one Audit Event in the same transaction (ADR-048).
- Reason catalog, both commands: `CUSTOMER_LEFT`, `CUSTOMER_REQUEST`, `SYSTEM_FAILURE`, `OTHER`; a note is required for `OTHER`, 1–500 runes (`MaxCorrectionNoteRunes`).
- Both commands require `sales.operate` and an open Sales Shift; no Manager Approval.
- Lock order for both commands: open Shift FOR SHARE → Service Session → draft → Checks ascending `(created_at, id)`.
- Error codes (exact): `NOTHING_AWAITING_SUBMISSION`, `SESSION_HAS_ORDER`, `PAYMENT_REQUIRES_REFUND`, `AWAITING_SUBMISSION_FOR_CLOSURE` (all 409), `SHIFT_AWAITING_SUBMISSION` (409), `INVALID_INPUT` (400).
- Audit event types (exact): `AWAITING_SUBMISSION_CANCELLED`, `CHECKOUT_ABANDONED`.
- Routes (exact): `POST /api/v1/sales/service-sessions/:id/cancel-awaiting-submission`, `POST /api/v1/sales/service-sessions/:id/abandon`.
- Integration tests need `make docker-up` and run with `make test-integration-fast` or `go test -count=1 -tags=integration ./internal/sales/...`.

## File Map

| File | Change |
| --- | --- |
| `internal/database/migrations/000017_add_checkout_recovery.sql` | Create: schema of spec §6 |
| `sql/queries/sales.sql` | Add recovery queries |
| `sql/queries/shift.sql` | Add `awaiting_submission_count` to `GetGlobalShiftClosureBlockers` |
| `internal/database/sqlc/*` | Regenerated |
| `internal/sales/domain.go` | State, kind, op, event, reason constants; validation |
| `internal/sales/dto.go` | Command, projection fields, `AbandonedCheckoutResponse` |
| `internal/sales/errors.go` | Four sentinels and their codes |
| `internal/sales/closure.go` | `DeriveAwaitingSubmission`, new closure precedence |
| `internal/sales/projection.go` | Awaiting flag, `withdrawn`, abandoned record, `ABANDONED` guard exemption |
| `internal/sales/session_close.go` | Extract `releaseHeldTableAssignments` |
| `internal/sales/checkout_recovery.go` | Create: both handlers |
| `internal/sales/http.go`, `routes.go` | Two routes |
| `internal/sales/comp.go`, `internal/preparation/cancel.go` | `preparation_unit_id` nullable ripple |
| `internal/shift/reconciliation.go`, `errors.go` | New blocker |
| `internal/sales/checkout_recovery_integration_test.go` | Create: every Phase 08 integration test |
| `spec/decisions.md`, `ROADMAP.md`, `docs/backlog/*` | ADR-064..066, status |

---

### Task 1: Schema, generated code, and constants

**Files:**
- Create: `internal/database/migrations/000017_add_checkout_recovery.sql`
- Modify: `internal/sales/domain.go`, `internal/sales/dto.go:332`, `internal/sales/projection.go:449`, `internal/sales/comp.go:327,345`, `internal/preparation/cancel.go:~838`, `internal/sales/completed_sale_integration_test.go:266`
- Test: `internal/sales/schema_integration_test.go`

**Interfaces:**
- Produces: constants `StateAbandoned = "ABANDONED"`, `CheckStateAbandoned = "ABANDONED"`, `DraftStateCancelled = "CANCELLED"`, `ChargeAdjustmentKindWithdrawal = "WITHDRAWAL"`; `ChargeAdjustmentResponse.PreparationUnitID` becomes `*uuid.UUID`; sqlc `InsertChargeAdjustmentParams.PreparationUnitID` becomes `uuid.NullUUID`.

- [ ] **Step 1: Write the failing schema test**

Append to `internal/sales/schema_integration_test.go`:

```go
// TestCheckoutRecoverySchema pins migration 000017 (Phase 08, spec §6).
func TestCheckoutRecoverySchema(t *testing.T) {
	db, _ := openSalesTestDB(t)
	ctx := context.Background()

	def := func(name string) string {
		var clause string
		require.NoError(t, db.QueryRowContext(ctx, `
			SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = $1`,
			name).Scan(&clause))
		return clause
	}
	assert.Contains(t, def("service_session_state_valid"), "ABANDONED")
	assert.Contains(t, def("check_state_valid"), "ABANDONED")
	assert.Contains(t, def("check_settlement_evidence_valid"), "ABANDONED")
	assert.Contains(t, def("order_draft_state_valid"), "CANCELLED")
	assert.Contains(t, def("charge_adjustment_kind_source_valid"), "WITHDRAWAL")

	var nullable string
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT is_nullable FROM information_schema.columns
		WHERE table_name = 'charge_adjustments' AND column_name = 'preparation_unit_id'`).
		Scan(&nullable))
	assert.Equal(t, "YES", nullable)

	var index string
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT indexdef FROM pg_indexes
		WHERE indexname = 'charge_adjustment_withdrawal_allocation_unique'`).Scan(&index))
	assert.Contains(t, index, "charge_allocation_id")
	assert.Contains(t, index, "WITHDRAWAL")

	var table int
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT count(*) FROM information_schema.tables
		WHERE table_name = 'abandoned_checkouts'`).Scan(&table))
	assert.Equal(t, 1, table)
}
```

Also change `TestServiceSessionStateDomain`: it asserts `NotContains "CANCELLED"`, which stays true. Leave it.

- [ ] **Step 2: Run it to verify it fails**

Run: `go test -count=1 -tags=integration ./internal/sales/ -run TestCheckoutRecoverySchema`
Expected: FAIL, `ABANDONED` not found in `service_session_state_valid`.

- [ ] **Step 3: Write the migration**

Create `internal/database/migrations/000017_add_checkout_recovery.sql`:

```sql
-- 000017: Checkout recovery (Phase 08, ADR-064 to ADR-066).
--
-- A WITHDRAWAL Charge Adjustment removes an unsubmitted Committed Item's
-- charge from its Check so the existing Refund can return the money. It has no
-- Preparation Unit, because unsubmitted work never had one. An abandoned
-- Service Session ends in ABANDONED with its drafts CANCELLED and its live
-- Checks ABANDONED, and one abandoned_checkouts row records who, when, and why.
-- Every statement is rerunnable.

ALTER TABLE charge_adjustments ALTER COLUMN preparation_unit_id DROP NOT NULL;

ALTER TABLE charge_adjustments DROP CONSTRAINT IF EXISTS charge_adjustment_kind_source_valid;
ALTER TABLE charge_adjustments ADD CONSTRAINT charge_adjustment_kind_source_valid CHECK (
       (kind = 'CANCELLATION'
            AND preparation_unit_id IS NOT NULL AND preparation_waste_id IS NULL)
    OR (kind = 'COMP'
            AND preparation_unit_id IS NOT NULL AND preparation_waste_id IS NOT NULL)
    OR (kind = 'WITHDRAWAL'
            AND preparation_unit_id IS NULL AND preparation_waste_id IS NULL
            AND scope = 'LIVE_CHECK')
);

-- No allocation is withdrawn twice. charge_adjustment_kind_unit_unique does not
-- cover WITHDRAWAL: its preparation_unit_id is NULL and NULLs never collide.
CREATE UNIQUE INDEX IF NOT EXISTS charge_adjustment_withdrawal_allocation_unique
    ON charge_adjustments (charge_allocation_id)
    WHERE kind = 'WITHDRAWAL';

ALTER TABLE service_sessions DROP CONSTRAINT IF EXISTS service_session_state_valid;
ALTER TABLE service_sessions ADD CONSTRAINT service_session_state_valid
    CHECK (state IN ('ACTIVE', 'CLOSED', 'ABANDONED'));

ALTER TABLE order_drafts DROP CONSTRAINT IF EXISTS order_draft_state_valid;
ALTER TABLE order_drafts ADD CONSTRAINT order_draft_state_valid
    CHECK (state IN ('EDITABLE', 'COMMITTED', 'CANCELLED'));

ALTER TABLE checks DROP CONSTRAINT IF EXISTS check_state_valid;
ALTER TABLE checks ADD CONSTRAINT check_state_valid
    CHECK (state IN ('OPEN', 'SETTLED', 'MERGED', 'ABANDONED'));

-- An ABANDONED Check keeps whatever settlement evidence it had: a Check that
-- was paid, withdrawn, and refunded was SETTLED first; an unpaid one was not.
ALTER TABLE checks DROP CONSTRAINT IF EXISTS check_settlement_evidence_valid;
ALTER TABLE checks ADD CONSTRAINT check_settlement_evidence_valid CHECK (
       (state = 'OPEN'
            AND merged_into_check_id IS NULL
            AND settled_at IS NULL
            AND settled_by_staff_identity_id IS NULL
            AND settled_during_sales_shift_id IS NULL
            AND settled_staff_access_session_id IS NULL)
    OR (state = 'SETTLED'
            AND merged_into_check_id IS NULL
            AND settled_at IS NOT NULL
            AND settled_by_staff_identity_id IS NOT NULL
            AND settled_during_sales_shift_id IS NOT NULL
            AND settled_staff_access_session_id IS NOT NULL)
    OR (state = 'MERGED'
            AND merged_into_check_id IS NOT NULL
            AND charge_vnd = 0
            AND settled_at IS NULL
            AND settled_by_staff_identity_id IS NULL
            AND settled_during_sales_shift_id IS NULL
            AND settled_staff_access_session_id IS NULL)
    OR (state = 'ABANDONED'
            AND merged_into_check_id IS NULL
            AND ((settled_at IS NULL
                  AND settled_by_staff_identity_id IS NULL
                  AND settled_during_sales_shift_id IS NULL
                  AND settled_staff_access_session_id IS NULL)
              OR (settled_at IS NOT NULL
                  AND settled_by_staff_identity_id IS NOT NULL
                  AND settled_during_sales_shift_id IS NOT NULL
                  AND settled_staff_access_session_id IS NOT NULL)))
);

CREATE TABLE IF NOT EXISTS abandoned_checkouts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    service_session_id UUID NOT NULL
        CONSTRAINT abandoned_checkouts_service_session_id_fkey
        REFERENCES service_sessions (id) ON DELETE RESTRICT,
    sales_shift_id UUID NOT NULL
        CONSTRAINT abandoned_checkouts_sales_shift_id_fkey
        REFERENCES sales_shifts (id) ON DELETE RESTRICT,
    reason TEXT NOT NULL,
    note TEXT,
    actor_staff_identity_id UUID NOT NULL
        CONSTRAINT abandoned_checkouts_actor_staff_identity_id_fkey
        REFERENCES staff_identities (id) ON DELETE RESTRICT,
    staff_access_session_id UUID NOT NULL
        CONSTRAINT abandoned_checkouts_staff_access_session_id_fkey
        REFERENCES staff_access_sessions (id) ON DELETE RESTRICT,
    occurred_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT abandoned_checkout_session_unique UNIQUE (service_session_id),
    CONSTRAINT abandoned_checkout_reason_valid CHECK (
        reason IN ('CUSTOMER_LEFT', 'CUSTOMER_REQUEST', 'SYSTEM_FAILURE', 'OTHER')),
    CONSTRAINT abandoned_checkout_note_valid CHECK (reason <> 'OTHER' OR note IS NOT NULL)
);

COMMENT ON TABLE abandoned_checkouts IS
    'Owned by internal/sales (Phase 08). The terminal record of an Abandoned Checkout.';
```

- [ ] **Step 4: Regenerate sqlc and add constants**

Run: `make sqlc`

In `internal/sales/domain.go`, next to the existing state blocks, add:

```go
// Phase 08 terminal states (spec §6). A Session with no Order that the
// customer left ends ABANDONED; its drafts end CANCELLED and its live Checks
// end ABANDONED.
const (
	StateAbandoned      = "ABANDONED"
	CheckStateAbandoned = "ABANDONED"
	DraftStateCancelled = "CANCELLED"
)

// ChargeAdjustmentKindWithdrawal removes an unsubmitted Committed Item's
// charge from its Check (ADR-065). It names a Charge Allocation and no
// Preparation Unit.
const ChargeAdjustmentKindWithdrawal = "WITHDRAWAL"
```

- [ ] **Step 5: Fix the nullable `preparation_unit_id` ripple**

Run: `go build ./... && go vet -tags=integration ./...`

Every error comes from `PreparationUnitID` changing from `uuid.UUID` to `uuid.NullUUID` in the sqlc structs. Apply exactly these rules:
- Writes (`InsertChargeAdjustmentParams`): wrap as `uuid.NullUUID{UUID: id, Valid: true}`. Sites: `internal/sales/comp.go:345` (`source.PreparationUnitID`), `internal/preparation/cancel.go` `insertCancellationAdjustments` (`id`).
- Comparisons: `internal/sales/comp.go:327` becomes
  `existing.PreparationUnitID.Valid && existing.PreparationUnitID.UUID == source.PreparationUnitID`.
- Responses: change `ChargeAdjustmentResponse.PreparationUnitID` in `internal/sales/dto.go` to `*uuid.UUID` with tag `json:"preparation_unit_id"`, and fill it with `nullUUIDPtr(row.PreparationUnitID)` in `loadCheckAdjustments` (`projection.go`) and in every other constructor the compiler names.
- Tests: `completed_sale_integration_test.go:266` becomes `require.NotNil(t, entry.Adjustment.PreparationUnitID)`.

Re-run until clean.

- [ ] **Step 6: Run the schema test and the existing suites**

Run: `go test -count=1 -tags=integration ./internal/sales/ ./internal/preparation/ ./internal/shift/`
Expected: PASS, including `TestCheckoutRecoverySchema`.

- [ ] **Step 7: Commit**

```bash
git add internal/database/migrations/000017_add_checkout_recovery.sql internal/database/sqlc internal/sales internal/preparation
git commit -m "feat(sales): schema for checkout recovery (Phase 08)"
```

---

### Task 2: Derive Awaiting Submission and block Session closure on it

**Files:**
- Modify: `internal/sales/closure.go`, `internal/sales/dto.go`, `internal/sales/projection.go`, `internal/sales/errors.go`, `sql/queries/sales.sql`
- Test: `internal/sales/closure_test.go`, `internal/sales/errors_test.go`, create `internal/sales/checkout_recovery_integration_test.go`

**Interfaces:**
- Consumes: Task 1 constants.
- Produces: `func DeriveAwaitingSubmission(checks []CheckResponse) []uuid.UUID`; `ServiceSessionResponse.AwaitingSubmission bool`, `.AwaitingSubmissionCommittedItemIDs []uuid.UUID`, `.AbandonedCheckout *AbandonedCheckoutResponse`; `ChargeAllocationResponse.Withdrawn bool`; `ClosureReadiness.AwaitingSubmission bool`; `ErrAwaitingSubmissionForClosure`; sqlc `GetAbandonedCheckoutBySession`; test helper `newRecoveryEnv`, `paidTakeaway`.

- [ ] **Step 1: Write the failing unit tests**

Append to `internal/sales/closure_test.go`:

```go
func TestDeriveAwaitingSubmission(t *testing.T) {
	paidItem, unpaidItem, withdrawnItem, submittedItem := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	checks := []sales.CheckResponse{
		{EffectiveReceivedVND: 50000, Allocations: []sales.ChargeAllocationResponse{
			{CommittedItemID: paidItem},
			{CommittedItemID: paidItem}, // split across two allocations: reported once
			{CommittedItemID: withdrawnItem, Withdrawn: true},
			{CommittedItemID: submittedItem, Submitted: true},
		}},
		{EffectiveReceivedVND: 0, Allocations: []sales.ChargeAllocationResponse{
			{CommittedItemID: unpaidItem},
		}},
	}
	require.Equal(t, []uuid.UUID{paidItem}, sales.DeriveAwaitingSubmission(checks))
	require.Empty(t, sales.DeriveAwaitingSubmission(nil))
}

func TestEvaluateClosureReadinessAwaitingSubmissionComesFirst(t *testing.T) {
	s := eligible()
	s.Checks[0].State = sales.CheckStateOpen // also unsettled
	s.AwaitingSubmission = true
	got := sales.EvaluateClosureReadiness(s)
	require.False(t, got.Eligible)
	require.ErrorIs(t, got.Err(), sales.ErrAwaitingSubmissionForClosure)
}
```

Add to the table in `TestMapHTTPErrorCodes` (`errors_test.go`):

```go
		{"awaiting submission for closure", ErrAwaitingSubmissionForClosure, http.StatusConflict, "AWAITING_SUBMISSION_FOR_CLOSURE"},
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/sales/ -run 'TestDeriveAwaitingSubmission|TestEvaluateClosureReadiness|TestMapHTTPErrorCodes'`
Expected: compile failure, undefined `DeriveAwaitingSubmission`, `AwaitingSubmission`, `Withdrawn`, `ErrAwaitingSubmissionForClosure`.

- [ ] **Step 3: Implement the DTO fields**

In `internal/sales/dto.go`, add to `ServiceSessionResponse` after `PreparationUnits`:

```go
	// Phase 08: paid Committed Items that have not entered an Order (spec §4),
	// derived, never stored.
	AwaitingSubmission                 bool        `json:"awaiting_submission"`
	AwaitingSubmissionCommittedItemIDs []uuid.UUID `json:"awaiting_submission_committed_item_ids"`
	// Phase 08: the terminal record, present only on an ABANDONED Session.
	AbandonedCheckout *AbandonedCheckoutResponse `json:"abandoned_checkout"`
```

Add to `ChargeAllocationResponse` after `Submitted`:

```go
	// Withdrawn is true once a WITHDRAWAL Charge Adjustment removed this
	// allocation's charge (Phase 08).
	Withdrawn bool `json:"withdrawn"`
```

Add the type:

```go
// AbandonedCheckoutResponse is the terminal record of an Abandoned Checkout.
type AbandonedCheckoutResponse struct {
	ID                   uuid.UUID `json:"id"`
	Reason               string    `json:"reason"`
	Note                 *string   `json:"note"`
	ActorStaffIdentityID uuid.UUID `json:"actor_staff_identity_id"`
	OccurredAt           time.Time `json:"occurred_at"`
}
```

In `newServiceSessionResponse` (`projection.go`) initialize `AwaitingSubmissionCommittedItemIDs: make([]uuid.UUID, 0)`.

- [ ] **Step 4: Implement derivation and closure precedence**

In `internal/sales/closure.go`, add:

```go
// DeriveAwaitingSubmission returns the Committed Items whose charge sits on a
// Check holding net money but that are neither submitted nor withdrawn, each
// once, in projection order. A non-empty result is the Awaiting Submission
// state (spec §4): Payment committed, Submit did not.
func DeriveAwaitingSubmission(checks []CheckResponse) []uuid.UUID {
	out := make([]uuid.UUID, 0)
	seen := make(map[uuid.UUID]bool)
	for _, check := range checks {
		if check.EffectiveReceivedVND <= 0 {
			continue
		}
		for _, allocation := range check.Allocations {
			if allocation.Submitted || allocation.Withdrawn || seen[allocation.CommittedItemID] {
				continue
			}
			seen[allocation.CommittedItemID] = true
			out = append(out, allocation.CommittedItemID)
		}
	}
	return out
}
```

Add `AwaitingSubmission bool` as the first field of `ClosureReadiness`. In `EvaluateClosureReadiness` set `out.AwaitingSubmission = session.AwaitingSubmission` and prepend `!out.AwaitingSubmission &&` to the `out.Eligible` expression. In `Err()`, make the first case:

```go
	case r.AwaitingSubmission:
		return ErrAwaitingSubmissionForClosure
```

and update the `Err` doc comment's order sentence to begin "paid work the bar never received, then unsettled money, …".

In `internal/sales/errors.go`, next to `ErrPendingRefundForClosure`:

```go
	ErrAwaitingSubmissionForClosure = errors.New("paid committed items must be submitted or cancelled before the service session closes")
```

and in `MapHTTPError` before the `ErrPendingRefundForClosure` case:

```go
	case errors.Is(err, ErrAwaitingSubmissionForClosure):
		return coded(http.StatusConflict, "AWAITING_SUBMISSION_FOR_CLOSURE", ErrAwaitingSubmissionForClosure)
```

- [ ] **Step 5: Fill the projection**

Append to `sql/queries/sales.sql`:

```sql
-- name: GetAbandonedCheckoutBySession :one
-- Phase 08: the terminal record the Service Session projection reads.
SELECT id, reason, note, actor_staff_identity_id, occurred_at
FROM abandoned_checkouts
WHERE service_session_id = $1;
```

Run `make sqlc`.

In `loadCheckForSnapshot` (`projection.go`):

1. Exempt `ABANDONED` from the state guard. The condition becomes
   `row.State != CheckStateMerged && row.State != CheckStateAbandoned && (row.State == CheckStateSettled) != SettlesCheck(financials.BalanceVND)`.
2. After `loadCheckAdjustments`, mark withdrawn allocations:

```go
	withdrawn := make(map[uuid.UUID]bool)
	for _, adjustment := range adjustments {
		if adjustment.Kind == ChargeAdjustmentKindWithdrawal {
			withdrawn[adjustment.ChargeAllocationID] = true
		}
	}
	for i := range allocations {
		allocations[i].Withdrawn = withdrawn[allocations[i].ID]
	}
```

In `LoadServiceSession`, right after `out.Checks = checks`:

```go
	out.AwaitingSubmissionCommittedItemIDs = DeriveAwaitingSubmission(checks)
	out.AwaitingSubmission = len(out.AwaitingSubmissionCommittedItemIDs) > 0
```

and before the `GetEditableDraft` block (which returns early):

```go
	abandoned, err := q.GetAbandonedCheckoutBySession(ctx, sessionID)
	switch {
	case err == nil:
		out.AbandonedCheckout = &AbandonedCheckoutResponse{
			ID:                   abandoned.ID,
			Reason:               abandoned.Reason,
			Note:                 nullStringPtr(abandoned.Note),
			ActorStaffIdentityID: abandoned.ActorStaffIdentityID,
			OccurredAt:           abandoned.OccurredAt,
		}
	case !errors.Is(err, sql.ErrNoRows):
		return out, fmt.Errorf("load abandoned checkout: %w", err)
	}
```

- [ ] **Step 6: Run the unit tests**

Run: `go test ./internal/sales/`
Expected: PASS.

- [ ] **Step 7: Write the induced-failure and retry integration tests**

Create `internal/sales/checkout_recovery_integration_test.go`:

```go
//go:build integration

package sales_test

import (
	"context"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// recoveryEnv drives the Phase 08 commands over the shared Sales world.
type recoveryEnv struct {
	*salesEnv
	// managerCode is the env manager's login code, for inline Refund approval.
	managerCode string
}

func newRecoveryEnv(t *testing.T) *recoveryEnv {
	t.Helper()
	env := &recoveryEnv{salesEnv: newSalesEnv(t)}
	require.NoError(t, env.DB.QueryRow(
		`SELECT login_code FROM staff_identities WHERE id = $1`, env.Actor.StaffID).
		Scan(&env.managerCode))
	return env
}

// paidTakeaway commits one Coffee at quantity 2 (50,000 VND) and pays it in
// cash without submitting: the Phase 08 failure boundary (spec §3).
func (e *recoveryEnv) paidTakeaway(t *testing.T) (sales.ServiceSessionResponse, uuid.UUID) {
	t.Helper()
	session := e.commitTakeawayDraft(t, 1)
	checkID := e.soleCheckID(t, session.ID)
	_, _, err := e.payCash(t, checkID, 50000, 50000)
	require.NoError(t, err)
	return e.GetSessionOK(t, session.ID), checkID
}

func TestAwaitingSubmissionIsVisibleAndBlocksClosure(t *testing.T) {
	env := newRecoveryEnv(t)
	session, _ := env.paidTakeaway(t)

	require.True(t, session.AwaitingSubmission)
	require.Len(t, session.AwaitingSubmissionCommittedItemIDs, 1)

	_, _, err := env.TryClose(t, session.ID)
	require.ErrorIs(t, err, sales.ErrAwaitingSubmissionForClosure)
}

func TestAwaitingSubmissionRetryCreatesOneOrder(t *testing.T) {
	env := newRecoveryEnv(t)
	ctx := context.Background()
	session, _ := env.paidTakeaway(t)

	requestID := uuid.New()
	first, _, err := env.SubmitWithRequestID(t, requestID, session.ID)
	require.NoError(t, err)
	require.False(t, first.AwaitingSubmission)

	replay, _, err := env.SubmitWithRequestID(t, requestID, session.ID)
	require.NoError(t, err)
	require.Equal(t, first.Orders[0].ID, replay.Orders[0].ID)

	_, _, err = env.TrySubmit(t, session.ID)
	require.ErrorIs(t, err, sales.ErrNothingToSubmit)

	var orders, units int
	require.NoError(t, env.DB.QueryRowContext(ctx,
		`SELECT count(*) FROM orders WHERE service_session_id = $1`, session.ID).Scan(&orders))
	require.NoError(t, env.DB.QueryRowContext(ctx, `
		SELECT count(*) FROM preparation_units pu
		JOIN order_items oi ON oi.id = pu.order_item_id
		JOIN orders o ON o.id = oi.order_id
		WHERE o.service_session_id = $1`, session.ID).Scan(&units))
	require.Equal(t, 1, orders)
	require.Equal(t, 2, units, "quantity 2 is two Preparation Units")
}
```

- [ ] **Step 8: Run them**

Run: `go test -count=1 -tags=integration ./internal/sales/ -run 'TestAwaitingSubmission'`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add sql/queries/sales.sql internal/database/sqlc internal/sales
git commit -m "feat(sales): derive Awaiting Submission and block Session closure on it"
```

---

### Task 3: Cancel Awaiting Submission

**Files:**
- Create: `internal/sales/checkout_recovery.go`
- Modify: `internal/sales/domain.go`, `internal/sales/dto.go`, `internal/sales/errors.go`, `sql/queries/sales.sql`
- Test: `internal/sales/domain_test.go`, `internal/sales/errors_test.go`, `internal/sales/checkout_recovery_integration_test.go`

**Interfaces:**
- Consumes: Task 1 constants; Task 2 `LoadServiceSession` awaiting fields; existing `lockOpenSalesShift`, `checkBalance`, `SettlesCheck`, `writeSalesAudit`, `normalizeCorrectionNote`, `validateCorrectionNote`, `AddCharge`.
- Produces: `type CheckoutRecoveryCommand struct{ RequestID uuid.UUID; ServiceSessionID uuid.UUID; Reason string; Note *string }`; `ValidateCheckoutRecoveryCommand(cmd CheckoutRecoveryCommand, note *string) error`; `NewCancelAwaitingSubmissionHandler(*Runner) *CancelAwaitingSubmissionHandler` with `Handle(ctx, Actor, CheckoutRecoveryCommand) (int, ServiceSessionResponse, error)`; helper `lockRecoverableSession(ctx, q, sessionID) error`; `checkoutRecoveryFingerprint`; `ErrNothingAwaitingSubmission`, `ErrSessionHasOrder`; sqlc `SessionHasOrder`, `LockSessionChecksForRecovery`, `ListWithdrawableAllocations`, `MarkOrderDraftCancelled`; test helpers `cancel`, `cancelWithRequestID`.

- [ ] **Step 1: Write the failing unit tests**

Append to `internal/sales/domain_test.go` (package `sales_test`; it already imports `response`, `sales`, `uuid`, `require`):

```go
func TestValidateCheckoutRecoveryCommand(t *testing.T) {
	note := "khách đổi ý"
	valid := sales.CheckoutRecoveryCommand{RequestID: uuid.New(), Reason: sales.RecoveryReasonCustomerLeft}
	require.NoError(t, sales.ValidateCheckoutRecoveryCommand(valid, nil))

	other := valid
	other.Reason = sales.RecoveryReasonOther
	require.ErrorIs(t, sales.ValidateCheckoutRecoveryCommand(other, nil), response.ErrInvalid)
	require.NoError(t, sales.ValidateCheckoutRecoveryCommand(other, &note))

	unknown := valid
	unknown.Reason = "ITEM_UNAVAILABLE"
	require.ErrorIs(t, sales.ValidateCheckoutRecoveryCommand(unknown, nil), response.ErrInvalid)

	noID := valid
	noID.RequestID = uuid.Nil
	require.ErrorIs(t, sales.ValidateCheckoutRecoveryCommand(noID, nil), response.ErrInvalid)
}
```

Add to `TestMapHTTPErrorCodes`:

```go
		{"nothing awaiting submission", ErrNothingAwaitingSubmission, http.StatusConflict, "NOTHING_AWAITING_SUBMISSION"},
		{"session has order", ErrSessionHasOrder, http.StatusConflict, "SESSION_HAS_ORDER"},
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/sales/ -run 'TestValidateCheckoutRecoveryCommand|TestMapHTTPErrorCodes'`
Expected: compile failure, undefined `CheckoutRecoveryCommand`.

- [ ] **Step 3: Implement the command type, reasons, validation, and errors**

`internal/sales/dto.go`:

```go
// CheckoutRecoveryCommand is the body of both Phase 08 commands, Cancel
// Awaiting Submission and Abandon Checkout.
type CheckoutRecoveryCommand struct {
	RequestID        uuid.UUID `json:"request_id"`
	ServiceSessionID uuid.UUID `json:"-"`
	Reason           string    `json:"reason"`
	Note             *string   `json:"note"`
}
```

`internal/sales/domain.go`:

```go
// --- Phase 08: checkout recovery ---

// Idempotency action names (idempotency_keys.action is VARCHAR(50)).
const (
	OpCancelAwaitingSubmission = "sales.cancel_awaiting_submission"
	OpAbandonCheckout          = "sales.abandon_checkout"
)

// Phase 08 audit event types (spec §11).
const (
	EventAwaitingSubmissionCancelled = "AWAITING_SUBMISSION_CANCELLED"
	EventCheckoutAbandoned           = "CHECKOUT_ABANDONED"
)

// Abandoned Checkout reason catalog (domain-rationale 08), shared by Cancel
// Awaiting Submission. migration 000017 enforces the same set.
const (
	RecoveryReasonCustomerLeft    = "CUSTOMER_LEFT"
	RecoveryReasonCustomerRequest = "CUSTOMER_REQUEST"
	RecoveryReasonSystemFailure   = "SYSTEM_FAILURE"
	RecoveryReasonOther           = "OTHER"
)

// ValidateCheckoutRecoveryCommand checks an already-normalized command before
// the mutation claims its idempotency key.
func ValidateCheckoutRecoveryCommand(cmd CheckoutRecoveryCommand, note *string) error {
	if cmd.RequestID == uuid.Nil {
		return fmt.Errorf("%w: request_id is required", response.ErrInvalid)
	}
	switch cmd.Reason {
	case RecoveryReasonCustomerLeft, RecoveryReasonCustomerRequest,
		RecoveryReasonSystemFailure, RecoveryReasonOther:
	default:
		return fmt.Errorf("%w: %q is not a valid checkout recovery reason",
			response.ErrInvalid, cmd.Reason)
	}
	return validateCorrectionNote(cmd.Reason, RecoveryReasonOther, note)
}
```

`internal/sales/errors.go` sentinels and mapping:

```go
	ErrNothingAwaitingSubmission = errors.New("the service session is not awaiting submission")
	ErrSessionHasOrder           = errors.New("the service session already has a submitted order")
```

```go
	case errors.Is(err, ErrNothingAwaitingSubmission):
		return coded(http.StatusConflict, "NOTHING_AWAITING_SUBMISSION", ErrNothingAwaitingSubmission)
	case errors.Is(err, ErrSessionHasOrder):
		return coded(http.StatusConflict, "SESSION_HAS_ORDER", ErrSessionHasOrder)
```

Run: `go test ./internal/sales/ -run 'TestValidateCheckoutRecoveryCommand|TestMapHTTPErrorCodes'` → PASS.

- [ ] **Step 4: Write the failing integration tests**

Append to `checkout_recovery_integration_test.go`:

```go
func (e *recoveryEnv) cancelWithRequestID(t *testing.T, requestID, sessionID uuid.UUID,
	reason string, note *string,
) (sales.ServiceSessionResponse, int, error) {
	t.Helper()
	status, resp, err := sales.NewCancelAwaitingSubmissionHandler(e.Runner).
		Handle(context.Background(), e.Actor, sales.CheckoutRecoveryCommand{
			RequestID: requestID, ServiceSessionID: sessionID, Reason: reason, Note: note,
		})
	if err != nil {
		status, err = mapErrorStatus(err)
	}
	return resp, status, err
}

func (e *recoveryEnv) cancel(t *testing.T, sessionID uuid.UUID) (
	sales.ServiceSessionResponse, int, error,
) {
	t.Helper()
	return e.cancelWithRequestID(t, uuid.New(), sessionID, sales.RecoveryReasonCustomerLeft, nil)
}

func TestCancelAwaitingSubmissionWithdrawsTheCharge(t *testing.T) {
	env := newRecoveryEnv(t)
	session, checkID := env.paidTakeaway(t)

	got, status, err := env.cancel(t, session.ID)
	require.NoError(t, err)
	require.Equal(t, 200, status)

	require.False(t, got.AwaitingSubmission)
	check := env.findCheck(t, got, checkID)
	require.Equal(t, int64(0), check.ChargeVND)
	require.Equal(t, int64(50000), check.PendingRefundVND)
	require.Equal(t, sales.CheckStateSettled, check.State)
	require.Len(t, check.ChargeAdjustments, 1)
	require.Equal(t, sales.ChargeAdjustmentKindWithdrawal, check.ChargeAdjustments[0].Kind)
	require.Nil(t, check.ChargeAdjustments[0].PreparationUnitID)
	require.True(t, check.Allocations[0].Withdrawn)
	env.RequireDraftState(t, session.ID, sales.DraftStateCancelled)
	require.Equal(t, 1, env.countAuditEvents(t, sales.EventAwaitingSubmissionCancelled))

	_, _, err = env.TrySubmit(t, session.ID)
	require.ErrorIs(t, err, sales.ErrNothingToSubmit, "a cancelled draft is never submitted")
}

func TestCancelAwaitingSubmissionSettlesAPartiallyPaidCheck(t *testing.T) {
	env := newRecoveryEnv(t)
	session := env.commitDineInDraft(t, 1) // 50,000 VND, dine-in allows partial payment
	checkID := env.soleCheckID(t, session.ID)
	_, _, err := env.payCash(t, checkID, 20000, 20000)
	require.NoError(t, err)

	got, _, err := env.cancel(t, session.ID)
	require.NoError(t, err)
	check := env.findCheck(t, got, checkID)
	require.Equal(t, sales.CheckStateSettled, check.State)
	require.Equal(t, int64(20000), check.PendingRefundVND)
	require.Equal(t, 1, env.countAuditEventsForCheck(t, sales.EventCheckSettled, checkID))
}

func TestCancelAwaitingSubmissionRejections(t *testing.T) {
	env := newRecoveryEnv(t)

	unpaid := env.commitTakeawayDraft(t, 1)
	_, _, err := env.cancel(t, unpaid.ID)
	require.ErrorIs(t, err, sales.ErrNothingAwaitingSubmission)

	submitted, _ := env.paidTakeaway(t)
	env.Submit(t, submitted.ID)
	_, _, err = env.cancel(t, submitted.ID)
	require.ErrorIs(t, err, sales.ErrSessionHasOrder)

	_, _, err = env.cancel(t, uuid.New())
	require.ErrorIs(t, err, sales.ErrServiceSessionNotFound)

	paid, _ := env.paidTakeaway(t)
	_, status, err := env.cancelWithRequestID(t, uuid.New(), paid.ID, sales.RecoveryReasonOther, nil)
	require.Error(t, err)
	require.Equal(t, 400, status)
}

func TestCancelAwaitingSubmissionReplays(t *testing.T) {
	env := newRecoveryEnv(t)
	session, _ := env.paidTakeaway(t)

	requestID := uuid.New()
	first, _, err := env.cancelWithRequestID(t, requestID, session.ID, sales.RecoveryReasonCustomerLeft, nil)
	require.NoError(t, err)
	replay, _, err := env.cancelWithRequestID(t, requestID, session.ID, sales.RecoveryReasonCustomerLeft, nil)
	require.NoError(t, err)
	require.Equal(t, first.Checks[0].ChargeAdjustments[0].ID, replay.Checks[0].ChargeAdjustments[0].ID)

	_, _, err = env.cancel(t, session.ID)
	require.ErrorIs(t, err, sales.ErrNothingAwaitingSubmission)
	require.Equal(t, 1, env.countAuditEvents(t, sales.EventAwaitingSubmissionCancelled))
}
```

Run: `go test -count=1 -tags=integration ./internal/sales/ -run TestCancelAwaitingSubmission`
Expected: compile failure, undefined `NewCancelAwaitingSubmissionHandler`.

- [ ] **Step 5: Add the queries**

Append to `sql/queries/sales.sql`:

```sql
-- name: SessionHasOrder :one
-- Phase 08: recovery applies only to a Session with no Order (ADR-066).
SELECT EXISTS (SELECT 1 FROM orders WHERE service_session_id = $1) AS has_order;

-- name: LockSessionChecksForRecovery :many
-- Phase 08: every Check of one Session, after the caller holds the Session
-- lock, in the ascending (created_at, id) order Submit and 5C use. Like
-- Submit, this runs Session-then-Checks against Payment's Check-then-Session,
-- so it inherits ADR-031's AB-BA window (ADR-066).
SELECT id, state, charge_vnd
FROM checks
WHERE service_session_id = $1
ORDER BY created_at ASC, id ASC
FOR UPDATE;

-- name: ListWithdrawableAllocations :many
-- Phase 08: the committed draft's Charge Allocations with their frozen charge,
-- computed the way GetGlobalShiftClosureBlockers computes base charge.
SELECT ca.id, ca.check_id,
       (ca.quantity::BIGINT * ci.unit_price_vnd)::BIGINT AS amount_vnd
FROM charge_allocations AS ca
JOIN committed_items AS ci ON ci.id = ca.committed_item_id
WHERE ci.order_draft_id = $1
ORDER BY ca.check_id, ca.id;

-- name: MarkOrderDraftCancelled :exec
UPDATE order_drafts SET state = 'CANCELLED' WHERE id = $1;
```

Run: `make sqlc`.

- [ ] **Step 6: Implement the handler**

Create `internal/sales/checkout_recovery.go`:

```go
package sales

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

// checkoutRecoveryFingerprint is the idempotency input of both Phase 08
// commands. The note is the normalized one, so differently padded replays
// match.
type checkoutRecoveryFingerprint struct {
	ServiceSessionID uuid.UUID `json:"service_session_id"`
	Reason           string    `json:"reason"`
	Note             *string   `json:"note"`
}

// lockOpenShiftForRecovery takes the open Sales Shift FOR SHARE first, as
// every money command does, and requires one: both commands write rows the
// Shift reconciles.
func lockOpenShiftForRecovery(ctx context.Context, q *sqlc.Queries) (uuid.UUID, error) {
	shiftID, err := lockOpenSalesShift(ctx, q)
	if err != nil {
		return uuid.Nil, err
	}
	if shiftID == uuid.Nil {
		return uuid.Nil, fmt.Errorf("%w: no open sales shift", ErrOpenShiftRequired)
	}
	return shiftID, nil
}

// lockRecoverableSession locks the Session and applies the preconditions both
// commands share: it exists, is ACTIVE, and has no Order (ADR-066).
func lockRecoverableSession(ctx context.Context, q *sqlc.Queries, sessionID uuid.UUID) error {
	session, err := q.LockServiceSessionForSubmission(ctx, sessionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: %s", ErrServiceSessionNotFound, sessionID)
		}
		return fmt.Errorf("lock service session: %w", err)
	}
	if session.State != StateActive {
		return fmt.Errorf("%w: %s", ErrServiceSessionClosed, sessionID)
	}
	hasOrder, err := q.SessionHasOrder(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("check session order: %w", err)
	}
	if hasOrder {
		return fmt.Errorf("%w: %s", ErrSessionHasOrder, sessionID)
	}
	return nil
}

// CancelAwaitingSubmissionHandler withdraws the charges of paid, unsubmitted
// Committed Items so the existing Refund can return the money (spec §5.1).
type CancelAwaitingSubmissionHandler struct{ runner *Runner }

// NewCancelAwaitingSubmissionHandler creates a CancelAwaitingSubmissionHandler.
func NewCancelAwaitingSubmissionHandler(runner *Runner) *CancelAwaitingSubmissionHandler {
	return &CancelAwaitingSubmissionHandler{runner: runner}
}

// Handle executes Cancel Awaiting Submission.
func (h *CancelAwaitingSubmissionHandler) Handle(ctx context.Context, actor Actor,
	cmd CheckoutRecoveryCommand,
) (int, ServiceSessionResponse, error) {
	note := normalizeCorrectionNote(cmd.Note)
	if err := ValidateCheckoutRecoveryCommand(cmd, note); err != nil {
		return 0, ServiceSessionResponse{}, err
	}
	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpCancelAwaitingSubmission,
		Fingerprint: checkoutRecoveryFingerprint{cmd.ServiceSessionID, cmd.Reason, note},
		Required:    []string{CapSalesOperate},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, ServiceSessionResponse, AuditRecord, error) {
			var zero ServiceSessionResponse
			q := mc.Queries

			shiftID, err := lockOpenShiftForRecovery(ctx, q)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			if err := lockRecoverableSession(ctx, q, cmd.ServiceSessionID); err != nil {
				return 0, zero, AuditRecord{}, err
			}
			draftID, err := q.LockSubmittableDraft(ctx, cmd.ServiceSessionID)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return 0, zero, AuditRecord{}, fmt.Errorf(
						"%w: %s", ErrNothingAwaitingSubmission, cmd.ServiceSessionID)
				}
				return 0, zero, AuditRecord{}, fmt.Errorf("lock submittable draft: %w", err)
			}
			checks, err := q.LockSessionChecksForRecovery(ctx, cmd.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("lock session checks: %w", err)
			}

			// Awaiting Submission is judged on the locked facts, through the
			// same derivation the projection uses, so the two cannot disagree.
			before, err := LoadServiceSession(ctx, q, cmd.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			if !before.AwaitingSubmission {
				return 0, zero, AuditRecord{}, fmt.Errorf(
					"%w: %s", ErrNothingAwaitingSubmission, cmd.ServiceSessionID)
			}

			occurredAt, err := q.GetSalesOccurredAt(ctx)
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("read occurrence time: %w", err)
			}
			allocations, err := q.ListWithdrawableAllocations(ctx, draftID)
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("list withdrawable allocations: %w", err)
			}

			removedByCheck := make(map[uuid.UUID]int64)
			adjustmentIDs := make([]uuid.UUID, 0, len(allocations))
			var withdrawnVND int64
			for _, allocation := range allocations {
				adjustment, err := q.InsertChargeAdjustment(ctx, sqlc.InsertChargeAdjustmentParams{
					Kind:               ChargeAdjustmentKindWithdrawal,
					Scope:              CompScopeLiveCheck,
					ChargeAllocationID: allocation.ID,
					CheckID:            allocation.CheckID,
					SalesShiftID:       shiftID,
					AmountVnd:          allocation.AmountVnd,
					CreatedAt:          occurredAt,
				})
				if err != nil {
					return 0, zero, AuditRecord{}, fmt.Errorf("insert withdrawal adjustment: %w", err)
				}
				adjustmentIDs = append(adjustmentIDs, adjustment.ID)
				checkTotal, err := AddCharge(removedByCheck[allocation.CheckID], allocation.AmountVnd)
				if err != nil {
					return 0, zero, AuditRecord{}, err
				}
				removedByCheck[allocation.CheckID] = checkTotal
				if withdrawnVND, err = AddCharge(withdrawnVND, allocation.AmountVnd); err != nil {
					return 0, zero, AuditRecord{}, err
				}
			}

			for _, check := range checks {
				removed, ok := removedByCheck[check.ID]
				if !ok {
					continue
				}
				if removed > check.ChargeVnd {
					return 0, zero, AuditRecord{}, fmt.Errorf(
						"%w: withdrawal %d exceeds stored charge %d of check %s",
						ErrChargeInvariantViolated, removed, check.ChargeVnd, check.ID)
				}
				newChargeVND := check.ChargeVnd - removed
				if err := q.UpdateAdjustedCheckCharge(ctx, sqlc.UpdateAdjustedCheckChargeParams{
					ChargeVnd: newChargeVND, ID: check.ID,
				}); err != nil {
					return 0, zero, AuditRecord{}, fmt.Errorf("update adjusted check charge: %w", err)
				}
				if check.State != CheckStateOpen {
					continue
				}
				balanceVND, err := checkBalance(ctx, q, check.ID, newChargeVND)
				if err != nil {
					return 0, zero, AuditRecord{}, err
				}
				if !SettlesCheck(balanceVND) {
					continue
				}
				if err := q.SettleCheck(ctx, sqlc.SettleCheckParams{
					ID:                          check.ID,
					SettledAt:                   sql.NullTime{Time: occurredAt, Valid: true},
					SettledByStaffIdentityID:    uuid.NullUUID{UUID: actor.StaffID, Valid: true},
					SettledDuringSalesShiftID:   uuid.NullUUID{UUID: shiftID, Valid: true},
					SettledStaffAccessSessionID: uuid.NullUUID{UUID: actor.SessionID, Valid: true},
				}); err != nil {
					return 0, zero, AuditRecord{}, fmt.Errorf("settle withdrawn check: %w", err)
				}
				if err := writeSalesAudit(ctx, q, actor, occurredAt, EventCheckSettled,
					map[string]any{
						"check_id":          check.ID,
						"sales_shift_id":    shiftID,
						"charge_before_vnd": check.ChargeVnd,
						"charge_after_vnd":  newChargeVND,
					}); err != nil {
					return 0, zero, AuditRecord{}, err
				}
			}

			if err := q.MarkOrderDraftCancelled(ctx, draftID); err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("mark draft cancelled: %w", err)
			}

			out, err := LoadServiceSession(ctx, q, cmd.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			return http.StatusOK, out, AuditRecord{
				EventType: EventAwaitingSubmissionCancelled,
				Details: map[string]any{
					"service_session_id":    cmd.ServiceSessionID,
					"order_draft_id":        draftID,
					"charge_adjustment_ids": adjustmentIDs,
					"withdrawn_vnd":         withdrawnVND,
					"reason":                cmd.Reason,
					"note":                  note,
				},
			}, nil
		})
}
```

- [ ] **Step 7: Run the tests**

Run: `go test -count=1 -tags=integration ./internal/sales/ -run 'TestCancelAwaitingSubmission|TestAwaitingSubmission'`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add sql/queries/sales.sql internal/database/sqlc internal/sales
git commit -m "feat(sales): Cancel Awaiting Submission withdraws unsubmitted charges"
```

---

### Task 4: Abandon Checkout

**Files:**
- Modify: `internal/sales/checkout_recovery.go`, `internal/sales/session_close.go`, `internal/sales/errors.go`, `sql/queries/sales.sql`
- Test: `internal/sales/errors_test.go`, `internal/sales/checkout_recovery_integration_test.go`

**Interfaces:**
- Consumes: Task 3 `lockOpenShiftForRecovery`, `lockRecoverableSession`, `checkoutRecoveryFingerprint`, `LockSessionChecksForRecovery`, test helper `cancel`.
- Produces: `NewAbandonCheckoutHandler(*Runner) *AbandonCheckoutHandler` with `Handle(ctx, Actor, CheckoutRecoveryCommand) (int, ServiceSessionResponse, error)`; `releaseHeldTableAssignments(ctx, q, actor, sessionID, at) ([]uuid.UUID, error)`; `ErrPaymentRequiresRefund`; sqlc `GetSessionHeldMoney`, `InsertAbandonedCheckout`, `CancelSessionDrafts`, `AbandonSessionChecks`, `AbandonServiceSession`; test helpers `abandon`, `abandonWithRequestID`, `refundWithdrawal`.

- [ ] **Step 1: Write the failing tests**

Add to `TestMapHTTPErrorCodes`:

```go
		{"payment requires refund", ErrPaymentRequiresRefund, http.StatusConflict, "PAYMENT_REQUIRES_REFUND"},
```

Append to `checkout_recovery_integration_test.go`:

```go
func (e *recoveryEnv) abandonWithRequestID(t *testing.T, requestID, sessionID uuid.UUID,
	reason string, note *string,
) (sales.ServiceSessionResponse, int, error) {
	t.Helper()
	status, resp, err := sales.NewAbandonCheckoutHandler(e.Runner).
		Handle(context.Background(), e.Actor, sales.CheckoutRecoveryCommand{
			RequestID: requestID, ServiceSessionID: sessionID, Reason: reason, Note: note,
		})
	if err != nil {
		status, err = mapErrorStatus(err)
	}
	return resp, status, err
}

func (e *recoveryEnv) abandon(t *testing.T, sessionID uuid.UUID) (
	sales.ServiceSessionResponse, int, error,
) {
	t.Helper()
	return e.abandonWithRequestID(t, uuid.New(), sessionID, sales.RecoveryReasonCustomerLeft, nil)
}

// refundWithdrawal refunds the Check's whole withdrawn charge against its
// single Payment, through the unchanged Refund command.
func (e *recoveryEnv) refundWithdrawal(t *testing.T, sessionID, checkID uuid.UUID,
	method string,
) sales.RefundResult {
	t.Helper()
	check := e.findCheck(t, e.GetSessionOK(t, sessionID), checkID)
	require.Len(t, check.ChargeAdjustments, 1)
	require.Len(t, check.Payments, 1)
	amount := check.ChargeAdjustments[0].AmountVND
	status, result, err := sales.NewRecordRefundHandler(e.Runner).
		Handle(context.Background(), e.Actor, sales.RecordRefundCommand{
			RequestID: uuid.New(),
			CheckID:   checkID,
			Method:    method,
			AdjustmentAllocations: []sales.RefundAdjustmentAllocationInput{
				{ChargeAdjustmentID: check.ChargeAdjustments[0].ID, AmountVND: amount},
			},
			PaymentAllocations: []sales.RefundPaymentAllocationInput{
				{PaymentID: check.Payments[0].ID, AmountVND: amount},
			},
			Reason: sales.RefundReasonCustomerRequest,
			ManagerApproval: sales.ManagerApprovalInput{
				ApproverLoginCode: e.managerCode, ManagerPIN: "1234",
			},
		})
	require.NoError(t, err)
	require.Equal(t, 201, status)
	return result
}

func TestAbandonUnpaidDineInReleasesTheTable(t *testing.T) {
	env := newRecoveryEnv(t)
	ctx := context.Background()
	session := env.commitDineInDraft(t, 1)

	got, status, err := env.abandon(t, session.ID)
	require.NoError(t, err)
	require.Equal(t, 200, status)
	require.Equal(t, sales.StateAbandoned, got.State)
	require.NotNil(t, got.AbandonedCheckout)
	require.Equal(t, sales.RecoveryReasonCustomerLeft, got.AbandonedCheckout.Reason)
	for _, check := range got.Checks {
		require.Equal(t, sales.CheckStateAbandoned, check.State)
	}

	var held int
	require.NoError(t, env.DB.QueryRowContext(ctx, `
		SELECT count(*) FROM table_assignments
		WHERE service_session_id = $1 AND released_at IS NULL`, session.ID).Scan(&held))
	require.Equal(t, 0, held)

	var orders, completed int
	require.NoError(t, env.DB.QueryRowContext(ctx,
		`SELECT count(*) FROM orders WHERE service_session_id = $1`, session.ID).Scan(&orders))
	require.NoError(t, env.DB.QueryRowContext(ctx,
		`SELECT count(*) FROM completed_sales WHERE service_session_id = $1`, session.ID).Scan(&completed))
	require.Zero(t, orders)
	require.Zero(t, completed)
	require.Equal(t, 1, env.countAuditEvents(t, sales.EventCheckoutAbandoned))
	require.Equal(t, 1, env.countAuditEvents(t, sales.EventTableAssignmentReleased))
}

func TestAbandonEmptySession(t *testing.T) {
	env := newRecoveryEnv(t)
	session := env.StartTakeaway(t)

	got, _, err := env.abandon(t, session.ID)
	require.NoError(t, err)
	require.Equal(t, sales.StateAbandoned, got.State)
	require.Nil(t, got.Draft)
}

func TestAbandonRejections(t *testing.T) {
	env := newRecoveryEnv(t)

	paid, _ := env.paidTakeaway(t)
	_, _, err := env.abandon(t, paid.ID)
	require.ErrorIs(t, err, sales.ErrPaymentRequiresRefund)

	withOrder, _ := env.paidTakeaway(t)
	env.Submit(t, withOrder.ID)
	_, _, err = env.abandon(t, withOrder.ID)
	require.ErrorIs(t, err, sales.ErrSessionHasOrder)

	cancelledNotRefunded, _ := env.paidTakeaway(t)
	_, _, err = env.cancel(t, cancelledNotRefunded.ID)
	require.NoError(t, err)
	_, _, err = env.abandon(t, cancelledNotRefunded.ID)
	require.ErrorIs(t, err, sales.ErrPaymentRequiresRefund)

	note := "  "
	unpaid := env.commitTakeawayDraft(t, 1)
	_, status, err := env.abandonWithRequestID(t, uuid.New(), unpaid.ID, sales.RecoveryReasonOther, &note)
	require.Error(t, err)
	require.Equal(t, 400, status, "a blank note normalizes to none, and OTHER requires one")
}

func TestAbandonReplaysAndThenRefusesANewRequest(t *testing.T) {
	env := newRecoveryEnv(t)
	session := env.commitTakeawayDraft(t, 1)

	requestID := uuid.New()
	first, _, err := env.abandonWithRequestID(t, requestID, session.ID, sales.RecoveryReasonCustomerLeft, nil)
	require.NoError(t, err)
	replay, _, err := env.abandonWithRequestID(t, requestID, session.ID, sales.RecoveryReasonCustomerLeft, nil)
	require.NoError(t, err)
	require.Equal(t, first.AbandonedCheckout.ID, replay.AbandonedCheckout.ID)

	_, _, err = env.abandon(t, session.ID)
	require.ErrorIs(t, err, sales.ErrServiceSessionClosed)
}

func TestCancelRefundAbandonCash(t *testing.T) {
	env := newRecoveryEnv(t)
	ctx := context.Background()
	session, checkID := env.paidTakeaway(t)

	_, _, err := env.cancel(t, session.ID)
	require.NoError(t, err)
	env.refundWithdrawal(t, session.ID, checkID, sales.RefundMethodCash)

	got, _, err := env.abandon(t, session.ID)
	require.NoError(t, err)
	require.Equal(t, sales.StateAbandoned, got.State)

	var netCash int64
	require.NoError(t, env.DB.QueryRowContext(ctx, `
		SELECT COALESCE((SELECT SUM(applied_amount_vnd) FROM payments WHERE sales_shift_id = $1), 0)
		     - COALESCE((SELECT SUM(r.amount_vnd) FROM refunds r
		                 JOIN refund_completions rc ON rc.refund_id = r.id
		                 WHERE r.sales_shift_id = $1), 0)`, env.ShiftID).Scan(&netCash))
	require.Zero(t, netCash, "the cash that came in went back out")
}

func TestCancelRefundAbandonManualQR(t *testing.T) {
	env := newRecoveryEnv(t)
	session := env.commitTakeawayDraft(t, 1)
	checkID := env.soleCheckID(t, session.ID)
	_, _, err := env.payManualQR(t, checkID, 50000, true, nil)
	require.NoError(t, err)

	_, _, err = env.cancel(t, session.ID)
	require.NoError(t, err)
	result := env.refundWithdrawal(t, session.ID, checkID, sales.RefundMethodManualQR)
	require.Equal(t, sales.RefundStatePending, result.Refund.State)

	_, _, err = env.abandon(t, session.ID)
	require.ErrorIs(t, err, sales.ErrPaymentRequiresRefund, "a pending Refund has not moved money")

	_, _, err = sales.NewConfirmManualQRRefundHandler(env.Runner).
		Handle(context.Background(), env.Actor, sales.ConfirmManualQRRefundCommand{
			RequestID: uuid.New(), RefundID: result.Refund.ID,
		})
	require.NoError(t, err)

	got, _, err := env.abandon(t, session.ID)
	require.NoError(t, err)
	require.Equal(t, sales.StateAbandoned, got.State)
}
```

Run: `go test -count=1 -tags=integration ./internal/sales/ -run 'TestAbandon|TestCancelRefundAbandon'`
Expected: compile failure, undefined `NewAbandonCheckoutHandler`.

- [ ] **Step 2: Add the queries**

Append to `sql/queries/sales.sql`:

```sql
-- name: GetSessionHeldMoney :one
-- Phase 08: the money an Abandon must see returned. Valid Payments exclude
-- voided ones; a Refund counts only once completed; a pending Refund is
-- counted separately because it has not moved money.
SELECT
    COALESCE((SELECT SUM(p.applied_amount_vnd)
              FROM payments AS p
              JOIN checks AS c ON c.id = p.check_id
              WHERE c.service_session_id = sqlc.arg(service_session_id)::uuid
                AND NOT EXISTS (SELECT 1 FROM payment_voids AS pv
                                WHERE pv.payment_id = p.id)), 0)::BIGINT
        AS valid_payment_vnd,
    COALESCE((SELECT SUM(r.amount_vnd)
              FROM refunds AS r
              JOIN refund_completions AS rc ON rc.refund_id = r.id
              JOIN checks AS c ON c.id = r.check_id
              WHERE c.service_session_id = sqlc.arg(service_session_id)::uuid
                AND r.completed_sale_id IS NULL), 0)::BIGINT
        AS completed_refund_vnd,
    (SELECT count(*)
     FROM refunds AS r
     JOIN checks AS c ON c.id = r.check_id
     WHERE c.service_session_id = sqlc.arg(service_session_id)::uuid
       AND NOT EXISTS (SELECT 1 FROM refund_completions AS rc
                       WHERE rc.refund_id = r.id))::BIGINT
        AS pending_refund_count;

-- name: InsertAbandonedCheckout :one
INSERT INTO abandoned_checkouts (
    service_session_id, sales_shift_id, reason, note,
    actor_staff_identity_id, staff_access_session_id, occurred_at
) VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id;

-- name: CancelSessionDrafts :exec
-- Only a Session with no Order is abandoned, so every COMMITTED draft here is
-- unsubmitted.
UPDATE order_drafts SET state = 'CANCELLED'
WHERE service_session_id = $1 AND state IN ('EDITABLE', 'COMMITTED');

-- name: AbandonSessionChecks :many
-- MERGED Checks keep their state; they already carry no charge.
UPDATE checks SET state = 'ABANDONED'
WHERE service_session_id = $1 AND state IN ('OPEN', 'SETTLED')
RETURNING id;

-- name: AbandonServiceSession :exec
UPDATE service_sessions SET state = 'ABANDONED' WHERE id = $1;
```

Run: `make sqlc`.

- [ ] **Step 3: Extract table release from Session closure**

In `internal/sales/session_close.go`, move the body of the `for _, assignment := range assignments` loop (plus the `ListHeldTableAssignments` call before it) into:

```go
// releaseHeldTableAssignments releases every Table the Session holds, one
// TABLE_ASSIGNMENT_RELEASED Audit Event each, and returns the released Table
// ids. Service Session closure and Abandon Checkout share it.
func releaseHeldTableAssignments(ctx context.Context, q *sqlc.Queries, actor Actor,
	sessionID uuid.UUID, occurredAt time.Time,
) ([]uuid.UUID, error) {
	assignments, err := q.ListHeldTableAssignments(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list held table assignments: %w", err)
	}
	released := make([]uuid.UUID, 0, len(assignments))
	for _, assignment := range assignments {
		// (existing comment about released_at = now() stays here)
		if err := q.ReleaseTableAssignment(ctx, sqlc.ReleaseTableAssignmentParams{
			ID:                        assignment.ID,
			ReleasedByStaffIdentityID: uuid.NullUUID{UUID: actor.StaffID, Valid: true},
		}); err != nil {
			return nil, fmt.Errorf("release table assignment: %w", err)
		}
		details, err := json.Marshal(tableAssignmentAudit{
			TableAssignmentID: assignment.ID,
			TableID:           assignment.TableID,
			ServiceSessionID:  sessionID,
		})
		if err != nil {
			return nil, fmt.Errorf("marshal table assignment audit: %w", err)
		}
		if _, err := q.InsertAuditEvent(ctx, sqlc.InsertAuditEventParams{
			EventType:  EventTableAssignmentReleased,
			ActorID:    uuid.NullUUID{UUID: actor.StaffID, Valid: true},
			SessionID:  uuid.NullUUID{UUID: actor.SessionID, Valid: true},
			Details:    details,
			OccurredAt: occurredAt,
		}); err != nil {
			return nil, fmt.Errorf("insert %s audit event: %w", EventTableAssignmentReleased, err)
		}
		released = append(released, assignment.TableID)
	}
	return released, nil
}
```

In `CloseServiceSessionHandler.Handle`, replace the removed block with
`releasedTableIDs, err := releaseHeldTableAssignments(ctx, q, actor, cmd.ServiceSessionID, completedAt)` and delete the later loop that rebuilt `releasedTableIDs`.

Run: `go test -count=1 -tags=integration ./internal/sales/ -run 'Close'`
Expected: PASS (behavior unchanged).

- [ ] **Step 4: Implement the handler**

Add to `internal/sales/errors.go`:

```go
	ErrPaymentRequiresRefund = errors.New("every payment must be fully refunded before the checkout is abandoned")
```

```go
	case errors.Is(err, ErrPaymentRequiresRefund):
		return coded(http.StatusConflict, "PAYMENT_REQUIRES_REFUND", ErrPaymentRequiresRefund)
```

Append to `internal/sales/checkout_recovery.go`:

```go
// AbandonCheckoutHandler ends a Session that has no Order and holds no money
// as an Abandoned Checkout (spec §5.2). It creates no Order, Preparation Unit,
// or Completed Sale.
type AbandonCheckoutHandler struct{ runner *Runner }

// NewAbandonCheckoutHandler creates an AbandonCheckoutHandler.
func NewAbandonCheckoutHandler(runner *Runner) *AbandonCheckoutHandler {
	return &AbandonCheckoutHandler{runner: runner}
}

// Handle executes Abandon Checkout.
func (h *AbandonCheckoutHandler) Handle(ctx context.Context, actor Actor,
	cmd CheckoutRecoveryCommand,
) (int, ServiceSessionResponse, error) {
	note := normalizeCorrectionNote(cmd.Note)
	if err := ValidateCheckoutRecoveryCommand(cmd, note); err != nil {
		return 0, ServiceSessionResponse{}, err
	}
	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpAbandonCheckout,
		Fingerprint: checkoutRecoveryFingerprint{cmd.ServiceSessionID, cmd.Reason, note},
		Required:    []string{CapSalesOperate},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, ServiceSessionResponse, AuditRecord, error) {
			var zero ServiceSessionResponse
			q := mc.Queries

			shiftID, err := lockOpenShiftForRecovery(ctx, q)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			if err := lockRecoverableSession(ctx, q, cmd.ServiceSessionID); err != nil {
				return 0, zero, AuditRecord{}, err
			}
			// The Check locks make the money read below final: a Payment or
			// Refund that commits first is seen, and one that comes later
			// waits and then finds the Session ABANDONED.
			if _, err := q.LockSessionChecksForRecovery(ctx, cmd.ServiceSessionID); err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("lock session checks: %w", err)
			}
			money, err := q.GetSessionHeldMoney(ctx, cmd.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("read session money: %w", err)
			}
			if money.PendingRefundCount > 0 || money.ValidPaymentVnd != money.CompletedRefundVnd {
				return 0, zero, AuditRecord{}, fmt.Errorf(
					"%w: received %d, returned %d, %d refund(s) pending",
					ErrPaymentRequiresRefund, money.ValidPaymentVnd,
					money.CompletedRefundVnd, money.PendingRefundCount)
			}

			occurredAt, err := q.GetSalesOccurredAt(ctx)
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("read occurrence time: %w", err)
			}
			abandonedID, err := q.InsertAbandonedCheckout(ctx, sqlc.InsertAbandonedCheckoutParams{
				ServiceSessionID:     cmd.ServiceSessionID,
				SalesShiftID:         shiftID,
				Reason:               cmd.Reason,
				Note:                 nullString(note),
				ActorStaffIdentityID: actor.StaffID,
				StaffAccessSessionID: actor.SessionID,
				OccurredAt:           occurredAt,
			})
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("insert abandoned checkout: %w", err)
			}
			if err := q.CancelSessionDrafts(ctx, cmd.ServiceSessionID); err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("cancel session drafts: %w", err)
			}
			checkIDs, err := q.AbandonSessionChecks(ctx, cmd.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("abandon session checks: %w", err)
			}
			releasedTableIDs, err := releaseHeldTableAssignments(ctx, q, actor,
				cmd.ServiceSessionID, occurredAt)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			if err := q.AbandonServiceSession(ctx, cmd.ServiceSessionID); err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("abandon service session: %w", err)
			}

			out, err := LoadServiceSession(ctx, q, cmd.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			return http.StatusOK, out, AuditRecord{
				EventType: EventCheckoutAbandoned,
				Details: map[string]any{
					"service_session_id":    cmd.ServiceSessionID,
					"abandoned_checkout_id": abandonedID,
					"check_ids":             checkIDs,
					"released_table_ids":    releasedTableIDs,
					"reason":                cmd.Reason,
					"note":                  note,
				},
			}, nil
		})
}
```

If sqlc names a parameter differently (for example `Note` as `sql.NullString`, which is what `nullString` returns), match the generated struct in `internal/database/sqlc/sales.sql.go`.

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/sales/ && go test -count=1 -tags=integration ./internal/sales/`
Expected: PASS, the whole package.

- [ ] **Step 6: Commit**

```bash
git add sql/queries/sales.sql internal/database/sqlc internal/sales
git commit -m "feat(sales): Abandon Checkout ends a Session with no Order and no money held"
```

---

### Task 5: Shift closure blocker

**Files:**
- Modify: `sql/queries/shift.sql` (`GetGlobalShiftClosureBlockers`), `internal/shift/reconciliation.go:263-279`, `internal/shift/errors.go`
- Test: create `internal/shift/closure_blockers_test.go`; modify `internal/shift/errors_test.go`, `internal/sales/checkout_recovery_integration_test.go`

**Interfaces:**
- Consumes: Task 3 and 4 commands (for the integration assertion).
- Produces: `sqlc.GetGlobalShiftClosureBlockersRow.AwaitingSubmissionCount int64`; `func closureBlockerErr(b sqlc.GetGlobalShiftClosureBlockersRow) error`; `shift.ErrAwaitingSubmission`.

- [ ] **Step 1: Write the failing tests**

Create `internal/shift/closure_blockers_test.go`:

```go
package shift

import (
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/stretchr/testify/require"
)

// TestClosureBlockerPrecedence pins spec 8 as Phase 08 extends it: Awaiting
// Submission reports ahead of the generic active-Session blocker and behind
// every money blocker.
func TestClosureBlockerPrecedence(t *testing.T) {
	require.NoError(t, closureBlockerErr(sqlc.GetGlobalShiftClosureBlockersRow{}))

	awaiting := sqlc.GetGlobalShiftClosureBlockersRow{
		AwaitingSubmissionCount:   1,
		ActiveServiceSessionCount: 1,
	}
	require.ErrorIs(t, closureBlockerErr(awaiting), ErrAwaitingSubmission)

	awaiting.UnresolvedCorrectionVnd = 1
	require.ErrorIs(t, closureBlockerErr(awaiting), ErrUnresolvedCorrection)
}
```

Add to the table in `internal/shift/errors_test.go` next to the `ErrActiveServiceSession` row:

```go
		{shift.ErrAwaitingSubmission, http.StatusConflict, "SHIFT_AWAITING_SUBMISSION"},
```

Append to `internal/sales/checkout_recovery_integration_test.go`:

```go
func TestShiftBlockersTrackAwaitingSubmission(t *testing.T) {
	env := newRecoveryEnv(t)
	ctx := context.Background()
	session, checkID := env.paidTakeaway(t)

	blockers, err := env.Queries.GetGlobalShiftClosureBlockers(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1), blockers.AwaitingSubmissionCount)

	_, _, err = env.cancel(t, session.ID)
	require.NoError(t, err)
	blockers, err = env.Queries.GetGlobalShiftClosureBlockers(ctx)
	require.NoError(t, err)
	require.Zero(t, blockers.AwaitingSubmissionCount)
	require.Equal(t, int64(50000), blockers.UnresolvedCorrectionVnd,
		"withdrawn but not yet refunded money blocks as an unresolved correction")

	env.refundWithdrawal(t, session.ID, checkID, sales.RefundMethodCash)
	_, _, err = env.abandon(t, session.ID)
	require.NoError(t, err)

	blockers, err = env.Queries.GetGlobalShiftClosureBlockers(ctx)
	require.NoError(t, err)
	require.Zero(t, blockers.UnsettledCheckCount)
	require.Zero(t, blockers.PendingRefundCount)
	require.Zero(t, blockers.UnresolvedCorrectionVnd)
	require.Zero(t, blockers.AwaitingSubmissionCount)
	require.Zero(t, blockers.ActiveServiceSessionCount, "the Shift may now close")
}
```

Run: `go test ./internal/shift/ -run 'TestClosureBlockerPrecedence|TestMapHTTPError'`
Expected: compile failure, undefined `closureBlockerErr`, `AwaitingSubmissionCount`, `ErrAwaitingSubmission`.

- [ ] **Step 2: Extend the blocker query**

In `sql/queries/shift.sql`, `GetGlobalShiftClosureBlockers`, insert before the `active_service_session_count` column (keep the comma placement valid), and add one line to the query's header comment: "Phase 08 adds awaiting_submission_count: ACTIVE Sessions whose committed, orderless draft sits on a Check holding net money."

```sql
    (SELECT count(*)
     FROM order_drafts AS d
     JOIN service_sessions AS s ON s.id = d.service_session_id
     WHERE s.state = 'ACTIVE'
       AND d.state = 'COMMITTED'
       AND NOT EXISTS (SELECT 1 FROM orders AS o WHERE o.order_draft_id = d.id)
       AND EXISTS (
           SELECT 1
           FROM committed_items AS ci
           JOIN charge_allocations AS ca ON ca.committed_item_id = ci.id
           WHERE ci.order_draft_id = d.id
             AND COALESCE((SELECT SUM(p.applied_amount_vnd)
                           FROM payments AS p
                           WHERE p.check_id = ca.check_id
                             AND NOT EXISTS (SELECT 1 FROM payment_voids AS pv
                                             WHERE pv.payment_id = p.id)), 0)
               - COALESCE((SELECT SUM(r.amount_vnd)
                           FROM refunds AS r
                           JOIN refund_completions AS rc ON rc.refund_id = r.id
                           WHERE r.check_id = ca.check_id
                             AND r.completed_sale_id IS NULL), 0) > 0))::BIGINT
        AS awaiting_submission_count,
```

A Session has at most one committed, orderless draft (`FindBlockingDraft`), so counting drafts counts Sessions.

Run: `make sqlc`.

- [ ] **Step 3: Implement the precedence**

`internal/shift/errors.go`:

```go
	ErrAwaitingSubmission = errors.New("the shift has service sessions awaiting submission")
```

and in its mapper, before the `ErrActiveServiceSession` case:

```go
	case errors.Is(err, ErrAwaitingSubmission):
		return response.NewCodedError(http.StatusConflict, "SHIFT_AWAITING_SUBMISSION", ErrAwaitingSubmission.Error(), err)
```

`internal/shift/reconciliation.go`, replace `loadClosureBlockers`'s switch:

```go
func loadClosureBlockers(ctx context.Context, q *sqlc.Queries) error {
	blockers, err := q.GetGlobalShiftClosureBlockers(ctx)
	if err != nil {
		return fmt.Errorf("load global closure blockers: %w", err)
	}
	return closureBlockerErr(blockers)
}

// closureBlockerErr applies spec 8's load-bearing precedence. Phase 08 puts
// Awaiting Submission fourth: every such Session is also active, so its place
// changes only which error staff read, never whether closure is blocked.
func closureBlockerErr(b sqlc.GetGlobalShiftClosureBlockersRow) error {
	switch {
	case b.UnsettledCheckCount > 0:
		return ErrUnsettledCheck
	case b.PendingRefundCount > 0:
		return ErrPendingRefund
	case b.UnresolvedCorrectionVnd > 0:
		return ErrUnresolvedCorrection
	case b.AwaitingSubmissionCount > 0:
		return ErrAwaitingSubmission
	case b.ActiveServiceSessionCount > 0:
		return ErrActiveServiceSession
	}
	return nil
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/shift/ && go test -count=1 -tags=integration ./internal/shift/ ./internal/sales/ -run 'Blocker|Reconciliation|TestShiftBlockersTrackAwaitingSubmission'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add sql/queries/shift.sql internal/database/sqlc internal/shift internal/sales/checkout_recovery_integration_test.go
git commit -m "feat(shift): Awaiting Submission blocks Shift closure ahead of active Sessions"
```

---

### Task 6: HTTP routes, Swagger, and the web client

**Files:**
- Modify: `internal/sales/http.go`, `internal/sales/routes.go`, `internal/sales/routes_test.go`, `docs/swagger.*`, `docs/docs.go`, `web/src/api/generated/**`

**Interfaces:**
- Consumes: Task 3 and 4 handlers.
- Produces: `Slices.CancelAwaitingSubmission`, `Slices.AbandonCheckout`; the two routes.

- [ ] **Step 1: Write the failing route test**

In `internal/sales/routes_test.go`, add to the expected-route map:

```go
		"POST /api/v1/sales/service-sessions/:id/cancel-awaiting-submission":             true,
		"POST /api/v1/sales/service-sessions/:id/abandon":                                true,
```

Change all three `require.Len(t, routes, 26, …)` to `28` with message `"Phase 08 brings the Sales surface to twenty-eight operations"`, and add:

```go
func TestPhase08RoutesAreRegistered(t *testing.T) {
	routes := registeredSalesRoutes(t)
	require.Contains(t, routes, "POST /api/v1/sales/service-sessions/:id/cancel-awaiting-submission")
	require.Contains(t, routes, "POST /api/v1/sales/service-sessions/:id/abandon")
}
```

Run: `go test ./internal/sales/ -run Routes`
Expected: FAIL, 26 routes.

- [ ] **Step 2: Wire the handlers and routes**

`routes.go`: add fields `CancelAwaitingSubmission *CancelAwaitingSubmissionHandler` and `AbandonCheckout *AbandonCheckoutHandler` to `Slices`, construct them in `NewSlices`, and register after the `close` route:

```go
	v1.POST("/sales/service-sessions/:id/cancel-awaiting-submission", s.handleCancelAwaitingSubmission,
		authn.RequireAuth(), authn.RequireCapability(CapSalesOperate))
	v1.POST("/sales/service-sessions/:id/abandon", s.handleAbandonCheckout,
		authn.RequireAuth(), authn.RequireCapability(CapSalesOperate))
```

`http.go`, after `handleCloseSession`:

```go
// handleCancelAwaitingSubmission godoc
//
//	@Summary		Cancel paid work that never reached the bar
//	@Description	Withdraws the charges of a Session's paid, unsubmitted Committed Items so the Refund command can return the money. Allowed only on an ACTIVE Session with no Order that is Awaiting Submission. Requires a reason (CUSTOMER_LEFT, CUSTOMER_REQUEST, SYSTEM_FAILURE, OTHER); OTHER requires a note.
//	@Tags			sales
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string					true	"Service Session ID"
//	@Param			body	body		CheckoutRecoveryCommand	true	"Cancel request"
//	@Success		200		{object}	response.APIResponse{data=ServiceSessionResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Router			/sales/service-sessions/{id}/cancel-awaiting-submission [post]
func (s *Slices) handleCancelAwaitingSubmission(c echo.Context) error {
	return s.handleCheckoutRecovery(c, s.CancelAwaitingSubmission.Handle)
}

// handleAbandonCheckout godoc
//
//	@Summary		Abandon an unsubmitted checkout
//	@Description	Ends an ACTIVE Session that has no Order and holds no money as an Abandoned Checkout: drafts CANCELLED, live Checks ABANDONED, Tables released, no Completed Sale. Every Payment must first be fully refunded. Requires a reason (CUSTOMER_LEFT, CUSTOMER_REQUEST, SYSTEM_FAILURE, OTHER); OTHER requires a note.
//	@Tags			sales
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string					true	"Service Session ID"
//	@Param			body	body		CheckoutRecoveryCommand	true	"Abandon request"
//	@Success		200		{object}	response.APIResponse{data=ServiceSessionResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Router			/sales/service-sessions/{id}/abandon [post]
func (s *Slices) handleAbandonCheckout(c echo.Context) error {
	return s.handleCheckoutRecovery(c, s.AbandonCheckout.Handle)
}

// handleCheckoutRecovery is the shared request path of the two Phase 08
// commands, which take the same body and return the same projection.
func (s *Slices) handleCheckoutRecovery(c echo.Context,
	handle func(context.Context, Actor, CheckoutRecoveryCommand) (int, ServiceSessionResponse, error),
) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	sessionID, err := parseUUIDParam(c, "id")
	if err != nil {
		return sendError(c, err)
	}
	body, err := bindBody[CheckoutRecoveryCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(body.RequestID); err != nil {
		return sendError(c, err)
	}
	body.ServiceSessionID = sessionID

	status, result, err := handle(c.Request().Context(), actor, body)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, result)
}
```

Add `"context"` to `http.go`'s imports if absent.

- [ ] **Step 3: Run the tests**

Run: `go test ./internal/sales/`
Expected: PASS.

- [ ] **Step 4: Regenerate Swagger and the web client**

Run: `make swagger && cd web && bun run codegen && bunx tsc --noEmit`
Expected: `docs/swagger.*` gain both routes; the generated models gain `CheckoutRecoveryCommand`, `AbandonedCheckoutResponse`, `awaiting_submission`, `withdrawn`; the type check passes. If it flags a `preparation_unit_id` use that assumed a non-null string, narrow it at the use site.

- [ ] **Step 5: Commit**

```bash
git add internal/sales docs/swagger.json docs/swagger.yaml docs/docs.go web/src/api/generated
git commit -m "feat(sales): expose cancel-awaiting-submission and abandon routes"
```

---

### Task 7: Concurrency

**Files:**
- Test: `internal/sales/checkout_recovery_integration_test.go`

**Interfaces:**
- Consumes: helpers `cancel`, `abandon`, `paidTakeaway`, `TrySubmit`, `TryCommit`, `payCash`.

- [ ] **Step 1: Write the tests**

Append (add `"errors"`, `"sync"`, and `"github.com/jackc/pgx/v5/pgconn"` to the imports):

```go
func isDeadlock(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "40P01"
}

func TestCancelAgainstConcurrentSubmit(t *testing.T) {
	env := newRecoveryEnv(t)
	ctx := context.Background()
	session, _ := env.paidTakeaway(t)

	var wg sync.WaitGroup
	var cancelErr, submitErr error
	wg.Add(2)
	go func() { defer wg.Done(); _, _, cancelErr = env.cancel(t, session.ID) }()
	go func() { defer wg.Done(); _, _, submitErr = env.TrySubmit(t, session.ID) }()
	wg.Wait()

	require.True(t, (cancelErr == nil) != (submitErr == nil),
		"exactly one wins: cancel=%v submit=%v", cancelErr, submitErr)
	var orders int
	require.NoError(t, env.DB.QueryRowContext(ctx,
		`SELECT count(*) FROM orders WHERE service_session_id = $1`, session.ID).Scan(&orders))
	if cancelErr == nil {
		require.ErrorIs(t, submitErr, sales.ErrNothingToSubmit)
		require.Zero(t, orders)
	} else {
		require.ErrorIs(t, cancelErr, sales.ErrSessionHasOrder)
		require.Equal(t, 1, orders)
	}
}

func TestAbandonAgainstConcurrentCommit(t *testing.T) {
	env := newRecoveryEnv(t)
	session := env.StartTakeaway(t)
	env.AddDraftItem(t, session.ID, env.CoffeeID, nil)

	var wg sync.WaitGroup
	var abandonErr, commitErr error
	wg.Add(2)
	go func() { defer wg.Done(); _, _, abandonErr = env.abandon(t, session.ID) }()
	go func() { defer wg.Done(); _, _, commitErr = env.TryCommit(t, session.ID) }()
	wg.Wait()

	// Commit locks draft and Session in one statement, whose tuple-lock order
	// PostgreSQL does not fix, while Abandon locks the Session then writes the
	// draft: a 40P01 on either side is the recorded window (ADR-066).
	if abandonErr != nil {
		require.True(t, isDeadlock(abandonErr), "abandon failed outside the window: %v", abandonErr)
	}
	if commitErr != nil {
		require.True(t, isDeadlock(commitErr) || errors.Is(commitErr, sales.ErrEditableDraftNotFound),
			"commit failed unexpectedly: %v", commitErr)
	}
	require.True(t, abandonErr == nil || commitErr == nil,
		"at least one side stands: abandon=%v commit=%v", abandonErr, commitErr)
	if abandonErr == nil {
		require.Equal(t, sales.StateAbandoned, env.GetSessionOK(t, session.ID).State,
			"an unpaid commit never stops an abandon")
	}
}

func TestCancelAgainstConcurrentPayment(t *testing.T) {
	// ADR-066 inherits ADR-031's window: either side may abort with 40P01,
	// never both, and nothing half-written survives.
	env := newRecoveryEnv(t)
	session := env.commitDineInDraft(t, 1)
	checkID := env.soleCheckID(t, session.ID)
	_, _, err := env.payCash(t, checkID, 20000, 20000)
	require.NoError(t, err)

	var wg sync.WaitGroup
	var cancelErr, payErr error
	wg.Add(2)
	go func() { defer wg.Done(); _, _, cancelErr = env.cancel(t, session.ID) }()
	go func() { defer wg.Done(); _, _, payErr = env.payCash(t, checkID, 30000, 30000) }()
	wg.Wait()

	if cancelErr != nil {
		require.True(t, isDeadlock(cancelErr), "cancel failed outside the window: %v", cancelErr)
	}
	// payErr is not constrained on its own: a Payment serialized after the
	// withdrawal meets a zero-charge, settled Check and is rejected by the
	// existing Payment rules, which is correct. The invariants below are what
	// must hold either way.
	require.True(t, cancelErr == nil || payErr == nil,
		"at least one side stands: cancel=%v pay=%v", cancelErr, payErr)

	got := env.GetSessionOK(t, session.ID)
	check := env.findCheck(t, got, checkID)
	if cancelErr == nil {
		require.Equal(t, int64(0), check.ChargeVND)
		require.Equal(t, check.EffectiveReceivedVND, check.PendingRefundVND,
			"every unit of money held is owed back")
	}
}
```

- [ ] **Step 2: Run them with the race detector, repeatedly**

Run: `go test -count=10 -race -tags=integration ./internal/sales/ -run 'Concurrent'`
Expected: PASS on all runs.

- [ ] **Step 3: Commit**

```bash
git add internal/sales/checkout_recovery_integration_test.go
git commit -m "test(sales): concurrency contracts for checkout recovery"
```

---

### Task 8: Decisions, roadmap, and backlog

**Files:**
- Modify: `spec/decisions.md`, `ROADMAP.md`, `docs/backlog/phase-08-recover-failed-or-abandoned-checkout.md`, `docs/backlog/README.md`, `docs/backlog/backend-alignment.md`, `docs/superpowers/specs/2026-10-02-phase-08-checkout-recovery-design.md` (Status line only)

- [ ] **Step 1: Append ADR-064 to ADR-066**

Append to `spec/decisions.md`, in the file's existing format (`## ADR-NNN: title`, then `* **Decision Date:**`, `* **Status:** Accepted`, `* **Context:**`, `* **Decision:**`, `* **Consequences:**`). Date `2026-10-02`. Take the three decisions' text from spec §13, and add to ADR-066's Consequences: "Service Session closure reports `AWAITING_SUBMISSION_FOR_CLOSURE` first; Shift closure reports `SHIFT_AWAITING_SUBMISSION` fourth, before `SHIFT_ACTIVE_SERVICE_SESSION`. Abandon can also meet a draft command (Commit, draft edits) in a Session/draft AB-BA, because those lock draft and Session in one statement; the outcome is the same 40P01-and-clean-retry contract."

- [ ] **Step 2: Update status documents**

- `ROADMAP.md`: add Phase **08** to the Delivered table with a link to the spec; remove it from Remaining; change Phase 09's "Blocked by" to `none`; change "Sixty-three architecture decisions" to "Sixty-six"; replace "Phase **08** is unblocked and is the next backend work to pick up." with "Phase **09** is unblocked and is the next backend work to pick up."
- Phase 08 ticket: `**Status:** completed`, tick every acceptance criterion except the `(Phase 11 — UI)` one.
- `docs/backlog/README.md`: Phase 08 row `completed`.
- `backend-alignment.md`, BA-5 row: append "Also withdraw a stuck, unsubmitted extra round from a Session that already has an Order (Phase 08 non-goal)."
- Spec status line: `**Status:** Approved`.

- [ ] **Step 3: Run the full gate**

Run: `make check && make test-integration`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add spec/decisions.md ROADMAP.md docs/backlog docs/superpowers/specs/2026-10-02-phase-08-checkout-recovery-design.md
git commit -m "docs: record Phase 08 decisions and mark it delivered"
```
