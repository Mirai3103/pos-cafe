// Package sales implements the Sales vertical slice. Phase 5A covers the
// Service Session lifecycle up to its commercial boundary: opening a Takeaway
// or Dine-in Session, maintaining Table assignments, and building the Order
// Draft. Commit, Payment, Submit, and closure land in 5B, 5C, and 5D.
package sales

import (
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
)

// CapSalesOperate is the capability every Sales operation requires. It is
// already derived for MANAGER and CASHIER by auth.DeriveCapabilities.
const CapSalesOperate = "sales.operate"

// Idempotency action names, stored in idempotency_keys.action (VARCHAR(50)).
const (
	OpStartTakeawaySession      = "sales.start_takeaway_session"
	OpStartDineInSession        = "sales.start_dine_in_session"
	OpSetSessionTables          = "sales.set_session_tables"
	OpAddDraftItem              = "sales.add_draft_item"
	OpSetDraftItemQuantity      = "sales.set_draft_item_quantity"
	OpSetDraftItemSize          = "sales.set_draft_item_size"
	OpSetDraftItemNote          = "sales.set_draft_item_note"
	OpSetDraftItemModifiers     = "sales.set_draft_item_modifiers"
	OpRemoveDraftItem           = "sales.remove_draft_item"
	OpGetServiceSession         = "sales.get_service_session"
	OpListServiceSessions       = "sales.list_service_sessions"
	OpCommitOrderDraft          = "sales.commit_order_draft"
	OpStartNewOrderDraft        = "sales.start_new_order_draft"
	OpSetOrderDraftCheckTarget  = "sales.set_order_draft_check_target"
	OpSubmitOrder               = "sales.submit_order"
	OpCloseServiceSession       = "sales.close_service_session"
	OpGetCompletedSale          = "sales.get_completed_sale"
	OpGetCompletedSaleBySession = "sales.get_completed_sale_by_session"
)

// Audit event types. Business events are UPPER_SNAKE_CASE and the denial event
// is lowercase dotted, matching internal/tables and internal/shift.
const (
	EventServiceSessionStarted       = "SERVICE_SESSION_STARTED"
	EventDineInServiceSessionStarted = "DINE_IN_SERVICE_SESSION_STARTED"
	EventTableAssignmentCreated      = "TABLE_ASSIGNMENT_CREATED"
	EventTableAssignmentReleased     = "TABLE_ASSIGNMENT_RELEASED"
	EventDraftItemAdded              = "ORDER_DRAFT_ITEM_ADDED"
	EventDraftItemQuantitySet        = "ORDER_DRAFT_ITEM_QUANTITY_SET"
	EventDraftItemSizeSet            = "ORDER_DRAFT_ITEM_SIZE_SET"
	EventDraftItemNoteSet            = "ORDER_DRAFT_ITEM_NOTE_SET"
	EventDraftItemModifiersSet       = "ORDER_DRAFT_ITEM_MODIFIERS_SET"
	EventDraftItemRemoved            = "ORDER_DRAFT_ITEM_REMOVED"
	// EventDraftItemsMerged has no canonical counterpart. A composition edit
	// that absorbs one row into another changes a visible quantity that no
	// command asked for; without its own event the audit trail cannot explain
	// the change.
	EventDraftItemsMerged    = "ORDER_DRAFT_ITEMS_MERGED"
	EventAuthorizationDenied = "sales.authorization_denied"
	// EventOrderDraftCommitted drops the canonical TAKEAWAY_CHECKOUT_ prefix.
	// Commit is mode-agnostic — the canonical source runs one handler for
	// Dine-in too — and 5A already dropped that prefix throughout.
	EventOrderDraftCommitted      = "ORDER_DRAFT_COMMITTED"
	EventOrderDraftStarted        = "ORDER_DRAFT_STARTED"
	EventOrderDraftCheckTargetSet = "ORDER_DRAFT_CHECK_TARGET_SET"
	// EventOrderSubmitted drops the canonical TAKEAWAY_ORDER_ prefix, for the
	// same reason EventOrderDraftCommitted did: Submit is mode-agnostic.
	EventOrderSubmitted       = "ORDER_SUBMITTED"
	EventServiceSessionClosed = "SERVICE_SESSION_CLOSED"
)

