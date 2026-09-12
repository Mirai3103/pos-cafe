package catalog

import (
	"context"
	"sort"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

type catalogSnapshot struct {
	categories []sqlc.MenuCategory
	items      []sqlc.MenuItem
	sizes      []sqlc.MenuItemSize
	groups     []sqlc.ModifierGroup
	options    []sqlc.ModifierOption
	catGroups  []sqlc.ListAllCategoryModifierGroupsRow
	itemGroups []sqlc.ListAllItemModifierGroupsRow
	exclusions []sqlc.ListAllItemModifierGroupExclusionsRow
	defaults   []sqlc.ListAllModifierGroupDefaultOptionsRow
}

func loadCatalogSnapshot(ctx context.Context, q *sqlc.Queries) (*catalogSnapshot, error) {
	categories, err := q.ListMenuCategories(ctx)
	if err != nil {
		return nil, err
	}

	items, err := q.ListAllMenuItems(ctx)
	if err != nil {
		return nil, err
	}

	sizes, err := q.ListAllMenuItemSizes(ctx)
	if err != nil {
		return nil, err
	}

	groups, err := q.ListModifierGroups(ctx)
	if err != nil {
		return nil, err
	}

	options, err := q.ListAllModifierOptions(ctx)
	if err != nil {
		return nil, err
	}

	catGroups, err := q.ListAllCategoryModifierGroups(ctx)
	if err != nil {
		return nil, err
	}

	itemGroups, err := q.ListAllItemModifierGroups(ctx)
	if err != nil {
		return nil, err
	}

	exclusions, err := q.ListAllItemModifierGroupExclusions(ctx)
	if err != nil {
		return nil, err
	}

	defaults, err := q.ListAllModifierGroupDefaultOptions(ctx)
	if err != nil {
		return nil, err
	}

	return &catalogSnapshot{
		categories: categories,
		items:      items,
		sizes:      sizes,
		groups:     groups,
		options:    options,
		catGroups:  catGroups,
		itemGroups: itemGroups,
		exclusions: exclusions,
		defaults:   defaults,
	}, nil
}

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

		groupByID := make(map[uuid.UUID]sqlc.ModifierGroup, len(snap.groups))
		for _, g := range snap.groups {
			groupByID[g.ID] = g
		}

		optionsByGroup := make(map[uuid.UUID][]sqlc.ModifierOption)
		for _, opt := range snap.options {
			optionsByGroup[opt.ModifierGroupID] = append(optionsByGroup[opt.ModifierGroupID], opt)
		}

		defaultOptionIDsByGroup := make(map[uuid.UUID][]uuid.UUID)
		for _, d := range snap.defaults {
			defaultOptionIDsByGroup[d.ModifierGroupID] = append(defaultOptionIDsByGroup[d.ModifierGroupID], d.ModifierOptionID)
		}

		categoryGroupIDs := make(map[uuid.UUID][]uuid.UUID)
		for _, cg := range snap.catGroups {
			categoryGroupIDs[cg.MenuCategoryID] = append(categoryGroupIDs[cg.MenuCategoryID], cg.ModifierGroupID)
		}

		itemDirectGroupIDs := make(map[uuid.UUID][]uuid.UUID)
		for _, ig := range snap.itemGroups {
			itemDirectGroupIDs[ig.MenuItemID] = append(itemDirectGroupIDs[ig.MenuItemID], ig.ModifierGroupID)
		}

		itemExclusionIDs := make(map[uuid.UUID][]uuid.UUID)
		for _, ie := range snap.exclusions {
			itemExclusionIDs[ie.MenuItemID] = append(itemExclusionIDs[ie.MenuItemID], ie.ModifierGroupID)
		}

		sizesByItem := make(map[uuid.UUID][]sqlc.MenuItemSize)
		for _, s := range snap.sizes {
			sizesByItem[s.MenuItemID] = append(sizesByItem[s.MenuItemID], s)
		}

		itemsByCategory := make(map[uuid.UUID][]SellableItemResponse)

		for _, item := range snap.items {
			if item.RetiredAt.Valid || !item.Available {
				continue
			}

			itemSizes := sizesByItem[item.ID]
			var availableSizes []SellableSizeResponse
			for _, s := range itemSizes {
				if s.Available && !s.RetiredAt.Valid {
					availableSizes = append(availableSizes, SellableSizeResponse{
						ID:       s.ID,
						Name:     s.Name,
						PriceVND: s.PriceVnd,
					})
				}
			}

			hasDirectPrice := item.PriceVnd.Valid && item.PriceVnd.Int64 > 0
			availSizeCount := len(availableSizes)

			inherited := categoryGroupIDs[item.CategoryID]
			exclusions := itemExclusionIDs[item.ID]
			direct := itemDirectGroupIDs[item.ID]
			effGroupIDs := EffectiveGroupIDs(inherited, exclusions, direct)

			var effGroups []EffectiveGroup
			availOptionCounts := make(map[uuid.UUID]int)
			var sellableGroups []SellableModifierGroupResponse

			for _, gID := range effGroupIDs {
				grp, ok := groupByID[gID]
				if !ok || grp.RetiredAt.Valid {
					continue
				}

				effGroups = append(effGroups, EffectiveGroup{
					ID:            grp.ID,
					MinSelections: grp.MinSelections,
				})

				grpOpts := optionsByGroup[grp.ID]
				var sellableOpts []SellableModifierOptionResponse
				availOptIDSet := make(map[uuid.UUID]bool)

				for _, opt := range grpOpts {
					if opt.Available && !opt.RetiredAt.Valid {
						sellableOpts = append(sellableOpts, SellableModifierOptionResponse{
							ID:           opt.ID,
							Name:         opt.Name,
							SurchargeVND: opt.SurchargeVnd,
						})
						availOptIDSet[opt.ID] = true
					}
				}

				availOptionCounts[grp.ID] = len(sellableOpts)

				rawDefaults := defaultOptionIDsByGroup[grp.ID]
				var validDefaults []uuid.UUID
				for _, defID := range rawDefaults {
					if availOptIDSet[defID] {
						validDefaults = append(validDefaults, defID)
					}
				}
				sortUUIDs(validDefaults)
				if validDefaults == nil {
					validDefaults = []uuid.UUID{}
				}

				if sellableOpts == nil {
					sellableOpts = []SellableModifierOptionResponse{}
				}

				sellableGroups = append(sellableGroups, SellableModifierGroupResponse{
					ID:               grp.ID,
					Name:             grp.Name,
					MinSelections:    grp.MinSelections,
					MaxSelections:    grp.MaxSelections,
					Options:          sellableOpts,
					DefaultOptionIDs: validDefaults,
				})
			}

			state := ItemState{
				Available:             true,
				HasDirectPrice:        hasDirectPrice,
				AvailableSizeCount:    availSizeCount,
				EffectiveGroups:       effGroups,
				AvailableOptionCounts: availOptionCounts,
			}

			if !IsSellable(state) {
				continue
			}

			sortByNormalizedNameThenID(sellableGroups,
				func(g SellableModifierGroupResponse) string { return g.Name },
				func(g SellableModifierGroupResponse) uuid.UUID { return g.ID })

			var itemPrice *int64
			if hasDirectPrice {
				price := item.PriceVnd.Int64
				itemPrice = &price
			}

			itemsByCategory[item.CategoryID] = append(itemsByCategory[item.CategoryID], SellableItemResponse{
				ID:             item.ID,
				CategoryID:     item.CategoryID,
				Name:           item.Name,
				PriceVND:       itemPrice,
				Sizes:          availableSizes,
				ModifierGroups: sellableGroups,
			})
		}

		var resultCategories []SellableCategoryResponse
		for _, cat := range snap.categories {
			if cat.RetiredAt.Valid {
				continue
			}

			catItems, ok := itemsByCategory[cat.ID]
			if !ok || len(catItems) == 0 {
				continue
			}

			resultCategories = append(resultCategories, SellableCategoryResponse{
				ID:    cat.ID,
				Name:  cat.Name,
				Items: catItems,
			})
		}

		if resultCategories == nil {
			resultCategories = []SellableCategoryResponse{}
		}

		return SellableMenuResponse{Categories: resultCategories}, nil
	})
}

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

		groupByID := make(map[uuid.UUID]sqlc.ModifierGroup, len(snap.groups))
		for _, g := range snap.groups {
			groupByID[g.ID] = g
		}

		optionsByGroup := make(map[uuid.UUID][]ManagementModifierOptionResponse)
		for _, opt := range snap.options {
			optionsByGroup[opt.ModifierGroupID] = append(optionsByGroup[opt.ModifierGroupID], buildManagementModifierOption(opt))
		}

		defaultOptionIDsByGroup := make(map[uuid.UUID][]uuid.UUID)
		for _, d := range snap.defaults {
			defaultOptionIDsByGroup[d.ModifierGroupID] = append(defaultOptionIDsByGroup[d.ModifierGroupID], d.ModifierOptionID)
		}
		for grpID := range defaultOptionIDsByGroup {
			sortUUIDs(defaultOptionIDsByGroup[grpID])
		}

		categoryGroupIDs := make(map[uuid.UUID][]uuid.UUID)
		for _, cg := range snap.catGroups {
			categoryGroupIDs[cg.MenuCategoryID] = append(categoryGroupIDs[cg.MenuCategoryID], cg.ModifierGroupID)
		}
		for catID := range categoryGroupIDs {
			sortUUIDs(categoryGroupIDs[catID])
		}

		itemDirectGroupIDs := make(map[uuid.UUID][]uuid.UUID)
		for _, ig := range snap.itemGroups {
			itemDirectGroupIDs[ig.MenuItemID] = append(itemDirectGroupIDs[ig.MenuItemID], ig.ModifierGroupID)
		}
		for itemID := range itemDirectGroupIDs {
			sortUUIDs(itemDirectGroupIDs[itemID])
		}

		itemExclusionIDs := make(map[uuid.UUID][]uuid.UUID)
		for _, ie := range snap.exclusions {
			itemExclusionIDs[ie.MenuItemID] = append(itemExclusionIDs[ie.MenuItemID], ie.ModifierGroupID)
		}
		for itemID := range itemExclusionIDs {
			sortUUIDs(itemExclusionIDs[itemID])
		}

		sizesByItem := make(map[uuid.UUID][]ManagementSizeResponse)
		for _, s := range snap.sizes {
			var retAt *time.Time
			if s.RetiredAt.Valid {
				t := s.RetiredAt.Time
				retAt = &t
			}
			var reason, note *string
			if s.RetirementReason.Valid {
				r := s.RetirementReason.String
				reason = &r
			}
			if s.RetirementNote.Valid {
				n := s.RetirementNote.String
				note = &n
			}

			sizesByItem[s.MenuItemID] = append(sizesByItem[s.MenuItemID], ManagementSizeResponse{
				ID:               s.ID,
				MenuItemID:       s.MenuItemID,
				Name:             s.Name,
				PriceVND:         s.PriceVnd,
				Available:        s.Available,
				Retired:          s.RetiredAt.Valid,
				RetiredAt:        retAt,
				RetirementReason: reason,
				RetirementNote:   note,
			})
		}

		itemsByCategory := make(map[uuid.UUID][]ManagementItemResponse)
		for _, item := range snap.items {
			var retAt *time.Time
			if item.RetiredAt.Valid {
				t := item.RetiredAt.Time
				retAt = &t
			}
			var reason, note *string
			if item.RetirementReason.Valid {
				r := item.RetirementReason.String
				reason = &r
			}
			if item.RetirementNote.Valid {
				n := item.RetirementNote.String
				note = &n
			}

			var priceVND *int64
			if item.PriceVnd.Valid {
				p := item.PriceVnd.Int64
				priceVND = &p
			}

			itemSizes := sizesByItem[item.ID]
			if itemSizes == nil {
				itemSizes = []ManagementSizeResponse{}
			}

			directGroups := itemDirectGroupIDs[item.ID]
			if directGroups == nil {
				directGroups = []uuid.UUID{}
			}

			exclusions := itemExclusionIDs[item.ID]
			if exclusions == nil {
				exclusions = []uuid.UUID{}
			}

			inherited := categoryGroupIDs[item.CategoryID]
			effGroupIDs := EffectiveGroupIDs(inherited, exclusions, directGroups)

			var effGroups []ManagementModifierGroupResponse
			for _, gID := range effGroupIDs {
				grp, ok := groupByID[gID]
				if !ok {
					continue
				}

				grpDefaults := make([]uuid.UUID, len(defaultOptionIDsByGroup[grp.ID]))
				copy(grpDefaults, defaultOptionIDsByGroup[grp.ID])
				sortUUIDs(grpDefaults)

				effGroups = append(effGroups, buildManagementModifierGroup(grp, optionsByGroup[grp.ID], grpDefaults))
			}

			sortByNormalizedNameThenID(effGroups,
				func(g ManagementModifierGroupResponse) string { return g.Name },
				func(g ManagementModifierGroupResponse) uuid.UUID { return g.ID })

			if effGroups == nil {
				effGroups = []ManagementModifierGroupResponse{}
			}

			itemsByCategory[item.CategoryID] = append(itemsByCategory[item.CategoryID], ManagementItemResponse{
				ID:                       item.ID,
				CategoryID:               item.CategoryID,
				Name:                     item.Name,
				PriceVND:                 priceVND,
				Available:                item.Available,
				Retired:                  item.RetiredAt.Valid,
				RetiredAt:                retAt,
				RetirementReason:         reason,
				RetirementNote:           note,
				Sizes:                    itemSizes,
				DirectModifierGroupIDs:   directGroups,
				ExcludedModifierGroupIDs: exclusions,
				ModifierGroups:           effGroups,
			})
		}

		var resultCategories []ManagementCategoryResponse
		for _, cat := range snap.categories {
			var retAt *time.Time
			if cat.RetiredAt.Valid {
				t := cat.RetiredAt.Time
				retAt = &t
			}
			var reason, note *string
			if cat.RetirementReason.Valid {
				r := cat.RetirementReason.String
				reason = &r
			}
			if cat.RetirementNote.Valid {
				n := cat.RetirementNote.String
				note = &n
			}

			catItems := itemsByCategory[cat.ID]
			if catItems == nil {
				catItems = []ManagementItemResponse{}
			}

			modGroupIDs := categoryGroupIDs[cat.ID]
			if modGroupIDs == nil {
				modGroupIDs = []uuid.UUID{}
			}

			resultCategories = append(resultCategories, ManagementCategoryResponse{
				ID:               cat.ID,
				Name:             cat.Name,
				Retired:          cat.RetiredAt.Valid,
				RetiredAt:        retAt,
				RetirementReason: reason,
				RetirementNote:   note,
				ModifierGroupIDs: modGroupIDs,
				Items:            catItems,
			})
		}

		if resultCategories == nil {
			resultCategories = []ManagementCategoryResponse{}
		}

		return ManagementMenuResponse{Categories: resultCategories}, nil
	})
}

