package preparation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/platform/database/sqlc"
	"github.com/google/uuid"
)

// cancelResolvedUnit is one selected unit's pre-resolved immutable facts: the
// Order Item chain, the owning Session, and — for a charged standard unit —
// the Charge Allocation whose quantity range covers its unit number and the
// immutable per-unit price. A Remake, or a standard unit beyond every
// allocation range, carries a zero CheckID and no charge.
type cancelResolvedUnit struct {
	UnitID             uuid.UUID
	State              string
	Priority           string
	OrderItemID        uuid.UUID
	ServiceSessionID   uuid.UUID
	ChargeAllocationID uuid.UUID
	CheckID            uuid.UUID
	UnitPriceVND       int64
}

// charged reports whether a resolved unit carries customer charge.
func (u cancelResolvedUnit) charged() bool { return u.CheckID != uuid.Nil }

// resolveCancelSelection resolves every selected unit and its charge mapping
// without taking a row lock, then enforces the lock-free batch
// preconditions. It returns the resolution and the one shared Service
// Session.
func resolveCancelSelection(ctx context.Context, q *sqlc.Queries,
	ids []uuid.UUID,
) (map[uuid.UUID]cancelResolvedUnit, uuid.UUID, error) {
	rows, err := q.ResolveCancellationUnits(ctx, ids)
	if err != nil {
		return nil, uuid.Nil, fmt.Errorf("resolve cancellation units: %w", err)
	}
	resolved, err := resolveCancelUnits(rows, ids)
	if err != nil {
		return nil, uuid.Nil, err
	}
	sessionID, err := validateCancelUnitsResolved(resolved, ids)
	if err != nil {
		return nil, uuid.Nil, err
	}
	return resolved, sessionID, nil
}

// resolveCancelUnits indexes a ResolveCancellationUnits result and rejects any
// selected id the query did not return: a missing unit is the 404 condition
// every selection member must fail on before anything is locked.
func resolveCancelUnits(rows []sqlc.ResolveCancellationUnitsRow,
	ids []uuid.UUID,
) (map[uuid.UUID]cancelResolvedUnit, error) {
	byID := make(map[uuid.UUID]cancelResolvedUnit, len(rows))
	for _, row := range rows {
		unit := cancelResolvedUnit{
			UnitID:           row.ID,
			State:            row.State,
			Priority:         row.Priority,
			OrderItemID:      row.OrderItemID,
			ServiceSessionID: row.ServiceSessionID,
		}
		if row.ChargeAllocationID.Valid {
			unit.ChargeAllocationID = row.ChargeAllocationID.UUID
		}
		if row.CheckID.Valid {
			unit.CheckID = row.CheckID.UUID
		}
		if row.UnitPriceVnd.Valid {
			unit.UnitPriceVND = row.UnitPriceVnd.Int64
		}
		byID[row.ID] = unit
	}
	for _, id := range ids {
		if _, found := byID[id]; !found {
			return nil, fmt.Errorf("%w: %s", ErrUnitNotFound, id)
		}
	}
	return byID, nil
}

// validateCancelUnitsResolved enforces the batch preconditions that need no
// lock: every unit is QUEUED and the whole selection shares one Service
// Session. A charged unit must resolve its allocation, Check, and positive
// price together; any other combination is a corrupt mapping, not a business
// state.
func validateCancelUnitsResolved(resolved map[uuid.UUID]cancelResolvedUnit,
	ids []uuid.UUID,
) (uuid.UUID, error) {
	var sessionID uuid.UUID
	for index, id := range ids {
		unit := resolved[id]
		if unit.State != StateQueued {
			return uuid.Nil, fmt.Errorf("%w: %s is %s",
				ErrCancellationSourceNotQueued, id, unit.State)
		}
		hasAllocation := unit.ChargeAllocationID != uuid.Nil
		hasCheck := unit.CheckID != uuid.Nil
		if hasAllocation != hasCheck {
			return uuid.Nil, fmt.Errorf("%w: unit %s resolves allocation %s but check %s",
				ErrChargeInvariantViolated, id, unit.ChargeAllocationID, unit.CheckID)
		}
		// Only a Remake is legitimately uncharged; a STANDARD unit beyond
		// every allocation range means its charge cannot be resolved, and
		// silently cancelling it without a reduction would under-refund.
		if unit.Priority != PriorityRemake && !hasCheck {
			return uuid.Nil, fmt.Errorf(
				"%w: standard unit %s resolves no charge allocation",
				ErrChargeInvariantViolated, id)
		}
		if hasCheck && unit.UnitPriceVND <= 0 {
			return uuid.Nil, fmt.Errorf("%w: unit %s resolves a non-positive price %d",
				ErrChargeInvariantViolated, id, unit.UnitPriceVND)
		}
		if index == 0 {
			sessionID = unit.ServiceSessionID
			continue
		}
		if unit.ServiceSessionID != sessionID {
			return uuid.Nil, fmt.Errorf(
				"%w: units span sessions %s and %s",
				ErrCancellationSessionMismatch, sessionID, unit.ServiceSessionID)
		}
	}
	return sessionID, nil
}

