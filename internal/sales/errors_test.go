package sales

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func codedFrom(t *testing.T, err error) *response.CodedError {
	t.Helper()
	var coded *response.CodedError
	require.ErrorAs(t, MapHTTPError(err), &coded)
	return coded
}

func TestMapHTTPErrorCodes(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"open shift required", ErrOpenShiftRequired, http.StatusConflict, "OPEN_SALES_SHIFT_REQUIRED"},
		{"session not found", ErrServiceSessionNotFound, http.StatusNotFound, "SERVICE_SESSION_NOT_FOUND"},
		{"session closed", ErrServiceSessionClosed, http.StatusConflict, "SERVICE_SESSION_ALREADY_CLOSED"},
		{"draft not found", ErrEditableDraftNotFound, http.StatusConflict, "EDITABLE_DRAFT_NOT_FOUND"},
		{"draft item not found", ErrDraftItemNotFound, http.StatusNotFound, "DRAFT_ITEM_NOT_FOUND"},
		{"menu item not found", ErrMenuItemNotFound, http.StatusNotFound, "MENU_ITEM_NOT_FOUND"},
		{"menu item unavailable", ErrMenuItemUnavailable, http.StatusConflict, "MENU_ITEM_UNAVAILABLE"},
		{"menu item retired", ErrMenuItemRetired, http.StatusConflict, "MENU_ITEM_RETIRED"},
		{"size not found", ErrSizeNotFound, http.StatusNotFound, "SIZE_NOT_FOUND"},
		{"size unavailable", ErrSizeUnavailable, http.StatusConflict, "SIZE_UNAVAILABLE"},
		{"size retired", ErrSizeRetired, http.StatusConflict, "SIZE_RETIRED"},
		{"option not found", ErrModifierOptionNotFound, http.StatusNotFound, "MODIFIER_OPTION_NOT_FOUND"},
		{"option unavailable", ErrModifierOptionUnavailable, http.StatusConflict, "MODIFIER_OPTION_UNAVAILABLE"},
		{"option retired", ErrModifierOptionRetired, http.StatusConflict, "MODIFIER_OPTION_RETIRED"},
		{"bad note", ErrInvalidPreparationNote, http.StatusBadRequest, "INVALID_PREPARATION_NOTE"},
		{"bad quantity", ErrInvalidQuantity, http.StatusBadRequest, "INVALID_INPUT"},
		{"table selection required", ErrTableSelectionRequired, http.StatusBadRequest, "DINE_IN_TABLE_SELECTION_REQUIRED"},
		{"table selection duplicate", ErrTableSelectionDuplicate, http.StatusBadRequest, "DINE_IN_TABLE_SELECTION_DUPLICATE"},
		{"table not found", ErrTableNotFound, http.StatusNotFound, "DINE_IN_TABLE_NOT_FOUND"},
		{"table unavailable", ErrTableUnavailable, http.StatusConflict, "DINE_IN_TABLE_UNAVAILABLE"},
		{"takeaway has no tables", ErrTakeawayTablesNotAvailable, http.StatusConflict, "TAKEAWAY_TABLE_ASSIGNMENT_NOT_AVAILABLE"},
		{"request conflict", ErrRequestConflict, http.StatusConflict, "REQUEST_CONFLICT"},
		{"stored result", ErrInvalidStoredResult, http.StatusInternalServerError, "INVALID_STORED_RESULT"},
		{"forbidden", ErrForbidden, http.StatusForbidden, "NOT_AUTHORIZED"},
		{"unauthorized", ErrUnauthorized, http.StatusUnauthorized, "NOT_AUTHORIZED"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			coded := codedFrom(t, tc.err)
			assert.Equal(t, tc.status, coded.Status)
			assert.Equal(t, tc.code, coded.Code)
		})
	}
}

// Both denial sentinels collapse to one client-visible code, so the API never
// discloses whether an identity exists, is disabled, or merely lacks a role.
func TestDenialsShareOneClientCode(t *testing.T) {
	assert.Equal(t, "NOT_AUTHORIZED", codedFrom(t, ErrForbidden).Code)
	assert.Equal(t, "NOT_AUTHORIZED", codedFrom(t, ErrUnauthorized).Code)
}

// A wrapped PostgreSQL detail is server-side evidence and must not reach the
// client message.
func TestWrappedDetailStaysServerSide(t *testing.T) {
	err := fmt.Errorf("%w: %s", ErrOpenShiftRequired, "Key (sales_shift_id)=(...) is not present")
	coded := codedFrom(t, err)
	assert.NotContains(t, coded.Message, "sales_shift_id")
	assert.Equal(t, ErrOpenShiftRequired.Error(), coded.Message)
}

