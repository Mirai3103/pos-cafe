//go:build integration

package tables_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/database"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/tables"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openTablesTestDB(t *testing.T) (*sql.DB, *sqlc.Queries) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	require.Contains(t, url, "_test")
	db, err := database.Open(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db, sqlc.New(db)
}

type testActor struct {
	StaffID   uuid.UUID
	SessionID uuid.UUID
}

func (a testActor) actor() tables.Actor {
	return tables.Actor{StaffID: a.StaffID, SessionID: a.SessionID}
}

// testLoginCode generates a high-entropy unique login code for a test identity.
// The staff_identities.login_code column is VARCHAR(24); the prefix shares that budget.
// Mirrors internal/catalog/executor_integration_test.go: identities are never
// deleted between runs, so per-process counters would collide across runs.
func testLoginCode(prefix string) string {
	room := 24 - len(prefix)
	if room < 8 {
		panic("testLoginCode: prefix leaves too little entropy budget")
	}
	return prefix + strings.ReplaceAll(uuid.NewString(), "-", "")[:room]
}

func newTestActor(t *testing.T, q *sqlc.Queries, roles []string, enabled bool) testActor {
	t.Helper()
	ctx := context.Background()

	loginCode := testLoginCode("T")
	row, err := q.CreateStaffIdentity(ctx, sqlc.CreateStaffIdentityParams{
		DisplayName: "Tables Test " + loginCode,
		Btrim:       loginCode,
		PinHash:     "",
		Enabled:     enabled,
	})
	require.NoError(t, err)

	for _, role := range roles {
		require.NoError(t, q.AddStaffRole(ctx, sqlc.AddStaffRoleParams{
			StaffIdentityID: row.ID,
			Role:            role,
		}))
	}

	session, err := q.CreateStaffSession(ctx, sqlc.CreateStaffSessionParams{
		TokenHash:           "tok_" + uuid.NewString()[:16],
		StaffIdentityID:     row.ID,
		State:               auth.SessionStateActive,
		LastHumanActivityAt: time.Now(),
		ExpiresAt:           time.Now().Add(8 * time.Hour),
	})
	require.NoError(t, err)

	return testActor{StaffID: row.ID, SessionID: session.ID}
}

type probeFingerprint struct {
	Name string `json:"name"`
}

type probeResult struct {
	Value string `json:"value"`
}

func TestExecuteMutationRequiresCapability(t *testing.T) {
	db, q := openTablesTestDB(t)
	runner := tables.NewRunner(db, q)
	ctx := context.Background()

	// BARISTA holds neither sales.operate nor tables.administer.
	barista := newTestActor(t, q, []string{"BARISTA"}, true)

	_, _, err := tables.ExecuteMutation(ctx, runner, barista.actor(), tables.MutationSpec{
		RequestID:   uuid.New(),
		Operation:   tables.OpCreateTable,
		Fingerprint: probeFingerprint{Name: "probe"},
		Required:    []string{tables.CapTablesAdminister},
	}, func(*sqlc.Queries) (int, probeResult, tables.AuditRecord, error) {
		t.Fatal("mutation body must not run for an unauthorized actor")
		return 0, probeResult{}, tables.AuditRecord{}, nil
	})
	require.ErrorIs(t, err, tables.ErrForbidden)
}

func TestExecuteMutationReplaysExactRequest(t *testing.T) {
	db, q := openTablesTestDB(t)
	runner := tables.NewRunner(db, q)
	ctx := context.Background()

	manager := newTestActor(t, q, []string{"MANAGER"}, true)
	requestID := uuid.New()
	spec := tables.MutationSpec{
		RequestID:   requestID,
		Operation:   tables.OpCreateTable,
		Fingerprint: probeFingerprint{Name: "replay probe"},
		Required:    []string{tables.CapTablesAdminister},
	}

	var runs atomic.Int32
	body := func(*sqlc.Queries) (int, probeResult, tables.AuditRecord, error) {
		runs.Add(1)
		return 201, probeResult{Value: "first"}, tables.AuditRecord{}, nil
	}

	code, first, err := tables.ExecuteMutation(ctx, runner, manager.actor(), spec, body)
	require.NoError(t, err)
	assert.Equal(t, 201, code)
	assert.Equal(t, "first", first.Value)

	code, second, err := tables.ExecuteMutation(ctx, runner, manager.actor(), spec, body)
	require.NoError(t, err)
	assert.Equal(t, 201, code)
	assert.Equal(t, "first", second.Value, "replay must return the stored result")
	assert.Equal(t, int32(1), runs.Load(), "mutation body must run exactly once")
}

