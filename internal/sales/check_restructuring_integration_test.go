//go:build integration

package sales_test

import (
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSplitCheck(t *testing.T) {
	env := newSalesEnv(t)

	t.Run("splitting onto a new check divides the charge", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 2)
		sourceID := env.soleCheckID(t, session.ID)
		originalCharge := env.checkCharge(t, sourceID)
		allocations := env.checkAllocations(t, session.ID, sourceID)

		got, _, err := env.splitToNewCheck(t, sourceID, []sales.SplitItem{
			{CommittedItemID: allocations[0].CommittedItemID, Quantity: allocations[0].AllocatedQuantity},
		})
		require.NoError(t, err)

		require.Len(t, got.Checks, 2)
		var total int64
		for _, check := range got.Checks {
			require.Equal(t, sales.CheckStateOpen, check.State)
			require.Greater(t, check.ChargeVND, int64(0))
			total += check.ChargeVND
		}
		require.Equal(t, originalCharge, total,
			"splitting must conserve the total charge")
	})

	t.Run("splitting onto an existing check combines the allocation", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 2)
		sourceID := env.soleCheckID(t, session.ID)
		allocations := env.checkAllocations(t, session.ID, sourceID)
		// The fixture commits its first item at quantity 2, but the
		// projection orders allocations by (committed_at, id) and commit
		// stamps every item with one timestamp, so a random uuid decides
		// the tie and index 0 is not reliably that item.
		first := allocations[0]
		for _, alloc := range allocations {
			if alloc.AllocatedQuantity >= 2 {
				first = alloc
				break
			}
		}
		require.GreaterOrEqual(t, first.AllocatedQuantity, int32(2),
			"fixture must commit at least two units of the first item")

		// First split creates the destination with one unit.
		got, _, err := env.splitToNewCheck(t, sourceID, []sales.SplitItem{
			{CommittedItemID: first.CommittedItemID, Quantity: 1},
		})
		require.NoError(t, err)
		destinationID := env.otherCheckID(t, got, sourceID)

		// Second split moves another unit of the same item onto it.
		got, _, err = env.splitToExistingCheck(t, sourceID, destinationID, []sales.SplitItem{
			{CommittedItemID: first.CommittedItemID, Quantity: 1},
		})
		require.NoError(t, err)

		destination := env.findCheck(t, got, destinationID)
		require.Len(t, destination.Allocations, 1,
			"the same committed item must combine into one allocation, not two")
		require.Equal(t, int32(2), destination.Allocations[0].AllocatedQuantity)
	})

	t.Run("splitting the whole source is rejected", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 1)
		sourceID := env.soleCheckID(t, session.ID)
		allocations := env.checkAllocations(t, session.ID, sourceID)

		_, _, err := env.splitToNewCheck(t, sourceID, []sales.SplitItem{
			{CommittedItemID: allocations[0].CommittedItemID, Quantity: allocations[0].AllocatedQuantity},
		})
		require.ErrorIs(t, err, sales.ErrSplitSourceWouldBeEmpty)
	})

	t.Run("a quantity above the allocation is rejected", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 2)
		sourceID := env.soleCheckID(t, session.ID)
		allocations := env.checkAllocations(t, session.ID, sourceID)

		_, _, err := env.splitToNewCheck(t, sourceID, []sales.SplitItem{
			{CommittedItemID: allocations[0].CommittedItemID, Quantity: allocations[0].AllocatedQuantity + 1},
		})
		require.ErrorIs(t, err, sales.ErrSplitQuantityExceedsAllocation)
	})

	t.Run("an item not on the source is rejected", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 2)
		sourceID := env.soleCheckID(t, session.ID)

		_, _, err := env.splitToNewCheck(t, sourceID, []sales.SplitItem{
			{CommittedItemID: uuid.New(), Quantity: 1},
		})
		require.ErrorIs(t, err, sales.ErrSplitAllocationNotFound)
	})

	t.Run("a paid check cannot be split", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 2)
		sourceID := env.soleCheckID(t, session.ID)
		allocations := env.checkAllocations(t, session.ID, sourceID)

		_, _, err := env.payCash(t, sourceID, 1_000, 1_000)
		require.NoError(t, err)

		_, _, err = env.splitToNewCheck(t, sourceID, []sales.SplitItem{
			{CommittedItemID: allocations[0].CommittedItemID, Quantity: 1},
		})
		require.ErrorIs(t, err, sales.ErrCheckHasPayment)
	})

	t.Run("the charge invariant survives the split", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 2)
		sourceID := env.soleCheckID(t, session.ID)
		allocations := env.checkAllocations(t, session.ID, sourceID)

		got, _, err := env.splitToNewCheck(t, sourceID, []sales.SplitItem{
			{CommittedItemID: allocations[0].CommittedItemID, Quantity: 1},
		})
		require.NoError(t, err)

		// A second read re-runs the projection's stored-charge comparison,
		// which is the invariant itself.
		reread := env.GetSessionOK(t, session.ID)
		require.Len(t, reread.Checks, len(got.Checks))
	})
}

