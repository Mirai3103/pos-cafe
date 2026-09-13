//go:build integration

package sales_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// probeResult is the trivial payload the executor tests round-trip through
// idempotency storage.
type probeResult struct {
	Value string `json:"value"`
}

type probeFingerprint struct {
	Value string `json:"value"`
}

// runProbe executes a minimal mutation through the real executor.
func runProbe(t *testing.T, runner *sales.Runner, actor sales.Actor,
	requestID uuid.UUID, value string, calls *atomic.Int64,
) (int, probeResult, error) {
	t.Helper()
	return sales.ExecuteMutation(context.Background(), runner, actor, sales.MutationSpec{
		RequestID:   requestID,
		Operation:   sales.OpStartTakeawaySession,
		Fingerprint: probeFingerprint{Value: value},
		Required:    []string{sales.CapSalesOperate},
	}, func(mc sales.MutationContext) (int, probeResult, sales.AuditRecord, error) {
		calls.Add(1)
		return 201, probeResult{Value: value}, sales.AuditRecord{
			EventType: sales.EventServiceSessionStarted,
			Details:   map[string]string{"value": value},
		}, nil
	})
}

func TestExecutorReplayReturnsStoredResult(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	requestID := uuid.New()
	var calls atomic.Int64

	status, first, err := runProbe(t, runner, actor, requestID, "one", &calls)
	require.NoError(t, err)
	assert.Equal(t, 201, status)
	assert.Equal(t, int64(1), calls.Load())

	status, second, err := runProbe(t, runner, actor, requestID, "one", &calls)
	require.NoError(t, err)
	assert.Equal(t, 201, status)
	assert.Equal(t, first, second)
	assert.Equal(t, int64(1), calls.Load(), "a replay must not run the mutation body again")
}

func TestExecutorRejectsReusedRequestIDWithDifferentPayload(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	requestID := uuid.New()
	var calls atomic.Int64

	_, _, err := runProbe(t, runner, actor, requestID, "one", &calls)
	require.NoError(t, err)

	_, _, err = runProbe(t, runner, actor, requestID, "two", &calls)
	require.ErrorIs(t, err, sales.ErrRequestConflict)
	assert.Equal(t, int64(1), calls.Load())
}

func TestExecutorDeniesMissingCapability(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"BARISTA"})
	var calls atomic.Int64

	_, _, err := runProbe(t, runner, actor, uuid.New(), "one", &calls)
	require.ErrorIs(t, err, sales.ErrForbidden)
	assert.Equal(t, int64(0), calls.Load())

	assertAuditEvent(t, db, sales.EventAuthorizationDenied, 1)
}

// Authority is reloaded inside the transaction and before the idempotency
// replay, so a revoked session cannot replay an earlier success.
func TestExecutorDeniesReplayAfterSessionRevoked(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	requestID := uuid.New()
	var calls atomic.Int64

	_, _, err := runProbe(t, runner, actor, requestID, "one", &calls)
	require.NoError(t, err)

	_, err = db.Exec(`UPDATE staff_access_sessions SET revoked_at = now() WHERE id = $1`,
		actor.SessionID)
	require.NoError(t, err)

	_, _, err = runProbe(t, runner, actor, requestID, "one", &calls)
	require.ErrorIs(t, err, sales.ErrUnauthorized)
}

func TestExecutorDeniesReplayAfterIdentityDisabled(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	requestID := uuid.New()
	var calls atomic.Int64

	_, _, err := runProbe(t, runner, actor, requestID, "one", &calls)
	require.NoError(t, err)

	_, err = db.Exec(`UPDATE staff_identities SET enabled = false WHERE id = $1`, actor.StaffID)
	require.NoError(t, err)

	_, _, err = runProbe(t, runner, actor, requestID, "one", &calls)
	require.ErrorIs(t, err, sales.ErrForbidden)
}

// Concurrent duplicates run the mutation exactly once.
func TestExecutorConcurrentDuplicatesRunOnce(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	requestID := uuid.New()
	var calls atomic.Int64

	start := make(chan struct{})
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			_, _, err := runProbe(t, runner, actor, requestID, "one", &calls)
			errs <- err
		}()
	}
	close(start)
	require.NoError(t, <-errs)
	require.NoError(t, <-errs)
	assert.Equal(t, int64(1), calls.Load())
}

// A business audit event is written for a success and none for a replay.
func TestExecutorAuditsOnceAcrossReplay(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	requestID := uuid.New()
	var calls atomic.Int64

	_, _, err := runProbe(t, runner, actor, requestID, "one", &calls)
	require.NoError(t, err)
	assertAuditEvent(t, db, sales.EventServiceSessionStarted, 1)

	_, _, err = runProbe(t, runner, actor, requestID, "one", &calls)
	require.NoError(t, err)
	assertAuditEvent(t, db, sales.EventServiceSessionStarted, 1)
}

// An audit failure must roll back the business mutation AND the idempotency
// claim, so the request can be retried rather than being permanently stuck
// replaying a result that was never committed.
func TestExecutorAuditFailureRollsBackTheClaim(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	requestID := uuid.New()

	// A channel cannot be marshalled, so the audit insert step fails after the
	// claim and after the mutation body ran.
	_, _, err := sales.ExecuteMutation(context.Background(), runner, actor, sales.MutationSpec{
		RequestID:   requestID,
		Operation:   sales.OpStartTakeawaySession,
		Fingerprint: probeFingerprint{Value: "one"},
		Required:    []string{sales.CapSalesOperate},
	}, func(mc sales.MutationContext) (int, probeResult, sales.AuditRecord, error) {
		return 201, probeResult{Value: "one"}, sales.AuditRecord{
			EventType: sales.EventServiceSessionStarted,
			Details:   make(chan int),
		}, nil
	})
	require.Error(t, err)

	var claims int
	require.NoError(t, db.QueryRow(
		`SELECT count(*) FROM idempotency_keys WHERE key = $1`, requestID).Scan(&claims))
	assert.Equal(t, 0, claims, "the idempotency claim must roll back with the mutation")

	assertAuditEvent(t, db, sales.EventServiceSessionStarted, 0)

	// The same request_id is therefore reusable.
	var calls atomic.Int64
	_, _, err = runProbe(t, runner, actor, requestID, "one", &calls)
	require.NoError(t, err)
	assert.Equal(t, int64(1), calls.Load())
}
