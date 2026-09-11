package catalog

import (
	"context"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

// Fingerprints and audit details

type setItemAvailabilityFingerprint struct {
	ItemID    uuid.UUID `json:"item_id"`
	Available bool      `json:"available"`
}

type itemAvailabilityChangedAuditDetails struct {
	ItemID       uuid.UUID `json:"item_id"`
	OldAvailable bool      `json:"old_available"`
	NewAvailable bool      `json:"new_available"`
}

type setSizeAvailabilityFingerprint struct {
	SizeID    uuid.UUID `json:"size_id"`
	Available bool      `json:"available"`
}

type sizeAvailabilityChangedAuditDetails struct {
	SizeID       uuid.UUID `json:"size_id"`
	MenuItemID   uuid.UUID `json:"menu_item_id"`
	OldAvailable bool      `json:"old_available"`
	NewAvailable bool      `json:"new_available"`
}

type setModifierOptionAvailabilityFingerprint struct {
	OptionID  uuid.UUID `json:"option_id"`
	Available bool      `json:"available"`
}

type modifierOptionAvailabilityChangedAuditDetails struct {
	OptionID        uuid.UUID `json:"option_id"`
	ModifierGroupID uuid.UUID `json:"modifier_group_id"`
	OldAvailable    bool      `json:"old_available"`
	NewAvailable    bool      `json:"new_available"`
}

// ============================================================================
// SetItemAvailabilityHandler
// ============================================================================

// SetItemAvailabilityHandler handles setting availability of menu items.
type SetItemAvailabilityHandler struct {
	runner *Runner
}

// NewSetItemAvailabilityHandler creates a new SetItemAvailabilityHandler.
func NewSetItemAvailabilityHandler(runner *Runner) *SetItemAvailabilityHandler {
	return &SetItemAvailabilityHandler{runner: runner}
}

// Handle executes the set item availability command.
func (h *SetItemAvailabilityHandler) Handle(ctx context.Context, actor Actor, cmd SetItemAvailabilityCommand) (int, ItemResponse, error) {
	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpItemSetAvailability,
		Fingerprint: setItemAvailabilityFingerprint{
			ItemID:    cmd.ItemID,
			Available: cmd.Available,
		},
		Required:          []string{CapManageAvailability},
		RequireManagerPIN: false,
	}

	return ExecuteMutation(ctx, h.runner, actor, spec, func(q *sqlc.Queries) (int, ItemResponse, AuditRecord, error) {
		// 1. Lock item row
		existing, err := q.GetMenuItemForUpdate(ctx, cmd.ItemID)
		if err != nil {
			return 0, ItemResponse{}, AuditRecord{}, MapDBError(err)
		}

		// 2. Check retirement
		if existing.RetiredAt.Valid {
			return 0, ItemResponse{}, AuditRecord{}, ErrEntityRetired
		}

		// 3. Same-state no-op: returns current state, does not update timestamps, does not emit audit event
		if existing.Available == cmd.Available {
			res := ItemResponse{
				ID:         existing.ID,
				CategoryID: existing.CategoryID,
				Name:       existing.Name,
				Available:  existing.Available,
			}
			if existing.PriceVnd.Valid {
				v := existing.PriceVnd.Int64
				res.PriceVND = &v
			}
			sizes, err := q.ListMenuItemSizesByItem(ctx, existing.ID)
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
			return 200, res, AuditRecord{}, nil
		}

		// 4. Update availability in database
		item, err := q.SetMenuItemAvailability(ctx, sqlc.SetMenuItemAvailabilityParams{
			ID:        cmd.ItemID,
			Available: cmd.Available,
		})
		if err != nil {
			return 0, ItemResponse{}, AuditRecord{}, MapDBError(err)
		}

		// 5. Build response
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

		// 6. Audit event
		audit := AuditRecord{
			EventType: EventItemAvailabilityChanged,
			Details: itemAvailabilityChangedAuditDetails{
				ItemID:       item.ID,
				OldAvailable: existing.Available,
				NewAvailable: item.Available,
			},
		}

		return 200, res, audit, nil
	})
}

// ============================================================================
// SetSizeAvailabilityHandler
// ============================================================================

// SetSizeAvailabilityHandler handles setting availability of menu item sizes.
type SetSizeAvailabilityHandler struct {
	runner *Runner
}

