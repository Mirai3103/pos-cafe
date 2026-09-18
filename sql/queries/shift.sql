-- -- Authority --
-- Names are prefixed because sqlc query names are global across the package.

-- name: GetShiftSessionAuthority :one
SELECT s.id AS session_id, s.staff_identity_id, s.state, s.active_workspace,
       s.last_human_activity_at, s.expires_at, s.revoked_at,
       i.enabled AS identity_enabled, i.display_name, i.login_code
FROM staff_access_sessions s
JOIN staff_identities i ON i.id = s.staff_identity_id
WHERE s.id = $1 AND s.staff_identity_id = $2;

-- name: GetShiftSessionRoles :many
SELECT role
FROM staff_operational_roles
WHERE staff_identity_id = $1
ORDER BY role ASC;

-- name: ShiftAdvisoryLock :exec
SELECT pg_advisory_xact_lock($1);

-- -- Sales Shift --

-- name: OpenSalesShift :one
INSERT INTO sales_shifts (opened_by_staff_identity_id, opening_float_vnd)
VALUES ($1, $2)
RETURNING id, state, opened_by_staff_identity_id, opening_float_vnd, opened_at;

-- name: GetOpenSalesShift :one
SELECT sh.id, sh.state, sh.opening_float_vnd, sh.opened_at,
       i.id AS opener_id,
       i.display_name AS opener_display_name,
       i.login_code AS opener_login_code
FROM sales_shifts sh
JOIN staff_identities i ON i.id = sh.opened_by_staff_identity_id
WHERE sh.state = 'OPEN'
ORDER BY sh.opened_at DESC
LIMIT 1;

-- Single-table so the row lock is unambiguous; the opener is fetched separately
-- with GetStaffSummary.
-- name: GetOpenSalesShiftForUpdate :one
SELECT id, state, opening_float_vnd, opened_at, opened_by_staff_identity_id
FROM sales_shifts
WHERE id = $1 AND state = 'OPEN'
LIMIT 1
FOR UPDATE;

-- name: GetStaffSummary :one
SELECT id, display_name, login_code
FROM staff_identities
WHERE id = $1;

-- -- Cash Movements --

