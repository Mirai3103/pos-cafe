//go:build integration

package shift_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/shift"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shiftFixture is a clean database with one open Sales Shift, a cashier who
// opened it, and a Manager who can approve Cash Movements.
type shiftFixture struct {
	DB       *sql.DB
	Queries  *sqlc.Queries
	Runner   *shift.Runner
	Cashier  testActor
	Manager  testActor
	Shift    shift.SalesShiftResponse
	Movement *shift.RecordCashMovementHandler
}

func newShiftFixture(t *testing.T) shiftFixture {
	t.Helper()
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	runner := shift.NewRunner(db, q)

	cashier := newTestActor(t, q, []string{"CASHIER"}, true)
	manager := newTestActorWithPin(t, q, []string{"MANAGER"}, true, "8642")

	_, opened, err := shift.NewOpenShiftHandler(runner).Handle(context.Background(), cashier.actor(),
		shift.OpenShiftCommand{RequestID: uuid.New(), OpeningFloatVND: int64Ptr(500000)})
	require.NoError(t, err)

	return shiftFixture{
		DB:       db,
		Queries:  q,
		Runner:   runner,
		Cashier:  cashier,
		Manager:  manager,
		Shift:    opened,
		Movement: shift.NewRecordCashMovementHandler(runner),
	}
}

func (f shiftFixture) command(method, reason string, amount int64, note *string) shift.RecordCashMovementCommand {
	return shift.RecordCashMovementCommand{
		RequestID:         uuid.New(),
		ShiftID:           f.Shift.ID,
		Method:            method,
		AmountVND:         &amount,
		Reason:            reason,
		Note:              note,
		ApproverLoginCode: f.Manager.LoginCode,
		ManagerPIN:        "8642",
	}
}

func TestCashMovementRecordsPayInAndPayOut(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()

	status, payIn, err := f.Movement.Handle(ctx, f.Cashier.actor(),
		f.command(shift.MethodPayIn, shift.ReasonAddChangeFund, 100000, nil))
	require.NoError(t, err)
	assert.Equal(t, 201, status)
	assert.Equal(t, int64(600000), payIn.ExpectedCashVND, "500000 float + 100000 pay in")
	assert.Equal(t, f.Cashier.StaffID, payIn.Movement.Initiator.ID)
	assert.Equal(t, f.Manager.StaffID, payIn.Movement.Approver.ID)
	assert.Nil(t, payIn.Movement.Note)

	_, payOut, err := f.Movement.Handle(ctx, f.Cashier.actor(),
		f.command(shift.MethodPayOut, shift.ReasonSafeDrop, 150000, nil))
	require.NoError(t, err)
	assert.Equal(t, int64(450000), payOut.ExpectedCashVND, "600000 - 150000 pay out")

	// Movements accumulate rather than replacing one another.
	_, third, err := f.Movement.Handle(ctx, f.Cashier.actor(),
		f.command(shift.MethodPayOut, shift.ReasonRemoveExcessFloat, 50000, nil))
	require.NoError(t, err)
	assert.Equal(t, int64(400000), third.ExpectedCashVND)
}

func TestCashMovementRequiresNoteForReasonOther(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()

	_, _, err := f.Movement.Handle(ctx, f.Cashier.actor(),
		f.command(shift.MethodPayOut, shift.ReasonOther, 50000, nil))
	require.Error(t, err)

	// A whitespace-only note normalizes to nil and must be rejected too.
	blank := "   "
	_, _, err = f.Movement.Handle(ctx, f.Cashier.actor(),
		f.command(shift.MethodPayOut, shift.ReasonOther, 50000, &blank))
	require.Error(t, err)

	note := "  mua da cho quay pha che  "
	_, res, err := f.Movement.Handle(ctx, f.Cashier.actor(),
		f.command(shift.MethodPayOut, shift.ReasonOther, 50000, &note))
	require.NoError(t, err)
	require.NotNil(t, res.Movement.Note)
	assert.Equal(t, "mua da cho quay pha che", *res.Movement.Note, "the note must be stored trimmed")
}

