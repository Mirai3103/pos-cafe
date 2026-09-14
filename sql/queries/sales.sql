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
SELECT id, service_session_id, state, check_target, created_at
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

-- name: InsertTableAssignmentsBatch :many
-- Batches assignTables' per-Table insert loop into one round trip. Two
-- single-array unnests joined by WITH ORDINALITY zip table_ids and sequences
-- into rows in lockstep, so row i of the result is table_ids[i] assigned at
-- sequences[i]; the caller relies on getting exactly len(table_ids) rows back
-- in that order.
INSERT INTO table_assignments (table_id, service_session_id, assigned_by_staff_identity_id, sequence)
SELECT tid.val, $2::uuid, $3::uuid, seq.val
FROM unnest($1::uuid[]) WITH ORDINALITY AS tid(val, ord)
JOIN unnest($4::int[]) WITH ORDINALITY AS seq(val, ord) ON seq.ord = tid.ord
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

-- name: LockMenuItemSizeForDraft :one
-- Locked FOR UPDATE for the same reason as LockMenuItemForDraft: without it, a
-- concurrent retirement of this Size can slip between validation and the
-- draft-item write.
SELECT id, menu_item_id, name, price_vnd, available, retired_at
FROM menu_item_sizes
WHERE id = $1
FOR UPDATE;

-- name: FindDraftItemByComposition :one
SELECT id, quantity
FROM order_draft_items
WHERE order_draft_id = $1
  AND menu_item_id = $2
  AND size_key = COALESCE(sqlc.narg(size_id)::uuid::text, '')
  AND note_key = COALESCE(sqlc.narg(preparation_note)::text, '')
  AND modifier_key = $3;

-- name: FindDraftItemByCompositionExcluding :one
-- findDraftItemByComposition plus an id <> $n clause, so the row being edited
-- never matches itself. A separate query rather than a nullable exclusion
-- parameter keeps the add path's query untouched.
SELECT id, quantity
FROM order_draft_items
WHERE order_draft_id = $1
  AND menu_item_id = $2
  AND size_key = COALESCE(sqlc.narg(size_id)::uuid::text, '')
  AND note_key = COALESCE(sqlc.narg(preparation_note)::text, '')
  AND modifier_key = $3
  AND id <> $4;

-- name: InsertDraftItem :one
INSERT INTO order_draft_items
    (order_draft_id, menu_item_id, size_id, preparation_note, modifier_key, quantity)
VALUES ($1, $2, $3, $4, $5, 1)
RETURNING id, quantity;

-- name: UpdateDraftItemComposition :exec
-- size_key and note_key are generated columns, so they follow the write.
UPDATE order_draft_items
SET size_id = $2, preparation_note = $3, modifier_key = $4
WHERE id = $1;

-- name: SetDraftItemQuantity :one
UPDATE order_draft_items
SET quantity = $2
WHERE id = $1
RETURNING id, quantity;

-- name: LockDraftItem :one
-- Scoped to the draft, so a caller cannot reach an item of another Session by
-- guessing its id.
SELECT id, order_draft_id, menu_item_id, size_id, quantity, preparation_note, modifier_key
FROM order_draft_items
WHERE id = $1 AND order_draft_id = $2
FOR UPDATE;

-- name: ListDraftItemOptionIDs :many
SELECT modifier_option_id
FROM order_draft_item_modifier_options
WHERE order_draft_item_id = $1
ORDER BY modifier_option_id ASC;

-- name: DeleteDraftItem :exec
DELETE FROM order_draft_items WHERE id = $1;

-- name: DeleteDraftItemModifierOptions :exec
DELETE FROM order_draft_item_modifier_options WHERE order_draft_item_id = $1;

