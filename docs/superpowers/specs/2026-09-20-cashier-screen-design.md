# Design Specification: Cashier POS Screen Prototype (`design-system/pos-cafe`, Phase 11 UI Exploration)

- **Author:** Antigravity & Team
- **Date:** 2026-09-20
- **Status:** In-Review
- **Target:** `design-system/pos-cafe/index.html`
- **Domain Authority:** [`CONTEXT.md`](../../CONTEXT.md), [`ROADMAP.md`](../../ROADMAP.md), [`spec/decisions.md`](../../spec/decisions.md)
- **Design Skill:** `design-taste-frontend` (Light Mode, Anti-AI Slop, High Ergonomics)

---

## 1. Purpose & Overview

Build a standalone, high-throughput Cashier POS touchscreen interface for cafe staff in `design-system/pos-cafe/index.html`.
The screen reflects the actual domain rules of the Go backend (`internal/sales`, `internal/shift`, `internal/catalog`, `internal/tables`, `internal/preparation`) while radically reducing cashier cognitive load, taps, and checkout duration during peak morning and lunch rushes.

### Goals
1. **Explore & Embody Domain Logic:** Accurately represent Service Sessions, Takeaway Service Numbers (`#01`, `#02`), Size rules, Modifier Groups with VND surcharges, Preparation Notes, Open Sales Shifts, Cash & Manual VietQR payments, and Dine-in Table Rounds.
2. **Minimal Keystrokes / Clicks:**
   - Single-tap add for uncustomized items.
   - Smart preselected defaults for customized items (Size M, 100% sugar, 100% ice) allowing 1-click confirmation via `Space` or `Enter`.
   - Dynamic Quick-Cash denomination buttons (Exact amount, round-up buttons e.g., 50k, 100k, 200k, 500k) with instant Change Due calculation.
   - Unified 1-tap "Thu tiền & Gửi pha chế" action that bridges the commercial Commit, Payment, and Preparation Submission boundaries.
3. **Dual Operational Modes:** Instant zero-friction switching between **Quick Takeaway (Mang đi)** and **Dine-in Floor Layout (Tại bàn)**.
4. **Light Mode Visual Excellence:** Clean, high-contrast crisp Slate & White base with a prominent Emerald Green financial accent, avoiding generic AI-purple glow or low-contrast slop.
5. **Standalone & Zero-Dependency Execution:** Self-contained HTML5 file with Tailwind CSS and Lucide icons that can be opened directly in any browser without npm build steps.

### Non-Goals
- Full production client generation or Go binary embedding (reserved for Phase 11 implementation plan).
- Offline persistent SQLite/IndexedDB sync queue (`CONTEXT.md` explicitly forbids client-side offline transaction queues).
- Integrated hardware cash-drawer triggers or thermal ESC/POS driver code in this UI prototype.

---

## 2. Design Read & Configuration (`design-taste-frontend`)

- **Design Read:** High-volume Cashier POS touchscreen interface for cafe baristas & cashiers, with an ultra-tactile, ergonomic operational language, leaning toward high visual density, instant feedback, and minimum keystrokes/taps.
- **Dials:**
  - `DESIGN_VARIANCE: 3` (Rigid, highly predictable 3-zone layout: Header, Catalog, Order & Checkout).
  - `MOTION_INTENSITY: 3` (Snappy 100-150ms tactile micro-interactions, no blocking transitions).
  - `VISUAL_DENSITY: 8` (Cockpit layout: high density of items, large touch targets, monospace VND figures).
- **Theme Choice:** **Crisp Light Mode**
  - Page Background: `#f8fafc` (`bg-slate-50`).
  - Card & Surface: `#ffffff` (`bg-white`) with fine borders (`border-slate-200`).
  - Primary Typography: `#0f172a` (`text-slate-900`) for headers, `#475569` (`text-slate-600`) for secondary text.
  - Accent Color: Emerald Green (`#059669` / `#10b981`) for total amounts, successful payments, and primary CTA.
  - State Indicators: Amber (`#f59e0b`) for billing/warning, Indigo (`#4f46e5`) for occupied tables, Rose (`#e11d48`) for item removal.