// Service Session states. 5A writes only StateActive; 5D writes StateClosed.
//
// There is no separate literal for the Shift's open state: whether a Shift is
// open is answered by reading sales_shifts, not by comparing a string, so no
// command needs the value.
const (
	StateActive = "ACTIVE"
	StateClosed = "CLOSED"
)

// Order Draft states. 5A writes only DraftStateEditable; 5B writes
// DraftStateCommitted.
const (
	DraftStateEditable  = "EDITABLE"
	DraftStateCommitted = "COMMITTED"
)

// Service modes.
const (
	ModeTakeaway = "TAKEAWAY"
	ModeDineIn   = "DINE_IN"
)

// Quantity bounds. The canonical maximum is floor(MAX_SAFE_INTEGER /
// MAX_MENU_PRICE_VND), an artifact of JavaScript integer precision rather than
// a business rule. Go computes line totals in int64 and has no such hazard, so
// this is a deliberate data-entry guard instead. See the spec, section 6.7.
const (
	MinQuantity int32 = 1
	MaxQuantity int32 = 9999
)

// MaxPreparationNoteLength is counted in Unicode code points so Go agrees with
// the database char_length check.
const MaxPreparationNoteLength = 200

// MaxServiceSequence keeps a formatted Service Number at six characters, which
// the service_session_number_valid check constraint requires.
const MaxServiceSequence int32 = 99999

// NormalizePreparationNote trims a note, converts an empty result to SQL NULL,
// and rejects anything longer than MaxPreparationNoteLength code points.
func NormalizePreparationNote(v *string) (sql.NullString, error) {
	if v == nil {
		return sql.NullString{}, nil
	}
	trimmed := strings.TrimSpace(*v)
	if trimmed == "" {
		return sql.NullString{}, nil
	}
	if utf8.RuneCountInString(trimmed) > MaxPreparationNoteLength {
		return sql.NullString{}, fmt.Errorf("%w: preparation_note exceeds %d characters",
			ErrInvalidPreparationNote, MaxPreparationNoteLength)
	}
	return sql.NullString{String: trimmed, Valid: true}, nil
}

// ValidateQuantity enforces the draft item quantity bounds.
func ValidateQuantity(q int32) error {
	if q < MinQuantity || q > MaxQuantity {
		return fmt.Errorf("%w: quantity must be between %d and %d",
			ErrInvalidQuantity, MinQuantity, MaxQuantity)
	}
	return nil
}

// SortedUUIDs returns a sorted copy, leaving the caller's slice untouched.
// Selection order carries meaning for audit events and assignment sequence, so
// callers must never sort in place.
func SortedUUIDs(ids []uuid.UUID) []uuid.UUID {
	out := make([]uuid.UUID, len(ids))
	copy(out, ids)
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

// HasDuplicateUUIDs reports whether ids contains the same value twice.
func HasDuplicateUUIDs(ids []uuid.UUID) bool {
	seen := make(map[uuid.UUID]struct{}, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			return true
		}
		seen[id] = struct{}{}
	}
	return false
}

// ModifierKeyFor builds the denormalized composition key: the option ids
// sorted and comma joined. The empty set is the empty string.
func ModifierKeyFor(ids []uuid.UUID) string {
	if len(ids) == 0 {
		return ""
	}
	sorted := SortedUUIDs(ids)
	parts := make([]string, len(sorted))
	for i, id := range sorted {
		parts[i] = id.String()
	}
	return strings.Join(parts, ",")
}

// FormatServiceNumber renders a Shift-scoped sequence as the operational
// label. It errors rather than producing a seventh character, so an overflow
// surfaces here instead of as an unmapped 23514 at insert time.
func FormatServiceNumber(seq int32) (string, error) {
	if seq < 1 || seq > MaxServiceSequence {
		return "", fmt.Errorf("%w: service sequence %d is outside 1..%d",
			ErrServiceSequenceExhausted, seq, MaxServiceSequence)
	}
	return fmt.Sprintf("S%05d", seq), nil
}

// Check states. 5B writes only CheckStateOpen; 5C writes the other two.
// The domain ships complete so the CURRENT_UNPAID target query's state filter
// is meaningful rather than vacuous. See ADR-014.
const (
	CheckStateOpen    = "OPEN"
	CheckStateSettled = "SETTLED"
	CheckStateMerged  = "MERGED"
)

