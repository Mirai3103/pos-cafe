# Design Specification: Web Slice 9a — Availability (`./web` + `internal/catalog`, Phase 11 UI)

- **Author:** Claude & Team
- **Date:** 2026-09-28
- **Status:** Draft, pending review
- **Phase:** Phase 11, UI half — see [`docs/backlog/phase-11-clients-over-lan-and-single-binary.md`](../../backlog/phase-11-clients-over-lan-and-single-binary.md)
- **Predecessors:**
  - [`2026-09-21-web-frontend-slice-sequence-design.md`](2026-09-21-web-frontend-slice-sequence-design.md) (slice sequence & definition of done)
  - [`2026-09-23-web-slice-3-pos-draft-design.md`](2026-09-23-web-slice-3-pos-draft-design.md) (sellable menu on the POS)
- **Visual authority:** [`design-system/pos-cafe/DESIGN.md`](../../../design-system/pos-cafe/DESIGN.md), [`design-system/pos-cafe/pages/settings.html`](../../../design-system/pos-cafe/pages/settings.html) tab "Kho & Món Tạm Hết"
- **Domain authority:** [`CONTEXT.md`](../../../CONTEXT.md) (Availability), `internal/catalog/` (Phase 2), ADR-048, ADR-054

---

## 1. Purpose & Scope

### 1.1 Splitting slice 9

The slice sequence names slice 9 "Settings and catalog administration". Its
surface is three independent jobs with different users:

| Sub-slice | Job | Who | Endpoints |
| --- | --- | --- | --- |
| **9a** (this spec) | Mark things temporarily unavailable and back | Manager, **Cashier, Barista** (`catalog.manage_availability`) | `/catalog/menu/availability`, `…/availability`, new batch |
| 9b | Catalog structure: categories, items, sizes, modifier groups, prices, retirement, assignments | Manager | `/catalog/*` |
| 9c | Staff administration | Manager | `/staff/*` |

Each sub-slice has its own spec, pull request, and UAT gate, per the slice
sequence's rule that one change stays reviewable. 9a goes first because it is
the only part used during service, every shift.

### 1.2 In Scope

- A new Go command, `POST /catalog/availability/batch`: an atomic, explicit-list
  availability change with one audit event (section 2, ADR-055).
- `/settings` becomes a tabbed layout with per-tab capability guards; the only
  tab in 9a is "Món tạm hết" (section 3, ADR-056).
- The header hides navigation entries the session lacks the capability for.
- The availability tab: stats, search, category filter, "only unavailable"
  filter, per-item/size/option toggles, sellability warning, restore-all
  (section 4).
- The POS sellable menu stays fresh when availability changes elsewhere
  (section 5).

### 1.3 Out of Scope (Recorded Cuts)

| Prototype element (`settings.html`) | Why it is cut |
| --- | --- |
| Tab "Quản lý Thực đơn & Topping" | Slice 9b. |
| Staff administration (not in the prototype) | Slice 9c. |
| Tab "Thông tin Quán & VietQR" (store profile, bank setup) | No Go endpoint stores a store profile or VietQR beneficiary. |
| Tab "Tùy chỉnh In & Hệ thống" (K80 receipt template, test data reset) | No endpoint; the receipt boundary is unresolved in [`open-questions.md`](../../backlog/open-questions.md). |
| Item image, item code ("cfsd"), price on the stock card | The availability menu carries none of them; reading the management menu only to decorate the card is not worth a second query. |
| Landing on the first permitted route after workspace declaration | Suggested by ADR-054; not needed to ship this tab. |

---

## 2. Backend: batch availability command

### 2.1 Contract

`POST /catalog/availability/batch`, requires `catalog.manage_availability`
(the same capability as the single-entity commands). No Manager PIN.

```json
{
  "request_id": "uuid",
  "changes": [
    { "kind": "item" | "size" | "modifier_option", "id": "uuid", "available": true }
  ]
}
```

Validation (`400 INVALID_INPUT`): `changes` holds 1 to 200 entries; `kind` is
one of the three values; no `(kind, id)` pair appears twice.

Response `200`:

```json
{ "results": [ { "kind": "size", "id": "uuid", "available": true, "changed": true } ] }
```

