//go:build integration

package preparation_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/httpvalidator"
	"github.com/Mirai3103/pos-cafe/internal/preparation"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// envelope mirrors the API's success/error wrapper, decoded only as far as the
// tests need.
type envelope struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// mountPreparationTestServer builds an Echo server with auth and Preparation
// routes mounted the same way cmd/api/main.go mounts them. It truncates
// nothing, so a populated test can seed its world first and mount over it.
func mountPreparationTestServer(db *sql.DB, q *sqlc.Queries) *echo.Echo {
	e := echo.New()
	e.Validator = httpvalidator.New()
	v1 := e.Group("/api/v1")
	authSlices := auth.NewSlices(db, q)
	authSlices.RegisterRoutes(v1)
	preparation.NewSlices(db, q).RegisterRoutes(v1, authSlices.Middleware)
	return e
}

// newPreparationTestServer truncates every Preparation and Sales table and
// mounts a fresh server over the clean database.
func newPreparationTestServer(t *testing.T) (*echo.Echo, *sqlc.Queries) {
	t.Helper()
	db, q := openPrepTestDB(t)
	truncatePrepTables(t, db)
	return mountPreparationTestServer(db, q), q
}

// signInPreparation creates an enabled identity with the given roles and
// returns a bearer token. It calls the real sign-in endpoint so the middleware
// path is exercised; it never constructs or hashes a bearer token itself.
func signInPreparation(t *testing.T, e *echo.Echo, q *sqlc.Queries, roles []string) string {
	t.Helper()
	ctx := context.Background()

	loginCode := testLoginCode("P")
	pin := "9753"
	hash, err := auth.HashPin(pin)
	require.NoError(t, err)

	row, err := q.CreateStaffIdentity(ctx, sqlc.CreateStaffIdentityParams{
		DisplayName: "Prep HTTP Test " + loginCode,
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

	body, err := json.Marshal(map[string]string{"login_code": loginCode, "pin": pin})
	require.NoError(t, err)
	rec := doPreparationRequest(t, e, http.MethodPost, "/api/v1/auth/sign-in", "", body)
	require.Equal(t, http.StatusOK, rec.Code, "sign-in failed: %s", rec.Body.String())

	var envl envelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envl))
	var payload struct {
		Token string `json:"token"`
	}
	require.NoError(t, json.Unmarshal(envl.Data, &payload))
	require.NotEmpty(t, payload.Token)
	return payload.Token
}

// doPreparationRequest sends a JSON request through the test server with an
// optional bearer token, exactly as a client would.
func doPreparationRequest(t *testing.T, e *echo.Echo, method, path, token string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// decodeEnvelope decodes a response body into the envelope.
func decodeEnvelope(t *testing.T, rec *httptest.ResponseRecorder) envelope {
	t.Helper()
	var envl envelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envl))
	return envl
}

func TestPreparationHTTPAuthorization(t *testing.T) {
	e, q := newPreparationTestServer(t)
	barista := signInPreparation(t, e, q, []string{"BARISTA"})
	manager := signInPreparation(t, e, q, []string{"MANAGER"})
	cashier := signInPreparation(t, e, q, []string{"CASHIER"})

	// The bulk body names a unit that does not exist on purpose: an authorized
	// request still completes with HTTP 200 and one UNIT_NOT_FOUND outcome, so
	// the authorization matrix needs no fixtures.
	bulkBody, err := json.Marshal(preparation.BulkAdvanceCommand{
		RequestID:          uuid.New(),
		PreparationUnitIDs: []uuid.UUID{uuid.New()},
		TargetState:        preparation.StateInPreparation,
	})
	require.NoError(t, err)

	for _, tc := range []struct {
		name, method, path string
		body               []byte
	}{
		{"queue", http.MethodGet, "/api/v1/preparation/queue", nil},
		{"bulk", http.MethodPost, "/api/v1/preparation/units/advance-many", bulkBody},
	} {
		t.Run(tc.name+" allows barista", func(t *testing.T) {
			rec := doPreparationRequest(t, e, tc.method, tc.path, barista, tc.body)
			assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		})
		t.Run(tc.name+" allows manager", func(t *testing.T) {
			rec := doPreparationRequest(t, e, tc.method, tc.path, manager, tc.body)
			assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		})
		t.Run(tc.name+" denies cashier", func(t *testing.T) {
			rec := doPreparationRequest(t, e, tc.method, tc.path, cashier, tc.body)
			assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
		})
		t.Run(tc.name+" denies anonymous", func(t *testing.T) {
			rec := doPreparationRequest(t, e, tc.method, tc.path, "", tc.body)
			assert.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
		})
	}
}

