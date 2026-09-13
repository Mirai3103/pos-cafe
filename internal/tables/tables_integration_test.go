//go:build integration

package tables_test

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
	"github.com/Mirai3103/pos-cafe/internal/tables"
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

// newTestServer builds an Echo server with auth and tables routes mounted the
// same way cmd/api/main.go mounts them.
func newTestServer(t *testing.T) (*echo.Echo, *sqlc.Queries) {
	t.Helper()
	db, q := openTablesTestDB(t)

	e := echo.New()
	e.Validator = httpvalidator.New()
	v1 := e.Group("/api/v1")

	authSlices := auth.NewSlices(db, q)
	authSlices.RegisterRoutes(v1)

	tablesSlices := tables.NewSlices(db, q)
	tablesSlices.RegisterRoutes(v1, authSlices.Middleware)

	return e, q
}

// signIn creates an identity with the given roles and returns a bearer token.
// It uses the real sign-in endpoint so the middleware path is exercised.
func signIn(t *testing.T, e *echo.Echo, q *sqlc.Queries, roles []string) string {
	t.Helper()
	ctx := context.Background()

	loginCode := testLoginCode("H")
	pin := "2468"
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
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestTablesHTTPHappyPath(t *testing.T) {
	e, q := newTestServer(t)
	token := signIn(t, e, q, []string{"MANAGER"})

	name := uniqueTableName("HTTP Ban")
	body, _ := json.Marshal(map[string]any{"request_id": uuid.New(), "name": name})
	rec := doRequest(t, e, http.MethodPost, "/api/v1/tables", token, body)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var env envelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	assert.True(t, env.Success)

	var created tables.TableResponse
	require.NoError(t, json.Unmarshal(env.Data, &created))
	assert.Equal(t, name, created.Name)
	assert.True(t, created.Available)

	// Rename via the route parameter.
	newName := uniqueTableName("HTTP Ban renamed")
	body, _ = json.Marshal(map[string]any{"request_id": uuid.New(), "name": newName})
	rec = doRequest(t, e, http.MethodPatch,
		"/api/v1/tables/"+created.ID.String()+"/name", token, body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// Set availability.
	body, _ = json.Marshal(map[string]any{"request_id": uuid.New(), "available": false})
	rec = doRequest(t, e, http.MethodPatch,
		"/api/v1/tables/"+created.ID.String()+"/availability", token, body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// Overview includes the Table with an empty occupancy array.
	rec = doRequest(t, e, http.MethodGet, "/api/v1/tables/overview", token, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"current_service_sessions":[]`)
}

func TestTablesHTTPRejectsUnauthenticated(t *testing.T) {
	e, _ := newTestServer(t)

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/tables/overview"},
		{http.MethodPost, "/api/v1/tables"},
		{http.MethodPatch, "/api/v1/tables/" + uuid.NewString() + "/name"},
		{http.MethodPatch, "/api/v1/tables/" + uuid.NewString() + "/availability"},
	} {
		rec := doRequest(t, e, tc.method, tc.path, "", []byte(`{}`))
		assert.Equal(t, http.StatusUnauthorized, rec.Code, "%s %s", tc.method, tc.path)
	}
}

func TestTablesHTTPCapabilityMapping(t *testing.T) {
	e, q := newTestServer(t)

	t.Run("a barista cannot read the overview", func(t *testing.T) {
		token := signIn(t, e, q, []string{"BARISTA"})
		rec := doRequest(t, e, http.MethodGet, "/api/v1/tables/overview", token, nil)
		assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	})

	t.Run("a cashier reads the overview but cannot create", func(t *testing.T) {
		token := signIn(t, e, q, []string{"CASHIER"})

		rec := doRequest(t, e, http.MethodGet, "/api/v1/tables/overview", token, nil)
		assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		body, _ := json.Marshal(map[string]any{
			"request_id": uuid.New(), "name": uniqueTableName("Cashier Ban"),
		})
		rec = doRequest(t, e, http.MethodPost, "/api/v1/tables", token, body)
		assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	})
}

func TestTablesHTTPValidation(t *testing.T) {
	e, q := newTestServer(t)
	token := signIn(t, e, q, []string{"MANAGER"})

	t.Run("rejects an invalid table_id", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"request_id": uuid.New(), "name": "Ban"})
		rec := doRequest(t, e, http.MethodPatch, "/api/v1/tables/not-a-uuid/name", token, body)
		assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())

		var env envelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
		require.NotNil(t, env.Error)
		assert.Equal(t, "INVALID_INPUT", env.Error.Code)
	})

	t.Run("rejects a missing request_id", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"name": uniqueTableName("No request id")})
		rec := doRequest(t, e, http.MethodPost, "/api/v1/tables", token, body)
		assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	})

	t.Run("reports a name conflict as 409 TABLE_NAME_CONFLICT", func(t *testing.T) {
		name := uniqueTableName("Conflict Ban")
		body, _ := json.Marshal(map[string]any{"request_id": uuid.New(), "name": name})
		rec := doRequest(t, e, http.MethodPost, "/api/v1/tables", token, body)
		require.Equal(t, http.StatusCreated, rec.Code)

		body, _ = json.Marshal(map[string]any{"request_id": uuid.New(), "name": name})
		rec = doRequest(t, e, http.MethodPost, "/api/v1/tables", token, body)
		require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())

		var env envelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
		require.NotNil(t, env.Error)
		assert.Equal(t, "TABLE_NAME_CONFLICT", env.Error.Code)
	})

	t.Run("reports a missing Table as 404", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"request_id": uuid.New(), "name": uniqueTableName("Ghost Ban"),
		})
		rec := doRequest(t, e, http.MethodPatch,
			"/api/v1/tables/"+uuid.NewString()+"/name", token, body)
		assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	})

	t.Run("rejects a missing available field", func(t *testing.T) {
		name := uniqueTableName("No available")
		body, _ := json.Marshal(map[string]any{"request_id": uuid.New(), "name": name})
		rec := doRequest(t, e, http.MethodPost, "/api/v1/tables", token, body)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

		var env envelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
		var created tables.TableResponse
		require.NoError(t, json.Unmarshal(env.Data, &created))

		body, _ = json.Marshal(map[string]any{"request_id": uuid.New()})
		rec = doRequest(t, e, http.MethodPatch,
			"/api/v1/tables/"+created.ID.String()+"/availability", token, body)
		assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	})

	t.Run("reports a conflicting request_id reuse as 409 REQUEST_CONFLICT", func(t *testing.T) {
		requestID := uuid.New()
		body, _ := json.Marshal(map[string]any{
			"request_id": requestID, "name": uniqueTableName("Idem Ban"),
		})
		rec := doRequest(t, e, http.MethodPost, "/api/v1/tables", token, body)
		require.Equal(t, http.StatusCreated, rec.Code)

		body, _ = json.Marshal(map[string]any{
			"request_id": requestID, "name": uniqueTableName("Idem Ban different"),
		})
		rec = doRequest(t, e, http.MethodPost, "/api/v1/tables", token, body)
		require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())

		var env envelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
		require.NotNil(t, env.Error)
		assert.Equal(t, "REQUEST_CONFLICT", env.Error.Code)
	})
}

func TestTablesHTTPReplayReturnsStoredResponse(t *testing.T) {
	e, q := newTestServer(t)
	token := signIn(t, e, q, []string{"MANAGER"})

	requestID := uuid.New()
	name := uniqueTableName("Replay Ban")
	body, _ := json.Marshal(map[string]any{"request_id": requestID, "name": name})

	first := doRequest(t, e, http.MethodPost, "/api/v1/tables", token, body)
	require.Equal(t, http.StatusCreated, first.Code, first.Body.String())

	second := doRequest(t, e, http.MethodPost, "/api/v1/tables", token, body)
	require.Equal(t, http.StatusCreated, second.Code, second.Body.String())
	assert.JSONEq(t, first.Body.String(), second.Body.String(),
		"an exact replay must return the stored response verbatim")
}
