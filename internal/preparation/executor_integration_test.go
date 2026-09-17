//go:build integration

package preparation_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/preparation"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func TestExecuteReadRequiresCapabilityAndAuditsDenial(t *testing.T) {
	env := newPrepEnv(t)

	_, err := preparation.ExecuteRead(
		context.Background(), env.PreparationRunner, env.CashierActor(),
		preparation.OpReadActiveQueue, preparation.CapPreparationOperate,
		func(*sqlc.Queries) (int, error) {
			t.Fatal("read body must not run for a cashier")
			return 0, nil
		},
	)
	require.ErrorIs(t, err, preparation.ErrForbidden)
	require.Equal(t, 1, env.CountAuditEvents(t, preparation.EventAuthorizationDenied))
}

func TestExecuteReadUsesReadOnlyTransaction(t *testing.T) {
	env := newPrepEnv(t)
	unit := env.SubmittedUnits(t, 1)[0]

	_, err := preparation.ExecuteRead(
		context.Background(), env.PreparationRunner, env.BaristaActor(),
		preparation.OpReadActiveQueue, preparation.CapPreparationOperate,
		func(q *sqlc.Queries) (int, error) {
			err := q.SetPreparationUnitState(context.Background(), sqlc.SetPreparationUnitStateParams{
				ID:         unit.ID,
				State:      preparation.StateInPreparation,
				OccurredAt: unit.QueuedAt,
			})
			return 0, err
		},
	)
	require.Error(t, err)
	require.Equal(t, preparation.StateQueued, env.UnitState(t, unit.ID))
}

func TestExecuteReadReloadsCurrentAuthority(t *testing.T) {
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
			require.NoError(t, tt.mutate(env))
			_, err := preparation.ExecuteRead(
				context.Background(), env.PreparationRunner, env.BaristaActor(),
				preparation.OpReadActiveQueue, preparation.CapPreparationOperate,
				func(*sqlc.Queries) (int, error) {
					t.Fatal("read body must not run after authority is removed")
					return 0, nil
				},
			)
			require.ErrorIs(t, err, tt.want)
			require.Equal(t, 1, env.CountAuditEvents(t, preparation.EventAuthorizationDenied))
		})
	}
}

func TestExecuteReadKeepsOneRepeatableSnapshot(t *testing.T) {
	env := newPrepEnv(t)
	unit := env.SubmittedUnits(t, 1)[0]
	var sessionID uuid.UUID
	require.NoError(t, env.DB.QueryRow(`
		SELECT o.service_session_id
		FROM preparation_units pu
		JOIN order_items oi ON oi.id = pu.order_item_id
		JOIN orders o ON o.id = oi.order_id
		WHERE pu.id = $1`, unit.ID).Scan(&sessionID))
	startUpdate := make(chan struct{})
	updated := make(chan error, 1)
	go func() {
		<-startUpdate
		// normalized_name carries CHECK (normalized_name = lower(name)), so the
		// rename must update both columns.
		_, err := env.DB.Exec(`UPDATE tables SET name = 'Bàn 2', normalized_name = lower('Bàn 2') WHERE id = $1`, env.TableID)
		updated <- err
	}()

	name, err := preparation.ExecuteRead(
		context.Background(), env.PreparationRunner, env.BaristaActor(),
		preparation.OpReadActiveQueue, preparation.CapPreparationOperate,
		func(q *sqlc.Queries) (string, error) {
			if _, err := q.GetPreparationCurrentTime(context.Background()); err != nil {
				return "", err
			}
			close(startUpdate)
			if err := <-updated; err != nil {
				return "", err
			}
			rows, err := q.ListCurrentPreparationTables(
				context.Background(), []uuid.UUID{sessionID},
			)
			if err != nil {
				return "", err
			}
			return rows[0].Name, nil
		},
	)
	require.NoError(t, err)
	require.Equal(t, "Bàn 1", name)

	var currentName string
	require.NoError(t, env.DB.QueryRow(`SELECT name FROM tables WHERE id = $1`, env.TableID).
		Scan(&currentName))
	require.Equal(t, "Bàn 2", currentName)
}

