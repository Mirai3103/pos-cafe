# Design Specification: Sales Shift Closure And Reconciliation (`internal/shift`, Phase 07)

- **Author:** OpenCode & Team
- **Date:** 2026-09-18
- **Status:** Approved
- **Phase:** Phase 07, Close and reconcile a Sales Shift
- **Predecessors:** [`2026-09-13-shift-slice-design.md`](2026-09-13-shift-slice-design.md) (Phase 4), [`2026-09-18-preparation-financial-corrections-design.md`](2026-09-18-preparation-financial-corrections-design.md) (Phase 6C)

---

## 1. Purpose

The Shift slice can open one Sales Shift, record Cash Movements, and derive
Expected Cash and Manual QR reconciliation totals. It cannot close a Shift. An
opened Shift therefore remains `OPEN` forever and prevents the next operating
cycle from starting.

Phase 07 adds a durable blind-count workflow, freezes financial activity while
staff reconcile, preserves every recount and recheck, requires Manager Approval
for a nonzero discrepancy, and closes the Shift with an immutable snapshot.

### Goals

1. Prevent Shift APIs from returning aggregate Expected Cash or server-derived
   aggregate source totals before an initial cash count is durably recorded.
2. Move a Shift through `OPEN -> CLOSING -> CLOSED` without holding a database
   transaction across HTTP requests.
3. Reject reconciliation while any global closure blocker remains.
4. Freeze the authoritative Shift totals when reconciliation starts.
5. Preserve every cash count and Manual QR observation with actor, session,
   sequence, and server time.
6. Compare Cash, Manual QR received, and Manual QR refunded independently.
7. Require a recount or recheck before a nonzero discrepancy may close.
8. Require one fresh Manager Approval for the exact final evidence and complete
   set of nonzero discrepancy reasons.
9. Atomically write an immutable closure snapshot and change the Shift to
   `CLOSED`.
10. Allow a new Shift to open after closure.
11. Let Managers inspect closed Shifts by time range and id.

### Non-Goals

- Frontend screens or generated TypeScript clients. Phase 11 owns them.
- Awaiting Submission and Abandoned Checkout. Phase 08 adds that blocker.
- Post-Shift Payment Correction or additive correction history. Phase 09 owns
  them and must not rewrite the Phase 07 closure snapshot.
- Receipt, fiscal invoice, accounting export, or drawer hardware integration.
- Storing bank screenshots, sender identities, or bank credentials.
- Creating balancing Payments, Refunds, or Cash Movements.
- Reopening a closed Shift or abandoning a `CLOSING` Shift back to `OPEN`.
- A new reconciliation package or a shared cross-slice executor.

### Accepted Consequences

Shift responses become intentionally less informative while a Shift is `OPEN`.
Opening Float, Cash Movement history, Refund details, Expected Cash, and the
aggregate source totals from which it can be derived are unavailable through
the current read until the initial count commits. Cash Movement mutation
responses likewise stop returning recomputed Expected Cash. They may return the
single movement just entered, and Open Shift may echo the Opening Float supplied
by its actor. Blindness means the service does not disclose the aggregate target
or aggregate derivation inputs; it does not pretend staff forget values they
personally entered or sales they personally processed. This is a deliberate
breaking contract needed to make the control server-enforced rather than a UI
convention.

Entering `CLOSING` suspends normal current-Shift activity and has no cancel path.
An interrupted workflow is resumed by any staff member who currently holds
`sales_shift.operate`. This avoids revealing Expected Cash and then returning the
same Shift to normal sales activity.

The existing behavior that allowed a Service Session to outlive its opening
Shift remains valid historical attribution, but it is no longer a normal route
through Shift closure. Phase 07 requires all active Sessions to close first.

---

## 2. Authority And Terminology

`CONTEXT.md` and `spec/decisions.md` are binding. The Phase 07 backlog ticket is
the acceptance target. The payment and reconciliation rationale explains intent
but does not override those authorities.

- A **Reconciliation** is the durable workflow attached one-to-one to a Shift
  when its initial cash count moves it to `CLOSING`.
- A **Cash Count** is one immutable observed drawer amount. Sequence 1 is the
  blind initial count. A later count is a recount.
- A **Manual QR Observation** is one immutable pair containing observed received
  and refunded totals. Both values are explicit, including zero.
- A **Closure Snapshot** is the immutable core history created when the Shift
  changes to `CLOSED`.
