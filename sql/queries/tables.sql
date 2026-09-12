-- -- Authority --

-- name: GetTablesSessionAuthority :one
SELECT s.id AS session_id, s.staff_identity_id, s.state, s.active_workspace,
       s.last_human_activity_at, s.expires_at, s.revoked_at,
       i.enabled AS identity_enabled, i.pin_hash
FROM staff_access_sessions s
JOIN staff_identities i ON i.id = s.staff_identity_id
WHERE s.id = $1 AND s.staff_identity_id = $2;

-- name: GetTablesSessionRoles :many
SELECT role
FROM staff_operational_roles
WHERE staff_identity_id = $1
ORDER BY role ASC;

-- name: TablesAdvisoryLock :exec
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

-- -- Tables --

-- name: CreateTable :one
INSERT INTO tables (name, normalized_name)
VALUES ($1, $2)
RETURNING id, name, normalized_name, available, created_at, updated_at;

-- name: GetTableForUpdate :one
SELECT id, name, normalized_name, available, created_at, updated_at
FROM tables
WHERE id = $1
FOR UPDATE;

-- name: RenameTable :one
UPDATE tables
SET name = $2, normalized_name = $3, updated_at = now()
WHERE id = $1
RETURNING id, name, normalized_name, available, created_at, updated_at;

-- name: SetTableAvailability :one
UPDATE tables
SET available = $2, updated_at = now()
WHERE id = $1
RETURNING id, name, normalized_name, available, created_at, updated_at;

-- name: ListTables :many
SELECT id, name, normalized_name, available, created_at, updated_at
FROM tables
ORDER BY created_at ASC, id ASC;

-- -- Occupancy (read-only view of Sales-owned tables) --

-- name: ListCurrentTableOccupants :many
SELECT ta.table_id, ss.id AS service_session_id, ss.service_number
FROM table_assignments ta
JOIN service_sessions ss ON ss.id = ta.service_session_id
WHERE ta.released_at IS NULL AND ss.state = 'ACTIVE'
ORDER BY ta.assigned_at ASC, ta.id ASC;
