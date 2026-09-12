package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

type attachItemModifierGroupFingerprint struct {
	ItemID          uuid.UUID `json:"item_id"`
	ModifierGroupID uuid.UUID `json:"modifier_group_id"`
}

type itemModifierGroupAttachedAuditDetails struct {
	ItemID          uuid.UUID `json:"item_id"`
	ModifierGroupID uuid.UUID `json:"modifier_group_id"`
}

// AttachItemModifierGroupHandler handles attaching a modifier group directly to an item.
type AttachItemModifierGroupHandler struct {
	runner *Runner
}

// NewAttachItemModifierGroupHandler creates a new AttachItemModifierGroupHandler.
func NewAttachItemModifierGroupHandler(runner *Runner) *AttachItemModifierGroupHandler {
	return &AttachItemModifierGroupHandler{runner: runner}
}

// Handle executes the attach item modifier group command.
func (h *AttachItemModifierGroupHandler) Handle(ctx context.Context, actor Actor, cmd AttachItemModifierGroupCommand) (int, ItemModifierGroupResponse, error) {
	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpItemAttachModifierGroup,
		Fingerprint: attachItemModifierGroupFingerprint{
			ItemID:          cmd.ItemID,
			ModifierGroupID: cmd.ModifierGroupID,
		},
		Required: []string{CapAdministerStructure},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec, func(q *sqlc.Queries) (int, ItemModifierGroupResponse, AuditRecord, error) {
		// 1. Lock owner (item)
		item, err := q.GetMenuItemForUpdate(ctx, cmd.ItemID)
		if err != nil {
			return 0, ItemModifierGroupResponse{}, AuditRecord{}, MapDBError(err)
		}
		if item.RetiredAt.Valid {
			return 0, ItemModifierGroupResponse{}, AuditRecord{}, fmt.Errorf("%w: item is retired", ErrEntityRetired)
		}

		// 2. Lock group
		group, err := q.GetModifierGroupForUpdate(ctx, cmd.ModifierGroupID)
		if err != nil {
			return 0, ItemModifierGroupResponse{}, AuditRecord{}, MapDBError(err)
		}
		if group.RetiredAt.Valid {
			return 0, ItemModifierGroupResponse{}, AuditRecord{}, fmt.Errorf("%w: modifier group is retired", ErrEntityRetired)
		}

		// 3. Create assignment
		err = q.CreateItemModifierGroup(ctx, sqlc.CreateItemModifierGroupParams{
			MenuItemID:      cmd.ItemID,
			ModifierGroupID: cmd.ModifierGroupID,
		})
		if err != nil {
			return 0, ItemModifierGroupResponse{}, AuditRecord{}, MapDBError(err)
		}

		res := ItemModifierGroupResponse{
			ItemID:          cmd.ItemID,
			ModifierGroupID: cmd.ModifierGroupID,
		}
		audit := AuditRecord{
			EventType: EventItemModifierGroupAttached,
			Details: itemModifierGroupAttachedAuditDetails{
				ItemID:          cmd.ItemID,
				ModifierGroupID: cmd.ModifierGroupID,
			},
		}
		return 200, res, audit, nil
	})
}

type attachCategoryModifierGroupFingerprint struct {
	CategoryID      uuid.UUID `json:"category_id"`
	ModifierGroupID uuid.UUID `json:"modifier_group_id"`
}

type categoryModifierGroupAttachedAuditDetails struct {
	CategoryID      uuid.UUID `json:"category_id"`
	ModifierGroupID uuid.UUID `json:"modifier_group_id"`
}

// AttachCategoryModifierGroupHandler handles attaching a modifier group to a category.
type AttachCategoryModifierGroupHandler struct {
	runner *Runner
}

// NewAttachCategoryModifierGroupHandler creates a new AttachCategoryModifierGroupHandler.
func NewAttachCategoryModifierGroupHandler(runner *Runner) *AttachCategoryModifierGroupHandler {
	return &AttachCategoryModifierGroupHandler{runner: runner}
}

