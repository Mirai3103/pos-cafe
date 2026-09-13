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

func TestGetServiceSessionReturnsSessionWithEmptyDraft(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	fx := seedSalesFixture(t, db, q)

	got, err := sales.NewGetServiceSessionHandler(runner).
		Handle(context.Background(), actor, fx.ServiceSessionID)
	require.NoError(t, err)

	assert.Equal(t, fx.ServiceSessionID, got.ID)
	assert.Equal(t, "S00001", got.ServiceNumber)
	assert.Equal(t, sales.ModeTakeaway, got.ServiceMode)
	assert.Equal(t, sales.StateActive, got.State)
	assert.Equal(t, fx.SalesShiftID, got.SalesShiftID)
	assert.Nil(t, got.CustomerIdentityID)
	assert.Empty(t, got.Tables)
	require.NotNil(t, got.Draft)
	assert.Equal(t, sales.DraftStateEditable, got.Draft.State)
	assert.Empty(t, got.Draft.Items)
	assert.Empty(t, got.Checks)
	assert.Empty(t, got.Orders)
	assert.Empty(t, got.PreparationUnits)
}

func TestGetServiceSessionNotFound(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})

	_, err := sales.NewGetServiceSessionHandler(runner).
		Handle(context.Background(), actor, uuid.New())
	require.ErrorIs(t, err, sales.ErrServiceSessionNotFound)
}

// A BARISTA holds no sales.operate and is denied on the read path, with the
// denial audited.
func TestGetServiceSessionDeniesBarista(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"BARISTA"})
	fx := seedSalesFixture(t, db, q)

	_, err := sales.NewGetServiceSessionHandler(runner).
		Handle(context.Background(), actor, fx.ServiceSessionID)
	require.ErrorIs(t, err, sales.ErrForbidden)

	assertAuditEvent(t, db, sales.EventAuthorizationDenied, 1)
}

// The projection reads a draft item's price and availability live from
// Catalog, so a Size price overrides the Item price.
func TestDraftItemProjectionUsesSizePriceWhenSized(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)
	ctx := context.Background()

	actor := seedActor(t, q, []string{"CASHIER"})
	fx := seedSalesFixture(t, db, q)
	sizeID := seedSize(t, db, fx.MenuItemID, "Lớn", 32000)

	_, err := db.ExecContext(ctx, `
		INSERT INTO order_draft_items (order_draft_id, menu_item_id, size_id, modifier_key)
		VALUES ($1, $2, $3, '')`, fx.DraftID, fx.MenuItemID, sizeID)
	require.NoError(t, err)

	got, err := sales.NewGetServiceSessionHandler(runner).
		Handle(ctx, actor, fx.ServiceSessionID)
	require.NoError(t, err)

	require.Len(t, got.Draft.Items, 1)
	item := got.Draft.Items[0]
	require.NotNil(t, item.PriceVND)
	assert.Equal(t, int64(32000), *item.PriceVND, "the Size price must override the Item price")
	require.NotNil(t, item.SizeName)
	assert.Equal(t, "Lớn", *item.SizeName)
	assert.True(t, item.Available)
}

// An item that became unavailable after being added stays in the draft and is
// projected as unavailable. The draft is a live proposal, not a snapshot.
func TestDraftItemProjectionReportsLiveAvailability(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)
	ctx := context.Background()

	actor := seedActor(t, q, []string{"CASHIER"})
	fx := seedSalesFixture(t, db, q)

	_, err := db.ExecContext(ctx, `
		INSERT INTO order_draft_items (order_draft_id, menu_item_id, modifier_key)
		VALUES ($1, $2, '')`, fx.DraftID, fx.MenuItemID)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `UPDATE menu_items SET available = false WHERE id = $1`,
		fx.MenuItemID)
	require.NoError(t, err)

	got, err := sales.NewGetServiceSessionHandler(runner).
		Handle(ctx, actor, fx.ServiceSessionID)
	require.NoError(t, err)

	require.Len(t, got.Draft.Items, 1)
	assert.False(t, got.Draft.Items[0].Available)
}

// Task 6 deferred this test until Commit existed: a Check's stored charge_vnd
// is a denormalization of its allocations, and a read that disagrees must fail
// rather than serve a wrong total.
func TestProjectionRejectsCorruptedCheckCharge(t *testing.T) {
	env := newSalesEnv(t)
	session := env.commitOneItemSession(t) // helper added in Task 8

	_, err := env.DB.Exec(
		`UPDATE checks SET charge_vnd = charge_vnd + 1 WHERE service_session_id = $1`,
		session.ID)
	require.NoError(t, err)

	_, err = env.GetSession(t, session.ID)
	require.ErrorIs(t, err, sales.ErrChargeInvariantViolated)
}
