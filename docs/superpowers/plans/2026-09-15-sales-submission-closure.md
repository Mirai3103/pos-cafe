# Submission, Preparation Units & Service Session Closure (Phase 5D) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the preparation boundary and the end of a Service Session's life — Submit, the later Order Draft it unblocks, the Preparation Unit advance chain, closure, and the immutable Completed Sale.

**Architecture:** Extends `internal/sales` in place and creates `internal/preparation` holding exactly one command. `internal/sales` creates Preparation Units at Submit and reads their state during closure; `internal/preparation` owns every state transition. Both packages use the generic `ExecuteMutation[T]` from 5A — closure carries `T = CompletedSaleResponse`, so no dedicated closing-requests table is needed. Closure policy lives in one pure function over the Service Session projection.

**Tech Stack:** Go 1.26+, Echo v4, PostgreSQL via `jackc/pgx/v5` (stdlib `database/sql` driver), sqlc, `google/uuid`, `stretchr/testify`, swag/OpenAPI 2.0.

**Spec:** [`docs/superpowers/specs/2026-09-15-sales-submission-closure-design.md`](../specs/2026-09-15-sales-submission-closure-design.md)

## Global Constraints

Every task's requirements implicitly include this section.

- **Package boundary (ADR-024).** `internal/sales` imports `internal/auth`, `internal/database/sqlc`, `internal/response`, `internal/httpvalidator`. `internal/preparation` imports the same four and **MUST NOT** import `internal/sales`. Neither imports `internal/catalog`, `internal/tables`, or `internal/shift`. Test files may import anything.
- **Query-file boundary (ADR-024).** No query in `sql/queries/sales.sql` writes `preparation_units.state` or touches `preparation_unit_transitions`. No query in `sql/queries/preparation.sql` writes `orders`, `order_items`, or `completed_sales`. sqlc generates one shared package, so this is enforced in review, not by the compiler.
- **Capability.** Sales operations require `sales.operate`; the advance command requires `preparation.operate`. Both already exist in `auth.RoleCapabilities`. **Do not modify the capability table.**
- **Idempotency.** Use the shared `idempotency_keys` table. Action names: `sales.submit_order`, `sales.close_service_session`, `preparation.advance_unit`.
- **Audit.** Use the shared `audit_events` table. Business events are `UPPER_SNAKE_CASE`.
- **No snapshot copy (ADR-025).** `order_items` stores `order_id` and `committed_item_id` only. Never add a commercial column to it.
- **Preparation state domain (ADR-028).** The `CHECK` declares all six states; 5D writes only `QUEUED`, `IN_PREPARATION`, `READY`, `FULFILLED`.
- **Empty collections** serialize as `[]`, never `null`. `sqlc.yaml` already sets `emit_empty_slices: true`.
- **Integration tests** carry `//go:build integration`, live in package `sales_test` or `preparation_test`, and run with `-p 1`.
- **Every task ends with a commit.** Run `make fmt` before committing. Run `make sqlc` after editing any `sql/queries/*.sql` file.

---

## File Structure

**New files:**

| File | Responsibility |
| --- | --- |
| `internal/database/migrations/000011_create_sales_submission_slice.sql` | Four new tables |
| `internal/sales/closure.go` | `ClosureReadiness` and `EvaluateClosureReadiness` — the one closure policy |
| `internal/sales/closure_test.go` | Unit tests for the policy, table-driven |
| `internal/sales/submit.go` | The Submit command |
| `internal/sales/session_close.go` | The closure command |
| `internal/sales/completed_sale_reads.go` | Two Completed Sale reads |
| `internal/sales/submit_integration_test.go` | Submit suites, both service modes |
| `internal/sales/session_close_integration_test.go` | Closure suites, success and four rejections |
| `internal/sales/completed_sale_integration_test.go` | Read suites |
| `internal/preparation/domain.go` | Capability, operation and event names, the transition table |
| `internal/preparation/domain_test.go` | Unit tests for the transition table |
| `internal/preparation/errors.go` | Sentinels and HTTP mapping |
| `internal/preparation/errors_test.go` | Mapping tests |
| `internal/preparation/executor.go` | `Actor`, `Runner`, `ExecuteMutation[T]`, adapted from `internal/sales/executor.go` |
| `internal/preparation/dto.go` | `AdvanceUnitCommand`, `PreparationUnitResponse` |
| `internal/preparation/advance.go` | The advance command |
| `internal/preparation/http.go` | Echo handler with Swagger annotations |
| `internal/preparation/routes.go` | `Slices` and `RegisterRoutes` |
| `internal/preparation/env_integration_test.go` | Test environment |
| `internal/preparation/advance_integration_test.go` | Advance suites |
| `sql/queries/preparation.sql` | Advance queries |

**Modified files:**

| File | Change |
| --- | --- |
| `internal/sales/domain.go` | Preparation unit states, new operation and event names, `ModeRequiresSettlementBeforeSubmit` |
| `internal/sales/errors.go` | Eight new sentinels and their HTTP status mapping |
| `internal/sales/errors_test.go` | Mapping tests for every new code |
| `internal/sales/dto.go` | `OrderResponse`, `OrderItemResponse`, `PreparationUnitResponse`, `PreparationTransitionResponse`, `CompletedSaleResponse`, `SubmitOrderCommand`, `CloseServiceSessionCommand`; `Orders` and `PreparationUnits` retyped |
| `internal/sales/dto_test.go` | Serialization tests for the new shapes |
| `internal/sales/projection.go` | Order and Preparation Unit assembly; real `submitted` |
| `internal/sales/http.go` | Four Echo handlers with Swagger annotations |
| `internal/sales/routes.go` | Four handlers on `Slices`, four routes |
| `internal/sales/schema_integration_test.go` | Assertions for the four new tables |
| `internal/sales/env_integration_test.go` | Submit, close, and advance helpers |
| `sql/queries/sales.sql` | Submit, closure, projection, and read queries; `FindBlockingDraft` relaxed |
| `cmd/api/main.go` | Mount `preparation.Slices` |
| `MIGRATE_PLAN.md` | Mark 5D and the Phase 5 tracker row complete |

---

## Task 1: Database Schema

**Files:**
- Create: `internal/database/migrations/000011_create_sales_submission_slice.sql`
- Modify: `internal/sales/schema_integration_test.go`

**Interfaces:**
- Consumes: 5A's `service_sessions`, `order_drafts`; 5B's `committed_items`; Phase 1's `staff_identities`, `staff_access_sessions`.
- Produces: tables `orders`, `order_items`, `preparation_units`, `preparation_unit_transitions`, `completed_sales`; unique indexes `order_draft_unique`, `order_item_committed_item_unique`, `preparation_unit_item_number_unique`, `completed_sale_service_session_unique`; constraints `preparation_unit_state_valid`, `preparation_unit_transition_states_valid`.

- [ ] **Step 1: Write the failing schema test**

Append to `internal/sales/schema_integration_test.go`:

```go
func TestSubmissionSchema(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	t.Run("all five tables exist", func(t *testing.T) {
		var n int
		err := db.QueryRowContext(ctx, `
			SELECT count(*) FROM information_schema.tables
			WHERE table_name IN ('orders', 'order_items', 'preparation_units',
			                     'preparation_unit_transitions', 'completed_sales')`).Scan(&n)
		require.NoError(t, err)
		require.Equal(t, 5, n)
	})

	t.Run("order_items carries no commercial snapshot", func(t *testing.T) {
		var n int
		err := db.QueryRowContext(ctx, `
			SELECT count(*) FROM information_schema.columns
			WHERE table_name = 'order_items'`).Scan(&n)
		require.NoError(t, err)
		require.Equal(t, 3, n, "ADR-025: id, order_id, committed_item_id only")
	})

	t.Run("one Order per Order Draft is unrepresentable", func(t *testing.T) {
		var n int
		err := db.QueryRowContext(ctx, `
			SELECT count(*) FROM pg_indexes
			WHERE indexname IN ('order_draft_unique', 'order_item_committed_item_unique',
			                    'preparation_unit_item_number_unique',
			                    'completed_sale_service_session_unique')`).Scan(&n)
		require.NoError(t, err)
		require.Equal(t, 4, n)
	})

	t.Run("preparation unit state declares all six canonical values", func(t *testing.T) {
		var clause string
		err := db.QueryRowContext(ctx, `
			SELECT pg_get_constraintdef(oid) FROM pg_constraint
			WHERE conname = 'preparation_unit_state_valid'`).Scan(&clause)
		require.NoError(t, err)
		for _, state := range []string{"QUEUED", "IN_PREPARATION", "READY", "FULFILLED", "CANCELLED", "WASTED"} {
			require.Contains(t, clause, state)
		}
	})

	t.Run("the transition graph is enforced in the database", func(t *testing.T) {
		var clause string
		err := db.QueryRowContext(ctx, `
			SELECT pg_get_constraintdef(oid) FROM pg_constraint
			WHERE conname = 'preparation_unit_transition_states_valid'`).Scan(&clause)
		require.NoError(t, err)
		require.Contains(t, clause, "QUEUED")
		require.Contains(t, clause, "IN_PREPARATION")
		require.Contains(t, clause, "READY")
		require.Contains(t, clause, "FULFILLED")
		require.NotContains(t, clause, "CANCELLED", "Phase 6 adds the Cancellation pairs")
	})
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -tags integration ./internal/sales/ -run TestSubmissionSchema -v -p 1`
Expected: FAIL — the tables do not exist, so the counts are 0 and the `pg_get_constraintdef` scans return `sql.ErrNoRows`.

- [ ] **Step 3: Write the migration**

Create `internal/database/migrations/000011_create_sales_submission_slice.sql` with the five `CREATE TABLE` statements and their indexes exactly as given in spec §5.1 through §5.5. Copy them verbatim; the spec's DDL is the authority. Precede the file with this header comment:

```sql
-- Phase 5D: Submission, Preparation Units & Service Session Closure.
--
-- Submit is the preparation boundary: it turns Committed Items into an Order
-- and the Preparation Units the bar works from, without repricing anything.
-- Closure freezes a Service Session into an immutable Completed Sale.
--
-- order_items deliberately carries no commercial snapshot (ADR-025):
-- committed_items is immutable by 5B's rule, and copying it across a
-- one-to-one foreign key would only create a way for the two to disagree.
--
-- preparation_units does snapshot, for a reason order_items does not share:
-- the bar display is the hottest read path in the system and Phase 6 reads
-- these same columns for FIFO ordering and alerts.
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test -tags integration ./internal/sales/ -run TestSubmissionSchema -v -p 1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
make fmt
git add internal/database/migrations/000011_create_sales_submission_slice.sql internal/sales/schema_integration_test.go
git commit -m "feat(sales): add Phase 5D submission, preparation and closure schema"
```

---

## Task 2: Domain Constants And Error Sentinels

**Files:**
- Modify: `internal/sales/domain.go`, `internal/sales/domain_test.go`, `internal/sales/errors.go`, `internal/sales/errors_test.go`

**Interfaces:**
- Consumes: Task 1's schema.
- Produces: `UnitStateQueued`, `UnitStateInPreparation`, `UnitStateReady`, `UnitStateFulfilled`, `UnitStateCancelled`, `UnitStateWasted`; `IsTerminalUnitState(string) bool`; `ModeRequiresSettlementBeforeSubmit(string) bool`; `OpSubmitOrder`, `OpCloseServiceSession`, `OpGetCompletedSale`, `OpGetCompletedSaleBySession`; `EventOrderSubmitted`, `EventServiceSessionClosed`; sentinels `ErrCheckNotSettledForSubmission`, `ErrCheckNotSettledForClosure`, `ErrUnsubmittedWorkForClosure`, `ErrOrderRequiredForClosure`, `ErrUnfulfilledPreparationForClosure`, `ErrCompletedSaleNotFound`, `ErrNothingToSubmit`.

- [ ] **Step 1: Write the failing unit tests**

Append to `internal/sales/domain_test.go`:

```go
func TestIsTerminalUnitState(t *testing.T) {
	terminal := []string{sales.UnitStateFulfilled, sales.UnitStateCancelled, sales.UnitStateWasted}
	for _, s := range terminal {
		require.True(t, sales.IsTerminalUnitState(s), s)
	}
	for _, s := range []string{sales.UnitStateQueued, sales.UnitStateInPreparation, sales.UnitStateReady} {
		require.False(t, sales.IsTerminalUnitState(s), s)
	}
}

func TestModeRequiresSettlementBeforeSubmit(t *testing.T) {
	// Takeaway keeps settlement-before-Submit: nothing is prepared for a
	// customer who has not paid and may walk. Dine-in supports both orderings,
	// because a seated customer's drinks go to the bar long before the bill.
	require.True(t, sales.ModeRequiresSettlementBeforeSubmit(sales.ModeTakeaway))
	require.False(t, sales.ModeRequiresSettlementBeforeSubmit(sales.ModeDineIn))
}
```

Append to `internal/sales/errors_test.go`:

```go
func TestPhase5DErrorMapping(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{sales.ErrCheckNotSettledForSubmission, http.StatusConflict, "CHECK_NOT_SETTLED_FOR_SUBMISSION"},
		{sales.ErrCheckNotSettledForClosure, http.StatusConflict, "CHECK_NOT_SETTLED_FOR_CLOSURE"},
		{sales.ErrUnsubmittedWorkForClosure, http.StatusConflict, "UNSUBMITTED_WORK_FOR_CLOSURE"},
		{sales.ErrOrderRequiredForClosure, http.StatusConflict, "ORDER_REQUIRED_FOR_CLOSURE"},
		{sales.ErrUnfulfilledPreparationForClosure, http.StatusConflict, "UNFULFILLED_PREPARATION_FOR_CLOSURE"},
		{sales.ErrCompletedSaleNotFound, http.StatusNotFound, "COMPLETED_SALE_NOT_FOUND"},
		{sales.ErrNothingToSubmit, http.StatusConflict, "NOTHING_TO_SUBMIT"},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			status, body := sales.ErrorResponse(tc.err)
			require.Equal(t, tc.status, status)
			require.Equal(t, tc.code, body.Error.Code)
		})
	}
}
```

If `errors_test.go` already has a helper that calls the package's error-mapping entry point under a different name, use that name instead of `sales.ErrorResponse` — read the top of the existing file and match it.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/sales/ -run 'TestIsTerminalUnitState|TestModeRequiresSettlement|TestPhase5DErrorMapping' -v`
Expected: FAIL to compile — the constants, functions, and sentinels are undefined.

- [ ] **Step 3: Add the domain constants and functions**

Append to `internal/sales/domain.go`:

```go
// Preparation Unit states. ADR-028 declares the complete canonical domain
// although 5D writes only the first four: 5A shipped a guessed partial domain
// for service_sessions.state and had to correct it, and 5C responded with the
// complete-domain precedent this follows.
const (
	UnitStateQueued        = "QUEUED"
	UnitStateInPreparation = "IN_PREPARATION"
	UnitStateReady         = "READY"
	UnitStateFulfilled     = "FULFILLED"
	UnitStateCancelled     = "CANCELLED"
	UnitStateWasted        = "WASTED"
)

// IsTerminalUnitState reports whether a Preparation Unit has reached a state
// it cannot leave. Closure requires every unit to be terminal. Cancelled and
// Wasted are unreachable in Phase 5 but accepted here, so the policy is
// written once against the complete domain rather than re-edited in Phase 6.
func IsTerminalUnitState(state string) bool {
	switch state {
	case UnitStateFulfilled, UnitStateCancelled, UnitStateWasted:
		return true
	default:
		return false
	}
}

// ModeRequiresSettlementBeforeSubmit reports whether a Service Session's mode
// forbids sending work to the bar while a Check still carries a balance.
//
// Takeaway does: nothing is prepared for a customer who has not paid and may
// walk. Dine-in does not: a seated customer's drinks go to the bar long before
// the bill is settled, so both Commit -> Submit -> Payment and Commit ->
// Payment -> Submit are valid service.
func ModeRequiresSettlementBeforeSubmit(mode string) bool {
	return mode == ModeTakeaway
}
```

Add to the existing operation-name block in `domain.go`:

```go
	OpSubmitOrder               = "sales.submit_order"
	OpCloseServiceSession       = "sales.close_service_session"
	OpGetCompletedSale          = "sales.get_completed_sale"
	OpGetCompletedSaleBySession = "sales.get_completed_sale_by_session"
```

Add to the existing audit-event block in `domain.go`:

```go
	// EventOrderSubmitted drops the canonical TAKEAWAY_ORDER_ prefix, for the
	// same reason EventOrderDraftCommitted did: Submit is mode-agnostic.
	EventOrderSubmitted       = "ORDER_SUBMITTED"
	EventServiceSessionClosed = "SERVICE_SESSION_CLOSED"
```

- [ ] **Step 4: Add the error sentinels and mapping**

Append to the sentinel block in `internal/sales/errors.go`:

```go
	ErrNothingToSubmit                 = errors.New("no committed order draft awaits submission")
	ErrCheckNotSettledForSubmission    = errors.New("every check must be settled before a takeaway order is submitted")
	ErrCheckNotSettledForClosure       = errors.New("every check must be settled before the service session closes")
	ErrUnsubmittedWorkForClosure       = errors.New("every committed item must be submitted before the service session closes")
	ErrOrderRequiredForClosure         = errors.New("a service session with no order cannot close")
	ErrUnfulfilledPreparationForClosure = errors.New("every preparation unit must be terminal before the service session closes")
	ErrCompletedSaleNotFound           = errors.New("completed sale not found")
```

Add to the `switch` in the mapping function, following the existing `coded(...)` style:

```go
	case errors.Is(err, ErrNothingToSubmit):
		return coded(http.StatusConflict, "NOTHING_TO_SUBMIT", ErrNothingToSubmit)
	case errors.Is(err, ErrCheckNotSettledForSubmission):
		return coded(http.StatusConflict, "CHECK_NOT_SETTLED_FOR_SUBMISSION", ErrCheckNotSettledForSubmission)
	case errors.Is(err, ErrCheckNotSettledForClosure):
		return coded(http.StatusConflict, "CHECK_NOT_SETTLED_FOR_CLOSURE", ErrCheckNotSettledForClosure)
	case errors.Is(err, ErrUnsubmittedWorkForClosure):
		return coded(http.StatusConflict, "UNSUBMITTED_WORK_FOR_CLOSURE", ErrUnsubmittedWorkForClosure)
	case errors.Is(err, ErrOrderRequiredForClosure):
		return coded(http.StatusConflict, "ORDER_REQUIRED_FOR_CLOSURE", ErrOrderRequiredForClosure)
	case errors.Is(err, ErrUnfulfilledPreparationForClosure):
		return coded(http.StatusConflict, "UNFULFILLED_PREPARATION_FOR_CLOSURE", ErrUnfulfilledPreparationForClosure)
	case errors.Is(err, ErrCompletedSaleNotFound):
		return coded(http.StatusNotFound, "COMPLETED_SALE_NOT_FOUND", ErrCompletedSaleNotFound)
```

Spec §9.3 splits the canonical `CHECK_NOT_SETTLED_FOR_SUBMISSION`, which that source raises for three distinct situations. `ErrNothingToSubmit` covers "no committed draft awaits submission"; a missing or closed Session reuses `ErrServiceSessionNotFound` and `ErrServiceSessionClosed`. A cashier told a Check is unsettled when the real problem is a closed Session will go looking in the wrong place.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/sales/ -run 'TestIsTerminalUnitState|TestModeRequiresSettlement|TestPhase5DErrorMapping' -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
make fmt
git add internal/sales/domain.go internal/sales/domain_test.go internal/sales/errors.go internal/sales/errors_test.go
git commit -m "feat(sales): add Phase 5D domain constants and error codes"
```

---

## Task 3: Closure Readiness Policy

**Files:**
- Create: `internal/sales/closure.go`, `internal/sales/closure_test.go`

**Interfaces:**
- Consumes: Task 2's `IsTerminalUnitState`; `CheckStateSettled` and `CheckStateMerged` from 5C.
- Produces: `type ClosureReadiness`, `func EvaluateClosureReadiness(ServiceSessionResponse) ClosureReadiness`, `func (ClosureReadiness) Err() error`.

This task is written before the DTO fields it reads exist. Task 4 adds `Orders` and `PreparationUnits` as real types. To keep this task independently testable, **do Task 4 first if you are executing out of order** — otherwise the compile fails. The recommended order is sequential.

- [ ] **Step 1: Write the failing unit tests**

Create `internal/sales/closure_test.go`:

```go
package sales_test

