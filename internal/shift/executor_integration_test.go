//go:build integration

package shift_test

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/database"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/shift"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openShiftTestDB(t *testing.T) (*sql.DB, *sqlc.Queries) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	require.Contains(t, url, "_test")
	db, err := database.Open(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db, sqlc.New(db)
}

// truncateShiftTables clears Shift state before a test.
//
// This is mandatory, not hygiene: the one-open-Shift invariant is global, so a
// Shift left open by an earlier test makes every later open fail. Integration
// packages run with -p 1 precisely so this truncation is safe.
func truncateShiftTables(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`TRUNCATE cash_movements, sales_shifts RESTART IDENTITY CASCADE`)
	require.NoError(t, err)
}

type testActor struct {
	StaffID   uuid.UUID
	SessionID uuid.UUID
	LoginCode string
	Pin       string
}

func (a testActor) actor() shift.Actor {
	return shift.Actor{StaffID: a.StaffID, SessionID: a.SessionID}
}

// testLoginCode generates a high-entropy unique login code for a test identity.
// staff_identities.login_code is VARCHAR(24) and the prefix shares that budget.
// Identities are never deleted between runs, so a per-process counter would
// collide across runs.
func testLoginCode(prefix string) string {
	room := 24 - len(prefix)
	if room < 8 {
		panic("testLoginCode: prefix leaves too little entropy budget")
	}
	return prefix + strings.ReplaceAll(uuid.NewString(), "-", "")[:room]
}

// newTestActor creates an identity with an active session and no usable PIN.
func newTestActor(t *testing.T, q *sqlc.Queries, roles []string, enabled bool) testActor {
	t.Helper()
	return newTestActorWithPin(t, q, roles, enabled, "")
}

