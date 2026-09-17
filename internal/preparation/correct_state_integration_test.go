//go:build integration

package preparation_test

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/preparation"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// advanceTo walks one unit forward to the target state through the legal
// chain, so suites can stage a unit at IN_PREPARATION, READY, or FULFILLED.
func advanceTo(t *testing.T, env *prepEnv, unitID uuid.UUID, target string) {
	t.Helper()
	for {
		state := env.UnitState(t, unitID)
		if state == target {
			return
		}
		next, ok := preparation.NextState(state)
		require.True(t, ok, "a unit in %s can never reach %s", state, target)
		_, _, err := env.Advance(t, unitID, next)
		require.NoError(t, err)
	}
}

// correctCmd builds a valid correction command for the given ids.
func correctCmd(ids []uuid.UUID, target, reason string, note *string) preparation.CorrectStateCommand {
	return preparation.CorrectStateCommand{
		PreparationUnitIDs: ids,
		TargetState:        target,
		Reason:             reason,
		Note:               note,
		ManagerPIN:         "1234",
	}
}

func TestCorrectState(t *testing.T) {
	t.Run("IN_PREPARATION to QUEUED corrects one step and answers exact fields", func(t *testing.T) {
		env := newPrepEnv(t)
		unit := env.SubmittedUnits(t, 1)[0]
		_, _, err := env.Advance(t, unit.ID, preparation.StateInPreparation)
		require.NoError(t, err)
		require.NotNil(t, env.UnitInPreparationAt(t, unit.ID),
			"the unit really did enter preparation before the correction")

		requestID := uuid.New()
		resp, status, err := env.CorrectStateWithRequestID(t, requestID,
			correctCmd([]uuid.UUID{unit.ID}, preparation.StateQueued,
				preparation.ReasonStateRecordedInError, notePtr("  chốt nhầm  ")))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, status, "a correction answers 200")

		// Exact response fields, in request order.
		require.NotNil(t, resp.Outcomes)
		require.Len(t, resp.Outcomes, 1)
		outcome := resp.Outcomes[0]
		require.NotEqual(t, uuid.Nil, outcome.CorrectionID)
		require.Equal(t, unit.ID, outcome.PreparationUnitID)
		require.Equal(t, preparation.StateInPreparation, outcome.PriorState)
		require.Equal(t, preparation.StateQueued, outcome.ResultingState)
		require.NotZero(t, outcome.CorrectedAt)

		// The unit sits back at QUEUED and its preparation start is cleared.
		require.Equal(t, preparation.StateQueued, env.UnitState(t, unit.ID))
		require.Nil(t, env.UnitInPreparationAt(t, unit.ID),
			"a correction to QUEUED clears in_preparation_at")

		// Exactly one fact, carrying the actor, the trimmed note, and the
		// shared instant.
		require.Equal(t, 1, env.CountCorrections(t, unit.ID))
		fact := env.UnitCorrection(t, unit.ID)
		require.Equal(t, outcome.CorrectionID, fact.ID)
		require.Equal(t, preparation.StateInPreparation, fact.PriorState)
		require.Equal(t, preparation.StateQueued, fact.ResultingState)
		require.Equal(t, preparation.ReasonStateRecordedInError, fact.Reason)
		require.NotNil(t, fact.Note)
		require.Equal(t, "chốt nhầm", *fact.Note, "the note is stored trimmed")
		require.Equal(t, env.ManagerActor().StaffID, fact.ActorID)
		require.Equal(t, env.ManagerActor().SessionID, fact.SessionID)
		require.True(t, fact.OccurredAt.Equal(outcome.CorrectedAt),
			"the fact and the outcome share one instant")

		// One reverse transition at that same instant.
		require.Equal(t, 1, env.CountTransitionsTo(t, unit.ID, preparation.StateQueued))
		transition := env.UnitTransitionTo(t, unit.ID, preparation.StateQueued)
		require.Equal(t, preparation.StateInPreparation, transition.PriorState)
		require.True(t, transition.OccurredAt.Equal(outcome.CorrectedAt))

		// One PREPARATION_STATE_CORRECTED audit naming the unit, at that
		// same instant.
		require.Equal(t, 1, env.CountAuditEventsByTypeAndUnit(
			t, preparation.EventPreparationStateCorrected, unit.ID))
		audits := env.UnitAuditEvents(t, unit.ID, preparation.EventPreparationStateCorrected)
		require.Len(t, audits, 1)
		require.True(t, audits[0].OccurredAt.Equal(outcome.CorrectedAt))

		// The exact stored result, replayable.
		claim, ok := env.IdempotencyClaim(t, env.ManagerActor(), requestID)
		require.True(t, ok, "a success stores its replayable result")
		require.Equal(t, preparation.OpCorrectState, claim.Action)
		require.Equal(t, int32(http.StatusOK), claim.ResponseCode)
		respJSON, err := json.Marshal(resp)
		require.NoError(t, err)
		require.JSONEq(t, string(respJSON), string(claim.ResponseBody))
	})

	t.Run("READY to IN_PREPARATION preserves the preparation start", func(t *testing.T) {
		env := newPrepEnv(t)
		unit := env.SubmittedUnits(t, 1)[0]
		advanceTo(t, env, unit.ID, preparation.StateReady)
		startedAt := env.UnitInPreparationAt(t, unit.ID)
		require.NotNil(t, startedAt)

		resp, status, err := env.CorrectState(t,
			correctCmd([]uuid.UUID{unit.ID}, preparation.StateInPreparation,
				preparation.ReasonOther, notePtr("bấm lọt máy")))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, status)
		require.Len(t, resp.Outcomes, 1)
		require.Equal(t, preparation.StateReady, resp.Outcomes[0].PriorState)
		require.Equal(t, preparation.StateInPreparation, resp.Outcomes[0].ResultingState)

		require.Equal(t, preparation.StateInPreparation, env.UnitState(t, unit.ID))
		require.NotNil(t, env.UnitInPreparationAt(t, unit.ID),
			"a correction to IN_PREPARATION keeps in_preparation_at")
		require.True(t, startedAt.Equal(*env.UnitInPreparationAt(t, unit.ID)),
			"the recorded preparation start survives the correction")
	})

	t.Run("FULFILLED to READY keeps the preparation start", func(t *testing.T) {
		env := newPrepEnv(t)
		unit := env.SubmittedUnits(t, 1)[0]
		advanceTo(t, env, unit.ID, preparation.StateFulfilled)
		startedAt := env.UnitInPreparationAt(t, unit.ID)
		require.NotNil(t, startedAt)

		resp, status, err := env.CorrectState(t,
			correctCmd([]uuid.UUID{unit.ID}, preparation.StateReady,
				preparation.ReasonStateRecordedInError, nil))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, status)
		require.Len(t, resp.Outcomes, 1)
		require.Equal(t, preparation.StateFulfilled, resp.Outcomes[0].PriorState)
		require.Equal(t, preparation.StateReady, resp.Outcomes[0].ResultingState)

		require.Equal(t, preparation.StateReady, env.UnitState(t, unit.ID))
		preserved := env.UnitInPreparationAt(t, unit.ID)
		require.NotNil(t, preserved)
		require.True(t, startedAt.Equal(*preserved),
			"the recorded preparation start survives the correction")
	})

	t.Run("one instant and exactly one fact, transition, and audit per unit across a batch", func(t *testing.T) {
		env := newPrepEnv(t)
		units := env.SubmittedUnits(t, 3)
		ids := []uuid.UUID{units[0].ID, units[1].ID, units[2].ID}
		_, _, err := env.BulkAdvance(t, preparation.BulkAdvanceCommand{
			RequestID: uuid.New(), PreparationUnitIDs: ids,
			TargetState: preparation.StateInPreparation,
		})
		require.NoError(t, err)

		// Request order deliberately reversed relative to submission order.
		request := []uuid.UUID{units[2].ID, units[0].ID, units[1].ID}
		resp, status, err := env.CorrectState(t,
			correctCmd(request, preparation.StateQueued,
				preparation.ReasonStateRecordedInError, nil))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, status)

		// Outcomes come back in original request order, never lock order.
		require.Len(t, resp.Outcomes, len(request))
		for i, outcome := range resp.Outcomes {
			require.Equal(t, request[i], outcome.PreparationUnitID,
				"outcome %d must follow the request order", i)
			require.Equal(t, preparation.StateInPreparation, outcome.PriorState)
			require.Equal(t, preparation.StateQueued, outcome.ResultingState)
		}

		// One timestamp across the whole batch.
		for _, outcome := range resp.Outcomes[1:] {
			require.True(t, resp.Outcomes[0].CorrectedAt.Equal(outcome.CorrectedAt),
				"every outcome of one batch shares one corrected_at")
		}

		for _, id := range request {
			require.Equal(t, preparation.StateQueued, env.UnitState(t, id))
			require.Nil(t, env.UnitInPreparationAt(t, id))
			require.Equal(t, 1, env.CountCorrections(t, id),
				"exactly one correction fact per selected unit")
			require.True(t, env.UnitCorrection(t, id).OccurredAt.Equal(resp.Outcomes[0].CorrectedAt))
			require.Equal(t, 1, env.CountTransitionsTo(t, id, preparation.StateQueued))
			require.True(t, env.UnitTransitionTo(t, id, preparation.StateQueued).
				OccurredAt.Equal(resp.Outcomes[0].CorrectedAt))
			audits := env.UnitAuditEvents(t, id, preparation.EventPreparationStateCorrected)
			require.Len(t, audits, 1, "exactly one audit per selected unit")
			require.True(t, audits[0].OccurredAt.Equal(resp.Outcomes[0].CorrectedAt))
		}
	})

	t.Run("fifty units correct in one batch", func(t *testing.T) {
		env := newPrepEnv(t)
		units := env.SubmittedUnits(t, 50)
		require.Len(t, units, 50)
		ids := make([]uuid.UUID, 0, len(units))
		for _, unit := range units {
			ids = append(ids, unit.ID)
		}
		_, _, err := env.BulkAdvance(t, preparation.BulkAdvanceCommand{
			RequestID: uuid.New(), PreparationUnitIDs: ids,
			TargetState: preparation.StateInPreparation,
		})
		require.NoError(t, err)

		// Submit in reverse byte order, so the lock order the handler takes
		// is the opposite of the request order.
		request := append([]uuid.UUID(nil), ids...)
		for i, j := 0, len(request)-1; i < j; i, j = i+1, j-1 {
			request[i], request[j] = request[j], request[i]
		}

		resp, status, err := env.CorrectState(t,
			correctCmd(request, preparation.StateQueued,
				preparation.ReasonStateRecordedInError, nil))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, status)
		require.Len(t, resp.Outcomes, 50)
		for i, outcome := range resp.Outcomes {
			require.Equal(t, request[i], outcome.PreparationUnitID,
				"outcomes keep the original request order")
			require.Equal(t, preparation.StateQueued, outcome.ResultingState)
		}
		for _, id := range ids {
			require.Equal(t, preparation.StateQueued, env.UnitState(t, id))
			require.Equal(t, 1, env.CountCorrections(t, id))
			require.Equal(t, 1, env.CountTransitionsTo(t, id, preparation.StateQueued))
			require.Equal(t, 1, env.CountAuditEventsByTypeAndUnit(
				t, preparation.EventPreparationStateCorrected, id))
		}
		require.Equal(t, 50, env.CountAllCorrections(t))
	})

	t.Run("zero ids fail at the boundary", func(t *testing.T) {
		env := newPrepEnv(t)
		unit := env.SubmittedUnits(t, 1)[0]
		_, _, err := env.Advance(t, unit.ID, preparation.StateInPreparation)
		require.NoError(t, err)

		requestID := uuid.New()
		cmd := correctCmd([]uuid.UUID{}, preparation.StateQueued,
			preparation.ReasonStateRecordedInError, nil)
		cmd.RequestID = requestID
		_, status, err := env.CorrectState(t, cmd)
		require.ErrorIs(t, err, response.ErrInvalid)
		require.Equal(t, http.StatusBadRequest, status)

		require.Equal(t, preparation.StateInPreparation, env.UnitState(t, unit.ID))
		require.Equal(t, 0, env.CountAllCorrections(t))
		_, ok := env.IdempotencyClaim(t, env.ManagerActor(), requestID)
		require.False(t, ok, "a boundary failure never claims its request id")
	})

	t.Run("fifty-one ids fail at the boundary", func(t *testing.T) {
		env := newPrepEnv(t)
		tooMany := make([]uuid.UUID, 0, preparation.MaxCorrectionUnits+1)
		for i := 0; i <= preparation.MaxCorrectionUnits; i++ {
			tooMany = append(tooMany, uuid.New())
		}
		_, status, err := env.CorrectState(t,
			correctCmd(tooMany, preparation.StateQueued,
				preparation.ReasonStateRecordedInError, nil))
		require.ErrorIs(t, err, response.ErrInvalid)
		require.Equal(t, http.StatusBadRequest, status)
		require.Equal(t, 0, env.CountAllCorrections(t))
	})

	t.Run("duplicate ids fail at the boundary", func(t *testing.T) {
		env := newPrepEnv(t)
		unit := env.SubmittedUnits(t, 1)[0]
		_, _, err := env.Advance(t, unit.ID, preparation.StateInPreparation)
		require.NoError(t, err)

		_, status, err := env.CorrectState(t,
			correctCmd([]uuid.UUID{unit.ID, unit.ID}, preparation.StateQueued,
				preparation.ReasonStateRecordedInError, nil))
		require.ErrorIs(t, err, response.ErrInvalid)
		require.Equal(t, http.StatusBadRequest, status)

		require.Equal(t, preparation.StateInPreparation, env.UnitState(t, unit.ID))
		require.Equal(t, 0, env.CountAllCorrections(t))
	})

	t.Run("a zero UUID in the selection fails at the boundary", func(t *testing.T) {
		env := newPrepEnv(t)
		_, status, err := env.CorrectState(t,
			correctCmd([]uuid.UUID{uuid.New(), uuid.Nil}, preparation.StateQueued,
				preparation.ReasonStateRecordedInError, nil))
		require.ErrorIs(t, err, response.ErrInvalid)
		require.Equal(t, http.StatusBadRequest, status)
		require.Equal(t, 0, env.CountAllCorrections(t))
	})
}