func TestPreparationHTTPBulkValidation(t *testing.T) {
	env := newPrepEnv(t)
	e := mountPreparationTestServer(env.DB, env.Queries)
	token := signInPreparation(t, e, env.Queries, []string{"BARISTA"})

	postBulk := func(t *testing.T, body any) *httptest.ResponseRecorder {
		t.Helper()
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		return doPreparationRequest(t, e, http.MethodPost,
			"/api/v1/preparation/units/advance-many", token, raw)
	}
	assertInvalidInput := func(t *testing.T, rec *httptest.ResponseRecorder) {
		t.Helper()
		assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		envl := decodeEnvelope(t, rec)
		require.NotNil(t, envl.Error)
		assert.Equal(t, "INVALID_INPUT", envl.Error.Code)
	}

	t.Run("rejects a missing request_id", func(t *testing.T) {
		rec := postBulk(t, map[string]any{
			"preparation_unit_ids": []uuid.UUID{uuid.New()},
			"target_state":         preparation.StateInPreparation,
		})
		assertInvalidInput(t, rec)
	})

	t.Run("rejects a malformed UUID in the array", func(t *testing.T) {
		body := []byte(`{"request_id":"` + uuid.NewString() + `",` +
			`"preparation_unit_ids":["not-a-uuid"],` +
			`"target_state":"IN_PREPARATION"}`)
		rec := doPreparationRequest(t, e, http.MethodPost,
			"/api/v1/preparation/units/advance-many", token, body)
		assertInvalidInput(t, rec)
	})

	t.Run("rejects empty ids", func(t *testing.T) {
		rec := postBulk(t, preparation.BulkAdvanceCommand{
			RequestID:          uuid.New(),
			PreparationUnitIDs: []uuid.UUID{},
			TargetState:        preparation.StateInPreparation,
		})
		assertInvalidInput(t, rec)
	})

	t.Run("rejects 51 ids", func(t *testing.T) {
		ids := make([]uuid.UUID, 51)
		for i := range ids {
			ids[i] = uuid.New()
		}
		rec := postBulk(t, preparation.BulkAdvanceCommand{
			RequestID:          uuid.New(),
			PreparationUnitIDs: ids,
			TargetState:        preparation.StateInPreparation,
		})
		assertInvalidInput(t, rec)
	})

	t.Run("rejects an invalid target", func(t *testing.T) {
		rec := postBulk(t, preparation.BulkAdvanceCommand{
			RequestID:          uuid.New(),
			PreparationUnitIDs: []uuid.UUID{uuid.New()},
			TargetState:        preparation.StateQueued,
		})
		assertInvalidInput(t, rec)
	})

	t.Run("reports a conflicting request-id reuse as 409", func(t *testing.T) {
		units := env.SubmittedUnits(t, 1)
		requestID := uuid.New()

		first := postBulk(t, preparation.BulkAdvanceCommand{
			RequestID:          requestID,
			PreparationUnitIDs: []uuid.UUID{units[0].ID},
			TargetState:        preparation.StateInPreparation,
		})
		require.Equal(t, http.StatusOK, first.Code, first.Body.String())

		// Same request id, different fingerprint: a conflict, never a replay.
		second := postBulk(t, preparation.BulkAdvanceCommand{
			RequestID:          requestID,
			PreparationUnitIDs: []uuid.UUID{units[0].ID},
			TargetState:        preparation.StateReady,
		})
		assert.Equal(t, http.StatusConflict, second.Code, second.Body.String())
		envl := decodeEnvelope(t, second)
		require.NotNil(t, envl.Error)
		assert.Equal(t, "REQUEST_CONFLICT", envl.Error.Code)
	})

	t.Run("a nonexistent unit id completes with one UNIT_NOT_FOUND outcome", func(t *testing.T) {
		rec := postBulk(t, preparation.BulkAdvanceCommand{
			RequestID:          uuid.New(),
			PreparationUnitIDs: []uuid.UUID{uuid.New()},
			TargetState:        preparation.StateInPreparation,
		})
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		envl := decodeEnvelope(t, rec)
		require.True(t, envl.Success)
		var resp preparation.BulkAdvanceResponse
		require.NoError(t, json.Unmarshal(envl.Data, &resp))
		require.Len(t, resp.Outcomes, 1)
		assert.Equal(t, preparation.BulkStatusFailed, resp.Outcomes[0].Status)
		assert.Equal(t, preparation.BulkCodeUnitNotFound, resp.Outcomes[0].Code)
		assert.Nil(t, resp.Outcomes[0].Unit)
	})
}

