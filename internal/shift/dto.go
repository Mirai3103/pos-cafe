package shift

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/google/uuid"
)

// OpenShiftCommand opens a Sales Shift.
//
// OpeningFloatVND is a pointer so a missing JSON field is rejected instead of
// silently defaulting to zero, which is itself a valid counted float.
type OpenShiftCommand struct {
	RequestID       uuid.UUID `json:"request_id"`
	OpeningFloatVND *int64    `json:"opening_float_vnd"`
}

// RecordCashMovementCommand records a Pay In or Pay Out against an open Shift.
//
// ShiftID comes from the route, not the body. ManagerPIN is a secret: it is
// excluded from the request fingerprint and must never be audited or logged.
type RecordCashMovementCommand struct {
	RequestID         uuid.UUID `json:"request_id"`
	ShiftID           uuid.UUID `json:"-"`
	Method            string    `json:"method"`
	AmountVND         *int64    `json:"amount_vnd"`
	Reason            string    `json:"reason"`
	Note              *string   `json:"note"`
	ApproverLoginCode string    `json:"approver_login_code"`
	ManagerPIN        string    `json:"manager_pin"`
}

// StartReconciliationCommand starts a Sales Shift's blind reconciliation with
// the initial Cash Count.
//
// ShiftID comes from the route, not the body. CountedCashVND is pointer-backed
// so an omitted field is rejected rather than silently counted as zero, which
// is itself a valid count.
type StartReconciliationCommand struct {
	RequestID      uuid.UUID `json:"request_id"`
	ShiftID        uuid.UUID `json:"-"`
	CountedCashVND *int64    `json:"counted_cash_vnd"`
}

// RecordCashCountCommand appends one immutable Cash Count attempt (a recount)
// to a CLOSING Shift's reconciliation.
//
// ShiftID comes from the route, not the body. CountedCashVND is pointer-backed
// so an omitted field is rejected rather than silently counted as zero, which
// is itself a valid count.
type RecordCashCountCommand struct {
	RequestID      uuid.UUID `json:"request_id"`
	ShiftID        uuid.UUID `json:"-"`
	CountedCashVND *int64    `json:"counted_cash_vnd"`
}

// RecordQRObservationCommand appends one immutable Manual QR observation
// attempt (a recheck) to a CLOSING Shift's reconciliation.
//
// ShiftID comes from the route, not the body. Both observed values are
// pointer-backed and mandatory together: one without the other is rejected,
// while an explicit zero on either is a meaningful observation (spec 14.3).
type RecordQRObservationCommand struct {
	RequestID           uuid.UUID `json:"request_id"`
	ShiftID             uuid.UUID `json:"-"`
	ObservedReceivedVND *int64    `json:"observed_received_vnd"`
	ObservedRefundedVND *int64    `json:"observed_refunded_vnd"`
}

// CloseDiscrepancyInput is one client-declared reason for a nonzero closure
// dimension. It carries reason metadata only: expected, observed, and
// difference amounts never come from the request — the server derives all
// three from the frozen snapshot and the final evidence (spec 9.4).
type CloseDiscrepancyInput struct {
	Dimension DiscrepancyDimension `json:"dimension"`
	Reason    DiscrepancyReason    `json:"reason"`
	Note      *string              `json:"note"`
}

// CloseShiftCommand closes a reconciled Sales Shift exactly or, with
// discrepancy reasons, under a fresh Manager Approval.
//
// ShiftID comes from the route, not the body. Discrepancies selects the
// operation: a non-null empty array is the exact close and any entry selects
// shift.close_with_discrepancy, which requires the approval pair. The field
// is checked for nil at the HTTP boundary so a JSON null cannot masquerade as
// the exact close. ManagerPIN is a secret: it is excluded from the request
// fingerprint and must never be audited or logged, and the approver login is
// excluded from the fingerprint too (spec 9.4).
type CloseShiftCommand struct {
	RequestID            uuid.UUID               `json:"request_id"`
	ShiftID              uuid.UUID               `json:"-"`
	FinalCashCountID     uuid.UUID               `json:"final_cash_count_id"`
	FinalQRObservationID uuid.UUID               `json:"final_qr_observation_id"`
	Discrepancies        []CloseDiscrepancyInput `json:"discrepancies"`
	ApproverLoginCode    string                  `json:"approver_login_code"`
	ManagerPIN           string                  `json:"manager_pin"`
}

