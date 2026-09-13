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

-- name: GetOpenSalesShiftID :one
-- Sales reads the Shift-owned table through its own query rather than
-- importing internal/shift, per ADR-006's precedent.
SELECT id FROM sales_shifts WHERE state = 'OPEN' LIMIT 1;

-- name: GetNextServiceSequence :one
-- Callers MUST hold the advisory lock on the Sales Shift before running this.
-- Without it two concurrent opens read the same maximum and one loses to the
-- unique index.
SELECT COALESCE(MAX(sequence), 0)::int + 1 AS next_sequence
FROM service_sessions
WHERE sales_shift_id = $1;

-- name: InsertServiceSession :one
INSERT INTO service_sessions
    (service_number, sequence, service_mode, state,
     created_by_staff_identity_id, sales_shift_id)
VALUES ($1, $2, $3, 'ACTIVE', $4, $5)
RETURNING id, service_number, sequence, service_mode, state, sales_shift_id, created_at;

-- name: InsertOrderDraft :one
INSERT INTO order_drafts (service_session_id) VALUES ($1)
RETURNING id, service_session_id, state, created_at;

-- name: LockTablesForAssignment :many
-- Locks the selected Tables in id order so two concurrent assignments over
-- overlapping sets cannot deadlock against each other. The caller must sort
-- the ids before calling.
SELECT id, name, available
FROM tables
WHERE id = ANY(sqlc.arg(table_ids)::uuid[])
ORDER BY id ASC
FOR UPDATE;

-- name: InsertTableAssignment :one
INSERT INTO table_assignments
    (table_id, service_session_id, assigned_by_staff_identity_id, sequence)
VALUES ($1, $2, $3, $4)
RETURNING id, table_id, service_session_id, sequence, assigned_at;

-- name: GetHighestAssignmentSequence :one
-- Includes released assignments, so a released sequence number is never
-- reused and the audit trail stays unambiguous.
SELECT COALESCE(MAX(sequence), -1)::int AS highest
FROM table_assignments
WHERE service_session_id = $1;

-- name: LockServiceSessionForUpdate :one
SELECT id, service_mode, state, sales_shift_id
FROM service_sessions
WHERE id = $1
FOR UPDATE;

-- name: LockCurrentTableAssignments :many
SELECT id, table_id, sequence
FROM table_assignments
WHERE service_session_id = $1 AND released_at IS NULL
ORDER BY sequence ASC
FOR UPDATE;

-- name: ReleaseTableAssignment :exec
-- released_at and released_by_staff_identity_id must be set together; the
-- table_assignment_release_evidence_valid constraint from migration 000006
-- rejects one without the other.
UPDATE table_assignments
SET released_at = now(), released_by_staff_identity_id = $2
WHERE id = $1;

-- name: GetSalesShiftStateByID :one
SELECT state FROM sales_shifts WHERE id = $1;

-- name: ListEffectiveModifierGroupIDs :many
-- (inherited - exclusions) + direct, for one Menu Item.
--
-- internal/catalog computes the same set in the exported pure function
-- EffectiveGroupIDs. Sales does not import it: MIGRATE_PLAN section 4.1
-- forbids importing another slice, and ADR-006 established reading another
-- slice's tables through one's own query. Expressing the algebra once in SQL
-- keeps the duplication to one function against one query, which
-- TestSalesResolutionMatchesCatalog pins together. See ADR-012.
WITH target AS (
    SELECT id, category_id FROM menu_items WHERE id = $1
),
inherited AS (
    SELECT cmg.modifier_group_id
    FROM category_modifier_groups cmg
    JOIN target ON target.category_id = cmg.menu_category_id
    WHERE NOT EXISTS (
        SELECT 1
        FROM item_modifier_group_exclusions ex
        WHERE ex.menu_item_id = (SELECT id FROM target)
          AND ex.modifier_group_id = cmg.modifier_group_id
    )
),
direct AS (
    SELECT img.modifier_group_id
    FROM item_modifier_groups img
    JOIN target ON target.id = img.menu_item_id
)
SELECT modifier_group_id
FROM (
    SELECT modifier_group_id FROM inherited
    UNION
    SELECT modifier_group_id FROM direct
) g
ORDER BY modifier_group_id ASC;

-- name: ListDefaultModifierOptionIDs :many
-- Declared defaults of the given Groups, filtered to what is currently
-- selectable. A retired Group's defaults never apply.
SELECT DISTINCT o.id
FROM modifier_group_default_options d
JOIN modifier_options o ON o.id = d.modifier_option_id
JOIN modifier_groups g ON g.id = o.modifier_group_id
WHERE d.modifier_group_id = ANY(sqlc.arg(group_ids)::uuid[])
  AND o.available
  AND o.retired_at IS NULL
  AND g.retired_at IS NULL
ORDER BY o.id ASC;

-- name: ListModifierOptionsForValidation :many
SELECT o.id, o.modifier_group_id, o.available,
       (o.retired_at IS NOT NULL) AS option_retired,
       (g.retired_at IS NOT NULL) AS group_retired
FROM modifier_options o
JOIN modifier_groups g ON g.id = o.modifier_group_id
WHERE o.id = ANY(sqlc.arg(option_ids)::uuid[]);

-- name: LockEditableDraft :one
-- Checks every precondition and takes the lock in one statement, so there is
-- no window between the check and the write.
--
-- No match means the Session is missing, closed, its draft already committed,
-- or its Sales Shift closed. The caller reports EDITABLE_DRAFT_NOT_FOUND for
-- all four: the remedy is the same, and distinguishing them would leak state
-- about Sessions the caller did not ask about.
SELECT d.id AS order_draft_id, s.id AS service_session_id, s.service_mode
FROM service_sessions s
JOIN order_drafts d ON d.service_session_id = s.id
JOIN sales_shifts sh ON sh.id = s.sales_shift_id
WHERE s.id = $1
  AND s.state = 'ACTIVE'
  AND d.state = 'EDITABLE'
  AND sh.state = 'OPEN'
FOR UPDATE OF d, s;

-- name: LockMenuItemForDraft :one
-- Locked FOR UPDATE so the Item cannot be retired between validation and
-- write. Sales rows are always locked before Catalog rows, and
-- internal/catalog never locks Sales rows, so no deadlock cycle exists.
SELECT id, category_id, name, price_vnd, available, retired_at
FROM menu_items
WHERE id = $1
FOR UPDATE;

-- name: GetMenuItemSizeForDraft :one
SELECT id, menu_item_id, name, price_vnd, available, retired_at
FROM menu_item_sizes
WHERE id = $1;

-- name: FindDraftItemByComposition :one
SELECT id, quantity
FROM order_draft_items
WHERE order_draft_id = $1
  AND menu_item_id = $2
  AND size_key = COALESCE(sqlc.narg(size_id)::uuid::text, '')
  AND note_key = COALESCE(sqlc.narg(preparation_note)::text, '')
  AND modifier_key = $3;

-- name: InsertDraftItem :one
INSERT INTO order_draft_items
    (order_draft_id, menu_item_id, size_id, preparation_note, modifier_key, quantity)
VALUES ($1, $2, $3, $4, $5, 1)
RETURNING id, quantity;

-- name: SetDraftItemQuantity :one
UPDATE order_draft_items
SET quantity = $2
WHERE id = $1
RETURNING id, quantity;

-- name: DeleteDraftItemModifierOptions :exec
DELETE FROM order_draft_item_modifier_options WHERE order_draft_item_id = $1;

-- name: InsertDraftItemModifierOption :exec
INSERT INTO order_draft_item_modifier_options (order_draft_item_id, modifier_option_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;
