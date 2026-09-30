package catalog

import (
	"context"
	"slices"

	"github.com/Mirai3103/pos-cafe/internal/platform/database/sqlc"
	"github.com/google/uuid"
)

// AvailabilityMenuHandler handles reading the availability menu projection; prices are included only for callers holding catalog.view_prices (ADR-061).
type AvailabilityMenuHandler struct {
	runner *Runner
}

// NewAvailabilityMenuHandler creates a new AvailabilityMenuHandler.
func NewAvailabilityMenuHandler(runner *Runner) *AvailabilityMenuHandler {
	return &AvailabilityMenuHandler{runner: runner}
}

// Handle executes the availability menu projection query.
func (h *AvailabilityMenuHandler) Handle(ctx context.Context, actor Actor) (AvailabilityMenuResponse, error) {
	return ExecuteReadWithCapabilities(ctx, h.runner, actor, CapManageAvailability, func(q *sqlc.Queries, caps []string) (AvailabilityMenuResponse, error) {
		showPrices := slices.Contains(caps, CapViewPrices)

		snap, err := loadCatalogSnapshot(ctx, q)
		if err != nil {
			return AvailabilityMenuResponse{}, err
		}
		return newAvailabilityProjection(snap, showPrices).menu(snap), nil
	})
}

// availabilityProjection builds the Availability Menu: every active
// category, item, size, group, and option with its availability flag, and
// prices only when showPrices is set.
type availabilityProjection struct {
	assignments    assignmentIndex
	showPrices     bool
	optionsByGroup map[uuid.UUID][]AvailabilityModifierOptionResponse
	sizesByItem    map[uuid.UUID][]AvailabilitySizeResponse
	// lowestSizePrice is the cheapest active size price per item, shown as
	// a sized item's price.
	lowestSizePrice map[uuid.UUID]int64
}

func newAvailabilityProjection(snap *catalogSnapshot, showPrices bool) availabilityProjection {
	p := availabilityProjection{
		assignments:     newAssignmentIndex(snap),
		showPrices:      showPrices,
		optionsByGroup:  make(map[uuid.UUID][]AvailabilityModifierOptionResponse),
		sizesByItem:     make(map[uuid.UUID][]AvailabilitySizeResponse),
		lowestSizePrice: make(map[uuid.UUID]int64),
	}
	for _, opt := range snap.options {
		if opt.RetiredAt.Valid {
			continue
		}
		p.optionsByGroup[opt.ModifierGroupID] = append(p.optionsByGroup[opt.ModifierGroupID], p.option(opt))
	}
	for _, s := range snap.sizes {
		if s.RetiredAt.Valid {
			continue
		}
		p.sizesByItem[s.MenuItemID] = append(p.sizesByItem[s.MenuItemID], AvailabilitySizeResponse{
			ID:        s.ID,
			Name:      s.Name,
			Available: s.Available,
		})
		if cur, ok := p.lowestSizePrice[s.MenuItemID]; !ok || s.PriceVnd < cur {
			p.lowestSizePrice[s.MenuItemID] = s.PriceVnd
		}
	}
	return p
}

func (p availabilityProjection) menu(snap *catalogSnapshot) AvailabilityMenuResponse {
	itemsByCategory := make(map[uuid.UUID][]AvailabilityItemResponse)
	for _, item := range snap.items {
		if item.RetiredAt.Valid {
			continue
		}
		itemsByCategory[item.CategoryID] = append(itemsByCategory[item.CategoryID], p.item(item))
	}

	var categories []AvailabilityCategoryResponse
	for _, cat := range snap.categories {
		if cat.RetiredAt.Valid {
			continue
		}
		categories = append(categories, AvailabilityCategoryResponse{
			ID:    cat.ID,
			Name:  cat.Name,
			Icon:  nullStringPtr(cat.Icon),
			Items: nonNil(itemsByCategory[cat.ID]),
		})
	}
	return AvailabilityMenuResponse{Categories: nonNil(categories)}
}

func (p availabilityProjection) item(item sqlc.MenuItem) AvailabilityItemResponse {
	resp := AvailabilityItemResponse{
		ID:             item.ID,
		CategoryID:     item.CategoryID,
		Name:           item.Name,
		Code:           nullStringPtr(item.Code),
		ImageURL:       ImageURL(item.ImageKey),
		Available:      item.Available,
		Sizes:          nonNil(p.sizesByItem[item.ID]),
		ModifierGroups: p.modifierGroups(item),
	}
	if p.showPrices {
		if item.PriceVnd.Valid {
			resp.PriceVND = nullInt64Ptr(item.PriceVnd)
		} else if price, ok := p.lowestSizePrice[item.ID]; ok {
			resp.PriceVND = &price
		}
	}
	return resp
}

// modifierGroups projects the item's active effective groups, sorted by name.
func (p availabilityProjection) modifierGroups(item sqlc.MenuItem) []AvailabilityModifierGroupResponse {
	var groups []AvailabilityModifierGroupResponse
	for _, gID := range p.assignments.effectiveGroups(item) {
		grp, ok := p.assignments.groupByID[gID]
		if !ok || grp.RetiredAt.Valid {
			continue
		}
		groups = append(groups, AvailabilityModifierGroupResponse{
			ID:            grp.ID,
			Name:          grp.Name,
			MinSelections: grp.MinSelections,
			MaxSelections: grp.MaxSelections,
			Options:       nonNil(p.optionsByGroup[grp.ID]),
		})
	}
	sortByNormalizedNameThenID(groups,
		func(g AvailabilityModifierGroupResponse) string { return g.Name },
		func(g AvailabilityModifierGroupResponse) uuid.UUID { return g.ID })
	return nonNil(groups)
}

func (p availabilityProjection) option(opt sqlc.ModifierOption) AvailabilityModifierOptionResponse {
	resp := AvailabilityModifierOptionResponse{ID: opt.ID, Name: opt.Name, Available: opt.Available}
	if p.showPrices {
		surcharge := opt.SurchargeVnd
		resp.SurchargeVND = &surcharge
	}
	return resp
}
