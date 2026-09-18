-- Preparation slice queries.
--
-- Boundary (ADR-024): nothing here writes orders, order_items, or
-- completed_sales. internal/sales creates Preparation Units at Submit and
-- reads their state during closure; this package owns every transition.

-- name: LockPreparationUnit :one
SELECT id, order_item_id, unit_number, state, service_number, category_name,
       item_name, size_name, modifiers, preparation_note, queued_at,
       in_preparation_at, priority, remake_of_preparation_unit_id
FROM preparation_units
WHERE id = $1
FOR UPDATE;

-- name: GetPreparationUnit :one
SELECT id, order_item_id, unit_number, state, service_number, category_name,
       item_name, size_name, modifiers, preparation_note, queued_at,
       in_preparation_at, priority, remake_of_preparation_unit_id
FROM preparation_units
WHERE id = $1;

-- name: ListPreparationUnitsByIDs :many
-- Batched projection read for a set of ids already known to exist (e.g. a
-- cancellation batch), avoiding one GetPreparationUnit round trip per unit.
SELECT id, order_item_id, unit_number, state, service_number, category_name,
       item_name, size_name, modifiers, preparation_note, queued_at,
       in_preparation_at, priority, remake_of_preparation_unit_id
FROM preparation_units
WHERE id = ANY(sqlc.arg(preparation_unit_ids)::uuid[]);

-- name: SetPreparationUnitState :exec
UPDATE preparation_units
SET state = sqlc.arg(state),
    in_preparation_at = CASE
        WHEN sqlc.arg(state)::text = 'IN_PREPARATION'
            THEN sqlc.arg(occurred_at)::timestamptz
        ELSE in_preparation_at
    END
WHERE id = sqlc.arg(id);

