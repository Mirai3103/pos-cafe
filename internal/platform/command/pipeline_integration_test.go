//go:build integration

package command_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/platform/command"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	probeDenialEvent = "probe.authorization_denied"
	probeEvent       = "PROBE_DONE"
	capAdminister    = "tables.administer"
	testManagerPIN   = "4826"
)

var errProbeWrongPIN = errors.New("probe wrong pin")

// strictPolicy mirrors how the Tables and Catalog slices audit denials.
var strictPolicy = command.Policy{
	DenialEventType:      probeDenialEvent,
	LogScope:             "probe",
	Attribution:          command.AttributeActorIfConfirmed,
	OnDenialAuditFailure: command.ReturnAuditError,
}

// lenientPolicy mirrors how the Sales, Shift, and Preparation slices audit
// denials.
var lenientPolicy = command.Policy{
	DenialEventType:  probeDenialEvent,
	LogScope:         "probe",
	AuditReadDenials: true,
	SecurityDenials:  []error{errProbeWrongPIN},
}

type probeResult struct {
	Value string `json:"value"`
}

type identity struct {
	command.Actor
	LoginCode string
}

func newRunner(t *testing.T, policy command.Policy) (*command.Runner, *sqlc.Queries) {
	t.Helper()
	require.NotNil(t, commandTestDB)
	q := sqlc.New(commandTestDB)
	return command.NewRunner(commandTestDB, q, policy), q
}

// newIdentity creates an enabled identity with a live session. Identities are
// never deleted between runs, so login codes carry enough entropy not to
// collide with earlier runs.
func newIdentity(t *testing.T, q *sqlc.Queries, roles ...string) identity {
	t.Helper()
	ctx := context.Background()

	pinHash, err := auth.HashPin(testManagerPIN)
	require.NoError(t, err)
	loginCode := "C" + strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", "")[:20])
	row, err := q.CreateStaffIdentity(ctx, sqlc.CreateStaffIdentityParams{
		DisplayName: "Command Test " + loginCode,
		Btrim:       loginCode,
		PinHash:     pinHash,
		Enabled:     true,
	})
	require.NoError(t, err)
	for _, role := range roles {
		require.NoError(t, q.AddStaffRole(ctx, sqlc.AddStaffRoleParams{StaffIdentityID: row.ID, Role: role}))
	}
	session, err := q.CreateStaffSession(ctx, sqlc.CreateStaffSessionParams{
		TokenHash:           "tok_" + uuid.NewString(),
		StaffIdentityID:     row.ID,
		State:               auth.SessionStateActive,
		LastHumanActivityAt: time.Now(),
		ExpiresAt:           time.Now().Add(8 * time.Hour),
	})
	require.NoError(t, err)
	return identity{Actor: command.Actor{StaffID: row.ID, SessionID: session.ID}, LoginCode: loginCode}
}

// newOperation returns an operation name unique to one test, so audit rows
// can be found by it. idempotency_keys.action is VARCHAR(50).
func newOperation() string {
	return "probe." + uuid.NewString()[:8]
}

type auditRow struct {
	EventType string
	ActorID   uuid.NullUUID
	SessionID uuid.NullUUID
	Reason    string
}

func auditRows(t *testing.T, operation string) []auditRow {
	t.Helper()
	rows, err := commandTestDB.Query(`
		SELECT event_type, actor_id, session_id, coalesce(details->>'reason', '')
		FROM audit_events
		WHERE details->>'operation' = $1 OR details->>'probe_operation' = $1
		ORDER BY occurred_at, id`, operation)
	require.NoError(t, err)
	defer rows.Close()
	var out []auditRow
	for rows.Next() {
		var r auditRow
		require.NoError(t, rows.Scan(&r.EventType, &r.ActorID, &r.SessionID, &r.Reason))
		out = append(out, r)
	}
	require.NoError(t, rows.Err())
	return out
}

