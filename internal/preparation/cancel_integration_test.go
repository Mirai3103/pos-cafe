//go:build integration

package preparation_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/preparation"
	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cancelEnv is the fixture world for the Cancellation/Change workflow suites:
// the shared prepEnv plus the cancellation command helpers and the direct
// reads the financial assertions need.
type cancelEnv struct{ *prepEnv }

func newCancelEnv(t *testing.T) *cancelEnv {
	t.Helper()
	return &cancelEnv{prepEnv: newPrepEnv(t)}
}

// cancel runs the cancellation command under a caller-chosen request id as the
// given actor, deriving the status the HTTP layer would have answered with on
// error the way the other Phase 6 helpers do.
func (e *cancelEnv) cancel(t *testing.T, requestID uuid.UUID, actor preparation.Actor,
	cmd preparation.CancelUnitsCommand,
) (preparation.CancelUnitsResponse, int, error) {
	t.Helper()
	if cmd.RequestID == uuid.Nil {
		cmd.RequestID = requestID
	}
	status, resp, err := preparation.NewCancelUnitsHandler(e.PreparationRunner).
		Handle(context.Background(), actor, cmd)
	if err != nil {
		status, _ = preparation.ErrorResponse(err)
	}
	return resp, status, err
}

// Cancel runs the command as the seeded Cashier, who holds sales.operate but
// not preparation.operate — the capability the route demands.
func (e *cancelEnv) Cancel(t *testing.T, cmd preparation.CancelUnitsCommand) (
	preparation.CancelUnitsResponse, int, error,
) {
	t.Helper()
	return e.cancel(t, uuid.New(), e.CashierActor(), cmd)
}

// CancelAs runs the command as an arbitrary actor, for capability tests.
func (e *cancelEnv) CancelAs(t *testing.T, actor preparation.Actor,
	cmd preparation.CancelUnitsCommand,
) (preparation.CancelUnitsResponse, int, error) {
	t.Helper()
	return e.cancel(t, uuid.New(), actor, cmd)
}

// CancelWithRequestID replays a specific request id, as the Cashier.
func (e *cancelEnv) CancelWithRequestID(t *testing.T, requestID uuid.UUID,
	cmd preparation.CancelUnitsCommand,
) (preparation.CancelUnitsResponse, int, error) {
	t.Helper()
	return e.cancel(t, requestID, e.CashierActor(), cmd)
}

// cancelCommand builds a Cancellation command for the given selection. The
// request id is left zero so the helper running the command fills it — either
// fresh or with the explicit replay id.
func cancelCommand(ids []uuid.UUID, kind, reason string) preparation.CancelUnitsCommand {
	return preparation.CancelUnitsCommand{
		PreparationUnitIDs: ids,
		Kind:               kind,
		Reason:             reason,
	}
}

// cancelCheckRow is the financial-and-settlement slice of one Check as the
// cancellation suites assert it.
type cancelCheckRow struct {
	ID             uuid.UUID
	State          string
	ChargeVND      int64
	SettledAt      *time.Time
	SettledBy      *uuid.UUID
	SettledShiftID *uuid.UUID
	SettledSession *uuid.UUID
}

func (e *cancelEnv) CheckForUnit(t *testing.T, unitID uuid.UUID) cancelCheckRow {
	t.Helper()
	var row cancelCheckRow
	var settledAt sql.NullTime
	var by, shift, session uuid.NullUUID
	require.NoError(t, e.DB.QueryRow(`
		SELECT c.id, c.state, c.charge_vnd, c.settled_at,
		       c.settled_by_staff_identity_id,
		       c.settled_during_sales_shift_id,
		       c.settled_staff_access_session_id
		FROM preparation_units AS pu
		JOIN order_items AS oi ON oi.id = pu.order_item_id
		JOIN charge_allocations AS ca ON ca.committed_item_id = oi.committed_item_id
		JOIN checks AS c ON c.id = ca.check_id
		WHERE pu.id = $1
		ORDER BY ca.created_at ASC, ca.id ASC
		LIMIT 1`, unitID).
		Scan(&row.ID, &row.State, &row.ChargeVND, &settledAt, &by, &shift, &session))
	if settledAt.Valid {
		row.SettledAt = &settledAt.Time
	}
	if by.Valid {
		row.SettledBy = &by.UUID
	}
	if shift.Valid {
		row.SettledShiftID = &shift.UUID
	}
	if session.Valid {
		row.SettledSession = &session.UUID
	}
	return row
}

// OrderIDForUnit resolves the Order that submitted the unit's Order Item.
func (e *cancelEnv) OrderIDForUnit(t *testing.T, unitID uuid.UUID) uuid.UUID {
	t.Helper()
	var orderID uuid.UUID
	require.NoError(t, e.DB.QueryRow(`
		SELECT oi.order_id
		FROM preparation_units AS pu
		JOIN order_items AS oi ON oi.id = pu.order_item_id
		WHERE pu.id = $1`, unitID).Scan(&orderID))
	return orderID
}

// OrderIDsForSession lists a Session's submitted Orders oldest first.
func (e *cancelEnv) OrderIDsForSession(t *testing.T, sessionID uuid.UUID) []uuid.UUID {
	t.Helper()
	rows, err := e.DB.Query(`
		SELECT id FROM orders
		WHERE service_session_id = $1
		ORDER BY submitted_at ASC, id ASC`, sessionID)
	require.NoError(t, err)
	defer rows.Close()
	ids := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		require.NoError(t, rows.Scan(&id))
		ids = append(ids, id)
	}
	require.NoError(t, rows.Err())
	return ids
}

