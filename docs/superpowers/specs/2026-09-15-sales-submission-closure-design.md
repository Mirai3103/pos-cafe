# Design Specification: Submission, Preparation Units & Service Session Closure (`internal/sales`, `internal/preparation`, Phase 5D)

- **Author:** Claude Opus 5 & Team
- **Date:** 2026-09-15
- **Status:** Draft
- **Phase:** Phase 5D, the last of four sub-phases of Phase 5 (Core Sales, Orders & Payments)
- **Predecessors:** [`2026-09-13-sales-session-draft-design.md`](2026-09-13-sales-session-draft-design.md) (5A), [`2026-09-14-sales-commit-checks-design.md`](2026-09-14-sales-commit-checks-design.md) (5B), [`2026-09-14-sales-payments-settlement-design.md`](2026-09-14-sales-payments-settlement-design.md) (5C)

---

## 1. Purpose

This specification defines the Go implementation of the preparation boundary and the end of a Service Session's life: Submit, which turns Committed Items into an Order and the Preparation Units the bar works from; the later Order Draft that Submit unblocks; the advance of a Preparation Unit along its prepared-to-delivered chain; and the closure that freezes everything into an immutable Completed Sale.

As in 5A, 5B, and 5C, the implementation may improve structure, schema, and correctness, but the observable business behavior must match the canonical source in `cafe-pos/src/sales` and the definitions in `cafe-pos/CONTEXT.md`. Where the TypeScript runtime behavior conflicts with those documents, the canonical documents win. Known implementation defects are corrected rather than migrated, and every deviation is recorded as an ADR.

5D is the sub-phase at which Phase 5 becomes a deployable whole. Every earlier sub-phase closed with a note that it was a consistency boundary rather than an end state; this one closes the loop — open a Service Session, order, take money, prepare, and close the sale, entirely through the public API.

### Goals

1. Implement Submit as the preparation boundary: create one `Order`, one `order_item` per Committed Item, and one `preparation_unit` per unit of ordered quantity, without repricing anything.
2. Honor the canonical difference in ordering rules between service modes — takeaway requires settlement before Submit, dine-in does not.
3. Unblock the later Order Draft that 5B deliberately left unreachable, so a Service Session can run multiple rounds.
4. Introduce `internal/preparation` holding exactly one command: advancing a Preparation Unit along `QUEUED → IN_PREPARATION → READY → FULFILLED`.
5. Implement Service Session closure behind a single closure-readiness policy function, producing an immutable Completed Sale and releasing every held Table Assignment.
6. Fill the `orders` and `preparation_units` projection arrays and the `submitted` flag that 5A, 5B, and 5C shipped as documented placeholders, without changing the shape of the contract.
7. Keep every mutation atomically idempotent and every successful state change auditable, on the mechanism 5A established.

### Non-Goals

- **Cancellation, Waste, Comp, Remake, Preparation State Correction, Preparation Alerts, and the Preparation Queue reads.** These are Phase 6. `internal/preparation` ships in 5D with one command and grows into the rest of its surface there (ADR-021).
- **Abandoned Checkout.** It is outside Phase 5 entirely. §6.6 records the closure limitation this leaves behind.
- **Refund, Payment Void, and Post-Shift Payment Correction.** Unchanged from 5C. The canonical closure check for pending Refunds is therefore not migrated (ADR-027).
- **Recent Completed Sales listing.** The canonical module exposes a list of recent Completed Sales alongside the two reads 5D needs. No branch of 5D depends on it and no Phase 5 behavior is unreachable without it.
- Reporting, exports, receipt or fiscal invoice documents.

### Accepted Consequences

**A Service Session holding unsubmitted Committed Items cannot be closed.** Closure requires every Charge Allocation to be submitted, and the canonical escape from that state — recording an Abandoned Checkout when the customer leaves without the work being sent — is outside Phase 5. Staff who commit a draft must submit it before the sale can complete. This is a known limitation, recorded here so it is not mistaken for a defect during review, and it is resolved by Abandoned Checkout rather than by weakening closure.

**A Preparation Unit cannot reach `CANCELLED` or `WASTED` in Phase 5.** Both states are declared in the schema (ADR-026) and both are accepted by the closure policy, because the policy is written once against the complete domain rather than re-edited in Phase 6. `FULFILLED` is a complete terminal path on its own, so closure is fully reachable without them.

---

## 2. Authority And Terminology