import (
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// eligible builds a Service Session projection that satisfies all four
// closure conditions. Each test below breaks exactly one of them, so a
// failing assertion names the condition that actually regressed.
func eligible() sales.ServiceSessionResponse {
	itemID := uuid.New()
	return sales.ServiceSessionResponse{
		Checks: []sales.CheckResponse{{
			ID:    uuid.New(),
			State: sales.CheckStateSettled,
			Allocations: []sales.ChargeAllocationResponse{{
				ID: uuid.New(), CommittedItemID: itemID, Submitted: true,
			}},
		}},
		Orders:           []sales.OrderResponse{{ID: uuid.New()}},
		PreparationUnits: []sales.PreparationUnitResponse{{ID: uuid.New(), State: sales.UnitStateFulfilled}},
	}
}

func TestEvaluateClosureReadinessEligible(t *testing.T) {
	got := sales.EvaluateClosureReadiness(eligible())
	require.True(t, got.Eligible)
	require.NoError(t, got.Err())
}

func TestEvaluateClosureReadinessUnsettledCheck(t *testing.T) {
	s := eligible()
	s.Checks[0].State = sales.CheckStateOpen
	got := sales.EvaluateClosureReadiness(s)
	require.False(t, got.Eligible)
	require.Len(t, got.UnsettledCheckIDs, 1)
	require.ErrorIs(t, got.Err(), sales.ErrCheckNotSettledForClosure)
}

func TestEvaluateClosureReadinessMergedCheckIsNotUnsettled(t *testing.T) {
	// A merged Check was absorbed into another and owes nothing.
	s := eligible()
	s.Checks = append(s.Checks, sales.CheckResponse{ID: uuid.New(), State: sales.CheckStateMerged})
	got := sales.EvaluateClosureReadiness(s)
	require.True(t, got.Eligible)
}

func TestEvaluateClosureReadinessAllChecksMerged(t *testing.T) {
	// A Session whose every Check was merged away has no surviving Check and
	// has settled nothing, so it is not eligible even though nothing is OPEN.
	s := eligible()
	s.Checks = []sales.CheckResponse{{ID: uuid.New(), State: sales.CheckStateMerged}}
	got := sales.EvaluateClosureReadiness(s)
	require.False(t, got.Eligible)
	require.ErrorIs(t, got.Err(), sales.ErrCheckNotSettledForClosure)
}

func TestEvaluateClosureReadinessNoChecks(t *testing.T) {
	s := eligible()
	s.Checks = nil
	got := sales.EvaluateClosureReadiness(s)
	require.False(t, got.Eligible)
	require.ErrorIs(t, got.Err(), sales.ErrCheckNotSettledForClosure)
}

func TestEvaluateClosureReadinessUnsubmittedWork(t *testing.T) {
	s := eligible()
	s.Checks[0].Allocations[0].Submitted = false
	got := sales.EvaluateClosureReadiness(s)
	require.False(t, got.Eligible)
	require.Len(t, got.UnsubmittedCommittedItemIDs, 1)
	require.ErrorIs(t, got.Err(), sales.ErrUnsubmittedWorkForClosure)
}

func TestEvaluateClosureReadinessNoOrder(t *testing.T) {
	s := eligible()
	s.Orders = nil
	got := sales.EvaluateClosureReadiness(s)
	require.False(t, got.Eligible)
	require.ErrorIs(t, got.Err(), sales.ErrOrderRequiredForClosure)
}

func TestEvaluateClosureReadinessNonterminalUnit(t *testing.T) {
	for _, state := range []string{sales.UnitStateQueued, sales.UnitStateInPreparation, sales.UnitStateReady} {
		t.Run(state, func(t *testing.T) {
			s := eligible()
			s.PreparationUnits[0].State = state
			got := sales.EvaluateClosureReadiness(s)
			require.False(t, got.Eligible)
			require.Len(t, got.NonterminalUnitIDs, 1)
			require.ErrorIs(t, got.Err(), sales.ErrUnfulfilledPreparationForClosure)
		})
	}
}

func TestEvaluateClosureReadinessCancelledAndWastedAreTerminal(t *testing.T) {
	for _, state := range []string{sales.UnitStateCancelled, sales.UnitStateWasted} {
		t.Run(state, func(t *testing.T) {
			s := eligible()
			s.PreparationUnits[0].State = state
			require.True(t, sales.EvaluateClosureReadiness(s).Eligible)
		})
	}
}

func TestEvaluateClosureReadinessRejectionOrder(t *testing.T) {
	// Staff fix what they are told about first, so the order decides which of
	// several outstanding problems they are sent to resolve: money, then
	// work, then the bar.
	s := eligible()
	s.Checks[0].State = sales.CheckStateOpen
	s.Checks[0].Allocations[0].Submitted = false
	s.Orders = nil
	s.PreparationUnits[0].State = sales.UnitStateQueued

	got := sales.EvaluateClosureReadiness(s)
	require.ErrorIs(t, got.Err(), sales.ErrCheckNotSettledForClosure)

	s.Checks[0].State = sales.CheckStateSettled
	require.ErrorIs(t, sales.EvaluateClosureReadiness(s).Err(), sales.ErrUnsubmittedWorkForClosure)

	s.Checks[0].Allocations[0].Submitted = true
	require.ErrorIs(t, sales.EvaluateClosureReadiness(s).Err(), sales.ErrOrderRequiredForClosure)

	s.Orders = []sales.OrderResponse{{ID: uuid.New()}}
	require.ErrorIs(t, sales.EvaluateClosureReadiness(s).Err(), sales.ErrUnfulfilledPreparationForClosure)
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/sales/ -run TestEvaluateClosureReadiness -v`
Expected: FAIL to compile — `ClosureReadiness` and `EvaluateClosureReadiness` do not exist.

- [ ] **Step 3: Write the policy**

Create `internal/sales/closure.go`:

```go
package sales

import "github.com/google/uuid"

// ClosureReadiness is the verdict on whether a Service Session may close,
// carrying the failing detail alongside it so the API can say what is
// outstanding rather than only that something is.
type ClosureReadiness struct {
	Eligible bool

	AllChecksSettled    bool
	AllWorkSubmitted    bool
	HasOrder            bool
	AllPreparationDone  bool

	UnsettledCheckIDs           []uuid.UUID
	UnsubmittedCommittedItemIDs []uuid.UUID
	NonterminalUnitIDs          []uuid.UUID
}

// EvaluateClosureReadiness is the one closure policy boundary, so the API's
// answer and any client's preview cannot drift apart. It is pure: every input
// it needs already travels in the Service Session projection, which is why 5D
// exposes no readiness endpoint.
//
// The canonical function also reports Checks carrying a pending Refund. That
// branch is not migrated (ADR-029): Refund is outside Phase 5, the column it
// reads is absent from the contract, and a check with no data source behind it
// is a check that always passes.
func EvaluateClosureReadiness(session ServiceSessionResponse) ClosureReadiness {
	out := ClosureReadiness{
		UnsettledCheckIDs:           make([]uuid.UUID, 0),
		UnsubmittedCommittedItemIDs: make([]uuid.UUID, 0),
		NonterminalUnitIDs:          make([]uuid.UUID, 0),
	}

	survivingCheck := false
	for _, check := range session.Checks {
		if check.State != CheckStateMerged {
			survivingCheck = true
		}
		if check.State != CheckStateSettled && check.State != CheckStateMerged {
			out.UnsettledCheckIDs = append(out.UnsettledCheckIDs, check.ID)
		}
		for _, allocation := range check.Allocations {
			if !allocation.Submitted {
				out.UnsubmittedCommittedItemIDs = append(
					out.UnsubmittedCommittedItemIDs, allocation.CommittedItemID)
			}
		}
	}
	for _, unit := range session.PreparationUnits {
		if !IsTerminalUnitState(unit.State) {
			out.NonterminalUnitIDs = append(out.NonterminalUnitIDs, unit.ID)
		}
	}

	// survivingCheck matters on its own: a Session whose Checks were all
	// merged away has no surviving Check and has settled nothing, even though
	// no Check is OPEN.
	out.AllChecksSettled = survivingCheck && len(out.UnsettledCheckIDs) == 0
	out.HasOrder = len(session.Orders) > 0
	out.AllWorkSubmitted = out.HasOrder && len(out.UnsubmittedCommittedItemIDs) == 0
	out.AllPreparationDone = out.HasOrder && len(out.NonterminalUnitIDs) == 0
	out.Eligible = out.AllChecksSettled && out.AllWorkSubmitted && out.AllPreparationDone

	return out
}

// Err returns the first unmet condition as a domain error, or nil when the
// Session may close. The order is load-bearing: staff fix what they are told
// about first, so it decides which of several outstanding problems they are
// sent to resolve — money before work, and work before the bar.
func (r ClosureReadiness) Err() error {
	switch {
	case !r.AllChecksSettled:
		return ErrCheckNotSettledForClosure
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

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/sales/ -run TestEvaluateClosureReadiness -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
make fmt
git add internal/sales/closure.go internal/sales/closure_test.go
git commit -m "feat(sales): add the Service Session closure readiness policy"
```

---

## Task 4: Order And Preparation DTOs And Projection

**Files:**
- Modify: `internal/sales/dto.go`, `internal/sales/dto_test.go`, `internal/sales/projection.go`, `sql/queries/sales.sql`, `internal/sales/projection_integration_test.go`

**Interfaces:**
- Consumes: Task 1's schema; the existing `LoadServiceSession`.
- Produces: `OrderResponse`, `OrderItemResponse`, `PreparationUnitResponse`, `UnitModifierResponse`, `SubmitOrderCommand`, `CloseServiceSessionCommand`; `ServiceSessionResponse.Orders []OrderResponse`; `ServiceSessionResponse.PreparationUnits []PreparationUnitResponse`; real `ChargeAllocationResponse.Submitted`; sqlc queries `ListSessionOrders`, `ListOrderItems`, `ListSessionPreparationUnits`, `ListSubmittedCommittedItems`.

- [ ] **Step 1: Write the failing serialization test**

Append to `internal/sales/dto_test.go`:

```go
func TestServiceSessionResponseOrdersAndUnitsSerialize(t *testing.T) {
	// The arrays were []struct{} placeholders through 5A, 5B and 5C. Filling
	// them must not change the shape of the contract: same keys, same types
	// for everything that already existed.
	s := sales.ServiceSessionResponse{
		Orders: []sales.OrderResponse{{
			ID:          uuid.New(),
			SubmittedAt: time.Unix(0, 0).UTC(),
			Items:       []sales.OrderItemResponse{{ID: uuid.New()}},
		}},
		PreparationUnits: []sales.PreparationUnitResponse{{
			ID:         uuid.New(),
			UnitNumber: 1,
			State:      sales.UnitStateQueued,
			Modifiers:  []sales.UnitModifierResponse{},
		}},
	}
	b, err := json.Marshal(s)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(b, &got))
	require.Contains(t, got, "orders")
	require.Contains(t, got, "preparation_units")
	require.Len(t, got["orders"], 1)
	require.Len(t, got["preparation_units"], 1)
}

func TestEmptyOrdersAndUnitsSerializeAsArrays(t *testing.T) {
	b, err := json.Marshal(sales.ServiceSessionResponse{
		Orders:           []sales.OrderResponse{},
		PreparationUnits: []sales.PreparationUnitResponse{},
	})
	require.NoError(t, err)
	require.Contains(t, string(b), `"orders":[]`)
	require.Contains(t, string(b), `"preparation_units":[]`)
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/sales/ -run 'TestServiceSessionResponseOrdersAndUnits|TestEmptyOrdersAndUnits' -v`
Expected: FAIL to compile — `OrderResponse` and `PreparationUnitResponse` do not exist and `Orders` is still `[]struct{}`.

- [ ] **Step 3: Add the DTOs**

In `internal/sales/dto.go`, replace the two placeholder fields on `ServiceSessionResponse`:

```go
	// Filled from 5D.
	Orders []OrderResponse `json:"orders"`
	// Filled from 5D.
	PreparationUnits []PreparationUnitResponse `json:"preparation_units"`
```

Update the doc comment above `ServiceSessionResponse` so it no longer says 5D will fill them, and update the comment above `ChargeAllocationResponse.Submitted` to state that it is derived from the existence of an Order Item.

Append these types to `dto.go`:

```go
// OrderResponse is one submitted Order: the preparation boundary crossed once
// for one Order Draft.
type OrderResponse struct {
	ID                 uuid.UUID           `json:"id"`
	OrderDraftID       uuid.UUID           `json:"order_draft_id"`
	SubmittedByStaffID uuid.UUID           `json:"submitted_by_staff_identity_id"`
	SubmittedSessionID uuid.UUID           `json:"submitted_staff_access_session_id"`
	SubmittedAt        time.Time           `json:"submitted_at"`
	Items              []OrderItemResponse `json:"items"`
}

// OrderItemResponse is a Committed Item after submission.
//
// It carries the commercial snapshot by reference rather than by copy
// (ADR-025): committed_items is immutable by 5B's rule, so the identifier is
// the snapshot. The names and amounts a client needs are already on the
// Check's Charge Allocations, keyed by the same CommittedItemID.
type OrderItemResponse struct {
	ID              uuid.UUID `json:"id"`
	CommittedItemID uuid.UUID `json:"committed_item_id"`
}

// UnitModifierResponse is one frozen Modifier Option on a Preparation Unit.
// It carries no price: the bar needs to know what to make, not what it cost.
type UnitModifierResponse struct {
	GroupName  string `json:"group_name"`
	OptionName string `json:"option_name"`
}

// PreparationUnitResponse is one individually prepared unit of an ordered
// item. A Committed Item of quantity three becomes three of these.
type PreparationUnitResponse struct {
	ID              uuid.UUID              `json:"id"`
	OrderItemID     uuid.UUID              `json:"order_item_id"`
	UnitNumber      int32                  `json:"unit_number"`
	State           string                 `json:"state"`
	ServiceNumber   string                 `json:"service_number"`
	CategoryName    string                 `json:"category_name"`
	ItemName        string                 `json:"item_name"`
	SizeName        *string                `json:"size_name"`
	Modifiers       []UnitModifierResponse `json:"modifiers"`
	PreparationNote *string                `json:"preparation_note"`
	QueuedAt        time.Time              `json:"queued_at"`
}

// SubmitOrderCommand sends a Service Session's committed round to the bar.
type SubmitOrderCommand struct {
	RequestID        uuid.UUID `json:"request_id" validate:"required"`
	ServiceSessionID uuid.UUID `json:"-"`
}

// CloseServiceSessionCommand completes a Service Session into a Completed Sale.
type CloseServiceSessionCommand struct {
	RequestID        uuid.UUID `json:"request_id" validate:"required"`
	ServiceSessionID uuid.UUID `json:"-"`
}
```

- [ ] **Step 4: Add the projection queries**

Append to `sql/queries/sales.sql`:

```sql
-- name: ListSessionOrders :many
SELECT id, order_draft_id, submitted_by_staff_identity_id,
       submitted_staff_access_session_id, submitted_at
FROM orders
WHERE service_session_id = $1
ORDER BY submitted_at ASC, id ASC;

-- name: ListOrderItems :many
SELECT id, order_id, committed_item_id
FROM order_items
WHERE order_id = ANY(sqlc.arg(order_ids)::uuid[])
ORDER BY order_id ASC, id ASC;

-- name: ListSessionPreparationUnits :many
SELECT pu.id, pu.order_item_id, pu.unit_number, pu.state, pu.service_number,
       pu.category_name, pu.item_name, pu.size_name, pu.modifiers,
       pu.preparation_note, pu.queued_at
FROM preparation_units pu
JOIN order_items oi ON oi.id = pu.order_item_id
JOIN orders o ON o.id = oi.order_id
WHERE o.service_session_id = $1
ORDER BY pu.queued_at ASC, pu.id ASC;

-- name: ListSubmittedCommittedItems :many
-- The `submitted` flag on a Charge Allocation is derived, not stored: there is
-- no submitted column anywhere in the schema, and therefore no flag that can
-- fall out of step with the Order that defines it.
SELECT committed_item_id
FROM order_items
WHERE committed_item_id = ANY(sqlc.arg(committed_item_ids)::uuid[]);
```

Run `make sqlc`.

- [ ] **Step 5: Assemble them in the projection**

In `internal/sales/projection.go`, change `newServiceSessionResponse` to initialize the real slice types:

```go
		Orders:           make([]OrderResponse, 0),
		PreparationUnits: make([]PreparationUnitResponse, 0),
```

Add these functions to `projection.go`:

```go
// loadOrders assembles the Session's Orders with their items.
func loadOrders(ctx context.Context, q *sqlc.Queries, sessionID uuid.UUID) (
	[]OrderResponse, error,
) {
	rows, err := q.ListSessionOrders(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("load session orders: %w", err)
	}
	out := make([]OrderResponse, 0, len(rows))
	if len(rows) == 0 {
		return out, nil
	}

	orderIDs := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		orderIDs = append(orderIDs, row.ID)
	}
	itemRows, err := q.ListOrderItems(ctx, orderIDs)
	if err != nil {
		return nil, fmt.Errorf("load order items: %w", err)
	}
	itemsByOrder := make(map[uuid.UUID][]OrderItemResponse, len(rows))
	for _, item := range itemRows {
		itemsByOrder[item.OrderID] = append(itemsByOrder[item.OrderID], OrderItemResponse{
			ID:              item.ID,
			CommittedItemID: item.CommittedItemID,
		})
	}

	for _, row := range rows {
		items := itemsByOrder[row.ID]
		if items == nil {
			items = make([]OrderItemResponse, 0)
		}
		out = append(out, OrderResponse{
			ID:                 row.ID,
			OrderDraftID:       row.OrderDraftID,
			SubmittedByStaffID: row.SubmittedByStaffIdentityID,
			SubmittedSessionID: row.SubmittedStaffAccessSessionID,
			SubmittedAt:        row.SubmittedAt,
			Items:              items,
		})
	}
	return out, nil
}

// loadPreparationUnits assembles the Session's Preparation Units.
//
// internal/sales reads unit state here and creates units at Submit; every
// state transition belongs to internal/preparation (ADR-024).
func loadPreparationUnits(ctx context.Context, q *sqlc.Queries, sessionID uuid.UUID) (
	[]PreparationUnitResponse, error,
) {
	rows, err := q.ListSessionPreparationUnits(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("load preparation units: %w", err)
	}
	out := make([]PreparationUnitResponse, 0, len(rows))
	for _, row := range rows {
		mods := make([]UnitModifierResponse, 0)
		if len(row.Modifiers) > 0 {
			if err := json.Unmarshal(row.Modifiers, &mods); err != nil {
				return nil, fmt.Errorf("decode preparation unit modifiers: %w", err)
			}
		}
		out = append(out, PreparationUnitResponse{
			ID:              row.ID,
			OrderItemID:     row.OrderItemID,
			UnitNumber:      row.UnitNumber,
			State:           row.State,
			ServiceNumber:   row.ServiceNumber,
			CategoryName:    row.CategoryName,
			ItemName:        row.ItemName,
			SizeName:        nullStringPtr(row.SizeName),
			Modifiers:       mods,
			PreparationNote: nullStringPtr(row.PreparationNote),
			QueuedAt:        row.QueuedAt,
		})
	}
	return out, nil
}

// loadSubmittedItems returns the set of Committed Items that have entered an
// Order, which is what a Charge Allocation's `submitted` flag reports.
func loadSubmittedItems(ctx context.Context, q *sqlc.Queries, itemIDs []uuid.UUID) (
	map[uuid.UUID]struct{}, error,
) {
	out := make(map[uuid.UUID]struct{})
	if len(itemIDs) == 0 {
		return out, nil
	}
	rows, err := q.ListSubmittedCommittedItems(ctx, itemIDs)
	if err != nil {
		return nil, fmt.Errorf("load submitted committed items: %w", err)
	}
	for _, id := range rows {
		out[id] = struct{}{}
	}
	return out, nil
}
```

`nullStringPtr` already exists in this package; if the helper is named differently, use the existing name — grep for how `PreparationNote` is converted in the Charge Allocation assembly and match it. Add `"encoding/json"` to the file's imports.

In `LoadServiceSession`, after the Checks are assembled, call the two loaders and assign the results to `out.Orders` and `out.PreparationUnits`, returning any error unchanged.

In the Charge Allocation assembly (the function that calls `ListCheckAllocations`), call `loadSubmittedItems` with the same `itemIDs` slice already built there, and set each allocation's `Submitted` from the returned set instead of the constant `false`.

- [ ] **Step 6: Write the failing integration assertion**

Append to `internal/sales/projection_integration_test.go`:

```go
func TestProjectionOrdersAndUnitsStartEmpty(t *testing.T) {
	env := newSalesEnv(t)
	session := env.commitTakeawayDraft(t, 2)

	got := env.GetSessionOK(t, session.ID)
	require.NotNil(t, got.Orders)
	require.Empty(t, got.Orders)
	require.NotNil(t, got.PreparationUnits)
	require.Empty(t, got.PreparationUnits)
	require.Len(t, got.Checks, 1)
	for _, allocation := range got.Checks[0].Allocations {
		require.False(t, allocation.Submitted, "nothing is submitted before Submit exists")
	}
}
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/sales/ -run 'TestServiceSessionResponseOrdersAndUnits|TestEmptyOrdersAndUnits' -v`
Expected: PASS

Run: `go test -tags integration ./internal/sales/ -run TestProjectionOrdersAndUnitsStartEmpty -v -p 1`
Expected: PASS

Run: `go build ./... && go test ./internal/sales/`
Expected: PASS — the whole package still compiles with the retyped fields.

- [ ] **Step 8: Commit**

```bash
make fmt
git add sql/queries/sales.sql internal/database/sqlc internal/sales/dto.go internal/sales/dto_test.go internal/sales/projection.go internal/sales/projection_integration_test.go
git commit -m "feat(sales): fill the Order and Preparation Unit projection arrays"
```

---

## Task 5: Submit

**Files:**
- Create: `internal/sales/submit.go`, `internal/sales/submit_integration_test.go`
- Modify: `sql/queries/sales.sql`, `internal/sales/env_integration_test.go`, `internal/sales/http.go`, `internal/sales/routes.go`

**Interfaces:**
- Consumes: Task 2's `OpSubmitOrder`, `EventOrderSubmitted`, `ModeRequiresSettlementBeforeSubmit`, `ErrNothingToSubmit`, `ErrCheckNotSettledForSubmission`; Task 4's `SubmitOrderCommand` and projection.
- Produces: `SubmitOrderHandler`, `NewSubmitOrderHandler(*Runner) *SubmitOrderHandler`, `(*SubmitOrderHandler).Handle(ctx, Actor, SubmitOrderCommand) (int, ServiceSessionResponse, error)`; sqlc queries `LockSubmittableDraft`, `LockChecksForSubmission`, `InsertOrder`, `InsertOrderItem`, `ListCommittedItemsForSubmission`, `InsertPreparationUnit`; env helpers `Submit`, `TrySubmit`, `SubmitWithRequestID`.

- [ ] **Step 1: Add the Submit queries**

Append to `sql/queries/sales.sql`:

```sql
-- name: LockSubmittableDraft :one
-- The Service Session and its committed-but-unsubmitted Order Draft.
--
-- NOT EXISTS rather than the canonical LEFT JOIN ... IS NULL: PostgreSQL
-- refuses row locks across a LEFT JOIN's nullable side, which forces the
-- canonical source to scope FOR UPDATE by hand and explain the workaround in
-- two places. Written this way the restriction does not arise.
SELECT ss.id AS service_session_id, ss.service_number, ss.service_mode,
       od.id AS order_draft_id
FROM service_sessions ss
JOIN order_drafts od ON od.service_session_id = ss.id
WHERE ss.id = $1
  AND ss.state = 'ACTIVE'
  AND od.state = 'COMMITTED'
  AND NOT EXISTS (SELECT 1 FROM orders o WHERE o.order_draft_id = od.id)
FOR UPDATE
LIMIT 1;

-- name: LockChecksForSubmission :many
-- Every distinct Check reachable from the draft's Committed Items, locked in
-- the ascending (created_at, id) order 5C's lock protocol established, so
-- Submit and a concurrent Payment serialize instead of deadlocking.
SELECT DISTINCT c.id, c.state, c.created_at
FROM committed_items ci
JOIN charge_allocations ca ON ca.committed_item_id = ci.id
JOIN checks c ON c.id = ca.check_id
WHERE ci.order_draft_id = $1
ORDER BY c.created_at ASC, c.id ASC
FOR UPDATE OF c;

-- name: InsertOrder :one
INSERT INTO orders (service_session_id, order_draft_id,
                    submitted_by_staff_identity_id,
                    submitted_staff_access_session_id, submitted_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (order_draft_id) DO NOTHING
RETURNING id;

-- name: ListCommittedItemsForSubmission :many
SELECT id, category_name, item_name, size_name, quantity, preparation_note
FROM committed_items
WHERE order_draft_id = $1
ORDER BY committed_at ASC, id ASC;

-- name: InsertOrderItem :one
INSERT INTO order_items (order_id, committed_item_id)
VALUES ($1, $2)
RETURNING id;

-- name: InsertPreparationUnit :exec
INSERT INTO preparation_units (order_item_id, unit_number, service_number,
                               category_name, item_name, size_name,
                               modifiers, preparation_note, queued_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);
```

Run `make sqlc`.

- [ ] **Step 2: Write the failing integration tests**

First add the env helpers. Append to `internal/sales/env_integration_test.go`:

```go
func (e *salesEnv) TrySubmit(t *testing.T, sessionID uuid.UUID) (
	sales.ServiceSessionResponse, int, error,
) {
	t.Helper()
	return e.SubmitWithRequestID(t, uuid.New(), sessionID)
}

func (e *salesEnv) SubmitWithRequestID(t *testing.T, requestID, sessionID uuid.UUID) (
	sales.ServiceSessionResponse, int, error,
) {
	t.Helper()
	status, resp, err := sales.NewSubmitOrderHandler(e.Runner).
		Handle(context.Background(), e.Actor, sales.SubmitOrderCommand{
			RequestID:        requestID,
			ServiceSessionID: sessionID,
		})
	if err != nil {
		status, err = mapErrorStatus(err)
	}
	return resp, status, err
}

func (e *salesEnv) Submit(t *testing.T, sessionID uuid.UUID) sales.ServiceSessionResponse {
	t.Helper()
	got, _, err := e.TrySubmit(t, sessionID)
	require.NoError(t, err)
	return got
}

// SubmitAs runs Submit as another actor, for authorization denial tests.
func (e *salesEnv) SubmitAs(t *testing.T, actor sales.Actor, sessionID uuid.UUID) (
	sales.ServiceSessionResponse, int, error,
) {
	t.Helper()
	status, resp, err := sales.NewSubmitOrderHandler(e.Runner).
		Handle(context.Background(), actor, sales.SubmitOrderCommand{
			RequestID:        uuid.New(),
			ServiceSessionID: sessionID,
		})
	if err != nil {
		status, err = mapErrorStatus(err)
	}
	return resp, status, err
}
```

The env holds no handler aggregate: every helper constructs its handler from `e.Runner` and maps the error through `mapErrorStatus`, exactly as `payCashWithRequestID` does. Follow that shape for every helper this plan adds.

Create `internal/sales/submit_integration_test.go`:

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

func TestSubmitTakeaway(t *testing.T) {
	env := newSalesEnv(t)
	ctx := context.Background()

	t.Run("a settled takeaway check submits", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 2)
		checkID := env.soleCheckID(t, session.ID)
		charge := env.checkCharge(t, checkID)
		_, _, err := env.payCash(t, checkID, charge, charge)
		require.NoError(t, err)

		got := env.Submit(t, session.ID)

		require.Len(t, got.Orders, 1)
		require.Len(t, got.Orders[0].Items, 2)
		require.Equal(t, env.Actor.StaffID, got.Orders[0].SubmittedByStaffID)
		require.NotEmpty(t, got.PreparationUnits)
		for _, allocation := range got.Checks[0].Allocations {
			require.True(t, allocation.Submitted)
		}
	})

	t.Run("an unsettled takeaway check is refused", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 1)

		_, _, err := env.TrySubmit(t, session.ID)
		require.ErrorIs(t, err, sales.ErrCheckNotSettledForSubmission)

		got := env.GetSessionOK(t, session.ID)
		require.Empty(t, got.Orders, "a refused Submit records nothing")
		require.Empty(t, got.PreparationUnits)
	})

	t.Run("a session with no committed draft is refused", func(t *testing.T) {
		session := env.StartTakeaway(t)
		env.SeedEditableDraftWithDefaultTarget(t, session.ID)

		_, _, err := env.TrySubmit(t, session.ID)
		require.ErrorIs(t, err, sales.ErrNothingToSubmit)
	})

	t.Run("submitting twice with a fresh request id is a no-op", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 1)
		checkID := env.soleCheckID(t, session.ID)
		charge := env.checkCharge(t, checkID)
		_, _, err := env.payCash(t, checkID, charge, charge)
		require.NoError(t, err)
		env.Submit(t, session.ID)

		// UNIQUE (order_draft_id) means the draft is no longer submittable,
		// so the second attempt finds nothing awaiting submission.
		_, _, err = env.TrySubmit(t, session.ID)
		require.ErrorIs(t, err, sales.ErrNothingToSubmit)

		var n int
		require.NoError(t, env.DB.QueryRowContext(ctx,
			`SELECT count(*) FROM orders WHERE service_session_id = $1`, session.ID).Scan(&n))
		require.Equal(t, 1, n)
	})

	t.Run("a replayed request id returns the stored result", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 1)
		checkID := env.soleCheckID(t, session.ID)
		charge := env.checkCharge(t, checkID)
		_, _, err := env.payCash(t, checkID, charge, charge)
		require.NoError(t, err)

		requestID := uuid.New()
		first, _, err := env.SubmitWithRequestID(t, requestID, session.ID)
		require.NoError(t, err)
		second, _, err := env.SubmitWithRequestID(t, requestID, session.ID)
		require.NoError(t, err)
		require.Equal(t, first.Orders[0].ID, second.Orders[0].ID)
	})

	t.Run("a barista cannot submit", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 1)
		checkID := env.soleCheckID(t, session.ID)
		charge := env.checkCharge(t, checkID)
		_, _, err := env.payCash(t, checkID, charge, charge)
		require.NoError(t, err)

		_, _, err = env.SubmitAs(t, env.BaristaActor(), session.ID)
		require.ErrorIs(t, err, sales.ErrForbidden)
	})
}

func TestSubmitDineIn(t *testing.T) {
	env := newSalesEnv(t)

	t.Run("an unsettled dine-in check submits", func(t *testing.T) {
		// Dine-in supports Commit -> Submit -> Payment: a seated customer's
		// drinks go to the bar long before the bill is settled. A rule that
		// forced settlement first would make dine-in unusable.
		session := env.commitDineInDraft(t, 2)

		got := env.Submit(t, session.ID)
		require.Len(t, got.Orders, 1)
		require.Equal(t, sales.CheckStateOpen, got.Checks[0].State)
	})

	t.Run("dine-in also supports Commit -> Payment -> Submit", func(t *testing.T) {
		session := env.commitDineInDraft(t, 1)
		checkID := env.soleCheckID(t, session.ID)
		charge := env.checkCharge(t, checkID)
		_, _, err := env.payCash(t, checkID, charge, charge)
		require.NoError(t, err)

		got := env.Submit(t, session.ID)
		require.Len(t, got.Orders, 1)
		require.Equal(t, sales.CheckStateSettled, got.Checks[0].State)
	})
}

func TestSubmitFansOutPreparationUnits(t *testing.T) {
	env := newSalesEnv(t)

	t.Run("quantity three produces three units numbered 1..3", func(t *testing.T) {
		session := env.commitDineInDraftWithQuantity(t, 3)

		got := env.Submit(t, session.ID)

		require.Len(t, got.PreparationUnits, 3)
		numbers := map[int32]bool{}
		for _, unit := range got.PreparationUnits {
			numbers[unit.UnitNumber] = true
			require.Equal(t, sales.UnitStateQueued, unit.State)
			require.Equal(t, got.ServiceNumber, unit.ServiceNumber)
			require.NotEmpty(t, unit.ItemName)
			require.NotNil(t, unit.Modifiers)
		}
		require.Equal(t, map[int32]bool{1: true, 2: true, 3: true}, numbers)
	})
}

func TestSubmitWritesAuditEvent(t *testing.T) {
	env := newSalesEnv(t)
	session := env.commitDineInDraft(t, 1)
	before := env.countAuditEvents(t, sales.EventOrderSubmitted)

	env.Submit(t, session.ID)

	require.Equal(t, before+1, env.countAuditEvents(t, sales.EventOrderSubmitted))
}
```

Add the three dine-in helpers this suite needs to `env_integration_test.go`. `commitDineInDraft(t, n)` mirrors the existing `commitTakeawayDraft(t, n)` but starts with `StartDineIn` against a seeded table; `commitDineInDraftWithQuantity(t, q)` adds one draft item of quantity `q` and commits. Read `commitTakeawayDraft` and follow its structure exactly, substituting the dine-in start.

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test -tags integration ./internal/sales/ -run TestSubmit -v -p 1`
Expected: FAIL to compile — `SubmitOrder` is not a field on the handler aggregate and `SubmitOrderHandler` does not exist.

- [ ] **Step 4: Write the Submit command**

Create `internal/sales/submit.go`:

```go
package sales

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

// SubmitOrderHandler crosses the preparation boundary: it turns a committed
// Order Draft's Committed Items into an Order, its Order Items, and the
// Preparation Units the bar works from, repricing nothing.
type SubmitOrderHandler struct{ runner *Runner }

// NewSubmitOrderHandler creates a SubmitOrderHandler.
func NewSubmitOrderHandler(runner *Runner) *SubmitOrderHandler {
	return &SubmitOrderHandler{runner: runner}
}

// Handle executes Submit.
//
// Submit deliberately does not require an open Sales Shift. Only starting a
// new Order Draft does: a Shift can end while drinks are still being prepared,
// and staff must be able to finish what is already in flight. What the Shift
// rule blocks is opening new work.
func (h *SubmitOrderHandler) Handle(ctx context.Context, actor Actor,
	cmd SubmitOrderCommand,
) (int, ServiceSessionResponse, error) {
	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpSubmitOrder,
		Fingerprint: sessionScopedFingerprint{ServiceSessionID: cmd.ServiceSessionID},
		Required:    []string{CapSalesOperate},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, ServiceSessionResponse, AuditRecord, error) {
			var zero ServiceSessionResponse
			q := mc.Queries

			source, err := q.LockSubmittableDraft(ctx, cmd.ServiceSessionID)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return 0, zero, AuditRecord{}, fmt.Errorf(
						"%w: %s", ErrNothingToSubmit, cmd.ServiceSessionID)
				}
				return 0, zero, AuditRecord{}, fmt.Errorf("lock submittable draft: %w", err)
			}

			checks, err := q.LockChecksForSubmission(ctx, source.OrderDraftID)
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("lock checks for submission: %w", err)
			}
			if len(checks) == 0 {
				return 0, zero, AuditRecord{}, fmt.Errorf(
					"%w: committed draft has no charge", ErrNothingToSubmit)
			}
			if ModeRequiresSettlementBeforeSubmit(source.ServiceMode) {
				for _, check := range checks {
					if check.State != CheckStateSettled {
						return 0, zero, AuditRecord{}, fmt.Errorf(
							"%w: check %s is %s", ErrCheckNotSettledForSubmission,
							check.ID, check.State)
					}
				}
			}

			occurredAt := time.Now()
			orderID, err := q.InsertOrder(ctx, sqlc.InsertOrderParams{
				ServiceSessionID:              source.ServiceSessionID,
				OrderDraftID:                  source.OrderDraftID,
				SubmittedByStaffIdentityID:    actor.StaffID,
				SubmittedStaffAccessSessionID: actor.SessionID,
				SubmittedAt:                   occurredAt,
			})
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					// ON CONFLICT DO NOTHING returned no row: another
					// transaction submitted this draft first. The row lock
					// makes this unreachable; the branch is the backstop.
					return 0, zero, AuditRecord{}, fmt.Errorf(
						"%w: %s", ErrNothingToSubmit, source.OrderDraftID)
				}
				return 0, zero, AuditRecord{}, fmt.Errorf("insert order: %w", err)
			}

			items, err := q.ListCommittedItemsForSubmission(ctx, source.OrderDraftID)
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("list committed items: %w", err)
			}
			itemIDs := make([]uuid.UUID, 0, len(items))
			for _, item := range items {
				itemIDs = append(itemIDs, item.ID)
			}
			modifiers, err := loadUnitModifiers(ctx, q, itemIDs)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}

			unitCount := 0
			for _, item := range items {
				orderItemID, err := q.InsertOrderItem(ctx, sqlc.InsertOrderItemParams{
					OrderID:         orderID,
					CommittedItemID: item.ID,
				})
				if err != nil {
					return 0, zero, AuditRecord{}, fmt.Errorf("insert order item: %w", err)
				}
				mods := modifiers[item.ID]
				if mods == nil {
					mods = make([]UnitModifierResponse, 0)
				}
				encoded, err := json.Marshal(mods)
				if err != nil {
					return 0, zero, AuditRecord{}, fmt.Errorf("encode unit modifiers: %w", err)
				}
				// One unit per unit of ordered quantity: a Committed Item of
				// quantity three is three separately prepared drinks.
				for n := int32(1); n <= item.Quantity; n++ {
					if err := q.InsertPreparationUnit(ctx, sqlc.InsertPreparationUnitParams{
						OrderItemID:     orderItemID,
						UnitNumber:      n,
						ServiceNumber:   source.ServiceNumber,
						CategoryName:    item.CategoryName,
						ItemName:        item.ItemName,
						SizeName:        item.SizeName,
						Modifiers:       encoded,
						PreparationNote: item.PreparationNote,
						QueuedAt:        occurredAt,
					}); err != nil {
						return 0, zero, AuditRecord{}, fmt.Errorf("insert preparation unit: %w", err)
					}
					unitCount++
				}
			}

			out, err := LoadServiceSession(ctx, q, cmd.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}

			checkIDs := make([]uuid.UUID, 0, len(checks))
			for _, check := range checks {
				checkIDs = append(checkIDs, check.ID)
			}
			return http.StatusOK, out, AuditRecord{
				EventType: EventOrderSubmitted,
				Details: map[string]any{
					"service_session_id":     source.ServiceSessionID,
					"order_draft_id":         source.OrderDraftID,
					"order_id":               orderID,
					"check_ids":              checkIDs,
					"order_item_count":       len(items),
					"preparation_unit_count": unitCount,
				},
			}, nil
		})
}