// SubmitAnotherRound submits one more round of the given quantity into an
// existing session through the real Sales handlers, so Change suites have two
// Orders in one Session.
func (e *cancelEnv) SubmitAnotherRound(t *testing.T, sessionID uuid.UUID,
	quantity int32,
) []sales.PreparationUnitResponse {
	t.Helper()
	ctx := context.Background()
	actor := e.salesActor(e.manager)

	// The Submit response projects the whole Session; snapshot the units that
	// existed before so the helper returns only the new round's units.
	before, err := sales.LoadServiceSession(ctx, e.Queries, sessionID)
	require.NoError(t, err)
	existing := make(map[uuid.UUID]struct{}, len(before.PreparationUnits))
	for _, unit := range before.PreparationUnits {
		existing[unit.ID] = struct{}{}
	}

	_, _, err = sales.NewStartNewOrderDraftHandler(e.SalesRunner).
		Handle(ctx, actor, sales.StartNewOrderDraftCommand{
			RequestID:        uuid.New(),
			ServiceSessionID: sessionID,
		})
	require.NoError(t, err)

	_, resp, err := sales.NewAddDraftItemHandler(e.SalesRunner).
		Handle(ctx, actor, sales.AddDraftItemCommand{
			RequestID:        uuid.New(),
			ServiceSessionID: sessionID,
			MenuItemID:       e.CoffeeID,
		})
	require.NoError(t, err)
	require.NotEmpty(t, resp.Draft.Items)

	_, _, err = sales.NewSetDraftItemQuantityHandler(e.SalesRunner).
		Handle(ctx, actor, sales.SetDraftItemQuantityCommand{
			RequestID:        uuid.New(),
			ServiceSessionID: sessionID,
			DraftItemID:      resp.Draft.Items[0].ID,
			Quantity:         &quantity,
		})
	require.NoError(t, err)

	_, _, err = sales.NewCommitOrderDraftHandler(e.SalesRunner).
		Handle(ctx, actor, sales.CommitOrderDraftCommand{
			RequestID:        uuid.New(),
			ServiceSessionID: sessionID,
		})
	require.NoError(t, err)

	_, submitted, err := sales.NewSubmitOrderHandler(e.SalesRunner).
		Handle(ctx, actor, sales.SubmitOrderCommand{
			RequestID:        uuid.New(),
			ServiceSessionID: sessionID,
		})
	require.NoError(t, err)

	newUnits := make([]sales.PreparationUnitResponse, 0, quantity)
	for _, unit := range submitted.PreparationUnits {
		if _, seen := existing[unit.ID]; !seen {
			newUnits = append(newUnits, unit)
		}
	}
	require.Len(t, newUnits, int(quantity), "only the new round's units are returned")
	return newUnits
}

// PayCheck applies a Cash Payment to a Check through the real Sales handler.
func (e *cancelEnv) PayCheck(t *testing.T, checkID uuid.UUID, amountVND int64) {
	t.Helper()
	_, _, err := sales.NewPayCashHandler(e.SalesRunner).Handle(
		context.Background(), e.salesActor(e.manager), sales.PayCashCommand{
			RequestID:        uuid.New(),
			CheckID:          checkID,
			AppliedAmountVND: amountVND,
			CashTenderedVND:  amountVND,
		})
	require.NoError(t, err)
}

// Projection reads a Session through the real Sales projection.
func (e *cancelEnv) Projection(t *testing.T, sessionID uuid.UUID) sales.ServiceSessionResponse {
	t.Helper()
	projection, err := sales.LoadServiceSession(context.Background(), e.Queries, sessionID)
	require.NoError(t, err)
	return projection
}

// ProjectedCheck finds one Check inside a Session projection.
func (e *cancelEnv) ProjectedCheck(t *testing.T, sessionID, checkID uuid.UUID) sales.CheckResponse {
	t.Helper()
	projection := e.Projection(t, sessionID)
	for _, check := range projection.Checks {
		if check.ID == checkID {
			return check
		}
	}
	require.FailNow(t, "check not found in session projection", checkID)
	return sales.CheckResponse{}
}

// CountCancellations counts the Cancellation facts recorded for one unit.
func (e *cancelEnv) CountCancellations(t *testing.T, unitID uuid.UUID) int {
	t.Helper()
	var n int
	require.NoError(t, e.DB.QueryRow(
		`SELECT count(*) FROM preparation_cancellations WHERE preparation_unit_id = $1`,
		unitID).Scan(&n))
	return n
}

// CountAdjustments counts the Charge Adjustments recorded for one unit.
func (e *cancelEnv) CountAdjustments(t *testing.T, unitID uuid.UUID) int {
	t.Helper()
	var n int
	require.NoError(t, e.DB.QueryRow(
		`SELECT count(*) FROM charge_adjustments WHERE preparation_unit_id = $1`,
		unitID).Scan(&n))
	return n
}

// CountAdjustmentsForCheck counts the live Charge Adjustments of one Check.
func (e *cancelEnv) CountAdjustmentsForCheck(t *testing.T, checkID uuid.UUID) int {
	t.Helper()
	var n int
	require.NoError(t, e.DB.QueryRow(
		`SELECT count(*) FROM charge_adjustments WHERE check_id = $1`, checkID).Scan(&n))
	return n
}

// cancellationFactRow is one stored Cancellation fact as the suites read it.
type cancellationFactRow struct {
	ID                 uuid.UUID
	Kind               string
	ChargeAdjustmentID *uuid.UUID
	ReplacementOrderID *uuid.UUID
	Reason             string
	Note               *string
	OccurredAt         time.Time
	ActorID            uuid.UUID
	SessionID          uuid.UUID
}

func (e *cancelEnv) UnitCancellation(t *testing.T, unitID uuid.UUID) cancellationFactRow {
	t.Helper()
	var row cancellationFactRow
	var adjustment, replacement uuid.NullUUID
	var note sql.NullString
	require.NoError(t, e.DB.QueryRow(`
		SELECT id, kind, charge_adjustment_id, replacement_order_id, reason, note,
		       occurred_at, actor_staff_identity_id, staff_access_session_id
		FROM preparation_cancellations
		WHERE preparation_unit_id = $1`, unitID).
		Scan(&row.ID, &row.Kind, &adjustment, &replacement, &row.Reason, &note,
			&row.OccurredAt, &row.ActorID, &row.SessionID))
	if adjustment.Valid {
		row.ChargeAdjustmentID = &adjustment.UUID
	}
	if replacement.Valid {
		row.ReplacementOrderID = &replacement.UUID
	}
	if note.Valid {
		row.Note = &note.String
	}
	return row
}

// adjustmentFactRow is one stored Charge Adjustment as the suites read it.
type adjustmentFactRow struct {
	ID                 uuid.UUID
	Kind               string
	Scope              string
	ChargeAllocationID uuid.UUID
	CheckID            uuid.UUID
	AmountVND          int64
	CreatedAt          time.Time
}

func (e *cancelEnv) UnitAdjustment(t *testing.T, unitID uuid.UUID) adjustmentFactRow {
	t.Helper()
	var row adjustmentFactRow
	require.NoError(t, e.DB.QueryRow(`
		SELECT id, kind, scope, charge_allocation_id, check_id, amount_vnd, created_at
		FROM charge_adjustments
		WHERE preparation_unit_id = $1`, unitID).
		Scan(&row.ID, &row.Kind, &row.Scope, &row.ChargeAllocationID, &row.CheckID,
			&row.AmountVND, &row.CreatedAt))
	return row
}

