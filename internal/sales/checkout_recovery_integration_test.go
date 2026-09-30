//go:build integration

package sales_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// recoveryEnv drives the Phase 08 commands over the shared Sales world.
type recoveryEnv struct {
	*salesEnv
	// managerCode is the env manager's login code, for inline Refund approval.
	managerCode string
}

func newRecoveryEnv(t *testing.T) *recoveryEnv {
	t.Helper()
	env := &recoveryEnv{salesEnv: newSalesEnv(t)}
	require.NoError(t, env.DB.QueryRow(
		`SELECT login_code FROM staff_identities WHERE id = $1`, env.Actor.StaffID).
		Scan(&env.managerCode))
	return env
}

// paidTakeaway commits one Coffee at quantity 2 (50,000 VND) and pays it in
// cash without submitting: the Phase 08 failure boundary (spec §3).
func (e *recoveryEnv) paidTakeaway(t *testing.T) (sales.ServiceSessionResponse, uuid.UUID) {
	t.Helper()
	session := e.commitTakeawayDraft(t, 1)
	checkID := e.soleCheckID(t, session.ID)
	_, _, err := e.payCash(t, checkID, 50000, 50000)
	require.NoError(t, err)
	return e.GetSessionOK(t, session.ID), checkID
}

func TestAwaitingSubmissionIsVisibleAndBlocksClosure(t *testing.T) {
	env := newRecoveryEnv(t)
	session, _ := env.paidTakeaway(t)

	require.True(t, session.AwaitingSubmission)
	require.Len(t, session.AwaitingSubmissionCommittedItemIDs, 1)

	_, _, err := env.TryClose(t, session.ID)
	require.ErrorIs(t, err, sales.ErrAwaitingSubmissionForClosure)
}

func TestAwaitingSubmissionRetryCreatesOneOrder(t *testing.T) {
	env := newRecoveryEnv(t)
	ctx := context.Background()
	session, _ := env.paidTakeaway(t)

	requestID := uuid.New()
	first, _, err := env.SubmitWithRequestID(t, requestID, session.ID)
	require.NoError(t, err)
	require.False(t, first.AwaitingSubmission)

	replay, _, err := env.SubmitWithRequestID(t, requestID, session.ID)
	require.NoError(t, err)
	require.Equal(t, first.Orders[0].ID, replay.Orders[0].ID)

	_, _, err = env.TrySubmit(t, session.ID)
	require.ErrorIs(t, err, sales.ErrNothingToSubmit)

	var orders, units int
	require.NoError(t, env.DB.QueryRowContext(ctx,
		`SELECT count(*) FROM orders WHERE service_session_id = $1`, session.ID).Scan(&orders))
	require.NoError(t, env.DB.QueryRowContext(ctx, `
		SELECT count(*) FROM preparation_units pu
		JOIN order_items oi ON oi.id = pu.order_item_id
		JOIN orders o ON o.id = oi.order_id
		WHERE o.service_session_id = $1`, session.ID).Scan(&units))
	require.Equal(t, 1, orders)
	require.Equal(t, 2, units, "quantity 2 is two Preparation Units")
}

func (e *recoveryEnv) cancelWithRequestID(t *testing.T, requestID, sessionID uuid.UUID,
	reason string, note *string,
) (sales.ServiceSessionResponse, int, error) {
	t.Helper()
	status, resp, err := sales.NewCancelAwaitingSubmissionHandler(e.Runner).
		Handle(context.Background(), e.Actor, sales.CheckoutRecoveryCommand{
			RequestID: requestID, ServiceSessionID: sessionID, Reason: reason, Note: note,
		})
	if err != nil {
		status, err = mapErrorStatus(err)
	}
	return resp, status, err
}

func (e *recoveryEnv) cancel(t *testing.T, sessionID uuid.UUID) (
	sales.ServiceSessionResponse, int, error,
) {
	t.Helper()
	return e.cancelWithRequestID(t, uuid.New(), sessionID, sales.RecoveryReasonCustomerLeft, nil)
}

