//go:build integration

package sales_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/httpvalidator"
	"github.com/Mirai3103/pos-cafe/internal/preparation"
	"github.com/Mirai3103/pos-cafe/internal/sales"
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

// newTestServer builds an Echo server with auth, shift, sales, and
// preparation routes mounted the same way cmd/api/main.go mounts them. Shift
// is included because a Session can only open against a Shift opened through
// the Shift API; preparation is included because a Waste must be recorded
// through its real route before Comp can consume it.
func newTestServer(t *testing.T) (*echo.Echo, *sql.DB, *sqlc.Queries) {
	t.Helper()
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)

	e := echo.New()
	e.Validator = httpvalidator.New()
	v1 := e.Group("/api/v1")

	authSlices := auth.NewSlices(db, q)
	authSlices.RegisterRoutes(v1)

	shiftSlices := shift.NewSlices(db, q)
	shiftSlices.RegisterRoutes(v1, authSlices.Middleware)

	salesSlices := sales.NewSlices(db, q)
	salesSlices.RegisterRoutes(v1, authSlices.Middleware)

	preparationSlices := preparation.NewSlices(db, q)
	preparationSlices.RegisterRoutes(v1, authSlices.Middleware)

	return e, db, q
}

// signIn creates an identity with the given roles and returns a bearer token.
// It uses the real sign-in endpoint so the middleware path is exercised.
func signIn(t *testing.T, e *echo.Echo, q *sqlc.Queries, roles []string, pin string) string {
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
	return payload.Token
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

// openShiftOverHTTP opens a Sales Shift through the Shift API and returns its
// id. Every Sales mutation needs an open Shift; opening it through the API
// keeps the whole happy path on real routes.
func openShiftOverHTTP(t *testing.T, e *echo.Echo, token string) uuid.UUID {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"request_id": uuid.New(), "opening_float_vnd": 500000})
	rec := doRequest(t, e, http.MethodPost, "/api/v1/shifts", token, body)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var env envelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	var opened struct {
		ID uuid.UUID `json:"id"`
	}
	require.NoError(t, json.Unmarshal(env.Data, &opened))
	return opened.ID
}

// openTakeawayOverHTTP opens a Takeaway Session through the API and returns
// the decoded projection.
func openTakeawayOverHTTP(t *testing.T, e *echo.Echo, token string) sales.ServiceSessionResponse {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"request_id": uuid.New()})
	rec := doRequest(t, e, http.MethodPost, "/api/v1/sales/service-sessions/takeaway", token, body)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	return decodeSession(t, rec)
}

// decodeSession decodes the envelope of a successful Sales response into the
// Service Session projection, asserting the envelope shape on the way.
func decodeSession(t *testing.T, rec *httptest.ResponseRecorder) sales.ServiceSessionResponse {
	t.Helper()
	var env envelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	assert.True(t, env.Success, "response must be a success envelope: %s", rec.Body.String())
	require.NotNil(t, env.Data, "success responses must carry data: %s", rec.Body.String())
	require.Nil(t, env.Error, "success responses must carry no error: %s", rec.Body.String())

	var session sales.ServiceSessionResponse
	require.NoError(t, json.Unmarshal(env.Data, &session))
	return session
}