---

## 3. Screen Layout & Architecture

The layout occupies `100dvh` in a fixed, non-scrolling master container optimized for 16:9 / 16:10 POS touchscreens.

```
+---------------------------------------------------------------------------------------------+
| TOP BAR (Height: 56px)                                                                      |
| [Brand POS Cafe]  |  [Tab: Mang đi (F1)] [Tab: Tại bàn (F2)]  |  [Search /]  | Ca #04 | 10:50 |
+-------------------------------------------------------------+-------------------------------+
| MENU CATALOG & FLOOR AREA (62% width)                       | ORDER BILL & CHECKOUT (38%)   |
| Category Pills: [Tất cả] [Cà phê] [Trà] [Đá xay] [Bánh ngọt]| Order Title: Đơn mang đi #14  |
|                                                             | Items List (Scrollable):      |
| Product Grid (Touch targets >= 80px):                       | 1. Cà phê sữa đá (M)   35.000 |
| +-----------------+ +-----------------+ +-----------------+ |    - 100% ngọt, 100% đá       |
| | Cà phê sữa đá   | | Bạc xỉu         | | Trà đào cam sả  | |    [-] [ 1 ] [+]      [x]     |
| | 35.000 đ        | | 39.000 đ        | | 45.000 đ        | | 2. Croissant bơ tỏi   32.000 |
| +-----------------+ +-----------------+ +-----------------+ | ----------------------------- |
| +-----------------+ +-----------------+ +-----------------+ | Tạm tính:              67.000 |
| | Cold Brew Cam Sả| | Matcha Latte    | | Tiramisu        | | TỔNG CỘNG:          67.000 đ |
| | 45.000 đ        | | 48.000 đ        | | 40.000 đ        | | ----------------------------- |
| +-----------------+ +-----------------+ +-----------------+ | Phương thức: [Tiền mặt] [QR]  |
|                                                             | Gợi ý: [Đúng tiền] [70k] [100k|
| (Khi bật Tại bàn: Chuyển thành Sơ đồ bàn Tầng 1/2/Sân vườn) | Tiền thừa: 33.000 đ           |
|                                                             | [ >>> THU TIỀN & GỬI PHA CHẾ ]|
+-------------------------------------------------------------+-------------------------------+
```

---

## 4. Detailed Component Specifications

### 4.1 Top Bar (Header)
- **Brand & Shift Info:** Displays active Sales Shift badge (`Ca #04 - Nguyễn Thu Ngân`), real-time clock (`Asia/Ho_Chi_Minh`), and cash drawer fund indication.
- **Mode Switcher:**
  - `[Mang đi (F1)]`: Generates sequential Service Number (`#01`, `#02`...).
  - `[Tại bàn (F2)]`: Displays Floor Plan and links cart to selected Table ID.
- **Quick Search Input:** `/` or `Ctrl+K` to focus; instantaneous filter by dish name or shortcut acronym (e.g. `cfsd` -> Cà phê sữa đá).

### 4.2 Menu Catalog Grid
- **Category Tabs:** Pill buttons for fast switching (`Tất cả`, `Cà phê Việt`, `Cà phê pha máy`, `Trà & Macchiato`, `Đá xay`, `Bánh & Điểm tâm`).
- **Product Cards:**
  - White card with subtle border (`border-slate-200`), hover transition, active press feedback.
  - Displays: Item Name, Base Price in whole VND, Size tag if multi-size.
  - Click behavior:
    - Items without modifiers (e.g., Croissant, bottled drinks): Appends directly to cart with quantity +1.
    - Items with options: Opens the Modifier Popover.