func TestCancelAwaitingSubmissionWithdrawsTheCharge(t *testing.T) {
	env := newRecoveryEnv(t)
	session, checkID := env.paidTakeaway(t)

	got, status, err := env.cancel(t, session.ID)
	require.NoError(t, err)
	require.Equal(t, 200, status)

	require.False(t, got.AwaitingSubmission)
	check := env.findCheck(t, got, checkID)
	require.Equal(t, int64(0), check.ChargeVND)
	require.Equal(t, int64(50000), check.PendingRefundVND)
	require.Equal(t, sales.CheckStateSettled, check.State)
	require.Len(t, check.ChargeAdjustments, 1)
	require.Equal(t, sales.ChargeAdjustmentKindWithdrawal, check.ChargeAdjustments[0].Kind)
	require.Nil(t, check.ChargeAdjustments[0].PreparationUnitID)
	require.True(t, check.Allocations[0].Withdrawn)
	env.RequireDraftState(t, session.ID, sales.DraftStateCancelled)
	require.Equal(t, 1, env.countAuditEvents(t, sales.EventAwaitingSubmissionCancelled))

	_, _, err = env.TrySubmit(t, session.ID)
	require.ErrorIs(t, err, sales.ErrNothingToSubmit, "a cancelled draft is never submitted")
}

func TestCancelAwaitingSubmissionSettlesAPartiallyPaidCheck(t *testing.T) {
	env := newRecoveryEnv(t)
	session := env.commitDineInDraft(t, 1) // 50,000 VND, dine-in allows partial payment
	checkID := env.soleCheckID(t, session.ID)
	_, _, err := env.payCash(t, checkID, 20000, 20000)
	require.NoError(t, err)

	got, _, err := env.cancel(t, session.ID)
	require.NoError(t, err)
	check := env.findCheck(t, got, checkID)
	require.Equal(t, sales.CheckStateSettled, check.State)
	require.Equal(t, int64(20000), check.PendingRefundVND)
	require.Equal(t, 1, env.countAuditEventsForCheck(t, sales.EventCheckSettled, checkID))
}

func TestCancelAwaitingSubmissionRejections(t *testing.T) {
	env := newRecoveryEnv(t)

	unpaid := env.commitTakeawayDraft(t, 1)
	_, _, err := env.cancel(t, unpaid.ID)
	require.ErrorIs(t, err, sales.ErrNothingAwaitingSubmission)

	submitted, _ := env.paidTakeaway(t)
	env.Submit(t, submitted.ID)
	_, _, err = env.cancel(t, submitted.ID)
	require.ErrorIs(t, err, sales.ErrSessionHasOrder)

	_, _, err = env.cancel(t, uuid.New())
	require.ErrorIs(t, err, sales.ErrServiceSessionNotFound)

	paid, _ := env.paidTakeaway(t)
	_, status, err := env.cancelWithRequestID(t, uuid.New(), paid.ID, sales.RecoveryReasonOther, nil)
	require.Error(t, err)
	require.Equal(t, 400, status)
}

func TestCancelAwaitingSubmissionReplays(t *testing.T) {
	env := newRecoveryEnv(t)
	session, _ := env.paidTakeaway(t)

	requestID := uuid.New()
	first, _, err := env.cancelWithRequestID(t, requestID, session.ID, sales.RecoveryReasonCustomerLeft, nil)
	require.NoError(t, err)
	replay, _, err := env.cancelWithRequestID(t, requestID, session.ID, sales.RecoveryReasonCustomerLeft, nil)
	require.NoError(t, err)
	require.Equal(t, first.Checks[0].ChargeAdjustments[0].ID, replay.Checks[0].ChargeAdjustments[0].ID)

	_, _, err = env.cancel(t, session.ID)
	require.ErrorIs(t, err, sales.ErrNothingAwaitingSubmission)
	require.Equal(t, 1, env.countAuditEvents(t, sales.EventAwaitingSubmissionCancelled))
}

func (e *recoveryEnv) abandonWithRequestID(t *testing.T, requestID, sessionID uuid.UUID,
	reason string, note *string,
) (sales.ServiceSessionResponse, int, error) {
	t.Helper()
	status, resp, err := sales.NewAbandonCheckoutHandler(e.Runner).
		Handle(context.Background(), e.Actor, sales.CheckoutRecoveryCommand{
			RequestID: requestID, ServiceSessionID: sessionID, Reason: reason, Note: note,
		})
	if err != nil {
		status, err = mapErrorStatus(err)
	}
	return resp, status, err
}

func (e *recoveryEnv) abandon(t *testing.T, sessionID uuid.UUID) (
	sales.ServiceSessionResponse, int, error,
) {
	t.Helper()
	return e.abandonWithRequestID(t, uuid.New(), sessionID, sales.RecoveryReasonCustomerLeft, nil)
}

