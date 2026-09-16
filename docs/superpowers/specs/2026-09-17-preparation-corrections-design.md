# Design Specification: Preparation Corrections & Recovery (`internal/preparation`, Phase 6B)

- **Author:** OpenCode & Team
- **Date:** 2026-09-17
- **Status:** Approved
- **Phase:** Phase 6B, the second sub-phase of Preparation Station / Kitchen Display
- **Predecessor:** [`2026-09-16-preparation-queue-transitions-design.md`](2026-09-16-preparation-queue-transitions-design.md) (Phase 6A)

---

## 1. Purpose

Phase 6A shipped the active Preparation Queue, server-authoritative aging, and idempotent single and bulk forward transitions. Phase 6B adds the exceptional preparation workflows needed when prepared work is lost or an operational state was recorded incorrectly: alerts and acknowledgment, Waste, Remake, Remake-only priority, and Preparation State Correction.

The implementation is business-equivalent to the canonical TypeScript Preparation module while remaining native to the Go architecture. In particular, the Go service keeps correction facts in typed tables, uses the shared idempotency executor, and records every current-state change in `preparation_unit_transitions` rather than reconstructing business history from audit JSON.

### Goals

1. Record an `IN_PREPARATION` or `READY` unit as `WASTED` without deleting or rewriting its history.
2. Create a prominent Waste alert that remains active until an identified staff member acknowledges it.
3. Create at most one linked Remake for a Waste, without adding a new customer charge.
4. Prioritize active Remakes ahead of ordinary FIFO work without introducing a general rush mechanism.
5. Correct one mistaken preparation transition backward for 1 through 50 selected units in one atomic, auditable command.
6. Require a current Manager to re-enter their own PIN for every State Correction, including replay.
7. Extend the queue with active alerts and recent Waste/Remake history while preserving its privacy boundary and repeatable snapshot.
8. Preserve authorization, idempotency, audit, closure, and concurrency guarantees from Phases 5D and 6A.

### Non-Goals

- Cancellation or change commands.
- Writing `CANCELLATION` or `CHANGE` alerts; Phase 6B declares those kinds for the complete alert domain, but Phase 6C creates them.
- Refund, Comp, Payment Void, pending-refund closure policy, or Check charge mutation.
- General rush flags, arbitrary priorities, promised preparation times, or SLA scheduling.
- Editing or deleting Waste, Remake, alert, correction, transition, or audit history.
- State Correction across more than one reverse lifecycle step.
- State Correction of `CANCELLED` or `WASTED` units.
- Frontend integration, polling timers, or TanStack Query behavior; those remain Phase 7 work.
- Server-Sent Events, WebSockets, or another server-push transport.
- Extracting a shared executor across vertical slices.

### Accepted Consequences

Phase 6B adds fields to Preparation Unit projections and adds `alerts` and `corrections` to the queue response. Phase 7 has not integrated the frontend yet, so additive contract changes require no compatibility placeholders.

An unacknowledged alert does not block Service Session closure. Alert acknowledgment means only that staff saw the operational notice; it has no commercial or financial effect. A Wasted unit is terminal for closure, while a newly created Remake is nonterminal and therefore blocks closure until it reaches a terminal state.

---

## 2. Authority And Terminology

The canonical terms remain:

- **Waste** is a submitted physical unit that cannot be delivered after preparation began. Its Waste fact preserves actor, time, prior state, reason, and note.
- **Remake** is a new Preparation Unit linked to a Wasted source unit and prepared without charging the customer again.
- **Preparation Alert** is an operational notice that remains active until an identified staff member acknowledges seeing it.
- **Preparation State Correction** is an auditable declaration that one recorded transition was mistaken. It preserves that transition and restores the immediately preceding operational state.
- `STANDARD` and `REMAKE` are the only priority values. Only an active Remake receives launch priority.
- Alert acknowledgment records who saw the notice and when. It never changes a unit, Check, Payment, charge, Refund, or Comp.

Stable API and persistence reason codes are operation-scoped:

| Operation | Allowed reasons |
| --- | --- |
| Waste | `PREPARATION_ERROR`, `QUALITY_FAILURE`, `CUSTOMER_REQUEST`, `OTHER` |
| Remake | `PREPARATION_ERROR`, `QUALITY_FAILURE`, `OTHER` |
| State Correction | `STATE_RECORDED_IN_ERROR`, `OTHER` |
| Cancellation/change alert (reserved for 6C) | `CUSTOMER_REQUEST`, `ORDER_ENTRY_ERROR`, `ITEM_UNAVAILABLE`, `OTHER` |

