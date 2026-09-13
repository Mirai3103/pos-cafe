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

func setSize(t *testing.T, runner *sales.Runner, actor sales.Actor,
	sessionID, itemID uuid.UUID, sizeID *uuid.UUID,
) (sales.ServiceSessionResponse, error) {
	t.Helper()
	_, resp, err := sales.NewSetDraftItemSizeHandler(runner).Handle(
		context.Background(), actor, sales.SetDraftItemSizeCommand{
			RequestID: uuid.New(), ServiceSessionID: sessionID,
			DraftItemID: itemID, SizeID: sizeID,
		})
	return resp, err
}

func setNote(t *testing.T, runner *sales.Runner, actor sales.Actor,
	sessionID, itemID uuid.UUID, note *string,
) (sales.ServiceSessionResponse, error) {
	t.Helper()
	_, resp, err := sales.NewSetDraftItemNoteHandler(runner).Handle(
		context.Background(), actor, sales.SetDraftItemNoteCommand{
			RequestID: uuid.New(), ServiceSessionID: sessionID,
			DraftItemID: itemID, PreparationNote: note,
		})
	return resp, err
}

func setModifiers(t *testing.T, runner *sales.Runner, actor sales.Actor,
	sessionID, itemID uuid.UUID, optionIDs []uuid.UUID,
) (sales.ServiceSessionResponse, error) {
	t.Helper()
	if optionIDs == nil {
		optionIDs = []uuid.UUID{}
	}
	_, resp, err := sales.NewSetDraftItemModifiersHandler(runner).Handle(
		context.Background(), actor, sales.SetDraftItemModifiersCommand{
			RequestID: uuid.New(), ServiceSessionID: sessionID,
			DraftItemID: itemID, ModifierOptionIDs: optionIDs,
		})
	return resp, err
}

// Editing the Size into an existing composition merges the two rows.
func TestSetSizeMergesIntoExistingComposition(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	_, session, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)

	itemID := seedSizedMenuItem(t, db, "Trà sữa")
	small := seedSize(t, db, itemID, "Nhỏ", 30000)
	large := seedSize(t, db, itemID, "Lớn", 40000)

	// Two lines: one Large, one Small.
	_, err = addItem(t, runner, actor, sales.AddDraftItemCommand{
		ServiceSessionID: session.ID, MenuItemID: itemID, SizeID: &large})
	require.NoError(t, err)
	resp, err := addItem(t, runner, actor, sales.AddDraftItemCommand{
		ServiceSessionID: session.ID, MenuItemID: itemID, SizeID: &small})
	require.NoError(t, err)
	require.Len(t, resp.Draft.Items, 2)

	var smallItemID, largeItemID uuid.UUID
	for _, it := range resp.Draft.Items {
		if *it.SizeID == small {
			smallItemID = it.ID
		} else {
			largeItemID = it.ID
		}
	}
	require.NotEqual(t, uuid.Nil, smallItemID)

	// Change the Small line to Large: it now matches the Large line.
	merged, err := setSize(t, runner, actor, session.ID, smallItemID, &large)
	require.NoError(t, err)

	require.Len(t, merged.Draft.Items, 1, "the two rows must merge into one")
	assert.Equal(t, largeItemID, merged.Draft.Items[0].ID,
		"the pre-existing row survives and the edited row is deleted")
	assert.Equal(t, int32(2), merged.Draft.Items[0].Quantity, "quantities are summed")

	assertAuditEvent(t, db, sales.EventDraftItemsMerged, 1)
	assertModifierKeyIntegrity(t, db)
}

// The same merge happens through the Preparation Note path.
func TestSetNoteMergesIntoExistingComposition(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	_, session, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)
	menuItemID := seedMenuItem(t, db, "Cà phê sữa", 25000)

	note := "ít đá"
	_, err = addItem(t, runner, actor, sales.AddDraftItemCommand{
		ServiceSessionID: session.ID, MenuItemID: menuItemID, PreparationNote: &note})
	require.NoError(t, err)
	resp, err := addItem(t, runner, actor, sales.AddDraftItemCommand{
		ServiceSessionID: session.ID, MenuItemID: menuItemID})
	require.NoError(t, err)
	require.Len(t, resp.Draft.Items, 2)

	var noteless uuid.UUID
	for _, it := range resp.Draft.Items {
		if it.PreparationNote == nil {
			noteless = it.ID
		}
	}

	merged, err := setNote(t, runner, actor, session.ID, noteless, &note)
	require.NoError(t, err)
	require.Len(t, merged.Draft.Items, 1)
	assert.Equal(t, int32(2), merged.Draft.Items[0].Quantity)
	assertAuditEvent(t, db, sales.EventDraftItemsMerged, 1)
}

