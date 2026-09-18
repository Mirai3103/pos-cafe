# Design Specification: Preparation Cancellation & Financial Corrections (`internal/preparation`, `internal/sales`, `internal/shift`, Phase 6C)

- **Author:** OpenCode & Team
- **Date:** 2026-09-18
- **Status:** Approved
- **Phase:** Phase 6C, the final sub-phase of Preparation Station / Kitchen Display
- **Predecessors:** [`2026-09-17-preparation-corrections-design.md`](2026-09-17-preparation-corrections-design.md) (Phase 6B), [`2026-09-14-sales-payments-settlement-design.md`](2026-09-14-sales-payments-settlement-design.md) (Phase 5C), [`2026-09-15-sales-submission-closure-design.md`](2026-09-15-sales-submission-closure-design.md) (Phase 5D)

---

## 1. Purpose

Phase 6A shipped the operational Preparation Queue and ordinary transitions. Phase 6B shipped Waste, Remake, alerts, and state correction while deliberately reserving Cancellation and Change for the financial design that they require. Phase 6C closes that boundary: queued work can be cancelled or replaced, charged Waste can be Comped, real money can be refunded, incorrect Payments can be voided, and Service Session closure and Shift reconciliation reads can distinguish settled debt from money still owed back to the customer.

The canonical TypeScript service implements Cancellation, Cancellation-adjusted allocations, pending-Refund projection, and the pending-Refund Service Session closure branch. It does not yet implement Refund, Comp, or Payment Void commands. Those missing behaviors are specified by the resolved opening-day decisions under `cafe-pos/.scratch/opening-day-pos/issues/01`, `06`, `07`, and `08`. Phase 6C therefore has two authorities:

1. Preserve the observable canonical Cancellation and Change behavior where it exists.
2. Implement the unresolved financial commands from the accepted opening-day decisions using the established Go transaction, authorization, idempotency, audit, and projection conventions.

### Goals

1. Cancel 1 through 50 queued Preparation Units atomically, writing `CANCELLED`, typed transition history, typed Cancellation facts, and `CANCELLATION` or `CHANGE` alerts.
2. Remove the charge for each charged cancelled unit without mutating its immutable Committed Item or original Charge Allocation.
3. Require a Change to identify an already-submitted replacement Order in the same active Service Session.
4. Comp a charged Wasted unit, before or after Service Session closure, through one reasoned Manager-approved append-only correction.
5. Record Cash and Manual QR Refunds against explicit Charge Adjustments and original Payments without deleting or editing either source.
6. Complete Cash Refunds immediately and keep Manual QR Refunds pending until staff confirm the outbound transfer.
7. Void an incorrect Payment in full while its original Sales Shift remains open, preserving the Payment and appending a reasoned Manager-approved reversal.
8. Keep a fully paid Check `SETTLED` while it carries a pending Refund, while preventing Service Session closure from hiding that obligation.
9. Complete Expected Cash with the Cash Refund and Cash Payment Void terms deferred by ADR-020.
10. Preserve immutable Completed Sale core facts while projecting later Comp and Refund history alongside them.

### Non-Goals

- Manual QR customer excess. Phase 6C continues rejecting every Payment above the current Check balance.
- Post-Shift Payment Correction. A Payment may be voided only while its original Shift remains open.
- Payment Void after Service Session closure, even when the original Shift remains open. That case needs a post-sale replacement-Payment design and is deferred with the broader correction work rather than allowed to mutate a Completed Sale.
- Arbitrary Manager charge edits, percentage discounts, general discounts, coupons, loyalty benefits, or negative-priced Order Items.
- Comp of Cancellation. Cancellation already removes its charge; Comp applies only to a charged Wasted unit.
- Abandoned Checkout and the paid Awaiting Submission recovery flow.
- Observed Manual QR reconciliation fields or nonzero QR discrepancy approval at Shift closure.
- Sales Shift closure itself. The current Go slice exposes Open Shift, current read, and Cash Movement but no Close Shift command; closure and discrepancy handling require a separate design.
- Fiscal-invoice correction, receipt generation, reporting exports, or accounting integration.
- Frontend integration. Phase 7 owns client workflows and generated API types.
- Watermill publication, distributed transactions, or a new `internal/corrections` package.
- Extracting a shared executor across vertical slices.

### Accepted Consequences

This is one large design specification because its commands share the same financial equations and refundable-capacity locks. It remains multiple command-level consistency boundaries: no command opens transactions in two packages, and no event is used to complete an invariant later.

The Preparation package receives one narrow exception to ADR-024. Cancellation is fundamentally a Preparation Unit transition and alert, but it must also reduce the Check charge atomically. Its handler may therefore write the Phase 6C charge-adjustment and Check rows through Preparation-owned sqlc queries. This exception applies only to Cancel/Change; it is not general permission for Preparation to own Payments, Refunds, Comps, or other Sales behavior.

---

## 2. Authority And Terminology

- A **Cancellation** terminates a queued original or Remake Preparation Unit before preparation begins. A charged original unit loses its charge; an uncharged Remake has no financial effect.
- A **Change** is a Cancellation whose intended replacement has already been submitted in a later Order in the same Service Session. Submitted item snapshots are never edited in place.
- A **Charge Adjustment** is an append-only reduction of customer charge sourced by exactly one Cancellation or Comp. It is not a general editing primitive.
- A **Comp** waives the charge of one charged Wasted unit. It requires Manager Approval and may be recorded before closure or as a later correction linked to an immutable Completed Sale.
- A **Refund** is real money returned through the original Payment method. It consumes both corrected refundable capacity and original Payment refundable capacity.
- A **Cash Refund** completes in the same transaction in which staff record handing back cash.
- A **Manual QR Refund** begins as an approved pending intent and completes only when staff confirm the outbound bank transfer.
- A **Payment Void** declares one whole Payment record incorrect without representing money returned. It appends a reversal and never edits or deletes the Payment.
- **Pending Refund** is money the system still owes the customer. For an active sale it is the excess of valid retained Payment over the live corrected Check charge.
- A **Live Adjustment** changes an active Service Session's Check charge. A **Post-Sale Adjustment** links to a Completed Sale and does not rewrite the closed Check or Completed Sale snapshot.

Stable reason catalogs are operation-scoped:

