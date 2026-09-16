package preparation

import (
	"time"

	"github.com/google/uuid"
)

// UnitModifierResponse is one frozen Modifier Option on a Preparation Unit.
// It carries no price: the bar needs to know what to make, not what it cost.
type UnitModifierResponse struct {
	GroupName  string `json:"group_name"`
	OptionName string `json:"option_name"`
}

// UnitResponse is one Preparation Unit as the bar sees it.
type UnitResponse struct {
	ID              uuid.UUID              `json:"id"`
	OrderItemID     uuid.UUID              `json:"order_item_id"`
	UnitNumber      int32                  `json:"unit_number"`
	State           string                 `json:"state"`
	ServiceNumber   string                 `json:"service_number"`
	CategoryName    string                 `json:"category_name"`
	ItemName        string                 `json:"item_name"`
	SizeName        *string                `json:"size_name"`
	Modifiers       []UnitModifierResponse `json:"modifiers"`
	PreparationNote *string                `json:"preparation_note"`
	QueuedAt        time.Time              `json:"queued_at"`
	InPreparationAt *time.Time             `json:"in_preparation_at"`
	// Priority is STANDARD for every original unit and REMAKE for linked
	// replacements (spec §5.1).
	Priority string `json:"priority"`
	// RemakeOfPreparationUnitID links a Remake to the wasted source unit it
	// replaces; nil for originals.
	RemakeOfPreparationUnitID *uuid.UUID `json:"remake_of_preparation_unit_id"`
}

// QueueUnitResponse is one active Preparation Unit on the bar's queue.
//
// Like UnitResponse it carries the commercial snapshot by name only — no
// prices, no Checks, no Payments, no staff credentials — and adds the two
// facts a queue display needs beyond the single-unit view: how many physical
// units the Order Item totals (including terminal siblings, so a barista can
// see this drink is one of three) and which Tables the round currently sits
// on, projected from the Session's unreleased assignments at read time.
type QueueUnitResponse struct {
	ID                 uuid.UUID              `json:"id"`
	OrderItemID        uuid.UUID              `json:"order_item_id"`
	OrderItemUnitCount int32                  `json:"order_item_unit_count"`
	UnitNumber         int32                  `json:"unit_number"`
	State              string                 `json:"state"`
	ServiceNumber      string                 `json:"service_number"`
	TableNames         []string               `json:"table_names"`
	CategoryName       string                 `json:"category_name"`
	ItemName           string                 `json:"item_name"`
	SizeName           *string                `json:"size_name"`
	Modifiers          []UnitModifierResponse `json:"modifiers"`
	PreparationNote    *string                `json:"preparation_note"`
	QueuedAt           time.Time              `json:"queued_at"`
	InPreparationAt    *time.Time             `json:"in_preparation_at"`
	// Priority and the Remake link mirror UnitResponse (spec §6.1): only
	// active Remakes take the queue's priority lane.
	Priority                  string     `json:"priority"`
	RemakeOfPreparationUnitID *uuid.UUID `json:"remake_of_preparation_unit_id"`
}

// QueueResponse is the whole active queue as the bar display reads it.
//
// ObservedAt is the database clock at the read, and Units holds every unit in
// the active states QUEUED, IN_PREPARATION, and READY, ordered by queued_at
// then id. Reading mutates nothing.
type QueueResponse struct {
	ObservedAt time.Time           `json:"observed_at"`
	Units      []QueueUnitResponse `json:"units"`
}

// AdvanceUnitCommand moves a Preparation Unit one step along its chain.
//
// TargetState is explicit rather than the command meaning "advance one step".
// A bar display can be seconds stale and two baristas can act on the same unit
// at once; with an explicit target the loser of that race gets
// INVALID_TRANSITION and re-reads, where an implicit "next" would silently
// push the unit one state further than either person intended.
type AdvanceUnitCommand struct {
	RequestID   uuid.UUID `json:"request_id" validate:"required"`
	TargetState string    `json:"target_state" validate:"required"`
	UnitID      uuid.UUID `json:"-"`
}

