# POS Cafe Multi-Screen Suite Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the complete 6-screen POS Cafe client suite in `design-system/pos-cafe/pages/` with real-time cross-tab synchronization, tactile feedback, and Crisp Light aesthetics.

**Architecture:** Multi-page modular HTML5 applications sharing an event bus (`BroadcastChannel('pos_cafe_bus')` and `localStorage`). Each screen is self-contained with Tailwind CSS CDN, Lucide Icons, and Google Fonts (Outfit + JetBrains Mono), adhering to touch-first ergonomics (>= 48px targets) and whole-VND financial integrity.

**Tech Stack:** HTML5, Tailwind CSS, Lucide Icons, Google Fonts (Outfit, JetBrains Mono), Web Audio API, BroadcastChannel API.

**Spec:** `docs/superpowers/specs/2026-09-20-pos-screens-suite-design.md`

## Global Constraints

- Theme: Crisp Light Mode (`bg-slate-50`, `bg-white`, `border-slate-200`, `text-slate-900`, `text-slate-500`).
- Accents: Emerald (`#059669` / `#10b981`), Indigo (`#4f46e5`), Amber (`#d97706`), Rose (`#dc2626`).
- Typography: Outfit for UI, JetBrains Mono for numbers, currency, timestamps, and codes.
- Whole VND formatting: e.g. `35.000 đ` (never decimal points).
- Touch targets: >= 48px minimum height and width for all interactive elements.
- Zero-dependency: Standalone HTML5 runnable directly in any modern browser.
- Banned: No emojis, no em-dashes, no AI-purple gradients, no pure black `#000000`.

---

### Task 1: Navigation Hub & Shared Cross-Tab Sync Bus

**Files:**
- Create: `design-system/pos-cafe/shared/pos-bus.js`
- Modify: `design-system/pos-cafe/index.html`
- Test: `tests/test-bus.js` or node verification script

**Interfaces:**
- Produces: `window.POS_BUS` helper with `publish(event, payload)`, `subscribe(event, callback)`, `getOrders()`, `saveOrder(order)`, `getActiveShift()`, `saveActiveShift(shift)`.
- Global Nav: Floating station switcher button `[Trạm]` in top bar linking to all screens with hotkeys `[F1]` to `[F7]`.

- [ ] **Step 1: Create shared/pos-bus.js**
Implement cross-tab state syncing via BroadcastChannel and localStorage fallbacks.
- [ ] **Step 2: Add Station Switcher to index.html**
Integrate station navigation dropdown/modal in top bar of the Cashier screen.
- [ ] **Step 3: Verify bus events**
Test dispatching and receiving events in Node environment.
- [ ] **Step 4: Commit changes**
`git add design-system/pos-cafe/shared/pos-bus.js design-system/pos-cafe/index.html`
`git commit -m "feat(ui): add cross-tab real-time sync bus and navigation hub"`

---

### Task 2: Screen 1 - Staff PIN Auth & Workspace Session Lock (`pages/auth.html`)

**Files:**
- Create: `design-system/pos-cafe/pages/auth.html`
- Test: node verification script

**Interfaces:**
- Consumes: `window.POS_BUS`
- Produces: Authenticated staff session in `localStorage.POS_CURRENT_STAFF`, redirects to selected workspace (`index.html`, `kds.html`, `shift.html`, `tables.html`).
- Modal Mode: Supports `?mode=manager_approval` with postMessage callback.

- [ ] **Step 1: Build auth.html layout**
Centered light card with PIN dots, 3x4 touch numpad (keys >= 64px), staff avatar shortcuts (`1234` Cashier, `5678` Barista, `8888` Manager, `2468` Waiter).
- [ ] **Step 2: Implement PIN validation & Workspace routing**
Handles 4-digit PIN entry, tactile click chirp, invalid shake animation, and workspace navigation.
- [ ] **Step 3: Implement Manager Approval popup mode**
Handles URL param `?mode=manager_approval` to authorize elevated actions.
- [ ] **Step 4: Verify touch targets and zero em-dashes**
Run automated checks.
- [ ] **Step 5: Commit changes**
`git add design-system/pos-cafe/pages/auth.html`
`git commit -m "feat(ui): implement staff pin auth and workspace session lock screen"`

---

### Task 3: Screen 2 - Kitchen / Barista Display System (`pages/kds.html`)

**Files:**
- Create: `design-system/pos-cafe/pages/kds.html`
- Test: node verification script

**Interfaces:**
- Consumes: `window.POS_BUS` events (`NEW_ORDER`, `ORDER_CANCELLED`, `ORDER_REMAKE`).
- Produces: Order status updates (`QUEUED` -> `IN_PREPARATION` -> `READY` -> `FULFILLED`).

- [ ] **Step 1: Build KDS header & 3-column Kanban layout**
Columns: "Chờ pha (Queued)", "Đang pha (In Preparation)", "Đã xong (Ready)".
- [ ] **Step 2: Implement Ticket Card with live timer & modifiers**
Color escalation (< 3 min green, 3-5 min amber, > 5 min red pulsing), item checklist, custom notes tags.
- [ ] **Step 3: Implement Web Audio new ticket alert & status transitions**
Audio chime when order arrives via bus; 1-touch buttons to advance ticket status.
- [ ] **Step 4: Add station filtering**
Filter by `[Tất cả]`, `[Quầy Bar]`, `[Bếp Bánh]`.
- [ ] **Step 5: Verify layout and touch targets**
Run automated checks.
- [ ] **Step 6: Commit changes**
`git add design-system/pos-cafe/pages/kds.html`
`git commit -m "feat(ui): implement barista kitchen display system with live ticket kanban"`

