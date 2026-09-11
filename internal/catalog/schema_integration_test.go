//go:build integration

package catalog_test

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/database"
	"github.com/stretchr/testify/require"
)

func openCatalogTestDB(t *testing.T) *sql.DB {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	require.Contains(t, url, "_test")
	db, err := database.Open(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	return db
}

func requireTableExists(t *testing.T, db *sql.DB, tableName string) {
	t.Helper()
	var table string
	err := db.QueryRow(`SELECT $1::regclass::text`, tableName).Scan(&table)
	require.NoError(t, err, "table %s must exist", tableName)
}

func TestCatalogMigrationConstraints(t *testing.T) {
	db := openCatalogTestDB(t)

	// Verify all eleven Catalog and supporting tables exist.
	catalogTables := []string{
		"menu_categories",
		"menu_items",
		"menu_item_sizes",
		"modifier_groups",
		"modifier_options",
		"item_modifier_groups",
		"category_modifier_groups",
		"item_modifier_group_exclusions",
		"modifier_group_default_options",
		"catalog_mutation_requests",
		"audit_events",
	}
	for _, tbl := range catalogTables {
		requireTableExists(t, db, tbl)
	}

	// modifier_groups: min_selections > max_selections must fail
	_, err := db.Exec(`INSERT INTO modifier_groups
		(name, normalized_name, min_selections, max_selections)
		VALUES ('Sugar', 'sugar', 2, 1)`)
	require.Error(t, err, "min_selections > max_selections must be rejected")

	// modifier_groups: max_selections < 1 must fail
	_, err = db.Exec(`INSERT INTO modifier_groups
		(name, normalized_name, min_selections, max_selections)
		VALUES ('Bad', 'bad', 0, 0)`)
	require.Error(t, err, "max_selections < 1 must be rejected")

	// modifier_options: negative surcharge must fail
	_, err = db.Exec(`INSERT INTO modifier_options
		(id, modifier_group_id, name, normalized_name, surcharge_vnd)
		VALUES (gen_random_uuid(), gen_random_uuid(), 'Test', 'test', -1)`)
	require.Error(t, err, "negative surcharge must be rejected")

	// menu_items: price_vnd out of range must fail
	_, err = db.Exec(`INSERT INTO menu_items
		(category_id, name, normalized_name, price_vnd)
		VALUES (gen_random_uuid(), 'Expensive', 'expensive', 0)`)
	require.Error(t, err, "price_vnd = 0 must be rejected")

	// modifier_groups: normalized_name uniqueness must fail
	_, _ = db.Exec(`DELETE FROM modifier_groups WHERE normalized_name = 'sugar'`)
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM modifier_groups WHERE normalized_name = 'sugar'`)
	})
	_, err = db.Exec(`INSERT INTO modifier_groups
		(name, normalized_name, min_selections, max_selections)
		VALUES ('Sugar', 'sugar', 1, 1)`)
	require.NoError(t, err, "first modifier_group with normalized_name 'sugar' should succeed")

	_, err = db.Exec(`INSERT INTO modifier_groups
		(name, normalized_name, min_selections, max_selections)
		VALUES ('Sweetener', 'sugar', 1, 1)`)
	require.Error(t, err, "second modifier_group with same normalized_name must be rejected")

	// menu_items: retirement_consistency_check must fail if retired_at set without retirement_reason
	_, err = db.Exec(`INSERT INTO menu_items
		(category_id, name, normalized_name, price_vnd, retired_at, retirement_reason)
		VALUES (gen_random_uuid(), 'RetiredItem', 'retireditem', 10000, now(), NULL)`)
	require.Error(t, err, "retired_at with null retirement_reason must be rejected")

	// menu_categories: retirement_consistency_check must fail if retired_at set without retirement_reason
	_, err = db.Exec(`INSERT INTO menu_categories
		(name, normalized_name, retired_at, retirement_reason)
		VALUES ('RetiredCategory', 'retiredcategory', now(), NULL)`)
	require.Error(t, err, "retired_at with null retirement_reason must be rejected")

	// menu_categories: retirement_note_limit_check must fail if retirement_note exceeds 500 chars
	_, err = db.Exec(`INSERT INTO menu_categories
		(name, normalized_name, retired_at, retirement_reason, retirement_note)
		VALUES ('LongNoteCategory', 'longnotecategory', now(), 'obsolete', repeat('x', 501))`)
	require.Error(t, err, "retirement_note longer than 500 chars must be rejected")
}
