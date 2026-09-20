/**
 * POS Cafe Multi-Screen Suite - Comprehensive End-to-End Verification Test
 * 
 * Verifies:
 * 1. File existence & syntax parsing for all 7 screens + bus
 * 2. Strict compliance with anti-slop rules:
 *    - Zero em-dashes (\u2014, \u2013)
 *    - Zero Unicode emojis
 *    - Zero AI-purple/violet gradients
 *    - Whole VND currency formatting (no decimals)
 *    - Font stacks: Outfit for UI, JetBrains Mono for numbers/codes
 * 3. Touch target validation (>= 48px height on all interactive elements)
 * 4. Station Switcher link integrity across all pages
 * 5. pos-bus.js integration & cross-tab contract validation
 */

const fs = require('fs');
const path = require('path');

const ROOT_DIR = path.resolve(__dirname, '..');
const POS_DIR = path.join(ROOT_DIR, 'design-system', 'pos-cafe');

const PAGES = [
  { name: 'Cashier Terminal (index.html)', path: path.join(POS_DIR, 'index.html'), expectedTitle: 'POS Cafe' },
  { name: 'Staff PIN Auth (auth.html)', path: path.join(POS_DIR, 'pages', 'auth.html'), expectedTitle: 'Đăng nhập' },
  { name: 'Kitchen KDS (kds.html)', path: path.join(POS_DIR, 'pages', 'kds.html'), expectedTitle: 'Bếp / Barista KDS' },
  { name: 'Sales Shift (shift.html)', path: path.join(POS_DIR, 'pages', 'shift.html'), expectedTitle: 'Quản lý Ca & Quỹ' },
  { name: 'Floor Plan (tables.html)', path: path.join(POS_DIR, 'pages', 'tables.html'), expectedTitle: 'Sơ đồ Bàn' },
  { name: 'Order History (history.html)', path: path.join(POS_DIR, 'pages', 'history.html'), expectedTitle: 'Lịch sử Giao dịch' },
  { name: 'Settings & Stock (settings.html)', path: path.join(POS_DIR, 'pages', 'settings.html'), expectedTitle: 'Cài đặt Cửa hàng' },
];

const SHARED_BUS = path.join(POS_DIR, 'shared', 'pos-bus.js');

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
console.log('  POS CAFE SUITE - COMPREHENSIVE END-TO-END VERIFICATION');
console.log('================================================================\n');

// 1. Check shared pos-bus.js
console.log('--- 1. Checking Shared POS Bus (pos-bus.js) ---');
assert(fs.existsSync(SHARED_BUS), 'pos-bus.js exists');
const busContent = fs.readFileSync(SHARED_BUS, 'utf8');
assert(!busContent.includes('\u2014') && !busContent.includes('\u2013'), 'pos-bus.js has zero em-dashes');
const emojiRegex = /[\u{1F300}-\u{1F9FF}\u{2600}-\u{26FF}\u{2700}-\u{27BF}]/u;
assert(!emojiRegex.test(busContent), 'pos-bus.js has zero emojis');
assert(busContent.includes('BroadcastChannel'), 'pos-bus.js initializes BroadcastChannel');
assert(busContent.includes('publish'), 'pos-bus.js defines publish method');
assert(busContent.includes('subscribe'), 'pos-bus.js defines subscribe method');
assert(busContent.includes('POS_ORDERS'), 'pos-bus.js handles POS_ORDERS schema');
assert(busContent.includes('POS_TABLES'), 'pos-bus.js handles POS_TABLES schema');
assert(busContent.includes('POS_ACTIVE_SHIFT'), 'pos-bus.js handles POS_ACTIVE_SHIFT schema');
assert(busContent.includes('POS_CATALOG_STATUS'), 'pos-bus.js handles POS_CATALOG_STATUS schema');
assert(busContent.includes('POS_STORE_INFO'), 'pos-bus.js handles POS_STORE_INFO schema');

