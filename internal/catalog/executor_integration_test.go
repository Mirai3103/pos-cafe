//go:build integration

package catalog_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/catalog"
	"github.com/Mirai3103/pos-cafe/internal/database"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openExecutorTestDB(t *testing.T) (*sql.DB, *sqlc.Queries) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	require.Contains(t, url, "_test")
	db, err := database.Open(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db, sqlc.New(db)
}

type testIdentity struct {
	StaffID  uuid.UUID
	SessionID uuid.UUID
}

func createTestIdentity(t *testing.T, db *sql.DB, q *sqlc.Queries, roles []string, enabled bool) testIdentity {
	t.Helper()
	ctx := context.Background()

	loginCode := fmt.Sprintf("test%s", uuid.New().String()[:6])
	row, err := q.CreateStaffIdentity(ctx, sqlc.CreateStaffIdentityParams{
		DisplayName: fmt.Sprintf("Test Staff %s", loginCode),
		Btrim:       loginCode,
		PinHash:     "",
		Enabled:     enabled,
	})
	require.NoError(t, err)
	staffID := row.ID

	for _, role := range roles {
		err = q.AddStaffRole(ctx, sqlc.AddStaffRoleParams{
			StaffIdentityID: staffID,
			Role:            role,
		})
		require.NoError(t, err)
	}

	sessionID := uuid.New()
	_, err = q.CreateStaffSession(ctx, sqlc.CreateStaffSessionParams{
		TokenHash:           fmt.Sprintf("tok_%s", sessionID.String()[:8]),
		StaffIdentityID:     staffID,
		State:               auth.SessionStateActive,
		ActiveWorkspace:     sql.NullString{String: auth.WorkspaceCashier, Valid: true},
		LastAuthenticatedAt: time.Now(),
		LastHumanActivityAt: time.Now(),
		ExpiresAt:           time.Now().Add(12 * time.Hour),
	})
	require.NoError(t, err)

	// Retrieve the session ID that was auto-generated
	var actualSessionID uuid.UUID
	err = db.QueryRow(`SELECT id FROM staff_access_sessions WHERE token_hash = $1`,
		fmt.Sprintf("tok_%s", sessionID.String()[:8])).Scan(&actualSessionID)
	require.NoError(t, err)

	return testIdentity{StaffID: staffID, SessionID: actualSessionID}
}

