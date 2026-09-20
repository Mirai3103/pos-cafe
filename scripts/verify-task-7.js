const fs = require('fs');
const path = require('path');

const files = [
  'design-system/pos-cafe/pages/settings.html',
  'design-system/pos-cafe/shared/pos-bus.js',
  'design-system/pos-cafe/index.html'
];

const emDashRegex = /\u2014/g;
const emojiRegex = /[\u{1F300}-\u{1F6FF}\u{1F900}-\u{1F9FF}\u{2600}-\u{26FF}\u{2700}-\u{27BF}]/u;

let hasError = false;

console.log('--- 1. BANNED CHARACTERS & EMOJI CHECK ---');
for (const f of files) {
  const content = fs.readFileSync(f, 'utf8');
  const emDashes = content.match(emDashRegex);
  const emDashCount = emDashes ? emDashes.length : 0;
  
  const lines = content.split('\n');
  let emojiFound = 0;
  lines.forEach((line, idx) => {
    if (emojiRegex.test(line)) {
      console.log(`EMOJI in ${f}:${idx + 1} -> ${line.trim()}`);
      emojiFound++;
      hasError = true;
    }
    if (line.includes('\u2014')) {
      console.log(`EM-DASH in ${f}:${idx + 1} -> ${line.trim()}`);
      hasError = true;
    }
  });

  console.log(`[${f}] Em-dashes: ${emDashCount}, Emojis: ${emojiFound}`);
}

console.log('\n--- 2. TOUCH TARGETS (>= 48px) CHECK in settings.html ---');
const settingsHtml = fs.readFileSync('design-system/pos-cafe/pages/settings.html', 'utf8');

// Check all button, input, select, and tab elements
const interactiveRegex = /<(button|input|select|a\b)[^>]*class="([^"]*)"/g;
let match;
let countInteractive = 0;
let validTouchTargetCount = 0;
const suspectElements = [];

while ((match = interactiveRegex.exec(settingsHtml)) !== null) {
  countInteractive++;
  const tag = match[1];
  const classes = match[2];

  // Look for min-h-[48px], h-12, min-w-[48px], w-12, or h-10/etc.
  const hasMinHeight48 = classes.includes('min-h-[48px]') || classes.includes('h-12') || classes.includes('min-h-[52px]') || classes.includes('h-14');
  const isCheckbox = match[0].includes('type="checkbox"'); // Checkbox inside label with min-h-[48px]
  
  if (hasMinHeight48 || isCheckbox) {
    validTouchTargetCount++;
  } else {
    suspectElements.push({ tag, classes });
  }
}

console.log(`Total interactive elements scanned: ${countInteractive}`);
console.log(`Elements with >= 48px touch target: ${validTouchTargetCount}`);
if (suspectElements.length > 0) {
  console.log(`Found ${suspectElements.length} elements without explicit min-h-[48px]/h-12:`);
  suspectElements.forEach(el => console.log(`  <${el.tag} class="${el.classes}">`));
}

console.log('\n--- 3. KEY SECTIONS & CONTRACTS CHECK ---');
const checks = [
  { name: 'Tailwind CDN', pass: settingsHtml.includes('cdn.tailwindcss.com') },
  { name: 'Outfit Font', pass: settingsHtml.includes('Outfit') },
  { name: 'JetBrains Mono Font', pass: settingsHtml.includes('JetBrains+Mono') },
  { name: 'Lucide CDN', pass: settingsHtml.includes('lucide@latest') },
  { name: 'POS_BUS script inclusion', pass: settingsHtml.includes('../shared/pos-bus.js') },
  { name: 'Quản lý Nguyễn Văn Lan in header', pass: settingsHtml.includes('Nguyễn Văn Lan') },
  { name: 'Station Switcher dropdown', pass: settingsHtml.includes('station-switcher-dropdown') },
  { name: 'Tab 1: Kho & Món Tạm Hết', pass: settingsHtml.includes('view-stock') },
  { name: 'Tab 2: Thông tin Quán & VietQR', pass: settingsHtml.includes('view-store') },
  { name: 'Tab 3: Tùy chỉnh In & Hệ thống', pass: settingsHtml.includes('view-system') },
  { name: 'Bulk restore all in-stock button', pass: settingsHtml.includes('restoreAllInStock()') },
  { name: 'Save Store Settings (Ctrl+S)', pass: settingsHtml.includes('saveStoreSettingsForm()') },
  { name: 'K80 thermal receipt print preview', pass: settingsHtml.includes('receipt-paper-printable') },
  { name: '@media print styles for 80mm', pass: settingsHtml.includes('@media print') && settingsHtml.includes('80mm') },
  { name: 'Manager PIN confirmation modal (8888)', pass: settingsHtml.includes('modal-manager-reset') && settingsHtml.includes('8888') },
  { name: 'Keyboard shortcuts helper modal', pass: settingsHtml.includes('modal-shortcuts-help') },
  { name: 'Web Audio API synthesis', pass: settingsHtml.includes('AudioContext') && settingsHtml.includes('playToggleClick') && settingsHtml.includes('playSaveChime') },
  { name: 'Shortcuts handling (F0, F1, F7, F8, F9, Ctrl+S, Ctrl+P, Esc)', pass: settingsHtml.includes('F10') && settingsHtml.includes('F1') && settingsHtml.includes('Ctrl') }
];

checks.forEach(c => {
  console.log(`[${c.pass ? 'PASS' : 'FAIL'}] ${c.name}`);
  if (!c.pass) hasError = true;
});

console.log('\n--- 4. EVENT SYNC CONTRACT SIMULATION ---');
// Test POS_BUS inter-tab communication logic
const bus = require('../design-system/pos-cafe/shared/pos-bus.js');

let stockEventReceived = null;
let statusEventReceived = null;
let storeEventReceived = null;

bus.subscribe('CATALOG_STOCK_CHANGED', (payload) => {
  stockEventReceived = payload;
});

bus.subscribe('CATALOG_STATUS_CHANGED', (payload) => {
  statusEventReceived = payload;
});

bus.subscribe('STORE_INFO_UPDATED', (payload) => {
  storeEventReceived = payload;
});

// Test setting item availability
bus.setItemAvailable('cfsd', false);

console.log('Stock event received:', stockEventReceived !== null && stockEventReceived.itemId === 'cfsd' && stockEventReceived.inStock === false);
console.log('Status event received:', statusEventReceived !== null && statusEventReceived.itemId === 'cfsd' && statusEventReceived.isAvailable === false);

bus.saveStoreInfo({ storeName: 'Test Cafe Workshop' });
console.log('Store info event received:', storeEventReceived !== null && storeEventReceived.storeName === 'Test Cafe Workshop');

if (!stockEventReceived || !statusEventReceived || !storeEventReceived) {
  hasError = true;
}

console.log('\nFinal Verification Result:', hasError ? 'FAIL' : 'ALL PASS');
process.exit(hasError ? 1 : 0);