- A **Shift Discrepancy** is one nonzero signed difference for `CASH`,
  `MANUAL_QR_RECEIVED`, or `MANUAL_QR_REFUNDED`.
- Difference always means `observed - expected`. A positive value is an excess;
  a negative value is a shortage.
- **Active Shift** means either `OPEN` or `CLOSING`. At most one may exist.

The discrepancy reason catalog is:

| Reason | Use |
| --- | --- |
| `CASH_COUNT_DIFFERENCE` | A known cash-count difference |
| `QR_OBSERVATION_DIFFERENCE` | A known Manual QR observation difference |
| `UNEXPLAINED` | The difference remains unexplained after recheck |
| `OTHER` | Another explanation; a note is required |

Notes are trimmed, immutable, and 1 through 500 characters when present.
`OTHER` requires a note; every other reason forbids one.

---

## 3. Architecture

### 3.1 Package Ownership

`internal/shift` owns:

- Reconciliation start, cash-count, QR-observation, and final-close commands.
- Shift state transitions and closure blockers.
- The frozen reconciliation and closure projections.
- Closed-Shift list and detail reads.
- Every Phase 07 table and query.

`internal/sales` changes only Session Start. It must hold the current Shift
`FOR SHARE` from open-Shift validation through Session insertion so Shift
closure cannot miss a Session created concurrently.

No slice imports another business slice. Both use their own sqlc queries over
the shared PostgreSQL schema.

### 3.2 Operations

| Operation | Capability | Manager Approval | Success |
| --- | --- | --- | ---: |
| Start reconciliation | `sales_shift.operate` | No | `201` |
| Record cash count | `sales_shift.operate` | No | `201` |
| Record QR observation | `sales_shift.operate` | No | `201` |
| Close exact Shift | `sales_shift.operate` | No | `200` |
| Close discrepant Shift | `sales_shift.operate` | Yes | `200` |
| Read current Shift | `sales_shift.operate` | No | `200` |
| List/read closed Shifts | `audit.inspect` | No | `200` |

Every mutation uses the existing Shift executor. It reloads current authority,
claims idempotency, writes business facts and Audit Events, stores the response,
and commits once.

Stable operations are:

- `shift.start_reconciliation`
- `shift.record_cash_count`
- `shift.record_qr_observation`
- `shift.close_exact`
- `shift.close_with_discrepancy`

The close route chooses the exact operation when its discrepancy-reason list is
empty and the discrepant operation when the list is non-empty. Omitting reasons
cannot bypass approval: final server-derived discrepancies are checked inside
the transaction and an exact command fails when any is nonzero.

---

## 4. Lifecycle

### 4.1 `OPEN`

Normal Shift actions continue to require `OPEN`. The current-Shift read returns
only id, state, opener identity, and opening time. It does not return any money,
Cash Movement, Refund, or reconciliation field. Cash Movement creation returns
the created movement and Shift metadata but no Expected Cash or source total.

### 4.2 `OPEN -> CLOSING`

Start Reconciliation performs, in order:

1. Reload current authority and claim the idempotent command.
2. Lock the named `OPEN` Shift `FOR UPDATE`.
3. Evaluate all global blockers in the precedence defined in section 8.
4. Load the authoritative reconciliation totals and Cash Movement sums.
5. Compute Expected Cash with the existing checked formula.
6. Insert the immutable reconciliation snapshot.
7. Insert cash-count sequence 1 from the request.
8. Change the Shift to `CLOSING`.
9. Write reconciliation-start and cash-count Audit Events.
10. Commit and only then return the revealed snapshot.

If any step fails, the Shift remains `OPEN` and the initial count is not stored.

### 4.3 `CLOSING`

All ordinary commands that require an open Shift reject `CLOSING`. The active
Shift unique index still prevents another Shift from opening. Staff may append
cash counts and QR observations. Any currently authorized Shift operator may
resume the workflow; the original starter is not a lock owner.

The current-Shift read returns the frozen snapshot and every attempt ordered by
sequence. It never recalculates expected values from unrestricted current data.
No pre-commit error response may contain Expected Cash, an aggregate source
total, or arithmetic operands. In particular, guarded-range errors are mapped
to a stable generic message until the initial count and snapshot commit.

### 4.4 `CLOSING -> CLOSED`

Final Close performs, in order:

1. Reload authority and, for the discrepant operation, verify fresh Manager
   Approval before replay.
