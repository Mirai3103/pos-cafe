//go:build integration

package shift_test

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/shift"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Closed-Shift history fixtures. History is a read over immutable closure
// rows, so the tests seed closed Shifts with direct SQL (the precedent the
// reconciliation seeding helpers set). Direct seeding is what makes two
// closures with an identical closed_at possible: the real close command
// stamps now() and could not produce a tie to walk a cursor across.

// History window and closure instants. The window covers every seeded row;
// two closures share tieClosedAt so the (closed_at, id) keyset tuple is
// exercised across a page boundary, and one closure sorts strictly newest.
var (
	historyWindowFrom = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	historyWindowTo   = time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)

	tieClosedAt  = time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC)
	soloClosedAt = time.Date(2026, 9, 3, 8, 0, 0, 0, time.UTC)
)

// closedShiftSeed is one closed Shift's variable facts. An empty Reason seeds
// an exact close; a Reason seeds the recount a nonzero difference requires
// plus one CASH discrepancy row whose expected term is the canonical 500000
// Opening Float.
type closedShiftSeed struct {
	OpenedAt     time.Time
	ClosedAt     time.Time
	ObservedCash int64
	Reason       shift.DiscrepancyReason
	Note         *string
}

// seedClosedShift inserts one CLOSED Shift with its reconciliation snapshot,
// its final evidence, its closure, and (when a reason is set) its discrepancy
// row. operator opens, starts, counts, observes, and closes; approver approves
// a discrepant close. The shift id and closure id are returned: the detail
// route is keyed by shift id and the ordering tuple is (closed_at, closure id).
func seedClosedShift(t *testing.T, db *sql.DB, operator, approver testActor,
	seed closedShiftSeed,
) (uuid.UUID, uuid.UUID) {
	t.Helper()

	const openingFloat, expectedCash = int64(500000), int64(500000)
	shiftID, closureID := uuid.New(), uuid.New()
	reconciliationID, observationID := uuid.New(), uuid.New()
	startedAt := seed.OpenedAt.Add(time.Hour)
	attemptAt := seed.ClosedAt.Add(-time.Minute)
	cashDifference := seed.ObservedCash - expectedCash

	require.NoError(t, db.QueryRow(`
		INSERT INTO sales_shifts (id, state, opened_by_staff_identity_id,
		                          opening_float_vnd, opened_at)
		VALUES ($1, 'CLOSED', $2, $3, $4)
		RETURNING id`,
		shiftID, operator.StaffID, openingFloat, seed.OpenedAt).Scan(&shiftID))

	require.NoError(t, db.QueryRow(`
		INSERT INTO shift_reconciliations (id, sales_shift_id,
		                                   started_by_staff_identity_id,
		                                   started_staff_access_session_id,
		                                   started_at, opening_float_vnd,
		                                   pay_in_vnd, pay_out_vnd,
		                                   cash_payment_vnd, cash_payment_void_vnd,
		                                   cash_refund_vnd, expected_cash_vnd,
		                                   manual_qr_payment_vnd,
		                                   manual_qr_payment_void_vnd,
		                                   expected_manual_qr_received_vnd,
		                                   manual_qr_refund_vnd)
		VALUES ($1, $2, $3, $4, $5, $6, 0, 0, 0, 0, 0, $7, 0, 0, 0, 0)
		RETURNING id`,
		reconciliationID, shiftID, operator.StaffID, operator.SessionID,
		startedAt, openingFloat, expectedCash).Scan(&reconciliationID))

	require.NoError(t, db.QueryRow(`
		INSERT INTO shift_qr_observations (id, reconciliation_id, sequence,
		                                   observed_received_vnd,
		                                   observed_refunded_vnd,
		                                   observed_by_staff_identity_id,
		                                   observed_staff_access_session_id,
		                                   observed_at)
		VALUES ($1, $2, 1, 0, 0, $3, $4, $5)
		RETURNING id`,
		observationID, reconciliationID, operator.StaffID, operator.SessionID,
		attemptAt).Scan(&observationID))

	// The blind initial count is always the frozen expectation. A discrepant
	// close additionally carries the sequence-2 recount the final evidence
	// rule demands, so the seeded state is one the API itself could produce.
	initialCountID := uuid.New()
	require.NoError(t, db.QueryRow(`
		INSERT INTO shift_cash_counts (id, reconciliation_id, sequence,
		                               counted_cash_vnd, counted_by_staff_identity_id,
		                               counted_staff_access_session_id, counted_at)
		VALUES ($1, $2, 1, $3, $4, $5, $6)
		RETURNING id`,
		initialCountID, reconciliationID, expectedCash, operator.StaffID,
		operator.SessionID, attemptAt).Scan(&initialCountID))
	finalCountID := initialCountID
	if cashDifference != 0 {
		finalCountID = uuid.New()
		require.NoError(t, db.QueryRow(`
			INSERT INTO shift_cash_counts (id, reconciliation_id, sequence,
			                               counted_cash_vnd, counted_by_staff_identity_id,
			                               counted_staff_access_session_id, counted_at)
			VALUES ($1, $2, 2, $3, $4, $5, $6)
			RETURNING id`,
			finalCountID, reconciliationID, seed.ObservedCash, operator.StaffID,
			operator.SessionID, attemptAt).Scan(&finalCountID))
	}

	if cashDifference == 0 {
		require.NoError(t, db.QueryRow(`
			INSERT INTO shift_closures (id, sales_shift_id, reconciliation_id,
			    initial_cash_count_id, final_cash_count_id, final_qr_observation_id,
			    opener_staff_identity_id, closer_staff_identity_id,
			    closer_staff_access_session_id, opened_at, closed_at,
			    opening_float_vnd, pay_in_vnd, pay_out_vnd, cash_payment_vnd,
			    cash_payment_void_vnd, cash_refund_vnd, expected_cash_vnd,
			    manual_qr_payment_vnd, manual_qr_payment_void_vnd,
			    expected_manual_qr_received_vnd, manual_qr_refund_vnd,
			    observed_cash_vnd, observed_manual_qr_received_vnd,
			    observed_manual_qr_refunded_vnd, cash_difference_vnd,
			    manual_qr_received_difference_vnd, manual_qr_refunded_difference_vnd)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11,
			        $12, 0, 0, 0, 0, 0, $13, 0, 0, 0, 0,
			        $14, 0, 0, $15, 0, 0)
			RETURNING id`,
			closureID, shiftID, reconciliationID, initialCountID, finalCountID,
			observationID, operator.StaffID, operator.StaffID, operator.SessionID,
			seed.OpenedAt, seed.ClosedAt,
			openingFloat, expectedCash, seed.ObservedCash, cashDifference).
			Scan(&closureID))
		return shiftID, closureID
	}

	// shift_closure_approval_difference_consistent pairs a nonzero difference
	// with a non-null approver.
	require.NoError(t, db.QueryRow(`
		INSERT INTO shift_closures (id, sales_shift_id, reconciliation_id,
		    initial_cash_count_id, final_cash_count_id, final_qr_observation_id,
		    opener_staff_identity_id, closer_staff_identity_id,
		    closer_staff_access_session_id, approved_by_staff_identity_id,
		    opened_at, closed_at, opening_float_vnd, pay_in_vnd, pay_out_vnd,
		    cash_payment_vnd, cash_payment_void_vnd, cash_refund_vnd,
		    expected_cash_vnd, manual_qr_payment_vnd, manual_qr_payment_void_vnd,
		    expected_manual_qr_received_vnd, manual_qr_refund_vnd,
		    observed_cash_vnd, observed_manual_qr_received_vnd,
		    observed_manual_qr_refunded_vnd, cash_difference_vnd,
		    manual_qr_received_difference_vnd, manual_qr_refunded_difference_vnd)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
		        $13, 0, 0, 0, 0, 0, $14, 0, 0, 0, 0,
		        $15, 0, 0, $16, 0, 0)
		RETURNING id`,
		closureID, shiftID, reconciliationID, initialCountID, finalCountID,
		observationID, operator.StaffID, operator.StaffID, operator.SessionID,
		approver.StaffID, seed.OpenedAt, seed.ClosedAt,
		openingFloat, expectedCash, seed.ObservedCash, cashDifference).
		Scan(&closureID))

	_, err := db.Exec(`
		INSERT INTO shift_discrepancies (shift_closure_id, dimension, expected_vnd,
		                                 observed_vnd, difference_vnd, reason, note)
		VALUES ($1, 'CASH', $2, $3, $4, $5, NULLIF(btrim($6), ''))`,
		closureID, expectedCash, seed.ObservedCash, cashDifference,
		string(seed.Reason), derefString(seed.Note))
	require.NoError(t, err)
	return shiftID, closureID
}

