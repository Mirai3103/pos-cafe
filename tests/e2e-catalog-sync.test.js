/**
 * POS Cafe - Task 6 Verification Test: Cashier Terminal Sync Integration & End-to-End Verification
 * 
 * Verifies:
 * 1. Design & System Compliance (Crisp Emerald, Outfit/JetBrains Mono, zero em-dashes, zero emojis, >=48px touch targets)
 * 2. Cashier Terminal (index.html) dynamic catalog hydration from window.POS_BUS.getCatalogData()
 * 3. Settings Terminal (settings.html) keyboard shortcuts (Ctrl+N, Ctrl+S, Esc) and event broadcasts
 * 4. Cross-tab real-time catalog synchronization:
 *    - Creation of new category in settings propagates to index.html pills bar
 *    - Creation of new modifier group in settings propagates to bus
 *    - Creation of new beverage in settings propagates to index.html product grid
 *    - Batch linker assignment updates item assignedModifierGroupIds across tabs
 *    - openModifierModal(item) in index.html dynamically reads effective groups,
 *      fetches modifier groups and options from POS_BUS, and renders dynamic toppings
 *    - Price calculation with dynamic toppings and commit to cart
 *    - Excluded modifier groups and single-select modifier groups
 */

const fs = require('fs');
const path = require('path');
const vm = require('vm');

const ROOT_DIR = path.resolve(__dirname, '..');
const POS_DIR = path.join(ROOT_DIR, 'design-system', 'pos-cafe');
const INDEX_PATH = path.join(POS_DIR, 'index.html');
const SETTINGS_PATH = path.join(POS_DIR, 'pages', 'settings.html');
const POS_BUS_PATH = path.join(POS_DIR, 'shared', 'pos-bus.js');

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
console.log('  TASK 6: CASHIER TERMINAL SYNC INTEGRATION & E2E VERIFICATION');
console.log('================================================================\n');

// 1. Static HTML & Code Compliance Verification
console.log('--- 1. Checking Static HTML & Design Compliance ---');
assert(fs.existsSync(INDEX_PATH), 'index.html exists');
assert(fs.existsSync(SETTINGS_PATH), 'settings.html exists');
assert(fs.existsSync(POS_BUS_PATH), 'pos-bus.js exists');

const indexHtml = fs.readFileSync(INDEX_PATH, 'utf8');
const settingsHtml = fs.readFileSync(SETTINGS_PATH, 'utf8');

const emojiRegex = /[\u{1F300}-\u{1F9FF}\u{2600}-\u{26FF}\u{2700}-\u{27BF}]/u;

// Em-dash check
assert(!indexHtml.includes('\u2014') && !indexHtml.includes('\u2013'), 'index.html has zero em-dashes');
assert(!settingsHtml.includes('\u2014') && !settingsHtml.includes('\u2013'), 'settings.html has zero em-dashes');

// Emoji check
assert(!emojiRegex.test(indexHtml), 'index.html has zero Unicode emojis');
assert(!emojiRegex.test(settingsHtml), 'settings.html has zero Unicode emojis');

// AI-purple gradient check
assert(!/bg-gradient-to-[a-z]+\s+from-(?:purple|violet|fuchsia)/i.test(indexHtml), 'index.html has no AI-purple gradients');
assert(!/bg-gradient-to-[a-z]+\s+from-(?:purple|violet|fuchsia)/i.test(settingsHtml), 'settings.html has no AI-purple gradients');

// Typography check
assert(indexHtml.includes('Outfit') && indexHtml.includes('JetBrains+Mono'), 'index.html imports Outfit and JetBrains Mono');
assert(settingsHtml.includes('Outfit') && settingsHtml.includes('JetBrains+Mono'), 'settings.html imports Outfit and JetBrains Mono');

// Currency format check (whole VND, no decimals)
const decimalMatches = (indexHtml + settingsHtml).match(/[0-9]+[.,][0-9]{1,2}\s*(?:đ|vnd|\$)/gi) || [];
assert(decimalMatches.length === 0, 'index.html and settings.html strictly use whole VND without decimals');

// Check required method definitions in index.html
assert(indexHtml.includes('hydrateCatalogFromBus'), 'index.html defines hydrateCatalogFromBus');
assert(indexHtml.includes('renderProductGrid'), 'index.html defines renderProductGrid');
assert(indexHtml.includes('getItemEffectiveModifierGroups'), 'index.html defines getItemEffectiveModifierGroups');
assert(indexHtml.includes('CATALOG_ITEMS_UPDATED'), 'index.html subscribes to CATALOG_ITEMS_UPDATED');

