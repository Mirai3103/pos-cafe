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