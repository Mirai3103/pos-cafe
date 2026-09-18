# Design Specification: Commit, Checks & Charge Allocations (`internal/sales`, Phase 5B)

- **Author:** Claude Opus 5 & Team
- **Date:** 2026-09-14
- **Status:** Draft
- **Phase:** Phase 5B, the second of four sub-phases of Phase 5 (Core Sales, Orders & Payments)
- **Predecessor:** [`2026-09-13-sales-session-draft-design.md`](2026-09-13-sales-session-draft-design.md) (Phase 5A)

---

## 1. Purpose

This specification defines the Go implementation of Commit — the commercial boundary of the Sales domain — together with the entities it creates: Committed Items, Checks, and Charge Allocations. It also implements the two Order Draft targeting commands that 5A deferred, because both exist only to steer where a Commit's charges land.

As in 5A, the implementation may improve structure, schema, and correctness, but the observable business behavior must match the canonical source in `cafe-pos/src/sales` and the definitions in `CONTEXT.md`. Where the TypeScript runtime behavior conflicts with those documents, the canonical documents win. Known implementation defects are corrected rather than migrated, and every deviation is recorded as an ADR.

`MIGRATE_PLAN.md`'s Phase 5 sketch remains superseded, for the reasons 5A §1 gives. In particular the sketch's `checks` table carries `discount_vnd` and `tax_vnd` columns that exist nowhere in the canonical model; this specification does not create them.

### Goals

1. Implement Commit: revalidate an Order Draft in full, create immutable Committed Items with frozen prices and names, and place their charges in a Check through Charge Allocations.
2. Introduce the `checks`, `committed_items`, `committed_item_modifier_options`, and `charge_allocations` tables.
3. Add the per-draft Check target (`check_target`) and the two commands that read and write it: `START_NEW_ORDER_DRAFT` and `SET_ORDER_DRAFT_CHECK_TARGET`.
4. Enforce the completeness rules 5A deliberately deferred — a sized Item carries exactly one Size, and every effective Modifier Group's minimum and maximum selection counts are satisfied — at the one point where they matter, which is where price is fixed.
5. Populate the `checks` array of the Service Session projection, which 5A shipped as a documented empty placeholder.
6. Keep every mutation atomically idempotent and every successful state change auditable, on the mechanism 5A established.

### Non-Goals

- **Payments.** Cash and Manual QR Payments, and the settlement transition that turns an `OPEN` Check into a `SETTLED` one, belong to 5C. `payments` is projected as a documented empty array, and the `checks.settled_*` columns are not created.
- **Check splitting and merging.** Both belong to 5C. The `charge_allocations` table nevertheless ships in its final shape, because splitting redistributes allocation quantities rather than restructuring the table.
- **Submit, Orders, Preparation Units, Service Session closure, and Completed Sale.** These belong to 5D. The `submitted` flag on a Charge Allocation is projected as a constant `false`.
- **Cancellation-adjusted allocation quantities.** The canonical read subtracts cancelled Preparation Units from an allocation's effective quantity. `preparation_cancellations` is a Phase 6 table and no such adjustment exists yet.
- Refund, Payment Void, Comp, Remake, and Waste.
- Reporting, exports, and historical sale listings.

### Accepted Consequences Of Deferring Submit

Two limitations follow from 5B shipping without Submit, and both are recorded here so they are not mistaken for defects during review.

**A Check can be charged but never settled.** No Payment exists, so every Check created in 5B remains `OPEN` permanently, its balance equal to its charge. `SETTLED` and `MERGED` are declared in the state domain (§5.2) but no code path can write them until 5C.

**A Service Session accepts exactly one Commit.** The canonical `START_NEW_ORDER_DRAFT` refuses to open a new draft while the Session holds either an `EDITABLE` draft or a `COMMITTED` draft that has not yet become an Order. In 5B there is no `orders` table, so the second clause matches every committed draft, and a Session that has committed once can never open another draft through the API.

This is deliberately *not* worked around. The rule's purpose is to stop staff stacking rounds ahead of the kitchen, and relaxing it in 5B would mean shipping a rule that no phase wants and that 5D must remember to tighten again. A documented dead end that disappears when 5D lands is preferable to a temporary rule that must be un-shipped.

The consequence for coverage is concrete: the `CURRENT_UNPAID` branch of Check targeting, which reuses a Session's existing open Check, is unreachable through the API in 5B, because the only Commit a Session can perform is its first. §13.2 therefore exercises that branch against a directly seeded second draft, in the same way Phase 3's integration tests seeded `service_sessions` before `internal/sales` owned them. The code path 5D depends on ships tested rather than merely written.

As with 5A, 5B is not a deployable end state. It is a complete, testable consistency boundary.

---

## 2. Authority And Terminology

5B adds these terms from `CONTEXT.md` to those 5A established:

- **Commit** is the commercial boundary that revalidates an Order Draft, creates immutable Committed Items, and places their charges in a Check without creating an Order or preparation work.
- A **Committed Item** is an immutable commercial snapshot created when a valid draft item's charge joins a Check, preserving names, choices, quantity, price components, total, and Preparation Note before Payment or Order submission.
- A **Check** is a grouping of charges awaiting settlement within a Service Session; Payments settle the Check, while receipts and fiscal invoices are separate documents derived from it.
- **Submit**, by contrast, is the preparation boundary that turns selected Committed Items into an Order and Preparation Units *without repricing them*. It is a 5D concern, and the distinction is the reason Commit and Submit are separate: price is fixed at Commit and never revisited.

