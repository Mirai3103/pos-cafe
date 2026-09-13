# Design Specification: Tables Slice (`internal/tables`)

- **Author:** Claude Opus 5 & Team
- **Date:** 2026-09-12
- **Status:** Approved
- **Phase:** Phase 3, Tables & Floor Layout

---

## 1. Purpose

This specification defines the Go implementation of the existing TypeScript Tables business behavior found in `cafe-pos/src/tables`. The implementation may improve structure and correctness, but it must preserve the canonical business rules in `cafe-pos/CONTEXT.md`.

When TypeScript runtime behavior conflicts with those documents, the canonical documents win. Known implementation defects are corrected rather than migrated.

The Phase 3 section of `MIGRATE_PLAN.md` was written before the canonical source was examined and describes a different entity (`capacity`, `display_order`, `is_active`, no occupancy). That sketch is superseded by this specification. `MIGRATE_PLAN.md` remains a phase-status tracker and is not rewritten; this document is the design authority for Phase 3.

### Goals

1. Implement the Table entity, its naming rules, and its Availability lifecycle as one consistency boundary.
2. Provide a single overview read that reports every Table together with the active Service Sessions currently occupying it.
3. Recheck current authority inside each operation transaction.
4. Make every mutation atomically idempotent and every successful state change auditable.
5. Expose a REST contract that remains stable when Phase 5 takes ownership of Sales.

### Non-Goals

- Table capacity, display order, floor coordinates, or any layout geometry.
- Destructive deletion of a Table.
- Retirement as a distinct lifecycle concept. Tables have Availability only.
- Assigning, releasing, merging, or splitting Table occupancy. Those are Phase 5 Sales commands.
- Reservations or booking.
- Fresh Manager PIN verification. No Tables command is price-sensitive.
- Reliable external event delivery.

---

## 2. Authority And Terminology

The implementation uses the terms defined in `cafe-pos/CONTEXT.md`:

- A **Table** is a named physical service location that may be associated with one or more active Service Sessions. It is not an order owner and not an exclusive tab.
- A **Service Session** is the continuous period in which one customer party is served, containing its Orders and current Table assignments until staff explicitly close it.
- A **Service Number** is a short operational label for one Anonymous Service Session, used to identify takeaway handoff and shown alongside current Table assignments for dine-in without identifying the customer.
- **Availability** is temporary eligibility for new work.

The word `Availability` retains its domain capitalization in documentation. Go identifiers use normal exported naming.

Because a Table may host more than one active Service Session, shared occupancy (two parties seated at one large Table) is valid domain behavior, not an error state.

---

## 3. Architecture

`internal/tables` owns the Table consistency boundary. It follows the structure established by `internal/catalog`: one package organized by behavior, with a `Runner` that owns transaction orchestration, per-operation handler types, a `Slices` aggregate, and `RegisterRoutes(v1, authn)`.

The package contains:

- Table creation, rename, and Availability.
- The overview read, including current occupancy.
- Shared domain validation, transactional command execution, authorization, request fingerprinting, and DTO assembly.

Each handler owns a narrow, consumer-defined interface containing only the generated sqlc operations it uses. API DTOs remain separate from generated database models.

General transaction mechanics remain in `internal/database`. Tables transaction orchestration and Tables request fingerprints remain in `internal/tables`. `internal/tables` may import `internal/auth` for capability derivation, as `internal/catalog` already does, but must not import an auth-owned idempotency helper and must not import `internal/sales`.

### 3.1 Operations

| Operation | Capability | Idempotent |
| --- | --- | --- |
| Overview read | `sales.operate` | Not applicable |
| Create Table | `tables.administer` | Yes |
| Rename Table | `tables.administer` | Yes |
| Set Table Availability | `tables.administer` | Yes |

### 3.2 Command Flow

Every mutation executes this sequence:

```text
begin transaction
-> reload current identity, session, roles, and capabilities
-> verify tables.administer
-> claim or replay actor-scoped request_id
-> lock the affected Table row
-> validate current business state
-> apply mutation
-> insert authoritative Audit Event
-> store idempotent result
-> commit
```

No Tables command verifies a Manager PIN.

### 3.3 Read Flow

