# Design Specification: Sales Session & Order Draft Slice (`internal/sales`, Phase 5A)

- **Author:** Claude Opus 5 & Team
- **Date:** 2026-09-13
- **Status:** Draft
- **Phase:** Phase 5A, the first of four sub-phases of Phase 5 (Core Sales, Orders & Payments)

---

## 1. Purpose

This specification defines the Go implementation of the Service Session and Order Draft portion of the existing TypeScript Sales behavior found in `cafe-pos/src/sales`. The implementation may improve structure, schema, and correctness, but the observable business behavior must match the canonical source and the definitions in `cafe-pos/CONTEXT.md`.

When TypeScript runtime behavior conflicts with those documents, the canonical documents win. Known implementation defects are corrected rather than migrated.

The Phase 5 section of `MIGRATE_PLAN.md` was written before the canonical source was examined and describes a different subsystem. It lists `order_rounds`, a `checks` table carrying `discount_vnd` and `tax_vnd`, and an `order_items.status` domain of `PENDING/PREPARING/READY/SERVED/CANCELLED`. None of those exist in the canonical source. More importantly, the sketch misses the central structural fact of the canonical model: **Commit and Submit are two distinct boundaries**, and Payment happens between them. That sketch is superseded by this specification and its three successors. `MIGRATE_PLAN.md` remains a phase-status tracker and is not rewritten.

### Goals

1. Transfer business ownership of `service_sessions` and `table_assignments` from their Phase 3 provisioning to `internal/sales`, and complete `service_sessions` with its `sales_shift_id` column.
2. Implement the Service Session lifecycle up to, but not including, its commercial boundary: opening a Takeaway or Dine-in Session, and maintaining its Table assignments.
3. Implement the Order Draft as a mutable, deliberately incomplete proposal, including composition-keyed merging of draft items.
4. Resolve effective Modifier Groups for a Menu Item without importing `internal/catalog`.
5. Allocate Service Numbers race-safely and without an exhaustion failure mode.
6. Make every mutation atomically idempotent and every successful state change auditable.
7. Expose a REST contract whose shape remains stable when 5B, 5C, and 5D take ownership of Checks, Payments, Orders, and closure.

### Non-Goals

- **Commit.** Creating Committed Items, Checks, and Charge Allocations belongs to 5B. Consequently the two canonical commands `START_NEW_ORDER_DRAFT` and `SET_ORDER_DRAFT_CHECK_TARGET` are also deferred to 5B: both exist only to steer where a Commit's charges land, and neither can be validated before a Check exists.
- **Payments, Check splitting, and Check merging.** These belong to 5C.
- **Submit, Orders, Preparation Units, Service Session closure, and Completed Sale.** These belong to 5D.
- **Full Modifier Group rule validation.** Minimum and maximum selection counts, and the requirement that a sized item carry exactly one Size, are Commit-time concerns. Section 6.5 explains why enforcing them at draft time would be wrong rather than merely early.
- Cancellation, Waste, Comp, Remake, Refund, and Payment Void.
- Reporting, exports, and historical sale listings.

### Accepted Consequence Of Deferring Commit

Because 5A ships no Commit, an Order Draft can be built but never turned into a charge, and no money can be taken. A Service Session opened in 5A remains `ACTIVE` permanently, because closure is a 5D operation. 5A is therefore not a deployable end state. It is a complete, testable consistency boundary that the remaining sub-phases build on, and the limitation is recorded here so it is not mistaken for a defect during review.

---

## 2. Authority And Terminology

The implementation uses the terms defined in `cafe-pos/CONTEXT.md`:

- A **Service Session** is the continuous period in which one customer party is served, containing its Orders and current Table assignments until staff explicitly close it.
- An **Anonymous Service Session** is an opening-day Service Session with no durable customer identity. Every Service Session in 5A is anonymous; `customer_identity_id` is not a column and is emitted as a constant `null` in the projection.
- A **Service Number** is a short operational label for one Anonymous Service Session, used to identify takeaway handoff and shown alongside current Table assignments for dine-in without identifying the customer.
- An **Order Draft** is a mutable proposed batch of menu items that has not been submitted to preparation.
- A **Preparation Note** is free text attached to an ordered Menu Item solely as a preparation instruction; it cannot satisfy Modifier Group rules, alter price, or substitute for an unavailable or priced Modifier Option.
- A **Table** is a named physical service location that may be associated with one or more active Service Sessions.
- A **Modifier Group** is a reusable set of menu choices assigned unchanged as a Menu Category default or directly to a Menu Item; an item may exclude an inherited group but cannot redefine it.
- A **Sales Shift** is the continuous accountability window for the cashier station's cash fund and payment activity.
- A **Staff Access Session** is the period during which one Staff Identity is authenticated on one device, and is explicitly independent of a Sales Shift.

These domain terms retain their capitalization in documentation. Go identifiers use normal exported naming.

A Service Session is owned by the station, not by the staff member who opened it. Any identity holding `sales.operate` may act on any active Service Session; the opener is retained as an auditable fact, not as an ownership claim. This matches the canonical staff-handoff behavior.

---

## 3. Phase 5 Decomposition

The canonical Sales module is roughly 16,000 lines of TypeScript exposing eighteen commands, five reads, and seventy-eight error codes. Phase 4, by comparison, shipped three operations. A single Phase 5 specification would be unreviewable and a single implementation plan unexecutable. Phase 5 is therefore delivered as four sub-phases, each with its own design specification, implementation plan, and test suite:

| Sub-phase | Scope | Boundary rationale |
| --- | --- | --- |
| **5A** (this document) | Service Session lifecycle, Table assignments, Order Draft | No money and no immutable commercial record. Independently testable. |
| **5B** | Commit, Committed Items, Checks, Charge Allocations, Order Draft targeting | Price fixing is its own consistency boundary. |
| **5C** | Cash and Manual QR Payments, Check splitting and merging, settlement | Completes the Expected Cash formula deferred by ADR-008. |
| **5D** | Submit, Orders, Preparation Units, Service Session closure, Completed Sale | Most entangled with Phase 6; sequenced last. |

The sub-phases are strictly ordered. Each depends on the schema and behavior of its predecessor.

---

## 4. Architecture

