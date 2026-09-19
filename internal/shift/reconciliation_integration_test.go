//go:build integration

package shift_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/Mirai3103/pos-cafe/internal/shift"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startCommand builds a Start Reconciliation command against the fixture's
// Shift with the given counted cash.
func (f shiftFixture) startCommand(counted int64, requestID uuid.UUID) shift.StartReconciliationCommand {
	return shift.StartReconciliationCommand{
		RequestID:      requestID,
		ShiftID:        f.Shift.ID,
		CountedCashVND: int64Ptr(counted),
	}
}

// requireStartBlocked runs one start attempt and asserts it is rejected with
// the expected sentinel, mapped to a 409 with the expected stable code, while
// no reconciliation row is written and the Shift stays OPEN (spec 4.2: if any
// step fails the Shift remains OPEN and the initial count is not stored).
func requireStartBlocked(t *testing.T, f shiftFixture,
	start *shift.StartReconciliationHandler, sentinel error, code string,
) {
	t.Helper()
	ctx := context.Background()

	_, _, err := start.Handle(ctx, f.Cashier.actor(), f.startCommand(1000, uuid.New()))
	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel)

	mapped := shift.MapHTTPError(err)
	var coded *response.CodedError
	require.True(t, errors.As(mapped, &coded), "expected a *response.CodedError")
	assert.Equal(t, http.StatusConflict, coded.Status)
	assert.Equal(t, code, coded.Code)

	var reconRows int
	require.NoError(t, f.DB.QueryRow(`SELECT count(*) FROM shift_reconciliations`).Scan(&reconRows))
	assert.Equal(t, 0, reconRows, "a blocked start writes no reconciliation row")

	var countRows int
	require.NoError(t, f.DB.QueryRow(`SELECT count(*) FROM shift_cash_counts`).Scan(&countRows))
	assert.Equal(t, 0, countRows, "a blocked start stores no initial count")

	var state string
	require.NoError(t, f.DB.QueryRow(
		`SELECT state FROM sales_shifts WHERE id = $1`, f.Shift.ID).Scan(&state))
	assert.Equal(t, shift.StateOpen, state, "a blocked start leaves the Shift OPEN")
}

func TestStartReconciliationFreezesSnapshotAndInitialCount(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()
	start := shift.NewStartReconciliationHandler(f.Runner)

	// Financial facts on a settled Check closed with its Session, so no
	// closure blocker trips.
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

	// Expected Cash = 500000 + 115000 - 30000 - 20000 + 100000 - 30000 = 635000.
	counted := int64(635_000)
	status, res, err := start.Handle(ctx, f.Cashier.actor(),
		shift.StartReconciliationCommand{
			RequestID: uuid.New(), ShiftID: f.Shift.ID, CountedCashVND: &counted,
		})
	require.NoError(t, err)
	assert.Equal(t, 201, status)

	assert.Equal(t, f.Shift.ID, res.ID)
	assert.Equal(t, shift.StateClosing, res.State)
	assert.Equal(t, f.Cashier.StaffID, res.Opener.ID)

	recon := res.Reconciliation
	assert.NotEqual(t, uuid.Nil, recon.ID)
	assert.Equal(t, f.Cashier.StaffID, recon.Starter.ID)
	assert.False(t, recon.StartedAt.IsZero())

	assert.Equal(t, int64(500_000), recon.OpeningFloatVND)
	assert.Equal(t, int64(100_000), recon.PayInVND)
	assert.Equal(t, int64(30_000), recon.PayOutVND)
	assert.Equal(t, int64(115_000), recon.CashPaymentVND,
		"cash payments count original applied amounts, voided ones included")
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

	require.Len(t, recon.CashCounts, 1)
	first := recon.CashCounts[0]
	assert.Equal(t, 1, first.Sequence, "the blind initial count is sequence 1")
	assert.Equal(t, int64(635_000), first.CountedCashVND)
	assert.Equal(t, f.Cashier.StaffID, first.CountedBy.ID)
	assert.False(t, first.CountedAt.IsZero())

	assert.NotNil(t, recon.QRObservations)
	assert.Empty(t, recon.QRObservations)

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
	assert.Equal(t, int64(50_000), qrReceived.ExpectedVND)
	assert.Nil(t, qrReceived.ObservedVND, "no QR observation exists yet")
	assert.Nil(t, qrReceived.DifferenceVND)

	qrRefunded := recon.Preview.Dimensions[2]
	assert.Equal(t, shift.DiscrepancyDimension(shift.DimensionManualQRRefunded), qrRefunded.Dimension)
	assert.Equal(t, int64(10_000), qrRefunded.ExpectedVND)
	assert.Nil(t, qrRefunded.ObservedVND)
	assert.Nil(t, qrRefunded.DifferenceVND)

	assert.False(t, recon.Preview.CanClose,
		"at least one QR observation is required before the Shift can close")

	// Persisted facts: the Shift is CLOSING with exactly one reconciliation
	// and one sequence-1 count.
	var state string
	require.NoError(t, f.DB.QueryRow(
		`SELECT state FROM sales_shifts WHERE id = $1`, f.Shift.ID).Scan(&state))
	assert.Equal(t, shift.StateClosing, state)

	var reconRows int
	require.NoError(t, f.DB.QueryRow(
		`SELECT count(*) FROM shift_reconciliations WHERE sales_shift_id = $1`,
		f.Shift.ID).Scan(&reconRows))
	assert.Equal(t, 1, reconRows)

	var countRows int
	require.NoError(t, f.DB.QueryRow(
		`SELECT count(*) FROM shift_cash_counts WHERE sequence = 1`).Scan(&countRows))
	assert.Equal(t, 1, countRows)

	// Both start and initial-count audit events in the same transaction.
	assert.Equal(t, 1, countAuditEvents(t, f.DB, shift.EventReconciliationStarted, f.Shift.ID))
	assert.Equal(t, 1, countAuditEvents(t, f.DB, shift.EventCashCountRecorded, f.Shift.ID))
}

