//go:build integration

package sales_test

import (
	"context"
	"database/sql"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
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

// approvalProbePIN is the PIN every seeded approver authenticates with. It is
// eight digits so its exact text cannot occur inside a 64-character SHA-256
// fingerprint by coincidence.
const approvalProbePIN = "86429753"

// seedApprover creates an identity that can be named in an ApprovalSpec.
// Approval authenticates by login code and PIN only, so the identity needs no
// access session. It returns the canonical login code the row stores.
func seedApprover(t *testing.T, q *sqlc.Queries, roles []string, enabled bool) (uuid.UUID, string) {
	t.Helper()
	ctx := context.Background()

	hash, err := auth.HashPin(approvalProbePIN)
	require.NoError(t, err)

	loginCode := testLoginCode("M")
	row, err := q.CreateStaffIdentity(ctx, sqlc.CreateStaffIdentityParams{
		DisplayName: "Approver " + loginCode,
		Btrim:       loginCode,
		PinHash:     hash,
		Enabled:     enabled,
	})
	require.NoError(t, err)

	for _, role := range roles {
		require.NoError(t, q.AddStaffRole(ctx, sqlc.AddStaffRoleParams{
			StaffIdentityID: row.ID,
			Role:            role,
		}))
	}
	return row.ID, row.LoginCode
}

// loginCodeOf reads the stored canonical login code of a seeded identity.
func loginCodeOf(t *testing.T, db *sql.DB, staffID uuid.UUID) string {
	t.Helper()
	var code string
	require.NoError(t, db.QueryRow(
		`SELECT login_code FROM staff_identities WHERE id = $1`, staffID).Scan(&code))
	return code
}

// runApprovalProbe executes a minimal mutation that requires one inline
// Manager Approval, mirroring how Comp, Refund, and Payment Void construct
// their specs. It returns the approver the mutation body saw, so callers can
// prove the verified summary reaches the callback.
func runApprovalProbe(t *testing.T, runner *sales.Runner, actor sales.Actor,
	requestID uuid.UUID, approval sales.ApprovalSpec, calls *atomic.Int64,
) (int, probeResult, *auth.ApproverSummary, error) {
	t.Helper()
	var approver *auth.ApproverSummary
	status, result, err := sales.ExecuteMutation(context.Background(), runner, actor, sales.MutationSpec{
		RequestID:   requestID,
		Operation:   sales.OpStartTakeawaySession,
		Fingerprint: probeFingerprint{Value: "approved"},
		Required:    []string{sales.CapSalesOperate},
		Approval:    &approval,
	}, func(mc sales.MutationContext) (int, probeResult, sales.AuditRecord, error) {
		calls.Add(1)
		approver = mc.Approver
		return 201, probeResult{Value: "approved"}, sales.AuditRecord{
			EventType: sales.EventServiceSessionStarted,
			Details:   map[string]string{"value": "approved"},
		}, nil
	})
	return status, result, approver, err
}

// A valid Manager approval verifies before the request is claimed, and the
// verified summary reaches the mutation body.
func TestSalesExecutorApprovalAllowsValidManager(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	cashier := seedActor(t, q, []string{"CASHIER"})
	managerID, managerCode := seedApprover(t, q, []string{"MANAGER"}, true)
	var calls atomic.Int64

	status, result, approver, err := runApprovalProbe(t, runner, cashier, uuid.New(),
		sales.ApprovalSpec{
			ApproverLoginCode:  managerCode,
			ManagerPIN:         approvalProbePIN,
			RequiredCapability: sales.CapSalesOperate,
		}, &calls)
	require.NoError(t, err)
	assert.Equal(t, 201, status)
	assert.Equal(t, probeResult{Value: "approved"}, result)
	require.NotNil(t, approver, "an approval spec must deliver an approver")
	assert.Equal(t, managerID, approver.ID)
	assert.Equal(t, managerCode, approver.LoginCode)
	assert.NotEmpty(t, approver.DisplayName)
	assert.Equal(t, int64(1), calls.Load())
	assertAuditEvent(t, db, sales.EventServiceSessionStarted, 1)
	assertAuditEvent(t, db, sales.EventAuthorizationDenied, 0)
}

// A Manager working alone supplies their own login code and PIN. The code is
// lower-cased and padded on purpose: normalization must still find the
// identity, and initiator and approver are recorded as the same staff member.
func TestSalesExecutorApprovalAllowsSelfApproval(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	manager := seedActor(t, q, []string{"MANAGER"})
	code := loginCodeOf(t, db, manager.StaffID)
	var calls atomic.Int64

	status, _, approver, err := runApprovalProbe(t, runner, manager, uuid.New(),
		sales.ApprovalSpec{
			ApproverLoginCode:  "  " + strings.ToLower(code) + "  ",
			ManagerPIN:         "1234",
			RequiredCapability: sales.CapSalesOperate,
		}, &calls)
	require.NoError(t, err)
	assert.Equal(t, 201, status)
	require.NotNil(t, approver)
	assert.Equal(t, manager.StaffID, approver.ID,
		"a self-approved command records the initiator as its approver")
	assert.Equal(t, code, approver.LoginCode)
	assert.Equal(t, int64(1), calls.Load())
}

// Every approval denial reason collapses to one client-visible outcome, is
// committed as security-denial evidence, and leaves the request unclaimed.
func TestSalesExecutorApprovalDeniesInvalidApprovers(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	cashier := seedActor(t, q, []string{"CASHIER"})
	_, managerCode := seedApprover(t, q, []string{"MANAGER"}, true)
	_, disabledCode := seedApprover(t, q, []string{"MANAGER"}, false)
	_, demotedCode := seedApprover(t, q, []string{"CASHIER"}, true)
	unknownCode := testLoginCode("X")

	cases := []struct {
		name       string
		loginCode  string
		pin        string
		capability string
		reason     string
	}{
		{"wrong pin", managerCode, "00000000", sales.CapSalesOperate, auth.ApprovalDenialInvalidPin},
		{"unknown login code", unknownCode, approvalProbePIN, sales.CapSalesOperate, auth.ApprovalDenialInvalidPin},
		{"disabled approver", disabledCode, approvalProbePIN, sales.CapSalesOperate, auth.ApprovalDenialIdentityDisabled},
		{"demoted approver", demotedCode, approvalProbePIN, sales.CapSalesOperate, auth.ApprovalDenialManagerRoleRequired},
		// The Manager role derives every real capability, so the
		// capability branch is exercised with one no role ever holds.
		{"removed capability", managerCode, approvalProbePIN, "sales.comp.approve", auth.ApprovalDenialCapabilityRequired},
	}

	var calls atomic.Int64
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, _, approver, err := runApprovalProbe(t, runner, cashier, uuid.New(),
				sales.ApprovalSpec{
					ApproverLoginCode:  tc.loginCode,
					ManagerPIN:         tc.pin,
					RequiredCapability: tc.capability,
				}, &calls)
			require.Error(t, err)
			assert.ErrorIs(t, err, sales.ErrForbidden,
				"every approval denial must collapse to one client-visible outcome")
			assert.NotErrorIs(t, err, sales.ErrUnauthorized)
			assert.Equal(t, 0, status)
			assert.Nil(t, approver, "the mutation body must not run for a denied approval")
			assert.NotContains(t, err.Error(), tc.pin, "the attempted PIN never reaches the error")

			var reason, details string
			require.NoError(t, db.QueryRow(
				`SELECT details->>'reason', details::text FROM audit_events
				 WHERE event_type = $1 ORDER BY occurred_at DESC, id DESC LIMIT 1`,
				sales.EventAuthorizationDenied).Scan(&reason, &details))
			assert.Contains(t, reason, tc.reason)
			assert.Contains(t, details, sales.OpStartTakeawaySession, "the denial names the denied operation")
			assert.NotContains(t, details, tc.pin)
			assert.NotContains(t, details, tc.loginCode)
		})
	}

	assert.Equal(t, int64(0), calls.Load(), "no denied approval may run the mutation body")
	assertAuditEvent(t, db, sales.EventAuthorizationDenied, len(cases))
	assertAuditEvent(t, db, sales.EventServiceSessionStarted, 0)

	var claims int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM idempotency_keys`).Scan(&claims))
	assert.Equal(t, 0, claims, "a denied approval must not claim the request")
}

// Approval is verified before the idempotency lookup, so a replay cannot
// succeed using an approver who has since been disabled. The stored result
// survives the denial untouched.
func TestSalesExecutorApprovalRunsBeforeReplay(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	cashier := seedActor(t, q, []string{"CASHIER"})
	managerID, managerCode := seedApprover(t, q, []string{"MANAGER"}, true)
	requestID := uuid.New()
	var calls atomic.Int64

	approval := sales.ApprovalSpec{
		ApproverLoginCode:  managerCode,
		ManagerPIN:         approvalProbePIN,
		RequiredCapability: sales.CapSalesOperate,
	}

	status, first, approver, err := runApprovalProbe(t, runner, cashier, requestID, approval, &calls)
	require.NoError(t, err)
	assert.Equal(t, 201, status)
	require.NotNil(t, approver)
	assert.Equal(t, int64(1), calls.Load())

	_, err = db.Exec(`UPDATE staff_identities SET enabled = false WHERE id = $1`, managerID)
	require.NoError(t, err)

	status, _, approver, err = runApprovalProbe(t, runner, cashier, requestID, approval, &calls)
	require.Error(t, err)
	assert.ErrorIs(t, err, sales.ErrForbidden)
	assert.Equal(t, 0, status)
	assert.Nil(t, approver, "the stored result must be unreachable behind a denied approval")
	assert.Equal(t, int64(1), calls.Load(), "a denied replay must not run the mutation body again")
	assertAuditEvent(t, db, sales.EventAuthorizationDenied, 1)
	assertAuditEvent(t, db, sales.EventServiceSessionStarted, 1)

	var storedCode int32
	var storedBody string
	require.NoError(t, db.QueryRow(
		`SELECT response_code, response_body::text FROM idempotency_keys WHERE key = $1`,
		requestID).Scan(&storedCode, &storedBody))
	assert.Equal(t, int32(201), storedCode, "the denied replay must not rewrite the stored status")
	assert.Contains(t, storedBody, first.Value)
}

// Neither credential may reach the fingerprint or any stored artifact: a
// replay authenticating with a rotated PIN must still return the original
// stored result, and no audit or idempotency row may contain either value.
func TestSalesExecutorApprovalExcludesCredentialsFromStorage(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	cashier := seedActor(t, q, []string{"CASHIER"})
	managerID, managerCode := seedApprover(t, q, []string{"MANAGER"}, true)
	requestID := uuid.New()
	var calls atomic.Int64

	status, first, _, err := runApprovalProbe(t, runner, cashier, requestID,
		sales.ApprovalSpec{
			ApproverLoginCode:  managerCode,
			ManagerPIN:         approvalProbePIN,
			RequiredCapability: sales.CapSalesOperate,
		}, &calls)
	require.NoError(t, err)
	assert.Equal(t, 201, status)

	// Rotate the approver's PIN. The replay authenticates with the new PIN and
	// differs only in the credential; if the PIN were part of the fingerprint,
	// the exact replay would conflict instead of returning the stored result.
	newHash, err := auth.HashPin("99998888")
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE staff_identities SET pin_hash = $1 WHERE id = $2`, newHash, managerID)
	require.NoError(t, err)

	status, replay, _, err := runApprovalProbe(t, runner, cashier, requestID,
		sales.ApprovalSpec{
			ApproverLoginCode:  managerCode,
			ManagerPIN:         "99998888",
			RequiredCapability: sales.CapSalesOperate,
		}, &calls)
	require.NoError(t, err)
	assert.Equal(t, 201, status)
	assert.Equal(t, first, replay)
	assert.Equal(t, int64(1), calls.Load())

	var auditHits int
	require.NoError(t, db.QueryRow(
		`SELECT count(*) FROM audit_events
		 WHERE details::text LIKE '%' || $1 || '%' OR details::text LIKE '%' || $2 || '%'`,
		approvalProbePIN, managerCode).Scan(&auditHits))
	assert.Equal(t, 0, auditHits, "no approval credential may reach the audit trail")

	var idempotencyHits int
	require.NoError(t, db.QueryRow(
		`SELECT count(*) FROM idempotency_keys
		 WHERE response_body::text LIKE '%' || $1 || '%'
		    OR request_hash LIKE '%' || $1 || '%'
		    OR response_body::text LIKE '%' || $2 || '%'
		    OR request_hash LIKE '%' || $2 || '%'`,
		approvalProbePIN, managerCode).Scan(&idempotencyHits))
	assert.Equal(t, 0, idempotencyHits, "no approval credential may reach the idempotency record")
}
