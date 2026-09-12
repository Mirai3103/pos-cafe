# Design Specification: Sales Shift Slice (`internal/shift`)

- **Author:** Claude Opus 5 & Team
- **Date:** 2026-09-13
- **Status:** Approved
- **Phase:** Phase 4, Sales Shift & Cash Movements

---

## 1. Purpose

This specification defines the Go implementation of the existing TypeScript Sales Shift business behavior found in `cafe-pos/src/sales-shift`. The implementation may improve structure, schema, and correctness, but the observable business behavior must match the canonical source and the definitions in `cafe-pos/CONTEXT.md`.

When TypeScript runtime behavior conflicts with those documents, the canonical documents win. Known implementation defects are corrected rather than migrated.

The Phase 4 section of `MIGRATE_PLAN.md` was written before the canonical source was examined and describes a different subsystem: it lists a `close_shift` operation, stored `closing_cash_vnd` and `expected_cash_vnd` columns, and a three-valued movement `type` including `CASH_DROP`. None of those match the canonical source. That sketch is superseded by this specification. `MIGRATE_PLAN.md` remains a phase-status tracker and is not rewritten; this document is the design authority for Phase 4.

### Goals

1. Implement the Sales Shift as one consistency boundary holding the cashier station's cash accountability.
2. Enforce the invariant that at most one Sales Shift is `OPEN` across the whole system, with the database as the race-safe authority.
3. Record Cash Movements that require approval by a second identity holding the Manager role.
4. Compute Expected Cash dynamically rather than storing it.
5. Make every mutation atomically idempotent and every successful state change auditable.
6. Expose a REST contract whose shape remains stable when Phase 5 takes ownership of Payments.

### Non-Goals

- **Close Shift, Shift Reconciliation, and Shift Discrepancy.** `CONTEXT.md` defines these concepts, but the canonical source does not implement them, and Shift Reconciliation is defined as a comparison of calculated cash *and Manual QR activity* against counted cash and bank-observed totals. Both inputs belong to Phase 5 Payments. Designing closure before Payments exist would produce a reconciliation that must be redesigned.
- Cash Refunds as a term in the Expected Cash formula. Refunds are a Phase 5 entity.
- Post-Shift Payment Correction.
- Editing, voiding, or deleting a recorded Cash Movement. Cash Movements are append-only.
- Shift handoff between staff, shift scheduling, or calendar-day boundaries. A Sales Shift is an accountability window, not a work schedule.
- Reporting, exports, or historical shift listings.

### Accepted Consequence Of Deferring Closure

Because Phase 4 ships no Close operation, a Sales Shift that has been opened remains `OPEN` permanently, and a second Sales Shift cannot be opened afterwards. This is inherited from the canonical source, not introduced here. It is acceptable because Phase 4 is not a deployable end state: Phase 5 is the phase in which a cashier station completes a real accountability cycle. The specification records the limitation explicitly so that it is not mistaken for a defect during review.

---

## 2. Authority And Terminology

The implementation uses the terms defined in `cafe-pos/CONTEXT.md`:

- A **Sales Shift** is the continuous accountability window for the cashier station's cash fund and payment activity, opened and closed by identified staff while every intervening action retains its own actor.
- An **Opening Float** is the actual whole-VND cash counted in the cashier station's fund when identified staff open a Sales Shift.
- A **Cash Movement** is an auditable Pay In or Pay Out that changes a Sales Shift's Expected Cash without representing a Sale, Payment, or Refund.
- **Expected Cash** is the Sales Shift's calculated cash responsibility: Opening Float plus Cash Payments and Pay Ins, less Cash Refunds and Pay Outs, including valid same-Shift reversals.
- A **Staff Access Session** is the period during which one Staff Identity is authenticated on one device. It is explicitly independent of a Sales Shift: signing out does not close a Shift, and closing a Shift does not end a session.

These domain terms retain their capitalization in documentation. Go identifiers use normal exported naming.

