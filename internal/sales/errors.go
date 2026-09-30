package sales

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/Mirai3103/pos-cafe/internal/platform/command"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrOpenShiftRequired      = errors.New("an open sales shift is required")
	ErrServiceSessionNotFound = errors.New("service session not found")
	ErrServiceSessionClosed   = errors.New("service session is already closed")
	ErrEditableDraftNotFound  = errors.New("no editable order draft for this service session")
	ErrDraftItemNotFound      = errors.New("draft item not found")

	ErrMenuItemNotFound    = errors.New("menu item not found")
	ErrMenuItemUnavailable = errors.New("menu item is unavailable")
	ErrMenuItemRetired     = errors.New("menu item is retired")

	ErrSizeNotFound    = errors.New("size not found for this menu item")
	ErrSizeUnavailable = errors.New("size is unavailable")
	ErrSizeRetired     = errors.New("size is retired")

	ErrModifierOptionNotFound    = errors.New("modifier option not found for this menu item")
	ErrModifierOptionUnavailable = errors.New("modifier option is unavailable")
	ErrModifierOptionRetired     = errors.New("modifier option is retired")

	ErrInvalidPreparationNote = errors.New("invalid preparation note")
	ErrInvalidQuantity        = errors.New("invalid quantity")

	ErrLineTotalOutOfRange      = errors.New("line total out of range")
	ErrCheckChargeOutOfRange    = errors.New("check charge out of range")
	ErrInsufficientCashTendered = errors.New("cash tendered is below the applied amount")
	ErrInvalidCheckTarget       = errors.New("invalid check target")

	ErrTableSelectionRequired     = errors.New("at least one table is required for a dine-in session")
	ErrTableSelectionDuplicate    = errors.New("the same table was selected twice")
	ErrTableNotFound              = errors.New("table not found")
	ErrTableUnavailable           = errors.New("table is unavailable")
	ErrTakeawayTablesNotAvailable = errors.New("a takeaway session cannot be assigned tables")

	ErrRequestConflict     = command.ErrRequestConflict
	ErrInvalidStoredResult = command.ErrInvalidStoredResult
	ErrForbidden           = command.ErrForbidden
	ErrUnauthorized        = command.ErrUnauthorized

	// ErrServiceSequenceExhausted cannot occur in normal operation: a Shift
	// would need 99,999 Service Sessions. It exists so FormatServiceNumber has
	// a typed failure instead of emitting a seventh character that the
	// database check would reject as an unmapped 23514.
	ErrServiceSequenceExhausted = errors.New("service number sequence exhausted for this shift")

	ErrEmptyDraft = errors.New("order draft has no items")

	ErrCommitMenuItemUnavailable = errors.New("menu item is unavailable at commit")
	ErrCommitMenuItemRetired     = errors.New("menu item is retired at commit")

	ErrCommitSizeRequired    = errors.New("a size must be chosen before commit")
	ErrCommitSizeInvalid     = errors.New("size is not valid for this menu item")
	ErrCommitSizeUnavailable = errors.New("size is unavailable at commit")
	ErrCommitSizeRetired     = errors.New("size is retired at commit")

	ErrCommitModifierOptionInvalid     = errors.New("modifier option is not selectable for this menu item")
	ErrCommitModifierOptionUnavailable = errors.New("modifier option is unavailable at commit")
	ErrCommitModifierOptionRetired     = errors.New("modifier option is retired at commit")
	ErrCommitModifierGroupInvalid      = errors.New("modifier group selection rules are not satisfied")
	ErrCommitModifierGroupRetired      = errors.New("a required modifier group is retired")

	ErrNewOrderDraftNotAvailable = errors.New("a new order draft cannot be started yet")

	// ErrChargeInvariantViolated reports that a Check's stored charge_vnd
	// disagrees with the sum of its allocations. That is a defect, not a
	// business state, so it is deliberately absent from MapHTTPError and
	// surfaces as a 500 with the Check id logged.
	ErrChargeInvariantViolated = errors.New("check charge does not match its allocations")

	ErrCheckNotFound            = errors.New("check not found")
	ErrCheckNotOpen             = errors.New("check is not open")
	ErrCheckHasPayment          = errors.New("check already carries a payment")
	ErrCheckHasChargeAdjustment = errors.New("check carries a live charge adjustment")
	ErrChecksDifferentSession   = errors.New("checks belong to different service sessions")

	ErrPaymentExceedsBalance   = errors.New("payment exceeds the check balance")
	ErrManualQRReceiptRequired = errors.New("the bank receipt must be confirmed before recording a manual QR payment")

	ErrInvalidCheckSplit              = errors.New("invalid check split")
	ErrSplitAllocationNotFound        = errors.New("committed item is not allocated to the source check")
	ErrSplitQuantityExceedsAllocation = errors.New("split quantity exceeds the allocated quantity")
	ErrSplitSourceWouldBeEmpty        = errors.New("the split would empty the source check")
	ErrSplitDestinationWouldBeEmpty   = errors.New("the split would leave the destination check empty")
	ErrInvalidCheckMerge              = errors.New("invalid check merge")

	// ErrSettlementInvariantViolated reports that a Check's state disagrees
	// with its balance — SETTLED with money owed, or OPEN with none. That is
	// a defect, not a business state, so it is deliberately absent from
	// MapHTTPError and surfaces as a 500 with the Check id logged. It is the
	// read-path half of the pair guarding settlement; the database constraint
	// check_settlement_evidence_valid is the other half.
	ErrSettlementInvariantViolated = errors.New("check state does not match its balance")

	// ErrFinancialInvariantViolated reports that persisted correction facts
	// cannot satisfy the live financial equation — a live adjustment larger
	// than its base charge, a Void larger than its Payment, a completed Refund
	// larger than the valid receipt, or allocations that disagree with their
	// source. That is a defect, not a business state, so it is deliberately
	// absent from MapHTTPError and surfaces as a 500 with the Check id logged.
	ErrFinancialInvariantViolated = errors.New("check financials do not satisfy their invariant")

	ErrNothingToSubmit                  = errors.New("no committed order draft awaits submission")
	ErrNothingAwaitingSubmission        = errors.New("the service session is not awaiting submission")
	ErrSessionHasOrder                  = errors.New("the service session already has a submitted order")
	ErrPaymentRequiresRefund            = errors.New("every payment must be fully refunded before the checkout is abandoned")
	ErrCheckNotSettledForSubmission     = errors.New("every check must be settled before a takeaway order is submitted")
	ErrCheckNotSettledForClosure        = errors.New("every check must be settled before the service session closes")
	ErrAwaitingSubmissionForClosure     = errors.New("paid committed items must be submitted or cancelled before the service session closes")
	ErrPendingRefundForClosure          = errors.New("every pending refund must be resolved before the service session closes")
	ErrUnsubmittedWorkForClosure        = errors.New("every committed item must be submitted before the service session closes")
	ErrOrderRequiredForClosure          = errors.New("a service session with no order cannot close")
	ErrUnfulfilledPreparationForClosure = errors.New("every preparation unit must be terminal before the service session closes")
	ErrCompletedSaleNotFound            = errors.New("completed sale not found")

	// Comp conditions. A missing Waste is a not-found answer; a
	// Wasted Remake is an uncharged source; a second Comp of one Waste is
	// a lifecycle conflict the unique facts reject; and a source mapping
	// that moved under a concurrent restructuring refuses whole.
	ErrWasteNotFound            = errors.New("waste not found")
	ErrCompSourceNotCharged     = errors.New("the comp source is not a charged standard unit")
	ErrWasteAlreadyComped       = errors.New("the waste already carries a comp")
	ErrChargeAdjustmentConflict = errors.New("the charge mapping changed concurrently")

	// Refund conditions. A selected source id that resolves to no
	// row is a not-found answer, so it stays distinguishable from a source
	// that exists but belongs to another Check, carries the wrong scope, or is
	// otherwise unusable — that is an invalid allocation. An allocation that
	// exceeds either source's remaining capacity is a conflict, and a Refund
	// already completed cannot be completed again.
	ErrRefundNotFound                  = errors.New("refund not found")
	ErrRefundSourceNotFound            = errors.New("refund source not found")
	ErrRefundAllocationInvalid         = errors.New("refund allocation is invalid")
	ErrRefundExceedsAdjustmentCapacity = errors.New("refund exceeds the charge adjustment's remaining capacity")
	ErrRefundExceedsPaymentCapacity    = errors.New("refund exceeds the payment's remaining capacity")
	ErrRefundExceedsPendingRefund      = errors.New("refund exceeds the pending refund still owed back")
	ErrRefundMethodMismatch            = errors.New("refund method does not match the payment method")
	ErrRefundAlreadyCompleted          = errors.New("refund is already completed")

	// Payment Void conditions. A missing Payment is a not-found
	// answer; one whole Void per Payment makes a second attempt a lifecycle
	// conflict; any Refund allocation — pending or completed — locks the
	// Payment for good; and the original Shift must still be the currently
	// open one because voiding after a Shift close needs the separately
	// deferred Post-Shift Payment Correction.
	ErrPaymentNotFound        = errors.New("payment not found")
	ErrPaymentAlreadyVoided   = errors.New("payment already carries a void")
	ErrPaymentHasRefund       = errors.New("payment carries a refund allocation")
	ErrPaymentVoidShiftClosed = errors.New("the payment's original sales shift is not currently open")
)

