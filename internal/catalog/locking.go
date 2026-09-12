package catalog

import (
	"context"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

// lockSizeWithParentCheck locks the parent MenuItem then the child MenuItemSize
// (parent-before-child ordering avoids deadlocks under concurrent mutation),
// returning ErrEntityRetired if either is already retired.
func lockSizeWithParentCheck(ctx context.Context, q *sqlc.Queries, sizeID uuid.UUID) (sqlc.MenuItemSize, error) {
	sizeRow, err := q.GetMenuItemSizeByID(ctx, sizeID)
	if err != nil {
		return sqlc.MenuItemSize{}, MapDBError(err)
	}
	parent, err := q.GetMenuItemForUpdate(ctx, sizeRow.MenuItemID)
	if err != nil {
		return sqlc.MenuItemSize{}, MapDBError(err)
	}
	if parent.RetiredAt.Valid {
		return sqlc.MenuItemSize{}, ErrEntityRetired
	}
	existing, err := q.GetMenuItemSizeForUpdate(ctx, sizeID)
	if err != nil {
		return sqlc.MenuItemSize{}, MapDBError(err)
	}
	if existing.RetiredAt.Valid {
		return sqlc.MenuItemSize{}, ErrEntityRetired
	}
	return existing, nil
}

// lockModifierOptionWithParentCheck locks the parent ModifierGroup then the
// child ModifierOption, returning ErrEntityRetired if either is already retired.
func lockModifierOptionWithParentCheck(ctx context.Context, q *sqlc.Queries, optionID uuid.UUID) (sqlc.ModifierOption, error) {
	optRow, err := q.GetModifierOptionByID(ctx, optionID)
	if err != nil {
		return sqlc.ModifierOption{}, MapDBError(err)
	}
	parent, err := q.GetModifierGroupForUpdate(ctx, optRow.ModifierGroupID)
	if err != nil {
		return sqlc.ModifierOption{}, MapDBError(err)
	}
	if parent.RetiredAt.Valid {
		return sqlc.ModifierOption{}, ErrEntityRetired
	}
	existing, err := q.GetModifierOptionForUpdate(ctx, optionID)
	if err != nil {
		return sqlc.ModifierOption{}, MapDBError(err)
	}
	if existing.RetiredAt.Valid {
		return sqlc.ModifierOption{}, ErrEntityRetired
	}
	return existing, nil
}
