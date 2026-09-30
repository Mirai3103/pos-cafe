package catalog

import (
	"context"
	"database/sql"
	"sort"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/platform/database/sqlc"
	"github.com/google/uuid"
)

// catalogSnapshot is every catalog row the menu projections read, loaded
// inside one repeatable-read transaction so the projection is consistent.
type catalogSnapshot struct {
	categories []sqlc.MenuCategory
	items      []sqlc.MenuItem
	sizes      []sqlc.MenuItemSize
	groups     []sqlc.ModifierGroup
	options    []sqlc.ModifierOption
	catGroups  []sqlc.ListAllCategoryModifierGroupsRow
	itemGroups []sqlc.ListAllItemModifierGroupsRow
	exclusions []sqlc.ListAllItemModifierGroupExclusionsRow
	defaults   []sqlc.ListAllModifierGroupDefaultOptionsRow
}

func loadCatalogSnapshot(ctx context.Context, q *sqlc.Queries) (*catalogSnapshot, error) {
	categories, err := q.ListMenuCategories(ctx)
	if err != nil {
		return nil, err
	}

	items, err := q.ListAllMenuItems(ctx)
	if err != nil {
		return nil, err
	}

	sizes, err := q.ListAllMenuItemSizes(ctx)
	if err != nil {
		return nil, err
	}

	groups, err := q.ListModifierGroups(ctx)
	if err != nil {
		return nil, err
	}

	options, err := q.ListAllModifierOptions(ctx)
	if err != nil {
		return nil, err
	}

	catGroups, err := q.ListAllCategoryModifierGroups(ctx)
	if err != nil {
		return nil, err
	}

	itemGroups, err := q.ListAllItemModifierGroups(ctx)
	if err != nil {
		return nil, err
	}

	exclusions, err := q.ListAllItemModifierGroupExclusions(ctx)
	if err != nil {
		return nil, err
	}

	defaults, err := q.ListAllModifierGroupDefaultOptions(ctx)
	if err != nil {
		return nil, err
	}

	return &catalogSnapshot{
		categories: categories,
		items:      items,
		sizes:      sizes,
		groups:     groups,
		options:    options,
		catGroups:  catGroups,
		itemGroups: itemGroups,
		exclusions: exclusions,
		defaults:   defaults,
	}, nil
}

// defaultOptionsByGroup keys default option ids by modifier group, in row
// order.
func defaultOptionsByGroup(rows []sqlc.ListAllModifierGroupDefaultOptionsRow) map[uuid.UUID][]uuid.UUID {
	return groupBy(rows,
		func(d sqlc.ListAllModifierGroupDefaultOptionsRow) uuid.UUID { return d.ModifierGroupID },
		func(d sqlc.ListAllModifierGroupDefaultOptionsRow) uuid.UUID { return d.ModifierOptionID })
}

// assignmentIndex keys a snapshot's modifier groups and group assignments by
// owner, which every menu projection needs to resolve an item's effective
// groups.
type assignmentIndex struct {
	groupByID map[uuid.UUID]sqlc.ModifierGroup
	// Group ids keyed by category id, item id, and item id respectively.
	categoryGroups   map[uuid.UUID][]uuid.UUID
	itemDirectGroups map[uuid.UUID][]uuid.UUID
	itemExclusions   map[uuid.UUID][]uuid.UUID
}

func newAssignmentIndex(snap *catalogSnapshot) assignmentIndex {
	groupByID := make(map[uuid.UUID]sqlc.ModifierGroup, len(snap.groups))
	for _, g := range snap.groups {
		groupByID[g.ID] = g
	}
	return assignmentIndex{
		groupByID: groupByID,
		categoryGroups: groupBy(snap.catGroups,
			func(r sqlc.ListAllCategoryModifierGroupsRow) uuid.UUID { return r.MenuCategoryID },
			func(r sqlc.ListAllCategoryModifierGroupsRow) uuid.UUID { return r.ModifierGroupID }),
		itemDirectGroups: groupBy(snap.itemGroups,
			func(r sqlc.ListAllItemModifierGroupsRow) uuid.UUID { return r.MenuItemID },
			func(r sqlc.ListAllItemModifierGroupsRow) uuid.UUID { return r.ModifierGroupID }),
		itemExclusions: groupBy(snap.exclusions,
			func(r sqlc.ListAllItemModifierGroupExclusionsRow) uuid.UUID { return r.MenuItemID },
			func(r sqlc.ListAllItemModifierGroupExclusionsRow) uuid.UUID { return r.ModifierGroupID }),
	}
}

// sortIDLists sorts every assignment id list in place, for projections that
// return the lists themselves.
func (x assignmentIndex) sortIDLists() {
	for _, byOwner := range []map[uuid.UUID][]uuid.UUID{x.categoryGroups, x.itemDirectGroups, x.itemExclusions} {
		for id := range byOwner {
			sortUUIDs(byOwner[id])
		}
	}
}

// effectiveGroups returns the ids of the groups item offers: its category's
// groups minus its exclusions, plus its direct groups.
func (x assignmentIndex) effectiveGroups(item sqlc.MenuItem) []uuid.UUID {
	return EffectiveGroupIDs(x.categoryGroups[item.CategoryID], x.itemExclusions[item.ID], x.itemDirectGroups[item.ID])
}

