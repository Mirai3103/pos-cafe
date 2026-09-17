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