func TestCorrectStateBatchAtomicity(t *testing.T) {
	// batchWorld stages two IN_PREPARATION units and returns them.
	batchWorld := func(t *testing.T) (*prepEnv, uuid.UUID, uuid.UUID) {
		t.Helper()
		env := newPrepEnv(t)
		units := env.SubmittedUnits(t, 2)
		_, _, err := env.BulkAdvance(t, preparation.BulkAdvanceCommand{
			RequestID:          uuid.New(),
			PreparationUnitIDs: []uuid.UUID{units[0].ID, units[1].ID},
			TargetState:        preparation.StateInPreparation,
		})
		require.NoError(t, err)
		return env, units[0].ID, units[1].ID
	}

	// expectNoWrites pins the all-or-nothing contract: every unit keeps its
	// state, no fact, audit, or claim exists, and no transition into the
	// target was added beyond the baseline the setup already created.
	expectNoWrites := func(t *testing.T, env *prepEnv, requestID uuid.UUID,
		states map[uuid.UUID]string, target string, transitionsBefore map[uuid.UUID]int,
	) {
		t.Helper()
		for id, state := range states {
			require.Equal(t, state, env.UnitState(t, id), "unit %s must be untouched", id)
			require.Equal(t, 0, env.CountCorrections(t, id))
			require.Equal(t, transitionsBefore[id], env.CountTransitionsTo(t, id, target),
				"unit %s must gain no transition into %s", id, target)
			require.Equal(t, 0, env.CountAuditEventsByTypeAndUnit(
				t, preparation.EventPreparationStateCorrected, id))
		}
		require.Equal(t, 0, env.CountAllCorrections(t))
		_, ok := env.IdempotencyClaim(t, env.ManagerActor(), requestID)
		require.False(t, ok, "a rejected batch never claims its request id")
	}
	// transitionsBefore snapshots the setup's transitions into the target
	// for every unit in states.
	transitionsBefore := func(t *testing.T, env *prepEnv,
		states map[uuid.UUID]string, target string,
	) map[uuid.UUID]int {
		t.Helper()
		before := make(map[uuid.UUID]int, len(states))
		for id := range states {
			before[id] = env.CountTransitionsTo(t, id, target)
		}
		return before
	}

	t.Run("a missing unit rejects the entire batch", func(t *testing.T) {
		env, first, second := batchWorld(t)
		states := map[uuid.UUID]string{
			first:  preparation.StateInPreparation,
			second: preparation.StateInPreparation,
		}
		before := transitionsBefore(t, env, states, preparation.StateQueued)
		requestID := uuid.New()
		cmd := correctCmd([]uuid.UUID{first, uuid.New(), second}, preparation.StateQueued,
			preparation.ReasonStateRecordedInError, nil)
		cmd.RequestID = requestID
		_, status, err := env.CorrectState(t, cmd)
		require.ErrorIs(t, err, preparation.ErrUnitNotFound)
		require.Equal(t, http.StatusNotFound, status)
		expectNoWrites(t, env, requestID, states, preparation.StateQueued, before)
	})

	t.Run("a stale state rejects the entire batch", func(t *testing.T) {
		env, first, second := batchWorld(t)
		advanceTo(t, env, second, preparation.StateReady)
		states := map[uuid.UUID]string{
			first:  preparation.StateInPreparation,
			second: preparation.StateReady,
		}
		before := transitionsBefore(t, env, states, preparation.StateQueued)
		requestID := uuid.New()
		cmd := correctCmd([]uuid.UUID{first, second}, preparation.StateQueued,
			preparation.ReasonStateRecordedInError, nil)
		cmd.RequestID = requestID
		_, status, err := env.CorrectState(t, cmd)
		require.ErrorIs(t, err, preparation.ErrInvalidTransition,
			"a correction to QUEUED requires IN_PREPARATION, and the whole batch answers for it")
		require.Equal(t, http.StatusConflict, status)
		expectNoWrites(t, env, requestID, states, preparation.StateQueued, before)
	})

	t.Run("an exceptional terminal state rejects the entire batch", func(t *testing.T) {
		env, first, second := batchWorld(t)
		_, _, err := env.Waste(t, second, preparation.ReasonQualityFailure, nil)
		require.NoError(t, err)
		states := map[uuid.UUID]string{
			first:  preparation.StateInPreparation,
			second: preparation.StateWasted,
		}
		before := transitionsBefore(t, env, states, preparation.StateQueued)
		requestID := uuid.New()
		cmd := correctCmd([]uuid.UUID{first, second}, preparation.StateQueued,
			preparation.ReasonStateRecordedInError, nil)
		cmd.RequestID = requestID
		_, status, err := env.CorrectState(t, cmd)
		require.ErrorIs(t, err, preparation.ErrInvalidTransition)
		require.Equal(t, http.StatusConflict, status)
		expectNoWrites(t, env, requestID, states, preparation.StateQueued, before)
	})

	t.Run("a skipped reverse step rejects the entire batch", func(t *testing.T) {
		env, first, second := batchWorld(t)
		advanceTo(t, env, first, preparation.StateReady)
		advanceTo(t, env, second, preparation.StateFulfilled)
		states := map[uuid.UUID]string{
			first:  preparation.StateReady,
			second: preparation.StateFulfilled,
		}
		before := transitionsBefore(t, env, states, preparation.StateInPreparation)
		requestID := uuid.New()
		cmd := correctCmd([]uuid.UUID{first, second}, preparation.StateInPreparation,
			preparation.ReasonStateRecordedInError, nil)
		cmd.RequestID = requestID
		_, status, err := env.CorrectState(t, cmd)
		require.ErrorIs(t, err, preparation.ErrInvalidTransition,
			"correcting FULFILLED to IN_PREPARATION would skip READY, so the batch is refused")
		require.Equal(t, http.StatusConflict, status)
		expectNoWrites(t, env, requestID, states, preparation.StateInPreparation, before)
	})

	t.Run("mixed owning sessions with one closed reject the entire batch", func(t *testing.T) {
		env := newPrepEnv(t)
		closed := env.SubmittedTakeawayUnits(t, 1)[0]
		open := env.SubmittedTakeawayUnits(t, 1)[0]
		_, _, err := env.BulkAdvance(t, preparation.BulkAdvanceCommand{
			RequestID:          uuid.New(),
			PreparationUnitIDs: []uuid.UUID{closed.ID, open.ID},
			TargetState:        preparation.StateInPreparation,
		})
		require.NoError(t, err)

		// A closed Session needs terminal work: waste the closed side's unit,
		// then settle and close its Session.
		_, _, err = env.Waste(t, closed.ID, preparation.ReasonQualityFailure, nil)
		require.NoError(t, err)
		env.SettleAndCloseSession(t, env.SessionIDForUnit(t, closed.ID))

		states := map[uuid.UUID]string{
			closed.ID: preparation.StateWasted,
			open.ID:   preparation.StateInPreparation,
		}
		before := transitionsBefore(t, env, states, preparation.StateQueued)
		requestID := uuid.New()
		cmd := correctCmd([]uuid.UUID{closed.ID, open.ID}, preparation.StateQueued,
			preparation.ReasonStateRecordedInError, nil)
		cmd.RequestID = requestID
		_, status, err := env.CorrectState(t, cmd)
		require.ErrorIs(t, err, preparation.ErrServiceSessionClosed,
			"one closed Session rejects the whole batch, before any unit is touched")
		require.Equal(t, http.StatusConflict, status)
		expectNoWrites(t, env, requestID, states, preparation.StateQueued, before)
	})

	t.Run("a closed session rejects the entire batch", func(t *testing.T) {
		env := newPrepEnv(t)
		unit := env.SubmittedTakeawayUnits(t, 1)[0]
		_, _, err := env.Advance(t, unit.ID, preparation.StateInPreparation)
		require.NoError(t, err)
		_, _, err = env.Waste(t, unit.ID, preparation.ReasonQualityFailure, nil)
		require.NoError(t, err)
		sessionID := env.SessionIDForUnit(t, unit.ID)
		env.SettleAndCloseSession(t, sessionID)

		states := map[uuid.UUID]string{unit.ID: preparation.StateWasted}
		before := transitionsBefore(t, env, states, preparation.StateQueued)
		requestID := uuid.New()
		cmd := correctCmd([]uuid.UUID{unit.ID}, preparation.StateQueued,
			preparation.ReasonStateRecordedInError, nil)
		cmd.RequestID = requestID
		_, status, err := env.CorrectState(t, cmd)
		require.ErrorIs(t, err, preparation.ErrServiceSessionClosed)
		require.Equal(t, http.StatusConflict, status)
		expectNoWrites(t, env, requestID, states, preparation.StateQueued, before)
	})
}

