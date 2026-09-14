//go:build integration

package shift_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"

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

// insertPayment records one Payment against the open Shift. For CASH the
// tendered/change columns are written; for MANUAL_QR they stay NULL, because
// payment_method_facts_valid rejects QR rows that carry cash facts.
func (e *shiftEnv) insertPayment(t *testing.T, method string, appliedVND, tenderedVND int64) {
	t.Helper()
	e.insertPaymentForShift(t, e.ShiftID, method, appliedVND, tenderedVND)
}

// insertPaymentForShift records one Payment against the named Shift, so tests
// can plant Payments that must not leak into the open Shift's figure.
func (e *shiftEnv) insertPaymentForShift(t *testing.T, shiftID uuid.UUID,
	method string, appliedVND, tenderedVND int64,
) {
	t.Helper()
	switch method {
	case "CASH":
		// payment_method_facts_valid: tendered >= applied and change =
		// tendered - applied.
		_, err := e.DB.Exec(`
			INSERT INTO payments (check_id, sales_shift_id, actor_staff_identity_id,
			                      staff_access_session_id, applied_amount_vnd, method,
			                      cash_tendered_vnd, change_due_vnd)
			VALUES ($1, $2, $3, $4, $5, 'CASH', $6, $7)`,
			e.checkID, shiftID, e.Cashier.StaffID, e.Cashier.SessionID,
			appliedVND, tenderedVND, tenderedVND-appliedVND)
		require.NoError(t, err)
	case "MANUAL_QR":
		_, err := e.DB.Exec(`
			INSERT INTO payments (check_id, sales_shift_id, actor_staff_identity_id,
			                      staff_access_session_id, applied_amount_vnd, method)
			VALUES ($1, $2, $3, $4, $5, 'MANUAL_QR')`,
			e.checkID, shiftID, e.Cashier.StaffID, e.Cashier.SessionID, appliedVND)
		require.NoError(t, err)
	default:
		t.Fatalf("unsupported payment method %q", method)
	}
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
