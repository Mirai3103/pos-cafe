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
	token, _ := signInPreparationIdentity(t, e, q, roles)
	return token
}

// signInPreparationIdentity is signInPreparation with the created identity's
// id, for tests that must mutate the signed-in identity itself — the rotated
// PIN replay suite rotates the acting manager's PIN through the generated
// query while the bearer session stays valid.
func signInPreparationIdentity(t *testing.T, e *echo.Echo, q *sqlc.Queries,
	roles []string,
) (string, uuid.UUID) {
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
	return payload.Token, row.ID
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

// --- Phase 6B: HTTP routes for Waste, Remake, Acknowledge, State Correction ---

// correctionHTTPEnv is the fixture world for the Phase 6B HTTP route suites:
// the shared prepEnv plus an Echo server mounted over it and one real bearer
// token per role. The tokens belong to identities created through the real
// sign-in endpoint; the env's seeded actors drive the sales/preparation
// fixtures, which need no HTTP layer.
type correctionHTTPEnv struct {
	*prepEnv

	server *echo.Echo

	managerToken string
	baristaToken string
	cashierToken string
}

// newCorrectionHTTPEnv truncates the world, seeds the shared env, and mounts
// the authenticated Preparation server over it.
func newCorrectionHTTPEnv(t *testing.T) *correctionHTTPEnv {
	t.Helper()
	env := newPrepEnv(t)
	e := mountPreparationTestServer(env.DB, env.Queries)
	return &correctionHTTPEnv{
		prepEnv:      env,
		server:       e,
		managerToken: signInPreparation(t, e, env.Queries, []string{"MANAGER"}),
		baristaToken: signInPreparation(t, e, env.Queries, []string{"BARISTA"}),
		cashierToken: signInPreparation(t, e, env.Queries, []string{"CASHIER"}),
	}
}

// countPrepWrites totals the row counts a Preparation mutation may create, so
// the validation suites can prove a malformed request never reaches the
// database. preparation_units is included so a leaked idempotency claim cannot
// hide behind a rollback the suite did not expect.
func countPrepWrites(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(`
		SELECT (SELECT count(*) FROM idempotency_keys)
		     + (SELECT count(*) FROM preparation_wastes)
		     + (SELECT count(*) FROM preparation_remakes)
		     + (SELECT count(*) FROM preparation_state_corrections)
		     + (SELECT count(*) FROM preparation_cancellations)
		     + (SELECT count(*) FROM charge_adjustments)
		     + (SELECT count(*) FROM preparation_alerts)
		     + (SELECT count(*) FROM preparation_unit_transitions)
		     + (SELECT count(*) FROM preparation_units)`).Scan(&n))
	return n
}

// assertPreparationError requires the response status and the envelope's
// stable error code.
func assertPreparationError(t *testing.T, rec *httptest.ResponseRecorder,
	status int, code string,
) {
	t.Helper()
	require.Equal(t, status, rec.Code, rec.Body.String())
	envl := decodeEnvelope(t, rec)
	require.NotNil(t, envl.Error)
	assert.Equal(t, code, envl.Error.Code)
}

// wasteBody marshals a Waste request body. The path supplies the unit id, so
// the body carries only request_id, reason, and note.
func wasteBody(t *testing.T, requestID uuid.UUID, reason string, note *string) []byte {
	t.Helper()
	raw, err := json.Marshal(preparation.WasteUnitCommand{
		RequestID: requestID, Reason: reason, Note: note,
	})
	require.NoError(t, err)
	return raw
}

// remakeBody marshals a Remake request body. The path supplies the Waste id,
// so the body carries only request_id, reason, and note.
func remakeBody(t *testing.T, requestID uuid.UUID, reason string, note *string) []byte {
	t.Helper()
	raw, err := json.Marshal(preparation.RemakeUnitCommand{
		RequestID: requestID, Reason: reason, Note: note,
	})
	require.NoError(t, err)
	return raw
}

// acknowledgeBody marshals an acknowledgment request body.
func acknowledgeBody(t *testing.T, requestID uuid.UUID) []byte {
	t.Helper()
	raw, err := json.Marshal(preparation.AcknowledgeAlertCommand{RequestID: requestID})
	require.NoError(t, err)
	return raw
}

// correctStateBody marshals a State Correction request body.
func correctStateBody(t *testing.T, requestID uuid.UUID, ids []uuid.UUID,
	target, reason string, note *string, pin string,
) []byte {
	t.Helper()
	raw, err := json.Marshal(preparation.CorrectStateCommand{
		RequestID:          requestID,
		PreparationUnitIDs: ids,
		TargetState:        target,
		Reason:             reason,
		Note:               note,
		ManagerPIN:         pin,
	})
	require.NoError(t, err)
	return raw
}

// cancelUnitsBody marshals a Cancellation/Change request body.
func cancelUnitsBody(t *testing.T, cmd preparation.CancelUnitsCommand) []byte {
	t.Helper()
	raw, err := json.Marshal(cmd)
	require.NoError(t, err)
	return raw
}

