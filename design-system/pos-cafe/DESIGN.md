---
name: Crisp Emerald POS
colors:
  surface: '#f8fafc'
  surface-dim: '#f1f5f9'
  surface-bright: '#ffffff'
  surface-container-lowest: '#ffffff'
  surface-container-low: '#f8fafc'
  surface-container: '#f1f5f9'
  surface-container-high: '#e2e8f0'
  surface-container-highest: '#cbd5e1'
  on-surface: '#0f172a'
  on-surface-variant: '#475569'
  inverse-surface: '#1e293b'
  inverse-on-surface: '#f8fafc'
  outline: '#cbd5e1'
  outline-variant: '#e2e8f0'
  surface-tint: '#059669'
  primary: '#059669'
  on-primary: '#ffffff'
  primary-container: '#ecfdf5'
  on-primary-container: '#065f46'
  inverse-primary: '#6ee7b7'
  secondary: '#4f46e5'
  on-secondary: '#ffffff'
  secondary-container: '#eef2ff'
  on-secondary-container: '#3730a3'
  tertiary: '#d97706'
  on-tertiary: '#ffffff'
  tertiary-container: '#fffbeb'
  on-tertiary-container: '#92400e'
  error: '#dc2626'
  on-error: '#ffffff'
  error-container: '#fef2f2'
  on-error-container: '#991b1b'
  background: '#f8fafc'
  on-background: '#0f172a'
  surface-variant: '#e2e8f0'
typography:
  headline-xl:
    fontFamily: Outfit
    fontSize: 32px
    fontWeight: '700'
    lineHeight: 40px
    letterSpacing: -0.02em
  headline-lg:
    fontFamily: Outfit
    fontSize: 24px
    fontWeight: '700'
    lineHeight: 32px
    letterSpacing: -0.015em
  headline-sm:
    fontFamily: Outfit
    fontSize: 18px
    fontWeight: '600'
    lineHeight: 26px
    letterSpacing: -0.01em
  body-lg:
    fontFamily: Outfit
    fontSize: 16px
    fontWeight: '500'
    lineHeight: 24px
  body-md:
    fontFamily: Outfit
    fontSize: 14px
    fontWeight: '400'
    lineHeight: 22px
  body-sm:
    fontFamily: Outfit
    fontSize: 12px
    fontWeight: '400'
    lineHeight: 18px
  metric-display:
    fontFamily: JetBrains Mono
    fontSize: 28px
    fontWeight: '700'
    lineHeight: 32px
    letterSpacing: -0.03em
  metric-large:
    fontFamily: JetBrains Mono
    fontSize: 20px
    fontWeight: '600'
    lineHeight: 24px
    letterSpacing: -0.02em
  metric-base:
    fontFamily: JetBrains Mono
    fontSize: 14px
    fontWeight: '500'
    lineHeight: 20px
    letterSpacing: 0em
  label-code:
    fontFamily: JetBrains Mono
    fontSize: 11px
    fontWeight: '600'
    lineHeight: 14px
    letterSpacing: 0.06em
spacing:
  touch-target: 48px
  gutter: 0.75rem
  margin: 1rem
  space-xs: 0.25rem
  space-sm: 0.5rem
  space-md: 0.75rem
  space-lg: 1rem
  space-xl: 1.5rem
---

# Design System: Cafe POS Cashier Terminal (Crisp Emerald Edition)
**Skill:** stitch-design-taste  
**Target File:** `design-system/pos-cafe/index.html`  
**Reference Architecture:** Clean Light Mode POS Touchscreen Terminal  

---

## Configuration: Style Dials

| Dial | Level | Description |
|---|---|---|
| **Creativity** | `7` | High-agency tactile UI, smart defaults, Vietnamese acronym search, Placewaifu media integration, thermal receipt simulator. |
| **Density** | `8` | Cockpit-dense layout optimized for rapid cashier throughput during peak cafe morning rush. |
| **Variance** | `6` | Asymmetric 62/38 split screen with dynamic catalog vs. dine-in floor layout switching. |
| **Motion Intent** | `6` | Tactile key depression, Web Audio API chirps, 150ms spring dialogs, hardware-accelerated transforms. |

---

## 1. Visual Theme & Atmosphere

The interface embodies a **Crisp Light Modern Cafe POS** system tailored specifically for high-velocity coffee shop operations in Vietnam. It balances ergonomic operational utility with modern Nordic warmth:
- **Clarity Under Glare:** Sunlight-readable bright palette with high-contrast Slate-900 typography on crisp white and slate-50 backgrounds, eliminating screen fatigue during 8-hour cashier shifts.
- **Physical Tactile Feedback:** Every button and card depresses slightly on tap (`translateY(1px)` or `active:scale-[0.98]`), backed by real-time Web Audio API sound synthesis (gentle click chirps and cash register chimes).
- **Extreme Speed & Low Friction:** Pre-selected smart defaults (Size M, 100% sugar, 100% ice) allow one-touch order submission. Vietnamese acronym search (`cfsd` for *Cà phê sữa đá*, `bx` for *Bạc xỉu*, `tdcs` for *Trà đào cam sả*) enables finding items in under 2 seconds.

