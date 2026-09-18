//go:build integration

package sales_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// voidEnv drives the Sales Payment Void command over the Refund env's fixture
// world. Void consumes a Payment and a Check, and the interesting Check shapes
// (a reduced charge, a Refund allocation) are produced by Comp and Refund, so
// those helpers are the fixture.
type voidEnv struct {
	*refundEnv
}

func newVoidEnv(t *testing.T) *voidEnv {
	t.Helper()
	return &voidEnv{refundEnv: newRefundEnv(t)}
}

// ---------- Invocation helpers ----------

// voidCommand builds a Payment Void command with a fresh request id.
func (e *voidEnv) voidCommand(paymentID uuid.UUID, reason string,
	note *string,
) sales.VoidPaymentCommand {
	return e.voidCommandWithRequestID(uuid.New(), paymentID, reason, note)
}

func (e *voidEnv) voidCommandWithRequestID(requestID, paymentID uuid.UUID, reason string,
	note *string,
) sales.VoidPaymentCommand {
	return sales.VoidPaymentCommand{
		RequestID:       requestID,
		PaymentID:       paymentID,
		Reason:          reason,
		Note:            note,
		ManagerApproval: sales.ManagerApprovalInput{
			ApproverLoginCode: e.managerCode,
			ManagerPIN:        "1234",
		},
	}
}

// void runs Void and returns the mapped HTTP status and raw error.
func (e *voidEnv) void(t *testing.T, cmd sales.VoidPaymentCommand) (
	int, sales.ServiceSessionResponse, error,
) {
	t.Helper()
	status, result, err := sales.NewVoidPaymentHandler(e.Runner).
		Handle(context.Background(), e.Actor, cmd)
	if err != nil {
		status, err = mapErrorStatus(err)
	}
	return status, result, err
}

// voidAs runs Void as another actor, for authorization tests.
func (e *voidEnv) voidAs(t *testing.T, actor sales.Actor, cmd sales.VoidPaymentCommand) (
	int, sales.ServiceSessionResponse, error,
) {
	t.Helper()
	status, result, err := sales.NewVoidPaymentHandler(e.Runner).
		Handle(context.Background(), actor, cmd)
	if err != nil {
		status, err = mapErrorStatus(err)
	}
	return status, result, err
}

// voidOK runs Void and requires the 201 the route promises.
func (e *voidEnv) voidOK(t *testing.T, cmd sales.VoidPaymentCommand) sales.ServiceSessionResponse {
	t.Helper()
	status, result, err := e.void(t, cmd)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, status)
	return result
}

// settledTakeawayCheck opens one Coffee Takeaway, pays 25,000 VND with the
// given method, and returns the settled Check and its only Payment. A Manual
// QR payment carries the given reference.
func (e *voidEnv) settledTakeawayCheck(t *testing.T, method string,
	reference *string,
) (uuid.UUID, uuid.UUID) {
	t.Helper()
	session := e.StartTakeaway(t)
	e.AddDraftItem(t, session.ID, e.CoffeeID, nil)
	session = e.Commit(t, session.ID)
	checkID := e.soleCheckID(t, session.ID)

	var status int
	var err error
	if method == sales.PaymentMethodManualQR {
		_, status, err = e.payManualQR(t, checkID, 25000, true, reference)
	} else {
		_, status, err = e.payCash(t, checkID, 25000, 25000)
	}
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, sales.CheckStateSettled, e.compCheck(t, checkID).State)

	return checkID, e.solePaymentIDForCheck(t, checkID)
}

// ---------- Database assertion helpers ----------

type voidPaymentFact struct {
	ID                   uuid.UUID
	CheckID              uuid.UUID
	SalesShiftID         uuid.UUID
	Method               string
	AppliedAmountVND     int64
	CashTenderedVND      sql.NullInt64
	ChangeDueVND         sql.NullInt64
	TransactionReference sql.NullString
	ReceivedAt           time.Time
}

func (e *voidEnv) paymentFactFor(t *testing.T, paymentID uuid.UUID) voidPaymentFact {
	t.Helper()
	var row voidPaymentFact
	require.NoError(t, e.DB.QueryRow(`
		SELECT id, check_id, sales_shift_id, method, applied_amount_vnd,
		       cash_tendered_vnd, change_due_vnd, transaction_reference, received_at
		FROM payments WHERE id = $1`, paymentID).Scan(
		&row.ID, &row.CheckID, &row.SalesShiftID, &row.Method, &row.AppliedAmountVND,
		&row.CashTenderedVND, &row.ChangeDueVND, &row.TransactionReference, &row.ReceivedAt))
	return row
}

type voidFactRow struct {
	ID           uuid.UUID
	PaymentID    uuid.UUID
	SalesShiftID uuid.UUID
	AmountVND    int64
	Reason       string
	Note         sql.NullString
	Actor        uuid.UUID
	Approved     uuid.UUID
	OccurredAt   time.Time
}

func (e *voidEnv) voidFactFor(t *testing.T, paymentID uuid.UUID) voidFactRow {
	t.Helper()
	var row voidFactRow
	require.NoError(t, e.DB.QueryRow(`
		SELECT id, payment_id, sales_shift_id, amount_vnd, reason, note,
		       actor_staff_identity_id, approved_by_staff_identity_id, occurred_at
		FROM payment_voids WHERE payment_id = $1`, paymentID).Scan(
		&row.ID, &row.PaymentID, &row.SalesShiftID, &row.AmountVND, &row.Reason,
		&row.Note, &row.Actor, &row.Approved, &row.OccurredAt))
	return row
}

func (e *voidEnv) voidCount(t *testing.T, paymentID uuid.UUID) int {
	t.Helper()
	var n int
	require.NoError(t, e.DB.QueryRow(
		`SELECT count(*) FROM payment_voids WHERE payment_id = $1`, paymentID).Scan(&n))
	return n
}

