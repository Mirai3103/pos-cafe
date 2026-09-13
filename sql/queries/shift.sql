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
