package catalog

import (
	"context"
	"fmt"
	"sort"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
)

// lockGroupsForSet locks every group in current ∪ desired in id order and
// rejects retired groups the command would add.
func lockGroupsForSet(ctx context.Context, q *sqlc.Queries, current, desired []uuid.UUID) error {
	all := UnionIDs(current, desired)
	if len(all) == 0 {
		return nil
	}
	rows, err := q.LockModifierGroupsByIDs(ctx, all)
	if err != nil {
		return MapDBError(err)
	}
	if len(rows) != len(all) {
		return fmt.Errorf("%w: modifier group not found", ErrNotFound)
	}
	added, _ := DiffIDSets(current, desired)
	for _, r := range rows {
		if r.RetiredAt.Valid && ContainsID(added, r.ID) {
			return fmt.Errorf("%w: modifier group %s is retired", ErrEntityRetired, r.ID)
		}
	}
	return nil
}

func sortExclusionRefs(refs []ExclusionRef) {
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].ModifierGroupID != refs[j].ModifierGroupID {
			return refs[i].ModifierGroupID.String() < refs[j].ModifierGroupID.String()
		}
		return refs[i].ItemID.String() < refs[j].ItemID.String()
	})
}

// deleteCategoryGroupExclusions enforces ADR-059 when a category stops providing a group.
func deleteCategoryGroupExclusions(ctx context.Context, q *sqlc.Queries, categoryID, groupID uuid.UUID) ([]ExclusionRef, error) {
	itemIDs, err := q.DeleteCategoryGroupExclusions(ctx, sqlc.DeleteCategoryGroupExclusionsParams{CategoryID: categoryID, GroupID: groupID})
	if err != nil {
		return nil, MapDBError(err)
	}
	refs := make([]ExclusionRef, 0, len(itemIDs))
	for _, id := range itemIDs {
		refs = append(refs, ExclusionRef{ItemID: id, ModifierGroupID: groupID})
	}
	return refs, nil
}

// === Command 7 ===

type replaceItemGroupsFingerprint struct {
	ItemID   uuid.UUID   `json:"item_id"`
	Direct   []uuid.UUID `json:"direct_group_ids"`
	Excluded []uuid.UUID `json:"excluded_group_ids"`
}

type itemModifierGroupsReplacedAudit struct {
	ItemID   uuid.UUID   `json:"item_id"`
	Direct   IDSetChange `json:"direct"`
	Excluded IDSetChange `json:"excluded"`
}

// ReplaceItemModifierGroupsHandler replaces an item's direct and excluded groups.
type ReplaceItemModifierGroupsHandler struct {
	runner *Runner
}

// NewReplaceItemModifierGroupsHandler creates a ReplaceItemModifierGroupsHandler.
func NewReplaceItemModifierGroupsHandler(runner *Runner) *ReplaceItemModifierGroupsHandler {
	return &ReplaceItemModifierGroupsHandler{runner: runner}
}

