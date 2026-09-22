# Design Specification: Web Slice 3 — POS-a: Sellable Menu & Order Draft (`./web`, Phase 11 UI)

- **Author:** Claude & Team
- **Date:** 2026-09-23
- **Status:** Draft, pending review
- **Phase:** Phase 11, UI half — see [`docs/backlog/phase-11-clients-over-lan-and-single-binary.md`](../../backlog/phase-11-clients-over-lan-and-single-binary.md)
- **Predecessors:**
  - [`2026-09-21-web-frontend-slice-sequence-design.md`](2026-09-21-web-frontend-slice-sequence-design.md) (slice sequence & definition of done)
  - [`2026-09-22-web-slice-2-shift-management-design.md`](2026-09-22-web-slice-2-shift-management-design.md) (shift management, request ID, manager approval store)
- **Visual authority:** [`design-system/pos-cafe/DESIGN.md`](../../../design-system/pos-cafe/DESIGN.md) and [`design-system/pos-cafe/index.html`](../../../design-system/pos-cafe/index.html)
- **Domain authority:** [`CONTEXT.md`](../../../CONTEXT.md), [`spec/decisions.md`](../../../spec/decisions.md), `internal/catalog/` (Phase 02), `internal/sales/` (Phase 05A)

---

## 1. Purpose & Scope

Slice 3 delivers the first third of the Cashier Terminal: **POS-a (Sellable Menu & Order Draft)** in `web/src/features/pos/`, replacing the placeholder view in `web/src/routes/_app/index.tsx` with an interactive, API-backed screen against Go `/catalog/menu/sellable` and `/sales/service-sessions/*` endpoints.

This slice reads the sellable menu, displays catalog items with real-time category filtering and search, starts anonymous Takeaway Service Sessions, and allows cashiers to assemble and edit Order Draft items (sizes, modifier options, preparation notes, quantities). It handles no money, records no payments, and submits no orders to preparation.

### 1.1 In Scope

1. **POS Terminal Screen Layout (`web/src/features/pos/components/pos-view.tsx`)**:
   - Replaces `web/src/features/pos/components/pos-terminal-view.tsx` at route `/_app/`.
   - Desktop/tablet two-column workspace adhering to `DESIGN.md`: Zone 1 Catalog Matrix (~62% width) and Zone 2 Order Bill (~38% width), with independent internal vertical scrolling.
2. **Sellable Menu Presentation (`web/src/features/pos/components/menu-grid.tsx`, `menu-item-card.tsx`)**:
   - Fetches sellable menu from `GET /catalog/menu/sellable` (server-filtered for active and available items, sizes, and modifier options).
   - Horizontal category filter pills rail with "Tất cả" (All) and category counts. Minimum touch target height 48px.
   - Quick search input filtering item names and diacritic-insensitive acronyms (e.g., `cfsd` for *Cà phê sữa đá*, `tdcs` for *Trà đào cam sả*) with hotkey `/`.
   - Product cards with media container, Lucide category icon fallback on broken image, starting price in JetBrains Mono, and tactile audio feedback (`playTapChirp()`).
3. **Order Draft Management (`web/src/features/pos/components/draft-panel.tsx`, `draft-item-row.tsx`)**:
   - Single-bill model per client tab: active Takeaway Service Session ID stored in `sessionStorage` (`pos_active_session_id`).
   - Lazy session initiation: the first item addition triggers `POST /sales/service-sessions/takeaway` with `newRequestId()`, persisting the session ID before/alongside item addition to prevent abandoned empty sessions.
   - Resuming draft on page refresh via `GET /sales/service-sessions/:id`.
   - Mappings for draft mutations returning full `ServiceSessionResponse` projection:
     - Add item: `POST /sales/service-sessions/:id/draft/items`
     - Update quantity: `PATCH /sales/service-sessions/:id/draft/items/:item_id/quantity`
     - Update size: `PATCH /sales/service-sessions/:id/draft/items/:item_id/size`
     - Update modifiers: `PATCH /sales/service-sessions/:id/draft/items/:item_id/modifiers`
     - Update preparation note: `PATCH /sales/service-sessions/:id/draft/items/:item_id/preparation-note`
     - Remove item: `DELETE /sales/service-sessions/:id/draft/items/:item_id` (passing `request_id` via query param per Go router contract)