// TestStartReconciliationAcceptsExplicitZeroCount pins the pointer boundary's
// other half (spec 9.1): an explicit zero is a meaningful count — an empty
// drawer — so it starts reconciliation and persists as the sequence-1 count,
// here as a 500000 shortage that requires a recount before closure.
func TestStartReconciliationAcceptsExplicitZeroCount(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()
	start := shift.NewStartReconciliationHandler(f.Runner)

	status, res, err := start.Handle(ctx, f.Cashier.actor(), f.startCommand(0, uuid.New()))
	require.NoError(t, err)
	assert.Equal(t, 201, status)
	assert.Equal(t, shift.StateClosing, res.State)
	assert.Equal(t, int64(500_000), res.Reconciliation.ExpectedCashVND,
		"the empty fixture's Expected Cash is its Opening Float")

	require.Len(t, res.Reconciliation.CashCounts, 1)
	first := res.Reconciliation.CashCounts[0]
	assert.Equal(t, 1, first.Sequence)
	assert.Equal(t, int64(0), first.CountedCashVND)

	require.Len(t, res.Reconciliation.Preview.Dimensions, 3)
	cash := res.Reconciliation.Preview.Dimensions[0]
	require.NotNil(t, cash.ObservedVND, "a present zero count is a meaningful observed value")
	assert.Equal(t, int64(0), *cash.ObservedVND)
	require.NotNil(t, cash.DifferenceVND)
	assert.Equal(t, int64(-500_000), *cash.DifferenceVND)
	assert.True(t, cash.RecheckRequired)
	assert.False(t, res.Reconciliation.Preview.CanClose)
}

// TestStartReconciliationCurrentReadRevealsOnlyAfterSuccess pins the reveal
// boundary: the OPEN current read shows the redacted shape before start, and
// once the Shift becomes CLOSING the same read reports the frozen
// reconciliation (spec 4.3, 9.5).
func TestStartReconciliationCurrentReadRevealsOnlyAfterSuccess(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()

	current := shift.NewCurrentShiftHandler(f.Runner)
	before, err := current.Handle(ctx, f.Cashier.actor())
	require.NoError(t, err)
	require.NotNil(t, before)
	beforeRaw, err := json.Marshal(before)
	require.NoError(t, err)
	assert.NotContains(t, string(beforeRaw), "reconciliation",
		"the OPEN read reveals no reconciliation before the blind count commits")
	assert.NotContains(t, string(beforeRaw), "expected_cash_vnd")

	start := shift.NewStartReconciliationHandler(f.Runner)
	counted := int64(500_000)
	_, _, err = start.Handle(ctx, f.Cashier.actor(), f.startCommand(counted, uuid.New()))
	require.NoError(t, err)

	after, err := current.Handle(ctx, f.Cashier.actor())
	require.NoError(t, err)
	require.NotNil(t, after, "the CLOSING Shift is active and the read reveals its snapshot")
	afterRaw, err := json.Marshal(after)
	require.NoError(t, err)

	var closing shift.ClosingShiftResponse
	require.NoError(t, json.Unmarshal(afterRaw, &closing))
	assert.Equal(t, shift.StateClosing, closing.State)
	assert.Equal(t, f.Shift.ID, closing.ID)
	assert.Equal(t, int64(500_000), closing.Reconciliation.ExpectedCashVND,
		"the current read reveals the same frozen snapshot start returned")
	assert.Equal(t, counted, closing.Reconciliation.CashCounts[0].CountedCashVND)
}