// Handle executes the attach category modifier group command.
func (h *AttachCategoryModifierGroupHandler) Handle(ctx context.Context, actor Actor, cmd AttachCategoryModifierGroupCommand) (int, CategoryModifierGroupResponse, error) {
	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpCategoryAttachModifierGroup,
		Fingerprint: attachCategoryModifierGroupFingerprint{
			CategoryID:      cmd.CategoryID,
			ModifierGroupID: cmd.ModifierGroupID,
		},
		Required: []string{CapAdministerStructure},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec, func(q *sqlc.Queries) (int, CategoryModifierGroupResponse, AuditRecord, error) {
		// 1. Lock owner (category)
		category, err := q.GetMenuCategoryForUpdate(ctx, cmd.CategoryID)
		if err != nil {
			return 0, CategoryModifierGroupResponse{}, AuditRecord{}, MapDBError(err)
		}
		if category.RetiredAt.Valid {
			return 0, CategoryModifierGroupResponse{}, AuditRecord{}, fmt.Errorf("%w: category is retired", ErrEntityRetired)
		}

		// 2. Lock group
		group, err := q.GetModifierGroupForUpdate(ctx, cmd.ModifierGroupID)
		if err != nil {
			return 0, CategoryModifierGroupResponse{}, AuditRecord{}, MapDBError(err)
		}
		if group.RetiredAt.Valid {
			return 0, CategoryModifierGroupResponse{}, AuditRecord{}, fmt.Errorf("%w: modifier group is retired", ErrEntityRetired)
		}

		// 3. Create assignment
		err = q.CreateCategoryModifierGroup(ctx, sqlc.CreateCategoryModifierGroupParams{
			MenuCategoryID:  cmd.CategoryID,
			ModifierGroupID: cmd.ModifierGroupID,
		})
		if err != nil {
			return 0, CategoryModifierGroupResponse{}, AuditRecord{}, MapDBError(err)
		}

		res := CategoryModifierGroupResponse{
			CategoryID:      cmd.CategoryID,
			ModifierGroupID: cmd.ModifierGroupID,
		}
		audit := AuditRecord{
			EventType: EventCategoryModifierGroupAttached,
			Details: categoryModifierGroupAttachedAuditDetails{
				CategoryID:      cmd.CategoryID,
				ModifierGroupID: cmd.ModifierGroupID,
			},
		}
		return 200, res, audit, nil
	})
}

type excludeInheritedModifierGroupFingerprint struct {
	ItemID          uuid.UUID `json:"item_id"`
	ModifierGroupID uuid.UUID `json:"modifier_group_id"`
}

type itemInheritedModifierGroupExcludedAuditDetails struct {
	ItemID          uuid.UUID `json:"item_id"`
	ModifierGroupID uuid.UUID `json:"modifier_group_id"`
}

// ExcludeInheritedModifierGroupHandler handles excluding an inherited modifier group from an item.
type ExcludeInheritedModifierGroupHandler struct {
	runner *Runner
}

// NewExcludeInheritedModifierGroupHandler creates a new ExcludeInheritedModifierGroupHandler.
func NewExcludeInheritedModifierGroupHandler(runner *Runner) *ExcludeInheritedModifierGroupHandler {
	return &ExcludeInheritedModifierGroupHandler{runner: runner}
}

// Handle executes the exclude inherited modifier group command.
func (h *ExcludeInheritedModifierGroupHandler) Handle(ctx context.Context, actor Actor, cmd ExcludeInheritedModifierGroupCommand) (int, ItemModifierGroupExclusionResponse, error) {
	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpItemExcludeInheritedModifierGroup,
		Fingerprint: excludeInheritedModifierGroupFingerprint{
			ItemID:          cmd.ItemID,
			ModifierGroupID: cmd.ModifierGroupID,
		},
		Required: []string{CapAdministerStructure},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec, func(q *sqlc.Queries) (int, ItemModifierGroupExclusionResponse, AuditRecord, error) {
		// 1. Lock owner (item)
		item, err := q.GetMenuItemForUpdate(ctx, cmd.ItemID)
		if err != nil {
			return 0, ItemModifierGroupExclusionResponse{}, AuditRecord{}, MapDBError(err)
		}
		if item.RetiredAt.Valid {
			return 0, ItemModifierGroupExclusionResponse{}, AuditRecord{}, fmt.Errorf("%w: item is retired", ErrEntityRetired)
		}

		// 2. Lock group
		group, err := q.GetModifierGroupForUpdate(ctx, cmd.ModifierGroupID)
		if err != nil {
			return 0, ItemModifierGroupExclusionResponse{}, AuditRecord{}, MapDBError(err)
		}
		if group.RetiredAt.Valid {
			return 0, ItemModifierGroupExclusionResponse{}, AuditRecord{}, fmt.Errorf("%w: modifier group is retired", ErrEntityRetired)
		}

		// 3. Verify that item's category is currently assigned to this modifier group
		_, err = q.GetCategoryModifierGroup(ctx, sqlc.GetCategoryModifierGroupParams{
			MenuCategoryID:  item.CategoryID,
			ModifierGroupID: cmd.ModifierGroupID,
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return 0, ItemModifierGroupExclusionResponse{}, AuditRecord{}, fmt.Errorf("%w: modifier group is not assigned to item category", ErrInvalidInheritance)
			}
			return 0, ItemModifierGroupExclusionResponse{}, AuditRecord{}, MapDBError(err)
		}

		// 4. Create exclusion
		err = q.CreateItemModifierGroupExclusion(ctx, sqlc.CreateItemModifierGroupExclusionParams{
			MenuItemID:      cmd.ItemID,
			ModifierGroupID: cmd.ModifierGroupID,
		})
		if err != nil {
			return 0, ItemModifierGroupExclusionResponse{}, AuditRecord{}, MapDBError(err)
		}

		res := ItemModifierGroupExclusionResponse{
			ItemID:          cmd.ItemID,
			ModifierGroupID: cmd.ModifierGroupID,
		}
		audit := AuditRecord{
			EventType: EventItemInheritedModifierGroupExcluded,
			Details: itemInheritedModifierGroupExcludedAuditDetails{
				ItemID:          cmd.ItemID,
				ModifierGroupID: cmd.ModifierGroupID,
			},
		}
		return 200, res, audit, nil
	})
}

