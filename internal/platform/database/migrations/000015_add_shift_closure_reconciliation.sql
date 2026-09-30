-- Phase 7: Shift Closure & Reconciliation.
--
-- Reconciliation freezes the Shift's financial facts once per Shift; Cash
-- Counts and QR Observations append to per-reconciliation attempt ledgers;
-- the closure repeats the frozen scalars and stores the three signed
-- differences; discrepancies are one row per nonzero closure dimension. A
-- closed Shift's detail is therefore one immutable aggregate that never
-- depends on recalculating the reconciliation from future financial rows.
--
-- Every evidence foreign key is restrictive: Shift, Staff Identity, and Staff
-- Access Session facts cannot be deleted out from under a reconciliation.
-- Rows are never updated or deleted, no triggers, views, or JSON columns.
--
-- Every statement is rerunnable.
--
-- Monetary bounds: observed values use the non-negative money-input bound
-- (0..2147483647, the MaxAmountVND convention); expected values keep the
-- symmetric bound because a drawer responsibility can be negative; the frozen
-- source sums are each a sum of individually-bounded positive amounts, so the
-- database keeps only their structural non-negativity while Go's guarded
-- arithmetic checks every aggregate addition and every observed-minus-expected
-- subtraction before persistence.

-- The state domain gains CLOSING: reconciliation has started but the Shift
-- has not closed. Opener, Opening Float, and opening time stay immutable, and
-- closure facts are not added to this table.
ALTER TABLE sales_shifts
    DROP CONSTRAINT IF EXISTS sales_shift_state_valid;
ALTER TABLE sales_shifts
    ADD CONSTRAINT sales_shift_state_valid
    CHECK (state IN ('OPEN', 'CLOSING', 'CLOSED'));

-- The sole authority for the one-active-Shift invariant. The constant key
-- turns the index into a full-table partial unique index, which a plain state
-- key could not be: OPEN and CLOSING are different keys, and at most one row
-- may carry either. A Go pre-check may produce a friendlier error, but this
-- index resolves genuine races.
DROP INDEX IF EXISTS sales_shift_only_one_open_unique;
CREATE UNIQUE INDEX IF NOT EXISTS sales_shift_only_one_active_unique
    ON sales_shifts ((true))
    WHERE state IN ('OPEN', 'CLOSING');

COMMENT ON TABLE sales_shifts IS
    'Owned by internal/shift (Phase 4; closure in Phase 7). At most one row may be in OPEN or CLOSING state. Closure facts live in shift_closures, not here.';

