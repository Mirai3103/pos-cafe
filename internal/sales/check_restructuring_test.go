package sales_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestValidateSplitItems(t *testing.T) {
	a := uuid.MustParse("77777777-7777-7777-7777-777777777777")
	b := uuid.MustParse("88888888-8888-8888-8888-888888888888")

	t.Run("a well-formed list is accepted", func(t *testing.T) {
		require.NoError(t, sales.ValidateSplitItems([]sales.SplitItem{
			{CommittedItemID: a, Quantity: 1},
			{CommittedItemID: b, Quantity: 2},
		}))
	})

	t.Run("an empty list is rejected", func(t *testing.T) {
		require.ErrorIs(t, sales.ValidateSplitItems(nil), sales.ErrInvalidCheckSplit)
	})

	t.Run("a duplicated committed item is rejected", func(t *testing.T) {
		require.ErrorIs(t, sales.ValidateSplitItems([]sales.SplitItem{
			{CommittedItemID: a, Quantity: 1},
			{CommittedItemID: a, Quantity: 1},
		}), sales.ErrInvalidCheckSplit)
	})

	t.Run("a non-positive quantity is rejected", func(t *testing.T) {
		require.ErrorIs(t, sales.ValidateSplitItems([]sales.SplitItem{
			{CommittedItemID: a, Quantity: 0},
		}), sales.ErrInvalidCheckSplit)
	})

	t.Run("a quantity above the bound is request validation", func(t *testing.T) {
		// The upper bound guards the field's shape rather than describing a
		// split the client cannot have meant, so it lands on INVALID_INPUT
		// alongside the other quantity bounds (ADR-018).
		err := sales.ValidateSplitItems([]sales.SplitItem{
			{CommittedItemID: a, Quantity: 10_000},
		})
		require.ErrorIs(t, err, response.ErrInvalid)
		require.NotErrorIs(t, err, sales.ErrInvalidCheckSplit)
	})
}

// The fingerprint must not depend on the order the client listed items in,
// so a retried request that reshuffled its array replays rather than
// double-splitting.
func TestNormalizeSplitItemsSortsByCommittedItem(t *testing.T) {
	a := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	b := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	forward := sales.NormalizeSplitItems([]sales.SplitItem{
		{CommittedItemID: a, Quantity: 1}, {CommittedItemID: b, Quantity: 2},
	})
	reversed := sales.NormalizeSplitItems([]sales.SplitItem{
		{CommittedItemID: b, Quantity: 2}, {CommittedItemID: a, Quantity: 1},
	})
	require.Equal(t, forward, reversed)
	require.Equal(t, a, forward[0].CommittedItemID)
}

// A Check carrying a live Charge Adjustment is a conflict for Split and Merge,
// so the guard's sentinel must reach clients as a 409
// CHECK_HAS_CHARGE_ADJUSTMENT like every other Check conflict.
func TestMapHTTPErrorCheckHasChargeAdjustment(t *testing.T) {
	var coded *response.CodedError
	require.ErrorAs(t, sales.MapHTTPError(
		fmt.Errorf("wrapped: %w", sales.ErrCheckHasChargeAdjustment)), &coded)
	require.Equal(t, http.StatusConflict, coded.Status)
	require.Equal(t, "CHECK_HAS_CHARGE_ADJUSTMENT", coded.Code)
}