type setModifierGroupDefaultsFingerprint struct {
	GroupID   uuid.UUID   `json:"group_id"`
	OptionIDs []uuid.UUID `json:"option_ids"`
}

type modifierGroupDefaultsChangedAuditDetails struct {
	GroupID   uuid.UUID   `json:"group_id"`
	OptionIDs []uuid.UUID `json:"option_ids"`
}

// SetModifierGroupDefaultsHandler handles replacing default options of a modifier group.
type SetModifierGroupDefaultsHandler struct {
	runner *Runner
}

// NewSetModifierGroupDefaultsHandler creates a new SetModifierGroupDefaultsHandler.
func NewSetModifierGroupDefaultsHandler(runner *Runner) *SetModifierGroupDefaultsHandler {
	return &SetModifierGroupDefaultsHandler{runner: runner}
}

// Handle executes the set modifier group defaults command.
func (h *SetModifierGroupDefaultsHandler) Handle(ctx context.Context, actor Actor, cmd SetModifierGroupDefaultsCommand) (int, ModifierGroupDefaultsResponse, error) {
	optIDs := cmd.OptionIDs
	if optIDs == nil {
		optIDs = []uuid.UUID{}
	}

	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpModifierGroupSetDefaults,
		Fingerprint: setModifierGroupDefaultsFingerprint{
			GroupID:   cmd.GroupID,
			OptionIDs: optIDs,
		},
		Required: []string{CapAdministerStructure},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec, func(q *sqlc.Queries) (int, ModifierGroupDefaultsResponse, AuditRecord, error) {
		// 1. Lock group
		group, err := q.GetModifierGroupForUpdate(ctx, cmd.GroupID)
		if err != nil {
			return 0, ModifierGroupDefaultsResponse{}, AuditRecord{}, MapDBError(err)
		}
		if group.RetiredAt.Valid {
			return 0, ModifierGroupDefaultsResponse{}, AuditRecord{}, fmt.Errorf("%w: modifier group is retired", ErrEntityRetired)
		}

		// 2. Validate cardinality
		count := len(optIDs)
		if count < int(group.MinSelections) || count > int(group.MaxSelections) {
			return 0, ModifierGroupDefaultsResponse{}, AuditRecord{}, fmt.Errorf("%w: default options count %d must be between min %d and max %d", ErrInvalidModifierConfiguration, count, group.MinSelections, group.MaxSelections)
		}

		// 3. Validate distinct options
		seen := make(map[uuid.UUID]bool, len(optIDs))
		for _, id := range optIDs {
			if seen[id] {
				return 0, ModifierGroupDefaultsResponse{}, AuditRecord{}, fmt.Errorf("%w: duplicate default option %s", ErrInvalidModifierConfiguration, id)
			}
			seen[id] = true
		}

		// 4. Validate all options belong to this group, are available, and not retired
		if len(optIDs) > 0 {
			options, err := q.ListModifierOptionsByGroup(ctx, cmd.GroupID)
			if err != nil {
				return 0, ModifierGroupDefaultsResponse{}, AuditRecord{}, MapDBError(err)
			}

			optMap := make(map[uuid.UUID]sqlc.ModifierOption, len(options))
			for _, opt := range options {
				optMap[opt.ID] = opt
			}

			for _, id := range optIDs {
				opt, ok := optMap[id]
				if !ok {
					return 0, ModifierGroupDefaultsResponse{}, AuditRecord{}, fmt.Errorf("%w: option %s does not belong to group", ErrInvalidModifierConfiguration, id)
				}
				if opt.RetiredAt.Valid {
					return 0, ModifierGroupDefaultsResponse{}, AuditRecord{}, fmt.Errorf("%w: default option %s is retired", ErrEntityRetired, id)
				}
				if !opt.Available {
					return 0, ModifierGroupDefaultsResponse{}, AuditRecord{}, fmt.Errorf("%w: default option %s is not available", ErrInvalidModifierConfiguration, id)
				}
			}
		}

		// 5. Delete existing defaults and reinsert
		if err := q.DeleteModifierGroupDefaultOptions(ctx, cmd.GroupID); err != nil {
			return 0, ModifierGroupDefaultsResponse{}, AuditRecord{}, MapDBError(err)
		}

		if len(optIDs) > 0 {
			if err := q.CreateModifierGroupDefaultOptions(ctx, sqlc.CreateModifierGroupDefaultOptionsParams{
				ModifierGroupID: cmd.GroupID,
				OptionIds:       optIDs,
			}); err != nil {
				return 0, ModifierGroupDefaultsResponse{}, AuditRecord{}, MapDBError(err)
			}
		}

		res := ModifierGroupDefaultsResponse{
			GroupID:   cmd.GroupID,
			OptionIDs: optIDs,
		}
		audit := AuditRecord{
			EventType: EventModifierGroupDefaultsChanged,
			Details: modifierGroupDefaultsChangedAuditDetails{
				GroupID:   cmd.GroupID,
				OptionIDs: optIDs,
			},
		}
		return 200, res, audit, nil
	})
}
