# Admin Menu, Category, Topping Management & Batch Linker Matrix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build an administrative catalog studio in the POS Cafe web app (`settings.html` and `pos-bus.js`) allowing managers to create and manage menu items with multi-size pricing, categories, and modifier groups, powered by a 3-column Batch Linker Matrix with a Live Cashier Simulator that eliminates friction when assigning toppings to products, synced across tabs to the Cashier Terminal (`index.html`).

**Architecture:** Extend `pos-bus.js` with structured catalog persistence (`POS_CATALOG_DATA`) and `BroadcastChannel` events. Implement the Catalog Studio directly within `settings.html` under a new primary sub-nav tab featuring 4 segmented views (Items, Categories, Modifier Groups, and Batch Linker). Cashier terminal (`index.html`) subscribes to catalog updates to reflect new items and toppings dynamically.

**Tech Stack:** HTML5, Tailwind CSS v3/v4 CDN, Lucide Icons, Web Audio API synthesis, Vanilla ES6+ JavaScript, BroadcastChannel, localStorage.

**Spec:** [`docs/superpowers/specs/2026-09-20-admin-menu-topping-forms-design.md`](file:///E:/Code/pos-cafe/docs/superpowers/specs/2026-09-20-admin-menu-topping-forms-design.md)

## Global Constraints
- Touch targets must be $\ge 48\text{px}$ (`min-h-[48px]`, `min-w-[48px]`).
- Strict typography: `Outfit` for UI & headings, `JetBrains Mono` for currency and numeric metrics.
- Currency formatting: Whole Vietnamese Dong formatted with dot thousand-separators (e.g. `35.000 đ`). No fractional decimals.
- Color palette: Crisp Emerald (`#059669` / `#10b981`), Slate-900 typography, Slate-50 background. AI-purple gradients and pure black (`#000000`) are banned.
- Audio synthesis: Web Audio API sine/triangle oscillators for tactile clicks and two-tone cash register chimes.

---

### Task 1: Catalog Data Layer & Cross-Tab Event Bus Extension

**Files:**
- Modify: `design-system/pos-cafe/shared/pos-bus.js`
- Test: `tests/pos-bus-catalog.test.js`

**Interfaces:**
- Consumes: Existing `readStorage()`, `writeStorage()`, `publish()` from `pos-bus.js`.
- Produces:
  - `window.POS_BUS.getCatalogData()` $\rightarrow$ `{ categories, items, modifierGroups }`
  - `window.POS_BUS.saveCatalogData(data)` $\rightarrow$ `data`
  - `window.POS_BUS.saveMenuItem(item)` $\rightarrow$ `item`
  - `window.POS_BUS.deleteMenuItem(itemId)` $\rightarrow$ `boolean`
  - `window.POS_BUS.saveCategory(category)` $\rightarrow$ `category`
  - `window.POS_BUS.deleteCategory(categoryId)` $\rightarrow$ `boolean`
  - `window.POS_BUS.saveModifierGroup(group)` $\rightarrow$ `group`
  - `window.POS_BUS.deleteModifierGroup(groupId)` $\rightarrow$ `boolean`
  - `window.POS_BUS.batchAssignModifierGroup(groupId, itemIds)` $\rightarrow$ `void`
  - Broadcast Event: `CATALOG_ITEMS_UPDATED` with payload `{ catalogData, action, targetId }`

- [ ] **Step 1: Write the test verifying catalog bus CRUD and batch assignment**

Create `tests/pos-bus-catalog.test.js`:
```javascript
const assert = require('assert');
const fs = require('fs');
const path = require('path');

// Mock localStorage and window
const storage = {};
global.window = {
  localStorage: {
    getItem: (k) => storage[k] || null,
    setItem: (k, v) => { storage[k] = String(v); },
    removeItem: (k) => { delete storage[k]; }
  }
};

// Load pos-bus.js
require(path.join(__dirname, '../design-system/pos-cafe/shared/pos-bus.js'));
const bus = global.window.POS_BUS;

console.log('--- Testing POS_BUS Catalog API ---');

// Test 1: Seed data initialization
const catalog = bus.getCatalogData();
assert(catalog && Array.isArray(catalog.categories), 'Catalog must contain categories array');
assert(Array.isArray(catalog.items), 'Catalog must contain items array');
assert(Array.isArray(catalog.modifierGroups), 'Catalog must contain modifierGroups array');
console.log('✓ Test 1 Passed: Seed catalog initialized with categories and items');

// Test 2: Save new menu item
const newItem = {
  id: 'tra_dao_cam_sa_test',
  name: 'Trà đào cam sả Test',
  categoryId: 'tea',
  categoryName: 'Trà & Macchiato',
  acronym: 'tdcst',
  basePrice: 45000,
  sizes: [
    { id: 'M', name: 'Size M', price: 45000, isDefault: true },
    { id: 'L', name: 'Size L', price: 52000 }
  ],
  defaultSize: 'M',
  assignedModifierGroupIds: ['top_tea']
};
bus.saveMenuItem(newItem);
const fetched = bus.getCatalogData().items.find(i => i.id === newItem.id);
assert(fetched && fetched.name === newItem.name, 'Item must be saved and retrievable');
console.log('✓ Test 2 Passed: Menu item saved successfully');

// Test 3: Batch assign modifier group
bus.batchAssignModifierGroup('top_cheese', [newItem.id]);
const fetchedAfterBatch = bus.getCatalogData().items.find(i => i.id === newItem.id);
assert(fetchedAfterBatch.assignedModifierGroupIds.includes('top_cheese'), 'Modifier group must be batch assigned');
console.log('✓ Test 3 Passed: Modifier group batch assigned successfully');

// Clean up
bus.deleteMenuItem(newItem.id);
assert(!bus.getCatalogData().items.some(i => i.id === newItem.id), 'Item must be deleted');
console.log('✓ Test 4 Passed: Menu item deleted cleanly');
console.log('All Catalog Bus tests passed successfully!');
```

- [ ] **Step 2: Run test to verify it fails before implementation**

Run: `node tests/pos-bus-catalog.test.js`  
Expected: FAIL with `bus.getCatalogData is not a function`

- [ ] **Step 3: Implement catalog methods in `pos-bus.js`**

Add `STORAGE_KEYS.CATALOG_DATA = 'POS_CATALOG_DATA'`, define `DEFAULT_CATALOG_DATA` with full seed items, categories, and modifier groups (`sugar`, `ice`, `top_tea`, `top_cheese`), and implement:
- `getCatalogData()`
- `saveCatalogData(data)`
- `saveMenuItem(item)`
- `deleteMenuItem(itemId)`
- `saveCategory(category)`
- `deleteCategory(categoryId)`
- `saveModifierGroup(group)`
- `deleteModifierGroup(groupId)`
- `batchAssignModifierGroup(groupId, itemIds)`

- [ ] **Step 4: Run test to verify it passes**

Run: `node tests/pos-bus-catalog.test.js`  
Expected: `All Catalog Bus tests passed successfully!`

- [ ] **Step 5: Commit changes**

```bash
git add design-system/pos-cafe/shared/pos-bus.js tests/pos-bus-catalog.test.js
git commit -m "feat(catalog): add catalog data layer and cross-tab bus methods"
```

---

### Task 2: Sub-Nav Tab & Catalog Studio Shell in `settings.html`

**Files:**
- Modify: `design-system/pos-cafe/pages/settings.html`

**Interfaces:**
- Consumes: `window.POS_BUS.getCatalogData()`.
- Produces:
  - Header Sub-Nav Tab: `#tab-btn-catalog`
  - Main Catalog Container: `#view-catalog`
  - Segmented Pills Controller: `switchCatalogSubView(subViewName)` ('items', 'categories', 'modifiers', 'linker')
  - Live Counter: `#tab-catalog-counter`

- [ ] **Step 1: Add Sub-Nav button for Catalog Studio**

In `settings.html` sub-nav list (around line 350):
```html
<!-- Tab 2: Quản lý Thực đơn & Topping -->
<button id="tab-btn-catalog" type="button" onclick="switchSettingsTab('catalog')" role="tab" aria-selected="false"
  class="min-h-[48px] h-12 px-4 py-2 rounded-xl font-semibold text-xs sm:text-sm flex items-center gap-2.5 transition text-slate-600 hover:text-slate-900 hover:bg-slate-100 cursor-pointer select-none border border-transparent">
  <i data-lucide="utensils" class="w-4 h-4 text-slate-400"></i>
  <span>Quản lý Thực đơn & Topping</span>
  <span id="tab-catalog-counter" class="px-2 py-0.5 rounded-full text-[11px] font-mono font-bold bg-slate-100 text-slate-600 border border-slate-200">11 món</span>
</button>
```

- [ ] **Step 2: Add Main Catalog View `#view-catalog` with Segmented Control Pills**

Add `<section id="view-catalog" class="space-y-6 hidden animate-card-in">` with:
- Top Header Strip introducing Menu & Topping Studio with quick action `[+ THÊM MÓN MỚI (Ctrl+N)]`.
- Segmented Pills Bar:
  - `catalog-pill-items`: `[Món & Định giá]`
  - `catalog-pill-categories`: `[Danh mục món]`
  - `catalog-pill-modifiers`: `[Nhóm Topping]`
  - `catalog-pill-linker`: `[⚡ Ma trận Gán Topping (Batch Linker)]`
- Four view containers:
  - `#catalog-subview-items`
  - `#catalog-subview-categories`
  - `#catalog-subview-modifiers`
  - `#catalog-subview-linker`

- [ ] **Step 3: Update `switchSettingsTab` and implement `switchCatalogSubView` in script**

Support `'catalog'` tab in `switchSettingsTab(tabKey)`, update aria-selected, toggle active pill styles, and implement `switchCatalogSubView(viewKey)`.

- [ ] **Step 4: Verify in browser / syntax check**

Ensure no JavaScript errors when clicking the new tab and switching between subviews.

- [ ] **Step 5: Commit changes**

```bash
git add design-system/pos-cafe/pages/settings.html
git commit -m "feat(settings): add catalog studio tab and segmented sub-nav shell"
```

---

### Task 3: Menu Items & Multi-Size Pricing View & Modal Form

**Files:**
- Modify: `design-system/pos-cafe/pages/settings.html`

**Interfaces:**
- Consumes: `POS_BUS.getCatalogData().items`, `categories`, `modifierGroups`.
- Produces:
  - `renderCatalogItemsGrid()`
  - `openMenuItemModal(itemId)` (null for create, id for edit)
  - `closeMenuItemModal()`
  - `saveMenuItemForm(event)`
  - `deleteMenuItemConfirmed(itemId)`
  - Auto-acronym generator `handleItemNameInput(name)`

- [ ] **Step 1: Build Menu Items Grid & Filter Bar in `#catalog-subview-items`**

Render:
- Filter bar: Search input with Vietnamese acronym matching (`cfsd`, `tdcs`), category dropdown, and `[+ THÊM MÓN MỚI (Ctrl+N)]` button.
- Responsive Card Grid (`grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4`):
  - Thumbnail with category fallback icon.
  - Item name, category badge, acronym badge (`[cfsd]`).
  - Size pricing tags: `[Size M: 35.000 đ]`, `[Size L: 42.000 đ]`.
  - Assigned modifier group chips: `[+ Topping Trà]`, `[+ Kem Cheese]`.
  - Action buttons: `[Sửa món]` and `[Gán Topping]` (jumps to Batch Linker with this item pre-focused).

- [ ] **Step 2: Build 2-Column Menu Item Modal `#modal-item-form`**

Modal structure:
- **Left Column**:
  - Item Name input (`oninput="handleItemNameInput(this.value)"`) $\rightarrow$ auto-fills Acronym input.
  - Category selector (select dropdown with existing categories).
  - Multi-size Switch toggle:
    - If off: Single Base Price input (`min-h-[48px]`, formatted VND).
    - If on: Interactive Sizes Table (Size S, M, L prices + Radio selector for default size + Add/Remove size button).
  - Image preset chips & Custom URL input with live preview.
  - Badge selector (`Bán chạy`, `Món hot`, `Mới`, `None`).
  - Description textarea.
- **Right Column (Modifiers & Toppings)**:
  - Category inherited modifier groups indicator (with toggle to exclude).
  - Additional modifier groups selection chips (Emerald when selected, dashed outline when unselected). Click to toggle.

- [ ] **Step 3: Implement JavaScript handlers in `settings.html`**

Implement:
- `renderCatalogItemsGrid()`
- `openMenuItemModal(itemId)`
- `closeMenuItemModal()`
- `saveMenuItemForm()`: validates required name and non-negative prices, saves via `POS_BUS.saveMenuItem(item)`, triggers audio chime, closes modal, and refreshes grid.
- `deleteMenuItemConfirmed(itemId)`

- [ ] **Step 4: Verify item creation and editing**

Create a test drink with multi-size pricing and assigned modifier groups, verify save and render.

- [ ] **Step 5: Commit changes**

```bash
git add design-system/pos-cafe/pages/settings.html
git commit -m "feat(catalog): implement menu items view and multi-size modal studio"
```

---

### Task 4: Categories & Modifier Groups Studio Views & Modals

**Files:**
- Modify: `design-system/pos-cafe/pages/settings.html`

**Interfaces:**
- Consumes: `POS_BUS.getCatalogData().categories`, `modifierGroups`.
- Produces:
  - `renderCatalogCategoriesGrid()`
  - `openCategoryModal(catId)` / `saveCategoryForm()`
  - `renderCatalogModifierGroupsGrid()`
  - `openModifierGroupModal(groupId)` / `saveModifierGroupForm()`
  - Dynamic option rows: `addModifierOptionRow()`, `removeModifierOptionRow(index)`

- [ ] **Step 1: Build Categories View & Modal (`#catalog-subview-categories`)**

- Grid of Category Bento Cards showing icon, name, item count, default inherited modifier groups, and edit button.
- Modal `#modal-category-form`:
  - Name input.
  - Icon picker: Grid of Lucide icon buttons (`coffee`, `cup-soda`, `leaf`, `sparkles`, `cake`, `layers`, `heart`, `flame`, `zap`) allowing 1-click icon selection.
  - Default modifier groups multi-select chips.
  - Save / Delete actions.

- [ ] **Step 2: Build Modifier Groups View (`#catalog-subview-modifiers`)**

- Grid of Modifier Group Cards showing:
  - Group name (e.g. "Topping Trà Trái Cây").
  - Selection rule badge (`Chọn 1 duy nhất` / `Chọn nhiều 0-5`).
  - List of child options with prices (`Trân châu trắng +10.000 đ`).
  - Count of items currently using this group.
  - Action buttons: `[Chỉnh sửa]` and `[Gán nhanh vào món...]` (switches to Batch Linker).

- [ ] **Step 3: Build Modifier Group Modal (`#modal-modifier-group-form`) with Dynamic Option Rows**

Modal `#modal-modifier-group-form`:
- Group Name input.
- Selection Type selector: Radio buttons for Single (`'single'`) vs Multiple (`'multiple'`).
- Min / Max number inputs ($\ge 48\text{px}$ touch targets).
- Dynamic Child Options Table:
  - Rows with Option Name input, Surcharge VND input, and Delete row button.
  - `[+ Thêm dòng Topping]` button.
  - Pressing `Enter` in the price input automatically triggers `addModifierOptionRow()` and focuses the new name input.

- [ ] **Step 4: Implement script logic and verify**

Implement `renderCatalogCategoriesGrid()`, `renderCatalogModifierGroupsGrid()`, `saveCategoryForm()`, `saveModifierGroupForm()`. Test creating a new category and a new modifier group.

- [ ] **Step 5: Commit changes**

```bash
git add design-system/pos-cafe/pages/settings.html
git commit -m "feat(catalog): implement category and modifier group studio views"
```

---

### Task 5: Interactive Batch Linker Matrix (Anti-Complexity UX)

**Files:**
- Modify: `design-system/pos-cafe/pages/settings.html`

**Interfaces:**
- Consumes: `POS_BUS.getCatalogData()`.
- Produces:
  - `renderBatchLinkerMatrix()`
  - `selectLinkerModifierGroup(groupId)`
  - `toggleLinkerItem(itemId)`
  - `toggleLinkerCategoryAll(categoryId)`
  - `updateLiveSimulator(itemId)`
  - `saveBatchLinkerAssignments()`

- [ ] **Step 1: Build 3-Column Batch Linker Layout (`#catalog-subview-linker`)**

Create desktop 3-column split (`grid grid-cols-1 lg:grid-cols-12 gap-5`):
- **Column 1 (lg:col-span-3)**: Modifier Groups selection list. Highlights currently active group, shows assigned items badge (e.g. `Đang gán 4 món`).
- **Column 2 (lg:col-span-5)**: Target Items Accordion grouped by Category.
  - Category header with 1-click action: `[Chọn tất cả món [Tên nhóm]]`.
  - Item rows with checkbox ($\ge 48\text{px}$ touch row), item thumbnail, name, price, and preview trigger.
  - Sticky bottom action bar: `[LƯU ÁP DỤNG NGAY (Enter)]` (Emerald, 56px height) with assigned items summary badge.
- **Column 3 (lg:col-span-4)**: Live Cashier POS Simulator.
  - Clean tablet bezel container rendering the exact Cashier Modifier Popover layout (Size chips, Sugar/Ice, and Topping chips) for the selected item.
  - Reacts instantly as checkboxes in Column 2 are toggled.

- [ ] **Step 2: Implement Batch Linker state and interactions**

In `settings.html`:
```javascript
let linkerState = {
  activeGroupId: 'top_tea',
  selectedItemIds: new Set(),
  simulatedItemId: 'tdcs'
};
```
Implement:
- `selectLinkerModifierGroup(groupId)`: Loads current item assignments into `linkerState.selectedItemIds`.
- `toggleLinkerCategoryAll(categoryId)`: Toggles all items in the given category in 1 click.
- `toggleLinkerItem(itemId)`: Toggles individual item.
- `updateLiveSimulator(itemId)`: Renders the simulated popover in Column 3.
- `saveBatchLinkerAssignments()`: Calls `POS_BUS.batchAssignModifierGroup(groupId, Array.from(linkerState.selectedItemIds))`, plays cash register sound chime, shows toast notification, and updates counters.

- [ ] **Step 3: Verify Batch Linker interaction**

Test selecting a modifier group, clicking "Chọn tất cả món Trà", observing the Live Simulator update immediately, and saving.

- [ ] **Step 4: Commit changes**

```bash
git add design-system/pos-cafe/pages/settings.html
git commit -m "feat(catalog): implement 3-column interactive batch linker matrix"
```

---

### Task 6: Cashier Terminal Sync Integration & End-to-End Verification

**Files:**
- Modify: `design-system/pos-cafe/index.html`
- Modify: `design-system/pos-cafe/pages/settings.html` (keyboard shortcuts binding)
- Test: `tests/e2e-catalog-sync.test.js`

**Interfaces:**
- Consumes: `POS_BUS.getCatalogData()`, `POS_BUS.subscribe('CATALOG_ITEMS_UPDATED')`.
- Produces: Dynamic catalog rendering in Cashier Terminal.

- [ ] **Step 1: Write E2E test script for catalog sync**

Create `tests/e2e-catalog-sync.test.js`:
```javascript
const assert = require('assert');
const path = require('path');

const storage = {};
global.window = {
  localStorage: {
    getItem: (k) => storage[k] || null,
    setItem: (k, v) => { storage[k] = String(v); },
    removeItem: (k) => { delete storage[k]; }
  },
  BroadcastChannel: function() {
    return { postMessage: () => {}, close: () => {} };
  }
};

require(path.join(__dirname, '../design-system/pos-cafe/shared/pos-bus.js'));
const bus = global.window.POS_BUS;

console.log('--- E2E Catalog Sync Test ---');

// 1. Create a custom category
const cat = bus.saveCategory({ id: 'cat_signature', name: 'Món Chữ Ký', icon: 'sparkles', order: 9 });
assert(bus.getCatalogData().categories.some(c => c.id === 'cat_signature'), 'New category must exist');

// 2. Create a custom beverage
const drink = bus.saveMenuItem({
  id: 'matcha_kem_dua',
  name: 'Matcha kem dừa',
  categoryId: 'cat_signature',
  categoryName: 'Món Chữ Ký',
  acronym: 'mkd',
  basePrice: 55000,
  sizes: [
    { id: 'M', name: 'Size M', price: 55000, isDefault: true },
    { id: 'L', name: 'Size L', price: 62000 }
  ],
  defaultSize: 'M',
  assignedModifierGroupIds: ['top_tea']
});
assert(bus.getCatalogData().items.some(i => i.id === 'matcha_kem_dua'), 'New item must exist');

// 3. Batch assign modifier
bus.batchAssignModifierGroup('top_cheese', [drink.id]);
const updated = bus.getCatalogData().items.find(i => i.id === drink.id);
assert(updated.assignedModifierGroupIds.includes('top_cheese'), 'Batch assign must persist');

console.log('✓ E2E Test Passed: Complete catalog lifecycle verified');
```

- [ ] **Step 2: Update `index.html` to consume dynamic catalog data**

In `index.html`:
- On initialization, read `window.POS_BUS.getCatalogData()`. If present, hydrate `state.categories`, `state.menuItems`, and modifier groups dynamically.
- Subscribe to `CATALOG_ITEMS_UPDATED`:
  ```javascript
  window.POS_BUS.subscribe('CATALOG_ITEMS_UPDATED', function(payload) {
    hydrateCatalogFromBus();
    renderCategories();
    renderProductGrid();
  });
  ```
- In `openModifierModal(item)`, dynamically lookup modifier groups assigned to `item` (`item.assignedModifierGroupIds`) from `catalogData.modifierGroups` and render dynamic topping options.

- [ ] **Step 3: Run E2E test script**

Run: `node tests/e2e-catalog-sync.test.js`  
Expected: `✓ E2E Test Passed: Complete catalog lifecycle verified`

- [ ] **Step 4: Bind keyboard shortcuts in `settings.html`**

- `Ctrl + N`: Open New Item Form if on catalog tab.
- `Ctrl + S`: Trigger save on currently open modal or Batch Linker.
- `Esc`: Close any open modal.

- [ ] **Step 5: Commit changes**

```bash
git add design-system/pos-cafe/index.html design-system/pos-cafe/pages/settings.html tests/e2e-catalog-sync.test.js
git commit -m "feat(pos): integrate dynamic catalog sync between admin settings and cashier terminal"
```
