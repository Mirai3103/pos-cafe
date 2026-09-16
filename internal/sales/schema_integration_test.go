//go:build integration

package sales_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
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

func TestCommitSchema(t *testing.T) {
	db, _ := openSalesTestDB(t)
	ctx := context.Background()

	t.Run("order_drafts carries check_target defaulting to CURRENT_UNPAID", func(t *testing.T) {
		var def string
		err := db.QueryRowContext(ctx, `
			SELECT column_default FROM information_schema.columns
			WHERE table_name = 'order_drafts' AND column_name = 'check_target'`).Scan(&def)
		require.NoError(t, err)
		require.Contains(t, def, "CURRENT_UNPAID")
	})

	t.Run("checks state domain admits all three canonical values", func(t *testing.T) {
		var clause string
		err := db.QueryRowContext(ctx, `
			SELECT pg_get_constraintdef(oid) FROM pg_constraint
			WHERE conname = 'check_state_valid'`).Scan(&clause)
		require.NoError(t, err)
		require.Contains(t, clause, "OPEN")
		require.Contains(t, clause, "SETTLED")
		require.Contains(t, clause, "MERGED")
	})

	t.Run("committed_items enforces total equals quantity times unit price", func(t *testing.T) {
		var clause string
		err := db.QueryRowContext(ctx, `
			SELECT pg_get_constraintdef(oid) FROM pg_constraint
			WHERE conname = 'committed_item_total_vnd_valid'`).Scan(&clause)
		require.NoError(t, err)
		require.Contains(t, clause, "quantity")
		require.Contains(t, clause, "unit_price_vnd")
	})

	t.Run("charge_allocations is unique per committed item and check", func(t *testing.T) {
		var n int
		err := db.QueryRowContext(ctx, `
			SELECT count(*) FROM pg_indexes
			WHERE indexname = 'charge_allocation_item_check_unique'`).Scan(&n)
		require.NoError(t, err)
		require.Equal(t, 1, n)
	})
}

// TestPaymentSchema proves Phase 5C's settlement columns, constraint, and
// payment table exist exactly as migration 000010 declares them.
func TestPaymentSchema(t *testing.T) {
	db, _ := openSalesTestDB(t)
	ctx := context.Background()

	t.Run("checks carries all five settlement columns", func(t *testing.T) {
		var n int
		err := db.QueryRowContext(ctx, `
			SELECT count(*) FROM information_schema.columns
			WHERE table_name = 'checks'
			  AND column_name IN ('merged_into_check_id', 'settled_at',
			                      'settled_by_staff_identity_id',
			                      'settled_during_sales_shift_id',
			                      'settled_staff_access_session_id')`).Scan(&n)
		require.NoError(t, err)
		require.Equal(t, 5, n)
	})

	t.Run("settlement evidence constraint covers all three states", func(t *testing.T) {
		var clause string
		err := db.QueryRowContext(ctx, `
			SELECT pg_get_constraintdef(oid) FROM pg_constraint
			WHERE conname = 'check_settlement_evidence_valid'`).Scan(&clause)
		require.NoError(t, err)
		require.Contains(t, clause, "OPEN")
		require.Contains(t, clause, "SETTLED")
		require.Contains(t, clause, "MERGED")
	})

	t.Run("payments enforces the cash and manual QR fact sets", func(t *testing.T) {
		var clause string
		err := db.QueryRowContext(ctx, `
			SELECT pg_get_constraintdef(oid) FROM pg_constraint
			WHERE conname = 'payment_method_facts_valid'`).Scan(&clause)
		require.NoError(t, err)
		require.Contains(t, clause, "cash_tendered_vnd")
		require.Contains(t, clause, "transaction_reference")
	})

	t.Run("payments carries both indexes", func(t *testing.T) {
		var n int
		err := db.QueryRowContext(ctx, `
			SELECT count(*) FROM pg_indexes
			WHERE tablename = 'payments'
			  AND indexname IN ('payment_check_index', 'payment_cash_shift_index')`).Scan(&n)
		require.NoError(t, err)
		require.Equal(t, 2, n)
	})
}

