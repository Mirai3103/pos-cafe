-- Phase 5C: Payments, Settlement & Check Restructuring.
--
-- Money arrives here. A Payment is applied to a Check; when the balance
-- reaches zero the Check settles in the same transaction, recording who
-- settled it, during which Sales Shift, and on which access session.
-- Refund, Payment Void, and Comp are outside Phase 5.

-- The five settlement columns ADR-014 deferred from migration 000009.
ALTER TABLE checks
    ADD COLUMN IF NOT EXISTS merged_into_check_id            UUID REFERENCES checks(id) ON DELETE RESTRICT,
    ADD COLUMN IF NOT EXISTS settled_at                      TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS settled_by_staff_identity_id    UUID REFERENCES staff_identities(id) ON DELETE RESTRICT,
    ADD COLUMN IF NOT EXISTS settled_during_sales_shift_id   UUID REFERENCES sales_shifts(id) ON DELETE RESTRICT,
    ADD COLUMN IF NOT EXISTS settled_staff_access_session_id UUID REFERENCES staff_access_sessions(id) ON DELETE RESTRICT;

-- Ties the evidence to the state, so a Check that is settled but does not
-- know who settled it is unrepresentable. Settlement evidence is recorded
-- along all four dimensions because this is cash-reconciliation data.
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
);

-- sales_shift_id is stored rather than derived through the Session, because
-- the Shift in which the money reached the cashier is an independent fact:
-- a Session opened in one Shift can be paid in the next. See ADR-019.
--
-- There is deliberately no receipt_observed_in_bank_app column. It is a
-- must-be-true attestation, recorded in the request and the audit event; a
-- column that is true on every row stores nothing.
CREATE TABLE IF NOT EXISTS payments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    check_id UUID NOT NULL REFERENCES checks(id) ON DELETE RESTRICT,
    sales_shift_id UUID NOT NULL REFERENCES sales_shifts(id) ON DELETE RESTRICT,
    actor_staff_identity_id UUID NOT NULL REFERENCES staff_identities(id) ON DELETE RESTRICT,
    staff_access_session_id UUID NOT NULL REFERENCES staff_access_sessions(id) ON DELETE RESTRICT,
    applied_amount_vnd BIGINT NOT NULL,
    method TEXT NOT NULL,
    cash_tendered_vnd BIGINT,
    change_due_vnd BIGINT,
    transaction_reference TEXT,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT payment_method_valid
        CHECK (method IN ('CASH', 'MANUAL_QR')),
    CONSTRAINT payment_method_facts_valid CHECK (
           (method = 'CASH'
                AND cash_tendered_vnd IS NOT NULL
                AND change_due_vnd IS NOT NULL
                AND transaction_reference IS NULL
                AND applied_amount_vnd > 0
                AND cash_tendered_vnd >= applied_amount_vnd
                AND change_due_vnd = cash_tendered_vnd - applied_amount_vnd)
        OR (method = 'MANUAL_QR'
                AND cash_tendered_vnd IS NULL
                AND change_due_vnd IS NULL
                AND applied_amount_vnd > 0
                AND (transaction_reference IS NULL
                     OR (transaction_reference = btrim(transaction_reference)
                         AND char_length(transaction_reference) BETWEEN 1 AND 100)))
    )
);

-- Serves both grouping Payments by Check and their presentation order.
CREATE INDEX IF NOT EXISTS payment_check_index
    ON payments (check_id, received_at, id);

-- Serves internal/shift's Expected Cash sum directly.
CREATE INDEX IF NOT EXISTS payment_cash_shift_index
    ON payments (sales_shift_id) WHERE method = 'CASH';

COMMENT ON TABLE payments IS
    'Owned by internal/sales. Immutable after insert: no phase updates a row. Read by internal/shift for Expected Cash.';