func TestCorrectStateDenials(t *testing.T) {
	type denial struct {
		name   string
		actor  func(e *prepEnv) preparation.Actor
		pin    string
		mutate func(t *testing.T, e *prepEnv)
		want   error
	}
	denials := []denial{
		{
			name:  "a barista fails the manager gate",
			actor: func(e *prepEnv) preparation.Actor { return e.BaristaActor() },
			want:  preparation.ErrForbidden,
		},
		{
			name:  "a cashier lacks the capability",
			actor: func(e *prepEnv) preparation.Actor { return e.CashierActor() },
			want:  preparation.ErrForbidden,
		},
		{
			name:  "a wrong own PIN is denied",
			actor: func(e *prepEnv) preparation.Actor { return e.ManagerActor() },
			pin:   "000000",
			want:  preparation.ErrInvalidManagerPIN,
		},
		{
			name:  "a disabled actor is denied",
			actor: func(e *prepEnv) preparation.Actor { return e.ManagerActor() },
			mutate: func(t *testing.T, e *prepEnv) {
				e.SetIdentityEnabled(t, e.ManagerActor(), false)
			},
			want: preparation.ErrForbidden,
		},
		{
			name:  "a removed manager role is denied",
			actor: func(e *prepEnv) preparation.Actor { return e.ManagerActor() },
			mutate: func(t *testing.T, e *prepEnv) {
				e.ReplaceRoles(t, e.ManagerActor(), []string{auth.RoleBarista})
			},
			want: preparation.ErrForbidden,
		},
		{
			name:  "a removed capability is denied",
			actor: func(e *prepEnv) preparation.Actor { return e.ManagerActor() },
			mutate: func(t *testing.T, e *prepEnv) {
				e.ReplaceRoles(t, e.ManagerActor(), []string{auth.RoleCashier})
			},
			want: preparation.ErrForbidden,
		},
	}

	for _, tt := range denials {
		t.Run(tt.name, func(t *testing.T) {
			env := newPrepEnv(t)
			units := env.SubmittedUnits(t, 2)
			ids := []uuid.UUID{units[0].ID, units[1].ID}
			_, _, err := env.BulkAdvance(t, preparation.BulkAdvanceCommand{
				RequestID: uuid.New(), PreparationUnitIDs: ids,
				TargetState: preparation.StateInPreparation,
			})
			require.NoError(t, err)
			if tt.mutate != nil {
				tt.mutate(t, env)
			}

			cmd := correctCmd(ids, preparation.StateQueued,
				preparation.ReasonStateRecordedInError, nil)
			cmd.RequestID = uuid.New()
			if tt.pin != "" {
				cmd.ManagerPIN = tt.pin
			}
			_, status, err := env.CorrectStateAs(t, tt.actor(env), cmd)
			require.ErrorIs(t, err, tt.want)
			require.Equal(t, http.StatusForbidden, status,
				"every manager/PIN denial collapses into the NOT_AUTHORIZED response")

			// No unit moved, no fact exists, and the denial left committed
			// evidence naming the operation — with no claim and no PIN.
			for _, id := range ids {
				require.Equal(t, preparation.StateInPreparation, env.UnitState(t, id))
				require.Equal(t, 0, env.CountCorrections(t, id))
				require.Equal(t, 0, env.CountTransitionsTo(t, id, preparation.StateQueued))
				require.Equal(t, 0, env.CountAuditEventsByTypeAndUnit(
					t, preparation.EventPreparationStateCorrected, id))
			}
			_, ok := env.IdempotencyClaim(t, tt.actor(env), cmd.RequestID)
			require.False(t, ok, "a denial never claims its request id")
			var evidence int
			require.NoError(t, env.DB.QueryRow(`
				SELECT count(*) FROM audit_events
				WHERE event_type = $1 AND details->>'operation' = $2`,
				preparation.EventAuthorizationDenied, preparation.OpCorrectState,
			).Scan(&evidence))
			require.Equal(t, 1, evidence,
				"exactly one committed denial audit row names the correction operation")
		})
	}
}

