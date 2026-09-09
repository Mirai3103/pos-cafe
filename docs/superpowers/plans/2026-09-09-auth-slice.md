# Authentication & Staff Management (`internal/auth`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement Phase 1: Authentication & Staff Management (`internal/auth`) vertical slice, providing PIN-based login, multi-role RBAC, session lifecycle management, manager bootstrapping, staff administration with deduplication, and Echo middlewares.

**Architecture:** Vertical Slice Architecture with CQRS commands/queries. Each feature has its own file encapsulating Command/Query, Interface dependency, Handler, and Echo HTTP adapter. Centralized PostgreSQL access via `sqlc`, transaction management with `database.TxManager`, and unified idempotency tracking via `idempotency_keys`.

**Tech Stack:** Go 1.26+, Echo v4, PostgreSQL, pgx/v5 (std `database/sql` driver wrapper), sqlc, `golang.org/x/crypto/bcrypt`, `github.com/google/uuid`, swaggo/swag.

**Spec:** [`docs/superpowers/specs/2026-09-09-auth-slice-design.md`](file:///home/laffy/Desktop/go-vertical-slice-template-main/pos-cafe/.worktrees/auth-slice/docs/superpowers/specs/2026-09-09-auth-slice-design.md)  
**Decisions:** [`spec/decisions.md`](file:///home/laffy/Desktop/go-vertical-slice-template-main/pos-cafe/.worktrees/auth-slice/spec/decisions.md)

## Global Constraints

- Working directory for all implementation: `.worktrees/auth-slice/`
- PIN format must be 4–8 digits (`^\d{4,8}$`) hashed using `bcrypt` (ADR-002)
- Multi-role support (`MANAGER`, `CASHIER`, `BARISTA`) via `staff_operational_roles` (ADR-001)
- Explicit `VARCHAR` column lengths in DB migrations (ADR-004)
- Unified idempotency via `idempotency_keys` table using `request_id UUID` (ADR-005)
- Hybrid session token delivery: response body `{ "token": "..." }` + HTTP-only cookie `staff_session_token` (ADR-003)
- Advisory lock `739201` for Manager bootstrap; advisory lock `1247091103` for Enabled Manager invariant
- All API responses must use uniform envelope `{ "success": bool, "data": ..., "error": ... }` via `internal/response` package

---

### Task 1: Database Migration & SQLC Queries

**Files:**
- Create: `internal/database/migrations/000002_create_auth_tables.sql`
- Create: `sql/queries/auth.sql`
- Output: `internal/database/sqlc/auth.sql.go`, `internal/database/sqlc/models.go`, `internal/database/sqlc/querier.go`

**Interfaces:**
- Consumes: PostgreSQL schema embedded via `internal/database/migrations`
- Produces: `sqlc.Querier` methods for auth & staff management

- [x] **Step 1: Write database migration SQL**

Create `internal/database/migrations/000002_create_auth_tables.sql`:
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

- [x] **Step 2: Write sqlc queries for auth**

Create `sql/queries/auth.sql`:
```sql
-- name: GetStaffByLoginCode :one
SELECT id, display_name, login_code, pin_hash, enabled, created_at
FROM staff_identities
WHERE upper(btrim(login_code)) = upper(btrim($1))
LIMIT 1;

-- name: GetStaffByID :one
SELECT id, display_name, login_code, pin_hash, enabled, created_at
FROM staff_identities
WHERE id = $1
LIMIT 1;

-- name: ListActiveIdentities :many
SELECT display_name, login_code
FROM staff_identities
WHERE enabled = true
ORDER BY display_name ASC;

-- name: ListAllStaff :many
SELECT id, display_name, login_code, enabled, created_at
FROM staff_identities
ORDER BY display_name ASC;

-- name: GetStaffRoles :many
SELECT role
FROM staff_operational_roles
WHERE staff_identity_id = $1
ORDER BY role ASC;

-- name: ListAllStaffRoles :many
SELECT staff_identity_id, role
FROM staff_operational_roles
ORDER BY staff_identity_id, role ASC;

-- name: CreateStaffIdentity :one
INSERT INTO staff_identities (display_name, login_code, pin_hash, enabled)
VALUES ($1, upper(btrim($2)), $3, $4)
RETURNING id, display_name, login_code, enabled, created_at;

-- name: SetStaffEnabled :one
UPDATE staff_identities
SET enabled = $2
WHERE id = $1
RETURNING id, display_name, login_code, enabled;

-- name: UpdateStaffPin :exec
UPDATE staff_identities
SET pin_hash = $2
WHERE id = $1;

-- name: AddStaffRole :exec
INSERT INTO staff_operational_roles (staff_identity_id, role)
VALUES ($1, $2)
ON CONFLICT (staff_identity_id, role) DO NOTHING;

-- name: ClearStaffRoles :exec
DELETE FROM staff_operational_roles
WHERE staff_identity_id = $1;

-- name: CountActiveManagers :one
SELECT count(DISTINCT si.id)::bigint
FROM staff_identities si
JOIN staff_operational_roles sor ON sor.staff_identity_id = si.id
WHERE si.enabled = true AND sor.role = 'MANAGER';

-- name: CreateStaffSession :one
INSERT INTO staff_access_sessions (
    token_hash, staff_identity_id, state, active_workspace, 
    last_authenticated_at, last_human_activity_at, expires_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, token_hash, staff_identity_id, state, active_workspace, expires_at, revoked_at;

-- name: GetSessionByTokenHash :one
SELECT 
    s.id AS session_id,
    s.token_hash,
    s.staff_identity_id,
    s.state AS session_state,
    s.active_workspace,
    s.last_authenticated_at,
    s.last_human_activity_at,
    s.expires_at,
    s.revoked_at,
    si.display_name,
    si.login_code,
    si.pin_hash,
    si.enabled AS identity_enabled
FROM staff_access_sessions s
JOIN staff_identities si ON si.id = s.staff_identity_id
WHERE s.token_hash = $1
LIMIT 1;

-- name: UpdateSessionState :exec
UPDATE staff_access_sessions
SET state = $2
WHERE id = $1 AND revoked_at IS NULL;

-- name: UpdateSessionActivity :exec
UPDATE staff_access_sessions
SET last_human_activity_at = $2
WHERE id = $1 AND revoked_at IS NULL;

-- name: UpdateSessionWorkspace :exec
UPDATE staff_access_sessions
SET active_workspace = $2
WHERE id = $1 AND revoked_at IS NULL;

-- name: RevokeSession :exec
UPDATE staff_access_sessions
SET revoked_at = now()
WHERE id = $1;

-- name: RevokeAllStaffSessions :exec
UPDATE staff_access_sessions
SET revoked_at = now()
WHERE staff_identity_id = $1 AND revoked_at IS NULL;

-- name: GetIdempotencyKey :one
SELECT key, actor_id, action, request_hash, response_code, response_body, created_at
FROM idempotency_keys
WHERE actor_id = $1 AND key = $2
LIMIT 1;

-- name: InsertIdempotencyKey :exec
INSERT INTO idempotency_keys (key, actor_id, action, request_hash, response_code, response_body)
VALUES ($1, $2, $3, $4, $5, $6);
```

- [x] **Step 3: Run `sqlc generate`**

Run: `sqlc generate` in `.worktrees/auth-slice/`  
Expected: Generates `internal/database/sqlc/auth.sql.go` without error.

- [x] **Step 4: Verify migration & sqlc compile**

Run: `go test ./...` in `.worktrees/auth-slice/`  
Expected: PASS (Existing category tests continue to pass).

- [x] **Step 5: Commit**

```bash
git add internal/database/migrations/000002_create_auth_tables.sql sql/queries/auth.sql internal/database/sqlc/
git commit -m "feat(auth): add database migrations and sqlc queries"
```

---

### Task 2: Domain Models & Core Security Helpers

**Files:**
- Create: `internal/auth/domain.go`
- Create: `internal/auth/domain_test.go`

**Interfaces:**
- Consumes: standard library `crypto/rand`, `crypto/sha256`, `golang.org/x/crypto/bcrypt`
- Produces: `Role*` constants, `Capability*` constants, `DeriveCapabilities`, `ValidatePinFormat`, `HashPin`, `VerifyPin`, `GenerateToken`, `HashToken`, `InactivityTimeout`

- [x] **Step 1: Write failing unit test for domain helpers**

Create `internal/auth/domain_test.go`:
```go
package auth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidatePinFormat(t *testing.T) {
	assert.NoError(t, ValidatePinFormat("1234"))
	assert.NoError(t, ValidatePinFormat("12345678"))
	assert.Error(t, ValidatePinFormat("123"))      // too short
	assert.Error(t, ValidatePinFormat("123456789")) // too long
	assert.Error(t, ValidatePinFormat("123a"))      // non-digit
	assert.Error(t, ValidatePinFormat(""))          // empty
}

func TestHashAndVerifyPin(t *testing.T) {
	pin := "123456"
	hash, err := HashPin(pin)
	require.NoError(t, err)
	assert.NotEmpty(t, hash)

	assert.True(t, VerifyPin(hash, "123456"))
	assert.False(t, VerifyPin(hash, "654321"))
	assert.False(t, VerifyPin("", "123456"))
}

func TestDeriveCapabilities(t *testing.T) {
	// Cashier
	cashierCaps := DeriveCapabilities([]string{RoleCashier})
	assert.Contains(t, cashierCaps, "sales.operate")
	assert.Contains(t, cashierCaps, "catalog.view_prices")
	assert.NotContains(t, cashierCaps, "preparation.operate")
	assert.NotContains(t, cashierCaps, "staff.administer")

	// Barista
	baristaCaps := DeriveCapabilities([]string{RoleBarista})
	assert.Contains(t, baristaCaps, "preparation.operate")
	assert.NotContains(t, baristaCaps, "sales.operate")

	// Manager
	managerCaps := DeriveCapabilities([]string{RoleManager})
	assert.Contains(t, managerCaps, "staff.administer")
	assert.Contains(t, managerCaps, "sales.operate")
	assert.Contains(t, managerCaps, "preparation.operate")

	// Combined multi-role deduplication
	combined := DeriveCapabilities([]string{RoleCashier, RoleBarista})
	assert.Contains(t, combined, "sales.operate")
	assert.Contains(t, combined, "preparation.operate")
}

func TestTokenGenerationAndHashing(t *testing.T) {
	token, hash, err := GenerateSessionToken()
	require.NoError(t, err)
	assert.NotEmpty(t, token)
	assert.Len(t, hash, 64) // SHA-256 hex string is 64 chars

	assert.Equal(t, hash, HashToken(token))
}

func TestInactivityTimeout(t *testing.T) {
	assert.Equal(t, 5*time.Minute, GetInactivityTimeout(WorkspaceCashier))
	assert.Equal(t, 5*time.Minute, GetInactivityTimeout(WorkspaceManager))
	assert.Equal(t, 15*time.Minute, GetInactivityTimeout(WorkspacePreparation))
	assert.Equal(t, 5*time.Minute, GetInactivityTimeout(""))
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/auth/...` in `.worktrees/auth-slice/`  
Expected: FAIL (types and functions not defined).

- [x] **Step 3: Implement domain logic**

Create `internal/auth/domain.go`:
```go
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"regexp"
	"slices"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Operational Roles
const (
	RoleManager = "MANAGER"
	RoleCashier = "CASHIER"
	RoleBarista = "BARISTA"
)

var AllRoles = []string{RoleManager, RoleCashier, RoleBarista}

// Workspaces
const (
	WorkspaceCashier     = "cashier"
	WorkspaceManager     = "manager"
	WorkspacePreparation = "preparation"
)

// Session States
const (
	SessionStateActive = "active"
	SessionStateLocked = "locked"
)

// Inactivity Timeouts
const (
	CashierManagerInactivityTimeout = 5 * time.Minute
	PreparationInactivityTimeout   = 15 * time.Minute
	SessionDuration                = 12 * time.Hour
)

// Capabilities
var RoleCapabilities = map[string][]string{
	RoleManager: {
		"catalog.view_prices",
		"catalog.manage_availability",
		"catalog.administer_structure",
		"catalog.change_price",
		"sales.operate",
		"sales_shift.operate",
		"preparation.operate",
		"staff.administer",
		"audit.inspect",
		"tables.administer",
	},
	RoleCashier: {
		"catalog.view_prices",
		"catalog.manage_availability",
		"sales.operate",
		"sales_shift.operate",
	},
	RoleBarista: {
		"catalog.manage_availability",
		"preparation.operate",
	},
}

var pinRegex = regexp.MustCompile(`^\d{4,8}$`)

// Dummy hash for constant-time comparison when loginCode is unknown
const dummyBcryptHash = "$2a$10$92IXUNpkjO0rOQ5byMi.Ye4oKoEa3Ro9llC/.og/at2.uheWG/igi"

func ValidatePinFormat(pin string) error {
	if !pinRegex.MatchString(pin) {
		return errors.New("mã PIN phải có từ 4 đến 8 chữ số")
	}
	return nil
}

func HashPin(pin string) (string, error) {
	if err := ValidatePinFormat(pin); err != nil {
		return "", err
	}
	bytes, err := bcrypt.GenerateFromPassword([]byte(pin), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func VerifyPin(pinHash, attemptedPin string) bool {
	target := pinHash
	if target == "" {
		target = dummyBcryptHash
	}
	err := bcrypt.CompareHashAndPassword([]byte(target), []byte(attemptedPin))
	return err == nil && pinHash != ""
}

func DeriveCapabilities(roles []string) []string {
	seen := make(map[string]struct{})
	var result []string
	for _, role := range roles {
		caps, ok := RoleCapabilities[role]
		if !ok {
			continue
		}
		for _, c := range caps {
			if _, exists := seen[c]; !exists {
				seen[c] = struct{}{}
				result = append(result, c)
			}
		}
	}
	slices.Sort(result)
	return result
}

func GenerateSessionToken() (token string, tokenHash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	tokenHash = HashToken(token)
	return token, tokenHash, nil
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func GetInactivityTimeout(workspace string) time.Duration {
	if workspace == WorkspacePreparation {
		return PreparationInactivityTimeout
	}
	return CashierManagerInactivityTimeout
}
```

- [x] **Step 4: Run tests to verify they pass**

Run: `go test -v ./internal/auth/...` in `.worktrees/auth-slice/`  
Expected: PASS.

- [x] **Step 5: Commit**

```bash
git add internal/auth/domain.go internal/auth/domain_test.go
git commit -m "feat(auth): implement domain models, capabilities, and security helpers"
```

---

### Task 3: DTOs & Validation Schema

**Files:**
- Create: `internal/auth/dto.go`
- Create: `internal/auth/dto_test.go`

**Interfaces:**
- Consumes: `github.com/google/uuid`, `internal/httpvalidator`
- Produces: Request & Response structs for all auth & staff endpoints

- [x] **Step 1: Write unit test for DTO validation**

Create `internal/auth/dto_test.go`:
```go
package auth

import (
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/httpvalidator"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestSignInRequestValidation(t *testing.T) {
	v := httpvalidator.New()

	valid := SignInRequest{
		LoginCode: "QL01",
		Pin:       "1234",
	}
	assert.NoError(t, v.Validate(&valid))

	invalid := SignInRequest{
		LoginCode: "",
		Pin:       "12", // too short
	}
	assert.Error(t, v.Validate(&invalid))
}

func TestCreateStaffRequestValidation(t *testing.T) {
	v := httpvalidator.New()

	valid := CreateStaffRequest{
		RequestID:   uuid.New(),
		DisplayName: "Nguyễn Văn A",
		LoginCode:   "NV01",
		Enabled:     true,
		Roles:       []string{RoleCashier},
		Pin:         "1234",
		ManagerPin:  "9999",
	}
	assert.NoError(t, v.Validate(&valid))

	invalid := CreateStaffRequest{
		RequestID:   uuid.Nil,
		DisplayName: "",
		Roles:       []string{},
		Pin:         "123",
	}
	assert.Error(t, v.Validate(&invalid))
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/auth/...`  
Expected: FAIL.

- [x] **Step 3: Implement DTOs**

Create `internal/auth/dto.go`:
```go
package auth

import (
	"time"

	"github.com/google/uuid"
)

// === Authentication DTOs ===

type BootstrapManagerRequest struct {
	DisplayName string `json:"display_name" validate:"required,min=2,max=120"`
	LoginCode   string `json:"login_code" validate:"required,min=2,max=24,alphanumunicode"`
	Pin         string `json:"pin" validate:"required,min=4,max=8,numeric"`
}

type SignInRequest struct {
	LoginCode string `json:"login_code" validate:"required,min=1,max=24"`
	Pin       string `json:"pin" validate:"required,min=4,max=8,numeric"`
}

type UnlockRequest struct {
	Pin string `json:"pin" validate:"required,min=4,max=8,numeric"`
}

type DeclareWorkspaceRequest struct {
	Workspace string `json:"workspace" validate:"required,oneof=cashier manager preparation"`
}

type IdentitySummaryResponse struct {
	DisplayName string `json:"display_name"`
	LoginCode   string `json:"login_code"`
}

type StaffProfileResponse struct {
	ID           uuid.UUID `json:"id"`
	DisplayName  string    `json:"display_name"`
	LoginCode    string    `json:"login_code"`
	Enabled      bool      `json:"enabled"`
	Roles        []string  `json:"roles"`
	Capabilities []string  `json:"capabilities"`
}

type SignInResponse struct {
	Token string               `json:"token"`
	Staff StaffProfileResponse `json:"staff"`
}

type SessionStateResponse struct {
	State        string    `json:"state"` // "authenticated", "locked", "signed_out"
	StaffID      uuid.UUID `json:"staff_id,omitempty"`
	DisplayName  string    `json:"display_name,omitempty"`
	LoginCode    string    `json:"login_code,omitempty"`
	Roles        []string  `json:"roles,omitempty"`
	Capabilities []string  `json:"capabilities,omitempty"`
	Workspace    *string   `json:"workspace,omitempty"`
}

// === Staff Administration DTOs ===

type CreateStaffRequest struct {
	RequestID   uuid.UUID `json:"request_id" validate:"required"`
	DisplayName string    `json:"display_name" validate:"required,min=1,max=120"`
	LoginCode   string    `json:"login_code" validate:"required,min=1,max=24"`
	Enabled     bool      `json:"enabled"`
	Roles       []string  `json:"roles" validate:"required,min=1,dive,oneof=MANAGER CASHIER BARISTA"`
	Pin         string    `json:"pin" validate:"required,min=4,max=8,numeric"`
	ManagerPin  string    `json:"manager_pin" validate:"required,min=4,max=8,numeric"`
}

type SetStaffEnabledRequest struct {
	RequestID       uuid.UUID `json:"request_id" validate:"required"`
	ExpectedEnabled bool      `json:"expected_enabled"`
	Enabled         bool      `json:"enabled"`
	ManagerPin      string    `json:"manager_pin" validate:"required,min=4,max=8,numeric"`
}

type ReplaceStaffRolesRequest struct {
	RequestID  uuid.UUID `json:"request_id" validate:"required"`
	Roles      []string  `json:"roles" validate:"required,min=1,dive,oneof=MANAGER CASHIER BARISTA"`
	ManagerPin string    `json:"manager_pin" validate:"required,min=4,max=8,numeric"`
}

type ResetStaffPinRequest struct {
	RequestID  uuid.UUID `json:"request_id" validate:"required"`
	Pin        string    `json:"pin" validate:"required,min=4,max=8,numeric"`
	ManagerPin string    `json:"manager_pin" validate:"required,min=4,max=8,numeric"`
}

type StaffDetailResponse struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"display_name"`
	LoginCode   string    `json:"login_code"`
	Enabled     bool      `json:"enabled"`
	Roles       []string  `json:"roles"`
	CreatedAt   time.Time `json:"created_at"`
}
```

- [x] **Step 4: Run tests to verify they pass**

Run: `go test -v ./internal/auth/...`  
Expected: PASS.

- [x] **Step 5: Commit**

```bash
git add internal/auth/dto.go internal/auth/dto_test.go
git commit -m "feat(auth): define DTO models and validation rules"
```

---

### Task 4: Public Auth Slices (Sign In, Unlock, Session, Lock, Sign Out, Workspace, Activity, Identities)

**Files:**
- Create: `internal/auth/sign_in.go`
- Create: `internal/auth/unlock_session.go`
- Create: `internal/auth/get_session.go`
- Create: `internal/auth/lock_session.go`
- Create: `internal/auth/sign_out.go`
- Create: `internal/auth/declare_workspace.go`
- Create: `internal/auth/record_activity.go`
- Create: `internal/auth/list_identities.go`

**Interfaces:**
- Consumes: `sqlc.Querier`, `database.TxManager`, `internal/response`
- Produces: HTTP Handlers for `/api/v1/auth/*`

- [x] **Step 1: Write `sign_in.go` handler & cookie helper**

Create `internal/auth/sign_in.go`:
```go
package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

const SessionCookieName = "staff_session_token"

func setSessionCookie(c echo.Context, token string, maxAge int) {
	c.SetCookie(&http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   c.Scheme() == "https",
	})
}

func clearSessionCookie(c echo.Context) {
	setSessionCookie(c, "", -1)
}

type SignInHandler struct {
	queries *sqlc.Queries
}

func NewSignInHandler(queries *sqlc.Queries) *SignInHandler {
	return &SignInHandler{queries: queries}
}

func (h *SignInHandler) Handle(ctx context.Context, req SignInRequest) (*SignInResponse, error) {
	staff, err := h.queries.GetStaffByLoginCode(ctx, req.LoginCode)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			VerifyPin("", req.Pin) // Constant time check
			return nil, fmt.Errorf("%w: thông tin đăng nhập không chính xác", response.ErrInvalid)
		}
		return nil, fmt.Errorf("lookup staff: %w", err)
	}

	if !VerifyPin(staff.PinHash, req.Pin) {
		return nil, fmt.Errorf("%w: thông tin đăng nhập không chính xác", response.ErrInvalid)
	}

	if !staff.Enabled {
		return nil, fmt.Errorf("%w: tài khoản nhân viên đã bị vô hiệu hóa", response.ErrForbidden)
	}

	roles, err := h.queries.GetStaffRoles(ctx, staff.ID)
	if err != nil {
		return nil, fmt.Errorf("lookup staff roles: %w", err)
	}

	token, tokenHash, err := GenerateSessionToken()
	if err != nil {
		return nil, fmt.Errorf("generate session token: %w", err)
	}

	now := time.Now().UTC()
	expiresAt := now.Add(SessionDuration)

	_, err = h.queries.CreateStaffSession(ctx, sqlc.CreateStaffSessionParams{
		TokenHash:            tokenHash,
		StaffIdentityID:      staff.ID,
		State:                SessionStateActive,
		ActiveWorkspace:      sql.NullString{},
		LastAuthenticatedAt: now,
		LastHumanActivityAt: now,
		ExpiresAt:            expiresAt,
	})
	if err != nil {
		return nil, fmt.Errorf("create staff session: %w", err)
	}

	caps := DeriveCapabilities(roles)

	return &SignInResponse{
		Token: token,
		Staff: StaffProfileResponse{
			ID:           staff.ID,
			DisplayName:  staff.DisplayName,
			LoginCode:    staff.LoginCode,
			Enabled:      staff.Enabled,
			Roles:        roles,
			Capabilities: caps,
		},
	}, nil
}

// HandleHTTP godoc
// @Summary Đăng nhập bằng mã PIN
// @Description Xác thực nhân viên bằng login_code và pin 4-8 số
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body SignInRequest true "Sign in credentials"
// @Success 200 {object} response.APIResponse{data=SignInResponse}
// @Failure 400 {object} response.APIResponse
// @Failure 401 {object} response.APIResponse
// @Router /auth/sign-in [post]
func (h *SignInHandler) HandleHTTP(c echo.Context) error {
	var req SignInRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, fmt.Errorf("%w: invalid payload", response.ErrInvalid))
	}
	if err := c.Validate(&req); err != nil {
		return response.Error(c, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error()))
	}

	res, err := h.Handle(c.Request().Context(), req)
	if err != nil {
		return response.Error(c, err)
	}

	setSessionCookie(c, res.Token, int(SessionDuration.Seconds()))
	return response.OK(c, res)
}
```

- [x] **Step 2: Implement `get_session.go`, `lock_session.go`, `unlock_session.go`, `sign_out.go`**

Create `internal/auth/get_session.go`:
```go
package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/labstack/echo/v4"
)

func extractToken(c echo.Context) string {
	authHeader := c.Request().Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		return strings.TrimPrefix(authHeader, "Bearer ")
	}
	if cookie, err := c.Cookie(SessionCookieName); err == nil && cookie.Value != "" {
		return cookie.Value
	}
	return ""
}

type GetSessionHandler struct {
	queries *sqlc.Queries
}

func NewGetSessionHandler(queries *sqlc.Queries) *GetSessionHandler {
	return &GetSessionHandler{queries: queries}
}

func (h *GetSessionHandler) Handle(ctx context.Context, token string) (*SessionStateResponse, error) {
	if token == "" {
		return &SessionStateResponse{State: "signed_out"}, nil
	}

	tokenHash := HashToken(token)
	sess, err := h.queries.GetSessionByTokenHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &SessionStateResponse{State: "signed_out"}, nil
		}
		return nil, fmt.Errorf("lookup session: %w", err)
	}

	if sess.RevokedAt.Valid || time.Now().UTC().After(sess.ExpiresAt) || !sess.IdentityEnabled {
		return &SessionStateResponse{State: "signed_out"}, nil
	}

	roles, err := h.queries.GetStaffRoles(ctx, sess.StaffIdentityID)
	if err != nil {
		return nil, fmt.Errorf("lookup roles: %w", err)
	}

	workspace := ""
	if sess.ActiveWorkspace.Valid {
		workspace = sess.ActiveWorkspace.String
	}

	// Inactivity Check
	timeout := GetInactivityTimeout(workspace)
	if sess.SessionState == SessionStateActive && time.Now().UTC().Sub(sess.LastHumanActivityAt) >= timeout {
		_ = h.queries.UpdateSessionState(ctx, sqlc.UpdateSessionStateParams{
			ID:    sess.SessionID,
			State: SessionStateLocked,
		})
		sess.SessionState = SessionStateLocked
	}

	var wsPtr *string
	if workspace != "" {
		wsPtr = &workspace
	}

	caps := DeriveCapabilities(roles)
	return &SessionStateResponse{
		State:        sess.SessionState,
		StaffID:      sess.StaffIdentityID,
		DisplayName:  sess.DisplayName,
		LoginCode:    sess.LoginCode,
		Roles:        roles,
		Capabilities: caps,
		Workspace:    wsPtr,
	}, nil
}

func (h *GetSessionHandler) HandleHTTP(c echo.Context) error {
	token := extractToken(c)
	res, err := h.Handle(c.Request().Context(), token)
	if err != nil {
		return response.Error(c, err)
	}
	return response.OK(c, res)
}
```

Create `internal/auth/lock_session.go`:
```go
package auth

import (
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/labstack/echo/v4"
)

type LockSessionHandler struct {
	queries *sqlc.Queries
}

func NewLockSessionHandler(queries *sqlc.Queries) *LockSessionHandler {
	return &LockSessionHandler{queries: queries}
}

func (h *LockSessionHandler) HandleHTTP(c echo.Context) error {
	staff := GetStaff(c)
	if staff == nil {
		return response.Error(c, fmt.Errorf("%w: unauthorized", response.ErrForbidden))
	}

	err := h.queries.UpdateSessionState(c.Request().Context(), sqlc.UpdateSessionStateParams{
		ID:    staff.SessionID,
		State: SessionStateLocked,
	})
	if err != nil {
		return response.Error(c, fmt.Errorf("lock session: %w", err))
	}

	return response.OK(c, map[string]string{"state": SessionStateLocked})
}
```

Create `internal/auth/unlock_session.go`:
```go
package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/labstack/echo/v4"
)

type UnlockSessionHandler struct {
	queries *sqlc.Queries
}

func NewUnlockSessionHandler(queries *sqlc.Queries) *UnlockSessionHandler {
	return &UnlockSessionHandler{queries: queries}
}

func (h *UnlockSessionHandler) Handle(ctx context.Context, token string, pin string) (*SignInResponse, error) {
	if token == "" {
		return nil, fmt.Errorf("%w: session không tồn tại", response.ErrForbidden)
	}

	tokenHash := HashToken(token)
	sess, err := h.queries.GetSessionByTokenHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: session không hợp lệ", response.ErrForbidden)
		}
		return nil, fmt.Errorf("lookup session: %w", err)
	}

	if sess.RevokedAt.Valid || !sess.IdentityEnabled {
		return nil, fmt.Errorf("%w: session đã hết hạn", response.ErrForbidden)
	}

	if !VerifyPin(sess.PinHash, pin) {
		return nil, fmt.Errorf("%w: mã PIN không đúng", response.ErrInvalid)
	}

	if err := h.queries.UpdateSessionState(ctx, sqlc.UpdateSessionStateParams{
		ID:    sess.SessionID,
		State: SessionStateActive,
	}); err != nil {
		return nil, fmt.Errorf("unlock session: %w", err)
	}

	roles, err := h.queries.GetStaffRoles(ctx, sess.StaffIdentityID)
	if err != nil {
		return nil, fmt.Errorf("lookup roles: %w", err)
	}

	return &SignInResponse{
		Token: token,
		Staff: StaffProfileResponse{
			ID:           sess.StaffIdentityID,
			DisplayName:  sess.DisplayName,
			LoginCode:    sess.LoginCode,
			Enabled:      sess.IdentityEnabled,
			Roles:        roles,
			Capabilities: DeriveCapabilities(roles),
		},
	}, nil
}

func (h *UnlockSessionHandler) HandleHTTP(c echo.Context) error {
	token := extractToken(c)
	var req UnlockRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, fmt.Errorf("%w: invalid payload", response.ErrInvalid))
	}
	if err := c.Validate(&req); err != nil {
		return response.Error(c, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error()))
	}

	res, err := h.Handle(c.Request().Context(), token, req.Pin)
	if err != nil {
		return response.Error(c, err)
	}

	return response.OK(c, res)
}
```

Create `internal/auth/sign_out.go`:
```go
package auth

import (
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/labstack/echo/v4"
)

type SignOutHandler struct {
	queries *sqlc.Queries
}

func NewSignOutHandler(queries *sqlc.Queries) *SignOutHandler {
	return &SignOutHandler{queries: queries}
}

func (h *SignOutHandler) HandleHTTP(c echo.Context) error {
	staff := GetStaff(c)
	if staff != nil {
		_ = h.queries.RevokeSession(c.Request().Context(), staff.SessionID)
	}
	clearSessionCookie(c)
	return response.OK(c, map[string]string{"state": "signed_out"})
}
```

Create `internal/auth/declare_workspace.go`:
```go
package auth

import (
	"database/sql"
	"fmt"
	"slices"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/labstack/echo/v4"
)

type DeclareWorkspaceHandler struct {
	queries *sqlc.Queries
}

func NewDeclareWorkspaceHandler(queries *sqlc.Queries) *DeclareWorkspaceHandler {
	return &DeclareWorkspaceHandler{queries: queries}
}

func (h *DeclareWorkspaceHandler) HandleHTTP(c echo.Context) error {
	staff := GetStaff(c)
	if staff == nil {
		return response.Error(c, fmt.Errorf("%w: unauthorized", response.ErrForbidden))
	}

	var req DeclareWorkspaceRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, fmt.Errorf("%w: invalid payload", response.ErrInvalid))
	}
	if err := c.Validate(&req); err != nil {
		return response.Error(c, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error()))
	}

	// Validate capability for requested workspace
	switch req.Workspace {
	case WorkspaceManager:
		if !slices.Contains(staff.Roles, RoleManager) {
			return response.Error(c, fmt.Errorf("%w: bạn không có quyền khu vực quản lý", response.ErrForbidden))
		}
	case WorkspacePreparation:
		if !slices.Contains(staff.Capabilities, "preparation.operate") {
			return response.Error(c, fmt.Errorf("%w: bạn không có quyền khu vực pha chế", response.ErrForbidden))
		}
	case WorkspaceCashier:
		if !slices.Contains(staff.Capabilities, "sales.operate") {
			return response.Error(c, fmt.Errorf("%w: bạn không có quyền khu vực thu ngân", response.ErrForbidden))
		}
	}

	err := h.queries.UpdateSessionWorkspace(c.Request().Context(), sqlc.UpdateSessionWorkspaceParams{
		ID:              staff.SessionID,
		ActiveWorkspace: sql.NullString{String: req.Workspace, Valid: true},
	})
	if err != nil {
		return response.Error(c, fmt.Errorf("update workspace: %w", err))
	}

	return response.OK(c, map[string]string{"workspace": req.Workspace})
}
```

Create `internal/auth/record_activity.go`:
```go
package auth

import (
	"fmt"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/labstack/echo/v4"
)

type RecordActivityHandler struct {
	queries *sqlc.Queries
}

func NewRecordActivityHandler(queries *sqlc.Queries) *RecordActivityHandler {
	return &RecordActivityHandler{queries: queries}
}

func (h *RecordActivityHandler) HandleHTTP(c echo.Context) error {
	staff := GetStaff(c)
	if staff == nil {
		return response.Error(c, fmt.Errorf("%w: unauthorized", response.ErrForbidden))
	}

	err := h.queries.UpdateSessionActivity(c.Request().Context(), sqlc.UpdateSessionActivityParams{
		ID:                   staff.SessionID,
		LastHumanActivityAt: time.Now().UTC(),
	})
	if err != nil {
		return response.Error(c, fmt.Errorf("record activity: %w", err))
	}

	return response.OK(c, map[string]bool{"recorded": true})
}
```

Create `internal/auth/list_identities.go`:
```go
package auth

import (
	"context"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/labstack/echo/v4"
)

type ListIdentitiesHandler struct {
	queries *sqlc.Queries
}

func NewListIdentitiesHandler(queries *sqlc.Queries) *ListIdentitiesHandler {
	return &ListIdentitiesHandler{queries: queries}
}

func (h *ListIdentitiesHandler) Handle(ctx context.Context) ([]IdentitySummaryResponse, error) {
	rows, err := h.queries.ListActiveIdentities(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active identities: %w", err)
	}

	res := make([]IdentitySummaryResponse, len(rows))
	for i, r := range rows {
		res[i] = IdentitySummaryResponse{
			DisplayName: r.DisplayName,
			LoginCode:   r.LoginCode,
		}
	}
	return res, nil
}

func (h *ListIdentitiesHandler) HandleHTTP(c echo.Context) error {
	res, err := h.Handle(c.Request().Context())
	if err != nil {
		return response.Error(c, err)
	}
	return response.OK(c, res)
}
```

- [x] **Step 3: Run `go test ./internal/auth/...`**

Run: `go test ./internal/auth/...`  
Expected: PASS.

- [x] **Step 4: Commit**

```bash
git add internal/auth/
git commit -m "feat(auth): implement public authentication handlers"
```

---

### Task 5: Bootstrap Manager Slice (First-Time Setup)

**Files:**
- Create: `internal/auth/bootstrap_manager.go`

**Interfaces:**
- Consumes: `*sql.DB`, `sqlc.Querier`, `internal/response`
- Produces: `BootstrapManagerHandler` for `POST /api/v1/auth/bootstrap`

- [x] **Step 1: Implement `bootstrap_manager.go` with advisory lock**

Create `internal/auth/bootstrap_manager.go`:
```go
package auth

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/labstack/echo/v4"
)

const BootstrapManagerAdvisoryLockID = 739201

type BootstrapManagerHandler struct {
	db      *sql.DB
	queries *sqlc.Queries
}

func NewBootstrapManagerHandler(db *sql.DB, queries *sqlc.Queries) *BootstrapManagerHandler {
	return &BootstrapManagerHandler{db: db, queries: queries}
}

func (h *BootstrapManagerHandler) Handle(ctx context.Context, req BootstrapManagerRequest) (*StaffProfileResponse, error) {
	pinHash, err := HashPin(req.Pin)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error())
	}

	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Acquire transactional advisory lock to prevent concurrent initialization
	var locked bool
	if err := tx.QueryRowContext(ctx, "SELECT pg_advisory_xact_lock($1)", BootstrapManagerAdvisoryLockID).Scan(&locked); err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("acquire bootstrap advisory lock: %w", err)
	}

	qtx := h.queries.WithTx(tx)

	// Check if any manager already exists
	activeCount, err := qtx.CountActiveManagers(ctx)
	if err != nil {
		return nil, fmt.Errorf("check existing managers: %w", err)
	}
	if activeCount > 0 {
		return nil, fmt.Errorf("%w: hệ thống đã có Quản lý được cài đặt", response.ErrConflict)
	}

	// Create manager identity
	created, err := qtx.CreateStaffIdentity(ctx, sqlc.CreateStaffIdentityParams{
		DisplayName: req.DisplayName,
		Upper:       strings.ToUpper(strings.TrimSpace(req.LoginCode)),
		PinHash:     pinHash,
		Enabled:     true,
	})
	if err != nil {
		return nil, fmt.Errorf("create manager identity: %w", err)
	}

	// Assign MANAGER role
	if err := qtx.AddStaffRole(ctx, sqlc.AddStaffRoleParams{
		StaffIdentityID: created.ID,
		Role:            RoleManager,
	}); err != nil {
		return nil, fmt.Errorf("assign manager role: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit bootstrap transaction: %w", err)
	}

	return &StaffProfileResponse{
		ID:           created.ID,
		DisplayName:  created.DisplayName,
		LoginCode:    created.LoginCode,
		Enabled:      created.Enabled,
		Roles:        []string{RoleManager},
		Capabilities: DeriveCapabilities([]string{RoleManager}),
	}, nil
}

func (h *BootstrapManagerHandler) HandleHTTP(c echo.Context) error {
	var req BootstrapManagerRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, fmt.Errorf("%w: invalid payload", response.ErrInvalid))
	}
	if err := c.Validate(&req); err != nil {
		return response.Error(c, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error()))
	}

	res, err := h.Handle(c.Request().Context(), req)
	if err != nil {
		return response.Error(c, err)
	}

	return response.Created(c, res)
}
```

- [x] **Step 2: Verify compile**

Run: `go test ./internal/auth/...`  
Expected: PASS.

- [x] **Step 3: Commit**

```bash
git add internal/auth/bootstrap_manager.go
git commit -m "feat(auth): add manager bootstrap handler with advisory lock"
```

---

### Task 6: Unified Idempotency Helper & Staff Administration Slices

**Files:**
- Create: `internal/auth/idempotency.go`
- Create: `internal/auth/staff_me.go`
- Create: `internal/auth/staff_list.go`
- Create: `internal/auth/staff_create.go`
- Create: `internal/auth/staff_set_enabled.go`
- Create: `internal/auth/staff_replace_roles.go`
- Create: `internal/auth/staff_reset_pin.go`

**Interfaces:**
- Consumes: `*sql.DB`, `sqlc.Querier`, `idempotency_keys` table
- Produces: HTTP Handlers for `/api/v1/staff/*`

- [x] **Step 1: Write `idempotency.go` helper**

Create `internal/auth/idempotency.go`:
```go
package auth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
)

func ComputeRequestHash(action string, payload any) string {
	b, _ := json.Marshal(payload)
	sum := sha256.Sum256(append([]byte(action+":"), b...))
	return hex.EncodeToString(sum[:])
}

// ExecuteWithIdempotency wraps mutation in an idempotency check and stores replayable response.
func ExecuteWithIdempotency[T any](
	ctx context.Context,
	q *sqlc.Queries,
	actorID uuid.UUID,
	key uuid.UUID,
	action string,
	payload any,
	fn func() (int, T, error),
) (int, T, error) {
	var zero T
	reqHash := ComputeRequestHash(action, payload)

	// Check if already executed
	existing, err := q.GetIdempotencyKey(ctx, sqlc.GetIdempotencyKeyParams{
		ActorID: actorID,
		Key:     key,
	})
	if err == nil {
		if existing.RequestHash != reqHash {
			return 0, zero, fmt.Errorf("%w: mã yêu cầu (request_id) đã được dùng cho payload khác", response.ErrConflict)
		}
		var stored T
		if err := json.Unmarshal(existing.ResponseBody, &stored); err != nil {
			return 0, zero, fmt.Errorf("unmarshal cached response: %w", err)
		}
		return int(existing.ResponseCode), stored, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return 0, zero, fmt.Errorf("check idempotency key: %w", err)
	}

	// Execute operation
	code, result, err := fn()
	if err != nil {
		return code, result, err
	}

	// Save result
	resultBytes, _ := json.Marshal(result)
	_ = q.InsertIdempotencyKey(ctx, sqlc.InsertIdempotencyKeyParams{
		Key:          key,
		ActorID:      actorID,
		Action:       action,
		RequestHash:  reqHash,
		ResponseCode: int32(code),
		ResponseBody: resultBytes,
	})

	return code, result, nil
}
```

- [x] **Step 2: Implement `staff_me.go` & `staff_list.go`**

Create `internal/auth/staff_me.go`:
```go
package auth

import (
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/labstack/echo/v4"
)

type StaffMeHandler struct{}

func NewStaffMeHandler() *StaffMeHandler {
	return &StaffMeHandler{}
}

func (h *StaffMeHandler) HandleHTTP(c echo.Context) error {
	staff := GetStaff(c)
	if staff == nil {
		return response.Error(c, fmt.Errorf("%w: unauthorized", response.ErrForbidden))
	}

	return response.OK(c, StaffProfileResponse{
		ID:           staff.StaffID,
		DisplayName:  staff.DisplayName,
		LoginCode:    staff.LoginCode,
		Enabled:      true,
		Roles:        staff.Roles,
		Capabilities: staff.Capabilities,
	})
}
```

Create `internal/auth/staff_list.go`:
```go
package auth

import (
	"context"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

type StaffListHandler struct {
	queries *sqlc.Queries
}

func NewStaffListHandler(queries *sqlc.Queries) *StaffListHandler {
	return &StaffListHandler{queries: queries}
}

func (h *StaffListHandler) Handle(ctx context.Context) ([]StaffDetailResponse, error) {
	staffRows, err := h.queries.ListAllStaff(ctx)
	if err != nil {
		return nil, fmt.Errorf("list all staff: %w", err)
	}

	roleRows, err := h.queries.ListAllStaffRoles(ctx)
	if err != nil {
		return nil, fmt.Errorf("list all staff roles: %w", err)
	}

	rolesMap := make(map[uuid.UUID][]string)
	for _, r := range roleRows {
		rolesMap[r.StaffIdentityID] = append(rolesMap[r.StaffIdentityID], r.Role)
	}

	res := make([]StaffDetailResponse, len(staffRows))
	for i, s := range staffRows {
		roles := rolesMap[s.ID]
		if roles == nil {
			roles = []string{}
		}
		res[i] = StaffDetailResponse{
			ID:          s.ID,
			DisplayName: s.DisplayName,
			LoginCode:   s.LoginCode,
			Enabled:     s.Enabled,
			Roles:       roles,
			CreatedAt:   s.CreatedAt,
		}
	}

	return res, nil
}

func (h *StaffListHandler) HandleHTTP(c echo.Context) error {
	res, err := h.Handle(c.Request().Context())
	if err != nil {
		return response.Error(c, err)
	}
	return response.OK(c, res)
}
```

- [x] **Step 3: Implement `staff_create.go`**

Create `internal/auth/staff_create.go`:
```go
package auth

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/labstack/echo/v4"
)

type StaffCreateHandler struct {
	db      *sql.DB
	queries *sqlc.Queries
}

func NewStaffCreateHandler(db *sql.DB, queries *sqlc.Queries) *StaffCreateHandler {
	return &StaffCreateHandler{db: db, queries: queries}
}

func (h *StaffCreateHandler) Handle(ctx context.Context, actor *StaffClaims, req CreateStaffRequest) (int, *StaffDetailResponse, error) {
	// 1. Verify manager pin
	actorStaff, err := h.queries.GetStaffByID(ctx, actor.StaffID)
	if err != nil {
		return 0, nil, fmt.Errorf("lookup actor: %w", err)
	}
	if !VerifyPin(actorStaff.PinHash, req.ManagerPin) {
		return 0, nil, fmt.Errorf("%w: PIN Quản lý không đúng", response.ErrForbidden)
	}

	// 2. Execute with Idempotency
	return ExecuteWithIdempotency(ctx, h.queries, actor.StaffID, req.RequestID, "staff.create", req, func() (int, *StaffDetailResponse, error) {
		pinHash, err := HashPin(req.Pin)
		if err != nil {
			return 0, nil, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error())
		}

		tx, err := h.db.BeginTx(ctx, nil)
		if err != nil {
			return 0, nil, fmt.Errorf("begin tx: %w", err)
		}
		defer tx.Rollback()

		qtx := h.queries.WithTx(tx)

		created, err := qtx.CreateStaffIdentity(ctx, sqlc.CreateStaffIdentityParams{
			DisplayName: req.DisplayName,
			Upper:       strings.ToUpper(strings.TrimSpace(req.LoginCode)),
			PinHash:     pinHash,
			Enabled:     req.Enabled,
		})
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return 0, nil, fmt.Errorf("%w: mã đăng nhập đã được sử dụng", response.ErrConflict)
			}
			return 0, nil, fmt.Errorf("insert staff: %w", err)
		}

		for _, role := range req.Roles {
			if err := qtx.AddStaffRole(ctx, sqlc.AddStaffRoleParams{
				StaffIdentityID: created.ID,
				Role:            role,
			}); err != nil {
				return 0, nil, fmt.Errorf("add role: %w", err)
			}
		}

		if err := tx.Commit(); err != nil {
			return 0, nil, fmt.Errorf("commit tx: %w", err)
		}

		return http.StatusCreated, &StaffDetailResponse{
			ID:          created.ID,
			DisplayName: created.DisplayName,
			LoginCode:   created.LoginCode,
			Enabled:     created.Enabled,
			Roles:       req.Roles,
			CreatedAt:   created.CreatedAt,
		}, nil
	})
}

func (h *StaffCreateHandler) HandleHTTP(c echo.Context) error {
	staff := GetStaff(c)
	if staff == nil {
		return response.Error(c, fmt.Errorf("%w: unauthorized", response.ErrForbidden))
	}

	var req CreateStaffRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, fmt.Errorf("%w: invalid payload", response.ErrInvalid))
	}
	if err := c.Validate(&req); err != nil {
		return response.Error(c, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error()))
	}

	code, res, err := h.Handle(c.Request().Context(), staff, req)
	if err != nil {
		return response.Error(c, err)
	}

	if code == http.StatusCreated {
		return response.Created(c, res)
	}
	return response.OK(c, res)
}
```

- [x] **Step 4: Implement `staff_set_enabled.go`, `staff_replace_roles.go`, `staff_reset_pin.go`**

Create `internal/auth/staff_set_enabled.go`:
```go
package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

const EnabledManagerInvariantLockID = 1247091103

type StaffSetEnabledHandler struct {
	db      *sql.DB
	queries *sqlc.Queries
}

func NewStaffSetEnabledHandler(db *sql.DB, queries *sqlc.Queries) *StaffSetEnabledHandler {
	return &StaffSetEnabledHandler{db: db, queries: queries}
}

func (h *StaffSetEnabledHandler) Handle(ctx context.Context, actor *StaffClaims, targetID uuid.UUID, req SetStaffEnabledRequest) (int, *StaffDetailResponse, error) {
	if req.Enabled == req.ExpectedEnabled {
		return 0, nil, fmt.Errorf("%w: trạng thái mới phải khác trạng thái hiện tại", response.ErrInvalid)
	}

	actorStaff, err := h.queries.GetStaffByID(ctx, actor.StaffID)
	if err != nil {
		return 0, nil, fmt.Errorf("lookup actor: %w", err)
	}
	if !VerifyPin(actorStaff.PinHash, req.ManagerPin) {
		return 0, nil, fmt.Errorf("%w: PIN Quản lý không đúng", response.ErrForbidden)
	}

	return ExecuteWithIdempotency(ctx, h.queries, actor.StaffID, req.RequestID, "staff.set_enabled", req, func() (int, *StaffDetailResponse, error) {
		tx, err := h.db.BeginTx(ctx, nil)
		if err != nil {
			return 0, nil, fmt.Errorf("begin tx: %w", err)
		}
		defer tx.Rollback()

		var locked bool
		if err := tx.QueryRowContext(ctx, "SELECT pg_advisory_xact_lock($1)", EnabledManagerInvariantLockID).Scan(&locked); err != nil && err != sql.ErrNoRows {
			return 0, nil, fmt.Errorf("acquire invariant lock: %w", err)
		}

		qtx := h.queries.WithTx(tx)

		target, err := qtx.GetStaffByID(ctx, targetID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return 0, nil, fmt.Errorf("%w: không tìm thấy nhân viên", response.ErrNotFound)
			}
			return 0, nil, fmt.Errorf("get target: %w", err)
		}

		if target.Enabled != req.ExpectedEnabled {
			return 0, nil, fmt.Errorf("%w: trạng thái nhân viên đã thay đổi, vui lòng tải lại", response.ErrConflict)
		}

		roles, err := qtx.GetStaffRoles(ctx, targetID)
		if err != nil {
			return 0, nil, fmt.Errorf("get target roles: %w", err)
		}

		// Invariant check: If disabling a manager, ensure at least one other active manager remains
		if !req.Enabled && slices.Contains(roles, RoleManager) {
			activeManagers, err := qtx.CountActiveManagers(ctx)
			if err != nil {
				return 0, nil, fmt.Errorf("count active managers: %w", err)
			}
			if activeManagers <= 1 {
				return 0, nil, fmt.Errorf("%w: phải còn ít nhất một Quản lý đang hoạt động", response.ErrConflict)
			}
		}

		updated, err := qtx.SetStaffEnabled(ctx, sqlc.SetStaffEnabledParams{
			ID:      targetID,
			Enabled: req.Enabled,
		})
		if err != nil {
			return 0, nil, fmt.Errorf("update enabled: %w", err)
		}

		// If disabled, revoke all active sessions for this staff member
		if !req.Enabled {
			_ = qtx.RevokeAllStaffSessions(ctx, targetID)
		}

		if err := tx.Commit(); err != nil {
			return 0, nil, fmt.Errorf("commit tx: %w", err)
		}

		return http.StatusOK, &StaffDetailResponse{
			ID:          updated.ID,
			DisplayName: updated.DisplayName,
			LoginCode:   updated.LoginCode,
			Enabled:     updated.Enabled,
			Roles:       roles,
			CreatedAt:   target.CreatedAt,
		}, nil
	})
}

func (h *StaffSetEnabledHandler) HandleHTTP(c echo.Context) error {
	staff := GetStaff(c)
	if staff == nil {
		return response.Error(c, fmt.Errorf("%w: unauthorized", response.ErrForbidden))
	}

	targetID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.Error(c, fmt.Errorf("%w: invalid staff id", response.ErrInvalid))
	}

	var req SetStaffEnabledRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, fmt.Errorf("%w: invalid payload", response.ErrInvalid))
	}
	if err := c.Validate(&req); err != nil {
		return response.Error(c, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error()))
	}

	_, res, err := h.Handle(c.Request().Context(), staff, targetID, req)
	if err != nil {
		return response.Error(c, err)
	}

	return response.OK(c, res)
}
```

Create `internal/auth/staff_replace_roles.go`:
```go
package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

type StaffReplaceRolesHandler struct {
	db      *sql.DB
	queries *sqlc.Queries
}

func NewStaffReplaceRolesHandler(db *sql.DB, queries *sqlc.Queries) *StaffReplaceRolesHandler {
	return &StaffReplaceRolesHandler{db: db, queries: queries}
}

func (h *StaffReplaceRolesHandler) Handle(ctx context.Context, actor *StaffClaims, targetID uuid.UUID, req ReplaceStaffRolesRequest) (int, *StaffDetailResponse, error) {
	actorStaff, err := h.queries.GetStaffByID(ctx, actor.StaffID)
	if err != nil {
		return 0, nil, fmt.Errorf("lookup actor: %w", err)
	}
	if !VerifyPin(actorStaff.PinHash, req.ManagerPin) {
		return 0, nil, fmt.Errorf("%w: PIN Quản lý không đúng", response.ErrForbidden)
	}

	return ExecuteWithIdempotency(ctx, h.queries, actor.StaffID, req.RequestID, "staff.replace_roles", req, func() (int, *StaffDetailResponse, error) {
		tx, err := h.db.BeginTx(ctx, nil)
		if err != nil {
			return 0, nil, fmt.Errorf("begin tx: %w", err)
		}
		defer tx.Rollback()

		var locked bool
		if err := tx.QueryRowContext(ctx, "SELECT pg_advisory_xact_lock($1)", EnabledManagerInvariantLockID).Scan(&locked); err != nil && err != sql.ErrNoRows {
			return 0, nil, fmt.Errorf("acquire invariant lock: %w", err)
		}

		qtx := h.queries.WithTx(tx)

		target, err := qtx.GetStaffByID(ctx, targetID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return 0, nil, fmt.Errorf("%w: không tìm thấy nhân viên", response.ErrNotFound)
			}
			return 0, nil, fmt.Errorf("get target: %w", err)
		}

		currentRoles, err := qtx.GetStaffRoles(ctx, targetID)
		if err != nil {
			return 0, nil, fmt.Errorf("get target roles: %w", err)
		}

		// Invariant check: If removing MANAGER role from an active manager, ensure at least one other active manager remains
		hadManager := slices.Contains(currentRoles, RoleManager)
		willHaveManager := slices.Contains(req.Roles, RoleManager)
		if target.Enabled && hadManager && !willHaveManager {
			activeManagers, err := qtx.CountActiveManagers(ctx)
			if err != nil {
				return 0, nil, fmt.Errorf("count active managers: %w", err)
			}
			if activeManagers <= 1 {
				return 0, nil, fmt.Errorf("%w: phải còn ít nhất một Quản lý đang hoạt động", response.ErrConflict)
			}
		}

		if err := qtx.ClearStaffRoles(ctx, targetID); err != nil {
			return 0, nil, fmt.Errorf("clear roles: %w", err)
		}

		for _, r := range req.Roles {
			if err := qtx.AddStaffRole(ctx, sqlc.AddStaffRoleParams{
				StaffIdentityID: targetID,
				Role:            r,
			}); err != nil {
				return 0, nil, fmt.Errorf("add role: %w", err)
			}
		}

		if err := tx.Commit(); err != nil {
			return 0, nil, fmt.Errorf("commit tx: %w", err)
		}

		return http.StatusOK, &StaffDetailResponse{
			ID:          target.ID,
			DisplayName: target.DisplayName,
			LoginCode:   target.LoginCode,
			Enabled:     target.Enabled,
			Roles:       req.Roles,
			CreatedAt:   target.CreatedAt,
		}, nil
	})
}

func (h *StaffReplaceRolesHandler) HandleHTTP(c echo.Context) error {
	staff := GetStaff(c)
	if staff == nil {
		return response.Error(c, fmt.Errorf("%w: unauthorized", response.ErrForbidden))
	}

	targetID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.Error(c, fmt.Errorf("%w: invalid staff id", response.ErrInvalid))
	}

	var req ReplaceStaffRolesRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, fmt.Errorf("%w: invalid payload", response.ErrInvalid))
	}
	if err := c.Validate(&req); err != nil {
		return response.Error(c, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error()))
	}

	_, res, err := h.Handle(c.Request().Context(), staff, targetID, req)
	if err != nil {
		return response.Error(c, err)
	}

	return response.OK(c, res)
}
```

Create `internal/auth/staff_reset_pin.go`:
```go
package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

type StaffResetPinHandler struct {
	db      *sql.DB
	queries *sqlc.Queries
}

func NewStaffResetPinHandler(db *sql.DB, queries *sqlc.Queries) *StaffResetPinHandler {
	return &StaffResetPinHandler{db: db, queries: queries}
}

func (h *StaffResetPinHandler) Handle(ctx context.Context, actor *StaffClaims, targetID uuid.UUID, req ResetStaffPinRequest) (int, map[string]string, error) {
	actorStaff, err := h.queries.GetStaffByID(ctx, actor.StaffID)
	if err != nil {
		return 0, nil, fmt.Errorf("lookup actor: %w", err)
	}
	if !VerifyPin(actorStaff.PinHash, req.ManagerPin) {
		return 0, nil, fmt.Errorf("%w: PIN Quản lý không đúng", response.ErrForbidden)
	}

	return ExecuteWithIdempotency(ctx, h.queries, actor.StaffID, req.RequestID, "staff.reset_pin", req, func() (int, map[string]string, error) {
		pinHash, err := HashPin(req.Pin)
		if err != nil {
			return 0, nil, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error())
		}

		target, err := h.queries.GetStaffByID(ctx, targetID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return 0, nil, fmt.Errorf("%w: không tìm thấy nhân viên", response.ErrNotFound)
			}
			return 0, nil, fmt.Errorf("get target: %w", err)
		}

		if err := h.queries.UpdateStaffPin(ctx, sqlc.UpdateStaffPinParams{
			ID:      target.ID,
			PinHash: pinHash,
		}); err != nil {
			return 0, nil, fmt.Errorf("update pin: %w", err)
		}

		// Revoke sessions
		_ = h.queries.RevokeAllStaffSessions(ctx, targetID)

		return http.StatusOK, map[string]string{"message": "đổi mã PIN thành công"}, nil
	})
}

func (h *StaffResetPinHandler) HandleHTTP(c echo.Context) error {
	staff := GetStaff(c)
	if staff == nil {
		return response.Error(c, fmt.Errorf("%w: unauthorized", response.ErrForbidden))
	}

	targetID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.Error(c, fmt.Errorf("%w: invalid staff id", response.ErrInvalid))
	}

	var req ResetStaffPinRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, fmt.Errorf("%w: invalid payload", response.ErrInvalid))
	}
	if err := c.Validate(&req); err != nil {
		return response.Error(c, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error()))
	}

	_, res, err := h.Handle(c.Request().Context(), staff, targetID, req)
	if err != nil {
		return response.Error(c, err)
	}

	return response.OK(c, res)
}
```

- [x] **Step 5: Verify compile**

Run: `go test ./internal/auth/...`  
Expected: PASS.

- [x] **Step 6: Commit**

```bash
git add internal/auth/
git commit -m "feat(auth): implement staff administration handlers with idempotency"
```

---

### Task 7: Echo Middlewares & Context Injection

**Files:**
- Create: `internal/auth/middleware.go`
- Create: `internal/auth/middleware_test.go`

**Interfaces:**
- Consumes: `sqlc.Queries`, `echo.Context`
- Produces: `RequireAuth(allowedRoles ...string)`, `RequireCapability(cap string)`, `GetStaff(c echo.Context) *StaffClaims`

- [x] **Step 1: Implement `middleware.go`**

Create `internal/auth/middleware.go`:
```go
package auth

import (
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

const StaffContextKey = "auth_staff_claims"

type StaffClaims struct {
	StaffID      uuid.UUID
	SessionID    uuid.UUID
	DisplayName  string
	LoginCode    string
	Roles        []string
	Capabilities []string
	Workspace    *string
}

func GetStaff(c echo.Context) *StaffClaims {
	if val := c.Get(StaffContextKey); val != nil {
		if claims, ok := val.(*StaffClaims); ok {
			return claims
		}
	}
	return nil
}

type Middleware struct {
	queries *sqlc.Queries
}

func NewMiddleware(queries *sqlc.Queries) *Middleware {
	return &Middleware{queries: queries}
}

func (m *Middleware) RequireAuth(allowedRoles ...string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			token := extractToken(c)
			if token == "" {
				return response.Error(c, fmt.Errorf("%w: vui lòng đăng nhập", response.ErrForbidden))
			}

			tokenHash := HashToken(token)
			sess, err := m.queries.GetSessionByTokenHash(c.Request().Context(), tokenHash)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return response.Error(c, fmt.Errorf("%w: phiên đăng nhập không hợp lệ", response.ErrForbidden))
				}
				return response.Error(c, fmt.Errorf("lookup session: %w", err))
			}

			if sess.RevokedAt.Valid || time.Now().UTC().After(sess.ExpiresAt) || !sess.IdentityEnabled {
				return response.Error(c, fmt.Errorf("%w: phiên đăng nhập đã hết hạn hoặc bị thu hồi", response.ErrForbidden))
			}

			workspace := ""
			if sess.ActiveWorkspace.Valid {
				workspace = sess.ActiveWorkspace.String
			}

			// Inactivity lock check
			timeout := GetInactivityTimeout(workspace)
			if sess.SessionState == SessionStateActive && time.Now().UTC().Sub(sess.LastHumanActivityAt) >= timeout {
				_ = m.queries.UpdateSessionState(c.Request().Context(), sqlc.UpdateSessionStateParams{
					ID:    sess.SessionID,
					State: SessionStateLocked,
				})
				sess.SessionState = SessionStateLocked
			}

			if sess.SessionState != SessionStateActive {
				return response.Error(c, fmt.Errorf("%w: phiên đang bị khóa, vui lòng mở khóa", response.ErrForbidden))
			}

			roles, err := m.queries.GetStaffRoles(c.Request().Context(), sess.StaffIdentityID)
			if err != nil {
				return response.Error(c, fmt.Errorf("lookup roles: %w", err))
			}

			// Role check
			if len(allowedRoles) > 0 {
				hasRole := false
				for _, r := range allowedRoles {
					if slices.Contains(roles, r) {
						hasRole = true
						break
					}
				}
				if !hasRole {
					return response.Error(c, fmt.Errorf("%w: bạn không có quyền thực hiện thao tác này", response.ErrForbidden))
				}
			}

			var wsPtr *string
			if workspace != "" {
				wsPtr = &workspace
			}

			claims := &StaffClaims{
				StaffID:      sess.StaffIdentityID,
				SessionID:    sess.SessionID,
				DisplayName:  sess.DisplayName,
				LoginCode:    sess.LoginCode,
				Roles:        roles,
				Capabilities: DeriveCapabilities(roles),
				Workspace:    wsPtr,
			}
			c.Set(StaffContextKey, claims)

			return next(c)
		}
	}
}

func (m *Middleware) RequireCapability(capability string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			staff := GetStaff(c)
			if staff == nil {
				return response.Error(c, fmt.Errorf("%w: vui lòng đăng nhập", response.ErrForbidden))
			}
			if !slices.Contains(staff.Capabilities, capability) {
				return response.Error(c, fmt.Errorf("%w: thiếu quyền %s", response.ErrForbidden, capability))
			}
			return next(c)
		}
	}
}
```

- [x] **Step 2: Verify compile and unit test**

Run: `go test ./internal/auth/...`  
Expected: PASS.

- [x] **Step 3: Commit**

```bash
git add internal/auth/middleware.go
git commit -m "feat(auth): implement RequireAuth and RequireCapability Echo middlewares"
```

---

### Task 8: Routes Registration & Integration in Main

**Files:**
- Create: `internal/auth/routes.go`
- Modify: `cmd/api/main.go`

**Interfaces:**
- Consumes: `*sql.DB`, `*sqlc.Queries`, `eventbus.Bus`, `echo.Group`
- Produces: `auth.NewSlices`, `RegisterRoutes`

- [x] **Step 1: Write `routes.go`**

Create `internal/auth/routes.go`:
```go
package auth

import (
	"database/sql"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/labstack/echo/v4"
)

type Slices struct {
	Middleware       *Middleware
	Bootstrap        *BootstrapManagerHandler
	SignIn           *SignInHandler
	Unlock           *UnlockSessionHandler
	GetSession       *GetSessionHandler
	Lock             *LockSessionHandler
	SignOut          *SignOutHandler
	DeclareWorkspace *DeclareWorkspaceHandler
	RecordActivity   *RecordActivityHandler
	ListIdentities   *ListIdentitiesHandler
	StaffMe          *StaffMeHandler
	StaffList        *StaffListHandler
	StaffCreate      *StaffCreateHandler
	StaffSetEnabled  *StaffSetEnabledHandler
	StaffReplaceRole *StaffReplaceRolesHandler
	StaffResetPin    *StaffResetPinHandler
}

func NewSlices(db *sql.DB, queries *sqlc.Queries) *Slices {
	return &Slices{
		Middleware:       NewMiddleware(queries),
		Bootstrap:        NewBootstrapManagerHandler(db, queries),
		SignIn:           NewSignInHandler(queries),
		Unlock:           NewUnlockSessionHandler(queries),
		GetSession:       NewGetSessionHandler(queries),
		Lock:             NewLockSessionHandler(queries),
		SignOut:          NewSignOutHandler(queries),
		DeclareWorkspace: NewDeclareWorkspaceHandler(queries),
		RecordActivity:   NewRecordActivityHandler(queries),
		ListIdentities:   NewListIdentitiesHandler(queries),
		StaffMe:          NewStaffMeHandler(),
		StaffList:        NewStaffListHandler(queries),
		StaffCreate:      NewStaffCreateHandler(db, queries),
		StaffSetEnabled:  NewStaffSetEnabledHandler(db, queries),
		StaffReplaceRole: NewStaffReplaceRolesHandler(db, queries),
		StaffResetPin:    NewStaffResetPinHandler(db, queries),
	}
}

func (s *Slices) RegisterRoutes(v1 *echo.Group) {
	// Auth routes
	authGroup := v1.Group("/auth")
	authGroup.POST("/bootstrap", s.Bootstrap.HandleHTTP)
	authGroup.GET("/identities", s.ListIdentities.HandleHTTP)
	authGroup.POST("/sign-in", s.SignIn.HandleHTTP)
	authGroup.GET("/session", s.GetSession.HandleHTTP)
	authGroup.POST("/unlock", s.Unlock.HandleHTTP)
	authGroup.POST("/lock", s.Lock.HandleHTTP, s.Middleware.RequireAuth())
	authGroup.POST("/sign-out", s.SignOut.HandleHTTP, s.Middleware.RequireAuth())
	authGroup.POST("/workspace", s.DeclareWorkspace.HandleHTTP, s.Middleware.RequireAuth())
	authGroup.POST("/activity", s.RecordActivity.HandleHTTP, s.Middleware.RequireAuth())

	// Staff administration routes (Protected by MANAGER role)
	staffGroup := v1.Group("/staff", s.Middleware.RequireAuth(RoleManager))
	staffGroup.GET("/me", s.StaffMe.HandleHTTP)
	staffGroup.GET("", s.StaffList.HandleHTTP)
	staffGroup.POST("", s.StaffCreate.HandleHTTP)
	staffGroup.PATCH("/:id/enabled", s.StaffSetEnabled.HandleHTTP)
	staffGroup.PUT("/:id/roles", s.StaffReplaceRole.HandleHTTP)
	staffGroup.POST("/:id/reset-pin", s.StaffResetPin.HandleHTTP)
}
```

- [x] **Step 2: Wire in `cmd/api/main.go`**

In `cmd/api/main.go`:
Register `authSlices := auth.NewSlices(db, queries)` and `authSlices.RegisterRoutes(v1)`.

- [x] **Step 3: Verify compile**

Run: `go test ./...` in `.worktrees/auth-slice/`  
Expected: PASS.

- [x] **Step 4: Commit**

```bash
git add internal/auth/routes.go cmd/api/main.go
git commit -m "feat(auth): register auth and staff routes in api v1"
```

---

### Task 9: End-to-End Integration Testing

**Files:**
- Create: `internal/auth/auth_integration_test.go`

**Interfaces:**
- Consumes: Real test PostgreSQL instance via `TEST_DATABASE_URL`
- Produces: Complete automated integration test covering bootstrap, login, lock, unlock, staff CRUD, idempotency, and invariant enforcement.

- [x] **Step 1: Write integration tests**

Create `internal/auth/auth_integration_test.go`:
Test scenarios:
1. `TestBootstrapManager`: first call creates manager, second call returns 409 conflict.
2. `TestSignInAndSession`: sign in with PIN gets token, verifies session state, wrong PIN fails.
3. `TestSessionLockAndUnlock`: inactivity lock and unlock with PIN.
4. `TestStaffAdministrationAndIdempotency`: Manager creates staff with `request_id`, duplicate call with same `request_id` returns cached response, different payload returns 409.
5. `TestManagerInvariant`: attempting to deactivate the last remaining manager returns 409 `FINAL_ENABLED_MANAGER_REQUIRED`.
6. `TestSessionRevocationOnDeactivation`: deactivating staff revokes their active sessions.

- [x] **Step 2: Run integration tests**

Run: `go test -v ./internal/auth/...`  
Expected: PASS.

- [x] **Step 3: Commit**

```bash
git add internal/auth/auth_integration_test.go
git commit -m "test(auth): add comprehensive integration test suite"
```

---

### Task 10: OpenAPI / Swagger Documentation & Verification

**Files:**
- Modify: `docs/docs.go`, `docs/swagger.json`, `docs/swagger.yaml`

- [x] **Step 1: Generate updated swagger docs**

Run: `swag init -g cmd/api/main.go -o docs` in `.worktrees/auth-slice/`  
Expected: Successfully generates OpenAPI spec including all `/api/v1/auth/*` and `/api/v1/staff/*` endpoints.

- [x] **Step 2: Run complete test suite**

Run: `go test -v ./...` in `.worktrees/auth-slice/`  
Expected: All packages pass with 0 errors.

- [x] **Step 3: Commit**

```bash
git add docs/
git commit -m "docs(auth): update swagger documentation for auth and staff endpoints"
```