// countCancelWrites totals every row a cancellation could create, so a
// rejected batch can prove it wrote nothing at all.
func (e *cancelEnv) countCancelWrites(t *testing.T) int {
	t.Helper()
	var n int
	require.NoError(t, e.DB.QueryRow(`
		SELECT (SELECT count(*) FROM charge_adjustments)
		     + (SELECT count(*) FROM preparation_cancellations)
		     + (SELECT count(*) FROM preparation_alerts)
		     + (SELECT count(*) FROM preparation_unit_transitions)
		     + (SELECT count(*) FROM idempotency_keys)`).Scan(&n))
	return n
}

// TestCancelUnitsUnpaidChargedUnitSettles cancels one charged queued unit of an
// unpaid Check: the charge falls by the immutable unit price, the Check
// settles with evidence, and every fact commits with the batch.
func TestCancelUnitsUnpaidChargedUnitSettles(t *testing.T) {
	env := newCancelEnv(t)
	unit := env.SubmittedUnits(t, 1)[0]
	sessionID := env.SessionIDForUnit(t, unit.ID)
	check := env.CheckForUnit(t, unit.ID)
	require.Equal(t, sales.CheckStateOpen, check.State)
	require.EqualValues(t, 25000, check.ChargeVND)

	resp, status, err := env.Cancel(t, cancelCommand(
		[]uuid.UUID{unit.ID}, preparation.CancelKindCancellation, preparation.ReasonCustomerRequest))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)

	require.Len(t, resp.Outcomes, 1)
	outcome := resp.Outcomes[0]
	assert.Equal(t, unit.ID, outcome.PreparationUnitID)
	assert.Equal(t, preparation.StateQueued, outcome.PriorState)
	assert.Equal(t, preparation.StateCancelled, outcome.ResultingState)
	require.NotNil(t, outcome.ChargeAdjustmentID)
	assert.EqualValues(t, 25000, outcome.ChargeRemovedVND)

	require.Len(t, resp.Alerts, 1)
	alert := resp.Alerts[0]
	assert.Equal(t, preparation.AlertKindCancellation, alert.Kind)
	assert.Equal(t, unit.ID, alert.PreparationUnitID)
	assert.Equal(t, preparation.ReasonCustomerRequest, alert.Reason)
	assert.Nil(t, alert.WasteID)
	assert.Nil(t, alert.AcknowledgedAt)
	assert.Equal(t, int32(1), alert.UnitNumber)
	assert.Equal(t, unit.ServiceNumber, alert.ServiceNumber)
	assert.Equal(t, unit.ItemName, alert.ItemName)

	// Unit, transition, fact, and alert all commit with the adjustment.
	assert.Equal(t, preparation.StateCancelled, env.UnitState(t, unit.ID))
	transitions := env.CountTransitionsTo(t, unit.ID, preparation.StateCancelled)
	assert.Equal(t, 1, transitions)
	require.Equal(t, 1, env.CountCancellations(t, unit.ID))
	fact := env.UnitCancellation(t, unit.ID)
	assert.Equal(t, preparation.CancelKindCancellation, fact.Kind)
	require.NotNil(t, fact.ChargeAdjustmentID)
	assert.Equal(t, *outcome.ChargeAdjustmentID, *fact.ChargeAdjustmentID)
	assert.Equal(t, outcome.OccurredAt, fact.OccurredAt)
	assert.Equal(t, env.CashierActor().StaffID, fact.ActorID)

	require.Equal(t, 1, env.CountAdjustments(t, unit.ID))
	adjustment := env.UnitAdjustment(t, unit.ID)
	assert.Equal(t, "CANCELLATION", adjustment.Kind)
	assert.Equal(t, "LIVE_CHECK", adjustment.Scope)
	assert.EqualValues(t, 25000, adjustment.AmountVND)
	assert.Equal(t, check.ID, adjustment.CheckID)
	assert.Equal(t, fact.OccurredAt, adjustment.CreatedAt)

	// The charged Check loses the price and settles with all four evidence
	// columns, because the corrected balance is zero.
	after := env.CheckForUnit(t, unit.ID)
	assert.EqualValues(t, 0, after.ChargeVND)
	assert.Equal(t, sales.CheckStateSettled, after.State)
	require.NotNil(t, after.SettledAt)
	require.NotNil(t, after.SettledBy)
	require.NotNil(t, after.SettledShiftID)
	require.NotNil(t, after.SettledSession)
	assert.Equal(t, env.CashierActor().StaffID, *after.SettledBy)
	assert.Equal(t, env.ShiftID, *after.SettledShiftID)
	assert.Equal(t, env.CashierActor().SessionID, *after.SettledSession)

	// The projection agrees and carries no pending refund: nothing was paid.
	projected := env.ProjectedCheck(t, sessionID, check.ID)
	assert.EqualValues(t, 0, projected.ChargeVND)
	assert.EqualValues(t, 0, projected.BalanceVND)
	assert.EqualValues(t, 0, projected.PendingRefundVND)
	assert.Equal(t, sales.CheckStateSettled, projected.State)

	// One audit per business fact, plus the settlement event.
	assert.Equal(t, 1, env.CountAuditEventsByTypeAndUnit(
		t, preparation.EventPreparationUnitCancelled, unit.ID))
	assert.Equal(t, 1, env.CountAuditEventsByTypeAndUnit(
		t, preparation.EventPreparationAlertCreated, unit.ID))
	assert.Equal(t, 1, env.CountAuditEventsByTypeAndUnit(
		t, preparation.EventCheckChargeAdjusted, unit.ID))
	assert.Equal(t, 1, countCheckSettledAudits(t, env, check.ID))
}

// TestCancelUnitsFullyPaidCreatesPendingRefund cancels a charged unit of a
// fully paid Check: the Check stays SETTLED while its pending Refund appears.
func TestCancelUnitsFullyPaidCreatesPendingRefund(t *testing.T) {
	env := newCancelEnv(t)
	unit := env.SubmittedTakeawayUnits(t, 1)[0]
	sessionID := env.SessionIDForUnit(t, unit.ID)
	check := env.CheckForUnit(t, unit.ID)
	require.Equal(t, sales.CheckStateSettled, check.State)
	settledAt := check.SettledAt
	require.NotNil(t, settledAt)

	resp, status, err := env.Cancel(t, cancelCommand(
		[]uuid.UUID{unit.ID}, preparation.CancelKindCancellation, preparation.ReasonItemUnavailable))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
	require.Len(t, resp.Outcomes, 1)
	require.NotNil(t, resp.Outcomes[0].ChargeAdjustmentID)
	assert.EqualValues(t, 25000, resp.Outcomes[0].ChargeRemovedVND)

	after := env.CheckForUnit(t, unit.ID)
	assert.EqualValues(t, 0, after.ChargeVND)
	assert.Equal(t, sales.CheckStateSettled, after.State,
		"a settled Check remains settled while it carries a pending refund")
	require.NotNil(t, after.SettledAt)
	assert.Equal(t, *settledAt, *after.SettledAt,
		"the original settlement evidence is not rewritten")

	projected := env.ProjectedCheck(t, sessionID, check.ID)
	assert.EqualValues(t, 0, projected.ChargeVND)
	assert.EqualValues(t, 25000, projected.EffectiveReceivedVND)
	assert.EqualValues(t, 0, projected.BalanceVND)
	assert.EqualValues(t, 25000, projected.PendingRefundVND)
}