A Sales Shift is owned by the station, not by the staff member who opened it. Any identity holding `sales_shift.operate` may record Cash Movements against the open Shift; the opener is retained as an auditable fact, not as an ownership claim.

---

## 3. Architecture

`internal/shift` owns the Sales Shift consistency boundary. It follows the structure established by `internal/tables`: one package organized by behavior, with a `Runner` that owns transaction orchestration, per-operation handler types, a `Slices` aggregate, and `RegisterRoutes(v1, authn)`.

The package contains:

- Sales Shift opening.
- Cash Movement recording.
- The current-Shift read, including Expected Cash and the Cash Movement list.
- Shared domain validation, transactional command execution, authorization, request fingerprinting, and DTO assembly.

Each handler owns a narrow, consumer-defined interface containing only the generated sqlc operations it uses. API DTOs remain separate from generated database models.

General transaction mechanics remain in `internal/database`. Shift transaction orchestration and Shift request fingerprints remain in `internal/shift`. `internal/shift` may import `internal/auth` for capability derivation and Manager approval, as `internal/catalog` and `internal/tables` already do for capability derivation. It must not import `internal/sales`, `internal/tables`, or `internal/catalog`.

### 3.1 Operations

| Operation | Capability | Manager approval | Idempotent |
| --- | --- | --- | --- |
| Read current Sales Shift | `sales_shift.operate` | No | Not applicable |
| Open Sales Shift | `sales_shift.operate` | No | Yes |
| Record Cash Movement | `sales_shift.operate` | Yes | Yes |

`sales_shift.operate` already exists in `auth.DeriveCapabilities` and is held by `MANAGER` and `CASHIER`. `BARISTA` holds neither this capability nor access to any Shift route. No change to the capability table is required.

### 3.2 Command Flow

Every mutation executes this sequence:

```text
begin transaction
-> reload current identity, session, roles, and capabilities
-> verify sales_shift.operate
-> verify Manager approval (Cash Movement only)
-> claim or replay actor-scoped request_id
-> lock the affected Sales Shift row (Cash Movement only)
-> validate current business state
-> apply mutation
-> insert authoritative Audit Event
-> store idempotent result
-> commit
```

In-transaction authority reload occurs **before** idempotent replay. An actor whose session was locked, revoked, or expired, whose identity was disabled, or whose role was removed cannot replay an earlier successful request. For Cash Movement, Manager approval is likewise re-verified before replay, so a replay cannot succeed using an approver who has since been disabled or demoted.

### 3.3 Read Flow

The current-Shift read reloads current authority and assembles its projection in a read-only, repeatable-read transaction, so that the capability check, the Shift row, the Cash Movement list, and the Expected Cash aggregate observe one database snapshot.

---

## 4. Manager Approval

Recording a Cash Movement requires two identities: an **initiator**, who is the authenticated actor, and an **approver**, who authenticates inline with a login code and PIN. This differs from the Catalog pattern, in which a price-sensitive command re-verifies the *actor's own* Manager PIN. The Cash Movement model is a genuine second-party approval and is canonical: it exists so that a cashier cannot move cash out of the drawer without a Manager present at the terminal.

A new exported primitive is added to `internal/auth`:

```go
func VerifyManagerApproval(
    ctx context.Context,
    q Querier,
    approverLoginCode string,
    managerPin string,
    requiredCapability string,
) (ApproverSummary, error)
```

Its behavior:

1. Normalize the supplied login code by trimming surrounding whitespace and upper-casing it, matching the canonical `upper(btrim(...))` lookup.
2. Load the matching Staff Identity with `SELECT ... FOR UPDATE`, so that a concurrent disablement or role change cannot interleave between verification and use.
3. Verify the PIN. **The PIN verification runs even when no identity matched**, against a dummy hash, so that response timing does not reveal whether a login code exists. `internal/auth` already uses this technique in `staff_create.go` and its siblings.
4. Reject a disabled identity.
5. Reject an identity that does not hold the `MANAGER` role.
6. Reject an identity whose derived capabilities do not include `requiredCapability`.