// loadUnitModifiers returns each Committed Item's Modifier Options in the
// order the bar display shows them.
//
// Sorting is by group name then option name under the vi-VN collation, so two
// units of the same configuration are byte-identical on the display and a
// barista comparing two tickets is comparing the same text.
func loadUnitModifiers(ctx context.Context, q *sqlc.Queries, itemIDs []uuid.UUID) (
	map[uuid.UUID][]UnitModifierResponse, error,
) {
	out := make(map[uuid.UUID][]UnitModifierResponse)
	if len(itemIDs) == 0 {
		return out, nil
	}
	rows, err := q.ListCommittedItemModifiers(ctx, itemIDs)
	if err != nil {
		return nil, fmt.Errorf("load committed item modifiers: %w", err)
	}
	for _, row := range rows {
		out[row.CommittedItemID] = append(out[row.CommittedItemID], UnitModifierResponse{
			GroupName:  row.ModifierGroupName,
			OptionName: row.ModifierOptionName,
		})
	}
	c := collate.New(language.Vietnamese)
	for id, mods := range out {
		sort.SliceStable(mods, func(i, j int) bool {
			if g := c.CompareString(mods[i].GroupName, mods[j].GroupName); g != 0 {
				return g < 0
			}
			return c.CompareString(mods[i].OptionName, mods[j].OptionName) < 0
		})
		out[id] = mods
	}
	return out, nil
}
```

Add `"net/http"` and `"sort"` to the imports. `sessionScopedFingerprint` already exists in `draft_rounds.go`; reuse it rather than declaring a second one.

If `golang.org/x/text` is not already a dependency, run `go get golang.org/x/text@latest` and `make tidy`. If the team prefers not to add it, replace the collator with `strings.Compare` and add a comment noting that the ordering then differs from canonical for diacritics — but prefer the collator, because the display text is Vietnamese.

- [ ] **Step 5: Wire the route**

Add to `Slices` in `internal/sales/routes.go`:

```go
	SubmitOrder *SubmitOrderHandler
```

and in `NewSlices`:

```go
		SubmitOrder: NewSubmitOrderHandler(runner),
```

and in `RegisterRoutes`:

```go
	v1.POST("/sales/service-sessions/:id/submit", s.handleSubmitOrder,
		authn.RequireAuth(), authn.RequireCapability(CapSalesOperate))
```

Add to `internal/sales/http.go`:

```go
// handleSubmitOrder godoc
//
//	@Summary		Submit the committed round to the bar
//	@Description	Turns the Service Session's committed Order Draft into an Order, its Order Items, and one Preparation Unit per unit of ordered quantity, repricing nothing. A takeaway Session requires every Check settled first; a dine-in Session does not, because both Commit -> Submit -> Payment and Commit -> Payment -> Submit are valid service.
//	@Tags			sales
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string				true	"Service Session ID"
//	@Param			body	body		SubmitOrderCommand	true	"Submit request"
//	@Success		200		{object}	response.APIResponse{data=ServiceSessionResponse}
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Router			/sales/service-sessions/{id}/submit [post]
func (s *Slices) handleSubmitOrder(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	sessionID, err := parseUUIDParam(c, "id")
	if err != nil {
		return sendError(c, err)
	}
	body, err := bindBody[SubmitOrderCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(body.RequestID); err != nil {
		return sendError(c, err)
	}
	body.ServiceSessionID = sessionID

	status, result, err := s.SubmitOrder.Handle(c.Request().Context(), actor, body)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, result)
}
```

Add a route-table assertion for the new path to `internal/sales/routes_test.go`, following the existing entries there exactly.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test -tags integration ./internal/sales/ -run TestSubmit -v -p 1`
Expected: PASS

