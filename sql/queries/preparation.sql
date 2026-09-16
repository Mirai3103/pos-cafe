-- Preparation slice queries.
--
-- Boundary (ADR-024): nothing here writes orders, order_items, or
-- completed_sales. internal/sales creates Preparation Units at Submit and
-- reads their state during closure; this package owns every transition.

-- name: LockPreparationUnit :one
SELECT id, order_item_id, unit_number, state, service_number, category_name,
       item_name, size_name, modifiers, preparation_note, queued_at
FROM preparation_units
WHERE id = $1
FOR UPDATE;

-- name: GetPreparationUnit :one
SELECT id, order_item_id, unit_number, state, service_number, category_name,
       item_name, size_name, modifiers, preparation_note, queued_at
FROM preparation_units
WHERE id = $1;

-- name: SetPreparationUnitState :exec
UPDATE preparation_units SET state = $2 WHERE id = $1;

-- name: InsertPreparationUnitTransition :exec
INSERT INTO preparation_unit_transitions (preparation_unit_id, prior_state,
                                          resulting_state,
                                          actor_staff_identity_id,
                                          staff_access_session_id, occurred_at)
VALUES ($1, $2, $3, $4, $5, $6);
