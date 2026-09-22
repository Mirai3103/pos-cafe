# Design Specification: Web Slice 4 — POS-b: Commit, Check & Cash Payment (`./web`, Phase 11 UI)

- **Author:** Claude & Team
- **Date:** 2026-09-24
- **Status:** Draft, pending review
- **Phase:** Phase 11, UI half — see [`docs/backlog/phase-11-clients-over-lan-and-single-binary.md`](../../backlog/phase-11-clients-over-lan-and-single-binary.md)
- **Predecessors:**
  - [`2026-09-21-web-frontend-slice-sequence-design.md`](2026-09-21-web-frontend-slice-sequence-design.md) (slice sequence & definition of done)
  - [`2026-09-22-web-slice-2-shift-management-design.md`](2026-09-22-web-slice-2-shift-management-design.md) (request ID, manager approval, shift state)
  - [`2026-09-23-web-slice-3-pos-draft-design.md`](2026-09-23-web-slice-3-pos-draft-design.md) (sellable menu, Order Draft, session persistence)
- **Visual authority:** [`design-system/pos-cafe/DESIGN.md`](../../../design-system/pos-cafe/DESIGN.md)
- **Domain authority:** [`CONTEXT.md`](../../../CONTEXT.md), [`spec/decisions.md`](../../../spec/decisions.md), `internal/sales/` (Phases 05B and 05C)

---

## 1. Purpose & Scope

Slice 4 delivers the second third of the Cashier Terminal: **POS-b (Commit, Check
& Cash Payment)**. It is the money path, and it closes the cash takeaway tracer
bullet named in Phase 11.

Slice 3 left the Order Bill able to price a draft but unable to take money: the
"Thanh toán (F9)" button ships disabled with the caption "Mở ở Slice 4". This
slice makes that button work. A cashier assembles a draft, presses F9, enters
the cash the customer handed over, and the screen tells them the change to give
back.

Two Go endpoints carry the whole slice:

- `POST /sales/service-sessions/{id}/draft/commit` — revalidates the draft,
  freezes prices into immutable Committed Items, and charges a Check.
- `POST /sales/checks/{check_id}/payments/cash` — applies cash to the Check and
  settles it in the same transaction when the balance reaches zero.

### 1.1 In Scope

1. **POS phase derivation (`web/src/features/pos/utils/phase.ts`)**: a pure
   function deriving the POS phase from `ServiceSessionResponse`, replacing any
   client-held notion of "where we are in the sale". Section 3.
2. **Checkout API seam (`web/src/features/pos/api/use-checkout.ts`)**:
   `useCommitDraft(sessionId)` and `usePayCash(sessionId)`, both writing the
   returned projection straight into the react-query cache.
3. **Payment dialog (`web/src/features/pos/components/payment-dialog.tsx`)**:
   total, quick-tender denominations, a cash-tendered field driven by the
   existing keypad hotkeys, live change due, and a result screen showing the
   change the server computed.
4. **Check panel (`web/src/features/pos/components/check-panel.tsx`)**: the Order
   Bill's read-only face once the draft is committed — the Check's charge
   allocations, its outstanding balance, and, after settlement, the "Khách tiếp
   theo" action that opens the next Service Session.
5. **Pure payment arithmetic (`web/src/features/pos/utils/payment.ts`)**: change
   due, tender sufficiency, and quick-tender suggestions.
6. **Session orchestration extraction (`web/src/features/pos/api/use-pos-session.ts`)**:
   the Service Session id lifecycle moves out of `pos-view.tsx`, which is already
   403 lines and would otherwise absorb the whole checkout flow.
7. **Vietnamese error mapping** for the commit and payment error codes.
8. **Unit tests** under `bun test`, per the sequence spec's definition of done.

### 1.2 Out of Scope (Deliberate Omissions & Recorded Cuts)

- **Submit and Session closure (Slice 5)**: "Gửi bếp" stays disabled. This leaves
  a deliberate dead end, accepted with open eyes — see section 2.
- **Manual QR / VietQR payment**: `POST /sales/checks/{id}/payments/manual-qr`
  exists and works, but the sequence spec scopes Slice 4 to cash. The VietQR tab
  stays disabled with a "Sắp có" label.
