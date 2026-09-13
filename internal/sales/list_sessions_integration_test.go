//go:build integration

package sales_test

import (
	"context"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListActiveSessionsOrdersByCreation(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)

	_, first, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)
	_, second, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)

	got, err := sales.NewListActiveSessionsHandler(runner).Handle(context.Background(), actor)
	require.NoError(t, err)

	require.Len(t, got, 2)
	assert.Equal(t, first.ID, got[0].ID)
	assert.Equal(t, second.ID, got[1].ID)
}

func TestListActiveSessionsExcludesClosed(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)

	_, session, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)

	// 5D owns closure; the test drives the state directly.
	_, err = db.Exec(`UPDATE service_sessions SET state = 'CLOSED' WHERE id = $1`, session.ID)
	require.NoError(t, err)

	got, err := sales.NewListActiveSessionsHandler(runner).Handle(context.Background(), actor)
	require.NoError(t, err)
	assert.Empty(t, got)
}

// Each listed Session carries its full projection, including its draft, so the
// cashier's open-tabs screen needs one request rather than one per Session.
func TestListActiveSessionsIncludesDrafts(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	menuItemID := seedMenuItem(t, db, "Cà phê sữa", 25000)

	_, session, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)
	_, err = addItem(t, runner, actor, sales.AddDraftItemCommand{
		ServiceSessionID: session.ID, MenuItemID: menuItemID})
	require.NoError(t, err)

	got, err := sales.NewListActiveSessionsHandler(runner).Handle(context.Background(), actor)
	require.NoError(t, err)

	require.Len(t, got, 1)
	require.NotNil(t, got[0].Draft)
	assert.Len(t, got[0].Draft.Items, 1)
}

func TestListActiveSessionsDeniesBarista(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"BARISTA"})

	_, err := sales.NewListActiveSessionsHandler(runner).Handle(context.Background(), actor)
	require.ErrorIs(t, err, sales.ErrForbidden)
}
