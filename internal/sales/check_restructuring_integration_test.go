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
