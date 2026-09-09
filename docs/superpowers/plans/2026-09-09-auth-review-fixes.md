# Auth Slice Review Fixes Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix critical and important issues identified in PR #2 code review.

**Architecture:** Each task is a self-contained fix that can be tested independently. Tasks are ordered by dependency — earlier tasks may affect later ones.

**Tech Stack:** Go, PostgreSQL, sqlc, Echo, bcrypt

**Spec:** `docs/superpowers/specs/2026-09-09-auth-slice-design.md`

## Global Constraints
- Maintain backward compatibility with existing database schema (no destructive migrations)
- All existing tests must continue passing
- Vietnamese error messages for user-facing responses
- Follow existing vertical slice patterns in the codebase

---

## Task 1: Fix Session State Contract (`active` → `authenticated`)

**Files:**
- Modify: `internal/auth/domain.go:33-36`
- Modify: `internal/auth/get_session.go:82-90`
- Modify: `internal/auth/middleware.go:84`

**Interfaces:**
- Consumes: `SessionStateActive` constant, `SessionStateLocked` constant
- Produces: API returns `"authenticated"` instead of `"active"` for active sessions

- [x] **Step 1: Update SessionStateActive constant**

```go
// In internal/auth/domain.go
const (
    SessionStateActive = "authenticated" // Changed from "active"
    SessionStateLocked = "locked"
)
```

- [x] **Step 2: Verify GetSession returns correct state**

The `get_session.go` uses `sess.SessionState` which comes from the DB. The DB stores `'active'` as the state value. We need to map DB state to API state.

```go
// In internal/auth/get_session.go, after line 80 (before returning)
apiState := sess.SessionState
if apiState == "active" {
    apiState = SessionStateActive
}
```

- [x] **Step 3: Update middleware to check for DB state value**

```go
// In internal/auth/middleware.go, line 84
if sess.SessionState != SessionStateActive && sess.SessionState != "active" {
```

- [x] **Step 4: Run tests**

Run: `go test ./internal/auth/...`
Expected: All tests pass

- [x] **Step 5: Commit**

```bash
git add internal/auth/domain.go internal/auth/get_session.go internal/auth/middleware.go
git commit -m "fix(auth): return 'authenticated' instead of 'active' in session state"
```

---

## Task 2: Fix Sign-Out to Propagate Revocation Errors

**Files:**
- Modify: `internal/auth/sign_out.go:28-34`

**Interfaces:**
- Consumes: `GetStaff(c)`, `RevokeSession()`
- Produces: Error response if revocation fails

- [x] **Step 1: Write failing test**

```go
// In internal/auth/handlers_test.go, add test case
func TestSignOutHandler_RevocationFailure(t *testing.T) {
    // Setup: mock RevokeSession to return error
    // Call HandleHTTP
    // Assert: response is 500 INTERNAL_ERROR, not 200 OK
}
```

- [x] **Step 2: Fix sign_out.go**

```go
func (h *SignOutHandler) HandleHTTP(c echo.Context) error {
    staff := GetStaff(c)
    if staff != nil {
        if err := h.queries.RevokeSession(c.Request().Context(), staff.SessionID); err != nil {
            return response.Error(c, fmt.Errorf("revoke session: %w", err))
        }
    }
    clearSessionCookie(c)
    return response.OK(c, map[string]string{"state": "signed_out"})
}
```

- [x] **Step 3: Run tests**

Run: `go test ./internal/auth/...`
Expected: New test passes, existing tests pass

- [x] **Step 4: Commit**

```bash
git add internal/auth/sign_out.go internal/auth/handlers_test.go
git commit -m "fix(auth): propagate revocation errors in sign-out"
```

---

## Task 3: Fix Unlock Atomicity

**Files:**
- Modify: `internal/auth/unlock_session.go:45-58`

**Interfaces:**
- Consumes: `UpdateSessionState()`, `UpdateSessionActivity()`
- Produces: Atomic unlock operation

- [x] **Step 1: Write failing test**