// 2. Setup Multi-Tab Simulation Environment
console.log('\n--- 2. Setting Up Multi-Tab Environment (localStorage & BroadcastChannel) ---');

const mockStorage = {};
const channelSubscribers = {};

class MockBroadcastChannel {
  constructor(name) {
    this.name = name;
    if (!channelSubscribers[name]) {
      channelSubscribers[name] = [];
    }
    channelSubscribers[name].push(this);
    this.onmessage = null;
  }

  postMessage(data) {
    const subs = channelSubscribers[this.name] || [];
    subs.forEach(s => {
      if (s !== this && typeof s.onmessage === 'function') {
        setTimeout(() => {
          s.onmessage({ data: JSON.parse(JSON.stringify(data)) });
        }, 0);
      }
    });
  }

  close() {
    const subs = channelSubscribers[this.name] || [];
    channelSubscribers[this.name] = subs.filter(s => s !== this);
  }
}

global.localStorage = {
  getItem: (k) => mockStorage[k] || null,
  setItem: (k, v) => { mockStorage[k] = String(v); },
  removeItem: (k) => { delete mockStorage[k]; },
  clear: () => { Object.keys(mockStorage).forEach(k => delete mockStorage[k]); }
};
global.BroadcastChannel = MockBroadcastChannel;
global.window = global;

// Load pos-bus.js
const busCode = fs.readFileSync(POS_BUS_PATH, 'utf8');
vm.runInThisContext(busCode);
const bus = global.window.POS_BUS;
assert(!!bus && typeof bus.getCatalogData === 'function', 'POS_BUS loaded with getCatalogData()');

// Verify initial catalog seeding
const seedCatalog = bus.getCatalogData();
assert(Array.isArray(seedCatalog.categories) && seedCatalog.categories.length >= 5, 'Seed catalog contains at least 5 categories');
assert(Array.isArray(seedCatalog.items) && seedCatalog.items.length >= 10, 'Seed catalog contains menu items');
assert(Array.isArray(seedCatalog.modifierGroups) && seedCatalog.modifierGroups.length >= 3, 'Seed catalog contains modifier groups');

// Helper to create mock DOM elements
function createMockElement(id, classList = []) {
  const classes = new Set(classList);
  return {
    id,
    classList: {
      add: (c) => classes.add(c),
      remove: (c) => classes.delete(c),
      contains: (c) => classes.has(c),
      toggle: (c) => { classes.has(c) ? classes.delete(c) : classes.add(c); }
    },
    get className() { return Array.from(classes).join(' '); },
    set className(val) { classes.clear(); (val || '').split(/\s+/).filter(Boolean).forEach(c => classes.add(c)); },
    innerHTML: '',
    textContent: '',
    value: '',
    style: {},
    attributes: {},
    setAttribute: function(k, v) { this.attributes[k] = String(v); },
    getAttribute: function(k) { return this.attributes[k] || null; },
    focus: () => {},
    blur: () => {},
    querySelector: function(sel) { return null; },
    querySelectorAll: function(sel) { return []; },
    closest: function(sel) { return null; }
  };
}

// 3. Test Cashier Terminal (index.html) Sandbox & Hydration
console.log('\n--- 3. Testing Cashier Terminal (index.html) Catalog Hydration ---');

const rawIndexDOM = {
  'category-pills-bar': createMockElement('category-pills-bar'),
  'catalog-grid': createMockElement('catalog-grid'),
  'catalog-total-items': createMockElement('catalog-total-items'),
  'modifier-modal-backdrop': createMockElement('modifier-modal-backdrop', ['hidden']),
  'modifier-modal-dialog': createMockElement('modifier-modal-dialog'),
  'modifier-modal-title': createMockElement('modifier-modal-title'),
  'modifier-modal-category': createMockElement('modifier-modal-category'),
  'modifier-modal-subtitle': createMockElement('modifier-modal-subtitle'),
  'modifier-modal-img': createMockElement('modifier-modal-img'),
  'modifier-modal-icon-wrap': createMockElement('modifier-modal-icon-wrap'),
  'modifier-modal-icon': createMockElement('modifier-modal-icon'),
  'modifier-modal-body': createMockElement('modifier-modal-body'),
  'modifier-modal-total-price': createMockElement('modifier-modal-total-price'),
  'modifier-btn-price': createMockElement('modifier-btn-price'),
  'modifier-qty-display': createMockElement('modifier-qty-display'),
  'modifier-note-input': createMockElement('modifier-note-input'),
  'bill-items-container': createMockElement('bill-items-container')
};

