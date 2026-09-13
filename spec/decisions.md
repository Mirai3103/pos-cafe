# Architecture Decision Records (ADR) - POS Cafe Backend

This document records architectural and technical design decisions made during the migration from TypeScript to Golang.

---

## ADR-001: Multi-Role Model for Staff (`staff_operational_roles`)

* **Decision Date:** 2026-09-09
* **Status:** Accepted
* **Context:** The legacy system allowed an employee to hold multiple roles simultaneously (e.g., `MANAGER` concurrently acting as `CASHIER`, or `CASHIER` concurrently acting as `BARISTA`).
* **Decision:** Retain the multi-role model using the relation table `staff_operational_roles(staff_identity_id, role)`.
* **Consequences:** 100% backward-compatible with the React frontend and existing POS business authorization logic.

---

## ADR-002: PIN Hashing Algorithm with `bcrypt`

* **Decision Date:** 2026-09-09
* **Status:** Accepted
* **Context:** The legacy system used `argon2id` from a Node.js library. An optimal solution was required for the Go backend.
* **Decision:** Use the standard `golang.org/x/crypto/bcrypt` package with default/standard cost to hash and verify staff PINs (4–8 digits).
* **Consequences:** Optimizes performance and memory footprint on low-spec POS hardware (Celeron processors, 2–4GB RAM).

---

## ADR-003: Hybrid Token Authentication Mechanism (Bearer Header + Cookie)

* **Decision Date:** 2026-09-09
* **Status:** Accepted
* **Context:** Must flexibly accommodate Web SPA clients, Desktop Terminals, and embedded hardware applications.
* **Decision:** Support a hybrid mechanism:
* Upon successful login/unlock, the API returns `token` in the JSON response body and simultaneously sets an HTTP-only cookie.
* The `RequireAuth` middleware prioritizes reading the token from the `Authorization: Bearer <token>` header, falling back to the `staff_session_token` cookie if absent.


* **Consequences:** Maximum flexibility across all client types; straightforward testing via Postman/curl and Swagger UI.

---

## ADR-004: Explicit Column Size Constraints (`VARCHAR(n)`)