```go
// In internal/auth/handlers_test.go, add test case
func TestUnlockHandler_PartialFailure(t *testing.T) {
    // Setup: mock UpdateSessionState succeeds, UpdateSessionActivity fails
    // Call Handle
    // Assert: session state remains "locked" (rolled back)
}
```

- [x] **Step 2: Fix unlock_session.go**

```go
func (h *UnlockSessionHandler) Handle(ctx context.Context, token string, pin string) (*SignInResponse, error) {
    // ... existing validation code ...

    now := time.Now().UTC()
    
    // Use a transaction for atomic unlock
    // Note: This requires db field. Add db *sql.DB to struct if not present.
    tx, err := h.db.BeginTx(ctx, nil)
    if err != nil {
        return nil, fmt.Errorf("begin tx: %w", err)
    }
    defer func() { _ = tx.Rollback() }()

    qtx := h.queries.WithTx(tx)

    if err := qtx.UpdateSessionState(ctx, sqlc.UpdateSessionStateParams{
        ID:    sess.SessionID,
        State: SessionStateActive,
    }); err != nil {
        return nil, fmt.Errorf("unlock session: %w", err)
    }

    if err := qtx.UpdateSessionActivity(ctx, sqlc.UpdateSessionActivityParams{
        ID:                  sess.SessionID,
        LastHumanActivityAt: now,
    }); err != nil {
        return nil, fmt.Errorf("update session activity: %w", err)
    }

    if err := tx.Commit(); err != nil {
        return nil, fmt.Errorf("commit unlock tx: %w", err)
    }

    // ... rest of existing code ...
}
```

- [x] **Step 3: Update constructor to accept db**

```go
// In internal/auth/unlock_session.go
type UnlockSessionHandler struct {
    db      *sql.DB
    queries *sqlc.Queries
}

func NewUnlockSessionHandler(db *sql.DB, queries *sqlc.Queries) *UnlockSessionHandler {
    return &UnlockSessionHandler{db: db, queries: queries}
}
```

- [x] **Step 4: Update routes.go to pass db**

```go
// In internal/auth/routes.go, line 35
Unlock: NewUnlockSessionHandler(db, queries),
```

- [x] **Step 5: Run tests**

Run: `go test ./internal/auth/...`
Expected: All tests pass

- [x] **Step 6: Commit**

```bash
git add internal/auth/unlock_session.go internal/auth/routes.go internal/auth/handlers_test.go
git commit -m "fix(auth): make unlock operation atomic with transaction"
```

---

## Task 4: Add PATCH to CORS Allowed Methods

**Files:**
- Modify: `cmd/api/main.go:119-123`

**Interfaces:**
- Consumes: Echo CORS middleware config
- Produces: PATCH method allowed in CORS preflight

- [x] **Step 1: Fix CORS config**

```go
// In cmd/api/main.go, line 121
AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions},
```

- [x] **Step 2: Run tests**

Run: `go test ./...`
Expected: All tests pass

- [x] **Step 3: Commit**

```bash
git add cmd/api/main.go
git commit -m "fix(api): add PATCH to CORS allowed methods"
```

---

## Task 5: Add FINAL_ENABLED_MANAGER_REQUIRED Error Code

**Files:**
- Modify: `internal/response/response.go`
- Modify: `internal/auth/staff_set_enabled.go:81-83`
- Modify: `internal/auth/staff_replace_roles.go:73-75`

**Interfaces:**
- Consumes: `response.Error()`
- Produces: New error type `ErrManagerInvariant`

- [x] **Step 1: Add new error type**

```go
// In internal/response/response.go, after line 17
var (
    // ... existing errors ...
    ErrManagerInvariant = errors.New("manager invariant violation")
)
```

- [x] **Step 2: Add error code mapping**

```go
// In internal/response/response.go, in the Error function, add case before default
case errors.Is(err, ErrManagerInvariant):
    return c.JSON(http.StatusConflict, APIResponse{
        Success: false,
        Error: &APIError{
            Code:    "FINAL_ENABLED_MANAGER_REQUIRED",
            Message: err.Error(),
        },
    })
```