2. Lock the named `CLOSING` Shift `FOR UPDATE`.
3. Re-evaluate all global blockers.
4. Reload source totals and require exact equality with the frozen snapshot.
5. Require the submitted final attempt ids to be the latest attempts.
6. Derive the three signed differences.
7. Enforce recount, recheck, reason, note, and approval rules.
8. Insert the closure snapshot and nonzero discrepancy rows.
9. Change the Shift to `CLOSED`.
10. Write the exact or discrepant closure Audit Event and commit.

There is no `CLOSED -> OPEN` transition.

---

## 5. Database Design

Migration `000015_add_shift_closure_reconciliation.sql` adds the Phase 07 state,
facts, constraints, and indexes below. Generated sqlc files are never edited by
hand. Every foreign key uses restrictive delete behavior; Shift, Staff Identity,
and Staff Access Session evidence cannot be deleted out from under a
reconciliation.

### 5.1 `sales_shifts`

The state constraint adds `CLOSING`. The existing partial unique index is
replaced by an index that permits at most one row where state is `OPEN` or
`CLOSING`.

Existing opener, Opening Float, and opening time remain immutable. Closure facts
are not added as nullable columns to this table.

### 5.2 `shift_reconciliations`

One immutable row is created per Shift:

| Column | Type | Notes |
| --- | --- | --- |
| `id` | `UUID` PK | stable public identity |
| `sales_shift_id` | `UUID NOT NULL UNIQUE` | FK to the target Shift |
| `started_by_staff_identity_id` | `UUID NOT NULL` | initiator identity FK |
| `started_staff_access_session_id` | `UUID NOT NULL` | initiator session FK |
| `started_at` | `TIMESTAMPTZ NOT NULL` | database clock |
| `opening_float_vnd` | `BIGINT NOT NULL` | frozen opening fact |
| `pay_in_vnd`, `pay_out_vnd` | `BIGINT NOT NULL` | frozen Cash Movement sums |
| `cash_payment_vnd` | `BIGINT NOT NULL` | original Cash applied amount |
| `cash_payment_void_vnd` | `BIGINT NOT NULL` | voided Cash amount |
| `cash_refund_vnd` | `BIGINT NOT NULL` | completed Cash Refund amount |
| `expected_cash_vnd` | `BIGINT NOT NULL` | checked signed result |
| `manual_qr_payment_vnd` | `BIGINT NOT NULL` | original QR applied amount |
| `manual_qr_payment_void_vnd` | `BIGINT NOT NULL` | voided QR amount |
| `expected_manual_qr_received_vnd` | `BIGINT NOT NULL` | payment less void |
| `manual_qr_refund_vnd` | `BIGINT NOT NULL` | completed QR Refund amount |
| `pending_manual_qr_refund_vnd` | `BIGINT NOT NULL` | closure evidence, constrained to zero |
| `pending_refund_vnd` | `BIGINT NOT NULL` | closure evidence, constrained to zero |
| `unresolved_post_sale_adjustment_vnd` | `BIGINT NOT NULL` | closure evidence, constrained to zero |

The source fields are stored separately even where a net field is also stored.
Historical readers can therefore explain the result without querying mutable
projections or silently changing the meaning of an old total.

### 5.3 `shift_cash_counts`

Each row contains `id UUID` primary key, `reconciliation_id UUID` foreign key,
positive `sequence INT`, non-negative `counted_cash_vnd BIGINT`, actor identity
and access-session foreign keys, and `counted_at TIMESTAMPTZ` from the database
clock. The unique key `(reconciliation_id, sequence)` also supports ordered
reads. Rows are never updated or deleted.

Sequence 1 is the blind initial count. If final Cash differs from Expected Cash,
the final sequence must be at least 2. Exact sequence 1 may close directly.

### 5.4 `shift_qr_observations`

Each row contains `id UUID` primary key, `reconciliation_id UUID` foreign key,
positive `sequence INT`, non-negative `observed_received_vnd BIGINT`,
non-negative `observed_refunded_vnd BIGINT`, actor identity and access-session
foreign keys, and `observed_at TIMESTAMPTZ` from the database clock. Both
observed values are mandatory. The unique key `(reconciliation_id, sequence)`
also supports ordered reads. Rows are never updated or deleted.

At least one QR observation is required for every closure. If either QR
dimension differs, the final sequence must be at least 2.

