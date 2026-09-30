-- Phase 6C: Preparation Cancellation & Financial Corrections.
--
-- A Charge Adjustment is an append-only reduction of customer charge sourced
-- by exactly one Cancellation or Comp. Cancellation and Comp facts name that
-- source; Payment Void, Refund, its two allocation sets, and its completion
-- are append-only money facts. Every source record stays immutable: only the
-- already-denormalized checks.charge_vnd and preparation_units.state carry
-- current-state meaning, and the transition constraint gains the one
-- Cancellation pair.
--
-- Every statement is rerunnable: the schema contract tests execute this file
-- as text, and the backfill suites run it again on cleanup.

CREATE TABLE IF NOT EXISTS charge_adjustments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    kind TEXT NOT NULL,
    scope TEXT NOT NULL,
    preparation_unit_id UUID NOT NULL
        CONSTRAINT charge_adjustments_preparation_unit_id_fkey
        REFERENCES preparation_units (id) ON DELETE RESTRICT,
    preparation_waste_id UUID
        CONSTRAINT charge_adjustments_preparation_waste_id_fkey
        REFERENCES preparation_wastes (id) ON DELETE RESTRICT,
    charge_allocation_id UUID NOT NULL
        CONSTRAINT charge_adjustments_charge_allocation_id_fkey
        REFERENCES charge_allocations (id) ON DELETE RESTRICT,
    check_id UUID NOT NULL
        CONSTRAINT charge_adjustments_check_id_fkey
        REFERENCES checks (id) ON DELETE RESTRICT,
    completed_sale_id UUID
        CONSTRAINT charge_adjustments_completed_sale_id_fkey
        REFERENCES completed_sales (id) ON DELETE RESTRICT,
    sales_shift_id UUID NOT NULL
        CONSTRAINT charge_adjustments_sales_shift_id_fkey
        REFERENCES sales_shifts (id) ON DELETE RESTRICT,
    amount_vnd BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT charge_adjustment_kind_source_valid CHECK (
        (kind = 'CANCELLATION' AND preparation_waste_id IS NULL) OR
        (kind = 'COMP' AND preparation_waste_id IS NOT NULL)
    ),
    CONSTRAINT charge_adjustment_scope_valid CHECK (
        (scope = 'LIVE_CHECK' AND completed_sale_id IS NULL) OR
        (scope = 'POST_SALE' AND completed_sale_id IS NOT NULL)
    ),
    CONSTRAINT charge_adjustment_amount_positive CHECK (amount_vnd > 0),
    CONSTRAINT charge_adjustment_kind_unit_unique UNIQUE (kind, preparation_unit_id)
);

CREATE INDEX IF NOT EXISTS charge_adjustment_check_index
    ON charge_adjustments (check_id, created_at, id);
CREATE INDEX IF NOT EXISTS charge_adjustment_completed_sale_index
    ON charge_adjustments (completed_sale_id, created_at, id);
CREATE INDEX IF NOT EXISTS charge_adjustment_shift_index
    ON charge_adjustments (sales_shift_id, created_at, id);
CREATE INDEX IF NOT EXISTS charge_adjustment_unit_index
    ON charge_adjustments (preparation_unit_id, created_at, id);
CREATE INDEX IF NOT EXISTS charge_adjustment_waste_index
    ON charge_adjustments (preparation_waste_id, created_at, id);

CREATE TABLE IF NOT EXISTS preparation_cancellations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    preparation_unit_id UUID NOT NULL
        CONSTRAINT preparation_cancellations_preparation_unit_id_fkey
        REFERENCES preparation_units (id) ON DELETE RESTRICT,
    kind TEXT NOT NULL,
    charge_adjustment_id UUID
        CONSTRAINT preparation_cancellations_charge_adjustment_id_fkey
        REFERENCES charge_adjustments (id) ON DELETE RESTRICT,
    replacement_order_id UUID
        CONSTRAINT preparation_cancellations_replacement_order_id_fkey
        REFERENCES orders (id) ON DELETE RESTRICT,
    reason TEXT NOT NULL,
    note TEXT,
    actor_staff_identity_id UUID NOT NULL
        CONSTRAINT preparation_cancellations_actor_staff_identity_id_fkey
        REFERENCES staff_identities (id) ON DELETE RESTRICT,
    staff_access_session_id UUID NOT NULL
        CONSTRAINT preparation_cancellations_staff_access_session_id_fkey
        REFERENCES staff_access_sessions (id) ON DELETE RESTRICT,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT preparation_cancellation_unit_unique UNIQUE (preparation_unit_id),
    CONSTRAINT preparation_cancellation_adjustment_unique UNIQUE (charge_adjustment_id),
    CONSTRAINT preparation_cancellation_kind_replacement_valid CHECK (
        (kind = 'CANCELLATION' AND replacement_order_id IS NULL) OR
        (kind = 'CHANGE' AND replacement_order_id IS NOT NULL)
    ),
    CONSTRAINT preparation_cancellation_reason_valid CHECK (
        reason IN ('CUSTOMER_REQUEST', 'ORDER_ENTRY_ERROR', 'ITEM_UNAVAILABLE', 'OTHER')
    ),
    CONSTRAINT preparation_cancellation_note_valid CHECK (
        note IS NULL OR char_length(note) BETWEEN 1 AND 500
    ),
    CONSTRAINT preparation_cancellation_other_note_valid CHECK (
        reason <> 'OTHER' OR (note IS NOT NULL AND btrim(note) <> '')
    )
);

