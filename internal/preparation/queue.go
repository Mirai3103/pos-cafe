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

// correctionEntryKind values: the discriminator the generated UNION ALL
// history query reports on every row (spec §6.4).
const (
	correctionEntryWaste  = "WASTE"
	correctionEntryRemake = "REMAKE"
)

// Handle projects the bar's queue in one read: every Preparation Unit in the
// active states QUEUED, IN_PREPARATION, and READY — plus a CANCELLED or
// WASTED unit while it still has an unacknowledged alert — ordered by
// priority lane then queued_at then id (spec §6.2), the active alerts oldest
// first with each unit's identity projected at read time (spec §6.3), and the
// Waste and Remake history of active Sessions, newest first, capped at 50
// (spec §6.4), with each round's current unreleased Table assignments read at
// the same instant.
//
// One read-only REPEATABLE READ transaction carries the authority check, the
// units, the tables, the alerts, and the history, so the projection is
// internally consistent — every collection describes the same database
// snapshot even when a Waste, Remake, or acknowledgment commits while the
// read runs — and reading mutates nothing.
func (h *ActiveQueueHandler) Handle(ctx context.Context, actor Actor) (QueueResponse, error) {
	return ExecuteRead(ctx, h.runner, actor, OpReadActiveQueue, CapPreparationOperate,
		func(q *sqlc.Queries) (QueueResponse, error) {
			// Every collection is allocated before any query, so the success
			// response never carries a nil slice — the JSON contract
			// serializes an empty collection as [] not null.
			units := make([]QueueUnitResponse, 0)
			alerts := make([]QueueAlertResponse, 0)
			corrections := make([]QueueCorrectionResponse, 0)

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

			for _, row := range rows {
				unit, err := queueUnitFromRow(row, tablesBySession[row.ServiceSessionID])
				if err != nil {
					return QueueResponse{}, err
				}
				units = append(units, unit)
			}

			// The alerts and the history are two more set-based queries; the
			// mapper preserves their SQL order and refuses malformed rows.
			alertRows, err := q.ListActivePreparationAlerts(ctx)
			if err != nil {
				return QueueResponse{}, fmt.Errorf("list active preparation alerts: %w", err)
			}
			for _, row := range alertRows {
				alert, err := queueAlertFromRow(row)
				if err != nil {
					return QueueResponse{}, err
				}
				alerts = append(alerts, alert)
			}

			correctionRows, err := q.ListRecentPreparationCorrections(ctx)
			if err != nil {
				return QueueResponse{}, fmt.Errorf("list recent preparation corrections: %w", err)
			}
			for _, row := range correctionRows {
				correction, err := queueCorrectionFromRow(row)
				if err != nil {
					return QueueResponse{}, err
				}
				corrections = append(corrections, correction)
			}

			return QueueResponse{
				ObservedAt:  observedAt,
				Units:       units,
				Alerts:      alerts,
				Corrections: corrections,
			}, nil
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

// queueAlertFromRow maps one active alert row onto the queue DTO with explicit
// nullable handling (spec §6.3): the unit identity columns are projected
// values, and WasteID resolves through the Waste fact for WASTE alerts only —
// the reserved Cancellation kinds keep it null. A WASTE alert is written in
// the same transaction as its Waste fact, so a WASTE row that fails to resolve
// one is a malformed projection: an internal error, never a partial contract
// row.
func queueAlertFromRow(row sqlc.ListActivePreparationAlertsRow) (QueueAlertResponse, error) {
	var note *string
	if row.Note.Valid {
		value := row.Note.String
		note = &value
	}
	var wasteID *uuid.UUID
	if row.WasteID.Valid {
		value := row.WasteID.UUID
		wasteID = &value
	} else if row.Kind == AlertKindWaste {
		return QueueAlertResponse{}, fmt.Errorf(
			"active %s alert %s does not resolve its waste fact", row.Kind, row.ID)
	}
	// The queue query reads only unacknowledged alerts, so both fields are
	// null in practice; they are mapped from the row so the projection stays
	// honest if that filter ever widens.
	var acknowledgedBy *uuid.UUID
	if row.AcknowledgedByStaffIdentityID.Valid {
		value := row.AcknowledgedByStaffIdentityID.UUID
		acknowledgedBy = &value
	}
	var acknowledgedAt *time.Time
	if row.AcknowledgedAt.Valid {
		value := row.AcknowledgedAt.Time
		acknowledgedAt = &value
	}
	return QueueAlertResponse{
		ID:                            row.ID,
		Kind:                          row.Kind,
		PreparationUnitID:             row.PreparationUnitID,
		ServiceNumber:                 row.ServiceNumber,
		ItemName:                      row.ItemName,
		UnitNumber:                    row.UnitNumber,
		Reason:                        row.Reason,
		Note:                          note,
		WasteID:                       wasteID,
		CreatedAt:                     row.CreatedAt,
		AcknowledgedByStaffIdentityID: acknowledgedBy,
		AcknowledgedAt:                acknowledgedAt,
	}, nil
}

// queueCorrectionFromRow maps one Waste/Remake history row onto the queue DTO
// (spec §6.4). entry_kind discriminates two row shapes, so the mapper is
// discriminator-specific: a WASTE row must carry no remake linkage and a
// REMAKE row must carry its full Waste linkage (Waste id, source unit, source
// unit number). An unknown discriminator or a malformed nullable combination
// is an internal error, never a partial contract row.
func queueCorrectionFromRow(row sqlc.ListRecentPreparationCorrectionsRow) (QueueCorrectionResponse, error) {
	var note *string
	if row.Note.Valid {
		value := row.Note.String
		note = &value
	}

	switch row.EntryKind {
	case correctionEntryWaste:
		if row.WasteID.Valid || row.SourcePreparationUnitID.Valid || row.SourceUnitNumber.Valid {
			return QueueCorrectionResponse{}, fmt.Errorf(
				"waste history row %s carries remake-only fields", row.FactID)
		}
		return QueueCorrectionResponse{
			EntryKind:         row.EntryKind,
			ID:                row.FactID,
			PreparationUnitID: row.PreparationUnitID,
			ServiceNumber:     row.ServiceNumber,
			ItemName:          row.ItemName,
			UnitNumber:        row.UnitNumber,
			Reason:            row.Reason,
			Note:              note,
			OccurredAt:        row.OccurredAt,
		}, nil
	case correctionEntryRemake:
		if !row.WasteID.Valid || !row.SourcePreparationUnitID.Valid || !row.SourceUnitNumber.Valid {
			return QueueCorrectionResponse{}, fmt.Errorf(
				"remake history row %s is missing its waste linkage", row.FactID)
		}
		wasteID := row.WasteID.UUID
		sourceUnitID := row.SourcePreparationUnitID.UUID
		sourceUnitNumber := row.SourceUnitNumber.Int32
		return QueueCorrectionResponse{
			EntryKind:               row.EntryKind,
			ID:                      row.FactID,
			PreparationUnitID:       row.PreparationUnitID,
			WasteID:                 &wasteID,
			SourcePreparationUnitID: &sourceUnitID,
			SourceUnitNumber:        &sourceUnitNumber,
			ServiceNumber:           row.ServiceNumber,
			ItemName:                row.ItemName,
			UnitNumber:              row.UnitNumber,
			Reason:                  row.Reason,
			Note:                    note,
			OccurredAt:              row.OccurredAt,
		}, nil
	default:
		return QueueCorrectionResponse{}, fmt.Errorf(
			"unknown preparation correction entry kind %q", row.EntryKind)
	}
}