Every optional note is trimmed. A present note is 1 through 500 characters, and `OTHER` requires a non-empty note. Recorded reasons and notes are immutable.

---

## 3. Position In Phase 6

| Sub-phase | Scope | Status |
| --- | --- | --- |
| **6A** | Active queue read, server-authoritative aging, bulk advance | Completed |
| **6B** | Alerts, acknowledgment, Waste, Remake, Remake priority, state correction | This specification |
| **6C** | Cancellation/change and Check, Refund, Comp, and closure integration | Deferred |

The alert table declares `CANCELLATION`, `CHANGE`, and `WASTE` from its first migration. This follows the complete-domain precedent of ADR-028 and lets Phase 6C add alert writers without reopening the alert enum or response contract. Phase 6B writes only `WASTE`.

The transition constraint is narrower: it admits only transitions whose commands exist. Phase 6B adds Waste and reverse-correction pairs; Phase 6C adds `QUEUED -> CANCELLED` with the Cancellation command that owns that behavior.

---

## 4. Architecture

### 4.1 Package Boundary

The existing boundary remains:

- `internal/sales` creates original Preparation Units synchronously at Submit and reads all Preparation Units for Service Session and Completed Sale projections.
- `internal/preparation` owns every Preparation Unit state transition, queue projection, alert lifecycle, Waste, Remake, and State Correction.
- `internal/preparation` may read and lock the owning Order Item and Service Session through its own SQL. It does not mutate Sales tables.
- A Remake Unit is created by `internal/preparation`, not `internal/sales`, because it is the replacement fact produced by a Preparation correction rather than a new submitted sale line.

No event bus participates in these transactions. Each business fact, current-state change, transition row, audit row, and idempotent result commits atomically in PostgreSQL.

### 4.2 Components

Phase 6B adds or extends:

- Migration `000013_add_preparation_corrections.sql`.
- Preparation sqlc queries for alerts, Waste, Remake, correction facts, session/order-item locking, queue projection, and batch audit insertion.
- `AcknowledgeAlertHandler`, `WasteUnitHandler`, `RemakeUnitHandler`, and `CorrectStateHandler`.
- Preparation DTOs, errors, reason validation, route handlers, and Swagger annotations.
- The Preparation mutation executor's optional current-Manager own-PIN verification.
- The active queue projection with alerts and recent Waste/Remake history.
- Sales Service Session and Completed Sale projections with Remake metadata.

The package does not gain a repository interface or a generalized correction framework. Existing concrete `Runner`, `MutationContext`, sqlc queries, and package-private helpers remain the appropriate boundary.

### 4.3 HTTP Operations

| Operation | Method and path | Capability and approval | Success |
| --- | --- | --- | ---: |
| Read active queue | `GET /api/v1/preparation/queue` | `preparation.operate` | `200` |
| Acknowledge alert | `POST /api/v1/preparation/alerts/:alert_id/acknowledge` | `preparation.operate` | `200` |
| Waste unit | `POST /api/v1/preparation/units/:unit_id/waste` | `preparation.operate` | `201` |
| Create Remake | `POST /api/v1/preparation/wastes/:waste_id/remake` | `preparation.operate` | `201` |
| Correct states | `POST /api/v1/preparation/units/correct-state` | Manager + `preparation.operate` + actor's fresh PIN | `200` |

Manager and Barista may acknowledge, Waste, and Remake. Cashier alone may not. State Correction is Manager-only and uses the current actor's PIN; it is not second-party Manager Approval.

---

## 5. Database Design

### 5.1 Preparation Unit Remake Metadata

`preparation_units` gains:

- `priority TEXT NOT NULL DEFAULT 'STANDARD'`.
- `remake_of_preparation_unit_id UUID NULL REFERENCES preparation_units(id) ON DELETE RESTRICT`.

Existing rows backfill to `STANDARD` with no Remake link. A check constraint permits exactly:

```text
STANDARD + null remake link
REMAKE   + non-null remake link
```

The existing `(state, queued_at)` index remains. The queue is small, the priority expression has only two values, and no measured result justifies another index in 6B.

### 5.2 Preparation Alerts

`preparation_alerts` stores:

- Identity: `id`, `preparation_unit_id`.
- Meaning: `kind`, `reason`, nullable `note`.
- Creation evidence: actor Staff Identity, Staff Access Session, and `created_at`.
- Acknowledgment evidence: nullable actor Staff Identity, Staff Access Session, and `acknowledged_at`.

Constraints enforce:

- Kind is `CANCELLATION`, `CHANGE`, or `WASTE`.
- Acknowledgment fields are either all null or all present.
- `WASTE` reasons use the Waste reason catalog.
- Future `CANCELLATION` and `CHANGE` reasons use the reserved Cancellation catalog: `CUSTOMER_REQUEST`, `ORDER_ENTRY_ERROR`, `ITEM_UNAVAILABLE`, or `OTHER`.
- Note length and `OTHER` requirements hold at the database boundary.

An index on `(acknowledged_at, created_at, id)` supports active-alert reads. No unique unit constraint exists because Phase 6C may produce more than one meaningful alert over a unit's history.

### 5.3 Waste

`preparation_wastes` stores:

- `id` and unique `preparation_unit_id`.
- `prior_state`, constrained to `IN_PREPARATION` or `READY`.
- `reason`, nullable `note`.
- Actor Staff Identity, Staff Access Session, and `occurred_at`.

The unique unit reference makes Waste a single terminal fact. The source Preparation Unit remains present with current state `WASTED`.

### 5.4 Remake

`preparation_remakes` stores:

- `id`.
- Unique `waste_id`.
- Unique replacement `preparation_unit_id`.
- `reason`, nullable `note`.
- Actor Staff Identity, Staff Access Session, and `created_at`.

One Waste therefore has at most one direct Remake. If that Remake is later Wasted, its own Waste may create the next Remake, forming an explicit chain through each unit's `remake_of_preparation_unit_id`.

### 5.5 State Corrections

`preparation_state_corrections` stores:

- `id` and `preparation_unit_id`.
- `prior_state` and `resulting_state`.
- `reason`, nullable `note`.
- Actor Staff Identity, Staff Access Session, and `occurred_at`.

The legal correction pairs are:

```text
IN_PREPARATION -> QUEUED
READY          -> IN_PREPARATION
FULFILLED      -> READY
```

An index on `(preparation_unit_id, occurred_at, id)` supports per-unit history. Corrections are append-only; repeated future mistakes produce separate facts.

### 5.6 Typed Transition History

The `preparation_unit_transitions` pair constraint keeps the three forward pairs and adds:

```text
IN_PREPARATION -> WASTED
READY          -> WASTED
IN_PREPARATION -> QUEUED
READY          -> IN_PREPARATION
FULFILLED      -> READY
```

Waste and State Correction write this table in the same transaction as their dedicated fact table and current-state update. The dedicated fact answers why the exceptional action happened; the transition answers how the unit's state changed. Audit remains the separate trail of who invoked the business action.

A correction to `QUEUED` clears `in_preparation_at`. Corrections to `IN_PREPARATION` or `READY` preserve the existing timestamp because the unit did enter preparation at that earlier recorded instant.

---

## 6. Queue Projection

### 6.1 Response Contract

The queue response becomes:

```json
{
  "observed_at": "2026-09-17T08:00:00Z",
  "units": [],
  "alerts": [],
  "corrections": []
}
```

Every collection is non-null. The same read-only `REPEATABLE READ` transaction reloads authority and reads the database clock, units, current Table assignments, unit counts, active alerts, and correction history.

Preparation Unit responses gain:

- `priority`.
- Nullable `remake_of_preparation_unit_id`.

The same fields are added to mutation responses, Sales Service Session projections, and Completed Sale projections.
`order_item_unit_count` continues to count every physical unit for the Order Item, including Remakes and terminal units, so creating a Remake increases that count.

### 6.2 Unit Visibility And Ordering

The queue includes:

- Every `QUEUED`, `IN_PREPARATION`, or `READY` unit.
- A `CANCELLED` or `WASTED` unit while it has an unacknowledged alert.
- No `FULFILLED` unit.

Phase 6B can produce only the Wasted terminal case. The Cancelled rule is present so Phase 6C adds a writer without changing queue semantics.

Ordering is deterministic:

1. Active `REMAKE` units, FIFO by `queued_at, id`.
2. Active `STANDARD` units, FIFO by `queued_at, id`.
3. Terminal units retained by active alerts, ordered by `queued_at, id`.