type BulkAdvanceCommand struct {
	RequestID          uuid.UUID   `json:"request_id"`
	PreparationUnitIDs []uuid.UUID `json:"preparation_unit_ids"`
	TargetState        string      `json:"target_state"`
}

type BulkAdvanceOutcome struct {
	PreparationUnitID uuid.UUID     `json:"preparation_unit_id"`
	Status            string        `json:"status"`
	Unit              *UnitResponse `json:"unit,omitempty"`
	Code              string        `json:"code,omitempty"`
}

type BulkAdvanceResponse struct {
	TargetState string               `json:"target_state"`
	Outcomes    []BulkAdvanceOutcome `json:"outcomes"`
}

// --- Phase 6B: Corrections & Recovery contracts ---

// AcknowledgeAlertCommand acknowledges one Preparation Alert. The path
// supplies the Alert id; the body carries only the idempotency key. The
// command never changes unit state or financial meaning (spec §7.3).
type AcknowledgeAlertCommand struct {
	RequestID uuid.UUID `json:"request_id"`
	AlertID   uuid.UUID `json:"-"`
}

// AlertResponse is one Preparation Alert as a mutation returns it: the
// created WASTE alert inside a Waste result, or the acknowledged alert in an
// acknowledgment result. Queue reads use QueueAlertResponse, which adds the
// unit identity projected at read time.
type AlertResponse struct {
	ID                            uuid.UUID  `json:"id"`
	PreparationUnitID             uuid.UUID  `json:"preparation_unit_id"`
	Kind                          string     `json:"kind"`
	Reason                        string     `json:"reason"`
	Note                          *string    `json:"note"`
	CreatedAt                     time.Time  `json:"created_at"`
	AcknowledgedByStaffIdentityID *uuid.UUID `json:"acknowledged_by_staff_identity_id"`
	AcknowledgedAt                *time.Time `json:"acknowledged_at"`
}

// WasteResponse is the result of wasting one Preparation Unit: the Waste
// fact's identity and meaning, the recorded state change, and the
// unacknowledged WASTE alert created alongside it (spec §7.2).
type WasteResponse struct {
	ID                uuid.UUID     `json:"id"`
	PreparationUnitID uuid.UUID     `json:"preparation_unit_id"`
	PriorState        string        `json:"prior_state"`
	ResultingState    string        `json:"resulting_state"`
	Reason            string        `json:"reason"`
	Note              *string       `json:"note"`
	OccurredAt        time.Time     `json:"occurred_at"`
	Alert             AlertResponse `json:"alert"`
}

// WasteUnitCommand wastes one Preparation Unit. The path supplies the unit
// id; the reason must be in the Waste catalog and the optional note is
// normalized before validation (spec §7.2).
type WasteUnitCommand struct {
	RequestID uuid.UUID `json:"request_id"`
	Reason    string    `json:"reason"`
	Note      *string   `json:"note"`
	UnitID    uuid.UUID `json:"-"`
}

// RemakeResponse is the result of remaking one Waste: the Remake fact's
// identity and meaning plus the replacement unit as the bar now sees it —
// a fresh QUEUED unit with the next number of the same Order Item and the
// source's immutable preparation snapshot (spec §7.4).
type RemakeResponse struct {
	ID                      uuid.UUID    `json:"id"`
	WasteID                 uuid.UUID    `json:"waste_id"`
	SourcePreparationUnitID uuid.UUID    `json:"source_preparation_unit_id"`
	Reason                  string       `json:"reason"`
	Note                    *string      `json:"note"`
	CreatedAt               time.Time    `json:"created_at"`
	Unit                    UnitResponse `json:"unit"`
}

// RemakeUnitCommand creates the linked replacement for one Waste. The path
// supplies the Waste id (spec §7.4).
type RemakeUnitCommand struct {
	RequestID uuid.UUID `json:"request_id"`
	Reason    string    `json:"reason"`
	Note      *string   `json:"note"`
	WasteID   uuid.UUID `json:"-"`
}