5D adds these terms from `cafe-pos/CONTEXT.md`:

- **Submit** is the preparation boundary that turns selected Committed Items into an Order and Preparation Units **without repricing them**. Its counterpart, Commit, is the commercial boundary; the two are distinct events and a Check exists between them.
- An **Order Item** is a Committed Item after submission as part of an Order, retaining its immutable commercial snapshot for preparation and history. The word *retaining* is the authority for §5.2: the snapshot is preserved, not copied.
- A **Preparation Unit** is one individually prepared unit of an ordered item, which is why a Committed Item of quantity three becomes three units.
- **Queued**, **In Preparation**, **Ready**, and **Fulfilled** are the four states 5D writes; **Fulfilled** is the terminal state after staff confirm the unit has been handed to the customer or table, and an Order's fulfillment is **derived from its units rather than tracked independently**. That last clause is the authority for having no Order-level preparation state.
- **Awaiting Submission** is the unresolved state of paid Committed Items that have not yet entered an Order and the Preparation Queue. It is a description of a situation, not a stored state, and 5D represents it as the absence of an Order Item.
- A **Completed Sale** is the immutable outcome of a Service Session that staff close after every Preparation Unit, required Remake, Check, Refund, submission, and correction has reached its permitted terminal state.
- An **Abandoned Checkout** is the recorded terminal outcome of unsubmitted Committed Items and their Check after the customer leaves. 5D does not implement it; §1 records the consequence.

A Completed Sale's immutability, like a Committed Item's and a Payment's, is a rule about this system's writes rather than a database guarantee: no command in Phase 5 updates a `completed_sales` row, or any row it snapshots, after closure.

---

## 3. Position In Phase 5

Per ADR-010, Phase 5 ships as four strictly ordered sub-phases. 5A delivered the Service Session lifecycle, Table assignments, and the Order Draft; 5B delivered Commit and the Check; 5C delivered Payments, settlement, and Check restructuring. 5D delivers the preparation boundary and closure.

5D depends on its predecessors in five specific places: the `service_sessions` table with its `ACTIVE`/`CLOSED` domain and the `CLOSED` value 5A declared but never wrote; the `order_drafts` state machine and the `FindBlockingDraft` seam 5B left explicitly for this sub-phase; the `committed_items` table in its final immutable shape; the `charge_allocations` table and its `submitted` placeholder; and the settled-Check state introduced by 5C, which the takeaway Submit rule and the whole of closure are written against.

It also shifts one boundary outward: ADR-021 moves the Preparation Unit advance command from Phase 6 into 5D.

---

## 4. Architecture

5D spans two packages.

`internal/sales` gains Submit, closure, the Completed Sale reads, and the relaxation of the new-draft rule. `internal/preparation` is created, holding one command.

### 4.1 Package Boundary

`internal/sales` **creates** Preparation Units at Submit and **reads their state** during closure. `internal/preparation` owns **every state transition**.

sqlc generates one shared `internal/database/sqlc` package, so this boundary is a discipline the compiler cannot enforce. It is therefore stated here, restated in a header comment in `sql/queries/preparation.sql`, and enforced in review: no query in `sql/queries/sales.sql` writes `preparation_units.state`, and no query in `sql/queries/preparation.sql` writes `orders`, `order_items`, or `completed_sales`.

The boundary is drawn now, while `internal/preparation` holds one command, because it must exist by Phase 6 regardless, and drawing it later means extracting code from an `internal/sales` that would by then also carry the queue reads, Waste, Remake, and State Correction (ADR-022).

### 4.2 Executor

`internal/catalog`, `internal/shift`, and `internal/sales` each own a `MutationSpec`, a `Runner`, and a generic `ExecuteMutation[T]`. `internal/preparation` follows that established convention with its own. The duplication is deliberate and pre-existing; 5D does not introduce a shared executor package, because doing so would restructure three shipped slices in the service of one new command.

### 4.3 Operations

| Operation | Package | Capability | Result |
| --- | --- | --- | --- |
| Submit Order | `sales` | `sales.operate` | Service Session projection |
| Start New Order Draft | `sales` | `sales.operate` | Service Session projection *(exists; rule relaxed)* |
| Advance Preparation Unit | `preparation` | `preparation.operate` | Preparation Unit |
| Close Service Session | `sales` | `sales.operate` | Completed Sale |
| Get Completed Sale | `sales` | `sales.operate` | Completed Sale |
| Get Completed Sale By Service Session | `sales` | `sales.operate` | Completed Sale |