`results` lists every requested entry in the sorted order of section 2.2, with
`changed: false` for a same-state no-op.

### 2.2 Execution

Runs inside `ExecuteMutation` like every catalog command, so authority reload,
denial audit, idempotency claim, and replay are inherited unchanged.

- **Operation:** `catalog.availability.set_batch` (`OpAvailabilitySetBatch`).
- **Fingerprint:** the `changes` list sorted by `(kind, id)`. Reordering the
  same request replays; changing any entry is `409 REQUEST_CONFLICT`.
- **Order:** entries are processed sorted by `(kind, id)`, locking each row
  with the existing `GetMenuItemForUpdate`, `GetMenuItemSizeForUpdate`, and
  `GetModifierOptionForUpdate`. A fixed lock order keeps two concurrent batches
  from deadlocking.
- **Refusal:** an unknown id is `404 CATALOG_NOT_FOUND`; a retired entity is
  `409 ENTITY_RETIRED`. Either rolls back the whole batch.
- **No-op:** an entry already in the requested state is not written, exactly as
  the single commands behave.
- **Write:** the existing `SetMenuItemAvailability`,
  `SetMenuItemSizeAvailability`, and `SetModifierOptionAvailability` queries.
- **Audit:** one event, `catalog.availability.batch_changed`
  (`EventAvailabilityBatchChanged`), details
  `{"changes":[{"kind","id","old_available","new_available"}]}` listing only
  entries that changed. When nothing changed, no audit event is written — the
  single commands' no-op rule.

No migration: the audit and idempotency tables are shared.

### 2.3 Files

| File | Change |
| --- | --- |
| `internal/catalog/domain.go` | `OpAvailabilitySetBatch`, `EventAvailabilityBatchChanged`, kind constants |
| `internal/catalog/dto.go` | `SetAvailabilityBatchRequest`, `AvailabilityChangeInput`, `AvailabilityBatchResponse`, `AvailabilityBatchResult` |
| `internal/catalog/availability.go` | `SetAvailabilityBatchHandler` |
| `internal/catalog/http.go` | `handleSetAvailabilityBatch` with Swagger annotations |
| `internal/catalog/routes.go` | route registration |
| `docs/swagger.*`, `web/src/api/generated/` | regenerated |
| `spec/decisions.md` | ADR-055 |

### 2.4 ADR-055

**Bulk availability is one atomic, explicit-list command with one audit event.**
Rejected: a server-scoped "restore all" (it would restore an entity another
terminal marked unavailable after the operator last saw the list), and one
audit event per entity (it would require `ExecuteMutation` to accept several
`AuditRecord`s, touching every catalog command for one caller). One event per
user intent matches ADR-048.

---

## 3. Settings shell, guards, navigation

### 3.1 Nested routes

```
routes/_app/settings.tsx               layout: tab bar + <Outlet/>
routes/_app/settings/index.tsx         redirects to the first permitted tab
routes/_app/settings/availability.tsx  requireCapability("catalog.manage_availability")
```

9b and 9c add `settings/menu.tsx` and `settings/staff.tsx`.

`features/settings/lib/tabs.ts` exports `SETTINGS_TABS: { to, label, icon,
capability }[]` and `firstPermittedTab(capabilities)`. It is the single source
for the layout's guard, the index redirect, and the tab bar. A tab that is not
built yet is absent, not a placeholder. In 9a the array has one entry.

### 3.2 Guards

`lib/guards.ts` gains `requireAnyCapability(capabilities: string[])`, with the
same `/no-access` redirect and the same locked-session exemption as
`requireCapability`. The settings layout calls it with every tab's capability.

### 3.3 ADR-056

**`/settings` is guarded per tab, not by `staff.administer`.** ADR-054 recorded
`/settings` as requiring `staff.administer`. Availability is operational work
that Cashier and Barista hold `catalog.manage_availability` for; guarding the
whole route by a Manager-only capability would lock them out. The layout now
requires any tab's capability, and each tab route its own.

### 3.4 Header navigation

