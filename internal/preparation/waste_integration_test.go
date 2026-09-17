//go:build integration

package preparation_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/preparation"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// notePtr returns a pointer to s; a helper for the command's optional note.
func notePtr(s string) *string { return &s }

func TestWasteUnit(t *testing.T) {
	t.Run("wastes from IN_PREPARATION with a trimmed note", func(t *testing.T) {
		env := newPrepEnv(t)
		unit := env.SubmittedUnits(t, 1)[0]
		_, _, err := env.Advance(t, unit.ID, preparation.StateInPreparation)
		require.NoError(t, err)
		startedAt := env.UnitInPreparationAt(t, unit.ID)
		require.NotNil(t, startedAt)

		requestID := uuid.New()
		resp, status, err := env.WasteWithRequestID(t, requestID, unit.ID,
			preparation.ReasonQualityFailure, notePtr("  Đá bịt ống pha  "))
		require.NoError(t, err)
		require.Equal(t, http.StatusCreated, status, "a first waste answers 201")

		// Exact response fields: the Waste fact's identity and meaning, the
		// recorded state change, and the fresh unacknowledged WASTE alert.
		require.NotEqual(t, uuid.Nil, resp.ID)
		require.Equal(t, unit.ID, resp.PreparationUnitID)
		require.Equal(t, preparation.StateInPreparation, resp.PriorState)
		require.Equal(t, preparation.StateWasted, resp.ResultingState)
		require.Equal(t, preparation.ReasonQualityFailure, resp.Reason)
		require.NotNil(t, resp.Note)
		require.Equal(t, "Đá bịt ống pha", *resp.Note, "the note is stored trimmed")
		require.NotZero(t, resp.OccurredAt)
		require.NotEqual(t, uuid.Nil, resp.Alert.ID)
		require.Equal(t, unit.ID, resp.Alert.PreparationUnitID)
		require.Equal(t, preparation.AlertKindWaste, resp.Alert.Kind)
		require.Equal(t, preparation.ReasonQualityFailure, resp.Alert.Reason)
		require.NotNil(t, resp.Alert.Note)
		require.Equal(t, "Đá bịt ống pha", *resp.Alert.Note)
		require.Nil(t, resp.Alert.AcknowledgedByStaffIdentityID)
		require.Nil(t, resp.Alert.AcknowledgedAt)

		// One Waste fact with the actor's identity on it.
		require.Equal(t, preparation.StateWasted, env.UnitState(t, unit.ID))
		require.Equal(t, 1, env.CountWastes(t, unit.ID))
		waste := env.UnitWaste(t, unit.ID)
		require.Equal(t, resp.ID, waste.ID)
		require.Equal(t, preparation.StateInPreparation, waste.PriorState)
		require.Equal(t, env.BaristaActor().StaffID, waste.ActorID)
		require.Equal(t, env.BaristaActor().SessionID, waste.SessionID)
		require.NotNil(t, waste.Note)
		require.Equal(t, "Đá bịt ống pha", *waste.Note)

		// Wasting a unit never rewrites when it entered preparation.
		require.Equal(t, startedAt, env.UnitInPreparationAt(t, unit.ID))

		// One typed transition into WASTED from the unit's prior state.
		require.Equal(t, 1, env.CountTransitionsTo(t, unit.ID, preparation.StateWasted))
		transition := env.UnitTransitionTo(t, unit.ID, preparation.StateWasted)
		require.Equal(t, preparation.StateInPreparation, transition.PriorState)

		// One unacknowledged WASTE alert.
		require.Equal(t, 1, env.CountAlerts(t, unit.ID))
		alert := env.UnitAlert(t, unit.ID)
		require.Equal(t, resp.Alert.ID, alert.ID)
		require.Equal(t, preparation.AlertKindWaste, alert.Kind)
		require.Nil(t, alert.AcknowledgedAt)

		// Two business audits, one per event type.
		require.Equal(t, 1, env.CountAuditEventsByTypeAndUnit(
			t, preparation.EventPreparationUnitWasted, unit.ID))
		require.Equal(t, 1, env.CountAuditEventsByTypeAndUnit(
			t, preparation.EventPreparationAlertCreated, unit.ID))

		// One stored result: the exact response body, replayable.
		claim, ok := env.IdempotencyClaim(t, env.BaristaActor(), requestID)
		require.True(t, ok, "a success stores its replayable result")
		require.Equal(t, preparation.OpWasteUnit, claim.Action)
		require.Equal(t, int32(http.StatusCreated), claim.ResponseCode)
		respJSON, err := json.Marshal(resp)
		require.NoError(t, err)
		require.JSONEq(t, string(respJSON), string(claim.ResponseBody))
	})

	t.Run("wastes from READY without a note", func(t *testing.T) {
		env := newPrepEnv(t)
		unit := env.SubmittedUnits(t, 1)[0]
		_, _, err := env.Advance(t, unit.ID, preparation.StateInPreparation)
		require.NoError(t, err)
		_, _, err = env.Advance(t, unit.ID, preparation.StateReady)
		require.NoError(t, err)

		resp, status, err := env.Waste(t, unit.ID, preparation.ReasonCustomerRequest, nil)
		require.NoError(t, err)
		require.Equal(t, http.StatusCreated, status)

		require.Equal(t, preparation.StateReady, resp.PriorState)
		require.Equal(t, preparation.StateWasted, resp.ResultingState)
		require.Nil(t, resp.Note)
		require.Equal(t, preparation.StateWasted, env.UnitState(t, unit.ID))
		require.Equal(t, 1, env.CountWastes(t, unit.ID))
		require.Equal(t, 1, env.CountAlerts(t, unit.ID))
		require.Equal(t, 1, env.CountTransitionsTo(t, unit.ID, preparation.StateWasted))
		transition := env.UnitTransitionTo(t, unit.ID, preparation.StateWasted)
		require.Equal(t, preparation.StateReady, transition.PriorState)
	})

	t.Run("one occurrence time is shared by the fact, transition, alert, and both audits", func(t *testing.T) {
		env := newPrepEnv(t)
		unit := env.SubmittedUnits(t, 1)[0]
		_, _, err := env.Advance(t, unit.ID, preparation.StateInPreparation)
		require.NoError(t, err)

		resp, _, err := env.Waste(t, unit.ID, preparation.ReasonPreparationError, nil)
		require.NoError(t, err)

		waste := env.UnitWaste(t, unit.ID)
		alert := env.UnitAlert(t, unit.ID)
		transition := env.UnitTransitionTo(t, unit.ID, preparation.StateWasted)
		audits := env.UnitAuditEvents(t, unit.ID,
			preparation.EventPreparationUnitWasted, preparation.EventPreparationAlertCreated)
		require.Len(t, audits, 2)
		require.ElementsMatch(t, []string{
			preparation.EventPreparationUnitWasted,
			preparation.EventPreparationAlertCreated,
		}, []string{audits[0].EventType, audits[1].EventType})

		require.True(t, resp.OccurredAt.Equal(waste.OccurredAt))
		require.True(t, resp.OccurredAt.Equal(alert.CreatedAt))
		require.True(t, resp.OccurredAt.Equal(transition.OccurredAt))
		for _, event := range audits {
			require.True(t, resp.OccurredAt.Equal(event.OccurredAt),
				"both audits share the fact's occurrence time")
		}
	})

	t.Run("rejection from QUEUED, FULFILLED, CANCELLED, and WASTED writes nothing", func(t *testing.T) {
		env := newPrepEnv(t)

		// Each arrangement returns the unit id and the state it was left in
		// before the rejected waste.
		arrangements := []struct {
			priorState string
			arrange    func(t *testing.T) uuid.UUID
		}{
			{preparation.StateQueued, func(t *testing.T) uuid.UUID {
				return env.SubmittedUnits(t, 1)[0].ID
			}},
			{preparation.StateFulfilled, func(t *testing.T) uuid.UUID {
				unit := env.SubmittedUnits(t, 1)[0].ID
				for _, target := range []string{
					preparation.StateInPreparation,
					preparation.StateReady,
					preparation.StateFulfilled,
				} {
					_, _, err := env.Advance(t, unit, target)
					require.NoError(t, err)
				}
				return unit
			}},
			{preparation.StateCancelled, func(t *testing.T) uuid.UUID {
				// Phase 6C ships no cancellation command, so the exceptional
				// state is arranged directly; the state CHECK admits it.
				unit := env.SubmittedUnits(t, 1)[0].ID
				_, err := env.DB.Exec(
					`UPDATE preparation_units SET state = 'CANCELLED' WHERE id = $1`, unit)
				require.NoError(t, err)
				return unit
			}},
			{preparation.StateWasted, func(t *testing.T) uuid.UUID {
				unit := env.SubmittedUnits(t, 1)[0].ID
				_, _, err := env.Advance(t, unit, preparation.StateInPreparation)
				require.NoError(t, err)
				_, _, err = env.Waste(t, unit, preparation.ReasonQualityFailure, nil)
				require.NoError(t, err)
				return unit
			}},
		}

		for _, arrangement := range arrangements {
			t.Run("from "+arrangement.priorState, func(t *testing.T) {
				unit := arrangement.arrange(t)
				wastesBefore := env.CountWastes(t, unit)
				alertsBefore := env.CountAlerts(t, unit)
				transitionsBefore := env.CountTransitionsTo(t, unit, preparation.StateWasted)

				_, status, err := env.Waste(t, unit, preparation.ReasonQualityFailure, nil)
				require.ErrorIs(t, err, preparation.ErrInvalidTransition)
				require.Equal(t, http.StatusConflict, status)

				require.Equal(t, arrangement.priorState, env.UnitState(t, unit),
					"the rejected waste leaves the unit's state untouched")
				require.Equal(t, wastesBefore, env.CountWastes(t, unit),
					"the rejected waste records no fact")
				require.Equal(t, alertsBefore, env.CountAlerts(t, unit),
					"the rejected waste records no alert")
				require.Equal(t, transitionsBefore, env.CountTransitionsTo(t, unit, preparation.StateWasted),
					"the rejected waste records no transition")
			})
		}
	})

	t.Run("an unknown unit is not found", func(t *testing.T) {
		env := newPrepEnv(t)
		_, status, err := env.Waste(t, uuid.New(), preparation.ReasonQualityFailure, nil)
		require.ErrorIs(t, err, preparation.ErrUnitNotFound)
		require.Equal(t, http.StatusNotFound, status)
	})

	t.Run("a reason outside the Waste catalog is refused before any claim", func(t *testing.T) {
		env := newPrepEnv(t)
		unit := env.SubmittedUnits(t, 1)[0]

		requestID := uuid.New()
		_, status, err := env.WasteWithRequestID(t, requestID, unit.ID, "TOO_SLOW", nil)
		require.ErrorIs(t, err, preparation.ErrInvalidReason)
		require.Equal(t, http.StatusBadRequest, status)
		require.Equal(t, preparation.StateQueued, env.UnitState(t, unit.ID))

		_, ok := env.IdempotencyClaim(t, env.BaristaActor(), requestID)
		require.False(t, ok, "a boundary validation failure never claims its request id")
	})

	t.Run("OTHER without a note is refused and a blank note cannot satisfy it", func(t *testing.T) {
		env := newPrepEnv(t)
		unit := env.SubmittedUnits(t, 1)[0]

		_, _, err := env.Waste(t, unit.ID, preparation.ReasonOther, nil)
		require.ErrorIs(t, err, preparation.ErrInvalidNote)

		// The blank note normalizes to nil before validation, so it cannot
		// satisfy the OTHER reason's note rule.
		_, _, err = env.Waste(t, unit.ID, preparation.ReasonOther, notePtr("   "))
		require.ErrorIs(t, err, preparation.ErrInvalidNote)

		_, _, err = env.Waste(t, unit.ID, preparation.ReasonQualityFailure,
			notePtr(strings.Repeat("ể", preparation.MaxNoteRunes+1)))
		require.ErrorIs(t, err, preparation.ErrInvalidNote)

		require.Equal(t, preparation.StateQueued, env.UnitState(t, unit.ID))
		require.Equal(t, 0, env.CountWastes(t, unit.ID))
	})

	t.Run("a cashier cannot waste", func(t *testing.T) {
		env := newPrepEnv(t)
		unit := env.SubmittedUnits(t, 1)[0]

		_, status, err := env.WasteAs(t, env.CashierActor(), unit.ID,
			preparation.ReasonQualityFailure, nil)
		require.ErrorIs(t, err, preparation.ErrForbidden)
		require.Equal(t, http.StatusForbidden, status)
		require.Equal(t, preparation.StateQueued, env.UnitState(t, unit.ID))
		require.Equal(t, 0, env.CountWastes(t, unit.ID))
	})

	t.Run("a replayed request id returns the stored result without new writes", func(t *testing.T) {
		env := newPrepEnv(t)
		unit := env.SubmittedUnits(t, 1)[0]
		_, _, err := env.Advance(t, unit.ID, preparation.StateInPreparation)
		require.NoError(t, err)

		requestID := uuid.New()
		first, status, err := env.WasteWithRequestID(t, requestID, unit.ID,
			preparation.ReasonQualityFailure, notePtr("  ít đá  "))
		require.NoError(t, err)
		require.Equal(t, http.StatusCreated, status)

		wastes := env.CountWastes(t, unit.ID)
		alerts := env.CountAlerts(t, unit.ID)
		transitions := env.CountTransitionsTo(t, unit.ID, preparation.StateWasted)
		wastedAudits := env.CountAuditEvents(t, preparation.EventPreparationUnitWasted)
		alertAudits := env.CountAuditEvents(t, preparation.EventPreparationAlertCreated)

		second, status, err := env.WasteWithRequestID(t, requestID, unit.ID,
			preparation.ReasonQualityFailure, notePtr("ít đá"))
		require.NoError(t, err)
		require.Equal(t, http.StatusCreated, status, "the replay returns the stored 201")

		firstJSON, err := json.Marshal(first)
		require.NoError(t, err)
		secondJSON, err := json.Marshal(second)
		require.NoError(t, err)
		require.JSONEq(t, string(firstJSON), string(secondJSON),
			"the replay returns the exact stored response")

		require.Equal(t, wastes, env.CountWastes(t, unit.ID), "a replay records no duplicate fact")
		require.Equal(t, alerts, env.CountAlerts(t, unit.ID), "a replay records no duplicate alert")
		require.Equal(t, transitions, env.CountTransitionsTo(t, unit.ID, preparation.StateWasted),
			"a replay records no duplicate transition")
		require.Equal(t, wastedAudits, env.CountAuditEvents(t, preparation.EventPreparationUnitWasted),
			"a replay records no duplicate audit")
		require.Equal(t, alertAudits, env.CountAuditEvents(t, preparation.EventPreparationAlertCreated),
			"a replay records no duplicate audit")
	})

	t.Run("a conflicting request-id reuse is refused", func(t *testing.T) {
		env := newPrepEnv(t)
		unit := env.SubmittedUnits(t, 1)[0]
		_, _, err := env.Advance(t, unit.ID, preparation.StateInPreparation)
		require.NoError(t, err)

		requestID := uuid.New()
		_, _, err = env.WasteWithRequestID(t, requestID, unit.ID,
			preparation.ReasonQualityFailure, nil)
		require.NoError(t, err)

		_, status, err := env.WasteWithRequestID(t, requestID, unit.ID,
			preparation.ReasonCustomerRequest, nil)
		require.ErrorIs(t, err, preparation.ErrRequestConflict)
		require.Equal(t, http.StatusConflict, status)

		require.Equal(t, 1, env.CountWastes(t, unit.ID))
		require.Equal(t, 1, env.CountAlerts(t, unit.ID))
	})

	t.Run("current capability is checked before replay", func(t *testing.T) {
		env := newPrepEnv(t)
		unit := env.SubmittedUnits(t, 1)[0]
		_, _, err := env.Advance(t, unit.ID, preparation.StateInPreparation)
		require.NoError(t, err)

		requestID := uuid.New()
		_, _, err = env.WasteWithRequestID(t, requestID, unit.ID,
			preparation.ReasonQualityFailure, nil)
		require.NoError(t, err)
		wastes := env.CountWastes(t, unit.ID)

		env.ReplaceRoles(t, env.BaristaActor(), nil)
		t.Cleanup(func() {
			env.ReplaceRoles(t, env.BaristaActor(), []string{auth.RoleBarista})
		})

		_, _, err = env.WasteWithRequestID(t, requestID, unit.ID,
			preparation.ReasonQualityFailure, nil)
		require.ErrorIs(t, err, preparation.ErrForbidden,
			"the capability is rechecked even on the replay path")

		require.Equal(t, wastes, env.CountWastes(t, unit.ID))
		_, ok := env.IdempotencyClaim(t, env.BaristaActor(), requestID)
		require.True(t, ok, "the denial leaves the stored result untouched")
	})
}