---

## 5. Database Design

Migration `000011_create_sales_submission_slice.sql`.

### 5.1 `orders`

```sql
CREATE TABLE IF NOT EXISTS orders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    service_session_id UUID NOT NULL REFERENCES service_sessions(id) ON DELETE RESTRICT,
    order_draft_id UUID NOT NULL REFERENCES order_drafts(id) ON DELETE RESTRICT,
    submitted_by_staff_identity_id UUID NOT NULL REFERENCES staff_identities(id) ON DELETE RESTRICT,
    submitted_staff_access_session_id UUID NOT NULL REFERENCES staff_access_sessions(id) ON DELETE RESTRICT,
    submitted_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS order_draft_unique ON orders (order_draft_id);
CREATE INDEX IF NOT EXISTS order_service_session_index ON orders (service_session_id);
```

`UNIQUE (order_draft_id)` carries more weight than it appears to. It makes "one Order per Order Draft" unrepresentable rather than merely enforced in code; it turns a replayed Submit into a no-op instead of a second Order; and it is the row the relaxed new-draft rule joins against to decide whether a round is still outstanding. Three separate correctness properties rest on this one index.

### 5.2 `order_items`

```sql
CREATE TABLE IF NOT EXISTS order_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id UUID NOT NULL REFERENCES orders(id) ON DELETE RESTRICT,
    committed_item_id UUID NOT NULL REFERENCES committed_items(id) ON DELETE RESTRICT
);

CREATE UNIQUE INDEX IF NOT EXISTS order_item_committed_item_unique
    ON order_items (committed_item_id);
CREATE INDEX IF NOT EXISTS order_item_order_index ON order_items (order_id);
```

The canonical source copies eight commercial columns — `menu_item_id`, `category_name`, `item_name`, `size_name`, `quantity`, `unit_price_vnd`, `total_vnd`, `preparation_note` — from `committed_items` into `order_items`. 5D does not (ADR-023). `committed_items` is immutable by 5B's rule and constrained by 5B's checks; reproducing it across a one-to-one foreign key adds no guarantee and adds a way for the two to disagree. CONTEXT.md says an Order Item *retains* its snapshot, which the foreign key satisfies.

`UNIQUE (committed_item_id)` is what the `submitted` flag is derived from (§9.1). There is no `submitted` column anywhere in the schema, and therefore no flag that can fall out of step with the Order that defines it.

### 5.3 `preparation_units`

```sql
CREATE TABLE IF NOT EXISTS preparation_units (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_item_id UUID NOT NULL REFERENCES order_items(id) ON DELETE RESTRICT,
    unit_number INTEGER NOT NULL,
    state TEXT NOT NULL DEFAULT 'QUEUED',
    service_number TEXT NOT NULL,
    category_name TEXT NOT NULL,
    item_name TEXT NOT NULL,
    size_name TEXT,
    modifiers JSONB NOT NULL DEFAULT '[]'::jsonb,
    preparation_note TEXT,
    queued_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT preparation_unit_state_valid
        CHECK (state IN ('QUEUED', 'IN_PREPARATION', 'READY', 'FULFILLED', 'CANCELLED', 'WASTED')),
    CONSTRAINT preparation_unit_number_valid
        CHECK (unit_number BETWEEN 1 AND 9999)
);

CREATE UNIQUE INDEX IF NOT EXISTS preparation_unit_item_number_unique
    ON preparation_units (order_item_id, unit_number);
CREATE INDEX IF NOT EXISTS preparation_unit_state_queued_index
    ON preparation_units (state, queued_at);
```

Here the denormalization is kept, and for a reason `order_items` does not share: the bar display is the hottest read path in the system, refreshed continuously during service, and Phase 6 will read these same columns for FIFO ordering and alerts. Making that view join five tables on every refresh would be the wrong trade. The asymmetry between this table and `order_items` is deliberate and is the difference between a snapshot that serves a read path and a copy that serves nothing.

`state` declares all six canonical values although 5D writes only four. 5A declared a three-value `service_sessions.state` domain it had guessed, and had to correct it; 5C responded by declaring the complete Check domain up front (ADR-014). 5D follows that precedent (ADR-026).

### 5.4 `preparation_unit_transitions`

