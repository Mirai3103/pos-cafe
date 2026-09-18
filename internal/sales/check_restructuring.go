package sales

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
)

// lockChecks acquires several Checks under the uniform 5C protocol and
// evaluates the preconditions shared by Split and Merge.
//
// Every requested id must come back: a missing one means the client named a
// Check that does not exist.
func lockChecks(ctx context.Context, q *sqlc.Queries, ids []uuid.UUID) (
	map[uuid.UUID]lockedCheck, error,
) {
	rows, err := q.LockChecksForRestructuring(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("lock checks for restructuring: %w", err)
	}

	out := make(map[uuid.UUID]lockedCheck, len(rows))
	for _, row := range rows {
		out[row.ID] = lockedCheck{
			ID:               row.ID,
			State:            row.State,
			ChargeVND:        row.ChargeVnd,
			ServiceSessionID: row.ServiceSessionID,
		}
	}
	for _, id := range ids {
		if _, ok := out[id]; !ok {
			return nil, fmt.Errorf("%w: check %s", ErrCheckNotFound, id)
		}
	}

	// Belonging to one Service Session is a property of the set rather than of
	// any one Check, so it is decided before the per-Check preconditions. §6.4
	// lists it alongside them.
	var session uuid.UUID
	for _, row := range rows {
		if session == uuid.Nil {
			session = row.ServiceSessionID
		} else if row.ServiceSessionID != session {
			return nil, fmt.Errorf("%w: %s and %s",
				ErrChecksDifferentSession, session, row.ServiceSessionID)
		}
	}

	// The Session is locked after every Check (ADR-030). Locking it inside the
	// Check statement would put it between two Check locks and deadlock against
	// a Payment holding a sibling Check. The state used by checkPreconditions
	// comes from this lock, not from the Check rows.
	//
	// The not-found branch is defensive: a Session row cannot disappear while
	// its Checks are locked by the same transaction, because there is no
	// session-delete path.
	sessionRow, err := q.LockServiceSessionForUpdate(ctx, session)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: session %s", ErrServiceSessionNotFound, session)
		}
		return nil, fmt.Errorf("lock service session: %w", err)
	}
	shiftID, err := lockOpenSalesShift(ctx, q)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		check := out[id]
		check.ServiceSessionState = sessionRow.State
		check.SalesShiftID = shiftID
		out[id] = check
	}
	for _, id := range ids {
		if err := checkPreconditions(out[id]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// assertNoPayments refuses to restructure a Check that has taken money.
//
// This is the central rule of 5C. Arranging Checks is work done before money
// is taken; once money has been taken, a Check's structure is reconciliation
// evidence rather than a sorting tool. The guard is stricter than CONTEXT.md's
// "a Settled Check accepts no merge or split" on purpose: it blocks on the
// first Payment, because a partially paid Check is already evidence.
func assertNoPayments(ctx context.Context, q *sqlc.Queries, ids []uuid.UUID) error {
	n, err := q.CountPaymentsForChecks(ctx, ids)
	if err != nil {
		return fmt.Errorf("count payments for checks: %w", err)
	}
	if n > 0 {
		return fmt.Errorf("%w: %d payment(s) recorded", ErrCheckHasPayment, n)
	}
	return nil
}

// assertNoLiveChargeAdjustments refuses to restructure a Check whose charge
// has been reduced by a Cancellation or Comp.
//
// A live adjustment names the exact Charge Allocation it reduced, so a Split
// or Merge that rewrote or moved that allocation would strand the adjustment
// (design §6.2). The Check lock serializes the two commands: once a correction
// commits, restructuring rejects here.
func assertNoLiveChargeAdjustments(ctx context.Context, q *sqlc.Queries, ids []uuid.UUID) error {
	n, err := q.CountLiveChargeAdjustmentsForChecks(ctx, ids)
	if err != nil {
		return fmt.Errorf("count live charge adjustments: %w", err)
	}
	if n != 0 {
		return ErrCheckHasChargeAdjustment
	}
	return nil
}

// ValidateSplitItems rejects a moved-item list that cannot describe a split.
func ValidateSplitItems(items []SplitItem) error {
	if len(items) == 0 {
		return fmt.Errorf("%w: at least one item must be moved", ErrInvalidCheckSplit)
	}
	seen := make(map[uuid.UUID]struct{}, len(items))
	for _, item := range items {
		if _, dup := seen[item.CommittedItemID]; dup {
			return fmt.Errorf("%w: committed item %s listed twice",
				ErrInvalidCheckSplit, item.CommittedItemID)
		}
		seen[item.CommittedItemID] = struct{}{}
		// §6.4 names a non-positive quantity as a split the client cannot have
		// meant. The upper bound is a different kind of rule — a data-entry
		// guard on the field's shape — so it is request validation (ADR-018).
		if item.Quantity < MinQuantity {
			return fmt.Errorf("%w: quantity %d must be at least %d",
				ErrInvalidCheckSplit, item.Quantity, MinQuantity)
		}
		if item.Quantity > MaxQuantity {
			return fmt.Errorf("%w: quantity %d exceeds the maximum of %d",
				response.ErrInvalid, item.Quantity, MaxQuantity)
		}
	}
	return nil
}

// NormalizeSplitItems sorts by Committed Item so the request fingerprint does
// not depend on the order the client happened to list them in.
func NormalizeSplitItems(items []SplitItem) []SplitItem {
	out := make([]SplitItem, len(items))
	copy(out, items)
	sort.Slice(out, func(i, j int) bool {
		return out[i].CommittedItemID.String() < out[j].CommittedItemID.String()
	})
	return out
}

type splitFingerprint struct {
	SourceCheckID uuid.UUID   `json:"source_check_id"`
	Destination   string      `json:"destination"`
	DestinationID *uuid.UUID  `json:"destination_check_id"`
	Items         []SplitItem `json:"items"`
}

type splitAudit struct {
	ServiceSessionID   uuid.UUID   `json:"service_session_id"`
	SourceCheckID      uuid.UUID   `json:"source_check_id"`
	DestinationCheckID uuid.UUID   `json:"destination_check_id"`
	MovedItems         []SplitItem `json:"moved_items"`
	MovedChargeVND     int64       `json:"moved_charge_vnd"`
}

// SplitCheckHandler moves part of a Check's charge onto another Check.
type SplitCheckHandler struct{ runner *Runner }

// NewSplitCheckHandler creates a new SplitCheckHandler.
func NewSplitCheckHandler(runner *Runner) *SplitCheckHandler {
	return &SplitCheckHandler{runner: runner}
}

// Handle redistributes allocation quantities between two Checks.
func (h *SplitCheckHandler) Handle(ctx context.Context, actor Actor, cmd SplitCheckCommand) (
	int, ServiceSessionResponse, error,
) {
	var zero ServiceSessionResponse

	items := NormalizeSplitItems(cmd.Items)
	if err := ValidateSplitItems(items); err != nil {
		return 0, zero, err
	}
	switch cmd.Destination.Type {
	case SplitDestinationNewCheck:
		if cmd.Destination.CheckID != nil {
			return 0, zero, fmt.Errorf("%w: destination.check_id is not allowed for NEW_CHECK",
				response.ErrInvalid)
		}
	case SplitDestinationExistingCheck:
		if cmd.Destination.CheckID == nil {
			return 0, zero, fmt.Errorf("%w: destination.check_id is required for EXISTING_CHECK",
				response.ErrInvalid)
		}
		if *cmd.Destination.CheckID == cmd.SourceCheckID {
			return 0, zero, fmt.Errorf("%w: destination is the source check", ErrInvalidCheckSplit)
		}
	default:
		return 0, zero, fmt.Errorf("%w: destination.type must be %s or %s",
			response.ErrInvalid, SplitDestinationNewCheck, SplitDestinationExistingCheck)
	}

	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpSplitCheck,
		Fingerprint: splitFingerprint{
			SourceCheckID: cmd.SourceCheckID,
			Destination:   cmd.Destination.Type,
			DestinationID: cmd.Destination.CheckID,
			Items:         items,
		},
		Required: []string{CapSalesOperate},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, ServiceSessionResponse, AuditRecord, error) {
			q := mc.Queries

			ids := []uuid.UUID{cmd.SourceCheckID}
			if cmd.Destination.CheckID != nil {
				ids = append(ids, *cmd.Destination.CheckID)
			}
			locked, err := lockChecks(ctx, q, ids)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			if err := assertNoLiveChargeAdjustments(ctx, q, ids); err != nil {
				return 0, zero, AuditRecord{}, err
			}
			if err := assertNoPayments(ctx, q, ids); err != nil {
				return 0, zero, AuditRecord{}, err
			}
			if err := assertChargesMatchAllocations(ctx, q, ids, locked); err != nil {
				return 0, zero, AuditRecord{}, err
			}
			source := locked[cmd.SourceCheckID]

			itemIDs := make([]uuid.UUID, 0, len(items))
			for _, item := range items {
				itemIDs = append(itemIDs, item.CommittedItemID)
			}
			allocRows, err := q.ListAllocationsForItems(ctx, sqlc.ListAllocationsForItemsParams{
				CheckID: source.ID, CommittedItemIds: itemIDs,
			})
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("list source allocations: %w", err)
			}
			sourceByItem := make(map[uuid.UUID]sqlc.ListAllocationsForItemsRow, len(allocRows))
			for _, row := range allocRows {
				sourceByItem[row.CommittedItemID] = row
			}

			var movedChargeVND int64
			for _, item := range items {
				allocation, ok := sourceByItem[item.CommittedItemID]
				if !ok {
					return 0, zero, AuditRecord{}, fmt.Errorf("%w: committed item %s",
						ErrSplitAllocationNotFound, item.CommittedItemID)
				}
				if item.Quantity > allocation.Quantity {
					return 0, zero, AuditRecord{}, fmt.Errorf("%w: %d of %d for committed item %s",
						ErrSplitQuantityExceedsAllocation, item.Quantity,
						allocation.Quantity, item.CommittedItemID)
				}
				amountVND, err := LineTotal(item.Quantity, allocation.UnitPriceVnd)
				if err != nil {
					return 0, zero, AuditRecord{}, err
				}
				movedChargeVND, err = AddCharge(movedChargeVND, amountVND)
				if err != nil {
					return 0, zero, AuditRecord{}, err
				}
			}

			sourceChargeVND, err := SubtractCharge(source.ChargeVND, movedChargeVND)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			// A Check cannot be split empty. Moving everything is Merge,
			// which says what it means.
			if sourceChargeVND <= 0 {
				return 0, zero, AuditRecord{}, fmt.Errorf("%w: source check %s",
					ErrSplitSourceWouldBeEmpty, source.ID)
			}

			var destinationChargeVND int64
			destinationID := uuid.Nil
			if cmd.Destination.CheckID != nil {
				destination := locked[*cmd.Destination.CheckID]
				destinationID = destination.ID
				destinationChargeVND, err = AddCharge(destination.ChargeVND, movedChargeVND)
			} else {
				destinationChargeVND, err = AddCharge(0, movedChargeVND)
			}
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			if destinationChargeVND <= 0 {
				return 0, zero, AuditRecord{}, fmt.Errorf("%w: nothing would be charged",
					ErrSplitDestinationWouldBeEmpty)
			}

			occurredAt := time.Now()
			if destinationID == uuid.Nil {
				created, err := q.InsertCheck(ctx, sqlc.InsertCheckParams{
					ServiceSessionID: source.ServiceSessionID,
					CreatedAt:        occurredAt,
				})
				if err != nil {
					return 0, zero, AuditRecord{}, fmt.Errorf("create destination check: %w", err)
				}
				destinationID = created.ID
			}

			destAllocRows, err := q.ListAllocationsForItems(ctx, sqlc.ListAllocationsForItemsParams{
				CheckID: destinationID, CommittedItemIds: itemIDs,
			})
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("list destination allocations: %w", err)
			}
			destByItem := make(map[uuid.UUID]sqlc.ListAllocationsForItemsRow, len(destAllocRows))
			for _, row := range destAllocRows {
				destByItem[row.CommittedItemID] = row
			}

			// The whole redistribution is planned in Go first, so applying it is
			// a fixed number of statements rather than one per moved item. Both
			// Checks are locked FOR UPDATE throughout, and the window other
			// Payments and restructurings wait on should not grow with the size
			// of this split.
			var setIDs []uuid.UUID
			var setQuantities []int32
			var deleteIDs []uuid.UUID
			var insertItemIDs []uuid.UUID
			var insertQuantities []int32
			for _, item := range items {
				allocation := sourceByItem[item.CommittedItemID]
				if item.Quantity == allocation.Quantity {
					deleteIDs = append(deleteIDs, allocation.ID)
				} else {
					setIDs = append(setIDs, allocation.ID)
					setQuantities = append(setQuantities, allocation.Quantity-item.Quantity)
				}

				if existing, ok := destByItem[item.CommittedItemID]; ok {
					setIDs = append(setIDs, existing.ID)
					setQuantities = append(setQuantities, existing.Quantity+item.Quantity)
					continue
				}
				insertItemIDs = append(insertItemIDs, item.CommittedItemID)
				insertQuantities = append(insertQuantities, item.Quantity)
			}

			if len(setIDs) > 0 {
				if err := q.SetAllocationQuantities(ctx, sqlc.SetAllocationQuantitiesParams{
					Ids: setIDs, Quantities: setQuantities,
				}); err != nil {
					return 0, zero, AuditRecord{}, fmt.Errorf("rewrite allocation quantities: %w", err)
				}
			}
			if len(deleteIDs) > 0 {
				if err := q.DeleteAllocations(ctx, deleteIDs); err != nil {
					return 0, zero, AuditRecord{}, fmt.Errorf("delete source allocations: %w", err)
				}
			}
			if len(insertItemIDs) > 0 {
				if err := q.InsertChargeAllocations(ctx, sqlc.InsertChargeAllocationsParams{
					CheckID:          destinationID,
					CreatedAt:        occurredAt,
					CommittedItemIds: insertItemIDs,
					Quantities:       insertQuantities,
				}); err != nil {
					return 0, zero, AuditRecord{}, fmt.Errorf("insert destination allocations: %w", err)
				}
			}

			if err := q.SetCheckCharge(ctx, sqlc.SetCheckChargeParams{
				ID: source.ID, ChargeVnd: sourceChargeVND,
			}); err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("rewrite source charge: %w", err)
			}
			if err := q.SetCheckCharge(ctx, sqlc.SetCheckChargeParams{
				ID: destinationID, ChargeVnd: destinationChargeVND,
			}); err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("rewrite destination charge: %w", err)
			}

			result, err := LoadServiceSession(ctx, q, source.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			return 200, result, AuditRecord{
				EventType: EventCheckSplit,
				Details: splitAudit{
					ServiceSessionID:   source.ServiceSessionID,
					SourceCheckID:      source.ID,
					DestinationCheckID: destinationID,
					MovedItems:         items,
					MovedChargeVND:     movedChargeVND,
				},
			}, nil
		})
}

