package sales

import (
	"time"

	"github.com/google/uuid"
)

// ---------- Commands ----------

// StartTakeawaySessionCommand opens an anonymous Takeaway Service Session.
type StartTakeawaySessionCommand struct {
	RequestID uuid.UUID `json:"request_id"`
}

// StartDineInSessionCommand opens a Dine-in Session against one or more Tables.
type StartDineInSessionCommand struct {
	RequestID uuid.UUID   `json:"request_id"`
	TableIDs  []uuid.UUID `json:"table_ids"`
}

// SetSessionTablesCommand replaces a Dine-in Session's current Table set.
// An empty TableIDs releases every assignment and is permitted.
type SetSessionTablesCommand struct {
	RequestID        uuid.UUID   `json:"request_id"`
	ServiceSessionID uuid.UUID   `json:"-"`
	TableIDs         []uuid.UUID `json:"table_ids"`
}

// AddDraftItemCommand adds one unit of a configured Menu Item to the draft.
//
// ModifierOptionIDs is a pointer because absent and empty mean different
// things: absent applies the menu's default options, empty applies none.
// A non-pointer slice would collapse both to nil.
type AddDraftItemCommand struct {
	RequestID         uuid.UUID    `json:"request_id"`
	ServiceSessionID  uuid.UUID    `json:"-"`
	MenuItemID        uuid.UUID    `json:"menu_item_id"`
	SizeID            *uuid.UUID   `json:"size_id"`
	PreparationNote   *string      `json:"preparation_note"`
	ModifierOptionIDs *[]uuid.UUID `json:"modifier_option_ids"`
}

// SetDraftItemQuantityCommand sets an absolute quantity. Zero is rejected;
// removal is its own command.
type SetDraftItemQuantityCommand struct {
	RequestID        uuid.UUID `json:"request_id"`
	ServiceSessionID uuid.UUID `json:"-"`
	DraftItemID      uuid.UUID `json:"-"`
	Quantity         *int32    `json:"quantity"`
}

// SetDraftItemSizeCommand sets or clears the Size. A nil SizeID clears it.
type SetDraftItemSizeCommand struct {
	RequestID        uuid.UUID  `json:"request_id"`
	ServiceSessionID uuid.UUID  `json:"-"`
	DraftItemID      uuid.UUID  `json:"-"`
	SizeID           *uuid.UUID `json:"size_id"`
}

// SetDraftItemNoteCommand sets or clears the Preparation Note.
type SetDraftItemNoteCommand struct {
	RequestID        uuid.UUID `json:"request_id"`
	ServiceSessionID uuid.UUID `json:"-"`
	DraftItemID      uuid.UUID `json:"-"`
	PreparationNote  *string   `json:"preparation_note"`
}

// SetDraftItemModifiersCommand replaces the selected options. Unlike the add
// command this list is required; an empty list is taken literally and no
// defaults are applied.
type SetDraftItemModifiersCommand struct {
	RequestID         uuid.UUID   `json:"request_id"`
	ServiceSessionID  uuid.UUID   `json:"-"`
	DraftItemID       uuid.UUID   `json:"-"`
	ModifierOptionIDs []uuid.UUID `json:"modifier_option_ids"`
}

// RemoveDraftItemCommand removes a draft item outright.
type RemoveDraftItemCommand struct {
	RequestID        uuid.UUID `json:"request_id"`
	ServiceSessionID uuid.UUID `json:"-"`
	DraftItemID      uuid.UUID `json:"-"`
}

// ---------- Responses ----------

// SessionTableResponse is a Table currently assigned to a Service Session.
type SessionTableResponse struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// SelectedModifierOptionResponse is one chosen Modifier Option on a draft item.
type SelectedModifierOptionResponse struct {
	ID           uuid.UUID `json:"id"`
	GroupID      uuid.UUID `json:"group_id"`
	GroupName    string    `json:"group_name"`
	Name         string    `json:"name"`
	SurchargeVND int64     `json:"surcharge_vnd"`
}

// DraftItemResponse is one line of the Order Draft.
//
// PriceVND is nil when the Menu Item is priced through a required Size that
// has not been chosen yet. A draft tolerates that incompleteness; Commit does
// not.
type DraftItemResponse struct {
	ID                      uuid.UUID                        `json:"id"`
	MenuItemID              uuid.UUID                        `json:"menu_item_id"`
	Name                    string                           `json:"name"`
	PriceVND                *int64                           `json:"price_vnd"`
	SizeID                  *uuid.UUID                       `json:"size_id"`
	SizeName                *string                          `json:"size_name"`
	Quantity                int32                            `json:"quantity"`
	Available               bool                             `json:"available"`
	PreparationNote         *string                          `json:"preparation_note"`
	SelectedModifierOptions []SelectedModifierOptionResponse `json:"selected_modifier_options"`
}

// OrderDraftResponse is the editable Order Draft.
type OrderDraftResponse struct {
	ID    uuid.UUID           `json:"id"`
	State string              `json:"state"`
	Items []DraftItemResponse `json:"items"`
}

// ServiceSessionResponse is the one projection every Sales operation returns.
//
// It ships in its final shape from 5A. Checks, Orders, and PreparationUnits
// are always present and empty until 5B, 5C, and 5D fill them, so the contract
// never breaks. Preparation alerts and corrections are Phase 6 concerns and are
// omitted entirely rather than stubbed.
//
// CustomerIdentityID is a constant null: every opening-day Service Session is
// anonymous, and the field exists so a future Loyalty Program does not reshape
// the contract.
//
// SalesShiftID has no canonical counterpart. ADR-011 makes the Service Number
// unique only within its Shift, so a client storing or printing one needs the
// Shift alongside it.
type ServiceSessionResponse struct {
	ID                 uuid.UUID              `json:"id"`
	ServiceNumber      string                 `json:"service_number"`
	ServiceMode        string                 `json:"service_mode"`
	State              string                 `json:"state"`
	CustomerIdentityID *uuid.UUID             `json:"customer_identity_id"`
	SalesShiftID       uuid.UUID              `json:"sales_shift_id"`
	Tables             []SessionTableResponse `json:"tables"`
	CreatedAt          time.Time              `json:"created_at"`
	Draft              *OrderDraftResponse    `json:"draft"`

	// Filled by 5B and 5C.
	Checks []struct{} `json:"checks"`
	// Filled by 5D.
	Orders []struct{} `json:"orders"`
	// Filled by 5D.
	PreparationUnits []struct{} `json:"preparation_units"`
}