`internal/sales` owns the Service Session and Order Draft consistency boundary. It follows the structure established by `internal/tables` and `internal/shift`: one package organized by behavior, with a `Runner` that owns transaction orchestration, per-operation handler types, a `Slices` aggregate, and `RegisterRoutes(v1, authn)`.

The package contains, in 5A:

- Service Session opening, for both service modes.
- Table assignment maintenance for Dine-in Sessions.
- Order Draft item mutation.
- The Service Session read and the active-Session list.
- Shared domain validation, transactional command execution, authorization, request fingerprinting, Service Number allocation, catalog resolution, and DTO assembly.

Each handler owns a narrow, consumer-defined interface containing only the generated sqlc operations it uses. API DTOs remain separate from generated database models.

General transaction mechanics remain in `internal/database`. Sales transaction orchestration and Sales request fingerprints remain in `internal/sales`. `internal/sales` may import `internal/auth` for capability derivation, as `internal/catalog`, `internal/tables`, and `internal/shift` already do. It must not import `internal/catalog`, `internal/tables`, or `internal/shift`.

### 4.1 Operations

| Operation | Capability | Manager approval | Idempotent |
| --- | --- | --- | --- |
| Read a Service Session | `sales.operate` | No | Not applicable |
| List active Service Sessions | `sales.operate` | No | Not applicable |
| Open Takeaway Service Session | `sales.operate` | No | Yes |
| Open Dine-in Service Session | `sales.operate` | No | Yes |
| Set Service Session Tables | `sales.operate` | No | Yes |
| Add Order Draft item | `sales.operate` | No | Yes |
| Set draft item quantity | `sales.operate` | No | Yes |
| Set draft item Size | `sales.operate` | No | Yes |
| Set draft item Preparation Note | `sales.operate` | No | Yes |
| Set draft item Modifier Options | `sales.operate` | No | Yes |
| Remove draft item | `sales.operate` | No | Yes |

`sales.operate` already exists in `auth.DeriveCapabilities` and is held by `MANAGER` and `CASHIER`. `BARISTA` holds neither this capability nor access to any Sales route. No change to the capability table is required.

No 5A operation requires Manager Approval. This matches the canonical source, in which every Sales command authorizes on `sales.operate` alone; second-party approval enters the Sales domain only with Refund, Payment Void, and Comp, in later sub-phases.

### 4.2 Command Flow

Every mutation executes this sequence:

```text
begin transaction
-> reload current identity, session, roles, and capabilities
-> verify sales.operate
-> claim or replay actor-scoped request_id
-> require an OPEN Sales Shift
-> lock the affected rows
-> validate current business state
-> apply mutation
-> insert authoritative Audit Event
-> store idempotent result
-> commit
```

In-transaction authority reload occurs **before** idempotent replay, as established in Phase 4. An actor whose session was locked, revoked, or expired, whose identity was disabled, or whose role was removed cannot replay an earlier successful request.

The open-Sales-Shift requirement is deliberately placed **after** the idempotency claim rather than before it. A replay of a request that succeeded during a Shift must return its stored result even if that Shift has since closed; re-evaluating the Shift precondition ahead of the claim would turn a successful past mutation into a spurious `OPEN_SALES_SHIFT_REQUIRED`. The precondition guards new work only.

### 4.3 Read Flow

Both reads reload current authority and assemble their projection in a read-only, `REPEATABLE READ` transaction, so that the capability check, the Service Session row, its Table assignments, its Order Draft, the draft items, and their selected Modifier Options all observe one database snapshot.

---

## 5. Database Design

Migration `000008_create_sales_draft_slice.sql`.

### 5.1 Corrections To `service_sessions`

Phase 3 provisioned `service_sessions` for the Tables overview read under ADR-006, deliberately omitting one column and, as it turns out, guessing one domain wrongly. 5A completes and corrects it.

**Adding `sales_shift_id`**, exactly as ADR-006 anticipated:

```sql
ALTER TABLE service_sessions
    ADD COLUMN sales_shift_id UUID NOT NULL REFERENCES sales_shifts(id) ON DELETE RESTRICT;
```

The column is `NOT NULL` without a default. This is safe because `service_sessions` has no writer before 5A: Phase 3 only reads it, and its integration tests seed it directly. The migration asserts the table is empty and fails loudly if it is not, rather than silently backfilling a fabricated Shift.

**Correcting the `state` domain.** Migration `000006` declared `CHECK (state IN ('ACTIVE', 'COMPLETED', 'CANCELLED'))`. The canonical `SERVICE_SESSION_STATES` is `['ACTIVE', 'CLOSED']`, and `CONTEXT.md` describes a Service Session as continuing "until staff explicitly close it". `COMPLETED` belongs to the separate Completed Sale entity, and no canonical path cancels a Service Session — an Abandoned Checkout is its own recorded outcome, not a session state. Phase 3 guessed a vocabulary it had no need for. 5A replaces the constraint with `CHECK (state IN ('ACTIVE', 'CLOSED'))`. 5A only ever writes `ACTIVE`; `CLOSED` is written by 5D.

**Replacing the Service Number index.** The global unique index `service_session_service_number_unique` is dropped and replaced by a Shift-scoped one, per Section 6.2:

```sql
DROP INDEX IF EXISTS service_session_service_number_unique;
CREATE UNIQUE INDEX service_session_number_per_shift_unique
    ON service_sessions (sales_shift_id, service_number);
```

The existing `CHECK (service_number ~ '^[A-Z0-9]{6}$')` is retained unchanged; the allocation format in Section 6.2 satisfies it.

A `sequence INTEGER NOT NULL` column is added alongside, holding the numeric part of the Service Number as an authoritative ordinal, with a unique index on `(sales_shift_id, sequence)`.

An index on `(state, created_at ASC, id ASC)` supports the active-Session list.

### 5.2 `order_drafts`

One Order Draft per Service Session in 5A.

| Column | Type | Notes |
| --- | --- | --- |
| `id` | `UUID` PK | |
| `service_session_id` | `UUID NOT NULL` | references `service_sessions(id)` |
| `state` | `TEXT NOT NULL DEFAULT 'EDITABLE'` | `CHECK (state IN ('EDITABLE', 'COMMITTED'))` |
| `created_at` | `TIMESTAMPTZ NOT NULL DEFAULT now()` | |