func TestCorrectStateReplay(t *testing.T) {
	t.Run("an exact replay requires the current authority and returns the stored response", func(t *testing.T) {
		env := newPrepEnv(t)
		units := env.SubmittedUnits(t, 2)
		ids := []uuid.UUID{units[0].ID, units[1].ID}
		_, _, err := env.BulkAdvance(t, preparation.BulkAdvanceCommand{
			RequestID: uuid.New(), PreparationUnitIDs: ids,
			TargetState: preparation.StateInPreparation,
		})
		require.NoError(t, err)

		requestID := uuid.New()
		first, status, err := env.CorrectStateWithRequestID(t, requestID,
			correctCmd(ids, preparation.StateQueued,
				preparation.ReasonStateRecordedInError, notePtr("  chốt nhầm  ")))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, status)

		facts := env.CountAllCorrections(t)
		transitions := env.CountTransitionsTo(t, units[0].ID, preparation.StateQueued)
		audits := env.CountAuditEvents(t, preparation.EventPreparationStateCorrected)

		// The replay normalizes to the same fingerprint even with different
		// padding, and the current authority and current PIN still gate it.
		second, status, err := env.CorrectStateWithRequestID(t, requestID,
			correctCmd(ids, preparation.StateQueued,
				preparation.ReasonStateRecordedInError, notePtr("chốt nhầm")))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, status, "the replay returns the stored 200")

		firstJSON, err := json.Marshal(first)
		require.NoError(t, err)
		secondJSON, err := json.Marshal(second)
		require.NoError(t, err)
		require.JSONEq(t, string(firstJSON), string(secondJSON),
			"the replay returns the exact stored response")

		require.Equal(t, facts, env.CountAllCorrections(t), "a replay records no duplicate fact")
		require.Equal(t, transitions, env.CountTransitionsTo(t, units[0].ID, preparation.StateQueued),
			"a replay records no duplicate transition")
		require.Equal(t, audits, env.CountAuditEvents(t, preparation.EventPreparationStateCorrected),
			"a replay records no duplicate audit")
	})

	t.Run("a replay is denied once the manager role is gone", func(t *testing.T) {
		env := newPrepEnv(t)
		units := env.SubmittedUnits(t, 1)
		_, _, err := env.Advance(t, units[0].ID, preparation.StateInPreparation)
		require.NoError(t, err)
		requestID := uuid.New()
		cmd := correctCmd([]uuid.UUID{units[0].ID}, preparation.StateQueued,
			preparation.ReasonStateRecordedInError, nil)
		_, status, err := env.CorrectStateWithRequestID(t, requestID, cmd)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, status)

		facts := env.CountAllCorrections(t)
		env.ReplaceRoles(t, env.ManagerActor(), []string{auth.RoleBarista})

		_, status, err = env.CorrectStateWithRequestID(t, requestID, cmd)
		require.ErrorIs(t, err, preparation.ErrForbidden,
			"the current authority is rechecked even on the replay path")
		require.Equal(t, http.StatusForbidden, status)
		require.Equal(t, facts, env.CountAllCorrections(t),
			"the denied replay writes nothing further")
	})

	t.Run("a rotated PIN permits the replay only with the new PIN", func(t *testing.T) {
		env := newPrepEnv(t)
		units := env.SubmittedUnits(t, 1)
		_, _, err := env.Advance(t, units[0].ID, preparation.StateInPreparation)
		require.NoError(t, err)
		requestID := uuid.New()
		cmd := correctCmd([]uuid.UUID{units[0].ID}, preparation.StateQueued,
			preparation.ReasonStateRecordedInError, nil)
		first, status, err := env.CorrectStateWithRequestID(t, requestID, cmd)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, status)

		facts := env.CountAllCorrections(t)
		audits := env.CountAuditEvents(t, preparation.EventPreparationStateCorrected)
		env.RotatePIN(t, env.ManagerActor(), "5678")

		// The old PIN is well-formed but no longer current: the replay is
		// denied before it reaches the stored result.
		oldCmd := correctCmd([]uuid.UUID{units[0].ID}, preparation.StateQueued,
			preparation.ReasonStateRecordedInError, nil)
		oldCmd.ManagerPIN = "1234"
		_, status, err = env.CorrectStateWithRequestID(t, requestID, oldCmd)
		require.ErrorIs(t, err, preparation.ErrInvalidManagerPIN)
		require.Equal(t, http.StatusForbidden, status)
		require.Equal(t, facts, env.CountAllCorrections(t))
		require.Equal(t, audits, env.CountAuditEvents(t, preparation.EventPreparationStateCorrected))

		// With the new PIN the exact replay returns the stored response. The
		// command carries no PIN, so the helper fills the actor's own
		// current one from the rotated registry.
		cmd.ManagerPIN = ""
		second, status, err := env.CorrectStateWithRequestID(t, requestID, cmd)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, status)
		firstJSON, err := json.Marshal(first)
		require.NoError(t, err)
		secondJSON, err := json.Marshal(second)
		require.NoError(t, err)
		require.JSONEq(t, string(firstJSON), string(secondJSON))
		require.Equal(t, facts, env.CountAllCorrections(t),
			"the replayed correction records no duplicate fact")
	})
}