// CorrectStateCommand reverses 1 through MaxCorrectionUnits units one step
// along the chain. ManagerPIN is request-only: it verifies the actor's own
// identity inside the mutation and must never reach a fingerprint, stored
// result, database fact, audit, or log (spec §7.5).
type CorrectStateCommand struct {
	RequestID          uuid.UUID   `json:"request_id"`
	PreparationUnitIDs []uuid.UUID `json:"preparation_unit_ids"`
	TargetState        string      `json:"target_state"`
	Reason             string      `json:"reason"`
	Note               *string     `json:"note"`
	ManagerPIN         string      `json:"manager_pin"`
}

// CorrectStateOutcome is one unit's correction inside a batch, reported in
// request order (spec §7.5).
type CorrectStateOutcome struct {
	CorrectionID      uuid.UUID `json:"correction_id"`
	PreparationUnitID uuid.UUID `json:"preparation_unit_id"`
	PriorState        string    `json:"prior_state"`
	ResultingState    string    `json:"resulting_state"`
	CorrectedAt       time.Time `json:"corrected_at"`
}

// CorrectStateResponse is the result of one State Correction batch. Outcomes
// is allocated by the handler as a non-nil slice sized to the selection.
type CorrectStateResponse struct {
	Outcomes []CorrectStateOutcome `json:"outcomes"`
}

// correctStateFingerprint is the normalized, credential-free business input a
// State Correction stands for. ManagerPIN is deliberately absent — a PIN must
// never enter a fingerprint, so replays stay comparable without ever hashing
// a secret (spec §7.5). The correction command builds it from the normalized
// command, preserving request order.
type correctStateFingerprint struct {
	PreparationUnitIDs []uuid.UUID `json:"preparation_unit_ids"`
	TargetState        string      `json:"target_state"`
	Reason             string      `json:"reason"`
	Note               *string     `json:"note"`
}

// QueueAlertResponse is one active alert on the queue read (spec §6.3): unit
// identity projected from the unit at the read's snapshot, and WasteID
// resolved through the Waste fact for WASTE alerts only — the reserved
// Cancellation kinds carry a null waste_id. Acknowledgment evidence is
// always null here because only unacknowledged alerts appear, but the fields
// stay on the contract so the client reads one alert shape.
type QueueAlertResponse struct {
	ID                            uuid.UUID  `json:"id"`
	Kind                          string     `json:"kind"`
	PreparationUnitID             uuid.UUID  `json:"preparation_unit_id"`
	ServiceNumber                 string     `json:"service_number"`
	ItemName                      string     `json:"item_name"`
	UnitNumber                    int32      `json:"unit_number"`
	Reason                        string     `json:"reason"`
	Note                          *string    `json:"note"`
	WasteID                       *uuid.UUID `json:"waste_id"`
	CreatedAt                     time.Time  `json:"created_at"`
	AcknowledgedByStaffIdentityID *uuid.UUID `json:"acknowledged_by_staff_identity_id"`
	AcknowledgedAt                *time.Time `json:"acknowledged_at"`
}

// QueueCorrectionResponse is one entry of the queue's Waste and Remake
// history (spec §6.4). EntryKind discriminates the two typed facts; the
// fields a WASTE entry does not carry stay nil:
//
//	WASTE  — ID is the Waste id, PreparationUnitID the wasted source unit.
//	REMAKE — ID is the Remake id, WasteID its source fact, and
//	         PreparationUnitID the replacement unit, with the source unit's
//	         id and number alongside.
type QueueCorrectionResponse struct {
	EntryKind               string     `json:"entry_kind"`
	ID                      uuid.UUID  `json:"id"`
	PreparationUnitID       uuid.UUID  `json:"preparation_unit_id"`
	WasteID                 *uuid.UUID `json:"waste_id"`
	SourcePreparationUnitID *uuid.UUID `json:"source_preparation_unit_id"`
	SourceUnitNumber        *int32     `json:"source_unit_number"`
	ServiceNumber           string     `json:"service_number"`
	ItemName                string     `json:"item_name"`
	UnitNumber              int32      `json:"unit_number"`
	Reason                  string     `json:"reason"`
	Note                    *string    `json:"note"`
	OccurredAt              time.Time  `json:"occurred_at"`
}