// AvailabilityMenuHandler handles reading the price-free availability menu projection.
type AvailabilityMenuHandler struct {
	runner *Runner
}

// NewAvailabilityMenuHandler creates a new AvailabilityMenuHandler.
func NewAvailabilityMenuHandler(runner *Runner) *AvailabilityMenuHandler {
	return &AvailabilityMenuHandler{runner: runner}
}

// Handle executes the availability menu projection query.
func (h *AvailabilityMenuHandler) Handle(ctx context.Context, actor Actor) (AvailabilityMenuResponse, error) {
	return ExecuteRead(ctx, h.runner, actor, CapManageAvailability, func(q *sqlc.Queries) (AvailabilityMenuResponse, error) {
		snap, err := loadCatalogSnapshot(ctx, q)
		if err != nil {
			return AvailabilityMenuResponse{}, err
		}

		groupByID := make(map[uuid.UUID]sqlc.ModifierGroup, len(snap.groups))
		for _, g := range snap.groups {
			groupByID[g.ID] = g
		}

		optionsByGroup := make(map[uuid.UUID][]AvailabilityModifierOptionResponse)
		for _, opt := range snap.options {
			if opt.RetiredAt.Valid {
				continue
			}
			optionsByGroup[opt.ModifierGroupID] = append(optionsByGroup[opt.ModifierGroupID], AvailabilityModifierOptionResponse{
				ID:        opt.ID,
				Name:      opt.Name,
				Available: opt.Available,
			})
		}

		categoryGroupIDs := make(map[uuid.UUID][]uuid.UUID)
		for _, cg := range snap.catGroups {
			categoryGroupIDs[cg.MenuCategoryID] = append(categoryGroupIDs[cg.MenuCategoryID], cg.ModifierGroupID)
		}

		itemDirectGroupIDs := make(map[uuid.UUID][]uuid.UUID)
		for _, ig := range snap.itemGroups {
			itemDirectGroupIDs[ig.MenuItemID] = append(itemDirectGroupIDs[ig.MenuItemID], ig.ModifierGroupID)
		}

		itemExclusionIDs := make(map[uuid.UUID][]uuid.UUID)
		for _, ie := range snap.exclusions {
			itemExclusionIDs[ie.MenuItemID] = append(itemExclusionIDs[ie.MenuItemID], ie.ModifierGroupID)
		}

		sizesByItem := make(map[uuid.UUID][]AvailabilitySizeResponse)
		for _, s := range snap.sizes {
			if s.RetiredAt.Valid {
				continue
			}
			sizesByItem[s.MenuItemID] = append(sizesByItem[s.MenuItemID], AvailabilitySizeResponse{
				ID:        s.ID,
				Name:      s.Name,
				Available: s.Available,
			})
		}

		itemsByCategory := make(map[uuid.UUID][]AvailabilityItemResponse)
		for _, item := range snap.items {
			if item.RetiredAt.Valid {
				continue
			}

			itemSizes := sizesByItem[item.ID]
			if itemSizes == nil {
				itemSizes = []AvailabilitySizeResponse{}
			}

			inherited := categoryGroupIDs[item.CategoryID]
			exclusions := itemExclusionIDs[item.ID]
			direct := itemDirectGroupIDs[item.ID]
			effGroupIDs := EffectiveGroupIDs(inherited, exclusions, direct)

			var effGroups []AvailabilityModifierGroupResponse
			for _, gID := range effGroupIDs {
				grp, ok := groupByID[gID]
				if !ok || grp.RetiredAt.Valid {
					continue
				}

				grpOpts := optionsByGroup[grp.ID]
				if grpOpts == nil {
					grpOpts = []AvailabilityModifierOptionResponse{}
				}

				effGroups = append(effGroups, AvailabilityModifierGroupResponse{
					ID:            grp.ID,
					Name:          grp.Name,
					MinSelections: grp.MinSelections,
					MaxSelections: grp.MaxSelections,
					Options:       grpOpts,
				})
			}

			sortByNormalizedNameThenID(effGroups,
				func(g AvailabilityModifierGroupResponse) string { return g.Name },
				func(g AvailabilityModifierGroupResponse) uuid.UUID { return g.ID })

			if effGroups == nil {
				effGroups = []AvailabilityModifierGroupResponse{}
			}

			itemsByCategory[item.CategoryID] = append(itemsByCategory[item.CategoryID], AvailabilityItemResponse{
				ID:             item.ID,
				CategoryID:     item.CategoryID,
				Name:           item.Name,
				Available:      item.Available,
				Sizes:          itemSizes,
				ModifierGroups: effGroups,
			})
		}

		var resultCategories []AvailabilityCategoryResponse
		for _, cat := range snap.categories {
			if cat.RetiredAt.Valid {
				continue
			}

			catItems := itemsByCategory[cat.ID]
			if catItems == nil {
				catItems = []AvailabilityItemResponse{}
			}

			resultCategories = append(resultCategories, AvailabilityCategoryResponse{
				ID:    cat.ID,
				Name:  cat.Name,
				Items: catItems,
			})
		}

		if resultCategories == nil {
			resultCategories = []AvailabilityCategoryResponse{}
		}

		return AvailabilityMenuResponse{Categories: resultCategories}, nil
	})
}