### 5.5 `shift_closures`

One immutable row is created per reconciliation and Shift:

| Column group | Type / constraint |
| --- | --- |
| `id` | `UUID` primary key |
| `sales_shift_id`, `reconciliation_id` | `UUID NOT NULL UNIQUE`, foreign keys |
| `initial_cash_count_id`, `final_cash_count_id`, `final_qr_observation_id` | required UUID foreign keys |
| `opener_staff_identity_id`, `closer_staff_identity_id` | required identity foreign keys |
| `closer_staff_access_session_id` | required access-session foreign key |
| `approved_by_staff_identity_id` | nullable Manager identity foreign key |
| `opened_at`, `closed_at` | required `TIMESTAMPTZ`; close is not before open |
| Source and expected snapshot | every scalar column from `shift_reconciliations` |
| `observed_cash_vnd`, `observed_manual_qr_received_vnd`, `observed_manual_qr_refunded_vnd` | required `BIGINT` |
| `cash_difference_vnd`, `manual_qr_received_difference_vnd`, `manual_qr_refunded_difference_vnd` | required signed `BIGINT` |

The closure repeats the scalar snapshot deliberately. A closed-Shift detail is
one immutable aggregate and never depends on recalculating the reconciliation
from future financial rows. Stable Staff Identity ids are the actor facts;
display name and login code remain identity projection labels rather than copied
business facts.

A database check requires a null approver when all three differences are zero
and a non-null approver when any difference is nonzero. Checks also enforce each
stored difference as its bounded observed-minus-expected equation. An index on
`(closed_at DESC, id DESC)` serves history pagination. Application validation
requires all referenced attempts to belong to the same reconciliation because a
cross-table same-parent constraint is not expressible as one ordinary foreign
key.

### 5.6 `shift_discrepancies`

There is at most one row per closure and dimension. Each row has `id UUID`
primary key, `shift_closure_id UUID` foreign key, dimension, expected and
observed `BIGINT` amounts, nonzero signed difference, reason, optional note, and
`created_at TIMESTAMPTZ`. The unique key is
`(shift_closure_id, dimension)`. Exact dimensions have no row.

Application validation requires the row set to equal the nonzero differences in
the closure. Database constraints enforce the dimension allowlist, nonzero
difference, uniqueness, difference equation, and reason/note shape.
`CASH_COUNT_DIFFERENCE` is valid only for `CASH`;
`QR_OBSERVATION_DIFFERENCE` is valid only for either QR dimension;
`UNEXPLAINED` and `OTHER` are valid for every dimension.

### 5.7 Monetary Bounds

Observed values use the existing non-negative money-input bound. Expected Cash
keeps its existing symmetric bound because a drawer responsibility can be
negative. Aggregate addition and every `observed - expected` subtraction are
checked before persistence. Range failure never truncates or wraps.

---

## 6. Reconciliation Equations

The snapshot consumes the Phase 6C Shift query; it does not introduce a second
financial model.

```text
Expected Cash
= Opening Float
+ Cash Payments
- Cash Payment Voids
- completed Cash Refunds
+ Pay Ins
- Pay Outs
```

```text
Expected Manual QR Received
= Manual QR Payments
- Manual QR Payment Voids
```

```text
Expected Manual QR Refunded
= completed Manual QR Refunds
```

The three closure differences are:

```text
Cash Difference = Observed Cash - Expected Cash
QR Received Difference = Observed QR Received - Expected QR Received
QR Refunded Difference = Observed QR Refunded - Expected QR Refunded
```

Pending Refunds do not change expected money movement. They block start and
close instead. No discrepancy produces an automatic balancing record.

---

## 7. Attempt And Discrepancy Rules

1. Initial Cash is recorded before any expected or source total is returned.
2. A cash recount appends a row; it never replaces the initial count.
3. A QR recheck appends the full received/refunded pair.
4. Final evidence must be the latest row in each attempt ledger.
5. Cash difference requires at least two Cash Counts.
6. Either QR difference requires at least two QR Observations.
7. Each nonzero dimension requires exactly one reason entry.
8. A zero dimension forbids a reason entry.
9. One fresh Manager Approval authorizes the complete final discrepancy set.
10. The approver may be the initiator, preserving ADR-009.
11. A later attempt makes a previously prepared close request stale; the client
    refreshes and submits the latest ids.

---

## 8. Closure Blockers

