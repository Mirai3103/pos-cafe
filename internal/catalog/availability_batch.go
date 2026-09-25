package catalog

import (
	"context"
	"fmt"
	"sort"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
)

type availabilityBatchFingerprint struct {
	Changes []AvailabilityChange `json:"changes"`
}

type availabilityBatchAuditChange struct {
	Kind         string    `json:"kind"`
	ID           uuid.UUID `json:"id"`
	OldAvailable bool      `json:"old_available"`
	NewAvailable bool      `json:"new_available"`
}

type availabilityBatchChangedAuditDetails struct {
	Changes []availabilityBatchAuditChange `json:"changes"`
}

// SetAvailabilityBatchHandler applies many availability changes atomically.
type SetAvailabilityBatchHandler struct {
	runner *Runner
}

// NewSetAvailabilityBatchHandler creates a new SetAvailabilityBatchHandler.
func NewSetAvailabilityBatchHandler(runner *Runner) *SetAvailabilityBatchHandler {
	return &SetAvailabilityBatchHandler{runner: runner}
}

// Handle executes the batch availability command. Every entry succeeds or the
// whole batch rolls back; one audit event lists the entries that changed.
func (h *SetAvailabilityBatchHandler) Handle(ctx context.Context, actor Actor, cmd SetAvailabilityBatchCommand) (int, AvailabilityBatchResponse, error) {
	changes, err := NormalizeAvailabilityChanges(cmd.Changes)
	if err != nil {
		return 0, AvailabilityBatchResponse{}, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error())
	}

	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpAvailabilitySetBatch,
		Fingerprint: availabilityBatchFingerprint{Changes: changes},
		Required:    []string{CapManageAvailability},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec, func(q *sqlc.Queries) (int, AvailabilityBatchResponse, AuditRecord, error) {
		steps, err := planAvailabilityLocks(ctx, q, changes)
		if err != nil {
			return 0, AvailabilityBatchResponse{}, AuditRecord{}, err
		}

		previous := make(map[string]bool, len(steps))
		for _, step := range steps {
			old, err := applyAvailabilityChange(ctx, q, step.change)
			if err != nil {
				return 0, AvailabilityBatchResponse{}, AuditRecord{}, err
			}
			previous[availabilityKey(step.change)] = old
		}

		res := AvailabilityBatchResponse{Results: make([]AvailabilityBatchResult, 0, len(changes))}
		var audited []availabilityBatchAuditChange
		for _, c := range changes {
			old := previous[availabilityKey(c)]
			changed := old != c.Available
			res.Results = append(res.Results, AvailabilityBatchResult{
				Kind: c.Kind, ID: c.ID, Available: c.Available, Changed: changed,
			})
			if changed {
				audited = append(audited, availabilityBatchAuditChange{
					Kind: c.Kind, ID: c.ID, OldAvailable: old, NewAvailable: c.Available,
				})
			}
		}

		if len(audited) == 0 {
			return 200, res, AuditRecord{}, nil
		}
		return 200, res, AuditRecord{
			EventType: EventAvailabilityBatchChanged,
			Details:   availabilityBatchChangedAuditDetails{Changes: audited},
		}, nil
	})
}

func availabilityKey(c AvailabilityChange) string {
	return c.Kind + ":" + c.ID.String()
}

// availabilityLockStep places one change in the global lock order.
type availabilityLockStep struct {
	change    AvailabilityChange
	treeRank  int       // 0: Menu Item tree, 1: Modifier Group tree
	parentID  uuid.UUID // the Menu Item or Modifier Group that is locked first
	childRank int       // 0: the parent itself, 1: a child of it
}

// planAvailabilityLocks resolves each entry's parent without locking, then
// orders the entries so that every batch locks rows in one global order:
// Menu Items by id, each followed by its Sizes; then Modifier Groups by id,
// each followed by its Options. The lock helpers take the parent before the
// child, so ordering by (kind, id) alone could deadlock crossed batches.
func planAvailabilityLocks(ctx context.Context, q *sqlc.Queries, changes []AvailabilityChange) ([]availabilityLockStep, error) {
	steps := make([]availabilityLockStep, 0, len(changes))
	for _, c := range changes {
		step := availabilityLockStep{change: c}
		switch c.Kind {
		case AvailabilityKindItem:
			step.treeRank, step.parentID, step.childRank = 0, c.ID, 0
		case AvailabilityKindSize:
			row, err := q.GetMenuItemSizeByID(ctx, c.ID)
			if err != nil {
				return nil, MapDBError(err)
			}
			step.treeRank, step.parentID, step.childRank = 0, row.MenuItemID, 1
		case AvailabilityKindModifierOption:
			row, err := q.GetModifierOptionByID(ctx, c.ID)
			if err != nil {
				return nil, MapDBError(err)
			}
			step.treeRank, step.parentID, step.childRank = 1, row.ModifierGroupID, 1
		}
		steps = append(steps, step)
	}
	sort.Slice(steps, func(i, j int) bool {
		a, b := steps[i], steps[j]
		if a.treeRank != b.treeRank {
			return a.treeRank < b.treeRank
		}
		if a.parentID != b.parentID {
			return a.parentID.String() < b.parentID.String()
		}
		if a.childRank != b.childRank {
			return a.childRank < b.childRank
		}
		return a.change.ID.String() < b.change.ID.String()
	})
	return steps, nil
}

// applyAvailabilityChange locks one entity, refuses a retired one, writes the
// new state unless it is already current, and returns the previous state.
func applyAvailabilityChange(ctx context.Context, q *sqlc.Queries, c AvailabilityChange) (bool, error) {
	switch c.Kind {
	case AvailabilityKindItem:
		existing, err := q.GetMenuItemForUpdate(ctx, c.ID)
		if err != nil {
			return false, MapDBError(err)
		}
		if existing.RetiredAt.Valid {
			return false, ErrEntityRetired
		}
		if existing.Available != c.Available {
			if _, err := q.SetMenuItemAvailability(ctx, sqlc.SetMenuItemAvailabilityParams{ID: c.ID, Available: c.Available}); err != nil {
				return false, MapDBError(err)
			}
		}
		return existing.Available, nil
	case AvailabilityKindSize:
		existing, err := lockSizeWithParentCheck(ctx, q, c.ID)
		if err != nil {
			return false, err
		}
		if existing.Available != c.Available {
			if _, err := q.SetMenuItemSizeAvailability(ctx, sqlc.SetMenuItemSizeAvailabilityParams{ID: c.ID, Available: c.Available}); err != nil {
				return false, MapDBError(err)
			}
		}
		return existing.Available, nil
	case AvailabilityKindModifierOption:
		existing, err := lockModifierOptionWithParentCheck(ctx, q, c.ID)
		if err != nil {
			return false, err
		}
		if existing.Available != c.Available {
			if _, err := q.SetModifierOptionAvailability(ctx, sqlc.SetModifierOptionAvailabilityParams{ID: c.ID, Available: c.Available}); err != nil {
				return false, MapDBError(err)
			}
		}
		return existing.Available, nil
	}
	return false, fmt.Errorf("%w: unknown availability kind %q", response.ErrInvalid, c.Kind)
}