Run: `go test ./internal/sales/`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
make fmt
git add sql/queries/sales.sql internal/database/sqlc internal/sales/submit.go internal/sales/submit_integration_test.go internal/sales/env_integration_test.go internal/sales/http.go internal/sales/routes.go internal/sales/routes_test.go
git commit -m "feat(sales): submit a committed round to the bar"
```

---

## Task 6: The Later Order Draft

**Files:**
- Modify: `sql/queries/sales.sql:450-467`, `internal/sales/draft_rounds_integration_test.go`

**Interfaces:**
- Consumes: Task 5's `orders` rows and `Submit` env helper.
- Produces: no new Go symbols — `FindBlockingDraft` changes meaning only.

5B shipped `FindBlockingDraft` blocking on **any** `COMMITTED` draft, with a comment recording that the second clause would become "COMMITTED without a corresponding Order" once 5D introduced the `orders` table, and that relaxing it earlier would have shipped a rule no phase wanted. This task makes exactly that change.

- [ ] **Step 1: Write the failing integration tests**

Append to `internal/sales/draft_rounds_integration_test.go`:

```go
func TestMultipleRounds(t *testing.T) {
	env := newSalesEnv(t)

	t.Run("a submitted round unblocks the next draft", func(t *testing.T) {
		session := env.commitDineInDraft(t, 1)
		env.Submit(t, session.ID)

		_, _, err := env.TryStartNewDraft(t, session.ID)
		require.NoError(t, err)

		env.AddDraftItem(t, session.ID, env.TeaID, nil)
		second := env.Commit(t, session.ID)
		require.Len(t, second.Checks[0].Allocations, 2,
			"the second round joins the session's open Check")

		got := env.Submit(t, session.ID)
		require.Len(t, got.Orders, 2)
	})

	t.Run("an unsubmitted committed round still blocks", func(t *testing.T) {
		// The rule exists to stop staff stacking rounds ahead of the kitchen,
		// not to limit a Service Session to one round.
		session := env.commitDineInDraft(t, 1)

		_, _, err := env.TryStartNewDraft(t, session.ID)
		require.ErrorIs(t, err, sales.ErrNewOrderDraftNotAvailable)
	})

	t.Run("an editable draft still blocks", func(t *testing.T) {
		session := env.StartDineIn(t, env.TableID)
		env.SeedEditableDraftWithDefaultTarget(t, session.ID)

		_, _, err := env.TryStartNewDraft(t, session.ID)
		require.ErrorIs(t, err, sales.ErrNewOrderDraftNotAvailable)
	})
}
```

The env's seeded world is `CoffeeID`, `TeaID`, `SizedItemID` / `LargeSizeID`, and `TableID`; `AddDraftItem` takes `(t, sessionID, itemID, sizeID *uuid.UUID)` and adds one unit, so quantity is set afterwards through `SetDraftItemQuantity`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -tags integration ./internal/sales/ -run TestMultipleRounds -v -p 1`
Expected: FAIL on the first subtest — `TryStartNewDraft` returns `ErrNewOrderDraftNotAvailable` even though the round was submitted, because `FindBlockingDraft` still matches every `COMMITTED` draft.

- [ ] **Step 3: Relax the query**

Replace `FindBlockingDraft` in `sql/queries/sales.sql` with:

```sql
-- name: FindBlockingDraft :one
-- A draft that prevents a new one opening: EDITABLE, or COMMITTED without a
-- corresponding Order.
--
-- 5D added the orders table and completed the second clause as 5B's comment
-- promised. The rule stops staff stacking rounds ahead of the kitchen; it does
-- not limit a Service Session to one round.
--
-- NOT EXISTS rather than a LEFT JOIN, for the reason LockSubmittableDraft
-- gives: PostgreSQL refuses row locks across a LEFT JOIN's nullable side.
SELECT id
FROM order_drafts
WHERE service_session_id = $1
  AND (
        state = 'EDITABLE'
     OR (state = 'COMMITTED'
         AND NOT EXISTS (SELECT 1 FROM orders o WHERE o.order_draft_id = order_drafts.id))
  )
FOR UPDATE
LIMIT 1;
```

Run `make sqlc`. The generated signature does not change, so `draft_rounds.go` needs no edit.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test -tags integration ./internal/sales/ -run 'TestMultipleRounds|TestStartNewOrderDraft' -v -p 1`
Expected: PASS — including the pre-existing 5B suite, whose "second commit is blocked" cases must still pass now that the reason is an unsubmitted Order rather than a missing table.

- [ ] **Step 5: Commit**

```bash
make fmt
git add sql/queries/sales.sql internal/database/sqlc internal/sales/draft_rounds_integration_test.go
git commit -m "feat(sales): unblock the later Order Draft once a round is submitted"
```

---

## Task 7: `internal/preparation` Foundations

**Files:**
- Create: `internal/preparation/domain.go`, `internal/preparation/domain_test.go`, `internal/preparation/errors.go`, `internal/preparation/errors_test.go`, `internal/preparation/executor.go`

**Interfaces:**
- Consumes: `internal/auth`, `internal/database/sqlc`, `internal/response`.
- Produces: `CapPreparationOperate`, `OpAdvanceUnit`, `EventPreparationUnitAdvanced`; states `StateQueued`, `StateInPreparation`, `StateReady`, `StateFulfilled`, `StateCancelled`, `StateWasted`; `NextState(string) (string, bool)`, `IsLegalAdvance(from, to string) bool`, `IsAdvanceTarget(string) bool`; `ErrUnitNotFound`, `ErrInvalidTransition`, `ErrRequestConflict`, `ErrInvalidStoredResult`, `ErrForbidden`, `ErrUnauthorized`, `ErrorResponse`; `Actor`, `Runner`, `NewRunner`, `MutationSpec`, `MutationContext`, `AuditRecord`, `ExecuteMutation[T]`.

- [ ] **Step 1: Write the failing transition-table tests**

Create `internal/preparation/domain_test.go`:

```go
package preparation_test

import (
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/preparation"
	"github.com/stretchr/testify/require"
)

func TestNextState(t *testing.T) {
	cases := []struct {
		from string
		next string
		ok   bool
	}{
		{preparation.StateQueued, preparation.StateInPreparation, true},
		{preparation.StateInPreparation, preparation.StateReady, true},
		{preparation.StateReady, preparation.StateFulfilled, true},
		{preparation.StateFulfilled, "", false},
		{preparation.StateCancelled, "", false},
		{preparation.StateWasted, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.from, func(t *testing.T) {
			next, ok := preparation.NextState(tc.from)
			require.Equal(t, tc.ok, ok)
			require.Equal(t, tc.next, next)
		})
	}
}

func TestIsLegalAdvance(t *testing.T) {
	require.True(t, preparation.IsLegalAdvance(preparation.StateQueued, preparation.StateInPreparation))
	require.True(t, preparation.IsLegalAdvance(preparation.StateInPreparation, preparation.StateReady))
	require.True(t, preparation.IsLegalAdvance(preparation.StateReady, preparation.StateFulfilled))

	// Skipping a step is not an advance.
	require.False(t, preparation.IsLegalAdvance(preparation.StateQueued, preparation.StateReady))
	require.False(t, preparation.IsLegalAdvance(preparation.StateQueued, preparation.StateFulfilled))
	// Going backwards is a State Correction, which is Phase 6.
	require.False(t, preparation.IsLegalAdvance(preparation.StateReady, preparation.StateInPreparation))
	// Terminal states are terminal.
	require.False(t, preparation.IsLegalAdvance(preparation.StateFulfilled, preparation.StateFulfilled))
	// Cancellation and Waste are Phase 6 commands, not advances.
	require.False(t, preparation.IsLegalAdvance(preparation.StateQueued, preparation.StateCancelled))
	require.False(t, preparation.IsLegalAdvance(preparation.StateInPreparation, preparation.StateWasted))
}

func TestIsAdvanceTarget(t *testing.T) {
	for _, s := range []string{preparation.StateInPreparation, preparation.StateReady, preparation.StateFulfilled} {
		require.True(t, preparation.IsAdvanceTarget(s), s)
	}
	for _, s := range []string{preparation.StateQueued, preparation.StateCancelled, preparation.StateWasted, "", "BREWING"} {
		require.False(t, preparation.IsAdvanceTarget(s), s)
	}
}
```

Create `internal/preparation/errors_test.go`:

```go
package preparation_test

import (
	"net/http"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/preparation"
	"github.com/stretchr/testify/require"
)

func TestErrorMapping(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{preparation.ErrUnitNotFound, http.StatusNotFound, "PREPARATION_UNIT_NOT_FOUND"},
		{preparation.ErrInvalidTransition, http.StatusConflict, "INVALID_TRANSITION"},
		{preparation.ErrRequestConflict, http.StatusConflict, "REQUEST_CONFLICT"},
		{preparation.ErrInvalidStoredResult, http.StatusInternalServerError, "INVALID_STORED_RESULT"},
		{preparation.ErrForbidden, http.StatusForbidden, "NOT_AUTHORIZED"},
		{preparation.ErrUnauthorized, http.StatusUnauthorized, "UNAUTHORIZED"},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			status, body := preparation.ErrorResponse(tc.err)
			require.Equal(t, tc.status, status)
			require.Equal(t, tc.code, body.Error.Code)
		})
	}
}
```

Match `internal/sales/errors.go` for the exact status of `INVALID_STORED_RESULT`, `NOT_AUTHORIZED`, and `UNAUTHORIZED` — read its mapping and copy those three verbatim rather than trusting the table above.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/preparation/ -v`
Expected: FAIL — the package does not exist.

- [ ] **Step 3: Write the domain**

Create `internal/preparation/domain.go`:

```go
// Package preparation implements the Preparation vertical slice.
//
// Phase 5D creates it holding exactly one command: advancing a Preparation
// Unit along the linear chain from queued to fulfilled. Cancellation, Waste,
// Remake, State Correction, Preparation Alerts, and the Preparation Queue
// reads are Phase 6 and grow into this package.
//
// The boundary with internal/sales (ADR-024): internal/sales creates
// Preparation Units at Submit and reads their state during closure; this
// package owns every state transition. sqlc generates one shared package, so
// the boundary is enforced in review rather than by the compiler.
package preparation

// CapPreparationOperate is the capability the advance command requires. It is
// already derived for MANAGER and BARISTA by auth.DeriveCapabilities, so a
// Barista can advance units but cannot submit or close, and a Cashier the
// reverse. No capability table change was needed for Phase 5D.
const CapPreparationOperate = "preparation.operate"

// OpAdvanceUnit is the idempotency action name, stored in
// idempotency_keys.action (VARCHAR(50)).
const OpAdvanceUnit = "preparation.advance_unit"

// EventPreparationUnitAdvanced is the audit event type.
const EventPreparationUnitAdvanced = "PREPARATION_UNIT_ADVANCED"

// Preparation Unit states. 5D writes only the first four; StateCancelled and
// StateWasted arrive with Phase 6's commands (ADR-028).
const (
	StateQueued        = "QUEUED"
	StateInPreparation = "IN_PREPARATION"
	StateReady         = "READY"
	StateFulfilled     = "FULFILLED"
	StateCancelled     = "CANCELLED"
	StateWasted        = "WASTED"
)

// advanceChain is the legal graph, taken unchanged from the canonical source.
// It is strictly linear: no step may be skipped, and no state may be left once
// it is terminal.
var advanceChain = map[string]string{
	StateQueued:        StateInPreparation,
	StateInPreparation: StateReady,
	StateReady:         StateFulfilled,
}

// NextState returns the single state that may follow the given one, and
// whether any may.
func NextState(from string) (string, bool) {
	next, ok := advanceChain[from]
	return next, ok
}

// IsLegalAdvance reports whether `to` is the immediate successor of `from`.
func IsLegalAdvance(from, to string) bool {
	next, ok := advanceChain[from]
	return ok && next == to
}

// IsAdvanceTarget reports whether a state can be requested as an advance
// target. Queued is where a unit starts, not somewhere it can be sent;
// Cancelled and Wasted are reached by their own Phase 6 commands.
func IsAdvanceTarget(state string) bool {
	switch state {
	case StateInPreparation, StateReady, StateFulfilled:
		return true
	default:
		return false
	}
}
```

- [ ] **Step 4: Write the errors**

Create `internal/preparation/errors.go` by copying the structure of `internal/sales/errors.go` — the same `coded(...)` helper, the same `ErrorResponse` signature, the same `response.APIResponse` shape — and reducing the sentinel list to:

```go
var (
	ErrUnitNotFound        = errors.New("preparation unit not found")
	ErrInvalidTransition   = errors.New("not a legal advance from the unit's current state")
	ErrRequestConflict     = errors.New("request conflict")
	ErrInvalidStoredResult = errors.New("invalid stored result")
	ErrForbidden           = errors.New("forbidden")
	ErrUnauthorized        = errors.New("unauthorized")
	ErrSessionExpired      = errors.New("staff access session expired")
	ErrSessionRevoked      = errors.New("staff access session revoked")
)
```

Include `ErrSessionExpired` and `ErrSessionRevoked` with the same codes and statuses `internal/sales/errors.go` gives them — `executor.go` in the next step raises them from `reloadAuthority`, so omitting them would leave a security denial mapping to a generic 500.

- [ ] **Step 5: Write the executor**

Copy `internal/sales/executor.go` to `internal/preparation/executor.go` verbatim, then make exactly these changes:

1. Change the package clause to `package preparation`.
2. Change the denial audit event name from `EventAuthorizationDenied`'s value to `"preparation.authorization_denied"`, declared as a constant in this file.
3. Delete `ExecuteRead` if the advance command is the package's only operation — 5D adds no Preparation read. If `go vet` reports it unused, that is the signal to delete it.
4. Leave `Actor`, `Runner`, `NewRunner`, `MutationSpec`, `MutationContext`, `AuditRecord`, `IDToLockKey`, `fpHash`, `reloadAuthority`, `verifyCapabilities`, `recordDenial`, `finishDenial`, `isSecurityDenial`, and `ExecuteMutation` unchanged.

This duplication is deliberate and matches the established convention: `internal/catalog`, `internal/shift`, and `internal/sales` each own a copy. 5D does not introduce a shared executor package, because doing so would restructure three shipped slices in the service of one new command.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/preparation/ -v`
Expected: PASS

Run: `go vet ./internal/preparation/`
Expected: no findings.

- [ ] **Step 7: Verify the package boundary**

Run: `go list -deps ./internal/preparation | Select-String "pos-cafe/internal"`
Expected: `internal/auth`, `internal/database/sqlc`, `internal/response`, and their own dependencies — and **no** `internal/sales`, `internal/catalog`, `internal/tables`, or `internal/shift`.

- [ ] **Step 8: Commit**

```bash
make fmt
git add internal/preparation
git commit -m "feat(preparation): add the Preparation slice foundations"
```

---

## Task 8: Advance A Preparation Unit

**Files:**
- Create: `sql/queries/preparation.sql`, `internal/preparation/dto.go`, `internal/preparation/advance.go`, `internal/preparation/http.go`, `internal/preparation/routes.go`, `internal/preparation/env_integration_test.go`, `internal/preparation/advance_integration_test.go`
- Modify: `cmd/api/main.go`

**Interfaces:**
- Consumes: Task 7's domain, errors, and executor; Task 1's `preparation_units` and `preparation_unit_transitions`.
- Produces: `AdvanceUnitCommand{RequestID, UnitID, TargetState}`, `UnitResponse`, `AdvanceUnitHandler`, `NewAdvanceUnitHandler(*Runner) *AdvanceUnitHandler`, `(*AdvanceUnitHandler).Handle(ctx, Actor, AdvanceUnitCommand) (int, UnitResponse, error)`, `Slices`, `NewSlices(*sql.DB, *sqlc.Queries) *Slices`, `(*Slices).RegisterRoutes(*echo.Group, *auth.Middleware)`; sqlc queries `LockPreparationUnit`, `SetPreparationUnitState`, `InsertPreparationUnitTransition`, `GetPreparationUnit`.

- [ ] **Step 1: Add the queries**

Create `sql/queries/preparation.sql`:

```sql
-- Preparation slice queries.
--
-- Boundary (ADR-024): nothing here writes orders, order_items, or
-- completed_sales. internal/sales creates Preparation Units at Submit and
-- reads their state during closure; this package owns every transition.

-- name: LockPreparationUnit :one
SELECT id, order_item_id, unit_number, state, service_number, category_name,
       item_name, size_name, modifiers, preparation_note, queued_at
FROM preparation_units
WHERE id = $1
FOR UPDATE;

-- name: GetPreparationUnit :one
SELECT id, order_item_id, unit_number, state, service_number, category_name,
       item_name, size_name, modifiers, preparation_note, queued_at
FROM preparation_units
WHERE id = $1;

-- name: SetPreparationUnitState :exec
UPDATE preparation_units SET state = $2 WHERE id = $1;

-- name: InsertPreparationUnitTransition :exec
INSERT INTO preparation_unit_transitions (preparation_unit_id, prior_state,
                                          resulting_state,
                                          actor_staff_identity_id,
                                          staff_access_session_id, occurred_at)