| Operation | Allowed reasons |
| --- | --- |
| Cancellation / Change | `CUSTOMER_REQUEST`, `ORDER_ENTRY_ERROR`, `ITEM_UNAVAILABLE`, `OTHER` |
| Comp | `CAFE_ERROR`, `QUALITY_FAILURE`, `SERVICE_RECOVERY`, `OTHER` |
| Refund | `CUSTOMER_REQUEST`, `ITEM_UNAVAILABLE`, `CAFE_ERROR`, `OTHER` |
| Payment Void | `DUPLICATE_PAYMENT`, `WRONG_AMOUNT`, `WRONG_METHOD`, `PAYMENT_RECORDED_IN_ERROR`, `OTHER` |

Every optional note is trimmed. A present note is 1 through 500 characters, and `OTHER` requires a non-empty note. Reasons and notes are immutable. `RETURN_QR_EXCESS` is deliberately absent because Manual QR excess is a non-goal.

---

## 3. Position In Phase 6

| Sub-phase | Scope | Status |
| --- | --- | --- |
| **6A** | Active queue read, server-authoritative aging, bulk advance | Completed |
| **6B** | Alerts, acknowledgment, Waste, Remake, Remake priority, state correction | Completed |
| **6C** | Cancellation/Change, Charge Adjustments, Comp, Refund, Payment Void, Service Session closure, and Shift read integration | This specification |

Phase 6B already declared `CANCELLATION` and `CHANGE` alert kinds, Cancellation reason constraints, terminal-unit alert retention, and the complete Preparation Unit state domain. Phase 6C writes those reserved values and extends the transition-pair constraint with `QUEUED -> CANCELLED`; it does not reopen the alert or current-state enums.

Phase 5C established immutable Payments, stored Check charges, settlement evidence, and the incomplete Expected Cash formula. Phase 5D established the package boundary, terminal closure policy, and ADR-029's explicit pending-Refund omission. Phase 6C completes each deferred branch rather than introducing a second financial model.

---

## 4. Architecture

### 4.1 Package Ownership

`internal/preparation` owns:

- Cancel and Change command validation.
- The `QUEUED -> CANCELLED` state transition.
- Preparation Cancellation facts and Cancellation/Change alerts.
- The live Charge Adjustment and Check charge update that must commit with Cancellation.
- Queue visibility of cancelled units and alerts.

`internal/sales` owns:

- Charge Adjustment and corrected Check financial projections.
- Comp, Refund, Manual QR Refund confirmation, and Payment Void commands.
- Settlement/reopening consequences, pending-Refund policy, and correction history.
- Service Session and Completed Sale projections.

`internal/shift` owns:

- Expected Cash and current-Shift reconciliation reads over Payment Void and completed Refund facts.
- No write to Sales or Preparation tables and no import of those packages.

No new orchestration package is introduced. The shared generated `internal/database/sqlc` package remains a mechanical database adapter rather than a domain owner.

### 4.2 Consistency Boundaries

Each public command is one PostgreSQL transaction through the executor of its owning package. Authority, Manager Approval where required, idempotency claim, locks, business facts, denormalized state, typed transitions, audit events, and stored result either all commit or all roll back.

Watermill is not involved. Publishing a correction before its financial side is durable, or publishing money before its source correction is durable, would weaken a local atomic invariant into an eventually consistent one for no operational benefit.

### 4.3 Operations

| Operation | Package | Capability | Manager Approval | Result |
| --- | --- | --- | --- | --- |
| Cancel / Change units | `preparation` | `sales.operate` | No | Cancellation outcomes and alerts |
| Comp Waste | `sales` | `sales.operate` | Yes | Charge Adjustment / Comp result |
| Record Refund | `sales` | `sales.operate` | Yes | Refund result |
| Confirm Manual QR Refund | `sales` | `sales.operate` | No new approval | Completed Refund result |
| Void Payment | `sales` | `sales.operate` | Yes | Service Session projection |

Manager Approval uses `auth.VerifyManagerApproval` with the Manager role and `sales.operate`. Self-approval remains permitted by ADR-009. The initiator and approver are always recorded separately.

The Sales executor gains the Shift executor's existing optional-approval shape: `MutationSpec` carries credential-free approval configuration, `MutationContext` exposes the verified `ApproverSummary`, and verification runs before fingerprinting and replay. Preparation Cancellation does not need this extension because it requires no Manager Approval.

Stable idempotency operations are:

- `preparation.cancel_units`
- `sales.comp_waste`
- `sales.record_refund`
- `sales.confirm_qr_refund`
- `sales.void_payment`

Credentials are request-only executor input. Approver login code and PIN are excluded from fingerprints, stored responses, business facts, audit details, and logs.

---

## 5. Database Design

Migration `000014_add_preparation_financial_corrections.sql` adds the following append-only facts and constraints. Existing source records are never updated or deleted, except for the already-denormalized current `checks.charge_vnd`, Check state/evidence, and `preparation_units.state` fields whose purpose is to represent current state.

### 5.1 `charge_adjustments`

| Column | Type | Notes |
| --- | --- | --- |
| `id` | `UUID` PK | |
| `kind` | `TEXT NOT NULL` | `CANCELLATION` or `COMP` |
| `scope` | `TEXT NOT NULL` | `LIVE_CHECK` or `POST_SALE` |
| `preparation_unit_id` | `UUID NOT NULL` | immutable source unit |
| `preparation_waste_id` | `UUID NULL` | required only for Comp |
| `charge_allocation_id` | `UUID NOT NULL` | original charged allocation |
| `check_id` | `UUID NOT NULL` | affected Check |
| `completed_sale_id` | `UUID NULL` | required only for `POST_SALE` |
| `sales_shift_id` | `UUID NOT NULL` | Shift in which the adjustment is recorded |
| `amount_vnd` | `BIGINT NOT NULL` | positive, one physical unit price |
| `created_at` | `TIMESTAMPTZ NOT NULL` | PostgreSQL clock |

Constraints enforce valid `kind`/source pairings and valid `scope`/Completed Sale pairings. A Cancellation has no Waste id. A Comp has one. A live adjustment has no Completed Sale id; a post-sale adjustment has one. `amount_vnd` is positive.

Indexes serve Check financial projection, Completed Sale correction history, source-unit lookup, and outstanding refundable-capacity locks. A unique `(kind, preparation_unit_id)` prevents a second charge reduction for the same unit and kind. The Comp fact adds the stronger one-Comp-per-Waste rule.

`checks.charge_vnd` remains a denormalized current value for active sales. Its invariant becomes:

```text
stored charge
= sum(original Charge Allocation amounts)
- sum(LIVE_CHECK Charge Adjustments)
```

`POST_SALE` adjustments are excluded. They correct the historical commercial outcome without rewriting the immutable closed Check snapshot.

### 5.2 `preparation_cancellations`