The overview read reloads current authority and assembles its projection in a read-only, repeatable-read transaction so that the Table list and the occupancy join observe one database snapshot. Two focused queries and deterministic Go assembly are acceptable.

---

## 4. Database Design

Migration `000006_create_tables_slice.sql` creates three tables. All identities are UUIDs generated by PostgreSQL. All timestamps are `TIMESTAMPTZ`.

### 4.1 `tables`

- `id UUID PRIMARY KEY DEFAULT gen_random_uuid()`
- `name TEXT NOT NULL`
- `normalized_name TEXT NOT NULL`
- `available BOOLEAN NOT NULL DEFAULT true`
- `created_at TIMESTAMPTZ NOT NULL DEFAULT now()`
- `updated_at TIMESTAMPTZ NOT NULL DEFAULT now()`

Constraints:

- Unique index on `normalized_name`.
- Check: `char_length(name) between 1 and 60 and name = btrim(name)`.
- Check: `normalized_name = lower(name)`.

Tables have no capacity, display order, description, Retirement, or destructive delete operation.

### 4.2 `service_sessions`

This table is provisioned early so that Phase 3 can implement and test the canonical overview read. Business ownership belongs to `internal/sales` in Phase 5. The migration carries a SQL comment stating this.

- `id UUID PRIMARY KEY DEFAULT gen_random_uuid()`
- `service_number TEXT NOT NULL`, unique, check `service_number ~ '^[A-Z0-9]{6}$'`
- `service_mode TEXT NOT NULL DEFAULT 'TAKEAWAY'`, check in (`DINE_IN`, `TAKEAWAY`)
- `state TEXT NOT NULL DEFAULT 'ACTIVE'`, check in (`ACTIVE`, `COMPLETED`, `CANCELLED`)
- `created_by_staff_identity_id UUID NOT NULL REFERENCES staff_identities(id)`
- `created_at TIMESTAMPTZ NOT NULL DEFAULT now()`

The canonical `sales_shift_id UUID NOT NULL REFERENCES sales_shifts(id)` column is deliberately omitted because `sales_shifts` is a Phase 4 table. Phase 5 adds it with `ALTER TABLE`. No other canonical column is omitted, so Phase 5 performs one additive alteration rather than a sequence of backfills.

### 4.3 `table_assignments`

Provisioned early on the same terms as `service_sessions`, with the same ownership comment. All canonical columns are present, because omitting any of them would only defer an equivalent `ALTER TABLE`.

- `id UUID PRIMARY KEY DEFAULT gen_random_uuid()`
- `table_id UUID NOT NULL REFERENCES tables(id)`
- `service_session_id UUID NOT NULL REFERENCES service_sessions(id)`
- `assigned_at TIMESTAMPTZ NOT NULL DEFAULT now()`
- `assigned_by_staff_identity_id UUID NOT NULL REFERENCES staff_identities(id)`
- `sequence INTEGER NOT NULL`
- `released_at TIMESTAMPTZ`
- `released_by_staff_identity_id UUID REFERENCES staff_identities(id)`

Constraints:

- Partial unique index on `(table_id, service_session_id) WHERE released_at IS NULL`.
- Index on `table_id`; index on `service_session_id`.
- Check: `released_at` and `released_by_staff_identity_id` are both null or both non-null.

Phase 3 writes no rows to `service_sessions` or `table_assignments` through its API. Integration tests seed them directly to exercise the overview read.

### 4.4 Idempotency

Tables reuses the shared `idempotency_keys` table created in migration `000002`, in accordance with ADR-005. No new idempotency table is created.

Columns used: `actor_id`, `key` (the client `request_id`), `action`, `request_hash`, `response_code`, `response_body`, `created_at`. The primary key is `(actor_id, key)`.

Action values are:

- `tables.create_table`
- `tables.rename_table`
- `tables.set_table_availability`

Action and request hash are compared during replay.

### 4.5 Audit Events

Tables reuses the shared `audit_events` table created in migration `000003`. Event types are:

- `TABLE_CREATED` with details `{table_id, name}`
- `TABLE_RENAMED` with details `{table_id, before_name, after_name}`
- `TABLE_AVAILABILITY_CHANGED` with details `{table_id, before_available, after_available}`

Every successful Table state change inserts an event in the same transaction. An idempotent replay and a successful same-state Availability no-op do not insert a duplicate business event. Event details must not contain PINs, session tokens, request headers, or complete request payloads.