// refundWithdrawal refunds the Check's whole withdrawn charge against its
// single Payment, through the unchanged Refund command.
func (e *recoveryEnv) refundWithdrawal(t *testing.T, sessionID, checkID uuid.UUID,
	method string,
) sales.RefundResult {
	t.Helper()
	check := e.findCheck(t, e.GetSessionOK(t, sessionID), checkID)
	require.Len(t, check.ChargeAdjustments, 1)
	require.Len(t, check.Payments, 1)
	amount := check.ChargeAdjustments[0].AmountVND
	status, result, err := sales.NewRecordRefundHandler(e.Runner).
		Handle(context.Background(), e.Actor, sales.RecordRefundCommand{
			RequestID: uuid.New(),
			CheckID:   checkID,
			Method:    method,
			AdjustmentAllocations: []sales.RefundAdjustmentAllocationInput{
				{ChargeAdjustmentID: check.ChargeAdjustments[0].ID, AmountVND: amount},
			},
			PaymentAllocations: []sales.RefundPaymentAllocationInput{
				{PaymentID: check.Payments[0].ID, AmountVND: amount},
			},
			Reason: sales.RefundReasonCustomerRequest,
			ManagerApproval: sales.ManagerApprovalInput{
				ApproverLoginCode: e.managerCode, ManagerPIN: "1234",
			},
		})
	require.NoError(t, err)
	require.Equal(t, 201, status)
	return result
}

func TestAbandonUnpaidDineInReleasesTheTable(t *testing.T) {
	env := newRecoveryEnv(t)
	ctx := context.Background()
	session := env.commitDineInDraft(t, 1)

	got, status, err := env.abandon(t, session.ID)
	require.NoError(t, err)
	require.Equal(t, 200, status)
	require.Equal(t, sales.StateAbandoned, got.State)
	require.NotNil(t, got.AbandonedCheckout)
	require.Equal(t, sales.RecoveryReasonCustomerLeft, got.AbandonedCheckout.Reason)
	for _, check := range got.Checks {
		require.Equal(t, sales.CheckStateAbandoned, check.State)
	}

	var held int
	require.NoError(t, env.DB.QueryRowContext(ctx, `
		SELECT count(*) FROM table_assignments
		WHERE service_session_id = $1 AND released_at IS NULL`, session.ID).Scan(&held))
	require.Equal(t, 0, held)

	var orders, completed int
	require.NoError(t, env.DB.QueryRowContext(ctx,
		`SELECT count(*) FROM orders WHERE service_session_id = $1`, session.ID).Scan(&orders))
	require.NoError(t, env.DB.QueryRowContext(ctx,
		`SELECT count(*) FROM completed_sales WHERE service_session_id = $1`, session.ID).Scan(&completed))
	require.Zero(t, orders)
	require.Zero(t, completed)
	require.Equal(t, 1, env.countAuditEvents(t, sales.EventCheckoutAbandoned))
	require.Equal(t, 1, env.countAuditEvents(t, sales.EventTableAssignmentReleased))
}

func TestAbandonEmptySession(t *testing.T) {
	env := newRecoveryEnv(t)
	session := env.StartTakeaway(t)

	got, _, err := env.abandon(t, session.ID)
	require.NoError(t, err)
	require.Equal(t, sales.StateAbandoned, got.State)
	require.Nil(t, got.Draft)
}

func TestAbandonRejections(t *testing.T) {
	env := newRecoveryEnv(t)

	paid, _ := env.paidTakeaway(t)
	_, _, err := env.abandon(t, paid.ID)
	require.ErrorIs(t, err, sales.ErrPaymentRequiresRefund)

	withOrder, _ := env.paidTakeaway(t)
	env.Submit(t, withOrder.ID)
	_, _, err = env.abandon(t, withOrder.ID)
	require.ErrorIs(t, err, sales.ErrSessionHasOrder)

	cancelledNotRefunded, _ := env.paidTakeaway(t)
	_, _, err = env.cancel(t, cancelledNotRefunded.ID)
	require.NoError(t, err)
	_, _, err = env.abandon(t, cancelledNotRefunded.ID)
	require.ErrorIs(t, err, sales.ErrPaymentRequiresRefund)

	note := "  "
	unpaid := env.commitTakeawayDraft(t, 1)
	_, status, err := env.abandonWithRequestID(t, uuid.New(), unpaid.ID, sales.RecoveryReasonOther, &note)
	require.Error(t, err)
	require.Equal(t, 400, status, "a blank note normalizes to none, and OTHER requires one")
}

