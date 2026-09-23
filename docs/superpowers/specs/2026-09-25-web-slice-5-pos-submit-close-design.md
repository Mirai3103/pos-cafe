# Design Specification: Web Slice 5 — POS-c: Submit, Session Closure & Completed Sale (`./web`, Phase 11 UI)

- **Author:** Claude & Team
- **Date:** 2026-09-25
- **Status:** Draft, pending review
- **Phase:** Phase 11, UI half — see [`docs/backlog/phase-11-clients-over-lan-and-single-binary.md`](../../backlog/phase-11-clients-over-lan-and-single-binary.md)
- **Predecessors:**
  - [`2026-09-21-web-frontend-slice-sequence-design.md`](2026-09-21-web-frontend-slice-sequence-design.md) (slice sequence & definition of done)
  - [`2026-09-23-web-slice-3-pos-draft-design.md`](2026-09-23-web-slice-3-pos-draft-design.md) (sellable menu, Order Draft, session persistence)
  - [`2026-09-24-web-slice-4-pos-checkout-design.md`](2026-09-24-web-slice-4-pos-checkout-design.md) (phase derivation, commit → pay sequence, the accepted dead end)
- **Visual authority:** [`design-system/pos-cafe/DESIGN.md`](../../../design-system/pos-cafe/DESIGN.md)
- **Domain authority:** [`CONTEXT.md`](../../../CONTEXT.md), [`spec/decisions.md`](../../../spec/decisions.md), `internal/sales/` (Phase 05D and the closure work)

---

## 1. Purpose & Scope

Slice 5 delivers the last third of the Cashier Terminal: **POS-c (Submit,
Session Closure & Completed Sale)**. It produces the data that Slice 6 (the
Preparation Queue) and Slice 8 (History) display.

Slice 4 ends on a deliberate dead end (its section 2). Once a takeaway Check is
settled, the Service Session can accept no further rounds and cannot close,
because Submit had not shipped. "Gửi bếp" is disabled with the caption "Mở ở
Slice 5", and "Khách tiếp theo" leaves the settled session behind, `ACTIVE` and
inert. This slice removes the dead end and cleans up those stranded sessions.

Three Go endpoints carry the slice, plus one read the slice depends on:

- `POST /sales/service-sessions/{id}/submit` — turns the committed draft into an
  Order, its Order Items, and one Preparation Unit per unit of quantity. A
  takeaway Session requires every Check settled first.
- `POST /sales/service-sessions/{id}/close` — freezes the Session into an
  immutable Completed Sale. Idempotent: closing a closed Session returns its
  existing Completed Sale.
- `GET /sales/service-sessions/{id}/completed-sale` — reads that Completed Sale
  back.
- `GET /sales/service-sessions` — every `ACTIVE` Service Session with its full
  projection; the cashier's open-tabs view.

### 1.1 In Scope

1. **Phase derivation, extended (`utils/phase.ts`)**: `SETTLED` splits into
   `AWAITING_SUBMIT`, `IN_PREPARATION`, and `READY_TO_CLOSE`. Section 3.
2. **Automatic submit after payment (`api/use-checkout.ts`)**: the checkout
   sequence becomes commit → pay → submit. Section 4.
3. **Session closure (`api/use-close-session.ts`)**: the "Hoàn tất" action, and
   its idempotent retry. Section 5.
4. **Pending orders drawer (`components/pending-orders-drawer.tsx`,
   `components/pending-order-row.tsx`)**: every active takeaway Session, its
   status, and preparation progress. Tapping a row reopens it. Section 6.
5. **Completed Sale dialog (`components/completed-sale-dialog.tsx`)**: the
   read-only summary shown after closure. Section 7.
6. **Check panel, extended (`components/check-panel.tsx`)**: the new phases and
   their actions.
7. **Vietnamese error mapping** for the submit and closure error codes.
8. **UAT helper script** that advances Preparation Units to `FULFILLED` until
   Slice 6 ships a Preparation Queue. Section 10.
9. **Unit tests** under `bun test`, per the sequence spec's definition of done.

### 1.2 Out of Scope (Deliberate Omissions & Recorded Cuts)

