# Design Specification: Recover a Failed or Abandoned Checkout (`internal/sales`, Phase 08)

- **Date:** 2026-10-02
- **Status:** In review
- **Phase:** Phase 08, [recover a failed or abandoned checkout](../../backlog/phase-08-recover-failed-or-abandoned-checkout.md)
- **Predecessors:** [5D submission and closure](2026-09-15-sales-submission-closure-design.md), [6C financial corrections](2026-09-18-preparation-financial-corrections-design.md), [07 Shift closure](2026-09-18-shift-closure-reconciliation-design.md)

---

## 1. Purpose

A checkout today has no exit short of completion. If a customer leaves after
Commit, or Submit never lands after Payment, the Service Session stays `ACTIVE`
forever, its Check stays open or holds unreturned money, and the Sales Shift
cannot close because an active Session is a Shift closure blocker.

Phase 08 names the paid-but-unsubmitted state, gives staff a way to withdraw it
and return the money through the existing Refund, and lets an unsubmitted
checkout end as an auditable **Abandoned Checkout** without an Order, a
Preparation Unit, or a Completed Sale.

### Goals

1. Expose **Awaiting Submission** on the Service Session projection and the
   active Session list.
2. Keep Submit as the retry, with its existing guarantee of one Order per draft.
3. Add **Cancel Awaiting Submission**, which withdraws the unsubmitted charges so
   the existing Refund can return the money.
4. Add **Abandon Checkout**, which terminates a Session that has no Order and no
   unreturned money.
5. Name Awaiting Submission in the Service Session and Shift closure blocker
   precedence.

### Non-Goals

- Withdrawing one unsubmitted round from a Session that already has an Order.
  Such a Session closes through the normal Completed Sale lifecycle; a stuck
  extra round there is recorded for backlog BA-5.
- Persisting a history of failed Submit attempts (ADR-064).
- Automatic Submit retry. `CONTEXT.md` forbids it.
- Listing or searching abandoned Sessions. Phase 09 owns history.
- The Cashier interface. That criterion is `(Phase 11 — UI)` and belongs to a
  later web slice.

---

## 2. Authority and Terminology

`CONTEXT.md` defines **Awaiting Submission** and **Abandoned Checkout**.
[`domain-rationale/06`](../../domain-rationale/06-define-payments-shifts-and-reconciliation.md)
fixes the rules: staff retry Submit or Cancel and Refund; an unsubmitted checkout
with no Payment may be abandoned; one with any Payment must first be fully
refunded; submitted work follows Cancellation or Waste instead.
[`domain-rationale/08`](../../domain-rationale/08-define-staff-permissions-and-audit.md)
lets the Cashier record an Abandoned Checkout, requires a reason category, and
fixes its catalog: `CUSTOMER_LEFT`, `CUSTOMER_REQUEST`, `SYSTEM_FAILURE`, `OTHER`.

**Withdrawal** is new vocabulary for this spec only: a Charge Adjustment that
removes an unsubmitted Committed Item's charge from its Check. It is not a
Cancellation, which `CONTEXT.md` reserves for submitted work.

---

## 3. The Failure Boundary

Commit, Payment, and Submit are separate commands, each one PostgreSQL
transaction. Submit cannot fail halfway: it inserts the Order, Order Items, and
Preparation Units together or not at all. The only durable partial state is
therefore **between** transactions: Payment has committed and Submit has not.
That window is reached by a client crash, a lost network response, or a Submit
that returned an error and was not retried.

The state already exists in the database; Phase 08 only names and exposes it.
Tests reach it deterministically by recording a Payment and not calling Submit.
No fault injection is needed.

---

## 4. Awaiting Submission (derived)

An `ACTIVE` Service Session is **Awaiting Submission** when it has an Order Draft
in state `COMMITTED` with no Order, and at least one Check allocating that
draft's Committed Items has net money received:

```text
valid (non-voided) Payments - completed Refunds > 0
```

It is computed in the projection, not stored (ADR-064). It ends when Submit
creates the Order, or when Cancel Awaiting Submission marks the draft
`CANCELLED`.

**Retry is Submit, unchanged.** Submit already locks the draft, inserts the Order
under `ON CONFLICT DO NOTHING` on the draft, and replays by request identifier.
A retry with the original request identifier returns the stored result; a retry
with a new identifier after success answers `NOTHING_TO_SUBMIT`. Neither creates
a second Order or Preparation Unit. Phase 08 adds tests for this, not code.

---

## 5. Commands