func TestAbandonReplaysAndThenRefusesANewRequest(t *testing.T) {
	env := newRecoveryEnv(t)
	session := env.commitTakeawayDraft(t, 1)

	requestID := uuid.New()
	first, _, err := env.abandonWithRequestID(t, requestID, session.ID, sales.RecoveryReasonCustomerLeft, nil)
	require.NoError(t, err)
	replay, _, err := env.abandonWithRequestID(t, requestID, session.ID, sales.RecoveryReasonCustomerLeft, nil)
	require.NoError(t, err)
	require.Equal(t, first.AbandonedCheckout.ID, replay.AbandonedCheckout.ID)

	_, _, err = env.abandon(t, session.ID)
	require.ErrorIs(t, err, sales.ErrServiceSessionClosed)
}

func TestCancelRefundAbandonCash(t *testing.T) {
	env := newRecoveryEnv(t)
	ctx := context.Background()
	session, checkID := env.paidTakeaway(t)

	_, _, err := env.cancel(t, session.ID)
	require.NoError(t, err)
	env.refundWithdrawal(t, session.ID, checkID, sales.RefundMethodCash)

	got, _, err := env.abandon(t, session.ID)
	require.NoError(t, err)
	require.Equal(t, sales.StateAbandoned, got.State)

	var netCash int64
	require.NoError(t, env.DB.QueryRowContext(ctx, `
		SELECT COALESCE((SELECT SUM(applied_amount_vnd) FROM payments WHERE sales_shift_id = $1), 0)
		     - COALESCE((SELECT SUM(r.amount_vnd) FROM refunds r
		                 JOIN refund_completions rc ON rc.refund_id = r.id
		                 WHERE r.sales_shift_id = $1), 0)`, env.ShiftID).Scan(&netCash))
	require.Zero(t, netCash, "the cash that came in went back out")
}

func TestCancelRefundAbandonManualQR(t *testing.T) {
	env := newRecoveryEnv(t)
	session := env.commitTakeawayDraft(t, 1)
	checkID := env.soleCheckID(t, session.ID)
	_, _, err := env.payManualQR(t, checkID, 50000, true, nil)
	require.NoError(t, err)

	_, _, err = env.cancel(t, session.ID)
	require.NoError(t, err)
	result := env.refundWithdrawal(t, session.ID, checkID, sales.RefundMethodManualQR)
	require.Equal(t, sales.RefundStatePending, result.Refund.State)

	_, _, err = env.abandon(t, session.ID)
	require.ErrorIs(t, err, sales.ErrPaymentRequiresRefund, "a pending Refund has not moved money")

	_, _, err = sales.NewConfirmManualQRRefundHandler(env.Runner).
		Handle(context.Background(), env.Actor, sales.ConfirmManualQRRefundCommand{
			RequestID: uuid.New(), RefundID: result.Refund.ID,
		})
	require.NoError(t, err)

	got, _, err := env.abandon(t, session.ID)
	require.NoError(t, err)
	require.Equal(t, sales.StateAbandoned, got.State)
}

func TestShiftBlockersTrackAwaitingSubmission(t *testing.T) {
	env := newRecoveryEnv(t)
	ctx := context.Background()
	session, checkID := env.paidTakeaway(t)

	blockers, err := env.Queries.GetGlobalShiftClosureBlockers(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1), blockers.AwaitingSubmissionCount)

	_, _, err = env.cancel(t, session.ID)
	require.NoError(t, err)
	blockers, err = env.Queries.GetGlobalShiftClosureBlockers(ctx)
	require.NoError(t, err)
	require.Zero(t, blockers.AwaitingSubmissionCount)
	require.Equal(t, int64(50000), blockers.UnresolvedCorrectionVnd,
		"withdrawn but not yet refunded money blocks as an unresolved correction")

	env.refundWithdrawal(t, session.ID, checkID, sales.RefundMethodCash)
	_, _, err = env.abandon(t, session.ID)
	require.NoError(t, err)

	blockers, err = env.Queries.GetGlobalShiftClosureBlockers(ctx)
	require.NoError(t, err)
	require.Zero(t, blockers.UnsettledCheckCount)
	require.Zero(t, blockers.PendingRefundCount)
	require.Zero(t, blockers.UnresolvedCorrectionVnd)
	require.Zero(t, blockers.AwaitingSubmissionCount)
	require.Zero(t, blockers.ActiveServiceSessionCount, "the Shift may now close")
}