// revalidateCancelMapping re-resolves the selection under the locks: a Split
// or Merge that committed between the pre-resolution and the Check lock
// changed the established mapping, and building an adjustment on a stale
// attribution would strand it. The loser refuses whole rather than repair.
func revalidateCancelMapping(ctx context.Context, q *sqlc.Queries,
	ids []uuid.UUID, resolved map[uuid.UUID]cancelResolvedUnit,
) error {
	rows, err := q.ResolveCancellationUnits(ctx, ids)
	if err != nil {
		return fmt.Errorf("re-resolve cancellation units: %w", err)
	}
	revalidated, err := resolveCancelUnits(rows, ids)
	if err != nil {
		return err
	}
	for _, id := range ids {
		before, after := resolved[id], revalidated[id]
		if before.ChargeAllocationID != after.ChargeAllocationID ||
			before.CheckID != after.CheckID ||
			before.UnitPriceVND != after.UnitPriceVND ||
			before.Priority != after.Priority ||
			before.ServiceSessionID != after.ServiceSessionID {
			return fmt.Errorf(
				"%w: the charge mapping of unit %s changed concurrently",
				ErrChargeAdjustmentConflict, id)
		}
	}
	return nil
}

// validateCancellationReplacement enforces the CHANGE preconditions with
// non-locking reads: the replacement Order must exist, belong to the same
// active Service Session, differ from every source Order, and have been
// submitted after every source Order.
func validateCancellationReplacement(ctx context.Context, q *sqlc.Queries,
	cmd CancelUnitsCommand, sessionID uuid.UUID, resolved map[uuid.UUID]cancelResolvedUnit,
) error {
	if cmd.Kind != CancelKindChange {
		return nil
	}

	sourceOrderIDs, err := cancelSourceOrderIDs(ctx, q, sessionID, resolved)
	if err != nil {
		return err
	}

	replacement, err := q.GetCancellationReplacementOrder(ctx,
		sqlc.GetCancellationReplacementOrderParams{
			ServiceSessionID:   sessionID,
			SourceOrderIds:     uuidSortedCopy(sourceOrderIDs),
			ReplacementOrderID: *cmd.ReplacementOrderID,
		})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: %s", ErrReplacementOrderNotFound, *cmd.ReplacementOrderID)
		}
		return fmt.Errorf("resolve replacement order: %w", err)
	}
	if !replacement.SameSession {
		return fmt.Errorf("%w: %s belongs to another service session",
			ErrReplacementOrderInvalid, replacement.ID)
	}
	if !replacement.DiffersFromSourceOrders {
		return fmt.Errorf("%w: %s is a source order of the selection",
			ErrReplacementOrderInvalid, replacement.ID)
	}
	if !replacement.SubmittedAfterSourceOrders {
		return fmt.Errorf("%w: %s was not submitted after every source order",
			ErrReplacementOrderInvalid, replacement.ID)
	}
	return nil
}

// cancelSourceOrderIDs resolves the distinct source Orders of a selection
// through the immutable Order Item chain; nothing about them can change after
// Submit.
func cancelSourceOrderIDs(ctx context.Context, q *sqlc.Queries,
	sessionID uuid.UUID, resolved map[uuid.UUID]cancelResolvedUnit,
) ([]uuid.UUID, error) {
	orders, err := q.ListSessionOrders(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list session orders for replacement resolution: %w", err)
	}
	orderIDs := make([]uuid.UUID, 0, len(orders))
	for _, order := range orders {
		orderIDs = append(orderIDs, order.ID)
	}
	items, err := q.ListOrderItems(ctx, orderIDs)
	if err != nil {
		return nil, fmt.Errorf("list order items for replacement resolution: %w", err)
	}
	orderByItem := make(map[uuid.UUID]uuid.UUID, len(items))
	for _, item := range items {
		orderByItem[item.ID] = item.OrderID
	}

	sourceSet := make(map[uuid.UUID]struct{}, len(resolved))
	for _, unit := range resolved {
		orderID, found := orderByItem[unit.OrderItemID]
		if !found {
			return nil, fmt.Errorf("%w: unit %s resolves no source order",
				ErrChargeInvariantViolated, unit.UnitID)
		}
		sourceSet[orderID] = struct{}{}
	}
	sourceOrderIDs := make([]uuid.UUID, 0, len(sourceSet))
	for orderID := range sourceSet {
		sourceOrderIDs = append(sourceOrderIDs, orderID)
	}
	return sourceOrderIDs, nil
}

// cancelCheckIDs collects the affected Checks of a selection, deduplicated and
// ascending in UUID byte order so every multi-row Check lock is deterministic.
func cancelCheckIDs(resolved map[uuid.UUID]cancelResolvedUnit,
	ids []uuid.UUID,
) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(ids))
	checkIDs := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		unit := resolved[id]
		if !unit.charged() {
			continue
		}
		if _, duplicate := seen[unit.CheckID]; duplicate {
			continue
		}
		seen[unit.CheckID] = struct{}{}
		checkIDs = append(checkIDs, unit.CheckID)
	}
	return uuidSortedCopy(checkIDs)
}
