-- Phase 5B: Commit, Committed Items, Checks & Charge Allocations.
--
-- Commit is the commercial boundary: it revalidates an Order Draft, freezes
-- prices and names into immutable Committed Items, and places their charges
-- in a Check. Payments and Check restructuring belong to 5C; Submit, Orders,
-- and closure belong to 5D.

-- The Check target belongs to the draft, not the Session, so it resets to the
-- canonical default every time a new draft opens.
ALTER TABLE order_drafts
    ADD COLUMN IF NOT EXISTS check_target TEXT NOT NULL DEFAULT 'CURRENT_UNPAID';

ALTER TABLE order_drafts ADD CONSTRAINT order_draft_check_target_valid
    CHECK (check_target IN ('CURRENT_UNPAID', 'NEW_CHECK'));

-- The state domain ships complete even though 5B writes only OPEN, so the
-- CURRENT_UNPAID target query's state filter is meaningful rather than
-- vacuous and 5C adds columns without rewriting this constraint. See ADR-014.
CREATE TABLE IF NOT EXISTS checks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    service_session_id UUID NOT NULL REFERENCES service_sessions(id) ON DELETE RESTRICT,
    state TEXT NOT NULL DEFAULT 'OPEN',
    charge_vnd BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT check_state_valid CHECK (state IN ('OPEN', 'SETTLED', 'MERGED')),
    CONSTRAINT check_charge_vnd_valid CHECK (charge_vnd >= 0)
);

CREATE INDEX IF NOT EXISTS check_service_session_index
    ON checks (service_session_id);

-- Serves the CURRENT_UNPAID lookup directly.
CREATE INDEX IF NOT EXISTS check_open_per_session_index
    ON checks (service_session_id, created_at DESC, id DESC)
    WHERE state = 'OPEN';

-- An immutable commercial snapshot. The name columns are copies, not
-- references: a Menu Item renamed or retired tomorrow must not rewrite what a
-- customer was charged for today. menu_item_id is retained as a reporting
-- link. There is deliberately no size_id, matching the canonical schema.
CREATE TABLE IF NOT EXISTS committed_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_draft_id UUID NOT NULL REFERENCES order_drafts(id) ON DELETE RESTRICT,
    source_draft_item_id UUID NOT NULL REFERENCES order_draft_items(id) ON DELETE RESTRICT,
    menu_item_id UUID NOT NULL REFERENCES menu_items(id) ON DELETE RESTRICT,
    category_name TEXT NOT NULL,
    item_name TEXT NOT NULL,
    size_name TEXT,
    quantity INTEGER NOT NULL,
    unit_price_vnd BIGINT NOT NULL,
    total_vnd BIGINT NOT NULL,
    preparation_note TEXT,
    committed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT committed_item_quantity_valid
        CHECK (quantity BETWEEN 1 AND 9999),
    CONSTRAINT committed_item_unit_price_vnd_valid
        CHECK (unit_price_vnd > 0),
    CONSTRAINT committed_item_total_vnd_valid
        CHECK (total_vnd > 0 AND total_vnd = quantity::BIGINT * unit_price_vnd)
);

-- Committing one draft item twice is unrepresentable. The EDITABLE ->
-- COMMITTED draft transition is the primary guard; this index is the
-- database-level backstop that turns a logic error into a constraint
-- violation rather than a duplicate charge.
CREATE UNIQUE INDEX IF NOT EXISTS committed_item_source_draft_item_unique
    ON committed_items (source_draft_item_id);

CREATE INDEX IF NOT EXISTS committed_item_order_draft_index
    ON committed_items (order_draft_id);

CREATE TABLE IF NOT EXISTS committed_item_modifier_options (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    committed_item_id UUID NOT NULL REFERENCES committed_items(id) ON DELETE CASCADE,
    modifier_group_id UUID NOT NULL REFERENCES modifier_groups(id) ON DELETE RESTRICT,
    modifier_group_name TEXT NOT NULL,
    modifier_option_id UUID NOT NULL REFERENCES modifier_options(id) ON DELETE RESTRICT,
    modifier_option_name TEXT NOT NULL,
    surcharge_vnd BIGINT NOT NULL,
    CONSTRAINT committed_item_modifier_option_surcharge_vnd_valid
        CHECK (surcharge_vnd >= 0)
);

CREATE UNIQUE INDEX IF NOT EXISTS committed_item_modifier_option_unique
    ON committed_item_modifier_options (committed_item_id, modifier_option_id);

CREATE INDEX IF NOT EXISTS committed_item_modifier_option_item_index
    ON committed_item_modifier_options (committed_item_id);

-- A row states that a given quantity of one Committed Item is charged to one
-- Check. 5B always writes exactly one per item at full quantity; 5C's Split
-- reduces one and inserts another against a different Check, which is a data
-- change rather than a schema change.
CREATE TABLE IF NOT EXISTS charge_allocations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    committed_item_id UUID NOT NULL REFERENCES committed_items(id) ON DELETE CASCADE,
    check_id UUID NOT NULL REFERENCES checks(id) ON DELETE RESTRICT,
    quantity INTEGER NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT charge_allocation_quantity_valid
        CHECK (quantity BETWEEN 1 AND 9999)
);

CREATE UNIQUE INDEX IF NOT EXISTS charge_allocation_item_check_unique
    ON charge_allocations (committed_item_id, check_id);

CREATE INDEX IF NOT EXISTS charge_allocation_check_index
    ON charge_allocations (check_id);

COMMENT ON TABLE checks IS
    'Owned by internal/sales. 5B writes only OPEN; 5C adds settlement.';
COMMENT ON TABLE committed_items IS
    'Owned by internal/sales. Immutable after insert: no phase updates a row.';