- **Partial payment**: the backend accepts `applied_amount_vnd` below the
  balance. This slice always applies the full balance; a Check is either
  untouched or settled.
- **Check split and merge** (`/split`, `/merge`) and **check targeting**
  (`PUT /draft/check-target`): these serve dine-in bill splitting, which needs
  Slice 7's table model to be meaningful.
- **Payment void** (`POST /sales/payments/{id}/void`): a correction path
  requiring Manager Approval, belonging with the other corrections.
- **Receipt printing**: out of scope for all nine slices per the sequence spec.

---

## 2. The Accepted Dead End

`StartNewOrderDraftHandler` refuses to open a new draft while a *blocking draft*
stands — a committed draft that has not been submitted
(`internal/sales/draft_rounds.go:77`, `FindBlockingDraft`). Submit lands in
Slice 5.

The consequence is structural, not incidental: once Slice 4 commits and settles a
Check, that Service Session can accept no further rounds and cannot close. It
stays `ACTIVE` until Slice 5 gives it a submit and a close.

Two options were weighed. Pulling Submit and Close forward would close the tracer
bullet completely and leave no stranded sessions, at the cost of hollowing out
Slice 5 and enlarging this pull request. Holding the slice boundary keeps the
sequence honest and the PR reviewable.

**Decision: hold the boundary.** After settlement the Check panel offers "Khách
tiếp theo", which opens a *new* Service Session for the next customer. The
settled session is left behind, `ACTIVE` and inert. This is acceptable because:

- Nothing is lost. The Check is settled, the money is recorded against the open
  Sales Shift, and the audit trail is complete.
- Slice 5 inherits the cleanup naturally: submitting and closing these sessions
  is exactly the work it does.
- UAT is unaffected. A tester verifies that money is taken correctly, which is
  what this slice claims to deliver.

The disabled "Gửi bếp" button carries the caption "Mở ở Slice 5", matching how
Slice 3 labelled its own cuts.

---

## 3. Phase Derivation

### 3.1 The principle

Every endpoint in this slice returns a complete `ServiceSessionResponse`. The
server therefore already holds the entire truth of where a sale stands. The
client derives its phase from that projection rather than tracking a parallel
state machine.

This is the decision the slice's error handling rests on. A client-held state
machine would need explicit recovery for every way a two-request sequence can
fail, plus reconciliation on page load and across browser tabs. Derivation makes
all of that fall out for free: re-reading the session is the recovery.

### 3.2 `web/src/features/pos/utils/phase.ts`

```ts
export type PosPhase = "NO_SESSION" | "DRAFTING" | "AWAITING_PAYMENT" | "SETTLED";

export function derivePosPhase(session: SalesServiceSessionResponse | null): PosPhase;
```

`LoadServiceSession` reads the draft through `GetEditableDraft`
(`internal/sales/projection.go:88`), so **`draft` is `null` once the draft is
committed**. That single fact makes the derivation unambiguous:

| Condition on `ServiceSessionResponse` | Phase | Bill panel |
| :--- | :--- | :--- |
| No session | `NO_SESSION` | `DraftEmptyState` (Slice 3) |
| `draft != null` and `draft.state === "EDITABLE"` | `DRAFTING` | `draft-panel.tsx` |
| `draft == null`, some check is `OPEN` with `balance_vnd > 0` | `AWAITING_PAYMENT` | `check-panel.tsx` |
| `draft == null`, every check is `SETTLED` | `SETTLED` | `check-panel.tsx` |

### 3.3 Edge cases

- **`draft == null` with no checks.** Unreachable by the domain: a committed
  draft always produces a Check. `derivePosPhase` returns `NO_SESSION` rather
  than throwing, so `pos-view.tsx` opens a fresh session instead of rendering a
  blank panel.
- **More than one `OPEN` check.** Reachable only through split or merge, neither
  of which this slice offers. The Check panel picks the oldest `OPEN` check by
  `created_at`, shows a warning, and disables payment, because deciding which
  bill to collect is the split feature's job.
- **The Sales Shift closes mid-sale.** Slice 2's `NoShiftNotice` already covers
  `DRAFTING`. In `AWAITING_PAYMENT` the payment action is *not* blocked
  client-side: the charge is already committed and the money is owed. The
  backend rejects the payment if the shift has closed, and that rejection
  surfaces like any other error.

