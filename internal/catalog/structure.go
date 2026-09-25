package catalog

import (
	"context"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
)

type moveItemCategoryFingerprint struct {
	ItemID     uuid.UUID `json:"item_id"`
	CategoryID uuid.UUID `json:"category_id"`
}

type itemCategoryChangedAudit struct {
	ItemID                   uuid.UUID   `json:"item_id"`
	FromCategoryID           uuid.UUID   `json:"from_category_id"`
	ToCategoryID             uuid.UUID   `json:"to_category_id"`
	RemovedExclusionGroupIDs []uuid.UUID `json:"removed_exclusion_group_ids"`
}

// MoveItemCategoryHandler moves a Menu Item to another category.
type MoveItemCategoryHandler struct {
	runner *Runner
}

// NewMoveItemCategoryHandler creates a MoveItemCategoryHandler.
func NewMoveItemCategoryHandler(runner *Runner) *MoveItemCategoryHandler {
	return &MoveItemCategoryHandler{runner: runner}
}

// Handle locks the item, then both categories in id order, moves the item, and
// deletes exclusions the target category does not provide (ADR-059).
func (h *MoveItemCategoryHandler) Handle(ctx context.Context, actor Actor, cmd MoveItemCategoryCommand) (int, ItemCategoryResponse, error) {
	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpItemMoveCategory,
		Fingerprint: moveItemCategoryFingerprint{ItemID: cmd.ItemID, CategoryID: cmd.CategoryID},
		Required:    []string{CapAdministerStructure},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec, func(q *sqlc.Queries) (int, ItemCategoryResponse, AuditRecord, error) {
		item, err := q.GetMenuItemForUpdate(ctx, cmd.ItemID)
		if err != nil {
			return 0, ItemCategoryResponse{}, AuditRecord{}, MapDBError(err)
		}
		if item.RetiredAt.Valid {
			return 0, ItemCategoryResponse{}, AuditRecord{}, fmt.Errorf("%w: item is retired", ErrEntityRetired)
		}
		if item.CategoryID == cmd.CategoryID {
			return 200, ItemCategoryResponse{ItemID: item.ID, CategoryID: item.CategoryID, RemovedExclusionGroupIDs: []uuid.UUID{}}, AuditRecord{}, nil
		}

		for _, catID := range UnionIDs([]uuid.UUID{item.CategoryID, cmd.CategoryID}) {
			cat, err := q.GetMenuCategoryForUpdate(ctx, catID)
			if err != nil {
				return 0, ItemCategoryResponse{}, AuditRecord{}, MapDBError(err)
			}
			if catID == cmd.CategoryID && cat.RetiredAt.Valid {
				return 0, ItemCategoryResponse{}, AuditRecord{}, fmt.Errorf("%w: target category is retired", ErrEntityRetired)
			}
		}

		moved, err := q.MoveMenuItemToCategory(ctx, sqlc.MoveMenuItemToCategoryParams{ID: cmd.ItemID, CategoryID: cmd.CategoryID})
		if err != nil {
			return 0, ItemCategoryResponse{}, AuditRecord{}, MapDBError(err)
		}
		removed, err := q.DeleteItemExclusionsOutsideCategory(ctx, sqlc.DeleteItemExclusionsOutsideCategoryParams{
			ItemID: cmd.ItemID, CategoryID: cmd.CategoryID,
		})
		if err != nil {
			return 0, ItemCategoryResponse{}, AuditRecord{}, MapDBError(err)
		}
		removed = UnionIDs(removed)

		res := ItemCategoryResponse{ItemID: moved.ID, CategoryID: moved.CategoryID, RemovedExclusionGroupIDs: removed}
		audit := AuditRecord{
			EventType: EventItemCategoryChanged,
			Details: itemCategoryChangedAudit{
				ItemID: moved.ID, FromCategoryID: item.CategoryID, ToCategoryID: moved.CategoryID, RemovedExclusionGroupIDs: removed,
			},
		}
		return 200, res, audit, nil
	})
}

type addSizeFingerprint struct {
	ItemID   uuid.UUID `json:"item_id"`
	Name     string    `json:"name"`
	PriceVND int64     `json:"price_vnd"`
}