| Column | Type | Notes |
| --- | --- | --- |
| `id` | `UUID` PK | |
| `preparation_unit_id` | `UUID NOT NULL UNIQUE` | one terminal Cancellation per unit |
| `kind` | `TEXT NOT NULL` | `CANCELLATION` or `CHANGE` |
| `charge_adjustment_id` | `UUID NULL UNIQUE` | null only for an uncharged Remake |
| `replacement_order_id` | `UUID NULL` | required only for `CHANGE` |
| `reason`, `note` | typed reason, nullable note | immutable |
| `actor_staff_identity_id` | `UUID NOT NULL` | initiator |
| `staff_access_session_id` | `UUID NOT NULL` | initiator session |
| `occurred_at` | `TIMESTAMPTZ NOT NULL` | shared action timestamp |

Constraints enforce the kind/replacement pairing and reason/note catalog. Application and integration invariants enforce that a charged original unit carries an adjustment while a Remake does not.

### 5.3 `sales_comps`

| Column | Type | Notes |
| --- | --- | --- |
| `id` | `UUID` PK | |
| `preparation_waste_id` | `UUID NOT NULL UNIQUE` | one Comp per Waste |
| `charge_adjustment_id` | `UUID NOT NULL UNIQUE` | exact adjustment created by the Comp |
| `reason`, `note` | typed reason, nullable note | immutable |
| `actor_staff_identity_id` | `UUID NOT NULL` | initiator |
| `staff_access_session_id` | `UUID NOT NULL` | initiator session |
| `approved_by_staff_identity_id` | `UUID NOT NULL` | Manager approver |
| `occurred_at` | `TIMESTAMPTZ NOT NULL` | shared action timestamp |

The source Waste points to a `WASTED` standard-priority unit. A Wasted Remake is uncharged and cannot be Comped.

### 5.4 `payment_voids`

| Column | Type | Notes |
| --- | --- | --- |
| `id` | `UUID` PK | |
| `payment_id` | `UUID NOT NULL UNIQUE` | whole Payment only |
| `sales_shift_id` | `UUID NOT NULL` | original Payment Shift |
| `amount_vnd` | `BIGINT NOT NULL` | frozen applied amount |
| `reason`, `note` | typed reason, nullable note | immutable |
| `actor_staff_identity_id` | `UUID NOT NULL` | initiator |
| `staff_access_session_id` | `UUID NOT NULL` | initiator session |
| `approved_by_staff_identity_id` | `UUID NOT NULL` | Manager approver |
| `occurred_at` | `TIMESTAMPTZ NOT NULL` | PostgreSQL clock |

The amount is the original Payment's whole `applied_amount_vnd`; partial Void is not supported. A Payment with any Refund allocation cannot be voided. A voided Payment cannot later receive one.

### 5.5 `refunds`

| Column | Type | Notes |
| --- | --- | --- |
| `id` | `UUID` PK | |
| `check_id` | `UUID NOT NULL` | source Check |
| `completed_sale_id` | `UUID NULL` | present for post-sale Refund |
| `sales_shift_id` | `UUID NOT NULL` | Shift in which the Refund is issued |
| `method` | `TEXT NOT NULL` | `CASH` or `MANUAL_QR` |
| `amount_vnd` | `BIGINT NOT NULL` | positive |
| `reason`, `note` | typed reason, nullable note | immutable |
| `actor_staff_identity_id` | `UUID NOT NULL` | initiator |
| `staff_access_session_id` | `UUID NOT NULL` | initiator session |
| `approved_by_staff_identity_id` | `UUID NOT NULL` | Manager approver |
| `created_at` | `TIMESTAMPTZ NOT NULL` | approval/intent time |

`completed_sale_id` is absent for an active Service Session and present when the source adjustment is `POST_SALE`. The Check, Completed Sale, and every selected adjustment must belong to the same original Service Session.

### 5.6 Refund Allocations

`refund_payment_allocations` contains `(refund_id, payment_id, amount_vnd)` with a unique Refund/Payment pair and positive amount.

`refund_adjustment_allocations` contains `(refund_id, charge_adjustment_id, amount_vnd)` with a unique Refund/Adjustment pair and positive amount.

For one Refund:

```text
refund.amount_vnd
= sum(refund_payment_allocations.amount_vnd)
= sum(refund_adjustment_allocations.amount_vnd)
```

Every selected Payment has the Refund method, belongs to the Refund Check, is not voided, and retains enough unallocated refundable capacity. Every selected Charge Adjustment belongs to the same Check and scope, and retains enough unallocated corrected capacity. Pending Manual QR Refund intents reserve both capacities immediately, so a concurrent Refund cannot spend them again.

### 5.7 `refund_completions`

| Column | Type | Notes |
| --- | --- | --- |
| `id` | `UUID` PK | |
| `refund_id` | `UUID NOT NULL UNIQUE` | one completion per Refund |
| `transaction_reference` | `TEXT NULL` | Manual QR outbound reference only |
| `completed_by_staff_identity_id` | `UUID NOT NULL` | staff confirming movement |
| `staff_access_session_id` | `UUID NOT NULL` | confirming session |
| `completed_at` | `TIMESTAMPTZ NOT NULL` | money-movement time |

Cash Refund creates its Refund and completion in one transaction and carries no transaction reference. Manual QR Refund creates only the Refund intent; confirmation later appends this row. Refund state is derived: no completion is `PENDING`, a completion is `COMPLETED`. No mutable state column exists.

### 5.8 Existing Constraint Changes

The `preparation_unit_transition_states_valid` constraint gains exactly `('QUEUED', 'CANCELLED')`.

The Check settlement-evidence constraint keeps its three states. Phase 6C does not add `REFUND_PENDING`: a Check's state records whether customer debt is covered, while pending Refund is a separate obligation. A Payment Void may legally move `SETTLED -> OPEN` by clearing all four settlement-evidence columns in the same statement.

---

## 6. Financial Policy

### 6.1 Live Check Financials

For an active or not-yet-corrected Check:

```text
base_charge_vnd
= sum(original Charge Allocation amounts)

charge_vnd
= base_charge_vnd
- sum(LIVE_CHECK Charge Adjustments)

valid_payment_vnd
= sum(Payments without Payment Void)

completed_refund_vnd
= sum(Refunds with Refund Completion and completed_sale_id IS NULL)

effective_received_vnd
= valid_payment_vnd - completed_refund_vnd

balance_vnd
= max(charge_vnd - effective_received_vnd, 0)

pending_refund_vnd
= max(effective_received_vnd - charge_vnd, 0)
```