// Order Draft Check targets. The target belongs to the draft, not the
// Session, and resets to CheckTargetCurrentUnpaid when a new draft opens.
const (
	CheckTargetCurrentUnpaid = "CURRENT_UNPAID"
	CheckTargetNewCheck      = "NEW_CHECK"
)

// ValidateCheckTarget rejects a target outside the domain.
func ValidateCheckTarget(target string) error {
	switch target {
	case CheckTargetCurrentUnpaid, CheckTargetNewCheck:
		return nil
	default:
		return fmt.Errorf("%w: check_target must be %s or %s",
			ErrInvalidCheckTarget, CheckTargetCurrentUnpaid, CheckTargetNewCheck)
	}
}

// LineTotal computes quantity * unitPriceVND with an explicit overflow guard.
//
// The canonical implementation guards against Number.MAX_SAFE_INTEGER because
// JavaScript loses integer precision beyond it. Go has no such limit, but its
// integer arithmetic wraps silently, so money arithmetic that does not check
// can produce a negative total without failing. The bound is int64's, not a
// business ceiling. See ADR-013.
func LineTotal(quantity int32, unitPriceVND int64) (int64, error) {
	if quantity < MinQuantity || quantity > MaxQuantity {
		return 0, fmt.Errorf("%w: quantity %d out of range", ErrLineTotalOutOfRange, quantity)
	}
	if unitPriceVND <= 0 {
		return 0, fmt.Errorf("%w: unit price %d is not positive", ErrLineTotalOutOfRange, unitPriceVND)
	}
	if unitPriceVND > math.MaxInt64/int64(quantity) {
		return 0, fmt.Errorf("%w: %d x %d overflows", ErrLineTotalOutOfRange, quantity, unitPriceVND)
	}
	return int64(quantity) * unitPriceVND, nil
}

// AddCharge accumulates a Check's charge with an explicit overflow guard.
func AddCharge(totalVND, deltaVND int64) (int64, error) {
	if deltaVND > 0 && totalVND > math.MaxInt64-deltaVND {
		return 0, fmt.Errorf("%w: %d + %d overflows", ErrCheckChargeOutOfRange, totalVND, deltaVND)
	}
	if deltaVND < 0 && totalVND < math.MinInt64-deltaVND {
		return 0, fmt.Errorf("%w: %d + %d underflows", ErrCheckChargeOutOfRange, totalVND, deltaVND)
	}
	sum := totalVND + deltaVND
	if sum < 0 {
		return 0, fmt.Errorf("%w: %d is negative", ErrCheckChargeOutOfRange, sum)
	}
	return sum, nil
}

const (
	OpPayCash     = "sales.pay_cash"
	OpPayManualQR = "sales.pay_manual_qr"
	OpSplitCheck  = "sales.split_check"
	OpMergeChecks = "sales.merge_checks"
)

const (
	EventCashPaymentRecorded     = "CASH_PAYMENT_RECORDED"
	EventManualQRPaymentRecorded = "MANUAL_QR_PAYMENT_RECORDED"
	EventCheckSettled            = "CHECK_SETTLED"
	EventCheckSplit              = "CHECK_SPLIT"
	EventCheckMerged             = "CHECK_MERGED"
)

// Payment methods. The canonical domain has exactly these two; Card is named
// in MIGRATE_PLAN's superseded sketch but exists nowhere in the canonical
// model, so it is not declared.
const (
	PaymentMethodCash     = "CASH"
	PaymentMethodManualQR = "MANUAL_QR"
)

// Refund methods. A Refund is returned through the original Payment's method,
// so the two share their names but stay separate literals: a future method
// could exist for one and not the other.
const (
	RefundMethodCash     = "CASH"
	RefundMethodManualQR = "MANUAL_QR"
)

// Refund states are derived from completion evidence, never stored: a Refund
// without a completion is PENDING, one with a completion is COMPLETED. A
// Manual QR Refund stays PENDING until staff confirm the outbound transfer.
const (
	RefundStatePending   = "PENDING"
	RefundStateCompleted = "COMPLETED"
)

// Split destinations.
const (
	SplitDestinationNewCheck      = "NEW_CHECK"
	SplitDestinationExistingCheck = "EXISTING_CHECK"
)

