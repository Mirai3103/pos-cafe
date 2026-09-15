# Design Specification: Payments, Settlement & Check Restructuring (`internal/sales`, Phase 5C)

- **Author:** Claude Opus 5 & Team
- **Date:** 2026-09-14
- **Status:** Draft
- **Phase:** Phase 5C, the third of four sub-phases of Phase 5 (Core Sales, Orders & Payments)
- **Predecessors:** [`2026-09-13-sales-session-draft-design.md`](2026-09-13-sales-session-draft-design.md) (5A), [`2026-09-14-sales-commit-checks-design.md`](2026-09-14-sales-commit-checks-design.md) (5B)

---

## 1. Purpose

This specification defines the Go implementation of the point at which money arrives: Cash and Manual QR Payments, the settlement transition that closes a fully paid Check, and the two restructuring operations — Split and Merge — that let staff arrange Checks before any money is taken. It also completes the `expected_cash_vnd` figure that ADR-008 shipped partial in Phase 4, because 5C is the first sub-phase that creates the Cash Payment term that figure was missing.

As in 5A and 5B, the implementation may improve structure, schema, and correctness, but the observable business behavior must match the canonical source in `cafe-pos/src/sales` and the definitions in `cafe-pos/CONTEXT.md`. Where the TypeScript runtime behavior conflicts with those documents, the canonical documents win. Known implementation defects are corrected rather than migrated, and every deviation is recorded as an ADR.

### Goals

1. Implement Cash Payment and Manual QR Payment as one execution path with two input shapes, guarded so that a Check can never be overpaid.
2. Introduce the `payments` table, and complete `checks` with the five settlement columns and the composite constraint ADR-014 deferred.
3. Settle a Check automatically, in the same transaction, when a Payment brings its balance to zero — recording who settled it, during which Sales Shift, and on which access session.
4. Implement Split Check and Merge Checks, both forbidden on any Check that has already received a Payment.
5. Populate the `payments` array and the `total_applied_vnd` / `balance_vnd` figures of the Service Session projection, which 5B shipped as documented placeholders.
6. Complete `internal/shift`'s Expected Cash with its Cash Payment term.
7. Keep every mutation atomically idempotent and every successful state change auditable, on the mechanism 5A established.

### Non-Goals

- **Refund, Payment Void, Post-Shift Payment Correction, and Comp.** These are outside Phase 5 entirely. This is why `pending_refund_vnd` remains absent from the contract rather than stubbed, and why the canonical `evaluateCheckSettlement`'s `hasPendingRefund` and `hasCustomerExcess` parameters are not migrated (§6.3, ADR-017).
- **The Cash Refund term of Expected Cash.** It has no data source in Phase 5. ADR-020 records which term remains missing and why.
- **Submit, Orders, Preparation Units, Service Session closure, and Completed Sale.** These belong to 5D. The `submitted` flag on a Charge Allocation is still projected as a constant `false`.
- **Cancellation-adjusted allocation quantities.** `preparation_cancellations` is a Phase 6 table, as 5B recorded.
- Reporting, exports, historical sale listings, and receipt or fiscal invoice documents.

### Accepted Consequences Of Deferring Submit

5B recorded that a Service Session accepts exactly one Commit until 5D introduces the `orders` table, and that this made the `CURRENT_UNPAID` branch of Check targeting unreachable through the API, forcing a seeded fixture.

**5C does not inherit that limitation.** Split with a `NEW_CHECK` destination creates a second Check out of a single Commit. Every 5C branch is therefore reachable through the public API: commit a draft holding two or more items, split one item onto a new Check, and both `EXISTING_CHECK` splitting and Merge become exercisable against real command output. 5C seeds no fixtures.

One consequence does persist: a Session cannot commit twice, so a Check cannot grow after its first Commit. Nothing in 5C depends on that.

As with 5A and 5B, 5C is not a deployable end state. It is a complete, testable consistency boundary.

---

## 2. Authority And Terminology

5C adds these terms from `cafe-pos/CONTEXT.md`:

- A **Payment** is a confirmed, immutable receipt of money applied to a Check, recorded independently from ordering and preparation; an attempted or unverified transfer is not a Payment.
- A **Cash Payment** is a Payment recording the amount applied to a Check separately from cash tendered and change due; **its net cash effect is the applied amount**. That last clause is the authority for §6.6.
- A **Manual QR Payment** is a bank-transfer Payment for the exact amount staff confirm as received in the bank account, without cash-style change or automatic bank or payment-gateway confirmation. The absence of automatic confirmation is the reason §6.2 requires an explicit staff attestation.
- A **Settled Check** is a Check whose valid Payments leave exactly zero balance with no pending Refund or customer excess; **it accepts no further merge or split operations**.
- **Expected Cash** is the Sales Shift's calculated cash responsibility: Opening Float plus Cash Payments and Pay Ins, less Cash Refunds and Pay Outs.

A Payment's immutability, like a Committed Item's, is a rule about this system's writes rather than a database guarantee: no command in Phase 5 updates a `payments` row after its insert. Corrections are Payment Void and Post-Shift Payment Correction, both outside Phase 5, and both append-only by definition.

---

## 3. Position In Phase 5

Per ADR-010, Phase 5 ships as four strictly ordered sub-phases. 5A delivered the Service Session lifecycle, Table assignments, and the Order Draft; 5B delivered Commit and the Check as a grouping of charges owed. 5C delivers the settlement of that debt and the restructuring that precedes it. 5D adds Submit, Orders, Preparation Units, and closure.

5C depends on 5B in four specific places: the `checks` table and its complete state domain (ADR-014), the `charge_allocations` table in its final shape, the stored-charge invariant of 5B §5.6, and the executor with its authority reload, idempotency claim, and audit insertion.

It also settles one debt from Phase 4: ADR-008's partial Expected Cash.

