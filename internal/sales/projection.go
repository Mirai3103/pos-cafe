package sales

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

// newServiceSessionResponse returns a response with every collection
// initialized, so the JSON contract never emits null for an array.
func newServiceSessionResponse() ServiceSessionResponse {
	return ServiceSessionResponse{
		Tables:           make([]SessionTableResponse, 0),
		Checks:           make([]CheckResponse, 0),
		Orders:           make([]struct{}, 0),
		PreparationUnits: make([]struct{}, 0),
	}
}

// newDraftItemResponse returns a draft item with its collection initialized,
// so the JSON contract never emits null for selected_modifier_options.
func newDraftItemResponse() DraftItemResponse {
	return DraftItemResponse{
		SelectedModifierOptions: make([]SelectedModifierOptionResponse, 0),
	}
}

// LoadServiceSession assembles the full projection for one Service Session.
//
// Every mutation returns this, so a client never needs a follow-up read and a
// composition merge that changed an id the client was holding is immediately
// visible. Callers must run it inside the same transaction as their mutation.
func LoadServiceSession(ctx context.Context, q *sqlc.Queries, sessionID uuid.UUID) (
	ServiceSessionResponse, error,
) {
	out := newServiceSessionResponse()

	session, err := q.GetServiceSession(ctx, sessionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return out, fmt.Errorf("%w: %s", ErrServiceSessionNotFound, sessionID)
		}
		return out, fmt.Errorf("load service session: %w", err)
	}

	out.ID = session.ID
	out.ServiceNumber = session.ServiceNumber
	out.ServiceMode = session.ServiceMode
	out.State = session.State
	out.SalesShiftID = session.SalesShiftID
	out.CreatedAt = session.CreatedAt

	tableRows, err := q.ListServiceSessionTables(ctx, sessionID)
	if err != nil {
		return out, fmt.Errorf("load session tables: %w", err)
	}
	for _, row := range tableRows {
		out.Tables = append(out.Tables, SessionTableResponse{ID: row.ID, Name: row.Name})
	}

	// Checks load before the draft block: a Session that has committed its
	// only draft has Checks but no EDITABLE draft, so the no-draft path below
	// must still carry them.
	checks, err := loadChecks(ctx, q, sessionID)
	if err != nil {
		return out, err
	}
	out.Checks = checks

	draft, err := q.GetEditableDraft(ctx, sessionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// 5A always creates a draft with its Session, so this is only
			// reachable once 5B can commit one without a successor.
			return out, nil
		}
		return out, fmt.Errorf("load editable draft: %w", err)
	}

	items, err := loadDraftItems(ctx, q, draft.ID)
	if err != nil {
		return out, err
	}
	out.Draft = &OrderDraftResponse{
		ID:          draft.ID,
		State:       draft.State,
		CheckTarget: draft.CheckTarget,
		Items:       items,
	}

	return out, nil
}

// loadDraftItems reads the draft's items and their selected options in two
// queries rather than one per item.
func loadDraftItems(ctx context.Context, q *sqlc.Queries, draftID uuid.UUID) (
	[]DraftItemResponse, error,
) {
	itemRows, err := q.ListDraftItems(ctx, draftID)
	if err != nil {
		return nil, fmt.Errorf("load draft items: %w", err)
	}

	optionRows, err := q.ListDraftItemModifierOptions(ctx, draftID)
	if err != nil {
		return nil, fmt.Errorf("load draft item modifier options: %w", err)
	}

	// The query already orders by group name then option name, so appending in
	// scan order preserves the projection's ordering per item.
	byItem := make(map[uuid.UUID][]SelectedModifierOptionResponse, len(itemRows))
	for _, row := range optionRows {
		byItem[row.OrderDraftItemID] = append(byItem[row.OrderDraftItemID],
			SelectedModifierOptionResponse{
				ID:           row.OptionID,
				GroupID:      row.GroupID,
				GroupName:    row.GroupName,
				Name:         row.OptionName,
				SurchargeVND: row.SurchargeVnd,
			})
	}

	items := make([]DraftItemResponse, 0, len(itemRows))
	for _, row := range itemRows {
		options := byItem[row.ID]
		if options == nil {
			options = make([]SelectedModifierOptionResponse, 0)
		}
		item := DraftItemResponse{
			ID:         row.ID,
			MenuItemID: row.MenuItemID,
			Name:       row.MenuItemName,
			Quantity:   row.Quantity,
			// The availability expression is never SQL NULL: mi.available is
			// NOT NULL, IS NULL predicates never yield NULL, and s.available is
			// NULL only when di.size_id IS NULL has already made the OR true.
			Available:               row.Available.Bool,
			SelectedModifierOptions: options,
		}
		if row.PriceVnd.Valid {
			v := row.PriceVnd.Int64
			item.PriceVND = &v
		}
		if row.SizeID.Valid {
			id := row.SizeID.UUID
			item.SizeID = &id
		}
		if row.SizeName.Valid {
			name := row.SizeName.String
			item.SizeName = &name
		}
		if row.PreparationNote.Valid {
			note := row.PreparationNote.String
			item.PreparationNote = &note
		}
		items = append(items, item)
	}
	return items, nil
}

