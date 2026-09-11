//go:build integration

package catalog_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/catalog"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type httpTestContext struct {
	e            *echo.Echo
	db           *sql.DB
	q            *sqlc.Queries
	slices       *catalog.Slices
	managerToken string
	managerID    catalogTestIdentity
	cashierToken string
	cashierID    catalogTestIdentity
	baristaToken string
	baristaID    catalogTestIdentity
}

func setupHTTPTest(t *testing.T) *httpTestContext {
	t.Helper()
	db, q := openExecutorTestDB(t)
	cleanCategoryTestTables(t, db)

	e := echo.New()
	v1 := e.Group("/api/v1")

	authMiddleware := auth.NewMiddleware(q)
	slices := catalog.NewSlices(db, q)
	slices.RegisterRoutes(v1, authMiddleware)

	// Manager has all catalog caps + audit.inspect + change_price
	mgrToken, mgrIdent := createHTTPTestIdentity(t, db, q, []string{auth.RoleManager}, "123456")
	// Cashier has view_prices, manage_availability
	cashierToken, cashierIdent := createHTTPTestIdentity(t, db, q, []string{auth.RoleCashier}, "")
	// Barista has manage_availability
	baristaToken, baristaIdent := createHTTPTestIdentity(t, db, q, []string{auth.RoleBarista}, "")

	return &httpTestContext{
		e:            e,
		db:           db,
		q:            q,
		slices:       slices,
		managerToken: mgrToken,
		managerID:    mgrIdent,
		cashierToken: cashierToken,
		cashierID:    cashierIdent,
		baristaToken: baristaToken,
		baristaID:    baristaIdent,
	}
}

func createHTTPTestIdentity(t *testing.T, db *sql.DB, q *sqlc.Queries, roles []string, pin string) (string, catalogTestIdentity) {
	t.Helper()
	ctx := context.Background()
	loginCode := fmt.Sprintf("t%s", uuid.New().String()[:6])
	var pinHash string
	if pin != "" {
		var err error
		pinHash, err = auth.HashPin(pin)
		require.NoError(t, err)
	}
	row, err := q.CreateStaffIdentity(ctx, sqlc.CreateStaffIdentityParams{
		DisplayName: fmt.Sprintf("HTTP Staff %s", loginCode),
		Btrim:       loginCode,
		PinHash:     pinHash,
		Enabled:     true,
	})
	require.NoError(t, err)

	for _, role := range roles {
		err = q.AddStaffRole(ctx, sqlc.AddStaffRoleParams{
			StaffIdentityID: row.ID,
			Role:            role,
		})
		require.NoError(t, err)
	}

	token := fmt.Sprintf("test-token-%s", uuid.New().String())
	tokenHash := auth.HashToken(token)

	session, err := q.CreateStaffSession(ctx, sqlc.CreateStaffSessionParams{
		TokenHash:           tokenHash,
		StaffIdentityID:     row.ID,
		State:               auth.SessionStateActive,
		ActiveWorkspace:     sql.NullString{String: auth.WorkspaceManager, Valid: true},
		LastAuthenticatedAt: time.Now(),
		LastHumanActivityAt: time.Now(),
		ExpiresAt:           time.Now().Add(12 * time.Hour),
	})
	require.NoError(t, err)

	return token, catalogTestIdentity{
		StaffID:   row.ID,
		SessionID: session.ID,
		PIN:       pin,
	}
}