// voidAuditDetails reads the first audit event of one type whose details name
// the given key/value pair.
func (e *voidEnv) voidAuditDetails(t *testing.T, eventType, key, value string) map[string]any {
	t.Helper()
	var raw []byte
	require.NoError(t, e.DB.QueryRow(`
		SELECT details FROM audit_events
		WHERE event_type = $1 AND details->>$2 = $3
		ORDER BY occurred_at ASC, id ASC LIMIT 1`,
		eventType, key, value).Scan(&raw))
	out := map[string]any{}
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

// installVoidTrigger installs a forced-failure trigger and removes it when the
// test ends, mirroring the package's other failure-injection fixtures.
func installVoidTrigger(t *testing.T, env *voidEnv, name, ddl, table string) {
	t.Helper()
	_, err := env.DB.Exec(ddl)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = env.DB.Exec("DROP TRIGGER IF EXISTS " + name + " ON " + table)
		_, _ = env.DB.Exec("DROP FUNCTION IF EXISTS " + name + "()")
	})
}

// assertVoidRolledBack proves one injected failure left no trace: no Void
// fact, an untouched Payment and Check, no business events, and an
// unconsumed request id.
func assertVoidRolledBack(t *testing.T, env *voidEnv, paymentID, checkID uuid.UUID,
	beforePayment voidPaymentFact, beforeCheck compCheckRow, requestID uuid.UUID,
) {
	t.Helper()
	assert.Equal(t, 0, env.voidCount(t, paymentID))
	assert.Equal(t, beforePayment, env.paymentFactFor(t, paymentID))
	after := env.compCheck(t, checkID)
	assert.Equal(t, beforeCheck.State, after.State)
	assert.EqualValues(t, beforeCheck.ChargeVND, after.ChargeVND)
	assert.Equal(t, beforeCheck.EvidenceCount, after.EvidenceCount)
	assert.Equal(t, beforeCheck.SettledBy, after.SettledBy)
	assert.Equal(t, beforeCheck.SettledShiftID, after.SettledShiftID)
	assert.Equal(t, beforeCheck.SettledSession, after.SettledSession)
	assert.Equal(t, beforeCheck.SettledAt.Time, after.SettledAt.Time)
	assert.Equal(t, 0, env.countAuditEvents(t, sales.EventPaymentVoided))
	assert.Equal(t, 0, env.countAuditEvents(t, sales.EventCheckReopenedAfterPaymentVoid))
	assert.Equal(t, 0, env.idempotencyClaimCount(t, env.Actor, requestID))
}

// ---------- Tests ----------

// TestVoidPaymentCashReopensSettledCheck voids a whole Cash Payment: the Check
// reopens, all four settlement-evidence columns clear, the source Payment is
// untouched, and the Void fact and both audit events commit.
func TestVoidPaymentCashReopensSettledCheck(t *testing.T) {
	env := newVoidEnv(t)
	checkID, paymentID := env.settledTakeawayCheck(t, sales.PaymentMethodCash, nil)
	before := env.paymentFactFor(t, paymentID)

	note := "  ghi nhầm máy  "
	status, result, err := env.void(t,
		env.voidCommand(paymentID, sales.VoidReasonWrongAmount, &note))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, status)

	require.Len(t, result.Checks, 1)
	check := result.Checks[0]
	assert.Equal(t, checkID, check.ID)
	assert.Equal(t, sales.CheckStateOpen, check.State, "a voided covering Payment reopens the Check")
	assert.EqualValues(t, 25000, check.ChargeVND)
	assert.EqualValues(t, 25000, check.TotalAppliedVND, "the original Payment sum never changes meaning")
	assert.EqualValues(t, 25000, check.TotalVoidedVND)
	assert.EqualValues(t, 0, check.TotalRefundedVND)
	assert.EqualValues(t, 0, check.EffectiveReceivedVND)
	assert.EqualValues(t, 25000, check.BalanceVND)
	assert.EqualValues(t, 0, check.PendingRefundVND)

	require.Len(t, check.Payments, 1)
	require.NotNil(t, check.Payments[0].Void, "void evidence rides on the source Payment")
	void := check.Payments[0].Void
	assert.EqualValues(t, before.AppliedAmountVND, void.AmountVND, "the Void is always whole")
	assert.Equal(t, sales.VoidReasonWrongAmount, void.Reason)
	require.NotNil(t, void.Note)
	assert.Equal(t, "ghi nhầm máy", *void.Note, "the note must be trimmed")
	assert.Equal(t, env.Actor.StaffID, void.ActorStaffIdentityID)
	assert.Equal(t, env.Actor.StaffID, void.ApprovedByStaffIdentityID, "self-approval records both roles")

	after := env.compCheck(t, checkID)
	assert.Equal(t, sales.CheckStateOpen, after.State)
	assert.Equal(t, 0, after.EvidenceCount, "all four settlement-evidence columns must clear together")

	fact := env.voidFactFor(t, paymentID)
	assert.EqualValues(t, before.AppliedAmountVND, fact.AmountVND)
	assert.Equal(t, env.ShiftID, fact.SalesShiftID)
	assert.Equal(t, sales.VoidReasonWrongAmount, fact.Reason)
	require.True(t, fact.Note.Valid)
	assert.Equal(t, "ghi nhầm máy", fact.Note.String)
	assert.Equal(t, env.Actor.StaffID, fact.Actor)
	assert.Equal(t, env.Actor.StaffID, fact.Approved)
	assert.False(t, fact.OccurredAt.IsZero())

	assert.Equal(t, before, env.paymentFactFor(t, paymentID),
		"the source Payment row is never edited")
	assert.Equal(t, 1, env.voidCount(t, paymentID))
	assert.Equal(t, 1, env.countAuditEventsForCheck(t, sales.EventPaymentVoided, checkID))
	assert.Equal(t, 1, env.countAuditEventsForCheck(t, sales.EventCheckReopenedAfterPaymentVoid, checkID))
}

