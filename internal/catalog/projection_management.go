package catalog

import (
	"context"

	"github.com/Mirai3103/pos-cafe/internal/platform/database/sqlc"
	"github.com/google/uuid"
)

// ManagementMenuHandler handles reading the full catalog management projection.
type ManagementMenuHandler struct {
	runner *Runner
}

// NewManagementMenuHandler creates a new ManagementMenuHandler.
func NewManagementMenuHandler(runner *Runner) *ManagementMenuHandler {
	return &ManagementMenuHandler{runner: runner}
}

// Handle executes the management menu projection query.
func (h *ManagementMenuHandler) Handle(ctx context.Context, actor Actor) (ManagementMenuResponse, error) {
	return ExecuteRead(ctx, h.runner, actor, CapViewPrices, func(q *sqlc.Queries) (ManagementMenuResponse, error) {
		snap, err := loadCatalogSnapshot(ctx, q)
		if err != nil {
			return ManagementMenuResponse{}, err
		}
		return newManagementProjection(snap).menu(snap), nil
	})
}

// managementProjection builds the Management Menu: every category, item,
// size, group, and option, retired or not, with assignment id lists sorted.
type managementProjection struct {
	assignments     assignmentIndex
	optionsByGroup  map[uuid.UUID][]ManagementModifierOptionResponse
	defaultsByGroup map[uuid.UUID][]uuid.UUID
	sizesByItem     map[uuid.UUID][]ManagementSizeResponse
}

func newManagementProjection(snap *catalogSnapshot) managementProjection {
	assignments := newAssignmentIndex(snap)
	assignments.sortIDLists()
	return managementProjection{
		assignments:     assignments,
		optionsByGroup:  groupBy(snap.options, modifierOptionGroupID, buildManagementModifierOption),
		defaultsByGroup: defaultOptionsByGroup(snap.defaults),
		sizesByItem: groupBy(snap.sizes,
			func(s sqlc.MenuItemSize) uuid.UUID { return s.MenuItemID },
			buildManagementSize),
	}
}

func (p managementProjection) menu(snap *catalogSnapshot) ManagementMenuResponse {
	itemsByCategory := groupBy(snap.items,
		func(item sqlc.MenuItem) uuid.UUID { return item.CategoryID },
		p.item)

	var categories []ManagementCategoryResponse
	for _, cat := range snap.categories {
		categories = append(categories, p.category(cat, itemsByCategory[cat.ID]))
	}
	return ManagementMenuResponse{Categories: nonNil(categories)}
}

func (p managementProjection) category(cat sqlc.MenuCategory, items []ManagementItemResponse) ManagementCategoryResponse {
	retiredAt, reason, note := retirementDetails(cat.RetiredAt, cat.RetirementReason, cat.RetirementNote)
	return ManagementCategoryResponse{
		ID:               cat.ID,
		Name:             cat.Name,
		Icon:             nullStringPtr(cat.Icon),
		DisplayOrder:     cat.DisplayOrder,
		Retired:          cat.RetiredAt.Valid,
		RetiredAt:        retiredAt,
		RetirementReason: reason,
		RetirementNote:   note,
		ModifierGroupIDs: nonNil(p.assignments.categoryGroups[cat.ID]),
		Items:            nonNil(items),
	}
}

func (p managementProjection) item(item sqlc.MenuItem) ManagementItemResponse {
	retiredAt, reason, note := retirementDetails(item.RetiredAt, item.RetirementReason, item.RetirementNote)
	return ManagementItemResponse{
		ID:                       item.ID,
		CategoryID:               item.CategoryID,
		Name:                     item.Name,
		Code:                     nullStringPtr(item.Code),
		Badge:                    nullStringPtr(item.Badge),
		Description:              nullStringPtr(item.Description),
		ImageURL:                 ImageURL(item.ImageKey),
		PriceVND:                 nullInt64Ptr(item.PriceVnd),
		Available:                item.Available,
		Retired:                  item.RetiredAt.Valid,
		RetiredAt:                retiredAt,
		RetirementReason:         reason,
		RetirementNote:           note,
		Sizes:                    nonNil(p.sizesByItem[item.ID]),
		DirectModifierGroupIDs:   nonNil(p.assignments.itemDirectGroups[item.ID]),
		ExcludedModifierGroupIDs: nonNil(p.assignments.itemExclusions[item.ID]),
		ModifierGroups:           p.modifierGroups(item),
	}
}

// modifierGroups projects every effective group of the item, retired ones
// included, sorted by name.
func (p managementProjection) modifierGroups(item sqlc.MenuItem) []ManagementModifierGroupResponse {
	var groups []ManagementModifierGroupResponse
	for _, gID := range p.assignments.effectiveGroups(item) {
		grp, ok := p.assignments.groupByID[gID]
		if !ok {
			continue
		}
		groups = append(groups, buildManagementModifierGroup(grp, p.optionsByGroup[grp.ID], sortedCopy(p.defaultsByGroup[grp.ID])))
	}
	sortByNormalizedNameThenID(groups,
		func(g ManagementModifierGroupResponse) string { return g.Name },
		func(g ManagementModifierGroupResponse) uuid.UUID { return g.ID })
	return nonNil(groups)
}

func buildManagementSize(s sqlc.MenuItemSize) ManagementSizeResponse {
	retiredAt, reason, note := retirementDetails(s.RetiredAt, s.RetirementReason, s.RetirementNote)
	return ManagementSizeResponse{
		ID:               s.ID,
		MenuItemID:       s.MenuItemID,
		Name:             s.Name,
		PriceVND:         s.PriceVnd,
		Available:        s.Available,
		Retired:          s.RetiredAt.Valid,
		RetiredAt:        retiredAt,
		RetirementReason: reason,
		RetirementNote:   note,
	}
}
