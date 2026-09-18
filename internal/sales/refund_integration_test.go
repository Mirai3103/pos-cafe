//go:build integration

package sales_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// refundEnv drives the Sales Refund command over the Comp env's fixture world:
// a Refund always consumes a Charge Adjustment, and in this phase every Charge
// Adjustment is produced by a Comp, so the Comp helpers are the fixture.
type refundEnv struct {
	*compEnv
	// managerCode is the env manager's login code, resolved once so a command
	// helper can build a valid inline approval without a *testing.T.
	managerCode string
}

func newRefundEnv(t *testing.T) *refundEnv {
	t.Helper()
	env := &refundEnv{compEnv: newCompEnv(t)}
	env.managerCode = env.loginCode(t, env.Actor)
	return env
}

// ---------- Fixture helpers ----------

// paidTakeawayWastedUnitWithMethod mirrors paidTakeawayWastedUnit for either
// Payment method: one unit paid in full before Submit, then wasted.
func (e *refundEnv) paidTakeawayWastedUnitWithMethod(t *testing.T, method string) (
	sales.ServiceSessionResponse, sales.PreparationUnitResponse, uuid.UUID, uuid.UUID,
) {
	t.Helper()
	session := e.StartTakeaway(t)
	e.AddDraftItem(t, session.ID, e.CoffeeID, nil)
	session = e.Commit(t, session.ID)
	checkID := e.soleCheckID(t, session.ID)

	var status int
	var err error
	switch method {
	case sales.PaymentMethodManualQR:
		_, status, err = e.payManualQR(t, checkID, 25000, true, nil)
	default:
		_, status, err = e.payCash(t, checkID, 25000, 25000)
	}
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)

	session = e.Submit(t, session.ID)
	require.Len(t, session.PreparationUnits, 1)
	unit := session.PreparationUnits[0]
	e.advanceToReady(t, unit.ID)
	return session, unit, e.wasteUnit(t, unit.ID), checkID
}

// paidTwoUnitTwoComp pays a two-unit Check in full with two Cash Payments,
// submits it, wastes both units, and comps both: the Check ends with no charge
// and two independent refundable capacities.
func (e *refundEnv) paidTwoUnitTwoComp(t *testing.T) (
	sales.ServiceSessionResponse, uuid.UUID, sales.CompResult, sales.CompResult, []uuid.UUID,
) {
	t.Helper()
	session := e.commitDineInDraftWithQuantity(t, 2)
	checkID := e.soleCheckID(t, session.ID)
	for range 2 {
		_, status, err := e.payCash(t, checkID, 25000, 25000)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, status)
	}
	session = e.Submit(t, session.ID)
	require.Len(t, session.PreparationUnits, 2)

	compA := e.compOK(t, e.compCommand(t, e.wasteUnitAfterAdvance(t, session.PreparationUnits[0].ID),
		sales.CompReasonCafeError, nil))
	compB := e.compOK(t, e.compCommand(t, e.wasteUnitAfterAdvance(t, session.PreparationUnits[1].ID),
		sales.CompReasonCafeError, nil))
	return session, checkID, compA, compB, e.paymentIDsForCheck(t, checkID)
}

// wasteUnitAfterAdvance moves one unit to READY and wastes it.
func (e *refundEnv) wasteUnitAfterAdvance(t *testing.T, unitID uuid.UUID) uuid.UUID {
	t.Helper()
	e.advanceToReady(t, unitID)
	return e.wasteUnit(t, unitID)
}

// paymentIDsForCheck returns every Payment of one Check in receipt order.
func (e *refundEnv) paymentIDsForCheck(t *testing.T, checkID uuid.UUID) []uuid.UUID {
	t.Helper()
	rows, err := e.DB.Query(
		`SELECT id FROM payments WHERE check_id = $1 ORDER BY received_at ASC, id ASC`, checkID)
	require.NoError(t, err)
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		require.NoError(t, rows.Scan(&id))
		ids = append(ids, id)
	}
	require.NoError(t, rows.Err())
	return ids
}

// solePaymentIDForCheck returns the Check's only Payment id.
func (e *refundEnv) solePaymentIDForCheck(t *testing.T, checkID uuid.UUID) uuid.UUID {
	t.Helper()
	ids := e.paymentIDsForCheck(t, checkID)
	require.Len(t, ids, 1)
	return ids[0]
}

// ---------- Refund invocation helpers ----------

// refundCommand builds a one-Payment, one-Adjustment Refund command.
func (e *refundEnv) refundCommand(checkID uuid.UUID, method string, paymentID,
	adjustmentID uuid.UUID, amountVND int64,
) sales.RecordRefundCommand {
	return sales.RecordRefundCommand{
		RequestID: uuid.New(),
		CheckID:   checkID,
		Method:    method,
		AdjustmentAllocations: []sales.RefundAdjustmentAllocationInput{
			{ChargeAdjustmentID: adjustmentID, AmountVND: amountVND},
		},
		PaymentAllocations: []sales.RefundPaymentAllocationInput{
			{PaymentID: paymentID, AmountVND: amountVND},
		},
		Reason: sales.RefundReasonCustomerRequest,
		ManagerApproval: sales.ManagerApprovalInput{
			ApproverLoginCode: e.managerCode,
			ManagerPIN:        "1234",
		},
	}
}

func (e *refundEnv) refund(t *testing.T, cmd sales.RecordRefundCommand) (
	int, sales.RefundResult, error,
) {
	t.Helper()
	status, result, err := sales.NewRecordRefundHandler(e.Runner).
		Handle(context.Background(), e.Actor, cmd)
	if err != nil {
		status, err = mapErrorStatus(err)
	}
	return status, result, err
}

func (e *refundEnv) refundAs(t *testing.T, actor sales.Actor, cmd sales.RecordRefundCommand) (
	int, sales.RefundResult, error,
) {
	t.Helper()
	status, result, err := sales.NewRecordRefundHandler(e.Runner).
		Handle(context.Background(), actor, cmd)
	if err != nil {
		status, err = mapErrorStatus(err)
	}
	return status, result, err
}

func (e *refundEnv) refundOK(t *testing.T, cmd sales.RecordRefundCommand) sales.RefundResult {
	t.Helper()
	status, result, err := e.refund(t, cmd)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, status)
	return result
}

// ---------- Database assertion helpers ----------

type refundFact struct {
	ID              uuid.UUID
	CheckID         uuid.UUID
	CompletedSaleID uuid.NullUUID
	SalesShiftID    uuid.UUID
	Method          string
	AmountVND       int64
	Reason          string
	Note            sql.NullString
	Actor           uuid.UUID
	Approved        uuid.UUID
	CreatedAt       time.Time
}

func (e *refundEnv) refundFactFor(t *testing.T, refundID uuid.UUID) refundFact {
	t.Helper()
	var row refundFact
	require.NoError(t, e.DB.QueryRow(`
		SELECT id, check_id, completed_sale_id, sales_shift_id, method, amount_vnd,
		       reason, note, actor_staff_identity_id, approved_by_staff_identity_id,
		       created_at
		FROM refunds WHERE id = $1`, refundID).Scan(
		&row.ID, &row.CheckID, &row.CompletedSaleID, &row.SalesShiftID, &row.Method,
		&row.AmountVND, &row.Reason, &row.Note, &row.Actor, &row.Approved, &row.CreatedAt))
	return row
}

func (e *refundEnv) refundCount(t *testing.T, checkID uuid.UUID) int {
	t.Helper()
	var n int
	require.NoError(t, e.DB.QueryRow(
		`SELECT count(*) FROM refunds WHERE check_id = $1`, checkID).Scan(&n))
	return n
}

func (e *refundEnv) refundCompletionCount(t *testing.T, refundID uuid.UUID) int {
	t.Helper()
	var n int
	require.NoError(t, e.DB.QueryRow(
		`SELECT count(*) FROM refund_completions WHERE refund_id = $1`, refundID).Scan(&n))
	return n
}

