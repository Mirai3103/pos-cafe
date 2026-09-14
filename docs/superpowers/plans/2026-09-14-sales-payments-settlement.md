# Payments, Settlement & Check Restructuring (Phase 5C) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the point at which money arrives — Cash and Manual QR Payments, automatic settlement of a fully paid Check, Split and Merge — and complete the Expected Cash figure Phase 4 shipped partial.

**Architecture:** Extends `internal/sales` in place, reusing 5A's `Runner`, `ExecuteMutation`, and projection assembly, and 5B's `checks` / `charge_allocations` tables and stored-charge invariant. All four new commands share one lock protocol: `checks` `FOR UPDATE` in ascending id order, `service_sessions` and `sales_shifts` `FOR SHARE`. A Payment that zeroes the balance settles its Check in the same transaction. `internal/shift` gains one query and one parameter, and imports no Sales package.

**Tech Stack:** Go 1.26+, Echo v4, PostgreSQL via `jackc/pgx/v5` (stdlib `database/sql` driver), sqlc, `google/uuid`, `stretchr/testify`, swag/OpenAPI 2.0.

**Spec:** [`docs/superpowers/specs/2026-09-14-sales-payments-settlement-design.md`](../specs/2026-09-14-sales-payments-settlement-design.md)

## Global Constraints

Every task's requirements implicitly include this section.

- **Package boundary.** `internal/sales` imports `internal/auth`, `internal/database/sqlc`, `internal/response`, `internal/httpvalidator`. It MUST NOT import `internal/catalog`, `internal/tables`, or `internal/shift`. `internal/shift` MUST NOT import `internal/sales`; it reads `payments` through its own sqlc query. Test files may import anything.
- **Capability.** Every 5C operation requires `sales.operate`. Do not modify the capability table. No 5C operation requires Manager Approval.
- **Idempotency.** Use the shared `idempotency_keys` table (ADR-005, ADR-007). Action names: `sales.pay_cash`, `sales.pay_manual_qr`, `sales.split_check`, `sales.merge_checks`.
- **Audit.** Use the shared `audit_events` table. Business events are `UPPER_SNAKE_CASE`. A settling Payment writes **two** events; a non-settling one writes one.
- **Money.** All monetary columns are `BIGINT`, mapped to Go `int64`. Constraints assert positivity and relational identities only — never a `MAX_SAFE_INTEGER`-derived ceiling (ADR-013). Go guards overflow explicitly, because Go wraps silently.
- **Lock protocol (ADR-016).** All four commands: `checks` `FOR UPDATE` ascending by id, then `service_sessions` and `sales_shifts` `FOR SHARE`. Never `FOR UPDATE` a parent row.
- **Error codes (ADR-018).** One code per condition, not per operation. Field-shape violations are request validation, not domain codes.
- **Settlement (ADR-017).** No settlement command and no settlement route. The condition is `balance == 0`, written as exactly that.
- **Empty collections** serialize as `[]`, never `null`. `sqlc.yaml` already sets `emit_empty_slices: true`.
- **Integration tests** carry `//go:build integration`, live in package `sales_test` (or `shift_test`), and run with `-p 1`.
- **Every task ends with a commit.** Run `make fmt` before committing. Run `make sqlc` after editing any `sql/queries/*.sql` file.

---

## File Structure

**New files in `internal/sales/`:**

| File | Responsibility |
| --- | --- |
| `payments.go` | Both Payment commands on one execution path, plus the settlement transition |
| `payments_test.go` | Unit tests for change due, balance, settlement condition, fingerprints |
| `check_restructuring.go` | Split and Merge handlers |
| `check_restructuring_test.go` | Unit tests for split/merge arithmetic and validation |
| `payments_integration_test.go` | Cash, Manual QR, mixed settlement, rejection suites |
| `check_restructuring_integration_test.go` | Split and Merge suites |
| `payment_concurrency_integration_test.go` | Same-Check, different-Check, and split-vs-payment races |

**Modified files in `internal/sales/`:**

| File | Change |
| --- | --- |
| `domain.go` | Payment methods, split destinations, new operation and event names, `SubtractCharge`, `ChangeDue`, `SettlesCheck`, `ValidateTransactionReference` |
| `domain_test.go` | Unit tests for the new pure functions |
| `errors.go` | Thirteen new sentinels, `ErrSettlementInvariantViolated`, and HTTP status mapping |
| `errors_test.go` | Mapping tests for every new code |
| `dto.go` | Four commands, `SplitDestination`, `SplitItem`, `PaymentResponse`; `CheckResponse.Payments` retyped and `MergedIntoCheckID` added |
| `dto_test.go` | Serialization tests for the new shapes |
| `projection.go` | Payment assembly, real `total_applied_vnd` / `balance_vnd`, settlement read invariant |
| `projection_integration_test.go` | Read-path assertions against seeded payment rows |
| `http.go` | Four Echo handlers with Swagger annotations |
| `routes.go` | Four handlers on `Slices`, four routes |
| `schema_integration_test.go` | Assertions for the completed `checks` and the new `payments` |

**Modified files elsewhere:**

| File | Change |
| --- | --- |
| `internal/database/migrations/000010_create_sales_payment_slice.sql` | Schema (new) |
| `sql/queries/sales.sql` | Payment, settlement, and restructuring queries appended; `ListSessionChecks` extended |
| `sql/queries/shift.sql` | `SumCashPaymentsForShift` appended |
| `internal/shift/domain.go` | `ComputeExpectedCash` gains the Cash Payment parameter |
| `internal/shift/domain_test.go` | Tests for the completed formula |
| `internal/shift/current.go`, `internal/shift/cash_movement.go` | Both call sites pass the new term |
| `internal/shift/http.go` | Swagger description names Refund as the outstanding term |
| `internal/shift/expected_cash_integration_test.go` | New: Cash Payments raise the figure, Manual QR does not |
| `spec/decisions.md` | Append ADR-016 through ADR-020 |
| `MIGRATE_PLAN.md` | 5C spec and plan links in the Phase 5 sub-phase table |

---

## Task 1: Database Schema

**Files:**
- Create: `internal/database/migrations/000010_create_sales_payment_slice.sql`
- Modify: `internal/sales/schema_integration_test.go`

**Interfaces:**
- Consumes: 5B's `checks`, `charge_allocations`; Phase 4's `sales_shifts`; Phase 1's `staff_identities`, `staff_access_sessions`.
- Produces: table `payments`; columns `checks.merged_into_check_id`, `checks.settled_at`, `checks.settled_by_staff_identity_id`, `checks.settled_during_sales_shift_id`, `checks.settled_staff_access_session_id`; constraints `check_settlement_evidence_valid`, `payment_method_valid`, `payment_method_facts_valid`; indexes `payment_check_index`, `payment_cash_shift_index`.

- [ ] **Step 1: Write the failing schema test**

Append to `internal/sales/schema_integration_test.go`:

```go
func TestPaymentSchema(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	t.Run("checks carries all five settlement columns", func(t *testing.T) {
		var n int
		err := db.QueryRowContext(ctx, `
			SELECT count(*) FROM information_schema.columns
			WHERE table_name = 'checks'
			  AND column_name IN ('merged_into_check_id', 'settled_at',
			                      'settled_by_staff_identity_id',
			                      'settled_during_sales_shift_id',
			                      'settled_staff_access_session_id')`).Scan(&n)
		require.NoError(t, err)
		require.Equal(t, 5, n)
	})

	t.Run("settlement evidence constraint covers all three states", func(t *testing.T) {
		var clause string
		err := db.QueryRowContext(ctx, `
			SELECT pg_get_constraintdef(oid) FROM pg_constraint
			WHERE conname = 'check_settlement_evidence_valid'`).Scan(&clause)
		require.NoError(t, err)
		require.Contains(t, clause, "OPEN")
		require.Contains(t, clause, "SETTLED")
		require.Contains(t, clause, "MERGED")
	})

	t.Run("payments enforces the cash and manual QR fact sets", func(t *testing.T) {
		var clause string
		err := db.QueryRowContext(ctx, `
			SELECT pg_get_constraintdef(oid) FROM pg_constraint
			WHERE conname = 'payment_method_facts_valid'`).Scan(&clause)
		require.NoError(t, err)
		require.Contains(t, clause, "cash_tendered_vnd")
		require.Contains(t, clause, "transaction_reference")
	})

	t.Run("payments carries both indexes", func(t *testing.T) {
		var n int
		err := db.QueryRowContext(ctx, `
			SELECT count(*) FROM pg_indexes
			WHERE tablename = 'payments'
			  AND indexname IN ('payment_check_index', 'payment_cash_shift_index')`).Scan(&n)
		require.NoError(t, err)
		require.Equal(t, 2, n)
	})
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -tags integration ./internal/sales/ -run TestPaymentSchema -v -p 1`
Expected: FAIL — the columns and table do not exist yet, so the counts are 0 and the `pg_get_constraintdef` scans return `sql.ErrNoRows`.

- [ ] **Step 3: Write the migration**

Create `internal/database/migrations/000010_create_sales_payment_slice.sql`:

```sql
-- Phase 5C: Payments, Settlement & Check Restructuring.
--
-- Money arrives here. A Payment is applied to a Check; when the balance
-- reaches zero the Check settles in the same transaction, recording who
-- settled it, during which Sales Shift, and on which access session.
-- Refund, Payment Void, and Comp are outside Phase 5.

-- The five settlement columns ADR-014 deferred from migration 000009.
ALTER TABLE checks
    ADD COLUMN IF NOT EXISTS merged_into_check_id            UUID REFERENCES checks(id) ON DELETE RESTRICT,
    ADD COLUMN IF NOT EXISTS settled_at                      TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS settled_by_staff_identity_id    UUID REFERENCES staff_identities(id) ON DELETE RESTRICT,
    ADD COLUMN IF NOT EXISTS settled_during_sales_shift_id   UUID REFERENCES sales_shifts(id) ON DELETE RESTRICT,
    ADD COLUMN IF NOT EXISTS settled_staff_access_session_id UUID REFERENCES staff_access_sessions(id) ON DELETE RESTRICT;

-- Ties the evidence to the state, so a Check that is settled but does not
-- know who settled it is unrepresentable. Settlement evidence is recorded
-- along all four dimensions because this is cash-reconciliation data.
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
);

-- sales_shift_id is stored rather than derived through the Session, because
-- the Shift in which the money reached the cashier is an independent fact:
-- a Session opened in one Shift can be paid in the next. See ADR-019.
--
-- There is deliberately no receipt_observed_in_bank_app column. It is a
-- must-be-true attestation, recorded in the request and the audit event; a
-- column that is true on every row stores nothing.
CREATE TABLE IF NOT EXISTS payments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    check_id UUID NOT NULL REFERENCES checks(id) ON DELETE RESTRICT,
    sales_shift_id UUID NOT NULL REFERENCES sales_shifts(id) ON DELETE RESTRICT,
    actor_staff_identity_id UUID NOT NULL REFERENCES staff_identities(id) ON DELETE RESTRICT,
    staff_access_session_id UUID NOT NULL REFERENCES staff_access_sessions(id) ON DELETE RESTRICT,
    applied_amount_vnd BIGINT NOT NULL,
    method TEXT NOT NULL,
    cash_tendered_vnd BIGINT,
    change_due_vnd BIGINT,
    transaction_reference TEXT,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT payment_method_valid
        CHECK (method IN ('CASH', 'MANUAL_QR')),
    CONSTRAINT payment_method_facts_valid CHECK (
           (method = 'CASH'
                AND cash_tendered_vnd IS NOT NULL
                AND change_due_vnd IS NOT NULL
                AND transaction_reference IS NULL
                AND applied_amount_vnd > 0
                AND cash_tendered_vnd >= applied_amount_vnd
                AND change_due_vnd = cash_tendered_vnd - applied_amount_vnd)
        OR (method = 'MANUAL_QR'
                AND cash_tendered_vnd IS NULL
                AND change_due_vnd IS NULL
                AND applied_amount_vnd > 0
                AND (transaction_reference IS NULL
                     OR (transaction_reference = btrim(transaction_reference)
                         AND char_length(transaction_reference) BETWEEN 1 AND 100)))
    )
);

-- Serves both grouping Payments by Check and their presentation order.
CREATE INDEX IF NOT EXISTS payment_check_index
    ON payments (check_id, received_at, id);

-- Serves internal/shift's Expected Cash sum directly.
CREATE INDEX IF NOT EXISTS payment_cash_shift_index
    ON payments (sales_shift_id) WHERE method = 'CASH';

COMMENT ON TABLE payments IS
    'Owned by internal/sales. Immutable after insert: no phase updates a row. Read by internal/shift for Expected Cash.';
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test -tags integration ./internal/sales/ -run TestPaymentSchema -v -p 1`
Expected: PASS

- [ ] **Step 5: Verify the constraint actually rejects bad evidence**

Append to the same test file:

```go
func TestSettlementEvidenceConstraintRejectsPartialEvidence(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	var sessionID uuid.UUID
	err := db.QueryRowContext(ctx, `SELECT id FROM service_sessions LIMIT 1`).Scan(&sessionID)
	if errors.Is(err, sql.ErrNoRows) {
		t.Skip("no service session fixture available")
	}
	require.NoError(t, err)

	var checkID uuid.UUID
	require.NoError(t, db.QueryRowContext(ctx,
		`INSERT INTO checks (service_session_id, charge_vnd) VALUES ($1, 1000) RETURNING id`,
		sessionID).Scan(&checkID))

	_, err = db.ExecContext(ctx,
		`UPDATE checks SET state = 'SETTLED', settled_at = now() WHERE id = $1`, checkID)
	require.Error(t, err, "SETTLED without the other three evidence columns must be rejected")
	require.Contains(t, err.Error(), "check_settlement_evidence_valid")

	_, err = db.ExecContext(ctx,
		`UPDATE checks SET state = 'MERGED', merged_into_check_id = $1 WHERE id = $1`, checkID)
	require.Error(t, err, "MERGED with a non-zero charge must be rejected")
}
```

Run: `go test -tags integration ./internal/sales/ -run TestSettlementEvidence -v -p 1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
make fmt
git add internal/database/migrations/000010_create_sales_payment_slice.sql internal/sales/schema_integration_test.go
git commit -m "feat(sales): add Phase 5C payment and settlement schema"
```

---

## Task 2: Domain Constants And Pure Functions

**Files:**
- Modify: `internal/sales/domain.go`
- Test: `internal/sales/domain_test.go`

**Interfaces:**
- Consumes: 5B's `AddCharge(totalVND, deltaVND int64) (int64, error)`, `LineTotal(quantity int32, unitPriceVND int64) (int64, error)`, `ErrCheckChargeOutOfRange`.
- Produces:
  - Constants `PaymentMethodCash = "CASH"`, `PaymentMethodManualQR = "MANUAL_QR"`, `SplitDestinationNewCheck = "NEW_CHECK"`, `SplitDestinationExistingCheck = "EXISTING_CHECK"`, `MaxTransactionReferenceLength = 100`.
  - Operations `OpPayCash`, `OpPayManualQR`, `OpSplitCheck`, `OpMergeChecks`.
  - Events `EventCashPaymentRecorded`, `EventManualQRPaymentRecorded`, `EventCheckSettled`, `EventCheckSplit`, `EventCheckMerged`.
  - `SubtractCharge(totalVND, deltaVND int64) (int64, error)`
  - `ChangeDue(tenderedVND, appliedVND int64) (int64, error)`
  - `SettlesCheck(balanceVND int64) bool`
  - `ValidateTransactionReference(ref *string) (*string, error)`

- [ ] **Step 1: Write the failing tests**

Append to `internal/sales/domain_test.go`:

```go
func TestSubtractCharge(t *testing.T) {
	t.Run("subtracts within range", func(t *testing.T) {
		got, err := sales.SubtractCharge(85_000, 25_000)
		require.NoError(t, err)
		require.Equal(t, int64(60_000), got)
	})

	t.Run("reaching zero is allowed", func(t *testing.T) {
		got, err := sales.SubtractCharge(85_000, 85_000)
		require.NoError(t, err)
		require.Equal(t, int64(0), got)
	})

	t.Run("a negative result is rejected", func(t *testing.T) {
		_, err := sales.SubtractCharge(85_000, 85_001)
		require.ErrorIs(t, err, sales.ErrCheckChargeOutOfRange)
	})

	t.Run("underflow is rejected rather than wrapped", func(t *testing.T) {
		_, err := sales.SubtractCharge(math.MinInt64+1, 10)
		require.ErrorIs(t, err, sales.ErrCheckChargeOutOfRange)
	})
}

func TestChangeDue(t *testing.T) {
	t.Run("exact tender leaves no change", func(t *testing.T) {
		got, err := sales.ChangeDue(85_000, 85_000)
		require.NoError(t, err)
		require.Equal(t, int64(0), got)
	})

	t.Run("over-tender returns the difference", func(t *testing.T) {
		got, err := sales.ChangeDue(100_000, 85_000)
		require.NoError(t, err)
		require.Equal(t, int64(15_000), got)
	})

	t.Run("under-tender is rejected", func(t *testing.T) {
		_, err := sales.ChangeDue(80_000, 85_000)
		require.ErrorIs(t, err, sales.ErrInsufficientCashTendered)
	})
}

func TestSettlesCheck(t *testing.T) {
	require.True(t, sales.SettlesCheck(0))
	require.False(t, sales.SettlesCheck(1))
	require.False(t, sales.SettlesCheck(85_000))
}

func TestValidateTransactionReference(t *testing.T) {
	t.Run("nil stays nil", func(t *testing.T) {
		got, err := sales.ValidateTransactionReference(nil)
		require.NoError(t, err)
		require.Nil(t, got)
	})

	t.Run("surrounding whitespace is trimmed", func(t *testing.T) {
		in := "  FT24012345  "
		got, err := sales.ValidateTransactionReference(&in)
		require.NoError(t, err)
		require.NotNil(t, got)
		require.Equal(t, "FT24012345", *got)
	})

	t.Run("empty after trimming becomes nil", func(t *testing.T) {
		in := "   "
		got, err := sales.ValidateTransactionReference(&in)
		require.NoError(t, err)
		require.Nil(t, got)
	})

	t.Run("one hundred characters is accepted", func(t *testing.T) {
		in := strings.Repeat("A", 100)
		got, err := sales.ValidateTransactionReference(&in)
		require.NoError(t, err)
		require.Equal(t, 100, len(*got))
	})

	t.Run("one hundred and one characters is rejected", func(t *testing.T) {
		in := strings.Repeat("A", 101)
		_, err := sales.ValidateTransactionReference(&in)
		require.ErrorIs(t, err, response.ErrInvalid)
	})
}
```

