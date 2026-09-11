package catalog

import (
	"context"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

func normalizeName(raw string) (display, key string) {
	return NormalizeName(raw)
}

// Fingerprints and audit details

type renameItemFingerprint struct {
	ItemID uuid.UUID `json:"item_id"`
	Name   string    `json:"name"`
}

type itemRenamedAuditDetails struct {
	ItemID  uuid.UUID `json:"item_id"`
	OldName string    `json:"old_name"`
	NewName string    `json:"new_name"`
}

type renameSizeFingerprint struct {
	SizeID uuid.UUID `json:"size_id"`
	Name   string    `json:"name"`
}

type sizeRenamedAuditDetails struct {
	SizeID     uuid.UUID `json:"size_id"`
	MenuItemID uuid.UUID `json:"menu_item_id"`
	OldName    string    `json:"old_name"`
	NewName    string    `json:"new_name"`
}

type renameModifierGroupFingerprint struct {
	GroupID uuid.UUID `json:"group_id"`
	Name    string    `json:"name"`
}

type modifierGroupRenamedAuditDetails struct {
	GroupID uuid.UUID `json:"group_id"`
	OldName string    `json:"old_name"`
	NewName string    `json:"new_name"`
}

type renameModifierOptionFingerprint struct {
	OptionID uuid.UUID `json:"option_id"`
	Name     string    `json:"name"`
}

type modifierOptionRenamedAuditDetails struct {
	OptionID        uuid.UUID `json:"option_id"`
	ModifierGroupID uuid.UUID `json:"modifier_group_id"`
	OldName         string    `json:"old_name"`
	NewName         string    `json:"new_name"`
}

// ============================================================================
// RenameItemHandler
// ============================================================================

// RenameItemHandler handles renaming of menu items.
type RenameItemHandler struct {
	runner *Runner
}

// NewRenameItemHandler creates a new RenameItemHandler.
func NewRenameItemHandler(runner *Runner) *RenameItemHandler {
	return &RenameItemHandler{runner: runner}
}

// Handle executes the item rename command.
func (h *RenameItemHandler) Handle(ctx context.Context, actor Actor, cmd RenameItemCommand) (int, ItemResponse, error) {
	display, key := normalizeName(cmd.Name)
	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpItemRename,
		Fingerprint: renameItemFingerprint{
			ItemID: cmd.ItemID,
			Name:   display,
		},
		Required: []string{CapAdministerStructure},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec, func(q *sqlc.Queries) (int, ItemResponse, AuditRecord, error) {
		existing, err := q.GetMenuItemForUpdate(ctx, cmd.ItemID)
		if err != nil {
			return 0, ItemResponse{}, AuditRecord{}, MapDBError(err)
		}
		if existing.RetiredAt.Valid {
			return 0, ItemResponse{}, AuditRecord{}, ErrEntityRetired
		}

		item, err := q.RenameMenuItem(ctx, sqlc.RenameMenuItemParams{
			ID:             cmd.ItemID,
			Name:           display,
			NormalizedName: key,
		})
		if err != nil {
			return 0, ItemResponse{}, AuditRecord{}, MapDBError(err)
		}

		res := ItemResponse{
			ID:         item.ID,
			CategoryID: item.CategoryID,
			Name:       item.Name,
			Available:  item.Available,
		}
		if item.PriceVnd.Valid {
			v := item.PriceVnd.Int64
			res.PriceVND = &v
		}

		sizes, err := q.ListMenuItemSizesByItem(ctx, item.ID)
		if err != nil {
			return 0, ItemResponse{}, AuditRecord{}, MapDBError(err)
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

		audit := AuditRecord{
			EventType: EventItemRenamed,
			Details: itemRenamedAuditDetails{
				ItemID:  item.ID,
				OldName: existing.Name,
				NewName: item.Name,
			},
		}
		return 200, res, audit, nil
	})
}

// ============================================================================
// RenameSizeHandler
// ============================================================================

// RenameSizeHandler handles renaming of menu item sizes.
type RenameSizeHandler struct {
	runner *Runner
}

// NewRenameSizeHandler creates a new RenameSizeHandler.
func NewRenameSizeHandler(runner *Runner) *RenameSizeHandler {
	return &RenameSizeHandler{runner: runner}
}

// Handle executes the size rename command.
func (h *RenameSizeHandler) Handle(ctx context.Context, actor Actor, cmd RenameSizeCommand) (int, SizeResponse, error) {
	display, key := normalizeName(cmd.Name)
	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpSizeRename,
		Fingerprint: renameSizeFingerprint{
			SizeID: cmd.SizeID,
			Name:   display,
		},
		Required: []string{CapAdministerStructure},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec, func(q *sqlc.Queries) (int, SizeResponse, AuditRecord, error) {
		// Lock parent before child:
		// 1. Obtain parent MenuItemID
		sizeRow, err := q.GetMenuItemSizeByID(ctx, cmd.SizeID)
		if err != nil {
			return 0, SizeResponse{}, AuditRecord{}, MapDBError(err)
		}

		// 2. Lock parent MenuItem and check retirement
		parent, err := q.GetMenuItemForUpdate(ctx, sizeRow.MenuItemID)
		if err != nil {
			return 0, SizeResponse{}, AuditRecord{}, MapDBError(err)
		}
		if parent.RetiredAt.Valid {
			return 0, SizeResponse{}, AuditRecord{}, ErrEntityRetired
		}

		// 3. Lock child MenuItemSize and check retirement
		existing, err := q.GetMenuItemSizeForUpdate(ctx, cmd.SizeID)
		if err != nil {
			return 0, SizeResponse{}, AuditRecord{}, MapDBError(err)
		}
		if existing.RetiredAt.Valid {
			return 0, SizeResponse{}, AuditRecord{}, ErrEntityRetired
		}

		size, err := q.RenameMenuItemSize(ctx, sqlc.RenameMenuItemSizeParams{
			ID:             cmd.SizeID,
			Name:           display,
			NormalizedName: key,
		})
		if err != nil {
			return 0, SizeResponse{}, AuditRecord{}, MapDBError(err)
		}

		res := SizeResponse{
			ID:        size.ID,
			Name:      size.Name,
			PriceVND:  size.PriceVnd,
			Available: size.Available,
		}
		audit := AuditRecord{
			EventType: EventSizeRenamed,
			Details: sizeRenamedAuditDetails{
				SizeID:     size.ID,
				MenuItemID: size.MenuItemID,
				OldName:    existing.Name,
				NewName:    size.Name,
			},
		}
		return 200, res, audit, nil
	})
}

