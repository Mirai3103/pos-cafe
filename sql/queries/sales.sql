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

-- name: GetSalesOccurredAt :one
SELECT clock_timestamp()::timestamptz AS occurred_at;

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
-- 5D added the orders table and completed the second clause as 5B's comment
-- promised. The rule stops staff stacking rounds ahead of the kitchen; it does
-- not limit a Service Session to one round.
--
-- NOT EXISTS rather than a LEFT JOIN, for the reason LockSubmittableDraft
-- gives: PostgreSQL refuses row locks across a LEFT JOIN's nullable side.
-- (The outer service_session_id is spelled order_drafts.service_session_id
-- because sqlc's analyzer, unlike PostgreSQL, sees the subquery's orders
-- column of the same name and calls the bare reference ambiguous.)
SELECT id
FROM order_drafts
WHERE order_drafts.service_session_id = $1
  AND (
        state = 'EDITABLE'
     OR (state = 'COMMITTED'
         AND NOT EXISTS (SELECT 1 FROM orders o WHERE o.order_draft_id = order_drafts.id))
  )
FOR UPDATE
LIMIT 1;

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
-- Ordered by (received_at, id), served directly by payment_check_index. The
-- LEFT JOIN carries Phase 6C Payment Void evidence so the Check projection can
-- present and subtract a voided Payment without a second read; void_id is null
-- while the Payment stands.
SELECT p.id, p.method, p.applied_amount_vnd, p.cash_tendered_vnd,
       p.change_due_vnd, p.transaction_reference, p.sales_shift_id,
       p.received_at,
       pv.id AS void_id,
       pv.amount_vnd AS void_amount_vnd,
       pv.reason AS void_reason,
       pv.note AS void_note,
       pv.actor_staff_identity_id AS void_actor_staff_identity_id,
       pv.approved_by_staff_identity_id AS void_approved_by_staff_identity_id,
       pv.occurred_at AS void_occurred_at
FROM payments AS p
LEFT JOIN payment_voids AS pv ON pv.payment_id = p.id
WHERE p.check_id = $1
ORDER BY p.received_at ASC, p.id ASC;

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
-- The 5C lock protocol (ADR-016 as amended by ADR-030), first half: the Check
-- row FOR UPDATE. SQL does not guarantee that one statement's FOR UPDATE OF c, s
-- acquires the two relations' tuple locks in OF-list order, so the Session lock
-- is a separate statement: the caller locks the Check here and its Session
-- through LockServiceSessionForUpdate immediately afterwards, which is the same
-- Check-then-Session order lockChecks uses for restructurings. See ADR-030.
--
-- The Session is exclusive (FOR UPDATE, not FOR SHARE), because a Payment does
-- not merely read the Session to evaluate a precondition -- it rebuilds the
-- whole Service Session read model through LoadServiceSession inside the same
-- READ COMMITTED transaction, and that rebuild is several statements. A sibling
-- Check's commit landing between them is observed half-applied and trips the
-- settlement invariant, which rolls back a valid Payment.
--
-- The Shift is deliberately absent. A Payment's sales_shift_id is the Shift
-- open at the moment of the Payment, which is not necessarily the one the
-- Session was opened in (ADR-019), so it comes from LockOpenSalesShiftForShare.
--
-- No row means the Check id does not exist. The state columns come back
-- unfiltered so the caller can report which precondition failed.
SELECT c.id, c.state, c.charge_vnd, c.service_session_id
FROM checks c
WHERE c.id = $1
FOR UPDATE;

-- name: LockOpenSalesShiftForShare :one
-- The Sales Shift open right now, locked FOR SHARE. Only one Shift can be
-- active (OPEN or CLOSING) at a time, enforced by
-- sales_shift_only_one_active_unique, so no ordering or disambiguation is
-- needed.
--
-- Read from sales_shifts rather than through the Check's Session. The Shift in
-- which money reached the cashier is an independent fact — a Session opened in
-- one Shift can be paid in the next — which is why ADR-019 stores it on the
-- Payment at all.
--
-- FOR SHARE, not FOR UPDATE: every command here only reads the Shift to
-- evaluate a precondition. Shift closure takes FOR UPDATE and stays excluded
-- for the duration of the transaction. See §6.1.
--
-- No row means no Shift is open.
SELECT id
FROM sales_shifts
WHERE state = 'OPEN'
LIMIT 1
FOR SHARE;