Both commands run through `ExecuteMutation` and require `sales.operate` and an
open Sales Shift. Neither needs Manager Approval; the Refund between them keeps
its own. Both take:

```json
{
  "request_id": "uuid",
  "reason": "CUSTOMER_LEFT | CUSTOMER_REQUEST | SYSTEM_FAILURE | OTHER",
  "note": "string, required and non-blank when reason is OTHER"
}
```

The note is normalized and bounded exactly as Refund's note is. Validation runs
before the mutation, so a malformed request never claims its idempotency key.
The fingerprint is `{service_session_id, reason, note}`.

### 5.1 `POST /sales/service-sessions/:id/cancel-awaiting-submission`

Preconditions, in order:

1. The Session exists and is `ACTIVE`, else `SERVICE_SESSION_NOT_FOUND` or
   `SERVICE_SESSION_ALREADY_CLOSED`.
2. It has no Order, else `SESSION_HAS_ORDER`.
3. It is Awaiting Submission, else `NOTHING_AWAITING_SUBMISSION`. An unpaid
   checkout is abandoned directly; it needs no withdrawal.

Effects:

- For every Charge Allocation of the committed draft's Committed Items, insert a
  `WITHDRAWAL` Charge Adjustment: scope `LIVE_CHECK`, the allocation's Check, the
  open Shift, the allocation's full charge as `amount_vnd`, and no Preparation
  Unit.
- Set the draft to `CANCELLED`.
- Answer `200` with the Service Session projection.

The withdrawn charge turns received money into money owed back. The existing
Refund consumes those adjustments like any other `LIVE_CHECK` correction, since
Refund reads neither the adjustment kind nor its Preparation Unit.

### 5.2 `POST /sales/service-sessions/:id/abandon`

Preconditions, in order:

1. The Session exists and is `ACTIVE`, else `SERVICE_SESSION_NOT_FOUND` or
   `SERVICE_SESSION_ALREADY_CLOSED`.
2. It has no Order, else `SESSION_HAS_ORDER`.
3. On every Check, valid Payments equal completed Refunds, and no Refund is
   pending, else `PAYMENT_REQUIRES_REFUND`.

A Session holding only an `EDITABLE` draft, or nothing at all, may be abandoned;
this is the exit for a takeaway opened by mistake.

Effects, in one transaction:

- Insert one `abandoned_checkouts` row.
- Set every `EDITABLE` or `COMMITTED` draft to `CANCELLED`.
- Set every `OPEN` or `SETTLED` Check to `ABANDONED`; `MERGED` Checks stay.
- Release every held Table Assignment, one `TABLE_ASSIGNMENT_RELEASED` Audit
  Event each, the same path Service Session closure uses.
- Set the Session to `ABANDONED`.
- Answer `200` with the Service Session projection.

No Order, Preparation Unit, or Completed Sale is created. Committed Items,
Charge Allocations, adjustments, Payments, and Refunds remain as they were.

### 5.3 The two flows

```text
unpaid:  abandon
paid:    cancel-awaiting-submission -> POST /sales/refunds (cash, or QR then confirm) -> abandon
```

A partially paid pay-first Check follows the paid flow: the withdrawal covers
the whole charge, and the Refund returns what was received.

---

## 6. Database

One migration, `000017_add_checkout_recovery.sql`.

**`charge_adjustments`**

- `preparation_unit_id` becomes nullable.
- `charge_adjustment_kind_source_valid` becomes:

  ```text
  (kind = 'CANCELLATION' AND preparation_unit_id IS NOT NULL AND preparation_waste_id IS NULL)
  OR (kind = 'COMP' AND preparation_unit_id IS NOT NULL AND preparation_waste_id IS NOT NULL)
  OR (kind = 'WITHDRAWAL' AND preparation_unit_id IS NULL AND preparation_waste_id IS NULL
      AND scope = 'LIVE_CHECK')
  ```

- A partial unique index on `(charge_allocation_id) WHERE kind = 'WITHDRAWAL'`
  guarantees that no allocation is withdrawn twice.
  `charge_adjustment_kind_unit_unique` is unchanged; NULLs do not collide in it.

**State domains**

| Table | Constraint | Values after |
| --- | --- | --- |
| `service_sessions` | `service_session_state_valid` | `ACTIVE`, `CLOSED`, `ABANDONED` |
| `checks` | `check_state_valid` | `OPEN`, `SETTLED`, `MERGED`, `ABANDONED` |
| `order_drafts` | `order_draft_state_valid` | `EDITABLE`, `COMMITTED`, `CANCELLED` |

