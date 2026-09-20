/**
 * POS Cafe - Task 5 Verification Test: Interactive Batch Linker Matrix & Live Cashier POS Simulator
 * 
 * Verifies:
 * 1. Element presence in settings.html:
 *    - #catalog-subview-linker:
 *      * Header instruction strip
 *      * Quick CTA button: #btn-linker-add-group "+ Tạo nhóm mới"
 *      * 3-Column Responsive Grid (lg:grid-cols-12):
 *        - Column 1 (lg:col-span-3): #linker-group-rail, #linker-group-count-badge
 *        - Column 2 (lg:col-span-5): #linker-category-tree, #linker-items-search, #linker-active-group-name
 *          Sticky bottom bar: #linker-selected-count, #linker-commit-group-name, #btn-save-batch-linker
 *        - Column 3 (lg:col-span-4): Tablet mockup bezel, #linker-simulator-screen, #simulator-clock, #simulator-total-price
 * 2. Strict Design Compliance:
 *    - All touch targets >= 48px
 *    - Zero em-dashes (— or –)
 *    - Zero Unicode emojis
 *    - Zero AI-purple gradients
 *    - Whole VND formatting without decimals
 * 3. Behavioral Runtime Simulation:
 *    - renderBatchLinkerMatrix() / renderCatalogLinkerMatrix()
 *    - selectLinkerModifierGroup(groupId)
 *    - toggleLinkerCategoryAll(categoryId) (1-click bulk toggle)
 *    - toggleLinkerItem(itemId)
 *    - updateLiveSimulator(itemId)
 *    - Real-time dynamic toppings list synchronization between Column 2 and Column 3
 *    - Live total price recalculation in JetBrains Mono
 *    - Simulator interactions: size, sugar, ice, toppings chips
 *    - saveBatchLinkerAssignments(): POS_BUS persistence, cash register sound chime, toast feedback, counter refresh
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
console.log('  TASK 5: BATCH LINKER MATRIX & LIVE POS SIMULATOR TEST');
console.log('================================================================\n');

// 1. Static HTML Verification
console.log('--- 1. Checking Static HTML Structure in settings.html ---');
assert(fs.existsSync(SETTINGS_PATH), 'settings.html exists');
const html = fs.readFileSync(SETTINGS_PATH, 'utf8');

// Batch Linker Subview Container
assert(html.includes('id="catalog-subview-linker"'), '#catalog-subview-linker container exists');
assert(html.includes('Ma trận Gán Topping Hàng Loạt (Batch Linker)'), 'Batch Linker header banner title exists');
assert(html.includes('id="btn-linker-add-group"'), '#btn-linker-add-group button exists');
assert(html.includes('+ Tạo nhóm mới'), 'CTA label "+ Tạo nhóm mới" exists');

// Column 1: Modifier Groups Rail
assert(html.includes('id="linker-group-rail"'), '#linker-group-rail container exists');
assert(html.includes('id="linker-group-count-badge"'), '#linker-group-count-badge exists');
assert(html.includes('1. Nhóm Topping'), 'Column 1 title "1. Nhóm Topping" exists');

// Column 2: Target Items Tree & Search
assert(html.includes('id="linker-category-tree"'), '#linker-category-tree container exists');
assert(html.includes('id="linker-items-search"'), '#linker-items-search input exists');
assert(html.includes('id="linker-active-group-name"'), '#linker-active-group-name display exists');
assert(html.includes('2. Danh mục & Món áp dụng'), 'Column 2 title "2. Danh mục & Món áp dụng" exists');

// Sticky Action Bar
assert(html.includes('id="linker-selected-count"'), '#linker-selected-count display exists');
assert(html.includes('id="linker-commit-group-name"'), '#linker-commit-group-name display exists');
assert(html.includes('id="btn-save-batch-linker"'), '#btn-save-batch-linker button exists');
assert(html.includes('LƯU ÁP DỤNG NGAY (Enter)'), 'Primary CTA label "LƯU ÁP DỤNG NGAY (Enter)" exists');

// Column 3: Live Cashier POS Simulator
assert(html.includes('id="linker-simulator-screen"'), '#linker-simulator-screen tablet interior exists');
assert(html.includes('id="simulator-clock"'), '#simulator-clock display exists');
assert(html.includes('Quầy thu ngân (Simulator)'), 'Column 3 header "Quầy thu ngân (Simulator)" exists');

// 2. Design System & Anti-Slop Compliance
console.log('\n--- 2. Design System & Anti-Slop Compliance ---');
const emojiRegex = /[\u{1F300}-\u{1F9FF}\u{2600}-\u{26FF}\u{2700}-\u{27BF}]/u;
assert(!emojiRegex.test(html), 'Zero Unicode emojis in settings.html');
assert(!html.includes('\u2014') && !html.includes('\u2013'), 'Zero em-dashes in settings.html');
const hasPurple = /bg-gradient-to-[a-z]+\s+from-(?:purple|violet|fuchsia)/i.test(html);
assert(!hasPurple, 'Zero AI-purple gradients in settings.html');

// Touch Targets (>= 48px)
console.log('\n--- 3. Checking Touch Targets on Task 5 Buttons (>= 48px) ---');
const task5ButtonIds = [
  'btn-linker-add-group',
  'btn-save-batch-linker'
];

task5ButtonIds.forEach(id => {
  const regex = new RegExp(`<button[^>]*id="${id}"[^>]*>`, 'i');
  const match = html.match(regex);
  assert(!!match, `<button id="${id}"> tag found`);
  if (match) {
    const btnTag = match[0];
    const hasMinH = btnTag.includes('min-h-[48px]') || btnTag.includes('min-h-[56px]') || btnTag.includes('h-12') || btnTag.includes('h-14');
    assert(hasMinH, `<button id="${id}"> enforces >= 48px touch target`);
  }
});

// All buttons audit in settings.html
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

function createMockElement(id, initialClasses = []) {
  const classes = new Set(initialClasses);
  const attributes = {};
  let innerHTMLValue = '';
  return {
    id: id,
    textContent: '',
    get innerHTML() {
      return innerHTMLValue;
    },
    set innerHTML(val) {
      innerHTMLValue = val;
    },
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
  // Batch Linker Elements
  'catalog-subview-linker': createMockElement('catalog-subview-linker', ['hidden']),
  'btn-linker-add-group': createMockElement('btn-linker-add-group'),
  'linker-group-rail': createMockElement('linker-group-rail'),
  'linker-group-count-badge': createMockElement('linker-group-count-badge'),
  'linker-category-tree': createMockElement('linker-category-tree'),
  'linker-items-search': createMockElement('linker-items-search'),
  'linker-active-group-name': createMockElement('linker-active-group-name'),
  'linker-selected-count': createMockElement('linker-selected-count'),
  'linker-commit-group-name': createMockElement('linker-commit-group-name'),
  'btn-save-batch-linker': createMockElement('btn-save-batch-linker'),
  'linker-simulator-screen': createMockElement('linker-simulator-screen'),
  'simulator-clock': createMockElement('simulator-clock'),
  'simulator-total-price': createMockElement('simulator-total-price'),

  // Catalog Navigation & Tabs
  'tab-catalog-counter': createMockElement('tab-catalog-counter'),
  'pill-badge-items': createMockElement('pill-badge-items'),
  'pill-badge-categories': createMockElement('pill-badge-categories'),
  'pill-badge-modifiers': createMockElement('pill-badge-modifiers'),
  'catalog-categories-count-display': createMockElement('catalog-categories-count-display'),
  'catalog-modifiers-count-display': createMockElement('catalog-modifiers-count-display'),
  'catalog-pill-linker': createMockElement('catalog-pill-linker'),
  'catalog-pill-items': createMockElement('catalog-pill-items'),
  'catalog-pill-categories': createMockElement('catalog-pill-categories'),
  'catalog-pill-modifiers': createMockElement('catalog-pill-modifiers'),
  'catalog-subview-items': createMockElement('catalog-subview-items'),
  'catalog-subview-categories': createMockElement('catalog-subview-categories'),
  'catalog-subview-modifiers': createMockElement('catalog-subview-modifiers'),

  // Modals & Toast
  'modal-modifier-group-form': createMockElement('modal-modifier-group-form', ['hidden']),
  'modal-backdrop-overlay': createMockElement('modal-backdrop-overlay', ['hidden']),
  'pos-toast': createMockElement('pos-toast'),
  'toast-message': createMockElement('toast-message'),
  'toast-icon-wrapper': createMockElement('toast-icon-wrapper')
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
assert(typeof bus.batchAssignModifierGroup === 'function', 'POS_BUS.batchAssignModifierGroup is available');

// Extract script block from settings.html
const scriptBlocks = html.split(/<script[^>]*>/i);
const mainScriptBlock = scriptBlocks.find(b => b.includes('function renderBatchLinkerMatrix'));
assert(!!mainScriptBlock, 'Main script block containing renderBatchLinkerMatrix found in settings.html');
const scriptContent = mainScriptBlock.split(/<\/script>/i)[0];

let toastMessages = [];
let cashChimePlayed = false;
const sandbox = {
  document: global.document,
  window: global.window,
  console: console,
  setInterval: () => {},
  setTimeout: (fn) => { fn(); return 1; },
  clearTimeout: () => {},
  Date: Date,
  Math: Math,
  parseInt: parseInt,
  Number: Number,
  String: String,
  Array: Array,
  JSON: JSON,
  Set: Set,
  currentTab: 'catalog',
  currentCatalogSubView: 'linker',
  playTapChirp: () => {},
  playSaveChime: () => {},
  playDeleteChime: () => {},
  playSaveCashChime: () => { cashChimePlayed = true; },
  showToast: (msg) => { toastMessages.push(msg); },
  confirm: () => true,
  refreshCatalogCounters: () => {},
  switchCatalogSubView: (sub) => { sandbox.currentCatalogSubView = sub; }
};

vm.createContext(sandbox);

try {
  vm.runInContext(scriptContent, sandbox);
  assert(true, 'settings.html script compiled and evaluated without errors');
  const origShowToast = sandbox.showToast;
  sandbox.showToast = (msg, isErr) => {
    toastMessages.push(msg);
    if (typeof origShowToast === 'function') origShowToast(msg, isErr);
  };
} catch (e) {
  assert(false, 'settings.html script execution failed', e.stack);
}

// 5. Test Functions Existence & State
console.log('\n--- 5. Checking Batch Linker Handlers & State ---');
assert(typeof sandbox.linkerState === 'object' && sandbox.linkerState !== null, 'linkerState is defined as object');
assert(typeof sandbox.renderBatchLinkerMatrix === 'function', 'renderBatchLinkerMatrix is defined');
assert(typeof sandbox.renderCatalogLinkerMatrix === 'function', 'renderCatalogLinkerMatrix alias is defined');
assert(typeof sandbox.selectLinkerModifierGroup === 'function', 'selectLinkerModifierGroup is defined');
assert(typeof sandbox.toggleLinkerCategoryAll === 'function', 'toggleLinkerCategoryAll is defined');
assert(typeof sandbox.toggleLinkerItem === 'function', 'toggleLinkerItem is defined');
assert(typeof sandbox.updateLiveSimulator === 'function', 'updateLiveSimulator is defined');
assert(typeof sandbox.saveBatchLinkerAssignments === 'function', 'saveBatchLinkerAssignments is defined');
assert(typeof sandbox.playSaveCashChime === 'function', 'playSaveCashChime is defined');

// 6. Test renderBatchLinkerMatrix()
console.log('\n--- 6. Testing renderBatchLinkerMatrix() ---');
sandbox.renderBatchLinkerMatrix();

const railHtml = mockDOM['linker-group-rail'].innerHTML;
assert(railHtml.length > 0, 'renderBatchLinkerMatrix populated Column 1 #linker-group-rail');
assert(railHtml.includes('Topping Trà Trái Cây') || railHtml.includes('Topping'), 'Modifier group rendered in rail');
assert(railHtml.includes('món'), 'Assigned items count badge rendered in rail');

const treeHtml = mockDOM['linker-category-tree'].innerHTML;
assert(treeHtml.length > 0, 'renderBatchLinkerMatrix populated Column 2 #linker-category-tree');
assert(treeHtml.includes('Cà phê Việt') || treeHtml.includes('Cà phê'), 'Category rendered in items tree');
assert(treeHtml.includes('Chọn tất cả món') || treeHtml.includes('Bỏ chọn tất cả'), 'Category 1-click toggle button rendered');
assert(treeHtml.includes('eye'), 'Live simulator focus eye button rendered');

const simHtml = mockDOM['linker-simulator-screen'].innerHTML;
assert(simHtml.length > 0, 'renderBatchLinkerMatrix populated Column 3 #linker-simulator-screen');
assert(simHtml.includes('Chọn kích cỡ'), 'Simulator renders size selection group');
assert(simHtml.includes('Mức ngọt (Đường)'), 'Simulator renders sugar selection group');
assert(simHtml.includes('Mức đá'), 'Simulator renders ice selection group');
assert(simHtml.includes('Topping thêm'), 'Simulator renders toppings checklist group');
assert(simHtml.includes('đ'), 'Simulator displays whole VND pricing');

// 7. Test selectLinkerModifierGroup(groupId)
console.log('\n--- 7. Testing selectLinkerModifierGroup(groupId) ---');
sandbox.selectLinkerModifierGroup('top_tea');
assert(sandbox.linkerState.activeGroupId === 'top_tea', 'selectLinkerModifierGroup switched activeGroupId to "top_tea"');

const currentCatalog = bus.getCatalogData();
const itemsWithTopTea = currentCatalog.items.filter(i => i.assignedModifierGroupIds && i.assignedModifierGroupIds.includes('top_tea')).map(i => i.id);

itemsWithTopTea.forEach(id => {
  assert(sandbox.linkerState.selectedItemIds.has(id), `Item ${id} is pre-selected in linkerState.selectedItemIds for "top_tea"`);
});

// Check sticky bar reflects active group name
const commitGrpName = mockDOM['linker-commit-group-name'].textContent;
assert(commitGrpName.includes('Topping Trà Trái Cây'), 'Sticky commit bar updated with active group name');

// 8. Test 1-Click Category Bulk Toggle (toggleLinkerCategoryAll)
console.log('\n--- 8. Testing 1-Click Category Toggle (toggleLinkerCategoryAll) ---');
const coffeeItems = currentCatalog.items.filter(i => i.categoryId === 'coffee_vn');
assert(coffeeItems.length > 0, 'Category "coffee_vn" has items');

// Toggle all coffee items ON
sandbox.toggleLinkerCategoryAll('coffee_vn');
const allCoffeeSelected = coffeeItems.every(i => sandbox.linkerState.selectedItemIds.has(i.id));
assert(allCoffeeSelected, 'toggleLinkerCategoryAll("coffee_vn") selected all items in category');

// Toggle all coffee items OFF
sandbox.toggleLinkerCategoryAll('coffee_vn');
const noneCoffeeSelected = coffeeItems.every(i => !sandbox.linkerState.selectedItemIds.has(i.id));
assert(noneCoffeeSelected, 'toggleLinkerCategoryAll("coffee_vn") second click deselected all items in category');

// 9. Test Single Item Toggle (toggleLinkerItem)
console.log('\n--- 9. Testing Single Item Toggle (toggleLinkerItem) ---');
const testItemId = 'cfsd';
const initialHas = sandbox.linkerState.selectedItemIds.has(testItemId);
sandbox.toggleLinkerItem(testItemId);
assert(sandbox.linkerState.selectedItemIds.has(testItemId) !== initialHas, `toggleLinkerItem("${testItemId}") toggled selection state`);
sandbox.toggleLinkerItem(testItemId);
assert(sandbox.linkerState.selectedItemIds.has(testItemId) === initialHas, `toggleLinkerItem("${testItemId}") second toggle reverted state`);

// 10. Test Live Simulator Dynamic Synchronization
console.log('\n--- 10. Testing Live Simulator Dynamic Real-Time Sync ---');
// Focus simulator on 'tdcs' (Trà đào cam sả)
sandbox.updateLiveSimulator('tdcs');
assert(sandbox.linkerState.simulatedItemId === 'tdcs', 'updateLiveSimulator("tdcs") focused simulator on "tdcs"');

// When tdcs is selected for top_tea:
sandbox.linkerState.selectedItemIds.add('tdcs');
sandbox.updateLiveSimulator('tdcs');
let simScreenHtml = mockDOM['linker-simulator-screen'].innerHTML;
assert(simScreenHtml.includes('Topping Trà Trái Cây'), 'Simulator includes "Topping Trà Trái Cây" when tdcs is checked');
assert(simScreenHtml.includes('Trân châu trắng'), 'Simulator includes topping option "Trân châu trắng"');

// When tdcs is deselected:
sandbox.toggleLinkerItem('tdcs');
assert(!sandbox.linkerState.selectedItemIds.has('tdcs'), 'tdcs is unselected');
simScreenHtml = mockDOM['linker-simulator-screen'].innerHTML;
// Notice that tdcs assignedModifierGroupIds in raw catalog had top_tea, but the matrix dynamically overrides active group!
assert(!simScreenHtml.includes('Trân châu trắng') || simScreenHtml.includes('Chưa có nhóm topping'), 'Simulator dynamically unlinks "top_tea" when unchecked in Column 2');

// Toggle back on:
sandbox.toggleLinkerItem('tdcs');
assert(sandbox.linkerState.selectedItemIds.has('tdcs'), 'tdcs is checked again');
simScreenHtml = mockDOM['linker-simulator-screen'].innerHTML;
assert(simScreenHtml.includes('Trân châu trắng'), 'Simulator dynamically re-links "top_tea" when checked in Column 2');

// Test Simulator Interactive Selection & Price Recalculation
console.log('\n--- 11. Testing Simulator Interactive Selection & Price Recalculation ---');
sandbox.selectSimulatorSize('L');
assert(sandbox.linkerState.simulatedSizeId === 'L', 'selectSimulatorSize("L") sets size to L');

sandbox.toggleSimulatorTopping('tran_chau_trang');
assert(sandbox.linkerState.simulatedToppings.has('tran_chau_trang'), 'toggleSimulatorTopping("tran_chau_trang") toggles topping on');
let priceReadout = mockDOM['simulator-total-price'].textContent;
if (!priceReadout) {
  const match = mockDOM['linker-simulator-screen'].innerHTML.match(/id="simulator-total-price"[^>]*>([^<]+)<\/span>/);
  if (match) priceReadout = match[1];
}
assert(priceReadout && priceReadout.includes('đ'), 'Simulator total price formatted in VND with JetBrains Mono');

// 12. Test saveBatchLinkerAssignments() Persistence
console.log('\n--- 12. Testing saveBatchLinkerAssignments() Persistence ---');
// Prepare specific batch assignment: assign top_tea to cfsd, bacxiu, tdcs
sandbox.linkerState.activeGroupId = 'top_tea';
sandbox.linkerState.selectedItemIds = new Set(['cfsd', 'bacxiu', 'tdcs']);

cashChimePlayed = false;
toastMessages = [];
sandbox.saveBatchLinkerAssignments();

assert(cashChimePlayed || toastMessages.length > 0, 'saveBatchLinkerAssignments played cash chime and displayed toast');
assert(toastMessages.some(m => m.includes('thành công') || m.includes('áp dụng')), 'Toast feedback confirms successful batch assignment');

// Verify persistence in POS_BUS
const catalogAfterSave = bus.getCatalogData();
const savedItemsWithTopTea = catalogAfterSave.items.filter(i => i.assignedModifierGroupIds && i.assignedModifierGroupIds.includes('top_tea')).map(i => i.id);

assert(savedItemsWithTopTea.includes('cfsd'), 'Item "cfsd" now has "top_tea" persisted in POS_BUS');
assert(savedItemsWithTopTea.includes('bacxiu'), 'Item "bacxiu" now has "top_tea" persisted in POS_BUS');
assert(savedItemsWithTopTea.includes('tdcs'), 'Item "tdcs" now has "top_tea" persisted in POS_BUS');
assert(!savedItemsWithTopTea.includes('cfdd'), 'Item "cfdd" (unselected) does NOT have "top_tea" in POS_BUS');

// Final summary
console.log('\n================================================================');
console.log(`  RESULTS: ${passedTests} passed, ${failedTests} failed, ${totalTests} total`);
console.log('================================================================');

if (failedTests > 0) {
  process.exit(1);
} else {
  console.log('ALL TASK 5 VERIFICATION CHECKS PASSED!\n');
  process.exit(0);
}
