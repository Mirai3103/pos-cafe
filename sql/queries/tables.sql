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
