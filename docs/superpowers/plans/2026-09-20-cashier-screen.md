# Cashier POS Screen Prototype Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a standalone, high-throughput Cashier POS touchscreen interface in `design-system/pos-cafe/index.html` implementing the domain rules from `CONTEXT.md` (takeaway service numbers, sizes, modifiers, open shifts, cash denominations, VietQR, dine-in table rounds) with minimum staff keystrokes and anti-slop Light Mode visual design.

**Architecture:** A self-contained, zero-dependency HTML5 application using Tailwind CSS CDN, Lucide Icons, and an embedded reactive state engine (vanilla JS). The layout provides a 3-zone cockpit: Top Bar (Shift/Status/Modes), Left/Center (Menu Catalog & Floor Layout, 62%), Right (Order Bill & 1-Tap Quick Checkout, 38%).

**Tech Stack:** HTML5, Tailwind CSS (via CDN with custom color tokens), Lucide Icons, Google Fonts (`Outfit` and `JetBrains Mono`), Vanilla JavaScript.

**Spec:** [`docs/superpowers/specs/2026-09-20-cashier-screen-design.md`](../specs/2026-09-20-cashier-screen-design.md)

## Global Constraints

- **Theme:** Crisp Light Mode (`bg-slate-50`, `bg-white`, `border-slate-200`, `text-slate-900`, `text-slate-600`).
- **Accent:** Emerald Green (`#059669` / `#10b981`) for Money, Checkout CTA, and Change Due.
- **Typography:** `Outfit` for display/UI, `JetBrains Mono` for all currency (VND) numbers and counters.
- **Money formatting:** Whole VND numbers formatted with dot thousand-separators (e.g. `35.000 đ`).
- **Touch target:** Minimum `48px` x `48px` for all interactive buttons and cards.
- **Zero-Dependency:** File must execute directly when double-clicked or opened in any modern browser without a build step.
- **Banned Patterns:** No em-dashes (`—`), no AI-purple gradients, no generic 3-card equal feature rows.

---

### Task 1: Core Shell, Top Bar, State Engine & Catalog Mock Data

**Files:**
- Create: `design-system/pos-cafe/index.html`
- Test: Open file or verify HTML structure via Node script.

**Interfaces:**
- Produces: `window.POS_STATE` reactive store containing:
  - `activeShift`: `{ id: "SHIFT-20260920-01", cashierName: "Nguyễn Thu Ngân", cashFundVND: 2000000 }`
  - `mode`: `'takeaway' | 'dinein'`
  - `serviceNumber`: `14`
  - `cart`: Array of items
  - `categories`: Array of category definitions
  - `menuItems`: Array of full menu item objects
  - `tables`: Array of table status objects

- [ ] **Step 1: Create initial HTML skeleton with Tailwind CDN, Google Fonts, and Lucide Icons**
Write `design-system/pos-cafe/index.html` with doctype, meta tags, Google Fonts (`Outfit`, `JetBrains Mono`), Tailwind CSS script, and Lucide Icons script.

- [ ] **Step 2: Implement State Engine and Mock Data**
Embed JavaScript store containing realistic cafe data:
- Categories: `all` (Tất cả), `coffee_vn` (Cà phê Việt), `coffee_machine` (Cà phê máy), `tea` (Trà & Macchiato), `freeze` (Đá xay & Sinh tố), `bakery` (Bánh & Điểm tâm).
- Menu items: Cà phê sữa đá, Cà phê đen đá, Bạc xỉu, Cold Brew cam sả, Espresso, Latte, Trà đào cam sả, Trà vải hoa hồng, Freeze trà xanh, Croissant bơ tỏi, Tiramisu.
- Tables for Dine-in: T01 - T08 (Tầng 1), T09 - T14 (Tầng 2), S01 - S05 (Sân vườn).

- [ ] **Step 3: Implement Top Bar Component**
Render header (height: 56px, `bg-white border-b border-slate-200`):
- Logo & App Name: "POS Cafe"
- Mode Switcher Pills: `[Mang đi (F1)]` and `[Tại bàn (F2)]`
- Quick search bar with shortcut hint `/`
- Shift Badge: `Ca #04 - Nguyễn Thu Ngân`
- Real-time digital clock

- [ ] **Step 4: Verify rendering in browser**
Run simple verification to confirm HTML syntax and valid markup.

