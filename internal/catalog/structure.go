package catalog

import (
	"context"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
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
