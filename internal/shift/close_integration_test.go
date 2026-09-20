//go:build integration

package shift_test

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/shift"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Close-command fixtures. Every close test starts from the shift fixture: an
// OPEN Shift whose Expected Cash is the 500000 Opening Float and whose Manual
// QR expectations are both zero, moved to CLOSING by startReconciliationExact.

// appendCashCount appends one Cash Count attempt and returns it.
func appendCashCount(t *testing.T, f shiftFixture, counted int64) shift.CashCountResponse {
	t.Helper()
	_, res, err := shift.NewRecordCashCountHandler(f.Runner).Handle(
		context.Background(), f.Cashier.actor(), f.cashCountCommand(counted, uuid.New()))
	require.NoError(t, err)
	return res.CashCount
}

// appendQRObservation appends one Manual QR observation attempt and returns it.
func appendQRObservation(t *testing.T, f shiftFixture, received, refunded int64) shift.QRObservationResponse {
	t.Helper()
	_, res, err := shift.NewRecordQRObservationHandler(f.Runner).Handle(
		context.Background(), f.Manager.actor(), f.qrObservationCommand(received, refunded, uuid.New()))
	require.NoError(t, err)
	return res.QRObservation
}

func (f shiftFixture) closeCommand(requestID, cashCountID, qrObservationID uuid.UUID,
	discrepancies []shift.CloseDiscrepancyInput, approverLoginCode, managerPin string,
) shift.CloseShiftCommand {
	return shift.CloseShiftCommand{
		RequestID:            requestID,
		ShiftID:              f.Shift.ID,
		FinalCashCountID:     cashCountID,
		FinalQRObservationID: qrObservationID,
		Discrepancies:        discrepancies,
		ApproverLoginCode:    approverLoginCode,
		ManagerPIN:           managerPin,
	}
}

// reasonInput builds one reason entry without a note.
func reasonInput(dimension shift.DiscrepancyDimension, reason shift.DiscrepancyReason) shift.CloseDiscrepancyInput {
	return shift.CloseDiscrepancyInput{Dimension: dimension, Reason: reason}
}

// Typed copies of the catalog constants, so assert.Equal compares like types
// against the response DTOs' DiscrepancyDimension and DiscrepancyReason.
var (
	dimCash             = shift.DiscrepancyDimension(shift.DimensionCash)
	dimQRReceived       = shift.DiscrepancyDimension(shift.DimensionManualQRReceived)
	dimQRRefunded       = shift.DiscrepancyDimension(shift.DimensionManualQRRefunded)
	reasonCashCount     = shift.DiscrepancyReason(shift.ReasonCashCountDifference)
	reasonQRObservation = shift.DiscrepancyReason(shift.ReasonQRObservationDifference)
	reasonUnexplained   = shift.DiscrepancyReason(shift.ReasonUnexplained)
)

// requireShiftClosureCounts asserts how many closure and discrepancy rows the
// fixture's Shift carries, so failure tests prove nothing persisted.
func requireShiftClosureCounts(t *testing.T, f shiftFixture, wantClosures, wantDiscrepancies int) {
	t.Helper()
	var closures int
	require.NoError(t, f.DB.QueryRow(
		`SELECT count(*) FROM shift_closures WHERE sales_shift_id = $1`,
		f.Shift.ID).Scan(&closures))
	assert.Equal(t, wantClosures, closures, "closure rows for the shift")

	var discrepancies int
	require.NoError(t, f.DB.QueryRow(
		`SELECT count(*) FROM shift_discrepancies d
		 JOIN shift_closures c ON c.id = d.shift_closure_id
		 WHERE c.sales_shift_id = $1`, f.Shift.ID).Scan(&discrepancies))
	assert.Equal(t, wantDiscrepancies, discrepancies, "discrepancy rows for the shift")
}

// requireNoBalancingRows proves a close created no balancing Payment, Refund,
// or Cash Movement (spec 1 non-goals, 5.6).
func requireNoBalancingRows(t *testing.T, f shiftFixture) {
	t.Helper()
	var payments, refunds, movements int
	require.NoError(t, f.DB.QueryRow(`
		SELECT (SELECT count(*) FROM payments WHERE sales_shift_id = $1),
		       (SELECT count(*) FROM refunds   WHERE sales_shift_id = $1),
		       (SELECT count(*) FROM cash_movements WHERE sales_shift_id = $1)`,
		f.Shift.ID).Scan(&payments, &refunds, &movements))
	assert.Zero(t, payments, "close must not create balancing payments")
	assert.Zero(t, refunds, "close must not create balancing refunds")
	assert.Zero(t, movements, "close must not create balancing cash movements")
}

// requireShiftState reads the Shift's stored state.
func requireShiftState(t *testing.T, f shiftFixture) string {
	t.Helper()
	var state string
	require.NoError(t, f.DB.QueryRow(
		`SELECT state FROM sales_shifts WHERE id = $1`, f.Shift.ID).Scan(&state))
	return state
}

// requireClosureRow reads the Shift's single closure row back for assertions.
func requireClosureRow(t *testing.T, f shiftFixture) (
	approverID uuid.NullUUID,
	cashDiff, qrReceivedDiff, qrRefundedDiff int64,
	observedCash, observedQRReceived, observedQRRefunded int64,
) {
	t.Helper()
	require.NoError(t, f.DB.QueryRow(`
		SELECT approved_by_staff_identity_id,
		       cash_difference_vnd, manual_qr_received_difference_vnd,
		       manual_qr_refunded_difference_vnd,
		       observed_cash_vnd, observed_manual_qr_received_vnd,
		       observed_manual_qr_refunded_vnd
		FROM shift_closures WHERE sales_shift_id = $1`, f.Shift.ID).
		Scan(&approverID, &cashDiff, &qrReceivedDiff, &qrRefundedDiff,
			&observedCash, &observedQRReceived, &observedQRRefunded))
	return
}

// requireNoCredentialsInResponse proves the close response carries no PIN and
// no approval login input field (spec 9.6, 10).
func requireNoCredentialsInResponse(t *testing.T, res shift.ClosedShiftDetailResponse) {
	t.Helper()
	body, err := json.Marshal(res)
	require.NoError(t, err)
	assert.NotContains(t, string(body), "manager_pin")
	assert.NotContains(t, string(body), "approver_login_code")
}