// TestVoidPaymentManualQRReopensSettledCheck proves the void is method-agnostic
// and preserves the source Payment's bank reference.
func TestVoidPaymentManualQRReopensSettledCheck(t *testing.T) {
	env := newVoidEnv(t)
	reference := "MB-REF-001"
	checkID, paymentID := env.settledTakeawayCheck(t, sales.PaymentMethodManualQR, &reference)
	before := env.paymentFactFor(t, paymentID)
	require.True(t, before.TransactionReference.Valid)

	result := env.voidOK(t, env.voidCommand(paymentID, sales.VoidReasonDuplicatePayment, nil))
	check := env.findCheck(t, result, checkID)
	assert.Equal(t, sales.CheckStateOpen, check.State)
	require.Len(t, check.Payments, 1)
	assert.Equal(t, sales.PaymentMethodManualQR, check.Payments[0].Method)
	require.NotNil(t, check.Payments[0].Void)
	assert.Equal(t, sales.VoidReasonDuplicatePayment, check.Payments[0].Void.Reason)
	assert.Equal(t, 0, env.compCheck(t, checkID).EvidenceCount)
	assert.Equal(t, before, env.paymentFactFor(t, paymentID))
}

// TestVoidPaymentKeepsSettledCheckCovered voids one of two Payments after a
// Comp reduced the charge: the remaining valid Payment still covers what is
// owed, so the Check stays SETTLED with its original evidence and no reopening
// event is written.
func TestVoidPaymentKeepsSettledCheckCovered(t *testing.T) {
	env := newVoidEnv(t)
	session := env.commitDineInDraftWithQuantity(t, 2)
	checkID := env.soleCheckID(t, session.ID)
	for range 2 {
		_, status, err := env.payCash(t, checkID, 25000, 25000)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, status)
	}
	session = env.Submit(t, session.ID)
	require.Len(t, session.PreparationUnits, 2)

	env.compOK(t, env.compCommand(t, env.wasteUnitAfterAdvance(t,
		session.PreparationUnits[0].ID), sales.CompReasonCafeError, nil))

	before := env.compCheck(t, checkID)
	require.Equal(t, sales.CheckStateSettled, before.State)
	require.EqualValues(t, 25000, before.ChargeVND)

	payments := env.paymentIDsForCheck(t, checkID)
	require.Len(t, payments, 2)
	result := env.voidOK(t, env.voidCommand(payments[1], sales.VoidReasonWrongMethod, nil))

	check := env.findCheck(t, result, checkID)
	assert.Equal(t, sales.CheckStateSettled, check.State,
		"remaining valid receipt still covers the reduced charge")
	assert.EqualValues(t, 25000, check.TotalVoidedVND)
	assert.EqualValues(t, 25000, check.EffectiveReceivedVND)
	assert.EqualValues(t, 0, check.BalanceVND)
	assert.EqualValues(t, 0, check.PendingRefundVND)

	after := env.compCheck(t, checkID)
	assert.Equal(t, before.State, after.State)
	assert.Equal(t, before.EvidenceCount, after.EvidenceCount,
		"a still-covered Check keeps its original settlement evidence")
	assert.Equal(t, before.SettledBy, after.SettledBy)
	assert.Equal(t, before.SettledShiftID, after.SettledShiftID)
	assert.Equal(t, before.SettledAt.Time, after.SettledAt.Time)

	assert.Equal(t, 1, env.countAuditEvents(t, sales.EventPaymentVoided))
	assert.Equal(t, 0, env.countAuditEvents(t, sales.EventCheckReopenedAfterPaymentVoid))
}

// TestVoidPaymentAlreadyVoidedRejects proves one Void per Payment: a second
// command conflicts and writes nothing.
func TestVoidPaymentAlreadyVoidedRejects(t *testing.T) {
	env := newVoidEnv(t)
	checkID, paymentID := env.settledTakeawayCheck(t, sales.PaymentMethodCash, nil)
	env.voidOK(t, env.voidCommand(paymentID, sales.VoidReasonWrongAmount, nil))

	status, _, err := env.void(t, env.voidCommand(paymentID, sales.VoidReasonWrongAmount, nil))
	require.ErrorIs(t, err, sales.ErrPaymentAlreadyVoided)
	assert.Equal(t, http.StatusConflict, status)

	assert.Equal(t, 1, env.voidCount(t, paymentID))
	assert.Equal(t, 1, env.countAuditEventsForCheck(t, sales.EventPaymentVoided, checkID))
	assert.Equal(t, 1, env.countAuditEvents(t, sales.EventCheckReopenedAfterPaymentVoid))
}

// TestVoidPaymentReplayReturnsStoredResult proves an exact replay is
// idempotent: the stored projection comes back, and no fact, event, or claim
// is duplicated.
func TestVoidPaymentReplayReturnsStoredResult(t *testing.T) {
	env := newVoidEnv(t)
	checkID, paymentID := env.settledTakeawayCheck(t, sales.PaymentMethodCash, nil)

	cmd := env.voidCommand(paymentID, sales.VoidReasonPaymentRecordedInError, nil)
	first := env.voidOK(t, cmd)
	status, second, err := env.void(t, cmd)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, status)

	firstJSON, err := json.Marshal(first)
	require.NoError(t, err)
	secondJSON, err := json.Marshal(second)
	require.NoError(t, err)
	assert.JSONEq(t, string(firstJSON), string(secondJSON),
		"an exact replay returns the stored result")

	assert.Equal(t, 1, env.voidCount(t, paymentID))
	assert.Equal(t, 1, env.countAuditEventsForCheck(t, sales.EventPaymentVoided, checkID))
	assert.Equal(t, 1, env.idempotencyClaimCount(t, env.Actor, cmd.RequestID))
}

