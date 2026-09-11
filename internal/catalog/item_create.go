package catalog

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

type createItemSizeFingerprint struct {
	Name     string `json:"name"`
	PriceVND int64  `json:"price_vnd"`
}

type createItemFingerprint struct {
	CategoryID uuid.UUID                   `json:"category_id"`
	Name       string                      `json:"name"`
	PriceVND   *int64                      `json:"price_vnd,omitempty"`
	Sizes      []createItemSizeFingerprint `json:"sizes,omitempty"`
}

type itemCreatedSizeAudit struct {
	SizeID   uuid.UUID `json:"size_id"`
	Name     string    `json:"name"`
	PriceVND int64     `json:"price_vnd"`
}

type itemCreatedAuditDetails struct {
	ItemID     uuid.UUID              `json:"item_id"`
	CategoryID uuid.UUID              `json:"category_id"`
	Name       string                 `json:"name"`
	PriceVND   *int64                 `json:"price_vnd,omitempty"`
	Sizes      []itemCreatedSizeAudit `json:"sizes,omitempty"`
}

// CreateItemHandler handles creation of direct and sized menu items.
type CreateItemHandler struct {
	runner *Runner
}

// NewCreateItemHandler creates a new CreateItemHandler.
func NewCreateItemHandler(runner *Runner) *CreateItemHandler {
	return &CreateItemHandler{runner: runner}
}

// Handle executes the item creation command.
func (h *CreateItemHandler) Handle(ctx context.Context, actor Actor, cmd CreateItemCommand) (int, ItemResponse, error) {
	display, key := NormalizeName(cmd.Name)

	var sizesFp []createItemSizeFingerprint
	if len(cmd.Sizes) > 0 {
		sizesFp = make([]createItemSizeFingerprint, len(cmd.Sizes))
		for i, s := range cmd.Sizes {
			sDisplay, _ := NormalizeName(s.Name)
			sizesFp[i] = createItemSizeFingerprint{
				Name:     sDisplay,
				PriceVND: s.PriceVND,
			}
		}
	}

	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpItemCreate,
		Fingerprint: createItemFingerprint{
			CategoryID: cmd.CategoryID,
			Name:       display,
			PriceVND:   cmd.PriceVND,
			Sizes:      sizesFp,
		},
		Required:          []string{CapAdministerStructure, CapChangePrice},
		RequireManagerPIN: true,
		ManagerPIN:        cmd.ManagerPIN,
	}

	return ExecuteMutation(ctx, h.runner, actor, spec, func(q *sqlc.Queries) (int, ItemResponse, AuditRecord, error) {
		// 1. Lock parent category row for deterministic concurrency control.
		_, err := q.GetMenuCategoryForUpdate(ctx, cmd.CategoryID)
		if err != nil {
			return 0, ItemResponse{}, AuditRecord{}, MapDBError(err)
		}

		// 2. Validate pricing configuration: exactly one pricing form.
		hasDirect := cmd.PriceVND != nil
		hasSizes := len(cmd.Sizes) > 0

		if (!hasDirect && !hasSizes) || (hasDirect && hasSizes) {
			return 0, ItemResponse{}, AuditRecord{}, ErrInvalidPricingConfiguration
		}

		// 3. Validate item name is non-empty after normalization.
		if display == "" {
			return 0, ItemResponse{}, AuditRecord{}, fmt.Errorf("%w: item name cannot be empty", ErrInvalidPricingConfiguration)
		}

		// 4. Validate direct price if present.
		if hasDirect {
			if err := ValidatePrice(*cmd.PriceVND); err != nil {
				return 0, ItemResponse{}, AuditRecord{}, fmt.Errorf("%w: %s", ErrInvalidPricingConfiguration, err.Error())
			}
		}

		// 5. Validate sizes if present: duplicate normalized names and price bounds.
		if hasSizes {
			seenSizes := make(map[string]bool, len(cmd.Sizes))
			for _, s := range cmd.Sizes {
				sDisplay, sKey := NormalizeName(s.Name)
				if sDisplay == "" {
					return 0, ItemResponse{}, AuditRecord{}, fmt.Errorf("%w: size name cannot be empty", ErrInvalidPricingConfiguration)
				}
				if seenSizes[sKey] {
					return 0, ItemResponse{}, AuditRecord{}, fmt.Errorf("%w: duplicate size name %q", ErrNameConflict, sDisplay)
				}
				seenSizes[sKey] = true

				if err := ValidatePrice(s.PriceVND); err != nil {
					return 0, ItemResponse{}, AuditRecord{}, fmt.Errorf("%w: size %q price %d: %s", ErrInvalidPricingConfiguration, sDisplay, s.PriceVND, err.Error())
				}
			}
		}

		// 6. Create Menu Item in database.
		var priceNull sql.NullInt64
		if hasDirect {
			priceNull = sql.NullInt64{Int64: *cmd.PriceVND, Valid: true}
		}

		item, err := q.CreateMenuItem(ctx, sqlc.CreateMenuItemParams{
			CategoryID:     cmd.CategoryID,
			Name:           display,
			NormalizedName: key,
			PriceVnd:       priceNull,
			Available:      true,
		})
		if err != nil {
			return 0, ItemResponse{}, AuditRecord{}, MapDBError(err)
		}

		// 7. Create Sizes in database if sized item.
		var createdSizes []sqlc.MenuItemSize
		if hasSizes {
			createdSizes = make([]sqlc.MenuItemSize, 0, len(cmd.Sizes))
			for _, s := range cmd.Sizes {
				sDisplay, sKey := NormalizeName(s.Name)
				sizeRow, err := q.CreateMenuItemSize(ctx, sqlc.CreateMenuItemSizeParams{
					MenuItemID:     item.ID,
					Name:           sDisplay,
					NormalizedName: sKey,
					PriceVnd:       s.PriceVND,
					Available:      true,
				})
				if err != nil {
					return 0, ItemResponse{}, AuditRecord{}, MapDBError(err)
				}
				createdSizes = append(createdSizes, sizeRow)
			}
		}

		// 8. Assemble ItemResponse.
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
		if len(createdSizes) > 0 {
			res.Sizes = make([]SizeResponse, len(createdSizes))
			for i, s := range createdSizes {
				res.Sizes[i] = SizeResponse{
					ID:        s.ID,
					Name:      s.Name,
					PriceVND:  s.PriceVnd,
					Available: s.Available,
				}
			}
		}

		// 9. Assemble secret-free AuditRecord.
		auditDetails := itemCreatedAuditDetails{
			ItemID:     item.ID,
			CategoryID: item.CategoryID,
			Name:       item.Name,
		}
		if item.PriceVnd.Valid {
			v := item.PriceVnd.Int64
			auditDetails.PriceVND = &v
		}
		if len(createdSizes) > 0 {
			auditDetails.Sizes = make([]itemCreatedSizeAudit, len(createdSizes))
			for i, s := range createdSizes {
				auditDetails.Sizes[i] = itemCreatedSizeAudit{
					SizeID:   s.ID,
					Name:     s.Name,
					PriceVND: s.PriceVnd,
				}
			}
		}

		audit := AuditRecord{
			EventType: EventItemCreated,
			Details:   auditDetails,
		}

		return 201, res, audit, nil
	})
}