---

## 4. Architecture

`internal/sales` gains the Payment and Check-restructuring commands. No new package is created: a Payment is bounded by the Check it settles, and a Check belongs to the Service Session that `internal/sales` already owns as one consistency boundary.

The package gains, in 5C:

- `payments.go` — both Payment commands on one execution path, and the settlement transition.
- `check_restructuring.go` — Split and Merge.
- Payment DTOs and the settlement fields, extending `dto.go`; Payment assembly, extending `projection.go`.

`internal/shift` gains exactly one sqlc query and one additional parameter on `ComputeExpectedCash`. **It does not import `internal/sales`.** It reads the `payments` table through its own sqlc query, in the same way it already reads `cash_movements`, and in the same way `internal/sales` resolves Catalog through its own SQL rather than importing `internal/catalog` (ADR-012). `internal/sales` still imports no slice but `internal/auth`.

Each handler continues to own a narrow, consumer-defined interface containing only the generated sqlc operations it uses.

### 4.1 Operations

| Operation | Capability | Manager approval | Idempotent |
| --- | --- | --- | --- |
| Pay cash | `sales.operate` | No | Yes |
| Pay manual QR | `sales.operate` | No | Yes |
| Split check | `sales.operate` | No | Yes |
| Merge checks | `sales.operate` | No | Yes |

No 5C operation requires Manager Approval, matching the canonical source: second-party approval enters the Sales domain only with Refund, Payment Void, and Comp. Taking a customer's money is the ordinary work of a cashier, not an exception to it. Reversing it is the exception, and it is not in Phase 5.

Action names for the shared `idempotency_keys` table, fully qualified per ADR-007: `sales.pay_cash`, `sales.pay_manual_qr`, `sales.split_check`, `sales.merge_checks`. The longest is 20 characters and fits the existing `action VARCHAR(50)` column.

Operation constants follow the established naming: `OpPayCash`, `OpPayManualQR`, `OpSplitCheck`, `OpMergeChecks`.

### 4.2 Command Flow

Unchanged from 5A §4.2. Authority is reloaded inside the transaction and **before** idempotent replay; the open-Sales-Shift requirement is evaluated **after** the idempotency claim, so a replay of a Payment that succeeded during a Shift still returns its stored result once that Shift has closed.

The replay-returns-a-snapshot consequence that 5B called latent is now observable. A replayed Commit response shows the Check as it stood at commit time, with a balance that a later Payment may have reduced. This is correct — idempotency reproduces the original outcome, not the current state — and both the Commit and the Payment Swagger descriptions say so.

---

## 5. Database Design

Migration `000010_create_sales_payment_slice.sql`.

All monetary columns are `BIGINT`, with positivity and relational guards only and no `MAX_SAFE_INTEGER`-derived ceiling, per ADR-013.

### 5.1 Completing `checks`

```sql
ALTER TABLE checks
    ADD COLUMN merged_into_check_id            UUID REFERENCES checks(id) ON DELETE RESTRICT,
    ADD COLUMN settled_at                      TIMESTAMPTZ,
    ADD COLUMN settled_by_staff_identity_id    UUID REFERENCES staff_identities(id) ON DELETE RESTRICT,
    ADD COLUMN settled_during_sales_shift_id   UUID REFERENCES sales_shifts(id) ON DELETE RESTRICT,
    ADD COLUMN settled_staff_access_session_id UUID REFERENCES staff_access_sessions(id) ON DELETE RESTRICT;
```

These are the five columns ADR-014 deferred, added exactly as that record promised, without rewriting the `check_state_valid` constraint 5B shipped complete.

The composite constraint ties the evidence to the state:

```sql
ALTER TABLE checks ADD CONSTRAINT check_settlement_evidence_valid CHECK (
       (state = 'OPEN'
            AND merged_into_check_id IS NULL
            AND settled_at IS NULL AND settled_by_staff_identity_id IS NULL
            AND settled_during_sales_shift_id IS NULL
            AND settled_staff_access_session_id IS NULL)
    OR (state = 'SETTLED'
            AND merged_into_check_id IS NULL
            AND settled_at IS NOT NULL AND settled_by_staff_identity_id IS NOT NULL
            AND settled_during_sales_shift_id IS NOT NULL
            AND settled_staff_access_session_id IS NOT NULL)
    OR (state = 'MERGED'
            AND merged_into_check_id IS NOT NULL
            AND charge_vnd = 0
            AND settled_at IS NULL AND settled_by_staff_identity_id IS NULL
            AND settled_during_sales_shift_id IS NULL
            AND settled_staff_access_session_id IS NULL)
);
```

This constraint is the centrepiece of 5C's schema. It makes "a Check that is settled but does not know who settled it, or during which Shift" unrepresentable. Settlement evidence is recorded along all four dimensions — person, Shift, access session, and time — because this is cash-reconciliation data, not metadata.

The `MERGED` branch requires `charge_vnd = 0`, which is self-consistent with 5B's stored-charge invariant: Merge either moves or absorbs every allocation of the absorbed Check, so the live sum over its allocations is exactly zero. The 5B read path needs no relaxation.

5B's partial index `check_open_per_session_index ... WHERE state = 'OPEN'` is unchanged. From 5C its filter does real work, exactly as 5B §6.4 anticipated.

### 5.2 `payments`

