# Design Specification: Web Slice 9b — Catalog Structure (`./web`, Phase 11 UI)

- **Author:** Claude & Team
- **Date:** 2026-09-30
- **Status:** Draft, pending review
- **Phase:** Phase 11, UI half — see [`docs/backlog/phase-11-clients-over-lan-and-single-binary.md`](../../backlog/phase-11-clients-over-lan-and-single-binary.md)
- **Predecessors:**
  - [`2026-09-21-web-frontend-slice-sequence-design.md`](2026-09-21-web-frontend-slice-sequence-design.md) (slice sequence & definition of done)
  - [`2026-09-28-web-slice-9a-availability-design.md`](2026-09-28-web-slice-9a-availability-design.md) (splits slice 9; settings shell and tab guards)
  - [`2026-09-29-backend-alignment-ba1-catalog-design.md`](2026-09-29-backend-alignment-ba1-catalog-design.md) (every command this slice calls)
- **Visual authority:** [`design-system/pos-cafe/pages/settings.html`](../../../design-system/pos-cafe/pages/settings.html), tab "Quản lý Thực đơn & Topping", its three modals and the Batch Linker; [`2026-09-20-admin-menu-topping-forms-design.md`](2026-09-20-admin-menu-topping-forms-design.md)
- **Domain authority:** [`CONTEXT.md`](../../../CONTEXT.md) (Retirement, Availability), ADR-048, ADR-058 through ADR-061

---

## 1. Purpose & Scope

Slice 9b gives a Manager the catalog administration the prototype designs: menu items
with prices and sizes, categories, modifier groups with their options, and the Batch
Linker that assigns a group to many items at once. BA-1 closed every backend gap
first, so **9b adds no Go code**.

### 1.1 In Scope

- Tab `/settings/catalog` with four views: Món & Định giá, Danh mục, Nhóm Topping,
  Ma trận Gán Topping (section 3).
- Three form modals (item, category, modifier group) whose single "Lưu" runs a
  planned sequence of existing commands (section 4, ADR-062).
- Item image upload with in-browser resize (section 5).
- The Batch Linker with a live POS preview built on the real item picker (section 6).
- Retirement with reason for items, sizes, categories, groups, and options (section 7).

One pull request for the whole slice, by decision at design time.

### 1.2 Out of Scope (Recorded Cuts)

| Prototype element | Why it is cut |
| --- | --- |
| "Size mặc định" radio in the item form | The backend has no default Size. The POS picker preselects the first available Size. |
| Preset image chips | Replaced by real upload (section 5). |
| `localStorage` / `pos-bus.js` cross-tab sync | Replaced by query invalidation (section 2.3). |
| Multi-size toggle when **editing** an item | Pricing mode is fixed at creation (ADR-060). The toggle shows only on create. |
| Showing or reinstating retired entities | No reinstate command exists (BA-1 1.2). Retired entities are hidden. |
| Tabs "Thông tin Quán & VietQR", "Tùy chỉnh In & Hệ thống" | BA-2 and the receipt question; they stay disabled planned tabs. |

---

## 2. Structure

### 2.1 Route and guard

- New route `web/src/routes/_app/settings/catalog.tsx`, rendered inside the existing
  `SettingsLayout`.
- `SETTINGS_TABS` in `features/settings/lib/tabs.ts` gains
  `{ to: "/settings/catalog", label: "Quản lý Thực đơn & Topping", icon: Utensils, capability: "catalog.administer_structure" }`;
  the `catalog` entry leaves `PLANNED_SETTINGS_TABS`. The per-tab guard from 9a
  (ADR-056) applies unchanged.
- The view is a search param, `?view=items|categories|groups|linker`, default
  `items`, validated in the route's `validateSearch`. Views share one capability, so
  they need no separate guards, and browser back works.
- The tab label carries a counter of non-retired items ("12 món"), as the prototype does.

### 2.2 Code layout

