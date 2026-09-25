package catalog

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"

	"github.com/labstack/echo/v4"
)

// Image limits and the public URL prefix (ADR-057).
const (
	MaxImageBytes      = 1 << 20
	MaxImageUploadBody = MaxImageBytes + 64<<10 // multipart framing and the request_id field
	MediaURLPrefix     = "/media/catalog/"
	mediaCatalogDir    = "catalog"
)

var (
	ErrInvalidImage  = errors.New("invalid image")
	ErrImageTooLarge = errors.New("image too large")
)

var (
	imageKeyPattern = regexp.MustCompile(`^[0-9a-f]{64}\.(jpg|png|webp)$`)
	imageExtByType  = map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp"}
	imageTypeByExt  = map[string]string{".jpg": "image/jpeg", ".png": "image/png", ".webp": "image/webp"}
)

// StoredImage names a saved image file.
type StoredImage struct {
	Key    string
	SHA256 string
}

// MediaStore keeps catalog images as content-addressed files.
type MediaStore struct {
	dir string
}

// NewMediaStore creates root/catalog if needed.
func NewMediaStore(root string) (*MediaStore, error) {
	dir := filepath.Join(root, mediaCatalogDir)
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // G301: served publicly by design
		return nil, fmt.Errorf("create media directory %s: %w", dir, err)
	}
	return &MediaStore{dir: dir}, nil
}

// Save validates data and writes it under its content hash. It writes to a temp
// file and renames, so a reader never sees a partial image. Saving bytes that
// already exist is a no-op.
func (m *MediaStore) Save(data []byte) (StoredImage, error) {
	if len(data) == 0 {
		return StoredImage{}, fmt.Errorf("%w: file is empty", ErrInvalidImage)
	}
	if len(data) > MaxImageBytes {
		return StoredImage{}, fmt.Errorf("%w: %d bytes exceeds %d", ErrImageTooLarge, len(data), MaxImageBytes)
	}
	contentType := http.DetectContentType(data)
	ext, ok := imageExtByType[contentType]
	if !ok {
		return StoredImage{}, fmt.Errorf("%w: unsupported content type %q", ErrInvalidImage, contentType)
	}
	sum := sha256.Sum256(data)
	hexSum := hex.EncodeToString(sum[:])
	stored := StoredImage{Key: hexSum + ext, SHA256: hexSum}

	final := filepath.Join(m.dir, stored.Key)
	if _, err := os.Stat(final); err == nil {
		return stored, nil
	}
	tmp, err := os.CreateTemp(m.dir, ".tmp-*")
	if err != nil {
		return StoredImage{}, fmt.Errorf("create temp image: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return StoredImage{}, fmt.Errorf("write temp image: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return StoredImage{}, fmt.Errorf("close temp image: %w", err)
	}
	if err := os.Rename(tmpName, final); err != nil {
		_ = os.Remove(tmpName)
		return StoredImage{}, fmt.Errorf("rename image into place: %w", err)
	}
	return stored, nil
}

// RegisterRoutes mounts GET /media/catalog/:key on the root router, outside
// /api/v1 and outside RequireAuth: an <img> tag cannot send a bearer token.
func (m *MediaStore) RegisterRoutes(e *echo.Echo) {
	e.GET(MediaURLPrefix+":key", m.serve)
}

func (m *MediaStore) serve(c echo.Context) error {
	key := c.Param("key")
	if !imageKeyPattern.MatchString(key) {
		return echo.ErrNotFound
	}
	p := filepath.Join(m.dir, key)
	if info, err := os.Stat(p); err != nil || info.IsDir() {
		return echo.ErrNotFound
	}
	h := c.Response().Header()
	h.Set(echo.HeaderContentType, imageTypeByExt[filepath.Ext(key)])
	h.Set("Cache-Control", "public, max-age=31536000, immutable")
	h.Set("X-Content-Type-Options", "nosniff")
	return c.File(p)
}

// ImageURL renders a stored key as a relative URL, or nil when there is none.
func ImageURL(key sql.NullString) *string {
	if !key.Valid {
		return nil
	}
	u := MediaURLPrefix + key.String
	return &u
}