// StaffSummary is the only staff representation that crosses the Shift
// boundary. It carries exactly these three fields.
type StaffSummary struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"display_name"`
	LoginCode   string    `json:"login_code"`
}

func staffSummaryFromApprover(a auth.ApproverSummary) StaffSummary {
	return StaffSummary{ID: a.ID, DisplayName: a.DisplayName, LoginCode: a.LoginCode}
}

// SalesShiftMetadata is the reusable Shift identification every Shift response
// shares: identity, state, opening time, and opener.
type SalesShiftMetadata struct {
	ID       uuid.UUID    `json:"id"`
	State    string       `json:"state"`
	OpenedAt time.Time    `json:"opened_at"`
	Opener   StaffSummary `json:"opener"`
}

// SalesShiftResponse is the API representation of a Sales Shift, including its
// Opening Float. It is the open command's response, which may echo a value
// supplied by its own actor. Reads that must not disclose the float are built
// from SalesShiftMetadata alone.
type SalesShiftResponse struct {
	SalesShiftMetadata
	OpeningFloatVND int64 `json:"opening_float_vnd"`
}

// OpenCurrentShiftResponse is the current-Shift read while the Shift is OPEN:
// the Shift metadata and nothing else. Its field allowlist (id, state,
// opened_at, opener) is part of the blind-count boundary, so it carries no
// Opening Float, Expected Cash, Cash Movement, Refund, or reconciliation
// field.
type OpenCurrentShiftResponse struct {
	SalesShiftMetadata
}

// CashMovementResponse is the API representation of one Cash Movement.
type CashMovementResponse struct {
	ID           uuid.UUID    `json:"id"`
	SalesShiftID uuid.UUID    `json:"sales_shift_id"`
	Method       string       `json:"method"`
	AmountVND    int64        `json:"amount_vnd"`
	Reason       string       `json:"reason"`
	Note         *string      `json:"note"`
	Initiator    StaffSummary `json:"initiator"`
	Approver     StaffSummary `json:"approver"`
	OccurredAt   time.Time    `json:"occurred_at"`
}

// CashMovementResult is the Cash Movement command response. It carries the
// created movement only: while the Shift is OPEN, no Expected Cash or source
// total may cross the boundary.
type CashMovementResult struct {
	Movement CashMovementResponse `json:"movement"`
}

// CashCountResponse is one immutable Cash Count attempt. Sequence 1 is the
// blind initial count; a higher sequence is a recount.
type CashCountResponse struct {
	ID             uuid.UUID    `json:"id"`
	Sequence       int          `json:"sequence"`
	CountedCashVND int64        `json:"counted_cash_vnd"`
	CountedBy      StaffSummary `json:"counted_by"`
	CountedAt      time.Time    `json:"counted_at"`
}

// QRObservationResponse is one immutable Manual QR observation attempt. Both
// observed values are explicit, including zero.
type QRObservationResponse struct {
	ID                  uuid.UUID    `json:"id"`
	Sequence            int          `json:"sequence"`
	ObservedReceivedVND int64        `json:"observed_received_vnd"`
	ObservedRefundedVND int64        `json:"observed_refunded_vnd"`
	ObservedBy          StaffSummary `json:"observed_by"`
	ObservedAt          time.Time    `json:"observed_at"`
}

// ReconciliationPreviewEntry is one dimension's comparison between the frozen
// expected value and the latest observed evidence. ObservedVND and
// DifferenceVND are nil while no evidence of that dimension exists yet. A
// positive difference is an excess; a negative one is a shortage.
type ReconciliationPreviewEntry struct {
	Dimension       DiscrepancyDimension `json:"dimension"`
	ExpectedVND     int64                `json:"expected_vnd"`
	ObservedVND     *int64               `json:"observed_vnd"`
	DifferenceVND   *int64               `json:"difference_vnd"`
	RecheckRequired bool                 `json:"recheck_required"`
}

// ReconciliationPreview compares all three dimensions and reports whether the
// Shift can close exactly. Dimensions always holds exactly three entries, in
// DimensionCash, DimensionManualQRReceived, DimensionManualQRRefunded order,
// and serializes as [] never null.
type ReconciliationPreview struct {
	Dimensions []ReconciliationPreviewEntry `json:"dimensions"`
	CanClose   bool                         `json:"can_close"`
}

// ReconciliationResponse is the frozen reconciliation snapshot: the expected
// scalars with the source facts they were computed from, the append-only
// attempts, and the preview built from the latest evidence. It is written once
// when the Shift moves to CLOSING and never recalculated from live data.
type ReconciliationResponse struct {
	ID        uuid.UUID    `json:"id"`
	Starter   StaffSummary `json:"starter"`
	StartedAt time.Time    `json:"started_at"`

	OpeningFloatVND                 int64 `json:"opening_float_vnd"`
	PayInVND                        int64 `json:"pay_in_vnd"`
	PayOutVND                       int64 `json:"pay_out_vnd"`
	CashPaymentVND                  int64 `json:"cash_payment_vnd"`
	CashPaymentVoidVND              int64 `json:"cash_payment_void_vnd"`
	CashRefundVND                   int64 `json:"cash_refund_vnd"`
	ExpectedCashVND                 int64 `json:"expected_cash_vnd"`
	ManualQRPaymentVND              int64 `json:"manual_qr_payment_vnd"`
	ManualQRPaymentVoidVND          int64 `json:"manual_qr_payment_void_vnd"`
	ExpectedManualQRReceivedVND     int64 `json:"expected_manual_qr_received_vnd"`
	ManualQRRefundVND               int64 `json:"manual_qr_refund_vnd"`
	PendingManualQRRefundVND        int64 `json:"pending_manual_qr_refund_vnd"`
	PendingRefundVND                int64 `json:"pending_refund_vnd"`
	UnresolvedPostSaleAdjustmentVND int64 `json:"unresolved_post_sale_adjustment_vnd"`

	CashCounts     []CashCountResponse     `json:"cash_counts"`
	QRObservations []QRObservationResponse `json:"qr_observations"`
	Preview        ReconciliationPreview   `json:"preview"`
}

// ClosingShiftResponse is the read and command response while the Shift is
// CLOSING: the Shift metadata plus its frozen reconciliation.
type ClosingShiftResponse struct {
	SalesShiftMetadata
	Reconciliation ReconciliationResponse `json:"reconciliation"`
}

// CurrentShiftResponse is the current-Shift read's state-dispatched payload
// (spec 9.5): the redacted OPEN shape, the frozen CLOSING reconciliation
// shape, or nil (data: null) when no Shift is active.
//
// The branches are unexported so only the constructors can build the tagged
// DTO, and MarshalJSON serializes exactly the set branch. The envelope
// therefore keeps the branch's own key allowlist (spec 9.6) instead of
// wrapping it in a discriminator key: an OPEN read serializes as
// id/state/opened_at/opener and a CLOSING read as metadata plus
// reconciliation.
type CurrentShiftResponse struct {
	open    *OpenCurrentShiftResponse
	closing *ClosingShiftResponse
}

// newOpenCurrentShiftResponse tags the OPEN branch of the current-Shift read.
func newOpenCurrentShiftResponse(open OpenCurrentShiftResponse) *CurrentShiftResponse {
	return &CurrentShiftResponse{open: &open}
}

// newClosingCurrentShiftResponse tags the CLOSING branch of the current-Shift
// read.
func newClosingCurrentShiftResponse(closing ClosingShiftResponse) *CurrentShiftResponse {
	return &CurrentShiftResponse{closing: &closing}
}

// MarshalJSON serializes exactly one branch. encoding/json marshals a nil
// *CurrentShiftResponse as null without calling this method (the no-active-
// Shift case keeps its data: null shape); the nil check guards a direct call.
func (r *CurrentShiftResponse) MarshalJSON() ([]byte, error) {
	switch {
	case r == nil:
		return []byte("null"), nil
	case r.open != nil:
		return json.Marshal(r.open)
	case r.closing != nil:
		return json.Marshal(r.closing)
	default:
		return nil, errors.New("current shift response carries no branch")
	}
}

// CashCountResult is the append-cash-count response (spec 9.2): the newly
// appended attempt plus the full preview rebuilt from the latest evidence.
type CashCountResult struct {
	CashCount CashCountResponse     `json:"cash_count"`
	Preview   ReconciliationPreview `json:"preview"`
}

// QRObservationResult is the append-qr-observation response (spec 9.3): the
// newly appended attempt plus the full preview rebuilt from the latest
// evidence.
type QRObservationResult struct {
	QRObservation QRObservationResponse `json:"qr_observation"`
	Preview       ReconciliationPreview `json:"preview"`
}

// DiscrepancyResponse is one nonzero signed difference with its catalogued
// reason. Exact dimensions have no row.
type DiscrepancyResponse struct {
	Dimension     DiscrepancyDimension `json:"dimension"`
	ExpectedVND   int64                `json:"expected_vnd"`
	ObservedVND   int64                `json:"observed_vnd"`
	DifferenceVND int64                `json:"difference_vnd"`
	Reason        DiscrepancyReason    `json:"reason"`
	Note          *string              `json:"note"`
	CreatedAt     time.Time            `json:"created_at"`
}

// ClosedShiftSummaryResponse is one closed Shift in the history list: the
// open/close facts and the three closure differences with both of their terms.
type ClosedShiftSummaryResponse struct {
	ID       uuid.UUID    `json:"id"`
	Opener   StaffSummary `json:"opener"`
	Closer   StaffSummary `json:"closer"`
	OpenedAt time.Time    `json:"opened_at"`
	ClosedAt time.Time    `json:"closed_at"`

	OpeningFloatVND int64 `json:"opening_float_vnd"`

	ExpectedCashVND   int64 `json:"expected_cash_vnd"`
	ObservedCashVND   int64 `json:"observed_cash_vnd"`
	CashDifferenceVND int64 `json:"cash_difference_vnd"`

	ExpectedManualQRReceivedVND   int64 `json:"expected_manual_qr_received_vnd"`
	ObservedManualQRReceivedVND   int64 `json:"observed_manual_qr_received_vnd"`
	ManualQRReceivedDifferenceVND int64 `json:"manual_qr_received_difference_vnd"`

	ExpectedManualQRRefundedVND   int64 `json:"expected_manual_qr_refunded_vnd"`
	ObservedManualQRRefundedVND   int64 `json:"observed_manual_qr_refunded_vnd"`
	ManualQRRefundedDifferenceVND int64 `json:"manual_qr_refunded_difference_vnd"`

	HasDiscrepancy bool `json:"has_discrepancy"`
}

// ClosedShiftDetailResponse is one closed Shift's immutable detail: the
// summary plus the complete frozen source scalars, the reconciliation starter,
// every attempt, the discrepancy rows (an empty list when the close was
// exact), and the approving Manager when the close was discrepant.
type ClosedShiftDetailResponse struct {
	ClosedShiftSummaryResponse
	Starter StaffSummary `json:"starter"`

	PayInVND                        int64 `json:"pay_in_vnd"`
	PayOutVND                       int64 `json:"pay_out_vnd"`
	CashPaymentVND                  int64 `json:"cash_payment_vnd"`
	CashPaymentVoidVND              int64 `json:"cash_payment_void_vnd"`
	CashRefundVND                   int64 `json:"cash_refund_vnd"`
	ManualQRPaymentVND              int64 `json:"manual_qr_payment_vnd"`
	ManualQRPaymentVoidVND          int64 `json:"manual_qr_payment_void_vnd"`
	PendingManualQRRefundVND        int64 `json:"pending_manual_qr_refund_vnd"`
	PendingRefundVND                int64 `json:"pending_refund_vnd"`
	UnresolvedPostSaleAdjustmentVND int64 `json:"unresolved_post_sale_adjustment_vnd"`

	CashCounts     []CashCountResponse     `json:"cash_counts"`
	QRObservations []QRObservationResponse `json:"qr_observations"`
	Discrepancies  []DiscrepancyResponse   `json:"discrepancies"`
	Approver       *StaffSummary           `json:"approver"`
}