// TestVoidPaymentRejectsRefundAllocation proves a Payment carrying any Refund
// allocation — pending or completed — can never be voided.
func TestVoidPaymentRejectsRefundAllocation(t *testing.T) {
	t.Run("pending Manual QR refund", func(t *testing.T) {
		env := newVoidEnv(t)
		_, _, wasteID, checkID := env.paidTakeawayWastedUnitWithMethod(t,
			sales.PaymentMethodManualQR)
		comp := env.compOK(t, env.compCommand(t, wasteID, sales.CompReasonCafeError, nil))
		paymentID := env.solePaymentIDForCheck(t, checkID)
		env.refundOK(t, env.refundCommand(checkID, sales.RefundMethodManualQR,
			paymentID, comp.Comp.ChargeAdjustmentID, 25000))

		status, _, err := env.void(t, env.voidCommand(paymentID, sales.VoidReasonDuplicatePayment, nil))
		require.ErrorIs(t, err, sales.ErrPaymentHasRefund)
		assert.Equal(t, http.StatusConflict, status)

		assert.Equal(t, 0, env.voidCount(t, paymentID))
		assert.Equal(t, 0, env.countAuditEvents(t, sales.EventPaymentVoided))
		assert.Equal(t, sales.CheckStateSettled, env.compCheck(t, checkID).State)
	})

	t.Run("completed Cash refund", func(t *testing.T) {
		env := newVoidEnv(t)
		_, _, wasteID, checkID := env.paidTakeawayWastedUnitWithMethod(t, sales.PaymentMethodCash)
		comp := env.compOK(t, env.compCommand(t, wasteID, sales.CompReasonCafeError, nil))
		paymentID := env.solePaymentIDForCheck(t, checkID)
		result := env.refundOK(t, env.refundCommand(checkID, sales.RefundMethodCash,
			paymentID, comp.Comp.ChargeAdjustmentID, 25000))
		require.Equal(t, sales.RefundStateCompleted, result.Refund.State)

		status, _, err := env.void(t, env.voidCommand(paymentID, sales.VoidReasonDuplicatePayment, nil))
		require.ErrorIs(t, err, sales.ErrPaymentHasRefund)
		assert.Equal(t, http.StatusConflict, status)

		assert.Equal(t, 0, env.voidCount(t, paymentID))
		assert.Equal(t, 0, env.countAuditEvents(t, sales.EventPaymentVoided))
	})
}

// TestVoidPaymentRejectsMergedCheck proves a Check absorbed by a Merge refuses
// the Void whole. Merge cannot itself carry a Payment, so the fixture inserts
// one directly; the command still revalidates the locked state.
func TestVoidPaymentRejectsMergedCheck(t *testing.T) {
	env := newVoidEnv(t)
	session := env.commitTakeawayDraft(t, 2)
	sourceID := env.soleCheckID(t, session.ID)
	allocations := env.checkAllocations(t, session.ID, sourceID)

	split, _, err := env.splitToNewCheck(t, sourceID, []sales.SplitItem{
		{CommittedItemID: allocations[0].CommittedItemID, Quantity: 1},
	})
	require.NoError(t, err)
	absorbedID := env.otherCheckID(t, split, sourceID)
	_, _, err = env.mergeChecks(t, sourceID, absorbedID)
	require.NoError(t, err)

	paymentID := env.insertCashPayment(t, absorbedID, 25000)

	status, _, err := env.void(t, env.voidCommand(paymentID, sales.VoidReasonWrongAmount, nil))
	require.ErrorIs(t, err, sales.ErrCheckNotOpen)
	assert.Equal(t, http.StatusConflict, status)
	assert.Equal(t, 0, env.voidCount(t, paymentID))
}

// TestVoidPaymentRejectsClosedSession proves Payment Void is a pre-close
// command: a closed Service Session refuses it even though the Shift is open.
func TestVoidPaymentRejectsClosedSession(t *testing.T) {
	env := newVoidEnv(t)
	session := env.StartTakeaway(t)
	env.AddDraftItem(t, session.ID, env.CoffeeID, nil)
	session = env.Commit(t, session.ID)
	checkID := env.soleCheckID(t, session.ID)
	_, status, err := env.payCash(t, checkID, 25000, 25000)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
	session = env.Submit(t, session.ID)
	env.FulfillAll(t, session.ID)
	env.Close(t, session.ID)
	paymentID := env.solePaymentIDForCheck(t, checkID)

	status, _, err = env.void(t, env.voidCommand(paymentID, sales.VoidReasonWrongAmount, nil))
	require.ErrorIs(t, err, sales.ErrServiceSessionClosed)
	assert.Equal(t, http.StatusConflict, status)
	assert.Equal(t, 0, env.voidCount(t, paymentID))
}

