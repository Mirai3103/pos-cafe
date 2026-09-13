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

// draftWithOneItem opens a Session and adds one item, returning both ids.
func draftWithOneItem(t *testing.T, runner *sales.Runner, actor sales.Actor,
	menuItemID uuid.UUID,
) (sessionID uuid.UUID, draftItemID uuid.UUID) {
	t.Helper()
	_, session, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)

	resp, err := addItem(t, runner, actor, sales.AddDraftItemCommand{
		ServiceSessionID: session.ID, MenuItemID: menuItemID})
	require.NoError(t, err)
	require.Len(t, resp.Draft.Items, 1)
	return session.ID, resp.Draft.Items[0].ID
}

func setQuantity(t *testing.T, runner *sales.Runner, actor sales.Actor,
	sessionID, itemID uuid.UUID, quantity int32,
) (sales.ServiceSessionResponse, error) {
	t.Helper()
	_, resp, err := sales.NewSetDraftItemQuantityHandler(runner).Handle(
		context.Background(), actor, sales.SetDraftItemQuantityCommand{
			RequestID:        uuid.New(),
			ServiceSessionID: sessionID,
			DraftItemID:      itemID,
			Quantity:         &quantity,
		})
	return resp, err
}

func TestSetDraftItemQuantity(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	menuItemID := seedMenuItem(t, db, "Cà phê sữa", 25000)
	sessionID, itemID := draftWithOneItem(t, runner, actor, menuItemID)

	resp, err := setQuantity(t, runner, actor, sessionID, itemID, 7)
	require.NoError(t, err)
	require.Len(t, resp.Draft.Items, 1)
	assert.Equal(t, int32(7), resp.Draft.Items[0].Quantity)

	assertAuditEvent(t, db, sales.EventDraftItemQuantitySet, 1)
}

func TestSetDraftItemQuantityBounds(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	menuItemID := seedMenuItem(t, db, "Cà phê sữa", 25000)
	sessionID, itemID := draftWithOneItem(t, runner, actor, menuItemID)

	// Zero is rejected rather than treated as removal: removal is its own
	// command with its own audit event.
	_, err := setQuantity(t, runner, actor, sessionID, itemID, 0)
	require.ErrorIs(t, err, sales.ErrInvalidQuantity)

	_, err = setQuantity(t, runner, actor, sessionID, itemID, 10000)
	require.ErrorIs(t, err, sales.ErrInvalidQuantity)

	_, err = setQuantity(t, runner, actor, sessionID, itemID, 9999)
	require.NoError(t, err)
}

// An item id belonging to another Session's draft is not reachable.
func TestSetDraftItemQuantityRejectsForeignItem(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	menuItemID := seedMenuItem(t, db, "Cà phê sữa", 25000)

	sessionA, _ := draftWithOneItem(t, runner, actor, menuItemID)
	_, foreignItemID := draftWithOneItem(t, runner, actor, menuItemID)

	_, err := setQuantity(t, runner, actor, sessionA, foreignItemID, 3)
	require.ErrorIs(t, err, sales.ErrDraftItemNotFound)
}

func TestRemoveDraftItem(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	menuItemID := seedMenuItem(t, db, "Cà phê sữa", 25000)
	sessionID, itemID := draftWithOneItem(t, runner, actor, menuItemID)

	_, resp, err := sales.NewRemoveDraftItemHandler(runner).Handle(
		context.Background(), actor, sales.RemoveDraftItemCommand{
			RequestID:        uuid.New(),
			ServiceSessionID: sessionID,
			DraftItemID:      itemID,
		})
	require.NoError(t, err)
	assert.Empty(t, resp.Draft.Items)

	assertAuditEvent(t, db, sales.EventDraftItemRemoved, 1)

	// The cascade cleared the option rows too.
	var options int
	require.NoError(t, db.QueryRow(
		`SELECT count(*) FROM order_draft_item_modifier_options`).Scan(&options))
	assert.Equal(t, 0, options)
}