// TestPreparationHTTPWasteRoute exercises the POST
// /preparation/units/{unit_id}/waste surface end to end: authorization, the
// boundary validation classes, the operation-specific 404, the lifecycle and
// request-id conflicts, and the 201 success envelope.
func TestPreparationHTTPWasteRoute(t *testing.T) {
	env := newCorrectionHTTPEnv(t)

	wastePath := func(unitID string) string {
		return "/api/v1/preparation/units/" + unitID + "/waste"
	}
	postWaste := func(t *testing.T, token, unitID string, body []byte) *httptest.ResponseRecorder {
		t.Helper()
		return doPreparationRequest(t, env.server, http.MethodPost, wastePath(unitID), token, body)
	}
	wasteUnit := func(t *testing.T, unitID uuid.UUID) {
		t.Helper()
		_, _, err := env.Advance(t, unitID, preparation.StateInPreparation)
		require.NoError(t, err)
	}

	t.Run("manager and barista may waste; cashier and anonymous are denied", func(t *testing.T) {
		units := env.SubmittedUnits(t, 2)
		for _, unit := range units {
			wasteUnit(t, unit.ID)
		}

		managerRec := postWaste(t, env.managerToken, units[0].ID.String(),
			wasteBody(t, uuid.New(), preparation.ReasonQualityFailure, nil))
		require.Equal(t, http.StatusCreated, managerRec.Code, managerRec.Body.String())

		baristaRec := postWaste(t, env.baristaToken, units[1].ID.String(),
			wasteBody(t, uuid.New(), preparation.ReasonQualityFailure, nil))
		require.Equal(t, http.StatusCreated, baristaRec.Code, baristaRec.Body.String())

		cashierRec := postWaste(t, env.cashierToken, units[0].ID.String(),
			wasteBody(t, uuid.New(), preparation.ReasonQualityFailure, nil))
		assertPreparationError(t, cashierRec, http.StatusForbidden, "FORBIDDEN")

		anonymousRec := postWaste(t, "", units[0].ID.String(),
			wasteBody(t, uuid.New(), preparation.ReasonQualityFailure, nil))
		assertPreparationError(t, anonymousRec, http.StatusUnauthorized, "UNAUTHORIZED")
	})

	t.Run("wasting a unit in preparation answers 201 with the waste and its alert", func(t *testing.T) {
		unit := env.SubmittedUnits(t, 1)[0]
		wasteUnit(t, unit.ID)

		note := "  ly vo roi  "
		rec := postWaste(t, env.baristaToken, unit.ID.String(),
			wasteBody(t, uuid.New(), preparation.ReasonQualityFailure, &note))
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

		envl := decodeEnvelope(t, rec)
		require.True(t, envl.Success)
		var resp preparation.WasteResponse
		require.NoError(t, json.Unmarshal(envl.Data, &resp))
		assert.Equal(t, unit.ID, resp.PreparationUnitID)
		assert.Equal(t, preparation.StateInPreparation, resp.PriorState)
		assert.Equal(t, preparation.StateWasted, resp.ResultingState)
		assert.Equal(t, preparation.ReasonQualityFailure, resp.Reason)
		require.NotNil(t, resp.Note)
		assert.Equal(t, "ly vo roi", *resp.Note, "the note is normalized before the handler runs")
		assert.Equal(t, preparation.AlertKindWaste, resp.Alert.Kind)
		assert.Equal(t, unit.ID, resp.Alert.PreparationUnitID)
		assert.Nil(t, resp.Alert.AcknowledgedAt, "a fresh waste alert is unacknowledged")
	})

	t.Run("malformed requests are INVALID_INPUT without any database write", func(t *testing.T) {
		before := countPrepWrites(t, env.DB)
		unitID := uuid.New()

		// Malformed path UUID.
		rec := postWaste(t, env.baristaToken, "not-a-uuid",
			wasteBody(t, uuid.New(), preparation.ReasonQualityFailure, nil))
		assertPreparationError(t, rec, http.StatusBadRequest, "INVALID_INPUT")

		// Malformed JSON body.
		rec = doPreparationRequest(t, env.server, http.MethodPost, wastePath(unitID.String()),
			env.baristaToken, []byte(`{"request_id": not json`))
		assertPreparationError(t, rec, http.StatusBadRequest, "INVALID_INPUT")

		// Missing request id.
		rec = doPreparationRequest(t, env.server, http.MethodPost, wastePath(unitID.String()),
			env.baristaToken, []byte(`{"reason":"QUALITY_FAILURE"}`))
		assertPreparationError(t, rec, http.StatusBadRequest, "INVALID_INPUT")

		// A reason outside the Waste catalog.
		rec = postWaste(t, env.baristaToken, unitID.String(),
			wasteBody(t, uuid.New(), "MADE_UP_REASON", nil))
		assertPreparationError(t, rec, http.StatusBadRequest, "INVALID_INPUT")

		// A blank note cannot satisfy the OTHER reason's note requirement.
		blank := "   "
		rec = postWaste(t, env.baristaToken, unitID.String(),
			wasteBody(t, uuid.New(), preparation.ReasonOther, &blank))
		assertPreparationError(t, rec, http.StatusBadRequest, "INVALID_INPUT")

		require.Equal(t, before, countPrepWrites(t, env.DB),
			"a malformed waste must never reach the database")
	})

	t.Run("an unknown unit is 404 PREPARATION_UNIT_NOT_FOUND", func(t *testing.T) {
		rec := postWaste(t, env.baristaToken, uuid.NewString(),
			wasteBody(t, uuid.New(), preparation.ReasonQualityFailure, nil))
		assertPreparationError(t, rec, http.StatusNotFound, "PREPARATION_UNIT_NOT_FOUND")
	})

	t.Run("a queued unit cannot be wasted", func(t *testing.T) {
		unit := env.SubmittedUnits(t, 1)[0]
		rec := postWaste(t, env.baristaToken, unit.ID.String(),
			wasteBody(t, uuid.New(), preparation.ReasonQualityFailure, nil))
		assertPreparationError(t, rec, http.StatusConflict, "INVALID_TRANSITION")
	})

	t.Run("an already wasted unit cannot be wasted again", func(t *testing.T) {
		unit := env.SubmittedUnits(t, 1)[0]
		wasteUnit(t, unit.ID)
		_, _, err := env.Waste(t, unit.ID, preparation.ReasonQualityFailure, nil)
		require.NoError(t, err)

		rec := postWaste(t, env.baristaToken, unit.ID.String(),
			wasteBody(t, uuid.New(), preparation.ReasonQualityFailure, nil))
		assertPreparationError(t, rec, http.StatusConflict, "INVALID_TRANSITION")
	})

	t.Run("reusing a request id for a different waste is 409 REQUEST_CONFLICT", func(t *testing.T) {
		units := env.SubmittedUnits(t, 2)
		for _, unit := range units {
			wasteUnit(t, unit.ID)
		}
		requestID := uuid.New()

		first := postWaste(t, env.baristaToken, units[0].ID.String(),
			wasteBody(t, requestID, preparation.ReasonQualityFailure, nil))
		require.Equal(t, http.StatusCreated, first.Code, first.Body.String())

		// Same request id, different fingerprint: a conflict, never a replay.
		second := postWaste(t, env.baristaToken, units[1].ID.String(),
			wasteBody(t, requestID, preparation.ReasonPreparationError, nil))
		assertPreparationError(t, second, http.StatusConflict, "REQUEST_CONFLICT")
	})
}