// TestCancelUnitsPartialPaymentReducedToZero cancels one of two units on a
// partially paid Check: the corrected charge equals the receipt, so the Check
// settles with evidence and no pending refund appears.
func TestCancelUnitsPartialPaymentReducedToZero(t *testing.T) {
	env := newCancelEnv(t)
	units := env.SubmittedUnits(t, 2)
	sessionID := env.SessionIDForUnit(t, units[0].ID)
	check := env.CheckForUnit(t, units[0].ID)
	require.EqualValues(t, 50000, check.ChargeVND)

	env.PayCheck(t, check.ID, 25000)
	open := env.CheckForUnit(t, units[0].ID)
	require.Equal(t, sales.CheckStateOpen, open.State)

	resp, status, err := env.Cancel(t, cancelCommand(
		[]uuid.UUID{units[0].ID}, preparation.CancelKindCancellation, preparation.ReasonCustomerRequest))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
	require.Len(t, resp.Outcomes, 1)

	after := env.CheckForUnit(t, units[0].ID)
	assert.EqualValues(t, 25000, after.ChargeVND)
	assert.Equal(t, sales.CheckStateSettled, after.State)
	require.NotNil(t, after.SettledAt)
	require.NotNil(t, after.SettledBy)
	require.NotNil(t, after.SettledShiftID)
	require.NotNil(t, after.SettledSession)
	assert.Equal(t, env.CashierActor().StaffID, *after.SettledBy)

	projected := env.ProjectedCheck(t, sessionID, check.ID)
	assert.EqualValues(t, 25000, projected.ChargeVND)
	assert.EqualValues(t, 25000, projected.EffectiveReceivedVND)
	assert.EqualValues(t, 0, projected.BalanceVND)
	assert.EqualValues(t, 0, projected.PendingRefundVND)
	assert.Equal(t, sales.CheckStateSettled, projected.State)
}

// TestCancelUnitsChangeReplacement accepts a valid later replacement Order in
// the same Session and rejects every invalid replacement shape.
func TestCancelUnitsChangeReplacement(t *testing.T) {
	t.Run("a later submitted Order in the same Session is accepted", func(t *testing.T) {
		env := newCancelEnv(t)
		first := env.SubmittedUnits(t, 1)[0]
		sessionID := env.SessionIDForUnit(t, first.ID)
		second := env.SubmitAnotherRound(t, sessionID, 1)[0]
		replacementOrderID := env.OrderIDForUnit(t, second.ID)

		cmd := cancelCommand([]uuid.UUID{first.ID}, preparation.CancelKindChange,
			preparation.ReasonCustomerRequest)
		cmd.ReplacementOrderID = &replacementOrderID

		resp, status, err := env.Cancel(t, cmd)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, status)
		require.Len(t, resp.Alerts, 1)
		assert.Equal(t, preparation.AlertKindChange, resp.Alerts[0].Kind)

		require.Equal(t, 1, env.CountCancellations(t, first.ID))
		fact := env.UnitCancellation(t, first.ID)
		assert.Equal(t, preparation.CancelKindChange, fact.Kind)
		require.NotNil(t, fact.ReplacementOrderID)
		assert.Equal(t, replacementOrderID, *fact.ReplacementOrderID)

		alert := env.UnitAlert(t, first.ID)
		assert.Equal(t, preparation.AlertKindChange, alert.Kind,
			"a Change alert uses the reserved CHANGE kind")

		// The replacement Order is never edited: it still owns its unit.
		assert.Equal(t, preparation.StateQueued, env.UnitState(t, second.ID))
	})

	t.Run("an unknown replacement Order is 404", func(t *testing.T) {
		env := newCancelEnv(t)
		unit := env.SubmittedUnits(t, 1)[0]
		missing := uuid.New()
		cmd := cancelCommand([]uuid.UUID{unit.ID}, preparation.CancelKindChange,
			preparation.ReasonCustomerRequest)
		cmd.ReplacementOrderID = &missing

		before := env.countCancelWrites(t)
		_, status, err := env.Cancel(t, cmd)
		require.ErrorIs(t, err, preparation.ErrReplacementOrderNotFound)
		require.Equal(t, http.StatusNotFound, status)
		assert.Equal(t, before, env.countCancelWrites(t))
	})

	t.Run("the source Order itself is not a replacement", func(t *testing.T) {
		env := newCancelEnv(t)
		unit := env.SubmittedUnits(t, 1)[0]
		sourceOrderID := env.OrderIDForUnit(t, unit.ID)
		cmd := cancelCommand([]uuid.UUID{unit.ID}, preparation.CancelKindChange,
			preparation.ReasonCustomerRequest)
		cmd.ReplacementOrderID = &sourceOrderID

		before := env.countCancelWrites(t)
		_, status, err := env.Cancel(t, cmd)
		require.ErrorIs(t, err, preparation.ErrReplacementOrderInvalid)
		require.Equal(t, http.StatusConflict, status)
		assert.Equal(t, before, env.countCancelWrites(t))
	})

	t.Run("an earlier Order is not a later replacement", func(t *testing.T) {
		env := newCancelEnv(t)
		first := env.SubmittedUnits(t, 1)[0]
		sessionID := env.SessionIDForUnit(t, first.ID)
		second := env.SubmitAnotherRound(t, sessionID, 1)[0]
		earlierOrderID := env.OrderIDForUnit(t, first.ID)

		cmd := cancelCommand([]uuid.UUID{second.ID}, preparation.CancelKindChange,
			preparation.ReasonCustomerRequest)
		cmd.ReplacementOrderID = &earlierOrderID

		before := env.countCancelWrites(t)
		_, status, err := env.Cancel(t, cmd)
		require.ErrorIs(t, err, preparation.ErrReplacementOrderInvalid)
		require.Equal(t, http.StatusConflict, status)
		assert.Equal(t, before, env.countCancelWrites(t))
	})

	t.Run("an Order of another Session is not a replacement", func(t *testing.T) {
		env := newCancelEnv(t)
		unit := env.SubmittedUnits(t, 1)[0]
		other := env.SubmittedUnits(t, 1)[0]
		foreignOrderID := env.OrderIDForUnit(t, other.ID)

		cmd := cancelCommand([]uuid.UUID{unit.ID}, preparation.CancelKindChange,
			preparation.ReasonCustomerRequest)
		cmd.ReplacementOrderID = &foreignOrderID

		before := env.countCancelWrites(t)
		_, status, err := env.Cancel(t, cmd)
		require.ErrorIs(t, err, preparation.ErrReplacementOrderInvalid)
		require.Equal(t, http.StatusConflict, status)
		assert.Equal(t, before, env.countCancelWrites(t))
	})
}

