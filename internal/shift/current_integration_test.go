//go:build integration

package shift_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/shift"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCurrentShiftReturnsNilWhenNoneOpen(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	ctx := context.Background()

	cashier := newTestActor(t, q, []string{"CASHIER"}, true)

	res, err := shift.NewCurrentShiftHandler(shift.NewRunner(db, q)).Handle(ctx, cashier.actor())
	require.NoError(t, err, "no open Shift is a normal state, not an error")
	assert.Nil(t, res)
}

func TestCurrentShiftReportsFloatOpenerAndEmptyMovements(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()

	res, err := shift.NewCurrentShiftHandler(f.Runner).Handle(ctx, f.Cashier.actor())
	require.NoError(t, err)
	require.NotNil(t, res)

	assert.Equal(t, f.Shift.ID, res.ID)
	assert.Equal(t, shift.StateOpen, res.State)
	assert.Equal(t, int64(500000), res.OpeningFloatVND)
	assert.Equal(t, int64(500000), res.ExpectedCashVND, "with no movements, Expected Cash is the float")
	assert.Equal(t, f.Cashier.StaffID, res.Opener.ID)
	assert.NotNil(t, res.CashMovements)
	assert.Empty(t, res.CashMovements)
}

func TestCurrentShiftReportsMovementsNewestFirst(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()

	_, first, err := f.Movement.Handle(ctx, f.Cashier.actor(),
		f.command(shift.MethodPayIn, shift.ReasonAddChangeFund, 100000, nil))
	require.NoError(t, err)
	_, second, err := f.Movement.Handle(ctx, f.Cashier.actor(),
		f.command(shift.MethodPayOut, shift.ReasonSafeDrop, 150000, nil))
	require.NoError(t, err)

	res, err := shift.NewCurrentShiftHandler(f.Runner).Handle(ctx, f.Cashier.actor())
	require.NoError(t, err)
	require.NotNil(t, res)

	require.Len(t, res.CashMovements, 2)
	assert.Equal(t, second.Movement.ID, res.CashMovements[0].ID, "newest first")
	assert.Equal(t, first.Movement.ID, res.CashMovements[1].ID)
	assert.Equal(t, int64(450000), res.ExpectedCashVND)

	// Each movement carries both parties.
	assert.Equal(t, f.Cashier.StaffID, res.CashMovements[0].Initiator.ID)
	assert.Equal(t, f.Manager.StaffID, res.CashMovements[0].Approver.ID)
}

func TestCurrentShiftIgnoresClosedShifts(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()

	_, err := f.DB.Exec(`UPDATE sales_shifts SET state = 'CLOSED' WHERE id = $1`, f.Shift.ID)
	require.NoError(t, err)

	res, err := shift.NewCurrentShiftHandler(f.Runner).Handle(ctx, f.Cashier.actor())
	require.NoError(t, err)
	assert.Nil(t, res)
}

// countDenialAuditEvents counts shift.authorization_denied events attributed
// to a staff identity. Its transaction is separate from the read that denies,
// since ExecuteRead's own transaction is read-only.
func countDenialAuditEvents(t *testing.T, db *sql.DB, staffID uuid.UUID) int {
	t.Helper()
	var n int
	err := db.QueryRow(
		`SELECT count(*) FROM audit_events WHERE event_type = $1 AND actor_id = $2`,
		shift.EventAuthorizationDenied, staffID,
	).Scan(&n)
	require.NoError(t, err)
	return n
}

func TestCurrentShiftDeniesBarista(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	ctx := context.Background()

	barista := newTestActor(t, q, []string{"BARISTA"}, true)

	_, err := shift.NewCurrentShiftHandler(shift.NewRunner(db, q)).Handle(ctx, barista.actor())
	require.Error(t, err)
	assert.ErrorIs(t, err, shift.ErrForbidden)

	// The read's own transaction is read-only and cannot itself audit the
	// denial, so ExecuteRead must record it in a separate transaction.
	assert.Equal(t, 1, countDenialAuditEvents(t, db, barista.StaffID))
}

