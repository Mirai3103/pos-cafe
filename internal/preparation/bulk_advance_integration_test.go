//go:build integration

package preparation_test

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/preparation"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBulkAdvanceAllSuccess(t *testing.T) {
	env := newPrepEnv(t)
	units := env.SubmittedUnits(t, 3)
	ids := []uuid.UUID{units[2].ID, units[0].ID, units[1].ID}

	status, got, err := env.BulkAdvance(t, preparation.BulkAdvanceCommand{
		RequestID: uuid.New(), PreparationUnitIDs: ids,
		TargetState: preparation.StateInPreparation,
	})
	require.NoError(t, err)
	require.Equal(t, 200, status)
	require.Len(t, got.Outcomes, 3)
	for i, outcome := range got.Outcomes {
		require.Equal(t, ids[i], outcome.PreparationUnitID)
		require.Equal(t, preparation.BulkStatusAdvanced, outcome.Status)
		require.NotNil(t, outcome.Unit)
		require.NotNil(t, outcome.Unit.InPreparationAt)
		require.Empty(t, outcome.Code)
		require.Equal(t, 1, env.CountTransitions(t, ids[i]))
		require.Equal(t, 1, env.UnitAuditCount(t, ids[i]))
	}
}

func TestBulkAdvancePreservesValidUnitAmongFailures(t *testing.T) {
	env := newPrepEnv(t)
	units := env.SubmittedUnits(t, 2)
	_, _, err := env.Advance(t, units[0].ID, preparation.StateInPreparation)
	require.NoError(t, err)
	missing := uuid.New()

	_, got, err := env.BulkAdvance(t, preparation.BulkAdvanceCommand{
		RequestID:          uuid.New(),
		PreparationUnitIDs: []uuid.UUID{units[0].ID, missing, units[1].ID},
		TargetState:        preparation.StateInPreparation,
	})
	require.NoError(t, err)
	require.Equal(t, []string{
		preparation.BulkCodeInvalidTransition,
		preparation.BulkCodeUnitNotFound,
		"",
	}, []string{got.Outcomes[0].Code, got.Outcomes[1].Code, got.Outcomes[2].Code})
	require.Equal(t, preparation.BulkStatusAdvanced, got.Outcomes[2].Status)
	require.Equal(t, preparation.StateInPreparation, env.UnitState(t, units[1].ID))
	require.Equal(t, 0, env.UnitAuditCount(t, missing))
}

func TestBulkAdvanceAllFailuresReturns200(t *testing.T) {
	env := newPrepEnv(t)
	units := env.SubmittedUnits(t, 1)
	_, _, err := env.Advance(t, units[0].ID, preparation.StateInPreparation)
	require.NoError(t, err)
	missing := uuid.New()

	staleTransitions := env.CountTransitions(t, units[0].ID)
	staleAudits := env.UnitAuditCount(t, units[0].ID)

	status, got, err := env.BulkAdvance(t, preparation.BulkAdvanceCommand{
		RequestID:          uuid.New(),
		PreparationUnitIDs: []uuid.UUID{missing, units[0].ID},
		TargetState:        preparation.StateInPreparation,
	})
	require.NoError(t, err, "all-failed outcomes are still a 200, not an error")
	require.Equal(t, 200, status)
	require.Len(t, got.Outcomes, 2)

	require.Equal(t, missing, got.Outcomes[0].PreparationUnitID)
	require.Equal(t, preparation.BulkStatusFailed, got.Outcomes[0].Status)
	require.Equal(t, preparation.BulkCodeUnitNotFound, got.Outcomes[0].Code)
	require.Nil(t, got.Outcomes[0].Unit)

	require.Equal(t, units[0].ID, got.Outcomes[1].PreparationUnitID)
	require.Equal(t, preparation.BulkStatusFailed, got.Outcomes[1].Status)
	require.Equal(t, preparation.BulkCodeInvalidTransition, got.Outcomes[1].Code)
	require.Nil(t, got.Outcomes[1].Unit)

	require.Equal(t, staleTransitions, env.CountTransitions(t, units[0].ID),
		"the command must add zero transitions to the stale unit")
	require.Equal(t, staleAudits, env.UnitAuditCount(t, units[0].ID),
		"the command must add zero audits to the stale unit")
	require.Equal(t, 0, env.CountTransitions(t, missing))
	require.Equal(t, 0, env.UnitAuditCount(t, missing))
}