**Charge Allocation** is not a `CONTEXT.md` term. It is the structural link this specification introduces, following the canonical schema: a row stating that a given quantity of one Committed Item is charged to one Check. It exists because 5C must be able to move part of an item's charge to another Check without rewriting the immutable Committed Item.

The immutability of a Committed Item is a rule about *this system's* writes, not a database guarantee: no command in 5B, 5C, or 5D updates a `committed_items` row after its insert. Corrections are recorded as separate events, never as edits.

---

## 3. Position In Phase 5

Per ADR-010, Phase 5 ships as four strictly ordered sub-phases. 5A delivered the Service Session lifecycle, Table assignments, and the Order Draft. 5B delivers the boundary at which a proposal becomes money owed. 5C adds Payments and Check restructuring; 5D adds Submit, Orders, Preparation Units, and closure.

5B depends on 5A's schema and behavior in four specific places: the `LockEditableDraft` query and its precondition semantics, the `order_drafts` state domain including the `COMMITTED` value 5A declared but never wrote, the effective-Modifier-Group resolution established by ADR-012, and the executor with its authority reload, idempotency claim, and audit insertion.

---

## 4. Architecture

`internal/sales` gains Commit and the Check lifecycle. No new package is created: Commit is the same consistency boundary as the Order Draft it consumes, sharing the Session lock, the Shift precondition, and the executor. Splitting it out would put a transaction boundary through the middle of one atomic operation.

The package gains, in 5B:

- Commit, in `commit.go`, with its revalidation and pricing logic.
- The two draft-targeting commands, in `draft_rounds.go`, following the canonical grouping that keeps round-lifecycle operations separate from draft-item edits.
- Check assembly for the projection, extending `projection.go`.
- Committed Item and Charge Allocation DTOs, extending `dto.go`.

Each handler continues to own a narrow, consumer-defined interface containing only the generated sqlc operations it uses. `internal/sales` still imports no slice but `internal/auth`.

### 4.1 Operations

| Operation | Capability | Manager approval | Idempotent |
| --- | --- | --- | --- |
| Commit Order Draft | `sales.operate` | No | Yes |
| Start new Order Draft | `sales.operate` | No | Yes |
| Set Order Draft Check target | `sales.operate` | No | Yes |

No 5B operation requires Manager Approval, matching the canonical source: second-party approval enters the Sales domain only with Refund, Payment Void, and Comp. Commit fixes a price but takes no money, and a cashier committing a customer's order is the ordinary case, not an exception.

Action names for the shared `idempotency_keys` table, fully qualified per ADR-007: `sales.commit_order_draft`, `sales.start_new_order_draft`, `sales.set_order_draft_check_target`. The longest is 34 characters and fits the existing `action VARCHAR(50)` column.

Operation constants follow 5A's naming: `OpCommitOrderDraft`, `OpStartNewOrderDraft`, `OpSetOrderDraftCheckTarget`.

### 4.2 Command Flow

Unchanged from 5A §4.2. Authority is reloaded inside the transaction and **before** idempotent replay; the open-Sales-Shift requirement is evaluated **after** the idempotency claim, so a replay of a Commit that succeeded during a Shift still returns its stored result once that Shift has closed.

The replay-returns-a-snapshot rule of 5A §5.5 carries a sharper consequence here. A Commit's stored response body contains the Check as it stood at commit time. In 5B nothing can change a Check afterwards, so the distinction is latent; from 5C onward a replayed Commit response can show a balance that a Payment has since reduced. This is correct — idempotency reproduces the original outcome — and the Swagger description of the Commit response states it.

---

## 5. Database Design

Migration `000009_create_sales_commit_slice.sql`.

All monetary columns are `BIGINT`, per MIGRATE_PLAN §4.2. The canonical schema stores them as `numeric` with a `trunc()` check constraint to compensate for JavaScript's lack of an integer type; that compensation has no purpose in Go and is not migrated.

### 5.1 Adding `order_drafts.check_target`

```sql
ALTER TABLE order_drafts
    ADD COLUMN check_target TEXT NOT NULL DEFAULT 'CURRENT_UNPAID'
        CHECK (check_target IN ('CURRENT_UNPAID', 'NEW_CHECK'));
```

The default matches the canonical default, so a draft created by 5A before this migration behaves exactly as a canonical draft would.

The target belongs to the **draft**, not to the Session, and therefore resets to `CURRENT_UNPAID` every time a new draft opens. A cashier who directed one round to a new Check does not silently direct the next one there too. §13.2 pins this.

### 5.2 `checks`

| Column | Type | Notes |
| --- | --- | --- |
| `id` | `UUID` PK | |
| `service_session_id` | `UUID NOT NULL` | references `service_sessions(id)` `ON DELETE RESTRICT` |
| `state` | `TEXT NOT NULL DEFAULT 'OPEN'` | `CHECK (state IN ('OPEN', 'SETTLED', 'MERGED'))` |
| `charge_vnd` | `BIGINT NOT NULL DEFAULT 0` | `CHECK (charge_vnd >= 0)` |
| `created_at` | `TIMESTAMPTZ NOT NULL DEFAULT now()` | |

The state domain is complete from 5B even though 5B writes only `OPEN`, for the same reason 5A declared `COMMITTED` in the `order_drafts` domain it never wrote: the Check target query in §6.4 filters on `state = 'OPEN'`, and that filter must be meaningful rather than vacuous. 5C then adds settlement columns with `ALTER TABLE ADD COLUMN` and never has to rewrite this constraint.

The settlement and merge columns — `settled_at`, `settled_by_staff_identity_id`, `settled_during_sales_shift_id`, `settled_staff_access_session_id`, `merged_into_check_id` — and the composite constraint tying them to the state are **not** created in 5B. Without them, `SETTLED` and `MERGED` are unreachable, which §13.2 asserts.