Self-approval is permitted: a Manager operating the terminal alone may supply their own login code and PIN. This matches the canonical source, and the Audit Event records initiator and approver separately, so a self-approved movement remains distinguishable in the audit trail.

The four canonical denial reasons (`INVALID_PIN`, `IDENTITY_DISABLED`, `MANAGER_ROLE_REQUIRED`, `CAPABILITY_REQUIRED`) all collapse to a single client-visible outcome, `MANAGER_APPROVAL_UNAVAILABLE`, so that the API does not disclose which condition failed. The specific reason is recorded in the server log and in the authorization-denial Audit Event.

This primitive is placed in `internal/auth` rather than in `internal/shift` because Phase 5 requires the identical mechanism for Refund, Payment Void, and Comp. Security-critical verification logic is not duplicated across slices. This is consistent with ADR-007: sharing an auth *primitive* is not the same as sharing an idempotency *helper*, which ADR-007 prohibits.

---

## 5. Database Design

Migration `000007_create_shift_slice.sql` creates two tables. All identities are UUIDs generated by PostgreSQL. All timestamps are `TIMESTAMPTZ`. All monetary amounts are `BIGINT`, per `MIGRATE_PLAN.md` §4.2; the canonical source stores them as `numeric`, which is a defect worth correcting during migration rather than preserving.

The shared upper bound for all Phase 4 monetary values is `2147483647` VND, matching the canonical `MAX_OPENING_FLOAT_VND` and `MAX_CASH_MOVEMENT_VND`. The bound is retained even though `BIGINT` could hold more, because it is the value the canonical business rules are written against.

### 5.1 `sales_shifts`

- `id UUID PRIMARY KEY DEFAULT gen_random_uuid()`
- `state TEXT NOT NULL DEFAULT 'OPEN'`
- `opened_by_staff_identity_id UUID NOT NULL REFERENCES staff_identities(id)`
- `opening_float_vnd BIGINT NOT NULL`
- `opened_at TIMESTAMPTZ NOT NULL DEFAULT now()`

Constraints:

- Partial unique index on `(state) WHERE state = 'OPEN'`.
- Check: `state in ('OPEN', 'CLOSED')`.
- Check: `opening_float_vnd >= 0 and opening_float_vnd <= 2147483647`.

The `CLOSED` state is present in the check constraint although no Phase 4 operation produces it. Including it now means Phase 5 adds a closure command without a schema migration on the state domain. The column is not otherwise anticipatory: no `closed_at`, `closing_cash_vnd`, or `expected_cash_vnd` column is created, because their correct shape depends on a reconciliation design that does not exist yet.

The partial unique index is the sole authority for the one-open-Shift invariant. A Go-side pre-check may produce a friendlier error, but it is advisory: two concurrent opens are resolved by the index, and the resulting `23505` is translated, not surfaced as a 500.

### 5.2 `cash_movements`

- `id UUID PRIMARY KEY DEFAULT gen_random_uuid()`
- `sales_shift_id UUID NOT NULL REFERENCES sales_shifts(id)`
- `method TEXT NOT NULL`
- `amount_vnd BIGINT NOT NULL`
- `reason TEXT NOT NULL`
- `note TEXT`
- `initiated_by_staff_identity_id UUID NOT NULL REFERENCES staff_identities(id)`
- `initiated_staff_access_session_id UUID NOT NULL REFERENCES staff_access_sessions(id)`
- `approved_by_staff_identity_id UUID NOT NULL REFERENCES staff_identities(id)`
- `occurred_at TIMESTAMPTZ NOT NULL DEFAULT now()`

Constraints:

- Index on `(sales_shift_id, occurred_at)`.
- Check: `method in ('PAY_IN', 'PAY_OUT')`.
- Check: `amount_vnd > 0 and amount_vnd <= 2147483647`.
- Check: `reason in ('ADD_CHANGE_FUND', 'REMOVE_EXCESS_FLOAT', 'SAFE_DROP', 'OTHER')`.
- Check, combining note shape and the reason-specific requirement:
  `(note is null or (note = btrim(note) and char_length(note) between 1 and 500)) and (reason <> 'OTHER' or note is not null)`.

The canonical `CASH_DROP` movement type from the `MIGRATE_PLAN.md` sketch does not exist. A safe drop is expressed as `method = 'PAY_OUT'` with `reason = 'SAFE_DROP'`, which is the canonical modelling and keeps the cash-effect sign derivable from `method` alone.

There is no column recording a reversal or correction, because reversals are a Phase 5 concept and the table is append-only in Phase 4.

### 5.3 Idempotency

Shift reuses the shared `idempotency_keys` table created in migration `000002`, in accordance with ADR-005 and ADR-007. The canonical source's two dedicated tables, `sales_shift_opening_requests` and `cash_movement_requests`, are **not** migrated.

Columns used: `actor_id`, `key` (the client `request_id`), `action`, `request_hash`, `response_code`, `response_body`, `created_at`. The primary key is `(actor_id, key)`.

Action values are:

- `shift.open_shift`
- `shift.record_cash_movement`

Action and request hash are compared during replay. This is a strict improvement over the canonical opening flow, which compares only `opening_float_vnd` and therefore accepts a replay whose other fields differ.

### 5.4 Audit Events

Shift reuses the shared `audit_events` table created in migration `000003`. Event types are:

- `SALES_SHIFT_OPENED` with details `{sales_shift_id, opening_float_vnd}`
- `CASH_MOVEMENT_RECORDED` with details `{cash_movement_id, sales_shift_id, method, amount_vnd, reason, note, initiator_staff_identity_id, approver_staff_identity_id}`
- `shift.authorization_denied` with details `{operation, reason}`, written when authority reload or Manager approval fails

Business events use the `UPPER_SNAKE_CASE` form and denial events use the lowercase dotted form, matching the convention already established by `internal/tables` (`TABLE_CREATED` alongside `tables.authorization_denied`).

Every successful state change inserts an event in the same transaction. An idempotent replay does not insert a duplicate business event. Event details must not contain PINs, session tokens, request headers, or complete request payloads.

---

## 6. Business Rules

### 6.1 Opening Float

The Opening Float is required, is an integer number of VND, and lies between `0` and `2147483647` inclusive. Zero is valid: a station may legitimately open with an empty fund.

The Opening Float is the counted cash at opening. It is never derived from a prior Shift's balance, and Phase 4 provides no mechanism to carry a balance forward.

### 6.2 The One-Open-Shift Invariant

At most one Sales Shift may be in `OPEN` state at any time, across the entire system rather than per station or per staff member. An attempt to open a second Shift is rejected with `SALES_SHIFT_ALREADY_OPEN`.

The invariant is enforced by the partial unique index. Under two truly concurrent opens, exactly one succeeds and the other receives `SALES_SHIFT_ALREADY_OPEN`; neither receives a generic 500.

### 6.3 Cash Movements

A Cash Movement is recorded against a Sales Shift that is currently `OPEN`. Recording against a Shift that does not exist, or that is not `OPEN`, is rejected with `OPEN_SALES_SHIFT_REQUIRED`. The Shift row is locked with `SELECT ... FOR UPDATE` before the movement is inserted, so that a concurrent state change cannot be missed.

The amount is a strictly positive integer number of VND, at most `2147483647`. Direction is carried by `method`, never by a negative amount.

A note is optional in general, is trimmed, and is between 1 and 500 characters when present. When `reason` is `OTHER`, a note is mandatory: an unexplained "other" movement is precisely the shrinkage the control exists to prevent. Both the Go validator and the database check enforce this, and the database check is authoritative.

Cash Movements are append-only. Phase 4 provides no operation to edit, reverse, or delete one.

