//go:build integration

package sales_test

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"sync"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/Mirai3103/pos-cafe/internal/shift"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startTakeaway is the shorthand every start test uses.
func startTakeaway(t *testing.T, runner *sales.Runner, actor sales.Actor, requestID uuid.UUID) (
	int, sales.ServiceSessionResponse, error,
) {
	t.Helper()
	return sales.NewStartTakeawaySessionHandler(runner).Handle(
		context.Background(), actor,
		sales.StartTakeawaySessionCommand{RequestID: requestID})
}

// startErrorCode maps a handler error through the HTTP layer's mapping and
// returns its stable code, the way paymentErrorCode does for Payments.
func startErrorCode(t *testing.T, err error) string {
	t.Helper()
	_, mapped := mapErrorStatus(err)
	var coded *response.CodedError
	require.ErrorAs(t, mapped, &coded, "expected a coded error, got %v", mapped)
	return coded.Code
}

// countServiceSessions counts every Service Session row.
func countServiceSessions(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM service_sessions`).Scan(&n))
	return n
}

// Session Start must acquire the one open Sales Shift FOR SHARE before
// inserting its Session and hold it through commit (spec 11.1): the holder
// transaction below plays reconciliation, which takes the Shift FOR UPDATE and
// validates closure blockers against committed data. A Session that appears
// only after the release cannot be missed by blocker validation.
func TestSessionStartLocksOpenShift(t *testing.T) {
	t.Run("start blocks on a held open shift and starts after release", func(t *testing.T) {
		db, q := openSalesTestDB(t)
		truncateSalesTables(t, db)
		runner := sales.NewRunner(db, q)

		actor := seedActor(t, q, []string{"CASHIER"})
		shiftID := seedOpenShift(t, q, actor.StaffID)

		// Hold the Shift FOR UPDATE, like reconciliation start does.
		holder := holdRowLock(t, db,
			`SELECT id::text FROM sales_shifts WHERE id = $1 FOR UPDATE`, shiftID)

		var wg sync.WaitGroup
		var status int
		var resp sales.ServiceSessionResponse
		var err error
		wg.Add(1)
		go func() {
			defer wg.Done()
			status, resp, err = startTakeaway(t, runner, actor, uuid.New())
		}()

		// The start must be parked on the Shift row while the holder owns it.
		// The uncommitted Session insert would also park here later, on the
		// FK's FOR KEY SHARE — which is exactly too late, as the CLOSING
		// subtest proves — so this wait only synchronizes the race; the gate
		// itself is discriminated below.
		waitForLockWaiters(t, db, 1)
		holder.release(t)
		requireCorrectionRaceResolved(t, &wg)

		require.NoError(t, err)
		assert.Equal(t, 201, status)
		assert.Equal(t, shiftID, resp.SalesShiftID)
		assert.Equal(t, 1, countServiceSessions(t, db),
			"the released start must insert exactly its one Session")

		var state string
		require.NoError(t, db.QueryRow(
			`SELECT state FROM sales_shifts WHERE id = $1`, shiftID).Scan(&state))
		assert.Equal(t, "OPEN", state, "the start must leave the Shift OPEN")
	})

	t.Run("a shift that turns CLOSING under the lock rejects start", func(t *testing.T) {
		db, q := openSalesTestDB(t)
		truncateSalesTables(t, db)
		runner := sales.NewRunner(db, q)

		actor := seedActor(t, q, []string{"CASHIER"})
		shiftID := seedOpenShift(t, q, actor.StaffID)

		holder := holdRowLock(t, db,
			`SELECT id::text FROM sales_shifts WHERE id = $1 FOR UPDATE`, shiftID)

		var wg sync.WaitGroup
		var err error
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err = startTakeaway(t, runner, actor, uuid.New())
		}()
		waitForLockWaiters(t, db, 1)

		// Reconciliation's OPEN -> CLOSING transition happens under its own
		// FOR UPDATE, which the holder already owns, so the flip is written on
		// the holder transaction and becomes visible exactly at release.
		_, execErr := holder.tx.Exec(
			`UPDATE sales_shifts SET state = 'CLOSING' WHERE id = $1`, shiftID)
		require.NoError(t, execErr)
		holder.release(t)
		requireCorrectionRaceResolved(t, &wg)

		require.ErrorIs(t, err, sales.ErrOpenShiftRequired)
		status, _ := mapErrorStatus(err)
		assert.Equal(t, http.StatusConflict, status)
		assert.Equal(t, "OPEN_SALES_SHIFT_REQUIRED", startErrorCode(t, err))
		assert.Equal(t, 0, countServiceSessions(t, db),
			"a rejected start must insert no Session")
	})

	t.Run("a dine-in start is rejected the same way when the shift turns CLOSING", func(t *testing.T) {
		db, q := openSalesTestDB(t)
		truncateSalesTables(t, db)
		runner := sales.NewRunner(db, q)

		actor := seedActor(t, q, []string{"CASHIER"})
		shiftID := seedOpenShift(t, q, actor.StaffID)
		tableID := seedTable(t, db, "Bàn đóng ca")

		holder := holdRowLock(t, db,
			`SELECT id::text FROM sales_shifts WHERE id = $1 FOR UPDATE`, shiftID)

		var wg sync.WaitGroup
		var err error
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err = sales.NewStartDineInSessionHandler(runner).Handle(
				context.Background(), actor,
				sales.StartDineInSessionCommand{RequestID: uuid.New(), TableIDs: []uuid.UUID{tableID}})
		}()
		waitForLockWaiters(t, db, 1)

		_, execErr := holder.tx.Exec(
			`UPDATE sales_shifts SET state = 'CLOSING' WHERE id = $1`, shiftID)
		require.NoError(t, execErr)
		holder.release(t)
		requireCorrectionRaceResolved(t, &wg)

		require.ErrorIs(t, err, sales.ErrOpenShiftRequired)
		assert.Equal(t, "OPEN_SALES_SHIFT_REQUIRED", startErrorCode(t, err))
		assert.Equal(t, 0, countServiceSessions(t, db),
			"a rejected dine-in start must insert no Session")
		var assignments int
		require.NoError(t, db.QueryRow(`SELECT count(*) FROM table_assignments`).Scan(&assignments))
		assert.Zero(t, assignments, "a rejected dine-in start must assign no Table")
	})
}

func TestStartTakeawaySessionAllocatesSequentialNumbers(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)

	status, first, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)
	assert.Equal(t, 201, status)
	assert.Equal(t, "S00001", first.ServiceNumber)
	assert.Equal(t, sales.ModeTakeaway, first.ServiceMode)
	assert.Equal(t, sales.StateActive, first.State)
	require.NotNil(t, first.Draft, "opening a Session must create its editable draft")
	assert.Empty(t, first.Draft.Items)
	assert.Empty(t, first.Tables)

	_, second, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)
	assert.Equal(t, "S00002", second.ServiceNumber)

	assertAuditEvent(t, db, sales.EventServiceSessionStarted, 2)
}

func TestStartTakeawaySessionRequiresOpenShift(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	// Deliberately no Shift.

	_, _, err := startTakeaway(t, runner, actor, uuid.New())
	require.ErrorIs(t, err, sales.ErrOpenShiftRequired)

	var sessions int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM service_sessions`).Scan(&sessions))
	assert.Equal(t, 0, sessions, "a rejected open must create nothing")
}

