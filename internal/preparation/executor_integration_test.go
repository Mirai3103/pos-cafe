//go:build integration

package preparation_test

import (
	"context"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/preparation"
	"github.com/google/uuid"
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
			if _, err := q.GetPreparationObservedAt(context.Background()); err != nil {
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