- [ ] **Step 5: Commit**
```bash
git add design-system/pos-cafe/index.html
git commit -m "feat(ui): create core shell, top bar and state engine for cashier pos"
```

---

### Task 2: Menu Catalog Grid & Category Filter

**Files:**
- Modify: `design-system/pos-cafe/index.html`

**Interfaces:**
- Consumes: `POS_STATE.categories`, `POS_STATE.menuItems`, `POS_STATE.activeCategory`, `POS_STATE.searchQuery`.
- Produces: `renderCatalog()`, `setCategory(id)`, `filterItems(query)`, `handleItemClick(itemId)`.

- [ ] **Step 1: Build Category Pill Filter Bar**
Horizontal flex row of tactile pill buttons with active state highlighting (`bg-slate-900 text-white` when active, `bg-white text-slate-700 hover:bg-slate-100 border border-slate-200` when inactive).

- [ ] **Step 2: Build Responsive Product Card Grid**
Grid layout (`grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-3 p-4 overflow-y-auto`):
- Card: `bg-white rounded-xl border border-slate-200 p-3 flex flex-col justify-between hover:border-slate-400 active:scale-[0.98] transition cursor-pointer select-none min-h-[110px]`.
- Top: Item name (semi-bold, clean text), Category tag.
- Bottom: Price formatted in whole VND (`font-mono font-bold text-slate-900`), Size badge if item has multiple sizes.

- [ ] **Step 3: Implement Quick Search & Acronym Matching**
Add search handler that matches both full text and Vietnamese acronyms (e.g. `cfsd` matches "Cà phê sữa đá", `tdcs` matches "Trà đào cam sả").

- [ ] **Step 4: Connect Card Clicks**
If item has no size/modifiers: directly invoke `addToCart(item, defaultOptions)`.
If item has size/modifiers: invoke `openModifierModal(item)`.

- [ ] **Step 5: Verify category switching and item filtering**
Test switching between categories and filtering via search.

- [ ] **Step 6: Commit**
```bash
git add design-system/pos-cafe/index.html
git commit -m "feat(ui): implement menu catalog grid and category filtering"
```

---

### Task 3: Modifier Popover with Smart Defaults

**Files:**
- Modify: `design-system/pos-cafe/index.html`

**Interfaces:**
- Consumes: `selectedItemForModifier`.
- Produces: `openModifierModal(item)`, `closeModifierModal()`, `confirmModifierSelection()`.

- [ ] **Step 1: Build Modal Container & Backdrop**
Modal overlay (`fixed inset-0 z-50 bg-slate-900/40 backdrop-blur-xs flex items-center justify-center p-4`).
Modal dialog (`bg-white rounded-2xl shadow-2xl border border-slate-200 w-full max-w-lg overflow-hidden flex flex-col max-h-[90vh]`).

- [ ] **Step 2: Build Option Selection Groups**
- **Size Selection:** Radio chips (`S: 29.000 đ`, `M: 35.000 đ (Mặc định)`, `L: 42.000 đ`).
- **Mức ngọt:** Chips (`0%`, `30%`, `50%`, `70%`, `100% (Mặc định)`).
- **Mức đá:** Chips (`Nóng`, `Không đá`, `50% đá`, `100% đá (Mặc định)`).
- **Topping thêm:** Checkbox chips with surcharges (`+ Thạch cà phê (10.000 đ)`, `+ Kem Cheese (12.000 đ)`, `+ Trân châu trắng (10.000 đ)`).
- **Ghi chú pha chế:** Quick-tags (`Ít sữa`, `Pha đậm`, `Để đá riêng`) and text input for custom barista instructions.

- [ ] **Step 3: Calculate Dynamic Total & Add Action**
Bottom footer bar showing live calculated item total (`font-mono font-bold text-lg text-emerald-700`).
Primary CTA: `[Thêm vào đơn - 45.000 đ (Enter / Space)]`.

- [ ] **Step 4: Wire Keyboard Shortcuts**
`Space` / `Enter`: Confirm and add to cart.
`Esc`: Cancel and close modal.

- [ ] **Step 5: Verify Modifier Modal Flow**
Select item, change sugar/ice, toggle toppings, verify calculated total and cart addition.