- **Preparation Queue (Slice 6)**: the POS never moves a Preparation Unit. Units
  advance only from the KDS, so closure cannot happen on the POS alone. Section 2.
- **Dine-in Sessions (Slice 7)**: the drawer filters them out. Dine-in submit
  (before payment) and table release are Slice 7's work.
- **Abandoned Session cancellation**: the backend has no endpoint to cancel a
  Service Session. An empty or abandoned draft stays `ACTIVE` and stays in the
  drawer indefinitely. Recorded here; not solved.
- **History (Slice 8)**: the Completed Sale is shown once, after closure. Browsing
  past Completed Sales belongs to History.
- **Manual QR / VietQR, partial payment, split and merge, payment void,
  refunds**: unchanged from Slice 4's cuts.
- **Receipt printing**: out of scope for all nine slices per the sequence spec.

---

## 2. The Kitchen Dependency

A Service Session closes only when, in this order of refusal, every Check is
settled, every committed item has been submitted, the Session carries at least
one Order, and **every Preparation Unit is terminal** (`FULFILLED`, `CANCELLED`,
or `WASTED`). Submit creates units in `QUEUED`, and only the Preparation Queue
advances them.

So on its own, Slice 5 can submit but can never close through the UI. Three
options were weighed:

- **A pending-orders list with closure gated on the kitchen** — ship submit,
  closure, and the Completed Sale; closure becomes reachable in the UI once
  Slice 6 lands.
- **Cashier-side fulfilment on the POS** — a "Giao món" action calling
  `/preparation/units/advance-many`. This closes the loop now, but moves KDS
  behaviour into the POS and takes over Slice 6's scope.
- **Reordering the slices** — ship submit only, build the KDS, then add closure.

**Decision: the pending-orders list, with closure gated on the kitchen.** The
POS reflects preparation progress and enables "Hoàn tất" once every unit is
terminal. Until Slice 6 ships, UAT advances units with a helper script
(section 10). The script is scaffolding. It is not a product feature.

---

## 3. Phase Derivation

### 3.1 The principle, unchanged

There is still no client-held state machine. Every Sales endpoint returns the
whole `ServiceSessionResponse`, and `derivePosPhase` is the only source of
phase. Reloading, a second tab, or a half-finished sequence all resolve to the
same phase.

### 3.2 The phases

```ts
export type PosPhase =
  | "NO_SESSION"
  | "DRAFTING"
  | "AWAITING_PAYMENT"
  | "AWAITING_SUBMIT"
  | "IN_PREPARATION"
  | "READY_TO_CLOSE";
```

| Phase | Condition, evaluated in order | Right-hand panel |
| :--- | :--- | :--- |
| `NO_SESSION` | no session | empty Draft panel |
| `DRAFTING` | `draft.state === "EDITABLE"` | Draft panel |
| `AWAITING_PAYMENT` | an `OPEN` live Check exists | Check panel, "Thu tiền" |
| `AWAITING_SUBMIT` | any live Check allocation has `submitted !== true` | Check panel, "Gửi bếp" |
| `IN_PREPARATION` | any Preparation Unit is not terminal | Check panel, progress, "Hoàn tất" disabled |
| `READY_TO_CLOSE` | otherwise | Check panel, "Hoàn tất" |

`SETTLED` is removed. Every place that read it now reads the three phases that
replace it.

### 3.3 New helpers in `phase.ts`

- `isTerminalUnit(unit)` — `state` is `FULFILLED`, `CANCELLED`, or `WASTED`.
- `preparationProgress(session)` — `{ done, total }`, counting terminal units
  over all units. Remake units count like any other unit: a wasted unit is
  terminal, and its remake is one more unit that must finish.
- `hasUnsubmittedWork(session)` — any live Check allocation with
  `submitted !== true`.

### 3.4 Edge cases

- **A settled session with no units and every allocation submitted.** The domain
  cannot produce this, because submit always creates units. Answering
  `READY_TO_CLOSE` is safe here: the close call refuses with
  `ORDER_REQUIRED_FOR_CLOSURE` if the state really is wrong.
