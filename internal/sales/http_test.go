//go:build integration

package sales_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/httpvalidator"
	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

// newSalesHTTPHarness mounts the stack cmd/api mounts — echo with the shared
// validator, the auth routes, and the Sales routes behind the real auth
// middleware, all against the test database — and signs a manager in through
// POST /api/v1/auth/sign-in, returning the app and the Bearer token. Auth
// sessions live only in Postgres, so there is no way to serve these routes
// with a mock middleware; a request that passes here passed the same
// middleware production uses.
func newSalesHTTPHarness(t *testing.T) (*echo.Echo, string) {
	t.Helper()
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)

	// seedActor mints access sessions directly, which produces a token hash no
	// client could present; this harness needs a presentable token, so the
	// manager identity carries a known PIN and signs in through the real
	// route. testLoginCode keeps the login code unique across runs.
	code := testLoginCode("HTTPMGR")
	pinHash, err := auth.HashPin("1234")
	require.NoError(t, err)
	identity, err := q.CreateStaffIdentity(context.Background(), sqlc.CreateStaffIdentityParams{
		DisplayName: "HTTP Manager " + code,
		Btrim:       code,
		PinHash:     pinHash,
		Enabled:     true,
	})
	require.NoError(t, err)
	require.NoError(t, q.AddStaffRole(context.Background(), sqlc.AddStaffRoleParams{
		StaffIdentityID: identity.ID,
		Role:            auth.RoleManager,
	}))

	e := echo.New()
	e.Validator = httpvalidator.New()
	v1 := e.Group("/api/v1")
	authSlices := auth.NewSlices(db, q)
	authSlices.RegisterRoutes(v1)
	salesSlices := sales.NewSlices(db, q)
	salesSlices.RegisterRoutes(v1, authSlices.Middleware)

	token := httpHarnessSignIn(t, e, code, "1234")
	return e, token
}

// httpHarnessSignIn signs in through the real auth route and returns the
// token it issued.
func httpHarnessSignIn(t *testing.T, e *echo.Echo, code, pin string) string {
	t.Helper()
	body, err := json.Marshal(auth.SignInRequest{LoginCode: code, Pin: pin})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sign-in", strings.NewReader(string(body)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var res struct {
		Success bool `json:"success"`
		Data    struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res), rec.Body.String())
	require.NotEmpty(t, res.Data.Token)
	return res.Data.Token
}

// performAuthorizedRequest sends a request with the harness manager's Bearer
// token and a raw JSON body, returning the raw recorder.
func performAuthorizedRequest(t *testing.T, e *echo.Echo, token, method, path, body string,
) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	req.Header.Set(echo.HeaderAuthorization, "Bearer "+token)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// A request that violates a 5C field shape must be answered by request
// validation, before any domain state is consulted — so none of these needs a
// Check to exist. The empty item list is the exception in shape only: it is
// well-formed JSON describing a split that cannot happen, so it is
// INVALID_CHECK_SPLIT (409), not a binding failure.
func TestPhase5CRequestValidation(t *testing.T) {
	e, token := newSalesHTTPHarness(t)

	cases := []struct {
		name   string
		method string
		path   string
		body   string
		status int
	}{
		{"cash with a non-positive applied amount", http.MethodPost,
			"/api/v1/sales/checks/" + uuid.New().String() + "/payments/cash",
			`{"request_id":"` + uuid.New().String() + `","applied_amount_vnd":0,"cash_tendered_vnd":1000}`,
			http.StatusBadRequest},
		{"cash with a malformed check id", http.MethodPost,
			"/api/v1/sales/checks/not-a-uuid/payments/cash",
			`{"request_id":"` + uuid.New().String() + `","applied_amount_vnd":1000,"cash_tendered_vnd":1000}`,
			http.StatusBadRequest},
		{"manual qr with an over-long reference", http.MethodPost,
			"/api/v1/sales/checks/" + uuid.New().String() + "/payments/manual-qr",
			`{"request_id":"` + uuid.New().String() + `","applied_amount_vnd":1000,` +
				`"receipt_observed_in_bank_app":true,"transaction_reference":"` + strings.Repeat("A", 101) + `"}`,
			http.StatusBadRequest},
		{"split with an unknown destination type", http.MethodPost,
			"/api/v1/sales/checks/" + uuid.New().String() + "/split",
			`{"request_id":"` + uuid.New().String() + `","destination":{"type":"SOMEWHERE"},` +
				`"items":[{"committed_item_id":"` + uuid.New().String() + `","quantity":1}]}`,
			http.StatusBadRequest},
		{"split with an empty item list", http.MethodPost,
			"/api/v1/sales/checks/" + uuid.New().String() + "/split",
			`{"request_id":"` + uuid.New().String() + `","destination":{"type":"NEW_CHECK"},"items":[]}`,
			http.StatusConflict},
		{"split to an existing check without an id", http.MethodPost,
			"/api/v1/sales/checks/" + uuid.New().String() + "/split",
			`{"request_id":"` + uuid.New().String() + `","destination":{"type":"EXISTING_CHECK"},` +
				`"items":[{"committed_item_id":"` + uuid.New().String() + `","quantity":1}]}`,
			http.StatusBadRequest},
		{"merge without a request id", http.MethodPost,
			"/api/v1/sales/checks/merge",
			`{"surviving_check_id":"` + uuid.New().String() + `","absorbed_check_id":"` + uuid.New().String() + `"}`,
			http.StatusBadRequest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := performAuthorizedRequest(t, e, token, tc.method, tc.path, tc.body)
			require.Equal(t, tc.status, rec.Code, rec.Body.String())
		})
	}
}