| Column | Type | Notes |
| --- | --- | --- |
| `id` | `UUID` PK | |
| `check_id` | `UUID NOT NULL` | references `checks(id)` `ON DELETE RESTRICT` |
| `sales_shift_id` | `UUID NOT NULL` | references `sales_shifts(id)` `ON DELETE RESTRICT` |
| `actor_staff_identity_id` | `UUID NOT NULL` | references `staff_identities(id)` `ON DELETE RESTRICT` |
| `staff_access_session_id` | `UUID NOT NULL` | references `staff_access_sessions(id)` `ON DELETE RESTRICT` |
| `applied_amount_vnd` | `BIGINT NOT NULL` | |
| `method` | `TEXT NOT NULL` | `CHECK (method IN ('CASH', 'MANUAL_QR'))` |
| `cash_tendered_vnd` | `BIGINT` | `CASH` only |
| `change_due_vnd` | `BIGINT` | `CASH` only |
| `transaction_reference` | `TEXT` | `MANUAL_QR` only, nullable |
| `received_at` | `TIMESTAMPTZ NOT NULL DEFAULT now()` | |

`sales_shift_id` is **stored on the Payment** rather than derived through `check → service_session → sales_shift`. This is a reconciliation record: the Shift in which money actually reached the cashier's hands is a fact worth freezing, and a Session opened in one Shift can be paid in the next. The derived path would answer that question wrongly. It is also what makes `internal/shift`'s Expected Cash query a single-table scan.

The composite constraint, migrated whole except for the `MAX_SAFE_INTEGER` ceilings:

```sql
CONSTRAINT payment_method_facts_valid CHECK (
       (method = 'CASH'
            AND cash_tendered_vnd IS NOT NULL AND change_due_vnd IS NOT NULL
            AND transaction_reference IS NULL
            AND applied_amount_vnd > 0
            AND cash_tendered_vnd >= applied_amount_vnd
            AND change_due_vnd = cash_tendered_vnd - applied_amount_vnd)
    OR (method = 'MANUAL_QR'
            AND cash_tendered_vnd IS NULL AND change_due_vnd IS NULL
            AND applied_amount_vnd > 0
            AND (transaction_reference IS NULL
                 OR (transaction_reference = btrim(transaction_reference)
                     AND char_length(transaction_reference) BETWEEN 1 AND 100)))
)
```

The change-due identity is asserted in the database alongside being computed in Go, in the same spirit as 5B's `total_vnd = quantity * unit_price_vnd`. The two agreeing is the invariant; a `23514` here means they disagree, which is a defect (§10).

**There is no `receipt_observed_in_bank_app` column.** It is a must-be-true attestation at data-entry time, not an event with two possible outcomes: a Manual QR Payment does not exist unless staff saw the money arrive. Storing a column that is constant `true` on every row stores nothing. The attestation lives where it is meaningful — in the request body, where it is required, and in the audit event, where it records what the staff member asserted and when. See ADR-019.

### 5.3 Indexes

```sql
CREATE INDEX payment_check_index ON payments (check_id, received_at, id);
CREATE INDEX payment_cash_shift_index ON payments (sales_shift_id) WHERE method = 'CASH';
```

The first serves both grouping Payments by Check and the `(received_at, id)` presentation order of §9.1. The second is a partial index serving the one query `internal/shift` adds (§6.6) directly.

### 5.4 Idempotency And Audit Storage

Unchanged. Mutations write to the shared `idempotency_keys` table (ADR-005, ADR-007); audit rows go to `audit_events` (Phase 2). No new idempotency table is created.

---

## 6. Business Rules

### 6.1 The Uniform Check Lock Protocol

The canonical source uses two different locking protocols. `payments.ts` locks `checks`, `service_sessions`, and `sales_shifts` all `FOR UPDATE` in one joined statement. `check-restructuring.ts` locks `FOR UPDATE OF checks` only, and carries a comment explaining that locking the joined parent rows "can create a reverse dependency when Payment already owns one of the affected Check rows" — a deadlock hazard documented rather than removed.

5C uses one protocol for all four commands:

1. `checks` rows — `FOR UPDATE`, in ascending id order.
2. `service_sessions` and `sales_shifts` — `FOR SHARE`.

`FOR SHARE` is the semantically correct lock: all four commands only *read* Session and Shift state to evaluate a precondition; none of them writes those rows. Two cashiers paying two different Checks of the same Session therefore do not serialize against each other, while Session closure (5D) and Shift closure — both of which take `FOR UPDATE` — remain excluded for the duration of the transaction. This is the reasoning of ADR-015 applied to parent rows instead of Catalog rows, and it preserves 5A's Sales-before-Catalog lock order untouched. See ADR-016.

### 6.2 Recording A Payment

Both Payment commands share one execution path, differing in input validation and in the row they build.

Constraints of the form "this field must be positive", "this string must be trimmed and at most 100 characters" are **request validation**, not domain error codes, following 5A's convention that validation messages use JSON field names. They run before the idempotency claim, so a malformed request never consumes a `request_id`.

Inside the transaction, in this order:

1. Acquire the Check under the §6.1 protocol. The lock query and the precondition query are separate, so the specific failure is distinguishable: a Check that does not exist yields `CHECK_NOT_FOUND`; one that is not `OPEN` yields `CHECK_NOT_OPEN`; a closed Session yields `SERVICE_SESSION_ALREADY_CLOSED`; a Shift that is not open yields `OPEN_SALES_SHIFT_REQUIRED`. The canonical source collapses all four into one empty `WHERE` result and a single code; a cashier told which of the four is wrong can act, and told only "check not open for cash payment" cannot.
2. Recompute the Check's charge from its allocations and compare it against the stored `checks.charge_vnd`. A mismatch is a defect: it surfaces as a 500 with the Check id logged, per 5B §5.6.
3. `balance = charge − Σ applied`. If `applied_amount_vnd > balance` → `PAYMENT_EXCEEDS_CHECK_BALANCE`. This guard is what makes overpayment unrepresentable, and therefore what makes `pending_refund_vnd` unnecessary throughout Phase 5.
4. `CASH` only: if `cash_tendered_vnd < applied_amount_vnd` → `INSUFFICIENT_CASH_TENDERED`. Otherwise `change_due_vnd = cash_tendered_vnd − applied_amount_vnd`.
5. `MANUAL_QR` only: `receipt_observed_in_bank_app` must be `true` → `MANUAL_QR_RECEIPT_CONFIRMATION_REQUIRED`. This code is kept distinct even though it is formally "a field must equal true", because it expresses a policy — no Payment exists before a staff member has seen the money arrive in the bank account — and the cashier screen needs to say that rather than report a generic validation failure.
6. Insert the `payments` row, taking `sales_shift_id` from the Session's Shift.
7. Write one `CASH_PAYMENT_RECORDED` or `MANUAL_QR_PAYMENT_RECORDED` audit event. The `receipt_observed_in_bank_app` attestation lives here, in the event details.
8. Evaluate settlement (§6.3).