-- name: InsertCashMovement :one
INSERT INTO cash_movements (
    sales_shift_id, method, amount_vnd, reason, note,
    initiated_by_staff_identity_id, initiated_staff_access_session_id,
    approved_by_staff_identity_id
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING id, sales_shift_id, method, amount_vnd, reason, note,
          initiated_by_staff_identity_id, approved_by_staff_identity_id, occurred_at;

-- name: ListCashMovements :many
SELECT cm.id, cm.sales_shift_id, cm.method, cm.amount_vnd, cm.reason, cm.note, cm.occurred_at,
       ini.id AS initiator_id,
       ini.display_name AS initiator_display_name,
       ini.login_code AS initiator_login_code,
       apr.id AS approver_id,
       apr.display_name AS approver_display_name,
       apr.login_code AS approver_login_code
FROM cash_movements cm
JOIN staff_identities ini ON ini.id = cm.initiated_by_staff_identity_id
JOIN staff_identities apr ON apr.id = cm.approved_by_staff_identity_id
WHERE cm.sales_shift_id = $1
ORDER BY cm.occurred_at DESC, cm.id DESC;

-- name: SumCashMovements :one
SELECT
    COALESCE(SUM(amount_vnd) FILTER (WHERE method = 'PAY_IN'), 0)::BIGINT AS pay_in_vnd,
    COALESCE(SUM(amount_vnd) FILTER (WHERE method = 'PAY_OUT'), 0)::BIGINT AS pay_out_vnd
FROM cash_movements
WHERE sales_shift_id = $1;

-- -- Phase 6C: Payment Void, Refund & correction reconciliation --

-- name: GetShiftReconciliationTotals :one
-- Every Phase 6C reconciliation term for one Shift in one read (ADR-046).
-- Cash and Manual QR Payment terms count original applied amounts; a Payment
-- Void removes its source's whole amount; completed Refunds count money out;
-- a pending Manual QR Refund has not moved money yet.
--
-- pending_refund_vnd is money owed back, matching the Check equation in spec
-- 6.1. For every distinct Check carrying a LIVE_CHECK Charge Adjustment
-- attributed to this Shift it sums
--     greatest(valid_payments - completed_live_refunds - corrected_charge, 0)
-- where corrected_charge = base allocations - all LIVE_CHECK adjustments on
-- that Check (from every shift, because they all shape its current charge),
-- valid_payments excludes voided Payments, and completed_live_refunds counts
-- only completed Refunds without a Completed Sale. A Check touched by more
-- than one shift reports its whole live obligation in each affected shift's
-- read; a single shift read is the authoritative view of its own obligations
-- and never over-reports from adjustment capacity. unresolved_post_sale_
-- adjustment_vnd keeps its capacity-based meaning (the POST_SALE adjustment
-- amount not yet covered by completed Refunds) and is included in
-- pending_refund_vnd. internal/shift owns this SQL and imports neither sales
-- nor preparation.
WITH shift AS (
    SELECT sqlc.arg(sales_shift_id)::uuid AS shift_id
),
live_check_obligations AS (
    SELECT DISTINCT ca.check_id
    FROM charge_adjustments AS ca, shift
    WHERE ca.scope = 'LIVE_CHECK'
      AND ca.sales_shift_id = shift.shift_id
),
live_check_financials AS (
    SELECT lc.check_id,
           COALESCE((SELECT SUM(ca.quantity::BIGINT * ci.unit_price_vnd)
                     FROM charge_allocations AS ca
                     JOIN committed_items AS ci ON ci.id = ca.committed_item_id
                     WHERE ca.check_id = lc.check_id), 0)::BIGINT
               AS base_charge_vnd,
           COALESCE((SELECT SUM(ca.amount_vnd)
                     FROM charge_adjustments AS ca
                     WHERE ca.check_id = lc.check_id
                       AND ca.scope = 'LIVE_CHECK'), 0)::BIGINT
               AS live_adjustment_vnd,
           COALESCE((SELECT SUM(p.applied_amount_vnd)
                     FROM payments AS p
                     WHERE p.check_id = lc.check_id
                       AND NOT EXISTS (SELECT 1
                                       FROM payment_voids AS pv
                                       WHERE pv.payment_id = p.id)), 0)::BIGINT
               AS valid_payment_vnd,
           COALESCE((SELECT SUM(r.amount_vnd)
                     FROM refunds AS r
                     JOIN refund_completions AS rc ON rc.refund_id = r.id
                     WHERE r.check_id = lc.check_id
                       AND r.completed_sale_id IS NULL), 0)::BIGINT
               AS completed_live_refund_vnd
    FROM live_check_obligations AS lc
),
post_sale AS (
    SELECT (COALESCE((SELECT SUM(ca.amount_vnd)
                      FROM charge_adjustments AS ca
                      WHERE ca.sales_shift_id = (SELECT shift_id FROM shift)
                        AND ca.scope = 'POST_SALE'), 0)
            - COALESCE((SELECT SUM(raa.amount_vnd)
                        FROM refund_adjustment_allocations AS raa
                        JOIN refunds AS r ON r.id = raa.refund_id
                        JOIN refund_completions AS rc ON rc.refund_id = r.id
                        JOIN charge_adjustments AS ca2
                          ON ca2.id = raa.charge_adjustment_id
                        WHERE ca2.sales_shift_id = (SELECT shift_id FROM shift)
                          AND ca2.scope = 'POST_SALE'), 0))::BIGINT
               AS unresolved_post_sale_adjustment_vnd
)
SELECT
    (SELECT COALESCE(SUM(p.applied_amount_vnd), 0)::BIGINT
     FROM payments AS p, shift
     WHERE p.sales_shift_id = shift.shift_id
       AND p.method = 'CASH') AS cash_payment_vnd,
    (SELECT COALESCE(SUM(p.applied_amount_vnd), 0)::BIGINT
     FROM payments AS p
     JOIN payment_voids AS pv ON pv.payment_id = p.id, shift
     WHERE p.sales_shift_id = shift.shift_id
       AND p.method = 'CASH') AS cash_payment_void_vnd,
    (SELECT COALESCE(SUM(r.amount_vnd), 0)::BIGINT
     FROM refunds AS r
     JOIN refund_completions AS rc ON rc.refund_id = r.id, shift
     WHERE r.sales_shift_id = shift.shift_id
       AND r.method = 'CASH') AS cash_refund_vnd,
    (SELECT COALESCE(SUM(p.applied_amount_vnd), 0)::BIGINT
     FROM payments AS p, shift
     WHERE p.sales_shift_id = shift.shift_id
       AND p.method = 'MANUAL_QR') AS manual_qr_payment_vnd,
    (SELECT COALESCE(SUM(p.applied_amount_vnd), 0)::BIGINT
     FROM payments AS p
     JOIN payment_voids AS pv ON pv.payment_id = p.id, shift
     WHERE p.sales_shift_id = shift.shift_id
       AND p.method = 'MANUAL_QR') AS manual_qr_payment_void_vnd,
    (SELECT COALESCE(SUM(r.amount_vnd), 0)::BIGINT
     FROM refunds AS r
     JOIN refund_completions AS rc ON rc.refund_id = r.id, shift
     WHERE r.sales_shift_id = shift.shift_id
       AND r.method = 'MANUAL_QR') AS manual_qr_refund_vnd,
    (SELECT COALESCE(SUM(r.amount_vnd), 0)::BIGINT
     FROM refunds AS r
     LEFT JOIN refund_completions AS rc ON rc.refund_id = r.id, shift
     WHERE r.sales_shift_id = shift.shift_id
       AND r.method = 'MANUAL_QR'
       AND rc.id IS NULL) AS pending_manual_qr_refund_vnd,
    (((SELECT COALESCE(SUM(GREATEST(cf.valid_payment_vnd
                                    - cf.completed_live_refund_vnd
                                    - (cf.base_charge_vnd - cf.live_adjustment_vnd),
                                    0)), 0)::BIGINT
       FROM live_check_financials AS cf)
      + (SELECT unresolved_post_sale_adjustment_vnd FROM post_sale)))::BIGINT
         AS pending_refund_vnd,
    (SELECT unresolved_post_sale_adjustment_vnd FROM post_sale)
         AS unresolved_post_sale_adjustment_vnd;

-- name: ListShiftRefunds :many
-- The Shift response's Refund summaries ordered by (created_at, id), each
-- carrying derived completion state and no credentials.
SELECT r.id, r.check_id, r.completed_sale_id, r.method, r.amount_vnd,
       r.reason, r.note, r.created_at,
       CASE WHEN rc.id IS NOT NULL THEN true ELSE false END AS completed,
       rc.completed_at, rc.transaction_reference
FROM refunds AS r
LEFT JOIN refund_completions AS rc ON rc.refund_id = r.id
WHERE r.sales_shift_id = $1
ORDER BY r.created_at ASC, r.id ASC;

-- -- Phase 7: Shift Closure & Reconciliation --
--
-- Reconciliation freezes the Shift's financial facts once per Shift; Cash
-- Counts and QR Observations append to per-reconciliation attempt ledgers;
-- the closure repeats the frozen scalars. Reads join staff_identities for
-- display names, which stay identity projection labels, never copied facts.

-- name: LockSalesShiftForReconciliation :one
-- The Shift row FOR UPDATE for Start Reconciliation and Final Close. The state
-- is returned rather than filtered so an unknown Shift, an OPEN Shift, and a
-- CLOSING one map to their own errors instead of collapsing into one missing
-- row; the caller branches on it.
SELECT id, state, opened_by_staff_identity_id, opening_float_vnd, opened_at
FROM sales_shifts
WHERE id = $1
LIMIT 1
FOR UPDATE;

-- name: TransitionSalesShiftToClosing :exec
-- The caller locks the Shift with LockSalesShiftForReconciliation and verifies
-- the OPEN state first, so no guard is repeated here.
UPDATE sales_shifts SET state = 'CLOSING' WHERE id = $1;

-- name: TransitionSalesShiftToClosed :exec
UPDATE sales_shifts SET state = 'CLOSED' WHERE id = $1;

-- name: InsertShiftReconciliation :one
-- The immutable per-Shift snapshot. The three blocker-evidence columns are
-- omitted: reconciliation may only start when every global blocker is clear,
-- so they are born at their CHECK-enforced zero.
INSERT INTO shift_reconciliations (
    sales_shift_id, started_by_staff_identity_id, started_staff_access_session_id,
    opening_float_vnd, pay_in_vnd, pay_out_vnd,
    cash_payment_vnd, cash_payment_void_vnd, cash_refund_vnd, expected_cash_vnd,
    manual_qr_payment_vnd, manual_qr_payment_void_vnd, expected_manual_qr_received_vnd,
    manual_qr_refund_vnd
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
RETURNING id, started_at;

-- name: InsertShiftCashCount :one
-- One append-only Cash Count attempt; counted_at comes from the database
-- clock and is returned with the id.
INSERT INTO shift_cash_counts (
    reconciliation_id, sequence, counted_cash_vnd,
    counted_by_staff_identity_id, counted_staff_access_session_id
) VALUES ($1, $2, $3, $4, $5)
RETURNING id, sequence, counted_cash_vnd, counted_at;

-- name: InsertShiftQRObservation :one
-- One append-only Manual QR observation attempt; both observed values are
-- explicit, including zero.
INSERT INTO shift_qr_observations (
    reconciliation_id, sequence, observed_received_vnd, observed_refunded_vnd,
    observed_by_staff_identity_id, observed_staff_access_session_id
) VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, sequence, observed_received_vnd, observed_refunded_vnd, observed_at;

-- name: GetGlobalShiftClosureBlockers :one
-- Every closure blocker for the whole cafe in one row (spec 8), so Go chooses
-- the first public error by precedence without a race between separate reads.
-- Deliberately distinct from GetShiftReconciliationTotals: no Shift filter.
-- The unresolved-correction amount is defined independently of Shift
-- attribution: for every Check carrying any LIVE_CHECK Charge Adjustment it
-- sums greatest(valid non-voided Payments - completed live Refunds
-- - (base Charge Allocations - all LIVE_CHECK adjustments on that Check), 0),
-- plus, for every POST_SALE Charge Adjustment, greatest(its amount - its
-- completed Refund allocations, 0). It reuses GetShiftReconciliationTotals'
-- conventions: valid Payments exclude voided ones, completed live Refunds are
-- completed Refunds without a Completed Sale, and base charge comes from the
-- Charge Allocations' frozen unit prices.
WITH live_check_obligations AS (
    SELECT DISTINCT ca.check_id
    FROM charge_adjustments AS ca
    WHERE ca.scope = 'LIVE_CHECK'
),
live_check_financials AS (
    SELECT lc.check_id,
           COALESCE((SELECT SUM(ca.quantity::BIGINT * ci.unit_price_vnd)
                     FROM charge_allocations AS ca
                     JOIN committed_items AS ci ON ci.id = ca.committed_item_id
                     WHERE ca.check_id = lc.check_id), 0)::BIGINT
               AS base_charge_vnd,
           COALESCE((SELECT SUM(ca.amount_vnd)
                     FROM charge_adjustments AS ca
                     WHERE ca.check_id = lc.check_id
                       AND ca.scope = 'LIVE_CHECK'), 0)::BIGINT
               AS live_adjustment_vnd,
           COALESCE((SELECT SUM(p.applied_amount_vnd)
                     FROM payments AS p
                     WHERE p.check_id = lc.check_id
                       AND NOT EXISTS (SELECT 1
                                       FROM payment_voids AS pv
                                       WHERE pv.payment_id = p.id)), 0)::BIGINT
               AS valid_payment_vnd,
           COALESCE((SELECT SUM(r.amount_vnd)
                     FROM refunds AS r
                     JOIN refund_completions AS rc ON rc.refund_id = r.id
                     WHERE r.check_id = lc.check_id
                       AND r.completed_sale_id IS NULL), 0)::BIGINT
               AS completed_live_refund_vnd
    FROM live_check_obligations AS lc
),
post_sale AS (
    SELECT COALESCE(SUM(GREATEST(
               ca.amount_vnd
               - COALESCE((SELECT SUM(raa.amount_vnd)
                           FROM refund_adjustment_allocations AS raa
                           JOIN refunds AS r ON r.id = raa.refund_id
                           JOIN refund_completions AS rc ON rc.refund_id = r.id
                           WHERE raa.charge_adjustment_id = ca.id), 0),
               0)), 0)::BIGINT
               AS unresolved_post_sale_adjustment_vnd
    FROM charge_adjustments AS ca
    WHERE ca.scope = 'POST_SALE'
)
SELECT
    (SELECT count(*) FROM checks WHERE state = 'OPEN')::BIGINT
        AS unsettled_check_count,
    -- A pending Refund intent is one lacking its unique completion row.
    (SELECT count(*)
     FROM refunds AS r
     WHERE NOT EXISTS (SELECT 1 FROM refund_completions AS rc
                       WHERE rc.refund_id = r.id))::BIGINT
        AS pending_refund_count,
    (((SELECT COALESCE(SUM(GREATEST(cf.valid_payment_vnd
                                    - cf.completed_live_refund_vnd
                                    - (cf.base_charge_vnd - cf.live_adjustment_vnd),
                                    0)), 0)::BIGINT
       FROM live_check_financials AS cf)
      + (SELECT unresolved_post_sale_adjustment_vnd FROM post_sale)))::BIGINT
        AS unresolved_correction_vnd,
    (SELECT count(*) FROM service_sessions WHERE state = 'ACTIVE')::BIGINT
        AS active_service_session_count;

-- name: GetReconciliationSnapshot :one
-- The frozen per-Shift reconciliation, read back for the CLOSING response and
-- re-verified against fresh totals at Final Close.
SELECT id, sales_shift_id, started_by_staff_identity_id,
       started_staff_access_session_id, started_at,
       opening_float_vnd, pay_in_vnd, pay_out_vnd,
       cash_payment_vnd, cash_payment_void_vnd, cash_refund_vnd, expected_cash_vnd,
       manual_qr_payment_vnd, manual_qr_payment_void_vnd,
       expected_manual_qr_received_vnd, manual_qr_refund_vnd,
       pending_manual_qr_refund_vnd, pending_refund_vnd,
       unresolved_post_sale_adjustment_vnd
FROM shift_reconciliations
WHERE sales_shift_id = $1;

-- name: GetLatestReconciliationEvidence :one
-- The final attempt of each ledger in one read. The QR side is nullable
-- because a reconciliation may not have an observation yet; the Cash side is
-- never empty while a reconciliation exists, because Start inserts sequence 1.
-- sqlc's analyzer calls the bare reconciliation_id reference ambiguous across
-- the two CTEs, so each WHERE spells its table name out.
WITH latest_cash AS (
    SELECT id, sequence, counted_cash_vnd, counted_at
    FROM shift_cash_counts
    WHERE shift_cash_counts.reconciliation_id = $1
    ORDER BY sequence DESC, id DESC
    LIMIT 1
),
latest_qr AS (
    SELECT id, sequence, observed_received_vnd, observed_refunded_vnd, observed_at
    FROM shift_qr_observations
    WHERE shift_qr_observations.reconciliation_id = $1
    ORDER BY sequence DESC, id DESC
    LIMIT 1
)
SELECT lc.id AS cash_count_id, lc.sequence AS cash_count_sequence,
       lc.counted_cash_vnd, lc.counted_at,
       lq.id AS qr_observation_id, lq.sequence AS qr_observation_sequence,
       lq.observed_received_vnd, lq.observed_refunded_vnd, lq.observed_at
FROM latest_cash AS lc
LEFT JOIN latest_qr AS lq ON true;

-- name: InsertShiftClosure :one
-- The immutable closure aggregate. opened_at repeats the Shift's immutable
-- fact; closed_at comes from the database clock and is returned with the id.
INSERT INTO shift_closures (
    sales_shift_id, reconciliation_id,
    initial_cash_count_id, final_cash_count_id, final_qr_observation_id,
    opener_staff_identity_id, closer_staff_identity_id, closer_staff_access_session_id,
    approved_by_staff_identity_id, opened_at,
    opening_float_vnd, pay_in_vnd, pay_out_vnd,
    cash_payment_vnd, cash_payment_void_vnd, cash_refund_vnd, expected_cash_vnd,
    manual_qr_payment_vnd, manual_qr_payment_void_vnd, expected_manual_qr_received_vnd,
    manual_qr_refund_vnd,
    observed_cash_vnd, observed_manual_qr_received_vnd, observed_manual_qr_refunded_vnd,
    cash_difference_vnd, manual_qr_received_difference_vnd, manual_qr_refunded_difference_vnd
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17,
          $18, $19, $20, $21, $22, $23, $24, $25, $26, $27)
RETURNING id, closed_at;

-- name: InsertShiftDiscrepancies :exec
-- The close command's whole discrepancy set in one statement: one row per
-- nonzero closure dimension, at most three, so the rows and the closure commit
-- together. Notes arrive as a plain text array because a database/sql array
-- parameter cannot carry NULL elements: an empty string means no note, and
-- NULLIF(btrim(note), '') turns it into SQL NULL before the reason/note
-- checks see it.
INSERT INTO shift_discrepancies (
    shift_closure_id, dimension, expected_vnd, observed_vnd, difference_vnd, reason, note
)
SELECT sqlc.arg(shift_closure_id)::uuid,
       dims.dimension, exps.expected_vnd, obss.observed_vnd, diffs.difference_vnd,
       rsn.reason,
       NULLIF(btrim(nts.note), '')
FROM unnest(sqlc.arg(dimensions)::text[]) WITH ORDINALITY AS dims(dimension, ord)
JOIN unnest(sqlc.arg(expecteds)::bigint[]) WITH ORDINALITY AS exps(expected_vnd, ord)
  ON exps.ord = dims.ord
JOIN unnest(sqlc.arg(observeds)::bigint[]) WITH ORDINALITY AS obss(observed_vnd, ord)
  ON obss.ord = dims.ord
JOIN unnest(sqlc.arg(differences)::bigint[]) WITH ORDINALITY AS diffs(difference_vnd, ord)
  ON diffs.ord = dims.ord
JOIN unnest(sqlc.arg(reasons)::text[]) WITH ORDINALITY AS rsn(reason, ord)
  ON rsn.ord = dims.ord
JOIN unnest(sqlc.arg(notes)::text[]) WITH ORDINALITY AS nts(note, ord)
  ON nts.ord = dims.ord;

-- name: ListShiftCashCounts :many
-- One reconciliation's append-only Cash Counts, oldest first.
SELECT id, sequence, counted_cash_vnd,
       counted_by_staff_identity_id, counted_staff_access_session_id, counted_at
FROM shift_cash_counts
WHERE reconciliation_id = $1
ORDER BY sequence ASC, id ASC;

-- name: ListShiftQRObservations :many
-- One reconciliation's append-only QR Observations, oldest first.
SELECT id, sequence, observed_received_vnd, observed_refunded_vnd,
       observed_by_staff_identity_id, observed_staff_access_session_id, observed_at
FROM shift_qr_observations
WHERE reconciliation_id = $1
ORDER BY sequence ASC, id ASC;

-- name: ListShiftClosureDiscrepancies :many
-- One closure's nonzero discrepancy rows; exact dimensions have no row.
SELECT id, shift_closure_id, dimension, expected_vnd, observed_vnd,
       difference_vnd, reason, note, created_at
FROM shift_discrepancies
WHERE shift_closure_id = $1
ORDER BY dimension ASC;

-- name: ListClosedShiftSummaries :many
-- Closed-Shift history, ordered by (closed_at DESC, id DESC) per the history
-- index. The window is [closed_from, closed_to). The exclusive cursor is the
-- last (closed_at, id) of the previous page; the caller passes both cursor
-- values or neither and validates that pairing.
SELECT c.sales_shift_id, c.id AS closure_id, c.opened_at, c.closed_at,
       opener.id AS opener_staff_identity_id,
       opener.display_name AS opener_display_name,
       opener.login_code AS opener_login_code,
       closer.id AS closer_staff_identity_id,
       closer.display_name AS closer_display_name,
       closer.login_code AS closer_login_code,
       c.opening_float_vnd,
       c.expected_cash_vnd, c.observed_cash_vnd, c.cash_difference_vnd,
       c.expected_manual_qr_received_vnd, c.observed_manual_qr_received_vnd,
       c.manual_qr_received_difference_vnd,
       -- The expected refunded amount is the completed refund sum itself, so
       -- the snapshot's manual_qr_refund_vnd doubles as the equation's
       -- expected term under its response name.
       c.manual_qr_refund_vnd AS expected_manual_qr_refunded_vnd,
       c.observed_manual_qr_refunded_vnd,
       c.manual_qr_refunded_difference_vnd,
       (c.cash_difference_vnd <> 0
        OR c.manual_qr_received_difference_vnd <> 0
        OR c.manual_qr_refunded_difference_vnd <> 0) AS has_discrepancy
FROM shift_closures AS c
JOIN staff_identities AS opener ON opener.id = c.opener_staff_identity_id
JOIN staff_identities AS closer ON closer.id = c.closer_staff_identity_id
WHERE c.closed_at >= sqlc.arg(closed_from)::timestamptz
  AND c.closed_at < sqlc.arg(closed_to)::timestamptz
  AND (sqlc.narg(cursor_closed_at)::timestamptz IS NULL
       OR (c.closed_at, c.id) < (sqlc.narg(cursor_closed_at)::timestamptz,
                                 sqlc.narg(cursor_id)::uuid))
ORDER BY c.closed_at DESC, c.id DESC
LIMIT sqlc.arg(row_limit)::int;

-- name: GetClosedShiftDetail :one
-- One closed Shift's immutable detail aggregate, keyed by the Shift id the
-- history route exposes. An open or closing Shift id finds no row here. The
-- closure repeats every frozen scalar, so the detail never recalculates the
-- reconciliation; the caller reads the attempt ledgers and the discrepancy
-- rows through their own queries.
SELECT c.id AS closure_id, c.sales_shift_id, c.reconciliation_id,
       c.initial_cash_count_id, c.final_cash_count_id, c.final_qr_observation_id,
       c.opened_at, c.closed_at,
       opener.id AS opener_staff_identity_id,
       opener.display_name AS opener_display_name,
       opener.login_code AS opener_login_code,
       closer.id AS closer_staff_identity_id,
       closer.display_name AS closer_display_name,
       closer.login_code AS closer_login_code,
       approver.id AS approved_by_staff_identity_id,
       approver.display_name AS approved_by_display_name,
       approver.login_code AS approved_by_login_code,
       r.started_at,
       starter.id AS started_by_staff_identity_id,
       starter.display_name AS starter_display_name,
       starter.login_code AS starter_login_code,
       c.opening_float_vnd, c.pay_in_vnd, c.pay_out_vnd,
       c.cash_payment_vnd, c.cash_payment_void_vnd, c.cash_refund_vnd,
       c.expected_cash_vnd,
       c.manual_qr_payment_vnd, c.manual_qr_payment_void_vnd,
       c.expected_manual_qr_received_vnd, c.manual_qr_refund_vnd,
       c.pending_manual_qr_refund_vnd, c.pending_refund_vnd,
       c.unresolved_post_sale_adjustment_vnd,
       c.observed_cash_vnd, c.observed_manual_qr_received_vnd,
       c.observed_manual_qr_refunded_vnd,
       c.cash_difference_vnd, c.manual_qr_received_difference_vnd,
       c.manual_qr_refunded_difference_vnd
FROM shift_closures AS c
JOIN shift_reconciliations AS r ON r.id = c.reconciliation_id
JOIN staff_identities AS opener ON opener.id = c.opener_staff_identity_id
JOIN staff_identities AS closer ON closer.id = c.closer_staff_identity_id
JOIN staff_identities AS starter ON starter.id = r.started_by_staff_identity_id
LEFT JOIN staff_identities AS approver ON approver.id = c.approved_by_staff_identity_id
WHERE c.sales_shift_id = $1;