func mustNotRun[T any](t *testing.T) func(command.MutationContext) (int, T, command.AuditRecord, error) {
	return func(command.MutationContext) (int, T, command.AuditRecord, error) {
		t.Error("mutation body must not run")
		var zero T
		return 0, zero, command.AuditRecord{}, nil
	}
}

func okBody(value string) func(command.MutationContext) (int, probeResult, command.AuditRecord, error) {
	return func(command.MutationContext) (int, probeResult, command.AuditRecord, error) {
		return 201, probeResult{Value: value}, command.AuditRecord{}, nil
	}
}

func TestMutationDenialAttributionFollowsPolicy(t *testing.T) {
	ctx := context.Background()

	for _, tc := range []struct {
		name      string
		policy    command.Policy
		wantActor bool
	}{
		{"attribute only when confirmed", strictPolicy, false},
		{"attribute always", lenientPolicy, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner, q := newRunner(t, tc.policy)
			manager := newIdentity(t, q, auth.RoleManager)
			unknownSession := command.Actor{StaffID: manager.StaffID, SessionID: uuid.New()}
			op := newOperation()

			_, _, err := command.ExecuteMutation(ctx, runner, unknownSession, command.MutationSpec{
				RequestID: uuid.New(), Operation: op, Fingerprint: op, Required: []string{capAdminister},
			}, mustNotRun[probeResult](t))
			require.ErrorIs(t, err, command.ErrUnauthorized)
			assert.EqualError(t, err, "unauthorized: session not found or identity mismatch")

			rows := auditRows(t, op)
			require.Len(t, rows, 1, "the denial evidence must be committed")
			assert.Equal(t, probeDenialEvent, rows[0].EventType)
			assert.Equal(t, tc.wantActor, rows[0].ActorID.Valid)
			assert.False(t, rows[0].SessionID.Valid, "an unknown session is never attributed")
		})
	}

	t.Run("capability denial names actor and session", func(t *testing.T) {
		runner, q := newRunner(t, strictPolicy)
		barista := newIdentity(t, q, auth.RoleBarista)
		op := newOperation()

		_, _, err := command.ExecuteMutation(ctx, runner, barista.Actor, command.MutationSpec{
			RequestID: uuid.New(), Operation: op, Fingerprint: op, Required: []string{capAdminister},
		}, mustNotRun[probeResult](t))
		require.ErrorIs(t, err, command.ErrForbidden)

		rows := auditRows(t, op)
		require.Len(t, rows, 1)
		assert.Equal(t, uuid.NullUUID{UUID: barista.StaffID, Valid: true}, rows[0].ActorID)
		assert.Equal(t, uuid.NullUUID{UUID: barista.SessionID, Valid: true}, rows[0].SessionID)
		assert.Equal(t, `forbidden: missing capability "tables.administer"`, rows[0].Reason)
	})
}

func TestMutationDenialAuditFailureFollowsPolicy(t *testing.T) {
	ctx := context.Background()
	// An identity that does not exist fails the audit row's actor foreign key
	// whenever the policy attributes the actor unconditionally.
	ghost := command.Actor{StaffID: uuid.New(), SessionID: uuid.New()}
	spec := func(op string) command.MutationSpec {
		return command.MutationSpec{RequestID: uuid.New(), Operation: op, Fingerprint: op}
	}

	t.Run("return the denial", func(t *testing.T) {
		runner, _ := newRunner(t, lenientPolicy)
		op := newOperation()
		_, _, err := command.ExecuteMutation(ctx, runner, ghost, spec(op), mustNotRun[probeResult](t))
		require.ErrorIs(t, err, command.ErrUnauthorized)
		assert.Empty(t, auditRows(t, op))
	})

	t.Run("return the audit error", func(t *testing.T) {
		policy := strictPolicy
		policy.Attribution = command.AttributeActorAlways
		runner, _ := newRunner(t, policy)
		op := newOperation()
		_, _, err := command.ExecuteMutation(ctx, runner, ghost, spec(op), mustNotRun[probeResult](t))
		require.ErrorContains(t, err, "insert denial audit event")
		assert.NotErrorIs(t, err, command.ErrUnauthorized)
		assert.Empty(t, auditRows(t, op))
	})
}

