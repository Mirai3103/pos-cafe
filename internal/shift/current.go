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

// Handle returns the active Sales Shift's state-dispatched read (spec 9.5):
// the redacted metadata while OPEN, the frozen reconciliation while CLOSING,
// and nil when no Shift is active.
//
// "No Shift is currently active" is a normal operating state that the cashier
// screen renders directly, so it is a successful nil rather than a not-found
// error. While the Shift is OPEN the read discloses no money: only id, state,
// opened_at, and opener cross the boundary, because no aggregate may be
// revealed before the blind initial count commits (spec 4.1). While the Shift
// is CLOSING the read reports the frozen reconciliation with every attempt
// ordered by sequence (spec 4.3) — it never recalculates expected values from
// unrestricted current data.
//
// The read runs in a read-only repeatable-read transaction, so the capability
// check, the Shift row, and the frozen snapshot observe one snapshot.
func (h *CurrentShiftHandler) Handle(ctx context.Context, actor Actor) (*CurrentShiftResponse, error) {
	return ExecuteRead(ctx, h.runner, actor, OpGetCurrentShift, CapSalesShiftOperate,
		func(q *sqlc.Queries) (*CurrentShiftResponse, error) {
			row, err := q.GetActiveSalesShift(ctx)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return nil, nil
				}
				return nil, fmt.Errorf("load active sales shift: %w", err)
			}

			switch row.State {
			case StateOpen:
				return projectOpenCurrentShift(row), nil
			case StateClosing:
				return projectClosingCurrentShift(ctx, q, row)
			default:
				// The active-Shift unique index admits only OPEN and CLOSING
				// rows, so any other state is corrupt data, not a client-facing
				// condition; it surfaces as a logged 500.
				return nil, fmt.Errorf("unknown active sales shift state %q", row.State)
			}
		})
}

// projectOpenCurrentShift builds the OPEN read: the Shift metadata and nothing
// else (spec 4.1, 9.6). Its field allowlist (id, state, opened_at, opener) is
// part of the blind-count boundary, so no Opening Float, Expected Cash, Cash
// Movement, Refund, or reconciliation field is projected.
func projectOpenCurrentShift(row sqlc.GetActiveSalesShiftRow) *CurrentShiftResponse {
	return newOpenCurrentShiftResponse(OpenCurrentShiftResponse{
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
	})
}

// projectClosingCurrentShift builds the CLOSING read: the Shift metadata plus
// the frozen reconciliation loaded through the shared snapshot loader (spec
// 4.3, 9.6). Only shift_reconciliations, the append-only attempt ledgers, and
// the preview built from the latest evidence are read — never
// GetShiftReconciliationTotals or any other live-totals query — so live data
// written after the snapshot committed cannot change this projection.
func projectClosingCurrentShift(ctx context.Context, q *sqlc.Queries,
	row sqlc.GetActiveSalesShiftRow,
) (*CurrentShiftResponse, error) {
	recon, err := loadReconciliationSnapshot(ctx, q, row.ID)
	if err != nil {
		return nil, err
	}
	return newClosingCurrentShiftResponse(ClosingShiftResponse{
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
		Reconciliation: recon,
	}), nil
}
