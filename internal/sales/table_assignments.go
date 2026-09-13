package sales

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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

// assignTables inserts one assignment per Table in a single statement,
// numbering them from startSequence in the caller's selection order, and
// returns the audit details for each.
func assignTables(ctx context.Context, q *sqlc.Queries, actor Actor,
	sessionID uuid.UUID, tableIDs []uuid.UUID, startSequence int32,
) ([]tableAssignmentAudit, error) {
	sequences := make([]int32, len(tableIDs))
	for i := range tableIDs {
		sequences[i] = startSequence + int32(i)
	}
	rows, err := q.InsertTableAssignmentsBatch(ctx, sqlc.InsertTableAssignmentsBatchParams{
		Column1: tableIDs,
		Column2: sessionID,
		Column3: actor.StaffID,
		Column4: sequences,
	})
	if err != nil {
		return nil, fmt.Errorf("insert table assignments: %w", err)
	}
	out := make([]tableAssignmentAudit, len(rows))
	for i, row := range rows {
		out[i] = tableAssignmentAudit{
			TableAssignmentID: row.ID,
			TableID:           row.TableID,
			ServiceSessionID:  sessionID,
		}
	}
	return out, nil
}

// nextAssignmentSequence returns the next sequence for a Session, continuing
// past released assignments so a number is never reused.
func nextAssignmentSequence(ctx context.Context, q *sqlc.Queries, sessionID uuid.UUID) (int32, error) {
	highest, err := q.GetHighestAssignmentSequence(ctx, sessionID)
	if err != nil {
		return 0, fmt.Errorf("load highest assignment sequence: %w", err)
	}
	return highest + 1, nil
}

// writeAssignmentAudits inserts one audit event per Table assignment change,
// in a single statement. The executor's AuditRecord holds the single
// Session-level event; these are the per-Table events alongside it, inserted
// in the same transaction so an audit failure still rolls the whole mutation
// back.
func writeAssignmentAudits(ctx context.Context, q *sqlc.Queries, actor Actor,
	eventType string, entries []tableAssignmentAudit,
) error {
	if len(entries) == 0 {
		return nil
	}
	detailsBatch := make([]string, len(entries))
	for i, entry := range entries {
		details, err := json.Marshal(entry)
		if err != nil {
			return fmt.Errorf("marshal table assignment audit: %w", err)
		}
		detailsBatch[i] = string(details)
	}
	if err := q.InsertAuditEventsBatch(ctx, sqlc.InsertAuditEventsBatchParams{
		EventType:    eventType,
		ActorID:      actor.StaffID,
		SessionID:    actor.SessionID,
		OccurredAt:   time.Now(),
		DetailsBatch: detailsBatch,
	}); err != nil {
		return fmt.Errorf("insert %s audit events: %w", eventType, err)
	}
	return nil
}

type setSessionTablesFingerprint struct {
	ServiceSessionID uuid.UUID   `json:"service_session_id"`
	TableIDs         []uuid.UUID `json:"table_ids"`
}

// SetSessionTablesHandler replaces a Dine-in Session's Table set.
type SetSessionTablesHandler struct{ runner *Runner }

// NewSetSessionTablesHandler creates a new SetSessionTablesHandler.
func NewSetSessionTablesHandler(runner *Runner) *SetSessionTablesHandler {
	return &SetSessionTablesHandler{runner: runner}
}