func TestReadDenialAuditFollowsPolicy(t *testing.T) {
	ctx := context.Background()

	for _, tc := range []struct {
		name     string
		policy   command.Policy
		wantRows int
	}{
		{"audited", lenientPolicy, 1},
		{"not audited", strictPolicy, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner, q := newRunner(t, tc.policy)
			barista := newIdentity(t, q, auth.RoleBarista)
			op := newOperation()

			_, err := command.ExecuteRead(ctx, runner, barista.Actor,
				command.ReadSpec{Operation: op, Required: []string{capAdminister}},
				func(command.ReadContext) (int, error) {
					t.Fatal("read body must not run")
					return 0, nil
				})
			require.ErrorIs(t, err, command.ErrForbidden)
			rows := auditRows(t, op)
			require.Len(t, rows, tc.wantRows)
			if tc.wantRows > 0 {
				assert.Equal(t, probeDenialEvent, rows[0].EventType)
				assert.Equal(t, barista.SessionID, rows[0].SessionID.UUID)
			}
		})
	}

	t.Run("body sees current capabilities", func(t *testing.T) {
		runner, q := newRunner(t, strictPolicy)
		cashier := newIdentity(t, q, auth.RoleCashier)
		caps, err := command.ExecuteRead(ctx, runner, cashier.Actor,
			command.ReadSpec{Required: []string{"sales.operate"}},
			func(rc command.ReadContext) ([]string, error) {
				assert.Equal(t, cashier.Actor, rc.Actor)
				return rc.Capabilities, nil
			})
		require.NoError(t, err)
		assert.Equal(t, auth.DeriveCapabilities([]string{auth.RoleCashier}), caps)
	})
}

func TestMutationGate(t *testing.T) {
	ctx := context.Background()
	runner, q := newRunner(t, lenientPolicy)
	manager := newIdentity(t, q, auth.RoleManager)

	t.Run("security denial is recorded and committed", func(t *testing.T) {
		op := newOperation()
		_, _, err := command.ExecuteMutation(ctx, runner, manager.Actor, command.MutationSpec{
			RequestID: uuid.New(), Operation: op, Fingerprint: op,
			Gate: func(context.Context, command.GateContext) error {
				return fmt.Errorf("%w: pin rejected", errProbeWrongPIN)
			},
		}, mustNotRun[probeResult](t))
		require.ErrorIs(t, err, errProbeWrongPIN)
		rows := auditRows(t, op)
		require.Len(t, rows, 1)
		assert.Equal(t, "probe wrong pin: pin rejected", rows[0].Reason)
	})

	t.Run("other errors abort without evidence", func(t *testing.T) {
		op := newOperation()
		boom := errors.New("lock staff identity: boom")
		_, _, err := command.ExecuteMutation(ctx, runner, manager.Actor, command.MutationSpec{
			RequestID: uuid.New(), Operation: op, Fingerprint: op,
			Gate: func(context.Context, command.GateContext) error { return boom },
		}, mustNotRun[probeResult](t))
		require.ErrorIs(t, err, boom)
		assert.Empty(t, auditRows(t, op))
	})

	t.Run("gate sees the reloaded authority", func(t *testing.T) {
		op := newOperation()
		_, _, err := command.ExecuteMutation(ctx, runner, manager.Actor, command.MutationSpec{
			RequestID: uuid.New(), Operation: op, Fingerprint: op, Required: []string{capAdminister},
			Gate: func(_ context.Context, gc command.GateContext) error {
				assert.NotNil(t, gc.Queries)
				assert.Equal(t, manager.Actor, gc.Actor)
				assert.Equal(t, manager.StaffID, gc.Authority.StaffIdentityID)
				assert.Equal(t, manager.LoginCode, gc.Authority.LoginCode)
				assert.Contains(t, gc.Capabilities, capAdminister)
				assert.Equal(t, []string{capAdminister}, gc.Required)
				return nil
			},
		}, okBody("gated"))
		require.NoError(t, err)
	})

	t.Run("a failing gate denies an exact replay", func(t *testing.T) {
		op := newOperation()
		spec := command.MutationSpec{RequestID: uuid.New(), Operation: op, Fingerprint: op}
		_, _, err := command.ExecuteMutation(ctx, runner, manager.Actor, spec, okBody("first"))
		require.NoError(t, err)

		spec.Gate = func(context.Context, command.GateContext) error {
			return fmt.Errorf("%w: rotated", errProbeWrongPIN)
		}
		_, _, err = command.ExecuteMutation(ctx, runner, manager.Actor, spec, mustNotRun[probeResult](t))
		require.ErrorIs(t, err, errProbeWrongPIN)
	})
}