-- name: SumCheckPayments :one
SELECT COALESCE(SUM(applied_amount_vnd), 0)::BIGINT AS total_applied_vnd
FROM payments
WHERE check_id = $1;

-- name: SumCheckAllocatedCharge :one
-- The live sum that a Check's stored charge_vnd denormalizes, in one round trip
-- rather than loading every allocation and its modifiers to add them up.
--
-- The caller compares this against the stored value; it is evidence, never
-- authority. The product cannot overflow BIGINT because charge_allocations
-- bounds quantity to 1..9999, and PostgreSQL would raise rather than wrap.
SELECT COALESCE(SUM(ca.quantity::BIGINT * ci.unit_price_vnd), 0)::BIGINT AS allocated_vnd
FROM charge_allocations ca
JOIN committed_items ci ON ci.id = ca.committed_item_id
WHERE ca.check_id = $1;

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

-- name: LockChecksForRestructuring :many
-- The 5C lock protocol over a set of Checks (ADR-016 as amended by ADR-030),
-- ordered by id so two concurrent restructurings take the rows in the same
-- order and cannot deadlock against each other or against a Payment.
--
-- The Session is deliberately NOT locked here, and `service_sessions` is not
-- joined. Locking it inside this statement would place it between two Check
-- locks (check(A) -> session -> check(B)) and create a cycle against a Payment
-- that already holds check(B) and is waiting for the Session. The caller locks
-- every Check first and the Session afterwards, which is the same order
-- LockCheckForPayment uses. See lockChecks and ADR-030.
--
-- As in LockCheckForPayment, the Shift is not joined: the Shift precondition
-- is about the Shift open now, which LockOpenSalesShiftForShare reads.
SELECT c.id, c.state, c.charge_vnd, c.service_session_id
FROM checks c
WHERE c.id = ANY($1::uuid[])
ORDER BY c.id
FOR UPDATE;

-- name: CountPaymentsForChecks :one
SELECT count(*)::BIGINT AS payment_count
FROM payments
WHERE check_id = ANY($1::uuid[]);

-- name: ListAllocationsForItems :many
-- One Check's allocations restricted to a set of Committed Items, with the
-- frozen unit price the moved amount is computed from.
--
-- The array argument is named through sqlc.arg so the generated params struct
-- carries CommittedItemIds rather than a positional Column2.
SELECT ca.id, ca.committed_item_id, ca.quantity, ci.unit_price_vnd
FROM charge_allocations ca
JOIN committed_items ci ON ci.id = ca.committed_item_id
WHERE ca.check_id = sqlc.arg(check_id)
  AND ca.committed_item_id = ANY(sqlc.arg(committed_item_ids)::uuid[])
ORDER BY ca.committed_item_id;

-- name: ListCheckAllocationQuantities :many
SELECT id, committed_item_id, quantity
FROM charge_allocations
WHERE check_id = $1
ORDER BY committed_item_id;

-- name: SetAllocationQuantities :exec
-- A whole set of quantity rewrites in one statement. Split and Merge compute
-- the new quantities in Go and hand the batch over, so the work done while the
-- Checks are locked is a fixed number of round trips rather than one per
-- allocation touched.
UPDATE charge_allocations ca
SET quantity = q.quantity
FROM unnest(sqlc.arg(ids)::uuid[]) WITH ORDINALITY AS d(id, ord)
JOIN unnest(sqlc.arg(quantities)::int[]) WITH ORDINALITY AS q(quantity, ord) ON q.ord = d.ord
WHERE ca.id = d.id;

-- name: DeleteAllocations :exec
DELETE FROM charge_allocations
WHERE id = ANY(sqlc.arg(ids)::uuid[]);

-- name: MoveAllocationsToCheck :exec
UPDATE charge_allocations
SET check_id = sqlc.arg(check_id)
WHERE id = ANY(sqlc.arg(ids)::uuid[]);

-- name: InsertChargeAllocations :exec
-- The batched counterpart of InsertChargeAllocation, for a Split's destination
-- side. charge_allocation_item_check_unique allows at most one allocation per
-- (Committed Item, Check), so the caller lists only the items that do not have
-- one yet.
INSERT INTO charge_allocations (committed_item_id, check_id, quantity, created_at)
SELECT i.committed_item_id, sqlc.arg(check_id)::uuid, q.quantity, sqlc.arg(created_at)::timestamptz
FROM unnest(sqlc.arg(committed_item_ids)::uuid[]) WITH ORDINALITY AS i(committed_item_id, ord)
JOIN unnest(sqlc.arg(quantities)::int[]) WITH ORDINALITY AS q(quantity, ord) ON q.ord = i.ord;

