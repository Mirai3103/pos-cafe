//go:build integration

package sales_test

import (
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestStartNewOrderDraftRejectedWhileADraftIsEditable(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartTakeaway(t)

	_, err := env.TryStartNewDraft(t, session.ID)

	require.ErrorIs(t, err, sales.ErrNewOrderDraftNotAvailable)
}

// In 5B every COMMITTED draft blocks, because the orders table 5D introduces
// does not exist yet. The rule exists to stop staff stacking rounds ahead of
// the kitchen; relaxing it now would ship a rule no phase wants.
func TestStartNewOrderDraftRejectedAfterCommitUntilSubmitExists(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartTakeaway(t)
	env.AddDraftItem(t, session.ID, env.CoffeeID, nil)
	env.Commit(t, session.ID)

	_, err := env.TryStartNewDraft(t, session.ID)

	require.ErrorIs(t, err, sales.ErrNewOrderDraftNotAvailable)
}

func TestStartNewOrderDraftRejectedForAnUnknownSession(t *testing.T) {
	env := newSalesEnv(t)

	_, err := env.TryStartNewDraft(t, uuid.New())

	require.ErrorIs(t, err, sales.ErrServiceSessionNotFound)
}

func TestStartNewOrderDraftDeniedForBarista(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartTakeaway(t)

	_, err := env.AsBarista().TryStartNewDraft(t, session.ID)

	require.ErrorIs(t, err, sales.ErrForbidden)
}

func TestSetCheckTargetDeniedForBarista(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartTakeaway(t)

	_, err := env.AsBarista().TrySetCheckTarget(t, session.ID, "NEW_CHECK")

	require.ErrorIs(t, err, sales.ErrForbidden)
}

func TestSetCheckTargetUpdatesTheDraft(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartTakeaway(t)

	// The brief's `got := env.SetCheckTarget(...)` exercises the handler
	// through the handler-invoking Try variant; the SQL-seeding helper cannot
	// return a projection.
	got, err := env.TrySetCheckTarget(t, session.ID, "NEW_CHECK")
	require.NoError(t, err)

	require.Equal(t, "NEW_CHECK", got.Draft.CheckTarget)
}

func TestCheckTargetDefaultsToCurrentUnpaid(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartTakeaway(t)

	require.Equal(t, "CURRENT_UNPAID", session.Draft.CheckTarget)
}

// The target belongs to the draft, not the Session.
func TestCheckTargetResetsWhenANewDraftOpens(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartTakeaway(t)
	env.SetCheckTarget(t, session.ID, "NEW_CHECK")
	env.AddDraftItem(t, session.ID, env.CoffeeID, nil)
	env.Commit(t, session.ID)

	env.SeedEditableDraftWithDefaultTarget(t, session.ID)

	got := env.GetSessionOK(t, session.ID)
	require.Equal(t, "CURRENT_UNPAID", got.Draft.CheckTarget)
}

func TestSetCheckTargetRejectedOnceCommitted(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartTakeaway(t)
	env.AddDraftItem(t, session.ID, env.CoffeeID, nil)
	env.Commit(t, session.ID)

	_, err := env.TrySetCheckTarget(t, session.ID, "NEW_CHECK")

	require.ErrorIs(t, err, sales.ErrEditableDraftNotFound)
}

func TestSetCheckTargetRejectsAnUnknownValue(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartTakeaway(t)

	_, err := env.TrySetCheckTarget(t, session.ID, "PAID")

	require.ErrorIs(t, err, sales.ErrInvalidCheckTarget)
}

// The brief writes `_, _, err := env.TryStartNewDraft(...)`, but this env's
// helper returns (response, error); dropping the second blank is the minimal
// equivalent change.
func TestMultipleRounds(t *testing.T) {
	env := newSalesEnv(t)

	t.Run("a submitted round unblocks the next draft", func(t *testing.T) {
		session := env.commitDineInDraft(t, 1)
		env.Submit(t, session.ID)

		_, err := env.TryStartNewDraft(t, session.ID)
		require.NoError(t, err)

		env.AddDraftItem(t, session.ID, env.TeaID, nil)
		second := env.Commit(t, session.ID)
		require.Len(t, second.Checks[0].Allocations, 2,
			"the second round joins the session's open Check")

		got := env.Submit(t, session.ID)
		require.Len(t, got.Orders, 2)
	})

	t.Run("an unsubmitted committed round still blocks", func(t *testing.T) {
		// The rule exists to stop staff stacking rounds ahead of the kitchen,
		// not to limit a Service Session to one round.
		session := env.commitDineInDraft(t, 1)

		_, err := env.TryStartNewDraft(t, session.ID)
		require.ErrorIs(t, err, sales.ErrNewOrderDraftNotAvailable)
	})

	t.Run("an editable draft still blocks", func(t *testing.T) {
		// The brief also seeds an EDITABLE draft here, but StartDineIn already
		// opens the Session's initial editable draft and the
		// order_draft_editable_per_session_unique constraint rejects a second
		// one; the initial draft is the blocking draft this subtest needs.
		session := env.StartDineIn(t, env.TableID)

		_, err := env.TryStartNewDraft(t, session.ID)
		require.ErrorIs(t, err, sales.ErrNewOrderDraftNotAvailable)
	})
}