// TestCloseShift drives Final Close: exact and Manager-approved discrepant
// closure of a CLOSING Shift (spec 4.4, 7, 9.4, 10).
func TestCloseShift(t *testing.T) {
	ctx := context.Background()

	t.Run("exact first-attempt close", func(t *testing.T) {
		f := newShiftFixture(t)
		started := startReconciliationExact(t, f)
		observation := appendQRObservation(t, f, 0, 0)
		closes := shift.NewCloseShiftHandler(f.Runner)

		initial := started.Reconciliation.CashCounts[0]
		status, res, err := closes.Handle(ctx, f.Cashier.actor(),
			f.closeCommand(uuid.New(), initial.ID, observation.ID, []shift.CloseDiscrepancyInput{}, "", ""))
		require.NoError(t, err)
		assert.Equal(t, 200, status)

		// Three signed zero differences; no discrepancy rows and no approver.
		assert.Equal(t, int64(0), res.CashDifferenceVND)
		assert.Equal(t, int64(0), res.ManualQRReceivedDifferenceVND)
		assert.Equal(t, int64(0), res.ManualQRRefundedDifferenceVND)
		assert.False(t, res.HasDiscrepancy)
		assert.NotNil(t, res.Discrepancies, "discrepancies serialize as [] never null")
		assert.Empty(t, res.Discrepancies)
		assert.Nil(t, res.Approver)

		assert.Equal(t, f.Shift.ID, res.ID)
		assert.Equal(t, f.Cashier.StaffID, res.Closer.ID)
		assert.Equal(t, f.Cashier.StaffID, res.Starter.ID)
		assert.Equal(t, int64(500000), res.OpeningFloatVND)
		assert.Equal(t, int64(500000), res.ObservedCashVND)
		require.Len(t, res.CashCounts, 1)
		require.Len(t, res.QRObservations, 1)
		assert.False(t, res.ClosedAt.IsZero())
		requireNoCredentialsInResponse(t, res)

		// The Shift is CLOSED and exactly one closure row exists.
		assert.Equal(t, shift.StateClosed, requireShiftState(t, f))
		requireShiftClosureCounts(t, f, 1, 0)
		requireNoBalancingRows(t, f)

		// One exact-closure audit event with the final evidence ids.
		assert.Equal(t, 1, countAuditEvents(t, f.DB, shift.EventShiftClosedExact, f.Shift.ID))
		assert.Equal(t, 0, countAuditEvents(t, f.DB, shift.EventShiftClosedWithDiscrepancy, f.Shift.ID))
	})

	t.Run("exact close with a shortage initial count requires a recount", func(t *testing.T) {
		f := newShiftFixture(t)
		_, start, err := shift.NewStartReconciliationHandler(f.Runner).Handle(
			ctx, f.Cashier.actor(), f.startCommand(499_000, uuid.New()))
		require.NoError(t, err)
		observation := appendQRObservation(t, f, 0, 0)
		closes := shift.NewCloseShiftHandler(f.Runner)

		initial := start.Reconciliation.CashCounts[0]
		_, _, err = closes.Handle(ctx, f.Cashier.actor(),
			f.closeCommand(uuid.New(), initial.ID, observation.ID, []shift.CloseDiscrepancyInput{}, "", ""))
		require.Error(t, err)
		requireCodedError(t, err, 409, "SHIFT_CASH_RECOUNT_REQUIRED")
		assert.ErrorIs(t, err, shift.ErrCashRecountRequired)

		// Nothing persisted: the Shift stays CLOSING with no closure.
		assert.Equal(t, shift.StateClosing, requireShiftState(t, f))
		requireShiftClosureCounts(t, f, 0, 0)
	})

	t.Run("cash shortage after recount", func(t *testing.T) {
		f := newShiftFixture(t)
		startReconciliationExact(t, f)
		recount := appendCashCount(t, f, 499_000)
		observation := appendQRObservation(t, f, 0, 0)
		closes := shift.NewCloseShiftHandler(f.Runner)

		status, res, err := closes.Handle(ctx, f.Cashier.actor(),
			f.closeCommand(uuid.New(), recount.ID, observation.ID,
				[]shift.CloseDiscrepancyInput{reasonInput(shift.DimensionCash, shift.ReasonCashCountDifference)},
				f.Manager.LoginCode, f.Manager.Pin))
		require.NoError(t, err)
		assert.Equal(t, 200, status)

		// Three signed values: a 1000 cash shortage and exact QR dimensions.
		assert.Equal(t, int64(-1_000), res.CashDifferenceVND)
		assert.Equal(t, int64(0), res.ManualQRReceivedDifferenceVND)
		assert.Equal(t, int64(0), res.ManualQRRefundedDifferenceVND)
		assert.Equal(t, int64(499_000), res.ObservedCashVND)
		assert.True(t, res.HasDiscrepancy)

		// Exactly the nonzero discrepancy rows with the correct pairing.
		require.Len(t, res.Discrepancies, 1)
		assert.Equal(t, dimCash, res.Discrepancies[0].Dimension)
		assert.Equal(t, reasonCashCount, res.Discrepancies[0].Reason)
		assert.Equal(t, int64(500000), res.Discrepancies[0].ExpectedVND)
		assert.Equal(t, int64(499_000), res.Discrepancies[0].ObservedVND)
		assert.Equal(t, int64(-1_000), res.Discrepancies[0].DifferenceVND)
		assert.Nil(t, res.Discrepancies[0].Note)

		// One approver identity.
		require.NotNil(t, res.Approver)
		assert.Equal(t, f.Manager.StaffID, res.Approver.ID)
		requireNoCredentialsInResponse(t, res)

		approverID, cashDiff, qrReceivedDiff, qrRefundedDiff, _, _, _ :=
			requireClosureRow(t, f)
		assert.True(t, approverID.Valid)
		assert.Equal(t, f.Manager.StaffID, approverID.UUID)
		assert.Equal(t, int64(-1_000), cashDiff)
		assert.Equal(t, int64(0), qrReceivedDiff)
		assert.Equal(t, int64(0), qrRefundedDiff)
		assert.Equal(t, shift.StateClosed, requireShiftState(t, f))
		requireShiftClosureCounts(t, f, 1, 1)
		requireNoBalancingRows(t, f)
		assert.Equal(t, 1, countAuditEvents(t, f.DB, shift.EventShiftClosedWithDiscrepancy, f.Shift.ID))
	})

	t.Run("QR received difference after second QR observation", func(t *testing.T) {
		f := newShiftFixture(t)
		started := startReconciliationExact(t, f)
		appendQRObservation(t, f, 0, 0)
		recheck := appendQRObservation(t, f, 730_000, 0)
		closes := shift.NewCloseShiftHandler(f.Runner)

		initial := started.Reconciliation.CashCounts[0]
		status, res, err := closes.Handle(ctx, f.Cashier.actor(),
			f.closeCommand(uuid.New(), initial.ID, recheck.ID,
				[]shift.CloseDiscrepancyInput{reasonInput(shift.DimensionManualQRReceived, shift.ReasonQRObservationDifference)},
				f.Manager.LoginCode, f.Manager.Pin))
		require.NoError(t, err)
		assert.Equal(t, 200, status)

		assert.Equal(t, int64(0), res.CashDifferenceVND)
		assert.Equal(t, int64(730_000), res.ManualQRReceivedDifferenceVND, "an uncounted QR excess is positive")
		assert.Equal(t, int64(0), res.ManualQRRefundedDifferenceVND)
		assert.Equal(t, int64(730_000), res.ObservedManualQRReceivedVND)

		require.Len(t, res.Discrepancies, 1)
		assert.Equal(t, dimQRReceived, res.Discrepancies[0].Dimension)
		assert.Equal(t, reasonQRObservation, res.Discrepancies[0].Reason)

		require.NotNil(t, res.Approver)
		assert.Equal(t, f.Manager.StaffID, res.Approver.ID)
		requireShiftClosureCounts(t, f, 1, 1)
		requireNoBalancingRows(t, f)
	})

	t.Run("QR refunded difference", func(t *testing.T) {
		f := newShiftFixture(t)
		started := startReconciliationExact(t, f)
		appendQRObservation(t, f, 0, 0)
		recheck := appendQRObservation(t, f, 0, 50_000)
		closes := shift.NewCloseShiftHandler(f.Runner)

		initial := started.Reconciliation.CashCounts[0]
		status, res, err := closes.Handle(ctx, f.Cashier.actor(),
			f.closeCommand(uuid.New(), initial.ID, recheck.ID,
				[]shift.CloseDiscrepancyInput{reasonInput(shift.DimensionManualQRRefunded, shift.ReasonUnexplained)},
				f.Manager.LoginCode, f.Manager.Pin))
		require.NoError(t, err)
		assert.Equal(t, 200, status)

		assert.Equal(t, int64(0), res.CashDifferenceVND)
		assert.Equal(t, int64(0), res.ManualQRReceivedDifferenceVND)
		assert.Equal(t, int64(50_000), res.ManualQRRefundedDifferenceVND)

		require.Len(t, res.Discrepancies, 1)
		assert.Equal(t, dimQRRefunded, res.Discrepancies[0].Dimension)
		assert.Equal(t, reasonUnexplained, res.Discrepancies[0].Reason)

		requireShiftClosureCounts(t, f, 1, 1)
		requireNoBalancingRows(t, f)
	})

	t.Run("all three differences", func(t *testing.T) {
		f := newShiftFixture(t)
		startReconciliationExact(t, f)
		recount := appendCashCount(t, f, 499_000)
		appendQRObservation(t, f, 0, 0)
		recheck := appendQRObservation(t, f, 730_000, 50_000)
		closes := shift.NewCloseShiftHandler(f.Runner)

		status, res, err := closes.Handle(ctx, f.Cashier.actor(),
			f.closeCommand(uuid.New(), recount.ID, recheck.ID, []shift.CloseDiscrepancyInput{
				reasonInput(shift.DimensionCash, shift.ReasonCashCountDifference),
				reasonInput(shift.DimensionManualQRReceived, shift.ReasonQRObservationDifference),
				reasonInput(shift.DimensionManualQRRefunded, shift.ReasonQRObservationDifference),
			}, f.Manager.LoginCode, f.Manager.Pin))
		require.NoError(t, err)
		assert.Equal(t, 200, status)

		assert.Equal(t, int64(-1_000), res.CashDifferenceVND)
		assert.Equal(t, int64(730_000), res.ManualQRReceivedDifferenceVND)
		assert.Equal(t, int64(50_000), res.ManualQRRefundedDifferenceVND)
		assert.True(t, res.HasDiscrepancy)

		// Exactly the three nonzero dimensions, one reason entry each.
		require.Len(t, res.Discrepancies, 3)
		assert.Equal(t, dimCash, res.Discrepancies[0].Dimension)
		assert.Equal(t, reasonCashCount, res.Discrepancies[0].Reason)
		assert.Equal(t, dimQRReceived, res.Discrepancies[1].Dimension)
		assert.Equal(t, reasonQRObservation, res.Discrepancies[1].Reason)
		assert.Equal(t, dimQRRefunded, res.Discrepancies[2].Dimension)
		assert.Equal(t, reasonQRObservation, res.Discrepancies[2].Reason)

		require.NotNil(t, res.Approver)
		assert.Equal(t, f.Manager.StaffID, res.Approver.ID)
		requireShiftClosureCounts(t, f, 1, 3)
		requireNoBalancingRows(t, f)
		assert.Equal(t, 1, countAuditEvents(t, f.DB, shift.EventShiftClosedWithDiscrepancy, f.Shift.ID))
	})

	t.Run("missing reason", func(t *testing.T) {
		f := newShiftFixture(t)
		startReconciliationExact(t, f)
		appendCashCount(t, f, 499_000)
		appendQRObservation(t, f, 0, 0)
		recheck := appendQRObservation(t, f, 730_000, 0)
		closes := shift.NewCloseShiftHandler(f.Runner)

		// The recount (sequence 2) and recheck exist, but the Cash dimension's
		// reason entry is missing: the nonzero difference without exactly one
		// entry conflicts (spec 7.7).
		cashID := latestCashCountID(t, f)
		_, _, err := closes.Handle(ctx, f.Cashier.actor(),
			f.closeCommand(uuid.New(), cashID, recheck.ID,
				[]shift.CloseDiscrepancyInput{reasonInput(shift.DimensionManualQRReceived, shift.ReasonQRObservationDifference)},
				f.Manager.LoginCode, f.Manager.Pin))
		require.Error(t, err)
		requireCodedError(t, err, 409, "SHIFT_DISCREPANCY_REASON_REQUIRED")
		assert.ErrorIs(t, err, shift.ErrDiscrepancyReasonRequired)

		assert.Equal(t, shift.StateClosing, requireShiftState(t, f))
		requireShiftClosureCounts(t, f, 0, 0)
		assert.Equal(t, 0, countAuditEvents(t, f.DB, shift.EventShiftClosedWithDiscrepancy, f.Shift.ID))
	})

	t.Run("unexpected reason", func(t *testing.T) {
		f := newShiftFixture(t)
		started := startReconciliationExact(t, f)
		observation := appendQRObservation(t, f, 0, 0)
		closes := shift.NewCloseShiftHandler(f.Runner)

		// Every server-derived difference is zero, so a reason entry for the
		// exact QR received dimension is unexpected (spec 7.8).
		initial := started.Reconciliation.CashCounts[0]
		_, _, err := closes.Handle(ctx, f.Cashier.actor(),
			f.closeCommand(uuid.New(), initial.ID, observation.ID,
				[]shift.CloseDiscrepancyInput{reasonInput(shift.DimensionManualQRReceived, shift.ReasonUnexplained)},
				f.Manager.LoginCode, f.Manager.Pin))
		require.Error(t, err)
		requireCodedError(t, err, 409, "SHIFT_DISCREPANCY_REASON_UNEXPECTED")
		assert.ErrorIs(t, err, shift.ErrDiscrepancyReasonUnexpected)

		requireShiftClosureCounts(t, f, 0, 0)
	})

	t.Run("unexpected reason pairing", func(t *testing.T) {
		f := newShiftFixture(t)
		startReconciliationExact(t, f)
		appendCashCount(t, f, 500_000)
		appendQRObservation(t, f, 0, 0)
		recheck := appendQRObservation(t, f, 730_000, 0)
		closes := shift.NewCloseShiftHandler(f.Runner)

		// CASH_COUNT_DIFFERENCE explains only a Cash difference; on a Manual QR
		// dimension the pairing is invalid (spec 5.6).
		_, _, err := closes.Handle(ctx, f.Cashier.actor(),
			f.closeCommand(uuid.New(), latestCashCountID(t, f), recheck.ID,
				[]shift.CloseDiscrepancyInput{reasonInput(shift.DimensionManualQRReceived, shift.ReasonCashCountDifference)},
				f.Manager.LoginCode, f.Manager.Pin))
		require.Error(t, err)
		requireCodedError(t, err, 409, "SHIFT_DISCREPANCY_REASON_UNEXPECTED")
		assert.ErrorIs(t, err, shift.ErrDiscrepancyReasonUnexpected)

		requireShiftClosureCounts(t, f, 0, 0)
	})

	t.Run("missing Manager approval", func(t *testing.T) {
		f := newShiftFixture(t)
		cashID, qrID := shortageEvidence(t, f)
		closes := shift.NewCloseShiftHandler(f.Runner)

		// A discrepant close without the approval pair is a failed Manager
		// approval at the command level: 403, collapsed, and nothing persisted
		// (spec 10, 12). The HTTP boundary rejects the omission earlier with a
		// 400 (spec 9.4); a domain caller without the pair is denied here.
		_, _, err := closes.Handle(ctx, f.Cashier.actor(),
			f.closeCommand(uuid.New(), cashID, qrID,
				[]shift.CloseDiscrepancyInput{reasonInput(shift.DimensionCash, shift.ReasonUnexplained)},
				"", ""))
		require.Error(t, err)
		requireCodedError(t, err, 403, "MANAGER_APPROVAL_UNAVAILABLE")
		assert.ErrorIs(t, err, shift.ErrManagerApprovalUnavailable)

		requireShiftClosureCounts(t, f, 0, 0)
		assert.Equal(t, shift.StateClosing, requireShiftState(t, f))
	})

	t.Run("disabled Manager approval", func(t *testing.T) {
		f := newShiftFixture(t)
		cashID, qrID := shortageEvidence(t, f)
		closes := shift.NewCloseShiftHandler(f.Runner)

		_, err := f.DB.Exec(`UPDATE staff_identities SET enabled = false WHERE id = $1`, f.Manager.StaffID)
		require.NoError(t, err)

		_, _, err = closes.Handle(ctx, f.Cashier.actor(),
			f.closeCommand(uuid.New(), cashID, qrID,
				[]shift.CloseDiscrepancyInput{reasonInput(shift.DimensionCash, shift.ReasonUnexplained)},
				f.Manager.LoginCode, f.Manager.Pin))
		require.Error(t, err)
		requireCodedError(t, err, 403, "MANAGER_APPROVAL_UNAVAILABLE")

		requireShiftClosureCounts(t, f, 0, 0)
	})

	t.Run("demoted Manager approval", func(t *testing.T) {
		f := newShiftFixture(t)
		cashID, qrID := shortageEvidence(t, f)
		closes := shift.NewCloseShiftHandler(f.Runner)

		_, err := f.DB.Exec(`DELETE FROM staff_operational_roles WHERE staff_identity_id = $1`, f.Manager.StaffID)
		require.NoError(t, err)

		_, _, err = closes.Handle(ctx, f.Cashier.actor(),
			f.closeCommand(uuid.New(), cashID, qrID,
				[]shift.CloseDiscrepancyInput{reasonInput(shift.DimensionCash, shift.ReasonUnexplained)},
				f.Manager.LoginCode, f.Manager.Pin))
		require.Error(t, err)
		requireCodedError(t, err, 403, "MANAGER_APPROVAL_UNAVAILABLE")

		requireShiftClosureCounts(t, f, 0, 0)
	})

	t.Run("wrong PIN approval", func(t *testing.T) {
		f := newShiftFixture(t)
		cashID, qrID := shortageEvidence(t, f)
		closes := shift.NewCloseShiftHandler(f.Runner)

		_, _, err := closes.Handle(ctx, f.Cashier.actor(),
			f.closeCommand(uuid.New(), cashID, qrID,
				[]shift.CloseDiscrepancyInput{reasonInput(shift.DimensionCash, shift.ReasonUnexplained)},
				f.Manager.LoginCode, "0000"))
		require.Error(t, err)
		requireCodedError(t, err, 403, "MANAGER_APPROVAL_UNAVAILABLE")

		requireShiftClosureCounts(t, f, 0, 0)
	})

	t.Run("self-approval", func(t *testing.T) {
		f := newShiftFixture(t)
		// The Manager both starts the reconciliation and approves their own
		// discrepant close (spec 7.10, ADR-009).
		startReconciliationExact(t, f)
		recount := appendCashCount(t, f, 499_000)
		observation := appendQRObservation(t, f, 0, 0)
		closes := shift.NewCloseShiftHandler(f.Runner)

		status, res, err := closes.Handle(ctx, f.Manager.actor(),
			f.closeCommand(uuid.New(), recount.ID, observation.ID,
				[]shift.CloseDiscrepancyInput{reasonInput(shift.DimensionCash, shift.ReasonUnexplained)},
				f.Manager.LoginCode, f.Manager.Pin))
		require.NoError(t, err)
		assert.Equal(t, 200, status)

		// The approver is the initiator; both identities are the Manager's.
		require.NotNil(t, res.Approver)
		assert.Equal(t, f.Manager.StaffID, res.Approver.ID)
		assert.Equal(t, f.Manager.StaffID, res.Closer.ID)
		requireShiftClosureCounts(t, f, 1, 1)
	})

	t.Run("another-Manager approval", func(t *testing.T) {
		f := newShiftFixture(t)
		cashID, qrID := shortageEvidence(t, f)
		closes := shift.NewCloseShiftHandler(f.Runner)

		// The Cashier closes; a different staff member, the Manager, approves.
		status, res, err := closes.Handle(ctx, f.Cashier.actor(),
			f.closeCommand(uuid.New(), cashID, qrID,
				[]shift.CloseDiscrepancyInput{reasonInput(shift.DimensionCash, shift.ReasonUnexplained)},
				f.Manager.LoginCode, f.Manager.Pin))
		require.NoError(t, err)
		assert.Equal(t, 200, status)

		assert.Equal(t, f.Cashier.StaffID, res.Closer.ID)
		require.NotNil(t, res.Approver)
		assert.Equal(t, f.Manager.StaffID, res.Approver.ID)
		assert.NotEqual(t, res.Closer.ID, res.Approver.ID)
		requireShiftClosureCounts(t, f, 1, 1)
	})

	t.Run("stale non-latest evidence", func(t *testing.T) {
		f := newShiftFixture(t)
		startReconciliationExact(t, f)
		observation := appendQRObservation(t, f, 0, 0)
		superseded := appendCashCount(t, f, 499_000)
		latest := appendCashCount(t, f, 500_000)
		closes := shift.NewCloseShiftHandler(f.Runner)

		// A later recount makes the prepared close request stale (spec 7.11):
		// the submitted id exists but is not the ledger's latest row.
		_, _, err := closes.Handle(ctx, f.Cashier.actor(),
			f.closeCommand(uuid.New(), superseded.ID, observation.ID, []shift.CloseDiscrepancyInput{}, "", ""))
		require.Error(t, err)
		requireCodedError(t, err, 409, "SHIFT_RECONCILIATION_STALE")
		assert.ErrorIs(t, err, shift.ErrReconciliationStale)
		requireShiftClosureCounts(t, f, 0, 0)

		// The same staleness applies to the QR ledger: a second observation
		// supersedes the first.
		appendQRObservation(t, f, 730_000, 0)
		_, _, err = closes.Handle(ctx, f.Cashier.actor(),
			f.closeCommand(uuid.New(), latest.ID, observation.ID,
				[]shift.CloseDiscrepancyInput{reasonInput(shift.DimensionManualQRReceived, shift.ReasonQRObservationDifference)},
				f.Manager.LoginCode, f.Manager.Pin))
		require.Error(t, err)
		requireCodedError(t, err, 409, "SHIFT_RECONCILIATION_STALE")
		assert.ErrorIs(t, err, shift.ErrReconciliationStale)
		requireShiftClosureCounts(t, f, 0, 0)
	})

	t.Run("unknown final attempt id", func(t *testing.T) {
		f := newShiftFixture(t)
		startReconciliationExact(t, f)
		observation := appendQRObservation(t, f, 0, 0)
		closes := shift.NewCloseShiftHandler(f.Runner)

		initial := latestCashCountID(t, f)
		_, _, err := closes.Handle(ctx, f.Cashier.actor(),
			f.closeCommand(uuid.New(), uuid.New(), observation.ID, []shift.CloseDiscrepancyInput{}, "", ""))
		require.Error(t, err)
		requireCodedError(t, err, 404, "SHIFT_RECONCILIATION_ATTEMPT_NOT_FOUND")

		_, _, err = closes.Handle(ctx, f.Cashier.actor(),
			f.closeCommand(uuid.New(), initial, uuid.New(), []shift.CloseDiscrepancyInput{}, "", ""))
		require.Error(t, err)
		requireCodedError(t, err, 404, "SHIFT_RECONCILIATION_ATTEMPT_NOT_FOUND")

		requireShiftClosureCounts(t, f, 0, 0)
	})

	t.Run("changed live source total", func(t *testing.T) {
		f := newShiftFixture(t)
		startReconciliationExact(t, f)
		observation := appendQRObservation(t, f, 0, 0)
		closes := shift.NewCloseShiftHandler(f.Runner)

		// An uncoordinated writer changes a live source total after the
		// snapshot froze it (spec 11.3). Final Close must refuse rather than
		// freeze unexplained numbers.
		_, err := f.DB.Exec(`
			INSERT INTO cash_movements (sales_shift_id, method, amount_vnd, reason,
			                            initiated_by_staff_identity_id,
			                            initiated_staff_access_session_id,
			                            approved_by_staff_identity_id)
			VALUES ($1, 'PAY_IN', 100000, 'ADD_CHANGE_FUND', $2, $3, $2)`,
			f.Shift.ID, f.Cashier.StaffID, f.Cashier.SessionID)
		require.NoError(t, err)

		initial := latestCashCountID(t, f)
		_, _, err = closes.Handle(ctx, f.Cashier.actor(),
			f.closeCommand(uuid.New(), initial, observation.ID, []shift.CloseDiscrepancyInput{}, "", ""))
		require.Error(t, err)
		requireCodedError(t, err, 409, "SHIFT_RECONCILIATION_SOURCE_CHANGED")
		assert.ErrorIs(t, err, shift.ErrReconciliationSourceChanged)

		assert.Equal(t, shift.StateClosing, requireShiftState(t, f))
		requireShiftClosureCounts(t, f, 0, 0)
	})

	t.Run("exact replay", func(t *testing.T) {
		f := newShiftFixture(t)
		startReconciliationExact(t, f)
		observation := appendQRObservation(t, f, 0, 0)
		closes := shift.NewCloseShiftHandler(f.Runner)

		requestID := uuid.New()
		cmd := f.closeCommand(requestID, latestCashCountID(t, f), observation.ID,
			[]shift.CloseDiscrepancyInput{}, "", "")
		status, first, err := closes.Handle(ctx, f.Cashier.actor(), cmd)
		require.NoError(t, err)
		assert.Equal(t, 200, status)

		replayStatus, replay, err := closes.Handle(ctx, f.Cashier.actor(), cmd)
		require.NoError(t, err)
		assert.Equal(t, 200, replayStatus, "an exact replay returns the original 200")
		assert.Equal(t, first.ID, replay.ID)
		assert.True(t, first.ClosedAt.Equal(replay.ClosedAt))
		assert.Equal(t, first.CashDifferenceVND, replay.CashDifferenceVND)
		assert.Equal(t, first.Discrepancies, replay.Discrepancies)

		// The replay wrote no second closure, audit event, or discrepancy row.
		requireShiftClosureCounts(t, f, 1, 0)
		assert.Equal(t, 1, countAuditEvents(t, f.DB, shift.EventShiftClosedExact, f.Shift.ID))
	})

	t.Run("discrepant replay", func(t *testing.T) {
		f := newShiftFixture(t)
		cashID, qrID := shortageEvidence(t, f)
		closes := shift.NewCloseShiftHandler(f.Runner)

		requestID := uuid.New()
		cmd := f.closeCommand(requestID, cashID, qrID,
			[]shift.CloseDiscrepancyInput{reasonInput(shift.DimensionCash, shift.ReasonUnexplained)},
			f.Manager.LoginCode, f.Manager.Pin)
		status, first, err := closes.Handle(ctx, f.Cashier.actor(), cmd)
		require.NoError(t, err)
		assert.Equal(t, 200, status)

		replayStatus, replay, err := closes.Handle(ctx, f.Cashier.actor(), cmd)
		require.NoError(t, err)
		assert.Equal(t, 200, replayStatus, "an exact replay returns the original 200")
		assert.Equal(t, first.ID, replay.ID)
		assert.True(t, first.ClosedAt.Equal(replay.ClosedAt))
		assert.Equal(t, first.Approver, replay.Approver)

		requireShiftClosureCounts(t, f, 1, 1)
		assert.Equal(t, 1, countAuditEvents(t, f.DB, shift.EventShiftClosedWithDiscrepancy, f.Shift.ID))

		// Approval runs before replay: a wrong PIN cannot reach the stored
		// success (spec 10).
		badPIN := f.closeCommand(requestID, cashID, qrID,
			[]shift.CloseDiscrepancyInput{reasonInput(shift.DimensionCash, shift.ReasonUnexplained)},
			f.Manager.LoginCode, "0000")
		_, _, err = closes.Handle(ctx, f.Cashier.actor(), badPIN)
		require.Error(t, err)
		requireCodedError(t, err, 403, "MANAGER_APPROVAL_UNAVAILABLE")
		requireShiftClosureCounts(t, f, 1, 1)
	})

	t.Run("request conflict on a reused id", func(t *testing.T) {
		f := newShiftFixture(t)
		startRequestID := uuid.New()
		_, start, err := shift.NewStartReconciliationHandler(f.Runner).Handle(
			ctx, f.Cashier.actor(), f.startCommand(500_000, startRequestID))
		require.NoError(t, err)
		observation := appendQRObservation(t, f, 0, 0)
		closes := shift.NewCloseShiftHandler(f.Runner)
		initial := start.Reconciliation.CashCounts[0]

		// A request id already consumed by another operation (the start) is a
		// conflict for the close, whatever the fingerprint (spec 10).
		_, _, err = closes.Handle(ctx, f.Cashier.actor(),
			f.closeCommand(startRequestID, initial.ID, observation.ID, []shift.CloseDiscrepancyInput{}, "", ""))
		require.Error(t, err)
		requireCodedError(t, err, 409, "REQUEST_CONFLICT")
		assert.ErrorIs(t, err, shift.ErrRequestConflict)

		// The same is true for a request id this close command itself
		// consumed with a different fingerprint: the replay comparison happens
		// before any state check, so a changed command shape cannot slip
		// through as a second close.
		replayRequestID := uuid.New()
		_, _, err = closes.Handle(ctx, f.Cashier.actor(),
			f.closeCommand(replayRequestID, initial.ID, observation.ID, []shift.CloseDiscrepancyInput{}, "", ""))
		require.NoError(t, err)

		_, _, err = closes.Handle(ctx, f.Cashier.actor(),
			f.closeCommand(replayRequestID, initial.ID, observation.ID,
				[]shift.CloseDiscrepancyInput{reasonInput(shift.DimensionCash, shift.ReasonUnexplained)},
				f.Manager.LoginCode, f.Manager.Pin))
		require.Error(t, err)
		requireCodedError(t, err, 409, "REQUEST_CONFLICT")
		assert.ErrorIs(t, err, shift.ErrRequestConflict)

		// The conflicting attempts wrote no second closure: the exact close
		// that consumed replayRequestID stands alone.
		requireShiftClosureCounts(t, f, 1, 0)
	})

	t.Run("second independent close", func(t *testing.T) {
		f := newShiftFixture(t)
		cashID, qrID := shortageEvidence(t, f)
		closes := shift.NewCloseShiftHandler(f.Runner)

		// Two independent closers with different request ids race through the
		// Shift row lock: one snapshot wins, the loser reads CLOSED and reports
		// SALES_SHIFT_ALREADY_CLOSED (spec 11.2).
		type outcome struct {
			status int
			res    shift.ClosedShiftDetailResponse
			err    error
		}
		results := make(chan outcome, 2)
		var wg sync.WaitGroup
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				cmd := f.closeCommand(uuid.New(), cashID, qrID,
					[]shift.CloseDiscrepancyInput{reasonInput(shift.DimensionCash, shift.ReasonUnexplained)},
					f.Manager.LoginCode, f.Manager.Pin)
				status, res, err := closes.Handle(ctx, f.Cashier.actor(), cmd)
				results <- outcome{status: status, res: res, err: err}
			}()
		}
		wg.Wait()
		close(results)

		wins, losses := 0, 0
		for r := range results {
			switch {
			case r.err == nil:
				wins++
				assert.Equal(t, 200, r.status)
			default:
				losses++
				assert.ErrorIs(t, r.err, shift.ErrShiftAlreadyClosed)
				requireCodedError(t, r.err, 409, "SALES_SHIFT_ALREADY_CLOSED")
			}
		}
		assert.Equal(t, 1, wins, "exactly one independent close commits the snapshot")
		assert.Equal(t, 1, losses, "the second close loses under the Shift row lock")

		requireShiftClosureCounts(t, f, 1, 1)
		assert.Equal(t, shift.StateClosed, requireShiftState(t, f))
	})
}

