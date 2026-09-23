# Design Specification: Web Slice 6 — KDS, the Preparation Queue (`./web`, Phase 11 UI)

- **Author:** Claude & Team
- **Date:** 2026-09-26
- **Status:** Draft, pending review
- **Phase:** Phase 11, UI half — see [`docs/backlog/phase-11-clients-over-lan-and-single-binary.md`](../../backlog/phase-11-clients-over-lan-and-single-binary.md)
- **Predecessors:**
  - [`2026-09-21-web-frontend-slice-sequence-design.md`](2026-09-21-web-frontend-slice-sequence-design.md) (slice sequence & definition of done)
  - [`2026-09-22-web-slice-2-shift-management-design.md`](2026-09-22-web-slice-2-shift-management-design.md) (request ID, Manager Approval)
  - [`2026-09-25-web-slice-5-pos-submit-close.md`](2026-09-25-web-slice-5-pos-submit-close.md) — plan (the source of the data this slice displays)
- **Visual authority:** [`design-system/pos-cafe/DESIGN.md`](../../../design-system/pos-cafe/DESIGN.md), [`design-system/pos-cafe/pages/kds.html`](../../../design-system/pos-cafe/pages/kds.html)
- **Domain authority:** [`CONTEXT.md`](../../../CONTEXT.md), [`spec/decisions.md`](../../../spec/decisions.md), [`docs/domain-rationale/07-define-preparation-queue.md`](../../domain-rationale/07-define-preparation-queue.md), `internal/preparation/` (Phases 6A–6C)

---

## 1. Purpose & Scope

Slice 6 replaces the `kds.tsx` placeholder with the real Kitchen Display
System: the shared, always-on screen a barista or preparation station works
from. It has nothing to show until Slice 5 submits an Order, which is why it
sits sixth in the delivery sequence. It is also the last slice required by
Phase 11's UI acceptance criterion (Sign-in, Cashier, Preparation Queue).

Two Go endpoint groups carry the whole slice:

- `GET /preparation/queue` — one consistent read of every active Preparation
  Unit, the active Preparation Alerts, and the last 50 Waste/Remake entries.
- `POST /preparation/units/*`, `POST /preparation/wastes/{id}/remake`,
  `POST /preparation/alerts/{id}/acknowledge` — the full unit lifecycle.

### 1.1 In Scope

1. **Queue read** (`features/kds/api/use-preparation-queue.ts`): polls
   `GET /preparation/queue` every 5 seconds — this screen is always "active
   work," so polling is unconditional, unlike the cashier's phase-gated poll.
2. **Board derivation** (`features/kds/lib/board.ts`): pure functions turning
   `units[]` into three columns (Queued, In Preparation, Ready) grouped into
   ticket cards by `service_number`, plus elapsed-time formatting and the
   distinct-category list for the filter bar.
3. **Ticket cards** (`features/kds/components/ticket-card.tsx`): one card per
   `service_number` per column, listing its unit lines with checkboxes, a
   primary "advance the whole ticket" button, and a secondary "advance
   selected" action once any line is checked.
4. **Waste and Remake** (`waste-dialog.tsx`, `corrections-log.tsx`): mark a
   unit Wasted with a reason and note; remake a Wasted entry into a new linked
   unit from the corrections log.
5. **Correct-state ("Hoàn tác")**: a Manager-PIN-gated undo of the barista's
   own last transition, reachable only from a unit already visible in the
   In Preparation or Ready column (see section 4).
6. **Alerts panel**: active Preparation Alerts (cancellation, change, waste),
   oldest first, each with an Acknowledge action.
7. **Category filter bar**: chips built from the categories actually present
   in the live queue, replacing the prototype's hardcoded Bar/Bakery stations.
8. **Vietnamese error mapping** for the preparation error codes.
9. **Unit tests** under `bun test`, per the sequence spec's definition of done.

### 1.2 Out of Scope (Deliberate Omissions & Recorded Cuts)

- **Shortage reporting and label reprinting** (`Báo thiếu`, `In lại tem` in
  the prototype): no stock-availability or label-printing endpoint exists
  anywhere in the backend. Both buttons render **disabled** with a "Chưa hỗ
  trợ" tooltip, preserving the prototype's layout without pretending either
  works.
- **Receipt/label printing**: out of scope for all nine slices per the
  sequence spec.
- **Realtime push**: no WebSocket. Polling only, per the sequence spec's
  explicit non-goal — measured only if 5s polling proves insufficient.
