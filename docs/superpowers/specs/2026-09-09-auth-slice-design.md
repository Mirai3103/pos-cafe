# Design Specification: Authentication & Staff Management Slice (`internal/auth`)

- **Author:** Antigravity & Team
- **Date:** 2026-09-09
- **Status:** Approved
- **Target Worktree:** `.worktrees/auth-slice` (`feat/auth-slice`)

---

## 1. Overview & Goals

This specification defines the complete architecture and implementation details for **Phase 1: `internal/auth`** in the POS Cafe backend migration from TypeScript (`cafe-pos`) to Golang (`pos-cafe`).

### Primary Objectives:
1. Provide ultra-fast, secure PIN-based authentication optimized for low-spec POS hardware (Celeron, 2–4GB RAM).
2. Support multi-role staff identities (`MANAGER`, `CASHIER`, `BARISTA`) with deterministic capability derivation.
3. Deliver a robust session lifecycle with POS screen locking (`active` vs `locked`) and inactivity timeouts.
4. Enforce strict administrative safety invariants (at least one enabled Manager must always exist) using PostgreSQL transactional advisory locks.
5. Standardize mutation request deduplication via a unified, reusable `idempotency_keys` table (ADR-005).
6. Provide a clean, idiomatic Go Vertical Slice structure matching the existing `internal/category` reference slice.

---

## 2. Database Schema & Migration

File: `internal/database/migrations/000002_create_auth_tables.sql`

```sql
-- 1. Staff Identities
CREATE TABLE IF NOT EXISTS staff_identities (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    display_name VARCHAR(120) NOT NULL,
    login_code VARCHAR(24) NOT NULL,
    pin_hash VARCHAR(72) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS staff_login_code_unique 
    ON staff_identities (upper(btrim(login_code)));

-- 2. Staff Operational Roles (Multi-Role Support)
CREATE TABLE IF NOT EXISTS staff_operational_roles (
    staff_identity_id UUID NOT NULL REFERENCES staff_identities(id) ON DELETE CASCADE,
    role VARCHAR(20) NOT NULL CHECK (role IN ('MANAGER', 'CASHIER', 'BARISTA')),
    PRIMARY KEY (staff_identity_id, role)
);

-- 3. Staff Access Sessions
CREATE TABLE IF NOT EXISTS staff_access_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash VARCHAR(64) NOT NULL UNIQUE,
    staff_identity_id UUID NOT NULL REFERENCES staff_identities(id) ON DELETE CASCADE,
    state VARCHAR(20) NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'locked')),
    active_workspace VARCHAR(20) CHECK (active_workspace IN ('cashier', 'manager', 'preparation')),
    last_authenticated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_human_activity_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_staff_access_sessions_lookup 
    ON staff_access_sessions (token_hash) 
    WHERE revoked_at IS NULL;

-- 4. Unified Idempotency Table (ADR-005)
CREATE TABLE IF NOT EXISTS idempotency_keys (
    key UUID NOT NULL,
    actor_id UUID NOT NULL REFERENCES staff_identities(id) ON DELETE CASCADE,
    action VARCHAR(50) NOT NULL,
    request_hash VARCHAR(64) NOT NULL,
    response_code INT NOT NULL,
    response_body JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (actor_id, key)
);

CREATE INDEX IF NOT EXISTS idx_idempotency_keys_created_at 
    ON idempotency_keys (created_at);
```

---

## 3. Domain Models & Security Architecture

### 3.1 Roles & Capabilities Mapping (`internal/auth/domain.go`)
- **Roles:**
  - `MANAGER`: Full operational and administrative access.
  - `CASHIER`: Front-of-house, price inspection, order submission, cash drawer operations.
  - `BARISTA`: Kitchen display operations, drink status updates, ingredient availability.
- **Capabilities Matrix:**
  ```go
  var RoleCapabilities = map[string][]string{
      "MANAGER": {
          "catalog.view_prices", "catalog.manage_availability", "catalog.administer_structure",
          "catalog.change_price", "sales.operate", "sales_shift.operate", "preparation.operate",
          "staff.administer", "audit.inspect", "tables.administer",
      },
      "CASHIER": {
          "catalog.view_prices", "catalog.manage_availability",
          "sales.operate", "sales_shift.operate",
      },
      "BARISTA": {
          "catalog.manage_availability", "preparation.operate",
      },
  }
  ```
