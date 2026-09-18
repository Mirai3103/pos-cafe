# Design Specification: Preparation Queue Reads & Bulk Transitions (`internal/preparation`, Phase 6A)

- **Author:** OpenCode & Team
- **Date:** 2026-09-16
- **Status:** Approved
- **Phase:** Phase 6A, the first sub-phase of Preparation Station / Kitchen Display
- **Predecessor:** [`2026-09-15-sales-submission-closure-design.md`](2026-09-15-sales-submission-closure-design.md) (Phase 5D)

---

## 1. Purpose

Phase 5D created `internal/preparation`, created one Preparation Unit per physical item at Submit, and exposed the single-unit linear advance command needed to complete a sale. Phase 6A turns that command-only boundary into an operational bar queue without yet adding exceptional correction workflows.

The implementation follows the canonical behavior in `cafe-pos/src/preparation`, `CONTEXT.md`, and the existing Go architecture. Where the old checklist in `MIGRATE_PLAN.md` conflicts with the shipped Phase 5D boundary, the shipped boundary wins: Submit already creates Preparation Units atomically, so Phase 6A does not recreate them through an asynchronous event consumer.

### Goals

1. Expose a privacy-safe, side-effect-free active Preparation Queue ordered FIFO.
2. Project current Table assignments and total physical unit count without exposing commercial or financial data.
3. Record the instant a unit enters `IN_PREPARATION` so all clients can display server-authoritative aging.
4. Advance up to 50 selected units in one idempotent request while preserving successful units when another selected unit is stale or invalid.
5. Keep the existing single-unit command and the new bulk command on one transition implementation.
6. Preserve current authorization, idempotency, audit, transition-history, and concurrency guarantees.

### Non-Goals

- Cancellation or change notices, Preparation Alerts, and alert acknowledgment.
- Waste, Remake, Remake priority, and correction-history reads.
- Manager-PIN Preparation State Correction.
- Refund, Comp, pending-refund closure policy, or Check charge mutation.
- Frontend polling timers or TanStack Query integration; those belong to Phase 7.
- Server-Sent Events, WebSockets, or another server-push transport.
- Publishing or consuming `order.submitted` through Watermill.
- Extracting a shared executor from the existing vertical slices.

### Accepted Consequences

The Phase 6A queue response contains only `observed_at` and `units`. Phase 6B adds alert and correction collections when their persistence and behavior exist. This is an additive API change before Phase 7 integrates the frontend, so placeholder response fields provide no compatibility value.

The queue uses polling. The canonical client polls every five seconds and invalidates immediately after a local mutation. Phase 6A provides the pure GET required by that model but does not prescribe a backend polling interval.

---

## 2. Authority And Terminology

The canonical terms remain unchanged:

- A **Preparation Queue** is the shared FIFO view of submitted Preparation Units. Staff may work out of order; only a future Remake carries explicit priority.
- A **Preparation Unit** is one physical unit tracked independently. It is never exclusively assigned to one Barista.
- `QUEUED`, `IN_PREPARATION`, and `READY` are active queue states.
- `FULFILLED`, `CANCELLED`, and `WASTED` are terminal states and do not appear in the active queue.
- `FULFILLED` means handed to the customer or table, not merely prepared.
- **Observed At** is the PostgreSQL instant at which the queue snapshot was read. Clients age units from this authority instead of trusting local clocks.

The queue is an operational projection. It contains item identity and preparation facts, but no Menu Price, Check balance, Payment, Sales Shift money, or staff credential fields.

---

## 3. Position In Phase 6

Phase 6 is decomposed because the canonical Preparation module spans independent consistency boundaries:

| Sub-phase | Scope | Status |
| --- | --- | --- |
| **6A** | Active queue read, server-authoritative aging, bulk advance | This specification |
| **6B** | Alerts, acknowledgment, Waste, Remake, Remake priority, state correction | Deferred |
| **6C** | Cancellation/change and the required Check, Refund, Comp, and closure integration | Deferred |

Cancellation is not grouped into 6A or 6B. It removes charge from submitted work and can create a Refund obligation, while the Go contract deliberately has no `pending_refund_vnd` yet (ADR-029). Designing Cancellation without that financial boundary would either strand a sale or allow closure with money still owed.

---

## 4. Architecture

### 4.1 Package Boundary

The Phase 5D boundary remains:

- `internal/sales` creates Preparation Units synchronously in the Submit transaction and reads their state for Service Session and Completed Sale projections.
- `internal/preparation` owns every Preparation Unit state transition and the active queue projection.
- `internal/preparation` may read `orders`, `order_items`, `table_assignments`, and `tables` through its own SQL, following ADR-012 and ADR-024. It does not write those tables.