// Service Numbers restart per Shift, which is the whole point of ADR-011.
func TestServiceNumberRestartsInANewShift(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)
	ctx := context.Background()

	actor := seedActor(t, q, []string{"CASHIER"})
	firstShift := seedOpenShift(t, q, actor.StaffID)

	_, first, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)
	assert.Equal(t, "S00001", first.ServiceNumber)

	// Close the first Shift and open a second. Phase 4 ships no close command,
	// so the test drives the state directly.
	_, err = db.ExecContext(ctx, `UPDATE sales_shifts SET state = 'CLOSED' WHERE id = $1`, firstShift)
	require.NoError(t, err)
	secondShift := seedOpenShift(t, q, actor.StaffID)

	_, second, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)
	assert.Equal(t, "S00001", second.ServiceNumber, "numbering restarts within each Shift")
	assert.Equal(t, secondShift, second.SalesShiftID)
}

// The advisory lock serializes allocation, so concurrent opens produce a
// gapless sequence with no duplicates and no retry loop.
func TestConcurrentStartsAllocateGaplessSequence(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)

	const n = 8
	start := make(chan struct{})
	numbers := make(chan string, n)
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			<-start
			_, resp, err := startTakeaway(t, runner, actor, uuid.New())
			errs <- err
			numbers <- resp.ServiceNumber
		}()
	}
	close(start)

	seen := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		require.NoError(t, <-errs)
		num := <-numbers
		assert.False(t, seen[num], "duplicate Service Number %s", num)
		seen[num] = true
	}
	for i := 1; i <= n; i++ {
		expected, err := sales.FormatServiceNumber(int32(i))
		require.NoError(t, err)
		assert.True(t, seen[expected], "sequence must be gapless; %s is missing", expected)
	}
}

