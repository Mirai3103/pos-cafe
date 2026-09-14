package sales

import (
	"context"
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
			SalesShiftID:     row.SalesShiftID,
		}
	}
	for _, id := range ids {
		if _, ok := out[id]; !ok {
			return nil, fmt.Errorf("%w: check %s", ErrCheckNotFound, id)
		}
	}

	var session uuid.UUID
	for _, row := range rows {
		if session == uuid.Nil {
			session = row.ServiceSessionID
		} else if row.ServiceSessionID != session {
			return nil, fmt.Errorf("%w: %s and %s",
				ErrChecksDifferentSession, session, row.ServiceSessionID)
		}
		if row.State != CheckStateOpen {
			return nil, fmt.Errorf("%w: check %s is %s", ErrCheckNotOpen, row.ID, row.State)
		}
		if row.ServiceSessionState != SessionStateActive {
			return nil, fmt.Errorf("%w: session %s", ErrServiceSessionClosed, row.ServiceSessionID)
		}
		if row.SalesShiftState != ShiftStateOpen {
			return nil, fmt.Errorf("%w: check %s", ErrOpenShiftRequired, row.ID)
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
		if item.Quantity < MinQuantity || item.Quantity > MaxQuantity {
			return fmt.Errorf("%w: quantity %d is outside [%d, %d]",
				ErrInvalidCheckSplit, item.Quantity, MinQuantity, MaxQuantity)
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
			if err := assertNoPayments(ctx, q, ids); err != nil {
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

			for _, item := range items {
				allocation := sourceByItem[item.CommittedItemID]
				if item.Quantity == allocation.Quantity {
					if err := q.DeleteAllocation(ctx, allocation.ID); err != nil {
						return 0, zero, AuditRecord{}, fmt.Errorf("delete source allocation: %w", err)
					}
				} else {
					if err := q.SetAllocationQuantity(ctx, sqlc.SetAllocationQuantityParams{
						ID: allocation.ID, Quantity: allocation.Quantity - item.Quantity,
					}); err != nil {
						return 0, zero, AuditRecord{}, fmt.Errorf("reduce source allocation: %w", err)
					}
				}

				if existing, ok := destByItem[item.CommittedItemID]; ok {
					if err := q.SetAllocationQuantity(ctx, sqlc.SetAllocationQuantityParams{
						ID: existing.ID, Quantity: existing.Quantity + item.Quantity,
					}); err != nil {
						return 0, zero, AuditRecord{}, fmt.Errorf("raise destination allocation: %w", err)
					}
					continue
				}
				if err := q.InsertChargeAllocation(ctx, sqlc.InsertChargeAllocationParams{
					CommittedItemID: item.CommittedItemID,
					CheckID:         destinationID,
					Quantity:        item.Quantity,
					CreatedAt:       occurredAt,
				}); err != nil {
					return 0, zero, AuditRecord{}, fmt.Errorf("insert destination allocation: %w", err)
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