// ModifierGroupsHandler handles reading modifier groups for management.
type ModifierGroupsHandler struct {
	runner *Runner
}

// NewModifierGroupsHandler creates a new ModifierGroupsHandler.
func NewModifierGroupsHandler(runner *Runner) *ModifierGroupsHandler {
	return &ModifierGroupsHandler{runner: runner}
}

// Handle executes the modifier groups query.
func (h *ModifierGroupsHandler) Handle(ctx context.Context, actor Actor) ([]ManagementModifierGroupResponse, error) {
	return ExecuteRead(ctx, h.runner, actor, CapViewPrices, func(q *sqlc.Queries) ([]ManagementModifierGroupResponse, error) {
		groups, err := q.ListModifierGroups(ctx)
		if err != nil {
			return nil, err
		}

		options, err := q.ListAllModifierOptions(ctx)
		if err != nil {
			return nil, err
		}

		defaultOpts, err := q.ListAllModifierGroupDefaultOptions(ctx)
		if err != nil {
			return nil, err
		}

		optionsByGroup := make(map[uuid.UUID][]ManagementModifierOptionResponse)
		for _, opt := range options {
			optionsByGroup[opt.ModifierGroupID] = append(optionsByGroup[opt.ModifierGroupID], buildManagementModifierOption(opt))
		}

		defaultOptionIDsByGroup := make(map[uuid.UUID][]uuid.UUID)
		for _, d := range defaultOpts {
			defaultOptionIDsByGroup[d.ModifierGroupID] = append(defaultOptionIDsByGroup[d.ModifierGroupID], d.ModifierOptionID)
		}
		for grpID := range defaultOptionIDsByGroup {
			sortUUIDs(defaultOptionIDsByGroup[grpID])
		}

		var result []ManagementModifierGroupResponse
		for _, grp := range groups {
			defaults := make([]uuid.UUID, len(defaultOptionIDsByGroup[grp.ID]))
			copy(defaults, defaultOptionIDsByGroup[grp.ID])
			sortUUIDs(defaults)

			result = append(result, buildManagementModifierGroup(grp, optionsByGroup[grp.ID], defaults))
		}

		if result == nil {
			result = []ManagementModifierGroupResponse{}
		}

		return result, nil
	})
}