// Handle locks the item, then its groups in id order (the same order as attach).
func (h *ReplaceItemModifierGroupsHandler) Handle(ctx context.Context, actor Actor, cmd ReplaceItemModifierGroupsCommand) (int, ItemModifierGroupsResponse, error) {
	direct, err := NormalizeIDSet(cmd.DirectGroupIDs, "direct_group_ids")
	if err != nil {
		return 0, ItemModifierGroupsResponse{}, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error())
	}
	excluded, err := NormalizeIDSet(cmd.ExcludedGroupIDs, "excluded_group_ids")
	if err != nil {
		return 0, ItemModifierGroupsResponse{}, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error())
	}
	for _, id := range direct {
		if ContainsID(excluded, id) {
			return 0, ItemModifierGroupsResponse{}, fmt.Errorf("%w: group %s cannot be both direct and excluded", ErrInvalidInheritance, id)
		}
	}

	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpItemReplaceModifierGroups,
		Fingerprint: replaceItemGroupsFingerprint{ItemID: cmd.ItemID, Direct: direct, Excluded: excluded},
		Required:    []string{CapAdministerStructure},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec, func(q *sqlc.Queries) (int, ItemModifierGroupsResponse, AuditRecord, error) {
		item, err := q.GetMenuItemForUpdate(ctx, cmd.ItemID)
		if err != nil {
			return 0, ItemModifierGroupsResponse{}, AuditRecord{}, MapDBError(err)
		}
		if item.RetiredAt.Valid {
			return 0, ItemModifierGroupsResponse{}, AuditRecord{}, fmt.Errorf("%w: item is retired", ErrEntityRetired)
		}

		curDirect, err := q.ListItemDirectGroupIDs(ctx, cmd.ItemID)
		if err != nil {
			return 0, ItemModifierGroupsResponse{}, AuditRecord{}, MapDBError(err)
		}
		curExcluded, err := q.ListItemExcludedGroupIDs(ctx, cmd.ItemID)
		if err != nil {
			return 0, ItemModifierGroupsResponse{}, AuditRecord{}, MapDBError(err)
		}
		if err := lockGroupsForSet(ctx, q, UnionIDs(curDirect, curExcluded), UnionIDs(direct, excluded)); err != nil {
			return 0, ItemModifierGroupsResponse{}, AuditRecord{}, err
		}

		provided, err := q.ListCategoryGroupIDs(ctx, item.CategoryID)
		if err != nil {
			return 0, ItemModifierGroupsResponse{}, AuditRecord{}, MapDBError(err)
		}
		for _, id := range excluded {
			if !ContainsID(provided, id) {
				return 0, ItemModifierGroupsResponse{}, AuditRecord{}, fmt.Errorf("%w: group %s is not provided by the item's category", ErrInvalidInheritance, id)
			}
		}

		dAdd, dRem := DiffIDSets(curDirect, direct)
		eAdd, eRem := DiffIDSets(curExcluded, excluded)
		for _, g := range dRem {
			if err := q.DeleteItemModifierGroup(ctx, sqlc.DeleteItemModifierGroupParams{MenuItemID: cmd.ItemID, ModifierGroupID: g}); err != nil {
				return 0, ItemModifierGroupsResponse{}, AuditRecord{}, MapDBError(err)
			}
		}
		for _, g := range eRem {
			if err := q.DeleteItemModifierGroupExclusion(ctx, sqlc.DeleteItemModifierGroupExclusionParams{MenuItemID: cmd.ItemID, ModifierGroupID: g}); err != nil {
				return 0, ItemModifierGroupsResponse{}, AuditRecord{}, MapDBError(err)
			}
		}
		for _, g := range dAdd {
			if err := q.CreateItemModifierGroup(ctx, sqlc.CreateItemModifierGroupParams{MenuItemID: cmd.ItemID, ModifierGroupID: g}); err != nil {
				return 0, ItemModifierGroupsResponse{}, AuditRecord{}, MapDBError(err)
			}
		}
		for _, g := range eAdd {
			if err := q.CreateItemModifierGroupExclusion(ctx, sqlc.CreateItemModifierGroupExclusionParams{MenuItemID: cmd.ItemID, ModifierGroupID: g}); err != nil {
				return 0, ItemModifierGroupsResponse{}, AuditRecord{}, MapDBError(err)
			}
		}

		res := ItemModifierGroupsResponse{ItemID: cmd.ItemID, DirectGroupIDs: direct, ExcludedGroupIDs: excluded}
		if len(dAdd)+len(dRem)+len(eAdd)+len(eRem) == 0 {
			return 200, res, AuditRecord{}, nil
		}
		audit := AuditRecord{
			EventType: EventItemModifierGroupsReplaced,
			Details: itemModifierGroupsReplacedAudit{
				ItemID:   cmd.ItemID,
				Direct:   IDSetChange{Added: dAdd, Removed: dRem},
				Excluded: IDSetChange{Added: eAdd, Removed: eRem},
			},
		}
		return 200, res, audit, nil
	})
}

// === Command 8 ===

type replaceCategoryGroupsFingerprint struct {
	CategoryID uuid.UUID   `json:"category_id"`
	GroupIDs   []uuid.UUID `json:"group_ids"`
}