func TestCorrectStateRollbacksAreAtomic(t *testing.T) {
	t.Run("a forced audit failure rolls back every unit, fact, transition, and claim", func(t *testing.T) {
		env := newPrepEnv(t)
		units := env.SubmittedUnits(t, 2)
		ids := []uuid.UUID{units[0].ID, units[1].ID}
		_, _, err := env.BulkAdvance(t, preparation.BulkAdvanceCommand{
			RequestID: uuid.New(), PreparationUnitIDs: ids,
			TargetState: preparation.StateInPreparation,
		})
		require.NoError(t, err)
		starts := map[uuid.UUID]*time.Time{}
		for _, id := range ids {
			starts[id] = env.UnitInPreparationAt(t, id)
		}

		// The per-unit audits are written inside the mutation through
		// writePreparationAudits; raising there forces the whole batch — and
		// with it the mutation — to abort.
		_, err = env.DB.Exec(`
			CREATE OR REPLACE FUNCTION fail_preparation_state_corrected_audit() RETURNS trigger AS $$
			BEGIN
				IF NEW.event_type = 'PREPARATION_STATE_CORRECTED' THEN
					RAISE EXCEPTION 'forced correction audit failure';
				END IF;
				RETURN NEW;
			END;
			$$ LANGUAGE plpgsql;
			CREATE TRIGGER fail_preparation_state_corrected_audit
			BEFORE INSERT ON audit_events
			FOR EACH ROW EXECUTE FUNCTION fail_preparation_state_corrected_audit();`)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = env.DB.Exec(`DROP TRIGGER IF EXISTS fail_preparation_state_corrected_audit ON audit_events`)
			_, _ = env.DB.Exec(`DROP FUNCTION IF EXISTS fail_preparation_state_corrected_audit()`)
		})

		requestID := uuid.New()
		cmd := correctCmd(ids, preparation.StateQueued,
			preparation.ReasonStateRecordedInError, nil)
		cmd.RequestID = requestID
		_, _, err = env.CorrectState(t, cmd)
		require.Error(t, err, "the forced audit failure must abort the whole command")

		for _, id := range ids {
			require.Equal(t, preparation.StateInPreparation, env.UnitState(t, id),
				"the failed transaction must leave every unit in preparation")
			require.True(t, starts[id].Equal(*env.UnitInPreparationAt(t, id)),
				"in_preparation_at must survive the aborted correction")
			require.Equal(t, 0, env.CountCorrections(t, id))
			require.Equal(t, 0, env.CountTransitionsTo(t, id, preparation.StateQueued))
			require.Equal(t, 0, env.CountAuditEventsByTypeAndUnit(
				t, preparation.EventPreparationStateCorrected, id))
		}
		require.Equal(t, 0, env.CountAllCorrections(t))
		_, ok := env.IdempotencyClaim(t, env.ManagerActor(), requestID)
		require.False(t, ok, "the failed transaction must not leave its claim behind")
	})

	t.Run("a forced result-storage failure rolls back every unit, fact, transition, and claim", func(t *testing.T) {
		env := newPrepEnv(t)
		units := env.SubmittedUnits(t, 2)
		ids := []uuid.UUID{units[0].ID, units[1].ID}
		_, _, err := env.BulkAdvance(t, preparation.BulkAdvanceCommand{
			RequestID: uuid.New(), PreparationUnitIDs: ids,
			TargetState: preparation.StateInPreparation,
		})
		require.NoError(t, err)

		// The idempotency claim is written with response_code 0 at INSERT
		// time; the result store is the UPDATE that sets response_code to
		// 200. Raising there forces the executor's last write to fail with
		// the whole correction already applied.
		_, err = env.DB.Exec(`
			CREATE OR REPLACE FUNCTION fail_preparation_correct_state_result_store() RETURNS trigger AS $$
			BEGIN
				IF NEW.response_code <> 0 AND NEW.action = 'preparation.correct_state' THEN
					RAISE EXCEPTION 'forced correction result store failure';
				END IF;
				RETURN NEW;
			END;
			$$ LANGUAGE plpgsql;
			CREATE TRIGGER fail_preparation_correct_state_result_store
			BEFORE UPDATE ON idempotency_keys
			FOR EACH ROW EXECUTE FUNCTION fail_preparation_correct_state_result_store();`)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = env.DB.Exec(`DROP TRIGGER IF EXISTS fail_preparation_correct_state_result_store ON idempotency_keys`)
			_, _ = env.DB.Exec(`DROP FUNCTION IF EXISTS fail_preparation_correct_state_result_store()`)
		})

		requestID := uuid.New()
		cmd := correctCmd(ids, preparation.StateQueued,
			preparation.ReasonStateRecordedInError, nil)
		cmd.RequestID = requestID
		_, _, err = env.CorrectState(t, cmd)
		require.Error(t, err, "the forced store failure must abort the whole command")

		for _, id := range ids {
			require.Equal(t, preparation.StateInPreparation, env.UnitState(t, id))
			require.Equal(t, 0, env.CountCorrections(t, id))
			require.Equal(t, 0, env.CountTransitionsTo(t, id, preparation.StateQueued))
			require.Equal(t, 0, env.CountAuditEventsByTypeAndUnit(
				t, preparation.EventPreparationStateCorrected, id))
		}
		require.Equal(t, 0, env.CountAllCorrections(t))
		_, ok := env.IdempotencyClaim(t, env.ManagerActor(), requestID)
		require.False(t, ok, "the failed transaction must not leave its claim behind")
	})
}