// TestPreparationHTTPRemakeRoute exercises the POST
// /preparation/wastes/{waste_id}/remake surface end to end.
func TestPreparationHTTPRemakeRoute(t *testing.T) {
	env := newCorrectionHTTPEnv(t)

	remakePath := func(wasteID string) string {
		return "/api/v1/preparation/wastes/" + wasteID + "/remake"
	}
	postRemake := func(t *testing.T, token, wasteID string, body []byte) *httptest.ResponseRecorder {
		t.Helper()
		return doPreparationRequest(t, env.server, http.MethodPost, remakePath(wasteID), token, body)
	}
	// wasteViaEnv creates one wasted unit through the env fixtures and returns
	// the Waste fact's id.
	wasteViaEnv := func(t *testing.T) (unitID, wasteID uuid.UUID) {
		t.Helper()
		unit := env.SubmittedUnits(t, 1)[0]
		_, _, err := env.Advance(t, unit.ID, preparation.StateInPreparation)
		require.NoError(t, err)
		waste, _, err := env.Waste(t, unit.ID, preparation.ReasonQualityFailure, nil)
		require.NoError(t, err)
		return unit.ID, waste.ID
	}

	t.Run("manager and barista may remake; cashier and anonymous are denied", func(t *testing.T) {
		_, managerWaste := wasteViaEnv(t)
		_, baristaWaste := wasteViaEnv(t)

		managerRec := postRemake(t, env.managerToken, managerWaste.String(),
			remakeBody(t, uuid.New(), preparation.ReasonPreparationError, nil))
		require.Equal(t, http.StatusCreated, managerRec.Code, managerRec.Body.String())

		baristaRec := postRemake(t, env.baristaToken, baristaWaste.String(),
			remakeBody(t, uuid.New(), preparation.ReasonPreparationError, nil))
		require.Equal(t, http.StatusCreated, baristaRec.Code, baristaRec.Body.String())

		cashierRec := postRemake(t, env.cashierToken, managerWaste.String(),
			remakeBody(t, uuid.New(), preparation.ReasonPreparationError, nil))
		assertPreparationError(t, cashierRec, http.StatusForbidden, "FORBIDDEN")

		anonymousRec := postRemake(t, "", managerWaste.String(),
			remakeBody(t, uuid.New(), preparation.ReasonPreparationError, nil))
		assertPreparationError(t, anonymousRec, http.StatusUnauthorized, "UNAUTHORIZED")
	})

	t.Run("remaking a waste answers 201 with the linked replacement unit", func(t *testing.T) {
		sourceUnit, wasteID := wasteViaEnv(t)

		rec := postRemake(t, env.baristaToken, wasteID.String(),
			remakeBody(t, uuid.New(), preparation.ReasonPreparationError, nil))
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

		envl := decodeEnvelope(t, rec)
		require.True(t, envl.Success)
		var resp preparation.RemakeResponse
		require.NoError(t, json.Unmarshal(envl.Data, &resp))
		assert.Equal(t, wasteID, resp.WasteID)
		assert.Equal(t, sourceUnit, resp.SourcePreparationUnitID)
		assert.Equal(t, preparation.ReasonPreparationError, resp.Reason)
		assert.Equal(t, preparation.StateQueued, resp.Unit.State)
		assert.Equal(t, preparation.PriorityRemake, resp.Unit.Priority)
		require.NotNil(t, resp.Unit.RemakeOfPreparationUnitID)
		assert.Equal(t, sourceUnit, *resp.Unit.RemakeOfPreparationUnitID)
		assert.Equal(t, int32(2), resp.Unit.UnitNumber, "the replacement takes the next unit number")
	})

	t.Run("malformed requests are INVALID_INPUT without any database write", func(t *testing.T) {
		before := countPrepWrites(t, env.DB)
		wasteID := uuid.New().String()

		// Malformed path UUID.
		rec := postRemake(t, env.baristaToken, "not-a-uuid",
			remakeBody(t, uuid.New(), preparation.ReasonPreparationError, nil))
		assertPreparationError(t, rec, http.StatusBadRequest, "INVALID_INPUT")

		// Malformed JSON body.
		rec = doPreparationRequest(t, env.server, http.MethodPost, remakePath(wasteID),
			env.baristaToken, []byte(`{"request_id": not json`))
		assertPreparationError(t, rec, http.StatusBadRequest, "INVALID_INPUT")

		// Missing request id.
		rec = doPreparationRequest(t, env.server, http.MethodPost, remakePath(wasteID),
			env.baristaToken, []byte(`{"reason":"PREPARATION_ERROR"}`))
		assertPreparationError(t, rec, http.StatusBadRequest, "INVALID_INPUT")

		// CUSTOMER_REQUEST is a Waste reason, not a Remake reason.
		rec = postRemake(t, env.baristaToken, wasteID,
			remakeBody(t, uuid.New(), preparation.ReasonCustomerRequest, nil))
		assertPreparationError(t, rec, http.StatusBadRequest, "INVALID_INPUT")

		// The OTHER reason requires a note; nil cannot satisfy it.
		rec = postRemake(t, env.baristaToken, wasteID,
			remakeBody(t, uuid.New(), preparation.ReasonOther, nil))
		assertPreparationError(t, rec, http.StatusBadRequest, "INVALID_INPUT")

		require.Equal(t, before, countPrepWrites(t, env.DB),
			"a malformed remake must never reach the database")
	})

	t.Run("an unknown waste is 404 PREPARATION_WASTE_NOT_FOUND", func(t *testing.T) {
		rec := postRemake(t, env.baristaToken, uuid.NewString(),
			remakeBody(t, uuid.New(), preparation.ReasonPreparationError, nil))
		assertPreparationError(t, rec, http.StatusNotFound, "PREPARATION_WASTE_NOT_FOUND")
	})

	t.Run("a waste already carrying its remake is 409 PREPARATION_WASTE_ALREADY_REMADE", func(t *testing.T) {
		_, wasteID := wasteViaEnv(t)

		first := postRemake(t, env.baristaToken, wasteID.String(),
			remakeBody(t, uuid.New(), preparation.ReasonPreparationError, nil))
		require.Equal(t, http.StatusCreated, first.Code, first.Body.String())

		second := postRemake(t, env.baristaToken, wasteID.String(),
			remakeBody(t, uuid.New(), preparation.ReasonPreparationError, nil))
		assertPreparationError(t, second, http.StatusConflict, "PREPARATION_WASTE_ALREADY_REMADE")
	})

	t.Run("reusing a request id for a different remake is 409 REQUEST_CONFLICT", func(t *testing.T) {
		_, firstWaste := wasteViaEnv(t)
		_, secondWaste := wasteViaEnv(t)
		requestID := uuid.New()

		first := postRemake(t, env.baristaToken, firstWaste.String(),
			remakeBody(t, requestID, preparation.ReasonPreparationError, nil))
		require.Equal(t, http.StatusCreated, first.Code, first.Body.String())

		second := postRemake(t, env.baristaToken, secondWaste.String(),
			remakeBody(t, requestID, preparation.ReasonQualityFailure, nil))
		assertPreparationError(t, second, http.StatusConflict, "REQUEST_CONFLICT")
	})
}