- **A session that is no longer `ACTIVE`.** `derivePosPhase` is never asked. The
  session hook routes it to the Completed Sale path (section 7.3).
- **The zero-live-checks guard** from Slice 4 stays as it is.

---

## 4. Submit

### 4.1 The three-request sequence

`useCheckoutFlow.confirmPayment` extends Slice 4's commit → pay with a third
step:

1. `commit`, if the phase is `DRAFTING`. This is unchanged.
2. `pay cash` against the open Check. This is unchanged.
3. `submit`, as soon as the pay response shows no open Check and unsubmitted
   work remains.

Every step writes the returned projection into the react-query cache, so the
phase moves forward from server data alone.

### 4.2 The result screen

The payment dialog's result screen still shows the change the server computed.
Under it, one line reports the submit outcome:

- Success: "Đã gửi bếp" with a check icon.
- Failure: a warning, "Đã thu tiền nhưng chưa gửi được bếp", followed by the
  mapped error message.

**The money is recorded either way.** A submit failure never reads as a payment
failure.

### 4.3 Retry and idempotency

- The submit `request_id` is generated once per submit attempt sequence and
  kept in a ref. "Gửi bếp lại" reuses it. The ref is cleared on success, or when
  the active session changes.
- `NOTHING_TO_SUBMIT` counts as success: another tab, or a previous attempt
  whose response was lost, has already submitted. The hook refetches the session
  and reports success.
- `CHECK_NOT_SETTLED_FOR_SUBMISSION` refetches the session, and the phase falls
  back to `AWAITING_PAYMENT` on its own.

### 4.4 The Check panel in `AWAITING_SUBMIT`

The panel shows the header badge "Chờ gửi bếp", the settled totals exactly as in
Slice 4, and a primary "Gửi bếp (F9)" button that runs the submit step alone.
"Khách tiếp theo" stays available as a secondary action. The session then waits
in the drawer, labelled "Chờ gửi bếp", so nothing is lost.

This is also how the sessions stranded by Slice 4 are cleaned up: reopen from
the drawer, then press "Gửi bếp".

### 4.5 "Khách tiếp theo"

`nextCustomer` keeps its Slice 4 behaviour: it clears the request-id refs
(now including submit) and the active-session pointer, and Slice 3's lazy open
on the first added item starts the next session. It is offered in every phase
from `AWAITING_SUBMIT` onward. The session left behind is always reachable from
the drawer.

---

## 5. Session Closure

### 5.1 `useCloseSession(sessionId)`

- Calls `POST /sales/service-sessions/{id}/close` with a `request_id` held in a
  ref. A retry reuses it; success or a session change clears it.
- On `201`, the hook stores the returned `CompletedSaleResponse` for the dialog
  (section 7), invalidates the active-sessions query, and removes the session's
  query from the cache.
- Closure is idempotent on the server, so a retry after a lost response returns
  the same Completed Sale.

### 5.2 The Check panel in `IN_PREPARATION` and `READY_TO_CLOSE`

- The header badge reads "Đang pha chế" or "Sẵn sàng hoàn tất".
- A progress row reads "Đã xong x/y món", with a thin bar.
- The primary button "Hoàn tất (F9)" is enabled only in `READY_TO_CLOSE`. In
  `IN_PREPARATION` it is disabled and captioned "Chờ bếp hoàn tất".
- "Khách tiếp theo" is secondary in both phases.
- The active session query refetches every 5 s while the phase is
  `IN_PREPARATION`, so the button enables without the cashier reloading.

### 5.3 F9

| Phase | F9 |
| :--- | :--- |
| `NO_SESSION`, `DRAFTING`, `AWAITING_PAYMENT` | open payment (unchanged) |
| `AWAITING_SUBMIT` | submit |
| `IN_PREPARATION` | next customer |
| `READY_TO_CLOSE` | close |
| Completed Sale dialog open | dismiss the dialog |

The Slice 4 guard stays: F9 does nothing while the item picker or the payment
dialog is open.

---

## 6. The Pending Orders Drawer

### 6.1 Data

`useActiveSessions()` in `api/use-pos.ts` wraps `GET /sales/service-sessions` and
keeps only `service_mode === "TAKEAWAY"`. There is no SSE or websocket in the
backend, so the drawer polls:

- every **5 s** while the drawer is open;
- every **15 s** while it is closed, only to keep the badge current;
- immediately after this terminal pays, submits, or closes (query invalidation).

### 6.2 Entry point

A button at the top of the menu zone reads `Đơn đang chờ (N)`. A green count dot
shows how many sessions are in `READY_TO_CLOSE`. The hotkey is **F4**. The
drawer is the shadcn `Sheet`, sliding in from the right.

### 6.3 Rows (`pending-order-row.tsx`)

Each row shows:

- `#service_number`, and the age "x phút trước" from `created_at`;
- a status chip derived from `derivePosPhase`: Đang soạn, Chờ thu tiền, Chờ gửi
  bếp, Đang pha chế, Sẵn sàng hoàn tất;
- an item summary (the first two names, then "+n món"), and the total;
- for `IN_PREPARATION` and `READY_TO_CLOSE`, "x/y món xong" with a thin bar;
- a highlight on the row that is the current active session.

### 6.4 Order

`READY_TO_CLOSE` first, because it is work the cashier can finish now. Then
`AWAITING_SUBMIT`, because money has been taken but the kitchen has not heard.
Every other row follows by `created_at`, oldest first.

### 6.5 Tapping a row reopens it

`usePosSession` gains `switchSession(id)`. It writes the pointer the same way
`ensureSessionId` does. The drawer closes, and the right-hand panel renders the
reopened session's phase. The drawer has no action buttons of its own, so every
action has exactly one path.

Switching away from a session with a draft in progress needs no confirmation.
That draft stays `ACTIVE` on the server and reappears in the drawer as "Đang
soạn".

### 6.6 States

- Loading: skeleton rows.
- Empty: "Không có đơn nào đang chờ".
- Error: the mapped message from the error envelope, with a "Thử lại" button.

---

## 7. The Completed Sale Dialog

### 7.1 Content (`completed-sale-dialog.tsx`, read-only)

- Title "Đơn #012 đã hoàn tất", with `completed_at` and
  `completed_by_display_name`.
- The items, from the Completed Sale Checks' `allocations`.
- The totals: `charge_vnd`, `effective_received_vnd`, and each payment (cash
  tendered, change given).
- A preparation summary, for example "3 món đã giao · 1 hủy", counted from
  `preparation_units` by state.

Every figure is the server's frozen record. Nothing is recomputed.

### 7.2 Dismissal

"Xong" (F9 or Enter) clears the active-session pointer. The POS returns to an
empty Draft panel, ready for the next customer.

### 7.3 Recovery through `GET /completed-sale`

Today `usePosSession` silently drops the pointer when the session is no longer
`ACTIVE`, for example after another tab closed it. Slice 5 changes this: when the
fetched session's state is `CLOSED`, the hook reads
`GET /sales/service-sessions/{id}/completed-sale` and shows the same dialog. If
that read fails, or answers `404`, the pointer is dropped as before.

---

## 8. Error Handling

New entries in `web/src/lib/error-messages.ts`:

| Code | Vietnamese message | Behaviour |
| :--- | :--- | :--- |
| `NOTHING_TO_SUBMIT` | — | Treated as success; refetch |
| `CHECK_NOT_SETTLED_FOR_SUBMISSION` | "Đơn chưa thu đủ tiền, chưa thể gửi bếp." | Refetch; phase returns to payment |
| `UNFULFILLED_PREPARATION_FOR_CLOSURE` | "Bếp chưa hoàn tất tất cả món." | Refetch |
| `UNSUBMITTED_WORK_FOR_CLOSURE` | "Còn món chưa gửi bếp." | Refetch |
| `CHECK_NOT_SETTLED_FOR_CLOSURE` | "Đơn chưa thu đủ tiền." | Refetch |
| `ORDER_REQUIRED_FOR_CLOSURE` | "Đơn chưa có món nào được gửi bếp." | Refetch |
| `PENDING_REFUND_FOR_CLOSURE` | "Đơn còn khoản hoàn tiền chưa xử lý. Vui lòng báo quản lý." | Refetch |
| `COMPLETED_SALE_NOT_FOUND` | "Không tìm thấy hóa đơn hoàn tất." | Drop pointer |
| `SERVICE_SESSION_ALREADY_CLOSED` | existing | Go to section 7.3 |

