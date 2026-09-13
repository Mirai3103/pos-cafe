package shift_test

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/Mirai3103/pos-cafe/internal/shift"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMapDBErrorMapsConstraintFailures(t *testing.T) {
	assert.NoError(t, shift.MapDBError(nil))

	unique := &pgconn.PgError{Code: "23505", Message: "duplicate key", Detail: "sales_shift_only_one_open_unique"}
	assert.ErrorIs(t, shift.MapDBError(unique), shift.ErrShiftAlreadyOpen)

	fk := &pgconn.PgError{Code: "23503", Message: "foreign key violation", ConstraintName: "cash_movements_sales_shift_id_fkey"}
	assert.ErrorIs(t, shift.MapDBError(fk), shift.ErrOpenShiftRequired)

	// A foreign key violation on any other column (initiator, its session, the
	// approver) is a defect, not this business state: it must pass through
	// unmapped so it surfaces as a logged 500.
	otherFK := &pgconn.PgError{Code: "23503", Message: "foreign key violation", ConstraintName: "cash_movements_approved_by_staff_identity_id_fkey"}
	mappedOtherFK := shift.MapDBError(otherFK)
	assert.NotErrorIs(t, mappedOtherFK, shift.ErrOpenShiftRequired)
	assert.ErrorIs(t, mappedOtherFK, otherFK)

	// A check violation is a defect, not a business state: it must pass through
	// unmapped so it surfaces as a logged 500.
	check := &pgconn.PgError{Code: "23514", Message: "cash_movement_note_valid"}
	mapped := shift.MapDBError(check)
	assert.NotErrorIs(t, mapped, shift.ErrShiftAlreadyOpen)
	assert.NotErrorIs(t, mapped, shift.ErrOpenShiftRequired)
	assert.ErrorIs(t, mapped, check)
}

func TestMapHTTPErrorStatusesAndCodes(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{shift.ErrShiftAlreadyOpen, http.StatusConflict, "SALES_SHIFT_ALREADY_OPEN"},
		{shift.ErrOpenShiftRequired, http.StatusConflict, "OPEN_SALES_SHIFT_REQUIRED"},
		{shift.ErrManagerApprovalUnavailable, http.StatusForbidden, "MANAGER_APPROVAL_UNAVAILABLE"},
		{shift.ErrRequestConflict, http.StatusConflict, "REQUEST_CONFLICT"},
		{shift.ErrExpectedCashOutOfRange, http.StatusBadRequest, "EXPECTED_CASH_OUT_OF_RANGE"},
		{shift.ErrForbidden, http.StatusForbidden, "FORBIDDEN"},
		{shift.ErrUnauthorized, http.StatusUnauthorized, "UNAUTHORIZED"},
		{shift.ErrInvalidStoredResult, http.StatusInternalServerError, "INVALID_STORED_RESULT"},
		{response.ErrInvalid, http.StatusBadRequest, "INVALID_INPUT"},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			wrapped := fmt.Errorf("context: %w", tc.err)
			mapped := shift.MapHTTPError(wrapped)

			var coded *response.CodedError
			require.True(t, errors.As(mapped, &coded), "expected a *response.CodedError")
			assert.Equal(t, tc.status, coded.Status)
			assert.Equal(t, tc.code, coded.Code)
		})
	}
}

func TestMapHTTPErrorHidesDBDetail(t *testing.T) {
	// A raw PostgreSQL constraint detail must never reach the client message,
	// even though MapDBError legitimately embeds it in the Go error chain for
	// server-side logging.
	unique := &pgconn.PgError{Code: "23505", Message: "duplicate key", Detail: "Key (state)=(OPEN) already exists."}
	mapped := shift.MapHTTPError(shift.MapDBError(unique))

	var coded *response.CodedError
	require.True(t, errors.As(mapped, &coded))
	assert.NotContains(t, coded.Message, "Key (state)")
}

func TestMapHTTPErrorHidesApprovalReason(t *testing.T) {
	// The denial reason belongs in the log and the audit event, never in the
	// client message, which must not reveal whether a login code exists.
	wrapped := fmt.Errorf("%w: IDENTITY_DISABLED", shift.ErrManagerApprovalUnavailable)
	mapped := shift.MapHTTPError(wrapped)

	var coded *response.CodedError
	require.True(t, errors.As(mapped, &coded))
	assert.NotContains(t, coded.Message, "IDENTITY_DISABLED")
	assert.NotContains(t, coded.Message, "INVALID_PIN")
}