// TestPreparationHTTPAcknowledgeRoute exercises the POST
// /preparation/alerts/{alert_id}/acknowledge surface end to end.
func TestPreparationHTTPAcknowledgeRoute(t *testing.T) {
	env := newCorrectionHTTPEnv(t)

	acknowledgePath := func(alertID string) string {
		return "/api/v1/preparation/alerts/" + alertID + "/acknowledge"
	}
	postAcknowledge := func(t *testing.T, token, alertID string, body []byte) *httptest.ResponseRecorder {
		t.Helper()
		return doPreparationRequest(t, env.server, http.MethodPost,
			acknowledgePath(alertID), token, body)
	}
	// alertViaEnv creates one wasted unit through the env fixtures and returns
	// the WASTE alert's id.
	alertViaEnv := func(t *testing.T) uuid.UUID {
		t.Helper()
		unit := env.SubmittedUnits(t, 1)[0]
		_, _, err := env.Advance(t, unit.ID, preparation.StateInPreparation)
		require.NoError(t, err)
		waste, _, err := env.Waste(t, unit.ID, preparation.ReasonQualityFailure, nil)
		require.NoError(t, err)
		return waste.Alert.ID
	}

	t.Run("manager and barista may acknowledge; cashier and anonymous are denied", func(t *testing.T) {
		managerAlert := alertViaEnv(t)
		baristaAlert := alertViaEnv(t)

		managerRec := postAcknowledge(t, env.managerToken, managerAlert.String(),
			acknowledgeBody(t, uuid.New()))
		require.Equal(t, http.StatusOK, managerRec.Code, managerRec.Body.String())

		baristaRec := postAcknowledge(t, env.baristaToken, baristaAlert.String(),
			acknowledgeBody(t, uuid.New()))
		require.Equal(t, http.StatusOK, baristaRec.Code, baristaRec.Body.String())

		cashierRec := postAcknowledge(t, env.cashierToken, managerAlert.String(),
			acknowledgeBody(t, uuid.New()))
		assertPreparationError(t, cashierRec, http.StatusForbidden, "FORBIDDEN")

		anonymousRec := postAcknowledge(t, "", managerAlert.String(), acknowledgeBody(t, uuid.New()))
		assertPreparationError(t, anonymousRec, http.StatusUnauthorized, "UNAUTHORIZED")
	})

	t.Run("acknowledging an alert answers 200 with the acknowledgment evidence", func(t *testing.T) {
		alertID := alertViaEnv(t)

		rec := postAcknowledge(t, env.baristaToken, alertID.String(), acknowledgeBody(t, uuid.New()))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		envl := decodeEnvelope(t, rec)
		require.True(t, envl.Success)
		var resp preparation.AlertResponse
		require.NoError(t, json.Unmarshal(envl.Data, &resp))
		assert.Equal(t, alertID, resp.ID)
		assert.Equal(t, preparation.AlertKindWaste, resp.Kind)
		require.NotNil(t, resp.AcknowledgedAt)
		require.NotNil(t, resp.AcknowledgedByStaffIdentityID)

		stored := env.AlertAcknowledgment(t, alertID)
		require.NotNil(t, stored.AcknowledgedBy)
		assert.Equal(t, *resp.AcknowledgedByStaffIdentityID, *stored.AcknowledgedBy)
	})

	t.Run("malformed requests are INVALID_INPUT without any database write", func(t *testing.T) {
		before := countPrepWrites(t, env.DB)
		alertID := uuid.New().String()

		// Malformed path UUID.
		rec := postAcknowledge(t, env.baristaToken, "not-a-uuid", acknowledgeBody(t, uuid.New()))
		assertPreparationError(t, rec, http.StatusBadRequest, "INVALID_INPUT")

		// Malformed JSON body.
		rec = doPreparationRequest(t, env.server, http.MethodPost, acknowledgePath(alertID),
			env.baristaToken, []byte(`{"request_id": not json`))
		assertPreparationError(t, rec, http.StatusBadRequest, "INVALID_INPUT")

		// Missing request id.
		rec = doPreparationRequest(t, env.server, http.MethodPost, acknowledgePath(alertID),
			env.baristaToken, []byte(`{}`))
		assertPreparationError(t, rec, http.StatusBadRequest, "INVALID_INPUT")

		require.Equal(t, before, countPrepWrites(t, env.DB),
			"a malformed acknowledgment must never reach the database")
	})

	t.Run("an unknown alert is 404 PREPARATION_ALERT_NOT_FOUND", func(t *testing.T) {
		rec := postAcknowledge(t, env.baristaToken, uuid.NewString(), acknowledgeBody(t, uuid.New()))
		assertPreparationError(t, rec, http.StatusNotFound, "PREPARATION_ALERT_NOT_FOUND")
	})

	t.Run("a second acknowledgment is 409 PREPARATION_ALERT_ALREADY_ACKNOWLEDGED", func(t *testing.T) {
		alertID := alertViaEnv(t)

		first := postAcknowledge(t, env.baristaToken, alertID.String(), acknowledgeBody(t, uuid.New()))
		require.Equal(t, http.StatusOK, first.Code, first.Body.String())

		second := postAcknowledge(t, env.baristaToken, alertID.String(), acknowledgeBody(t, uuid.New()))
		assertPreparationError(t, second, http.StatusConflict, "PREPARATION_ALERT_ALREADY_ACKNOWLEDGED")
	})

	t.Run("reusing a request id for a different acknowledgment is 409 REQUEST_CONFLICT", func(t *testing.T) {
		firstAlert := alertViaEnv(t)
		secondAlert := alertViaEnv(t)
		requestID := uuid.New()

		first := postAcknowledge(t, env.baristaToken, firstAlert.String(), acknowledgeBody(t, requestID))
		require.Equal(t, http.StatusOK, first.Code, first.Body.String())

		second := postAcknowledge(t, env.baristaToken, secondAlert.String(), acknowledgeBody(t, requestID))
		assertPreparationError(t, second, http.StatusConflict, "REQUEST_CONFLICT")
	})
}