func sortUUIDs(ids []uuid.UUID) {
	sort.Slice(ids, func(i, j int) bool {
		return ids[i].String() < ids[j].String()
	})
}

func buildManagementModifierOption(opt sqlc.ModifierOption) ManagementModifierOptionResponse {
	var retAt *time.Time
	if opt.RetiredAt.Valid {
		t := opt.RetiredAt.Time
		retAt = &t
	}
	var reason, note *string
	if opt.RetirementReason.Valid {
		r := opt.RetirementReason.String
		reason = &r
	}
	if opt.RetirementNote.Valid {
		n := opt.RetirementNote.String
		note = &n
	}
	return ManagementModifierOptionResponse{
		ID:               opt.ID,
		ModifierGroupID:  opt.ModifierGroupID,
		Name:             opt.Name,
		SurchargeVND:     opt.SurchargeVnd,
		Available:        opt.Available,
		Retired:          opt.RetiredAt.Valid,
		RetiredAt:        retAt,
		RetirementReason: reason,
		RetirementNote:   note,
	}
}

func buildManagementModifierGroup(grp sqlc.ModifierGroup, options []ManagementModifierOptionResponse, defaultIDs []uuid.UUID) ManagementModifierGroupResponse {
	var retAt *time.Time
	if grp.RetiredAt.Valid {
		t := grp.RetiredAt.Time
		retAt = &t
	}
	var reason, note *string
	if grp.RetirementReason.Valid {
		r := grp.RetirementReason.String
		reason = &r
	}
	if grp.RetirementNote.Valid {
		n := grp.RetirementNote.String
		note = &n
	}
	if options == nil {
		options = []ManagementModifierOptionResponse{}
	}
	return ManagementModifierGroupResponse{
		ID:               grp.ID,
		Name:             grp.Name,
		MinSelections:    grp.MinSelections,
		MaxSelections:    grp.MaxSelections,
		Retired:          grp.RetiredAt.Valid,
		RetiredAt:        retAt,
		RetirementReason: reason,
		RetirementNote:   note,
		Options:          options,
		DefaultOptionIDs: defaultIDs,
	}
}

// sortByNormalizedNameThenID sorts items in place by the normalized name of each
// item (the display value of NormalizeName(nameOf(item))), then idOf(item).String(),
// computing each key exactly once per element instead of on every comparison.
func sortByNormalizedNameThenID[T any](items []T, nameOf func(T) string, idOf func(T) uuid.UUID) {
	type keyed struct {
		key string
		id  string
		idx int
	}
	keys := make([]keyed, len(items))
	for i, it := range items {
		key, _ := NormalizeName(nameOf(it))
		keys[i] = keyed{key: key, id: idOf(it).String(), idx: i}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].key != keys[j].key {
			return keys[i].key < keys[j].key
		}
		return keys[i].id < keys[j].id
	})
	sorted := make([]T, len(items))
	for i, k := range keys {
		sorted[i] = items[k.idx]
	}
	copy(items, sorted)
}