type mergeFingerprint struct {
	SurvivingCheckID uuid.UUID `json:"surviving_check_id"`
	AbsorbedCheckID  uuid.UUID `json:"absorbed_check_id"`
}

type mergeAudit struct {
	ServiceSessionID uuid.UUID `json:"service_session_id"`
	SurvivingCheckID uuid.UUID `json:"surviving_check_id"`
	AbsorbedCheckID  uuid.UUID `json:"absorbed_check_id"`
	MergedChargeVND  int64     `json:"merged_charge_vnd"`
}

// MergeChecksHandler absorbs one Check into another.
type MergeChecksHandler struct{ runner *Runner }

// NewMergeChecksHandler creates a new MergeChecksHandler.
func NewMergeChecksHandler(runner *Runner) *MergeChecksHandler {
	return &MergeChecksHandler{runner: runner}
}

// Handle moves every allocation of the absorbed Check onto the survivor.
func (h *MergeChecksHandler) Handle(ctx context.Context, actor Actor, cmd MergeChecksCommand) (
	int, ServiceSessionResponse, error,
) {
	var zero ServiceSessionResponse
	if cmd.SurvivingCheckID == cmd.AbsorbedCheckID {
		return 0, zero, fmt.Errorf("%w: a check cannot absorb itself", ErrInvalidCheckMerge)
	}

	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpMergeChecks,
		Fingerprint: mergeFingerprint{
			SurvivingCheckID: cmd.SurvivingCheckID,
			AbsorbedCheckID:  cmd.AbsorbedCheckID,
		},
		Required: []string{CapSalesOperate},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, ServiceSessionResponse, AuditRecord, error) {
			q := mc.Queries

			ids := []uuid.UUID{cmd.SurvivingCheckID, cmd.AbsorbedCheckID}
			locked, err := lockChecks(ctx, q, ids)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			if err := assertNoLiveChargeAdjustments(ctx, q, ids); err != nil {
				return 0, zero, AuditRecord{}, err
			}
			if err := assertNoPayments(ctx, q, ids); err != nil {
				return 0, zero, AuditRecord{}, err
			}
			if err := assertChargesMatchAllocations(ctx, q, ids, locked); err != nil {
				return 0, zero, AuditRecord{}, err
			}
			surviving := locked[cmd.SurvivingCheckID]
			absorbed := locked[cmd.AbsorbedCheckID]

			absorbedAllocations, err := q.ListCheckAllocationQuantities(ctx, absorbed.ID)
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("list absorbed allocations: %w", err)
			}
			survivingAllocations, err := q.ListCheckAllocationQuantities(ctx, surviving.ID)
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("list surviving allocations: %w", err)
			}
			survivingByItem := make(map[uuid.UUID]sqlc.ListCheckAllocationQuantitiesRow,
				len(survivingAllocations))
			for _, row := range survivingAllocations {
				survivingByItem[row.CommittedItemID] = row
			}

			// Planned in Go first, like Split: three statements regardless of how
			// many allocations the absorbed Check carried, while both Checks are
			// locked FOR UPDATE.
			var setIDs []uuid.UUID
			var setQuantities []int32
			var moveIDs []uuid.UUID
			var absorbedIDs []uuid.UUID
			for _, allocation := range absorbedAllocations {
				existing, ok := survivingByItem[allocation.CommittedItemID]
				if !ok {
					// Nothing to combine with: the row simply changes Check.
					moveIDs = append(moveIDs, allocation.ID)
					continue
				}
				setIDs = append(setIDs, existing.ID)
				setQuantities = append(setQuantities, existing.Quantity+allocation.Quantity)
				absorbedIDs = append(absorbedIDs, allocation.ID)
			}

			if len(setIDs) > 0 {
				if err := q.SetAllocationQuantities(ctx, sqlc.SetAllocationQuantitiesParams{
					Ids: setIDs, Quantities: setQuantities,
				}); err != nil {
					return 0, zero, AuditRecord{}, fmt.Errorf("combine allocations: %w", err)
				}
			}
			if len(moveIDs) > 0 {
				if err := q.MoveAllocationsToCheck(ctx, sqlc.MoveAllocationsToCheckParams{
					CheckID: surviving.ID, Ids: moveIDs,
				}); err != nil {
					return 0, zero, AuditRecord{}, fmt.Errorf("move allocations: %w", err)
				}
			}
			if len(absorbedIDs) > 0 {
				if err := q.DeleteAllocations(ctx, absorbedIDs); err != nil {
					return 0, zero, AuditRecord{}, fmt.Errorf("delete absorbed allocations: %w", err)
				}
			}

			mergedChargeVND, err := AddCharge(surviving.ChargeVND, absorbed.ChargeVND)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			if err := q.SetCheckCharge(ctx, sqlc.SetCheckChargeParams{
				ID: surviving.ID, ChargeVnd: mergedChargeVND,
			}); err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("raise surviving charge: %w", err)
			}
			if err := q.MarkCheckMerged(ctx, sqlc.MarkCheckMergedParams{
				ID:                absorbed.ID,
				MergedIntoCheckID: uuid.NullUUID{UUID: surviving.ID, Valid: true},
			}); err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("mark check merged: %w", err)
			}

			result, err := LoadServiceSession(ctx, q, surviving.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			return 200, result, AuditRecord{
				EventType: EventCheckMerged,
				Details: mergeAudit{
					ServiceSessionID: surviving.ServiceSessionID,
					SurvivingCheckID: surviving.ID,
					AbsorbedCheckID:  absorbed.ID,
					MergedChargeVND:  mergedChargeVND,
				},
			}, nil
		})
}
