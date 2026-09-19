//go:build integration

package shift_test

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Seeding infrastructure for the reconciliation tests, recovered from the
// deleted internal/shift/expected_cash_integration_test.go (commit 4c28d04)
// and adapted. internal/sales owns service_sessions, checks, payments, and
// refunds; seeding rows another slice owns with raw SQL follows the precedent
// Phase 3's tests set when they seeded service_sessions before internal/sales
// owned them. No money assertions live here — Task 1's redaction of this
// file's Expected Cash assertions is intentional.

// testServiceNumber draws a service_number matching ^[A-Z0-9]{6}$ from a fresh
// UUID. Uniqueness is only required among one Shift's Sessions (ADR-011), and
// every env seeds one Session against a fresh Shift, but a random value keeps
// the fixture independent of that invariant.
func testServiceNumber() string {
	return strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", ""))[:6]
}

// seedActiveServiceSession inserts one ACTIVE Service Session with direct SQL
// and returns its id. It is the minimal active-Session closure blocker.
func seedActiveServiceSession(t *testing.T, db *sql.DB, shiftID, actorID uuid.UUID) uuid.UUID {
	t.Helper()

	var sessionID uuid.UUID
	require.NoError(t, db.QueryRow(
		`INSERT INTO service_sessions (service_number, sequence, sales_shift_id, created_by_staff_identity_id)
		 VALUES ($1,
		         (SELECT COALESCE(MAX(sequence), 0) + 1
		          FROM service_sessions WHERE sales_shift_id = $2),
		         $2, $3)
		 RETURNING id`,
		testServiceNumber(), shiftID, actorID).Scan(&sessionID))
	return sessionID
}

// seedShiftEnvCheck inserts one ACTIVE Service Session plus one OPEN Check
// with direct SQL and returns the Check's id. The Session is created against
// the named Sales Shift, which service_sessions.sales_shift_id (NOT NULL since
// Phase 5A) requires. This is exactly the unsettled-Check blocker shape.
func seedShiftEnvCheck(t *testing.T, db *sql.DB, shiftID, actorID uuid.UUID) uuid.UUID {
	t.Helper()

	var sessionID uuid.UUID
	require.NoError(t, db.QueryRow(
		`INSERT INTO service_sessions (service_number, sequence, sales_shift_id, created_by_staff_identity_id)
		 VALUES ($1,
		         (SELECT COALESCE(MAX(sequence), 0) + 1
		          FROM service_sessions WHERE sales_shift_id = $2),
		         $2, $3)
		 RETURNING id`,
		testServiceNumber(), shiftID, actorID).Scan(&sessionID))

	var checkID uuid.UUID
	require.NoError(t, db.QueryRow(
		`INSERT INTO checks (service_session_id) VALUES ($1) RETURNING id`,
		sessionID).Scan(&checkID))
	return checkID
}

// seedSettledCheck inserts one CLOSED Service Session carrying one SETTLED
// Check (with the four settlement-evidence columns the
// check_settlement_evidence_valid constraint demands) and returns the Check's
// id. Payments and Refunds need a Check to attach to, and a settled Check on a
// closed Session trips no closure blocker: the unsettled-Check blocker reads
// only OPEN Checks and the active-Session blocker only ACTIVE Sessions.
func seedSettledCheck(t *testing.T, db *sql.DB, shiftID, actorID, sessionID uuid.UUID) uuid.UUID {
	t.Helper()

	var createdSession uuid.UUID
	require.NoError(t, db.QueryRow(
		`INSERT INTO service_sessions (service_number, sequence, state, sales_shift_id, created_by_staff_identity_id)
		 VALUES ($1,
		         (SELECT COALESCE(MAX(sequence), 0) + 1
		          FROM service_sessions WHERE sales_shift_id = $2),
		         'CLOSED', $2, $3)
		 RETURNING id`,
		testServiceNumber(), shiftID, actorID).Scan(&createdSession))

	var checkID uuid.UUID
	require.NoError(t, db.QueryRow(`
		INSERT INTO checks (service_session_id, state, settled_at,
		                    settled_by_staff_identity_id, settled_during_sales_shift_id,
		                    settled_staff_access_session_id)
		VALUES ($1, 'SETTLED', now(), $2, $3, $4) RETURNING id`,
		createdSession, actorID, shiftID, sessionID).Scan(&checkID))
	return checkID
}

