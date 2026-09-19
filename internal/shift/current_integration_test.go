//go:build integration

package shift_test

import (
	"context"
	"database/sql"
	"encoding/json"
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

// TestCurrentShiftReturnsRedactedOpenShape asserts the OPEN read's redacted
// contract: id, state, opened_at, and opener only. The Opening Float, Expected
// Cash, Cash Movements, and Refunds must not appear before the blind initial
// count commits (spec 4.1); the raw-JSON key allowlist is part of the
// blind-count boundary (spec 9.6).
func TestCurrentShiftReturnsRedactedOpenShape(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()

	res, err := shift.NewCurrentShiftHandler(f.Runner).Handle(ctx, f.Cashier.actor())
	require.NoError(t, err)
	require.NotNil(t, res)

	raw, err := json.Marshal(res)
	require.NoError(t, err)

	// The envelope carries exactly the four allowed keys and nothing else:
	// no money, no history, no reconciliation.
	var data map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &data))
	assert.ElementsMatch(t, []string{"id", "state", "opened_at", "opener"}, jsonKeys(data))

	var open shift.OpenCurrentShiftResponse
	require.NoError(t, json.Unmarshal(raw, &open))
	assert.Equal(t, f.Shift.ID, open.ID)
	assert.Equal(t, shift.StateOpen, open.State)
	assert.False(t, open.OpenedAt.IsZero())
	assert.Equal(t, f.Cashier.StaffID, open.Opener.ID)
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