func TestWasteRollbacksAreAtomic(t *testing.T) {
	t.Run("forced second-audit failure rolls back everything", func(t *testing.T) {
		env := newPrepEnv(t)
		unit := env.SubmittedUnits(t, 1)[0]
		_, _, err := env.Advance(t, unit.ID, preparation.StateInPreparation)
		require.NoError(t, err)
		startedAt := env.UnitInPreparationAt(t, unit.ID)
		require.NotNil(t, startedAt)

		// The Waste batch writes PREPARATION_UNIT_WASTED first and
		// PREPARATION_ALERT_CREATED second; failing the second row forces
		// the whole batch — and with it the mutation — to abort.
		_, err = env.DB.Exec(`
			CREATE FUNCTION fail_preparation_alert_created_audit() RETURNS trigger AS $$
			BEGIN
				IF NEW.event_type = 'PREPARATION_ALERT_CREATED' THEN
					RAISE EXCEPTION 'forced second preparation audit failure';
				END IF;
				RETURN NEW;
			END;
			$$ LANGUAGE plpgsql;
			CREATE TRIGGER fail_preparation_alert_created_audit
			BEFORE INSERT ON audit_events
			FOR EACH ROW EXECUTE FUNCTION fail_preparation_alert_created_audit();`)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = env.DB.Exec(`DROP TRIGGER IF EXISTS fail_preparation_alert_created_audit ON audit_events`)
			_, _ = env.DB.Exec(`DROP FUNCTION IF EXISTS fail_preparation_alert_created_audit()`)
		})

		requestID := uuid.New()
		_, _, err = env.WasteWithRequestID(t, requestID, unit.ID,
			preparation.ReasonQualityFailure, nil)
		require.Error(t, err, "the forced audit failure must abort the whole command")

		require.Equal(t, preparation.StateInPreparation, env.UnitState(t, unit.ID),
			"the failed transaction must leave the unit in preparation")
		require.Equal(t, startedAt, env.UnitInPreparationAt(t, unit.ID))
		require.Equal(t, 0, env.CountWastes(t, unit.ID))
		require.Equal(t, 0, env.CountAlerts(t, unit.ID))
		require.Equal(t, 0, env.CountTransitionsTo(t, unit.ID, preparation.StateWasted))
		require.Equal(t, 0, env.CountAuditEvents(t, preparation.EventPreparationUnitWasted))
		require.Equal(t, 0, env.CountAuditEvents(t, preparation.EventPreparationAlertCreated))

		_, ok := env.IdempotencyClaim(t, env.BaristaActor(), requestID)
		require.False(t, ok, "the failed transaction must not leave its claim behind")
	})

	t.Run("forced result-storage failure rolls back everything", func(t *testing.T) {
		env := newPrepEnv(t)
		unit := env.SubmittedUnits(t, 1)[0]
		_, _, err := env.Advance(t, unit.ID, preparation.StateInPreparation)
		require.NoError(t, err)

		// The idempotency claim is written with response_code 0 at INSERT
		// time; the result store is the UPDATE that sets response_code to
		// 201. Raising there forces the executor's last write to fail with
		// the waste already applied.
		_, err = env.DB.Exec(`
			CREATE OR REPLACE FUNCTION fail_preparation_waste_result_store() RETURNS trigger AS $$
			BEGIN
				IF NEW.response_code <> 0 AND NEW.action = 'preparation.waste_unit' THEN
					RAISE EXCEPTION 'forced waste result store failure';
				END IF;
				RETURN NEW;
			END;
			$$ LANGUAGE plpgsql;
			CREATE TRIGGER fail_preparation_waste_result_store
			BEFORE UPDATE ON idempotency_keys
			FOR EACH ROW EXECUTE FUNCTION fail_preparation_waste_result_store();`)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = env.DB.Exec(`DROP TRIGGER IF EXISTS fail_preparation_waste_result_store ON idempotency_keys`)
			_, _ = env.DB.Exec(`DROP FUNCTION IF EXISTS fail_preparation_waste_result_store()`)
		})

		requestID := uuid.New()
		_, _, err = env.WasteWithRequestID(t, requestID, unit.ID,
			preparation.ReasonQualityFailure, nil)
		require.Error(t, err, "the forced store failure must abort the whole command")

		require.Equal(t, preparation.StateInPreparation, env.UnitState(t, unit.ID),
			"the failed transaction must leave the unit in preparation")
		require.Equal(t, 0, env.CountWastes(t, unit.ID))
		require.Equal(t, 0, env.CountAlerts(t, unit.ID))
		require.Equal(t, 0, env.CountTransitionsTo(t, unit.ID, preparation.StateWasted))
		require.Equal(t, 0, env.CountAuditEvents(t, preparation.EventPreparationUnitWasted))
		require.Equal(t, 0, env.CountAuditEvents(t, preparation.EventPreparationAlertCreated))

		var claims int
		require.NoError(t, env.DB.QueryRow(
			`SELECT count(*) FROM idempotency_keys WHERE actor_id = $1 AND key = $2`,
			env.BaristaActor().StaffID, requestID,
		).Scan(&claims))
		require.Zero(t, claims, "the failed transaction must not leave its claim behind")
	})
}

