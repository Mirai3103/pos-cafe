package preparation

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

// decodeModifiers decodes a Preparation Unit's frozen Modifier Option
// snapshots. An empty raw value is an empty list, not an error, and the
// returned slice is never nil, so the JSON contract serializes [] not null.
func decodeModifiers(raw json.RawMessage) ([]UnitModifierResponse, error) {
	modifiers := make([]UnitModifierResponse, 0)
	if len(raw) == 0 {
		return modifiers, nil
	}
	if err := json.Unmarshal(raw, &modifiers); err != nil {
		return nil, fmt.Errorf("decode preparation unit modifiers: %w", err)
	}
	if modifiers == nil {
		modifiers = make([]UnitModifierResponse, 0)
	}
	return modifiers, nil
}

// ActiveQueueHandler reads the bar's whole active queue.
type ActiveQueueHandler struct{ runner *Runner }

// NewActiveQueueHandler creates an ActiveQueueHandler.
func NewActiveQueueHandler(runner *Runner) *ActiveQueueHandler {
	return &ActiveQueueHandler{runner: runner}
}

// Handle projects every Preparation Unit in the active states QUEUED,
// IN_PREPARATION, and READY, ordered by queued_at then id, with each round's
// current unreleased Table assignments read at the same instant.
//
// One read-only REPEATABLE READ transaction carries the authority check, the
// units, and the tables, so the projection is internally consistent — the
// tables a unit shows were its session's tables at the same snapshot the
// units were read at — and reading mutates nothing.
func (h *ActiveQueueHandler) Handle(ctx context.Context, actor Actor) (QueueResponse, error) {
	return ExecuteRead(ctx, h.runner, actor, OpReadActiveQueue, CapPreparationOperate,
		func(q *sqlc.Queries) (QueueResponse, error) {
			observedAt, err := q.GetPreparationCurrentTime(ctx)
			if err != nil {
				return QueueResponse{}, fmt.Errorf("read preparation observed time: %w", err)
			}
			rows, err := q.ListActivePreparationUnits(ctx)
			if err != nil {
				return QueueResponse{}, fmt.Errorf("list active preparation units: %w", err)
			}

			// The tables query is one set-based round trip for every session
			// on the queue, deduplicated first so N units of one round do not
			// widen the lookup.
			sessionIDs := make([]uuid.UUID, 0, len(rows))
			seenSessions := make(map[uuid.UUID]struct{}, len(rows))
			for _, row := range rows {
				// Phase 5D rows used the application clock. Keep their client-side
				// ages non-negative while new writes use the database clock.
				if row.QueuedAt.After(observedAt) {
					observedAt = row.QueuedAt
				}
				if row.InPreparationAt.Valid && row.InPreparationAt.Time.After(observedAt) {
					observedAt = row.InPreparationAt.Time
				}
				if _, ok := seenSessions[row.ServiceSessionID]; !ok {
					seenSessions[row.ServiceSessionID] = struct{}{}
					sessionIDs = append(sessionIDs, row.ServiceSessionID)
				}
			}
			tablesBySession := make(map[uuid.UUID][]string, len(sessionIDs))
			if len(sessionIDs) > 0 {
				tableRows, err := q.ListCurrentPreparationTables(ctx, sessionIDs)
				if err != nil {
					return QueueResponse{}, fmt.Errorf("list current preparation tables: %w", err)
				}
				for _, row := range tableRows {
					tablesBySession[row.ServiceSessionID] = append(
						tablesBySession[row.ServiceSessionID], row.Name,
					)
				}
			}

			units := make([]QueueUnitResponse, 0, len(rows))
			for _, row := range rows {
				unit, err := queueUnitFromRow(row, tablesBySession[row.ServiceSessionID])
				if err != nil {
					return QueueResponse{}, err
				}
				units = append(units, unit)
			}
			return QueueResponse{ObservedAt: observedAt, Units: units}, nil
		})
}

// queueUnitFromRow maps one query row onto the queue DTO with explicit
// nullable handling — no reflection, no JSON round-tripping — so every absent
// optional becomes the contract's shape: an empty TableNames and Modifiers,
// and nil SizeName, PreparationNote, and InPreparationAt.
func queueUnitFromRow(row sqlc.ListActivePreparationUnitsRow,
	tableNames []string,
) (QueueUnitResponse, error) {
	modifiers, err := decodeModifiers(row.Modifiers)
	if err != nil {
		return QueueUnitResponse{}, err
	}
	tables := append([]string{}, tableNames...)
	var sizeName, preparationNote *string
	if row.SizeName.Valid {
		value := row.SizeName.String
		sizeName = &value
	}
	if row.PreparationNote.Valid {
		value := row.PreparationNote.String
		preparationNote = &value
	}
	var inPreparationAt *time.Time
	if row.InPreparationAt.Valid {
		value := row.InPreparationAt.Time
		inPreparationAt = &value
	}
	var remakeOf *uuid.UUID
	if row.RemakeOfPreparationUnitID.Valid {
		value := row.RemakeOfPreparationUnitID.UUID
		remakeOf = &value
	}
	return QueueUnitResponse{
		ID:                 row.ID,
		OrderItemID:        row.OrderItemID,
		OrderItemUnitCount: row.OrderItemUnitCount,
		UnitNumber:         row.UnitNumber,
		State:              row.State,
		ServiceNumber:      row.ServiceNumber,
		TableNames:         tables,
		CategoryName:       row.CategoryName,
		ItemName:           row.ItemName,
		SizeName:           sizeName,
		Modifiers:          modifiers,
		PreparationNote:    preparationNote,
		QueuedAt:           row.QueuedAt,
		InPreparationAt:    inPreparationAt,
		Priority:           row.Priority,
		// A Remake links to the wasted source unit it replaces; originals
		// carry a nil link.
		RemakeOfPreparationUnitID: remakeOf,
	}, nil
}