func TestStartTakeawaySessionIsIdempotent(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)

	requestID := uuid.New()
	_, first, err := startTakeaway(t, runner, actor, requestID)
	require.NoError(t, err)

	_, replay, err := startTakeaway(t, runner, actor, requestID)
	require.NoError(t, err)
	assert.Equal(t, first.ID, replay.ID, "a replay must not open a second Session")

	var sessions int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM service_sessions`).Scan(&sessions))
	assert.Equal(t, 1, sessions)
}

// A replay must survive its Shift closing: the open-Shift precondition guards
// new work only, and runs after the idempotency claim.
func TestReplaySurvivesShiftClosing(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	shiftID := seedOpenShift(t, q, actor.StaffID)

	requestID := uuid.New()
	_, first, err := startTakeaway(t, runner, actor, requestID)
	require.NoError(t, err)

	_, err = db.Exec(`UPDATE sales_shifts SET state = 'CLOSED' WHERE id = $1`, shiftID)
	require.NoError(t, err)

	_, replay, err := startTakeaway(t, runner, actor, requestID)
	require.NoError(t, err, "a replay must return its stored result after the Shift closed")
	assert.Equal(t, first.ID, replay.ID)
}

// shiftReconciler drives the real Phase 07 Start Reconciliation handler
// against the Sales package's own clone database. The Shift package does not
// import internal/sales, but the reverse import is clean (the Shift library
// depends only on auth, sqlc, and response), so the cross-slice races of spec
// 14.4 run both real handlers instead of a state-flip proxy for the
// reconciliation side. The Sales-side Session Start gate itself keeps its raw
// UPDATE proxy in TestSessionStartLocksOpenShift, where the Shift package's
// handler is not involved.
type shiftReconciler struct {
	handler *shift.StartReconciliationHandler
	shiftID uuid.UUID
	actor   shift.Actor
}

func newShiftReconciler(t *testing.T, env *salesEnv) shiftReconciler {
	t.Helper()
	return shiftReconciler{
		handler: shift.NewStartReconciliationHandler(shift.NewRunner(env.DB, env.Queries)),
		shiftID: env.ShiftID,
		actor:   shift.Actor{StaffID: env.Actor.StaffID, SessionID: env.Actor.SessionID},
	}
}

// start runs one Start Reconciliation command with the fixture's counted
// cash: the seeded Shift's 100000 Opening Float with no other facts.
func (r shiftReconciler) start(requestID uuid.UUID) error {
	counted := int64(100_000)
	_, _, err := r.handler.Handle(context.Background(), r.actor,
		shift.StartReconciliationCommand{RequestID: requestID, ShiftID: r.shiftID, CountedCashVND: &counted})
	return err
}

// countReconciliations counts the Shift snapshot rows in the clone database.
func countReconciliations(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM shift_reconciliations`).Scan(&n))
	return n
}

// countIdempotencyRecords counts the idempotency records one request id
// carries. A rolled-back command leaves none.
func countIdempotencyRecords(t *testing.T, db *sql.DB, requestID uuid.UUID) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(
		`SELECT count(*) FROM idempotency_keys WHERE key = $1`, requestID).Scan(&n))
	return n
}

// countPayments counts one Check's Payment rows.
func countPayments(t *testing.T, db *sql.DB, checkID uuid.UUID) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(
		`SELECT count(*) FROM payments WHERE check_id = $1`, checkID).Scan(&n))
	return n
}

// countRefunds counts one Check's Refund rows.
func countRefunds(t *testing.T, db *sql.DB, checkID uuid.UUID) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(
		`SELECT count(*) FROM refunds WHERE check_id = $1`, checkID).Scan(&n))
	return n
}

// countVoids counts the Voids of one Payment.
func countVoids(t *testing.T, db *sql.DB, paymentID uuid.UUID) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(
		`SELECT count(*) FROM payment_voids WHERE payment_id = $1`, paymentID).Scan(&n))
	return n
}