### 6.4 Expected Cash

Expected Cash is calculated on read, never stored. The Phase 4 formula is:

```text
expected_cash_vnd = opening_float_vnd
                  + sum(amount_vnd where method = 'PAY_IN')
                  - sum(amount_vnd where method = 'PAY_OUT')
```

The canonical formula additionally adds the applied amount of every `CASH` Payment belonging to the Shift, and `CONTEXT.md` further subtracts Cash Refunds. Both depend on the `payments` table, which depends on `checks`, which depends on `service_sessions` and order drafts. Provisioning that chain in Phase 4 would replicate the most intricate constraint set in the system a phase early. Phase 4 therefore ships the partial formula, and Phase 5 adds the remaining terms to a single query.

The API field is present and correctly named from Phase 4 onward, so the response shape does not change when the formula completes. This trade-off is recorded as ADR-008. Until Phase 5 lands, `expected_cash_vnd` reflects fund movements only and must not be presented to staff as a reconciliation figure.

The aggregate is computed by SQL `SUM` over `BIGINT`. A result outside the inclusive range `[-2147483647, 2147483647]` is reported as `EXPECTED_CASH_OUT_OF_RANGE`, preserving the canonical error code. The canonical source raises this code to guard JavaScript's safe-integer limit; in Go the guard is a domain bound rather than a numeric-representation bound, and it is retained because a figure outside that range indicates corrupt data rather than a legitimate drawer balance. The bound is symmetric because sustained Pay Outs can legitimately drive the partial Phase 4 figure negative.

### 6.5 The Current Read

The current read returns the single `OPEN` Sales Shift together with its opener summary, its Expected Cash, and its Cash Movements. When no Shift is open it returns a successful response whose `data` is `null`, not a 404: "no Shift is currently open" is a normal operating state that the cashier screen renders directly.

Cash Movements are ordered by `occurred_at` descending with `id` descending as a deterministic tie-breaker, matching the canonical ordering and making the list stable for movements recorded within the same transaction.

Each staff summary — opener, initiator, approver — carries exactly three fields: identity, display name, and login code. No PIN hash, role list, enablement flag, or session detail crosses the Shift boundary.

---

## 7. Authorization

Routes require an authenticated, active Staff Access Session. Echo middleware rejects obviously unauthenticated or under-privileged requests, but business handlers reload current identity, session, roles, and capabilities inside their transaction.

| Operation | Required capability | Second-party approval |
| --- | --- | --- |
| Read current Sales Shift | `sales_shift.operate` | None |
| Open Sales Shift | `sales_shift.operate` | None |
| Record Cash Movement | `sales_shift.operate` | Enabled `MANAGER` holding `sales_shift.operate` |

The resulting role behavior is canonical and intentional:

- `MANAGER` may read, open, and record Cash Movements, and may approve them.
- `CASHIER` may read, open, and initiate a Cash Movement, but cannot approve one.
- `BARISTA` holds neither capability and is denied all three routes.

---

## 8. REST API

Routes are registered on a `/shifts` Echo group mounted on the existing `/api/v1` group, and use the existing `{success,data,error}` response envelope.

| Method and path | Capability | Success status |
| --- | --- | --- |
| `GET /api/v1/shifts/current` | `sales_shift.operate` | 200 |
| `POST /api/v1/shifts` | `sales_shift.operate` | 201 |
| `POST /api/v1/shifts/:shift_id/cash-movements` | `sales_shift.operate` + Manager approval | 201 |

Request bodies:

- `POST /api/v1/shifts`: `{request_id, opening_float_vnd}`
- `POST /api/v1/shifts/:shift_id/cash-movements`: `{request_id, method, amount_vnd, reason, note?, approver_login_code, manager_pin}`

The Sales Shift identifier is present in the route and is not duplicated in the body, matching the Phase 3 convention. The canonical source passes `salesShiftId` in the payload; the route parameter is the equivalent binding and is checked against the locked Shift row identically.