### 6.3 Settlement Is A Consequence

If the balance after applying this Payment is zero, the same transaction moves the Check to `SETTLED`, writes all four evidence columns from the reloaded actor and the Session's Shift, and emits a `CHECK_SETTLED` audit event.

There is no "settle check" command and no settlement route. A Check that has been paid in full but is still `OPEN` is a state nobody wants to exist, so it is not permitted to exist: the constraint of §5.1 and the read invariant of §9.1 guard it from both sides.

A final Payment therefore writes **two** audit events in one transaction. Payment and settlement are two distinct facts about the business, not one, and a reconciliation reading the audit log must be able to see the moment the Check closed independently of the money that closed it.

The canonical `evaluateCheckSettlement` is **not** migrated as a three-parameter policy function. Its other two inputs, `hasPendingRefund` and `hasCustomerExcess`, are constant `false` with no data source anywhere in Phase 5; keeping them would pre-build an extension point for a module that has not been designed. The settlement condition in 5C is `balance == 0`, and it is written as exactly that. When Refund arrives it will bring its own definition of readiness. See ADR-017.

### 6.4 Split Check

Split moves part of a Check's charge onto another Check, before any money has been taken.

Input: `source_check_id`, a `destination` that is either `NEW_CHECK` or `EXISTING_CHECK` with a `check_id`, and a list of `items`, each a `committed_item_id` and a `quantity`.

- An empty list, a duplicated `committed_item_id`, or a non-positive `quantity` → `INVALID_CHECK_SPLIT`. A destination equal to the source → the same code.
- Both Checks must exist (`CHECK_NOT_FOUND`), be `OPEN` (`CHECK_NOT_OPEN`), and belong to the same Service Session (`CHECKS_DIFFERENT_SERVICE_SESSION`). The Session must be `ACTIVE` and the Shift `OPEN`.
- **Any Payment on either Check blocks the whole operation** → `CHECK_HAS_PAYMENT`. This is the central rule of 5C. Arranging Checks is work done before money is taken; once money has been taken, a Check's structure is reconciliation evidence rather than a sorting tool. `CONTEXT.md` states it directly: a Settled Check accepts no further merge or split operations. The rule here is stricter, and deliberately so — it blocks on the *first* Payment, not on settlement, because a partially paid Check is already evidence.
- Each item must have an allocation on the source Check (`SPLIT_ALLOCATION_NOT_FOUND`) whose quantity is not exceeded (`SPLIT_QUANTITY_EXCEEDS_ALLOCATION`).
- The source's remaining charge must be `> 0` (`SPLIT_SOURCE_WOULD_BE_EMPTY`) and the destination's resulting charge must be `> 0` (`SPLIT_DESTINATION_WOULD_BE_EMPTY`). A Check cannot be split empty; moving everything is Merge, which says what it means.

Writes, in one transaction: the source allocation is reduced by the moved quantity, or deleted when the whole quantity moves; the destination allocation is increased when one already exists for that Committed Item, otherwise inserted; both Checks' `charge_vnd` are rewritten; and a `NEW_CHECK` destination is created in the same transaction, never observable at zero. One `CHECK_SPLIT` audit event carries `service_session_id`, both Check ids, and the moved items.

Moved amounts accumulate through the same guarded arithmetic as 5B, raising `CHECK_CHARGE_OUT_OF_RANGE` on overflow.

### 6.5 Merge Checks

Merge absorbs one Check into another. Input: `surviving_check_id` and `absorbed_check_id`.

The same preconditions apply: the two ids must differ (`INVALID_CHECK_MERGE`), both Checks must exist and be `OPEN`, belong to the same Session, carry no Payment, and the Session and Shift must be `ACTIVE` and `OPEN`.

Each allocation of the absorbed Check is added to the surviving Check's allocation for the same Committed Item when one exists, and otherwise simply has its `check_id` rewritten. The surviving Check's `charge_vnd` gains the absorbed charge; the absorbed Check moves to `MERGED` with `charge_vnd = 0` and `merged_into_check_id` pointing at the survivor — the three facts the `MERGED` branch of §5.1 requires. One `CHECK_MERGED` audit event.

### 6.6 Completing Expected Cash

`internal/shift` adds one query:

```sql
-- name: SumCashPaymentsForShift :one
SELECT COALESCE(SUM(applied_amount_vnd) FILTER (WHERE method = 'CASH'), 0)::BIGINT AS cash_payment_vnd
FROM payments
WHERE sales_shift_id = $1;
```

`ComputeExpectedCash` gains one parameter, and the formula becomes Opening Float plus Cash Payments and Pay Ins, less Pay Outs.

The term sums **applied** amounts, not tendered amounts. `CONTEXT.md` defines a Cash Payment as recording the amount applied to the Check separately from cash tendered and change due, and states that its net cash effect is the applied amount: the change left the drawer at the same moment the tendered cash entered it.

The range guard stays symmetric — sustained Pay Outs can still drive the figure legitimately negative.

The Cash Refund term remains missing, and the Swagger description says so, naming Refund rather than "Phase 5" as the outstanding dependency. See ADR-020.

