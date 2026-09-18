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
// Service Session: its Checks, Orders, Preparation Units, the recorded
// Preparation history, and the additive post-sale correction history. Callers
// must run it inside the same transaction as their mutation, exactly as
// LoadServiceSession's callers do.
//
// The Checks load under SnapshotCompletedSaleCore: only facts that existed
// before closure — LIVE_CHECK adjustments and Refunds with a null
// completed_sale_id — shape the snapshot, and post-sale Refund allocations
// never consume historical refundable capacity. The boundary is structural
// (completed_sale_id), never a clock, so a later correction can only append to
// PostSaleCorrections. The history reads preparation_unit_transitions
// (ADR-027): the moves are business content the sale is made of, not a report
// reconstructed from audit payloads.
func LoadCompletedSale(ctx context.Context, q *sqlc.Queries, saleID uuid.UUID) (
	CompletedSaleResponse, error,
) {
	out := CompletedSaleResponse{
		Checks:              make([]CompletedSaleCheckResponse, 0),
		Orders:              make([]OrderResponse, 0),
		PreparationUnits:    make([]PreparationUnitResponse, 0),
		PreparationHistory:  make([]PreparationTransitionResponse, 0),
		PostSaleCorrections: make([]PostSaleCorrectionResponse, 0),
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

	checks, err := loadChecksForSnapshot(ctx, q, sale.ServiceSessionID,
		SnapshotCompletedSaleCore)
	if err != nil {
		return out, err
	}
	out.Checks = make([]CompletedSaleCheckResponse, 0, len(checks))
	for _, check := range checks {
		out.Checks = append(out.Checks, CompletedSaleCheckResponse{
			ID:                   check.ID,
			State:                check.State,
			BaseChargeVND:        check.BaseChargeVND,
			ChargeVND:            check.ChargeVND,
			TotalAppliedVND:      check.TotalAppliedVND,
			TotalVoidedVND:       check.TotalVoidedVND,
			TotalRefundedVND:     check.TotalRefundedVND,
			EffectiveReceivedVND: check.EffectiveReceivedVND,
			BalanceVND:           check.BalanceVND,
			PendingRefundVND:     check.PendingRefundVND,
			Payments:             check.Payments,
			Allocations:          check.Allocations,
			ChargeAdjustments:    check.ChargeAdjustments,
			Refunds:              check.Refunds,
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

	corrections, err := loadCompletedSalePostSaleCorrections(ctx, q, sale.ID)
	if err != nil {
		return out, err
	}
	out.PostSaleCorrections = corrections

	return out, nil
}

// loadCompletedSalePostSaleCorrections projects one Completed Sale's whole
// additive history: every POST_SALE Comp, each with the Refunds that consumed
// its corrected capacity and the amount still owed back, ordered by occurrence
// then id. Membership is structural — every row carries this sale's id — so no
// clock decides what the closed snapshot contains. The immutable core loaders
// never read these rows; only a post-sale correction response does.
func loadCompletedSalePostSaleCorrections(ctx context.Context, q *sqlc.Queries,
	saleID uuid.UUID,
) ([]PostSaleCorrectionResponse, error) {
	rows, err := q.ListCompletedSalePostSaleCorrections(ctx, saleID)
	if err != nil {
		return nil, fmt.Errorf("load post-sale corrections: %w", err)
	}

	// Hydrate every post-sale Refund once, in the query's occurrence order,
	// and index it under each Comp adjustment it allocated against. The two
	// capacity sums are derived from those same allocations: allocated counts
	// pending Manual QR intents, completed counts only money that has left.
	refundsByAdjustment := make(map[uuid.UUID][]RefundResponse)
	allocated := make(map[uuid.UUID]int64)
	completed := make(map[uuid.UUID]int64)
	seenRefunds := make(map[uuid.UUID]struct{})
	for _, row := range rows {
		if !row.EntryKind.Valid || row.EntryKind.String != postSaleEntryKindRefund ||
			!row.RefundID.Valid {
			continue
		}
		if _, seen := seenRefunds[row.RefundID.UUID]; seen {
			continue
		}
		seenRefunds[row.RefundID.UUID] = struct{}{}

		refund, err := loadPostSaleRefundForHistory(ctx, q, row)
		if err != nil {
			return nil, err
		}
		for _, allocation := range refund.AdjustmentAllocations {
			refundsByAdjustment[allocation.ID] = append(refundsByAdjustment[allocation.ID], refund)
			next, err := AddCharge(allocated[allocation.ID], allocation.AmountVND)
			if err != nil {
				return nil, fmt.Errorf("%w: post-sale refund allocations: %v",
					ErrFinancialInvariantViolated, err)
			}
			allocated[allocation.ID] = next
			if refund.State != RefundStateCompleted {
				continue
			}
			next, err = AddCharge(completed[allocation.ID], allocation.AmountVND)
			if err != nil {
				return nil, fmt.Errorf("%w: completed post-sale refunds: %v",
					ErrFinancialInvariantViolated, err)
			}
			completed[allocation.ID] = next
		}
	}

	out := make([]PostSaleCorrectionResponse, 0, len(rows))
	for _, row := range rows {
		if !row.EntryKind.Valid || row.EntryKind.String != postSaleEntryKindComp {
			continue
		}
		if !row.ChargeAdjustmentID.Valid || !row.AmountVnd.Valid ||
			!row.CompletedSaleID.Valid || row.CompletedSaleID.UUID != saleID {
			return nil, fmt.Errorf(
				"%w: post-sale comp row of sale %s is missing its adjustment",
				ErrFinancialInvariantViolated, saleID)
		}
		adjustmentID := row.ChargeAdjustmentID.UUID
		remainingVND := row.AmountVnd.Int64 - allocated[adjustmentID]
		outstandingVND := row.AmountVnd.Int64 - completed[adjustmentID]
		if remainingVND < 0 || outstandingVND < 0 {
			return nil, fmt.Errorf(
				"%w: post-sale adjustment %s amount %d is below its refund allocations",
				ErrFinancialInvariantViolated, adjustmentID, row.AmountVnd.Int64)
		}

		refunds := refundsByAdjustment[adjustmentID]
		if refunds == nil {
			refunds = make([]RefundResponse, 0)
		}
		out = append(out, PostSaleCorrectionResponse{
			Adjustment: ChargeAdjustmentResponse{
				ID:                     adjustmentID,
				Kind:                   row.AdjustmentKind.String,
				Scope:                  row.Scope.String,
				PreparationUnitID:      row.PreparationUnitID.UUID,
				PreparationWasteID:     nullUUIDPtr(row.PreparationWasteID),
				ChargeAllocationID:     row.ChargeAllocationID.UUID,
				CompletedSaleID:        nullUUIDPtr(row.CompletedSaleID),
				SalesShiftID:           row.SalesShiftID.UUID,
				AmountVND:              row.AmountVnd.Int64,
				RemainingRefundableVND: remainingVND,
				CreatedAt:              row.CreatedAt.Time,
			},
			Comp: CompResponse{
				ID:                        row.SalesCompID.UUID,
				WasteID:                   row.PreparationWasteID.UUID,
				PreparationUnitID:         row.PreparationUnitID.UUID,
				ChargeAdjustmentID:        adjustmentID,
				AmountVND:                 row.AmountVnd.Int64,
				Reason:                    row.Reason.String,
				Note:                      nullStringPtr(row.Note),
				ActorStaffIdentityID:      row.ActorStaffIdentityID.UUID,
				ApprovedByStaffIdentityID: row.ApprovedByStaffIdentityID.UUID,
				OccurredAt:                row.OccurredAt.Time,
			},
			Refunds:              refunds,
			OutstandingRefundVND: outstandingVND,
		})
	}
	return out, nil
}

// loadPostSaleRefundForHistory assembles one post-sale Refund projection from
// its history row and both allocation collections. State is derived from the
// completion evidence, never stored.
func loadPostSaleRefundForHistory(ctx context.Context, q *sqlc.Queries,
	row sqlc.ListCompletedSalePostSaleCorrectionsRow,
) (RefundResponse, error) {
	paymentRows, err := q.ListRefundPaymentAllocations(ctx, row.RefundID.UUID)
	if err != nil {
		return RefundResponse{}, fmt.Errorf("load refund payment allocations: %w", err)
	}
	adjustmentRows, err := q.ListRefundAdjustmentAllocations(ctx, row.RefundID.UUID)
	if err != nil {
		return RefundResponse{}, fmt.Errorf("load refund adjustment allocations: %w", err)
	}

	out := RefundResponse{
		ID:                        row.RefundID.UUID,
		CheckID:                   row.CheckID.UUID,
		CompletedSaleID:           nullUUIDPtr(row.CompletedSaleID),
		SalesShiftID:              row.SalesShiftID.UUID,
		Method:                    row.RefundMethod.String,
		AmountVND:                 row.AmountVnd.Int64,
		State:                     RefundStatePending,
		Reason:                    row.Reason.String,
		Note:                      nullStringPtr(row.Note),
		ActorStaffIdentityID:      row.ActorStaffIdentityID.UUID,
		ApprovedByStaffIdentityID: row.ApprovedByStaffIdentityID.UUID,
		CreatedAt:                 row.CreatedAt.Time,
		PaymentAllocations:        make([]RefundAllocationResponse, 0, len(paymentRows)),
		AdjustmentAllocations:     make([]RefundAllocationResponse, 0, len(adjustmentRows)),
	}
	for _, allocation := range paymentRows {
		out.PaymentAllocations = append(out.PaymentAllocations,
			RefundAllocationResponse{ID: allocation.PaymentID, AmountVND: allocation.AmountVnd})
	}
	for _, allocation := range adjustmentRows {
		out.AdjustmentAllocations = append(out.AdjustmentAllocations,
			RefundAllocationResponse{
				ID:        allocation.ChargeAdjustmentID,
				AmountVND: allocation.AmountVnd,
			})
	}
	if row.RefundCompletionID.Valid {
		out.State = RefundStateCompleted
		out.Completion = &RefundCompletionResponse{
			ID:                            row.RefundCompletionID.UUID,
			TransactionReference:          nullStringPtr(row.TransactionReference),
			CompletedByStaffIdentityID:    row.CompletedByStaffIdentityID.UUID,
			CompletedStaffAccessSessionID: row.CompletedStaffAccessSessionID.UUID,
			CompletedAt:                   row.CompletedAt.Time,
		}
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