CREATE TABLE IF NOT EXISTS shift_reconciliations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sales_shift_id UUID NOT NULL
        CONSTRAINT shift_reconciliations_sales_shift_id_fkey
        REFERENCES sales_shifts (id) ON DELETE RESTRICT,
    started_by_staff_identity_id UUID NOT NULL
        CONSTRAINT shift_reconciliations_started_by_staff_identity_id_fkey
        REFERENCES staff_identities (id) ON DELETE RESTRICT,
    started_staff_access_session_id UUID NOT NULL
        CONSTRAINT shift_reconciliations_started_staff_access_session_id_fkey
        REFERENCES staff_access_sessions (id) ON DELETE RESTRICT,
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- The frozen source facts and the checked expected results they produced.
    -- Source fields are stored separately even where a net field is also
    -- stored, so historical readers can explain the result.
    opening_float_vnd BIGINT NOT NULL,
    pay_in_vnd BIGINT NOT NULL,
    pay_out_vnd BIGINT NOT NULL,
    cash_payment_vnd BIGINT NOT NULL,
    cash_payment_void_vnd BIGINT NOT NULL,
    cash_refund_vnd BIGINT NOT NULL,
    expected_cash_vnd BIGINT NOT NULL,
    manual_qr_payment_vnd BIGINT NOT NULL,
    manual_qr_payment_void_vnd BIGINT NOT NULL,
    expected_manual_qr_received_vnd BIGINT NOT NULL,
    manual_qr_refund_vnd BIGINT NOT NULL,
    -- Closure evidence. Reconciliation may only start when every blocker is
    -- clear, so the columns are born zero and the checks keep them there.
    pending_manual_qr_refund_vnd BIGINT NOT NULL DEFAULT 0,
    pending_refund_vnd BIGINT NOT NULL DEFAULT 0,
    unresolved_post_sale_adjustment_vnd BIGINT NOT NULL DEFAULT 0,
    CONSTRAINT shift_reconciliation_sales_shift_unique UNIQUE (sales_shift_id),
    CONSTRAINT shift_reconciliation_opening_float_vnd_valid
        CHECK (opening_float_vnd >= 0 AND opening_float_vnd <= 2147483647),
    CONSTRAINT shift_reconciliation_pay_in_vnd_valid CHECK (pay_in_vnd >= 0),
    CONSTRAINT shift_reconciliation_pay_out_vnd_valid CHECK (pay_out_vnd >= 0),
    CONSTRAINT shift_reconciliation_cash_payment_vnd_valid CHECK (cash_payment_vnd >= 0),
    CONSTRAINT shift_reconciliation_cash_payment_void_vnd_valid
        CHECK (cash_payment_void_vnd >= 0),
    CONSTRAINT shift_reconciliation_cash_refund_vnd_valid CHECK (cash_refund_vnd >= 0),
    CONSTRAINT shift_reconciliation_expected_cash_vnd_valid
        CHECK (expected_cash_vnd >= -2147483647 AND expected_cash_vnd <= 2147483647),
    CONSTRAINT shift_reconciliation_manual_qr_payment_vnd_valid
        CHECK (manual_qr_payment_vnd >= 0),
    CONSTRAINT shift_reconciliation_manual_qr_payment_void_vnd_valid
        CHECK (manual_qr_payment_void_vnd >= 0),
    CONSTRAINT shift_reconciliation_expected_manual_qr_received_vnd_valid
        CHECK (expected_manual_qr_received_vnd >= -2147483647
               AND expected_manual_qr_received_vnd <= 2147483647),
    CONSTRAINT shift_reconciliation_manual_qr_refund_vnd_valid
        CHECK (manual_qr_refund_vnd >= 0),
    CONSTRAINT shift_reconciliation_pending_manual_qr_refund_vnd_zero
        CHECK (pending_manual_qr_refund_vnd = 0),
    CONSTRAINT shift_reconciliation_pending_refund_vnd_zero
        CHECK (pending_refund_vnd = 0),
    CONSTRAINT shift_reconciliation_unresolved_post_sale_adjustment_vnd_zero
        CHECK (unresolved_post_sale_adjustment_vnd = 0)
);

CREATE TABLE IF NOT EXISTS shift_cash_counts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    reconciliation_id UUID NOT NULL
        CONSTRAINT shift_cash_counts_reconciliation_id_fkey
        REFERENCES shift_reconciliations (id) ON DELETE RESTRICT,
    sequence INT NOT NULL,
    counted_cash_vnd BIGINT NOT NULL,
    counted_by_staff_identity_id UUID NOT NULL
        CONSTRAINT shift_cash_counts_counted_by_staff_identity_id_fkey
        REFERENCES staff_identities (id) ON DELETE RESTRICT,
    counted_staff_access_session_id UUID NOT NULL
        CONSTRAINT shift_cash_counts_counted_staff_access_session_id_fkey
        REFERENCES staff_access_sessions (id) ON DELETE RESTRICT,
    counted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- The unique key is also the ordered-read key: attempts read by sequence.
    CONSTRAINT shift_cash_count_reconciliation_sequence_unique
        UNIQUE (reconciliation_id, sequence),
    CONSTRAINT shift_cash_count_sequence_positive CHECK (sequence > 0),
    CONSTRAINT shift_cash_count_counted_cash_vnd_valid
        CHECK (counted_cash_vnd >= 0 AND counted_cash_vnd <= 2147483647)
);