// TestVoidPaymentRejectsOriginalShiftClosedOrNotCurrent proves the original
// Payment Shift must still be the currently open one.
func TestVoidPaymentRejectsOriginalShiftClosedOrNotCurrent(t *testing.T) {
	t.Run("original shift closed", func(t *testing.T) {
		env := newVoidEnv(t)
		_, paymentID := env.settledTakeawayCheck(t, sales.PaymentMethodCash, nil)
		env.CloseShift(t)

		status, _, err := env.void(t, env.voidCommand(paymentID, sales.VoidReasonWrongAmount, nil))
		require.ErrorIs(t, err, sales.ErrPaymentVoidShiftClosed)
		assert.Equal(t, http.StatusConflict, status)
		assert.Equal(t, 0, env.voidCount(t, paymentID))
	})

	t.Run("original shift is no longer the open one", func(t *testing.T) {
		env := newVoidEnv(t)
		_, paymentID := env.settledTakeawayCheck(t, sales.PaymentMethodCash, nil)
		env.CloseShift(t)
		secondShift := seedOpenShift(t, env.Queries, env.Actor.StaffID)
		require.NotEqual(t, env.ShiftID, secondShift)

		status, _, err := env.void(t, env.voidCommand(paymentID, sales.VoidReasonWrongAmount, nil))
		require.ErrorIs(t, err, sales.ErrPaymentVoidShiftClosed)
		assert.Equal(t, http.StatusConflict, status)
		assert.Equal(t, 0, env.voidCount(t, paymentID))
	})
}

// TestVoidPaymentUnknownPaymentRejects proves a missing Payment is a not-found
// answer, not a defect.
func TestVoidPaymentUnknownPaymentRejects(t *testing.T) {
	env := newVoidEnv(t)

	unknown := uuid.New()
	status, _, err := env.void(t, env.voidCommand(unknown, sales.VoidReasonWrongAmount, nil))
	require.ErrorIs(t, err, sales.ErrPaymentNotFound)
	assert.Equal(t, http.StatusNotFound, status)
	assert.Equal(t, 0, env.voidCount(t, unknown))
}

// TestVoidPaymentAuthorization covers the one Manager Approval, the
// initiator's own capability, self-approval, separate recording, replay, and
// credential hygiene.
func TestVoidPaymentAuthorization(t *testing.T) {
	t.Run("denies a wrong manager pin with committed denial evidence", func(t *testing.T) {
		env := newVoidEnv(t)
		checkID, paymentID := env.settledTakeawayCheck(t, sales.PaymentMethodCash, nil)

		cmd := env.voidCommand(paymentID, sales.VoidReasonWrongAmount, nil)
		cmd.ManagerApproval.ManagerPIN = "00000000"
		status, _, err := env.void(t, cmd)
		require.ErrorIs(t, err, sales.ErrForbidden)
		assert.Equal(t, http.StatusForbidden, status)

		assert.Equal(t, 0, env.voidCount(t, paymentID))
		assert.Equal(t, sales.CheckStateSettled, env.compCheck(t, checkID).State)
		assert.Equal(t, 1, env.countAuditEvents(t, sales.EventAuthorizationDenied))
		assert.Equal(t, 0, env.idempotencyClaimCount(t, env.Actor, cmd.RequestID),
			"a denied approval must not consume the request id")
	})

	t.Run("denies an approver without the manager role", func(t *testing.T) {
		env := newVoidEnv(t)
		_, paymentID := env.settledTakeawayCheck(t, sales.PaymentMethodCash, nil)
		cashier := env.newCashierActor(t)

		cmd := env.voidCommand(paymentID, sales.VoidReasonWrongAmount, nil)
		cmd.ManagerApproval = env.approvalFor(t, cashier)
		status, _, err := env.void(t, cmd)
		require.ErrorIs(t, err, sales.ErrForbidden)
		assert.Equal(t, http.StatusForbidden, status)
		assert.Equal(t, 0, env.voidCount(t, paymentID))
	})

	t.Run("denies an initiator without sales.operate", func(t *testing.T) {
		env := newVoidEnv(t)
		_, paymentID := env.settledTakeawayCheck(t, sales.PaymentMethodCash, nil)

		cmd := env.voidCommand(paymentID, sales.VoidReasonWrongAmount, nil)
		status, _, err := env.voidAs(t, env.BaristaActor(), cmd)
		require.ErrorIs(t, err, sales.ErrForbidden)
		assert.Equal(t, http.StatusForbidden, status)
		assert.Equal(t, 0, env.voidCount(t, paymentID))
	})

	t.Run("records a cashier initiator and manager approver separately", func(t *testing.T) {
		env := newVoidEnv(t)
		_, paymentID := env.settledTakeawayCheck(t, sales.PaymentMethodCash, nil)
		cashier := env.newCashierActor(t)

		cmd := env.voidCommand(paymentID, sales.VoidReasonWrongAmount, nil)
		status, result, err := env.voidAs(t, cashier, cmd)
		require.NoError(t, err)
		require.Equal(t, http.StatusCreated, status)

		fact := env.voidFactFor(t, paymentID)
		assert.Equal(t, cashier.StaffID, fact.Actor)
		assert.Equal(t, env.Actor.StaffID, fact.Approved)
		require.Len(t, result.Checks, 1)
		require.Len(t, result.Checks[0].Payments, 1)
		require.NotNil(t, result.Checks[0].Payments[0].Void)
		assert.Equal(t, cashier.StaffID, result.Checks[0].Payments[0].Void.ActorStaffIdentityID)
		assert.Equal(t, env.Actor.StaffID, result.Checks[0].Payments[0].Void.ApprovedByStaffIdentityID)
	})

	t.Run("replay is denied once the approver is disabled", func(t *testing.T) {
		env := newVoidEnv(t)
		_, paymentID := env.settledTakeawayCheck(t, sales.PaymentMethodCash, nil)
		cashier := env.newCashierActor(t)

		cmd := env.voidCommand(paymentID, sales.VoidReasonWrongAmount, nil)
		status, first, err := env.voidAs(t, cashier, cmd)
		require.NoError(t, err)
		require.Equal(t, http.StatusCreated, status)

		_, err = env.DB.Exec(
			`UPDATE staff_identities SET enabled = false WHERE id = $1`, env.Actor.StaffID)
		require.NoError(t, err)

		status, _, err = env.voidAs(t, cashier, cmd)
		require.ErrorIs(t, err, sales.ErrForbidden)
		assert.Equal(t, http.StatusForbidden, status,
			"a replay still requires a currently valid Manager Approval")

		var storedCode int
		require.NoError(t, env.DB.QueryRow(
			`SELECT response_code FROM idempotency_keys WHERE actor_id = $1 AND key = $2`,
			cashier.StaffID, cmd.RequestID).Scan(&storedCode))
		assert.Equal(t, http.StatusCreated, storedCode)
		assert.Equal(t, 1, env.voidCount(t, paymentID))
		assert.Equal(t, first.Checks[0].Payments[0].Void.ID, env.voidFactFor(t, paymentID).ID)
	})

	t.Run("keeps credentials out of storage, results, and audits", func(t *testing.T) {
		env := newVoidEnv(t)
		checkID, paymentID := env.settledTakeawayCheck(t, sales.PaymentMethodCash, nil)
		code := env.loginCode(t, env.Actor)

		cmd := env.voidCommand(paymentID, sales.VoidReasonWrongAmount, nil)
		env.voidOK(t, cmd)

		stored := env.storedResultBody(t, env.Actor, cmd.RequestID)
		assert.NotContains(t, stored, `"manager_pin"`)
		assert.NotContains(t, stored, `"approver_login_code"`)
		assert.NotContains(t, stored, code)
		assert.NotContains(t, stored, `"1234"`)

		for _, eventType := range []string{
			sales.EventPaymentVoided,
			sales.EventCheckReopenedAfterPaymentVoid,
		} {
			var details []byte
			require.NoError(t, env.DB.QueryRow(`
				SELECT details FROM audit_events
				WHERE event_type = $1 AND details->>'check_id' = $2
				ORDER BY occurred_at DESC, id DESC LIMIT 1`,
				eventType, checkID.String()).Scan(&details))
			text := string(details)
			assert.NotContains(t, text, "manager_pin")
			assert.NotContains(t, text, "approver_login_code")
			assert.NotContains(t, text, code)
			assert.NotContains(t, text, `"1234"`)
		}
	})
}

