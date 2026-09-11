//go:build integration

package catalog_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
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
	StaffID   uuid.UUID
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

func countAuthorizationDenials(t *testing.T, db *sql.DB, operation string) int {
	t.Helper()

	var count int
	err := db.QueryRowContext(t.Context(), `
		SELECT count(*)
		FROM audit_events
		WHERE event_type = $1
		  AND details->>'operation' = $2`, catalog.EventAuthorizationDenied, operation).Scan(&count)
	require.NoError(t, err)
	return count
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
	operation := fmt.Sprintf("test.session_not_found.%s", uuid.New())

	spec := catalog.MutationSpec{
		RequestID: uuid.New(),
		Operation: operation,
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
	assert.Equal(t, 1, countAuthorizationDenials(t, db, operation))
}

// --- Test: Session revoked → UNAUTHORIZED ---

func TestExecuteMutation_SessionRevoked(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()

	ident := createTestIdentity(t, db, q, []string{auth.RoleManager}, true)

	err := q.RevokeSession(ctx, ident.SessionID)
	require.NoError(t, err)

	operation := fmt.Sprintf("test.revoked.%s", uuid.New())
	spec := catalog.MutationSpec{
		RequestID: uuid.New(),
		Operation: operation,
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
	assert.Equal(t, 1, countAuthorizationDenials(t, db, operation))
}

func TestExecuteMutation_SessionLocked(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()
	ident := createTestIdentity(t, db, q, []string{auth.RoleManager}, true)

	err := q.UpdateSessionState(ctx, sqlc.UpdateSessionStateParams{
		State: auth.SessionStateLocked,
		ID:    ident.SessionID,
	})
	require.NoError(t, err)

	operation := fmt.Sprintf("test.locked.%s", uuid.New())
	_, _, err = catalog.ExecuteMutation(ctx, runner,
		catalog.Actor{StaffID: ident.StaffID, SessionID: ident.SessionID},
		catalog.MutationSpec{
			RequestID: uuid.New(),
			Operation: operation,
			Required:  []string{"catalog.administer_structure"},
		},
		func(q *sqlc.Queries) (int, struct{}, catalog.AuditRecord, error) {
			t.Fatal("callback should not be called")
			return 0, struct{}{}, catalog.AuditRecord{}, nil
		},
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, catalog.ErrUnauthorized)
	assert.Equal(t, 1, countAuthorizationDenials(t, db, operation))
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

func TestExecuteMutation_InactiveSessionDenied(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()
	ident := createTestIdentity(t, db, q, []string{auth.RoleManager}, true)

	_, err := db.ExecContext(ctx, `
		UPDATE staff_access_sessions
		SET active_workspace = $2, last_human_activity_at = $3
		WHERE id = $1`, ident.SessionID, auth.WorkspacePreparation,
		time.Now().Add(-auth.PreparationInactivityTimeout-time.Minute))
	require.NoError(t, err)

	operation := fmt.Sprintf("test.inactive.%s", uuid.New())
	called := false
	_, _, err = catalog.ExecuteMutation(ctx, runner,
		catalog.Actor{StaffID: ident.StaffID, SessionID: ident.SessionID},
		catalog.MutationSpec{
			RequestID: uuid.New(),
			Operation: operation,
			Required:  []string{"catalog.administer_structure"},
		},
		func(q *sqlc.Queries) (int, struct{}, catalog.AuditRecord, error) {
			called = true
			return 200, struct{}{}, catalog.AuditRecord{EventType: "test.unexpected"}, nil
		},
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, catalog.ErrUnauthorized)
	assert.False(t, called)
	assert.Equal(t, 1, countAuthorizationDenials(t, db, operation))
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
		RequestID:         uuid.New(),
		Operation:         "test.price_sensitive",
		Required:          []string{"catalog.change_price"},
		RequireManagerPIN: true,
		ManagerPIN:        "9999", // wrong PIN
		Fingerprint:       struct{}{},
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

func TestExecuteMutation_ManagerPINImplicitlyRequiresPriceCapability(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()
	ident := createTestIdentity(t, db, q, []string{auth.RoleCashier}, true)

	pinHash, err := auth.HashPin("1234")
	require.NoError(t, err)
	err = q.UpdateStaffPin(ctx, sqlc.UpdateStaffPinParams{ID: ident.StaffID, PinHash: pinHash})
	require.NoError(t, err)

	operation := fmt.Sprintf("test.implicit_price_capability.%s", uuid.New())
	called := false
	_, _, err = catalog.ExecuteMutation(ctx, runner,
		catalog.Actor{StaffID: ident.StaffID, SessionID: ident.SessionID},
		catalog.MutationSpec{
			RequestID:         uuid.New(),
			Operation:         operation,
			Fingerprint:       struct{}{},
			Required:          []string{"catalog.manage_availability"},
			ManagerPIN:        "1234",
			RequireManagerPIN: true,
		},
		func(q *sqlc.Queries) (int, struct{}, catalog.AuditRecord, error) {
			called = true
			return 200, struct{}{}, catalog.AuditRecord{EventType: "test.unexpected"}, nil
		},
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, catalog.ErrForbidden)
	assert.False(t, called)
	assert.Equal(t, 1, countAuthorizationDenials(t, db, operation))
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
		RequestID:   uuid.New(),
		Operation:   "test.replay",
		Fingerprint: struct{ Name string }{Name: "ReplayTest"},
		Required:    []string{"catalog.administer_structure"},
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

func TestExecuteMutation_FingerprintMarshalFailure(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()
	ident := createTestIdentity(t, db, q, []string{auth.RoleManager}, true)
	requestID := uuid.New()
	called := false

	_, _, err := catalog.ExecuteMutation(ctx, runner,
		catalog.Actor{StaffID: ident.StaffID, SessionID: ident.SessionID},
		catalog.MutationSpec{
			RequestID:   requestID,
			Operation:   "test.invalid_fingerprint",
			Fingerprint: make(chan int),
			Required:    []string{"catalog.administer_structure"},
		},
		func(q *sqlc.Queries) (int, struct{}, catalog.AuditRecord, error) {
			called = true
			return 200, struct{}{}, catalog.AuditRecord{EventType: "test.unexpected"}, nil
		},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "marshal mutation fingerprint")
	assert.False(t, called)

	var count int
	err = db.QueryRowContext(ctx, `
		SELECT count(*) FROM catalog_mutation_requests
		WHERE actor_id = $1 AND request_id = $2`, ident.StaffID, requestID).Scan(&count)
	require.NoError(t, err)
	assert.Zero(t, count)
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

	type Res struct{ CategoryID uuid.UUID }
	actor := catalog.Actor{StaffID: ident.StaffID, SessionID: ident.SessionID}
	requestID := uuid.New()
	uniqueOp := fmt.Sprintf("test.audit_failure.%s", requestID)
	categoryName := fmt.Sprintf("Audit rollback %s", requestID)
	normalizedName := requestID.String()

	_, err := db.ExecContext(ctx, `
		CREATE OR REPLACE FUNCTION fail_catalog_test_audit() RETURNS trigger AS $$
		BEGIN
			IF NEW.event_type LIKE 'test.audit_failure.%' THEN
				RAISE EXCEPTION 'forced catalog audit failure';
			END IF;
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql;
		DROP TRIGGER IF EXISTS fail_catalog_test_audit ON audit_events;
		CREATE TRIGGER fail_catalog_test_audit
		BEFORE INSERT ON audit_events
		FOR EACH ROW EXECUTE FUNCTION fail_catalog_test_audit();`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, cleanupErr := db.ExecContext(context.Background(), `
			DROP TRIGGER IF EXISTS fail_catalog_test_audit ON audit_events;
			DROP FUNCTION IF EXISTS fail_catalog_test_audit();`)
		require.NoError(t, cleanupErr)
	})

	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	spec := catalog.MutationSpec{
		RequestID:   requestID,
		Operation:   uniqueOp,
		Fingerprint: struct{}{},
		Required:    []string{"catalog.administer_structure"},
	}

	_, _, err = catalog.ExecuteMutation(ctx, runner, actor, spec,
		func(q *sqlc.Queries) (int, Res, catalog.AuditRecord, error) {
			_, claimErr := q.GetCatalogMutationRequest(ctx, sqlc.GetCatalogMutationRequestParams{
				ActorID:   ident.StaffID,
				RequestID: requestID,
			})
			if claimErr != nil {
				return 0, Res{}, catalog.AuditRecord{}, fmt.Errorf("idempotency claim missing before mutation: %w", claimErr)
			}
			created, createErr := q.CreateMenuCategory(ctx, sqlc.CreateMenuCategoryParams{
				Name:           categoryName,
				NormalizedName: normalizedName,
			})
			if createErr != nil {
				return 0, Res{}, catalog.AuditRecord{}, createErr
			}
			return 201, Res{CategoryID: created.ID}, catalog.AuditRecord{
				EventType: uniqueOp,
				Details:   map[string]any{"category_id": created.ID},
			}, nil
		},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "insert audit event")
	assert.Empty(t, logs.String(), "executor must return audit errors without logging them")

	var categoryCount int
	err = db.QueryRowContext(ctx, `SELECT count(*) FROM menu_categories WHERE normalized_name = $1`, normalizedName).
		Scan(&categoryCount)
	require.NoError(t, err)
	assert.Zero(t, categoryCount)

	var requestCount int
	err = db.QueryRowContext(ctx, `
		SELECT count(*) FROM catalog_mutation_requests
		WHERE actor_id = $1 AND request_id = $2`, ident.StaffID, requestID).Scan(&requestCount)
	require.NoError(t, err)
	assert.Zero(t, requestCount)
}

// --- Test: Denial event committed without business changes ---

func TestExecuteMutation_DenialEventCommitted(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()

	ident := createTestIdentity(t, db, q, []string{auth.RoleBarista}, true)
	actor := catalog.Actor{StaffID: ident.StaffID, SessionID: ident.SessionID}
	operation := fmt.Sprintf("test.denial.%s", uuid.New())

	spec := catalog.MutationSpec{
		RequestID: uuid.New(),
		Operation: operation,
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

	assert.Equal(t, 1, countAuthorizationDenials(t, db, spec.Operation))
}

// --- Test: Two-goroutine concurrent duplicate → only one succeeds ---

func TestExecuteMutation_ConcurrentDuplicate(t *testing.T) {
	db, q := openExecutorTestDB(t)
	db.SetMaxOpenConns(4)
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

	var calls atomic.Int32
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	results := make(chan error, 2)

	callback := func(q *sqlc.Queries) (int, Res, catalog.AuditRecord, error) {
		if calls.Add(1) == 1 {
			close(firstEntered)
			<-releaseFirst
		}
		return 201, Res{N: 1}, catalog.AuditRecord{EventType: "test.concurrent"}, nil
	}

	var wg sync.WaitGroup
	wg.Go(func() {
		_, _, err := catalog.ExecuteMutation(ctx, runner, actor, spec, callback)
		results <- err
	})
	<-firstEntered
	wg.Go(func() {
		_, _, err := catalog.ExecuteMutation(ctx, runner, actor, spec, callback)
		results <- err
	})

	deadline := time.Now().Add(3 * time.Second)
	waitingOnAdvisoryLock := false
	lockBits := binary.BigEndian.Uint64(actor.StaffID[:8]) ^ binary.BigEndian.Uint64(requestID[:8])
	classID := int64(uint32(lockBits >> 32))
	objectID := int64(uint32(lockBits))
	for time.Now().Before(deadline) {
		var waiting int
		err := db.QueryRowContext(ctx, `
			SELECT count(*) FROM pg_locks
			WHERE locktype = 'advisory'
			  AND classid = $1::oid
			  AND objid = $2::oid
			  AND objsubid = 1
			  AND NOT granted`, classID, objectID).Scan(&waiting)
		require.NoError(t, err)
		if waiting > 0 && db.Stats().InUse >= 2 {
			waitingOnAdvisoryLock = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	close(releaseFirst)
	wg.Wait()
	close(results)

	require.True(t, waitingOnAdvisoryLock, "second transaction must overlap and wait on the advisory lock")
	for err := range results {
		require.NoError(t, err)
	}
	assert.Equal(t, int32(1), calls.Load(), "callback should be called exactly once")
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