type sizeCreatedAudit struct {
	SizeID     uuid.UUID `json:"size_id"`
	MenuItemID uuid.UUID `json:"menu_item_id"`
	Name       string    `json:"name"`
	PriceVND   int64     `json:"price_vnd"`
}

// AddSizeHandler adds a Size to an item that is already sized (ADR-060).
type AddSizeHandler struct {
	runner *Runner
}

// NewAddSizeHandler creates an AddSizeHandler.
func NewAddSizeHandler(runner *Runner) *AddSizeHandler {
	return &AddSizeHandler{runner: runner}
}

// Handle requires a fresh Manager PIN because it sets a price.
func (h *AddSizeHandler) Handle(ctx context.Context, actor Actor, cmd AddSizeCommand) (int, SizeResponse, error) {
	display, key := NormalizeName(cmd.Name)
	spec := MutationSpec{
		RequestID:         cmd.RequestID,
		Operation:         OpSizeCreate,
		Fingerprint:       addSizeFingerprint{ItemID: cmd.ItemID, Name: display, PriceVND: cmd.PriceVND},
		Required:          []string{CapAdministerStructure, CapChangePrice},
		RequireManagerPIN: true,
		ManagerPIN:        cmd.ManagerPIN,
	}

	return ExecuteMutation(ctx, h.runner, actor, spec, func(q *sqlc.Queries) (int, SizeResponse, AuditRecord, error) {
		item, err := q.GetMenuItemForUpdate(ctx, cmd.ItemID)
		if err != nil {
			return 0, SizeResponse{}, AuditRecord{}, MapDBError(err)
		}
		if item.RetiredAt.Valid {
			return 0, SizeResponse{}, AuditRecord{}, fmt.Errorf("%w: item is retired", ErrEntityRetired)
		}
		if item.PriceVnd.Valid {
			return 0, SizeResponse{}, AuditRecord{}, fmt.Errorf("%w: a single-price item has no sizes", ErrInvalidPricingConfiguration)
		}
		if display == "" {
			return 0, SizeResponse{}, AuditRecord{}, fmt.Errorf("%w: size name cannot be empty", response.ErrInvalid)
		}
		if err := ValidatePrice(cmd.PriceVND); err != nil {
			return 0, SizeResponse{}, AuditRecord{}, fmt.Errorf("%w: %s", ErrInvalidPricingConfiguration, err.Error())
		}

		size, err := q.CreateMenuItemSize(ctx, sqlc.CreateMenuItemSizeParams{
			MenuItemID: cmd.ItemID, Name: display, NormalizedName: key, PriceVnd: cmd.PriceVND, Available: true,
		})
		if err != nil {
			return 0, SizeResponse{}, AuditRecord{}, MapDBError(err)
		}

		res := SizeResponse{ID: size.ID, Name: size.Name, PriceVND: size.PriceVnd, Available: size.Available}
		audit := AuditRecord{
			EventType: EventSizeCreated,
			Details:   sizeCreatedAudit{SizeID: size.ID, MenuItemID: cmd.ItemID, Name: size.Name, PriceVND: size.PriceVnd},
		}
		return 201, res, audit, nil
	})
}

type addModifierOptionFingerprint struct {
	GroupID      uuid.UUID `json:"group_id"`
	Name         string    `json:"name"`
	SurchargeVND int64     `json:"surcharge_vnd"`
}

type modifierOptionCreatedAudit struct {
	OptionID        uuid.UUID `json:"option_id"`
	ModifierGroupID uuid.UUID `json:"modifier_group_id"`
	Name            string    `json:"name"`
	SurchargeVND    int64     `json:"surcharge_vnd"`
}

// AddModifierOptionHandler adds an Option to an existing modifier group.
type AddModifierOptionHandler struct {
	runner *Runner
}

// NewAddModifierOptionHandler creates an AddModifierOptionHandler.
func NewAddModifierOptionHandler(runner *Runner) *AddModifierOptionHandler {
	return &AddModifierOptionHandler{runner: runner}
}

