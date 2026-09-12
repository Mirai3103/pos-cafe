# POS Cafe Backend Migration Plan: TypeScript to Golang

> **Source Project:** `/home/laffy/cafe-pos/src` (Fullstack TS: React 19 + tRPC + Drizzle ORM + PostgreSQL)  
> **Target Project:** `/home/laffy/Desktop/go-vertical-slice-template-main/pos-cafe` (Go 1.26+ + Echo v4 + PostgreSQL + pgx/v5 + sqlc + Watermill)  
> **Primary Goal:** Eliminate lag on low-spec POS terminals (Celeron, 2–4GB RAM), reduce RAM usage from ~500MB to < 30MB, achieve instant boot (< 20ms)

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
- [x] **Production-Ready PostgreSQL:** Powered by `jackc/pgx/v5` with connection pool, query cancellation, and type-safe scanning.
- [x] **Embedded Auto-Migrations:** Embedded SQL files via `embed.FS`, runs automatically on startup.
- [x] **Data Access Layer (sqlc):** Type-safe SQL compilation, `Querier` interface enabled for mocking.
- [x] **Event Bus (Watermill):** In-memory Pub/Sub for background side-effects.
- [x] **Request Validation:** Custom Echo validator using `validator/v10`.
- [x] **Standard Response & Error Handling:** Uniform `{success, data, error}` envelope and HTTP status mapping.
- [x] **Interactive Swagger UI:** OpenAPI 2.0 docs live at `/swagger/index.html`.
- [x] **Reference Vertical Slice:** Complete `category` slice with CQRS (Commands, Queries, DTOs, Tests).

---

### Phase 1: Authentication & Staff Management (`internal/auth`) (✅ COMPLETED — PR #2 #5195b5c, review fixes applied)
*Focus: Secure, fast PIN-based login for POS terminals without heavy OAuth/JWT overhead.*

- [x] **1.1 Database Schema Migration:** `000002_create_auth_tables.sql` — `staff_identities`, `staff_operational_roles`, `staff_access_sessions`, `idempotency_keys` (ADR-001..005).
- [x] **1.2 SQL Queries (`sql/queries/auth.sql`):** `GetStaffByLoginCode/ByID`, `ListActiveIdentities/ListAllStaff`, `GetStaffRoles/ListAllStaffRoles`, `CreateStaffIdentity`, `SetStaffEnabled`, `UpdateStaffPin`, `Add/ClearStaffRoles`, `CountActiveManagers`+`CountManagers`, session & idempotency queries (`sqlc` generated).
- [x] **1.3 Business Slices:** `sign_in`/`sign_out`, `bootstrap_manager` (advisory lock 739201), `staff_create/list/me/set_enabled/replace_roles/reset_pin` (lock 1247091103), `get_session/lock/unlock/declare_workspace/record_activity/list_identities`, `idempotency` (tx+advisory lock, targetID fingerprint), `ratelimit` (5/15m sign-in, 3/5m unlock).
- [x] **1.4 Middleware:** `RequireAuth(roles...)` + `RequireCapability` + `RateLimit`, hybrid Bearer/Cookie (`staff_session_token`), inactivity auto-lock (5m cashier/manager, 15m preparation), API state `authenticated` (DB `active` → API `authenticated`).
- [x] **1.5 Testing:** Unit (`domain_test`, `dto_test`, `handlers_test`, `middleware_test`, `ratelimit_test`, `idempotency_test`) + Integration (`auth_integration_test`, `staff_test`, `bootstrap_manager_test`); review fixes: sign-out error propagation, unlock atomic tx, `FINAL_ENABLED_MANAGER_REQUIRED` (`ErrManagerInvariant`), CORS PATCH, idempotency atomicity, bootstrap `CountManagers`.

---

### Phase 2: Catalog Expansion (`internal/catalog`) (✅ COMPLETED)
*Focus: Expand beyond simple categories to support menu items, sizes with absolute pricing, topping modifiers, category/item attachments, inherited exclusions, and dedicated menu projections.*
*Approved Design Spec:* [`docs/superpowers/specs/2026-09-10-catalog-slice-design.md`](docs/superpowers/specs/2026-09-10-catalog-slice-design.md)
*Implementation Plan:* [`docs/superpowers/plans/2026-09-10-catalog-slice.md`](docs/superpowers/plans/2026-09-10-catalog-slice.md)

- [x] **2.1 Database Schema Migration:** `000003_create_catalog_tables.sql`
  - Created tables: `menu_categories` (expanded from 000001), `menu_items`, `menu_item_sizes` (storing absolute `price_vnd` per size rather than relative adjustments; direct items have nullable `price_vnd`), `modifier_groups`, `modifier_options`, `category_modifier_groups`, `item_modifier_groups`, `item_modifier_group_exclusions`, `modifier_group_default_options`, `catalog_mutation_requests` (idempotency tracking), and `audit_events`.