// TestCancelUnitsRemakeChangesNoCharge cancels a queued Remake: it writes no
// Charge Adjustment and changes no Check.
func TestCancelUnitsRemakeChangesNoCharge(t *testing.T) {
	env := newCancelEnv(t)
	source := env.SubmittedUnits(t, 1)[0]
	_, _, err := env.Advance(t, source.ID, preparation.StateInPreparation)
	require.NoError(t, err)
	waste, _, err := env.Waste(t, source.ID, preparation.ReasonQualityFailure, nil)
	require.NoError(t, err)
	remake, _, err := env.Remake(t, waste.ID, preparation.ReasonPreparationError, nil)
	require.NoError(t, err)
	replacementID := remake.Unit.ID
	check := env.CheckForUnit(t, source.ID)

	resp, status, err := env.Cancel(t, cancelCommand(
		[]uuid.UUID{replacementID}, preparation.CancelKindCancellation, preparation.ReasonCustomerRequest))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
	require.Len(t, resp.Outcomes, 1)
	assert.Nil(t, resp.Outcomes[0].ChargeAdjustmentID)
	assert.EqualValues(t, 0, resp.Outcomes[0].ChargeRemovedVND)

	after := env.CheckForUnit(t, source.ID)
	assert.EqualValues(t, check.ChargeVND, after.ChargeVND,
		"an uncharged Remake changes no Check charge")
	assert.Equal(t, check.State, after.State)
	assert.Equal(t, 0, env.CountAdjustments(t, replacementID))
	assert.Equal(t, 0, env.CountAdjustmentsForCheck(t, check.ID))

	fact := env.UnitCancellation(t, replacementID)
	assert.Nil(t, fact.ChargeAdjustmentID)
	assert.Equal(t, preparation.StateCancelled, env.UnitState(t, replacementID))
	assert.Equal(t, 1, env.CountTransitionsTo(t, replacementID, preparation.StateCancelled))
	assert.Equal(t, 1, env.CountAlerts(t, replacementID))
}

// TestCancelUnitsFiftyUnitBatch cancels the maximum batch and preserves the
// request's own order in the response.
func TestCancelUnitsFiftyUnitBatch(t *testing.T) {
	env := newCancelEnv(t)
	units := env.SubmittedUnits(t, 50)
	sessionID := env.SessionIDForUnit(t, units[0].ID)
	check := env.CheckForUnit(t, units[0].ID)
	require.EqualValues(t, 50*25000, check.ChargeVND)

	// Reverse the fixture order: the response must follow the request, not
	// the fixture and not the locks' byte order.
	requested := make([]uuid.UUID, 0, len(units))
	for i := len(units) - 1; i >= 0; i-- {
		requested = append(requested, units[i].ID)
	}

	resp, status, err := env.Cancel(t, cancelCommand(
		requested, preparation.CancelKindCancellation, preparation.ReasonCustomerRequest))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
	require.Len(t, resp.Outcomes, 50)
	require.Len(t, resp.Alerts, 50)

	for i, outcome := range resp.Outcomes {
		assert.Equal(t, requested[i], outcome.PreparationUnitID,
			"outcomes follow the request's own order")
		require.NotNil(t, outcome.ChargeAdjustmentID)
		assert.EqualValues(t, 25000, outcome.ChargeRemovedVND)
		assert.Equal(t, requested[i], resp.Alerts[i].PreparationUnitID)
	}

	after := env.CheckForUnit(t, units[0].ID)
	assert.EqualValues(t, 0, after.ChargeVND)
	assert.Equal(t, sales.CheckStateSettled, after.State)
	assert.Equal(t, 50, env.CountAdjustmentsForCheck(t, check.ID))

	// One database timestamp for the whole batch.
	var distinctStamps int
	require.NoError(t, env.DB.QueryRow(`
		SELECT count(DISTINCT occurred_at)
		FROM preparation_cancellations AS pc
		JOIN preparation_units AS pu ON pu.id = pc.preparation_unit_id
		JOIN order_items AS oi ON oi.id = pu.order_item_id
		JOIN orders AS o ON o.id = oi.order_id
		WHERE o.service_session_id = $1`, sessionID).Scan(&distinctStamps))
	assert.Equal(t, 1, distinctStamps, "the whole batch shares one timestamp")

	projected := env.ProjectedCheck(t, sessionID, check.ID)
	assert.EqualValues(t, 0, projected.ChargeVND)
	assert.EqualValues(t, 0, projected.PendingRefundVND)
}