---

## 4. Component Structure (`web/src/features/pos/`)

The feature keeps the `api/` · `components/` · `utils/` convention used by
`features/shift`.

**New files**

| File | Responsibility |
| :--- | :--- |
| `utils/phase.ts` | `derivePosPhase()` (section 3) |
| `utils/phase.test.ts` | Phase table and edge cases |
| `utils/payment.ts` | Change due, tender sufficiency, quick-tender suggestions |
| `utils/payment.test.ts` | Arithmetic and suggestion cases |
| `api/use-checkout.ts` | `useCommitDraft()`, `usePayCash()` |
| `api/use-pos-session.ts` | Service Session id lifecycle, extracted from `pos-view.tsx` |
| `components/payment-dialog.tsx` | The checkout dialog (section 6) |
| `components/payment-dialog.test.tsx` | Render assertions |
| `components/check-panel.tsx` | Committed and settled bill faces (section 7) |
| `components/check-panel.test.tsx` | Render assertions |

**Modified files**

| File | Change |
| :--- | :--- |
| `components/pos-view.tsx` | Shrinks to layout and wiring; switches bill panel on phase; session state moves to `use-pos-session.ts` |
| `components/draft-panel.tsx` | The disabled Slice 3 CTA becomes a live "Thanh toán (F9)" button taking `onCheckout`. The payment-method tabs stay in place with "Tiền mặt" active and "VietQR" disabled; the dine-in switcher and "Hủy đơn" keep their Slice 3 disabled state untouched |
| `lib/error-messages.ts` | Commit and payment codes (section 8) |
| `lib/utils.ts` | Receives `VND_DENOMINATIONS` |
| `features/shift/utils/denomination.ts` | Re-exports `VND_DENOMINATIONS` from `lib/utils.ts` |

`VND_DENOMINATIONS` moves because two features now need it and `formatVND`
already lives in `lib/utils.ts`. Re-exporting from `denomination.ts` keeps every
existing import in the shift feature working untouched.

---

## 5. Data Flow & Command Sequencing

### 5.1 Cache coordination

Both mutations return the full projection, so each writes its response directly
into the session query cache:

```ts
queryClient.setQueryData(getGetSalesServiceSessionsIdQueryKey(sessionId), res);
```

This is the pattern `useStartTakeawaySession` established in Slice 3. No
invalidation, no refetch round-trip, no flicker. The phase advances because it is
derived from the projection that was just written.

### 5.2 The two-request sequence

Confirming the payment dialog runs commit and pay-cash back to back:

1. `POST /sales/service-sessions/{id}/draft/commit` with `request_id` **A**. The
   response carries the newly created Check: its `id` and its `balance_vnd`.
2. `POST /sales/checks/{check_id}/payments/cash` with `request_id` **B**,
   `applied_amount_vnd` taken from **step 1's** `balance_vnd`, and
   `cash_tendered_vnd` from the cashier's entry.

**The applied amount comes from the server, never from the client's subtotal.**
Commit revalidates the draft and freezes prices at that moment; a price changed
in Admin since the item was added is reflected in the Check and not in
`pricing.ts`. Slice 3's pricing utilities remain display-only.

The same rule governs the change due: the result screen shows
`payments[].change_due_vnd` from the response, not a locally computed figure.
`payment.ts` computes change only to preview it live while the cashier types.

### 5.3 Idempotency

`request_id` A and B are generated once per dialog lifetime with `newRequestId()`
and held in a `useRef`. "Thử lại" after a network failure replays the same
identifier, so `ExecuteMutation`'s idempotency store reproduces the original
outcome instead of committing or charging twice.

### 5.4 Commit succeeded, payment failed

This is the sequence's only genuinely awkward state, and derivation dissolves it.
The commit response is already in the cache, so the session's phase is
`AWAITING_PAYMENT` the moment step 1 returns.

The dialog stays open and re-renders as "Đã chốt đơn — chưa thu tiền", offering
only the payment action; the commit step disappears from the flow. If the cashier
closes the dialog, or the tab is reloaded, or a second terminal opens the same
session, all of them derive `AWAITING_PAYMENT` and offer "Thu tiền (F9)". No
recovery code is needed anywhere.

