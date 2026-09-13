-- Queries for internal/sales (Phase 5A).
--
-- Authority, role, and advisory-lock queries are slice-local by ADR-007: the
-- shared table is shared, the helper logic is not.

-- name: GetSalesSessionAuthority :one
SELECT s.id AS session_id, s.staff_identity_id, s.state, s.active_workspace,
       s.last_human_activity_at, s.expires_at, s.revoked_at,
       i.enabled AS identity_enabled, i.display_name, i.login_code
FROM staff_access_sessions s
JOIN staff_identities i ON i.id = s.staff_identity_id
WHERE s.id = $1 AND s.staff_identity_id = $2;

-- name: GetSalesSessionRoles :many
SELECT role
FROM staff_operational_roles
WHERE staff_identity_id = $1
ORDER BY role ASC;

-- name: SalesAdvisoryLock :exec
SELECT pg_advisory_xact_lock($1);
