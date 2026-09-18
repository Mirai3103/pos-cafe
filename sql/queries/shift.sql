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

-- name: SumCashPaymentsForShift :one
-- Expected Cash's Cash Payment term (ADR-020). The sum is over APPLIED
-- amounts, not tendered amounts: CONTEXT.md defines a Cash Payment's net cash
-- effect as the applied amount, because the change left the drawer at the same
-- moment the tendered cash entered it.
--
-- internal/shift reads the payments table through its own query rather than
-- importing internal/sales, following ADR-012.
SELECT COALESCE(SUM(applied_amount_vnd) FILTER (WHERE method = 'CASH'), 0)::BIGINT
    AS cash_payment_vnd
FROM payments
WHERE sales_shift_id = $1;

-- -- Phase 6C: Payment Void, Refund & correction reconciliation --

-- name: GetShiftReconciliationTotals :one
-- Every Phase 6C reconciliation term for one Shift in one read (ADR-046).
-- Cash and Manual QR Payment terms count original applied amounts; a Payment
-- Void removes its source's whole amount; completed Refunds count money out;
-- a pending Manual QR Refund has not moved money yet. pending_refund_vnd is
-- the unresolved corrected capacity of every Charge Adjustment attributed to
-- the Shift (its full amount less the Refund allocations already completed
-- against it, pending intents included); unresolved_post_sale_adjustment_vnd
-- is the POST_SALE subset. internal/shift owns this SQL and imports neither
-- sales nor preparation.
SELECT
    (SELECT COALESCE(SUM(p.applied_amount_vnd), 0)::BIGINT
     FROM payments AS p
     WHERE p.sales_shift_id = $1
       AND p.method = 'CASH') AS cash_payment_vnd,
    (SELECT COALESCE(SUM(p.applied_amount_vnd), 0)::BIGINT
     FROM payments AS p
     JOIN payment_voids AS pv ON pv.payment_id = p.id
     WHERE p.sales_shift_id = $1
       AND p.method = 'CASH') AS cash_payment_void_vnd,
    (SELECT COALESCE(SUM(r.amount_vnd), 0)::BIGINT
     FROM refunds AS r
     JOIN refund_completions AS rc ON rc.refund_id = r.id
     WHERE r.sales_shift_id = $1
       AND r.method = 'CASH') AS cash_refund_vnd,
    (SELECT COALESCE(SUM(p.applied_amount_vnd), 0)::BIGINT
     FROM payments AS p
     WHERE p.sales_shift_id = $1
       AND p.method = 'MANUAL_QR') AS manual_qr_payment_vnd,
    (SELECT COALESCE(SUM(p.applied_amount_vnd), 0)::BIGINT
     FROM payments AS p
     JOIN payment_voids AS pv ON pv.payment_id = p.id
     WHERE p.sales_shift_id = $1
       AND p.method = 'MANUAL_QR') AS manual_qr_payment_void_vnd,
    (SELECT COALESCE(SUM(r.amount_vnd), 0)::BIGINT
     FROM refunds AS r
     JOIN refund_completions AS rc ON rc.refund_id = r.id
     WHERE r.sales_shift_id = $1
       AND r.method = 'MANUAL_QR') AS manual_qr_refund_vnd,
    (SELECT COALESCE(SUM(r.amount_vnd), 0)::BIGINT
     FROM refunds AS r
     LEFT JOIN refund_completions AS rc ON rc.refund_id = r.id
     WHERE r.sales_shift_id = $1
       AND r.method = 'MANUAL_QR'
       AND rc.id IS NULL) AS pending_manual_qr_refund_vnd,
    (SELECT (COALESCE(SUM(ca.amount_vnd), 0)
             - COALESCE((SELECT SUM(raa.amount_vnd)
                         FROM refund_adjustment_allocations AS raa
                         JOIN refunds AS r ON r.id = raa.refund_id
                         JOIN refund_completions AS rc ON rc.refund_id = r.id
                         JOIN charge_adjustments AS ca2
                           ON ca2.id = raa.charge_adjustment_id
                         WHERE ca2.sales_shift_id = $1), 0))::BIGINT
     FROM charge_adjustments AS ca
     WHERE ca.sales_shift_id = $1) AS pending_refund_vnd,
    (SELECT (COALESCE(SUM(ca.amount_vnd), 0)
             - COALESCE((SELECT SUM(raa.amount_vnd)
                         FROM refund_adjustment_allocations AS raa
                         JOIN refunds AS r ON r.id = raa.refund_id
                         JOIN refund_completions AS rc ON rc.refund_id = r.id
                         JOIN charge_adjustments AS ca2
                           ON ca2.id = raa.charge_adjustment_id
                         WHERE ca2.sales_shift_id = $1
                           AND ca2.scope = 'POST_SALE'), 0))::BIGINT
     FROM charge_adjustments AS ca
     WHERE ca.sales_shift_id = $1
       AND ca.scope = 'POST_SALE') AS unresolved_post_sale_adjustment_vnd;

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