func TestExecuteReadAuditFailurePreservesDenial(t *testing.T) {
	env := newPrepEnv(t)
	_, err := env.DB.Exec(`
		CREATE FUNCTION fail_preparation_denial_audit() RETURNS trigger AS $$
		BEGIN
			IF NEW.event_type = 'preparation.authorization_denied' THEN
				RAISE EXCEPTION 'forced denial audit failure';
			END IF;
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql;
		CREATE TRIGGER fail_preparation_denial_audit
		BEFORE INSERT ON audit_events
		FOR EACH ROW EXECUTE FUNCTION fail_preparation_denial_audit();`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = env.DB.Exec(`DROP TRIGGER IF EXISTS fail_preparation_denial_audit ON audit_events`)
		_, _ = env.DB.Exec(`DROP FUNCTION IF EXISTS fail_preparation_denial_audit()`)
	})

	_, err = preparation.ExecuteRead(
		context.Background(), env.PreparationRunner, env.CashierActor(),
		preparation.OpReadActiveQueue, preparation.CapPreparationOperate,
		func(*sqlc.Queries) (int, error) {
			t.Fatal("denied read body must not run")
			return 0, nil
		},
	)
	require.ErrorIs(t, err, preparation.ErrForbidden)
}

// pinGateResult is the replayable payload the PIN-gate probe returns.
type pinGateResult struct {
	Value string `json:"value"`
}

// pinGateFingerprint is the credential-free business input of the PIN-gate
// probe. It deliberately carries no PIN: the executor's fingerprint must stay
// credential-free even for gated commands.
type pinGateFingerprint struct {
	Value string `json:"value"`
}

// pinGateProbeOp is the executor operation name of the PIN-gate probe. It is
// not one of the production operations, so probe claims can never be confused
// with a real command's stored result.
const pinGateProbeOp = "preparation.test_pin_gate"

// executePinGateProbe runs one mutation that requires the Manager self-PIN
// gate and preparation.operate. *calls counts every time the mutation body
// actually runs, so tests can prove a replay never reruns the callback. On
// error the status is mapped the way the HTTP layer would map it.
func executePinGateProbe(env *prepEnv, actor preparation.Actor, requestID uuid.UUID,
	pin string, calls *int,
) (int, pinGateResult, error) {
	status, result, err := preparation.ExecuteMutation(
		context.Background(), env.PreparationRunner, actor,
		preparation.MutationSpec{
			RequestID:         requestID,
			Operation:         pinGateProbeOp,
			Fingerprint:       pinGateFingerprint{Value: "probe"},
			Required:          []string{preparation.CapPreparationOperate},
			RequireManagerPIN: true,
			ManagerPIN:        pin,
		},
		func(preparation.MutationContext) (int, pinGateResult, preparation.AuditRecord, error) {
			*calls++
			return http.StatusOK, pinGateResult{Value: "probe-result"}, preparation.AuditRecord{}, nil
		},
	)
	if err != nil {
		status, _ = preparation.ErrorResponse(err)
	}
	return status, result, err
}

func TestExecuteMutationRequiresCurrentManagerAndOwnPINBeforeReplay(t *testing.T) {
	t.Run("current manager with own pin executes and replays", func(t *testing.T) {
		env := newPrepEnv(t)
		manager := env.ManagerActor()
		requestID := uuid.New()
		calls := 0

		status, result, err := executePinGateProbe(env, manager, requestID, env.PINOf(manager), &calls)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, status)
		require.Equal(t, pinGateResult{Value: "probe-result"}, result)
		require.Equal(t, 1, calls)
		require.Equal(t, 0, env.CountAuditEvents(t, preparation.EventAuthorizationDenied))

		claim, found := env.IdempotencyClaim(t, manager, requestID)
		require.True(t, found, "a gated success stores its replayable result")
		require.Equal(t, pinGateProbeOp, claim.Action)
		require.Equal(t, int32(http.StatusOK), claim.ResponseCode)

		// The gate passes before the replay branch is reached, so the same
		// request id returns the stored result and the callback never reruns.
		status, result, err = executePinGateProbe(env, manager, requestID, env.PINOf(manager), &calls)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, status)
		require.Equal(t, pinGateResult{Value: "probe-result"}, result)
		require.Equal(t, 1, calls)
		require.Equal(t, 0, env.CountAuditEvents(t, preparation.EventAuthorizationDenied))
	})

	t.Run("capability without the manager role is denied", func(t *testing.T) {
		env := newPrepEnv(t)
		barista := env.BaristaActor()
		requestID := uuid.New()
		calls := 0

		// The barista holds preparation.operate, so capability verification
		// alone proves nothing here; the gate's own Manager re-check denies.
		status, _, err := executePinGateProbe(env, barista, requestID, env.PINOf(barista), &calls)
		require.ErrorIs(t, err, preparation.ErrForbidden)
		require.NotErrorIs(t, err, preparation.ErrInvalidManagerPIN)
		require.Equal(t, http.StatusForbidden, status)
		require.Equal(t, 0, calls)
		require.Equal(t, 1, env.CountAuditEvents(t, preparation.EventAuthorizationDenied))

		_, found := env.IdempotencyClaim(t, barista, requestID)
		require.False(t, found, "a denial must not claim the request")
	})

	t.Run("missing pin is denied", func(t *testing.T) {
		env := newPrepEnv(t)
		manager := env.ManagerActor()
		requestID := uuid.New()
		calls := 0

		status, _, err := executePinGateProbe(env, manager, requestID, "", &calls)
		require.ErrorIs(t, err, preparation.ErrInvalidManagerPIN)
		require.Equal(t, http.StatusForbidden, status)
		require.Equal(t, 0, calls)
		require.Equal(t, 1, env.CountAuditEvents(t, preparation.EventAuthorizationDenied))
	})
}

