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
//
// RequestID also binds from the request_id query parameter: many HTTP
// clients, proxies, and load balancers strip or ignore a DELETE request's
// body, and the idempotency key must still reach the handler when they do. A
// body value, if present, takes precedence.
type RemoveDraftItemCommand struct {
	RequestID        uuid.UUID `json:"request_id" query:"request_id"`
	ServiceSessionID uuid.UUID `json:"-"`
	DraftItemID      uuid.UUID `json:"-"`
}

// CommitOrderDraftCommand fixes the draft's prices into a Check.
type CommitOrderDraftCommand struct {
	RequestID        uuid.UUID `json:"request_id"`
	ServiceSessionID uuid.UUID `json:"-"`
}

// StartNewOrderDraftCommand opens the Session's next Order Draft.
type StartNewOrderDraftCommand struct {
	RequestID        uuid.UUID `json:"request_id"`
	ServiceSessionID uuid.UUID `json:"-"`
}

// SetCheckTargetCommand steers where the next Commit's charges land.
type SetCheckTargetCommand struct {
	RequestID        uuid.UUID `json:"request_id"`
	ServiceSessionID uuid.UUID `json:"-"`
	CheckTarget      string    `json:"check_target"`
}

// PayCashCommand records cash received against one Check. The applied amount
// and the cash tendered are separate facts: the applied amount is what the
// customer owed, the tendered amount is what they handed over.
type PayCashCommand struct {
	RequestID        uuid.UUID `json:"request_id"`
	CheckID          uuid.UUID `json:"-"`
	AppliedAmountVND int64     `json:"applied_amount_vnd"`
	CashTenderedVND  int64     `json:"cash_tendered_vnd"`
}

// PayManualQRCommand records a bank transfer staff confirmed as received.
//
// ReceiptObservedInBankApp is required to be true: a Manual QR Payment has no
// automatic bank or gateway confirmation, so a staff member seeing the money
// arrive is the only evidence there is. It is not stored as a column — a
// value that is true on every row stores nothing — but it is recorded in the
// audit event. See ADR-019.
type PayManualQRCommand struct {
	RequestID                uuid.UUID `json:"request_id"`
	CheckID                  uuid.UUID `json:"-"`
	AppliedAmountVND         int64     `json:"applied_amount_vnd"`
	ReceiptObservedInBankApp bool      `json:"receipt_observed_in_bank_app"`
	TransactionReference     *string   `json:"transaction_reference"`
}

// SplitItem is one Committed Item and the quantity of it being moved.
type SplitItem struct {
	CommittedItemID uuid.UUID `json:"committed_item_id"`
	Quantity        int32     `json:"quantity"`
}

// SplitDestination names where the moved charge lands. Type is NEW_CHECK or
// EXISTING_CHECK; CheckID is required for the latter and forbidden for the
// former.
type SplitDestination struct {
	Type    string     `json:"type"`
	CheckID *uuid.UUID `json:"check_id,omitempty"`
}

// SplitCheckCommand moves part of a Check's charge onto another Check. Both
// Checks must be OPEN, belong to the same Service Session, and carry no
// Payment.
type SplitCheckCommand struct {
	RequestID     uuid.UUID        `json:"request_id"`
	SourceCheckID uuid.UUID        `json:"-"`
	Destination   SplitDestination `json:"destination"`
	Items         []SplitItem      `json:"items"`
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
//
// CheckTarget belongs to the draft, not the Session: it resets to
// CURRENT_UNPAID every time a new draft opens, so a cashier who directed one
// round to a new Check does not silently direct the next one there too.
type OrderDraftResponse struct {
	ID          uuid.UUID           `json:"id"`
	State       string              `json:"state"`
	CheckTarget string              `json:"check_target"`
	Items       []DraftItemResponse `json:"items"`
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

	// Filled from 5B; payments within each Check are filled by 5C.
	Checks []CheckResponse `json:"checks"`
	// Filled by 5D.
	Orders []struct{} `json:"orders"`
	// Filled by 5D.
	PreparationUnits []struct{} `json:"preparation_units"`
}

// CommittedModifierResponse is one frozen Modifier Option on a Committed Item.
// Names and surcharge are snapshots: a later rename in Catalog must not
// rewrite what a customer was charged for.
type CommittedModifierResponse struct {
	GroupID      uuid.UUID `json:"group_id"`
	GroupName    string    `json:"group_name"`
	OptionID     uuid.UUID `json:"option_id"`
	OptionName   string    `json:"option_name"`
	SurchargeVND int64     `json:"surcharge_vnd"`
}

// ChargeAllocationResponse is one Committed Item's charge against one Check.
//
// Submitted is a constant false in 5B and is filled by 5D, which introduces
// the orders table this flag is derived from.
type ChargeAllocationResponse struct {
	ID                uuid.UUID                   `json:"id"`
	CommittedItemID   uuid.UUID                   `json:"committed_item_id"`
	MenuItemID        uuid.UUID                   `json:"menu_item_id"`
	CategoryName      string                      `json:"category_name"`
	Name              string                      `json:"name"`
	SizeName          *string                     `json:"size_name"`
	PreparationNote   *string                     `json:"preparation_note"`
	Modifiers         []CommittedModifierResponse `json:"modifiers"`
	CommittedQuantity int32                       `json:"committed_quantity"`
	CommittedTotalVND int64                       `json:"committed_total_vnd"`
	AllocatedQuantity int32                       `json:"allocated_quantity"`
	AmountVND         int64                       `json:"amount_vnd"`
	CreatedAt         time.Time                   `json:"created_at"`
	Submitted         bool                        `json:"submitted"`
}

// PaymentResponse is one confirmed receipt of money applied to a Check.
//
// The method-dependent fields are pointers with omitempty, so a Manual QR
// Payment does not carry two null cash fields and a Cash Payment does not
// carry a null bank reference.
type PaymentResponse struct {
	ID                   uuid.UUID `json:"id"`
	Method               string    `json:"method"`
	AppliedAmountVND     int64     `json:"applied_amount_vnd"`
	CashTenderedVND      *int64    `json:"cash_tendered_vnd,omitempty"`
	ChangeDueVND         *int64    `json:"change_due_vnd,omitempty"`
	TransactionReference *string   `json:"transaction_reference,omitempty"`
	SalesShiftID         uuid.UUID `json:"sales_shift_id"`
	ReceivedAt           time.Time `json:"received_at"`
}

// CheckResponse is a grouping of charges awaiting settlement.
//
// TotalAppliedVND is the sum of the Check's Payments and BalanceVND is
// ChargeVND minus it; both carry real values from 5C. MergedIntoCheckID is
// present only on a MERGED Check — an open Check does not carry a field
// pointing nowhere. PendingRefundVND is deliberately absent: Refund is
// outside Phase 5 entirely, and a Payment can never exceed the balance.
type CheckResponse struct {
	ID                uuid.UUID  `json:"id"`
	State             string     `json:"state"`
	ChargeVND         int64      `json:"charge_vnd"`
	TotalAppliedVND   int64      `json:"total_applied_vnd"`
	BalanceVND        int64      `json:"balance_vnd"`
	MergedIntoCheckID *uuid.UUID `json:"merged_into_check_id,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`

	Payments    []PaymentResponse          `json:"payments"`
	Allocations []ChargeAllocationResponse `json:"allocations"`
}
