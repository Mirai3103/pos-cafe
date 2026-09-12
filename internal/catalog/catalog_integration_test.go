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
	"sync"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/catalog"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/httpvalidator"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type e2eContext struct {
	e       *echo.Echo
	db      *sql.DB
	q       *sqlc.Queries
	slices  *catalog.Slices
	token   string
	staffID uuid.UUID
	sessID  uuid.UUID
}

func setupE2EApp(t *testing.T) *e2eContext {
	t.Helper()
	db, q := openExecutorTestDB(t)

	// Clean both auth and catalog tables
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `
		TRUNCATE TABLE 
			staff_access_sessions, 
			idempotency_keys, 
			staff_operational_roles, 
			staff_identities,
			menu_item_sizes,
			menu_items,
			menu_categories, 
			modifier_groups,
			catalog_mutation_requests, 
			audit_events 
		RESTART IDENTITY CASCADE;
	`)
	require.NoError(t, err)

	e := echo.New()
	e.Validator = httpvalidator.New()

	v1 := e.Group("/api/v1")
	authSlices := auth.NewSlices(db, q)
	authSlices.RegisterRoutes(v1)

	catalogSlices := catalog.NewSlices(db, q)
	catalogSlices.RegisterRoutes(v1, authSlices.Middleware)

	return &e2eContext{
		e:      e,
		db:     db,
		q:      q,
		slices: catalogSlices,
	}
}