-- name: SetCheckCharge :exec
UPDATE checks SET charge_vnd = $2 WHERE id = $1;

-- name: MarkCheckMerged :exec
-- The absorbed Check keeps no charge and points at the survivor, which is
-- what the MERGED branch of check_settlement_evidence_valid requires.
UPDATE checks
SET state = 'MERGED', charge_vnd = 0, merged_into_check_id = $2
WHERE id = $1;

-- name: ListSessionOrders :many
SELECT id, order_draft_id, submitted_by_staff_identity_id,
       submitted_staff_access_session_id, submitted_at
FROM orders
WHERE service_session_id = $1
ORDER BY submitted_at ASC, id ASC;

-- name: ListOrderItems :many
SELECT id, order_id, committed_item_id
FROM order_items
WHERE order_id = ANY(sqlc.arg(order_ids)::uuid[])
ORDER BY order_id ASC, id ASC;

-- name: ListSessionPreparationUnits :many
-- One read feeding both the live Service Session and Completed Sale unit
-- projections. priority and remake_of_preparation_unit_id carry the Phase 6B
-- Remake metadata; original units are STANDARD with a null link.
SELECT pu.id, pu.order_item_id, pu.unit_number, pu.state, pu.service_number,
       pu.category_name, pu.item_name, pu.size_name, pu.modifiers,
       pu.preparation_note, pu.queued_at, pu.priority,
       pu.remake_of_preparation_unit_id
FROM preparation_units pu
JOIN order_items oi ON oi.id = pu.order_item_id
JOIN orders o ON o.id = oi.order_id
WHERE o.service_session_id = $1
ORDER BY pu.queued_at ASC, pu.id ASC;

-- name: ListSubmittedCommittedItems :many
-- The `submitted` flag on a Charge Allocation is derived, not stored: there is
-- no submitted column anywhere in the schema, and therefore no flag that can
-- fall out of step with the Order that defines it.
SELECT committed_item_id
FROM order_items
WHERE committed_item_id = ANY(sqlc.arg(committed_item_ids)::uuid[]);

-- name: LockServiceSessionForSubmission :one
-- The Submit source's Service Session, locked first. The state is returned
-- rather than filtered so an unknown Session and a closed one map to their own
-- errors instead of collapsing into ErrNothingToSubmit, per spec §9.3.
SELECT id, service_number, service_mode, state
FROM service_sessions
WHERE id = $1
FOR UPDATE;

-- name: LockSubmittableDraft :one
-- The committed-but-unsubmitted Order Draft of the Session the caller has
-- already locked with LockServiceSessionForSubmission. Only the draft is
-- locked here: the Session lock is its own query so a missing or closed
-- Session can be told apart from "nothing to submit".
--
-- NOT EXISTS rather than the canonical LEFT JOIN ... IS NULL: PostgreSQL
-- refuses row locks across a LEFT JOIN's nullable side, which forces the
-- canonical source to scope FOR UPDATE by hand and explain the workaround in
-- two places. Written this way the restriction does not arise.
--
-- Known remaining window, recorded rather than reordered (ADR-031): this
-- query runs after the Session lock and before LockChecksForSubmission, so
-- the AB-BA window spans Submit's Session → Draft → Checks lock sequence —
-- the reverse of Payment's Check-then-Session order — and a concurrent
-- Payment can abort one side with 40P01 across it.
SELECT od.id AS order_draft_id
FROM order_drafts od
WHERE od.service_session_id = $1
  AND od.state = 'COMMITTED'
  AND NOT EXISTS (SELECT 1 FROM orders o WHERE o.order_draft_id = od.id)
FOR UPDATE
LIMIT 1;

-- name: LockChecksForSubmission :many
-- Every distinct Check reachable from the draft's Committed Items, locked in
-- the ascending (created_at, id) order 5C's lock protocol established, so
-- Submit and a concurrent Payment serialize instead of deadlocking.
--
-- The draft linkage lives in an IN subquery rather than a DISTINCT over a
-- join: PostgreSQL refuses the locking clause alongside DISTINCT, the same
-- restriction the NOT EXISTS form of LockSubmittableDraft avoids. Selecting
-- from checks directly makes DISTINCT unnecessary — c.id is the primary key —
-- and keeps the lock scoped to the checks relation exactly as the join's
-- FOR UPDATE OF c intended.
SELECT c.id, c.state, c.created_at
FROM checks c
WHERE c.id IN (
    SELECT ca.check_id
    FROM committed_items ci
    JOIN charge_allocations ca ON ca.committed_item_id = ci.id
    WHERE ci.order_draft_id = $1
)
ORDER BY c.created_at ASC, c.id ASC
FOR UPDATE;