const indexDOM = new Proxy(rawIndexDOM, {
  get: (target, prop) => {
    if (typeof prop !== 'string') return undefined;
    if (!target[prop]) {
      target[prop] = createMockElement(prop);
    }
    return target[prop];
  }
});

// Extract index.html script
const indexScriptBlocks = indexHtml.split(/<script[^>]*>/i);
const indexMainScript = indexScriptBlocks.find(b => b.includes('function renderCategoryPills') && b.includes('function openModifierModal'));
assert(!!indexMainScript, 'Found main script in index.html');
const indexScriptCode = indexMainScript.split(/<\/script>/i)[0];

const indexSandbox = {
  window: {
    POS_BUS: bus,
    lucide: { createIcons: () => {} },
    location: { href: '' }
  },
  document: {
    getElementById: (id) => indexDOM[id] || null,
    addEventListener: () => {},
    activeElement: null
  },
  console: console,
  setInterval: () => 1,
  clearInterval: () => {},
  setTimeout: (fn) => { fn(); return 1; },
  clearTimeout: () => {},
  Date: Date,
  Math: Math,
  parseInt: parseInt,
  Number: Number,
  String: String,
  Array: Array,
  JSON: JSON,
  formatVND: (n) => String(n).replace(/\B(?=(\d{3})+(?!\d))/g, '.') + ' đ',
  playTapChirp: () => {},
  playSuccessChirp: () => {},
  showToast: (msg) => {},
  initDigitalClock: () => {},
  initKeyboardShortcuts: () => {},
  updateTopBarUI: () => {},
  updateBillUI: () => {},
  renderQuickPayment: () => {},
  renderFloorPlan: () => {}
};
indexSandbox.window.window = indexSandbox.window;

vm.createContext(indexSandbox);

try {
  vm.runInContext(indexScriptCode, indexSandbox);
  Object.defineProperty(indexSandbox, 'modifierState', {
    get: () => indexSandbox.window.modifierState,
    set: (val) => { indexSandbox.window.modifierState = val; },
    configurable: true
  });
  assert(true, 'index.html script evaluated without errors');
} catch (err) {
  assert(false, 'index.html script compilation failed', err.stack);
}

// Check exported functions in index.html
assert(typeof indexSandbox.hydrateCatalogFromBus === 'function', 'hydrateCatalogFromBus is defined in index.html');
assert(typeof indexSandbox.renderCategoryPills === 'function', 'renderCategoryPills is defined in index.html');
assert(typeof indexSandbox.renderProductGrid === 'function', 'renderProductGrid is defined in index.html');
assert(typeof indexSandbox.openModifierModal === 'function', 'openModifierModal is defined in index.html');
assert(typeof indexSandbox.getItemEffectiveModifierGroups === 'function', 'getItemEffectiveModifierGroups is defined in index.html');

// Test initial catalog hydration
indexSandbox.hydrateCatalogFromBus();
const state = indexSandbox.window.POS_STATE;
assert(!!state, 'window.POS_STATE is instantiated');
assert(Array.isArray(state.categories) && state.categories.length >= 5, 'state.categories populated from bus');
assert(Array.isArray(state.menuItems) && state.menuItems.length >= 10, 'state.menuItems populated from bus');

// Test rendering category pills
indexSandbox.renderCategoryPills(state);
const pillsHtml = indexDOM['category-pills-bar'].innerHTML;
assert(pillsHtml.includes('Tất cả'), 'renderCategoryPills includes "Tất cả" category pill');
assert(pillsHtml.includes('Cà phê Việt'), 'renderCategoryPills includes "Cà phê Việt" category pill');
assert(pillsHtml.includes('Trà & Macchiato'), 'renderCategoryPills includes "Trà & Macchiato" category pill');
assert(pillsHtml.includes('min-h-[48px]'), 'Category pills satisfy min-h-[48px] touch target');