// TestStartReconciliationRejectsBlockersInPrecedenceOrder seeds one blocker at
// a time, in reverse precedence order, and asserts each start attempt reports
// the highest-precedence violation's stable code (spec 8).
func TestStartReconciliationRejectsBlockersInPrecedenceOrder(t *testing.T) {
	f := newShiftFixture(t)
	start := shift.NewStartReconciliationHandler(f.Runner)

	// Precedence 4 alone: an active Service Session.
	seedActiveServiceSession(t, f.DB, f.Shift.ID, f.Cashier.StaffID)
	requireStartBlocked(t, f, start, shift.ErrActiveServiceSession, "SHIFT_ACTIVE_SERVICE_SESSION")

	// Precedence 3 now present too: an unresolved POST_SALE correction. The
	// correction chain's own Check is settled so blocker 1 stays clear.
	correction := seedCorrectionCheck(t, f.DB, f.Shift.ID, f.Cashier.StaffID, f.Cashier.SessionID, 90_000)
	settleCheck(t, f.DB, correction.CheckID, f.Cashier.StaffID, f.Shift.ID, f.Cashier.SessionID)
	saleID := seedCompletedSale(t, f.DB, f.Shift.ID, f.Cashier.StaffID, f.Cashier.SessionID)
	seedPostSaleAdjustment(t, f.DB, correction, saleID, f.Shift.ID,
		f.Cashier.StaffID, f.Cashier.SessionID, 40_000)
	requireStartBlocked(t, f, start, shift.ErrUnresolvedCorrection, "SHIFT_UNRESOLVED_CORRECTION")

	// Precedence 2 now present too: a pending Refund intent without its
	// completion. It does not reduce the correction term, and precedence
	// reports the pending Refund first (spec 8).
	seedRefund(t, f.DB, correction.CheckID, f.Shift.ID, f.Cashier.StaffID, f.Cashier.SessionID,
		"CASH", 10_000, nil, time.Now(), nil)
	requireStartBlocked(t, f, start, shift.ErrPendingRefund, "SHIFT_PENDING_REFUND")

	// Precedence 1 now present too: the correction chain's Check reopens.
	reopenCheck(t, f.DB, correction.CheckID)
	requireStartBlocked(t, f, start, shift.ErrUnsettledCheck, "SHIFT_UNSETTLED_CHECK")
}

// TestStartReconciliationLiveCheckCorrectionBlocksUntilRefunded drives the
// LIVE_CHECK term of the unresolved-correction amount (spec 8) through the
// handler: a settled Check whose applied Payment exceeds what its completed
// live Refunds and LIVE_CHECK adjustments cover blocks start, and a completed
// live Refund that closes the gap frees it.
func TestStartReconciliationLiveCheckCorrectionBlocksUntilRefunded(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()
	start := shift.NewStartReconciliationHandler(f.Runner)

	// The chain's live obligation is
	// 90000 payment - 0 refunds - (90000 base - 40000 adjustment) = 40000 > 0.
	// The chain's Session closes with the Check settled, so only the
	// correction obligation blocks.
	correction := seedCorrectionCheck(t, f.DB, f.Shift.ID, f.Cashier.StaffID, f.Cashier.SessionID, 90_000)
	settleCheck(t, f.DB, correction.CheckID, f.Cashier.StaffID, f.Shift.ID, f.Cashier.SessionID)
	closeCheckSession(t, f.DB, correction.CheckID)
	seedPayment(t, f.DB, correction.CheckID, f.Shift.ID, f.Cashier.StaffID, f.Cashier.SessionID,
		"CASH", 90_000, 90_000)
	seedLiveAdjustment(t, f.DB, correction, f.Shift.ID, 40_000)
	requireStartBlocked(t, f, start, shift.ErrUnresolvedCorrection, "SHIFT_UNRESOLVED_CORRECTION")

	// A completed live Refund covering the remaining obligation resolves the
	// term: 90000 - 40000 - (90000 - 40000) = 0.
	completedAt := time.Now().Add(-time.Minute)
	seedRefund(t, f.DB, correction.CheckID, f.Shift.ID, f.Cashier.StaffID, f.Cashier.SessionID,
		"CASH", 40_000, nil, time.Now().Add(-2*time.Minute), &completedAt)

	status, res, err := start.Handle(ctx, f.Cashier.actor(),
		f.startCommand(550_000, uuid.New()))
	require.NoError(t, err)
	assert.Equal(t, 201, status)
	assert.Equal(t, shift.StateClosing, res.State)
	assert.Equal(t, int64(90_000), res.Reconciliation.CashPaymentVND)
	assert.Equal(t, int64(40_000), res.Reconciliation.CashRefundVND)
	assert.Equal(t, int64(550_000), res.Reconciliation.ExpectedCashVND)
}