Blockers are global across the cafe, not scoped only by opening Shift. This
matches the single active cashier Shift and prevents hidden work from surviving
normal closure.

All blocker facts are collected in one read, but the first public error follows
this load-bearing precedence:

```text
1. unsettled Checks
2. pending Refund intents
3. unresolved financial correction obligations
4. active Service Sessions
```

An unsettled Check is a Check whose state is `OPEN`; `SETTLED` and `MERGED` do
not block. A pending Refund is a Refund lacking its unique completion row.

The global unresolved-correction amount is defined independently of Shift
attribution:

```text
sum over every Check carrying any LIVE_CHECK adjustment of
  max(valid non-voided Payments
      - completed live Refunds
      - (base Charge Allocations - all LIVE_CHECK adjustments), 0)

+ sum over every POST_SALE Charge Adjustment of
  max(adjustment amount - completed Refund allocations to that adjustment, 0)
```

It blocks when greater than zero. A pending Refund intent does not reduce either
term until its completion exists, so it may make both blocker 2 and blocker 3
true; precedence reports pending Refund first. The Shift-scoped reconciliation
query remains authoritative for snapshot totals, while a new global query owns
this blocker predicate. An active Session is any
`service_sessions.state = 'ACTIVE'` row.

The financial blockers precede the generic active-Session blocker so staff see
the most actionable money problem. Shift closure does not duplicate
unsubmitted-work or Preparation Unit rules; Service Session closure owns those
and the remaining active Session is the Shift-level blocker.

Phase 08 appends Awaiting Submission at the documented position without
weakening these Phase 07 checks.

---

## 9. HTTP Contract

### 9.1 Start Reconciliation

`POST /api/v1/shifts/:shift_id/reconciliation`

```json
{
  "request_id": "uuid",
  "counted_cash_vnd": 1250000
}
```

`counted_cash_vnd` is pointer-backed at the HTTP boundary so omitted and explicit
zero remain distinct. Success returns `201` with the `CLOSING` Shift through the
`ClosingShiftResponse` defined in section 9.6.

### 9.2 Append Cash Count

`POST /api/v1/shifts/:shift_id/reconciliation/cash-counts`

```json
{
  "request_id": "uuid",
  "counted_cash_vnd": 1249000
}
```

Success returns `201` with the appended attempt and `ReconciliationPreview`
defined in section 9.6.

### 9.3 Append Manual QR Observation

`POST /api/v1/shifts/:shift_id/reconciliation/qr-observations`

```json
{
  "request_id": "uuid",
  "observed_received_vnd": 730000,
  "observed_refunded_vnd": 50000
}
```

Both money fields are pointer-backed and mandatory. Success returns `201` with
the appended observation and `ReconciliationPreview`.

### 9.4 Final Close

`POST /api/v1/shifts/:shift_id/close`

```json
{
  "request_id": "uuid",
  "final_cash_count_id": "uuid",
  "final_qr_observation_id": "uuid",
  "discrepancies": [
    {
      "dimension": "CASH",
      "reason": "CASH_COUNT_DIFFERENCE"
    }
  ],
  "approver_login_code": "MGR01",
  "manager_pin": "request-only secret"
}
```

`discrepancies` is a non-null empty array for exact closure. Approval fields are
absent for exact closure and required together for discrepant closure. Expected,
observed, and difference amounts never come from this request.

The fingerprint contains Shift id, final attempt ids, normalized dimension /
reason / note tuples, and no approver login or PIN.

### 9.5 Reads

- `GET /api/v1/shifts/current` returns the `OPEN` redacted shape, the `CLOSING`
  reconciliation shape, or `data: null` when no Shift is active.
- Existing Cash Movement creation no longer returns `expected_cash_vnd` while
  the Shift is `OPEN`; its success response contains the movement and redacted
  Shift metadata.
- `GET /api/v1/shifts?closed_from=<instant>&closed_to=<instant>&cursor=<token>&limit=<n>`
  requires `audit.inspect`, returns closed summaries ordered by
  `(closed_at DESC, id DESC)`, and uses bounded cursor pagination.
- `GET /api/v1/shifts/:shift_id` requires `audit.inspect` and returns one closed
  immutable detail. An open or closing id is not exposed through this history
  route.

Collections serialize as `[]`, never `null`. Server instants remain RFC 3339;
client presentation uses `Asia/Ho_Chi_Minh`.

### 9.6 Response DTOs