func TestCashMovementRejectsInvalidInput(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()

	cases := []struct {
		name   string
		method string
		reason string
		amount int64
	}{
		{"unknown method", "CASH_DROP", shift.ReasonSafeDrop, 50000},
		{"unknown reason", shift.MethodPayIn, "PETTY_CASH", 50000},
		{"zero amount", shift.MethodPayIn, shift.ReasonAddChangeFund, 0},
		{"negative amount", shift.MethodPayOut, shift.ReasonSafeDrop, -50000},
		{"over-bound amount", shift.MethodPayIn, shift.ReasonAddChangeFund, shift.MaxAmountVND + 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := f.Movement.Handle(ctx, f.Cashier.actor(),
				f.command(tc.method, tc.reason, tc.amount, nil))
			require.Error(t, err)
		})
	}
}

func TestCashMovementRequiresOpenShift(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()

	// A Shift that does not exist.
	cmd := f.command(shift.MethodPayIn, shift.ReasonAddChangeFund, 50000, nil)
	cmd.ShiftID = uuid.New()
	_, _, err := f.Movement.Handle(ctx, f.Cashier.actor(), cmd)
	require.Error(t, err)
	assert.ErrorIs(t, err, shift.ErrOpenShiftRequired)

	// A Shift that is no longer OPEN.
	_, err = f.DB.Exec(`UPDATE sales_shifts SET state = 'CLOSED' WHERE id = $1`, f.Shift.ID)
	require.NoError(t, err)

	_, _, err = f.Movement.Handle(ctx, f.Cashier.actor(),
		f.command(shift.MethodPayIn, shift.ReasonAddChangeFund, 50000, nil))
	require.Error(t, err)
	assert.ErrorIs(t, err, shift.ErrOpenShiftRequired)
}

func TestCashMovementApprovalRules(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()

	disabled := newTestActorWithPin(t, f.Queries, []string{"MANAGER"}, false, "8642")
	cashierApprover := newTestActorWithPin(t, f.Queries, []string{"CASHIER"}, true, "8642")

	cases := []struct {
		name      string
		loginCode string
		pin       string
	}{
		{"wrong pin", f.Manager.LoginCode, "0000"},
		{"unknown login code", "ZZUNKNOWN", "8642"},
		{"disabled manager", disabled.LoginCode, "8642"},
		{"cashier cannot approve", cashierApprover.LoginCode, "8642"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := f.command(shift.MethodPayOut, shift.ReasonSafeDrop, 50000, nil)
			cmd.ApproverLoginCode = tc.loginCode
			cmd.ManagerPIN = tc.pin

			_, _, err := f.Movement.Handle(ctx, f.Cashier.actor(), cmd)
			require.Error(t, err)
			assert.ErrorIs(t, err, shift.ErrManagerApprovalUnavailable,
				"every denial reason must collapse to one client-visible outcome")
		})
	}

	var recorded int
	require.NoError(t, f.DB.QueryRow(
		`SELECT count(*) FROM cash_movements WHERE sales_shift_id = $1`, f.Shift.ID).Scan(&recorded))
	assert.Equal(t, 0, recorded, "a denied approval must record nothing")
}

func TestCashMovementAllowsManagerSelfApproval(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	runner := shift.NewRunner(db, q)
	ctx := context.Background()

	// A Manager working alone supplies their own login code and PIN.
	manager := newTestActorWithPin(t, q, []string{"MANAGER"}, true, "8642")
	_, opened, err := shift.NewOpenShiftHandler(runner).Handle(ctx, manager.actor(),
		shift.OpenShiftCommand{RequestID: uuid.New(), OpeningFloatVND: int64Ptr(500000)})
	require.NoError(t, err)

	amount := int64(50000)
	_, res, err := shift.NewRecordCashMovementHandler(runner).Handle(ctx, manager.actor(),
		shift.RecordCashMovementCommand{
			RequestID:         uuid.New(),
			ShiftID:           opened.ID,
			Method:            shift.MethodPayOut,
			AmountVND:         &amount,
			Reason:            shift.ReasonSafeDrop,
			ApproverLoginCode: manager.LoginCode,
			ManagerPIN:        "8642",
		})
	require.NoError(t, err)

	// Initiator and approver are recorded separately, so a self-approved
	// movement stays distinguishable in the audit trail.
	assert.Equal(t, manager.StaffID, res.Movement.Initiator.ID)
	assert.Equal(t, manager.StaffID, res.Movement.Approver.ID)
}

