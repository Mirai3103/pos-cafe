-- Phase 6A: server-authoritative Preparation Queue aging.
ALTER TABLE preparation_units
    ADD COLUMN IF NOT EXISTS in_preparation_at TIMESTAMPTZ;

WITH first_start AS (
    SELECT DISTINCT ON (preparation_unit_id)
           preparation_unit_id,
           occurred_at
    FROM preparation_unit_transitions
    WHERE resulting_state = 'IN_PREPARATION'
    ORDER BY preparation_unit_id, occurred_at, id
)
UPDATE preparation_units AS pu
SET in_preparation_at = first_start.occurred_at
FROM first_start
WHERE pu.id = first_start.preparation_unit_id
  AND pu.in_preparation_at IS NULL;
