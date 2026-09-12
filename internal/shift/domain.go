// Package shift implements the Sales Shift vertical slice: the accountability
// window for the cashier station's cash fund, the Cash Movements that change
// its Expected Cash, and the read that reports both.
package shift

import (
	"fmt"
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
)

// Audit event types. Business events are UPPER_SNAKE_CASE and the denial event
// is lowercase dotted, matching the convention in internal/tables.
const (
	EventSalesShiftOpened     = "SALES_SHIFT_OPENED"
	EventCashMovementRecorded = "CASH_MOVEMENT_RECORDED"
	EventAuthorizationDenied  = "shift.authorization_denied"
)

// Sales Shift states. Phase 4 produces only StateOpen; StateClosed exists in
// the schema so Phase 5 adds a close command without a state-domain migration.
const (
	StateOpen   = "OPEN"
	StateClosed = "CLOSED"
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
	n := utf8.RuneCountInString(*note)
	if n < 1 {
		return fmt.Errorf("note cannot be empty")
	}
	if n > MaxNoteLength {
		return fmt.Errorf("note is %d characters, maximum is %d", n, MaxNoteLength)
	}
	return nil
}

// ComputeExpectedCash returns the Sales Shift's calculated cash responsibility.
//
// Phase 4 formula: Opening Float plus Pay Ins less Pay Outs. Phase 5 adds Cash
// Payments and subtracts Cash Refunds; until then this figure reflects fund
// movements only and is not a reconciliation figure. See ADR-008.
//
// The guard is symmetric because sustained Pay Outs can drive the partial
// Phase 4 figure negative. A total outside the bound indicates corrupt data,
// not a legitimate drawer balance.
func ComputeExpectedCash(openingFloatVND, payInVND, payOutVND int64) (int64, error) {
	total := openingFloatVND + payInVND - payOutVND
	if total > MaxAmountVND || total < -MaxAmountVND {
		return 0, fmt.Errorf("%w: expected cash %d is outside [%d, %d]",
			ErrExpectedCashOutOfRange, total, -MaxAmountVND, MaxAmountVND)
	}
	return total, nil
}