Two indexes:

```sql
CREATE INDEX check_service_session_index ON checks (service_session_id);
CREATE INDEX check_open_per_session_index
    ON checks (service_session_id, created_at DESC, id DESC)
    WHERE state = 'OPEN';
```

The partial index serves the `CURRENT_UNPAID` lookup of §6.4 directly.

### 5.3 `committed_items`

| Column | Type | Notes |
| --- | --- | --- |
| `id` | `UUID` PK | |
| `order_draft_id` | `UUID NOT NULL` | references `order_drafts(id)` `ON DELETE RESTRICT` |
| `source_draft_item_id` | `UUID NOT NULL` | references `order_draft_items(id)` `ON DELETE RESTRICT`; unique |
| `menu_item_id` | `UUID NOT NULL` | references `menu_items(id)` `ON DELETE RESTRICT` |
| `category_name` | `TEXT NOT NULL` | snapshot |
| `item_name` | `TEXT NOT NULL` | snapshot |
| `size_name` | `TEXT` | snapshot; `NULL` for a directly priced Item |
| `quantity` | `INTEGER NOT NULL` | `CHECK (quantity BETWEEN 1 AND 9999)` |
| `unit_price_vnd` | `BIGINT NOT NULL` | `CHECK (unit_price_vnd > 0)` |
| `total_vnd` | `BIGINT NOT NULL` | `CHECK (total_vnd > 0 AND total_vnd = quantity * unit_price_vnd)` |
| `preparation_note` | `TEXT` | snapshot |
| `committed_at` | `TIMESTAMPTZ NOT NULL DEFAULT now()` | |

The name columns are copies, not references. A Menu Item renamed or retired tomorrow does not rewrite what a customer was charged for today — which is the entire point of the entity, and the reason `CONTEXT.md` calls it an immutable commercial snapshot.

`menu_item_id` is nevertheless retained, as a link for later reporting. There is deliberately no `size_id` column: the canonical schema keeps the Item reference and not the Size reference, and inventing a column no consumer reads would be speculative work. The quantity bound reuses the 1–9,999 range 5A established, so a draft item that passed validation can never fail this constraint.

`total_vnd = quantity * unit_price_vnd` is asserted in the database as well as computed in Go. The two agreeing is the invariant; a `23514` here means they disagree, which is a defect (§10).

The unique index on `source_draft_item_id` makes committing one draft item twice unrepresentable. The `EDITABLE → COMMITTED` draft transition of §6.5 is the primary guard; this index is the database-level backstop that turns a logic error into a constraint violation rather than a duplicate charge.

Index on `(order_draft_id)` for reading back a draft's committed items.

### 5.4 `committed_item_modifier_options`

| Column | Type | Notes |
| --- | --- | --- |
| `id` | `UUID` PK | |
| `committed_item_id` | `UUID NOT NULL` | references `committed_items(id)` `ON DELETE CASCADE` |
| `modifier_group_id` | `UUID NOT NULL` | references `modifier_groups(id)` `ON DELETE RESTRICT` |
| `modifier_group_name` | `TEXT NOT NULL` | snapshot |
| `modifier_option_id` | `UUID NOT NULL` | references `modifier_options(id)` `ON DELETE RESTRICT` |
| `modifier_option_name` | `TEXT NOT NULL` | snapshot |
| `surcharge_vnd` | `BIGINT NOT NULL` | `CHECK (surcharge_vnd >= 0)` |

Unique on `(committed_item_id, modifier_option_id)`; index on `(committed_item_id)`.

Group and option names are snapshotted for the same reason the item name is. The surcharge is snapshotted because it is a price component: `unit_price_vnd` is the base price plus the sum of these surcharges, and a receipt must be able to show that arithmetic years later.

### 5.5 `charge_allocations`

| Column | Type | Notes |
| --- | --- | --- |
| `id` | `UUID` PK | |
| `committed_item_id` | `UUID NOT NULL` | references `committed_items(id)` `ON DELETE CASCADE` |
| `check_id` | `UUID NOT NULL` | references `checks(id)` `ON DELETE RESTRICT` |
| `quantity` | `INTEGER NOT NULL` | `CHECK (quantity BETWEEN 1 AND 9999)` |
| `created_at` | `TIMESTAMPTZ NOT NULL DEFAULT now()` | |

Unique on `(committed_item_id, check_id)`; index on `(check_id)`.

5B always writes exactly one allocation per Committed Item, carrying the full committed quantity. The table ships in its final shape because 5C's Split reduces one allocation's quantity and inserts another against a different Check — a data change, not a schema change. The unique index is what makes "the same item, split across two Checks" representable as two rows while forbidding two rows for the same pair.

### 5.6 The Stored Charge Invariant

`checks.charge_vnd` is a stored denormalization of `sum(allocation.quantity * committed_item.unit_price_vnd)` over the Check's allocations, in the same spirit as 5A's `modifier_key`: derived, never authoritative, rewritten by every statement sequence that changes an allocation.

It is stored rather than always derived because 5C freezes the charge of a settled or merged Check, at which point the sum over live allocations stops being the right answer.

Every read that assembles a Check recomputes the sum and compares. A mismatch is a defect, not a business state: it surfaces as a 500 with the Check id logged, never as a client-visible error code. §13.2 requires a test that corrupts `charge_vnd` directly and asserts the read refuses rather than serving a wrong total.

### 5.7 Idempotency And Audit Storage

