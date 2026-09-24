# Design Specification: Web Slice 7 — Tables and Dine-in (`./web`, Phase 11 UI)

- **Author:** Claude & Team
- **Date:** 2026-09-27
- **Status:** Draft, pending review
- **Phase:** Phase 11, UI half — see [`docs/backlog/phase-11-clients-over-lan-and-single-binary.md`](../../backlog/phase-11-clients-over-lan-and-single-binary.md)
- **Predecessors:**
  - [`2026-09-21-web-frontend-slice-sequence-design.md`](2026-09-21-web-frontend-slice-sequence-design.md) (slice sequence & definition of done)
  - [`2026-09-24-web-slice-4-pos-checkout-design.md`](2026-09-24-web-slice-4-pos-checkout-design.md) (commit, cash payment)
  - [`2026-09-25-web-slice-5-pos-submit-close-design.md`](2026-09-25-web-slice-5-pos-submit-close-design.md) (submit, closure, pending orders drawer)
- **Visual authority:** [`design-system/pos-cafe/DESIGN.md`](../../../design-system/pos-cafe/DESIGN.md), [`design-system/pos-cafe/pages/tables.html`](../../../design-system/pos-cafe/pages/tables.html)
- **Domain authority:** [`CONTEXT.md`](../../../CONTEXT.md) (Table, Service Session, Service Number), [`docs/domain-rationale/01-define-mixed-service-order-lifecycle.md`](../../domain-rationale/01-define-mixed-service-order-lifecycle.md), `internal/tables/` (Phase 3), `internal/sales/` (Phases 5A–5D)

---

## 1. Purpose & Scope

Slice 7 replaces the `tables.tsx` placeholder with the real floor view and
teaches the POS the dine-in service lifecycle. Until now the POS has sold only
takeaway: commit, pay, then submit. A seated customer is served differently,
and the Go side already allows it — `ModeRequiresSettlementBeforeSubmit` is
true only for `TAKEAWAY`. A dine-in Session orders in rounds, each round goes to
the bar before any money changes hands, and the bill is collected at the end.

The slice blocks nothing and is blocked by nothing but slices 1–5.

### 1.1 In Scope

- **Floor view** at `/tables`: every Table with its availability and its
  current Service Sessions, polled.
- **Open a dine-in Session** at one or more Tables (`POST /sales/service-sessions/dine-in`).
- **Enter an occupied Table**: jump to the POS with that Session active; choose
  between Sessions when a Table carries more than one.
- **Dine-in on the POS**: session header with Table names, ordering in rounds
  (`POST /sales/service-sessions/{id}/draft`), send-to-bar (commit + submit)
  before payment, cash collection of the open Check, closure.
- **Change Tables** for a Session (`PUT /sales/service-sessions/{id}/tables`):
  one dialog covers move, add, and release.
- **Pending orders drawer** lists dine-in Sessions alongside takeaway.
- **Table administration** for `tables.administer`: create, rename, set
  availability (`POST /tables`, `PATCH /tables/{id}/name`,
  `PATCH /tables/{id}/availability`).
- **Dev seed** creates eight Tables idempotently.

### 1.2 Out of Scope (Deliberate Omissions & Recorded Cuts)

| Prototype element (`tables.html`) | Why it is cut |
| --- | --- |
| Merge two Tables' Sessions ("Gộp bàn") | No Go operation merges Service Sessions. Moving a Session onto an occupied Table is possible (Tables are not exclusive), which covers the physical move. |
| Split items to another Table ("Tách món") | No Go operation moves Committed Items between Sessions. Check split within one Session exists but stays out, as in slice 4. |
| Zones / floors, guest count, "needs cleaning" status | The Table model has `name` and `available` only. |
| Manual QR payment, partial payment | Slice 4's cash-only, full-balance payment dialog is reused unchanged. |
| Check target (`CURRENT_UNPAID` / `NEW_CHECK`) | The default, `CURRENT_UNPAID`, is what a table bill needs: every round joins one Check. |
| Releasing every Table from a live Session | The API permits an empty `table_ids`; the UI requires at least one. Closure releases Tables. |
| Realtime push | Polling, as in slice 6. |

---

## 2. Domain recap

- A **Table** is a named physical location. It may carry more than one active
  Service Session; it is never an exclusive tab. An unavailable Table cannot
  receive a new assignment (`DINE_IN_TABLE_UNAVAILABLE`).
