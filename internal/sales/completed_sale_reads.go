package sales

import (
	"context"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

// LoadCompletedSale assembles the immutable Completed Sale for one closed
// Service Session: its Checks, Orders, Preparation Units, and the recorded
// Preparation history. Callers must run it inside the same transaction as
// their mutation, exactly as LoadServiceSession's callers do.
//
// The Checks reuse the same per-Check loaders the Service Session projection
// uses, so a Completed Sale serves the money evidence under the same
// invariants — charge and settlement are re-verified on read, never trusted.
// The history reads preparation_unit_transitions (ADR-027): the moves are
// business content the sale is made of, not a report reconstructed from audit
// payloads.
func LoadCompletedSale(ctx context.Context, q *sqlc.Queries, saleID uuid.UUID) (
	CompletedSaleResponse, error,
) {
	out := CompletedSaleResponse{
		Checks:             make([]CompletedSaleCheckResponse, 0),
		Orders:             make([]OrderResponse, 0),
		PreparationUnits:   make([]PreparationUnitResponse, 0),
		PreparationHistory: make([]PreparationTransitionResponse, 0),
	}

	sale, err := q.GetCompletedSale(ctx, saleID)
	if err != nil {
		// ErrNoRows is unreachable for both callers: FindCompletedSaleByServiceSession
		// and InsertCompletedSale's RETURNING only hand out ids this
		// transaction can see. There is no sale-not-found sentinel to map it
		// to, and inventing one for an unreachable branch would be worse than
		// letting it surface as the defect it is.
		return out, fmt.Errorf("load completed sale %s: %w", saleID, err)
	}

	out.ID = sale.ID
	out.State = CompletedSaleStateCompleted
	out.ServiceSessionID = sale.ServiceSessionID
	out.ServiceNumber = sale.ServiceNumber
	out.ServiceMode = sale.ServiceMode
	out.ServiceSessionState = sale.ServiceSessionState
	out.ServiceSessionOpenedAt = sale.ServiceSessionCreatedAt
	out.CompletedByStaffID = sale.CompletedByStaffIdentityID
	out.CompletedBySessionID = sale.CompletedStaffAccessSessionID
	out.CompletedByName = sale.CompletedByDisplayName
	out.CompletedAt = sale.CompletedAt

	checks, err := loadChecks(ctx, q, sale.ServiceSessionID)
	if err != nil {
		return out, err
	}
	out.Checks = make([]CompletedSaleCheckResponse, 0, len(checks))
	for _, check := range checks {
		out.Checks = append(out.Checks, CompletedSaleCheckResponse{
			ID:              check.ID,
			State:           check.State,
			ChargeVND:       check.ChargeVND,
			TotalAppliedVND: check.TotalAppliedVND,
			BalanceVND:      check.BalanceVND,
			Payments:        check.Payments,
			Allocations:     check.Allocations,
		})
	}

	orders, err := loadOrders(ctx, q, sale.ServiceSessionID)
	if err != nil {
		return out, err
	}
	out.Orders = orders

	units, err := loadPreparationUnits(ctx, q, sale.ServiceSessionID)
	if err != nil {
		return out, err
	}
	out.PreparationUnits = units

	transitions, err := q.ListSessionPreparationTransitions(ctx, sale.ServiceSessionID)
	if err != nil {
		return out, fmt.Errorf("load preparation transitions: %w", err)
	}
	for _, row := range transitions {
		out.PreparationHistory = append(out.PreparationHistory, PreparationTransitionResponse{
			ID:             row.ID,
			UnitID:         row.PreparationUnitID,
			PriorState:     row.PriorState,
			ResultingState: row.ResultingState,
			ActorStaffID:   row.ActorStaffIdentityID,
			StaffSessionID: row.StaffAccessSessionID,
			OccurredAt:     row.OccurredAt,
		})
	}

	return out, nil
}