### 8.1 Secret Handling

`manager_pin` is removed from the command before the request fingerprint is computed, matching the canonical `fingerprint()` function. Including it would make the idempotency key sensitive to a secret and would store a PIN-derived value at rest.

`manager_pin` must never appear in an Audit Event, a structured log field, an error message, or a response body. `approver_login_code` is not a secret and is retained in the audit trail through `approver_staff_identity_id`.

### 8.2 Response Contracts

`POST /api/v1/shifts` returns the opened Shift:

```json
{
  "id": "uuid",
  "state": "OPEN",
  "opening_float_vnd": 500000,
  "opened_at": "2026-09-13T01:00:00Z",
  "opener": { "id": "uuid", "display_name": "Quản lý A", "login_code": "QLA" }
}
```

`GET /api/v1/shifts/current` returns that object extended with `expected_cash_vnd` and `cash_movements`, or `null`:

```json
{
  "id": "uuid",
  "state": "OPEN",
  "opening_float_vnd": 500000,
  "opened_at": "2026-09-13T01:00:00Z",
  "opener": { "id": "uuid", "display_name": "Quản lý A", "login_code": "QLA" },
  "expected_cash_vnd": 450000,
  "cash_movements": [
    {
      "id": "uuid",
      "sales_shift_id": "uuid",
      "method": "PAY_OUT",
      "amount_vnd": 50000,
      "reason": "SAFE_DROP",
      "note": null,
      "initiator": { "id": "uuid", "display_name": "Thu ngân B", "login_code": "TNB" },
      "approver": { "id": "uuid", "display_name": "Quản lý A", "login_code": "QLA" },
      "occurred_at": "2026-09-13T03:00:00Z"
    }
  ]
}
```

`cash_movements` is always an array and is serialized as `[]` when empty, never as `null`.

`POST /api/v1/shifts/:shift_id/cash-movements` returns the recorded movement together with the resulting Expected Cash, so that the terminal updates its drawer figure without a second request:

```json
{
  "movement": { "...": "as above" },
  "expected_cash_vnd": 450000
}
```

---

## 9. Transactions And Concurrency

The idempotency sequence is:

1. Reload and authorize the current actor.
2. Verify Manager approval, for Cash Movement.
3. Claim `(actor_id, request_id)`.
4. Compare action and normalized-input hash.
5. Replay the stored status and body for an exact match.
6. Return `REQUEST_CONFLICT` for a different reuse of the same key.
7. For a new request, mutate, audit, and store the result atomically.

Failed operations are not cached. Concurrent identical requests cannot execute the mutation twice.

Cash Movement locks its Sales Shift row with `SELECT ... FOR UPDATE` before validating state and inserting. Opening has no row to lock; the partial unique index is the authority for concurrent opens.

Friendly pre-checks may provide domain-specific errors, but PostgreSQL constraints remain authoritative for races.

The current read uses a read-only repeatable-read transaction so that the capability check, the Shift row, the movement list, and the Expected Cash aggregate observe one snapshot.

---

## 10. Errors

Handlers translate expected failures into stable domain codes and the existing HTTP envelope:

| Domain error | Code | HTTP |
| --- | --- | --- |
| `ErrShiftAlreadyOpen` | `SALES_SHIFT_ALREADY_OPEN` | 409 |
| `ErrOpenShiftRequired` | `OPEN_SALES_SHIFT_REQUIRED` | 409 |
| `ErrManagerApprovalUnavailable` | `MANAGER_APPROVAL_UNAVAILABLE` | 403 |
| `ErrRequestConflict` | `REQUEST_CONFLICT` | 409 |
| `ErrExpectedCashOutOfRange` | `EXPECTED_CASH_OUT_OF_RANGE` | 400 |
| `ErrForbidden` | `FORBIDDEN` | 403 |
| `ErrUnauthorized` | `UNAUTHORIZED` | 401 |
| `ErrInvalidStoredResult` | `INVALID_STORED_RESULT` | 500 with a generic client message and detailed server log |
| `response.ErrInvalid` | `INVALID_INPUT` | 400 |