// derefString maps a nil note to the empty string the insert's NULLIF turns
// back into SQL NULL.
func derefString(note *string) string {
	if note == nil {
		return ""
	}
	return *note
}

// seedClosedShiftsBulk seeds n exact closed Shifts sharing one closed_at in a
// single statement. The cap test needs more than one page of 100 rows, and
// row-at-a-time seeding would issue ~500 statements where this CTE chain
// issues one, so the bulk insert is the cheap variant of "seed > 100 rows".
// Every row is an exact close (all differences zero, no approver), which
// satisfies every closure constraint.
func seedClosedShiftsBulk(t *testing.T, db *sql.DB, operator testActor, n int,
	openedAt, closedAt time.Time,
) {
	t.Helper()
	var seeded int
	require.NoError(t, db.QueryRow(`
		WITH ins_shift AS (
			INSERT INTO sales_shifts (state, opened_by_staff_identity_id,
			                          opening_float_vnd, opened_at)
			SELECT 'CLOSED', $1, 500000, $3
			FROM generate_series(1, $5::int)
			RETURNING id AS shift_id, opened_at
		),
		ins_recon AS (
			INSERT INTO shift_reconciliations (sales_shift_id,
			    started_by_staff_identity_id, started_staff_access_session_id,
			    started_at, opening_float_vnd, pay_in_vnd, pay_out_vnd,
			    cash_payment_vnd, cash_payment_void_vnd, cash_refund_vnd,
			    expected_cash_vnd, manual_qr_payment_vnd, manual_qr_payment_void_vnd,
			    expected_manual_qr_received_vnd, manual_qr_refund_vnd)
			SELECT shift_id, $1, $2, $3, 500000, 0, 0, 0, 0, 0, 500000, 0, 0, 0, 0
			FROM ins_shift
			RETURNING id AS recon_id, sales_shift_id
		),
		ins_count AS (
			INSERT INTO shift_cash_counts (reconciliation_id, sequence,
			    counted_cash_vnd, counted_by_staff_identity_id,
			    counted_staff_access_session_id, counted_at)
			SELECT recon_id, 1, 500000, $1, $2, $4
			FROM ins_recon
			RETURNING id AS count_id, reconciliation_id
		),
		ins_obs AS (
			INSERT INTO shift_qr_observations (reconciliation_id, sequence,
			    observed_received_vnd, observed_refunded_vnd,
			    observed_by_staff_identity_id, observed_staff_access_session_id,
			    observed_at)
			SELECT recon_id, 1, 0, 0, $1, $2, $4
			FROM ins_recon
			RETURNING id AS obs_id, reconciliation_id
		),
		ins_closure AS (
			INSERT INTO shift_closures (sales_shift_id, reconciliation_id,
			    initial_cash_count_id, final_cash_count_id, final_qr_observation_id,
			    opener_staff_identity_id, closer_staff_identity_id,
			    closer_staff_access_session_id, opened_at, closed_at,
			    opening_float_vnd, pay_in_vnd, pay_out_vnd, cash_payment_vnd,
			    cash_payment_void_vnd, cash_refund_vnd, expected_cash_vnd,
			    manual_qr_payment_vnd, manual_qr_payment_void_vnd,
			    expected_manual_qr_received_vnd, manual_qr_refund_vnd,
			    observed_cash_vnd, observed_manual_qr_received_vnd,
			    observed_manual_qr_refunded_vnd, cash_difference_vnd,
			    manual_qr_received_difference_vnd, manual_qr_refunded_difference_vnd)
			SELECT r.sales_shift_id, r.recon_id, c.count_id, c.count_id, o.obs_id,
			       $1, $1, $2, $3, $4,
			       500000, 0, 0, 0, 0, 0, 500000, 0, 0, 0, 0,
			       500000, 0, 0, 0, 0, 0
			FROM ins_recon r
			JOIN ins_count c ON c.reconciliation_id = r.recon_id
			JOIN ins_obs o ON o.reconciliation_id = r.recon_id
			RETURNING sales_shift_id
		)
		SELECT count(*) FROM ins_closure`,
		operator.StaffID, operator.SessionID, openedAt, closedAt, n).Scan(&seeded))
	require.Equal(t, n, seeded, "every requested closed snapshot is seeded")
}