// settleCheck moves one Check to SETTLED with the evidence the database
// constraint requires, so a correction chain stops blocking as an unsettled
// Check without losing the rows the correction queries read.
func settleCheck(t *testing.T, db *sql.DB, checkID, actorID, shiftID, sessionID uuid.UUID) {
	t.Helper()
	_, err := db.Exec(`
		UPDATE checks
		SET state = 'SETTLED', settled_at = now(),
		    settled_by_staff_identity_id = $2, settled_during_sales_shift_id = $3,
		    settled_staff_access_session_id = $4
		WHERE id = $1`,
		checkID, actorID, shiftID, sessionID)
	require.NoError(t, err)
}

// reopenCheck moves a settled Check back to OPEN, clearing the settlement
// evidence the check_settlement_evidence_valid constraint pairs with that
// state. It turns a correction chain back into the unsettled-Check blocker.
func reopenCheck(t *testing.T, db *sql.DB, checkID uuid.UUID) {
	t.Helper()
	_, err := db.Exec(`
		UPDATE checks
		SET state = 'OPEN', settled_at = NULL, settled_by_staff_identity_id = NULL,
		    settled_during_sales_shift_id = NULL, settled_staff_access_session_id = NULL
		WHERE id = $1`, checkID)
	require.NoError(t, err)
}

// closeCheckSession closes the Service Session owning a Check, so a seeded
// correction chain stops reporting the active-Session blocker once its
// financial obligation is resolved and a start is expected to succeed.
func closeCheckSession(t *testing.T, db *sql.DB, checkID uuid.UUID) {
	t.Helper()
	_, err := db.Exec(`
		UPDATE service_sessions
		SET state = 'CLOSED'
		WHERE id = (SELECT service_session_id FROM checks WHERE id = $1)`, checkID)
	require.NoError(t, err)
}

// seedPayment inserts one Payment against the named Check and Shift and
// returns its id.
func seedPayment(t *testing.T, db *sql.DB, checkID, shiftID, actorID, sessionID uuid.UUID,
	method string, appliedVND, tenderedVND int64,
) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	switch method {
	case "CASH":
		// payment_method_facts_valid: tendered >= applied and change =
		// tendered - applied.
		require.NoError(t, db.QueryRow(`
			INSERT INTO payments (check_id, sales_shift_id, actor_staff_identity_id,
			                      staff_access_session_id, applied_amount_vnd, method,
			                      cash_tendered_vnd, change_due_vnd)
			VALUES ($1, $2, $3, $4, $5, 'CASH', $6, $7) RETURNING id`,
			checkID, shiftID, actorID, sessionID,
			appliedVND, tenderedVND, tenderedVND-appliedVND).Scan(&id))
	case "MANUAL_QR":
		require.NoError(t, db.QueryRow(`
			INSERT INTO payments (check_id, sales_shift_id, actor_staff_identity_id,
			                      staff_access_session_id, applied_amount_vnd, method)
			VALUES ($1, $2, $3, $4, $5, 'MANUAL_QR') RETURNING id`,
			checkID, shiftID, actorID, sessionID, appliedVND).Scan(&id))
	default:
		t.Fatalf("unsupported payment method %q", method)
	}
	return id
}

// seedPaymentVoid voids a whole Payment: the amount is copied from the source,
// matching the whole-void rule.
func seedPaymentVoid(t *testing.T, db *sql.DB, paymentID, shiftID, actorID, sessionID uuid.UUID) {
	t.Helper()
	_, err := db.Exec(`
		INSERT INTO payment_voids (payment_id, sales_shift_id, amount_vnd, reason,
		                           actor_staff_identity_id, staff_access_session_id,
		                           approved_by_staff_identity_id)
		SELECT id, $2, applied_amount_vnd, 'DUPLICATE_PAYMENT', $3, $4, $3
		FROM payments WHERE id = $1`,
		paymentID, shiftID, actorID, sessionID)
	require.NoError(t, err)
}

// seededCorrection is the fixture chain a Charge Adjustment needs: the Check
// carrying the allocation, the Charge Allocation the adjustment reduces, and
// the Preparation Unit that sources it.
type seededCorrection struct {
	CheckID      uuid.UUID
	AllocationID uuid.UUID
	UnitID       uuid.UUID
}