func TestExecuteMutationWrongManagerPINWritesDenialWithoutPIN(t *testing.T) {
	env := newPrepEnv(t)
	manager := env.ManagerActor()
	requestID := uuid.New()
	calls := 0
	const wrongPIN = "9999"

	status, _, err := executePinGateProbe(env, manager, requestID, wrongPIN, &calls)
	require.ErrorIs(t, err, preparation.ErrInvalidManagerPIN)
	require.NotErrorIs(t, err, preparation.ErrForbidden,
		"the wrong-PIN sentinel stays distinct from the role/capability sentinel")
	require.Equal(t, http.StatusForbidden, status)
	_, body := preparation.ErrorResponse(err)
	require.Equal(t, "NOT_AUTHORIZED", body.Error.Code)
	require.Equal(t, 0, calls)
	require.Equal(t, 1, env.CountAuditEvents(t, preparation.EventAuthorizationDenied),
		"exactly one denial evidence row for the denied call")

	// The attempted PIN never reaches the error, the response, or the audit.
	require.NotContains(t, err.Error(), wrongPIN)
	responseJSON, marshalErr := json.Marshal(body)
	require.NoError(t, marshalErr)
	require.NotContains(t, string(responseJSON), wrongPIN)
	var details string
	require.NoError(t, env.DB.QueryRow(
		`SELECT details::text FROM audit_events WHERE event_type = $1`,
		preparation.EventAuthorizationDenied,
	).Scan(&details))
	require.NotContains(t, details, wrongPIN)
	require.Contains(t, details, pinGateProbeOp, "the denial names the denied operation")

	_, found := env.IdempotencyClaim(t, manager, requestID)
	require.False(t, found, "the denial happened before the idempotency claim")
}

func TestExecuteMutationManagerPINRotationControlsReplay(t *testing.T) {
	env := newPrepEnv(t)
	manager := env.ManagerActor()
	requestID := uuid.New()
	calls := 0

	status, firstResult, err := executePinGateProbe(env, manager, requestID, env.PINOf(manager), &calls)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, 1, calls)
	claimBefore, found := env.IdempotencyClaim(t, manager, requestID)
	require.True(t, found)

	oldPIN := env.PINOf(manager)
	env.RotatePIN(t, manager, "5678")

	// The old PIN is denied even though the exact replay exists: the gate
	// verifies the PIN before the replay lookup is ever reached.
	status, _, err = executePinGateProbe(env, manager, requestID, oldPIN, &calls)
	require.ErrorIs(t, err, preparation.ErrInvalidManagerPIN)
	require.Equal(t, http.StatusForbidden, status)
	require.Equal(t, 1, calls)
	require.Equal(t, 1, env.CountAuditEvents(t, preparation.EventAuthorizationDenied))

	// The new PIN returns the exact stored response without rerunning the
	// callback.
	status, replayResult, err := executePinGateProbe(env, manager, requestID, env.PINOf(manager), &calls)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, firstResult, replayResult)
	require.Equal(t, 1, calls)

	claimAfter, found := env.IdempotencyClaim(t, manager, requestID)
	require.True(t, found)
	require.Equal(t, claimBefore, claimAfter, "a replay never rewrites the stored result")
}