```sql
CREATE TABLE IF NOT EXISTS preparation_unit_transitions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    preparation_unit_id UUID NOT NULL REFERENCES preparation_units(id) ON DELETE RESTRICT,
    prior_state TEXT NOT NULL,
    resulting_state TEXT NOT NULL,
    actor_staff_identity_id UUID NOT NULL REFERENCES staff_identities(id) ON DELETE RESTRICT,
    staff_access_session_id UUID NOT NULL REFERENCES staff_access_sessions(id) ON DELETE RESTRICT,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT preparation_unit_transition_states_valid
        CHECK (
            (prior_state, resulting_state) IN (
                ('QUEUED', 'IN_PREPARATION'),
                ('IN_PREPARATION', 'READY'),
                ('READY', 'FULFILLED')
            )
        )
);

CREATE INDEX IF NOT EXISTS preparation_unit_transition_unit_index
    ON preparation_unit_transitions (preparation_unit_id, occurred_at);
```

The canonical source has no such table: it reconstructs a Completed Sale's `preparationHistory` by joining `audit_events` on `details ->> 'preparationUnitId' = id::text`, parsing each JSONB payload, and silently discarding any row that fails to parse. 5D writes a real table instead (ADR-025). A Completed Sale is immutable content, not a derived report, and building immutable content out of a loosely-typed audit payload over an uncastable join is the class of defect the earlier sub-phases have consistently corrected rather than migrated.

The audit event is still written, exactly as every other command writes one. Audit is the system's trail of who did what; this table is business data that a Completed Sale is made of. They are different concerns that happen to record the same moment.

The composite `CHECK` encodes the legal transition graph in the database. Phase 6 extends it when it adds Cancellation, Waste, and State Correction, each of which introduces its own legal pairs.

### 5.5 `completed_sales`

```sql
CREATE TABLE IF NOT EXISTS completed_sales (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    service_session_id UUID NOT NULL REFERENCES service_sessions(id) ON DELETE RESTRICT,
    completed_by_staff_identity_id UUID NOT NULL REFERENCES staff_identities(id) ON DELETE RESTRICT,
    completed_staff_access_session_id UUID NOT NULL REFERENCES staff_access_sessions(id) ON DELETE RESTRICT,
    completed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS completed_sale_service_session_unique
    ON completed_sales (service_session_id);
```

`UNIQUE (service_session_id)` makes a double closure fail at the database rather than only at the state check above it.

### 5.6 Idempotency Storage

No table is added for closure idempotency. The canonical source maintains a dedicated `completed_sale_closing_requests` table because its `runIdempotentMutation` helper is typed to the Service Session projection and cannot carry a Completed Sale result. The Go executor introduced in 5A is generic over the result type and stores the replayable body as JSON, so closure uses the shared mechanism with `T = CompletedSaleResponse` (ADR-024). One fewer table, one fewer replay path, and closure's audit and idempotency live where every other command's do.

---

## 6. Business Rules

### 6.1 Submit

Submit runs in one transaction and proceeds in this order.

1. **Lock the source.** Select the Service Session in state `ACTIVE` joined to its Order Draft in state `COMMITTED` for which no Order exists, `FOR UPDATE`.
2. **Lock the Checks.** Select the distinct Checks reachable from the draft's Committed Items through their Charge Allocations, `FOR UPDATE`, ordered deterministically by `created_at, id` — the same lock protocol 5C established in §6.1, so Submit and a concurrent Payment cannot deadlock against each other.
3. **Apply the mode rule.** If the Service Session is `TAKEAWAY` and any of those Checks is not `SETTLED`, reject. If it is `DINE_IN`, do not check settlement at all.
4. **Insert the Order**, then one `order_item` per Committed Item of the draft, ordered by `committed_at, id`.
5. **Insert Preparation Units**: for each Committed Item of quantity *n*, *n* rows numbered 1..*n*, each carrying the Service Number, the category, item and size names, the item's modifier snapshot, and the preparation note. Modifiers are sorted by group name then option name under the `vi-VN` collation, matching the canonical ordering, so two units of the same configuration are byte-identical on the bar display.
6. **Audit** `ORDER_SUBMITTED` with the Service Session, Order Draft, Check ids, Order id, and the two counts.

