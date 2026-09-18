// Package shift implements the Sales Shift vertical slice: the accountability
// window for the cashier station's cash fund, the Cash Movements that change
// its Expected Cash, and the read that reports both.
package shift

import (
	"fmt"
	"math"
	"strings"
	"unicode/utf8"
)

// CapSalesShiftOperate is the capability every Shift operation requires. It is
// already derived for MANAGER and CASHIER by auth.DeriveCapabilities.
const CapSalesShiftOperate = "sales_shift.operate"

// Idempotency action names, stored in idempotency_keys.action.
const (
	OpOpenShift          = "shift.open_shift"
	OpRecordCashMovement = "shift.record_cash_movement"
	OpGetCurrentShift    = "shift.get_current_shift"
)

// Audit event types. Business events are UPPER_SNAKE_CASE and the denial event
// is lowercase dotted, matching the convention in internal/tables.
const (
	EventSalesShiftOpened     = "SALES_SHIFT_OPENED"
	EventCashMovementRecorded = "CASH_MOVEMENT_RECORDED"
	EventAuthorizationDenied  = "shift.authorization_denied"
)

// Sales Shift states. StateClosing marks a Shift whose reconciliation has
// started but that has not closed yet: ordinary commands still require OPEN,
// and no route returns a CLOSING Shift to OPEN or reopens a CLOSED one.
const (
	StateOpen    = "OPEN"
	StateClosing = "CLOSING"
	StateClosed  = "CLOSED"
)

// DiscrepancyDimension is one axis a closure difference is measured on.
type DiscrepancyDimension string

// DiscrepancyReason is one catalogued explanation for a nonzero difference.
type DiscrepancyReason string

// Reconciliation dimensions, in preview order.
const (
	DimensionCash             = "CASH"
	DimensionManualQRReceived = "MANUAL_QR_RECEIVED"
	DimensionManualQRRefunded = "MANUAL_QR_REFUNDED"
)

// Discrepancy reasons. ReasonOther is declared once with the Cash Movement
// reasons above and shared with this catalog: the same OTHER value carries the
// same requires-a-note rule in both.
const (
	ReasonCashCountDifference     = "CASH_COUNT_DIFFERENCE"
	ReasonQRObservationDifference = "QR_OBSERVATION_DIFFERENCE"
	ReasonUnexplained             = "UNEXPLAINED"
)

// Cash Movement methods. Direction is carried here, never by a negative amount.
const (
	MethodPayIn  = "PAY_IN"
	MethodPayOut = "PAY_OUT"
)

// Cash Movement reasons.
const (
	ReasonAddChangeFund     = "ADD_CHANGE_FUND"
	ReasonRemoveExcessFloat = "REMOVE_EXCESS_FLOAT"
	ReasonSafeDrop          = "SAFE_DROP"
	ReasonOther             = "OTHER"
)

// MaxAmountVND is the inclusive upper bound on every Phase 4 monetary value.
// It matches the canonical MAX_OPENING_FLOAT_VND and MAX_CASH_MOVEMENT_VND.
// BIGINT could hold more, but this is the bound the business rules are written
// against, so it is enforced in Go and in the database.
const MaxAmountVND int64 = 2147483647

// MaxNoteLength is the inclusive upper bound on a trimmed note, counted in
// Unicode code points so Go agrees with the database char_length check.
const MaxNoteLength = 500

// ValidateOpeningFloat checks a counted Opening Float. Zero is valid.
func ValidateOpeningFloat(v int64) error {
	if v < 0 {
		return fmt.Errorf("opening_float_vnd cannot be negative")
	}
	if v > MaxAmountVND {
		return fmt.Errorf("opening_float_vnd exceeds the maximum of %d", MaxAmountVND)
	}
	return nil
}

// ValidateAmount checks a Cash Movement amount, which is strictly positive.
func ValidateAmount(v int64) error {
	if v <= 0 {
		return fmt.Errorf("amount_vnd must be greater than 0")
	}
	if v > MaxAmountVND {
		return fmt.Errorf("amount_vnd exceeds the maximum of %d", MaxAmountVND)
	}
	return nil
}

// ValidateMethod checks a Cash Movement method.
func ValidateMethod(m string) error {
	switch m {
	case MethodPayIn, MethodPayOut:
		return nil
	default:
		return fmt.Errorf("method must be %s or %s", MethodPayIn, MethodPayOut)
	}
}

// ValidateReason checks a Cash Movement reason.
func ValidateReason(r string) error {
	switch r {
	case ReasonAddChangeFund, ReasonRemoveExcessFloat, ReasonSafeDrop, ReasonOther:
		return nil
	default:
		return fmt.Errorf("reason must be one of %s, %s, %s, %s",
			ReasonAddChangeFund, ReasonRemoveExcessFloat, ReasonSafeDrop, ReasonOther)
	}
}

// NormalizeNote trims a note and reports nil for an empty result, matching the
// canonical `note || null` transform. Normalizing before fingerprinting keeps
// an idempotent replay stable across cosmetic whitespace differences.
func NormalizeNote(note *string) *string {
	if note == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*note)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// ValidateNote checks an already-normalized note against its length bounds and
// the reason-specific requirement. Reason OTHER requires a note.
func ValidateNote(note *string, reason string) error {
	if note == nil {
		if reason == ReasonOther {
			return fmt.Errorf("note is required when reason is %s", ReasonOther)
		}
		return nil
	}
	return checkNoteLength(note)
}