func TestBulkAdvanceDeduplicatesAtFirstOccurrence(t *testing.T) {
	env := newPrepEnv(t)
	units := env.SubmittedUnits(t, 2)
	first, second := units[0].ID, units[1].ID

	status, got, err := env.BulkAdvance(t, preparation.BulkAdvanceCommand{
		RequestID:          uuid.New(),
		PreparationUnitIDs: []uuid.UUID{second, first, second},
		TargetState:        preparation.StateInPreparation,
	})
	require.NoError(t, err)
	require.Equal(t, 200, status)
	require.Len(t, got.Outcomes, 2, "a duplicate id collapses to its first occurrence")
	require.Equal(t, second, got.Outcomes[0].PreparationUnitID,
		"outcomes keep first-selection order")
	require.Equal(t, first, got.Outcomes[1].PreparationUnitID)
	for _, outcome := range got.Outcomes {
		require.Equal(t, preparation.BulkStatusAdvanced, outcome.Status)
	}
	require.Equal(t, 1, env.CountTransitions(t, second), "each id is moved once")
	require.Equal(t, 1, env.CountTransitions(t, first))
	require.Equal(t, 2, env.CountAuditEvents(t, preparation.EventPreparationUnitAdvanced))
}

func TestBulkAdvanceAcceptsFiftyUnits(t *testing.T) {
	env := newPrepEnv(t)
	units := env.SubmittedUnits(t, 50)
	require.Len(t, units, 50)
	ids := make([]uuid.UUID, 0, len(units))
	for _, unit := range units {
		ids = append(ids, unit.ID)
	}

	status, got, err := env.BulkAdvance(t, preparation.BulkAdvanceCommand{
		RequestID:          uuid.New(),
		PreparationUnitIDs: ids,
		TargetState:        preparation.StateInPreparation,
	})
	require.NoError(t, err)
	require.Equal(t, 200, status)
	require.Len(t, got.Outcomes, 50)
	for i, outcome := range got.Outcomes {
		require.Equal(t, ids[i], outcome.PreparationUnitID)
		require.Equal(t, preparation.BulkStatusAdvanced, outcome.Status)
		require.NotNil(t, outcome.Unit)
	}
	var advanced int
	require.NoError(t, env.DB.QueryRow(
		`SELECT count(*) FROM preparation_units WHERE state = $1`,
		preparation.StateInPreparation,
	).Scan(&advanced))
	require.Equal(t, 50, advanced)
	var transitions int
	require.NoError(t, env.DB.QueryRow(
		`SELECT count(*) FROM preparation_unit_transitions`,
	).Scan(&transitions))
	require.Equal(t, 50, transitions)
	require.Equal(t, 50, env.CountAuditEvents(t, preparation.EventPreparationUnitAdvanced))
}