VALUES ($1, $2, $3, $4, $5, $6);
```

Run `make sqlc`.

- [ ] **Step 2: Write the failing integration tests**

Create `internal/preparation/env_integration_test.go`. It needs a seeded Service Session that has reached Submit, which lives in `internal/sales` — a test file may import it, and this one does. Build it like `internal/sales/env_integration_test.go`: open the test database, truncate the Sales and Preparation tables, hold a `sales.Runner` and a `preparation.Runner` over the same pool, seed an open Sales Shift plus a small Catalog, and seed **three** actors — a MANAGER to drive the Sales path, a BARISTA who holds `preparation.operate`, and a CASHIER who does not. The Cashier is what makes the capability denial test meaningful: a Manager holds both capabilities and would prove nothing.

Expose:

```go
// SubmittedUnits returns the Preparation Units of a freshly submitted dine-in
// round of the given quantity.
func (e *prepEnv) SubmittedUnits(t *testing.T, quantity int32) []sales.PreparationUnitResponse

// Advance runs the advance command as the Barista, who holds
// preparation.operate.
func (e *prepEnv) Advance(t *testing.T, unitID uuid.UUID, target string) (
	preparation.UnitResponse, int, error)

// AdvanceAs runs it as an arbitrary actor, for capability tests.
func (e *prepEnv) AdvanceAs(t *testing.T, actor preparation.Actor, unitID uuid.UUID,
	target string) (preparation.UnitResponse, int, error)

// AdvanceWithRequestID replays a specific request id.
func (e *prepEnv) AdvanceWithRequestID(t *testing.T, requestID, unitID uuid.UUID,
	target string) (preparation.UnitResponse, int, error)

// UnitState reads a unit's state straight from the database.
func (e *prepEnv) UnitState(t *testing.T, unitID uuid.UUID) string

// CountTransitions counts the transition rows for a unit.
func (e *prepEnv) CountTransitions(t *testing.T, unitID uuid.UUID) int

// CashierActor holds sales.operate but not preparation.operate.
func (e *prepEnv) CashierActor() preparation.Actor
```

Create `internal/preparation/advance_integration_test.go`:

```go
//go:build integration

package preparation_test

import (
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/preparation"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAdvanceUnit(t *testing.T) {
	env := newPrepEnv(t)

	t.Run("the full chain reaches fulfilled", func(t *testing.T) {
		unit := env.SubmittedUnits(t, 1)[0]
		require.Equal(t, preparation.StateQueued, unit.State)

		for _, target := range []string{
			preparation.StateInPreparation,
			preparation.StateReady,
			preparation.StateFulfilled,
		} {
			got, _, err := env.Advance(t, unit.ID, target)
			require.NoError(t, err)
			require.Equal(t, target, got.State)
		}
		require.Equal(t, preparation.StateFulfilled, env.UnitState(t, unit.ID))
		require.Equal(t, 3, env.CountTransitions(t, unit.ID),
			"one transition row per advance")
	})

	t.Run("skipping a step is refused", func(t *testing.T) {
		unit := env.SubmittedUnits(t, 1)[0]

		_, _, err := env.Advance(t, unit.ID, preparation.StateFulfilled)
		require.ErrorIs(t, err, preparation.ErrInvalidTransition)
		require.Equal(t, preparation.StateQueued, env.UnitState(t, unit.ID))
		require.Equal(t, 0, env.CountTransitions(t, unit.ID))
	})

	t.Run("going backwards is refused", func(t *testing.T) {
		unit := env.SubmittedUnits(t, 1)[0]
		_, _, err := env.Advance(t, unit.ID, preparation.StateInPreparation)
		require.NoError(t, err)

		_, _, err = env.Advance(t, unit.ID, preparation.StateQueued)
		require.ErrorIs(t, err, preparation.ErrInvalidTransition)
	})

	t.Run("a fulfilled unit cannot advance", func(t *testing.T) {
		unit := env.SubmittedUnits(t, 1)[0]
		for _, target := range []string{
			preparation.StateInPreparation,
			preparation.StateReady,
			preparation.StateFulfilled,
		} {
			_, _, err := env.Advance(t, unit.ID, target)
			require.NoError(t, err)
		}
		_, _, err := env.Advance(t, unit.ID, preparation.StateFulfilled)
		require.ErrorIs(t, err, preparation.ErrInvalidTransition)
	})

	t.Run("cancelled and wasted are not advance targets", func(t *testing.T) {
		unit := env.SubmittedUnits(t, 1)[0]
		for _, target := range []string{preparation.StateCancelled, preparation.StateWasted} {
			_, _, err := env.Advance(t, unit.ID, target)
			require.ErrorIs(t, err, preparation.ErrInvalidTransition)
		}
	})

	t.Run("an unknown unit is not found", func(t *testing.T) {
		_, _, err := env.Advance(t, uuid.New(), preparation.StateInPreparation)
		require.ErrorIs(t, err, preparation.ErrUnitNotFound)
	})

	t.Run("units of one item advance independently", func(t *testing.T) {
		units := env.SubmittedUnits(t, 3)
		require.Len(t, units, 3)

		_, _, err := env.Advance(t, units[0].ID, preparation.StateInPreparation)
		require.NoError(t, err)

		require.Equal(t, preparation.StateInPreparation, env.UnitState(t, units[0].ID))
		require.Equal(t, preparation.StateQueued, env.UnitState(t, units[1].ID))
		require.Equal(t, preparation.StateQueued, env.UnitState(t, units[2].ID))
	})

	t.Run("a replayed request id returns the stored result", func(t *testing.T) {
		unit := env.SubmittedUnits(t, 1)[0]
		requestID := uuid.New()

		first, _, err := env.AdvanceWithRequestID(t, requestID, unit.ID, preparation.StateInPreparation)
		require.NoError(t, err)
		second, _, err := env.AdvanceWithRequestID(t, requestID, unit.ID, preparation.StateInPreparation)
		require.NoError(t, err)

		require.Equal(t, first.State, second.State)
		require.Equal(t, 1, env.CountTransitions(t, unit.ID), "a replay records nothing further")
	})

	t.Run("a cashier cannot advance", func(t *testing.T) {
		unit := env.SubmittedUnits(t, 1)[0]
		_, _, err := env.AdvanceAs(t, env.CashierActor(), unit.ID, preparation.StateInPreparation)
		require.ErrorIs(t, err, preparation.ErrForbidden)
	})
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test -tags integration ./internal/preparation/ -run TestAdvanceUnit -v -p 1`
Expected: FAIL to compile — `AdvanceUnitHandler` and `UnitResponse` do not exist.

- [ ] **Step 4: Write the DTOs**

Create `internal/preparation/dto.go`:

```go
package preparation

import (
	"time"

	"github.com/google/uuid"
)

// UnitModifierResponse is one frozen Modifier Option on a Preparation Unit.
// It carries no price: the bar needs to know what to make, not what it cost.
type UnitModifierResponse struct {
	GroupName  string `json:"group_name"`
	OptionName string `json:"option_name"`
}

// UnitResponse is one Preparation Unit as the bar sees it.
type UnitResponse struct {
	ID              uuid.UUID              `json:"id"`
	OrderItemID     uuid.UUID              `json:"order_item_id"`
	UnitNumber      int32                  `json:"unit_number"`
	State           string                 `json:"state"`
	ServiceNumber   string                 `json:"service_number"`
	CategoryName    string                 `json:"category_name"`
	ItemName        string                 `json:"item_name"`
	SizeName        *string                `json:"size_name"`
	Modifiers       []UnitModifierResponse `json:"modifiers"`
	PreparationNote *string                `json:"preparation_note"`
	QueuedAt        time.Time              `json:"queued_at"`
}

// AdvanceUnitCommand moves a Preparation Unit one step along its chain.
//
// TargetState is explicit rather than the command meaning "advance one step".
// A bar display can be seconds stale and two baristas can act on the same unit
// at once; with an explicit target the loser of that race gets
// INVALID_TRANSITION and re-reads, where an implicit "next" would silently
// push the unit one state further than either person intended.
type AdvanceUnitCommand struct {
	RequestID   uuid.UUID `json:"request_id" validate:"required"`
	TargetState string    `json:"target_state" validate:"required"`
	UnitID      uuid.UUID `json:"-"`
}
```

- [ ] **Step 5: Write the advance command**

Create `internal/preparation/advance.go`:

```go
package preparation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

// AdvanceUnitHandler moves one Preparation Unit along the linear chain from
// queued to fulfilled.
type AdvanceUnitHandler struct{ runner *Runner }

// NewAdvanceUnitHandler creates an AdvanceUnitHandler.
func NewAdvanceUnitHandler(runner *Runner) *AdvanceUnitHandler {
	return &AdvanceUnitHandler{runner: runner}
}

// advanceFingerprint is the normalized business input this request stands for.
type advanceFingerprint struct {
	UnitID      uuid.UUID `json:"unit_id"`
	TargetState string    `json:"target_state"`
}

// Handle executes the advance.
func (h *AdvanceUnitHandler) Handle(ctx context.Context, actor Actor,
	cmd AdvanceUnitCommand,
) (int, UnitResponse, error) {
	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpAdvanceUnit,
		Fingerprint: advanceFingerprint{UnitID: cmd.UnitID, TargetState: cmd.TargetState},
		Required:    []string{CapPreparationOperate},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, UnitResponse, AuditRecord, error) {
			var zero UnitResponse
			q := mc.Queries

			if !IsAdvanceTarget(cmd.TargetState) {
				return 0, zero, AuditRecord{}, fmt.Errorf(
					"%w: %q is not an advance target", ErrInvalidTransition, cmd.TargetState)
			}

			unit, err := q.LockPreparationUnit(ctx, cmd.UnitID)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return 0, zero, AuditRecord{}, fmt.Errorf("%w: %s", ErrUnitNotFound, cmd.UnitID)
				}
				return 0, zero, AuditRecord{}, fmt.Errorf("lock preparation unit: %w", err)
			}
			if !IsLegalAdvance(unit.State, cmd.TargetState) {
				return 0, zero, AuditRecord{}, fmt.Errorf(
					"%w: %s cannot advance to %s", ErrInvalidTransition, unit.State, cmd.TargetState)
			}

			occurredAt := time.Now()
			if err := q.SetPreparationUnitState(ctx, sqlc.SetPreparationUnitStateParams{
				ID:    unit.ID,
				State: cmd.TargetState,
			}); err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("set preparation unit state: %w", err)
			}
			// The transition row is business data a Completed Sale is made of,
			// not a derived report (ADR-027). The audit event below records
			// the same moment for a different purpose.
			if err := q.InsertPreparationUnitTransition(ctx,
				sqlc.InsertPreparationUnitTransitionParams{
					PreparationUnitID:    unit.ID,
					PriorState:           unit.State,
					ResultingState:       cmd.TargetState,
					ActorStaffIdentityID: actor.StaffID,
					StaffAccessSessionID: actor.SessionID,
					OccurredAt:           occurredAt,
				}); err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("insert preparation unit transition: %w", err)
			}

			out, err := loadUnit(ctx, q, unit.ID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}

			return http.StatusOK, out, AuditRecord{
				EventType: EventPreparationUnitAdvanced,
				Details: map[string]any{
					"preparation_unit_id": unit.ID,
					"prior_state":         unit.State,
					"resulting_state":     cmd.TargetState,
				},
			}, nil
		})
}

// loadUnit reads one Preparation Unit back after the update.
func loadUnit(ctx context.Context, q *sqlc.Queries, unitID uuid.UUID) (UnitResponse, error) {
	row, err := q.GetPreparationUnit(ctx, unitID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return UnitResponse{}, fmt.Errorf("%w: %s", ErrUnitNotFound, unitID)
		}
		return UnitResponse{}, fmt.Errorf("load preparation unit: %w", err)
	}
	mods := make([]UnitModifierResponse, 0)
	if len(row.Modifiers) > 0 {
		if err := json.Unmarshal(row.Modifiers, &mods); err != nil {
			return UnitResponse{}, fmt.Errorf("decode preparation unit modifiers: %w", err)
		}
	}
	var sizeName, note *string
	if row.SizeName.Valid {
		sizeName = &row.SizeName.String
	}
	if row.PreparationNote.Valid {
		note = &row.PreparationNote.String
	}
	return UnitResponse{
		ID:              row.ID,
		OrderItemID:     row.OrderItemID,
		UnitNumber:      row.UnitNumber,
		State:           row.State,
		ServiceNumber:   row.ServiceNumber,
		CategoryName:    row.CategoryName,
		ItemName:        row.ItemName,
		SizeName:        sizeName,
		Modifiers:       mods,
		PreparationNote: note,
		QueuedAt:        row.QueuedAt,
	}, nil
}
```

- [ ] **Step 6: Wire the routes**

Create `internal/preparation/routes.go` and `internal/preparation/http.go`, following `internal/sales/routes.go` and `internal/sales/http.go` exactly — the same `Slices` shape, the same reason for mounting on `v1` rather than a sub-group, the same `getActor` / `parseUUIDParam` / `bindBody` / `checkRequestID` / `sendError` / `sendResult` helpers copied into this package.

`routes.go`:

```go
// Slices aggregates the Preparation handlers.
type Slices struct {
	Runner *Runner

	AdvanceUnit *AdvanceUnitHandler
}

// NewSlices wires every Preparation handler onto a shared Runner.
func NewSlices(db *sql.DB, queries *sqlc.Queries) *Slices {
	runner := NewRunner(db, queries)
	return &Slices{
		Runner:      runner,
		AdvanceUnit: NewAdvanceUnitHandler(runner),
	}
}

// RegisterRoutes mounts the Preparation routes under /preparation.
//
// Routes are mounted directly on v1 rather than a /preparation sub-group
// carrying RequireAuth, because any echo.Group holding group-level middleware
// also auto-registers two echo_route_not_found catch-all routes. This matches
// internal/sales, internal/tables, and internal/shift.
func (s *Slices) RegisterRoutes(v1 *echo.Group, authn *auth.Middleware) {
	v1.POST("/preparation/units/:unit_id/advance", s.handleAdvanceUnit,
		authn.RequireAuth(), authn.RequireCapability(CapPreparationOperate))
}
```

`http.go` handler:

```go
// handleAdvanceUnit godoc
//
//	@Summary		Advance a Preparation Unit
//	@Description	Moves one unit along the linear chain QUEUED -> IN_PREPARATION -> READY -> FULFILLED. The target state is explicit, so two baristas acting on a stale display get a conflict rather than a silent double advance. Cancellation, Waste, Remake, and State Correction are Phase 6.
//	@Tags			preparation
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			unit_id	path		string				true	"Preparation Unit ID"
//	@Param			body	body		AdvanceUnitCommand	true	"Advance request"
//	@Success		200		{object}	response.APIResponse{data=UnitResponse}
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Router			/preparation/units/{unit_id}/advance [post]
func (s *Slices) handleAdvanceUnit(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	unitID, err := parseUUIDParam(c, "unit_id")
	if err != nil {
		return sendError(c, err)
	}
	body, err := bindBody[AdvanceUnitCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(body.RequestID); err != nil {
		return sendError(c, err)
	}
	body.UnitID = unitID

	status, result, err := s.AdvanceUnit.Handle(c.Request().Context(), actor, body)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, result)
}
```

In `cmd/api/main.go`, construct `preparation.NewSlices(db, queries)` next to the existing `sales.NewSlices(...)` and call its `RegisterRoutes(v1, authn)` alongside the others.

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test -tags integration ./internal/preparation/ -run TestAdvanceUnit -v -p 1`
Expected: PASS