func TestMapDBError(t *testing.T) {
	t.Run("service session FK means no open shift", func(t *testing.T) {
		pgErr := &pgconn.PgError{
			Code:           "23503",
			ConstraintName: serviceSessionSalesShiftFK,
			Detail:         "Key (sales_shift_id)=(x) is not present in table \"sales_shifts\".",
		}
		assert.ErrorIs(t, MapDBError(pgErr), ErrOpenShiftRequired)
	})

	t.Run("an unrelated FK is a defect not a business state", func(t *testing.T) {
		pgErr := &pgconn.PgError{Code: "23503", ConstraintName: "some_other_fkey"}
		mapped := MapDBError(pgErr)
		assert.NotErrorIs(t, mapped, ErrOpenShiftRequired)
		assert.Equal(t, pgErr, mapped)
	})

	t.Run("check violation is never mapped", func(t *testing.T) {
		pgErr := &pgconn.PgError{Code: "23514", ConstraintName: "order_draft_item_quantity_valid"}
		assert.Equal(t, pgErr, MapDBError(pgErr))
	})

	t.Run("nil stays nil", func(t *testing.T) {
		assert.NoError(t, MapDBError(nil))
	})
}

func TestMapHTTPErrorPassesThroughCodedAndUnknown(t *testing.T) {
	coded := response.NewCodedError(http.StatusTeapot, "TEAPOT", "teapot", nil)
	assert.Equal(t, error(coded), MapHTTPError(coded))

	unknown := errors.New("boom")
	assert.Equal(t, unknown, MapHTTPError(unknown))

	assert.NoError(t, MapHTTPError(nil))
}

func TestMapHTTPErrorCommitCodes(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{ErrEmptyDraft, http.StatusConflict, "EMPTY_DRAFT"},
		{ErrCommitMenuItemUnavailable, http.StatusConflict, "COMMIT_MENU_ITEM_UNAVAILABLE"},
		{ErrCommitMenuItemRetired, http.StatusConflict, "COMMIT_MENU_ITEM_RETIRED"},
		{ErrCommitSizeRequired, http.StatusConflict, "COMMIT_SIZE_REQUIRED"},
		{ErrCommitSizeInvalid, http.StatusConflict, "COMMIT_SIZE_INVALID"},
		{ErrCommitSizeUnavailable, http.StatusConflict, "COMMIT_SIZE_UNAVAILABLE"},
		{ErrCommitSizeRetired, http.StatusConflict, "COMMIT_SIZE_RETIRED"},
		{ErrCommitModifierOptionInvalid, http.StatusConflict, "COMMIT_MODIFIER_OPTION_INVALID"},
		{ErrCommitModifierOptionUnavailable, http.StatusConflict, "COMMIT_MODIFIER_OPTION_UNAVAILABLE"},
		{ErrCommitModifierOptionRetired, http.StatusConflict, "COMMIT_MODIFIER_OPTION_RETIRED"},
		{ErrCommitModifierGroupInvalid, http.StatusConflict, "COMMIT_MODIFIER_GROUP_INVALID"},
		{ErrCommitModifierGroupRetired, http.StatusConflict, "COMMIT_MODIFIER_GROUP_RETIRED"},
		{ErrNewOrderDraftNotAvailable, http.StatusConflict, "NEW_ORDER_DRAFT_NOT_AVAILABLE"},
		{ErrLineTotalOutOfRange, http.StatusUnprocessableEntity, "LINE_TOTAL_OUT_OF_RANGE"},
		{ErrCheckChargeOutOfRange, http.StatusUnprocessableEntity, "CHECK_CHARGE_OUT_OF_RANGE"},
		{ErrInvalidCheckTarget, http.StatusUnprocessableEntity, "INVALID_CHECK_TARGET"},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			coded := codedFrom(t, fmt.Errorf("wrapped: %w", tc.err))
			require.Equal(t, tc.status, coded.Status)
			require.Equal(t, tc.code, coded.Code)
		})
	}
}

// The stored-charge invariant is a defect detector, never a business state:
// it must not reach the client as a recognizable code. MapHTTPError leaves it
// unmapped, so response.Error surfaces it as a logged 500.
func TestChargeInvariantViolationIsNotAClientCode(t *testing.T) {
	mapped := MapHTTPError(fmt.Errorf("wrapped: %w", ErrChargeInvariantViolated))
	var coded *response.CodedError
	assert.NotErrorAs(t, mapped, &coded)
}

