/**
 * POS Cafe Shared Event Bus & Cross-Tab State Layer
 * Module: pos-bus.js
 * 
 * Provides real-time inter-tab broadcasting via BroadcastChannel ('pos_cafe_bus')
 * and durable cross-tab persistence via localStorage.
 * Attached to window.POS_BUS.
 */

(function () {
  'use strict';

  var CHANNEL_NAME = 'pos_cafe_bus';
  var STORAGE_KEYS = {
    ORDERS: 'POS_ORDERS',
    TABLES: 'POS_TABLES',
    ACTIVE_SHIFT: 'POS_ACTIVE_SHIFT',
    CATALOG_STATUS: 'POS_CATALOG_STATUS',
    CURRENT_STAFF: 'POS_CURRENT_STAFF',
    STORE_INFO: 'POS_STORE_INFO',
    CATALOG_DATA: 'POS_CATALOG_DATA'
  };

  // 1. Initial Mock Seed Data
  var DEFAULT_STORE_INFO = {
    storeName: 'POS Cafe - Specialty Coffee & Tea',
    storeTagline: 'Cà phê & Trà thủ công',
    address: '120 Hoàng Hoa Thám, P. Thụy Khuê, Tây Hồ, Hà Nội',
    hotline: '0987 654 321',
    wifiSSID: 'POS Cafe Free WiFi',
    wifiPass: 'caphedenda',
    vietqr: {
      bankId: 'MB',
      bankName: 'MB Bank (Ngân hàng Quân Đội)',
      accountNo: '0987654321',
      accountName: 'NGUYEN THU NGAN',
      template: 'compact'
    }
  };

  var DEFAULT_CURRENT_STAFF = {
    pin: '1234',
    name: 'Nguyễn Văn Lan',
    role: 'cashier',
    workspace: 'cashier'
  };

  var DEFAULT_ACTIVE_SHIFT = {
    shiftCode: 'Ca #04',
    cashierName: 'Nguyễn Thu Ngân',
    cashierPin: '1234',
    startTime: new Date(Date.now() - 4 * 60 * 60 * 1000).toISOString(),
    openingFloat: 2000000,
    cashSales: 850000,
    transferSales: 620000,
    totalSales: 1470000,
    orderCount: 13,
    cashIn: 0,
    cashOut: 0,
    status: 'open',
    note: 'Ca sáng ngày ' + new Date().toLocaleDateString('vi-VN')
  };

  var DEFAULT_CATALOG_STATUS = {};

  var DEFAULT_CATALOG_DATA = {
    categories: [
      {
        id: 'coffee_vn',
        name: 'Cà phê Việt',
        icon: 'coffee',
        order: 1,
        defaultGroupIds: ['sugar', 'ice']
      },
      {
        id: 'coffee_machine',
        name: 'Cà phê máy',
        icon: 'cup-soda',
        order: 2,
        defaultGroupIds: ['sugar', 'ice']
      },
      {
        id: 'tea',
        name: 'Trà & Macchiato',
        icon: 'leaf',
        order: 3,
        defaultGroupIds: ['sugar', 'ice', 'top_tea']
      },
      {
        id: 'freeze',
        name: 'Đá xay & Sinh tố',
        icon: 'sparkles',
        order: 4,
        defaultGroupIds: ['sugar', 'ice', 'top_tea']
      },
      {
        id: 'bakery',
        name: 'Bánh & Điểm tâm',
        icon: 'croissant',
        order: 5,
        defaultGroupIds: []
      }
    ],
    modifierGroups: [
      {
        id: 'sugar',
        name: 'Mức đường',
        selectionType: 'single',
        minSelect: 1,
        maxSelect: 1,
        options: [
          { id: 'sugar_0', name: '0%', price: 0, isAvailable: true },
          { id: 'sugar_30', name: '30%', price: 0, isAvailable: true },
          { id: 'sugar_50', name: '50%', price: 0, isAvailable: true },
          { id: 'sugar_70', name: '70%', price: 0, isAvailable: true },
          { id: 'sugar_100', name: '100% (Mặc định)', price: 0, isAvailable: true, isDefault: true }
        ]
      },
      {
        id: 'ice',
        name: 'Mức đá',
        selectionType: 'single',
        minSelect: 1,
        maxSelect: 1,
        options: [
          { id: 'ice_hot', name: 'Nóng', price: 0, isAvailable: true },
          { id: 'ice_none', name: 'Không đá', price: 0, isAvailable: true },
          { id: 'ice_less', name: '50% đá', price: 0, isAvailable: true },
          { id: 'ice_100', name: '100% đá (Mặc định)', price: 0, isAvailable: true, isDefault: true },
          { id: 'ice_separate', name: 'Đá riêng', price: 0, isAvailable: true }
        ]
      },
      {
        id: 'top_tea',
        name: 'Topping Trà Trái Cây',
        selectionType: 'multiple',
        minSelect: 0,
        maxSelect: 5,
        options: [
          { id: 'tran_chau_trang', name: 'Trân châu trắng', price: 10000, isAvailable: true },
          { id: 'thach_dao', name: 'Thạch đào', price: 10000, isAvailable: true },
          { id: 'thach_cf', name: 'Thạch cà phê', price: 10000, isAvailable: true },
          { id: 'kem_cheese', name: 'Kem Cheese', price: 12000, isAvailable: true }
        ]
      },
      {
        id: 'top_cheese',
        name: 'Lớp Váng Sữa & Kem Cheese',
        selectionType: 'single',
        minSelect: 0,
        maxSelect: 1,
        options: [
          { id: 'kem_pho_mai', name: 'Kem Phô Mai Macchiato', price: 12000, isAvailable: true },
          { id: 'kem_muoi', name: 'Kem Muối', price: 12000, isAvailable: true }
        ]
      }
    ],
    items: [
      {
        id: 'cfsd',
        name: 'Cà phê sữa đá',
        categoryId: 'coffee_vn',
        categoryName: 'Cà phê Việt',
        basePrice: 35000,
        imageUrl: 'https://placewaifu.com/image/300/200?id=1',
        hasModifiers: true,
        sizes: [
          { id: 'S', name: 'Size S', price: 29000 },
          { id: 'M', name: 'Size M', price: 35000, isDefault: true },
          { id: 'L', name: 'Size L', price: 42000 }
        ],
        defaultSize: 'M',
        acronym: 'cfsd',
        badge: 'Bán chạy',
        description: 'Cà phê pha phin truyền thống hòa quyện cùng sữa đặc béo ngậy',
        assignedModifierGroupIds: ['sugar', 'ice'],
        excludedModifierGroupIds: [],
        isAvailable: true
      },
      {
        id: 'cfdd',
        name: 'Cà phê đen đá',
        categoryId: 'coffee_vn',
        categoryName: 'Cà phê Việt',
        basePrice: 30000,
        imageUrl: 'https://placewaifu.com/image/300/200?id=2',
        hasModifiers: true,
        sizes: [
          { id: 'S', name: 'Size S', price: 25000 },
          { id: 'M', name: 'Size M', price: 30000, isDefault: true },
          { id: 'L', name: 'Size L', price: 37000 }
        ],
        defaultSize: 'M',
        acronym: 'cfdd',
        badge: '',
        description: 'Cà phê phin mộc nguyên chất đậm đà chuẩn vị',
        assignedModifierGroupIds: ['sugar', 'ice'],
        excludedModifierGroupIds: [],
        isAvailable: true
      },
      {
        id: 'bacxiu',
        name: 'Bạc xỉu',
        categoryId: 'coffee_vn',
        categoryName: 'Cà phê Việt',
        basePrice: 39000,
        imageUrl: 'https://placewaifu.com/image/300/200?id=3',
        hasModifiers: true,
        sizes: [
          { id: 'S', name: 'Size S', price: 32000 },
          { id: 'M', name: 'Size M', price: 39000, isDefault: true },
          { id: 'L', name: 'Size L', price: 46000 }
        ],
        defaultSize: 'M',
        acronym: 'bx',
        badge: 'Đặc trưng',
        description: 'Nhiều sữa ít cà phê, béo thơm ngọt dịu dễ uống',
        assignedModifierGroupIds: ['sugar', 'ice'],
        excludedModifierGroupIds: [],
        isAvailable: true
      },
      {
        id: 'cfmuoi',
        name: 'Cà phê muối',
        categoryId: 'coffee_vn',
        categoryName: 'Cà phê Việt',
        basePrice: 39000,
        imageUrl: 'https://placewaifu.com/image/300/200?id=12',
        hasModifiers: true,
        sizes: [
          { id: 'S', name: 'Size S', price: 32000 },
          { id: 'M', name: 'Size M', price: 39000, isDefault: true },
          { id: 'L', name: 'Size L', price: 46000 }
        ],
        defaultSize: 'M',
        acronym: 'cpm',
        badge: 'Món hot',
        description: 'Cà phê phin truyền thống phủ lớp kem muối mặn mà béo ngậy',
        assignedModifierGroupIds: ['sugar', 'ice', 'top_cheese'],
        excludedModifierGroupIds: [],
        isAvailable: true
      },
      {
        id: 'coldbrew_cs',
        name: 'Cold Brew cam sả',
        categoryId: 'coffee_vn',
        categoryName: 'Cà phê Việt',
        basePrice: 45000,
        imageUrl: 'https://placewaifu.com/image/300/200?id=4',
        hasModifiers: true,
        sizes: [
          { id: 'M', name: 'Size M', price: 45000, isDefault: true },
          { id: 'L', name: 'Size L', price: 52000 }
        ],
        defaultSize: 'M',
        acronym: 'cbcs',
        badge: 'Mới',
        description: 'Cà phê ủ lạnh 16 giờ thơm hương vỏ cam tươi và sả thanh mát',
        assignedModifierGroupIds: ['sugar', 'ice'],
        excludedModifierGroupIds: [],
        isAvailable: true
      },
      {
        id: 'espresso',
        name: 'Espresso',
        categoryId: 'coffee_machine',
        categoryName: 'Cà phê máy',
        basePrice: 30000,
        imageUrl: 'https://placewaifu.com/image/300/200?id=5',
        hasModifiers: true,
        sizes: [
          { id: 'Single', name: 'Single Shot', price: 30000, isDefault: true },
          { id: 'Double', name: 'Double Shot', price: 40000 }
        ],
        defaultSize: 'Single',
        acronym: 'esp',
        badge: '',
        description: 'Chiết xuất từ hạt Arabica Cầu Đất nguyên chất đậm đặc',
        assignedModifierGroupIds: ['sugar'],
        excludedModifierGroupIds: [],
        isAvailable: true
      },
      {
        id: 'latte',
        name: 'Latte',
        categoryId: 'coffee_machine',
        categoryName: 'Cà phê máy',
        basePrice: 48000,
        imageUrl: 'https://placewaifu.com/image/300/200?id=6',
        hasModifiers: true,
        sizes: [
          { id: 'S', name: 'Size S', price: 42000 },
          { id: 'M', name: 'Size M', price: 48000, isDefault: true },
          { id: 'L', name: 'Size L', price: 55000 }
        ],
        defaultSize: 'M',
        acronym: 'lt',
        badge: '',
        description: 'Espresso thơm dịu cùng sữa tươi thanh trùng đánh bông bọt mịn',
        assignedModifierGroupIds: ['sugar', 'ice'],
        excludedModifierGroupIds: [],
        isAvailable: true
      },
      {
        id: 'tdcs',
        name: 'Trà đào cam sả',
        categoryId: 'tea',
        categoryName: 'Trà & Macchiato',
        basePrice: 45000,
        imageUrl: 'https://placewaifu.com/image/300/200?id=7',
        hasModifiers: true,
        sizes: [
          { id: 'M', name: 'Size M', price: 45000, isDefault: true },
          { id: 'L', name: 'Size L', price: 52000 }
        ],
        defaultSize: 'M',
        acronym: 'tdcs',
        badge: 'Món hot',
        description: 'Trà đen hảo hạng kết hợp miếng đào giòn ngọt và cam vàng thơm mát',
        assignedModifierGroupIds: ['sugar', 'ice', 'top_tea'],
        excludedModifierGroupIds: [],
        isAvailable: true
      },
      {
        id: 'tvhh',
        name: 'Trà vải hoa hồng',
        categoryId: 'tea',
        categoryName: 'Trà & Macchiato',
        basePrice: 45000,
        imageUrl: 'https://placewaifu.com/image/300/200?id=8',
        hasModifiers: true,
        sizes: [
          { id: 'M', name: 'Size M', price: 45000, isDefault: true },
          { id: 'L', name: 'Size L', price: 52000 }
        ],
        defaultSize: 'M',
        acronym: 'tvhh',
        badge: '',
        description: 'Vị ngọt thanh từ quả vải và hương hoa hồng quyến rũ',
        assignedModifierGroupIds: ['sugar', 'ice', 'top_tea'],
        excludedModifierGroupIds: [],
        isAvailable: true
      },
      {
        id: 'freeze_tx',
        name: 'Freeze trà xanh',
        categoryId: 'freeze',
        categoryName: 'Đá xay & Sinh tố',
        basePrice: 52000,
        imageUrl: 'https://placewaifu.com/image/300/200?id=9',
        hasModifiers: true,
        sizes: [
          { id: 'M', name: 'Size M', price: 52000, isDefault: true },
          { id: 'L', name: 'Size L', price: 59000 }
        ],
        defaultSize: 'M',
        acronym: 'ftx',
        badge: 'Yêu thích',
        description: 'Matcha Uji Nhật Bản xay cùng đá tuyết và thạch trà xanh giòn dai',
        assignedModifierGroupIds: ['sugar', 'ice', 'top_tea'],
        excludedModifierGroupIds: [],
        isAvailable: true
      },
      {
        id: 'croissant_bt',
        name: 'Croissant bơ tỏi',
        categoryId: 'bakery',
        categoryName: 'Bánh & Điểm tâm',
        basePrice: 32000,
        imageUrl: 'https://placewaifu.com/image/300/200?id=10',
        hasModifiers: false,
        sizes: [],
        defaultSize: null,
        acronym: 'cb',
        badge: '',
        description: 'Bánh sừng bò nướng giòn rụm thơm lừng sốt bơ tỏi',
        assignedModifierGroupIds: [],
        excludedModifierGroupIds: [],
        isAvailable: true
      },
      {
        id: 'tiramisu',
        name: 'Tiramisu',
        categoryId: 'bakery',
        categoryName: 'Bánh & Điểm tâm',
        basePrice: 40000,
        imageUrl: 'https://placewaifu.com/image/300/200?id=11',
        hasModifiers: false,
        sizes: [],
        defaultSize: null,
        acronym: 'tms',
        badge: 'Chef Pick',
        description: 'Bánh tráng miệng phong cách Ý phủ bột cacao nguyên chất và kem phô mai',
        assignedModifierGroupIds: [],
        excludedModifierGroupIds: [],
        isAvailable: true
      }
    ]
  };

  var DEFAULT_ORDERS = [
    {
      id: 'ORD-010',
      orderId: 'ORD-010',
      serviceNumber: 10,
      mode: 'takeaway',
      table: null,
      status: 'completed',
      items: [
        {
          id: 'item_10_1',
          name: 'Cold Brew cam sả',
          sizeName: 'Size M',
          quantity: 1,
          unitPrice: 45000,
          lineTotalVND: 45000,
          modifiers: [
            { group: 'sugar', name: 'Đường', value: '100%' },
            { group: 'ice', name: 'Đá', value: '100% đá' }
          ],
          preparationNote: ''
        }
      ],
      subtotalVND: 45000,
      discountVND: 0,
      grandTotalVND: 45000,
      paymentMethod: 'cash',
      tenderedVND: 50000,
      changeDueVND: 5000,
      timestamp: new Date(Date.now() - 65 * 60 * 1000).toISOString(),
      shiftCode: 'Ca #04',
      cashierName: 'Nguyễn Thu Ngân'
    },
    {
      id: 'ORD-011',
      orderId: 'ORD-011',
      serviceNumber: 11,
      mode: 'dinein',
      table: { id: 'T01', name: 'Bàn 01' },
      status: 'completed',
      items: [
        {
          id: 'item_11_1',
          name: 'Cà phê sữa đá',
          sizeName: 'Size M',
          quantity: 1,
          unitPrice: 35000,
          lineTotalVND: 35000,
          modifiers: [
            { group: 'sugar', name: 'Đường', value: '100%' },
            { group: 'ice', name: 'Đá', value: '100% đá' }
          ],
          preparationNote: ''
        },
        {
          id: 'item_11_2',
          name: 'Bạc xỉu',
          sizeName: 'Size M',
          quantity: 1,
          unitPrice: 39000,
          lineTotalVND: 39000,
          modifiers: [
            { group: 'sugar', name: 'Đường', value: '70%' },
            { group: 'ice', name: 'Đá', value: 'Ít đá' }
          ],
          preparationNote: 'Ít sữa đặc'
        }
      ],
      subtotalVND: 74000,
      discountVND: 0,
      grandTotalVND: 74000,
      paymentMethod: 'cash',
      tenderedVND: 100000,
      changeDueVND: 26000,
      timestamp: new Date(Date.now() - 42 * 60 * 1000).toISOString(),
      shiftCode: 'Ca #04',
      cashierName: 'Nguyễn Thu Ngân'
    },
    {
      id: 'ORD-012',
      orderId: 'ORD-012',
      serviceNumber: 12,
      mode: 'takeaway',
      table: null,
      status: 'preparing',
      items: [
        {
          id: 'item_12_1',
          name: 'Trà đào cam sả',
          sizeName: 'Size L',
          quantity: 2,
          unitPrice: 49000,
          lineTotalVND: 98000,
          modifiers: [
            { group: 'sugar', name: 'Đường', value: '70%' },
            { group: 'ice', name: 'Đá', value: '100% đá' }
          ],
          preparationNote: 'Nhiều đào miếng'
        }
      ],
      subtotalVND: 98000,
      discountVND: 0,
      grandTotalVND: 98000,
      paymentMethod: 'qr',
      tenderedVND: 98000,
      changeDueVND: 0,
      timestamp: new Date(Date.now() - 14 * 60 * 1000).toISOString(),
      shiftCode: 'Ca #04',
      cashierName: 'Nguyễn Thu Ngân'
    },
    {
      id: 'ORD-013',
      orderId: 'ORD-013',
      serviceNumber: 13,
      mode: 'dinein',
      table: { id: 'T04', name: 'Bàn 04' },
      status: 'ready',
      items: [
        {
          id: 'item_13_1',
          name: 'Freeze trà xanh',
          sizeName: 'Size M',
          quantity: 1,
          unitPrice: 52000,
          lineTotalVND: 52000,
          modifiers: [
            { group: 'sugar', name: 'Đường', value: '100%' },
            { group: 'ice', name: 'Đá', value: '100% đá' }
          ],
          preparationNote: 'Thêm thạch'
        },
        {
          id: 'item_13_2',
          name: 'Tiramisu',
          sizeName: '',
          quantity: 1,
          unitPrice: 40000,
          lineTotalVND: 40000,
          modifiers: [],
          preparationNote: ''
        }
      ],
      subtotalVND: 92000,
      discountVND: 0,
      grandTotalVND: 92000,
      paymentMethod: 'cash',
      tenderedVND: 100000,
      changeDueVND: 8000,
      timestamp: new Date(Date.now() - 5 * 60 * 1000).toISOString(),
      shiftCode: 'Ca #04',
      cashierName: 'Nguyễn Thu Ngân'
    }
  ];

  // 2. Storage Helpers
  function readStorage(key, fallback) {
    try {
      if (typeof window === 'undefined' || !window.localStorage) {
        return fallback;
      }
      var raw = window.localStorage.getItem(key);
      if (!raw) return fallback;
      return JSON.parse(raw);
    } catch (err) {
      console.warn('[POS_BUS] Error reading localStorage key:', key, err);
      return fallback;
    }
  }

  function writeStorage(key, value) {
    try {
      if (typeof window === 'undefined' || !window.localStorage) {
        return;
      }
      window.localStorage.setItem(key, JSON.stringify(value));
    } catch (err) {
      console.error('[POS_BUS] Error writing localStorage key:', key, err);
    }
  }

  // 3. Ensure Seeding
  function ensureSeeded() {
    if (typeof window === 'undefined' || !window.localStorage) return;

    if (!window.localStorage.getItem(STORAGE_KEYS.STORE_INFO)) {
      writeStorage(STORAGE_KEYS.STORE_INFO, DEFAULT_STORE_INFO);
    }
    if (!window.localStorage.getItem(STORAGE_KEYS.CURRENT_STAFF)) {
      writeStorage(STORAGE_KEYS.CURRENT_STAFF, DEFAULT_CURRENT_STAFF);
    }
    if (!window.localStorage.getItem(STORAGE_KEYS.ACTIVE_SHIFT)) {
      writeStorage(STORAGE_KEYS.ACTIVE_SHIFT, DEFAULT_ACTIVE_SHIFT);
    }
    if (!window.localStorage.getItem(STORAGE_KEYS.CATALOG_STATUS)) {
      writeStorage(STORAGE_KEYS.CATALOG_STATUS, DEFAULT_CATALOG_STATUS);
    }
    if (!window.localStorage.getItem(STORAGE_KEYS.ORDERS)) {
      writeStorage(STORAGE_KEYS.ORDERS, DEFAULT_ORDERS);
    }
    if (!window.localStorage.getItem(STORAGE_KEYS.CATALOG_DATA)) {
      writeStorage(STORAGE_KEYS.CATALOG_DATA, DEFAULT_CATALOG_DATA);
    }
  }

  ensureSeeded();

  // 4. BroadcastChannel & Subscribers
  var channel = null;
  if (typeof window !== 'undefined' && typeof window.BroadcastChannel === 'function') {
    try {
      channel = new window.BroadcastChannel(CHANNEL_NAME);
    } catch (e) {
      console.warn('[POS_BUS] BroadcastChannel init failed, fallback to storage events', e);
    }
  }

  var subscriberIdCounter = 1;
  var subscribers = [];

  function notifySubscribers(eventType, payload, rawMessage) {
    var count = subscribers.length;
    for (var i = 0; i < count; i++) {
      var sub = subscribers[i];
      if (sub && (sub.eventType === '*' || sub.eventType === eventType)) {
        try {
          sub.callback(payload, rawMessage);
        } catch (callbackErr) {
          console.error('[POS_BUS] Error in subscriber callback for event:', eventType, callbackErr);
        }
      }
    }
  }

  if (channel) {
    channel.onmessage = function (event) {
      if (!event || !event.data) return;
      var data = event.data;
      notifySubscribers(data.type, data.payload, data);
    };
  }

  // Fallback: cross-tab storage event listener
  if (typeof window !== 'undefined' && window.addEventListener) {
    window.addEventListener('storage', function (e) {
      if (!e.key) return;
      if (e.key === STORAGE_KEYS.ORDERS) {
        notifySubscribers('STORAGE_ORDERS_SYNC', readStorage(STORAGE_KEYS.ORDERS, []), { type: 'STORAGE_ORDERS_SYNC', payload: readStorage(STORAGE_KEYS.ORDERS, []), timestamp: Date.now() });
      } else if (e.key === STORAGE_KEYS.ACTIVE_SHIFT) {
        notifySubscribers('STORAGE_SHIFT_SYNC', readStorage(STORAGE_KEYS.ACTIVE_SHIFT, null), { type: 'STORAGE_SHIFT_SYNC', payload: readStorage(STORAGE_KEYS.ACTIVE_SHIFT, null), timestamp: Date.now() });
      } else if (e.key === STORAGE_KEYS.CATALOG_STATUS) {
        notifySubscribers('STORAGE_CATALOG_SYNC', readStorage(STORAGE_KEYS.CATALOG_STATUS, {}), { type: 'STORAGE_CATALOG_SYNC', payload: readStorage(STORAGE_KEYS.CATALOG_STATUS, {}), timestamp: Date.now() });
      } else if (e.key === STORAGE_KEYS.CURRENT_STAFF) {
        notifySubscribers('STORAGE_STAFF_SYNC', readStorage(STORAGE_KEYS.CURRENT_STAFF, null), { type: 'STORAGE_STAFF_SYNC', payload: readStorage(STORAGE_KEYS.CURRENT_STAFF, null), timestamp: Date.now() });
      } else if (e.key === STORAGE_KEYS.STORE_INFO) {
        notifySubscribers('STORAGE_STORE_INFO_SYNC', readStorage(STORAGE_KEYS.STORE_INFO, null), { type: 'STORAGE_STORE_INFO_SYNC', payload: readStorage(STORAGE_KEYS.STORE_INFO, null), timestamp: Date.now() });
      } else if (e.key === STORAGE_KEYS.CATALOG_DATA) {
        var catalogData = readStorage(STORAGE_KEYS.CATALOG_DATA, null);
        notifySubscribers('STORAGE_CATALOG_DATA_SYNC', catalogData, { type: 'STORAGE_CATALOG_DATA_SYNC', payload: catalogData, timestamp: Date.now() });
        notifySubscribers('CATALOG_ITEMS_UPDATED', { catalogData: catalogData, action: 'storage_sync' }, { type: 'CATALOG_ITEMS_UPDATED', payload: { catalogData: catalogData, action: 'storage_sync' }, timestamp: Date.now() });
      }
    });
  }

  // 5. Public API Object
  var bus = {
    CHANNEL_NAME: CHANNEL_NAME,
    STORAGE_KEYS: STORAGE_KEYS,

    /**
     * Broadcasts an event to all tabs and dispatches locally.
     * Also saves to localStorage where appropriate.
     */
    publish: function (eventType, payload) {
      var message = {
        type: eventType,
        payload: payload,
        timestamp: Date.now()
      };

      // Durable storage side-effects for known business events
      if (eventType === 'NEW_ORDER' && payload) {
        this.saveOrder(payload, false); // false = do not re-publish NEW_ORDER inside saveOrder
      } else if (eventType === 'ORDER_STATUS_CHANGED' && payload && payload.orderId && payload.status) {
        this.updateOrderStatus(payload.orderId, payload.status, false);
      } else if (eventType === 'SHIFT_UPDATED' && payload) {
        writeStorage(STORAGE_KEYS.ACTIVE_SHIFT, payload);
      } else if ((eventType === 'CATALOG_STATUS_CHANGED' || eventType === 'CATALOG_STOCK_CHANGED') && payload && payload.catalogStatus) {
        writeStorage(STORAGE_KEYS.CATALOG_STATUS, payload.catalogStatus);
      } else if (eventType === 'STAFF_UPDATED' && payload) {
        writeStorage(STORAGE_KEYS.CURRENT_STAFF, payload);
      } else if (eventType === 'STORE_INFO_UPDATED' && payload) {
        writeStorage(STORAGE_KEYS.STORE_INFO, payload);
      } else if (eventType === 'CATALOG_ITEMS_UPDATED' && payload && payload.catalogData) {
        writeStorage(STORAGE_KEYS.CATALOG_DATA, payload.catalogData);
      }

      // Broadcast across tabs
      if (channel) {
        try {
          channel.postMessage(message);
        } catch (postErr) {
          console.error('[POS_BUS] BroadcastChannel postMessage error:', postErr);
        }
      }

      // Dispatch to local tab subscribers
      notifySubscribers(eventType, payload, message);
      return message;
    },

    /**
     * Registers a listener callback for a specific event or '*' for all events.
     * Returns an unsubscribe function.
     */
    subscribe: function (eventType, callback) {
      if (typeof callback !== 'function') {
        throw new Error('[POS_BUS] Callback must be a function');
      }

      var id = subscriberIdCounter++;
      var sub = { id: id, eventType: eventType, callback: callback };
      subscribers.push(sub);

      return function unsubscribe() {
        subscribers = subscribers.filter(function (s) {
          return s.id !== id;
        });
      };
    },

    /**
     * Gets all active and historical orders from localStorage.
     */
    getOrders: function () {
      var orders = readStorage(STORAGE_KEYS.ORDERS, null);
      if (!orders || !Array.isArray(orders)) {
        writeStorage(STORAGE_KEYS.ORDERS, DEFAULT_ORDERS);
        return JSON.parse(JSON.stringify(DEFAULT_ORDERS));
      }
      return orders;
    },

    /**
     * Saves or updates an order in localStorage.
     */
    saveOrder: function (order, shouldPublish) {
      if (!order) return null;
      if (typeof shouldPublish === 'undefined') shouldPublish = true;

      var orders = this.getOrders();
      var id = order.id || order.orderId || ('ORD-' + String(order.serviceNumber || (orders.length + 1)).padStart(3, '0'));
      var cloned = JSON.parse(JSON.stringify(order));
      cloned.id = id;
      cloned.orderId = id;
      cloned.timestamp = cloned.timestamp || new Date().toISOString();
      cloned.status = cloned.status || 'preparing';

      var existingIdx = -1;
      for (var i = 0; i < orders.length; i++) {
        if (orders[i].id === id || orders[i].orderId === id || (order.serviceNumber && orders[i].serviceNumber === order.serviceNumber)) {
          existingIdx = i;
          break;
        }
      }

      if (existingIdx >= 0) {
        orders[existingIdx] = Object.assign({}, orders[existingIdx], cloned, { updatedAt: new Date().toISOString() });
        cloned = orders[existingIdx];
      } else {
        orders.unshift(cloned);
      }

      writeStorage(STORAGE_KEYS.ORDERS, orders);

      if (shouldPublish) {
        this.publish('ORDER_SAVED', cloned);
      }

      return cloned;
    },

    /**
     * Updates an order status (e.g. 'preparing', 'ready', 'completed', 'cancelled')
     */
    updateOrderStatus: function (orderId, status, shouldPublish) {
      if (!orderId || !status) return null;
      if (typeof shouldPublish === 'undefined') shouldPublish = true;

      var orders = this.getOrders();
      var updatedOrder = null;

      for (var i = 0; i < orders.length; i++) {
        if (orders[i].id === orderId || orders[i].orderId === orderId || String(orders[i].serviceNumber) === String(orderId)) {
          orders[i].status = status;
          orders[i].updatedAt = new Date().toISOString();
          updatedOrder = orders[i];
          break;
        }
      }

      if (updatedOrder) {
        writeStorage(STORAGE_KEYS.ORDERS, orders);
        if (shouldPublish) {
          this.publish('ORDER_STATUS_CHANGED', { orderId: orderId, status: status, order: updatedOrder });
        }
      }

      return updatedOrder;
    },

    /**
     * Gets current active shift data.
     */
    getActiveShift: function () {
      var shift = readStorage(STORAGE_KEYS.ACTIVE_SHIFT, null);
      if (!shift) {
        writeStorage(STORAGE_KEYS.ACTIVE_SHIFT, DEFAULT_ACTIVE_SHIFT);
        return JSON.parse(JSON.stringify(DEFAULT_ACTIVE_SHIFT));
      }
      return shift;
    },

    /**
     * Saves active shift data and broadcasts SHIFT_UPDATED.
     */
    saveActiveShift: function (shift) {
      if (!shift) return null;
      writeStorage(STORAGE_KEYS.ACTIVE_SHIFT, shift);
      this.publish('SHIFT_UPDATED', shift);
      return shift;
    },

    /**
     * Gets catalog item/topping availability status map.
     */
    getCatalogStatus: function () {
      var status = readStorage(STORAGE_KEYS.CATALOG_STATUS, null);
      if (!status || typeof status !== 'object') {
        writeStorage(STORAGE_KEYS.CATALOG_STATUS, DEFAULT_CATALOG_STATUS);
        return {};
      }
      return status;
    },

    /**
     * Sets item/topping availability and broadcasts CATALOG_STATUS_CHANGED.
     */
    setItemAvailable: function (itemId, isAvailable) {
      if (!itemId) return null;
      var status = this.getCatalogStatus();
      var inStockVal = !!isAvailable;
      status[itemId] = { inStock: inStockVal, updatedAt: new Date().toISOString() };
      writeStorage(STORAGE_KEYS.CATALOG_STATUS, status);
      this.publish('CATALOG_STOCK_CHANGED', { itemId: itemId, inStock: inStockVal, catalogStatus: status });
      this.publish('CATALOG_STATUS_CHANGED', { itemId: itemId, isAvailable: inStockVal, inStock: inStockVal, catalogStatus: status });
      return status;
    },

    /**
     * Checks if item is available.
     */
    isItemAvailable: function (itemId) {
      if (!itemId) return true;
      var status = this.getCatalogStatus();
      var entry = status[itemId];
      if (entry === undefined || entry === null) return true;
      if (typeof entry === 'boolean') return entry;
      if (typeof entry === 'object' && typeof entry.inStock === 'boolean') return entry.inStock;
      return true;
    },

    /**
     * Records cash sales transaction into active shift.
     */
    recordCashTransaction: function (amount) {
      var shift = this.getActiveShift();
      if (shift && typeof amount === 'number') {
        shift.cashSales = (shift.cashSales || 0) + amount;
        shift.totalSales = (shift.totalSales || 0) + amount;
        return this.saveActiveShift(shift);
      }
      return shift;
    },

    /**
     * Updates table status and broadcasts TABLE_STATUS_CHANGED.
     */
    setTableStatus: function (tableId, status, meta) {
      if (!tableId) return null;
      var tables = readStorage(STORAGE_KEYS.TABLES, []);
      var updatedTable = null;
      for (var i = 0; i < tables.length; i++) {
        if (tables[i].id === tableId) {
          tables[i].status = status;
          if (meta) {
            tables[i].activeSession = Object.assign({}, tables[i].activeSession || {}, meta);
          }
          updatedTable = tables[i];
          break;
        }
      }
      if (tables.length > 0) {
        writeStorage(STORAGE_KEYS.TABLES, tables);
      }
      var payload = { tableId: tableId, status: status, meta: meta, table: updatedTable };
      this.publish('TABLE_STATUS_CHANGED', payload);
      return payload;
    },

    /**
     * Gets current authenticated staff info.
     */
    getCurrentStaff: function () {
      var staff = readStorage(STORAGE_KEYS.CURRENT_STAFF, null);
      if (!staff) {
        writeStorage(STORAGE_KEYS.CURRENT_STAFF, DEFAULT_CURRENT_STAFF);
        return JSON.parse(JSON.stringify(DEFAULT_CURRENT_STAFF));
      }
      return staff;
    },

    /**
     * Sets authenticated staff info and broadcasts STAFF_UPDATED.
     */
    setCurrentStaff: function (staff) {
      if (!staff) return null;
      writeStorage(STORAGE_KEYS.CURRENT_STAFF, staff);
      this.publish('STAFF_UPDATED', staff);
      return staff;
    },

    /**
     * Gets store information and VietQR details.
     */
    getStoreInfo: function () {
      var info = readStorage(STORAGE_KEYS.STORE_INFO, null);
      if (!info) {
        writeStorage(STORAGE_KEYS.STORE_INFO, DEFAULT_STORE_INFO);
        return JSON.parse(JSON.stringify(DEFAULT_STORE_INFO));
      }
      return info;
    },

    /**
     * Saves store information and broadcasts STORE_INFO_UPDATED.
     */
    saveStoreInfo: function (info) {
      if (!info) return null;
      writeStorage(STORAGE_KEYS.STORE_INFO, info);
      this.publish('STORE_INFO_UPDATED', info);
      return info;
    },

    /**
     * Gets catalog data (categories, items, modifierGroups).
     * Seeds DEFAULT_CATALOG_DATA if empty.
     */
    getCatalogData: function () {
      var data = readStorage(STORAGE_KEYS.CATALOG_DATA, null);
      if (!data || !Array.isArray(data.items) || !Array.isArray(data.categories) || !Array.isArray(data.modifierGroups)) {
        writeStorage(STORAGE_KEYS.CATALOG_DATA, DEFAULT_CATALOG_DATA);
        return JSON.parse(JSON.stringify(DEFAULT_CATALOG_DATA));
      }
      return data;
    },

    /**
     * Saves entire catalog data and broadcasts CATALOG_ITEMS_UPDATED.
     */
    saveCatalogData: function (catalogData, shouldPublish) {
      if (!catalogData) return null;
      if (typeof shouldPublish === 'undefined') shouldPublish = true;
      writeStorage(STORAGE_KEYS.CATALOG_DATA, catalogData);
      if (shouldPublish) {
        this.publish('CATALOG_ITEMS_UPDATED', { catalogData: catalogData, action: 'save_all' });
      }
      return catalogData;
    },

    /**
     * Saves or updates a menu item in catalog.
     * Automatically recalculates basePrice from default size if sizes are defined.
     */
    saveMenuItem: function (item, shouldPublish) {
      if (!item) return null;
      if (typeof shouldPublish === 'undefined') shouldPublish = true;

      var catalog = this.getCatalogData();
      var cloned = JSON.parse(JSON.stringify(item));
      if (!cloned.id) {
        cloned.id = 'item_' + Date.now();
      }

      // Auto recalculate basePrice from default size if sizes present
      if (Array.isArray(cloned.sizes) && cloned.sizes.length > 0) {
        var defSize = cloned.sizes.find(function (s) { return s.isDefault || s.id === cloned.defaultSize; }) || cloned.sizes[0];
        if (defSize && typeof defSize.price === 'number') {
          cloned.basePrice = defSize.price;
          cloned.defaultSize = defSize.id;
        }
      }

      cloned.assignedModifierGroupIds = cloned.assignedModifierGroupIds || [];
      cloned.excludedModifierGroupIds = cloned.excludedModifierGroupIds || [];
      if (typeof cloned.isAvailable === 'undefined') {
        cloned.isAvailable = true;
      }

      var existingIdx = -1;
      for (var i = 0; i < catalog.items.length; i++) {
        if (catalog.items[i].id === cloned.id) {
          existingIdx = i;
          break;
        }
      }

      if (existingIdx >= 0) {
        catalog.items[existingIdx] = Object.assign({}, catalog.items[existingIdx], cloned);
        cloned = catalog.items[existingIdx];
      } else {
        catalog.items.push(cloned);
      }

      this.saveCatalogData(catalog, false);

      if (shouldPublish) {
        this.publish('CATALOG_ITEMS_UPDATED', {
          catalogData: catalog,
          action: 'save_item',
          targetId: cloned.id,
          item: cloned
        });
      }

      return cloned;
    },

    /**
     * Deletes a menu item from catalog.
     */
    deleteMenuItem: function (itemId, shouldPublish) {
      if (!itemId) return false;
      if (typeof shouldPublish === 'undefined') shouldPublish = true;

      var catalog = this.getCatalogData();
      var initialLen = catalog.items.length;
      catalog.items = catalog.items.filter(function (it) {
        return it.id !== itemId;
      });

      var deleted = catalog.items.length < initialLen;
      if (deleted) {
        this.saveCatalogData(catalog, false);
        if (shouldPublish) {
          this.publish('CATALOG_ITEMS_UPDATED', {
            catalogData: catalog,
            action: 'delete_item',
            targetId: itemId
          });
        }
      }
      return deleted;
    },

    /**
     * Saves or updates a category in catalog.
     */
    saveCategory: function (category, shouldPublish) {
      if (!category) return null;
      if (typeof shouldPublish === 'undefined') shouldPublish = true;

      var catalog = this.getCatalogData();
      var cloned = JSON.parse(JSON.stringify(category));
      if (!cloned.id) {
        cloned.id = 'cat_' + Date.now();
      }
      cloned.defaultGroupIds = cloned.defaultGroupIds || [];
      if (typeof cloned.order === 'undefined') {
        cloned.order = catalog.categories.length + 1;
      }

      var existingIdx = -1;
      for (var i = 0; i < catalog.categories.length; i++) {
        if (catalog.categories[i].id === cloned.id) {
          existingIdx = i;
          break;
        }
      }

      if (existingIdx >= 0) {
        catalog.categories[existingIdx] = Object.assign({}, catalog.categories[existingIdx], cloned);
        cloned = catalog.categories[existingIdx];
      } else {
        catalog.categories.push(cloned);
      }

      this.saveCatalogData(catalog, false);

      if (shouldPublish) {
        this.publish('CATALOG_ITEMS_UPDATED', {
          catalogData: catalog,
          action: 'save_category',
          targetId: cloned.id,
          category: cloned
        });
      }

      return cloned;
    },

    /**
     * Deletes a category from catalog.
     */
    deleteCategory: function (categoryId, shouldPublish) {
      if (!categoryId) return false;
      if (typeof shouldPublish === 'undefined') shouldPublish = true;

      var catalog = this.getCatalogData();
      var initialLen = catalog.categories.length;
      catalog.categories = catalog.categories.filter(function (c) {
        return c.id !== categoryId;
      });

      var deleted = catalog.categories.length < initialLen;
      if (deleted) {
        this.saveCatalogData(catalog, false);
        if (shouldPublish) {
          this.publish('CATALOG_ITEMS_UPDATED', {
            catalogData: catalog,
            action: 'delete_category',
            targetId: categoryId
          });
        }
      }
      return deleted;
    },

    /**
     * Saves or updates a modifier group in catalog.
     */
    saveModifierGroup: function (group, shouldPublish) {
      if (!group) return null;
      if (typeof shouldPublish === 'undefined') shouldPublish = true;

      var catalog = this.getCatalogData();
      var cloned = JSON.parse(JSON.stringify(group));
      if (!cloned.id) {
        cloned.id = 'mod_' + Date.now();
      }
      cloned.options = cloned.options || [];

      var existingIdx = -1;
      for (var i = 0; i < catalog.modifierGroups.length; i++) {
        if (catalog.modifierGroups[i].id === cloned.id) {
          existingIdx = i;
          break;
        }
      }

      if (existingIdx >= 0) {
        catalog.modifierGroups[existingIdx] = Object.assign({}, catalog.modifierGroups[existingIdx], cloned);
        cloned = catalog.modifierGroups[existingIdx];
      } else {
        catalog.modifierGroups.push(cloned);
      }

      this.saveCatalogData(catalog, false);

      if (shouldPublish) {
        this.publish('CATALOG_ITEMS_UPDATED', {
          catalogData: catalog,
          action: 'save_modifier_group',
          targetId: cloned.id,
          group: cloned
        });
      }

      return cloned;
    },

    /**
     * Deletes a modifier group from catalog and unlinks from all items/categories.
     */
    deleteModifierGroup: function (groupId, shouldPublish) {
      if (!groupId) return false;
      if (typeof shouldPublish === 'undefined') shouldPublish = true;

      var catalog = this.getCatalogData();
      var initialLen = catalog.modifierGroups.length;
      catalog.modifierGroups = catalog.modifierGroups.filter(function (g) {
        return g.id !== groupId;
      });

      var deleted = catalog.modifierGroups.length < initialLen;
      if (deleted) {
        // Unlink from items
        catalog.items.forEach(function (item) {
          if (Array.isArray(item.assignedModifierGroupIds)) {
            item.assignedModifierGroupIds = item.assignedModifierGroupIds.filter(function (id) {
              return id !== groupId;
            });
          }
          if (Array.isArray(item.excludedModifierGroupIds)) {
            item.excludedModifierGroupIds = item.excludedModifierGroupIds.filter(function (id) {
              return id !== groupId;
            });
          }
        });
        // Unlink from categories
        catalog.categories.forEach(function (cat) {
          if (Array.isArray(cat.defaultGroupIds)) {
            cat.defaultGroupIds = cat.defaultGroupIds.filter(function (id) {
              return id !== groupId;
            });
          }
        });

        this.saveCatalogData(catalog, false);

        if (shouldPublish) {
          this.publish('CATALOG_ITEMS_UPDATED', {
            catalogData: catalog,
            action: 'delete_modifier_group',
            targetId: groupId
          });
        }
      }
      return deleted;
    },

    /**
     * Batch assigns modifier group to target items, and removes it from non-target items.
     */
    batchAssignModifierGroup: function (groupId, itemIds, shouldPublish) {
      if (!groupId) return null;
      itemIds = Array.isArray(itemIds) ? itemIds : [];
      if (typeof shouldPublish === 'undefined') shouldPublish = true;

      var catalog = this.getCatalogData();
      var targetSet = {};
      for (var i = 0; i < itemIds.length; i++) {
        targetSet[itemIds[i]] = true;
      }

      catalog.items.forEach(function (item) {
        item.assignedModifierGroupIds = item.assignedModifierGroupIds || [];
        var idx = item.assignedModifierGroupIds.indexOf(groupId);
        if (targetSet[item.id]) {
          if (idx === -1) {
            item.assignedModifierGroupIds.push(groupId);
          }
        } else {
          if (idx !== -1) {
            item.assignedModifierGroupIds.splice(idx, 1);
          }
        }
      });

      this.saveCatalogData(catalog, false);

      if (shouldPublish) {
        this.publish('CATALOG_ITEMS_UPDATED', {
          catalogData: catalog,
          action: 'batch_assign_modifier_group',
          targetId: groupId,
          itemIds: itemIds
        });
      }

      return catalog;
    }
  };

  // Expose globally
  if (typeof window !== 'undefined') {
    window.POS_BUS = bus;
  }
  if (typeof module !== 'undefined' && module.exports) {
    module.exports = bus;
  }
})();
