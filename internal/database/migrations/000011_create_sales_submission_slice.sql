-- Phase 5D: Submission, Preparation Units & Service Session Closure.
--
-- Submit is the preparation boundary: it turns Committed Items into an Order
-- and the Preparation Units the bar works from, without repricing anything.
-- Closure freezes a Service Session into an immutable Completed Sale.
--
-- order_items deliberately carries no commercial snapshot (ADR-025):
-- committed_items is immutable by 5B's rule, and copying it across a
-- one-to-one foreign key would only create a way for the two to disagree.
--
-- preparation_units does snapshot, for a reason order_items does not share:
-- the bar display is the hottest read path in the system and Phase 6 reads
-- these same columns for FIFO ordering and alerts.

CREATE TABLE IF NOT EXISTS orders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    service_session_id UUID NOT NULL REFERENCES service_sessions(id) ON DELETE RESTRICT,
    order_draft_id UUID NOT NULL REFERENCES order_drafts(id) ON DELETE RESTRICT,
    submitted_by_staff_identity_id UUID NOT NULL REFERENCES staff_identities(id) ON DELETE RESTRICT,
    submitted_staff_access_session_id UUID NOT NULL REFERENCES staff_access_sessions(id) ON DELETE RESTRICT,
    submitted_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS order_draft_unique ON orders (order_draft_id);
CREATE INDEX IF NOT EXISTS order_service_session_index ON orders (service_session_id);

CREATE TABLE IF NOT EXISTS order_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id UUID NOT NULL REFERENCES orders(id) ON DELETE RESTRICT,
    committed_item_id UUID NOT NULL REFERENCES committed_items(id) ON DELETE RESTRICT
);

CREATE UNIQUE INDEX IF NOT EXISTS order_item_committed_item_unique
    ON order_items (committed_item_id);
CREATE INDEX IF NOT EXISTS order_item_order_index ON order_items (order_id);

CREATE TABLE IF NOT EXISTS preparation_units (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_item_id UUID NOT NULL REFERENCES order_items(id) ON DELETE RESTRICT,
    unit_number INTEGER NOT NULL,
    state TEXT NOT NULL DEFAULT 'QUEUED',
    service_number TEXT NOT NULL,
    category_name TEXT NOT NULL,
    item_name TEXT NOT NULL,
    size_name TEXT,
    modifiers JSONB NOT NULL DEFAULT '[]'::jsonb,
    preparation_note TEXT,
    queued_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT preparation_unit_state_valid
        CHECK (state IN ('QUEUED', 'IN_PREPARATION', 'READY', 'FULFILLED', 'CANCELLED', 'WASTED')),
    CONSTRAINT preparation_unit_number_valid
        CHECK (unit_number BETWEEN 1 AND 9999)
);

CREATE UNIQUE INDEX IF NOT EXISTS preparation_unit_item_number_unique
    ON preparation_units (order_item_id, unit_number);
CREATE INDEX IF NOT EXISTS preparation_unit_state_queued_index
    ON preparation_units (state, queued_at);

CREATE TABLE IF NOT EXISTS preparation_unit_transitions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    preparation_unit_id UUID NOT NULL REFERENCES preparation_units(id) ON DELETE RESTRICT,
    prior_state TEXT NOT NULL,
    resulting_state TEXT NOT NULL,
    actor_staff_identity_id UUID NOT NULL REFERENCES staff_identities(id) ON DELETE RESTRICT,
    staff_access_session_id UUID NOT NULL REFERENCES staff_access_sessions(id) ON DELETE RESTRICT,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT preparation_unit_transition_states_valid
        CHECK (
            (prior_state, resulting_state) IN (
                ('QUEUED', 'IN_PREPARATION'),
                ('IN_PREPARATION', 'READY'),
                ('READY', 'FULFILLED')
            )
        )
);

CREATE INDEX IF NOT EXISTS preparation_unit_transition_unit_index
    ON preparation_unit_transitions (preparation_unit_id, occurred_at);

CREATE TABLE IF NOT EXISTS completed_sales (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    service_session_id UUID NOT NULL REFERENCES service_sessions(id) ON DELETE RESTRICT,
    completed_by_staff_identity_id UUID NOT NULL REFERENCES staff_identities(id) ON DELETE RESTRICT,
    completed_staff_access_session_id UUID NOT NULL REFERENCES staff_access_sessions(id) ON DELETE RESTRICT,
    completed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS completed_sale_service_session_unique
    ON completed_sales (service_session_id);