func TestMergeChecks(t *testing.T) {
	env := newSalesEnv(t)

	// Merge is reachable through the public API in 5C: splitting creates the
	// second Check that merging then absorbs. No fixture is seeded.
	splitThenTwoChecks := func(t *testing.T) (sessionID, survivingID, absorbedID uuid.UUID, total int64) {
		t.Helper()
		session := env.commitTakeawayDraft(t, 2)
		sourceID := env.soleCheckID(t, session.ID)
		total = env.checkCharge(t, sourceID)
		allocations := env.checkAllocations(t, session.ID, sourceID)

		got, _, err := env.splitToNewCheck(t, sourceID, []sales.SplitItem{
			{CommittedItemID: allocations[0].CommittedItemID, Quantity: 1},
		})
		require.NoError(t, err)
		return session.ID, sourceID, env.otherCheckID(t, got, sourceID), total
	}

	t.Run("merging returns the whole charge to the survivor", func(t *testing.T) {
		sessionID, survivingID, absorbedID, total := splitThenTwoChecks(t)

		got, _, err := env.mergeChecks(t, survivingID, absorbedID)
		require.NoError(t, err)

		surviving := env.findCheck(t, got, survivingID)
		absorbed := env.findCheck(t, got, absorbedID)
		require.Equal(t, sales.CheckStateOpen, surviving.State)
		require.Equal(t, total, surviving.ChargeVND)
		require.Equal(t, sales.CheckStateMerged, absorbed.State)
		require.Equal(t, int64(0), absorbed.ChargeVND)
		require.NotNil(t, absorbed.MergedIntoCheckID)
		require.Equal(t, survivingID, *absorbed.MergedIntoCheckID)
		require.Empty(t, absorbed.Allocations)
		_ = sessionID
	})

	t.Run("overlapping allocations combine into one row", func(t *testing.T) {
		_, survivingID, absorbedID, _ := splitThenTwoChecks(t)

		got, _, err := env.mergeChecks(t, survivingID, absorbedID)
		require.NoError(t, err)

		surviving := env.findCheck(t, got, survivingID)
		seen := make(map[uuid.UUID]int)
		for _, allocation := range surviving.Allocations {
			seen[allocation.CommittedItemID]++
		}
		for item, n := range seen {
			require.Equal(t, 1, n, "committed item %s has more than one allocation", item)
		}
	})

	t.Run("merging a check into itself is rejected", func(t *testing.T) {
		_, survivingID, _, _ := splitThenTwoChecks(t)

		_, _, err := env.mergeChecks(t, survivingID, survivingID)
		require.ErrorIs(t, err, sales.ErrInvalidCheckMerge)
	})

	t.Run("a paid check cannot be merged", func(t *testing.T) {
		_, survivingID, absorbedID, _ := splitThenTwoChecks(t)

		_, _, err := env.payCash(t, survivingID, 1_000, 1_000)
		require.NoError(t, err)

		_, _, err = env.mergeChecks(t, survivingID, absorbedID)
		require.ErrorIs(t, err, sales.ErrCheckHasPayment)
	})

	t.Run("a merged check cannot be merged again", func(t *testing.T) {
		_, survivingID, absorbedID, _ := splitThenTwoChecks(t)

		_, _, err := env.mergeChecks(t, survivingID, absorbedID)
		require.NoError(t, err)

		_, _, err = env.mergeChecks(t, survivingID, absorbedID)
		require.ErrorIs(t, err, sales.ErrCheckNotOpen)
	})

	t.Run("checks of different sessions cannot be merged", func(t *testing.T) {
		_, survivingID, _, _ := splitThenTwoChecks(t)
		otherSession := env.commitTakeawayDraft(t, 1)
		foreignID := env.soleCheckID(t, otherSession.ID)

		_, _, err := env.mergeChecks(t, survivingID, foreignID)
		require.ErrorIs(t, err, sales.ErrChecksDifferentSession)
	})

	t.Run("an unknown check reports CHECK_NOT_FOUND", func(t *testing.T) {
		_, survivingID, _, _ := splitThenTwoChecks(t)

		_, _, err := env.mergeChecks(t, survivingID, uuid.New())
		require.ErrorIs(t, err, sales.ErrCheckNotFound)
	})
}

