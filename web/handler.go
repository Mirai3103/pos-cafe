package web

import (
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/labstack/echo/v4"
)

// RegisterHandlers mounts the embedded SPA static assets and HTML5 fallback onto the Echo router.
func RegisterHandlers(e *echo.Echo) error {
	sub, err := DistFS()
	if err != nil {
		return err
	}

	return RegisterFS(e, sub)
}

// RegisterFS mounts a given fs.FS filesystem onto Echo. Useful for testing with in-memory filesystems.
func RegisterFS(e *echo.Echo, fsys fs.FS) error {
	fileServer := http.FileServer(http.FS(fsys))

	handler := func(c echo.Context) error {
		reqPath := path.Clean(c.Request().URL.Path)

		// Never handle API, Swagger, or health check routes in SPA handler
		if strings.HasPrefix(reqPath, "/api") ||
			strings.HasPrefix(reqPath, "/swagger") ||
			reqPath == "/health" {
			return echo.ErrNotFound
		}

		// Clean path for fs.Open: remove leading slash
		cleanRelative := strings.TrimPrefix(reqPath, "/")
		if cleanRelative == "" {
			cleanRelative = "index.html"
		}

		// Check if the requested file exists in embedded filesystem
		file, err := fsys.Open(cleanRelative)
		if err == nil {
			stat, statErr := file.Stat()
			_ = file.Close()
			if statErr == nil && !stat.IsDir() {
				if cleanRelative == "index.html" {
					c.Response().Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
				} else if strings.HasPrefix(reqPath, "/assets/") {
					c.Response().Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				fileServer.ServeHTTP(c.Response(), c.Request())
				return nil
			}
		}

		// If the request specifically targeted an asset or has a file extension,
		// and it wasn't found above, return 404 instead of index.html.
		// This prevents browsers from interpreting a 404 HTML page as a JS/CSS module.
		if strings.HasPrefix(reqPath, "/assets/") || (path.Ext(reqPath) != "" && path.Ext(reqPath) != ".html") {
			return echo.ErrNotFound
		}

		// For all client-side SPA routes (e.g. /, /tables, /kds, /pos, /shift, /auth/login),
		// serve index.html with Cache-Control: no-cache.
		indexBytes, err := fs.ReadFile(fsys, "index.html")
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return echo.NewHTTPError(http.StatusNotFound, "frontend build not found: run 'make build' or 'cd web && bun run build'")
			}
			return echo.ErrInternalServerError
		}

		c.Response().Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		if c.Request().Method == http.MethodHead {
			c.Response().Header().Set("Content-Type", "text/html; charset=UTF-8")
			return c.NoContent(http.StatusOK)
		}
		return c.HTMLBlob(http.StatusOK, indexBytes)
	}

	e.GET("/", handler)
	e.GET("/*", handler)
	e.HEAD("/", handler)
	e.HEAD("/*", handler)

	return nil
}