-- name: InsertPreparationUnitTransition :exec
INSERT INTO preparation_unit_transitions (preparation_unit_id, prior_state,
                                          resulting_state,
                                          actor_staff_identity_id,
                                          staff_access_session_id, occurred_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: GetPreparationCurrentTime :one
SELECT clock_timestamp()::timestamptz AS current_time;

-- name: ListActivePreparationUnits :many
-- The bar's work list: every active unit, plus a CANCELLED or WASTED unit
-- while it still has an unacknowledged alert (EXISTS, so several alerts on
-- one unit cannot duplicate the row). Active Remakes come first, active
-- STANDARD units second, alert-retained terminal units last; each lane is
-- FIFO by queued_at then id. unit_count is every physical unit of the Order
-- Item, including Remakes and terminal units, computed per row via a
-- correlated subquery (order_item_id is the leading column of
-- preparation_unit_item_number_unique) rather than a full-table GROUP BY, so
-- the cost tracks the small active-queue result set on this polled read.
SELECT pu.id,
       pu.order_item_id,
       pu.unit_number,
       pu.state,
       pu.service_number,
       pu.category_name,
       pu.item_name,
       pu.size_name,
       pu.modifiers,
       pu.preparation_note,
       pu.queued_at,
       pu.in_preparation_at,
       pu.priority,
       pu.remake_of_preparation_unit_id,
       o.service_session_id,
       (SELECT count(*)::integer
        FROM preparation_units AS pu2
        WHERE pu2.order_item_id = pu.order_item_id) AS order_item_unit_count
FROM preparation_units AS pu
JOIN order_items AS oi ON oi.id = pu.order_item_id
JOIN orders AS o ON o.id = oi.order_id
WHERE pu.state IN ('QUEUED', 'IN_PREPARATION', 'READY')
   OR (
       pu.state IN ('CANCELLED', 'WASTED')
       AND EXISTS (
           SELECT 1
           FROM preparation_alerts AS pa
           WHERE pa.preparation_unit_id = pu.id
             AND pa.acknowledged_at IS NULL
       )
   )
ORDER BY
    CASE
        WHEN pu.state IN ('QUEUED', 'IN_PREPARATION', 'READY')
             AND pu.priority = 'REMAKE' THEN 1
        WHEN pu.state IN ('QUEUED', 'IN_PREPARATION', 'READY') THEN 2
        ELSE 3
    END,
    pu.queued_at, pu.id;

-- name: ListCurrentPreparationTables :many
SELECT ta.service_session_id, t.name
FROM table_assignments AS ta
JOIN tables AS t ON t.id = ta.table_id
WHERE ta.service_session_id = ANY(sqlc.arg(service_session_ids)::uuid[])
  AND ta.released_at IS NULL
ORDER BY ta.service_session_id, ta.sequence, ta.id;

-- Phase 6B: correction locks, facts, and queue recovery reads.
--
-- Lock order is the concurrency contract (design section 10): Remake and
-- State Correction lock owning Service Sessions before their work rows, so
-- they serialize with Service Session closure. Waste uses the unit lock.

-- name: LockPreparationServiceSessions :many
-- Locks the owning Service Sessions before correction work. Callers pass
-- unique ids; ORDER BY id makes the multi-Session lock order deterministic.
SELECT id, service_number, state
FROM service_sessions
WHERE id = ANY(sqlc.arg(service_session_ids)::uuid[])
ORDER BY id ASC
FOR UPDATE;

-- name: LockPreparationOrderItem :one
-- Locks the Order Item so Remake's max(unit_number) + 1 allocation
-- serializes across concurrent Wastes of the same item, and returns the
-- owning Session the caller locked first.
SELECT oi.id, oi.order_id, o.service_session_id
FROM order_items AS oi
JOIN orders AS o ON o.id = oi.order_id
WHERE oi.id = $1
FOR UPDATE OF oi;

-- name: LockPreparationWaste :one
-- Locks the Waste and its source Preparation Unit, and resolves the owning
-- Order Item and Service Session ids the caller has already locked.
SELECT w.id, w.preparation_unit_id, w.prior_state, w.reason, w.note,
       w.actor_staff_identity_id, w.staff_access_session_id, w.occurred_at,
       pu.order_item_id, o.service_session_id
FROM preparation_wastes AS w
JOIN preparation_units AS pu ON pu.id = w.preparation_unit_id
JOIN order_items AS oi ON oi.id = pu.order_item_id
JOIN orders AS o ON o.id = oi.order_id
WHERE w.id = $1
FOR UPDATE OF w, pu;

-- name: LockPreparationAlert :one
-- Locks one alert for acknowledgment, returning its creation and
-- acknowledgment evidence.
SELECT id, preparation_unit_id, kind, reason, note,
       created_by_staff_identity_id, created_staff_access_session_id,
       created_at, acknowledged_by_staff_identity_id,
       acknowledged_staff_access_session_id, acknowledged_at
FROM preparation_alerts
WHERE id = $1
FOR UPDATE;

-- name: ListPreparationUnitsForCorrection :many
-- Resolves the selected units and their owning Sessions without locks, so a
-- correction can reject missing ids before taking any.
SELECT pu.id, pu.state, pu.order_item_id, o.service_session_id
FROM preparation_units AS pu
JOIN order_items AS oi ON oi.id = pu.order_item_id
JOIN orders AS o ON o.id = oi.order_id
WHERE pu.id = ANY(sqlc.arg(preparation_unit_ids)::uuid[]);

-- name: LockPreparationUnitsForCorrection :many
-- Locks the correction batch in unit-id order after its Sessions are locked.
SELECT id, state
FROM preparation_units
WHERE id = ANY(sqlc.arg(preparation_unit_ids)::uuid[])
ORDER BY id ASC
FOR UPDATE;

-- name: SetPreparationUnitCorrectedState :exec
-- Applies one reverse correction. Only a correction back to QUEUED clears
-- in_preparation_at; the later targets keep it because the unit really did
-- enter preparation at the earlier recorded instant.
UPDATE preparation_units
SET state = sqlc.arg(resulting_state),
    in_preparation_at = CASE
        WHEN sqlc.arg(resulting_state)::text = 'QUEUED'
            THEN NULL
        ELSE in_preparation_at
    END
WHERE id = sqlc.arg(id);

-- name: InsertPreparationWaste :one
INSERT INTO preparation_wastes (preparation_unit_id, prior_state, reason, note,
                                actor_staff_identity_id,
                                staff_access_session_id, occurred_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, preparation_unit_id, prior_state, reason, note,
          actor_staff_identity_id, staff_access_session_id, occurred_at;

-- name: InsertPreparationAlert :one
INSERT INTO preparation_alerts (preparation_unit_id, kind, reason, note,
                                created_by_staff_identity_id,
                                created_staff_access_session_id, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, preparation_unit_id, kind, reason, note,
          created_by_staff_identity_id, created_staff_access_session_id,
          created_at, acknowledged_by_staff_identity_id,
          acknowledged_staff_access_session_id, acknowledged_at;

-- name: AcknowledgePreparationAlert :one
-- Fills the acknowledgment tuple together; the all-or-nothing check
-- constraint rejects any partial write.
UPDATE preparation_alerts
SET acknowledged_by_staff_identity_id = sqlc.arg(acknowledged_by_staff_identity_id),
    acknowledged_staff_access_session_id = sqlc.arg(acknowledged_staff_access_session_id),
    acknowledged_at = sqlc.arg(acknowledged_at)
WHERE id = sqlc.arg(id)
RETURNING id, acknowledged_by_staff_identity_id,
          acknowledged_staff_access_session_id, acknowledged_at;

-- name: InsertPreparationRemakeUnit :one
-- Creates the linked replacement unit: the source unit's immutable
-- preparation snapshot under the next unit number, QUEUED at REMAKE
-- priority, and linked back to its source. Adds no Order Item or charge.
INSERT INTO preparation_units (order_item_id, unit_number, state, service_number,
                               category_name, item_name, size_name, modifiers,
                               preparation_note, queued_at, priority,
                               remake_of_preparation_unit_id)
VALUES ($1, $2, 'QUEUED', $3, $4, $5, $6, $7, $8, $9, 'REMAKE', $10)
RETURNING id, order_item_id, unit_number, state, service_number, category_name,
          item_name, size_name, modifiers, preparation_note, queued_at,
          in_preparation_at, priority, remake_of_preparation_unit_id;

-- name: InsertPreparationRemake :one
INSERT INTO preparation_remakes (waste_id, preparation_unit_id, reason, note,
                                 actor_staff_identity_id,
                                 staff_access_session_id, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, waste_id, preparation_unit_id, reason, note,
          actor_staff_identity_id, staff_access_session_id, created_at;

-- name: GetNextPreparationUnitNumber :one
-- Runs under the owning Order Item lock, so max + 1 cannot collide across
-- concurrent Remakes of the same item.
SELECT (coalesce(max(unit_number), 0) + 1)::integer AS next_unit_number
FROM preparation_units
WHERE order_item_id = $1;

-- name: InsertPreparationStateCorrection :one
INSERT INTO preparation_state_corrections (preparation_unit_id, prior_state,
                                           resulting_state, reason, note,
                                           actor_staff_identity_id,
                                           staff_access_session_id,
                                           occurred_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING id, preparation_unit_id, prior_state, resulting_state, reason, note,
          actor_staff_identity_id, staff_access_session_id, occurred_at;

-- name: InsertPreparationAuditEventsBatch :exec
-- Batches audit events of differing types into one round trip. event_types
-- and details_batch must be equal length; the parallel unnests zip row-wise
-- and pad the shorter array with nulls, so any caller drift fails the target
-- columns' NOT NULL constraints instead of silently truncating one array.
-- details_batch is text[] cast to jsonb per element, for the same pq.Array
-- reason the Catalog batch query records. Callers validate equal non-zero
-- lengths in Go before calling.
INSERT INTO audit_events (event_type, actor_id, session_id, details, occurred_at)
SELECT batch.event_type_value, sqlc.arg(actor_id)::uuid,
       sqlc.arg(session_id)::uuid, batch.details_value::jsonb,
       sqlc.arg(occurred_at)::timestamptz
FROM (
    SELECT unnest(sqlc.arg(event_types)::text[]) AS event_type_value,
           unnest(sqlc.arg(details_batch)::text[]) AS details_value
) AS batch;

-- name: ListActivePreparationAlerts :many
-- The queue's active alerts, oldest first. The Waste join matches only
-- WASTE alerts, so waste_id resolves through the Waste fact for WASTE and
-- stays null for the reserved Cancellation kinds even if their unit carries
-- a Waste.
SELECT pa.id, pa.preparation_unit_id, pa.kind, pa.reason, pa.note,
       pa.created_by_staff_identity_id, pa.created_staff_access_session_id,
       pa.created_at, pa.acknowledged_by_staff_identity_id,
       pa.acknowledged_staff_access_session_id, pa.acknowledged_at,
       pu.service_number, pu.item_name, pu.unit_number,
       w.id AS waste_id
FROM preparation_alerts AS pa
JOIN preparation_units AS pu ON pu.id = pa.preparation_unit_id
LEFT JOIN preparation_wastes AS w
       ON w.preparation_unit_id = pu.id AND pa.kind = 'WASTE'
WHERE pa.acknowledged_at IS NULL
ORDER BY pa.created_at ASC, pa.id ASC;

-- name: ListRecentPreparationCorrections :many
-- The queue's Waste and Remake history for active Sessions: one set-based
-- UNION ALL, newest first, capped at 50. entry_kind discriminates the two
-- row shapes; the nullable columns carry what each shape needs.
SELECT 'WASTE' AS entry_kind,
       w.id AS fact_id,
       NULL::uuid AS waste_id,
       w.preparation_unit_id,
       NULL::uuid AS source_preparation_unit_id,
       NULL::integer AS source_unit_number,
       pu.unit_number,
       pu.service_number,
       pu.item_name,
       w.reason,
       w.note,
       w.occurred_at
FROM preparation_wastes AS w
JOIN preparation_units AS pu ON pu.id = w.preparation_unit_id
JOIN order_items AS oi ON oi.id = pu.order_item_id
JOIN orders AS o ON o.id = oi.order_id
JOIN service_sessions AS ss ON ss.id = o.service_session_id
WHERE ss.state = 'ACTIVE'
UNION ALL
SELECT 'REMAKE' AS entry_kind,
       r.id AS fact_id,
       r.waste_id,
       r.preparation_unit_id,
       w.preparation_unit_id AS source_preparation_unit_id,
       wpu.unit_number AS source_unit_number,
       pu.unit_number,
       pu.service_number,
       pu.item_name,
       r.reason,
       r.note,
       r.created_at AS occurred_at
FROM preparation_remakes AS r
JOIN preparation_wastes AS w ON w.id = r.waste_id
JOIN preparation_units AS pu ON pu.id = r.preparation_unit_id
JOIN preparation_units AS wpu ON wpu.id = w.preparation_unit_id
JOIN order_items AS oi ON oi.id = pu.order_item_id
JOIN orders AS o ON o.id = oi.order_id
JOIN service_sessions AS ss ON ss.id = o.service_session_id
WHERE ss.state = 'ACTIVE'
ORDER BY occurred_at DESC, fact_id DESC
LIMIT 50;

-- Phase 6C: Cancellation locks, resolution, and facts.
--
-- Lock order is the concurrency contract (design section 11.1): resolve
-- ownership without locks, then lock Checks ascending by id, Service Sessions
-- ascending by id, the current open Sales Shift, and finally the selected
-- Preparation Units ascending by id. Restructuring locks Checks; closure locks
-- Sessions; Waste and State Correction lock Sessions before units but never
-- wait on a Check, so no lock cycle exists.

-- name: ResolveCancellationUnits :many
-- Non-locking resolution of a Cancellation selection. Each STANDARD unit maps
-- to the immutable per-unit price of its Committed Item and to the Charge
-- Allocation whose cumulative quantity range (allocations ordered by
-- created_at then id) covers its unit_number. A REMAKE unit, or a unit beyond
-- every allocation range, carries a null allocation, Check, and price because
-- it was never charged.
WITH selected AS (
    SELECT pu.id, pu.state, pu.priority, pu.unit_number, pu.order_item_id,
           oi.committed_item_id, o.service_session_id
    FROM preparation_units AS pu
    JOIN order_items AS oi ON oi.id = pu.order_item_id
    JOIN orders AS o ON o.id = oi.order_id
    WHERE pu.id = ANY(sqlc.arg(preparation_unit_ids)::uuid[])
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
    WHERE ca.committed_item_id IN (SELECT committed_item_id FROM selected)
)
SELECT s.id, s.state, s.priority, s.unit_number, s.order_item_id,
       s.committed_item_id, s.service_session_id,
       r.charge_allocation_id, r.check_id, r.unit_price_vnd
FROM selected AS s
LEFT JOIN ranges AS r
       ON r.committed_item_id = s.committed_item_id
      AND s.priority = 'STANDARD'
      AND s.unit_number > r.range_start
      AND s.unit_number <= r.range_end
ORDER BY s.id ASC;

-- name: LockPreparationChecksForCancellation :many
-- Step 1 of the common correction lock order: every affected Check FOR UPDATE,
-- ordered by id so concurrent corrections take the rows in the same order.
SELECT id, state, charge_vnd, service_session_id
FROM checks
WHERE id = ANY(sqlc.arg(check_ids)::uuid[])
ORDER BY id ASC
FOR UPDATE;

-- name: LockPreparationSessionsForCancellation :many
-- Step 2: the owning Service Sessions, after their Checks and before the
-- current Shift and the work rows.
SELECT id, service_number, state
FROM service_sessions
WHERE id = ANY(sqlc.arg(service_session_ids)::uuid[])
ORDER BY id ASC
FOR UPDATE;

-- name: LockOpenSalesShiftForCancellation :one
-- Step 3: the one open Sales Shift. FOR SHARE, because Cancellation only reads
-- the Shift for settlement evidence and never writes Shift state; Shift
-- closure takes FOR UPDATE and stays excluded for the whole transaction. No
-- row means no Shift is open.
SELECT id, state
FROM sales_shifts
WHERE state = 'OPEN'
LIMIT 1
FOR SHARE;

-- name: LockPreparationUnitsForCancellation :many
-- Step 5: the selected Preparation Units, locked last in id order after their
-- Checks and Sessions. The caller revalidates QUEUED and re-resolves the
-- unit-to-allocation mapping against the committed rows.
SELECT id, state, priority, unit_number, order_item_id
FROM preparation_units
WHERE id = ANY(sqlc.arg(preparation_unit_ids)::uuid[])
ORDER BY id ASC
FOR UPDATE;

-- name: GetCancellationReplacementOrder :one
-- Non-locking resolution for CHANGE. Returns no row when the replacement Order
-- does not exist at all; the three boolean columns let the handler reject a
-- cross-Session, source, or not-later Order with one typed error.
SELECT o.id, o.service_session_id, o.submitted_at,
       (o.service_session_id = sqlc.arg(service_session_id)::uuid) AS same_session,
       NOT EXISTS (
           SELECT 1
           FROM unnest(sqlc.arg(source_order_ids)::uuid[]) AS source_order(id)
           WHERE source_order.id = o.id
       ) AS differs_from_source_orders,
       o.submitted_at > (
           SELECT COALESCE(MAX(src.submitted_at), '-infinity'::timestamptz)
           FROM orders AS src
           WHERE src.id = ANY(sqlc.arg(source_order_ids)::uuid[])
       ) AS submitted_after_source_orders
FROM orders AS o
WHERE o.id = sqlc.arg(replacement_order_id);

-- name: GetPreparationCheckFinancials :one
-- Financial evidence for a Cancellation's affected Check, read while the
-- caller holds the Check lock. base_charge_vnd is the live sum of original
-- Charge Allocations; live_adjustment_vnd is every committed LIVE_CHECK
-- adjustment; valid_payment_vnd excludes voided Payments; completed_refund_vnd
-- counts completed live Refunds. The handler verifies stored charge = base -
-- live adjustments, then recomputes settlement and pending Refund from these
-- terms.
SELECT c.id, c.state, c.charge_vnd, c.service_session_id,
       COALESCE((SELECT SUM(ca.quantity::BIGINT * ci.unit_price_vnd)
                 FROM charge_allocations AS ca
                 JOIN committed_items AS ci ON ci.id = ca.committed_item_id
                 WHERE ca.check_id = c.id), 0)::BIGINT AS base_charge_vnd,
       COALESCE((SELECT SUM(ca.amount_vnd)
                 FROM charge_adjustments AS ca
                 WHERE ca.check_id = c.id
                   AND ca.scope = 'LIVE_CHECK'), 0)::BIGINT AS live_adjustment_vnd,
       COALESCE((SELECT SUM(p.applied_amount_vnd)
                 FROM payments AS p
                 WHERE p.check_id = c.id
                   AND NOT EXISTS (SELECT 1
                                   FROM payment_voids AS pv
                                   WHERE pv.payment_id = p.id)), 0)::BIGINT
           AS valid_payment_vnd,
       COALESCE((SELECT SUM(r.amount_vnd)
                 FROM refunds AS r
                 JOIN refund_completions AS rc ON rc.refund_id = r.id
                 WHERE r.check_id = c.id
                   AND r.completed_sale_id IS NULL), 0)::BIGINT
           AS completed_refund_vnd
FROM checks AS c
WHERE c.id = $1;

-- name: InsertChargeAdjustment :one
INSERT INTO charge_adjustments (
    kind, scope, preparation_unit_id, preparation_waste_id,
    charge_allocation_id, check_id, completed_sale_id, sales_shift_id,
    amount_vnd, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING id, kind, scope, preparation_unit_id, preparation_waste_id,
          charge_allocation_id, check_id, completed_sale_id, sales_shift_id,
          amount_vnd, created_at;

-- name: UpdateAdjustedCheckCharge :exec
-- Writes the denormalized live charge after one Cancellation batch plans all
-- of its adjustments. The invariant is stored charge = base charge - live
-- adjustments; POST_SALE adjustments are excluded.
UPDATE checks
SET charge_vnd = sqlc.arg(charge_vnd)
WHERE id = sqlc.arg(id);

-- name: InsertPreparationCancellation :one
INSERT INTO preparation_cancellations (
    preparation_unit_id, kind, charge_adjustment_id, replacement_order_id,
    reason, note, actor_staff_identity_id, staff_access_session_id, occurred_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING id, preparation_unit_id, kind, charge_adjustment_id,
          replacement_order_id, reason, note, actor_staff_identity_id,
          staff_access_session_id, occurred_at;

-- name: SettleAdjustedCheck :exec
-- The settlement consequence of a Cancellation or Comp that reduces a live
-- Check to zero balance. All four evidence columns are written together
-- because check_settlement_evidence_valid rejects any partial set; the
-- initiator and the current open Shift supply the evidence.
UPDATE checks
SET state = 'SETTLED',
    settled_at = sqlc.arg(settled_at),
    settled_by_staff_identity_id = sqlc.arg(settled_by_staff_identity_id),
    settled_during_sales_shift_id = sqlc.arg(settled_during_sales_shift_id),
    settled_staff_access_session_id = sqlc.arg(settled_staff_access_session_id)
WHERE id = sqlc.arg(id);
