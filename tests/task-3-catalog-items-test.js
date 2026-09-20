/**
 * POS Cafe - Task 3 Verification Test: Menu Items & Multi-Size Pricing View & Modal Form
 * 
 * Verifies:
 * 1. Element presence in settings.html:
 *    - #catalog-subview-items:
 *      * Search input (#catalog-items-search-input) with clear button
 *      * Category filter select (#catalog-items-category-filter)
 *      * Primary CTA (+ THÊM MÓN MỚI (Ctrl+N))
 *      * Responsive Cards Grid (#catalog-items-grid)
 *    - 2-Column Menu Item Modal (#modal-item-form):
 *      * Left column: #item-form-name, #item-form-acronym, #item-form-category,
 *        #btn-toggle-multi-size, #item-single-price-container, #item-multi-size-container,
 *        #btn-add-size-row, #item-form-image, preset chips, marketing badge selector, #item-form-description
 *      * Right column: #item-inherited-modifiers-list, #item-additional-modifiers-list,
 *        price preview (#item-preview-base-price, #item-preview-groups-count, #item-preview-sizes-range)
 *      * Modal footer: Cancel button, Save button [LƯU MÓN ĂN (Ctrl+S)], Delete button (#btn-item-form-delete)
 * 2. Strict Design Compliance:
 *    - All touch targets >= 48px
 *    - Zero em-dashes
 *    - Zero Unicode emojis
 *    - Zero AI-purple gradients
 *    - Whole VND formatting without decimals
 * 3. Behavioral Runtime Simulation:
 *    - Vietnamese acronym generator ('cfsd', 'tdcs', 'bx')
 *    - Multi-factor search matcher
 *    - renderCatalogItemsGrid() rendering and filtering
 *    - openMenuItemModal() create vs edit mode
 *    - handleItemNameInput() auto acronym generation
 *    - Sizes matrix add, remove, and select default size
 *    - Modifier groups inheritance, exclude toggles, and additional group chips
 *    - saveMenuItemForm() creating and editing items with automatic basePrice calculation
 *    - deleteMenuItemConfirmed() removing items
 *    - jumpToBatchLinker() pre-focusing item
 */

const fs = require('fs');
const path = require('path');
const vm = require('vm');

const SETTINGS_PATH = path.join(__dirname, '..', 'design-system', 'pos-cafe', 'pages', 'settings.html');
const POS_BUS_PATH = path.join(__dirname, '..', 'design-system', 'pos-cafe', 'shared', 'pos-bus.js');

let totalTests = 0;
let passedTests = 0;
let failedTests = 0;

function assert(condition, testName, errorDetails = '') {
  totalTests++;
  if (condition) {
    passedTests++;
    console.log(`  [PASS] ${testName}`);
  } else {
    failedTests++;
    console.error(`  [FAIL] ${testName}: ${errorDetails}`);
  }
}

console.log('================================================================');
console.log('  TASK 3: MENU ITEMS GRID & 2-COLUMN MODAL VERIFICATION');
console.log('================================================================\n');

// 1. Static HTML Verification
console.log('--- 1. Checking Static HTML Structure in settings.html ---');
assert(fs.existsSync(SETTINGS_PATH), 'settings.html exists');
const html = fs.readFileSync(SETTINGS_PATH, 'utf8');

// #catalog-subview-items Filter Bar
assert(html.includes('id="catalog-subview-items"'), '#catalog-subview-items container exists');
assert(html.includes('id="catalog-items-search-input"'), '#catalog-items-search-input exists');
assert(html.includes('id="btn-clear-items-search"'), '#btn-clear-items-search clear button exists');
assert(html.includes('id="catalog-items-category-filter"'), '#catalog-items-category-filter exists');
assert(html.includes('id="btn-catalog-items-add-item"'), '#btn-catalog-items-add-item CTA exists');
assert(html.includes('+ THÊM MÓN MỚI (Ctrl+N)'), 'Primary CTA text "+ THÊM MÓN MỚI (Ctrl+N)" exists');
assert(html.includes('id="catalog-items-count-display"'), '#catalog-items-count-display exists');

// Responsive Grid
assert(html.includes('id="catalog-items-grid"'), '#catalog-items-grid container exists');
assert(html.includes('grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4'), 'Grid has responsive 1/2/3 columns classes');

