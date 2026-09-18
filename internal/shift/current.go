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

// Handle returns the open Sales Shift with its Expected Cash, reconciliation
// scalars, Cash Movements, and Refunds, or nil when no Shift is open.
//
// "No Shift is currently open" is a normal operating state that the cashier
// screen renders directly, so it is a successful nil rather than a not-found
// error.
//
// The read runs in a read-only repeatable-read transaction, so the capability
// check, the Shift row, the movement list, the reconciliation aggregate, and
// the Refund list all observe one snapshot.
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

			totals, err := q.GetShiftReconciliationTotals(ctx, row.ID)
			if err != nil {
				return nil, fmt.Errorf("get shift reconciliation totals: %w", err)
			}

			expected, err := ComputeExpectedCash(row.OpeningFloatVnd, totals.CashPaymentVnd,
				totals.CashPaymentVoidVnd, totals.CashRefundVnd, payInVND, payOutVND)
			if err != nil {
				return nil, err
			}

			refundRows, err := q.ListShiftRefunds(ctx, row.ID)
			if err != nil {
				return nil, fmt.Errorf("list shift refunds: %w", err)
			}

			// Serialize an empty list as [] rather than null.
			refunds := make([]RefundSummaryResponse, 0, len(refundRows))
			for _, refund := range refundRows {
				refunds = append(refunds, refundSummaryFromRow(refund))
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
				ExpectedCashVND:                 expected,
				CashPaymentVND:                  totals.CashPaymentVnd,
				CashPaymentVoidVND:              totals.CashPaymentVoidVnd,
				CashRefundVND:                   totals.CashRefundVnd,
				ManualQRPaymentVND:              totals.ManualQrPaymentVnd,
				ManualQRPaymentVoidVND:          totals.ManualQrPaymentVoidVnd,
				ManualQRRefundVND:               totals.ManualQrRefundVnd,
				PendingManualQRRefundVND:        totals.PendingManualQrRefundVnd,
				PendingRefundVND:                totals.PendingRefundVnd,
				UnresolvedPostSaleAdjustmentVND: totals.UnresolvedPostSaleAdjustmentVnd,
				CashMovements:                   movements,
				Refunds:                         refunds,
			}, nil
		})
}

// refundSummaryFromRow projects one Refund row. State is derived from the
// query's completion evidence rather than copied from a stored column, so a
// Refund cannot report COMPLETED without a completion time.
func refundSummaryFromRow(row sqlc.ListShiftRefundsRow) RefundSummaryResponse {
	summary := RefundSummaryResponse{
		ID:        row.ID,
		CheckID:   row.CheckID,
		Method:    row.Method,
		AmountVND: row.AmountVnd,
		State:     RefundStatePending,
		CreatedAt: row.CreatedAt,
	}
	if row.CompletedSaleID.Valid {
		completedSaleID := row.CompletedSaleID.UUID
		summary.CompletedSaleID = &completedSaleID
	}
	// State and completion time come from the same completion row, so a Refund
	// can never report COMPLETED without its completion time.
	if row.Completed && row.CompletedAt.Valid {
		completedAt := row.CompletedAt.Time
		summary.State = RefundStateCompleted
		summary.CompletedAt = &completedAt
	}
	return summary
}
