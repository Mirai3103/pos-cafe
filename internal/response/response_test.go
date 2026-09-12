package response_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponseHelpers(t *testing.T) {
	t.Parallel()
	e := echo.New()

	t.Run("OK helper", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		c := e.NewContext(httptest.NewRequest(http.MethodGet, "/", nil), rec)

		err := response.OK(c, map[string]string{"foo": "bar"})
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)

		var resp response.APIResponse
		err = json.Unmarshal(rec.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.True(t, resp.Success)
		assert.NotNil(t, resp.Data)
	})

	t.Run("Created helper", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		c := e.NewContext(httptest.NewRequest(http.MethodPost, "/", nil), rec)

		err := response.Created(c, "created_id_1")
		require.NoError(t, err)
		assert.Equal(t, http.StatusCreated, rec.Code)

		var resp response.APIResponse
		err = json.Unmarshal(rec.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.True(t, resp.Success)
	})

	t.Run("NoContent helper", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		c := e.NewContext(httptest.NewRequest(http.MethodDelete, "/", nil), rec)

		err := response.NoContent(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusNoContent, rec.Code)
	})
}

func TestErrorMapping(t *testing.T) {
	t.Parallel()
	e := echo.New()

	tests := []struct {
		name         string
		err          error
		expectedCode int
		expectedKey  string
	}{
		{
			name:         "nil error returns nil",
			err:          nil,
			expectedCode: 0,
		},
		{
			name:         "ErrNotFound maps to 404",
			err:          fmt.Errorf("%w: item not found", response.ErrNotFound),
			expectedCode: http.StatusNotFound,
			expectedKey:  "NOT_FOUND",
		},
		{
			name:         "ErrConflict maps to 409",
			err:          fmt.Errorf("%w: duplicate key", response.ErrConflict),
			expectedCode: http.StatusConflict,
			expectedKey:  "CONFLICT",
		},
		{
			name:         "ErrManagerInvariant maps to 409",
			err:          fmt.Errorf("%w: final enabled manager", response.ErrManagerInvariant),
			expectedCode: http.StatusConflict,
			expectedKey:  "FINAL_ENABLED_MANAGER_REQUIRED",
		},
		{
			name:         "ErrInvalid maps to 400",
			err:          fmt.Errorf("%w: bad input", response.ErrInvalid),
			expectedCode: http.StatusBadRequest,
			expectedKey:  "BAD_REQUEST",
		},
		{
			name:         "ErrUnauthorized maps to 401",
			err:          fmt.Errorf("%w: auth required", response.ErrUnauthorized),
			expectedCode: http.StatusUnauthorized,
			expectedKey:  "UNAUTHORIZED",
		},
		{
			name:         "ErrForbidden maps to 403",
			err:          fmt.Errorf("%w: access forbidden", response.ErrForbidden),
			expectedCode: http.StatusForbidden,
			expectedKey:  "FORBIDDEN",
		},
		{
			name:         "ErrTooManyRequests maps to 429",
			err:          fmt.Errorf("%w: retry later", response.ErrTooManyRequests),
			expectedCode: http.StatusTooManyRequests,
			expectedKey:  "TOO_MANY_REQUESTS",
		},
		{
			name:         "Generic error maps to 500",
			err:          errors.New("something catastrophic"),
			expectedCode: http.StatusInternalServerError,
			expectedKey:  "INTERNAL_ERROR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			c := e.NewContext(httptest.NewRequest(http.MethodGet, "/test", nil), rec)

			err := response.Error(c, tt.err)
			require.NoError(t, err)

			if tt.err == nil {
				assert.Equal(t, http.StatusOK, rec.Code) // default recorder code if nothing written
				return
			}

			assert.Equal(t, tt.expectedCode, rec.Code)
			var resp response.APIResponse
			unmarshalErr := json.Unmarshal(rec.Body.Bytes(), &resp)
			require.NoError(t, unmarshalErr)
			assert.False(t, resp.Success)
			assert.Equal(t, tt.expectedKey, resp.Error.Code)
		})
	}
}

func TestCodedErrorMapping(t *testing.T) {
	t.Parallel()
	e := echo.New()

	tests := []struct {
		name           string
		err            error
		expectedCode   int
		expectedStatus string
		expectedMsg    string
	}{
		{
			name:           "coded error returns exact status and code",
			err:            response.NewCodedError(409, "CATALOG_NAME_CONFLICT", "Tên đã tồn tại", nil),
			expectedCode:   http.StatusConflict,
			expectedStatus: "CATALOG_NAME_CONFLICT",
			expectedMsg:    "Tên đã tồn tại",
		},
		{
			name:           "coded error with cause returns its own code",
			err:            response.NewCodedError(400, "INVALID_PRICING_CONFIGURATION", "bad pricing", response.ErrInvalid),
			expectedCode:   http.StatusBadRequest,
			expectedStatus: "INVALID_PRICING_CONFIGURATION",
			expectedMsg:    "bad pricing",
		},
		{
			name:           "coded 404 error",
			err:            response.NewCodedError(404, "CATALOG_NOT_FOUND", "item not found", nil),
			expectedCode:   http.StatusNotFound,
			expectedStatus: "CATALOG_NOT_FOUND",
			expectedMsg:    "item not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			c := e.NewContext(httptest.NewRequest(http.MethodGet, "/test", nil), rec)

			err := response.Error(c, tt.err)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedCode, rec.Code)

			var resp response.APIResponse
			unmarshalErr := json.Unmarshal(rec.Body.Bytes(), &resp)
			require.NoError(t, unmarshalErr)
			assert.False(t, resp.Success)
			assert.Equal(t, tt.expectedStatus, resp.Error.Code)
			assert.Equal(t, tt.expectedMsg, resp.Error.Message)
		})
	}
}