Add `"math"`, `"strings"`, and `"github.com/Mirai3103/pos-cafe/internal/response"` to the file's imports if they are not present.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/sales/ -run 'TestSubtractCharge|TestChangeDue|TestSettlesCheck|TestValidateTransactionReference' -v`
Expected: FAIL — `undefined: sales.SubtractCharge` and the other three.

- [ ] **Step 3: Add the constants and functions**

Append to the constant blocks in `internal/sales/domain.go`, alongside the existing operation and event names:

```go
const (
	OpPayCash     = "sales.pay_cash"
	OpPayManualQR = "sales.pay_manual_qr"
	OpSplitCheck  = "sales.split_check"
	OpMergeChecks = "sales.merge_checks"
)

const (
	EventCashPaymentRecorded     = "CASH_PAYMENT_RECORDED"
	EventManualQRPaymentRecorded = "MANUAL_QR_PAYMENT_RECORDED"
	EventCheckSettled            = "CHECK_SETTLED"
	EventCheckSplit              = "CHECK_SPLIT"
	EventCheckMerged             = "CHECK_MERGED"
)

// Payment methods. The canonical domain has exactly these two; Card is named
// in MIGRATE_PLAN's superseded sketch but exists nowhere in the canonical
// model, so it is not declared.
const (
	PaymentMethodCash     = "CASH"
	PaymentMethodManualQR = "MANUAL_QR"
)

// Split destinations.
const (
	SplitDestinationNewCheck      = "NEW_CHECK"
	SplitDestinationExistingCheck = "EXISTING_CHECK"
)

// MaxTransactionReferenceLength bounds a Manual QR Payment's bank reference.
const MaxTransactionReferenceLength = 100
```

Append the functions:

```go
// SubtractCharge reduces a running charge, refusing to go negative.
//
// Go's integer arithmetic wraps silently, so money arithmetic that does not
// check is money arithmetic that can produce a positive total out of an
// underflow. The check is one comparison.
func SubtractCharge(totalVND, deltaVND int64) (int64, error) {
	result := totalVND - deltaVND
	if deltaVND > 0 && result > totalVND {
		return 0, fmt.Errorf("%w: subtracting %d from %d underflows",
			ErrCheckChargeOutOfRange, deltaVND, totalVND)
	}
	if result < 0 {
		return 0, fmt.Errorf("%w: subtracting %d from %d is negative",
			ErrCheckChargeOutOfRange, deltaVND, totalVND)
	}
	return result, nil
}

// ChangeDue is the cash handed back: tendered less applied.
//
// A cashier cannot hand back money they were not given, so under-tender is a
// business rejection rather than an arithmetic one.
func ChangeDue(tenderedVND, appliedVND int64) (int64, error) {
	if tenderedVND < appliedVND {
		return 0, fmt.Errorf("%w: tendered %d is below applied %d",
			ErrInsufficientCashTendered, tenderedVND, appliedVND)
	}
	return tenderedVND - appliedVND, nil
}

// SettlesCheck reports whether a resulting balance closes the Check.
//
// Refund and customer excess do not exist in Phase 5, so the canonical
// three-input readiness policy reduces to exactly this. See ADR-017.
func SettlesCheck(balanceVND int64) bool { return balanceVND == 0 }

// ValidateTransactionReference trims a Manual QR bank reference and bounds it.
// A reference that is empty after trimming is treated as absent, matching the
// canonical `command.transactionReference?.trim() || null`.
func ValidateTransactionReference(ref *string) (*string, error) {
	if ref == nil {
		return nil, nil
	}
	trimmed := strings.TrimSpace(*ref)
	if trimmed == "" {
		return nil, nil
	}
	if len([]rune(trimmed)) > MaxTransactionReferenceLength {
		return nil, fmt.Errorf("%w: transaction_reference is %d characters, maximum is %d",
			response.ErrInvalid, len([]rune(trimmed)), MaxTransactionReferenceLength)
	}
	return &trimmed, nil
}
```

Add `"strings"` and `"github.com/Mirai3103/pos-cafe/internal/response"` to `domain.go`'s imports.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/sales/ -run 'TestSubtractCharge|TestChangeDue|TestSettlesCheck|TestValidateTransactionReference' -v`
Expected: PASS. `ErrInsufficientCashTendered` does not exist yet — Task 3 adds it, so add a temporary declaration in `errors.go` now as part of this task:

```go
ErrInsufficientCashTendered = errors.New("cash tendered is below the applied amount")
```

- [ ] **Step 5: Commit**

```bash
make fmt
git add internal/sales/domain.go internal/sales/domain_test.go internal/sales/errors.go
git commit -m "feat(sales): add Phase 5C payment domain constants and arithmetic"
```

---

## Task 3: Error Sentinels And HTTP Mapping

**Files:**
- Modify: `internal/sales/errors.go`
- Test: `internal/sales/errors_test.go`

**Interfaces:**
- Consumes: `coded(status int, code string, err error) *response.CodedError` from `errors.go`.
- Produces: sentinels `ErrCheckNotFound`, `ErrCheckNotOpen`, `ErrCheckHasPayment`, `ErrChecksDifferentSession`, `ErrPaymentExceedsBalance`, `ErrInsufficientCashTendered`, `ErrManualQRReceiptRequired`, `ErrInvalidCheckSplit`, `ErrSplitAllocationNotFound`, `ErrSplitQuantityExceedsAllocation`, `ErrSplitSourceWouldBeEmpty`, `ErrSplitDestinationWouldBeEmpty`, `ErrInvalidCheckMerge`, `ErrSettlementInvariantViolated`.

- [ ] **Step 1: Write the failing mapping tests**

Append to `internal/sales/errors_test.go`:

```go
func TestMapHTTPErrorPhase5C(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"check not found", sales.ErrCheckNotFound, http.StatusNotFound, "CHECK_NOT_FOUND"},
		{"check not open", sales.ErrCheckNotOpen, http.StatusConflict, "CHECK_NOT_OPEN"},
		{"check has payment", sales.ErrCheckHasPayment, http.StatusConflict, "CHECK_HAS_PAYMENT"},
		{"different session", sales.ErrChecksDifferentSession, http.StatusConflict, "CHECKS_DIFFERENT_SERVICE_SESSION"},
		{"over balance", sales.ErrPaymentExceedsBalance, http.StatusConflict, "PAYMENT_EXCEEDS_CHECK_BALANCE"},
		{"under tender", sales.ErrInsufficientCashTendered, http.StatusConflict, "INSUFFICIENT_CASH_TENDERED"},
		{"receipt required", sales.ErrManualQRReceiptRequired, http.StatusConflict, "MANUAL_QR_RECEIPT_CONFIRMATION_REQUIRED"},
		{"invalid split", sales.ErrInvalidCheckSplit, http.StatusConflict, "INVALID_CHECK_SPLIT"},
		{"allocation missing", sales.ErrSplitAllocationNotFound, http.StatusConflict, "SPLIT_ALLOCATION_NOT_FOUND"},
		{"quantity exceeds", sales.ErrSplitQuantityExceedsAllocation, http.StatusConflict, "SPLIT_QUANTITY_EXCEEDS_ALLOCATION"},
		{"source empty", sales.ErrSplitSourceWouldBeEmpty, http.StatusConflict, "SPLIT_SOURCE_WOULD_BE_EMPTY"},
		{"destination empty", sales.ErrSplitDestinationWouldBeEmpty, http.StatusConflict, "SPLIT_DESTINATION_WOULD_BE_EMPTY"},
		{"invalid merge", sales.ErrInvalidCheckMerge, http.StatusConflict, "INVALID_CHECK_MERGE"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var coded *response.CodedError
			require.ErrorAs(t, sales.MapHTTPError(fmt.Errorf("wrapped: %w", tc.err)), &coded)
			require.Equal(t, tc.status, coded.Status)
			require.Equal(t, tc.code, coded.Code)
		})
	}
}

// The settlement invariant is a defect, not a business state. It must reach
// the client as an unmapped 500, exactly as the charge invariant does.
func TestSettlementInvariantIsNotMapped(t *testing.T) {
	err := sales.MapHTTPError(fmt.Errorf("wrapped: %w", sales.ErrSettlementInvariantViolated))
	var coded *response.CodedError
	require.False(t, errors.As(err, &coded),
		"a settlement invariant violation must not become a client-visible code")
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/sales/ -run 'TestMapHTTPErrorPhase5C|TestSettlementInvariant' -v`
Expected: FAIL — `undefined: sales.ErrCheckNotFound` and the rest.

- [ ] **Step 3: Add the sentinels**

Append to the `var (...)` block in `internal/sales/errors.go` (keeping the `ErrInsufficientCashTendered` line Task 2 added, rather than duplicating it):

```go
	ErrCheckNotFound          = errors.New("check not found")
	ErrCheckNotOpen           = errors.New("check is not open")
	ErrCheckHasPayment        = errors.New("check already carries a payment")
	ErrChecksDifferentSession = errors.New("checks belong to different service sessions")

	ErrPaymentExceedsBalance   = errors.New("payment exceeds the check balance")
	ErrManualQRReceiptRequired = errors.New("the bank receipt must be confirmed before recording a manual QR payment")

	ErrInvalidCheckSplit              = errors.New("invalid check split")
	ErrSplitAllocationNotFound        = errors.New("committed item is not allocated to the source check")
	ErrSplitQuantityExceedsAllocation = errors.New("split quantity exceeds the allocated quantity")
	ErrSplitSourceWouldBeEmpty        = errors.New("the split would empty the source check")
	ErrSplitDestinationWouldBeEmpty   = errors.New("the split would leave the destination check empty")
	ErrInvalidCheckMerge              = errors.New("invalid check merge")

	// ErrSettlementInvariantViolated reports that a Check's state disagrees
	// with its balance — SETTLED with money owed, or OPEN with none. That is
	// a defect, not a business state, so it is deliberately absent from
	// MapHTTPError and surfaces as a 500 with the Check id logged. It is the
	// read-path half of the pair guarding settlement; the database constraint
	// check_settlement_evidence_valid is the other half.
	ErrSettlementInvariantViolated = errors.New("check state does not match its balance")
```

- [ ] **Step 4: Add the status mapping**

Insert into `MapHTTPError`'s switch in `internal/sales/errors.go`, immediately before the `ErrLineTotalOutOfRange` case:

```go
	case errors.Is(err, ErrCheckNotFound):
		return coded(http.StatusNotFound, "CHECK_NOT_FOUND", ErrCheckNotFound)
	case errors.Is(err, ErrCheckNotOpen):
		return coded(http.StatusConflict, "CHECK_NOT_OPEN", ErrCheckNotOpen)
	case errors.Is(err, ErrCheckHasPayment):
		return coded(http.StatusConflict, "CHECK_HAS_PAYMENT", ErrCheckHasPayment)
	case errors.Is(err, ErrChecksDifferentSession):
		return coded(http.StatusConflict, "CHECKS_DIFFERENT_SERVICE_SESSION", ErrChecksDifferentSession)
	case errors.Is(err, ErrPaymentExceedsBalance):
		return coded(http.StatusConflict, "PAYMENT_EXCEEDS_CHECK_BALANCE", ErrPaymentExceedsBalance)
	case errors.Is(err, ErrInsufficientCashTendered):
		return coded(http.StatusConflict, "INSUFFICIENT_CASH_TENDERED", ErrInsufficientCashTendered)
	case errors.Is(err, ErrManualQRReceiptRequired):
		return coded(http.StatusConflict, "MANUAL_QR_RECEIPT_CONFIRMATION_REQUIRED", ErrManualQRReceiptRequired)
	case errors.Is(err, ErrInvalidCheckSplit):
		return coded(http.StatusConflict, "INVALID_CHECK_SPLIT", ErrInvalidCheckSplit)
	case errors.Is(err, ErrSplitAllocationNotFound):
		return coded(http.StatusConflict, "SPLIT_ALLOCATION_NOT_FOUND", ErrSplitAllocationNotFound)
	case errors.Is(err, ErrSplitQuantityExceedsAllocation):
		return coded(http.StatusConflict, "SPLIT_QUANTITY_EXCEEDS_ALLOCATION", ErrSplitQuantityExceedsAllocation)
	case errors.Is(err, ErrSplitSourceWouldBeEmpty):
		return coded(http.StatusConflict, "SPLIT_SOURCE_WOULD_BE_EMPTY", ErrSplitSourceWouldBeEmpty)
	case errors.Is(err, ErrSplitDestinationWouldBeEmpty):
		return coded(http.StatusConflict, "SPLIT_DESTINATION_WOULD_BE_EMPTY", ErrSplitDestinationWouldBeEmpty)
	case errors.Is(err, ErrInvalidCheckMerge):
		return coded(http.StatusConflict, "INVALID_CHECK_MERGE", ErrInvalidCheckMerge)
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/sales/ -run 'TestMapHTTPErrorPhase5C|TestSettlementInvariant' -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
make fmt
git add internal/sales/errors.go internal/sales/errors_test.go
git commit -m "feat(sales): add Phase 5C error codes keyed by condition"
```

---

## Task 4: Payment DTOs And Projection

**Files:**
- Modify: `internal/sales/dto.go`, `internal/sales/projection.go`, `sql/queries/sales.sql`
- Test: `internal/sales/dto_test.go`, `internal/sales/projection_integration_test.go`

**Interfaces:**
- Consumes: `CheckResponse` from 5B; `loadCheckAllocations`; `AddCharge`; `SubtractCharge`; `SettlesCheck`.
- Produces:
  - `PaymentResponse{ID uuid.UUID; Method string; AppliedAmountVND int64; CashTenderedVND *int64; ChangeDueVND *int64; TransactionReference *string; SalesShiftID uuid.UUID; ReceivedAt time.Time}`
  - `CheckResponse.Payments []PaymentResponse` (retyped from `[]struct{}`), `CheckResponse.MergedIntoCheckID *uuid.UUID`
  - sqlc `ListCheckPayments(ctx, checkID) ([]ListCheckPaymentsRow, error)`; `ListSessionChecks` extended with `merged_into_check_id`

- [ ] **Step 1: Write the failing DTO test**

Append to `internal/sales/dto_test.go`:

```go
func TestPaymentResponseSerialization(t *testing.T) {
	t.Run("a cash payment carries tendered and change", func(t *testing.T) {
		tendered, change := int64(100_000), int64(15_000)
		b, err := json.Marshal(sales.PaymentResponse{
			ID:               uuid.MustParse("11111111-1111-1111-1111-111111111111"),
			Method:           sales.PaymentMethodCash,
			AppliedAmountVND: 85_000,
			CashTenderedVND:  &tendered,
			ChangeDueVND:     &change,
			SalesShiftID:     uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		})
		require.NoError(t, err)
		require.Contains(t, string(b), `"cash_tendered_vnd":100000`)
		require.Contains(t, string(b), `"change_due_vnd":15000`)
		require.NotContains(t, string(b), "transaction_reference")
	})

	t.Run("a manual QR payment carries no cash fields", func(t *testing.T) {
		ref := "FT24012345"
		b, err := json.Marshal(sales.PaymentResponse{
			Method:               sales.PaymentMethodManualQR,
			AppliedAmountVND:     85_000,
			TransactionReference: &ref,
		})
		require.NoError(t, err)
		require.Contains(t, string(b), `"transaction_reference":"FT24012345"`)
		require.NotContains(t, string(b), "cash_tendered_vnd")
		require.NotContains(t, string(b), "change_due_vnd")
	})
}

func TestCheckResponseSerialization5C(t *testing.T) {
	t.Run("an open check omits merged_into_check_id", func(t *testing.T) {
		b, err := json.Marshal(sales.CheckResponse{
			State:      sales.CheckStateOpen,
			ChargeVND:  85_000,
			BalanceVND: 85_000,
			Payments:   []sales.PaymentResponse{},
		})
		require.NoError(t, err)
		require.NotContains(t, string(b), "merged_into_check_id")
		require.Contains(t, string(b), `"payments":[]`)
		require.NotContains(t, string(b), "pending_refund_vnd")
	})

	t.Run("a merged check carries merged_into_check_id", func(t *testing.T) {
		into := uuid.MustParse("33333333-3333-3333-3333-333333333333")
		b, err := json.Marshal(sales.CheckResponse{
			State:             sales.CheckStateMerged,
			MergedIntoCheckID: &into,
			Payments:          []sales.PaymentResponse{},
		})
		require.NoError(t, err)
		require.Contains(t, string(b), `"merged_into_check_id":"33333333-3333-3333-3333-333333333333"`)
	})
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/sales/ -run 'TestPaymentResponseSerialization|TestCheckResponseSerialization5C' -v`
Expected: FAIL — `undefined: sales.PaymentResponse`, and `CheckResponse` has no `MergedIntoCheckID`.

- [ ] **Step 3: Add the DTOs**

In `internal/sales/dto.go`, add `PaymentResponse` and replace the placeholder fields on `CheckResponse`:

