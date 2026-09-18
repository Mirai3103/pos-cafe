package shift

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
)

// CurrentShiftHandler serves the current Sales Shift read.
type CurrentShiftHandler struct{ runner *Runner }

// NewCurrentShiftHandler creates a new CurrentShiftHandler.
func NewCurrentShiftHandler(runner *Runner) *CurrentShiftHandler {
	return &CurrentShiftHandler{runner: runner}
}

// Handle returns the open Sales Shift's redacted metadata, or nil when no
// Shift is open.
//
// "No Shift is currently open" is a normal operating state that the cashier
// screen renders directly, so it is a successful nil rather than a not-found
// error. While the Shift is OPEN the read discloses no money: only id, state,
// opened_at, and opener cross the boundary, because no aggregate may be
// revealed before the blind initial count commits. The CLOSING read reports
// the frozen reconciliation instead.
//
// The read runs in a read-only repeatable-read transaction, so the capability
// check and the Shift row observe one snapshot.
func (h *CurrentShiftHandler) Handle(ctx context.Context, actor Actor) (*OpenCurrentShiftResponse, error) {
	return ExecuteRead(ctx, h.runner, actor, OpGetCurrentShift, CapSalesShiftOperate,
		func(q *sqlc.Queries) (*OpenCurrentShiftResponse, error) {
			row, err := q.GetOpenSalesShift(ctx)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return nil, nil
				}
				return nil, fmt.Errorf("load open sales shift: %w", err)
			}

			return &OpenCurrentShiftResponse{
				SalesShiftMetadata: SalesShiftMetadata{
					ID:       row.ID,
					State:    row.State,
					OpenedAt: row.OpenedAt,
					Opener: StaffSummary{
						ID:          row.OpenerID,
						DisplayName: row.OpenerDisplayName,
						LoginCode:   row.OpenerLoginCode,
					},
				},
			}, nil
		})
}