---

## 2. Color Palette & Roles

### Base & Surfaces
- **Canvas Base (`#f8fafc` - Slate-50):** Background surface for the entire terminal shell.
- **Pure Surface (`#ffffff`):** Card surfaces, order bill paper container, modal dialog background.
- **Surface Accent / Substrate (`#f1f5f9` - Slate-100):** Inactive category pills, image placeholder wells, table card headers.
- **Structural Lines (`#e2e8f0` - Slate-200):** Crisp 1px structural dividing lines across panels and tables.

### Typography & Depth
- **Charcoal Ink (`#0f172a` - Slate-900):** Primary headings, product names, monetary totals. Never pure black.
- **Muted Steel (`#64748b` - Slate-500):** Category names, modifier tags, subtitle indicators.
- **Whisper Slate (`#94a3b8` - Slate-400):** Placeholder text, shortcut badges, inactive icons.

### Accents & Operational Status
- **Emerald Signal (`#059669` / `#10b981`):** Primary operational accent for monetary grand totals, quick cash tender buttons, checkout commitment, and successful order toasts.
- **Indigo Dine-In (`#4f46e5` / `#6366f1`):** Dine-in mode, active table indicator, occupied table cards, round counter tags.
- **Amber Warning (`#d97706` / `#f59e0b`):** Billing hold status, pending settlement alerts, table payment wait indicators.
- **Rose Destructive (`#dc2626` / `#ef4444`):** Cart item removal, order cancellation, void confirmation.

### Banned Colors
- AI purple/violet neon gradients are strictly forbidden.
- Pure black (`#000000`) is strictly forbidden (always use Slate-900 `#0f172a`).
- Oversaturated accents (> 80% saturation) that induce eye fatigue.

---

## 3. Typography Architecture

The typographic hierarchy strictly segregates operational text from financial/numerical metrics:

- **Primary UI & Headings:** `Outfit`, sans-serif (Weights: 500, 600, 700).
  - Clean geometry, open counters, tall x-height ensuring crisp readability for Vietnamese diacritics (`ẩ`, `ễ`, `ớ`, `ự`, `đ`).
  - Leading: tight on headlines (`1.15`), comfortable on descriptions (`1.4`).
- **Financial, Numerical & Metric Readouts:** `JetBrains Mono`, monospace (Weights: 500, 600, 700).
  - All currency values formatted in whole Vietnamese Dong with dot thousand-separators (e.g., `35.000 đ`, `105.000 đ`).
  - Real-time digital clock (`HH:mm:ss`), table session timers (`01:24:10`), keyboard shortcuts (`[F1]`, `[Enter]`).
  - Strict tabular figures prevent visual jitter when totals update during multi-item ordering.
- **Banned Fonts:** `Inter` is banned for creative character; generic system serifs (`Times New Roman`, `Georgia`) are banned.

---

## 4. Screen Layout & Zoning (1024px+ POS Landscape Standard)

The viewport is locked to a 100dvh desktop/tablet workspace divided into three functional zones:

```
+-------------------------------------------------------------------------------+
| Top Bar (h-16): Brand | Shift Badge | Realtime Clock | Mode Switcher | Search |
+---------------------------------------------+---------------------------------+
| Zone 1: Catalog & Floor Matrix (62% Width)  | Zone 2: Order Bill (38% Width)  |
|                                             |                                 |
| Mode A: Catalog Matrix                      | - Bill Header & Order #         |
| - Horizontal Category Filter Pills (>=48px) | - Scrollable Itemized Cart      |
| - Product Card Grid (2-4 columns)           |   - Name, Size, Modifiers       |
|   - Image Header (Placewaifu + Fallback)    |   - Touch Stepper [-] [Qty] [+] |
|   - Item Name, Category, Price in VND       | - Financial Summary (Grand Total|
|                                             | - Quick Cash Smart Denominations|
| Mode B: Dine-In Floor Plan                  |   or Dynamic VietQR Generator   |
| - Zone Pills: Tầng 1, Tầng 2, Sân vườn      | - 1-Tap Checkout CTA (>=56px)   |
| - Interactive Table Cards with Round Status |                                 |
+---------------------------------------------+---------------------------------+
| Bottom Status Bar: Version | Active Cashier | Item Counter | Sound Toggle     |
+-------------------------------------------------------------------------------+
```

---

## 5. Component Behaviors & Specifications

### 5.1 Product Cards
- **Geometry:** `rounded-2xl`, border `1px solid #e2e8f0`, background `#ffffff`, `min-h-[210px]`.
- **Top Media Container (`h-28 sm:h-32`):**
  - Integrated with Placewaifu mock images (`https://placewaifu.com/image/300/200?id=X`).
  - Hover zoom effect (`group-hover:scale-105` in 300ms).
  - Offline fallback: smooth auto-hide of broken `img` and display of category Lucide icon on slate-100 bed.
  - Floating badge (e.g., "Bán chạy", "Món hot") anchored with glassmorphism `backdrop-blur-xs`.
  - Size indicator badge (`3 cỡ`) anchored bottom-right on media.