**The rule: every 409 refetches the session.** The phase follows the server, so
a stale poll corrects itself on the next render.

---

## 9. Component Structure (`web/src/features/pos/`)

| File | Change |
| :--- | :--- |
| `utils/phase.ts` | New phases and helpers (section 3) |
| `api/use-pos.ts` | `useActiveSessions`, `useSubmitOrder`, `useCompletedSale` |
| `api/use-close-session.ts` | New (section 5.1) |
| `api/use-checkout.ts` | Third step, submit (section 4) |
| `api/use-pos-session.ts` | `switchSession`, closed-session recovery |
| `components/check-panel.tsx` | New phases and actions |
| `components/payment-dialog.tsx` | Submit outcome line on the result screen |
| `components/pending-orders-drawer.tsx` | New |
| `components/pending-order-row.tsx` | New |
| `components/completed-sale-dialog.tsx` | New |
| `components/pos-view.tsx` | Drawer entry point, F4, F9 by phase, dialog wiring |

`pos-view.tsx` is 390 lines. The new F9 and close wiring goes into a
`usePosHotkeys` hook, so the view does not grow.

---

## 10. Testing & UAT

### 10.1 Unit tests (`bun test`)

- `utils/phase.test.ts`: every phase; `WASTED` and `CANCELLED` count as
  terminal; a remake unit keeps the session `IN_PREPARATION`; the no-units edge
  case.
- `api/use-checkout.test.ts`: commit → pay → submit; pay succeeds and submit
  fails; a retry reuses the submit `request_id`; `NOTHING_TO_SUBMIT` counts as
  success.
- `api/use-close-session.test.ts`: success stores the Completed Sale; a retry
  reuses the `request_id`; a 409 refetches.
- Render tests: `check-panel` (the three new phases); `pending-orders-drawer`
  (order, badge count, filtering out dine-in, row tap switches the session);
  `completed-sale-dialog` (totals and preparation summary).
- `pos-view.test.tsx`: F9 by phase; F4 opens the drawer.

### 10.2 UAT helper

`scripts/uat-advance-units.ts` (bun) takes a service number or session id. It
reads the session's Preparation Units and calls
`POST /preparation/units/advance-many` until each unit reaches `FULFILLED`. It
signs in with credentials from environment variables, as `scripts/dev-seed.ts`
does. The script is deleted once Slice 6 ships.

---

## 11. Definition of Done & UAT Handover

### 11.1 Verification checklist

1. `bun test` passes; `bun run lint` and `tsc -b` are clean.
2. `derivePosPhase` is still the only source of phase; `SETTLED` no longer exists.
3. The submit and close `request_id` values are stable across retries.
4. No Completed Sale figure is computed on the client.
5. `pos-view.tsx` does not grow.
6. One pull request for the slice.

### 11.2 UAT gate handover

Handed over as `uat-handover.md`, per Slice 1's precedent.

**Happy path:** open a Sales Shift → add items → F9 → pay cash → see "Đã gửi bếp"
→ "Khách tiếp theo" → F4 shows the order as "Đang pha chế 0/y" → run the UAT
script → the order moves to the top as "Sẵn sàng hoàn tất" → tap it → F9 "Hoàn
tất" → the Completed Sale dialog shows the correct totals → "Xong".

**Unhappy paths:**

1. Cut the network after payment, before submit. The result screen warns "chưa
   gửi được bếp". Restore the network and press "Gửi bếp lại": exactly one Order
   is created.
2. Reopen a session stranded by Slice 4 from the drawer, then "Gửi bếp". It
   submits.
3. Press "Hoàn tất" while one unit is still preparing, forcing the request
   against a stale poll. The Vietnamese message appears, and the phase returns to
   `IN_PREPARATION`.
4. Close the same session from two tabs. The second tab shows the same Completed
   Sale, with no error.
5. Reload during `IN_PREPARATION`. The progress comes back exactly as it was.