All arithmetic uses guarded `int64` operations. Underflow, overflow, a stored charge mismatch, a negative effective receipt, or allocation sums that disagree are internal invariant failures, not client conflicts.

Pending Manual QR Refunds do not reduce `effective_received_vnd`; money has not left yet. Their allocations reserve capacity, but `pending_refund_vnd` remains until confirmation appends the completion.

### 6.2 Settlement Consequences

Ordinary Payment settlement remains a consequence, not a public command.

Cancellation or Comp may reduce an `OPEN` Check to zero balance. The adjustment transaction then settles the Check, writes all four settlement evidence columns using the initiator and current open Shift, and emits `CHECK_SETTLED`. This includes a zero-charge Check and a Check that simultaneously carries a pending Refund.

A `SETTLED` Check remains `SETTLED` while `pending_refund_vnd > 0`. It cannot be split or merged because it is not open and because it has Payments. Closure, not Check state, enforces resolution of money owed back.

Payment Void recomputes financials without the source Payment. If balance becomes positive, the same transaction moves the Check from `SETTLED` to `OPEN`, clears all settlement evidence, and writes `CHECK_REOPENED_AFTER_PAYMENT_VOID`. If remaining valid Payments still cover the charge, the Check stays settled.

A valid completed Refund consumes only corrected excess. It cannot make balance positive and does not reopen the Check.

A Check carrying any live Charge Adjustment cannot be Split or Merged, even when it remains `OPEN` and has no Payment. Existing restructuring rewrites or moves Charge Allocations; allowing it after an adjustment would invalidate the adjustment's immutable allocation and Check attribution. Split and Merge return `CHECK_HAS_CHARGE_ADJUSTMENT`, and racing restructuring serializes on the Check lock.

### 6.3 Post-Sale Corrections

A post-sale Comp creates a `POST_SALE` Charge Adjustment linked to the Completed Sale. It does not update `checks.charge_vnd`, allocations, Check state, settlement evidence, or any Completed Sale core field.

The post-sale correction amount is refundable capacity outside the live Check equation. A post-sale Refund consumes that capacity and original Payment refundable capacity. The immutable sale projection remains the record as closed; additive correction history explains the later commercial and money effects.

---

## 7. Cancellation And Change

### 7.1 Request

`POST /api/v1/preparation/units/cancel`

```json
{
  "request_id": "uuid",
  "preparation_unit_ids": ["uuid"],
  "kind": "CANCELLATION",
  "replacement_order_id": null,
  "reason": "CUSTOMER_REQUEST",
  "note": null
}
```

Boundary rules:

- `request_id` is non-zero.
- The selection contains 1 through 50 non-zero, unique ids.
- `kind` is `CANCELLATION` or `CHANGE`.
- `CHANGE` requires `replacement_order_id`; `CANCELLATION` forbids it.
- Reason and note follow the Cancellation catalog.
- The normalized UUID-sorted selection and normalized business fields form the fingerprint.

### 7.2 Preconditions

Every unit must exist, be `QUEUED`, and belong to the same active Service Session. An open Sales Shift is required. The command is all-or-nothing; a missing, stale, or cross-Session unit rejects the entire request.

For `CHANGE`, the replacement Order must:

- Exist and belong to the same Service Session.
- Differ from every source Order.
- Have been submitted after every source Order.

The command does not create or edit the replacement. Staff first submit the intended configuration as a new Order, then Cancel the old queued work with a link to that Order.

### 7.3 Charged-Unit Resolution

Original units map to Charge Allocations by the original allocation ranges ordered by allocation creation and id. Existing Cancellations do not renumber later units. The selected unit's adjustment amount is its immutable configured per-unit price.

A Remake has `priority = REMAKE` and no customer charge. Cancelling it writes no Charge Adjustment and changes no Check.

### 7.4 Writes

For every selected unit, one timestamp applies to the batch and the transaction:

1. Appends a live Charge Adjustment when the unit is charged.
2. Updates and verifies each affected `checks.charge_vnd` once after all adjustments are planned.
3. Moves the unit to `CANCELLED`.
4. Appends the typed `QUEUED -> CANCELLED` transition.
5. Appends the Preparation Cancellation fact.
6. Creates an unacknowledged `CANCELLATION` or `CHANGE` alert.
7. Applies any Check settlement consequence.
8. Writes `PREPARATION_UNIT_CANCELLED`, `PREPARATION_ALERT_CREATED`, `CHECK_CHARGE_ADJUSTED`, and any `CHECK_SETTLED` audit events.
9. Stores the replayable batch result.

The route returns `200`. Its replayable data contract is:

```json
{
  "outcomes": [
    {
      "cancellation_id": "uuid",
      "preparation_unit_id": "uuid",
      "prior_state": "QUEUED",
      "resulting_state": "CANCELLED",
      "charge_adjustment_id": "uuid-or-null",
      "charge_removed_vnd": 48000,
      "occurred_at": "timestamp"
    }
  ],
  "alerts": [
    {
      "id": "uuid",
      "kind": "CANCELLATION",
      "preparation_unit_id": "uuid",
      "service_number": "S00001",
      "item_name": "Ca phe sua",
      "unit_number": 1,
      "reason": "CUSTOMER_REQUEST",
      "note": null,
      "created_at": "timestamp",
      "acknowledged_at": null,
      "acknowledged_by_staff_identity_id": null,
      "waste_id": null
    }
  ]
}
```

There is one outcome and one alert per input unit in request order. An uncharged Remake has null `charge_adjustment_id` and zero `charge_removed_vnd`. The result contains no full Check, Payment, or Refund data.

Cancellation does not use Phase 6A savepoints. Partial operational advancement is useful; a partial financial correction is not.

---

## 8. Comp

### 8.1 Request And Source

`POST /api/v1/sales/wastes/:waste_id/comp`

The body carries `request_id`, `reason`, `note`, and Manager Approval credentials.

The Waste must exist, its unit must be `WASTED`, and the unit must be a charged standard unit. A Wasted Remake has no allocation and returns `COMP_SOURCE_NOT_CHARGED`. The unique Waste reference permits one Comp only.

### 8.2 Active Service Session

When the source Session is active, Comp creates a `LIVE_CHECK` adjustment, updates the Check charge, recomputes settlement and pending Refund, appends the Comp fact and audits, and returns the updated Service Session plus correction identity.

An open Sales Shift is required. If the Comp settles an open Check, settlement evidence uses the initiator and current Shift; the Manager approver remains separately recorded on the Comp.

### 8.3 Closed Service Session