- A **dine-in Session** opens with an empty editable Order Draft and at least
  one Table (`DINE_IN_TABLE_SELECTION_REQUIRED`).
- **Rounds.** Commit freezes the draft into Committed Items and charges the
  Session's current open Check; `draft` is then `null` in the projection.
  Submit turns the committed round into an Order and Preparation Units.
  `POST .../draft` opens the next round and is refused
  (`NEW_ORDER_DRAFT_NOT_AVAILABLE`) while an editable draft exists or a
  committed round has not been submitted.
- **Payment timing.** Dine-in may submit before or after payment. Takeaway may
  not.
- **Closure** requires every Check settled, every committed item submitted, at
  least one Order, and every Preparation Unit terminal. An editable draft does
  not block closure. Closure releases every held Table.

---

## 3. Floor view (`/tables`)

**Route.** `beforeLoad: requireCapability("sales.operate")` — the same
capability `GET /tables/overview` requires.

**Data.** `useTablesOverview()` wraps `useGetTablesOverview` with
`refetchInterval: 10_000`, and every Table or Session mutation in this slice
invalidates it.

**Card states**, derived by a pure `tableCardState(row)`:

| State | Condition | Card |
| --- | --- | --- |
| `FREE` | `available` and no current Sessions | "Trống", tappable |
| `OCCUPIED` | at least one current Session | "Có khách", lists each Service Number |
| `UNAVAILABLE` | `!available` and no current Sessions | "Tạm ngưng", dimmed, not tappable for opening |

An unavailable Table that still carries a Session shows `OCCUPIED` with an
"Tạm ngưng" marker: the Session is still served there.

**Tap a free Table** → `OpenTableDialog`, with that Table preselected and the
other free, available Tables selectable for a party spread across Tables.
Confirm posts `dine-in` with one `request_id` held for the dialog's lifetime,
then `switchSession(id)` and navigates to `/`.

**Tap an occupied Table.** One Session → `switchSession` and navigate to `/`.
Several → a popover listing Service Numbers, plus "Mở phiên mới tại bàn này",
which opens `OpenTableDialog` for that Table.

**No open Shift.** The floor still renders; opening a Session is disabled and
the reused `NoShiftNotice` explains why. Entering an existing Session still
works.

**Header stats** replace the placeholder's invented ones: total Tables, free,
occupied, unavailable — counted from the overview.

**Loading, empty, error.** Loading skeleton; "Chưa có bàn nào" with the admin's
"Thêm bàn" action when the list is empty; the error envelope's message with a
retry otherwise.

### 3.1 Table administration

Visible only when `hasCapability("tables.administer")`.

- **"Thêm bàn"** in the header → `TableNameDialog` → `POST /tables`.
- A **⋯ menu** on every card → "Đổi tên" (`TableNameDialog`,
  `PATCH .../name`) and "Tạm ngưng" / "Mở lại" (`PATCH .../availability`,
  no confirmation, reversible).
- `TABLE_NAME_CONFLICT` shows inline under the name field.
- Each dialog or toggle holds one `request_id` per intent via `command.ts`.

### 3.2 Dev seed

`scripts/dev-seed.ts` creates `Bàn 1` … `Bàn 8` when no Table with that name
exists, so re-running it is harmless.

---

## 4. Dine-in on the POS

### 4.1 Mode dispatch

`PosView` reads `session.service_mode`. `TAKEAWAY` (and no Session) keep
today's path — `derivePosPhase`, `useCheckoutFlow`, `CheckPanelActions` —
untouched. `DINE_IN` uses `deriveDineInStatus`, `useDineInFlow`, and
`DineInActions`. The menu grid, draft panel, item picker, payment dialog,
completed-sale dialog, and close flow are shared.

### 4.2 `deriveDineInStatus(session)`

A linear phase cannot describe a dine-in Session: one Session can at once hold
an unpaid Check, units in preparation, and a new round being drafted. The
status is therefore a set of independent facts, pure and derived from the
projection:

```ts
interface DineInStatus {
  draftItemCount: number;       // items in the editable draft, 0 if none
  hasEditableDraft: boolean;
  hasUnsubmittedWork: boolean;  // reuses phase.ts
  openCheck: SalesCheckResponse | null;  // reuses selectOpenCheck
  hasMultipleOpenChecks: boolean;
  progress: PreparationProgress;
  canClose: boolean;            // mirrors EvaluateClosureReadiness
  canOrder: boolean;            // !hasUnsubmittedWork
}
```