// TestPreparationHTTPCorrectStateRoute exercises the POST
// /preparation/units/correct-state surface end to end. The Manager role and
// PIN-correctness checks live inside the transaction — the route carries the
// same capability middleware as the other mutations — so a Barista's denial
// comes from the executor's self-PIN gate, not from this route's middleware.
func TestPreparationHTTPCorrectStateRoute(t *testing.T) {
	env := newCorrectionHTTPEnv(t)

	const correctStatePath = "/api/v1/preparation/units/correct-state"
	postCorrect := func(t *testing.T, token string, body []byte) *httptest.ResponseRecorder {
		t.Helper()
		return doPreparationRequest(t, env.server, http.MethodPost, correctStatePath, token, body)
	}
	// unitsInPreparation creates count units and advances each to
	// IN_PREPARATION, the required prior state of a correction to QUEUED.
	unitsInPreparation := func(t *testing.T, count int) []uuid.UUID {
		t.Helper()
		units := env.SubmittedUnits(t, int32(count))
		ids := make([]uuid.UUID, 0, len(units))
		for _, unit := range units {
			_, _, err := env.Advance(t, unit.ID, preparation.StateInPreparation)
			require.NoError(t, err)
			ids = append(ids, unit.ID)
		}
		return ids
	}

	t.Run("only a manager with their own pin may correct", func(t *testing.T) {
		ids := unitsInPreparation(t, 2)

		managerRec := postCorrect(t, env.managerToken, correctStateBody(t,
			uuid.New(), ids[:1], preparation.StateQueued,
			preparation.ReasonStateRecordedInError, nil, "9753"))
		require.Equal(t, http.StatusOK, managerRec.Code, managerRec.Body.String())

		// The Barista passes the route's capability middleware; the executor's
		// Manager self-PIN gate denies inside the transaction. The PIN is
		// well-formed, so this is a collapsed 403, never a 400.
		baristaRec := postCorrect(t, env.baristaToken, correctStateBody(t,
			uuid.New(), ids[1:2], preparation.StateQueued,
			preparation.ReasonStateRecordedInError, nil, "9753"))
		assertPreparationError(t, baristaRec, http.StatusForbidden, "NOT_AUTHORIZED")

		cashierRec := postCorrect(t, env.cashierToken, correctStateBody(t,
			uuid.New(), ids[1:2], preparation.StateQueued,
			preparation.ReasonStateRecordedInError, nil, "9753"))
		assertPreparationError(t, cashierRec, http.StatusForbidden, "FORBIDDEN")

		anonymousRec := postCorrect(t, "", correctStateBody(t,
			uuid.New(), ids[1:2], preparation.StateQueued,
			preparation.ReasonStateRecordedInError, nil, "9753"))
		assertPreparationError(t, anonymousRec, http.StatusUnauthorized, "UNAUTHORIZED")

		// A well-formed PIN that is not the manager's own current PIN is
		// denied inside the transaction with the same collapsed response.
		wrongPINRec := postCorrect(t, env.managerToken, correctStateBody(t,
			uuid.New(), ids[1:2], preparation.StateQueued,
			preparation.ReasonStateRecordedInError, nil, "0000"))
		assertPreparationError(t, wrongPINRec, http.StatusForbidden, "NOT_AUTHORIZED")

		assert.Equal(t, preparation.StateInPreparation, env.UnitState(t, ids[1]),
			"every denied correction leaves the unit untouched")
	})

	t.Run("malformed requests are INVALID_INPUT without any database write", func(t *testing.T) {
		before := countPrepWrites(t, env.DB)
		ids := []uuid.UUID{uuid.New(), uuid.New()}

		for name, body := range map[string][]byte{
			// Malformed JSON body.
			"malformed json": []byte(`{"request_id": not json`),
			// Missing request id.
			"missing request id": []byte(`{"preparation_unit_ids":["` + ids[0].String() + `"],` +
				`"target_state":"QUEUED","reason":"STATE_RECORDED_IN_ERROR","manager_pin":"9753"}`),
			// A repeated id is rejected, never silently deduplicated.
			"duplicate ids": correctStateBody(t, uuid.New(),
				[]uuid.UUID{ids[0], ids[0]}, preparation.StateQueued,
				preparation.ReasonStateRecordedInError, nil, "9753"),
			// The empty selection is below the batch's lower bound.
			"empty selection": correctStateBody(t, uuid.New(), []uuid.UUID{},
				preparation.StateQueued, preparation.ReasonStateRecordedInError, nil, "9753"),
			// 51 ids are above the batch's upper bound.
			"51 ids": correctStateBody(t, uuid.New(), func() []uuid.UUID {
				many := make([]uuid.UUID, 51)
				for i := range many {
					many[i] = uuid.New()
				}
				return many
			}(), preparation.StateQueued, preparation.ReasonStateRecordedInError, nil, "9753"),
			// A zero UUID inside the selection.
			"zero uuid in selection": correctStateBody(t, uuid.New(),
				[]uuid.UUID{uuid.Nil}, preparation.StateQueued,
				preparation.ReasonStateRecordedInError, nil, "9753"),
			// FULFILLED is a terminal state, never a correction target.
			"invalid target": correctStateBody(t, uuid.New(), ids[:1],
				preparation.StateFulfilled, preparation.ReasonStateRecordedInError, nil, "9753"),
			// A reason outside the correction catalog.
			"invalid reason": correctStateBody(t, uuid.New(), ids[:1],
				preparation.StateQueued, preparation.ReasonQualityFailure, nil, "9753"),
			// The OTHER reason requires a note; nil cannot satisfy it.
			"missing note for other": correctStateBody(t, uuid.New(), ids[:1],
				preparation.StateQueued, preparation.ReasonOther, nil, "9753"),
			// PIN shapes the boundary refuses before any transaction.
			"pin too short": correctStateBody(t, uuid.New(), ids[:1],
				preparation.StateQueued, preparation.ReasonStateRecordedInError, nil, "12"),
			"pin too long": correctStateBody(t, uuid.New(), ids[:1],
				preparation.StateQueued, preparation.ReasonStateRecordedInError, nil, "123456789"),
			"pin not digits": correctStateBody(t, uuid.New(), ids[:1],
				preparation.StateQueued, preparation.ReasonStateRecordedInError, nil, "abcd"),
			"pin empty": correctStateBody(t, uuid.New(), ids[:1],
				preparation.StateQueued, preparation.ReasonStateRecordedInError, nil, ""),
		} {
			rec := postCorrect(t, env.managerToken, body)
			assertPreparationError(t, rec, http.StatusBadRequest, "INVALID_INPUT")
			assert.Equal(t, before, countPrepWrites(t, env.DB),
				"the %q class of malformed correction must never reach the database", name)
		}
	})

	t.Run("an unknown unit id is 404 PREPARATION_UNIT_NOT_FOUND", func(t *testing.T) {
		rec := postCorrect(t, env.managerToken, correctStateBody(t,
			uuid.New(), []uuid.UUID{uuid.New()}, preparation.StateQueued,
			preparation.ReasonStateRecordedInError, nil, "9753"))
		assertPreparationError(t, rec, http.StatusNotFound, "PREPARATION_UNIT_NOT_FOUND")
	})

	t.Run("a stale batch is 409 INVALID_TRANSITION", func(t *testing.T) {
		// The unit sits at QUEUED, so a correction to QUEUED - which requires
		// IN_PREPARATION - is refused whole: no half-reversed selections.
		unit := env.SubmittedUnits(t, 1)[0]
		rec := postCorrect(t, env.managerToken, correctStateBody(t,
			uuid.New(), []uuid.UUID{unit.ID}, preparation.StateQueued,
			preparation.ReasonStateRecordedInError, nil, "9753"))
		assertPreparationError(t, rec, http.StatusConflict, "INVALID_TRANSITION")
	})

	t.Run("reusing a request id for a different correction is 409 REQUEST_CONFLICT", func(t *testing.T) {
		ids := unitsInPreparation(t, 2)
		requestID := uuid.New()

		first := postCorrect(t, env.managerToken, correctStateBody(t,
			requestID, ids[:1], preparation.StateQueued,
			preparation.ReasonStateRecordedInError, nil, "9753"))
		require.Equal(t, http.StatusOK, first.Code, first.Body.String())

		second := postCorrect(t, env.managerToken, correctStateBody(t,
			requestID, ids[1:2], preparation.StateQueued,
			preparation.ReasonOther, ptrString("nham trang thai"), "9753"))
		assertPreparationError(t, second, http.StatusConflict, "REQUEST_CONFLICT")
	})

	t.Run("a manager correction answers 200 with outcomes in request order", func(t *testing.T) {
		ids := unitsInPreparation(t, 2)
		// Reverse the fixture order in the request: the response must follow
		// the request's own order, not the locks' byte order.
		requested := []uuid.UUID{ids[1], ids[0]}
		note := "  nham trang thai  "

		rec := postCorrect(t, env.managerToken, correctStateBody(t,
			uuid.New(), requested, preparation.StateQueued,
			preparation.ReasonStateRecordedInError, &note, "9753"))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		envl := decodeEnvelope(t, rec)
		require.True(t, envl.Success)
		var resp preparation.CorrectStateResponse
		require.NoError(t, json.Unmarshal(envl.Data, &resp))
		require.Len(t, resp.Outcomes, 2)
		for i, outcome := range resp.Outcomes {
			assert.Equal(t, requested[i], outcome.PreparationUnitID, "outcomes follow request order")
			assert.Equal(t, preparation.StateInPreparation, outcome.PriorState)
			assert.Equal(t, preparation.StateQueued, outcome.ResultingState)
			assert.NotEqual(t, uuid.Nil, outcome.CorrectionID)
		}

		assert.Equal(t, preparation.StateQueued, env.UnitState(t, ids[0]))
		assert.Equal(t, preparation.StateQueued, env.UnitState(t, ids[1]))
		for _, id := range ids {
			assert.Equal(t, 1, env.CountCorrections(t, id))
			correction := env.UnitCorrection(t, id)
			assert.Equal(t, preparation.ReasonStateRecordedInError, correction.Reason)
			require.NotNil(t, correction.Note)
			assert.Equal(t, "nham trang thai", *correction.Note,
				"the note is normalized before the handler runs")
		}

		// The whole raw response, decoded, must not carry any PIN fragment in
		// any object key or string value at any depth.
		var raw any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &raw))
		for _, fragment := range []string{"manager_pin", "pin", "pin_hash"} {
			assert.False(t, jsonContainsFragment(raw, fragment),
				"a correction response must not contain %q anywhere: %s", fragment, rec.Body.String())
		}
	})
}

