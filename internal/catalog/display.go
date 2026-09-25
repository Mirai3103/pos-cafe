package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
)

type itemDetailsFingerprint struct {
	ItemID      uuid.UUID `json:"item_id"`
	Code        *string   `json:"code"`
	Badge       *string   `json:"badge"`
	Description *string   `json:"description"`
}

type itemDetailsValues struct {
	Code        *string `json:"code"`
	Badge       *string `json:"badge"`
	Description *string `json:"description"`
}

type itemDetailsChangedAudit struct {
	ItemID uuid.UUID         `json:"item_id"`
	Before itemDetailsValues `json:"before"`
	After  itemDetailsValues `json:"after"`
}

// SetItemDetailsHandler sets a Menu Item's code, badge, and description (ADR-058).
type SetItemDetailsHandler struct {
	runner *Runner
}

// NewSetItemDetailsHandler creates a SetItemDetailsHandler.
func NewSetItemDetailsHandler(runner *Runner) *SetItemDetailsHandler {
	return &SetItemDetailsHandler{runner: runner}
}

// Handle replaces all three display fields.
func (h *SetItemDetailsHandler) Handle(ctx context.Context, actor Actor, cmd SetItemDetailsCommand) (int, ItemDetailsResponse, error) {
	code, codeKey, codeErr := NormalizeItemCode(cmd.Code)
	badge, badgeErr := NormalizeBadge(cmd.Badge)
	desc, descErr := NormalizeDescription(cmd.Description)
	if err := errors.Join(codeErr, badgeErr, descErr); err != nil {
		return 0, ItemDetailsResponse{}, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error())
	}

	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpItemSetDetails,
		Fingerprint: itemDetailsFingerprint{
			ItemID: cmd.ItemID, Code: nullStringPtr(code), Badge: nullStringPtr(badge), Description: nullStringPtr(desc),
		},
		Required: []string{CapAdministerStructure},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec, func(q *sqlc.Queries) (int, ItemDetailsResponse, AuditRecord, error) {
		existing, err := q.GetMenuItemForUpdate(ctx, cmd.ItemID)
		if err != nil {
			return 0, ItemDetailsResponse{}, AuditRecord{}, MapDBError(err)
		}
		if existing.RetiredAt.Valid {
			return 0, ItemDetailsResponse{}, AuditRecord{}, fmt.Errorf("%w: item is retired", ErrEntityRetired)
		}

		if codeKey.Valid {
			_, err := q.GetActiveMenuItemIDByCode(ctx, sqlc.GetActiveMenuItemIDByCodeParams{
				NormalizedCode: codeKey, ExcludeID: cmd.ItemID,
			})
			if err == nil {
				return 0, ItemDetailsResponse{}, AuditRecord{}, fmt.Errorf("%w: code %q is used by another item", ErrCodeConflict, code.String)
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return 0, ItemDetailsResponse{}, AuditRecord{}, MapDBError(err)
			}
		}

		item, err := q.UpdateMenuItemDetails(ctx, sqlc.UpdateMenuItemDetailsParams{
			ID: cmd.ItemID, Code: code, NormalizedCode: codeKey, Badge: badge, Description: desc,
		})
		if err != nil {
			return 0, ItemDetailsResponse{}, AuditRecord{}, MapDBError(err)
		}

		res := ItemDetailsResponse{
			ItemID: item.ID, Code: nullStringPtr(item.Code), Badge: nullStringPtr(item.Badge), Description: nullStringPtr(item.Description),
		}
		audit := AuditRecord{
			EventType: EventItemDetailsChanged,
			Details: itemDetailsChangedAudit{
				ItemID: item.ID,
				Before: itemDetailsValues{
					Code: nullStringPtr(existing.Code), Badge: nullStringPtr(existing.Badge), Description: nullStringPtr(existing.Description),
				},
				After: itemDetailsValues{Code: res.Code, Badge: res.Badge, Description: res.Description},
			},
		}
		return 200, res, audit, nil
	})
}

type categoryDetailsFingerprint struct {
	CategoryID   uuid.UUID `json:"category_id"`
	Icon         *string   `json:"icon"`
	DisplayOrder int32     `json:"display_order"`
}

type categoryDetailsValues struct {
	Icon         *string `json:"icon"`
	DisplayOrder int32   `json:"display_order"`
}

type categoryDetailsChangedAudit struct {
	CategoryID uuid.UUID             `json:"category_id"`
	Before     categoryDetailsValues `json:"before"`
	After      categoryDetailsValues `json:"after"`
}

// SetCategoryDetailsHandler sets a Menu Category's icon and display order.
type SetCategoryDetailsHandler struct {
	runner *Runner
}

// NewSetCategoryDetailsHandler creates a SetCategoryDetailsHandler.
func NewSetCategoryDetailsHandler(runner *Runner) *SetCategoryDetailsHandler {
	return &SetCategoryDetailsHandler{runner: runner}
}

// Handle replaces the icon and display order.
func (h *SetCategoryDetailsHandler) Handle(ctx context.Context, actor Actor, cmd SetCategoryDetailsCommand) (int, CategoryDetailsResponse, error) {
	icon, iconErr := NormalizeCategoryIcon(cmd.Icon)
	if err := errors.Join(iconErr, ValidateDisplayOrder(cmd.DisplayOrder)); err != nil {
		return 0, CategoryDetailsResponse{}, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error())
	}

	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpCategorySetDetails,
		Fingerprint: categoryDetailsFingerprint{CategoryID: cmd.CategoryID, Icon: nullStringPtr(icon), DisplayOrder: cmd.DisplayOrder},
		Required:    []string{CapAdministerStructure},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec, func(q *sqlc.Queries) (int, CategoryDetailsResponse, AuditRecord, error) {
		existing, err := q.GetMenuCategoryForUpdate(ctx, cmd.CategoryID)
		if err != nil {
			return 0, CategoryDetailsResponse{}, AuditRecord{}, MapDBError(err)
		}
		if existing.RetiredAt.Valid {
			return 0, CategoryDetailsResponse{}, AuditRecord{}, fmt.Errorf("%w: category is retired", ErrEntityRetired)
		}

		cat, err := q.UpdateMenuCategoryDetails(ctx, sqlc.UpdateMenuCategoryDetailsParams{
			ID: cmd.CategoryID, Icon: icon, DisplayOrder: cmd.DisplayOrder,
		})
		if err != nil {
			return 0, CategoryDetailsResponse{}, AuditRecord{}, MapDBError(err)
		}

		res := CategoryDetailsResponse{CategoryID: cat.ID, Icon: nullStringPtr(cat.Icon), DisplayOrder: cat.DisplayOrder}
		audit := AuditRecord{
			EventType: EventCategoryDetailsChanged,
			Details: categoryDetailsChangedAudit{
				CategoryID: cat.ID,
				Before:     categoryDetailsValues{Icon: nullStringPtr(existing.Icon), DisplayOrder: existing.DisplayOrder},
				After:      categoryDetailsValues{Icon: res.Icon, DisplayOrder: res.DisplayOrder},
			},
		}
		return 200, res, audit, nil
	})
}