CREATE INDEX IF NOT EXISTS preparation_cancellation_occurred_index
    ON preparation_cancellations (occurred_at, id);

CREATE TABLE IF NOT EXISTS sales_comps (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    preparation_waste_id UUID NOT NULL
        CONSTRAINT sales_comps_preparation_waste_id_fkey
        REFERENCES preparation_wastes (id) ON DELETE RESTRICT,
    charge_adjustment_id UUID NOT NULL
        CONSTRAINT sales_comps_charge_adjustment_id_fkey
        REFERENCES charge_adjustments (id) ON DELETE RESTRICT,
    reason TEXT NOT NULL,
    note TEXT,
    actor_staff_identity_id UUID NOT NULL
        CONSTRAINT sales_comps_actor_staff_identity_id_fkey
        REFERENCES staff_identities (id) ON DELETE RESTRICT,
    staff_access_session_id UUID NOT NULL
        CONSTRAINT sales_comps_staff_access_session_id_fkey
        REFERENCES staff_access_sessions (id) ON DELETE RESTRICT,
    approved_by_staff_identity_id UUID NOT NULL
        CONSTRAINT sales_comps_approved_by_staff_identity_id_fkey
        REFERENCES staff_identities (id) ON DELETE RESTRICT,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT sales_comp_waste_unique UNIQUE (preparation_waste_id),
    CONSTRAINT sales_comp_adjustment_unique UNIQUE (charge_adjustment_id),
    CONSTRAINT sales_comp_reason_valid CHECK (
        reason IN ('CAFE_ERROR', 'QUALITY_FAILURE', 'SERVICE_RECOVERY', 'OTHER')
    ),
    CONSTRAINT sales_comp_note_valid CHECK (
        note IS NULL OR char_length(note) BETWEEN 1 AND 500
    ),
    CONSTRAINT sales_comp_other_note_valid CHECK (
        reason <> 'OTHER' OR (note IS NOT NULL AND btrim(note) <> '')
    )
);

CREATE INDEX IF NOT EXISTS sales_comp_occurred_index
    ON sales_comps (occurred_at, id);

CREATE TABLE IF NOT EXISTS payment_voids (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    payment_id UUID NOT NULL
        CONSTRAINT payment_voids_payment_id_fkey
        REFERENCES payments (id) ON DELETE RESTRICT,
    sales_shift_id UUID NOT NULL
        CONSTRAINT payment_voids_sales_shift_id_fkey
        REFERENCES sales_shifts (id) ON DELETE RESTRICT,
    amount_vnd BIGINT NOT NULL,
    reason TEXT NOT NULL,
    note TEXT,
    actor_staff_identity_id UUID NOT NULL
        CONSTRAINT payment_voids_actor_staff_identity_id_fkey
        REFERENCES staff_identities (id) ON DELETE RESTRICT,
    staff_access_session_id UUID NOT NULL
        CONSTRAINT payment_voids_staff_access_session_id_fkey
        REFERENCES staff_access_sessions (id) ON DELETE RESTRICT,
    approved_by_staff_identity_id UUID NOT NULL
        CONSTRAINT payment_voids_approved_by_staff_identity_id_fkey
        REFERENCES staff_identities (id) ON DELETE RESTRICT,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT payment_void_payment_unique UNIQUE (payment_id),
    CONSTRAINT payment_void_amount_positive CHECK (amount_vnd > 0),
    CONSTRAINT payment_void_reason_valid CHECK (
        reason IN ('DUPLICATE_PAYMENT', 'WRONG_AMOUNT', 'WRONG_METHOD',
                   'PAYMENT_RECORDED_IN_ERROR', 'OTHER')
    ),
    CONSTRAINT payment_void_note_valid CHECK (
        note IS NULL OR char_length(note) BETWEEN 1 AND 500
    ),
    CONSTRAINT payment_void_other_note_valid CHECK (
        reason <> 'OTHER' OR (note IS NOT NULL AND btrim(note) <> '')
    )
);

CREATE INDEX IF NOT EXISTS payment_void_shift_index
    ON payment_voids (sales_shift_id, occurred_at, id);