The field allowlists are part of the blind-count boundary:

- `OpenCurrentShiftResponse`: `id`, `state`, `opened_at`, `opener` only.
- `ClosingShiftResponse`: Shift metadata plus `reconciliation`.
- `ReconciliationResponse`: id, starter, start time, every frozen source and
  expected scalar, `cash_counts`, `qr_observations`, and `preview`.
- `CashCountResponse`: id, sequence, counted Cash, actor, and count time.
- `QRObservationResponse`: id, sequence, observed received/refunded, actor, and
  observation time.
- `ReconciliationPreview`: three dimension entries. Each contains expected,
  nullable latest observed, nullable signed difference, and
  `recheck_required`; it also exposes `can_close`.
- `ClosedShiftSummaryResponse`: Shift id, open/close times, opener/closer,
  Opening Float, expected/observed/difference for all three dimensions, and
  `has_discrepancy`.
- `ClosedShiftDetailResponse`: the summary, complete frozen source scalars,
  starter, all Cash Counts, all QR Observations, non-null discrepancy list, and
  optional approver.

Start and both attempt routes return the closing response or its newly appended
attempt plus the full preview. Final Close returns
`ClosedShiftDetailResponse`, including when the initiator is a Cashier. History
list returns summaries; history detail returns the same closed detail. No DTO
contains a PIN, approval login input, transaction reference, screenshot, or
sender identity.

History requires both `closed_from` and `closed_to` as RFC 3339 instants. The
lower bound is inclusive, the upper bound exclusive, and the range may span at
most 31 days. `limit` defaults to 50 and is capped at 100. The opaque base64url
cursor contains a version, last `closed_at`, last id, and the normalized range;
it is exclusive and must match the request range. A malformed or mismatched
cursor returns `400`, preventing duplicate or omitted pages caused by filter
changes.

---

## 10. Authorization, Approval, And Idempotency

The HTTP middleware is only a transport guard. Every read and mutation reloads
the current identity, access session, roles, and capabilities through the Shift
runner according to ADR-048.

Normal reconciliation requires `sales_shift.operate`. Closed history requires
`audit.inspect`, which is assigned to Manager and not Cashier. No fresh PIN is
needed for a read.

Discrepant Close uses `auth.VerifyManagerApproval` with the Manager role and
`sales_shift.operate`. Approval runs before idempotent replay, so a disabled,
demoted, or wrong-PIN Manager cannot replay a prior success. Self-approval is
allowed.

PIN and approver login are request-only verification inputs. They never enter
fingerprints, business records, stored responses, Audit Event details, or logs.

Exact replay returns the original status and response without another attempt,
snapshot, discrepancy, or Audit Event. Reusing a request id with another
operation or fingerprint returns `REQUEST_CONFLICT`.

---

## 11. Locking And Concurrency

### 11.1 Lock Protocol

Start, attempt, and close commands lock the target Shift `FOR UPDATE`. They never
lock Checks or Sessions after taking that lock. Blocker reads are MVCC reads.

Payment, Refund, Payment Void, Comp, Cancellation, and Cash Movement commands
coordinate through the current Shift row. Therefore an in-flight financial
writer either commits before reconciliation obtains its lock and appears in the
snapshot, or waits and then rejects when the Shift is no longer `OPEN`.

Draft and Commit commands do not take the Shift row lock directly. They require
an `ACTIVE` Service Session and serialize with Session closure through existing
Draft/Session locks. Reconciliation cannot start while that Session is active,
and Session Start cannot create a replacement after the Shift gate closes.
Consequently Commit either finishes before Session closure and remains covered
by the active-Session blocker, or rejects after Session closure. Closure never
takes Check or Session row locks while holding the Shift, so it does not create
an inverse lock cycle.

Session Start must acquire the open Shift `FOR SHARE` before inserting its
Session and hold it through commit. This closes the only known race in which a
new active Session could otherwise appear after blocker validation.

### 11.2 Accepted Race Outcomes

- Two independent starts: one commits `CLOSING`; the other returns already
  closing.
- Attempt versus close: Shift locking serializes them. If the attempt wins, the
  close's supplied ids are stale. If close wins, the attempt sees `CLOSED`.
- Two independent closes: one snapshot wins; the other returns already closed.
- Exact idempotent replay: the stored successful response is returned after
  current authority and required approval are rechecked.
- Financial writer versus start: the writer is wholly included or wholly
  rejected after the state transition.