---

## 7. Authorization

Unchanged from 5A §8. Every operation requires `sales.operate`, held by `MANAGER` and `CASHIER` and not by `BARISTA`. Authority is reloaded inside the transaction, covering the identity's enabled flag, the access session's state and expiry, and the current role set. A denial writes a `sales.authorization_denied` audit event and returns `NOT_AUTHORIZED`, with the specific reason reaching the server log and the audit event only.

The reloaded actor is also the source of the `settled_by_staff_identity_id` and `settled_staff_access_session_id` evidence in §6.3, so settlement can never be attributed to an authority that was not verified in the same transaction.

---

## 8. REST API

Four routes are added under `/api/v1/sales`, mounted directly on `v1` in the manner `routes.go` already documents, each with `RequireAuth()` and `RequireCapability(CapSalesOperate)`.

| Method | Path | Operation |
| --- | --- | --- |
| POST | `/sales/checks/{check_id}/payments/cash` | Record a Cash Payment |
| POST | `/sales/checks/{check_id}/payments/manual-qr` | Record a Manual QR Payment |
| POST | `/sales/checks/{check_id}/split` | Split a Check |
| POST | `/sales/checks/merge` | Merge two Checks |

The routes are Check-centric rather than nested under `/service-sessions/{id}`, because all four canonical commands are keyed by Check id. Nesting would add a path parameter that carries no information and must then be validated against the Check it supposedly contains, and Merge — which acts on two peer Checks — has no natural Session-nested shape at all.

Request bodies:

| Route | Body |
| --- | --- |
| `POST .../payments/cash` | `request_id`, `applied_amount_vnd`, `cash_tendered_vnd` |
| `POST .../payments/manual-qr` | `request_id`, `applied_amount_vnd`, `receipt_observed_in_bank_app`, `transaction_reference?` |
| `POST .../split` | `request_id`, `destination`, `items[]` |
| `POST .../merge` | `request_id`, `surviving_check_id`, `absorbed_check_id` |

`destination` is `{"type": "NEW_CHECK"}` or `{"type": "EXISTING_CHECK", "check_id": "..."}`. Each `items[]` entry is a `committed_item_id` and a `quantity` in 1–9,999.

All four return the complete Service Session projection, as every mutation since 5A does, so a client never has to issue a second call to learn the state its command produced.

---

## 9. Response Contract

Two changes to the projection, plus one new read invariant.

### 9.1 `checks[].payments` Is Populated

The shape depends on the method. A Cash Payment carries `cash_tendered_vnd` and `change_due_vnd`; a Manual QR Payment carries `transaction_reference`. The method-dependent fields are pointers with `omitempty`, so a Manual QR Payment does not carry two null cash fields:

```json
{
  "id": "...",
  "method": "CASH",
  "applied_amount_vnd": 85000,
  "cash_tendered_vnd": 100000,
  "change_due_vnd": 15000,
  "sales_shift_id": "...",
  "received_at": "..."
}
```

Payments are ordered by `(received_at, id)`.

`total_applied_vnd` and `balance_vnd`, which 5B shipped as documented zeroes, now carry real values. `pending_refund_vnd` remains absent, per §1.

A Check additionally carries `merged_into_check_id`, present only when `state` is `MERGED` and `omitempty` otherwise — an open Check does not carry a field pointing nowhere.

`submitted` on each allocation is still a constant `false`, documented as completed by 5D.

**New read invariant.** For every Check that is not `MERGED`: `(state == "SETTLED") == (balance_vnd == 0)`. A violation is a defect and surfaces as a 500 with the Check id logged, never as a client-visible error code. This is the other half of the pair guarding §6.3: the database guarantees that settlement evidence is complete, and the read path guarantees that the state matches the balance. A `MERGED` Check missing its `merged_into_check_id` is likewise a 500.

Empty collections serialize as `[]`, never `null`.

### 9.2 Errors

5C adds thirteen codes:

`CHECK_NOT_FOUND`, `CHECK_NOT_OPEN`, `CHECK_HAS_PAYMENT`, `CHECKS_DIFFERENT_SERVICE_SESSION`, `PAYMENT_EXCEEDS_CHECK_BALANCE`, `INSUFFICIENT_CASH_TENDERED`, `MANUAL_QR_RECEIPT_CONFIRMATION_REQUIRED`, `INVALID_CHECK_SPLIT`, `SPLIT_ALLOCATION_NOT_FOUND`, `SPLIT_QUANTITY_EXCEEDS_ALLOCATION`, `SPLIT_SOURCE_WOULD_BE_EMPTY`, `SPLIT_DESTINATION_WOULD_BE_EMPTY`, `INVALID_CHECK_MERGE`.

It reuses `NOT_AUTHORIZED`, `OPEN_SALES_SHIFT_REQUIRED`, `SERVICE_SESSION_ALREADY_CLOSED`, `CHECK_CHARGE_OUT_OF_RANGE`, `REQUEST_CONFLICT`, and `INVALID_STORED_RESULT`.

The canonical source declares twenty-seven codes across this surface. The mapping, carried in full so a reader of the canonical source can trace any code:

| Canonical | 5C |
| --- | --- |
| `CHECK_NOT_OPEN_FOR_CASH_PAYMENT`, `CHECK_NOT_OPEN_FOR_MANUAL_QR_PAYMENT`, `CHECK_NOT_OPEN_FOR_SPLIT`, `CHECK_NOT_OPEN_FOR_MERGE` | `CHECK_NOT_OPEN` |
| `SPLIT_CHECK_NOT_FOUND`, `SPLIT_DESTINATION_CHECK_NOT_FOUND`, `MERGE_CHECK_NOT_FOUND` | `CHECK_NOT_FOUND` |
| `CHECK_HAS_PAYMENT`, `MERGE_CHECK_HAS_PAYMENT` | `CHECK_HAS_PAYMENT` |
| `SPLIT_CHECKS_DIFFERENT_SERVICE_SESSION`, `MERGE_CHECKS_DIFFERENT_SERVICE_SESSION` | `CHECKS_DIFFERENT_SERVICE_SESSION` |
| `SERVICE_SESSION_NOT_OPEN_FOR_SPLIT`, `MERGE_SERVICE_SESSION_NOT_OPEN` | `SERVICE_SESSION_ALREADY_CLOSED` (5A) |
| `SALES_SHIFT_NOT_OPEN_FOR_SPLIT`, `MERGE_SALES_SHIFT_NOT_OPEN` | `OPEN_SALES_SHIFT_REQUIRED` (5A) |
| `INVALID_CASH_PAYMENT_AMOUNTS`, `INVALID_MANUAL_QR_PAYMENT_AMOUNT`, `INVALID_MANUAL_QR_TRANSACTION_REFERENCE` | request validation (§6.2) |
| the remaining nine | migrated unchanged |

See ADR-018. Status mapping: `CHECK_NOT_FOUND` is `404`; every other new code describes a state the caller must resolve and is `409`; `CHECK_CHARGE_OUT_OF_RANGE` stays `422` as in 5B.

Swagger annotates all four operations with Bearer security, request DTOs, the projection response, and error status codes, bringing the documented Sales surface to eighteen operations.

---

## 10. Transactions And Concurrency

All four mutations run in one `READ COMMITTED` transaction each. Reads remain read-only `REPEATABLE READ`.

Lock order, per §6.1: `checks` `FOR UPDATE` in ascending id order, then `service_sessions` and `sales_shifts` `FOR SHARE`. Because all four commands take the same locks in the same order, no deadlock cycle exists among them, and none of them can cycle against 5A or 5B, whose Sales-before-Catalog order is unchanged.

- **Two Payments on the same Check** serialize on that Check's row lock. The loser recomputes the balance and may receive `PAYMENT_EXCEEDS_CHECK_BALANCE` — correct, because the Check was paid off while it waited. If both carried the same `request_id`, idempotency returns the stored result instead.
- **Two Payments on different Checks of the same Session** proceed in parallel, which is the point of `FOR SHARE` on the parent rows. §11.2 asserts it.
- **Split or Merge racing a Payment** on a shared Check serializes on that Check's row lock; whichever runs second sees the other's outcome and fails cleanly with `CHECK_HAS_PAYMENT` or `PAYMENT_EXCEEDS_CHECK_BALANCE`.
- **`SumCashPaymentsForShift`** runs in `internal/shift`'s read-only transaction and takes no locks.

**Expected constraint violations.** A `23514` on `payment_method_facts_valid` means Go's change-due arithmetic and the database's disagree, which is a defect and surfaces as a 500 with the constraint name logged. A `23514` on `check_settlement_evidence_valid` means a settlement or merge wrote incomplete evidence, likewise a defect. A `23505` on `charge_allocation_item_check_unique` during Split or Merge means the upsert logic failed to find an existing allocation it should have found, also a defect.

Watermill publishes nothing in 5C. The first genuine event consumer remains the Preparation queue in Phase 6, fed by 5D's Submit.

---

## 11. Testing

### 11.1 Unit Tests

- Change-due arithmetic: exact tender, over-tender, and the rejected under-tender.
- Balance arithmetic: a partial Payment, a second partial Payment reaching zero, and an over-balance Payment at the boundary (`applied == balance` succeeds, `applied == balance + 1` fails).
- The settlement condition: `balance == 0` settles, any positive balance does not.
- Manual QR attestation: `false` and absent both rejected; `transaction_reference` null, empty-after-trim, at 100 characters, and at 101 characters.
- Split arithmetic: partial quantity moved, whole quantity moved, source-would-be-empty at the boundary, and the overflow guard.
- Merge arithmetic: disjoint allocations, overlapping allocations combining into one, and the resulting charges.
- Expected Cash: Cash Payments included, Manual QR Payments excluded, applied rather than tendered amounts summed, and the symmetric range guard.
- Request fingerprint stability for all four commands, including the item list of a Split sorted canonically so that a reordered list is the same request.
- Domain error to HTTP status mapping for every new code.
- DTO serialization: a Cash Payment's fields, a Manual QR Payment's fields with the cash fields absent, `merged_into_check_id` present only when `MERGED`, `pending_refund_vnd` absent, `submitted` constant `false`, empty collections as `[]`.

### 11.2 PostgreSQL Integration Tests

Derived from the canonical `manual-qr-payment.integration.test.ts`, `mixed-payment-settlement.integration.test.ts`, `check-splitting.integration.test.ts`, and `charge-allocations.integration.test.ts`. Every scenario runs against real command output, with no seeded fixtures (§1).

