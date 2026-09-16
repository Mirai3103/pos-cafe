package preparation_test

import (
	"net/http"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/preparation"
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