4. **Item Configuration Dialog (`web/src/features/pos/components/item-picker-dialog.tsx`)**:
   - Modal dialog for configuring items with sizes or modifier groups (simple items with no sizes and no modifiers add directly with 1 tap).
   - Reusable for both adding a new item and editing an existing draft item row.
   - Size selection: required if item has sizes; exactly one selectable.
   - Modifier groups: enforces `min_selections` and `max_selections`, pre-selects `default_option_ids`, calculates surcharges.
   - Preparation note: input up to 200 Unicode code points (matching `MaxPreparationNoteLength`).
   - Live price calculation in JetBrains Mono.
5. **Shift State Handling (Shift Gate)**:
   - Reads `useCurrentShift()` from Slice 2.
   - When no shift is open (`state !== "OPEN"`): menu remains browsable, but the bill panel displays a prominent `NoShiftNotice` with a direct link to `/shift`, and item addition is prevented client-side.
6. **Seed Data Extension (`scripts/dev-seed.ts`)**:
   - Extends the script to authenticate as the bootstrapped Manager (`QL01`) and create realistic sample categories, modifier groups, and sellable items with sizes to enable full UAT verification.
7. **Pure Logic Unit Tests (`bun test`)**:
   - `web/src/features/pos/utils/pricing.test.ts`: item prices, sizes, surcharges, line totals, and draft subtotals.
   - `web/src/features/pos/utils/selection.test.ts`: modifier validation, min/max selection bounds, default selection resolution.

### 1.2 Out of Scope (Deliberate Omissions & Recorded Cuts)

- **Commit, Check, and Payments (Slice 4)**: The bill panel displays the calculated subtotal, but payment controls ("Thanh toán (F9)", payment method tabs) are displayed in a `disabled` state with helper text indicating they arrive in Slice 4.
- **Order Submission and Session Closure (Slice 5)**: The "Gửi bếp" (Submit to Kitchen) action is displayed in a `disabled` state.
- **Dine-In Mode and Table Management (Slice 7)**: The mode switcher displays "Mang đi" active, while "Tại bàn (F2)" is rendered in a `disabled` state with a "Sắp có" badge.
- **Bulk Clear Cart ("Hủy đơn")**: The backend provides no atomic endpoint to discard an active Order Draft or delete all items in bulk. The "Hủy đơn" header button is rendered in a `disabled` state. Staff remove unwanted items individually via the row delete button.
- **Open Tabs Switcher**: Browsing and switching between multiple open takeaway sessions (`GET /sales/service-sessions`) is deferred to Slice 5 when session closure is implemented.
- **Receipt Printing**: Out of scope per `web-frontend-slice-sequence-design.md` section 7.

---

## 2. Visual & Ergonomic Architecture

The implementation strictly follows the visual tokens and design rules established in `design-system/pos-cafe/DESIGN.md`:

### 2.1 Color Tokens
- **Canvas & Shell Base:** `#f8fafc` (Slate-50).
- **Surface & Containers:** `#ffffff` (White) for product cards, bill panel, and dialog body.
- **Substrate / Inactive Elements:** `#f1f5f9` (Slate-100) for inactive category pills and image wells.
- **Borders & Dividers:** `#e2e8f0` (Slate-200).
- **Typography Ink:** `#0f172a` (Slate-900) for item titles, headers, and monetary amounts. Never pure black (`#000000`).
- **Muted Steel:** `#64748b` (Slate-500) for secondary details, category badges, and notes.
- **Emerald Accent:** `#059669` / `#10b981` (Primary Emerald) for active category pills, quick-add buttons, and monetary total readouts.
- **Rose Destructive:** `#dc2626` / `#ef4444` for item removal.
- **Strictly Banned Colors:** Pure black (`#000000`), AI purple/violet neon glows.

### 2.2 Typography Hierarchy
- **Primary UI & Headings:** `Outfit`, sans-serif (Weights 500, 600, 700). High readability for Vietnamese diacritics.
- **Currency & Numerics:** `JetBrains Mono`, monospace (Weights 600, 700). Tabular figures to avoid jitter.
- **VND Formatting:** Strictly whole numbers with dot thousand-separators (e.g., `35.000 đ`, `105.000 đ`). Fractional decimals are strictly forbidden.
- **Strictly Banned Fonts:** `Inter` is banned for creative character; generic system serifs are banned.

