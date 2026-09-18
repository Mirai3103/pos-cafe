//go:build integration

package sales_test

import (
	"context"
	"database/sql"
	"strings"
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
		require.Contains(t, clause, "CANCELLED", "Phase 6C adds the QUEUED -> CANCELLED pair")
	})
}

// --- Phase 6C: Comp, Void, and Refund persistence ---

// constraintDef reads one constraint's decompiled definition by name and fails
// the test when the constraint does not exist.
func constraintDef(t *testing.T, db *sql.DB, name string) string {
	t.Helper()
	var clause string
	err := db.QueryRow(`
		SELECT pg_get_constraintdef(oid)
		FROM pg_constraint
		WHERE conname = $1`, name).Scan(&clause)
	require.NoError(t, err, "constraint %s must exist", name)
	return clause
}

// indexDef reads one index's decompiled definition by name and fails the test
// when the index does not exist.
func indexDef(t *testing.T, db *sql.DB, name string) string {
	t.Helper()
	var def string
	err := db.QueryRow(`
		SELECT indexdef
		FROM pg_indexes
		WHERE indexname = $1`, name).Scan(&def)
	require.NoError(t, err, "index %s must exist", name)
	return def
}

// correctionFixture resolves the world one Phase 6C insertion needs: a
// submitted dine-in Session with two standard units, their Check and shared
// original Charge Allocation, one Waste and one COMP Charge Adjustment per
// unit, and one Cash Payment.
type correctionFixture struct {
	Env           *salesEnv
	SessionID     uuid.UUID
	CheckID       uuid.UUID
	AllocationID  uuid.UUID
	UnitIDs       []uuid.UUID
	WasteIDs      []uuid.UUID
	AdjustmentIDs []uuid.UUID
	PaymentID     uuid.UUID
}

func newCorrectionFixture(t *testing.T) correctionFixture {
	t.Helper()
	env := newSalesEnv(t)
	committed := env.commitDineInDraftWithQuantity(t, 2)
	submitted := env.Submit(t, committed.ID)
	require.Len(t, submitted.PreparationUnits, 2)

	fixture := correctionFixture{Env: env, SessionID: committed.ID}
	fixture.CheckID = env.soleCheckID(t, committed.ID)
	for _, unit := range submitted.PreparationUnits {
		fixture.UnitIDs = append(fixture.UnitIDs, unit.ID)
		require.NoError(t, env.DB.QueryRow(`
			SELECT ca.id
			FROM order_items oi
			JOIN charge_allocations ca ON ca.committed_item_id = oi.committed_item_id
			WHERE oi.id = $1
			ORDER BY ca.created_at ASC, ca.id ASC
			LIMIT 1`, unit.OrderItemID).Scan(&fixture.AllocationID))

		var wasteID uuid.UUID
		require.NoError(t, env.DB.QueryRow(`
			INSERT INTO preparation_wastes (preparation_unit_id, prior_state, reason,
				actor_staff_identity_id, staff_access_session_id)
			VALUES ($1, 'READY', 'QUALITY_FAILURE', $2, $3)
			RETURNING id`, unit.ID, env.Actor.StaffID, env.Actor.SessionID).Scan(&wasteID))
		fixture.WasteIDs = append(fixture.WasteIDs, wasteID)

		var adjustmentID uuid.UUID
		require.NoError(t, env.DB.QueryRow(`
			INSERT INTO charge_adjustments (kind, scope, preparation_unit_id,
				preparation_waste_id, charge_allocation_id, check_id,
				sales_shift_id, amount_vnd)
			VALUES ('COMP', 'LIVE_CHECK', $1, $2, $3, $4, $5, 25000)
			RETURNING id`,
			unit.ID, wasteID, fixture.AllocationID, fixture.CheckID,
			env.ShiftID).Scan(&adjustmentID))
		fixture.AdjustmentIDs = append(fixture.AdjustmentIDs, adjustmentID)
	}
	fixture.PaymentID = env.insertCashPayment(t, fixture.CheckID, 25000)
	return fixture
}