func TestCashMovementIsIdempotent(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()

	cmd := f.command(shift.MethodPayOut, shift.ReasonSafeDrop, 50000, nil)

	_, first, err := f.Movement.Handle(ctx, f.Cashier.actor(), cmd)
	require.NoError(t, err)

	status, replay, err := f.Movement.Handle(ctx, f.Cashier.actor(), cmd)
	require.NoError(t, err)
	assert.Equal(t, 201, status)
	assert.Equal(t, first.Movement.ID, replay.Movement.ID)
	assert.Equal(t, first.ExpectedCashVND, replay.ExpectedCashVND)

	var recorded int
	require.NoError(t, f.DB.QueryRow(
		`SELECT count(*) FROM cash_movements WHERE sales_shift_id = $1`, f.Shift.ID).Scan(&recorded))
	assert.Equal(t, 1, recorded, "a replay must not record a second movement")

	// The same request_id with a different amount is a conflict.
	conflicting := cmd
	other := int64(70000)
	conflicting.AmountVND = &other
	_, _, err = f.Movement.Handle(ctx, f.Cashier.actor(), conflicting)
	require.Error(t, err)
	assert.ErrorIs(t, err, shift.ErrRequestConflict)
}

// TestCashMovementFingerprintExcludesManagerPin encodes spec §8.1: manager_pin
// is removed from the command before the request fingerprint is computed.
//
// Approval re-runs before the idempotency claim, so the replay's PIN must
// authenticate for the request to reach the fingerprint comparison at all — a
// merely wrong PIN dies with MANAGER_APPROVAL_UNAVAILABLE, and a different
// approver's login code changes the fingerprint and conflicts by design. The
// differing-but-valid PIN is therefore produced by rotating the SAME
// identity's PIN between the original request and the replay: the rotated PIN
// authenticates, and only the fingerprint decides the outcome. If a regression
// ever put the PIN into the fingerprint, the replay fails with
// ErrRequestConflict instead of returning the stored result.
func TestCashMovementFingerprintExcludesManagerPin(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()

	cmd := f.command(shift.MethodPayOut, shift.ReasonSafeDrop, 50000, nil)

	_, first, err := f.Movement.Handle(ctx, f.Cashier.actor(), cmd)
	require.NoError(t, err)

	// Rotate the approver's PIN, so the replay can authenticate with a PIN
	// different from the one the original request was approved with.
	newHash, err := auth.HashPin("9999")
	require.NoError(t, err)
	_, err = f.DB.Exec(`UPDATE staff_identities SET pin_hash = $1 WHERE id = $2`, newHash, f.Manager.StaffID)
	require.NoError(t, err)

	// Replay the exact same request — same request_id, same business fields,
	// same approver login code — with the rotated PIN.
	cmd.ManagerPIN = "9999"

	status, replay, err := f.Movement.Handle(ctx, f.Cashier.actor(), cmd)
	require.NoError(t, err)
	assert.Equal(t, 201, status)
	assert.Equal(t, first.Movement.ID, replay.Movement.ID)
	assert.Equal(t, first.ExpectedCashVND, replay.ExpectedCashVND)

	var recorded int
	require.NoError(t, f.DB.QueryRow(
		`SELECT count(*) FROM cash_movements WHERE sales_shift_id = $1`, f.Shift.ID).Scan(&recorded))
	assert.Equal(t, 1, recorded, "a PIN-insensitive replay must not record a second movement")
}

func TestCashMovementPinNeverPersisted(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()

	_, _, err := f.Movement.Handle(ctx, f.Cashier.actor(),
		f.command(shift.MethodPayOut, shift.ReasonSafeDrop, 50000, nil))
	require.NoError(t, err)

	// The PIN must reach neither the audit trail nor the idempotency record.
	var auditHits int
	require.NoError(t, f.DB.QueryRow(
		`SELECT count(*) FROM audit_events WHERE details::text LIKE '%8642%'`).Scan(&auditHits))
	assert.Equal(t, 0, auditHits)

	var idempotencyHits int
	require.NoError(t, f.DB.QueryRow(
		`SELECT count(*) FROM idempotency_keys
		 WHERE response_body::text LIKE '%8642%' OR request_hash LIKE '%8642%'`).Scan(&idempotencyHits))
	assert.Equal(t, 0, idempotencyHits)
}