Unchanged. Mutations write to the shared `idempotency_keys` table (ADR-005, ADR-007); audit rows go to `audit_events` (Phase 2). No new idempotency table is created.

---

## 6. Business Rules

### 6.1 Commit Preconditions

Commit acquires the draft through 5A's `LockEditableDraft` query, unchanged. That single statement requires an `ACTIVE` Session, an `EDITABLE` draft, and an `OPEN` Sales Shift, and locks the draft and Session rows. No match yields `EDITABLE_DRAFT_NOT_FOUND`.

A draft with no items is rejected with `EMPTY_DRAFT`. Committing nothing would create a Check with a zero charge and no allocations, which is not a commercial event.

### 6.2 Revalidation

Commit revalidates everything 5A's draft commands validated, plus the completeness rules 5A deferred. The draft items are read and locked in `(created_at, id)` order, which also fixes the order of the resulting Committed Items.

Two whole-draft checks run first, in this order:

1. If any item's Menu Item is retired → `COMMIT_MENU_ITEM_RETIRED`.
2. If any item's Menu Item is unavailable → `COMMIT_MENU_ITEM_UNAVAILABLE`.

This precedence is deliberate and reproduces the canonical behavior: a draft containing one unavailable item and one retired item reports the *retired* one, regardless of their draft order. The distinction is observable, so it is specified rather than left to the implementer to tidy into per-item ordering.

Then, per item in draft order:

3. The Item is *sized* when its own `price_vnd` is `NULL`. A sized Item with no Size → `COMMIT_SIZE_REQUIRED`.
4. A Size that does not exist or belongs to a different Menu Item → `COMMIT_SIZE_INVALID`.
5. A retired Size → `COMMIT_SIZE_RETIRED`. An unavailable Size → `COMMIT_SIZE_UNAVAILABLE`.
6. A Size given for a directly priced Item → `COMMIT_SIZE_INVALID`.
7. A selected Option whose Modifier Group is not effective for the Item → `COMMIT_MODIFIER_OPTION_INVALID`. A selection valid when it was made can become invalid if the Item's Category attachments changed since.
8. A selected Option, or its Group, retired → `COMMIT_MODIFIER_OPTION_RETIRED`. An unavailable Option → `COMMIT_MODIFIER_OPTION_UNAVAILABLE`.
9. For each effective Group: a **retired** Group with `min_selections > 0` → `COMMIT_MODIFIER_GROUP_RETIRED`; a retired Group with `min_selections = 0` is skipped entirely, since nothing is required of it. A live Group whose selected count falls outside `[min_selections, max_selections]` → `COMMIT_MODIFIER_GROUP_INVALID`.

Rule 9 is the check 5A explicitly deferred, and the reason this specification, not 5A, owns the `COMMIT_*` error family.

Every `COMMIT_*` error carries the offending Menu Item's name, wrapped onto the sentinel in the manner 5A already uses for `ErrOpenShiftRequired`. A cashier told only that "an item is unavailable" cannot act; told *which* item, they can.

The canonical duplicate-Option check is **not** migrated. It guards against the same Option appearing twice in one draft item's selection, which 5A's `order_draft_item_modifier_options` primary key on `(order_draft_item_id, modifier_option_id)` makes unrepresentable. A check that cannot fire is noise.

Any failure aborts the whole Commit. There is no partial commit: the transaction rolls back and the draft remains `EDITABLE` and editable.

### 6.3 Pricing

For each item, the base price is the Size's `price_vnd` when the Item is sized and the Item's own `price_vnd` otherwise. The unit price is that base plus the sum of the selected Options' surcharges. The line total is the unit price times the quantity.

Both are computed in `int64`. `LINE_TOTAL_OUT_OF_RANGE` is raised if a line total is not positive or would overflow; `CHECK_CHARGE_OUT_OF_RANGE` if the accumulated Check charge would.

These guards are kept even though they are practically unreachable — 9,999 units at the maximum menu price is roughly 2.1 × 10¹³, and a Check would need some 430,000 such lines to overflow `int64`. Go's integer arithmetic wraps silently on overflow, so money arithmetic that does not check is money arithmetic that can produce a negative total without failing. The check is one comparison.

What is *not* migrated is the canonical ceiling. `MAX_CHECK_CHARGE_VND` is `Number.MAX_SAFE_INTEGER`, a JavaScript precision artifact rather than a business rule — the same category of foreign constraint that 5A removed when it replaced the derived `MAX_DRAFT_QUANTITY` with a plain 9,999. Go against `BIGINT` needs only the positivity and overflow guards. See ADR-013.

### 6.4 Check Targeting

The draft's `check_target` decides where the charges land:

- **`CURRENT_UNPAID`** reuses the Session's most recent `OPEN` Check — ordered by `created_at DESC, id DESC`, locked `FOR UPDATE` — and creates one only if the Session has none.
- **`NEW_CHECK`** always creates a Check.

A newly created Check starts at `charge_vnd = 0` and is immediately raised by the committed amount in the same transaction; it is never observable at zero.

The name `CURRENT_UNPAID` is canonical and is retained even though "unpaid" is vacuous in 5B, where no Check can be paid. The `state = 'OPEN'` filter is what gives it meaning from 5C onward: once a Check can settle, `CURRENT_UNPAID` correctly skips settled Checks and reuses only one still awaiting money.

The canonical `CHECK_CREATION_FAILED` code is **not** migrated. It describes an `INSERT ... RETURNING` yielding no row, which is a programming defect rather than a business state — exactly the category Phase 3 and Phase 4 established as not migrated, and which 5A §11 restates.

### 6.5 What Commit Writes

In one transaction, in this order:

1. One `committed_items` row per draft item, with its snapshot and computed prices.
2. The `committed_item_modifier_options` rows for those items. The table carries no ordering column, so insert order is not observable; presentation order is fixed by the read query in §9.1, as 5A already does for a draft item's selected Options.
3. One `charge_allocations` row per Committed Item, full quantity, against the target Check.
4. The target Check's `charge_vnd`, raised by the committed total.
5. The draft's state, `EDITABLE → COMMITTED`.
6. One `ORDER_DRAFT_COMMITTED` audit event.

Draft items and their selected Options are **not** deleted. They are the provenance that `source_draft_item_id` points at, and deleting them would break the link that lets an auditor see what was proposed versus what was charged.

After the transaction, 5A's partial unique index — one `EDITABLE` draft per Session — permits a new draft to be created, which is what `START_NEW_ORDER_DRAFT` does.

### 6.6 Starting A New Order Draft

`START_NEW_ORDER_DRAFT` is a Session round-lifecycle operation, not a draft-item operation. It locks the Service Session row, requires it `ACTIVE` and belonging to the open Shift, and refuses with `NEW_ORDER_DRAFT_NOT_AVAILABLE` when the Session holds a *blocking* draft.

A blocking draft is one that is `EDITABLE`, or `COMMITTED` without a corresponding Order. In 5B the second clause reduces to every `COMMITTED` draft, with the consequence recorded in §1. The query is nevertheless written in its canonical shape, with the `orders` join added by 5D, so that the rule's intent is visible in the code rather than reconstructed later.

A new draft is created `EDITABLE` with `check_target` at its column default.

### 6.7 Setting The Check Target

`SET_ORDER_DRAFT_CHECK_TARGET` acquires the draft through the same `LockEditableDraft` query the six 5A draft commands use, and writes `check_target`. It therefore carries the same preconditions: `ACTIVE` Session, `EDITABLE` draft, `OPEN` Shift. Setting a target on a committed draft is not a thing that can happen.

---

## 7. Catalog Resolution At Commit

Commit needs more than the effective Group *ids* that ADR-012's `ListEffectiveModifierGroupIDs` returns: rule 9 of §6.2 needs each Group's `min_selections`, `max_selections`, and retirement state, for every Menu Item in the draft at once.

5B therefore adds one query, `ListEffectiveModifierGroupsForCommit`, which resolves `(inherited − exclusions) + direct` for a *set* of Menu Items and returns the Group attributes alongside the ids. The existing per-item query stays as it is; the draft path does not need the extra columns and should not pay for them.

This introduces a second expression of the same set algebra, which is precisely the drift risk ADR-012 exists to contain. The existing `TestSalesResolutionMatchesCatalog` consistency test is therefore **extended** to cover the new query over the same shared fixtures, asserting that for every fixture Menu Item the batched query's group set equals `catalog.EffectiveGroupIDs` and equals the single-item query's result. Without that extension, 5B would ship a third hand-maintained copy of the algebra with a mechanical link to only one of the others.

`internal/sales` still does not import `internal/catalog`. The test may; production code may not.

---

## 8. Authorization

Unchanged from 5A §8. Every operation requires `sales.operate`, held by `MANAGER` and `CASHIER` and not by `BARISTA`. Authority is reloaded inside the transaction, covering the identity's enabled flag, the access session's state and expiry, and the current role set. A denial writes a `sales.authorization_denied` audit event and returns `NOT_AUTHORIZED`, with the specific reason reaching the server log and the audit event only.

---

## 9. REST API

Three routes are added under `/api/v1/sales`, mounted directly on `v1` in the manner `routes.go` already documents.

| Method | Path | Operation |
| --- | --- | --- |
| POST | `/service-sessions/{id}/draft/commit` | Commit the Order Draft |
| POST | `/service-sessions/{id}/draft` | Start a new Order Draft |
| PUT | `/service-sessions/{id}/draft/check-target` | Set the Check target |

Request bodies:

| Route | Body |
| --- | --- |
| `POST .../draft/commit` | `request_id` |
| `POST .../draft` | `request_id` |
| `PUT .../draft/check-target` | `request_id`, `check_target` (`CURRENT_UNPAID` \| `NEW_CHECK`) |

Every mutation returns the complete Service Session projection, as all 5A mutations do.

### 9.1 Response Contract

Two changes to the projection 5A shipped.

**`draft` gains `check_target`**, and becomes genuinely nullable — exactly as 5A §9.1 predicted. After a Commit, the Session has no editable draft, and `draft` is `null` until a new one is started.

**`checks` is populated.** Each Check:

```json
{
  "id": "...",
  "state": "OPEN",
  "created_at": "...",
  "charge_vnd": 85000,
  "total_applied_vnd": 0,
  "balance_vnd": 85000,
  "payments": [],
  "allocations": [
    {
      "id": "...",
      "committed_item_id": "...",
      "menu_item_id": "...",
      "category_name": "Cà phê",
      "name": "Cà phê sữa",
      "size_name": "Lớn",
      "preparation_note": null,
      "modifiers": [
        { "group_id": "...", "group_name": "Đá", "option_id": "...", "option_name": "Ít đá", "surcharge_vnd": 0 }
      ],
      "committed_quantity": 2,
      "committed_total_vnd": 85000,
      "allocated_quantity": 2,
      "amount_vnd": 85000,
      "created_at": "...",
      "submitted": false
    }
  ]
}
```

`total_applied_vnd` is the sum of the Check's Payments and is therefore always `0` in 5B; `balance_vnd` is `charge_vnd − total_applied_vnd`. Both ship in their final shape and are documented in Swagger as completed by 5C, following the precedent ADR-008 set for `expected_cash_vnd`.