- **Cancel** (`POST /preparation/units/cancel`): per the domain doc, a
  post-submission change or cancellation is driven from the sales/POS side
  (cancel the old unit, resubmit through a new Order), not from the KDS
  screen. No Cancel affordance ships here.
- **Bulk Correct-state**: the backend allows correcting up to
  `MaxCorrectionUnits` units in one all-or-nothing command, but this slice
  only exposes the single-unit "undo my last tap" button described in
  section 4. A multi-unit correction picker is not built.
- **Correcting a Fulfilled unit**: `FULFILLED → READY` is a legal correction
  target, but a Fulfilled unit leaves the queue response the instant it gets
  there, so this screen has no selection surface that can ever reach it. Out
  of scope; would need a separate lookup flow.
- **Station concept**: the backend has no "station" (bar/bakery) field. The
  category filter is data-driven from `category_name`, not a hardcoded list.

---

## 2. Domain recap (from `docs/domain-rationale/07-define-preparation-queue.md`)

- Lifecycle: `QUEUED → IN_PREPARATION → READY → FULFILLED`. A Queued unit may
  be Cancelled; an In Preparation or Ready unit may be Wasted. A Remake is a
  new, linked unit, not a state on the Wasted one.
- The queue is shared — no exclusive claim by one barista.
- Units reach Ready/Fulfilled independently: **partial handoff is a domain
  rule, not a nice-to-have**, which is why the ticket-card action model
  supports per-line selection (section 3) rather than one whole-ticket
  button.
- `Ready` means done, not yet handed over. `Fulfilled` is recorded only when
  staff confirm handoff.
- Only an active Remake carries priority; there is no general rush flag or
  SLA. The screen shows elapsed Queued and elapsed In-Preparation time
  instead.
- Fulfilled units leave the active queue immediately. Cancelled/Wasted units
  stay only until their Alert is acknowledged.
- `internal/preparation/queue.go`'s `ActiveQueueHandler` already returns
  `units` sorted by priority lane, then `queued_at`, then `id` — the frontend
  groups, it does not re-sort.

---

## 3. Screen structure

```
features/kds/
  components/
    kds-view.tsx              orchestrator: queue fetch, filter/selection state, dialogs
    queue-column.tsx          one of three columns; renders its TicketCards
    ticket-card.tsx           one card per service_number within a column
    category-filter-bar.tsx   chips from distinct category_name in the live queue
    alerts-panel.tsx          active alerts, oldest first, Acknowledge button
    corrections-log.tsx       collapsed-by-default strip of the last 50 Waste/Remake entries
    waste-dialog.tsx          reason + note form; single- or multi-unit
    correct-state-dialog.tsx  confirm + reason, then promptApproval() for manager_pin
  api/
    use-preparation-queue.ts   useGetPreparationQueue, unwrap, 5s poll
    use-preparation-actions.ts advance / bulk-advance / waste / remake / correct-state / acknowledge
  lib/
    board.ts                   pure grouping + elapsed-time + category-list functions
```

`kds-view.tsx` owns no rendering beyond composing the above — column and
ticket rendering, both dialogs, and the grouping logic each live in their own
file, so no file is expected to approach the ~300-line split threshold the
sequence spec sets.

### 3.1 Ticket cards and partial handoff

Each `TicketCard` groups every visible unit sharing one `service_number`
within its column, listing each unit line (item, size, modifiers, note,
`unit_number`). It renders:

- A primary button matching its column's action (see the table in section 4)
  that advances **every unit currently in the card** via
  `postPreparationUnitsAdvanceMany` — the common case, one tap per ticket.
- Checkboxes per line. Checking any line switches the primary button to
  "advance selected," scoped to just the checked unit ids — the mechanism
  that satisfies the domain's independent-handoff rule.
- A per-line overflow menu for Waste (opens `waste-dialog.tsx`) and, on an
  In Preparation or Ready line, "Hoàn tác" (opens `correct-state-dialog.tsx`).

