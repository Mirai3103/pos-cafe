//go:build integration

package preparation_test

import (
	"context"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/preparation"
	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// The corrections E2E suites drive the three approved Phase 6B recovery
// workflows end to end through the real handlers on both sides of the ADR-024
// boundary: Preparation's Waste, Acknowledge, Remake, CorrectState, and
// Advance, and Sales' Submit, Pay, Close, and Completed Sale reads. At every
// boundary a workflow asserts the five things a recovery command may never
// break: the stored facts, the queue's visibility and order, the money, the
// closure readiness verdict, and the metadata plus complete typed history the
// Completed Sale later publishes.

// readQueue runs the active-queue read as the Barista, who holds
// preparation.operate.
func readQueue(t *testing.T, env *prepEnv) preparation.QueueResponse {
	t.Helper()
	queue, err := preparation.NewActiveQueueHandler(env.PreparationRunner).
		Handle(context.Background(), env.BaristaActor())
	require.NoError(t, err)
	return queue
}

// queueUnitByID returns the queue unit with the given id and its position in
// the queue's ordering, failing the test when absent.
func queueUnitByID(t *testing.T, queue preparation.QueueResponse,
	unitID uuid.UUID,
) (preparation.QueueUnitResponse, int) {
	t.Helper()
	for i, unit := range queue.Units {
		if unit.ID == unitID {
			return unit, i
		}
	}
	t.Fatalf("queue does not contain unit %s", unitID)
	return preparation.QueueUnitResponse{}, -1
}

// queueAlertByID returns the active queue alert with the given id, failing the
// test when absent.
func queueAlertByID(t *testing.T, queue preparation.QueueResponse,
	alertID uuid.UUID,
) preparation.QueueAlertResponse {
	t.Helper()
	for _, alert := range queue.Alerts {
		if alert.ID == alertID {
			return alert
		}
	}
	t.Fatalf("queue does not contain active alert %s", alertID)
	return preparation.QueueAlertResponse{}
}

// queueCorrectionByID returns the queue history entry with the given fact id,
// failing the test when absent.
func queueCorrectionByID(t *testing.T, queue preparation.QueueResponse,
	factID uuid.UUID,
) preparation.QueueCorrectionResponse {
	t.Helper()
	for _, entry := range queue.Corrections {
		if entry.ID == factID {
			return entry
		}
	}
	t.Fatalf("queue history does not contain fact %s", factID)
	return preparation.QueueCorrectionResponse{}
}

// requireMoneyUnchanged asserts a Preparation command changed no financial
// meaning: the Service Session's state and every Check's stored charge slice
// must be exactly as they were before. Readiness verdicts are deliberately not
// compared — a Remake legitimately adds a nonterminal unit and a settled Check
// legitimately moves the verdict — but the money never moves.
func requireMoneyUnchanged(t *testing.T, env *prepEnv, sessionID uuid.UUID,
	before sessionFinancials, phase string,
) {
	t.Helper()
	after := env.SessionFinancials(t, sessionID)
	require.Equal(t, before.State, after.State, "%s must not change the session state", phase)
	require.Equal(t, before.Checks, after.Checks,
		"%s must change no check's charge, applied amounts, payments, or allocations", phase)
}

// advanceSteps advances one unit through the given target states in order,
// failing the test on the first denial.
func advanceSteps(t *testing.T, env *prepEnv, unitID uuid.UUID, targets ...string) {
	t.Helper()
	for _, target := range targets {
		resp, _, err := env.Advance(t, unitID, target)
		require.NoError(t, err, "advancing %s to %s", unitID, target)
		require.Equal(t, target, resp.State)
	}
}

// requireChain pins one unit's complete typed history inside a Completed
// Sale's preparation_history: exactly the given prior→resulting pairs, in
// occurrence order, every row carrying real actor and session identity.
func requireChain(t *testing.T, sale sales.CompletedSaleResponse, unitID uuid.UUID,
	pairs [][2]string,
) {
	t.Helper()
	transitions := make([]sales.PreparationTransitionResponse, 0, len(pairs))
	for _, transition := range sale.PreparationHistory {
		if transition.UnitID == unitID {
			transitions = append(transitions, transition)
		}
	}
	require.Len(t, transitions, len(pairs),
		"unit %s must carry exactly its typed history", unitID)
	for i, transition := range transitions {
		require.Equal(t, pairs[i][0], transition.PriorState, "unit %s transition %d", unitID, i)
		require.Equal(t, pairs[i][1], transition.ResultingState, "unit %s transition %d", unitID, i)
		require.NotEqual(t, uuid.Nil, transition.ActorStaffID, "unit %s transition %d", unitID, i)
		require.NotEqual(t, uuid.Nil, transition.StaffSessionID, "unit %s transition %d", unitID, i)
	}
}

// TestCorrectionsE2EWasteAcknowledgeRemakeClose drives the first approved
// workflow: Submit -> advance -> Waste -> acknowledge -> Remake -> fulfill ->
// close -> read the Completed Sale. It pins the Waste fact stack, the
// alert-only visibility of the wasted unit, the linked REMAKE replacement and
// its priority lane, the untouched money, and the immutable metadata and
// typed history the Completed Sale publishes.
func TestCorrectionsE2EWasteAcknowledgeRemakeClose(t *testing.T) {
	env := newPrepEnv(t)
	ctx := context.Background()
	manager := env.salesActor(env.ManagerActor())

	units := env.SubmittedDressedUnits(t, 2)
	wasted, survivor := units[0], units[1]
	numbers := env.OrderItemUnitNumbers(t, wasted.ID)
	require.Equal(t, []int32{1, 2}, numbers, "two originals carry numbers 1 and 2")

	sessionID := env.SessionIDForUnit(t, wasted.ID)
	moneyBefore := env.SessionFinancials(t, sessionID)

	// --- Advance one unit into preparation. ---
	advanced, _, err := env.Advance(t, wasted.ID, preparation.StateInPreparation)
	require.NoError(t, err)
	require.NotNil(t, advanced.InPreparationAt)
	require.Equal(t, preparation.PriorityStandard, advanced.Priority)
	require.Nil(t, advanced.RemakeOfPreparationUnitID, "originals carry no remake link")

	// --- Waste: one transaction writes the fact, the state, the transition,
	// the alert, and both audits, and touches no money. ---
	wasteNote := "Làm lỡ đơn"
	wasteResp, wasteStatus, err := env.Waste(t, wasted.ID,
		preparation.ReasonQualityFailure, &wasteNote)
	require.NoError(t, err)
	require.Equal(t, 201, wasteStatus)
	require.Equal(t, preparation.StateInPreparation, wasteResp.PriorState)
	require.Equal(t, preparation.StateWasted, wasteResp.ResultingState)
	require.Equal(t, preparation.ReasonQualityFailure, wasteResp.Reason)
	require.Equal(t, wasted.ID, wasteResp.PreparationUnitID)
	require.Equal(t, preparation.StateWasted, env.UnitState(t, wasted.ID))
	require.Equal(t, 1, env.CountWastes(t, wasted.ID))
	require.Equal(t, 1, env.CountTransitionsTo(t, wasted.ID, preparation.StateWasted))
	require.Equal(t, 1, env.CountAlerts(t, wasted.ID))
	require.Equal(t, 1, env.CountAuditEventsByTypeAndUnit(t,
		preparation.EventPreparationUnitWasted, wasted.ID))
	require.Equal(t, 1, env.CountAuditEventsByTypeAndUnit(t,
		preparation.EventPreparationAlertCreated, wasted.ID))
	fact := env.UnitWaste(t, wasted.ID)
	require.Equal(t, env.BaristaActor().StaffID, fact.ActorID)
	require.Equal(t, env.BaristaActor().SessionID, fact.SessionID)
	requireMoneyUnchanged(t, env, sessionID, moneyBefore, "Waste")

	// Queue boundary: the wasted unit stays visible only through its active
	// alert, in the retained lane behind the live work; the history shows the
	// WASTE fact.
	queue := readQueue(t, env)
	require.Len(t, queue.Units, 2, "the live survivor plus the alert-retained wasted unit")
	_, wastedAt := queueUnitByID(t, queue, wasted.ID)
	_, survivorAt := queueUnitByID(t, queue, survivor.ID)
	require.Greater(t, wastedAt, survivorAt,
		"the alert-retained wasted unit sits behind the live standard unit")
	require.Equal(t, preparation.StateWasted, queue.Units[wastedAt].State)
	require.Len(t, queue.Alerts, 1)
	alert := queueAlertByID(t, queue, wasteResp.Alert.ID)
	require.Equal(t, preparation.AlertKindWaste, alert.Kind)
	require.Equal(t, wasted.ID, alert.PreparationUnitID)
	require.Equal(t, wasteResp.ID, *alert.WasteID,
		"a WASTE alert resolves its waste fact")
	require.Equal(t, preparation.ReasonQualityFailure, alert.Reason)
	require.Len(t, queue.Corrections, 1)
	history := queueCorrectionByID(t, queue, wasteResp.ID)
	require.Equal(t, "WASTE", history.EntryKind)
	require.Equal(t, wasted.ID, history.PreparationUnitID)
	require.Nil(t, history.WasteID, "a WASTE entry carries no remake linkage")
	require.Equal(t, wasted.UnitNumber, history.UnitNumber)
	require.Equal(t, wasteNote, *history.Note)

	// --- Acknowledge: records evidence and removes the wasted unit from the
	// queue; nothing else moves. ---
	ackResp, ackStatus, err := env.Acknowledge(t, wasteResp.Alert.ID)
	require.NoError(t, err)
	require.Equal(t, 200, ackStatus)
	require.NotNil(t, ackResp.AcknowledgedAt)
	require.NotNil(t, ackResp.AcknowledgedByStaffIdentityID)
	require.Equal(t, env.BaristaActor().StaffID, *ackResp.AcknowledgedByStaffIdentityID)
	acknowledgment := env.AlertAcknowledgment(t, wasteResp.Alert.ID)
	require.NotNil(t, acknowledgment.AcknowledgedAt)
	require.NotNil(t, acknowledgment.AcknowledgedBy)
	require.Equal(t, env.BaristaActor().StaffID, *acknowledgment.AcknowledgedBy)
	require.Equal(t, env.BaristaActor().SessionID, *acknowledgment.AcknowledgedSessionID)
	require.Equal(t, preparation.StateWasted, env.UnitState(t, wasted.ID),
		"acknowledgment never changes unit state")
	requireMoneyUnchanged(t, env, sessionID, moneyBefore, "Acknowledge")

	queue = readQueue(t, env)
	require.Len(t, queue.Units, 1, "only the survivor remains after acknowledgment")
	for _, unit := range queue.Units {
		require.NotEqual(t, wasted.ID, unit.ID,
			"the acknowledged wasted unit must not appear on the queue")
	}
	require.Empty(t, queue.Alerts, "the acknowledged alert leaves the queue")
	require.Len(t, queue.Corrections, 1, "the WASTE history survives acknowledgment")
	require.Equal(t, 1, env.CountAuditEventsByTypeAndUnit(t,
		preparation.EventPreparationAlertAcknowledged, wasted.ID))

	// --- Remake: one linked replacement, next number, copied snapshot,
	// REMAKE priority, and no new charge anywhere. ---
	remakeNote := "Làm lại"
	remakeResp, remakeStatus, err := env.Remake(t, wasteResp.ID,
		preparation.ReasonPreparationError, &remakeNote)
	require.NoError(t, err)
	require.Equal(t, 201, remakeStatus)
	require.Equal(t, wasteResp.ID, remakeResp.WasteID)
	require.Equal(t, wasted.ID, remakeResp.SourcePreparationUnitID)
	replacement := remakeResp.Unit
	require.Equal(t, preparation.StateQueued, replacement.State)
	require.Equal(t, int32(3), replacement.UnitNumber, "the replacement takes the next number")
	require.Equal(t, preparation.PriorityRemake, replacement.Priority)
	require.NotNil(t, replacement.RemakeOfPreparationUnitID)
	require.Equal(t, wasted.ID, *replacement.RemakeOfPreparationUnitID)
	require.Equal(t, wasted.ServiceNumber, replacement.ServiceNumber,
		"the replacement copies the source's immutable snapshot")
	require.Equal(t, wasted.ItemName, replacement.ItemName)
	require.Equal(t, wasted.CategoryName, replacement.CategoryName)
	require.Equal(t, wasted.SizeName, replacement.SizeName)
	require.Equal(t, wasted.PreparationNote, replacement.PreparationNote)
	require.Len(t, replacement.Modifiers, len(wasted.Modifiers))
	for i := range wasted.Modifiers {
		require.Equal(t, wasted.Modifiers[i].GroupName, replacement.Modifiers[i].GroupName)
		require.Equal(t, wasted.Modifiers[i].OptionName, replacement.Modifiers[i].OptionName)
	}
	require.Equal(t, 1, env.CountRemakes(t, wasteResp.ID))
	require.Equal(t, []int32{1, 2, 3}, env.OrderItemUnitNumbers(t, wasted.ID))
	require.Equal(t, 0, env.CountAuditEventsByTypeAndUnit(t,
		preparation.EventPreparationRemakeCreated, wasted.ID),
		"the remake audit names the replacement, not the source")
	require.Equal(t, 1, env.CountAuditEventsByTypeAndUnit(t,
		preparation.EventPreparationRemakeCreated, replacement.ID))
	requireMoneyUnchanged(t, env, sessionID, moneyBefore, "Remake")

	queue = readQueue(t, env)
	replacementView, replacementAt := queueUnitByID(t, queue, replacement.ID)
	require.Equal(t, preparation.PriorityRemake, replacementView.Priority)
	require.Equal(t, wasted.ID, *replacementView.RemakeOfPreparationUnitID)
	_, survivorAt = queueUnitByID(t, queue, survivor.ID)
	require.Less(t, replacementAt, survivorAt,
		"the active remake sorts ahead of ordinary standard work")
	require.Empty(t, queue.Alerts)
	require.Len(t, queue.Corrections, 2)
	remakeEntry := queueCorrectionByID(t, queue, remakeResp.ID)
	require.Equal(t, "REMAKE", remakeEntry.EntryKind)
	require.Equal(t, wasteResp.ID, *remakeEntry.WasteID)
	require.Equal(t, wasted.ID, *remakeEntry.SourcePreparationUnitID)
	require.Equal(t, wasted.UnitNumber, *remakeEntry.SourceUnitNumber)
	require.Equal(t, replacement.ID, remakeEntry.PreparationUnitID)
	require.Equal(t, replacement.UnitNumber, remakeEntry.UnitNumber)

	// --- Fulfill both live units; the wasted unit is already terminal. ---
	advanceSteps(t, env, replacement.ID,
		preparation.StateInPreparation, preparation.StateReady, preparation.StateFulfilled)
	advanceSteps(t, env, survivor.ID,
		preparation.StateInPreparation, preparation.StateReady, preparation.StateFulfilled)

	// Terminal readiness: every unit is terminal, so closure readiness names
	// no nonterminal unit; the Check is still open until settlement.
	ready := env.SessionFinancials(t, sessionID)
	require.Empty(t, ready.NonterminalUnitIDs, "wasted and fulfilled units are all terminal")

	// --- Close through the real Sales handlers, then read the Completed
	// Sale. ---
	env.SettleAndCloseSession(t, sessionID)
	_, sale, err := sales.NewGetCompletedSaleBySessionHandler(env.SalesRunner).
		Handle(ctx, manager, sessionID)
	require.NoError(t, err)
	require.Equal(t, sales.CompletedSaleStateCompleted, sale.State)
	require.Equal(t, "CLOSED", sale.ServiceSessionState)

	// Metadata: originals stay STANDARD with a constant null link; only the
	// replacement is REMAKE pointing at the exact wasted source.
	require.Len(t, sale.PreparationUnits, 3)
	for _, unit := range sale.PreparationUnits {
		switch unit.ID {
		case replacement.ID:
			require.Equal(t, preparation.PriorityRemake, unit.Priority)
			require.NotNil(t, unit.RemakeOfPreparationUnitID)
			require.Equal(t, wasted.ID, *unit.RemakeOfPreparationUnitID)
		default:
			require.Equal(t, preparation.PriorityStandard, unit.Priority)
			require.Nil(t, unit.RemakeOfPreparationUnitID)
		}
	}

	// Complete typed history: every transition of every unit, and no others.
	require.Len(t, sale.PreparationHistory, 8)
	requireChain(t, sale, wasted.ID, [][2]string{
		{preparation.StateQueued, preparation.StateInPreparation},
		{preparation.StateInPreparation, preparation.StateWasted},
	})
	requireChain(t, sale, replacement.ID, [][2]string{
		{preparation.StateQueued, preparation.StateInPreparation},
		{preparation.StateInPreparation, preparation.StateReady},
		{preparation.StateReady, preparation.StateFulfilled},
	})
	requireChain(t, sale, survivor.ID, [][2]string{
		{preparation.StateQueued, preparation.StateInPreparation},
		{preparation.StateInPreparation, preparation.StateReady},
		{preparation.StateReady, preparation.StateFulfilled},
	})

	// The money the sale publishes is exactly what Submit charged: two
	// surcharged units, fully settled, zero balance.
	require.Len(t, sale.Checks, 1)
	require.Equal(t, int64(60000), sale.Checks[0].ChargeVND)
	require.Equal(t, int64(60000), sale.Checks[0].TotalAppliedVND)
	require.Equal(t, int64(0), sale.Checks[0].BalanceVND)
	require.Equal(t, "SETTLED", sale.Checks[0].State)
}

// TestCorrectionsE2ECorrectBackwardReAdvanceClose drives the second approved
// workflow: Submit -> advance -> correct backward -> re-advance -> close. It
// pins the atomic Manager-only batch, the one-step reverse transitions, the
// cleared-then-reset preparation start, the untouched money, and the complete
// corrected history the Completed Sale publishes.
func TestCorrectionsE2ECorrectBackwardReAdvanceClose(t *testing.T) {
	env := newPrepEnv(t)
	manager := env.ManagerActor()

	units := env.SubmittedUnits(t, 2)
	first, second := units[0], units[1]
	sessionID := env.SessionIDForUnit(t, first.ID)
	moneyBefore := env.SessionFinancials(t, sessionID)

	// --- Advance both units to READY. ---
	advanceSteps(t, env, first.ID, preparation.StateInPreparation, preparation.StateReady)
	advanceSteps(t, env, second.ID, preparation.StateInPreparation, preparation.StateReady)
	firstStart := env.UnitInPreparationAt(t, first.ID)
	secondStart := env.UnitInPreparationAt(t, second.ID)
	require.NotNil(t, firstStart)
	require.NotNil(t, secondStart)

	// --- Correct both backward one step to IN_PREPARATION, atomically, as
	// the Manager with their own PIN. ---
	correctionNote := "Trạng thái ghi nhầm"
	correctResp, correctStatus, err := env.CorrectState(t, preparation.CorrectStateCommand{
		PreparationUnitIDs: []uuid.UUID{first.ID, second.ID},
		TargetState:        preparation.StateInPreparation,
		Reason:             preparation.ReasonStateRecordedInError,
		Note:               &correctionNote,
	})
	require.NoError(t, err)
	require.Equal(t, 200, correctStatus)
	require.Len(t, correctResp.Outcomes, 2)
	require.Equal(t, first.ID, correctResp.Outcomes[0].PreparationUnitID,
		"outcomes follow the request order")
	require.Equal(t, second.ID, correctResp.Outcomes[1].PreparationUnitID)
	for i, outcome := range correctResp.Outcomes {
		require.Equal(t, preparation.StateReady, outcome.PriorState, "outcome %d", i)
		require.Equal(t, preparation.StateInPreparation, outcome.ResultingState, "outcome %d", i)
	}
	require.NotEqual(t, correctResp.Outcomes[0].CorrectionID, correctResp.Outcomes[1].CorrectionID)
	starts := map[uuid.UUID]*time.Time{first.ID: firstStart, second.ID: secondStart}
	for _, unit := range []sales.PreparationUnitResponse{first, second} {
		require.Equal(t, preparation.StateInPreparation, env.UnitState(t, unit.ID))
		require.Equal(t, 1, env.CountCorrections(t, unit.ID))
		// Each unit already recorded QUEUED->IN_PREPARATION when the round
		// was advanced, so the reverse correction is its second transition
		// into IN_PREPARATION and its only one into READY.
		require.Equal(t, 2, env.CountTransitionsTo(t, unit.ID, preparation.StateInPreparation))
		require.Equal(t, 1, env.CountTransitionsTo(t, unit.ID, preparation.StateReady))
		require.Equal(t, 3, env.CountTransitions(t, unit.ID))
		correction := env.UnitCorrection(t, unit.ID)
		require.Equal(t, preparation.StateReady, correction.PriorState)
		require.Equal(t, preparation.ReasonStateRecordedInError, correction.Reason)
		require.Equal(t, manager.StaffID, correction.ActorID)
		require.Equal(t, manager.SessionID, correction.SessionID)
		require.Equal(t, 1, env.CountAuditEventsByTypeAndUnit(t,
			preparation.EventPreparationStateCorrected, unit.ID))
		require.Equal(t, starts[unit.ID], env.UnitInPreparationAt(t, unit.ID),
			"a correction away from QUEUED keeps the recorded preparation start")
	}
	requireMoneyUnchanged(t, env, sessionID, moneyBefore, "CorrectState")

	// Queue boundary: both corrected units are ordinary standard work again.
	queue := readQueue(t, env)
	require.Len(t, queue.Units, 2)
	for _, unit := range []sales.PreparationUnitResponse{first, second} {
		view, _ := queueUnitByID(t, queue, unit.ID)
		require.Equal(t, preparation.StateInPreparation, view.State)
		require.Equal(t, preparation.PriorityStandard, view.Priority)
		require.Nil(t, view.RemakeOfPreparationUnitID)
	}
	require.Empty(t, queue.Alerts)
	require.Empty(t, queue.Corrections, "corrections write no queue history entry")

	// --- Re-advance both units to FULFILLED. ---
	advanceSteps(t, env, first.ID, preparation.StateReady, preparation.StateFulfilled)
	advanceSteps(t, env, second.ID, preparation.StateReady, preparation.StateFulfilled)
	ready := env.SessionFinancials(t, sessionID)
	require.Empty(t, ready.NonterminalUnitIDs, "every unit is terminal after re-advance")

	// --- Close through the real Sales handlers, then read the Completed
	// Sale. ---
	env.SettleAndCloseSession(t, sessionID)
	_, sale, err := sales.NewGetCompletedSaleBySessionHandler(env.SalesRunner).
		Handle(context.Background(), env.salesActor(manager), sessionID)
	require.NoError(t, err)
	require.Equal(t, sales.CompletedSaleStateCompleted, sale.State)

	require.Len(t, sale.PreparationUnits, 2)
	for _, unit := range sale.PreparationUnits {
		require.Equal(t, preparation.PriorityStandard, unit.Priority)
		require.Nil(t, unit.RemakeOfPreparationUnitID)
	}

	// Complete typed history: the correction's reverse transition is real
	// business history, ordered between the advance that preceded it and the
	// re-advance that followed.
	require.Len(t, sale.PreparationHistory, 10)
	for _, unit := range []sales.PreparationUnitResponse{first, second} {
		requireChain(t, sale, unit.ID, [][2]string{
			{preparation.StateQueued, preparation.StateInPreparation},
			{preparation.StateInPreparation, preparation.StateReady},
			{preparation.StateReady, preparation.StateInPreparation},
			{preparation.StateInPreparation, preparation.StateReady},
			{preparation.StateReady, preparation.StateFulfilled},
		})
	}

	require.Len(t, sale.Checks, 1)
	require.Equal(t, int64(50000), sale.Checks[0].ChargeVND,
		"two plainly priced units, and no correction ever changed a charge")
	require.Equal(t, int64(50000), sale.Checks[0].TotalAppliedVND)
	require.Equal(t, int64(0), sale.Checks[0].BalanceVND)
}

// TestCorrectionsE2EChainedRemakeClose drives the third approved workflow:
// Wasted Remake -> chained Remake -> fulfill -> close. A replacement may
// itself be wasted and remade, so each chain link is its own Waste fact and
// Remake fact, every replacement points at its exact source, and closure
// treats the whole chain as terminal.
func TestCorrectionsE2EChainedRemakeClose(t *testing.T) {
	env := newPrepEnv(t)

	units := env.SubmittedDressedUnits(t, 1)
	original := units[0]
	sessionID := env.SessionIDForUnit(t, original.ID)
	moneyBefore := env.SessionFinancials(t, sessionID)
	require.Equal(t, []int32{1}, env.OrderItemUnitNumbers(t, original.ID))

	// --- Waste the original and remake it. ---
	advanceSteps(t, env, original.ID, preparation.StateInPreparation)
	wasteNote := "Đổ ra ngoài"
	firstWaste, _, err := env.Waste(t, original.ID,
		preparation.ReasonPreparationError, &wasteNote)
	require.NoError(t, err)
	require.Equal(t, preparation.StateWasted, env.UnitState(t, original.ID))

	firstRemake, _, err := env.Remake(t, firstWaste.ID,
		preparation.ReasonPreparationError, nil)
	require.NoError(t, err)
	first := firstRemake.Unit
	require.Equal(t, int32(2), first.UnitNumber)
	require.Equal(t, preparation.PriorityRemake, first.Priority)
	require.Equal(t, original.ID, *first.RemakeOfPreparationUnitID)
	require.Equal(t, original.PreparationNote, first.PreparationNote,
		"the snapshot copies through the first link")

	// --- Waste the replacement and chain a second Remake from its waste. ---
	advanceSteps(t, env, first.ID, preparation.StateInPreparation)
	secondWasteNote := "Làm sai công thức"
	secondWaste, _, err := env.Waste(t, first.ID,
		preparation.ReasonQualityFailure, &secondWasteNote)
	require.NoError(t, err)
	require.Equal(t, preparation.StateWasted, env.UnitState(t, first.ID))

	// Queue boundary between the links: both wasted units are retained by
	// their active alerts, both alerts are oldest first, and the history is
	// newest first across both entry kinds.
	queue := readQueue(t, env)
	require.Len(t, queue.Units, 2, "both wasted units remain visible behind their alerts")
	for _, unitID := range []uuid.UUID{original.ID, first.ID} {
		_, at := queueUnitByID(t, queue, unitID)
		require.Equal(t, preparation.StateWasted, queue.Units[at].State)
	}
	require.Len(t, queue.Alerts, 2)
	require.Equal(t, firstWaste.Alert.ID, queue.Alerts[0].ID,
		"alerts are oldest first")
	require.Equal(t, secondWaste.Alert.ID, queue.Alerts[1].ID)
	require.Len(t, queue.Corrections, 3,
		"two WASTE facts and the first link's REMAKE fact")
	require.Equal(t, secondWaste.ID, queue.Corrections[0].ID, "history is newest first")
	require.Equal(t, firstRemake.ID, queue.Corrections[1].ID)
	require.Equal(t, firstWaste.ID, queue.Corrections[2].ID)

	secondRemake, _, err := env.Remake(t, secondWaste.ID,
		preparation.ReasonOther, &secondWasteNote)
	require.NoError(t, err)
	second := secondRemake.Unit
	require.Equal(t, int32(3), second.UnitNumber, "each link takes the next number")
	require.Equal(t, preparation.PriorityRemake, second.Priority)
	require.NotNil(t, second.RemakeOfPreparationUnitID)
	require.Equal(t, first.ID, *second.RemakeOfPreparationUnitID,
		"a chained remake links to its exact wasted source")
	require.Equal(t, original.PreparationNote, second.PreparationNote,
		"the snapshot copies through the second link")
	require.Equal(t, []int32{1, 2, 3}, env.OrderItemUnitNumbers(t, original.ID))
	requireMoneyUnchanged(t, env, sessionID, moneyBefore, "Chained Remake")

	// --- Fulfill the final replacement; both wasted links are terminal. ---
	advanceSteps(t, env, second.ID,
		preparation.StateInPreparation, preparation.StateReady, preparation.StateFulfilled)
	ready := env.SessionFinancials(t, sessionID)
	require.Empty(t, ready.NonterminalUnitIDs,
		"the whole chain is terminal: two wasted links, one fulfilled replacement")

	env.SettleAndCloseSession(t, sessionID)
	_, sale, err := sales.NewGetCompletedSaleBySessionHandler(env.SalesRunner).
		Handle(context.Background(), env.salesActor(env.ManagerActor()), sessionID)
	require.NoError(t, err)
	require.Equal(t, sales.CompletedSaleStateCompleted, sale.State)

	require.Len(t, sale.PreparationUnits, 3)
	for _, unit := range sale.PreparationUnits {
		switch unit.ID {
		case original.ID:
			require.Equal(t, preparation.PriorityStandard, unit.Priority)
			require.Nil(t, unit.RemakeOfPreparationUnitID)
		case first.ID:
			require.Equal(t, preparation.PriorityRemake, unit.Priority)
			require.Equal(t, original.ID, *unit.RemakeOfPreparationUnitID)
		case second.ID:
			require.Equal(t, preparation.PriorityRemake, unit.Priority)
			require.Equal(t, first.ID, *unit.RemakeOfPreparationUnitID)
		}
	}

	require.Len(t, sale.PreparationHistory, 7)
	requireChain(t, sale, original.ID, [][2]string{
		{preparation.StateQueued, preparation.StateInPreparation},
		{preparation.StateInPreparation, preparation.StateWasted},
	})
	requireChain(t, sale, first.ID, [][2]string{
		{preparation.StateQueued, preparation.StateInPreparation},
		{preparation.StateInPreparation, preparation.StateWasted},
	})
	requireChain(t, sale, second.ID, [][2]string{
		{preparation.StateQueued, preparation.StateInPreparation},
		{preparation.StateInPreparation, preparation.StateReady},
		{preparation.StateReady, preparation.StateFulfilled},
	})

	require.Len(t, sale.Checks, 1)
	require.Equal(t, int64(30000), sale.Checks[0].ChargeVND,
		"one surcharged unit, and no remake ever added a charge")
	require.Equal(t, int64(30000), sale.Checks[0].TotalAppliedVND)
	require.Equal(t, int64(0), sale.Checks[0].BalanceVND)
}