`canClose` is: at least one live Check and none unsettled, no pending refund,
at least one Order, no unsubmitted work, every unit terminal. It exists to
enable a button; the server remains the authority and its refusal is shown.

### 4.3 Session header

A strip above the draft: "Bàn 3, Bàn 4 · #D-012" and a **"Đổi bàn"** button
opening `ChangeTablesDialog`. The dialog lists the Session's current Tables
(checked) and every available Table (occupied ones marked with their Service
Numbers, since sharing is legal). At least one must stay checked. Confirm sends
the full set to `PUT .../tables` and invalidates the overview.

### 4.4 Ordering in rounds

- `ensureDraft()` joins `ensureSessionId()` in `usePosSession`. When the active
  Session is dine-in and has no editable draft, adding an item first posts
  `.../draft`, deduplicated by a promise ref exactly as the lazy takeaway
  Session is. Sending a round therefore never leaves an empty draft behind.
- `NEW_ORDER_DRAFT_NOT_AVAILABLE` after a concurrent change refetches the
  Session and retries the add once.
- While `!canOrder` the menu grid is disabled with "Gửi bếp lượt trước để gọi
  thêm".

### 4.5 Actions

`DineInActions` shows every action whose condition holds, one of them primary:

| Button | Shown when | Does |
| --- | --- | --- |
| **Gửi bếp** | `draftItemCount > 0` or `hasUnsubmittedWork` | commit if the draft has items, then submit |
| **Thu tiền** | `openCheck` and not `hasMultipleOpenChecks` | opens the shared `PaymentDialog` for `openCheck.balance_vnd`; pays only, never commits |
| **Hoàn tất** | `canClose` | the shared `useCloseFlow`, then the completed-sale dialog |
| **Về sơ đồ bàn** | always | clears the pointer and navigates to `/tables` |

"Thu tiền" is disabled while `draftItemCount > 0`, with "Gửi bếp hoặc xóa món
đang soạn trước khi thu tiền": uncommitted items have no frozen price and the
customer must not leave without them being resolved. Multiple open Checks show
slice 4's warning and refuse collection.

**F9** fires the first enabled of Gửi bếp → Thu tiền → Hoàn tất;
`use-pos-hotkeys.ts` gains the mode.

### 4.6 `useDineInFlow`

- **Send to bar.** `commitRequestId` and `submitRequestId` are generated on
  first attempt and kept until success, so a retry replays rather than
  duplicates. A `COMMIT_FAILURE_CODES` error returns to the draft toast. If
  commit succeeds and submit fails, the projection shows `hasUnsubmittedWork`,
  so the same button retries submit alone. `NOTHING_TO_SUBMIT` counts as done.
- **Collect.** `payCash(openCheck.id, { applied_amount_vnd: balance, cash_tendered_vnd })`
  with one `payRequestId` per dialog; no commit, no automatic submit.
- **Close.** Delegates to `useCloseFlow`. Dismissing the completed-sale dialog
  clears the pointer and navigates to `/tables`.

`useCheckoutFlow` is not changed.

### 4.7 Pending orders drawer

`toPendingOrders` drops its `TAKEAWAY` filter. A dine-in row carries its Table
names (or "Chưa có bàn") where takeaway shows "Mang đi", and a status label from
`dineInStatusLabel(status)`, first match wins:

1. `canClose` → "Sẵn sàng hoàn tất"
2. `hasUnsubmittedWork` or `draftItemCount > 0` → "Chờ gửi bếp"
3. `openCheck` → "Chờ thu tiền"
4. units not all terminal → "Đang pha chế"
5. otherwise → "Đang soạn"

The drawer's sort priority maps these onto the existing takeaway buckets.

---

## 5. Error messages

`src/lib/error-messages.ts` gains Vietnamese messages for `TABLE_NOT_FOUND`,
`TABLE_NAME_CONFLICT`, `DINE_IN_TABLE_SELECTION_REQUIRED`,
`DINE_IN_TABLE_SELECTION_DUPLICATE`, `DINE_IN_TABLE_NOT_FOUND`,
`DINE_IN_TABLE_UNAVAILABLE`, `TAKEAWAY_TABLE_ASSIGNMENT_NOT_AVAILABLE`, and
`NEW_ORDER_DRAFT_NOT_AVAILABLE`. A 409 on any dialog refetches the overview.