func TestExecuteMutationRejectsConflictingReuse(t *testing.T) {
	db, q := openTablesTestDB(t)
	runner := tables.NewRunner(db, q)
	ctx := context.Background()

	manager := newTestActor(t, q, []string{"MANAGER"}, true)
	requestID := uuid.New()

	body := func(*sqlc.Queries) (int, probeResult, tables.AuditRecord, error) {
		return 201, probeResult{Value: "ok"}, tables.AuditRecord{}, nil
	}

	_, _, err := tables.ExecuteMutation(ctx, runner, manager.actor(), tables.MutationSpec{
		RequestID:   requestID,
		Operation:   tables.OpCreateTable,
		Fingerprint: probeFingerprint{Name: "original"},
		Required:    []string{tables.CapTablesAdminister},
	}, body)
	require.NoError(t, err)

	_, _, err = tables.ExecuteMutation(ctx, runner, manager.actor(), tables.MutationSpec{
		RequestID:   requestID,
		Operation:   tables.OpCreateTable,
		Fingerprint: probeFingerprint{Name: "different"},
		Required:    []string{tables.CapTablesAdminister},
	}, body)
	require.ErrorIs(t, err, tables.ErrRequestConflict)
}

func TestExecuteMutationRunsOnceUnderConcurrency(t *testing.T) {
	db, q := openTablesTestDB(t)
	runner := tables.NewRunner(db, q)
	ctx := context.Background()

	manager := newTestActor(t, q, []string{"MANAGER"}, true)
	spec := tables.MutationSpec{
		RequestID:   uuid.New(),
		Operation:   tables.OpCreateTable,
		Fingerprint: probeFingerprint{Name: "concurrent"},
		Required:    []string{tables.CapTablesAdminister},
	}

	var runs atomic.Int32
	var start sync.WaitGroup
	var done sync.WaitGroup
	start.Add(1)

	const goroutines = 4
	errs := make([]error, goroutines)
	for i := range goroutines {
		done.Add(1)
		go func() {
			defer done.Done()
			start.Wait()
			_, _, errs[i] = tables.ExecuteMutation(ctx, runner, manager.actor(), spec,
				func(*sqlc.Queries) (int, probeResult, tables.AuditRecord, error) {
					runs.Add(1)
					return 201, probeResult{Value: "once"}, tables.AuditRecord{}, nil
				})
		}()
	}
	start.Done()
	done.Wait()

	for _, err := range errs {
		require.NoError(t, err)
	}
	assert.Equal(t, int32(1), runs.Load(), "concurrent duplicates must execute the body once")
}

func TestExecuteMutationDoesNotCacheFailures(t *testing.T) {
	db, q := openTablesTestDB(t)
	runner := tables.NewRunner(db, q)
	ctx := context.Background()

	manager := newTestActor(t, q, []string{"MANAGER"}, true)
	spec := tables.MutationSpec{
		RequestID:   uuid.New(),
		Operation:   tables.OpCreateTable,
		Fingerprint: probeFingerprint{Name: "fails then succeeds"},
		Required:    []string{tables.CapTablesAdminister},
	}

	boom := fmt.Errorf("%w: deliberate", tables.ErrTableNotFound)
	_, _, err := tables.ExecuteMutation(ctx, runner, manager.actor(), spec,
		func(*sqlc.Queries) (int, probeResult, tables.AuditRecord, error) {
			return 0, probeResult{}, tables.AuditRecord{}, boom
		})
	require.ErrorIs(t, err, tables.ErrTableNotFound)

	// The failed claim rolled back, so the same request_id is reusable.
	code, res, err := tables.ExecuteMutation(ctx, runner, manager.actor(), spec,
		func(*sqlc.Queries) (int, probeResult, tables.AuditRecord, error) {
			return 201, probeResult{Value: "retried"}, tables.AuditRecord{}, nil
		})
	require.NoError(t, err)
	assert.Equal(t, 201, code)
	assert.Equal(t, "retried", res.Value)
}