### 2.3 Touch Ergonomics & Interaction
- **NO Sub-48px Touch Targets:** Every touchable component (category pills, product cards, size chips, modifier checkboxes, quantity steppers `[-]`/`[+]`, modal action buttons) maintains a minimum bounding box of `48px` (`min-h-[48px] min-w-[48px]`).
- **Tactile Depression:** All interactive elements feature immediate tactile feedback on press (`active:scale-[0.98]` or `active:translate-y-[1px]`).
- **Audio Feedback:** Real-time Web Audio API synthesis via `playTapChirp()` on item selection, pill tap, and quantity stepper clicks.
- **Spring Transitions:** Modal popovers enter with `fade-in zoom-in-95 duration-150`.
- **Image Fallback:** Product cards handle broken image URLs by immediately hiding the broken `img` element and displaying a themed category Lucide icon on a `#f1f5f9` background. Broken image placeholders or alt-text artifacts are forbidden.
- **No Emojis & No Em-Dashes:** Strictly enforced across all UI strings, badges, tooltips, and code comments.

---

## 3. Data Flow & State Lifecycle

```
                           [useCurrentShift()]
                                    |
                    +---------------+---------------+
                    |                               |
             state !== "OPEN"                 state === "OPEN"
                    |                               |
        Render NoShiftNotice in Bill       Render Order Draft Panel
        Disable item addition              Enable item addition
                    |                               |
                    +---------------+---------------+
                                    |
                        [GET /catalog/menu/sellable]
                                    |
                         Render Category Pills
                         Render Product Card Grid
                                    |
                             (Cashier Taps Card)
                                    |
                     +--------------+--------------+
                     |                             |
             Simple Item                   Has Sizes or Modifiers
       (no size, no modifiers)                     |
                     |                    Open <ItemPickerDialog>
                     |                    (Select size, modifiers, note, qty)
                     |                             |
                     +--------------+--------------+
                                    |
                        Check sessionStorage pointer
                                    |
                     +--------------+--------------+
                     |                             |
           No active session               Session ID exists
                     |                             |
         POST /service-sessions/takeaway           |
         (with newRequestId())                     |
         Save ID to sessionStorage                 |
                     |                             |
                     +--------------+--------------+
                                    |
                     POST /service-sessions/:id/draft/items
                     (with newRequestId())
                                    |
                     Server returns updated ServiceSessionResponse
                                    |
                     Write projection directly into Query Cache
                                    |
                     Update DraftPanel & Line Items
```

### 3.1 Session Persistence & Cache Coordination
Per **ADR-053**, server state stays on the server. There is no client-side `usePosStore` holding cart items or discounts.
- The single client pointer is `pos_active_session_id` in `sessionStorage`.
- The active session is fetched via `useGetSalesServiceSessionsId(sessionId)`.
- When an item is added, updated, or removed, the Go backend mutation returns the full `ServiceSessionResponse` projection.
- The API wrapper writes this returned projection directly into the React Query cache:
  ```typescript
  queryClient.setQueryData(getGetSalesServiceSessionsIdQueryKey(sessionId), rawEnvelope);
  ```
  This eliminates duplicate network round-trips while keeping the view strictly server-authoritative.

### 3.2 Command Idempotency
All mutating commands (`POST /takeaway`, `POST /draft/items`, `PATCH .../quantity`, `PATCH .../size`, `PATCH .../modifiers`, `PATCH .../preparation-note`, `DELETE .../draft/items/:item_id`) construct their payload using `withRequestId(...)` or pass a `newRequestId()` generated once when the user initiates the action. Retries on network timeouts reuse the exact same `request_id`.

---

## 4. Component Structure (`web/src/features/pos/`)

To prevent monolithic files and ensure isolation, `web/src/features/pos/` is divided into focused components under 300 lines each:

```
web/src/features/pos/
├── api/
│   ├── use-pos.ts                    # Hook wrappers over generated catalog & sales endpoints
│   └── use-pos.test.ts               # Tests for API seam wrappers and cache update semantics
├── components/
│   ├── pos-view.tsx                  # Root layout coordinator (2 columns, shift gate, dialog states)
│   ├── menu-grid.tsx                 # Category filter rail, search input, item card grid container
│   ├── menu-item-card.tsx            # Single product card with image/fallback, price, touch handling
│   ├── item-picker-dialog.tsx        # Modal for size, modifier groups, prep note, and quantity configuration
│   ├── draft-panel.tsx               # Order bill aside: header, scrollable item list, totals, disabled CTAs
│   ├── draft-item-row.tsx            # Single draft item row: name, size, modifiers, note, stepper, remove button
│   ├── no-shift-notice.tsx           # Empty/warning state in bill panel when sales shift is closed
│   └── draft-empty-state.tsx         # Empty state in bill panel when no items have been added yet
└── utils/
    ├── pricing.ts                    # Pure pricing functions: base price, surcharges, line total, subtotal
    ├── pricing.test.ts               # Unit tests for pricing logic
    ├── selection.ts                  # Pure selection functions: defaults, min/max rules, option toggling
    └── selection.test.ts             # Unit tests for modifier selection rules
```

---

## 5. Detailed Component Specifications

### 5.1 Menu Grid & Cards (`menu-grid.tsx`, `menu-item-card.tsx`)
- **Category Filter Rail:**
  - Horizontal scrolling container (`overflow-x-auto no-scrollbar`).
  - Pills have `min-h-[48px] px-4 rounded-xl font-semibold text-sm transition-all`.
  - Inactive state: `bg-white text-slate-700 border border-slate-200 hover:bg-slate-100`.
  - Active state: `bg-emerald-600 text-white shadow-xs`.
  - First pill is "Tất cả" showing aggregate item count. Subsequent pills represent categories from `CatalogSellableCategoryResponse`.
- **Quick Search Input:**
  - Placed above the category rail or beside it.
  - Height `48px`, `rounded-xl bg-white border border-slate-200 pl-10 pr-10 text-sm font-medium`.
  - Icon `<Search className="w-4 h-4 text-slate-400" />` left-aligned. Clear button right-aligned.
  - Shortcut badge `[/]` indicates pressing slash focuses the input.
  - Filter logic matches both normalized lowercase name and diacritic-stripped first-letter acronyms (e.g., `cfsd` matches *Cà phê sữa đá*).
- **Product Card (`menu-item-card.tsx`):**
  - Geometry: `rounded-2xl border border-slate-200 bg-white p-3 flex flex-col justify-between min-h-[210px] cursor-pointer hover:border-emerald-500 hover:shadow-sm active:scale-[0.98] transition-all`.
  - Media: top container `h-28 rounded-xl overflow-hidden bg-slate-100 relative flex items-center justify-center text-slate-400`.
    - Image tag with `onerror` hiding itself and revealing Lucide coffee/tea/utensils icon.
    - Floating size count badge when item has multiple sizes (e.g., `3 cỡ`).
  - Text: product name (`font-semibold text-slate-900 text-sm line-clamp-1 mt-2`), category subtitle (`text-xs text-slate-500`).
  - Price: formatted whole VND in `font-mono font-bold text-emerald-700 text-sm sm:text-base`. If item has sizes, displays starting price `Từ [min_price] đ`.

### 5.2 Item Configuration Dialog (`item-picker-dialog.tsx`)
- **Dialog Shell:**
  - Fixed backdrop `bg-slate-900/40 backdrop-blur-xs flex items-center justify-center p-4 z-50`.
  - Container `bg-white rounded-2xl shadow-2xl border border-slate-200 w-full max-w-lg flex flex-col max-h-[90vh] animate-in fade-in zoom-in-95 duration-150`.
- **Header:**
  - Product name, category badge, and close button (`h-12 w-12` minimum touch target).
- **Size Selector Section (when `sizes.length > 0`):**
  - Section title: "Chọn kích cỡ" (Bắt buộc chọn 1).
  - Size pills in a responsive grid: each chip has `min-h-[48px] px-4 rounded-xl border flex items-center justify-between font-medium text-sm transition-all`.
  - Selected state: `border-emerald-600 bg-emerald-50 text-emerald-900 font-bold ring-1 ring-emerald-600`.
  - Displays size name and corresponding price in JetBrains Mono.