**The mode rule is the single most migration-sensitive behavior in 5D.** Takeaway keeps settlement-before-Submit: nothing is prepared for a customer who has not paid and may walk. Dine-in supports both `Commit → Submit → Payment` and `Commit → Payment → Submit`, because a seated customer's drinks go to the bar long before the bill is settled. Both orders are valid dine-in service, and a rule that forced settlement first would make dine-in unusable.

**Locking without the `LEFT JOIN` restriction.** The canonical source expresses "a COMMITTED draft with no Order" as a `LEFT JOIN orders ... WHERE orders.id IS NULL`, which forces it to scope `FOR UPDATE` manually because PostgreSQL refuses row locks across a `LEFT JOIN`'s nullable side — it carries a comment explaining the workaround in two places. 5D writes the same condition as `NOT EXISTS`, where the restriction does not arise and `FOR UPDATE` applies plainly. Same semantics, one fewer trap. The same rewrite is applied to `FindBlockingDraft` (§6.2).

### 6.2 The Later Order Draft

5B shipped `FindBlockingDraft` blocking on any `COMMITTED` draft, with a comment recording that the second clause would become `COMMITTED without a corresponding Order` when 5D introduced the `orders` table, and that relaxing it earlier would have shipped a rule no phase wanted. 5D makes exactly that change, via `NOT EXISTS` as above.

The rule then reads as the canonical one intends: a new round may not open while a draft is still `EDITABLE`, or while a committed round has not yet been sent to the bar. Its purpose is to stop staff stacking rounds ahead of the kitchen, not to limit a Service Session to one round.

### 6.3 Advancing A Preparation Unit

One command, in `internal/preparation`, in one transaction: lock the unit `FOR UPDATE`, reject if the requested target is not the successor of its current state, update the state, insert the transition row, and audit `PREPARATION_UNIT_ADVANCED`.

The legal graph is strictly linear and taken unchanged from the canonical source:

```
QUEUED → IN_PREPARATION → READY → FULFILLED
```

**The target state is explicit in the request**, rather than the command meaning "advance one step". A bar display can be seconds stale, and two baristas can act on the same unit at once. With an explicit target, the loser of that race gets `INVALID_TRANSITION` and re-reads; with an implicit "next", it would silently push the unit one state further than either person intended. The canonical source makes the same choice, and it is worth keeping for the same reason.

### 6.4 Closure Readiness

`evaluateClosureReadiness` is a pure function over the Service Session projection. It is the one place closure policy lives, so that the API's answer and any client's preview cannot drift apart — the same intent the canonical source states over its own version, and the reason the canonical exports it to the frontend rather than exposing a readiness endpoint. 5D likewise exposes no readiness endpoint: the Service Session projection already carries every input, and once 5D fills the `orders` and `preparation_units` arrays, a client can evaluate the identical policy locally.

A Service Session is eligible to close when all four hold:

- **Every Check is `SETTLED` or `MERGED`**, and at least one Check is not `MERGED`. The second clause matters: a Session whose only Checks were all merged away has no surviving Check and has settled nothing.
- **Every Charge Allocation is submitted.**
- **At least one Order exists.**
- **Every Preparation Unit is terminal** — `FULFILLED`, `CANCELLED`, or `WASTED`.

The function returns the failing detail alongside the verdict — which Checks are unsettled, which Committed Items are unsubmitted, which units are non-terminal — so the API can say what is outstanding rather than only that something is.

The canonical function also reports Checks carrying a pending Refund. 5D omits that branch (ADR-027): Refund is outside Phase 5, `pending_refund_vnd` is absent from the contract by 5C's decision, and a check with no data source behind it is a check that always passes. Phase 6 or a later Refund phase restores it together with the column it reads.

### 6.5 Closure

In one transaction: lock the Service Session `FOR UPDATE`; if a Completed Sale already exists for it, return that one; if the Session is not `ACTIVE`, reject; build the projection and evaluate readiness; insert `completed_sales`; release every Table Assignment still held, auditing each release; set the Session's state to `CLOSED`; audit `SERVICE_SESSION_CLOSED`; and return the Completed Sale.

**Rejections are ordered**, and the order is the canonical one: unsettled Checks, then unsubmitted work, then a missing Order, then non-terminal preparation. Staff fix what they are told about first, so the order decides which of several outstanding problems they are sent to resolve — money before work, and work before the bar.

**Neither Submit nor closure requires an open Sales Shift.** Only starting a new Order Draft does. This is the canonical arrangement and 5D keeps it: a Shift can end while drinks are still being prepared, and staff must be able to finish what is already in flight. What the Shift rule blocks is opening *new* work.