- Session Start versus reconciliation start: the Session is wholly visible as a
  blocker or Session Start rejects after `CLOSING`.
- Open Shift versus final close: the active-Shift unique index prevents overlap;
  an open operation ordered after closure may create the next Shift.

### 11.3 Final Source Verification

Although normal writers cannot change a `CLOSING` Shift, Final Close reloads
every source total and compares it to the reconciliation snapshot. Any mismatch
returns a conflict and writes no closure. This detects an uncoordinated future
writer or corrupted state rather than freezing unexplained numbers.

---

## 12. Error Contract

New stable errors include:

- `SALES_SHIFT_ALREADY_CLOSING`
- `SALES_SHIFT_ALREADY_CLOSED`
- `SHIFT_RECONCILIATION_NOT_STARTED`
- `SHIFT_UNSETTLED_CHECK`
- `SHIFT_PENDING_REFUND`
- `SHIFT_UNRESOLVED_CORRECTION`
- `SHIFT_ACTIVE_SERVICE_SESSION`
- `SHIFT_CASH_RECOUNT_REQUIRED`
- `SHIFT_QR_RECHECK_REQUIRED`
- `SHIFT_RECONCILIATION_STALE`
- `SHIFT_RECONCILIATION_SOURCE_CHANGED`
- `SHIFT_RECONCILIATION_CALCULATION_FAILED`
- `SHIFT_DISCREPANCY_REASON_REQUIRED`
- `SHIFT_DISCREPANCY_REASON_UNEXPECTED`

Mapping:

| Condition | HTTP status |
| --- | ---: |
| Malformed request, omitted explicit value, invalid reason/note | `400` |
| Missing, locked, expired, or revoked session | `401` |
| Missing capability or failed Manager Approval | `403` |
| Unknown Shift or attempt id | `404` |
| Lifecycle, blocker, recount, stale evidence, source mismatch | `409` |
| Request-caused money range failure | `422` |
| Corrupt stored result, unknown constraint, infrastructure failure | `500` |

Constraint mapping is by known PostgreSQL constraint name. Phase 07 narrows the
old blanket Shift mapping that treated every unique violation as already-open
and every `sql.ErrNoRows` as open-Shift-required. Before the initial count
commits, `EXPECTED_CASH_OUT_OF_RANGE` and every calculation failure use the
generic `SHIFT_RECONCILIATION_CALCULATION_FAILED` message and expose no values or
operands. Infrastructure failures remain the generic `500` envelope.

---

## 13. Audit Contract

Phase 07 introduces:

- `SHIFT_RECONCILIATION_STARTED`
- `SHIFT_CASH_COUNT_RECORDED`
- `SHIFT_QR_OBSERVATION_RECORDED`
- `SHIFT_CLOSED_EXACT`
- `SHIFT_CLOSED_WITH_DISCREPANCY`

Starting reconciliation writes both the start and initial-count events in the
same transaction. Each later attempt writes one event. Closure details contain
the snapshot id, final evidence ids, all three differences, discrepancy reasons,
and approver identity when present. Existing first-class actor/session columns
identify the initiator.

Failed authorization retains the executor's denial event behavior. Failed
business validation writes no success event and no idempotency result.

---

## 14. Testing

### 14.1 Unit Tests

- Expected Cash and Manual QR equations.
- Checked signed differences and boundary failures.
- Reason allowlist, note trimming, and `OTHER` requirements.
- Valid and invalid reason-to-dimension pairings.
- Exact reason-to-nonzero-dimension matching.
- Cash recount and QR recheck requirements.
- Deterministic blocker precedence.
- DTO serialization with empty arrays and no credentials.

### 14.2 Migration And PostgreSQL Tests

- `OPEN`, `CLOSING`, and `CLOSED` state constraints.
- At most one `OPEN` or `CLOSING` Shift.
- One reconciliation and one closure per Shift.
- Attempt sequence uniqueness and non-negative bounds.
- Closure approver/difference consistency.
- Unique, nonzero discrepancy dimensions and reason/note constraints.
- Closed snapshot remains unchanged when later additive rows are seeded.

### 14.3 Command Integration Tests

- Initial count commits before reveal and rolls back with every injected failure.
- Pre-commit range and infrastructure errors expose no reconciliation amount.
- Every global blocker and progressive precedence combination.
- Exact closure from first Cash Count and first QR Observation.
- Cash shortage/excess, QR received difference, QR refunded difference, and all
  combined discrepancy sets.