When the source Session is closed, the handler must find its Completed Sale and creates a `POST_SALE` adjustment. It writes no Check, allocation, settlement, Preparation Unit, or Completed Sale core field. An open current Shift is still required because this is a new financial correction that must be resolved and reconciled.

The route returns `201`. Its result has the same discriminated shape for both scopes:

```json
{
  "scope": "LIVE_CHECK",
  "comp": {
    "id": "uuid",
    "waste_id": "uuid",
    "preparation_unit_id": "uuid",
    "charge_adjustment_id": "uuid",
    "amount_vnd": 48000,
    "reason": "CAFE_ERROR",
    "note": null,
    "actor_staff_identity_id": "uuid",
    "approved_by_staff_identity_id": "uuid",
    "occurred_at": "timestamp"
  },
  "service_session": {}
}
```

For `POST_SALE`, `service_session` is absent, `completed_sale_id` is present, and `outstanding_post_sale_refund_vnd` equals the uncompleted post-sale correction amount. For `LIVE_CHECK`, the inverse holds.

---

## 9. Refund

### 9.1 Record Refund

`POST /api/v1/sales/refunds`

```json
{
  "request_id": "uuid",
  "check_id": "uuid",
  "method": "CASH",
  "adjustment_allocations": [
    { "charge_adjustment_id": "uuid", "amount_vnd": 48000 }
  ],
  "payment_allocations": [
    { "payment_id": "uuid", "amount_vnd": 48000 }
  ],
  "reason": "ITEM_UNAVAILABLE",
  "note": null,
  "manager_approval": {
    "approver_login_code": "MGR001",
    "manager_pin": "1234"
  }
}
```

Boundary validation rejects empty allocations, duplicate ids, non-positive amounts, mismatched sums, invalid methods, invalid reasons, and malformed approval fields before a request id is consumed.

Inside the transaction:

1. Require the current open Sales Shift.
2. Lock and validate the Check and its active Session or Completed Sale.
3. Lock selected Payments and Charge Adjustments in UUID order.
4. Reject a voided Payment, a Payment of another method or Check, mixed live/post-sale adjustments, or a source from another sale.
5. Sum all existing Refund allocations, including pending Manual QR Refunds, and verify remaining capacity independently on every Payment and Adjustment.
6. For a live Refund, refuse an amount above current `pending_refund_vnd`.
7. Insert the Refund and both allocation sets.
8. For Cash, insert Refund Completion immediately. For Manual QR, leave it pending.
9. Write `REFUND_RECORDED` and, for Cash, `REFUND_COMPLETED` audit events.
10. Store the replayable result.

A Refund cannot mix Cash and Manual QR Payments. Staff issue separate Refunds when corrected value was originally paid through both methods.

The route returns `201`. The result uses the same `LIVE_CHECK` / `POST_SALE` discriminator as Comp and contains a `refund` with id, Check, optional Completed Sale, Shift, method, amount, derived `PENDING` or `COMPLETED` state, both allocation collections, reason/note, initiator, approver, creation time, and nullable completion evidence. `LIVE_CHECK` additionally carries the updated Service Session; `POST_SALE` carries the Completed Sale id and additive correction history.

### 9.2 Confirm Manual QR Refund

`POST /api/v1/sales/refunds/:refund_id/confirm`

The body carries `request_id` and optional trimmed outbound `transaction_reference` of at most 100 characters.

The Refund must exist, use `MANUAL_QR`, lack a completion, and belong to the current open Shift. Current `sales.operate` authority is required; a second Manager Approval is not. The approval already authorized the exact Refund, while confirmation attests that its approved money movement occurred.

The command appends the completion, writes `MANUAL_QR_REFUND_COMPLETED`, stores the result, and makes the completed amount visible to Check, Completed Sale, and Shift projections. It returns `200` with the same Refund result shape and derived state `COMPLETED`.

Exact replay returns the original result. Another request id after completion receives `REFUND_ALREADY_COMPLETED`.

---

## 10. Payment Void

`POST /api/v1/sales/payments/:payment_id/void`

The body carries `request_id`, reason, note, and Manager Approval credentials.

The original Payment, Check, Service Session, and original Payment Shift must exist. The Session must still be active, the Check must not be merged, and the original Shift must be the currently open Shift. This deliberately makes Payment Void a pre-close command. A closed Session needs an additive post-sale correction and replacement-Payment contract that Phase 6C does not define; a closed Shift additionally requires the separately deferred Post-Shift Payment Correction.

The Payment must have no prior Void and no Refund allocation, pending or completed. Void is always for the entire applied amount.

The transaction appends `payment_voids`, recomputes Check financials and settlement state, writes `PAYMENT_VOIDED`, writes `CHECK_REOPENED_AFTER_PAYMENT_VOID` when applicable, and returns `201` with the complete Service Session projection. It does not automatically create a replacement Payment; staff record one through the ordinary Cash or Manual QR command.

---

## 11. Locking And Concurrency

### 11.1 Common Lock Order

Correction commands pre-resolve ownership with non-locking reads when necessary, then acquire and revalidate rows in this order:

```text
1. Checks, ascending UUID
2. Service Sessions, ascending UUID
3. current or original Sales Shift
4. Payments and Charge Adjustments, each ascending UUID
5. Preparation source facts and Preparation Units, ascending UUID
```

This preserves ADR-030's Check-before-Session order for commands that rebuild the Service Session projection. Existing State Correction and Remake commands take Session before unit but never wait on a Check; therefore they cannot form a cycle with this protocol. Ordinary advance and Waste serialize on the unit row and do not later request a Check lock.

Submit remains the documented exception from ADR-031: it locks Session and Draft before Checks. Cancel or active Comp racing a later Submit on the same Session can therefore reach the same narrow AB-BA window as Payment versus Submit. Phase 6C does not add an unsafe in-transaction retry. PostgreSQL `40P01` is an accepted retryable outcome for this pairing only; rollback removes every business write and idempotency claim, and a client retry is a first attempt.

### 11.2 Required Races