### 6.6 What Closure Cannot Yet Resolve

Because Abandoned Checkout is outside Phase 5, a Service Session whose customer leaves after a Commit but before a Submit is stuck: its Charge Allocations are unsubmitted, so closure rejects, and no command in Phase 5 moves them to a terminal outcome. The canonical resolution is to record the Abandoned Checkout, which preserves actor, time, and reason without creating a Completed Sale. 5D does not weaken the closure rule to work around its absence, because doing so would let genuinely unfinished work close as a completed sale.

---

## 7. Authorization

No change to `internal/auth` is required. `RoleCapabilities` has carried `preparation.operate` since Phase 1, granted to Manager and Barista, and `sales.operate`, granted to Manager and Cashier.

The split falls out correctly without new configuration: a Barista can advance Preparation Units but cannot submit or close; a Cashier can submit and close but cannot advance; a Manager can do both. The existing inactivity timeouts — fifteen minutes for preparation, five for cashier and manager — apply unchanged.

---

## 8. REST API

| Method | Path | Capability |
| --- | --- | --- |
| `POST` | `/v1/sales/service-sessions/:id/submit` | `sales.operate` |
| `POST` | `/v1/sales/service-sessions/:id/close` | `sales.operate` |
| `GET` | `/v1/sales/completed-sales/:id` | `sales.operate` |
| `GET` | `/v1/sales/service-sessions/:id/completed-sale` | `sales.operate` |
| `POST` | `/v1/preparation/units/:unit_id/advance` | `preparation.operate` |

`POST /v1/sales/service-sessions/:id/draft` already exists; only its blocking rule changes, and its contract does not.

Submit takes only the idempotency request id. Advance takes the request id and `target_state`. Close takes only the request id.

Successful mutations return `200`, except closure, which returns `201` with the created Completed Sale.

---

## 9. Response Contract

### 9.1 Filling The 5D Placeholders

`ServiceSessionResponse` was fixed in shape by 5A precisely so this moment would not break it. Three placeholders are replaced, and no field is added, removed, or retyped:

- `Orders []struct{}` becomes `[]OrderResponse` — id, order draft id, submitted-at, the submitting staff identity and access session, and the Order's items.
- `PreparationUnits []struct{}` becomes `[]PreparationUnitResponse` — id, order item id, unit number, state, service number, category, item and size names, modifiers, preparation note, and queued-at.
- `ChargeAllocationResponse.Submitted` stops being a constant `false` and becomes the existence of an `order_items` row for that Committed Item.

Clients written against 5B or 5C continue to parse 5D responses without modification.

### 9.2 `CompletedSaleResponse`

Id; `state` as the literal `"COMPLETED"`; the Service Session with `state` `"CLOSED"`; the closing staff identity, access session, and display name; `completed_at`; the Checks, each `SETTLED` with a zero balance and carrying its Payments and Charge Allocations; the Orders; the Preparation Units; and `preparation_history`, read from `preparation_unit_transitions`.

### 9.3 Errors

5D adds eight codes: `CHECK_NOT_SETTLED_FOR_SUBMISSION`, `CHECK_NOT_SETTLED_FOR_CLOSURE`, `UNSUBMITTED_WORK_FOR_CLOSURE`, `ORDER_REQUIRED_FOR_CLOSURE`, `UNFULFILLED_PREPARATION_FOR_CLOSURE`, `COMPLETED_SALE_NOT_FOUND`, `PREPARATION_UNIT_NOT_FOUND`, and `INVALID_TRANSITION`.

It reuses `NOT_AUTHORIZED`, `OPEN_SALES_SHIFT_REQUIRED`, `SERVICE_SESSION_NOT_FOUND`, `SERVICE_SESSION_ALREADY_CLOSED`, `NEW_ORDER_DRAFT_NOT_AVAILABLE`, `REQUEST_CONFLICT`, and `INVALID_STORED_RESULT`.

Mapping from the canonical codes:

| Canonical | 5D |
| --- | --- |
| `CHECK_NOT_SETTLED_FOR_SUBMISSION` (raised for a missing source, an empty allocation set, and an unsettled Check alike) | split: `SERVICE_SESSION_NOT_FOUND` for a missing or already-closed source, `CHECK_NOT_SETTLED_FOR_SUBMISSION` for the settlement rule |
| `CHECK_NOT_SETTLED_FOR_CLOSURE` | `CHECK_NOT_SETTLED_FOR_CLOSURE` |
| `PENDING_REFUND_FOR_CLOSURE` | not migrated (ADR-027) |
| `COMPLETED_SALE_CREATION_FAILED` | not migrated — it reports a failed insert that the Go path surfaces as a database error |
| `INVALID_CLOSURE_STORED_RESULT` | `INVALID_STORED_RESULT` (5A) |
| `ADVANCE_FAILED` | not migrated — it belongs to the canonical bulk-advance command, which is Phase 6 |
| the remainder | migrated unchanged |

The canonical Submit raises one code for three distinct situations, two of which are "no such submittable session" rather than anything about a Check. 5D separates them, because a cashier told a Check is unsettled when the real problem is a closed Session will go looking in the wrong place.

Status mapping: `COMPLETED_SALE_NOT_FOUND` and `PREPARATION_UNIT_NOT_FOUND` are `404`; every other new code describes a state the caller must resolve and is `409`.

Swagger annotates all five operations with Bearer security, request DTOs, response schemas, and error status codes, bringing the documented Sales surface to twenty-two operations and adding the first Preparation operation.

---

## 10. Transactions And Concurrency

Every mutation is one transaction, through its package's executor, with the authority reload, idempotency claim, and audit insertion 5A established.

- **Two concurrent Submits of the same draft.** Both lock the Session and draft; the loser finds the Order already present and returns the projection unchanged. `UNIQUE (order_draft_id)` is the backstop if the lock is ever bypassed.
- **Submit against a concurrent Payment.** Both take Check locks in `created_at, id` order, so they serialize rather than deadlock.
- **Two concurrent advances of the same unit.** The unit row lock serializes them; the second sees a state that is no longer the requested target's predecessor and gets `INVALID_TRANSITION`.
- **Closure against a concurrent Payment or advance.** Closure locks the Session, then reads the projection; a Payment or advance that commits first is included, one that commits after finds the Session `CLOSED` and is rejected by its own Session check.
- **Two concurrent closures.** The Session lock serializes them; the second finds the existing Completed Sale and returns it. `UNIQUE (service_session_id)` is the backstop.

---

## 11. Testing

### 11.1 Unit Tests

`evaluateClosureReadiness` carries the bulk of 5D's unit coverage, because it is pure and every closure rule lives in it: each of the four conditions failing alone, several failing together, the all-Checks-merged edge, and the eligible case. The preparation transition table is likewise pure — every legal pair, every illegal pair including backwards moves and skips, and every attempt to leave a terminal state.

### 11.2 PostgreSQL Integration Tests

- **Submit, takeaway:** rejected while any Check is unsettled; succeeds once settled.
- **Submit, dine-in:** succeeds with an outstanding balance; both `Commit → Submit → Payment` and `Commit → Payment → Submit` complete.
- **Fan-out:** a Committed Item of quantity three produces three units numbered 1..3, each carrying the same snapshot and sorted modifiers.
- **Multiple rounds:** commit, submit, open a second draft, commit and submit it; and the negative — a second draft is refused while the first round is committed but unsubmitted.
- **Advance:** the full chain to `FULFILLED`; skipping a step is refused; advancing from `FULFILLED` is refused; each success writes one transition row.
- **Closure:** the eligible case produces a Completed Sale, releases the Table Assignments, and sets the Session `CLOSED`; and four rejection tests, one per condition, asserting the code *and* that the higher-priority conditions are satisfied so the ordering is genuinely exercised.
- **Completed Sale reads:** by id and by Service Session, including `preparation_history` in occurrence order.
- **Concurrency:** parallel Submits of one draft; parallel advances of one unit; parallel closures of one Session.
- **Idempotency:** a replayed Submit and a replayed closure each return the stored response and create nothing further.

### 11.3 HTTP Tests

Route wiring, capability enforcement for all five operations — including a Barista refused Submit and a Cashier refused advance — request validation, and the error-to-status mapping of §9.3.

### 11.4 Reachability

**Every 5D branch is exercised through the public API, with no seeded fixtures.** 5B needed a fixture for Check targeting and recorded why; 5C removed that need for its own surface; 5D removes it for the whole of Phase 5. The complete path — open a Service Session, order, commit, pay, submit, prepare to fulfillment, close — runs entirely on real command output. This is the direct consequence of ADR-021 and the main reason for it.