func TestMutationApproval(t *testing.T) {
	ctx := context.Background()
	errUnavailable := errors.New("manager approval unavailable")
	policy := lenientPolicy
	policy.ApprovalDenied = errUnavailable
	runner, q := newRunner(t, policy)
	cashier := newIdentity(t, q, auth.RoleCashier)
	approver := newIdentity(t, q, auth.RoleManager)

	t.Run("rejected approval maps to the policy sentinel", func(t *testing.T) {
		op := newOperation()
		_, _, err := command.ExecuteMutation(ctx, runner, cashier.Actor, command.MutationSpec{
			RequestID: uuid.New(), Operation: op, Fingerprint: op,
			Approval: &command.Approval{
				ApproverLoginCode: approver.LoginCode, ManagerPIN: "0000", RequiredCapability: capAdminister,
			},
		}, mustNotRun[probeResult](t))
		require.ErrorIs(t, err, errUnavailable)
		assert.NotErrorIs(t, err, auth.ErrManagerApprovalDenied, "only the slice's sentinel is mapped")
		assert.EqualError(t, err, "manager approval unavailable: manager approval denied: "+auth.ApprovalDenialInvalidPin)

		rows := auditRows(t, op)
		require.Len(t, rows, 1)
		assert.Equal(t, cashier.StaffID, rows[0].ActorID.UUID, "the initiator, not the approver, is attributed")
	})

	t.Run("default sentinel is ErrForbidden", func(t *testing.T) {
		runner, _ := newRunner(t, lenientPolicy)
		op := newOperation()
		_, _, err := command.ExecuteMutation(ctx, runner, cashier.Actor, command.MutationSpec{
			RequestID: uuid.New(), Operation: op, Fingerprint: op,
			Approval: &command.Approval{ApproverLoginCode: approver.LoginCode, ManagerPIN: "0000"},
		}, mustNotRun[probeResult](t))
		require.ErrorIs(t, err, command.ErrForbidden)
	})

	t.Run("approved body receives the approver", func(t *testing.T) {
		op := newOperation()
		_, res, err := command.ExecuteMutation(ctx, runner, cashier.Actor, command.MutationSpec{
			RequestID: uuid.New(), Operation: op, Fingerprint: op,
			Approval: &command.Approval{
				ApproverLoginCode: strings.ToLower(approver.LoginCode), ManagerPIN: testManagerPIN,
				RequiredCapability: capAdminister,
			},
		}, func(mc command.MutationContext) (int, probeResult, command.AuditRecord, error) {
			require.NotNil(t, mc.Approver)
			return 200, probeResult{Value: mc.Approver.ID.String()}, command.AuditRecord{}, nil
		})
		require.NoError(t, err)
		assert.Equal(t, approver.StaffID.String(), res.Value)
	})
}

