//go:build integration

package sales_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/preparation"
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

// The projection must report a directly inserted Payment, and must refuse to
// serve a Check whose state contradicts its balance.
func TestProjectionReportsPaymentsAndGuardsSettlement(t *testing.T) {
	env := newSalesEnv(t)
	ctx := context.Background()

	session := env.commitTakeawayDraft(t, 2) // two items, one OPEN Check
	checkID := env.soleCheckID(t, session.ID)
	charge := env.checkCharge(t, checkID)

	t.Run("a partial payment lowers the balance and stays OPEN", func(t *testing.T) {
		env.insertCashPayment(t, checkID, charge/2)

		got := env.GetSessionOK(t, session.ID)
		require.Len(t, got.Checks, 1)
		require.Equal(t, sales.CheckStateOpen, got.Checks[0].State)
		require.Equal(t, charge/2, got.Checks[0].TotalAppliedVND)
		require.Equal(t, charge-charge/2, got.Checks[0].BalanceVND)
		require.Len(t, got.Checks[0].Payments, 1)
		require.Equal(t, sales.PaymentMethodCash, got.Checks[0].Payments[0].Method)
		require.NotNil(t, got.Checks[0].Payments[0].CashTenderedVND)
	})

	t.Run("a state that contradicts the balance fails the read", func(t *testing.T) {
		_, err := env.DB.ExecContext(ctx, `
			UPDATE checks SET state = 'SETTLED', settled_at = now(),
			  settled_by_staff_identity_id = $2,
			  settled_during_sales_shift_id = $3,
			  settled_staff_access_session_id = $4
			WHERE id = $1`,
			checkID, env.Actor.StaffID, env.ShiftID, env.Actor.SessionID)
		require.NoError(t, err)

		_, err = env.GetSession(t, session.ID)
		require.ErrorIs(t, err, sales.ErrSettlementInvariantViolated)
	})
}

func TestProjectionOrdersAndUnitsStartEmpty(t *testing.T) {
	env := newSalesEnv(t)
	session := env.commitTakeawayDraft(t, 2)

	got := env.GetSessionOK(t, session.ID)
	require.NotNil(t, got.Orders)
	require.Empty(t, got.Orders)
	require.NotNil(t, got.PreparationUnits)
	require.Empty(t, got.PreparationUnits)
	require.Len(t, got.Checks, 1)
	for _, allocation := range got.Checks[0].Allocations {
		require.False(t, allocation.Submitted, "nothing is submitted before Submit exists")
	}
}

