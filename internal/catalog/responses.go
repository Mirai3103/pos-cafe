package catalog

import (
	"context"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

// newItemResponse builds the command response for a menu item. Sizes are
// omitted when the item has none.
func newItemResponse(item sqlc.MenuItem, sizes []sqlc.MenuItemSize) ItemResponse {
	res := ItemResponse{
		ID:         item.ID,
		CategoryID: item.CategoryID,
		Name:       item.Name,
		PriceVND:   nullInt64Ptr(item.PriceVnd),
		Available:  item.Available,
	}
	if len(sizes) > 0 {
		res.Sizes = make([]SizeResponse, len(sizes))
		for i, s := range sizes {
			res.Sizes[i] = SizeResponse{
				ID:        s.ID,
				Name:      s.Name,
				PriceVND:  s.PriceVnd,
				Available: s.Available,
			}
		}
	}
	return res
}

// loadItemResponse lists the item's sizes and builds its command response.
func loadItemResponse(ctx context.Context, q *sqlc.Queries, item sqlc.MenuItem) (ItemResponse, error) {
	sizes, err := q.ListMenuItemSizesByItem(ctx, item.ID)
	if err != nil {
		return ItemResponse{}, MapDBError(err)
	}
	return newItemResponse(item, sizes), nil
}

// newModifierGroupResponse builds the command response for a modifier group.
func newModifierGroupResponse(group sqlc.ModifierGroup, options []sqlc.ModifierOption, defaultIDs []uuid.UUID) ModifierGroupResponse {
	res := ModifierGroupResponse{
		ID:               group.ID,
		Name:             group.Name,
		MinSelections:    group.MinSelections,
		MaxSelections:    group.MaxSelections,
		Options:          make([]ModifierOptionResponse, len(options)),
		DefaultOptionIDs: defaultIDs,
	}
	for i, o := range options {
		res.Options[i] = ModifierOptionResponse{
			ID:              o.ID,
			ModifierGroupID: o.ModifierGroupID,
			Name:            o.Name,
			SurchargeVND:    o.SurchargeVnd,
			Available:       o.Available,
		}
	}
	return res
}

// loadModifierGroupResponse lists the group's options, then its default
// options, and builds its command response.
func loadModifierGroupResponse(ctx context.Context, q *sqlc.Queries, group sqlc.ModifierGroup) (ModifierGroupResponse, error) {
	options, err := q.ListModifierOptionsByGroup(ctx, group.ID)
	if err != nil {
		return ModifierGroupResponse{}, MapDBError(err)
	}
	defaultRows, err := q.ListModifierGroupDefaultOptionsByGroup(ctx, group.ID)
	if err != nil {
		return ModifierGroupResponse{}, MapDBError(err)
	}
	defaultIDs := make([]uuid.UUID, len(defaultRows))
	for i, d := range defaultRows {
		defaultIDs[i] = d.ModifierOptionID
	}
	return newModifierGroupResponse(group, options, defaultIDs), nil
}
