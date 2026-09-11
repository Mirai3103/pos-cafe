package catalog

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

// Fingerprints and audit details (ManagerPIN excluded by design)

type repriceItemFingerprint struct {
	ItemID   uuid.UUID `json:"item_id"`
	PriceVND int64     `json:"price_vnd"`
}

type itemRepricedAuditDetails struct {
	ItemID      uuid.UUID `json:"item_id"`
	OldPriceVND int64     `json:"old_price_vnd"`
	NewPriceVND int64     `json:"new_price_vnd"`
}

type repriceSizeFingerprint struct {
	SizeID   uuid.UUID `json:"size_id"`
	PriceVND int64     `json:"price_vnd"`
}

type sizeRepricedAuditDetails struct {
	SizeID      uuid.UUID `json:"size_id"`
	MenuItemID  uuid.UUID `json:"menu_item_id"`
	OldPriceVND int64     `json:"old_price_vnd"`
	NewPriceVND int64     `json:"new_price_vnd"`
}

type repriceModifierOptionFingerprint struct {
	OptionID     uuid.UUID `json:"option_id"`
	SurchargeVND int64     `json:"surcharge_vnd"`
}

type modifierOptionRepricedAuditDetails struct {
	OptionID        uuid.UUID `json:"option_id"`
	ModifierGroupID uuid.UUID `json:"modifier_group_id"`
	OldSurchargeVND int64     `json:"old_surcharge_vnd"`
	NewSurchargeVND int64     `json:"new_surcharge_vnd"`
}

// ============================================================================
// RepriceItemHandler
// ============================================================================

// RepriceItemHandler handles repricing of direct menu items.
type RepriceItemHandler struct {
	runner *Runner
}

// NewRepriceItemHandler creates a new RepriceItemHandler.
func NewRepriceItemHandler(runner *Runner) *RepriceItemHandler {
	return &RepriceItemHandler{runner: runner}
}

// Handle executes the direct item reprice command.
func (h *RepriceItemHandler) Handle(ctx context.Context, actor Actor, cmd RepriceItemCommand) (int, ItemResponse, error) {
	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpItemReprice,
		Fingerprint: repriceItemFingerprint{
			ItemID:   cmd.ItemID,
			PriceVND: cmd.PriceVND,
		},
		Required:          []string{CapAdministerStructure, CapChangePrice},
		RequireManagerPIN: true,
		ManagerPIN:        cmd.ManagerPIN,
	}

	return ExecuteMutation(ctx, h.runner, actor, spec, func(q *sqlc.Queries) (int, ItemResponse, AuditRecord, error) {
		// 1. Validate price bounds
		if err := ValidatePrice(cmd.PriceVND); err != nil {
			return 0, ItemResponse{}, AuditRecord{}, fmt.Errorf("%w: %s", ErrInvalidPricingConfiguration, err.Error())
		}

		// 2. Lock item row
		existing, err := q.GetMenuItemForUpdate(ctx, cmd.ItemID)
		if err != nil {
			return 0, ItemResponse{}, AuditRecord{}, MapDBError(err)
		}

		// 3. Check retirement
		if existing.RetiredAt.Valid {
			return 0, ItemResponse{}, AuditRecord{}, ErrEntityRetired
		}

		// 4. Direct items only: sized items (price_vnd == null) cannot be repriced directly
		if !existing.PriceVnd.Valid {
			return 0, ItemResponse{}, AuditRecord{}, fmt.Errorf("%w: cannot reprice sized item", ErrInvalidPricingConfiguration)
		}

		// 5. Update price in database
		item, err := q.RepriceMenuItem(ctx, sqlc.RepriceMenuItemParams{
			ID:       cmd.ItemID,
			PriceVnd: sql.NullInt64{Int64: cmd.PriceVND, Valid: true},
		})
		if err != nil {
			return 0, ItemResponse{}, AuditRecord{}, MapDBError(err)
		}

		// 6. Build response
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

		// 7. Secret-free audit event
		audit := AuditRecord{
			EventType: EventItemRepriced,
			Details: itemRepricedAuditDetails{
				ItemID:      item.ID,
				OldPriceVND: existing.PriceVnd.Int64,
				NewPriceVND: item.PriceVnd.Int64,
			},
		}

		return 200, res, audit, nil
	})
}

// ============================================================================
// RepriceSizeHandler
// ============================================================================

// RepriceSizeHandler handles repricing of menu item sizes.
type RepriceSizeHandler struct {
	runner *Runner
}

// NewRepriceSizeHandler creates a new RepriceSizeHandler.
func NewRepriceSizeHandler(runner *Runner) *RepriceSizeHandler {
	return &RepriceSizeHandler{runner: runner}
}