// TestStartReconciliationPostSaleAllocationResolvesCorrection drives the
// POST_SALE term of the unresolved-correction amount (spec 8) through the
// handler: an unresolved POST_SALE adjustment blocks start, a completed
// Refund's allocation must cover the adjustment in full, and only then does
// the start succeed.
func TestStartReconciliationPostSaleAllocationResolvesCorrection(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()
	start := shift.NewStartReconciliationHandler(f.Runner)

	// The adjustment's obligation is 40000 - 0 completed Refund allocations.
	correction := seedCorrectionCheck(t, f.DB, f.Shift.ID, f.Cashier.StaffID, f.Cashier.SessionID, 90_000)
	settleCheck(t, f.DB, correction.CheckID, f.Cashier.StaffID, f.Shift.ID, f.Cashier.SessionID)
	closeCheckSession(t, f.DB, correction.CheckID)
	saleID := seedCompletedSale(t, f.DB, f.Shift.ID, f.Cashier.StaffID, f.Cashier.SessionID)
	adjustmentID := seedPostSaleAdjustment(t, f.DB, correction, saleID, f.Shift.ID,
		f.Cashier.StaffID, f.Cashier.SessionID, 40_000)
	requireStartBlocked(t, f, start, shift.ErrUnresolvedCorrection, "SHIFT_UNRESOLVED_CORRECTION")

	// A completed Refund allocated 15000 against the 40000 adjustment leaves
	// 25000 unresolved: still blocked.
	completedAt := time.Now().Add(-time.Minute)
	partial := seedRefund(t, f.DB, correction.CheckID, f.Shift.ID, f.Cashier.StaffID, f.Cashier.SessionID,
		"CASH", 15_000, &saleID, time.Now().Add(-2*time.Minute), &completedAt)
	seedRefundAdjustmentAllocation(t, f.DB, partial, adjustmentID, 15_000)
	requireStartBlocked(t, f, start, shift.ErrUnresolvedCorrection, "SHIFT_UNRESOLVED_CORRECTION")

	// A second completed Refund with an allocation covering the remainder
	// resolves the term: 40000 - 15000 - 25000 = 0.
	rest := seedRefund(t, f.DB, correction.CheckID, f.Shift.ID, f.Cashier.StaffID, f.Cashier.SessionID,
		"CASH", 25_000, &saleID, time.Now().Add(-2*time.Minute), &completedAt)
	seedRefundAdjustmentAllocation(t, f.DB, rest, adjustmentID, 25_000)

	status, res, err := start.Handle(ctx, f.Cashier.actor(),
		f.startCommand(460_000, uuid.New()))
	require.NoError(t, err)
	assert.Equal(t, 201, status)
	assert.Equal(t, shift.StateClosing, res.State)
	assert.Equal(t, int64(40_000), res.Reconciliation.CashRefundVND)
	assert.Equal(t, int64(460_000), res.Reconciliation.ExpectedCashVND,
		"500000 float - 40000 completed refunds, the base charge untouched")
}

