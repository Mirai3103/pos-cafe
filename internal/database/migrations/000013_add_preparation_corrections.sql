-- Phase 6B: Preparation Corrections & Recovery.
--
-- Exceptional preparation workflows need typed facts, not audit JSON: an
-- alert stays active until an identified staff member acknowledges it, a
-- Waste is one terminal fact per unit, a Remake is one linked replacement
-- per Waste, and a State Correction is an auditable declaration that one
-- recorded transition was mistaken.
--
-- CANCELLATION and CHANGE alert kinds are declared here for the complete
-- alert domain (ADR-028), but Phase 6B ships no writer for them; Phase 6C
-- adds the Cancellation commands. The transition constraint stays narrower:
-- it admits only pairs whose commands exist, so Cancellation's pair waits
-- for Phase 6C too.
--
-- Every statement is rerunnable: the backfill contract test drops the 6B
-- objects, executes this file as text, and its cleanup executes it again.

ALTER TABLE preparation_units
    ADD COLUMN IF NOT EXISTS priority TEXT NOT NULL DEFAULT 'STANDARD';

ALTER TABLE preparation_units
    DROP CONSTRAINT IF EXISTS preparation_unit_priority_valid;
ALTER TABLE preparation_units
    ADD CONSTRAINT preparation_unit_priority_valid
    CHECK (priority IN ('STANDARD', 'REMAKE'));

ALTER TABLE preparation_units
    ADD COLUMN IF NOT EXISTS remake_of_preparation_unit_id UUID;

ALTER TABLE preparation_units
    DROP CONSTRAINT IF EXISTS preparation_units_remake_of_preparation_unit_id_fkey;
ALTER TABLE preparation_units
    ADD CONSTRAINT preparation_units_remake_of_preparation_unit_id_fkey
    FOREIGN KEY (remake_of_preparation_unit_id)
    REFERENCES preparation_units (id)
    ON DELETE RESTRICT;

ALTER TABLE preparation_units
    DROP CONSTRAINT IF EXISTS preparation_unit_priority_link_valid;
ALTER TABLE preparation_units
    ADD CONSTRAINT preparation_unit_priority_link_valid
    CHECK (
        (priority = 'STANDARD' AND remake_of_preparation_unit_id IS NULL)
        OR (priority = 'REMAKE' AND remake_of_preparation_unit_id IS NOT NULL)
    );

CREATE TABLE IF NOT EXISTS preparation_alerts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    preparation_unit_id UUID NOT NULL
        CONSTRAINT preparation_alerts_preparation_unit_id_fkey
        REFERENCES preparation_units (id) ON DELETE RESTRICT,
    kind TEXT NOT NULL,
    reason TEXT NOT NULL,
    note TEXT,
    created_by_staff_identity_id UUID NOT NULL
        CONSTRAINT preparation_alerts_created_by_staff_identity_id_fkey
        REFERENCES staff_identities (id) ON DELETE RESTRICT,
    created_staff_access_session_id UUID NOT NULL
        CONSTRAINT preparation_alerts_created_staff_access_session_id_fkey
        REFERENCES staff_access_sessions (id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    acknowledged_by_staff_identity_id UUID
        CONSTRAINT preparation_alerts_acknowledged_by_staff_identity_id_fkey
        REFERENCES staff_identities (id) ON DELETE RESTRICT,
    acknowledged_staff_access_session_id UUID
        CONSTRAINT preparation_alerts_acknowledged_staff_access_session_id_fkey
        REFERENCES staff_access_sessions (id) ON DELETE RESTRICT,
    acknowledged_at TIMESTAMPTZ,
    CONSTRAINT preparation_alert_kind_valid
        CHECK (kind IN ('CANCELLATION', 'CHANGE', 'WASTE')),
    CONSTRAINT preparation_alert_reason_valid
        CHECK (
            (kind = 'WASTE' AND reason IN (
                'PREPARATION_ERROR', 'QUALITY_FAILURE', 'CUSTOMER_REQUEST', 'OTHER'))
            OR (kind IN ('CANCELLATION', 'CHANGE') AND reason IN (
                'CUSTOMER_REQUEST', 'ORDER_ENTRY_ERROR', 'ITEM_UNAVAILABLE', 'OTHER'))
        ),
    CONSTRAINT preparation_alert_note_valid
        CHECK (note IS NULL OR char_length(note) BETWEEN 1 AND 500),
    CONSTRAINT preparation_alert_other_note_valid
        CHECK (reason <> 'OTHER' OR (note IS NOT NULL AND btrim(note) <> '')),
    CONSTRAINT preparation_alert_acknowledgment_tuple_valid
        CHECK (
            (acknowledged_by_staff_identity_id IS NULL
             AND acknowledged_staff_access_session_id IS NULL
             AND acknowledged_at IS NULL)
            OR (acknowledged_by_staff_identity_id IS NOT NULL
                AND acknowledged_staff_access_session_id IS NOT NULL
                AND acknowledged_at IS NOT NULL)
        )
);

CREATE INDEX IF NOT EXISTS preparation_alert_active_index
    ON preparation_alerts (acknowledged_at, created_at, id);