// TestCurrentShiftReturnsClosingSnapshot pins the CLOSING read (spec 4.3,
// 9.5): the Shift metadata plus the frozen reconciliation — frozen totals,
// every attempt ordered by sequence, and the preview — with the exact key
// allowlists of spec 9.6 on every nested object.
func TestCurrentShiftReturnsClosingSnapshot(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()
	start := shift.NewStartReconciliationHandler(f.Runner)

	// Financial facts on a settled Check closed with its Session, so no
	// closure blocker trips: Expected Cash = 500000 + 115000 - 30000 - 20000
	// + 100000 - 30000 = 635000; Expected QR Received = 50000; Expected QR
	// Refunded = 10000.
	checkID := seedSettledCheck(t, f.DB, f.Shift.ID, f.Cashier.StaffID, f.Cashier.SessionID)
	seedPayment(t, f.DB, checkID, f.Shift.ID, f.Cashier.StaffID, f.Cashier.SessionID,
		"CASH", 85_000, 100_000)
	voided := seedPayment(t, f.DB, checkID, f.Shift.ID, f.Cashier.StaffID, f.Cashier.SessionID,
		"CASH", 30_000, 30_000)
	seedPaymentVoid(t, f.DB, voided, f.Shift.ID, f.Cashier.StaffID, f.Cashier.SessionID)
	seedPayment(t, f.DB, checkID, f.Shift.ID, f.Cashier.StaffID, f.Cashier.SessionID,
		"MANUAL_QR", 50_000, 0)
	completedAt := time.Now().Add(-time.Minute)
	seedRefund(t, f.DB, checkID, f.Shift.ID, f.Cashier.StaffID, f.Cashier.SessionID,
		"CASH", 20_000, nil, time.Now().Add(-2*time.Minute), &completedAt)
	seedRefund(t, f.DB, checkID, f.Shift.ID, f.Cashier.StaffID, f.Cashier.SessionID,
		"MANUAL_QR", 10_000, nil, time.Now().Add(-2*time.Minute), &completedAt)

	_, _, err := f.Movement.Handle(ctx, f.Cashier.actor(),
		f.command(shift.MethodPayIn, shift.ReasonAddChangeFund, 100_000, nil))
	require.NoError(t, err)
	_, _, err = f.Movement.Handle(ctx, f.Cashier.actor(),
		f.command(shift.MethodPayOut, shift.ReasonSafeDrop, 30_000, nil))
	require.NoError(t, err)

	// The blind count is 630000, a 5000 shortage; the recount and the QR
	// recheck land exactly, so every ledger carries its attempts and the
	// preview can close.
	counted := int64(630_000)
	_, startRes, err := start.Handle(ctx, f.Cashier.actor(),
		shift.StartReconciliationCommand{
			RequestID: uuid.New(), ShiftID: f.Shift.ID, CountedCashVND: &counted,
		})
	require.NoError(t, err)

	recount := int64(635_000)
	_, countRes, err := shift.NewRecordCashCountHandler(f.Runner).Handle(ctx, f.Cashier.actor(),
		shift.RecordCashCountCommand{
			RequestID: uuid.New(), ShiftID: f.Shift.ID, CountedCashVND: &recount,
		})
	require.NoError(t, err)
	_, obsRes, err := shift.NewRecordQRObservationHandler(f.Runner).Handle(ctx, f.Cashier.actor(),
		shift.RecordQRObservationCommand{
			RequestID:           uuid.New(),
			ShiftID:             f.Shift.ID,
			ObservedReceivedVND: int64Ptr(50_000),
			ObservedRefundedVND: int64Ptr(10_000),
		})
	require.NoError(t, err)

	res, err := shift.NewCurrentShiftHandler(f.Runner).Handle(ctx, f.Cashier.actor())
	require.NoError(t, err)
	require.NotNil(t, res, "a CLOSING Shift is active and must be returned")

	raw, err := json.Marshal(res)
	require.NoError(t, err)

	// The envelope is the closing shape itself — metadata plus reconciliation,
	// with no wrapper key naming the branch (spec 9.5, 9.6).
	var data map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &data))
	assert.ElementsMatch(t,
		[]string{"id", "state", "opened_at", "opener", "reconciliation"}, jsonKeys(data))

	var closing shift.ClosingShiftResponse
	require.NoError(t, json.Unmarshal(raw, &closing))
	assert.Equal(t, f.Shift.ID, closing.ID)
	assert.Equal(t, shift.StateClosing, closing.State)
	assert.Equal(t, f.Cashier.StaffID, closing.Opener.ID)

	// The frozen snapshot values, identical to what start revealed.
	recon := closing.Reconciliation
	assert.Equal(t, startRes.Reconciliation.ID, recon.ID)
	assert.Equal(t, f.Cashier.StaffID, recon.Starter.ID)
	assert.False(t, recon.StartedAt.IsZero())

	assert.Equal(t, int64(500_000), recon.OpeningFloatVND)
	assert.Equal(t, int64(100_000), recon.PayInVND)
	assert.Equal(t, int64(30_000), recon.PayOutVND)
	assert.Equal(t, int64(115_000), recon.CashPaymentVND)
	assert.Equal(t, int64(30_000), recon.CashPaymentVoidVND)
	assert.Equal(t, int64(20_000), recon.CashRefundVND)
	assert.Equal(t, int64(635_000), recon.ExpectedCashVND)
	assert.Equal(t, int64(50_000), recon.ManualQRPaymentVND)
	assert.Equal(t, int64(0), recon.ManualQRPaymentVoidVND)
	assert.Equal(t, int64(50_000), recon.ExpectedManualQRReceivedVND)
	assert.Equal(t, int64(10_000), recon.ManualQRRefundVND)
	assert.Equal(t, int64(0), recon.PendingManualQRRefundVND)
	assert.Equal(t, int64(0), recon.PendingRefundVND)
	assert.Equal(t, int64(0), recon.UnresolvedPostSaleAdjustmentVND)

	// Every attempt ordered by sequence: the blind count, then the recount.
	require.Len(t, recon.CashCounts, 2)
	assert.Equal(t, 1, recon.CashCounts[0].Sequence)
	assert.Equal(t, counted, recon.CashCounts[0].CountedCashVND)
	assert.Equal(t, 2, recon.CashCounts[1].Sequence)
	assert.Equal(t, recount, recon.CashCounts[1].CountedCashVND)
	assert.Equal(t, countRes.CashCount.ID, recon.CashCounts[1].ID)

	require.Len(t, recon.QRObservations, 1)
	assert.Equal(t, 1, recon.QRObservations[0].Sequence)
	assert.Equal(t, int64(50_000), recon.QRObservations[0].ObservedReceivedVND)
	assert.Equal(t, int64(10_000), recon.QRObservations[0].ObservedRefundedVND)
	assert.Equal(t, obsRes.QRObservation.ID, recon.QRObservations[0].ID)

	// The preview compares the frozen expectations with the latest evidence:
	// exact on all three dimensions, so the Shift can close exactly.
	require.Len(t, recon.Preview.Dimensions, 3)
	cash := recon.Preview.Dimensions[0]
	assert.Equal(t, shift.DiscrepancyDimension(shift.DimensionCash), cash.Dimension)
	assert.Equal(t, int64(635_000), cash.ExpectedVND)
	require.NotNil(t, cash.ObservedVND)
	assert.Equal(t, int64(635_000), *cash.ObservedVND)
	require.NotNil(t, cash.DifferenceVND)
	assert.Equal(t, int64(0), *cash.DifferenceVND)
	assert.False(t, cash.RecheckRequired)

	qrReceived := recon.Preview.Dimensions[1]
	assert.Equal(t, shift.DiscrepancyDimension(shift.DimensionManualQRReceived), qrReceived.Dimension)
	require.NotNil(t, qrReceived.ObservedVND)
	assert.Equal(t, int64(50_000), *qrReceived.ObservedVND)
	require.NotNil(t, qrReceived.DifferenceVND)
	assert.Equal(t, int64(0), *qrReceived.DifferenceVND)

	qrRefunded := recon.Preview.Dimensions[2]
	assert.Equal(t, shift.DiscrepancyDimension(shift.DimensionManualQRRefunded), qrRefunded.Dimension)
	require.NotNil(t, qrRefunded.ObservedVND)
	assert.Equal(t, int64(10_000), *qrRefunded.ObservedVND)

	assert.True(t, recon.Preview.CanClose)

	// Raw JSON key allowlists (spec 9.6) on the serialized shape the HTTP
	// layer sends: reconciliation, attempts, and preview entries each carry
	// exactly their documented keys.
	var reconData map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data["reconciliation"], &reconData))
	assert.ElementsMatch(t, []string{
		"id", "starter", "started_at",
		"opening_float_vnd", "pay_in_vnd", "pay_out_vnd",
		"cash_payment_vnd", "cash_payment_void_vnd", "cash_refund_vnd",
		"expected_cash_vnd", "manual_qr_payment_vnd", "manual_qr_payment_void_vnd",
		"expected_manual_qr_received_vnd", "manual_qr_refund_vnd",
		"pending_manual_qr_refund_vnd", "pending_refund_vnd",
		"unresolved_post_sale_adjustment_vnd",
		"cash_counts", "qr_observations", "preview",
	}, jsonKeys(reconData))

	var countsRaw []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(reconData["cash_counts"], &countsRaw))
	require.Len(t, countsRaw, 2)
	for _, countData := range countsRaw {
		assert.ElementsMatch(t,
			[]string{"id", "sequence", "counted_cash_vnd", "counted_by", "counted_at"},
			jsonKeys(countData))
	}

	var obsRaw []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(reconData["qr_observations"], &obsRaw))
	require.Len(t, obsRaw, 1)
	assert.ElementsMatch(t,
		[]string{"id", "sequence", "observed_received_vnd", "observed_refunded_vnd",
			"observed_by", "observed_at"},
		jsonKeys(obsRaw[0]))

	var previewData map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(reconData["preview"], &previewData))
	assert.ElementsMatch(t, []string{"dimensions", "can_close"}, jsonKeys(previewData))

	var dimsRaw []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(previewData["dimensions"], &dimsRaw))
	require.Len(t, dimsRaw, 3)
	for _, dimData := range dimsRaw {
		assert.ElementsMatch(t,
			[]string{"dimension", "expected_vnd", "observed_vnd", "difference_vnd", "recheck_required"},
			jsonKeys(dimData))
	}
}

