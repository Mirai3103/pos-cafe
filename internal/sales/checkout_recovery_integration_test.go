//go:build integration

package sales_test

import (
	"context"
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
