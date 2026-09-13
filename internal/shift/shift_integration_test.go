//go:build integration

package shift_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/httpvalidator"
	"github.com/Mirai3103/pos-cafe/internal/shift"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type envelope struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// newTestServer builds an Echo server with auth and shift routes mounted the
// same way cmd/api/main.go mounts them.
func newTestServer(t *testing.T) (*echo.Echo, *sqlc.Queries) {
	t.Helper()
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)

	e := echo.New()
	e.Validator = httpvalidator.New()
	v1 := e.Group("/api/v1")

	authSlices := auth.NewSlices(db, q)
	authSlices.RegisterRoutes(v1)

	shiftSlices := shift.NewSlices(db, q)
	shiftSlices.RegisterRoutes(v1, authSlices.Middleware)

	return e, q
}

// signIn creates an identity with the given roles and returns a bearer token.
// It uses the real sign-in endpoint so the middleware path is exercised.
func signIn(t *testing.T, e *echo.Echo, q *sqlc.Queries, roles []string, pin string) (string, string) {
	t.Helper()
	ctx := context.Background()

	loginCode := testLoginCode("H")
	hash, err := auth.HashPin(pin)
	require.NoError(t, err)

	row, err := q.CreateStaffIdentity(ctx, sqlc.CreateStaffIdentityParams{
		DisplayName: "HTTP Test " + loginCode,
		Btrim:       loginCode,
		PinHash:     hash,
		Enabled:     true,
	})
	require.NoError(t, err)
	for _, role := range roles {
		require.NoError(t, q.AddStaffRole(ctx, sqlc.AddStaffRoleParams{
			StaffIdentityID: row.ID, Role: role,
		}))
	}

	body, _ := json.Marshal(map[string]string{"login_code": loginCode, "pin": pin})
	rec := doRequest(t, e, http.MethodPost, "/api/v1/auth/sign-in", "", body)
	require.Equal(t, http.StatusOK, rec.Code, "sign-in failed: %s", rec.Body.String())

	var env envelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	var payload struct {
		Token string `json:"token"`
	}
	require.NoError(t, json.Unmarshal(env.Data, &payload))
	require.NotEmpty(t, payload.Token)
	return payload.Token, loginCode
}