// TestStartReconciliationCalculationFailureIsGeneric injects an Expected Cash
// range failure through seeded Payments and asserts the public error is the
// generic calculation-failed code with no amount or operand text, and that
// nothing persisted (spec 4.3, 12).
func TestStartReconciliationCalculationFailureIsGeneric(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()

	checkID := seedSettledCheck(t, f.DB, f.Shift.ID, f.Cashier.StaffID, f.Cashier.SessionID)
	for i := 0; i < 3; i++ {
		seedPayment(t, f.DB, checkID, f.Shift.ID, f.Cashier.StaffID, f.Cashier.SessionID,
			"CASH", 2_000_000_000, 2_000_000_000)
	}

	start := shift.NewStartReconciliationHandler(f.Runner)
	_, _, err := start.Handle(ctx, f.Cashier.actor(), f.startCommand(0, uuid.New()))
	require.Error(t, err, "the sum breaches the Expected Cash bound")

	mapped := shift.MapHTTPError(err)
	var coded *response.CodedError
	require.True(t, errors.As(mapped, &coded))
	assert.Equal(t, http.StatusInternalServerError, coded.Status)
	assert.Equal(t, "SHIFT_RECONCILIATION_CALCULATION_FAILED", coded.Code)
	assert.NotRegexp(t, `[0-9]`, coded.Message, "no amount or operand may cross the boundary")
	assert.NotContains(t, coded.Message, "expected cash")
	assert.NotContains(t, coded.Message, "outside")

	// Nothing persisted: no snapshot, no count, no audit events, and the
	// Shift is still OPEN to the current read.
	var reconRows int
	require.NoError(t, f.DB.QueryRow(`SELECT count(*) FROM shift_reconciliations`).Scan(&reconRows))
	assert.Equal(t, 0, reconRows)
	var countRows int
	require.NoError(t, f.DB.QueryRow(`SELECT count(*) FROM shift_cash_counts`).Scan(&countRows))
	assert.Equal(t, 0, countRows)
	assert.Equal(t, 0, countAuditEvents(t, f.DB, shift.EventReconciliationStarted, f.Shift.ID))
	assert.Equal(t, 0, countAuditEvents(t, f.DB, shift.EventCashCountRecorded, f.Shift.ID))

	current := shift.NewCurrentShiftHandler(f.Runner)
	open, readErr := current.Handle(ctx, f.Cashier.actor())
	require.NoError(t, readErr)
	require.NotNil(t, open)
	openRaw, err := json.Marshal(open)
	require.NoError(t, err)
	var redacted shift.OpenCurrentShiftResponse
	require.NoError(t, json.Unmarshal(openRaw, &redacted))
	assert.Equal(t, shift.StateOpen, redacted.State,
		"the Shift is still OPEN to the current read after the failed start")
	assert.NotContains(t, string(openRaw), "reconciliation",
		"no snapshot may be revealed by a failed start")
}

// TestStartReconciliationQRReceivedCalculationFailureIsGeneric injects an
// Expected Manual QR Received range failure through seeded Payments and asserts
// the public error is the generic calculation-failed code with no amount or
// operand text, and that nothing persisted (spec 4.3, 12). The bound mirrors
// the symmetric expected_manual_qr_received_vnd check the snapshot table
// enforces, so a breach must fail the checked formula before the insert.
func TestStartReconciliationQRReceivedCalculationFailureIsGeneric(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()

	// Each Payment sits inside the per-Payment money bound, but their
	// aggregate breaches the snapshot's symmetric ±MaxAmountVND window.
	checkID := seedSettledCheck(t, f.DB, f.Shift.ID, f.Cashier.StaffID, f.Cashier.SessionID)
	for i := 0; i < 2; i++ {
		seedPayment(t, f.DB, checkID, f.Shift.ID, f.Cashier.StaffID, f.Cashier.SessionID,
			"MANUAL_QR", 2_000_000_000, 0)
	}

	start := shift.NewStartReconciliationHandler(f.Runner)
	_, _, err := start.Handle(ctx, f.Cashier.actor(), f.startCommand(0, uuid.New()))
	require.Error(t, err, "the net Manual QR sum breaches the Expected Manual QR Received bound")

	mapped := shift.MapHTTPError(err)
	var coded *response.CodedError
	require.True(t, errors.As(mapped, &coded))
	assert.Equal(t, http.StatusInternalServerError, coded.Status)
	assert.Equal(t, "SHIFT_RECONCILIATION_CALCULATION_FAILED", coded.Code)
	assert.NotRegexp(t, `[0-9]`, coded.Message, "no amount or operand may cross the boundary")
	assert.NotContains(t, coded.Message, "expected manual QR")
	assert.NotContains(t, coded.Message, "outside")

	// Nothing persisted: no snapshot, no count, no audit events, and the
	// Shift is still OPEN to the current read.
	var reconRows int
	require.NoError(t, f.DB.QueryRow(`SELECT count(*) FROM shift_reconciliations`).Scan(&reconRows))
	assert.Equal(t, 0, reconRows)
	var countRows int
	require.NoError(t, f.DB.QueryRow(`SELECT count(*) FROM shift_cash_counts`).Scan(&countRows))
	assert.Equal(t, 0, countRows)
	assert.Equal(t, 0, countAuditEvents(t, f.DB, shift.EventReconciliationStarted, f.Shift.ID))
	assert.Equal(t, 0, countAuditEvents(t, f.DB, shift.EventCashCountRecorded, f.Shift.ID))

	current := shift.NewCurrentShiftHandler(f.Runner)
	open, readErr := current.Handle(ctx, f.Cashier.actor())
	require.NoError(t, readErr)
	require.NotNil(t, open)
	openRaw, err := json.Marshal(open)
	require.NoError(t, err)
	var redacted shift.OpenCurrentShiftResponse
	require.NoError(t, json.Unmarshal(openRaw, &redacted))
	assert.Equal(t, shift.StateOpen, redacted.State,
		"the Shift is still OPEN to the current read after the failed start")
	assert.NotContains(t, string(openRaw), "reconciliation",
		"no snapshot may be revealed by a failed start")
}

