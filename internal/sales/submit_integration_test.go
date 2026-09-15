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

func TestSubmitTakeaway(t *testing.T) {
	env := newSalesEnv(t)
	ctx := context.Background()

	t.Run("a settled takeaway check submits", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 2)
		checkID := env.soleCheckID(t, session.ID)
		charge := env.checkCharge(t, checkID)
		_, _, err := env.payCash(t, checkID, charge, charge)
		require.NoError(t, err)

		got := env.Submit(t, session.ID)

		require.Len(t, got.Orders, 1)
		require.Len(t, got.Orders[0].Items, 2)
		require.Equal(t, env.Actor.StaffID, got.Orders[0].SubmittedByStaffID)
		require.NotEmpty(t, got.PreparationUnits)
		for _, allocation := range got.Checks[0].Allocations {
			require.True(t, allocation.Submitted)
		}
	})

	t.Run("an unsettled takeaway check is refused", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 1)

		_, _, err := env.TrySubmit(t, session.ID)
		require.ErrorIs(t, err, sales.ErrCheckNotSettledForSubmission)

		got := env.GetSessionOK(t, session.ID)
		require.Empty(t, got.Orders, "a refused Submit records nothing")
		require.Empty(t, got.PreparationUnits)
	})

	t.Run("a session with no committed draft is refused", func(t *testing.T) {
		// 5A opens every Session with its one EDITABLE draft, so a fresh
		// Session already holds no COMMITTED draft — the state Submit's lock
		// query filters for. Seeding another EDITABLE draft would violate
		// order_draft_editable_per_session_unique.
		session := env.StartTakeaway(t)

		_, _, err := env.TrySubmit(t, session.ID)
		require.ErrorIs(t, err, sales.ErrNothingToSubmit)
	})

	t.Run("an unknown session is not found", func(t *testing.T) {
		// Spec §9.3: a missing source is 404, not the blanket 409 the old
		// single-joint lock query collapsed every miss into.
		_, status, err := env.TrySubmit(t, uuid.New())
		require.ErrorIs(t, err, sales.ErrServiceSessionNotFound)
		require.Equal(t, http.StatusNotFound, status)
	})

	t.Run("a closed session is refused, not 'nothing to submit'", func(t *testing.T) {
		session := readyToClose(t, env, 1)
		env.Close(t, session.ID)

		_, status, err := env.TrySubmit(t, session.ID)
		require.ErrorIs(t, err, sales.ErrServiceSessionClosed)
		require.Equal(t, http.StatusConflict, status)
	})

	t.Run("submitting twice with a fresh request id is a no-op", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 1)
		checkID := env.soleCheckID(t, session.ID)
		charge := env.checkCharge(t, checkID)
		_, _, err := env.payCash(t, checkID, charge, charge)
		require.NoError(t, err)
		env.Submit(t, session.ID)

		// UNIQUE (order_draft_id) means the draft is no longer submittable,
		// so the second attempt finds nothing awaiting submission.
		_, _, err = env.TrySubmit(t, session.ID)
		require.ErrorIs(t, err, sales.ErrNothingToSubmit)

		var n int
		require.NoError(t, env.DB.QueryRowContext(ctx,
			`SELECT count(*) FROM orders WHERE service_session_id = $1`, session.ID).Scan(&n))
		require.Equal(t, 1, n)
	})

	t.Run("a replayed request id returns the stored result", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 1)
		checkID := env.soleCheckID(t, session.ID)
		charge := env.checkCharge(t, checkID)
		_, _, err := env.payCash(t, checkID, charge, charge)
		require.NoError(t, err)

		requestID := uuid.New()
		first, _, err := env.SubmitWithRequestID(t, requestID, session.ID)
		require.NoError(t, err)
		second, _, err := env.SubmitWithRequestID(t, requestID, session.ID)
		require.NoError(t, err)
		require.Equal(t, first.Orders[0].ID, second.Orders[0].ID)
	})

	t.Run("a barista cannot submit", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 1)
		checkID := env.soleCheckID(t, session.ID)
		charge := env.checkCharge(t, checkID)
		_, _, err := env.payCash(t, checkID, charge, charge)
		require.NoError(t, err)

		_, _, err = env.SubmitAs(t, env.BaristaActor(), session.ID)
		require.ErrorIs(t, err, sales.ErrForbidden)
	})
}

func TestSubmitDineIn(t *testing.T) {
	env := newSalesEnv(t)

	t.Run("an unsettled dine-in check submits", func(t *testing.T) {
		// Dine-in supports Commit -> Submit -> Payment: a seated customer's
		// drinks go to the bar long before the bill is settled. A rule that
		// forced settlement first would make dine-in unusable.
		session := env.commitDineInDraft(t, 2)

		got := env.Submit(t, session.ID)
		require.Len(t, got.Orders, 1)
		require.Equal(t, sales.CheckStateOpen, got.Checks[0].State)
	})

	t.Run("dine-in also supports Commit -> Payment -> Submit", func(t *testing.T) {
		session := env.commitDineInDraft(t, 1)
		checkID := env.soleCheckID(t, session.ID)
		charge := env.checkCharge(t, checkID)
		_, _, err := env.payCash(t, checkID, charge, charge)
		require.NoError(t, err)

		got := env.Submit(t, session.ID)
		require.Len(t, got.Orders, 1)
		require.Equal(t, sales.CheckStateSettled, got.Checks[0].State)
	})
}

func TestSubmitFansOutPreparationUnits(t *testing.T) {
	env := newSalesEnv(t)

	t.Run("quantity three produces three units numbered 1..3", func(t *testing.T) {
		session := env.commitDineInDraftWithQuantity(t, 3)

		got := env.Submit(t, session.ID)

		require.Len(t, got.PreparationUnits, 3)
		numbers := map[int32]bool{}
		for _, unit := range got.PreparationUnits {
			numbers[unit.UnitNumber] = true
			require.Equal(t, sales.UnitStateQueued, unit.State)
			require.Equal(t, got.ServiceNumber, unit.ServiceNumber)
			require.NotEmpty(t, unit.ItemName)
			require.NotNil(t, unit.Modifiers)
		}
		require.Equal(t, map[int32]bool{1: true, 2: true, 3: true}, numbers)
	})
}

func TestSubmitWritesAuditEvent(t *testing.T) {
	env := newSalesEnv(t)
	session := env.commitDineInDraft(t, 1)
	before := env.countAuditEvents(t, sales.EventOrderSubmitted)

	env.Submit(t, session.ID)

	require.Equal(t, before+1, env.countAuditEvents(t, sales.EventOrderSubmitted))
}