### 4.3 Modifier Popover (Ultra-Fast Customizer)
- Centered modal dialog with backdrop scrim.
- **Preselected Defaults:** Size M, 100% Ngọt, 100% Đá.
- **Option Groups:**
  - Size: Radio chips (`S: 29k`, `M: 35k`, `L: 42k`).
  - Đường: Chips (`0%`, `30%`, `50%`, `70%`, `100%`).
  - Đá: Chips (`Nóng`, `Không đá`, `50% đá`, `100% đá`).
  - Topping: Checkbox chips with surcharges (`+ Thạch cafe 10k`, `+ Kem Cheese 12k`, `+ Trân châu trắng 10k`).
  - Preparation Note: Quick-tag buttons (`Ít sữa`, `Đậm cà phê`, `Để đá riêng`) and text input.
- **Primary Action:** Large button `Thêm vào đơn [Enter / Space]` showing dynamic calculated item total.

### 4.4 Order Bill & Financial Settlement
- **Cart Item Rows:**
  - Compact, high readability.
  - Item name, selected size, modifier summary.
  - Stepper `[-] [Qty] [+]` and remove button `[Trash]`.
  - Line total in bold monospace VND.
- **Financial Equation:**
  - Tạm tính (Subtotal).
  - Khuyến mãi / Giảm giá (nếu có).
  - **TỔNG CỘNG:** Highlighted in emerald green with large `font-mono` text.
- **Quick Cash & VietQR:**
  - Tab 1: **Tiền mặt (Cash):**
    - Dynamic calculation of denomination buttons based on total:
      * Exact (`Đúng tiền`)
      * Nearest rounded 10k/50k
      * 100k, 200k, 500k
    - Displays `Tiền khách đưa` and `Tiền thừa trả khách` in high-contrast emerald box.
  - Tab 2: **VietQR:**
    - Generates dynamic VietQR image with exact bill amount, cafe bank account info, and transfer content `POS <ServiceNumber>`.
    - Confirmation button `[Xác nhận đã nhận & Gửi pha chế]`.
- **Checkout Action Button:**
  - Full-width button: `[THU TIỀN & GỬI PHA CHẾ (Enter)]`.
  - On click: Triggers audio chirp / visual success toast, increments Service Number, prints order receipt preview, and clears the cart for the next customer in < 200ms.

### 4.5 Floor Layout & Dine-in Mode
- Activated via `F2` or Top Bar.
- **Zones:** `Tầng 1 (8 bàn)`, `Tầng 2 (6 bàn)`, `Sân vườn (5 bàn)`.
- **Table Cards:**
  - Table number & seating capacity (`Bàn 01 - 4 chỗ`).
  - Status:
    * `Trống (Available)`: Crisp light border, 1-click opens new ordering session.
    * `Đang phục vụ (Occupied)`: Indigo accent, showing active time, item count, and running total.
    * `Chờ thanh toán (Billing)`: Amber accent, high priority for cashier.
- **Table Operations:**
  - `Gửi bếp thêm món (Submit Round)`: Sends newly added items to preparation without settling the bill.
  - `In tạm tính (Print Pre-bill)`: Prints draft bill for customer review.
  - `Thanh toán bàn`: Opens the checkout drawer to collect payment and frees the table to `Trống`.

---

## 5. Keyboard Shortcuts Map

| Shortcut | Action |
| :--- | :--- |
| `F1` | Chuyển sang đơn Mang đi (Takeaway) |
| `F2` | Chuyển sang Sơ đồ bàn (Dine-in) |
| `/` hoặc `Ctrl+K` | Tìm kiếm món nhanh |
| `Space` | Xác nhận chọn món trong Popover |
| `Enter` | Hoàn tất thanh toán & gửi đơn |
| `Esc` | Đóng popover / Thoát modal |
| `Delete` / `Backspace` | Xóa món đang chọn khỏi giỏ |

---

## 6. Implementation File

- **File Path:** `design-system/pos-cafe/index.html`
- **Stack:** HTML5, Tailwind CSS (via CDN with custom config), Lucide Icons, Vanilla JavaScript reactive state engine.
