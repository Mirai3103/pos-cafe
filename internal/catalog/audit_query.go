package catalog

import (
	"context"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

// AuditEventsHandler handles queries for catalog audit events.
type AuditEventsHandler struct {
	runner *Runner
}

// NewAuditEventsHandler creates a new AuditEventsHandler.
func NewAuditEventsHandler(runner *Runner) *AuditEventsHandler {
	return &AuditEventsHandler{runner: runner}
}

// Handle executes the audit events query.
func (h *AuditEventsHandler) Handle(ctx context.Context, actor Actor, limit int32) ([]AuditEventResponse, error) {
	if limit <= 0 {
		limit = 50
	}
	return ExecuteRead(ctx, h.runner, actor, CapAuditInspect, func(q *sqlc.Queries) ([]AuditEventResponse, error) {
		events, err := q.ListAuditEvents(ctx, sqlc.ListAuditEventsParams{
			EventType: "catalog.",
			Limit:     limit,
		})
		if err != nil {
			return nil, err
		}

		result := make([]AuditEventResponse, len(events))
		for i, ev := range events {
			var actorID, sessionID *uuid.UUID
			if ev.ActorID.Valid {
				id := ev.ActorID.UUID
				actorID = &id
			}
			if ev.SessionID.Valid {
				id := ev.SessionID.UUID
				sessionID = &id
			}
			result[i] = AuditEventResponse{
				ID:         ev.ID,
				EventType:  ev.EventType,
				ActorID:    actorID,
				SessionID:  sessionID,
				Details:    ev.Details,
				OccurredAt: ev.OccurredAt,
			}
		}
		return result, nil
	})
}
