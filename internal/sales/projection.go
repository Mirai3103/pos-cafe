package sales

import (
	"context"
	"database/sql"
	"encoding/json"
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
		Orders:           make([]OrderResponse, 0),
		PreparationUnits: make([]PreparationUnitResponse, 0),
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

	orders, err := loadOrders(ctx, q, sessionID)
	if err != nil {
		return out, err
	}
	out.Orders = orders

	units, err := loadPreparationUnits(ctx, q, sessionID)
	if err != nil {
		return out, err
	}
	out.PreparationUnits = units

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

		payments, totalAppliedVND, err := loadCheckPayments(ctx, q, row.ID)
		if err != nil {
			return nil, err
		}
		balanceVND, err := SubtractCharge(row.ChargeVnd, totalAppliedVND)
		if err != nil {
			return nil, fmt.Errorf("check %s balance: %w", row.ID, err)
		}

		// The read-path half of the settlement guard. The database constraint
		// guarantees that a SETTLED Check carries complete evidence; this
		// guarantees that its state matches the money. A MERGED Check is
		// exempt: its charge and allocations moved to the survivor, and
		// check_settlement_evidence_valid already requires it to point at one.
		if row.State != CheckStateMerged &&
			(row.State == CheckStateSettled) != SettlesCheck(balanceVND) {
			slog.Error("check state does not match its balance",
				"check_id", row.ID, "state", row.State, "balance_vnd", balanceVND)
			return nil, fmt.Errorf("%w: check %s", ErrSettlementInvariantViolated, row.ID)
		}

		check := CheckResponse{
			ID:              row.ID,
			State:           row.State,
			ChargeVND:       row.ChargeVnd,
			TotalAppliedVND: totalAppliedVND,
			BalanceVND:      balanceVND,
			CreatedAt:       row.CreatedAt,
			Payments:        payments,
			Allocations:     allocations,
		}
		if row.MergedIntoCheckID.Valid {
			into := row.MergedIntoCheckID.UUID
			check.MergedIntoCheckID = &into
		}
		out = append(out, check)
	}
	return out, nil
}

// loadCheckPayments returns one Check's Payments and their summed applied
// amount, in (received_at, id) order.
func loadCheckPayments(ctx context.Context, q *sqlc.Queries, checkID uuid.UUID) (
	[]PaymentResponse, int64, error,
) {
	rows, err := q.ListCheckPayments(ctx, checkID)
	if err != nil {
		return nil, 0, fmt.Errorf("load check payments: %w", err)
	}

	out := make([]PaymentResponse, 0, len(rows))
	var totalAppliedVND int64
	for _, row := range rows {
		totalAppliedVND, err = AddCharge(totalAppliedVND, row.AppliedAmountVnd)
		if err != nil {
			return nil, 0, fmt.Errorf("sum payments of check %s: %w", checkID, err)
		}
		payment := PaymentResponse{
			ID:               row.ID,
			Method:           row.Method,
			AppliedAmountVND: row.AppliedAmountVnd,
			SalesShiftID:     row.SalesShiftID,
			ReceivedAt:       row.ReceivedAt,
		}
		if row.CashTenderedVnd.Valid {
			v := row.CashTenderedVnd.Int64
			payment.CashTenderedVND = &v
		}
		if row.ChangeDueVnd.Valid {
			v := row.ChangeDueVnd.Int64
			payment.ChangeDueVND = &v
		}
		if row.TransactionReference.Valid {
			v := row.TransactionReference.String
			payment.TransactionReference = &v
		}
		out = append(out, payment)
	}
	return out, totalAppliedVND, nil
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
	submitted, err := loadSubmittedItems(ctx, q, itemIDs)
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
		_, isSubmitted := submitted[row.CommittedItemID]
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
			Submitted:         isSubmitted,
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

// loadOrders assembles the Session's Orders with their items.
func loadOrders(ctx context.Context, q *sqlc.Queries, sessionID uuid.UUID) (
	[]OrderResponse, error,
) {
	rows, err := q.ListSessionOrders(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("load session orders: %w", err)
	}
	out := make([]OrderResponse, 0, len(rows))
	if len(rows) == 0 {
		return out, nil
	}

	orderIDs := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		orderIDs = append(orderIDs, row.ID)
	}
	itemRows, err := q.ListOrderItems(ctx, orderIDs)
	if err != nil {
		return nil, fmt.Errorf("load order items: %w", err)
	}
	itemsByOrder := make(map[uuid.UUID][]OrderItemResponse, len(rows))
	for _, item := range itemRows {
		itemsByOrder[item.OrderID] = append(itemsByOrder[item.OrderID], OrderItemResponse{
			ID:              item.ID,
			CommittedItemID: item.CommittedItemID,
		})
	}

	for _, row := range rows {
		items := itemsByOrder[row.ID]
		if items == nil {
			items = make([]OrderItemResponse, 0)
		}
		out = append(out, OrderResponse{
			ID:                 row.ID,
			OrderDraftID:       row.OrderDraftID,
			SubmittedByStaffID: row.SubmittedByStaffIdentityID,
			SubmittedSessionID: row.SubmittedStaffAccessSessionID,
			SubmittedAt:        row.SubmittedAt,
			Items:              items,
		})
	}
	return out, nil
}

// loadPreparationUnits assembles the Session's Preparation Units.
//
// internal/sales reads unit state here and creates units at Submit; every
// state transition belongs to internal/preparation (ADR-024).
func loadPreparationUnits(ctx context.Context, q *sqlc.Queries, sessionID uuid.UUID) (
	[]PreparationUnitResponse, error,
) {
	rows, err := q.ListSessionPreparationUnits(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("load preparation units: %w", err)
	}
	out := make([]PreparationUnitResponse, 0, len(rows))
	for _, row := range rows {
		mods := make([]UnitModifierResponse, 0)
		if len(row.Modifiers) > 0 {
			if err := json.Unmarshal(row.Modifiers, &mods); err != nil {
				return nil, fmt.Errorf("decode preparation unit modifiers: %w", err)
			}
		}
		out = append(out, PreparationUnitResponse{
			ID:              row.ID,
			OrderItemID:     row.OrderItemID,
			UnitNumber:      row.UnitNumber,
			State:           row.State,
			ServiceNumber:   row.ServiceNumber,
			CategoryName:    row.CategoryName,
			ItemName:        row.ItemName,
			SizeName:        nullStringPtr(row.SizeName),
			Modifiers:       mods,
			PreparationNote: nullStringPtr(row.PreparationNote),
			QueuedAt:        row.QueuedAt,
		})
	}
	return out, nil
}

// loadSubmittedItems returns the set of Committed Items that have entered an
// Order, which is what a Charge Allocation's `submitted` flag reports.
func loadSubmittedItems(ctx context.Context, q *sqlc.Queries, itemIDs []uuid.UUID) (
	map[uuid.UUID]struct{}, error,
) {
	out := make(map[uuid.UUID]struct{})
	if len(itemIDs) == 0 {
		return out, nil
	}
	rows, err := q.ListSubmittedCommittedItems(ctx, itemIDs)
	if err != nil {
		return nil, fmt.Errorf("load submitted committed items: %w", err)
	}
	for _, id := range rows {
		out[id] = struct{}{}
	}
	return out, nil
}
