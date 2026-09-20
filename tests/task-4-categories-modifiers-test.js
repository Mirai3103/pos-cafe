/**
 * POS Cafe - Task 4 Verification Test: Categories Manager & Modifier Groups Studio
 * 
 * Verifies:
 * 1. Element presence in settings.html:
 *    - #catalog-subview-categories:
 *      * Header with title & counter (#catalog-categories-count-display)
 *      * Primary CTA (#btn-catalog-categories-add-cat) "+ THÊM DANH MỤC"
 *      * Responsive Category Bento Cards Grid (#catalog-categories-grid)
 *    - Category Modal (#modal-category-form):
 *      * Inputs: #category-form-id, #category-form-name, #category-form-order
 *      * Lucide Icon Picker Grid (#category-icon-picker-grid) with 12 preset icons
 *      * Default Modifier Groups chips container (#category-default-modifiers-chips)
 *      * Footer buttons: Cancel, #btn-category-form-delete, #btn-category-form-save
 *    - #catalog-subview-modifiers:
 *      * Header with title & counter (#catalog-modifiers-count-display)
 *      * Primary CTA (#btn-catalog-modifiers-add-group) "+ TẠO NHÓM TOPPING"
 *      * Responsive Modifier Groups Grid (#catalog-modifiers-grid)
 *    - Modifier Group Studio Modal (#modal-modifier-group-form):
 *      * Inputs: #modifier-group-form-id, #modifier-group-form-name
 *      * Selection type controls: single/multiple radios, #modifier-group-min-select, #modifier-group-max-select
 *      * Options list table (#modifier-group-options-list)
 *      * CTA #btn-add-modifier-option-row
 *      * Footer buttons: Cancel, #btn-modifier-group-delete, #btn-modifier-group-save
 * 2. Strict Design Compliance:
 *    - All touch targets >= 48px
 *    - Zero em-dashes (— or –)
 *    - Zero Unicode emojis
 *    - Zero AI-purple gradients
 *    - Whole VND formatting without decimals
 * 3. Behavioral Runtime Simulation:
 *    - Category: renderCatalogCategoriesGrid(), openCategoryModal(), selectCategoryIcon(),
 *      renderCategoryDefaultModifierChips(), toggleCategoryDefaultModifier(), saveCategoryForm(), deleteCategoryConfirmed()
 *    - Modifiers: renderCatalogModifierGroupsGrid(), openModifierGroupModal(), setModifierSelectionType(),
 *      addModifierOptionRow(), removeModifierOptionRow(), saveModifierGroupForm(), deleteModifierGroupConfirmed(),
 *      jumpToBatchLinkerForGroup()
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
console.log('  TASK 4: CATEGORIES MANAGER & MODIFIER GROUPS STUDIO TEST');
console.log('================================================================\n');

// 1. Static HTML Verification
console.log('--- 1. Checking Static HTML Structure in settings.html ---');
assert(fs.existsSync(SETTINGS_PATH), 'settings.html exists');
const html = fs.readFileSync(SETTINGS_PATH, 'utf8');

// Categories Subview
assert(html.includes('id="catalog-subview-categories"'), '#catalog-subview-categories container exists');
assert(html.includes('id="catalog-categories-count-display"'), '#catalog-categories-count-display exists');
assert(html.includes('id="btn-catalog-categories-add-cat"'), '#btn-catalog-categories-add-cat button exists');
assert(html.includes('+ THÊM DANH MỤC'), 'CTA label "+ THÊM DANH MỤC" exists');
assert(html.includes('id="catalog-categories-grid"'), '#catalog-categories-grid exists');

// Category Modal (#modal-category-form)
assert(html.includes('id="modal-category-form"'), '#modal-category-form exists');
assert(html.includes('id="modal-category-form-title"'), '#modal-category-form-title exists');
assert(html.includes('id="category-form-id"'), '#category-form-id hidden input exists');
assert(html.includes('id="category-form-name"'), '#category-form-name input exists');
assert(html.includes('id="category-form-order"'), '#category-form-order input exists');
assert(html.includes('id="category-icon-picker-grid"'), '#category-icon-picker-grid exists');

// Icon picker includes 12 preset icons
const expectedIcons = ['coffee', 'cup-soda', 'leaf', 'sparkles', 'cake', 'layers', 'heart', 'flame', 'zap', 'utensils', 'milk', 'citrus'];
expectedIcons.forEach(icon => {
  assert(html.includes(`data-icon="${icon}"`), `Icon picker button with data-icon="${icon}" exists`);
});

assert(html.includes('id="category-default-modifiers-chips"'), '#category-default-modifiers-chips container exists');
assert(html.includes('id="btn-category-form-delete"'), '#btn-category-form-delete button exists');
assert(html.includes('id="btn-category-form-save"'), '#btn-category-form-save button exists');

// Modifier Groups Subview
assert(html.includes('id="catalog-subview-modifiers"'), '#catalog-subview-modifiers container exists');
assert(html.includes('id="catalog-modifiers-count-display"'), '#catalog-modifiers-count-display exists');
assert(html.includes('id="btn-catalog-modifiers-add-group"'), '#btn-catalog-modifiers-add-group button exists');
assert(html.includes('+ TẠO NHÓM TOPPING'), 'CTA label "+ TẠO NHÓM TOPPING" exists');
assert(html.includes('id="catalog-modifiers-grid"'), '#catalog-modifiers-grid exists');

// Modifier Group Form Modal (#modal-modifier-group-form)
assert(html.includes('id="modal-modifier-group-form"'), '#modal-modifier-group-form exists');
assert(html.includes('id="modal-modifier-group-title"'), '#modal-modifier-group-title exists');
assert(html.includes('id="modifier-group-form-id"'), '#modifier-group-form-id hidden input exists');
assert(html.includes('id="modifier-group-form-name"'), '#modifier-group-form-name input exists');
assert(html.includes('id="btn-mod-type-single"'), '#btn-mod-type-single radio button exists');
assert(html.includes('id="btn-mod-type-multiple"'), '#btn-mod-type-multiple radio button exists');
assert(html.includes('id="modifier-group-min-select"'), '#modifier-group-min-select input exists');
assert(html.includes('id="modifier-group-max-select"'), '#modifier-group-max-select input exists');
assert(html.includes('id="modifier-group-options-list"'), '#modifier-group-options-list exists');
assert(html.includes('id="btn-add-modifier-option-row"'), '#btn-add-modifier-option-row button exists');
assert(html.includes('id="btn-modifier-group-delete"'), '#btn-modifier-group-delete button exists');
assert(html.includes('id="btn-modifier-group-save"'), '#btn-modifier-group-save button exists');

// 2. Design System & Anti-Slop Compliance
console.log('\n--- 2. Design System & Anti-Slop Compliance ---');
const emojiRegex = /[\u{1F300}-\u{1F9FF}\u{2600}-\u{26FF}\u{2700}-\u{27BF}]/u;
assert(!emojiRegex.test(html), 'Zero Unicode emojis in settings.html');
assert(!html.includes('\u2014') && !html.includes('\u2013'), 'Zero em-dashes (— or –) in settings.html');
const hasPurple = /bg-gradient-to-[a-z]+\s+from-(?:purple|violet|fuchsia)/i.test(html);
assert(!hasPurple, 'Zero AI-purple gradients in settings.html');

// Touch Targets (>= 48px)
console.log('\n--- 3. Checking Touch Targets on Task 4 Buttons (>= 48px) ---');
const task4ButtonIds = [
  'btn-catalog-categories-add-cat',
  'btn-category-form-delete',
  'btn-category-form-save',
  'btn-catalog-modifiers-add-group',
  'btn-mod-type-single',
  'btn-mod-type-multiple',
  'btn-add-modifier-option-row',
  'btn-modifier-group-delete',
  'btn-modifier-group-save'
];

task4ButtonIds.forEach(id => {
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
    querySelectorAll: (selector) => {
      // Basic mock for selector query
      return [];
    },
    querySelector: () => null
  };
}

const mockDOM = {
  // Category Subview & Modal
  'catalog-subview-categories': createMockElement('catalog-subview-categories'),
  'catalog-categories-grid': createMockElement('catalog-categories-grid'),
  'catalog-categories-count-display': createMockElement('catalog-categories-count-display'),
  'btn-catalog-categories-add-cat': createMockElement('btn-catalog-categories-add-cat'),
  'modal-category-form': createMockElement('modal-category-form', ['hidden']),
  'modal-category-form-title': createMockElement('modal-category-form-title'),
  'category-form-id': createMockElement('category-form-id'),
  'category-form-name': createMockElement('category-form-name'),
  'category-form-order': createMockElement('category-form-order'),
  'category-icon-picker-grid': createMockElement('category-icon-picker-grid'),
  'category-default-modifiers-chips': createMockElement('category-default-modifiers-chips'),
  'btn-category-form-delete': createMockElement('btn-category-form-delete', ['hidden']),
  'btn-category-form-save': createMockElement('btn-category-form-save'),

  // Modifier Groups Subview & Modal
  'catalog-subview-modifiers': createMockElement('catalog-subview-modifiers'),
  'catalog-modifiers-grid': createMockElement('catalog-modifiers-grid'),
  'catalog-modifiers-count-display': createMockElement('catalog-modifiers-count-display'),
  'btn-catalog-modifiers-add-group': createMockElement('btn-catalog-modifiers-add-group'),
  'modal-modifier-group-form': createMockElement('modal-modifier-group-form', ['hidden']),
  'modal-modifier-group-title': createMockElement('modal-modifier-group-title'),
  'modifier-group-form-id': createMockElement('modifier-group-form-id'),
  'modifier-group-form-name': createMockElement('modifier-group-form-name'),
  'btn-mod-type-single': createMockElement('btn-mod-type-single'),
  'btn-mod-type-multiple': createMockElement('btn-mod-type-multiple'),
  'mod-type-single-dot': createMockElement('mod-type-single-dot'),
  'mod-type-multiple-dot': createMockElement('mod-type-multiple-dot'),
  'modifier-group-min-select': createMockElement('modifier-group-min-select'),
  'modifier-group-max-select': createMockElement('modifier-group-max-select'),
  'modifier-group-options-list': createMockElement('modifier-group-options-list'),
  'btn-add-modifier-option-row': createMockElement('btn-add-modifier-option-row'),
  'btn-modifier-group-delete': createMockElement('btn-modifier-group-delete', ['hidden']),
  'btn-modifier-group-save': createMockElement('btn-modifier-group-save'),

  // Shared Modals & Elements
  'modal-backdrop-overlay': createMockElement('modal-backdrop-overlay', ['hidden']),
  'tab-catalog-counter': createMockElement('tab-catalog-counter'),
  'pill-badge-items': createMockElement('pill-badge-items'),
  'pill-badge-categories': createMockElement('pill-badge-categories'),
  'pill-badge-modifiers': createMockElement('pill-badge-modifiers'),
  'pos-toast': createMockElement('pos-toast'),
  'toast-message': createMockElement('toast-message'),
  'toast-icon-wrapper': createMockElement('toast-icon-wrapper')
};

global.document = {
  getElementById: (id) => mockDOM[id] || null,
  addEventListener: () => {},
  querySelectorAll: (selector) => {
    if (selector === '.cat-icon-btn') {
      return expectedIcons.map(icon => {
        const btn = createMockElement('icon-' + icon);
        btn.setAttribute('data-icon', icon);
        return btn;
      });
    }
    if (selector.includes('modifier-opt-row')) {
      return [];
    }
    return [];
  }
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
assert(typeof bus.saveCategory === 'function', 'POS_BUS.saveCategory is available');
assert(typeof bus.deleteCategory === 'function', 'POS_BUS.deleteCategory is available');
assert(typeof bus.saveModifierGroup === 'function', 'POS_BUS.saveModifierGroup is available');
assert(typeof bus.deleteModifierGroup === 'function', 'POS_BUS.deleteModifierGroup is available');

// Extract script block from settings.html
const scriptBlocks = html.split(/<script[^>]*>/i);
const mainScriptBlock = scriptBlocks.find(b => b.includes('function renderCatalogCategoriesGrid'));
assert(!!mainScriptBlock, 'Main script block containing renderCatalogCategoriesGrid found in settings.html');
const scriptContent = mainScriptBlock.split(/<\/script>/i)[0];

let toastMessages = [];
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
  currentTab: 'catalog',
  currentCatalogSubView: 'categories',
  playTapChirp: () => {},
  playSaveChime: () => {},
  playDeleteChime: () => {},
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

// 5. Test Categories Subsystem
console.log('\n--- 5. Testing Categories Subsystem ---');
assert(typeof sandbox.renderCatalogCategoriesGrid === 'function', 'renderCatalogCategoriesGrid function is defined');
assert(typeof sandbox.openCategoryModal === 'function', 'openCategoryModal function is defined');
assert(typeof sandbox.closeCategoryModal === 'function', 'closeCategoryModal function is defined');
assert(typeof sandbox.selectCategoryIcon === 'function', 'selectCategoryIcon function is defined');
assert(typeof sandbox.saveCategoryForm === 'function', 'saveCategoryForm function is defined');
assert(typeof sandbox.deleteCategoryConfirmed === 'function', 'deleteCategoryConfirmed function is defined');

// Test renderCatalogCategoriesGrid
sandbox.renderCatalogCategoriesGrid();
const catGridHtml = mockDOM['catalog-categories-grid'].innerHTML;
assert(catGridHtml.length > 0, 'renderCatalogCategoriesGrid populated HTML into #catalog-categories-grid');
assert(catGridHtml.includes('Cà phê'), 'Category "Cà phê" is rendered in grid');
assert(catGridHtml.includes('Trà & Macchiato') || catGridHtml.includes('Trà'), 'Category "Trà & Macchiato" is rendered in grid');
assert(catGridHtml.includes('món'), 'Items count badge displayed in category card');

// Test openCategoryModal (Create Mode)
sandbox.openCategoryModal();
assert(!mockDOM['modal-category-form'].classList.contains('hidden'), 'openCategoryModal opens modal (hidden class removed)');
assert(!mockDOM['modal-backdrop-overlay'].classList.contains('hidden'), 'Modal backdrop overlay is shown');
assert(mockDOM['category-form-name'].value === '', 'Create mode resets name input');
assert(mockDOM['btn-category-form-delete'].classList.contains('hidden'), 'Create mode hides delete button');

// Test Icon Selection
sandbox.selectCategoryIcon('leaf');
assert(sandbox.selectedCategoryIcon === 'leaf', 'selectCategoryIcon updates selectedCategoryIcon state to "leaf"');

// Test Default Modifier Groups Chips
sandbox.renderCategoryDefaultModifierChips();
const chipsHtml = mockDOM['category-default-modifiers-chips'].innerHTML;
assert(chipsHtml.includes('Topping Trà Trái Cây') || chipsHtml.includes('Topping'), 'Default modifier group chips rendered');

// Test toggleCategoryDefaultModifier
const testModGroup = bus.getCatalogData().modifierGroups[0];
if (testModGroup) {
  const initialSelected = sandbox.selectedCategoryDefaultGroupIds.includes(testModGroup.id);
  sandbox.toggleCategoryDefaultModifier(testModGroup.id);
  const afterToggle = sandbox.selectedCategoryDefaultGroupIds.includes(testModGroup.id);
  assert(afterToggle !== initialSelected, 'toggleCategoryDefaultModifier toggled modifier group selection');
}

// Test saveCategoryForm Validation
toastMessages = [];
mockDOM['category-form-name'].value = '   ';
sandbox.saveCategoryForm();
assert(toastMessages.some(m => m.includes('nhập tên danh mục')), 'saveCategoryForm validates empty name input');

// Test saveCategoryForm Create New Category
const uniqueCatName = 'Trà Sữa Đặc Biệt ' + Date.now();
mockDOM['category-form-name'].value = uniqueCatName;
mockDOM['category-form-order'].value = '99';
sandbox.saveCategoryForm();
const catData = bus.getCatalogData();
const createdCat = catData.categories.find(c => c.name === uniqueCatName);
assert(!!createdCat, `saveCategoryForm successfully created "${uniqueCatName}" in POS_BUS`);
assert(createdCat.icon === 'leaf', `Created category preserved selected icon "leaf"`);
assert(mockDOM['modal-category-form'].classList.contains('hidden'), 'Modal closed after successful save');

// Test openCategoryModal (Edit Mode)
sandbox.openCategoryModal(createdCat.id);
assert(mockDOM['category-form-name'].value === uniqueCatName, 'Edit mode populates category name');
assert(!mockDOM['btn-category-form-delete'].classList.contains('hidden'), 'Edit mode shows delete button');

// Test deleteCategoryConfirmed
sandbox.deleteCategoryConfirmed(createdCat.id);
const catDataAfterDelete = bus.getCatalogData();
assert(!catDataAfterDelete.categories.some(c => c.id === createdCat.id), 'deleteCategoryConfirmed successfully deleted category');

// 6. Test Modifier Groups Studio Subsystem
console.log('\n--- 6. Testing Modifier Groups Studio Subsystem ---');
assert(typeof sandbox.renderCatalogModifierGroupsGrid === 'function', 'renderCatalogModifierGroupsGrid function is defined');
assert(typeof sandbox.renderCatalogModifiersGrid === 'function', 'renderCatalogModifiersGrid alias is defined');
assert(typeof sandbox.openModifierGroupModal === 'function', 'openModifierGroupModal function is defined');
assert(typeof sandbox.closeModifierGroupModal === 'function', 'closeModifierGroupModal function is defined');
assert(typeof sandbox.setModifierSelectionType === 'function', 'setModifierSelectionType function is defined');
assert(typeof sandbox.addModifierOptionRow === 'function', 'addModifierOptionRow function is defined');
assert(typeof sandbox.removeModifierOptionRow === 'function', 'removeModifierOptionRow function is defined');
assert(typeof sandbox.saveModifierGroupForm === 'function', 'saveModifierGroupForm function is defined');
assert(typeof sandbox.deleteModifierGroupConfirmed === 'function', 'deleteModifierGroupConfirmed function is defined');
assert(typeof sandbox.jumpToBatchLinkerForGroup === 'function', 'jumpToBatchLinkerForGroup function is defined');

// Test renderCatalogModifierGroupsGrid
sandbox.renderCatalogModifierGroupsGrid();
const modGridHtml = mockDOM['catalog-modifiers-grid'].innerHTML;
assert(modGridHtml.length > 0, 'renderCatalogModifierGroupsGrid populated HTML into #catalog-modifiers-grid');
assert(modGridHtml.includes('Topping'), 'Modifier group rendered in grid');
assert(modGridHtml.includes('Áp dụng cho') || modGridHtml.includes('áp dụng'), 'Applied items count badge displayed');
assert(modGridHtml.includes('đ') || modGridHtml.includes('VND'), 'Child options display surcharge pricing');

// Test openModifierGroupModal (Create Mode)
sandbox.openModifierGroupModal();
assert(!mockDOM['modal-modifier-group-form'].classList.contains('hidden'), 'openModifierGroupModal opens modal');
assert(mockDOM['modifier-group-form-name'].value === '', 'Create mode resets group name');
assert(mockDOM['btn-modifier-group-delete'].classList.contains('hidden'), 'Create mode hides delete button');
assert(sandbox.currentModifierOptions.length >= 1, 'Default option row is initialized');

// Test Selection Type Switch
sandbox.setModifierSelectionType('single');
assert(sandbox.currentModifierSelectionType === 'single', 'setModifierSelectionType switches to single mode');
assert(String(mockDOM['modifier-group-min-select'].value) === '0' || String(mockDOM['modifier-group-min-select'].value) === '1', 'Min select is bounded for single mode');

sandbox.setModifierSelectionType('multiple');
assert(sandbox.currentModifierSelectionType === 'multiple', 'setModifierSelectionType switches to multiple mode');

// Test addModifierOptionRow and removeModifierOptionRow
const initialOptCount = sandbox.currentModifierOptions.length;
sandbox.addModifierOptionRow('Hạt sen tươi', 15000);
assert(sandbox.currentModifierOptions.length === initialOptCount + 1, 'addModifierOptionRow appended new option');
const lastOpt = sandbox.currentModifierOptions[sandbox.currentModifierOptions.length - 1];
assert(lastOpt.name === 'Hạt sen tươi' && lastOpt.price === 15000, 'New option has correct name and surcharge');

sandbox.removeModifierOptionRow(sandbox.currentModifierOptions.length - 1);
assert(sandbox.currentModifierOptions.length === initialOptCount, 'removeModifierOptionRow removed target option');

// Test saveModifierGroupForm Validation
toastMessages = [];
mockDOM['modifier-group-form-name'].value = '   ';
sandbox.saveModifierGroupForm();
assert(toastMessages.some(m => m.includes('nhập tên nhóm')), 'saveModifierGroupForm validates empty name');

// Test saveModifierGroupForm Create New Group
const uniqueGroupName = 'Extra Topping X ' + Date.now();
mockDOM['modifier-group-form-name'].value = uniqueGroupName;
sandbox.currentModifierOptions = [
  { id: 'opt_1', name: 'Trân châu đường đen', price: 10000, isAvailable: true },
  { id: 'opt_2', name: 'Thạch củ năng', price: 8000, isAvailable: true }
];
sandbox.saveModifierGroupForm();
const catalogNow = bus.getCatalogData();
const createdGroup = catalogNow.modifierGroups.find(g => g.name === uniqueGroupName);
assert(!!createdGroup, `saveModifierGroupForm successfully created "${uniqueGroupName}" in POS_BUS`);
assert(createdGroup.options.length === 2, 'Created group has 2 options');
assert(mockDOM['modal-modifier-group-form'].classList.contains('hidden'), 'Modal closed after successful save');

// Test openModifierGroupModal (Edit Mode)
sandbox.openModifierGroupModal(createdGroup.id);
assert(mockDOM['modifier-group-form-name'].value === uniqueGroupName, 'Edit mode populates group name');
assert(!mockDOM['btn-modifier-group-delete'].classList.contains('hidden'), 'Edit mode shows delete button');

// Test deleteModifierGroupConfirmed
sandbox.deleteModifierGroupConfirmed(createdGroup.id);
const catalogAfterGroupDelete = bus.getCatalogData();
assert(!catalogAfterGroupDelete.modifierGroups.some(g => g.id === createdGroup.id), 'deleteModifierGroupConfirmed deleted group');

// Test jumpToBatchLinkerForGroup
toastMessages = [];
const sampleGroupId = catalogAfterGroupDelete.modifierGroups[0].id;
sandbox.jumpToBatchLinkerForGroup(sampleGroupId);
assert(sandbox.currentCatalogSubView === 'linker', 'jumpToBatchLinkerForGroup switched subview to "linker"');
assert(toastMessages.length > 0, 'Toast displayed feedback message');

// Final summary
console.log('\n================================================================');
console.log(`  RESULTS: ${passedTests} passed, ${failedTests} failed, ${totalTests} total`);
console.log('================================================================');

if (failedTests > 0) {
  process.exit(1);
} else {
  console.log('ALL TASK 4 VERIFICATION CHECKS PASSED!\n');
  process.exit(0);
}