The `COMMITTED` state is included in the domain from 5A even though nothing writes it, because the draft lock query in Section 10 filters on `state = 'EDITABLE'` and that filter must be meaningful rather than vacuous. 5B writes `COMMITTED`.

A partial unique index enforces at most one editable draft per Session:

```sql
CREATE UNIQUE INDEX order_draft_editable_per_session_unique
    ON order_drafts (service_session_id)
    WHERE state = 'EDITABLE';
```

5B relaxes nothing here: the canonical model also permits only one editable draft at a time, and `START_NEW_ORDER_DRAFT` commits the current one before opening the next.

### 5.3 `order_draft_items`

| Column | Type | Notes |
| --- | --- | --- |
| `id` | `UUID` PK | |
| `order_draft_id` | `UUID NOT NULL` | references `order_drafts(id)` `ON DELETE CASCADE` |
| `menu_item_id` | `UUID NOT NULL` | references `menu_items(id)` `ON DELETE RESTRICT` |
| `size_id` | `UUID` | nullable; references `menu_item_sizes(id)` `ON DELETE RESTRICT` |
| `quantity` | `INTEGER NOT NULL DEFAULT 1` | `CHECK (quantity BETWEEN 1 AND 9999)` |
| `preparation_note` | `TEXT` | nullable; `CHECK` on trimmed length 1..200 when present |
| `modifier_key` | `TEXT NOT NULL` | sorted, comma-joined Modifier Option ids; `''` when none |
| `created_at` | `TIMESTAMPTZ NOT NULL DEFAULT now()` | |

The composition key needs to treat two `NULL`s as equal, which a plain unique index does not. Two generated columns make the key total:

```sql
size_key GENERATED ALWAYS AS (coalesce(size_id::text, '')) STORED,
note_key GENERATED ALWAYS AS (coalesce(preparation_note, '')) STORED
```

```sql
CREATE UNIQUE INDEX order_draft_item_composition_unique
    ON order_draft_items (order_draft_id, menu_item_id, size_key, note_key, modifier_key);
```

This mirrors the canonical schema, which uses the same three key columns for the same reason.

`modifier_key` is a denormalized projection of `order_draft_item_modifier_options`. It exists solely to make the composition index possible; it is derived, never authoritative, and every write that changes the option set rewrites it in the same statement sequence. Section 13 requires a test that pins the two representations together.

### 5.4 `order_draft_item_modifier_options`

| Column | Type | Notes |
| --- | --- | --- |
| `order_draft_item_id` | `UUID NOT NULL` | references `order_draft_items(id)` `ON DELETE CASCADE` |
| `modifier_option_id` | `UUID NOT NULL` | references `modifier_options(id)` `ON DELETE RESTRICT` |

Primary key is the pair. There is no surrogate id and no ordering column: a Modifier Group's options are each selectable at most once, and the projection orders them by Group then Option name.

### 5.5 Idempotency

Every mutation writes to the shared `idempotency_keys` table introduced in Phase 1, per ADR-005 and reaffirmed by ADR-007. No new idempotency table is created; `catalog_mutation_requests` remains the acknowledged historical exception and is not a precedent.

Action names are fully qualified: `sales.start_takeaway_session`, `sales.start_dine_in_session`, `sales.set_session_tables`, `sales.add_draft_item`, `sales.set_draft_item_quantity`, `sales.set_draft_item_size`, `sales.set_draft_item_note`, `sales.set_draft_item_modifiers`, `sales.remove_draft_item`.

Fingerprints are computed over the normalized request payload. Two normalizations matter:

- **Table id order is not significant.** Selection order does not change the target Table set, so `table_ids` is sorted for the fingerprint. The unsorted order is retained for assignment `sequence` and for Audit Events, which do care about the order staff selected Tables in. This reproduces a deliberate canonical decision, which its source documents in a comment.
- **Modifier Option id order is not significant**, for the same reason, and is sorted for the fingerprint.

`preparation_note` is trimmed before fingerprinting, so that a replay differing only in surrounding whitespace is a replay rather than a conflict. This matches the Phase 4 treatment of Cash Movement notes.

`internal/sales` owns its executor. Sharing the idempotency **table** does not imply sharing **helper logic**; `internal/sales` does not import idempotency helpers from another slice.

Action names fit the existing `action VARCHAR(50)` column; the longest 5A name is 29 characters.

**A replay returns a snapshot, not a fresh read.** `idempotency_keys.response_body` stores the projection as it stood when the mutation committed, and a replay returns that row verbatim. Where the projection contains live-derived fields — `available` on a draft item, most notably — a replayed response can therefore disagree with the current database. This is correct: idempotency exists so that a retried request produces the original outcome, and a client that wants current state issues the read. Clients must not treat a replayed mutation response as a refresh. A stored body that no longer parses into the current DTO shape yields `INVALID_STORED_RESULT`, which is why that code is in 5A's error set.

Storing a full Session projection per mutation makes `idempotency_keys` grow faster in Sales than in any earlier slice, since a single order can involve a dozen draft edits. 5A adds no retention policy — none exists for the earlier slices either — but records the concern here so it is weighed once Sales traffic is real rather than discovered as disk pressure.

### 5.6 Audit Events

Audit rows are written to the `audit_events` table introduced in Phase 2. 5A emits:

`SERVICE_SESSION_STARTED`, `DINE_IN_SERVICE_SESSION_STARTED`, `TABLE_ASSIGNMENT_CREATED`, `TABLE_ASSIGNMENT_RELEASED`, `ORDER_DRAFT_ITEM_ADDED`, `ORDER_DRAFT_ITEM_QUANTITY_SET`, `ORDER_DRAFT_ITEM_SIZE_SET`, `ORDER_DRAFT_ITEM_NOTE_SET`, `ORDER_DRAFT_ITEM_MODIFIERS_SET`, `ORDER_DRAFT_ITEM_REMOVED`, `ORDER_DRAFT_ITEMS_MERGED`, and `sales.authorization_denied`.

`ORDER_DRAFT_ITEMS_MERGED` has no canonical counterpart and is added here. A composition edit that silently absorbs one draft row into another changes a visible quantity without any command having asked for that quantity; without its own event, the audit trail cannot explain the change. Its details carry the surviving item id, the absorbed item id, and both quantities.

