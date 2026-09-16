package sales

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"

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
		// The closure transaction can never see ErrNoRows here:
		// FindCompletedSaleByServiceSession and InsertCompletedSale's RETURNING
		// only hand out ids this transaction can see. The by-id read can: it
		// accepts arbitrary ids, and GetCompletedSaleHandler maps the error to
		// ErrCompletedSaleNotFound for it.
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

// GetCompletedSaleHandler serves the Completed Sale read by id.
type GetCompletedSaleHandler struct{ runner *Runner }

// NewGetCompletedSaleHandler creates a new GetCompletedSaleHandler.
func NewGetCompletedSaleHandler(runner *Runner) *GetCompletedSaleHandler {
	return &GetCompletedSaleHandler{runner: runner}
}

// Handle returns one immutable Completed Sale with its Checks, Orders,
// Preparation Units, and recorded preparation history.
//
// The read carries no open-Shift requirement: staff must be able to inspect a
// sale after its Shift closes, exactly as they can a Session. An unknown id is
// a not-found answer, not a defect — the caller may hold a stale link.
func (h *GetCompletedSaleHandler) Handle(ctx context.Context, actor Actor, saleID uuid.UUID) (
	int, CompletedSaleResponse, error,
) {
	sale, err := ExecuteRead(ctx, h.runner, actor, OpGetCompletedSale, CapSalesOperate,
		func(q *sqlc.Queries) (CompletedSaleResponse, error) {
			out, err := LoadCompletedSale(ctx, q, saleID)
			if errors.Is(err, sql.ErrNoRows) {
				return CompletedSaleResponse{},
					fmt.Errorf("%w: %s", ErrCompletedSaleNotFound, saleID)
			}
			return out, err
		})
	if err != nil {
		return 0, CompletedSaleResponse{}, err
	}
	return http.StatusOK, sale, nil
}

// GetCompletedSaleBySessionHandler serves the Completed Sale read by Service
// Session.
type GetCompletedSaleBySessionHandler struct{ runner *Runner }

// NewGetCompletedSaleBySessionHandler creates a new
// GetCompletedSaleBySessionHandler.
func NewGetCompletedSaleBySessionHandler(runner *Runner) *GetCompletedSaleBySessionHandler {
	return &GetCompletedSaleBySessionHandler{runner: runner}
}

// Handle returns the immutable Completed Sale of one Service Session.
//
// The Session's id resolves to its sale's id through the same lookup the
// closure path replays on, so an open Session and an unknown Session answer
// identically: neither has a Completed Sale.
func (h *GetCompletedSaleBySessionHandler) Handle(ctx context.Context, actor Actor,
	sessionID uuid.UUID,
) (int, CompletedSaleResponse, error) {
	sale, err := ExecuteRead(ctx, h.runner, actor, OpGetCompletedSaleBySession, CapSalesOperate,
		func(q *sqlc.Queries) (CompletedSaleResponse, error) {
			saleID, err := q.FindCompletedSaleByServiceSession(ctx, sessionID)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return CompletedSaleResponse{},
						fmt.Errorf("%w: session %s", ErrCompletedSaleNotFound, sessionID)
				}
				return CompletedSaleResponse{},
					fmt.Errorf("find completed sale: %w", err)
			}
			return LoadCompletedSale(ctx, q, saleID)
		})
	if err != nil {
		return 0, CompletedSaleResponse{}, err
	}
	return http.StatusOK, sale, nil
}