// MaxTransactionReferenceLength bounds a Manual QR Payment's bank reference.
const MaxTransactionReferenceLength = 100

// SubtractCharge reduces a running charge, refusing to go negative.
//
// Go's integer arithmetic wraps silently, so money arithmetic that does not
// check is money arithmetic that can produce a positive total out of an
// underflow. The check is one comparison.
func SubtractCharge(totalVND, deltaVND int64) (int64, error) {
	result := totalVND - deltaVND
	if deltaVND > 0 && result > totalVND {
		return 0, fmt.Errorf("%w: subtracting %d from %d underflows",
			ErrCheckChargeOutOfRange, deltaVND, totalVND)
	}
	if result < 0 {
		return 0, fmt.Errorf("%w: subtracting %d from %d is negative",
			ErrCheckChargeOutOfRange, deltaVND, totalVND)
	}
	return result, nil
}

// ChangeDue is the cash handed back: tendered less applied.
//
// A cashier cannot hand back money they were not given, so under-tender is a
// business rejection rather than an arithmetic one.
func ChangeDue(tenderedVND, appliedVND int64) (int64, error) {
	if tenderedVND < appliedVND {
		return 0, fmt.Errorf("%w: tendered %d is below applied %d",
			ErrInsufficientCashTendered, tenderedVND, appliedVND)
	}
	return tenderedVND - appliedVND, nil
}

// SettlesCheck reports whether a resulting balance closes the Check.
//
// A Check settles when nothing is owed, including a zero-charge Check and one
// that carries a pending Refund: state records whether customer debt is
// covered, while money owed back is a separate obligation that closure, not
// state, enforces. See ADR-017.
func SettlesCheck(balanceVND int64) bool { return balanceVND == 0 }

// ValidateTransactionReference trims a Manual QR bank reference and bounds it.
// A reference that is empty after trimming is treated as absent, matching the
// canonical `command.transactionReference?.trim() || null`.
func ValidateTransactionReference(ref *string) (*string, error) {
	if ref == nil {
		return nil, nil
	}
	trimmed := strings.TrimSpace(*ref)
	if trimmed == "" {
		return nil, nil
	}
	if len([]rune(trimmed)) > MaxTransactionReferenceLength {
		return nil, fmt.Errorf("%w: transaction_reference is %d characters, maximum is %d",
			response.ErrInvalid, len([]rune(trimmed)), MaxTransactionReferenceLength)
	}
	return &trimmed, nil
}

// Preparation Unit states. ADR-028 declares the complete canonical domain
// although 5D writes only the first four: 5A shipped a guessed partial domain
// for service_sessions.state and had to correct it, and 5C responded with the
// complete-domain precedent this follows.
const (
	UnitStateQueued        = "QUEUED"
	UnitStateInPreparation = "IN_PREPARATION"
	UnitStateReady         = "READY"
	UnitStateFulfilled     = "FULFILLED"
	UnitStateCancelled     = "CANCELLED"
	UnitStateWasted        = "WASTED"
)

// IsTerminalUnitState reports whether a Preparation Unit has reached a state
// it cannot leave. Closure requires every unit to be terminal. Cancelled and
// Wasted are unreachable in Phase 5 but accepted here, so the policy is
// written once against the complete domain rather than re-edited in Phase 6.
func IsTerminalUnitState(state string) bool {
	switch state {
	case UnitStateFulfilled, UnitStateCancelled, UnitStateWasted:
		return true
	default:
		return false
	}
}

// ModeRequiresSettlementBeforeSubmit reports whether a Service Session's mode
// forbids sending work to the bar while a Check still carries a balance.
//
// Takeaway does: nothing is prepared for a customer who has not paid and may
// walk. Dine-in does not: a seated customer's drinks go to the bar long before
// the bill is settled, so both Commit -> Submit -> Payment and Commit ->
// Payment -> Submit are valid service.
func ModeRequiresSettlementBeforeSubmit(mode string) bool {
	return mode == ModeTakeaway
}

// --- Phase 6C: Comp ---

// OpCompWaste is the idempotency action name, stored in
// idempotency_keys.action (VARCHAR(50)).
const OpCompWaste = "sales.comp_waste"