Table assignment events reuse the canonical names and detail shapes, because `internal/tables` already reads those rows and its overview behavior must not shift.

---

## 6. Business Rules

### 6.1 The Open Sales Shift Requirement

Every 5A mutation requires a Sales Shift in state `OPEN`, and fails with `OPEN_SALES_SHIFT_REQUIRED` otherwise. The new Service Session records that Shift's id. Draft edits re-assert the requirement through the draft lock query in Section 10, which joins `sales_shifts` and filters on `state = 'OPEN'`: a draft belonging to a Session whose Shift has closed is not editable.

Reads carry no Shift requirement. Staff must be able to inspect a Service Session after its Shift closes.

This is the point at which the Phase 4 limitation becomes operationally visible. Phase 4 ships no Close Shift operation, so in a deployed system the first Shift opened stays open. 5A neither worsens nor repairs that; 5C is the sub-phase that makes a complete accountability cycle possible.

### 6.2 Service Number Allocation

The canonical implementation derives the Service Number from the first six hexadecimal characters of the Session's UUID, uppercased, relies on a globally unique index to detect collisions, and retries the whole transaction up to five times before failing with `SERVICE_NUMBER_UNAVAILABLE`.

This is treated as a defect, for three reasons. The value space is 16,777,216, the index is globally unique and never reset, and the collision probability therefore rises with the total number of Sessions ever recorded rather than with concurrent activity. A cafe accumulating sessions over years reaches a point at which retries become routine and then a point at which five retries no longer suffice — an unrecoverable failure in the single most frequent operation in the system. `CONTEXT.md`, meanwhile, defines the Service Number as an *operational label* for identifying takeaway handoff. An operational label needs to be unambiguous among the sessions currently being served, not unique for all time.

5A allocates instead a sequential number scoped to the Sales Shift, formatted as `S` followed by five zero-padded digits: `S00001`, `S00002`, and so on. This satisfies the existing six-character constraint without altering it, cannot exhaust within any plausible Shift, and gives staff a short number that is easier to read aloud than six random characters.

Allocation is race-safe by transaction-scoped advisory lock keyed on the Sales Shift id, taken before the maximum is computed:

```text
pg_advisory_xact_lock(<sales advisory namespace>, <shift id hash>)
-> SELECT coalesce(max(sequence), 0) + 1 FROM service_sessions WHERE sales_shift_id = $1
-> INSERT
```

The advisory lock serializes allocation, so no retry loop is required. The Shift-scoped unique index remains as a final safety net; a `23505` on it indicates a defect in the allocation path and is not caught and retried. Catalog already uses transaction-scoped advisory locks for normalized name collisions, so this introduces no new mechanism.

The numeric part is stored in the dedicated `sequence` column rather than parsed back out of the formatted string. Deriving an ordinal by substring arithmetic on a display label is the kind of coupling that breaks the first time the format changes.

### 6.3 Table Assignment

Opening a Dine-in Session requires at least one Table (`DINE_IN_TABLE_SELECTION_REQUIRED`) and rejects a duplicated id within one request (`DINE_IN_TABLE_SELECTION_DUPLICATE`). Each selected Table is locked `FOR UPDATE` and must exist (`DINE_IN_TABLE_NOT_FOUND`) and be available (`DINE_IN_TABLE_UNAVAILABLE`).

A Table already assigned to another active Service Session is **not** rejected. `CONTEXT.md` states that a Table "may be associated with one or more active Service Sessions", and `internal/tables` already models occupancy as a list rather than a flag. Exclusive table occupancy is not a rule of this system.

Setting Tables on an existing Session computes the difference between current and desired assignments. Removed assignments are released by setting `released_at` and `released_by_staff_identity_id` together — the Phase 3 check constraint requires both — rather than deleted, preserving the history. Added assignments continue the Session's `sequence` from its highest existing value, including released ones, so a released sequence number is never reused. Only newly added Tables are validated for availability; a Table that is already assigned and has since been marked unavailable does not block an unrelated change to the same Session.

Setting Tables on a Takeaway Session is rejected with `TAKEAWAY_TABLE_ASSIGNMENT_NOT_AVAILABLE`. Setting them on a closed Session is rejected with `SERVICE_SESSION_ALREADY_CLOSED`.

An empty `table_ids` on the set-Tables command releases every assignment and is permitted; the canonical source rejects an empty selection only when opening a Session. A Dine-in party that has left its table but not yet paid is a real situation.

### 6.4 Draft Item Composition And Merging

A draft item's composition is the tuple `(menu_item_id, size_id, preparation_note, modifier option set)`. Two draft items with the same composition are the same line, and the canonical system keeps them as one row carrying a quantity.

**Adding.** The add command inserts one row and, on composition conflict, increments the existing row's quantity by one. The command takes no quantity parameter; it always adds one unit, exactly as the canonical source does. Callers wanting five units either add five times or add once and set the quantity.

**Editing into a collision.** Setting a draft item's Size, Preparation Note, or Modifier Options can make it identical in composition to another row in the same draft. When that happens the two rows **merge**: the pre-existing row's quantity increases by the edited row's quantity, and the edited row is deleted. The command returns the projection reflecting the merge, so the caller sees the edited item's id has vanished.

This is the single most easily missed behavior in the canonical source, because it is implemented as a helper called from three different commands and produces a result that no single command's name suggests. Section 13 requires direct coverage of each of the three paths.

Merging deletes the absorbed row, which cascades its `order_draft_item_modifier_options`. The surviving row's option rows are already correct, since the two rows agreed on composition by definition.

The merged quantity is validated against the 9,999 bound of Section 6.7 and rejected if it would exceed it, rather than silently clamping.

### 6.5 Draft Item Validation Depth

Adding or editing a draft item validates only that what has actually been chosen is currently choosable:

- The Menu Item exists, is not retired, and is available.
- A given Size belongs to that Menu Item, is not retired, and is available.
- Every given Modifier Option exists, belongs to a Modifier Group that is effective for that Menu Item, and neither the Option nor its Group is retired, and the Option is available.
- A Preparation Note trims to between 1 and 200 characters, or to nothing, in which case it is stored as `NULL`.

