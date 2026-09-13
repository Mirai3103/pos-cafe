//go:build integration

package sales_test

import (
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCommitFreezesPricesAndOpensACheck(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartTakeaway(t)
	env.AddDraftItem(t, session.ID, env.CoffeeID, nil)

	got := env.Commit(t, session.ID)

	require.Nil(t, got.Draft, "the draft is committed, so the session has no editable draft")
	require.Len(t, got.Checks, 1)
	check := got.Checks[0]
	require.Equal(t, "OPEN", check.State)
	require.Len(t, check.Allocations, 1)
	require.Equal(t, check.Allocations[0].AmountVND, check.ChargeVND)
	require.Equal(t, check.ChargeVND, check.BalanceVND)
	require.Zero(t, check.TotalAppliedVND)
	require.False(t, check.Allocations[0].Submitted)
	require.Empty(t, check.Payments)
}

func TestCommitIsModeAgnostic(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartDineIn(t, env.TableID)
	env.AddDraftItem(t, session.ID, env.CoffeeID, nil)

	got := env.Commit(t, session.ID)

	require.Len(t, got.Checks, 1)
	require.Len(t, got.Checks[0].Allocations, 1)
}

// The Sized Item's price lives on its Size, so the charge and the frozen
// size_name both come from the Size row, never from the item's NULL price.
func TestCommitChargesTheSizePriceAndFreezesTheSizeName(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartTakeaway(t)
	env.AddDraftItem(t, session.ID, env.SizedItemID, &env.LargeSizeID)

	got := env.Commit(t, session.ID)

	require.Len(t, got.Checks, 1)
	check := got.Checks[0]
	require.Equal(t, int64(30_000), check.ChargeVND)
	require.Len(t, check.Allocations, 1)
	require.Equal(t, check.ChargeVND, check.Allocations[0].AmountVND)
	require.NotNil(t, check.Allocations[0].SizeName)
	require.Equal(t, "Lớn", *check.Allocations[0].SizeName)

	// The snapshot persists: a re-read of the Session still shows the frozen
	// size name and the size-derived charge.
	after := env.GetSessionOK(t, session.ID)
	require.Len(t, after.Checks, 1)
	require.Equal(t, int64(30_000), after.Checks[0].ChargeVND)
	require.Len(t, after.Checks[0].Allocations, 1)
	require.NotNil(t, after.Checks[0].Allocations[0].SizeName)
	require.Equal(t, "Lớn", *after.Checks[0].Allocations[0].SizeName)
}

func TestCommitRejectsAnEmptyDraft(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartTakeaway(t)

	_, err := env.TryCommit(t, session.ID)

	require.ErrorIs(t, err, sales.ErrEmptyDraft)
	env.RequireNoChecks(t, session.ID)
	env.RequireDraftState(t, session.ID, "EDITABLE")
}

func TestCommitReportsRetiredBeforeUnavailable(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartTakeaway(t)
	env.AddDraftItem(t, session.ID, env.CoffeeID, nil)
	env.AddDraftItem(t, session.ID, env.TeaID, nil)
	env.SetMenuItemAvailable(t, env.CoffeeID, false)
	env.RetireMenuItem(t, env.TeaID)

	_, err := env.TryCommit(t, session.ID)

	require.ErrorIs(t, err, sales.ErrCommitMenuItemRetired)
	env.RequireNoChecks(t, session.ID)
}

func TestCommitRejectionsLeaveTheDraftEditable(t *testing.T) {
	cases := []struct {
		name    string
		arrange func(t *testing.T, env *salesEnv, sessionID uuid.UUID)
		want    error
	}{
		{
			name: "unavailable menu item",
			arrange: func(t *testing.T, env *salesEnv, sessionID uuid.UUID) {
				env.AddDraftItem(t, sessionID, env.CoffeeID, nil)
				env.SetMenuItemAvailable(t, env.CoffeeID, false)
			},
			want: sales.ErrCommitMenuItemUnavailable,
		},
		{
			name: "sized item with no size",
			arrange: func(t *testing.T, env *salesEnv, sessionID uuid.UUID) {
				env.AddDraftItem(t, sessionID, env.SizedItemID, nil)
			},
			want: sales.ErrCommitSizeRequired,
		},
		{
			name: "retired size",
			arrange: func(t *testing.T, env *salesEnv, sessionID uuid.UUID) {
				env.AddDraftItem(t, sessionID, env.SizedItemID, &env.LargeSizeID)
				env.RetireSize(t, env.LargeSizeID)
			},
			want: sales.ErrCommitSizeRetired,
		},
		{
			name: "unavailable size",
			arrange: func(t *testing.T, env *salesEnv, sessionID uuid.UUID) {
				env.AddDraftItem(t, sessionID, env.SizedItemID, &env.LargeSizeID)
				env.SetSizeAvailable(t, env.LargeSizeID, false)
			},
			want: sales.ErrCommitSizeUnavailable,
		},
		{
			name: "option whose group stopped being effective",
			arrange: func(t *testing.T, env *salesEnv, sessionID uuid.UUID) {
				env.AddDraftItemWithOptions(t, sessionID, env.CoffeeID, env.ToppingOptionID)
				env.ExcludeGroupFromItem(t, env.CoffeeID, env.ToppingGroupID)
			},
			want: sales.ErrCommitModifierOptionInvalid,
		},
		{
			name: "retired option",
			arrange: func(t *testing.T, env *salesEnv, sessionID uuid.UUID) {
				env.AddDraftItemWithOptions(t, sessionID, env.CoffeeID, env.ToppingOptionID)
				env.RetireOption(t, env.ToppingOptionID)
			},
			want: sales.ErrCommitModifierOptionRetired,
		},
		{
			name: "unavailable option",
			arrange: func(t *testing.T, env *salesEnv, sessionID uuid.UUID) {
				env.AddDraftItemWithOptions(t, sessionID, env.CoffeeID, env.ToppingOptionID)
				env.SetOptionAvailable(t, env.ToppingOptionID, false)
			},
			want: sales.ErrCommitModifierOptionUnavailable,
		},
		{
			name: "unsatisfied required group",
			arrange: func(t *testing.T, env *salesEnv, sessionID uuid.UUID) {
				env.SetGroupSelectionBounds(t, env.ToppingGroupID, 1, 1)
				env.AddDraftItem(t, sessionID, env.CoffeeID, nil)
			},
			want: sales.ErrCommitModifierGroupInvalid,
		},
		{
			name: "retired group that requires a selection",
			arrange: func(t *testing.T, env *salesEnv, sessionID uuid.UUID) {
				env.SetGroupSelectionBounds(t, env.ToppingGroupID, 1, 1)
				env.AddDraftItem(t, sessionID, env.CoffeeID, nil)
				env.RetireGroup(t, env.ToppingGroupID)
			},
			want: sales.ErrCommitModifierGroupRetired,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newSalesEnv(t)
			session := env.StartTakeaway(t)
			tc.arrange(t, env, session.ID)

			_, err := env.TryCommit(t, session.ID)

			require.ErrorIs(t, err, tc.want)
			env.RequireNoChecks(t, session.ID)
			env.RequireNoCommittedItems(t, session.ID)
			env.RequireDraftState(t, session.ID, "EDITABLE")
		})
	}
}