// TestCancelUnitsBatchRollbacks proves the command is all-or-nothing: one
// missing, stale, cross-Session, or shiftless selection rolls the whole batch
// back.
func TestCancelUnitsBatchRollbacks(t *testing.T) {
	t.Run("a missing unit rejects the whole batch", func(t *testing.T) {
		env := newCancelEnv(t)
		unit := env.SubmittedUnits(t, 1)[0]
		check := env.CheckForUnit(t, unit.ID)

		before := env.countCancelWrites(t)
		_, status, err := env.Cancel(t, cancelCommand(
			[]uuid.UUID{unit.ID, uuid.New()},
			preparation.CancelKindCancellation, preparation.ReasonCustomerRequest))
		require.ErrorIs(t, err, preparation.ErrUnitNotFound)
		require.Equal(t, http.StatusNotFound, status)

		assert.Equal(t, before, env.countCancelWrites(t))
		assert.Equal(t, preparation.StateQueued, env.UnitState(t, unit.ID))
		assert.EqualValues(t, check.ChargeVND, env.CheckForUnit(t, unit.ID).ChargeVND)
	})

	t.Run("a stale unit rejects the whole batch", func(t *testing.T) {
		env := newCancelEnv(t)
		units := env.SubmittedUnits(t, 2)
		_, _, err := env.Advance(t, units[0].ID, preparation.StateInPreparation)
		require.NoError(t, err)
		check := env.CheckForUnit(t, units[1].ID)

		before := env.countCancelWrites(t)
		_, status, err := env.Cancel(t, cancelCommand(
			[]uuid.UUID{units[1].ID, units[0].ID},
			preparation.CancelKindCancellation, preparation.ReasonCustomerRequest))
		require.ErrorIs(t, err, preparation.ErrCancellationSourceNotQueued)
		require.Equal(t, http.StatusConflict, status)

		assert.Equal(t, before, env.countCancelWrites(t))
		assert.Equal(t, preparation.StateQueued, env.UnitState(t, units[1].ID))
		assert.EqualValues(t, check.ChargeVND, env.CheckForUnit(t, units[1].ID).ChargeVND)
	})

	t.Run("a cross-Session selection rejects the whole batch", func(t *testing.T) {
		env := newCancelEnv(t)
		first := env.SubmittedUnits(t, 1)[0]
		second := env.SubmittedUnits(t, 1)[0]
		require.NotEqual(t, env.SessionIDForUnit(t, first.ID), env.SessionIDForUnit(t, second.ID))

		before := env.countCancelWrites(t)
		_, status, err := env.Cancel(t, cancelCommand(
			[]uuid.UUID{first.ID, second.ID},
			preparation.CancelKindCancellation, preparation.ReasonCustomerRequest))
		require.ErrorIs(t, err, preparation.ErrCancellationSessionMismatch)
		require.Equal(t, http.StatusConflict, status)

		assert.Equal(t, before, env.countCancelWrites(t))
		assert.Equal(t, preparation.StateQueued, env.UnitState(t, first.ID))
		assert.Equal(t, preparation.StateQueued, env.UnitState(t, second.ID))
	})

	t.Run("a closed Shift rejects the whole batch", func(t *testing.T) {
		env := newCancelEnv(t)
		unit := env.SubmittedUnits(t, 1)[0]
		check := env.CheckForUnit(t, unit.ID)
		_, err := env.DB.Exec(
			`UPDATE sales_shifts SET state = 'CLOSED' WHERE id = $1`, env.ShiftID)
		require.NoError(t, err)

		before := env.countCancelWrites(t)
		_, status, err := env.Cancel(t, cancelCommand(
			[]uuid.UUID{unit.ID},
			preparation.CancelKindCancellation, preparation.ReasonCustomerRequest))
		require.ErrorIs(t, err, preparation.ErrOpenShiftRequired)
		require.Equal(t, http.StatusConflict, status)

		assert.Equal(t, before, env.countCancelWrites(t))
		assert.Equal(t, preparation.StateQueued, env.UnitState(t, unit.ID))
		assert.EqualValues(t, check.ChargeVND, env.CheckForUnit(t, unit.ID).ChargeVND)
	})
}

// TestCancelUnitsReplayAndConflict proves the idempotency contract: an exact
// replay returns the stored result and writes nothing more, and a reused
// request id with different meaning conflicts.
func TestCancelUnitsReplayAndConflict(t *testing.T) {
	env := newCancelEnv(t)
	units := env.SubmittedUnits(t, 2)
	sessionID := env.SessionIDForUnit(t, units[0].ID)
	check := env.CheckForUnit(t, units[0].ID)
	requestID := uuid.New()

	requested := []uuid.UUID{units[1].ID, units[0].ID}
	first, status, err := env.CancelWithRequestID(t, requestID,
		cancelCommand(requested, preparation.CancelKindCancellation, preparation.ReasonCustomerRequest))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
	require.Len(t, first.Outcomes, 2)

	// The sorted fingerprint makes a differently ordered replay the same
	// request; the stored result is returned unchanged.
	replayOrder := []uuid.UUID{units[0].ID, units[1].ID}
	second, status, err := env.CancelWithRequestID(t, requestID,
		cancelCommand(replayOrder, preparation.CancelKindCancellation, preparation.ReasonCustomerRequest))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
	firstJSON, err := json.Marshal(first)
	require.NoError(t, err)
	secondJSON, err := json.Marshal(second)
	require.NoError(t, err)
	require.JSONEq(t, string(firstJSON), string(secondJSON))

	assert.Equal(t, 1, env.CountCancellations(t, units[0].ID))
	assert.Equal(t, 1, env.CountCancellations(t, units[1].ID))
	assert.Equal(t, 2, env.CountAdjustmentsForCheck(t, check.ID))
	assert.Equal(t, 2, env.CountAuditEvents(
		t, preparation.EventCheckChargeAdjusted))

	// Same request id, different business input: a conflict, never a replay.
	_, status, err = env.CancelWithRequestID(t, requestID,
		cancelCommand(requested, preparation.CancelKindCancellation, preparation.ReasonItemUnavailable))
	require.ErrorIs(t, err, preparation.ErrRequestConflict)
	require.Equal(t, http.StatusConflict, status)
	assert.Equal(t, 1, env.CountCancellations(t, units[0].ID))

	projected := env.ProjectedCheck(t, sessionID, check.ID)
	assert.EqualValues(t, 0, projected.ChargeVND)
}

// TestCancelUnitsAuthorization requires sales.operate: the Cashier and the
// Manager may cancel, a Barista alone may not, and current capability is
// rechecked even on the replay path.
func TestCancelUnitsAuthorization(t *testing.T) {
	t.Run("cashier and manager may cancel; barista is denied", func(t *testing.T) {
		env := newCancelEnv(t)
		cashierUnit := env.SubmittedUnits(t, 1)[0]
		managerUnit := env.SubmittedUnits(t, 1)[0]
		baristaUnit := env.SubmittedUnits(t, 1)[0]

		_, status, err := env.CancelAs(t, env.CashierActor(), cancelCommand(
			[]uuid.UUID{cashierUnit.ID}, preparation.CancelKindCancellation,
			preparation.ReasonCustomerRequest))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, status)

		_, status, err = env.CancelAs(t, env.ManagerActor(), cancelCommand(
			[]uuid.UUID{managerUnit.ID}, preparation.CancelKindCancellation,
			preparation.ReasonCustomerRequest))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, status)

		before := env.countCancelWrites(t)
		_, status, err = env.CancelAs(t, env.BaristaActor(), cancelCommand(
			[]uuid.UUID{baristaUnit.ID}, preparation.CancelKindCancellation,
			preparation.ReasonCustomerRequest))
		require.ErrorIs(t, err, preparation.ErrForbidden)
		require.Equal(t, http.StatusForbidden, status)
		assert.Equal(t, before, env.countCancelWrites(t))
		assert.Equal(t, preparation.StateQueued, env.UnitState(t, baristaUnit.ID))
	})

	t.Run("removed capability denies an exact replay", func(t *testing.T) {
		env := newCancelEnv(t)
		unit := env.SubmittedUnits(t, 1)[0]
		requestID := uuid.New()

		_, status, err := env.CancelWithRequestID(t, requestID, cancelCommand(
			[]uuid.UUID{unit.ID}, preparation.CancelKindCancellation,
			preparation.ReasonCustomerRequest))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, status)

		env.ReplaceRoles(t, env.CashierActor(), nil)
		t.Cleanup(func() {
			env.ReplaceRoles(t, env.CashierActor(), []string{auth.RoleCashier})
		})

		_, status, err = env.CancelWithRequestID(t, requestID, cancelCommand(
			[]uuid.UUID{unit.ID}, preparation.CancelKindCancellation,
			preparation.ReasonCustomerRequest))
		require.ErrorIs(t, err, preparation.ErrForbidden,
			"the capability is rechecked even on the replay path")
		require.Equal(t, http.StatusForbidden, status)

		assert.Equal(t, 1, env.CountCancellations(t, unit.ID),
			"the denial leaves the stored result untouched")
	})
}