// seedHistoryTie seeds the three closed Shifts every pagination test walks:
// two closures sharing tieClosedAt (one exact, one discrepant) and one
// strictly newer closure. It returns the seeded shift ids keyed to their
// closure ids.
func seedHistoryTie(t *testing.T, f shiftFixture) map[uuid.UUID]uuid.UUID {
	t.Helper()
	seeded := make(map[uuid.UUID]uuid.UUID)
	for _, seed := range []closedShiftSeed{
		{OpenedAt: tieClosedAt.Add(-8 * time.Hour), ClosedAt: tieClosedAt, ObservedCash: 500000},
		{OpenedAt: tieClosedAt.Add(-8 * time.Hour), ClosedAt: tieClosedAt,
			ObservedCash: 499000, Reason: shift.ReasonUnexplained},
		{OpenedAt: soloClosedAt.Add(-8 * time.Hour), ClosedAt: soloClosedAt, ObservedCash: 500000},
	} {
		shiftID, closureID := seedClosedShift(t, f.DB, f.Cashier, f.Manager, seed)
		seeded[shiftID] = closureID
	}
	require.Len(t, seeded, 3, "three distinct closed Shifts are seeded")
	return seeded
}

// expectedHistoryOrder sorts the seeded (shift id, closure id) pairs by the
// history key (closed_at DESC, id DESC): newest first, ties broken by the
// higher closure id, and returns the shift ids in that order.
func expectedHistoryOrder(t *testing.T, f shiftFixture, seeded map[uuid.UUID]uuid.UUID) []uuid.UUID {
	t.Helper()
	type keyed struct {
		shiftID   uuid.UUID
		closureID uuid.UUID
		closedAt  time.Time
	}
	rows := make([]keyed, 0, len(seeded))
	for shiftID, closureID := range seeded {
		var closedAt time.Time
		require.NoError(t, f.DB.QueryRow(
			`SELECT closed_at FROM shift_closures WHERE id = $1`, closureID).Scan(&closedAt))
		rows = append(rows, keyed{shiftID, closureID, closedAt})
	}
	slices.SortFunc(rows, func(a, b keyed) int {
		if c := b.closedAt.Compare(a.closedAt); c != 0 {
			return c
		}
		return strings.Compare(b.closureID.String(), a.closureID.String())
	})
	order := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		order = append(order, row.shiftID)
	}
	return order
}