PostgreSQL `23505` unique violations on the partial open-Shift index map to `SALES_SHIFT_ALREADY_OPEN`, and `23503` foreign-key violations on `sales_shift_id` map to `OPEN_SALES_SHIFT_REQUIRED`, inside the Shift boundary. They must not become accidental generic 500 responses. A `23514` check violation indicates that Go validation and the database disagree; it is a defect, is logged with the constraint name, and surfaces as a genuine 500.

The TypeScript codes `OPEN_FAILED` and `CASH_MOVEMENT_FAILED` are not migrated, for the reason established in Phase 3: a `RETURNING` clause yielding no row after a successful existence check and row lock is a programming defect, not a business state.

The canonical code `CASH_MOVEMENT_REQUEST_CONFLICT` is folded into `REQUEST_CONFLICT`. The two describe the same condition, and the shared `idempotency_keys` table makes a per-operation variant unnecessary. The `action` column already distinguishes which operation conflicted.

Validation messages use JSON field names.

---

## 11. Audit And Notifications

The Audit Event row is part of the business transaction. Audit insertion failure rolls back the Shift mutation and the idempotency claim.

Events contain the actor, the access session, the operation-specific target, and the relevant facts. Secrets are prohibited.

Watermill may publish optional post-commit notifications. The in-memory bus is not authoritative, and failure to publish a secondary notification does not reverse committed Shift state.

---

## 12. Testing

### 12.1 Unit Tests

- Opening Float validation at the `0` and `2147483647` boundaries, and rejection of negative and over-bound values.
- Cash Movement amount validation: rejection of zero, of negatives, and of over-bound values.
- The `reason = 'OTHER'` note requirement, including a whitespace-only note, which trims to empty and must be rejected.
- Note trimming and the 1 and 500 character boundaries.
- Request fingerprint stability, and proof that `manager_pin` does not affect the fingerprint while every other field does.
- Expected Cash arithmetic, including the symmetric out-of-range guard.
- Cash Movement ordering, including the `id` tie-breaker for identical `occurred_at`.
- Domain error to HTTP code mapping.
- `auth.VerifyManagerApproval` login-code normalization and denial-reason selection.

### 12.2 PostgreSQL Integration Tests

Derived from the canonical scenarios in `sales-shift.integration.test.ts`, `cash-movements.integration.test.ts`, and `sales-shift-authorization.integration.test.ts`:

1. A Cashier opens a Sales Shift with a counted float; the current read returns it with Expected Cash equal to the float and an empty movement list.
2. A second open attempt is rejected with `SALES_SHIFT_ALREADY_OPEN`, and truly concurrent opens yield exactly one success.
3. A Pay In and a Pay Out adjust Expected Cash in the correct directions and accumulate correctly over several movements.
4. A Cash Movement against a non-existent or non-`OPEN` Shift is rejected with `OPEN_SALES_SHIFT_REQUIRED`.
5. Manager approval succeeds for an enabled Manager, including self-approval, and fails identically for a wrong PIN, an unknown login code, a disabled identity, and a Cashier-only approver.
6. Authority is reloaded, so an actor stripped of `sales_shift.operate` mid-session is denied and nothing changes.
7. Idempotency: exact replay returns the stored result; the same `request_id` with a different payload returns `REQUEST_CONFLICT`; concurrent duplicate requests execute the mutation once.
8. Replay is denied after the initiator's identity is disabled or session revoked, and after the approver is disabled or demoted.
9. Audit failure rolls back the movement and the idempotency claim.
10. The current read observes one snapshot across the Shift row, the movement list, and the aggregate.

Additional coverage:

- Migration and database constraints: the partial unique open index, the monetary bounds, the `method` and `reason` domains, and the combined note check.
- Confirmation that `manager_pin` appears in no `audit_events` row and no `idempotency_keys` row.