// loadChecks assembles every Check of a Service Session with its allocations.
//
// The Check's stored charge_vnd is a denormalization of the sum over its
// allocations, in the same spirit as 5A's modifier_key: derived, never
// authoritative. It is stored rather than always derived because 5C freezes
// the charge of a settled or merged Check, at which point the live sum stops
// being the right answer. Every read therefore recomputes and compares, and a
// mismatch fails the read rather than serving a wrong total.
func loadChecks(ctx context.Context, q *sqlc.Queries, sessionID uuid.UUID) (
	[]CheckResponse, error,
) {
	checkRows, err := q.ListSessionChecks(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("load session checks: %w", err)
	}

	out := make([]CheckResponse, 0, len(checkRows))
	for _, row := range checkRows {
		allocations, allocatedVND, err := loadCheckAllocations(ctx, q, row.ID)
		if err != nil {
			return nil, err
		}
		if allocatedVND != row.ChargeVnd {
			slog.Error("check charge does not match its allocations",
				"check_id", row.ID,
				"stored_charge_vnd", row.ChargeVnd,
				"allocated_vnd", allocatedVND)
			return nil, fmt.Errorf("%w: check %s", ErrChargeInvariantViolated, row.ID)
		}

		// No Payment exists before 5C, so applied is zero and the balance is
		// the whole charge. Both ship in their final shape.
		out = append(out, CheckResponse{
			ID:              row.ID,
			State:           row.State,
			ChargeVND:       row.ChargeVnd,
			TotalAppliedVND: 0,
			BalanceVND:      row.ChargeVnd,
			CreatedAt:       row.CreatedAt,
			Payments:        make([]struct{}, 0),
			Allocations:     allocations,
		})
	}
	return out, nil
}

// loadCheckAllocations returns one Check's allocations and their summed amount.
func loadCheckAllocations(ctx context.Context, q *sqlc.Queries, checkID uuid.UUID) (
	[]ChargeAllocationResponse, int64, error,
) {
	rows, err := q.ListCheckAllocations(ctx, checkID)
	if err != nil {
		return nil, 0, fmt.Errorf("load check allocations: %w", err)
	}

	itemIDs := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		itemIDs = append(itemIDs, row.CommittedItemID)
	}
	modifiers, err := loadCommittedModifiers(ctx, q, itemIDs)
	if err != nil {
		return nil, 0, err
	}

	out := make([]ChargeAllocationResponse, 0, len(rows))
	var totalVND int64
	for _, row := range rows {
		amountVND, err := LineTotal(row.AllocatedQuantity, row.UnitPriceVnd)
		if err != nil {
			return nil, 0, err
		}
		totalVND, err = AddCharge(totalVND, amountVND)
		if err != nil {
			return nil, 0, err
		}

		mods := modifiers[row.CommittedItemID]
		if mods == nil {
			mods = make([]CommittedModifierResponse, 0)
		}
		out = append(out, ChargeAllocationResponse{
			ID:                row.ID,
			CommittedItemID:   row.CommittedItemID,
			MenuItemID:        row.MenuItemID,
			CategoryName:      row.CategoryName,
			Name:              row.ItemName,
			SizeName:          nullStringPtr(row.SizeName),
			PreparationNote:   nullStringPtr(row.PreparationNote),
			Modifiers:         mods,
			CommittedQuantity: row.CommittedQuantity,
			CommittedTotalVND: row.CommittedTotalVnd,
			AllocatedQuantity: row.AllocatedQuantity,
			AmountVND:         amountVND,
			CreatedAt:         row.CreatedAt,
			// Filled by 5D, which introduces the orders table this is
			// derived from.
			Submitted: false,
		})
	}
	return out, totalVND, nil
}

// loadCommittedModifiers groups frozen modifier snapshots by Committed Item.
// The query orders by (group name, option name), so presentation order comes
// from the read rather than from insert order — the table carries no ordering
// column, exactly as 5A's selected options do not.
func loadCommittedModifiers(ctx context.Context, q *sqlc.Queries, itemIDs []uuid.UUID) (
	map[uuid.UUID][]CommittedModifierResponse, error,
) {
	out := make(map[uuid.UUID][]CommittedModifierResponse, len(itemIDs))
	if len(itemIDs) == 0 {
		return out, nil
	}
	rows, err := q.ListCommittedItemModifiers(ctx, itemIDs)
	if err != nil {
		return nil, fmt.Errorf("load committed item modifiers: %w", err)
	}
	for _, row := range rows {
		out[row.CommittedItemID] = append(out[row.CommittedItemID], CommittedModifierResponse{
			GroupID:      row.ModifierGroupID,
			GroupName:    row.ModifierGroupName,
			OptionID:     row.ModifierOptionID,
			OptionName:   row.ModifierOptionName,
			SurchargeVND: row.SurchargeVnd,
		})
	}
	return out, nil
}
