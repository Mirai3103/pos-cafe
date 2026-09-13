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

// Handle returns the open Sales Shift with its Expected Cash and Cash
// Movements, or nil when no Shift is open.
//
// "No Shift is currently open" is a normal operating state that the cashier
// screen renders directly, so it is a successful nil rather than a not-found
// error.
//
// The read runs in a read-only repeatable-read transaction, so the capability
// check, the Shift row, the movement list, and the Expected Cash aggregate all
// observe one snapshot.
func (h *CurrentShiftHandler) Handle(ctx context.Context, actor Actor) (*CurrentSalesShiftResponse, error) {
	return ExecuteRead(ctx, h.runner, actor, OpGetCurrentShift, CapSalesShiftOperate,
		func(q *sqlc.Queries) (*CurrentSalesShiftResponse, error) {
			row, err := q.GetOpenSalesShift(ctx)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return nil, nil
				}
				return nil, fmt.Errorf("load open sales shift: %w", err)
			}

			movementRows, err := q.ListCashMovements(ctx, row.ID)
			if err != nil {
				return nil, fmt.Errorf("list cash movements: %w", err)
			}

			// Serialize an empty list as [] rather than null.
			var payInVND, payOutVND int64
			movements := make([]CashMovementResponse, 0, len(movementRows))
			for _, m := range movementRows {
				switch m.Method {
				case MethodPayIn:
					payInVND += m.AmountVnd
				case MethodPayOut:
					payOutVND += m.AmountVnd
				}

				var note *string
				if m.Note.Valid {
					value := m.Note.String
					note = &value
				}
				movements = append(movements, CashMovementResponse{
					ID:           m.ID,
					SalesShiftID: m.SalesShiftID,
					Method:       m.Method,
					AmountVND:    m.AmountVnd,
					Reason:       m.Reason,
					Note:         note,
					Initiator: StaffSummary{
						ID:          m.InitiatorID,
						DisplayName: m.InitiatorDisplayName,
						LoginCode:   m.InitiatorLoginCode,
					},
					Approver: StaffSummary{
						ID:          m.ApproverID,
						DisplayName: m.ApproverDisplayName,
						LoginCode:   m.ApproverLoginCode,
					},
					OccurredAt: m.OccurredAt,
				})
			}

			expected, err := ComputeExpectedCash(row.OpeningFloatVnd, payInVND, payOutVND)
			if err != nil {
				return nil, err
			}

			return &CurrentSalesShiftResponse{
				SalesShiftResponse: SalesShiftResponse{
					ID:              row.ID,
					State:           row.State,
					OpeningFloatVND: row.OpeningFloatVnd,
					OpenedAt:        row.OpenedAt,
					Opener: StaffSummary{
						ID:          row.OpenerID,
						DisplayName: row.OpenerDisplayName,
						LoginCode:   row.OpenerLoginCode,
					},
				},
				ExpectedCashVND: expected,
				CashMovements:   movements,
			}, nil
		})
}