// TestVoidPaymentAuditDetails pins the business meaning each audit records.
func TestVoidPaymentAuditDetails(t *testing.T) {
	env := newVoidEnv(t)
	checkID, paymentID := env.settledTakeawayCheck(t, sales.PaymentMethodCash, nil)
	note := "khách quẹt nhầm"

	status, result, err := env.void(t,
		env.voidCommand(paymentID, sales.VoidReasonDuplicatePayment, &note))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, status)
	voidID := result.Checks[0].Payments[0].Void.ID

	voided := env.voidAuditDetails(t, sales.EventPaymentVoided, "payment_id", paymentID.String())
	assert.Equal(t, voidID.String(), voided["payment_void_id"])
	assert.Equal(t, checkID.String(), voided["check_id"])
	assert.Equal(t, env.ShiftID.String(), voided["sales_shift_id"])
	assert.Equal(t, sales.PaymentMethodCash, voided["method"])
	assert.EqualValues(t, 25000, voided["amount_vnd"])
	assert.Equal(t, sales.VoidReasonDuplicatePayment, voided["reason"])
	assert.Equal(t, note, voided["note"])
	assert.Equal(t, env.Actor.StaffID.String(), voided["actor_staff_identity_id"])
	assert.Equal(t, env.Actor.StaffID.String(), voided["approved_by_staff_identity_id"])

	reopened := env.voidAuditDetails(t, sales.EventCheckReopenedAfterPaymentVoid,
		"check_id", checkID.String())
	assert.Equal(t, voidID.String(), reopened["payment_void_id"])
	assert.Equal(t, paymentID.String(), reopened["payment_id"])
	assert.Equal(t, "SETTLED", reopened["prior_state"])
	assert.Equal(t, "OPEN", reopened["resulting_state"])
	assert.EqualValues(t, 25000, reopened["charge_vnd"])
	assert.EqualValues(t, 25000, reopened["balance_vnd"])
	assert.EqualValues(t, 25000, reopened["voided_amount_vnd"])
}

