# Design Spec: Admin Menu, Category, Topping Management & Batch Linker Matrix

**Date**: 2026-09-20  
**Target Files**:
- `design-system/pos-cafe/pages/settings.html`
- `design-system/pos-cafe/shared/pos-bus.js`
- `design-system/pos-cafe/index.html`
**Design Tokens**: Crisp Emerald Edition (`DESIGN_VARIANCE: 5`, `MOTION_INTENSITY: 4`, `VISUAL_DENSITY: 7`, $\ge 48\text{px}$ touch targets, Outfit + JetBrains Mono)

---

## 1. Problem Statement & Objectives

### 1.1 The Challenge
In cafe POS systems, managing product catalogs and modifiers is often a major pain point for managers:
- Configuring multi-size pricing (Size S, M, L) requires tedious, repetitive form inputs.
- Linking toppings/modifiers to drinks is clumsy: assigning modifier groups one by one to each beverage wastes enormous time during menu updates.
- In naive UIs, managers cannot visualize what the cashier will see during an actual rush shift.

### 1.2 The Solution
Deliver a streamlined, ergonomic administrative interface directly within [`settings.html`](file:///E:/Code/pos-cafe/design-system/pos-cafe/pages/settings.html) featuring:
1. **Menu Item Studio**: 2-column modal for item metadata, dynamic multi-size pricing matrix, and in-place modifier group tags.
2. **Category Manager**: Visual card management with Lucide icon selector and default modifier group inheritance.
3. **Modifier Group & Option Studio**: Custom single/multi selection rules (`min`/`max`), and dynamic option rows with quick keyboard entry (`Enter` to add next row).
4. **Interactive Batch Linker Matrix**: A 3-column split view (Modifier Groups $\rightarrow$ Category & Item Selection $\rightarrow$ Live Cashier POS Simulator) enabling 1-click batch assignment and zero-friction preview.
5. **Cross-Tab Synchronization**: Durable persistence in `localStorage` via [`pos-bus.js`](file:///E:/Code/pos-cafe/design-system/pos-cafe/shared/pos-bus.js), broadcasting `CATALOG_ITEMS_UPDATED` so the Cashier Terminal ([`index.html`](file:///E:/Code/pos-cafe/design-system/pos-cafe/index.html)) dynamically renders new drinks, prices, and toppings without page reloads.

---

## 2. Data Architecture & Storage Schema

Data is stored in `localStorage` under key `POS_CATALOG_DATA` and managed by `window.POS_BUS`.

```typescript
interface CatalogCategory {
  id: string;              // e.g. "coffee_vn", "tea"
  name: string;            // e.g. "Trà & Macchiato"
  icon: string;            // Lucide icon name, e.g. "leaf"
  order: number;
  defaultGroupIds: string[]; // Modifier groups inherited by all items in this category
}

interface MenuItemSize {
  id: string;              // "S", "M", "L", or custom
  name: string;            // "Size S", "Size M", "Size L"
  price: number;           // Whole VND, e.g. 45000
  isDefault?: boolean;
}

interface CatalogMenuItem {
  id: string;              // e.g. "tdcs"
  name: string;            // e.g. "Trà đào cam sả"
  categoryId: string;      // "tea"
  categoryName: string;    // "Trà & Macchiato"
  acronym: string;         // "tdcs" for fast Vietnamese search
  badge?: string;          // "Bán chạy", "Món hot", "Mới"
  description: string;
  imageUrl: string;
  hasModifiers: boolean;
  sizes: MenuItemSize[];
  defaultSize: string;
  basePrice: number;       // Base price of default size
  assignedModifierGroupIds: string[]; // Explicitly assigned groups
  excludedModifierGroupIds: string[]; // Groups excluded from category inheritance
  isAvailable: boolean;
}

interface ModifierOption {
  id: string;              // e.g. "tran_chau_trang"
  name: string;            // "Trân châu trắng"
  price: number;           // Surcharge in VND, e.g. 10000
  isAvailable: boolean;
}

interface ModifierGroup {
  id: string;              // e.g. "top_tea"
  name: string;            // "Topping Trà Trái Cây"
  selectionType: 'single' | 'multiple'; // Radio vs Checkbox
  minSelect: number;
  maxSelect: number;
  options: ModifierOption[];
}
```

### 2.1 Default Seed Data
Seed data integrates seamlessly with existing `INITIAL_CATEGORIES`, `INITIAL_MENU_ITEMS`, and `MODIFIER_TOPPINGS`:
- Categories: Cà phê Việt (`coffee_vn`), Cà phê máy (`coffee_machine`), Trà & Macchiato (`tea`), Đá xay & Sinh tố (`freeze`), Bánh & Điểm tâm (`bakery`).
- Modifier Groups:
  - `sugar`: Mức đường (0%, 30%, 50%, 70%, 100% Mặc định, single select).
  - `ice`: Mức đá (Nóng, Không đá, Ít đá, 100% đá Mặc định, Đá riêng, single select).
  - `top_tea`: Topping Trà (Trân châu trắng +10k, Thạch đào +10k, Thạch cà phê +10k, Kem cheese +12k, multi select 0-5).
  - `top_cheese`: Lớp Váng Sữa & Kem Cheese (Kem Phô Mai Macchiato +12k, Kem Muối +12k, single select 0-1).

---

## 3. UI/UX Component Specifications

### 3.1 Sub-Nav Bar Enhancement in `settings.html`
Add a dedicated tab in the header sub-nav:
```html
<!-- Tab: Quản lý Thực đơn & Topping -->
<button id="tab-btn-catalog" type="button" onclick="switchSettingsTab('catalog')" role="tab"
  class="min-h-[48px] h-12 px-4 py-2 rounded-xl font-semibold text-xs sm:text-sm flex items-center gap-2.5 transition ...">
  <i data-lucide="utensils" class="w-4 h-4"></i>
  <span>Quản lý Thực đơn & Topping</span>
  <span id="tab-catalog-counter" class="px-2 py-0.5 rounded-full text-[11px] font-mono font-bold bg-emerald-100 text-emerald-800">12 món</span>
</button>
```

### 3.2 Segmented Pills for View Switching
Inside `#view-catalog`:
- `[Món & Định giá]` (`catalog-items`)
- `[Danh mục]` (`catalog-categories`)
- `[Nhóm Topping]` (`catalog-modifiers`)
- `[⚡ Ma trận Gán Topping]` (`catalog-linker`)

### 3.3 Menu Item Studio (`#modal-item-form`)
- **Width**: `max-w-4xl`, 2 columns on desktop, responsive stack on mobile.
- **Left Column**:
  - Name input with automatic Vietnamese acronym generation (e.g., "Cà phê muối" $\rightarrow$ `cpm`).
  - Category selector.
  - Multi-size toggle:
    - If disabled: Single base price input in VND.
    - If enabled: Dynamic table for Size S, M, L with radio selector for default size.
  - Image preview with fallback and preset image picker chips.
  - Badge selector (`Bán chạy`, `Món hot`, `Mới`, `None`).
- **Right Column (Modifiers & Toppings)**:
  - Section 1: Inherited groups from category (with toggle to exclude).
  - Section 2: Assigned modifier groups via interactive chips with checkmark, option count, and price summary. Click to toggle.

### 3.4 Modifier Group Studio (`#modal-modifier-group-form`)
- **Name**: Text input (e.g. "Topping Trà Trái Cây").
- **Type**: Single selection (radio) or Multiple selection (checkbox).
- **Min / Max Select**: Numeric steppers ($\ge 48\text{px}$ touch targets).
- **Dynamic Option Rows**:
  - Table: Option Name, Price (VND), Remove Action.
  - `[+ Thêm dòng Topping]` button. Pressing `Enter` in the price field automatically appends a new row and focuses its name field.

### 3.5 Interactive Batch Linker Matrix (`#view-catalog-linker`)
3-column desktop layout:
- **Column 1 (28% width - Modifier Groups Rail)**:
  - List of modifier groups with active state indicator and count of assigned items.
  - Quick action to create new group.
- **Column 2 (42% width - Target Items Tree)**:
  - Accordion / grouped tree by Category.
  - Category-level bulk action: `[Chọn tất cả món Trà (1-Click)]`.
  - Item-level checkboxes with item thumbnail, name, and current pricing.
  - Sticky bottom commit action: `[LƯU ÁP DỤNG NGAY]` (Emerald, 56px height, with sound chime).
- **Column 3 (30% width - Live Cashier POS Simulator)**:
  - Framed tablet mockup showing the Cashier Modifier Popover for the currently selected item.
  - Real-time updates as toppings are toggled on/off in Column 2.

---

## 4. Keyboard & Audio Ergonomics

- **Keyboard Shortcuts**:
  - `Ctrl + N`: Open New Item Form.
  - `Ctrl + S`: Commit and save active form or batch matrix.
  - `Esc`: Close any open modal or drawer.
  - `Enter` in option price: Append next option row.
- **Web Audio API Feedback**:
  - Chip / Checkbox toggle: 800Hz sine chirp (30ms).
  - Save success: Arpeggio chord (523.25Hz $\rightarrow$ 1046.5Hz).
  - Form validation failure: Low buzz error tone (150Hz) + UI shake animation.

---

## 5. Verification & Testing Plan

1. **Category Management**:
   - Create a new category "Đồ uống hạt & Sữa thực vật" with icon `nut`.
   - Verify category appears in Category lists and filters.
2. **Item Management with Multi-Size**:
   - Create "Trà xoài macchiato", set Sizes M (42.000đ) and L (49.000đ).
   - Verify acronym `txm` auto-generates.
   - Assign modifier groups and save.
3. **Modifier Group Creation**:
   - Create group "Topping Trái Cây Tươi" with options: Đào miếng (+10.000đ), Nha đam (+8.000đ), Hạt sen (+12.000đ).
   - Test `Enter` key auto-row creation.
4. **Batch Linker Matrix**:
   - Select "Topping Trái Cây Tươi" in Column 1.
   - Click "Chọn tất cả món Trà" in Column 2.
   - Observe real-time updates in Column 3 (Live POS Simulator).
   - Save and verify toast and audio chime.
5. **Cross-Tab Cashier Verification**:
   - Switch to `index.html` (F1).
   - Verify newly added drinks and toppings appear in the catalog and open properly in the cashier ordering modal.