// insertSalesComp writes one raw Comp row and returns the error untouched, so
// a rejection case proves the database refuses it.
func insertSalesComp(t *testing.T, env *salesEnv, wasteID, adjustmentID uuid.UUID,
	reason string, note any,
) (uuid.UUID, error) {
	t.Helper()
	var id uuid.UUID
	err := env.DB.QueryRow(`
		INSERT INTO sales_comps (preparation_waste_id, charge_adjustment_id, reason,
			note, actor_staff_identity_id, staff_access_session_id,
			approved_by_staff_identity_id, occurred_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now())
		RETURNING id`,
		wasteID, adjustmentID, reason, note, env.Actor.StaffID, env.Actor.SessionID,
		env.Actor.StaffID).Scan(&id)
	return id, err
}

// insertPaymentVoid writes one raw Payment Void row.
func insertPaymentVoid(t *testing.T, env *salesEnv, paymentID uuid.UUID, amountVND int64,
	reason string, note any,
) (uuid.UUID, error) {
	t.Helper()
	var id uuid.UUID
	err := env.DB.QueryRow(`
		INSERT INTO payment_voids (payment_id, sales_shift_id, amount_vnd, reason,
			note, actor_staff_identity_id, staff_access_session_id,
			approved_by_staff_identity_id, occurred_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now())
		RETURNING id`,
		paymentID, env.ShiftID, amountVND, reason, note, env.Actor.StaffID,
		env.Actor.SessionID, env.Actor.StaffID).Scan(&id)
	return id, err
}

// insertRefund writes one raw Refund intent row.
func insertRefund(t *testing.T, env *salesEnv, checkID uuid.UUID, method string,
	amountVND int64, reason string, note any,
) (uuid.UUID, error) {
	t.Helper()
	var id uuid.UUID
	err := env.DB.QueryRow(`
		INSERT INTO refunds (check_id, sales_shift_id, method, amount_vnd, reason,
			note, actor_staff_identity_id, staff_access_session_id,
			approved_by_staff_identity_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now())
		RETURNING id`,
		checkID, env.ShiftID, method, amountVND, reason, note, env.Actor.StaffID,
		env.Actor.SessionID, env.Actor.StaffID).Scan(&id)
	return id, err
}

// insertRefundCompletion writes one raw Refund completion row.
func insertRefundCompletion(t *testing.T, env *salesEnv, refundID uuid.UUID,
	reference any,
) (uuid.UUID, error) {
	t.Helper()
	var id uuid.UUID
	err := env.DB.QueryRow(`
		INSERT INTO refund_completions (refund_id, transaction_reference,
			completed_by_staff_identity_id, staff_access_session_id, completed_at)
		VALUES ($1, $2, $3, $4, now())
		RETURNING id`,
		refundID, reference, env.Actor.StaffID, env.Actor.SessionID).Scan(&id)
	return id, err
}

