//go:build integration

package shift_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// insertShiftRow opens a Sales Shift with raw SQL so constraint behavior is
// tested without going through the slice.
func insertShiftRow(t *testing.T, db *sql.DB, openerID uuid.UUID, floatVND int64) (uuid.UUID, error) {
	t.Helper()
	var id uuid.UUID
	err := db.QueryRow(
		`INSERT INTO sales_shifts (opened_by_staff_identity_id, opening_float_vnd)
		 VALUES ($1, $2) RETURNING id`,
		openerID, floatVND,
	).Scan(&id)
	return id, err
}

func TestSchemaRejectsSecondOpenShift(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	actor := newTestActor(t, q, []string{"CASHIER"}, true)

	_, err := insertShiftRow(t, db, actor.StaffID, 500000)
	require.NoError(t, err)

	_, err = insertShiftRow(t, db, actor.StaffID, 700000)
	require.Error(t, err, "a second active Sales Shift must violate the partial unique index")
	assert.Contains(t, err.Error(), "sales_shift_only_one_active_unique")
}

func TestSchemaAllowsOpenAfterClose(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	actor := newTestActor(t, q, []string{"CASHIER"}, true)

	first, err := insertShiftRow(t, db, actor.StaffID, 500000)
	require.NoError(t, err)

	// CLOSED exists in the check constraint although Phase 4 ships no close
	// command, so Phase 5 adds one without a state-domain migration.
	_, err = db.Exec(`UPDATE sales_shifts SET state = 'CLOSED' WHERE id = $1`, first)
	require.NoError(t, err)

	_, err = insertShiftRow(t, db, actor.StaffID, 700000)
	assert.NoError(t, err, "the partial index must only constrain active (OPEN or CLOSING) rows")
}