// restructuringSnapshot captures every Check and Charge Allocation row of one
// Session, so a rejected Split or Merge can be proved to have rewritten none
// of them.
type restructuringSnapshot struct {
	Checks      []restructuringCheckRow
	Allocations []restructuringAllocationRow
}

type restructuringCheckRow struct {
	ID                uuid.UUID
	State             string
	ChargeVND         int64
	MergedIntoCheckID string
}

type restructuringAllocationRow struct {
	ID              uuid.UUID
	CheckID         uuid.UUID
	CommittedItemID uuid.UUID
	Quantity        int32
	UnitPriceVND    int64
}

func snapshotRestructuringRows(t *testing.T, env *salesEnv, sessionID uuid.UUID) restructuringSnapshot {
	t.Helper()
	var snapshot restructuringSnapshot

	checkRows, err := env.DB.Query(`
		SELECT id, state, charge_vnd, coalesce(merged_into_check_id::text, '')
		FROM checks
		WHERE service_session_id = $1
		ORDER BY id`, sessionID)
	require.NoError(t, err)
	defer checkRows.Close()
	for checkRows.Next() {
		var row restructuringCheckRow
		require.NoError(t, checkRows.Scan(
			&row.ID, &row.State, &row.ChargeVND, &row.MergedIntoCheckID))
		snapshot.Checks = append(snapshot.Checks, row)
	}
	require.NoError(t, checkRows.Err())

	allocationRows, err := env.DB.Query(`
		SELECT ca.id, ca.check_id, ca.committed_item_id, ca.quantity, ci.unit_price_vnd
		FROM charge_allocations ca
		JOIN checks c ON c.id = ca.check_id
		JOIN committed_items ci ON ci.id = ca.committed_item_id
		WHERE c.service_session_id = $1
		ORDER BY ca.id`, sessionID)
	require.NoError(t, err)
	defer allocationRows.Close()
	for allocationRows.Next() {
		var row restructuringAllocationRow
		require.NoError(t, allocationRows.Scan(
			&row.ID, &row.CheckID, &row.CommittedItemID, &row.Quantity, &row.UnitPriceVND))
		snapshot.Allocations = append(snapshot.Allocations, row)
	}
	require.NoError(t, allocationRows.Err())

	return snapshot
}

// adjustedRestructuringFixture is an otherwise eligible restructuring world:
// one ACTIVE dine-in Session, split into two OPEN unpaid Checks, where the
// source Check carries one live Charge Adjustment. The stored charge already
// reflects the adjustment, so only the new guard stands between the handlers
// and a valid Split or Merge.
type adjustedRestructuringFixture struct {
	env        *salesEnv
	sessionID  uuid.UUID
	adjustedID uuid.UUID
	otherID    uuid.UUID
	movedItem  sales.SplitItem
}