**`abandoned_checkouts`**

```text
id                           UUID PK default gen_random_uuid()
service_session_id           UUID NOT NULL UNIQUE -> service_sessions
sales_shift_id               UUID NOT NULL -> sales_shifts
reason                       TEXT NOT NULL CHECK in the four-value catalog
note                         TEXT, CHECK (reason <> 'OTHER' OR note IS NOT NULL)
actor_staff_identity_id      UUID NOT NULL -> staff_identities
staff_access_session_id      UUID NOT NULL -> staff_access_sessions
occurred_at                  TIMESTAMPTZ NOT NULL
```

All foreign keys are `ON DELETE RESTRICT`. The withdrawal's reason and note live
in its Audit Event; the Abandoned Checkout row is the terminal record the
projection reads.

**Ripple.** `preparation_unit_id` turning nullable changes sqlc's generated type
from `uuid.UUID` to `uuid.NullUUID`. Every query in `sql/queries/preparation.sql`,
`sales.sql`, and `shift.sql` that selects the column follows. The change is
mechanical but crosses into `internal/preparation` and `internal/shift`.

---

## 7. Reads

`GET /sales/service-sessions/:id` gains:

| Field | Type | Meaning |
| --- | --- | --- |
| `awaiting_submission` | bool | Section 4 |
| `awaiting_submission_committed_item_ids` | uuid[] | The committed draft's items; empty when false |
| `abandoned_checkout` | object or null | `reason`, `note`, `actor_staff_identity_id`, `occurred_at` |

Each Charge Allocation gains `withdrawn: bool`. State fields carry the new
values from section 6.

The active Service Session list gains `awaiting_submission` on each row, so the
POS can raise those Sessions to the top.

---

## 8. Closure Blockers

### 8.1 Service Session closure

`EvaluateClosureReadiness` stays pure and gains `AwaitingSubmission` from the
projection. The new first entry of `ClosureReadiness.Err()`:

```text
1. awaiting submission        AWAITING_SUBMISSION_FOR_CLOSURE   (new)
2. unsettled Checks           CHECK_NOT_SETTLED_FOR_CLOSURE
3. pending Refund             PENDING_REFUND_FOR_CLOSURE
4. unsubmitted work           UNSUBMITTED_WORK_FOR_CLOSURE
5. no Order                   ORDER_REQUIRED_FOR_CLOSURE
6. unfinished preparation     UNFULFILLED_PREPARATION_FOR_CLOSURE
```

It comes first because money has been taken for drinks the bar has never
received; that is the most urgent problem on the screen.

### 8.2 Sales Shift closure

`GetGlobalShiftClosureBlockers` gains `awaiting_submission_count`, evaluated in
the same read. Phase 07's precedence becomes:

```text
1. unsettled Checks
2. pending Refund intents
3. unresolved financial correction obligations
4. Sessions Awaiting Submission   SHIFT_AWAITING_SUBMISSION   (new)
5. active Service Sessions
```

Every Session Awaiting Submission is also active, so position 4 changes only
which error staff see, never whether closure is blocked. A withdrawal not yet
refunded is already caught by blocker 3, whose formula counts every
`LIVE_CHECK` adjustment. An `ABANDONED` Check is not `OPEN` and does not block.

---

## 9. Locking and Concurrency

Both commands lock in Submit's order: Service Session, then drafts, then Checks
in ascending `(created_at, id)`.

| Race | Outcome |
| --- | --- |
| Cancel and Submit | Serialized on the Session lock. The loser sees the draft cancelled (`NOTHING_TO_SUBMIT`) or the Order present (`SESSION_HAS_ORDER`). Both never succeed. |
| Abandon and Commit, Start Draft, or Submit | Serialized on the Session lock. The loser sees a Session no longer `ACTIVE` or an Order present. Commit and draft edits lock draft and Session in one statement, whose tuple-lock order PostgreSQL does not fix, so this pair can also abort one side with 40P01; the contract is ADR-031's. |
| Cancel or Abandon and Payment or Refund | Payment and Refund lock Check then Session, so this is ADR-031's AB-BA window. At worst one side aborts with 40P01, a `500`; the rollback includes its idempotency claim, so a retry runs as a first attempt. Otherwise the serialized loser sees current money and answers from it. |

Abandon re-reads money under its Check locks, so a Payment that commits first
is seen and the abandon is refused with `PAYMENT_REQUIRES_REFUND`.

---

## 10. Errors