It does **not** validate that the draft is complete. An item whose Menu Item has Size choices may sit in the draft with no Size. A Modifier Group requiring a minimum of one selection may have none. This is not laxity deferred for convenience: `CONTEXT.md` defines the Order Draft as "a mutable proposed batch" and defines Commit as the boundary that "revalidates an Order Draft". A draft that refused to hold an incomplete line would force staff to resolve every choice in a fixed order while a customer is still deciding, and would make it impossible to put an item on screen before its size is known. Completeness is checked once, at Commit, in 5B — which is also the only point at which it matters, because that is where price is fixed.

The corresponding canonical error codes for completeness (`COMMIT_SIZE_REQUIRED`, `COMMIT_MODIFIER_GROUP_INVALID`, and the rest of the `COMMIT_*` family) are therefore absent from 5A's error set by design, not by omission.

### 6.6 Default Modifier Options

When `modifier_option_ids` is **absent** from an add request, the system applies the effective Modifier Groups' declared default options, filtered to those that are available and whose Group is not retired. When it is present but **empty**, no options are applied.

These two cases are distinct and must remain so. Absent means "use the menu's defaults"; empty means "the customer declined every option". Go's JSON decoding collapses both to a nil slice unless the DTO field is a pointer to a slice, so the request DTO declares `ModifierOptionIDs *[]uuid.UUID`. The fingerprint likewise distinguishes them, so that an add with defaults and an add with an explicit empty selection are different requests rather than an idempotency conflict.

Defaults apply only on add. The three composition-editing commands take the caller's selection literally, including an empty one.

### 6.7 Quantity Bounds

The canonical maximum draft quantity is computed as `floor(Number.MAX_SAFE_INTEGER / MAX_MENU_PRICE_VND)`, approximately 4,194,304. That figure is an artifact of JavaScript's integer precision limit, not a business rule; it protects the runtime from producing a silently wrong line total. Go computes line totals in `int64` against `BIGINT` columns and has no equivalent hazard, so migrating the number verbatim would carry a foreign constraint into a system that does not need it.

5A sets the bound at 1 to 9,999 per draft item and enforces it in both the Go domain and a database check constraint. The upper bound is chosen to be far beyond any real cafe order while remaining an obvious data-entry guard: a cashier who types an extra digit is far more likely than a customer ordering ten thousand drinks. The line-total range guard (`LINE_TOTAL_OUT_OF_RANGE`) still exists in the domain for 5B, where prices enter the calculation; at 5A there is no price and no total.

A quantity of zero is rejected by validation rather than treated as removal. Removal is its own command with its own audit event.

### 6.8 Preparation Note

A note is trimmed. If it trims to empty it is stored as `NULL`. If it exceeds 200 characters after trimming it is rejected with `INVALID_PREPARATION_NOTE`. Because `preparation_note` participates in the composition key through `note_key`, changing a note can trigger the merge described in Section 6.4.

---

## 7. Catalog Resolution Without A Catalog Import

Draft item validation needs the effective Modifier Groups of a Menu Item: the Groups inherited from its Menu Category, less the Groups that Item excludes, plus the Groups attached directly to that Item. `internal/catalog` already implements exactly this set algebra in the exported pure function `catalog.EffectiveGroupIDs`.

`internal/sales` does not call it. MIGRATE_PLAN §4.1 requires that a slice not import another slice's code, and ADR-006 already established the alternative for this codebase: `internal/tables` reads the Sales-owned `service_sessions` and `table_assignments` through its own sqlc query rather than importing `internal/sales`. 5A applies the same rule in the opposite direction.

Nor does `internal/sales` reimplement the set algebra in Go, which would be the worst of both worlds — a second copy of the logic with no mechanical link to the first. Instead the resolution is expressed **once, in SQL**, as a single sqlc query in `sql/queries/sales.sql`:

```sql
-- name: ListEffectiveModifierGroupsForMenuItem :many
-- (inherited - exclusions) + direct, for one Menu Item.
```

The query reads `menu_items`, `category_modifier_groups`, `item_modifier_groups`, `item_modifier_group_exclusions`, and `modifier_groups`. A companion query returns the available, non-retired default options of those Groups, and a third validates a given set of Option ids against them.

The duplication that remains is between one Go function and one SQL query, and Section 13 requires a consistency test that runs both against shared fixtures — including a Group that is both inherited and excluded, and a Group that is both excluded and directly attached — and asserts identical results. A test may import both packages; only production code is constrained. This converts the drift risk from something noticed in production into something that fails CI.

---

## 8. Authorization

Every operation requires `sales.operate`, derived from the actor's current roles by `auth.DeriveCapabilities`. `MANAGER` and `CASHIER` hold it; `BARISTA` does not and receives `NOT_AUTHORIZED` on every Sales route.

Authority is reloaded inside the transaction, as described in Section 4.2. The reload covers the identity's enabled flag, the access session's state and expiry, and the current role set. A denial writes a `sales.authorization_denied` audit event recording the attempted identity, the operation, and the denial reason, following the pattern established by Catalog and reaffirmed for Shift.

The client-visible code for every denial is `NOT_AUTHORIZED`. The specific reason reaches the server log and the denial audit event only, so that the API does not disclose whether an identity exists, is disabled, or merely lacks a role.

---

## 9. REST API

All routes are registered under `/api/v1/sales` and require authentication.

| Method | Path | Operation |
| --- | --- | --- |
| GET | `/service-sessions` | List active Service Sessions |
| GET | `/service-sessions/{id}` | Read one Service Session |
| POST | `/service-sessions/takeaway` | Open Takeaway Session |
| POST | `/service-sessions/dine-in` | Open Dine-in Session |
| PUT | `/service-sessions/{id}/tables` | Set Session Tables |
| POST | `/service-sessions/{id}/draft/items` | Add draft item |
| PATCH | `/service-sessions/{id}/draft/items/{item_id}/quantity` | Set quantity |
| PATCH | `/service-sessions/{id}/draft/items/{item_id}/size` | Set Size |
| PATCH | `/service-sessions/{id}/draft/items/{item_id}/preparation-note` | Set Preparation Note |
| PATCH | `/service-sessions/{id}/draft/items/{item_id}/modifiers` | Set Modifier Options |
| DELETE | `/service-sessions/{id}/draft/items/{item_id}` | Remove draft item |

