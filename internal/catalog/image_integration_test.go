//go:build integration

package catalog_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/catalog"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestMediaStore(t *testing.T) *catalog.MediaStore {
	t.Helper()
	m, err := catalog.NewMediaStore(t.TempDir())
	require.NoError(t, err)
	return m
}

func TestItemImageCommands(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	root := t.TempDir()
	media, err := catalog.NewMediaStore(root)
	require.NoError(t, err)
	setImage := catalog.NewSetItemImageHandler(runner, media)
	clearImage := catalog.NewClearItemImageHandler(runner)
	ctx := context.Background()
	price := int64(30000)

	t.Run("upload stores the file and the key; replay is a no-op", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		actor := managerActor(t, db, q)
		cat := createTestCategoryDirect(t, db, "Coffee")
		item := createTestItemDirect(t, db, cat, "A", &price, false)
		data := tinyPNG(t)
		cmd := catalog.SetItemImageCommand{RequestID: uuid.New(), ItemID: item, Data: data}

		status, res, err := setImage.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		require.NotNil(t, res.ImageURL)

		var key string
		require.NoError(t, db.QueryRow(`SELECT image_key FROM menu_items WHERE id = $1`, item).Scan(&key))
		assert.Equal(t, "/media/catalog/"+key, *res.ImageURL)
		_, err = os.Stat(filepath.Join(root, "catalog", key))
		require.NoError(t, err)

		_, again, err := setImage.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		assert.Equal(t, res, again)
		assert.Equal(t, 1, auditCount(t, db, catalog.EventItemImageSet))

		cmd.Data = tinyWebP()
		_, _, err = setImage.Handle(ctx, actor, cmd)
		assert.True(t, errors.Is(err, catalog.ErrRequestConflict), "same request_id, different bytes")
	})

	t.Run("clear removes the key and keeps the file", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		actor := managerActor(t, db, q)
		cat := createTestCategoryDirect(t, db, "Coffee")
		item := createTestItemDirect(t, db, cat, "A", &price, false)
		_, _, err := setImage.Handle(ctx, actor, catalog.SetItemImageCommand{RequestID: uuid.New(), ItemID: item, Data: tinyPNG(t)})
		require.NoError(t, err)

		_, res, err := clearImage.Handle(ctx, actor, catalog.ClearItemImageCommand{RequestID: uuid.New(), ItemID: item})
		require.NoError(t, err)
		assert.Nil(t, res.ImageURL)
		var key *string
		require.NoError(t, db.QueryRow(`SELECT image_key FROM menu_items WHERE id = $1`, item).Scan(&key))
		assert.Nil(t, key)
		assert.Equal(t, 1, auditCount(t, db, catalog.EventItemImageCleared))
	})

	t.Run("invalid bytes never reach the database", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		cat := createTestCategoryDirect(t, db, "Coffee")
		item := createTestItemDirect(t, db, cat, "A", &price, false)
		_, _, err := setImage.Handle(ctx, managerActor(t, db, q), catalog.SetItemImageCommand{RequestID: uuid.New(), ItemID: item, Data: []byte("not an image at all, just text")})
		assert.True(t, errors.Is(err, catalog.ErrInvalidImage))
	})

	t.Run("retired item is ErrEntityRetired", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		cat := createTestCategoryDirect(t, db, "Coffee")
		item := createTestItemDirect(t, db, cat, "A", &price, true)
		_, _, err := setImage.Handle(ctx, managerActor(t, db, q), catalog.SetItemImageCommand{RequestID: uuid.New(), ItemID: item, Data: tinyPNG(t)})
		assert.True(t, errors.Is(err, catalog.ErrEntityRetired))
	})
}