// Correction scopes. A LIVE_CHECK adjustment changes the active Session's
// Check charge; a POST_SALE adjustment links to a Completed Sale and never
// rewrites its snapshot (spec §2, §6.3).
const (
	CompScopeLiveCheck = "LIVE_CHECK"
	CompScopePostSale  = "POST_SALE"
)

// ChargeAdjustmentKindComp is the one Charge Adjustment kind this package
// writes. ADR-041 makes charge reduction append-only: the adjustment names its
// immutable source and never edits it.
const ChargeAdjustmentKindComp = "COMP"

// Comp reason catalog (spec §2). Every operation keeps its own allowlist; the
// migration 000014 constraint enforces the same set at the database boundary.
const (
	CompReasonCafeError       = "CAFE_ERROR"
	CompReasonQualityFailure  = "QUALITY_FAILURE"
	CompReasonServiceRecovery = "SERVICE_RECOVERY"
	CompReasonOther           = "OTHER"
)

var compReasons = []string{
	CompReasonCafeError, CompReasonQualityFailure, CompReasonServiceRecovery, CompReasonOther,
}

// MaxCorrectionNoteRunes is the inclusive upper bound for a present correction
// note, counted in Unicode code points so Go agrees with the database
// char_length check.
const MaxCorrectionNoteRunes = 500

// Phase 6C business audit event types (spec §15). CHECK_SETTLED already exists
// above: the settlement fact is the same event whichever command produced it.
const (
	EventCheckChargeAdjusted = "CHECK_CHARGE_ADJUSTED"
	EventSalesCompRecorded   = "SALES_COMP_RECORDED"
)

// NormalizeCompNote trims surrounding whitespace from an optional Comp note
// and collapses a blank note to nil. Callers normalize BEFORE validating and
// BEFORE building the fingerprint, so replays of differently padded input stay
// equal.
func NormalizeCompNote(note *string) *string {
	if note == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*note)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// ValidateCompReason checks a Comp reason against the Comp catalog.
func ValidateCompReason(reason string) error {
	for _, allowed := range compReasons {
		if reason == allowed {
			return nil
		}
	}
	return fmt.Errorf("%w: %q is not a valid comp reason", response.ErrInvalid, reason)
}

// ValidateCompNote validates an already-normalized optional note: a present
// note is 1 through MaxCorrectionNoteRunes code points, and the OTHER reason
// requires one.
func ValidateCompNote(reason string, note *string) error {
	if note != nil {
		runes := utf8.RuneCountInString(*note)
		if runes < 1 || runes > MaxCorrectionNoteRunes {
			return fmt.Errorf("%w: a note must be 1 through %d characters",
				response.ErrInvalid, MaxCorrectionNoteRunes)
		}
		return nil
	}
	if reason == CompReasonOther {
		return fmt.Errorf("%w: the %s reason requires a note", response.ErrInvalid, CompReasonOther)
	}
	return nil
}

// ValidateManagerApprovalInput checks the shape of one inline Manager Approval
// before any request id is consumed. Meaning failures — a wrong PIN, a
// disabled identity, a missing role or capability — stay inside the executor's
// transaction and collapse to one client-visible denial.
func ValidateManagerApprovalInput(in ManagerApprovalInput) error {
	if auth.NormalizeLoginCode(in.ApproverLoginCode) == "" {
		return fmt.Errorf("%w: approver_login_code is required", response.ErrInvalid)
	}
	if err := auth.ValidatePinFormat(in.ManagerPIN); err != nil {
		return fmt.Errorf("%w: manager_pin %s", response.ErrInvalid, err.Error())
	}
	return nil
}

// ValidateCompWasteCommand validates a Comp at the boundary, before any
// transaction and before the credential values are copied into the executor's
// ApprovalSpec. The note arrives already normalized.
func ValidateCompWasteCommand(cmd CompWasteCommand, note *string) error {
	if cmd.RequestID == uuid.Nil {
		return fmt.Errorf("%w: request_id is required", response.ErrInvalid)
	}
	if cmd.WasteID == uuid.Nil {
		return fmt.Errorf("%w: waste_id is required", response.ErrInvalid)
	}
	if err := ValidateCompReason(cmd.Reason); err != nil {
		return err
	}
	if err := ValidateCompNote(cmd.Reason, note); err != nil {
		return err
	}
	return ValidateManagerApprovalInput(cmd.ManagerApproval)
}