func doRequest(t *testing.T, e *echo.Echo, method, path, token string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	reader := bytes.NewReader(body)
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestShiftHTTPHappyPath(t *testing.T) {
	e, q := newTestServer(t)
	cashierToken, _ := signIn(t, e, q, []string{"CASHIER"}, "2468")
	_, managerCode := signIn(t, e, q, []string{"MANAGER"}, "8642")

	// No Shift is open yet: 200 with data null, never 404.
	rec := doRequest(t, e, http.MethodGet, "/api/v1/shifts/current", cashierToken, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var env envelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	assert.True(t, env.Success)
	assert.JSONEq(t, "null", string(env.Data))

	// Open a Shift.
	body, _ := json.Marshal(map[string]any{"request_id": uuid.New(), "opening_float_vnd": 500000})
	rec = doRequest(t, e, http.MethodPost, "/api/v1/shifts", cashierToken, body)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	var opened shift.SalesShiftResponse
	require.NoError(t, json.Unmarshal(env.Data, &opened))
	assert.Equal(t, shift.StateOpen, opened.State)
	assert.Equal(t, int64(500000), opened.OpeningFloatVND)

	// Record a Cash Movement with Manager approval.
	body, _ = json.Marshal(map[string]any{
		"request_id":          uuid.New(),
		"method":              shift.MethodPayOut,
		"amount_vnd":          50000,
		"reason":              shift.ReasonSafeDrop,
		"approver_login_code": managerCode,
		"manager_pin":         "8642",
	})
	rec = doRequest(t, e, http.MethodPost,
		"/api/v1/shifts/"+opened.ID.String()+"/cash-movements", cashierToken, body)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	var result shift.CashMovementResult
	require.NoError(t, json.Unmarshal(env.Data, &result))
	assert.Equal(t, int64(450000), result.ExpectedCashVND)

	// The current read now reports the movement.
	rec = doRequest(t, e, http.MethodGet, "/api/v1/shifts/current", cashierToken, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	var current shift.CurrentSalesShiftResponse
	require.NoError(t, json.Unmarshal(env.Data, &current))
	assert.Equal(t, int64(450000), current.ExpectedCashVND)
	require.Len(t, current.CashMovements, 1)
	assert.Equal(t, result.Movement.ID, current.CashMovements[0].ID)
}

func TestShiftHTTPSerializesEmptyMovementsAsArray(t *testing.T) {
	e, q := newTestServer(t)
	token, _ := signIn(t, e, q, []string{"CASHIER"}, "2468")

	body, _ := json.Marshal(map[string]any{"request_id": uuid.New(), "opening_float_vnd": 500000})
	rec := doRequest(t, e, http.MethodPost, "/api/v1/shifts", token, body)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	rec = doRequest(t, e, http.MethodGet, "/api/v1/shifts/current", token, nil)
	require.Equal(t, http.StatusOK, rec.Code)

	// Assert on the raw JSON: an empty list must be [] and never null.
	assert.Contains(t, rec.Body.String(), `"cash_movements":[]`)
	assert.NotContains(t, rec.Body.String(), `"cash_movements":null`)
}

func TestShiftHTTPAuthorization(t *testing.T) {
	e, q := newTestServer(t)
	baristaToken, _ := signIn(t, e, q, []string{"BARISTA"}, "1357")

	openBody, _ := json.Marshal(map[string]any{"request_id": uuid.New(), "opening_float_vnd": 500000})
	movementBody, _ := json.Marshal(map[string]any{
		"request_id": uuid.New(), "method": shift.MethodPayIn, "amount_vnd": 1000,
		"reason": shift.ReasonAddChangeFund, "approver_login_code": "ZZ", "manager_pin": "8642",
	})

	cases := []struct {
		name   string
		method string
		path   string
		body   []byte
	}{
		{"current", http.MethodGet, "/api/v1/shifts/current", nil},
		{"open", http.MethodPost, "/api/v1/shifts", openBody},
		{"cash movement", http.MethodPost,
			"/api/v1/shifts/" + uuid.New().String() + "/cash-movements", movementBody},
	}
	for _, tc := range cases {
		t.Run(tc.name+" denies barista", func(t *testing.T) {
			rec := doRequest(t, e, tc.method, tc.path, baristaToken, tc.body)
			assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
		})
		t.Run(tc.name+" denies anonymous", func(t *testing.T) {
			rec := doRequest(t, e, tc.method, tc.path, "", tc.body)
			assert.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
		})
	}
}

func TestShiftHTTPValidation(t *testing.T) {
	e, q := newTestServer(t)
	token, _ := signIn(t, e, q, []string{"CASHIER"}, "2468")

	openBody, _ := json.Marshal(map[string]any{"request_id": uuid.New(), "opening_float_vnd": 500000})
	rec := doRequest(t, e, http.MethodPost, "/api/v1/shifts", token, openBody)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var env envelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	var opened shift.SalesShiftResponse
	require.NoError(t, json.Unmarshal(env.Data, &opened))

	cases := []struct {
		name string
		path string
		body map[string]any
	}{
		{
			name: "missing request_id",
			path: "/api/v1/shifts",
			body: map[string]any{"opening_float_vnd": 500000},
		},
		{
			// Zero is a valid float, so a missing field must be rejected rather
			// than defaulted.
			name: "missing opening_float_vnd",
			path: "/api/v1/shifts",
			body: map[string]any{"request_id": uuid.New()},
		},
		{
			name: "missing amount_vnd",
			path: "/api/v1/shifts/" + opened.ID.String() + "/cash-movements",
			body: map[string]any{
				"request_id": uuid.New(), "method": shift.MethodPayIn,
				"reason": shift.ReasonAddChangeFund,
				"approver_login_code": "ZZ", "manager_pin": "8642",
			},
		},
		{
			name: "malformed manager_pin",
			path: "/api/v1/shifts/" + opened.ID.String() + "/cash-movements",
			body: map[string]any{
				"request_id": uuid.New(), "method": shift.MethodPayIn, "amount_vnd": 1000,
				"reason": shift.ReasonAddChangeFund,
				"approver_login_code": "ZZ", "manager_pin": "abc",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(tc.body)
			rec := doRequest(t, e, http.MethodPost, tc.path, token, body)
			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())

			var errEnv envelope
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errEnv))
			require.NotNil(t, errEnv.Error)
			assert.Equal(t, "INVALID_INPUT", errEnv.Error.Code)
		})
	}

	t.Run("malformed shift_id", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"request_id": uuid.New(), "method": shift.MethodPayIn, "amount_vnd": 1000,
			"reason": shift.ReasonAddChangeFund,
			"approver_login_code": "ZZ", "manager_pin": "8642",
		})
		rec := doRequest(t, e, http.MethodPost, "/api/v1/shifts/not-a-uuid/cash-movements", token, body)
		assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	})
}

