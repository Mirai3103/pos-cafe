//go:build integration

package sales_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func addItem(t *testing.T, runner *sales.Runner, actor sales.Actor,
	cmd sales.AddDraftItemCommand,
) (sales.ServiceSessionResponse, error) {
	t.Helper()
	if cmd.RequestID == uuid.Nil {
		cmd.RequestID = uuid.New()
	}
	_, resp, err := sales.NewAddDraftItemHandler(runner).Handle(context.Background(), actor, cmd)
	return resp, err
}

// Adding the same composition twice yields one row with quantity 2. The
// command takes no quantity parameter; it always adds one unit.
func TestAddSameCompositionIncrementsQuantity(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	_, session, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)
	itemID := seedMenuItem(t, db, "Cà phê sữa", 25000)

	cmd := sales.AddDraftItemCommand{ServiceSessionID: session.ID, MenuItemID: itemID}
	_, err = addItem(t, runner, actor, cmd)
	require.NoError(t, err)

	resp, err := addItem(t, runner, actor, cmd)
	require.NoError(t, err)

	require.Len(t, resp.Draft.Items, 1, "the same composition must not create a second line")
	assert.Equal(t, int32(2), resp.Draft.Items[0].Quantity)
	assertAuditEvent(t, db, sales.EventDraftItemAdded, 2)
}

// Two compositions that differ in any component are two lines.
func TestAddDifferentCompositionsAreSeparateLines(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	_, session, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)
	itemID := seedMenuItem(t, db, "Cà phê sữa", 25000)

	_, err = addItem(t, runner, actor, sales.AddDraftItemCommand{
		ServiceSessionID: session.ID, MenuItemID: itemID})
	require.NoError(t, err)

	note := "ít đường"
	resp, err := addItem(t, runner, actor, sales.AddDraftItemCommand{
		ServiceSessionID: session.ID, MenuItemID: itemID, PreparationNote: &note})
	require.NoError(t, err)

	assert.Len(t, resp.Draft.Items, 2, "a different note is a different composition")
}

// Absent modifier_option_ids applies the menu's defaults; an explicitly empty
// list applies none. The two are different requests, not an idempotency
// conflict.
func TestAddAppliesDefaultsOnlyWhenOptionsAbsent(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	_, session, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)

	itemID, defaultOptionID := seedItemWithDefaultOption(t, db)

	withDefaults, err := addItem(t, runner, actor, sales.AddDraftItemCommand{
		ServiceSessionID: session.ID, MenuItemID: itemID})
	require.NoError(t, err)
	require.Len(t, withDefaults.Draft.Items, 1)
	require.Len(t, withDefaults.Draft.Items[0].SelectedModifierOptions, 1)
	assert.Equal(t, defaultOptionID, withDefaults.Draft.Items[0].SelectedModifierOptions[0].ID)

	empty := []uuid.UUID{}
	withNone, err := addItem(t, runner, actor, sales.AddDraftItemCommand{
		ServiceSessionID:  session.ID,
		MenuItemID:        itemID,
		ModifierOptionIDs: &empty,
	})
	require.NoError(t, err)
	assert.Len(t, withNone.Draft.Items, 2,
		"an explicit empty selection is a different composition from the defaults")
}

// A draft tolerates incompleteness: an item whose Menu Item requires a Size is
// accepted with no Size. Commit, in 5B, is where completeness is checked.
func TestAddAcceptsIncompleteConfiguration(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	_, session, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)

	// A sized item has a NULL price_vnd of its own.
	itemID := seedSizedMenuItem(t, db, "Trà sữa")
	seedSize(t, db, itemID, "Lớn", 40000)

	resp, err := addItem(t, runner, actor, sales.AddDraftItemCommand{
		ServiceSessionID: session.ID, MenuItemID: itemID})
	require.NoError(t, err, "a draft holds an item with no Size chosen yet")
	require.Len(t, resp.Draft.Items, 1)
	assert.Nil(t, resp.Draft.Items[0].PriceVND, "an unsized sized-item has no price yet")
}