// Test rendering product grid
indexSandbox.renderProductGrid(state);
const gridHtml = indexDOM['catalog-grid'].innerHTML;
assert(gridHtml.includes('Cà phê sữa đá'), 'renderProductGrid renders "Cà phê sữa đá"');
assert(gridHtml.includes('35.000 đ'), 'renderProductGrid renders price formatted in whole VND');

// 4. Test Settings Terminal (settings.html) Keyboard Shortcuts
console.log('\n--- 4. Testing Settings Terminal (settings.html) Shortcuts & Actions ---');

const rawSettingsDOM = {
  'modal-item-form': createMockElement('modal-item-form', ['hidden']),
  'modal-category-form': createMockElement('modal-category-form', ['hidden']),
  'modal-modifier-group-form': createMockElement('modal-modifier-group-form', ['hidden']),
  'modal-backdrop-overlay': createMockElement('modal-backdrop-overlay', ['hidden']),
  'station-switcher-dropdown': createMockElement('station-switcher-dropdown', ['hidden']),
  'tab-catalog-counter': createMockElement('tab-catalog-counter'),
  'pill-badge-items': createMockElement('pill-badge-items'),
  'pill-badge-categories': createMockElement('pill-badge-categories'),
  'pill-badge-modifiers': createMockElement('pill-badge-modifiers'),
  'catalog-categories-count-display': createMockElement('catalog-categories-count-display'),
  'catalog-modifiers-count-display': createMockElement('catalog-modifiers-count-display')
};

const settingsDOM = new Proxy(rawSettingsDOM, {
  get: (target, prop) => {
    if (typeof prop !== 'string') return undefined;
    if (!target[prop]) {
      target[prop] = createMockElement(prop);
    }
    return target[prop];
  }
});

const settingsScriptBlocks = settingsHtml.split(/<script[^>]*>/i);
const settingsMainScript = settingsScriptBlocks.find(b => b.includes('function refreshCatalogCounters') && b.includes('function saveMenuItemForm'));
assert(!!settingsMainScript, 'Found main script in settings.html');
const settingsScriptCode = settingsMainScript.split(/<\/script>/i)[0];

let keydownHandler = null;
let savedFormType = null;
let itemModalOpened = false;

const settingsSandbox = {
  window: {
    POS_BUS: bus,
    lucide: { createIcons: () => {} },
    location: { href: '' }
  },
  document: {
    getElementById: (id) => settingsDOM[id] || null,
    addEventListener: (evt, handler) => {
      if (evt === 'keydown') keydownHandler = handler;
    },
    activeElement: null
  },
  console: console,
  setInterval: () => 1,
  clearInterval: () => {},
  setTimeout: (fn) => { fn(); return 1; },
  clearTimeout: () => {},
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
  playSaveChime: () => {},
  showToast: (msg) => {},
  openMenuItemModal: () => { itemModalOpened = true; settingsDOM['modal-item-form'].classList.remove('hidden'); },
  saveMenuItemForm: () => { savedFormType = 'item'; },
  saveCategoryForm: () => { savedFormType = 'category'; },
  saveModifierGroupForm: () => { savedFormType = 'modifier'; },
  saveBatchLinkerAssignments: () => { savedFormType = 'linker'; },
  saveStoreSettingsForm: () => { savedFormType = 'store'; }
};
settingsSandbox.window.window = settingsSandbox.window;

vm.createContext(settingsSandbox);

try {
  vm.runInContext(settingsScriptCode, settingsSandbox);
  assert(true, 'settings.html script evaluated without errors');
} catch (err) {
  assert(false, 'settings.html script compilation failed', err.stack);
}

assert(typeof keydownHandler === 'function', 'Keydown event handler registered in settings.html');

// Wrap save handlers to observe shortcut invocation
settingsSandbox.saveMenuItemForm = () => { savedFormType = 'item'; };
settingsSandbox.saveCategoryForm = () => { savedFormType = 'category'; };
settingsSandbox.saveModifierGroupForm = () => { savedFormType = 'modifier'; };
settingsSandbox.saveBatchLinkerAssignments = () => { savedFormType = 'linker'; };

// Test Shortcut 1: Ctrl + N opens new menu item modal
settingsSandbox.currentTab = 'catalog';
settingsDOM['modal-item-form'].classList.add('hidden');
keydownHandler({ ctrlKey: true, key: 'n', preventDefault: () => {} });
assert(!settingsDOM['modal-item-form'].classList.contains('hidden'), 'Ctrl+N opens MenuItemModal when on Catalog tab');