func TestMapHTTPErrorPhase5C(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"check not found", ErrCheckNotFound, http.StatusNotFound, "CHECK_NOT_FOUND"},
		{"check not open", ErrCheckNotOpen, http.StatusConflict, "CHECK_NOT_OPEN"},
		{"check has payment", ErrCheckHasPayment, http.StatusConflict, "CHECK_HAS_PAYMENT"},
		{"different session", ErrChecksDifferentSession, http.StatusConflict, "CHECKS_DIFFERENT_SERVICE_SESSION"},
		{"over balance", ErrPaymentExceedsBalance, http.StatusConflict, "PAYMENT_EXCEEDS_CHECK_BALANCE"},
		{"under tender", ErrInsufficientCashTendered, http.StatusConflict, "INSUFFICIENT_CASH_TENDERED"},
		{"receipt required", ErrManualQRReceiptRequired, http.StatusConflict, "MANUAL_QR_RECEIPT_CONFIRMATION_REQUIRED"},
		{"invalid split", ErrInvalidCheckSplit, http.StatusConflict, "INVALID_CHECK_SPLIT"},
		{"allocation missing", ErrSplitAllocationNotFound, http.StatusConflict, "SPLIT_ALLOCATION_NOT_FOUND"},
		{"quantity exceeds", ErrSplitQuantityExceedsAllocation, http.StatusConflict, "SPLIT_QUANTITY_EXCEEDS_ALLOCATION"},
		{"source empty", ErrSplitSourceWouldBeEmpty, http.StatusConflict, "SPLIT_SOURCE_WOULD_BE_EMPTY"},
		{"destination empty", ErrSplitDestinationWouldBeEmpty, http.StatusConflict, "SPLIT_DESTINATION_WOULD_BE_EMPTY"},
		{"invalid merge", ErrInvalidCheckMerge, http.StatusConflict, "INVALID_CHECK_MERGE"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var coded *response.CodedError
			require.ErrorAs(t, MapHTTPError(fmt.Errorf("wrapped: %w", tc.err)), &coded)
			require.Equal(t, tc.status, coded.Status)
			require.Equal(t, tc.code, coded.Code)
		})
	}
}

// The settlement invariant is a defect, not a business state. It must reach
// the client as an unmapped 500, exactly as the charge invariant does.
func TestSettlementInvariantIsNotMapped(t *testing.T) {
	err := MapHTTPError(fmt.Errorf("wrapped: %w", ErrSettlementInvariantViolated))
	var coded *response.CodedError
	require.False(t, errors.As(err, &coded),
		"a settlement invariant violation must not become a client-visible code")
}

func TestPhase5DErrorMapping(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{ErrCheckNotSettledForSubmission, http.StatusConflict, "CHECK_NOT_SETTLED_FOR_SUBMISSION"},
		{ErrCheckNotSettledForClosure, http.StatusConflict, "CHECK_NOT_SETTLED_FOR_CLOSURE"},
		{ErrUnsubmittedWorkForClosure, http.StatusConflict, "UNSUBMITTED_WORK_FOR_CLOSURE"},
		{ErrOrderRequiredForClosure, http.StatusConflict, "ORDER_REQUIRED_FOR_CLOSURE"},
		{ErrUnfulfilledPreparationForClosure, http.StatusConflict, "UNFULFILLED_PREPARATION_FOR_CLOSURE"},
		{ErrCompletedSaleNotFound, http.StatusNotFound, "COMPLETED_SALE_NOT_FOUND"},
		{ErrNothingToSubmit, http.StatusConflict, "NOTHING_TO_SUBMIT"},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			coded := codedFrom(t, tc.err)
			require.Equal(t, tc.status, coded.Status)
			require.Equal(t, tc.code, coded.Code)
		})
	}
}

// TestPhase6CRefundErrorMapping pins every Refund condition code, including
// the aggregate pending-obligation overrun, which is its own condition rather
// than a source-capacity exhaustion.
func TestPhase6CRefundErrorMapping(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{ErrRefundNotFound, http.StatusNotFound, "REFUND_NOT_FOUND"},
		{ErrRefundSourceNotFound, http.StatusNotFound, "REFUND_SOURCE_NOT_FOUND"},
		{ErrRefundAllocationInvalid, http.StatusBadRequest, "REFUND_ALLOCATION_INVALID"},
		{ErrRefundExceedsAdjustmentCapacity, http.StatusConflict, "REFUND_EXCEEDS_ADJUSTMENT_CAPACITY"},
		{ErrRefundExceedsPaymentCapacity, http.StatusConflict, "REFUND_EXCEEDS_PAYMENT_CAPACITY"},
		{ErrRefundExceedsPendingRefund, http.StatusConflict, "REFUND_EXCEEDS_PENDING_REFUND"},
		{ErrRefundMethodMismatch, http.StatusConflict, "REFUND_METHOD_MISMATCH"},
		{ErrRefundAlreadyCompleted, http.StatusConflict, "REFUND_ALREADY_COMPLETED"},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			coded := codedFrom(t, fmt.Errorf("wrapped: %w", tc.err))
			require.Equal(t, tc.status, coded.Status)
			require.Equal(t, tc.code, coded.Code)
		})
	}
}