| Code | HTTP | When |
| --- | --- | --- |
| `NOTHING_AWAITING_SUBMISSION` | 409 | Cancel on a Session that is not Awaiting Submission |
| `SESSION_HAS_ORDER` | 409 | Cancel or Abandon on a Session with an Order |
| `PAYMENT_REQUIRES_REFUND` | 409 | Abandon while money is held or a Refund is pending |
| `AWAITING_SUBMISSION_FOR_CLOSURE` | 409 | Service Session closure, section 8.1 |
| `SHIFT_AWAITING_SUBMISSION` | 409 | Shift reconciliation or closure, section 8.2 |
| `INVALID_INPUT` | 400 | Unknown reason, or `OTHER` without a note |

Existing codes are reused for a missing Session, a closed or abandoned Session,
and a missing open Shift.

---

## 11. Audit

Each event is written in its command's transaction (ADR-048).

| Event | Details |
| --- | --- |
| `AWAITING_SUBMISSION_CANCELLED` | `service_session_id`, `order_draft_id`, `charge_adjustment_ids`, `withdrawn_vnd`, `reason`, `note` |
| `CHECKOUT_ABANDONED` | `service_session_id`, `abandoned_checkout_id`, `check_ids`, `released_table_ids`, `reason`, `note` |
| `TABLE_ASSIGNMENT_RELEASED` | Unchanged shape, one per released Table |

---

## 12. Testing

Integration tests on the PostgreSQL template:

1. **Induced failure.** Commit, record a cash Payment, do not Submit. Assert
   `awaiting_submission`, `AWAITING_SUBMISSION_FOR_CLOSURE` from Session
   closure, and `SHIFT_AWAITING_SUBMISSION` from Shift reconciliation.
2. **Retry.** Submit succeeds; a replay with the same request identifier returns
   the stored result; a new identifier answers `NOTHING_TO_SUBMIT`; exactly one
   Order and the expected Preparation Units exist.
3. **Cancel and refund, cash.** Cancel, Refund in cash, Abandon. No Order,
   Preparation Unit, or Completed Sale exists; Expected Cash nets to zero; the
   Shift closes.
4. **Cancel and refund, Manual QR.** Abandon is refused while the Refund is
   pending and succeeds after confirmation.
5. **Unpaid abandonment.** Abandon a committed dine-in checkout; the Table is
   released, Checks are `ABANDONED`, the Shift closes.
6. **Empty abandonment.** Abandon a Session with only an `EDITABLE` draft.
7. **Rejections.** Abandon with money held; Cancel or Abandon with an Order;
   Cancel an unpaid checkout; `OTHER` without a note.
8. **Replay.** Cancel and Abandon replay by request identifier; a new
   identifier on an abandoned Session answers `SERVICE_SESSION_ALREADY_CLOSED`.
9. **Concurrency.** Cancel against Submit: exactly one wins. Abandon against
   Commit: exactly one wins. Cancel against Payment: success, or a 40P01 abort
   with a clean retry.

Unit tests: the blocker order in `ClosureReadiness.Err()`, request validation,
and the error mapping.

---

## 13. Decisions

Recorded in `spec/decisions.md` when the phase merges.

- **ADR-064: Awaiting Submission is derived, not stored.** The backlog asked for
  the persisted "failure meaning". A failed Submit rolls back completely, so
  recording it would need a second transaction and a table used only as a log.
  The durable facts, Payment present and Order absent, already say everything
  staff act on. Rejected: a `submit_failures` table.
- **ADR-065: Unsubmitted charges leave a Check through a `WITHDRAWAL` Charge
  Adjustment, and paid recovery is Cancel, Refund, Abandon.** Refund returns money
  only against a corrected charge, and every existing adjustment needs a
  Preparation Unit that unsubmitted work never has. Three commands keep one
  intent per Audit Event and let a Manual QR Refund stay pending between steps.
  Rejected: one composite "cancel and refund" command, which cannot finish a
  pending QR Refund and merges three audited intents.
- **ADR-066: Abandonment ends only a Session with no Order, and its commands
  inherit ADR-031's lock window.** A Session with a submitted Order closes as a
  Completed Sale, as `CONTEXT.md` states. Awaiting Submission is first in Session
  closure precedence and fourth, before active Sessions, in Shift closure
  precedence.

---

## 14. Delivery

One pull request on a branch from `master`. When it merges:

- `ROADMAP.md` moves Phase 08 to Delivered; Phase 09 depends only on it.
- The backlog ticket is marked `completed`.
- `backend-alignment.md` BA-5 gains the stuck-extra-round case from section 1.
