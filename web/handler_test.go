package web_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/Mirai3103/pos-cafe/web"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestEcho(t *testing.T, mockFS fstest.MapFS) *echo.Echo {
	t.Helper()
	e := echo.New()

	// Register sample API & health routes to verify they take precedence
	e.GET("/health", func(c echo.Context) error {
		return c.String(http.StatusOK, "healthy")
	})
	e.GET("/api/v1/ping", func(c echo.Context) error {
		return c.String(http.StatusOK, "pong")
	})

	err := web.RegisterFS(e, mockFS)
	require.NoError(t, err)

	return e
}

func TestRegisterFS(t *testing.T) {
	mockFS := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte("<!DOCTYPE html><html><body><div id=\"root\"></div></body></html>"),
		},
		"assets/main-123.js": &fstest.MapFile{
			Data: []byte("console.log('pos-cafe');"),
		},
		"assets/style-456.css": &fstest.MapFile{
			Data: []byte("body { margin: 0; }"),
		},
		"favicon.ico": &fstest.MapFile{
			Data: []byte("fake-icon"),
		},
	}

	e := setupTestEcho(t, mockFS)

	t.Run("serves index.html at root", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), "<div id=\"root\"></div>")
		assert.Contains(t, rec.Header().Get("Content-Type"), "text/html")
		assert.Contains(t, rec.Header().Get("Cache-Control"), "no-cache")
	})

	t.Run("serves index.html for SPA client routes", func(t *testing.T) {
		routes := []string{"/tables", "/pos", "/kds", "/shift", "/auth/login", "/settings/general"}
		for _, r := range routes {
			req := httptest.NewRequest(http.MethodGet, r, nil)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusOK, rec.Code, "route: %s", r)
			assert.Contains(t, rec.Body.String(), "<div id=\"root\"></div>", "route: %s", r)
			assert.Contains(t, rec.Header().Get("Cache-Control"), "no-cache", "route: %s", r)
		}
	})

	t.Run("serves static assets with immutable cache header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/assets/main-123.js", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), "console.log('pos-cafe');")
		assert.Contains(t, rec.Header().Get("Cache-Control"), "immutable")
	})

	t.Run("returns 404 for missing static asset instead of falling back to index.html", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/assets/non-existent-chunk.js", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
		assert.NotContains(t, rec.Body.String(), "<div id=\"root\"></div>")
	})

	t.Run("returns 404 for missing file with extension", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/logo.png", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
		assert.NotContains(t, rec.Body.String(), "<div id=\"root\"></div>")
	})

	t.Run("serves existing root-level static asset", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/favicon.ico", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "fake-icon", rec.Body.String())
	})

	t.Run("does not swallow API routes that exist", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "pong", rec.Body.String())
	})

	t.Run("does not swallow unregistered API routes with index.html", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/unknown-endpoint", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
		assert.NotContains(t, rec.Body.String(), "<div id=\"root\"></div>")
	})

	t.Run("does not swallow health check route", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "healthy", rec.Body.String())
	})

	t.Run("supports HEAD requests for SPA routes", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodHead, "/tables", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Empty(t, rec.Body.String())
	})
}

func TestRegisterFS_MissingIndexHTML(t *testing.T) {
	emptyFS := fstest.MapFS{}
	e := echo.New()
	err := web.RegisterFS(e, emptyFS)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "frontend build not found")
}
