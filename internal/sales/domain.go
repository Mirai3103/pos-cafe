// Package sales implements the Sales vertical slice. Phase 5A covers the
// Service Session lifecycle up to its commercial boundary: opening a Takeaway
// or Dine-in Session, maintaining Table assignments, and building the Order
// Draft. Commit, Payment, Submit, and closure land in 5B, 5C, and 5D.
package sales

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

// CapSalesOperate is the capability every Sales operation requires. It is
// already derived for MANAGER and CASHIER by auth.DeriveCapabilities.
const CapSalesOperate = "sales.operate"

// Idempotency action names, stored in idempotency_keys.action (VARCHAR(50)).
const (
	OpStartTakeawaySession  = "sales.start_takeaway_session"
	OpStartDineInSession    = "sales.start_dine_in_session"
	OpSetSessionTables      = "sales.set_session_tables"
	OpAddDraftItem          = "sales.add_draft_item"
	OpSetDraftItemQuantity  = "sales.set_draft_item_quantity"
	OpSetDraftItemSize      = "sales.set_draft_item_size"
	OpSetDraftItemNote      = "sales.set_draft_item_note"
	OpSetDraftItemModifiers = "sales.set_draft_item_modifiers"
	OpRemoveDraftItem       = "sales.remove_draft_item"
	OpGetServiceSession     = "sales.get_service_session"
	OpListServiceSessions   = "sales.list_service_sessions"
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
)

// Service Session states. 5A writes only StateActive; 5D writes StateClosed.
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

// Temporary: removed in Task 3 when errors.go defines these for real.
var (
	ErrInvalidPreparationNote   = errors.New("invalid preparation note")
	ErrInvalidQuantity          = errors.New("invalid quantity")
	ErrServiceSequenceExhausted = errors.New("service sequence exhausted")
)