// ModifierGroupsHandler handles reading modifier groups for management.
type ModifierGroupsHandler struct {
	runner *Runner
}

// NewModifierGroupsHandler creates a new ModifierGroupsHandler.
func NewModifierGroupsHandler(runner *Runner) *ModifierGroupsHandler {
	return &ModifierGroupsHandler{runner: runner}
}

// Handle executes the modifier groups query.
func (h *ModifierGroupsHandler) Handle(ctx context.Context, actor Actor) ([]ManagementModifierGroupResponse, error) {
	return ExecuteRead(ctx, h.runner, actor, CapViewPrices, func(q *sqlc.Queries) ([]ManagementModifierGroupResponse, error) {
		groups, err := q.ListModifierGroups(ctx)
		if err != nil {
			return nil, err
		}

		options, err := q.ListAllModifierOptions(ctx)
		if err != nil {
			return nil, err
		}

		defaultOpts, err := q.ListAllModifierGroupDefaultOptions(ctx)
		if err != nil {
			return nil, err
		}

		optionsByGroup := groupBy(options, modifierOptionGroupID, buildManagementModifierOption)
		defaultsByGroup := defaultOptionsByGroup(defaultOpts)

		var result []ManagementModifierGroupResponse
		for _, grp := range groups {
			result = append(result, buildManagementModifierGroup(grp, optionsByGroup[grp.ID], sortedCopy(defaultsByGroup[grp.ID])))
		}
		return nonNil(result), nil
	})
}

func buildManagementModifierOption(opt sqlc.ModifierOption) ManagementModifierOptionResponse {
	retiredAt, reason, note := retirementDetails(opt.RetiredAt, opt.RetirementReason, opt.RetirementNote)
	return ManagementModifierOptionResponse{
		ID:               opt.ID,
		ModifierGroupID:  opt.ModifierGroupID,
		Name:             opt.Name,
		SurchargeVND:     opt.SurchargeVnd,
		Available:        opt.Available,
		Retired:          opt.RetiredAt.Valid,
		RetiredAt:        retiredAt,
		RetirementReason: reason,
		RetirementNote:   note,
	}
}

func buildManagementModifierGroup(grp sqlc.ModifierGroup, options []ManagementModifierOptionResponse, defaultIDs []uuid.UUID) ManagementModifierGroupResponse {
	retiredAt, reason, note := retirementDetails(grp.RetiredAt, grp.RetirementReason, grp.RetirementNote)
	return ManagementModifierGroupResponse{
		ID:               grp.ID,
		Name:             grp.Name,
		MinSelections:    grp.MinSelections,
		MaxSelections:    grp.MaxSelections,
		Retired:          grp.RetiredAt.Valid,
		RetiredAt:        retiredAt,
		RetirementReason: reason,
		RetirementNote:   note,
		Options:          nonNil(options),
		DefaultOptionIDs: defaultIDs,
	}
}

func modifierOptionGroupID(opt sqlc.ModifierOption) uuid.UUID { return opt.ModifierGroupID }

// retirementDetails converts the nullable retirement columns every catalog
// entity carries into response pointers.
func retirementDetails(at sql.NullTime, reason, note sql.NullString) (*time.Time, *string, *string) {
	var retiredAt *time.Time
	if at.Valid {
		t := at.Time
		retiredAt = &t
	}
	return retiredAt, nullStringPtr(reason), nullStringPtr(note)
}

func nullInt64Ptr(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}

// groupBy buckets rows by key, keeping row order within each bucket.
func groupBy[T, V any](rows []T, key func(T) uuid.UUID, value func(T) V) map[uuid.UUID][]V {
	out := make(map[uuid.UUID][]V)
	for _, r := range rows {
		k := key(r)
		out[k] = append(out[k], value(r))
	}
	return out
}

// nonNil returns s, or an empty slice when s is nil, so a list encodes as []
// rather than null.
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// sortedCopy returns a sorted copy of ids that is never nil.
func sortedCopy(ids []uuid.UUID) []uuid.UUID {
	out := make([]uuid.UUID, len(ids))
	copy(out, ids)
	sortUUIDs(out)
	return out
}

func sortUUIDs(ids []uuid.UUID) {
	sort.Slice(ids, func(i, j int) bool {
		return ids[i].String() < ids[j].String()
	})
}

// sortByNormalizedNameThenID sorts items in place by the normalized name of each
// item (the display value of NormalizeName(nameOf(item))), then idOf(item).String(),
// computing each key exactly once per element instead of on every comparison.
func sortByNormalizedNameThenID[T any](items []T, nameOf func(T) string, idOf func(T) uuid.UUID) {
	type keyed struct {
		key string
		id  string
		idx int
	}
	keys := make([]keyed, len(items))
	for i, it := range items {
		key, _ := NormalizeName(nameOf(it))
		keys[i] = keyed{key: key, id: idOf(it).String(), idx: i}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].key != keys[j].key {
			return keys[i].key < keys[j].key
		}
		return keys[i].id < keys[j].id
	})
	sorted := make([]T, len(items))
	for i, k := range keys {
		sorted[i] = items[k.idx]
	}
	copy(items, sorted)
}