CREATE TABLE IF NOT EXISTS preparation_wastes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    preparation_unit_id UUID NOT NULL
        CONSTRAINT preparation_wastes_preparation_unit_id_fkey
        REFERENCES preparation_units (id) ON DELETE RESTRICT,
    prior_state TEXT NOT NULL,
    reason TEXT NOT NULL,
    note TEXT,
    actor_staff_identity_id UUID NOT NULL
        CONSTRAINT preparation_wastes_actor_staff_identity_id_fkey
        REFERENCES staff_identities (id) ON DELETE RESTRICT,
    staff_access_session_id UUID NOT NULL
        CONSTRAINT preparation_wastes_staff_access_session_id_fkey
        REFERENCES staff_access_sessions (id) ON DELETE RESTRICT,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT preparation_waste_unit_unique UNIQUE (preparation_unit_id),
    CONSTRAINT preparation_waste_prior_state_valid
        CHECK (prior_state IN ('IN_PREPARATION', 'READY')),
    CONSTRAINT preparation_waste_reason_valid
        CHECK (reason IN ('PREPARATION_ERROR', 'QUALITY_FAILURE',
                          'CUSTOMER_REQUEST', 'OTHER')),
    CONSTRAINT preparation_waste_note_valid
        CHECK (note IS NULL OR char_length(note) BETWEEN 1 AND 500),
    CONSTRAINT preparation_waste_other_note_valid
        CHECK (reason <> 'OTHER' OR (note IS NOT NULL AND btrim(note) <> ''))
);

CREATE TABLE IF NOT EXISTS preparation_remakes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    waste_id UUID NOT NULL
        CONSTRAINT preparation_remakes_waste_id_fkey
        REFERENCES preparation_wastes (id) ON DELETE RESTRICT,
    preparation_unit_id UUID NOT NULL
        CONSTRAINT preparation_remakes_preparation_unit_id_fkey
        REFERENCES preparation_units (id) ON DELETE RESTRICT,
    reason TEXT NOT NULL,
    note TEXT,
    actor_staff_identity_id UUID NOT NULL
        CONSTRAINT preparation_remakes_actor_staff_identity_id_fkey
        REFERENCES staff_identities (id) ON DELETE RESTRICT,
    staff_access_session_id UUID NOT NULL
        CONSTRAINT preparation_remakes_staff_access_session_id_fkey
        REFERENCES staff_access_sessions (id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT preparation_remake_waste_unique UNIQUE (waste_id),
    CONSTRAINT preparation_remake_unit_unique UNIQUE (preparation_unit_id),
    CONSTRAINT preparation_remake_reason_valid
        CHECK (reason IN ('PREPARATION_ERROR', 'QUALITY_FAILURE', 'OTHER')),
    CONSTRAINT preparation_remake_note_valid
        CHECK (note IS NULL OR char_length(note) BETWEEN 1 AND 500),
    CONSTRAINT preparation_remake_other_note_valid
        CHECK (reason <> 'OTHER' OR (note IS NOT NULL AND btrim(note) <> ''))
);

CREATE TABLE IF NOT EXISTS preparation_state_corrections (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    preparation_unit_id UUID NOT NULL
        CONSTRAINT preparation_state_corrections_preparation_unit_id_fkey
        REFERENCES preparation_units (id) ON DELETE RESTRICT,
    prior_state TEXT NOT NULL,
    resulting_state TEXT NOT NULL,
    reason TEXT NOT NULL,
    note TEXT,
    actor_staff_identity_id UUID NOT NULL
        CONSTRAINT preparation_state_corrections_actor_staff_identity_id_fkey
        REFERENCES staff_identities (id) ON DELETE RESTRICT,
    staff_access_session_id UUID NOT NULL
        CONSTRAINT preparation_state_corrections_staff_access_session_id_fkey
        REFERENCES staff_access_sessions (id) ON DELETE RESTRICT,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT preparation_state_correction_states_valid
        CHECK (
            (prior_state, resulting_state) IN (
                ('IN_PREPARATION', 'QUEUED'),
                ('READY', 'IN_PREPARATION'),
                ('FULFILLED', 'READY')
            )
        ),
    CONSTRAINT preparation_state_correction_reason_valid
        CHECK (reason IN ('STATE_RECORDED_IN_ERROR', 'OTHER')),
    CONSTRAINT preparation_state_correction_note_valid
        CHECK (note IS NULL OR char_length(note) BETWEEN 1 AND 500),
    CONSTRAINT preparation_state_correction_other_note_valid
        CHECK (reason <> 'OTHER' OR (note IS NOT NULL AND btrim(note) <> ''))
);

CREATE INDEX IF NOT EXISTS preparation_state_correction_unit_index
    ON preparation_state_corrections (preparation_unit_id, occurred_at, id);

ALTER TABLE preparation_unit_transitions
    DROP CONSTRAINT IF EXISTS preparation_unit_transition_states_valid;
ALTER TABLE preparation_unit_transitions
    ADD CONSTRAINT preparation_unit_transition_states_valid
    CHECK (
        (prior_state, resulting_state) IN (
            ('QUEUED', 'IN_PREPARATION'),
            ('IN_PREPARATION', 'READY'),
            ('READY', 'FULFILLED'),
            ('IN_PREPARATION', 'WASTED'),
            ('READY', 'WASTED'),
            ('IN_PREPARATION', 'QUEUED'),
            ('READY', 'IN_PREPARATION'),
            ('FULFILLED', 'READY')
        )
    );