- **Modifier Groups Section:**
  - Iterates over `modifier_groups[]`.
  - Group Header: group name plus selection rule indicator (e.g., `Chọn 1`, `Tùy chọn, tối đa 3`).
  - If `max_selections === 1` and `min_selections === 1`: radio behavior (clicking an option unselects other options in the group).
  - If `max_selections > 1`: checkbox behavior. Clicking an unselected option when count reaches `max_selections` is prevented.
  - Option Chip: `min-h-[48px] px-3.5 rounded-xl border flex items-center justify-between text-xs sm:text-sm`.
  - Displays option name and surcharge if `surcharge_vnd > 0` (e.g., `+5.000 đ`).
- **Preparation Note Section:**
  - Input field for special instructions with character counter `(N/200)`.
  - Trims whitespace and blocks input exceeding 200 code points.
- **Footer:**
  - Quantity Stepper: circular `[-]` and `[+]` buttons (`w-12 h-12 rounded-xl border border-slate-200 text-slate-700 flex items-center justify-center active:scale-95`), quantity readout in JetBrains Mono. Range: 1 to 9999.
  - Live calculated total in JetBrains Mono (`text-lg font-bold text-emerald-700`).
  - Action button: `min-h-[48px] h-12 px-6 rounded-xl bg-emerald-600 hover:bg-emerald-700 text-white font-bold text-sm shadow-xs active:scale-[0.98] transition-all flex items-center justify-center`. Disabled if any required modifier group fails its `min_selections` rule.

### 5.3 Order Bill Aside (`draft-panel.tsx`, `draft-item-row.tsx`)
- **Bill Header:**
  - Icon, title "Đơn mang đi #[service_number]" (or "Đơn mang đi" if session is not yet created), badge "Mang đi", subtitle "Khách mua mang về".
  - Action button: "Hủy đơn" with trash icon, displayed with `disabled` attribute and tooltip explaining single-item deletion.
- **Item List (`draft-items-container`):**
  - Scrollable area (`flex-1 overflow-y-auto p-4 space-y-2.5 bg-slate-50/50`).
  - Empty state (`draft-empty-state.tsx`) when draft has 0 items: cart icon, "Chưa có món nào trong đơn", prompt "Chọn món từ thực đơn bên trái để bắt đầu".
  - Draft Item Rows (`draft-item-row.tsx`):
    - Background `#ffffff`, `rounded-xl border border-slate-200 p-3 flex flex-col gap-2 shadow-2xs`.
    - Clicking row body reopens `<ItemPickerDialog>` populated with existing item configurations for editing.
    - Top line: product name, size name badge, remove button (`w-10 h-10` text-slate-400 hover:text-rose-600 rounded-lg flex items-center justify-center).
    - Modifiers subtitle: comma-separated list of selected modifier options with individual surcharges.
    - Preparation note: subtle italicized text with note icon.
    - Bottom line: unit price in JetBrains Mono, quantity stepper (`[-]` and `[+]` minimum 40px/48px hit area, quantity value), line total in bold JetBrains Mono.
    - Warning indicator if item has `available: false`: amber badge "Tạm hết hàng" warning cashier before checkout.
- **Financial Summary:**
  - Container: `border-t border-slate-200 bg-white p-4 space-y-1.5 flex-shrink-0`.
  - Row 1: "Tạm tính" (Subtotal) formatted in JetBrains Mono.
  - Row 2: "Tổng cộng" (Grand Total) formatted in JetBrains Mono `text-2xl font-bold text-emerald-700`.
- **Bill Actions / Checkout Drawer:**
  - Container: `border-t border-slate-200 bg-slate-50 p-4 space-y-2.5 flex-shrink-0`.
  - Dine-In Switcher: "Tại bàn (F2)" button disabled with "Sắp có" badge.
  - Payment Tabs: "Tiền mặt (Cash)" and "Chuyển khoản VietQR" displayed in `disabled` state.
  - CTA Button: "Thanh toán (F9)" with `min-h-[52px] w-full rounded-xl bg-slate-300 text-slate-500 font-bold cursor-not-allowed` and helper caption "Chức năng thanh toán sẽ mở ở Slice 4".

---

## 6. API Seam & Error Handling

### 6.1 Hook Wrapper Seam (`web/src/features/pos/api/use-pos.ts`)
Wraps generated Orval hooks from `@/api/generated/endpoints/catalog/catalog` and `@/api/generated/endpoints/sales/sales`:

```typescript
// Query hooks
export function useSellableMenu();
export function useServiceSession(sessionId: string | null);

// Mutation hooks (intent-scoped requestId, unwrapping envelope, query cache write)
export function useStartTakeawaySession();
export function useAddDraftItem(sessionId: string);
export function useUpdateDraftItemQuantity(sessionId: string);
export function useUpdateDraftItemSize(sessionId: string);
export function useUpdateDraftItemModifiers(sessionId: string);
export function useUpdateDraftItemPreparationNote(sessionId: string);
export function useRemoveDraftItem(sessionId: string);
```

### 6.2 Vietnamese Error Code Mapping (`web/src/lib/error-messages.ts`)
Extends `ERROR_MESSAGES` with stable error codes emitted by Go `internal/sales/errors.go`:

| Backend Error Code | Vietnamese User Message |
| :--- | :--- |
| `OPEN_SALES_SHIFT_REQUIRED` | Ca bán hàng chưa được mở. Vui lòng mở ca trước khi tạo đơn. |
| `SERVICE_SESSION_NOT_FOUND` | Phiên phục vụ không tồn tại hoặc đã bị xóa. |
| `SERVICE_SESSION_ALREADY_CLOSED` | Phiên phục vụ này đã kết thúc. |
| `EDITABLE_DRAFT_NOT_FOUND` | Không tìm thấy đơn nháp hợp lệ để chỉnh sửa. |
| `DRAFT_ITEM_NOT_FOUND` | Món trong đơn không tồn tại hoặc đã bị xóa. |
| `MENU_ITEM_NOT_FOUND` | Món không có trong thực đơn. |
| `MENU_ITEM_UNAVAILABLE` | Món này hiện đang tạm ngưng phục vụ. |
| `MENU_ITEM_RETIRED` | Món này đã ngừng kinh doanh. |
| `SIZE_NOT_FOUND` | Kích cỡ món không hợp lệ. |
| `SIZE_UNAVAILABLE` | Kích cỡ đã chọn hiện đang tạm hết. |
| `SIZE_RETIRED` | Kích cỡ này đã ngừng phục vụ. |
| `MODIFIER_OPTION_NOT_FOUND` | Tùy chọn topping không tồn tại. |
| `MODIFIER_OPTION_UNAVAILABLE` | Tùy chọn topping đã chọn hiện đang tạm hết. |
| `MODIFIER_OPTION_RETIRED` | Tùy chọn topping này đã ngừng phục vụ. |
| `INVALID_PREPARATION_NOTE` | Ghi chú pha chế không hợp lệ (tối đa 200 ký tự). |
| `INVALID_QUANTITY` | Số lượng món phải từ 1 đến 9999. |

---

## 7. Development Seed Script Extension (`scripts/dev-seed.ts`)

To satisfy Definition of Done requirement 5 (closing the seed data gap), `scripts/dev-seed.ts` is expanded to seed sample catalog entities via authentic API calls:

1. **Manager Sign-In:** Authenticates as `QL01` / PIN `1234` via `POST /auth/sign-in` to acquire a JWT token with `RoleManager` capabilities.
2. **Categories (`POST /catalog/categories`):**
   - `Cà phê` (Coffee)
   - `Trà trái cây` (Fruit Tea)
   - `Bánh ngọt` (Pastries)
3. **Modifier Groups (`POST /catalog/modifier-groups`):**
   - Group 1: `Mức đường` (Sugar Level), `min_selections: 1, max_selections: 1`, options: `100% đường` (default), `70% đường`, `50% đường`, `Không đường`. Surcharges: 0 VND.
   - Group 2: `Topping thêm` (Add-ons), `min_selections: 0, max_selections: 3`, options: `Trân châu trắng` (+5.000 đ), `Thạch nha đam` (+5.000 đ), `Kem phô mai` (+10.000 đ).
4. **Items (`POST /catalog/items`):**
   - Simple Item: `Croissant bơ tỏi` (35.000 đ, no sizes, no modifiers).
   - Item with Sizes: `Cà phê đen` (Size S: 25.000 đ, Size M: 29.000 đ, Size L: 35.000 đ).
   - Configurable Item: `Cà phê sữa đá` (Sizes S/M/L, linked to `Mức đường` and `Topping thêm`).
   - Configurable Item: `Trà đào cam sả` (Sizes M/L, linked to `Mức đường` and `Topping thêm`).
