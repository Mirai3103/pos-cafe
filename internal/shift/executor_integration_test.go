//go:build integration

package shift_test

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/shift"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// openShiftTestDB returns the shared package pool and its queries. The pool is
// owned by TestMain and closed when the package's clone is dropped, so callers
// must not close it.
func openShiftTestDB(t *testing.T) (*sql.DB, *sqlc.Queries) {
	t.Helper()
	require.NotNil(t, shiftTestDB)
	return shiftTestDB, sqlc.New(shiftTestDB)
}

// truncateShiftTables clears Shift state before a test.
//
// This is mandatory, not hygiene: the one-open-Shift invariant is global, so a
// Shift left open by an earlier test makes every later open fail. Integration
// packages run with -p 1 precisely so this truncation is safe.
func truncateShiftTables(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`
		TRUNCATE shift_discrepancies, shift_closures, shift_qr_observations,
		         shift_cash_counts, shift_reconciliations,
		         refund_completions, refund_adjustment_allocations,
		         refund_payment_allocations, refunds, payment_voids, sales_comps,
		         preparation_cancellations, charge_adjustments,
		         cash_movements, sales_shifts
		RESTART IDENTITY CASCADE`)
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

func TestExecuteMutationAuditsDenialAttributionOnSessionNotFound(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	runner := shift.NewRunner(db, q)
	ctx := context.Background()

	cashier := newTestActor(t, q, []string{"CASHIER"}, true)
	// A SessionID with no matching row makes reloadAuthority return
	// sql.ErrNoRows: the race between the HTTP middleware's own valid check
	// and this transaction's reload. actor.StaffID names a real, currently
	// enabled identity and must still be attributed on the denial event.
	actor := shift.Actor{StaffID: cashier.StaffID, SessionID: uuid.New()}

	_, _, err := shift.ExecuteMutation(ctx, runner, actor, shift.MutationSpec{
		RequestID:   uuid.New(),
		Operation:   shift.OpOpenShift,
		Fingerprint: probeFingerprint{Label: "denied"},
		Required:    []string{shift.CapSalesShiftOperate},
	}, func(shift.MutationContext) (int, probeResult, shift.AuditRecord, error) {
		t.Fatal("mutation body must not run for a denied actor")
		return 0, probeResult{}, shift.AuditRecord{}, nil
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, shift.ErrUnauthorized)

	var actorID uuid.NullUUID
	require.NoError(t, db.QueryRow(
		`SELECT actor_id FROM audit_events WHERE event_type = $1 ORDER BY occurred_at DESC LIMIT 1`,
		shift.EventAuthorizationDenied,
	).Scan(&actorID))
	require.True(t, actorID.Valid, "denial audit event must attribute the actor even when the session reload misses")
	assert.Equal(t, cashier.StaffID, actorID.UUID)
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

// startReconciliation exercises the Phase 7 query path one command would run:
// lock the OPEN Shift, freeze the totals into a reconciliation, append the
// blind initial Cash Count, and move the Shift to CLOSING. It returns the
// reconciliation id and its initial count id.
func startReconciliation(
	t *testing.T, ctx context.Context, q *sqlc.Queries, shiftID uuid.UUID, starter testActor,
) (uuid.UUID, uuid.UUID) {
	t.Helper()

	locked, err := q.LockSalesShiftForReconciliation(ctx, shiftID)
	require.NoError(t, err)
	require.Equal(t, shift.StateOpen, locked.State)

	totals, err := q.GetShiftReconciliationTotals(ctx, shiftID)
	require.NoError(t, err)
	movements, err := q.SumCashMovements(ctx, shiftID)
	require.NoError(t, err)
	expectedCash, err := shift.ComputeExpectedCash(locked.OpeningFloatVnd,
		totals.CashPaymentVnd, totals.CashPaymentVoidVnd, totals.CashRefundVnd,
		movements.PayInVnd, movements.PayOutVnd)
	require.NoError(t, err)

	reconciliation, err := q.InsertShiftReconciliation(ctx, sqlc.InsertShiftReconciliationParams{
		SalesShiftID:                shiftID,
		StartedByStaffIdentityID:    starter.StaffID,
		StartedStaffAccessSessionID: starter.SessionID,
		OpeningFloatVnd:             locked.OpeningFloatVnd,
		PayInVnd:                    movements.PayInVnd,
		PayOutVnd:                   movements.PayOutVnd,
		CashPaymentVnd:              totals.CashPaymentVnd,
		CashPaymentVoidVnd:          totals.CashPaymentVoidVnd,
		CashRefundVnd:               totals.CashRefundVnd,
		ExpectedCashVnd:             expectedCash,
		ManualQrPaymentVnd:          totals.ManualQrPaymentVnd,
		ManualQrPaymentVoidVnd:      totals.ManualQrPaymentVoidVnd,
		ExpectedManualQrReceivedVnd: totals.ManualQrPaymentVnd - totals.ManualQrPaymentVoidVnd,
		ManualQrRefundVnd:           totals.ManualQrRefundVnd,
	})
	require.NoError(t, err)
	assert.False(t, reconciliation.StartedAt.IsZero())

	count, err := q.InsertShiftCashCount(ctx, sqlc.InsertShiftCashCountParams{
		ReconciliationID:            reconciliation.ID,
		Sequence:                    1,
		CountedCashVnd:              expectedCash,
		CountedByStaffIdentityID:    starter.StaffID,
		CountedStaffAccessSessionID: starter.SessionID,
	})
	require.NoError(t, err)
	assert.False(t, count.CountedAt.IsZero())

	require.NoError(t, q.TransitionSalesShiftToClosing(ctx, shiftID))
	return reconciliation.ID, count.ID
}

// closeShift exercises the Phase 7 query path Final Close runs: the closure
// aggregate plus its discrepancy set. approver nil means an exact close.
func closeShift(
	t *testing.T, ctx context.Context, q *sqlc.Queries, shiftID, reconciliationID,
	initialCountID, finalQrObservationID uuid.UUID, closer testActor, approver uuid.NullUUID,
	observedCashVND, expectedCashVND int64,
) uuid.UUID {
	t.Helper()

	difference := observedCashVND - expectedCashVND
	closure, err := q.InsertShiftClosure(ctx, sqlc.InsertShiftClosureParams{
		SalesShiftID:               shiftID,
		ReconciliationID:           reconciliationID,
		InitialCashCountID:         initialCountID,
		FinalCashCountID:           initialCountID,
		FinalQrObservationID:       finalQrObservationID,
		OpenerStaffIdentityID:      closer.StaffID,
		CloserStaffIdentityID:      closer.StaffID,
		CloserStaffAccessSessionID: closer.SessionID,
		ApprovedByStaffIdentityID:  approver,
		OpenedAt:                   time.Now().Add(-time.Hour),
		OpeningFloatVnd:            expectedCashVND,
		ExpectedCashVnd:            expectedCashVND,
		ObservedCashVnd:            observedCashVND,
		CashDifferenceVnd:          difference,
	})
	require.NoError(t, err)
	assert.False(t, closure.ClosedAt.IsZero())

	if difference == 0 {
		require.NoError(t, q.InsertShiftDiscrepancies(ctx, sqlc.InsertShiftDiscrepanciesParams{
			ShiftClosureID: closure.ID,
			Dimensions:     []string{},
			Expecteds:      []int64{},
			Observeds:      []int64{},
			Differences:    []int64{},
			Reasons:        []string{},
			Notes:          []string{},
		}), "an empty discrepancy set must insert nothing without error")
	} else {
		require.NoError(t, q.InsertShiftDiscrepancies(ctx, sqlc.InsertShiftDiscrepanciesParams{
			ShiftClosureID: closure.ID,
			Dimensions:     []string{shift.DimensionCash},
			Expecteds:      []int64{expectedCashVND},
			Observeds:      []int64{observedCashVND},
			Differences:    []int64{difference},
			Reasons:        []string{shift.ReasonCashCountDifference},
			Notes:          []string{""},
		}))
	}

	require.NoError(t, q.TransitionSalesShiftToClosed(ctx, shiftID))
	return closure.ID
}

func TestShiftReconciliationQueryRoundTrip(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	ctx := context.Background()

	cashier := newTestActor(t, q, []string{"CASHIER"}, true)
	manager := newTestActor(t, q, []string{"MANAGER"}, true)

	// A fresh cafe has no blockers of any kind.
	blockers, err := q.GetGlobalShiftClosureBlockers(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), blockers.UnsettledCheckCount)
	assert.Equal(t, int64(0), blockers.PendingRefundCount)
	assert.Equal(t, int64(0), blockers.UnresolvedCorrectionVnd)
	assert.Equal(t, int64(0), blockers.ActiveServiceSessionCount)

	opened, err := q.OpenSalesShift(ctx, sqlc.OpenSalesShiftParams{
		OpenedByStaffIdentityID: cashier.StaffID,
		OpeningFloatVnd:         500000,
	})
	require.NoError(t, err)

	reconciliationID, initialCountID := startReconciliation(t, ctx, q, opened.ID, cashier)

	// The latest evidence is the initial count alone until a QR recheck lands.
	latest, err := q.GetLatestReconciliationEvidence(ctx, reconciliationID)
	require.NoError(t, err)
	assert.Equal(t, initialCountID, latest.CashCountID)
	assert.Equal(t, int32(1), latest.CashCountSequence)
	assert.False(t, latest.QrObservationID.Valid, "no QR observation exists yet")

	qr, err := q.InsertShiftQRObservation(ctx, sqlc.InsertShiftQRObservationParams{
		ReconciliationID:             reconciliationID,
		Sequence:                     1,
		ObservedReceivedVnd:          0,
		ObservedRefundedVnd:          0,
		ObservedByStaffIdentityID:    cashier.StaffID,
		ObservedStaffAccessSessionID: cashier.SessionID,
	})
	require.NoError(t, err)
	latest, err = q.GetLatestReconciliationEvidence(ctx, reconciliationID)
	require.NoError(t, err)
	require.True(t, latest.QrObservationID.Valid)
	assert.Equal(t, qr.ID, latest.QrObservationID.UUID)

	snapshot, err := q.GetReconciliationSnapshot(ctx, opened.ID)
	require.NoError(t, err)
	assert.Equal(t, reconciliationID, snapshot.ID)
	assert.Equal(t, int64(500000), snapshot.ExpectedCashVnd)
	assert.Equal(t, int64(0), snapshot.PendingRefundVnd)

	closureID := closeShift(t, ctx, q, opened.ID, reconciliationID, initialCountID, qr.ID,
		cashier, uuid.NullUUID{}, 500000, 500000)

	discrepancies, err := q.ListShiftClosureDiscrepancies(ctx, closureID)
	require.NoError(t, err)
	assert.Empty(t, discrepancies, "an exact close leaves no discrepancy rows")

	counts, err := q.ListShiftCashCounts(ctx, reconciliationID)
	require.NoError(t, err)
	require.Len(t, counts, 1)
	observations, err := q.ListShiftQRObservations(ctx, reconciliationID)
	require.NoError(t, err)
	require.Len(t, observations, 1)

	detail, err := q.GetClosedShiftDetail(ctx, opened.ID)
	require.NoError(t, err)
	assert.Equal(t, closureID, detail.ClosureID)
	assert.Equal(t, cashier.StaffID, detail.StartedByStaffIdentityID)
	assert.Equal(t, int64(500000), detail.ExpectedCashVnd)
	assert.Equal(t, int64(500000), detail.ObservedCashVnd)
	assert.Equal(t, int64(0), detail.CashDifferenceVnd)
	assert.False(t, detail.ApprovedByStaffIdentityID.Valid)

	// A new Shift opens after closure; its close is discrepant and approved.
	second, err := q.OpenSalesShift(ctx, sqlc.OpenSalesShiftParams{
		OpenedByStaffIdentityID: cashier.StaffID,
		OpeningFloatVnd:         500000,
	})
	require.NoError(t, err, "the active-Shift index must allow a Shift after closure")

	secondReconciliationID, secondInitialCountID := startReconciliation(t, ctx, q, second.ID, cashier)
	secondQr, err := q.InsertShiftQRObservation(ctx, sqlc.InsertShiftQRObservationParams{
		ReconciliationID:             secondReconciliationID,
		Sequence:                     1,
		ObservedReceivedVnd:          0,
		ObservedRefundedVnd:          0,
		ObservedByStaffIdentityID:    cashier.StaffID,
		ObservedStaffAccessSessionID: cashier.SessionID,
	})
	require.NoError(t, err)
	secondClosureID := closeShift(t, ctx, q, second.ID, secondReconciliationID, secondInitialCountID,
		secondQr.ID, cashier, uuid.NullUUID{UUID: manager.StaffID, Valid: true}, 390000, 500000)

	secondDiscrepancies, err := q.ListShiftClosureDiscrepancies(ctx, secondClosureID)
	require.NoError(t, err)
	require.Len(t, secondDiscrepancies, 1)
	assert.Equal(t, shift.DimensionCash, secondDiscrepancies[0].Dimension)
	assert.Equal(t, int64(-110000), secondDiscrepancies[0].DifferenceVnd)
	assert.Equal(t, shift.ReasonCashCountDifference, secondDiscrepancies[0].Reason)
	assert.False(t, secondDiscrepancies[0].Note.Valid, "an empty Go note must store SQL NULL")

	secondDetail, err := q.GetClosedShiftDetail(ctx, second.ID)
	require.NoError(t, err)
	require.True(t, secondDetail.ApprovedByStaffIdentityID.Valid)
	assert.Equal(t, manager.StaffID, secondDetail.ApprovedByStaffIdentityID.UUID)

	// History reads both closures newest-first across an exclusive cursor.
	windowFrom := time.Now().Add(-24 * time.Hour)
	windowTo := time.Now().Add(24 * time.Hour)
	page, err := q.ListClosedShiftSummaries(ctx, sqlc.ListClosedShiftSummariesParams{
		ClosedFrom: windowFrom,
		ClosedTo:   windowTo,
		RowLimit:   1,
	})
	require.NoError(t, err)
	require.Len(t, page, 1)
	assert.Equal(t, second.ID, page[0].SalesShiftID, "the newest closure comes first")
	assert.True(t, page[0].HasDiscrepancy.Bool)

	page, err = q.ListClosedShiftSummaries(ctx, sqlc.ListClosedShiftSummariesParams{
		ClosedFrom:     windowFrom,
		ClosedTo:       windowTo,
		CursorClosedAt: sql.NullTime{Time: page[0].ClosedAt, Valid: true},
		CursorID:       uuid.NullUUID{UUID: page[0].ClosureID, Valid: true},
		RowLimit:       10,
	})
	require.NoError(t, err)
	require.Len(t, page, 1)
	assert.Equal(t, opened.ID, page[0].SalesShiftID, "the cursor excludes the first page")
	assert.False(t, page[0].HasDiscrepancy.Bool)
}