// Handle executes the size reprice command.
func (h *RepriceSizeHandler) Handle(ctx context.Context, actor Actor, cmd RepriceSizeCommand) (int, SizeResponse, error) {
	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpSizeReprice,
		Fingerprint: repriceSizeFingerprint{
			SizeID:   cmd.SizeID,
			PriceVND: cmd.PriceVND,
		},
		Required:          []string{CapAdministerStructure, CapChangePrice},
		RequireManagerPIN: true,
		ManagerPIN:        cmd.ManagerPIN,
	}

	return ExecuteMutation(ctx, h.runner, actor, spec, func(q *sqlc.Queries) (int, SizeResponse, AuditRecord, error) {
		// 1. Validate price bounds
		if err := ValidatePrice(cmd.PriceVND); err != nil {
			return 0, SizeResponse{}, AuditRecord{}, fmt.Errorf("%w: %s", ErrInvalidPricingConfiguration, err.Error())
		}

		// Lock parent before child:
		// 2. Obtain parent MenuItemID
		sizeRow, err := q.GetMenuItemSizeByID(ctx, cmd.SizeID)
		if err != nil {
			return 0, SizeResponse{}, AuditRecord{}, MapDBError(err)
		}

		// 3. Lock parent MenuItem and check retirement
		parent, err := q.GetMenuItemForUpdate(ctx, sizeRow.MenuItemID)
		if err != nil {
			return 0, SizeResponse{}, AuditRecord{}, MapDBError(err)
		}
		if parent.RetiredAt.Valid {
			return 0, SizeResponse{}, AuditRecord{}, ErrEntityRetired
		}

		// 4. Lock child MenuItemSize and check retirement
		existing, err := q.GetMenuItemSizeForUpdate(ctx, cmd.SizeID)
		if err != nil {
			return 0, SizeResponse{}, AuditRecord{}, MapDBError(err)
		}
		if existing.RetiredAt.Valid {
			return 0, SizeResponse{}, AuditRecord{}, ErrEntityRetired
		}

		// 5. Update price in database
		size, err := q.RepriceMenuItemSize(ctx, sqlc.RepriceMenuItemSizeParams{
			ID:       cmd.SizeID,
			PriceVnd: cmd.PriceVND,
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

		// 7. Secret-free audit event
		audit := AuditRecord{
			EventType: EventSizeRepriced,
			Details: sizeRepricedAuditDetails{
				SizeID:      size.ID,
				MenuItemID:  size.MenuItemID,
				OldPriceVND: existing.PriceVnd,
				NewPriceVND: size.PriceVnd,
			},
		}

		return 200, res, audit, nil
	})
}

// ============================================================================
// RepriceModifierOptionHandler
// ============================================================================

// RepriceModifierOptionHandler handles repricing of modifier options.
type RepriceModifierOptionHandler struct {
	runner *Runner
}

// NewRepriceModifierOptionHandler creates a new RepriceModifierOptionHandler.
func NewRepriceModifierOptionHandler(runner *Runner) *RepriceModifierOptionHandler {
	return &RepriceModifierOptionHandler{runner: runner}
}

// Handle executes the modifier option reprice command.
func (h *RepriceModifierOptionHandler) Handle(ctx context.Context, actor Actor, cmd RepriceModifierOptionCommand) (int, ModifierOptionResponse, error) {
	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpModifierOptionReprice,
		Fingerprint: repriceModifierOptionFingerprint{
			OptionID:     cmd.OptionID,
			SurchargeVND: cmd.SurchargeVND,
		},
		Required:          []string{CapAdministerStructure, CapChangePrice},
		RequireManagerPIN: true,
		ManagerPIN:        cmd.ManagerPIN,
	}

	return ExecuteMutation(ctx, h.runner, actor, spec, func(q *sqlc.Queries) (int, ModifierOptionResponse, AuditRecord, error) {
		// 1. Validate surcharge bounds
		if err := ValidateSurcharge(cmd.SurchargeVND); err != nil {
			return 0, ModifierOptionResponse{}, AuditRecord{}, fmt.Errorf("%w: %s", ErrInvalidModifierConfiguration, err.Error())
		}

		// Lock parent before child:
		// 2. Obtain parent ModifierGroupID
		optRow, err := q.GetModifierOptionByID(ctx, cmd.OptionID)
		if err != nil {
			return 0, ModifierOptionResponse{}, AuditRecord{}, MapDBError(err)
		}

		// 3. Lock parent ModifierGroup and check retirement
		parent, err := q.GetModifierGroupForUpdate(ctx, optRow.ModifierGroupID)
		if err != nil {
			return 0, ModifierOptionResponse{}, AuditRecord{}, MapDBError(err)
		}
		if parent.RetiredAt.Valid {
			return 0, ModifierOptionResponse{}, AuditRecord{}, ErrEntityRetired
		}

		// 4. Lock child ModifierOption and check retirement
		existing, err := q.GetModifierOptionForUpdate(ctx, cmd.OptionID)
		if err != nil {
			return 0, ModifierOptionResponse{}, AuditRecord{}, MapDBError(err)
		}
		if existing.RetiredAt.Valid {
			return 0, ModifierOptionResponse{}, AuditRecord{}, ErrEntityRetired
		}

		// 5. Update surcharge in database
		opt, err := q.RepriceModifierOption(ctx, sqlc.RepriceModifierOptionParams{
			ID:           cmd.OptionID,
			SurchargeVnd: cmd.SurchargeVND,
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

		// 7. Secret-free audit event
		audit := AuditRecord{
			EventType: EventModifierOptionRepriced,
			Details: modifierOptionRepricedAuditDetails{
				OptionID:        opt.ID,
				ModifierGroupID: opt.ModifierGroupID,
				OldSurchargeVND: existing.SurchargeVnd,
				NewSurchargeVND: opt.SurchargeVnd,
			},
		}

		return 200, res, audit, nil
	})
}