// serviceSessionSalesShiftFK is the auto-generated name of the only foreign
// key whose violation means "no open Shift to attach to". service_sessions has
// another FK (the creating identity) whose violation is a defect, not this
// business state.
const serviceSessionSalesShiftFK = "service_sessions_sales_shift_id_fkey"

// MapDBError maps PostgreSQL driver and database errors to domain sentinels
// inside the Sales boundary, so an expected constraint failure never becomes
// an accidental generic 500.
func MapDBError(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		msg := pgErr.Detail
		if msg == "" {
			msg = pgErr.Message
		}
		if pgErr.Code == "23503" && pgErr.ConstraintName == serviceSessionSalesShiftFK {
			return fmt.Errorf("%w: %s", ErrOpenShiftRequired, msg)
		}
		// Every other code is deliberately unmapped:
		//
		//   23505 on the composition index is absorbed by the add path's
		//   upsert and never reaches here.
		//   23505 on service_session_number_per_shift_unique means the
		//   advisory-locked allocation path has a defect.
		//   23514 means Go validation and the database disagree.
		//
		// All three are defects, not business states, and must surface as
		// logged 500s.
	}
	return err
}

// httpErrors is the Sales HTTP error mapping. Rules are tried in order and
// the first match wins.
//
// Most rules answer with their sentinel's fixed text rather than the wrapped
// error, because the wrapped text may carry PostgreSQL detail that must stay
// server-side. The few rules that answer with the full error text wrap only
// client-safe detail.
//
// sql.ErrNoRows is deliberately not mapped. Sales reads several different rows
// and each caller decides which sentinel a miss means, so a bare sql.ErrNoRows
// reaching the mapper is a defect in the caller and surfaces as a 500.
var httpErrors = response.ErrorMapper{
	sentinelMessage(ErrOpenShiftRequired, http.StatusConflict, "OPEN_SALES_SHIFT_REQUIRED"),
	sentinelMessage(ErrServiceSessionNotFound, http.StatusNotFound, "SERVICE_SESSION_NOT_FOUND"),
	sentinelMessage(ErrServiceSessionClosed, http.StatusConflict, "SERVICE_SESSION_ALREADY_CLOSED"),
	sentinelMessage(ErrEditableDraftNotFound, http.StatusConflict, "EDITABLE_DRAFT_NOT_FOUND"),
	sentinelMessage(ErrDraftItemNotFound, http.StatusNotFound, "DRAFT_ITEM_NOT_FOUND"),

	sentinelMessage(ErrMenuItemNotFound, http.StatusNotFound, "MENU_ITEM_NOT_FOUND"),
	sentinelMessage(ErrMenuItemUnavailable, http.StatusConflict, "MENU_ITEM_UNAVAILABLE"),
	sentinelMessage(ErrMenuItemRetired, http.StatusConflict, "MENU_ITEM_RETIRED"),

	sentinelMessage(ErrSizeNotFound, http.StatusNotFound, "SIZE_NOT_FOUND"),
	sentinelMessage(ErrSizeUnavailable, http.StatusConflict, "SIZE_UNAVAILABLE"),
	sentinelMessage(ErrSizeRetired, http.StatusConflict, "SIZE_RETIRED"),

	sentinelMessage(ErrModifierOptionNotFound, http.StatusNotFound, "MODIFIER_OPTION_NOT_FOUND"),
	sentinelMessage(ErrModifierOptionUnavailable, http.StatusConflict, "MODIFIER_OPTION_UNAVAILABLE"),
	sentinelMessage(ErrModifierOptionRetired, http.StatusConflict, "MODIFIER_OPTION_RETIRED"),

	errorMessage(ErrInvalidPreparationNote, http.StatusBadRequest, "INVALID_PREPARATION_NOTE"),

	sentinelMessage(ErrTableSelectionRequired, http.StatusBadRequest, "DINE_IN_TABLE_SELECTION_REQUIRED"),
	sentinelMessage(ErrTableSelectionDuplicate, http.StatusBadRequest, "DINE_IN_TABLE_SELECTION_DUPLICATE"),
	sentinelMessage(ErrTableNotFound, http.StatusNotFound, "DINE_IN_TABLE_NOT_FOUND"),
	// The wrapped message names the Table so staff know which one to free;
	// it contains no data the caller could not already see.
	errorMessage(ErrTableUnavailable, http.StatusConflict, "DINE_IN_TABLE_UNAVAILABLE"),
	sentinelMessage(ErrTakeawayTablesNotAvailable, http.StatusConflict, "TAKEAWAY_TABLE_ASSIGNMENT_NOT_AVAILABLE"),

	errorMessage(ErrRequestConflict, http.StatusConflict, "REQUEST_CONFLICT"),
	{Target: ErrInvalidStoredResult, Spec: response.ErrorSpec{Status: http.StatusInternalServerError, Code: "INVALID_STORED_RESULT", Message: "an unexpected error occurred"}},
	{Target: ErrServiceSequenceExhausted, Spec: response.ErrorSpec{Status: http.StatusInternalServerError, Code: "SERVICE_SEQUENCE_EXHAUSTED", Message: "an unexpected error occurred"}},

	// Both denial sentinels collapse to one code so the API never discloses
	// which condition failed. The reason is in the server log and the audit
	// event.
	{Target: ErrForbidden, Spec: response.ErrorSpec{Status: http.StatusForbidden, Code: "NOT_AUTHORIZED", Message: "not authorized"}},
	{Target: ErrUnauthorized, Spec: response.ErrorSpec{Status: http.StatusUnauthorized, Code: "NOT_AUTHORIZED", Message: "not authorized"}},

	// ErrInvalidQuantity is wrapped in response.ErrInvalid by its caller, so
	// it lands on the generic validation code alongside every other bad field.
	errorMessage(response.ErrInvalid, http.StatusBadRequest, "INVALID_INPUT"),
	errorMessage(ErrInvalidQuantity, http.StatusBadRequest, "INVALID_INPUT"),

	sentinelMessage(ErrEmptyDraft, http.StatusConflict, "EMPTY_DRAFT"),
	sentinelMessage(ErrCommitMenuItemRetired, http.StatusConflict, "COMMIT_MENU_ITEM_RETIRED"),
	sentinelMessage(ErrCommitMenuItemUnavailable, http.StatusConflict, "COMMIT_MENU_ITEM_UNAVAILABLE"),
	sentinelMessage(ErrCommitSizeRequired, http.StatusConflict, "COMMIT_SIZE_REQUIRED"),
	sentinelMessage(ErrCommitSizeInvalid, http.StatusConflict, "COMMIT_SIZE_INVALID"),
	sentinelMessage(ErrCommitSizeRetired, http.StatusConflict, "COMMIT_SIZE_RETIRED"),
	sentinelMessage(ErrCommitSizeUnavailable, http.StatusConflict, "COMMIT_SIZE_UNAVAILABLE"),
	sentinelMessage(ErrCommitModifierOptionInvalid, http.StatusConflict, "COMMIT_MODIFIER_OPTION_INVALID"),
	sentinelMessage(ErrCommitModifierOptionRetired, http.StatusConflict, "COMMIT_MODIFIER_OPTION_RETIRED"),
	sentinelMessage(ErrCommitModifierOptionUnavailable, http.StatusConflict, "COMMIT_MODIFIER_OPTION_UNAVAILABLE"),
	sentinelMessage(ErrCommitModifierGroupRetired, http.StatusConflict, "COMMIT_MODIFIER_GROUP_RETIRED"),
	sentinelMessage(ErrCommitModifierGroupInvalid, http.StatusConflict, "COMMIT_MODIFIER_GROUP_INVALID"),
	sentinelMessage(ErrNewOrderDraftNotAvailable, http.StatusConflict, "NEW_ORDER_DRAFT_NOT_AVAILABLE"),
	sentinelMessage(ErrCheckNotFound, http.StatusNotFound, "CHECK_NOT_FOUND"),
	sentinelMessage(ErrCheckNotOpen, http.StatusConflict, "CHECK_NOT_OPEN"),
	sentinelMessage(ErrCheckHasPayment, http.StatusConflict, "CHECK_HAS_PAYMENT"),
	sentinelMessage(ErrCheckHasChargeAdjustment, http.StatusConflict, "CHECK_HAS_CHARGE_ADJUSTMENT"),
	sentinelMessage(ErrChecksDifferentSession, http.StatusConflict, "CHECKS_DIFFERENT_SERVICE_SESSION"),
	sentinelMessage(ErrPaymentExceedsBalance, http.StatusConflict, "PAYMENT_EXCEEDS_CHECK_BALANCE"),
	sentinelMessage(ErrInsufficientCashTendered, http.StatusConflict, "INSUFFICIENT_CASH_TENDERED"),
	sentinelMessage(ErrManualQRReceiptRequired, http.StatusConflict, "MANUAL_QR_RECEIPT_CONFIRMATION_REQUIRED"),
	sentinelMessage(ErrInvalidCheckSplit, http.StatusConflict, "INVALID_CHECK_SPLIT"),
	sentinelMessage(ErrSplitAllocationNotFound, http.StatusConflict, "SPLIT_ALLOCATION_NOT_FOUND"),
	sentinelMessage(ErrSplitQuantityExceedsAllocation, http.StatusConflict, "SPLIT_QUANTITY_EXCEEDS_ALLOCATION"),
	sentinelMessage(ErrSplitSourceWouldBeEmpty, http.StatusConflict, "SPLIT_SOURCE_WOULD_BE_EMPTY"),
	sentinelMessage(ErrSplitDestinationWouldBeEmpty, http.StatusConflict, "SPLIT_DESTINATION_WOULD_BE_EMPTY"),
	sentinelMessage(ErrInvalidCheckMerge, http.StatusConflict, "INVALID_CHECK_MERGE"),
	sentinelMessage(ErrLineTotalOutOfRange, http.StatusUnprocessableEntity, "LINE_TOTAL_OUT_OF_RANGE"),
	sentinelMessage(ErrCheckChargeOutOfRange, http.StatusUnprocessableEntity, "CHECK_CHARGE_OUT_OF_RANGE"),
	sentinelMessage(ErrInvalidCheckTarget, http.StatusUnprocessableEntity, "INVALID_CHECK_TARGET"),
	sentinelMessage(ErrNothingAwaitingSubmission, http.StatusConflict, "NOTHING_AWAITING_SUBMISSION"),
	sentinelMessage(ErrPaymentRequiresRefund, http.StatusConflict, "PAYMENT_REQUIRES_REFUND"),
	sentinelMessage(ErrSessionHasOrder, http.StatusConflict, "SESSION_HAS_ORDER"),
	sentinelMessage(ErrNothingToSubmit, http.StatusConflict, "NOTHING_TO_SUBMIT"),
	sentinelMessage(ErrCheckNotSettledForSubmission, http.StatusConflict, "CHECK_NOT_SETTLED_FOR_SUBMISSION"),
	sentinelMessage(ErrCheckNotSettledForClosure, http.StatusConflict, "CHECK_NOT_SETTLED_FOR_CLOSURE"),
	sentinelMessage(ErrAwaitingSubmissionForClosure, http.StatusConflict, "AWAITING_SUBMISSION_FOR_CLOSURE"),
	sentinelMessage(ErrPendingRefundForClosure, http.StatusConflict, "PENDING_REFUND_FOR_CLOSURE"),
	sentinelMessage(ErrUnsubmittedWorkForClosure, http.StatusConflict, "UNSUBMITTED_WORK_FOR_CLOSURE"),
	sentinelMessage(ErrOrderRequiredForClosure, http.StatusConflict, "ORDER_REQUIRED_FOR_CLOSURE"),
	sentinelMessage(ErrUnfulfilledPreparationForClosure, http.StatusConflict, "UNFULFILLED_PREPARATION_FOR_CLOSURE"),
	sentinelMessage(ErrCompletedSaleNotFound, http.StatusNotFound, "COMPLETED_SALE_NOT_FOUND"),
	sentinelMessage(ErrWasteNotFound, http.StatusNotFound, "WASTE_NOT_FOUND"),
	sentinelMessage(ErrCompSourceNotCharged, http.StatusConflict, "COMP_SOURCE_NOT_CHARGED"),
	sentinelMessage(ErrWasteAlreadyComped, http.StatusConflict, "WASTE_ALREADY_COMPED"),
	sentinelMessage(ErrChargeAdjustmentConflict, http.StatusConflict, "CHARGE_ADJUSTMENT_CONFLICT"),
	sentinelMessage(ErrRefundNotFound, http.StatusNotFound, "REFUND_NOT_FOUND"),
	sentinelMessage(ErrRefundSourceNotFound, http.StatusNotFound, "REFUND_SOURCE_NOT_FOUND"),
	sentinelMessage(ErrRefundAllocationInvalid, http.StatusBadRequest, "REFUND_ALLOCATION_INVALID"),
	sentinelMessage(ErrRefundExceedsAdjustmentCapacity, http.StatusConflict, "REFUND_EXCEEDS_ADJUSTMENT_CAPACITY"),
	sentinelMessage(ErrRefundExceedsPaymentCapacity, http.StatusConflict, "REFUND_EXCEEDS_PAYMENT_CAPACITY"),
	sentinelMessage(ErrRefundExceedsPendingRefund, http.StatusConflict, "REFUND_EXCEEDS_PENDING_REFUND"),
	sentinelMessage(ErrRefundMethodMismatch, http.StatusConflict, "REFUND_METHOD_MISMATCH"),
	sentinelMessage(ErrRefundAlreadyCompleted, http.StatusConflict, "REFUND_ALREADY_COMPLETED"),
	sentinelMessage(ErrPaymentNotFound, http.StatusNotFound, "PAYMENT_NOT_FOUND"),
	sentinelMessage(ErrPaymentAlreadyVoided, http.StatusConflict, "PAYMENT_ALREADY_VOIDED"),
	sentinelMessage(ErrPaymentHasRefund, http.StatusConflict, "PAYMENT_HAS_REFUND"),
	sentinelMessage(ErrPaymentVoidShiftClosed, http.StatusConflict, "PAYMENT_VOID_SHIFT_CLOSED"),
}

// sentinelMessage maps target to a client message of the sentinel's own text,
// never the wrapped detail.
func sentinelMessage(target error, status int, code string) response.ErrorRule {
	return response.ErrorRule{
		Target: target,
		Spec:   response.ErrorSpec{Status: status, Code: code, Message: target.Error()},
	}
}

// errorMessage maps target to a client message of the full error text, for
// sentinels whose wrapped detail is safe and useful to the caller.
func errorMessage(target error, status int, code string) response.ErrorRule {
	return response.ErrorRule{Target: target, Spec: response.ErrorSpec{Status: status, Code: code}}
}

// MapHTTPError maps Sales domain errors and input validation errors to
// *response.CodedError. A nil error, an error already carrying a
// *response.CodedError, and an unmatched error are returned unchanged.
func MapHTTPError(err error) error {
	return httpErrors.Map(err)
}
