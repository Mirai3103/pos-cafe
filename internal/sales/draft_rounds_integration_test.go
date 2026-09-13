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