A new feature folder, `web/src/features/catalog/`, following the 9a pattern:

| Path | Job |
| --- | --- |
| `api/use-catalog-admin.ts` | Queries for manage menu and modifier groups; the plan runner hook; invalidation |
| `lib/save-plan.ts` | Pure planners: `planItemSave`, `planCategorySave`, `planGroupSave` |
| `lib/validation.ts` | Client-side form validation |
| `lib/item-inheritance.ts` | Effective groups of an item; exclusions pruned on category change |
| `lib/sellable-preview.ts` | Builds a `CatalogSellableItemResponse` for the POS preview |
| `lib/image-resize.ts` | Canvas resize and WebP encode |
| `lib/category-icons.ts` | The fixed set of Lucide icon names offered for categories |
| `components/` | Four views, three modals, `RetireDialog`, `SaveProgress`, linker columns |

### 2.3 Data

- **Read:** `GET /catalog/menu/manage` (categories → items → sizes, direct and
  excluded group ids, category group ids) and `GET /catalog/modifier-groups` (every
  group, including groups attached to nothing). Retired entities are filtered out on
  the client.
- **After any save** (success or partial failure): invalidate the manage menu,
  modifier groups, `menu/sellable`, and `menu/availability` queries, so the POS and
  the 9a tab see the change on their next render.

---

## 3. Views

Each follows `settings.html` for layout, cards, and tokens.

| View | Content |
| --- | --- |
| **Món & Định giá** | Search by name and code (stored code, else `getAcronym(name)`); category filter; card grid with image, code, badge, price or price range, and group count; "Thêm món" (Ctrl+N) |
| **Danh mục** | Cards ordered by `display_order` then name: icon, name, item count, default groups; edit and retire actions |
| **Nhóm Topping** | Cards: name, rule ("Chọn 1" when `max = 1`, else "Chọn min–max"), options with surcharge, count of items and categories using the group; create, edit, retire |
| **Ma trận Gán Topping** | Section 6 |

Loading, empty, and error states render from the real error envelope, as in every slice.

### 3.1 Item modal

- **Left column:** name; category select; pricing (single price, or size rows with
  name and price, where the mode toggle exists only on create); code, badge
  (none, BEST_SELLER, HOT, NEW, SIGNATURE, CHEF_PICK, with Vietnamese labels),
  description (≤ 300); image (section 5).
- **Right column:** "Kế thừa từ danh mục": the category's groups, each with a toggle
  that excludes it; "Gán thêm": chips for every other non-retired group.
- Changing the category recomputes the inherited list immediately and drops
  exclusions the new category does not provide (`item-inheritance.ts`), matching
  ADR-059 so the planned replace-set never fails with `INVALID_INHERITANCE`.
- Removing a saved size row marks it for retirement; the last remaining size cannot
  be removed.

### 3.2 Category modal

Name; icon picker over `category-icons.ts`; display order (0–9999); chips for default
modifier groups.

### 3.3 Modifier group modal

Name; "Chọn 1" / "Chọn nhiều" (choosing "Chọn 1" sets `max = 1`; the backend stores
only bounds); min and max steppers; option rows with name, surcharge, and a default
checkbox. `Enter` in a surcharge field appends a row and focuses its name. Removing a
saved option row marks it for retirement.

### 3.4 Keyboard and sound

Ctrl+N opens the item modal on the items view; Ctrl+S saves the open modal or the
linker; Esc closes a modal. Sounds use the existing `@/lib/sound` helpers.

---

## 4. Saving a form (ADR-062)

A form maps to several independent commands. "Lưu" does not call one endpoint; it
plans and runs a sequence.

### 4.1 Planning

`plan…Save(snapshot, form)` is a pure function. `snapshot` is the server state when
the modal opened (or `null` for create). It returns `Step[]`:

```ts
interface Step {
  label: string;          // Vietnamese, e.g. "Đổi giá Size L"
  needsPin: boolean;      // command requires manager_pin
  run(ctx: { pin?: string; ids: CreatedIds }): Promise<void>;
}
```

Each `run` generates its own `request_id` with `newRequestId()`. Unchanged fields
produce no step; an unchanged form produces an empty plan and "Lưu" only closes.

### 4.2 Fixed order

| Form | Order |
| --- | --- |
| Item, edit | rename → move category → reprice item, or rename/reprice sizes → add sizes → retire sizes → details → image → modifier groups |
| Item, create | `POST /items` (price or sizes) → details → image → modifier groups, on the new id |
| Category | create or rename → details (icon, order) → modifier groups |
| Modifier group, create | `POST /modifier-groups` with options and `default_option_names` |
| Modifier group, edit | rename → rename/reprice options → add options → selection rule (min, max, defaults) → retire options |

Why the order matters:

- **Move before modifier groups:** `excluded_group_ids` must be a subset of the
  **new** category's groups.
- **Add options before the selection rule:** a raised `max` may need the new options
  to count.
- **Selection rule before retiring options:** retirement does not re-check bounds or
  defaults, so the rule must already exclude the options being retired.

### 4.3 PIN

If any step has `needsPin`, the existing `ManagerApprovalDialog` opens **once**
before the first step runs. The PIN lives only in the runner's local variable for
that run and is never stored. A wrong PIN fails the step like any other error.

### 4.4 Partial failure and retry

- The runner stops at the first failing step and keeps the modal open. A
  `SaveProgress` list shows each step as done (✓), failed (✗ with the message from
  `error-messages.ts`), or not run.
- The queries are refetched (section 2.3).
- Pressing "Lưu" again **re-plans against the refreshed snapshot**. Steps that
  already succeeded no longer differ and drop out. This is also safe when a request
  timed out after the server committed, because the refetch shows the true state.
- When the create step succeeded, the modal switches to edit mode on the new id.
- Re-planning matches a form's unsaved size or option row to an existing one with the
  same name, so a row created by a step that succeeded is not added twice. A completed
  image upload resets the form's image to "keep", because a blob cannot be diffed
  against a stored image.

### 4.5 Client validation