- **Cancel versus advance or Waste.** Unit lock serialization permits exactly one lifecycle result.
- **Cancel versus Payment.** Both lock the Check before the Session. The loser recomputes against the winner's committed charge or receipt.
- **Cancel or active Comp versus Submit.** Either command commits first, or PostgreSQL aborts one transaction with `40P01` under ADR-031. No partial correction, Order, audit, or idempotency result survives the aborted side.
- **Cancel versus Split/Merge.** The Check lock serializes both. Once Cancellation creates a live adjustment, restructuring rejects with `CHECK_HAS_CHARGE_ADJUSTMENT`; if restructuring commits first, Cancellation re-resolves and revalidates the unit-to-allocation mapping.
- **Cancel versus closure.** Closure cannot pass while the source unit is queued. If Cancellation commits first, closure sees terminal work and any pending Refund; if closure owns the Session first, Cancellation revalidates and rejects the closed Session.
- **Comp versus closure.** Active Comp either creates its adjustment before closure, which then observes any pending Refund, or runs as a post-sale Comp after closure.
- **Two Comps for one Waste.** The Waste/source locks and unique constraint permit one.
- **Overlapping Refunds.** Payment and Adjustment locks prevent either refundable capacity from being allocated twice, including by pending Manual QR intents.
- **Refund versus Payment Void.** Both lock Check, Session, Shift, then Payment. Either the Void wins and Refund rejects a voided Payment, or the Refund allocation wins and Void rejects a refunded Payment.
- **Payment Void versus replacement Payment.** Check and Session locks serialize the two; the resulting projection satisfies one settlement invariant.

Unexpected database, audit, serialization, projection, or stored-result failures abort the entire outer transaction and idempotency claim. Expected unique violations are mapped only when the named constraint represents a documented concurrent domain race.

---

## 12. Projection Contracts

### 12.1 Check

Each Check adds:

- `base_charge_vnd`
- `total_voided_vnd`
- `total_refunded_vnd`
- `effective_received_vnd`
- `pending_refund_vnd`
- `charge_adjustments` as a non-null collection
- `refunds` as a non-null collection

Existing `charge_vnd` becomes the live adjusted charge. `total_applied_vnd` remains the immutable sum of original Payments for historical clarity; valid/effective receipt calculations subtract Voids and completed Refunds explicitly rather than making that field silently change meaning. `balance_vnd` is the customer amount still due.

Payment entries add nullable Void evidence and `remaining_refundable_vnd`. Refund entries expose method, amount, derived state, source allocations, initiator, approver, creation, and completion evidence. No credential field is projected.

### 12.2 Service Session Closure

`ClosureReadiness` adds:

- `AllRefundsResolved bool`
- `PendingRefundCheckIDs []uuid.UUID`

Eligibility requires the existing four conditions and no pending Refund. Rejection precedence is load-bearing:

```text
1. unsettled Checks
2. pending Refunds
3. unsubmitted work
4. missing Order
5. nonterminal Preparation Units
```

The new domain error is `PENDING_REFUND_FOR_CLOSURE`, mapped to `409`.

### 12.3 Completed Sale

The core Completed Sale snapshot remains the facts visible at closure. Phase 6C adds a non-null `post_sale_corrections` collection containing later Comp and Refund history. Adding history does not mutate the Check charge, original Payment, Preparation Unit, or Completed Sale row.

Pre-close Cancellation, Comp, Refund, and Void facts are included in the snapshot's Check/correction projections because they existed before closure. Later facts appear only in additive post-sale history.

Completed Sale loading uses a structural closure boundary, not occurrence timestamps. Every pre-close correction takes the Service Session lock and revalidates `ACTIVE`; closure takes the same lock, so one commits wholly before the other. A correction that observes `CLOSED` either rejects (Cancellation and Payment Void) or writes an explicit `POST_SALE` adjustment and Refund carrying `completed_sale_id` (Comp and Refund). Application and database clocks therefore do not decide snapshot membership.

Core Check/Payment/correction loaders include `LIVE_CHECK` adjustments and Refunds with null `completed_sale_id`, and exclude every `POST_SALE` adjustment and post-sale Refund allocation. Historical `remaining_refundable_vnd` likewise ignores allocations belonging to post-sale Refunds. A separate query loads post-sale facts by `completed_sale_id`, ordered by occurrence and id. The implementation may pass a structural snapshot mode into shared loaders or use dedicated Completed Sale queries, but it may not rebuild immutable core fields from unrestricted current tables. No JSON snapshot column is added.

Comp and Refund mutation responses use a `scope` discriminator. `LIVE_CHECK` carries the updated `service_session` and no Completed Sale; `POST_SALE` carries `completed_sale_id` plus additive correction history and no mutable Service Session projection. Exactly one branch is present.

### 12.4 Preparation Queue

The queue already retains `CANCELLED` units carrying unacknowledged alerts. Phase 6C makes that reserved branch reachable. Cancellation and Change alerts use the existing shape and ordering. Acknowledgment removes the terminal unit and alert from the active queue without changing charge, Refund, closure, or history.

The Preparation Queue continues exposing no prices, Charge Adjustments, Check balances, Payments, Refunds, Sales Shift money, Manager Approval credentials, or financial audit history.

### 12.5 Sales Shift

Expected Cash becomes:

```text
Opening Float
+ non-voided Cash Payments
- completed Cash Refunds
+ Pay Ins
- Pay Outs
```

The current Shift response adds these scalar fields:

- `cash_payment_vnd`: original Cash Payment applied amounts.
- `cash_payment_void_vnd`: Cash amounts removed by Payment Void.
- `cash_refund_vnd`: completed Cash Refunds.
- `manual_qr_payment_vnd`: original Manual QR Payment amounts.
- `manual_qr_payment_void_vnd`: Manual QR amounts removed by Payment Void.
- `manual_qr_refund_vnd`: completed Manual QR Refunds.
- `pending_manual_qr_refund_vnd`: approved Manual QR Refunds lacking completion.
- `pending_refund_vnd`: total unresolved live and post-sale money owed back from adjustments attributed to the Shift.
- `unresolved_post_sale_adjustment_vnd`: post-sale adjustment amount not yet covered by completed Refunds.

It also adds `refunds`, a non-null list of Refund summaries ordered by `(created_at, id)`. Each summary carries id, Check, optional Completed Sale, method, amount, derived state, creation/completion time, and no credentials. Manual QR net received is `manual_qr_payment_vnd - manual_qr_payment_void_vnd`; completed Refunds remain separately visible rather than silently changing received history.

`internal/shift` derives every term with its own sqlc queries and imports no Sales or Preparation package.

Because Shift Close is not implemented, Phase 6C reports pending Refund intents and unresolved post-sale correction amounts in the current-Shift read but does not add a Shift closure route or claim to enforce its future closure policy. A later Shift Close design must consume these authoritative fields rather than inventing another calculation.

---

## 13. Authorization And Security

Cancel/Change requires `sales.operate`, not `preparation.operate`: it is initiated by Cashier work and changes customer charge. Manager also has the capability. Barista alone cannot cancel commercial work.

