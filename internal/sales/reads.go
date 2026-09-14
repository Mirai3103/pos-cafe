package sales

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

// GetServiceSessionHandler serves the single Service Session read.
type GetServiceSessionHandler struct{ runner *Runner }

// NewGetServiceSessionHandler creates a new GetServiceSessionHandler.
func NewGetServiceSessionHandler(runner *Runner) *GetServiceSessionHandler {
	return &GetServiceSessionHandler{runner: runner}
}

// Handle returns one Service Session with its Tables and Order Draft.
//
// The read carries no open-Shift requirement: staff must be able to inspect a
// Session after its Shift closes.
func (h *GetServiceSessionHandler) Handle(ctx context.Context, actor Actor, sessionID uuid.UUID) (
	ServiceSessionResponse, error,
) {
	return ExecuteRead(ctx, h.runner, actor, OpGetServiceSession, CapSalesOperate,
		func(q *sqlc.Queries) (ServiceSessionResponse, error) {
			return LoadServiceSession(ctx, q, sessionID)
		})
}

// ListActiveSessionsHandler serves the cashier's open-Sessions list.
type ListActiveSessionsHandler struct{ runner *Runner }

// NewListActiveSessionsHandler creates a new ListActiveSessionsHandler.
func NewListActiveSessionsHandler(runner *Runner) *ListActiveSessionsHandler {
	return &ListActiveSessionsHandler{runner: runner}
}

// Handle returns every ACTIVE Service Session with its full projection.
//
// Each Session carries its draft, so the open-tabs screen needs one request
// rather than one per Session. A single cafe has a handful of open Sessions at
// a time, so the per-Session queries are bounded in practice; if that ever
// stops being true, the fix is a batched projection query, not pagination that
// would hide open tabs from staff.
func (h *ListActiveSessionsHandler) Handle(ctx context.Context, actor Actor) (
	[]ServiceSessionResponse, error,
) {
	return ExecuteRead(ctx, h.runner, actor, OpListServiceSessions, CapSalesOperate,
		func(q *sqlc.Queries) ([]ServiceSessionResponse, error) {
			rows, err := q.ListActiveServiceSessions(ctx)
			if err != nil {
				return nil, fmt.Errorf("list active service sessions: %w", err)
			}
			out := make([]ServiceSessionResponse, 0, len(rows))
			for _, row := range rows {
				session, err := LoadServiceSession(ctx, q, row.ID)
				if err != nil {
					// A Check invariant violation is a defect confined to that
					// one Session's data; the rest of the open-tabs list must
					// still reach the cashier rather than 500 in its entirety.
					if errors.Is(err, ErrChargeInvariantViolated) {
						slog.Error("excluding service session from open-tabs list",
							"session_id", row.ID, "error", err)
						continue
					}
					return nil, err
				}
				out = append(out, session)
			}
			return out, nil
		})
}