func TestPreparationHTTPQueueUsesEmptyArrays(t *testing.T) {
	e, q := newPreparationTestServer(t)
	token := signInPreparation(t, e, q, []string{"BARISTA"})

	rec := doPreparationRequest(t, e, http.MethodGet, "/api/v1/preparation/queue", token, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	for _, collection := range []string{"units", "alerts", "corrections"} {
		assert.Contains(t, rec.Body.String(), `"`+collection+`":[]`,
			"an empty queue collection must serialize as [] so sparse displays never see null")
		assert.NotContains(t, rec.Body.String(), `"`+collection+`":null`)
	}
}

func TestPreparationHTTPQueuePrivacyContract(t *testing.T) {
	env := newPrepEnv(t)
	e := mountPreparationTestServer(env.DB, env.Queries)
	token := signInPreparation(t, e, env.Queries, []string{"BARISTA"})

	// Arrange every queue collection: an active standard unit, a wasted unit
	// retained by its alert, the remake that replaces it, and both history
	// facts.
	units := env.SubmittedUnits(t, 2)
	_, _, err := env.Advance(t, units[0].ID, preparation.StateInPreparation)
	require.NoError(t, err)
	waste, _, err := env.Waste(t, units[0].ID, preparation.ReasonQualityFailure, nil)
	require.NoError(t, err)
	_, _, err = env.Remake(t, waste.ID, preparation.ReasonPreparationError, nil)
	require.NoError(t, err)

	rec := doPreparationRequest(t, e, http.MethodGet, "/api/v1/preparation/queue", token, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	envl := decodeEnvelope(t, rec)
	require.True(t, envl.Success)
	var queue struct {
		Units       []map[string]json.RawMessage `json:"units"`
		Alerts      []map[string]json.RawMessage `json:"alerts"`
		Corrections []map[string]json.RawMessage `json:"corrections"`
	}
	require.NoError(t, json.Unmarshal(envl.Data, &queue))
	require.Len(t, queue.Units, 3, "standard, remake, and alert-retained source are all on the queue")
	require.Len(t, queue.Alerts, 1, "the waste alert is on the queue")
	require.Len(t, queue.Corrections, 2, "the Waste and Remake facts are on the queue")

	assert.ElementsMatch(t, []string{
		"id", "order_item_id", "order_item_unit_count", "unit_number",
		"state", "service_number", "table_names", "category_name", "item_name",
		"size_name", "modifiers", "preparation_note", "queued_at", "in_preparation_at",
		// Phase 6B (spec §6.1): unit responses gain Remake priority metadata.
		"priority", "remake_of_preparation_unit_id",
	}, keysOf(queue.Units[0]), "the queue unit must expose exactly the bar projection keys")

	assert.ElementsMatch(t, []string{
		// Phase 6B (spec §6.3): the active alert projection.
		"id", "kind", "preparation_unit_id", "service_number", "item_name",
		"unit_number", "reason", "note", "waste_id", "created_at",
		"acknowledged_by_staff_identity_id", "acknowledged_at",
	}, keysOf(queue.Alerts[0]), "the queue alert must expose exactly the alert projection keys")

	assert.ElementsMatch(t, []string{
		// Phase 6B (spec §6.4): the Waste and Remake history entry.
		"entry_kind", "id", "preparation_unit_id", "waste_id",
		"source_preparation_unit_id", "source_unit_number", "service_number",
		"item_name", "unit_number", "reason", "note", "occurred_at",
	}, keysOf(queue.Corrections[0]), "the correction history entry must expose exactly its keys")

	// The whole raw response, decoded, must not carry any financial or
	// credential fragment in any object key or string value at any depth.
	var raw any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &raw))
	for _, fragment := range []string{
		"price", "allocation", "check", "payment", "balance",
		"sales_shift", "pin", "pin_hash", "token_hash",
	} {
		assert.False(t, jsonContainsFragment(raw, fragment),
			"queue response must not contain %q anywhere: %s", fragment, rec.Body.String())
	}
}

// keysOf collects the JSON object's key set.
func keysOf(object map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	return keys
}

// jsonContainsFragment reports whether the decoded JSON value contains the
// case-insensitive fragment in any object key or string value, at any depth.
func jsonContainsFragment(value any, fragment string) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if strings.Contains(strings.ToLower(key), fragment) ||
				jsonContainsFragment(child, fragment) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if jsonContainsFragment(child, fragment) {
				return true
			}
		}
	case string:
		return strings.Contains(strings.ToLower(typed), fragment)
	}
	return false
}
