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

// CurrentSalesShiftResponse is the current-Shift read.
//
// ExpectedCashVND is complete in shape but partial in value until Phase 5 adds
// Cash Payments and Cash Refunds. CashMovements is always an array and is
// serialized as [] when empty, never as null.
type CurrentSalesShiftResponse struct {
	SalesShiftResponse
	ExpectedCashVND int64                  `json:"expected_cash_vnd"`
	CashMovements   []CashMovementResponse `json:"cash_movements"`
}

// CashMovementResult is the Cash Movement command response. It returns the
// resulting Expected Cash so the terminal updates its drawer figure without a
// second request.
type CashMovementResult struct {
	Movement        CashMovementResponse `json:"movement"`
	ExpectedCashVND int64                `json:"expected_cash_vnd"`
}
