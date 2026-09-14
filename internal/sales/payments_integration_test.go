//go:build integration

package sales_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPayCash(t *testing.T) {
	env := newSalesEnv(t)
	ctx := context.Background()

	t.Run("paying in full settles the check with complete evidence", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 2)
		checkID := env.soleCheckID(t, session.ID)
		charge := env.checkCharge(t, checkID)

		got, _, err := env.payCash(t, checkID, charge, charge+15_000)
		require.NoError(t, err)

		require.Len(t, got.Checks, 1)
		require.Equal(t, sales.CheckStateSettled, got.Checks[0].State)
		require.Equal(t, charge, got.Checks[0].TotalAppliedVND)
		require.Equal(t, int64(0), got.Checks[0].BalanceVND)
		require.Len(t, got.Checks[0].Payments, 1)
		require.Equal(t, int64(15_000), *got.Checks[0].Payments[0].ChangeDueVND)

		var settledBy, shift, session2 uuid.UUID
		require.NoError(t, env.DB.QueryRowContext(ctx, `
			SELECT settled_by_staff_identity_id, settled_during_sales_shift_id,
			       settled_staff_access_session_id
			FROM checks WHERE id = $1`, checkID).Scan(&settledBy, &shift, &session2))
		require.Equal(t, env.Actor.StaffID, settledBy)
		require.Equal(t, env.ShiftID, shift)
		require.Equal(t, env.Actor.SessionID, session2)
	})

	t.Run("a partial payment leaves the check open", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 2)
		checkID := env.soleCheckID(t, session.ID)
		charge := env.checkCharge(t, checkID)

		got, _, err := env.payCash(t, checkID, charge-1_000, charge-1_000)
		require.NoError(t, err)
		require.Equal(t, sales.CheckStateOpen, got.Checks[0].State)
		require.Equal(t, int64(1_000), got.Checks[0].BalanceVND)
	})

	t.Run("over the balance is rejected and records nothing", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 1)
		checkID := env.soleCheckID(t, session.ID)
		charge := env.checkCharge(t, checkID)

		_, status, err := env.payCash(t, checkID, charge+1, charge+1)
		require.Error(t, err)
		require.Equal(t, http.StatusConflict, status)
		require.ErrorIs(t, err, sales.ErrPaymentExceedsBalance)
		require.Equal(t, 0, env.countPayments(t, checkID))
	})

	t.Run("under-tendered cash is rejected", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 1)
		checkID := env.soleCheckID(t, session.ID)
		charge := env.checkCharge(t, checkID)

		_, _, err := env.payCash(t, checkID, charge, charge-1)
		require.ErrorIs(t, err, sales.ErrInsufficientCashTendered)
		require.Equal(t, 0, env.countPayments(t, checkID))
	})

	t.Run("paying a settled check reports CHECK_NOT_OPEN", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 1)
		checkID := env.soleCheckID(t, session.ID)
		charge := env.checkCharge(t, checkID)

		_, _, err := env.payCash(t, checkID, charge, charge)
		require.NoError(t, err)

		_, _, err = env.payCash(t, checkID, 1_000, 1_000)
		require.ErrorIs(t, err, sales.ErrCheckNotOpen)
	})

	t.Run("an unknown check reports CHECK_NOT_FOUND", func(t *testing.T) {
		_, status, err := env.payCash(t, uuid.New(), 1_000, 1_000)
		require.ErrorIs(t, err, sales.ErrCheckNotFound)
		require.Equal(t, http.StatusNotFound, status)
	})

	t.Run("a settling payment writes two audit events", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 1)
		checkID := env.soleCheckID(t, session.ID)
		charge := env.checkCharge(t, checkID)

		before := env.countAuditEvents(t, sales.EventCheckSettled)
		_, _, err := env.payCash(t, checkID, charge, charge)
		require.NoError(t, err)

		require.Equal(t, before+1, env.countAuditEvents(t, sales.EventCheckSettled))
		require.Equal(t, 1, env.countAuditEventsForCheck(t, sales.EventCashPaymentRecorded, checkID))
	})

	t.Run("an exact replay records only one payment", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 1)
		checkID := env.soleCheckID(t, session.ID)
		charge := env.checkCharge(t, checkID)
		requestID := uuid.New()

		first, _, err := env.payCashWithRequestID(t, requestID, checkID, charge, charge)
		require.NoError(t, err)
		second, _, err := env.payCashWithRequestID(t, requestID, checkID, charge, charge)
		require.NoError(t, err)

		require.Equal(t, first.Checks[0].ID, second.Checks[0].ID)
		require.Equal(t, 1, env.countPayments(t, checkID))
	})

	t.Run("the same request id with a different amount conflicts", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 2)
		checkID := env.soleCheckID(t, session.ID)
		charge := env.checkCharge(t, checkID)
		requestID := uuid.New()

		_, _, err := env.payCashWithRequestID(t, requestID, checkID, 1_000, 1_000)
		require.NoError(t, err)
		_, _, err = env.payCashWithRequestID(t, requestID, checkID, 2_000, 2_000)
		require.ErrorIs(t, err, sales.ErrRequestConflict)
		_ = charge
	})

	t.Run("a barista is denied", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 1)
		checkID := env.soleCheckID(t, session.ID)

		_, status, err := env.payCashAs(t, env.BaristaActor(), checkID, 1_000, 1_000)
		require.Error(t, err)
		require.Equal(t, http.StatusForbidden, status)
		require.Equal(t, 0, env.countPayments(t, checkID))
	})
}
