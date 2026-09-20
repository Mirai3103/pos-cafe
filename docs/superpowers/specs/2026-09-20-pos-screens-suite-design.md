# Design Specification: POS Cafe Multi-Screen Suite

- **Author:** Antigravity & Team
- **Date:** 2026-09-20
- **Status:** Approved
- **Skill:** design-taste-frontend (Anti-Slop Modern Frontend)
- **Target Directory:** `design-system/pos-cafe/pages/`
- **Companion Screen:** `design-system/pos-cafe/index.html` (Cashier Screen)

---

## 1. Overview & Design Read

> **Design Read:** High-velocity POS Cafe Suite for Baristas, Waiters, and Managers, with a Crisp Light Nordic-Modern operational language, leaning toward Tailwind utilities, Outfit typography, JetBrains Mono numbers, and Emerald/Indigo/Amber accents with tactile ergonomics (>= 48px touch targets, zero em-dashes, no AI-purple gradients, and real-time cross-tab sync).

### Core Style Dials
- **`DESIGN_VARIANCE: 6`** - Structured split-view and responsive grids with functional asymmetry.
- **`MOTION_INTENSITY: 6`** - Tactile key depression (`translateY(1px)` / `scale(0.98)`), Web Audio API sound synthesis (tap chirps and cash register chimes), spring dialogs (150ms).
- **`VISUAL_DENSITY: 8`** - Cockpit-dense information layout optimized for high throughput during peak cafe morning rush.

---

## 2. Architecture & Real-Time Sync Bus

The system adopts a **Multi-page Modular** architecture:
- Each screen is a standalone HTML5 file that runs directly in any browser without build tooling.
- Shared CSS and design tokens derived from `design-system/pos-cafe/DESIGN.md`.
- **Cross-Tab Real-Time Sync:**
  - Storage: `localStorage` keys (`POS_ORDERS`, `POS_ACTIVE_SHIFT`, `POS_TABLES`, `POS_CATALOG_STATUS`, `POS_CURRENT_STAFF`).
  - Event Bus: `window.BroadcastChannel('pos_cafe_bus')`.
  - When an order is placed on `index.html`, a `NEW_ORDER` message is dispatched. The Barista KDS (`pages/kds.html`) immediately receives the event, plays a new-ticket chime, and appends the ticket into the "Chờ pha" column in `< 100ms`.

### Global Navigation Hub & Shortcuts
Every screen includes a persistent, collapsible top/bottom navigation hub allowing staff to jump instantly between stations:
- `[F1]` **Thu ngân (Cashier):** `../index.html`
- `[F3]` **Pha chế (Barista KDS):** `kds.html`
- `[F4]` **Quản lý Ca (Shift & Cash):** `shift.html`
- `[F5]` **Sơ đồ Bàn (Table Operations):** `tables.html`
- `[F6]` **Lịch sử đơn (History & Audit):** `history.html`
- `[F7]` **Cài đặt món (Settings & Stock):** `settings.html`
- `[Esc]` **Khóa màn hình (Lock / Auth):** `auth.html`

---

## 3. Detailed Screen Specifications

### Screen 1: `pages/auth.html` - Staff PIN Auth & Workspace Session Lock
1. **Purpose:** Provide secure, fast 4-digit PIN authentication for staff, workspace selection, and manager authorization modal.
2. **Layout & Components:**
   - Centered terminal card on Canvas Slate-50 background.
   - 4-digit PIN display: masked pill slots that illuminate on keypress.
   - Large tactile Numpad: 3x4 grid with keys (1-9, Clear, 0, Backspace), minimum `64px` tap target per key.
   - Staff Avatar & Role Selector: Quick-switch between sample staff profiles:
     - `1234`: Nguyễn Văn Lan (Thu ngân - Cashier)
     - `5678`: Trần Hoàng Nam (Barista quầy bar)
     - `8888`: Lê Minh Tuấn (Cửa hàng trưởng - Manager)
     - `2468`: Phạm Thu Hà (Nhân viên phục vụ - Waiter)
   - Workspace Destination: Quầy thu ngân, Quầy bar pha chế, Khu vực phục vụ bàn, hoặc Bảng quản lý.
   - Manager Approval Dialog Mode: Callable via URL param or parent postMessage (`?mode=manager_approval&action=void_order`) with immediate PIN challenge and return callback.

### Screen 2: `pages/kds.html` - Kitchen / Barista Display System (KDS)
1. **Purpose:** Visual FIFO order dispatch for baristas and kitchen staff, eliminating printed paper waste and accelerating preparation speed.
2. **Layout & Kanban Columns:**
   - Header Bar: Active station toggle (`[Tất cả]`, `[Quầy Pha chế - 8]`, `[Bếp Bánh - 3]`), active tickets count, average fulfillment timer (e.g. `2.8 phút/đơn`), audio alert toggle.
   - Column 1: **Chờ pha (Queued)**: New orders waiting to be started.
   - Column 2: **Đang pha (In Preparation)**: Tickets currently being brewed/prepared.
   - Column 3: **Đã xong chờ giao (Ready for Pickup)**: Drinks finished, waiting for runner or customer counter call.
3. **Ticket Card Anatomy:**
   - Ticket Header: Order/Service `#14` in large JetBrains Mono, Table `#` or `Mang đi`, item count.
   - Live Timer Badge:
     - Green: `< 3 phút` (On schedule)
     - Amber: `3 - 5 phút` (Approaching warning)
     - Red Pulsing: `> 5 phút` (Overdue rush priority)
   - Item Checklist: Product name, size, detailed modifiers (`100% đường, 100% đá`), preparation notes in high-contrast badge (`[Để đá riêng]`, `[Ít sữa]`).
   - Action Buttons (>= 48px height):
     - `[Bắt đầu làm]` -> Moves to "Đang pha".
     - `[Xong món]` -> Moves to "Đã xong", triggers notification.
     - `[Đã giao khách]` -> Completes ticket and archives to history.
   - Remake Priority Flag: Visual amber/red banner for items flagged as Remake (Pha lại).