### 4.6 Name Normalization

Table name normalization differs from Catalog normalization, and this difference is deliberate.

1. Trim surrounding whitespace.
2. Collapse each run of internal whitespace to a single space. Catalog preserves internal whitespace; Tables does not, because `"Bàn  1"` and `"Bàn 1"` name the same physical location.
3. The uniqueness key is the Unicode lowercase form of the result.

Normalization occurs in Go before persistence. The unique index on `normalized_name` is the race-safe authority. The `normalized_name = lower(name)` check keeps the two columns consistent; because `name` has already been trimmed and collapsed in Go, the database check and the Go normalization agree.

Go uses `strings.ToLower`, which performs Unicode lowercasing. The TypeScript source uses `toLocaleLowerCase('vi-VN')`; for Vietnamese text the two produce identical results, and the database check uses plain `lower()`, so `strings.ToLower` is the correct choice for agreement with the authoritative index.

---

## 5. Business Rules

### 5.1 Naming

A Table name is required. After normalization it must be between 1 and 60 characters, counted as Unicode code points rather than bytes, so that Go agrees with the database `char_length` check on Vietnamese text. Normalized names are globally unique. A rename to a name already used by another Table is rejected.

Renaming preserves Table identity, its Availability, its creation timestamp, and all existing assignment history.

### 5.2 Availability

`available` expresses whether a Table is eligible to receive new work. It is not a statement about whether the Table is currently occupied.

Setting Availability to its current value is a successful no-op: it returns current state, does not update `updated_at`, and does not emit another business Audit Event. This matches the Catalog rule for the same concept.

A no-op is nonetheless a successful response, so it stores its idempotency result like any other successful command. The distinction is that it writes no Audit Event and no row change, not that it skips the idempotency record.

Availability and occupancy are independent, and no rule couples them:

- A Table may be made unavailable while active Service Sessions are still seated at it. Those sessions continue normally.
- A Table may be available and simultaneously occupied, because a Table accepts more than one Service Session.

Making a Table unavailable never deletes it, never releases assignments, and never alters historical records.

### 5.3 Occupancy

A Table's current occupants are the Service Sessions for which an assignment row exists with `released_at IS NULL` and whose session `state` is `ACTIVE`. A released assignment or a session in `COMPLETED` or `CANCELLED` state is not an occupant.

Each occupant is reported as exactly two fields: the Service Session identity and its Service Number. No other Sales detail is exposed through the Tables boundary.

A Table with no current occupants reports an empty list, never a null.

### 5.4 Lifecycle

Tables have no Retirement and no delete. A Table that is no longer in use is made unavailable. Its identity and history are retained permanently.

---

## 6. Authorization

Routes require an authenticated, active Staff Access Session. Echo middleware rejects obviously unauthenticated or under-privileged requests, but business handlers reload current identity, session, roles, and capabilities inside their transaction.

Capability mapping:

| Operation | Required capability |
| --- | --- |
| Overview read | `sales.operate` |
| Create Table | `tables.administer` |
| Rename Table | `tables.administer` |
| Set Table Availability | `tables.administer` |

Both capabilities already exist in `auth.DeriveCapabilities`. No change to the Auth slice is required.

The resulting role behavior is canonical and intentional:

- `MANAGER` holds both capabilities and may read and administer.
- `CASHIER` holds `sales.operate` only: it reads the overview but cannot create, rename, or change Availability.
- `BARISTA` holds neither: it cannot read the Table overview.

In-transaction authority reload occurs **before** idempotent replay. An actor whose session was locked, revoked, or expired, whose identity was disabled, or whose role was removed cannot replay an earlier successful request.

---

## 7. REST API

Routes are registered on a `/tables` Echo group mounted on the existing `/api/v1` group, and use the existing `{success,data,error}` response envelope. Full paths are:

| Method and path | Capability | Success status |
| --- | --- | --- |
| `GET /api/v1/tables/overview` | `sales.operate` | 200 |
| `POST /api/v1/tables` | `tables.administer` | 201 |
| `PATCH /api/v1/tables/:table_id/name` | `tables.administer` | 200 |
| `PATCH /api/v1/tables/:table_id/availability` | `tables.administer` | 200 |