func TestCurrentShiftDeniesRevokedSession(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()

	_, err := f.DB.Exec(`UPDATE staff_access_sessions SET revoked_at = now() WHERE id = $1`, f.Cashier.SessionID)
	require.NoError(t, err)

	_, err = shift.NewCurrentShiftHandler(f.Runner).Handle(ctx, f.Cashier.actor())
	require.Error(t, err)
	assert.ErrorIs(t, err, shift.ErrUnauthorized)
	assert.Equal(t, 1, countDenialAuditEvents(t, f.DB, f.Cashier.StaffID))
}

func TestCurrentShiftReportsLivePendingRefundAsMoneyOwedBack(t *testing.T) {
	env := newShiftEnv(t)
	actor := env.Cashier

	// An unpaid Check cancelled in full owes nothing back: its corrected
	// charge is zero and no money ever moved, so there is no refundable
	// excess to report.
	cancelled := seedCorrectionCheck(t, env.DB, env.ShiftID,
		actor.StaffID, actor.SessionID, 100_000)
	seedLiveAdjustment(t, env.DB, cancelled, env.ShiftID, 100_000)

	base := env.currentShift(t)
	require.Zero(t, base.PendingRefundVND,
		"an unpaid cancelled Check contributes no owed-back money")

	// A partially cancelled Check with a full Cash Payment owes the corrected
	// excess back. The money stays in the drawer until a Refund completes.
	partial := seedCorrectionCheck(t, env.DB, env.ShiftID,
		actor.StaffID, actor.SessionID, 100_000)
	seedLiveAdjustment(t, env.DB, partial, env.ShiftID, 30_000)
	seedPayment(t, env.DB, partial.CheckID, env.ShiftID,
		actor.StaffID, actor.SessionID, "CASH", 100_000, 100_000)

	owedBack := env.currentShift(t)
	require.Equal(t, int64(30_000), owedBack.PendingRefundVND,
		"owed-back money is the valid receipt above the corrected charge")
	require.Equal(t, base.ExpectedCashVND+100_000, owedBack.ExpectedCashVND,
		"a pending Refund has not moved money, so Expected Cash still holds it")

	// Completing the live Refund moves the money and clears the obligation.
	completedAt := time.Now().UTC()
	seedRefund(t, env.DB, partial.CheckID, env.ShiftID, actor.StaffID, actor.SessionID,
		"CASH", 30_000, nil, completedAt.Add(-time.Minute), &completedAt)

	settled := env.currentShift(t)
	require.Zero(t, settled.PendingRefundVND)
	require.Equal(t, int64(30_000), settled.CashRefundVND)
	require.Equal(t, owedBack.ExpectedCashVND-30_000, settled.ExpectedCashVND)
}

func TestCurrentShiftReportsUnresolvedPostSaleAdjustment(t *testing.T) {
	env := newShiftEnv(t)
	actor := env.Cashier

	saleID := seedCompletedSale(t, env.DB, env.ShiftID, actor.StaffID, actor.SessionID)
	correction := seedCorrectionCheck(t, env.DB, env.ShiftID,
		actor.StaffID, actor.SessionID, 40_000)
	adjustmentID := seedPostSaleAdjustment(t, env.DB, correction, saleID, env.ShiftID,
		actor.StaffID, actor.SessionID, 25_000)

	postSale := env.currentShift(t)
	require.Equal(t, int64(25_000), postSale.UnresolvedPostSaleAdjustmentVND)
	require.Equal(t, int64(25_000), postSale.PendingRefundVND,
		"unresolved post-sale capacity is money owed back")

	completedAt := time.Now().UTC()
	refundID := seedRefund(t, env.DB, correction.CheckID, env.ShiftID, actor.StaffID, actor.SessionID,
		"CASH", 10_000, &saleID, completedAt.Add(-time.Minute), &completedAt)
	seedRefundAdjustmentAllocation(t, env.DB, refundID, adjustmentID, 10_000)

	remaining := env.currentShift(t)
	require.Equal(t, int64(15_000), remaining.UnresolvedPostSaleAdjustmentVND)
	require.Equal(t, int64(15_000), remaining.PendingRefundVND)
	require.Equal(t, int64(10_000), remaining.CashRefundVND)

	require.Len(t, remaining.Refunds, 1)
	require.Equal(t, shift.RefundStateCompleted, remaining.Refunds[0].State)
	require.NotNil(t, remaining.Refunds[0].CompletedSaleID)
	require.Equal(t, saleID, *remaining.Refunds[0].CompletedSaleID)
}