func TestSchemaRejectsInvalidShiftValues(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	actor := newTestActor(t, q, []string{"CASHIER"}, true)

	_, err := insertShiftRow(t, db, actor.StaffID, -1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sales_shift_opening_float_vnd_valid")

	_, err = insertShiftRow(t, db, actor.StaffID, 2147483648)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sales_shift_opening_float_vnd_valid")

	// Zero is valid: a station may legitimately open with an empty fund.
	_, err = insertShiftRow(t, db, actor.StaffID, 0)
	assert.NoError(t, err)
}

func TestSchemaRejectsInvalidCashMovements(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	actor := newTestActor(t, q, []string{"CASHIER"}, true)
	shiftID, err := insertShiftRow(t, db, actor.StaffID, 500000)
	require.NoError(t, err)

	insert := func(method string, amount int64, reason string, note any) error {
		_, execErr := db.Exec(
			`INSERT INTO cash_movements (
				sales_shift_id, method, amount_vnd, reason, note,
				initiated_by_staff_identity_id, initiated_staff_access_session_id,
				approved_by_staff_identity_id)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			shiftID, method, amount, reason, note,
			actor.StaffID, actor.SessionID, actor.StaffID,
		)
		return execErr
	}

	cases := []struct {
		name       string
		method     string
		amount     int64
		reason     string
		note       any
		constraint string
	}{
		{"unknown method", "CASH_DROP", 1000, "SAFE_DROP", nil, "cash_movement_method_valid"},
		{"zero amount", "PAY_IN", 0, "ADD_CHANGE_FUND", nil, "cash_movement_amount_vnd_valid"},
		{"negative amount", "PAY_OUT", -1000, "SAFE_DROP", nil, "cash_movement_amount_vnd_valid"},
		{"over-bound amount", "PAY_IN", 2147483648, "ADD_CHANGE_FUND", nil, "cash_movement_amount_vnd_valid"},
		{"unknown reason", "PAY_IN", 1000, "PETTY_CASH", nil, "cash_movement_reason_valid"},
		{"OTHER without note", "PAY_OUT", 1000, "OTHER", nil, "cash_movement_note_valid"},
		{"untrimmed note", "PAY_OUT", 1000, "OTHER", "  padded  ", "cash_movement_note_valid"},
		{"empty note", "PAY_OUT", 1000, "OTHER", "", "cash_movement_note_valid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			insertErr := insert(tc.method, tc.amount, tc.reason, tc.note)
			require.Error(t, insertErr)
			assert.Contains(t, insertErr.Error(), tc.constraint)
		})
	}

	assert.NoError(t, insert("PAY_OUT", 50000, "SAFE_DROP", nil))
	assert.NoError(t, insert("PAY_OUT", 50000, "OTHER", "mua da cho quay pha che"))
}

// -- Phase 7: closure & reconciliation schema --

// insertShiftReconciliationRow seeds one immutable reconciliation with an
// exact, quiet Phase 6C snapshot: an Opening Float of 500000 and no other
// money movement. The three blocker-evidence columns are omitted and default
// to their CHECK-enforced zero.
func insertShiftReconciliationRow(
	t *testing.T, db *sql.DB, shiftID, starterID, sessionID uuid.UUID,
) (uuid.UUID, error) {
	t.Helper()
	var id uuid.UUID
	err := db.QueryRow(
		`INSERT INTO shift_reconciliations (
			sales_shift_id, started_by_staff_identity_id, started_staff_access_session_id,
			opening_float_vnd, pay_in_vnd, pay_out_vnd,
			cash_payment_vnd, cash_payment_void_vnd, cash_refund_vnd, expected_cash_vnd,
			manual_qr_payment_vnd, manual_qr_payment_void_vnd,
			expected_manual_qr_received_vnd, manual_qr_refund_vnd
		 ) VALUES ($1, $2, $3, 500000, 0, 0, 0, 0, 0, 500000, 0, 0, 0, 0)
		 RETURNING id`,
		shiftID, starterID, sessionID,
	).Scan(&id)
	return id, err
}

// insertCashCountRow appends one Cash Count attempt with raw SQL.
func insertCashCountRow(
	t *testing.T, db *sql.DB, reconciliationID uuid.UUID, sequence int, amountVND int64, actor testActor,
) (uuid.UUID, error) {
	t.Helper()
	var id uuid.UUID
	err := db.QueryRow(
		`INSERT INTO shift_cash_counts (
			reconciliation_id, sequence, counted_cash_vnd,
			counted_by_staff_identity_id, counted_staff_access_session_id
		 ) VALUES ($1, $2, $3, $4, $5)
		 RETURNING id`,
		reconciliationID, sequence, amountVND, actor.StaffID, actor.SessionID,
	).Scan(&id)
	return id, err
}

// insertQRObservationRow appends one Manual QR observation attempt with raw SQL.
func insertQRObservationRow(
	t *testing.T, db *sql.DB, reconciliationID uuid.UUID, sequence int,
	receivedVND, refundedVND int64, actor testActor,
) (uuid.UUID, error) {
	t.Helper()
	var id uuid.UUID
	err := db.QueryRow(
		`INSERT INTO shift_qr_observations (
			reconciliation_id, sequence, observed_received_vnd, observed_refunded_vnd,
			observed_by_staff_identity_id, observed_staff_access_session_id
		 ) VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id`,
		reconciliationID, sequence, receivedVND, refundedVND, actor.StaffID, actor.SessionID,
	).Scan(&id)
	return id, err
}

// closingShiftFixture carries the evidence chain one closure insert must
// reference: the CLOSING Shift, its frozen reconciliation, two Cash Counts,
// and one QR Observation.
type closingShiftFixture struct {
	ShiftID          uuid.UUID
	ReconciliationID uuid.UUID
	InitialCountID   uuid.UUID
	FinalCountID     uuid.UUID
	QRObservationID  uuid.UUID
	Opener           testActor
	OpenedAt         time.Time
}

// seedClosingShiftFixture builds the full evidence chain for one CLOSING
// Shift: an OPEN Shift with a frozen exact snapshot (Opening Float 500000, no
// movement), Cash Counts 1 and 2 both counting 500000, QR Observation 1
// observing explicit zeroes, and the Shift moved to CLOSING.
func seedClosingShiftFixture(t *testing.T, db *sql.DB, q *sqlc.Queries) closingShiftFixture {
	t.Helper()
	cashier := newTestActor(t, q, []string{"CASHIER"}, true)

	shiftID, err := insertShiftRow(t, db, cashier.StaffID, 500000)
	require.NoError(t, err)

	reconciliationID, err := insertShiftReconciliationRow(t, db, shiftID, cashier.StaffID, cashier.SessionID)
	require.NoError(t, err)

	initialCountID, err := insertCashCountRow(t, db, reconciliationID, 1, 500000, cashier)
	require.NoError(t, err)
	finalCountID, err := insertCashCountRow(t, db, reconciliationID, 2, 500000, cashier)
	require.NoError(t, err)
	qrObservationID, err := insertQRObservationRow(t, db, reconciliationID, 1, 0, 0, cashier)
	require.NoError(t, err)

	var openedAt time.Time
	require.NoError(t, db.QueryRow(`SELECT opened_at FROM sales_shifts WHERE id = $1`, shiftID).Scan(&openedAt))

	_, err = db.Exec(`UPDATE sales_shifts SET state = 'CLOSING' WHERE id = $1`, shiftID)
	require.NoError(t, err)

	return closingShiftFixture{
		ShiftID:          shiftID,
		ReconciliationID: reconciliationID,
		InitialCountID:   initialCountID,
		FinalCountID:     finalCountID,
		QRObservationID:  qrObservationID,
		Opener:           cashier,
		OpenedAt:         openedAt,
	}
}

// insertClosureRow writes one closure snapshot. The fixture supplies the
// evidence foreign keys and the opener; the caller varies the approver, the
// claimed opened_at, and the cash observed/difference pair. The QR dimensions
// stay exact (zero expected, zero observed, zero difference).
func insertClosureRow(
	t *testing.T, db *sql.DB, shiftID, reconciliationID uuid.UUID, f closingShiftFixture,
	approverID any, openedAt time.Time, observedCashVND, expectedCashVND, cashDifferenceVND int64,
) (uuid.UUID, error) {
	t.Helper()
	var id uuid.UUID
	err := db.QueryRow(
		`INSERT INTO shift_closures (
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
		 ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
		           500000, 0, 0, 0, 0, 0, $11, 0, 0, $12, 0,
		           $13, 0, 0, $14, 0, 0)
		 RETURNING id`,
		shiftID, reconciliationID,
		f.InitialCountID, f.FinalCountID, f.QRObservationID,
		f.Opener.StaffID, f.Opener.StaffID, f.Opener.SessionID,
		approverID, openedAt,
		expectedCashVND, 0,
		observedCashVND, cashDifferenceVND,
	).Scan(&id)
	return id, err
}

// insertDiscrepancyRow writes one discrepancy row with raw SQL.
func insertDiscrepancyRow(
	t *testing.T, db *sql.DB, closureID uuid.UUID, dimension string,
	expectedVND, observedVND, differenceVND int64, reason string, note any,
) error {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO shift_discrepancies (
			shift_closure_id, dimension, expected_vnd, observed_vnd,
			difference_vnd, reason, note
		 ) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		closureID, dimension, expectedVND, observedVND, differenceVND, reason, note,
	)
	return err
}

func TestShiftClosureSchema(t *testing.T) {
	t.Run("two active shifts are rejected", func(t *testing.T) {
		db, q := openShiftTestDB(t)
		truncateShiftTables(t, db)
		actor := newTestActor(t, q, []string{"CASHIER"}, true)

		first, err := insertShiftRow(t, db, actor.StaffID, 500000)
		require.NoError(t, err)

		_, err = db.Exec(`UPDATE sales_shifts SET state = 'CLOSING' WHERE id = $1`, first)
		require.NoError(t, err)

		_, err = insertShiftRow(t, db, actor.StaffID, 700000)
		require.Error(t, err, "an OPEN and a CLOSING Shift are both active; the second must fail")
		assert.Contains(t, err.Error(), "sales_shift_only_one_active_unique")
	})

	t.Run("CLOSING is a valid state and unknown states are not", func(t *testing.T) {
		db, q := openShiftTestDB(t)
		truncateShiftTables(t, db)
		actor := newTestActor(t, q, []string{"CASHIER"}, true)

		shiftID, err := insertShiftRow(t, db, actor.StaffID, 500000)
		require.NoError(t, err)

		_, err = db.Exec(`UPDATE sales_shifts SET state = 'CLOSING' WHERE id = $1`, shiftID)
		require.NoError(t, err, "the widened state check must accept CLOSING")

		_, err = db.Exec(`UPDATE sales_shifts SET state = 'PAUSED' WHERE id = $1`, shiftID)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "sales_shift_state_valid")
	})

	t.Run("one reconciliation per shift", func(t *testing.T) {
		db, q := openShiftTestDB(t)
		truncateShiftTables(t, db)
		actor := newTestActor(t, q, []string{"CASHIER"}, true)

		shiftID, err := insertShiftRow(t, db, actor.StaffID, 500000)
		require.NoError(t, err)

		_, err = insertShiftReconciliationRow(t, db, shiftID, actor.StaffID, actor.SessionID)
		require.NoError(t, err)

		_, err = insertShiftReconciliationRow(t, db, shiftID, actor.StaffID, actor.SessionID)
		require.Error(t, err, "a second reconciliation for the same Shift must fail")
		assert.Contains(t, err.Error(), "shift_reconciliation_sales_shift_unique")
	})

	t.Run("reconciliation blocker evidence must be cleared to zero", func(t *testing.T) {
		db, q := openShiftTestDB(t)
		truncateShiftTables(t, db)
		actor := newTestActor(t, q, []string{"CASHIER"}, true)

		shiftID, err := insertShiftRow(t, db, actor.StaffID, 500000)
		require.NoError(t, err)

		seed := func(column string) error {
			_, seedErr := db.Exec(
				`INSERT INTO shift_reconciliations (
					sales_shift_id, started_by_staff_identity_id, started_staff_access_session_id,
					opening_float_vnd, pay_in_vnd, pay_out_vnd,
					cash_payment_vnd, cash_payment_void_vnd, cash_refund_vnd, expected_cash_vnd,
					manual_qr_payment_vnd, manual_qr_payment_void_vnd,
					expected_manual_qr_received_vnd, manual_qr_refund_vnd,
					`+column+`
				 ) VALUES ($1, $2, $3, 500000, 0, 0, 0, 0, 0, 500000, 0, 0, 0, 0, 1)`,
				shiftID, actor.StaffID, actor.SessionID,
			)
			return seedErr
		}

		for _, column := range []string{
			"pending_manual_qr_refund_vnd",
			"pending_refund_vnd",
			"unresolved_post_sale_adjustment_vnd",
		} {
			err := seed(column)
			require.Error(t, err, "%s must be constrained to zero", column)
			assert.Contains(t, err.Error(), "shift_reconciliation_"+column+"_zero")
		}
	})

	t.Run("cash count ledger bounds and uniqueness", func(t *testing.T) {
		db, q := openShiftTestDB(t)
		truncateShiftTables(t, db)
		actor := newTestActor(t, q, []string{"CASHIER"}, true)

		shiftID, err := insertShiftRow(t, db, actor.StaffID, 500000)
		require.NoError(t, err)
		reconciliationID, err := insertShiftReconciliationRow(t, db, shiftID, actor.StaffID, actor.SessionID)
		require.NoError(t, err)

		_, err = insertCashCountRow(t, db, reconciliationID, 1, 500000, actor)
		require.NoError(t, err)

		_, err = insertCashCountRow(t, db, reconciliationID, 2, -1, actor)
		require.Error(t, err, "a negative Cash Count must fail")
		assert.Contains(t, err.Error(), "shift_cash_count_counted_cash_vnd_valid")

		_, err = insertCashCountRow(t, db, reconciliationID, 0, 500000, actor)
		require.Error(t, err, "a zero sequence must fail")
		assert.Contains(t, err.Error(), "shift_cash_count_sequence_positive")

		_, err = insertCashCountRow(t, db, reconciliationID, 1, 400000, actor)
		require.Error(t, err, "a duplicated sequence must fail")
		assert.Contains(t, err.Error(), "shift_cash_count_reconciliation_sequence_unique")
	})

	t.Run("QR observation ledger bounds and uniqueness", func(t *testing.T) {
		db, q := openShiftTestDB(t)
		truncateShiftTables(t, db)
		actor := newTestActor(t, q, []string{"CASHIER"}, true)

		shiftID, err := insertShiftRow(t, db, actor.StaffID, 500000)
		require.NoError(t, err)
		reconciliationID, err := insertShiftReconciliationRow(t, db, shiftID, actor.StaffID, actor.SessionID)
		require.NoError(t, err)

		_, err = insertQRObservationRow(t, db, reconciliationID, 1, 0, 0, actor)
		require.NoError(t, err, "explicit zeroes are valid observations")

		_, err = insertQRObservationRow(t, db, reconciliationID, 2, -1, 0, actor)
		require.Error(t, err, "a negative observed received amount must fail")
		assert.Contains(t, err.Error(), "shift_qr_observation_observed_received_vnd_valid")

		_, err = insertQRObservationRow(t, db, reconciliationID, 2, 0, -1, actor)
		require.Error(t, err, "a negative observed refunded amount must fail")
		assert.Contains(t, err.Error(), "shift_qr_observation_observed_refunded_vnd_valid")

		_, err = insertQRObservationRow(t, db, reconciliationID, 1, 1000, 0, actor)
		require.Error(t, err, "a duplicated sequence must fail")
		assert.Contains(t, err.Error(), "shift_qr_observation_reconciliation_sequence_unique")
	})

	t.Run("exact closure with an approver is rejected", func(t *testing.T) {
		db, q := openShiftTestDB(t)
		truncateShiftTables(t, db)
		fixture := seedClosingShiftFixture(t, db, q)

		_, err := insertClosureRow(t, db, fixture.ShiftID, fixture.ReconciliationID, fixture,
			fixture.Opener.StaffID, fixture.OpenedAt, 500000, 500000, 0)
		require.Error(t, err, "an exact close must not carry an approver")
		assert.Contains(t, err.Error(), "shift_closure_approval_difference_consistent")
	})

	t.Run("nonzero difference without an approver is rejected", func(t *testing.T) {
		db, q := openShiftTestDB(t)
		truncateShiftTables(t, db)
		fixture := seedClosingShiftFixture(t, db, q)

		_, err := insertClosureRow(t, db, fixture.ShiftID, fixture.ReconciliationID, fixture,
			nil, fixture.OpenedAt, 490000, 500000, -10000)
		require.Error(t, err, "a discrepant close must carry an approver")
		assert.Contains(t, err.Error(), "shift_closure_approval_difference_consistent")
	})

	t.Run("closure difference must equal observed minus expected", func(t *testing.T) {
		db, q := openShiftTestDB(t)
		truncateShiftTables(t, db)
		fixture := seedClosingShiftFixture(t, db, q)

		// The approver keeps the approval/difference check satisfied so the
		// equation check is the only violated constraint.
		_, err := insertClosureRow(t, db, fixture.ShiftID, fixture.ReconciliationID, fixture,
			fixture.Opener.StaffID, fixture.OpenedAt, 500000, 500000, -10000)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "shift_closure_cash_difference_equation")
	})

	t.Run("closure cannot close before it opens", func(t *testing.T) {
		db, q := openShiftTestDB(t)
		truncateShiftTables(t, db)
		fixture := seedClosingShiftFixture(t, db, q)

		_, err := insertClosureRow(t, db, fixture.ShiftID, fixture.ReconciliationID, fixture,
			nil, fixture.OpenedAt.Add(time.Hour), 500000, 500000, 0)
		require.Error(t, err, "closed_at must never precede opened_at")
		assert.Contains(t, err.Error(), "shift_closure_close_not_before_open")
	})

	t.Run("one closure per shift and per reconciliation", func(t *testing.T) {
		db, q := openShiftTestDB(t)
		truncateShiftTables(t, db)
		actor := newTestActor(t, q, []string{"CASHIER"}, true)
		fixture := seedClosingShiftFixture(t, db, q)

		// Retire the fixture Shift so a second Shift can exist at all, then
		// close-and-reopen-free: both Shifts end up CLOSED.
		_, err := db.Exec(`UPDATE sales_shifts SET state = 'CLOSED' WHERE id = $1`, fixture.ShiftID)
		require.NoError(t, err)
		secondShiftID, err := insertShiftRow(t, db, actor.StaffID, 500000)
		require.NoError(t, err)
		_, err = db.Exec(`UPDATE sales_shifts SET state = 'CLOSED' WHERE id = $1`, secondShiftID)
		require.NoError(t, err)
		secondReconciliationID, err := insertShiftReconciliationRow(t, db, secondShiftID, actor.StaffID, actor.SessionID)
		require.NoError(t, err)

		closureID, err := insertClosureRow(t, db, fixture.ShiftID, fixture.ReconciliationID, fixture,
			nil, fixture.OpenedAt, 500000, 500000, 0)
		require.NoError(t, err, "the exact baseline closure must insert cleanly")
		assert.NotZero(t, closureID)

		_, err = insertClosureRow(t, db, fixture.ShiftID, secondReconciliationID, fixture,
			nil, fixture.OpenedAt, 500000, 500000, 0)
		require.Error(t, err, "a second closure for the same Shift must fail")
		assert.Contains(t, err.Error(), "shift_closure_sales_shift_unique")

		_, err = insertClosureRow(t, db, secondShiftID, fixture.ReconciliationID, fixture,
			nil, fixture.OpenedAt, 500000, 500000, 0)
		require.Error(t, err, "a second closure for the same reconciliation must fail")
		assert.Contains(t, err.Error(), "shift_closure_reconciliation_unique")
	})

	t.Run("discrepancy constraints", func(t *testing.T) {
		db, q := openShiftTestDB(t)
		truncateShiftTables(t, db)
		fixture := seedClosingShiftFixture(t, db, q)

		closureID, err := insertClosureRow(t, db, fixture.ShiftID, fixture.ReconciliationID, fixture,
			fixture.Opener.StaffID, fixture.OpenedAt, 490000, 500000, -10000)
		require.NoError(t, err, "the discrepant baseline closure must insert cleanly")

		cases := []struct {
			name       string
			dimension  string
			expected   int64
			observed   int64
			difference int64
			reason     string
			note       any
			constraint string
		}{
			{
				name: "zero difference", dimension: "CASH",
				expected: 100, observed: 100, difference: 0,
				reason: "CASH_COUNT_DIFFERENCE", note: nil,
				constraint: "shift_discrepancy_difference_nonzero",
			},
			{
				name: "invalid reason/dimension pair", dimension: "CASH",
				expected: 100, observed: 90, difference: -10,
				reason: "QR_OBSERVATION_DIFFERENCE", note: nil,
				constraint: "shift_discrepancy_reason_dimension_valid",
			},
			{
				name: "unknown dimension", dimension: "TIPS",
				expected: 100, observed: 90, difference: -10,
				reason: "UNEXPLAINED", note: nil,
				constraint: "shift_discrepancy_dimension_valid",
			},
			{
				name: "unknown reason", dimension: "CASH",
				expected: 100, observed: 90, difference: -10,
				reason: "COUNTING_ERROR", note: nil,
				// Every unknown reason also fails the reason/dimension pair
				// check, and PostgreSQL reports whichever it evaluates
				// first, so the assertion matches the reason-check family.
				constraint: "shift_discrepancy_reason",
			},
			{
				name: "non-OTHER note present", dimension: "CASH",
				expected: 100, observed: 90, difference: -10,
				reason: "UNEXPLAINED", note: "drawer was short",
				constraint: "shift_discrepancy_non_other_note_absent",
			},
			{
				name: "OTHER without note", dimension: "CASH",
				expected: 100, observed: 90, difference: -10,
				reason: "OTHER", note: nil,
				constraint: "shift_discrepancy_other_note_valid",
			},
			{
				name: "untrimmed OTHER note", dimension: "CASH",
				expected: 100, observed: 90, difference: -10,
				reason: "OTHER", note: "  padded  ",
				constraint: "shift_discrepancy_note_valid",
			},
			{
				name: "difference not observed minus expected", dimension: "CASH",
				expected: 100, observed: 90, difference: -5,
				reason: "CASH_COUNT_DIFFERENCE", note: nil,
				constraint: "shift_discrepancy_difference_equation",
			},
			{
				name: "negative observed amount", dimension: "CASH",
				expected: 100, observed: -1, difference: -101,
				reason: "CASH_COUNT_DIFFERENCE", note: nil,
				constraint: "shift_discrepancy_observed_vnd_valid",
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				insertErr := insertDiscrepancyRow(t, db, closureID, tc.dimension,
					tc.expected, tc.observed, tc.difference, tc.reason, tc.note)
				require.Error(t, insertErr)
				assert.Contains(t, insertErr.Error(), tc.constraint)
			})
		}

		// The valid shapes: CASH_COUNT_DIFFERENCE with no note, and OTHER with
		// a trimmed note.
		require.NoError(t, insertDiscrepancyRow(t, db, closureID, "CASH",
			100, 90, -10, "CASH_COUNT_DIFFERENCE", nil))
		require.NoError(t, insertDiscrepancyRow(t, db, closureID, "MANUAL_QR_RECEIVED",
			200, 180, -20, "OTHER", "khach bo qua lan cho"))

		// At most one row per closure and dimension.
		err = insertDiscrepancyRow(t, db, closureID, "CASH",
			100, 80, -20, "CASH_COUNT_DIFFERENCE", nil)
		require.Error(t, err, "a second row for the same dimension must fail")
		assert.Contains(t, err.Error(), "shift_discrepancy_closure_dimension_unique")
	})
}