Request bodies:

- `POST /api/v1/tables`: `{request_id, name}`
- `PATCH /api/v1/tables/:table_id/name`: `{request_id, name}`
- `PATCH /api/v1/tables/:table_id/availability`: `{request_id, available}`

Every mutation body contains `request_id`. The Table identifier is present in the route and is not duplicated in the body.

### 7.1 Response Contracts

All three commands return the same Table representation:

```json
{ "id": "uuid", "name": "Bàn 1", "available": true }
```

The overview returns an array:

```json
[
  {
    "id": "uuid",
    "name": "Bàn ghép",
    "available": true,
    "current_service_sessions": [
      { "service_session_id": "uuid", "service_number": "A1B2C3" }
    ]
  }
]
```

`current_service_sessions` is always an array and is serialized as `[]` when empty, never as `null`. Each occupant object carries exactly two fields. This contract is complete as of Phase 3 and does not change when Phase 5 assumes ownership of Sales.

### 7.2 Ordering

Tables are ordered by `created_at` ascending, with `id` ascending as a deterministic tie-breaker. The TypeScript source orders by `created_at` alone, which is non-deterministic for Tables created within the same transaction; the tie-breaker corrects this.

Occupants within a Table are ordered by `assigned_at` ascending, with `id` ascending as a tie-breaker, matching the canonical source.

---

## 8. Transactions And Concurrency

Mutation transactions lock the affected Table row with `SELECT ... FOR UPDATE` before validating and applying changes. Creation has no row to lock; the unique index on `normalized_name` is the authority for concurrent creation of the same name.

The idempotency sequence is:

1. Reload and authorize the current actor.
2. Claim `(actor_id, request_id)`.
3. Compare action and normalized-input hash.
4. Replay the stored status and body for an exact match.
5. Return `REQUEST_CONFLICT` for a different reuse of the same key.
6. For a new request, mutate, audit, and store the result atomically.

Failed operations are not cached. Concurrent identical requests cannot execute the mutation twice.

Friendly uniqueness pre-checks may provide domain-specific errors, but PostgreSQL unique constraints remain authoritative for races.

The overview read uses a read-only repeatable-read transaction so that the capability check, the Table list, and the occupancy join observe one snapshot.

---

## 9. Errors

Handlers translate expected failures into stable domain codes and the existing HTTP envelope:

| Domain error | Code | HTTP |
| --- | --- | --- |
| `ErrTableNotFound` | `TABLE_NOT_FOUND` | 404 |
| `ErrNameConflict` | `TABLE_NAME_CONFLICT` | 409 |
| `ErrRequestConflict` | `REQUEST_CONFLICT` | 409 |
| `ErrForbidden` | `FORBIDDEN` | 403 |
| `ErrUnauthorized` | `UNAUTHORIZED` | 401 |
| `ErrInvalidStoredResult` | `INVALID_STORED_RESULT` | 500 with a generic client message and detailed server log |
| `response.ErrInvalid` | `INVALID_INPUT` | 400 |

PostgreSQL `23505` unique violations map to `TABLE_NAME_CONFLICT` and `23503` foreign-key violations map to `TABLE_NOT_FOUND`, inside the Tables boundary. They must not become accidental generic 500 responses.

The TypeScript codes `CREATE_FAILED`, `RENAME_FAILED`, and `AVAILABILITY_CHANGE_FAILED` are not migrated. In Go, a `RETURNING` clause yielding no row after a successful existence check and row lock is a programming defect, not a business state. Such a condition surfaces as a genuine 500 with a logged error rather than as a named business code.

Validation messages use JSON field names.

---

## 10. Audit And Notifications

The Audit Event row is part of the business transaction. Audit insertion failure rolls back the Table mutation and the idempotency claim.

Events contain the actor, the access session, the operation-specific target, and before/after facts. Secrets are prohibited.

Watermill may publish optional post-commit notifications. The in-memory bus is not authoritative, and failure to publish a secondary notification does not reverse committed Table state.

---

## 11. Testing

### 11.1 Unit Tests

- Name normalization: trimming, internal whitespace collapsing, Unicode lowercasing of Vietnamese text.
- Name length validation at the 1 and 60 boundaries, before and after normalization.
- Request fingerprint normalization and stability.
- Domain error to HTTP code mapping.
- Occupancy assembly from query rows, including empty and shared cases.