---

## 6. The Payment Dialog (`payment-dialog.tsx`)

### 6.1 Entry

"Thanh toán (F9)" in `draft-panel.tsx` is enabled when the phase is `DRAFTING`,
the draft holds at least one item, and a Sales Shift is open. The F9 hotkey uses
`useHotkeys` from react-hotkeys-hook, the library standardised in commit
`74cab90`. (`menu-grid.tsx` still binds `/` through a raw `window` listener; that
inconsistency is real but unrelated to this slice and is left alone.)

### 6.2 Layout

Following `DESIGN.md`:

- **Header**: "Thanh toán", with the Session's `service_number`.
- **Total**: JetBrains Mono, large, emerald — the amount the Check will charge.
- **Quick tender row**: the buttons returned by `suggestTenders` — the exact
  total first, then up to three round-up suggestions drawn from
  `VND_DENOMINATIONS`, four buttons at most.
- **Cash tendered field**: numeric, auto-focused. Driven by the existing
  `useKeypadHotkeys` hook (`web/src/hooks/use-keypad-hotkeys.ts`), which already
  provides digits, Backspace, `C` to clear, Enter to submit, and Escape to close.
  Nothing new is written for keyboard handling.
- **Change due**: updates on every keystroke. When the tender is short it turns
  red, reads "Còn thiếu …", and the confirm button is disabled.
- **Footer**: "Hủy (Esc)" and "Xác nhận (Enter)".

### 6.3 Result screen

On success the dialog does not close. It shows the change due at display size,
plays `playSuccessChirp()`, and offers "Xong (Enter)". A cashier counting notes
out of the drawer needs that number to stay on screen.

On failure it plays `playErrorBuzz()` and shows the message from
`messageForError`.

### 6.4 `utils/payment.ts`

```ts
export function changeDue(tenderedVnd: number, totalVnd: number): number;
export function isTenderSufficient(tenderedVnd: number, totalVnd: number): boolean;
export function suggestTenders(totalVnd: number): number[];
```

`suggestTenders` rounds the total up against `VND_DENOMINATIONS`, drops
duplicates, and returns at most four values, the exact total first. For a total
of 47.000 it yields `[47000, 50000, 100000, 200000]`; for 200.000,
`[200000, 500000]`.

---

## 7. The Check Panel (`check-panel.tsx`)

**`AWAITING_PAYMENT`** — the committed bill, read-only:

- A "Đã chốt" badge replacing the draft's editing affordances.
- `allocations[]` listed with name, `size_name`, modifier names, quantity, and
  `amount_vnd`. These are the frozen snapshots, not catalog reads.
- "Còn phải thu" showing `balance_vnd`.
- "Thu tiền (F9)" opening the payment dialog directly at its payment step.

**`SETTLED`**:

- A "Đã thanh toán" badge, the total received, and the change given.
- "Khách tiếp theo (F9)" opening a new Service Session.
- "Gửi bếp", disabled, captioned "Mở ở Slice 5".

---

## 8. Error Handling

Codes added to `web/src/lib/error-messages.ts`, sourced from
`internal/sales/errors.go:285-325`:

| Code | Vietnamese message |
| :--- | :--- |
| `EMPTY_DRAFT` | Đơn chưa có món nào để thanh toán. |
| `COMMIT_MENU_ITEM_UNAVAILABLE` | Một món trong đơn vừa được tạm ngưng phục vụ. Vui lòng kiểm tra lại đơn. |
| `COMMIT_MENU_ITEM_RETIRED` | Một món trong đơn đã ngừng kinh doanh. Vui lòng xóa món đó khỏi đơn. |
| `COMMIT_SIZE_REQUIRED` | Một món trong đơn chưa chọn kích cỡ. |
| `COMMIT_SIZE_INVALID` | Kích cỡ của một món trong đơn không còn hợp lệ. |
| `COMMIT_SIZE_UNAVAILABLE` | Kích cỡ của một món trong đơn vừa tạm hết. |
| `COMMIT_SIZE_RETIRED` | Kích cỡ của một món trong đơn đã ngừng phục vụ. |
| `COMMIT_MODIFIER_OPTION_INVALID` | Tùy chọn topping của một món không còn hợp lệ. |
| `COMMIT_MODIFIER_OPTION_UNAVAILABLE` | Tùy chọn topping của một món vừa tạm hết. |
| `COMMIT_MODIFIER_OPTION_RETIRED` | Tùy chọn topping của một món đã ngừng phục vụ. |
| `COMMIT_MODIFIER_GROUP_INVALID` | Lựa chọn topping của một món không thỏa quy định của nhóm. |
| `COMMIT_MODIFIER_GROUP_RETIRED` | Một nhóm topping bắt buộc đã ngừng áp dụng. |
| `NEW_ORDER_DRAFT_NOT_AVAILABLE` | Đơn trước chưa được gửi bếp nên chưa thể mở đơn mới. |
| `CHECK_NOT_FOUND` | Không tìm thấy hóa đơn. |
| `CHECK_NOT_OPEN` | Hóa đơn này không còn ở trạng thái chờ thu tiền. |
| `CHECK_HAS_PAYMENT` | Hóa đơn này đã được thanh toán. |
| `PAYMENT_EXCEEDS_CHECK_BALANCE` | Số tiền thu vượt quá số còn phải thu của hóa đơn. |
| `INSUFFICIENT_CASH_TENDERED` | Tiền khách đưa ít hơn số tiền cần thu. |

**The `COMMIT_*` family is the one that shapes the UI.** Commit revalidates the
whole draft, so an item another terminal disabled seconds earlier, or a modifier
group an administrator just reshaped, fails the commit. The dialog closes, the
cashier lands back on the draft, and the message names the problem. Nothing
further is needed: a failed commit leaves the draft `EDITABLE`, so
`derivePosPhase` keeps the screen in `DRAFTING` on its own.

`INSUFFICIENT_CASH_TENDERED` is unreachable while the client's guard holds. It is
mapped anyway, as the safety net for a client-side arithmetic drift.

---

## 9. Unit Testing Strategy

`bun test`, pure logic and render output only. The sequence spec's definition of
done rules out browser tests: the Go suites already carry behavioural coverage,
and duplicating it in the browser slows each slice without adding signal.

### 9.1 `utils/phase.test.ts`

All four phases from the section 3.2 table, plus: `draft == null` with an empty
`checks` array; two `OPEN` checks; a mix of `SETTLED` and `OPEN`; and a session
whose only check is `SETTLED` with a non-zero `total_applied_vnd`.

### 9.2 `utils/payment.test.ts`

`changeDue` including the exact-tender zero case; `isTenderSufficient` at, above,
and below the total; `suggestTenders` for a total between denominations, a total
landing exactly on one, and a total above the largest denomination.

### 9.3 Component render tests

`payment-dialog.test.tsx` and `check-panel.test.tsx` use `renderToString`,
matching `draft-panel.test.tsx`. They assert rendered output — totals, change
due, badges, the disabled "Gửi bếp" caption — not interaction.

---

## 10. Definition of Done & UAT Handover

### 10.1 Verification checklist

1. `bun test` passes; `bun run lint` and `tsc -b` are clean.
2. No client-side state machine: `derivePosPhase` is the only source of phase.
3. `applied_amount_vnd` and the displayed change both originate from server
   responses.
4. `request_id` values are stable across retries within one dialog lifetime.
5. `pos-view.tsx` is smaller than it was before this slice.
6. One pull request for the slice.

### 10.2 UAT gate handover

Handed over as `uat-handover.md`, per Slice 1's precedent.

**Happy path:** open a Sales Shift → add several items → F9 → enter the cash
tendered → check the change due → confirm → see "Đã thanh toán" with the change →
"Khách tiếp theo" opens a fresh session.

**Unhappy paths:**

1. Reload the page while in `AWAITING_PAYMENT`; the Check and its outstanding
   balance must come back exactly as they were.
2. Disable an item in Admin while it sits in a draft, then commit; the commit is
   refused with the Vietnamese message, and the draft remains editable.
3. Disconnect the network mid-confirm, then press "Thử lại"; verify in the Shift
   view that exactly one payment was recorded.