```go
// PaymentResponse is one confirmed receipt of money applied to a Check.
//
// The method-dependent fields are pointers with omitempty, so a Manual QR
// Payment does not carry two null cash fields and a Cash Payment does not
// carry a null bank reference.
type PaymentResponse struct {
	ID                   uuid.UUID `json:"id"`
	Method               string    `json:"method"`
	AppliedAmountVND     int64     `json:"applied_amount_vnd"`
	CashTenderedVND      *int64    `json:"cash_tendered_vnd,omitempty"`
	ChangeDueVND         *int64    `json:"change_due_vnd,omitempty"`
	TransactionReference *string   `json:"transaction_reference,omitempty"`
	SalesShiftID         uuid.UUID `json:"sales_shift_id"`
	ReceivedAt           time.Time `json:"received_at"`
}

// CheckResponse is a grouping of charges awaiting settlement.
//
// TotalAppliedVND is the sum of the Check's Payments and BalanceVND is
// ChargeVND minus it; both carry real values from 5C. MergedIntoCheckID is
// present only on a MERGED Check — an open Check does not carry a field
// pointing nowhere. PendingRefundVND is deliberately absent: Refund is
// outside Phase 5 entirely, and a Payment can never exceed the balance.
type CheckResponse struct {
	ID                uuid.UUID  `json:"id"`
	State             string     `json:"state"`
	ChargeVND         int64      `json:"charge_vnd"`
	TotalAppliedVND   int64      `json:"total_applied_vnd"`
	BalanceVND        int64      `json:"balance_vnd"`
	MergedIntoCheckID *uuid.UUID `json:"merged_into_check_id,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`

	Payments    []PaymentResponse          `json:"payments"`
	Allocations []ChargeAllocationResponse `json:"allocations"`
}
```

- [ ] **Step 4: Add the queries**

Append to `sql/queries/sales.sql`:

```sql
-- name: ListCheckPayments :many
-- Ordered by (received_at, id), served directly by payment_check_index.
SELECT id, method, applied_amount_vnd, cash_tendered_vnd, change_due_vnd,
       transaction_reference, sales_shift_id, received_at
FROM payments
WHERE check_id = $1
ORDER BY received_at ASC, id ASC;
```

Replace the existing `ListSessionChecks` body so the projection can read the merge link:

```sql
-- name: ListSessionChecks :many
SELECT id, state, charge_vnd, merged_into_check_id, created_at
FROM checks
WHERE service_session_id = $1
ORDER BY created_at ASC, id ASC;
```

Run: `make sqlc`

- [ ] **Step 5: Populate the projection**

In `internal/sales/projection.go`, replace the body of the `loadChecks` loop that built the 5B placeholder:

```go
	out := make([]CheckResponse, 0, len(checkRows))
	for _, row := range checkRows {
		allocations, allocatedVND, err := loadCheckAllocations(ctx, q, row.ID)
		if err != nil {
			return nil, err
		}
		if allocatedVND != row.ChargeVnd {
			slog.Error("check charge does not match its allocations",
				"check_id", row.ID,
				"stored_charge_vnd", row.ChargeVnd,
				"allocated_vnd", allocatedVND)
			return nil, fmt.Errorf("%w: check %s", ErrChargeInvariantViolated, row.ID)
		}

		payments, totalAppliedVND, err := loadCheckPayments(ctx, q, row.ID)
		if err != nil {
			return nil, err
		}
		balanceVND, err := SubtractCharge(row.ChargeVnd, totalAppliedVND)
		if err != nil {
			return nil, fmt.Errorf("check %s balance: %w", row.ID, err)
		}

		// The read-path half of the settlement guard. The database constraint
		// guarantees that a SETTLED Check carries complete evidence; this
		// guarantees that its state matches the money. A MERGED Check is
		// exempt: its charge and allocations moved to the survivor.
		if row.State != CheckStateMerged &&
			(row.State == CheckStateSettled) != SettlesCheck(balanceVND) {
			slog.Error("check state does not match its balance",
				"check_id", row.ID, "state", row.State, "balance_vnd", balanceVND)
			return nil, fmt.Errorf("%w: check %s", ErrSettlementInvariantViolated, row.ID)
		}
		if row.State == CheckStateMerged && !row.MergedIntoCheckID.Valid {
			slog.Error("merged check has no surviving check", "check_id", row.ID)
			return nil, fmt.Errorf("%w: merged check %s", ErrSettlementInvariantViolated, row.ID)
		}

		check := CheckResponse{
			ID:              row.ID,
			State:           row.State,
			ChargeVND:       row.ChargeVnd,
			TotalAppliedVND: totalAppliedVND,
			BalanceVND:      balanceVND,
			CreatedAt:       row.CreatedAt,
			Payments:        payments,
			Allocations:     allocations,
		}
		if row.MergedIntoCheckID.Valid {
			into := row.MergedIntoCheckID.UUID
			check.MergedIntoCheckID = &into
		}
		out = append(out, check)
	}
	return out, nil
}

// loadCheckPayments returns one Check's Payments and their summed applied
// amount, in (received_at, id) order.
func loadCheckPayments(ctx context.Context, q *sqlc.Queries, checkID uuid.UUID) (
	[]PaymentResponse, int64, error,
) {
	rows, err := q.ListCheckPayments(ctx, checkID)
	if err != nil {
		return nil, 0, fmt.Errorf("load check payments: %w", err)
	}

	out := make([]PaymentResponse, 0, len(rows))
	var totalAppliedVND int64
	for _, row := range rows {
		totalAppliedVND, err = AddCharge(totalAppliedVND, row.AppliedAmountVnd)
		if err != nil {
			return nil, 0, fmt.Errorf("sum payments of check %s: %w", checkID, err)
		}
		payment := PaymentResponse{
			ID:               row.ID,
			Method:           row.Method,
			AppliedAmountVND: row.AppliedAmountVnd,
			SalesShiftID:     row.SalesShiftID,
			ReceivedAt:       row.ReceivedAt,
		}
		if row.CashTenderedVnd.Valid {
			v := row.CashTenderedVnd.Int64
			payment.CashTenderedVND = &v
		}
		if row.ChangeDueVnd.Valid {
			v := row.ChangeDueVnd.Int64
			payment.ChangeDueVND = &v
		}
		if row.TransactionReference.Valid {
			v := row.TransactionReference.String
			payment.TransactionReference = &v
		}
		out = append(out, payment)
	}
	return out, totalAppliedVND, nil
}
```

- [ ] **Step 6: Run the unit tests to verify they pass**

Run: `go test ./internal/sales/ -run 'TestPaymentResponseSerialization|TestCheckResponseSerialization5C' -v && go build ./...`
Expected: PASS, and the package builds.

- [ ] **Step 7: Write the projection integration test**

Append to `internal/sales/projection_integration_test.go`:

```go
// The projection must report a directly inserted Payment, and must refuse to
// serve a Check whose state contradicts its balance.
func TestProjectionReportsPaymentsAndGuardsSettlement(t *testing.T) {
	env := newSalesEnv(t)
	ctx := context.Background()

	session := env.commitTakeawayDraft(t, 2) // two items, one OPEN Check
	checkID := env.soleCheckID(t, session.ID)
	charge := env.checkCharge(t, checkID)

	t.Run("a partial payment lowers the balance and stays OPEN", func(t *testing.T) {
		env.insertCashPayment(t, checkID, charge/2)

		got := env.getSession(t, session.ID)
		require.Len(t, got.Checks, 1)
		require.Equal(t, sales.CheckStateOpen, got.Checks[0].State)
		require.Equal(t, charge/2, got.Checks[0].TotalAppliedVND)
		require.Equal(t, charge-charge/2, got.Checks[0].BalanceVND)
		require.Len(t, got.Checks[0].Payments, 1)
		require.Equal(t, sales.PaymentMethodCash, got.Checks[0].Payments[0].Method)
		require.NotNil(t, got.Checks[0].Payments[0].CashTenderedVND)
	})

	t.Run("a state that contradicts the balance fails the read", func(t *testing.T) {
		_, err := env.db.ExecContext(ctx, `
			UPDATE checks SET state = 'SETTLED', settled_at = now(),
			  settled_by_staff_identity_id = $2,
			  settled_during_sales_shift_id = $3,
			  settled_staff_access_session_id = $4
			WHERE id = $1`,
			checkID, env.cashierIdentityID, env.shiftID, env.cashierSessionID)
		require.NoError(t, err)

		_, err = env.getSessionErr(t, session.ID)
		require.ErrorIs(t, err, sales.ErrSettlementInvariantViolated)
	})
}
```

If `newSalesEnv` does not yet expose `commitTakeawayDraft`, `soleCheckID`, `checkCharge`, `insertCashPayment`, `getSessionErr`, `cashierIdentityID`, `shiftID`, or `cashierSessionID`, add them to `env_integration_test.go` following the helpers 5B already defines there. `insertCashPayment` is a direct `INSERT` used only by read-path tests:

```go
func (e *salesEnv) insertCashPayment(t *testing.T, checkID uuid.UUID, appliedVND int64) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	require.NoError(t, e.db.QueryRowContext(context.Background(), `
		INSERT INTO payments (check_id, sales_shift_id, actor_staff_identity_id,
		                      staff_access_session_id, applied_amount_vnd, method,
		                      cash_tendered_vnd, change_due_vnd)
		VALUES ($1, $2, $3, $4, $5, 'CASH', $5, 0) RETURNING id`,
		checkID, e.shiftID, e.cashierIdentityID, e.cashierSessionID, appliedVND).Scan(&id))
	return id
}
```

- [ ] **Step 8: Run the integration test**

Run: `go test -tags integration ./internal/sales/ -run TestProjectionReportsPaymentsAndGuardsSettlement -v -p 1`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
make fmt
git add internal/sales/dto.go internal/sales/dto_test.go internal/sales/projection.go internal/sales/projection_integration_test.go internal/sales/env_integration_test.go sql/queries/sales.sql internal/database/sqlc
git commit -m "feat(sales): populate Check payments and settlement fields in the projection"
```

---

## Task 5: Cash Payment

**Files:**
- Create: `internal/sales/payments.go`, `internal/sales/payments_test.go`, `internal/sales/payments_integration_test.go`
- Modify: `internal/sales/dto.go`, `internal/sales/http.go`, `internal/sales/routes.go`, `sql/queries/sales.sql`

**Interfaces:**
- Consumes: `ExecuteMutation`, `MutationSpec`, `AuditRecord`, `LoadServiceSession`, `ChangeDue`, `SubtractCharge`, `AddCharge`, `SettlesCheck`, and the Task 3 sentinels.
- Produces:
  - `PayCashCommand{RequestID uuid.UUID; CheckID uuid.UUID; AppliedAmountVND int64; CashTenderedVND int64}`
  - `PayCashHandler` with `Handle(ctx, actor, cmd) (int, ServiceSessionResponse, error)`
  - `lockedCheck` struct and `lockCheckForMutation(ctx, q, checkID) (lockedCheck, error)` — reused by Tasks 6, 7, 8
  - `checkBalance(ctx, q, checkID, storedChargeVND int64) (int64, error)` — reused by Task 6
  - sqlc `LockCheckForPayment`, `InsertPayment`, `SettleCheck`

- [ ] **Step 1: Write the failing unit tests**

Create `internal/sales/payments_test.go`:

```go
package sales_test

import (
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPayCashFingerprintIsStable(t *testing.T) {
	checkID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	requestID := uuid.MustParse("55555555-5555-5555-5555-555555555555")

	first := sales.PayCashFingerprintFor(sales.PayCashCommand{
		RequestID: requestID, CheckID: checkID,
		AppliedAmountVND: 85_000, CashTenderedVND: 100_000,
	})
	second := sales.PayCashFingerprintFor(sales.PayCashCommand{
		RequestID: requestID, CheckID: checkID,
		AppliedAmountVND: 85_000, CashTenderedVND: 100_000,
	})
	require.Equal(t, first, second)

	different := sales.PayCashFingerprintFor(sales.PayCashCommand{
		RequestID: requestID, CheckID: checkID,
		AppliedAmountVND: 85_001, CashTenderedVND: 100_000,
	})
	require.NotEqual(t, first, different,
		"a different applied amount must be a different request")
}

func TestValidateCashAmounts(t *testing.T) {
	require.NoError(t, sales.ValidateCashAmounts(85_000, 100_000))
	require.Error(t, sales.ValidateCashAmounts(0, 100_000))
	require.Error(t, sales.ValidateCashAmounts(-1, 100_000))
	require.Error(t, sales.ValidateCashAmounts(85_000, 0))
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/sales/ -run 'TestPayCashFingerprint|TestValidateCashAmounts' -v`
Expected: FAIL — `undefined: sales.PayCashCommand`.

- [ ] **Step 3: Add the queries**

Append to `sql/queries/sales.sql`:

```sql
-- name: LockCheckForPayment :one
-- The uniform 5C lock protocol (ADR-016): the Check row FOR UPDATE, its
-- parents FOR SHARE. The parents are only read to evaluate a precondition, so
-- locking them FOR UPDATE would serialize two cashiers paying different
-- Checks of one Session for no correctness gain.
--
-- No row means the Check id does not exist. The state columns come back
-- unfiltered so the caller can report which precondition failed.
SELECT c.id, c.state, c.charge_vnd,
       s.id AS service_session_id, s.state AS service_session_state,
       sh.id AS sales_shift_id, sh.state AS sales_shift_state
FROM checks c
JOIN service_sessions s ON s.id = c.service_session_id
JOIN sales_shifts sh ON sh.id = s.sales_shift_id
WHERE c.id = $1
FOR UPDATE OF c
FOR SHARE OF s, sh;

-- name: SumCheckPayments :one
SELECT COALESCE(SUM(applied_amount_vnd), 0)::BIGINT AS total_applied_vnd
FROM payments
WHERE check_id = $1;

-- name: InsertPayment :one
INSERT INTO payments (
    check_id, sales_shift_id, actor_staff_identity_id, staff_access_session_id,
    applied_amount_vnd, method, cash_tendered_vnd, change_due_vnd,
    transaction_reference, received_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING id;

-- name: SettleCheck :exec
-- Writes all four evidence columns together, because the composite constraint
-- check_settlement_evidence_valid rejects any partial set.
UPDATE checks
SET state = 'SETTLED',
    settled_at = $2,
    settled_by_staff_identity_id = $3,
    settled_during_sales_shift_id = $4,
    settled_staff_access_session_id = $5
WHERE id = $1;
```

Run: `make sqlc`

- [ ] **Step 4: Write the shared payment path and the cash command**

Create `internal/sales/payments.go`:

```go
package sales

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
)

// lockedCheck is a Check acquired under the uniform 5C lock protocol, with
// the parent state its caller needs to evaluate preconditions.
type lockedCheck struct {
	ID               uuid.UUID
	State            string
	ChargeVND        int64
	ServiceSessionID uuid.UUID
	SalesShiftID     uuid.UUID
}

// lockCheckForMutation takes the Check FOR UPDATE and its parents FOR SHARE,
// then reports the first failing precondition.
//
// The lock query and the precondition evaluation are deliberately separate.
// The canonical source folds every condition into one WHERE clause and reports
// a single code for an empty result; a cashier told which of the four is wrong
// can act, and told only "check not open" cannot.
func lockCheckForMutation(ctx context.Context, q *sqlc.Queries, checkID uuid.UUID) (
	lockedCheck, error,
) {
	var zero lockedCheck
	row, err := q.LockCheckForPayment(ctx, checkID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return zero, fmt.Errorf("%w: check %s", ErrCheckNotFound, checkID)
		}
		return zero, fmt.Errorf("lock check for mutation: %w", err)
	}
	if row.ServiceSessionState != SessionStateActive {
		return zero, fmt.Errorf("%w: session %s", ErrServiceSessionClosed, row.ServiceSessionID)
	}
	if row.SalesShiftState != ShiftStateOpen {
		return zero, fmt.Errorf("%w: check %s", ErrOpenShiftRequired, checkID)
	}
	if row.State != CheckStateOpen {
		return zero, fmt.Errorf("%w: check %s is %s", ErrCheckNotOpen, checkID, row.State)
	}
	return lockedCheck{
		ID:               row.ID,
		State:            row.State,
		ChargeVND:        row.ChargeVnd,
		ServiceSessionID: row.ServiceSessionID,
		SalesShiftID:     row.SalesShiftID,
	}, nil
}

// checkBalance verifies the stored charge against the live allocation sum and
// returns what is still owed.
//
// The charge comparison is 5B's invariant: stored charge_vnd is a
// denormalization, and a disagreement is a defect rather than a business
// state, so it fails the request with a logged 500.
func checkBalance(ctx context.Context, q *sqlc.Queries, checkID uuid.UUID,
	storedChargeVND int64,
) (int64, error) {
	_, allocatedVND, err := loadCheckAllocations(ctx, q, checkID)
	if err != nil {
		return 0, err
	}
	if allocatedVND != storedChargeVND {
		return 0, fmt.Errorf("%w: check %s stored %d, allocated %d",
			ErrChargeInvariantViolated, checkID, storedChargeVND, allocatedVND)
	}
	appliedVND, err := q.SumCheckPayments(ctx, checkID)
	if err != nil {
		return 0, fmt.Errorf("sum check payments: %w", err)
	}
	return SubtractCharge(storedChargeVND, appliedVND)
}

// paymentInput is the method-specific part of one Payment.
type paymentInput struct {
	Method               string
	AppliedAmountVND     int64
	CashTenderedVND      *int64
	ChangeDueVND         *int64
	TransactionReference *string
	AuditEvent           string
	AuditDetails         func(paymentID uuid.UUID, check lockedCheck) any
}

// recordPayment is the one execution path both Payment commands take.
//
// Settlement is a consequence, not a command: when this Payment brings the
// balance to zero the same transaction settles the Check and emits a second
// audit event. See ADR-017.
func recordPayment(ctx context.Context, q *sqlc.Queries, actor Actor,
	checkID uuid.UUID, in paymentInput,
) (ServiceSessionResponse, AuditRecord, error) {
	var zero ServiceSessionResponse

	check, err := lockCheckForMutation(ctx, q, checkID)
	if err != nil {
		return zero, AuditRecord{}, err
	}

	balanceVND, err := checkBalance(ctx, q, check.ID, check.ChargeVND)
	if err != nil {
		return zero, AuditRecord{}, err
	}
	if in.AppliedAmountVND > balanceVND {
		return zero, AuditRecord{}, fmt.Errorf("%w: applying %d to a balance of %d",
			ErrPaymentExceedsBalance, in.AppliedAmountVND, balanceVND)
	}

	receivedAt := time.Now()
	paymentID, err := q.InsertPayment(ctx, sqlc.InsertPaymentParams{
		CheckID:              check.ID,
		SalesShiftID:         check.SalesShiftID,
		ActorStaffIdentityID: actor.StaffID,
		StaffAccessSessionID: actor.SessionID,
		AppliedAmountVnd:     in.AppliedAmountVND,
		Method:               in.Method,
		CashTenderedVnd:      nullInt64(in.CashTenderedVND),
		ChangeDueVnd:         nullInt64(in.ChangeDueVND),
		TransactionReference: nullString(in.TransactionReference),
		ReceivedAt:           receivedAt,
	})
	if err != nil {
		return zero, AuditRecord{}, fmt.Errorf("insert payment: %w", err)
	}

	remainingVND, err := SubtractCharge(balanceVND, in.AppliedAmountVND)
	if err != nil {
		return zero, AuditRecord{}, err
	}
	if SettlesCheck(remainingVND) {
		if err := q.SettleCheck(ctx, sqlc.SettleCheckParams{
			ID:                          check.ID,
			SettledAt:                   sql.NullTime{Time: receivedAt, Valid: true},
			SettledByStaffIdentityID:    uuid.NullUUID{UUID: actor.StaffID, Valid: true},
			SettledDuringSalesShiftID:   uuid.NullUUID{UUID: check.SalesShiftID, Valid: true},
			SettledStaffAccessSessionID: uuid.NullUUID{UUID: actor.SessionID, Valid: true},
		}); err != nil {
			return zero, AuditRecord{}, fmt.Errorf("settle check: %w", err)
		}
		// Payment and settlement are two distinct facts. ExecuteMutation
		// writes the one it is handed, so the settlement event is written
		// here, inside the same transaction.
		settledDetails, err := marshalAuditDetails(checkSettledAudit{
			CheckID:      check.ID,
			PaymentID:    paymentID,
			SalesShiftID: check.SalesShiftID,
		})
		if err != nil {
			return zero, AuditRecord{}, err
		}
		if _, err := q.InsertAuditEvent(ctx, sqlc.InsertAuditEventParams{
			EventType:  EventCheckSettled,
			ActorID:    uuid.NullUUID{UUID: actor.StaffID, Valid: true},
			SessionID:  uuid.NullUUID{UUID: actor.SessionID, Valid: true},
			Details:    settledDetails,
			OccurredAt: receivedAt,
		}); err != nil {
			return zero, AuditRecord{}, fmt.Errorf("insert check settled audit event: %w", err)
		}
	}

	result, err := LoadServiceSession(ctx, q, check.ServiceSessionID)
	if err != nil {
		return zero, AuditRecord{}, err
	}
	return result, AuditRecord{
		EventType: in.AuditEvent,
		Details:   in.AuditDetails(paymentID, check),
	}, nil
}

type checkSettledAudit struct {
	CheckID      uuid.UUID `json:"check_id"`
	PaymentID    uuid.UUID `json:"payment_id"`
	SalesShiftID uuid.UUID `json:"sales_shift_id"`
}

type cashPaymentAudit struct {
	PaymentID        uuid.UUID `json:"payment_id"`
	CheckID          uuid.UUID `json:"check_id"`
	SalesShiftID     uuid.UUID `json:"sales_shift_id"`
	AppliedAmountVND int64     `json:"applied_amount_vnd"`
	CashTenderedVND  int64     `json:"cash_tendered_vnd"`
	ChangeDueVND     int64     `json:"change_due_vnd"`
}

type payCashFingerprint struct {
	CheckID          uuid.UUID `json:"check_id"`
	AppliedAmountVND int64     `json:"applied_amount_vnd"`
	CashTenderedVND  int64     `json:"cash_tendered_vnd"`
}

// PayCashFingerprintFor exposes the normalized business fingerprint so tests
// can pin its stability.
func PayCashFingerprintFor(cmd PayCashCommand) any {
	return payCashFingerprint{
		CheckID:          cmd.CheckID,
		AppliedAmountVND: cmd.AppliedAmountVND,
		CashTenderedVND:  cmd.CashTenderedVND,
	}
}

// ValidateCashAmounts rejects amounts that cannot describe a cash payment.
// These are field-shape rules, so they map to INVALID_INPUT rather than a
// domain code (ADR-018).
func ValidateCashAmounts(appliedVND, tenderedVND int64) error {
	if appliedVND <= 0 {
		return fmt.Errorf("%w: applied_amount_vnd must be positive", response.ErrInvalid)
	}
	if tenderedVND <= 0 {
		return fmt.Errorf("%w: cash_tendered_vnd must be positive", response.ErrInvalid)
	}
	return nil
}

// PayCashHandler records a Cash Payment against a Check.
type PayCashHandler struct{ runner *Runner }

// NewPayCashHandler creates a new PayCashHandler.
func NewPayCashHandler(runner *Runner) *PayCashHandler {
	return &PayCashHandler{runner: runner}
}

// Handle applies cash to a Check and settles it when nothing is left owed.
func (h *PayCashHandler) Handle(ctx context.Context, actor Actor, cmd PayCashCommand) (
	int, ServiceSessionResponse, error,
) {
	var zero ServiceSessionResponse
	if err := ValidateCashAmounts(cmd.AppliedAmountVND, cmd.CashTenderedVND); err != nil {
		return 0, zero, err
	}
	changeDueVND, err := ChangeDue(cmd.CashTenderedVND, cmd.AppliedAmountVND)
	if err != nil {
		return 0, zero, err
	}

	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpPayCash,
		Fingerprint: PayCashFingerprintFor(cmd),
		Required:    []string{CapSalesOperate},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, ServiceSessionResponse, AuditRecord, error) {
			tendered, change := cmd.CashTenderedVND, changeDueVND
			result, audit, err := recordPayment(ctx, mc.Queries, actor, cmd.CheckID, paymentInput{
				Method:           PaymentMethodCash,
				AppliedAmountVND: cmd.AppliedAmountVND,
				CashTenderedVND:  &tendered,
				ChangeDueVND:     &change,
				AuditEvent:       EventCashPaymentRecorded,
				AuditDetails: func(paymentID uuid.UUID, check lockedCheck) any {
					return cashPaymentAudit{
						PaymentID:        paymentID,
						CheckID:          check.ID,
						SalesShiftID:     check.SalesShiftID,
						AppliedAmountVND: cmd.AppliedAmountVND,
						CashTenderedVND:  cmd.CashTenderedVND,
						ChangeDueVND:     changeDueVND,
					}
				},
			})
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			return 200, result, audit, nil
		})
}
```

Add to `internal/sales/dto.go`:

```go
// PayCashCommand records cash received against one Check. The applied amount
// and the cash tendered are separate facts: the applied amount is what the
// customer owed, the tendered amount is what they handed over.
type PayCashCommand struct {
	RequestID        uuid.UUID `json:"request_id"`
	CheckID          uuid.UUID `json:"-"`
	AppliedAmountVND int64     `json:"applied_amount_vnd"`
	CashTenderedVND  int64     `json:"cash_tendered_vnd"`
}
```

Add these two helpers at the bottom of `payments.go` itself, and add `"encoding/json"` to that file's imports. They live here rather than beside 5A's `nullString`, because `marshalAuditDetails` is needed only by the one event that is written inside a mutation body:

```go
// nullInt64 converts an optional int64 to its sql.NullInt64 form.
func nullInt64(v *int64) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *v, Valid: true}
}

// marshalAuditDetails encodes audit details for an event written inside a
// mutation body rather than by ExecuteMutation.
func marshalAuditDetails(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal audit details: %w", err)
	}
	return b, nil
}
```

If `SessionStateActive` and `ShiftStateOpen` do not exist in `domain.go`, add them:

```go
// Cross-slice state literals Sales compares against. They are declared here
// rather than imported, because internal/sales imports no slice but auth.
const (
	SessionStateActive = "ACTIVE"
	ShiftStateOpen     = "OPEN"
)
```

- [ ] **Step 5: Run the unit tests to verify they pass**

Run: `go test ./internal/sales/ -run 'TestPayCashFingerprint|TestValidateCashAmounts' -v && go build ./...`
Expected: PASS

- [ ] **Step 6: Wire the route**

In `internal/sales/routes.go`, add the field, the constructor line, and the route:

```go
	PayCash *PayCashHandler
```

```go
		PayCash: NewPayCashHandler(runner),
```

```go
	v1.POST("/sales/checks/:check_id/payments/cash", s.handlePayCash,
		authn.RequireAuth(), authn.RequireCapability(CapSalesOperate))
```

In `internal/sales/http.go`:

```go
// handlePayCash godoc
//
//	@Summary		Record a Cash Payment
//	@Description	Applies cash to an open Check. The applied amount may not exceed the Check's balance, and the cash tendered may not be below the applied amount. When the Payment brings the balance to zero the Check settles in the same transaction. A replayed response reproduces the original outcome, so it may show a balance that a later Payment has since reduced.
//	@Tags			sales
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			check_id	path		string			true	"Check ID"
//	@Param			body		body		PayCashCommand	true	"Cash payment request"
//	@Success		200			{object}	response.APIResponse{data=ServiceSessionResponse}
//	@Failure		400			{object}	response.APIResponse
//	@Failure		401			{object}	response.APIResponse
//	@Failure		403			{object}	response.APIResponse
//	@Failure		404			{object}	response.APIResponse
//	@Failure		409			{object}	response.APIResponse
//	@Router			/sales/checks/{check_id}/payments/cash [post]
func (s *Slices) handlePayCash(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	checkID, err := parseUUIDParam(c, "check_id")
	if err != nil {
		return sendError(c, err)
	}
	body, err := bindBody[PayCashCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(body.RequestID); err != nil {
		return sendError(c, err)
	}
	body.CheckID = checkID

	status, result, err := s.PayCash.Handle(c.Request().Context(), actor, body)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, result)
}
```

- [ ] **Step 7: Write the integration tests**

Create `internal/sales/payments_integration_test.go`:

```go
//go:build integration

package sales_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPayCash(t *testing.T) {
	env := newSalesEnv(t)
	ctx := context.Background()

	t.Run("paying in full settles the check with complete evidence", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 2)
		checkID := env.soleCheckID(t, session.ID)
		charge := env.checkCharge(t, checkID)

		got, _, err := env.payCash(t, checkID, charge, charge+15_000)
		require.NoError(t, err)

		require.Len(t, got.Checks, 1)
		require.Equal(t, sales.CheckStateSettled, got.Checks[0].State)
		require.Equal(t, charge, got.Checks[0].TotalAppliedVND)
		require.Equal(t, int64(0), got.Checks[0].BalanceVND)
		require.Len(t, got.Checks[0].Payments, 1)
		require.Equal(t, int64(15_000), *got.Checks[0].Payments[0].ChangeDueVND)

		var settledBy, shift, session2 uuid.UUID
		require.NoError(t, env.db.QueryRowContext(ctx, `
			SELECT settled_by_staff_identity_id, settled_during_sales_shift_id,
			       settled_staff_access_session_id
			FROM checks WHERE id = $1`, checkID).Scan(&settledBy, &shift, &session2))
		require.Equal(t, env.cashierIdentityID, settledBy)
		require.Equal(t, env.shiftID, shift)
		require.Equal(t, env.cashierSessionID, session2)
	})

	t.Run("a partial payment leaves the check open", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 2)
		checkID := env.soleCheckID(t, session.ID)
		charge := env.checkCharge(t, checkID)

		got, _, err := env.payCash(t, checkID, charge-1_000, charge-1_000)
		require.NoError(t, err)
		require.Equal(t, sales.CheckStateOpen, got.Checks[0].State)
		require.Equal(t, int64(1_000), got.Checks[0].BalanceVND)
	})

	t.Run("over the balance is rejected and records nothing", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 1)
		checkID := env.soleCheckID(t, session.ID)
		charge := env.checkCharge(t, checkID)

		_, status, err := env.payCash(t, checkID, charge+1, charge+1)
		require.Error(t, err)
		require.Equal(t, http.StatusConflict, status)
		require.ErrorIs(t, err, sales.ErrPaymentExceedsBalance)
		require.Equal(t, 0, env.countPayments(t, checkID))
	})

	t.Run("under-tendered cash is rejected", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 1)
		checkID := env.soleCheckID(t, session.ID)
		charge := env.checkCharge(t, checkID)

		_, _, err := env.payCash(t, checkID, charge, charge-1)
		require.ErrorIs(t, err, sales.ErrInsufficientCashTendered)
		require.Equal(t, 0, env.countPayments(t, checkID))
	})

	t.Run("paying a settled check reports CHECK_NOT_OPEN", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 1)
		checkID := env.soleCheckID(t, session.ID)
		charge := env.checkCharge(t, checkID)

		_, _, err := env.payCash(t, checkID, charge, charge)
		require.NoError(t, err)

		_, _, err = env.payCash(t, checkID, 1_000, 1_000)
		require.ErrorIs(t, err, sales.ErrCheckNotOpen)
	})

	t.Run("an unknown check reports CHECK_NOT_FOUND", func(t *testing.T) {
		_, status, err := env.payCash(t, uuid.New(), 1_000, 1_000)
		require.ErrorIs(t, err, sales.ErrCheckNotFound)
		require.Equal(t, http.StatusNotFound, status)
	})

	t.Run("a settling payment writes two audit events", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 1)
		checkID := env.soleCheckID(t, session.ID)
		charge := env.checkCharge(t, checkID)

		before := env.countAuditEvents(t, sales.EventCheckSettled)
		_, _, err := env.payCash(t, checkID, charge, charge)
		require.NoError(t, err)

		require.Equal(t, before+1, env.countAuditEvents(t, sales.EventCheckSettled))
		require.Equal(t, 1, env.countAuditEventsForCheck(t, sales.EventCashPaymentRecorded, checkID))
	})

	t.Run("an exact replay records only one payment", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 1)
		checkID := env.soleCheckID(t, session.ID)
		charge := env.checkCharge(t, checkID)
		requestID := uuid.New()

		first, _, err := env.payCashWithRequestID(t, requestID, checkID, charge, charge)
		require.NoError(t, err)
		second, _, err := env.payCashWithRequestID(t, requestID, checkID, charge, charge)
		require.NoError(t, err)

		require.Equal(t, first.Checks[0].ID, second.Checks[0].ID)
		require.Equal(t, 1, env.countPayments(t, checkID))
	})

	t.Run("the same request id with a different amount conflicts", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 2)
		checkID := env.soleCheckID(t, session.ID)
		charge := env.checkCharge(t, checkID)
		requestID := uuid.New()

		_, _, err := env.payCashWithRequestID(t, requestID, checkID, 1_000, 1_000)
		require.NoError(t, err)
		_, _, err = env.payCashWithRequestID(t, requestID, checkID, 2_000, 2_000)
		require.ErrorIs(t, err, sales.ErrRequestConflict)
		_ = charge
	})

	t.Run("a barista is denied", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 1)
		checkID := env.soleCheckID(t, session.ID)

		_, status, err := env.payCashAs(t, env.baristaActor, checkID, 1_000, 1_000)
		require.Error(t, err)
		require.Equal(t, http.StatusForbidden, status)
		require.Equal(t, 0, env.countPayments(t, checkID))
	})
}
```

Add the `env` helpers this test needs to `env_integration_test.go`: `payCash`, `payCashWithRequestID`, `payCashAs`, `countPayments`, `countAuditEvents`, `countAuditEventsForCheck`, and `baristaActor`, following the patterns 5B's `commit_integration_test.go` already uses for calling a handler and asserting a status.

- [ ] **Step 8: Run the integration tests**