// Handle requires a fresh Manager PIN because it sets a surcharge.
func (h *AddModifierOptionHandler) Handle(ctx context.Context, actor Actor, cmd AddModifierOptionCommand) (int, ModifierOptionResponse, error) {
	display, key := NormalizeName(cmd.Name)
	spec := MutationSpec{
		RequestID:         cmd.RequestID,
		Operation:         OpModifierOptionCreate,
		Fingerprint:       addModifierOptionFingerprint{GroupID: cmd.GroupID, Name: display, SurchargeVND: cmd.SurchargeVND},
		Required:          []string{CapAdministerStructure, CapChangePrice},
		RequireManagerPIN: true,
		ManagerPIN:        cmd.ManagerPIN,
	}

	return ExecuteMutation(ctx, h.runner, actor, spec, func(q *sqlc.Queries) (int, ModifierOptionResponse, AuditRecord, error) {
		group, err := q.GetModifierGroupForUpdate(ctx, cmd.GroupID)
		if err != nil {
			return 0, ModifierOptionResponse{}, AuditRecord{}, MapDBError(err)
		}
		if group.RetiredAt.Valid {
			return 0, ModifierOptionResponse{}, AuditRecord{}, fmt.Errorf("%w: modifier group is retired", ErrEntityRetired)
		}
		if display == "" {
			return 0, ModifierOptionResponse{}, AuditRecord{}, fmt.Errorf("%w: option name cannot be empty", response.ErrInvalid)
		}
		if err := ValidateSurcharge(cmd.SurchargeVND); err != nil {
			return 0, ModifierOptionResponse{}, AuditRecord{}, fmt.Errorf("%w: %s", ErrInvalidPricingConfiguration, err.Error())
		}

		opt, err := q.CreateModifierOption(ctx, sqlc.CreateModifierOptionParams{
			ModifierGroupID: cmd.GroupID, Name: display, NormalizedName: key, SurchargeVnd: cmd.SurchargeVND, Available: true,
		})
		if err != nil {
			return 0, ModifierOptionResponse{}, AuditRecord{}, MapDBError(err)
		}

		res := ModifierOptionResponse{ID: opt.ID, ModifierGroupID: opt.ModifierGroupID, Name: opt.Name, SurchargeVND: opt.SurchargeVnd, Available: opt.Available}
		audit := AuditRecord{
			EventType: EventModifierOptionCreated,
			Details:   modifierOptionCreatedAudit{OptionID: opt.ID, ModifierGroupID: opt.ModifierGroupID, Name: opt.Name, SurchargeVND: opt.SurchargeVnd},
		}
		return 201, res, audit, nil
	})
}

type selectionRuleFingerprint struct {
	GroupID          uuid.UUID   `json:"group_id"`
	MinSelections    int32       `json:"min_selections"`
	MaxSelections    int32       `json:"max_selections"`
	DefaultOptionIDs []uuid.UUID `json:"default_option_ids"`
}

type selectionRuleValues struct {
	MinSelections    int32       `json:"min_selections"`
	MaxSelections    int32       `json:"max_selections"`
	DefaultOptionIDs []uuid.UUID `json:"default_option_ids"`
}

type selectionRuleChangedAudit struct {
	GroupID uuid.UUID           `json:"group_id"`
	Before  selectionRuleValues `json:"before"`
	After   selectionRuleValues `json:"after"`
}

// SetSelectionRuleHandler changes a group's min, max, and defaults atomically,
// so no intermediate state is ever invalid.
type SetSelectionRuleHandler struct {
	runner *Runner
}

// NewSetSelectionRuleHandler creates a SetSelectionRuleHandler.
func NewSetSelectionRuleHandler(runner *Runner) *SetSelectionRuleHandler {
	return &SetSelectionRuleHandler{runner: runner}
}