Every mutation accepts `request_id` in its JSON body and returns the **complete Service Session projection** after the change, matching the canonical contract. The client never needs a follow-up read, and a merge that changed an id the client was holding is immediately visible.

`DELETE` returns 200 with the projection rather than 204, for the same reason.

### 9.1 Response Contract

The Service Session projection ships in its final shape from 5A. Fields owned by later sub-phases are present and empty rather than absent, following the precedent set by ADR-008 for `expected_cash_vnd`:

```json
{
  "id": "...",
  "service_number": "S00001",
  "service_mode": "TAKEAWAY",
  "state": "ACTIVE",
  "customer_identity_id": null,
  "sales_shift_id": "...",
  "tables": [],
  "created_at": "...",
  "draft": {
    "id": "...",
    "state": "EDITABLE",
    "items": []
  },
  "checks": [],
  "orders": [],
  "preparation_units": []
}
```

`checks` is populated by 5B and 5C, `orders` and `preparation_units` by 5D. Each is documented in Swagger with the sub-phase that fills it, so an integrator reading the generated docs is not left guessing whether an empty array means "none" or "not implemented". `preparation_alerts` and `preparation_corrections` are Phase 6 concerns and are omitted entirely rather than stubbed; they are optional in the canonical contract, and a field no sub-phase of Phase 5 will ever fill does not belong in Phase 5's contract.

`sales_shift_id` has no canonical counterpart and is added. ADR-011 makes the Service Number unique only within its Shift, so any client that stores, prints, or cross-references a Service Number needs the Shift alongside it to keep the reference unambiguous. Exposing the Shift that already owns the Session is the cheapest way to keep that guarantee honest.

`draft` is nullable in the canonical contract and is always present in 5A, because opening a Session always creates its draft. It becomes genuinely nullable in 5B, when a draft can be committed without a successor.

Empty collections serialize as `[]`, never `null`.

A draft item carries `id`, `menu_item_id`, `name`, `price_vnd` (nullable, from the Size when one is chosen and from the Item otherwise), `size_id`, `size_name`, `quantity`, `available`, `preparation_note`, and `selected_modifier_options`. `available` is the item's current availability read at projection time, not a stored snapshot: a draft is a live proposal, and an item that became unavailable while the customer was deciding must be visible as such before Commit rejects it.

### 9.2 Request Bodies

| Route | Body |
| --- | --- |
| `POST /service-sessions/takeaway` | `request_id` |
| `POST /service-sessions/dine-in` | `request_id`, `table_ids` (min 1) |
| `PUT .../tables` | `request_id`, `table_ids` (may be empty) |
| `POST .../draft/items` | `request_id`, `menu_item_id`, `size_id?`, `preparation_note?`, `modifier_option_ids?` |
| `PATCH .../quantity` | `request_id`, `quantity` |
| `PATCH .../size` | `request_id`, `size_id` (nullable) |
| `PATCH .../preparation-note` | `request_id`, `preparation_note` (nullable) |
| `PATCH .../modifiers` | `request_id`, `modifier_option_ids` |
| `DELETE .../{item_id}` | `request_id` |

`modifier_option_ids` on the add route is a pointer, per Section 6.6. On the modifiers route it is required and may be empty.

Swagger annotates all eleven operations with Bearer security, request DTOs, the projection response, and error status codes.

---

## 10. Transactions And Concurrency

Every mutation runs in one transaction at `READ COMMITTED`. Reads run at `REPEATABLE READ`, read-only.

**The draft lock.** All six draft commands begin by acquiring the draft through a single query that both checks the preconditions and takes the lock:

```sql
SELECT d.id, s.service_mode
FROM service_sessions s
JOIN order_drafts d ON d.service_session_id = s.id
JOIN sales_shifts sh ON sh.id = s.sales_shift_id
WHERE s.id = $1 AND s.state = 'ACTIVE' AND d.state = 'EDITABLE' AND sh.state = 'OPEN'
FOR UPDATE OF d, s
```

A query that checks and locks in one step leaves no window between the check and the write. No match yields `EDITABLE_DRAFT_NOT_FOUND`, which deliberately does not distinguish a missing Session from a closed one or a closed Shift: the caller's remedy is the same, and the distinctions would leak state about Sessions the caller did not ask about. The dedicated `SERVICE_SESSION_NOT_FOUND` and `SERVICE_SESSION_ALREADY_CLOSED` codes are used by the Session-level commands and the read, where the caller is addressing the Session itself.

**Lock ordering.** Adding a draft item locks the draft and its Session first, then the `menu_items` row `FOR UPDATE`, preventing the Item from being retired between validation and write. The order is always Sales rows before Catalog rows. `internal/catalog` never locks Sales rows, so no cycle exists and no deadlock is possible between the two slices.

**Table locks.** Table assignment locks `tables` rows `FOR UPDATE` in sorted id order before validating availability, so two concurrent assignments touching overlapping Table sets cannot deadlock against each other.

**Service Number allocation.** Serialized by transaction-scoped advisory lock on the Sales Shift id, per Section 6.2.

**Idempotency claims.** Actor-scoped, following the Phase 4 mechanism: an advisory lock derived from the actor id and the request id, then a claim in `idempotency_keys`. Concurrent duplicate requests execute the mutation exactly once; the loser returns the stored result.

**Expected constraint violations.** A `23505` on the composition index is handled by the add path's upsert and is never surfaced. A `23503` on `sales_shift_id` maps to `OPEN_SALES_SHIFT_REQUIRED`. A `23505` on the Shift-scoped Service Number index indicates a defect in the advisory-locked allocation path and surfaces as a genuine 500 with the constraint name logged. A `23514` check violation likewise indicates that Go validation and the database disagree and is a defect, not a business state.

---

## 11. Errors

5A implements this subset of the canonical `SALES_ERROR_CODES`:

`NOT_AUTHORIZED`, `OPEN_SALES_SHIFT_REQUIRED`, `SERVICE_SESSION_NOT_FOUND`, `SERVICE_SESSION_ALREADY_CLOSED`, `EDITABLE_DRAFT_NOT_FOUND`, `DRAFT_ITEM_NOT_FOUND`, `MENU_ITEM_NOT_FOUND`, `MENU_ITEM_UNAVAILABLE`, `MENU_ITEM_RETIRED`, `SIZE_NOT_FOUND`, `SIZE_UNAVAILABLE`, `SIZE_RETIRED`, `MODIFIER_OPTION_NOT_FOUND`, `MODIFIER_OPTION_UNAVAILABLE`, `MODIFIER_OPTION_RETIRED`, `INVALID_PREPARATION_NOTE`, `DINE_IN_TABLE_SELECTION_REQUIRED`, `DINE_IN_TABLE_SELECTION_DUPLICATE`, `DINE_IN_TABLE_NOT_FOUND`, `DINE_IN_TABLE_UNAVAILABLE`, `TAKEAWAY_TABLE_ASSIGNMENT_NOT_AVAILABLE`, `REQUEST_CONFLICT`, `INVALID_STORED_RESULT`.

`SERVICE_NUMBER_UNAVAILABLE` is **not** migrated. It exists in the canonical source only as the terminal outcome of the retry loop that Section 6.2 replaces; under advisory-locked Shift-scoped allocation there is no state in which it can be produced, and a code that cannot occur is noise in a client's error handling.

An option id that is well-formed but belongs to a Group not effective for the Item returns `MODIFIER_OPTION_NOT_FOUND` rather than a distinct code, matching the canonical source: from the caller's position the option is not selectable for this item, and the reason is not the caller's business.

Following the precedent set in Phase 3 and reaffirmed in Phase 4, codes describing a `RETURNING` clause yielding no row after a successful existence check and row lock are not migrated. That is a programming defect, not a business state.

Validation messages use JSON field names.

---

## 12. Audit And Notifications

The Audit Event row is part of the business transaction. Audit insertion failure rolls back the Sales mutation and the idempotency claim.

Events contain the actor, the access session, the operation-specific target, and the relevant facts. Replays write no audit event.

Watermill may publish optional post-commit notifications. The in-memory bus is not authoritative, and failure to publish a secondary notification does not reverse committed state. 5A publishes none; the first genuine event consumer is the Preparation queue in Phase 6, fed by 5D's Submit.

---

## 13. Testing

### 13.1 Unit Tests

- Preparation Note normalization: trimming, the empty-to-`NULL` conversion, and the 1 and 200 character boundaries.
- Quantity validation at 0, 1, 9,999, and 10,000.
- `modifier_key` construction: sorting, comma joining, the empty set, and stability against input order.
- Composition equality, including the `NULL` Size and `NULL` note cases that `size_key` and `note_key` exist to handle.
- Service Number formatting at 1, 99,999, and the overflow boundary.
- Request fingerprint stability: proof that `table_ids` and `modifier_option_ids` order does not affect the fingerprint, that note whitespace does not, and that an absent `modifier_option_ids` and an empty one produce different fingerprints.
- Domain error to HTTP status mapping.
- DTO serialization: empty collections as `[]`, `customer_identity_id` as `null`, and the presence of the 5B/5D placeholder fields.

### 13.2 PostgreSQL Integration Tests

Derived from the canonical scenarios in `takeaway.integration.test.ts`, `draft-editing.integration.test.ts`, `dine-in-start.integration.test.ts`, `table-assignments.integration.test.ts`, `sized-items.integration.test.ts`, `direct-modifiers.integration.test.ts`, `inherited-modifiers.integration.test.ts`, `configured-availability.integration.test.ts`, `staff-handoff.integration.test.ts`, and `sales-authorization.integration.test.ts`:

1. A Cashier opens a Takeaway Session against an open Shift; the projection returns it `ACTIVE` with an empty editable draft and Service Number `S00001`. A second Session in the same Shift receives `S00002`.
2. Opening a Session with no open Shift is rejected with `OPEN_SALES_SHIFT_REQUIRED` and creates nothing.
3. A Dine-in Session opens with Tables assigned in selection order; the `internal/tables` overview read reports the Session as an occupant of each.
4. Dine-in rejects an empty selection, a duplicated id, an unknown Table, and an unavailable Table, and creates nothing in each case.
5. Two active Sessions may occupy the same Table concurrently.
6. Setting Tables adds, releases, and leaves untouched the correct assignments; released rows retain `released_at` and `released_by_staff_identity_id` together; `sequence` continues past released values. Releasing every Table is permitted.
7. Setting Tables on a Takeaway Session is rejected with `TAKEAWAY_TABLE_ASSIGNMENT_NOT_AVAILABLE`.
8. Adding the same composition twice yields one row with quantity 2.
9. Editing Size, Preparation Note, and Modifier Options each into an existing composition merges the rows, sums the quantities, deletes the absorbed row, and writes `ORDER_DRAFT_ITEMS_MERGED`. Each of the three paths is covered separately, and a merge that would exceed the quantity bound is rejected.
10. Adding without `modifier_option_ids` applies available, non-retired defaults; adding with an explicit empty list applies none; the two are not idempotency conflicts.
11. An unavailable or retired Menu Item, Size, Modifier Option, or Modifier Group is rejected with the specific code, at add and at each edit.
12. An Option belonging to a Group excluded by the Item is rejected with `MODIFIER_OPTION_NOT_FOUND`; the same Option is accepted for an Item that inherits the Group.
13. A draft item whose Menu Item requires a Size is accepted with no Size, and a required Group may hold no selection. The draft tolerates incompleteness.
14. An item that becomes unavailable after being added remains in the draft and is projected with `available: false`.
15. Draft edits are rejected with `EDITABLE_DRAFT_NOT_FOUND` once the Session's Shift is no longer open.
16. Authority is reloaded: an actor stripped of `sales.operate` mid-session is denied and nothing changes; a `BARISTA` is denied on every route.
17. Any Cashier may edit any active Session's draft, including one opened by a different Cashier.
18. Idempotency: exact replay returns the stored result; the same `request_id` with a different payload returns `REQUEST_CONFLICT`; concurrent duplicate requests execute the mutation once. A replay of a mutation whose Shift has since closed still returns its stored result, and a replay after the Menu Item became unavailable still reports the `available` value captured at commit time while a fresh read reports the current one.
19. Replay is denied after the actor's identity is disabled or session revoked.
20. Audit failure rolls back the mutation and the idempotency claim.
21. Both reads observe one snapshot across the Session row, assignments, draft, items, and options.

Additional coverage:

