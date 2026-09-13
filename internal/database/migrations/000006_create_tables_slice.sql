-- Phase 3: Tables & Floor Layout.
--
-- `tables` is owned by internal/tables.
--
-- `service_sessions` and `table_assignments` are provisioned here so that the
-- canonical Table overview read (which reports the active Service Sessions
-- occupying each Table) can ship complete in Phase 3. Business ownership of
-- both belongs to internal/sales in Phase 5; Phase 3 only reads them.
-- See ADR-006 in spec/decisions.md.

CREATE TABLE IF NOT EXISTS tables (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    normalized_name TEXT NOT NULL,
    available BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT table_name_valid
        CHECK (char_length(name) BETWEEN 1 AND 60 AND name = btrim(name)),
    CONSTRAINT table_normalized_name_valid
        CHECK (normalized_name = lower(name))
);

CREATE UNIQUE INDEX IF NOT EXISTS table_normalized_name_unique
    ON tables (normalized_name);

CREATE INDEX IF NOT EXISTS idx_tables_created_at
    ON tables (created_at ASC, id ASC);

-- Owned by internal/sales (Phase 5). Provisioned early for the Tables overview.
-- Phase 5 adds: ALTER TABLE service_sessions
--   ADD COLUMN sales_shift_id UUID NOT NULL REFERENCES sales_shifts(id);
CREATE TABLE IF NOT EXISTS service_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    service_number TEXT NOT NULL,
    service_mode TEXT NOT NULL DEFAULT 'TAKEAWAY',
    state TEXT NOT NULL DEFAULT 'ACTIVE',
    created_by_staff_identity_id UUID NOT NULL REFERENCES staff_identities(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT service_session_number_valid
        CHECK (service_number ~ '^[A-Z0-9]{6}$'),
    CONSTRAINT service_session_mode_valid
        CHECK (service_mode IN ('DINE_IN', 'TAKEAWAY')),
    CONSTRAINT service_session_state_valid
        CHECK (state IN ('ACTIVE', 'COMPLETED', 'CANCELLED'))
);

CREATE UNIQUE INDEX IF NOT EXISTS service_session_service_number_unique
    ON service_sessions (service_number);

COMMENT ON TABLE service_sessions IS
    'Owned by internal/sales (Phase 5). Provisioned in Phase 3 for the Tables overview read; sales_shift_id is added in Phase 5.';

-- Owned by internal/sales (Phase 5). Provisioned early for the Tables overview.
CREATE TABLE IF NOT EXISTS table_assignments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    table_id UUID NOT NULL REFERENCES tables(id) ON DELETE RESTRICT,
    service_session_id UUID NOT NULL REFERENCES service_sessions(id) ON DELETE RESTRICT,
    assigned_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    assigned_by_staff_identity_id UUID NOT NULL REFERENCES staff_identities(id) ON DELETE RESTRICT,
    sequence INTEGER NOT NULL,
    released_at TIMESTAMPTZ,
    released_by_staff_identity_id UUID REFERENCES staff_identities(id) ON DELETE RESTRICT,
    CONSTRAINT table_assignment_release_evidence_valid
        CHECK (
            (released_at IS NULL AND released_by_staff_identity_id IS NULL)
            OR (released_at IS NOT NULL AND released_by_staff_identity_id IS NOT NULL)
        )
);

CREATE UNIQUE INDEX IF NOT EXISTS table_assignment_current_unique
    ON table_assignments (table_id, service_session_id)
    WHERE released_at IS NULL;

CREATE INDEX IF NOT EXISTS table_assignment_table_index
    ON table_assignments (table_id);

CREATE INDEX IF NOT EXISTS table_assignment_service_session_index
    ON table_assignments (service_session_id);

COMMENT ON TABLE table_assignments IS
    'Owned by internal/sales (Phase 5). Provisioned in Phase 3 for the Tables overview read.';