Before planning, and shown on the field without a server call: name non-empty; a
price is whole VND from 1 to 2,147,483,647 (the backend's `ValidatePrice`) and a
surcharge from 0 to 2,147,483,647; size and option names are unique within their
item or group; at least one size; code matches `^[a-z0-9]{1,12}$`
after trim and lowercase; description ≤ 300; `0 ≤ min ≤ |defaults| ≤ max`,
`max ≥ 1`, and `max ≤` the number of options remaining after the save.

### 4.6 Accepted limitation

The diff baseline is the snapshot taken when the modal opened. A change made on
another terminal to the same entity in the meantime can be overwritten (details and
replace-set commands replace whole values). A cafe has one Manager editing the menu,
so this is accepted and marked with a `ponytail:` comment in `save-plan.ts`.

---

## 5. Images

- A file input accepts any image. `image-resize.ts` draws it to a `<canvas>` with its
  long edge at most 800 px and encodes `toBlob("image/webp", q)`, starting at
  `q = 0.85` and lowering until the blob is ≤ 1 MiB (the BA-1 cap).
- The upload happens only in the plan's image step: `PUT /catalog/items/{id}/image`,
  multipart with `request_id` and `file`, through the shared axios instance.
- "Gỡ ảnh" plans `DELETE /catalog/items/{id}/image`.
- No image library is added.

---

## 6. Batch Linker

Three columns, as the prototype.

1. **Groups rail:** every non-retired group, with its count of directly assigned items.
2. **Target tree:** items grouped by category.
   - A category checkbox edits `category_ids`; an item checkbox edits `item_ids`.
   - An item that receives the group through its category shows a "Kế thừa" label.
   - An item that **excludes** the group is disabled with "Đang loại trừ, bỏ loại trừ
     trong form Món", because assigning it is `INVALID_INHERITANCE` (BA-1 4.3).
   - "Chọn tất cả món X" ticks every enabled item of category X.
   - "LƯU ÁP DỤNG NGAY" sends one `PUT /modifier-groups/{id}/assignments`. No PIN.
3. **POS preview:** `ItemPickerDialog` is split into `ItemPickerBody` (the content)
   and the dialog shell, with no change to POS behavior. The preview renders
   `ItemPickerBody` for the focused item with a `CatalogSellableItemResponse` built by
   `sellable-preview.ts` from the manage menu plus the pending, unsaved selection. The
   preview is therefore the real picker, not a copy that can drift.

Switching groups with unsaved changes asks for confirmation.

---

## 7. Retirement

One `RetireDialog` serves items, sizes, categories, groups, and options:

- It shows the entity name and "Không thể hoàn tác. Lịch sử bán hàng vẫn được giữ."
- The reason is one of `NO_LONGER_OFFERED` ("Không bán nữa"), `MENU_RESTRUCTURE`
  ("Sắp xếp lại thực đơn"), `OTHER` ("Khác"); `OTHER` requires a note.
- Item, category, and group retirement run immediately from the card or the modal's
  "Xóa" button. Size and option retirement collect their reason in the dialog and
  run as steps of the form's plan.

---

## 8. Decisions and Documentation

- **ADR-062**: *A web form save is a planned sequence of existing commands, not one
  atomic command.* The plan is a diff of the form against the snapshot; recovery from
  a partial failure re-plans against refreshed server state. Rejected: a composite
  backend command per form (it would merge capabilities and audit events that
  ADR-048 keeps one per intent), and one save button per form section (it departs
  from the prototype's single save).
- `ROADMAP.md`: link this spec; mark 9b done after UAT.
- `features/settings/lib/tabs.ts`: `catalog` moves from planned to real.

---

## 9. Testing

Per the slice sequence's definition of done: `bun test` for pure logic and stores
only, no end-to-end tests.

- `save-plan.test.ts`: step order for each form and mode; an unchanged form gives an
  empty plan; `needsPin` is set exactly on price-bearing steps; re-planning after a
  partial failure omits the completed steps; a successful create re-plans as an edit.
- `validation.test.ts`: every rule in section 4.5.
- `item-inheritance.test.ts`: effective groups; exclusions dropped on category change.
- `sellable-preview.test.ts`: inherited minus excluded plus direct, with pending
  linker changes applied.
- `image-resize.test.ts`: target dimension arithmetic. Canvas encoding is checked in UAT.
- Existing `ItemPickerDialog` and POS tests pass unchanged after the body split.

---

## 10. UAT Gate

Performed by the operator on the running app. Implementation stops here; completion
is not self-certified.

1. Create an item with two sizes, an image, a code, a badge, and a modifier group. It
   appears on the POS (F1), and its picker shows the sizes and group.
2. Edit it: rename, change one size's price (the PIN is asked **once**), add a size,
   retire a size. The POS reflects it; an earlier Completed Sale keeps its old price.
3. Move an item that excludes a group to a category that does not provide the group.
   The exclusion disappears after save.
4. Go offline in DevTools after the first step of a multi-step save. The progress list
   shows ✓ and ✗. Go online, press "Lưu" again: only the remaining steps run, and the
   audit log has no duplicate event.
5. Create a group with three options using Enter; set min 1, max 2, and a default.
   Selecting three defaults is blocked on the form.
6. In the linker, assign a group to category Trà and untick one item. The preview
   updates live; after saving, the POS matches. An item excluding the group is disabled.
7. Create a category with an icon and order; retire it with reason "Khác" and no note:
   blocked until a note is entered.
8. As Cashier and as Barista, the tab is absent, and opening `/settings/catalog`
   directly is refused by the guard.