Run: `go build ./... && go test ./internal/preparation/`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
make fmt
git add sql/queries/preparation.sql internal/database/sqlc internal/preparation cmd/api/main.go
git commit -m "feat(preparation): advance a Preparation Unit toward fulfillment"
```

---

## Task 9: Close The Service Session

**Files:**
- Create: `internal/sales/session_close.go`, `internal/sales/session_close_integration_test.go`
- Modify: `sql/queries/sales.sql`, `internal/sales/dto.go`, `internal/sales/env_integration_test.go`, `internal/sales/http.go`, `internal/sales/routes.go`, `internal/sales/routes_test.go`

**Interfaces:**
- Consumes: Task 3's `EvaluateClosureReadiness` and `ClosureReadiness.Err`; Task 4's `CloseServiceSessionCommand`; Task 8's transition rows.
- Produces: `CompletedSaleResponse`, `CompletedSaleCheckResponse`, `PreparationTransitionResponse`, `CloseServiceSessionHandler`, `NewCloseServiceSessionHandler(*Runner) *CloseServiceSessionHandler`, `(*CloseServiceSessionHandler).Handle(ctx, Actor, CloseServiceSessionCommand) (int, CompletedSaleResponse, error)`, `LoadCompletedSale(ctx, *sqlc.Queries, uuid.UUID) (CompletedSaleResponse, error)`; sqlc queries `LockServiceSessionForClosure`, `FindCompletedSaleByServiceSession`, `InsertCompletedSale`, `ListHeldTableAssignments`, `ReleaseTableAssignment`, `CloseServiceSession`, `GetCompletedSale`, `ListSessionPreparationTransitions`.

- [ ] **Step 1: Add the closure queries**

Append to `sql/queries/sales.sql`:

```sql
-- name: LockServiceSessionForClosure :one
SELECT id, state, service_number, service_mode, created_at
FROM service_sessions
WHERE id = $1
FOR UPDATE;

-- name: FindCompletedSaleByServiceSession :one
SELECT id FROM completed_sales WHERE service_session_id = $1;

-- name: InsertCompletedSale :one
INSERT INTO completed_sales (service_session_id, completed_by_staff_identity_id,
                             completed_staff_access_session_id, completed_at)
VALUES ($1, $2, $3, $4)
RETURNING id;

-- name: ListHeldTableAssignments :many
SELECT id, table_id
FROM table_assignments
WHERE service_session_id = $1 AND released_at IS NULL
FOR UPDATE;

-- name: ReleaseTableAssignment :exec
UPDATE table_assignments
SET released_at = $2, released_by_staff_identity_id = $3
WHERE id = $1;

-- name: CloseServiceSession :exec
UPDATE service_sessions SET state = 'CLOSED' WHERE id = $1;

-- name: GetCompletedSale :one
SELECT cs.id, cs.service_session_id, cs.completed_by_staff_identity_id,
       cs.completed_staff_access_session_id, cs.completed_at,
       ss.service_number, ss.service_mode, ss.state AS service_session_state,
       ss.created_at AS service_session_created_at,
       si.display_name AS completed_by_display_name
FROM completed_sales cs
JOIN service_sessions ss ON ss.id = cs.service_session_id
JOIN staff_identities si ON si.id = cs.completed_by_staff_identity_id
WHERE cs.id = $1;

-- name: ListSessionPreparationTransitions :many
SELECT put.id, put.preparation_unit_id, put.prior_state, put.resulting_state,
       put.actor_staff_identity_id, put.staff_access_session_id, put.occurred_at
FROM preparation_unit_transitions put
JOIN preparation_units pu ON pu.id = put.preparation_unit_id
JOIN order_items oi ON oi.id = pu.order_item_id
JOIN orders o ON o.id = oi.order_id
WHERE o.service_session_id = $1
ORDER BY put.occurred_at ASC, put.id ASC;
```

Check the real column names on `table_assignments` and `staff_identities` before running `make sqlc` — grep migration `000008` for `released_at` and `released_by_staff_identity_id`, and migration `000002` for the display-name column, and correct the SQL to match. Then run `make sqlc`.

- [ ] **Step 2: Add the Completed Sale DTOs**

Append to `internal/sales/dto.go`:

```go
// CompletedSaleCheckResponse is one Check as it stood at closure: settled,
// with a zero balance, carrying its Payments and Charge Allocations.
type CompletedSaleCheckResponse struct {
	ID              uuid.UUID                  `json:"id"`
	State           string                     `json:"state"`
	ChargeVND       int64                      `json:"charge_vnd"`
	TotalAppliedVND int64                      `json:"total_applied_vnd"`
	BalanceVND      int64                      `json:"balance_vnd"`
	Payments        []PaymentResponse          `json:"payments"`
	Allocations     []ChargeAllocationResponse `json:"allocations"`
}

// PreparationTransitionResponse is one recorded move of a Preparation Unit.
//
// It reads from preparation_unit_transitions rather than reconstructing the
// history from audit payloads (ADR-027): a Completed Sale is immutable
// content, not a derived report.
type PreparationTransitionResponse struct {
	ID              uuid.UUID `json:"id"`
	UnitID          uuid.UUID `json:"preparation_unit_id"`
	PriorState      string    `json:"prior_state"`
	ResultingState  string    `json:"resulting_state"`
	ActorStaffID    uuid.UUID `json:"actor_staff_identity_id"`
	StaffSessionID  uuid.UUID `json:"staff_access_session_id"`
	OccurredAt      time.Time `json:"occurred_at"`
}

// CompletedSaleResponse is the immutable outcome of a closed Service Session.
type CompletedSaleResponse struct {
	ID                    uuid.UUID                       `json:"id"`
	State                 string                          `json:"state"`
	ServiceSessionID      uuid.UUID                       `json:"service_session_id"`
	ServiceNumber         string                          `json:"service_number"`
	ServiceMode           string                          `json:"service_mode"`
	ServiceSessionState   string                          `json:"service_session_state"`
	ServiceSessionOpenedAt time.Time                      `json:"service_session_opened_at"`
	CompletedByStaffID    uuid.UUID                       `json:"completed_by_staff_identity_id"`
	CompletedBySessionID  uuid.UUID                       `json:"completed_staff_access_session_id"`
	CompletedByName       string                          `json:"completed_by_display_name"`
	CompletedAt           time.Time                       `json:"completed_at"`
	Checks                []CompletedSaleCheckResponse    `json:"checks"`
	Orders                []OrderResponse                 `json:"orders"`
	PreparationUnits      []PreparationUnitResponse       `json:"preparation_units"`
	PreparationHistory    []PreparationTransitionResponse `json:"preparation_history"`
}

// CompletedSaleStateCompleted is the only state a Completed Sale has. It is a
// literal in the contract so a client can branch on it exactly as it branches
// on a Check's or a Session's state.
const CompletedSaleStateCompleted = "COMPLETED"
```

- [ ] **Step 3: Write the failing integration tests**

Add these env helpers to `internal/sales/env_integration_test.go`, each constructing its handler from `e.Runner` and mapping the error through `mapErrorStatus`, exactly as the Task 5 helpers do:

- `CloseWithRequestID(t, requestID, sessionID) (sales.CompletedSaleResponse, int, error)`
- `TryClose(t, sessionID) (sales.CompletedSaleResponse, int, error)` — delegates with a fresh request id
- `CloseAs(t, actor, sessionID) (sales.CompletedSaleResponse, int, error)` — for authorization denial tests
- `Close(t, sessionID) sales.CompletedSaleResponse` — requires no error
- `FulfillAll(t, sessionID)` — advances every Preparation Unit of the Session to `FULFILLED`, three calls per unit

`FulfillAll` reaches across the package boundary into `internal/preparation`, which a test file may do. It runs as the seeded BARISTA (`e.BaristaActor()`), who holds `preparation.operate`; the MANAGER actor holds it too, but using the Barista keeps the test path the same as production. Build a `preparation.Actor` from the same staff and access-session ids, since the two packages declare separate `Actor` types.

Create `internal/sales/session_close_integration_test.go`:

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

// readyToClose runs the complete path: commit, pay, submit, fulfill.
func readyToClose(t *testing.T, env *salesEnv, quantity int32) sales.ServiceSessionResponse {
	t.Helper()
	session := env.commitDineInDraftWithQuantity(t, quantity)
	checkID := env.soleCheckID(t, session.ID)
	charge := env.checkCharge(t, checkID)
	_, _, err := env.payCash(t, checkID, charge, charge)
	require.NoError(t, err)
	env.Submit(t, session.ID)
	env.FulfillAll(t, session.ID)
	return session
}

func TestCloseServiceSession(t *testing.T) {
	env := newSalesEnv(t)
	ctx := context.Background()

	t.Run("an eligible session closes into a completed sale", func(t *testing.T) {
		session := readyToClose(t, env, 2)

		sale, status, err := env.TryClose(t, session.ID)
		require.NoError(t, err)
		require.Equal(t, 201, status)

		require.Equal(t, sales.CompletedSaleStateCompleted, sale.State)
		require.Equal(t, session.ID, sale.ServiceSessionID)
		require.Equal(t, sales.StateClosed, sale.ServiceSessionState)
		require.Equal(t, env.Actor.StaffID, sale.CompletedByStaffID)
		require.NotEmpty(t, sale.CompletedByName)
		require.Len(t, sale.Checks, 1)
		require.Equal(t, int64(0), sale.Checks[0].BalanceVND)
		require.Len(t, sale.Orders, 1)
		require.Len(t, sale.PreparationUnits, 2)
		require.Len(t, sale.PreparationHistory, 6, "three advances per unit")

		var state string
		require.NoError(t, env.DB.QueryRowContext(ctx,
			`SELECT state FROM service_sessions WHERE id = $1`, session.ID).Scan(&state))
		require.Equal(t, sales.StateClosed, state)
	})

	t.Run("closing releases every held table assignment", func(t *testing.T) {
		session := readyToClose(t, env, 1)

		env.Close(t, session.ID)

		var held int
		require.NoError(t, env.DB.QueryRowContext(ctx, `
			SELECT count(*) FROM table_assignments
			WHERE service_session_id = $1 AND released_at IS NULL`, session.ID).Scan(&held))
		require.Equal(t, 0, held)
	})

	t.Run("closing twice returns the same completed sale", func(t *testing.T) {
		session := readyToClose(t, env, 1)

		first := env.Close(t, session.ID)
		second := env.Close(t, session.ID)
		require.Equal(t, first.ID, second.ID)

		var n int
		require.NoError(t, env.DB.QueryRowContext(ctx,
			`SELECT count(*) FROM completed_sales WHERE service_session_id = $1`,
			session.ID).Scan(&n))
		require.Equal(t, 1, n)
	})

	t.Run("a replayed request id returns the stored result", func(t *testing.T) {
		session := readyToClose(t, env, 1)
		requestID := uuid.New()

		first, _, err := env.CloseWithRequestID(t, requestID, session.ID)
		require.NoError(t, err)
		second, _, err := env.CloseWithRequestID(t, requestID, session.ID)
		require.NoError(t, err)
		require.Equal(t, first.ID, second.ID)
	})

	t.Run("an unknown session is not found", func(t *testing.T) {
		_, _, err := env.TryClose(t, uuid.New())
		require.ErrorIs(t, err, sales.ErrServiceSessionNotFound)
	})

	t.Run("a barista cannot close", func(t *testing.T) {
		session := readyToClose(t, env, 1)
		_, _, err := env.CloseAs(t, env.BaristaActor(), session.ID)
		require.ErrorIs(t, err, sales.ErrForbidden)
	})
}

func TestCloseServiceSessionRejections(t *testing.T) {
	env := newSalesEnv(t)

	// Each subtest satisfies every higher-priority condition, so the ordering
	// in ClosureReadiness.Err is genuinely exercised rather than shadowed.

	t.Run("an unsettled check is refused first", func(t *testing.T) {
		session := env.commitDineInDraftWithQuantity(t, 1)
		env.Submit(t, session.ID)
		env.FulfillAll(t, session.ID)

		_, _, err := env.TryClose(t, session.ID)
		require.ErrorIs(t, err, sales.ErrCheckNotSettledForClosure)
	})

	t.Run("unsubmitted work is refused once checks are settled", func(t *testing.T) {
		session := env.commitDineInDraftWithQuantity(t, 1)
		checkID := env.soleCheckID(t, session.ID)
		charge := env.checkCharge(t, checkID)
		_, _, err := env.payCash(t, checkID, charge, charge)
		require.NoError(t, err)

		_, _, err = env.TryClose(t, session.ID)
		require.ErrorIs(t, err, sales.ErrUnsubmittedWorkForClosure)
	})

	t.Run("nonterminal preparation is refused last", func(t *testing.T) {
		session := env.commitDineInDraftWithQuantity(t, 1)
		checkID := env.soleCheckID(t, session.ID)
		charge := env.checkCharge(t, checkID)
		_, _, err := env.payCash(t, checkID, charge, charge)
		require.NoError(t, err)
		env.Submit(t, session.ID)

		_, _, err = env.TryClose(t, session.ID)
		require.ErrorIs(t, err, sales.ErrUnfulfilledPreparationForClosure)
	})

	t.Run("a session with no order at all is refused", func(t *testing.T) {
		// A Session that has never committed has no Check either, so the
		// money condition fires first — this asserts the ordering holds even
		// when the missing Order is the more obvious problem.
		session := env.StartDineIn(t, env.TableID)

		_, _, err := env.TryClose(t, session.ID)
		require.ErrorIs(t, err, sales.ErrCheckNotSettledForClosure)
	})

	t.Run("a refused closure records nothing", func(t *testing.T) {
		session := env.commitDineInDraftWithQuantity(t, 1)

		_, _, err := env.TryClose(t, session.ID)
		require.Error(t, err)

		got := env.GetSessionOK(t, session.ID)
		require.Equal(t, sales.StateActive, got.State)
	})
}
```

- [ ] **Step 4: Run the tests to verify they fail**

Run: `go test -tags integration ./internal/sales/ -run TestCloseServiceSession -v -p 1`
Expected: FAIL to compile — `CloseSession` is not a field on the handler aggregate.

- [ ] **Step 5: Write the closure command**

Create `internal/sales/session_close.go`:

```go
package sales

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

// CloseServiceSessionHandler completes a Service Session into an immutable
// Completed Sale.
type CloseServiceSessionHandler struct{ runner *Runner }

// NewCloseServiceSessionHandler creates a CloseServiceSessionHandler.
func NewCloseServiceSessionHandler(runner *Runner) *CloseServiceSessionHandler {
	return &CloseServiceSessionHandler{runner: runner}
}

// Handle executes closure.
//
// Idempotency runs on the shared executor with T = CompletedSaleResponse
// (ADR-026). The canonical source maintains a dedicated
// completed_sale_closing_requests table only because its helper is typed to
// the Service Session projection; the Go executor is generic, so closure's
// replay and audit behave identically to every other command's.
//
// Like Submit, closure does not require an open Sales Shift: a Shift can end
// while drinks are still being prepared, and staff must be able to finish what
// is already in flight.
func (h *CloseServiceSessionHandler) Handle(ctx context.Context, actor Actor,
	cmd CloseServiceSessionCommand,
) (int, CompletedSaleResponse, error) {
	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpCloseServiceSession,
		Fingerprint: sessionScopedFingerprint{ServiceSessionID: cmd.ServiceSessionID},
		Required:    []string{CapSalesOperate},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, CompletedSaleResponse, AuditRecord, error) {
			var zero CompletedSaleResponse
			q := mc.Queries

			session, err := q.LockServiceSessionForClosure(ctx, cmd.ServiceSessionID)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return 0, zero, AuditRecord{}, fmt.Errorf(
						"%w: %s", ErrServiceSessionNotFound, cmd.ServiceSessionID)
				}
				return 0, zero, AuditRecord{}, fmt.Errorf("lock service session: %w", err)
			}

			// An already-closed Session returns its existing sale rather than
			// an error: closing twice is a duplicate, not a mistake.
			existingID, err := q.FindCompletedSaleByServiceSession(ctx, cmd.ServiceSessionID)
			switch {
			case err == nil:
				out, err := LoadCompletedSale(ctx, q, existingID)
				if err != nil {
					return 0, zero, AuditRecord{}, err
				}
				return http.StatusCreated, out, AuditRecord{}, nil
			case !errors.Is(err, sql.ErrNoRows):
				return 0, zero, AuditRecord{}, fmt.Errorf("find completed sale: %w", err)
			}
			if session.State != StateActive {
				return 0, zero, AuditRecord{}, fmt.Errorf(
					"%w: %s", ErrServiceSessionClosed, cmd.ServiceSessionID)
			}

			projection, err := LoadServiceSession(ctx, q, cmd.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			if err := EvaluateClosureReadiness(projection).Err(); err != nil {
				return 0, zero, AuditRecord{}, err
			}

			completedAt := time.Now()
			saleID, err := q.InsertCompletedSale(ctx, sqlc.InsertCompletedSaleParams{
				ServiceSessionID:              cmd.ServiceSessionID,
				CompletedByStaffIdentityID:    actor.StaffID,
				CompletedStaffAccessSessionID: actor.SessionID,
				CompletedAt:                   completedAt,
			})
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("insert completed sale: %w", err)
			}

			assignments, err := q.ListHeldTableAssignments(ctx, cmd.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("list held table assignments: %w", err)
			}
			for _, assignment := range assignments {
				if err := q.ReleaseTableAssignment(ctx, sqlc.ReleaseTableAssignmentParams{
					ID:                        assignment.ID,
					ReleasedAt:                sql.NullTime{Time: completedAt, Valid: true},
					ReleasedByStaffIdentityID: uuid.NullUUID{UUID: actor.StaffID, Valid: true},
				}); err != nil {
					return 0, zero, AuditRecord{}, fmt.Errorf("release table assignment: %w", err)
				}
			}

			if err := q.CloseServiceSession(ctx, cmd.ServiceSessionID); err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("close service session: %w", err)
			}

			out, err := LoadCompletedSale(ctx, q, saleID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}

			releasedTableIDs := make([]uuid.UUID, 0, len(assignments))
			for _, assignment := range assignments {
				releasedTableIDs = append(releasedTableIDs, assignment.TableID)
			}
			return http.StatusCreated, out, AuditRecord{
				EventType: EventServiceSessionClosed,
				Details: map[string]any{
					"service_session_id": cmd.ServiceSessionID,
					"completed_sale_id":  saleID,
					"released_table_ids": releasedTableIDs,
				},
			}, nil
		})
}
```