// TestCancelUnitsInjectedFailures forces a failure at each write of the
// transaction and proves the whole batch rolls back, claim included.
func TestCancelUnitsInjectedFailures(t *testing.T) {
	// assertRolledBack proves no cancellation write survived.
	assertRolledBack := func(t *testing.T, env *cancelEnv, unitID uuid.UUID,
		check cancelCheckRow, requestID uuid.UUID,
	) {
		t.Helper()
		assert.Equal(t, preparation.StateQueued, env.UnitState(t, unitID))
		assert.EqualValues(t, check.ChargeVND, env.CheckForUnit(t, unitID).ChargeVND)
		assert.Equal(t, check.State, env.CheckForUnit(t, unitID).State)
		assert.Equal(t, 0, env.CountCancellations(t, unitID))
		assert.Equal(t, 0, env.CountAdjustments(t, unitID))
		assert.Equal(t, 0, env.CountAlerts(t, unitID))
		assert.Equal(t, 0, env.CountTransitionsTo(t, unitID, preparation.StateCancelled))
		_, claimed := env.IdempotencyClaim(t, env.CashierActor(), requestID)
		assert.False(t, claimed, "the failed transaction must not leave its claim behind")
	}

	t.Run("adjustment insert failure", func(t *testing.T) {
		env := newCancelEnv(t)
		unit := env.SubmittedUnits(t, 1)[0]
		check := env.CheckForUnit(t, unit.ID)
		installCancelTrigger(t, env,
			"fail_cancel_adjustment",
			`CREATE FUNCTION fail_cancel_adjustment() RETURNS trigger AS $$
			 BEGIN
			     IF NEW.preparation_unit_id = '`+unit.ID.String()+`'::uuid THEN
			         RAISE EXCEPTION 'forced adjustment insert failure';
			     END IF;
			     RETURN NEW;
			 END;
			 $$ LANGUAGE plpgsql;
			 CREATE TRIGGER fail_cancel_adjustment
			 BEFORE INSERT ON charge_adjustments
			 FOR EACH ROW EXECUTE FUNCTION fail_cancel_adjustment();`,
			"charge_adjustments")

		requestID := uuid.New()
		_, status, err := env.CancelWithRequestID(t, requestID, cancelCommand(
			[]uuid.UUID{unit.ID}, preparation.CancelKindCancellation,
			preparation.ReasonCustomerRequest))
		require.Error(t, err, "the forced adjustment failure must abort the command")
		require.Equal(t, http.StatusInternalServerError, status)
		assertRolledBack(t, env, unit.ID, check, requestID)
	})

	t.Run("cancellation fact failure", func(t *testing.T) {
		env := newCancelEnv(t)
		unit := env.SubmittedUnits(t, 1)[0]
		check := env.CheckForUnit(t, unit.ID)
		installCancelTrigger(t, env,
			"fail_cancel_fact",
			`CREATE FUNCTION fail_cancel_fact() RETURNS trigger AS $$
			 BEGIN
			     IF NEW.preparation_unit_id = '`+unit.ID.String()+`'::uuid THEN
			         RAISE EXCEPTION 'forced cancellation fact failure';
			     END IF;
			     RETURN NEW;
			 END;
			 $$ LANGUAGE plpgsql;
			 CREATE TRIGGER fail_cancel_fact
			 BEFORE INSERT ON preparation_cancellations
			 FOR EACH ROW EXECUTE FUNCTION fail_cancel_fact();`,
			"preparation_cancellations")

		requestID := uuid.New()
		_, status, err := env.CancelWithRequestID(t, requestID, cancelCommand(
			[]uuid.UUID{unit.ID}, preparation.CancelKindCancellation,
			preparation.ReasonCustomerRequest))
		require.Error(t, err, "the forced fact failure must abort the command")
		require.Equal(t, http.StatusInternalServerError, status)
		assertRolledBack(t, env, unit.ID, check, requestID)
	})

	t.Run("alert insert failure", func(t *testing.T) {
		env := newCancelEnv(t)
		unit := env.SubmittedUnits(t, 1)[0]
		check := env.CheckForUnit(t, unit.ID)
		installCancelTrigger(t, env,
			"fail_cancel_alert",
			`CREATE FUNCTION fail_cancel_alert() RETURNS trigger AS $$
			 BEGIN
			     IF NEW.preparation_unit_id = '`+unit.ID.String()+`'::uuid THEN
			         RAISE EXCEPTION 'forced cancellation alert failure';
			     END IF;
			     RETURN NEW;
			 END;
			 $$ LANGUAGE plpgsql;
			 CREATE TRIGGER fail_cancel_alert
			 BEFORE INSERT ON preparation_alerts
			 FOR EACH ROW EXECUTE FUNCTION fail_cancel_alert();`,
			"preparation_alerts")

		requestID := uuid.New()
		_, status, err := env.CancelWithRequestID(t, requestID, cancelCommand(
			[]uuid.UUID{unit.ID}, preparation.CancelKindCancellation,
			preparation.ReasonCustomerRequest))
		require.Error(t, err, "the forced alert failure must abort the command")
		require.Equal(t, http.StatusInternalServerError, status)
		assertRolledBack(t, env, unit.ID, check, requestID)
	})

	t.Run("audit batch failure", func(t *testing.T) {
		env := newCancelEnv(t)
		unit := env.SubmittedUnits(t, 1)[0]
		check := env.CheckForUnit(t, unit.ID)
		installCancelTrigger(t, env,
			"fail_cancel_audit",
			`CREATE FUNCTION fail_cancel_audit() RETURNS trigger AS $$
			 BEGIN
			     IF NEW.event_type = 'PREPARATION_UNIT_CANCELLED' THEN
			         RAISE EXCEPTION 'forced cancellation audit failure';
			     END IF;
			     RETURN NEW;
			 END;
			 $$ LANGUAGE plpgsql;
			 CREATE TRIGGER fail_cancel_audit
			 BEFORE INSERT ON audit_events
			 FOR EACH ROW EXECUTE FUNCTION fail_cancel_audit();`,
			"audit_events")

		requestID := uuid.New()
		_, status, err := env.CancelWithRequestID(t, requestID, cancelCommand(
			[]uuid.UUID{unit.ID}, preparation.CancelKindCancellation,
			preparation.ReasonCustomerRequest))
		require.Error(t, err, "the forced audit failure must abort the command")
		require.Equal(t, http.StatusInternalServerError, status)
		assertRolledBack(t, env, unit.ID, check, requestID)
	})

	t.Run("result storage failure", func(t *testing.T) {
		env := newCancelEnv(t)
		unit := env.SubmittedUnits(t, 1)[0]
		check := env.CheckForUnit(t, unit.ID)
		installCancelTrigger(t, env,
			"fail_cancel_result_store",
			`CREATE FUNCTION fail_cancel_result_store() RETURNS trigger AS $$
			 BEGIN
			     IF NEW.action = 'preparation.cancel_units' AND NEW.response_code <> 0 THEN
			         RAISE EXCEPTION 'forced cancellation result store failure';
			     END IF;
			     RETURN NEW;
			 END;
			 $$ LANGUAGE plpgsql;
			 CREATE TRIGGER fail_cancel_result_store
			 BEFORE UPDATE ON idempotency_keys
			 FOR EACH ROW EXECUTE FUNCTION fail_cancel_result_store();`,
			"idempotency_keys")

		requestID := uuid.New()
		_, status, err := env.CancelWithRequestID(t, requestID, cancelCommand(
			[]uuid.UUID{unit.ID}, preparation.CancelKindCancellation,
			preparation.ReasonCustomerRequest))
		require.Error(t, err, "the forced store failure must abort the command")
		require.Equal(t, http.StatusInternalServerError, status)
		assertRolledBack(t, env, unit.ID, check, requestID)
	})
}