// Handle computes the difference between the Session's current and desired
// Table sets, releases what is gone, and assigns what is new.
//
// Only additions are validated for availability. A Table that is already
// assigned and has since been marked unavailable must not block an unrelated
// change to the same Session.
func (h *SetSessionTablesHandler) Handle(ctx context.Context, actor Actor,
	cmd SetSessionTablesCommand,
) (int, ServiceSessionResponse, error) {
	if HasDuplicateUUIDs(cmd.TableIDs) {
		return 0, ServiceSessionResponse{}, ErrTableSelectionDuplicate
	}

	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpSetSessionTables,
		Fingerprint: setSessionTablesFingerprint{
			ServiceSessionID: cmd.ServiceSessionID,
			TableIDs:         SortedUUIDs(cmd.TableIDs),
		},
		Required: []string{CapSalesOperate},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, ServiceSessionResponse, AuditRecord, error) {
			var zero ServiceSessionResponse
			q := mc.Queries

			session, err := q.LockServiceSessionForUpdate(ctx, cmd.ServiceSessionID)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return 0, zero, AuditRecord{},
						fmt.Errorf("%w: %s", ErrServiceSessionNotFound, cmd.ServiceSessionID)
				}
				return 0, zero, AuditRecord{}, fmt.Errorf("lock service session: %w", err)
			}
			if session.ServiceMode != ModeDineIn {
				return 0, zero, AuditRecord{}, ErrTakeawayTablesNotAvailable
			}
			if session.State != StateActive {
				return 0, zero, AuditRecord{}, ErrServiceSessionClosed
			}

			// Spec 6.1: every 5A mutation requires a Sales Shift in state OPEN.
			// Like the draft-lock query's sh.id = s.sales_shift_id AND
			// sh.state = 'OPEN', this deliberately checks the Session's OWN
			// Shift, not whether some open Shift exists.
			shiftState, err := q.GetSalesShiftStateByID(ctx, session.SalesShiftID)
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("load sales shift state: %w", err)
			}
			if shiftState != "OPEN" {
				return 0, zero, AuditRecord{}, ErrOpenShiftRequired
			}

			current, err := q.LockCurrentTableAssignments(ctx, cmd.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("lock current assignments: %w", err)
			}

			desired := make(map[uuid.UUID]struct{}, len(cmd.TableIDs))
			for _, id := range cmd.TableIDs {
				desired[id] = struct{}{}
			}
			assignedNow := make(map[uuid.UUID]struct{}, len(current))
			var toRelease []sqlc.LockCurrentTableAssignmentsRow
			for _, row := range current {
				assignedNow[row.TableID] = struct{}{}
				if _, keep := desired[row.TableID]; !keep {
					toRelease = append(toRelease, row)
				}
			}
			var toAdd []uuid.UUID
			for _, id := range cmd.TableIDs {
				if _, already := assignedNow[id]; !already {
					toAdd = append(toAdd, id)
				}
			}

			if err := lockAndValidateTables(ctx, q, toAdd); err != nil {
				return 0, zero, AuditRecord{}, err
			}

			released := make([]tableAssignmentAudit, 0, len(toRelease))
			for _, row := range toRelease {
				if err := q.ReleaseTableAssignment(ctx, sqlc.ReleaseTableAssignmentParams{
					ID:                        row.ID,
					ReleasedByStaffIdentityID: uuid.NullUUID{UUID: actor.StaffID, Valid: true},
				}); err != nil {
					return 0, zero, AuditRecord{}, fmt.Errorf("release table assignment: %w", err)
				}
				released = append(released, tableAssignmentAudit{
					TableAssignmentID: row.ID,
					TableID:           row.TableID,
					ServiceSessionID:  cmd.ServiceSessionID,
				})
			}
			if err := writeAssignmentAudits(ctx, q, actor, EventTableAssignmentReleased, released); err != nil {
				return 0, zero, AuditRecord{}, err
			}

			if len(toAdd) > 0 {
				startSeq, err := nextAssignmentSequence(ctx, q, cmd.ServiceSessionID)
				if err != nil {
					return 0, zero, AuditRecord{}, err
				}
				added, err := assignTables(ctx, q, actor, cmd.ServiceSessionID, toAdd, startSeq)
				if err != nil {
					return 0, zero, AuditRecord{}, err
				}
				if err := writeAssignmentAudits(ctx, q, actor, EventTableAssignmentCreated, added); err != nil {
					return 0, zero, AuditRecord{}, err
				}
			}

			result, err := LoadServiceSession(ctx, q, cmd.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			// The per-Table events above are the whole audit trail for this
			// command; there is no meaningful Session-level event to add.
			return 200, result, AuditRecord{}, nil
		})
}