Comp, Refund, and Payment Void require current initiator `sales.operate` plus one exact Manager Approval. Approval occurs before fingerprinting and idempotent replay, matching the Shift executor's established behavior. A disabled, demoted, or wrong-PIN approver denies an exact replay of an earlier success.

Manual QR Refund confirmation requires current `sales.operate` but no second approval. The approved amount and allocations cannot change during confirmation.

All ids are parsed UUIDs, enums use fixed allowlists, and sqlc parameterizes every query. No request value enters SQL syntax. PINs and login credentials never enter business records, responses, fingerprints, audit details, or logs.

Preparation's operational projection remains a least-privilege DTO so financial fields cannot leak through accidental JSON serialization.

---

## 14. Error Contract

Phase 6C introduces condition-based errors rather than operation-prefixed duplicates. At minimum:

- `CANCELLATION_SELECTION_INVALID`
- `CANCELLATION_SOURCE_NOT_QUEUED`
- `REPLACEMENT_ORDER_REQUIRED`
- `REPLACEMENT_ORDER_INVALID`
- `CHARGE_ADJUSTMENT_CONFLICT`
- `CHECK_HAS_CHARGE_ADJUSTMENT`
- `WASTE_NOT_FOUND`
- `COMP_SOURCE_NOT_CHARGED`
- `WASTE_ALREADY_COMPED`
- `REFUND_NOT_FOUND`
- `REFUND_ALLOCATION_INVALID`
- `REFUND_EXCEEDS_ADJUSTMENT_CAPACITY`
- `REFUND_EXCEEDS_PAYMENT_CAPACITY`
- `REFUND_METHOD_MISMATCH`
- `REFUND_ALREADY_COMPLETED`
- `PAYMENT_ALREADY_VOIDED`
- `PAYMENT_HAS_REFUND`
- `PAYMENT_VOID_SHIFT_CLOSED`
- `PENDING_REFUND_FOR_CLOSURE`

Mapping:

| Condition | HTTP status |
| --- | ---: |
| Malformed request, invalid enum/reason/note/allocation sums | `400` |
| Missing/locked/expired/revoked session | `401` |
| Missing capability or failed Manager Approval | `403` |
| Unknown unit, Waste, adjustment, Payment, Refund, Check, Order, or Completed Sale | `404` |
| Stale lifecycle, duplicate correction, exhausted capacity, pending obligation, closed source | `409` |
| Guarded monetary range failure caused by request size | `422` |
| Corrupt stored result, invariant mismatch, unknown constraint or infrastructure failure | `500` |

HTTP mapping inspects typed/sentinel errors and known PostgreSQL constraint names, never error strings.

---

## 15. Audit Contract

Phase 6C introduces:

- `PREPARATION_UNIT_CANCELLED`
- `CHECK_CHARGE_ADJUSTED`
- `SALES_COMP_RECORDED`
- `REFUND_RECORDED`
- `REFUND_COMPLETED`
- `MANUAL_QR_REFUND_COMPLETED`
- `PAYMENT_VOIDED`
- `CHECK_REOPENED_AFTER_PAYMENT_VOID`

Existing `PREPARATION_ALERT_CREATED`, `PREPARATION_ALERT_ACKNOWLEDGED`, and `CHECK_SETTLED` remain in use.

Audit details contain stable business ids, before/after financial meaning, reason/note where applicable, and initiator/approver identities through first-class columns or explicit ids. No event contains a PIN or PIN hash. Failed authorization writes denial evidence; a failed business transaction writes no success event.

---

## 16. Testing

### 16.1 Unit Tests

- Guarded base charge, adjusted charge, valid receipt, completed Refund, balance, and pending-Refund arithmetic.
- Settlement caused by Cancellation/Comp, including zero charge and simultaneous pending Refund.
- Reopening caused by Payment Void and the remaining-covered case.
- Reason allowlists, note trimming, 500-character bound, and mandatory `OTHER` note.
- Cancellation selection, kind/replacement pairing, request-order response, and stable UUID-sorted fingerprint.
- Refund allocation normalization, duplicate detection, equal-sum requirement, and method consistency.
- Closure precedence with unsettled and pending-Refund Checks failing together.
- Expected Cash with Cash Refund and Cash Payment Void terms.
- DTO serialization with non-null collections and no credentials.

### 16.2 Migration And PostgreSQL Integration Tests

- All kind/source/scope pair constraints and positive amount constraints.
- One Cancellation per unit, one Comp per Waste, one Void per Payment, one completion per Refund.
- The new `QUEUED -> CANCELLED` transition pair is accepted and every undeclared pair rejected.
- Stored Check charge equals original allocations less live adjustments.
- Refund and allocation sums agree; pending intents reserve capacity.
- Cash Refund always has a completion on successful command return; Manual QR Refund does not until confirmed.

Cancellation workflows:

1. Unpaid charged unit: charge decreases, no pending Refund, state/transition/fact/alert/audits all commit.
2. Fully paid unit: Check stays settled, pending Refund appears, closure rejects.
3. Partially paid open Check reduced to zero: Check settles with evidence and pending Refund as applicable.
4. Change accepts a valid later replacement Order and rejects missing, same, earlier, cross-Session, or unsubmitted replacement.
5. Remake Cancellation changes no allocation or Check charge.
6. Batch success preserves request order; one stale/missing/cross-Session unit rolls back every selected unit.
7. Alert acknowledgment removes the cancelled unit from the queue and changes no finance.
8. Split and Merge reject a Check carrying any live Charge Adjustment; racing Cancellation never strands an adjustment on a moved/deleted allocation.

Comp workflows:

1. Active charged Waste creates one live adjustment; duplicate Comp rejects.
2. Wasted Remake rejects as uncharged.
3. Pre-close Comp creates pending Refund when paid and blocks closure.
4. Closed-sale Comp creates post-sale correction without changing snapshot fields.

Refund workflows:

1. Cash Refund completes immediately, resolves all or part of pending Refund, and reduces Expected Cash.
2. Manual QR Refund remains pending, reserves capacities, blocks Service Session closure, appears in current-Shift reconciliation, then confirmation completes it.
3. Mixed-method source requires separate Refunds.
4. Allocation beyond either source Adjustment or source Payment rejects atomically.
5. Post-sale Refund links to Completed Sale and current Shift without changing the closed snapshot.
6. Pending and completed Refund replays create no duplicate intent, allocation, completion, or audit.
7. Completed Sale core fields, including Check totals, Payment refundable capacity, and pre-close correction collections, remain byte-equivalent before and after post-sale Comp/Refund; only `post_sale_corrections` changes.