// NewSetSizeAvailabilityHandler creates a new SetSizeAvailabilityHandler.
func NewSetSizeAvailabilityHandler(runner *Runner) *SetSizeAvailabilityHandler {
	return &SetSizeAvailabilityHandler{runner: runner}
}

// Handle executes the set size availability command.
func (h *SetSizeAvailabilityHandler) Handle(ctx context.Context, actor Actor, cmd SetSizeAvailabilityCommand) (int, SizeResponse, error) {
	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpSizeSetAvailability,
		Fingerprint: setSizeAvailabilityFingerprint{
			SizeID:    cmd.SizeID,
			Available: cmd.Available,
		},
		Required:          []string{CapManageAvailability},
		RequireManagerPIN: false,
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

		// 4. Same-state no-op
		if existing.Available == cmd.Available {
			res := SizeResponse{
				ID:        existing.ID,
				Name:      existing.Name,
				PriceVND:  existing.PriceVnd,
				Available: existing.Available,
			}
			return 200, res, AuditRecord{}, nil
		}

		// 5. Update availability in database
		size, err := q.SetMenuItemSizeAvailability(ctx, sqlc.SetMenuItemSizeAvailabilityParams{
			ID:        cmd.SizeID,
			Available: cmd.Available,
		})
		if err != nil {
			return 0, SizeResponse{}, AuditRecord{}, MapDBError(err)
		}

		// 6. Build response
		res := SizeResponse{
			ID:        size.ID,
			Name:      size.Name,
			PriceVND:  size.PriceVnd,
			Available: size.Available,
		}

		// 7. Audit event
		audit := AuditRecord{
			EventType: EventSizeAvailabilityChanged,
			Details: sizeAvailabilityChangedAuditDetails{
				SizeID:       size.ID,
				MenuItemID:   size.MenuItemID,
				OldAvailable: existing.Available,
				NewAvailable: size.Available,
			},
		}

		return 200, res, audit, nil
	})
}

// ============================================================================
// SetModifierOptionAvailabilityHandler
// ============================================================================

// SetModifierOptionAvailabilityHandler handles setting availability of modifier options.
type SetModifierOptionAvailabilityHandler struct {
	runner *Runner
}

// NewSetModifierOptionAvailabilityHandler creates a new SetModifierOptionAvailabilityHandler.
func NewSetModifierOptionAvailabilityHandler(runner *Runner) *SetModifierOptionAvailabilityHandler {
	return &SetModifierOptionAvailabilityHandler{runner: runner}
}

// Handle executes the set modifier option availability command.
func (h *SetModifierOptionAvailabilityHandler) Handle(ctx context.Context, actor Actor, cmd SetModifierOptionAvailabilityCommand) (int, ModifierOptionResponse, error) {
	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpModifierOptionSetAvailability,
		Fingerprint: setModifierOptionAvailabilityFingerprint{
			OptionID:  cmd.OptionID,
			Available: cmd.Available,
		},
		Required:          []string{CapManageAvailability},
		RequireManagerPIN: false,
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

		// 4. Same-state no-op
		if existing.Available == cmd.Available {
			res := ModifierOptionResponse{
				ID:              existing.ID,
				ModifierGroupID: existing.ModifierGroupID,
				Name:            existing.Name,
				SurchargeVND:    existing.SurchargeVnd,
				Available:       existing.Available,
			}
			return 200, res, AuditRecord{}, nil
		}

		// 5. Update availability in database
		opt, err := q.SetModifierOptionAvailability(ctx, sqlc.SetModifierOptionAvailabilityParams{
			ID:        cmd.OptionID,
			Available: cmd.Available,
		})
		if err != nil {
			return 0, ModifierOptionResponse{}, AuditRecord{}, MapDBError(err)
		}

		// 6. Build response
		res := ModifierOptionResponse{
			ID:              opt.ID,
			ModifierGroupID: opt.ModifierGroupID,
			Name:            opt.Name,
			SurchargeVND:    opt.SurchargeVnd,
			Available:       opt.Available,
		}

		// 7. Audit event
		audit := AuditRecord{
			EventType: EventModifierOptionAvailabilityChanged,
			Details: modifierOptionAvailabilityChangedAuditDetails{
				OptionID:        opt.ID,
				ModifierGroupID: opt.ModifierGroupID,
				OldAvailable:    existing.Available,
				NewAvailable:    opt.Available,
			},
		}

		return 200, res, audit, nil
	})
}