- [ ] **Step 6: Commit**
```bash
git add design-system/pos-cafe/index.html
git commit -m "feat(ui): implement modifier popover with smart defaults and quick tags"
```

---

### Task 4: Order Bill, Stepper & Financial Totals

**Files:**
- Modify: `design-system/pos-cafe/index.html`

**Interfaces:**
- Consumes: `POS_STATE.cart`, `POS_STATE.serviceNumber`, `POS_STATE.selectedTable`.
- Produces: `renderCart()`, `updateItemQty(cartItemId, delta)`, `removeCartItem(cartItemId)`, `clearCart()`, `getFinancialTotals()`.

- [ ] **Step 1: Build Order Bill Header**
Display order context:
- If Takeaway: `Đơn mang đi #14` with a badge `Mang đi`.
- If Dine-in: `Bàn T02 (Tầng 1)` with seating and duration info.
- Action button: `[Hủy đơn / Làm mới]` with confirmation.

- [ ] **Step 2: Build Cart Item List**
Scrollable list of ordered items:
- Item name + Size tag (`Cà phê sữa đá (M)`).
- Modifiers & Note summary line (`50% ngọt, 100% đá, + Thạch cafe, Note: Để đá riêng`).
- Quantity stepper: `[-]` `[Qty]` `[+]` with large click targets.
- Line total in bold `font-mono text-slate-900`.
- Delete button `[Trash icon]` with subtle hover state.

- [ ] **Step 3: Implement Financial Equation Summary**
Clean summary block:
- `Tạm tính:` `[Subtotal VND]`
- `Giảm giá:` `0 đ`
- `TỔNG CỘNG:` Extra-large `font-mono font-bold text-2xl text-emerald-700` with dot formatting (`67.000 đ`).

- [ ] **Step 4: Verify Cart Updates**
Add items, adjust quantities, delete items, verify accurate total calculations.

- [ ] **Step 5: Commit**
```bash
git add design-system/pos-cafe/index.html
git commit -m "feat(ui): implement order bill, item stepper and financial totals"
```

---

### Task 5: Quick Cash & VietQR Settlement Flow

**Files:**
- Modify: `design-system/pos-cafe/index.html`

**Interfaces:**
- Consumes: `getFinancialTotals().grandTotalVND`.
- Produces: `renderQuickPayment()`, `selectPaymentMethod('cash' | 'qr')`, `selectTenderedAmount(amount)`, `completeCheckout()`.

- [ ] **Step 1: Build Payment Method Tabs**
Tabs: `[Tiền mặt (Cash)]` and `[Chuyển khoản VietQR]`.

- [ ] **Step 2: Implement Dynamic Cash Denomination Calculation**
Given `grandTotalVND` (e.g. `67.000`):
Generate smart buttons:
1. `[Đúng tiền: 67.000 đ]`
2. `[70.000 đ]` (rounded to next 10k)
3. `[100.000 đ]`
4. `[200.000 đ]`
5. `[500.000 đ]`
Clicking any button sets `tenderedVND` and instantly calculates `changeDueVND = tenderedVND - grandTotalVND`.

- [ ] **Step 3: Render High-Contrast Change Due Display**
Prominent emerald-tinted container:
- Label: "Tiền thừa trả khách"
- Value: `33.000 đ` in extra-bold `font-mono text-xl text-emerald-800`.

- [ ] **Step 4: Build VietQR Payment Tab**
When VietQR tab is active:
- Render VietQR code SVG/image containing bank info (MB Bank / Vietcombank, STK: `999988888`, Chủ TK: `POS CAFE`), exact amount `67.000 đ`, content `POS14`.
- Notice: "Vui lòng kiểm tra thông báo tiền vào trước khi xác nhận".

- [ ] **Step 5: Implement 1-Tap Checkout Action (Commit + Pay + Submit)**
Large CTA button: `[THU TIỀN & GỬI PHA CHẾ (Enter)]`:
- On click:
  - Display success feedback notification (Toast: "Đã thu 67.000 đ - Đơn #14 đã gửi Barista pha chế!").
  - Trigger receipt preview popup.
  - Increment `serviceNumber` to `#15`.
  - Reset cart and payment fields in < 200ms.

- [ ] **Step 6: Verify Cash and QR Payment Flow**
Test with various bill amounts, verify change calculations, and test fast completion.