// TestPreparationHTTPCorrectStatePINReplay proves the HTTP surface follows the
// executor's rotated-PIN contract: the self-PIN gate runs before the
// idempotency replay, so an exact replay with the old PIN is denied after a
// rotation, and the same replay with the current PIN returns the stored result
// without recording anything further.
func TestPreparationHTTPCorrectStatePINReplay(t *testing.T) {
	env := newCorrectionHTTPEnv(t)
	token, staffID := signInPreparationIdentity(t, env.server, env.Queries, []string{"MANAGER"})

	unit := env.SubmittedUnits(t, 1)[0]
	_, _, err := env.Advance(t, unit.ID, preparation.StateInPreparation)
	require.NoError(t, err)

	requestID := uuid.New()

	post := func(pin string) *httptest.ResponseRecorder {
		return doPreparationRequest(t, env.server, http.MethodPost,
			"/api/v1/preparation/units/correct-state", token,
			correctStateBody(t, requestID, []uuid.UUID{unit.ID}, preparation.StateQueued,
				preparation.ReasonStateRecordedInError, nil, pin))
	}

	first := post("9753")
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	var firstResp preparation.CorrectStateResponse
	firstEnvl := decodeEnvelope(t, first)
	require.True(t, firstEnvl.Success)
	require.NoError(t, json.Unmarshal(firstEnvl.Data, &firstResp))
	require.Len(t, firstResp.Outcomes, 1)

	// Rotate the acting manager's PIN through the generated query; the bearer
	// session stays valid, so only the PIN gate stands between the replay and
	// its stored result.
	hash, err := auth.HashPin("5678")
	require.NoError(t, err)
	require.NoError(t, env.Queries.UpdateStaffPin(context.Background(),
		sqlc.UpdateStaffPinParams{ID: staffID, PinHash: hash}))

	// The old PIN is denied even though the exact replay exists: the gate
	// verifies the PIN before the replay lookup is ever reached.
	oldPIN := post("9753")
	assertPreparationError(t, oldPIN, http.StatusForbidden, "NOT_AUTHORIZED")

	// The current PIN replays the exact stored result.
	newPIN := post("5678")
	require.Equal(t, http.StatusOK, newPIN.Code, newPIN.Body.String())
	var replayResp preparation.CorrectStateResponse
	replayEnvl := decodeEnvelope(t, newPIN)
	require.True(t, replayEnvl.Success)
	require.NoError(t, json.Unmarshal(replayEnvl.Data, &replayResp))
	assert.Equal(t, firstResp, replayResp, "the replay returns the exact stored response")

	assert.Equal(t, 1, env.CountCorrections(t, unit.ID), "a replay records no duplicate fact")
}