- [x] **Step 3: Update staff_set_enabled.go**

```go
// Line 82, change from:
return 0, nil, fmt.Errorf("%w: phải còn ít nhất một Quản lý đang hoạt động", response.ErrConflict)
// To:
return 0, nil, fmt.Errorf("%w: phải còn ít nhất một Quản lý đang hoạt động", response.ErrManagerInvariant)
```

- [x] **Step 4: Update staff_replace_roles.go**

```go
// Line 74, change from:
return 0, nil, fmt.Errorf("%w: phải còn ít nhất một Quản lý đang hoạt động", response.ErrConflict)
// To:
return 0, nil, fmt.Errorf("%w: phải còn ít nhất một Quản lý đang hoạt động", response.ErrManagerInvariant)
```

- [x] **Step 5: Update response_test.go**

```go
// Add test case for ErrManagerInvariant
func TestError_ManagerInvariant(t *testing.T) {
    // ... test that ErrManagerInvariant returns 409 with code "FINAL_ENABLED_MANAGER_REQUIRED"
}
```

- [x] **Step 6: Run tests**

Run: `go test ./internal/response/... ./internal/auth/...`
Expected: All tests pass

- [x] **Step 7: Commit**

```bash
git add internal/response/response.go internal/response/response_test.go internal/auth/staff_set_enabled.go internal/auth/staff_replace_roles.go
git commit -m "fix(auth): add FINAL_ENABLED_MANAGER_REQUIRED error code"
```

---

## Task 6: Include TargetID in Idempotency Fingerprint

**Files:**
- Modify: `internal/auth/staff_replace_roles.go:39`
- Modify: `internal/auth/staff_set_enabled.go:45`
- Modify: `internal/auth/staff_reset_pin.go:38`

**Interfaces:**
- Consumes: `ComputeRequestHash()`
- Produces: Idempotency key includes target resource ID

- [x] **Step 1: Create composite payload struct**

```go
// In internal/auth/idempotency.go, add helper
type idempotencyPayload struct {
    TargetID uuid.UUID `json:"target_id"`
    Body     any       `json:"body"`
}

func ComputeRequestHashWithTarget(action string, targetID uuid.UUID, payload any) string {
    return ComputeRequestHash(action, idempotencyPayload{TargetID: targetID, Body: payload})
}
```

- [x] **Step 2: Update staff_replace_roles.go**

```go
// Line 39, change from:
return ExecuteWithIdempotency(ctx, h.queries, actor.StaffID, req.RequestID, "staff.replace_roles", req, func() ...
// To:
return ExecuteWithIdempotency(ctx, h.queries, actor.StaffID, req.RequestID, "staff.replace_roles", idempotencyPayload{TargetID: targetID, Body: req}, func() ...
```

- [x] **Step 3: Update staff_set_enabled.go**

```go
// Line 45, change from:
return ExecuteWithIdempotency(ctx, h.queries, actor.StaffID, req.RequestID, "staff.set_enabled", req, func() ...
// To:
return ExecuteWithIdempotency(ctx, h.queries, actor.StaffID, req.RequestID, "staff.set_enabled", idempotencyPayload{TargetID: targetID, Body: req}, func() ...
```

- [x] **Step 4: Update staff_reset_pin.go**

```go
// Line 38, change from:
return ExecuteWithIdempotency(ctx, h.queries, actor.StaffID, req.RequestID, "staff.reset_pin", req, func() ...
// To:
return ExecuteWithIdempotency(ctx, h.queries, actor.StaffID, req.RequestID, "staff.reset_pin", idempotencyPayload{TargetID: targetID, Body: req}, func() ...
```

- [x] **Step 5: Add test for target-specific idempotency**

```go
// In internal/auth/idempotency_test.go
func TestIdempotency_TargetSpecific(t *testing.T) {
    // Test that same request_id + different target_id returns ErrConflict
    // Test that same request_id + same target_id returns cached result
}
```

- [x] **Step 6: Run tests**