-- name: InsertDraftItemModifierOption :exec
INSERT INTO order_draft_item_modifier_options (order_draft_item_id, modifier_option_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: LockDraftItemsForCommit :many
-- Every draft item with the Catalog facts Commit revalidates against, locked
-- so the rows cannot change between validation and write: the draft items FOR
-- UPDATE, and the joined Menu Items FOR SHARE per ADR-015's lock list.
-- Ordered by (created_at, id), which also fixes the order of the Committed
-- Items.
SELECT di.id, di.menu_item_id, di.size_id, di.quantity, di.preparation_note,
       mi.name AS item_name, mi.price_vnd AS item_price_vnd,
       mi.available AS item_available,
       (mi.retired_at IS NOT NULL) AS item_retired,
       mc.name AS category_name
FROM order_draft_items di
JOIN menu_items mi ON mi.id = di.menu_item_id
JOIN menu_categories mc ON mc.id = mi.category_id
WHERE di.order_draft_id = $1
ORDER BY di.created_at ASC, di.id ASC
FOR UPDATE OF di FOR SHARE OF mi;

-- name: ListDraftItemOptionsForCommit :many
-- The selected Options of the given draft items, with the Group facts the
-- Commit rules need. The Options are locked FOR SHARE per ADR-015's lock
-- list, so a retirement or availability change cannot land between the
-- Commit validation and its writes; modifier_groups stay unlocked.
SELECT dio.order_draft_item_id, o.id AS option_id, o.name AS option_name,
       o.surcharge_vnd, o.available,
       (o.retired_at IS NOT NULL) AS option_retired,
       g.id AS group_id, g.name AS group_name,
       (g.retired_at IS NOT NULL) AS group_retired
FROM order_draft_item_modifier_options dio
JOIN modifier_options o ON o.id = dio.modifier_option_id
JOIN modifier_groups g ON g.id = o.modifier_group_id
WHERE dio.order_draft_item_id = ANY(sqlc.arg(draft_item_ids)::uuid[])
ORDER BY g.name ASC, o.name ASC, o.id ASC
FOR SHARE OF o;

-- name: ListEffectiveModifierGroupsForCommit :many
-- (inherited - exclusions) + direct, for a SET of Menu Items, returning the
-- selection rules Commit enforces.
--
-- This is the second expression of the algebra ListEffectiveModifierGroupIDs
-- already encodes. TestSalesResolutionMatchesCatalog pins both to
-- catalog.EffectiveGroupIDs over shared fixtures so they cannot drift. The
-- single-item query is left alone: the draft path does not need min/max and
-- should not pay for them. See ADR-012.
WITH targets AS (
    SELECT id, category_id FROM menu_items
    WHERE id = ANY(sqlc.arg(menu_item_ids)::uuid[])
),
inherited AS (
    SELECT t.id AS menu_item_id, cmg.modifier_group_id
    FROM targets t
    JOIN category_modifier_groups cmg ON cmg.menu_category_id = t.category_id
    WHERE NOT EXISTS (
        SELECT 1 FROM item_modifier_group_exclusions ex
        WHERE ex.menu_item_id = t.id
          AND ex.modifier_group_id = cmg.modifier_group_id
    )
),
direct AS (
    SELECT t.id AS menu_item_id, img.modifier_group_id
    FROM targets t
    JOIN item_modifier_groups img ON img.menu_item_id = t.id
),
effective AS (
    SELECT menu_item_id, modifier_group_id FROM inherited
    UNION
    SELECT menu_item_id, modifier_group_id FROM direct
)
SELECT e.menu_item_id, e.modifier_group_id, g.name AS group_name,
       g.min_selections, g.max_selections,
       (g.retired_at IS NOT NULL) AS group_retired
FROM effective e
JOIN modifier_groups g ON g.id = e.modifier_group_id
ORDER BY e.menu_item_id ASC, e.modifier_group_id ASC;

-- name: LockMenuItemSizesForCommit :many
-- Locked FOR SHARE: Commit only reads these rows and must merely prevent a
-- retirement or availability change landing mid-transaction. FOR UPDATE would
-- serialize two cashiers committing orders that share a popular item, on the
-- busiest path in the system, for no correctness gain. internal/catalog's
-- mutations take FOR UPDATE and are still excluded. See ADR-015.
SELECT id, menu_item_id, name, price_vnd, available,
       (retired_at IS NOT NULL) AS size_retired
FROM menu_item_sizes
WHERE id = ANY(sqlc.arg(size_ids)::uuid[])
ORDER BY id ASC
FOR SHARE;

-- name: LockCurrentOpenCheck :one
-- The Session's most recent OPEN Check, for the CURRENT_UNPAID target.
SELECT id, charge_vnd
FROM checks
WHERE service_session_id = $1 AND state = 'OPEN'
ORDER BY created_at DESC, id DESC
LIMIT 1
FOR UPDATE;

-- name: InsertCheck :one
INSERT INTO checks (service_session_id, created_at)
VALUES ($1, $2)
RETURNING id, charge_vnd;

-- name: RaiseCheckCharge :exec
UPDATE checks SET charge_vnd = $2 WHERE id = $1;

-- name: InsertCommittedItem :one
INSERT INTO committed_items (
    order_draft_id, source_draft_item_id, menu_item_id, category_name,
    item_name, size_name, quantity, unit_price_vnd, total_vnd,
    preparation_note, committed_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING id;

-- name: InsertCommittedItemModifierOption :exec
INSERT INTO committed_item_modifier_options (
    committed_item_id, modifier_group_id, modifier_group_name,
    modifier_option_id, modifier_option_name, surcharge_vnd
) VALUES ($1, $2, $3, $4, $5, $6);

-- name: InsertChargeAllocation :exec
INSERT INTO charge_allocations (committed_item_id, check_id, quantity, created_at)
VALUES ($1, $2, $3, $4);

-- name: MarkOrderDraftCommitted :exec
UPDATE order_drafts SET state = 'COMMITTED' WHERE id = $1;

-- name: SetOrderDraftCheckTarget :exec
UPDATE order_drafts SET check_target = $2 WHERE id = $1;

-- name: GetOrderDraftCheckTarget :one
SELECT check_target FROM order_drafts WHERE id = $1;

-- name: FindBlockingDraft :one
-- A draft that prevents a new one opening: EDITABLE, or COMMITTED without a
-- corresponding Order.
--
-- 5B has no orders table, so the second clause matches every COMMITTED draft
-- and a Session that has committed once cannot open another draft. That dead
-- end is deliberate and disappears when 5D adds the orders join here: the
-- rule exists to stop staff stacking rounds ahead of the kitchen, and
-- relaxing it now would ship a rule no phase wants. See the spec's accepted
-- consequences.
SELECT id
FROM order_drafts
WHERE service_session_id = $1
  AND state IN ('EDITABLE', 'COMMITTED')
ORDER BY created_at ASC, id ASC
LIMIT 1
FOR UPDATE;

-- name: InsertOrderDraftForSession :one
INSERT INTO order_drafts (service_session_id, created_at)
VALUES ($1, $2)
RETURNING id, state, check_target;

-- name: ListSessionChecks :many
SELECT id, state, charge_vnd, merged_into_check_id, created_at
FROM checks
WHERE service_session_id = $1
ORDER BY created_at ASC, id ASC;

-- name: ListCheckPayments :many
-- Ordered by (received_at, id), served directly by payment_check_index.
SELECT id, method, applied_amount_vnd, cash_tendered_vnd, change_due_vnd,
       transaction_reference, sales_shift_id, received_at
FROM payments
WHERE check_id = $1
ORDER BY received_at ASC, id ASC;

-- name: ListCheckAllocations :many
SELECT ca.id, ca.quantity AS allocated_quantity, ca.created_at,
       ci.id AS committed_item_id, ci.menu_item_id, ci.category_name,
       ci.item_name, ci.size_name, ci.quantity AS committed_quantity,
       ci.unit_price_vnd, ci.total_vnd AS committed_total_vnd,
       ci.preparation_note
FROM charge_allocations ca
JOIN committed_items ci ON ci.id = ca.committed_item_id
WHERE ca.check_id = $1
ORDER BY ci.committed_at ASC, ci.id ASC;

-- name: ListCommittedItemModifiers :many
SELECT committed_item_id, modifier_group_id, modifier_group_name,
       modifier_option_id, modifier_option_name, surcharge_vnd
FROM committed_item_modifier_options
WHERE committed_item_id = ANY(sqlc.arg(committed_item_ids)::uuid[])
ORDER BY modifier_group_name ASC, modifier_option_name ASC;

-- name: LockCheckForPayment :one
-- The uniform 5C lock protocol (ADR-016): the Check row FOR UPDATE, its
-- parents FOR SHARE. The parents are only read to evaluate a precondition, so
-- locking them FOR UPDATE would serialize two cashiers paying different
-- Checks of one Session for no correctness gain.
--
-- No row means the Check id does not exist. The state columns come back
-- unfiltered so the caller can report which precondition failed.
SELECT c.id, c.state, c.charge_vnd,
       s.id AS service_session_id, s.state AS service_session_state,
       sh.id AS sales_shift_id, sh.state AS sales_shift_state
FROM checks c
JOIN service_sessions s ON s.id = c.service_session_id
JOIN sales_shifts sh ON sh.id = s.sales_shift_id
WHERE c.id = $1
FOR UPDATE OF c
FOR SHARE OF s, sh;

-- name: SumCheckPayments :one
SELECT COALESCE(SUM(applied_amount_vnd), 0)::BIGINT AS total_applied_vnd
FROM payments
WHERE check_id = $1;

-- name: InsertPayment :one
INSERT INTO payments (
    check_id, sales_shift_id, actor_staff_identity_id, staff_access_session_id,
    applied_amount_vnd, method, cash_tendered_vnd, change_due_vnd,
    transaction_reference, received_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING id;

-- name: SettleCheck :exec
-- Writes all four evidence columns together, because the composite constraint
-- check_settlement_evidence_valid rejects any partial set.
UPDATE checks
SET state = 'SETTLED',
    settled_at = $2,
    settled_by_staff_identity_id = $3,
    settled_during_sales_shift_id = $4,
    settled_staff_access_session_id = $5
WHERE id = $1;