### 11.2 PostgreSQL Integration Tests

Derived from the six canonical scenarios in `tables.integration.test.ts`:

1. A Manager creates a Table with a required name; authorized sales staff read it; unauthorized staff cannot mutate it.
2. Renaming preserves identity and history; a conflicting name is rejected; a missing Table is rejected.
3. A Manager makes a Table unavailable and later restores it without deleting the record; a same-state request is a no-op that writes no Audit Event and does not change `updated_at`.
4. Authority is reloaded, so a Manager stripped of the role mid-session is denied and nothing changes.
5. Maintenance is idempotent and transactional: exact replay returns the stored result; the same `request_id` with a different payload returns `REQUEST_CONFLICT`; truly concurrent duplicate requests execute the mutation once; concurrent creation of the same normalized name yields exactly one success.
6. Current Table reads identify every active Service Session assigned to a Table by Service Number only, including shared occupancy; released assignments and non-`ACTIVE` sessions are excluded; an unoccupied Table returns an empty list.

Additional coverage:

- Migration and database constraints, including the Retirement-free lifecycle and the partial unique assignment index.
- Rollback on audit failure.
- Replay denial after identity disablement or session revocation.
- Consistent multi-query snapshots for the overview.

Integration packages run with `-p 1`.

### 11.3 HTTP Tests

- Authentication and route-to-capability mapping for all four operations, including the `BARISTA` overview denial and the `CASHIER` mutation denial.
- UUID parameter and request-body validation.
- Stable response envelopes, statuses, and error codes.
- `current_service_sessions` serialized as `[]` rather than `null`.
- Occupant objects containing exactly the two permitted fields.
- Swagger annotations covering all four operations with Bearer security.

---

## 12. Decision Record Updates

`MIGRATE_PLAN.md` is a phase-status tracker, not a design document. This specification supersedes its Phase 3 sketch, but the roadmap file is not rewritten to match; only its Phase 3 status and tracker row are marked complete when the phase lands. Design detail lives here.

`spec/decisions.md`: two records are added.

- **ADR-006** — Early provisioning of `service_sessions` and `table_assignments` in Phase 3, with `sales_shift_id` deferred to a Phase 5 `ALTER TABLE`. Context: the canonical Table overview reports active Service Sessions, so Phase 3 cannot deliver its canonical read without these tables. Consequence: the public Tables API contract is complete and stable from Phase 3 onward, at the cost of one additive alteration in Phase 5.
- **ADR-007** — Reaffirmation of the shared `idempotency_keys` table from ADR-005 for all subsequent slices. Context: Phase 2 introduced `catalog_mutation_requests`, contradicting ADR-005. Decision: Tables and later slices use `idempotency_keys`; the Catalog table is recorded as a historical exception rather than a precedent.

---

## 13. Acceptance Criteria

1. `internal/tables` exposes exactly one read and three commands. No generic CRUD, no delete, no Retirement.
2. The `tables` schema matches the canonical source: no capacity, no display order, and `available` rather than `is_active`.
3. `service_sessions` and `table_assignments` are created with SQL comments recording Phase 5 ownership, and `service_sessions` omits only `sales_shift_id`.
4. Every mutation is actor-scoped and idempotent against the shared `idempotency_keys` table. No new idempotency table is created.
5. Every successful state change writes exactly one Audit Event in the same transaction. Replays and same-state no-ops write none.
6. Current authority is reloaded inside the transaction and evaluated before idempotent replay.
7. The overview read reports complete occupancy, exposes exactly two fields per occupant, and serializes an empty occupancy as `[]`.
8. Table name normalization trims and collapses internal whitespace, and the unique index on `normalized_name` is the authority under concurrency.
9. Availability is independent of occupancy, and no rule couples them.
10. Expected database constraint failures map to stable API errors, with no accidental generic 500 responses.
11. Unit, PostgreSQL integration, HTTP, and concurrency tests pass. Existing Auth and Catalog suites remain passing.
12. Swagger documentation reflects all four Tables operations.
13. `spec/decisions.md` records ADR-006 and ADR-007. `MIGRATE_PLAN.md` has its Phase 3 status and tracker row marked complete, with no rewrite of its Phase 3 detail.