// newTestActorWithPin creates an identity whose PIN can be used for approval.
// Pass an empty pin when the identity never needs to approve anything.
func newTestActorWithPin(t *testing.T, q *sqlc.Queries, roles []string, enabled bool, pin string) testActor {
	t.Helper()
	ctx := context.Background()

	loginCode := testLoginCode("S")
	pinHash := ""
	if pin != "" {
		hash, err := auth.HashPin(pin)
		require.NoError(t, err)
		pinHash = hash
	}

	row, err := q.CreateStaffIdentity(ctx, sqlc.CreateStaffIdentityParams{
		DisplayName: "Shift Test " + loginCode,
		Btrim:       loginCode,
		PinHash:     pinHash,
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

	return testActor{StaffID: row.ID, SessionID: session.ID, LoginCode: loginCode, Pin: pin}
}

type probeFingerprint struct {
	Label string `json:"label"`
}

type probeResult struct {
	Value string `json:"value"`
}

func TestExecuteMutationRequiresCapability(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	runner := shift.NewRunner(db, q)
	ctx := context.Background()

	// BARISTA holds neither sales.operate nor sales_shift.operate.
	barista := newTestActor(t, q, []string{"BARISTA"}, true)

	_, _, err := shift.ExecuteMutation(ctx, runner, barista.actor(), shift.MutationSpec{
		RequestID:   uuid.New(),
		Operation:   shift.OpOpenShift,
		Fingerprint: probeFingerprint{Label: "probe"},
		Required:    []string{shift.CapSalesShiftOperate},
	}, func(shift.MutationContext) (int, probeResult, shift.AuditRecord, error) {
		t.Fatal("mutation body must not run for an unauthorized actor")
		return 0, probeResult{}, shift.AuditRecord{}, nil
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, shift.ErrForbidden)
}

func TestExecuteMutationReplaysStoredResult(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	runner := shift.NewRunner(db, q)
	ctx := context.Background()

	cashier := newTestActor(t, q, []string{"CASHIER"}, true)
	requestID := uuid.New()
	spec := shift.MutationSpec{
		RequestID:   requestID,
		Operation:   shift.OpOpenShift,
		Fingerprint: probeFingerprint{Label: "same"},
		Required:    []string{shift.CapSalesShiftOperate},
	}

	runs := 0
	body := func(shift.MutationContext) (int, probeResult, shift.AuditRecord, error) {
		runs++
		return 201, probeResult{Value: "first"}, shift.AuditRecord{}, nil
	}

	status, res, err := shift.ExecuteMutation(ctx, runner, cashier.actor(), spec, body)
	require.NoError(t, err)
	assert.Equal(t, 201, status)
	assert.Equal(t, "first", res.Value)

	status, res, err = shift.ExecuteMutation(ctx, runner, cashier.actor(), spec, body)
	require.NoError(t, err)
	assert.Equal(t, 201, status)
	assert.Equal(t, "first", res.Value)
	assert.Equal(t, 1, runs, "an exact replay must not re-run the mutation body")
}

func TestExecuteMutationRejectsReusedRequestID(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	runner := shift.NewRunner(db, q)
	ctx := context.Background()

	cashier := newTestActor(t, q, []string{"CASHIER"}, true)
	requestID := uuid.New()

	body := func(shift.MutationContext) (int, probeResult, shift.AuditRecord, error) {
		return 201, probeResult{Value: "stored"}, shift.AuditRecord{}, nil
	}

	_, _, err := shift.ExecuteMutation(ctx, runner, cashier.actor(), shift.MutationSpec{
		RequestID:   requestID,
		Operation:   shift.OpOpenShift,
		Fingerprint: probeFingerprint{Label: "original"},
		Required:    []string{shift.CapSalesShiftOperate},
	}, body)
	require.NoError(t, err)

	_, _, err = shift.ExecuteMutation(ctx, runner, cashier.actor(), shift.MutationSpec{
		RequestID:   requestID,
		Operation:   shift.OpOpenShift,
		Fingerprint: probeFingerprint{Label: "changed"},
		Required:    []string{shift.CapSalesShiftOperate},
	}, body)
	require.Error(t, err)
	assert.ErrorIs(t, err, shift.ErrRequestConflict)
}

func TestExecuteMutationDeniesReplayAfterIdentityDisabled(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	runner := shift.NewRunner(db, q)
	ctx := context.Background()

	cashier := newTestActor(t, q, []string{"CASHIER"}, true)
	spec := shift.MutationSpec{
		RequestID:   uuid.New(),
		Operation:   shift.OpOpenShift,
		Fingerprint: probeFingerprint{Label: "before"},
		Required:    []string{shift.CapSalesShiftOperate},
	}
	body := func(shift.MutationContext) (int, probeResult, shift.AuditRecord, error) {
		return 201, probeResult{Value: "stored"}, shift.AuditRecord{}, nil
	}

	_, _, err := shift.ExecuteMutation(ctx, runner, cashier.actor(), spec, body)
	require.NoError(t, err)

	_, err = db.Exec(`UPDATE staff_identities SET enabled = false WHERE id = $1`, cashier.StaffID)
	require.NoError(t, err)

	// Authority is reloaded before replay, so the stored result is unreachable.
	_, _, err = shift.ExecuteMutation(ctx, runner, cashier.actor(), spec, body)
	require.Error(t, err)
	assert.ErrorIs(t, err, shift.ErrForbidden)
}

func TestExecuteMutationApprovalRunsBeforeReplay(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	runner := shift.NewRunner(db, q)
	ctx := context.Background()

	cashier := newTestActor(t, q, []string{"CASHIER"}, true)
	manager := newTestActorWithPin(t, q, []string{"MANAGER"}, true, "8642")

	spec := shift.MutationSpec{
		RequestID:   uuid.New(),
		Operation:   shift.OpRecordCashMovement,
		Fingerprint: probeFingerprint{Label: "approved"},
		Required:    []string{shift.CapSalesShiftOperate},
		Approval: &shift.ApprovalSpec{
			ApproverLoginCode:  manager.LoginCode,
			ManagerPIN:         "8642",
			RequiredCapability: shift.CapSalesShiftOperate,
		},
	}

	var sawApprover uuid.UUID
	body := func(mc shift.MutationContext) (int, probeResult, shift.AuditRecord, error) {
		require.NotNil(t, mc.Approver, "an approval spec must deliver an approver")
		sawApprover = mc.Approver.ID
		return 201, probeResult{Value: "stored"}, shift.AuditRecord{}, nil
	}

	_, _, err := shift.ExecuteMutation(ctx, runner, cashier.actor(), spec, body)
	require.NoError(t, err)
	assert.Equal(t, manager.StaffID, sawApprover)

	// Demote the approver, then attempt an exact replay. Approval is verified
	// before the idempotency lookup, so the stored result is unreachable.
	_, err = db.Exec(`DELETE FROM staff_operational_roles WHERE staff_identity_id = $1`, manager.StaffID)
	require.NoError(t, err)

	_, _, err = shift.ExecuteMutation(ctx, runner, cashier.actor(), spec, body)
	require.Error(t, err)
	assert.ErrorIs(t, err, shift.ErrManagerApprovalUnavailable)
}

func TestExecuteMutationConcurrentDuplicatesRunOnce(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	runner := shift.NewRunner(db, q)
	ctx := context.Background()

	cashier := newTestActor(t, q, []string{"CASHIER"}, true)
	spec := shift.MutationSpec{
		RequestID:   uuid.New(),
		Operation:   shift.OpOpenShift,
		Fingerprint: probeFingerprint{Label: "concurrent"},
		Required:    []string{shift.CapSalesShiftOperate},
	}

	var mu sync.Mutex
	runs := 0
	body := func(shift.MutationContext) (int, probeResult, shift.AuditRecord, error) {
		mu.Lock()
		runs++
		mu.Unlock()
		return 201, probeResult{Value: "stored"}, shift.AuditRecord{}, nil
	}

	const goroutines = 4
	start := make(chan struct{})
	errs := make(chan error, goroutines)
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _, execErr := shift.ExecuteMutation(ctx, runner, cashier.actor(), spec, body)
			errs <- execErr
		}()
	}
	close(start)
	wg.Wait()
	close(errs)

	for execErr := range errs {
		assert.NoError(t, execErr, "every concurrent duplicate must succeed by replay")
	}
	assert.Equal(t, 1, runs, "the advisory lock must serialize duplicates so the body runs once")
}