func TestMutationBodyContextAndAudit(t *testing.T) {
	ctx := context.Background()
	runner, q := newRunner(t, strictPolicy)
	manager := newIdentity(t, q, auth.RoleManager)
	op := newOperation()

	code, res, err := command.ExecuteMutation(ctx, runner, manager.Actor, command.MutationSpec{
		RequestID: uuid.New(), Operation: op, Fingerprint: op,
	}, func(mc command.MutationContext) (int, probeResult, command.AuditRecord, error) {
		assert.Equal(t, manager.Actor, mc.Actor)
		assert.Nil(t, mc.Approver)
		// The transaction is usable for statements outside the generated
		// queries, such as savepoints.
		if _, err := mc.Tx.ExecContext(ctx, "SAVEPOINT probe_unit"); err != nil {
			return 0, probeResult{}, command.AuditRecord{}, err
		}
		if _, err := mc.Tx.ExecContext(ctx, "RELEASE SAVEPOINT probe_unit"); err != nil {
			return 0, probeResult{}, command.AuditRecord{}, err
		}
		return 201, probeResult{Value: "done"}, command.AuditRecord{
			EventType: probeEvent,
			Details:   map[string]string{"probe_operation": op},
		}, nil
	})
	require.NoError(t, err)
	assert.Equal(t, 201, code)
	assert.Equal(t, "done", res.Value)

	rows := auditRows(t, op)
	require.Len(t, rows, 1)
	assert.Equal(t, probeEvent, rows[0].EventType)
	assert.Equal(t, uuid.NullUUID{UUID: manager.StaffID, Valid: true}, rows[0].ActorID)
	assert.Equal(t, uuid.NullUUID{UUID: manager.SessionID, Valid: true}, rows[0].SessionID)
}

func TestMutationIdempotency(t *testing.T) {
	ctx := context.Background()
	runner, q := newRunner(t, strictPolicy)
	manager := newIdentity(t, q, auth.RoleManager)

	t.Run("replay returns the stored result once", func(t *testing.T) {
		op := newOperation()
		spec := command.MutationSpec{RequestID: uuid.New(), Operation: op, Fingerprint: op}
		var runs atomic.Int32
		body := func(command.MutationContext) (int, probeResult, command.AuditRecord, error) {
			runs.Add(1)
			return 201, probeResult{Value: "first"}, command.AuditRecord{}, nil
		}

		var wg sync.WaitGroup
		for range 4 {
			wg.Go(func() {
				code, res, err := command.ExecuteMutation(ctx, runner, manager.Actor, spec, body)
				assert.NoError(t, err)
				assert.Equal(t, 201, code)
				assert.Equal(t, "first", res.Value)
			})
		}
		wg.Wait()
		assert.Equal(t, int32(1), runs.Load())
	})

	t.Run("reuse for a different change conflicts", func(t *testing.T) {
		op := newOperation()
		requestID := uuid.New()
		_, _, err := command.ExecuteMutation(ctx, runner, manager.Actor, command.MutationSpec{
			RequestID: requestID, Operation: op, Fingerprint: "a",
		}, okBody("a"))
		require.NoError(t, err)

		_, _, err = command.ExecuteMutation(ctx, runner, manager.Actor, command.MutationSpec{
			RequestID: requestID, Operation: op, Fingerprint: "b",
		}, mustNotRun[probeResult](t))
		require.ErrorIs(t, err, command.ErrRequestConflict)
		assert.EqualError(t, err, "request conflict: request_id reused for a different change")
	})

	t.Run("corrupted stored result", func(t *testing.T) {
		op := newOperation()
		spec := command.MutationSpec{RequestID: uuid.New(), Operation: op, Fingerprint: op}
		_, _, err := command.ExecuteMutation(ctx, runner, manager.Actor, spec, okBody("x"))
		require.NoError(t, err)
		_, err = commandTestDB.Exec(`UPDATE idempotency_keys SET response_body = '"not an object"' WHERE actor_id = $1 AND key = $2`,
			manager.StaffID, spec.RequestID)
		require.NoError(t, err)

		_, _, err = command.ExecuteMutation(ctx, runner, manager.Actor, spec, mustNotRun[probeResult](t))
		require.ErrorIs(t, err, command.ErrInvalidStoredResult)
	})
}

// memoryStore is an IdempotencyStore outside the database, proving the
// pipeline depends only on the interface.
type memoryStore struct {
	mu   sync.Mutex
	recs map[[2]uuid.UUID]command.StoredRequest
}