5. **Idempotency:** The script handles re-runs cleanly by catching `409 CONFLICT` when categories or items already exist.

---

## 8. Unit Testing Strategy

In compliance with the slice definition of done, tests cover pure logic and state calculations using `bun test`. No browser integration or end-to-end testing frameworks (Cypress, Playwright, Vitest) are introduced.

### 8.1 `web/src/features/pos/utils/pricing.test.ts`
- Calculates base item price correctly for items without sizes.
- Calculates correct starting price for items with multiple sizes.
- Computes configured item unit price as `size_price + sum(option_surcharges)`.
- Computes item line total as `unit_price * quantity`.
- Computes draft subtotal as the sum of all item line totals.

### 8.2 `web/src/features/pos/utils/selection.test.ts`
- Resolves default option IDs on initial modal load.
- Validates that single-choice modifier groups (`min=1, max=1`) enforce radio behavior.
- Validates that multi-choice modifier groups respect `max_selections`.
- Validates that an item configuration is valid only when all required groups meet `min_selections`.
- Normalizes and trims preparation notes, truncating or rejecting notes over 200 code points.

---

## 9. Definition of Done & UAT Handover

### 9.1 Verification Checklist
- [ ] Placeholder `pos-terminal-view.tsx` completely replaced with real API-backed screen.
- [ ] No mock data remains in the active execution path.
- [ ] Catalog reads from real `GET /catalog/menu/sellable`.
- [ ] Item additions, edits, and removals call real `/sales/service-sessions/*` endpoints.
- [ ] Every mutation carries an intent-scoped `request_id` generated via `newRequestId()`.
- [ ] Strict compliance with `design-system/pos-cafe/DESIGN.md` (colors, typography, touch targets >= 48px, whole VND, audio feedback).
- [ ] `bun test` passes with zero failures across all new unit tests.
- [ ] `bun run lint` (`oxlint`) and `tsc -b` pass with zero errors.

### 9.2 UAT Gate Handover Script
1. **Setup:** Run `make dev-seed` to ensure Manager account and sample catalog items exist.
2. **Shift Gate Verification:**
   - Sign in as `QL01` / `1234`.
   - Navigate to `/` (Bán hàng).
   - If no shift is open: verify the Sellable Menu is displayed, but the bill area displays "Chưa mở ca bán hàng" with an action button to open a shift. Verify adding items is blocked.
3. **Open Shift & Begin Order:**
   - Navigate to `/shift`, open a shift with float `2.000.000 đ`.
   - Return to `/` (Bán hàng). Bill area now displays an empty order state.
4. **Simple Item Addition (1-Tap):**
   - Click "Croissant bơ tỏi".
   - Verify an anonymous Takeaway Service Session is opened, bill header updates to "Đơn mang đi #1", and item appears in the bill with quantity 1 and price `35.000 đ`.
5. **Configurable Item Addition (Modal Flow):**
   - Click "Cà phê sữa đá".
   - Verify `<ItemPickerDialog>` opens with smooth animation.
   - Verify size chips are displayed with Size S/M/L and minimum 48px height.
   - Select Size L (+6.000 đ), select "50% đường", select "Trân châu trắng" (+5.000 đ).
   - Type note: `Ít đá, mang đi xa`.
   - Set quantity to 2 using `[+]` button.
   - Verify live price reflects `(base + surcharges) * 2`.
   - Click "Thêm vào đơn". Verify line item appears in bill with detailed summary.
6. **In-Draft Item Modification:**
   - On the bill, tap `[+]` on Croissant. Verify quantity increases to 2 and subtotal updates.
   - Tap the "Cà phê sữa đá" row. Verify the configuration dialog reopens populated with the existing selections. Change to "70% đường" and click "Cập nhật". Verify bill row updates.
7. **Item Removal:**
   - Click the delete trash icon on Croissant. Verify Croissant is removed from the draft and grand total recalculates.
8. **Search & Category Navigation:**
   - Type `cfsd` or `tra` into the search bar. Verify the product grid filters in real-time.
   - Click category pill "Bánh ngọt". Verify only pastry items are displayed.
9. **Persistence Check:**
   - Refresh the browser (`F5`).
   - Verify the active order (#1) and its draft items are restored exactly as before from `sessionStorage` and `GET /sales/service-sessions/:id`.