A Remake unit (linked via `remake_of_preparation_unit_id`, `priority ===
"REMAKE"`) is visually flagged (a red "PHA LẠI" badge, matching the
prototype's remake badge) and, per the domain doc, is the only thing that
jumps the FIFO order — which it already does, because the backend's own
`queued_at`/priority-lane ordering places it correctly; the frontend applies
no separate priority logic.

---

## 4. Actions

| Action | Endpoint | Trigger | Target state | Manager PIN |
| :--- | :--- | :--- | :--- | :--- |
| Start preparing | `postPreparationUnitsAdvanceMany` (single-unit `advance` when exactly one id) | Queued column, card or selection | `IN_PREPARATION` | No |
| Mark ready | same | In Preparation column, card or selection | `READY` | No |
| Mark handed off | same | Ready column, card or selection | `FULFILLED` | No |
| Waste | `postPreparationUnitsUnitIdWaste` | Per-line overflow menu → `waste-dialog.tsx` (reason + note) | `WASTED` | No |
| Remake | `postPreparationWastesWasteIdRemake` | "Pha lại" on a Waste entry in `corrections-log.tsx` | new linked unit | No |
| Undo last transition | `postPreparationUnitsCorrectState` | "Hoàn tác" on an In Preparation or Ready line | one step back | **Yes** |
| Acknowledge alert | `postPreparationAlertsAlertIdAcknowledge` | Button on each alert row | — | No |

Every mutation is wrapped through `withRequestId()` (`lib/command.ts`),
generated once per tap and reused across retries — never regenerated inside
the calling function.

**Correct-state is intentionally narrow.** The backend reverses exactly one
step (`IN_PREPARATION→QUEUED`, `READY→IN_PREPARATION`,
`FULFILLED→READY`) per `RequiredPriorState` in `internal/preparation/domain.go`.
Because a Fulfilled unit is no longer present in the queue response, the only
two corrections reachable from this screen are the two "undo my last tap"
buttons attached directly to a still-visible unit. There is no standalone
correction picker. The flow mirrors `close-shift-dialog.tsx`:
`useManagerApprovalStore().promptApproval({title, description})` resolves to
`{approverLoginCode, managerPin}`; `managerPin` is sent as `manager_pin`,
`approverLoginCode` is not sent to the backend (the PIN alone authorizes).

Bulk-advance failures are partial per the domain doc ("an invalid unit
remains unchanged without corrupting valid units") — the response's per-unit
outcome list drives which lines show a failure state after the call, rather
than the whole card either succeeding or failing together.

---

## 5. Alerts and corrections log

- **`alerts-panel.tsx`** renders above the three columns whenever
  `alerts.length > 0`. This is the one element that must never be missed, so
  it is not collapsible. Each row shows `kind` (Vietnamese label: Hủy / Thay
  đổi / Hủy do lỗi), `item_name`, `unit_number`, `service_number`, `reason`,
  and an Acknowledge button.
- **`corrections-log.tsx`** is collapsed by default, showing the response's
  `corrections[]` (capped at 50, newest first) — a lightweight "what happened
  recently" strip. A `WASTE` entry with no corresponding active Remake shows
  the "Pha lại" action described above; a `REMAKE` entry is display-only.

---

## 6. Category filter

`board.ts` derives the filter chip list from the distinct `category_name`
values present in the current `units[]` — never a hardcoded station list.
"Tất cả" (All) is always first and is the default selection. Filtering is a
pure client-side narrowing of the already-fetched queue; it triggers no
additional request.

---

## 7. Polling

Reuses the existing convention from `features/pos/api/use-pos.ts`
(`IN_PREPARATION_POLL_MS = 5_000`) — this screen polls unconditionally at the
same interval, since it has no idle state to gate on the way the cashier's
per-session poll does.

---

## 8. Testing strategy

Same bar as every prior slice — pure logic and store transitions, no
integration or end-to-end tests:

- `board.ts`: grouping `units[]` into columns and ticket cards, elapsed-time
  formatting, category-list derivation, remake-badge detection.
- `use-preparation-actions.ts`: thin hook smoke tests, matching the sparse
  pattern already established for `useCloseFlow`/`useCheckoutFlow`.
- No new tests for `ManagerApprovalDialog` itself — its own store already has
  coverage; this slice only adds one more caller.

---

## 9. Definition of done

Identical to every prior slice (sequence spec §3):

1. Real API only — no mock data in the shipped path.
2. Loading, empty, and error states from the real envelope.
3. Guarded by `capabilities` — `requireCapability("preparation.operate")` is
   already wired on `/_app/kds`.
4. Every mutation carries a `request_id` generated per user intent.
5. Follows `design-system/pos-cafe/pages/kds.html`, with the cuts in
   section 1.2 recorded.
6. Basic tests only (section 8).
7. Ends at a UAT gate — implementation hands over a script, the operator
   confirms, completion is never self-certified.
8. One pull request.
