// Package preparation implements the Preparation vertical slice.
//
// Phase 5D creates it holding exactly one command: advancing a Preparation
// Unit along the linear chain from queued to fulfilled. Cancellation, Waste,
// Remake, State Correction, Preparation Alerts, and the Preparation Queue
// reads are Phase 6 and grow into this package.
//
// The boundary with internal/sales (ADR-024): internal/sales creates
// Preparation Units at Submit and reads their state during closure; this
// package owns every state transition. sqlc generates one shared package, so
// the boundary is enforced in review rather than by the compiler.
package preparation

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
)

// CapPreparationOperate is the capability the advance command requires. It is
// already derived for MANAGER and BARISTA by auth.DeriveCapabilities, so a
// Barista can advance units but cannot submit or close, and a Cashier the
// reverse. No capability table change was needed for Phase 5D.
const CapPreparationOperate = "preparation.operate"

// OpAdvanceUnit is the idempotency action name, stored in
// idempotency_keys.action (VARCHAR(50)).
const OpAdvanceUnit = "preparation.advance_unit"

// OpReadActiveQueue names the active-queue read for denial-audit evidence.
// Reads are not idempotency-keyed, so the name appears only in audit details.
const OpReadActiveQueue = "preparation.read_active_queue"

// EventPreparationUnitAdvanced is the audit event type.
const EventPreparationUnitAdvanced = "PREPARATION_UNIT_ADVANCED"

const (
	OpBulkAdvance = "preparation.bulk_advance"

	BulkStatusAdvanced = "ADVANCED"
	BulkStatusFailed   = "FAILED"

	BulkCodeUnitNotFound      = "UNIT_NOT_FOUND"
	BulkCodeInvalidTransition = "INVALID_TRANSITION"
)

// Preparation Unit states. 5D writes only the first four; StateCancelled and
// StateWasted arrive with Phase 6's commands (ADR-028).
const (
	StateQueued        = "QUEUED"
	StateInPreparation = "IN_PREPARATION"
	StateReady         = "READY"
	StateFulfilled     = "FULFILLED"
	StateCancelled     = "CANCELLED"
	StateWasted        = "WASTED"
)

// advanceChain is the legal graph, taken unchanged from the canonical source.
// It is strictly linear: no step may be skipped, and no state may be left once
// it is terminal.
var advanceChain = map[string]string{
	StateQueued:        StateInPreparation,
	StateInPreparation: StateReady,
	StateReady:         StateFulfilled,
}

// NextState returns the single state that may follow the given one, and
// whether any may.
func NextState(from string) (string, bool) {
	next, ok := advanceChain[from]
	return next, ok
}

// IsLegalAdvance reports whether `to` is the immediate successor of `from`.
func IsLegalAdvance(from, to string) bool {
	next, ok := advanceChain[from]
	return ok && next == to
}

// IsAdvanceTarget reports whether a state can be requested as an advance
// target. Queued is where a unit starts, not somewhere it can be sent;
// Cancelled and Wasted are reached by their own Phase 6 commands.
func IsAdvanceTarget(state string) bool {
	switch state {
	case StateInPreparation, StateReady, StateFulfilled:
		return true
	default:
		return false
	}
}

// --- Phase 6B: Corrections & Recovery ---

// Phase 6B idempotency operation names (spec §7.1), stored in
// idempotency_keys.action (VARCHAR(50)) alongside OpAdvanceUnit and
// OpBulkAdvance.
const (
	OpAcknowledgeAlert = "preparation.acknowledge_alert"
	OpWasteUnit        = "preparation.waste_unit"
	OpRemakeUnit       = "preparation.remake_unit"
	OpCorrectState     = "preparation.correct_state"
)

// Phase 6B business audit event types (spec §11). Actor identity, Staff
// Access Session, and occurrence time are first-class audit columns, and no
// event ever carries a PIN or PIN hash.
const (
	EventPreparationAlertCreated      = "PREPARATION_ALERT_CREATED"
	EventPreparationAlertAcknowledged = "PREPARATION_ALERT_ACKNOWLEDGED"
	EventPreparationUnitWasted        = "PREPARATION_UNIT_WASTED"
	EventPreparationRemakeCreated     = "PREPARATION_REMAKE_CREATED"
	EventPreparationStateCorrected    = "PREPARATION_STATE_CORRECTED"
)

// Preparation Alert kinds. The schema has admitted all three since the first
// migration, but Phase 6B writes only AlertKindWaste — Cancellation and
// Change alerts are Phase 6C and must arrive without new kinds.
const (
	AlertKindCancellation = "CANCELLATION"
	AlertKindChange       = "CHANGE"
	AlertKindWaste        = "WASTE"
)

// Preparation Unit priorities. STANDARD is the backfilled default for every
// original unit; only linked Remakes are REMAKE. Only active REMAKE units
// take the queue's priority lane.
const (
	PriorityStandard = "STANDARD"
	PriorityRemake   = "REMAKE"
)

// Reasons for Waste, Remake, and State Correction facts (spec §5). Each
// operation admits only its own catalog; the migration 000013 constraints
// enforce the same sets at the database boundary.
const (
	ReasonPreparationError     = "PREPARATION_ERROR"
	ReasonQualityFailure       = "QUALITY_FAILURE"
	ReasonCustomerRequest      = "CUSTOMER_REQUEST"
	ReasonOther                = "OTHER"
	ReasonStateRecordedInError = "STATE_RECORDED_IN_ERROR"
)