CREATE TABLE IF NOT EXISTS shift_qr_observations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    reconciliation_id UUID NOT NULL
        CONSTRAINT shift_qr_observations_reconciliation_id_fkey
        REFERENCES shift_reconciliations (id) ON DELETE RESTRICT,
    sequence INT NOT NULL,
    observed_received_vnd BIGINT NOT NULL,
    observed_refunded_vnd BIGINT NOT NULL,
    observed_by_staff_identity_id UUID NOT NULL
        CONSTRAINT shift_qr_observations_observed_by_staff_identity_id_fkey
        REFERENCES staff_identities (id) ON DELETE RESTRICT,
    observed_staff_access_session_id UUID NOT NULL
        CONSTRAINT shift_qr_observations_observed_staff_access_session_id_fkey
        REFERENCES staff_access_sessions (id) ON DELETE RESTRICT,
    observed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT shift_qr_observation_reconciliation_sequence_unique
        UNIQUE (reconciliation_id, sequence),
    CONSTRAINT shift_qr_observation_sequence_positive CHECK (sequence > 0),
    CONSTRAINT shift_qr_observation_observed_received_vnd_valid
        CHECK (observed_received_vnd >= 0 AND observed_received_vnd <= 2147483647),
    CONSTRAINT shift_qr_observation_observed_refunded_vnd_valid
        CHECK (observed_refunded_vnd >= 0 AND observed_refunded_vnd <= 2147483647)
);

