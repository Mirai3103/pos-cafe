# POS Cafe Backend Migration Plan: TypeScript to Golang

> ## 🔒 CLOSED — 2026-09-18
>
> **This migration is complete and this document is frozen.** See [ADR-047](spec/decisions.md).
>
> For current status and remaining work, read [`ROADMAP.md`](ROADMAP.md). This file
> is retained as the historical record of Phases 0 through 6C — several ADRs
> reference its checklists — and its body is not edited further.
>
> Three things to know before reading it:
>
> - **The tracker table at the bottom is stale.** It records Preparation as
>   `PENDING 2/3`; Phase 6C completed on 2026-09-18 in commit `3c6f61e`, making it
>   3/3. `ROADMAP.md` carries the corrected status.
> - **Section 7 is superseded** by backlog [Phase 11](docs/backlog/phase-11-clients-over-lan-and-single-binary.md),
>   which merges it with the LAN-serving requirements from the TypeScript tracker.
> - **The Go system has passed its source.** TypeScript tickets 07 and 11 remain
>   unimplemented in `cafe-pos` while Go delivered them as Phases 5C and 6C. From
>   there on, no canonical implementation exists to port: remaining work is designed
>   from [`CONTEXT.md`](CONTEXT.md), which now lives in this repository. References
>   to `cafe-pos/src` below are historical provenance and carry no live authority.

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

### Phase 4: Sales Shift & Cash Movements (`internal/shift`) (✅ COMPLETED)
*Approved Design Spec:* [`docs/superpowers/specs/2026-09-13-shift-slice-design.md`](docs/superpowers/specs/2026-09-13-shift-slice-design.md)
*Implementation Plan:* [`docs/superpowers/plans/2026-09-13-shift-slice.md`](docs/superpowers/plans/2026-09-13-shift-slice.md)

> The checklist below predates the canonical source review and is superseded by the spec above. It is kept only as a record of the original sketch.

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
*Approved Design Spec:* [`docs/superpowers/specs/2026-09-13-sales-session-draft-design.md`](docs/superpowers/specs/2026-09-13-sales-session-draft-design.md)
*Implementation Plan:* [`docs/superpowers/plans/2026-09-13-sales-session-draft.md`](docs/superpowers/plans/2026-09-13-sales-session-draft.md)

> The checklist below predates the canonical source review and is superseded by the spec above. It is kept only as a record of the original sketch. Phase 5 is delivered as four sub-phases (spec §3), each with its own design specification, implementation plan, and test suite; sub-phase spec links are added here as each lands.

| Sub-phase | Scope | Status |
| :--- | :--- | :---: |
| **5A** — Service Session lifecycle, Table assignments, Order Draft | [spec](docs/superpowers/specs/2026-09-13-sales-session-draft-design.md) / [plan](docs/superpowers/plans/2026-09-13-sales-session-draft.md) | ✅ COMPLETED (2026-09-13) |
| **5B** — Commit, Committed Items, Checks, Charge Allocations, Order Draft targeting | [spec](docs/superpowers/specs/2026-09-14-sales-commit-checks-design.md) / [plan](docs/superpowers/plans/2026-09-14-sales-commit-checks.md) | ✅ COMPLETED (2026-09-14) |
| **5C** — Cash and Manual QR Payments, Check splitting and merging, settlement | [spec](docs/superpowers/specs/2026-09-14-sales-payments-settlement-design.md) / [plan](docs/superpowers/plans/2026-09-14-sales-payments-settlement.md) | ✅ COMPLETED (2026-09-14) |
| **5D** — Submit, Orders, Preparation Units, Service Session closure, Completed Sale | [spec](docs/superpowers/specs/2026-09-15-sales-submission-closure-design.md) / [plan](docs/superpowers/plans/2026-09-15-sales-submission-closure.md) | ✅ COMPLETED (2026-09-15) |

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

*Approved 6A Design Spec:* [`docs/superpowers/specs/2026-09-16-preparation-queue-transitions-design.md`](docs/superpowers/specs/2026-09-16-preparation-queue-transitions-design.md)
*Approved 6B Design Spec:* [`docs/superpowers/specs/2026-09-17-preparation-corrections-design.md`](docs/superpowers/specs/2026-09-17-preparation-corrections-design.md)
*Approved 6B Implementation Plan:* [`docs/superpowers/plans/2026-09-17-preparation-corrections.md`](docs/superpowers/plans/2026-09-17-preparation-corrections.md)
*Approved 6C Design Spec:* [`docs/superpowers/specs/2026-09-18-preparation-financial-corrections-design.md`](docs/superpowers/specs/2026-09-18-preparation-financial-corrections-design.md)
*Approved 6C Implementation Plan:* [`docs/superpowers/plans/2026-09-18-preparation-financial-corrections.md`](docs/superpowers/plans/2026-09-18-preparation-financial-corrections.md)