func TestStartReconciliationIsIdempotent(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()
	start := shift.NewStartReconciliationHandler(f.Runner)

	requestID := uuid.New()
	status, first, err := start.Handle(ctx, f.Cashier.actor(), f.startCommand(123_000, requestID))
	require.NoError(t, err)
	assert.Equal(t, 201, status)

	replayStatus, replay, err := start.Handle(ctx, f.Cashier.actor(), f.startCommand(123_000, requestID))
	require.NoError(t, err)
	assert.Equal(t, 201, replayStatus)
	assert.Equal(t, first.Reconciliation.ID, replay.Reconciliation.ID)
	require.Len(t, replay.Reconciliation.CashCounts, 1)
	assert.Equal(t, first.Reconciliation.CashCounts[0].ID, replay.Reconciliation.CashCounts[0].ID)
	assert.Equal(t, 1, replay.Reconciliation.CashCounts[0].Sequence)

	// The replay wrote no second snapshot, count, or audit event.
	var reconRows int
	require.NoError(t, f.DB.QueryRow(
		`SELECT count(*) FROM shift_reconciliations WHERE sales_shift_id = $1`,
		f.Shift.ID).Scan(&reconRows))
	assert.Equal(t, 1, reconRows)
	var countRows int
	require.NoError(t, f.DB.QueryRow(
		`SELECT count(*) FROM shift_cash_counts`).Scan(&countRows))
	assert.Equal(t, 1, countRows)
	assert.Equal(t, 1, countAuditEvents(t, f.DB, shift.EventReconciliationStarted, f.Shift.ID))
	assert.Equal(t, 1, countAuditEvents(t, f.DB, shift.EventCashCountRecorded, f.Shift.ID))

	// The same request_id with a different body is a conflict, not a replay.
	_, _, err = start.Handle(ctx, f.Cashier.actor(), f.startCommand(456_000, requestID))
	require.Error(t, err)
	assert.ErrorIs(t, err, shift.ErrRequestConflict)
}

// TestStartReconciliationSecondStartIsAlreadyClosing asserts the state-based
// rejection path for two independent starts: the loser reads the Shift's
// CLOSING state under lock and receives SALES_SHIFT_ALREADY_CLOSING.
func TestStartReconciliationSecondStartIsAlreadyClosing(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()
	start := shift.NewStartReconciliationHandler(f.Runner)

	counted := int64(500_000)
	_, first, err := start.Handle(ctx, f.Cashier.actor(), f.startCommand(counted, uuid.New()))
	require.NoError(t, err)

	_, _, err = start.Handle(ctx, f.Manager.actor(), f.startCommand(499_000, uuid.New()))
	require.Error(t, err)
	assert.ErrorIs(t, err, shift.ErrShiftAlreadyClosing)

	mapped := shift.MapHTTPError(err)
	var coded *response.CodedError
	require.True(t, errors.As(mapped, &coded))
	assert.Equal(t, http.StatusConflict, coded.Status)
	assert.Equal(t, "SALES_SHIFT_ALREADY_CLOSING", coded.Code)

	// The loser wrote nothing: still exactly one snapshot and count.
	var reconRows int
	require.NoError(t, f.DB.QueryRow(
		`SELECT count(*) FROM shift_reconciliations WHERE sales_shift_id = $1`,
		f.Shift.ID).Scan(&reconRows))
	assert.Equal(t, 1, reconRows)
	assert.Len(t, first.Reconciliation.CashCounts, 1)
	assert.Equal(t, 1, countAuditEvents(t, f.DB, shift.EventReconciliationStarted, f.Shift.ID))
}

func TestStartReconciliationUnknownShiftIsNotFound(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()
	start := shift.NewStartReconciliationHandler(f.Runner)

	cmd := f.startCommand(1000, uuid.New())
	cmd.ShiftID = uuid.New()
	_, _, err := start.Handle(ctx, f.Cashier.actor(), cmd)
	require.Error(t, err)
	assert.ErrorIs(t, err, shift.ErrSalesShiftNotFound)

	mapped := shift.MapHTTPError(err)
	var coded *response.CodedError
	require.True(t, errors.As(mapped, &coded))
	assert.Equal(t, http.StatusNotFound, coded.Status)
	assert.Equal(t, "SALES_SHIFT_NOT_FOUND", coded.Code)
}