// TestCancelUnitsQueuePrivacy proves the Cancellation flow is visible on the
// queue with no financial leakage, and that acknowledgment clears the retained
// terminal unit without changing finance.
func TestCancelUnitsQueuePrivacy(t *testing.T) {
	env := newCancelEnv(t)
	units := env.SubmittedUnits(t, 2)
	sessionID := env.SessionIDForUnit(t, units[0].ID)

	// A later submitted Order gives the first unit a valid CHANGE
	// replacement; the second unit is a plain CANCELLATION.
	replacementUnit := env.SubmitAnotherRound(t, sessionID, 1)[0]
	replacementOrderID := env.OrderIDForUnit(t, replacementUnit.ID)
	changeCmd := cancelCommand([]uuid.UUID{units[0].ID}, preparation.CancelKindChange,
		preparation.ReasonCustomerRequest)
	changeCmd.ReplacementOrderID = &replacementOrderID
	_, status, err := env.Cancel(t, changeCmd)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)

	_, status, err = env.Cancel(t, cancelCommand(
		[]uuid.UUID{units[1].ID}, preparation.CancelKindCancellation,
		preparation.ReasonCustomerRequest))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)

	// The replacement Order's own charge stays; only the two cancelled
	// prices are removed.
	check := env.CheckForUnit(t, units[0].ID)
	assert.EqualValues(t, 25000, check.ChargeVND)

	queue, err := preparation.NewActiveQueueHandler(env.PreparationRunner).
		Handle(context.Background(), env.BaristaActor())
	require.NoError(t, err)

	// Both cancelled units are retained on the queue by their unacknowledged
	// alerts, one per kind.
	retainedUnits := map[uuid.UUID]struct{}{}
	for _, unit := range queue.Units {
		if unit.ID == units[0].ID || unit.ID == units[1].ID {
			assert.Equal(t, preparation.StateCancelled, unit.State)
			retainedUnits[unit.ID] = struct{}{}
		}
	}
	assert.Len(t, retainedUnits, 2, "the alert-retained cancelled units stay on the queue")

	alertKinds := map[string]bool{}
	for _, alert := range queue.Alerts {
		if alert.PreparationUnitID == units[0].ID || alert.PreparationUnitID == units[1].ID {
			alertKinds[alert.Kind] = true
			assert.Nil(t, alert.WasteID, "a Cancellation alert resolves no Waste fact")
		}
	}
	assert.Equal(t, map[string]bool{
		preparation.AlertKindCancellation: true,
		preparation.AlertKindChange:       true,
	}, alertKinds)

	raw, err := json.Marshal(queue)
	require.NoError(t, err)
	var decoded any
	require.NoError(t, json.Unmarshal(raw, &decoded))
	for _, fragment := range []string{"charge_vnd", "payment", "refund", "adjustment"} {
		assert.False(t, jsonContainsFragment(decoded, fragment),
			"queue output must not contain %q anywhere", fragment)
	}

	// Acknowledgment removes the terminal units and alerts from the active
	// queue and changes no finance.
	for _, unit := range units {
		alertID := env.UnitAlert(t, unit.ID).ID
		_, status, err = env.Acknowledge(t, alertID)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, status)
	}

	afterAck, err := preparation.NewActiveQueueHandler(env.PreparationRunner).
		Handle(context.Background(), env.BaristaActor())
	require.NoError(t, err)
	for _, unit := range afterAck.Units {
		assert.NotEqual(t, units[0].ID, unit.ID,
			"an acknowledged terminal unit leaves the active queue")
		assert.NotEqual(t, units[1].ID, unit.ID)
	}
	for _, alert := range afterAck.Alerts {
		assert.NotEqual(t, units[0].ID, alert.PreparationUnitID)
		assert.NotEqual(t, units[1].ID, alert.PreparationUnitID)
	}
	after := env.CheckForUnit(t, units[0].ID)
	assert.EqualValues(t, check.ChargeVND, after.ChargeVND)
	assert.Equal(t, check.State, after.State)
}

// installCancelTrigger installs a forced-failure trigger on one table and
// removes it at the end of the test, mirroring the package's other
// forced-failure fixtures.
func installCancelTrigger(t *testing.T, env *cancelEnv, name, ddl, table string) {
	t.Helper()
	_, err := env.DB.Exec(ddl)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = env.DB.Exec("DROP TRIGGER IF EXISTS " + name + " ON " + table)
		_, _ = env.DB.Exec("DROP FUNCTION IF EXISTS " + name + "()")
	})
}

// countCheckSettledAudits counts the CHECK_SETTLED audits naming one Check.
func countCheckSettledAudits(t *testing.T, env *cancelEnv, checkID uuid.UUID) int {
	t.Helper()
	var n int
	require.NoError(t, env.DB.QueryRow(`
		SELECT count(*)
		FROM audit_events
		WHERE event_type = $1
		  AND details->>'check_id' = $2`,
		preparation.EventCheckSettled, checkID.String()).Scan(&n))
	return n
}