// A Check with no corrections still projects the complete Phase 6C financial
// contract: the base charge, zero correction totals, and two empty arrays
// rather than nulls, so a client can iterate without a nil check.
func TestProjectionCorrectionCollectionsAreArrays(t *testing.T) {
	env := newSalesEnv(t)
	session := env.commitTakeawayDraft(t, 1)

	got := env.GetSessionOK(t, session.ID)
	require.Len(t, got.Checks, 1)
	check := got.Checks[0]
	require.Equal(t, check.ChargeVND, check.BaseChargeVND)
	require.Zero(t, check.TotalVoidedVND)
	require.Zero(t, check.TotalRefundedVND)
	require.Equal(t, check.TotalAppliedVND, check.EffectiveReceivedVND)
	require.Zero(t, check.PendingRefundVND)
	require.NotNil(t, check.ChargeAdjustments)
	require.Empty(t, check.ChargeAdjustments)
	require.NotNil(t, check.Refunds)
	require.Empty(t, check.Refunds)

	raw, err := json.Marshal(got)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"charge_adjustments":[]`)
	require.Contains(t, string(raw), `"refunds":[]`)
}

// A voided Payment stays in TotalAppliedVND and its whole reversal appears as
// Void evidence and TotalVoidedVND; effective receipt and the balance follow
// the corrected equation.
func TestProjectionPaymentVoidEvidence(t *testing.T) {
	env := newSalesEnv(t)
	session := env.commitTakeawayDraft(t, 1)
	checkID := env.soleCheckID(t, session.ID)
	paymentID := env.insertCashPayment(t, checkID, 10_000)

	voidID, err := insertPaymentVoid(t, env, paymentID, 10_000, "WRONG_AMOUNT", nil)
	require.NoError(t, err)

	got := env.GetSessionOK(t, session.ID)
	require.Len(t, got.Checks, 1)
	check := got.Checks[0]
	require.Equal(t, sales.CheckStateOpen, check.State)
	require.Equal(t, int64(10_000), check.TotalAppliedVND,
		"the original Payment sum is immutable")
	require.Equal(t, int64(10_000), check.TotalVoidedVND)
	require.Zero(t, check.EffectiveReceivedVND)
	require.Equal(t, check.ChargeVND, check.BalanceVND,
		"the whole Payment no longer covers any charge")
	require.Len(t, check.Payments, 1)
	require.NotNil(t, check.Payments[0].Void)
	require.Equal(t, voidID, check.Payments[0].Void.ID)
	require.Equal(t, int64(10_000), check.Payments[0].Void.AmountVND)
	require.Equal(t, "WRONG_AMOUNT", check.Payments[0].Void.Reason)
}

// paymentByID selects one Payment projection by id, failing when it is absent.
func paymentByID(t *testing.T, check sales.CheckResponse, paymentID uuid.UUID) sales.PaymentResponse {
	t.Helper()
	for _, payment := range check.Payments {
		if payment.ID == paymentID {
			return payment
		}
	}
	require.FailNow(t, "payment is absent from the check projection", paymentID)
	return sales.PaymentResponse{}
}

// A voided Payment cannot source a Refund, so it must not advertise refundable
// capacity even though its applied amount stays in the immutable receipt. Its
// non-voided peers keep their capacity, and both a pending and a completed
// Refund allocation still consume that peer capacity exactly as before.
func TestProjectionVoidedPaymentAdvertisesNoRefundableCapacity(t *testing.T) {
	fixture := newCorrectionFixture(t)
	env := fixture.Env

	// A second Payment gives the Check a non-voided peer. The fixture's two
	// live Comps removed the whole charge, so the Settled state is the evidence
	// the read-path guard requires.
	peerPaymentID := env.insertCashPayment(t, fixture.CheckID, 25_000)
	_, err := insertPaymentVoid(t, env, fixture.PaymentID, 25_000, "WRONG_AMOUNT", nil)
	require.NoError(t, err)
	_, err = env.DB.Exec(`
		UPDATE checks SET charge_vnd = 0, state = 'SETTLED', settled_at = now(),
		  settled_by_staff_identity_id = $2,
		  settled_during_sales_shift_id = $3,
		  settled_staff_access_session_id = $4
		WHERE id = $1`,
		fixture.CheckID, env.Actor.StaffID, env.ShiftID, env.Actor.SessionID)
	require.NoError(t, err)

	check := env.GetSessionOK(t, fixture.SessionID).Checks[0]
	require.NotNil(t, paymentByID(t, check, fixture.PaymentID).Void)
	require.Zero(t, paymentByID(t, check, fixture.PaymentID).RemainingRefundableVND,
		"a voided Payment advertises no refundable capacity")
	require.Equal(t, int64(25_000), paymentByID(t, check, peerPaymentID).RemainingRefundableVND,
		"a non-voided peer keeps its capacity")

	// The pending Manual QR Refund allocates equal sums from the peer Payment
	// and one Adjustment, reserving both capacities before money moves.
	refundID, err := insertRefund(t, env, fixture.CheckID, "MANUAL_QR", 10_000,
		"CUSTOMER_REQUEST", nil)
	require.NoError(t, err)
	_, err = env.DB.Exec(`
		INSERT INTO refund_payment_allocations (refund_id, payment_id, amount_vnd)
		VALUES ($1, $2, 10000)`, refundID, peerPaymentID)
	require.NoError(t, err)
	_, err = env.DB.Exec(`
		INSERT INTO refund_adjustment_allocations (refund_id, charge_adjustment_id, amount_vnd)
		VALUES ($1, $2, 10000)`, refundID, fixture.AdjustmentIDs[0])
	require.NoError(t, err)

	check = env.GetSessionOK(t, fixture.SessionID).Checks[0]
	require.Zero(t, paymentByID(t, check, fixture.PaymentID).RemainingRefundableVND)
	require.Equal(t, int64(15_000), paymentByID(t, check, peerPaymentID).RemainingRefundableVND,
		"a pending Manual QR Refund reserves peer capacity")

	_, err = insertRefundCompletion(t, env, refundID, "FT-OUT-1")
	require.NoError(t, err)

	check = env.GetSessionOK(t, fixture.SessionID).Checks[0]
	require.Zero(t, paymentByID(t, check, fixture.PaymentID).RemainingRefundableVND)
	require.Equal(t, int64(15_000), paymentByID(t, check, peerPaymentID).RemainingRefundableVND,
		"completing the Refund leaves the consumed peer capacity consumed")
}

// A live Comp reduces the stored charge; a pending Manual QR Refund then
// reserves Payment and Adjustment capacity without reducing effective receipt.
// Completing it is what removes money from the equation.
func TestProjectionCorrectedCheckFinancials(t *testing.T) {
	fixture := newCorrectionFixture(t)
	env := fixture.Env

	// The two live Comps removed the whole charge. Settle the Check with the
	// evidence the database and the read-path guard both require.
	_, err := env.DB.Exec(`
		UPDATE checks SET charge_vnd = 0, state = 'SETTLED', settled_at = now(),
		  settled_by_staff_identity_id = $2,
		  settled_during_sales_shift_id = $3,
		  settled_staff_access_session_id = $4
		WHERE id = $1`,
		fixture.CheckID, env.Actor.StaffID, env.ShiftID, env.Actor.SessionID)
	require.NoError(t, err)

	refundID, err := insertRefund(t, env, fixture.CheckID, "MANUAL_QR", 25_000,
		"CUSTOMER_REQUEST", nil)
	require.NoError(t, err)
	_, err = env.DB.Exec(`
		INSERT INTO refund_payment_allocations (refund_id, payment_id, amount_vnd)
		VALUES ($1, $2, 25000)`, refundID, fixture.PaymentID)
	require.NoError(t, err)
	_, err = env.DB.Exec(`
		INSERT INTO refund_adjustment_allocations (refund_id, charge_adjustment_id, amount_vnd)
		VALUES ($1, $2, 25000)`, refundID, fixture.AdjustmentIDs[0])
	require.NoError(t, err)

	t.Run("a pending Manual QR Refund is owed back, not received", func(t *testing.T) {
		got := env.GetSessionOK(t, fixture.SessionID)
		require.Len(t, got.Checks, 1)
		check := got.Checks[0]

		require.Equal(t, int64(50_000), check.BaseChargeVND)
		require.Equal(t, int64(0), check.ChargeVND)
		require.Equal(t, int64(25_000), check.TotalAppliedVND)
		require.Zero(t, check.TotalVoidedVND)
		require.Zero(t, check.TotalRefundedVND,
			"a pending Refund has not moved money")
		require.Equal(t, int64(25_000), check.EffectiveReceivedVND)
		require.Zero(t, check.BalanceVND)
		require.Equal(t, int64(25_000), check.PendingRefundVND)
		require.Equal(t, sales.CheckStateSettled, check.State)

		require.Len(t, check.ChargeAdjustments, 2)
		for _, adjustment := range check.ChargeAdjustments {
			require.Equal(t, "COMP", adjustment.Kind)
			require.Equal(t, "LIVE_CHECK", adjustment.Scope)
			require.Equal(t, int64(25_000), adjustment.AmountVND)
			require.NotNil(t, adjustment.PreparationWasteID)
		}
		require.Zero(t, check.ChargeAdjustments[0].RemainingRefundableVND,
			"the pending Refund reserved the first Adjustment's whole amount")
		require.Equal(t, int64(25_000), check.ChargeAdjustments[1].RemainingRefundableVND,
			"the second Adjustment is untouched")

		require.Len(t, check.Payments, 1)
		require.Zero(t, check.Payments[0].RemainingRefundableVND,
			"the pending Refund reserved the Payment's whole amount")
		require.Nil(t, check.Payments[0].Void)

		require.Len(t, check.Refunds, 1)
		refund := check.Refunds[0]
		require.Equal(t, refundID, refund.ID)
		require.Equal(t, sales.RefundMethodManualQR, refund.Method)
		require.Equal(t, sales.RefundStatePending, refund.State)
		require.Equal(t, int64(25_000), refund.AmountVND)
		require.Nil(t, refund.Completion)
		require.Len(t, refund.PaymentAllocations, 1)
		require.Equal(t, fixture.PaymentID, refund.PaymentAllocations[0].ID)
		require.Len(t, refund.AdjustmentAllocations, 1)
		require.Equal(t, fixture.AdjustmentIDs[0], refund.AdjustmentAllocations[0].ID)
	})

	t.Run("completing the Refund removes the money from effective receipt", func(t *testing.T) {
		_, err := insertRefundCompletion(t, env, refundID, "FT-OUT-1")
		require.NoError(t, err)

		got := env.GetSessionOK(t, fixture.SessionID)
		require.Len(t, got.Checks, 1)
		check := got.Checks[0]

		require.Equal(t, int64(25_000), check.TotalRefundedVND)
		require.Zero(t, check.EffectiveReceivedVND)
		require.Zero(t, check.BalanceVND)
		require.Zero(t, check.PendingRefundVND)

		require.Len(t, check.Refunds, 1)
		require.Equal(t, sales.RefundStateCompleted, check.Refunds[0].State)
		require.NotNil(t, check.Refunds[0].Completion)
		require.NotNil(t, check.Refunds[0].Completion.TransactionReference)
		require.Equal(t, "FT-OUT-1", *check.Refunds[0].Completion.TransactionReference)
	})
}

// --- Phase 6B: Remake projection (cross-slice arrangements) ---
//
// The arrangements reach across the ADR-024 boundary through
// internal/preparation's exported handlers — the same pattern FulfillAll uses
// in reverse — because only preparation may transition a unit or create a
// Remake. No preparation SQL is reproduced here.

// prepActor converts a Sales actor into the equivalent Preparation actor, so
// an identity seeded on the Sales side can drive preparation handlers.
func prepActor(actor sales.Actor) preparation.Actor {
	return preparation.Actor{StaffID: actor.StaffID, SessionID: actor.SessionID}
}

// prepHandlers returns the exported preparation correction handlers wired
// against the env's database.
type prepHandlers struct {
	advance    *preparation.AdvanceUnitHandler
	waste      *preparation.WasteUnitHandler
	remake     *preparation.RemakeUnitHandler
	correct    *preparation.CorrectStateHandler
	actor      preparation.Actor
	manager    preparation.Actor
	managerPIN string
}

func newPrepHandlers(env *salesEnv) *prepHandlers {
	runner := preparation.NewRunner(env.DB, env.Queries)
	// The env's BARISTA holds preparation.operate; the env's MANAGER holds it
	// too and its seeded PIN is "1234" (sales' shared actor hash).
	return &prepHandlers{
		advance:    preparation.NewAdvanceUnitHandler(runner),
		waste:      preparation.NewWasteUnitHandler(runner),
		remake:     preparation.NewRemakeUnitHandler(runner),
		correct:    preparation.NewCorrectStateHandler(runner),
		actor:      prepActor(env.BaristaActor()),
		manager:    prepActor(env.Actor),
		managerPIN: "1234",
	}
}

// advancePrepUnit moves one unit one step along the chain, failing the test
// on error.
func (p *prepHandlers) advancePrepUnit(t *testing.T, unitID uuid.UUID, target string) {
	t.Helper()
	_, _, err := p.advance.Handle(context.Background(), p.actor,
		preparation.AdvanceUnitCommand{RequestID: uuid.New(), UnitID: unitID, TargetState: target})
	require.NoError(t, err)
}

// fulfillPrepUnit advances one unit from QUEUED to FULFILLED, three calls
// along the linear chain. Unlike salesEnv.FulfillAll it targets a single unit,
// so terminal siblings (a WASTED source) are simply ignored.
func (p *prepHandlers) fulfillPrepUnit(t *testing.T, unitID uuid.UUID) {
	t.Helper()
	for _, target := range []string{
		preparation.StateInPreparation,
		preparation.StateReady,
		preparation.StateFulfilled,
	} {
		p.advancePrepUnit(t, unitID, target)
	}
}

// wastePrepUnit wastes one IN_PREPARATION or READY unit as the barista,
// returning the Waste fact's id.
func (p *prepHandlers) wastePrepUnit(t *testing.T, unitID uuid.UUID) uuid.UUID {
	t.Helper()
	_, resp, err := p.waste.Handle(context.Background(), p.actor,
		preparation.WasteUnitCommand{
			RequestID: uuid.New(),
			UnitID:    unitID,
			Reason:    preparation.ReasonQualityFailure,
		})
	require.NoError(t, err)
	return resp.ID
}

// remakePrepUnit creates the linked replacement for one Waste as the barista,
// returning the Remake result.
func (p *prepHandlers) remakePrepUnit(t *testing.T, wasteID uuid.UUID) preparation.RemakeResponse {
	t.Helper()
	_, resp, err := p.remake.Handle(context.Background(), p.actor,
		preparation.RemakeUnitCommand{
			RequestID: uuid.New(),
			WasteID:   wasteID,
			Reason:    preparation.ReasonQualityFailure,
		})
	require.NoError(t, err)
	return resp
}

// correctPrepUnitToQueued reverses one unit back to QUEUED as the manager
// with their own PIN.
func (p *prepHandlers) correctPrepUnitToQueued(t *testing.T, unitID uuid.UUID) {
	t.Helper()
	_, _, err := p.correct.Handle(context.Background(), p.manager,
		preparation.CorrectStateCommand{
			RequestID:          uuid.New(),
			PreparationUnitIDs: []uuid.UUID{unitID},
			TargetState:        preparation.StateQueued,
			Reason:             preparation.ReasonStateRecordedInError,
			ManagerPIN:         p.managerPIN,
		})
	require.NoError(t, err)
}

// committedDineInUnits commits, settles, and submits a dine-in round of the
// given quantity, returning the submitted projection. Every Check is settled
// first, so closure and charge-baseline assertions downstream face no
// outstanding money.
func (e *salesEnv) committedDineInUnits(t *testing.T, quantity int32) sales.ServiceSessionResponse {
	t.Helper()
	session := e.commitDineInDraftWithQuantity(t, quantity)
	checkID := e.soleCheckID(t, session.ID)
	charge := e.checkCharge(t, checkID)
	_, _, err := e.payCash(t, checkID, charge, charge)
	require.NoError(t, err)
	return e.Submit(t, session.ID)
}

func TestPreparationRemakeProjection(t *testing.T) {
	env := newSalesEnv(t)
	prep := newPrepHandlers(env)

	t.Run("original units project STANDARD with a null remake link", func(t *testing.T) {
		submitted := env.committedDineInUnits(t, 2)

		require.Len(t, submitted.PreparationUnits, 2)
		for _, unit := range submitted.PreparationUnits {
			require.Equal(t, "STANDARD", unit.Priority,
				"every original unit is STANDARD (spec §5.1)")
			require.Nil(t, unit.RemakeOfPreparationUnitID,
				"an original unit carries no remake link")
		}
	})

	t.Run("a remake projects REMAKE with the exact source link in the live session", func(t *testing.T) {
		submitted := env.committedDineInUnits(t, 2)
		source := submitted.PreparationUnits[0]

		prep.advancePrepUnit(t, source.ID, preparation.StateInPreparation)
		wasteID := prep.wastePrepUnit(t, source.ID)
		remake := prep.remakePrepUnit(t, wasteID)

		got := env.GetSessionOK(t, submitted.ID)
		require.Len(t, got.PreparationUnits, 3, "the wasted source stays projected beside its remake")

		var projectedSource, projectedRemake *sales.PreparationUnitResponse
		for i := range got.PreparationUnits {
			switch got.PreparationUnits[i].ID {
			case source.ID:
				projectedSource = &got.PreparationUnits[i]
			case remake.Unit.ID:
				projectedRemake = &got.PreparationUnits[i]
			}
		}
		require.NotNil(t, projectedSource, "the wasted source unit is still projected")
		require.NotNil(t, projectedRemake, "the remake unit is projected")

		require.Equal(t, sales.UnitStateWasted, projectedSource.State)
		require.Equal(t, "STANDARD", projectedSource.Priority,
			"a wasted original keeps STANDARD priority")
		require.Nil(t, projectedSource.RemakeOfPreparationUnitID)

		require.Equal(t, sales.UnitStateQueued, projectedRemake.State)
		require.Equal(t, "REMAKE", projectedRemake.Priority)
		require.NotNil(t, projectedRemake.RemakeOfPreparationUnitID)
		require.Equal(t, source.ID, *projectedRemake.RemakeOfPreparationUnitID,
			"the remake links to the exact wasted source unit")
		require.Equal(t, source.OrderItemID, projectedRemake.OrderItemID,
			"the remake belongs to the same Order Item")
		require.Equal(t, int32(3), projectedRemake.UnitNumber,
			"the remake takes the Order Item's next unit number")
		require.Equal(t, source.ItemName, projectedRemake.ItemName,
			"the remake carries the source's immutable snapshot")
		require.Equal(t, source.ServiceNumber, projectedRemake.ServiceNumber)
		require.NotNil(t, projectedRemake.Modifiers, "modifiers stay non-nil")

		// The query orders by queued_at ASC, id ASC; the mapper must preserve
		// that order. The two originals share one Submit clock reading, so
		// their relative order is the query's own tie-break and stays stable
		// across reads; the remake's later queue time must place it last.
		require.Equal(t, remake.Unit.ID, got.PreparationUnits[2].ID,
			"the remake trails both originals")
		require.ElementsMatch(t,
			[]uuid.UUID{submitted.PreparationUnits[0].ID, submitted.PreparationUnits[1].ID},
			[]uuid.UUID{got.PreparationUnits[0].ID, got.PreparationUnits[1].ID},
			"the originals keep their existing order")
	})

	t.Run("creating a remake changes no check charge, allocation quantity, or payment", func(t *testing.T) {
		submitted := env.committedDineInUnits(t, 2)
		before := submitted.Checks
		require.Len(t, before, 1)
		require.NotEmpty(t, before[0].Payments, "the check is settled before the correction")
		require.NotEmpty(t, before[0].Allocations)

		source := submitted.PreparationUnits[0]
		prep.advancePrepUnit(t, source.ID, preparation.StateInPreparation)
		wasteID := prep.wastePrepUnit(t, source.ID)
		prep.remakePrepUnit(t, wasteID)

		after := env.GetSessionOK(t, submitted.ID).Checks
		require.Equal(t, before, after,
			"a Waste and its Remake change no charge, allocation, or payment")
		require.Equal(t, before[0].ChargeVND, after[0].ChargeVND)
		for i, allocation := range after[0].Allocations {
			require.Equal(t, before[0].Allocations[i].AllocatedQuantity, allocation.AllocatedQuantity,
				"allocation quantity %d is untouched by the remake", i)
		}
	})
}

func TestCompletedSalePreparationRecoveryHistory(t *testing.T) {
	env := newSalesEnv(t)
	prep := newPrepHandlers(env)

	// A full recovery arc on one unit pair: the source is advanced, wasted,
	// remade; the remake is advanced, corrected back one step, then advanced
	// to FULFILLED. The correction makes the remake's history genuinely
	// ordered — it carries forward, reverse, and forward transitions again.
	submitted := env.committedDineInUnits(t, 1)
	source := submitted.PreparationUnits[0]

	prep.advancePrepUnit(t, source.ID, preparation.StateInPreparation)
	wasteID := prep.wastePrepUnit(t, source.ID)
	remake := prep.remakePrepUnit(t, wasteID)

	prep.advancePrepUnit(t, remake.Unit.ID, preparation.StateInPreparation)
	prep.correctPrepUnitToQueued(t, remake.Unit.ID)
	prep.fulfillPrepUnit(t, remake.Unit.ID)

	closed := env.Close(t, submitted.ID)

	// The Completed Sale read by session is the client's follow-up path; by-id
	// is covered by the equality test in TestCompletedSaleReads.
	sale, _, err := env.GetCompletedSaleBySession(t, submitted.ID)
	require.NoError(t, err)
	require.Equal(t, closed.ID, sale.ID)

	t.Run("both units project with their priority and link metadata", func(t *testing.T) {
		require.Len(t, sale.PreparationUnits, 2)
		byID := make(map[uuid.UUID]sales.PreparationUnitResponse, 2)
		for _, unit := range sale.PreparationUnits {
			byID[unit.ID] = unit
		}

		projectedSource, ok := byID[source.ID]
		require.True(t, ok, "the wasted source is in the Completed Sale")
		require.Equal(t, sales.UnitStateWasted, projectedSource.State)
		require.Equal(t, "STANDARD", projectedSource.Priority)
		require.Nil(t, projectedSource.RemakeOfPreparationUnitID)

		projectedRemake, ok := byID[remake.Unit.ID]
		require.True(t, ok, "the remake is in the Completed Sale")
		require.Equal(t, sales.UnitStateFulfilled, projectedRemake.State)
		require.Equal(t, "REMAKE", projectedRemake.Priority)
		require.NotNil(t, projectedRemake.RemakeOfPreparationUnitID)
		require.Equal(t, source.ID, *projectedRemake.RemakeOfPreparationUnitID)
	})

	t.Run("forward waste and reverse transitions are in occurrence order", func(t *testing.T) {
		type step struct {
			unitID uuid.UUID
			prior  string
			result string
		}
		want := []step{
			{source.ID, sales.UnitStateQueued, sales.UnitStateInPreparation},
			{source.ID, sales.UnitStateInPreparation, sales.UnitStateWasted},
			{remake.Unit.ID, sales.UnitStateQueued, sales.UnitStateInPreparation},
			{remake.Unit.ID, sales.UnitStateInPreparation, sales.UnitStateQueued},
			{remake.Unit.ID, sales.UnitStateQueued, sales.UnitStateInPreparation},
			{remake.Unit.ID, sales.UnitStateInPreparation, sales.UnitStateReady},
			{remake.Unit.ID, sales.UnitStateReady, sales.UnitStateFulfilled},
		}

		require.Len(t, sale.PreparationHistory, len(want))
		for i, expected := range want {
			got := sale.PreparationHistory[i]
			require.Equal(t, expected.unitID, got.UnitID, "transition %d unit", i)
			require.Equal(t, expected.prior, got.PriorState, "transition %d prior state", i)
			require.Equal(t, expected.result, got.ResultingState, "transition %d resulting state", i)
		}

		// Occurrence order means occurred_at never goes backwards.
		for i := 1; i < len(sale.PreparationHistory); i++ {
			require.False(t, sale.PreparationHistory[i].OccurredAt.
				Before(sale.PreparationHistory[i-1].OccurredAt),
				"transition %d must not precede its predecessor", i)
		}
	})
}

// TestCompletedSaleCoreExcludesPostSaleCorrections proves the structural
// snapshot boundary directly: after a post-sale Comp and Refund, the core
// Check still projects only the LIVE_CHECK adjustments and live Refunds that
// existed at closure, and its historical Payment refundable capacity ignores
// the allocation the post-sale Refund consumed. Only post_sale_corrections
// carries the later facts. No timestamp comparison is involved.
func TestCompletedSaleCoreExcludesPostSaleCorrections(t *testing.T) {
	env := newRefundEnv(t)

	session := env.commitDineInDraftWithQuantity(t, 2)
	checkID := env.soleCheckID(t, session.ID)
	for range 2 {
		_, status, err := env.payCash(t, checkID, 25000, 25000)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, status)
	}
	session = env.Submit(t, session.ID)
	require.Len(t, session.PreparationUnits, 2)

	wasteIDs := make([]uuid.UUID, 0, 2)
	for _, unit := range session.PreparationUnits {
		wasteIDs = append(wasteIDs, env.wasteUnitAfterAdvance(t, unit.ID))
	}
	payments := env.paymentIDsForCheck(t, checkID)
	require.Len(t, payments, 2)

	preCloseComp := env.compOK(t, env.compCommand(t, wasteIDs[0], sales.CompReasonCafeError, nil))
	require.Equal(t, sales.CompScopeLiveCheck, preCloseComp.Scope)
	preCloseRefund := env.refundOK(t, env.refundCommand(checkID, sales.RefundMethodCash,
		payments[0], preCloseComp.Comp.ChargeAdjustmentID, 25000))
	require.Equal(t, sales.RefundStateCompleted, preCloseRefund.Refund.State)

	sale := env.Close(t, session.ID)
	require.Len(t, sale.Checks, 1)
	require.Len(t, sale.Checks[0].ChargeAdjustments, 1)
	require.Len(t, sale.Checks[0].Refunds, 1)

	postCloseComp := env.compOK(t, env.compCommand(t, wasteIDs[1], sales.CompReasonCafeError, nil))
	require.Equal(t, sales.CompScopePostSale, postCloseComp.Scope)
	postCloseRefund := env.refundOK(t, env.refundCommand(checkID, sales.RefundMethodCash,
		payments[1], postCloseComp.Comp.ChargeAdjustmentID, 25000))
	require.Equal(t, sales.RefundStateCompleted, postCloseRefund.Refund.State)

	after, _, err := env.GetCompletedSale(t, sale.ID)
	require.NoError(t, err)
	require.Len(t, after.Checks, 1)
	check := after.Checks[0]

	require.EqualValues(t, 50000, check.BaseChargeVND)
	require.EqualValues(t, 25000, check.ChargeVND)
	require.EqualValues(t, 50000, check.TotalAppliedVND)
	require.EqualValues(t, 0, check.TotalVoidedVND)
	require.EqualValues(t, 25000, check.TotalRefundedVND,
		"the post-sale completed Refund is additive history, not closure evidence")
	require.EqualValues(t, 25000, check.EffectiveReceivedVND)
	require.EqualValues(t, 0, check.BalanceVND)
	require.EqualValues(t, 0, check.PendingRefundVND)

	require.Len(t, check.ChargeAdjustments, 1)
	require.Equal(t, preCloseComp.Comp.ChargeAdjustmentID, check.ChargeAdjustments[0].ID)
	require.Equal(t, sales.CompScopeLiveCheck, check.ChargeAdjustments[0].Scope)

	require.Len(t, check.Refunds, 1)
	require.Equal(t, preCloseRefund.Refund.ID, check.Refunds[0].ID)
	for _, refund := range check.Refunds {
		require.NotEqual(t, postCloseRefund.Refund.ID, refund.ID,
			"a post-sale Refund never enters the core snapshot")
	}

	for _, payment := range check.Payments {
		switch payment.ID {
		case payments[0]:
			require.EqualValues(t, 0, payment.RemainingRefundableVND,
				"the pre-close Refund consumed this Payment's capacity")
		case payments[1]:
			require.EqualValues(t, 25000, payment.RemainingRefundableVND,
				"the post-sale Refund allocation must not consume historical capacity")
		}
	}

	require.Len(t, after.PostSaleCorrections, 1)
	entry := after.PostSaleCorrections[0]
	require.Equal(t, postCloseComp.Comp.ChargeAdjustmentID, entry.Adjustment.ID)
	require.Equal(t, sales.CompScopePostSale, entry.Adjustment.Scope)
	require.Len(t, entry.Refunds, 1)
	require.Equal(t, postCloseRefund.Refund.ID, entry.Refunds[0].ID)
	require.Zero(t, entry.Adjustment.RemainingRefundableVND)
	require.Zero(t, entry.OutstandingRefundVND)
}