// countComps counts the Comp facts of one Waste.
func countComps(t *testing.T, db *sql.DB, wasteID uuid.UUID) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(
		`SELECT count(*) FROM sales_comps WHERE preparation_waste_id = $1`, wasteID).Scan(&n))
	return n
}

// shiftErrorCode maps a Shift-handler error through the Shift HTTP boundary's
// mapping and returns its stable code.
func shiftErrorCode(t *testing.T, err error) (int, string) {
	t.Helper()
	mapped := shift.MapHTTPError(err)
	var coded *response.CodedError
	require.ErrorAs(t, mapped, &coded, "expected a coded Shift error, got %v", mapped)
	return coded.Status, coded.Code
}

// TestSalesShiftConcurrentReconciliationRaces pins the Sales-side race matrix of
// spec 14.4: Session Start, the session-bound financial writers, Commit, and
// Service Session closure against Reconciliation Start.
//
// Every subtest holds a known row in a test-owned transaction, launches the
// racing handlers behind a start barrier, waits (polled from PostgreSQL's own
// lock-wait state) until they are demonstrably parked, and only then releases
// — no sleep creates or decides a race. Each branch asserts the accepted
// outcome's persisted facts (spec 11.2): a writer is wholly present or wholly
// absent, and a rejected start leaves no reconciliation, attempt, audit
// success event, or idempotency result behind.
//
// One structural note the assertions encode honestly: Payment, Void, Comp,
// live Refund, and Commit all require a committed ACTIVE Service Session, and
// that Session is itself a closure blocker (spec 8, 11.1). A reconciliation
// start can therefore never commit first against these writers — its blocker
// read runs after the writer's transaction — so the "start wins, writer is
// wholly rejected after CLOSING" ordering is pinned by Session Start (this
// test) and the Shift package's Cash Movement race, which have no active
// Session.
func TestSalesShiftConcurrentReconciliationRaces(t *testing.T) {
	ctx := context.Background()

	shiftStartOf := func(env *salesEnv) shiftReconciler { return newShiftReconciler(t, env) }

	t.Run("session start versus reconciliation start", func(t *testing.T) {
		env := newSalesEnv(t)
		reconciler := shiftStartOf(env)

		sessionRequestID := uuid.New()
		startRequestID := uuid.New()

		// The Shift row is the boundary: Session Start takes it FOR SHARE and
		// holds it through its Session insert, Reconciliation Start takes it
		// FOR UPDATE (spec 11.1).
		holder := holdRowLock(t, env.DB,
			`SELECT id::text FROM sales_shifts WHERE id = $1 FOR UPDATE`, env.ShiftID)

		var sessionErr, startErr error
		var wg sync.WaitGroup
		startBarrier := make(chan struct{})
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-startBarrier
			_, _, sessionErr = sales.NewStartTakeawaySessionHandler(env.Runner).Handle(
				ctx, env.Actor, sales.StartTakeawaySessionCommand{RequestID: sessionRequestID})
		}()
		go func() {
			defer wg.Done()
			<-startBarrier
			startErr = reconciler.start(startRequestID)
		}()
		close(startBarrier)
		waitForLockWaiters(t, env.DB, 2)
		holder.release(t)
		requireCorrectionRaceResolved(t, &wg)

		sessions := countServiceSessions(t, env.DB)
		if sessionErr == nil {
			// The Session is wholly visible as a blocker (spec 11.2): the
			// start's blocker read runs after the Session committed and names
			// it.
			require.ErrorIs(t, startErr, shift.ErrActiveServiceSession)
			status, code := shiftErrorCode(t, startErr)
			assert.Equal(t, http.StatusConflict, status)
			assert.Equal(t, "SHIFT_ACTIVE_SERVICE_SESSION", code)
			assert.Equal(t, 1, sessions)
			assert.Equal(t, 0, countReconciliations(t, env.DB),
				"a start blocked by the Session froze no snapshot")
			assert.Equal(t, 0, countIdempotencyRecords(t, env.DB, startRequestID))
			assert.Equal(t, 1, countIdempotencyRecords(t, env.DB, sessionRequestID))
		} else {
			// Session Start rejects after CLOSING (spec 11.2): the start won
			// the row, committed CLOSING, and the Session's FOR SHARE grant
			// re-read an empty OPEN predicate.
			require.ErrorIs(t, sessionErr, sales.ErrOpenShiftRequired)
			assert.Equal(t, "OPEN_SALES_SHIFT_REQUIRED", startErrorCode(t, sessionErr))
			assert.Equal(t, 0, sessions, "the rejected start inserted no Session")
			require.NoError(t, startErr)
			assert.Equal(t, 1, countReconciliations(t, env.DB))
			assert.Equal(t, 0, countIdempotencyRecords(t, env.DB, sessionRequestID),
				"the rejected Session Start rolls its claim back")
		}
	})

	t.Run("payment versus reconciliation start", func(t *testing.T) {
		env := newSalesEnv(t)
		reconciler := shiftStartOf(env)
		session := env.StartTakeaway(t)
		env.AddDraftItem(t, session.ID, env.CoffeeID, nil)
		session = env.Commit(t, session.ID)
		checkID := env.soleCheckID(t, session.ID)

		payRequestID := uuid.New()
		startRequestID := uuid.New()
		payCmd := sales.PayCashCommand{
			RequestID:        payRequestID,
			CheckID:          checkID,
			AppliedAmountVND: 25000,
			CashTenderedVND:  25000,
		}

		holder := holdRowLock(t, env.DB,
			`SELECT id::text FROM sales_shifts WHERE id = $1 FOR UPDATE`, env.ShiftID)
		var payErr, startErr error
		var wg sync.WaitGroup
		startBarrier := make(chan struct{})
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-startBarrier
			_, _, payErr = sales.NewPayCashHandler(env.Runner).Handle(ctx, env.Actor, payCmd)
		}()
		go func() {
			defer wg.Done()
			<-startBarrier
			startErr = reconciler.start(startRequestID)
		}()
		close(startBarrier)
		waitForLockWaiters(t, env.DB, 2)
		holder.release(t)
		requireCorrectionRaceResolved(t, &wg)

		// The Payment commits whenever it is scheduled: the start can never
		// transition the Shift first, because the writer's committed state
		// always trips a blocker (see the test's structural note). If the
		// start's blocker read ran before the Payment landed, the still-OPEN
		// Check is the unsettled-Check blocker; after the Payment landed, the
		// settled Check leaves the ACTIVE Session as the blocker.
		require.NoError(t, payErr)
		require.True(t,
			errors.Is(startErr, shift.ErrUnsettledCheck) ||
				errors.Is(startErr, shift.ErrActiveServiceSession),
			"the start must be rejected by a blocker the committed state presents, got %v", startErr)
		assert.Equal(t, 1, countPayments(t, env.DB, checkID))
		assert.Equal(t, 0, countReconciliations(t, env.DB))
		assert.Equal(t, 1, countIdempotencyRecords(t, env.DB, payRequestID))
		assert.Equal(t, 0, countIdempotencyRecords(t, env.DB, startRequestID))
	})

	t.Run("refund versus reconciliation start", func(t *testing.T) {
		env := newRefundEnv(t)
		reconciler := shiftStartOf(env.salesEnv)
		_, _, wasteID, checkID := env.paidTakeawayWastedUnitWithMethod(t, sales.PaymentMethodCash)
		comp := env.compOK(t, env.compCommand(t, wasteID, sales.CompReasonCafeError, nil))
		paymentID := env.solePaymentIDForCheck(t, checkID)

		startRequestID := uuid.New()
		refundCmd := env.refundCommand(checkID, sales.RefundMethodCash, paymentID,
			comp.Comp.ChargeAdjustmentID, 25000)

		holder := holdRowLock(t, env.DB,
			`SELECT id::text FROM sales_shifts WHERE id = $1 FOR UPDATE`, env.ShiftID)
		var refundErr, startErr error
		var wg sync.WaitGroup
		startBarrier := make(chan struct{})
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-startBarrier
			_, _, refundErr = sales.NewRecordRefundHandler(env.Runner).Handle(ctx, env.Actor, refundCmd)
		}()
		go func() {
			defer wg.Done()
			<-startBarrier
			startErr = reconciler.start(startRequestID)
		}()
		close(startBarrier)
		waitForLockWaiters(t, env.DB, 2)
		holder.release(t)
		requireCorrectionRaceResolved(t, &wg)

		// The Refund commits whenever it runs, and the start can never
		// transition first (see the test's structural note). If the start's
		// blocker read ran before the Refund landed, the still-unresolved
		// Comp is the unresolved-correction blocker (spec 8's precedence);
		// after the Refund landed, the committed ACTIVE Session is the blocker.
		require.NoError(t, refundErr)
		require.True(t,
			errors.Is(startErr, shift.ErrUnresolvedCorrection) ||
				errors.Is(startErr, shift.ErrActiveServiceSession),
			"the start must be rejected by a blocker the committed state presents, got %v", startErr)
		assert.Equal(t, 1, countRefunds(t, env.DB, checkID))
		assert.Equal(t, 0, countReconciliations(t, env.DB))
		assert.Equal(t, 1, countIdempotencyRecords(t, env.DB, refundCmd.RequestID))
		assert.Equal(t, 0, countIdempotencyRecords(t, env.DB, startRequestID))
	})

	t.Run("payment void versus reconciliation start", func(t *testing.T) {
		env := newVoidEnv(t)
		reconciler := shiftStartOf(env.salesEnv)
		session := env.StartTakeaway(t)
		env.AddDraftItem(t, session.ID, env.CoffeeID, nil)
		session = env.Commit(t, session.ID)
		checkID := env.soleCheckID(t, session.ID)
		_, _, err := env.payCash(t, checkID, 25000, 25000)
		require.NoError(t, err)
		paymentID := env.solePaymentIDForCheck(t, checkID)

		startRequestID := uuid.New()
		voidCmd := env.voidCommand(paymentID, sales.VoidReasonWrongAmount, nil)

		holder := holdRowLock(t, env.DB,
			`SELECT id::text FROM sales_shifts WHERE id = $1 FOR UPDATE`, env.ShiftID)
		var voidErr, startErr error
		var wg sync.WaitGroup
		startBarrier := make(chan struct{})
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-startBarrier
			_, _, voidErr = sales.NewVoidPaymentHandler(env.Runner).Handle(ctx, env.Actor, voidCmd)
		}()
		go func() {
			defer wg.Done()
			<-startBarrier
			startErr = reconciler.start(startRequestID)
		}()
		close(startBarrier)
		waitForLockWaiters(t, env.DB, 2)
		holder.release(t)
		requireCorrectionRaceResolved(t, &wg)

		// The Void commits whenever it runs. If it committed before the
		// start's blocker read, the reopened Check is the unsettled-Check
		// blocker (spec 8's precedence); otherwise the committed Session is.
		require.NoError(t, voidErr)
		require.True(t,
			errors.Is(startErr, shift.ErrUnsettledCheck) ||
				errors.Is(startErr, shift.ErrActiveServiceSession),
			"the start must be rejected by a blocker the Void's side of the race made visible, got %v", startErr)
		assert.Equal(t, 1, countVoids(t, env.DB, paymentID))
		assert.Equal(t, 0, countReconciliations(t, env.DB))
		assert.Equal(t, 1, countIdempotencyRecords(t, env.DB, voidCmd.RequestID))
		assert.Equal(t, 0, countIdempotencyRecords(t, env.DB, startRequestID))
	})

	t.Run("comp versus reconciliation start", func(t *testing.T) {
		env := newCompEnv(t)
		reconciler := shiftStartOf(env.salesEnv)
		_, _, wasteID := env.liveWastedUnit(t)

		startRequestID := uuid.New()
		compCmd := env.compCommand(t, wasteID, sales.CompReasonCafeError, nil)

		holder := holdRowLock(t, env.DB,
			`SELECT id::text FROM sales_shifts WHERE id = $1 FOR UPDATE`, env.ShiftID)
		var compErr, startErr error
		var wg sync.WaitGroup
		startBarrier := make(chan struct{})
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-startBarrier
			_, _, compErr = sales.NewCompWasteHandler(env.Runner).Handle(ctx, env.Actor, compCmd)
		}()
		go func() {
			defer wg.Done()
			<-startBarrier
			startErr = reconciler.start(startRequestID)
		}()
		close(startBarrier)
		waitForLockWaiters(t, env.DB, 2)
		holder.release(t)
		requireCorrectionRaceResolved(t, &wg)

		// The Comp commits whenever it runs, whichever way the race resolves.
		require.NoError(t, compErr)
		// If the start's blocker read ran before the Comp landed, the still
		// OPEN Check is the unsettled-Check blocker; after the Comp zeroed and
		// settled the Check, the committed ACTIVE Session is the blocker.
		require.True(t,
			errors.Is(startErr, shift.ErrUnsettledCheck) ||
				errors.Is(startErr, shift.ErrActiveServiceSession),
			"the start must be rejected by a blocker the committed state presents, got %v", startErr)
		assert.Equal(t, 1, countComps(t, env.DB, wasteID))
		assert.Equal(t, 0, countReconciliations(t, env.DB))
		assert.Equal(t, 1, countIdempotencyRecords(t, env.DB, compCmd.RequestID))
		assert.Equal(t, 0, countIdempotencyRecords(t, env.DB, startRequestID))
	})

	t.Run("commit versus reconciliation start", func(t *testing.T) {
		env := newSalesEnv(t)
		reconciler := shiftStartOf(env)
		session := env.StartTakeaway(t)
		env.AddDraftItem(t, session.ID, env.CoffeeID, nil)

		// Commit locks the Session through its editable Draft, while the
		// start never touches Session rows (spec 11.1) — so holding the
		// Session parks only the Commit, and the start's committed-state
		// blocker read decides independently of the queue.
		holder := holdRowLock(t, env.DB,
			`SELECT id::text FROM service_sessions WHERE id = $1 FOR UPDATE`, session.ID)

		commitRequestID := uuid.New()
		startRequestID := uuid.New()
		var commitErr error
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, commitErr = sales.NewCommitOrderDraftHandler(env.Runner).Handle(
				ctx, env.Actor, sales.CommitOrderDraftCommand{
					RequestID:        commitRequestID,
					ServiceSessionID: session.ID,
				})
		}()
		waitForLockWaiters(t, env.DB, 1)

		startErr := reconciler.start(startRequestID)
		require.ErrorIs(t, startErr, shift.ErrActiveServiceSession,
			"the start must reject on the committed ACTIVE Session, not freeze a snapshot that could miss the in-flight Commit")
		status, code := shiftErrorCode(t, startErr)
		assert.Equal(t, http.StatusConflict, status)
		assert.Equal(t, "SHIFT_ACTIVE_SERVICE_SESSION", code)
		assert.Equal(t, 0, countReconciliations(t, env.DB))
		assert.Equal(t, 0, countIdempotencyRecords(t, env.DB, startRequestID))

		holder.release(t)
		requireCorrectionRaceResolved(t, &wg)
		require.NoError(t, commitErr, "the Commit commits once its Session lock returns")

		var editable int
		require.NoError(t, env.DB.QueryRow(
			`SELECT count(*) FROM order_drafts WHERE service_session_id = $1 AND state = 'EDITABLE'`,
			session.ID).Scan(&editable))
		assert.Equal(t, 0, editable, "the Commit consumed the Session's editable draft")
	})

	t.Run("session closure versus reconciliation start", func(t *testing.T) {
		env := newCompEnv(t)
		reconciler := shiftStartOf(env.salesEnv)
		session := env.StartTakeaway(t)
		env.AddDraftItem(t, session.ID, env.CoffeeID, nil)
		session = env.Commit(t, session.ID)
		checkID := env.soleCheckID(t, session.ID)
		_, _, err := env.payCash(t, checkID, 25000, 25000)
		require.NoError(t, err)
		session = env.Submit(t, session.ID)
		// A Session closes only once every Preparation Unit is terminal, so
		// take the submitted unit READY and then WASTED before the race.
		require.Len(t, session.PreparationUnits, 1)
		unitID := session.PreparationUnits[0].ID
		env.advanceToReady(t, unitID)
		env.wasteUnit(t, unitID)

		holder := holdRowLock(t, env.DB,
			`SELECT id::text FROM service_sessions WHERE id = $1 FOR UPDATE`, session.ID)

		closeRequestID := uuid.New()
		startRequestID := uuid.New()
		var closeErr error
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, closeErr = sales.NewCloseServiceSessionHandler(env.Runner).Handle(
				ctx, env.Actor, sales.CloseServiceSessionCommand{
					RequestID:        closeRequestID,
					ServiceSessionID: session.ID,
				})
		}()
		waitForLockWaiters(t, env.DB, 1)

		startErr := reconciler.start(startRequestID)
		require.ErrorIs(t, startErr, shift.ErrActiveServiceSession,
			"the start must reject while the Session is still committed ACTIVE")
		assert.Equal(t, 0, countReconciliations(t, env.DB))

		holder.release(t)
		requireCorrectionRaceResolved(t, &wg)
		require.NoError(t, closeErr, "the closure commits once its Session lock returns")

		// With the Session closed, nothing blocks any more: the very same
		// start command commits CLOSING.
		require.NoError(t, reconciler.start(uuid.New()))
		assert.Equal(t, 1, countReconciliations(t, env.DB))
	})
}