func TestExecuteReadRequiresCapability(t *testing.T) {
	db, q := openTablesTestDB(t)
	runner := tables.NewRunner(db, q)
	ctx := context.Background()

	barista := newTestActor(t, q, []string{"BARISTA"}, true)
	_, err := tables.ExecuteRead(ctx, runner, barista.actor(), tables.CapSalesOperate,
		func(*sqlc.Queries) (int, error) {
			t.Fatal("read body must not run without sales.operate")
			return 0, nil
		})
	require.ErrorIs(t, err, tables.ErrForbidden)

	cashier := newTestActor(t, q, []string{"CASHIER"}, true)
	got, err := tables.ExecuteRead(ctx, runner, cashier.actor(), tables.CapSalesOperate,
		func(*sqlc.Queries) (int, error) { return 7, nil })
	require.NoError(t, err)
	assert.Equal(t, 7, got)
}

func TestExecuteMutationRollsBackOnAuditFailure(t *testing.T) {
	db, q := openTablesTestDB(t)
	runner := tables.NewRunner(db, q)
	ctx := context.Background()

	manager := newTestActor(t, q, []string{"MANAGER"}, true)
	requestID := uuid.New()
	spec := tables.MutationSpec{
		RequestID:   requestID,
		Operation:   tables.OpCreateTable,
		Fingerprint: probeFingerprint{Name: "audit failure"},
		Required:    []string{tables.CapTablesAdminister},
	}

	// A channel cannot be JSON-encoded, so writing the audit event fails after
	// the mutation body has already run.
	_, _, err := tables.ExecuteMutation(ctx, runner, manager.actor(), spec,
		func(*sqlc.Queries) (int, probeResult, tables.AuditRecord, error) {
			return 201, probeResult{Value: "doomed"}, tables.AuditRecord{
				EventType: tables.EventTableCreated,
				Details:   make(chan int),
			}, nil
		})
	require.Error(t, err, "an unwritable audit event must fail the operation")

	// The whole transaction rolled back, so the idempotency claim is gone too.
	var claims int
	require.NoError(t, db.QueryRow(
		`SELECT count(*) FROM idempotency_keys WHERE actor_id = $1 AND key = $2`,
		manager.StaffID, requestID).Scan(&claims))
	assert.Equal(t, 0, claims, "a failed audit write must roll back the idempotency claim")
}

func TestExecuteReadUsesReadOnlyTransaction(t *testing.T) {
	db, q := openTablesTestDB(t)
	runner := tables.NewRunner(db, q)
	ctx := context.Background()

	manager := newTestActor(t, q, []string{"MANAGER"}, true)

	// A write attempted inside the read transaction must be refused, proving
	// the read runs read-only rather than in a default read-write transaction.
	_, err := tables.ExecuteRead(ctx, runner, manager.actor(), tables.CapSalesOperate,
		func(q *sqlc.Queries) (int, error) {
			_, err := q.CreateTable(ctx, sqlc.CreateTableParams{
				Name:           "Ban readonly probe",
				NormalizedName: "ban readonly probe",
			})
			return 0, err
		})
	require.Error(t, err, "a write inside ExecuteRead must be refused")
}

func TestExecuteMutationDeniesDisabledIdentity(t *testing.T) {
	db, q := openTablesTestDB(t)
	runner := tables.NewRunner(db, q)
	ctx := context.Background()

	manager := newTestActor(t, q, []string{"MANAGER"}, true)
	// SetStaffEnabled is a :one query, so it returns (row, error).
	_, err := q.SetStaffEnabled(ctx, sqlc.SetStaffEnabledParams{
		ID:      manager.StaffID,
		Enabled: false,
	})
	require.NoError(t, err)

	_, _, err = tables.ExecuteMutation(ctx, runner, manager.actor(), tables.MutationSpec{
		RequestID:   uuid.New(),
		Operation:   tables.OpCreateTable,
		Fingerprint: probeFingerprint{Name: "disabled"},
		Required:    []string{tables.CapTablesAdminister},
	}, func(*sqlc.Queries) (int, probeResult, tables.AuditRecord, error) {
		t.Fatal("mutation body must not run for a disabled identity")
		return 0, probeResult{}, tables.AuditRecord{}, nil
	})
	require.ErrorIs(t, err, tables.ErrForbidden)
}