CREATE TABLE IF NOT EXISTS shift_closures (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sales_shift_id UUID NOT NULL
        CONSTRAINT shift_closures_sales_shift_id_fkey
        REFERENCES sales_shifts (id) ON DELETE RESTRICT,
    reconciliation_id UUID NOT NULL
        CONSTRAINT shift_closures_reconciliation_id_fkey
        REFERENCES shift_reconciliations (id) ON DELETE RESTRICT,
    -- The final attempts the close was built from. Application validation
    -- requires them to belong to the reconciliation above, because a
    -- cross-table same-parent constraint is not expressible as one ordinary
    -- foreign key.
    initial_cash_count_id UUID NOT NULL
        CONSTRAINT shift_closures_initial_cash_count_id_fkey
        REFERENCES shift_cash_counts (id) ON DELETE RESTRICT,
    final_cash_count_id UUID NOT NULL
        CONSTRAINT shift_closures_final_cash_count_id_fkey
        REFERENCES shift_cash_counts (id) ON DELETE RESTRICT,
    final_qr_observation_id UUID NOT NULL
        CONSTRAINT shift_closures_final_qr_observation_id_fkey
        REFERENCES shift_qr_observations (id) ON DELETE RESTRICT,
    opener_staff_identity_id UUID NOT NULL
        CONSTRAINT shift_closures_opener_staff_identity_id_fkey
        REFERENCES staff_identities (id) ON DELETE RESTRICT,
    closer_staff_identity_id UUID NOT NULL
        CONSTRAINT shift_closures_closer_staff_identity_id_fkey
        REFERENCES staff_identities (id) ON DELETE RESTRICT,
    closer_staff_access_session_id UUID NOT NULL
        CONSTRAINT shift_closures_closer_staff_access_session_id_fkey
        REFERENCES staff_access_sessions (id) ON DELETE RESTRICT,
    approved_by_staff_identity_id UUID
        CONSTRAINT shift_closures_approved_by_staff_identity_id_fkey
        REFERENCES staff_identities (id) ON DELETE RESTRICT,
    opened_at TIMESTAMPTZ NOT NULL,
    closed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- The closure repeats every scalar snapshot from shift_reconciliations
    -- deliberately: the closed detail never recalculates the reconciliation.
    opening_float_vnd BIGINT NOT NULL,
    pay_in_vnd BIGINT NOT NULL,
    pay_out_vnd BIGINT NOT NULL,
    cash_payment_vnd BIGINT NOT NULL,
    cash_payment_void_vnd BIGINT NOT NULL,
    cash_refund_vnd BIGINT NOT NULL,
    expected_cash_vnd BIGINT NOT NULL,
    manual_qr_payment_vnd BIGINT NOT NULL,
    manual_qr_payment_void_vnd BIGINT NOT NULL,
    expected_manual_qr_received_vnd BIGINT NOT NULL,
    manual_qr_refund_vnd BIGINT NOT NULL,
    pending_manual_qr_refund_vnd BIGINT NOT NULL DEFAULT 0,
    pending_refund_vnd BIGINT NOT NULL DEFAULT 0,
    unresolved_post_sale_adjustment_vnd BIGINT NOT NULL DEFAULT 0,
    -- The observed evidence and the signed differences it produced. A positive
    -- difference is an excess; a negative one is a shortage.
    observed_cash_vnd BIGINT NOT NULL,
    observed_manual_qr_received_vnd BIGINT NOT NULL,
    observed_manual_qr_refunded_vnd BIGINT NOT NULL,
    cash_difference_vnd BIGINT NOT NULL,
    manual_qr_received_difference_vnd BIGINT NOT NULL,
    manual_qr_refunded_difference_vnd BIGINT NOT NULL,
    CONSTRAINT shift_closure_sales_shift_unique UNIQUE (sales_shift_id),
    CONSTRAINT shift_closure_reconciliation_unique UNIQUE (reconciliation_id),
    CONSTRAINT shift_closure_close_not_before_open CHECK (closed_at >= opened_at),
    -- A null approver exactly when all three differences are zero; a non-null
    -- approver exactly when any difference is nonzero.
    CONSTRAINT shift_closure_approval_difference_consistent CHECK (
        (cash_difference_vnd = 0 AND manual_qr_received_difference_vnd = 0
         AND manual_qr_refunded_difference_vnd = 0)
        = (approved_by_staff_identity_id IS NULL)
    ),
    CONSTRAINT shift_closure_cash_difference_equation
        CHECK (cash_difference_vnd = observed_cash_vnd - expected_cash_vnd),
    CONSTRAINT shift_closure_manual_qr_received_difference_equation
        CHECK (manual_qr_received_difference_vnd
               = observed_manual_qr_received_vnd - expected_manual_qr_received_vnd),
    -- Expected Manual QR Refunded is the completed refund sum itself, so the
    -- snapshot's manual_qr_refund_vnd is the equation's expected term.
    CONSTRAINT shift_closure_manual_qr_refunded_difference_equation
        CHECK (manual_qr_refunded_difference_vnd
               = observed_manual_qr_refunded_vnd - manual_qr_refund_vnd),
    CONSTRAINT shift_closure_opening_float_vnd_valid
        CHECK (opening_float_vnd >= 0 AND opening_float_vnd <= 2147483647),
    CONSTRAINT shift_closure_pay_in_vnd_valid CHECK (pay_in_vnd >= 0),
    CONSTRAINT shift_closure_pay_out_vnd_valid CHECK (pay_out_vnd >= 0),
    CONSTRAINT shift_closure_cash_payment_vnd_valid CHECK (cash_payment_vnd >= 0),
    CONSTRAINT shift_closure_cash_payment_void_vnd_valid CHECK (cash_payment_void_vnd >= 0),
    CONSTRAINT shift_closure_cash_refund_vnd_valid CHECK (cash_refund_vnd >= 0),
    CONSTRAINT shift_closure_expected_cash_vnd_valid
        CHECK (expected_cash_vnd >= -2147483647 AND expected_cash_vnd <= 2147483647),
    CONSTRAINT shift_closure_manual_qr_payment_vnd_valid CHECK (manual_qr_payment_vnd >= 0),
    CONSTRAINT shift_closure_manual_qr_payment_void_vnd_valid
        CHECK (manual_qr_payment_void_vnd >= 0),
    CONSTRAINT shift_closure_expected_manual_qr_received_vnd_valid
        CHECK (expected_manual_qr_received_vnd >= -2147483647
               AND expected_manual_qr_received_vnd <= 2147483647),
    CONSTRAINT shift_closure_manual_qr_refund_vnd_valid CHECK (manual_qr_refund_vnd >= 0),
    CONSTRAINT shift_closure_pending_manual_qr_refund_vnd_zero
        CHECK (pending_manual_qr_refund_vnd = 0),
    CONSTRAINT shift_closure_pending_refund_vnd_zero
        CHECK (pending_refund_vnd = 0),
    CONSTRAINT shift_closure_unresolved_post_sale_adjustment_vnd_zero
        CHECK (unresolved_post_sale_adjustment_vnd = 0),
    CONSTRAINT shift_closure_observed_cash_vnd_valid
        CHECK (observed_cash_vnd >= 0 AND observed_cash_vnd <= 2147483647),
    CONSTRAINT shift_closure_observed_manual_qr_received_vnd_valid
        CHECK (observed_manual_qr_received_vnd >= 0
               AND observed_manual_qr_received_vnd <= 2147483647),
    CONSTRAINT shift_closure_observed_manual_qr_refunded_vnd_valid
        CHECK (observed_manual_qr_refunded_vnd >= 0
               AND observed_manual_qr_refunded_vnd <= 2147483647)
);