Each released Table Assignment also gets its own `TABLE_ASSIGNMENT_RELEASED` audit event in the canonical source. `AuditRecord` carries one event per mutation, so insert the per-assignment events directly with `q.InsertAuditEvent` inside the loop, exactly as the existing Table Assignment release path in `internal/sales/table_assignments.go` does — read that file and reuse its call shape.

- [ ] **Step 6: Write the Completed Sale loader**

Create `internal/sales/completed_sale_reads.go` with `LoadCompletedSale`, which reads the sale row, then assembles its Checks (reusing the same per-Check loaders `LoadServiceSession` uses, mapped into `CompletedSaleCheckResponse`), Orders via `loadOrders`, Preparation Units via `loadPreparationUnits`, and the history via `ListSessionPreparationTransitions`. Initialize all four slices with `make(..., 0)` so they serialize as `[]`.

- [ ] **Step 7: Wire the route**

Add `CloseSession *CloseServiceSessionHandler` to `Slices`, construct it in `NewSlices`, register

```go
	v1.POST("/sales/service-sessions/:id/close", s.handleCloseSession,
		authn.RequireAuth(), authn.RequireCapability(CapSalesOperate))
```

and add `handleCloseSession` to `http.go` following `handleSubmitOrder` exactly, with `@Success 201 {object} response.APIResponse{data=CompletedSaleResponse}` and a `@Description` that names the four closure conditions in their rejection order. Add the route-table assertion to `routes_test.go`.

- [ ] **Step 8: Run the tests to verify they pass**

Run: `go test -tags integration ./internal/sales/ -run TestCloseServiceSession -v -p 1`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
make fmt
git add sql/queries/sales.sql internal/database/sqlc internal/sales/session_close.go internal/sales/completed_sale_reads.go internal/sales/session_close_integration_test.go internal/sales/dto.go internal/sales/env_integration_test.go internal/sales/http.go internal/sales/routes.go internal/sales/routes_test.go
git commit -m "feat(sales): close a Service Session into a Completed Sale"
```

---

## Task 10: Completed Sale Reads

**Files:**
- Create: `internal/sales/completed_sale_integration_test.go`
- Modify: `internal/sales/completed_sale_reads.go`, `sql/queries/sales.sql`, `internal/sales/http.go`, `internal/sales/routes.go`, `internal/sales/routes_test.go`

**Interfaces:**
- Consumes: Task 9's `LoadCompletedSale` and `CompletedSaleResponse`.
- Produces: `GetCompletedSaleHandler`, `GetCompletedSaleBySessionHandler`, their `New...` constructors, and their `Handle(ctx, Actor, uuid.UUID) (int, CompletedSaleResponse, error)` methods; sqlc query `FindCompletedSaleIDByServiceSession`.

- [ ] **Step 1: Write the failing integration tests**

Create `internal/sales/completed_sale_integration_test.go`:

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

func TestCompletedSaleReads(t *testing.T) {
	env := newSalesEnv(t)
	ctx := context.Background()

	t.Run("reads by id and by service session agree", func(t *testing.T) {
		session := readyToClose(t, env, 2)
		closed := env.Close(t, session.ID)

		byID, _, err := env.GetCompletedSale(t, closed.ID)
		require.NoError(t, err)
		bySession, _, err := env.GetCompletedSaleBySession(t, session.ID)
		require.NoError(t, err)

		require.Equal(t, closed.ID, byID.ID)
		require.Equal(t, closed.ID, bySession.ID)
		require.Equal(t, byID, bySession)
	})

	t.Run("preparation history is in occurrence order", func(t *testing.T) {
		session := readyToClose(t, env, 1)
		sale := env.Close(t, session.ID)

		require.Len(t, sale.PreparationHistory, 3)
		require.Equal(t, sales.UnitStateQueued, sale.PreparationHistory[0].PriorState)
		require.Equal(t, sales.UnitStateInPreparation, sale.PreparationHistory[0].ResultingState)
		require.Equal(t, sales.UnitStateReady, sale.PreparationHistory[2].PriorState)
		require.Equal(t, sales.UnitStateFulfilled, sale.PreparationHistory[2].ResultingState)
		for _, transition := range sale.PreparationHistory {
			require.NotEqual(t, uuid.Nil, transition.ActorStaffID)
			require.NotEqual(t, uuid.Nil, transition.StaffSessionID)
		}
	})

	t.Run("an unknown sale is not found", func(t *testing.T) {
		_, _, err := env.GetCompletedSale(t, uuid.New())
		require.ErrorIs(t, err, sales.ErrCompletedSaleNotFound)
	})

	t.Run("an open session has no completed sale", func(t *testing.T) {
		session := env.StartTakeaway(t)
		_, _, err := env.GetCompletedSaleBySession(t, session.ID)
		require.ErrorIs(t, err, sales.ErrCompletedSaleNotFound)
	})
}
```

Add `GetCompletedSale(t, saleID)` and `GetCompletedSaleBySession(t, sessionID)` to the env, each returning `(sales.CompletedSaleResponse, int, error)` and built from `e.Runner` like every other helper.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -tags integration ./internal/sales/ -run TestCompletedSaleReads -v -p 1`
Expected: FAIL to compile — the two read handlers do not exist.

- [ ] **Step 3: Add the lookup query**

Append to `sql/queries/sales.sql`:

```sql
-- name: FindCompletedSaleIDByServiceSession :one
SELECT id FROM completed_sales WHERE service_session_id = $1;
```

This duplicates `FindCompletedSaleByServiceSession` from Task 9 in shape but not in role: that one runs inside the closure transaction, this one serves the read. If you prefer, delete this and reuse the Task 9 query — the generated method is identical. Prefer reusing it; add this query only if the closure one was named for its write-path role in a way that reads badly here.

- [ ] **Step 4: Write the read handlers**

Append to `internal/sales/completed_sale_reads.go` two handlers built on `ExecuteRead`, following the shape of `GetServiceSessionHandler` in this package exactly — same `ExecuteRead` call, same `OpGetCompletedSale` / `OpGetCompletedSaleBySession` operation names, same `CapSalesOperate`. The by-session handler resolves the id first and maps `sql.ErrNoRows` to `ErrCompletedSaleNotFound`; the by-id handler maps the same error from `GetCompletedSale`.

- [ ] **Step 5: Wire the routes**

Add both handlers to `Slices` and `NewSlices`, register

```go
	v1.GET("/sales/completed-sales/:id", s.handleGetCompletedSale,
		authn.RequireAuth(), authn.RequireCapability(CapSalesOperate))
	v1.GET("/sales/service-sessions/:id/completed-sale", s.handleGetCompletedSaleBySession,
		authn.RequireAuth(), authn.RequireCapability(CapSalesOperate))
```

and add both Echo handlers to `http.go` with full Swagger annotations, `@Success 200 {object} response.APIResponse{data=CompletedSaleResponse}` and `@Failure 404`. Add both route-table assertions to `routes_test.go`.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test -tags integration ./internal/sales/ -run TestCompletedSaleReads -v -p 1`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
make fmt
git add sql/queries/sales.sql internal/database/sqlc internal/sales/completed_sale_reads.go internal/sales/completed_sale_integration_test.go internal/sales/http.go internal/sales/routes.go internal/sales/routes_test.go
git commit -m "feat(sales): read a Completed Sale by id and by Service Session"
```

---

## Task 11: Concurrency, Documentation, And The Full Suite

**Files:**
- Create: `internal/sales/submission_concurrency_integration_test.go`
- Modify: `internal/preparation/advance_integration_test.go`, `docs/docs.go` (generated), `MIGRATE_PLAN.md`

**Interfaces:**
- Consumes: everything above.
- Produces: no new Go symbols.

- [ ] **Step 1: Write the concurrency tests**

Create `internal/sales/submission_concurrency_integration_test.go`:

```go
//go:build integration

package sales_test

import (
	"context"
	"sync"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestConcurrentSubmit(t *testing.T) {
	env := newSalesEnv(t)
	ctx := context.Background()
	session := env.commitDineInDraftWithQuantity(t, 1)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, errs[i] = env.TrySubmit(t, session.ID)
		}(i)
	}
	wg.Wait()

	succeeded := 0
	for _, err := range errs {
		if err == nil {
			succeeded++
		} else {
			require.ErrorIs(t, err, sales.ErrNothingToSubmit)
		}
	}
	require.Equal(t, 1, succeeded, "exactly one Submit creates the Order")

	var n int
	require.NoError(t, env.DB.QueryRowContext(ctx,
		`SELECT count(*) FROM orders WHERE service_session_id = $1`, session.ID).Scan(&n))
	require.Equal(t, 1, n, "UNIQUE (order_draft_id) is the backstop")
}

func TestConcurrentClose(t *testing.T) {
	env := newSalesEnv(t)
	ctx := context.Background()
	session := readyToClose(t, env, 1)

	var wg sync.WaitGroup
	sales := make([]uuid.UUID, 2)
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got, _, err := env.TryClose(t, session.ID)
			sales[i], errs[i] = got.ID, err
		}(i)
	}
	wg.Wait()

	for _, err := range errs {
		require.NoError(t, err, "closing twice is a duplicate, not a mistake")
	}
	require.Equal(t, sales[0], sales[1])

	var n int
	require.NoError(t, env.DB.QueryRowContext(ctx,
		`SELECT count(*) FROM completed_sales WHERE service_session_id = $1`,
		session.ID).Scan(&n))
	require.Equal(t, 1, n)
}

func TestSubmitAgainstConcurrentPayment(t *testing.T) {
	// Both take Check locks in ascending (created_at, id) order, so they
	// serialize instead of deadlocking.
	env := newSalesEnv(t)
	session := env.commitDineInDraftWithQuantity(t, 1)
	checkID := env.soleCheckID(t, session.ID)
	charge := env.checkCharge(t, checkID)

	var wg sync.WaitGroup
	wg.Add(2)
	var submitErr, payErr error
	go func() { defer wg.Done(); _, _, submitErr = env.TrySubmit(t, session.ID) }()
	go func() { defer wg.Done(); _, _, payErr = env.payCash(t, checkID, charge, charge) }()
	wg.Wait()

	require.NoError(t, submitErr, "dine-in Submit does not require settlement")
	require.NoError(t, payErr)
}
```

Append to `internal/preparation/advance_integration_test.go`:

```go
func TestConcurrentAdvance(t *testing.T) {
	env := newPrepEnv(t)
	unit := env.SubmittedUnits(t, 1)[0]

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, errs[i] = env.Advance(t, unit.ID, preparation.StateInPreparation)
		}(i)
	}
	wg.Wait()

	succeeded := 0
	for _, err := range errs {
		if err == nil {
			succeeded++
		} else {
			require.ErrorIs(t, err, preparation.ErrInvalidTransition)
		}
	}
	require.Equal(t, 1, succeeded, "the row lock serializes them")
	require.Equal(t, 1, env.CountTransitions(t, unit.ID))
}
```

Add `"sync"` to that file's imports.

- [ ] **Step 2: Run the concurrency tests**

Run: `go test -tags integration ./internal/sales/ -run 'TestConcurrentSubmit|TestConcurrentClose|TestSubmitAgainstConcurrentPayment' -v -p 1`
Expected: PASS

Run: `go test -tags integration ./internal/preparation/ -run TestConcurrentAdvance -v -p 1`
Expected: PASS

- [ ] **Step 3: Regenerate Swagger**

Run: `make swagger`

Verify the generated `docs/docs.go` now documents twenty-two Sales operations and one Preparation operation, each with Bearer security, its request DTO, its response schema, and its error statuses.

- [ ] **Step 4: Update the roadmap**

In `MIGRATE_PLAN.md`, set the 5D row to:

```markdown
| **5D** — Submit, Orders, Preparation Units, Service Session closure, Completed Sale | [spec](docs/superpowers/specs/2026-09-15-sales-submission-closure-design.md) / [plan](docs/superpowers/plans/2026-09-15-sales-submission-closure.md) | ✅ COMPLETED (2026-09-15) |
```

and set the 5C row's status the same way if the 5C branch has landed by then. Update the Phase 5 tracker row to `✅ DONE`, `6 / 6`, with the completion date — the spec's §13 of 5A recorded that the row is marked complete only when 5D lands.

- [ ] **Step 5: Run everything**

Run: `make check`
Expected: fmt clean, `go vet` silent, linter clean, unit tests pass.

Run: `make test-integration`
Expected: every suite passes, including the pre-existing 5A, 5B, and 5C suites.

- [ ] **Step 6: Verify the package boundary once more**

Run: `go list -deps ./internal/preparation | Select-String "pos-cafe/internal/sales"`
Expected: no output. `internal/preparation` must not import `internal/sales`.

- [ ] **Step 7: Commit**

```bash
make fmt
git add internal/sales/submission_concurrency_integration_test.go internal/preparation/advance_integration_test.go docs MIGRATE_PLAN.md
git commit -m "test(sales): cover Phase 5D concurrency and complete the roadmap"
```

---

## Verification Checklist

Run through this before opening the pull request.

- [ ] `make check` passes.
- [ ] `make test-integration` passes with `-p 1`.
- [ ] `go list -deps ./internal/preparation` shows no `internal/sales`.
- [ ] No query in `sql/queries/sales.sql` writes `preparation_units.state` or touches `preparation_unit_transitions`.
- [ ] No query in `sql/queries/preparation.sql` writes `orders`, `order_items`, or `completed_sales`.
- [ ] `order_items` has exactly three columns (ADR-025).
- [ ] `preparation_units.state` declares all six canonical values (ADR-028).
- [ ] No `completed_sale_closing_requests` table exists (ADR-026).
- [ ] `preparation_history` reads from `preparation_unit_transitions`, not from `audit_events` (ADR-027).
- [ ] `ServiceSessionResponse` has the same JSON keys and types it had in 5C, with `orders` and `preparation_units` now populated and `submitted` no longer constant.
- [ ] Every 5D branch is reachable through the public API. **No suite seeds a fixture row to reach a branch** — grep the new integration tests for direct `INSERT` statements and confirm each is an assertion helper, not a path to an otherwise-unreachable state.
- [ ] A takeaway Submit is refused while any Check is unsettled; a dine-in Submit succeeds in both Payment orderings.
- [ ] Closure rejections fire in the order money → work → Order → bar.
- [ ] A Barista can advance but not submit or close; a Cashier the reverse.