// 2-Column Menu Item Modal (#modal-item-form)
assert(html.includes('id="modal-item-form"'), '#modal-item-form modal dialog exists');
assert(html.includes('id="modal-item-form-title"'), '#modal-item-form-title heading exists');
assert(html.includes('id="form-menu-item"'), '#form-menu-item form exists');

// Left Column inputs
assert(html.includes('id="item-form-name"'), '#item-form-name input exists');
assert(html.includes('id="item-form-acronym"'), '#item-form-acronym input exists');
assert(html.includes('id="item-form-category"'), '#item-form-category select exists');
assert(html.includes('id="btn-toggle-multi-size"'), '#btn-toggle-multi-size toggle button exists');
assert(html.includes('id="item-single-price-container"'), '#item-single-price-container exists');
assert(html.includes('id="item-form-base-price"'), '#item-form-base-price input exists');
assert(html.includes('id="item-multi-size-container"'), '#item-multi-size-container exists');
assert(html.includes('id="btn-add-size-row"'), '#btn-add-size-row add size button exists');
assert(html.includes('id="item-sizes-list"'), '#item-sizes-list container exists');
assert(html.includes('id="item-form-image"'), '#item-form-image input exists');
assert(html.includes('id="item-form-image-preview-thumb"'), '#item-form-image-preview-thumb exists');
assert(html.includes('Cà phê sữa') && html.includes('Trà đào') && html.includes('Freeze đá xay'), 'Preset image chips exist');
assert(html.includes('id="item-marketing-badge-container"'), '#item-marketing-badge-container exists');
assert(html.includes('Bán chạy') && html.includes('Món hot') && html.includes('Chef Pick'), 'Marketing badge options exist');
assert(html.includes('id="item-form-description"'), '#item-form-description textarea exists');

// Right Column inputs
assert(html.includes('id="item-inherited-modifiers-list"'), '#item-inherited-modifiers-list exists');
assert(html.includes('id="item-additional-modifiers-list"'), '#item-additional-modifiers-list exists');
assert(html.includes('id="item-preview-base-price"'), '#item-preview-base-price readout exists');
assert(html.includes('id="item-preview-groups-count"'), '#item-preview-groups-count readout exists');
assert(html.includes('id="item-preview-sizes-range"'), '#item-preview-sizes-range readout exists');

// Modal Footer buttons
assert(html.includes('id="btn-item-form-delete"'), '#btn-item-form-delete button exists');
assert(html.includes('id="btn-item-form-save"'), '#btn-item-form-save button exists');
assert(html.includes('LƯU MÓN ĂN (Ctrl+S)'), 'Save button label "LƯU MÓN ĂN (Ctrl+S)" exists');

// Anti-slop design checks
console.log('\n--- 2. Design System & Anti-Slop Compliance ---');
const emojiRegex = /[\u{1F300}-\u{1F9FF}\u{2600}-\u{26FF}\u{2700}-\u{27BF}]/u;
assert(!emojiRegex.test(html), 'Zero Unicode emojis in settings.html');
assert(!html.includes('\u2014') && !html.includes('\u2013'), 'Zero em-dashes in settings.html');
const hasPurple = /bg-gradient-to-[a-z]+\s+from-(?:purple|violet|fuchsia)/i.test(html);
assert(!hasPurple, 'Zero AI-purple gradients in settings.html');

// Button Touch Target Verification
console.log('\n--- 3. Checking Touch Targets on Task 3 Buttons (>= 48px) ---');
const task3ButtonIds = [
  'btn-catalog-items-add-item',
  'btn-clear-items-search',
  'btn-toggle-multi-size',
  'btn-add-size-row',
  'btn-item-form-delete',
  'btn-item-form-save'
];

task3ButtonIds.forEach(id => {
  const regex = new RegExp(`<button[^>]*id="${id}"[^>]*>`, 'i');
  const match = html.match(regex);
  assert(!!match, `<button id="${id}"> tag found`);
  if (match) {
    const btnTag = match[0];
    const hasMinH = btnTag.includes('min-h-[48px]') || btnTag.includes('h-12');
    assert(hasMinH, `<button id="${id}"> enforces >= 48px touch target`);
  }
});

