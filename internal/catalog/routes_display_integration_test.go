//go:build integration

package catalog_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
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

	upload := func(t *testing.T, token string, requestID string, file []byte) *httptest.ResponseRecorder {
		t.Helper()
		var body bytes.Buffer
		w := multipart.NewWriter(&body)
		require.NoError(t, w.WriteField("request_id", requestID))
		part, err := w.CreateFormFile("file", "photo.png")
		require.NoError(t, err)
		_, err = part.Write(file)
		require.NoError(t, err)
		require.NoError(t, w.Close())
		req := httptest.NewRequest(http.MethodPut, "/api/v1/catalog/items/"+item.String()+"/image", &body)
		req.Header.Set("Content-Type", w.FormDataContentType())
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		tc.e.ServeHTTP(rec, req)
		return rec
	}

	t.Run("manager uploads an image", func(t *testing.T) {
		rec := upload(t, tc.managerToken, uuid.NewString(), tinyPNG(t))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		_, data := parseAPIResponse[catalog.ItemImageResponse](t, rec)
		require.NotNil(t, data.ImageURL)
	})

	t.Run("text renamed to png is INVALID_IMAGE", func(t *testing.T) {
		rec := upload(t, tc.managerToken, uuid.NewString(), []byte("plain text, not a picture"))
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		var res response.APIResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
		assert.Equal(t, "INVALID_IMAGE", res.Error.Code)
	})

	t.Run("2 MB upload is IMAGE_TOO_LARGE", func(t *testing.T) {
		rec := upload(t, tc.managerToken, uuid.NewString(), append(tinyPNG(t), make([]byte, 2<<20)...))
		assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
		var res response.APIResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
		assert.Equal(t, "IMAGE_TOO_LARGE", res.Error.Code)
	})

	t.Run("missing request_id is INVALID_INPUT", func(t *testing.T) {
		rec := upload(t, tc.managerToken, "", tinyPNG(t))
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("manager clears the image", func(t *testing.T) {
		rec := doJSONRequest(t, tc.e, http.MethodDelete, "/api/v1/catalog/items/"+item.String()+"/image", tc.managerToken,
			catalog.MutationRequest{RequestID: uuid.New()})
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	})
}
