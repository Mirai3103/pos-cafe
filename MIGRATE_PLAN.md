# POS Cafe Backend Migration Plan: TypeScript to Golang

> **Source Project:** `/home/laffy/cafe-pos/src` (Fullstack TS: React 19 + tRPC + Drizzle ORM + PostgreSQL)  
> **Target Project:** `/home/laffy/Desktop/go-vertical-slice-template-main/pos-cafe` (Go 1.22+ + Echo v4 + SQLite + sqlc + Watermill)  
> **Primary Goal:** Eliminate lag on low-spec POS terminals (Celeron, 2–4GB RAM), reduce RAM usage from ~500MB to < 30MB, achieve instant boot (< 20ms), and support 100% offline-first reliability.

---

## 🏗️ 1. Architecture Mapping (TypeScript -> Go Vertical Slice)

The source project is already cleanly structured around domain boundaries. We map each TypeScript domain module 1:1 to an independent Go Vertical Slice:

| TypeScript Module (`cafe-pos/src`) | Go Vertical Slice (`pos-cafe/internal`) | Primary Concerns / Use Cases |
| :--- | :--- | :--- |
| `src/auth/` | `internal/auth/` | Staff identities, PIN authentication, Role-Based Access Control, Sessions |
| `src/catalog/` | `internal/catalog/` | Categories, Items, Sizes, Modifiers/Toppings, Repricing, Availability |
| `src/tables/` | `internal/tables/` | Table layout, Table availability, Dining session mapping |
| `src/sales-shift/` | `internal/shift/` | Cash drawer, Open shift (float), Cash movements, Close shift & reconciliation |
| `src/sales/` | `internal/sales/` | Dine-in, Takeaway, Draft orders, Check splitting, Payments (Cash, QR), Receipt |
| `src/preparation/` | `internal/preparation/` | Barista/Kitchen queue, Unit status transitions, Remake, Waste tracking |
| `src/audit/` | `internal/audit/` | System audit logs (subscribed asynchronously via Watermill event bus) |
| `src/integrations/trpc/` | Handlers in each Slice | Replaced by Echo REST APIs + Swagger OpenAPI 2.0 |

---

## 📋 2. Detailed Phased Migration Roadmap

### Phase 0: Infrastructure & Boilerplate (✅ COMPLETED)
- [x] **Echo HTTP Server:** Robust routing, recover middleware, structured access logging (`slog`), CORS.
- [x] **Pure Go SQLite:** WAL mode, busy timeout (`5000ms`), single-connection writer pool, foreign keys enabled.
- [x] **Embedded Auto-Migrations:** Embedded SQL files via `embed.FS`, runs automatically on startup.
- [x] **Data Access Layer (sqlc):** Type-safe SQL compilation, `Querier` interface enabled for mocking.
- [x] **Event Bus (Watermill):** In-memory Pub/Sub for background side-effects.
- [x] **Request Validation:** Custom Echo validator using `validator/v10`.
- [x] **Standard Response & Error Handling:** Uniform `{success, data, error}` envelope and HTTP status mapping.
- [x] **Interactive Swagger UI:** OpenAPI 2.0 docs live at `/swagger/index.html`.
- [x] **Reference Vertical Slice:** Complete `category` slice with CQRS (Commands, Queries, DTOs, Tests).

---

### Phase 1: Authentication & Staff Management (`internal/auth`)
*Focus: Secure, fast PIN-based login for POS terminals without heavy OAuth/JWT overhead.*

- [ ] **1.1 Database Schema Migration:**
  - Create table `staff_identities` (`id`, `name`, `role` [MANAGER, CASHIER, BARISTA], `pin_hash`, `is_active`, `created_at`).
  - Create table `staff_sessions` (`token`, `staff_id`, `role`, `expires_at`, `created_at`).
- [ ] **1.2 SQL Queries (`sql/queries/auth.sql`):**
  - `GetStaffByPIN`, `GetStaffByID`, `ListActiveStaff`, `CreateStaff`, `UpdateStaffPIN`, `ToggleStaffStatus`.
- [ ] **1.3 Business Slices:**
  - `sign_in_pin.go`: Verify numeric PIN (using `golang.org/x/crypto/bcrypt`), return session token.
  - `sign_out.go`: Invalidate session.
  - `bootstrap_manager.go`: CLI or startup check to ensure at least one manager exists.
  - `staff_admin.go`: Manager-only actions: Create staff, reset PIN, deactivate staff.
- [ ] **1.4 Middleware:**
  - `RequireAuth(roles ...string)`: Echo middleware validating session token and checking RBAC.