`navItems` in `components/layout/pos-header.tsx` gains an optional
`capabilities: string[]` (any-of). An entry is hidden when the session holds
none of them. `/` and `/tables` → `sales.operate`; `/kds` →
`preparation.operate`; `/shift` → `sales_shift.operate`; `/settings` → every
settings tab's capability. `/history` has no guard until slice 8 and stays
visible. A Barista sees "Bếp KDS" and "Cài đặt".

---

## 4. Availability tab (`/settings/availability`)

### 4.1 Data

`features/settings/api/use-availability.ts`:

- `useAvailabilityMenu()` wraps `useGetCatalogMenuAvailability` with `unwrap`
  and `refetchInterval: 15_000`: another terminal may change availability.
- `useSetAvailability()` dispatches one of the three single PATCH commands by
  kind, with a fresh `request_id` per toggle, an optimistic update of the
  availability cache, and invalidation of both the availability and sellable
  menu queries on settle.
- `useRestoreAvailability()` posts the batch.

### 4.2 Pure view model

`features/settings/lib/availability.ts`, `toAvailabilityView(menu)`:

- `items[]`: id, name, category id and name, `available`, `sizes[]`, and
  `blockedBy: string[]` — the names of required modifier groups
  (`min_selections > 0`) that have no available option. CONTEXT.md: an item is
  not sellable while any required Modifier Group has no valid available
  selection.
- `toppings[]`: modifier options **deduplicated by id** (the response repeats a
  group under every item that uses it), each with its group name, grouped by
  group.
- `stats`: total, available, unavailable, counting items, sizes, and options.
- `unavailableRefs: { kind, id, name }[]`: what restore-all sends and lists.

`filterAvailability(view, { query, categoryId | "toppings" | "all",
onlyUnavailable })` is pure. Search is diacritic-insensitive over item, size,
option, and category names. `normalizeVietnamese` moves from
`features/pos/utils/search.ts` to `src/lib/search.ts` so both features import it
without crossing feature boundaries.

### 4.3 Layout

Following the prototype's stock tab:

- **Stats row:** "Tổng danh mục", "Đang còn hàng", "Tạm hết hàng", and a
  fourth card holding **"Khôi phục tất cả còn hàng (N)"**, disabled at N = 0.
- **Filter bar:** search input, "Chỉ xem món tạm hết" toggle, category pills
  from the API plus "Topping".
- **Item card:** name, category, a large availability switch, and a row of
  size chips (each a switch, "S ●  M ●  L ○"). When the item is off, its size
  chips are dimmed but still operable. When `blockedBy` is non-empty, a warning
  badge reads "Không bán được: hết tùy chọn bắt buộc (Đường)".
- **Topping view** (the "Topping" pill): options grouped under their group
  name, one switch each.
- **Tab counter:** "N tạm hết" in the tab bar, red when N > 0.

Touch targets follow `--spacing-touch`.

### 4.4 Restore all

`RestoreAvailabilityDialog` lists the `unavailableRefs` it will restore.
Its `request_id` is generated when the dialog opens and kept across retries
(`lib/command.ts`). Confirm posts the batch with every ref set `available:
true`. On `409` it refetches and shows "Danh sách đã thay đổi, vui lòng kiểm tra
lại" without closing, so the operator confirms the fresh list with a new
intent.

### 4.5 States

Loading: skeleton cards. No match: "Không tìm thấy món hoặc topping phù hợp"
with "Xóa bộ lọc". Empty menu: "Chưa có món nào trong thực đơn". Error: the
envelope message with retry. A toggle's `ENTITY_RETIRED` or `CATALOG_NOT_FOUND`
shows a toast, rolls back the optimistic change, and refetches.

---

## 5. POS freshness

Today `useSellableMenu` has `staleTime: 60_000`, no polling, and
`refetchOnWindowFocus: false` globally, so a POS never learns that another
terminal marked an item unavailable.

- `useSellableMenu` gains `refetchInterval: 30_000`.
- A draft-item mutation failing with `MENU_ITEM_UNAVAILABLE`,
  `MENU_ITEM_RETIRED`, `SIZE_UNAVAILABLE`, `SIZE_RETIRED`,
  `MODIFIER_OPTION_UNAVAILABLE`, or `MODIFIER_OPTION_RETIRED`, or a commit
  failing with the matching `COMMIT_*` code, invalidates the sellable menu at
  once.