- [ ] **Step 7: Commit**
```bash
git add design-system/pos-cafe/index.html
git commit -m "feat(ui): implement smart quick cash and vietqr settlement flow"
```

---

### Task 6: Dine-In Floor Plan & Table Management View

**Files:**
- Modify: `design-system/pos-cafe/index.html`

**Interfaces:**
- Consumes: `POS_STATE.tables`, `POS_STATE.selectedTable`.
- Produces: `renderFloorPlan()`, `selectTable(tableId)`, `submitTableRound()`, `settleTableBill()`.

- [ ] **Step 1: Build Floor Plan View Container**
When `operatingMode === 'dinein'`:
The center 62% area replaces the catalog with the Floor Plan:
- Area filter pills: `[Tất cả]`, `[Tầng 1 (8 bàn)]`, `[Tầng 2 (6 bàn)]`, `[Sân vườn (5 bàn)]`.

- [ ] **Step 2: Render Interactive Table Cards**
Table grid (`grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-4 p-4`):
- Table Card components:
  - **Trống (Available):** Clean white, dashed border, "+ Chọn bàn".
  - **Đang phục vụ (Occupied):** Indigo accent, seated duration (e.g. `35 phút`), item count, running subtotal.
  - **Chờ thanh toán (Billing):** Amber accent, pulsing border.

- [ ] **Step 3: Connect Table Ordering and Rounds**
Clicking a table opens its bill in the right panel:
- If empty: start new session, allow adding items from quick menu or toggle catalog.
- If occupied: view existing items (Round 1), allow adding new items (Round 2).
- Action button: `[Gửi bếp thêm món (Submit Round)]` (sends round to preparation without closing bill).

- [ ] **Step 4: Connect Table Settlement**
Clicking `[Thanh toán bàn]`:
- Opens Quick Cash / VietQR drawer for the table.
- Completing payment clears the table back to `Trống (Available)`.

- [ ] **Step 5: Verify Floor Plan Interactions**
Test switching between Takeaway and Dine-in, selecting tables, adding rounds, and settling table bills.

- [ ] **Step 6: Commit**
```bash
git add design-system/pos-cafe/index.html
git commit -m "feat(ui): implement floor plan and dine-in table rounds management"
```

---

### Task 7: Keyboard Hotkeys, Audio-Visual Feedback & Receipt Preview Modal

**Files:**
- Modify: `design-system/pos-cafe/index.html`

**Interfaces:**
- Consumes: Document keydown events, audio synthesizers.
- Produces: Global keyboard listener, Web Audio API beep feedback, printable receipt modal.

- [ ] **Step 1: Implement Global Keyboard Shortcut Listeners**
Attach event listener to `window`:
- `F1`: Switch to Takeaway mode.
- `F2`: Switch to Dine-in mode.
- `/` or `Ctrl+K`: Focus search input (prevent default `/` typing).
- `Space` / `Enter`: Context-sensitive confirmation (modifier modal or checkout).
- `Esc`: Close modals or clear search.

- [ ] **Step 2: Add Web Audio API Feedback**
Tiny synthesized sound (pleasant short sine wave click/chirp) on button tap and checkout success (no external MP3 asset dependencies).

- [ ] **Step 3: Implement Receipt Preview Modal**
Crisp thermal-paper receipt preview modal showing:
- POS Cafe header & Address
- Số hiệu ca & Tên thu ngân
- Mã số phục vụ (`#14`) hoặc Số bàn (`Bàn T02`)
- Danh sách món, sizes, modifiers
- Tổng tiền, Tiền khách đưa, Tiền thừa
- Mã vạch / QR xác thực
- Nút "In hóa đơn (Ctrl+P)" và "Đóng [Esc]".

- [ ] **Step 4: Comprehensive End-to-End Verification**
Simulate complete cashier workday:
1. Fast takeaway order with Cà phê sữa đá + Croissant.
2. Quick cash checkout with 100k bill.
3. Dine-in table order on Table 02, adding a second round.
4. Settle Table 02 via VietQR.
5. Verify zero console errors, zero layout jumps, and instant UI response.

- [ ] **Step 5: Commit**
```bash
git add design-system/pos-cafe/index.html
git commit -m "feat(ui): finalize keyboard hotkeys, tactile audio feedback and receipt preview"
```