// TestStartReconciliationRejectsInvalidCountedCash covers the handler-level
// defense behind the HTTP boundary's earlier rejection.
func TestStartReconciliationRejectsInvalidCountedCash(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()
	start := shift.NewStartReconciliationHandler(f.Runner)

	for _, counted := range []int64{-1, shift.MaxAmountVND + 1} {
		_, _, err := start.Handle(ctx, f.Cashier.actor(), f.startCommand(counted, uuid.New()))
		require.Error(t, err, "counted %d", counted)
		assert.ErrorIs(t, err, response.ErrInvalid)
	}
}

func TestShiftHTTPStartReconciliationHappyPath(t *testing.T) {
	e, q := newTestServer(t)
	cashierToken, _ := signIn(t, e, q, []string{"CASHIER"}, "2468")

	openBody, _ := json.Marshal(map[string]any{"request_id": uuid.New(), "opening_float_vnd": 500000})
	rec := doRequest(t, e, http.MethodPost, "/api/v1/shifts", cashierToken, openBody)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var env envelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	var opened shift.SalesShiftResponse
	require.NoError(t, json.Unmarshal(env.Data, &opened))

	body, _ := json.Marshal(map[string]any{"request_id": uuid.New(), "counted_cash_vnd": 500000})
	rec = doRequest(t, e, http.MethodPost,
		"/api/v1/shifts/"+opened.ID.String()+"/reconciliation", cashierToken, body)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	require.True(t, env.Success)

	// The closing shape's field allowlist: Shift metadata plus reconciliation.
	var data map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(env.Data, &data))
	assert.ElementsMatch(t,
		[]string{"id", "state", "opened_at", "opener", "reconciliation"},
		jsonKeys(data))

	var recon map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data["reconciliation"], &recon))
	assert.ElementsMatch(t, []string{
		"id", "starter", "started_at",
		"opening_float_vnd", "pay_in_vnd", "pay_out_vnd",
		"cash_payment_vnd", "cash_payment_void_vnd", "cash_refund_vnd",
		"expected_cash_vnd", "manual_qr_payment_vnd", "manual_qr_payment_void_vnd",
		"expected_manual_qr_received_vnd", "manual_qr_refund_vnd",
		"pending_manual_qr_refund_vnd", "pending_refund_vnd",
		"unresolved_post_sale_adjustment_vnd",
		"cash_counts", "qr_observations", "preview",
	}, jsonKeys(recon))

	// Collections serialize as [] never null.
	assert.JSONEq(t, "[]", string(recon["qr_observations"]))
	var counts []json.RawMessage
	require.NoError(t, json.Unmarshal(recon["cash_counts"], &counts))
	require.Len(t, counts, 1)

	var closing shift.ClosingShiftResponse
	require.NoError(t, json.Unmarshal(env.Data, &closing))
	assert.Equal(t, shift.StateClosing, closing.State)
	assert.Equal(t, int64(500_000), closing.Reconciliation.ExpectedCashVND)
}

func TestShiftHTTPStartReconciliationValidation(t *testing.T) {
	e, q := newTestServer(t)
	token, _ := signIn(t, e, q, []string{"CASHIER"}, "2468")

	openBody, _ := json.Marshal(map[string]any{"request_id": uuid.New(), "opening_float_vnd": 500000})
	rec := doRequest(t, e, http.MethodPost, "/api/v1/shifts", token, openBody)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var env envelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	var opened shift.SalesShiftResponse
	require.NoError(t, json.Unmarshal(env.Data, &opened))

	cases := []struct {
		name string
		path string
		body map[string]any
	}{
		{
			name: "missing counted_cash_vnd",
			path: "/api/v1/shifts/" + opened.ID.String() + "/reconciliation",
			body: map[string]any{"request_id": uuid.New()},
		},
		{
			// Explicit zero is valid, so a negative value must be rejected
			// rather than silently counted.
			name: "negative counted_cash_vnd",
			path: "/api/v1/shifts/" + opened.ID.String() + "/reconciliation",
			body: map[string]any{"request_id": uuid.New(), "counted_cash_vnd": -1},
		},
		{
			name: "missing request_id",
			path: "/api/v1/shifts/" + opened.ID.String() + "/reconciliation",
			body: map[string]any{"counted_cash_vnd": 500000},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(tc.body)
			rec := doRequest(t, e, http.MethodPost, tc.path, token, body)
			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())

			var errEnv envelope
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errEnv))
			require.NotNil(t, errEnv.Error)
			assert.Equal(t, "INVALID_INPUT", errEnv.Error.Code)
		})
	}

	t.Run("malformed shift_id", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"request_id": uuid.New(), "counted_cash_vnd": 500000})
		rec := doRequest(t, e, http.MethodPost, "/api/v1/shifts/not-a-uuid/reconciliation", token, body)
		assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	})
}