func (e *refundEnv) refundAllocationSums(t *testing.T, refundID uuid.UUID) (int64, int64) {
	t.Helper()
	var paymentVND, adjustmentVND int64
	require.NoError(t, e.DB.QueryRow(
		`SELECT coalesce(sum(amount_vnd), 0) FROM refund_payment_allocations WHERE refund_id = $1`,
		refundID).Scan(&paymentVND))
	require.NoError(t, e.DB.QueryRow(
		`SELECT coalesce(sum(amount_vnd), 0) FROM refund_adjustment_allocations WHERE refund_id = $1`,
		refundID).Scan(&adjustmentVND))
	return paymentVND, adjustmentVND
}

func (e *refundEnv) voidPaymentDirect(t *testing.T, paymentID uuid.UUID, amountVND int64) {
	t.Helper()
	_, err := e.DB.Exec(`
		INSERT INTO payment_voids (payment_id, sales_shift_id, amount_vnd, reason,
		                           actor_staff_identity_id, staff_access_session_id,
		                           approved_by_staff_identity_id)
		VALUES ($1, $2, $3, 'PAYMENT_RECORDED_IN_ERROR', $4, $5, $4)`,
		paymentID, e.ShiftID, amountVND, e.Actor.StaffID, e.Actor.SessionID)
	require.NoError(t, err)
}

// ---------- Tests ----------

// TestRecordRefundCashCompletesImmediately records a Cash Refund against a
// pending Refund capacity: the Refund and its completion commit together, the
// Check's corrected excess is resolved, and both source capacities are
// consumed exactly once.
func TestRecordRefundCashCompletesImmediately(t *testing.T) {
	env := newRefundEnv(t)
	session, unit, wasteID, checkID := env.paidTakeawayWastedUnitWithMethod(t,
		sales.PaymentMethodCash)
	comp := env.compOK(t, env.compCommand(t, wasteID, sales.CompReasonCafeError, nil))
	paymentID := env.solePaymentIDForCheck(t, checkID)
	adjustmentID := comp.Comp.ChargeAdjustmentID

	cmd := env.refundCommand(checkID, sales.RefundMethodCash, paymentID, adjustmentID, 25000)
	result := env.refundOK(t, cmd)

	assert.Equal(t, sales.CompScopeLiveCheck, result.Scope)
	require.NotNil(t, result.ServiceSession, "a live Refund must return the updated Service Session")
	assert.Nil(t, result.CompletedSaleID)
	assert.Empty(t, result.PostSaleCorrections)

	refund := result.Refund
	assert.Equal(t, checkID, refund.CheckID)
	assert.Nil(t, refund.CompletedSaleID)
	assert.Equal(t, sales.RefundMethodCash, refund.Method)
	assert.EqualValues(t, 25000, refund.AmountVND)
	assert.Equal(t, sales.RefundStateCompleted, refund.State)
	assert.Equal(t, sales.RefundReasonCustomerRequest, refund.Reason)
	assert.Equal(t, env.Actor.StaffID, refund.ActorStaffIdentityID)
	assert.Equal(t, env.Actor.StaffID, refund.ApprovedByStaffIdentityID, "self-approval records both roles")
	require.NotNil(t, refund.Completion)
	assert.Nil(t, refund.Completion.TransactionReference, "a Cash Refund carries no bank reference")
	require.Len(t, refund.PaymentAllocations, 1)
	assert.Equal(t, paymentID, refund.PaymentAllocations[0].ID)
	assert.EqualValues(t, 25000, refund.PaymentAllocations[0].AmountVND)
	require.Len(t, refund.AdjustmentAllocations, 1)
	assert.Equal(t, adjustmentID, refund.AdjustmentAllocations[0].ID)
	assert.EqualValues(t, 25000, refund.AdjustmentAllocations[0].AmountVND)

	fact := env.refundFactFor(t, refund.ID)
	assert.Equal(t, checkID, fact.CheckID)
	assert.False(t, fact.CompletedSaleID.Valid)
	assert.Equal(t, env.ShiftID, fact.SalesShiftID)
	assert.Equal(t, sales.RefundMethodCash, fact.Method)
	assert.EqualValues(t, 25000, fact.AmountVND)
	assert.Equal(t, env.Actor.StaffID, fact.Actor)
	assert.Equal(t, env.Actor.StaffID, fact.Approved)
	assert.Equal(t, 1, env.refundCount(t, checkID))
	assert.Equal(t, 1, env.refundCompletionCount(t, refund.ID))
	paymentVND, adjustmentVND := env.refundAllocationSums(t, refund.ID)
	assert.EqualValues(t, 25000, paymentVND)
	assert.EqualValues(t, 25000, adjustmentVND)
	assert.NotEqual(t, uuid.Nil, unit.ID, "the fixture unit is consumed by the comp")

	assert.Equal(t, 1, env.countAuditEvents(t, sales.EventRefundRecorded))
	assert.Equal(t, 1, env.countAuditEvents(t, sales.EventRefundCompleted))

	projected := env.projectedCheck(t, session.ID, checkID)
	assert.EqualValues(t, 0, projected.ChargeVND)
	assert.EqualValues(t, 0, projected.BalanceVND)
	assert.EqualValues(t, 0, projected.PendingRefundVND)
	assert.EqualValues(t, 25000, projected.TotalRefundedVND)
	assert.EqualValues(t, 0, projected.EffectiveReceivedVND)
	assert.Equal(t, sales.CheckStateSettled, projected.State)
	require.Len(t, projected.Refunds, 1)
	assert.Equal(t, sales.RefundStateCompleted, projected.Refunds[0].State)
	assert.EqualValues(t, 0, projected.Payments[0].RemainingRefundableVND)
	assert.EqualValues(t, 0, projected.ChargeAdjustments[0].RemainingRefundableVND)
}

// TestRecordRefundManualQRStaysPending records a Manual QR Refund: the intent
// commits without a completion, money has not moved, and both capacities stay
// reserved so nothing can spend them twice.
func TestRecordRefundManualQRStaysPending(t *testing.T) {
	env := newRefundEnv(t)
	session, _, wasteID, checkID := env.paidTakeawayWastedUnitWithMethod(t,
		sales.PaymentMethodManualQR)
	comp := env.compOK(t, env.compCommand(t, wasteID, sales.CompReasonCafeError, nil))
	paymentID := env.solePaymentIDForCheck(t, checkID)

	cmd := env.refundCommand(checkID, sales.RefundMethodManualQR, paymentID,
		comp.Comp.ChargeAdjustmentID, 25000)
	result := env.refundOK(t, cmd)

	assert.Equal(t, sales.CompScopeLiveCheck, result.Scope)
	assert.Equal(t, sales.RefundStatePending, result.Refund.State)
	assert.Nil(t, result.Refund.Completion)

	assert.Equal(t, 1, env.refundCount(t, checkID))
	assert.Equal(t, 0, env.refundCompletionCount(t, result.Refund.ID))
	assert.Equal(t, 1, env.countAuditEvents(t, sales.EventRefundRecorded))
	assert.Equal(t, 0, env.countAuditEvents(t, sales.EventRefundCompleted))

	// Pending money has not left: pending_refund_vnd is unchanged, while both
	// capacities are fully reserved.
	projected := env.projectedCheck(t, session.ID, checkID)
	assert.EqualValues(t, 25000, projected.PendingRefundVND)
	assert.EqualValues(t, 0, projected.TotalRefundedVND)
	assert.EqualValues(t, 25000, projected.EffectiveReceivedVND)
	require.Len(t, projected.Refunds, 1)
	assert.Equal(t, sales.RefundStatePending, projected.Refunds[0].State)
	assert.EqualValues(t, 0, projected.Payments[0].RemainingRefundableVND)
	assert.EqualValues(t, 0, projected.ChargeAdjustments[0].RemainingRefundableVND)

	// A second Refund cannot spend the reserved capacities again: the pending
	// intent already reserved the whole obligation.
	second := env.refundCommand(checkID, sales.RefundMethodManualQR, paymentID,
		comp.Comp.ChargeAdjustmentID, 1)
	status, _, err := env.refund(t, second)
	require.ErrorIs(t, err, sales.ErrRefundExceedsPendingRefund)
	assert.Equal(t, http.StatusConflict, status)
	assert.Equal(t, 1, env.refundCount(t, checkID))
	assert.Equal(t, 1, env.countAuditEvents(t, sales.EventRefundRecorded))
}

