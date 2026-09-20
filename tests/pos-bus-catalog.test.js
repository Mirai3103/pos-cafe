const assert = require('assert');
const path = require('path');

// Mock localStorage and window
const storage = {};
global.window = {
  localStorage: {
    getItem: (k) => (Object.prototype.hasOwnProperty.call(storage, k) ? storage[k] : null),
    setItem: (k, v) => { storage[k] = String(v); },
    removeItem: (k) => { delete storage[k]; },
    clear: () => {
      for (const k in storage) delete storage[k];
    }
  },
  addEventListener: () => {}
};

// Load pos-bus.js
require(path.join(__dirname, '../design-system/pos-cafe/shared/pos-bus.js'));
const bus = global.window.POS_BUS;

console.log('--- Testing POS_BUS Catalog API ---');

// Test 1: Seed data initialization and getCatalogData()
const catalog = bus.getCatalogData();
assert(catalog && Array.isArray(catalog.categories), 'Catalog must contain categories array');
assert(Array.isArray(catalog.items), 'Catalog must contain items array');
assert(Array.isArray(catalog.modifierGroups), 'Catalog must contain modifierGroups array');
assert(catalog.categories.length >= 5, 'Catalog should have at least 5 default categories');
assert(catalog.items.length >= 10, 'Catalog should have at least 10 default items');
assert(catalog.modifierGroups.length >= 4, 'Catalog should have at least 4 default modifier groups');
console.log('✓ Test 1 Passed: Seed catalog initialized with categories, items, and modifier groups');

// Test 2: Save new menu item & auto basePrice recalculation
const newItem = {
  id: 'tra_dao_cam_sa_test',
  name: 'Trà đào cam sả Test',
  categoryId: 'tea',
  categoryName: 'Trà & Macchiato',
  acronym: 'tdcst',
  sizes: [
    { id: 'M', name: 'Size M', price: 45000, isDefault: true },
    { id: 'L', name: 'Size L', price: 52000 }
  ],
  defaultSize: 'M',
  assignedModifierGroupIds: ['top_tea']
};
const savedItem = bus.saveMenuItem(newItem);
assert.strictEqual(savedItem.id, newItem.id);
assert.strictEqual(savedItem.basePrice, 45000, 'basePrice should be set from default size M');

const fetched = bus.getCatalogData().items.find(i => i.id === newItem.id);
assert(fetched && fetched.name === newItem.name, 'Item must be saved and retrievable');
assert.strictEqual(fetched.basePrice, 45000);
console.log('✓ Test 2 Passed: Menu item saved and basePrice calculated successfully');

// Test 3: Batch assign modifier group
bus.batchAssignModifierGroup('top_cheese', [newItem.id]);
const fetchedAfterBatch = bus.getCatalogData().items.find(i => i.id === newItem.id);
assert(fetchedAfterBatch.assignedModifierGroupIds.includes('top_cheese'), 'Modifier group must be batch assigned to target item');

// Check that an item not in the batch list does not have top_cheese
const otherItem = bus.getCatalogData().items.find(i => i.id !== newItem.id);
if (otherItem) {
  assert(!otherItem.assignedModifierGroupIds.includes('top_cheese'), 'Modifier group must not be assigned to non-target item');
}
console.log('✓ Test 3 Passed: Modifier group batch assigned successfully');

// Test 4: Category CRUD
const newCat = {
  id: 'smoothie',
  name: 'Sinh tố tươi',
  icon: 'blend',
  order: 6,
  defaultGroupIds: ['sugar', 'ice']
};
bus.saveCategory(newCat);
let catList = bus.getCatalogData().categories;
assert(catList.some(c => c.id === 'smoothie'), 'Category should be saved');

bus.deleteCategory('smoothie');
catList = bus.getCatalogData().categories;
assert(!catList.some(c => c.id === 'smoothie'), 'Category should be deleted');
console.log('✓ Test 4 Passed: Category saved and deleted cleanly');

// Test 5: Modifier Group CRUD and unlinking
const testGroup = {
  id: 'test_group',
  name: 'Test Modifier Group',
  selectionType: 'multiple',
  minSelect: 0,
  maxSelect: 3,
  options: [
    { id: 'opt_1', name: 'Option 1', price: 5000, isAvailable: true }
  ]
};
bus.saveModifierGroup(testGroup);
let modGroups = bus.getCatalogData().modifierGroups;
assert(modGroups.some(g => g.id === 'test_group'), 'Modifier group should be saved');

// Assign test_group to newItem
bus.batchAssignModifierGroup('test_group', [newItem.id]);
assert(bus.getCatalogData().items.find(i => i.id === newItem.id).assignedModifierGroupIds.includes('test_group'));

// Delete test_group and verify unlinking
bus.deleteModifierGroup('test_group');
modGroups = bus.getCatalogData().modifierGroups;
assert(!modGroups.some(g => g.id === 'test_group'), 'Modifier group should be deleted');
const itemAfterGroupDelete = bus.getCatalogData().items.find(i => i.id === newItem.id);
assert(!itemAfterGroupDelete.assignedModifierGroupIds.includes('test_group'), 'Deleted group must be unlinked from items');
console.log('✓ Test 5 Passed: Modifier group CRUD and unlinking verified');

// Test 6: Delete Menu Item
bus.deleteMenuItem(newItem.id);
assert(!bus.getCatalogData().items.some(i => i.id === newItem.id), 'Item must be deleted');
console.log('✓ Test 6 Passed: Menu item deleted cleanly');

// Test 7: Event Bus subscription for CATALOG_ITEMS_UPDATED
let eventFired = false;
let receivedPayload = null;
const unsub = bus.subscribe('CATALOG_ITEMS_UPDATED', (payload) => {
  eventFired = true;
  receivedPayload = payload;
});

bus.saveMenuItem({
  id: 'temp_event_item',
  name: 'Event Test Item',
  categoryId: 'coffee_vn',
  basePrice: 20000
});

assert(eventFired, 'CATALOG_ITEMS_UPDATED event should be published on saveMenuItem');
assert(receivedPayload && receivedPayload.catalogData, 'Event payload should include catalogData');
unsub();
bus.deleteMenuItem('temp_event_item');
console.log('✓ Test 7 Passed: CATALOG_ITEMS_UPDATED event bus verified');

console.log('\nAll Catalog Bus tests passed successfully!');
