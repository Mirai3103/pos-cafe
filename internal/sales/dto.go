package sales

import (
	"time"

	"github.com/google/uuid"
)

// ---------- Commands ----------

// ManagerApprovalInput is one inline Manager Approval: a second identity's
// login code and PIN, supplied with the request that needs one. It is
// request-only credentials. The executor copies the values into ApprovalSpec,
// and neither value ever reaches a fingerprint, stored result, business fact,
// audit detail, or log.
type ManagerApprovalInput struct {
	ApproverLoginCode string `json:"approver_login_code"`
	ManagerPIN        string `json:"manager_pin"`
}

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

// MergeChecksCommand absorbs one Check into another. Both must be OPEN,
// belong to the same Service Session, and carry no Payment.
type MergeChecksCommand struct {
	RequestID        uuid.UUID `json:"request_id"`
	SurvivingCheckID uuid.UUID `json:"surviving_check_id"`
	AbsorbedCheckID  uuid.UUID `json:"absorbed_check_id"`
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
// are always present, so the contract never breaks: a Session starts with all
// three empty, and they fill as Checks are committed and Orders are submitted.
// Preparation alerts and corrections are Phase 6 concerns and are
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
	// Filled from 5D.
	Orders []OrderResponse `json:"orders"`
	// Filled from 5D.
	PreparationUnits []PreparationUnitResponse `json:"preparation_units"`
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
// Submitted reports whether the Committed Item has entered an Order. It is
// derived from the existence of an Order Item rather than stored, so it can
// never fall out of step with the Order that defines it.
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
// carry a null bank reference. RemainingRefundableVND is the applied amount
// less every Refund allocation against it, pending Manual QR intents included.
// Void is nil while the Payment stands.
type PaymentResponse struct {
	ID                     uuid.UUID            `json:"id"`
	Method                 string               `json:"method"`
	AppliedAmountVND       int64                `json:"applied_amount_vnd"`
	CashTenderedVND        *int64               `json:"cash_tendered_vnd,omitempty"`
	ChangeDueVND           *int64               `json:"change_due_vnd,omitempty"`
	TransactionReference   *string              `json:"transaction_reference,omitempty"`
	SalesShiftID           uuid.UUID            `json:"sales_shift_id"`
	ReceivedAt             time.Time            `json:"received_at"`
	Void                   *PaymentVoidResponse `json:"void,omitempty"`
	RemainingRefundableVND int64                `json:"remaining_refundable_vnd"`
}

// PaymentVoidResponse is the append-only reversal of one whole Payment. The
// source Payment is never edited or deleted.
type PaymentVoidResponse struct {
	ID                        uuid.UUID `json:"id"`
	AmountVND                 int64     `json:"amount_vnd"`
	Reason                    string    `json:"reason"`
	Note                      *string   `json:"note"`
	ActorStaffIdentityID      uuid.UUID `json:"actor_staff_identity_id"`
	ApprovedByStaffIdentityID uuid.UUID `json:"approved_by_staff_identity_id"`
	OccurredAt                time.Time `json:"occurred_at"`
}

// ChargeAdjustmentResponse is one append-only reduction of customer charge
// sourced by exactly one Cancellation or Comp. It never mutates the original
// Charge Allocation it names. RemainingRefundableVND is the amount less every
// Refund allocation against it.
type ChargeAdjustmentResponse struct {
	ID                     uuid.UUID  `json:"id"`
	Kind                   string     `json:"kind"`
	Scope                  string     `json:"scope"`
	PreparationUnitID      uuid.UUID  `json:"preparation_unit_id"`
	PreparationWasteID     *uuid.UUID `json:"preparation_waste_id"`
	ChargeAllocationID     uuid.UUID  `json:"charge_allocation_id"`
	CompletedSaleID        *uuid.UUID `json:"completed_sale_id,omitempty"`
	SalesShiftID           uuid.UUID  `json:"sales_shift_id"`
	AmountVND              int64      `json:"amount_vnd"`
	RemainingRefundableVND int64      `json:"remaining_refundable_vnd"`
	CreatedAt              time.Time  `json:"created_at"`
}

// RefundAllocationResponse is one source of refunded value: the Payment id or
// the Charge Adjustment id, depending on which allocation collection carries
// it, and the amount allocated.
type RefundAllocationResponse struct {
	ID        uuid.UUID `json:"id"`
	AmountVND int64     `json:"amount_vnd"`
}

// RefundCompletionResponse is the append-only evidence that the Refund's money
// actually moved. TransactionReference is present only on a completed Manual
// QR Refund.
type RefundCompletionResponse struct {
	ID                            uuid.UUID `json:"id"`
	TransactionReference          *string   `json:"transaction_reference,omitempty"`
	CompletedByStaffIdentityID    uuid.UUID `json:"completed_by_staff_identity_id"`
	CompletedStaffAccessSessionID uuid.UUID `json:"completed_staff_access_session_id"`
	CompletedAt                   time.Time `json:"completed_at"`
}

// RefundResponse is one Refund with its derived state and both allocation
// collections. Cash completes in the transaction that records it; Manual QR
// stays PENDING until its completion is appended. No credential is projected.
type RefundResponse struct {
	ID                        uuid.UUID                  `json:"id"`
	CheckID                   uuid.UUID                  `json:"check_id"`
	CompletedSaleID           *uuid.UUID                 `json:"completed_sale_id,omitempty"`
	SalesShiftID              uuid.UUID                  `json:"sales_shift_id"`
	Method                    string                     `json:"method"`
	AmountVND                 int64                      `json:"amount_vnd"`
	State                     string                     `json:"state"`
	Reason                    string                     `json:"reason"`
	Note                      *string                    `json:"note"`
	ActorStaffIdentityID      uuid.UUID                  `json:"actor_staff_identity_id"`
	ApprovedByStaffIdentityID uuid.UUID                  `json:"approved_by_staff_identity_id"`
	CreatedAt                 time.Time                  `json:"created_at"`
	PaymentAllocations        []RefundAllocationResponse `json:"payment_allocations"`
	AdjustmentAllocations     []RefundAllocationResponse `json:"adjustment_allocations"`
	Completion                *RefundCompletionResponse  `json:"completion,omitempty"`
}

// CheckResponse is a grouping of charges awaiting settlement.
//
// ChargeVND is the live adjusted charge and BaseChargeVND is the original
// allocation sum it derives from. TotalAppliedVND stays the immutable sum of
// original Payments for historical clarity; Voids and completed Refunds are
// subtracted explicitly through TotalVoidedVND, TotalRefundedVND, and
// EffectiveReceivedVND rather than making that field change meaning.
// BalanceVND is the customer amount still due and PendingRefundVND is money
// owed back. MergedIntoCheckID is present only on a MERGED Check.
type CheckResponse struct {
	ID                   uuid.UUID  `json:"id"`
	State                string     `json:"state"`
	BaseChargeVND        int64      `json:"base_charge_vnd"`
	ChargeVND            int64      `json:"charge_vnd"`
	TotalAppliedVND      int64      `json:"total_applied_vnd"`
	TotalVoidedVND       int64      `json:"total_voided_vnd"`
	TotalRefundedVND     int64      `json:"total_refunded_vnd"`
	EffectiveReceivedVND int64      `json:"effective_received_vnd"`
	BalanceVND           int64      `json:"balance_vnd"`
	PendingRefundVND     int64      `json:"pending_refund_vnd"`
	MergedIntoCheckID    *uuid.UUID `json:"merged_into_check_id,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`

	Payments          []PaymentResponse          `json:"payments"`
	Allocations       []ChargeAllocationResponse `json:"allocations"`
	ChargeAdjustments []ChargeAdjustmentResponse `json:"charge_adjustments"`
	Refunds           []RefundResponse           `json:"refunds"`
}

// OrderResponse is one submitted Order: the preparation boundary crossed once
// for one Order Draft.
type OrderResponse struct {
	ID                 uuid.UUID           `json:"id"`
	OrderDraftID       uuid.UUID           `json:"order_draft_id"`
	SubmittedByStaffID uuid.UUID           `json:"submitted_by_staff_identity_id"`
	SubmittedSessionID uuid.UUID           `json:"submitted_staff_access_session_id"`
	SubmittedAt        time.Time           `json:"submitted_at"`
	Items              []OrderItemResponse `json:"items"`
}

// OrderItemResponse is a Committed Item after submission.
//
// It carries the commercial snapshot by reference rather than by copy
// (ADR-025): committed_items is immutable by 5B's rule, so the identifier is
// the snapshot. The names and amounts a client needs are already on the
// Check's Charge Allocations, keyed by the same CommittedItemID.
type OrderItemResponse struct {
	ID              uuid.UUID `json:"id"`
	CommittedItemID uuid.UUID `json:"committed_item_id"`
}

// UnitModifierResponse is one frozen Modifier Option on a Preparation Unit.
// It carries no price: the bar needs to know what to make, not what it cost.
type UnitModifierResponse struct {
	GroupName  string `json:"group_name"`
	OptionName string `json:"option_name"`
}

// PreparationUnitResponse is one individually prepared unit of an ordered
// item. A Committed Item of quantity three becomes three of these.
//
// Priority and RemakeOfPreparationUnitID are the Phase 6B Remake metadata the
// read projects for both the live Service Session and the Completed Sale:
// originals are STANDARD with a constant null link, and only a linked
// replacement is REMAKE pointing at the exact wasted source unit (spec §5.1).
// No alert or correction-history fields ride on the unit object.
type PreparationUnitResponse struct {
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
	Priority        string                 `json:"priority"`
	// RemakeOfPreparationUnitID links a Remake to the wasted source unit it
	// replaces; nil for every original unit.
	RemakeOfPreparationUnitID *uuid.UUID `json:"remake_of_preparation_unit_id"`
}

// SubmitOrderCommand sends a Service Session's committed round to the bar.
type SubmitOrderCommand struct {
	RequestID        uuid.UUID `json:"request_id" validate:"required"`
	ServiceSessionID uuid.UUID `json:"-"`
}

// CloseServiceSessionCommand completes a Service Session into a Completed Sale.
type CloseServiceSessionCommand struct {
	RequestID        uuid.UUID `json:"request_id" validate:"required"`
	ServiceSessionID uuid.UUID `json:"-"`
}

// CompletedSaleCheckResponse is one Check as it stood at closure: settled,
// with a zero balance, carrying its Payments and Charge Allocations.
type CompletedSaleCheckResponse struct {
	ID              uuid.UUID                  `json:"id"`
	State           string                     `json:"state"`
	ChargeVND       int64                      `json:"charge_vnd"`
	TotalAppliedVND int64                      `json:"total_applied_vnd"`
	BalanceVND      int64                      `json:"balance_vnd"`
	Payments        []PaymentResponse          `json:"payments"`
	Allocations     []ChargeAllocationResponse `json:"allocations"`
}

// PreparationTransitionResponse is one recorded move of a Preparation Unit.
//
// It reads from preparation_unit_transitions rather than reconstructing the
// history from audit payloads (ADR-027): a Completed Sale is immutable
// content, not a derived report.
type PreparationTransitionResponse struct {
	ID             uuid.UUID `json:"id"`
	UnitID         uuid.UUID `json:"preparation_unit_id"`
	PriorState     string    `json:"prior_state"`
	ResultingState string    `json:"resulting_state"`
	ActorStaffID   uuid.UUID `json:"actor_staff_identity_id"`
	StaffSessionID uuid.UUID `json:"staff_access_session_id"`
	OccurredAt     time.Time `json:"occurred_at"`
}

// CompletedSaleResponse is the immutable outcome of a closed Service Session.
type CompletedSaleResponse struct {
	ID                     uuid.UUID                       `json:"id"`
	State                  string                          `json:"state"`
	ServiceSessionID       uuid.UUID                       `json:"service_session_id"`
	ServiceNumber          string                          `json:"service_number"`
	ServiceMode            string                          `json:"service_mode"`
	ServiceSessionState    string                          `json:"service_session_state"`
	ServiceSessionOpenedAt time.Time                       `json:"service_session_opened_at"`
	CompletedByStaffID     uuid.UUID                       `json:"completed_by_staff_identity_id"`
	CompletedBySessionID   uuid.UUID                       `json:"completed_staff_access_session_id"`
	CompletedByName        string                          `json:"completed_by_display_name"`
	CompletedAt            time.Time                       `json:"completed_at"`
	Checks                 []CompletedSaleCheckResponse    `json:"checks"`
	Orders                 []OrderResponse                 `json:"orders"`
	PreparationUnits       []PreparationUnitResponse       `json:"preparation_units"`
	PreparationHistory     []PreparationTransitionResponse `json:"preparation_history"`
}

// CompletedSaleStateCompleted is the only state a Completed Sale has. It is a
// literal in the contract so a client can branch on it exactly as it branches
// on a Check's or a Session's state.
const CompletedSaleStateCompleted = "COMPLETED"

// ---------- Phase 6C: Comp ----------

// CompWasteCommand waives the charge of one charged Wasted unit. WasteID is
// json:"-": it comes from the path, never the body. ManagerApproval carries
// request-only credentials; the executor verifies them inline, and no
// credential ever reaches a fingerprint, stored result, business fact, audit
// detail, or log (spec §8.1).
type CompWasteCommand struct {
	RequestID       uuid.UUID            `json:"request_id"`
	WasteID         uuid.UUID            `json:"-"`
	Reason          string               `json:"reason"`
	Note            *string              `json:"note"`
	ManagerApproval ManagerApprovalInput `json:"manager_approval"`
}

// CompResponse is one recorded Comp: the append-only fact a client reads back.
// AmountVND is the immutable per-unit price the Comp waives; the Manager
// approver is recorded separately from the initiator, so a self-approved
// command stays distinguishable in the audit trail.
type CompResponse struct {
	ID                        uuid.UUID `json:"id"`
	WasteID                   uuid.UUID `json:"waste_id"`
	PreparationUnitID         uuid.UUID `json:"preparation_unit_id"`
	ChargeAdjustmentID        uuid.UUID `json:"charge_adjustment_id"`
	AmountVND                 int64     `json:"amount_vnd"`
	Reason                    string    `json:"reason"`
	Note                      *string   `json:"note"`
	ActorStaffIdentityID      uuid.UUID `json:"actor_staff_identity_id"`
	ApprovedByStaffIdentityID uuid.UUID `json:"approved_by_staff_identity_id"`
	OccurredAt                time.Time `json:"occurred_at"`
}

// PostSaleCorrectionResponse is one post-sale Comp correction in a closed
// sale's additive history: its POST_SALE Charge Adjustment, the Comp fact,
// the Refunds that have consumed that adjustment's corrected capacity
// (non-null, empty until the Refund command records one), and the amount of
// the correction still owed back.
type PostSaleCorrectionResponse struct {
	Adjustment           ChargeAdjustmentResponse `json:"adjustment"`
	Comp                 CompResponse             `json:"comp"`
	Refunds              []RefundResponse         `json:"refunds"`
	OutstandingRefundVND int64                    `json:"outstanding_refund_vnd"`
}

// CompResult is the discriminated Comp result. Exactly one branch is present:
// a live Comp carries the updated Service Session, while a post-sale Comp
// carries the Completed Sale id, the outstanding post-sale correction amount,
// and the additive correction history, and no mutable Session projection
// (spec §8.3, §12.3).
type CompResult struct {
	Scope                        string                       `json:"scope"`
	Comp                         CompResponse                 `json:"comp"`
	ServiceSession               *ServiceSessionResponse      `json:"service_session,omitempty"`
	CompletedSaleID              *uuid.UUID                   `json:"completed_sale_id,omitempty"`
	OutstandingPostSaleRefundVND *int64                       `json:"outstanding_post_sale_refund_vnd,omitempty"`
	PostSaleCorrections          []PostSaleCorrectionResponse `json:"post_sale_corrections,omitempty"`
}

// ---------- Phase 6C: Refund ----------

// RefundPaymentAllocationInput names one Payment the refunded value came in
// through and the amount allocated against it.
type RefundPaymentAllocationInput struct {
	PaymentID uuid.UUID `json:"payment_id"`
	AmountVND int64     `json:"amount_vnd"`
}

// RefundAdjustmentAllocationInput names one Charge Adjustment whose corrected
// value is being refunded and the amount allocated against it.
type RefundAdjustmentAllocationInput struct {
	ChargeAdjustmentID uuid.UUID `json:"charge_adjustment_id"`
	AmountVND          int64     `json:"amount_vnd"`
}

// RecordRefundCommand returns real money through the original Payment method
// while consuming both corrected refundable capacity and original Payment
// refundable capacity. Both allocation collections are required and must sum
// to the same positive amount. ManagerApproval carries request-only
// credentials; the executor verifies them inline, and no credential ever
// reaches a fingerprint, stored result, business fact, audit detail, or log
// (spec §9.1).
type RecordRefundCommand struct {
	RequestID             uuid.UUID                         `json:"request_id"`
	CheckID               uuid.UUID                         `json:"check_id"`
	Method                string                            `json:"method"`
	AdjustmentAllocations []RefundAdjustmentAllocationInput `json:"adjustment_allocations"`
	PaymentAllocations    []RefundPaymentAllocationInput    `json:"payment_allocations"`
	Reason                string                            `json:"reason"`
	Note                  *string                           `json:"note"`
	ManagerApproval       ManagerApprovalInput              `json:"manager_approval"`
}

// RefundResult is the discriminated Refund result. Exactly one branch is
// present: a live Refund carries the updated Service Session, while a
// post-sale Refund carries the Completed Sale id and its additive correction
// history and no mutable Session projection (spec §9.1, §12.3).
type RefundResult struct {
	Scope               string                       `json:"scope"`
	Refund              RefundResponse               `json:"refund"`
	ServiceSession      *ServiceSessionResponse      `json:"service_session,omitempty"`
	CompletedSaleID     *uuid.UUID                   `json:"completed_sale_id,omitempty"`
	PostSaleCorrections []PostSaleCorrectionResponse `json:"post_sale_corrections,omitempty"`
}