func TestOverlappingCorrectState(t *testing.T) {
	env := newPrepEnv(t)
	units := env.SubmittedUnits(t, 3)
	a, b, c := units[0].ID, units[1].ID, units[2].ID
	_, _, err := env.BulkAdvance(t, preparation.BulkAdvanceCommand{
		RequestID:          uuid.New(),
		PreparationUnitIDs: []uuid.UUID{a, b, c},
		TargetState:        preparation.StateInPreparation,
	})
	require.NoError(t, err)

	// Two overlapping batches share unit b. Both request ids differ, so the
	// executor serializes them on its own locks; whichever commits first
	// moves b, and the loser must re-read the committed state under the
	// deterministic session-then-unit lock order and lose as a whole.
	type call struct {
		cmd    preparation.CorrectStateCommand
		status int
		resp   preparation.CorrectStateResponse
		err    error
	}
	calls := []call{
		{cmd: correctCmd([]uuid.UUID{a, b}, preparation.StateQueued,
			preparation.ReasonStateRecordedInError, nil)},
		{cmd: correctCmd([]uuid.UUID{c, b}, preparation.StateQueued,
			preparation.ReasonStateRecordedInError, nil)},
	}
	for i := range calls {
		calls[i].cmd.RequestID = uuid.New()
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		var wg sync.WaitGroup
		for i := range calls {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				calls[i].resp, calls[i].status, calls[i].err = env.CorrectState(t, calls[i].cmd)
			}(i)
		}
		wg.Wait()
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("overlapping corrections deadlocked")
	}

	winner, loser := -1, -1
	for i := range calls {
		if calls[i].err == nil {
			require.Equal(t, http.StatusOK, calls[i].status)
			winner = i
		} else {
			require.ErrorIs(t, calls[i].err, preparation.ErrInvalidTransition,
				"the loser re-reads the winner's committed state and loses as a whole")
			require.Equal(t, http.StatusConflict, calls[i].status,
				"the loser maps to a typed conflict, never a 500")
			loser = i
		}
	}
	require.NotEqual(t, -1, winner, "exactly one batch wins")
	require.NotEqual(t, -1, loser, "the shared unit cannot serve two corrections")

	// The winner is complete: both its units corrected, in request order.
	require.Len(t, calls[winner].resp.Outcomes, 2)
	for i, outcome := range calls[winner].resp.Outcomes {
		require.Equal(t, calls[winner].cmd.PreparationUnitIDs[i], outcome.PreparationUnitID)
		require.Equal(t, preparation.StateQueued, outcome.ResultingState)
	}

	// The shared unit moved exactly once; the loser's other unit is untouched.
	require.Equal(t, 1, env.CountCorrections(t, b))
	require.Equal(t, 1, env.CountTransitionsTo(t, b, preparation.StateQueued))
	require.Equal(t, 1, env.CountAuditEventsByTypeAndUnit(
		t, preparation.EventPreparationStateCorrected, b))
	for _, unit := range []struct {
		id  uuid.UUID
		won bool
	}{{a, winner == 0}, {c, winner == 1}} {
		if unit.won {
			require.Equal(t, 1, env.CountCorrections(t, unit.id))
		} else {
			require.Equal(t, preparation.StateInPreparation, env.UnitState(t, unit.id),
				"the losing batch's other unit is untouched")
			require.Equal(t, 0, env.CountCorrections(t, unit.id))
			require.Equal(t, 0, env.CountTransitionsTo(t, unit.id, preparation.StateQueued))
			require.Equal(t, 0, env.CountAuditEventsByTypeAndUnit(
				t, preparation.EventPreparationStateCorrected, unit.id))
		}
	}
	require.Equal(t, 2, env.CountAllCorrections(t), "exactly two facts exist, one per corrected unit")
}