// ============================================================================
// RenameModifierGroupHandler
// ============================================================================

// RenameModifierGroupHandler handles renaming of modifier groups.
type RenameModifierGroupHandler struct {
	runner *Runner
}

// NewRenameModifierGroupHandler creates a new RenameModifierGroupHandler.
func NewRenameModifierGroupHandler(runner *Runner) *RenameModifierGroupHandler {
	return &RenameModifierGroupHandler{runner: runner}
}

// Handle executes the modifier group rename command.
func (h *RenameModifierGroupHandler) Handle(ctx context.Context, actor Actor, cmd RenameModifierGroupCommand) (int, ModifierGroupResponse, error) {
	display, key := normalizeName(cmd.Name)
	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpModifierGroupRename,
		Fingerprint: renameModifierGroupFingerprint{
			GroupID: cmd.GroupID,
			Name:    display,
		},
		Required: []string{CapAdministerStructure},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec, func(q *sqlc.Queries) (int, ModifierGroupResponse, AuditRecord, error) {
		existing, err := q.GetModifierGroupForUpdate(ctx, cmd.GroupID)
		if err != nil {
			return 0, ModifierGroupResponse{}, AuditRecord{}, MapDBError(err)
		}
		if existing.RetiredAt.Valid {
			return 0, ModifierGroupResponse{}, AuditRecord{}, ErrEntityRetired
		}

		group, err := q.RenameModifierGroup(ctx, sqlc.RenameModifierGroupParams{
			ID:             cmd.GroupID,
			Name:           display,
			NormalizedName: key,
		})
		if err != nil {
			return 0, ModifierGroupResponse{}, AuditRecord{}, MapDBError(err)
		}

		options, err := q.ListModifierOptionsByGroup(ctx, group.ID)
		if err != nil {
			return 0, ModifierGroupResponse{}, AuditRecord{}, MapDBError(err)
		}
		defaultRows, err := q.ListModifierGroupDefaultOptionsByGroup(ctx, group.ID)
		if err != nil {
			return 0, ModifierGroupResponse{}, AuditRecord{}, MapDBError(err)
		}
		defaultIDs := make([]uuid.UUID, len(defaultRows))
		for i, d := range defaultRows {
			defaultIDs[i] = d.ModifierOptionID
		}

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

		audit := AuditRecord{
			EventType: EventModifierGroupRenamed,
			Details: modifierGroupRenamedAuditDetails{
				GroupID: group.ID,
				OldName: existing.Name,
				NewName: group.Name,
			},
		}
		return 200, res, audit, nil
	})
}