func TestBulkAdvanceRestoresFirstSelectionOrder(t *testing.T) {
	env := newPrepEnv(t)
	units := env.SubmittedUnits(t, 3)
	ids := []uuid.UUID{units[0].ID, units[1].ID, units[2].ID}

	// Submit the ids in deliberately reverse-sorted (descending byte) order, so
	// the lock order the handler takes is the exact opposite of selection order.
	lockOrder := append([]uuid.UUID(nil), ids...)
	slices.SortFunc(lockOrder, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	slices.Reverse(lockOrder)
	for i := 1; i < len(lockOrder); i++ {
		require.Positive(t, bytes.Compare(lockOrder[i-1][:], lockOrder[i][:]),
			"the submitted order must be strictly descending")
	}

	status, got, err := env.BulkAdvance(t, preparation.BulkAdvanceCommand{
		RequestID:          uuid.New(),
		PreparationUnitIDs: lockOrder,
		TargetState:        preparation.StateInPreparation,
	})
	require.NoError(t, err)
	require.Equal(t, 200, status)
	require.Len(t, got.Outcomes, len(lockOrder))
	for i, outcome := range got.Outcomes {
		require.Equal(t, lockOrder[i], outcome.PreparationUnitID,
			"response order must be the submitted first-selection order, not lock order")
		require.Equal(t, preparation.BulkStatusAdvanced, outcome.Status)
	}
}

func TestBulkAdvanceReplayIsExact(t *testing.T) {
	env := newPrepEnv(t)
	units := env.SubmittedUnits(t, 2)
	ids := []uuid.UUID{units[0].ID, units[1].ID}
	cmd := preparation.BulkAdvanceCommand{
		RequestID:          uuid.New(),
		PreparationUnitIDs: ids,
		TargetState:        preparation.StateInPreparation,
	}

	_, first, err := env.BulkAdvance(t, cmd)
	require.NoError(t, err)
	firstJSON, err := json.Marshal(first)
	require.NoError(t, err)

	transitions := env.CountTransitions(t, units[0].ID)
	unitAudits := env.UnitAuditCount(t, units[0].ID)
	audits := env.CountAuditEvents(t, preparation.EventPreparationUnitAdvanced)

	_, second, err := env.BulkAdvance(t, cmd)
	require.NoError(t, err)
	secondJSON, err := json.Marshal(second)
	require.NoError(t, err)

	require.Equal(t, string(firstJSON), string(secondJSON),
		"the replay must return the exact stored response")
	require.Equal(t, transitions, env.CountTransitions(t, units[0].ID),
		"a replay records nothing further")
	require.Equal(t, unitAudits, env.UnitAuditCount(t, units[0].ID))
	require.Equal(t, audits, env.CountAuditEvents(t, preparation.EventPreparationUnitAdvanced))
	require.Equal(t, preparation.StateInPreparation, env.UnitState(t, units[0].ID))
}

func TestBulkAdvanceRejectsConflictingReplay(t *testing.T) {
	env := newPrepEnv(t)
	units := env.SubmittedUnits(t, 2)
	requestID := uuid.New()

	_, _, err := env.BulkAdvance(t, preparation.BulkAdvanceCommand{
		RequestID:          requestID,
		PreparationUnitIDs: []uuid.UUID{units[0].ID, units[1].ID},
		TargetState:        preparation.StateInPreparation,
	})
	require.NoError(t, err)

	_, _, err = env.BulkAdvance(t, preparation.BulkAdvanceCommand{
		RequestID:          requestID,
		PreparationUnitIDs: []uuid.UUID{units[0].ID, units[1].ID},
		TargetState:        preparation.StateReady,
	})
	require.ErrorIs(t, err, preparation.ErrRequestConflict,
		"the same request id with a different target is a conflict")

	_, _, err = env.BulkAdvance(t, preparation.BulkAdvanceCommand{
		RequestID:          requestID,
		PreparationUnitIDs: []uuid.UUID{units[1].ID},
		TargetState:        preparation.StateInPreparation,
	})
	require.ErrorIs(t, err, preparation.ErrRequestConflict,
		"the same request id with a different normalized id list is a conflict")

	require.Equal(t, 1, env.CountTransitions(t, units[0].ID))
	require.Equal(t, 1, env.CountTransitions(t, units[1].ID))
}

func TestBulkAdvanceRevalidatesAuthorityBeforeReplay(t *testing.T) {
	env := newPrepEnv(t)
	units := env.SubmittedUnits(t, 1)
	cmd := preparation.BulkAdvanceCommand{
		RequestID:          uuid.New(),
		PreparationUnitIDs: []uuid.UUID{units[0].ID},
		TargetState:        preparation.StateInPreparation,
	}
	_, _, err := env.BulkAdvance(t, cmd)
	require.NoError(t, err)

	_, err = env.DB.Exec(
		`UPDATE staff_access_sessions SET revoked_at = now() WHERE id = $1`,
		env.BaristaActor().SessionID)
	require.NoError(t, err)

	_, _, err = env.BulkAdvance(t, cmd)
	require.ErrorIs(t, err, preparation.ErrUnauthorized,
		"the current authority is rechecked even on the replay path")
	require.Equal(t, 1, env.CountTransitions(t, units[0].ID))
	require.Equal(t, 1, env.UnitAuditCount(t, units[0].ID))
}

func TestBulkAdvanceUnexpectedStoreFailureRollsBackEverything(t *testing.T) {
	env := newPrepEnv(t)
	units := env.SubmittedUnits(t, 2)
	ids := []uuid.UUID{units[0].ID, units[1].ID}

	// The idempotency claim is written with response_code 0 at INSERT time; the
	// result store is the UPDATE that sets response_code to 200. Raising there
	// forces the executor's last write to fail with the units already advanced.
	_, err := env.DB.Exec(`
		CREATE OR REPLACE FUNCTION fail_preparation_result_store() RETURNS trigger AS $$
		BEGIN
			IF NEW.response_code <> 0 AND NEW.action = 'preparation.bulk_advance' THEN
				RAISE EXCEPTION 'forced idempotency result failure';
			END IF;
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql;
		CREATE TRIGGER fail_preparation_result_store
		BEFORE UPDATE ON idempotency_keys
		FOR EACH ROW EXECUTE FUNCTION fail_preparation_result_store();`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = env.DB.Exec(`DROP TRIGGER IF EXISTS fail_preparation_result_store ON idempotency_keys`)
		_, _ = env.DB.Exec(`DROP FUNCTION IF EXISTS fail_preparation_result_store()`)
	})

	requestID := uuid.New()
	_, _, err = env.BulkAdvance(t, preparation.BulkAdvanceCommand{
		RequestID:          requestID,
		PreparationUnitIDs: ids,
		TargetState:        preparation.StateInPreparation,
	})
	require.Error(t, err, "the forced store failure must abort the whole command")

	for _, id := range ids {
		require.Equal(t, preparation.StateQueued, env.UnitState(t, id),
			"the failed outer transaction must leave the unit queued")
		require.Equal(t, 0, env.CountTransitions(t, id))
		require.Equal(t, 0, env.UnitAuditCount(t, id))
	}
	var claims int
	require.NoError(t, env.DB.QueryRow(
		`SELECT count(*) FROM idempotency_keys WHERE actor_id = $1 AND key = $2`,
		env.BaristaActor().StaffID, requestID,
	).Scan(&claims))
	require.Zero(t, claims, "the failed transaction must not leave its claim behind")
}