// All buttons in settings.html touch target audit
const allButtonTags = html.match(/<button[^>]*>/g) || [];
let violations = [];
allButtonTags.forEach(btn => {
  const pxMatch = btn.match(/min-h-\[(\d+)px\]/);
  const pxHeight = pxMatch ? parseInt(pxMatch[1], 10) : 0;
  const isBig = pxHeight >= 48 ||
                btn.includes('h-12') ||
                btn.includes('h-14') ||
                btn.includes('h-16') ||
                btn.includes('min-h-12') ||
                btn.includes('min-h-14') ||
                btn.includes('min-h-16') ||
                btn.includes('btnClass');
  if (!isBig) {
    violations.push(btn.substring(0, 70));
  }
});
assert(violations.length === 0, `All ${allButtonTags.length} <button> tags in settings.html enforce >= 48px touch target`);

// Currency check: Ensure whole VND format without decimals
const decimalVNDMatches = html.match(/[0-9]+[.,][0-9]{1,2}\s*(?:đ|vnd|\$)/gi) || [];
assert(decimalVNDMatches.length === 0, 'settings.html strictly uses whole VND without decimals');

// 4. Behavioral Runtime Simulation
console.log('\n--- 4. Behavioral Runtime Simulation ---');

const inMemoryStorage = {};

// Mock DOM elements
function createMockElement(id, initialClasses = []) {
  const classes = new Set(initialClasses);
  const attributes = {};
  return {
    id: id,
    textContent: '',
    innerHTML: '',
    value: '',
    classList: {
      add: (c) => classes.add(c),
      remove: (c) => classes.delete(c),
      contains: (c) => classes.has(c)
    },
    get className() {
      return Array.from(classes).join(' ');
    },
    set className(val) {
      classes.clear();
      (val || '').split(/\s+/).filter(Boolean).forEach(c => classes.add(c));
    },
    setAttribute: (k, v) => { attributes[k] = String(v); },
    getAttribute: (k) => attributes[k] || null,
    focus: () => {},
    querySelectorAll: () => [],
    querySelector: () => null
  };
}

const mockDOM = {
  'catalog-items-search-input': createMockElement('catalog-items-search-input'),
  'btn-clear-items-search': createMockElement('btn-clear-items-search', ['hidden']),
  'catalog-items-category-filter': createMockElement('catalog-items-category-filter'),
  'btn-catalog-items-add-item': createMockElement('btn-catalog-items-add-item'),
  'catalog-items-count-display': createMockElement('catalog-items-count-display'),
  'catalog-items-filter-label': createMockElement('catalog-items-filter-label'),
  'catalog-items-grid': createMockElement('catalog-items-grid'),
  'modal-item-form': createMockElement('modal-item-form', ['hidden']),
  'modal-item-form-title': createMockElement('modal-item-form-title'),
  'modal-backdrop-overlay': createMockElement('modal-backdrop-overlay', ['hidden']),
  'item-form-id': createMockElement('item-form-id'),
  'item-form-name': createMockElement('item-form-name'),
  'item-form-acronym': createMockElement('item-form-acronym'),
  'item-form-category': createMockElement('item-form-category'),
  'btn-toggle-multi-size': createMockElement('btn-toggle-multi-size'),
  'toggle-multi-size-label': createMockElement('toggle-multi-size-label'),
  'toggle-multi-size-indicator': createMockElement('toggle-multi-size-indicator'),
  'item-single-price-container': createMockElement('item-single-price-container', ['hidden']),
  'item-form-base-price': createMockElement('item-form-base-price'),
  'item-multi-size-container': createMockElement('item-multi-size-container'),
  'btn-add-size-row': createMockElement('btn-add-size-row'),
  'item-sizes-list': createMockElement('item-sizes-list'),
  'item-form-image': createMockElement('item-form-image'),
  'item-form-image-preview-thumb': createMockElement('item-form-image-preview-thumb'),
  'item-form-badge': createMockElement('item-form-badge'),
  'item-marketing-badge-container': createMockElement('item-marketing-badge-container'),
  'item-form-description': createMockElement('item-form-description'),
  'item-inherited-modifiers-list': createMockElement('item-inherited-modifiers-list'),
  'item-additional-modifiers-list': createMockElement('item-additional-modifiers-list'),
  'item-preview-base-price': createMockElement('item-preview-base-price'),
  'item-preview-groups-count': createMockElement('item-preview-groups-count'),
  'item-preview-sizes-range': createMockElement('item-preview-sizes-range'),
  'btn-item-form-delete': createMockElement('btn-item-form-delete', ['hidden']),
  'btn-item-form-save': createMockElement('btn-item-form-save'),
  'tab-catalog-counter': createMockElement('tab-catalog-counter'),
  'pill-badge-items': createMockElement('pill-badge-items'),
  'pill-badge-categories': createMockElement('pill-badge-categories'),
  'pill-badge-modifiers': createMockElement('pill-badge-modifiers'),
};