// listCommand builds one history list query.
func listCommand(from, to time.Time, cursor string, limit int) shift.ListClosedShiftsQuery {
	return shift.ListClosedShiftsQuery{
		ClosedFrom: from,
		ClosedTo:   to,
		Cursor:     cursor,
		Limit:      limit,
	}
}

// limitLabel names a limit subtest.
func limitLabel(limit int) string {
	if limit == 1 {
		return "limit=1"
	}
	return "limit=2"
}

// TestListClosedShifts drives the closed-Shift history list: the descending
// keyset walk across a same-instant tie, the Manager-only capability, and the
// empty-window shape (spec 9.5, 9.6; ADR-052).
func TestListClosedShifts(t *testing.T) {
	ctx := context.Background()

	t.Run("manager walks descending pages without duplicate or missing rows", func(t *testing.T) {
		for _, limit := range []int{1, 2} {
			t.Run(limitLabel(limit), func(t *testing.T) {
				f := newShiftFixture(t)
				seeded := seedHistoryTie(t, f)
				want := expectedHistoryOrder(t, f, seeded)

				var collected []uuid.UUID
				cursor := ""
				for pages := 0; ; pages++ {
					require.Less(t, pages, 10, "the walk must terminate")
					res, err := shift.NewListClosedShiftsHandler(f.Runner).
						Handle(ctx, f.Manager.actor(),
							listCommand(historyWindowFrom, historyWindowTo, cursor, limit))
					require.NoError(t, err)
					require.NotNil(t, res.Items, "items serialize as [] never null")

					for _, item := range res.Items {
						collected = append(collected, item.ID)
					}
					if res.NextCursor == "" {
						break
					}
					cursor = res.NextCursor
				}

				assert.Equal(t, want, collected,
					"the walked pages reproduce the full descending (closed_at, id) order")
				assert.Len(t, collected, 3)
			})
		}
	})

	t.Run("cashier is forbidden", func(t *testing.T) {
		f := newShiftFixture(t)
		seedHistoryTie(t, f)

		_, err := shift.NewListClosedShiftsHandler(f.Runner).
			Handle(ctx, f.Cashier.actor(), listCommand(historyWindowFrom, historyWindowTo, "", 50))
		require.Error(t, err)
		requireCodedError(t, err, http.StatusForbidden, "FORBIDDEN")
	})

	t.Run("limit above the cap is clamped to 100", func(t *testing.T) {
		f := newShiftFixture(t)
		seedClosedShiftsBulk(t, f.DB, f.Cashier, 101,
			tieClosedAt.Add(-8*time.Hour), tieClosedAt)

		// A limit above the cap is accepted, never rejected: the page stops at
		// 100 rows and still reports a cursor to continue from (spec 9.5).
		first, err := shift.NewListClosedShiftsHandler(f.Runner).
			Handle(ctx, f.Manager.actor(), listCommand(historyWindowFrom, historyWindowTo, "", 250))
		require.NoError(t, err)
		require.Len(t, first.Items, 100, "a limit above the cap is clamped to 100 rows")
		require.NotEmpty(t, first.NextCursor, "a clamped full page still reports a next cursor")

		second, err := shift.NewListClosedShiftsHandler(f.Runner).
			Handle(ctx, f.Manager.actor(),
				listCommand(historyWindowFrom, historyWindowTo, first.NextCursor, 250))
		require.NoError(t, err)
		require.Len(t, second.Items, 1, "the row past the cap arrives on the next page")
		assert.Empty(t, second.NextCursor, "the walk is exhausted")

		// The clamped boundary neither duplicates nor omits a row.
		collected := make(map[uuid.UUID]struct{}, 101)
		for _, items := range [][]shift.ClosedShiftSummaryResponse{first.Items, second.Items} {
			for _, item := range items {
				collected[item.ID] = struct{}{}
			}
		}
		assert.Len(t, collected, 101)
	})

	t.Run("empty window returns an empty list and no cursor", func(t *testing.T) {
		f := newShiftFixture(t)

		res, err := shift.NewListClosedShiftsHandler(f.Runner).
			Handle(ctx, f.Manager.actor(), listCommand(historyWindowFrom, historyWindowTo, "", 50))
		require.NoError(t, err)
		require.NotNil(t, res.Items, "items serialize as [] never null")
		assert.Empty(t, res.Items)
		assert.Empty(t, res.NextCursor)
	})
}