func TestAddRejectsUnselectableCatalogEntities(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	_, session, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)

	t.Run("unknown menu item", func(t *testing.T) {
		_, err := addItem(t, runner, actor, sales.AddDraftItemCommand{
			ServiceSessionID: session.ID, MenuItemID: uuid.New()})
		require.ErrorIs(t, err, sales.ErrMenuItemNotFound)
	})

	t.Run("unavailable menu item", func(t *testing.T) {
		itemID := seedMenuItem(t, db, "Hết hàng", 20000)
		_, err := db.Exec(`UPDATE menu_items SET available = false WHERE id = $1`, itemID)
		require.NoError(t, err)

		_, err = addItem(t, runner, actor, sales.AddDraftItemCommand{
			ServiceSessionID: session.ID, MenuItemID: itemID})
		require.ErrorIs(t, err, sales.ErrMenuItemUnavailable)
	})

	t.Run("retired menu item", func(t *testing.T) {
		itemID := seedMenuItem(t, db, "Ngừng bán", 20000)
		_, err := db.Exec(`
			UPDATE menu_items SET retired_at = now(), retirement_reason = 'DISCONTINUED'
			WHERE id = $1`, itemID)
		require.NoError(t, err)

		_, err = addItem(t, runner, actor, sales.AddDraftItemCommand{
			ServiceSessionID: session.ID, MenuItemID: itemID})
		require.ErrorIs(t, err, sales.ErrMenuItemRetired)
	})

	t.Run("size belonging to another item", func(t *testing.T) {
		itemA := seedMenuItem(t, db, "Món A", 20000)
		itemB := seedSizedMenuItem(t, db, "Món B")
		foreignSize := seedSize(t, db, itemB, "Lớn", 30000)

		_, err := addItem(t, runner, actor, sales.AddDraftItemCommand{
			ServiceSessionID: session.ID, MenuItemID: itemA, SizeID: &foreignSize})
		require.ErrorIs(t, err, sales.ErrSizeNotFound)
	})
}

// An option whose Group the Item excludes is not selectable here, and reports
// not-found rather than a distinct code.
func TestAddRejectsOptionFromExcludedGroup(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	_, session, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)

	excludingItem, inheritingItem, optionID := seedExclusionFixture(t, db)

	_, err = addItem(t, runner, actor, sales.AddDraftItemCommand{
		ServiceSessionID:  session.ID,
		MenuItemID:        excludingItem,
		ModifierOptionIDs: &[]uuid.UUID{optionID},
	})
	require.ErrorIs(t, err, sales.ErrModifierOptionNotFound)

	_, err = addItem(t, runner, actor, sales.AddDraftItemCommand{
		ServiceSessionID:  session.ID,
		MenuItemID:        inheritingItem,
		ModifierOptionIDs: &[]uuid.UUID{optionID},
	})
	require.NoError(t, err, "the same option is selectable for an item that inherits the group")
}

func TestAddRejectsOverlongPreparationNote(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	_, session, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)
	itemID := seedMenuItem(t, db, "Cà phê sữa", 25000)

	long := strings.Repeat("á", 201)
	_, err = addItem(t, runner, actor, sales.AddDraftItemCommand{
		ServiceSessionID: session.ID, MenuItemID: itemID, PreparationNote: &long})
	require.ErrorIs(t, err, sales.ErrInvalidPreparationNote)
}

// Draft edits stop working once the Session's Shift closes.
func TestAddRejectedAfterShiftCloses(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	shiftID := seedOpenShift(t, q, actor.StaffID)
	_, session, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)
	itemID := seedMenuItem(t, db, "Cà phê sữa", 25000)

	_, err = db.Exec(`UPDATE sales_shifts SET state = 'CLOSED' WHERE id = $1`, shiftID)
	require.NoError(t, err)

	_, err = addItem(t, runner, actor, sales.AddDraftItemCommand{
		ServiceSessionID: session.ID, MenuItemID: itemID})
	require.ErrorIs(t, err, sales.ErrEditableDraftNotFound)
}

// modifier_key is derived, never authoritative. It must always equal the
// sorted join of the row's actual option rows.
func TestModifierKeyMatchesStoredOptions(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	_, session, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)

	itemID, optionA, optionB := seedItemWithTwoOptions(t, db)

	_, err = addItem(t, runner, actor, sales.AddDraftItemCommand{
		ServiceSessionID:  session.ID,
		MenuItemID:        itemID,
		ModifierOptionIDs: &[]uuid.UUID{optionB, optionA},
	})
	require.NoError(t, err)

	assertModifierKeyIntegrity(t, db)
}

// A replay returns the projection as it stood when the mutation committed, not
// a fresh read. Idempotency exists so a retried request reproduces its original
// outcome; a client wanting current state issues the read.
func TestReplayReturnsTheCommittedSnapshotNotFreshState(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	_, session, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)
	menuItemID := seedMenuItem(t, db, "Cà phê sữa", 25000)

	cmd := sales.AddDraftItemCommand{
		RequestID:        uuid.New(),
		ServiceSessionID: session.ID,
		MenuItemID:       menuItemID,
	}
	first, err := addItem(t, runner, actor, cmd)
	require.NoError(t, err)
	require.True(t, first.Draft.Items[0].Available)

	_, err = db.Exec(`UPDATE menu_items SET available = false WHERE id = $1`, menuItemID)
	require.NoError(t, err)

	replay, err := addItem(t, runner, actor, cmd)
	require.NoError(t, err)
	assert.True(t, replay.Draft.Items[0].Available,
		"a replay reports the availability captured at commit time")

	fresh, err := sales.NewGetServiceSessionHandler(runner).
		Handle(context.Background(), actor, session.ID)
	require.NoError(t, err)
	assert.False(t, fresh.Draft.Items[0].Available,
		"a fresh read reports current availability")
}