// TestPreparationHTTPCancelRoute exercises the POST /preparation/units/cancel
// surface end to end. The route requires sales.operate, so the Cashier may
// cancel while the Barista is denied by the middleware.
func TestPreparationHTTPCancelRoute(t *testing.T) {
	env := newCorrectionHTTPEnv(t)

	const cancelPath = "/api/v1/preparation/units/cancel"
	postCancel := func(t *testing.T, token string, body []byte) *httptest.ResponseRecorder {
		t.Helper()
		return doPreparationRequest(t, env.server, http.MethodPost, cancelPath, token, body)
	}
	cancellationCmd := func(ids []uuid.UUID, kind, reason string) preparation.CancelUnitsCommand {
		return preparation.CancelUnitsCommand{
			RequestID:          uuid.New(),
			PreparationUnitIDs: ids,
			Kind:               kind,
			Reason:             reason,
		}
	}

	t.Run("cashier and manager may cancel; barista and anonymous are denied", func(t *testing.T) {
		cashierUnit := env.SubmittedUnits(t, 1)[0]
		managerUnit := env.SubmittedUnits(t, 1)[0]
		baristaUnit := env.SubmittedUnits(t, 1)[0]

		cashierRec := postCancel(t, env.cashierToken,
			cancelUnitsBody(t, cancellationCmd([]uuid.UUID{cashierUnit.ID},
				preparation.CancelKindCancellation, preparation.ReasonCustomerRequest)))
		require.Equal(t, http.StatusOK, cashierRec.Code, cashierRec.Body.String())

		managerRec := postCancel(t, env.managerToken,
			cancelUnitsBody(t, cancellationCmd([]uuid.UUID{managerUnit.ID},
				preparation.CancelKindCancellation, preparation.ReasonCustomerRequest)))
		require.Equal(t, http.StatusOK, managerRec.Code, managerRec.Body.String())

		baristaRec := postCancel(t, env.baristaToken,
			cancelUnitsBody(t, cancellationCmd([]uuid.UUID{baristaUnit.ID},
				preparation.CancelKindCancellation, preparation.ReasonCustomerRequest)))
		assertPreparationError(t, baristaRec, http.StatusForbidden, "FORBIDDEN")

		anonymousRec := postCancel(t, "",
			cancelUnitsBody(t, cancellationCmd([]uuid.UUID{baristaUnit.ID},
				preparation.CancelKindCancellation, preparation.ReasonCustomerRequest)))
		assertPreparationError(t, anonymousRec, http.StatusUnauthorized, "UNAUTHORIZED")

		assert.Equal(t, preparation.StateQueued, env.UnitState(t, baristaUnit.ID),
			"every denied cancellation leaves the unit untouched")
	})

	t.Run("a successful cancellation answers 200 with non-null collections", func(t *testing.T) {
		unit := env.SubmittedUnits(t, 1)[0]

		rec := postCancel(t, env.cashierToken,
			cancelUnitsBody(t, cancellationCmd([]uuid.UUID{unit.ID},
				preparation.CancelKindCancellation, preparation.ReasonCustomerRequest)))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		envl := decodeEnvelope(t, rec)
		require.True(t, envl.Success)
		var resp preparation.CancelUnitsResponse
		require.NoError(t, json.Unmarshal(envl.Data, &resp))
		require.Len(t, resp.Outcomes, 1)
		require.Len(t, resp.Alerts, 1)
		assert.Equal(t, unit.ID, resp.Outcomes[0].PreparationUnitID)
		assert.Equal(t, preparation.AlertKindCancellation, resp.Alerts[0].Kind)
		assert.Contains(t, rec.Body.String(), `"outcomes":[`)
		assert.Contains(t, rec.Body.String(), `"alerts":[`)

		// The raw response must not leak credentials or financial internals.
		var raw any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &raw))
		for _, fragment := range []string{"pin", "payment", "refund", "check_id"} {
			assert.False(t, jsonContainsFragment(raw, fragment),
				"a cancellation response must not contain %q anywhere", fragment)
		}
	})

	t.Run("malformed requests are rejected without any database write", func(t *testing.T) {
		before := countPrepWrites(t, env.DB)
		unitID := uuid.New()
		replacement := uuid.New()

		cases := map[string]struct {
			body   []byte
			status int
			code   string
		}{
			"malformed json": {
				body:   []byte(`{"request_id": not json`),
				status: http.StatusBadRequest, code: "INVALID_INPUT",
			},
			"missing request id": {
				body: []byte(`{"preparation_unit_ids":["` + unitID.String() + `"],` +
					`"kind":"CANCELLATION","reason":"CUSTOMER_REQUEST"}`),
				status: http.StatusBadRequest, code: "INVALID_INPUT",
			},
			"empty selection": {
				body: cancelUnitsBody(t, cancellationCmd([]uuid.UUID{},
					preparation.CancelKindCancellation, preparation.ReasonCustomerRequest)),
				status: http.StatusBadRequest, code: "CANCELLATION_SELECTION_INVALID",
			},
			"51 ids": {
				body: cancelUnitsBody(t, cancellationCmd(func() []uuid.UUID {
					many := make([]uuid.UUID, 51)
					for i := range many {
						many[i] = uuid.New()
					}
					return many
				}(), preparation.CancelKindCancellation, preparation.ReasonCustomerRequest)),
				status: http.StatusBadRequest, code: "CANCELLATION_SELECTION_INVALID",
			},
			"duplicate ids": {
				body: cancelUnitsBody(t, cancellationCmd([]uuid.UUID{unitID, unitID},
					preparation.CancelKindCancellation, preparation.ReasonCustomerRequest)),
				status: http.StatusBadRequest, code: "CANCELLATION_SELECTION_INVALID",
			},
			"zero uuid": {
				body: cancelUnitsBody(t, cancellationCmd([]uuid.UUID{uuid.Nil},
					preparation.CancelKindCancellation, preparation.ReasonCustomerRequest)),
				status: http.StatusBadRequest, code: "CANCELLATION_SELECTION_INVALID",
			},
			"invalid kind": {
				body: cancelUnitsBody(t, cancellationCmd([]uuid.UUID{unitID},
					"WASTE", preparation.ReasonCustomerRequest)),
				status: http.StatusBadRequest, code: "CANCELLATION_SELECTION_INVALID",
			},
			"change without replacement": {
				body: cancelUnitsBody(t, cancellationCmd([]uuid.UUID{unitID},
					preparation.CancelKindChange, preparation.ReasonCustomerRequest)),
				status: http.StatusBadRequest, code: "REPLACEMENT_ORDER_REQUIRED",
			},
			"cancellation with replacement": {
				body: cancelUnitsBody(t, preparation.CancelUnitsCommand{
					RequestID:          uuid.New(),
					PreparationUnitIDs: []uuid.UUID{unitID},
					Kind:               preparation.CancelKindCancellation,
					ReplacementOrderID: &replacement,
					Reason:             preparation.ReasonCustomerRequest,
				}),
				status: http.StatusBadRequest, code: "CANCELLATION_SELECTION_INVALID",
			},
			"invalid reason": {
				body: cancelUnitsBody(t, cancellationCmd([]uuid.UUID{unitID},
					preparation.CancelKindCancellation, preparation.ReasonQualityFailure)),
				status: http.StatusBadRequest, code: "INVALID_PREPARATION_REASON",
			},
			"other without note": {
				body: cancelUnitsBody(t, cancellationCmd([]uuid.UUID{unitID},
					preparation.CancelKindCancellation, preparation.ReasonOther)),
				status: http.StatusBadRequest, code: "INVALID_PREPARATION_NOTE",
			},
		}
		for name, tc := range cases {
			rec := postCancel(t, env.cashierToken, tc.body)
			assertPreparationError(t, rec, tc.status, tc.code)
			assert.Equal(t, before, countPrepWrites(t, env.DB),
				"the %q class of malformed cancellation must never reach the database", name)
		}
	})

	t.Run("an unknown unit is 404", func(t *testing.T) {
		rec := postCancel(t, env.cashierToken,
			cancelUnitsBody(t, cancellationCmd([]uuid.UUID{uuid.New()},
				preparation.CancelKindCancellation, preparation.ReasonCustomerRequest)))
		assertPreparationError(t, rec, http.StatusNotFound, "PREPARATION_UNIT_NOT_FOUND")
	})

	t.Run("a stale unit is 409", func(t *testing.T) {
		unit := env.SubmittedUnits(t, 1)[0]
		_, _, err := env.Advance(t, unit.ID, preparation.StateInPreparation)
		require.NoError(t, err)

		rec := postCancel(t, env.cashierToken,
			cancelUnitsBody(t, cancellationCmd([]uuid.UUID{unit.ID},
				preparation.CancelKindCancellation, preparation.ReasonCustomerRequest)))
		assertPreparationError(t, rec, http.StatusConflict, "CANCELLATION_SOURCE_NOT_QUEUED")
	})

	t.Run("an exact replay returns the stored result", func(t *testing.T) {
		unit := env.SubmittedUnits(t, 1)[0]
		requestID := uuid.New()
		body := cancelUnitsBody(t, preparation.CancelUnitsCommand{
			RequestID:          requestID,
			PreparationUnitIDs: []uuid.UUID{unit.ID},
			Kind:               preparation.CancelKindCancellation,
			Reason:             preparation.ReasonCustomerRequest,
		})

		first := postCancel(t, env.cashierToken, body)
		require.Equal(t, http.StatusOK, first.Code, first.Body.String())

		replay := postCancel(t, env.cashierToken, body)
		require.Equal(t, http.StatusOK, replay.Code, replay.Body.String())
		assert.JSONEq(t, first.Body.String(), replay.Body.String())

		// Same request id, different meaning: a conflict, never a replay.
		conflict := postCancel(t, env.cashierToken,
			cancelUnitsBody(t, preparation.CancelUnitsCommand{
				RequestID:          requestID,
				PreparationUnitIDs: []uuid.UUID{unit.ID},
				Kind:               preparation.CancelKindCancellation,
				Reason:             preparation.ReasonItemUnavailable,
			}))
		assertPreparationError(t, conflict, http.StatusConflict, "REQUEST_CONFLICT")

		var facts int
		require.NoError(t, env.DB.QueryRow(
			`SELECT count(*) FROM preparation_cancellations WHERE preparation_unit_id = $1`,
			unit.ID).Scan(&facts))
		assert.Equal(t, 1, facts)
	})

	t.Run("a closed Shift is 409", func(t *testing.T) {
		env := newCorrectionHTTPEnv(t)
		unit := env.SubmittedUnits(t, 1)[0]
		_, err := env.DB.Exec(`UPDATE sales_shifts SET state = 'CLOSED' WHERE id = $1`, env.ShiftID)
		require.NoError(t, err)

		rec := postCancel(t, env.cashierToken,
			cancelUnitsBody(t, cancellationCmd([]uuid.UUID{unit.ID},
				preparation.CancelKindCancellation, preparation.ReasonCustomerRequest)))
		assertPreparationError(t, rec, http.StatusConflict, "OPEN_SALES_SHIFT_REQUIRED")
	})
}

// ptrString returns a pointer to the given string literal.
func ptrString(value string) *string { return &value }
