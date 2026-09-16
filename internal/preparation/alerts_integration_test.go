//go:build integration

package preparation_test

import (
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/preparation"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// wastedAlertWithUnit creates one submitted unit, advances it to
// IN_PREPARATION, wastes it, and returns the unit id, the fresh unacknowledged
// WASTE alert's id, and the owning Service Session's id — the arrangement every
// acknowledgment test starts from.
func wastedAlertWithUnit(t *testing.T, env *prepEnv) (unitID, alertID, sessionID uuid.UUID) {
	t.Helper()
	unit := env.SubmittedUnits(t, 1)[0]
	_, _, err := env.Advance(t, unit.ID, preparation.StateInPreparation)
	require.NoError(t, err)
	waste, _, err := env.Waste(t, unit.ID, preparation.ReasonQualityFailure, nil)
	require.NoError(t, err)
	return unit.ID, waste.Alert.ID, env.SessionIDForUnit(t, unit.ID)
}

func TestAcknowledgeAlert(t *testing.T) {
	t.Run("Manager and Barista fill actor, session, and timestamp together and return 200", func(t *testing.T) {
		actors := []struct {
			name  string
			actor func(e *prepEnv) preparation.Actor
		}{
			{"manager", func(e *prepEnv) preparation.Actor { return e.ManagerActor() }},
			{"barista", func(e *prepEnv) preparation.Actor { return e.BaristaActor() }},
		}
		for _, tc := range actors {
			t.Run(tc.name, func(t *testing.T) {
				env := newPrepEnv(t)
				unitID, alertID, _ := wastedAlertWithUnit(t, env)
				actor := tc.actor(env)

				requestID := uuid.New()
				resp, status, err := env.AcknowledgeWithRequestIDAs(t, requestID, actor, alertID)
				require.NoError(t, err)
				require.Equal(t, http.StatusOK, status, "a first acknowledgment answers 200")

				// Exact response fields: the alert's immutable meaning plus the
				// acknowledgment evidence, with creation untouched.
				require.Equal(t, alertID, resp.ID)
				require.Equal(t, unitID, resp.PreparationUnitID)
				require.Equal(t, preparation.AlertKindWaste, resp.Kind)
				require.Equal(t, preparation.ReasonQualityFailure, resp.Reason)
				require.Nil(t, resp.Note)
				require.NotNil(t, resp.AcknowledgedByStaffIdentityID)
				require.Equal(t, actor.StaffID, *resp.AcknowledgedByStaffIdentityID)
				require.NotNil(t, resp.AcknowledgedAt)
				require.False(t, resp.AcknowledgedAt.IsZero())

				// The database tuple is filled together: identity, session, and
				// time, exactly as the response reports them.
				ack := env.AlertAcknowledgment(t, alertID)
				require.NotNil(t, ack.AcknowledgedBy, "the acking actor is stored")
				require.Equal(t, actor.StaffID, *ack.AcknowledgedBy)
				require.NotNil(t, ack.AcknowledgedSessionID, "the acking session is stored")
				require.Equal(t, actor.SessionID, *ack.AcknowledgedSessionID)
				require.NotNil(t, ack.AcknowledgedAt, "the acknowledgment time is stored")
				require.True(t, resp.AcknowledgedAt.Equal(*ack.AcknowledgedAt),
					"the response reads back the stored evidence")

				// Exactly one audit event, sharing the acknowledgment's one
				// timestamp.
				audits := env.UnitAuditEvents(t, unitID, preparation.EventPreparationAlertAcknowledged)
				require.Len(t, audits, 1)
				require.True(t, ack.AcknowledgedAt.Equal(audits[0].OccurredAt),
					"the audit shares the acknowledgment's one timestamp")

				// One stored result: the exact response body, replayable.
				claim, ok := env.IdempotencyClaim(t, actor, requestID)
				require.True(t, ok, "a success stores its replayable result")
				require.Equal(t, preparation.OpAcknowledgeAlert, claim.Action)
				require.Equal(t, int32(http.StatusOK), claim.ResponseCode)
				respJSON, err := json.Marshal(resp)
				require.NoError(t, err)
				require.JSONEq(t, string(respJSON), string(claim.ResponseBody))
			})
		}
	})

	t.Run("an unknown alert is not found", func(t *testing.T) {
		env := newPrepEnv(t)
		_, status, err := env.Acknowledge(t, uuid.New())
		require.ErrorIs(t, err, preparation.ErrAlertNotFound)
		require.Equal(t, http.StatusNotFound, status)
	})

	t.Run("a separately requested already-acknowledged alert is a conflict", func(t *testing.T) {
		env := newPrepEnv(t)
		_, alertID, _ := wastedAlertWithUnit(t, env)

		_, _, err := env.Acknowledge(t, alertID)
		require.NoError(t, err)
		stored := env.AlertAcknowledgment(t, alertID)

		// A second request id is a new acknowledgment request, not a replay:
		// the alert's own state is what rejects it.
		second, status, err := env.Acknowledge(t, alertID)
		require.ErrorIs(t, err, preparation.ErrAlertAlreadyAcknowledged)
		require.Equal(t, http.StatusConflict, status)
		require.Equal(t, preparation.AlertResponse{}, second,
			"a rejected acknowledgment returns no result")

		require.Equal(t, stored, env.AlertAcknowledgment(t, alertID),
			"the rejected request overwrites no evidence")
		require.Equal(t, 1, env.CountAuditEvents(t, preparation.EventPreparationAlertAcknowledged),
			"the rejected request writes no second audit")
	})

	t.Run("an exact replay returns the original identity and time with one audit", func(t *testing.T) {
		env := newPrepEnv(t)
		_, alertID, _ := wastedAlertWithUnit(t, env)

		requestID := uuid.New()
		first, status, err := env.AcknowledgeWithRequestID(t, requestID, alertID)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, status)

		stored := env.AlertAcknowledgment(t, alertID)
		auditsBefore := env.CountAuditEvents(t, preparation.EventPreparationAlertAcknowledged)

		second, status, err := env.AcknowledgeWithRequestID(t, requestID, alertID)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, status, "the replay returns the stored 200")

		firstJSON, err := json.Marshal(first)
		require.NoError(t, err)
		secondJSON, err := json.Marshal(second)
		require.NoError(t, err)
		require.JSONEq(t, string(firstJSON), string(secondJSON),
			"the replay returns the exact stored response, original identity and time")

		require.Equal(t, stored, env.AlertAcknowledgment(t, alertID),
			"the replay rewrites no evidence")
		require.Equal(t, auditsBefore, env.CountAuditEvents(t, preparation.EventPreparationAlertAcknowledged),
			"a replay records no duplicate audit")
	})

	t.Run("acknowledgment changes no unit state, Waste, charge, or closure readiness", func(t *testing.T) {
		env := newPrepEnv(t)
		unitID, alertID, sessionID := wastedAlertWithUnit(t, env)

		financialsBefore := env.SessionFinancials(t, sessionID)
		stateBefore := env.UnitState(t, unitID)
		wastesBefore := env.CountWastes(t, unitID)
		alertsBefore := env.CountAlerts(t, unitID)
		transitionsBefore := env.CountTransitions(t, unitID)
		auditsBefore := env.CountAuditEventsByTypeAndUnit(
			t, preparation.EventPreparationAlertAcknowledged, unitID)

		_, status, err := env.Acknowledge(t, alertID)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, status)

		require.Equal(t, stateBefore, env.UnitState(t, unitID),
			"acknowledgment changes no unit state")
		require.Equal(t, wastesBefore, env.CountWastes(t, unitID),
			"acknowledgment records no Waste")
		require.Equal(t, alertsBefore, env.CountAlerts(t, unitID),
			"acknowledgment creates no alert")
		require.Equal(t, transitionsBefore, env.CountTransitions(t, unitID),
			"acknowledgment records no transition")
		require.Equal(t, auditsBefore+1, env.CountAuditEventsByTypeAndUnit(
			t, preparation.EventPreparationAlertAcknowledged, unitID),
			"only the acknowledgment's own audit appears")
		require.Equal(t, financialsBefore, env.SessionFinancials(t, sessionID),
			"acknowledgment changes no Check charge, Payment, or closure readiness")
	})

	t.Run("an alert remains acknowledgeable after its Session closes", func(t *testing.T) {
		env := newPrepEnv(t)
		_, alertID, sessionID := wastedAlertWithUnit(t, env)

		env.SettleAndCloseSession(t, sessionID)

		resp, status, err := env.Acknowledge(t, alertID)
		require.NoError(t, err, "an alert stays acknowledgeable after its Session closes")
		require.Equal(t, http.StatusOK, status)
		require.NotNil(t, resp.AcknowledgedByStaffIdentityID)
		require.Equal(t, env.BaristaActor().StaffID, *resp.AcknowledgedByStaffIdentityID)
		require.NotNil(t, resp.AcknowledgedAt)

		ack := env.AlertAcknowledgment(t, alertID)
		require.NotNil(t, ack.AcknowledgedBy)
		require.NotNil(t, ack.AcknowledgedSessionID)
		require.NotNil(t, ack.AcknowledgedAt)
	})
}

