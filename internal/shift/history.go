package shift

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
)

// Closed-Shift history (spec 9.5, 9.6; ADR-052): a Manager-only read over the
// immutable closure tables. The list pages newest-first through an opaque
// cursor, the detail loads one closed Shift's frozen aggregate, and neither
// touches a live total, an OPEN Shift, or a credential.

const (
	// closedShiftCursorVersion versions the cursor payload: a cursor minted
	// by another format generation is rejected rather than misread.
	closedShiftCursorVersion = 1

	// MaxClosedShiftWindow is the largest [closed_from, closed_to) span one
	// history request may cover (spec 9.5: at most 31 days).
	MaxClosedShiftWindow = 31 * 24 * time.Hour

	// DefaultClosedShiftLimit is the page size when the request carries no
	// limit (spec 9.5).
	DefaultClosedShiftLimit = 50

	// MaxClosedShiftLimit caps one history page (spec 9.5).
	MaxClosedShiftLimit = 100
)

// closedShiftCursor is the versioned cursor payload (spec 9.5): the last row's
// position plus the normalized range it was minted over, so a client cannot
// splice a cursor into a differently-filtered request. It is encoded with
// base64.RawURLEncoding and is exclusive: the next page holds only rows
// strictly after (closed_at, id) under the (closed_at DESC, id DESC) order.
//
// The struct is exported because the codec functions are: the brief's cursor
// shape (field names and JSON tags) is otherwise verbatim.
type ClosedShiftCursor struct {
	Version    int       `json:"v"`
	ClosedAt   time.Time `json:"closed_at"`
	ID         uuid.UUID `json:"id"`
	ClosedFrom time.Time `json:"closed_from"`
	ClosedTo   time.Time `json:"closed_to"`
}