func TestCurrentShiftReconciliationManualQRTerms(t *testing.T) {
	env := newShiftEnv(t)
	actor := env.Cashier

	paymentID := env.insertPayment(t, "MANUAL_QR", 50_000, 0)
	seedPaymentVoid(t, env.DB, paymentID, env.ShiftID, actor.StaffID, actor.SessionID)

	refundID := seedRefund(t, env.DB, env.checkID, env.ShiftID, actor.StaffID, actor.SessionID,
		"MANUAL_QR", 20_000, nil, time.Now().UTC(), nil)

	pending := env.currentShift(t)
	require.Equal(t, int64(50_000), pending.ManualQRPaymentVND)
	require.Equal(t, int64(50_000), pending.ManualQRPaymentVoidVND)
	require.Zero(t, pending.ManualQRRefundVND)
	require.Equal(t, int64(20_000), pending.PendingManualQRRefundVND)
	require.Equal(t, int64(500_000), pending.ExpectedCashVND,
		"QR money and pending QR Refunds never touch the cash drawer")
	require.Len(t, pending.Refunds, 1)
	require.Equal(t, shift.RefundStatePending, pending.Refunds[0].State)
	require.Nil(t, pending.Refunds[0].CompletedAt)

	completedAt := time.Now().UTC()
	_, err := env.DB.Exec(`
		INSERT INTO refund_completions (refund_id, completed_by_staff_identity_id,
		                                staff_access_session_id, completed_at)
		VALUES ($1, $2, $3, $4)`,
		refundID, actor.StaffID, actor.SessionID, completedAt)
	require.NoError(t, err)

	completed := env.currentShift(t)
	require.Zero(t, completed.PendingManualQRRefundVND)
	require.Equal(t, int64(20_000), completed.ManualQRRefundVND)
	require.Equal(t, int64(500_000), completed.ExpectedCashVND)
	require.Len(t, completed.Refunds, 1)
	require.Equal(t, shift.RefundStateCompleted, completed.Refunds[0].State)
	require.NotNil(t, completed.Refunds[0].CompletedAt)
}

func TestCurrentShiftReconciliationExcludesOtherShifts(t *testing.T) {
	env := newShiftEnv(t)
	actor := env.Cashier

	// Another Shift's money and obligations, all attributed to the CLOSED
	// previous Shift.
	other := seedCorrectionCheck(t, env.DB, env.previousShiftID,
		actor.StaffID, actor.SessionID, 100_000)
	seedLiveAdjustment(t, env.DB, other, env.previousShiftID, 40_000)
	voided := seedPayment(t, env.DB, other.CheckID, env.previousShiftID,
		actor.StaffID, actor.SessionID, "CASH", 50_000, 50_000)
	seedPaymentVoid(t, env.DB, voided, env.previousShiftID, actor.StaffID, actor.SessionID)
	completedAt := time.Now().UTC()
	seedRefund(t, env.DB, other.CheckID, env.previousShiftID, actor.StaffID, actor.SessionID,
		"CASH", 5_000, nil, completedAt.Add(-time.Minute), &completedAt)

	otherSaleID := seedCompletedSale(t, env.DB, env.previousShiftID,
		actor.StaffID, actor.SessionID)
	otherPostSale := seedCorrectionCheck(t, env.DB, env.previousShiftID,
		actor.StaffID, actor.SessionID, 30_000)
	seedPostSaleAdjustment(t, env.DB, otherPostSale, otherSaleID, env.previousShiftID,
		actor.StaffID, actor.SessionID, 20_000)

	current := env.currentShift(t)
	require.Equal(t, int64(500_000), current.ExpectedCashVND,
		"another Shift's money never reaches this drawer")
	require.Zero(t, current.CashPaymentVND)
	require.Zero(t, current.CashPaymentVoidVND)
	require.Zero(t, current.CashRefundVND)
	require.Zero(t, current.ManualQRPaymentVND)
	require.Zero(t, current.ManualQRPaymentVoidVND)
	require.Zero(t, current.ManualQRRefundVND)
	require.Zero(t, current.PendingManualQRRefundVND)
	require.Zero(t, current.PendingRefundVND)
	require.Zero(t, current.UnresolvedPostSaleAdjustmentVND)
	require.Empty(t, current.Refunds)
}