Run: `go test -tags integration ./internal/sales/ -run TestPayCash -v -p 1`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
make fmt
git add internal/sales/payments.go internal/sales/payments_test.go internal/sales/payments_integration_test.go internal/sales/dto.go internal/sales/http.go internal/sales/routes.go internal/sales/env_integration_test.go sql/queries/sales.sql internal/database/sqlc
git commit -m "feat(sales): record Cash Payments and settle fully paid Checks"
```

---

## Task 6: Manual QR Payment

**Files:**
- Modify: `internal/sales/payments.go`, `internal/sales/payments_test.go`, `internal/sales/payments_integration_test.go`, `internal/sales/dto.go`, `internal/sales/http.go`, `internal/sales/routes.go`

**Interfaces:**
- Consumes: `recordPayment`, `paymentInput`, `ValidateTransactionReference`, `ErrManualQRReceiptRequired`.
- Produces:
  - `PayManualQRCommand{RequestID uuid.UUID; CheckID uuid.UUID; AppliedAmountVND int64; ReceiptObservedInBankApp bool; TransactionReference *string}`
  - `PayManualQRHandler` with `Handle(ctx, actor, cmd) (int, ServiceSessionResponse, error)`
  - `PayManualQRFingerprintFor(cmd PayManualQRCommand) any`

- [ ] **Step 1: Write the failing unit test**

Append to `internal/sales/payments_test.go`:

```go
func TestManualQRFingerprintNormalizesTheReference(t *testing.T) {
	checkID := uuid.MustParse("66666666-6666-6666-6666-666666666666")
	padded := "  FT24012345  "
	tight := "FT24012345"

	withPadding := sales.PayManualQRFingerprintFor(sales.PayManualQRCommand{
		CheckID: checkID, AppliedAmountVND: 85_000,
		ReceiptObservedInBankApp: true, TransactionReference: &padded,
	})
	withoutPadding := sales.PayManualQRFingerprintFor(sales.PayManualQRCommand{
		CheckID: checkID, AppliedAmountVND: 85_000,
		ReceiptObservedInBankApp: true, TransactionReference: &tight,
	})
	require.Equal(t, withPadding, withoutPadding,
		"a reference that differs only in whitespace is the same request")
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/sales/ -run TestManualQRFingerprint -v`
Expected: FAIL — `undefined: sales.PayManualQRFingerprintFor`.

- [ ] **Step 3: Add the command, the fingerprint, and the handler**

Add to `internal/sales/dto.go`:

```go
// PayManualQRCommand records a bank transfer staff confirmed as received.
//
// ReceiptObservedInBankApp is required to be true: a Manual QR Payment has no
// automatic bank or gateway confirmation, so a staff member seeing the money
// arrive is the only evidence there is. It is not stored as a column — a
// value that is true on every row stores nothing — but it is recorded in the
// audit event. See ADR-019.
type PayManualQRCommand struct {
	RequestID                uuid.UUID `json:"request_id"`
	CheckID                  uuid.UUID `json:"-"`
	AppliedAmountVND         int64     `json:"applied_amount_vnd"`
	ReceiptObservedInBankApp bool      `json:"receipt_observed_in_bank_app"`
	TransactionReference     *string   `json:"transaction_reference"`
}
```

Append to `internal/sales/payments.go`:

```go
type manualQRPaymentAudit struct {
	PaymentID                uuid.UUID `json:"payment_id"`
	CheckID                  uuid.UUID `json:"check_id"`
	SalesShiftID             uuid.UUID `json:"sales_shift_id"`
	AppliedAmountVND         int64     `json:"applied_amount_vnd"`
	ReceiptObservedInBankApp bool      `json:"receipt_observed_in_bank_app"`
	TransactionReference     *string   `json:"transaction_reference"`
}

type payManualQRFingerprint struct {
	CheckID              uuid.UUID `json:"check_id"`
	AppliedAmountVND     int64     `json:"applied_amount_vnd"`
	TransactionReference *string   `json:"transaction_reference"`
}

// PayManualQRFingerprintFor exposes the normalized business fingerprint.
//
// The reference is trimmed first, so a request that differs only in
// surrounding whitespace is the same request. The receipt attestation is not
// part of the fingerprint: it is required to be true, so it cannot vary
// between two requests that both succeed.
func PayManualQRFingerprintFor(cmd PayManualQRCommand) any {
	normalized, err := ValidateTransactionReference(cmd.TransactionReference)
	if err != nil {
		normalized = cmd.TransactionReference
	}
	return payManualQRFingerprint{
		CheckID:              cmd.CheckID,
		AppliedAmountVND:     cmd.AppliedAmountVND,
		TransactionReference: normalized,
	}
}

// PayManualQRHandler records a Manual QR Payment against a Check.
type PayManualQRHandler struct{ runner *Runner }

// NewPayManualQRHandler creates a new PayManualQRHandler.
func NewPayManualQRHandler(runner *Runner) *PayManualQRHandler {
	return &PayManualQRHandler{runner: runner}
}

// Handle applies a confirmed bank transfer to a Check.
func (h *PayManualQRHandler) Handle(ctx context.Context, actor Actor,
	cmd PayManualQRCommand,
) (int, ServiceSessionResponse, error) {
	var zero ServiceSessionResponse
	if cmd.AppliedAmountVND <= 0 {
		return 0, zero, fmt.Errorf("%w: applied_amount_vnd must be positive", response.ErrInvalid)
	}
	reference, err := ValidateTransactionReference(cmd.TransactionReference)
	if err != nil {
		return 0, zero, err
	}
	// A policy, not a field shape: no Payment exists before a staff member
	// has seen the money arrive, so this gets its own code rather than
	// landing on the generic validation one.
	if !cmd.ReceiptObservedInBankApp {
		return 0, zero, fmt.Errorf("%w: check %s", ErrManualQRReceiptRequired, cmd.CheckID)
	}

	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpPayManualQR,
		Fingerprint: PayManualQRFingerprintFor(cmd),
		Required:    []string{CapSalesOperate},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, ServiceSessionResponse, AuditRecord, error) {
			result, audit, err := recordPayment(ctx, mc.Queries, actor, cmd.CheckID, paymentInput{
				Method:               PaymentMethodManualQR,
				AppliedAmountVND:     cmd.AppliedAmountVND,
				TransactionReference: reference,
				AuditEvent:           EventManualQRPaymentRecorded,
				AuditDetails: func(paymentID uuid.UUID, check lockedCheck) any {
					return manualQRPaymentAudit{
						PaymentID:                paymentID,
						CheckID:                  check.ID,
						SalesShiftID:             check.SalesShiftID,
						AppliedAmountVND:         cmd.AppliedAmountVND,
						ReceiptObservedInBankApp: true,
						TransactionReference:     reference,
					}
				},
			})
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			return 200, result, audit, nil
		})
}
```

- [ ] **Step 4: Run the unit test to verify it passes**

Run: `go test ./internal/sales/ -run TestManualQRFingerprint -v && go build ./...`
Expected: PASS

- [ ] **Step 5: Wire the route**

`internal/sales/routes.go`: add `PayManualQR *PayManualQRHandler` to `Slices`, `PayManualQR: NewPayManualQRHandler(runner),` to `NewSlices`, and:

```go
	v1.POST("/sales/checks/:check_id/payments/manual-qr", s.handlePayManualQR,
		authn.RequireAuth(), authn.RequireCapability(CapSalesOperate))
```

`internal/sales/http.go`:

```go
// handlePayManualQR godoc
//
//	@Summary		Record a Manual QR Payment
//	@Description	Applies a bank transfer staff have confirmed as received to an open Check. receipt_observed_in_bank_app must be true, because a Manual QR Payment carries no automatic bank or gateway confirmation. The applied amount may not exceed the Check's balance. When the Payment brings the balance to zero the Check settles in the same transaction.
//	@Tags			sales
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			check_id	path		string				true	"Check ID"
//	@Param			body		body		PayManualQRCommand	true	"Manual QR payment request"
//	@Success		200			{object}	response.APIResponse{data=ServiceSessionResponse}
//	@Failure		400			{object}	response.APIResponse
//	@Failure		401			{object}	response.APIResponse
//	@Failure		403			{object}	response.APIResponse
//	@Failure		404			{object}	response.APIResponse
//	@Failure		409			{object}	response.APIResponse
//	@Router			/sales/checks/{check_id}/payments/manual-qr [post]
func (s *Slices) handlePayManualQR(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	checkID, err := parseUUIDParam(c, "check_id")
	if err != nil {
		return sendError(c, err)
	}
	body, err := bindBody[PayManualQRCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(body.RequestID); err != nil {
		return sendError(c, err)
	}
	body.CheckID = checkID

	status, result, err := s.PayManualQR.Handle(c.Request().Context(), actor, body)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, result)
}
```

- [ ] **Step 6: Write the integration tests**

Append to `internal/sales/payments_integration_test.go`:

```go
func TestPayManualQR(t *testing.T) {
	env := newSalesEnv(t)

	t.Run("paying in full settles the check and stores no cash fields", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 2)
		checkID := env.soleCheckID(t, session.ID)
		charge := env.checkCharge(t, checkID)
		ref := "FT24012345"

		got, _, err := env.payManualQR(t, checkID, charge, true, &ref)
		require.NoError(t, err)

		require.Equal(t, sales.CheckStateSettled, got.Checks[0].State)
		require.Len(t, got.Checks[0].Payments, 1)
		payment := got.Checks[0].Payments[0]
		require.Equal(t, sales.PaymentMethodManualQR, payment.Method)
		require.Nil(t, payment.CashTenderedVND)
		require.Nil(t, payment.ChangeDueVND)
		require.Equal(t, "FT24012345", *payment.TransactionReference)
	})

	t.Run("a payment without a reference is accepted", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 1)
		checkID := env.soleCheckID(t, session.ID)
		charge := env.checkCharge(t, checkID)

		got, _, err := env.payManualQR(t, checkID, charge, true, nil)
		require.NoError(t, err)
		require.Nil(t, got.Checks[0].Payments[0].TransactionReference)
	})

	t.Run("an unconfirmed receipt is rejected", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 1)
		checkID := env.soleCheckID(t, session.ID)
		charge := env.checkCharge(t, checkID)

		_, status, err := env.payManualQR(t, checkID, charge, false, nil)
		require.ErrorIs(t, err, sales.ErrManualQRReceiptRequired)
		require.Equal(t, http.StatusConflict, status)
		require.Equal(t, 0, env.countPayments(t, checkID))
	})

	t.Run("an over-long reference is a validation error", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 1)
		checkID := env.soleCheckID(t, session.ID)
		long := strings.Repeat("A", 101)

		_, status, err := env.payManualQR(t, checkID, 1_000, true, &long)
		require.Error(t, err)
		require.Equal(t, http.StatusBadRequest, status)
	})
}

// Mixed settlement: cash then manual QR closes the Check.
func TestMixedPaymentSettlement(t *testing.T) {
	env := newSalesEnv(t)

	session := env.commitTakeawayDraft(t, 2)
	checkID := env.soleCheckID(t, session.ID)
	charge := env.checkCharge(t, checkID)
	half := charge / 2

	got, _, err := env.payCash(t, checkID, half, half)
	require.NoError(t, err)
	require.Equal(t, sales.CheckStateOpen, got.Checks[0].State)
	require.Equal(t, charge-half, got.Checks[0].BalanceVND)

	got, _, err = env.payManualQR(t, checkID, charge-half, true, nil)
	require.NoError(t, err)
	require.Equal(t, sales.CheckStateSettled, got.Checks[0].State)
	require.Equal(t, int64(0), got.Checks[0].BalanceVND)
	require.Len(t, got.Checks[0].Payments, 2)
	require.Equal(t, sales.PaymentMethodCash, got.Checks[0].Payments[0].Method)
	require.Equal(t, sales.PaymentMethodManualQR, got.Checks[0].Payments[1].Method)
}
```

Add `payManualQR` to `env_integration_test.go`, mirroring `payCash`. Add `"strings"` to the test file's imports.

- [ ] **Step 7: Run the integration tests**

Run: `go test -tags integration ./internal/sales/ -run 'TestPayManualQR|TestMixedPaymentSettlement' -v -p 1`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
make fmt
git add internal/sales/payments.go internal/sales/payments_test.go internal/sales/payments_integration_test.go internal/sales/dto.go internal/sales/http.go internal/sales/routes.go internal/sales/env_integration_test.go
git commit -m "feat(sales): record Manual QR Payments"
```

---

## Task 7: Split Check

**Files:**
- Create: `internal/sales/check_restructuring.go`, `internal/sales/check_restructuring_test.go`, `internal/sales/check_restructuring_integration_test.go`
- Modify: `internal/sales/dto.go`, `internal/sales/http.go`, `internal/sales/routes.go`, `sql/queries/sales.sql`

**Interfaces:**
- Consumes: `lockCheckForMutation` (Task 5), `AddCharge`, `SubtractCharge`, `LineTotal`, `LoadServiceSession`.
- Produces:
  - `SplitItem{CommittedItemID uuid.UUID; Quantity int32}`, `SplitDestination{Type string; CheckID *uuid.UUID}`
  - `SplitCheckCommand{RequestID uuid.UUID; SourceCheckID uuid.UUID; Destination SplitDestination; Items []SplitItem}`
  - `SplitCheckHandler` with `Handle(ctx, actor, cmd) (int, ServiceSessionResponse, error)`
  - `ValidateSplitItems(items []SplitItem) error`, `NormalizeSplitItems(items []SplitItem) []SplitItem`
  - `lockChecks(ctx, q, ids []uuid.UUID) (map[uuid.UUID]lockedCheck, error)` — reused by Task 8
  - `assertNoPayments(ctx, q, ids []uuid.UUID) error` — reused by Task 8
  - sqlc `LockChecksForRestructuring`, `CountPaymentsForChecks`, `ListAllocationsForItems`, `SetAllocationQuantity`, `DeleteAllocation`, `SetCheckCharge`

- [ ] **Step 1: Write the failing unit tests**

Create `internal/sales/check_restructuring_test.go`:

```go
package sales_test

import (
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestValidateSplitItems(t *testing.T) {
	a := uuid.MustParse("77777777-7777-7777-7777-777777777777")
	b := uuid.MustParse("88888888-8888-8888-8888-888888888888")

	t.Run("a well-formed list is accepted", func(t *testing.T) {
		require.NoError(t, sales.ValidateSplitItems([]sales.SplitItem{
			{CommittedItemID: a, Quantity: 1},
			{CommittedItemID: b, Quantity: 2},
		}))
	})

	t.Run("an empty list is rejected", func(t *testing.T) {
		require.ErrorIs(t, sales.ValidateSplitItems(nil), sales.ErrInvalidCheckSplit)
	})

	t.Run("a duplicated committed item is rejected", func(t *testing.T) {
		require.ErrorIs(t, sales.ValidateSplitItems([]sales.SplitItem{
			{CommittedItemID: a, Quantity: 1},
			{CommittedItemID: a, Quantity: 1},
		}), sales.ErrInvalidCheckSplit)
	})

	t.Run("a non-positive quantity is rejected", func(t *testing.T) {
		require.ErrorIs(t, sales.ValidateSplitItems([]sales.SplitItem{
			{CommittedItemID: a, Quantity: 0},
		}), sales.ErrInvalidCheckSplit)
	})

	t.Run("a quantity above the bound is rejected", func(t *testing.T) {
		require.ErrorIs(t, sales.ValidateSplitItems([]sales.SplitItem{
			{CommittedItemID: a, Quantity: 10_000},
		}), sales.ErrInvalidCheckSplit)
	})
}

// The fingerprint must not depend on the order the client listed items in,
// so a retried request that reshuffled its array replays rather than
// double-splitting.
func TestNormalizeSplitItemsSortsByCommittedItem(t *testing.T) {
	a := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	b := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	forward := sales.NormalizeSplitItems([]sales.SplitItem{
		{CommittedItemID: a, Quantity: 1}, {CommittedItemID: b, Quantity: 2},
	})
	reversed := sales.NormalizeSplitItems([]sales.SplitItem{
		{CommittedItemID: b, Quantity: 2}, {CommittedItemID: a, Quantity: 1},
	})
	require.Equal(t, forward, reversed)
	require.Equal(t, a, forward[0].CommittedItemID)
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/sales/ -run 'TestValidateSplitItems|TestNormalizeSplitItems' -v`
Expected: FAIL — `undefined: sales.SplitItem`.

- [ ] **Step 3: Add the queries**

Append to `sql/queries/sales.sql`:

```sql
-- name: LockChecksForRestructuring :many
-- The uniform 5C lock protocol over a set of Checks, ordered by id so two
-- concurrent restructurings take the rows in the same order and cannot
-- deadlock against each other or against a Payment. See ADR-016.
SELECT c.id, c.state, c.charge_vnd, c.service_session_id,
       s.state AS service_session_state,
       s.sales_shift_id, sh.state AS sales_shift_state
FROM checks c
JOIN service_sessions s ON s.id = c.service_session_id
JOIN sales_shifts sh ON sh.id = s.sales_shift_id
WHERE c.id = ANY($1::uuid[])
ORDER BY c.id
FOR UPDATE OF c
FOR SHARE OF s, sh;

-- name: CountPaymentsForChecks :one
SELECT count(*)::BIGINT AS payment_count
FROM payments
WHERE check_id = ANY($1::uuid[]);

-- name: ListAllocationsForItems :many
-- One Check's allocations restricted to a set of Committed Items, with the
-- frozen unit price the moved amount is computed from.
--
-- The array argument is named through sqlc.arg so the generated params struct
-- carries CommittedItemIds rather than a positional Column2.
SELECT ca.id, ca.committed_item_id, ca.quantity, ci.unit_price_vnd
FROM charge_allocations ca
JOIN committed_items ci ON ci.id = ca.committed_item_id
WHERE ca.check_id = sqlc.arg(check_id)
  AND ca.committed_item_id = ANY(sqlc.arg(committed_item_ids)::uuid[])
ORDER BY ca.committed_item_id;

-- name: ListCheckAllocationQuantities :many
SELECT id, committed_item_id, quantity
FROM charge_allocations
WHERE check_id = $1
ORDER BY committed_item_id;

-- name: SetAllocationQuantity :exec
UPDATE charge_allocations SET quantity = $2 WHERE id = $1;

-- name: DeleteAllocation :exec
DELETE FROM charge_allocations WHERE id = $1;

-- name: MoveAllocationToCheck :exec
UPDATE charge_allocations SET check_id = $2 WHERE id = $1;

-- name: SetCheckCharge :exec
UPDATE checks SET charge_vnd = $2 WHERE id = $1;

-- name: MarkCheckMerged :exec
-- The absorbed Check keeps no charge and points at the survivor, which is
-- what the MERGED branch of check_settlement_evidence_valid requires.
UPDATE checks
SET state = 'MERGED', charge_vnd = 0, merged_into_check_id = $2
WHERE id = $1;
```

Run: `make sqlc`

- [ ] **Step 4: Write the split handler**

Create `internal/sales/check_restructuring.go`:

```go
package sales

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
)

// lockChecks acquires several Checks under the uniform 5C protocol and
// evaluates the preconditions shared by Split and Merge.
//
// Every requested id must come back: a missing one means the client named a
// Check that does not exist.
func lockChecks(ctx context.Context, q *sqlc.Queries, ids []uuid.UUID) (
	map[uuid.UUID]lockedCheck, error,
) {
	rows, err := q.LockChecksForRestructuring(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("lock checks for restructuring: %w", err)
	}
	out := make(map[uuid.UUID]lockedCheck, len(rows))
	for _, row := range rows {
		out[row.ID] = lockedCheck{
			ID:               row.ID,
			State:            row.State,
			ChargeVND:        row.ChargeVnd,
			ServiceSessionID: row.ServiceSessionID,
			SalesShiftID:     row.SalesShiftID,
		}
	}
	for _, id := range ids {
		if _, ok := out[id]; !ok {
			return nil, fmt.Errorf("%w: check %s", ErrCheckNotFound, id)
		}
	}

	var session uuid.UUID
	for _, row := range rows {
		if session == uuid.Nil {
			session = row.ServiceSessionID
		} else if row.ServiceSessionID != session {
			return nil, fmt.Errorf("%w: %s and %s",
				ErrChecksDifferentSession, session, row.ServiceSessionID)
		}
		if row.State != CheckStateOpen {
			return nil, fmt.Errorf("%w: check %s is %s", ErrCheckNotOpen, row.ID, row.State)
		}
		if row.ServiceSessionState != SessionStateActive {
			return nil, fmt.Errorf("%w: session %s", ErrServiceSessionClosed, row.ServiceSessionID)
		}
		if row.SalesShiftState != ShiftStateOpen {
			return nil, fmt.Errorf("%w: check %s", ErrOpenShiftRequired, row.ID)
		}
	}
	return out, nil
}

// assertNoPayments refuses to restructure a Check that has taken money.
//
// This is the central rule of 5C. Arranging Checks is work done before money
// is taken; once money has been taken, a Check's structure is reconciliation
// evidence rather than a sorting tool. The guard is stricter than CONTEXT.md's
// "a Settled Check accepts no merge or split" on purpose: it blocks on the
// first Payment, because a partially paid Check is already evidence.
func assertNoPayments(ctx context.Context, q *sqlc.Queries, ids []uuid.UUID) error {
	n, err := q.CountPaymentsForChecks(ctx, ids)
	if err != nil {
		return fmt.Errorf("count payments for checks: %w", err)
	}
	if n > 0 {
		return fmt.Errorf("%w: %d payment(s) recorded", ErrCheckHasPayment, n)
	}
	return nil
}

// ValidateSplitItems rejects a moved-item list that cannot describe a split.
func ValidateSplitItems(items []SplitItem) error {
	if len(items) == 0 {
		return fmt.Errorf("%w: at least one item must be moved", ErrInvalidCheckSplit)
	}
	seen := make(map[uuid.UUID]struct{}, len(items))
	for _, item := range items {
		if _, dup := seen[item.CommittedItemID]; dup {
			return fmt.Errorf("%w: committed item %s listed twice",
				ErrInvalidCheckSplit, item.CommittedItemID)
		}
		seen[item.CommittedItemID] = struct{}{}
		if item.Quantity < MinQuantity || item.Quantity > MaxQuantity {
			return fmt.Errorf("%w: quantity %d is outside [%d, %d]",
				ErrInvalidCheckSplit, item.Quantity, MinQuantity, MaxQuantity)
		}
	}
	return nil
}

// NormalizeSplitItems sorts by Committed Item so the request fingerprint does
// not depend on the order the client happened to list them in.
func NormalizeSplitItems(items []SplitItem) []SplitItem {
	out := make([]SplitItem, len(items))
	copy(out, items)
	sort.Slice(out, func(i, j int) bool {
		return out[i].CommittedItemID.String() < out[j].CommittedItemID.String()
	})
	return out
}

type splitFingerprint struct {
	SourceCheckID uuid.UUID   `json:"source_check_id"`
	Destination   string      `json:"destination"`
	DestinationID *uuid.UUID  `json:"destination_check_id"`
	Items         []SplitItem `json:"items"`
}

type splitAudit struct {
	ServiceSessionID    uuid.UUID   `json:"service_session_id"`
	SourceCheckID       uuid.UUID   `json:"source_check_id"`
	DestinationCheckID  uuid.UUID   `json:"destination_check_id"`
	MovedItems          []SplitItem `json:"moved_items"`
	MovedChargeVND      int64       `json:"moved_charge_vnd"`
}

// SplitCheckHandler moves part of a Check's charge onto another Check.
type SplitCheckHandler struct{ runner *Runner }

// NewSplitCheckHandler creates a new SplitCheckHandler.
func NewSplitCheckHandler(runner *Runner) *SplitCheckHandler {
	return &SplitCheckHandler{runner: runner}
}

// Handle redistributes allocation quantities between two Checks.
func (h *SplitCheckHandler) Handle(ctx context.Context, actor Actor, cmd SplitCheckCommand) (
	int, ServiceSessionResponse, error,
) {
	var zero ServiceSessionResponse

	items := NormalizeSplitItems(cmd.Items)
	if err := ValidateSplitItems(items); err != nil {
		return 0, zero, err
	}
	switch cmd.Destination.Type {
	case SplitDestinationNewCheck:
		if cmd.Destination.CheckID != nil {
			return 0, zero, fmt.Errorf("%w: destination.check_id is not allowed for NEW_CHECK",
				response.ErrInvalid)
		}
	case SplitDestinationExistingCheck:
		if cmd.Destination.CheckID == nil {
			return 0, zero, fmt.Errorf("%w: destination.check_id is required for EXISTING_CHECK",
				response.ErrInvalid)
		}
		if *cmd.Destination.CheckID == cmd.SourceCheckID {
			return 0, zero, fmt.Errorf("%w: destination is the source check", ErrInvalidCheckSplit)
		}
	default:
		return 0, zero, fmt.Errorf("%w: destination.type must be %s or %s",
			response.ErrInvalid, SplitDestinationNewCheck, SplitDestinationExistingCheck)
	}

	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpSplitCheck,
		Fingerprint: splitFingerprint{
			SourceCheckID: cmd.SourceCheckID,
			Destination:   cmd.Destination.Type,
			DestinationID: cmd.Destination.CheckID,
			Items:         items,
		},
		Required: []string{CapSalesOperate},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, ServiceSessionResponse, AuditRecord, error) {
			q := mc.Queries

			ids := []uuid.UUID{cmd.SourceCheckID}
			if cmd.Destination.CheckID != nil {
				ids = append(ids, *cmd.Destination.CheckID)
			}
			locked, err := lockChecks(ctx, q, ids)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			if err := assertNoPayments(ctx, q, ids); err != nil {
				return 0, zero, AuditRecord{}, err
			}
			source := locked[cmd.SourceCheckID]

			itemIDs := make([]uuid.UUID, 0, len(items))
			for _, item := range items {
				itemIDs = append(itemIDs, item.CommittedItemID)
			}
			allocRows, err := q.ListAllocationsForItems(ctx, sqlc.ListAllocationsForItemsParams{
				CheckID: source.ID, CommittedItemIds: itemIDs,
			})
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("list source allocations: %w", err)
			}
			sourceByItem := make(map[uuid.UUID]sqlc.ListAllocationsForItemsRow, len(allocRows))
			for _, row := range allocRows {
				sourceByItem[row.CommittedItemID] = row
			}

			var movedChargeVND int64
			for _, item := range items {
				allocation, ok := sourceByItem[item.CommittedItemID]
				if !ok {
					return 0, zero, AuditRecord{}, fmt.Errorf("%w: committed item %s",
						ErrSplitAllocationNotFound, item.CommittedItemID)
				}
				if item.Quantity > allocation.Quantity {
					return 0, zero, AuditRecord{}, fmt.Errorf("%w: %d of %d for committed item %s",
						ErrSplitQuantityExceedsAllocation, item.Quantity,
						allocation.Quantity, item.CommittedItemID)
				}
				amountVND, err := LineTotal(item.Quantity, allocation.UnitPriceVnd)
				if err != nil {
					return 0, zero, AuditRecord{}, err
				}
				movedChargeVND, err = AddCharge(movedChargeVND, amountVND)
				if err != nil {
					return 0, zero, AuditRecord{}, err
				}
			}

			sourceChargeVND, err := SubtractCharge(source.ChargeVND, movedChargeVND)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			// A Check cannot be split empty. Moving everything is Merge,
			// which says what it means.
			if sourceChargeVND <= 0 {
				return 0, zero, AuditRecord{}, fmt.Errorf("%w: source check %s",
					ErrSplitSourceWouldBeEmpty, source.ID)
			}

			var destinationChargeVND int64
			destinationID := uuid.Nil
			if cmd.Destination.CheckID != nil {
				destination := locked[*cmd.Destination.CheckID]
				destinationID = destination.ID
				destinationChargeVND, err = AddCharge(destination.ChargeVND, movedChargeVND)
			} else {
				destinationChargeVND, err = AddCharge(0, movedChargeVND)
			}
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			if destinationChargeVND <= 0 {
				return 0, zero, AuditRecord{}, fmt.Errorf("%w: nothing would be charged",
					ErrSplitDestinationWouldBeEmpty)
			}

			occurredAt := time.Now()
			if destinationID == uuid.Nil {
				created, err := q.InsertCheck(ctx, sqlc.InsertCheckParams{
					ServiceSessionID: source.ServiceSessionID,
					CreatedAt:        occurredAt,
				})
				if err != nil {
					return 0, zero, AuditRecord{}, fmt.Errorf("create destination check: %w", err)
				}
				destinationID = created.ID
			}

			destAllocRows, err := q.ListAllocationsForItems(ctx, sqlc.ListAllocationsForItemsParams{
				CheckID: destinationID, CommittedItemIds: itemIDs,
			})
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("list destination allocations: %w", err)
			}
			destByItem := make(map[uuid.UUID]sqlc.ListAllocationsForItemsRow, len(destAllocRows))
			for _, row := range destAllocRows {
				destByItem[row.CommittedItemID] = row
			}

			for _, item := range items {
				allocation := sourceByItem[item.CommittedItemID]
				if item.Quantity == allocation.Quantity {
					if err := q.DeleteAllocation(ctx, allocation.ID); err != nil {
						return 0, zero, AuditRecord{}, fmt.Errorf("delete source allocation: %w", err)
					}
				} else {
					if err := q.SetAllocationQuantity(ctx, sqlc.SetAllocationQuantityParams{
						ID: allocation.ID, Quantity: allocation.Quantity - item.Quantity,
					}); err != nil {
						return 0, zero, AuditRecord{}, fmt.Errorf("reduce source allocation: %w", err)
					}
				}

				if existing, ok := destByItem[item.CommittedItemID]; ok {
					if err := q.SetAllocationQuantity(ctx, sqlc.SetAllocationQuantityParams{
						ID: existing.ID, Quantity: existing.Quantity + item.Quantity,
					}); err != nil {
						return 0, zero, AuditRecord{}, fmt.Errorf("raise destination allocation: %w", err)
					}
					continue
				}
				if err := q.InsertChargeAllocation(ctx, sqlc.InsertChargeAllocationParams{
					CommittedItemID: item.CommittedItemID,
					CheckID:         destinationID,
					Quantity:        item.Quantity,
					CreatedAt:       occurredAt,
				}); err != nil {
					return 0, zero, AuditRecord{}, fmt.Errorf("insert destination allocation: %w", err)
				}
			}

			if err := q.SetCheckCharge(ctx, sqlc.SetCheckChargeParams{
				ID: source.ID, ChargeVnd: sourceChargeVND,
			}); err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("rewrite source charge: %w", err)
			}
			if err := q.SetCheckCharge(ctx, sqlc.SetCheckChargeParams{
				ID: destinationID, ChargeVnd: destinationChargeVND,
			}); err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("rewrite destination charge: %w", err)
			}

			result, err := LoadServiceSession(ctx, q, source.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			return 200, result, AuditRecord{
				EventType: EventCheckSplit,
				Details: splitAudit{
					ServiceSessionID:   source.ServiceSessionID,
					SourceCheckID:      source.ID,
					DestinationCheckID: destinationID,
					MovedItems:         items,
					MovedChargeVND:     movedChargeVND,
				},
			}, nil
		})
}
```

Add to `internal/sales/dto.go`:

```go
// SplitItem is one Committed Item and the quantity of it being moved.
type SplitItem struct {
	CommittedItemID uuid.UUID `json:"committed_item_id"`
	Quantity        int32     `json:"quantity"`
}

// SplitDestination names where the moved charge lands. Type is NEW_CHECK or
// EXISTING_CHECK; CheckID is required for the latter and forbidden for the
// former.
type SplitDestination struct {
	Type    string     `json:"type"`
	CheckID *uuid.UUID `json:"check_id,omitempty"`
}

// SplitCheckCommand moves part of a Check's charge onto another Check. Both
// Checks must be OPEN, belong to the same Service Session, and carry no
// Payment.
type SplitCheckCommand struct {
	RequestID     uuid.UUID        `json:"request_id"`
	SourceCheckID uuid.UUID        `json:"-"`
	Destination   SplitDestination `json:"destination"`
	Items         []SplitItem      `json:"items"`
}
```

- [ ] **Step 5: Run the unit tests to verify they pass**

Run: `go test ./internal/sales/ -run 'TestValidateSplitItems|TestNormalizeSplitItems' -v && go build ./...`
Expected: PASS

- [ ] **Step 6: Wire the route**

`internal/sales/routes.go`: add `SplitCheck *SplitCheckHandler`, `SplitCheck: NewSplitCheckHandler(runner),`, and:

```go
	v1.POST("/sales/checks/:check_id/split", s.handleSplitCheck,
		authn.RequireAuth(), authn.RequireCapability(CapSalesOperate))
```

`internal/sales/http.go`:

```go
// handleSplitCheck godoc
//
//	@Summary		Split a Check
//	@Description	Moves a quantity of one or more Committed Items from this Check onto another, either a newly created Check or an existing OPEN Check of the same Service Session. Rejected once either Check carries a Payment, because a paid Check is reconciliation evidence rather than a sorting tool. Neither the source nor the destination may be left with nothing charged.
//	@Tags			sales
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			check_id	path		string				true	"Source Check ID"
//	@Param			body		body		SplitCheckCommand	true	"Split request"
//	@Success		200			{object}	response.APIResponse{data=ServiceSessionResponse}
//	@Failure		400			{object}	response.APIResponse
//	@Failure		401			{object}	response.APIResponse
//	@Failure		403			{object}	response.APIResponse
//	@Failure		404			{object}	response.APIResponse
//	@Failure		409			{object}	response.APIResponse
//	@Router			/sales/checks/{check_id}/split [post]
func (s *Slices) handleSplitCheck(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	checkID, err := parseUUIDParam(c, "check_id")
	if err != nil {
		return sendError(c, err)
	}
	body, err := bindBody[SplitCheckCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(body.RequestID); err != nil {
		return sendError(c, err)
	}
	body.SourceCheckID = checkID

	status, result, err := s.SplitCheck.Handle(c.Request().Context(), actor, body)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, result)
}
```

- [ ] **Step 7: Write the integration tests**

Create `internal/sales/check_restructuring_integration_test.go`:

```go
//go:build integration

package sales_test

import (
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSplitCheck(t *testing.T) {
	env := newSalesEnv(t)

	t.Run("splitting onto a new check divides the charge", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 2)
		sourceID := env.soleCheckID(t, session.ID)
		originalCharge := env.checkCharge(t, sourceID)
		allocations := env.checkAllocations(t, session.ID, sourceID)

		got, _, err := env.splitToNewCheck(t, sourceID, []sales.SplitItem{
			{CommittedItemID: allocations[0].CommittedItemID, Quantity: allocations[0].AllocatedQuantity},
		})
		require.NoError(t, err)

		require.Len(t, got.Checks, 2)
		var total int64
		for _, check := range got.Checks {
			require.Equal(t, sales.CheckStateOpen, check.State)
			require.Greater(t, check.ChargeVND, int64(0))
			total += check.ChargeVND
		}
		require.Equal(t, originalCharge, total,
			"splitting must conserve the total charge")
	})

	t.Run("splitting onto an existing check combines the allocation", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 2)
		sourceID := env.soleCheckID(t, session.ID)
		allocations := env.checkAllocations(t, session.ID, sourceID)
		require.GreaterOrEqual(t, allocations[0].AllocatedQuantity, int32(2),
			"fixture must commit at least two units of the first item")

		// First split creates the destination with one unit.
		got, _, err := env.splitToNewCheck(t, sourceID, []sales.SplitItem{
			{CommittedItemID: allocations[0].CommittedItemID, Quantity: 1},
		})
		require.NoError(t, err)
		destinationID := env.otherCheckID(t, got, sourceID)

		// Second split moves another unit of the same item onto it.
		got, _, err = env.splitToExistingCheck(t, sourceID, destinationID, []sales.SplitItem{
			{CommittedItemID: allocations[0].CommittedItemID, Quantity: 1},
		})
		require.NoError(t, err)

		destination := env.findCheck(t, got, destinationID)
		require.Len(t, destination.Allocations, 1,
			"the same committed item must combine into one allocation, not two")
		require.Equal(t, int32(2), destination.Allocations[0].AllocatedQuantity)
	})

	t.Run("splitting the whole source is rejected", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 1)
		sourceID := env.soleCheckID(t, session.ID)
		allocations := env.checkAllocations(t, session.ID, sourceID)

		_, _, err := env.splitToNewCheck(t, sourceID, []sales.SplitItem{
			{CommittedItemID: allocations[0].CommittedItemID, Quantity: allocations[0].AllocatedQuantity},
		})
		require.ErrorIs(t, err, sales.ErrSplitSourceWouldBeEmpty)
	})

	t.Run("a quantity above the allocation is rejected", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 2)
		sourceID := env.soleCheckID(t, session.ID)
		allocations := env.checkAllocations(t, session.ID, sourceID)

		_, _, err := env.splitToNewCheck(t, sourceID, []sales.SplitItem{
			{CommittedItemID: allocations[0].CommittedItemID, Quantity: allocations[0].AllocatedQuantity + 1},
		})
		require.ErrorIs(t, err, sales.ErrSplitQuantityExceedsAllocation)
	})

	t.Run("an item not on the source is rejected", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 2)
		sourceID := env.soleCheckID(t, session.ID)

		_, _, err := env.splitToNewCheck(t, sourceID, []sales.SplitItem{
			{CommittedItemID: uuid.New(), Quantity: 1},
		})
		require.ErrorIs(t, err, sales.ErrSplitAllocationNotFound)
	})

	t.Run("a paid check cannot be split", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 2)
		sourceID := env.soleCheckID(t, session.ID)
		allocations := env.checkAllocations(t, session.ID, sourceID)

		_, _, err := env.payCash(t, sourceID, 1_000, 1_000)
		require.NoError(t, err)

		_, _, err = env.splitToNewCheck(t, sourceID, []sales.SplitItem{
			{CommittedItemID: allocations[0].CommittedItemID, Quantity: 1},
		})
		require.ErrorIs(t, err, sales.ErrCheckHasPayment)
	})

	t.Run("the charge invariant survives the split", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 2)
		sourceID := env.soleCheckID(t, session.ID)
		allocations := env.checkAllocations(t, session.ID, sourceID)

		got, _, err := env.splitToNewCheck(t, sourceID, []sales.SplitItem{
			{CommittedItemID: allocations[0].CommittedItemID, Quantity: 1},
		})
		require.NoError(t, err)

		// A second read re-runs the projection's stored-charge comparison,
		// which is the invariant itself.
		reread := env.getSession(t, session.ID)
		require.Len(t, reread.Checks, len(got.Checks))
	})
}
```