Run: `go test ./internal/auth/...`
Expected: All tests pass

- [x] **Step 7: Commit**

```bash
git add internal/auth/idempotency.go internal/auth/idempotency_test.go internal/auth/staff_replace_roles.go internal/auth/staff_set_enabled.go internal/auth/staff_reset_pin.go
git commit -m "fix(auth): include targetID in idempotency fingerprint"
```

---

## Task 7: Fix Bootstrap Manager Invariant Check

**Files:**
- Modify: `sql/queries/auth.sql:60-64`
- Regenerate: `internal/database/sqlc/auth.sql.go`

**Interfaces:**
- Consumes: `CountActiveManagers` query
- Produces: Bootstrap only when zero managers exist (any enabled or disabled)

- [x] **Step 1: Fix SQL query**

```sql
-- In sql/queries/auth.sql, line 60-64, change from:
-- name: CountActiveManagers :one
SELECT count(DISTINCT si.id)::bigint
FROM staff_identities si
JOIN staff_operational_roles sor ON sor.staff_identity_id = si.id
WHERE si.enabled = true AND sor.role = 'MANAGER';

-- To:
-- name: CountManagers :one
SELECT count(DISTINCT si.id)::bigint
FROM staff_identities si
JOIN staff_operational_roles sor ON sor.staff_identity_id = si.id
WHERE sor.role = 'MANAGER';
```

- [x] **Step 2: Regenerate sqlc**

Run: `sqlc generate`
Expected: Updated `auth.sql.go` with `CountManagers` method

- [x] **Step 3: Update bootstrap_manager.go**

```go
// In internal/auth/bootstrap_manager.go, line 47, change from:
activeCount, err := qtx.CountActiveManagers(ctx)
// To:
activeCount, err := qtx.CountManagers(ctx)
```

- [x] **Step 4: Update staff_set_enabled.go**

```go
// In internal/auth/staff_set_enabled.go, line 77, change from:
activeManagers, err := qtx.CountActiveManagers(ctx)
// To:
activeManagers, err := qtx.CountManagers(ctx)
```

- [x] **Step 5: Update staff_replace_roles.go**

```go
// In internal/auth/staff_replace_roles.go, line 69, change from:
activeManagers, err := qtx.CountActiveManagers(ctx)
// To:
activeManagers, err := qtx.CountManagers(ctx)
```

- [x] **Step 6: Update querier interface**

The `CountManagers` method should be added to the `Querier` interface. Verify `sqlc generate` handles this.

- [x] **Step 7: Run tests**

Run: `go test ./internal/auth/...`
Expected: All tests pass

- [x] **Step 8: Commit**

```bash
git add sql/queries/auth.sql internal/database/sqlc/ internal/auth/bootstrap_manager.go internal/auth/staff_set_enabled.go internal/auth/staff_replace_roles.go
git commit -m "fix(auth): bootstrap checks all managers, not just enabled"
```

---

## Task 8: Make Idempotency Atomic with Transaction

**Files:**
- Modify: `internal/auth/idempotency.go`

**Interfaces:**
- Consumes: `*sql.DB`, transaction
- Produces: Atomic mutation + idempotency record insertion

- [x] **Step 1: Write failing concurrent test**

```go
// In internal/auth/idempotency_test.go
func TestExecuteWithIdempotency_Concurrent(t *testing.T) {
    // Test that concurrent calls with same key only execute fn once
    // Use sync.WaitGroup and goroutines
    // Assert callCount == 1
}
```

- [x] **Step 2: Refactor ExecuteWithIdempotency to use transaction**