// TestVoidPaymentInjectedFailures proves every failure inside the transaction
// — including the conditional reopening event and the executor's stored
// result — rolls back the whole command.
func TestVoidPaymentInjectedFailures(t *testing.T) {
	t.Run("void fact insert failure", func(t *testing.T) {
		env := newVoidEnv(t)
		checkID, paymentID := env.settledTakeawayCheck(t, sales.PaymentMethodCash, nil)
		beforeCheck := env.compCheck(t, checkID)
		beforePayment := env.paymentFactFor(t, paymentID)
		installVoidTrigger(t, env, "fail_void_insert", `
			CREATE FUNCTION fail_void_insert() RETURNS trigger AS $$
			BEGIN
			    IF NEW.payment_id = '`+paymentID.String()+`'::uuid THEN
			        RAISE EXCEPTION 'forced void insert failure';
			    END IF;
			    RETURN NEW;
			END;
			$$ LANGUAGE plpgsql;
			CREATE TRIGGER fail_void_insert
			BEFORE INSERT ON payment_voids
			FOR EACH ROW EXECUTE FUNCTION fail_void_insert();`, "payment_voids")

		cmd := env.voidCommand(paymentID, sales.VoidReasonWrongAmount, nil)
		status, _, err := env.void(t, cmd)
		require.Error(t, err)
		require.Equal(t, http.StatusInternalServerError, status)
		assertVoidRolledBack(t, env, paymentID, checkID, beforePayment, beforeCheck, cmd.RequestID)
	})

	t.Run("check reopen failure", func(t *testing.T) {
		env := newVoidEnv(t)
		checkID, paymentID := env.settledTakeawayCheck(t, sales.PaymentMethodCash, nil)
		beforeCheck := env.compCheck(t, checkID)
		beforePayment := env.paymentFactFor(t, paymentID)
		installVoidTrigger(t, env, "fail_void_reopen", `
			CREATE FUNCTION fail_void_reopen() RETURNS trigger AS $$
			BEGIN
			    IF OLD.state = 'SETTLED' AND NEW.state = 'OPEN' THEN
			        RAISE EXCEPTION 'forced check reopen failure';
			    END IF;
			    RETURN NEW;
			END;
			$$ LANGUAGE plpgsql;
			CREATE TRIGGER fail_void_reopen
			BEFORE UPDATE ON checks
			FOR EACH ROW EXECUTE FUNCTION fail_void_reopen();`, "checks")

		cmd := env.voidCommand(paymentID, sales.VoidReasonWrongAmount, nil)
		status, _, err := env.void(t, cmd)
		require.Error(t, err)
		require.Equal(t, http.StatusInternalServerError, status)
		assertVoidRolledBack(t, env, paymentID, checkID, beforePayment, beforeCheck, cmd.RequestID)
	})

	t.Run("reopening audit failure", func(t *testing.T) {
		env := newVoidEnv(t)
		checkID, paymentID := env.settledTakeawayCheck(t, sales.PaymentMethodCash, nil)
		beforeCheck := env.compCheck(t, checkID)
		beforePayment := env.paymentFactFor(t, paymentID)
		installVoidTrigger(t, env, "fail_void_reopen_audit", `
			CREATE FUNCTION fail_void_reopen_audit() RETURNS trigger AS $$
			BEGIN
			    IF NEW.event_type = 'CHECK_REOPENED_AFTER_PAYMENT_VOID' THEN
			        RAISE EXCEPTION 'forced reopening audit failure';
			    END IF;
			    RETURN NEW;
			END;
			$$ LANGUAGE plpgsql;
			CREATE TRIGGER fail_void_reopen_audit
			BEFORE INSERT ON audit_events
			FOR EACH ROW EXECUTE FUNCTION fail_void_reopen_audit();`, "audit_events")

		cmd := env.voidCommand(paymentID, sales.VoidReasonWrongAmount, nil)
		status, _, err := env.void(t, cmd)
		require.Error(t, err)
		require.Equal(t, http.StatusInternalServerError, status)
		assertVoidRolledBack(t, env, paymentID, checkID, beforePayment, beforeCheck, cmd.RequestID)
	})

	t.Run("payment voided audit failure", func(t *testing.T) {
		env := newVoidEnv(t)
		checkID, paymentID := env.settledTakeawayCheck(t, sales.PaymentMethodCash, nil)
		beforeCheck := env.compCheck(t, checkID)
		beforePayment := env.paymentFactFor(t, paymentID)
		installVoidTrigger(t, env, "fail_void_audit", `
			CREATE FUNCTION fail_void_audit() RETURNS trigger AS $$
			BEGIN
			    IF NEW.event_type = 'PAYMENT_VOIDED' THEN
			        RAISE EXCEPTION 'forced void audit failure';
			    END IF;
			    RETURN NEW;
			END;
			$$ LANGUAGE plpgsql;
			CREATE TRIGGER fail_void_audit
			BEFORE INSERT ON audit_events
			FOR EACH ROW EXECUTE FUNCTION fail_void_audit();`, "audit_events")

		cmd := env.voidCommand(paymentID, sales.VoidReasonWrongAmount, nil)
		status, _, err := env.void(t, cmd)
		require.Error(t, err)
		require.Equal(t, http.StatusInternalServerError, status)
		assertVoidRolledBack(t, env, paymentID, checkID, beforePayment, beforeCheck, cmd.RequestID)
	})

	t.Run("result storage failure", func(t *testing.T) {
		env := newVoidEnv(t)
		checkID, paymentID := env.settledTakeawayCheck(t, sales.PaymentMethodCash, nil)
		beforeCheck := env.compCheck(t, checkID)
		beforePayment := env.paymentFactFor(t, paymentID)
		installVoidTrigger(t, env, "fail_void_result_store", `
			CREATE FUNCTION fail_void_result_store() RETURNS trigger AS $$
			BEGIN
			    IF NEW.action = 'sales.void_payment' AND NEW.response_code <> 0 THEN
			        RAISE EXCEPTION 'forced void result store failure';
			    END IF;
			    RETURN NEW;
			END;
			$$ LANGUAGE plpgsql;
			CREATE TRIGGER fail_void_result_store
			BEFORE UPDATE ON idempotency_keys
			FOR EACH ROW EXECUTE FUNCTION fail_void_result_store();`, "idempotency_keys")

		cmd := env.voidCommand(paymentID, sales.VoidReasonWrongAmount, nil)
		status, _, err := env.void(t, cmd)
		require.Error(t, err)
		require.Equal(t, http.StatusInternalServerError, status)
		assertVoidRolledBack(t, env, paymentID, checkID, beforePayment, beforeCheck, cmd.RequestID)
	})
}