No event consumer sits between Submit and queue visibility. A successful Submit and its Preparation Units remain one atomic PostgreSQL commit.

### 4.2 Components

`internal/preparation` gains:

- An `ExecuteRead` path using a read-only, repeatable-read transaction.
- An `ActiveQueueHandler` that assembles one queue snapshot with set-based queries.
- A `BulkAdvanceHandler` that uses the existing mutation executor and PostgreSQL savepoints.
- A package-private savepoint primitive on `MutationContext`; callers receive no general-purpose transaction API and cannot construct savepoint SQL from request data.
- A shared unit-transition helper used by single and bulk advance.
- Queue and bulk DTOs, HTTP handlers, route registration, Swagger annotations, and error mapping.

The existing `Runner`, authority reload, idempotency storage, and audit tables remain in place. Phase 6A does not refactor other slices to share these internals.

### 4.3 Operations

| Operation | Method and path | Capability | Result |
| --- | --- | --- | --- |
| Read active queue | `GET /api/v1/preparation/queue` | `preparation.operate` | Queue snapshot |
| Advance one unit | `POST /api/v1/preparation/units/:unit_id/advance` | `preparation.operate` | Preparation Unit |
| Advance many units | `POST /api/v1/preparation/units/advance-many` | `preparation.operate` | Per-unit outcomes |

Manager and Barista have `preparation.operate`; Cashier does not unless another assigned role contributes that capability.

---

## 5. Database Design

Migration `000012_add_preparation_queue_fields.sql` adds one nullable column:

```sql
ALTER TABLE preparation_units
    ADD COLUMN IF NOT EXISTS in_preparation_at TIMESTAMPTZ;
```

The migration backfills the column from the earliest existing `preparation_unit_transitions.occurred_at` whose `resulting_state = 'IN_PREPARATION'`. Units that have never entered preparation remain `NULL`.

`in_preparation_at` is denormalized current-state evidence for the queue's hot read path. The transition table remains the authoritative history. A successful `QUEUED -> IN_PREPARATION` writes the state, timestamp, transition row, and audit evidence in one transaction.

No priority or Remake link is added in 6A. Phase 6B adds those fields with the behavior that uses them.

The existing `(state, queued_at)` queue index remains sufficient. Queries add `id` as the deterministic in-memory or SQL tie-break; no new index is justified until measurement shows one is needed.

---

## 6. Queue Projection

### 6.1 Read Transaction

`ActiveQueueHandler.Handle` uses a read-only transaction at `REPEATABLE READ`:

1. Reload current session and identity authority.
2. Require `preparation.operate`.
3. Read `clock_timestamp()` from PostgreSQL as `observed_at`.
4. Read active units in `queued_at, id` order.
5. Raise `observed_at` to any later visible unit timestamp so Phase 5D rows stamped by a skewed application clock cannot produce a negative age.
6. Read total unit counts for the selected Order Item ids, including terminal siblings.
7. Resolve each Order Item to its Service Session and read current, unreleased Table assignments in assignment-sequence order.
8. Assemble and return the snapshot, then commit the read transaction.

All projection queries observe one database snapshot. They are set based; the handler does not issue one query per unit.

An authorized empty queue returns `200` with `units: []`. It is not a not-found condition.

### 6.2 Active Unit Rules

The queue includes only units whose current state is:

```text
QUEUED
IN_PREPARATION
READY
```

FIFO is the default display order, not a work claim. Reading a unit does not assign it, start it, or change any state. Multiple staff-LAN clients may poll concurrently.

Current Table names are projected at read time. Moving a Dine-in Session after submission changes the next queue read; the immutable preparation snapshot is not rewritten. Takeaway units have `table_names: []`.

### 6.3 Response Contract

```json
{
  "observed_at": "2026-09-16T08:00:00Z",
  "units": []
}
```

Each queue unit carries:

- `id`, `order_item_id`, `unit_number`, and `order_item_unit_count`.
- `state`, `service_number`, and `table_names`.
- `category_name`, `item_name`, `size_name`, `modifiers`, and `preparation_note`.
- `queued_at` and nullable `in_preparation_at`.

`order_item_unit_count` counts every physical unit for the Order Item, including units that have already left the active queue. This lets a grouped card still say, for example, "2 of 3" after one sibling is fulfilled.

The response deliberately omits prices, allocations, Checks, Payments, balances, Sales Shift ids, and actor credentials.

---

## 7. Transition Commands

### 7.1 Shared Transition Helper

Single and bulk advance call one helper that:

1. Locks the Preparation Unit `FOR UPDATE`.
2. Returns `UNIT_NOT_FOUND` if it does not exist.
3. Requires the target to be the immediate successor in the linear graph.
4. Chooses one occurrence time for the successful move.
5. Updates state and, for `IN_PREPARATION`, sets `in_preparation_at` to that occurrence time.
6. Inserts one `preparation_unit_transitions` row.
7. Returns the resulting Preparation Unit snapshot and audit details.

The legal graph remains:

```text
QUEUED -> IN_PREPARATION -> READY -> FULFILLED
```

Targets are explicit. A stale client therefore receives `INVALID_TRANSITION` instead of accidentally advancing a unit twice.

### 7.2 Bulk Request

The request contains:

```json
{
  "request_id": "uuid",
  "preparation_unit_ids": ["uuid"],
  "target_state": "IN_PREPARATION"
}
```

Boundary rules:

- `request_id` must be non-zero.
- The input list must contain 1 through 50 ids.
- Duplicate ids select the unit once, at their first position.
- `target_state` must be `IN_PREPARATION`, `READY`, or `FULFILLED`.
- The normalized first-occurrence list and target form the idempotency fingerprint.

### 7.3 Savepoint Processing

The outer mutation transaction performs authority verification and claims idempotency once. Unique unit ids are then processed in UUID order so overlapping bulk requests acquire row locks consistently.

Each selected unit runs inside a PostgreSQL savepoint:

- Success releases the savepoint and records an `ADVANCED` outcome.
- `UNIT_NOT_FOUND` or `INVALID_TRANSITION` rolls back to the savepoint and records a `FAILED` outcome.
- Any unexpected database, serialization, audit, or stored-result error aborts the entire outer transaction.

The savepoint helper uses fixed SQL controlled by the package, not names supplied by a caller. It runs the transition callback against the same transaction-scoped sqlc queries and always releases or rolls back the savepoint before returning.

The response restores first-selection order even though locks are taken in sorted order.

Every successful unit gets one transition row and one `PREPARATION_UNIT_ADVANCED` audit event. Failed units write neither. After savepoint processing, the bulk handler inserts successful audit details with the existing batch-audit query pattern before returning to the executor. Audit insertion and idempotency-result storage remain in the same outer transaction as all successful moves; either failure rolls back every successful unit.

### 7.4 Bulk Response And Replay

```json
{
  "target_state": "READY",
  "outcomes": [
    {
      "preparation_unit_id": "uuid",
      "status": "ADVANCED",
      "unit": {}
    },
    {
      "preparation_unit_id": "uuid",
      "status": "FAILED",
      "code": "INVALID_TRANSITION"
    }
  ]
}
```

A successful outcome contains the same Preparation Unit snapshot shape as the single-unit mutation, including `in_preparation_at`. It does not contain queue-only current Table names or sibling counts.

The command returns `200` even when some or all outcomes are `FAILED`: the request itself completed and reports each selected unit explicitly. The exact result is stored in the shared idempotency table and replayed after current authority is revalidated. Replay creates no additional state change, transition row, or audit event.

---

## 8. Authorization And Security

Both reads and mutations reload authority inside their transaction. Middleware checks improve the common path but are not the security boundary.

The queue read requires `preparation.operate`. A denied read cannot write its audit evidence in its read-only transaction, so it uses the same short separate write transaction pattern as other slice reads.

The projection itself enforces least privilege: financial fields never enter the DTO, so they cannot leak through accidental JSON serialization or a frontend rendering mistake.

Bulk processing accepts only parsed UUIDs and fixed enum values. Savepoint names and SQL are static; no request value is interpolated into SQL syntax.

---

## 9. Error Contract

The existing Preparation error codes remain sufficient.

Top-level HTTP mapping:

| Condition | Status |
| --- | ---: |
| Malformed UUID, empty or oversized selection, invalid target | `400` |
| Missing, locked, expired, or revoked session | `401` |
| Missing current capability or disabled identity | `403` |
| Idempotency key reused for a different normalized request | `409` |
| Corrupt stored result or unexpected infrastructure failure | `500` |

Within a completed bulk response, `UNIT_NOT_FOUND` and `INVALID_TRANSITION` are per-unit failure codes rather than top-level HTTP errors. The existing single-unit route continues mapping those conditions to `404` and `409` respectively.

Unexpected errors are never converted into partial outcomes. That distinction prevents a database outage or failed audit write from looking like an ordinary stale unit.

---

## 10. Concurrency And Idempotency