// TestRecordRefundPartial records a Refund smaller than the pending amount:
// both sources keep their remainder and the Check still owes money back.
func TestRecordRefundPartial(t *testing.T) {
	env := newRefundEnv(t)
	session, _, wasteID, checkID := env.paidTakeawayWastedUnitWithMethod(t,
		sales.PaymentMethodCash)
	comp := env.compOK(t, env.compCommand(t, wasteID, sales.CompReasonCafeError, nil))
	paymentID := env.solePaymentIDForCheck(t, checkID)

	result := env.refundOK(t, env.refundCommand(checkID, sales.RefundMethodCash,
		paymentID, comp.Comp.ChargeAdjustmentID, 10000))
	assert.Equal(t, sales.RefundStateCompleted, result.Refund.State)

	projected := env.projectedCheck(t, session.ID, checkID)
	assert.EqualValues(t, 10000, projected.TotalRefundedVND)
	assert.EqualValues(t, 15000, projected.EffectiveReceivedVND)
	assert.EqualValues(t, 15000, projected.PendingRefundVND)
	assert.EqualValues(t, 0, projected.BalanceVND)
	require.Len(t, projected.Payments, 1)
	assert.EqualValues(t, 15000, projected.Payments[0].RemainingRefundableVND)
	require.Len(t, projected.ChargeAdjustments, 1)
	assert.EqualValues(t, 15000, projected.ChargeAdjustments[0].RemainingRefundableVND)
}

// TestRecordRefundExhaustedCapacities proves each source capacity is checked
// independently: an exhausted Adjustment rejects even when its Payment still
// has room, and an exhausted Payment rejects even when its Adjustment does.
func TestRecordRefundExhaustedCapacities(t *testing.T) {
	env := newRefundEnv(t)
	_, checkID, compA, compB, paymentIDs := env.paidTwoUnitTwoComp(t)
	require.Len(t, paymentIDs, 2)

	// The first Refund consumes Adjustment A and Payment 1 entirely.
	env.refundOK(t, env.refundCommand(checkID, sales.RefundMethodCash, paymentIDs[0],
		compA.Comp.ChargeAdjustmentID, 25000))

	// Adjustment A is exhausted while Payment 2 still has capacity.
	status, _, err := env.refund(t, env.refundCommand(checkID, sales.RefundMethodCash,
		paymentIDs[1], compA.Comp.ChargeAdjustmentID, 25000))
	require.ErrorIs(t, err, sales.ErrRefundExceedsAdjustmentCapacity)
	assert.Equal(t, http.StatusConflict, status)

	// Payment 1 is exhausted while Adjustment B still has capacity.
	status, _, err = env.refund(t, env.refundCommand(checkID, sales.RefundMethodCash,
		paymentIDs[0], compB.Comp.ChargeAdjustmentID, 25000))
	require.ErrorIs(t, err, sales.ErrRefundExceedsPaymentCapacity)
	assert.Equal(t, http.StatusConflict, status)

	assert.Equal(t, 1, env.refundCount(t, checkID))
	assert.Equal(t, 1, env.countAuditEvents(t, sales.EventRefundRecorded))
}

// TestRecordRefundPostSale records a Refund after the Service Session closed:
// it links to the Completed Sale, consumes the POST_SALE adjustment and the
// original Payment, and leaves the immutable sale core untouched.
func TestRecordRefundPostSale(t *testing.T) {
	env := newRefundEnv(t)
	session, _, wasteID := env.liveWastedUnit(t)
	checkID := env.soleCheckID(t, session.ID)

	_, status, err := env.payCash(t, checkID, 25000, 25000)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
	sale := env.Close(t, session.ID)

	comp := env.compOK(t, env.compCommand(t, wasteID, sales.CompReasonCafeError, nil))
	require.Equal(t, sales.CompScopePostSale, comp.Scope)
	adjustmentID := comp.Comp.ChargeAdjustmentID
	paymentID := env.solePaymentIDForCheck(t, checkID)

	before := env.compCheck(t, checkID)
	result := env.refundOK(t, env.refundCommand(checkID, sales.RefundMethodCash,
		paymentID, adjustmentID, 25000))

	assert.Equal(t, sales.CompScopePostSale, result.Scope)
	assert.Nil(t, result.ServiceSession, "a post-sale Refund returns no mutable Service Session")
	require.NotNil(t, result.CompletedSaleID)
	assert.Equal(t, sale.ID, *result.CompletedSaleID)
	require.NotNil(t, result.Refund.CompletedSaleID)
	assert.Equal(t, sale.ID, *result.Refund.CompletedSaleID)
	assert.Equal(t, sales.RefundStateCompleted, result.Refund.State)
	require.NotNil(t, result.Refund.Completion)

	require.Len(t, result.PostSaleCorrections, 1)
	entry := result.PostSaleCorrections[0]
	assert.Equal(t, adjustmentID, entry.Adjustment.ID)
	assert.Equal(t, sales.CompScopePostSale, entry.Adjustment.Scope)
	require.NotNil(t, entry.Adjustment.CompletedSaleID)
	assert.Equal(t, sale.ID, *entry.Adjustment.CompletedSaleID)
	assert.EqualValues(t, 0, entry.Adjustment.RemainingRefundableVND)
	assert.Equal(t, comp.Comp.ID, entry.Comp.ID)
	assert.EqualValues(t, 0, entry.OutstandingRefundVND)
	require.Len(t, entry.Refunds, 1)
	assert.Equal(t, result.Refund.ID, entry.Refunds[0].ID)
	assert.Equal(t, sales.RefundStateCompleted, entry.Refunds[0].State)

	fact := env.refundFactFor(t, result.Refund.ID)
	require.True(t, fact.CompletedSaleID.Valid)
	assert.Equal(t, sale.ID, fact.CompletedSaleID.UUID)
	assert.Equal(t, env.ShiftID, fact.SalesShiftID)

	// The live Check projection never serves post-sale history.
	projected := env.projectedCheck(t, session.ID, checkID)
	assert.Empty(t, projected.ChargeAdjustments)
	assert.Empty(t, projected.Refunds)
	assert.EqualValues(t, 25000, projected.ChargeVND)

	// The closed Check and Completed Sale core rows are unchanged.
	after := env.compCheck(t, checkID)
	assert.Equal(t, before.State, after.State)
	assert.EqualValues(t, before.ChargeVND, after.ChargeVND)
	assert.Equal(t, before.EvidenceCount, after.EvidenceCount)
}

// TestRecordRefundPostSaleManualQRStaysPending records a post-sale Manual QR
// Refund: its capacity is reserved at once, while the correction stays
// outstanding until the transfer is confirmed.
func TestRecordRefundPostSaleManualQRStaysPending(t *testing.T) {
	env := newRefundEnv(t)
	session, _, wasteID, checkID := env.paidTakeawayWastedUnitWithMethod(t,
		sales.PaymentMethodManualQR)
	sale := env.Close(t, session.ID)
	comp := env.compOK(t, env.compCommand(t, wasteID, sales.CompReasonCafeError, nil))
	require.Equal(t, sales.CompScopePostSale, comp.Scope)
	paymentID := env.solePaymentIDForCheck(t, checkID)
	require.EqualValues(t, 25000, *comp.OutstandingPostSaleRefundVND)

	result := env.refundOK(t, env.refundCommand(checkID, sales.RefundMethodManualQR,
		paymentID, comp.Comp.ChargeAdjustmentID, 25000))
	assert.Equal(t, sales.CompScopePostSale, result.Scope)
	assert.Equal(t, sales.RefundStatePending, result.Refund.State)
	require.NotNil(t, result.Refund.CompletedSaleID)
	assert.Equal(t, sale.ID, *result.Refund.CompletedSaleID)

	require.Len(t, result.PostSaleCorrections, 1)
	entry := result.PostSaleCorrections[0]
	assert.EqualValues(t, 0, entry.Adjustment.RemainingRefundableVND,
		"a pending intent reserves the correction's capacity")
	assert.EqualValues(t, 25000, entry.OutstandingRefundVND,
		"money has not moved until the Manual QR Refund is confirmed")
	require.Len(t, entry.Refunds, 1)
	assert.Equal(t, sales.RefundStatePending, entry.Refunds[0].State)

	assert.Equal(t, 1, env.refundCount(t, checkID))
	assert.Equal(t, 0, env.refundCompletionCount(t, result.Refund.ID))
}