// TestCurrentShiftClosingKeepsFrozenSnapshotAfterLiveMutation is the corruption
// test: live money rows inserted after the snapshot committed, bypassing the
// API, must not change the CLOSING read. The projection reads only
// shift_reconciliations and the attempt ledgers — it never recalculates
// expected values from unrestricted current data (spec 4.3).
func TestCurrentShiftClosingKeepsFrozenSnapshotAfterLiveMutation(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()
	current := shift.NewCurrentShiftHandler(f.Runner)

	counted := int64(500_000)
	_, _, err := shift.NewStartReconciliationHandler(f.Runner).Handle(ctx, f.Cashier.actor(),
		f.startCommand(counted, uuid.New()))
	require.NoError(t, err)

	before, err := current.Handle(ctx, f.Cashier.actor())
	require.NoError(t, err)
	require.NotNil(t, before, "the Shift is CLOSING and the read must reveal the snapshot")
	beforeRaw, err := json.Marshal(before)
	require.NoError(t, err)

	// Raw live data after start: a Pay In movement and a Cash Payment on a
	// settled Check, both attributed to the Shift outside any API path.
	_, err = f.DB.Exec(`
		INSERT INTO cash_movements (sales_shift_id, method, amount_vnd, reason,
		                            initiated_by_staff_identity_id,
		                            initiated_staff_access_session_id,
		                            approved_by_staff_identity_id)
		VALUES ($1, 'PAY_IN', 999000, 'ADD_CHANGE_FUND', $2, $3, $2)`,
		f.Shift.ID, f.Cashier.StaffID, f.Cashier.SessionID)
	require.NoError(t, err)
	checkID := seedSettledCheck(t, f.DB, f.Shift.ID, f.Cashier.StaffID, f.Cashier.SessionID)
	seedPayment(t, f.DB, checkID, f.Shift.ID, f.Cashier.StaffID, f.Cashier.SessionID,
		"CASH", 250_000, 250_000)

	after, err := current.Handle(ctx, f.Cashier.actor())
	require.NoError(t, err)
	require.NotNil(t, after)
	afterRaw, err := json.Marshal(after)
	require.NoError(t, err)

	assert.JSONEq(t, string(beforeRaw), string(afterRaw),
		"the CLOSING read is the frozen snapshot; live data must not change it")

	var closing shift.ClosingShiftResponse
	require.NoError(t, json.Unmarshal(afterRaw, &closing))
	assert.Equal(t, int64(500_000), closing.Reconciliation.ExpectedCashVND,
		"expected cash stays frozen at the snapshot's value")
	assert.Equal(t, int64(0), closing.Reconciliation.PayInVND,
		"the post-start Pay In never enters the frozen movement sum")
	assert.Equal(t, int64(0), closing.Reconciliation.CashPaymentVND,
		"the post-start Payment never enters the frozen payment sum")
}

