//go:build integration

package catalog_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/catalog"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDisplayDetailsRoutes(t *testing.T) {
	tc := setupHTTPTest(t)
	price := int64(30000)
	cat := createTestCategoryDirect(t, tc.db, "Coffee")
	item := createTestItemDirect(t, tc.db, cat, "Americano", &price, false)
	itemPath := "/api/v1/catalog/items/" + item.String() + "/details"

	t.Run("manager sets item details", func(t *testing.T) {
		rec := doJSONRequest(t, tc.e, http.MethodPatch, itemPath, tc.managerToken, map[string]any{
			"request_id": uuid.New(), "code": "AM", "badge": "NEW", "description": nil,
		})
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		_, data := parseAPIResponse[catalog.ItemDetailsResponse](t, rec)
		assert.Equal(t, "AM", *data.Code)
		assert.Nil(t, data.Description)
	})

	t.Run("a missing key is INVALID_INPUT", func(t *testing.T) {
		rec := doJSONRequest(t, tc.e, http.MethodPatch, itemPath, tc.managerToken, map[string]any{
			"request_id": uuid.New(), "code": "AM", "badge": nil,
		})
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		var res response.APIResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
		assert.Equal(t, "INVALID_INPUT", res.Error.Code)
	})

	t.Run("cashier is forbidden", func(t *testing.T) {
		rec := doJSONRequest(t, tc.e, http.MethodPatch, itemPath, tc.cashierToken, map[string]any{
			"request_id": uuid.New(), "code": nil, "badge": nil, "description": nil,
		})
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("manager sets category details", func(t *testing.T) {
		rec := doJSONRequest(t, tc.e, http.MethodPatch, "/api/v1/catalog/categories/"+cat.String()+"/details", tc.managerToken,
			catalog.SetCategoryDetailsRequest{RequestID: uuid.New(), Icon: strPtr("coffee"), DisplayOrder: 2})
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		_, data := parseAPIResponse[catalog.CategoryDetailsResponse](t, rec)
		assert.Equal(t, int32(2), data.DisplayOrder)
	})
}