---

## 6. Files

| File | Change |
| --- | --- |
| `src/routes/_app/tables.tsx` | capability guard |
| `src/features/tables/api/use-tables.ts` | overview query, dine-in open, change tables, admin commands |
| `src/features/tables/lib/floor.ts` | `tableCardState`, stats, selection helpers |
| `src/features/tables/components/tables-view.tsx` | rewritten: floor grid, header, states |
| `src/features/tables/components/table-card.tsx` | one Table |
| `src/features/tables/components/open-table-dialog.tsx` | open a dine-in Session |
| `src/features/tables/components/session-picker.tsx` | choose among a Table's Sessions |
| `src/features/tables/components/table-name-dialog.tsx` | create / rename |
| `src/features/pos/utils/dine-in.ts` | `deriveDineInStatus`, `dineInStatusLabel` |
| `src/features/pos/api/use-dine-in.ts` | `useDineInFlow`, `useStartNextDraft` |
| `src/features/pos/api/use-pos-session.ts` | `ensureDraft` |
| `src/features/pos/components/dine-in-header.tsx` | Table strip + "Đổi bàn" |
| `src/features/pos/components/change-tables-dialog.tsx` | set Tables |
| `src/features/pos/components/dine-in-actions.tsx` | action bar |
| `src/features/pos/components/pos-view.tsx` | mode dispatch |
| `src/features/pos/hooks/use-pos-hotkeys.ts` | dine-in F9 |
| `src/features/pos/utils/pending-orders.ts` | dine-in rows |
| `src/lib/error-messages.ts` | codes in section 5 |
| `scripts/dev-seed.ts` | eight Tables |

`PosView` is already the largest component; the dine-in branch lives in the
new hook and components so it grows by dispatch only.

---

## 7. Testing strategy

Per the slice sequence's definition of done: pure logic and hook behaviour,
no end-to-end and no integration tests.

- `floor.test.ts`: card state for every combination of availability and
  occupancy; stats.
- `dine-in.test.ts`: `deriveDineInStatus` across fresh Session, drafting,
  committed-unsubmitted, submitted-unpaid, paid-in-preparation,
  ready-to-close, multiple open Checks; `dineInStatusLabel` precedence.
- `pending-orders.test.ts`: dine-in rows included, labelled, sorted.
- `use-dine-in.test.ts`: request ids survive retry; commit failure routes to
  the draft; submit-only retry after a failed submit; payment never commits.
- `use-pos-session.test.ts`: `ensureDraft` deduplicates and opens a round only
  for a dine-in Session without an editable draft.
- Component tests: `DineInActions` visibility and disabling;
  `ChangeTablesDialog` refuses an empty set; `TablesView` hides admin controls
  without `tables.administer`.

---

## 8. UAT gate

Implementation stops after handing over this script; the operator confirms.

1. Run the dev seed; `/tables` shows Bàn 1–8 as "Trống".
2. Without an open Shift, opening a Table is disabled with the notice.
3. Open a Shift. Tap Bàn 1, also select Bàn 2, confirm: the POS shows
   "Bàn 1, Bàn 2 · #…".
4. Add two drinks, "Gửi bếp": the KDS shows them; "Thu tiền" is available.
5. Add another drink: a new round opens; "Thu tiền" is disabled until it is sent.
6. "Gửi bếp" again; the Check balance covers both rounds.
7. "Đổi bàn" to Bàn 5 only: the floor shows Bàn 5 occupied, Bàn 1–2 free.
8. Open a second Session at Bàn 5 from its popover; both Service Numbers show.
9. Back on the first Session, "Thu tiền" with cash; mark every unit fulfilled
   on the KDS; "Hoàn tất" shows the Completed Sale; "Xong" returns to the floor
   with only the second Session on Bàn 5.
10. The pending orders drawer shows the second Session with its Table.
11. As a manager: add "Bàn 9", rename it, set it "Tạm ngưng"; it cannot be
    opened. As a cashier without `tables.administer`: no admin controls.
12. A takeaway sale end to end behaves exactly as before.

---

## 9. Definition of done

The slice sequence's section 3 applies unchanged. One pull request.
