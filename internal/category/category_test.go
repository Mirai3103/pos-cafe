package category_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/category"
	"github.com/Mirai3103/pos-cafe/internal/database"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/Mirai3103/pos-cafe/internal/validator"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestApp(t *testing.T) (*echo.Echo, func()) {
	t.Helper()

	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos?sslmode=disable"
	}

	ctx := context.Background()
	db, err := database.Open(ctx, dbURL)
	require.NoError(t, err)

	// Clean table before test suite
	_, _ = db.ExecContext(ctx, "TRUNCATE TABLE categories RESTART IDENTITY CASCADE")

	queries := sqlc.New(db)
	slices := category.NewSlices(queries, nil)

	e := echo.New()
	e.Validator = validator.New()

	v1 := e.Group("/api/v1")
	slices.RegisterRoutes(v1)

	cleanup := func() {
		_, _ = db.ExecContext(ctx, "TRUNCATE TABLE categories RESTART IDENTITY CASCADE")
		_ = db.Close()
	}

	return e, cleanup
}

func TestCategoryEndpoints(t *testing.T) {
	e, cleanup := setupTestApp(t)
	defer cleanup()

	t.Run("Create Category - Success", func(t *testing.T) {
		reqBody := category.CreateCommand{
			Name:         "Cà phê truyền thống",
			Description:  "Cà phê phin, đen đá, sữa đá",
			DisplayOrder: 1,
		}
		body, _ := json.Marshal(reqBody)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/categories", bytes.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusCreated, rec.Code)

		var resp response.APIResponse
		err := json.Unmarshal(rec.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.True(t, resp.Success)
	})

	t.Run("Create Category - Validation Failure (Name too short)", func(t *testing.T) {
		reqBody := category.CreateCommand{
			Name: "C", // < 2 chars
		}
		body, _ := json.Marshal(reqBody)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/categories", bytes.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("Create Category - Conflict (Duplicate Name)", func(t *testing.T) {
		reqBody := category.CreateCommand{
			Name: "Cà phê truyền thống", // Duplicate
		}
		body, _ := json.Marshal(reqBody)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/categories", bytes.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusConflict, rec.Code)
	})

	t.Run("Get Category By ID - Success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/categories/1", nil)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("Get Category By ID - Not Found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/categories/999", nil)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("List Categories", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/categories", nil)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("Update Category - Success", func(t *testing.T) {
		reqBody := category.UpdateCommand{
			Name:         "Cà phê pha phin Việt Nam",
			Description:  "Hương vị đậm đà nguyên bản",
			DisplayOrder: 2,
		}
		body, _ := json.Marshal(reqBody)

		req := httptest.NewRequest(http.MethodPut, "/api/v1/categories/1", bytes.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("Delete Category - Success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/categories/1", nil)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNoContent, rec.Code)

		// Verify it's deleted
		getReq := httptest.NewRequest(http.MethodGet, "/api/v1/categories/1", nil)
		getRec := httptest.NewRecorder()
		e.ServeHTTP(getRec, getReq)

		assert.Equal(t, http.StatusNotFound, getRec.Code)
	})
}
