package catalog_test

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/catalog"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 200, A: 255})
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func tinyWebP() []byte {
	// RIFF header + "WEBPVP8 " is what http.DetectContentType matches.
	b := []byte("RIFF\x1a\x00\x00\x00WEBPVP8 ")
	return append(b, make([]byte, 16)...)
}

func TestMediaStoreSave(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	m, err := catalog.NewMediaStore(root)
	require.NoError(t, err)

	data := tinyPNG(t)
	stored, err := m.Save(data)
	require.NoError(t, err)
	sum := sha256.Sum256(data)
	assert.Equal(t, hex.EncodeToString(sum[:]), stored.SHA256)
	assert.Equal(t, stored.SHA256+".png", stored.Key)

	onDisk, err := os.ReadFile(filepath.Join(root, "catalog", stored.Key))
	require.NoError(t, err)
	assert.Equal(t, data, onDisk)

	again, err := m.Save(data)
	require.NoError(t, err)
	assert.Equal(t, stored, again, "same bytes, same key")

	webp, err := m.Save(tinyWebP())
	require.NoError(t, err)
	assert.Equal(t, ".webp", filepath.Ext(webp.Key))

	entries, err := os.ReadDir(filepath.Join(root, "catalog"))
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".tmp-", "no temp file left behind")
	}
}

func TestMediaStoreRejects(t *testing.T) {
	t.Parallel()
	m, err := catalog.NewMediaStore(t.TempDir())
	require.NoError(t, err)

	_, err = m.Save(nil)
	assert.True(t, errors.Is(err, catalog.ErrInvalidImage))
	_, err = m.Save([]byte("hello, this is plain text pretending to be a png"))
	assert.True(t, errors.Is(err, catalog.ErrInvalidImage))
	_, err = m.Save(append(tinyPNG(t), make([]byte, catalog.MaxImageBytes)...))
	assert.True(t, errors.Is(err, catalog.ErrImageTooLarge))
}

func TestImageURL(t *testing.T) {
	t.Parallel()
	assert.Nil(t, catalog.ImageURL(sql.NullString{}))
	u := catalog.ImageURL(sql.NullString{String: "abc.png", Valid: true})
	require.NotNil(t, u)
	assert.Equal(t, "/media/catalog/abc.png", *u)
}

func TestMediaRoute(t *testing.T) {
	t.Parallel()
	m, err := catalog.NewMediaStore(t.TempDir())
	require.NoError(t, err)
	stored, err := m.Save(tinyPNG(t))
	require.NoError(t, err)

	e := echo.New()
	m.RegisterRoutes(e)
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	rec := get("/media/catalog/" + stored.Key)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "image/png", rec.Header().Get("Content-Type"))
	assert.Equal(t, "public, max-age=31536000, immutable", rec.Header().Get("Cache-Control"))
	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))

	for _, bad := range []string{
		"/media/catalog/..%2F..%2Fgo.mod",
		"/media/catalog/abc.png",
		"/media/catalog/" + stored.SHA256 + ".gif",
		"/media/catalog/" + stored.SHA256[:63] + "0.png", // valid shape, missing file
	} {
		assert.Equal(t, http.StatusNotFound, get(bad).Code, bad)
	}
}