// Test Shortcut 2: Ctrl + S saves currently active modal
savedFormType = null;
settingsDOM['modal-item-form'].classList.remove('hidden');
keydownHandler({ ctrlKey: true, key: 's', preventDefault: () => {} });
assert(savedFormType === 'item', 'Ctrl+S triggers saveMenuItemForm when item modal is open');

settingsDOM['modal-item-form'].classList.add('hidden');
settingsDOM['modal-category-form'].classList.remove('hidden');
keydownHandler({ ctrlKey: true, key: 's', preventDefault: () => {} });
assert(savedFormType === 'category', 'Ctrl+S triggers saveCategoryForm when category modal is open');

settingsDOM['modal-category-form'].classList.add('hidden');
settingsDOM['modal-modifier-group-form'].classList.remove('hidden');
keydownHandler({ ctrlKey: true, key: 's', preventDefault: () => {} });
assert(savedFormType === 'modifier', 'Ctrl+S triggers saveModifierGroupForm when modifier modal is open');

settingsDOM['modal-modifier-group-form'].classList.add('hidden');
settingsSandbox.currentCatalogSubView = 'linker';
keydownHandler({ ctrlKey: true, key: 's', preventDefault: () => {} });
assert(savedFormType === 'linker', 'Ctrl+S triggers saveBatchLinkerAssignments when on linker subview');

// Test Shortcut 3: Esc dismisses open modal
settingsDOM['modal-item-form'].classList.remove('hidden');
assert(!settingsDOM['modal-item-form'].classList.contains('hidden'), 'Item modal is open before Esc');
keydownHandler({ key: 'Escape', preventDefault: () => {} });
assert(settingsDOM['modal-item-form'].classList.contains('hidden'), 'Esc key successfully closes active modal');

// 5. Cross-Screen End-to-End Workflow Verification
console.log('\n--- 5. End-to-End Dynamic Sync Workflows ---');

// Step A: Create New Category in settings and sync to cashier
console.log('\n[Step A] Creating New Category in settings...');
const newCategory = {
  id: 'cat_specialty',
  name: 'Cà phê Đặc sản',
  icon: 'coffee',
  order: 6,
  defaultGroupIds: ['sugar', 'ice']
};
bus.saveCategory(newCategory);

// Cashier re-hydrates
indexSandbox.hydrateCatalogFromBus();
assert(state.categories.some(c => c.id === 'cat_specialty'), 'Cashier state.categories updated with new category');

indexSandbox.renderCategoryPills(state);
const updatedPillsHtml = indexDOM['category-pills-bar'].innerHTML;
assert(updatedPillsHtml.includes('Cà phê Đặc sản'), 'Cashier category bar dynamically renders "Cà phê Đặc sản" pill');

// Step B: Create New Modifier Group in settings
console.log('\n[Step B] Creating New Modifier Group in settings...');
const newModifierGroup = {
  id: 'mod_premium',
  name: 'Topping Thượng Hạng',
  selectionType: 'multiple',
  minSelect: 0,
  maxSelect: 5,
  options: [
    { id: 'opt_gold_pearl', name: 'Trân châu hoàng kim', price: 15000, isAvailable: true },
    { id: 'opt_herbal_jelly', name: 'Sương sáo mật ong', price: 12000, isAvailable: true }
  ]
};
bus.saveModifierGroup(newModifierGroup);

const catalogWithMod = bus.getCatalogData();
assert(catalogWithMod.modifierGroups.some(g => g.id === 'mod_premium'), 'Modifier group saved to POS_BUS');

// Step C: Create New Beverage in settings
console.log('\n[Step C] Creating New Beverage in settings...');
const newBeverage = {
  id: 'item_coldbrew_orange',
  name: 'Cold Brew Cam Vàng',
  categoryId: 'cat_specialty',
  categoryName: 'Cà phê Đặc sản',
  basePrice: 45000,
  hasModifiers: true,
  sizes: [
    { id: 'S', name: 'Size S', price: 39000 },
    { id: 'M', name: 'Size M', price: 45000, isDefault: true },
    { id: 'L', name: 'Size L', price: 52000 }
  ],
  defaultSize: 'M',
  acronym: 'cbcv',
  badge: 'Mới',
  description: 'Cà phê ủ lạnh 18 tiếng hòa quyện cùng nước cam tươi sảng khoái',
  assignedModifierGroupIds: [],
  excludedModifierGroupIds: [],
  isAvailable: true
};
bus.saveMenuItem(newBeverage);