func TestConcurrentAcknowledge(t *testing.T) {
	env := newPrepEnv(t)
	_, alertID, _ := wastedAlertWithUnit(t, env)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	statuses := make([]int, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, statuses[i], errs[i] = env.Acknowledge(t, alertID)
		}(i)
	}
	wg.Wait()

	succeeded := 0
	for i := range errs {
		if errs[i] == nil {
			succeeded++
			require.Equal(t, http.StatusOK, statuses[i])
		} else {
			require.ErrorIs(t, errs[i], preparation.ErrAlertAlreadyAcknowledged,
				"the loser re-reads the acknowledged row under the lock")
			require.Equal(t, http.StatusConflict, statuses[i],
				"the loser maps to a typed conflict, never a 500")
		}
	}
	require.Equal(t, 1, succeeded, "the row lock serializes them")

	ack := env.AlertAcknowledgment(t, alertID)
	require.NotNil(t, ack.AcknowledgedBy)
	require.NotNil(t, ack.AcknowledgedSessionID)
	require.NotNil(t, ack.AcknowledgedAt)
	require.Equal(t, 1, env.CountAuditEvents(t, preparation.EventPreparationAlertAcknowledged))
}

func TestAcknowledgeRollbacksAreAtomic(t *testing.T) {
	t.Run("forced audit failure restores all-null evidence and rolls back the claim", func(t *testing.T) {
		env := newPrepEnv(t)
		_, alertID, _ := wastedAlertWithUnit(t, env)

		// The acknowledgment writes its one audit inside the mutation; raising
		// on that event type forces the whole transaction — the evidence
		// UPDATE included — to abort.
		_, err := env.DB.Exec(`
			CREATE FUNCTION fail_preparation_alert_acknowledged_audit() RETURNS trigger AS $$
			BEGIN
				IF NEW.event_type = 'PREPARATION_ALERT_ACKNOWLEDGED' THEN
					RAISE EXCEPTION 'forced preparation audit failure';
				END IF;
				RETURN NEW;
			END;
			$$ LANGUAGE plpgsql;
			CREATE TRIGGER fail_preparation_alert_acknowledged_audit
			BEFORE INSERT ON audit_events
			FOR EACH ROW EXECUTE FUNCTION fail_preparation_alert_acknowledged_audit();`)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = env.DB.Exec(`DROP TRIGGER IF EXISTS fail_preparation_alert_acknowledged_audit ON audit_events`)
			_, _ = env.DB.Exec(`DROP FUNCTION IF EXISTS fail_preparation_alert_acknowledged_audit()`)
		})

		requestID := uuid.New()
		_, _, err = env.AcknowledgeWithRequestID(t, requestID, alertID)
		require.Error(t, err, "the forced audit failure must abort the whole command")

		ack := env.AlertAcknowledgment(t, alertID)
		require.Nil(t, ack.AcknowledgedBy, "the failed transaction must restore all-null evidence")
		require.Nil(t, ack.AcknowledgedSessionID)
		require.Nil(t, ack.AcknowledgedAt)
		require.Equal(t, 0, env.CountAuditEvents(t, preparation.EventPreparationAlertAcknowledged))

		_, ok := env.IdempotencyClaim(t, env.BaristaActor(), requestID)
		require.False(t, ok, "the failed transaction must not leave its claim behind")
	})

	t.Run("forced result-storage failure restores all-null evidence and rolls back the claim", func(t *testing.T) {
		env := newPrepEnv(t)
		_, alertID, _ := wastedAlertWithUnit(t, env)

		// The idempotency claim is written with response_code 0 at INSERT
		// time; the result store is the UPDATE that sets response_code to 200.
		// Raising there forces the executor's last write to fail with the
		// acknowledgment already applied.
		_, err := env.DB.Exec(`
			CREATE OR REPLACE FUNCTION fail_preparation_acknowledge_result_store() RETURNS trigger AS $$
			BEGIN
				IF NEW.response_code <> 0 AND NEW.action = 'preparation.acknowledge_alert' THEN
					RAISE EXCEPTION 'forced acknowledgment result store failure';
				END IF;
				RETURN NEW;
			END;
			$$ LANGUAGE plpgsql;
			CREATE TRIGGER fail_preparation_acknowledge_result_store
			BEFORE UPDATE ON idempotency_keys
			FOR EACH ROW EXECUTE FUNCTION fail_preparation_acknowledge_result_store();`)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = env.DB.Exec(`DROP TRIGGER IF EXISTS fail_preparation_acknowledge_result_store ON idempotency_keys`)
			_, _ = env.DB.Exec(`DROP FUNCTION IF EXISTS fail_preparation_acknowledge_result_store()`)
		})

		requestID := uuid.New()
		_, _, err = env.AcknowledgeWithRequestID(t, requestID, alertID)
		require.Error(t, err, "the forced store failure must abort the whole command")

		ack := env.AlertAcknowledgment(t, alertID)
		require.Nil(t, ack.AcknowledgedBy, "the failed transaction must restore all-null evidence")
		require.Nil(t, ack.AcknowledgedSessionID)
		require.Nil(t, ack.AcknowledgedAt)
		require.Equal(t, 0, env.CountAuditEvents(t, preparation.EventPreparationAlertAcknowledged))

		_, ok := env.IdempotencyClaim(t, env.BaristaActor(), requestID)
		require.False(t, ok, "the failed transaction must not leave its claim behind")
	})
}