> The sketch checklist at the bottom of this section predates Phase 5D, which already created Preparation Units synchronously at Submit and introduced the single-unit advance command; it is kept only as a record of the original sketch, and the sub-phase checklists below are authoritative. Phase 6 is delivered as ordered sub-phases (ADR-032) so Cancellation can be designed with its unfinished Refund/Comp and closure dependencies instead of weakening those boundaries. Synchronous Submit remains the queue-creation boundary: `order.submitted` is neither published nor consumed through Watermill for queue creation, per ADR-033.

| Sub-phase | Scope | Status |
| :--- | :--- | :---: |
| **6A** — Preparation Queue reads and bulk transitions | [spec](docs/superpowers/specs/2026-09-16-preparation-queue-transitions-design.md) | ✅ COMPLETED (2026-09-16) |
| **6B** — Alerts, Waste, Remake, priority, and state correction | [spec](docs/superpowers/specs/2026-09-17-preparation-corrections-design.md) / [plan](docs/superpowers/plans/2026-09-17-preparation-corrections.md) | ✅ COMPLETED (2026-09-17) |
| **6C** — Cancellation/change and financial correction integration | [spec](docs/superpowers/specs/2026-09-18-preparation-financial-corrections-design.md) / [plan](docs/superpowers/plans/2026-09-18-preparation-financial-corrections.md) | ✅ COMPLETED (2026-09-18) |

**6A — Preparation Queue reads and bulk transitions (✅ COMPLETED 2026-09-16):**

- [x] **6A.1 Database Schema Migration:** `000012_add_preparation_queue_fields.sql` — `in_preparation_at` aging field on `preparation_units` with backfill from `preparation_unit_transitions`, keeping all six canonical states from ADR-028 and the queue read schema private.
- [x] **6A.2 SQL Queries (`sql/queries/preparation.sql`):** active queue projection, per-unit transition locks and writes, `preparation_unit_transitions` and audit writes; idempotency through the shared `idempotency_keys` queries (ADR-007), all sqlc generated.
- [x] **6A.3 Shared Timestamp-Aware Transition:** one timestamp-aware advance helper shared by the 5D single-unit command and the 6A bulk path; missing and stale units become typed outcomes.
- [x] **6A.4 Authorized Read Executor:** read-only `REPEATABLE READ` transaction executor that re-verifies current authority on every queue read.
- [x] **6A.5 Active Queue Projection:** side-effect-free `GET` whose response contains only `observed_at` (the PostgreSQL clock at the read) and the active units ordered by `queued_at`, with `in_preparation_at` aging; launch freshness is polling with client invalidation after local mutations, per ADR-035 — no SSE, WebSockets, notifier seam, or background fan-out.
- [x] **6A.6 Bulk Advance:** contract normalization and validation, authority and idempotency verified once, unique units locked in UUID order, each unit processed in a fixed package-private PostgreSQL savepoint (ADR-034), exact replay with no duplicate transitions or audits, and outer-transaction rollback when audit or idempotency storage fails.
- [x] **6A.7 HTTP Routes & Swagger:** queue read and bulk advance routes with capability authorization, uniform error mapping, and OpenAPI 2.0 documentation.
- [x] **6A.8 Testing:** unit and PostgreSQL integration suites under `-race`, focused concurrency verification (`TestConcurrentAdvance`, `TestOverlappingBulkAdvance`), idempotent replay coverage, and green Sales regression (Submit, fulfillment, Completed Sale, Service Session closure).

**6B — Alerts, Waste, Remake, priority, and state correction (✅ COMPLETED 2026-09-17):**

- [x] **6B.1 Database Schema Migration:** `000013_add_preparation_corrections.sql` — typed correction facts (`preparation_wastes`, `preparation_remakes`, `preparation_state_corrections`, `preparation_alerts`), `priority` + `remake_of_preparation_unit_id` on `preparation_units` with a `STANDARD` backfill and enforced priority/link pairing, CHECK-enforced reason catalogs and exactly the eight Phase 6B transition pairs, and typed fact integrity (ADR-036).
- [x] **6B.2 SQL Queries (`sql/queries/preparation.sql`):** Waste/Acknowledge/Remake/CorrectState lock-and-write queries (session-first locking shared with closure), active-alert and correction-history queue projections, and the extended Sales unit reads carrying priority/link metadata; idempotency through the shared `idempotency_keys` queries (ADR-007), all sqlc generated.
- [x] **6B.3 Waste:** one atomic command writing the terminal Waste fact, `WASTED` state, typed transition, unacknowledged WASTE alert, two audits, and the replayable result; only `IN_PREPARATION`/`READY` units are admissible.
- [x] **6B.4 Alert Acknowledgment:** records complete evidence (identity, session, time) and controls only active-alert state and terminal-unit queue visibility; no closure, commercial, or financial effect (ADR-039).
- [x] **6B.5 Remake:** copies the immutable preparation snapshot, allocates a unique next unit number under the Order Item lock, links its wasted source, receives the REMAKE priority — the only launch priority (ADR-037) — and changes no charge.
- [x] **6B.6 State Correction:** Manager-only, own-PIN-gated before replay (ADR-038), one-step reverse over 1–50 unique units, deterministic UUID-byte lock ordering, all-or-nothing across the batch.
- [x] **6B.7 Queue & Sales Projections:** the queue returns non-null `units`/`alerts`/`corrections` from one repeatable-read snapshot with deterministic ordering and no financial leakage; Service Session and Completed Sale project priority/link metadata and the complete typed transition history (including Waste and Correction transitions).
- [x] **6B.8 HTTP Routes & Swagger:** correction routes with capability authorization, uniform error mapping, and OpenAPI 2.0 documentation; `manager_pin` exists only in the request binding and executor input.
- [x] **6B.9 Testing:** unit and PostgreSQL integration suites under `-race`, the ten-suite focused concurrency matrix (unit-level races, correction/remake versus closure), exact-replay and PIN rotation/revocation coverage, and the three end-to-end recovery workflows through real handlers on both sides of the ADR-024 boundary.

