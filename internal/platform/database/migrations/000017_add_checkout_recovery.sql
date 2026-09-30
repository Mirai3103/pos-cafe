-- 000017: Checkout recovery (Phase 08, ADR-064 to ADR-066).
--
-- A WITHDRAWAL Charge Adjustment removes an unsubmitted Committed Item's
-- charge from its Check so the existing Refund can return the money. It has no
-- Preparation Unit, because unsubmitted work never had one. An abandoned
-- Service Session ends in ABANDONED with its drafts CANCELLED and its live
-- Checks ABANDONED, and one abandoned_checkouts row records who, when, and why.
-- Every statement is rerunnable.

ALTER TABLE charge_adjustments ALTER COLUMN preparation_unit_id DROP NOT NULL;

ALTER TABLE charge_adjustments DROP CONSTRAINT IF EXISTS charge_adjustment_kind_source_valid;
ALTER TABLE charge_adjustments ADD CONSTRAINT charge_adjustment_kind_source_valid CHECK (
       (kind = 'CANCELLATION'
            AND preparation_unit_id IS NOT NULL AND preparation_waste_id IS NULL)
    OR (kind = 'COMP'
            AND preparation_unit_id IS NOT NULL AND preparation_waste_id IS NOT NULL)
    OR (kind = 'WITHDRAWAL'
            AND preparation_unit_id IS NULL AND preparation_waste_id IS NULL
            AND scope = 'LIVE_CHECK')
);

-- No allocation is withdrawn twice. charge_adjustment_kind_unit_unique does not
-- cover WITHDRAWAL: its preparation_unit_id is NULL and NULLs never collide.
CREATE UNIQUE INDEX IF NOT EXISTS charge_adjustment_withdrawal_allocation_unique
    ON charge_adjustments (charge_allocation_id)
    WHERE kind = 'WITHDRAWAL';

ALTER TABLE service_sessions DROP CONSTRAINT IF EXISTS service_session_state_valid;
ALTER TABLE service_sessions ADD CONSTRAINT service_session_state_valid
    CHECK (state IN ('ACTIVE', 'CLOSED', 'ABANDONED'));

ALTER TABLE order_drafts DROP CONSTRAINT IF EXISTS order_draft_state_valid;
ALTER TABLE order_drafts ADD CONSTRAINT order_draft_state_valid
    CHECK (state IN ('EDITABLE', 'COMMITTED', 'CANCELLED'));

ALTER TABLE checks DROP CONSTRAINT IF EXISTS check_state_valid;
ALTER TABLE checks ADD CONSTRAINT check_state_valid
    CHECK (state IN ('OPEN', 'SETTLED', 'MERGED', 'ABANDONED'));

-- An ABANDONED Check keeps whatever settlement evidence it had: a Check that
-- was paid, withdrawn, and refunded was SETTLED first; an unpaid one was not.
ALTER TABLE checks DROP CONSTRAINT IF EXISTS check_settlement_evidence_valid;
ALTER TABLE checks ADD CONSTRAINT check_settlement_evidence_valid CHECK (
       (state = 'OPEN'
            AND merged_into_check_id IS NULL
            AND settled_at IS NULL
            AND settled_by_staff_identity_id IS NULL
            AND settled_during_sales_shift_id IS NULL
            AND settled_staff_access_session_id IS NULL)
    OR (state = 'SETTLED'
            AND merged_into_check_id IS NULL
            AND settled_at IS NOT NULL
            AND settled_by_staff_identity_id IS NOT NULL
            AND settled_during_sales_shift_id IS NOT NULL
            AND settled_staff_access_session_id IS NOT NULL)
    OR (state = 'MERGED'
            AND merged_into_check_id IS NOT NULL
            AND charge_vnd = 0
            AND settled_at IS NULL
            AND settled_by_staff_identity_id IS NULL
            AND settled_during_sales_shift_id IS NULL
            AND settled_staff_access_session_id IS NULL)
    OR (state = 'ABANDONED'
            AND merged_into_check_id IS NULL
            AND ((settled_at IS NULL
                  AND settled_by_staff_identity_id IS NULL
                  AND settled_during_sales_shift_id IS NULL
                  AND settled_staff_access_session_id IS NULL)
              OR (settled_at IS NOT NULL
                  AND settled_by_staff_identity_id IS NOT NULL
                  AND settled_during_sales_shift_id IS NOT NULL
                  AND settled_staff_access_session_id IS NOT NULL)))
);

CREATE TABLE IF NOT EXISTS abandoned_checkouts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    service_session_id UUID NOT NULL
        CONSTRAINT abandoned_checkouts_service_session_id_fkey
        REFERENCES service_sessions (id) ON DELETE RESTRICT,
    sales_shift_id UUID NOT NULL
        CONSTRAINT abandoned_checkouts_sales_shift_id_fkey
        REFERENCES sales_shifts (id) ON DELETE RESTRICT,
    reason TEXT NOT NULL,
    note TEXT,
    actor_staff_identity_id UUID NOT NULL
        CONSTRAINT abandoned_checkouts_actor_staff_identity_id_fkey
        REFERENCES staff_identities (id) ON DELETE RESTRICT,
    staff_access_session_id UUID NOT NULL
        CONSTRAINT abandoned_checkouts_staff_access_session_id_fkey
        REFERENCES staff_access_sessions (id) ON DELETE RESTRICT,
    occurred_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT abandoned_checkout_session_unique UNIQUE (service_session_id),
    CONSTRAINT abandoned_checkout_reason_valid CHECK (
        reason IN ('CUSTOMER_LEFT', 'CUSTOMER_REQUEST', 'SYSTEM_FAILURE', 'OTHER')),
    CONSTRAINT abandoned_checkout_note_valid CHECK (reason <> 'OTHER' OR note IS NOT NULL)
);

COMMENT ON TABLE abandoned_checkouts IS
    'Owned by internal/sales (Phase 08). The terminal record of an Abandoned Checkout.';