`submitted` is `false` for every allocation in 5B and is documented as completed by 5D.

`pending_refund_vnd` is optional in the canonical contract and is **omitted**, not stubbed: Refund is outside Phase 5 entirely, and 5A established that a field no sub-phase of Phase 5 will fill does not belong in Phase 5's contract.

Checks are ordered by `(created_at, id)`; allocations within a Check by `(committed_at, id)` of their Committed Item, so the display order matches the order items were ordered in. An allocation's `modifiers` are ordered by `(modifier_group_name, modifier_option_name)` under the database's collation, matching how 5A orders a draft item's selected Options.

Empty collections serialize as `[]`, never `null`. Swagger annotates all three operations with Bearer security, request DTOs, the projection response, and error status codes.

---

## 10. Transactions And Concurrency

Commit runs in one `READ COMMITTED` transaction. Reads remain read-only `REPEATABLE READ`.

**Lock order** extends 5A's rule that Sales rows are locked before Catalog rows:

1. The draft and its Session, via `LockEditableDraft`.
2. The draft items, `FOR UPDATE`, in `(created_at, id)` order.
3. The Catalog rows — `menu_items`, `menu_item_sizes`, `modifier_options` — `FOR SHARE`, in sorted id order.
4. The target Check, `FOR UPDATE`.

**Catalog rows are locked `FOR SHARE`, not `FOR UPDATE`.** Commit needs to prevent an Item being retired or made unavailable between validation and write; it does not need to exclude another Commit that merely reads the same Item. `FOR UPDATE` would serialize two cashiers committing orders that happen to share a popular item, on the busiest path in the system, for no correctness gain. `FOR SHARE` blocks `internal/catalog`'s mutations — which take `FOR UPDATE` — while letting concurrent Commits proceed. See ADR-015.

5A's single-row `FOR UPDATE` in the add-draft-item path is left unchanged. It locks one row briefly and is not worth the churn.

**Concurrent Commits of the same draft** are serialized by the draft lock. The loser finds the draft no longer `EDITABLE` and receives `EDITABLE_DRAFT_NOT_FOUND`. If both carried the same `request_id`, idempotency returns the stored result instead, per 5A's actor-scoped claim mechanism.

**Concurrent Commits onto the same Check** — possible only from 5D, since 5B allows one Commit per Session — are serialized by the `FOR UPDATE` on the Check row in step 4, so `charge_vnd` cannot lose an update.

**Expected constraint violations.** A `23505` on `committed_item_source_draft_item_unique` means a draft item was committed twice despite the state transition, which is a defect and surfaces as a 500 with the constraint name logged. A `23514` on `committed_item_total_vnd_valid` means Go's arithmetic and the database's disagree, likewise a defect. A `23503` on `charge_allocations.check_id` cannot occur within the transaction that created the Check.

---

## 11. Errors

5B adds this subset of the canonical `SALES_ERROR_CODES`:

`EMPTY_DRAFT`, `COMMIT_MENU_ITEM_UNAVAILABLE`, `COMMIT_MENU_ITEM_RETIRED`, `COMMIT_SIZE_REQUIRED`, `COMMIT_SIZE_INVALID`, `COMMIT_SIZE_UNAVAILABLE`, `COMMIT_SIZE_RETIRED`, `COMMIT_MODIFIER_OPTION_INVALID`, `COMMIT_MODIFIER_OPTION_UNAVAILABLE`, `COMMIT_MODIFIER_OPTION_RETIRED`, `COMMIT_MODIFIER_GROUP_INVALID`, `COMMIT_MODIFIER_GROUP_RETIRED`, `LINE_TOTAL_OUT_OF_RANGE`, `CHECK_CHARGE_OUT_OF_RANGE`, `NEW_ORDER_DRAFT_NOT_AVAILABLE`.

It reuses 5A's `NOT_AUTHORIZED`, `OPEN_SALES_SHIFT_REQUIRED`, `SERVICE_SESSION_NOT_FOUND`, `SERVICE_SESSION_ALREADY_CLOSED`, `EDITABLE_DRAFT_NOT_FOUND`, `REQUEST_CONFLICT`, and `INVALID_STORED_RESULT`.

Two canonical codes are deliberately absent:

- **`CHECK_CREATION_FAILED`**, per §6.4, as a `RETURNING`-defect code.
- The duplicate-selection branch of **`COMMIT_MODIFIER_OPTION_INVALID`**, per §6.2. The code itself is implemented, for the not-effective-Group case.

Status mapping follows 5A: the `COMMIT_*` family and `EMPTY_DRAFT` are `409 Conflict`, since each describes a state the caller must resolve by editing the draft rather than a malformed request. `NEW_ORDER_DRAFT_NOT_AVAILABLE` is likewise `409`. The range guards are `422`, as they describe input that is well-formed but cannot produce a valid charge.

Validation messages use JSON field names.

---

## 12. Audit And Notifications

Audit rows are part of the business transaction; an audit insertion failure rolls back the Commit and the idempotency claim. Replays write no audit event.

5B emits three events:

- **`ORDER_DRAFT_COMMITTED`** — details carry `service_session_id`, `order_draft_id`, `check_id`, `committed_amount_vnd`, and `committed_item_count`.
- **`ORDER_DRAFT_STARTED`** — `service_session_id`, `order_draft_id`.
- **`ORDER_DRAFT_CHECK_TARGET_SET`** — `service_session_id`, `order_draft_id`, `check_target`.