// TestCurrentShiftClosingRejectsOpenShiftOnlyCommands pins the CLOSING side of
// the lifecycle gate (spec 4.3): every ordinary command that requires an OPEN
// Shift rejects once the Shift is CLOSING, through the existing open-shift
// gate — a state = 'OPEN' lookup that finds no row.
//
// The sales slice's Payment and Refund commands enforce the same gate the same
// way (lockOpenSalesShift filters state = 'OPEN' and checkPreconditions then
// reports its open-shift-required error), but the shift test package must not
// import internal/sales, so this test asserts the rejection through the shift
// boundary's own open-shift-required command, the Cash Movement.
func TestCurrentShiftClosingRejectsOpenShiftOnlyCommands(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()

	_, _, err := shift.NewStartReconciliationHandler(f.Runner).Handle(ctx, f.Cashier.actor(),
		f.startCommand(500_000, uuid.New()))
	require.NoError(t, err)

	_, _, err = f.Movement.Handle(ctx, f.Cashier.actor(),
		f.command(shift.MethodPayIn, shift.ReasonAddChangeFund, 100_000, nil))
	require.Error(t, err, "a CLOSING Shift has no open-shift row to attach money to")
	assert.ErrorIs(t, err, shift.ErrOpenShiftRequired)
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

// TestMovementRecordsAgainstItsOwnShift guards the Shift-scoped insert: a
// movement recorded against the open Shift carries exactly that Shift's id.
func TestMovementRecordsAgainstItsOwnShift(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()

	_, res, err := f.Movement.Handle(ctx, f.Cashier.actor(),
		f.command(shift.MethodPayIn, shift.ReasonAddChangeFund, 100000, nil))
	require.NoError(t, err)
	assert.Equal(t, f.Shift.ID, res.Movement.SalesShiftID)
	assert.NotEqual(t, uuid.Nil, res.Movement.SalesShiftID)
}