func TestShiftHTTPErrorCodes(t *testing.T) {
	e, q := newTestServer(t)
	cashierToken, _ := signIn(t, e, q, []string{"CASHIER"}, "2468")
	_, managerCode := signIn(t, e, q, []string{"MANAGER"}, "8642")

	openBody, _ := json.Marshal(map[string]any{"request_id": uuid.New(), "opening_float_vnd": 500000})
	rec := doRequest(t, e, http.MethodPost, "/api/v1/shifts", cashierToken, openBody)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var env envelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	var opened shift.SalesShiftResponse
	require.NoError(t, json.Unmarshal(env.Data, &opened))

	t.Run("second open conflicts", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"request_id": uuid.New(), "opening_float_vnd": 700000})
		rec := doRequest(t, e, http.MethodPost, "/api/v1/shifts", cashierToken, body)
		require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())

		var errEnv envelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errEnv))
		require.NotNil(t, errEnv.Error)
		assert.Equal(t, "SALES_SHIFT_ALREADY_OPEN", errEnv.Error.Code)
	})

	t.Run("wrong manager pin is forbidden without naming the reason", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"request_id": uuid.New(), "method": shift.MethodPayOut, "amount_vnd": 50000,
			"reason": shift.ReasonSafeDrop,
			"approver_login_code": managerCode, "manager_pin": "0000",
		})
		rec := doRequest(t, e, http.MethodPost,
			"/api/v1/shifts/"+opened.ID.String()+"/cash-movements", cashierToken, body)
		require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

		var errEnv envelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errEnv))
		require.NotNil(t, errEnv.Error)
		assert.Equal(t, "MANAGER_APPROVAL_UNAVAILABLE", errEnv.Error.Code)
		assert.NotContains(t, errEnv.Error.Message, "INVALID_PIN")
		assert.NotContains(t, errEnv.Error.Message, "IDENTITY_DISABLED")
	})

	t.Run("unknown shift conflicts", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"request_id": uuid.New(), "method": shift.MethodPayOut, "amount_vnd": 50000,
			"reason": shift.ReasonSafeDrop,
			"approver_login_code": managerCode, "manager_pin": "8642",
		})
		rec := doRequest(t, e, http.MethodPost,
			"/api/v1/shifts/"+uuid.New().String()+"/cash-movements", cashierToken, body)
		require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())

		var errEnv envelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errEnv))
		require.NotNil(t, errEnv.Error)
		assert.Equal(t, "OPEN_SALES_SHIFT_REQUIRED", errEnv.Error.Code)
	})
}

func TestShiftHTTPStaffSummaryFieldsAreExactlyThree(t *testing.T) {
	e, q := newTestServer(t)
	token, _ := signIn(t, e, q, []string{"CASHIER"}, "2468")

	body, _ := json.Marshal(map[string]any{"request_id": uuid.New(), "opening_float_vnd": 500000})
	rec := doRequest(t, e, http.MethodPost, "/api/v1/shifts", token, body)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var env envelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	var payload struct {
		Opener map[string]any `json:"opener"`
	}
	require.NoError(t, json.Unmarshal(env.Data, &payload))

	// No pin hash, role list, enablement flag, or session detail may cross the
	// Shift boundary.
	assert.Len(t, payload.Opener, 3)
	for _, key := range []string{"id", "display_name", "login_code"} {
		assert.Contains(t, payload.Opener, key)
	}
}