- **Catalog resolution consistency.** The sales SQL resolution and `catalog.EffectiveGroupIDs` produce identical results over shared fixtures, including a Group both inherited and excluded, a Group both excluded and directly attached, an Item whose Category has no default Groups, and a retired Group.
- **`modifier_key` integrity.** After every composition-editing command, the stored `modifier_key` equals the sorted join of the row's actual `order_draft_item_modifier_options`.
- **Concurrency.** Synchronized goroutines for duplicate `request_id`, and for Service Number allocation under simultaneous Session opens in one Shift, asserting a gapless sequence with no duplicates.
- **Migration.** The `NOT NULL` `sales_shift_id` addition, the corrected `state` domain, the replaced Service Number index, the composition index's treatment of `NULL` Size and note, and the partial editable-draft index.
- **Phase 3 compatibility.** The existing `internal/tables` overview suite passes unchanged against the altered `service_sessions`.

Integration packages run with `-p 1`.

### 13.3 HTTP Tests

- Authentication and route-to-capability mapping for all eleven operations, including the `BARISTA` denial on every route.
- UUID path parameter and request body validation.
- Stable response envelopes, statuses, and error codes.
- `DELETE` returning 200 with the projection.
- Empty collections serialized as `[]`.
- The absent-versus-empty `modifier_option_ids` distinction surviving JSON decoding.
- Swagger annotations covering all eleven operations with Bearer security.

---

## 14. Decision Record Updates

`spec/decisions.md`: three records are added.

- **ADR-010** — Decomposition of Phase 5 into four sub-phases. Context: the canonical Sales module is roughly 16,000 lines exposing eighteen commands, five reads, and seventy-eight error codes, against three operations in Phase 4. Decision: Phase 5 ships as 5A (Service Session and Order Draft), 5B (Commit, Checks, Charge Allocations), 5C (Payments, Check restructuring, settlement), and 5D (Submit, Orders, Preparation Units, closure), each with its own spec, plan, and suite, in that fixed order. Consequence: each sub-phase is reviewable and independently testable; the cost is four spec-and-plan cycles instead of one, and a public contract that ships with fields no sub-phase before its owner can fill.

- **ADR-011** — Service Number allocation scoped to a Sales Shift. Context: the canonical implementation derives the number from six hexadecimal characters of the Session UUID against a permanently global unique index with a five-attempt retry, so collision probability grows with all Sessions ever recorded and ends in an unrecoverable `SERVICE_NUMBER_UNAVAILABLE` in the system's most frequent operation. `CONTEXT.md` defines the Service Number as an operational label, which needs to be unambiguous among Sessions currently being served, not unique for all time. Decision: allocate `S%05d` sequentially within the Sales Shift, serialized by transaction-scoped advisory lock, with a `(sales_shift_id, service_number)` unique index as a safety net and a dedicated `sequence` column as the authoritative ordinal; the six-character check constraint is unchanged and `SERVICE_NUMBER_UNAVAILABLE` is not migrated. Consequence: no exhaustion mode, no retry loop, and a label staff can read aloud; the number is no longer globally unique, so any future cross-Shift reference must carry the Shift id alongside it.

- **ADR-012** — Sales resolves Catalog through its own SQL. Context: draft item validation needs each Menu Item's effective Modifier Groups, which `internal/catalog` already computes in the exported pure function `EffectiveGroupIDs`; MIGRATE_PLAN §4.1 forbids importing another slice, and ADR-006 established reading another slice's tables through one's own sqlc query. Decision: `internal/sales` expresses the resolution once, in SQL, in `sql/queries/sales.sql`, rather than importing the function or reimplementing it in Go; an integration test importing both packages pins the SQL result to `catalog.EffectiveGroupIDs` over shared fixtures including the inherited-and-excluded and excluded-and-direct cases. Consequence: slice boundaries hold at the code layer and the duplication is one function against one query with a mechanical consistency check, rather than two hand-maintained Go copies.

`MIGRATE_PLAN.md` is a phase-status tracker, not a design document. This specification supersedes its Phase 5 sketch, but the roadmap file is not rewritten to match; its Phase 5 entry gains links to the four sub-phase specs, and its tracker row is marked complete only when 5D lands.

---

## 15. Acceptance Criteria

1. `internal/sales` exposes exactly two reads and nine commands. No Commit, no Payment, no Submit, no closure, no Check.
2. `service_sessions` carries `sales_shift_id` as `NOT NULL`, its `state` domain is `('ACTIVE', 'CLOSED')`, and its Service Number index is scoped to the Sales Shift.
3. Service Numbers are allocated as `S%05d` sequentially within a Shift under a transaction-scoped advisory lock, with no retry loop and no exhaustion failure mode.
4. Draft items merge on composition, both when added and when edited into an existing composition, and a merge writes its own audit event.
5. Draft validation checks only what has been chosen; an item with no Size and an unsatisfied required Modifier Group are both accepted into a draft.
6. An absent `modifier_option_ids` applies menu defaults and an empty one applies none, and the two produce different idempotency fingerprints.
7. `internal/sales` imports no other slice but `internal/auth`, and the catalog resolution consistency test passes.
8. Every mutation is actor-scoped and idempotent against the shared `idempotency_keys` table. No new idempotency table is created.
9. Every successful state change writes exactly one Audit Event in the same transaction. Replays write none.
10. Current authority is re-evaluated inside the transaction and before idempotent replay, and the open-Shift precondition is evaluated after the idempotency claim so that a replay survives its Shift closing.
11. Every mutation returns the complete Service Session projection, with `checks`, `orders`, and `preparation_units` present and empty, each documented in Swagger with the sub-phase that fills it.
12. A Table may be occupied by more than one active Service Session, and the `internal/tables` overview suite passes unchanged.
13. Expected database constraint failures map to stable API errors, with no accidental generic 500 responses.
14. Unit, PostgreSQL integration, HTTP, and concurrency tests pass. Existing Auth, Catalog, Tables, and Shift suites remain passing.
15. Swagger documentation reflects all eleven Sales operations with Bearer security.
16. `spec/decisions.md` records ADR-010, ADR-011, and ADR-012. `MIGRATE_PLAN.md` gains sub-phase spec links under Phase 5 without a rewrite of its Phase 5 detail.