- **Card Body:** Product name (line-clamp-1), category subtitle, large VND price in JetBrains Mono, emerald quick-add button.
- **Touch Ergonomics:** Entire card surface is clickable with immediate tactile depression (`active:scale-[0.98]`).

### 5.2 Category Filter Pills
- **Geometry:** Horizontal scrolling flex rail, pill height minimum `48px`, padding `px-4`.
- **States:**
  - Inactive: `#ffffff` background, `border border-slate-200`, text `slate-700`.
  - Active: Emerald gradient fill `#059669`, text `#ffffff`, shadow-xs.
  - Count badge indicates total items per category.

### 5.3 Modifier Popover Modal
- **Smart Defaults:** Pre-selects Size M, 100% Sugar, 100% Ice. Cashier can immediately press `Enter` or `Space` to add uncustomized drinks in under 1 second.
- **Touch Chips:** All radio chips (Size, Sugar, Ice, Toppings) have `>= 48px` touch target height.
- **Quick Preparation Tags:** Chips for common bar requests (`[Ít sữa]`, `[Pha đậm]`, `[Để đá riêng]`) + freeform notes.
- **Live Footer:** Displays real-time recalculated price with large commit button.

### 5.4 Order Bill & Cart Management
- **Thermal Manifest Aesthetics:** Clean white column structured like an authentic thermal receipt roll.
- **Quantity Steppers:** Circular `[-]` and `[+]` buttons with `min-w-[48px] min-h-[48px]` touch targets.
- **Item Summary:** Lists size, custom sugar/ice percentages, toppings surcharge, and round tags for dine-in.

### 5.5 Quick Cash & VietQR Settlement Flow
- **Dynamic Cash Denominations:** Automatically calculated based on grand total:
  - Exact total button: `[Đúng tiền: XX.000 đ]`
  - Rounded up button: nearest 10.000 or 50.000 note
  - Higher standard notes: 100k, 200k, 500k
- **Live Change Due Container:** Prominent emerald readout container displaying change owed in extra-bold monospace.
- **VietQR Transfer Generator:** Dynamic MB Bank QR code with embedded bill amount and syntax `POS<number>`, fallback SVG pattern.
- **1-Tap Commit CTA:** Minimum `56px` height, full width, emerald background, triggers audio cash chime and auto-increments order sequence.

### 5.6 Dine-In Floor Plan & Table Management
- **Table Card Visual States:**
  - Available (*Trống*): White card, dashed border, "+ Chọn bàn" prompt.
  - Occupied (*Đang phục vụ*): Indigo header, live dining duration timer, item count, running bill total.
  - Billing (*Chờ thanh toán*): Amber border, pulsing status badge, direct "Thanh toán" shortcut.
- **Multi-Round Ordering:** Adding drinks to an occupied table marks them as "Lượt 2" (Round 2) with "Gửi bếp thêm món" action.

### 5.7 K80 Thermal Receipt Modal
- **Format:** 80mm continuous thermal paper layout with dashed cut lines, store header, order metadata, itemized grid, financial breakdown, QR code verification.
- **Print Optimization:** Dedicated `@media print` stylesheet strips the entire web application UI, printing strictly the receipt container.

---

## 6. Interaction & Motion Rules

- **Spring-like Dialog Transitions:** Modals enter with `fade-in zoom-in-95 duration-150` for instant responsiveness without sluggishness.
- **Hardware Acceleration:** Animations strictly utilize `transform` and `opacity`. Never animate `top`, `left`, `width`, or `height`.
- **Tactile Audio Engine:**
  - Tap click: 800Hz sine chirp for 30ms.
  - Checkout complete: Two-tone arpeggio (523.25Hz -> 1046.5Hz) simulating a mechanical cash drawer chime.
  - User can toggle audio mute on/off via the top bar sound button.
- **Keyboard Navigation Shortcuts:**
  - `F1`: Switch to Takeaway mode.
  - `F2`: Switch to Dine-In floor plan.
  - `/` or `Ctrl+K`: Jump directly to search bar.
  - `Space` / `Enter`: Quick add in modifier popover, confirm payment.
  - `Esc`: Dismiss modals and search focus.
  - `Delete` / `Backspace`: Remove selected cart item.

---

## 7. Anti-Patterns & Strict Exclusions

- **NO Emojis:** Prohibited across all UI text, labels, badges, and alerts. Use SVG icons (Lucide) exclusively.
- **NO Em-Dashes:** Banned across the entire codebase and documentation. Use hyphens (-), colons, or parentheses.
- **NO AI-Purple/Neon Aesthetics:** No purple button glows, no dark-purple gradients, no cyber neon highlights.
- **NO Sub-48px Touch Targets:** All touchable elements (buttons, chips, steppers, inputs) must maintain `>= 48px` minimum dimensions.
- **NO Fractional VND Formatting:** Vietnamese currency has no decimal points. Never format as `35.00 đ`; always format as whole integer `35.000 đ`.
- **NO Broken Image Placeholders:** When an image fails or is unavailable, immediately hide the image tag and reveal the clean category SVG fallback.
- **NO Full-Screen Page Reloads:** Single-page reactive state transitions in `< 100ms`.
