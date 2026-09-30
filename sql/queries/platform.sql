-- Queries shared by every vertical slice through internal/platform/command.

-- -- Authority --

-- name: GetSessionAuthority :one
-- Reloads a staff access session and its identity inside the command
-- transaction. The identity's PIN hash is deliberately not selected: gates that
-- verify a PIN load and lock the identity row themselves.
SELECT s.id AS session_id, s.staff_identity_id, s.state, s.active_workspace,
       s.last_human_activity_at, s.expires_at, s.revoked_at,
       i.enabled AS identity_enabled, i.display_name, i.login_code
FROM staff_access_sessions s
JOIN staff_identities i ON i.id = s.staff_identity_id
WHERE s.id = $1 AND s.staff_identity_id = $2;

-- name: GetSessionRoles :many
SELECT role
FROM staff_operational_roles
WHERE staff_identity_id = $1
ORDER BY role ASC;

-- name: AdvisoryXactLock :exec
SELECT pg_advisory_xact_lock($1);

-- -- Shared idempotency (ADR-005) --

-- name: GetIdempotencyRecord :one
SELECT key, actor_id, action, request_hash, response_code, response_body, created_at
FROM idempotency_keys
WHERE actor_id = $1 AND key = $2
LIMIT 1;

-- name: ClaimIdempotencyRecord :one
INSERT INTO idempotency_keys (key, actor_id, action, request_hash, response_code, response_body)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (actor_id, key) DO UPDATE
    SET response_code = idempotency_keys.response_code,
        response_body = idempotency_keys.response_body
RETURNING key, actor_id, action, request_hash, response_code, response_body, created_at;

-- name: StoreIdempotencyResult :exec
UPDATE idempotency_keys
SET response_code = $3, response_body = $4
WHERE actor_id = $1 AND key = $2;