func TestSalesHTTPHappyPath(t *testing.T) {
	e, db, q := newTestServer(t)
	token := signIn(t, e, q, []string{"CASHIER"}, "2468")

	shiftID := openShiftOverHTTP(t, e, token)
	itemID := seedMenuItem(t, db, "Cà phê sữa", 25000)

	// Open a Takeaway Session: 201 with the full projection.
	session := openTakeawayOverHTTP(t, e, token)
	assert.Equal(t, "S00001", session.ServiceNumber)
	assert.Equal(t, sales.ModeTakeaway, session.ServiceMode)
	assert.Equal(t, sales.StateActive, session.State)
	assert.Equal(t, shiftID, session.SalesShiftID)
	assert.Nil(t, session.CustomerIdentityID)
	assert.Empty(t, session.Tables)
	require.NotNil(t, session.Draft)
	assert.Equal(t, sales.DraftStateEditable, session.Draft.State)
	assert.Empty(t, session.Draft.Items)
	// Present and empty until their sub-phase fills them.
	assert.Empty(t, session.Checks)
	assert.Empty(t, session.Orders)
	assert.Empty(t, session.PreparationUnits)

	sessionID := session.ID

	// Add an item: 200 with the projection, draft now holds one line.
	body, _ := json.Marshal(map[string]any{"request_id": uuid.New(), "menu_item_id": itemID})
	rec := doRequest(t, e, http.MethodPost,
		"/api/v1/sales/service-sessions/"+sessionID.String()+"/draft/items", token, body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	session = decodeSession(t, rec)
	require.Len(t, session.Draft.Items, 1)
	added := session.Draft.Items[0]
	assert.Equal(t, itemID, added.MenuItemID)
	assert.Equal(t, "Cà phê sữa", added.Name)
	require.NotNil(t, added.PriceVND)
	assert.Equal(t, int64(25000), *added.PriceVND)
	assert.Equal(t, int32(1), added.Quantity)
	assert.True(t, added.Available)
	assert.Empty(t, added.SelectedModifierOptions)
	draftItemID := added.ID

	// Set its quantity: 200, quantity is absolute.
	body, _ = json.Marshal(map[string]any{"request_id": uuid.New(), "quantity": 3})
	rec = doRequest(t, e, http.MethodPatch,
		"/api/v1/sales/service-sessions/"+sessionID.String()+"/draft/items/"+draftItemID.String()+"/quantity",
		token, body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	session = decodeSession(t, rec)
	require.Len(t, session.Draft.Items, 1)
	assert.Equal(t, int32(3), session.Draft.Items[0].Quantity)

	// Set its preparation note: 200, trimmed.
	body, _ = json.Marshal(map[string]any{"request_id": uuid.New(), "preparation_note": "  ít đá  "})
	rec = doRequest(t, e, http.MethodPatch,
		"/api/v1/sales/service-sessions/"+sessionID.String()+"/draft/items/"+draftItemID.String()+"/preparation-note",
		token, body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	session = decodeSession(t, rec)
	require.NotNil(t, session.Draft.Items[0].PreparationNote)
	assert.Equal(t, "ít đá", *session.Draft.Items[0].PreparationNote)

	// Remove it: DELETE answers 200 with the projection body, never 204, so a
	// client sees the resulting draft without a follow-up read.
	body, _ = json.Marshal(map[string]any{"request_id": uuid.New()})
	rec = doRequest(t, e, http.MethodDelete,
		"/api/v1/sales/service-sessions/"+sessionID.String()+"/draft/items/"+draftItemID.String(),
		token, body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.NotEqual(t, http.StatusNoContent, rec.Code)
	session = decodeSession(t, rec)
	assert.Empty(t, session.Draft.Items)

	// Read the Session back: 200 with the same identity and the emptied draft.
	rec = doRequest(t, e, http.MethodGet,
		"/api/v1/sales/service-sessions/"+sessionID.String(), token, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	session = decodeSession(t, rec)
	assert.Equal(t, sessionID, session.ID)
	assert.Equal(t, "S00001", session.ServiceNumber)
	require.NotNil(t, session.Draft)
	assert.Empty(t, session.Draft.Items)
}

func TestSalesHTTPSerializesEmptyCollectionsAsArrays(t *testing.T) {
	e, _, q := newTestServer(t)
	token := signIn(t, e, q, []string{"CASHIER"}, "2468")

	// The open-tabs view over an empty world: data must be [], never null and
	// never omitted. Asserted on the raw JSON, because unmarshalling hides the
	// difference between [] and null.
	rec := doRequest(t, e, http.MethodGet, "/api/v1/sales/service-sessions", token, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"data":[]`)
	assert.NotContains(t, rec.Body.String(), `"data":null`)

	shiftID := openShiftOverHTTP(t, e, token)
	session := openTakeawayOverHTTP(t, e, token)

	rec = doRequest(t, e, http.MethodGet,
		"/api/v1/sales/service-sessions/"+session.ID.String(), token, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	raw := rec.Body.String()

	for _, collection := range []string{"tables", "items", "checks", "orders", "preparation_units"} {
		assert.Contains(t, raw, fmt.Sprintf(`"%s":[]`, collection),
			"empty %s must serialize as []", collection)
		assert.NotContains(t, raw, fmt.Sprintf(`"%s":null`, collection),
			"empty %s must never serialize as null", collection)
	}
	// The one null that is correct: an anonymous Session has no customer.
	assert.Contains(t, raw, `"customer_identity_id":null`)
	assert.Equal(t, shiftID, session.SalesShiftID)
}

// salesRoutes enumerates every Sales operation for the denial tests. Bodies
// are well-formed; the denial happens in the middleware chain before any
// handler logic runs, but valid requests keep the test honest about what is
// being denied.
func salesRoutes() []struct {
	name   string
	method string
	path   string
	body   map[string]any
} {
	sessionID := uuid.NewString()
	itemID := uuid.NewString()
	sessionPath := "/api/v1/sales/service-sessions/" + sessionID
	itemPath := sessionPath + "/draft/items/" + itemID

	return []struct {
		name   string
		method string
		path   string
		body   map[string]any
	}{
		{"list sessions", http.MethodGet, "/api/v1/sales/service-sessions", nil},
		{"get session", http.MethodGet, sessionPath, nil},
		{"open takeaway", http.MethodPost, "/api/v1/sales/service-sessions/takeaway",
			map[string]any{"request_id": uuid.New()}},
		{"open dine-in", http.MethodPost, "/api/v1/sales/service-sessions/dine-in",
			map[string]any{"request_id": uuid.New(), "table_ids": []uuid.UUID{uuid.New()}}},
		{"set tables", http.MethodPut, sessionPath + "/tables",
			map[string]any{"request_id": uuid.New(), "table_ids": []uuid.UUID{}}},
		{"add draft item", http.MethodPost, sessionPath + "/draft/items",
			map[string]any{"request_id": uuid.New(), "menu_item_id": uuid.New()}},
		{"set quantity", http.MethodPatch, itemPath + "/quantity",
			map[string]any{"request_id": uuid.New(), "quantity": 1}},
		{"set size", http.MethodPatch, itemPath + "/size",
			map[string]any{"request_id": uuid.New()}},
		{"set note", http.MethodPatch, itemPath + "/preparation-note",
			map[string]any{"request_id": uuid.New(), "preparation_note": "khoa"}},
		{"set modifiers", http.MethodPatch, itemPath + "/modifiers",
			map[string]any{"request_id": uuid.New(), "modifier_option_ids": []uuid.UUID{}}},
		{"remove item", http.MethodDelete, itemPath,
			map[string]any{"request_id": uuid.New()}},
		{"commit draft", http.MethodPost, sessionPath + "/draft/commit",
			map[string]any{"request_id": uuid.New()}},
		{"start new draft", http.MethodPost, sessionPath + "/draft",
			map[string]any{"request_id": uuid.New()}},
		{"set check target", http.MethodPut, sessionPath + "/draft/check-target",
			map[string]any{"request_id": uuid.New(), "check_target": "NEW_CHECK"}},
		{"comp waste", http.MethodPost, "/api/v1/sales/wastes/" + uuid.NewString() + "/comp",
			map[string]any{
				"request_id": uuid.New(),
				"reason":     "CAFE_ERROR",
				"manager_approval": map[string]any{
					"approver_login_code": "MGR001",
					"manager_pin":         "1234",
				},
			}},
		{"record refund", http.MethodPost, "/api/v1/sales/refunds",
			map[string]any{
				"request_id": uuid.New(),
				"check_id":   uuid.New(),
				"method":     "CASH",
				"adjustment_allocations": []map[string]any{
					{"charge_adjustment_id": uuid.New(), "amount_vnd": 1000},
				},
				"payment_allocations": []map[string]any{
					{"payment_id": uuid.New(), "amount_vnd": 1000},
				},
				"reason": "CUSTOMER_REQUEST",
				"manager_approval": map[string]any{
					"approver_login_code": "MGR001",
					"manager_pin":         "1234",
				},
			}},
		{"confirm refund", http.MethodPost,
			"/api/v1/sales/refunds/" + uuid.NewString() + "/confirm",
			map[string]any{"request_id": uuid.New()}},
		{"void payment", http.MethodPost,
			"/api/v1/sales/payments/" + uuid.NewString() + "/void",
			map[string]any{
				"request_id": uuid.New(),
				"reason":     "WRONG_AMOUNT",
				"manager_approval": map[string]any{
					"approver_login_code": "MGR001",
					"manager_pin":         "1234",
				},
			}},
	}
}

func TestSalesHTTPDeniesBaristaOnEveryRoute(t *testing.T) {
	e, _, q := newTestServer(t)
	// A BARISTA holds preparation.operate but no sales.operate, so every Sales
	// route must deny them.
	token := signIn(t, e, q, []string{"BARISTA"}, "1357")

	for _, route := range salesRoutes() {
		t.Run(route.name+" denies barista", func(t *testing.T) {
			body, _ := json.Marshal(route.body)
			rec := doRequest(t, e, route.method, route.path, token, body)
			require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

			var env envelope
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
			require.NotNil(t, env.Error)
			// The capability gate lives in the auth middleware, whose denial
			// code is FORBIDDEN. NOT_AUTHORIZED is the Sales executor's own
			// collapsed denial code for authority lost between the middleware
			// and the transaction; the middleware answers first here.
			assert.Equal(t, "FORBIDDEN", env.Error.Code)
		})
	}
}

func TestSalesHTTPDeniesAnonymousOnEveryRoute(t *testing.T) {
	e, _, _ := newTestServer(t)

	for _, route := range salesRoutes() {
		t.Run(route.name+" denies anonymous", func(t *testing.T) {
			body, _ := json.Marshal(route.body)
			rec := doRequest(t, e, route.method, route.path, "", body)
			require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())

			var env envelope
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
			require.NotNil(t, env.Error)
			assert.Equal(t, "UNAUTHORIZED", env.Error.Code)
		})
	}
}

func TestSalesHTTPValidation(t *testing.T) {
	e, _, q := newTestServer(t)
	token := signIn(t, e, q, []string{"CASHIER"}, "2468")
	_ = openShiftOverHTTP(t, e, token)

	t.Run("malformed session id", func(t *testing.T) {
		rec := doRequest(t, e, http.MethodGet,
			"/api/v1/sales/service-sessions/not-a-uuid", token, nil)
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())

		var env envelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
		require.NotNil(t, env.Error)
		assert.Equal(t, "INVALID_INPUT", env.Error.Code)
	})

	t.Run("malformed item id", func(t *testing.T) {
		session := openTakeawayOverHTTP(t, e, token)
		body, _ := json.Marshal(map[string]any{"request_id": uuid.New(), "quantity": 1})
		rec := doRequest(t, e, http.MethodPatch,
			"/api/v1/sales/service-sessions/"+session.ID.String()+"/draft/items/not-a-uuid/quantity",
			token, body)
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())

		var env envelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
		require.NotNil(t, env.Error)
		assert.Equal(t, "INVALID_INPUT", env.Error.Code)
	})

	t.Run("missing request_id", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{})
		rec := doRequest(t, e, http.MethodPost,
			"/api/v1/sales/service-sessions/takeaway", token, body)
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())

		var env envelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
		require.NotNil(t, env.Error)
		assert.Equal(t, "INVALID_INPUT", env.Error.Code)
	})
}

func TestSalesHTTPModifierOptionIDsAbsentVersusEmpty(t *testing.T) {
	e, db, q := newTestServer(t)
	token := signIn(t, e, q, []string{"CASHIER"}, "2468")
	_ = openShiftOverHTTP(t, e, token)

	// The Item's effective Group declares one default Option.
	itemID, defaultOptionID := seedItemWithDefaultOption(t, db)

	// Session A omits modifier_option_ids entirely: the menu's defaults apply.
	sessionA := openTakeawayOverHTTP(t, e, token)
	body, _ := json.Marshal(map[string]any{"request_id": uuid.New(), "menu_item_id": itemID})
	rec := doRequest(t, e, http.MethodPost,
		"/api/v1/sales/service-sessions/"+sessionA.ID.String()+"/draft/items", token, body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	sessionA = decodeSession(t, rec)
	require.Len(t, sessionA.Draft.Items, 1)
	require.Len(t, sessionA.Draft.Items[0].SelectedModifierOptions, 1,
		"an absent modifier_option_ids must apply the menu's default option")
	assert.Equal(t, defaultOptionID, sessionA.Draft.Items[0].SelectedModifierOptions[0].ID)
	assert.Contains(t, rec.Body.String(), "Ít đá",
		"the applied default option must be visible in the raw JSON")

	// Session B sends an explicit empty array: no options, no defaults.
	sessionB := openTakeawayOverHTTP(t, e, token)
	body, _ = json.Marshal(map[string]any{
		"request_id":          uuid.New(),
		"menu_item_id":        itemID,
		"modifier_option_ids": []uuid.UUID{},
	})
	rec = doRequest(t, e, http.MethodPost,
		"/api/v1/sales/service-sessions/"+sessionB.ID.String()+"/draft/items", token, body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	sessionB = decodeSession(t, rec)
	require.Len(t, sessionB.Draft.Items, 1)
	assert.Empty(t, sessionB.Draft.Items[0].SelectedModifierOptions,
		"an explicit empty modifier_option_ids must apply no options")
	assert.Contains(t, rec.Body.String(), `"selected_modifier_options":[]`,
		"the empty selection must be [] in the raw JSON, not null")

	// The two requests differ only in the presence of the field, and the two
	// drafts came out different: the distinction survived JSON decoding over
	// the wire.
	assert.Len(t, sessionA.Draft.Items[0].SelectedModifierOptions, 1)
	assert.Len(t, sessionB.Draft.Items[0].SelectedModifierOptions, 0)
	assert.NotEqual(t, sessionA.ID, sessionB.ID)
}