// Handle validates against the group's non-retired options.
func (h *SetSelectionRuleHandler) Handle(ctx context.Context, actor Actor, cmd SetSelectionRuleCommand) (int, SelectionRuleResponse, error) {
	defaults, err := NormalizeIDSet(cmd.DefaultOptionIDs, "default_option_ids")
	if err != nil {
		return 0, SelectionRuleResponse{}, fmt.Errorf("%w: %s", ErrInvalidModifierConfiguration, err.Error())
	}
	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpModifierGroupSetSelectionRule,
		Fingerprint: selectionRuleFingerprint{
			GroupID: cmd.GroupID, MinSelections: cmd.MinSelections, MaxSelections: cmd.MaxSelections, DefaultOptionIDs: defaults,
		},
		Required: []string{CapAdministerStructure},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec, func(q *sqlc.Queries) (int, SelectionRuleResponse, AuditRecord, error) {
		group, err := q.GetModifierGroupForUpdate(ctx, cmd.GroupID)
		if err != nil {
			return 0, SelectionRuleResponse{}, AuditRecord{}, MapDBError(err)
		}
		if group.RetiredAt.Valid {
			return 0, SelectionRuleResponse{}, AuditRecord{}, fmt.Errorf("%w: modifier group is retired", ErrEntityRetired)
		}

		options, err := q.ListModifierOptionsByGroup(ctx, cmd.GroupID)
		if err != nil {
			return 0, SelectionRuleResponse{}, AuditRecord{}, MapDBError(err)
		}
		active := 0
		optByID := make(map[uuid.UUID]sqlc.ModifierOption, len(options))
		for _, o := range options {
			optByID[o.ID] = o
			if !o.RetiredAt.Valid {
				active++
			}
		}
		if err := ValidateSelectionRule(cmd.MinSelections, cmd.MaxSelections, active, len(defaults)); err != nil {
			return 0, SelectionRuleResponse{}, AuditRecord{}, fmt.Errorf("%w: %s", ErrInvalidModifierConfiguration, err.Error())
		}
		for _, id := range defaults {
			o, ok := optByID[id]
			if !ok || o.RetiredAt.Valid || !o.Available {
				return 0, SelectionRuleResponse{}, AuditRecord{}, fmt.Errorf("%w: default option %s must belong to the group and be available", ErrInvalidModifierConfiguration, id)
			}
		}

		beforeRows, err := q.ListModifierGroupDefaultOptionsByGroup(ctx, cmd.GroupID)
		if err != nil {
			return 0, SelectionRuleResponse{}, AuditRecord{}, MapDBError(err)
		}
		beforeDefaults := make([]uuid.UUID, 0, len(beforeRows))
		for _, r := range beforeRows {
			beforeDefaults = append(beforeDefaults, r.ModifierOptionID)
		}

		updated, err := q.UpdateModifierGroupBounds(ctx, sqlc.UpdateModifierGroupBoundsParams{
			ID: cmd.GroupID, MinSelections: cmd.MinSelections, MaxSelections: cmd.MaxSelections,
		})
		if err != nil {
			return 0, SelectionRuleResponse{}, AuditRecord{}, MapDBError(err)
		}
		if err := q.DeleteModifierGroupDefaultOptions(ctx, cmd.GroupID); err != nil {
			return 0, SelectionRuleResponse{}, AuditRecord{}, MapDBError(err)
		}
		if len(defaults) > 0 {
			if err := q.CreateModifierGroupDefaultOptions(ctx, sqlc.CreateModifierGroupDefaultOptionsParams{
				ModifierGroupID: cmd.GroupID, OptionIds: defaults,
			}); err != nil {
				return 0, SelectionRuleResponse{}, AuditRecord{}, MapDBError(err)
			}
		}

		res := SelectionRuleResponse{
			GroupID: updated.ID, MinSelections: updated.MinSelections, MaxSelections: updated.MaxSelections, DefaultOptionIDs: defaults,
		}
		audit := AuditRecord{
			EventType: EventModifierGroupSelectionRuleChanged,
			Details: selectionRuleChangedAudit{
				GroupID: updated.ID,
				Before:  selectionRuleValues{MinSelections: group.MinSelections, MaxSelections: group.MaxSelections, DefaultOptionIDs: UnionIDs(beforeDefaults)},
				After:   selectionRuleValues{MinSelections: res.MinSelections, MaxSelections: res.MaxSelections, DefaultOptionIDs: defaults},
			},
		}
		return 200, res, audit, nil
	})
}
