package preparation_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/preparation"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/stretchr/testify/require"
)

func TestErrorMapping(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{preparation.ErrUnitNotFound, http.StatusNotFound, "PREPARATION_UNIT_NOT_FOUND"},
		{preparation.ErrInvalidTransition, http.StatusConflict, "INVALID_TRANSITION"},
		{preparation.ErrRequestConflict, http.StatusConflict, "REQUEST_CONFLICT"},
		{preparation.ErrInvalidStoredResult, http.StatusInternalServerError, "INVALID_STORED_RESULT"},
		// Both denial sentinels collapse to one code so the API never
		// discloses which condition failed. This copies internal/sales
		// verbatim per the brief's Step 4 note, which overrides the sample
		// table's "UNAUTHORIZED" cell: sales maps ErrUnauthorized to
		// NOT_AUTHORIZED (401), and spec §9.3 adds no UNAUTHORIZED code.
		{preparation.ErrForbidden, http.StatusForbidden, "NOT_AUTHORIZED"},
		{preparation.ErrUnauthorized, http.StatusUnauthorized, "NOT_AUTHORIZED"},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			status, body := preparation.ErrorResponse(tc.err)
			require.Equal(t, tc.status, status)
			require.Equal(t, tc.code, body.Error.Code)
		})
	}
}

// TestErrorResponse pins the stable Phase 6B status/code pairs. The mapping
// must resolve through errors.Is, so wrapped variants land on the same row.
func TestErrorResponse(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{preparation.ErrAlertNotFound, http.StatusNotFound, "PREPARATION_ALERT_NOT_FOUND"},
		{preparation.ErrWasteNotFound, http.StatusNotFound, "PREPARATION_WASTE_NOT_FOUND"},
		{preparation.ErrInvalidReason, http.StatusBadRequest, "INVALID_PREPARATION_REASON"},
		{preparation.ErrInvalidNote, http.StatusBadRequest, "INVALID_PREPARATION_NOTE"},
		{preparation.ErrAlertAlreadyAcknowledged, http.StatusConflict, "PREPARATION_ALERT_ALREADY_ACKNOWLEDGED"},
		{preparation.ErrWasteAlreadyRemade, http.StatusConflict, "PREPARATION_WASTE_ALREADY_REMADE"},
		{preparation.ErrServiceSessionClosed, http.StatusConflict, "SERVICE_SESSION_ALREADY_CLOSED"},
		// A well-formed PIN that fails self-authentication collapses into the
		// shared NOT_AUTHORIZED denial: the API never discloses which denial
		// condition failed.
		{preparation.ErrInvalidManagerPIN, http.StatusForbidden, "NOT_AUTHORIZED"},

		// errors.Is sees through wrapping.
		{fmt.Errorf("acknowledge alert %s: %w", "abc", preparation.ErrAlertNotFound),
			http.StatusNotFound, "PREPARATION_ALERT_NOT_FOUND"},
		{fmt.Errorf("remake waste %s: %w", "abc", preparation.ErrWasteAlreadyRemade),
			http.StatusConflict, "PREPARATION_WASTE_ALREADY_REMADE"},
		{fmt.Errorf("lock service session: %w", preparation.ErrServiceSessionClosed),
			http.StatusConflict, "SERVICE_SESSION_ALREADY_CLOSED"},

		// A missing or malformed PIN shape is a boundary error, never the
		// collapsed denial.
		{fmt.Errorf("%w: manager_pin must have 4 through 8 digits", response.ErrInvalid),
			http.StatusBadRequest, "INVALID_INPUT"},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			status, body := preparation.ErrorResponse(tc.err)
			require.Equal(t, tc.status, status)
			require.Equal(t, tc.code, body.Error.Code)
		})
	}
}

// TestErrorResponseClientMessages pins that the client sees the stable
// sentinel text (or the collapsed denial text), while wrapped detail —
// including PostgreSQL error detail — stays server-side only.
func TestErrorResponseClientMessages(t *testing.T) {
	status, body := preparation.ErrorResponse(
		fmt.Errorf("%w: %q is not a Waste reason", preparation.ErrInvalidReason, "SPOILED"))
	require.Equal(t, http.StatusBadRequest, status)
	require.Equal(t, "INVALID_PREPARATION_REASON", body.Error.Code)
	require.Equal(t, preparation.ErrInvalidReason.Error(), body.Error.Message)

	status, body = preparation.ErrorResponse(
		fmt.Errorf("insert preparation note: %w", preparation.ErrInvalidNote))
	require.Equal(t, http.StatusBadRequest, status)
	require.Equal(t, "INVALID_PREPARATION_NOTE", body.Error.Code)
	require.Equal(t, preparation.ErrInvalidNote.Error(), body.Error.Message)

	status, body = preparation.ErrorResponse(preparation.ErrInvalidManagerPIN)
	require.Equal(t, http.StatusForbidden, status)
	require.Equal(t, "NOT_AUTHORIZED", body.Error.Code)
	// Collapsed denial text, not the sentinel's own wording.
	require.Equal(t, "not authorized", body.Error.Message)
}