- [ ] **1.5 Testing:**
  - Unit & Integration tests for PIN verification, wrong PIN lockout, role permissions.

---

### Phase 2: Catalog Expansion (`internal/catalog`)
*Focus: Expand beyond simple categories to support menu items, sizes, and topping modifiers.*

- [ ] **2.1 Database Schema Migration:**
  - Create table `menu_items` (`id`, `category_id`, `name`, `description`, `base_price`, `is_active`).
  - Create table `item_sizes` (`id`, `item_id`, `name` [S, M, L], `price_adjustment`).
  - Create table `modifier_groups` (`id`, `name` [Độ ngọt, Lượng đá, Topping], `min_select`, `max_select`).
  - Create table `modifier_options` (`id`, `group_id`, `name` [Trân châu đen, Thạch củ năng, 50% đường], `price`).
  - Create table `item_modifier_groups` (link items to modifier groups).
- [ ] **2.2 SQL Queries (`sql/queries/catalog.sql`):**
  - CRUD for items, sizes, modifier groups, and options.
  - Efficient composite query: `GetFullMenu` (fetching all categories + items + sizes + modifiers in 1–2 queries).
- [ ] **2.3 Business Slices:**
  - `create_item.go`, `update_item.go`, `set_item_availability.go`.
  - `manage_modifiers.go`: Create/update modifier groups and options.
  - `get_menu.go`: High-performance query returning full hierarchical menu for the POS cashier screen.
- [ ] **2.4 Testing:**
  - Integration tests for price calculations, availability toggling, and menu hierarchy.

---

### Phase 3: Tables & Floor Layout (`internal/tables`)
*Focus: Manage dining areas, table availability, and active service session mapping.*

- [ ] **3.1 Database Schema Migration:**
  - Create table `tables` (`id`, `name`, `capacity`, `is_active`, `display_order`).
- [ ] **3.2 SQL Queries (`sql/queries/tables.sql`):**
  - `ListTablesWithStatus`, `CreateTable`, `RenameTable`, `SetTableAvailability`.
- [ ] **3.3 Business Slices:**
  - `get_overview.go`: Returns all tables with current occupancy status (Occupied / Available).
  - `create_table.go`, `rename_table.go`, `toggle_table.go`.
- [ ] **3.4 Testing:**
  - Integration tests for table status transitions and naming uniqueness.

---

### Phase 4: Sales Shift & Cash Movements (`internal/shift`)
*Focus: Cash drawer control, preventing staff shrinkage, shift handoff.*

- [ ] **4.1 Database Schema Migration:**
  - Create table `sales_shifts` (`id`, `staff_id`, `opened_at`, `closed_at`, `opening_float_vnd`, `closing_cash_vnd`, `expected_cash_vnd`, `notes`, `state` [OPEN, CLOSED]).
  - Create table `cash_movements` (`id`, `shift_id`, `staff_id`, `amount_vnd`, `type` [PAY_IN, PAY_OUT, CASH_DROP], `reason`, `created_at`).
- [ ] **4.2 SQL Queries (`sql/queries/shifts.sql`):**
  - `GetCurrentShift`, `OpenShift`, `CloseShift`, `RecordCashMovement`, `GetShiftSummary`.
- [ ] **4.3 Business Slices:**
  - `open_shift.go`: Verify no open shift exists, record opening cash float.
  - `cash_movement.go`: Record cash in/out (buying ice, paying supplier, mid-shift cash drop).
  - `close_shift.go`: Reconcile cash drawer, calculate discrepancy (over/short), close shift.
- [ ] **4.4 Testing:**
  - Test opening float constraints, multiple open shifts prevention, cash balance reconciliation.

---

### Phase 5: Core Sales, Orders & Payments (`internal/sales`)
*Focus: The heart of the POS system. Low-latency order placement, bill splitting, payments.*

- [ ] **5.1 Database Schema Migration:**
  - Create table `service_sessions` (`id`, `type` [DINE_IN, TAKEAWAY], `table_id`, `status` [ACTIVE, COMPLETED, CANCELLED], `opened_at`, `closed_at`).
  - Create table `order_rounds` (`id`, `session_id`, `round_number`, `staff_id`, `created_at`).
  - Create table `order_items` (`id`, `round_id`, `item_id`, `size_id`, `item_name`, `unit_price`, `quantity`, `notes`, `status` [PENDING, PREPARING, READY, SERVED, CANCELLED]).
  - Create table `order_item_modifiers` (`order_item_id`, `modifier_option_id`, `name`, `price`).
  - Create table `checks` (`id`, `session_id`, `check_number`, `subtotal_vnd`, `discount_vnd`, `tax_vnd`, `total_vnd`, `state` [OPEN, PAID, VOID]).
  - Create table `payments` (`id`, `check_id`, `method` [CASH, MANUAL_QR, CARD], `amount_vnd`, `reference_code`, `created_at`).
