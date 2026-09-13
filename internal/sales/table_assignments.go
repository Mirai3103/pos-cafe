package sales

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

// tableAssignmentAudit is one TABLE_ASSIGNMENT_CREATED or
// TABLE_ASSIGNMENT_RELEASED event's details. The names and shape match the
// canonical source, because internal/tables already reads these rows and its
// overview behavior must not shift.
type tableAssignmentAudit struct {
	TableAssignmentID uuid.UUID `json:"table_assignment_id"`
	TableID           uuid.UUID `json:"table_id"`
	ServiceSessionID  uuid.UUID `json:"service_session_id"`
}

// lockAndValidateTables locks every selected Table and rejects one that does
// not exist or is unavailable.
//
// A Table already occupied by another active Service Session is deliberately
// NOT rejected: CONTEXT.md allows a Table to carry more than one active
// Session, and internal/tables already models occupancy as a list.
//
// The lock is taken in sorted id order so two concurrent assignments over
// overlapping Table sets cannot deadlock.
func lockAndValidateTables(ctx context.Context, q *sqlc.Queries, tableIDs []uuid.UUID) error {
	if len(tableIDs) == 0 {
		return nil
	}
	rows, err := q.LockTablesForAssignment(ctx, SortedUUIDs(tableIDs))
	if err != nil {
		return fmt.Errorf("lock tables: %w", err)
	}

	byID := make(map[uuid.UUID]sqlc.LockTablesForAssignmentRow, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}
	// Iterate the caller's order so the error names the first Table the staff
	// member selected, not the lowest id.
	for _, id := range tableIDs {
		row, ok := byID[id]
		if !ok {
			return fmt.Errorf("%w: %s", ErrTableNotFound, id)
		}
		if !row.Available {
			return fmt.Errorf("%w: %s", ErrTableUnavailable, row.Name)
		}
	}
	return nil
}

// assignTables inserts one assignment per Table, numbering them from
// startSequence in the caller's selection order, and returns the audit details
// for each.
func assignTables(ctx context.Context, q *sqlc.Queries, actor Actor,
	sessionID uuid.UUID, tableIDs []uuid.UUID, startSequence int32,
) ([]tableAssignmentAudit, error) {
	out := make([]tableAssignmentAudit, 0, len(tableIDs))
	seq := startSequence
	for _, tableID := range tableIDs {
		row, err := q.InsertTableAssignment(ctx, sqlc.InsertTableAssignmentParams{
			TableID:                   tableID,
			ServiceSessionID:          sessionID,
			AssignedByStaffIdentityID: actor.StaffID,
			Sequence:                  seq,
		})
		if err != nil {
			return nil, fmt.Errorf("insert table assignment: %w", err)
		}
		out = append(out, tableAssignmentAudit{
			TableAssignmentID: row.ID,
			TableID:           tableID,
			ServiceSessionID:  sessionID,
		})
		seq++
	}
	return out, nil
}

// nextAssignmentSequence returns the next sequence for a Session, continuing
// past released assignments so a number is never reused.
//
//nolint:unused // brief-mandated helper; first consumed by the Set-session-Tables task
func nextAssignmentSequence(ctx context.Context, q *sqlc.Queries, sessionID uuid.UUID) (int32, error) {
	highest, err := q.GetHighestAssignmentSequence(ctx, sessionID)
	if err != nil {
		return 0, fmt.Errorf("load highest assignment sequence: %w", err)
	}
	return highest + 1, nil
}

// writeAssignmentAudits inserts one audit event per Table assignment change.
// The executor's AuditRecord holds the single Session-level event; these are
// the per-Table events alongside it, inserted in the same transaction so an
// audit failure still rolls the whole mutation back.
func writeAssignmentAudits(ctx context.Context, q *sqlc.Queries, actor Actor,
	eventType string, entries []tableAssignmentAudit,
) error {
	for _, entry := range entries {
		details, err := json.Marshal(entry)
		if err != nil {
			return fmt.Errorf("marshal table assignment audit: %w", err)
		}
		if _, err := q.InsertAuditEvent(ctx, sqlc.InsertAuditEventParams{
			EventType:  eventType,
			ActorID:    uuid.NullUUID{UUID: actor.StaffID, Valid: true},
			SessionID:  uuid.NullUUID{UUID: actor.SessionID, Valid: true},
			Details:    details,
			OccurredAt: time.Now(),
		}); err != nil {
			return fmt.Errorf("insert %s audit event: %w", eventType, err)
		}
	}
	return nil
}