-- name: InsertOrder :one
INSERT INTO orders (service_session_id, order_draft_id,
                    submitted_by_staff_identity_id,
                    submitted_staff_access_session_id, submitted_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (order_draft_id) DO NOTHING
RETURNING id;

-- name: ListCommittedItemsForSubmission :many
SELECT id, category_name, item_name, size_name, quantity, preparation_note
FROM committed_items
WHERE order_draft_id = $1
ORDER BY committed_at ASC, id ASC;

-- name: InsertOrderItem :one
INSERT INTO order_items (order_id, committed_item_id)
VALUES ($1, $2)
RETURNING id;

-- name: InsertPreparationUnit :exec
INSERT INTO preparation_units (order_item_id, unit_number, service_number,
                               category_name, item_name, size_name,
                               modifiers, preparation_note, queued_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: LockServiceSessionForClosure :one
SELECT id, state, service_number, service_mode, created_at
FROM service_sessions
WHERE id = $1
FOR UPDATE;

-- name: FindCompletedSaleByServiceSession :one
SELECT id FROM completed_sales WHERE service_session_id = $1;

-- name: InsertCompletedSale :one
INSERT INTO completed_sales (service_session_id, completed_by_staff_identity_id,
                             completed_staff_access_session_id, completed_at)
VALUES ($1, $2, $3, $4)
RETURNING id;

-- name: ListHeldTableAssignments :many
SELECT id, table_id
FROM table_assignments
WHERE service_session_id = $1 AND released_at IS NULL
FOR UPDATE;

-- name: CloseServiceSession :exec
UPDATE service_sessions SET state = 'CLOSED' WHERE id = $1;

-- name: GetCompletedSale :one
SELECT cs.id, cs.service_session_id, cs.completed_by_staff_identity_id,
       cs.completed_staff_access_session_id, cs.completed_at,
       ss.service_number, ss.service_mode, ss.state AS service_session_state,
       ss.created_at AS service_session_created_at,
       si.display_name AS completed_by_display_name
FROM completed_sales cs
JOIN service_sessions ss ON ss.id = cs.service_session_id
JOIN staff_identities si ON si.id = cs.completed_by_staff_identity_id
WHERE cs.id = $1;

-- name: ListSessionPreparationTransitions :many
SELECT put.id, put.preparation_unit_id, put.prior_state, put.resulting_state,
       put.actor_staff_identity_id, put.staff_access_session_id, put.occurred_at
FROM preparation_unit_transitions put
JOIN preparation_units pu ON pu.id = put.preparation_unit_id
JOIN order_items oi ON oi.id = pu.order_item_id
JOIN orders o ON o.id = oi.order_id
WHERE o.service_session_id = $1
ORDER BY put.occurred_at ASC, put.id ASC;

-- Phase 6C: Comp, Refund, and Payment Void resolution, locks, and facts.
--
-- Lock order is the concurrency contract (design section 11.1): non-locking
-- resolution first, then Checks ascending, Service Sessions ascending, the
-- current or original Sales Shift, Payments and Charge Adjustments each
-- ascending, and finally Preparation source facts and Units ascending.

-- name: ResolveCompSource :one
-- Non-locking ownership resolution for Comp. Resolves the Waste, its source
-- unit, the owning Order Item and Service Session, the Completed Sale when the
-- Session is already closed, and the original Charge Allocation whose
-- cumulative quantity range (allocations ordered by created_at then id) covers
-- the unit_number. A Wasted Remake maps to a null allocation and price because
-- it was never charged.
WITH source AS (
    SELECT w.id AS waste_id, w.preparation_unit_id, w.prior_state,
           pu.state AS unit_state, pu.priority, pu.unit_number,
           oi.id AS order_item_id, oi.committed_item_id, o.service_session_id
    FROM preparation_wastes AS w
    JOIN preparation_units AS pu ON pu.id = w.preparation_unit_id
    JOIN order_items AS oi ON oi.id = pu.order_item_id
    JOIN orders AS o ON o.id = oi.order_id
    WHERE w.id = $1
),
ranges AS (
    SELECT ca.committed_item_id, ca.id AS charge_allocation_id, ca.check_id,
           ci.unit_price_vnd,
           COALESCE(SUM(ca.quantity) OVER (
               PARTITION BY ca.committed_item_id
               ORDER BY ca.created_at, ca.id
               ROWS BETWEEN UNBOUNDED PRECEDING AND 1 PRECEDING), 0)::BIGINT
               AS range_start,
           COALESCE(SUM(ca.quantity) OVER (
               PARTITION BY ca.committed_item_id
               ORDER BY ca.created_at, ca.id
               ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW), 0)::BIGINT
               AS range_end
    FROM charge_allocations AS ca
    JOIN committed_items AS ci ON ci.id = ca.committed_item_id
    WHERE ca.committed_item_id = (SELECT committed_item_id FROM source)
)
SELECT s.waste_id, s.preparation_unit_id, s.prior_state, s.unit_state,
       s.priority, s.unit_number, s.order_item_id, s.service_session_id,
       ss.state AS service_session_state,
       cs.id AS completed_sale_id,
       r.charge_allocation_id, r.check_id, r.unit_price_vnd AS amount_vnd
FROM source AS s
JOIN service_sessions AS ss ON ss.id = s.service_session_id
LEFT JOIN completed_sales AS cs ON cs.service_session_id = s.service_session_id
LEFT JOIN ranges AS r
       ON r.committed_item_id = s.committed_item_id
      AND s.priority = 'STANDARD'
      AND s.unit_number > r.range_start
      AND s.unit_number <= r.range_end;

-- name: LockWasteForComp :one
-- Step 5: the Waste and its source Preparation Unit, locked after the Check,
-- Session, and Shift resolved by ResolveCompSource. The unit lock serializes
-- concurrent Comps of one Waste alongside the unique Waste reference.
SELECT w.id, w.preparation_unit_id, w.prior_state, w.reason, w.note,
       w.actor_staff_identity_id, w.staff_access_session_id, w.occurred_at,
       pu.state AS unit_state, pu.priority, pu.unit_number, pu.order_item_id
FROM preparation_wastes AS w
JOIN preparation_units AS pu ON pu.id = w.preparation_unit_id
WHERE w.id = $1
FOR UPDATE OF w, pu;

-- name: LockPaymentForVoid :one
-- Locks one Payment for Void after its Check, Session, and Shift are locked.
-- Refund allocations, if any, are read separately under this lock to reject a
-- refunded Payment.
SELECT id, check_id, sales_shift_id, method, applied_amount_vnd, received_at
FROM payments
WHERE id = $1
FOR UPDATE;

-- name: ReopenCheckAfterPaymentVoid :exec
-- The settlement consequence of a whole Payment Void that leaves a positive
-- balance: the Check moves back from SETTLED to OPEN. All four
-- settlement-evidence columns are cleared in the same statement because
-- check_settlement_evidence_valid rejects OPEN with any evidence set. The
-- caller has already recomputed the positive balance under the Check lock.
UPDATE checks
SET state = 'OPEN',
    settled_at = NULL,
    settled_by_staff_identity_id = NULL,
    settled_during_sales_shift_id = NULL,
    settled_staff_access_session_id = NULL
WHERE id = $1 AND state = 'SETTLED';

-- name: LockPaymentsForRefund :many
-- Steps 4: the selected Payments FOR UPDATE, each ascending UUID, so two
-- overlapping Refunds cannot allocate the same capacity twice and cannot
-- deadlock against each other.
SELECT p.id, p.check_id, p.sales_shift_id, p.method, p.applied_amount_vnd
FROM payments AS p
WHERE p.id = ANY(sqlc.arg(payment_ids)::uuid[])
ORDER BY p.id
FOR UPDATE;

-- name: LockChargeAdjustmentsForRefund :many
-- Step 4: the selected Charge Adjustments FOR UPDATE, each ascending UUID,
-- after the Payments so both refundable capacities are held in one order.
SELECT ca.id, ca.scope, ca.check_id, ca.completed_sale_id, ca.amount_vnd
FROM charge_adjustments AS ca
WHERE ca.id = ANY(sqlc.arg(charge_adjustment_ids)::uuid[])
ORDER BY ca.id
FOR UPDATE;

-- name: InsertSalesComp :one
INSERT INTO sales_comps (
    preparation_waste_id, charge_adjustment_id, reason, note,
    actor_staff_identity_id, staff_access_session_id,
    approved_by_staff_identity_id, occurred_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING id, preparation_waste_id, charge_adjustment_id, reason, note,
          actor_staff_identity_id, staff_access_session_id,
          approved_by_staff_identity_id, occurred_at;

-- name: InsertPaymentVoid :one
INSERT INTO payment_voids (
    payment_id, sales_shift_id, amount_vnd, reason, note,
    actor_staff_identity_id, staff_access_session_id,
    approved_by_staff_identity_id, occurred_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING id, payment_id, sales_shift_id, amount_vnd, reason, note,
          actor_staff_identity_id, staff_access_session_id,
          approved_by_staff_identity_id, occurred_at;

-- name: InsertRefund :one
INSERT INTO refunds (
    check_id, completed_sale_id, sales_shift_id, method, amount_vnd,
    reason, note, actor_staff_identity_id, staff_access_session_id,
    approved_by_staff_identity_id, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING id, check_id, completed_sale_id, sales_shift_id, method, amount_vnd,
          reason, note, actor_staff_identity_id, staff_access_session_id,
          approved_by_staff_identity_id, created_at;

-- name: InsertRefundPaymentAllocations :exec
-- Writes one Refund's whole Payment allocation set in one round trip. The two
-- single-array unnests zip row-wise by ordinality, so row i is
-- (payment_ids[i], amounts[i]); the caller validates equal lengths and
-- positive amounts before calling, and the pair unique constraint rejects a
-- duplicated source.
INSERT INTO refund_payment_allocations (refund_id, payment_id, amount_vnd)
SELECT sqlc.arg(refund_id)::uuid, p.payment_id, a.amount_vnd
FROM unnest(sqlc.arg(payment_ids)::uuid[]) WITH ORDINALITY AS p(payment_id, ord)
JOIN unnest(sqlc.arg(amounts)::bigint[]) WITH ORDINALITY AS a(amount_vnd, ord)
  ON a.ord = p.ord;

-- name: InsertRefundAdjustmentAllocations :exec
-- The Charge Adjustment counterpart of InsertRefundPaymentAllocations.
INSERT INTO refund_adjustment_allocations (refund_id, charge_adjustment_id, amount_vnd)
SELECT sqlc.arg(refund_id)::uuid, ca.charge_adjustment_id, a.amount_vnd
FROM unnest(sqlc.arg(charge_adjustment_ids)::uuid[]) WITH ORDINALITY
     AS ca(charge_adjustment_id, ord)
JOIN unnest(sqlc.arg(amounts)::bigint[]) WITH ORDINALITY AS a(amount_vnd, ord)
  ON a.ord = ca.ord;

-- name: InsertRefundCompletion :one
INSERT INTO refund_completions (
    refund_id, transaction_reference, completed_by_staff_identity_id,
    staff_access_session_id, completed_at
) VALUES ($1, $2, $3, $4, $5)
RETURNING id, refund_id, transaction_reference,
          completed_by_staff_identity_id, staff_access_session_id, completed_at;

-- name: LockRefundForConfirmation :one
-- Locks one Refund for Manual QR confirmation, after its Check, Session, and
-- Shift are locked, and returns the completion evidence that decides replay.
SELECT r.id, r.check_id, r.completed_sale_id, r.sales_shift_id, r.method,
       r.amount_vnd, r.created_at,
       rc.id AS completion_id, rc.completed_at
FROM refunds AS r
LEFT JOIN refund_completions AS rc ON rc.refund_id = r.id
WHERE r.id = $1
FOR UPDATE OF r;

-- name: ListCheckChargeAdjustments :many
-- The live Check projection's append-only charge reductions, ordered by
-- occurrence then id. POST_SALE adjustments are excluded: they correct a
-- closed sale and are read through ListCompletedSalePostSaleCorrections.
SELECT id, kind, scope, preparation_unit_id, preparation_waste_id,
       charge_allocation_id, completed_sale_id, sales_shift_id, amount_vnd,
       created_at
FROM charge_adjustments
WHERE check_id = $1 AND scope = 'LIVE_CHECK'
ORDER BY created_at ASC, id ASC;

-- name: ListCheckRefunds :many
-- The live Check projection's Refunds, each with nullable completion evidence
-- so the caller derives PENDING or COMPLETED without a mutable state column.
-- Post-sale Refunds are excluded; they are read through
-- ListCompletedSalePostSaleCorrections.
SELECT r.id, r.check_id, r.completed_sale_id, r.sales_shift_id, r.method,
       r.amount_vnd, r.reason, r.note, r.actor_staff_identity_id,
       r.staff_access_session_id, r.approved_by_staff_identity_id,
       r.created_at,
       rc.id AS completion_id, rc.transaction_reference,
       rc.completed_by_staff_identity_id,
       rc.staff_access_session_id AS completed_staff_access_session_id,
       rc.completed_at
FROM refunds AS r
LEFT JOIN refund_completions AS rc ON rc.refund_id = r.id
WHERE r.check_id = $1 AND r.completed_sale_id IS NULL
ORDER BY r.created_at ASC, r.id ASC;

-- name: ListRefundPaymentAllocations :many
-- One Refund's Payment allocations, ordered by Payment id, for the Refund
-- result and the Completed Sale history.
SELECT refund_id, payment_id, amount_vnd
FROM refund_payment_allocations
WHERE refund_id = $1
ORDER BY payment_id ASC;

-- name: ListRefundAdjustmentAllocations :many
-- One Refund's Charge Adjustment allocations, ordered by Adjustment id.
SELECT refund_id, charge_adjustment_id, amount_vnd
FROM refund_adjustment_allocations
WHERE refund_id = $1
ORDER BY charge_adjustment_id ASC;

-- name: ListRefundPaymentAllocationsByRefundIDs :many
-- Every Payment allocation for the given Refunds, for a Check's batched
-- Refund projection: one round trip replaces one ListRefundPaymentAllocations
-- call per Refund.
SELECT refund_id, payment_id, amount_vnd
FROM refund_payment_allocations
WHERE refund_id = ANY(sqlc.arg(refund_ids)::uuid[])
ORDER BY refund_id ASC, payment_id ASC;

-- name: ListRefundAdjustmentAllocationsByRefundIDs :many
-- Every Charge Adjustment allocation for the given Refunds, for a Check's
-- batched Refund projection: one round trip replaces one
-- ListRefundAdjustmentAllocations call per Refund.
SELECT refund_id, charge_adjustment_id, amount_vnd
FROM refund_adjustment_allocations
WHERE refund_id = ANY(sqlc.arg(refund_ids)::uuid[])
ORDER BY refund_id ASC, charge_adjustment_id ASC;

-- name: ListPaymentRefundAllocations :many
-- Every Refund allocation against the given Payments, with the owning Refund's
-- scope and completion evidence, so the Check projection can derive each
-- Payment's remaining_refundable_vnd. Pending Manual QR intents appear too:
-- they reserve capacity before money moves. The caller applies the structural
-- snapshot rule (a post-sale allocation belongs to history, not the live
-- capacity) through refund_completed_sale_id.
SELECT rpa.refund_id, rpa.payment_id, rpa.amount_vnd,
       r.completed_sale_id AS refund_completed_sale_id,
       r.method AS refund_method,
       CASE WHEN rc.id IS NOT NULL THEN true ELSE false END AS refund_completed
FROM refund_payment_allocations AS rpa
JOIN refunds AS r ON r.id = rpa.refund_id
LEFT JOIN refund_completions AS rc ON rc.refund_id = r.id
WHERE rpa.payment_id = ANY(sqlc.arg(payment_ids)::uuid[])
ORDER BY rpa.payment_id ASC, rpa.refund_id ASC;

-- name: ListAdjustmentRefundAllocations :many
-- Every Refund allocation against the given Charge Adjustments, with the
-- owning Refund's scope and completion evidence, so the Check projection can
-- derive each Adjustment's remaining corrected capacity. Pending Manual QR
-- intents reserve capacity here too.
SELECT raa.refund_id, raa.charge_adjustment_id, raa.amount_vnd,
       r.completed_sale_id AS refund_completed_sale_id,
       r.method AS refund_method,
       CASE WHEN rc.id IS NOT NULL THEN true ELSE false END AS refund_completed
FROM refund_adjustment_allocations AS raa
JOIN refunds AS r ON r.id = raa.refund_id
LEFT JOIN refund_completions AS rc ON rc.refund_id = r.id
WHERE raa.charge_adjustment_id = ANY(sqlc.arg(charge_adjustment_ids)::uuid[])
ORDER BY raa.charge_adjustment_id ASC, raa.refund_id ASC;

-- name: ListCompletedSalePostSaleCorrections :many
-- Additive post-sale correction history for one Completed Sale: every
-- POST_SALE charge reduction (with its Comp fact) and every post-sale Refund,
-- ordered by occurrence then id. entry_kind discriminates the two row shapes;
-- each shape fills only its own columns. The leading WHERE false header exists
-- so the generated row type carries every column as nullable; a real row
-- always fills its own shape. Each row carries every column a full
-- PostSaleCorrectionResponse needs, so the shared loader never fabricates a
-- zero placeholder. The immutable sale snapshot itself is never rebuilt from
-- these rows.
SELECT NULL::text AS entry_kind,
       NULL::uuid AS charge_adjustment_id,
       NULL::text AS adjustment_kind,
       NULL::uuid AS preparation_unit_id,
       NULL::uuid AS preparation_waste_id,
       NULL::uuid AS sales_comp_id,
       NULL::uuid AS refund_id,
       NULL::uuid AS check_id,
       NULL::text AS scope,
       NULL::uuid AS charge_allocation_id,
       NULL::uuid AS sales_shift_id,
       NULL::uuid AS completed_sale_id,
       NULL::bigint AS amount_vnd,
       NULL::text AS reason,
       NULL::text AS note,
       NULL::uuid AS actor_staff_identity_id,
       NULL::uuid AS approved_by_staff_identity_id,
       NULL::text AS refund_method,
       NULL::uuid AS refund_completion_id,
       NULL::text AS transaction_reference,
       NULL::uuid AS completed_by_staff_identity_id,
       NULL::uuid AS completed_staff_access_session_id,
       NULL::timestamptz AS completed_at,
       NULL::timestamptz AS created_at,
       NULL::timestamptz AS occurred_at
WHERE false
UNION ALL
SELECT 'COMP' AS entry_kind,
       ca.id AS charge_adjustment_id,
       ca.kind AS adjustment_kind,
       ca.preparation_unit_id,
       ca.preparation_waste_id,
       sc.id AS sales_comp_id,
       NULL::uuid AS refund_id,
       ca.check_id,
       ca.scope,
       ca.charge_allocation_id,
       ca.sales_shift_id,
       ca.completed_sale_id,
       ca.amount_vnd,
       sc.reason,
       sc.note,
       sc.actor_staff_identity_id,
       sc.approved_by_staff_identity_id,
       NULL::text AS refund_method,
       NULL::uuid AS refund_completion_id,
       NULL::text AS transaction_reference,
       NULL::uuid AS completed_by_staff_identity_id,
       NULL::uuid AS completed_staff_access_session_id,
       NULL::timestamptz AS completed_at,
       ca.created_at,
       sc.occurred_at
FROM charge_adjustments AS ca
JOIN sales_comps AS sc ON sc.charge_adjustment_id = ca.id
WHERE ca.scope = 'POST_SALE'
  AND ca.completed_sale_id = sqlc.arg(completed_sale_id)::uuid
UNION ALL
SELECT 'REFUND' AS entry_kind,
       NULL::uuid AS charge_adjustment_id,
       NULL::text AS adjustment_kind,
       NULL::uuid AS preparation_unit_id,
       NULL::uuid AS preparation_waste_id,
       NULL::uuid AS sales_comp_id,
       r.id AS refund_id,
       r.check_id,
       NULL::text AS scope,
       NULL::uuid AS charge_allocation_id,
       r.sales_shift_id,
       r.completed_sale_id,
       r.amount_vnd,
       r.reason,
       r.note,
       r.actor_staff_identity_id,
       r.approved_by_staff_identity_id,
       r.method AS refund_method,
       rc.id AS refund_completion_id,
       rc.transaction_reference,
       rc.completed_by_staff_identity_id,
       rc.staff_access_session_id AS completed_staff_access_session_id,
       rc.completed_at,
       r.created_at,
       r.created_at AS occurred_at
FROM refunds AS r
LEFT JOIN refund_completions AS rc ON rc.refund_id = r.id
WHERE r.completed_sale_id = sqlc.arg(completed_sale_id)::uuid
ORDER BY occurred_at ASC,
         charge_adjustment_id ASC NULLS LAST,
         refund_id ASC NULLS LAST;

-- name: CountLiveChargeAdjustmentsForChecks :one
-- Split and Merge rejection evidence (design section 6.2): the number of the
-- given Checks that carry at least one LIVE_CHECK Charge Adjustment. The
-- caller rejects with CHECK_HAS_CHARGE_ADJUSTMENT when this is non-zero.
SELECT count(*)::BIGINT AS live_adjustment_count
FROM charge_adjustments
WHERE check_id = ANY(sqlc.arg(check_ids)::uuid[])
  AND scope = 'LIVE_CHECK';