- Difference requires a second relevant attempt.
- Every attempt retains independent actor, session, sequence, and time.
- Explicit QR zeroes succeed; omitted values fail.
- Manager self-approval and another-Manager approval.
- Disabled, demoted, wrong-PIN, or missing approval denies without persistence.
- Exact and discrepant idempotent replay and request conflict.
- Latest-attempt requirement and source-snapshot mismatch.
- New Shift opens after closure.

### 14.4 Concurrency Tests

Tests use held row locks and PostgreSQL waiter observation rather than sleeps:

- Session Start against Reconciliation Start.
- Payment/Refund/Void/Comp/Cash Movement against Reconciliation Start.
- Commit and Session closure against Reconciliation Start.
- Two Reconciliation Starts with different actors and request ids.
- Attempt append against Final Close.
- Two Final Closes with different actors and request ids.
- Open new Shift against Final Close.

### 14.5 HTTP And Read Tests

- `OPEN` current response contains no money or derivation input.
- Cash Movement success contains no Expected Cash or derivation total.
- `CLOSING` current response is coherent and contains all attempts.
- `CLOSED` disappears from current and appears in Manager history.
- Cashier cannot list or read closed history; Manager can.
- Date-range pagination is stable at equal timestamps.
- Pagination rejects missing/invalid ranges, malformed or mismatched cursors,
  and never duplicates or omits equal-time rows.
- Validation, UUID parsing, capability guards, stable errors, and Swagger.
- No response or stored payload contains Manager PIN.

The Shift package seeds Sales-owned facts through SQL fixtures and does not
import `internal/sales`. Existing per-package PostgreSQL template clones remain
the integration isolation mechanism.

---

## 15. Documentation And Delivery

Implementation updates:

- Swagger routes and DTO schemas.
- The stale current-Shift description that says Refund is unimplemented.
- Shift comments that still defer Close to Phase 5.
- `ROADMAP.md` and the Phase 07 backlog status only after every acceptance test
  passes.

The implementation does not rewrite this approved specification. Any behavioral
change discovered during implementation requires an ADR and an explicit spec
follow-up rather than silent divergence.

### 15.1 Delivery Checkpoints

Phase 07 remains one specification because redaction, the active-Shift index,
the durable snapshot, and closure are one safety boundary and cannot ship in
contradictory combinations. Its implementation plan must still use three review
checkpoints:

1. **7A - Persistence and gate:** migration, state/index, redacted Shift DTOs,
   generic pre-reveal errors, global blocker queries, and Session Start lock.
2. **7B - Reconciliation and closure:** start, attempt ledgers, exact/discrepant
   close, approval, audit, and command integration tests.
3. **7C - Inspection and hardening:** current/closed reads, Manager history,
   pagination, concurrency/failure-injection suites, Swagger, and roadmap close.

Each checkpoint must compile and pass its relevant tests before the next begins;
only 7C completes the roadmap phase.

---

## 16. Decision Records

This design adds:

- ADR-049: Blind Shift reconciliation uses a non-abortable `CLOSING` state.
- ADR-050: Shift closure uses global blockers and the Shift row as its writer
  gate.
- ADR-051: Closure preserves normalized attempts and a three-dimensional
  immutable discrepancy snapshot.
- ADR-052: Closed Shift history requires `audit.inspect`.

---

## 17. Acceptance Trace

| Backlog criterion | Design coverage |
| --- | --- |
| Active Session, unsettled Check, pending Refund, unresolved correction blockers | Sections 8 and 11 |
| Blind initial count and retained recount actors/times | Sections 4, 5, and 9 |
| Correct Expected Cash including Void and Refund effects | Section 6 |
| Separate expected QR received/refunded | Sections 5 and 6 |
| Explicit observed QR zeroes; no screenshot/sender | Sections 1, 5, and 9 |
| Recount/recheck, reasons, Manager Approval | Sections 7 and 10 |
| Visible discrepancy; no balancing transaction | Sections 5, 6, and 9 |
| Atomic immutable closure snapshot | Sections 4, 5, and 11 |
| No reopen/current action; authorized inspection | Sections 4, 9, and 10 |
| New Shift after closure | Sections 5 and 11 |
| Integration and concurrency coverage | Section 14 |
| UI | Deferred to Phase 11 |