func TestBulkAdvanceAuditFailureRollsBackEverything(t *testing.T) {
	env := newPrepEnv(t)
	units := env.SubmittedUnits(t, 2)
	ids := []uuid.UUID{units[0].ID, units[1].ID}

	_, err := env.DB.Exec(`
		CREATE OR REPLACE FUNCTION fail_preparation_advance_audit() RETURNS trigger AS $$
		BEGIN
			IF NEW.event_type = 'PREPARATION_UNIT_ADVANCED' THEN
				RAISE EXCEPTION 'forced preparation audit failure';
			END IF;
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql;
		CREATE TRIGGER fail_preparation_advance_audit
		BEFORE INSERT ON audit_events
		FOR EACH ROW EXECUTE FUNCTION fail_preparation_advance_audit();`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = env.DB.Exec(`DROP TRIGGER IF EXISTS fail_preparation_advance_audit ON audit_events`)
		_, _ = env.DB.Exec(`DROP FUNCTION IF EXISTS fail_preparation_advance_audit()`)
	})

	requestID := uuid.New()
	_, _, err = env.BulkAdvance(t, preparation.BulkAdvanceCommand{
		RequestID:          requestID,
		PreparationUnitIDs: ids,
		TargetState:        preparation.StateInPreparation,
	})
	require.Error(t, err, "the forced audit failure must abort the whole command")

	for _, id := range ids {
		require.Equal(t, preparation.StateQueued, env.UnitState(t, id))
		require.Equal(t, 0, env.CountTransitions(t, id))
		require.Equal(t, 0, env.UnitAuditCount(t, id))
	}
	require.Equal(t, 0, env.CountAuditEvents(t, preparation.EventPreparationUnitAdvanced))
	var claims int
	require.NoError(t, env.DB.QueryRow(
		`SELECT count(*) FROM idempotency_keys WHERE actor_id = $1 AND key = $2`,
		env.BaristaActor().StaffID, requestID,
	).Scan(&claims))
	require.Zero(t, claims, "the failed transaction must not leave its claim behind")
}

