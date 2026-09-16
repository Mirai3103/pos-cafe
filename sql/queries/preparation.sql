-- Preparation slice queries.
--
-- Boundary (ADR-024): nothing here writes orders, order_items, or
-- completed_sales. internal/sales creates Preparation Units at Submit and
-- reads their state during closure; this package owns every transition.

-- name: LockPreparationUnit :one
SELECT id, order_item_id, unit_number, state, service_number, category_name,
       item_name, size_name, modifiers, preparation_note, queued_at,
       in_preparation_at
FROM preparation_units
WHERE id = $1
FOR UPDATE;

-- name: GetPreparationUnit :one
SELECT id, order_item_id, unit_number, state, service_number, category_name,
       item_name, size_name, modifiers, preparation_note, queued_at,
       in_preparation_at
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
       o.service_session_id,
       uc.unit_count AS order_item_unit_count
FROM preparation_units AS pu
JOIN order_items AS oi ON oi.id = pu.order_item_id
JOIN orders AS o ON o.id = oi.order_id
JOIN unit_counts AS uc ON uc.order_item_id = pu.order_item_id
WHERE pu.state IN ('QUEUED', 'IN_PREPARATION', 'READY')
ORDER BY pu.queued_at, pu.id;

-- name: ListCurrentPreparationTables :many
SELECT ta.service_session_id, t.name
FROM table_assignments AS ta
JOIN tables AS t ON t.id = ta.table_id
WHERE ta.service_session_id = ANY(sqlc.arg(service_session_ids)::uuid[])
  AND ta.released_at IS NULL
ORDER BY ta.service_session_id, ta.sequence, ta.id;
