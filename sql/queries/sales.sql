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

-- name: GetServiceSession :one
SELECT id, service_number, sequence, service_mode, state, sales_shift_id,
       created_by_staff_identity_id, created_at
FROM service_sessions
WHERE id = $1;

-- name: ListServiceSessionTables :many
-- Current assignments only. Released rows are history, not occupancy.
SELECT t.id, t.name, a.sequence
FROM table_assignments a
JOIN tables t ON t.id = a.table_id
WHERE a.service_session_id = $1 AND a.released_at IS NULL
ORDER BY a.sequence ASC;

-- name: GetEditableDraft :one
SELECT id, service_session_id, state, created_at
FROM order_drafts
WHERE service_session_id = $1 AND state = 'EDITABLE';

-- name: ListDraftItems :many
-- price_vnd is the Size price when a Size is chosen and the Item price
-- otherwise, matching the canonical Menu Price rule. available is read live
-- rather than snapshotted: a draft is a live proposal, and an item that became
-- unavailable while the customer was deciding must show as such.
--
-- price_vnd reads the Size price through a correlated scalar subquery rather
-- than the column directly. The subquery returns the same value (it references
-- the already-joined row), but sqlc infers scalar subqueries as nullable, so
-- COALESCE yields sql.NullInt64 instead of a bare int64 that cannot scan the
-- NULL price of a required-Size Item whose Size has not been chosen yet.
SELECT di.id, di.menu_item_id, mi.name AS menu_item_name,
       di.size_id, s.name AS size_name,
       COALESCE((SELECT s.price_vnd), mi.price_vnd) AS price_vnd,
       di.quantity, di.preparation_note, di.modifier_key, di.created_at,
       (mi.available AND mi.retired_at IS NULL
        AND (di.size_id IS NULL OR (s.available AND s.retired_at IS NULL))) AS available
FROM order_draft_items di
JOIN menu_items mi ON mi.id = di.menu_item_id
LEFT JOIN menu_item_sizes s ON s.id = di.size_id
WHERE di.order_draft_id = $1
ORDER BY di.created_at ASC, di.id ASC;

-- name: ListDraftItemModifierOptions :many
-- Ordered by Group then Option name, which is the order the projection emits.
SELECT m.order_draft_item_id, o.id AS option_id, o.name AS option_name,
       o.surcharge_vnd, g.id AS group_id, g.name AS group_name
FROM order_draft_item_modifier_options m
JOIN modifier_options o ON o.id = m.modifier_option_id
JOIN modifier_groups g ON g.id = o.modifier_group_id
JOIN order_draft_items di ON di.id = m.order_draft_item_id
WHERE di.order_draft_id = $1
ORDER BY g.name ASC, o.name ASC, o.id ASC;

-- name: ListActiveServiceSessions :many
SELECT id, service_number, sequence, service_mode, state, sales_shift_id,
       created_by_staff_identity_id, created_at
FROM service_sessions
WHERE state = 'ACTIVE'
ORDER BY created_at ASC, id ASC;