// EncodeClosedShiftCursor serializes one cursor to its opaque base64url form.
func EncodeClosedShiftCursor(c ClosedShiftCursor) (string, error) {
	payload, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("encode closed shift cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

// DecodeClosedShiftCursor parses and validates one opaque cursor against the
// request's normalized [closedFrom, closedTo) window. A decode failure, a
// version mismatch, or a range mismatch is INVALID_INPUT (spec 9.5, 12): the
// 400 prevents duplicate or omitted pages caused by filter changes instead of
// silently repositioning the walk.
func DecodeClosedShiftCursor(token string, closedFrom, closedTo time.Time) (ClosedShiftCursor, error) {
	var cursor ClosedShiftCursor

	payload, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return cursor, fmt.Errorf("%w: cursor is not valid base64url", response.ErrInvalid)
	}
	if err := json.Unmarshal(payload, &cursor); err != nil {
		return cursor, fmt.Errorf("%w: cursor payload is not a valid cursor", response.ErrInvalid)
	}
	if cursor.Version != closedShiftCursorVersion {
		return cursor, fmt.Errorf("%w: cursor version %d is not supported", response.ErrInvalid, cursor.Version)
	}
	if !cursor.ClosedFrom.Equal(closedFrom) || !cursor.ClosedTo.Equal(closedTo) {
		return cursor, fmt.Errorf("%w: cursor range does not match the request range", response.ErrInvalid)
	}
	if cursor.ClosedAt.IsZero() || cursor.ID == uuid.Nil {
		return cursor, fmt.Errorf("%w: cursor carries no page position", response.ErrInvalid)
	}
	return cursor, nil
}

// validateClosedShiftWindow checks the required half-open history window: the
// upper bound may not precede the lower one and the span may not exceed 31
// days (spec 9.5). An empty (from == to) window is well-formed and yields an
// empty page.
func validateClosedShiftWindow(closedFrom, closedTo time.Time) error {
	if closedTo.Before(closedFrom) {
		return fmt.Errorf("%w: closed_to is before closed_from", response.ErrInvalid)
	}
	if closedTo.Sub(closedFrom) > MaxClosedShiftWindow {
		return fmt.Errorf("%w: the closed range spans more than %d days",
			response.ErrInvalid, int(MaxClosedShiftWindow.Hours()/24))
	}
	return nil
}

// ListClosedShiftsHandler pages through closed-Shift summaries.
type ListClosedShiftsHandler struct{ runner *Runner }

// NewListClosedShiftsHandler creates a new ListClosedShiftsHandler.
func NewListClosedShiftsHandler(runner *Runner) *ListClosedShiftsHandler {
	return &ListClosedShiftsHandler{runner: runner}
}

// Handle returns one page of closed-Shift summaries ordered by
// (closed_at DESC, id DESC) within the request's half-open window (spec 9.5).
//
// Pagination is keyset: when the page is full, the last row's (closed_at,
// closure id) tuple is encoded into an opaque cursor whose row-value
// comparison — (closed_at, id) < (last closed_at, last id) — matches the
// descending ORDER BY exactly, so rows sharing one closed_at instant are
// never duplicated or skipped across a page boundary. The summary fields come
// solely from shift_closures and its staff joins; no live total or OPEN Shift
// participates.
//
// The read runs in a read-only repeatable-read transaction with the current
// authority reloaded inside it (ADR-048): the capability is audit.inspect,
// which only Managers hold (spec 10, ADR-052). No fresh PIN is needed for a
// read.
func (h *ListClosedShiftsHandler) Handle(ctx context.Context, actor Actor,
	query ListClosedShiftsQuery,
) (ClosedShiftListResponse, error) {
	return ExecuteRead(ctx, h.runner, actor, OpListClosedShifts, CapAuditInspect,
		func(q *sqlc.Queries) (ClosedShiftListResponse, error) {
			if err := validateClosedShiftWindow(query.ClosedFrom, query.ClosedTo); err != nil {
				return ClosedShiftListResponse{}, err
			}

			limit := query.Limit
			if limit <= 0 {
				limit = DefaultClosedShiftLimit
			}
			if limit > MaxClosedShiftLimit {
				limit = MaxClosedShiftLimit
			}

			params := sqlc.ListClosedShiftSummariesParams{
				ClosedFrom: query.ClosedFrom,
				ClosedTo:   query.ClosedTo,
				RowLimit:   int32(limit), //nolint:gosec // G115: the limit is clamped to [1, 100]
			}
			if query.Cursor != "" {
				cursor, err := DecodeClosedShiftCursor(query.Cursor, query.ClosedFrom, query.ClosedTo)
				if err != nil {
					return ClosedShiftListResponse{}, err
				}
				params.CursorClosedAt = sql.NullTime{Time: cursor.ClosedAt, Valid: true}
				params.CursorID = uuid.NullUUID{UUID: cursor.ID, Valid: true}
			}

			rows, err := q.ListClosedShiftSummaries(ctx, params)
			if err != nil {
				return ClosedShiftListResponse{}, fmt.Errorf("list closed shift summaries: %w", err)
			}

			items := make([]ClosedShiftSummaryResponse, 0, len(rows))
			for _, row := range rows {
				items = append(items, projectClosedShiftSummary(row))
			}

			// A full page may have a successor; the exclusive cursor carries
			// its last row's position and the minting range. The tuple's id is
			// the closure id, which is the second ORDER BY term.
			nextCursor := ""
			if len(rows) == limit {
				last := rows[len(rows)-1]
				token, err := EncodeClosedShiftCursor(ClosedShiftCursor{
					Version:    closedShiftCursorVersion,
					ClosedAt:   last.ClosedAt,
					ID:         last.ClosureID,
					ClosedFrom: query.ClosedFrom,
					ClosedTo:   query.ClosedTo,
				})
				if err != nil {
					return ClosedShiftListResponse{}, err
				}
				nextCursor = token
			}

			return ClosedShiftListResponse{Items: items, NextCursor: nextCursor}, nil
		})
}

// projectClosedShiftSummary builds one summary from its closure row and staff
// joins (spec 9.6): open/close times, both identities, and the three closure
// differences with both of their terms.
func projectClosedShiftSummary(row sqlc.ListClosedShiftSummariesRow) ClosedShiftSummaryResponse {
	return ClosedShiftSummaryResponse{
		ID: row.SalesShiftID,
		Opener: StaffSummary{
			ID:          row.OpenerStaffIdentityID,
			DisplayName: row.OpenerDisplayName,
			LoginCode:   row.OpenerLoginCode,
		},
		Closer: StaffSummary{
			ID:          row.CloserStaffIdentityID,
			DisplayName: row.CloserDisplayName,
			LoginCode:   row.CloserLoginCode,
		},
		OpenedAt:          row.OpenedAt,
		ClosedAt:          row.ClosedAt,
		OpeningFloatVND:   row.OpeningFloatVnd,
		ExpectedCashVND:   row.ExpectedCashVnd,
		ObservedCashVND:   row.ObservedCashVnd,
		CashDifferenceVND: row.CashDifferenceVnd,

		ExpectedManualQRReceivedVND:   row.ExpectedManualQrReceivedVnd,
		ObservedManualQRReceivedVND:   row.ObservedManualQrReceivedVnd,
		ManualQRReceivedDifferenceVND: row.ManualQrReceivedDifferenceVnd,

		ExpectedManualQRRefundedVND:   row.ExpectedManualQrRefundedVnd,
		ObservedManualQRRefundedVND:   row.ObservedManualQrRefundedVnd,
		ManualQRRefundedDifferenceVND: row.ManualQrRefundedDifferenceVnd,

		HasDiscrepancy: row.HasDiscrepancy.Bool,
	}
}

// GetClosedShiftHandler serves one closed Shift's immutable detail.
type GetClosedShiftHandler struct{ runner *Runner }

// NewGetClosedShiftHandler creates a new GetClosedShiftHandler.
func NewGetClosedShiftHandler(runner *Runner) *GetClosedShiftHandler {
	return &GetClosedShiftHandler{runner: runner}
}

// Handle returns one closed Shift's detail aggregate (spec 9.5, 9.6). The
// query matches only shift_closures rows, so an unknown id and a Shift that
// is still OPEN or CLOSING are indistinguishable from the route's point of
// view: both are a 404 that exposes no lifecycle state (spec 12).
//
// Every field comes from the frozen closure and reconciliation rows, the
// append-only attempt ledgers, and the stored discrepancy rows — the detail
// never recalculates the reconciliation. The read requires current
// audit.inspect authority reloaded inside the transaction (ADR-048, spec 10).
func (h *GetClosedShiftHandler) Handle(ctx context.Context, actor Actor,
	shiftID uuid.UUID,
) (ClosedShiftDetailResponse, error) {
	return ExecuteRead(ctx, h.runner, actor, OpGetClosedShift, CapAuditInspect,
		func(q *sqlc.Queries) (ClosedShiftDetailResponse, error) {
			row, err := q.GetClosedShiftDetail(ctx, shiftID)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return ClosedShiftDetailResponse{},
						fmt.Errorf("%w: no closed shift with this id", ErrSalesShiftNotFound)
				}
				return ClosedShiftDetailResponse{}, fmt.Errorf("load closed shift detail: %w", err)
			}

			detail := ClosedShiftDetailResponse{
				ClosedShiftSummaryResponse: projectClosedShiftDetailSummary(row),
				Starter: StaffSummary{
					ID:          row.StartedByStaffIdentityID,
					DisplayName: row.StarterDisplayName,
					LoginCode:   row.StarterLoginCode,
				},

				PayInVND:                        row.PayInVnd,
				PayOutVND:                       row.PayOutVnd,
				CashPaymentVND:                  row.CashPaymentVnd,
				CashPaymentVoidVND:              row.CashPaymentVoidVnd,
				CashRefundVND:                   row.CashRefundVnd,
				ManualQRPaymentVND:              row.ManualQrPaymentVnd,
				ManualQRPaymentVoidVND:          row.ManualQrPaymentVoidVnd,
				PendingManualQRRefundVND:        row.PendingManualQrRefundVnd,
				PendingRefundVND:                row.PendingRefundVnd,
				UnresolvedPostSaleAdjustmentVND: row.UnresolvedPostSaleAdjustmentVnd,
			}

			detail.CashCounts, err = loadClosedCashCounts(ctx, q, row.ReconciliationID)
			if err != nil {
				return ClosedShiftDetailResponse{}, err
			}
			detail.QRObservations, err = loadClosedQRObservations(ctx, q, row.ReconciliationID)
			if err != nil {
				return ClosedShiftDetailResponse{}, err
			}
			detail.Discrepancies, err = loadClosedDiscrepancies(ctx, q, row.ClosureID)
			if err != nil {
				return ClosedShiftDetailResponse{}, err
			}

			// A non-null approver exactly when the close was discrepant — the
			// closure's own approval-difference constraint guarantees it.
			if row.ApprovedByStaffIdentityID.Valid {
				detail.Approver = &StaffSummary{
					ID:          row.ApprovedByStaffIdentityID.UUID,
					DisplayName: row.ApprovedByDisplayName.String,
					LoginCode:   row.ApprovedByLoginCode.String,
				}
			}

			return detail, nil
		})
}