**6C — Cancellation/change and financial correction integration (✅ COMPLETED 2026-09-18):**

- [x] **6C.1 Database Schema Migration:** `000014_add_preparation_financial_corrections.sql` — append-only `charge_adjustments`, `preparation_cancellations`, `sales_comps`, `payment_voids`, `refunds`, `refund_payment_allocations`, `refund_adjustment_allocations`, and `refund_completions`, with kind/source/scope pairing, positive-amount, one-correction-per-source, and derived-state constraints, and the reserved `QUEUED -> CANCELLED` transition pair.
- [x] **6C.2 SQL Queries (`sql/queries/preparation.sql`, `sql/queries/sales.sql`, `sql/queries/shift.sql`):** cancellation/comp/refund/void lock-and-write queries, the corrected Check financials and pending-Refund derivations, and Shift reconciliation terms derived by `internal/shift` through its own queries; idempotency through the shared `idempotency_keys` queries (ADR-007), all sqlc generated.
- [x] **6C.3 Cancellation/Change:** Cashier/Manager-only cross-slice command cancelling 1–50 queued units atomically with one terminal state, typed transition, Cancellation/Change alert, audit evidence, and an append-only live Charge Adjustment for each charged original unit; Change requires an already-submitted later replacement Order in the same active Service Session, and the batch is all-or-nothing on the common Check-before-Session lock protocol (ADR-040).
- [x] **6C.4 Comp:** one Manager-approved Comp per charged Wasted standard unit, with a live adjustment and Check settlement when the corrected balance reaches zero on an active Session and a `POST_SALE` adjustment linked to the Completed Sale on a closed one; the Completed Sale snapshot is never rewritten (ADR-041).
- [x] **6C.5 Refund:** one Manager-approved command allocates equal positive totals against both explicit source Charge Adjustments and non-voided source Payments of one method and Check, including post-sale correction capacity; Cash completes in the recording transaction while Manual QR stays pending until explicit confirmation without a second approval or repeat Manager Approval (ADR-042, ADR-043).
- [x] **6C.6 Payment Void:** whole, append-only, Manager-approved reversal of one Payment against an active Service Session and its still-open original Shift, rejecting any Payment carrying a Refund allocation, reopening a Check only when remaining valid coverage no longer covers the charge (ADR-045).
- [x] **6C.7 Projections, Closure, And Shift Reconciliation:** Check projections carry the adjustment/refund equations, Service Session closure rejects pending Refund through `PENDING_REFUND_FOR_CLOSURE` with the documented precedence, Completed Sale exposes additive post-sale correction history, the Preparation Queue stays free of financial leakage, and the current-Shift read adds non-voided Cash Payments and completed Cash Refunds (with pending Refund and unresolved post-sale correction visibility) without implementing Shift Close (ADR-044, ADR-046).
- [x] **6C.8 HTTP Routes & Swagger:** the five operation routes with capability authorization, uniform typed error mapping, deterministic replay/conflict behavior, and OpenAPI 2.0 documentation; credentials exist only in the request binding and executor input, never in persistence, responses, audits, or logs.
- [x] **6C.9 Testing:** unit, PostgreSQL migration/integration, HTTP authorization/replay/privacy, failure-injection, and focused concurrency suites under `-race`, including the deterministic Cancellation and Sales correction race matrices in which only a Submit pairing may surface the retryable `40P01` abort, plus full Sales/Preparation/Shift regression.

The original sketch below is superseded by the sub-phase checklists above.
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
| **4. Sales Shift & Cash** | ✅ DONE | 3 | 3 / 3 | 2026-09-13 |
| **5. Sales, Orders & Pay** | ✅ DONE | 6 | 6 / 6 | 2026-09-15 |
| **6. Preparation (Barista)** | ⏳ PENDING | 3 | 2 / 3 | Phase 6 |
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
