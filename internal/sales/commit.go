package sales

import (
	"fmt"

	"github.com/google/uuid"
)

// EffectiveGroup is one Modifier Group that applies to a Menu Item, with the
// selection rules Commit enforces. 5A's draft validation deliberately ignores
// these counts: a draft is a proposal and tolerates incompleteness. Commit is
// where completeness matters, because it is where price is fixed.
type EffectiveGroup struct {
	GroupID       uuid.UUID
	GroupName     string
	MinSelections int32
	MaxSelections int32
	Retired       bool
}

// SelectedOption is one Modifier Option chosen on a draft item.
type SelectedOption struct {
	GroupID      uuid.UUID
	GroupName    string
	OptionID     uuid.UUID
	OptionName   string
	SurchargeVND int64
	Available    bool
	Retired      bool
	GroupRetired bool
}

// CommitSize is the Size a draft item selected, as it stands now.
type CommitSize struct {
	ID         uuid.UUID
	MenuItemID uuid.UUID
	Name       string
	PriceVND   int64
	Available  bool
	Retired    bool
}

// CommitCandidate is one draft item with everything Commit revalidates it
// against, loaded and locked by the handler.
//
// ItemPriceVND is nil exactly when the Menu Item is priced through its Sizes.
type CommitCandidate struct {
	DraftItemID     uuid.UUID
	MenuItemID      uuid.UUID
	ItemName        string
	CategoryName    string
	Quantity        int32
	ItemPriceVND    *int64
	PreparationNote *string
	SizeID          *uuid.UUID
	Size            *CommitSize
	EffectiveGroups []EffectiveGroup
	Selected        []SelectedOption
}

// ItemSnapshot is the immutable commercial record Commit persists.
type ItemSnapshot struct {
	SourceDraftItemID uuid.UUID
	MenuItemID        uuid.UUID
	CategoryName      string
	ItemName          string
	SizeName          *string
	Quantity          int32
	UnitPriceVND      int64
	TotalVND          int64
	PreparationNote   *string
	Modifiers         []CommittedModifierResponse
}

// BuildSnapshot revalidates one draft item and freezes it into a snapshot.
//
// The rule order is load-bearing and reproduces the canonical source: within
// each pair, retirement is reported before unavailability, because retirement
// is permanent and must not tell staff to try again later. Every error names
// the offending item, since a cashier told only that "an item is unavailable"
// cannot act on it.
//
// The canonical duplicate-option check is not migrated: 5A's
// order_draft_item_modifier_options primary key on (draft item, option) makes
// a duplicate unrepresentable, and a check that cannot fire is noise.
func BuildSnapshot(c CommitCandidate) (ItemSnapshot, error) {
	var zero ItemSnapshot

	basePriceVND, sizeName, err := resolveBasePrice(c)
	if err != nil {
		return zero, err
	}

	if err := validateSelections(c); err != nil {
		return zero, err
	}
	if err := validateGroupRules(c); err != nil {
		return zero, err
	}

	unitPriceVND := basePriceVND
	modifiers := make([]CommittedModifierResponse, 0, len(c.Selected))
	for _, opt := range c.Selected {
		unitPriceVND, err = AddCharge(unitPriceVND, opt.SurchargeVND)
		if err != nil {
			return zero, err
		}
		modifiers = append(modifiers, CommittedModifierResponse{
			GroupID:      opt.GroupID,
			GroupName:    opt.GroupName,
			OptionID:     opt.OptionID,
			OptionName:   opt.OptionName,
			SurchargeVND: opt.SurchargeVND,
		})
	}

	totalVND, err := LineTotal(c.Quantity, unitPriceVND)
	if err != nil {
		return zero, err
	}

	return ItemSnapshot{
		SourceDraftItemID: c.DraftItemID,
		MenuItemID:        c.MenuItemID,
		CategoryName:      c.CategoryName,
		ItemName:          c.ItemName,
		SizeName:          sizeName,
		Quantity:          c.Quantity,
		UnitPriceVND:      unitPriceVND,
		TotalVND:          totalVND,
		PreparationNote:   c.PreparationNote,
		Modifiers:         modifiers,
	}, nil
}

// resolveBasePrice applies the Size rules and returns the pre-surcharge price.
func resolveBasePrice(c CommitCandidate) (int64, *string, error) {
	sized := c.ItemPriceVND == nil

	if sized && c.SizeID == nil {
		return 0, nil, fmt.Errorf("%w: %s", ErrCommitSizeRequired, c.ItemName)
	}
	if !sized && c.SizeID != nil {
		return 0, nil, fmt.Errorf("%w: %s is priced directly", ErrCommitSizeInvalid, c.ItemName)
	}
	if !sized {
		return *c.ItemPriceVND, nil, nil
	}

	size := c.Size
	if size == nil || size.MenuItemID != c.MenuItemID {
		return 0, nil, fmt.Errorf("%w: %s", ErrCommitSizeInvalid, c.ItemName)
	}
	if size.Retired {
		return 0, nil, fmt.Errorf("%w: %s", ErrCommitSizeRetired, c.ItemName)
	}
	if !size.Available {
		return 0, nil, fmt.Errorf("%w: %s", ErrCommitSizeUnavailable, c.ItemName)
	}
	name := size.Name
	return size.PriceVND, &name, nil
}

// validateSelections rejects options that are no longer selectable.
func validateSelections(c CommitCandidate) error {
	effective := make(map[uuid.UUID]struct{}, len(c.EffectiveGroups))
	for _, g := range c.EffectiveGroups {
		effective[g.GroupID] = struct{}{}
	}
	for _, opt := range c.Selected {
		// A selection valid when it was made becomes invalid if the Item's
		// Category attachments changed since.
		if _, ok := effective[opt.GroupID]; !ok {
			return fmt.Errorf("%w: %s", ErrCommitModifierOptionInvalid, c.ItemName)
		}
		if opt.Retired || opt.GroupRetired {
			return fmt.Errorf("%w: %s", ErrCommitModifierOptionRetired, c.ItemName)
		}
		if !opt.Available {
			return fmt.Errorf("%w: %s", ErrCommitModifierOptionUnavailable, c.ItemName)
		}
	}
	return nil
}

// validateGroupRules enforces each effective Group's selection counts.
func validateGroupRules(c CommitCandidate) error {
	counts := make(map[uuid.UUID]int32, len(c.EffectiveGroups))
	for _, opt := range c.Selected {
		counts[opt.GroupID]++
	}
	for _, g := range c.EffectiveGroups {
		if g.Retired {
			// A retired Group that requires nothing is simply skipped; one
			// that requires a selection can no longer be satisfied.
			if g.MinSelections > 0 {
				return fmt.Errorf("%w: %s", ErrCommitModifierGroupRetired, c.ItemName)
			}
			continue
		}
		n := counts[g.GroupID]
		if n < g.MinSelections || n > g.MaxSelections {
			return fmt.Errorf("%w: %s", ErrCommitModifierGroupInvalid, c.ItemName)
		}
	}
	return nil
}