The canonical Commit event is named `TAKEAWAY_CHECKOUT_COMMITTED`. It is renamed here, because Commit is mode-agnostic — the canonical source runs the identical handler for Dine-in — and because 5A already dropped the `TAKEAWAY_` prefix throughout its own event names. Keeping it would imply a distinction that does not exist. The other two names are canonical and unchanged.

Watermill publishes nothing in 5B. The first genuine event consumer is the Preparation queue in Phase 6, fed by 5D's Submit.

---

## 13. Testing

### 13.1 Unit Tests

- Pricing: base plus surcharges for a sized Item, for a directly priced Item, for an Item with no selected Options, and with several Options across Groups.
- Line total and Check charge overflow guards at their boundaries, including the negative-total case that unchecked wrapping would produce.
- Group rule evaluation: count below minimum, above maximum, exactly at each bound, a retired Group with minimum zero (skipped), and a retired Group with a positive minimum (rejected).
- Size classification: sized Item without Size, directly priced Item with a Size, Size belonging to another Item.
- Whole-draft error precedence: a draft containing both a retired and an unavailable Item reports the retired one.
- Request fingerprint stability for all three commands.
- Domain error to HTTP status mapping for every new code.
- DTO serialization: `check_target` present on a draft, `draft` serialized as `null` after Commit, `payments` and empty collections as `[]`, `submitted` constant `false`, `pending_refund_vnd` absent.

### 13.2 PostgreSQL Integration Tests

Derived from the canonical `checkout.integration.test.ts`, `dine-in-checkout.integration.test.ts`, `charge-allocations.integration.test.ts`, and `repeated-orders-and-checks.integration.test.ts`:

1. A Cashier commits a Takeaway draft: Committed Items carry the frozen names and prices, one Charge Allocation each at full quantity, the Check's charge equals the sum, the draft is `COMMITTED`, and the projection returns `draft: null` with one `OPEN` Check.
2. The identical flow for a Dine-in Session produces the identical structure, confirming Commit is mode-agnostic.
3. Committing an empty draft is rejected with `EMPTY_DRAFT` and creates nothing.
4. Each `COMMIT_*` rejection is covered individually — retired and unavailable Item, missing/foreign/retired/unavailable Size, Size on a directly priced Item, Option whose Group is no longer effective, retired and unavailable Option and Group, and Group minimum/maximum violations — and each leaves the draft `EDITABLE` with no Check, no Committed Item, and no allocation.
5. Whole-draft precedence: retired beats unavailable regardless of draft order.
6. Prices are frozen: after a Commit, renaming the Menu Item, repricing the Size, and renaming a Modifier Option leave the Committed Item's snapshot and the Check's charge unchanged.
7. An Item made unavailable *after* Commit does not affect the committed record.
8. `NEW_CHECK` targeting creates a second Check rather than reusing the first — exercised against a seeded second draft, per §1.
9. `CURRENT_UNPAID` targeting reuses the Session's existing `OPEN` Check and accumulates its charge — likewise seeded, per §1.
10. `check_target` resets to `CURRENT_UNPAID` when a new draft is started.
11. `START_NEW_ORDER_DRAFT` is rejected with `NEW_ORDER_DRAFT_NOT_AVAILABLE` while an `EDITABLE` draft exists, and — in 5B — also after a Commit.
12. `SET_ORDER_DRAFT_CHECK_TARGET` is rejected with `EDITABLE_DRAFT_NOT_FOUND` once the draft is committed or the Shift has closed.
13. Commit is rejected with `EDITABLE_DRAFT_NOT_FOUND` once the Session's Shift is no longer open.
14. Authority is reloaded: an actor stripped of `sales.operate` mid-session is denied and nothing changes; a `BARISTA` is denied on all three routes.
15. Idempotency: exact replay returns the stored result without a second Check; the same `request_id` with a different payload returns `REQUEST_CONFLICT`; concurrent duplicate Commits execute once. A replay after the actor's identity is disabled or session revoked is denied.
16. Audit failure rolls back the Commit, the Check, and the idempotency claim.
17. The stored charge invariant: corrupting `checks.charge_vnd` directly makes the read fail rather than serve a wrong total.
18. `SETTLED` and `MERGED` are unreachable in 5B — no code path writes either, asserted over the whole suite's resulting data.

Additional coverage:

- **Catalog resolution consistency.** `TestSalesResolutionMatchesCatalog` is extended so the new batched commit-time query, the existing per-item query, and `catalog.EffectiveGroupIDs` agree over the shared fixtures, including a Group both inherited and excluded, a Group both excluded and directly attached, and a retired Group.
- **Concurrency.** Synchronized goroutines committing the same draft: exactly one succeeds, the other receives `EDITABLE_DRAFT_NOT_FOUND`; and two Sessions committing drafts that share a Menu Item both succeed, demonstrating `FOR SHARE` does not serialize them.
- **Migration.** The `check_target` column and its default over a pre-existing draft, the complete `checks` state domain, the composition and uniqueness constraints of the three new tables, and the `total_vnd = quantity * unit_price_vnd` constraint.
- **Phase 5A compatibility.** The existing Sales, Tables, Shift, Catalog, and Auth suites pass unchanged.

Integration packages run with `-p 1`.

### 13.3 HTTP Tests

- Authentication and capability mapping for all three routes, including the `BARISTA` denial.
- UUID path parameter and request body validation, including an invalid `check_target` value.
- Stable response envelopes, statuses, and error codes for every new code.
- `draft` serialized as `null` after a Commit, and `checks` populated in the documented shape.
- Swagger annotations covering all three operations with Bearer security.

---

## 14. Decision Record Updates

`spec/decisions.md`: three records are added.

