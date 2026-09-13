-- Phase 5A: Service Session & Order Draft.
--
-- internal/sales takes business ownership of service_sessions and
-- table_assignments, which migration 000006 provisioned early so the Tables
-- overview read could ship complete in Phase 3 (ADR-006). This migration
-- completes service_sessions, corrects a state domain Phase 3 guessed, and
-- creates the Order Draft tables.

-- service_sessions gains a NOT NULL column with no default, which is only
-- safe on an empty table. Phase 3 never writes the table through its API, so
-- in any real deployment it is empty. Fail loudly rather than backfilling a
-- fabricated Sales Shift.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM service_sessions) THEN
        RAISE EXCEPTION
            'service_sessions must be empty before Phase 5A adds sales_shift_id NOT NULL';
    END IF;
END $$;

ALTER TABLE service_sessions
    ADD COLUMN IF NOT EXISTS sales_shift_id UUID NOT NULL
        REFERENCES sales_shifts(id) ON DELETE RESTRICT,
    ADD COLUMN IF NOT EXISTS sequence INTEGER NOT NULL;

-- The canonical SERVICE_SESSION_STATES is ['ACTIVE', 'CLOSED']. Phase 3
-- guessed ('ACTIVE', 'COMPLETED', 'CANCELLED') for a domain it never wrote:
-- COMPLETED belongs to the separate Completed Sale entity, and no canonical
-- path cancels a Service Session.
ALTER TABLE service_sessions DROP CONSTRAINT IF EXISTS service_session_state_valid;
ALTER TABLE service_sessions ADD CONSTRAINT service_session_state_valid
    CHECK (state IN ('ACTIVE', 'CLOSED'));

ALTER TABLE service_sessions ADD CONSTRAINT service_session_sequence_valid
    CHECK (sequence BETWEEN 1 AND 99999);

-- A Service Number is an operational label, unique among the Sessions of one
-- Sales Shift rather than for all time. See ADR-011.
DROP INDEX IF EXISTS service_session_service_number_unique;

CREATE UNIQUE INDEX IF NOT EXISTS service_session_number_per_shift_unique
    ON service_sessions (sales_shift_id, service_number);

CREATE UNIQUE INDEX IF NOT EXISTS service_session_sequence_per_shift_unique
    ON service_sessions (sales_shift_id, sequence);

CREATE INDEX IF NOT EXISTS service_session_active_index
    ON service_sessions (state, created_at ASC, id ASC);

COMMENT ON TABLE service_sessions IS
    'Owned by internal/sales (Phase 5A).';
COMMENT ON TABLE table_assignments IS
    'Owned by internal/sales (Phase 5A). Read by internal/tables for the overview.';

-- One editable Order Draft per Service Session.
CREATE TABLE IF NOT EXISTS order_drafts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    service_session_id UUID NOT NULL REFERENCES service_sessions(id) ON DELETE RESTRICT,
    state TEXT NOT NULL DEFAULT 'EDITABLE',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT order_draft_state_valid
        CHECK (state IN ('EDITABLE', 'COMMITTED'))
);

-- 5A never writes COMMITTED. The state exists so the draft lock query's
-- state = 'EDITABLE' filter is meaningful rather than vacuous, and so 5B adds
-- Commit without a state-domain migration.
CREATE UNIQUE INDEX IF NOT EXISTS order_draft_editable_per_session_unique
    ON order_drafts (service_session_id)
    WHERE state = 'EDITABLE';

CREATE INDEX IF NOT EXISTS order_draft_service_session_index
    ON order_drafts (service_session_id);

COMMENT ON TABLE order_drafts IS
    'Owned by internal/sales (Phase 5A). Commit, which writes COMMITTED, lands in 5B.';

CREATE TABLE IF NOT EXISTS order_draft_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_draft_id UUID NOT NULL REFERENCES order_drafts(id) ON DELETE CASCADE,
    menu_item_id UUID NOT NULL REFERENCES menu_items(id) ON DELETE RESTRICT,
    size_id UUID REFERENCES menu_item_sizes(id) ON DELETE RESTRICT,
    quantity INTEGER NOT NULL DEFAULT 1,
    preparation_note TEXT,
    modifier_key TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- A plain unique index treats two NULLs as distinct, which would let the
    -- same composition exist twice whenever Size or note is absent. These
    -- generated columns make the composition key total.
    size_key TEXT GENERATED ALWAYS AS (coalesce(size_id::text, '')) STORED,
    note_key TEXT GENERATED ALWAYS AS (coalesce(preparation_note, '')) STORED,
    CONSTRAINT order_draft_item_quantity_valid
        CHECK (quantity BETWEEN 1 AND 9999),
    CONSTRAINT order_draft_item_note_valid
        CHECK (
            preparation_note IS NULL
            OR (char_length(preparation_note) BETWEEN 1 AND 200
                AND preparation_note = btrim(preparation_note))
        )
);

CREATE UNIQUE INDEX IF NOT EXISTS order_draft_item_composition_unique
    ON order_draft_items (order_draft_id, menu_item_id, size_key, note_key, modifier_key);

CREATE INDEX IF NOT EXISTS order_draft_item_draft_index
    ON order_draft_items (order_draft_id, created_at ASC, id ASC);

COMMENT ON COLUMN order_draft_items.modifier_key IS
    'Derived: sorted, comma-joined modifier_option_id list. Exists only to make the composition index possible; order_draft_item_modifier_options is authoritative.';

CREATE TABLE IF NOT EXISTS order_draft_item_modifier_options (
    order_draft_item_id UUID NOT NULL
        REFERENCES order_draft_items(id) ON DELETE CASCADE,
    modifier_option_id UUID NOT NULL
        REFERENCES modifier_options(id) ON DELETE RESTRICT,
    PRIMARY KEY (order_draft_item_id, modifier_option_id)
);