// seedCorrectionCheck inserts a fresh Service Session, Check, and the whole
// submission chain behind one single-unit Charge Allocation. The reconciliation
// query derives the base charge as quantity * unit_price_vnd, so unitPriceVND
// is the full original charge of this Check. The Check is born OPEN, so tests
// that must not trip the unsettled-Check blocker settle it with settleCheck.
func seedCorrectionCheck(t *testing.T, db *sql.DB, shiftID, actorID, sessionID uuid.UUID,
	unitPriceVND int64,
) seededCorrection {
	t.Helper()
	serviceNumber := testServiceNumber()
	label := "Correction " + serviceNumber

	var session uuid.UUID
	require.NoError(t, db.QueryRow(`
		INSERT INTO service_sessions (service_number, sequence, sales_shift_id,
		                              created_by_staff_identity_id)
		VALUES ($1,
		        (SELECT COALESCE(MAX(sequence), 0) + 1
		         FROM service_sessions WHERE sales_shift_id = $2),
		        $2, $3)
		RETURNING id`,
		serviceNumber, shiftID, actorID).Scan(&session))

	var categoryID uuid.UUID
	require.NoError(t, db.QueryRow(`
		INSERT INTO menu_categories (name, normalized_name) VALUES ($1, lower($1)) RETURNING id`,
		label).Scan(&categoryID))

	var menuItemID uuid.UUID
	require.NoError(t, db.QueryRow(`
		INSERT INTO menu_items (category_id, name, normalized_name, price_vnd, available)
		VALUES ($1, $2, lower($2), $3, true) RETURNING id`,
		categoryID, label, unitPriceVND).Scan(&menuItemID))

	var draftID uuid.UUID
	require.NoError(t, db.QueryRow(`
		INSERT INTO order_drafts (service_session_id) VALUES ($1) RETURNING id`,
		session).Scan(&draftID))

	var draftItemID uuid.UUID
	require.NoError(t, db.QueryRow(`
		INSERT INTO order_draft_items (order_draft_id, menu_item_id) VALUES ($1, $2) RETURNING id`,
		draftID, menuItemID).Scan(&draftItemID))

	var committedItemID uuid.UUID
	require.NoError(t, db.QueryRow(`
		INSERT INTO committed_items (order_draft_id, source_draft_item_id, menu_item_id,
		                             category_name, item_name, quantity, unit_price_vnd, total_vnd)
		VALUES ($1, $2, $3, $4, $4, 1, $5, $5) RETURNING id`,
		draftID, draftItemID, menuItemID, label, unitPriceVND).Scan(&committedItemID))

	var checkID uuid.UUID
	require.NoError(t, db.QueryRow(`
		INSERT INTO checks (service_session_id) VALUES ($1) RETURNING id`,
		session).Scan(&checkID))

	var allocationID uuid.UUID
	require.NoError(t, db.QueryRow(`
		INSERT INTO charge_allocations (committed_item_id, check_id, quantity)
		VALUES ($1, $2, 1) RETURNING id`,
		committedItemID, checkID).Scan(&allocationID))

	var orderID uuid.UUID
	require.NoError(t, db.QueryRow(`
		INSERT INTO orders (service_session_id, order_draft_id,
		                    submitted_by_staff_identity_id, submitted_staff_access_session_id)
		VALUES ($1, $2, $3, $4) RETURNING id`,
		session, draftID, actorID, sessionID).Scan(&orderID))

	var orderItemID uuid.UUID
	require.NoError(t, db.QueryRow(`
		INSERT INTO order_items (order_id, committed_item_id) VALUES ($1, $2) RETURNING id`,
		orderID, committedItemID).Scan(&orderItemID))

	var unitID uuid.UUID
	require.NoError(t, db.QueryRow(`
		INSERT INTO preparation_units (order_item_id, unit_number, service_number,
		                               category_name, item_name)
		VALUES ($1, 1, $2, $3, $4) RETURNING id`,
		orderItemID, serviceNumber, label, label).Scan(&unitID))

	return seededCorrection{CheckID: checkID, AllocationID: allocationID, UnitID: unitID}
}

// seedLiveAdjustment records a LIVE_CHECK Cancellation Charge Adjustment, the
// second shape of the unresolved-correction blocker: it raises the Check's
// live obligation without taking the Check out of settlement scope.
func seedLiveAdjustment(t *testing.T, db *sql.DB, c seededCorrection, shiftID uuid.UUID,
	amountVND int64,
) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	require.NoError(t, db.QueryRow(`
		INSERT INTO charge_adjustments (kind, scope, preparation_unit_id, charge_allocation_id,
		                                check_id, sales_shift_id, amount_vnd)
		VALUES ('CANCELLATION', 'LIVE_CHECK', $1, $2, $3, $4, $5) RETURNING id`,
		c.UnitID, c.AllocationID, c.CheckID, shiftID, amountVND).Scan(&id))
	return id
}

