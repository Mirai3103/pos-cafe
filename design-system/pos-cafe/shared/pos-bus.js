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
    ACTIVE_SHIFT: 'POS_ACTIVE_SHIFT',
    CATALOG_STATUS: 'POS_CATALOG_STATUS',
    CURRENT_STAFF: 'POS_CURRENT_STAFF',
    STORE_INFO: 'POS_STORE_INFO'
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
