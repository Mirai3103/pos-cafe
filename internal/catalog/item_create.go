package catalog

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
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
	spec := MutationSpec{
		RequestID:         cmd.RequestID,
		Operation:         OpItemCreate,
		Fingerprint:       newCreateItemFingerprint(cmd, display),
		Required:          []string{CapAdministerStructure, CapChangePrice},
		RequireManagerPIN: true,
		ManagerPIN:        cmd.ManagerPIN,
	}

	return ExecuteMutation(ctx, h.runner, actor, spec, func(q *sqlc.Queries) (int, ItemResponse, AuditRecord, error) {
		if cmd.CategoryID == uuid.Nil {
			return 0, ItemResponse{}, AuditRecord{}, fmt.Errorf("%w: category_id is required", response.ErrInvalid)
		}

		// Lock the parent category so the insert serializes with concurrent
		// changes to it, such as its retirement.
		category, err := q.GetMenuCategoryForUpdate(ctx, cmd.CategoryID)
		if err != nil {
			return 0, ItemResponse{}, AuditRecord{}, MapDBError(err)
		}
		if category.RetiredAt.Valid {
			return 0, ItemResponse{}, AuditRecord{}, ErrEntityRetired
		}
		if err := validateNewItem(cmd, display); err != nil {
			return 0, ItemResponse{}, AuditRecord{}, err
		}

		item, err := q.CreateMenuItem(ctx, sqlc.CreateMenuItemParams{
			CategoryID:     cmd.CategoryID,
			Name:           display,
			NormalizedName: key,
			PriceVnd:       newItemPrice(cmd.PriceVND),
			Available:      true,
		})
		if err != nil {
			return 0, ItemResponse{}, AuditRecord{}, MapDBError(err)
		}
		sizes, err := createItemSizes(ctx, q, item.ID, cmd.Sizes)
		if err != nil {
			return 0, ItemResponse{}, AuditRecord{}, err
		}

		audit := AuditRecord{
			EventType: EventItemCreated,
			Details:   newItemCreatedAudit(item, sizes),
		}
		return 201, newItemResponse(item, sizes), audit, nil
	})
}

// newCreateItemFingerprint normalizes the business input the idempotency key
// covers. The Manager PIN is deliberately absent.
func newCreateItemFingerprint(cmd CreateItemCommand, display string) createItemFingerprint {
	var sizes []createItemSizeFingerprint
	if len(cmd.Sizes) > 0 {
		sizes = make([]createItemSizeFingerprint, len(cmd.Sizes))
		for i, s := range cmd.Sizes {
			sDisplay, _ := NormalizeName(s.Name)
			sizes[i] = createItemSizeFingerprint{
				Name:     sDisplay,
				PriceVND: s.PriceVND,
			}
		}
	}
	return createItemFingerprint{
		CategoryID: cmd.CategoryID,
		Name:       display,
		PriceVND:   cmd.PriceVND,
		Sizes:      sizes,
	}
}

// validateNewItem checks that the item has exactly one pricing form, a
// non-empty name, a valid direct price, and distinct, validly priced sizes.
func validateNewItem(cmd CreateItemCommand, display string) error {
	hasDirect := cmd.PriceVND != nil
	hasSizes := len(cmd.Sizes) > 0
	if hasDirect == hasSizes {
		return ErrInvalidPricingConfiguration
	}
	if display == "" {
		return fmt.Errorf("%w: item name cannot be empty", response.ErrInvalid)
	}
	if hasDirect {
		if err := ValidatePrice(*cmd.PriceVND); err != nil {
			return fmt.Errorf("%w: %s", ErrInvalidPricingConfiguration, err.Error())
		}
	}

	seenSizes := make(map[string]bool, len(cmd.Sizes))
	for _, s := range cmd.Sizes {
		sDisplay, sKey := NormalizeName(s.Name)
		if sDisplay == "" {
			return fmt.Errorf("%w: size name cannot be empty", response.ErrInvalid)
		}
		if seenSizes[sKey] {
			return fmt.Errorf("%w: duplicate size name %q", ErrNameConflict, sDisplay)
		}
		seenSizes[sKey] = true

		if err := ValidatePrice(s.PriceVND); err != nil {
			return fmt.Errorf("%w: size %q price %d: %s", ErrInvalidPricingConfiguration, sDisplay, s.PriceVND, err.Error())
		}
	}
	return nil
}

func newItemPrice(price *int64) sql.NullInt64 {
	if price == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *price, Valid: true}
}

// createItemSizes inserts a sized item's sizes in one batch. A direct-price
// item has none and creates nothing.
func createItemSizes(ctx context.Context, q *sqlc.Queries, itemID uuid.UUID, sizes []CreateSizeInput) ([]sqlc.MenuItemSize, error) {
	if len(sizes) == 0 {
		return nil, nil
	}
	names := make([]string, len(sizes))
	normalizedNames := make([]string, len(sizes))
	prices := make([]int64, len(sizes))
	for i, s := range sizes {
		names[i], normalizedNames[i] = NormalizeName(s.Name)
		prices[i] = s.PriceVND
	}
	created, err := q.CreateMenuItemSizes(ctx, sqlc.CreateMenuItemSizesParams{
		MenuItemID:      itemID,
		Names:           names,
		NormalizedNames: normalizedNames,
		Prices:          prices,
	})
	if err != nil {
		return nil, MapDBError(err)
	}
	return created, nil
}

// newItemCreatedAudit assembles the audit details, which never carry the
// Manager PIN.
func newItemCreatedAudit(item sqlc.MenuItem, sizes []sqlc.MenuItemSize) itemCreatedAuditDetails {
	details := itemCreatedAuditDetails{
		ItemID:     item.ID,
		CategoryID: item.CategoryID,
		Name:       item.Name,
		PriceVND:   nullInt64Ptr(item.PriceVnd),
	}
	if len(sizes) > 0 {
		details.Sizes = make([]itemCreatedSizeAudit, len(sizes))
		for i, s := range sizes {
			details.Sizes[i] = itemCreatedSizeAudit{
				SizeID:   s.ID,
				Name:     s.Name,
				PriceVND: s.PriceVnd,
			}
		}
	}
	return details
}