Only active Remakes receive priority. A Wasted Remake retained for an alert is no longer recovery work and therefore appears with other alerted terminal units, not in the active priority lane.

After acknowledgment, the terminal unit and alert disappear from the active queue. The underlying unit, alert, Waste, transition, and audit records remain immutable.

### 6.3 Active Alerts

Alerts are ordered by `created_at, id` and expose:

- `id`, `kind`, and `preparation_unit_id`.
- `service_number`, `item_name`, and `unit_number` projected from the unit.
- `reason`, `note`, and `created_at`.
- Nullable acknowledgment identity and time.
- Nullable `waste_id`, resolved through the Waste fact for `WASTE` alerts.

Only unacknowledged alerts appear in the queue collection. An alert remains readable and acknowledgeable after its Service Session closes.

### 6.4 Waste And Remake History

`corrections` is the operational "Waste and Remake" history, matching the canonical client contract. It contains a union of:

- `WASTE`: Waste id, source unit, service/item/unit identity, reason, note, and occurrence time.
- `REMAKE`: Remake id, Waste id, replacement unit, source unit and source unit number, service/item/unit identity, reason, note, and creation time.

Only facts belonging to active Service Sessions appear. The combined result is ordered newest first and limited to 50 entries. Queries are set based and do not issue one query per row.

State Correction is intentionally not mixed into this collection. It remains visible through typed transition history, its dedicated fact table, and audit evidence.

### 6.5 Privacy Boundary

The queue still contains no Menu Price, Check balance, Payment, Sales Shift money, Refund, Comp, PIN, PIN hash, session token, or complete financial audit data.

---

## 7. Command Design

### 7.1 Shared Mutation Rules

All four mutations:

1. Validate and normalize boundary input.
2. Reload current authority inside the transaction.
3. Verify required capability and any Manager/PIN requirement before replay.
4. Fingerprint normalized business input with no credential fields.
5. Serialize duplicate request ids through the existing advisory lock.
6. Replay an exact stored result or reject conflicting reuse.
7. Claim idempotency before business writes.
8. Write facts, transitions, audits, and stored result in one transaction.

Unexpected SQL, audit, serialization, or idempotency failures abort the whole transaction. No infrastructure failure is translated into an ordinary domain conflict.

The stable idempotency operation names are `preparation.acknowledge_alert`, `preparation.waste_unit`, `preparation.remake_unit`, and `preparation.correct_state`.

### 7.2 Waste Unit

Request body:

```json
{
  "request_id": "uuid",
  "reason": "QUALITY_FAILURE",
  "note": null
}
```

The handler locks the unit `FOR UPDATE`, requires `IN_PREPARATION` or `READY`, chooses one occurrence time, and atomically:

1. Inserts the Waste fact.
2. Sets the current unit state to `WASTED`.
3. Inserts the typed transition.
4. Inserts an unacknowledged `WASTE` alert.
5. Inserts `PREPARATION_UNIT_WASTED` and `PREPARATION_ALERT_CREATED` audit events.
6. Stores the replayable result.

The result contains the Waste identity, prior/resulting states, reason/note/time, and created Alert. The executor's existing single-audit return does not need a cross-slice redesign; a package-private Preparation audit-batch helper writes the two events and the handler returns a zero `AuditRecord`.

### 7.3 Acknowledge Alert

Request body:

```json
{
  "request_id": "uuid"
}
```

The path supplies the Alert id. The handler locks the alert, rejects an unknown alert, and rejects an already acknowledged alert reached through a different request. It then sets acknowledgment identity, session, and time together and writes `PREPARATION_ALERT_ACKNOWLEDGED`.

An exact replay returns the original acknowledgment response. The command never changes unit state or financial meaning.

### 7.4 Create Remake

Request body:

```json
{
  "request_id": "uuid",
  "reason": "PREPARATION_ERROR",
  "note": null
}
```

The path supplies the Waste id. The handler first resolves the immutable source identifiers, then uses this lock order:

1. Owning Service Session.
2. Owning Order Item.
3. Waste and source Preparation Unit.

The Session must still be `ACTIVE`. The Order Item lock serializes `max(unit_number) + 1` allocation across different Wastes of the same item. The handler revalidates the Waste and source after taking locks, then creates:

- A new `QUEUED` Preparation Unit with a fresh id and queue timestamp.
- The next unit number for the same Order Item.
- The exact immutable preparation snapshot from the source unit.
- `priority = REMAKE` and `remake_of_preparation_unit_id = source id`.
- A Remake fact and `PREPARATION_REMAKE_CREATED` audit event.