func TestBulkAdvanceAuthorityDenialsAreAudited(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*prepEnv) error
		want   error
	}{
		{"locked", func(e *prepEnv) error {
			_, err := e.DB.Exec(`UPDATE staff_access_sessions SET state = 'locked' WHERE id = $1`, e.barista.SessionID)
			return err
		}, preparation.ErrUnauthorized},
		{"expired", func(e *prepEnv) error {
			_, err := e.DB.Exec(`UPDATE staff_access_sessions SET expires_at = now() - interval '1 minute' WHERE id = $1`, e.barista.SessionID)
			return err
		}, preparation.ErrUnauthorized},
		{"revoked", func(e *prepEnv) error {
			_, err := e.DB.Exec(`UPDATE staff_access_sessions SET revoked_at = now() WHERE id = $1`, e.barista.SessionID)
			return err
		}, preparation.ErrUnauthorized},
		{"disabled", func(e *prepEnv) error {
			_, err := e.DB.Exec(`UPDATE staff_identities SET enabled = false WHERE id = $1`, e.barista.StaffID)
			return err
		}, preparation.ErrForbidden},
		{"role revoked", func(e *prepEnv) error {
			_, err := e.DB.Exec(`DELETE FROM staff_operational_roles WHERE staff_identity_id = $1`, e.barista.StaffID)
			return err
		}, preparation.ErrForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newPrepEnv(t)
			units := env.SubmittedUnits(t, 2)
			require.NoError(t, tt.mutate(env))

			_, _, err := preparation.NewBulkAdvanceHandler(env.PreparationRunner).
				Handle(context.Background(), env.BaristaActor(), preparation.BulkAdvanceCommand{
					RequestID:          uuid.New(),
					PreparationUnitIDs: []uuid.UUID{units[0].ID, units[1].ID},
					TargetState:        preparation.StateInPreparation,
				})
			require.ErrorIs(t, err, tt.want)

			for _, unit := range units {
				require.Equal(t, preparation.StateQueued, env.UnitState(t, unit.ID),
					"the denial must have no callback effects")
				require.Equal(t, 0, env.CountTransitions(t, unit.ID))
				require.Equal(t, 0, env.UnitAuditCount(t, unit.ID))
			}
			var denials int
			require.NoError(t, env.DB.QueryRow(
				`SELECT count(*) FROM audit_events
				 WHERE event_type = $1 AND details->>'operation' = $2`,
				preparation.EventAuthorizationDenied, preparation.OpBulkAdvance,
			).Scan(&denials))
			require.Equal(t, 1, denials,
				"exactly one denial audit row naming the bulk operation")
		})
	}
}

func TestOverlappingBulkAdvanceDoesNotDeadlockOrDoubleApply(t *testing.T) {
	env := newPrepEnv(t)
	units := env.SubmittedUnits(t, 2)
	a, b := units[0].ID, units[1].ID

	type call struct {
		cmd    preparation.BulkAdvanceCommand
		status int
		resp   preparation.BulkAdvanceResponse
		err    error
	}
	calls := []call{
		{cmd: preparation.BulkAdvanceCommand{
			RequestID:          uuid.New(),
			PreparationUnitIDs: []uuid.UUID{a, b},
			TargetState:        preparation.StateInPreparation,
		}},
		{cmd: preparation.BulkAdvanceCommand{
			RequestID:          uuid.New(),
			PreparationUnitIDs: []uuid.UUID{b, a},
			TargetState:        preparation.StateInPreparation,
		}},
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		var wg sync.WaitGroup
		for i := range calls {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				calls[i].status, calls[i].resp, calls[i].err = env.BulkAdvance(t, calls[i].cmd)
			}(i)
		}
		wg.Wait()
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("overlapping bulk advances deadlocked")
	}

	advanced := make(map[uuid.UUID]int)
	failed := make(map[uuid.UUID]int)
	for i := range calls {
		require.NoError(t, calls[i].err)
		require.Equal(t, 200, calls[i].status)
		require.Len(t, calls[i].resp.Outcomes, 2)
		for _, outcome := range calls[i].resp.Outcomes {
			switch outcome.Status {
			case preparation.BulkStatusAdvanced:
				advanced[outcome.PreparationUnitID]++
				assert.NotNil(t, outcome.Unit)
			case preparation.BulkStatusFailed:
				require.Equal(t, preparation.BulkCodeInvalidTransition, outcome.Code)
				failed[outcome.PreparationUnitID]++
			}
		}
	}
	for _, id := range []uuid.UUID{a, b} {
		require.Equal(t, 1, advanced[id], "each unit must advance exactly once")
		require.Equal(t, 1, failed[id], "the competing command must report INVALID_TRANSITION")
		require.Equal(t, 1, env.CountTransitions(t, id))
		require.Equal(t, 1, env.UnitAuditCount(t, id))
	}
}