global.document = {
  getElementById: (id) => mockDOM[id] || null,
  addEventListener: () => {},
  querySelectorAll: () => []
};

global.window = {
  lucide: { createIcons: () => {} },
  localStorage: {
    getItem: (k) => (Object.prototype.hasOwnProperty.call(inMemoryStorage, k) ? inMemoryStorage[k] : null),
    setItem: (k, v) => { inMemoryStorage[k] = String(v); },
    removeItem: (k) => { delete inMemoryStorage[k]; },
    clear: () => {
      for (const k in inMemoryStorage) delete inMemoryStorage[k];
    }
  },
  addEventListener: () => {}
};

// Load POS_BUS
require(POS_BUS_PATH);
const bus = global.window.POS_BUS;
assert(typeof bus.saveMenuItem === 'function', 'POS_BUS.saveMenuItem is available');
assert(typeof bus.deleteMenuItem === 'function', 'POS_BUS.deleteMenuItem is available');

// Extract script block from settings.html
const scriptBlocks = html.split(/<script[^>]*>/i);
const mainScriptBlock = scriptBlocks.find(b => b.includes('function renderCatalogItemsGrid'));
assert(!!mainScriptBlock, 'Main script block containing renderCatalogItemsGrid found in settings.html');
const scriptContent = mainScriptBlock.split(/<\/script>/i)[0];

const sandbox = {
  document: global.document,
  window: global.window,
  console: console,
  setInterval: () => {},
  setTimeout: setTimeout,
  clearTimeout: clearTimeout,
  Date: Date,
  Math: Math,
  parseInt: parseInt,
  Number: Number,
  String: String,
  Array: Array,
  JSON: JSON,
  currentTab: 'catalog',
  currentCatalogSubView: 'items',
  playTapChirp: () => {},
  playCashChime: () => {},
  showToast: () => {},
  confirm: () => true,
  refreshCatalogCounters: () => {},
  switchCatalogSubView: () => {}
};

vm.createContext(sandbox);

vm.runInContext(`
  ${scriptContent}
  this.removeAccents = removeAccents;
  this.generateVietnameseAcronym = generateVietnameseAcronym;
  this.matchesCatalogItemSearch = matchesCatalogItemSearch;
  this.renderCatalogItemsGrid = renderCatalogItemsGrid;
  this.handleCatalogItemsSearch = handleCatalogItemsSearch;
  this.handleCatalogCategoryFilterChange = handleCatalogCategoryFilterChange;
  this.openMenuItemModal = openMenuItemModal;
  this.closeMenuItemModal = closeMenuItemModal;
  this.handleItemNameInput = handleItemNameInput;
  this.toggleItemMultiSize = toggleItemMultiSize;
  this.addItemSizeRow = addItemSizeRow;
  this.removeItemSizeRow = removeItemSizeRow;
  this.selectItemDefaultSize = selectItemDefaultSize;
  this.handleSizePriceChange = handleSizePriceChange;
  this.toggleItemExcludedModifierGroup = toggleItemExcludedModifierGroup;
  this.toggleItemModifierGroup = toggleItemModifierGroup;
  this.updateItemPricePreview = updateItemPricePreview;
  this.saveMenuItemForm = saveMenuItemForm;
  this.deleteMenuItemConfirmed = deleteMenuItemConfirmed;
  this.jumpToBatchLinker = jumpToBatchLinker;
`, sandbox);

// 4.1 Test Vietnamese acronym generator
assert(sandbox.generateVietnameseAcronym('Cà phê sữa đá') === 'cfsd', 'generateVietnameseAcronym("Cà phê sữa đá") -> "cfsd"');
assert(sandbox.generateVietnameseAcronym('Trà đào cam sả') === 'tdcs', 'generateVietnameseAcronym("Trà đào cam sả") -> "tdcs"');
assert(sandbox.generateVietnameseAcronym('Bạc xỉu') === 'bx', 'generateVietnameseAcronym("Bạc xỉu") -> "bx"');
assert(sandbox.generateVietnameseAcronym('Trà vải hoa hồng') === 'tvhh', 'generateVietnameseAcronym("Trà vải hoa hồng") -> "tvhh"');

// 4.2 Test search matching logic
const testItemCfsd = { id: 'cfsd', name: 'Cà phê sữa đá', acronym: 'cfsd', categoryName: 'Cà phê Việt' };
const testItemTdcs = { id: 'tdcs', name: 'Trà đào cam sả', acronym: 'tdcs', categoryName: 'Trà & Macchiato' };

