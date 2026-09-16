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
-- Item, including Remakes and terminal units.
WITH unit_counts AS (
    SELECT order_item_id, count(*)::integer AS unit_count
    FROM preparation_units
    GROUP BY order_item_id
)
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
       uc.unit_count AS order_item_unit_count
FROM preparation_units AS pu
JOIN order_items AS oi ON oi.id = pu.order_item_id
JOIN orders AS o ON o.id = oi.order_id
JOIN unit_counts AS uc ON uc.order_item_id = pu.order_item_id
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