var (
	wasteReasons      = []string{ReasonPreparationError, ReasonQualityFailure, ReasonCustomerRequest, ReasonOther}
	remakeReasons     = []string{ReasonPreparationError, ReasonQualityFailure, ReasonOther}
	correctionReasons = []string{ReasonStateRecordedInError, ReasonOther}
)

// MaxNoteRunes is the inclusive upper bound for a present Waste, Remake, or
// alert note, counted in Unicode code points.
const MaxNoteRunes = 500

// MaxCorrectionUnits is the inclusive upper bound of one State Correction
// batch.
const MaxCorrectionUnits = 50

// NormalizeCorrectionNote trims surrounding whitespace from an optional
// request note and collapses blank notes to nil. Callers normalize BEFORE
// validating and BEFORE building fingerprints, so a fingerprint stands for
// the normalized input and replays of differently padded input stay equal.
func NormalizeCorrectionNote(note *string) *string {
	if note == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*note)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// validateReason reports whether reason is a member of the given catalog,
// wrapping ErrInvalidReason so HTTP mapping needs no string inspection.
func validateReason(reason string, catalog []string) error {
	for _, allowed := range catalog {
		if reason == allowed {
			return nil
		}
	}
	return fmt.Errorf("%w: %q is not a valid reason", ErrInvalidReason, reason)
}

// ValidateWasteReason checks a Waste reason against the Waste catalog.
func ValidateWasteReason(reason string) error {
	return validateReason(reason, wasteReasons)
}

// ValidateRemakeReason checks a Remake reason against the Remake catalog,
// which is narrower than Waste's.
func ValidateRemakeReason(reason string) error {
	return validateReason(reason, remakeReasons)
}

// ValidateCorrectionReason checks a State Correction reason against the
// correction catalog.
func ValidateCorrectionReason(reason string) error {
	return validateReason(reason, correctionReasons)
}

// ValidateCorrectionNote validates an optional note for any Waste, Remake, or
// alert reason: a present note is 1 through MaxNoteRunes code points, and the
// OTHER reason requires one. Callers normalize first, so a blank note has
// already become nil and cannot satisfy OTHER.
func ValidateCorrectionNote(reason string, note *string) error {
	if note != nil {
		runes := utf8.RuneCountInString(*note)
		if runes < 1 || runes > MaxNoteRunes {
			return fmt.Errorf("%w: a note must be 1 through %d characters", ErrInvalidNote, MaxNoteRunes)
		}
		return nil
	}
	if reason == ReasonOther {
		return fmt.Errorf("%w: the %s reason requires a note", ErrInvalidNote, ReasonOther)
	}
	return nil
}

// RequiredPriorState returns the single state a unit must currently be in for
// a correction to target the given state, and whether the target is a
// correction target at all. The reverse chain is strictly one step —
// QUEUED <- IN_PREPARATION <- READY <- FULFILLED — and terminal or
// exceptional states (CANCELLED, WASTED, FULFILLED) are never targets.
func RequiredPriorState(target string) (string, bool) {
	switch target {
	case StateQueued:
		return StateInPreparation, true
	case StateInPreparation:
		return StateReady, true
	case StateReady:
		return StateFulfilled, true
	default:
		return "", false
	}
}

// ValidateCorrectionSelection validates a State Correction's unit selection:
// 1 through MaxCorrectionUnits non-zero, unique ids. Duplicates are rejected
// rather than silently deduplicated, and the input is never reordered or
// mutated — request order is preserved for the response, while lock ordering
// is a separate sorted copy owned by the command.
func ValidateCorrectionSelection(ids []uuid.UUID) error {
	if len(ids) < 1 || len(ids) > MaxCorrectionUnits {
		return fmt.Errorf("%w: preparation_unit_ids must contain 1 through %d ids",
			response.ErrInvalid, MaxCorrectionUnits)
	}
	seen := make(map[uuid.UUID]struct{}, len(ids))
	for _, id := range ids {
		if id == uuid.Nil {
			return fmt.Errorf("%w: preparation_unit_ids must not contain a zero UUID", response.ErrInvalid)
		}
		if _, duplicate := seen[id]; duplicate {
			return fmt.Errorf("%w: preparation_unit_ids must not repeat %s", response.ErrInvalid, id)
		}
		seen[id] = struct{}{}
	}
	return nil
}

// ValidateCorrectStateCommand validates a State Correction at the boundary,
// before any transaction: normalization of the note happens inside the note
// rule, so validation always sees normalized input. Boundary failures (shape,
// selection, target) wrap response.ErrInvalid; meaning failures (reason,
// note) carry their own sentinels. The PIN is checked for shape only — a
// well-formed PIN that fails self-authentication is denied later, inside the
// mutation, as ErrInvalidManagerPIN.
func ValidateCorrectStateCommand(cmd CorrectStateCommand) error {
	if cmd.RequestID == uuid.Nil {
		return fmt.Errorf("%w: request_id is required", response.ErrInvalid)
	}
	if err := ValidateCorrectionSelection(cmd.PreparationUnitIDs); err != nil {
		return err
	}
	if _, ok := RequiredPriorState(cmd.TargetState); !ok {
		return fmt.Errorf("%w: %q is not a correction target", response.ErrInvalid, cmd.TargetState)
	}
	if err := ValidateCorrectionReason(cmd.Reason); err != nil {
		return err
	}
	if err := ValidateCorrectionNote(cmd.Reason, NormalizeCorrectionNote(cmd.Note)); err != nil {
		return err
	}
	if err := auth.ValidatePinFormat(cmd.ManagerPIN); err != nil {
		return fmt.Errorf("%w: manager_pin %s", response.ErrInvalid, err.Error())
	}
	return nil
}
