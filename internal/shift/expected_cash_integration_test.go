//go:build integration

package shift_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/shift"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// shiftEnv is the fixture world for the Expected Cash read: one open Sales
// Shift opened through the slice's own handler, a second CLOSED Shift seeded
// directly, and a service_sessions + checks row pair seeded with direct SQL so
// Payments have a Check to attach to.
//
// internal/sales owns service_sessions, checks, and payments; seeding rows
// another slice owns with raw SQL follows the precedent Phase 3's tests set
// when they seeded service_sessions before internal/sales owned them.
type shiftEnv struct {
	DB      *sql.DB
	Queries *sqlc.Queries
	Runner  *shift.Runner

	Cashier testActor

	// ShiftID is the open Sales Shift the env opened; previousShiftID is a
	// second, CLOSED Shift whose Payments must never leak into the open
	// Shift's figure.
	ShiftID         uuid.UUID
	previousShiftID uuid.UUID

	// checkID is the open Check every seeded Payment is applied to.
	checkID uuid.UUID
}

func newShiftEnv(t *testing.T) *shiftEnv {
	t.Helper()
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	runner := shift.NewRunner(db, q)

	cashier := newTestActor(t, q, []string{"CASHIER"}, true)

	_, opened, err := shift.NewOpenShiftHandler(runner).Handle(context.Background(), cashier.actor(),
		shift.OpenShiftCommand{RequestID: uuid.New(), OpeningFloatVND: int64Ptr(500_000)})
	require.NoError(t, err)

	// The row is inserted CLOSED because the one-open-Shift invariant is
	// global: a second OPEN Shift is unrepresentable.
	var previousID uuid.UUID
	require.NoError(t, db.QueryRow(
		`INSERT INTO sales_shifts (state, opened_by_staff_identity_id, opening_float_vnd)
		 VALUES ('CLOSED', $1, 0) RETURNING id`, cashier.StaffID).Scan(&previousID))

	return &shiftEnv{
		DB:              db,
		Queries:         q,
		Runner:          runner,
		Cashier:         cashier,
		ShiftID:         opened.ID,
		previousShiftID: previousID,
		checkID:         seedShiftEnvCheck(t, db, opened.ID, cashier.StaffID),
	}
}

// seedShiftEnvCheck inserts one service_sessions row plus one checks row with
// direct SQL and returns the Check's id. The Session is created against the
// named Sales Shift, which service_sessions.sales_shift_id (NOT NULL since
// Phase 5A) requires.
func seedShiftEnvCheck(t *testing.T, db *sql.DB, shiftID, actorID uuid.UUID) uuid.UUID {
	t.Helper()

	var sessionID uuid.UUID
	require.NoError(t, db.QueryRow(
		`INSERT INTO service_sessions (service_number, sequence, sales_shift_id, created_by_staff_identity_id)
		 VALUES ($1, 1, $2, $3) RETURNING id`,
		testServiceNumber(), shiftID, actorID).Scan(&sessionID))

	var checkID uuid.UUID
	require.NoError(t, db.QueryRow(
		`INSERT INTO checks (service_session_id) VALUES ($1) RETURNING id`,
		sessionID).Scan(&checkID))
	return checkID
}

// testServiceNumber draws a service_number matching ^[A-Z0-9]{6}$ from a fresh
// UUID. Uniqueness is only required among one Shift's Sessions (ADR-011), and
// every env seeds one Session against a fresh Shift, but a random value keeps
// the fixture independent of that invariant.
func testServiceNumber() string {
	return strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", ""))[:6]
}

// currentShift runs the slice's own current-Shift read and returns its
// response with ExpectedCashVND.
func (e *shiftEnv) currentShift(t *testing.T) *shift.CurrentSalesShiftResponse {
	t.Helper()
	res, err := shift.NewCurrentShiftHandler(e.Runner).Handle(context.Background(), e.Cashier.actor())
	require.NoError(t, err)
	require.NotNil(t, res)
	return res
}

// insertPayment records one Payment against the open Shift and returns its
// id, so a later Payment Void can name it. For CASH the tendered/change
// columns are written; for MANUAL_QR they stay NULL, because
// payment_method_facts_valid rejects QR rows that carry cash facts.
func (e *shiftEnv) insertPayment(t *testing.T, method string, appliedVND, tenderedVND int64) uuid.UUID {
	t.Helper()
	return e.insertPaymentForShift(t, e.ShiftID, method, appliedVND, tenderedVND)
}

