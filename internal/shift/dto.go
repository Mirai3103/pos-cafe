package shift

import (
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

// SalesShiftResponse is the API representation of a Sales Shift.
type SalesShiftResponse struct {
	ID              uuid.UUID    `json:"id"`
	State           string       `json:"state"`
	OpeningFloatVND int64        `json:"opening_float_vnd"`
	OpenedAt        time.Time    `json:"opened_at"`
	Opener          StaffSummary `json:"opener"`
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

// Refund states in the Shift projection. A Refund's state is derived from the
// existence of its completion evidence, never stored or copied, so a Refund
// cannot report COMPLETED without a completion time.
const (
	RefundStatePending   = "PENDING"
	RefundStateCompleted = "COMPLETED"
)

// RefundSummaryResponse is one Refund in the current-Shift read: the money
// owed back or already returned, with its derived state. It carries no
// credentials and no allocation detail, matching the Shift boundary's
// least-disclosure rule.
type RefundSummaryResponse struct {
	ID              uuid.UUID  `json:"id"`
	CheckID         uuid.UUID  `json:"check_id"`
	CompletedSaleID *uuid.UUID `json:"completed_sale_id,omitempty"`
	Method          string     `json:"method"`
	AmountVND       int64      `json:"amount_vnd"`
	State           string     `json:"state"`
	CreatedAt       time.Time  `json:"created_at"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
}

// CurrentSalesShiftResponse is the current-Shift read.
//
// ExpectedCashVND is the drawer figure from ComputeExpectedCash; the remaining
// scalars are the Phase 6C reconciliation terms (ADR-046). pending_refund_vnd
// is money owed back, and unresolved_post_sale_adjustment_vnd is the post-sale
// correction amount not yet covered by completed Refunds. Both CashMovements
// and Refunds are always arrays and serialize as [] when empty, never null.
type CurrentSalesShiftResponse struct {
	SalesShiftResponse
	ExpectedCashVND                 int64                   `json:"expected_cash_vnd"`
	CashPaymentVND                  int64                   `json:"cash_payment_vnd"`
	CashPaymentVoidVND              int64                   `json:"cash_payment_void_vnd"`
	CashRefundVND                   int64                   `json:"cash_refund_vnd"`
	ManualQRPaymentVND              int64                   `json:"manual_qr_payment_vnd"`
	ManualQRPaymentVoidVND          int64                   `json:"manual_qr_payment_void_vnd"`
	ManualQRRefundVND               int64                   `json:"manual_qr_refund_vnd"`
	PendingManualQRRefundVND        int64                   `json:"pending_manual_qr_refund_vnd"`
	PendingRefundVND                int64                   `json:"pending_refund_vnd"`
	UnresolvedPostSaleAdjustmentVND int64                   `json:"unresolved_post_sale_adjustment_vnd"`
	CashMovements                   []CashMovementResponse  `json:"cash_movements"`
	Refunds                         []RefundSummaryResponse `json:"refunds"`
}

// CashMovementResult is the Cash Movement command response. It returns the
// resulting Expected Cash so the terminal updates its drawer figure without a
// second request.
type CashMovementResult struct {
	Movement        CashMovementResponse `json:"movement"`
	ExpectedCashVND int64                `json:"expected_cash_vnd"`
}