Integration packages run with `-p 1`.

### 12.3 HTTP Tests

- Authentication and route-to-capability mapping for all three operations, including the `BARISTA` denial on every route and the `CASHIER` approval denial.
- UUID parameter and request-body validation.
- Stable response envelopes, statuses, and error codes.
- `GET /api/v1/shifts/current` returning `data: null` with status 200 when no Shift is open.
- `cash_movements` serialized as `[]` rather than `null`.
- Staff summary objects containing exactly the three permitted fields.
- Swagger annotations covering all three operations with Bearer security.

---

## 13. Decision Record Updates

`spec/decisions.md`: two records are added.

- **ADR-008** — Partial Expected Cash in Phase 4. Context: the canonical formula requires `payments`, which transitively requires `checks`, `service_sessions`, and order drafts, all owned by Phase 5. Decision: Phase 4 computes Opening Float plus Pay Ins less Pay Outs, exposes `expected_cash_vnd` in its final API shape, and Phase 5 adds the Cash Payment and Cash Refund terms to one query. Consequence: the response contract is stable from Phase 4 onward, at the cost of a figure that is incomplete until Phase 5 lands; the specification and the Swagger description both state this.
- **ADR-009** — `auth.VerifyManagerApproval` as a shared second-party approval primitive. Context: Cash Movement requires approval by a second identity holding the Manager role, and Phase 5 requires the same mechanism for Refund, Payment Void, and Comp. Decision: the verification lives in `internal/auth` and is reused; slices supply only the required capability. Consequence: security-critical verification is implemented once; this is an approved exception to the general rule that slices do not share helpers, and it does not extend to idempotency helpers, which ADR-007 keeps slice-local.

`MIGRATE_PLAN.md` is a phase-status tracker, not a design document. This specification supersedes its Phase 4 sketch, but the roadmap file is not rewritten to match; only its Phase 4 status and tracker row are marked complete when the phase lands.

---

## 14. Acceptance Criteria

1. `internal/shift` exposes exactly one read and two commands. No close, no shift listing, no movement edit or delete.
2. The `sales_shifts` schema matches the canonical source: no stored `expected_cash_vnd`, no `closing_cash_vnd`, no `closed_at`.
3. At most one Sales Shift is `OPEN`, enforced by a partial unique index that holds under concurrency.
4. Cash Movement uses `method` and `reason` as two separate domains; `CASH_DROP` does not exist as a method.
5. All monetary columns are `BIGINT` and bounded at `2147483647`.
6. Cash Movement requires an enabled `MANAGER` approver verified by login code and PIN through `auth.VerifyManagerApproval`; all denial reasons collapse to `MANAGER_APPROVAL_UNAVAILABLE` for the client.
7. `manager_pin` appears in no fingerprint, audit row, log field, or response.
8. Every mutation is actor-scoped and idempotent against the shared `idempotency_keys` table. No new idempotency table is created.
9. Every successful state change writes exactly one Audit Event in the same transaction. Replays write none.
10. Current authority and Manager approval are re-evaluated inside the transaction and before idempotent replay.
11. Expected Cash is computed on read, never stored, and is guarded symmetrically against out-of-range totals.
12. `GET /api/v1/shifts/current` returns 200 with `data: null` when no Shift is open, and serializes an empty movement list as `[]`.
13. Expected database constraint failures map to stable API errors, with no accidental generic 500 responses.
14. Unit, PostgreSQL integration, HTTP, and concurrency tests pass. Existing Auth, Catalog, and Tables suites remain passing.
15. Swagger documentation reflects all three Shift operations, and the `expected_cash_vnd` description states that Cash Payments are added in Phase 5.
16. `spec/decisions.md` records ADR-008 and ADR-009. `MIGRATE_PLAN.md` has its Phase 4 status and tracker row marked complete, with no rewrite of its Phase 4 detail.