* **Decision Date:** 2026-09-09
* **Status:** Accepted
* **Context:** Although PostgreSQL stores `TEXT` and `VARCHAR` with equivalent RAM/disk efficiency, leaving column lengths unbounded at the database layer exposes the system to data bloat if clients submit oversized payloads.
* **Decision:** Enforce explicit length constraints across tables:
* `display_name`: `VARCHAR(120)`
* `login_code`: `VARCHAR(24)`
* `pin_hash`: `VARCHAR(72)` (fits bcrypt's 60-character output comfortably)
* `token_hash`: `VARCHAR(64)` (SHA-256 hex string)
* `role`, `state`, `active_workspace`: `VARCHAR(20)`


* **Consequences:** Reinforces data integrity directly at the database tier.

---

## ADR-005: Consolidation of Idempotency Tables into a Single Table (`idempotency_keys`)

* **Decision Date:** 2026-09-09
* **Status:** Accepted
* **Context:** The legacy TypeScript codebase provisioned a dedicated table per use case (`staff_identity_creation_requests`, `staff_identity_enabled_state_requests`, `staff_identity_role_replacement_requests`, `staff_identity_pin_reset_requests`), leading to the schema bloat anti-pattern and wasted resources.
* **Decision:**
* Replace all four legacy tables with a **single unified table** following standard Stripe / IETF patterns:
```sql
CREATE TABLE idempotency_keys (
    key UUID NOT NULL,                       -- request_id from frontend
    actor_id UUID,                           -- acting staff identity ID
    action VARCHAR(50) NOT NULL,             -- 'staff.create', 'staff.set_enabled', etc.
    request_hash VARCHAR(64) NOT NULL,       -- SHA-256 payload hash for mutation detection
    response_code INT NOT NULL,              -- HTTP status code (200, 201...)
    response_body JSONB NOT NULL,            -- Cached JSON payload to replay on retries
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (actor_id, key)
);

```


* Reuse this table across all future slices such as `shift`, `sales`, and `payments` without adding extra tables.


* **Consequences:**
* Highly concise database schema that eliminates redundant boilerplate.
* Enables instant response replaying during network instability or terminal double-clicks.
* Simplifies cleanup automation via a scheduled cron job (purging records older than 24 hours).

---

## ADR-006: Provision sớm schema Sales cho Phase 3 (`service_sessions`, `table_assignments`)
- **Ngày quyết định:** 2026-09-12
- **Trạng thái:** Accepted
- **Bối cảnh:** Theo `CONTEXT.md`, một Bàn "may be associated with one or more active Service Sessions". Read `overview` canonical của Tables phải trả về các Service Session đang chiếm bàn, tức là phụ thuộc vào `table_assignments` và `service_sessions` — hai bảng thuộc quyền sở hữu nghiệp vụ của `internal/sales` (Phase 5). Nếu chờ Phase 5, Phase 3 sẽ ship một contract API thiếu field và phải breaking change về sau.
- **Quyết định:**
  - Migration `000006` của Phase 3 tạo luôn `service_sessions` và `table_assignments`, kèm `COMMENT ON TABLE` ghi rõ quyền sở hữu thuộc `internal/sales` (Phase 5).
  - `service_sessions` lược bỏ **duy nhất** cột `sales_shift_id` vì `sales_shifts` là bảng của Phase 4. Phase 5 bổ sung bằng `ALTER TABLE service_sessions ADD COLUMN sales_shift_id UUID NOT NULL REFERENCES sales_shifts(id)`.
  - `internal/tables` **không** import `internal/sales`. Nó đọc occupancy qua query sqlc của riêng nó (`ListCurrentTableOccupants`), đúng nguyên tắc "dùng queries hoặc interface, đừng import struct" của MIGRATE_PLAN §4.1.
  - Phase 3 chỉ **đọc** hai bảng này, không ghi qua API. Test integration seed trực tiếp bằng SQL.
- **Hệ quả:**
  - Contract công khai của Tables hoàn chỉnh và ổn định ngay từ Phase 3; frontend không phải chịu breaking change khi Phase 5 lên.
  - Đổi lại, Phase 5 phải thực hiện đúng một thao tác `ALTER TABLE` bổ sung thay vì `CREATE TABLE`.

---

## ADR-007: Reaffirming the Shared `idempotency_keys` Table

* **Decision Date:** 2026-09-12
* **Status:** Accepted
* **Context:** ADR-005 mandated that all slices share a single `idempotency_keys` table. However, Phase 2 (Catalog) created a dedicated `catalog_mutation_requests` table, contradicting this decision and reintroducing the exact schema bloat anti-pattern that ADR-005 aimed to eliminate.
* **Decision:**
* Starting from Phase 3 onwards, all slices must write idempotency records to the shared `idempotency_keys` table, using fully qualified action names for the `action` column (e.g., `tables.create_table`, `tables.rename_table`, `tables.set_table_availability`).
* `catalog_mutation_requests` is acknowledged as a **historical exception**, not a precedent. Catalog will not be retrofitted during Phase 3; any cleanup will be handled as a separate task.
* Each slice continues to own its respective executor. Sharing the **database table** does not imply sharing the **helper logic**: `internal/tables` must not import idempotency helpers from `internal/auth`.


* **Consequences:**
* Prevents schema bloat by avoiding a new table for every slice.
* Preserves vertical slice boundaries at the code layer while consolidating the storage layer.

---

## ADR-008: Partial Expected Cash in Phase 4

* **Decision Date:** 2026-09-13
* **Status:** Accepted
* **Context:** The canonical Expected Cash formula is Opening Float plus Cash Payments and Pay Ins, less Cash Refunds and Pay Outs. The Cash Payment and Cash Refund terms read the `payments` table, which references `checks`, which in turn references `service_sessions` and order drafts — the whole chain is owned by Phase 5. Provisioning it in Phase 4 would replicate the most intricate constraint set in the system a phase early, for a figure Phase 4 cannot yet produce anyway.
* **Decision:**
* Phase 4 computes `expected_cash_vnd` as Opening Float plus Pay Ins less Pay Outs, on read, never stored.
* The API field ships in its final name and shape from Phase 4, so the response contract does not change when the formula completes.
* Phase 5 adds the Cash Payment and Cash Refund terms to the single `SumCashMovements`-adjacent computation in `internal/shift/current.go`.
* The Swagger description and the design spec both state that the figure is incomplete until Phase 5.
* **Consequences:**
* The public Shift API contract is complete and stable from Phase 4 onward.
* Until Phase 5 lands, `expected_cash_vnd` reflects fund movements only and must not be presented to staff as a reconciliation figure.
* The guard on the total is symmetric, because sustained Pay Outs can legitimately drive the partial figure negative.

---

## ADR-009: `auth.VerifyManagerApproval` as a Shared Second-Party Approval Primitive

* **Decision Date:** 2026-09-13
* **Status:** Accepted
* **Context:** Recording a Cash Movement requires approval by a second identity holding the Manager role, who authenticates inline with a login code and PIN. This differs from the Catalog pattern, which re-verifies the actor's own PIN. Phase 5 requires the identical mechanism for Refund, Payment Void, and Comp.
* **Decision:**
* The verification lives in `internal/auth` as `VerifyManagerApproval`, and slices supply only the capability the approver must hold.
* It locks the approver row with `FOR UPDATE`, runs PIN verification against a dummy hash when no identity matches so response timing reveals nothing, and reports one of four denial reasons.
* Callers collapse every denial reason to one client-visible code. The specific reason reaches the server log and the denial audit event only.
* **Consequences:**
* Security-critical verification is implemented and tested once rather than copied into each slice that needs it.
* This is a deliberate exception to the general rule that slices do not share helpers. It does not extend to idempotency helpers, which ADR-007 keeps slice-local.