// TestRecordRefundPostSaleAggregatePendingBound proves the post-sale gate is
// cumulative too: pending intents reserve the Completed Sale's outstanding
// correction amount, so they can never promise more than it still owes back.
func TestRecordRefundPostSaleAggregatePendingBound(t *testing.T) {
	env := newRefundEnv(t)
	session := env.commitDineInDraftWithQuantity(t, 2)
	checkID := env.soleCheckID(t, session.ID)
	_, status, err := env.payManualQR(t, checkID, 50000, true, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
	session = env.Submit(t, session.ID)
	require.Len(t, session.PreparationUnits, 2)

	wasteIDs := make([]uuid.UUID, 0, len(session.PreparationUnits))
	for _, unit := range session.PreparationUnits {
		wasteIDs = append(wasteIDs, env.wasteUnitAfterAdvance(t, unit.ID))
	}
	sale := env.Close(t, session.ID)
	comp := env.compOK(t, env.compCommand(t, wasteIDs[0], sales.CompReasonCafeError, nil))
	require.Equal(t, sales.CompScopePostSale, comp.Scope)
	require.NotNil(t, comp.CompletedSaleID)
	assert.Equal(t, sale.ID, *comp.CompletedSaleID)
	require.NotNil(t, comp.OutstandingPostSaleRefundVND)
	require.EqualValues(t, 25000, *comp.OutstandingPostSaleRefundVND)
	paymentID := env.solePaymentIDForCheck(t, checkID)

	// A 15,000 pending intent leaves 10,000 of the 25,000 obligation.
	first := env.refundOK(t, env.refundCommand(checkID, sales.RefundMethodManualQR,
		paymentID, comp.Comp.ChargeAdjustmentID, 15000))
	require.Equal(t, sales.CompScopePostSale, first.Scope)
	require.Equal(t, sales.RefundStatePending, first.Refund.State)

	// A second 15,000 exceeds the remaining obligation.
	status, _, err = env.refund(t, env.refundCommand(checkID, sales.RefundMethodManualQR,
		paymentID, comp.Comp.ChargeAdjustmentID, 15000))
	require.ErrorIs(t, err, sales.ErrRefundExceedsPendingRefund)
	assert.Equal(t, http.StatusConflict, status)

	// The exact remainder records; both intents stay pending, so no money has
	// moved and the entry still reports the whole 25,000 outstanding.
	second := env.refundOK(t, env.refundCommand(checkID, sales.RefundMethodManualQR,
		paymentID, comp.Comp.ChargeAdjustmentID, 10000))
	require.Len(t, second.PostSaleCorrections, 1)
	assert.EqualValues(t, 0, second.PostSaleCorrections[0].Adjustment.RemainingRefundableVND)
	assert.EqualValues(t, 25000, second.PostSaleCorrections[0].OutstandingRefundVND)

	// Nothing is left to promise.
	status, _, err = env.refund(t, env.refundCommand(checkID, sales.RefundMethodManualQR,
		paymentID, comp.Comp.ChargeAdjustmentID, 1))
	require.ErrorIs(t, err, sales.ErrRefundExceedsPendingRefund)
	assert.Equal(t, http.StatusConflict, status)
	assert.Equal(t, 2, env.refundCount(t, checkID))
	assert.Equal(t, 0, env.countAuditEvents(t, sales.EventRefundCompleted))
}

// TestRecordRefundSourceRejections proves the in-transaction source checks:
// a Payment or Adjustment of another Check, a Payment of another method, a
// voided Payment, and a scope mismatch are all refused atomically.
func TestRecordRefundSourceRejections(t *testing.T) {
	t.Run("rejects a payment from another check", func(t *testing.T) {
		env := newRefundEnv(t)
		_, _, wasteA, checkA := env.paidTakeawayWastedUnitWithMethod(t,
			sales.PaymentMethodCash)
		env.paidTakeawayWastedUnitWithMethod(t, sales.PaymentMethodCash)
		compA := env.compOK(t, env.compCommand(t, wasteA, sales.CompReasonCafeError, nil))
		// The second fixture's single Payment belongs to its own Check.
		var otherPaymentID uuid.UUID
		require.NoError(t, env.DB.QueryRow(`
			SELECT p.id FROM payments p
			WHERE p.check_id <> $1
			ORDER BY p.received_at DESC, p.id DESC LIMIT 1`, checkA).Scan(&otherPaymentID))

		status, _, err := env.refund(t, env.refundCommand(checkA, sales.RefundMethodCash,
			otherPaymentID, compA.Comp.ChargeAdjustmentID, 25000))
		require.ErrorIs(t, err, sales.ErrRefundAllocationInvalid)
		assert.Equal(t, http.StatusBadRequest, status)
		assert.Equal(t, 0, env.refundCount(t, checkA))
	})

	t.Run("rejects an adjustment from another check", func(t *testing.T) {
		env := newRefundEnv(t)
		_, _, wasteA, checkA := env.paidTakeawayWastedUnitWithMethod(t, sales.PaymentMethodCash)
		_, _, wasteB, _ := env.paidTakeawayWastedUnitWithMethod(t, sales.PaymentMethodCash)
		compA := env.compOK(t, env.compCommand(t, wasteA, sales.CompReasonCafeError, nil))
		compB := env.compOK(t, env.compCommand(t, wasteB, sales.CompReasonCafeError, nil))
		paymentA := env.solePaymentIDForCheck(t, checkA)

		status, _, err := env.refund(t, env.refundCommand(checkA, sales.RefundMethodCash,
			paymentA, compB.Comp.ChargeAdjustmentID, 25000))
		require.ErrorIs(t, err, sales.ErrRefundAllocationInvalid)
		assert.Equal(t, http.StatusBadRequest, status)
		assert.Equal(t, 0, env.refundCount(t, checkA))
		_ = compA
	})

	t.Run("rejects a payment of another method", func(t *testing.T) {
		env := newRefundEnv(t)
		_, _, wasteID, checkID := env.paidTakeawayWastedUnitWithMethod(t, sales.PaymentMethodCash)
		comp := env.compOK(t, env.compCommand(t, wasteID, sales.CompReasonCafeError, nil))
		paymentID := env.solePaymentIDForCheck(t, checkID)

		status, _, err := env.refund(t, env.refundCommand(checkID, sales.RefundMethodManualQR,
			paymentID, comp.Comp.ChargeAdjustmentID, 25000))
		require.ErrorIs(t, err, sales.ErrRefundMethodMismatch)
		assert.Equal(t, http.StatusConflict, status)
		assert.Equal(t, 0, env.refundCount(t, checkID))
	})

	t.Run("rejects a voided payment", func(t *testing.T) {
		env := newRefundEnv(t)
		_, _, wasteID, checkID := env.paidTakeawayWastedUnitWithMethod(t, sales.PaymentMethodCash)
		comp := env.compOK(t, env.compCommand(t, wasteID, sales.CompReasonCafeError, nil))
		paymentID := env.solePaymentIDForCheck(t, checkID)
		env.voidPaymentDirect(t, paymentID, 25000)

		status, _, err := env.refund(t, env.refundCommand(checkID, sales.RefundMethodCash,
			paymentID, comp.Comp.ChargeAdjustmentID, 25000))
		require.ErrorIs(t, err, sales.ErrRefundExceedsPaymentCapacity)
		assert.Equal(t, http.StatusConflict, status)
		assert.Equal(t, 0, env.refundCount(t, checkID))
	})

	t.Run("rejects a live adjustment in a post-sale refund", func(t *testing.T) {
		env := newRefundEnv(t)
		session, _, wasteID, checkID := env.paidTakeawayWastedUnitWithMethod(t,
			sales.PaymentMethodCash)
		comp := env.compOK(t, env.compCommand(t, wasteID, sales.CompReasonCafeError, nil))
		env.Close(t, session.ID)
		paymentID := env.solePaymentIDForCheck(t, checkID)

		// The Session is closed, so the Refund is post-sale and a LIVE_CHECK
		// adjustment cannot source it.
		status, _, err := env.refund(t, env.refundCommand(checkID, sales.RefundMethodCash,
			paymentID, comp.Comp.ChargeAdjustmentID, 25000))
		require.ErrorIs(t, err, sales.ErrRefundAllocationInvalid)
		assert.Equal(t, http.StatusBadRequest, status)
		assert.Equal(t, 0, env.refundCount(t, checkID))
	})
}

// TestRecordRefundRejectsAbovePendingRefund refuses a live Refund larger than
// the Check's current pending Refund even when both sources still carry room.
func TestRecordRefundRejectsAbovePendingRefund(t *testing.T) {
	env := newRefundEnv(t)
	session := env.commitDineInDraftWithQuantity(t, 2)
	checkID := env.soleCheckID(t, session.ID)

	// A partial 40,000 receipt on a 50,000 charge, then one 25,000 Comp:
	// charge 25,000, pending Refund 15,000, while the Adjustment still has
	// 25,000 and the Payment 40,000 of refundable capacity.
	_, status, err := env.payCash(t, checkID, 40000, 40000)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
	session = env.Submit(t, session.ID)
	require.Len(t, session.PreparationUnits, 2)
	comp := env.compOK(t, env.compCommand(t,
		env.wasteUnitAfterAdvance(t, session.PreparationUnits[0].ID),
		sales.CompReasonCafeError, nil))
	paymentID := env.solePaymentIDForCheck(t, checkID)

	projected := env.projectedCheck(t, session.ID, checkID)
	require.EqualValues(t, 15000, projected.PendingRefundVND)

	status, _, err = env.refund(t, env.refundCommand(checkID, sales.RefundMethodCash,
		paymentID, comp.Comp.ChargeAdjustmentID, 20000))
	require.ErrorIs(t, err, sales.ErrRefundExceedsPendingRefund)
	assert.Equal(t, http.StatusConflict, status)
	assert.Equal(t, 0, env.refundCount(t, checkID))

	// The largest valid amount still succeeds afterwards.
	env.refundOK(t, env.refundCommand(checkID, sales.RefundMethodCash,
		paymentID, comp.Comp.ChargeAdjustmentID, 15000))
}

// TestRecordRefundAggregatePendingBound proves the pending gate is cumulative:
// pending intents reserve the obligation at once, so a set of intents can
// never promise more than the Check's pending Refund, however much source
// capacity remains.
func TestRecordRefundAggregatePendingBound(t *testing.T) {
	env := newRefundEnv(t)
	session := env.commitDineInDraftWithQuantity(t, 2)
	checkID := env.soleCheckID(t, session.ID)

	// 50,000 base, a 40,000 Manual QR receipt, one 25,000 Comp: only 15,000 is
	// owed back while the Adjustment still carries 25,000 and the Payment
	// 40,000 of refundable capacity.
	_, status, err := env.payManualQR(t, checkID, 40000, true, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
	session = env.Submit(t, session.ID)
	require.Len(t, session.PreparationUnits, 2)
	comp := env.compOK(t, env.compCommand(t,
		env.wasteUnitAfterAdvance(t, session.PreparationUnits[0].ID),
		sales.CompReasonCafeError, nil))
	paymentID := env.solePaymentIDForCheck(t, checkID)

	projected := env.projectedCheck(t, session.ID, checkID)
	require.EqualValues(t, 15000, projected.PendingRefundVND)

	// Three pending intents of 5,000 exactly fit the 15,000 obligation.
	for i := range 3 {
		result := env.refundOK(t, env.refundCommand(checkID, sales.RefundMethodManualQR,
			paymentID, comp.Comp.ChargeAdjustmentID, 5000))
		require.Equal(t, sales.RefundStatePending, result.Refund.State, "intent %d", i)
	}

	// A fourth exceeds the obligation although both sources still have
	// capacity: the gate aggregates the pending intents.
	status, _, err = env.refund(t, env.refundCommand(checkID, sales.RefundMethodManualQR,
		paymentID, comp.Comp.ChargeAdjustmentID, 1))
	require.ErrorIs(t, err, sales.ErrRefundExceedsPendingRefund)
	assert.Equal(t, http.StatusConflict, status)

	assert.Equal(t, 3, env.refundCount(t, checkID))
	assert.Equal(t, 3, env.countAuditEvents(t, sales.EventRefundRecorded))
	assert.Equal(t, 0, env.countAuditEvents(t, sales.EventRefundCompleted))

	// Pending money has not moved, so the projection still owes the same
	// amount while all three intents keep their allocations reserved.
	projected = env.projectedCheck(t, session.ID, checkID)
	assert.EqualValues(t, 15000, projected.PendingRefundVND)
	require.Len(t, projected.Payments, 1)
	assert.EqualValues(t, 25000, projected.Payments[0].RemainingRefundableVND)
	require.Len(t, projected.ChargeAdjustments, 1)
	assert.EqualValues(t, 10000, projected.ChargeAdjustments[0].RemainingRefundableVND)
}

// TestRecordRefundCompletedDoesNotReservePending proves a completed Refund
// reduces the obligation instead of counting as an outstanding reservation:
// it already reduced pending_refund_vnd through the corrected financials.
func TestRecordRefundCompletedDoesNotReservePending(t *testing.T) {
	env := newRefundEnv(t)
	session := env.commitDineInDraftWithQuantity(t, 2)
	checkID := env.soleCheckID(t, session.ID)

	// 50,000 base, 30,000 Cash plus 15,000 Manual QR, one 25,000 Comp:
	// effective receipt 45,000 against a 25,000 charge leaves 20,000 owed.
	_, status, err := env.payCash(t, checkID, 30000, 30000)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
	_, status, err = env.payManualQR(t, checkID, 15000, true, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
	session = env.Submit(t, session.ID)
	require.Len(t, session.PreparationUnits, 2)
	comp := env.compOK(t, env.compCommand(t,
		env.wasteUnitAfterAdvance(t, session.PreparationUnits[0].ID),
		sales.CompReasonCafeError, nil))
	paymentIDs := env.paymentIDsForCheck(t, checkID)
	require.Len(t, paymentIDs, 2)
	cashPaymentID, qrPaymentID := paymentIDs[0], paymentIDs[1]

	// A completed Cash Refund moves 10,000 and reduces the pending Refund to
	// 10,000. It is not an outstanding reservation.
	env.refundOK(t, env.refundCommand(checkID, sales.RefundMethodCash, cashPaymentID,
		comp.Comp.ChargeAdjustmentID, 10000))
	projected := env.projectedCheck(t, session.ID, checkID)
	require.EqualValues(t, 10000, projected.PendingRefundVND)

	// The whole remaining 10,000 fits in two pending intents...
	env.refundOK(t, env.refundCommand(checkID, sales.RefundMethodManualQR, qrPaymentID,
		comp.Comp.ChargeAdjustmentID, 5000))
	env.refundOK(t, env.refundCommand(checkID, sales.RefundMethodManualQR, qrPaymentID,
		comp.Comp.ChargeAdjustmentID, 5000))

	// ...and no more. Had the completed Refund been double counted as a
	// reservation, the first 5,000 would have been refused.
	status, _, err = env.refund(t, env.refundCommand(checkID, sales.RefundMethodManualQR,
		qrPaymentID, comp.Comp.ChargeAdjustmentID, 1))
	require.ErrorIs(t, err, sales.ErrRefundExceedsPendingRefund)
	assert.Equal(t, http.StatusConflict, status)
	assert.Equal(t, 3, env.refundCount(t, checkID))
}

// TestRecordRefundReplay proves an exact replay returns the identical stored
// result with no duplicate Refund, allocation, completion, or audit, and that
// credentials never enter the stored result.
func TestRecordRefundReplay(t *testing.T) {
	env := newRefundEnv(t)
	_, _, wasteID, checkID := env.paidTakeawayWastedUnitWithMethod(t, sales.PaymentMethodCash)
	comp := env.compOK(t, env.compCommand(t, wasteID, sales.CompReasonCafeError, nil))
	paymentID := env.solePaymentIDForCheck(t, checkID)

	cmd := env.refundCommand(checkID, sales.RefundMethodCash, paymentID,
		comp.Comp.ChargeAdjustmentID, 25000)
	first := env.refundOK(t, cmd)

	status, second, err := env.refund(t, cmd)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, status)
	firstJSON, err := json.Marshal(first)
	require.NoError(t, err)
	secondJSON, err := json.Marshal(second)
	require.NoError(t, err)
	assert.JSONEq(t, string(firstJSON), string(secondJSON),
		"an exact replay returns the stored result")

	assert.Equal(t, 1, env.refundCount(t, checkID))
	assert.Equal(t, 1, env.refundCompletionCount(t, first.Refund.ID))
	assert.Equal(t, 1, env.countAuditEvents(t, sales.EventRefundRecorded))
	assert.Equal(t, 1, env.countAuditEvents(t, sales.EventRefundCompleted))
	assert.Equal(t, 1, env.idempotencyClaimCount(t, env.Actor, cmd.RequestID))

	stored := env.storedResultBody(t, env.Actor, cmd.RequestID)
	assert.NotContains(t, stored, `"manager_pin"`)
	assert.NotContains(t, stored, `"approver_login_code"`)
	assert.NotContains(t, stored, `"1234"`)
}

// TestRecordRefundAuthorization covers the one Manager Approval, the
// initiator's own capability, self-approval, separate recording, and the
// denial of a replay whose approver has since been disabled.
func TestRecordRefundAuthorization(t *testing.T) {
	fixture := func(t *testing.T) (*refundEnv, sales.RecordRefundCommand) {
		t.Helper()
		env := newRefundEnv(t)
		_, _, wasteID, checkID := env.paidTakeawayWastedUnitWithMethod(t, sales.PaymentMethodCash)
		comp := env.compOK(t, env.compCommand(t, wasteID, sales.CompReasonCafeError, nil))
		paymentID := env.solePaymentIDForCheck(t, checkID)
		return env, env.refundCommand(checkID, sales.RefundMethodCash, paymentID,
			comp.Comp.ChargeAdjustmentID, 25000)
	}

	t.Run("denies a wrong manager pin with committed denial evidence", func(t *testing.T) {
		env, cmd := fixture(t)
		cmd.ManagerApproval.ManagerPIN = "00000000"
		status, _, err := env.refund(t, cmd)
		require.ErrorIs(t, err, sales.ErrForbidden)
		assert.Equal(t, http.StatusForbidden, status)
		assert.Equal(t, 0, env.refundCount(t, cmd.CheckID))
		assert.Equal(t, 1, env.countAuditEvents(t, sales.EventAuthorizationDenied))
		assert.Equal(t, 0, env.idempotencyClaimCount(t, env.Actor, cmd.RequestID),
			"a denied approval must not consume the request id")
	})

	t.Run("denies an approver without the manager role", func(t *testing.T) {
		env, cmd := fixture(t)
		cashier := env.newCashierActor(t)
		cmd.ManagerApproval = env.approvalFor(t, cashier)
		status, _, err := env.refund(t, cmd)
		require.ErrorIs(t, err, sales.ErrForbidden)
		assert.Equal(t, http.StatusForbidden, status)
		assert.Equal(t, 0, env.refundCount(t, cmd.CheckID))
	})

	t.Run("denies an initiator without sales.operate", func(t *testing.T) {
		env, cmd := fixture(t)
		status, _, err := env.refundAs(t, env.BaristaActor(), cmd)
		require.ErrorIs(t, err, sales.ErrForbidden)
		assert.Equal(t, http.StatusForbidden, status)
		assert.Equal(t, 0, env.refundCount(t, cmd.CheckID))
	})

	t.Run("records a cashier initiator and manager approver separately", func(t *testing.T) {
		env, cmd := fixture(t)
		cashier := env.newCashierActor(t)
		status, result, err := env.refundAs(t, cashier, cmd)
		require.NoError(t, err)
		require.Equal(t, http.StatusCreated, status)
		assert.Equal(t, cashier.StaffID, result.Refund.ActorStaffIdentityID)
		assert.Equal(t, env.Actor.StaffID, result.Refund.ApprovedByStaffIdentityID)
	})

	t.Run("replay is denied once the approver is disabled", func(t *testing.T) {
		env, cmd := fixture(t)
		cashier := env.newCashierActor(t)
		status, first, err := env.refundAs(t, cashier, cmd)
		require.NoError(t, err)
		require.Equal(t, http.StatusCreated, status)

		_, err = env.DB.Exec(
			`UPDATE staff_identities SET enabled = false WHERE id = $1`, env.Actor.StaffID)
		require.NoError(t, err)

		status, _, err = env.refundAs(t, cashier, cmd)
		require.ErrorIs(t, err, sales.ErrForbidden)
		assert.Equal(t, http.StatusForbidden, status)
		assert.Equal(t, 1, env.refundCount(t, cmd.CheckID))
		assert.Equal(t, first.Refund.ID, env.refundFactFor(t, first.Refund.ID).ID)
	})

	t.Run("keeps credentials out of audits", func(t *testing.T) {
		env, cmd := fixture(t)
		code := env.loginCode(t, env.Actor)
		env.refundOK(t, cmd)

		var details []byte
		require.NoError(t, env.DB.QueryRow(`
			SELECT details FROM audit_events
			WHERE event_type = $1
			ORDER BY occurred_at DESC, id DESC LIMIT 1`,
			sales.EventRefundRecorded).Scan(&details))
		text := string(details)
		assert.NotContains(t, text, "manager_pin")
		assert.NotContains(t, text, "approver_login_code")
		assert.NotContains(t, text, code)
		assert.NotContains(t, text, `"1234"`)
	})
}

// TestRecordRefundAuditDetails pins the business meaning each audit records.
func TestRecordRefundAuditDetails(t *testing.T) {
	env := newRefundEnv(t)
	_, _, wasteID, checkID := env.paidTakeawayWastedUnitWithMethod(t, sales.PaymentMethodCash)
	comp := env.compOK(t, env.compCommand(t, wasteID, sales.CompReasonCafeError, nil))
	paymentID := env.solePaymentIDForCheck(t, checkID)

	note := "trả món"
	cmd := env.refundCommand(checkID, sales.RefundMethodCash, paymentID,
		comp.Comp.ChargeAdjustmentID, 25000)
	cmd.Reason = sales.RefundReasonItemUnavailable
	cmd.Note = &note
	result := env.refundOK(t, cmd)

	var raw []byte
	require.NoError(t, env.DB.QueryRow(`
		SELECT details FROM audit_events
		WHERE event_type = $1 AND details->>'refund_id' = $2`,
		sales.EventRefundRecorded, result.Refund.ID.String()).Scan(&raw))
	recorded := map[string]any{}
	require.NoError(t, json.Unmarshal(raw, &recorded))
	assert.Equal(t, checkID.String(), recorded["check_id"])
	assert.Equal(t, sales.RefundMethodCash, recorded["method"])
	assert.EqualValues(t, 25000, recorded["amount_vnd"])
	assert.Equal(t, sales.RefundReasonItemUnavailable, recorded["reason"])
	assert.Equal(t, note, recorded["note"])
	assert.Equal(t, env.ShiftID.String(), recorded["sales_shift_id"])
	assert.Equal(t, env.Actor.StaffID.String(), recorded["actor_staff_identity_id"])
	assert.Equal(t, env.Actor.StaffID.String(), recorded["approved_by_staff_identity_id"])

	var completedRaw []byte
	require.NoError(t, env.DB.QueryRow(`
		SELECT details FROM audit_events
		WHERE event_type = $1 AND details->>'refund_id' = $2`,
		sales.EventRefundCompleted, result.Refund.ID.String()).Scan(&completedRaw))
	completed := map[string]any{}
	require.NoError(t, json.Unmarshal(completedRaw, &completed))
	assert.Equal(t, checkID.String(), completed["check_id"])
	assert.EqualValues(t, 25000, completed["amount_vnd"])
	assert.Equal(t, env.Actor.StaffID.String(), completed["completed_by_staff_identity_id"])
}

// TestRecordRefundInjectedFailures proves every failure inside the transaction
// — the Refund fact, both allocation sets, the completion, the audit, and the
// executor's stored result — rolls back the whole command.
func TestRecordRefundInjectedFailures(t *testing.T) {
	type fixture struct {
		env           *refundEnv
		checkID       uuid.UUID
		adjustmentID  uuid.UUID
		paymentID     uuid.UUID
		command       sales.RecordRefundCommand
		refundsBefore int
		auditsBefore  int
	}

	newFixture := func(t *testing.T) fixture {
		t.Helper()
		env := newRefundEnv(t)
		_, _, wasteID, checkID := env.paidTakeawayWastedUnitWithMethod(t, sales.PaymentMethodCash)
		comp := env.compOK(t, env.compCommand(t, wasteID, sales.CompReasonCafeError, nil))
		paymentID := env.solePaymentIDForCheck(t, checkID)
		cmd := env.refundCommand(checkID, sales.RefundMethodCash, paymentID,
			comp.Comp.ChargeAdjustmentID, 25000)
		return fixture{
			env:           env,
			checkID:       checkID,
			adjustmentID:  comp.Comp.ChargeAdjustmentID,
			paymentID:     paymentID,
			command:       cmd,
			refundsBefore: env.refundCount(t, checkID),
			auditsBefore:  env.countAuditEvents(t, sales.EventRefundRecorded),
		}
	}

	assertRolledBack := func(t *testing.T, f fixture) {
		t.Helper()
		assert.Equal(t, f.refundsBefore, f.env.refundCount(t, f.checkID))
		assert.Equal(t, f.auditsBefore, f.env.countAuditEvents(t, sales.EventRefundRecorded))
		assert.Equal(t, 0, f.env.countAuditEvents(t, sales.EventRefundCompleted))
		assert.Equal(t, 0, f.env.idempotencyClaimCount(t, f.env.Actor, f.command.RequestID))

		var completions int
		require.NoError(t, f.env.DB.QueryRow(`
			SELECT count(*) FROM refund_completions
			WHERE refund_id IN (SELECT id FROM refunds WHERE check_id = $1)`,
			f.checkID).Scan(&completions))
		assert.Equal(t, 0, completions, "no completion survives")
	}

	t.Run("payment allocation failure", func(t *testing.T) {
		f := newFixture(t)
		installCompTrigger(t, f.env.compEnv, "fail_refund_payment_alloc", `
			CREATE FUNCTION fail_refund_payment_alloc() RETURNS trigger AS $$
			BEGIN
			    IF NEW.payment_id = '`+f.paymentID.String()+`'::uuid THEN
			        RAISE EXCEPTION 'forced refund payment allocation failure';
			    END IF;
			    RETURN NEW;
			END;
			$$ LANGUAGE plpgsql;
			CREATE TRIGGER fail_refund_payment_alloc
			BEFORE INSERT ON refund_payment_allocations
			FOR EACH ROW EXECUTE FUNCTION fail_refund_payment_alloc();`,
			"refund_payment_allocations")

		status, _, err := f.env.refund(t, f.command)
		require.Error(t, err)
		require.Equal(t, http.StatusInternalServerError, status)
		assertRolledBack(t, f)
	})

	t.Run("adjustment allocation failure", func(t *testing.T) {
		f := newFixture(t)
		installCompTrigger(t, f.env.compEnv, "fail_refund_adjustment_alloc", `
			CREATE FUNCTION fail_refund_adjustment_alloc() RETURNS trigger AS $$
			BEGIN
			    IF NEW.charge_adjustment_id = '`+f.adjustmentID.String()+`'::uuid THEN
			        RAISE EXCEPTION 'forced refund adjustment allocation failure';
			    END IF;
			    RETURN NEW;
			END;
			$$ LANGUAGE plpgsql;
			CREATE TRIGGER fail_refund_adjustment_alloc
			BEFORE INSERT ON refund_adjustment_allocations
			FOR EACH ROW EXECUTE FUNCTION fail_refund_adjustment_alloc();`,
			"refund_adjustment_allocations")

		status, _, err := f.env.refund(t, f.command)
		require.Error(t, err)
		require.Equal(t, http.StatusInternalServerError, status)
		assertRolledBack(t, f)
	})

	t.Run("completion failure", func(t *testing.T) {
		f := newFixture(t)
		installCompTrigger(t, f.env.compEnv, "fail_refund_completion", `
			CREATE FUNCTION fail_refund_completion() RETURNS trigger AS $$
			BEGIN
			    RAISE EXCEPTION 'forced refund completion failure';
			END;
			$$ LANGUAGE plpgsql;
			CREATE TRIGGER fail_refund_completion
			BEFORE INSERT ON refund_completions
			FOR EACH ROW EXECUTE FUNCTION fail_refund_completion();`,
			"refund_completions")

		status, _, err := f.env.refund(t, f.command)
		require.Error(t, err)
		require.Equal(t, http.StatusInternalServerError, status)
		assertRolledBack(t, f)
	})

	t.Run("result storage failure", func(t *testing.T) {
		f := newFixture(t)
		installCompTrigger(t, f.env.compEnv, "fail_refund_result_store", `
			CREATE FUNCTION fail_refund_result_store() RETURNS trigger AS $$
			BEGIN
			    IF NEW.action = 'sales.record_refund' AND NEW.response_code <> 0 THEN
			        RAISE EXCEPTION 'forced refund result store failure';
			    END IF;
			    RETURN NEW;
			END;
			$$ LANGUAGE plpgsql;
			CREATE TRIGGER fail_refund_result_store
			BEFORE UPDATE ON idempotency_keys
			FOR EACH ROW EXECUTE FUNCTION fail_refund_result_store();`,
			"idempotency_keys")

		status, _, err := f.env.refund(t, f.command)
		require.Error(t, err)
		require.Equal(t, http.StatusInternalServerError, status)
		assertRolledBack(t, f)
	})
}

// ---------- HTTP route ----------

// httpRefundFixture is a paid, comped Takeaway Session plus the ids a Refund
// request needs, all created through the real routes.
type httpRefundFixture struct {
	e            *echo.Echo
	token        string
	managerCode  string
	sessionID    uuid.UUID
	checkID      uuid.UUID
	paymentID    uuid.UUID
	adjustmentID uuid.UUID
}

func setupHTTPRefundFixture(t *testing.T) *httpRefundFixture {
	t.Helper()
	e, db, q := newTestServer(t)
	managerToken, managerCode := signInReturningLoginCode(t, e, q, []string{"MANAGER"}, "1357")
	baristaToken, _ := signInReturningLoginCode(t, e, q, []string{"BARISTA"}, "2468")
	_ = openShiftOverHTTP(t, e, managerToken)
	tableID := seedTable(t, db, "Bàn Refund")
	itemID := seedMenuItem(t, db, "Cà phê refund", 25000)

	body, _ := json.Marshal(map[string]any{
		"request_id": uuid.New(), "table_ids": []uuid.UUID{tableID},
	})
	rec := doRequest(t, e, http.MethodPost,
		"/api/v1/sales/service-sessions/dine-in", managerToken, body)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	session := decodeSession(t, rec)

	body, _ = json.Marshal(map[string]any{"request_id": uuid.New(), "menu_item_id": itemID})
	rec = doRequest(t, e, http.MethodPost,
		"/api/v1/sales/service-sessions/"+session.ID.String()+"/draft/items", managerToken, body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	body, _ = json.Marshal(map[string]any{"request_id": uuid.New()})
	rec = doRequest(t, e, http.MethodPost,
		"/api/v1/sales/service-sessions/"+session.ID.String()+"/draft/commit", managerToken, body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	session = decodeSession(t, rec)

	var checkID uuid.UUID
	require.NoError(t, db.QueryRow(
		`SELECT id FROM checks WHERE service_session_id = $1`, session.ID).Scan(&checkID))

	body, _ = json.Marshal(map[string]any{
		"request_id": uuid.New(), "applied_amount_vnd": 25000, "cash_tendered_vnd": 25000,
	})
	rec = doRequest(t, e, http.MethodPost,
		"/api/v1/sales/checks/"+checkID.String()+"/payments/cash", managerToken, body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	body, _ = json.Marshal(map[string]any{"request_id": uuid.New()})
	rec = doRequest(t, e, http.MethodPost,
		"/api/v1/sales/service-sessions/"+session.ID.String()+"/submit", managerToken, body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	session = decodeSession(t, rec)
	require.Len(t, session.PreparationUnits, 1)
	unitID := session.PreparationUnits[0].ID

	for _, target := range []string{"IN_PREPARATION", "READY"} {
		body, _ = json.Marshal(map[string]any{
			"request_id": uuid.New(), "target_state": target,
		})
		rec = doRequest(t, e, http.MethodPost,
			"/api/v1/preparation/units/"+unitID.String()+"/advance", baristaToken, body)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	}
	body, _ = json.Marshal(map[string]any{"request_id": uuid.New(), "reason": "QUALITY_FAILURE"})
	rec = doRequest(t, e, http.MethodPost,
		"/api/v1/preparation/units/"+unitID.String()+"/waste", baristaToken, body)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var wasteEnvelope envelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &wasteEnvelope))
	var waste struct {
		ID uuid.UUID `json:"id"`
	}
	require.NoError(t, json.Unmarshal(wasteEnvelope.Data, &waste))
	require.NotEqual(t, uuid.Nil, waste.ID)

	body, _ = json.Marshal(map[string]any{
		"request_id": uuid.New(),
		"reason":     "CAFE_ERROR",
		"manager_approval": map[string]any{
			"approver_login_code": managerCode,
			"manager_pin":         "1357",
		},
	})
	rec = doRequest(t, e, http.MethodPost,
		"/api/v1/sales/wastes/"+waste.ID.String()+"/comp", managerToken, body)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var compEnvelope envelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &compEnvelope))
	var compResult sales.CompResult
	require.NoError(t, json.Unmarshal(compEnvelope.Data, &compResult))

	var paymentID uuid.UUID
	require.NoError(t, db.QueryRow(
		`SELECT id FROM payments WHERE check_id = $1`, checkID).Scan(&paymentID))

	return &httpRefundFixture{
		e:            e,
		token:        managerToken,
		managerCode:  managerCode,
		sessionID:    session.ID,
		checkID:      checkID,
		paymentID:    paymentID,
		adjustmentID: compResult.Comp.ChargeAdjustmentID,
	}
}

// TestSalesHTTPRefund drives the route end to end and pins its statuses,
// envelope, replay, and credential-free body.
func TestSalesHTTPRefund(t *testing.T) {
	fx := setupHTTPRefundFixture(t)

	refundBody := func(requestID uuid.UUID, amountVND int64, approval map[string]any) []byte {
		body, _ := json.Marshal(map[string]any{
			"request_id": requestID,
			"check_id":   fx.checkID,
			"method":     "CASH",
			"adjustment_allocations": []map[string]any{
				{"charge_adjustment_id": fx.adjustmentID, "amount_vnd": amountVND},
			},
			"payment_allocations": []map[string]any{
				{"payment_id": fx.paymentID, "amount_vnd": amountVND},
			},
			"reason":           "CUSTOMER_REQUEST",
			"manager_approval": approval,
		})
		return body
	}
	approval := map[string]any{"approver_login_code": fx.managerCode, "manager_pin": "1357"}
	happyRequestID := uuid.New()

	t.Run("records a refund and returns 201", func(t *testing.T) {
		rec := doRequest(t, fx.e, http.MethodPost, "/api/v1/sales/refunds", fx.token,
			refundBody(happyRequestID, 10000, approval))
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

		var env envelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
		require.True(t, env.Success)
		var result sales.RefundResult
		require.NoError(t, json.Unmarshal(env.Data, &result))
		assert.Equal(t, sales.CompScopeLiveCheck, result.Scope)
		assert.EqualValues(t, 10000, result.Refund.AmountVND)
		assert.Equal(t, sales.RefundStateCompleted, result.Refund.State)
		require.NotNil(t, result.ServiceSession)
		assert.Nil(t, result.CompletedSaleID)

		raw := rec.Body.String()
		assert.Contains(t, raw, `"service_session"`)
		assert.NotContains(t, raw, `"manager_pin"`)
		assert.NotContains(t, raw, `"approver_login_code"`)
		assert.NotContains(t, raw, `"1357"`)
	})

	t.Run("exact replay returns the same refund", func(t *testing.T) {
		first := doRequest(t, fx.e, http.MethodPost, "/api/v1/sales/refunds", fx.token,
			refundBody(happyRequestID, 10000, approval))
		require.Equal(t, http.StatusCreated, first.Code, first.Body.String())

		second := doRequest(t, fx.e, http.MethodPost, "/api/v1/sales/refunds", fx.token,
			refundBody(happyRequestID, 10000, approval))
		require.Equal(t, http.StatusCreated, second.Code, second.Body.String())
		assert.JSONEq(t, first.Body.String(), second.Body.String())
	})

	t.Run("unequal allocation sums answer 400", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"request_id": uuid.New(),
			"check_id":   fx.checkID,
			"method":     "CASH",
			"adjustment_allocations": []map[string]any{
				{"charge_adjustment_id": fx.adjustmentID, "amount_vnd": 1000},
			},
			"payment_allocations": []map[string]any{
				{"payment_id": fx.paymentID, "amount_vnd": 2000},
			},
			"reason":           "CUSTOMER_REQUEST",
			"manager_approval": approval,
		})
		rec := doRequest(t, fx.e, http.MethodPost, "/api/v1/sales/refunds", fx.token, body)
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		var env envelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
		require.NotNil(t, env.Error)
		assert.Equal(t, "INVALID_INPUT", env.Error.Code)
	})

	t.Run("allocation sum overflow answers 422", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"request_id": uuid.New(),
			"check_id":   fx.checkID,
			"method":     "CASH",
			"adjustment_allocations": []map[string]any{
				{"charge_adjustment_id": uuid.New(), "amount_vnd": int64(math.MaxInt64)},
				{"charge_adjustment_id": uuid.New(), "amount_vnd": int64(math.MaxInt64)},
			},
			"payment_allocations": []map[string]any{
				{"payment_id": uuid.New(), "amount_vnd": int64(math.MaxInt64)},
				{"payment_id": uuid.New(), "amount_vnd": int64(math.MaxInt64)},
			},
			"reason":           "CUSTOMER_REQUEST",
			"manager_approval": approval,
		})
		rec := doRequest(t, fx.e, http.MethodPost, "/api/v1/sales/refunds", fx.token, body)
		require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	})

	t.Run("unknown check answers 404", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"request_id": uuid.New(),
			"check_id":   uuid.New(),
			"method":     "CASH",
			"adjustment_allocations": []map[string]any{
				{"charge_adjustment_id": fx.adjustmentID, "amount_vnd": 1000},
			},
			"payment_allocations": []map[string]any{
				{"payment_id": fx.paymentID, "amount_vnd": 1000},
			},
			"reason":           "CUSTOMER_REQUEST",
			"manager_approval": approval,
		})
		rec := doRequest(t, fx.e, http.MethodPost, "/api/v1/sales/refunds", fx.token, body)
		require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
		var env envelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
		require.NotNil(t, env.Error)
		assert.Equal(t, "CHECK_NOT_FOUND", env.Error.Code)
	})

	t.Run("wrong manager pin answers 403", func(t *testing.T) {
		rec := doRequest(t, fx.e, http.MethodPost, "/api/v1/sales/refunds", fx.token,
			refundBody(uuid.New(), 1000, map[string]any{
				"approver_login_code": fx.managerCode,
				"manager_pin":         "00000000",
			}))
		require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
		var env envelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
		require.NotNil(t, env.Error)
		assert.Equal(t, "NOT_AUTHORIZED", env.Error.Code)
	})

	t.Run("exhausted capacity answers 409", func(t *testing.T) {
		// The first subtest consumed 10,000 of the 25,000 capacity; a fresh
		// request for the rest of the Check's excess finds too little left.
		rec := doRequest(t, fx.e, http.MethodPost, "/api/v1/sales/refunds", fx.token,
			refundBody(uuid.New(), 20000, approval))
		require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
		var env envelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
		require.NotNil(t, env.Error)
		assert.Equal(t, "REFUND_EXCEEDS_PENDING_REFUND", env.Error.Code)
	})
}