- **Derivation Function:** `DeriveCapabilities(roles []string) []string` computes the deduplicated union of capabilities for the identity.

### 3.2 Security Rules & Advisory Locks
1. **PIN Format & Hashing:**
   - Format: 4–8 digits (`^\d{4,8}$`).
   - Hash: `golang.org/x/crypto/bcrypt` at `bcrypt.DefaultCost` (ADR-002).
   - Timing Protection: Verification on non-existent `login_code` checks against a fixed dummy bcrypt hash to ensure constant response latency.
2. **Bootstrap Advisory Lock:**
   - Lock ID: `739201` (`SELECT pg_advisory_xact_lock(739201)`).
   - Guarantees exactly one manager is created during first-boot bootstrapping.
3. **Enabled Manager Invariant Lock:**
   - Lock ID: `1247091103` (`SELECT pg_advisory_xact_lock(1247091103)`).
   - Executed inside the transaction when modifying staff enabled status or roles.
   - Throws error `FINAL_ENABLED_MANAGER_REQUIRED` (HTTP 409) if an action would leave zero enabled managers in the database.
4. **Active Session Revocation:**
   - Whenever an identity is disabled (`enabled = false`) or its PIN is reset, all active sessions for that identity are revoked immediately (`UPDATE staff_access_sessions SET revoked_at = now() WHERE staff_identity_id = $1 AND revoked_at IS NULL`).

### 3.3 Session Management
- **Token Generation:** 32 cryptographically secure random bytes via `crypto/rand`, base64url encoded.
- **Storage:** SHA-256 hash of the token string stored in `staff_access_sessions.token_hash`.
- **Hybrid Delivery (ADR-003):** Token returned in JSON response body and set as `staff_session_token` HTTP-only Cookie.
- **Inactivity Timeout:**
  - Cashier/Manager: 5 minutes.
  - Preparation: 15 minutes.
  - Transition: Automatically evaluated during session lookup or activity check. If timeout exceeded, state transitions from `active` to `locked`.

---

## 4. API Specification

All routes registered under Echo API group `/api/v1`.

### 4.1 Authentication Group (`/api/v1/auth`)

#### 1. `POST /api/v1/auth/bootstrap`
- **Access:** Public (Only succeeds if 0 managers exist).
- **Body:** `{ "display_name": string, "login_code": string, "pin": string }`
- **Responses:** `201 Created` with staff object, or `409 Conflict` ("Manager already configured").

#### 2. `GET /api/v1/auth/identities`
- **Access:** Public.
- **Query:** Returns active staff list for POS lock screen selection: `[{ "display_name": string, "login_code": string }]`.
- **Responses:** `200 OK`.

#### 3. `POST /api/v1/auth/sign-in`
- **Access:** Public.
- **Body:** `{ "login_code": string, "pin": string }`
- **Responses:** `200 OK` with `{ "token": string, "staff": { "id": uuid, "display_name": string, "login_code": string, "roles": string[], "capabilities": string[] } }` + `Set-Cookie`.
- **Errors:** `401 Unauthorized` ("Thông tin đăng nhập không hợp lệ" or "Tài khoản nhân viên đã bị vô hiệu hóa").

#### 4. `GET /api/v1/auth/session`
- **Access:** Hybrid (Bearer Token or Cookie).
- **Responses:** `200 OK` with session state (`authenticated`, `locked`, or `signed_out`).

#### 5. `POST /api/v1/auth/lock`
- **Access:** Authenticated.
- **Responses:** `200 OK` with `{ "state": "locked" }`.

#### 6. `POST /api/v1/auth/unlock`
- **Access:** Authenticated (Locked session).
- **Body:** `{ "pin": string }`
- **Responses:** `200 OK` with `{ "state": "authenticated", "staff": ... }`.
- **Errors:** `401 Unauthorized` ("Mã PIN không đúng").

#### 7. `POST /api/v1/auth/sign-out`
- **Access:** Authenticated.
- **Responses:** `200 OK` with `{ "state": "signed_out" }` + Cleared cookie.

#### 8. `POST /api/v1/auth/workspace`
- **Access:** Authenticated.
- **Body:** `{ "workspace": "cashier" | "manager" | "preparation" }`
- **Responses:** `200 OK` with `{ "workspace": string }`.
- **Errors:** `403 Forbidden` if missing required capability.