```go
func ExecuteWithIdempotency[T any](
    ctx context.Context,
    db *sql.DB,
    q *sqlc.Queries,
    actorID uuid.UUID,
    key uuid.UUID,
    action string,
    payload any,
    fn func(tx *sql.Tx, qtx *sqlc.Queries) (int, T, error),
) (int, T, error) {
    var zero T
    reqHash := ComputeRequestHash(action, payload)

    // Begin transaction
    tx, err := db.BeginTx(ctx, nil)
    if err != nil {
        return 0, zero, fmt.Errorf("begin idempotency tx: %w", err)
    }
    defer func() { _ = tx.Rollback() }()

    qtx := q.WithTx(tx)

    // Check if already executed (within transaction for serialization)
    existing, err := qtx.GetIdempotencyKey(ctx, sqlc.GetIdempotencyKeyParams{
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

    // Execute operation within same transaction
    code, result, err := fn(tx, qtx)
    if err != nil {
        return code, result, err
    }

    // Save idempotency record
    resultBytes, _ := json.Marshal(result)
    if _, err := tx.ExecContext(ctx, `
        INSERT INTO idempotency_keys (key, actor_id, action, request_hash, response_code, response_body)
        VALUES ($1, $2, $3, $4, $5, $6)
    `, key, actorID, action, reqHash, code, resultBytes); err != nil {
        return 0, zero, fmt.Errorf("save idempotency key: %w", err)
    }

    if err := tx.Commit(); err != nil {
        return 0, zero, fmt.Errorf("commit idempotency tx: %w", err)
    }

    return code, result, nil
}
```

- [x] **Step 3: Update all callers of ExecuteWithIdempotency**

Each caller must be updated to pass `db` and use the new signature `fn(tx *sql.Tx, qtx *sqlc.Queries)`:

- `internal/auth/staff_replace_roles.go`
- `internal/auth/staff_set_enabled.go`
- `internal/auth/staff_reset_pin.go`
- `internal/auth/staff_create.go`

- [x] **Step 4: Run tests**

Run: `go test ./internal/auth/...`
Expected: All tests pass including new concurrent test

- [x] **Step 5: Commit**

```bash
git add internal/auth/idempotency.go internal/auth/idempotency_test.go internal/auth/staff_replace_roles.go internal/auth/staff_set_enabled.go internal/auth/staff_reset_pin.go internal/auth/staff_create.go
git commit -m "fix(auth): make idempotency atomic with transaction"
```

---

## Task 9: Add Brute-Force Protection for PIN Endpoints

**Files:**
- Create: `internal/auth/ratelimit.go`
- Modify: `internal/auth/sign_in.go`
- Modify: `internal/auth/unlock_session.go`
- Modify: `cmd/api/main.go`

**Interfaces:**
- Consumes: Rate limiter middleware
- Produces: 429 Too Many Requests after N failed attempts

- [x] **Step 1: Create in-memory rate limiter**

```go
// internal/auth/ratelimit.go
package auth

import (
    "sync"
    "time"
)

type RateLimiter struct {
    mu       sync.Mutex
    attempts map[string][]time.Time
    max      int
    window   time.Duration
}

func NewRateLimiter(maxAttempts int, window time.Duration) *RateLimiter {
    return &RateLimiter{
        attempts: make(map[string][]time.Time),
        max:      maxAttempts,
        window:   window,
    }
}

func (r *RateLimiter) Allow(key string) bool {
    r.mu.Lock()
    defer r.mu.Unlock()

    now := time.Now()
    cutoff := now.Add(-r.window)

    // Clean old attempts
    attempts := r.attempts[key]
    valid := attempts[:0]
    for _, t := range attempts {
        if t.After(cutoff) {
            valid = append(valid, t)
        }
    }
    r.attempts[key] = valid

    if len(valid) >= r.max {
        return false
    }

    r.attempts[key] = append(r.attempts[key], now)
    return true
}

func (r *RateLimiter) Reset(key string) {
    r.mu.Lock()
    defer r.mu.Unlock()
    delete(r.attempts, key)
}
```

- [x] **Step 2: Create rate limit middleware**

```go
// In internal/auth/middleware.go, add
func (m *Middleware) RateLimit(limiter *RateLimiter, keyFunc func(c echo.Context) string) echo.MiddlewareFunc {
    return func(next echo.HandlerFunc) echo.HandlerFunc {
        return func(c echo.Context) error {
            key := keyFunc(c)
            if !limiter.Allow(key) {
                return response.Error(c, fmt.Errorf("%w: quá nhiều lần thử, vui lòng thử lại sau", response.ErrUnauthorized))
            }
            return next(c)
        }
    }
}
```

