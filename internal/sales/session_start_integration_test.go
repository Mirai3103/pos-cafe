//go:build integration

package sales_test

import (
	"context"
	"database/sql"
	"net/http"
	"sync"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/Mirai3103/pos-cafe/internal/sales"
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