- Availability mutations on the same device invalidate it too (section 4.1).

---

## 6. Error messages

`src/lib/error-messages.ts` gains `ENTITY_RETIRED` ("Mục này đã ngừng kinh
doanh.") and `CATALOG_NOT_FOUND` ("Không tìm thấy mục trong thực đơn."). The
sales codes of section 5 already have messages.

---

## 7. Files (web)

| File | Change |
| --- | --- |
| `src/routes/_app/settings.tsx` | layout with tab bar, `requireAnyCapability` |
| `src/routes/_app/settings/index.tsx` | redirect to first permitted tab |
| `src/routes/_app/settings/availability.tsx` | tab route |
| `src/lib/guards.ts` | `requireAnyCapability` |
| `src/lib/search.ts` | `normalizeVietnamese`, moved from POS |
| `src/lib/error-messages.ts` | section 6 |
| `src/components/layout/pos-header.tsx` | capability-filtered nav |
| `src/features/settings/lib/tabs.ts` | `SETTINGS_TABS`, `firstPermittedTab` |
| `src/features/settings/lib/availability.ts` | view model, filter |
| `src/features/settings/api/use-availability.ts` | query and commands |
| `src/features/settings/components/settings-layout.tsx` | tab bar |
| `src/features/settings/components/availability-view.tsx` | the tab |
| `src/features/settings/components/availability-stats.tsx` | stats row |
| `src/features/settings/components/availability-item-card.tsx` | one item |
| `src/features/settings/components/availability-toppings.tsx` | topping view |
| `src/features/settings/components/restore-availability-dialog.tsx` | restore all |
| `src/features/settings/components/settings-view.tsx` | deleted (placeholder) |
| `src/features/pos/api/use-pos.ts` | sellable refetch interval; draft-item mutations invalidate the sellable menu on the section 5 codes |
| `src/features/pos/api/use-checkout.ts`, `use-dine-in.ts` | commit failures with a `COMMIT_*` availability code invalidate the sellable menu |
| `src/features/pos/utils/search.ts` | import from `lib/search` |

---

## 8. Testing strategy

**Go** (PostgreSQL template integration tests, following
`commands_integration_test.go`): a mixed batch changes every kind and writes one
audit event listing only changed entries; an all-no-op batch writes no audit
event; a retired entry rolls back the whole batch; an unknown id is 404;
replay returns the stored response; the same `request_id` with a different list
is `REQUEST_CONFLICT`; reordered entries replay; a session without the
capability is 403 with a denial audit. Unit tests for validation (empty, over
200, duplicate, unknown kind) and fingerprint sorting.

**Web** (`bun test`, pure logic and hooks only, per the definition of done):
`availability.test.ts` (topping dedup, `blockedBy`, stats, `unavailableRefs`,
diacritic search, category and only-unavailable filters); `tabs.test.ts`
(`firstPermittedTab`); `guards.test.ts` (`requireAnyCapability`); header nav
filtering per role; restore dialog keeps its `request_id` across a retry; POS
sellable invalidation on an availability error code.

---

## 9. UAT gate

Implementation stops after handing over this script; the operator confirms.

1. As Manager, "Cài đặt" lands on "Món tạm hết"; the stats match the menu.
2. Turn "Bạc xỉu" off: a POS in another tab stops offering it within 30 seconds.
   Turn it on: it returns.
3. Turn size L of an item off: the POS offers S and M only.
4. Turn off every option of a required group: the item shows "Không bán được",
   and the POS no longer offers it.
5. Search "bac xiu" finds "Bạc xỉu". "Chỉ xem món tạm hết" shows only what is off.
6. Turn three entries off, press "Khôi phục tất cả còn hàng (3)": the dialog
   lists exactly those three; confirming turns all three on.
7. As Barista: the nav shows "Bếp KDS" and "Cài đặt"; toggling works. As
   Cashier: "Cài đặt" is present; toggling works.
8. Add an item to a draft, turn that item off on another terminal, press pay:
   the POS shows the Vietnamese error and its menu grid refreshes.
9. Takeaway and dine-in sales behave exactly as before.

---

## 10. Definition of done

The slice sequence's section 3 applies unchanged. One pull request carries both
the Go command and the web tab.