---

## 12. Decision Record Updates

- **ADR-021** — Phase 5D borrows the Preparation Unit advance command from Phase 6. Context: ADR-010 assigned Preparation Units to 5D meaning their creation at Submit, leaving every state transition to Phase 6. But closure requires every unit to be terminal, and a Completed Sale's `preparation_history` is made of those transitions, so without an advance command the closure branch would be unreachable through the API and the history would ship permanently empty. Decision: 5D implements the linear advance chain and nothing else of Phase 6. Consequence: Phase 5 closes as a genuinely deployable whole with no seeded fixtures; the cost is one command implemented one phase early, in the package that will own it anyway.
- **ADR-022** — `internal/preparation` is created in 5D. Context: the advance command could live in `internal/sales`, which already carries forty-plus files and is gaining four tables in this sub-phase. Decision: create the package now, with the read/write boundary of §4.1. Consequence: Phase 6 grows into an existing package instead of extracting code out of `internal/sales`; the cost is a package holding one command, and a boundary that review must enforce because sqlc's single generated package cannot.
- **ADR-023** — `order_items` carries no commercial snapshot. Context: the canonical table duplicates eight immutable columns from `committed_items` across a one-to-one foreign key. Decision: store the foreign key alone. Consequence: one source of truth and no possibility of divergence; the cost is a join on the Completed Sale and Order reads, and an intentional asymmetry with `preparation_units`, which snapshots for a read-path reason `order_items` does not have.
- **ADR-024** — Closure idempotency uses the shared executor. Context: the canonical source maintains `completed_sale_closing_requests` because its idempotency helper is typed to the Service Session projection. Decision: the Go executor is generic over the result type, so closure uses it with `T = CompletedSaleResponse`. Consequence: one fewer table and one fewer replay path; closure's idempotency and audit behave identically to every other command's.
- **ADR-025** — Preparation history is a table, not a projection over `audit_events`. Context: the canonical source reconstructs it by joining audit rows on a JSONB field cast to text and silently dropping unparseable rows. Decision: write `preparation_unit_transitions` in the same transaction as the advance, and keep the audit event alongside it. Consequence: a Completed Sale's immutable content rests on real foreign keys, real indexes, and a `CHECK`-enforced transition graph; the cost is one table and a deliberate, documented double write of the same moment to two records with different purposes.
- **ADR-026** — `preparation_units.state` declares all six canonical values in 5D. Context: 5A guessed a partial `service_sessions.state` domain and had to correct it; 5C responded with ADR-014's complete-domain precedent. Decision: declare `QUEUED`, `IN_PREPARATION`, `READY`, `FULFILLED`, `CANCELLED`, `WASTED`, and write only the first four. Consequence: Phase 6 adds commands without a schema migration; the closure policy is written once against the complete domain.
- **ADR-027** — The pending-Refund closure check is not migrated. Context: the canonical readiness function reports Checks carrying a pending Refund, but Refund is outside Phase 5 and `pending_refund_vnd` is absent from the contract by 5C's decision. Decision: omit the branch rather than stub it against a column that does not exist. Consequence: the closure policy has one fewer condition than canonical; it is restored together with Refund, and §6.4 records that the omission is deliberate.

---

## 13. Acceptance Criteria

1. Submit creates one Order, one Order Item per Committed Item, and one Preparation Unit per unit of ordered quantity, repricing nothing.
2. A takeaway Submit is refused while any Check is unsettled; a dine-in Submit succeeds in both Payment orderings.
3. A Service Session runs multiple rounds, and a new draft is refused while a committed round is unsubmitted.
4. A Preparation Unit advances `QUEUED → IN_PREPARATION → READY → FULFILLED`, refuses every other move, and records one transition row per advance.
5. Closure succeeds only when all four readiness conditions hold, and its rejections are ordered as §6.5 specifies.
6. Closure creates the Completed Sale, releases every held Table Assignment, sets the Service Session `CLOSED`, and is idempotent under replay.
7. The Completed Sale reads return the sale by id and by Service Session, with `preparation_history` in occurrence order.
8. `orders`, `preparation_units`, and `submitted` are populated, with no change to the shape of `ServiceSessionResponse`.
9. Every branch is reachable through the public API with no seeded fixtures.
10. `go build ./...`, `go vet ./...`, the linter, and the full test suite pass.