func doJSONRequest(t *testing.T, e *echo.Echo, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var bodyBytes []byte
	if body != nil {
		switch v := body.(type) {
		case []byte:
			bodyBytes = v
		case string:
			bodyBytes = []byte(v)
		default:
			var err error
			bodyBytes, err = json.Marshal(body)
			require.NoError(t, err)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(bodyBytes))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	if token != "" {
		req.Header.Set(echo.HeaderAuthorization, "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func parseAPIResponse[T any](t *testing.T, rec *httptest.ResponseRecorder) (response.APIResponse, T) {
	t.Helper()
	var apiRes response.APIResponse
	err := json.Unmarshal(rec.Body.Bytes(), &apiRes)
	require.NoError(t, err, "failed to unmarshal APIResponse from: %s", rec.Body.String())

	var data T
	if apiRes.Data != nil {
		dataBytes, err := json.Marshal(apiRes.Data)
		require.NoError(t, err)
		err = json.Unmarshal(dataBytes, &data)
		require.NoError(t, err)
	}
	return apiRes, data
}

func TestAuditEvents(t *testing.T) {
	tc := setupHTTPTest(t)

	// Seed audit events: one catalog event, one auth event
	ctx := context.Background()
	_, err := tc.q.InsertAuditEvent(ctx, sqlc.InsertAuditEventParams{
		EventType:  "auth.signed_in",
		ActorID:    uuid.NullUUID{UUID: tc.managerID.StaffID, Valid: true},
		SessionID:  uuid.NullUUID{UUID: tc.managerID.SessionID, Valid: true},
		Details:    []byte(`{"login_code":"test"}`),
		OccurredAt: time.Now().Add(-5 * time.Minute),
	})
	require.NoError(t, err)

	_, err = tc.q.InsertAuditEvent(ctx, sqlc.InsertAuditEventParams{
		EventType:  "catalog.category.created",
		ActorID:    uuid.NullUUID{UUID: tc.managerID.StaffID, Valid: true},
		SessionID:  uuid.NullUUID{UUID: tc.managerID.SessionID, Valid: true},
		Details:    []byte(`{"category_id":"` + uuid.New().String() + `","name":"Coffee"}`),
		OccurredAt: time.Now().Add(-2 * time.Minute),
	})
	require.NoError(t, err)

	_, err = tc.q.InsertAuditEvent(ctx, sqlc.InsertAuditEventParams{
		EventType:  "catalog.item.created",
		ActorID:    uuid.NullUUID{UUID: tc.managerID.StaffID, Valid: true},
		SessionID:  uuid.NullUUID{UUID: tc.managerID.SessionID, Valid: true},
		Details:    []byte(`{"item_id":"` + uuid.New().String() + `","name":"Espresso"}`),
		OccurredAt: time.Now().Add(-1 * time.Minute),
	})
	require.NoError(t, err)

	t.Run("Unauthenticated request rejected with 401", func(t *testing.T) {
		rec := doJSONRequest(t, tc.e, http.MethodGet, "/api/v1/catalog/audit-events", "", nil)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
		var res response.APIResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
		assert.False(t, res.Success)
		assert.NotNil(t, res.Error)
		assert.Equal(t, "UNAUTHORIZED", res.Error.Code)
	})

	t.Run("Cashier lacking audit.inspect rejected with 403", func(t *testing.T) {
		rec := doJSONRequest(t, tc.e, http.MethodGet, "/api/v1/catalog/audit-events", tc.cashierToken, nil)
		assert.Equal(t, http.StatusForbidden, rec.Code)
		var res response.APIResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
		assert.False(t, res.Success)
		assert.NotNil(t, res.Error)
		assert.Equal(t, "FORBIDDEN", res.Error.Code)
	})

	t.Run("Manager with audit.inspect succeeds newest-first", func(t *testing.T) {
		rec := doJSONRequest(t, tc.e, http.MethodGet, "/api/v1/catalog/audit-events?limit=10", tc.managerToken, nil)
		assert.Equal(t, http.StatusOK, rec.Code)

		var res response.APIResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
		assert.True(t, res.Success)

		dataBytes, err := json.Marshal(res.Data)
		require.NoError(t, err)
		var events []catalog.AuditEventResponse
		require.NoError(t, json.Unmarshal(dataBytes, &events))

		require.Len(t, events, 2)
		assert.Equal(t, "catalog.item.created", events[0].EventType)
		assert.Equal(t, "catalog.category.created", events[1].EventType)

		// Manager PIN must not be in any event details or output
		assert.NotContains(t, rec.Body.String(), "123456")
		assert.NotContains(t, rec.Body.String(), "manager_pin")
	})

	t.Run("Direct domain handler capability check", func(t *testing.T) {
		actorCashier := catalog.Actor{StaffID: tc.cashierID.StaffID, SessionID: tc.cashierID.SessionID}
		_, err := tc.slices.AuditEvents.Handle(ctx, actorCashier, 10)
		assert.Error(t, err)
		assert.True(t, errors.Is(err, catalog.ErrForbidden))

		actorMgr := catalog.Actor{StaffID: tc.managerID.StaffID, SessionID: tc.managerID.SessionID}
		evs, err := tc.slices.AuditEvents.Handle(ctx, actorMgr, 10)
		assert.NoError(t, err)
		assert.Len(t, evs, 2)
	})

	t.Run("Audit limit clamped to maximum 100 and defaults to 50 on non-positive", func(t *testing.T) {
		actorMgr := catalog.Actor{StaffID: tc.managerID.StaffID, SessionID: tc.managerID.SessionID}
		for i := 0; i < 105; i++ {
			_, err := tc.q.InsertAuditEvent(ctx, sqlc.InsertAuditEventParams{
				EventType:  catalog.EventItemCreated,
				ActorID:    uuid.NullUUID{UUID: tc.managerID.StaffID, Valid: true},
				SessionID:  uuid.NullUUID{UUID: tc.managerID.SessionID, Valid: true},
				Details:    []byte(fmt.Sprintf(`{"index":%d}`, i)),
				OccurredAt: time.Now(),
			})
			require.NoError(t, err)
		}

		// limit=200 should be clamped to 100
		evs, err := tc.slices.AuditEvents.Handle(ctx, actorMgr, 200)
		require.NoError(t, err)
		assert.Len(t, evs, 100)

		// HTTP endpoint with limit=150 should be clamped to 100
		rec := doJSONRequest(t, tc.e, http.MethodGet, "/api/v1/catalog/audit-events?limit=150", tc.managerToken, nil)
		assert.Equal(t, http.StatusOK, rec.Code)
		var res response.APIResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
		dataBytes, err := json.Marshal(res.Data)
		require.NoError(t, err)
		var httpEvs []catalog.AuditEventResponse
		require.NoError(t, json.Unmarshal(dataBytes, &httpEvs))
		assert.Len(t, httpEvs, 100)

		// limit <= 0 defaults to 50
		evsDefault, err := tc.slices.AuditEvents.Handle(ctx, actorMgr, 0)
		require.NoError(t, err)
		assert.Len(t, evsDefault, 50)
	})
}

func TestCatalogRoutes(t *testing.T) {
	tc := setupHTTPTest(t)

	// 1. Authentication & Capability Middleware
	t.Run("Auth middleware rejects unauthenticated", func(t *testing.T) {
		rec := doJSONRequest(t, tc.e, http.MethodPost, "/api/v1/catalog/categories", "", catalog.CreateCategoryCommand{
			RequestID: uuid.New(),
			Name:      "Tea",
		})
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("Capability middleware rejects unauthorized staff", func(t *testing.T) {
		rec := doJSONRequest(t, tc.e, http.MethodPost, "/api/v1/catalog/categories", tc.cashierToken, catalog.CreateCategoryCommand{
			RequestID: uuid.New(),
			Name:      "Tea",
		})
		assert.Equal(t, http.StatusForbidden, rec.Code)
		var res response.APIResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
		assert.Equal(t, "FORBIDDEN", res.Error.Code)
	})

	// 2. Input Validation
	t.Run("UUID parameter validation", func(t *testing.T) {
		rec := doJSONRequest(t, tc.e, http.MethodPatch, "/api/v1/catalog/categories/invalid-uuid/name", tc.managerToken, catalog.RenameRequest{
			RequestID: uuid.New(),
			Name:      "Valid Name",
		})
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		var res response.APIResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
		assert.Equal(t, "INVALID_INPUT", res.Error.Code)
	})

	t.Run("Malformed JSON body validation", func(t *testing.T) {
		rec := doJSONRequest(t, tc.e, http.MethodPost, "/api/v1/catalog/categories", tc.managerToken, "not json")
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("Missing request_id validation", func(t *testing.T) {
		rec := doJSONRequest(t, tc.e, http.MethodPost, "/api/v1/catalog/categories", tc.managerToken, map[string]string{
			"name": "Tea",
		})
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		var res response.APIResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
		assert.Equal(t, "INVALID_INPUT", res.Error.Code)
	})

	// 3. Category Endpoints
	var catID uuid.UUID
	t.Run("POST /categories -> 201 Created", func(t *testing.T) {
		cmd := catalog.CreateCategoryCommand{
			RequestID: uuid.New(),
			Name:      "Coffee Drinks",
		}
		rec := doJSONRequest(t, tc.e, http.MethodPost, "/api/v1/catalog/categories", tc.managerToken, cmd)
		assert.Equal(t, http.StatusCreated, rec.Code)
		res, cat := parseAPIResponse[catalog.CategoryResponse](t, rec)
		assert.True(t, res.Success)
		assert.Equal(t, "Coffee Drinks", cat.Name)
		assert.NotEqual(t, uuid.Nil, cat.ID)
		catID = cat.ID
	})

	t.Run("PATCH /categories/:category_id/name -> 200 OK", func(t *testing.T) {
		req := catalog.RenameRequest{
			RequestID: uuid.New(),
			Name:      "Hot & Cold Coffee",
		}
		rec := doJSONRequest(t, tc.e, http.MethodPatch, fmt.Sprintf("/api/v1/catalog/categories/%s/name", catID), tc.managerToken, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		res, cat := parseAPIResponse[catalog.CategoryResponse](t, rec)
		assert.True(t, res.Success)
		assert.Equal(t, "Hot & Cold Coffee", cat.Name)
	})

	// 4. Item Endpoints (Direct Priced)
	var directItemID uuid.UUID
	t.Run("POST /items (direct) -> 201 Created", func(t *testing.T) {
		price := int64(35000)
		cmd := catalog.CreateItemCommand{
			RequestID:  uuid.New(),
			CategoryID: catID,
			Name:       "Espresso Single",
			PriceVND:   &price,
			ManagerPIN: "123456",
		}
		rec := doJSONRequest(t, tc.e, http.MethodPost, "/api/v1/catalog/items", tc.managerToken, cmd)
		assert.Equal(t, http.StatusCreated, rec.Code)
		res, item := parseAPIResponse[catalog.ItemResponse](t, rec)
		assert.True(t, res.Success)
		assert.Equal(t, "Espresso Single", item.Name)
		require.NotNil(t, item.PriceVND)
		assert.Equal(t, int64(35000), *item.PriceVND)
		// Manager PIN must be omitted from output
		assert.NotContains(t, rec.Body.String(), "123456")
		directItemID = item.ID
	})

	t.Run("PATCH /items/:item_id/name -> 200 OK", func(t *testing.T) {
		req := catalog.RenameRequest{
			RequestID: uuid.New(),
			Name:      "Espresso Solo",
		}
		rec := doJSONRequest(t, tc.e, http.MethodPatch, fmt.Sprintf("/api/v1/catalog/items/%s/name", directItemID), tc.managerToken, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		res, item := parseAPIResponse[catalog.ItemResponse](t, rec)
		assert.True(t, res.Success)
		assert.Equal(t, "Espresso Solo", item.Name)
	})

	t.Run("PATCH /items/:item_id/price -> 200 OK", func(t *testing.T) {
		req := catalog.RepriceRequest{
			RequestID:  uuid.New(),
			PriceVND:   38000,
			ManagerPIN: "123456",
		}
		rec := doJSONRequest(t, tc.e, http.MethodPatch, fmt.Sprintf("/api/v1/catalog/items/%s/price", directItemID), tc.managerToken, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		res, item := parseAPIResponse[catalog.ItemResponse](t, rec)
		assert.True(t, res.Success)
		require.NotNil(t, item.PriceVND)
		assert.Equal(t, int64(38000), *item.PriceVND)
		assert.NotContains(t, rec.Body.String(), "123456")
	})

	t.Run("PATCH /items/:item_id/availability -> 200 OK", func(t *testing.T) {
		req := catalog.SetAvailabilityRequest{
			RequestID: uuid.New(),
			Available: false,
		}
		rec := doJSONRequest(t, tc.e, http.MethodPatch, fmt.Sprintf("/api/v1/catalog/items/%s/availability", directItemID), tc.baristaToken, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		res, item := parseAPIResponse[catalog.ItemResponse](t, rec)
		assert.True(t, res.Success)
		assert.False(t, item.Available)
	})

	// 5. Item Endpoints (Sized Items)
	var sizedItemID, sizeID uuid.UUID
	t.Run("POST /items (sized) -> 201 Created", func(t *testing.T) {
		cmd := catalog.CreateItemCommand{
			RequestID:  uuid.New(),
			CategoryID: catID,
			Name:       "Latte",
			Sizes: []catalog.CreateSizeInput{
				{Name: "Regular", PriceVND: 45000},
				{Name: "Large", PriceVND: 55000},
			},
			ManagerPIN: "123456",
		}
		rec := doJSONRequest(t, tc.e, http.MethodPost, "/api/v1/catalog/items", tc.managerToken, cmd)
		assert.Equal(t, http.StatusCreated, rec.Code)
		res, item := parseAPIResponse[catalog.ItemResponse](t, rec)
		assert.True(t, res.Success)
		assert.Equal(t, "Latte", item.Name)
		assert.Nil(t, item.PriceVND)
		require.Len(t, item.Sizes, 2)
		sizedItemID = item.ID
		sizeID = item.Sizes[0].ID
	})

	t.Run("PATCH /sizes/:size_id/name -> 200 OK", func(t *testing.T) {
		req := catalog.RenameRequest{
			RequestID: uuid.New(),
			Name:      "Standard",
		}
		rec := doJSONRequest(t, tc.e, http.MethodPatch, fmt.Sprintf("/api/v1/catalog/sizes/%s/name", sizeID), tc.managerToken, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		res, size := parseAPIResponse[catalog.SizeResponse](t, rec)
		assert.True(t, res.Success)
		assert.Equal(t, "Standard", size.Name)
	})

	t.Run("PATCH /sizes/:size_id/price -> 200 OK", func(t *testing.T) {
		req := catalog.RepriceRequest{
			RequestID:  uuid.New(),
			PriceVND:   48000,
			ManagerPIN: "123456",
		}
		rec := doJSONRequest(t, tc.e, http.MethodPatch, fmt.Sprintf("/api/v1/catalog/sizes/%s/price", sizeID), tc.managerToken, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		res, size := parseAPIResponse[catalog.SizeResponse](t, rec)
		assert.True(t, res.Success)
		assert.Equal(t, int64(48000), size.PriceVND)
		assert.NotContains(t, rec.Body.String(), "123456")
	})

	t.Run("PATCH /sizes/:size_id/availability -> 200 OK", func(t *testing.T) {
		req := catalog.SetAvailabilityRequest{
			RequestID: uuid.New(),
			Available: false,
		}
		rec := doJSONRequest(t, tc.e, http.MethodPatch, fmt.Sprintf("/api/v1/catalog/sizes/%s/availability", sizeID), tc.baristaToken, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		res, size := parseAPIResponse[catalog.SizeResponse](t, rec)
		assert.True(t, res.Success)
		assert.False(t, size.Available)
	})

	// 6. Modifier Group Endpoints
	var groupID, optionID uuid.UUID
	t.Run("POST /modifier-groups -> 201 Created", func(t *testing.T) {
		cmd := catalog.CreateModifierGroupCommand{
			RequestID:     uuid.New(),
			Name:          "Sugar Level",
			MinSelections: 1,
			MaxSelections: 1,
			Options: []catalog.CreateModifierOptionInput{
				{Name: "100%", SurchargeVND: 0},
				{Name: "50%", SurchargeVND: 0},
				{Name: "Extra Syrup", SurchargeVND: 5000},
			},
			DefaultOptionNames: []string{"100%"},
			ManagerPIN:         "123456",
		}
		rec := doJSONRequest(t, tc.e, http.MethodPost, "/api/v1/catalog/modifier-groups", tc.managerToken, cmd)
		assert.Equal(t, http.StatusCreated, rec.Code)
		res, group := parseAPIResponse[catalog.ModifierGroupResponse](t, rec)
		assert.True(t, res.Success)
		assert.Equal(t, "Sugar Level", group.Name)
		require.Len(t, group.Options, 3)
		require.Len(t, group.DefaultOptionIDs, 1)
		groupID = group.ID
		optionID = group.Options[2].ID
		assert.NotContains(t, rec.Body.String(), "123456")
	})

	t.Run("PATCH /modifier-groups/:group_id/name -> 200 OK", func(t *testing.T) {
		req := catalog.RenameRequest{
			RequestID: uuid.New(),
			Name:      "Sweetness Level",
		}
		rec := doJSONRequest(t, tc.e, http.MethodPatch, fmt.Sprintf("/api/v1/catalog/modifier-groups/%s/name", groupID), tc.managerToken, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		res, group := parseAPIResponse[catalog.ModifierGroupResponse](t, rec)
		assert.True(t, res.Success)
		assert.Equal(t, "Sweetness Level", group.Name)
	})

	t.Run("PUT /modifier-groups/:group_id/defaults -> 200 OK", func(t *testing.T) {
		req := catalog.SetModifierGroupDefaultsRequest{
			RequestID: uuid.New(),
			OptionIDs: []uuid.UUID{optionID},
		}
		rec := doJSONRequest(t, tc.e, http.MethodPut, fmt.Sprintf("/api/v1/catalog/modifier-groups/%s/defaults", groupID), tc.managerToken, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		res, group := parseAPIResponse[catalog.ModifierGroupDefaultsResponse](t, rec)
		assert.True(t, res.Success)
		assert.Equal(t, []uuid.UUID{optionID}, group.OptionIDs)
	})

	t.Run("PATCH /modifier-options/:option_id/name -> 200 OK", func(t *testing.T) {
		req := catalog.RenameRequest{
			RequestID: uuid.New(),
			Name:      "Special Syrup",
		}
		rec := doJSONRequest(t, tc.e, http.MethodPatch, fmt.Sprintf("/api/v1/catalog/modifier-options/%s/name", optionID), tc.managerToken, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		res, opt := parseAPIResponse[catalog.ModifierOptionResponse](t, rec)
		assert.True(t, res.Success)
		assert.Equal(t, "Special Syrup", opt.Name)
	})

	t.Run("PATCH /modifier-options/:option_id/price -> 200 OK", func(t *testing.T) {
		req := catalog.RepriceModifierOptionRequest{
			RequestID:    uuid.New(),
			SurchargeVND: 7000,
			ManagerPIN:   "123456",
		}
		rec := doJSONRequest(t, tc.e, http.MethodPatch, fmt.Sprintf("/api/v1/catalog/modifier-options/%s/price", optionID), tc.managerToken, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		res, opt := parseAPIResponse[catalog.ModifierOptionResponse](t, rec)
		assert.True(t, res.Success)
		assert.Equal(t, int64(7000), opt.SurchargeVND)
		assert.NotContains(t, rec.Body.String(), "123456")
	})

	t.Run("PATCH /modifier-options/:option_id/availability -> 200 OK", func(t *testing.T) {
		req := catalog.SetAvailabilityRequest{
			RequestID: uuid.New(),
			Available: false,
		}
		rec := doJSONRequest(t, tc.e, http.MethodPatch, fmt.Sprintf("/api/v1/catalog/modifier-options/%s/availability", optionID), tc.baristaToken, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		res, opt := parseAPIResponse[catalog.ModifierOptionResponse](t, rec)
		assert.True(t, res.Success)
		assert.False(t, opt.Available)
	})

	// 7. Assignment Endpoints
	t.Run("POST /categories/:category_id/modifier-groups/:group_id -> 200 OK", func(t *testing.T) {
		req := catalog.MutationRequest{RequestID: uuid.New()}
		rec := doJSONRequest(t, tc.e, http.MethodPost, fmt.Sprintf("/api/v1/catalog/categories/%s/modifier-groups/%s", catID, groupID), tc.managerToken, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		res, link := parseAPIResponse[catalog.CategoryModifierGroupResponse](t, rec)
		assert.True(t, res.Success)
		assert.Equal(t, catID, link.CategoryID)
		assert.Equal(t, groupID, link.ModifierGroupID)
	})

	t.Run("POST /items/:item_id/inherited-modifier-group-exclusions/:group_id -> 200 OK", func(t *testing.T) {
		req := catalog.MutationRequest{RequestID: uuid.New()}
		rec := doJSONRequest(t, tc.e, http.MethodPost, fmt.Sprintf("/api/v1/catalog/items/%s/inherited-modifier-group-exclusions/%s", sizedItemID, groupID), tc.managerToken, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		res, excl := parseAPIResponse[catalog.ItemModifierGroupExclusionResponse](t, rec)
		assert.True(t, res.Success)
		assert.Equal(t, sizedItemID, excl.ItemID)
		assert.Equal(t, groupID, excl.ModifierGroupID)
	})

	t.Run("POST /items/:item_id/modifier-groups/:group_id -> 200 OK", func(t *testing.T) {
		req := catalog.MutationRequest{RequestID: uuid.New()}
		rec := doJSONRequest(t, tc.e, http.MethodPost, fmt.Sprintf("/api/v1/catalog/items/%s/modifier-groups/%s", directItemID, groupID), tc.managerToken, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		res, link := parseAPIResponse[catalog.ItemModifierGroupResponse](t, rec)
		assert.True(t, res.Success)
		assert.Equal(t, directItemID, link.ItemID)
		assert.Equal(t, groupID, link.ModifierGroupID)
	})

	// 8. Read Projections
	t.Run("GET /menu/sellable -> 200 OK", func(t *testing.T) {
		rec := doJSONRequest(t, tc.e, http.MethodGet, "/api/v1/catalog/menu/sellable", tc.cashierToken, nil)
		assert.Equal(t, http.StatusOK, rec.Code)
		res, menu := parseAPIResponse[catalog.SellableMenuResponse](t, rec)
		assert.True(t, res.Success)
		assert.NotNil(t, menu.Categories)
	})

	t.Run("GET /menu/manage -> 200 OK", func(t *testing.T) {
		rec := doJSONRequest(t, tc.e, http.MethodGet, "/api/v1/catalog/menu/manage", tc.managerToken, nil)
		assert.Equal(t, http.StatusOK, rec.Code)
		res, menu := parseAPIResponse[catalog.ManagementMenuResponse](t, rec)
		assert.True(t, res.Success)
		assert.NotNil(t, menu.Categories)
	})

	t.Run("GET /menu/availability -> 200 OK", func(t *testing.T) {
		rec := doJSONRequest(t, tc.e, http.MethodGet, "/api/v1/catalog/menu/availability", tc.baristaToken, nil)
		assert.Equal(t, http.StatusOK, rec.Code)
		res, menu := parseAPIResponse[catalog.AvailabilityMenuResponse](t, rec)
		assert.True(t, res.Success)
		assert.NotNil(t, menu.Categories)
	})

	t.Run("GET /modifier-groups -> 200 OK", func(t *testing.T) {
		rec := doJSONRequest(t, tc.e, http.MethodGet, "/api/v1/catalog/modifier-groups", tc.managerToken, nil)
		assert.Equal(t, http.StatusOK, rec.Code)
		res, groups := parseAPIResponse[[]catalog.ModifierGroupManagementResponse](t, rec)
		assert.True(t, res.Success)
		assert.NotEmpty(t, groups)
	})

	// 9. Retirement Endpoints
	t.Run("POST /categories/:category_id/retirement -> 200 OK", func(t *testing.T) {
		req := catalog.RetireRequest{
			RequestID: uuid.New(),
			Reason:    "MENU_RESTRUCTURE",
		}
		rec := doJSONRequest(t, tc.e, http.MethodPost, fmt.Sprintf("/api/v1/catalog/categories/%s/retirement", catID), tc.managerToken, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		res, cat := parseAPIResponse[catalog.CategoryResponse](t, rec)
		assert.True(t, res.Success)
		assert.Equal(t, catID, cat.ID)
		assert.True(t, cat.Retired)
		require.NotNil(t, cat.RetiredAt)
		require.NotNil(t, cat.RetirementReason)
		assert.Equal(t, "MENU_RESTRUCTURE", *cat.RetirementReason)
	})

	t.Run("POST /sizes/:size_id/retirement -> 200 OK", func(t *testing.T) {
		req := catalog.RetireRequest{
			RequestID: uuid.New(),
			Reason:    "NO_LONGER_OFFERED",
		}
		rec := doJSONRequest(t, tc.e, http.MethodPost, fmt.Sprintf("/api/v1/catalog/sizes/%s/retirement", sizeID), tc.managerToken, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		res, size := parseAPIResponse[catalog.SizeResponse](t, rec)
		assert.True(t, res.Success)
		assert.Equal(t, sizeID, size.ID)
	})

	t.Run("POST /modifier-options/:option_id/retirement -> 200 OK", func(t *testing.T) {
		req := catalog.RetireRequest{
			RequestID: uuid.New(),
			Reason:    "MENU_RESTRUCTURE",
		}
		rec := doJSONRequest(t, tc.e, http.MethodPost, fmt.Sprintf("/api/v1/catalog/modifier-options/%s/retirement", optionID), tc.managerToken, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		res, opt := parseAPIResponse[catalog.ModifierOptionResponse](t, rec)
		assert.True(t, res.Success)
		assert.Equal(t, optionID, opt.ID)
	})

	t.Run("POST /modifier-groups/:group_id/retirement -> 200 OK", func(t *testing.T) {
		req := catalog.RetireRequest{
			RequestID: uuid.New(),
			Reason:    "OTHER",
			Note:      "Temporary group replaced by seasonal lineup",
		}
		rec := doJSONRequest(t, tc.e, http.MethodPost, fmt.Sprintf("/api/v1/catalog/modifier-groups/%s/retirement", groupID), tc.managerToken, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		res, grp := parseAPIResponse[catalog.ModifierGroupResponse](t, rec)
		assert.True(t, res.Success)
		assert.Equal(t, groupID, grp.ID)
	})

	t.Run("POST /items/:item_id/retirement -> 200 OK", func(t *testing.T) {
		req := catalog.RetireRequest{
			RequestID: uuid.New(),
			Reason:    "NO_LONGER_OFFERED",
		}
		rec := doJSONRequest(t, tc.e, http.MethodPost, fmt.Sprintf("/api/v1/catalog/items/%s/retirement", directItemID), tc.managerToken, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		res, item := parseAPIResponse[catalog.ItemResponse](t, rec)
		assert.True(t, res.Success)
		assert.Equal(t, directItemID, item.ID)
	})

	// 10. Stable Error Codes
	t.Run("404 CATALOG_NOT_FOUND", func(t *testing.T) {
		req := catalog.RenameRequest{
			RequestID: uuid.New(),
			Name:      "Nonexistent",
		}
		rec := doJSONRequest(t, tc.e, http.MethodPatch, fmt.Sprintf("/api/v1/catalog/categories/%s/name", uuid.New()), tc.managerToken, req)
		assert.Equal(t, http.StatusNotFound, rec.Code)
		var res response.APIResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
		assert.Equal(t, "CATALOG_NOT_FOUND", res.Error.Code)
	})

	t.Run("409 CATALOG_NAME_CONFLICT", func(t *testing.T) {
		cmd := catalog.CreateCategoryCommand{
			RequestID: uuid.New(),
			Name:      "Hot & Cold Coffee", // Already exists
		}
		rec := doJSONRequest(t, tc.e, http.MethodPost, "/api/v1/catalog/categories", tc.managerToken, cmd)
		assert.Equal(t, http.StatusConflict, rec.Code)
		var res response.APIResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
		assert.Equal(t, "CATALOG_NAME_CONFLICT", res.Error.Code)
	})

	t.Run("409 REQUEST_CONFLICT", func(t *testing.T) {
		fixedReqID := uuid.New()
		cmd1 := catalog.CreateCategoryCommand{
			RequestID: fixedReqID,
			Name:      "Unique Cat 1",
		}
		rec1 := doJSONRequest(t, tc.e, http.MethodPost, "/api/v1/catalog/categories", tc.managerToken, cmd1)
		assert.Equal(t, http.StatusCreated, rec1.Code)

		// Replay with different name
		cmd2 := catalog.CreateCategoryCommand{
			RequestID: fixedReqID,
			Name:      "Different Cat 2",
		}
		rec2 := doJSONRequest(t, tc.e, http.MethodPost, "/api/v1/catalog/categories", tc.managerToken, cmd2)
		assert.Equal(t, http.StatusConflict, rec2.Code)
		var res response.APIResponse
		require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &res))
		assert.Equal(t, "REQUEST_CONFLICT", res.Error.Code)
	})

	t.Run("400 INVALID_PRICING_CONFIGURATION", func(t *testing.T) {
		zeroPrice := int64(0)
		cmd := catalog.CreateItemCommand{
			RequestID:  uuid.New(),
			CategoryID: catID,
			Name:       "Free Drink",
			PriceVND:   &zeroPrice,
			ManagerPIN: "123456",
		}
		rec := doJSONRequest(t, tc.e, http.MethodPost, "/api/v1/catalog/items", tc.managerToken, cmd)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		var res response.APIResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
		assert.Equal(t, "INVALID_PRICING_CONFIGURATION", res.Error.Code)
	})

	t.Run("403 INVALID_MANAGER_PIN", func(t *testing.T) {
		req := catalog.RepriceRequest{
			RequestID:  uuid.New(),
			PriceVND:   50000,
			ManagerPIN: "000000", // Wrong PIN
		}
		rec := doJSONRequest(t, tc.e, http.MethodPatch, fmt.Sprintf("/api/v1/catalog/items/%s/price", sizedItemID), tc.managerToken, req)
		assert.Equal(t, http.StatusForbidden, rec.Code)
		var res response.APIResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
		assert.Equal(t, "INVALID_MANAGER_PIN", res.Error.Code)
	})

	t.Run("409 ENTITY_RETIRED", func(t *testing.T) {
		req := catalog.RenameRequest{
			RequestID: uuid.New(),
			Name:      "Retired Rename",
		}
		rec := doJSONRequest(t, tc.e, http.MethodPatch, fmt.Sprintf("/api/v1/catalog/items/%s/name", directItemID), tc.managerToken, req)
		assert.Equal(t, http.StatusConflict, rec.Code)
		var res response.APIResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
		assert.Equal(t, "ENTITY_RETIRED", res.Error.Code)
	})
}
