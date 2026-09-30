-- Phase 4: Sales Shift & Cash Movements.
--
-- Both tables are owned by internal/shift.
--
-- Expected Cash is NOT stored. It is computed on read from the Opening Float
-- and the Cash Movements. Phase 5 adds Cash Payments and Cash Refunds to the
-- same computation without a schema change. See ADR-008 in spec/decisions.md.

CREATE TABLE IF NOT EXISTS sales_shifts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    state TEXT NOT NULL DEFAULT 'OPEN',
    opened_by_staff_identity_id UUID NOT NULL REFERENCES staff_identities(id) ON DELETE RESTRICT,
    opening_float_vnd BIGINT NOT NULL,
    opened_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT sales_shift_state_valid
        CHECK (state IN ('OPEN', 'CLOSED')),
    CONSTRAINT sales_shift_opening_float_vnd_valid
        CHECK (opening_float_vnd >= 0 AND opening_float_vnd <= 2147483647)
);

-- The sole authority for the one-open-Shift invariant. A Go pre-check may
-- produce a friendlier error, but this index resolves genuine races.
CREATE UNIQUE INDEX IF NOT EXISTS sales_shift_only_one_open_unique
    ON sales_shifts (state)
    WHERE state = 'OPEN';

COMMENT ON TABLE sales_shifts IS
    'Owned by internal/shift (Phase 4). At most one row may be in OPEN state. Phase 4 ships no close operation; see the Non-Goals in the Phase 4 design spec.';

CREATE TABLE IF NOT EXISTS cash_movements (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sales_shift_id UUID NOT NULL REFERENCES sales_shifts(id) ON DELETE RESTRICT,
    method TEXT NOT NULL,
    amount_vnd BIGINT NOT NULL,
    reason TEXT NOT NULL,
    note TEXT,
    initiated_by_staff_identity_id UUID NOT NULL REFERENCES staff_identities(id) ON DELETE RESTRICT,
    initiated_staff_access_session_id UUID NOT NULL REFERENCES staff_access_sessions(id) ON DELETE RESTRICT,
    approved_by_staff_identity_id UUID NOT NULL REFERENCES staff_identities(id) ON DELETE RESTRICT,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT cash_movement_method_valid
        CHECK (method IN ('PAY_IN', 'PAY_OUT')),
    CONSTRAINT cash_movement_amount_vnd_valid
        CHECK (amount_vnd > 0 AND amount_vnd <= 2147483647),
    CONSTRAINT cash_movement_reason_valid
        CHECK (reason IN ('ADD_CHANGE_FUND', 'REMOVE_EXCESS_FLOAT', 'SAFE_DROP', 'OTHER')),
    CONSTRAINT cash_movement_note_valid
        CHECK (
            (note IS NULL OR (note = btrim(note) AND char_length(note) BETWEEN 1 AND 500))
            AND (reason <> 'OTHER' OR note IS NOT NULL)
        )
);

CREATE INDEX IF NOT EXISTS cash_movement_sales_shift_index
    ON cash_movements (sales_shift_id, occurred_at);

COMMENT ON TABLE cash_movements IS
    'Owned by internal/shift (Phase 4). Append-only: Phase 4 provides no edit, reverse, or delete operation. Direction is carried by method, never by a negative amount.';