// TestSalesHTTPVoidPayment drives the route end to end and pins its statuses,
// envelope, authorization, and credential-free body.
func TestSalesHTTPVoidPayment(t *testing.T) {
	e, db, q := newTestServer(t)
	managerToken, managerCode := signInReturningLoginCode(t, e, q, []string{"MANAGER"}, "1357")
	baristaToken, _ := signInReturningLoginCode(t, e, q, []string{"BARISTA"}, "2468")
	_ = openShiftOverHTTP(t, e, managerToken)
	itemID := seedMenuItem(t, db, "Cà phê void", 25000)

	session := openTakeawayOverHTTP(t, e, managerToken)
	body, _ := json.Marshal(map[string]any{"request_id": uuid.New(), "menu_item_id": itemID})
	rec := doRequest(t, e, http.MethodPost,
		"/api/v1/sales/service-sessions/"+session.ID.String()+"/draft/items", managerToken, body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	session = decodeSession(t, rec)

	body, _ = json.Marshal(map[string]any{"request_id": uuid.New()})
	rec = doRequest(t, e, http.MethodPost,
		"/api/v1/sales/service-sessions/"+session.ID.String()+"/draft/commit", managerToken, body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	session = decodeSession(t, rec)
	require.Len(t, session.Checks, 1)
	checkID := session.Checks[0].ID

	body, _ = json.Marshal(map[string]any{
		"request_id": uuid.New(), "applied_amount_vnd": 25000, "cash_tendered_vnd": 25000,
	})
	rec = doRequest(t, e, http.MethodPost,
		"/api/v1/sales/checks/"+checkID.String()+"/payments/cash", managerToken, body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	session = decodeSession(t, rec)
	require.Len(t, session.Checks[0].Payments, 1)
	paymentID := session.Checks[0].Payments[0].ID

	voidPath := "/api/v1/sales/payments/" + paymentID.String() + "/void"
	approval := map[string]any{"approver_login_code": managerCode, "manager_pin": "1357"}
	voidBody := func(requestID uuid.UUID, reason string, approval map[string]any) []byte {
		body, _ := json.Marshal(map[string]any{
			"request_id": requestID, "reason": reason, "manager_approval": approval,
		})
		return body
	}

	t.Run("voids and returns 201", func(t *testing.T) {
		requestID := uuid.New()
		rec := doRequest(t, e, http.MethodPost, voidPath, managerToken,
			voidBody(requestID, "WRONG_AMOUNT", approval))
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

		var env envelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
		require.True(t, env.Success)
		var result sales.ServiceSessionResponse
		require.NoError(t, json.Unmarshal(env.Data, &result))
		require.Len(t, result.Checks, 1)
		assert.Equal(t, sales.CheckStateOpen, result.Checks[0].State)
		assert.EqualValues(t, 25000, result.Checks[0].BalanceVND)
		require.Len(t, result.Checks[0].Payments, 1)
		require.NotNil(t, result.Checks[0].Payments[0].Void)
		assert.EqualValues(t, 25000, result.Checks[0].Payments[0].Void.AmountVND)

		raw := rec.Body.String()
		assert.Contains(t, raw, `"void":{`)
		assert.NotContains(t, raw, `"manager_pin"`)
		assert.NotContains(t, raw, `"approver_login_code"`)
		assert.NotContains(t, raw, `"1357"`)
	})

	t.Run("second void with a new request id answers 409", func(t *testing.T) {
		rec := doRequest(t, e, http.MethodPost, voidPath, managerToken,
			voidBody(uuid.New(), "WRONG_AMOUNT", approval))
		require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())

		var env envelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
		require.NotNil(t, env.Error)
		assert.Equal(t, "PAYMENT_ALREADY_VOIDED", env.Error.Code)
	})

	t.Run("invalid reason answers 400", func(t *testing.T) {
		rec := doRequest(t, e, http.MethodPost, voidPath, managerToken,
			voidBody(uuid.New(), "BROKEN", approval))
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		var env envelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
		require.NotNil(t, env.Error)
		assert.Equal(t, "INVALID_INPUT", env.Error.Code)
	})

	t.Run("missing request id answers 400", func(t *testing.T) {
		rec := doRequest(t, e, http.MethodPost, voidPath, managerToken,
			[]byte(`{"reason":"WRONG_AMOUNT","manager_approval":{"approver_login_code":"`+
				managerCode+`","manager_pin":"1357"}}`))
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	})

	t.Run("malformed manager approval answers 400", func(t *testing.T) {
		rec := doRequest(t, e, http.MethodPost, voidPath, managerToken,
			voidBody(uuid.New(), "WRONG_AMOUNT", map[string]any{}))
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		var env envelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
		require.NotNil(t, env.Error)
		assert.Equal(t, "INVALID_INPUT", env.Error.Code)
	})

	t.Run("unknown payment answers 404", func(t *testing.T) {
		rec := doRequest(t, e, http.MethodPost,
			"/api/v1/sales/payments/"+uuid.NewString()+"/void", managerToken,
			voidBody(uuid.New(), "WRONG_AMOUNT", approval))
		require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
		var env envelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
		require.NotNil(t, env.Error)
		assert.Equal(t, "PAYMENT_NOT_FOUND", env.Error.Code)
	})

	t.Run("wrong manager pin answers 403", func(t *testing.T) {
		rec := doRequest(t, e, http.MethodPost, voidPath, managerToken,
			voidBody(uuid.New(), "WRONG_AMOUNT", map[string]any{
				"approver_login_code": managerCode,
				"manager_pin":         "00000000",
			}))
		require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
		var env envelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
		require.NotNil(t, env.Error)
		assert.Equal(t, "NOT_AUTHORIZED", env.Error.Code)
	})

	t.Run("barista is denied by capability", func(t *testing.T) {
		rec := doRequest(t, e, http.MethodPost, voidPath, baristaToken,
			voidBody(uuid.New(), "WRONG_AMOUNT", approval))
		require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
		var env envelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
		require.NotNil(t, env.Error)
		assert.Equal(t, "FORBIDDEN", env.Error.Code)
	})

	t.Run("anonymous is denied by the middleware", func(t *testing.T) {
		rec := doRequest(t, e, http.MethodPost, voidPath, "",
			voidBody(uuid.New(), "WRONG_AMOUNT", approval))
		require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
	})
}