func TestCancelAgainstConcurrentSubmit(t *testing.T) {
	env := newRecoveryEnv(t)
	ctx := context.Background()
	session, _ := env.paidTakeaway(t)

	var wg sync.WaitGroup
	var cancelErr, submitErr error
	wg.Add(2)
	go func() { defer wg.Done(); _, _, cancelErr = env.cancel(t, session.ID) }()
	go func() { defer wg.Done(); _, _, submitErr = env.TrySubmit(t, session.ID) }()
	wg.Wait()

	require.True(t, (cancelErr == nil) != (submitErr == nil),
		"exactly one wins: cancel=%v submit=%v", cancelErr, submitErr)
	var orders int
	require.NoError(t, env.DB.QueryRowContext(ctx,
		`SELECT count(*) FROM orders WHERE service_session_id = $1`, session.ID).Scan(&orders))
	if cancelErr == nil {
		require.ErrorIs(t, submitErr, sales.ErrNothingToSubmit)
		require.Zero(t, orders)
	} else {
		require.ErrorIs(t, cancelErr, sales.ErrSessionHasOrder)
		require.Equal(t, 1, orders)
	}
}

func TestAbandonAgainstConcurrentCommit(t *testing.T) {
	env := newRecoveryEnv(t)
	session := env.StartTakeaway(t)
	env.AddDraftItem(t, session.ID, env.CoffeeID, nil)

	var wg sync.WaitGroup
	var abandonErr, commitErr error
	wg.Add(2)
	go func() { defer wg.Done(); _, _, abandonErr = env.abandon(t, session.ID) }()
	go func() { defer wg.Done(); _, commitErr = env.TryCommit(t, session.ID) }()
	wg.Wait()

	// Commit locks draft and Session in one statement, whose tuple-lock order
	// PostgreSQL does not fix, while Abandon locks the Session then writes the
	// draft: a 40P01 on either side is the recorded window (ADR-066).
	if abandonErr != nil {
		require.True(t, correctionRaceDeadlock(abandonErr), "abandon failed outside the window: %v", abandonErr)
	}
	if commitErr != nil {
		require.True(t, correctionRaceDeadlock(commitErr) || errors.Is(commitErr, sales.ErrEditableDraftNotFound),
			"commit failed unexpectedly: %v", commitErr)
	}
	require.True(t, abandonErr == nil || commitErr == nil,
		"at least one side stands: abandon=%v commit=%v", abandonErr, commitErr)
	if abandonErr == nil {
		require.Equal(t, sales.StateAbandoned, env.GetSessionOK(t, session.ID).State,
			"an unpaid commit never stops an abandon")
	}
}

func TestCancelAgainstConcurrentPayment(t *testing.T) {
	// ADR-066 inherits ADR-031's window: either side may abort with 40P01,
	// never both, and nothing half-written survives.
	env := newRecoveryEnv(t)
	session := env.commitDineInDraft(t, 1)
	checkID := env.soleCheckID(t, session.ID)
	_, _, err := env.payCash(t, checkID, 20000, 20000)
	require.NoError(t, err)

	var wg sync.WaitGroup
	var cancelErr, payErr error
	wg.Add(2)
	go func() { defer wg.Done(); _, _, cancelErr = env.cancel(t, session.ID) }()
	go func() { defer wg.Done(); _, _, payErr = env.payCash(t, checkID, 30000, 30000) }()
	wg.Wait()

	if cancelErr != nil {
		require.True(t, correctionRaceDeadlock(cancelErr), "cancel failed outside the window: %v", cancelErr)
	}
	// payErr is not constrained on its own: a Payment serialized after the
	// withdrawal meets a zero-charge, settled Check and is rejected by the
	// existing Payment rules, which is correct. The invariants below are what
	// must hold either way.
	require.True(t, cancelErr == nil || payErr == nil,
		"at least one side stands: cancel=%v pay=%v", cancelErr, payErr)

	got := env.GetSessionOK(t, session.ID)
	check := env.findCheck(t, got, checkID)
	if cancelErr == nil {
		require.Equal(t, int64(0), check.ChargeVND)
		require.Equal(t, check.EffectiveReceivedVND, check.PendingRefundVND,
			"every unit of money held is owed back")
	}
}

// A cancelled, orderless draft's Session must be abandoned, not re-ordered
// (ADR-066): a new round after Cancel would leave the withdrawn allocations
// unsubmitted forever and the Session stuck ACTIVE.
func TestCancelledDraftBlocksANewRoundUntilAbandon(t *testing.T) {
	env := newRecoveryEnv(t)
	session, checkID := env.paidTakeaway(t)

	_, _, err := env.cancel(t, session.ID)
	require.NoError(t, err)

	_, err = env.TryStartNewDraft(t, session.ID)
	require.ErrorIs(t, err, sales.ErrNewOrderDraftNotAvailable)

	env.refundWithdrawal(t, session.ID, checkID, sales.RefundMethodCash)
	got, _, err := env.abandon(t, session.ID)
	require.NoError(t, err)
	require.Equal(t, sales.StateAbandoned, got.State)
}
