package catalog

import (
	"context"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

// SellableMenuHandler handles reading the sellable menu projection.
type SellableMenuHandler struct {
	runner *Runner
}

// NewSellableMenuHandler creates a new SellableMenuHandler.
func NewSellableMenuHandler(runner *Runner) *SellableMenuHandler {
	return &SellableMenuHandler{runner: runner}
}

// Handle executes the sellable menu projection query.
func (h *SellableMenuHandler) Handle(ctx context.Context, actor Actor) (SellableMenuResponse, error) {
	return ExecuteRead(ctx, h.runner, actor, CapViewPrices, func(q *sqlc.Queries) (SellableMenuResponse, error) {
		snap, err := loadCatalogSnapshot(ctx, q)
		if err != nil {
			return SellableMenuResponse{}, err
		}
		return newSellableProjection(snap).menu(snap), nil
	})
}

// sellableProjection builds the Sellable Menu: only the items, sizes, and
// options that can be ordered right now, and only items that pass IsSellable.
type sellableProjection struct {
	assignments     assignmentIndex
	optionsByGroup  map[uuid.UUID][]sqlc.ModifierOption
	defaultsByGroup map[uuid.UUID][]uuid.UUID
	sizesByItem     map[uuid.UUID][]sqlc.MenuItemSize
}

func newSellableProjection(snap *catalogSnapshot) sellableProjection {
	return sellableProjection{
		assignments:     newAssignmentIndex(snap),
		optionsByGroup:  groupBy(snap.options, modifierOptionGroupID, func(o sqlc.ModifierOption) sqlc.ModifierOption { return o }),
		defaultsByGroup: defaultOptionsByGroup(snap.defaults),
		sizesByItem: groupBy(snap.sizes,
			func(s sqlc.MenuItemSize) uuid.UUID { return s.MenuItemID },
			func(s sqlc.MenuItemSize) sqlc.MenuItemSize { return s }),
	}
}

// menu lists the active categories that have at least one sellable item.
func (p sellableProjection) menu(snap *catalogSnapshot) SellableMenuResponse {
	itemsByCategory := make(map[uuid.UUID][]SellableItemResponse)
	for _, item := range snap.items {
		if resp, ok := p.item(item); ok {
			itemsByCategory[item.CategoryID] = append(itemsByCategory[item.CategoryID], resp)
		}
	}

	var categories []SellableCategoryResponse
	for _, cat := range snap.categories {
		catItems := itemsByCategory[cat.ID]
		if cat.RetiredAt.Valid || len(catItems) == 0 {
			continue
		}
		categories = append(categories, SellableCategoryResponse{
			ID:    cat.ID,
			Name:  cat.Name,
			Icon:  nullStringPtr(cat.Icon),
			Items: catItems,
		})
	}
	return SellableMenuResponse{Categories: nonNil(categories)}
}

// item projects one menu item, reporting false when it cannot be sold.
func (p sellableProjection) item(item sqlc.MenuItem) (SellableItemResponse, bool) {
	if item.RetiredAt.Valid || !item.Available {
		return SellableItemResponse{}, false
	}

	sizes := p.availableSizes(item.ID)
	hasDirectPrice := item.PriceVnd.Valid && item.PriceVnd.Int64 > 0
	groups, effective, availableOptions := p.modifierGroups(item)
	if !IsSellable(ItemState{
		Available:             true,
		HasDirectPrice:        hasDirectPrice,
		AvailableSizeCount:    len(sizes),
		EffectiveGroups:       effective,
		AvailableOptionCounts: availableOptions,
	}) {
		return SellableItemResponse{}, false
	}

	sortByNormalizedNameThenID(groups,
		func(g SellableModifierGroupResponse) string { return g.Name },
		func(g SellableModifierGroupResponse) uuid.UUID { return g.ID })

	var price *int64
	if hasDirectPrice {
		price = nullInt64Ptr(item.PriceVnd)
	}
	return SellableItemResponse{
		ID:             item.ID,
		CategoryID:     item.CategoryID,
		Name:           item.Name,
		Code:           nullStringPtr(item.Code),
		Badge:          nullStringPtr(item.Badge),
		ImageURL:       ImageURL(item.ImageKey),
		PriceVND:       price,
		Sizes:          sizes,
		ModifierGroups: groups,
	}, true
}

// availableSizes returns the item's available, active sizes; nil when there
// are none.
func (p sellableProjection) availableSizes(itemID uuid.UUID) []SellableSizeResponse {
	var sizes []SellableSizeResponse
	for _, s := range p.sizesByItem[itemID] {
		if s.Available && !s.RetiredAt.Valid {
			sizes = append(sizes, SellableSizeResponse{
				ID:       s.ID,
				Name:     s.Name,
				PriceVND: s.PriceVnd,
			})
		}
	}
	return sizes
}

// modifierGroups projects the item's active effective groups, and returns
// alongside them what IsSellable needs: each group's minimum and its count of
// orderable options.
func (p sellableProjection) modifierGroups(item sqlc.MenuItem) ([]SellableModifierGroupResponse, []EffectiveGroup, map[uuid.UUID]int) {
	var groups []SellableModifierGroupResponse
	var effective []EffectiveGroup
	availableOptions := make(map[uuid.UUID]int)
	for _, gID := range p.assignments.effectiveGroups(item) {
		grp, ok := p.assignments.groupByID[gID]
		if !ok || grp.RetiredAt.Valid {
			continue
		}
		effective = append(effective, EffectiveGroup{
			ID:            grp.ID,
			MinSelections: grp.MinSelections,
		})
		resp := p.modifierGroup(grp)
		availableOptions[grp.ID] = len(resp.Options)
		groups = append(groups, resp)
	}
	return groups, effective, availableOptions
}

// modifierGroup projects a group with its orderable options. Defaults that
// are not orderable are dropped.
func (p sellableProjection) modifierGroup(grp sqlc.ModifierGroup) SellableModifierGroupResponse {
	var options []SellableModifierOptionResponse
	orderable := make(map[uuid.UUID]bool)
	for _, opt := range p.optionsByGroup[grp.ID] {
		if opt.Available && !opt.RetiredAt.Valid {
			options = append(options, SellableModifierOptionResponse{
				ID:           opt.ID,
				Name:         opt.Name,
				SurchargeVND: opt.SurchargeVnd,
			})
			orderable[opt.ID] = true
		}
	}

	var defaults []uuid.UUID
	for _, id := range p.defaultsByGroup[grp.ID] {
		if orderable[id] {
			defaults = append(defaults, id)
		}
	}
	sortUUIDs(defaults)

	return SellableModifierGroupResponse{
		ID:               grp.ID,
		Name:             grp.Name,
		MinSelections:    grp.MinSelections,
		MaxSelections:    grp.MaxSelections,
		Options:          nonNil(options),
		DefaultOptionIDs: nonNil(defaults),
	}
}