#### 9. `POST /api/v1/auth/activity`
- **Access:** Authenticated.
- **Responses:** `200 OK` with `{ "recorded": true }`.

---

### 4.2 Staff Administration Group (`/api/v1/staff`)
*Protected by `RequireAuth("MANAGER")`.*

#### 1. `GET /api/v1/staff/me`
- Returns profile of the current authenticated actor.

#### 2. `GET /api/v1/staff`
- Lists all staff members with their roles and enabled status.

#### 3. `POST /api/v1/staff`
- **Idempotency:** Checked against `idempotency_keys` with action `'staff.create'`.
- **Body:** `{ "request_id": uuid, "display_name": string, "login_code": string, "enabled": bool, "roles": string[], "pin": string, "manager_pin": string }`
- **Responses:** `201 Created` with created staff identity.
- **Errors:** `403 Forbidden` ("PIN Quản lý không đúng"), `409 Conflict` ("Mã đăng nhập đã được sử dụng" or "Request conflict").

#### 4. `PUT /api/v1/staff/:id/roles`
- **Idempotency:** Checked against `idempotency_keys` with action `'staff.replace_roles'`.
- **Body:** `{ "request_id": uuid, "roles": string[], "manager_pin": string }`
- **Responses:** `200 OK`.
- **Errors:** `409 Conflict` if removing `MANAGER` from the last active manager.

#### 5. `PATCH /api/v1/staff/:id/enabled`
- **Idempotency:** Checked against `idempotency_keys` with action `'staff.set_enabled'`.
- **Body:** `{ "request_id": uuid, "expected_enabled": bool, "enabled": bool, "manager_pin": string }`
- **Responses:** `200 OK`.
- **Errors:** `409 Conflict` if disabling the last active manager or state is stale.

#### 6. `POST /api/v1/staff/:id/reset-pin`
- **Idempotency:** Checked against `idempotency_keys` with action `'staff.reset_pin'`.
- **Body:** `{ "request_id": uuid, "pin": string, "manager_pin": string }`
- **Responses:** `200 OK`.

---

## 5. Middleware Design (`internal/auth/middleware.go`)

### 5.1 `StaffContext` & Helper
```go
type StaffClaims struct {
    StaffID      uuid.UUID
    SessionID    uuid.UUID
    DisplayName  string
    LoginCode    string
    Roles        []string
    Capabilities []string
    Workspace    *string
}

func GetStaff(c echo.Context) *StaffClaims
```

### 5.2 Middlewares
1. **`RequireAuth(allowedRoles ...string) echo.MiddlewareFunc`**
   - Validates Bearer token or Cookie.
   - Verifies session in database (`revoked_at IS NULL`, `expires_at > now()`, `state == 'active'`).
   - If `allowedRoles` provided, verifies that `Roles` contains at least one allowed role.
2. **`RequireCapability(capability string) echo.MiddlewareFunc`**
   - Checks that `Capabilities` contains the required permission flag.

---

## 6. Implementation File Plan

Within `.worktrees/auth-slice/internal/auth`:
- `domain.go`: Roles, capabilities, timeouts, hashing/fingerprint helpers.
- `dto.go`: Input command structs, validation tags, response structs.
- `middleware.go`: Echo middlewares and context extraction.
- `routes.go`: Route registration for `/api/v1/auth` and `/api/v1/staff`.
- `bootstrap_manager.go`: First manager initialization with advisory lock.
- `sign_in.go`, `sign_out.go`, `get_session.go`, `lock_session.go`, `unlock_session.go`: Core auth handlers.
- `declare_workspace.go`, `record_activity.go`, `list_identities.go`: POS terminal handlers.
- `staff_me.go`, `staff_list.go`: Staff retrieval queries.
- `staff_create.go`, `staff_replace_roles.go`, `staff_set_enabled.go`, `staff_reset_pin.go`: Staff management handlers with idempotency.
- `idempotency.go`: Helper for checking/recording idempotency transactions.
- `auth_test.go`: Unit tests for domain logic, password verification, and validation.
- `auth_integration_test.go`: End-to-end integration tests using PostgreSQL test database.

---

## 7. Verification & Acceptance Criteria
1. `go test -v ./internal/auth/...` passes 100%.
2. Clean baseline regression check: `go test ./...` passes across the entire project.
3. Database migrations execute automatically on startup without error.
4. Swagger docs generated and verified with `swag init`.
5. Idempotent replays tested against concurrent and duplicated requests.