1. A Cashier pays a committed Check in full with cash: the Payment is recorded with correct change due, the Check becomes `SETTLED` with all four evidence columns populated, and the projection shows `balance_vnd: 0`.
2. The same with a Manual QR Payment, including a Payment with and without a `transaction_reference`.
3. Mixed settlement: a partial cash Payment followed by a Manual QR Payment for the remainder settles the Check; the Check stays `OPEN` between them with a positive balance.
4. Over-balance Payments are rejected with `PAYMENT_EXCEEDS_CHECK_BALANCE` and record nothing; a Payment against an already `SETTLED` Check reports `CHECK_NOT_OPEN` instead.
5. Under-tendered cash is rejected with `INSUFFICIENT_CASH_TENDERED`; a Manual QR Payment without the attestation is rejected with `MANUAL_QR_RECEIPT_CONFIRMATION_REQUIRED`.
6. Payment against a nonexistent Check, a closed Session, and a closed Shift each report their distinct code.
7. Split to a `NEW_CHECK` moves the requested quantity, leaves the source charge reduced, creates the destination at the moved amount, and the two charges sum to the original.
8. Split to an `EXISTING_CHECK` — reachable by splitting twice — combines with the destination's existing allocation for the same Committed Item into one row rather than two.
9. Splitting the entire source is rejected with `SPLIT_SOURCE_WOULD_BE_EMPTY`; an empty, duplicated, or non-positive item list and a self-targeted destination are each rejected with `INVALID_CHECK_SPLIT`; a quantity above the allocation with `SPLIT_QUANTITY_EXCEEDS_ALLOCATION`; an item not on the source with `SPLIT_ALLOCATION_NOT_FOUND`.
10. Merge combines two Checks: the survivor's charge is the sum, overlapping allocations combine, the absorbed Check is `MERGED` with `charge_vnd = 0` and `merged_into_check_id` set, and the projection reports it correctly.
11. Split and Merge are both rejected with `CHECK_HAS_PAYMENT` once any Payment exists on either Check, including a partial one, and reject a `SETTLED` or `MERGED` Check with `CHECK_NOT_OPEN`.
12. Split and Merge across two different Service Sessions are rejected with `CHECKS_DIFFERENT_SERVICE_SESSION`; merging a Check into itself with `INVALID_CHECK_MERGE`.
13. The stored charge invariant survives restructuring: after every Split and Merge, each Check's `charge_vnd` equals the live sum over its allocations.
14. The settlement invariant: corrupting `checks.state` to `SETTLED` on a Check with a positive balance makes the read fail rather than serve an inconsistent Check.
15. Expected Cash: a cash Payment raises the open Shift's `expected_cash_vnd` by the applied amount, a Manual QR Payment does not change it, and a Payment attributed to a previous Shift does not leak into the current one.
16. Authority is reloaded: an actor stripped of `sales.operate` mid-session is denied on all four routes and nothing changes; a `BARISTA` is denied on all four.
17. Idempotency: exact replay of each command returns the stored result without a second Payment or a second restructuring; the same `request_id` with a different payload returns `REQUEST_CONFLICT`; concurrent duplicate Payments execute once. A replay after the actor's identity is disabled or session revoked is denied.
18. Audit: a settling Payment writes exactly two events — the Payment and `CHECK_SETTLED`; a non-settling Payment writes one. An audit insertion failure rolls back the Payment, the settlement, and the idempotency claim.
19. Payments are never updated: over the whole suite, no `payments` row differs from its inserted values.

Additional coverage:

- **Concurrency.** Synchronized goroutines paying the same Check: exactly one succeeds outright and the other either fails cleanly or applies a still-valid remainder, with the final balance never negative. Two goroutines paying two Checks of the same Service Session both succeed, demonstrating that `FOR SHARE` on the parent rows does not serialize them. A Split racing a Payment on a shared Check resolves to exactly one of the two documented outcomes.
- **Migration.** The five new `checks` columns, all three branches of `check_settlement_evidence_valid` rejected at their boundaries, both branches of `payment_method_facts_valid`, the change-due identity, and the two new indexes.
- **Phase compatibility.** The existing Sales (5A and 5B), Tables, Shift, Catalog, and Auth suites pass unchanged, except for the Shift suite's `expected_cash_vnd` assertions, which gain the Cash Payment term.

Integration packages run with `-p 1`.

### 11.3 HTTP Tests

- Authentication and capability mapping for all four routes, including the `BARISTA` denial.
- UUID path parameter and request body validation: non-positive amounts, a malformed `destination` discriminator, an unknown `destination.type`, an empty `items` array, and an over-length `transaction_reference`.
- Stable response envelopes, statuses, and error codes for every new code, including `404` for `CHECK_NOT_FOUND`.
- A Cash Payment and a Manual QR Payment each serialize with only their own method's fields.
- `merged_into_check_id` appears only on a `MERGED` Check.
- Swagger annotations covering all four operations with Bearer security.

---

## 12. Decision Record Updates

`spec/decisions.md`: five records are added.

- **ADR-016 — One Check lock protocol for every 5C command.** Context: the canonical source locks `checks`, `service_sessions`, and `sales_shifts` all `FOR UPDATE` in the Payment path, but only `FOR UPDATE OF checks` in the restructuring path, with a source comment noting that locking parent rows there "can create a reverse dependency when Payment already owns one of the affected Check rows" — a deadlock hazard documented rather than removed. Decision: all four 5C commands lock `checks` `FOR UPDATE` in ascending id order, and `service_sessions` and `sales_shifts` `FOR SHARE`. `FOR SHARE` matches what the commands do, which is read parent state to evaluate a precondition. Consequence: no deadlock cycle exists among the 5C commands or against 5A and 5B; two cashiers paying different Checks of one Session proceed in parallel; Session and Shift closure, which take `FOR UPDATE`, remain excluded for the duration of each transaction.

- **ADR-017 — Settlement is a consequence of Payment, evaluated as a zero balance.** Context: the canonical `evaluateCheckSettlement` takes three inputs — `balanceVnd`, `hasPendingRefund`, `hasCustomerExcess` — of which the latter two are supplied as compile-time `false` constants from a `NO_RECORDED_CHECK_CORRECTIONS` object, because Refund does not exist yet. Decision: 5C has no settlement command and no settlement route; a Payment that brings the balance to zero settles the Check in the same transaction, writing all four evidence columns and a separate `CHECK_SETTLED` audit event. The condition is written as `balance == 0`. Consequence: a fully paid but unsettled Check cannot exist, guarded by the database constraint and the read invariant from both sides; Refund, when it arrives, brings its own readiness definition rather than inheriting a pre-built extension point nobody has designed against.