Add `splitToNewCheck`, `splitToExistingCheck`, `checkAllocations`, `otherCheckID`, and `findCheck` to `env_integration_test.go`. `commitTakeawayDraft(t, n)` must commit a draft with `n` distinct Menu Items, the first at quantity 2, so the combining and source-would-be-empty cases have the data they need.

- [ ] **Step 8: Run the integration tests**

Run: `go test -tags integration ./internal/sales/ -run TestSplitCheck -v -p 1`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
make fmt
git add internal/sales/check_restructuring.go internal/sales/check_restructuring_test.go internal/sales/check_restructuring_integration_test.go internal/sales/dto.go internal/sales/http.go internal/sales/routes.go internal/sales/env_integration_test.go sql/queries/sales.sql internal/database/sqlc
git commit -m "feat(sales): split a Check's charge onto another Check"
```

---

## Task 8: Merge Checks

**Files:**
- Modify: `internal/sales/check_restructuring.go`, `internal/sales/check_restructuring_integration_test.go`, `internal/sales/dto.go`, `internal/sales/http.go`, `internal/sales/routes.go`

**Interfaces:**
- Consumes: `lockChecks`, `assertNoPayments` (Task 7), `AddCharge`, `LoadServiceSession`.
- Produces:
  - `MergeChecksCommand{RequestID uuid.UUID; SurvivingCheckID uuid.UUID; AbsorbedCheckID uuid.UUID}`
  - `MergeChecksHandler` with `Handle(ctx, actor, cmd) (int, ServiceSessionResponse, error)`

- [ ] **Step 1: Write the failing integration test**

Append to `internal/sales/check_restructuring_integration_test.go`:

```go
func TestMergeChecks(t *testing.T) {
	env := newSalesEnv(t)

	// Merge is reachable through the public API in 5C: splitting creates the
	// second Check that merging then absorbs. No fixture is seeded.
	splitThenTwoChecks := func(t *testing.T) (sessionID, survivingID, absorbedID uuid.UUID, total int64) {
		t.Helper()
		session := env.commitTakeawayDraft(t, 2)
		sourceID := env.soleCheckID(t, session.ID)
		total = env.checkCharge(t, sourceID)
		allocations := env.checkAllocations(t, session.ID, sourceID)

		got, _, err := env.splitToNewCheck(t, sourceID, []sales.SplitItem{
			{CommittedItemID: allocations[0].CommittedItemID, Quantity: 1},
		})
		require.NoError(t, err)
		return session.ID, sourceID, env.otherCheckID(t, got, sourceID), total
	}

	t.Run("merging returns the whole charge to the survivor", func(t *testing.T) {
		sessionID, survivingID, absorbedID, total := splitThenTwoChecks(t)

		got, _, err := env.mergeChecks(t, survivingID, absorbedID)
		require.NoError(t, err)

		surviving := env.findCheck(t, got, survivingID)
		absorbed := env.findCheck(t, got, absorbedID)
		require.Equal(t, sales.CheckStateOpen, surviving.State)
		require.Equal(t, total, surviving.ChargeVND)
		require.Equal(t, sales.CheckStateMerged, absorbed.State)
		require.Equal(t, int64(0), absorbed.ChargeVND)
		require.NotNil(t, absorbed.MergedIntoCheckID)
		require.Equal(t, survivingID, *absorbed.MergedIntoCheckID)
		require.Empty(t, absorbed.Allocations)
		_ = sessionID
	})

	t.Run("overlapping allocations combine into one row", func(t *testing.T) {
		_, survivingID, absorbedID, _ := splitThenTwoChecks(t)

		got, _, err := env.mergeChecks(t, survivingID, absorbedID)
		require.NoError(t, err)

		surviving := env.findCheck(t, got, survivingID)
		seen := make(map[uuid.UUID]int)
		for _, allocation := range surviving.Allocations {
			seen[allocation.CommittedItemID]++
		}
		for item, n := range seen {
			require.Equal(t, 1, n, "committed item %s has more than one allocation", item)
		}
	})

	t.Run("merging a check into itself is rejected", func(t *testing.T) {
		_, survivingID, _, _ := splitThenTwoChecks(t)

		_, _, err := env.mergeChecks(t, survivingID, survivingID)
		require.ErrorIs(t, err, sales.ErrInvalidCheckMerge)
	})

	t.Run("a paid check cannot be merged", func(t *testing.T) {
		_, survivingID, absorbedID, _ := splitThenTwoChecks(t)

		_, _, err := env.payCash(t, survivingID, 1_000, 1_000)
		require.NoError(t, err)

		_, _, err = env.mergeChecks(t, survivingID, absorbedID)
		require.ErrorIs(t, err, sales.ErrCheckHasPayment)
	})

	t.Run("a merged check cannot be merged again", func(t *testing.T) {
		_, survivingID, absorbedID, _ := splitThenTwoChecks(t)

		_, _, err := env.mergeChecks(t, survivingID, absorbedID)
		require.NoError(t, err)

		_, _, err = env.mergeChecks(t, survivingID, absorbedID)
		require.ErrorIs(t, err, sales.ErrCheckNotOpen)
	})

	t.Run("checks of different sessions cannot be merged", func(t *testing.T) {
		_, survivingID, _, _ := splitThenTwoChecks(t)
		otherSession := env.commitTakeawayDraft(t, 1)
		foreignID := env.soleCheckID(t, otherSession.ID)

		_, _, err := env.mergeChecks(t, survivingID, foreignID)
		require.ErrorIs(t, err, sales.ErrChecksDifferentSession)
	})

	t.Run("an unknown check reports CHECK_NOT_FOUND", func(t *testing.T) {
		_, survivingID, _, _ := splitThenTwoChecks(t)

		_, _, err := env.mergeChecks(t, survivingID, uuid.New())
		require.ErrorIs(t, err, sales.ErrCheckNotFound)
	})
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -tags integration ./internal/sales/ -run TestMergeChecks -v -p 1`
Expected: FAIL — `env.mergeChecks` is undefined.

- [ ] **Step 3: Add the command and the handler**

Add to `internal/sales/dto.go`:

```go
// MergeChecksCommand absorbs one Check into another. Both must be OPEN,
// belong to the same Service Session, and carry no Payment.
type MergeChecksCommand struct {
	RequestID        uuid.UUID `json:"request_id"`
	SurvivingCheckID uuid.UUID `json:"surviving_check_id"`
	AbsorbedCheckID  uuid.UUID `json:"absorbed_check_id"`
}
```

Append to `internal/sales/check_restructuring.go`:

```go
type mergeFingerprint struct {
	SurvivingCheckID uuid.UUID `json:"surviving_check_id"`
	AbsorbedCheckID  uuid.UUID `json:"absorbed_check_id"`
}

type mergeAudit struct {
	ServiceSessionID uuid.UUID `json:"service_session_id"`
	SurvivingCheckID uuid.UUID `json:"surviving_check_id"`
	AbsorbedCheckID  uuid.UUID `json:"absorbed_check_id"`
	MergedChargeVND  int64     `json:"merged_charge_vnd"`
}

// MergeChecksHandler absorbs one Check into another.
type MergeChecksHandler struct{ runner *Runner }

// NewMergeChecksHandler creates a new MergeChecksHandler.
func NewMergeChecksHandler(runner *Runner) *MergeChecksHandler {
	return &MergeChecksHandler{runner: runner}
}

// Handle moves every allocation of the absorbed Check onto the survivor.
func (h *MergeChecksHandler) Handle(ctx context.Context, actor Actor, cmd MergeChecksCommand) (
	int, ServiceSessionResponse, error,
) {
	var zero ServiceSessionResponse
	if cmd.SurvivingCheckID == cmd.AbsorbedCheckID {
		return 0, zero, fmt.Errorf("%w: a check cannot absorb itself", ErrInvalidCheckMerge)
	}

	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpMergeChecks,
		Fingerprint: mergeFingerprint{
			SurvivingCheckID: cmd.SurvivingCheckID,
			AbsorbedCheckID:  cmd.AbsorbedCheckID,
		},
		Required: []string{CapSalesOperate},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, ServiceSessionResponse, AuditRecord, error) {
			q := mc.Queries

			ids := []uuid.UUID{cmd.SurvivingCheckID, cmd.AbsorbedCheckID}
			locked, err := lockChecks(ctx, q, ids)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			if err := assertNoPayments(ctx, q, ids); err != nil {
				return 0, zero, AuditRecord{}, err
			}
			surviving := locked[cmd.SurvivingCheckID]
			absorbed := locked[cmd.AbsorbedCheckID]

			absorbedAllocations, err := q.ListCheckAllocationQuantities(ctx, absorbed.ID)
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("list absorbed allocations: %w", err)
			}
			survivingAllocations, err := q.ListCheckAllocationQuantities(ctx, surviving.ID)
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("list surviving allocations: %w", err)
			}
			survivingByItem := make(map[uuid.UUID]sqlc.ListCheckAllocationQuantitiesRow,
				len(survivingAllocations))
			for _, row := range survivingAllocations {
				survivingByItem[row.CommittedItemID] = row
			}

			for _, allocation := range absorbedAllocations {
				existing, ok := survivingByItem[allocation.CommittedItemID]
				if !ok {
					// Nothing to combine with: the row simply changes Check.
					if err := q.MoveAllocationToCheck(ctx, sqlc.MoveAllocationToCheckParams{
						ID: allocation.ID, CheckID: surviving.ID,
					}); err != nil {
						return 0, zero, AuditRecord{}, fmt.Errorf("move allocation: %w", err)
					}
					continue
				}
				if err := q.SetAllocationQuantity(ctx, sqlc.SetAllocationQuantityParams{
					ID: existing.ID, Quantity: existing.Quantity + allocation.Quantity,
				}); err != nil {
					return 0, zero, AuditRecord{}, fmt.Errorf("combine allocation: %w", err)
				}
				if err := q.DeleteAllocation(ctx, allocation.ID); err != nil {
					return 0, zero, AuditRecord{}, fmt.Errorf("delete absorbed allocation: %w", err)
				}
			}

			mergedChargeVND, err := AddCharge(surviving.ChargeVND, absorbed.ChargeVND)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			if err := q.SetCheckCharge(ctx, sqlc.SetCheckChargeParams{
				ID: surviving.ID, ChargeVnd: mergedChargeVND,
			}); err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("raise surviving charge: %w", err)
			}
			if err := q.MarkCheckMerged(ctx, sqlc.MarkCheckMergedParams{
				ID:                absorbed.ID,
				MergedIntoCheckID: uuid.NullUUID{UUID: surviving.ID, Valid: true},
			}); err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("mark check merged: %w", err)
			}

			result, err := LoadServiceSession(ctx, q, surviving.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			return 200, result, AuditRecord{
				EventType: EventCheckMerged,
				Details: mergeAudit{
					ServiceSessionID: surviving.ServiceSessionID,
					SurvivingCheckID: surviving.ID,
					AbsorbedCheckID:  absorbed.ID,
					MergedChargeVND:  mergedChargeVND,
				},
			}, nil
		})
}
```

- [ ] **Step 4: Wire the route**

`internal/sales/routes.go`: add `MergeChecks *MergeChecksHandler`, `MergeChecks: NewMergeChecksHandler(runner),`, and:

```go
	v1.POST("/sales/checks/merge", s.handleMergeChecks,
		authn.RequireAuth(), authn.RequireCapability(CapSalesOperate))
```

Register this route **before** `/sales/checks/:check_id/split` in the file, so a literal segment is never shadowed by the parameter route.

`internal/sales/http.go`:

```go
// handleMergeChecks godoc
//
//	@Summary		Merge two Checks
//	@Description	Absorbs one Check into another. Both must be OPEN, belong to the same Service Session, and carry no Payment. The absorbed Check keeps no charge and records the Check it merged into. The route is flat rather than nested, because merging acts on two peer Checks.
//	@Tags			sales
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			body	body		MergeChecksCommand	true	"Merge request"
//	@Success		200		{object}	response.APIResponse{data=ServiceSessionResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Router			/sales/checks/merge [post]
func (s *Slices) handleMergeChecks(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	body, err := bindBody[MergeChecksCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(body.RequestID); err != nil {
		return sendError(c, err)
	}

	status, result, err := s.MergeChecks.Handle(c.Request().Context(), actor, body)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, result)
}
```

Add `mergeChecks` to `env_integration_test.go`, mirroring `splitToNewCheck`.

- [ ] **Step 5: Run the integration tests**

Run: `go test -tags integration ./internal/sales/ -run 'TestMergeChecks|TestSplitCheck' -v -p 1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
make fmt
git add internal/sales/check_restructuring.go internal/sales/check_restructuring_integration_test.go internal/sales/dto.go internal/sales/http.go internal/sales/routes.go internal/sales/env_integration_test.go
git commit -m "feat(sales): merge one Check into another"
```

---

## Task 9: Complete Expected Cash

**Files:**
- Modify: `sql/queries/shift.sql`, `internal/shift/domain.go`, `internal/shift/current.go`, `internal/shift/cash_movement.go`, `internal/shift/http.go`
- Test: `internal/shift/domain_test.go`, `internal/shift/expected_cash_integration_test.go` (create)

**Interfaces:**
- Consumes: 5C's `payments` table (read through `internal/shift`'s own sqlc query — `internal/shift` MUST NOT import `internal/sales`).
- Produces: sqlc `SumCashPaymentsForShift(ctx, shiftID) (int64, error)`; `ComputeExpectedCash(openingFloatVND, cashPaymentVND, payInVND, payOutVND int64) (int64, error)`.

- [ ] **Step 1: Write the failing unit test**

Append to `internal/shift/domain_test.go`:

```go
func TestComputeExpectedCashIncludesCashPayments(t *testing.T) {
	t.Run("cash payments raise the figure", func(t *testing.T) {
		got, err := shift.ComputeExpectedCash(500_000, 850_000, 100_000, 50_000)
		require.NoError(t, err)
		require.Equal(t, int64(1_400_000), got)
	})

	t.Run("with no payments the Phase 4 figure is unchanged", func(t *testing.T) {
		got, err := shift.ComputeExpectedCash(500_000, 0, 100_000, 50_000)
		require.NoError(t, err)
		require.Equal(t, int64(550_000), got)
	})

	t.Run("sustained pay outs may still drive it negative", func(t *testing.T) {
		got, err := shift.ComputeExpectedCash(100_000, 0, 0, 500_000)
		require.NoError(t, err)
		require.Equal(t, int64(-400_000), got)
	})

	t.Run("a total outside the bound is rejected", func(t *testing.T) {
		_, err := shift.ComputeExpectedCash(shift.MaxAmountVND, shift.MaxAmountVND, 0, 0)
		require.ErrorIs(t, err, shift.ErrExpectedCashOutOfRange)
	})
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/shift/ -run TestComputeExpectedCashIncludesCashPayments -v`
Expected: FAIL — `not enough arguments in call to shift.ComputeExpectedCash`.

- [ ] **Step 3: Add the query**

Append to `sql/queries/shift.sql`:

```sql
-- name: SumCashPaymentsForShift :one
-- Expected Cash's Cash Payment term (ADR-020). The sum is over APPLIED
-- amounts, not tendered amounts: CONTEXT.md defines a Cash Payment's net cash
-- effect as the applied amount, because the change left the drawer at the same
-- moment the tendered cash entered it.
--
-- internal/shift reads the payments table through its own query rather than
-- importing internal/sales, following ADR-012.
SELECT COALESCE(SUM(applied_amount_vnd) FILTER (WHERE method = 'CASH'), 0)::BIGINT
    AS cash_payment_vnd
FROM payments
WHERE sales_shift_id = $1;
```

Run: `make sqlc`

- [ ] **Step 4: Complete the formula**

In `internal/shift/domain.go`, replace `ComputeExpectedCash`:

```go
// ComputeExpectedCash returns the Sales Shift's calculated cash responsibility.
//
// Opening Float plus Cash Payments and Pay Ins, less Pay Outs. The Cash Refund
// term of the canonical formula has no data source: Refund is outside Phase 5
// entirely, and this figure completes when Refund arrives. See ADR-020, which
// supersedes ADR-008's claim that Phase 5 completes both terms.
//
// The guard is symmetric because sustained Pay Outs can legitimately drive the
// figure negative. A total outside the bound indicates corrupt data, not a
// legitimate drawer balance.
func ComputeExpectedCash(openingFloatVND, cashPaymentVND, payInVND, payOutVND int64) (int64, error) {
	total := openingFloatVND + cashPaymentVND + payInVND - payOutVND
	if total > MaxAmountVND || total < -MaxAmountVND {
		return 0, fmt.Errorf("%w: expected cash %d is outside [%d, %d]",
			ErrExpectedCashOutOfRange, total, -MaxAmountVND, MaxAmountVND)
	}
	return total, nil
}
```

In `internal/shift/current.go`, read the term and pass it:

```go
			cashPaymentVND, err := q.SumCashPaymentsForShift(ctx, row.ID)
			if err != nil {
				return nil, fmt.Errorf("sum cash payments for shift: %w", err)
			}

			expected, err := ComputeExpectedCash(row.OpeningFloatVnd, cashPaymentVND, payInVND, payOutVND)
			if err != nil {
				return nil, err
			}
```

In `internal/shift/cash_movement.go`, do the same at its call site:

```go
			cashPaymentVND, err := mc.Queries.SumCashPaymentsForShift(ctx, openShift.ID)
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("sum cash payments for shift: %w", err)
			}

			expected, err := ComputeExpectedCash(openShift.OpeningFloatVnd, cashPaymentVND,
				sums.PayInVnd, sums.PayOutVnd)