// TestGetClosedShift drives the closed-Shift detail read: the immutable
// aggregate, the 404s that keep OPEN and CLOSING ids unexposed, and the
// Manager-only capability (spec 9.5, 9.6; ADR-052).
func TestGetClosedShift(t *testing.T) {
	ctx := context.Background()

	t.Run("manager reads a closed exact detail", func(t *testing.T) {
		f := newShiftFixture(t)
		shiftID, _ := seedClosedShift(t, f.DB, f.Cashier, f.Manager, closedShiftSeed{
			OpenedAt:     tieClosedAt.Add(-8 * time.Hour),
			ClosedAt:     tieClosedAt,
			ObservedCash: 500000,
		})

		res, err := shift.NewGetClosedShiftHandler(f.Runner).Handle(ctx, f.Manager.actor(), shiftID)
		require.NoError(t, err)

		assert.Equal(t, shiftID, res.ID)
		assert.Equal(t, f.Cashier.StaffID, res.Opener.ID)
		assert.Equal(t, f.Cashier.StaffID, res.Closer.ID)
		assert.Equal(t, f.Cashier.StaffID, res.Starter.ID)
		assert.Equal(t, int64(500000), res.OpeningFloatVND)
		assert.Equal(t, int64(500000), res.ExpectedCashVND)
		assert.Equal(t, int64(500000), res.ObservedCashVND)
		assert.Equal(t, int64(0), res.CashDifferenceVND)
		assert.False(t, res.HasDiscrepancy)
		require.NotNil(t, res.Discrepancies, "discrepancies serialize as [] never null")
		assert.Empty(t, res.Discrepancies)
		require.Len(t, res.CashCounts, 1)
		assert.Equal(t, 1, res.CashCounts[0].Sequence)
		require.Len(t, res.QRObservations, 1)
		assert.Nil(t, res.Approver)
		assert.False(t, res.ClosedAt.IsZero())

		// No credential field may cross the boundary (spec 9.6, 10).
		requireNoCredentialsInResponse(t, res)
	})

	t.Run("manager reads a discrepant closed detail", func(t *testing.T) {
		f := newShiftFixture(t)
		shiftID, _ := seedClosedShift(t, f.DB, f.Cashier, f.Manager, closedShiftSeed{
			OpenedAt:     tieClosedAt.Add(-8 * time.Hour),
			ClosedAt:     tieClosedAt,
			ObservedCash: 499000,
			Reason:       shift.ReasonUnexplained,
		})

		res, err := shift.NewGetClosedShiftHandler(f.Runner).Handle(ctx, f.Manager.actor(), shiftID)
		require.NoError(t, err)

		assert.True(t, res.HasDiscrepancy)
		assert.Equal(t, int64(-1000), res.CashDifferenceVND)
		require.Len(t, res.Discrepancies, 1)
		assert.Equal(t, dimCash, res.Discrepancies[0].Dimension)
		assert.Equal(t, reasonUnexplained, res.Discrepancies[0].Reason)
		assert.Equal(t, int64(500000), res.Discrepancies[0].ExpectedVND)
		assert.Equal(t, int64(499000), res.Discrepancies[0].ObservedVND)
		assert.Equal(t, int64(-1000), res.Discrepancies[0].DifferenceVND)
		require.NotNil(t, res.Approver)
		assert.Equal(t, f.Manager.StaffID, res.Approver.ID)

		// The discrepant seed carries the blind count and its recount.
		require.Len(t, res.CashCounts, 2)
		requireNoCredentialsInResponse(t, res)
	})

	t.Run("unknown id is not found", func(t *testing.T) {
		f := newShiftFixture(t)

		_, err := shift.NewGetClosedShiftHandler(f.Runner).
			Handle(ctx, f.Manager.actor(), uuid.New())
		require.Error(t, err)
		requireCodedError(t, err, http.StatusNotFound, "SALES_SHIFT_NOT_FOUND")
		assert.ErrorIs(t, err, shift.ErrSalesShiftNotFound)
	})

	t.Run("open shift id is not found", func(t *testing.T) {
		f := newShiftFixture(t)

		_, err := shift.NewGetClosedShiftHandler(f.Runner).
			Handle(ctx, f.Manager.actor(), f.Shift.ID)
		require.Error(t, err)
		requireCodedError(t, err, http.StatusNotFound, "SALES_SHIFT_NOT_FOUND")
	})

	t.Run("closing shift id is not found", func(t *testing.T) {
		f := newShiftFixture(t)
		startReconciliationExact(t, f)

		_, err := shift.NewGetClosedShiftHandler(f.Runner).
			Handle(ctx, f.Manager.actor(), f.Shift.ID)
		require.Error(t, err)
		requireCodedError(t, err, http.StatusNotFound, "SALES_SHIFT_NOT_FOUND")
	})
}