// TestSettlementEvidenceConstraintRejectsPartialEvidence proves the database,
// not only Go, rejects a Check whose state claims settlement evidence the row
// does not carry.
func TestSettlementEvidenceConstraintRejectsPartialEvidence(t *testing.T) {
	env := newSalesEnv(t)
	ctx := context.Background()

	session := env.StartTakeaway(t)

	var checkID uuid.UUID
	require.NoError(t, env.DB.QueryRowContext(ctx,
		`INSERT INTO checks (service_session_id, charge_vnd) VALUES ($1, 1000) RETURNING id`,
		session.ID).Scan(&checkID))

	_, err := env.DB.ExecContext(ctx,
		`UPDATE checks SET state = 'SETTLED', settled_at = now() WHERE id = $1`, checkID)
	require.Error(t, err, "SETTLED without the other three evidence columns must be rejected")
	require.Contains(t, err.Error(), "check_settlement_evidence_valid")

	_, err = env.DB.ExecContext(ctx,
		`UPDATE checks SET state = 'MERGED', merged_into_check_id = $1 WHERE id = $1`, checkID)
	require.Error(t, err, "MERGED with a non-zero charge must be rejected")
}

func TestSubmissionSchema(t *testing.T) {
	db, _ := openSalesTestDB(t)
	ctx := context.Background()

	t.Run("all five tables exist", func(t *testing.T) {
		var n int
		err := db.QueryRowContext(ctx, `
			SELECT count(*) FROM information_schema.tables
			WHERE table_name IN ('orders', 'order_items', 'preparation_units',
			                     'preparation_unit_transitions', 'completed_sales')`).Scan(&n)
		require.NoError(t, err)
		require.Equal(t, 5, n)
	})

	t.Run("order_items carries no commercial snapshot", func(t *testing.T) {
		var n int
		err := db.QueryRowContext(ctx, `
			SELECT count(*) FROM information_schema.columns
			WHERE table_name = 'order_items'`).Scan(&n)
		require.NoError(t, err)
		require.Equal(t, 3, n, "ADR-025: id, order_id, committed_item_id only")
	})

	t.Run("one Order per Order Draft is unrepresentable", func(t *testing.T) {
		var n int
		err := db.QueryRowContext(ctx, `
			SELECT count(*) FROM pg_indexes
			WHERE indexname IN ('order_draft_unique', 'order_item_committed_item_unique',
			                    'preparation_unit_item_number_unique',
			                    'completed_sale_service_session_unique')`).Scan(&n)
		require.NoError(t, err)
		require.Equal(t, 4, n)
	})

	t.Run("preparation unit state declares all six canonical values", func(t *testing.T) {
		var clause string
		err := db.QueryRowContext(ctx, `
			SELECT pg_get_constraintdef(oid) FROM pg_constraint
			WHERE conname = 'preparation_unit_state_valid'`).Scan(&clause)
		require.NoError(t, err)
		for _, state := range []string{"QUEUED", "IN_PREPARATION", "READY", "FULFILLED", "CANCELLED", "WASTED"} {
			require.Contains(t, clause, state)
		}
	})

	t.Run("the transition graph is enforced in the database", func(t *testing.T) {
		var clause string
		err := db.QueryRowContext(ctx, `
			SELECT pg_get_constraintdef(oid) FROM pg_constraint
			WHERE conname = 'preparation_unit_transition_states_valid'`).Scan(&clause)
		require.NoError(t, err)
		require.Contains(t, clause, "QUEUED")
		require.Contains(t, clause, "IN_PREPARATION")
		require.Contains(t, clause, "READY")
		require.Contains(t, clause, "FULFILLED")
		require.NotContains(t, clause, "CANCELLED", "Phase 6 adds the Cancellation pairs")
	})
}
