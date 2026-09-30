package catalog

import (
	"context"

	"github.com/Mirai3103/pos-cafe/internal/platform/database/sqlc"
	"github.com/google/uuid"
)

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
		existing, err := q.GetMenuItemForUpdate(ctx, cmd.ItemID)
		if err != nil {
			return 0, ItemResponse{}, AuditRecord{}, MapDBError(err)
		}
		if existing.RetiredAt.Valid {
			return 0, ItemResponse{}, AuditRecord{}, ErrEntityRetired
		}

		// Same-state no-op: return the current state without touching timestamps
		// or emitting an audit event.
		if existing.Available == cmd.Available {
			res, err := loadItemResponse(ctx, q, existing)
			if err != nil {
				return 0, ItemResponse{}, AuditRecord{}, err
			}
			return 200, res, AuditRecord{}, nil
		}

		item, err := q.SetMenuItemAvailability(ctx, sqlc.SetMenuItemAvailabilityParams{
			ID:        cmd.ItemID,
			Available: cmd.Available,
		})
		if err != nil {
			return 0, ItemResponse{}, AuditRecord{}, MapDBError(err)
		}
		res, err := loadItemResponse(ctx, q, item)
		if err != nil {
			return 0, ItemResponse{}, AuditRecord{}, err
		}

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
		existing, err := lockSizeWithParentCheck(ctx, q, cmd.SizeID)
		if err != nil {
			return 0, SizeResponse{}, AuditRecord{}, err
		}

		// Same-state no-op: no update, no audit event.
		if existing.Available == cmd.Available {
			res := SizeResponse{
				ID:        existing.ID,
				Name:      existing.Name,
				PriceVND:  existing.PriceVnd,
				Available: existing.Available,
			}
			return 200, res, AuditRecord{}, nil
		}

		size, err := q.SetMenuItemSizeAvailability(ctx, sqlc.SetMenuItemSizeAvailabilityParams{
			ID:        cmd.SizeID,
			Available: cmd.Available,
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
		existing, err := lockModifierOptionWithParentCheck(ctx, q, cmd.OptionID)
		if err != nil {
			return 0, ModifierOptionResponse{}, AuditRecord{}, err
		}

		// Same-state no-op: no update, no audit event.
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

		opt, err := q.SetModifierOptionAvailability(ctx, sqlc.SetModifierOptionAvailabilityParams{
			ID:        cmd.OptionID,
			Available: cmd.Available,
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