### Screen 3: `pages/shift.html` - Sales Shift Management & Z-Report
1. **Purpose:** Full cash drawer accountability, opening float calculation, mid-shift cash in/out, and end-of-shift reconciliation with K80 Z-report printout.
2. **Key Workflows:**
   - **Mở ca (Open Shift):**
     - Prompt when no shift is active.
     - Denomination calculator: inputs for counting notes (500k, 200k, 100k, 50k, 20k, 10k, 5k, 2k, 1k).
     - Auto-calculates total Opening Float (e.g. `2.000.000 đ`).
     - Cashier confirmation button.
   - **Trong ca (Active Shift Dashboard):**
     - Metric cards: Tiền đầu ca, Doanh thu tiền mặt, Doanh thu VietQR, Tổng quỹ tiền mặt hiện tại.
     - Action `[+ Thu quỹ (Cash In)]`: nộp thêm tiền lẻ đổi từ két an toàn.
     - Action `[- Chi quỹ (Cash Out)]`: chi tiền mua đá, sữa, vật dụng kèm lý do và người nhận.
     - Audit log of all cash drawer movements.
   - **Đóng ca & Đối soát (Shift Closure & Reconciliation):**
     - Counted Cash input table (đếm tiền thực tế trong két).
     - Expected Cash calculation comparison.
     - Discrepancy indicator (Thừa tiền / Khớp tiền / Thiếu tiền) in colored badge.
     - Confirmation with Manager Approval PIN.
     - Generates Z-Report and opens K80 print modal with closing summary.

### Screen 4: `pages/tables.html` - Floor Plan & Table Operations (Waiter Mode)
1. **Purpose:** Dedicated tablet interface for service staff on the floor.
2. **Layout & Capabilities:**
   - Zone Filter Tabs: `Tất cả (19)`, `Tầng 1 (8 bàn)`, `Tầng 2 (6 bàn)`, `Sân vườn (5 bàn)`.
   - Grid of interactive table cards showing occupancy, active elapsed time, party size, running bill total.
   - Table Action Sheet:
     - **Chuyển bàn (Transfer Table):** Move all items from Table A to Table B with confirmation.
     - **Gộp bàn (Merge Tables):** Combine Table A and Table B for large parties.
     - **Tách bill (Split Check):** Select specific items to split onto a new bill or divide equally.
     - **Gọi thêm món (Add Round):** Seamless shortcut to catalog or inline quick add.
     - **Yêu cầu thanh toán (Request Bill):** Sets table status to `Chờ thanh toán`, alerting the cashier.

### Screen 5: `pages/history.html` - Order History, Audit & Void/Refund
1. **Purpose:** Full transaction log of completed and active checks, receipts reprint, and auditable cancellations.
2. **Components:**
   - Filter bar: Date selector (Hôm nay, Hôm qua, Tuần này), payment method filter (Tất cả, Tiền mặt, VietQR), order status filter (Đã thanh toán, Đã hủy, Hoàn tiền).
   - Order Table: Service #, Table/Takeaway, Time, Cashier, Item count, Total VND, Status, Actions.
   - Order Details Drawer: Itemized breakdown, payment timestamps, staff actor.
   - Actions:
     - `[In lại hóa đơn (Ctrl+P)]`: Triggers K80 receipt preview modal.
     - `[Hủy hóa đơn (Void)]`: Requires selecting a Reason Category (`Hết nguyên liệu`, `Khách đổi ý`, `Lỗi nhân viên`, `Khác`) and fresh Manager PIN.
     - `[Hoàn tiền (Refund)]`: Records refund transaction against original payment method.

### Screen 6: `pages/settings.html` - Catalog Stock Toggles & Store Setup
1. **Purpose:** Daily menu availability management and store metadata configuration.
2. **Components:**
   - **Quản lý Hết món (Out of Stock Toggles):**
     - Fast search filter for menu items and toppings.
     - Single-tap switch to toggle item status between `Còn hàng` and `Tạm hết hàng`.
     - When toggled off, `pos_cafe_bus` immediately broadcasts the change to `index.html` (item becomes disabled/grayed out).
   - **Cấu hình Thông tin Quán:**
     - Tên quán (e.g. `The Coffee Workshop`).
     - Địa chỉ, số hotline, mật khẩu WiFi.
     - VietQR banking info: Tên ngân hàng (MB Bank), STK, Tên chủ tài khoản.
     - Save button with toast confirmation.

---

## 4. Design Standards & Token Integrity

All screens must strictly adhere to the established design rules:
1. **Theme:** Crisp Light Mode (Canvas `#f8fafc`, Surfaces `#ffffff`, Borders `#e2e8f0`).
2. **Typography:**
   - Primary UI: `Outfit`, sans-serif.
   - Financial & Metrics: `JetBrains Mono`, monospace.
   - Whole VND currency format: `35.000 đ`, `105.000 đ` (no decimals).
3. **Touch Targets:** All interactive elements (buttons, chips, table cards, steppers) maintain `>= 48px` minimum touch target.
4. **Banned Patterns:**
   - No emojis anywhere (use Lucide SVG icons).
   - No em-dashes (use hyphens, colons, or parentheses).
   - No AI-purple gradients or neon glows.
   - No pure black `#000000` (use Slate-900 `#0f172a`).
5. **Zero-Dependency Architecture:** Each page is self-contained with Tailwind CSS CDN, Google Fonts, and Lucide Icons.
