package tables

import (
	"context"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

// OverviewHandler serves the Table overview read.
type OverviewHandler struct{ runner *Runner }

// NewOverviewHandler creates a new OverviewHandler.
func NewOverviewHandler(runner *Runner) *OverviewHandler {
	return &OverviewHandler{runner: runner}
}

// Handle returns every Table with the active Service Sessions occupying it.
//
// Occupancy is read from the Sales-owned service_sessions and
// table_assignments tables through this slice's own query. The Tables slice
// never imports internal/sales.
func (h *OverviewHandler) Handle(ctx context.Context, actor Actor) ([]TableOverviewRow, error) {
	return ExecuteRead(ctx, h.runner, actor, CapSalesOperate,
		func(q *sqlc.Queries) ([]TableOverviewRow, error) {
			tableRows, err := q.ListTables(ctx)
			if err != nil {
				return nil, fmt.Errorf("list tables: %w", err)
			}

			occupantRows, err := q.ListCurrentTableOccupants(ctx)
			if err != nil {
				return nil, fmt.Errorf("list current table occupants: %w", err)
			}

			// The query already orders by (assigned_at, id), so appending in
			// scan order preserves assignment order per Table.
			byTable := make(map[uuid.UUID][]TableOccupant, len(tableRows))
			for _, occ := range occupantRows {
				byTable[occ.TableID] = append(byTable[occ.TableID], TableOccupant{
					ServiceSessionID: occ.ServiceSessionID,
					ServiceNumber:    occ.ServiceNumber,
				})
			}

			out := make([]TableOverviewRow, 0, len(tableRows))
			for _, row := range tableRows {
				occupants := byTable[row.ID]
				if occupants == nil {
					// Serialize as [] rather than null.
					occupants = []TableOccupant{}
				}
				out = append(out, TableOverviewRow{
					TableResponse:          toTableResponse(row),
					CurrentServiceSessions: occupants,
				})
			}
			return out, nil
		})
}
