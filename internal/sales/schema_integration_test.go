//go:build integration

package sales_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestServiceSessionStateDomain proves Phase 3's guessed state domain is gone.
func TestServiceSessionStateDomain(t *testing.T) {
	db, _ := openSalesTestDB(t)
	ctx := context.Background()

	var clause string
	err := db.QueryRowContext(ctx, `
		SELECT pg_get_constraintdef(oid)
		FROM pg_constraint
		WHERE conname = 'service_session_state_valid'`).Scan(&clause)
	require.NoError(t, err)

	assert.Contains(t, clause, "ACTIVE")
	assert.Contains(t, clause, "CLOSED")
	assert.NotContains(t, clause, "COMPLETED")
	assert.NotContains(t, clause, "CANCELLED")
}

// TestServiceNumberIsShiftScoped proves the global unique index is gone and
// the Shift-scoped one replaced it (ADR-011).
func TestServiceNumberIsShiftScoped(t *testing.T) {
	db, _ := openSalesTestDB(t)
	ctx := context.Background()

	var globalCount int
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT count(*) FROM pg_indexes
		WHERE indexname = 'service_session_service_number_unique'`).Scan(&globalCount))
	assert.Equal(t, 0, globalCount, "the global Service Number index must be dropped")

	var def string
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT indexdef FROM pg_indexes
		WHERE indexname = 'service_session_number_per_shift_unique'`).Scan(&def))
	assert.Contains(t, def, "sales_shift_id")
	assert.Contains(t, def, "service_number")
}

// TestCompositionIndexTreatsNullsAsEqual is the whole reason size_key and
// note_key exist: a plain unique index would allow this duplicate.
func TestCompositionIndexTreatsNullsAsEqual(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	ctx := context.Background()

	fx := seedSalesFixture(t, db, q)
	draftID := fx.DraftID

	_, err := db.ExecContext(ctx, `
		INSERT INTO order_draft_items (order_draft_id, menu_item_id, size_id, preparation_note, modifier_key)
		VALUES ($1, $2, NULL, NULL, '')`, draftID, fx.MenuItemID)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `
		INSERT INTO order_draft_items (order_draft_id, menu_item_id, size_id, preparation_note, modifier_key)
		VALUES ($1, $2, NULL, NULL, '')`, draftID, fx.MenuItemID)
	require.Error(t, err, "a second NULL-Size, NULL-note row of the same composition must violate the unique index")
}

// TestQuantityBound proves the 9,999 ceiling from the spec is enforced by the
// database, not only by Go.
func TestQuantityBound(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	ctx := context.Background()

	fx := seedSalesFixture(t, db, q)

	_, err := db.ExecContext(ctx, `
		INSERT INTO order_draft_items (order_draft_id, menu_item_id, quantity, modifier_key)
		VALUES ($1, $2, 10000, '')`, fx.DraftID, fx.MenuItemID)
	require.Error(t, err)

	_, err = db.ExecContext(ctx, `
		INSERT INTO order_draft_items (order_draft_id, menu_item_id, quantity, modifier_key)
		VALUES ($1, $2, 0, '')`, fx.DraftID, fx.MenuItemID)
	require.Error(t, err)

	_, err = db.ExecContext(ctx, `
		INSERT INTO order_draft_items (order_draft_id, menu_item_id, quantity, modifier_key)
		VALUES ($1, $2, 9999, '')`, fx.DraftID, fx.MenuItemID)
	require.NoError(t, err)
}

// TestOneEditableDraftPerSession proves the partial unique index.
func TestOneEditableDraftPerSession(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	ctx := context.Background()

	fx := seedSalesFixture(t, db, q)

	_, err := db.ExecContext(ctx, `
		INSERT INTO order_drafts (service_session_id) VALUES ($1)`, fx.ServiceSessionID)
	require.Error(t, err, "a second EDITABLE draft for one Session must be rejected")

	// A COMMITTED draft alongside an EDITABLE one is allowed; the index is partial.
	_, err = db.ExecContext(ctx, `
		INSERT INTO order_drafts (service_session_id, state) VALUES ($1, 'COMMITTED')`,
		fx.ServiceSessionID)
	require.NoError(t, err)
}