- [x] **Step 3: Apply rate limiter to sign-in**

```go
// In internal/auth/routes.go
signInLimiter := NewRateLimiter(5, 15*time.Minute) // 5 attempts per 15 minutes
authGroup.POST("/signIn", s.SignIn.HandleHTTP, s.Middleware.RateLimit(signInLimiter, func(c echo.Context) string {
    var req struct{ LoginCode string `json:"login_code"` }
    _ = c.Bind(&req)
    return "signIn:" + req.LoginCode
}))
```

- [x] **Step 4: Apply rate limiter to unlock**

```go
// In internal/auth/routes.go
unlockLimiter := NewRateLimiter(3, 5*time.Minute) // 3 attempts per 5 minutes
authGroup.POST("/unlock", s.Unlock.HandleHTTP, s.Middleware.RateLimit(unlockLimiter, func(c echo.Context) string {
    token := extractToken(c)
    return "unlock:" + token
}))
```

- [x] **Step 5: Reset rate limit on successful auth**

```go
// In sign_in.go, after successful auth (line 53)
signInLimiter.Reset("signIn:" + req.LoginCode)

// In unlock_session.go, after successful unlock
unlockLimiter.Reset("unlock:" + token)
```

- [x] **Step 6: Add tests**

```go
// In internal/auth/idempotency_test.go or new file
func TestRateLimiter(t *testing.T) {
    limiter := NewRateLimiter(3, time.Minute)
    
    assert.True(t, limiter.Allow("key1"))
    assert.True(t, limiter.Allow("key1"))
    assert.True(t, limiter.Allow("key1"))
    assert.False(t, limiter.Allow("key1")) // 4th attempt blocked
    
    limiter.Reset("key1")
    assert.True(t, limiter.Allow("key1")) // After reset, allowed again
}
```

- [x] **Step 7: Run tests**

Run: `go test ./internal/auth/...`
Expected: All tests pass

- [x] **Step 8: Commit**

```bash
git add internal/auth/ratelimit.go internal/auth/middleware.go internal/auth/sign_in.go internal/auth/unlock_session.go internal/auth/routes.go
git commit -m "fix(auth): add brute-force protection for PIN endpoints"
```

---

## Task 10: Update Integration Test Command in CI

**Files:**
- Modify: `.github/workflows/ci.yml` (or equivalent)

**Interfaces:**
- Consumes: Go test commands
- Produces: Integration tests run separately with correct build tags

- [x] **Step 1: Add integration test job**

```yaml
# In .github/workflows/ci.yml
- name: Run integration tests
  if: env.TEST_DATABASE_URL != ''
  run: go test -v -tags=integration -race -p 1 ./internal/auth/...
  env:
    TEST_DATABASE_URL: ${{ secrets.TEST_DATABASE_URL }}
```

- [x] **Step 2: Verify unit tests still run without integration tag**

```bash
go test ./internal/auth/...
```

- [x] **Step 3: Commit**

```bash
git add .github/workflows/
git commit -m "ci(auth): add integration test job with correct build tags"
```

---

## Execution Order

1. Task 1 (Session state contract) — no dependencies
2. Task 2 (Sign-out error propagation) — no dependencies
3. Task 3 (Unlock atomicity) — no dependencies
4. Task 4 (CORS PATCH) — no dependencies
5. Task 5 (Error code) — no dependencies
6. Task 6 (Idempotency fingerprint) — no dependencies
7. Task 7 (Bootstrap invariant) — no dependencies
8. Task 8 (Idempotency atomicity) — depends on Task 6 (both modify idempotency)
9. Task 9 (Brute-force protection) — no dependencies
10. Task 10 (CI integration tests) — no dependencies

Tasks 1-7 and 9-10 can be executed in parallel. Task 8 should be executed after Task 6.