func TestConcurrentWaste(t *testing.T) {
	env := newPrepEnv(t)
	unit := env.SubmittedUnits(t, 1)[0]
	_, _, err := env.Advance(t, unit.ID, preparation.StateInPreparation)
	require.NoError(t, err)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	statuses := make([]int, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, statuses[i], errs[i] = env.Waste(t, unit.ID, preparation.ReasonQualityFailure, nil)
		}(i)
	}
	wg.Wait()

	succeeded := 0
	for i := range errs {
		if errs[i] == nil {
			succeeded++
		} else {
			require.ErrorIs(t, errs[i], preparation.ErrInvalidTransition,
				"the loser re-reads the wasted state under the lock")
			require.Equal(t, http.StatusConflict, statuses[i],
				"the loser maps to a typed conflict, never a 500")
		}
	}
	require.Equal(t, 1, succeeded, "the row lock serializes them")

	require.Equal(t, preparation.StateWasted, env.UnitState(t, unit.ID))
	require.Equal(t, 1, env.CountWastes(t, unit.ID))
	require.Equal(t, 1, env.CountAlerts(t, unit.ID))
	require.Equal(t, 1, env.CountTransitionsTo(t, unit.ID, preparation.StateWasted))
	require.Equal(t, 1, env.CountAuditEvents(t, preparation.EventPreparationUnitWasted))
	require.Equal(t, 1, env.CountAuditEvents(t, preparation.EventPreparationAlertCreated))
}