type categoryModifierGroupsReplacedAudit struct {
	CategoryID        uuid.UUID      `json:"category_id"`
	Added             []uuid.UUID    `json:"added"`
	Removed           []uuid.UUID    `json:"removed"`
	RemovedExclusions []ExclusionRef `json:"removed_exclusions"`
}

// ReplaceCategoryModifierGroupsHandler replaces the groups a category provides.
type ReplaceCategoryModifierGroupsHandler struct {
	runner *Runner
}

// NewReplaceCategoryModifierGroupsHandler creates a ReplaceCategoryModifierGroupsHandler.
func NewReplaceCategoryModifierGroupsHandler(runner *Runner) *ReplaceCategoryModifierGroupsHandler {
	return &ReplaceCategoryModifierGroupsHandler{runner: runner}
}

// Handle locks the category, then its groups in id order.
func (h *ReplaceCategoryModifierGroupsHandler) Handle(ctx context.Context, actor Actor, cmd ReplaceCategoryModifierGroupsCommand) (int, CategoryModifierGroupsResponse, error) {
	groups, err := NormalizeIDSet(cmd.GroupIDs, "group_ids")
	if err != nil {
		return 0, CategoryModifierGroupsResponse{}, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error())
	}
	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpCategoryReplaceModifierGroups,
		Fingerprint: replaceCategoryGroupsFingerprint{CategoryID: cmd.CategoryID, GroupIDs: groups},
		Required:    []string{CapAdministerStructure},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec, func(q *sqlc.Queries) (int, CategoryModifierGroupsResponse, AuditRecord, error) {
		cat, err := q.GetMenuCategoryForUpdate(ctx, cmd.CategoryID)
		if err != nil {
			return 0, CategoryModifierGroupsResponse{}, AuditRecord{}, MapDBError(err)
		}
		if cat.RetiredAt.Valid {
			return 0, CategoryModifierGroupsResponse{}, AuditRecord{}, fmt.Errorf("%w: category is retired", ErrEntityRetired)
		}
		current, err := q.ListCategoryGroupIDs(ctx, cmd.CategoryID)
		if err != nil {
			return 0, CategoryModifierGroupsResponse{}, AuditRecord{}, MapDBError(err)
		}
		if err := lockGroupsForSet(ctx, q, current, groups); err != nil {
			return 0, CategoryModifierGroupsResponse{}, AuditRecord{}, err
		}

		added, removed := DiffIDSets(current, groups)
		removedExcl := []ExclusionRef{}
		for _, g := range removed {
			if err := q.DeleteCategoryModifierGroup(ctx, sqlc.DeleteCategoryModifierGroupParams{MenuCategoryID: cmd.CategoryID, ModifierGroupID: g}); err != nil {
				return 0, CategoryModifierGroupsResponse{}, AuditRecord{}, MapDBError(err)
			}
			refs, err := deleteCategoryGroupExclusions(ctx, q, cmd.CategoryID, g)
			if err != nil {
				return 0, CategoryModifierGroupsResponse{}, AuditRecord{}, err
			}
			removedExcl = append(removedExcl, refs...)
		}
		for _, g := range added {
			if err := q.CreateCategoryModifierGroup(ctx, sqlc.CreateCategoryModifierGroupParams{MenuCategoryID: cmd.CategoryID, ModifierGroupID: g}); err != nil {
				return 0, CategoryModifierGroupsResponse{}, AuditRecord{}, MapDBError(err)
			}
		}
		sortExclusionRefs(removedExcl)

		res := CategoryModifierGroupsResponse{CategoryID: cmd.CategoryID, GroupIDs: groups, RemovedExclusions: removedExcl}
		if len(added)+len(removed) == 0 {
			return 200, res, AuditRecord{}, nil
		}
		audit := AuditRecord{
			EventType: EventCategoryModifierGroupsReplaced,
			Details: categoryModifierGroupsReplacedAudit{
				CategoryID: cmd.CategoryID, Added: added, Removed: removed, RemovedExclusions: removedExcl,
			},
		}
		return 200, res, audit, nil
	})
}