// checkNoteLength enforces the inclusive 1..MaxNoteLength rune bound on a
// present note. Runes, not bytes, so Go agrees with the database char_length
// check.
func checkNoteLength(note *string) error {
	n := utf8.RuneCountInString(*note)
	if n < 1 {
		return fmt.Errorf("note cannot be empty")
	}
	if n > MaxNoteLength {
		return fmt.Errorf("note is %d characters, maximum is %d", n, MaxNoteLength)
	}
	return nil
}

// ValidateDiscrepancyReason checks a discrepancy reason against its dimension
// and the reason catalog's note rules.
//
// CASH_COUNT_DIFFERENCE is valid only for CASH and QR_OBSERVATION_DIFFERENCE
// only for the two Manual QR dimensions; UNEXPLAINED and OTHER are valid for
// every dimension. OTHER requires a note; every other reason forbids one. The
// note must already be normalized (trimmed), as with ValidateNote.
func ValidateDiscrepancyReason(dimension, reason string, note *string) error {
	switch dimension {
	case DimensionCash, DimensionManualQRReceived, DimensionManualQRRefunded:
	default:
		return fmt.Errorf("dimension must be one of %s, %s, %s",
			DimensionCash, DimensionManualQRReceived, DimensionManualQRRefunded)
	}

	switch reason {
	case ReasonCashCountDifference:
		if dimension != DimensionCash {
			return fmt.Errorf("reason %s is valid only for dimension %s",
				ReasonCashCountDifference, DimensionCash)
		}
	case ReasonQRObservationDifference:
		if dimension != DimensionManualQRReceived && dimension != DimensionManualQRRefunded {
			return fmt.Errorf("reason %s is valid only for dimensions %s and %s",
				ReasonQRObservationDifference, DimensionManualQRReceived, DimensionManualQRRefunded)
		}
	case ReasonUnexplained, ReasonOther:
	default:
		return fmt.Errorf("reason must be one of %s, %s, %s, %s",
			ReasonCashCountDifference, ReasonQRObservationDifference, ReasonUnexplained, ReasonOther)
	}

	if reason == ReasonOther {
		if note == nil {
			return fmt.Errorf("note is required when reason is %s", ReasonOther)
		}
		return checkNoteLength(note)
	}
	if note != nil {
		return fmt.Errorf("note is only allowed when reason is %s", ReasonOther)
	}
	return nil
}

// ComputeExpectedCash returns the Sales Shift's calculated cash responsibility.
//
// Opening Float plus valid Cash Payments, less completed Cash Refunds, plus
// Pay Ins, less Pay Outs (ADR-046). The Cash Payment term counts original
// applied amounts and the Void term removes the source Payment's whole amount,
// so their difference is the non-voided Cash Payments. A pending Refund is
// absent from the formula because the money has not moved yet.
//
// The guard is symmetric because sustained Pay Outs can legitimately drive the
// figure negative. A total outside the bound indicates corrupt data, not a
// legitimate drawer balance.
func ComputeExpectedCash(
	openingFloatVND, cashPaymentVND, cashPaymentVoidVND, cashRefundVND, payInVND, payOutVND int64,
) (int64, error) {
	total, err := addAmount(openingFloatVND, cashPaymentVND)
	if err != nil {
		return 0, err
	}
	if total, err = subtractAmount(total, cashPaymentVoidVND); err != nil {
		return 0, err
	}
	if total, err = subtractAmount(total, cashRefundVND); err != nil {
		return 0, err
	}
	if total, err = addAmount(total, payInVND); err != nil {
		return 0, err
	}
	if total, err = subtractAmount(total, payOutVND); err != nil {
		return 0, err
	}
	if total > MaxAmountVND || total < -MaxAmountVND {
		return 0, fmt.Errorf("%w: expected cash %d is outside [%d, %d]",
			ErrExpectedCashOutOfRange, total, -MaxAmountVND, MaxAmountVND)
	}
	return total, nil
}

// ComputeDifference returns the signed closure difference observed - expected.
// A positive value is an excess; a negative value is a shortage. It runs
// through the same guarded arithmetic as Expected Cash so a wrap cannot
// masquerade as a legitimate difference.
func ComputeDifference(observed, expected int64) (int64, error) {
	return subtractAmount(observed, expected)
}

// addAmount and subtractAmount are the guarded arithmetic this formula runs
// through.
//
// The bound check below is only meaningful on a total that has not already
// wrapped, and Go's integer arithmetic wraps silently. The Cash Payment term is
// a SUM over a Shift's Payments, which has no upper bound of its own, so an
// unguarded sum would defeat the very check it feeds.
func addAmount(a, b int64) (int64, error) {
	if b > 0 && a > math.MaxInt64-b {
		return 0, fmt.Errorf("%w: %d + %d overflows", ErrExpectedCashOutOfRange, a, b)
	}
	if b < 0 && a < math.MinInt64-b {
		return 0, fmt.Errorf("%w: %d + %d underflows", ErrExpectedCashOutOfRange, a, b)
	}
	return a + b, nil
}

func subtractAmount(a, b int64) (int64, error) {
	if b > 0 && a < math.MinInt64+b {
		return 0, fmt.Errorf("%w: %d - %d underflows", ErrExpectedCashOutOfRange, a, b)
	}
	if b < 0 && a > math.MaxInt64+b {
		return 0, fmt.Errorf("%w: %d - %d overflows", ErrExpectedCashOutOfRange, a, b)
	}
	return a - b, nil
}
