package sales

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

// newServiceSessionResponse returns a response with every collection
// initialized, so the JSON contract never emits null for an array.
func newServiceSessionResponse() ServiceSessionResponse {
	return ServiceSessionResponse{
		Tables:           make([]SessionTableResponse, 0),
		Checks:           make([]struct{}, 0),
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
	out.Draft = &OrderDraftResponse{ID: draft.ID, State: draft.State, Items: items}

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