func (s *memoryStore) Find(_ context.Context, _ *sqlc.Queries, actorID, requestID uuid.UUID) (command.StoredRequest, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.recs[[2]uuid.UUID{actorID, requestID}]
	return rec, ok, nil
}

func (s *memoryStore) Claim(_ context.Context, _ *sqlc.Queries, actorID, requestID uuid.UUID,
	operation, requestHash string,
) (command.StoredRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := [2]uuid.UUID{actorID, requestID}
	if rec, ok := s.recs[key]; ok {
		return rec, nil
	}
	rec := command.StoredRequest{Operation: operation, RequestHash: requestHash, ResponseBody: json.RawMessage("null")}
	s.recs[key] = rec
	return rec, nil
}

func (s *memoryStore) Complete(_ context.Context, _ *sqlc.Queries, actorID, requestID uuid.UUID,
	responseCode int, responseBody []byte,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := [2]uuid.UUID{actorID, requestID}
	rec := s.recs[key]
	rec.ResponseCode, rec.ResponseBody = responseCode, responseBody
	s.recs[key] = rec
	return nil
}

func (s *memoryStore) DescribeConflict(stored command.StoredRequest, _, requestHash string) string {
	return fmt.Sprintf("operation %q hash mismatch (stored: %s, current: %s)", stored.Operation, stored.RequestHash, requestHash)
}

func TestMutationUsesPolicyIdempotencyStore(t *testing.T) {
	ctx := context.Background()
	store := &memoryStore{recs: map[[2]uuid.UUID]command.StoredRequest{}}
	policy := strictPolicy
	policy.Idempotency = store
	runner, q := newRunner(t, policy)
	manager := newIdentity(t, q, auth.RoleManager)
	op := newOperation()
	requestID := uuid.New()

	code, _, err := command.ExecuteMutation(ctx, runner, manager.Actor, command.MutationSpec{
		RequestID: requestID, Operation: op, Fingerprint: "a",
	}, okBody("stored"))
	require.NoError(t, err)
	assert.Equal(t, 201, code)

	var inTable int
	require.NoError(t, commandTestDB.QueryRow(
		`SELECT count(*) FROM idempotency_keys WHERE actor_id = $1 AND key = $2`,
		manager.StaffID, requestID).Scan(&inTable))
	assert.Zero(t, inTable, "a custom store replaces idempotency_keys")

	code, res, err := command.ExecuteMutation(ctx, runner, manager.Actor, command.MutationSpec{
		RequestID: requestID, Operation: op, Fingerprint: "a",
	}, mustNotRun[probeResult](t))
	require.NoError(t, err)
	assert.Equal(t, 201, code)
	assert.Equal(t, "stored", res.Value)

	_, _, err = command.ExecuteMutation(ctx, runner, manager.Actor, command.MutationSpec{
		RequestID: requestID, Operation: op, Fingerprint: "b",
	}, mustNotRun[probeResult](t))
	require.ErrorIs(t, err, command.ErrRequestConflict)
	hashA, _ := command.FingerprintHash("a")
	hashB, _ := command.FingerprintHash("b")
	assert.EqualError(t, err, fmt.Sprintf("request conflict: operation %q hash mismatch (stored: %s, current: %s)", op, hashA, hashB))
}

func TestAdvisoryLockSerializesTransactions(t *testing.T) {
	ctx := context.Background()
	runner, q := newRunner(t, strictPolicy)
	key := command.IDToLockKey(uuid.New())

	first, err := commandTestDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer first.Rollback() //nolint:errcheck
	require.NoError(t, runner.AdvisoryLock(ctx, q.WithTx(first), key))

	second, err := commandTestDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer second.Rollback() //nolint:errcheck
	var acquired bool
	require.NoError(t, second.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock($1)`, key).Scan(&acquired))
	assert.False(t, acquired, "the lock is held until the first transaction ends")

	require.NoError(t, first.Rollback())
	require.NoError(t, second.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock($1)`, key).Scan(&acquired))
	assert.True(t, acquired)
}