// seedCompletedSale freezes a fresh Service Session into a Completed Sale, so
// a POST_SALE Charge Adjustment has a sale to name. The Session is born CLOSED
// because completing a sale ends it; an ACTIVE sale Session would otherwise
// trip the active-Session blocker in tests that expect a start to succeed.
func seedCompletedSale(t *testing.T, db *sql.DB, shiftID, actorID, sessionID uuid.UUID) uuid.UUID {
	t.Helper()
	var session uuid.UUID
	require.NoError(t, db.QueryRow(`
		INSERT INTO service_sessions (service_number, sequence, state, sales_shift_id,
		                              created_by_staff_identity_id)
		VALUES ($1,
		        (SELECT COALESCE(MAX(sequence), 0) + 1
		         FROM service_sessions WHERE sales_shift_id = $2),
		        'CLOSED', $2, $3)
		RETURNING id`,
		testServiceNumber(), shiftID, actorID).Scan(&session))

	var saleID uuid.UUID
	require.NoError(t, db.QueryRow(`
		INSERT INTO completed_sales (service_session_id, completed_by_staff_identity_id,
		                             completed_staff_access_session_id)
		VALUES ($1, $2, $3) RETURNING id`,
		session, actorID, sessionID).Scan(&saleID))
	return saleID
}

// seedPostSaleAdjustment records a POST_SALE Comp linked to a Completed Sale.
// A Waste sources the adjustment so both the kind and scope constraints hold.
func seedPostSaleAdjustment(t *testing.T, db *sql.DB, c seededCorrection,
	completedSaleID, shiftID, actorID, sessionID uuid.UUID, amountVND int64,
) uuid.UUID {
	t.Helper()
	var wasteID uuid.UUID
	require.NoError(t, db.QueryRow(`
		INSERT INTO preparation_wastes (preparation_unit_id, prior_state, reason,
		                                actor_staff_identity_id, staff_access_session_id)
		VALUES ($1, 'READY', 'QUALITY_FAILURE', $2, $3) RETURNING id`,
		c.UnitID, actorID, sessionID).Scan(&wasteID))

	var id uuid.UUID
	require.NoError(t, db.QueryRow(`
		INSERT INTO charge_adjustments (kind, scope, preparation_unit_id, preparation_waste_id,
		                                charge_allocation_id, check_id, completed_sale_id,
		                                sales_shift_id, amount_vnd)
		VALUES ('COMP', 'POST_SALE', $1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		c.UnitID, wasteID, c.AllocationID, c.CheckID, completedSaleID, shiftID,
		amountVND).Scan(&id))
	return id
}

// seedRefund inserts one Refund and, when completedAt is non-nil, its
// completion. The projection derives Refund state from exactly this evidence.
func seedRefund(t *testing.T, db *sql.DB, checkID, shiftID, actorID, sessionID uuid.UUID,
	method string, amountVND int64, completedSaleID *uuid.UUID, createdAt time.Time,
	completedAt *time.Time,
) uuid.UUID {
	t.Helper()
	sale := uuid.NullUUID{}
	if completedSaleID != nil {
		sale = uuid.NullUUID{UUID: *completedSaleID, Valid: true}
	}

	var id uuid.UUID
	require.NoError(t, db.QueryRow(`
		INSERT INTO refunds (check_id, completed_sale_id, sales_shift_id, method, amount_vnd,
		                     reason, actor_staff_identity_id, staff_access_session_id,
		                     approved_by_staff_identity_id, created_at)
		VALUES ($1, $2, $3, $4, $5, 'CUSTOMER_REQUEST', $6, $7, $6, $8) RETURNING id`,
		checkID, sale, shiftID, method, amountVND, actorID, sessionID, createdAt).Scan(&id))

	if completedAt != nil {
		_, err := db.Exec(`
			INSERT INTO refund_completions (refund_id, completed_by_staff_identity_id,
			                                staff_access_session_id, completed_at)
			VALUES ($1, $2, $3, $4)`,
			id, actorID, sessionID, *completedAt)
		require.NoError(t, err)
	}
	return id
}

// seedRefundAdjustmentAllocation records a completed Refund's allocation to one
// Charge Adjustment, so the adjustment's unresolved obligation shrinks by
// exactly that amount (spec 8's POST_SALE term; the LIVE_CHECK term reads
// completed live Refunds directly).
func seedRefundAdjustmentAllocation(t *testing.T, db *sql.DB, refundID, adjustmentID uuid.UUID,
	amountVND int64,
) {
	t.Helper()
	_, err := db.Exec(`
		INSERT INTO refund_adjustment_allocations (refund_id, charge_adjustment_id, amount_vnd)
		VALUES ($1, $2, $3)`, refundID, adjustmentID, amountVND)
	require.NoError(t, err)
}
