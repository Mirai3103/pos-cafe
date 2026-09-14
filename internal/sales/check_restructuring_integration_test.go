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