// ============================================================================
// RenameModifierOptionHandler
// ============================================================================

// RenameModifierOptionHandler handles renaming of modifier options.
type RenameModifierOptionHandler struct {
	runner *Runner
}

// NewRenameModifierOptionHandler creates a new RenameModifierOptionHandler.
func NewRenameModifierOptionHandler(runner *Runner) *RenameModifierOptionHandler {
	return &RenameModifierOptionHandler{runner: runner}
}

// Handle executes the modifier option rename command.
func (h *RenameModifierOptionHandler) Handle(ctx context.Context, actor Actor, cmd RenameModifierOptionCommand) (int, ModifierOptionResponse, error) {
	display, key := normalizeName(cmd.Name)
	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpModifierOptionRename,
		Fingerprint: renameModifierOptionFingerprint{
			OptionID: cmd.OptionID,
			Name:     display,
		},
		Required: []string{CapAdministerStructure},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec, func(q *sqlc.Queries) (int, ModifierOptionResponse, AuditRecord, error) {
		// Lock parent before child:
		// 1. Obtain parent ModifierGroupID
		optRow, err := q.GetModifierOptionByID(ctx, cmd.OptionID)
		if err != nil {
			return 0, ModifierOptionResponse{}, AuditRecord{}, MapDBError(err)
		}

		// 2. Lock parent ModifierGroup and check retirement
		parent, err := q.GetModifierGroupForUpdate(ctx, optRow.ModifierGroupID)
		if err != nil {
			return 0, ModifierOptionResponse{}, AuditRecord{}, MapDBError(err)
		}
		if parent.RetiredAt.Valid {
			return 0, ModifierOptionResponse{}, AuditRecord{}, ErrEntityRetired
		}

		// 3. Lock child ModifierOption and check retirement
		existing, err := q.GetModifierOptionForUpdate(ctx, cmd.OptionID)
		if err != nil {
			return 0, ModifierOptionResponse{}, AuditRecord{}, MapDBError(err)
		}
		if existing.RetiredAt.Valid {
			return 0, ModifierOptionResponse{}, AuditRecord{}, ErrEntityRetired
		}

		opt, err := q.RenameModifierOption(ctx, sqlc.RenameModifierOptionParams{
			ID:             cmd.OptionID,
			Name:           display,
			NormalizedName: key,
		})
		if err != nil {
			return 0, ModifierOptionResponse{}, AuditRecord{}, MapDBError(err)
		}

		res := ModifierOptionResponse{
			ID:              opt.ID,
			ModifierGroupID: opt.ModifierGroupID,
			Name:            opt.Name,
			SurchargeVND:    opt.SurchargeVnd,
			Available:       opt.Available,
		}
		audit := AuditRecord{
			EventType: EventModifierOptionRenamed,
			Details: modifierOptionRenamedAuditDetails{
				OptionID:        opt.ID,
				ModifierGroupID: opt.ModifierGroupID,
				OldName:         existing.Name,
				NewName:         opt.Name,
			},
		}
		return 200, res, audit, nil
	})
}