- [x] **2.2 SQL Queries (`sql/queries/catalog.sql`):**
  - CRUD and lock-for-update queries for categories, items, sizes, modifier groups, and options.
  - Attachment and inherited-exclusion association queries.
  - Normalized name collision queries with advisory locks (`CatalogAdvisoryLock`).
  - Projection snapshot queries: `ListMenuCategories`, `ListAllMenuItems`, `ListAllMenuItemSizes`, `ListModifierGroups`, `ListAllModifierOptions`, `ListAllCategoryModifierGroups`, `ListAllItemModifierGroups`, `ListAllItemModifierGroupExclusions`, `ListAllModifierGroupDefaultOptions`, and `ListCatalogAuditEvents`.
- [x] **2.3 Business Slices & Projections:**
  - Category operations: `create_category.go`, `rename_category.go`.
  - Item operations: `item_create.go`, `item_commands.go` (rename, reprice, availability, retirement).
  - Size operations: `size_commands.go` (rename, reprice, availability, retirement).
  - Modifier operations: `modifier_create.go`, `modifier_commands.go` (rename, reprice, availability, retirement).
  - Assignment & Exclusion operations: `assignments.go` (attach item/category groups, exclude inherited groups, set default options).
  - Dedicated menu projections (`projections.go`):
    - `SellableMenu`: Returns only sellable, available, priced items with effective modifier groups, available options, and validated default selections for cashier/ordering.
    - `ManagementMenu`: Full hierarchical catalog projection including retired entities, explicit sizes, direct attachments, and exclusions for managerial control.
    - `AvailabilityMenu`: Lightweight projection for fast toggling of availability flags across categories, items, sizes, and modifier options.
    - `ModifierGroups`: Detailed management view of all modifier groups and options.
    - `AuditEvents`: Inspection endpoint for catalog audit records with actor and session tracking.
  - Robust mutation executor (`executor.go`): Transactional advisory locking, request idempotency replay/conflict detection, capability authorization, and fresh Manager PIN verification.
- [x] **2.4 Testing & Documentation:**
  - Comprehensive unit and integration test suites: `domain_test.go`, `errors_test.go`, `schema_integration_test.go`, `executor_integration_test.go`, `commands_integration_test.go`, `lifecycle_integration_test.go`, `projections_integration_test.go`, `projections_benchmark_test.go`, `routes_test.go`, and end-to-end `catalog_integration_test.go`.
  - Concurrency verification: Synchronized goroutines for duplicate `request_id` idempotency and normalized name collision safety.
  - Authorization defense: Verified replay denial after role/session revocation and audit evidence (`catalog.authorization_denied`).
  - OpenAPI 2.0 / Swagger documentation: All 27 catalog operations annotated with Bearer security, request DTOs, projection responses, and error status codes.

---

### Phase 3: Tables & Floor Layout (`internal/tables`) (✅ COMPLETED)
*Approved Design Spec:* [`docs/superpowers/specs/2026-09-12-tables-slice-design.md`](docs/superpowers/specs/2026-09-12-tables-slice-design.md)
*Implementation Plan:* [`docs/superpowers/plans/2026-09-12-tables-slice.md`](docs/superpowers/plans/2026-09-12-tables-slice.md)

> The checklist below predates the canonical source review and is superseded by the spec above. It is kept only as a record of the original sketch.

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
    - Save items to database within PostgreSQL transaction.
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
| **1. Auth & Staff** | ✅ DONE | 4 | 4 / 4 | 2026-09-09 (PR #2) |
| **2. Catalog & Menu** | ✅ DONE | 5 | 5 / 5 | Phase 2 |
| **3. Tables & Layout** | ✅ DONE | 4 | 4 / 4 | 2026-09-12 |
| **4. Sales Shift & Cash** | ⏳ PENDING | 3 | 0 / 3 | Phase 4 |
| **5. Sales, Orders & Pay** | ⏳ PENDING | 6 | 0 / 6 | Phase 5 |
| **6. Preparation (Barista)** | ⏳ PENDING | 3 | 0 / 3 | Phase 6 |
| **7. Frontend Single-Binary**| ⏳ PENDING | 2 | 0 / 2 | Phase 7 |

---

## 💡 4. Best Practices for This Migration

1. **Keep Slices Completely Independent:**
   - When migrating `internal/sales`, do not import structs from `internal/tables`. Use queries or interfaces.
2. **Use Integer for Money in Vietnam (VND):**
   - In PostgreSQL, store amounts as `BIGINT` (VND has no decimals, `BIGINT` prevents floating-point rounding errors). Avoid `REAL`/`FLOAT`.
3. **One Database Transaction Per Order:**
   - Wrap order items and session status changes in a single PostgreSQL transaction (`queries.WithTx(tx)`).
4. **Leverage Watermill for Peripheral Actions:**
   - Never let receipt printing or kitchen screen notifications block the main payment HTTP response. Always publish an event!