func sha256Hash(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// --- Test: ExecuteMutation basic success ---

func TestExecuteMutation_Success(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()

	ident := createTestIdentity(t, db, q, []string{auth.RoleManager}, true)

	type CatResponse struct {
		Name string `json:"name"`
	}

	want := CatResponse{Name: "Drinks"}

	spec := catalog.MutationSpec{
		RequestID: uuid.New(),
		Operation: "test.create_mutation",
		Fingerprint: struct {
			Name string
		}{Name: "Drinks"},
		Required: []string{"catalog.administer_structure"},
	}

	var calls int
	audit := catalog.AuditRecord{EventType: "test.created", Details: map[string]string{"name": "Drinks"}}

	status, got, err := catalog.ExecuteMutation(ctx, runner,
		catalog.Actor{StaffID: ident.StaffID, SessionID: ident.SessionID},
		spec,
		func(q *sqlc.Queries) (int, CatResponse, catalog.AuditRecord, error) {
			calls++
			return 201, want, audit, nil
		},
	)
	require.NoError(t, err)
	assert.Equal(t, 201, status)
	assert.Equal(t, want, got)
	assert.Equal(t, 1, calls)
}

// --- Test: Session not found → UNAUTHORIZED ---

func TestExecuteMutation_SessionNotFound(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()

	ident := createTestIdentity(t, db, q, []string{auth.RoleManager}, true)

	spec := catalog.MutationSpec{
		RequestID: uuid.New(),
		Operation: "test.noop",
		Required:  []string{"catalog.administer_structure"},
	}

	fakeSession := uuid.New()

	_, _, err := catalog.ExecuteMutation(ctx, runner,
		catalog.Actor{StaffID: ident.StaffID, SessionID: fakeSession},
		spec,
		func(q *sqlc.Queries) (int, struct{}, catalog.AuditRecord, error) {
			t.Fatal("callback should not be called")
			return 0, struct{}{}, catalog.AuditRecord{}, nil
		},
	)
	require.Error(t, err)
	assert.True(t, errors.Is(err, catalog.ErrUnauthorized), "expected ErrUnauthorized, got: %v", err)
}

// --- Test: Session revoked → UNAUTHORIZED ---

func TestExecuteMutation_SessionRevoked(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()

	ident := createTestIdentity(t, db, q, []string{auth.RoleManager}, true)

	// Revoke the session
	err := q.UpdateSessionState(ctx, sqlc.UpdateSessionStateParams{
		State: auth.SessionStateLocked,
		ID:    ident.SessionID,
	})
	require.NoError(t, err)

	spec := catalog.MutationSpec{
		RequestID: uuid.New(),
		Operation: "test.noop",
		Required:  []string{"catalog.administer_structure"},
	}

	_, _, err = catalog.ExecuteMutation(ctx, runner,
		catalog.Actor{StaffID: ident.StaffID, SessionID: ident.SessionID},
		spec,
		func(q *sqlc.Queries) (int, struct{}, catalog.AuditRecord, error) {
			t.Fatal("callback should not be called")
			return 0, struct{}{}, catalog.AuditRecord{}, nil
		},
	)
	require.Error(t, err)
	assert.True(t, errors.Is(err, catalog.ErrUnauthorized), "expected ErrUnauthorized, got: %v", err)
}

// --- Test: Session expired → UNAUTHORIZED ---

func TestExecuteMutation_SessionExpired(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()

	ident := createTestIdentity(t, db, q, []string{auth.RoleManager}, true)

	// Expire the session
	err := q.ExpireSession(ctx, sqlc.ExpireSessionParams{
		ExpiresAt: time.Now().Add(-1 * time.Hour),
		ID:        ident.SessionID,
	})
	require.NoError(t, err)

	spec := catalog.MutationSpec{
		RequestID: uuid.New(),
		Operation: "test.noop",
		Required:  []string{"catalog.administer_structure"},
	}

	_, _, err = catalog.ExecuteMutation(ctx, runner,
		catalog.Actor{StaffID: ident.StaffID, SessionID: ident.SessionID},
		spec,
		func(q *sqlc.Queries) (int, struct{}, catalog.AuditRecord, error) {
			t.Fatal("callback should not be called")
			return 0, struct{}{}, catalog.AuditRecord{}, nil
		},
	)
	require.Error(t, err)
	assert.True(t, errors.Is(err, catalog.ErrUnauthorized), "expected ErrUnauthorized, got: %v", err)
}

// --- Test: Identity disabled → UNAUTHORIZED ---

func TestExecuteMutation_IdentityDisabled(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()

	ident := createTestIdentity(t, db, q, []string{auth.RoleManager}, true)

	// Disable the identity
	err := q.DisableIdentity(ctx, sqlc.DisableIdentityParams{
		Enabled: false,
		ID:      ident.StaffID,
	})
	require.NoError(t, err)

	spec := catalog.MutationSpec{
		RequestID: uuid.New(),
		Operation: "test.noop",
		Required:  []string{"catalog.administer_structure"},
	}

	_, _, err = catalog.ExecuteMutation(ctx, runner,
		catalog.Actor{StaffID: ident.StaffID, SessionID: ident.SessionID},
		spec,
		func(q *sqlc.Queries) (int, struct{}, catalog.AuditRecord, error) {
			t.Fatal("callback should not be called")
			return 0, struct{}{}, catalog.AuditRecord{}, nil
		},
	)
	require.Error(t, err)
	assert.True(t, errors.Is(err, catalog.ErrForbidden), "expected ErrForbidden, got: %v", err)
}

// --- Test: Missing capabilities → FORBIDDEN ---

func TestExecuteMutation_MissingCapabilities(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	// Barista does not have catalog.administer_structure
	ident := createTestIdentity(t, db, q, []string{auth.RoleBarista}, true)
	ctx := context.Background()

	spec := catalog.MutationSpec{
		RequestID: uuid.New(),
		Operation: "test.noop",
		Required:  []string{"catalog.administer_structure"},
	}

	_, _, err := catalog.ExecuteMutation(ctx, runner,
		catalog.Actor{StaffID: ident.StaffID, SessionID: ident.SessionID},
		spec,
		func(q *sqlc.Queries) (int, struct{}, catalog.AuditRecord, error) {
			t.Fatal("callback should not be called")
			return 0, struct{}{}, catalog.AuditRecord{}, nil
		},
	)
	require.Error(t, err)
	assert.True(t, errors.Is(err, catalog.ErrForbidden), "expected ErrForbidden, got: %v", err)
}

// --- Test: Fresh Manager PIN verified ---

func TestExecuteMutation_InvalidManagerPin(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()

	pinHash, err := auth.HashPin("1234")
	require.NoError(t, err)

	loginCode := fmt.Sprintf("pinmgr%s", uuid.New().String()[:6])
	row, err := q.CreateStaffIdentity(ctx, sqlc.CreateStaffIdentityParams{
		DisplayName: "PIN Manager",
		Btrim:       loginCode,
		PinHash:     pinHash,
		Enabled:     true,
	})
	require.NoError(t, err)
	staffID := row.ID

	err = q.AddStaffRole(ctx, sqlc.AddStaffRoleParams{
		StaffIdentityID: staffID,
		Role:            auth.RoleManager,
	})
	require.NoError(t, err)

	sessionID := uuid.New()
	tokenHash := fmt.Sprintf("tok_%s", sessionID.String()[:8])
	_, err = q.CreateStaffSession(ctx, sqlc.CreateStaffSessionParams{
		TokenHash:           tokenHash,
		StaffIdentityID:     staffID,
		State:               auth.SessionStateActive,
		ActiveWorkspace:     sql.NullString{String: auth.WorkspaceManager, Valid: true},
		LastAuthenticatedAt: time.Now(),
		LastHumanActivityAt: time.Now(),
		ExpiresAt:           time.Now().Add(12 * time.Hour),
	})
	require.NoError(t, err)

	var actualSessionID uuid.UUID
	err = db.QueryRow(`SELECT id FROM staff_access_sessions WHERE token_hash = $1`, tokenHash).Scan(&actualSessionID)
	require.NoError(t, err)

	spec := catalog.MutationSpec{
		RequestID:           uuid.New(),
		Operation:           "test.price_sensitive",
		Required:            []string{"catalog.change_price"},
		RequireManagerPIN:   true,
		ManagerPIN:          "9999", // wrong PIN
		Fingerprint:         struct{}{},
	}

	_, _, err = catalog.ExecuteMutation(ctx, runner,
		catalog.Actor{StaffID: staffID, SessionID: actualSessionID},
		spec,
		func(q *sqlc.Queries) (int, struct{}, catalog.AuditRecord, error) {
			t.Fatal("callback should not be called")
			return 0, struct{}{}, catalog.AuditRecord{}, nil
		},
	)
	require.Error(t, err)
	assert.True(t, errors.Is(err, catalog.ErrInvalidManagerPin), "expected ErrInvalidManagerPin, got: %v", err)
}

// --- Test: Exact replay → same status+body, callback called once ---

func TestExecuteMutation_ExactReplay(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()

	ident := createTestIdentity(t, db, q, []string{auth.RoleManager}, true)

	type Res struct {
		ID string `json:"id"`
	}

	want := Res{ID: "cat-1"}

	spec := catalog.MutationSpec{
		RequestID: uuid.New(),
		Operation: "test.replay",
		Fingerprint: struct{ Name string }{Name: "ReplayTest"},
		Required:  []string{"catalog.administer_structure"},
	}

	audit := catalog.AuditRecord{EventType: "test.replay_done", Details: map[string]string{"id": "cat-1"}}

	actor := catalog.Actor{StaffID: ident.StaffID, SessionID: ident.SessionID}

	var calls int

	status1, got1, err := catalog.ExecuteMutation(ctx, runner, actor, spec,
		func(q *sqlc.Queries) (int, Res, catalog.AuditRecord, error) {
			calls++
			return 201, want, audit, nil
		},
	)
	require.NoError(t, err)
	assert.Equal(t, 201, status1)
	assert.Equal(t, want, got1)
	assert.Equal(t, 1, calls)

	// Replay exact same request
	status2, got2, err := catalog.ExecuteMutation(ctx, runner, actor, spec,
		func(q *sqlc.Queries) (int, Res, catalog.AuditRecord, error) {
			calls++
			return 201, want, audit, nil
		},
	)
	require.NoError(t, err)
	assert.Equal(t, status1, status2)
	assert.Equal(t, got1, got2)
	// callback must NOT be called on replay
	assert.Equal(t, 1, calls)
}

// --- Test: Conflicting reuse (different operation) → REQUEST_CONFLICT ---

func TestExecuteMutation_ConflictingReuse(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()

	ident := createTestIdentity(t, db, q, []string{auth.RoleManager}, true)

	type Res struct{}
	actor := catalog.Actor{StaffID: ident.StaffID, SessionID: ident.SessionID}
	requestID := uuid.New()

	spec1 := catalog.MutationSpec{
		RequestID:   requestID,
		Operation:   "test.op_a",
		Fingerprint: struct{}{},
		Required:    []string{"catalog.administer_structure"},
	}

	_, _, err := catalog.ExecuteMutation(ctx, runner, actor, spec1,
		func(q *sqlc.Queries) (int, Res, catalog.AuditRecord, error) {
			return 201, Res{}, catalog.AuditRecord{EventType: "test.a"}, nil
		},
	)
	require.NoError(t, err)

	// Different operation with same request_id
	spec2 := catalog.MutationSpec{
		RequestID:   requestID,
		Operation:   "test.op_b",
		Fingerprint: struct{}{},
		Required:    []string{"catalog.administer_structure"},
	}

	_, _, err = catalog.ExecuteMutation(ctx, runner, actor, spec2,
		func(q *sqlc.Queries) (int, Res, catalog.AuditRecord, error) {
			t.Fatal("callback should not be called")
			return 0, Res{}, catalog.AuditRecord{}, nil
		},
	)
	require.Error(t, err)
	assert.True(t, errors.Is(err, catalog.ErrRequestConflict), "expected ErrRequestConflict, got: %v", err)
}

// --- Test: Corrupted stored JSON → INVALID_STORED_RESULT ---

func TestExecuteMutation_CorruptedStoredResult(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()

	ident := createTestIdentity(t, db, q, []string{auth.RoleManager}, true)

	type Res struct{}
	actor := catalog.Actor{StaffID: ident.StaffID, SessionID: ident.SessionID}
	requestID := uuid.New()

	// First successful execution
	spec := catalog.MutationSpec{
		RequestID:   requestID,
		Operation:   "test.corrupt",
		Fingerprint: struct{ Name string }{Name: "X"},
		Required:    []string{"catalog.administer_structure"},
	}

	_, _, err := catalog.ExecuteMutation(ctx, runner, actor, spec,
		func(q *sqlc.Queries) (int, Res, catalog.AuditRecord, error) {
			return 201, Res{}, catalog.AuditRecord{EventType: "test.corrupt"}, nil
		},
	)
	require.NoError(t, err)

	// Corrupt the stored response_body with valid JSON but wrong structure
	_, err = db.Exec(`UPDATE catalog_mutation_requests
		SET response_body = '"not a struct"'
		WHERE actor_id = $1 AND request_id = $2`, ident.StaffID, requestID)
	require.NoError(t, err)

	// Replay: should detect corruption
	_, _, err = catalog.ExecuteMutation(ctx, runner, actor, spec,
		func(q *sqlc.Queries) (int, Res, catalog.AuditRecord, error) {
			t.Fatal("callback should not be called")
			return 0, Res{}, catalog.AuditRecord{}, nil
		},
	)
	require.Error(t, err)
	assert.True(t, errors.Is(err, catalog.ErrInvalidStoredResult), "expected ErrInvalidStoredResult, got: %v", err)
}

// --- Test: Authorization-before-replay (denied after role removal) ---

func TestExecuteMutation_AuthorizationBeforeReplay(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()

	ident := createTestIdentity(t, db, q, []string{auth.RoleManager}, true)

	type Res struct{}
	actor := catalog.Actor{StaffID: ident.StaffID, SessionID: ident.SessionID}
	requestID := uuid.New()

	spec := catalog.MutationSpec{
		RequestID:   requestID,
		Operation:   "test.auth_replay",
		Fingerprint: struct{}{},
		Required:    []string{"catalog.administer_structure"},
	}

	// First execution succeeds
	_, _, err := catalog.ExecuteMutation(ctx, runner, actor, spec,
		func(q *sqlc.Queries) (int, Res, catalog.AuditRecord, error) {
			return 201, Res{}, catalog.AuditRecord{EventType: "test.auth_replay"}, nil
		},
	)
	require.NoError(t, err)

	// Remove manager role
	err = q.ClearStaffRoles(ctx, ident.StaffID)
	require.NoError(t, err)
	err = q.AddStaffRole(ctx, sqlc.AddStaffRoleParams{
		StaffIdentityID: ident.StaffID,
		Role:            auth.RoleBarista,
	})
	require.NoError(t, err)

	// Replay: authorization check runs BEFORE replay
	_, _, err = catalog.ExecuteMutation(ctx, runner, actor, spec,
		func(q *sqlc.Queries) (int, Res, catalog.AuditRecord, error) {
			t.Fatal("callback should not be called")
			return 0, Res{}, catalog.AuditRecord{}, nil
		},
	)
	require.Error(t, err)
	assert.True(t, errors.Is(err, catalog.ErrForbidden), "expected ErrForbidden, got: %v", err)
}

// --- Test: Audit failure → rollback of mutation and idempotency claim ---

func TestExecuteMutation_AuditFailureRollback(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()

	ident := createTestIdentity(t, db, q, []string{auth.RoleManager}, true)

	type Res struct{}
	actor := catalog.Actor{StaffID: ident.StaffID, SessionID: ident.SessionID}
	requestID := uuid.New()

	uniqueOp := fmt.Sprintf("test.audit_rollback_%s", requestID.String()[:8])
	spec := catalog.MutationSpec{
		RequestID:   requestID,
		Operation:   uniqueOp,
		Fingerprint: struct{}{},
		Required:    []string{"catalog.administer_structure"},
	}

	_, _, err := catalog.ExecuteMutation(ctx, runner, actor, spec,
		func(q *sqlc.Queries) (int, Res, catalog.AuditRecord, error) {
			return 0, Res{}, catalog.AuditRecord{}, fmt.Errorf("simulated mutation failure")
		},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "simulated mutation failure")

	events, err := q.ListAuditEvents(ctx, sqlc.ListAuditEventsParams{
		Column1: sql.NullString{String: uniqueOp, Valid: true},
		Limit:   10,
	})
	require.NoError(t, err)
	assert.Empty(t, events, "no audit events should exist for failed mutation")

	var calls int
	_, _, err = catalog.ExecuteMutation(ctx, runner, actor, spec,
		func(q *sqlc.Queries) (int, Res, catalog.AuditRecord, error) {
			calls++
			return 201, Res{}, catalog.AuditRecord{
				EventType: uniqueOp,
				Details:   map[string]string{"op": "test"},
			}, nil
		},
	)
	require.NoError(t, err)
	assert.Equal(t, 1, calls)
}

// --- Test: Denial event committed without business changes ---

func TestExecuteMutation_DenialEventCommitted(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()

	ident := createTestIdentity(t, db, q, []string{auth.RoleBarista}, true)
	actor := catalog.Actor{StaffID: ident.StaffID, SessionID: ident.SessionID}

	spec := catalog.MutationSpec{
		RequestID: uuid.New(),
		Operation: "test.denial",
		Required:  []string{"catalog.administer_structure"},
	}

	_, _, err := catalog.ExecuteMutation(ctx, runner, actor, spec,
		func(q *sqlc.Queries) (int, struct{}, catalog.AuditRecord, error) {
			t.Fatal("callback should not be called")
			return 0, struct{}{}, catalog.AuditRecord{}, nil
		},
	)
	require.Error(t, err)
	assert.True(t, errors.Is(err, catalog.ErrForbidden))

	// A denial audit event should have been committed
	events, err := q.ListAuditEvents(ctx, sqlc.ListAuditEventsParams{
		Column1: sql.NullString{String: "catalog.denial", Valid: true},
		Limit:   10,
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(events), 1, "at least one denial event expected")
}

// --- Test: Two-goroutine concurrent duplicate → only one succeeds ---

func TestExecuteMutation_ConcurrentDuplicate(t *testing.T) {
	db, q := openExecutorTestDB(t)
	// Ensure both goroutines use the same connection for advisory lock serialization
	db.SetMaxOpenConns(1)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()

	ident := createTestIdentity(t, db, q, []string{auth.RoleManager}, true)

	type Res struct{ N int }
	actor := catalog.Actor{StaffID: ident.StaffID, SessionID: ident.SessionID}
	requestID := uuid.New()

	spec := catalog.MutationSpec{
		RequestID:   requestID,
		Operation:   "test.concurrent",
		Fingerprint: struct{}{},
		Required:    []string{"catalog.administer_structure"},
	}

	var mu sync.Mutex
	var successCount int
	var errCount int

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := catalog.ExecuteMutation(ctx, runner, actor, spec,
				func(q *sqlc.Queries) (int, Res, catalog.AuditRecord, error) {
					mu.Lock()
					successCount++
					mu.Unlock()
					// Simulate work
					time.Sleep(50 * time.Millisecond)
					return 201, Res{N: 1}, catalog.AuditRecord{EventType: "test.concurrent"}, nil
				},
			)
			mu.Lock()
			if err != nil {
				errCount++
			}
			mu.Unlock()
		}()
	}
	wg.Wait()

	// Exactly one goroutine should execute the callback; the other replays
	assert.Equal(t, 1, successCount, "callback should be called exactly once")
	assert.Equal(t, 0, errCount, "both goroutines should succeed (one executes, one replays)")
}