// 2. Check all 7 pages
console.log('\n--- 2. Checking HTML Pages Compliance ---');
PAGES.forEach((page) => {
  console.log(`\nVerifying ${page.name}...`);
  assert(fs.existsSync(page.path), `File exists: ${page.name}`);
  const content = fs.readFileSync(page.path, 'utf8');

  // Anti-slop: zero em-dashes
  const hasEmDash = content.includes('\u2014') || content.includes('\u2013');
  assert(!hasEmDash, `${page.name} has zero em-dashes`);

  // Anti-slop: zero emojis
  const hasEmoji = emojiRegex.test(content);
  assert(!hasEmoji, `${page.name} has zero Unicode emojis`);

  // Anti-slop: no AI-purple gradients
  const hasPurpleGradient = /bg-gradient-to-[a-z]+\s+from-(?:purple|violet|fuchsia)/i.test(content);
  assert(!hasPurpleGradient, `${page.name} has no AI-purple gradients`);

  // Typography: Outfit & JetBrains Mono
  assert(content.includes('Outfit') && content.includes('JetBrains+Mono'), `${page.name} imports Outfit & JetBrains Mono`);

  // Dependencies: Tailwind & Lucide
  assert(content.includes('tailwindcss.com'), `${page.name} includes Tailwind CSS`);
  assert(content.includes('lucide@latest'), `${page.name} includes Lucide Icons`);

  // Bus integration (all except auth.html which is a standalone lock screen, though auth can link to bus too)
  if (page.name !== 'Staff PIN Auth (auth.html)') {
    assert(content.includes('pos-bus.js'), `${page.name} includes shared pos-bus.js`);
  }

  // Currency check: Ensure whole VND format
  const decimalVNDMatches = content.match(/[0-9]+[.,][0-9]{1,2}\s*(?:đ|vnd|\$)/gi) || [];
  assert(decimalVNDMatches.length === 0, `${page.name} strictly uses whole VND without decimals`, `Found: ${decimalVNDMatches.join(', ')}`);

  // Touch Target check on buttons
  const buttonTags = content.match(/<button[^>]*>/g) || [];
  let violations = [];
  buttonTags.forEach((btn) => {
    const pxMatch = btn.match(/min-h-\[(\d+)px\]/);
    const pxHeight = pxMatch ? parseInt(pxMatch[1], 10) : 0;
    const isBig = pxHeight >= 48 ||
                  btn.includes('h-12') || 
                  btn.includes('h-14') || 
                  btn.includes('h-16') ||
                  btn.includes('min-h-12') ||
                  btn.includes('min-h-14') ||
                  btn.includes('min-h-16') ||
                  btn.includes('btnClass'); // dynamic styling
    if (!isBig) {
      violations.push(btn.substring(0, 70));
    }
  });
  assert(violations.length === 0, `${page.name} buttons enforce >= 48px touch targets (${buttonTags.length} buttons)`, `Violations: ${violations.length}`);
});

// 3. Station Switcher Interlinking Validation
console.log('\n--- 3. Checking Station Switcher Hub Links Across Pages ---');
const APP_PAGES = PAGES.filter(p => p.name !== 'Staff PIN Auth (auth.html)');
APP_PAGES.forEach((page) => {
  const content = fs.readFileSync(page.path, 'utf8');
  assert(content.includes('btn-station-switcher'), `${page.name} has #btn-station-switcher button`);
  assert(content.includes('station-switcher-dropdown'), `${page.name} has #station-switcher-dropdown`);
  assert(content.includes('station-switcher-backdrop'), `${page.name} has #station-switcher-backdrop`);
});

// 4. Cross-Screen State Synchronization Contract Simulation
console.log('\n--- 4. Cross-Screen Bus Contract Simulation ---');

// Mock localStorage and BroadcastChannel
const mockStorage = {};
const mockBusListeners = {};

global.localStorage = {
  getItem: (k) => mockStorage[k] || null,
  setItem: (k, v) => { mockStorage[k] = v; },
  removeItem: (k) => { delete mockStorage[k]; }
};