- [ ] **5.2 SQL Queries (`sql/queries/sales.sql`):**
  - Full transactional order placement, bill calculation, payment recording.
- [ ] **5.3 Business Slices:**
  - `start_session.go`: Open Dine-in (assign table) or Takeaway order.
  - `submit_order_round.go`:
    - Save items to database within SQLite transaction.
    - **Publish Event:** `h.bus.Publish("order.submitted", OrderSubmittedEvent{...})`.
  - `check_splitting.go`: Split items across multiple bills for split payments.
  - `settle_payment.go`: Record cash/QR payment, mark check as PAID, close session if all checks paid.
- [ ] **5.4 Testing:**
  - Concurrent checkout tests, round addition, partial payment reconciliation.

---

### Phase 6: Preparation Station / Kitchen Display (`internal/preparation`)
*Focus: Real-time drink queue for Baristas, status transitions.*

- [ ] **6.1 Database Schema Migration:**
  - Create table `preparation_units` (`id`, `order_item_id`, `item_name`, `options_summary`, `status` [QUEUED, BREWING, READY, SERVED], `notes`, `created_at`, `updated_at`).
- [ ] **6.2 Event Consumer (Watermill):**
  - Subscribe to `order.submitted`: Auto-create `preparation_units` for each item in the round.
- [ ] **6.3 Business Slices:**
  - `get_queue.go`: List active drinks in order of submission.
  - `update_status.go`: Barista marks item as Brewing -> Ready -> Served.
  - `remake_item.go` / `waste_item.go`: Record spilled/remade drinks.
- [ ] **6.4 Real-Time Updates (Optional):**
  - Expose Server-Sent Events (SSE) or lightweight WebSocket for zero-delay screen updates.

---

### Phase 7: Frontend Integration & Single-Binary Packaging
*Focus: Seamless UI connection and running everything as a single lightweight binary.*

- [ ] **7.1 OpenAPI Client Generation:**
  - Run `openapi-typescript` on `/swagger/doc.json` to generate TypeScript types for the React frontend.
  - Replace tRPC calls in `cafe-pos/src` with typed Fetch / TanStack Query hooks.
- [ ] **7.2 Static Asset Embedding (All-in-One Deployment):**
  - Build React app: `cd /home/laffy/cafe-pos && pnpm build`.
  - Configure Echo to serve static files from `dist/`:
    ```go
    e.Static("/", "../cafe-pos/dist")
    e.File("/*", "../cafe-pos/dist/index.html")
    ```
- [ ] **7.3 POS Hardware Run Verification:**
  - Verify total RAM usage under 30MB.
  - Verify cold startup time < 50ms.

---

## 📊 3. Migration Progress Tracker

| Module | Status | Estimated Slices | Completed | Target Completion |
| :--- | :---: | :---: | :---: | :---: |
| **0. Core Boilerplate** | ✅ DONE | 5 | 5 / 5 | 2026-09-08 |
| **1. Auth & Staff** | ⏳ PENDING | 4 | 0 / 4 | Phase 1 |
| **2. Catalog & Menu** | ⏳ PENDING | 5 | 1 / 5 | Phase 2 |
| **3. Tables & Layout** | ⏳ PENDING | 3 | 0 / 3 | Phase 3 |
| **4. Sales Shift & Cash** | ⏳ PENDING | 3 | 0 / 3 | Phase 4 |
| **5. Sales, Orders & Pay** | ⏳ PENDING | 6 | 0 / 6 | Phase 5 |
| **6. Preparation (Barista)** | ⏳ PENDING | 3 | 0 / 3 | Phase 6 |
| **7. Frontend Single-Binary**| ⏳ PENDING | 2 | 0 / 2 | Phase 7 |

---

## 💡 4. Best Practices for This Migration

1. **Keep Slices Completely Independent:**
   - When migrating `internal/sales`, do not import structs from `internal/tables`. Use queries or interfaces.
2. **Use Integer for Money in Vietnam (VND):**
   - In SQLite, store amounts as `INTEGER` (VND has no decimals). Avoid `REAL` for financial calculations to prevent floating-point rounding errors.
3. **One SQLite Transaction Per Order:**
   - Wrap order items and session status changes in a single SQLite transaction (`queries.WithTx(tx)`).
4. **Leverage Watermill for Peripheral Actions:**
   - Never let receipt printing or kitchen screen notifications block the main payment HTTP response. Always publish an event!