// shortageEvidence moves the fixture's Shift to a CLOSING state whose final
// evidence carries a 1000 cash shortage after a recount, returning the final
// Cash Count and QR Observation ids.
func shortageEvidence(t *testing.T, f shiftFixture) (uuid.UUID, uuid.UUID) {
	t.Helper()
	startReconciliationExact(t, f)
	recount := appendCashCount(t, f, 499_000)
	observation := appendQRObservation(t, f, 0, 0)
	return recount.ID, observation.ID
}

// latestCashCountID reads the Shift reconciliation's latest Cash Count id.
func latestCashCountID(t *testing.T, f shiftFixture) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	require.NoError(t, f.DB.QueryRow(`
		SELECT c.id FROM shift_cash_counts c
		JOIN shift_reconciliations r ON r.id = c.reconciliation_id
		WHERE r.sales_shift_id = $1
		ORDER BY c.sequence DESC, c.id DESC LIMIT 1`, f.Shift.ID).Scan(&id))
	return id
}

// TestCloseShiftHTTP drives the close route end to end: body validation at the
// boundary, the approval denial collapse, and the exact close's response shape
// through the full middleware stack (spec 9.4, 12).
func TestCloseShiftHTTP(t *testing.T) {
	e, q := newTestServer(t)
	cashierToken, _ := signIn(t, e, q, []string{"CASHIER"}, "2468")
	_, managerCode := signIn(t, e, q, []string{"MANAGER"}, "8642")

	// Open a Shift, start its reconciliation with an exact count, and record
	// one QR observation, all through the API.
	body, _ := json.Marshal(map[string]any{"request_id": uuid.New(), "opening_float_vnd": 500000})
	rec := doRequest(t, e, http.MethodPost, "/api/v1/shifts", cashierToken, body)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var env envelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	var opened shift.SalesShiftResponse
	require.NoError(t, json.Unmarshal(env.Data, &opened))

	body, _ = json.Marshal(map[string]any{"request_id": uuid.New(), "counted_cash_vnd": 500000})
	rec = doRequest(t, e, http.MethodPost,
		"/api/v1/shifts/"+opened.ID.String()+"/reconciliation", cashierToken, body)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	var started shift.ClosingShiftResponse
	require.NoError(t, json.Unmarshal(env.Data, &started))
	require.Len(t, started.Reconciliation.CashCounts, 1)
	cashID := started.Reconciliation.CashCounts[0].ID

	body, _ = json.Marshal(map[string]any{
		"request_id": uuid.New(), "observed_received_vnd": 0, "observed_refunded_vnd": 0,
	})
	rec = doRequest(t, e, http.MethodPost,
		"/api/v1/shifts/"+opened.ID.String()+"/reconciliation/qr-observations", cashierToken, body)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	var observation shift.QRObservationResult
	require.NoError(t, json.Unmarshal(env.Data, &observation))
	qrID := observation.QRObservation.ID

	closePath := "/api/v1/shifts/" + opened.ID.String() + "/close"

	t.Run("boundary validation", func(t *testing.T) {
		cases := []struct {
			name string
			body map[string]any
		}{
			{
				name: "final_cash_count_id omitted",
				body: map[string]any{"request_id": uuid.New(), "final_qr_observation_id": qrID, "discrepancies": []any{}},
			},
			{
				name: "final_qr_observation_id omitted",
				body: map[string]any{"request_id": uuid.New(), "final_cash_count_id": cashID, "discrepancies": []any{}},
			},
			{
				name: "discrepancies null",
				body: map[string]any{"request_id": uuid.New(), "final_cash_count_id": cashID,
					"final_qr_observation_id": qrID, "discrepancies": nil},
			},
			{
				name: "discrepancies omitted",
				body: map[string]any{"request_id": uuid.New(), "final_cash_count_id": cashID,
					"final_qr_observation_id": qrID},
			},
			{
				name: "unknown reason",
				body: map[string]any{"request_id": uuid.New(), "final_cash_count_id": cashID,
					"final_qr_observation_id": qrID,
					"discrepancies":           []any{map[string]any{"dimension": "CASH", "reason": "MYSTERY"}}},
			},
			{
				name: "unknown dimension",
				body: map[string]any{"request_id": uuid.New(), "final_cash_count_id": cashID,
					"final_qr_observation_id": qrID,
					"discrepancies":           []any{map[string]any{"dimension": "BAGS", "reason": "UNEXPLAINED"}}},
			},
			{
				name: "OTHER without note",
				body: map[string]any{"request_id": uuid.New(), "final_cash_count_id": cashID,
					"final_qr_observation_id": qrID,
					"discrepancies":           []any{map[string]any{"dimension": "CASH", "reason": "OTHER"}}},
			},
			{
				name: "note on a non-OTHER reason",
				body: map[string]any{"request_id": uuid.New(), "final_cash_count_id": cashID,
					"final_qr_observation_id": qrID,
					"discrepancies": []any{map[string]any{"dimension": "CASH", "reason": "UNEXPLAINED",
						"note": "why is there a note"}}},
			},
			{
				name: "duplicate dimension",
				body: map[string]any{"request_id": uuid.New(), "final_cash_count_id": cashID,
					"final_qr_observation_id": qrID,
					"discrepancies": []any{
						map[string]any{"dimension": "MANUAL_QR_RECEIVED", "reason": "UNEXPLAINED"},
						map[string]any{"dimension": "MANUAL_QR_RECEIVED", "reason": "OTHER", "note": "again"},
					}},
			},
			{
				name: "malformed manager_pin",
				body: map[string]any{"request_id": uuid.New(), "final_cash_count_id": cashID,
					"final_qr_observation_id": qrID,
					"discrepancies":           []any{map[string]any{"dimension": "CASH", "reason": "UNEXPLAINED"}},
					"approver_login_code":     managerCode, "manager_pin": "abc",
				},
			},
			{
				name: "approval pair omitted",
				body: map[string]any{"request_id": uuid.New(), "final_cash_count_id": cashID,
					"final_qr_observation_id": qrID,
					"discrepancies":           []any{map[string]any{"dimension": "CASH", "reason": "UNEXPLAINED"}},
				},
			},
			{
				name: "manager_pin omitted",
				body: map[string]any{"request_id": uuid.New(), "final_cash_count_id": cashID,
					"final_qr_observation_id": qrID,
					"discrepancies":           []any{map[string]any{"dimension": "CASH", "reason": "UNEXPLAINED"}},
					"approver_login_code":     managerCode,
				},
			},
			{
				name: "approver_login_code omitted",
				body: map[string]any{"request_id": uuid.New(), "final_cash_count_id": cashID,
					"final_qr_observation_id": qrID,
					"discrepancies":           []any{map[string]any{"dimension": "CASH", "reason": "UNEXPLAINED"}},
					"manager_pin":             "8642",
				},
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				body, _ := json.Marshal(tc.body)
				rec := doRequest(t, e, http.MethodPost, closePath, cashierToken, body)
				require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())

				var errEnv envelope
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errEnv))
				require.NotNil(t, errEnv.Error)
				assert.Equal(t, "INVALID_INPUT", errEnv.Error.Code)
			})
		}
	})

	t.Run("wrong manager pin is forbidden through the route", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"request_id": uuid.New(), "final_cash_count_id": cashID, "final_qr_observation_id": qrID,
			"discrepancies":       []any{map[string]any{"dimension": "CASH", "reason": "UNEXPLAINED"}},
			"approver_login_code": managerCode, "manager_pin": "0000",
		})
		rec := doRequest(t, e, http.MethodPost, closePath, cashierToken, body)
		require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

		var errEnv envelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errEnv))
		require.NotNil(t, errEnv.Error)
		assert.Equal(t, "MANAGER_APPROVAL_UNAVAILABLE", errEnv.Error.Code)
	})

	t.Run("unknown shift", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"request_id": uuid.New(), "final_cash_count_id": cashID, "final_qr_observation_id": qrID,
			"discrepancies": []any{},
		})
		rec := doRequest(t, e, http.MethodPost,
			"/api/v1/shifts/"+uuid.New().String()+"/close", cashierToken, body)
		require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())

		var errEnv envelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errEnv))
		require.NotNil(t, errEnv.Error)
		assert.Equal(t, "SALES_SHIFT_NOT_FOUND", errEnv.Error.Code)
	})

	t.Run("malformed shift id", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"request_id": uuid.New(), "final_cash_count_id": cashID, "final_qr_observation_id": qrID,
			"discrepancies": []any{},
		})
		rec := doRequest(t, e, http.MethodPost, "/api/v1/shifts/not-a-uuid/close", cashierToken, body)
		assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	})

	t.Run("denies barista", func(t *testing.T) {
		baristaToken, _ := signIn(t, e, q, []string{"BARISTA"}, "1357")
		body, _ := json.Marshal(map[string]any{
			"request_id": uuid.New(), "final_cash_count_id": cashID, "final_qr_observation_id": qrID,
			"discrepancies": []any{},
		})
		rec := doRequest(t, e, http.MethodPost, closePath, baristaToken, body)
		assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	})

	t.Run("denies anonymous", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"request_id": uuid.New(), "final_cash_count_id": cashID, "final_qr_observation_id": qrID,
			"discrepancies": []any{},
		})
		rec := doRequest(t, e, http.MethodPost, closePath, "", body)
		assert.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
	})

	t.Run("exact close returns the immutable detail", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"request_id": uuid.New(), "final_cash_count_id": cashID, "final_qr_observation_id": qrID,
			"discrepancies": []any{},
		})
		rec := doRequest(t, e, http.MethodPost, closePath, cashierToken, body)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
		require.True(t, env.Success)

		var detail map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(env.Data, &detail))
		for _, key := range []string{
			"id", "opener", "closer", "opened_at", "closed_at", "opening_float_vnd",
			"expected_cash_vnd", "observed_cash_vnd", "cash_difference_vnd",
			"expected_manual_qr_received_vnd", "observed_manual_qr_received_vnd",
			"manual_qr_received_difference_vnd",
			"expected_manual_qr_refunded_vnd", "observed_manual_qr_refunded_vnd",
			"manual_qr_refunded_difference_vnd", "has_discrepancy",
			"starter", "pay_in_vnd", "pay_out_vnd",
			"cash_payment_vnd", "cash_payment_void_vnd", "cash_refund_vnd",
			"manual_qr_payment_vnd", "manual_qr_payment_void_vnd",
			"pending_manual_qr_refund_vnd", "pending_refund_vnd",
			"unresolved_post_sale_adjustment_vnd",
			"cash_counts", "qr_observations", "discrepancies", "approver",
		} {
			assert.Contains(t, detail, key)
		}
		assert.JSONEq(t, "false", string(detail["has_discrepancy"]))
		assert.JSONEq(t, "[]", string(detail["discrepancies"]))
		assert.JSONEq(t, "null", string(detail["approver"]))

		// No PIN or approval login input field exists on the response.
		raw := string(env.Data)
		assert.NotContains(t, raw, "manager_pin")
		assert.NotContains(t, raw, "approver_login_code")
	})
}