- **ADR-013 — Monetary bounds follow `int64`, not the canonical JavaScript ceiling.** Context: the canonical `MAX_CHECK_CHARGE_VND` is `Number.MAX_SAFE_INTEGER`, and `check-totals.ts` guards every accumulation against it, because JavaScript numbers lose integer precision beyond that point. Go stores VND in `int64` against `BIGINT` columns and has no such limit; the constant is a foreign artifact, as 5A already found when it replaced the derived `MAX_DRAFT_QUANTITY` with a plain 9,999. Decision: `unit_price_vnd`, `total_vnd`, and `charge_vnd` are `BIGINT` constrained only to be positive (non-negative for a Check's charge); Go keeps explicit positivity and overflow guards, raising `LINE_TOTAL_OUT_OF_RANGE` and `CHECK_CHARGE_OUT_OF_RANGE`, because Go's arithmetic wraps silently rather than failing. No business ceiling is imposed on a Check's total. Consequence: no arbitrary limit propagates into a domain that does not need one, and the guards that remain exist for a reason that is true in Go.

- **ADR-014 — The `checks` state domain ships complete in 5B; settlement columns do not.** Context: 5B writes only `OPEN` Checks, but its Check-target query filters on `state = 'OPEN'`, and 5C introduces `SETTLED` and `MERGED` along with the columns evidencing them. Decision: migration `000009` declares `CHECK (state IN ('OPEN', 'SETTLED', 'MERGED'))` from the start, while `settled_at`, `settled_by_staff_identity_id`, `settled_during_sales_shift_id`, `settled_staff_access_session_id`, `merged_into_check_id`, and the composite constraint tying them to the state are added by 5C. This follows 5A's treatment of the `COMMITTED` value in the `order_drafts` domain. Consequence: the state filter is meaningful rather than vacuous from 5B, and 5C adds columns without rewriting a constraint; the cost is that two state values are unreachable until 5C, which 5B's suite asserts explicitly.

- **ADR-015 — Commit locks Catalog rows `FOR SHARE`.** Context: the canonical Commit locks `menu_items`, `menu_item_sizes`, and `modifier_options` `FOR UPDATE` to prevent retirement or an availability change between validation and write. Under `FOR UPDATE`, two cashiers committing orders that share one popular item serialize against each other on the system's busiest path, for no correctness gain — Commit only reads those rows. Decision: Commit takes `FOR SHARE` on Catalog rows, in sorted id order, after the Sales rows. `internal/catalog`'s mutations take `FOR UPDATE` and are therefore still blocked for the duration of a Commit. 5A's single-row `FOR UPDATE` in the add-draft-item path is left unchanged rather than churned. Consequence: concurrent Commits sharing menu items proceed in parallel while retirement and availability changes remain excluded; the Sales-before-Catalog lock order of 5A §10 is preserved, so no deadlock cycle is introduced.

`MIGRATE_PLAN.md` gains the 5B spec and plan links in its Phase 5 sub-phase table, with 5B marked complete when the implementation lands. Its Phase 5 detail is not rewritten, and its tracker row stays pending until 5D.

---

## 15. Acceptance Criteria

1. `internal/sales` exposes three new commands — Commit, Start new Order Draft, Set Check target — and no Payment, Split, Merge, Submit, or closure operation.
2. Commit revalidates Menu Item, Size, Modifier Option, and Modifier Group minimum/maximum selection rules, aborts entirely on any failure, and names the offending item in the error.
3. Committed Items are immutable snapshots: names, size name, modifier group and option names, surcharges, unit price, quantity, and total are frozen at commit time and unaffected by later Catalog changes.
4. `total_vnd = quantity * unit_price_vnd` is enforced in Go and in a database check constraint, and a Check's `charge_vnd` equals the sum of its allocations' amounts, verified on every read.
5. `CURRENT_UNPAID` reuses the Session's newest `OPEN` Check and `NEW_CHECK` always creates one; both branches are covered by tests, the reuse branch against a seeded fixture per §1.
6. `check_target` is per-draft and resets to `CURRENT_UNPAID` when a new draft opens.
7. `START_NEW_ORDER_DRAFT` refuses while a blocking draft exists, with the canonical query shape written so 5D adds only the `orders` join.
8. The `checks` state domain is complete from migration `000009`, no settlement column is created, and no 5B code path writes `SETTLED` or `MERGED`.
9. Monetary columns are `BIGINT` with positivity and overflow guards only; no `MAX_SAFE_INTEGER`-derived ceiling exists in the Go code or the schema.
10. Commit locks Sales rows before Catalog rows, and Catalog rows `FOR SHARE`; two Sessions committing drafts that share a Menu Item do not serialize.
11. Every mutation is actor-scoped and idempotent against the shared `idempotency_keys` table; no new idempotency table is created.
12. Every successful state change writes exactly one Audit Event in the same transaction; replays write none.
13. The projection ships `draft.check_target`, a genuinely nullable `draft`, and populated `checks` with `payments: []`, `total_applied_vnd: 0`, and `submitted: false`, each documented in Swagger with the sub-phase that completes it.
14. `internal/sales` imports no slice but `internal/auth`, and the extended catalog resolution consistency test pins both sales queries to `catalog.EffectiveGroupIDs`.
15. Unit, PostgreSQL integration, HTTP, and concurrency tests pass. Existing Auth, Catalog, Tables, Shift, and 5A Sales suites remain passing.
16. Swagger documentation reflects all fourteen Sales operations with Bearer security.
17. `spec/decisions.md` records ADR-013, ADR-014, and ADR-015. `MIGRATE_PLAN.md` gains the 5B links without a rewrite of its Phase 5 detail.