The Remake adds no Order Item, Charge Allocation, Check charge, Payment, Refund, or Comp. The Waste alert remains active until separately acknowledged.

### 7.5 Correct Preparation State

Request body:

```json
{
  "request_id": "uuid",
  "preparation_unit_ids": ["uuid"],
  "target_state": "IN_PREPARATION",
  "reason": "STATE_RECORDED_IN_ERROR",
  "note": null,
  "manager_pin": "1234"
}
```

Boundary rules:

- `request_id` is non-zero.
- The selection contains 1 through 50 non-zero, unique ids.
- `target_state` is `QUEUED`, `IN_PREPARATION`, or `READY`.
- Reason and note follow the State Correction catalog.
- `manager_pin` has the standard 4 through 8 digit shape.

The target determines one required prior state:

| Target | Required current state |
| --- | --- |
| `QUEUED` | `IN_PREPARATION` |
| `IN_PREPARATION` | `READY` |
| `READY` | `FULFILLED` |

The executor verifies that the current actor still has the Manager role and `preparation.operate`, locks the actor's credential/role evidence for the transaction, and verifies the actor's own PIN. Verification happens before idempotency replay. The PIN is absent from the fingerprint, audit details, logs, stored response, and every database record.

The handler resolves owning Session ids, locks unique Sessions in UUID order, then locks unique units in UUID order. Every Session must be `ACTIVE`, every unit must exist, and every unit must have the required current state. Any failure rejects the whole command; State Correction does not use per-unit savepoints.

One timestamp applies to the batch. The handler updates every current state, writes one reverse transition, one correction fact, and one `PREPARATION_STATE_CORRECTED` audit event per unit, then returns outcomes in request order. Audit or result-storage failure rolls back the entire batch.

The response shape is:

```json
{
  "outcomes": [
    {
      "correction_id": "uuid",
      "preparation_unit_id": "uuid",
      "prior_state": "READY",
      "resulting_state": "IN_PREPARATION",
      "corrected_at": "2026-09-17T08:00:00Z"
    }
  ]
}
```

---

## 8. Authorization And Security

Middleware remains a common-path check, not the security boundary. Every read and mutation reloads current session, identity, role, and capability evidence in its transaction.

Waste, Remake, and acknowledgment require `preparation.operate`. Manager and Barista receive it; Cashier alone does not.

State Correction additionally requires:

- The actor currently holds the Manager role.
- The actor currently has `preparation.operate`.
- The supplied PIN verifies against that same actor's current PIN hash.

This is self re-authentication, not Manager Approval. No approver login code exists, and no reusable Manager mode or grace period is created.

A malformed PIN is a `400` boundary error. A well-formed wrong PIN, missing Manager role, or missing current capability is a `403` denial. Denials produce the existing `AUTHORIZATION_DENIED` evidence without including the attempted PIN.

Authority and PIN are checked before replay. If a Manager's PIN rotates after a successful correction, replay with the new PIN succeeds and returns the stored result; replay with the old PIN fails authorization. If the Manager is disabled or loses the role/capability, replay is denied.

All identifiers are parsed UUIDs, all operation and reason values are fixed enums, and every query is parameterized through sqlc. No request value is interpolated into SQL syntax.

---

## 9. Error Contract

Top-level HTTP mapping:

| Condition | Status |
| --- | ---: |
| Malformed UUID/request, invalid enum/reason/note, duplicate or out-of-range batch, malformed PIN | `400` |
| Missing, locked, expired, or revoked session | `401` |
| Missing capability, missing Manager role, disabled identity, or wrong current PIN | `403` |
| Unknown Preparation Unit, Alert, or Waste | `404` |
| Illegal/stale transition, alert already acknowledged, Waste already remade, closed Session, request-id conflict | `409` |
| Corrupt stored result or unexpected infrastructure/audit failure | `500` |

Expected conditions receive distinct package errors so HTTP mapping does not inspect error strings. At minimum the package distinguishes Unit, Alert, and Waste not-found conditions; invalid lifecycle transitions; already acknowledged alerts; already remade Wastes; closed Sessions; Manager/PIN denial; invalid reasons/notes; and idempotency conflict.

Database unique violations are mapped only when the violated constraint represents an expected concurrent domain race. Unknown constraint failures remain `500` and retain wrapped internal context.