Payment Void workflows:

1. Cash and Manual QR Payment Void append reversal facts and leave source Payments unchanged.
2. Void reopens a Check when valid coverage falls below charge and clears settlement evidence atomically.
3. Void leaves Check settled when other valid Payments still cover charge.
4. A refunded Payment, already-voided Payment, closed original Shift, merged Check, or closed Service Session rejects.
5. Cash Void removes the Payment term from Expected Cash; QR Void removes it from received totals.

### 16.3 Concurrency Tests

- Cancel versus advance.
- Cancel versus Waste.
- Cancel versus Payment.
- Cancel versus Split/Merge.
- Cancel versus Submit, including the ADR-031 retryable `40P01` outcome.
- Cancel versus Service Session closure.
- Comp versus closure, before and after closure wins.
- Active Comp versus Submit, including the ADR-031 retryable `40P01` outcome.
- Concurrent Comp of one Waste.
- Overlapping Refunds on one Adjustment.
- Overlapping Refunds on one Payment.
- Refund versus Payment Void.
- Payment Void versus replacement Payment.

Tests synchronize goroutines at the relevant lock boundary and run focused suites under `-race`. Accepted outcomes are explicit. Only the ADR-031 Submit pairing may return retryable `40P01`; an unresolved hang, negative balance, duplicate capacity, partial fact, or invariant failure is never accepted.

### 16.4 Authorization, Idempotency, HTTP, And Swagger

- Cashier/Manager can Cancel; Barista alone cannot.
- Comp, Refund, and Void require valid current Manager Approval; self-approval works.
- Disabled/demoted approver, wrong PIN, revoked initiator, and removed initiator capability deny first execution and replay.
- Credential rotation does not alter the fingerprint and no credential appears in persistence, response, audit, or logs.
- Exact replay returns stored results; changed allocation order normalizes to the same request; changed amount/source/reason conflicts.
- Route validation, status/error mapping, response envelopes, and Bearer Swagger annotations cover all operations.
- Preparation Queue responses contain none of the Phase 6C financial fields.

### 16.5 Regression Verification

The implementation must pass:

- `go build ./...`
- `go vet ./...`
- the configured linter
- the complete unit suite
- the complete PostgreSQL integration suite
- focused Preparation/Sales/Shift concurrency suites under `-race`

Existing Auth, Catalog, Tables, Shift, Sales 5A-5D, Preparation 6A, and Preparation 6B behavior remains green.

---

## 17. Decision Record Updates

Implementation records these accepted decisions in `spec/decisions.md`:

- **ADR-040: Cancellation is one cross-slice PostgreSQL consistency boundary.** Preparation owns the command, state transition, fact, and alert, and receives a narrow exception to write the live Charge Adjustment and Check charge atomically. No event or new orchestration package is introduced.
- **ADR-041: Charge reduction is append-only and has live versus post-sale scope.** Cancellation and active Comp update the denormalized live Check charge; post-sale Comp links to Completed Sale and never rewrites its snapshot.
- **ADR-042: Refund consumes two independently locked capacities.** Every Refund allocates against both source Charge Adjustments and original non-voided Payments; pending Manual QR intents reserve both before money moves.
- **ADR-043: Refund completion is an append-only fact.** Cash intent and completion commit together; Manual QR completion is appended after outbound confirmation, with no mutable Refund state column.
- **ADR-044: A Check remains settled while Refund is pending.** Check state records covered customer debt; pending Refund is a separate obligation that blocks Service Session closure, restoring ADR-029's omitted branch without adding a Check state.
- **ADR-045: Payment Void is whole, append-only, and open-original-Shift only.** It may reopen a Check but never edits the Payment; a closed Shift requires the deferred Post-Shift Payment Correction.
- **ADR-046: Expected Cash uses valid Cash Payments less completed Cash Refunds.** Cash Payment Voids remove their source term, pending Refunds do not move money, and `internal/shift` derives all terms through its own SQL.

These ADRs and Phase 6C completion status in `MIGRATE_PLAN.md` land with implementation and passing tests, not with this design document alone.

---

## 18. Acceptance Criteria

1. Cashier or Manager can atomically Cancel 1 through 50 queued units; Barista alone cannot.
2. Every successful Cancellation writes one terminal state, typed transition, typed fact, alert, audit evidence, and replayable outcome per unit.
3. Charged original units create one live Charge Adjustment for their immutable unit price; uncharged Remakes change no Check.
4. Change requires an already-submitted later replacement Order in the same active Service Session and never edits an existing Order Item.
5. Batch Cancellation is all-or-nothing and follows the common Check-before-Session lock protocol.
6. A charged Wasted standard unit may be Comped once with Manager Approval; a Wasted Remake cannot.
7. Pre-close Comp updates live charge; post-sale Comp links to Completed Sale without rewriting the snapshot.
8. A Refund allocates equal positive totals against explicit source Adjustments and non-voided source Payments of one method and Check.
9. Concurrent or pending Refunds cannot spend Adjustment or Payment refundable capacity twice.
10. Cash Refund completes immediately; Manual QR Refund remains pending until explicit confirmation appends completion evidence.
11. Payment Void preserves the source Payment, reverses it in full, requires both an active Service Session and original Shift open, and rejects any Payment with Refund allocation.
12. Payment Void reopens and clears settlement evidence only when remaining valid coverage leaves a positive balance.
13. Cancellation or Comp settles an open Check when corrected balance reaches zero, with complete evidence and audit.
14. A settled Check remains settled while carrying pending Refund and remains ineligible for split or merge.
15. Service Session closure rejects pending Refund after unsettled Checks and before work/preparation failures.
16. Current-Shift reconciliation exposes pending Refunds and unresolved post-sale corrections without claiming a Shift Close command.
17. Expected Cash includes non-voided Cash Payments and completed Cash Refunds, and excludes pending Refunds.
18. Service Session and Completed Sale projections expose complete correction history with non-null collections and no credentials; post-sale facts change only additive history and leave every as-of core field unchanged.
19. Preparation Queue exposes Cancellation/Change alerts and alert-retained Cancelled units with no financial leakage.
20. Every successful correction is actor-scoped, idempotent, auditable, append-only at its source facts, and atomic with its current-state effects.
21. Exact replay requires current initiator authority and, where applicable, current valid Manager Approval, while creating no duplicate work.
22. Unit, migration, PostgreSQL integration, HTTP, Swagger, failure-injection, concurrency, and full regression verification pass.
23. Split and Merge reject every Check carrying a live Charge Adjustment, including under a concurrent Cancellation race.