// TestShiftHTTPHistoryList drives the history list route end to end: route
// registration across /shifts, /shifts/current, and /shifts/:shift_id, the
// Manager-only middleware, the boundary validation of the window and the
// cursor, and the page shape (spec 9.5, 12).
func TestShiftHTTPHistoryList(t *testing.T) {
	e, q := newTestServer(t)
	managerToken, _ := signIn(t, e, q, []string{"MANAGER"}, "8642")
	cashierToken, _ := signIn(t, e, q, []string{"CASHIER"}, "2468")

	// Seed through the shared pool the test server reads.
	db, _ := openShiftTestDB(t)
	operator := newTestActor(t, q, []string{"CASHIER"}, true)
	approver := newTestActorWithPin(t, q, []string{"MANAGER"}, true, "8642")
	for _, seed := range []closedShiftSeed{
		{OpenedAt: tieClosedAt.Add(-8 * time.Hour), ClosedAt: tieClosedAt, ObservedCash: 500000},
		{OpenedAt: tieClosedAt.Add(-8 * time.Hour), ClosedAt: tieClosedAt,
			ObservedCash: 499000, Reason: shift.ReasonUnexplained},
		{OpenedAt: soloClosedAt.Add(-8 * time.Hour), ClosedAt: soloClosedAt, ObservedCash: 500000},
	} {
		seedClosedShift(t, db, operator, approver, seed)
	}
	var anyClosedShiftID uuid.UUID
	require.NoError(t, db.QueryRow(`SELECT sales_shift_id FROM shift_closures LIMIT 1`).
		Scan(&anyClosedShiftID))

	listPath := "/api/v1/shifts?closed_from=" + historyWindowFrom.Format(time.RFC3339) +
		"&closed_to=" + historyWindowTo.Format(time.RFC3339)

	t.Run("route order keeps current, list, and detail unambiguous", func(t *testing.T) {
		// All three GET shapes answer: /current (the static segment beats the
		// param), /shifts (the list itself), and /shifts/:shift_id (detail).
		rec := doRequest(t, e, http.MethodGet, "/api/v1/shifts/current", managerToken, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		rec = doRequest(t, e, http.MethodGet, listPath, managerToken, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		rec = doRequest(t, e, http.MethodGet, "/api/v1/shifts/"+anyClosedShiftID.String(), managerToken, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	})

	t.Run("manager lists descending summaries", func(t *testing.T) {
		rec := doRequest(t, e, http.MethodGet, listPath, managerToken, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		var env envelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
		var page shift.ClosedShiftListResponse
		require.NoError(t, json.Unmarshal(env.Data, &page))
		require.Len(t, page.Items, 3)

		assert.True(t, !page.Items[0].ClosedAt.Before(page.Items[1].ClosedAt),
			"summaries descend by closed_at")
		assert.True(t, !page.Items[1].ClosedAt.Before(page.Items[2].ClosedAt))

		// The two tie rows share one closed_at, so which of them sorts first
		// is decided by the random closure-id tiebreak: locate the discrepant
		// closure by its flag instead of by its page position.
		discrepant := 0
		for _, item := range page.Items {
			if item.HasDiscrepancy {
				discrepant++
				assert.Equal(t, int64(500000), item.ExpectedCashVND)
				assert.Equal(t, int64(499000), item.ObservedCashVND)
				assert.Equal(t, int64(-1000), item.CashDifferenceVND)
			}
		}
		assert.Equal(t, 1, discrepant, "exactly one seeded closure is discrepant")
	})

	t.Run("cashier is forbidden", func(t *testing.T) {
		rec := doRequest(t, e, http.MethodGet, listPath, cashierToken, nil)
		require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	})

	t.Run("anonymous is unauthorized", func(t *testing.T) {
		rec := doRequest(t, e, http.MethodGet, listPath, "", nil)
		require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
	})

	t.Run("boundary validation", func(t *testing.T) {
		// A genuine cursor minted over the full window proves the
		// different-range rejection is about the range, not the token.
		rangeA := listPath + "&limit=1"
		rec := doRequest(t, e, http.MethodGet, rangeA, managerToken, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var env envelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
		var page shift.ClosedShiftListResponse
		require.NoError(t, json.Unmarshal(env.Data, &page))
		require.NotEmpty(t, page.NextCursor)
		goodCursor := page.NextCursor

		cases := []struct {
			name string
			path string
		}{
			{
				name: "missing closed_from",
				path: "/api/v1/shifts?closed_to=" + historyWindowTo.Format(time.RFC3339),
			},
			{
				name: "missing closed_to",
				path: "/api/v1/shifts?closed_from=" + historyWindowFrom.Format(time.RFC3339),
			},
			{
				name: "malformed closed_from",
				path: "/api/v1/shifts?closed_from=not-an-instant&closed_to=" + historyWindowTo.Format(time.RFC3339),
			},
			{
				name: "range over 31 days",
				path: "/api/v1/shifts?closed_from=" + historyWindowFrom.Format(time.RFC3339) +
					"&closed_to=" + historyWindowFrom.Add(31*24*time.Hour+time.Second).Format(time.RFC3339),
			},
			{
				name: "inverted range",
				path: "/api/v1/shifts?closed_from=" + historyWindowTo.Format(time.RFC3339) +
					"&closed_to=" + historyWindowFrom.Format(time.RFC3339),
			},
			{
				name: "malformed cursor",
				path: listPath + "&cursor=garbage!",
			},
			{
				name: "cursor with a different range",
				path: "/api/v1/shifts?closed_from=" + historyWindowFrom.Format(time.RFC3339) +
					"&closed_to=" + historyWindowTo.Add(-24*time.Hour).Format(time.RFC3339) +
					"&cursor=" + goodCursor,
			},
			{
				name: "limit below one",
				path: listPath + "&limit=0",
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				rec := doRequest(t, e, http.MethodGet, tc.path, managerToken, nil)
				require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())

				var errEnv envelope
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errEnv))
				require.NotNil(t, errEnv.Error)
				assert.Equal(t, "INVALID_INPUT", errEnv.Error.Code)
			})
		}
	})

	t.Run("empty window serializes items as an empty array", func(t *testing.T) {
		empty := "/api/v1/shifts?closed_from=" + historyWindowTo.Format(time.RFC3339) +
			"&closed_to=" + historyWindowTo.Add(24*time.Hour).Format(time.RFC3339)
		rec := doRequest(t, e, http.MethodGet, empty, managerToken, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		var env envelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
		var page shift.ClosedShiftListResponse
		require.NoError(t, json.Unmarshal(env.Data, &page))
		require.NotNil(t, page.Items)
		assert.Empty(t, page.Items)
	})

	t.Run("cursor walk through the route returns every closed shift once", func(t *testing.T) {
		var collected []uuid.UUID
		seen := make(map[uuid.UUID]struct{})
		path := listPath + "&limit=2"
		for pages := 0; pages < 10; pages++ {
			rec := doRequest(t, e, http.MethodGet, path, managerToken, nil)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			var env envelope
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
			var parsed shift.ClosedShiftListResponse
			require.NoError(t, json.Unmarshal(env.Data, &parsed))
			for _, item := range parsed.Items {
				collected = append(collected, item.ID)
				seen[item.ID] = struct{}{}
			}
			if parsed.NextCursor == "" {
				break
			}
			path = listPath + "&limit=2&cursor=" + parsed.NextCursor
		}
		assert.Len(t, collected, 3, "the walk covers every closed Shift")
		assert.Len(t, seen, len(collected), "no page repeats a row")
	})
}

// TestShiftHTTPHistoryDetail drives the closed-Shift detail route end to end:
// the Manager-only middleware and the 404s that keep unknown, OPEN, and
// CLOSING ids unexposed (spec 9.5, 12).
func TestShiftHTTPHistoryDetail(t *testing.T) {
	e, q := newTestServer(t)
	managerToken, _ := signIn(t, e, q, []string{"MANAGER"}, "8642")
	cashierToken, _ := signIn(t, e, q, []string{"CASHIER"}, "2468")

	db, _ := openShiftTestDB(t)
	operator := newTestActor(t, q, []string{"CASHIER"}, true)
	approver := newTestActorWithPin(t, q, []string{"MANAGER"}, true, "8642")
	closedShiftID, _ := seedClosedShift(t, db, operator, approver, closedShiftSeed{
		OpenedAt:     tieClosedAt.Add(-8 * time.Hour),
		ClosedAt:     tieClosedAt,
		ObservedCash: 500000,
	})

	// One active Shift at a time (the one-active-Shift index allows no more):
	// it is first OPEN, then flipped to CLOSING with a reconciliation row, so
	// both states are proven invisible to the history detail because neither
	// has a closure row.
	var activeShiftID uuid.UUID
	require.NoError(t, db.QueryRow(`
		INSERT INTO sales_shifts (state, opened_by_staff_identity_id, opening_float_vnd)
		VALUES ('OPEN', $1, 500000) RETURNING id`, operator.StaffID).Scan(&activeShiftID))

	detailPath := func(id uuid.UUID) string { return "/api/v1/shifts/" + id.String() }

	t.Run("manager reads the closed detail", func(t *testing.T) {
		rec := doRequest(t, e, http.MethodGet, detailPath(closedShiftID), managerToken, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		var env envelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
		var detail map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(env.Data, &detail))
		for _, key := range []string{
			"id", "opener", "closer", "opened_at", "closed_at", "opening_float_vnd",
			"expected_cash_vnd", "observed_cash_vnd", "cash_difference_vnd",
			"expected_manual_qr_received_vnd", "observed_manual_qr_received_vnd",
			"manual_qr_received_difference_vnd",
			"expected_manual_qr_refunded_vnd", "observed_manual_qr_refunded_vnd",
			"manual_qr_refunded_difference_vnd", "has_discrepancy",
			"starter", "pay_in_vnd", "pay_out_vnd",
			"cash_payment_vnd", "cash_payment_void_vnd", "cash_refund_vnd",
			"manual_qr_payment_vnd", "manual_qr_payment_void_vnd",
			"pending_manual_qr_refund_vnd", "pending_refund_vnd",
			"unresolved_post_sale_adjustment_vnd",
			"cash_counts", "qr_observations", "discrepancies", "approver",
		} {
			assert.Contains(t, detail, key)
		}
		assert.JSONEq(t, "[]", string(detail["discrepancies"]))
		assert.JSONEq(t, "null", string(detail["approver"]))
	})

	t.Run("cashier is forbidden", func(t *testing.T) {
		rec := doRequest(t, e, http.MethodGet, detailPath(closedShiftID), cashierToken, nil)
		require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	})

	t.Run("unknown id is not found", func(t *testing.T) {
		rec := doRequest(t, e, http.MethodGet, detailPath(uuid.New()), managerToken, nil)
		require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())

		var errEnv envelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errEnv))
		require.NotNil(t, errEnv.Error)
		assert.Equal(t, "SALES_SHIFT_NOT_FOUND", errEnv.Error.Code)
	})

	t.Run("open and closing ids are not found", func(t *testing.T) {
		rec := doRequest(t, e, http.MethodGet, detailPath(activeShiftID), managerToken, nil)
		assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())

		// Flip the active Shift to CLOSING with a reconciliation snapshot; the
		// history detail still finds no closure row for it.
		_, err := db.Exec(`
			INSERT INTO shift_reconciliations (sales_shift_id, started_by_staff_identity_id,
			                                   started_staff_access_session_id,
			                                   opening_float_vnd, pay_in_vnd, pay_out_vnd,
			                                   cash_payment_vnd, cash_payment_void_vnd,
			                                   cash_refund_vnd, expected_cash_vnd,
			                                   manual_qr_payment_vnd,
			                                   manual_qr_payment_void_vnd,
			                                   expected_manual_qr_received_vnd,
			                                   manual_qr_refund_vnd)
			VALUES ($1, $2, $3, 500000, 0, 0, 0, 0, 0, 500000, 0, 0, 0, 0)`,
			activeShiftID, operator.StaffID, operator.SessionID)
		require.NoError(t, err)
		_, err = db.Exec(`UPDATE sales_shifts SET state = 'CLOSING' WHERE id = $1`, activeShiftID)
		require.NoError(t, err)

		rec = doRequest(t, e, http.MethodGet, detailPath(activeShiftID), managerToken, nil)
		assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	})
}