// And through the Modifier Options path.
func TestSetModifiersMergesIntoExistingComposition(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	_, session, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)

	menuItemID, optionA, optionB := seedItemWithTwoOptions(t, db)

	_, err = addItem(t, runner, actor, sales.AddDraftItemCommand{
		ServiceSessionID: session.ID, MenuItemID: menuItemID,
		ModifierOptionIDs: &[]uuid.UUID{optionA}})
	require.NoError(t, err)
	resp, err := addItem(t, runner, actor, sales.AddDraftItemCommand{
		ServiceSessionID: session.ID, MenuItemID: menuItemID,
		ModifierOptionIDs: &[]uuid.UUID{optionB}})
	require.NoError(t, err)
	require.Len(t, resp.Draft.Items, 2)

	var withB uuid.UUID
	for _, it := range resp.Draft.Items {
		if len(it.SelectedModifierOptions) == 1 && it.SelectedModifierOptions[0].ID == optionB {
			withB = it.ID
		}
	}
	require.NotEqual(t, uuid.Nil, withB)

	merged, err := setModifiers(t, runner, actor, session.ID, withB, []uuid.UUID{optionA})
	require.NoError(t, err)
	require.Len(t, merged.Draft.Items, 1)
	assert.Equal(t, int32(2), merged.Draft.Items[0].Quantity)
	assertAuditEvent(t, db, sales.EventDraftItemsMerged, 1)
	assertModifierKeyIntegrity(t, db)
}

// A composition edit that collides with nothing is a plain update, with no
// merge event.
func TestCompositionEditWithoutCollisionIsAPlainUpdate(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	_, session, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)
	menuItemID := seedMenuItem(t, db, "Cà phê sữa", 25000)

	resp, err := addItem(t, runner, actor, sales.AddDraftItemCommand{
		ServiceSessionID: session.ID, MenuItemID: menuItemID})
	require.NoError(t, err)
	itemID := resp.Draft.Items[0].ID

	note := "nhiều đá"
	updated, err := setNote(t, runner, actor, session.ID, itemID, &note)
	require.NoError(t, err)

	require.Len(t, updated.Draft.Items, 1)
	assert.Equal(t, itemID, updated.Draft.Items[0].ID, "the row keeps its identity")
	require.NotNil(t, updated.Draft.Items[0].PreparationNote)
	assert.Equal(t, note, *updated.Draft.Items[0].PreparationNote)

	assertAuditEvent(t, db, sales.EventDraftItemsMerged, 0)
	assertAuditEvent(t, db, sales.EventDraftItemNoteSet, 1)
}

// A merge whose summed quantity would exceed the bound is rejected outright
// rather than silently clamped.
func TestMergeRejectsQuantityOverflow(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	_, session, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)
	menuItemID := seedMenuItem(t, db, "Cà phê sữa", 25000)

	note := "ít đá"
	first, err := addItem(t, runner, actor, sales.AddDraftItemCommand{
		ServiceSessionID: session.ID, MenuItemID: menuItemID, PreparationNote: &note})
	require.NoError(t, err)
	second, err := addItem(t, runner, actor, sales.AddDraftItemCommand{
		ServiceSessionID: session.ID, MenuItemID: menuItemID})
	require.NoError(t, err)
	require.Len(t, second.Draft.Items, 2)

	var noted, noteless uuid.UUID
	for _, it := range second.Draft.Items {
		if it.PreparationNote == nil {
			noteless = it.ID
		} else {
			noted = it.ID
		}
	}
	_ = first

	_, err = setQuantity(t, runner, actor, session.ID, noted, 9000)
	require.NoError(t, err)
	_, err = setQuantity(t, runner, actor, session.ID, noteless, 1500)
	require.NoError(t, err)

	_, err = setNote(t, runner, actor, session.ID, noteless, &note)
	require.ErrorIs(t, err, sales.ErrInvalidQuantity)

	// Nothing changed: both rows survive with their quantities.
	resp, err := sales.NewGetServiceSessionHandler(runner).
		Handle(context.Background(), actor, session.ID)
	require.NoError(t, err)
	assert.Len(t, resp.Draft.Items, 2)
}

// Clearing the Size is permitted; the draft tolerates incompleteness.
func TestSetSizeAcceptsNullToClear(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	_, session, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)

	menuItemID := seedSizedMenuItem(t, db, "Trà sữa")
	large := seedSize(t, db, menuItemID, "Lớn", 40000)

	resp, err := addItem(t, runner, actor, sales.AddDraftItemCommand{
		ServiceSessionID: session.ID, MenuItemID: menuItemID, SizeID: &large})
	require.NoError(t, err)

	cleared, err := setSize(t, runner, actor, session.ID, resp.Draft.Items[0].ID, nil)
	require.NoError(t, err)
	assert.Nil(t, cleared.Draft.Items[0].SizeID)
	assert.Nil(t, cleared.Draft.Items[0].PriceVND)
}

// Unlike add, the modifiers command takes an empty list literally and applies
// no defaults.
func TestSetModifiersAppliesNoDefaults(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	_, session, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)

	menuItemID, defaultOptionID := seedItemWithDefaultOption(t, db)

	resp, err := addItem(t, runner, actor, sales.AddDraftItemCommand{
		ServiceSessionID: session.ID, MenuItemID: menuItemID})
	require.NoError(t, err)
	require.Len(t, resp.Draft.Items[0].SelectedModifierOptions, 1)
	_ = defaultOptionID

	cleared, err := setModifiers(t, runner, actor, session.ID, resp.Draft.Items[0].ID, nil)
	require.NoError(t, err)
	assert.Empty(t, cleared.Draft.Items[0].SelectedModifierOptions,
		"an empty list must be taken literally, not replaced by defaults")
	assertModifierKeyIntegrity(t, db)
}
