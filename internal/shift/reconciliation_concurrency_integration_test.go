//go:build integration

package shift_test

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/shift"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Deterministic lock orchestration for the Shift slice's reconciliation races
// (spec 11.2, 14.4). Every race takes a known row lock in one test-owned
// transaction, launches the handlers behind a start barrier, polls
// pg_stat_activity's own lock-wait state until the handlers are demonstrably
// parked, releases the lock, and collects exactly one result per handler. No
// sleep creates or decides a race; the database is the synchronization.

// raceRowLock is one test-owned row lock. holdRowLock opens a transaction and
// locks the row every racing command must eventually take, so the test can
// launch the handlers behind the start barrier and know they are parked at
// that exact lock boundary before releasing them. It is the test-side
// substitute for a production test seam: no production code is aware of it.
type raceRowLock struct {
	tx     *sql.Tx
	locked bool
}

// holdRowLock acquires one row FOR UPDATE on the caller's behalf. The query
// must be a single-row SELECT ... FOR UPDATE naming the row the racing
// commands serialize on. The lock is rolled back by cleanup unless release
// commits it first, so a failing test can never strand the row and block the
// next test's truncation.
func holdRowLock(t *testing.T, db *sql.DB, query string, args ...any) *raceRowLock {
	t.Helper()
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	var id string
	require.NoError(t, tx.QueryRowContext(ctx, query, args...).Scan(&id),
		"the lock-holder row must exist")
	holder := &raceRowLock{tx: tx, locked: true}
	t.Cleanup(func() {
		if holder.locked {
			_ = holder.tx.Rollback()
		}
	})
	return holder
}

// release commits the holder, waking every command parked on the held row.
func (h *raceRowLock) release(t *testing.T) {
	t.Helper()
	require.True(t, h.locked, "the race lock was already released")
	h.locked = false
	require.NoError(t, h.tx.Commit())
}

// waitForBlockedQuery polls pg_stat_activity until a backend of this test
// database is parked on a lock while running a query whose text contains the
// given fragment. The sqlc name comments are part of the statement text
// PostgreSQL reports, so the fragment names the exact parked statement —
// evidence that the launched handler, not some other backend, is the waiter.
// require.Eventually supplies the deadline; the poll interval only paces it,
// and no sleep decides any outcome.
func waitForBlockedQuery(t *testing.T, db *sql.DB, queryFragment string) {
	t.Helper()
	require.Eventually(t, func() bool {
		var waiting bool
		err := db.QueryRow(`
			SELECT EXISTS (
				SELECT 1
				FROM pg_stat_activity
				WHERE datname = current_database()
				  AND pid <> pg_backend_pid()
				  AND wait_event_type = 'Lock'
				  AND query LIKE '%' || $1 || '%'
			)`, queryFragment).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond)
}

// waitForLockWaiters polls until want backends of this test database are
// parked on a lock. Tests in a package run sequentially, so during a race the
// only lock waiters are the racing handlers.
func waitForLockWaiters(t *testing.T, db *sql.DB, want int) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	seen := 0
	for time.Now().Before(deadline) {
		require.NoError(t, db.QueryRow(`
			SELECT count(*)
			FROM pg_stat_activity
			WHERE datname = current_database()
			  AND pid <> pg_backend_pid()
			  AND wait_event_type = 'Lock'`).Scan(&seen))
		if seen >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected %d backend(s) parked on a lock, saw %d after 15s", want, seen)
}

// requireRaceResolved waits for a synchronized race with a hard upper bound,
// so a blocked pair fails the test rather than hanging it.
func requireRaceResolved(t *testing.T, wg *sync.WaitGroup) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the synchronized race never resolved: a transaction is still blocked")
	}
}

// countIdempotencyClaims counts the idempotency records one actor carries for
// one request id. A rolled-back command claims nothing.
func countIdempotencyClaims(t *testing.T, db *sql.DB, actorID, requestID uuid.UUID) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(
		`SELECT count(*) FROM idempotency_keys WHERE actor_id = $1 AND key = $2`,
		actorID, requestID).Scan(&n))
	return n
}