func TestCommittedSnapshotSurvivesCatalogChanges(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartTakeaway(t)
	env.AddDraftItemWithOptions(t, session.ID, env.CoffeeID, env.ToppingOptionID)
	before := env.Commit(t, session.ID)
	require.Equal(t, int64(30_000), before.Checks[0].ChargeVND)

	env.RenameMenuItem(t, env.CoffeeID, "Tên mới")
	env.RenameOption(t, env.ToppingOptionID, "Topping mới")
	env.SetMenuItemAvailable(t, env.CoffeeID, false)

	after := env.GetSessionOK(t, session.ID)
	require.Equal(t, before.Checks[0].ChargeVND, after.Checks[0].ChargeVND)
	require.Equal(t, before.Checks[0].Allocations[0].Name, after.Checks[0].Allocations[0].Name)
	require.Equal(t,
		before.Checks[0].Allocations[0].Modifiers[0].OptionName,
		after.Checks[0].Allocations[0].Modifiers[0].OptionName)
}

func TestCommitIsIdempotent(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartTakeaway(t)
	env.AddDraftItem(t, session.ID, env.CoffeeID, nil)
	requestID := uuid.New()

	first := env.CommitWithRequestID(t, session.ID, requestID)
	second := env.CommitWithRequestID(t, session.ID, requestID)

	require.Equal(t, first.Checks[0].ID, second.Checks[0].ID)
	env.RequireCheckCount(t, session.ID, 1)
}

func TestCommitDeniedForBarista(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartTakeaway(t)
	env.AddDraftItem(t, session.ID, env.CoffeeID, nil)

	_, err := env.AsBarista().TryCommit(t, session.ID)

	require.ErrorIs(t, err, sales.ErrForbidden)
	env.RequireNoChecks(t, session.ID)
}

func TestCommitRequiresAnOpenShift(t *testing.T) {
	env := newSalesEnv(t)
	session := env.StartTakeaway(t)
	env.AddDraftItem(t, session.ID, env.CoffeeID, nil)
	env.CloseShift(t)

	_, err := env.TryCommit(t, session.ID)

	require.ErrorIs(t, err, sales.ErrEditableDraftNotFound)
}