func doHTTP(t *testing.T, e *echo.Echo, method, path, token string, body any) *httptest.ResponseRecorder {
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

func decodeBody[T any](t *testing.T, rec *httptest.ResponseRecorder) (response.APIResponse, T) {
	t.Helper()
	var apiRes response.APIResponse
	err := json.Unmarshal(rec.Body.Bytes(), &apiRes)
	require.NoError(t, err, "failed to decode JSON response: %s", rec.Body.String())

	var data T
	if apiRes.Data != nil {
		dataBytes, err := json.Marshal(apiRes.Data)
		require.NoError(t, err)
		err = json.Unmarshal(dataBytes, &data)
		require.NoError(t, err)
	}
	return apiRes, data
}

// ============================================================================
// Step 1: Comprehensive End-To-End Catalog Lifecycle Integration Test
// ============================================================================

func TestCatalogEndToEndLifecycle(t *testing.T) {
	app := setupE2EApp(t)

	// 1. Bootstrap Manager
	bootstrapBody := auth.BootstrapManagerRequest{
		DisplayName: "Catalog E2E Manager",
		LoginCode:   "MGRE2E",
		Pin:         "123456",
	}
	rec := doHTTP(t, app.e, http.MethodPost, "/api/v1/auth/bootstrap", "", bootstrapBody)
	require.Equal(t, http.StatusCreated, rec.Code, "Bootstrap should succeed: %s", rec.Body.String())

	var bootProfile auth.StaffProfileResponse
	_, bootProfile = decodeBody[auth.StaffProfileResponse](t, rec)
	require.NotEmpty(t, bootProfile.ID)
	app.staffID = bootProfile.ID

	// 2. Sign In Manager
	signInBody := auth.SignInRequest{
		LoginCode: "MGRE2E",
		Pin:       "123456",
	}
	rec = doHTTP(t, app.e, http.MethodPost, "/api/v1/auth/sign-in", "", signInBody)
	require.Equal(t, http.StatusOK, rec.Code, "Sign-in should succeed: %s", rec.Body.String())

	var signInRes auth.SignInResponse
	_, signInRes = decodeBody[auth.SignInResponse](t, rec)
	require.NotEmpty(t, signInRes.Token)
	app.token = signInRes.Token

	// Set workspace to manager
	rec = doHTTP(t, app.e, http.MethodPost, "/api/v1/auth/workspace", app.token, auth.DeclareWorkspaceRequest{
		Workspace: auth.WorkspaceManager,
	})
	require.Equal(t, http.StatusOK, rec.Code)

	// Retrieve session ID from database
	tokenHash := auth.HashToken(app.token)
	sess, err := app.q.GetSessionByTokenHash(context.Background(), tokenHash)
	require.NoError(t, err)
	app.sessID = sess.SessionID

	// 3. Create Categories
	// Category 1: Coffee
	rec = doHTTP(t, app.e, http.MethodPost, "/api/v1/catalog/categories", app.token, catalog.CreateCategoryCommand{
		RequestID: uuid.New(),
		Name:      "Coffee",
	})
	require.Equal(t, http.StatusCreated, rec.Code, "Create Coffee category: %s", rec.Body.String())
	_, coffeeCat := decodeBody[catalog.CategoryResponse](t, rec)
	coffeeID := coffeeCat.ID
	require.NotEmpty(t, coffeeID)

	// Category 2: Tea
	rec = doHTTP(t, app.e, http.MethodPost, "/api/v1/catalog/categories", app.token, catalog.CreateCategoryCommand{
		RequestID: uuid.New(),
		Name:      "Tea",
	})
	require.Equal(t, http.StatusCreated, rec.Code, "Create Tea category: %s", rec.Body.String())
	_, teaCat := decodeBody[catalog.CategoryResponse](t, rec)
	teaID := teaCat.ID
	require.NotEmpty(t, teaID)

	// 4. Create Items
	// Item 1: Direct item Espresso in Coffee
	price35k := int64(35000)
	rec = doHTTP(t, app.e, http.MethodPost, "/api/v1/catalog/items", app.token, catalog.CreateItemCommand{
		RequestID:  uuid.New(),
		CategoryID: coffeeID,
		Name:       "Espresso",
		PriceVND:   &price35k,
		ManagerPIN: "123456",
	})
	require.Equal(t, http.StatusCreated, rec.Code, "Create Espresso: %s", rec.Body.String())
	_, espressoItem := decodeBody[catalog.ItemResponse](t, rec)
	espressoID := espressoItem.ID
	require.NotEmpty(t, espressoID)

	// Item 2: Sized item Milk Tea in Tea
	rec = doHTTP(t, app.e, http.MethodPost, "/api/v1/catalog/items", app.token, catalog.CreateItemCommand{
		RequestID:  uuid.New(),
		CategoryID: teaID,
		Name:       "Milk Tea",
		Sizes: []catalog.CreateSizeInput{
			{Name: "Regular", PriceVND: 30000},
			{Name: "Large", PriceVND: 40000},
		},
		ManagerPIN: "123456",
	})
	require.Equal(t, http.StatusCreated, rec.Code, "Create Milk Tea: %s", rec.Body.String())
	_, milkTeaItem := decodeBody[catalog.ItemResponse](t, rec)
	milkTeaID := milkTeaItem.ID
	require.NotEmpty(t, milkTeaID)
	require.Len(t, milkTeaItem.Sizes, 2)
	var sizeRegularID, sizeLargeID uuid.UUID
	for _, s := range milkTeaItem.Sizes {
		if s.Name == "Regular" {
			sizeRegularID = s.ID
		} else if s.Name == "Large" {
			sizeLargeID = s.ID
		}
	}
	require.NotEmpty(t, sizeRegularID)
	require.NotEmpty(t, sizeLargeID)

	// Item 3: Direct item Green Tea in Tea (for exclusion test)
	price25k := int64(25000)
	rec = doHTTP(t, app.e, http.MethodPost, "/api/v1/catalog/items", app.token, catalog.CreateItemCommand{
		RequestID:  uuid.New(),
		CategoryID: teaID,
		Name:       "Green Tea",
		PriceVND:   &price25k,
		ManagerPIN: "123456",
	})
	require.Equal(t, http.StatusCreated, rec.Code, "Create Green Tea: %s", rec.Body.String())
	_, greenTeaItem := decodeBody[catalog.ItemResponse](t, rec)
	greenTeaID := greenTeaItem.ID
	require.NotEmpty(t, greenTeaID)

	// 5. Create Modifier Groups
	// Group 1: Sugar Level (Required/Default, min=1, max=1)
	rec = doHTTP(t, app.e, http.MethodPost, "/api/v1/catalog/modifier-groups", app.token, catalog.CreateModifierGroupCommand{
		RequestID:     uuid.New(),
		Name:          "Sugar Level",
		MinSelections: 1,
		MaxSelections: 1,
		Options: []catalog.CreateModifierOptionInput{
			{Name: "100%", SurchargeVND: 0},
			{Name: "50%", SurchargeVND: 0},
			{Name: "0%", SurchargeVND: 0},
		},
		DefaultOptionNames: []string{"100%"},
		ManagerPIN:         "123456",
	})
	require.Equal(t, http.StatusCreated, rec.Code, "Create Sugar Level group: %s", rec.Body.String())
	_, sugarGroup := decodeBody[catalog.ModifierGroupResponse](t, rec)
	sugarGroupID := sugarGroup.ID
	require.NotEmpty(t, sugarGroupID)
	require.Len(t, sugarGroup.Options, 3)
	var sugarOpt100ID, sugarOpt50ID uuid.UUID
	for _, opt := range sugarGroup.Options {
		if opt.Name == "100%" {
			sugarOpt100ID = opt.ID
		} else if opt.Name == "50%" {
			sugarOpt50ID = opt.ID
		}
	}
	require.NotEmpty(t, sugarOpt100ID)
	require.NotEmpty(t, sugarOpt50ID)

	// Group 2: Toppings (Optional, min=0, max=3)
	rec = doHTTP(t, app.e, http.MethodPost, "/api/v1/catalog/modifier-groups", app.token, catalog.CreateModifierGroupCommand{
		RequestID:     uuid.New(),
		Name:          "Toppings",
		MinSelections: 0,
		MaxSelections: 3,
		Options: []catalog.CreateModifierOptionInput{
			{Name: "Boba", SurchargeVND: 10000},
			{Name: "Pudding", SurchargeVND: 12000},
			{Name: "Grass Jelly", SurchargeVND: 8000},
		},
		ManagerPIN: "123456",
	})
	require.Equal(t, http.StatusCreated, rec.Code, "Create Toppings group: %s", rec.Body.String())
	_, toppingGroup := decodeBody[catalog.ModifierGroupResponse](t, rec)
	toppingGroupID := toppingGroup.ID
	require.NotEmpty(t, toppingGroupID)
	var bobaOptID uuid.UUID
	for _, opt := range toppingGroup.Options {
		if opt.Name == "Boba" {
			bobaOptID = opt.ID
		}
	}
	require.NotEmpty(t, bobaOptID)

	// Update default option for Sugar Level: change default from 100% to 50%
	rec = doHTTP(t, app.e, http.MethodPut, fmt.Sprintf("/api/v1/catalog/modifier-groups/%s/defaults", sugarGroupID), app.token, catalog.SetModifierGroupDefaultsRequest{
		RequestID: uuid.New(),
		OptionIDs: []uuid.UUID{sugarOpt50ID},
	})
	require.Equal(t, http.StatusOK, rec.Code, "Update defaults for Sugar Level: %s", rec.Body.String())
	_, updatedDefaults := decodeBody[catalog.ModifierGroupDefaultsResponse](t, rec)
	require.Equal(t, []uuid.UUID{sugarOpt50ID}, updatedDefaults.OptionIDs)

	// 6. Attachments
	// Attach Toppings to category Tea
	rec = doHTTP(t, app.e, http.MethodPost, fmt.Sprintf("/api/v1/catalog/categories/%s/modifier-groups/%s", teaID, toppingGroupID), app.token, catalog.MutationRequest{
		RequestID: uuid.New(),
	})
	require.Equal(t, http.StatusOK, rec.Code, "Attach Toppings to Tea category: %s", rec.Body.String())

	// Attach Sugar Level to item Milk Tea directly
	rec = doHTTP(t, app.e, http.MethodPost, fmt.Sprintf("/api/v1/catalog/items/%s/modifier-groups/%s", milkTeaID, sugarGroupID), app.token, catalog.MutationRequest{
		RequestID: uuid.New(),
	})
	require.Equal(t, http.StatusOK, rec.Code, "Attach Sugar Level to Milk Tea item: %s", rec.Body.String())

	// 7. Inherited Modifier Group Exclusion
	// Green Tea is in Tea category, so it inherits Toppings. Exclude Toppings from Green Tea.
	rec = doHTTP(t, app.e, http.MethodPost, fmt.Sprintf("/api/v1/catalog/items/%s/inherited-modifier-group-exclusions/%s", greenTeaID, toppingGroupID), app.token, catalog.MutationRequest{
		RequestID: uuid.New(),
	})
	require.Equal(t, http.StatusOK, rec.Code, "Exclude Toppings from Green Tea: %s", rec.Body.String())

	// 8. Availability Changes
	// Toggle Espresso availability to false
	rec = doHTTP(t, app.e, http.MethodPatch, fmt.Sprintf("/api/v1/catalog/items/%s/availability", espressoID), app.token, catalog.SetAvailabilityRequest{
		RequestID: uuid.New(),
		Available: false,
	})
	require.Equal(t, http.StatusOK, rec.Code, "Set Espresso unavailable: %s", rec.Body.String())

	// Check Sellable Menu: Espresso is excluded; Coffee has no sellable items, so Coffee category is omitted!
	rec = doHTTP(t, app.e, http.MethodGet, "/api/v1/catalog/menu/sellable", app.token, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	_, sellableMenu := decodeBody[catalog.SellableMenuResponse](t, rec)
	for _, c := range sellableMenu.Categories {
		require.NotEqual(t, coffeeID, c.ID, "Coffee category should be omitted when all items are unavailable")
	}

	// Restore Espresso availability to true
	rec = doHTTP(t, app.e, http.MethodPatch, fmt.Sprintf("/api/v1/catalog/items/%s/availability", espressoID), app.token, catalog.SetAvailabilityRequest{
		RequestID: uuid.New(),
		Available: true,
	})
	require.Equal(t, http.StatusOK, rec.Code, "Restore Espresso availability: %s", rec.Body.String())

	// Toggle Large size of Milk Tea to false
	rec = doHTTP(t, app.e, http.MethodPatch, fmt.Sprintf("/api/v1/catalog/sizes/%s/availability", sizeLargeID), app.token, catalog.SetAvailabilityRequest{
		RequestID: uuid.New(),
		Available: false,
	})
	require.Equal(t, http.StatusOK, rec.Code, "Set Large size unavailable: %s", rec.Body.String())

	// Verify Sellable Menu has only Regular size for Milk Tea
	rec = doHTTP(t, app.e, http.MethodGet, "/api/v1/catalog/menu/sellable", app.token, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	_, sellableMenu = decodeBody[catalog.SellableMenuResponse](t, rec)
	for _, cat := range sellableMenu.Categories {
		for _, it := range cat.Items {
			if it.ID == milkTeaID {
				require.Len(t, it.Sizes, 1, "Only Regular size should be sellable")
				assert.Equal(t, sizeRegularID, it.Sizes[0].ID)
			}
		}
	}

	// Restore Large size availability
	rec = doHTTP(t, app.e, http.MethodPatch, fmt.Sprintf("/api/v1/catalog/sizes/%s/availability", sizeLargeID), app.token, catalog.SetAvailabilityRequest{
		RequestID: uuid.New(),
		Available: true,
	})
	require.Equal(t, http.StatusOK, rec.Code)

	// 9. Reprice Entities
	// Reprice direct item Espresso (35000 -> 38000)
	rec = doHTTP(t, app.e, http.MethodPatch, fmt.Sprintf("/api/v1/catalog/items/%s/price", espressoID), app.token, catalog.RepriceRequest{
		RequestID:  uuid.New(),
		PriceVND:   38000,
		ManagerPIN: "123456",
	})
	require.Equal(t, http.StatusOK, rec.Code, "Reprice Espresso: %s", rec.Body.String())
	_, repricedEspresso := decodeBody[catalog.ItemResponse](t, rec)
	require.NotNil(t, repricedEspresso.PriceVND)
	assert.Equal(t, int64(38000), *repricedEspresso.PriceVND)

	// Reprice size Regular of Milk Tea (30000 -> 32000)
	rec = doHTTP(t, app.e, http.MethodPatch, fmt.Sprintf("/api/v1/catalog/sizes/%s/price", sizeRegularID), app.token, catalog.RepriceRequest{
		RequestID:  uuid.New(),
		PriceVND:   32000,
		ManagerPIN: "123456",
	})
	require.Equal(t, http.StatusOK, rec.Code, "Reprice size Regular: %s", rec.Body.String())
	_, repricedSize := decodeBody[catalog.SizeResponse](t, rec)
	assert.Equal(t, int64(32000), repricedSize.PriceVND)

	// Reprice modifier option Boba (10000 -> 11000)
	rec = doHTTP(t, app.e, http.MethodPatch, fmt.Sprintf("/api/v1/catalog/modifier-options/%s/price", bobaOptID), app.token, catalog.RepriceModifierOptionRequest{
		RequestID:    uuid.New(),
		SurchargeVND: 11000,
		ManagerPIN:   "123456",
	})
	require.Equal(t, http.StatusOK, rec.Code, "Reprice Boba option: %s", rec.Body.String())
	_, repricedOption := decodeBody[catalog.ModifierOptionResponse](t, rec)
	assert.Equal(t, int64(11000), repricedOption.SurchargeVND)

	// 10. Retire Entity
	// Create temporary item "Old Special" in Coffee
	price50k := int64(50000)
	rec = doHTTP(t, app.e, http.MethodPost, "/api/v1/catalog/items", app.token, catalog.CreateItemCommand{
		RequestID:  uuid.New(),
		CategoryID: coffeeID,
		Name:       "Old Special",
		PriceVND:   &price50k,
		ManagerPIN: "123456",
	})
	require.Equal(t, http.StatusCreated, rec.Code, "Create Old Special: %s", rec.Body.String())
	_, oldSpecialItem := decodeBody[catalog.ItemResponse](t, rec)
	oldSpecialID := oldSpecialItem.ID
	require.NotEmpty(t, oldSpecialID)

	// Retire "Old Special" item
	rec = doHTTP(t, app.e, http.MethodPost, fmt.Sprintf("/api/v1/catalog/items/%s/retirement", oldSpecialID), app.token, catalog.RetireRequest{
		RequestID: uuid.New(),
		Reason:    "NO_LONGER_OFFERED",
		Note:      "Archived for seasonal rotation",
	})
	require.Equal(t, http.StatusOK, rec.Code, "Retire Old Special: %s", rec.Body.String())

	// 11. Verify All Projections Through HTTP

	// 11.1 Sellable Menu Projection (/api/v1/catalog/menu/sellable)
	rec = doHTTP(t, app.e, http.MethodGet, "/api/v1/catalog/menu/sellable", app.token, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	_, sellable := decodeBody[catalog.SellableMenuResponse](t, rec)
	require.Len(t, sellable.Categories, 2)

	// Find categories
	var sellableCoffee, sellableTea *catalog.SellableCategoryResponse
	for i := range sellable.Categories {
		if sellable.Categories[i].ID == coffeeID {
			sellableCoffee = &sellable.Categories[i]
		} else if sellable.Categories[i].ID == teaID {
			sellableTea = &sellable.Categories[i]
		}
	}
	require.NotNil(t, sellableCoffee)
	require.NotNil(t, sellableTea)

	// Coffee items: Espresso only (Old Special is retired)
	require.Len(t, sellableCoffee.Items, 1)
	assert.Equal(t, "Espresso", sellableCoffee.Items[0].Name)
	require.NotNil(t, sellableCoffee.Items[0].PriceVND)
	assert.Equal(t, int64(38000), *sellableCoffee.Items[0].PriceVND)

	// Tea items: Milk Tea and Green Tea
	require.Len(t, sellableTea.Items, 2)
	var sellableMilkTea, sellableGreenTea *catalog.SellableItemResponse
	for i := range sellableTea.Items {
		if sellableTea.Items[i].ID == milkTeaID {
			sellableMilkTea = &sellableTea.Items[i]
		} else if sellableTea.Items[i].ID == greenTeaID {
			sellableGreenTea = &sellableTea.Items[i]
		}
	}
	require.NotNil(t, sellableMilkTea)
	require.NotNil(t, sellableGreenTea)

	// Milk Tea has sizes Regular (32000) and Large (40000)
	require.Len(t, sellableMilkTea.Sizes, 2)
	for _, s := range sellableMilkTea.Sizes {
		if s.Name == "Regular" {
			assert.Equal(t, int64(32000), s.PriceVND)
		} else if s.Name == "Large" {
			assert.Equal(t, int64(40000), s.PriceVND)
		}
	}

	// Milk Tea has effective modifier groups: Sugar Level (direct) and Toppings (inherited from Tea)
	require.Len(t, sellableMilkTea.ModifierGroups, 2)
	var sellSugar, sellTopping *catalog.SellableModifierGroupResponse
	for i := range sellableMilkTea.ModifierGroups {
		if sellableMilkTea.ModifierGroups[i].ID == sugarGroupID {
			sellSugar = &sellableMilkTea.ModifierGroups[i]
		} else if sellableMilkTea.ModifierGroups[i].ID == toppingGroupID {
			sellTopping = &sellableMilkTea.ModifierGroups[i]
		}
	}
	require.NotNil(t, sellSugar)
	require.NotNil(t, sellTopping)
	assert.Equal(t, []uuid.UUID{sugarOpt50ID}, sellSugar.DefaultOptionIDs)

	// Green Tea has Toppings EXCLUDED, so 0 modifier groups
	require.Empty(t, sellableGreenTea.ModifierGroups, "Green Tea should have 0 groups due to explicit exclusion")
	require.NotNil(t, sellableGreenTea.PriceVND)
	assert.Equal(t, int64(25000), *sellableGreenTea.PriceVND)

	// 11.2 Management Menu Projection (/api/v1/catalog/menu/manage)
	rec = doHTTP(t, app.e, http.MethodGet, "/api/v1/catalog/menu/manage", app.token, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	_, manage := decodeBody[catalog.ManagementMenuResponse](t, rec)
	require.Len(t, manage.Categories, 2)

	var manageCoffee *catalog.ManagementCategoryResponse
	for i := range manage.Categories {
		if manage.Categories[i].ID == coffeeID {
			manageCoffee = &manage.Categories[i]
		}
	}
	require.NotNil(t, manageCoffee)
	// Management includes retired item "Old Special"!
	require.Len(t, manageCoffee.Items, 2)
	var retiredItem *catalog.ManagementItemResponse
	for i := range manageCoffee.Items {
		if manageCoffee.Items[i].ID == oldSpecialID {
			retiredItem = &manageCoffee.Items[i]
		}
	}
	require.NotNil(t, retiredItem)
	assert.True(t, retiredItem.Retired)
	require.NotNil(t, retiredItem.RetirementReason)
	assert.Equal(t, "NO_LONGER_OFFERED", *retiredItem.RetirementReason)

	// 11.3 Availability Menu Projection (/api/v1/catalog/menu/availability)
	rec = doHTTP(t, app.e, http.MethodGet, "/api/v1/catalog/menu/availability", app.token, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	_, availMenu := decodeBody[catalog.AvailabilityMenuResponse](t, rec)
	require.Len(t, availMenu.Categories, 2)

	// 11.4 Modifier Groups (/api/v1/catalog/modifier-groups)
	rec = doHTTP(t, app.e, http.MethodGet, "/api/v1/catalog/modifier-groups", app.token, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	_, modGroups := decodeBody[[]catalog.ManagementModifierGroupResponse](t, rec)
	require.Len(t, modGroups, 2)

	// 11.5 Audit Events (/api/v1/catalog/audit-events)
	rec = doHTTP(t, app.e, http.MethodGet, "/api/v1/catalog/audit-events?limit=100", app.token, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	_, auditList := decodeBody[[]catalog.AuditEventResponse](t, rec)
	require.NotEmpty(t, auditList)

	// Verify events are returned newest-first and have correct actor_id
	for i := 1; i < len(auditList); i++ {
		assert.False(t, auditList[i-1].OccurredAt.Before(auditList[i].OccurredAt), "Audit events must be in newest-first order")
	}

	eventTypes := make(map[string]bool)
	for _, ev := range auditList {
		eventTypes[ev.EventType] = true
		if ev.ActorID != nil {
			assert.Equal(t, app.staffID, *ev.ActorID)
		}
	}

	assert.True(t, eventTypes[catalog.EventCategoryCreated], "must contain category created event")
	assert.True(t, eventTypes[catalog.EventItemCreated], "must contain item created event")
	assert.True(t, eventTypes[catalog.EventModifierGroupCreated], "must contain modifier group created event")
	assert.True(t, eventTypes[catalog.EventItemRepriced], "must contain item repriced event")
	assert.True(t, eventTypes[catalog.EventItemAvailabilityChanged], "must contain availability changed event")
	assert.True(t, eventTypes[catalog.EventItemRetired], "must contain item retired event")
	assert.True(t, eventTypes[catalog.EventCategoryModifierGroupAttached], "must contain category group attached event")
	assert.True(t, eventTypes[catalog.EventItemModifierGroupAttached], "must contain item group attached event")
	assert.True(t, eventTypes[catalog.EventItemInheritedModifierGroupExcluded], "must contain exclusion event")
}

// ============================================================================
// Step 2: Real Concurrency and Denial Scenarios
// ============================================================================

func TestCatalogConcurrencyAndDenials(t *testing.T) {
	app := setupE2EApp(t)

	// Bootstrap Manager
	rec := doHTTP(t, app.e, http.MethodPost, "/api/v1/auth/bootstrap", "", auth.BootstrapManagerRequest{
		DisplayName: "Concurrency Manager",
		LoginCode:   "MGRCONCUR",
		Pin:         "123456",
	})
	require.Equal(t, http.StatusCreated, rec.Code)
	_, bootProfile := decodeBody[auth.StaffProfileResponse](t, rec)
	app.staffID = bootProfile.ID

	// Sign In
	rec = doHTTP(t, app.e, http.MethodPost, "/api/v1/auth/sign-in", "", auth.SignInRequest{
		LoginCode: "MGRCONCUR",
		Pin:       "123456",
	})
	require.Equal(t, http.StatusOK, rec.Code)
	_, signInRes := decodeBody[auth.SignInResponse](t, rec)
	app.token = signInRes.Token

	// Session
	sess, err := app.q.GetSessionByTokenHash(context.Background(), auth.HashToken(app.token))
	require.NoError(t, err)
	app.sessID = sess.SessionID

	actor := catalog.Actor{StaffID: app.staffID, SessionID: app.sessID}

	t.Run("Concurrent_Duplicate_RequestID_ExactlyOneExecutes_SecondReplaysOrConflicts", func(t *testing.T) {
		reqID := uuid.New()
		const goroutines = 2
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(goroutines)

		type result struct {
			statusCode int
			body       string
		}
		results := make([]result, goroutines)

		for i := 0; i < goroutines; i++ {
			idx := i
			go func() {
				defer wg.Done()
				<-start

				rec := doHTTP(t, app.e, http.MethodPost, "/api/v1/catalog/categories", app.token, catalog.CreateCategoryCommand{
					RequestID: reqID,
					Name:      "Concurrent Cat",
				})
				results[idx] = result{statusCode: rec.Code, body: rec.Body.String()}
			}()
		}

		// Release all goroutines simultaneously
		close(start)
		wg.Wait()

		// Validate outcomes: exactly one executes and returns 201; the other returns 201 (identical cached replay).
		successCount := 0
		conflictCount := 0
		for _, r := range results {
			if r.statusCode == http.StatusCreated {
				successCount++
			} else if r.statusCode == http.StatusConflict {
				conflictCount++
			} else {
				t.Fatalf("unexpected status code: %d, body: %s", r.statusCode, r.body)
			}
		}

		assert.Equal(t, goroutines, successCount, "All concurrent requests with identical payload must return 201 Created")
		assert.Equal(t, 0, conflictCount, "No requests with identical payload should receive 409 Conflict")

		// Verify database has exactly 1 category named "Concurrent Cat"
		var count int
		err := app.db.QueryRowContext(context.Background(), `SELECT count(*) FROM menu_categories WHERE name = 'Concurrent Cat'`).Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 1, count, "Exactly 1 category row must exist in the database")
	})

	t.Run("Duplicate_RequestID_With_Different_Payload_Returns_409_Conflict", func(t *testing.T) {
		reqID := uuid.New()

		// First request succeeds
		rec1 := doHTTP(t, app.e, http.MethodPost, "/api/v1/catalog/categories", app.token, catalog.CreateCategoryCommand{
			RequestID: reqID,
			Name:      "First Category For Conflict",
		})
		require.Equal(t, http.StatusCreated, rec1.Code)

		// Second request with SAME request_id but DIFFERENT payload fails with 409 Conflict (REQUEST_CONFLICT)
		rec2 := doHTTP(t, app.e, http.MethodPost, "/api/v1/catalog/categories", app.token, catalog.CreateCategoryCommand{
			RequestID: reqID,
			Name:      "Different Payload Category",
		})
		require.Equal(t, http.StatusConflict, rec2.Code)
		assert.Contains(t, rec2.Body.String(), "REQUEST_CONFLICT")
	})

	t.Run("Concurrent_Normalized_Name_Collision_OneSucceeds_SecondFailsWithNameConflict", func(t *testing.T) {
		const goroutines = 2
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(goroutines)

		type result struct {
			statusCode int
			body       string
		}
		results := make([]result, goroutines)

		// Two distinct request IDs, but names that normalize to the exact same key "juice"
		names := []string{"Juice", "  JUICE  "}

		for i := 0; i < goroutines; i++ {
			idx := i
			go func() {
				defer wg.Done()
				<-start

				rec := doHTTP(t, app.e, http.MethodPost, "/api/v1/catalog/categories", app.token, catalog.CreateCategoryCommand{
					RequestID: uuid.New(),
					Name:      names[idx],
				})
				results[idx] = result{statusCode: rec.Code, body: rec.Body.String()}
			}()
		}

		close(start)
		wg.Wait()

		createdCount := 0
		nameConflictCount := 0
		for _, r := range results {
			if r.statusCode == http.StatusCreated {
				createdCount++
			} else if r.statusCode == http.StatusConflict {
				nameConflictCount++
				assert.Contains(t, r.body, "CATALOG_NAME_CONFLICT")
			} else {
				t.Fatalf("unexpected status code: %d, body: %s", r.statusCode, r.body)
			}
		}

		assert.Equal(t, 1, createdCount, "Exactly one category must be created")
		assert.Equal(t, 1, nameConflictCount, "Colliding request must fail with CATALOG_NAME_CONFLICT (409)")

		var count int
		err := app.db.QueryRowContext(context.Background(), `SELECT count(*) FROM menu_categories WHERE normalized_name = 'juice'`).Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 1, count, "Only 1 database record with normalized_name 'juice' should exist")
	})

	t.Run("Revoke_Role_Between_Request_And_Replay_Assert_Replay_Denial_And_Audit_Evidence", func(t *testing.T) {
		ctx := context.Background()
		reqID := uuid.New()
		cmd := catalog.CreateCategoryCommand{
			RequestID: reqID,
			Name:      "Pastries",
		}

		// 1. Initial request succeeds through HTTP
		rec := doHTTP(t, app.e, http.MethodPost, "/api/v1/catalog/categories", app.token, cmd)
		require.Equal(t, http.StatusCreated, rec.Code, "Initial request must succeed: %s", rec.Body.String())

		// Count initial audit events for catalog.authorization_denied
		var denialsBefore int
		err := app.db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE event_type = $1`, catalog.EventAuthorizationDenied).Scan(&denialsBefore)
		require.NoError(t, err)

		// 2. Revoke Manager role from the staff identity, demoting to Barista (which lacks catalog.administer_structure)
		err = app.q.ClearStaffRoles(ctx, app.staffID)
		require.NoError(t, err)
		err = app.q.AddStaffRole(ctx, sqlc.AddStaffRoleParams{
			StaffIdentityID: app.staffID,
			Role:            auth.RoleBarista,
		})
		require.NoError(t, err)

		// 3. Direct domain handler replay assertion:
		// When the actor whose role was revoked tries to replay the exact same mutation,
		// the domain executor verifies authorization BEFORE checking replay, records
		// catalog.authorization_denied to audit_events, and returns ErrForbidden.
		status, _, handlerErr := app.slices.CreateCategory.Handle(ctx, actor, cmd)
		require.Error(t, handlerErr)
		assert.True(t, errors.Is(handlerErr, catalog.ErrForbidden), "Expected ErrForbidden, got: %v", handlerErr)
		assert.Equal(t, 0, status)

		// 4. Assert denial-only audit evidence:
		// Verify an audit event with event_type = 'catalog.authorization_denied' was inserted
		var denialsAfter int
		err = app.db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE event_type = $1`, catalog.EventAuthorizationDenied).Scan(&denialsAfter)
		require.NoError(t, err)
		assert.Equal(t, denialsBefore+1, denialsAfter, "Expected exactly 1 new catalog.authorization_denied audit event")

		// Verify the details of the denial audit event
		var denialDetailsJSON []byte
		var denialActorID, denialSessionID uuid.UUID
		err = app.db.QueryRowContext(ctx, `
			SELECT actor_id, session_id, details 
			FROM audit_events 
			WHERE event_type = $1 
			ORDER BY occurred_at DESC 
			LIMIT 1
		`, catalog.EventAuthorizationDenied).Scan(&denialActorID, &denialSessionID, &denialDetailsJSON)
		require.NoError(t, err)
		assert.Equal(t, app.staffID, denialActorID)
		assert.Equal(t, app.sessID, denialSessionID)

		var denialDetails map[string]any
		err = json.Unmarshal(denialDetailsJSON, &denialDetails)
		require.NoError(t, err)
		assert.Equal(t, "catalog.category.create", denialDetails["operation"])

		// 5. Also assert HTTP layer denial:
		// Attempting replay over HTTP with the demoted user's token results in 403 Forbidden
		recReplay := doHTTP(t, app.e, http.MethodPost, "/api/v1/catalog/categories", app.token, cmd)
		assert.Equal(t, http.StatusForbidden, recReplay.Code, "Replay over HTTP must be denied with 403 Forbidden")

		// 6. Assert session revocation denial:
		// Revoke the session entirely
		err = app.q.RevokeSession(ctx, app.sessID)
		require.NoError(t, err)

		// Handler with revoked session returns ErrUnauthorized and records another denial audit event
		_, _, handlerErr2 := app.slices.CreateCategory.Handle(ctx, actor, cmd)
		require.Error(t, handlerErr2)
		assert.True(t, errors.Is(handlerErr2, catalog.ErrUnauthorized), "Expected ErrUnauthorized, got: %v", handlerErr2)

		var denialsFinal int
		err = app.db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE event_type = $1`, catalog.EventAuthorizationDenied).Scan(&denialsFinal)
		require.NoError(t, err)
		assert.Equal(t, denialsAfter+1, denialsFinal, "Expected second denial audit event for revoked session")

		// HTTP with revoked session returns 401 Unauthorized
		recRevoked := doHTTP(t, app.e, http.MethodPost, "/api/v1/catalog/categories", app.token, cmd)
		assert.Equal(t, http.StatusUnauthorized, recRevoked.Code, "Request over HTTP with revoked session must return 401")
	})
}