func newAdjustedRestructuringFixture(t *testing.T) adjustedRestructuringFixture {
	t.Helper()
	env := newSalesEnv(t)

	// Four units let the split below move one whole unit and still leave the
	// adjusted Check with a positive charge, so at RED the handlers would
	// otherwise succeed.
	committed := env.commitDineInDraftWithQuantity(t, 4)
	submitted := env.Submit(t, committed.ID)
	require.Len(t, submitted.PreparationUnits, 4)

	adjustedID := env.soleCheckID(t, committed.ID)
	allocation := env.checkAllocations(t, committed.ID, adjustedID)[0]
	require.Equal(t, int32(4), allocation.AllocatedQuantity,
		"the fixture commits one allocation of four units")

	got, _, err := env.splitToNewCheck(t, adjustedID, []sales.SplitItem{
		{CommittedItemID: allocation.CommittedItemID, Quantity: 1},
	})
	require.NoError(t, err)
	otherID := env.otherCheckID(t, got, adjustedID)

	var allocationID uuid.UUID
	var unitPriceVND int64
	require.NoError(t, env.DB.QueryRow(`
		SELECT ca.id, ci.unit_price_vnd
		FROM charge_allocations ca
		JOIN committed_items ci ON ci.id = ca.committed_item_id
		WHERE ca.check_id = $1`, adjustedID).Scan(&allocationID, &unitPriceVND))

	// Seed the live adjustment a Cancellation would have written, and apply
	// its charge consequence to the denormalized Check row (spec §6.1).
	_, err = env.DB.Exec(`
		INSERT INTO charge_adjustments (kind, scope, preparation_unit_id,
			charge_allocation_id, check_id, sales_shift_id, amount_vnd)
		VALUES ('CANCELLATION', 'LIVE_CHECK', $1, $2, $3, $4, $5)`,
		submitted.PreparationUnits[0].ID, allocationID, adjustedID,
		env.ShiftID, unitPriceVND)
	require.NoError(t, err)
	_, err = env.DB.Exec(
		`UPDATE checks SET charge_vnd = charge_vnd - $2 WHERE id = $1`,
		adjustedID, unitPriceVND)
	require.NoError(t, err)

	return adjustedRestructuringFixture{
		env:        env,
		sessionID:  committed.ID,
		adjustedID: adjustedID,
		otherID:    otherID,
		movedItem: sales.SplitItem{
			CommittedItemID: allocation.CommittedItemID,
			Quantity:        1,
		},
	}
}

// A live Charge Adjustment names the immutable Charge Allocation it reduced,
// so a Split or Merge that rewrote or moved that allocation would strand the
// adjustment. Both handlers reject with CHECK_HAS_CHARGE_ADJUSTMENT after the
// Check locks and before any charge or allocation mutation, leaving every
// Check and allocation row untouched (design section 6.2).
func TestSplitAndMergeRejectLiveChargeAdjustment(t *testing.T) {
	fixture := newAdjustedRestructuringFixture(t)
	env := fixture.env

	t.Run("split rejects a check carrying a live adjustment", func(t *testing.T) {
		before := snapshotRestructuringRows(t, env, fixture.sessionID)

		_, _, err := env.splitToNewCheck(t, fixture.adjustedID,
			[]sales.SplitItem{fixture.movedItem})

		require.ErrorIs(t, err, sales.ErrCheckHasChargeAdjustment)
		require.Equal(t, before, snapshotRestructuringRows(t, env, fixture.sessionID),
			"a rejected split must rewrite no Check or Charge Allocation row")
	})

	t.Run("merge rejects a check carrying a live adjustment", func(t *testing.T) {
		before := snapshotRestructuringRows(t, env, fixture.sessionID)

		_, _, err := env.mergeChecks(t, fixture.otherID, fixture.adjustedID)

		require.ErrorIs(t, err, sales.ErrCheckHasChargeAdjustment)
		require.Equal(t, before, snapshotRestructuringRows(t, env, fixture.sessionID),
			"a rejected merge must rewrite no Check or Charge Allocation row")
	})
}