// TestAdvanceAgainstCorrection pins the advance-versus-correction
// serialization: both commands revalidate under the unit row lock, so
// exactly one wins and the loser receives a lifecycle conflict without ever
// moving the unit from a state it did not re-read.
func TestAdvanceAgainstCorrection(t *testing.T) {
	t.Run("a correction that commits first makes the advance refuse from the corrected state", func(t *testing.T) {
		env := newPrepEnv(t)
		unit := env.SubmittedUnits(t, 1)[0]
		_, _, err := env.Advance(t, unit.ID, preparation.StateInPreparation)
		require.NoError(t, err)

		// Lock barrier on the unit row: the correction takes its Session
		// lock and then parks on the unit, having validated nothing yet.
		barrier, err := env.DB.BeginTx(context.Background(), nil)
		require.NoError(t, err)
		_, err = barrier.ExecContext(context.Background(),
			`SELECT id FROM preparation_units WHERE id = $1 FOR UPDATE`, unit.ID)
		require.NoError(t, err)

		correctionErrs := make(chan error, 1)
		go func() {
			_, _, err := env.CorrectState(t,
				correctCmd([]uuid.UUID{unit.ID}, preparation.StateQueued,
					preparation.ReasonStateRecordedInError, nil))
			correctionErrs <- err
		}()

		require.Eventually(t, func() bool {
			return lockWaiters(t, env) > 0
		}, 10*time.Second, 10*time.Millisecond,
			"the correction must park on the barrier's unit lock before it is released")

		require.NoError(t, barrier.Rollback(), "releasing the barrier must succeed")
		require.NoError(t, <-correctionErrs, "the correction parked behind the barrier must commit")

		// The advance starts only once the correction has committed, so it
		// reads the QUEUED row: QUEUED to READY skips a step, and the loser
		// must refuse rather than move the unit on.
		_, _, err = env.Advance(t, unit.ID, preparation.StateReady)
		require.ErrorIs(t, err, preparation.ErrInvalidTransition)
		status, _ := preparation.ErrorResponse(err)
		require.Equal(t, http.StatusConflict, status)

		require.Equal(t, preparation.StateQueued, env.UnitState(t, unit.ID))
		require.Equal(t, 1, env.CountCorrections(t, unit.ID))
		require.Equal(t, 0, env.CountTransitionsTo(t, unit.ID, preparation.StateReady),
			"the losing advance wrote no transition")
		require.Equal(t, 1, env.UnitAuditCount(t, unit.ID),
			"only the setup advance's audit exists; the losing advance wrote none")
	})

	t.Run("an advance that commits first makes the correction refuse from the advanced state", func(t *testing.T) {
		env := newPrepEnv(t)
		unit := env.SubmittedUnits(t, 1)[0]
		_, _, err := env.Advance(t, unit.ID, preparation.StateInPreparation)
		require.NoError(t, err)

		// Lock barrier on the unit row: the advance parks on its lock while
		// the correction has not started validating anything.
		barrier, err := env.DB.BeginTx(context.Background(), nil)
		require.NoError(t, err)
		_, err = barrier.ExecContext(context.Background(),
			`SELECT id FROM preparation_units WHERE id = $1 FOR UPDATE`, unit.ID)
		require.NoError(t, err)

		advanceErrs := make(chan error, 1)
		go func() {
			_, _, err := env.Advance(t, unit.ID, preparation.StateReady)
			advanceErrs <- err
		}()

		require.Eventually(t, func() bool {
			return lockWaiters(t, env) > 0
		}, 10*time.Second, 10*time.Millisecond,
			"the advance must park on the barrier's unit lock before it is released")

		require.NoError(t, barrier.Rollback(), "releasing the barrier must succeed")
		require.NoError(t, <-advanceErrs, "the advance parked behind the barrier must commit")

		// The correction re-reads READY under its lock: a correction to
		// QUEUED requires IN_PREPARATION, so the whole batch is refused.
		_, _, err = env.CorrectState(t,
			correctCmd([]uuid.UUID{unit.ID}, preparation.StateQueued,
				preparation.ReasonStateRecordedInError, nil))
		require.ErrorIs(t, err, preparation.ErrInvalidTransition)
		status, _ := preparation.ErrorResponse(err)
		require.Equal(t, http.StatusConflict, status)

		require.Equal(t, preparation.StateReady, env.UnitState(t, unit.ID))
		require.Equal(t, 0, env.CountCorrections(t, unit.ID),
			"the losing correction wrote no fact")
		require.Equal(t, 0, env.CountTransitionsTo(t, unit.ID, preparation.StateQueued),
			"the losing correction wrote no transition")
		require.Equal(t, 0, env.CountAuditEvents(t, preparation.EventPreparationStateCorrected),
			"the losing correction wrote no audit")
	})
}

