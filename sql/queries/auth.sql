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

-- name: CountManagers :one
SELECT count(DISTINCT si.id)::bigint
FROM staff_identities si
JOIN staff_operational_roles sor ON sor.staff_identity_id = si.id
WHERE sor.role = 'MANAGER';

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
