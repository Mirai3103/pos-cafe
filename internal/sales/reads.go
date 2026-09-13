package sales

import (
	"context"

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