// TestCorrectionAgainstClosure pins the correction-versus-closure
// serialization: both commands lock the Service Session first, so exactly one
// of two outcomes is possible — a closed-before-correction rejection, or a
// correction whose revived READY unit makes closure refuse.
func TestCorrectionAgainstClosure(t *testing.T) {
	t.Run("a correction that commits first makes closure refuse its revived unit", func(t *testing.T) {
		env := newPrepEnv(t)
		unit := env.SubmittedTakeawayUnits(t, 1)[0]
		advanceTo(t, env, unit.ID, preparation.StateFulfilled)
		sessionID := env.SessionIDForUnit(t, unit.ID)
		require.True(t, env.SessionFinancials(t, sessionID).ClosureEligible,
			"the fulfilled unit leaves the session otherwise closure-ready")

		// Lock barrier on the Service Session row: the correction parks
		// there — its first lock — and closure queues behind it.
		barrier, err := env.DB.BeginTx(context.Background(), nil)
		require.NoError(t, err)
		_, err = barrier.ExecContext(context.Background(),
			`SELECT id FROM service_sessions WHERE id = $1 FOR UPDATE`, sessionID)
		require.NoError(t, err)

		correctionErrs := make(chan error, 1)
		go func() {
			_, _, err := env.CorrectState(t,
				correctCmd([]uuid.UUID{unit.ID}, preparation.StateReady,
					preparation.ReasonStateRecordedInError, nil))
			correctionErrs <- err
		}()

		require.Eventually(t, func() bool {
			return lockWaiters(t, env) > 0
		}, 10*time.Second, 10*time.Millisecond,
			"the correction must park on the barrier's session lock before closure runs")

		// Closure starts only once the correction holds the Session lock, so
		// it must park there until the correction's transaction ends.
		closureErrs := make(chan error, 1)
		go func() {
			_, _, err := sales.NewCloseServiceSessionHandler(env.SalesRunner).Handle(
				context.Background(), env.salesActor(env.ManagerActor()),
				sales.CloseServiceSessionCommand{
					RequestID:        uuid.New(),
					ServiceSessionID: sessionID,
				})
			closureErrs <- err
		}()

		require.NoError(t, barrier.Rollback(), "releasing the barrier must succeed")

		require.NoError(t, <-correctionErrs, "the correction parked behind the barrier must commit")
		require.ErrorIs(t, <-closureErrs, sales.ErrUnfulfilledPreparationForClosure,
			"closure must refuse the corrected unit's nonterminal READY state")

		// The winner-only outcome: the unit sits at READY and the session is
		// still open.
		require.Equal(t, preparation.StateReady, env.UnitState(t, unit.ID))
		require.Equal(t, 1, env.CountCorrections(t, unit.ID))
		require.Equal(t, sales.StateActive, env.SessionFinancials(t, sessionID).State)
	})

	t.Run("a closure that commits first makes the correction reject the closed session", func(t *testing.T) {
		env := newPrepEnv(t)
		unit := env.SubmittedTakeawayUnits(t, 1)[0]
		advanceTo(t, env, unit.ID, preparation.StateFulfilled)
		sessionID := env.SessionIDForUnit(t, unit.ID)

		// A second seeded Manager drives the correction. The gate locks its
		// actor's own identity row FOR UPDATE and holds it while parked, so
		// the closure below must claim its mutations against a different
		// identity or its claims would block on the parked gate forever.
		corrector := seedActor(t, env.Queries, []string{auth.RoleManager})

		// Lock barrier on the correction's own executor advisory lock (the
		// same actor^request-id key the executor takes): the correction
		// parks before its idempotency claim and takes no business lock, so
		// closure can run to completion.
		requestID := uuid.New()
		key := preparation.IDToLockKey(corrector.StaffID) ^
			preparation.IDToLockKey(requestID)
		barrier, err := env.DB.BeginTx(context.Background(), nil)
		require.NoError(t, err)
		_, err = barrier.ExecContext(context.Background(),
			`SELECT pg_advisory_xact_lock($1)`, key)
		require.NoError(t, err)

		correctionErrs := make(chan error, 1)
		go func() {
			_, _, err := env.CorrectStateWithRequestIDAs(t, requestID, corrector,
				correctCmd([]uuid.UUID{unit.ID}, preparation.StateReady,
					preparation.ReasonStateRecordedInError, nil))
			correctionErrs <- err
		}()

		require.Eventually(t, func() bool {
			return lockWaiters(t, env) > 0
		}, 10*time.Second, 10*time.Millisecond,
			"the correction must park on its advisory lock before closure runs")

		// Closure completes while the correction is parked; the correction's
		// Session lock has not been taken, so nothing blocks it.
		env.SettleAndCloseSession(t, sessionID)
		require.Equal(t, sales.StateClosed, env.SessionFinancials(t, sessionID).State)

		require.NoError(t, barrier.Rollback(), "releasing the barrier must succeed")

		correctionErr := <-correctionErrs
		require.ErrorIs(t, correctionErr, preparation.ErrServiceSessionClosed,
			"the released correction must re-read the session state under the lock and refuse")
		status, _ := preparation.ErrorResponse(correctionErr)
		require.Equal(t, http.StatusConflict, status)

		// The winner-only outcome: no fact, the unit stays fulfilled, and no
		// claim survives the rollback.
		require.Equal(t, preparation.StateFulfilled, env.UnitState(t, unit.ID))
		require.Equal(t, 0, env.CountCorrections(t, unit.ID))
		_, ok := env.IdempotencyClaim(t, corrector, requestID)
		require.False(t, ok, "the rejected correction leaves no claim behind")
	})
}