assert(sandbox.matchesCatalogItemSearch(testItemCfsd, 'cfsd') === true, 'Matches stored acronym "cfsd"');
assert(sandbox.matchesCatalogItemSearch(testItemCfsd, 'CFSD') === true, 'Matches uppercase acronym "CFSD"');
assert(sandbox.matchesCatalogItemSearch(testItemCfsd, 'sua da') === true, 'Matches unaccented multi-word "sua da"');
assert(sandbox.matchesCatalogItemSearch(testItemCfsd, 'tdcs') === false, 'Does not match unrelated acronym "tdcs"');
assert(sandbox.matchesCatalogItemSearch(testItemTdcs, 'tdcs') === true, 'Matches stored acronym "tdcs"');
assert(sandbox.matchesCatalogItemSearch(testItemTdcs, 'tra dao') === true, 'Matches "tra dao"');

// 4.3 Test renderCatalogItemsGrid
sandbox.renderCatalogItemsGrid();
assert(mockDOM['catalog-items-grid'].innerHTML.length > 0, 'renderCatalogItemsGrid populates items cards');
assert(mockDOM['catalog-items-grid'].innerHTML.includes('Cà phê sữa đá'), 'Grid renders "Cà phê sữa đá"');
assert(mockDOM['catalog-items-grid'].innerHTML.includes('Chỉnh sửa'), 'Grid cards include "Chỉnh sửa" action button');
assert(mockDOM['catalog-items-grid'].innerHTML.includes('Gán Topping'), 'Grid cards include "Gán Topping" action button');

// 4.4 Test Search Filter
sandbox.handleCatalogItemsSearch('cfsd');
assert(mockDOM['catalog-items-grid'].innerHTML.includes('Cà phê sữa đá'), 'Search "cfsd" retains "Cà phê sữa đá"');
assert(!mockDOM['catalog-items-grid'].innerHTML.includes('Trà đào cam sả'), 'Search "cfsd" filters out "Trà đào cam sả"');

// 4.5 Test Category Filter
sandbox.handleCatalogItemsSearch('');
sandbox.handleCatalogCategoryFilterChange('tea');
assert(!mockDOM['catalog-items-grid'].innerHTML.includes('Cà phê sữa đá'), 'Category "tea" filters out coffee items');

// Reset filter
sandbox.handleCatalogCategoryFilterChange('all');

// 4.6 Test openMenuItemModal in Create Mode
sandbox.openMenuItemModal(null);
assert(!mockDOM['modal-item-form'].classList.contains('hidden'), 'openMenuItemModal(null) reveals #modal-item-form');
assert(mockDOM['modal-item-form-title'].textContent.includes('Thêm món mới'), 'Create mode sets title "Thêm món mới vào thực đơn"');
assert(mockDOM['btn-item-form-delete'].classList.contains('hidden'), 'Delete button hidden in create mode');

// 4.7 Test live acronym generation on name input
sandbox.handleItemNameInput('Trà ổi hồng muối ớt');
assert(mockDOM['item-form-acronym'].value === 'tohmo', 'handleItemNameInput live auto-fills acronym "tohmo"');

// 4.8 Test Sizes Matrix Manipulation
assert(sandbox.isItemMultiSize === true, 'Multi-size is enabled by default in create mode');
const initialSizesCount = sandbox.currentItemSizes.length;
sandbox.addItemSizeRow('Size XL', 55000, 'XL');
assert(sandbox.currentItemSizes.length === initialSizesCount + 1, 'addItemSizeRow adds new size row');
assert(sandbox.currentItemSizes[sandbox.currentItemSizes.length - 1].price === 55000, 'Added size row has 55.000 đ price');

// Select default size
sandbox.selectItemDefaultSize(sandbox.currentItemSizes.length - 1);
assert(sandbox.currentItemDefaultSizeId === 'XL', 'selectItemDefaultSize sets default size to "XL"');

// 4.9 Test Modifier Groups in Modal
sandbox.toggleItemModifierGroup('top_cheese');
assert(sandbox.currentAssignedModifierGroupIds.includes('top_cheese'), 'toggleItemModifierGroup assigns "top_cheese"');

sandbox.toggleItemExcludedModifierGroup('sugar');
assert(sandbox.currentExcludedModifierGroupIds.includes('sugar'), 'toggleItemExcludedModifierGroup excludes "sugar"');