// TestReconciliationConcurrency pins the Shift-side race matrix of spec 11.2
// and 14.4. Every subtest releases the held Shift row only after
// pg_stat_activity proves the handlers are parked on it, so the collision
// happens at the lock boundary rather than at the mercy of the scheduler, and
// each branch asserts the accepted outcome's persisted facts — never merely
// "no error and no panic".
func TestShiftReconciliationConcurrentRaces(t *testing.T) {
	ctx := context.Background()

	t.Run("two starts with different actors and request ids", func(t *testing.T) {
		f := newShiftFixture(t)
		start := shift.NewStartReconciliationHandler(f.Runner)

		// The Shift row is the lock both starts must take (spec 11.1). The
		// advisory duplicate lock differs per actor/request pair, so both
		// reaches park on the row itself.
		holder := holdRowLock(t, f.DB,
			`SELECT id::text FROM sales_shifts WHERE id = $1 FOR UPDATE`, f.Shift.ID)

		type startOutcome struct {
			actor     testActor
			requestID uuid.UUID
			status    int
			res       shift.ClosingShiftResponse
			err       error
		}
		results := make(chan startOutcome, 2)
		var wg sync.WaitGroup
		startBarrier := make(chan struct{})
		for _, actor := range []testActor{f.Cashier, f.Manager} {
			requestID := uuid.New()
			wg.Add(1)
			go func(a testActor, rid uuid.UUID) {
				defer wg.Done()
				<-startBarrier
				status, res, err := start.Handle(ctx, a.actor(), f.startCommand(500_000, rid))
				results <- startOutcome{actor: a, requestID: rid, status: status, res: res, err: err}
			}(actor, requestID)
		}
		close(startBarrier)
		waitForLockWaiters(t, f.DB, 2)
		holder.release(t)
		requireRaceResolved(t, &wg)
		close(results)

		// Two independent starts: one commits CLOSING, the other reads
		// CLOSING under the lock and reports the stable conflict (spec 11.2).
		wins, losses := 0, 0
		var winner startOutcome
		var loser startOutcome
		for r := range results {
			if r.err == nil {
				wins++
				assert.Equal(t, 201, r.status)
				assert.Equal(t, shift.StateClosing, r.res.State)
				winner = r
			} else {
				losses++
				require.ErrorIs(t, r.err, shift.ErrShiftAlreadyClosing)
				requireCodedError(t, r.err, 409, "SALES_SHIFT_ALREADY_CLOSING")
				loser = r
			}
		}
		require.Equal(t, 1, wins, "exactly one independent start commits CLOSING")
		require.Equal(t, 1, losses, "the second start loses under the Shift row lock")

		// Exactly one snapshot, one sequence-1 count, and one of each event —
		// the loser wrote nothing.
		var reconRows, countRows int
		require.NoError(t, f.DB.QueryRow(
			`SELECT count(*) FROM shift_reconciliations WHERE sales_shift_id = $1`,
			f.Shift.ID).Scan(&reconRows))
		require.NoError(t, f.DB.QueryRow(
			`SELECT count(*) FROM shift_cash_counts`).Scan(&countRows))
		assert.Equal(t, 1, reconRows, "one reconciliation row for the shift")
		assert.Equal(t, 1, countRows, "one blind initial count")
		assert.Equal(t, 1, countAuditEvents(t, f.DB, shift.EventReconciliationStarted, f.Shift.ID))
		assert.Equal(t, 1, countAuditEvents(t, f.DB, shift.EventCashCountRecorded, f.Shift.ID))
		assert.Equal(t, 1, countIdempotencyClaims(t, f.DB, winner.actor.StaffID, winner.requestID),
			"the winning start stores its replayable result")
		assert.Equal(t, 0, countIdempotencyClaims(t, f.DB, loser.actor.StaffID, loser.requestID),
			"the losing start rolls its claim back")
	})

	t.Run("cash movement versus start", func(t *testing.T) {
		f := newShiftFixture(t)
		start := shift.NewStartReconciliationHandler(f.Runner)

		movementCmd := f.command(shift.MethodPayOut, shift.ReasonSafeDrop, 50_000, nil)
		startRequestID := uuid.New()
		startCmd := f.startCommand(450_000, startRequestID)

		// The movement locks the Shift FOR UPDATE (its OPEN precondition) and
		// the start locks it FOR UPDATE too, so the held row serializes them:
		// the movement either commits before the snapshot (Pay Out frozen into
		// Expected Cash) or after the state transition (rejected).
		holder := holdRowLock(t, f.DB,
			`SELECT id::text FROM sales_shifts WHERE id = $1 FOR UPDATE`, f.Shift.ID)

		var moveErr error
		var startErr error
		var started shift.ClosingShiftResponse
		var wg sync.WaitGroup
		startBarrier := make(chan struct{})
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-startBarrier
			_, _, moveErr = f.Movement.Handle(ctx, f.Cashier.actor(), movementCmd)
		}()
		go func() {
			defer wg.Done()
			<-startBarrier
			_, started, startErr = start.Handle(ctx, f.Cashier.actor(), startCmd)
		}()
		close(startBarrier)
		// The two commands park on different statements; prove both before
		// releasing.
		waitForBlockedQuery(t, f.DB, "GetOpenSalesShiftForUpdate")
		waitForBlockedQuery(t, f.DB, "LockSalesShiftForReconciliation")
		holder.release(t)
		requireRaceResolved(t, &wg)

		// Whichever handler wins the row's grant queue, the outcome is one of
		// the two accepted ones (spec 11.1, 11.2): the movement commits before
		// the snapshot and is frozen into Expected Cash, or it waits and is
		// wholly rejected once the Shift is no longer OPEN.
		require.NoError(t, startErr, "the start itself must always succeed once scheduled")
		var movements int
		require.NoError(t, f.DB.QueryRow(
			`SELECT count(*) FROM cash_movements WHERE sales_shift_id = $1`,
			f.Shift.ID).Scan(&movements))
		if moveErr == nil {
			assert.Equal(t, int64(450_000), started.Reconciliation.ExpectedCashVND,
				"500000 float less the 50000 Pay Out, frozen at start")
			assert.Equal(t, 1, movements, "the committed movement stands beside the snapshot")
		} else {
			require.ErrorIs(t, moveErr, shift.ErrOpenShiftRequired,
				"the losing movement must reject on its OPEN precondition")
			requireCodedError(t, moveErr, 409, "OPEN_SALES_SHIFT_REQUIRED")
			assert.Equal(t, int64(500_000), started.Reconciliation.ExpectedCashVND,
				"the snapshot froze no movement")
			assert.Equal(t, 0, movements, "the rejected movement wrote nothing")
		}
		assert.Equal(t, moveErr == nil,
			countIdempotencyClaims(t, f.DB, f.Cashier.StaffID, movementCmd.RequestID) == 1,
			"a rejected movement rolls its claim back")
		assert.Equal(t, 1, countIdempotencyClaims(t, f.DB, f.Cashier.StaffID, startRequestID))
	})

	t.Run("append attempt versus final close", func(t *testing.T) {
		f := newShiftFixture(t)
		started := startReconciliationExact(t, f)
		observation := appendQRObservation(t, f, 0, 0)
		initial := started.Reconciliation.CashCounts[0]
		closes := shift.NewCloseShiftHandler(f.Runner)
		counts := shift.NewRecordCashCountHandler(f.Runner)

		closeRequestID := uuid.New()
		closeCmd := f.closeCommand(closeRequestID, initial.ID, observation.ID,
			[]shift.CloseDiscrepancyInput{}, "", "")
		countRequestID := uuid.New()
		countCmd := f.cashCountCommand(499_000, countRequestID)

		// Both commands lock the CLOSING Shift FOR UPDATE, so the held row
		// serializes them (spec 11.2): if the append wins, the close's
		// supplied count id is stale; if the close wins, the append sees
		// CLOSED.
		holder := holdRowLock(t, f.DB,
			`SELECT id::text FROM sales_shifts WHERE id = $1 FOR UPDATE`, f.Shift.ID)

		var countErr error
		var closeErr error
		var closeStatus int
		var wg sync.WaitGroup
		startBarrier := make(chan struct{})
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-startBarrier
			_, _, countErr = counts.Handle(ctx, f.Cashier.actor(), countCmd)
		}()
		go func() {
			defer wg.Done()
			<-startBarrier
			closeStatus, _, closeErr = closes.Handle(ctx, f.Cashier.actor(), closeCmd)
		}()
		close(startBarrier)
		waitForLockWaiters(t, f.DB, 2)
		holder.release(t)
		requireRaceResolved(t, &wg)

		if countErr == nil {
			// The append won: the close's final count is no longer the latest
			// evidence, so the close refuses as stale and nothing persisted.
			require.ErrorIs(t, closeErr, shift.ErrReconciliationStale)
			requireCodedError(t, closeErr, 409, "SHIFT_RECONCILIATION_STALE")
			assert.Equal(t, shift.StateClosing, requireShiftState(t, f))
			requireShiftClosureCounts(t, f, 0, 0)

			var countRows int
			require.NoError(t, f.DB.QueryRow(
				`SELECT count(*) FROM shift_cash_counts`).Scan(&countRows))
			assert.Equal(t, 2, countRows, "the recount stands beside the initial count")
			assert.Equal(t, 1, countIdempotencyClaims(t, f.DB, f.Cashier.StaffID, countRequestID))
			assert.Equal(t, 0, countIdempotencyClaims(t, f.DB, f.Cashier.StaffID, closeRequestID),
				"the stale close rolls its claim back")
		} else {
			// The close won: the append reads CLOSED and reports the
			// second-close conflict, appending nothing.
			require.ErrorIs(t, countErr, shift.ErrShiftAlreadyClosed)
			requireCodedError(t, countErr, 409, "SALES_SHIFT_ALREADY_CLOSED")
			require.NoError(t, closeErr)
			assert.Equal(t, 200, closeStatus)
			assert.Equal(t, shift.StateClosed, requireShiftState(t, f))
			requireShiftClosureCounts(t, f, 1, 0)

			var countRows int
			require.NoError(t, f.DB.QueryRow(
				`SELECT count(*) FROM shift_cash_counts`).Scan(&countRows))
			assert.Equal(t, 1, countRows, "the losing append stored no recount")
			assert.Equal(t, 0, countIdempotencyClaims(t, f.DB, f.Cashier.StaffID, countRequestID),
				"the losing append rolls its claim back")
			assert.Equal(t, 1, countIdempotencyClaims(t, f.DB, f.Cashier.StaffID, closeRequestID))
		}
	})

	t.Run("two closes with different actors and request ids", func(t *testing.T) {
		f := newShiftFixture(t)
		cashID, qrID := shortageEvidence(t, f)
		closes := shift.NewCloseShiftHandler(f.Runner)

		type closeOutcome struct {
			actor     testActor
			requestID uuid.UUID
			status    int
			err       error
		}
		results := make(chan closeOutcome, 2)
		var wg sync.WaitGroup
		startBarrier := make(chan struct{})

		// Hold the CLOSING Shift's row before launching, so both closers park
		// on it and the release grants them in queue order.
		holder := holdRowLock(t, f.DB,
			`SELECT id::text FROM sales_shifts WHERE id = $1 FOR UPDATE`, f.Shift.ID)
		for _, actor := range []testActor{f.Cashier, f.Manager} {
			requestID := uuid.New()
			wg.Add(1)
			go func(a testActor, rid uuid.UUID) {
				defer wg.Done()
				<-startBarrier
				// Both closers name the Manager approval pair; the Manager's
				// own close is a self-approval (spec 7.10).
				cmd := f.closeCommand(rid, cashID, qrID,
					[]shift.CloseDiscrepancyInput{reasonInput(shift.DimensionCash, shift.ReasonUnexplained)},
					f.Manager.LoginCode, f.Manager.Pin)
				status, _, err := closes.Handle(ctx, a.actor(), cmd)
				results <- closeOutcome{actor: a, requestID: rid, status: status, err: err}
			}(actor, requestID)
		}
		close(startBarrier)
		waitForLockWaiters(t, f.DB, 2)
		holder.release(t)
		requireRaceResolved(t, &wg)
		close(results)

		wins, losses := 0, 0
		var winner closeOutcome
		var loser closeOutcome
		for r := range results {
			if r.err == nil {
				wins++
				assert.Equal(t, 200, r.status)
				winner = r
			} else {
				losses++
				require.ErrorIs(t, r.err, shift.ErrShiftAlreadyClosed)
				requireCodedError(t, r.err, 409, "SALES_SHIFT_ALREADY_CLOSED")
				loser = r
			}
		}
		require.Equal(t, 1, wins, "one snapshot wins")
		require.Equal(t, 1, losses, "the other returns already closed")

		assert.Equal(t, shift.StateClosed, requireShiftState(t, f))
		requireShiftClosureCounts(t, f, 1, 1)
		assert.Equal(t, 1, countAuditEvents(t, f.DB, shift.EventShiftClosedWithDiscrepancy, f.Shift.ID))
		assert.Equal(t, 1, countIdempotencyClaims(t, f.DB, winner.actor.StaffID, winner.requestID))
		assert.Equal(t, 0, countIdempotencyClaims(t, f.DB, loser.actor.StaffID, loser.requestID),
			"the losing close rolls its claim back")
	})

	t.Run("open new shift versus final close", func(t *testing.T) {
		f := newShiftFixture(t)
		startReconciliationExact(t, f)
		observation := appendQRObservation(t, f, 0, 0)
		closes := shift.NewCloseShiftHandler(f.Runner)
		opens := shift.NewOpenShiftHandler(f.Runner)

		closeCmd := f.closeCommand(uuid.New(), latestCashCountID(t, f), observation.ID,
			[]shift.CloseDiscrepancyInput{}, "", "")

		t.Run("an open ordered before the closure commits conflicts", func(t *testing.T) {
			// Hold the CLOSING Shift's row so the close parks at its lock,
			// then open in the test goroutine: the active-Shift unique index
			// sees the still-committed CLOSING row and refuses the overlap
			// (spec 11.2).
			holder := holdRowLock(t, f.DB,
				`SELECT id::text FROM sales_shifts WHERE id = $1 FOR UPDATE`, f.Shift.ID)

			var closeErr error
			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _, closeErr = closes.Handle(ctx, f.Cashier.actor(), closeCmd)
			}()
			waitForBlockedQuery(t, f.DB, "LockSalesShiftForReconciliation")

			_, _, openErr := opens.Handle(ctx, f.Cashier.actor(),
				shift.OpenShiftCommand{RequestID: uuid.New(), OpeningFloatVND: int64Ptr(500000)})
			require.ErrorIs(t, openErr, shift.ErrShiftAlreadyOpen)
			requireCodedError(t, openErr, 409, "SALES_SHIFT_ALREADY_OPEN")

			holder.release(t)
			requireRaceResolved(t, &wg)
			require.NoError(t, closeErr)
			assert.Equal(t, shift.StateClosed, requireShiftState(t, f))
		})

		t.Run("an open ordered after the closure creates the next shift", func(t *testing.T) {
			status, opened, err := opens.Handle(ctx, f.Cashier.actor(),
				shift.OpenShiftCommand{RequestID: uuid.New(), OpeningFloatVND: int64Ptr(500000)})
			require.NoError(t, err)
			assert.Equal(t, 201, status)
			assert.Equal(t, shift.StateOpen, opened.State)
			assert.NotEqual(t, f.Shift.ID, opened.ID)

			var openShifts int
			require.NoError(t, f.DB.QueryRow(
				`SELECT count(*) FROM sales_shifts WHERE state = 'OPEN'`).Scan(&openShifts))
			assert.Equal(t, 1, openShifts, "exactly one OPEN shift exists after the closure")
		})
	})
}
