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
