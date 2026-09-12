package tables_test

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/Mirai3103/pos-cafe/internal/tables"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMapDBError(t *testing.T) {
	t.Run("nil stays nil", func(t *testing.T) {
		require.NoError(t, tables.MapDBError(nil))
	})

	t.Run("no rows becomes not found", func(t *testing.T) {
		err := tables.MapDBError(sql.ErrNoRows)
		require.ErrorIs(t, err, tables.ErrTableNotFound)
	})

	t.Run("unique violation becomes name conflict", func(t *testing.T) {
		err := tables.MapDBError(&pgconn.PgError{Code: "23505", Message: "duplicate key"})
		require.ErrorIs(t, err, tables.ErrNameConflict)
	})

	t.Run("foreign key violation becomes not found", func(t *testing.T) {
		err := tables.MapDBError(&pgconn.PgError{Code: "23503", Message: "fk violation"})
		require.ErrorIs(t, err, tables.ErrTableNotFound)
	})

	t.Run("unknown error passes through", func(t *testing.T) {
		sentinel := errors.New("boom")
		require.ErrorIs(t, tables.MapDBError(sentinel), sentinel)
	})
}

func TestMapHTTPError(t *testing.T) {
	cases := []struct {
		name     string
		in       error
		wantCode string
		wantHTTP int
	}{
		{"not found", tables.ErrTableNotFound, "TABLE_NOT_FOUND", http.StatusNotFound},
		{"name conflict", tables.ErrNameConflict, "TABLE_NAME_CONFLICT", http.StatusConflict},
		{"request conflict", tables.ErrRequestConflict, "REQUEST_CONFLICT", http.StatusConflict},
		{"forbidden", tables.ErrForbidden, "FORBIDDEN", http.StatusForbidden},
		{"unauthorized", tables.ErrUnauthorized, "UNAUTHORIZED", http.StatusUnauthorized},
		{"invalid stored result", tables.ErrInvalidStoredResult, "INVALID_STORED_RESULT", http.StatusInternalServerError},
		{"invalid input", response.ErrInvalid, "INVALID_INPUT", http.StatusBadRequest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mapped := tables.MapHTTPError(fmt.Errorf("%w: context", tc.in))
			var coded *response.CodedError
			require.ErrorAs(t, mapped, &coded)
			assert.Equal(t, tc.wantCode, coded.Code)
			assert.Equal(t, tc.wantHTTP, coded.Status)
		})
	}

	t.Run("nil stays nil", func(t *testing.T) {
		require.NoError(t, tables.MapHTTPError(nil))
	})

	t.Run("stored result error hides detail from the client", func(t *testing.T) {
		mapped := tables.MapHTTPError(fmt.Errorf("%w: raw body 0xdeadbeef", tables.ErrInvalidStoredResult))
		var coded *response.CodedError
		require.ErrorAs(t, mapped, &coded)
		assert.NotContains(t, coded.Message, "0xdeadbeef")
	})
}