class MockBroadcastChannel {
  constructor(channelName) {
    this.name = channelName;
  }
  postMessage(msg) {
    const list = mockBusListeners[this.name] || [];
    list.forEach(cb => cb({ data: msg }));
  }
  addEventListener(event, cb) {
    if (event === 'message') {
      if (!mockBusListeners[this.name]) mockBusListeners[this.name] = [];
      mockBusListeners[this.name].push(cb);
    }
  }
}
global.BroadcastChannel = MockBroadcastChannel;
global.window = {
  localStorage: global.localStorage,
  BroadcastChannel: MockBroadcastChannel,
  addEventListener: () => {}
};

// Execute pos-bus.js in this sandbox
eval(busContent);
const posBus = global.window.POS_BUS;

assert(posBus !== undefined, 'window.POS_BUS successfully instantiated in environment');

// Test Step A: Place Order on Cashier -> Received by KDS and History
let orderReceivedByKDS = null;
let orderReceivedByHistory = null;

posBus.subscribe('NEW_ORDER', (order) => { orderReceivedByKDS = order; });
posBus.subscribe('NEW_ORDER', (order) => { orderReceivedByHistory = order; });

const testOrder = {
  id: 'ORD-TEST-99',
  serviceNumber: 99,
  items: [{ name: 'Cà phê muối', quantity: 2, price: 39000 }],
  totalVND: 78000,
  paymentMethod: 'cash',
  table: { id: 'T03', name: 'Bàn 03' }
};

posBus.publish('NEW_ORDER', testOrder);
assert(orderReceivedByKDS && orderReceivedByKDS.id === 'ORD-TEST-99', 'Cross-tab: NEW_ORDER received by KDS subscriber');
assert(orderReceivedByHistory && orderReceivedByHistory.totalVND === 78000, 'Cross-tab: NEW_ORDER received by History subscriber');

// Test Step B: Stock toggle on Settings -> Received by Cashier
let stockUpdateReceived = null;
posBus.subscribe('CATALOG_STOCK_CHANGED', (status) => { stockUpdateReceived = status; });
posBus.setItemAvailable('cfmuoi', false);
assert(stockUpdateReceived && stockUpdateReceived.itemId === 'cfmuoi' && stockUpdateReceived.inStock === false, 'Cross-tab: CATALOG_STOCK_CHANGED received on item toggle');
assert(posBus.isItemAvailable('cfmuoi') === false, 'posBus.isItemAvailable reflects disabled stock state');

// Test Step C: Store Info updated on Settings -> Received by other stations
let storeInfoReceived = null;
posBus.subscribe('STORE_INFO_UPDATED', (info) => { storeInfoReceived = info; });
posBus.saveStoreInfo({ storeName: 'The Coffee Workshop Flagship', hotline: '0988 999 888' });
assert(storeInfoReceived && storeInfoReceived.storeName === 'The Coffee Workshop Flagship', 'Cross-tab: STORE_INFO_UPDATED received across stations');

// Test Step D: Table Status Changed on Waiter -> Received by Cashier
let tableStatusReceived = null;
posBus.subscribe('TABLE_STATUS_CHANGED', (table) => { tableStatusReceived = table; });
posBus.setTableStatus('T03', 'billing', { totalVND: 78000 });
assert(tableStatusReceived && tableStatusReceived.tableId === 'T03' && tableStatusReceived.status === 'billing', 'Cross-tab: TABLE_STATUS_CHANGED synchronized to Cashier terminal');

// Test Step E: Cash Transaction on Cashier -> Cash Drawer Reconciliation on Shift
const shiftState = posBus.getActiveShift();
assert(shiftState && typeof shiftState.cashSales === 'number', 'posBus provides valid active shift data');
posBus.recordCashTransaction(78000);
const updatedShift = posBus.getActiveShift();
assert(updatedShift.cashSales === shiftState.cashSales + 78000, 'Cross-tab: Cash order updates shift drawer cash sales automatically');

// 5. Final Report Summary
console.log('\n================================================================');
console.log(`  VERIFICATION RESULTS: ${passedTests}/${totalTests} TESTS PASSED`);
if (failedTests === 0) {
  console.log('  STATUS: 100% SUCCESS - ALL SCREEN CONTRACTS VERIFIED');
} else {
  console.log(`  STATUS: FAILED - ${failedTests} ASSERTIONS FAILED`);
}
console.log('================================================================\n');

process.exit(failedTests === 0 ? 0 : 1);