---

## 10. Concurrency And Closure

- **Advance races Waste on one unit.** The unit lock serializes both. One commits; the other observes the new state and receives a conflict.
- **Advance races State Correction.** The sorted unit locks serialize both. The loser cannot silently move from an unexpected state.
- **Two requests Waste one unit.** One creates the unique Waste; the other observes `WASTED` or the unique fact and receives a conflict.
- **Two requests acknowledge one alert.** One fills the acknowledgment tuple; the other receives a conflict unless it is an exact replay.
- **Two requests Remake one Waste.** Session, Order Item, and Waste locks serialize them; the unique Waste reference permits one Remake.
- **Two different Wastes of one Order Item are remade concurrently.** The Order Item lock serializes unit-number allocation, so both receive distinct numbers.
- **Remake races closure.** Both lock the Service Session first. Closure either commits before Remake, causing Remake to reject the closed Session, or Remake creates a queued unit first, causing closure readiness to reject nonterminal work.
- **State Correction races closure.** Both lock Service Sessions first. Closure cannot commit a Completed Sale while a correction reopens a unit.
- **Waste races closure.** A Waste candidate is already nonterminal, so closure cannot pass before Waste; after Waste it remains terminal. Either snapshot is safe.
- **Acknowledgment races closure.** They may proceed independently because acknowledgment has no closure or financial meaning.

A Wasted unit satisfies the existing terminal-state closure rule. A queued, in-preparation, or ready Remake does not. Once every Remake is terminal, closure proceeds without a special Remake counter because all replacement units are real Preparation Units in the same Session.

Completed Sale includes original and Remake units, Remake links, and every forward, Waste, and reverse-correction transition from the typed history table.

---

## 11. Audit Contract

Phase 6B introduces these business event types:

- `PREPARATION_ALERT_CREATED`.
- `PREPARATION_ALERT_ACKNOWLEDGED`.
- `PREPARATION_UNIT_WASTED`.
- `PREPARATION_REMAKE_CREATED`.
- `PREPARATION_STATE_CORRECTED`.

Audit details carry stable ids and business meaning:

- Alert events identify alert, unit, kind, reason, and note as applicable.
- Waste identifies Waste, unit, prior/resulting state, reason, and note.
- Remake identifies Remake, Waste, source unit, replacement unit, reason, and note.
- State Correction identifies correction, unit, prior/resulting state, reason, and note.

Actor identity, Staff Access Session, and occurrence time remain first-class audit columns. No event contains a PIN or PIN hash. Failed authorization writes only denial evidence; failed business transactions write no success event.

---

## 12. Testing

### 12.1 Unit Tests

- Reason allowlists, trimming, 500-character bound, and required `OTHER` note.
- State Correction target-to-required-prior mapping.
- Selection rejects zero, more than 50, duplicate ids, and zero UUIDs.
- State Correction fingerprint excludes Manager PIN.
- Priority and alert-retention ordering helpers.
- Empty queue collections serialize as `[]`, never `null`.

### 12.2 PostgreSQL Integration Tests

- Migration backfills existing units to `STANDARD` and enforces Remake-link pairing.
- Waste succeeds from `IN_PREPARATION` and `READY`, and rejects every other state.
- Waste atomically writes current state, Waste fact, transition, alert, two audits, and result.
- Alert acknowledgment fills all evidence fields and removes the alert/unit from the next active queue read.
- Acknowledgment remains possible after Session closure.
- Remake copies the preparation snapshot, increments unit number, sets priority/link, and changes no charge.
- One Waste creates at most one Remake; a Wasted Remake may create the next linked Remake.
- State Correction covers all three legal reverse pairs and rejects skipped or terminal exceptional states.
- Correction to `QUEUED` clears `in_preparation_at`; the other targets preserve it.
- Correction rejects missing units, duplicates, stale units, and units belonging to closed Sessions atomically.
- Same-input replay creates no duplicate fact, unit, transition, alert, or audit.
- Conflicting request-id reuse is rejected.
- Forced audit or result-storage failure rolls back every write and the idempotency claim.

### 12.3 Queue Projection Tests

- Active Remakes sort before active Standard units; each class remains FIFO.
- Alert-retained terminal units sort after active work.
- A Wasted unit remains until its alert is acknowledged.
- Active alerts are oldest first and contain the Waste id.
- Waste/Remake history is newest first, limited to 50, and restricted to active Sessions.
- Remake and source links project correctly.
- Queue, Service Session, and Completed Sale contain no financial leakage from the new fields.
- Queue reads remain internally consistent under concurrent mutation.