- **ADR-018 — Sales error codes are keyed by condition, not by operation.** Context: the canonical source declares twenty-seven codes across Payments, Split, and Merge, of which six pairs differ only by an operation prefix for an identical condition (`SALES_SHIFT_NOT_OPEN_FOR_SPLIT` against `MERGE_SALES_SHIFT_NOT_OPEN`, both describing what 5A already calls `OPEN_SALES_SHIFT_REQUIRED`), and three more describe field-shape violations that 5A's conventions treat as request validation. Decision: 5C declares thirteen new codes, one per condition, reuses 5A's and 5B's codes where the condition is the same, and demotes field-shape codes to request validation. `CHECK_CREATION_FAILED` is not migrated, per 5B §6.4. The design spec carries the complete canonical-to-5C mapping table. Consequence: a client handles one code per situation instead of one per situation per operation; the trace back to the canonical source stays mechanical through the mapping table; distinguishing `CHECK_NOT_FOUND` from `CHECK_NOT_OPEN` — which the canonical source collapses into one empty `WHERE` result — additionally makes the failure actionable for a cashier.

- **ADR-019 — A Payment stores its own Sales Shift and no attestation column.** Context: the canonical `payments` table stores `salesShiftId` even though it is reachable through `check → service_session → sales_shift`, and the Manual QR command takes a `receiptObservedInBankApp` boolean that must be `true` for the Payment to exist. Decision: `sales_shift_id` is stored on the Payment, because the Shift in which the money reached the cashier is an independent fact — a Session opened in one Shift can be paid in the next, and the derived path would answer the reconciliation question wrongly. No `receipt_observed_in_bank_app` column is created; the attestation is required in the request body and recorded in the audit event's details. Consequence: Expected Cash is a single-table scan over a partial index; a Payment's Shift attribution survives any later change to its Session; and the database stores no column whose value is `true` on every row.

- **ADR-020 — Expected Cash gains its Cash Payment term in 5C; the Cash Refund term is deferred to Refund.** Context: ADR-008 shipped `expected_cash_vnd` as Opening Float plus Pay Ins less Pay Outs, and recorded that "Phase 5 adds the Cash Payment and Cash Refund terms". 5C is the first sub-phase that creates a Cash Payment, and no sub-phase of Phase 5 creates a Refund. Decision: `internal/shift` adds one sqlc query summing applied amounts of `CASH` Payments for a Shift, and `ComputeExpectedCash` becomes Opening Float plus Cash Payments and Pay Ins, less Pay Outs. The sum is over applied amounts rather than tendered amounts, per `CONTEXT.md`. The Cash Refund term is deferred to whichever phase introduces Refund, and the Swagger description names Refund as the outstanding dependency rather than "Phase 5". `internal/shift` reads the `payments` table through its own query and does not import `internal/sales`, following ADR-012. Consequence: Expected Cash becomes a usable reconciliation figure for every cafe that does not issue cash refunds, which is the current operating reality; the remaining gap is named precisely instead of being attributed to a phase that will close without filling it.

`MIGRATE_PLAN.md` gains the 5C spec and plan links in its Phase 5 sub-phase table, with 5C marked complete when the implementation lands. Its Phase 5 detail is not rewritten, and its tracker row stays pending until 5D.

---

## 13. Acceptance Criteria

1. `internal/sales` exposes four new commands — pay cash, pay manual QR, split check, merge checks — and no Refund, Payment Void, Comp, Submit, or closure operation.
2. A Check can never be overpaid: an `applied_amount_vnd` above the live balance is rejected, and no code path produces a negative balance.
3. A Payment that brings the balance to zero settles the Check in the same transaction, writing all four evidence columns; no settlement command or route exists; a fully paid `OPEN` Check is rejected by the database constraint and by the read invariant.
4. A Cash Payment's `change_due_vnd` equals `cash_tendered_vnd − applied_amount_vnd`, enforced in Go and in a database check constraint.
5. Split and Merge are rejected on any Check carrying a Payment, and on any Check that is not `OPEN`; a Split can neither empty its source nor create an empty destination.
6. After every Split and Merge, each affected Check's `charge_vnd` equals the live sum over its allocations, and a `MERGED` Check has `charge_vnd = 0` with `merged_into_check_id` set.
7. All four commands take the same locks in the same order: `checks` `FOR UPDATE` ascending by id, parents `FOR SHARE`; two Payments on different Checks of one Session do not serialize.
8. The `payments` table stores `sales_shift_id`, stores no attestation column, and no row is ever updated after insert.
9. `expected_cash_vnd` is Opening Float plus Cash Payments and Pay Ins, less Pay Outs, summing applied amounts; `internal/shift` imports no Sales package; Swagger names Refund as the outstanding term.
10. The projection ships populated `payments`, real `total_applied_vnd` and `balance_vnd`, `merged_into_check_id` only when `MERGED`, no `pending_refund_vnd`, and `submitted` still constant `false` documented as completed by 5D.
11. Thirteen new error codes, one per condition, with the canonical-to-5C mapping documented; `CHECK_NOT_FOUND` maps to `404` and the rest of the new family to `409`.
12. Every mutation is actor-scoped and idempotent against the shared `idempotency_keys` table; no new idempotency table is created.
13. Every successful state change writes its Audit Events in the same transaction — two for a settling Payment, one otherwise; replays write none.
14. `internal/sales` imports no slice but `internal/auth`.
15. Unit, PostgreSQL integration, HTTP, and concurrency tests pass, with no seeded fixtures. Existing Auth, Catalog, Tables, Shift, 5A, and 5B suites remain passing, the Shift suite updated only for the completed Expected Cash term.
16. Swagger documentation reflects all eighteen Sales operations with Bearer security.
17. `spec/decisions.md` records ADR-016 through ADR-020. `MIGRATE_PLAN.md` gains the 5C links without a rewrite of its Phase 5 detail.