CREATE TABLE IF NOT EXISTS refunds (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    check_id UUID NOT NULL
        CONSTRAINT refunds_check_id_fkey
        REFERENCES checks (id) ON DELETE RESTRICT,
    completed_sale_id UUID
        CONSTRAINT refunds_completed_sale_id_fkey
        REFERENCES completed_sales (id) ON DELETE RESTRICT,
    sales_shift_id UUID NOT NULL
        CONSTRAINT refunds_sales_shift_id_fkey
        REFERENCES sales_shifts (id) ON DELETE RESTRICT,
    method TEXT NOT NULL,
    amount_vnd BIGINT NOT NULL,
    reason TEXT NOT NULL,
    note TEXT,
    actor_staff_identity_id UUID NOT NULL
        CONSTRAINT refunds_actor_staff_identity_id_fkey
        REFERENCES staff_identities (id) ON DELETE RESTRICT,
    staff_access_session_id UUID NOT NULL
        CONSTRAINT refunds_staff_access_session_id_fkey
        REFERENCES staff_access_sessions (id) ON DELETE RESTRICT,
    approved_by_staff_identity_id UUID NOT NULL
        CONSTRAINT refunds_approved_by_staff_identity_id_fkey
        REFERENCES staff_identities (id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT refund_method_valid CHECK (method IN ('CASH', 'MANUAL_QR')),
    CONSTRAINT refund_amount_positive CHECK (amount_vnd > 0),
    CONSTRAINT refund_reason_valid CHECK (
        reason IN ('CUSTOMER_REQUEST', 'ITEM_UNAVAILABLE', 'CAFE_ERROR', 'OTHER')
    ),
    CONSTRAINT refund_note_valid CHECK (
        note IS NULL OR char_length(note) BETWEEN 1 AND 500
    ),
    CONSTRAINT refund_other_note_valid CHECK (
        reason <> 'OTHER' OR (note IS NOT NULL AND btrim(note) <> '')
    )
);

CREATE INDEX IF NOT EXISTS refund_check_index
    ON refunds (check_id, created_at, id);
CREATE INDEX IF NOT EXISTS refund_completed_sale_index
    ON refunds (completed_sale_id, created_at, id);
CREATE INDEX IF NOT EXISTS refund_shift_index
    ON refunds (sales_shift_id, created_at, id);

CREATE TABLE IF NOT EXISTS refund_payment_allocations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    refund_id UUID NOT NULL
        CONSTRAINT refund_payment_allocations_refund_id_fkey
        REFERENCES refunds (id) ON DELETE RESTRICT,
    payment_id UUID NOT NULL
        CONSTRAINT refund_payment_allocations_payment_id_fkey
        REFERENCES payments (id) ON DELETE RESTRICT,
    amount_vnd BIGINT NOT NULL,
    CONSTRAINT refund_payment_allocation_pair_unique UNIQUE (refund_id, payment_id),
    CONSTRAINT refund_payment_allocation_amount_positive CHECK (amount_vnd > 0)
);

CREATE INDEX IF NOT EXISTS refund_payment_allocation_payment_index
    ON refund_payment_allocations (payment_id, refund_id);

CREATE TABLE IF NOT EXISTS refund_adjustment_allocations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    refund_id UUID NOT NULL
        CONSTRAINT refund_adjustment_allocations_refund_id_fkey
        REFERENCES refunds (id) ON DELETE RESTRICT,
    charge_adjustment_id UUID NOT NULL
        CONSTRAINT refund_adjustment_allocations_charge_adjustment_id_fkey
        REFERENCES charge_adjustments (id) ON DELETE RESTRICT,
    amount_vnd BIGINT NOT NULL,
    CONSTRAINT refund_adjustment_allocation_pair_unique UNIQUE (refund_id, charge_adjustment_id),
    CONSTRAINT refund_adjustment_allocation_amount_positive CHECK (amount_vnd > 0)
);

CREATE INDEX IF NOT EXISTS refund_adjustment_allocation_adjustment_index
    ON refund_adjustment_allocations (charge_adjustment_id, refund_id);

CREATE TABLE IF NOT EXISTS refund_completions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    refund_id UUID NOT NULL
        CONSTRAINT refund_completions_refund_id_fkey
        REFERENCES refunds (id) ON DELETE RESTRICT,
    transaction_reference TEXT,
    completed_by_staff_identity_id UUID NOT NULL
        CONSTRAINT refund_completions_completed_by_staff_identity_id_fkey
        REFERENCES staff_identities (id) ON DELETE RESTRICT,
    staff_access_session_id UUID NOT NULL
        CONSTRAINT refund_completions_staff_access_session_id_fkey
        REFERENCES staff_access_sessions (id) ON DELETE RESTRICT,
    completed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT refund_completion_refund_unique UNIQUE (refund_id),
    CONSTRAINT refund_completion_reference_valid CHECK (
        transaction_reference IS NULL
        OR (transaction_reference = btrim(transaction_reference)
            AND char_length(transaction_reference) BETWEEN 1 AND 100)
    )
);

CREATE INDEX IF NOT EXISTS refund_completion_completed_index
    ON refund_completions (completed_at, id);

-- The complete transition domain gains exactly the Cancellation pair; every
-- other pair stays exactly as migration 000013 declared it.
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
            ('FULFILLED', 'READY'),
            ('QUEUED', 'CANCELLED')
        )
    );