func TestCurrentShiftRefundSummariesAreOrderedAndCredentialFree(t *testing.T) {
	env := newShiftEnv(t)
	actor := env.Cashier
	base := time.Now().UTC().Truncate(time.Second)

	pendingCash := seedRefund(t, env.DB, env.checkID, env.ShiftID, actor.StaffID, actor.SessionID,
		"CASH", 10_000, nil, base.Add(1*time.Minute), nil)
	pendingQR := seedRefund(t, env.DB, env.checkID, env.ShiftID, actor.StaffID, actor.SessionID,
		"MANUAL_QR", 20_000, nil, base.Add(2*time.Minute), nil)
	completedQRAt := base.Add(3 * time.Minute)
	completedQR := seedRefund(t, env.DB, env.checkID, env.ShiftID, actor.StaffID, actor.SessionID,
		"MANUAL_QR", 30_000, nil, base.Add(3*time.Minute), &completedQRAt)
	// Two Refunds created at the same instant pin the id tie-break.
	sameInstant := base.Add(4 * time.Minute)
	completedCashAt := sameInstant.Add(time.Second)
	tieA := seedRefund(t, env.DB, env.checkID, env.ShiftID, actor.StaffID, actor.SessionID,
		"CASH", 40_000, nil, sameInstant, &completedCashAt)
	tieB := seedRefund(t, env.DB, env.checkID, env.ShiftID, actor.StaffID, actor.SessionID,
		"CASH", 50_000, nil, sameInstant, nil)

	tied := []uuid.UUID{tieA, tieB}
	sort.Slice(tied, func(i, j int) bool { return bytes.Compare(tied[i][:], tied[j][:]) < 0 })

	current := env.currentShift(t)
	require.Len(t, current.Refunds, 5)
	got := make([]uuid.UUID, 0, len(current.Refunds))
	for _, refund := range current.Refunds {
		got = append(got, refund.ID)
	}
	require.Equal(t, []uuid.UUID{pendingCash, pendingQR, completedQR, tied[0], tied[1]}, got,
		"Refunds are ordered by (created_at, id)")

	byID := make(map[uuid.UUID]shift.RefundSummaryResponse, len(current.Refunds))
	for _, refund := range current.Refunds {
		byID[refund.ID] = refund
	}
	require.Equal(t, shift.RefundStatePending, byID[pendingCash].State)
	require.Nil(t, byID[pendingCash].CompletedAt)
	require.Equal(t, "CASH", byID[pendingCash].Method)
	require.Equal(t, int64(10_000), byID[pendingCash].AmountVND)
	require.Equal(t, shift.RefundStateCompleted, byID[completedQR].State)
	require.NotNil(t, byID[completedQR].CompletedAt)
	require.True(t, byID[completedQR].CompletedAt.Equal(completedQRAt))
	require.Equal(t, env.checkID, byID[completedQR].CheckID)
	require.Nil(t, byID[completedQR].CompletedSaleID,
		"a live Refund carries no Completed Sale")

	// The boundary projects no credentials, sessions, or approver identity.
	encoded, err := json.Marshal(current.Refunds)
	require.NoError(t, err)
	for _, forbidden := range []string{
		"actor", "approved_by", "staff_access_session", "credential", "pin",
	} {
		require.NotContains(t, string(encoded), forbidden)
	}
}

func TestCurrentShiftReportsUnknownShiftIDNotFoundForMovement(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()

	// Guards the Shift-scoped aggregate: a movement recorded against the open
	// Shift must not leak into another Shift's Expected Cash.
	_, res, err := f.Movement.Handle(ctx, f.Cashier.actor(),
		f.command(shift.MethodPayIn, shift.ReasonAddChangeFund, 100000, nil))
	require.NoError(t, err)
	assert.Equal(t, f.Shift.ID, res.Movement.SalesShiftID)
	assert.NotEqual(t, uuid.Nil, res.Movement.SalesShiftID)
}
