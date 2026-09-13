package sales_test

import (
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func candidate() sales.CommitCandidate {
	return sales.CommitCandidate{
		DraftItemID:  uuid.New(),
		MenuItemID:   uuid.New(),
		ItemName:     "Cà phê sữa",
		CategoryName: "Cà phê",
		Quantity:     2,
		ItemPriceVND: ptrInt64(25_000),
	}
}

func ptrInt64(v int64) *int64 { return &v }

func TestBuildSnapshotPricesADirectlyPricedItem(t *testing.T) {
	got, err := sales.BuildSnapshot(candidate())
	require.NoError(t, err)
	require.Equal(t, int64(25_000), got.UnitPriceVND)
	require.Equal(t, int64(50_000), got.TotalVND)
	require.Nil(t, got.SizeName)
}

func TestBuildSnapshotAddsSurchargesToTheBasePrice(t *testing.T) {
	groupID := uuid.New()
	c := candidate()
	c.EffectiveGroups = []sales.EffectiveGroup{
		{GroupID: groupID, MinSelections: 0, MaxSelections: 2},
	}
	c.Selected = []sales.SelectedOption{
		{GroupID: groupID, OptionID: uuid.New(), OptionName: "Trân châu", SurchargeVND: 5_000, Available: true},
		{GroupID: groupID, OptionID: uuid.New(), OptionName: "Thạch", SurchargeVND: 3_000, Available: true},
	}
	got, err := sales.BuildSnapshot(c)
	require.NoError(t, err)
	require.Equal(t, int64(33_000), got.UnitPriceVND)
	require.Equal(t, int64(66_000), got.TotalVND)
	require.Len(t, got.Modifiers, 2)
}

func TestBuildSnapshotRequiresASizeForASizedItem(t *testing.T) {
	c := candidate()
	c.ItemPriceVND = nil // sized items carry no own price
	_, err := sales.BuildSnapshot(c)
	require.ErrorIs(t, err, sales.ErrCommitSizeRequired)
	require.Contains(t, err.Error(), "Cà phê sữa")
}

func TestBuildSnapshotRejectsASizeOnADirectlyPricedItem(t *testing.T) {
	c := candidate()
	sizeID := uuid.New()
	c.SizeID = &sizeID
	c.Size = &sales.CommitSize{
		ID: sizeID, MenuItemID: c.MenuItemID, Name: "Lớn", PriceVND: 30_000, Available: true,
	}
	_, err := sales.BuildSnapshot(c)
	require.ErrorIs(t, err, sales.ErrCommitSizeInvalid)
}

func TestBuildSnapshotRejectsAForeignSize(t *testing.T) {
	c := candidate()
	c.ItemPriceVND = nil
	sizeID := uuid.New()
	c.SizeID = &sizeID
	c.Size = &sales.CommitSize{
		ID: sizeID, MenuItemID: uuid.New(), Name: "Lớn", PriceVND: 30_000, Available: true,
	}
	_, err := sales.BuildSnapshot(c)
	require.ErrorIs(t, err, sales.ErrCommitSizeInvalid)
}

func TestBuildSnapshotRejectsARetiredSizeBeforeAnUnavailableOne(t *testing.T) {
	c := candidate()
	c.ItemPriceVND = nil
	sizeID := uuid.New()
	c.SizeID = &sizeID
	c.Size = &sales.CommitSize{
		ID: sizeID, MenuItemID: c.MenuItemID, Name: "Lớn",
		PriceVND: 30_000, Available: false, Retired: true,
	}
	_, err := sales.BuildSnapshot(c)
	require.ErrorIs(t, err, sales.ErrCommitSizeRetired)
}

func TestBuildSnapshotRejectsAnOptionOutsideTheEffectiveGroups(t *testing.T) {
	c := candidate()
	c.EffectiveGroups = []sales.EffectiveGroup{
		{GroupID: uuid.New(), MinSelections: 0, MaxSelections: 1},
	}
	c.Selected = []sales.SelectedOption{
		{GroupID: uuid.New(), OptionID: uuid.New(), OptionName: "Lạ", SurchargeVND: 0},
	}
	_, err := sales.BuildSnapshot(c)
	require.ErrorIs(t, err, sales.ErrCommitModifierOptionInvalid)
}

func TestBuildSnapshotEnforcesGroupSelectionCounts(t *testing.T) {
	groupID := uuid.New()

	t.Run("below the minimum", func(t *testing.T) {
		c := candidate()
		c.EffectiveGroups = []sales.EffectiveGroup{
			{GroupID: groupID, MinSelections: 1, MaxSelections: 1},
		}
		_, err := sales.BuildSnapshot(c)
		require.ErrorIs(t, err, sales.ErrCommitModifierGroupInvalid)
	})

	t.Run("above the maximum", func(t *testing.T) {
		c := candidate()
		c.EffectiveGroups = []sales.EffectiveGroup{
			{GroupID: groupID, MinSelections: 0, MaxSelections: 1},
		}
		c.Selected = []sales.SelectedOption{
			{GroupID: groupID, OptionID: uuid.New(), OptionName: "A", Available: true},
			{GroupID: groupID, OptionID: uuid.New(), OptionName: "B", Available: true},
		}
		_, err := sales.BuildSnapshot(c)
		require.ErrorIs(t, err, sales.ErrCommitModifierGroupInvalid)
	})

	t.Run("exactly at both bounds", func(t *testing.T) {
		c := candidate()
		c.EffectiveGroups = []sales.EffectiveGroup{
			{GroupID: groupID, MinSelections: 1, MaxSelections: 2},
		}
		c.Selected = []sales.SelectedOption{
			{GroupID: groupID, OptionID: uuid.New(), OptionName: "A", Available: true},
		}
		_, err := sales.BuildSnapshot(c)
		require.NoError(t, err)
	})
}

func TestBuildSnapshotSkipsARetiredGroupThatRequiresNothing(t *testing.T) {
	c := candidate()
	c.EffectiveGroups = []sales.EffectiveGroup{
		{GroupID: uuid.New(), MinSelections: 0, MaxSelections: 1, Retired: true},
	}
	_, err := sales.BuildSnapshot(c)
	require.NoError(t, err)
}

func TestBuildSnapshotRejectsARetiredGroupThatRequiresASelection(t *testing.T) {
	c := candidate()
	c.EffectiveGroups = []sales.EffectiveGroup{
		{GroupID: uuid.New(), MinSelections: 1, MaxSelections: 1, Retired: true},
	}
	_, err := sales.BuildSnapshot(c)
	require.ErrorIs(t, err, sales.ErrCommitModifierGroupRetired)
}

func TestBuildSnapshotRejectsARetiredOptionBeforeAnUnavailableOne(t *testing.T) {
	groupID := uuid.New()
	c := candidate()
	c.EffectiveGroups = []sales.EffectiveGroup{
		{GroupID: groupID, MinSelections: 0, MaxSelections: 1},
	}
	c.Selected = []sales.SelectedOption{
		{GroupID: groupID, OptionID: uuid.New(), OptionName: "A", Available: false, Retired: true},
	}
	_, err := sales.BuildSnapshot(c)
	require.ErrorIs(t, err, sales.ErrCommitModifierOptionRetired)
}