// TestDecodeClosedShiftCursor drives the exported cursor codec's rejections:
// garbage input, a wrong version, and a range mismatch are all INVALID_INPUT,
// while a genuine page cursor decodes to its position (spec 9.5).
func TestDecodeClosedShiftCursor(t *testing.T) {
	f := newShiftFixture(t)
	_, closureID := seedClosedShift(t, f.DB, f.Cashier, f.Manager, closedShiftSeed{
		OpenedAt:     tieClosedAt.Add(-8 * time.Hour),
		ClosedAt:     tieClosedAt,
		ObservedCash: 500000,
	})

	res, err := shift.NewListClosedShiftsHandler(f.Runner).
		Handle(context.Background(), f.Manager.actor(),
			listCommand(historyWindowFrom, historyWindowTo, "", 1))
	require.NoError(t, err)
	require.NotEmpty(t, res.NextCursor)

	t.Run("a genuine cursor decodes to its position", func(t *testing.T) {
		cursor, err := shift.DecodeClosedShiftCursor(res.NextCursor, historyWindowFrom, historyWindowTo)
		require.NoError(t, err)
		assert.True(t, cursor.ClosedAt.Equal(tieClosedAt))
		// The ordering tuple's id is the closure id, not the shift id.
		assert.Equal(t, closureID, cursor.ID)
	})

	t.Run("garbage is rejected", func(t *testing.T) {
		_, err := shift.DecodeClosedShiftCursor("%%%not-base64%%%", historyWindowFrom, historyWindowTo)
		require.Error(t, err)
		requireCodedError(t, err, http.StatusBadRequest, "INVALID_INPUT")
	})

	t.Run("a wrong version is rejected", func(t *testing.T) {
		payload, err := base64.RawURLEncoding.DecodeString(res.NextCursor)
		require.NoError(t, err)
		var fields map[string]any
		require.NoError(t, json.Unmarshal(payload, &fields))
		fields["v"] = 999
		tampered, err := json.Marshal(fields)
		require.NoError(t, err)
		token := base64.RawURLEncoding.EncodeToString(tampered)

		_, err = shift.DecodeClosedShiftCursor(token, historyWindowFrom, historyWindowTo)
		require.Error(t, err)
		requireCodedError(t, err, http.StatusBadRequest, "INVALID_INPUT")
	})

	t.Run("a range mismatch is rejected", func(t *testing.T) {
		_, err := shift.DecodeClosedShiftCursor(res.NextCursor,
			historyWindowFrom.Add(24*time.Hour), historyWindowTo)
		require.Error(t, err)
		requireCodedError(t, err, http.StatusBadRequest, "INVALID_INPUT")
	})

	t.Run("encode and decode round-trip one position", func(t *testing.T) {
		want := shift.ClosedShiftCursor{
			Version:    1,
			ClosedAt:   soloClosedAt,
			ID:         closureID,
			ClosedFrom: historyWindowFrom,
			ClosedTo:   historyWindowTo,
		}
		token, err := shift.EncodeClosedShiftCursor(want)
		require.NoError(t, err)
		require.NotContains(t, token, "=", "the encoding is unpadded base64url")

		got, err := shift.DecodeClosedShiftCursor(token, historyWindowFrom, historyWindowTo)
		require.NoError(t, err)
		assert.True(t, got.ClosedAt.Equal(want.ClosedAt))
		assert.Equal(t, want.ID, got.ID)
		assert.True(t, got.ClosedFrom.Equal(want.ClosedFrom))
		assert.True(t, got.ClosedTo.Equal(want.ClosedTo))
	})
}