- **Two clients advance one unit to the same target.** The row lock serializes them. One succeeds; the other observes the new state and receives `INVALID_TRANSITION`.
- **Overlapping bulk requests.** Both lock unique ids in UUID order, preventing a lock-order cycle. Locks remain held until the outer transaction finishes.
- **A bulk request contains a stale and a valid unit.** The stale savepoint rolls back; the valid savepoint commits with the outer transaction.
- **A failure occurs after several unit successes.** An unexpected error rolls back the outer transaction, including all prior unit changes and the idempotency claim.
- **A response is lost.** Retrying the same actor, request id, normalized ids, and target returns the stored result without repeating work.
- **Authority is revoked after a success.** Authority is checked before replay, so the old success cannot be retrieved through a now-unauthorized session.
- **Queue reads race a transition or Table reassignment.** Repeatable-read returns one internally consistent old or new snapshot, never a mixture assembled across commits.

---

## 11. Testing

### 11.1 Unit Tests

- First-occurrence duplicate normalization and stable response ordering.
- Selection-size and target validation.
- Every legal and illegal transition remains covered through the shared helper.
- Queue DTO serialization uses `[]`, not `null`, for empty collections.

### 11.2 PostgreSQL Integration Tests

- Empty active queue.
- FIFO ordering and `id` tie-break for equal timestamps.
- Active-state inclusion and terminal-state exclusion.
- PostgreSQL `observed_at` and populated `in_preparation_at` after start.
- Current Table assignment replacement is visible without rewriting the unit.
- Takeaway has no Table names.
- `order_item_unit_count` includes terminal siblings.
- Single advance still writes one state change, transition, and audit event.
- Bulk all-success, mixed success/failure, and all-failure results.
- Duplicate ids select once; 50 ids are accepted and 51 rejected.
- Output follows first-selection order while locks follow sorted order.
- Replay creates no duplicate transitions or audit events; conflicting reuse is rejected.
- Concurrent and overlapping bulk requests do not deadlock and never apply one step twice.
- A forced unexpected failure rolls back every successful savepoint and the idempotency claim.

### 11.3 Authorization, HTTP, And Documentation Tests

- Manager and Barista can read and advance; Cashier alone cannot.
- Locked, expired, revoked, disabled, and role-revoked actors are rejected using current authority.
- Denied reads and mutations leave authorization-denied audit evidence.
- Queue JSON contains no price, Check, Payment, balance, Shift-money, or credential fields.
- Route wiring, request validation, response envelopes, and error status mapping.
- Swagger includes Bearer security, request schemas, result schemas, and documented failures for all three Preparation operations.

### 11.4 Regression Verification

The implementation must pass `go build ./...`, `go vet ./...`, the configured linter, the complete unit and PostgreSQL integration suites, and race-focused Preparation concurrency tests. The existing public flow from Submit through fulfillment and Service Session closure remains green.

---

## 12. Decision Record Updates

- **ADR-032: Phase 6 is decomposed.** Queue and ordinary transitions ship before correction workflows; Cancellation waits for its financial dependencies.
- **ADR-033: Submit remains the queue creation boundary.** Preparation Units are not recreated asynchronously through Watermill because atomic Submit already provides stronger delivery semantics.
- **ADR-034: Bulk advance uses per-unit savepoints.** Operationally stale units become typed outcomes while infrastructure failures remain transaction-wide failures.
- **ADR-035: Launch queue freshness uses polling.** A pure GET plus client invalidation satisfies the staff-LAN requirement without long-lived server connections or background fan-out.

These decisions are added to `spec/decisions.md` during implementation, when their code and tests land.

---

## 13. Acceptance Criteria

1. An authorized Manager or Barista can read an empty or populated active queue; an unauthorized actor cannot.
2. Queue units are FIFO by `queued_at, id`, include only active states, and expose no financial data.
3. Queue `observed_at` comes from PostgreSQL, and `in_preparation_at` is written when a unit starts.
4. Current Table names and total Order Item unit count are projected correctly in one consistent read snapshot.
5. Single advance preserves its public behavior and uses the same transition implementation as bulk advance.
6. Bulk advance accepts 1–50 ids, selects duplicates once, locks deterministically, and returns outcomes in first-selection order.
7. A stale or missing selected unit does not roll back valid selected units; an unexpected failure rolls back all of them.
8. Every successful move creates exactly one transition and one business audit event; every failed outcome creates neither.
9. Same-input replay returns the stored response without duplicate work, while conflicting request-id reuse is rejected.
10. Submit still creates Preparation Units directly and atomically; no Watermill consumer, SSE, or WebSocket is introduced.
11. Swagger and HTTP tests cover the complete 6A surface.
12. Build, vet, lint, full tests, PostgreSQL integration tests, and concurrency verification pass.