---

### Task 4: Screen 3 - Sales Shift Management & Z-Report (`pages/shift.html`)

**Files:**
- Create: `design-system/pos-cafe/pages/shift.html`
- Test: node verification script

**Interfaces:**
- Consumes: `window.POS_BUS` orders and cash transactions.
- Produces: `localStorage.POS_ACTIVE_SHIFT` updates, opening float record, cash in/out logs, closing Z-report.

- [ ] **Step 1: Build Opening Float Denomination Calculator**
Numpad counter for currency notes (500k to 1k) calculating initial cash fund.
- [ ] **Step 2: Build In-Shift Cash Fund Dashboard**
Cards for Opening Float, Cash Sales, VietQR Sales, Current Expected Cash. Cash In / Cash Out drawer modals with reason inputs.
- [ ] **Step 3: Build Shift Closure & Reconciliation Dialog**
Counted cash inputs, expected cash comparison, discrepancy calculation (Thừa/Khớp/Thiếu).
- [ ] **Step 4: Build K80 Z-Report Preview & Print Modal**
Closing financial statement with dedicated `@media print` CSS.
- [ ] **Step 5: Verify calculations & touch targets**
Run automated checks.
- [ ] **Step 6: Commit changes**
`git add design-system/pos-cafe/pages/shift.html`
`git commit -m "feat(ui): implement sales shift management and z-report reconciliation"`

---

### Task 5: Screen 4 - Floor Plan & Table Operations (`pages/tables.html`)

**Files:**
- Create: `design-system/pos-cafe/pages/tables.html`
- Test: node verification script

**Interfaces:**
- Consumes: `window.POS_BUS` table sessions.
- Produces: Table transfer, merge, and split check events.

- [ ] **Step 1: Build Floor Plan Grid & Zone Filters**
Tabs: Tầng 1, Tầng 2, Sân vườn. Table cards showing status, party size, running total.
- [ ] **Step 2: Implement Table Transfer (Chuyển bàn)**
Select source table -> target empty table -> transfer all orders.
- [ ] **Step 3: Implement Table Merge (Gộp bàn)**
Select Table A and Table B -> merge into unified session.
- [ ] **Step 4: Implement Split Check (Tách bill)**
Item selection modal to split charges onto separate check.
- [ ] **Step 5: Verify touch targets and UI transitions**
Run automated checks.
- [ ] **Step 6: Commit changes**
`git add design-system/pos-cafe/pages/tables.html`
`git commit -m "feat(ui): implement floor plan and table operations in waiter mode"`

---

### Task 6: Screen 5 - Order History, Audit & Void/Refund (`pages/history.html`)

**Files:**
- Create: `design-system/pos-cafe/pages/history.html`
- Test: node verification script

**Interfaces:**
- Consumes: `localStorage.POS_ORDERS`.
- Produces: Void records, Refund transactions, K80 re-prints.

- [ ] **Step 1: Build Order History Data Table & Search Filters**
Filter by date, payment method (Tiền mặt / VietQR), order status.
- [ ] **Step 2: Build Order Details Drawer**
Itemized list, modifiers, payment details, cashier name, timestamps.
- [ ] **Step 3: Implement K80 Receipt Re-print**
Modal with printable thermal receipt layout.
- [ ] **Step 4: Implement Void / Refund with Reason and Manager Approval**
Reason dropdown, mandatory notes for "Khác", PIN verification check.
- [ ] **Step 5: Verify table responsiveness and touch targets**
Run automated checks.
- [ ] **Step 6: Commit changes**
`git add design-system/pos-cafe/pages/history.html`
`git commit -m "feat(ui): implement order history audit and void refund manager flow"`

---

### Task 7: Screen 6 - Catalog Stock Toggles & Store Setup (`pages/settings.html`)

**Files:**
- Create: `design-system/pos-cafe/pages/settings.html`
- Test: node verification script

**Interfaces:**
- Produces: `localStorage.POS_CATALOG_STATUS` and `localStorage.POS_STORE_INFO` broadcasting updates via `pos_cafe_bus`.

- [ ] **Step 1: Build Out-of-Stock Quick Toggles**
Searchable list of all menu items and toppings with 1-tap on/off switches.
- [ ] **Step 2: Build Store & VietQR Setup Form**
Store name, address, hotline, WiFi password, Bank name, STK, Account holder.
- [ ] **Step 3: Broadcast stock updates to index.html**
Disabled items gray out on Cashier screen in real time.
- [ ] **Step 4: Verify form persistence and touch targets**
Run automated checks.
- [ ] **Step 5: Commit changes**
`git add design-system/pos-cafe/pages/settings.html`
`git commit -m "feat(ui): implement catalog out-of-stock toggles and store settings"`

---

### Task 8: Comprehensive End-to-End Integration Verification

**Files:**
- Test: `tests/e2e-suite-test.js`

**Verification Criteria:**
- Cross-tab ordering: Placing order on `index.html` immediately renders ticket on `kds.html` and adds row in `history.html`.
- Out-of-stock: Toggling off item on `settings.html` disables card on `index.html`.
- Shift reconciliation: Cash orders on `index.html` update expected cash on `shift.html`.
- Touch targets >= 48px across all 7 screens.
- Zero em-dashes and zero AI-purple gradients across all files.

- [ ] **Step 1: Run comprehensive node verification across all pages**
- [ ] **Step 2: Verify touch targets, font stacks, and no em-dashes**
- [ ] **Step 3: Commit final suite polish**
`git add .`
`git commit -m "feat(ui): finalize pos cafe multi-screen suite with real-time sync"`