// projectClosedShiftDetailSummary builds the summary half of the detail from
// its closure row. The difference equations live in the database, so the
// discrepancy flag is the stored differences' disjunction.
func projectClosedShiftDetailSummary(row sqlc.GetClosedShiftDetailRow) ClosedShiftSummaryResponse {
	return ClosedShiftSummaryResponse{
		ID: row.SalesShiftID,
		Opener: StaffSummary{
			ID:          row.OpenerStaffIdentityID,
			DisplayName: row.OpenerDisplayName,
			LoginCode:   row.OpenerLoginCode,
		},
		Closer: StaffSummary{
			ID:          row.CloserStaffIdentityID,
			DisplayName: row.CloserDisplayName,
			LoginCode:   row.CloserLoginCode,
		},
		OpenedAt:          row.OpenedAt,
		ClosedAt:          row.ClosedAt,
		OpeningFloatVND:   row.OpeningFloatVnd,
		ExpectedCashVND:   row.ExpectedCashVnd,
		ObservedCashVND:   row.ObservedCashVnd,
		CashDifferenceVND: row.CashDifferenceVnd,

		ExpectedManualQRReceivedVND:   row.ExpectedManualQrReceivedVnd,
		ObservedManualQRReceivedVND:   row.ObservedManualQrReceivedVnd,
		ManualQRReceivedDifferenceVND: row.ManualQrReceivedDifferenceVnd,

		ExpectedManualQRRefundedVND:   row.ManualQrRefundVnd,
		ObservedManualQRRefundedVND:   row.ObservedManualQrRefundedVnd,
		ManualQRRefundedDifferenceVND: row.ManualQrRefundedDifferenceVnd,

		HasDiscrepancy: row.CashDifferenceVnd != 0 ||
			row.ManualQrReceivedDifferenceVnd != 0 ||
			row.ManualQrRefundedDifferenceVnd != 0,
	}
}