```

Adapt the error-return shape to whatever the surrounding closure in `cash_movement.go` uses.

- [ ] **Step 5: Update the Swagger description**

In `internal/shift/http.go`, replace the `@Description` line that names Phase 5:

```go
//	@Description	Returns the open Sales Shift with its Expected Cash and Cash Movements, or null when no Shift is open. expected_cash_vnd covers the Opening Float, Cash Payments, and Cash Movements. The Cash Refund term is still outstanding, because Refund is not implemented (ADR-020).
```

In `internal/shift/dto.go`, update the comment above `ExpectedCashVND` to name Refund rather than Phase 5.

- [ ] **Step 6: Run the unit test to verify it passes**

Run: `go test ./internal/shift/ -run TestComputeExpectedCashIncludesCashPayments -v && go build ./...`
Expected: PASS

- [ ] **Step 7: Write the integration test**

Create `internal/shift/expected_cash_integration_test.go`:

```go
//go:build integration

package shift_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExpectedCashCountsCashPayments(t *testing.T) {
	env := newShiftEnv(t)

	before := env.currentShift(t).ExpectedCashVND

	t.Run("a cash payment raises the figure by the applied amount", func(t *testing.T) {
		env.insertPayment(t, "CASH", 85_000, 100_000)
		require.Equal(t, before+85_000, env.currentShift(t).ExpectedCashVND,
			"the applied amount, not the tendered amount, is the net cash effect")
	})

	t.Run("a manual QR payment does not change it", func(t *testing.T) {
		current := env.currentShift(t).ExpectedCashVND
		env.insertPayment(t, "MANUAL_QR", 50_000, 0)
		require.Equal(t, current, env.currentShift(t).ExpectedCashVND)
	})

	t.Run("a payment attributed to another shift does not leak in", func(t *testing.T) {
		current := env.currentShift(t).ExpectedCashVND
		env.insertPaymentForShift(t, env.previousShiftID, "CASH", 70_000, 70_000)
		require.Equal(t, current, env.currentShift(t).ExpectedCashVND)
	})
}
```

Add `insertPayment`, `insertPaymentForShift`, `previousShiftID`, and `currentShift` to `internal/shift`'s integration test environment. `insertPayment` needs a Check to attach to; seed one with a directly inserted `service_sessions` + `checks` row, following the precedent Phase 3's tests set when they seeded `service_sessions` before `internal/sales` owned them. `insertPayment` with method `MANUAL_QR` must pass `NULL` for the cash columns, or `payment_method_facts_valid` rejects the row.

- [ ] **Step 8: Run the integration test and the whole Shift suite**

Run: `go test -tags integration ./internal/shift/ -v -p 1`
Expected: PASS, including the pre-existing Shift suites with their `expected_cash_vnd` assertions updated for the new term.

- [ ] **Step 9: Commit**

```bash
make fmt
git add sql/queries/shift.sql internal/shift internal/database/sqlc
git commit -m "feat(shift): complete Expected Cash with its Cash Payment term"
```

---

## Task 10: Concurrency, Documentation, And The Full Suite

**Files:**
- Create: `internal/sales/payment_concurrency_integration_test.go`
- Modify: `spec/decisions.md`, `MIGRATE_PLAN.md`, `internal/sales/routes_test.go`, `docs/swagger.json`, `docs/swagger.yaml`

**Interfaces:**
- Consumes: everything Tasks 1 through 9 produced.
- Produces: no new Go API. Documentation and the verified-green suite.

- [ ] **Step 1: Write the concurrency tests**

Create `internal/sales/payment_concurrency_integration_test.go`:

```go
//go:build integration

package sales_test

import (
	"sync"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/stretchr/testify/require"
)

// Two Payments on the same Check serialize on that Check's row lock.
func TestConcurrentPaymentsOnOneCheck(t *testing.T) {
	env := newSalesEnv(t)
	session := env.commitTakeawayDraft(t, 2)
	checkID := env.soleCheckID(t, session.ID)
	charge := env.checkCharge(t, checkID)

	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, _, errs[i] = env.payCash(t, checkID, charge, charge)
		}(i)
	}
	close(start)
	wg.Wait()

	var succeeded int
	for _, err := range errs {
		if err == nil {
			succeeded++
			continue
		}
		require.ErrorIs(t, err, sales.ErrPaymentExceedsBalance,
			"the losing payment must fail cleanly, not corrupt the balance")
	}
	require.Equal(t, 1, succeeded)
	require.Equal(t, 1, env.countPayments(t, checkID))

	got := env.getSession(t, session.ID)
	require.Equal(t, int64(0), got.Checks[0].BalanceVND)
	require.Equal(t, sales.CheckStateSettled, got.Checks[0].State)
}

// FOR SHARE on the parent rows is what lets these two proceed in parallel.
// Under the canonical FOR UPDATE they would serialize on the Session row.
func TestConcurrentPaymentsOnTwoChecksOfOneSession(t *testing.T) {
	env := newSalesEnv(t)
	session := env.commitTakeawayDraft(t, 2)
	firstID := env.soleCheckID(t, session.ID)
	allocations := env.checkAllocations(t, session.ID, firstID)

	got, _, err := env.splitToNewCheck(t, firstID, []sales.SplitItem{
		{CommittedItemID: allocations[0].CommittedItemID, Quantity: 1},
	})
	require.NoError(t, err)
	secondID := env.otherCheckID(t, got, firstID)

	firstCharge := env.checkCharge(t, firstID)
	secondCharge := env.checkCharge(t, secondID)

	start := make(chan struct{})
	var wg sync.WaitGroup
	var firstErr, secondErr error
	wg.Add(2)
	go func() { defer wg.Done(); <-start; _, _, firstErr = env.payCash(t, firstID, firstCharge, firstCharge) }()
	go func() { defer wg.Done(); <-start; _, _, secondErr = env.payCash(t, secondID, secondCharge, secondCharge) }()
	close(start)
	wg.Wait()

	require.NoError(t, firstErr)
	require.NoError(t, secondErr)

	final := env.getSession(t, session.ID)
	for _, check := range final.Checks {
		require.Equal(t, sales.CheckStateSettled, check.State)
	}
}

// A Split racing a Payment on a shared Check resolves to one of exactly two
// documented outcomes, never to a corrupted charge.
func TestSplitRacingPayment(t *testing.T) {
	env := newSalesEnv(t)
	session := env.commitTakeawayDraft(t, 2)
	checkID := env.soleCheckID(t, session.ID)
	allocations := env.checkAllocations(t, session.ID, checkID)

	start := make(chan struct{})
	var wg sync.WaitGroup
	var payErr, splitErr error
	wg.Add(2)
	go func() { defer wg.Done(); <-start; _, _, payErr = env.payCash(t, checkID, 1_000, 1_000) }()
	go func() {
		defer wg.Done()
		<-start
		_, _, splitErr = env.splitToNewCheck(t, checkID, []sales.SplitItem{
			{CommittedItemID: allocations[0].CommittedItemID, Quantity: 1},
		})
	}()
	close(start)
	wg.Wait()

	if payErr == nil && splitErr == nil {
		t.Fatal("a payment and a split on one check must not both succeed")
	}
	if splitErr != nil {
		require.ErrorIs(t, splitErr, sales.ErrCheckHasPayment)
	}

	// Whichever won, the projection must still reconcile.
	_ = env.getSession(t, session.ID)
}
```

- [ ] **Step 2: Run the concurrency tests**

Run: `go test -tags integration ./internal/sales/ -run 'TestConcurrent|TestSplitRacingPayment' -v -p 1 -count 3`
Expected: PASS on every repetition. `-count 3` is deliberate: a race that passes once has not been shown to be absent.

- [ ] **Step 3: Cover authorization on all four routes**

Append to `internal/sales/payments_integration_test.go`:

```go
// Every 5C route must deny a BARISTA and an actor whose capability was
// removed mid-session, and must change nothing when it does.
func TestPhase5CAuthorization(t *testing.T) {
	env := newSalesEnv(t)

	session := env.commitTakeawayDraft(t, 2)
	checkID := env.soleCheckID(t, session.ID)
	allocations := env.checkAllocations(t, session.ID, checkID)
	charge := env.checkCharge(t, checkID)

	calls := map[string]func(actor sales.Actor) (int, error){
		"pay cash": func(actor sales.Actor) (int, error) {
			_, status, err := env.payCashAs(t, actor, checkID, 1_000, 1_000)
			return status, err
		},
		"pay manual qr": func(actor sales.Actor) (int, error) {
			_, status, err := env.payManualQRAs(t, actor, checkID, 1_000, true, nil)
			return status, err
		},
		"split check": func(actor sales.Actor) (int, error) {
			_, status, err := env.splitToNewCheckAs(t, actor, checkID, []sales.SplitItem{
				{CommittedItemID: allocations[0].CommittedItemID, Quantity: 1},
			})
			return status, err
		},
		"merge checks": func(actor sales.Actor) (int, error) {
			_, status, err := env.mergeChecksAs(t, actor, checkID, uuid.New())
			return status, err
		},
	}

	for name, call := range calls {
		t.Run(name+" denies a barista", func(t *testing.T) {
			status, err := call(env.baristaActor)
			require.Error(t, err)
			require.Equal(t, http.StatusForbidden, status)
		})
	}

	t.Run("authority is reloaded inside the transaction", func(t *testing.T) {
		actor := env.newCashierActor(t)
		env.revokeCapability(t, actor, sales.CapSalesOperate)

		_, status, err := env.payCashAs(t, actor, checkID, charge, charge)
		require.Error(t, err)
		require.Equal(t, http.StatusForbidden, status)
		require.Equal(t, 0, env.countPayments(t, checkID))
	})
}
```

Add `payManualQRAs`, `splitToNewCheckAs`, `mergeChecksAs`, `newCashierActor`, and `revokeCapability` to `env_integration_test.go`, following the actor-swapping helpers 5B's authorization tests already use.

Run: `go test -tags integration ./internal/sales/ -run TestPhase5CAuthorization -v -p 1`
Expected: PASS

- [ ] **Step 4: Cover HTTP request validation**

Append to `internal/sales/http_test.go` (create it if the package does not have one, following the HTTP test style 5A established):

```go
func TestPhase5CRequestValidation(t *testing.T) {
	e, slices := newSalesHTTPHarness(t)

	cases := []struct {
		name   string
		method string
		path   string
		body   string
		status int
	}{
		{"cash with a non-positive applied amount", http.MethodPost,
			"/api/v1/sales/checks/" + uuid.New().String() + "/payments/cash",
			`{"request_id":"` + uuid.New().String() + `","applied_amount_vnd":0,"cash_tendered_vnd":1000}`,
			http.StatusBadRequest},
		{"cash with a malformed check id", http.MethodPost,
			"/api/v1/sales/checks/not-a-uuid/payments/cash",
			`{"request_id":"` + uuid.New().String() + `","applied_amount_vnd":1000,"cash_tendered_vnd":1000}`,
			http.StatusBadRequest},
		{"manual qr with an over-long reference", http.MethodPost,
			"/api/v1/sales/checks/" + uuid.New().String() + "/payments/manual-qr",
			`{"request_id":"` + uuid.New().String() + `","applied_amount_vnd":1000,` +
				`"receipt_observed_in_bank_app":true,"transaction_reference":"` + strings.Repeat("A", 101) + `"}`,
			http.StatusBadRequest},
		{"split with an unknown destination type", http.MethodPost,
			"/api/v1/sales/checks/" + uuid.New().String() + "/split",
			`{"request_id":"` + uuid.New().String() + `","destination":{"type":"SOMEWHERE"},` +
				`"items":[{"committed_item_id":"` + uuid.New().String() + `","quantity":1}]}`,
			http.StatusBadRequest},
		{"split with an empty item list", http.MethodPost,
			"/api/v1/sales/checks/" + uuid.New().String() + "/split",
			`{"request_id":"` + uuid.New().String() + `","destination":{"type":"NEW_CHECK"},"items":[]}`,
			http.StatusConflict},
		{"split to an existing check without an id", http.MethodPost,
			"/api/v1/sales/checks/" + uuid.New().String() + "/split",
			`{"request_id":"` + uuid.New().String() + `","destination":{"type":"EXISTING_CHECK"},` +
				`"items":[{"committed_item_id":"` + uuid.New().String() + `","quantity":1}]}`,
			http.StatusBadRequest},
		{"merge without a request id", http.MethodPost,
			"/api/v1/sales/checks/merge",
			`{"surviving_check_id":"` + uuid.New().String() + `","absorbed_check_id":"` + uuid.New().String() + `"}`,
			http.StatusBadRequest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := performAuthorizedRequest(t, e, slices, tc.method, tc.path, tc.body)
			require.Equal(t, tc.status, rec.Code, rec.Body.String())
		})
	}
}
```

Note the empty-item-list case is `409` rather than `400`: an empty list is well-formed JSON that describes a split which cannot happen, so it is `INVALID_CHECK_SPLIT` by §9.2, not a binding failure.

Add `newSalesHTTPHarness` and `performAuthorizedRequest` if the package lacks them, reusing whatever 5A's HTTP tests already do to mount the routes and attach a valid Bearer token.

Run: `go test ./internal/sales/ -run TestPhase5CRequestValidation -v`
Expected: PASS

- [ ] **Step 5: Assert the route table**

Append to `internal/sales/routes_test.go`:

```go
func TestPhase5CRoutesAreRegistered(t *testing.T) {
	routes := registeredSalesRoutes(t)
	require.Contains(t, routes, "POST /api/v1/sales/checks/:check_id/payments/cash")
	require.Contains(t, routes, "POST /api/v1/sales/checks/:check_id/payments/manual-qr")
	require.Contains(t, routes, "POST /api/v1/sales/checks/:check_id/split")
	require.Contains(t, routes, "POST /api/v1/sales/checks/merge")
	require.Len(t, routes, 18, "Phase 5C brings the Sales surface to eighteen operations")
}
```

Adapt `registeredSalesRoutes` to whatever helper `routes_test.go` already uses to enumerate the Echo route table.

Run: `go test ./internal/sales/ -run TestPhase5CRoutesAreRegistered -v`
Expected: PASS

- [ ] **Step 6: Append the decision records**

Append ADR-016 through ADR-020 to `spec/decisions.md`, copying the five records verbatim from §12 of the spec, formatted to match the existing entries in that file (a `## ADR-0NN: Title` heading followed by `* **Decision Date:**`, `* **Status:**`, `* **Context:**`, `* **Decision:**`, `* **Consequences:**`).

ADR-020 must state explicitly that it supersedes ADR-008's claim that "Phase 5 adds the Cash Payment and Cash Refund terms", since Phase 5 adds only the first. Add a one-line pointer at the end of ADR-008 itself: `* **Superseded in part by ADR-020** for the Cash Refund term.`

- [ ] **Step 7: Update MIGRATE_PLAN.md**

In the Phase 5 sub-phase table, replace the 5C row:

```markdown
| **5C** — Cash and Manual QR Payments, Check splitting and merging, settlement | [spec](docs/superpowers/specs/2026-09-14-sales-payments-settlement-design.md) / [plan](docs/superpowers/plans/2026-09-14-sales-payments-settlement.md) | ✅ COMPLETED (2026-09-14) |
```

Leave the Phase 5 detail checklist and the tracker row untouched; the tracker stays pending until 5D.

- [ ] **Step 8: Regenerate Swagger**

Run: `make swagger`
Expected: `docs/swagger.json` and `docs/swagger.yaml` gain all four Sales routes with `BearerAuth`, and the Shift description no longer names Phase 5 as the outstanding Expected Cash dependency.

Verify: `grep -c 'sales/checks' docs/swagger.json` returns a non-zero count, and `grep 'Cash Refund' docs/swagger.json` finds the updated Shift description.

- [ ] **Step 9: Run the full suite**

Run:

```bash
make fmt
make lint
go test ./...
go test -tags integration ./... -p 1
```

Expected: all green. If the Auth, Catalog, Tables, or 5A/5B Sales suites fail, the failure is a 5C regression — fix it rather than adjusting the assertion, except for the Shift suite's `expected_cash_vnd` figures, which legitimately change with the completed formula.

- [ ] **Step 10: Commit**

```bash
git add internal/sales/payment_concurrency_integration_test.go internal/sales/payments_integration_test.go internal/sales/http_test.go internal/sales/routes_test.go spec/decisions.md MIGRATE_PLAN.md docs/swagger.json docs/swagger.yaml
git commit -m "docs(sales): record Phase 5C decisions and verify the full suite"
```

---

## Verification Checklist

Run before opening the pull request. Every line must be confirmed by command output, not by reading the code.

- [ ] `go test ./...` passes.
- [ ] `go test -tags integration ./... -p 1` passes.
- [ ] `make lint` passes.
- [ ] `go test -tags integration ./internal/sales/ -run 'TestConcurrent|TestSplitRacingPayment' -p 1 -count 3` passes on every repetition.
- [ ] `grep -rn "internal/sales" internal/shift/*.go | grep -v _test` returns nothing — the Shift slice imports no Sales package.
- [ ] `grep -rn "internal/catalog\|internal/tables\|internal/shift" internal/sales/*.go | grep -v _test` returns nothing.
- [ ] `grep -rn "receipt_observed_in_bank_app" internal/database/migrations/` returns nothing — the attestation is never a column.
- [ ] `grep -rn "MAX_SAFE_INTEGER\|9007199254740991" internal/ sql/` returns nothing.
- [ ] No route named `settle` exists: `grep -rn '"/sales/checks.*settle"' internal/sales/routes.go` returns nothing.
- [ ] The Sales route table has exactly eighteen entries.
- [ ] `spec/decisions.md` contains ADR-016 through ADR-020, and ADR-008 carries its supersession pointer.