// Cashier syncs item
indexSandbox.hydrateCatalogFromBus();
assert(state.menuItems.some(i => i.id === 'item_coldbrew_orange'), 'Cashier state.menuItems updated with new beverage');

indexSandbox.renderProductGrid(state);
const updatedGridHtml = indexDOM['catalog-grid'].innerHTML;
assert(updatedGridHtml.includes('Cold Brew Cam Vàng'), 'Cashier product grid dynamically displays "Cold Brew Cam Vàng"');
assert(updatedGridHtml.includes('45.000 đ'), 'Product grid displays default size price formatted in VND');

// Step D: Batch Linker Assignment in settings
console.log('\n[Step D] Batch Assigning Modifier Group to New Beverage...');
bus.batchAssignModifierGroup('mod_premium', ['item_coldbrew_orange']);

indexSandbox.hydrateCatalogFromBus();
const syncedBeverage = state.menuItems.find(i => i.id === 'item_coldbrew_orange');
assert(syncedBeverage.assignedModifierGroupIds.includes('mod_premium'), 'Beverage assignedModifierGroupIds updated with "mod_premium"');

// Step E: Open Modifier Modal in Cashier Terminal & Select Dynamic Toppings
console.log('\n[Step E] Opening Modifier Modal with Dynamic Toppings in Cashier Terminal...');
indexSandbox.openModifierModal(syncedBeverage);

// Verify effective groups resolution
const effectiveGroups = indexSandbox.getItemEffectiveModifierGroups(syncedBeverage);
assert(effectiveGroups.some(g => g.id === 'mod_premium'), 'Effective groups contains assigned "mod_premium"');
assert(effectiveGroups.some(g => g.id === 'sugar'), 'Effective groups contains category default "sugar"');
assert(effectiveGroups.some(g => g.id === 'ice'), 'Effective groups contains category default "ice"');

// Verify dynamic topping groups & options
const modalBodyHtml = indexDOM['modifier-modal-body'].innerHTML;
assert(modalBodyHtml.includes('Topping Thượng Hạng'), 'Modifier modal renders dynamic group name "Topping Thượng Hạng"');
assert(modalBodyHtml.includes('Trân châu hoàng kim'), 'Modifier modal renders dynamic option "Trân châu hoàng kim"');
assert(modalBodyHtml.includes('Sương sáo mật ong'), 'Modifier modal renders dynamic option "Sương sáo mật ong"');
assert(modalBodyHtml.includes('+15.000 đ'), 'Option surcharge displayed in whole VND (+15.000 đ)');
assert(modalBodyHtml.includes('+12.000 đ'), 'Option surcharge displayed in whole VND (+12.000 đ)');
assert(modalBodyHtml.includes('min-h-[48px]'), 'Modifier chips enforce min-h-[48px] touch target');

// Verify smart defaults
assert(indexSandbox.modifierState.sizeId === 'M', 'Smart default size is Size M');
assert(indexSandbox.modifierState.sugar === '100%', 'Smart default sugar is 100%');
assert(indexSandbox.modifierState.ice === '100% đá', 'Smart default ice is 100% đá');

// Verify initial price calculation: 45.000 đ for Size M
let totals = indexSandbox.calculateCurrentModifierTotal();
assert(totals.basePrice === 45000, 'Base price calculated as 45.000 đ');
assert(totals.toppingsTotal === 0, 'Initial toppings surcharge is 0 đ');
assert(totals.lineTotal === 45000, 'Initial line total is 45.000 đ');

// Simulate cashier selecting "Trân châu hoàng kim" (+15.000 đ)
indexSandbox.toggleModifierTopping('opt_gold_pearl', 'mod_premium', 'multiple');
assert(indexSandbox.modifierState.toppings.includes('opt_gold_pearl'), 'Topping "opt_gold_pearl" added to selection');

totals = indexSandbox.calculateCurrentModifierTotal();
assert(totals.toppingsTotal === 15000, 'Toppings total updated to 15.000 đ');
assert(totals.unitTotal === 60000, 'Unit total recalculated to 60.000 đ (45k + 15k)');
assert(totals.lineTotal === 60000, 'Line total recalculated to 60.000 đ');