-- History pagination reads closed Shifts newest-first and is stable at equal
-- timestamps because the id breaks the tie.
CREATE INDEX IF NOT EXISTS shift_closures_closed_at_id_desc_index
    ON shift_closures (closed_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS shift_discrepancies (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    shift_closure_id UUID NOT NULL
        CONSTRAINT shift_discrepancies_shift_closure_id_fkey
        REFERENCES shift_closures (id) ON DELETE RESTRICT,
    dimension TEXT NOT NULL,
    expected_vnd BIGINT NOT NULL,
    observed_vnd BIGINT NOT NULL,
    difference_vnd BIGINT NOT NULL,
    reason TEXT NOT NULL,
    note TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Exact dimensions have no row: the row set equals the nonzero
    -- differences in the closure, which application validation enforces.
    CONSTRAINT shift_discrepancy_closure_dimension_unique
        UNIQUE (shift_closure_id, dimension),
    CONSTRAINT shift_discrepancy_dimension_valid
        CHECK (dimension IN ('CASH', 'MANUAL_QR_RECEIVED', 'MANUAL_QR_REFUNDED')),
    CONSTRAINT shift_discrepancy_expected_vnd_valid
        CHECK (expected_vnd >= -2147483647 AND expected_vnd <= 2147483647),
    CONSTRAINT shift_discrepancy_observed_vnd_valid
        CHECK (observed_vnd >= 0 AND observed_vnd <= 2147483647),
    CONSTRAINT shift_discrepancy_difference_nonzero CHECK (difference_vnd <> 0),
    CONSTRAINT shift_discrepancy_difference_equation
        CHECK (difference_vnd = observed_vnd - expected_vnd),
    CONSTRAINT shift_discrepancy_reason_valid
        CHECK (reason IN ('CASH_COUNT_DIFFERENCE', 'QR_OBSERVATION_DIFFERENCE',
                          'UNEXPLAINED', 'OTHER')),
    -- CASH_COUNT_DIFFERENCE explains only a Cash difference;
    -- QR_OBSERVATION_DIFFERENCE explains only a Manual QR difference;
    -- UNEXPLAINED and OTHER explain any dimension.
    CONSTRAINT shift_discrepancy_reason_dimension_valid CHECK (
        (reason = 'CASH_COUNT_DIFFERENCE' AND dimension = 'CASH') OR
        (reason = 'QR_OBSERVATION_DIFFERENCE'
         AND dimension IN ('MANUAL_QR_RECEIVED', 'MANUAL_QR_REFUNDED')) OR
        (reason IN ('UNEXPLAINED', 'OTHER'))
    ),
    CONSTRAINT shift_discrepancy_note_valid
        CHECK (note IS NULL OR (note = btrim(note) AND char_length(note) BETWEEN 1 AND 500)),
    CONSTRAINT shift_discrepancy_other_note_valid
        CHECK (reason <> 'OTHER' OR (note IS NOT NULL AND btrim(note) <> '')),
    -- Notes belong to OTHER only: every other reason stands without one.
    CONSTRAINT shift_discrepancy_non_other_note_absent
        CHECK (reason = 'OTHER' OR note IS NULL)
);