// TestSalesFinancialCorrectionsSchema proves migration 000014 created the six
// Phase 6C Comp, Void, and Refund fact tables, their foreign keys, named
// constraints, and indexes, and that the database rejects every invalid fact
// shape.
func TestSalesFinancialCorrectionsSchema(t *testing.T) {
	db, _ := openSalesTestDB(t)

	t.Run("all six Correction fact tables exist", func(t *testing.T) {
		var tables int
		require.NoError(t, db.QueryRow(`
			SELECT count(*) FROM information_schema.tables
			WHERE table_schema = 'public'
			  AND table_name IN ('sales_comps', 'payment_voids', 'refunds',
			                     'refund_payment_allocations',
			                     'refund_adjustment_allocations', 'refund_completions')`).
			Scan(&tables))
		require.Equal(t, 6, tables)
	})

	t.Run("every foreign key restricts deletion", func(t *testing.T) {
		foreignKeys := []struct {
			name       string
			references string
		}{
			{"sales_comps_preparation_waste_id_fkey", "REFERENCES preparation_wastes(id) ON DELETE RESTRICT"},
			{"sales_comps_charge_adjustment_id_fkey", "REFERENCES charge_adjustments(id) ON DELETE RESTRICT"},
			{"sales_comps_actor_staff_identity_id_fkey", "REFERENCES staff_identities(id) ON DELETE RESTRICT"},
			{"sales_comps_staff_access_session_id_fkey", "REFERENCES staff_access_sessions(id) ON DELETE RESTRICT"},
			{"sales_comps_approved_by_staff_identity_id_fkey", "REFERENCES staff_identities(id) ON DELETE RESTRICT"},
			{"payment_voids_payment_id_fkey", "REFERENCES payments(id) ON DELETE RESTRICT"},
			{"payment_voids_sales_shift_id_fkey", "REFERENCES sales_shifts(id) ON DELETE RESTRICT"},
			{"payment_voids_actor_staff_identity_id_fkey", "REFERENCES staff_identities(id) ON DELETE RESTRICT"},
			{"payment_voids_staff_access_session_id_fkey", "REFERENCES staff_access_sessions(id) ON DELETE RESTRICT"},
			{"payment_voids_approved_by_staff_identity_id_fkey", "REFERENCES staff_identities(id) ON DELETE RESTRICT"},
			{"refunds_check_id_fkey", "REFERENCES checks(id) ON DELETE RESTRICT"},
			{"refunds_completed_sale_id_fkey", "REFERENCES completed_sales(id) ON DELETE RESTRICT"},
			{"refunds_sales_shift_id_fkey", "REFERENCES sales_shifts(id) ON DELETE RESTRICT"},
			{"refunds_actor_staff_identity_id_fkey", "REFERENCES staff_identities(id) ON DELETE RESTRICT"},
			{"refunds_staff_access_session_id_fkey", "REFERENCES staff_access_sessions(id) ON DELETE RESTRICT"},
			{"refunds_approved_by_staff_identity_id_fkey", "REFERENCES staff_identities(id) ON DELETE RESTRICT"},
			{"refund_payment_allocations_refund_id_fkey", "REFERENCES refunds(id) ON DELETE RESTRICT"},
			{"refund_payment_allocations_payment_id_fkey", "REFERENCES payments(id) ON DELETE RESTRICT"},
			{"refund_adjustment_allocations_refund_id_fkey", "REFERENCES refunds(id) ON DELETE RESTRICT"},
			{"refund_adjustment_allocations_charge_adjustment_id_fkey", "REFERENCES charge_adjustments(id) ON DELETE RESTRICT"},
			{"refund_completions_refund_id_fkey", "REFERENCES refunds(id) ON DELETE RESTRICT"},
			{"refund_completions_completed_by_staff_identity_id_fkey", "REFERENCES staff_identities(id) ON DELETE RESTRICT"},
			{"refund_completions_staff_access_session_id_fkey", "REFERENCES staff_access_sessions(id) ON DELETE RESTRICT"},
		}
		for _, fk := range foreignKeys {
			clause := constraintDef(t, db, fk.name)
			require.Contains(t, clause, fk.references, "constraint %s", fk.name)
		}
	})

	t.Run("the named check and unique constraints exist", func(t *testing.T) {
		require.Contains(t, constraintDef(t, db, "sales_comp_waste_unique"),
			"UNIQUE (preparation_waste_id)")
		require.Contains(t, constraintDef(t, db, "sales_comp_adjustment_unique"),
			"UNIQUE (charge_adjustment_id)")
		compReason := constraintDef(t, db, "sales_comp_reason_valid")
		for _, allowed := range []string{
			"CAFE_ERROR", "QUALITY_FAILURE", "SERVICE_RECOVERY", "OTHER",
		} {
			require.Contains(t, compReason, allowed)
		}
		require.Contains(t, constraintDef(t, db, "sales_comp_note_valid"), "500")
		require.Contains(t, constraintDef(t, db, "sales_comp_other_note_valid"), "btrim")

		require.Contains(t, constraintDef(t, db, "payment_void_payment_unique"),
			"UNIQUE (payment_id)")
		require.Contains(t, constraintDef(t, db, "payment_void_amount_positive"),
			"amount_vnd > 0")
		voidReason := constraintDef(t, db, "payment_void_reason_valid")
		for _, allowed := range []string{
			"DUPLICATE_PAYMENT", "WRONG_AMOUNT", "WRONG_METHOD",
			"PAYMENT_RECORDED_IN_ERROR", "OTHER",
		} {
			require.Contains(t, voidReason, allowed)
		}
		require.Contains(t, constraintDef(t, db, "payment_void_note_valid"), "500")
		require.Contains(t, constraintDef(t, db, "payment_void_other_note_valid"), "btrim")

		method := constraintDef(t, db, "refund_method_valid")
		require.Contains(t, method, "CASH")
		require.Contains(t, method, "MANUAL_QR")
		require.Contains(t, constraintDef(t, db, "refund_amount_positive"), "amount_vnd > 0")
		refundReason := constraintDef(t, db, "refund_reason_valid")
		for _, allowed := range []string{
			"CUSTOMER_REQUEST", "ITEM_UNAVAILABLE", "CAFE_ERROR", "OTHER",
		} {
			require.Contains(t, refundReason, allowed)
		}
		require.Contains(t, constraintDef(t, db, "refund_note_valid"), "500")
		require.Contains(t, constraintDef(t, db, "refund_other_note_valid"), "btrim")

		require.Contains(t, constraintDef(t, db, "refund_payment_allocation_pair_unique"),
			"UNIQUE (refund_id, payment_id)")
		require.Contains(t, constraintDef(t, db, "refund_payment_allocation_amount_positive"),
			"amount_vnd > 0")
		require.Contains(t, constraintDef(t, db, "refund_adjustment_allocation_pair_unique"),
			"UNIQUE (refund_id, charge_adjustment_id)")
		require.Contains(t, constraintDef(t, db, "refund_adjustment_allocation_amount_positive"),
			"amount_vnd > 0")
		require.Contains(t, constraintDef(t, db, "refund_completion_refund_unique"),
			"UNIQUE (refund_id)")
		reference := strings.Join(strings.Fields(
			constraintDef(t, db, "refund_completion_reference_valid")), " ")
		require.Contains(t, reference, "btrim")
		// PostgreSQL 16 decompiles BETWEEN 1 AND 100 into its two bounds.
		require.Contains(t, reference, ">= 1")
		require.Contains(t, reference, "<= 100")
	})

	t.Run("the required indexes exist", func(t *testing.T) {
		require.Contains(t, indexDef(t, db, "sales_comp_occurred_index"), "(occurred_at, id)")
		require.Contains(t, indexDef(t, db, "payment_void_shift_index"),
			"(sales_shift_id, occurred_at, id)")
		require.Contains(t, indexDef(t, db, "refund_check_index"), "(check_id, created_at, id)")
		require.Contains(t, indexDef(t, db, "refund_completed_sale_index"),
			"(completed_sale_id, created_at, id)")
		require.Contains(t, indexDef(t, db, "refund_shift_index"),
			"(sales_shift_id, created_at, id)")
		require.Contains(t, indexDef(t, db, "refund_payment_allocation_payment_index"),
			"(payment_id, refund_id)")
		require.Contains(t, indexDef(t, db, "refund_adjustment_allocation_adjustment_index"),
			"(charge_adjustment_id, refund_id)")
		require.Contains(t, indexDef(t, db, "refund_completion_completed_index"),
			"(completed_at, id)")
	})

	t.Run("a valid Comp, Void, Refund, allocations, and completion are accepted", func(t *testing.T) {
		fixture := newCorrectionFixture(t)
		env := fixture.Env

		compID, err := insertSalesComp(t, env, fixture.WasteIDs[0],
			fixture.AdjustmentIDs[0], "CAFE_ERROR", nil)
		require.NoError(t, err, "one Comp per Waste with a valid reason is the accepted shape")
		require.NotEqual(t, uuid.Nil, compID)

		voidID, err := insertPaymentVoid(t, env, fixture.PaymentID, 25000,
			"WRONG_AMOUNT", nil)
		require.NoError(t, err, "one whole-amount Void per Payment is the accepted shape")
		require.NotEqual(t, uuid.Nil, voidID)

		refundID, err := insertRefund(t, env, fixture.CheckID, "CASH", 25000,
			"ITEM_UNAVAILABLE", nil)
		require.NoError(t, err)
		require.NotEqual(t, uuid.Nil, refundID)

		_, err = env.DB.Exec(`
			INSERT INTO refund_payment_allocations (refund_id, payment_id, amount_vnd)
			VALUES ($1, $2, 10000)`, refundID, fixture.PaymentID)
		require.NoError(t, err, "a Payment allocation pair is accepted")

		_, err = env.DB.Exec(`
			INSERT INTO refund_adjustment_allocations (refund_id, charge_adjustment_id, amount_vnd)
			VALUES ($1, $2, 10000)`, refundID, fixture.AdjustmentIDs[0])
		require.NoError(t, err, "an Adjustment allocation pair is accepted")

		completionID, err := insertRefundCompletion(t, env, refundID, nil)
		require.NoError(t, err, "a Cash Refund completion carries no reference")
		require.NotEqual(t, uuid.Nil, completionID)
	})

	t.Run("duplicate facts and allocation pairs are rejected", func(t *testing.T) {
		fixture := newCorrectionFixture(t)
		env := fixture.Env

		_, err := insertSalesComp(t, env, fixture.WasteIDs[0], fixture.AdjustmentIDs[0],
			"CAFE_ERROR", nil)
		require.NoError(t, err)

		_, err = insertSalesComp(t, env, fixture.WasteIDs[0], fixture.AdjustmentIDs[1],
			"CAFE_ERROR", nil)
		require.Error(t, err, "a second Comp for one Waste must be rejected")
		require.Contains(t, err.Error(), "sales_comp_waste_unique")

		_, err = insertSalesComp(t, env, fixture.WasteIDs[1], fixture.AdjustmentIDs[0],
			"CAFE_ERROR", nil)
		require.Error(t, err, "a second Comp naming one Charge Adjustment must be rejected")
		require.Contains(t, err.Error(), "sales_comp_adjustment_unique")

		_, err = insertPaymentVoid(t, env, fixture.PaymentID, 25000, "WRONG_AMOUNT", nil)
		require.NoError(t, err)
		_, err = insertPaymentVoid(t, env, fixture.PaymentID, 25000, "WRONG_AMOUNT", nil)
		require.Error(t, err, "a second Void for one Payment must be rejected")
		require.Contains(t, err.Error(), "payment_void_payment_unique")

		refundID, err := insertRefund(t, env, fixture.CheckID, "CASH", 25000,
			"CUSTOMER_REQUEST", nil)
		require.NoError(t, err)

		_, err = env.DB.Exec(`
			INSERT INTO refund_payment_allocations (refund_id, payment_id, amount_vnd)
			VALUES ($1, $2, 10000)`, refundID, fixture.PaymentID)
		require.NoError(t, err)
		_, err = env.DB.Exec(`
			INSERT INTO refund_payment_allocations (refund_id, payment_id, amount_vnd)
			VALUES ($1, $2, 5000)`, refundID, fixture.PaymentID)
		require.Error(t, err, "a second allocation for one Refund/Payment pair must be rejected")
		require.Contains(t, err.Error(), "refund_payment_allocation_pair_unique")

		_, err = env.DB.Exec(`
			INSERT INTO refund_adjustment_allocations (refund_id, charge_adjustment_id, amount_vnd)
			VALUES ($1, $2, 10000)`, refundID, fixture.AdjustmentIDs[0])
		require.NoError(t, err)
		_, err = env.DB.Exec(`
			INSERT INTO refund_adjustment_allocations (refund_id, charge_adjustment_id, amount_vnd)
			VALUES ($1, $2, 5000)`, refundID, fixture.AdjustmentIDs[0])
		require.Error(t, err, "a second allocation for one Refund/Adjustment pair must be rejected")
		require.Contains(t, err.Error(), "refund_adjustment_allocation_pair_unique")

		_, err = insertRefundCompletion(t, env, refundID, nil)
		require.NoError(t, err)
		_, err = insertRefundCompletion(t, env, refundID, nil)
		require.Error(t, err, "a second completion for one Refund must be rejected")
		require.Contains(t, err.Error(), "refund_completion_refund_unique")
	})

	t.Run("invalid enums, amounts, and notes are rejected", func(t *testing.T) {
		fixture := newCorrectionFixture(t)
		env := fixture.Env

		_, err := insertSalesComp(t, env, fixture.WasteIDs[0], fixture.AdjustmentIDs[0],
			"STATE_RECORDED_IN_ERROR", nil)
		require.Error(t, err, "a non-Comp reason must be rejected")
		require.Contains(t, err.Error(), "sales_comp_reason_valid")

		_, err = insertSalesComp(t, env, fixture.WasteIDs[0], fixture.AdjustmentIDs[0],
			"OTHER", nil)
		require.Error(t, err, "Comp OTHER without a note must be rejected")
		require.Contains(t, err.Error(), "sales_comp_other_note_valid")

		_, err = insertSalesComp(t, env, fixture.WasteIDs[0], fixture.AdjustmentIDs[0],
			"CAFE_ERROR", strings.Repeat("a", 501))
		require.Error(t, err, "a Comp note over 500 characters must be rejected")
		require.Contains(t, err.Error(), "sales_comp_note_valid")

		_, err = insertPaymentVoid(t, env, fixture.PaymentID, 0, "WRONG_AMOUNT", nil)
		require.Error(t, err, "a non-positive Void amount must be rejected")
		require.Contains(t, err.Error(), "payment_void_amount_positive")

		_, err = insertPaymentVoid(t, env, fixture.PaymentID, 25000, "REFUNDED", nil)
		require.Error(t, err, "a non-Void reason must be rejected")
		require.Contains(t, err.Error(), "payment_void_reason_valid")

		_, err = insertPaymentVoid(t, env, fixture.PaymentID, 25000, "OTHER", nil)
		require.Error(t, err, "Void OTHER without a note must be rejected")
		require.Contains(t, err.Error(), "payment_void_other_note_valid")

		_, err = insertRefund(t, env, fixture.CheckID, "CARD", 25000,
			"CUSTOMER_REQUEST", nil)
		require.Error(t, err, "an unknown Refund method must be rejected")
		require.Contains(t, err.Error(), "refund_method_valid")

		_, err = insertRefund(t, env, fixture.CheckID, "CASH", 0,
			"CUSTOMER_REQUEST", nil)
		require.Error(t, err, "a non-positive Refund amount must be rejected")
		require.Contains(t, err.Error(), "refund_amount_positive")

		_, err = insertRefund(t, env, fixture.CheckID, "CASH", 25000,
			"STATE_RECORDED_IN_ERROR", nil)
		require.Error(t, err, "a non-Refund reason must be rejected")
		require.Contains(t, err.Error(), "refund_reason_valid")

		_, err = insertRefund(t, env, fixture.CheckID, "CASH", 25000, "OTHER", nil)
		require.Error(t, err, "Refund OTHER without a note must be rejected")
		require.Contains(t, err.Error(), "refund_other_note_valid")

		refundID, err := insertRefund(t, env, fixture.CheckID, "CASH", 25000,
			"CUSTOMER_REQUEST", nil)
		require.NoError(t, err)

		_, err = env.DB.Exec(`
			INSERT INTO refund_payment_allocations (refund_id, payment_id, amount_vnd)
			VALUES ($1, $2, 0)`, refundID, fixture.PaymentID)
		require.Error(t, err, "a non-positive Payment allocation must be rejected")
		require.Contains(t, err.Error(), "refund_payment_allocation_amount_positive")

		_, err = env.DB.Exec(`
			INSERT INTO refund_adjustment_allocations (refund_id, charge_adjustment_id, amount_vnd)
			VALUES ($1, $2, 0)`, refundID, fixture.AdjustmentIDs[0])
		require.Error(t, err, "a non-positive Adjustment allocation must be rejected")
		require.Contains(t, err.Error(), "refund_adjustment_allocation_amount_positive")
	})

	t.Run("the Refund completion reference is bounded and trimmed", func(t *testing.T) {
		fixture := newCorrectionFixture(t)
		env := fixture.Env

		newRefund := func(t *testing.T) uuid.UUID {
			t.Helper()
			refundID, err := insertRefund(t, env, fixture.CheckID, "MANUAL_QR", 25000,
				"CUSTOMER_REQUEST", nil)
			require.NoError(t, err)
			return refundID
		}

		_, err := insertRefundCompletion(t, env, newRefund(t), "")
		require.Error(t, err, "an empty reference must be rejected")
		require.Contains(t, err.Error(), "refund_completion_reference_valid")

		_, err = insertRefundCompletion(t, env, newRefund(t), strings.Repeat("a", 101))
		require.Error(t, err, "a reference over 100 characters must be rejected")
		require.Contains(t, err.Error(), "refund_completion_reference_valid")

		_, err = insertRefundCompletion(t, env, newRefund(t), " BANK-REF-1 ")
		require.Error(t, err, "an untrimmed reference must be rejected")
		require.Contains(t, err.Error(), "refund_completion_reference_valid")

		_, err = insertRefundCompletion(t, env, newRefund(t), strings.Repeat("a", 100))
		require.NoError(t, err, "a reference of exactly 100 characters is accepted")
	})
}