func TestAdvanceAgainstWaste(t *testing.T) {
	env := newPrepEnv(t)
	unit := env.SubmittedUnits(t, 1)[0]
	_, _, err := env.Advance(t, unit.ID, preparation.StateInPreparation)
	require.NoError(t, err)

	// Lock barrier: hold the unit row in its IN_PREPARATION state while the
	// Waste transaction starts, so the waste parks at its LockPreparationUnit
	// step with its earlier read of the unit still IN_PREPARATION.
	barrier, err := env.DB.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	_, err = barrier.ExecContext(context.Background(),
		`SELECT id FROM preparation_units WHERE id = $1 FOR UPDATE`, unit.ID)
	require.NoError(t, err)

	wasteErrs := make(chan error, 1)
	go func() {
		_, _, err := env.Waste(t, unit.ID, preparation.ReasonQualityFailure, nil)
		wasteErrs <- err
	}()

	// A session of this database waiting on a lock is the parked waste; the
	// suite runs sequentially, so no other lock waiter can exist. The wait
	// shows in pg_stat_activity — the waste parks on the barrier holder's
	// transactionid, which pg_locks lists with a NULL database, so a
	// database-scoped pg_locks probe would miss it.
	require.Eventually(t, func() bool {
		var waiters int
		require.NoError(t, env.DB.QueryRow(`
			SELECT count(*) FROM pg_stat_activity
			WHERE datname = current_database()
			  AND wait_event_type = 'Lock'
			  AND pid <> pg_backend_pid()`,
		).Scan(&waiters))
		return waiters > 0
	}, 10*time.Second, 10*time.Millisecond,
		"the waste must park on the barrier's unit lock before it is released")

	require.NoError(t, barrier.Rollback(), "releasing the barrier must succeed")
	require.NoError(t, <-wasteErrs, "the waste parked behind the barrier must commit")

	// The advance starts only once the waste has committed, so its row lock
	// hands it the wasted row: it must re-read the current state under the
	// lock and refuse, never move a WASTED unit to READY.
	_, _, err = env.Advance(t, unit.ID, preparation.StateReady)
	require.ErrorIs(t, err, preparation.ErrInvalidTransition)
	status, _ := preparation.ErrorResponse(err)
	require.Equal(t, http.StatusConflict, status)

	// The final counts match the winner — the waste — only.
	require.Equal(t, preparation.StateWasted, env.UnitState(t, unit.ID))
	require.Equal(t, 1, env.CountWastes(t, unit.ID))
	require.Equal(t, 1, env.CountAlerts(t, unit.ID))
	require.Equal(t, 1, env.CountTransitionsTo(t, unit.ID, preparation.StateWasted))
	require.Equal(t, 0, env.CountTransitionsTo(t, unit.ID, preparation.StateReady),
		"the losing advance wrote no transition")
	require.Equal(t, 1, env.CountAuditEvents(t, preparation.EventPreparationUnitWasted))
	require.Equal(t, 1, env.CountAuditEvents(t, preparation.EventPreparationAlertCreated))
	require.Equal(t, 1, env.CountAuditEventsByTypeAndUnit(
		t, preparation.EventPreparationUnitAdvanced, unit.ID),
		"only the setup advance audit exists; the losing advance wrote none")
}