// loadClosedCashCounts reads the closure's append-only Cash Count ledger with
// each attempt's actor summary (spec 9.6). The ledger orders by sequence, so
// the detail reports attempts oldest-first.
func loadClosedCashCounts(ctx context.Context, q *sqlc.Queries,
	reconciliationID uuid.UUID,
) ([]CashCountResponse, error) {
	rows, err := q.ListShiftCashCounts(ctx, reconciliationID)
	if err != nil {
		return nil, fmt.Errorf("list closed shift cash counts: %w", err)
	}
	counts := make([]CashCountResponse, 0, len(rows))
	for _, row := range rows {
		countedBy, err := q.GetStaffSummary(ctx, row.CountedByStaffIdentityID)
		if err != nil {
			return nil, fmt.Errorf("load cash count actor: %w", err)
		}
		counts = append(counts, CashCountResponse{
			ID:             row.ID,
			Sequence:       int(row.Sequence),
			CountedCashVND: row.CountedCashVnd,
			CountedBy:      staffSummaryFromRow(countedBy),
			CountedAt:      row.CountedAt,
		})
	}
	return counts, nil
}

// loadClosedQRObservations reads the closure's append-only Manual QR
// observation ledger with each attempt's actor summary (spec 9.6).
func loadClosedQRObservations(ctx context.Context, q *sqlc.Queries,
	reconciliationID uuid.UUID,
) ([]QRObservationResponse, error) {
	rows, err := q.ListShiftQRObservations(ctx, reconciliationID)
	if err != nil {
		return nil, fmt.Errorf("list closed shift qr observations: %w", err)
	}
	observations := make([]QRObservationResponse, 0, len(rows))
	for _, row := range rows {
		observedBy, err := q.GetStaffSummary(ctx, row.ObservedByStaffIdentityID)
		if err != nil {
			return nil, fmt.Errorf("load qr observation actor: %w", err)
		}
		observations = append(observations, QRObservationResponse{
			ID:                  row.ID,
			Sequence:            int(row.Sequence),
			ObservedReceivedVND: row.ObservedReceivedVnd,
			ObservedRefundedVND: row.ObservedRefundedVnd,
			ObservedBy:          staffSummaryFromRow(observedBy),
			ObservedAt:          row.ObservedAt,
		})
	}
	return observations, nil
}

// loadClosedDiscrepancies reads the closure's stored nonzero difference rows
// with their catalogued reasons (spec 9.6). An exact close loads an empty,
// non-nil list.
func loadClosedDiscrepancies(ctx context.Context, q *sqlc.Queries,
	closureID uuid.UUID,
) ([]DiscrepancyResponse, error) {
	rows, err := q.ListShiftClosureDiscrepancies(ctx, closureID)
	if err != nil {
		return nil, fmt.Errorf("list closure discrepancies: %w", err)
	}
	discrepancies := make([]DiscrepancyResponse, 0, len(rows))
	for _, row := range rows {
		var note *string
		if row.Note.Valid {
			note = &row.Note.String
		}
		discrepancies = append(discrepancies, DiscrepancyResponse{
			Dimension:     DiscrepancyDimension(row.Dimension),
			ExpectedVND:   row.ExpectedVnd,
			ObservedVND:   row.ObservedVnd,
			DifferenceVND: row.DifferenceVnd,
			Reason:        DiscrepancyReason(row.Reason),
			Note:          note,
			CreatedAt:     row.CreatedAt,
		})
	}
	return discrepancies, nil
}
