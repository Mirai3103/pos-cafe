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