func TestShiftHTTPStartReconciliationAuthorization(t *testing.T) {
	e, q := newTestServer(t)
	baristaToken, _ := signIn(t, e, q, []string{"BARISTA"}, "1357")

	body, _ := json.Marshal(map[string]any{"request_id": uuid.New(), "counted_cash_vnd": 500000})
	path := "/api/v1/shifts/" + uuid.New().String() + "/reconciliation"

	rec := doRequest(t, e, http.MethodPost, path, baristaToken, body)
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

	rec = doRequest(t, e, http.MethodPost, path, "", body)
	assert.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
}

func TestShiftHTTPStartReconciliationUnknownShift(t *testing.T) {
	e, q := newTestServer(t)
	token, _ := signIn(t, e, q, []string{"CASHIER"}, "2468")

	body, _ := json.Marshal(map[string]any{"request_id": uuid.New(), "counted_cash_vnd": 500000})
	rec := doRequest(t, e, http.MethodPost,
		"/api/v1/shifts/"+uuid.New().String()+"/reconciliation", token, body)
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())

	var errEnv envelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errEnv))
	require.NotNil(t, errEnv.Error)
	assert.Equal(t, "SALES_SHIFT_NOT_FOUND", errEnv.Error.Code)
}

func TestShiftHTTPStartReconciliationBlocker(t *testing.T) {
	e, q := newTestServer(t)
	token, _ := signIn(t, e, q, []string{"CASHIER"}, "2468")
	db, _ := openShiftTestDB(t)

	openBody, _ := json.Marshal(map[string]any{"request_id": uuid.New(), "opening_float_vnd": 500000})
	rec := doRequest(t, e, http.MethodPost, "/api/v1/shifts", token, openBody)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var env envelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	var opened shift.SalesShiftResponse
	require.NoError(t, json.Unmarshal(env.Data, &opened))

	// An active Service Session is the last blocker in the precedence order,
	// so with everything else clear it is the one reported. The Session's
	// owner can be any identity: the blocker counts Sessions, not owners.
	seeder := newTestActor(t, q, []string{"CASHIER"}, true)
	seedActiveServiceSession(t, db, opened.ID, seeder.StaffID)

	body, _ := json.Marshal(map[string]any{"request_id": uuid.New(), "counted_cash_vnd": 500000})
	rec = doRequest(t, e, http.MethodPost,
		"/api/v1/shifts/"+opened.ID.String()+"/reconciliation", token, body)
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())

	var errEnv envelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errEnv))
	require.NotNil(t, errEnv.Error)
	assert.Equal(t, "SHIFT_ACTIVE_SERVICE_SESSION", errEnv.Error.Code)
}

func TestShiftHTTPStartReconciliationCalculationFailure(t *testing.T) {
	e, q := newTestServer(t)
	token, _ := signIn(t, e, q, []string{"CASHIER"}, "2468")
	db, _ := openShiftTestDB(t)

	openBody, _ := json.Marshal(map[string]any{"request_id": uuid.New(), "opening_float_vnd": 500000})
	rec := doRequest(t, e, http.MethodPost, "/api/v1/shifts", token, openBody)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var env envelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	var opened shift.SalesShiftResponse
	require.NoError(t, json.Unmarshal(env.Data, &opened))

	// Seed facts that trip no blocker but do breach the Expected Cash bound.
	// The seeded actor may differ from the signed-in one: the totals are
	// Shift-scoped, not actor-scoped.
	seeder := newTestActor(t, q, []string{"CASHIER"}, true)
	checkID := seedSettledCheck(t, db, opened.ID, seeder.StaffID, seeder.SessionID)
	for i := 0; i < 3; i++ {
		seedPayment(t, db, checkID, opened.ID, seeder.StaffID, seeder.SessionID,
			"CASH", 2_000_000_000, 2_000_000_000)
	}

	body, _ := json.Marshal(map[string]any{"request_id": uuid.New(), "counted_cash_vnd": 0})
	rec = doRequest(t, e, http.MethodPost,
		"/api/v1/shifts/"+opened.ID.String()+"/reconciliation", token, body)
	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())

	var errEnv envelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errEnv))
	require.NotNil(t, errEnv.Error)
	assert.Equal(t, "SHIFT_RECONCILIATION_CALCULATION_FAILED", errEnv.Error.Code)
	assert.NotRegexp(t, `[0-9]`, errEnv.Error.Message, "no amount may cross the boundary")

	var reconRows int
	require.NoError(t, db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM shift_reconciliations`).Scan(&reconRows))
	assert.Equal(t, 0, reconRows)
}

// jsonKeys returns a decoded JSON object's keys for allowlist assertions.
func jsonKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