// --- Test: ExecuteRead basic success ---

func TestExecuteRead_Success(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()

	ident := createTestIdentity(t, db, q, []string{auth.RoleManager}, true)
	actor := catalog.Actor{StaffID: ident.StaffID, SessionID: ident.SessionID}

	type ReadResult struct {
		Items []string `json:"items"`
	}

	want := ReadResult{Items: []string{"coffee", "tea"}}

	got, err := catalog.ExecuteRead(ctx, runner, actor, "catalog.view_prices",
		func(q *sqlc.Queries) (ReadResult, error) {
			return want, nil
		},
	)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

// --- Test: ExecuteRead missing capability → FORBIDDEN ---

func TestExecuteRead_Forbidden(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()

	ident := createTestIdentity(t, db, q, []string{auth.RoleBarista}, true)
	actor := catalog.Actor{StaffID: ident.StaffID, SessionID: ident.SessionID}

	_, err := catalog.ExecuteRead(ctx, runner, actor, "audit.inspect",
		func(q *sqlc.Queries) (struct{}, error) {
			t.Fatal("callback should not be called")
			return struct{}{}, nil
		},
	)
	require.Error(t, err)
	assert.True(t, errors.Is(err, catalog.ErrForbidden))
}

// --- Test: ExecuteRead session not found → UNAUTHORIZED ---

func TestExecuteRead_SessionNotFound(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()

	_, err := catalog.ExecuteRead(ctx, runner,
		catalog.Actor{StaffID: uuid.New(), SessionID: uuid.New()},
		"catalog.view_prices",
		func(q *sqlc.Queries) (struct{}, error) {
			t.Fatal("callback should not be called")
			return struct{}{}, nil
		},
	)
	require.Error(t, err)
	assert.True(t, errors.Is(err, catalog.ErrUnauthorized))
}