func TestExecuteMutationRemovedManagerRoleOrCapabilityDeniesReplay(t *testing.T) {
	t.Run("manager role removed while capability kept", func(t *testing.T) {
		env := newPrepEnv(t)
		manager := env.ManagerActor()
		requestID := uuid.New()
		calls := 0

		_, _, err := executePinGateProbe(env, manager, requestID, env.PINOf(manager), &calls)
		require.NoError(t, err)
		require.Equal(t, 1, calls)

		// Swap MANAGER for BARISTA: preparation.operate survives, so capability
		// verification passes and the gate's own Manager re-check is what
		// denies the replay.
		env.ReplaceRoles(t, manager, []string{auth.RoleBarista})

		status, _, err := executePinGateProbe(env, manager, requestID, env.PINOf(manager), &calls)
		require.ErrorIs(t, err, preparation.ErrForbidden)
		require.NotErrorIs(t, err, preparation.ErrInvalidManagerPIN)
		require.Equal(t, http.StatusForbidden, status)
		require.Equal(t, 1, calls)
		require.Equal(t, 1, env.CountAuditEvents(t, preparation.EventAuthorizationDenied))

		var reason string
		require.NoError(t, env.DB.QueryRow(
			`SELECT details->>'reason' FROM audit_events WHERE event_type = $1`,
			preparation.EventAuthorizationDenied,
		).Scan(&reason))
		require.Contains(t, reason, "manager role required")

		// The stored result survives untouched; the denial never claims or
		// rewrites the request.
		claim, found := env.IdempotencyClaim(t, manager, requestID)
		require.True(t, found)
		require.Equal(t, int32(http.StatusOK), claim.ResponseCode)
	})

	t.Run("capability removed", func(t *testing.T) {
		env := newPrepEnv(t)
		manager := env.ManagerActor()
		requestID := uuid.New()
		calls := 0

		_, _, err := executePinGateProbe(env, manager, requestID, env.PINOf(manager), &calls)
		require.NoError(t, err)
		require.Equal(t, 1, calls)

		// CASHIER holds neither preparation.operate nor any gate-worthy role,
		// so the replay is denied before the body can ever rerun.
		env.ReplaceRoles(t, manager, []string{auth.RoleCashier})

		status, _, err := executePinGateProbe(env, manager, requestID, env.PINOf(manager), &calls)
		require.ErrorIs(t, err, preparation.ErrForbidden)
		require.Equal(t, http.StatusForbidden, status)
		require.Equal(t, 1, calls)
		require.Equal(t, 1, env.CountAuditEvents(t, preparation.EventAuthorizationDenied))
	})
}

func TestExecuteMutationManagerIdentityAndRolesRemainLocked(t *testing.T) {
	env := newPrepEnv(t)
	manager := env.ManagerActor()
	requestID := uuid.New()
	calls := 0

	// lockUnavailable proves a row is locked by the mutation transaction: a
	// separate session asking for the lock with NOWAIT must receive the
	// lock_not_available SQLSTATE instead of blocking.
	lockUnavailable := func(t *testing.T, query string, arg any) {
		t.Helper()
		var one int
		err := env.DB.QueryRow(query, arg).Scan(&one)
		require.Error(t, err, "the mutation transaction must hold this lock")
		var pgErr *pgconn.PgError
		require.ErrorAs(t, err, &pgErr)
		require.Equal(t, "55P03", pgErr.Code)
	}

	status, _, err := preparation.ExecuteMutation(
		context.Background(), env.PreparationRunner, manager,
		preparation.MutationSpec{
			RequestID:         requestID,
			Operation:         pinGateProbeOp,
			Fingerprint:       pinGateFingerprint{Value: "lock-probe"},
			Required:          []string{preparation.CapPreparationOperate},
			RequireManagerPIN: true,
			ManagerPIN:        env.PINOf(manager),
		},
		func(preparation.MutationContext) (int, pinGateResult, preparation.AuditRecord, error) {
			calls++
			// The gate's locks persist for the whole mutation: while the body
			// runs, no other session can lock the actor's identity row or its
			// roles, so a concurrent disablement, PIN rotation, or role swap
			// cannot interleave between verification and commit.
			lockUnavailable(t,
				`SELECT id FROM staff_identities WHERE id = $1 FOR UPDATE NOWAIT`, manager.StaffID)
			lockUnavailable(t,
				`SELECT role FROM staff_operational_roles WHERE staff_identity_id = $1 FOR UPDATE NOWAIT`,
				manager.StaffID)
			return http.StatusOK, pinGateResult{Value: "locked"}, preparation.AuditRecord{}, nil
		},
	)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, 1, calls)

	// After the commit the locks are released: another session can lock the
	// same rows again.
	var id uuid.UUID
	require.NoError(t, env.DB.QueryRow(
		`SELECT id FROM staff_identities WHERE id = $1 FOR UPDATE NOWAIT`,
		manager.StaffID).Scan(&id))
	var role string
	require.NoError(t, env.DB.QueryRow(
		`SELECT role FROM staff_operational_roles WHERE staff_identity_id = $1 FOR UPDATE NOWAIT`,
		manager.StaffID).Scan(&role))
}