// insertPaymentForShift records one Payment against the named Shift, so tests
// can plant Payments that must not leak into the open Shift's figure.
func (e *shiftEnv) insertPaymentForShift(t *testing.T, shiftID uuid.UUID,
	method string, appliedVND, tenderedVND int64,
) uuid.UUID {
	t.Helper()
	return seedPayment(t, e.DB, e.checkID, shiftID, e.Cashier.StaffID, e.Cashier.SessionID,
		method, appliedVND, tenderedVND)
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
// is the full original charge of this Check.
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

// seedLiveAdjustment records a LIVE_CHECK Charge Adjustment attributed to the
// named Shift: the attribution that makes the Check part of that Shift's
// owed-back obligation.
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
// a POST_SALE Charge Adjustment has a sale to name.
func seedCompletedSale(t *testing.T, db *sql.DB, shiftID, actorID, sessionID uuid.UUID) uuid.UUID {
	t.Helper()
	var session uuid.UUID
	require.NoError(t, db.QueryRow(`
		INSERT INTO service_sessions (service_number, sequence, sales_shift_id,
		                              created_by_staff_identity_id)
		VALUES ($1,
		        (SELECT COALESCE(MAX(sequence), 0) + 1
		         FROM service_sessions WHERE sales_shift_id = $2),
		        $2, $3)
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

// seedRefundAdjustmentAllocation allocates part of a completed Refund against
// a POST_SALE Charge Adjustment, consuming that adjustment's refundable
// capacity.
func seedRefundAdjustmentAllocation(t *testing.T, db *sql.DB, refundID, adjustmentID uuid.UUID,
	amountVND int64,
) {
	t.Helper()
	_, err := db.Exec(`
		INSERT INTO refund_adjustment_allocations (refund_id, charge_adjustment_id, amount_vnd)
		VALUES ($1, $2, $3)`, refundID, adjustmentID, amountVND)
	require.NoError(t, err)
}

func TestExpectedCashCountsCashPayments(t *testing.T) {
	env := newShiftEnv(t)

	before := env.currentShift(t).ExpectedCashVND

	t.Run("a cash payment raises the figure by the applied amount", func(t *testing.T) {
		env.insertPayment(t, "CASH", 85_000, 100_000)
		require.Equal(t, before+85_000, env.currentShift(t).ExpectedCashVND,
			"the applied amount, not the tendered amount, is the net cash effect")
	})

	t.Run("a manual QR payment does not change it", func(t *testing.T) {
		current := env.currentShift(t).ExpectedCashVND
		env.insertPayment(t, "MANUAL_QR", 50_000, 0)
		require.Equal(t, current, env.currentShift(t).ExpectedCashVND)
	})

	t.Run("a payment attributed to another shift does not leak in", func(t *testing.T) {
		current := env.currentShift(t).ExpectedCashVND
		env.insertPaymentForShift(t, env.previousShiftID, "CASH", 70_000, 70_000)
		require.Equal(t, current, env.currentShift(t).ExpectedCashVND)
	})
}

func TestExpectedCashExcludesVoidsAndPendingRefunds(t *testing.T) {
	env := newShiftEnv(t)
	actor := env.Cashier

	before := env.currentShift(t)
	require.NotNil(t, before.Refunds, "an empty Refund list must still be an array")
	require.Empty(t, before.Refunds)

	// A voided Payment keeps its original amount in the payment term but is
	// removed whole by the void term.
	voidedPayment := env.insertPayment(t, "CASH", 90_000, 90_000)
	seedPaymentVoid(t, env.DB, voidedPayment, env.ShiftID, actor.StaffID, actor.SessionID)
	env.insertPayment(t, "CASH", 120_000, 200_000)

	current := env.currentShift(t)
	require.Equal(t, int64(210_000), current.CashPaymentVND,
		"the payment term counts original applied amounts")
	require.Equal(t, int64(90_000), current.CashPaymentVoidVND,
		"the void term removes the source Payment's whole amount")
	require.Equal(t, before.ExpectedCashVND+120_000, current.ExpectedCashVND,
		"only the non-voided Payment reaches the drawer")

	// A completed Cash Refund has left the drawer; a Refund without its
	// completion has not.
	completedAt := time.Now().UTC()
	seedRefund(t, env.DB, env.checkID, env.ShiftID, actor.StaffID, actor.SessionID,
		"CASH", 30_000, nil, completedAt.Add(-time.Minute), &completedAt)
	seedRefund(t, env.DB, env.checkID, env.ShiftID, actor.StaffID, actor.SessionID,
		"CASH", 10_000, nil, time.Now().UTC(), nil)

	current = env.currentShift(t)
	require.Equal(t, int64(30_000), current.CashRefundVND,
		"only a completed Cash Refund has left the drawer")
	require.Equal(t, before.ExpectedCashVND+120_000-30_000, current.ExpectedCashVND)
}