// 4.10 Test Item Creation & Validation via saveMenuItemForm
mockDOM['item-form-name'].value = 'Trà ổi hồng muối ớt';
mockDOM['item-form-category'].value = 'tea';
mockDOM['item-form-acronym'].value = 'tohmo';

sandbox.saveMenuItemForm();
const catalogAfterSave = bus.getCatalogData();
const createdItem = catalogAfterSave.items.find(i => i.name === 'Trà ổi hồng muối ớt');
assert(!!createdItem, 'New item "Trà ổi hồng muối ớt" saved to catalog in POS_BUS');
assert(createdItem.categoryId === 'tea', 'Saved item has correct category "tea"');
assert(createdItem.acronym === 'tohmo', 'Saved item has correct acronym "tohmo"');
assert(createdItem.basePrice === 55000, 'Saved item basePrice auto-calculated from default size XL (55.000 đ)');
assert(createdItem.assignedModifierGroupIds.includes('top_cheese'), 'Saved item has assigned modifier "top_cheese"');
assert(createdItem.excludedModifierGroupIds.includes('sugar'), 'Saved item has excluded modifier "sugar"');

// 4.11 Test openMenuItemModal in Edit Mode
sandbox.openMenuItemModal(createdItem.id);
assert(mockDOM['modal-item-form-title'].textContent.includes('Chỉnh sửa món'), 'Edit mode sets title "Chỉnh sửa món"');
assert(!mockDOM['btn-item-form-delete'].classList.contains('hidden'), 'Delete button visible in edit mode');
assert(mockDOM['item-form-name'].value === 'Trà ổi hồng muối ớt', 'Form name pre-filled with item name');
assert(mockDOM['item-form-acronym'].value === 'tohmo', 'Form acronym pre-filled');

// Edit item name and price
mockDOM['item-form-name'].value = 'Trà ổi hồng đặc biệt';
sandbox.handleSizePriceChange(sandbox.currentItemSizes.length - 1, 60000);
sandbox.saveMenuItemForm();

const catalogAfterEdit = bus.getCatalogData();
const editedItem = catalogAfterEdit.items.find(i => i.id === createdItem.id);
assert(editedItem.name === 'Trà ổi hồng đặc biệt', 'Item name updated to "Trà ổi hồng đặc biệt"');
assert(editedItem.basePrice === 60000, 'Item basePrice updated to 60.000 đ');

// 4.12 Test Item Deletion via deleteMenuItemConfirmed
sandbox.deleteMenuItemConfirmed(createdItem.id);
const catalogAfterDelete = bus.getCatalogData();
assert(!catalogAfterDelete.items.some(i => i.id === createdItem.id), 'Item deleted cleanly from POS_BUS catalog');

// 4.13 Test jumpToBatchLinker
sandbox.jumpToBatchLinker('cfsd');
assert(sandbox.catalogPreFocusedItemId === 'cfsd', 'jumpToBatchLinker sets catalogPreFocusedItemId to "cfsd"');

// 4.14 Test Single Price Mode
sandbox.openMenuItemModal(null);
sandbox.toggleItemMultiSize(false);
assert(sandbox.isItemMultiSize === false, 'toggleItemMultiSize(false) sets isItemMultiSize to false');
mockDOM['item-form-name'].value = 'Bánh sừng bò Pháp';
mockDOM['item-form-category'].value = 'bakery';
mockDOM['item-form-base-price'].value = 28000;
sandbox.saveMenuItemForm();

const bakeryItem = bus.getCatalogData().items.find(i => i.name === 'Bánh sừng bò Pháp');
assert(!!bakeryItem, 'Single-price item "Bánh sừng bò Pháp" saved to catalog');
assert(bakeryItem.basePrice === 28000, 'Single-price item basePrice is 28.000 đ');
assert(bakeryItem.sizes.length === 0, 'Single-price item has empty sizes array');

// Clean up test bakery item
bus.deleteMenuItem(bakeryItem.id);

console.log('\n================================================================');
console.log(`  VERIFICATION RESULTS: ${passedTests}/${totalTests} TESTS PASSED`);
if (failedTests === 0) {
  console.log('  STATUS: 100% SUCCESS - TASK 3 SPECIFICATION & CONTRACT VERIFIED');
} else {
  console.error(`  STATUS: FAILED - ${failedTests} test(s) failed`);
  process.exit(1);
}