// Simulate increasing quantity to 2
indexSandbox.updateModifierQty(1);
totals = indexSandbox.calculateCurrentModifierTotal();
assert(totals.quantity === 2, 'Quantity updated to 2');
assert(totals.lineTotal === 120000, 'Line total updated to 120.000 đ (60k x 2)');

// Step F: Confirm modifier selection and commit to cart
console.log('\n[Step F] Committing Custom Beverage to Cart...');
indexSandbox.confirmModifierSelection();

assert(Array.isArray(state.cart) && state.cart.length > 0, 'Item successfully committed to state.cart');
const cartItem = state.cart.find(ci => ci.menuItemId === 'item_coldbrew_orange' || ci.id === 'item_coldbrew_orange');
assert(!!cartItem, 'Cart contains "item_coldbrew_orange"');
assert(cartItem.quantity === 2, 'Cart item quantity is 2');
assert(cartItem.unitPrice === 60000, 'Cart item unit price is 60.000 đ');
assert((cartItem.lineTotalVND || cartItem.lineTotal) === 120000, 'Cart item line total is 120.000 đ');
assert(cartItem.modifiers.some(m => m.name === 'Trân châu hoàng kim' && m.price === 15000), 'Cart item preserves structured modifier with price');

// Step G: Test Excluded Modifier Groups
console.log('\n[Step G] Testing Excluded Modifier Groups...');
const beverageNoSugar = {
  id: 'item_pure_tea',
  name: 'Trà Mộc Nguyên Bản',
  categoryId: 'cat_specialty',
  basePrice: 30000,
  hasModifiers: true,
  assignedModifierGroupIds: [],
  excludedModifierGroupIds: ['sugar'], // sugar excluded
  isAvailable: true
};
bus.saveMenuItem(beverageNoSugar);
indexSandbox.hydrateCatalogFromBus();
indexSandbox.openModifierModal(beverageNoSugar);

const noSugarModalHtml = indexDOM['modifier-modal-body'].innerHTML;
assert(!noSugarModalHtml.includes('Mức ngọt (Đường)'), 'Sugar section omitted when "sugar" is in excludedModifierGroupIds');
assert(noSugarModalHtml.includes('Mức đá'), 'Ice section still rendered when ice is not excluded');

// Step H: Test Single-Select Modifier Group Behavior
console.log('\n[Step H] Testing Single-Select Modifier Group Behavior...');
const singleSelectGroup = {
  id: 'mod_cheese_foam',
  name: 'Lớp Váng Sữa',
  selectionType: 'single',
  options: [
    { id: 'opt_foam_salted', name: 'Kem Muối', price: 12000 },
    { id: 'opt_foam_cheese', name: 'Kem Phô Mai', price: 14000 }
  ]
};
bus.saveModifierGroup(singleSelectGroup);
bus.batchAssignModifierGroup('mod_cheese_foam', ['item_coldbrew_orange']);

indexSandbox.hydrateCatalogFromBus();
const orangeWithCheese = state.menuItems.find(i => i.id === 'item_coldbrew_orange');
indexSandbox.openModifierModal(orangeWithCheese);

// Select first single-select option
indexSandbox.toggleModifierTopping('opt_foam_salted', 'mod_cheese_foam', 'single');
assert(indexSandbox.modifierState.toppings.includes('opt_foam_salted'), 'Single option "opt_foam_salted" selected');

// Select second single-select option (should replace first option)
indexSandbox.toggleModifierTopping('opt_foam_cheese', 'mod_cheese_foam', 'single');
assert(!indexSandbox.modifierState.toppings.includes('opt_foam_salted'), 'Previous option "opt_foam_salted" deselected');
assert(indexSandbox.modifierState.toppings.includes('opt_foam_cheese'), 'New option "opt_foam_cheese" selected');

// 6. Summary and Exit Code
console.log('\n================================================================');
console.log(`  E2E CATALOG SYNC VERIFICATION RESULTS: ${passedTests}/${totalTests} TESTS PASSED`);
if (failedTests > 0) {
  console.error(`  STATUS: ${failedTests} FAILED TESTS`);
  process.exit(1);
} else {
  console.log('  STATUS: 100% SUCCESS - ALL CONTRACTS VERIFIED');
  process.exit(0);
}