### 12.4 Authorization And HTTP Tests

- Manager and Barista can Waste, Remake, and acknowledge; Cashier alone cannot.
- Only Manager can State Correct, using their own current PIN.
- Wrong PIN, removed Manager role, removed capability, disabled identity, and locked/expired/revoked sessions are denied.
- State Correction replay requires current authority and current PIN.
- PIN rotation permits replay with the new PIN and rejects the old PIN without changing the fingerprint.
- Denial audit contains no attempted PIN.
- Route binding, request validation, response envelopes, status mapping, and Swagger security/errors cover all operations.

### 12.5 Concurrency And Regression Tests

- Advance versus Waste on one unit.
- Advance versus State Correction on one unit.
- Concurrent Waste of one unit.
- Concurrent acknowledgment of one alert.
- Concurrent Remake of one Waste.
- Concurrent Remakes from different Wastes of one Order Item.
- Overlapping State Correction batches lock deterministically and remain atomic.
- Remake versus Service Session closure.
- State Correction versus Service Session closure.
- Submit -> queue -> Waste/Remake -> fulfillment -> closure.
- Submit -> advance -> State Correction -> re-advance -> closure.
- Completed Sale contains Remake metadata and complete typed transition history.

Verification runs `go build ./...`, `go vet ./...`, the configured linter, full tests, PostgreSQL integration tests, and focused Preparation concurrency tests under `-race`.

---

## 13. Decision Record Updates

Implementation records these accepted decisions in `spec/decisions.md`:

- **ADR-036: Phase 6B uses typed correction facts and typed transition history.** Waste, Remake, State Correction, and Alerts receive explicit tables; current-state changes also enter `preparation_unit_transitions` instead of being reconstructed from audit JSON.
- **ADR-037: A Remake is a linked new unit and the only launch priority.** It copies the source preparation snapshot, changes no charge, and allocates its unit number under the Order Item lock.
- **ADR-038: State Correction is an atomic one-step reverse command with Manager self re-authentication.** It accepts 1 through 50 unique units, requires the actor's current Manager role and PIN, and is not second-party approval.
- **ADR-039: Alert acknowledgment controls terminal-unit queue visibility only.** Unacknowledged exceptional units remain prominent; acknowledgment records who saw them and has no closure, commercial, or financial effect.

These ADRs and the Phase 6B completion status in `MIGRATE_PLAN.md` land with implementation and passing tests, not with this design document alone.

---

## 14. Acceptance Criteria

1. Authorized Manager or Barista can Waste an `IN_PREPARATION` or `READY` unit with a valid reason; every other state is rejected.
2. Waste atomically creates its fact, `WASTED` state, transition, active alert, audit evidence, and replayable result.
3. An unacknowledged Wasted unit remains visible after active work and disappears from the queue only after acknowledgment.
4. Alert acknowledgment records actor, session, and time without changing preparation, closure, or financial meaning.
5. One Waste creates at most one linked `REMAKE` unit with copied preparation snapshot, next unit number, and no new customer charge.
6. Active Remakes sort ahead of ordinary FIFO work; no general rush priority is introduced.
7. Remake and State Correction serialize with Service Session closure so closure cannot miss newly nonterminal work.
8. State Correction accepts 1 through 50 unique units, moves each exactly one step backward, and is all-or-nothing.
9. State Correction requires the current Manager actor's own fresh PIN before first execution or replay, and no credential enters persistence, fingerprint, response, audit, or logs.
10. Every successful exceptional state change creates exactly one typed transition and dedicated fact per unit. State Correction writes one action audit per unit; Waste writes its action audit plus the separately required Alert-created audit. Failures create none.
11. Exact replay returns the stored response without duplicate work; conflicting request-id reuse is rejected.
12. Queue reads return non-null `units`, `alerts`, and `corrections` from one repeatable snapshot with no financial leakage.
13. Service Session and Completed Sale projections include priority and Remake links, and Completed Sale history includes Waste and State Correction transitions.
14. Phase 6B creates no Cancellation/change writer, Refund, Comp, pending-refund policy, charge mutation, event consumer, SSE, or WebSocket.
15. Build, vet, lint, full tests, PostgreSQL integration tests, HTTP/Swagger tests, and race-focused concurrency verification pass.
