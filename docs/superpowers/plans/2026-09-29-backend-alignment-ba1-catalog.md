# Backend Alignment BA-1 — Catalog Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the Go catalog the display fields, item images, and structural commands the `settings.html` catalog forms need, return them from the menu projections, and replace the slice 9a availability-card mocks with real data.

**Architecture:** One migration adds nullable display columns. Eleven new idempotent, audited commands follow the existing `ExecuteMutation` pipeline in `internal/catalog/executor.go`. Images are content-addressed files under `MEDIA_DIR`, served unauthenticated at `/media/catalog/{key}`. Projections gain fields; the availability projection gates prices on `catalog.view_prices`.

**Tech Stack:** Go 1.27, Echo v4, PostgreSQL, sqlc (`make sqlc`), swag (`make swagger`), testify; web: React + bun + orval (`bun run codegen`).

**Spec:** [`docs/superpowers/specs/2026-09-29-backend-alignment-ba1-catalog-design.md`](../specs/2026-09-29-backend-alignment-ba1-catalog-design.md). Read it before any task; section numbers below refer to it.

## Global Constraints

- Every mutation: `request_id` required, idempotent through `ExecuteMutation`, one Audit Event in the same transaction (ADR-048). Never call `db.BeginTx` directly in a command.
- Capability `catalog.administer_structure` on every new command. Commands 4 and 5 (add Size, add Option) also need `catalog.change_price` and a fresh Manager PIN (`RequireManagerPIN: true`).
- Item code: trimmed, stored as entered; key is lowercase and must match `^[a-z0-9]{1,12}$`. Unique among non-retired items.
- Badge enum: `BEST_SELLER`, `HOT`, `NEW`, `SIGNATURE`, `CHEF_PICK`.
- Description: trimmed, 1–300 runes, blank stored as `NULL`.
- Category icon: `^[a-z0-9-]{1,40}$`. `display_order`: 0–9999.
- Image: ≤ 1 MiB (`1 << 20` bytes), type sniffed with `http.DetectContentType`, only `image/jpeg`→`.jpg`, `image/png`→`.png`, `image/webp`→`.webp`. Key `hex(sha256) + ext`, regex `^[0-9a-f]{64}\.(jpg|png|webp)$`.
- Media URL: `/media/catalog/<key>`, relative. Served with `Cache-Control: public, max-age=31536000, immutable` and `X-Content-Type-Options: nosniff`, no auth.
- Replace-set lists: at most 500 ids each, no duplicates, no nil UUID.
- New error codes: `CATALOG_CODE_CONFLICT` (409), `INVALID_IMAGE` (400), `IMAGE_TOO_LARGE` (413).
- Swagger comments are Vietnamese, matching `internal/catalog/http.go`.
- Integration tests carry `//go:build integration`, live in `package catalog_test`, and run with `make test-integration` (needs `make docker-up`). Unit tests have no build tag and run with `make test`.
- Commit messages end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## File Map

| File | Responsibility | Task |
| --- | --- | --- |
| `internal/database/migrations/000016_add_catalog_display_fields.sql` | Columns, checks, unique code index, backfill | 1 |
| `sql/queries/catalog.sql` | Existing queries return new columns; new queries per task | 1, 4–11 |
| `internal/catalog/domain_display.go` | Pure validators and id-set helpers | 2 |
| `internal/catalog/media.go` | `MediaStore`: save, serve, URL | 3 |
| `internal/catalog/errors.go` | New sentinels and HTTP mapping | 3, 4 |
| `internal/catalog/domain.go` | New `Op*` / `Event*` constants | 4 |
| `internal/catalog/display.go` | Item and category details commands | 4 |
| `internal/catalog/image.go` | Set and clear item image commands | 5 |
| `internal/catalog/structure.go` | Move item, add Size, add Option, selection rule | 6, 7, 8 |
| `internal/catalog/assignment_sets.go` | Replace-set commands 7, 8, 9 | 9, 10 |
| `internal/catalog/http_display.go` | HTTP for details and image | 4, 5 |
| `internal/catalog/http_structure.go` | HTTP for commands 3–9 | 6–10 |
| `internal/catalog/dto.go` | Request/response/command types | 4–11 |
| `internal/catalog/routes.go` | Handler wiring and routes | 4–10 |
| `internal/catalog/executor.go` | `ExecuteReadWithCapabilities` | 11 |
| `internal/catalog/projections.go` | New projection fields, price gating | 11 |
| `config/config.go`, `cmd/api/main.go`, `web/handler.go`, `.env.example`, `.gitignore` | `MEDIA_DIR`, media route, SPA exclusion | 3, 5 |
| `web/src/features/settings/lib/*`, `web/src/lib/error-messages.ts`, `web/vite.config.ts` | Real card fields, new error text, `/media` proxy | 12 |
| `scripts/dev-seed.ts`, `spec/decisions.md`, backlog docs, `ROADMAP.md` | Seed and documentation | 13 |

---

### Task 1: Migration and SQL column plumbing

**Files:**
- Create: `internal/database/migrations/000016_add_catalog_display_fields.sql`
- Modify: `sql/queries/catalog.sql` (every query returning full `menu_items` or `menu_categories` rows; `ListMenuCategories` order)
- Regenerate: `internal/database/sqlc/*` via `make sqlc`
- Test: `internal/catalog/schema_display_integration_test.go`

**Interfaces:**
- Produces: `sqlc.MenuItem` gains `Code, NormalizedCode, Badge, Description, ImageKey sql.NullString`; `sqlc.MenuCategory` gains `Icon sql.NullString, DisplayOrder int32`. `ListMenuCategories` orders by `display_order, normalized_name, id`. Unique index name `menu_items_active_code_key`.

- [ ] **Step 1: Write the failing schema test**

Create `internal/catalog/schema_display_integration_test.go`:

```go
//go:build integration

package catalog_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCatalogDisplayFieldConstraints(t *testing.T) {
	db, _ := openExecutorTestDB(t)
	ctx := context.Background()
	cleanCategoryTestTables(t, db)
	catID := createTestCategoryDirect(t, db, "Coffee")
	price := int64(30000)
	itemA := createTestItemDirect(t, db, catID, "Americano", &price, false)
	itemB := createTestItemDirect(t, db, catID, "Bac xiu", &price, false)

	t.Run("badge outside the enum is rejected", func(t *testing.T) {
		_, err := db.ExecContext(ctx, `UPDATE menu_items SET badge = 'FAVORITE' WHERE id = $1`, itemA)
		require.Error(t, err)
	})

	t.Run("code requires its normalized form", func(t *testing.T) {
		_, err := db.ExecContext(ctx, `UPDATE menu_items SET code = 'X', normalized_code = NULL WHERE id = $1`, itemA)
		require.Error(t, err)
	})

	t.Run("active codes are unique", func(t *testing.T) {
		_, err := db.ExecContext(ctx, `UPDATE menu_items SET code = 'CF', normalized_code = 'cf' WHERE id = $1`, itemA)
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, `UPDATE menu_items SET code = 'cf', normalized_code = 'cf' WHERE id = $1`, itemB)
		var pgErr *pgconn.PgError
		require.ErrorAs(t, err, &pgErr)
		assert.Equal(t, "menu_items_active_code_key", pgErr.ConstraintName)
	})

	t.Run("a retired item's code can be reused", func(t *testing.T) {
		retired := createTestItemDirect(t, db, catID, "Old coffee", &price, true)
		_, err := db.ExecContext(ctx, `UPDATE menu_items SET code = 'cf', normalized_code = 'cf' WHERE id = $1`, retired)
		require.NoError(t, err)
	})

	t.Run("description is capped at 300 characters", func(t *testing.T) {
		_, err := db.ExecContext(ctx, `UPDATE menu_items SET description = $2 WHERE id = $1`, itemA, strings.Repeat("a", 301))
		require.Error(t, err)
	})

	t.Run("image key must be a content hash", func(t *testing.T) {
		_, err := db.ExecContext(ctx, `UPDATE menu_items SET image_key = 'x.png' WHERE id = $1`, itemA)
		require.Error(t, err)
		_, err = db.ExecContext(ctx, `UPDATE menu_items SET image_key = $2 WHERE id = $1`, itemA, strings.Repeat("a", 64)+".webp")
		require.NoError(t, err)
	})

	t.Run("category icon and order are checked", func(t *testing.T) {
		_, err := db.ExecContext(ctx, `UPDATE menu_categories SET icon = 'Coffee Cup' WHERE id = $1`, catID)
		require.Error(t, err)
		_, err = db.ExecContext(ctx, `UPDATE menu_categories SET display_order = 10000 WHERE id = $1`, catID)
		require.Error(t, err)
		_, err = db.ExecContext(ctx, `UPDATE menu_categories SET icon = 'cup-soda', display_order = 3 WHERE id = $1`, catID)
		require.NoError(t, err)
	})

	t.Run("categories list by display order, then name", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		zeta := createTestCategoryDirect(t, db, "Zeta")
		alpha := createTestCategoryDirect(t, db, "Alpha")
		_, err := db.ExecContext(ctx,
			`UPDATE menu_categories SET display_order = CASE WHEN id = $1 THEN 1 ELSE 2 END`, zeta)
		require.NoError(t, err)

		cats, err := sqlc.New(db).ListMenuCategories(ctx)
		require.NoError(t, err)
		require.Len(t, cats, 2)
		assert.Equal(t, zeta, cats[0].ID)
		assert.Equal(t, alpha, cats[1].ID)
		assert.Equal(t, int32(1), cats[0].DisplayOrder)
	})
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `make docker-up && go test -tags integration ./internal/catalog/ -run TestCatalogDisplayFieldConstraints -count=1`
Expected: compile FAIL — `cats[0].DisplayOrder undefined`.

- [ ] **Step 3: Write the migration**

Create `internal/database/migrations/000016_add_catalog_display_fields.sql`:

```sql
-- 000016: Menu display fields (BA-1, ADR-058).
--
-- Code, badge, description, image, icon, and display order change how the
-- menu looks, never what is sold or what it costs. They are edited in place
-- and never copied into Committed Item snapshots. Every statement is
-- rerunnable.

ALTER TABLE menu_items
    ADD COLUMN IF NOT EXISTS code TEXT,
    ADD COLUMN IF NOT EXISTS normalized_code TEXT,
    ADD COLUMN IF NOT EXISTS badge TEXT,
    ADD COLUMN IF NOT EXISTS description TEXT,
    ADD COLUMN IF NOT EXISTS image_key TEXT;

ALTER TABLE menu_items DROP CONSTRAINT IF EXISTS menu_items_code_check;
ALTER TABLE menu_items ADD CONSTRAINT menu_items_code_check CHECK (
    (code IS NULL AND normalized_code IS NULL)
    OR (code IS NOT NULL AND normalized_code ~ '^[a-z0-9]{1,12}$')
);

ALTER TABLE menu_items DROP CONSTRAINT IF EXISTS menu_items_badge_check;
ALTER TABLE menu_items ADD CONSTRAINT menu_items_badge_check CHECK (
    badge IS NULL OR badge IN ('BEST_SELLER', 'HOT', 'NEW', 'SIGNATURE', 'CHEF_PICK')
);

ALTER TABLE menu_items DROP CONSTRAINT IF EXISTS menu_items_description_check;
ALTER TABLE menu_items ADD CONSTRAINT menu_items_description_check CHECK (
    description IS NULL OR char_length(description) BETWEEN 1 AND 300
);

ALTER TABLE menu_items DROP CONSTRAINT IF EXISTS menu_items_image_key_check;
ALTER TABLE menu_items ADD CONSTRAINT menu_items_image_key_check CHECK (
    image_key IS NULL OR image_key ~ '^[0-9a-f]{64}\.(jpg|png|webp)$'
);

-- A retired item's code may be reused.
CREATE UNIQUE INDEX IF NOT EXISTS menu_items_active_code_key
    ON menu_items (normalized_code)
    WHERE retired_at IS NULL AND normalized_code IS NOT NULL;

ALTER TABLE menu_categories
    ADD COLUMN IF NOT EXISTS icon TEXT,
    ADD COLUMN IF NOT EXISTS display_order INT NOT NULL DEFAULT 0;

ALTER TABLE menu_categories DROP CONSTRAINT IF EXISTS menu_categories_icon_check;
ALTER TABLE menu_categories ADD CONSTRAINT menu_categories_icon_check CHECK (
    icon IS NULL OR icon ~ '^[a-z0-9-]{1,40}$'
);

ALTER TABLE menu_categories DROP CONSTRAINT IF EXISTS menu_categories_display_order_check;
ALTER TABLE menu_categories ADD CONSTRAINT menu_categories_display_order_check CHECK (
    display_order BETWEEN 0 AND 9999
);

-- Existing categories keep their creation order.
UPDATE menu_categories c
SET display_order = o.rn
FROM (
    SELECT id, row_number() OVER (ORDER BY created_at, id) AS rn
    FROM menu_categories
) o
WHERE c.id = o.id AND c.display_order = 0;
```

- [ ] **Step 4: Append the new columns to every full-row query**

sqlc returns the model type (`sqlc.MenuItem`, `sqlc.MenuCategory`) only when a query selects every column in table order. Appending the new columns keeps every existing caller compiling. Run this from the repo root:

```bash
python - <<'EOF'
import pathlib, re
p = pathlib.Path("sql/queries/catalog.sql")
s = p.read_text(encoding="utf-8")
ITEM = ["CreateMenuItem", "GetMenuItemByID", "GetMenuItemForUpdate", "RenameMenuItem",
        "RepriceMenuItem", "SetMenuItemAvailability", "RetireMenuItem", "ListMenuItemsByCategory",
        "ListMenuItemsByCategoryPaginated", "ListAllMenuItemsPaginated", "ListAllMenuItems"]
CAT = ["CreateMenuCategory", "GetMenuCategoryByID", "GetMenuCategoryForUpdate",
       "RenameMenuCategory", "RetireMenuCategory", "ListMenuCategories"]
blocks = re.split(r"(?m)^(?=-- name: )", s)
out = []
found = set()
for b in blocks:
    m = re.match(r"-- name: (\w+)", b)
    name = m.group(1) if m else None
    found.add(name)
    if name in ITEM:
        n = b.replace("created_at, updated_at",
                      "created_at, updated_at,\n       code, normalized_code, badge, description, image_key", 1)
        assert n != b, name
        b = n
    elif name in CAT:
        n = b.replace("retirement_note, updated_at",
                      "retirement_note, updated_at,\n       icon, display_order", 1)
        assert n != b, name
        b = n
    out.append(b)
missing = (set(ITEM) | set(CAT)) - found
assert not missing, f"queries not found: {missing}"
s = "".join(out)
old = "FROM menu_categories\nORDER BY normalized_name ASC, id ASC;"
assert s.count(old) == 1
s = s.replace(old, "FROM menu_categories\nORDER BY display_order ASC, normalized_name ASC, id ASC;")
p.write_text(s, encoding="utf-8", newline="\n")
EOF
```

Then regenerate: `make sqlc`
Expected: no error; `git diff internal/database/sqlc/models.go` shows the new fields on `MenuItem` and `MenuCategory`; `grep -c "MenuItem, error" internal/database/sqlc/querier.go` is unchanged from before.

- [ ] **Step 5: Run the test and the build**

Run: `go build ./... && go test -tags integration ./internal/catalog/ -run TestCatalogDisplayFieldConstraints -count=1`
Expected: PASS. (The test database template is rebuilt from the embedded migrations.)

- [ ] **Step 6: Run the full catalog suite to confirm nothing regressed**

Run: `go test -tags integration ./internal/catalog/... -count=1`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/database/migrations/000016_add_catalog_display_fields.sql sql/queries/catalog.sql internal/database/sqlc internal/catalog/schema_display_integration_test.go
git commit -m "feat(catalog): add menu display field columns

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: Pure display validators and id-set helpers

**Files:**
- Create: `internal/catalog/domain_display.go`
- Test: `internal/catalog/domain_display_test.go`

**Interfaces:**
- Produces (all in package `catalog`):
  - `const BadgeBestSeller, BadgeHot, BadgeNew, BadgeSignature, BadgeChefPick string`
  - `const MaxDescriptionRunes = 300`, `MaxDisplayOrder = 9999`, `MaxAssignmentIDs = 500`
  - `func NormalizeItemCode(code *string) (display, key sql.NullString, err error)`
  - `func NormalizeBadge(badge *string) (sql.NullString, error)`
  - `func NormalizeDescription(desc *string) (sql.NullString, error)`
  - `func NormalizeCategoryIcon(icon *string) (sql.NullString, error)`
  - `func ValidateDisplayOrder(order int32) error`
  - `func ValidateSelectionRule(minSel, maxSel int32, activeOptions, defaults int) error`
  - `func NormalizeIDSet(ids []uuid.UUID, field string) ([]uuid.UUID, error)` — sorted copy, never nil
  - `func DiffIDSets(current, desired []uuid.UUID) (added, removed []uuid.UUID)` — sorted, never nil
  - `func UnionIDs(sets ...[]uuid.UUID) []uuid.UUID` — sorted, deduplicated, never nil
  - `func ContainsID(ids []uuid.UUID, id uuid.UUID) bool`
  - `func nullStringPtr(ns sql.NullString) *string`

- [ ] **Step 1: Write the failing unit tests**

Create `internal/catalog/domain_display_test.go`:

```go
package catalog_test

import (
	"strings"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/catalog"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func strp(s string) *string { return &s }

func TestNormalizeItemCode(t *testing.T) {
	t.Parallel()
	display, key, err := catalog.NormalizeItemCode(strp("  CFsd "))
	require.NoError(t, err)
	assert.Equal(t, "CFsd", display.String)
	assert.Equal(t, "cfsd", key.String)

	for _, blank := range []*string{nil, strp(""), strp("   ")} {
		display, key, err = catalog.NormalizeItemCode(blank)
		require.NoError(t, err)
		assert.False(t, display.Valid)
		assert.False(t, key.Valid)
	}

	for _, bad := range []string{"cà phê", "ab-cd", "abcdefghijklm"} {
		_, _, err = catalog.NormalizeItemCode(strp(bad))
		assert.Error(t, err, bad)
	}
}

func TestNormalizeBadge(t *testing.T) {
	t.Parallel()
	for _, ok := range []string{"BEST_SELLER", "HOT", "NEW", "SIGNATURE", "CHEF_PICK"} {
		b, err := catalog.NormalizeBadge(strp(ok))
		require.NoError(t, err)
		assert.Equal(t, ok, b.String)
	}
	b, err := catalog.NormalizeBadge(nil)
	require.NoError(t, err)
	assert.False(t, b.Valid)
	_, err = catalog.NormalizeBadge(strp("FAVORITE"))
	assert.Error(t, err)
	_, err = catalog.NormalizeBadge(strp("hot"))
	assert.Error(t, err, "badges are case-sensitive codes")
}

func TestNormalizeDescription(t *testing.T) {
	t.Parallel()
	d, err := catalog.NormalizeDescription(strp("  Phin truyền thống  "))
	require.NoError(t, err)
	assert.Equal(t, "Phin truyền thống", d.String)

	d, err = catalog.NormalizeDescription(strp("   "))
	require.NoError(t, err)
	assert.False(t, d.Valid)

	_, err = catalog.NormalizeDescription(strp(strings.Repeat("ạ", 300)))
	require.NoError(t, err, "300 runes is allowed even when bytes exceed 300")
	_, err = catalog.NormalizeDescription(strp(strings.Repeat("a", 301)))
	assert.Error(t, err)
}

func TestNormalizeCategoryIcon(t *testing.T) {
	t.Parallel()
	i, err := catalog.NormalizeCategoryIcon(strp("cup-soda"))
	require.NoError(t, err)
	assert.Equal(t, "cup-soda", i.String)
	i, err = catalog.NormalizeCategoryIcon(nil)
	require.NoError(t, err)
	assert.False(t, i.Valid)
	_, err = catalog.NormalizeCategoryIcon(strp("Coffee Cup"))
	assert.Error(t, err)
}

func TestValidateDisplayOrder(t *testing.T) {
	t.Parallel()
	assert.NoError(t, catalog.ValidateDisplayOrder(0))
	assert.NoError(t, catalog.ValidateDisplayOrder(9999))
	assert.Error(t, catalog.ValidateDisplayOrder(-1))
	assert.Error(t, catalog.ValidateDisplayOrder(10000))
}

func TestValidateSelectionRule(t *testing.T) {
	t.Parallel()
	assert.NoError(t, catalog.ValidateSelectionRule(0, 3, 3, 0))
	assert.NoError(t, catalog.ValidateSelectionRule(1, 1, 4, 1))
	assert.Error(t, catalog.ValidateSelectionRule(-1, 1, 3, 0), "min below 0")
	assert.Error(t, catalog.ValidateSelectionRule(0, 0, 3, 0), "max below 1")
	assert.Error(t, catalog.ValidateSelectionRule(2, 1, 3, 1), "min above max")
	assert.Error(t, catalog.ValidateSelectionRule(0, 4, 3, 0), "max above active options")
	assert.Error(t, catalog.ValidateSelectionRule(1, 2, 3, 0), "defaults below min")
	assert.Error(t, catalog.ValidateSelectionRule(0, 1, 3, 2), "defaults above max")
}

func TestIDSetHelpers(t *testing.T) {
	t.Parallel()
	a, b, c := uuid.MustParse("00000000-0000-0000-0000-00000000000a"),
		uuid.MustParse("00000000-0000-0000-0000-00000000000b"),
		uuid.MustParse("00000000-0000-0000-0000-00000000000c")

	set, err := catalog.NormalizeIDSet([]uuid.UUID{c, a}, "ids")
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{a, c}, set)

	set, err = catalog.NormalizeIDSet(nil, "ids")
	require.NoError(t, err)
	assert.NotNil(t, set)
	assert.Empty(t, set)

	_, err = catalog.NormalizeIDSet([]uuid.UUID{a, a}, "ids")
	assert.Error(t, err, "duplicates")
	_, err = catalog.NormalizeIDSet([]uuid.UUID{uuid.Nil}, "ids")
	assert.Error(t, err, "nil id")
	tooMany := make([]uuid.UUID, catalog.MaxAssignmentIDs+1)
	for i := range tooMany {
		tooMany[i] = uuid.New()
	}
	_, err = catalog.NormalizeIDSet(tooMany, "ids")
	assert.Error(t, err, "over the cap")

	added, removed := catalog.DiffIDSets([]uuid.UUID{a, b}, []uuid.UUID{b, c})
	assert.Equal(t, []uuid.UUID{c}, added)
	assert.Equal(t, []uuid.UUID{a}, removed)
	added, removed = catalog.DiffIDSets(nil, nil)
	assert.NotNil(t, added)
	assert.NotNil(t, removed)

	assert.Equal(t, []uuid.UUID{a, b, c}, catalog.UnionIDs([]uuid.UUID{c, a}, []uuid.UUID{a, b}))
	assert.True(t, catalog.ContainsID([]uuid.UUID{a, b}, b))
	assert.False(t, catalog.ContainsID([]uuid.UUID{a, b}, c))
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/catalog/ -run 'TestNormalize|TestValidateDisplayOrder|TestValidateSelectionRule|TestIDSetHelpers' -count=1`
Expected: compile FAIL — `undefined: catalog.NormalizeItemCode`.

- [ ] **Step 3: Implement**

Create `internal/catalog/domain_display.go`:

```go
package catalog

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Menu Item badges (ADR-058). The web maps each code to its label and color.
const (
	BadgeBestSeller = "BEST_SELLER"
	BadgeHot        = "HOT"
	BadgeNew        = "NEW"
	BadgeSignature  = "SIGNATURE"
	BadgeChefPick   = "CHEF_PICK"
)

// Limits on display fields and replace-set commands.
const (
	MaxDescriptionRunes = 300
	MaxDisplayOrder     = 9999
	MaxAssignmentIDs    = 500
)

var (
	itemCodePattern     = regexp.MustCompile(`^[a-z0-9]{1,12}$`)
	categoryIconPattern = regexp.MustCompile(`^[a-z0-9-]{1,40}$`)
	validBadges         = map[string]bool{
		BadgeBestSeller: true, BadgeHot: true, BadgeNew: true, BadgeSignature: true, BadgeChefPick: true,
	}
)

// NormalizeItemCode trims a Menu Item code and returns its display form and its
// lowercase uniqueness key. A nil or blank code clears it.
func NormalizeItemCode(code *string) (display, key sql.NullString, err error) {
	if code == nil {
		return sql.NullString{}, sql.NullString{}, nil
	}
	d := strings.TrimSpace(*code)
	if d == "" {
		return sql.NullString{}, sql.NullString{}, nil
	}
	k := strings.ToLower(d)
	if !itemCodePattern.MatchString(k) {
		return sql.NullString{}, sql.NullString{}, fmt.Errorf("code %q must be 1 to 12 letters or digits", d)
	}
	return sql.NullString{String: d, Valid: true}, sql.NullString{String: k, Valid: true}, nil
}

// NormalizeBadge accepts nil (no badge) or one of the Badge* codes.
func NormalizeBadge(badge *string) (sql.NullString, error) {
	if badge == nil {
		return sql.NullString{}, nil
	}
	if !validBadges[*badge] {
		return sql.NullString{}, fmt.Errorf("unknown badge %q", *badge)
	}
	return sql.NullString{String: *badge, Valid: true}, nil
}

// NormalizeDescription trims a description; blank clears it.
func NormalizeDescription(desc *string) (sql.NullString, error) {
	if desc == nil {
		return sql.NullString{}, nil
	}
	d := strings.TrimSpace(*desc)
	if d == "" {
		return sql.NullString{}, nil
	}
	if utf8.RuneCountInString(d) > MaxDescriptionRunes {
		return sql.NullString{}, fmt.Errorf("description must be at most %d characters", MaxDescriptionRunes)
	}
	return sql.NullString{String: d, Valid: true}, nil
}

// NormalizeCategoryIcon accepts nil (no icon) or a Lucide icon name.
func NormalizeCategoryIcon(icon *string) (sql.NullString, error) {
	if icon == nil {
		return sql.NullString{}, nil
	}
	if !categoryIconPattern.MatchString(*icon) {
		return sql.NullString{}, fmt.Errorf("icon %q must match %s", *icon, categoryIconPattern)
	}
	return sql.NullString{String: *icon, Valid: true}, nil
}

// ValidateDisplayOrder checks a category display order.
func ValidateDisplayOrder(order int32) error {
	if order < 0 || order > MaxDisplayOrder {
		return fmt.Errorf("display_order %d is out of range [0, %d]", order, MaxDisplayOrder)
	}
	return nil
}

// ValidateSelectionRule checks a modifier group's bounds against its active
// (non-retired) option count and its default option count.
func ValidateSelectionRule(minSel, maxSel int32, activeOptions, defaults int) error {
	if minSel < 0 || maxSel < 1 || minSel > maxSel {
		return fmt.Errorf("invalid min/max selections bounds (%d, %d)", minSel, maxSel)
	}
	if int(maxSel) > activeOptions {
		return fmt.Errorf("max selections %d exceeds active option count %d", maxSel, activeOptions)
	}
	if defaults < int(minSel) || defaults > int(maxSel) {
		return fmt.Errorf("default options count %d must be between min %d and max %d", defaults, minSel, maxSel)
	}
	return nil
}

// NormalizeIDSet validates a replace-set list and returns a sorted copy. The
// sort makes the same set fingerprint alike in any order.
func NormalizeIDSet(ids []uuid.UUID, field string) ([]uuid.UUID, error) {
	if len(ids) > MaxAssignmentIDs {
		return nil, fmt.Errorf("%s must hold at most %d ids", field, MaxAssignmentIDs)
	}
	seen := make(map[uuid.UUID]struct{}, len(ids))
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id == uuid.Nil {
			return nil, fmt.Errorf("%s contains an empty id", field)
		}
		if _, dup := seen[id]; dup {
			return nil, fmt.Errorf("%s contains %s twice", field, id)
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sortUUIDs(out)
	return out, nil
}

// DiffIDSets returns what desired adds to and removes from current.
func DiffIDSets(current, desired []uuid.UUID) (added, removed []uuid.UUID) {
	added, removed = []uuid.UUID{}, []uuid.UUID{}
	for _, id := range desired {
		if !ContainsID(current, id) {
			added = append(added, id)
		}
	}
	for _, id := range current {
		if !ContainsID(desired, id) {
			removed = append(removed, id)
		}
	}
	sortUUIDs(added)
	sortUUIDs(removed)
	return added, removed
}

// UnionIDs merges sets into one sorted, deduplicated list.
func UnionIDs(sets ...[]uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]struct{}{}
	out := []uuid.UUID{}
	for _, set := range sets {
		for _, id := range set {
			if _, dup := seen[id]; !dup {
				seen[id] = struct{}{}
				out = append(out, id)
			}
		}
	}
	sortUUIDs(out)
	return out
}

// ContainsID reports whether id is in ids.
func ContainsID(ids []uuid.UUID, id uuid.UUID) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

func nullStringPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	s := ns.String
	return &s
}
```

`sortUUIDs` already exists in `projections.go:697`; reuse it rather than adding a second sort helper.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/catalog/ -run 'TestNormalize|TestValidateDisplayOrder|TestValidateSelectionRule|TestIDSetHelpers' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/catalog/domain_display.go internal/catalog/domain_display_test.go
git commit -m "feat(catalog): add display field validators and id-set helpers

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: Media store, `MEDIA_DIR`, and the `/media` route

**Files:**
- Create: `internal/catalog/media.go`
- Test: `internal/catalog/media_test.go`
- Modify: `internal/catalog/errors.go` (sentinels live in `media.go`; add HTTP mapping here), `internal/catalog/errors_test.go`
- Modify: `config/config.go`, `config/config_test.go`, `cmd/api/main.go`, `web/handler.go`, `web/handler_test.go`, `.env.example`, `.gitignore`

**Interfaces:**
- Produces:
  - `const MaxImageBytes = 1 << 20`, `MaxImageUploadBody = MaxImageBytes + 64<<10`, `MediaURLPrefix = "/media/catalog/"`
  - `var ErrInvalidImage, ErrImageTooLarge error`
  - `type StoredImage struct { Key, SHA256 string }`
  - `func NewMediaStore(root string) (*MediaStore, error)` — creates `root/catalog`
  - `func (m *MediaStore) Save(data []byte) (StoredImage, error)`
  - `func (m *MediaStore) RegisterRoutes(e *echo.Echo)` — `GET /media/catalog/:key`
  - `func ImageURL(key sql.NullString) *string`
  - `config.Config.MediaDir string` (env `MEDIA_DIR`, default `./data/media`)

- [ ] **Step 1: Write the failing unit tests**

Create `internal/catalog/media_test.go`:

```go
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
```

Add to `internal/catalog/errors_test.go` inside the `tests` table of `TestMapHTTPError`:

```go
		{
			name:       "ErrInvalidImage",
			err:        catalog.ErrInvalidImage,
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_IMAGE",
		},
		{
			name:       "ErrImageTooLarge",
			err:        catalog.ErrImageTooLarge,
			wantStatus: http.StatusRequestEntityTooLarge,
			wantCode:   "IMAGE_TOO_LARGE",
		},
```

Add to `web/handler_test.go`:

```go
func TestSPAHandlerLeavesMediaAlone(t *testing.T) {
	e := setupTestEcho(t, fstest.MapFS{"index.html": {Data: []byte("<html>spa</html>")}})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/media/catalog/missing", nil))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.NotContains(t, rec.Body.String(), "spa")
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/catalog/ ./web/ -run 'TestMediaStore|TestImageURL|TestMediaRoute|TestMapHTTPError|TestSPAHandlerLeavesMediaAlone' -count=1`
Expected: compile FAIL — `undefined: catalog.NewMediaStore`. (`TestSPAHandlerLeavesMediaAlone` fails once the package compiles: extensionless paths fall back to `index.html` today.)

- [ ] **Step 3: Implement `media.go`**

Create `internal/catalog/media.go`:

```go
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
```

In `internal/catalog/errors.go`, add these cases to `MapHTTPError` just before `case errors.Is(err, response.ErrInvalid):`:

```go
	case errors.Is(err, ErrInvalidImage):
		return response.NewCodedError(http.StatusBadRequest, "INVALID_IMAGE", err.Error(), err)
	case errors.Is(err, ErrImageTooLarge):
		return response.NewCodedError(http.StatusRequestEntityTooLarge, "IMAGE_TOO_LARGE", err.Error(), err)
```

- [ ] **Step 4: Exclude `/media` from the SPA fallback**

In `web/handler.go`, extend the guard at the top of `handler`:

```go
		// Never handle API, Swagger, media, or health check routes in SPA handler
		if strings.HasPrefix(reqPath, "/api") ||
			strings.HasPrefix(reqPath, "/swagger") ||
			strings.HasPrefix(reqPath, "/media") ||
			reqPath == "/health" {
			return echo.ErrNotFound
		}
```

- [ ] **Step 5: Add `MEDIA_DIR` and mount the route**

In `config/config.go`: add field `MediaDir string` to `Config` (after `DatabaseURL`), and in `Load()` set `MediaDir: getEnv("MEDIA_DIR", "./data/media"),`. In `Validate()`, add:

```go
	if strings.TrimSpace(c.MediaDir) == "" {
		return fmt.Errorf("MEDIA_DIR must not be empty")
	}
```

Add to `config/config_test.go` (match the file's existing style for building a valid `Config`):

```go
func TestValidateRejectsEmptyMediaDir(t *testing.T) {
	cfg := validTestConfig()
	cfg.MediaDir = " "
	require.Error(t, cfg.Validate())
}
```

If `config_test.go` has no `validTestConfig` helper, build the struct inline with the same values the neighboring tests use plus `MediaDir: "./data/media"`, and add `MediaDir: "./data/media"` to every existing valid-config literal in that file so they keep passing.

In `cmd/api/main.go`, just before `// 6. Register Vertical Slices`:

```go
	// Catalog images (ADR-057): content-addressed files served without auth.
	mediaStore, err := catalog.NewMediaStore(cfg.MediaDir)
	if err != nil {
		return fmt.Errorf("init media store: %w", err)
	}
	mediaStore.RegisterRoutes(e)
```

(If `err` is not yet declared at that point in `run`, use `mediaStore, mediaErr := …` instead.)

Append to `.env.example`:

```
# Catalog images (content-addressed files). Back this directory up with the database.
MEDIA_DIR=./data/media
```

Append `/data/` to `.gitignore`.

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/catalog/ ./web/ ./config/ -count=1 && go build ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/catalog/media.go internal/catalog/media_test.go internal/catalog/errors.go internal/catalog/errors_test.go config cmd/api/main.go web/handler.go web/handler_test.go .env.example .gitignore
git commit -m "feat(catalog): store and serve catalog images under MEDIA_DIR

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: Item and category details commands (commands 1 and 2)

**Files:**
- Modify: `sql/queries/catalog.sql` (3 queries), then `make sqlc`
- Modify: `internal/catalog/domain.go` (all new `Op*` and `Event*` constants for the whole plan)
- Modify: `internal/catalog/errors.go`, `internal/catalog/errors_test.go` (`ErrCodeConflict`)
- Modify: `internal/catalog/dto.go`
- Create: `internal/catalog/display.go`, `internal/catalog/http_display.go`
- Modify: `internal/catalog/routes.go`
- Test: `internal/catalog/display_integration_test.go`, `internal/catalog/routes_display_integration_test.go`

**Interfaces:**
- Consumes: `NormalizeItemCode`, `NormalizeBadge`, `NormalizeDescription`, `NormalizeCategoryIcon`, `ValidateDisplayOrder`, `nullStringPtr` (Task 2).
- Produces:
  - `var ErrCodeConflict error` → `409 CATALOG_CODE_CONFLICT`
  - `type SetItemDetailsCommand struct { RequestID, ItemID uuid.UUID; Code, Badge, Description *string }`
  - `type ItemDetailsResponse struct { ItemID uuid.UUID; Code, Badge, Description *string }`
  - `func NewSetItemDetailsHandler(r *Runner) *SetItemDetailsHandler`; `Handle(ctx, Actor, SetItemDetailsCommand) (int, ItemDetailsResponse, error)`
  - `type SetCategoryDetailsCommand struct { RequestID, CategoryID uuid.UUID; Icon *string; DisplayOrder int32 }`
  - `type CategoryDetailsResponse struct { CategoryID uuid.UUID; Icon *string; DisplayOrder int32 }`
  - `func NewSetCategoryDetailsHandler(r *Runner) *SetCategoryDetailsHandler`
  - `type PresentString struct { Present bool; Value *string }` (JSON key presence)
  - Test helpers (package `catalog_test`): `managerActor(t, db, q) catalog.Actor`, `cashierActor(t, db, q) catalog.Actor`, `retireCategoryDirect(t, db, id)`, `auditCount(t, db, eventType) int`, `strPtr(s) *string`

- [ ] **Step 1: Add every new operation and event constant**

In `internal/catalog/domain.go`, append inside the `Op*` const block:

```go
	OpItemSetDetails                    = "catalog.item.set_details"
	OpCategorySetDetails                = "catalog.category.set_details"
	OpItemSetImage                      = "catalog.item.set_image"
	OpItemClearImage                    = "catalog.item.clear_image"
	OpItemMoveCategory                  = "catalog.item.move_category"
	OpSizeCreate                        = "catalog.size.create"
	OpModifierOptionCreate              = "catalog.modifier_option.create"
	OpModifierGroupSetSelectionRule     = "catalog.modifier_group.set_selection_rule"
	OpItemReplaceModifierGroups         = "catalog.item.replace_modifier_groups"
	OpCategoryReplaceModifierGroups     = "catalog.category.replace_modifier_groups"
	OpModifierGroupReplaceAssignments   = "catalog.modifier_group.replace_assignments"
```

and inside the `Event*` const block (before `EventAuthorizationDenied`):

```go
	EventItemDetailsChanged                 = "catalog.item.details_changed"
	EventCategoryDetailsChanged             = "catalog.category.details_changed"
	EventItemImageSet                       = "catalog.item.image_set"
	EventItemImageCleared                   = "catalog.item.image_cleared"
	EventItemCategoryChanged                = "catalog.item.category_changed"
	EventSizeCreated                        = "catalog.size.created"
	EventModifierOptionCreated              = "catalog.modifier_option.created"
	EventModifierGroupSelectionRuleChanged  = "catalog.modifier_group.selection_rule_changed"
	EventItemModifierGroupsReplaced         = "catalog.item.modifier_groups_replaced"
	EventCategoryModifierGroupsReplaced     = "catalog.category.modifier_groups_replaced"
	EventModifierGroupAssignmentsReplaced   = "catalog.modifier_group.assignments_replaced"
```

Run `gofmt -w internal/catalog/domain.go` to realign.

- [ ] **Step 2: Add the queries and regenerate**

Append to `sql/queries/catalog.sql`:

```sql
-- -- Display Details (BA-1) --

-- name: GetActiveMenuItemIDByCode :one
SELECT id
FROM menu_items
WHERE normalized_code = sqlc.arg(normalized_code)
  AND retired_at IS NULL
  AND id <> sqlc.arg(exclude_id);

-- name: UpdateMenuItemDetails :one
UPDATE menu_items
SET code = $2, normalized_code = $3, badge = $4, description = $5, updated_at = now()
WHERE id = $1
RETURNING id, category_id, name, normalized_name, price_vnd,
          available, retired_at, retirement_reason, retirement_note,
          created_at, updated_at,
          code, normalized_code, badge, description, image_key;

-- name: UpdateMenuCategoryDetails :one
UPDATE menu_categories
SET icon = $2, display_order = $3, updated_at = now()
WHERE id = $1
RETURNING id, name, normalized_name, created_at,
          retired_at, retirement_reason, retirement_note, updated_at,
          icon, display_order;
```

Run: `make sqlc`
Expected: `GetActiveMenuItemIDByCodeParams{NormalizedCode sql.NullString, ExcludeID uuid.UUID}`, `UpdateMenuItemDetailsParams{ID, Code, NormalizedCode, Badge, Description}`, `UpdateMenuCategoryDetailsParams{ID, Icon sql.NullString, DisplayOrder int32}` exist in `internal/database/sqlc/catalog.sql.go`.

- [ ] **Step 3: Write the failing command tests**

Create `internal/catalog/display_integration_test.go`:

```go
//go:build integration

package catalog_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/catalog"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testManagerPIN = "123456"

func strPtr(s string) *string { return &s }

func managerActor(t *testing.T, db *sql.DB, q *sqlc.Queries) catalog.Actor {
	t.Helper()
	ident := createCatalogTestIdentity(t, db, q, []string{auth.RoleManager}, true, testManagerPIN)
	return catalog.Actor{StaffID: ident.StaffID, SessionID: ident.SessionID}
}

func cashierActor(t *testing.T, db *sql.DB, q *sqlc.Queries) catalog.Actor {
	t.Helper()
	ident := createCatalogTestIdentity(t, db, q, []string{auth.RoleCashier}, true, "")
	return catalog.Actor{StaffID: ident.StaffID, SessionID: ident.SessionID}
}

func retireCategoryDirect(t *testing.T, db *sql.DB, id uuid.UUID) {
	t.Helper()
	_, err := db.Exec(`UPDATE menu_categories SET retired_at = now(), retirement_reason = 'NO_LONGER_OFFERED' WHERE id = $1`, id)
	require.NoError(t, err)
}

func auditCount(t *testing.T, db *sql.DB, eventType string) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM audit_events WHERE event_type = $1`, eventType).Scan(&n))
	return n
}

func TestSetItemDetails(t *testing.T) {
	db, q := openExecutorTestDB(t)
	handler := catalog.NewSetItemDetailsHandler(catalog.NewRunner(db, q))
	ctx := context.Background()
	price := int64(29000)

	t.Run("sets then clears the three fields, one audit event each", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		actor := managerActor(t, db, q)
		cat := createTestCategoryDirect(t, db, "Coffee")
		item := createTestItemDirect(t, db, cat, "Ca phe sua da", &price, false)

		status, res, err := handler.Handle(ctx, actor, catalog.SetItemDetailsCommand{
			RequestID: uuid.New(), ItemID: item,
			Code: strPtr("  CFSD "), Badge: strPtr(catalog.BadgeBestSeller), Description: strPtr("  Phin truyền thống "),
		})
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		require.NotNil(t, res.Code)
		assert.Equal(t, "CFSD", *res.Code)
		assert.Equal(t, catalog.BadgeBestSeller, *res.Badge)
		assert.Equal(t, "Phin truyền thống", *res.Description)

		var key string
		require.NoError(t, db.QueryRow(`SELECT normalized_code FROM menu_items WHERE id = $1`, item).Scan(&key))
		assert.Equal(t, "cfsd", key)

		_, res, err = handler.Handle(ctx, actor, catalog.SetItemDetailsCommand{RequestID: uuid.New(), ItemID: item})
		require.NoError(t, err)
		assert.Nil(t, res.Code)
		assert.Nil(t, res.Badge)
		assert.Nil(t, res.Description)
		assert.Equal(t, 2, auditCount(t, db, catalog.EventItemDetailsChanged))
	})

	t.Run("a code used by another active item is ErrCodeConflict", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		actor := managerActor(t, db, q)
		cat := createTestCategoryDirect(t, db, "Coffee")
		a := createTestItemDirect(t, db, cat, "A", &price, false)
		b := createTestItemDirect(t, db, cat, "B", &price, false)
		_, _, err := handler.Handle(ctx, actor, catalog.SetItemDetailsCommand{RequestID: uuid.New(), ItemID: a, Code: strPtr("cf")})
		require.NoError(t, err)
		_, _, err = handler.Handle(ctx, actor, catalog.SetItemDetailsCommand{RequestID: uuid.New(), ItemID: b, Code: strPtr("CF")})
		assert.True(t, errors.Is(err, catalog.ErrCodeConflict), "got %v", err)

		_, _, err = handler.Handle(ctx, actor, catalog.SetItemDetailsCommand{RequestID: uuid.New(), ItemID: a, Code: strPtr("cf"), Badge: strPtr(catalog.BadgeHot)})
		require.NoError(t, err, "an item may keep its own code")
	})

	t.Run("a retired item's code can be reused", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		actor := managerActor(t, db, q)
		cat := createTestCategoryDirect(t, db, "Coffee")
		old := createTestItemDirect(t, db, cat, "Old", &price, true)
		_, err := db.Exec(`UPDATE menu_items SET code = 'cf', normalized_code = 'cf' WHERE id = $1`, old)
		require.NoError(t, err)
		item := createTestItemDirect(t, db, cat, "New", &price, false)
		_, _, err = handler.Handle(ctx, actor, catalog.SetItemDetailsCommand{RequestID: uuid.New(), ItemID: item, Code: strPtr("cf")})
		require.NoError(t, err)
	})

	t.Run("invalid values are INVALID_INPUT", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		actor := managerActor(t, db, q)
		cat := createTestCategoryDirect(t, db, "Coffee")
		item := createTestItemDirect(t, db, cat, "A", &price, false)
		for name, cmd := range map[string]catalog.SetItemDetailsCommand{
			"code with accents": {Code: strPtr("cà phê")},
			"code too long":     {Code: strPtr("abcdefghijklm")},
			"unknown badge":     {Badge: strPtr("FAVORITE")},
			"description 301":   {Description: strPtr(strings.Repeat("a", 301))},
		} {
			cmd.RequestID, cmd.ItemID = uuid.New(), item
			_, _, err := handler.Handle(ctx, actor, cmd)
			assert.True(t, errors.Is(err, response.ErrInvalid), "%s: got %v", name, err)
		}
	})

	t.Run("retired item is ErrEntityRetired", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		actor := managerActor(t, db, q)
		cat := createTestCategoryDirect(t, db, "Coffee")
		item := createTestItemDirect(t, db, cat, "A", &price, true)
		_, _, err := handler.Handle(ctx, actor, catalog.SetItemDetailsCommand{RequestID: uuid.New(), ItemID: item, Badge: strPtr(catalog.BadgeNew)})
		assert.True(t, errors.Is(err, catalog.ErrEntityRetired))
	})

	t.Run("cashier is forbidden", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		cat := createTestCategoryDirect(t, db, "Coffee")
		item := createTestItemDirect(t, db, cat, "A", &price, false)
		_, _, err := handler.Handle(ctx, cashierActor(t, db, q), catalog.SetItemDetailsCommand{RequestID: uuid.New(), ItemID: item})
		assert.True(t, errors.Is(err, catalog.ErrForbidden))
	})

	t.Run("replay returns the stored result; a changed body conflicts", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		actor := managerActor(t, db, q)
		cat := createTestCategoryDirect(t, db, "Coffee")
		item := createTestItemDirect(t, db, cat, "A", &price, false)
		cmd := catalog.SetItemDetailsCommand{RequestID: uuid.New(), ItemID: item, Code: strPtr("aa")}
		_, first, err := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		_, second, err := handler.Handle(ctx, actor, cmd)
		require.NoError(t, err)
		assert.Equal(t, first, second)
		assert.Equal(t, 1, auditCount(t, db, catalog.EventItemDetailsChanged))

		cmd.Code = strPtr("bb")
		_, _, err = handler.Handle(ctx, actor, cmd)
		assert.True(t, errors.Is(err, catalog.ErrRequestConflict))
	})
}

func TestSetCategoryDetails(t *testing.T) {
	db, q := openExecutorTestDB(t)
	handler := catalog.NewSetCategoryDetailsHandler(catalog.NewRunner(db, q))
	ctx := context.Background()

	t.Run("sets icon and order with an audit event", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		cat := createTestCategoryDirect(t, db, "Coffee")
		status, res, err := handler.Handle(ctx, managerActor(t, db, q), catalog.SetCategoryDetailsCommand{
			RequestID: uuid.New(), CategoryID: cat, Icon: strPtr("coffee"), DisplayOrder: 3,
		})
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.Equal(t, "coffee", *res.Icon)
		assert.Equal(t, int32(3), res.DisplayOrder)
		assert.Equal(t, 1, auditCount(t, db, catalog.EventCategoryDetailsChanged))
	})

	t.Run("invalid icon and order are INVALID_INPUT", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		actor := managerActor(t, db, q)
		cat := createTestCategoryDirect(t, db, "Coffee")
		_, _, err := handler.Handle(ctx, actor, catalog.SetCategoryDetailsCommand{RequestID: uuid.New(), CategoryID: cat, Icon: strPtr("Coffee Cup")})
		assert.True(t, errors.Is(err, response.ErrInvalid))
		_, _, err = handler.Handle(ctx, actor, catalog.SetCategoryDetailsCommand{RequestID: uuid.New(), CategoryID: cat, DisplayOrder: 10000})
		assert.True(t, errors.Is(err, response.ErrInvalid))
	})

	t.Run("retired category is ErrEntityRetired", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		cat := createTestCategoryDirect(t, db, "Coffee")
		retireCategoryDirect(t, db, cat)
		_, _, err := handler.Handle(ctx, managerActor(t, db, q), catalog.SetCategoryDetailsCommand{RequestID: uuid.New(), CategoryID: cat})
		assert.True(t, errors.Is(err, catalog.ErrEntityRetired))
	})
}
```

Add to `errors_test.go`: in `TestMapDBError`,

```go
	// The active-code unique index maps to ErrCodeConflict, not ErrNameConflict
	err = catalog.MapDBError(&pgconn.PgError{Code: "23505", ConstraintName: "menu_items_active_code_key"})
	assert.True(t, errors.Is(err, catalog.ErrCodeConflict))
	assert.False(t, errors.Is(err, catalog.ErrNameConflict))
```

and in the `TestMapHTTPError` table:

```go
		{
			name:       "ErrCodeConflict",
			err:        catalog.ErrCodeConflict,
			wantStatus: http.StatusConflict,
			wantCode:   "CATALOG_CODE_CONFLICT",
		},
```

- [ ] **Step 4: Run to verify failure**

Run: `go test -tags integration ./internal/catalog/ -run 'TestSetItemDetails|TestSetCategoryDetails|TestMapDBError|TestMapHTTPError' -count=1`
Expected: compile FAIL — `undefined: catalog.NewSetItemDetailsHandler`.

- [ ] **Step 5: Add `ErrCodeConflict`**

In `internal/catalog/errors.go`: add `ErrCodeConflict = errors.New("catalog code conflict")` to the `var` block. In `MapDBError`, at the top of `case "23505":`

```go
		case "23505": // unique_violation
			if pgErr.ConstraintName == "menu_items_active_code_key" {
				return fmt.Errorf("%w: %s", ErrCodeConflict, pgErr.Detail)
			}
```

In `MapHTTPError`, add before the `ErrNameConflict` case:

```go
	case errors.Is(err, ErrCodeConflict):
		return response.NewCodedError(http.StatusConflict, "CATALOG_CODE_CONFLICT", err.Error(), err)
```

- [ ] **Step 6: Add the DTOs**

Append to `internal/catalog/dto.go` (add `"encoding/json"` to its imports):

```go
// === Display Details (BA-1) ===

// PresentString records whether a JSON key was present, so a missing key and
// an explicit null are told apart.
type PresentString struct {
	Present bool
	Value   *string
}

// UnmarshalJSON marks the key present; null leaves Value nil.
func (p *PresentString) UnmarshalJSON(b []byte) error {
	p.Present = true
	if string(b) == "null" {
		p.Value = nil
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	p.Value = &s
	return nil
}

// SetItemDetailsRequest replaces all three display fields. Every key must be
// present; null clears.
type SetItemDetailsRequest struct {
	RequestID   uuid.UUID     `json:"request_id"`
	Code        PresentString `json:"code" swaggertype:"string" extensions:"x-nullable"`
	Badge       PresentString `json:"badge" swaggertype:"string" enums:"BEST_SELLER,HOT,NEW,SIGNATURE,CHEF_PICK" extensions:"x-nullable"`
	Description PresentString `json:"description" swaggertype:"string" extensions:"x-nullable"`
}

// SetItemDetailsCommand carries the parameters for setting item display fields.
type SetItemDetailsCommand struct {
	RequestID   uuid.UUID
	ItemID      uuid.UUID
	Code        *string
	Badge       *string
	Description *string
}

// ItemDetailsResponse is an item's display fields.
type ItemDetailsResponse struct {
	ItemID      uuid.UUID `json:"item_id"`
	Code        *string   `json:"code"`
	Badge       *string   `json:"badge"`
	Description *string   `json:"description"`
}

// SetCategoryDetailsRequest sets a category's icon and display order.
type SetCategoryDetailsRequest struct {
	RequestID    uuid.UUID `json:"request_id"`
	Icon         *string   `json:"icon"`
	DisplayOrder int32     `json:"display_order"`
}

// SetCategoryDetailsCommand carries the parameters for setting category display fields.
type SetCategoryDetailsCommand struct {
	RequestID    uuid.UUID
	CategoryID   uuid.UUID
	Icon         *string
	DisplayOrder int32
}

// CategoryDetailsResponse is a category's display fields.
type CategoryDetailsResponse struct {
	CategoryID   uuid.UUID `json:"category_id"`
	Icon         *string   `json:"icon"`
	DisplayOrder int32     `json:"display_order"`
}
```

- [ ] **Step 7: Implement the commands**

Create `internal/catalog/display.go`:

```go
package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
)

type itemDetailsFingerprint struct {
	ItemID      uuid.UUID `json:"item_id"`
	Code        *string   `json:"code"`
	Badge       *string   `json:"badge"`
	Description *string   `json:"description"`
}

type itemDetailsValues struct {
	Code        *string `json:"code"`
	Badge       *string `json:"badge"`
	Description *string `json:"description"`
}

type itemDetailsChangedAudit struct {
	ItemID uuid.UUID         `json:"item_id"`
	Before itemDetailsValues `json:"before"`
	After  itemDetailsValues `json:"after"`
}

// SetItemDetailsHandler sets a Menu Item's code, badge, and description (ADR-058).
type SetItemDetailsHandler struct {
	runner *Runner
}

// NewSetItemDetailsHandler creates a SetItemDetailsHandler.
func NewSetItemDetailsHandler(runner *Runner) *SetItemDetailsHandler {
	return &SetItemDetailsHandler{runner: runner}
}

// Handle replaces all three display fields.
func (h *SetItemDetailsHandler) Handle(ctx context.Context, actor Actor, cmd SetItemDetailsCommand) (int, ItemDetailsResponse, error) {
	code, codeKey, codeErr := NormalizeItemCode(cmd.Code)
	badge, badgeErr := NormalizeBadge(cmd.Badge)
	desc, descErr := NormalizeDescription(cmd.Description)
	if err := errors.Join(codeErr, badgeErr, descErr); err != nil {
		return 0, ItemDetailsResponse{}, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error())
	}

	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpItemSetDetails,
		Fingerprint: itemDetailsFingerprint{
			ItemID: cmd.ItemID, Code: nullStringPtr(code), Badge: nullStringPtr(badge), Description: nullStringPtr(desc),
		},
		Required: []string{CapAdministerStructure},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec, func(q *sqlc.Queries) (int, ItemDetailsResponse, AuditRecord, error) {
		existing, err := q.GetMenuItemForUpdate(ctx, cmd.ItemID)
		if err != nil {
			return 0, ItemDetailsResponse{}, AuditRecord{}, MapDBError(err)
		}
		if existing.RetiredAt.Valid {
			return 0, ItemDetailsResponse{}, AuditRecord{}, fmt.Errorf("%w: item is retired", ErrEntityRetired)
		}

		if codeKey.Valid {
			_, err := q.GetActiveMenuItemIDByCode(ctx, sqlc.GetActiveMenuItemIDByCodeParams{
				NormalizedCode: codeKey, ExcludeID: cmd.ItemID,
			})
			if err == nil {
				return 0, ItemDetailsResponse{}, AuditRecord{}, fmt.Errorf("%w: code %q is used by another item", ErrCodeConflict, code.String)
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return 0, ItemDetailsResponse{}, AuditRecord{}, MapDBError(err)
			}
		}

		item, err := q.UpdateMenuItemDetails(ctx, sqlc.UpdateMenuItemDetailsParams{
			ID: cmd.ItemID, Code: code, NormalizedCode: codeKey, Badge: badge, Description: desc,
		})
		if err != nil {
			return 0, ItemDetailsResponse{}, AuditRecord{}, MapDBError(err)
		}

		res := ItemDetailsResponse{
			ItemID: item.ID, Code: nullStringPtr(item.Code), Badge: nullStringPtr(item.Badge), Description: nullStringPtr(item.Description),
		}
		audit := AuditRecord{
			EventType: EventItemDetailsChanged,
			Details: itemDetailsChangedAudit{
				ItemID: item.ID,
				Before: itemDetailsValues{
					Code: nullStringPtr(existing.Code), Badge: nullStringPtr(existing.Badge), Description: nullStringPtr(existing.Description),
				},
				After: itemDetailsValues{Code: res.Code, Badge: res.Badge, Description: res.Description},
			},
		}
		return 200, res, audit, nil
	})
}

type categoryDetailsFingerprint struct {
	CategoryID   uuid.UUID `json:"category_id"`
	Icon         *string   `json:"icon"`
	DisplayOrder int32     `json:"display_order"`
}

type categoryDetailsValues struct {
	Icon         *string `json:"icon"`
	DisplayOrder int32   `json:"display_order"`
}

type categoryDetailsChangedAudit struct {
	CategoryID uuid.UUID             `json:"category_id"`
	Before     categoryDetailsValues `json:"before"`
	After      categoryDetailsValues `json:"after"`
}

// SetCategoryDetailsHandler sets a Menu Category's icon and display order.
type SetCategoryDetailsHandler struct {
	runner *Runner
}

// NewSetCategoryDetailsHandler creates a SetCategoryDetailsHandler.
func NewSetCategoryDetailsHandler(runner *Runner) *SetCategoryDetailsHandler {
	return &SetCategoryDetailsHandler{runner: runner}
}

// Handle replaces the icon and display order.
func (h *SetCategoryDetailsHandler) Handle(ctx context.Context, actor Actor, cmd SetCategoryDetailsCommand) (int, CategoryDetailsResponse, error) {
	icon, iconErr := NormalizeCategoryIcon(cmd.Icon)
	if err := errors.Join(iconErr, ValidateDisplayOrder(cmd.DisplayOrder)); err != nil {
		return 0, CategoryDetailsResponse{}, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error())
	}

	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpCategorySetDetails,
		Fingerprint: categoryDetailsFingerprint{CategoryID: cmd.CategoryID, Icon: nullStringPtr(icon), DisplayOrder: cmd.DisplayOrder},
		Required:    []string{CapAdministerStructure},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec, func(q *sqlc.Queries) (int, CategoryDetailsResponse, AuditRecord, error) {
		existing, err := q.GetMenuCategoryForUpdate(ctx, cmd.CategoryID)
		if err != nil {
			return 0, CategoryDetailsResponse{}, AuditRecord{}, MapDBError(err)
		}
		if existing.RetiredAt.Valid {
			return 0, CategoryDetailsResponse{}, AuditRecord{}, fmt.Errorf("%w: category is retired", ErrEntityRetired)
		}

		cat, err := q.UpdateMenuCategoryDetails(ctx, sqlc.UpdateMenuCategoryDetailsParams{
			ID: cmd.CategoryID, Icon: icon, DisplayOrder: cmd.DisplayOrder,
		})
		if err != nil {
			return 0, CategoryDetailsResponse{}, AuditRecord{}, MapDBError(err)
		}

		res := CategoryDetailsResponse{CategoryID: cat.ID, Icon: nullStringPtr(cat.Icon), DisplayOrder: cat.DisplayOrder}
		audit := AuditRecord{
			EventType: EventCategoryDetailsChanged,
			Details: categoryDetailsChangedAudit{
				CategoryID: cat.ID,
				Before:     categoryDetailsValues{Icon: nullStringPtr(existing.Icon), DisplayOrder: existing.DisplayOrder},
				After:      categoryDetailsValues{Icon: res.Icon, DisplayOrder: res.DisplayOrder},
			},
		}
		return 200, res, audit, nil
	})
}
```

- [ ] **Step 8: Run the command tests**

Run: `go test -tags integration ./internal/catalog/ -run 'TestSetItemDetails|TestSetCategoryDetails|TestMapDBError|TestMapHTTPError' -count=1`
Expected: PASS.

- [ ] **Step 9: Write the failing route test**

Create `internal/catalog/routes_display_integration_test.go`:

```go
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
```

Run: `go test -tags integration ./internal/catalog/ -run TestDisplayDetailsRoutes -count=1`
Expected: FAIL — 404/405 (routes not registered).

- [ ] **Step 10: Add the HTTP handlers and routes**

Create `internal/catalog/http_display.go`:

```go
package catalog

import (
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/labstack/echo/v4"
)

// handleSetItemDetails godoc
//
//	@Summary		Cập nhật thông tin hiển thị của món
//	@Description	Ghi đè mã món, huy hiệu và mô tả. Cả ba khóa đều bắt buộc; gửi null để xóa. Yêu cầu quyền catalog.administer_structure.
//	@Tags			Catalog
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			item_id	path		string					true	"Item ID (UUID)"
//	@Param			request	body		SetItemDetailsRequest	true	"Thông tin hiển thị"
//	@Success		200		{object}	response.APIResponse{data=ItemDetailsResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Failure		500		{object}	response.APIResponse
//	@Router			/catalog/items/{item_id}/details [patch]
func (s *Slices) handleSetItemDetails(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	itemID, err := parseUUIDParam(c, "item_id")
	if err != nil {
		return sendError(c, err)
	}
	req, err := bindBody[SetItemDetailsRequest](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(req.RequestID); err != nil {
		return sendError(c, err)
	}
	if !req.Code.Present || !req.Badge.Present || !req.Description.Present {
		return sendError(c, fmt.Errorf("%w: code, badge, and description are all required (null clears)", response.ErrInvalid))
	}

	status, res, err := s.SetItemDetails.Handle(c.Request().Context(), actor, SetItemDetailsCommand{
		RequestID: req.RequestID, ItemID: itemID,
		Code: req.Code.Value, Badge: req.Badge.Value, Description: req.Description.Value,
	})
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
}

// handleSetCategoryDetails godoc
//
//	@Summary		Cập nhật biểu tượng và thứ tự danh mục
//	@Description	Đặt biểu tượng (tên icon Lucide) và thứ tự hiển thị (0 đến 9999). Yêu cầu quyền catalog.administer_structure.
//	@Tags			Catalog
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			category_id	path		string						true	"Category ID (UUID)"
//	@Param			request		body		SetCategoryDetailsRequest	true	"Thông tin hiển thị"
//	@Success		200			{object}	response.APIResponse{data=CategoryDetailsResponse}
//	@Failure		400			{object}	response.APIResponse
//	@Failure		401			{object}	response.APIResponse
//	@Failure		403			{object}	response.APIResponse
//	@Failure		404			{object}	response.APIResponse
//	@Failure		409			{object}	response.APIResponse
//	@Failure		500			{object}	response.APIResponse
//	@Router			/catalog/categories/{category_id}/details [patch]
func (s *Slices) handleSetCategoryDetails(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	catID, err := parseUUIDParam(c, "category_id")
	if err != nil {
		return sendError(c, err)
	}
	req, err := bindBody[SetCategoryDetailsRequest](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(req.RequestID); err != nil {
		return sendError(c, err)
	}

	status, res, err := s.SetCategoryDetails.Handle(c.Request().Context(), actor, SetCategoryDetailsCommand{
		RequestID: req.RequestID, CategoryID: catID, Icon: req.Icon, DisplayOrder: req.DisplayOrder,
	})
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
}
```

In `internal/catalog/routes.go`: add fields `SetItemDetails *SetItemDetailsHandler` and `SetCategoryDetails *SetCategoryDetailsHandler` to `Slices`, construct them in `NewSlices` (`NewSetItemDetailsHandler(runner)`, `NewSetCategoryDetailsHandler(runner)`), and add to `RegisterRoutes`:

```go
	// Display details (BA-1)
	catalog.PATCH("/items/:item_id/details", s.handleSetItemDetails, authn.RequireCapability(CapAdministerStructure))
	catalog.PATCH("/categories/:category_id/details", s.handleSetCategoryDetails, authn.RequireCapability(CapAdministerStructure))
```

- [ ] **Step 11: Run all Task 4 tests**

Run: `go test -tags integration ./internal/catalog/ -run 'TestSetItemDetails|TestSetCategoryDetails|TestDisplayDetailsRoutes|TestMapDBError|TestMapHTTPError' -count=1`
Expected: PASS.

- [ ] **Step 12: Commit**

```bash
git add sql/queries/catalog.sql internal/database/sqlc internal/catalog
git commit -m "feat(catalog): item and category display details commands

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: Set and clear the item image

**Files:**
- Modify: `sql/queries/catalog.sql` (1 query), then `make sqlc`
- Modify: `internal/catalog/dto.go`, `internal/catalog/routes.go` (`NewSlices` takes the media store), `internal/catalog/http_display.go`
- Create: `internal/catalog/image.go`
- Modify callers of `catalog.NewSlices`: `cmd/api/main.go`, `internal/catalog/catalog_integration_test.go:67`, `internal/catalog/routes_test.go:49`
- Test: `internal/catalog/image_integration_test.go`, extend `internal/catalog/routes_display_integration_test.go`

**Interfaces:**
- Consumes: `MediaStore.Save`, `StoredImage`, `ImageURL`, `MaxImageBytes`, `MaxImageUploadBody`, `ErrImageTooLarge` (Task 3); `managerActor`, `auditCount` (Task 4).
- Produces:
  - `func NewSlices(db *sql.DB, queries *sqlc.Queries, media *MediaStore) *Slices`
  - `type SetItemImageCommand struct { RequestID, ItemID uuid.UUID; Data []byte }`
  - `type ClearItemImageCommand struct { RequestID, ItemID uuid.UUID }`
  - `type ItemImageResponse struct { ItemID uuid.UUID; ImageURL *string }`
  - `func NewSetItemImageHandler(r *Runner, m *MediaStore) *SetItemImageHandler`, `func NewClearItemImageHandler(r *Runner) *ClearItemImageHandler`
  - Test helper `newTestMediaStore(t) *catalog.MediaStore`

- [ ] **Step 1: Add the query**

Append to `sql/queries/catalog.sql`:

```sql
-- name: SetMenuItemImageKey :one
UPDATE menu_items
SET image_key = $2, updated_at = now()
WHERE id = $1
RETURNING id, category_id, name, normalized_name, price_vnd,
          available, retired_at, retirement_reason, retirement_note,
          created_at, updated_at,
          code, normalized_code, badge, description, image_key;
```

Run: `make sqlc`

- [ ] **Step 2: Write the failing tests**

Create `internal/catalog/image_integration_test.go`:

```go
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
```

Note: `tinyPNG` and `tinyWebP` live in `media_test.go`, which has no build tag and is in the same `catalog_test` package, so integration builds see them.

Append to `TestDisplayDetailsRoutes` in `routes_display_integration_test.go` (add imports `"bytes"`, `"mime/multipart"`, `"net/http/httptest"`):

```go
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
```

- [ ] **Step 3: Run to verify failure**

Run: `go test -tags integration ./internal/catalog/ -run 'TestItemImageCommands|TestDisplayDetailsRoutes' -count=1`
Expected: compile FAIL — `undefined: catalog.NewSetItemImageHandler`.

- [ ] **Step 4: Add DTOs and implement the commands**

Append to `internal/catalog/dto.go`:

```go
// SetItemImageCommand carries the uploaded bytes for an item image.
type SetItemImageCommand struct {
	RequestID uuid.UUID
	ItemID    uuid.UUID
	Data      []byte
}

// ClearItemImageCommand removes an item's image.
type ClearItemImageCommand struct {
	RequestID uuid.UUID
	ItemID    uuid.UUID
}

// ItemImageResponse is an item's image URL, or null.
type ItemImageResponse struct {
	ItemID   uuid.UUID `json:"item_id"`
	ImageURL *string   `json:"image_url"`
}
```

Create `internal/catalog/image.go`:

```go
package catalog

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

type setItemImageFingerprint struct {
	ItemID uuid.UUID `json:"item_id"`
	SHA256 string    `json:"sha256"`
}

type clearItemImageFingerprint struct {
	ItemID uuid.UUID `json:"item_id"`
}

type itemImageAudit struct {
	ItemID           uuid.UUID `json:"item_id"`
	ImageKey         *string   `json:"image_key"`
	PreviousImageKey *string   `json:"previous_image_key"`
}

// SetItemImageHandler stores an uploaded image and points the item at it (ADR-057).
type SetItemImageHandler struct {
	runner *Runner
	media  *MediaStore
}

// NewSetItemImageHandler creates a SetItemImageHandler.
func NewSetItemImageHandler(runner *Runner, media *MediaStore) *SetItemImageHandler {
	return &SetItemImageHandler{runner: runner, media: media}
}

// Handle writes the file before the transaction, so a rollback leaves an
// unreferenced file and never a reference to a missing one.
func (h *SetItemImageHandler) Handle(ctx context.Context, actor Actor, cmd SetItemImageCommand) (int, ItemImageResponse, error) {
	stored, err := h.media.Save(cmd.Data)
	if err != nil {
		return 0, ItemImageResponse{}, err
	}
	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpItemSetImage,
		Fingerprint: setItemImageFingerprint{ItemID: cmd.ItemID, SHA256: stored.SHA256},
		Required:    []string{CapAdministerStructure},
	}
	key := sql.NullString{String: stored.Key, Valid: true}
	return ExecuteMutation(ctx, h.runner, actor, spec, func(q *sqlc.Queries) (int, ItemImageResponse, AuditRecord, error) {
		return setImageKey(ctx, q, cmd.ItemID, key, EventItemImageSet)
	})
}

// ClearItemImageHandler removes an item's image reference. The file stays.
type ClearItemImageHandler struct {
	runner *Runner
}

// NewClearItemImageHandler creates a ClearItemImageHandler.
func NewClearItemImageHandler(runner *Runner) *ClearItemImageHandler {
	return &ClearItemImageHandler{runner: runner}
}

// Handle clears image_key.
func (h *ClearItemImageHandler) Handle(ctx context.Context, actor Actor, cmd ClearItemImageCommand) (int, ItemImageResponse, error) {
	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpItemClearImage,
		Fingerprint: clearItemImageFingerprint{ItemID: cmd.ItemID},
		Required:    []string{CapAdministerStructure},
	}
	return ExecuteMutation(ctx, h.runner, actor, spec, func(q *sqlc.Queries) (int, ItemImageResponse, AuditRecord, error) {
		return setImageKey(ctx, q, cmd.ItemID, sql.NullString{}, EventItemImageCleared)
	})
}

func setImageKey(ctx context.Context, q *sqlc.Queries, itemID uuid.UUID, key sql.NullString, event string) (int, ItemImageResponse, AuditRecord, error) {
	existing, err := q.GetMenuItemForUpdate(ctx, itemID)
	if err != nil {
		return 0, ItemImageResponse{}, AuditRecord{}, MapDBError(err)
	}
	if existing.RetiredAt.Valid {
		return 0, ItemImageResponse{}, AuditRecord{}, fmt.Errorf("%w: item is retired", ErrEntityRetired)
	}
	item, err := q.SetMenuItemImageKey(ctx, sqlc.SetMenuItemImageKeyParams{ID: itemID, ImageKey: key})
	if err != nil {
		return 0, ItemImageResponse{}, AuditRecord{}, MapDBError(err)
	}
	res := ItemImageResponse{ItemID: item.ID, ImageURL: ImageURL(item.ImageKey)}
	audit := AuditRecord{
		EventType: event,
		Details: itemImageAudit{
			ItemID: item.ID, ImageKey: nullStringPtr(item.ImageKey), PreviousImageKey: nullStringPtr(existing.ImageKey),
		},
	}
	return 200, res, audit, nil
}
```

- [ ] **Step 5: Wire the media store into `Slices` and add the HTTP handlers**

In `internal/catalog/routes.go`: change the signature to `func NewSlices(db *sql.DB, queries *sqlc.Queries, media *MediaStore) *Slices`; add fields `SetItemImage *SetItemImageHandler` and `ClearItemImage *ClearItemImageHandler`, construct them with `NewSetItemImageHandler(runner, media)` and `NewClearItemImageHandler(runner)`; register:

```go
	catalog.PUT("/items/:item_id/image", s.handleSetItemImage, authn.RequireCapability(CapAdministerStructure))
	catalog.DELETE("/items/:item_id/image", s.handleClearItemImage, authn.RequireCapability(CapAdministerStructure))
```

Update the three callers: in `cmd/api/main.go` use `catalog.NewSlices(db, queries, mediaStore)`; in `internal/catalog/catalog_integration_test.go:67` and `internal/catalog/routes_test.go:49` use `catalog.NewSlices(db, q, newTestMediaStore(t))`.

Append to `internal/catalog/http_display.go` (add imports `"errors"`, `"io"`, `"net/http"`, `"github.com/google/uuid"`):

```go
// handleSetItemImage godoc
//
//	@Summary		Tải ảnh món
//	@Description	Tải ảnh JPEG, PNG hoặc WebP (tối đa 1 MB) cho món. Trình duyệt nên thu nhỏ ảnh trước khi gửi. Yêu cầu quyền catalog.administer_structure.
//	@Tags			Catalog
//	@Accept			multipart/form-data
//	@Produce		json
//	@Security		BearerAuth
//	@Param			item_id		path		string	true	"Item ID (UUID)"
//	@Param			request_id	formData	string	true	"Request ID (UUID)"
//	@Param			file		formData	file	true	"Ảnh món"
//	@Success		200			{object}	response.APIResponse{data=ItemImageResponse}
//	@Failure		400			{object}	response.APIResponse
//	@Failure		401			{object}	response.APIResponse
//	@Failure		403			{object}	response.APIResponse
//	@Failure		404			{object}	response.APIResponse
//	@Failure		409			{object}	response.APIResponse
//	@Failure		413			{object}	response.APIResponse
//	@Failure		500			{object}	response.APIResponse
//	@Router			/catalog/items/{item_id}/image [put]
func (s *Slices) handleSetItemImage(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	itemID, err := parseUUIDParam(c, "item_id")
	if err != nil {
		return sendError(c, err)
	}

	req := c.Request()
	req.Body = http.MaxBytesReader(c.Response(), req.Body, MaxImageUploadBody)
	if err := req.ParseMultipartForm(MaxImageUploadBody); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return sendError(c, fmt.Errorf("%w: upload exceeds %d bytes", ErrImageTooLarge, MaxImageBytes))
		}
		return sendError(c, fmt.Errorf("%w: invalid multipart body: %s", response.ErrInvalid, err.Error()))
	}
	requestID, err := uuid.Parse(req.FormValue("request_id"))
	if err != nil || requestID == uuid.Nil {
		return sendError(c, fmt.Errorf("%w: request_id is required", response.ErrInvalid))
	}
	fh, err := c.FormFile("file")
	if err != nil {
		return sendError(c, fmt.Errorf("%w: file is required", response.ErrInvalid))
	}
	f, err := fh.Open()
	if err != nil {
		return sendError(c, fmt.Errorf("%w: cannot read file: %s", response.ErrInvalid, err.Error()))
	}
	defer f.Close() //nolint:errcheck
	data, err := io.ReadAll(io.LimitReader(f, MaxImageBytes+1))
	if err != nil {
		return sendError(c, fmt.Errorf("%w: cannot read file: %s", response.ErrInvalid, err.Error()))
	}

	status, res, err := s.SetItemImage.Handle(req.Context(), actor, SetItemImageCommand{RequestID: requestID, ItemID: itemID, Data: data})
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
}

// handleClearItemImage godoc
//
//	@Summary		Gỡ ảnh món
//	@Description	Gỡ ảnh khỏi món. File ảnh vẫn được giữ trên đĩa. Yêu cầu quyền catalog.administer_structure.
//	@Tags			Catalog
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			item_id	path		string			true	"Item ID (UUID)"
//	@Param			request	body		MutationRequest	true	"Request ID"
//	@Success		200		{object}	response.APIResponse{data=ItemImageResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Failure		500		{object}	response.APIResponse
//	@Router			/catalog/items/{item_id}/image [delete]
func (s *Slices) handleClearItemImage(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	itemID, err := parseUUIDParam(c, "item_id")
	if err != nil {
		return sendError(c, err)
	}
	req, err := bindBody[MutationRequest](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(req.RequestID); err != nil {
		return sendError(c, err)
	}
	status, res, err := s.ClearItemImage.Handle(c.Request().Context(), actor, ClearItemImageCommand{RequestID: req.RequestID, ItemID: itemID})
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
}
```

- [ ] **Step 6: Run the tests and the build**

Run: `go build ./... && go test -tags integration ./internal/catalog/ -run 'TestItemImageCommands|TestDisplayDetailsRoutes' -count=1`
Expected: PASS. If the 2 MB case returns 400 instead of 413, the multipart parser wrapped the error without `%w`; then detect it with `strings.Contains(err.Error(), "request body too large")` as a fallback beside `errors.As`, and keep the test.

- [ ] **Step 7: Commit**

```bash
git add sql/queries/catalog.sql internal/database/sqlc internal/catalog cmd/api/main.go
git commit -m "feat(catalog): upload and clear item images

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: Move an item to another category (command 3)

**Files:**
- Modify: `sql/queries/catalog.sql` (2 queries), then `make sqlc`
- Modify: `internal/catalog/dto.go`, `internal/catalog/routes.go`
- Create: `internal/catalog/structure.go`, `internal/catalog/http_structure.go`
- Test: `internal/catalog/structure_integration_test.go`

**Interfaces:**
- Consumes: `managerActor`, `auditCount`, `retireCategoryDirect`, `strPtr` (Task 4).
- Produces:
  - `type MoveItemCategoryCommand struct { RequestID, ItemID, CategoryID uuid.UUID }`
  - `type ItemCategoryResponse struct { ItemID, CategoryID uuid.UUID; RemovedExclusionGroupIDs []uuid.UUID }`
  - `func NewMoveItemCategoryHandler(r *Runner) *MoveItemCategoryHandler`
  - Test helpers: `attachCategoryGroupDirect(t, db, catID, groupID)`, `attachItemGroupDirect(t, db, itemID, groupID)`, `excludeItemGroupDirect(t, db, itemID, groupID)`, `idsFrom(t, db, query string, arg uuid.UUID) []uuid.UUID`

- [ ] **Step 1: Add the queries**

Append to `sql/queries/catalog.sql`:

```sql
-- -- Structure (BA-1) --

-- name: MoveMenuItemToCategory :one
UPDATE menu_items
SET category_id = $2, updated_at = now()
WHERE id = $1
RETURNING id, category_id, name, normalized_name, price_vnd,
          available, retired_at, retirement_reason, retirement_note,
          created_at, updated_at,
          code, normalized_code, badge, description, image_key;

-- name: DeleteItemExclusionsOutsideCategory :many
-- Exclusion invariant (ADR-059): an exclusion exists only while the item's
-- category provides the group.
DELETE FROM item_modifier_group_exclusions e
WHERE e.menu_item_id = sqlc.arg(item_id)
  AND NOT EXISTS (
      SELECT 1 FROM category_modifier_groups c
      WHERE c.menu_category_id = sqlc.arg(category_id)
        AND c.modifier_group_id = e.modifier_group_id)
RETURNING e.modifier_group_id;
```

Run: `make sqlc`. Expected: `DeleteItemExclusionsOutsideCategory(ctx, DeleteItemExclusionsOutsideCategoryParams{ItemID, CategoryID}) ([]uuid.UUID, error)`.

- [ ] **Step 2: Write the failing test**

Create `internal/catalog/structure_integration_test.go`:

```go
//go:build integration

package catalog_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/catalog"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func attachCategoryGroupDirect(t *testing.T, db *sql.DB, catID, groupID uuid.UUID) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO category_modifier_groups (menu_category_id, modifier_group_id) VALUES ($1, $2)`, catID, groupID)
	require.NoError(t, err)
}

func attachItemGroupDirect(t *testing.T, db *sql.DB, itemID, groupID uuid.UUID) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO item_modifier_groups (menu_item_id, modifier_group_id) VALUES ($1, $2)`, itemID, groupID)
	require.NoError(t, err)
}

func excludeItemGroupDirect(t *testing.T, db *sql.DB, itemID, groupID uuid.UUID) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO item_modifier_group_exclusions (menu_item_id, modifier_group_id) VALUES ($1, $2)`, itemID, groupID)
	require.NoError(t, err)
}

// idsFrom runs a one-argument query returning a single uuid column.
func idsFrom(t *testing.T, db *sql.DB, query string, arg uuid.UUID) []uuid.UUID {
	t.Helper()
	rows, err := db.Query(query, arg)
	require.NoError(t, err)
	defer rows.Close()
	out := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		require.NoError(t, rows.Scan(&id))
		out = append(out, id)
	}
	require.NoError(t, rows.Err())
	return out
}

const exclusionsOfItem = `SELECT modifier_group_id FROM item_modifier_group_exclusions WHERE menu_item_id = $1 ORDER BY modifier_group_id`

func TestMoveItemCategory(t *testing.T) {
	db, q := openExecutorTestDB(t)
	handler := catalog.NewMoveItemCategoryHandler(catalog.NewRunner(db, q))
	ctx := context.Background()
	price := int64(30000)

	t.Run("moves and drops exclusions the new category does not provide", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		actor := managerActor(t, db, q)
		from := createTestCategoryDirect(t, db, "Coffee")
		to := createTestCategoryDirect(t, db, "Tea")
		sugar := createTestModifierGroupDirect(t, db, "Sugar", 0, 1, false)
		ice := createTestModifierGroupDirect(t, db, "Ice", 0, 1, false)
		attachCategoryGroupDirect(t, db, from, sugar)
		attachCategoryGroupDirect(t, db, from, ice)
		attachCategoryGroupDirect(t, db, to, ice)
		item := createTestItemDirect(t, db, from, "Latte", &price, false)
		excludeItemGroupDirect(t, db, item, sugar)
		excludeItemGroupDirect(t, db, item, ice)

		status, res, err := handler.Handle(ctx, actor, catalog.MoveItemCategoryCommand{RequestID: uuid.New(), ItemID: item, CategoryID: to})
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.Equal(t, to, res.CategoryID)
		assert.Equal(t, []uuid.UUID{sugar}, res.RemovedExclusionGroupIDs)
		assert.Equal(t, []uuid.UUID{ice}, idsFrom(t, db, exclusionsOfItem, item), "the exclusion the new category provides stays")

		var details []byte
		require.NoError(t, db.QueryRow(`SELECT details FROM audit_events WHERE event_type = $1`, catalog.EventItemCategoryChanged).Scan(&details))
		var audit struct {
			From    uuid.UUID   `json:"from_category_id"`
			To      uuid.UUID   `json:"to_category_id"`
			Removed []uuid.UUID `json:"removed_exclusion_group_ids"`
		}
		require.NoError(t, json.Unmarshal(details, &audit))
		assert.Equal(t, from, audit.From)
		assert.Equal(t, to, audit.To)
		assert.Equal(t, []uuid.UUID{sugar}, audit.Removed)
	})

	t.Run("moving to the current category is a no-op without audit", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		cat := createTestCategoryDirect(t, db, "Coffee")
		item := createTestItemDirect(t, db, cat, "Latte", &price, false)
		_, res, err := handler.Handle(ctx, managerActor(t, db, q), catalog.MoveItemCategoryCommand{RequestID: uuid.New(), ItemID: item, CategoryID: cat})
		require.NoError(t, err)
		assert.Equal(t, cat, res.CategoryID)
		assert.Empty(t, res.RemovedExclusionGroupIDs)
		assert.Equal(t, 0, auditCount(t, db, catalog.EventItemCategoryChanged))
	})

	t.Run("a name taken in the target is CATALOG_NAME_CONFLICT", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		from := createTestCategoryDirect(t, db, "Coffee")
		to := createTestCategoryDirect(t, db, "Tea")
		item := createTestItemDirect(t, db, from, "Latte", &price, false)
		createTestItemDirect(t, db, to, "latte", &price, false)
		_, _, err := handler.Handle(ctx, managerActor(t, db, q), catalog.MoveItemCategoryCommand{RequestID: uuid.New(), ItemID: item, CategoryID: to})
		assert.True(t, errors.Is(err, catalog.ErrNameConflict), "got %v", err)
	})

	t.Run("retired target is ErrEntityRetired", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		from := createTestCategoryDirect(t, db, "Coffee")
		to := createTestCategoryDirect(t, db, "Tea")
		retireCategoryDirect(t, db, to)
		item := createTestItemDirect(t, db, from, "Latte", &price, false)
		_, _, err := handler.Handle(ctx, managerActor(t, db, q), catalog.MoveItemCategoryCommand{RequestID: uuid.New(), ItemID: item, CategoryID: to})
		assert.True(t, errors.Is(err, catalog.ErrEntityRetired))
	})

	t.Run("unknown target is ErrNotFound", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		from := createTestCategoryDirect(t, db, "Coffee")
		item := createTestItemDirect(t, db, from, "Latte", &price, false)
		_, _, err := handler.Handle(ctx, managerActor(t, db, q), catalog.MoveItemCategoryCommand{RequestID: uuid.New(), ItemID: item, CategoryID: uuid.New()})
		assert.True(t, errors.Is(err, catalog.ErrNotFound))
	})
}
```

Run: `go test -tags integration ./internal/catalog/ -run TestMoveItemCategory -count=1`
Expected: compile FAIL — `undefined: catalog.NewMoveItemCategoryHandler`.

- [ ] **Step 3: Add DTOs and implement**

Append to `internal/catalog/dto.go`:

```go
// === Structure (BA-1) ===

// MoveItemCategoryRequest moves an item to another category.
type MoveItemCategoryRequest struct {
	RequestID  uuid.UUID `json:"request_id"`
	CategoryID uuid.UUID `json:"category_id"`
}

// MoveItemCategoryCommand carries the parameters for moving an item.
type MoveItemCategoryCommand struct {
	RequestID  uuid.UUID
	ItemID     uuid.UUID
	CategoryID uuid.UUID
}

// ItemCategoryResponse reports an item's category and the exclusions the move dropped.
type ItemCategoryResponse struct {
	ItemID                   uuid.UUID   `json:"item_id"`
	CategoryID               uuid.UUID   `json:"category_id"`
	RemovedExclusionGroupIDs []uuid.UUID `json:"removed_exclusion_group_ids"`
}
```

Create `internal/catalog/structure.go`:

```go
package catalog

import (
	"context"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

type moveItemCategoryFingerprint struct {
	ItemID     uuid.UUID `json:"item_id"`
	CategoryID uuid.UUID `json:"category_id"`
}

type itemCategoryChangedAudit struct {
	ItemID                   uuid.UUID   `json:"item_id"`
	FromCategoryID           uuid.UUID   `json:"from_category_id"`
	ToCategoryID             uuid.UUID   `json:"to_category_id"`
	RemovedExclusionGroupIDs []uuid.UUID `json:"removed_exclusion_group_ids"`
}

// MoveItemCategoryHandler moves a Menu Item to another category.
type MoveItemCategoryHandler struct {
	runner *Runner
}

// NewMoveItemCategoryHandler creates a MoveItemCategoryHandler.
func NewMoveItemCategoryHandler(runner *Runner) *MoveItemCategoryHandler {
	return &MoveItemCategoryHandler{runner: runner}
}

// Handle locks the item, then both categories in id order, moves the item, and
// deletes exclusions the target category does not provide (ADR-059).
func (h *MoveItemCategoryHandler) Handle(ctx context.Context, actor Actor, cmd MoveItemCategoryCommand) (int, ItemCategoryResponse, error) {
	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpItemMoveCategory,
		Fingerprint: moveItemCategoryFingerprint{ItemID: cmd.ItemID, CategoryID: cmd.CategoryID},
		Required:    []string{CapAdministerStructure},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec, func(q *sqlc.Queries) (int, ItemCategoryResponse, AuditRecord, error) {
		item, err := q.GetMenuItemForUpdate(ctx, cmd.ItemID)
		if err != nil {
			return 0, ItemCategoryResponse{}, AuditRecord{}, MapDBError(err)
		}
		if item.RetiredAt.Valid {
			return 0, ItemCategoryResponse{}, AuditRecord{}, fmt.Errorf("%w: item is retired", ErrEntityRetired)
		}
		if item.CategoryID == cmd.CategoryID {
			return 200, ItemCategoryResponse{ItemID: item.ID, CategoryID: item.CategoryID, RemovedExclusionGroupIDs: []uuid.UUID{}}, AuditRecord{}, nil
		}

		for _, catID := range UnionIDs([]uuid.UUID{item.CategoryID, cmd.CategoryID}) {
			cat, err := q.GetMenuCategoryForUpdate(ctx, catID)
			if err != nil {
				return 0, ItemCategoryResponse{}, AuditRecord{}, MapDBError(err)
			}
			if catID == cmd.CategoryID && cat.RetiredAt.Valid {
				return 0, ItemCategoryResponse{}, AuditRecord{}, fmt.Errorf("%w: target category is retired", ErrEntityRetired)
			}
		}

		moved, err := q.MoveMenuItemToCategory(ctx, sqlc.MoveMenuItemToCategoryParams{ID: cmd.ItemID, CategoryID: cmd.CategoryID})
		if err != nil {
			return 0, ItemCategoryResponse{}, AuditRecord{}, MapDBError(err)
		}
		removed, err := q.DeleteItemExclusionsOutsideCategory(ctx, sqlc.DeleteItemExclusionsOutsideCategoryParams{
			ItemID: cmd.ItemID, CategoryID: cmd.CategoryID,
		})
		if err != nil {
			return 0, ItemCategoryResponse{}, AuditRecord{}, MapDBError(err)
		}
		removed = UnionIDs(removed)

		res := ItemCategoryResponse{ItemID: moved.ID, CategoryID: moved.CategoryID, RemovedExclusionGroupIDs: removed}
		audit := AuditRecord{
			EventType: EventItemCategoryChanged,
			Details: itemCategoryChangedAudit{
				ItemID: moved.ID, FromCategoryID: item.CategoryID, ToCategoryID: moved.CategoryID, RemovedExclusionGroupIDs: removed,
			},
		}
		return 200, res, audit, nil
	})
}
```

- [ ] **Step 4: Add the HTTP handler and route**

Create `internal/catalog/http_structure.go`:

```go
package catalog

import (
	"github.com/labstack/echo/v4"
)

// handleMoveItemCategory godoc
//
//	@Summary		Chuyển món sang danh mục khác
//	@Description	Chuyển món sang danh mục khác. Các loại trừ nhóm topping mà danh mục mới không cung cấp sẽ bị xóa. Yêu cầu quyền catalog.administer_structure.
//	@Tags			Catalog
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			item_id	path		string					true	"Item ID (UUID)"
//	@Param			request	body		MoveItemCategoryRequest	true	"Danh mục đích"
//	@Success		200		{object}	response.APIResponse{data=ItemCategoryResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Failure		500		{object}	response.APIResponse
//	@Router			/catalog/items/{item_id}/category [patch]
func (s *Slices) handleMoveItemCategory(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	itemID, err := parseUUIDParam(c, "item_id")
	if err != nil {
		return sendError(c, err)
	}
	req, err := bindBody[MoveItemCategoryRequest](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(req.RequestID); err != nil {
		return sendError(c, err)
	}
	status, res, err := s.MoveItemCategory.Handle(c.Request().Context(), actor, MoveItemCategoryCommand{
		RequestID: req.RequestID, ItemID: itemID, CategoryID: req.CategoryID,
	})
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
}
```

In `routes.go`: field `MoveItemCategory *MoveItemCategoryHandler`, constructed with `NewMoveItemCategoryHandler(runner)`, route:

```go
	// Structure (BA-1)
	catalog.PATCH("/items/:item_id/category", s.handleMoveItemCategory, authn.RequireCapability(CapAdministerStructure))
```

- [ ] **Step 5: Run the tests**

Run: `go test -tags integration ./internal/catalog/ -run TestMoveItemCategory -count=1 && go build ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add sql/queries/catalog.sql internal/database/sqlc internal/catalog
git commit -m "feat(catalog): move an item to another category

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7: Add a Size and add a Modifier Option (commands 4 and 5)

**Files:**
- Modify: `internal/catalog/dto.go`, `internal/catalog/structure.go`, `internal/catalog/http_structure.go`, `internal/catalog/routes.go`
- Test: `internal/catalog/structure_integration_test.go`

**Interfaces:**
- Consumes: `testManagerPIN`, `managerActor`, `auditCount` (Task 4); existing `sqlc.CreateMenuItemSize`, `sqlc.CreateModifierOption`, `ValidatePrice`, `ValidateSurcharge`, `NormalizeName`, `SizeResponse`, `ModifierOptionResponse`.
- Produces:
  - `type AddSizeCommand struct { RequestID, ItemID uuid.UUID; Name string; PriceVND int64; ManagerPIN string }` → `NewAddSizeHandler(r)`, returns `(int, SizeResponse, error)`, status 201
  - `type AddModifierOptionCommand struct { RequestID, GroupID uuid.UUID; Name string; SurchargeVND int64; ManagerPIN string }` → `NewAddModifierOptionHandler(r)`, returns `(int, ModifierOptionResponse, error)`, status 201

- [ ] **Step 1: Write the failing tests**

Append to `internal/catalog/structure_integration_test.go`:

```go
func TestAddSize(t *testing.T) {
	db, q := openExecutorTestDB(t)
	handler := catalog.NewAddSizeHandler(catalog.NewRunner(db, q))
	ctx := context.Background()

	t.Run("adds an available size to a sized item", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		cat := createTestCategoryDirect(t, db, "Coffee")
		item := createTestItemDirect(t, db, cat, "Latte", nil, false)
		createTestSizeDirect(t, db, item, "Size M", 35000, false)

		status, res, err := handler.Handle(ctx, managerActor(t, db, q), catalog.AddSizeCommand{
			RequestID: uuid.New(), ItemID: item, Name: "  Size XL ", PriceVND: 59000, ManagerPIN: testManagerPIN,
		})
		require.NoError(t, err)
		assert.Equal(t, 201, status)
		assert.Equal(t, "Size XL", res.Name)
		assert.Equal(t, int64(59000), res.PriceVND)
		assert.True(t, res.Available)
		assert.Equal(t, 1, auditCount(t, db, catalog.EventSizeCreated))
	})

	t.Run("single-price item is INVALID_PRICING_CONFIGURATION", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		cat := createTestCategoryDirect(t, db, "Bakery")
		price := int64(35000)
		item := createTestItemDirect(t, db, cat, "Croissant", &price, false)
		_, _, err := handler.Handle(ctx, managerActor(t, db, q), catalog.AddSizeCommand{
			RequestID: uuid.New(), ItemID: item, Name: "Large", PriceVND: 40000, ManagerPIN: testManagerPIN,
		})
		assert.True(t, errors.Is(err, catalog.ErrInvalidPricingConfiguration))
	})

	t.Run("a retired size's name is still taken", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		cat := createTestCategoryDirect(t, db, "Coffee")
		item := createTestItemDirect(t, db, cat, "Latte", nil, false)
		createTestSizeDirect(t, db, item, "Size M", 35000, false)
		createTestSizeDirect(t, db, item, "Size S", 29000, true)
		_, _, err := handler.Handle(ctx, managerActor(t, db, q), catalog.AddSizeCommand{
			RequestID: uuid.New(), ItemID: item, Name: "size s", PriceVND: 30000, ManagerPIN: testManagerPIN,
		})
		assert.True(t, errors.Is(err, catalog.ErrNameConflict))
	})

	t.Run("wrong PIN is ErrInvalidManagerPin", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		cat := createTestCategoryDirect(t, db, "Coffee")
		item := createTestItemDirect(t, db, cat, "Latte", nil, false)
		createTestSizeDirect(t, db, item, "Size M", 35000, false)
		_, _, err := handler.Handle(ctx, managerActor(t, db, q), catalog.AddSizeCommand{
			RequestID: uuid.New(), ItemID: item, Name: "Size L", PriceVND: 40000, ManagerPIN: "000000",
		})
		assert.True(t, errors.Is(err, catalog.ErrInvalidManagerPin))
	})

	t.Run("price out of range is INVALID_PRICING_CONFIGURATION", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		cat := createTestCategoryDirect(t, db, "Coffee")
		item := createTestItemDirect(t, db, cat, "Latte", nil, false)
		createTestSizeDirect(t, db, item, "Size M", 35000, false)
		_, _, err := handler.Handle(ctx, managerActor(t, db, q), catalog.AddSizeCommand{
			RequestID: uuid.New(), ItemID: item, Name: "Size L", PriceVND: 0, ManagerPIN: testManagerPIN,
		})
		assert.True(t, errors.Is(err, catalog.ErrInvalidPricingConfiguration))
	})
}

func TestAddModifierOption(t *testing.T) {
	db, q := openExecutorTestDB(t)
	handler := catalog.NewAddModifierOptionHandler(catalog.NewRunner(db, q))
	ctx := context.Background()

	t.Run("adds an available, non-default option", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		group := createTestModifierGroupDirect(t, db, "Topping", 0, 1, false)
		createTestModifierOptionDirect(t, db, group, "Pearl", 5000, true, false)

		status, res, err := handler.Handle(ctx, managerActor(t, db, q), catalog.AddModifierOptionCommand{
			RequestID: uuid.New(), GroupID: group, Name: "Thạch dừa", SurchargeVND: 8000, ManagerPIN: testManagerPIN,
		})
		require.NoError(t, err)
		assert.Equal(t, 201, status)
		assert.Equal(t, "Thạch dừa", res.Name)
		assert.Equal(t, int64(8000), res.SurchargeVND)
		assert.True(t, res.Available)
		assert.Empty(t, idsFrom(t, db, `SELECT modifier_option_id FROM modifier_group_default_options WHERE modifier_group_id = $1`, group))
		assert.Equal(t, 1, auditCount(t, db, catalog.EventModifierOptionCreated))
	})

	t.Run("duplicate name is CATALOG_NAME_CONFLICT", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		group := createTestModifierGroupDirect(t, db, "Topping", 0, 1, false)
		createTestModifierOptionDirect(t, db, group, "Pearl", 5000, true, false)
		_, _, err := handler.Handle(ctx, managerActor(t, db, q), catalog.AddModifierOptionCommand{
			RequestID: uuid.New(), GroupID: group, Name: "pearl", SurchargeVND: 0, ManagerPIN: testManagerPIN,
		})
		assert.True(t, errors.Is(err, catalog.ErrNameConflict))
	})

	t.Run("retired group is ErrEntityRetired", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		group := createTestModifierGroupDirect(t, db, "Topping", 0, 1, true)
		_, _, err := handler.Handle(ctx, managerActor(t, db, q), catalog.AddModifierOptionCommand{
			RequestID: uuid.New(), GroupID: group, Name: "Jelly", SurchargeVND: 0, ManagerPIN: testManagerPIN,
		})
		assert.True(t, errors.Is(err, catalog.ErrEntityRetired))
	})

	t.Run("cashier is forbidden", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		group := createTestModifierGroupDirect(t, db, "Topping", 0, 1, false)
		_, _, err := handler.Handle(ctx, cashierActor(t, db, q), catalog.AddModifierOptionCommand{
			RequestID: uuid.New(), GroupID: group, Name: "Jelly", SurchargeVND: 0, ManagerPIN: testManagerPIN,
		})
		assert.True(t, errors.Is(err, catalog.ErrForbidden))
	})
}
```

Run: `go test -tags integration ./internal/catalog/ -run 'TestAddSize|TestAddModifierOption' -count=1`
Expected: compile FAIL — `undefined: catalog.NewAddSizeHandler`.

- [ ] **Step 2: Add DTOs**

Append to `internal/catalog/dto.go`:

```go
// AddSizeRequest adds a Size to a sized item.
type AddSizeRequest struct {
	RequestID  uuid.UUID `json:"request_id"`
	Name       string    `json:"name"`
	PriceVND   int64     `json:"price_vnd"`
	ManagerPIN string    `json:"manager_pin"`
}

// AddSizeCommand carries the parameters for adding a Size.
type AddSizeCommand struct {
	RequestID  uuid.UUID
	ItemID     uuid.UUID
	Name       string
	PriceVND   int64
	ManagerPIN string
}

// AddModifierOptionRequest adds an Option to a modifier group.
type AddModifierOptionRequest struct {
	RequestID    uuid.UUID `json:"request_id"`
	Name         string    `json:"name"`
	SurchargeVND int64     `json:"surcharge_vnd"`
	ManagerPIN   string    `json:"manager_pin"`
}

// AddModifierOptionCommand carries the parameters for adding an Option.
type AddModifierOptionCommand struct {
	RequestID    uuid.UUID
	GroupID      uuid.UUID
	Name         string
	SurchargeVND int64
	ManagerPIN   string
}
```

- [ ] **Step 3: Implement**

Append to `internal/catalog/structure.go` (add imports `"github.com/Mirai3103/pos-cafe/internal/response"`):

```go
type addSizeFingerprint struct {
	ItemID   uuid.UUID `json:"item_id"`
	Name     string    `json:"name"`
	PriceVND int64     `json:"price_vnd"`
}

type sizeCreatedAudit struct {
	SizeID     uuid.UUID `json:"size_id"`
	MenuItemID uuid.UUID `json:"menu_item_id"`
	Name       string    `json:"name"`
	PriceVND   int64     `json:"price_vnd"`
}

// AddSizeHandler adds a Size to an item that is already sized (ADR-060).
type AddSizeHandler struct {
	runner *Runner
}

// NewAddSizeHandler creates an AddSizeHandler.
func NewAddSizeHandler(runner *Runner) *AddSizeHandler {
	return &AddSizeHandler{runner: runner}
}

// Handle requires a fresh Manager PIN because it sets a price.
func (h *AddSizeHandler) Handle(ctx context.Context, actor Actor, cmd AddSizeCommand) (int, SizeResponse, error) {
	display, key := NormalizeName(cmd.Name)
	spec := MutationSpec{
		RequestID:         cmd.RequestID,
		Operation:         OpSizeCreate,
		Fingerprint:       addSizeFingerprint{ItemID: cmd.ItemID, Name: display, PriceVND: cmd.PriceVND},
		Required:          []string{CapAdministerStructure, CapChangePrice},
		RequireManagerPIN: true,
		ManagerPIN:        cmd.ManagerPIN,
	}

	return ExecuteMutation(ctx, h.runner, actor, spec, func(q *sqlc.Queries) (int, SizeResponse, AuditRecord, error) {
		item, err := q.GetMenuItemForUpdate(ctx, cmd.ItemID)
		if err != nil {
			return 0, SizeResponse{}, AuditRecord{}, MapDBError(err)
		}
		if item.RetiredAt.Valid {
			return 0, SizeResponse{}, AuditRecord{}, fmt.Errorf("%w: item is retired", ErrEntityRetired)
		}
		if item.PriceVnd.Valid {
			return 0, SizeResponse{}, AuditRecord{}, fmt.Errorf("%w: a single-price item has no sizes", ErrInvalidPricingConfiguration)
		}
		if display == "" {
			return 0, SizeResponse{}, AuditRecord{}, fmt.Errorf("%w: size name cannot be empty", response.ErrInvalid)
		}
		if err := ValidatePrice(cmd.PriceVND); err != nil {
			return 0, SizeResponse{}, AuditRecord{}, fmt.Errorf("%w: %s", ErrInvalidPricingConfiguration, err.Error())
		}

		size, err := q.CreateMenuItemSize(ctx, sqlc.CreateMenuItemSizeParams{
			MenuItemID: cmd.ItemID, Name: display, NormalizedName: key, PriceVnd: cmd.PriceVND, Available: true,
		})
		if err != nil {
			return 0, SizeResponse{}, AuditRecord{}, MapDBError(err)
		}

		res := SizeResponse{ID: size.ID, Name: size.Name, PriceVND: size.PriceVnd, Available: size.Available}
		audit := AuditRecord{
			EventType: EventSizeCreated,
			Details:   sizeCreatedAudit{SizeID: size.ID, MenuItemID: cmd.ItemID, Name: size.Name, PriceVND: size.PriceVnd},
		}
		return 201, res, audit, nil
	})
}

type addModifierOptionFingerprint struct {
	GroupID      uuid.UUID `json:"group_id"`
	Name         string    `json:"name"`
	SurchargeVND int64     `json:"surcharge_vnd"`
}

type modifierOptionCreatedAudit struct {
	OptionID        uuid.UUID `json:"option_id"`
	ModifierGroupID uuid.UUID `json:"modifier_group_id"`
	Name            string    `json:"name"`
	SurchargeVND    int64     `json:"surcharge_vnd"`
}

// AddModifierOptionHandler adds an Option to an existing modifier group.
type AddModifierOptionHandler struct {
	runner *Runner
}

// NewAddModifierOptionHandler creates an AddModifierOptionHandler.
func NewAddModifierOptionHandler(runner *Runner) *AddModifierOptionHandler {
	return &AddModifierOptionHandler{runner: runner}
}

// Handle requires a fresh Manager PIN because it sets a surcharge.
func (h *AddModifierOptionHandler) Handle(ctx context.Context, actor Actor, cmd AddModifierOptionCommand) (int, ModifierOptionResponse, error) {
	display, key := NormalizeName(cmd.Name)
	spec := MutationSpec{
		RequestID:         cmd.RequestID,
		Operation:         OpModifierOptionCreate,
		Fingerprint:       addModifierOptionFingerprint{GroupID: cmd.GroupID, Name: display, SurchargeVND: cmd.SurchargeVND},
		Required:          []string{CapAdministerStructure, CapChangePrice},
		RequireManagerPIN: true,
		ManagerPIN:        cmd.ManagerPIN,
	}

	return ExecuteMutation(ctx, h.runner, actor, spec, func(q *sqlc.Queries) (int, ModifierOptionResponse, AuditRecord, error) {
		group, err := q.GetModifierGroupForUpdate(ctx, cmd.GroupID)
		if err != nil {
			return 0, ModifierOptionResponse{}, AuditRecord{}, MapDBError(err)
		}
		if group.RetiredAt.Valid {
			return 0, ModifierOptionResponse{}, AuditRecord{}, fmt.Errorf("%w: modifier group is retired", ErrEntityRetired)
		}
		if display == "" {
			return 0, ModifierOptionResponse{}, AuditRecord{}, fmt.Errorf("%w: option name cannot be empty", response.ErrInvalid)
		}
		if err := ValidateSurcharge(cmd.SurchargeVND); err != nil {
			return 0, ModifierOptionResponse{}, AuditRecord{}, fmt.Errorf("%w: %s", ErrInvalidPricingConfiguration, err.Error())
		}

		opt, err := q.CreateModifierOption(ctx, sqlc.CreateModifierOptionParams{
			ModifierGroupID: cmd.GroupID, Name: display, NormalizedName: key, SurchargeVnd: cmd.SurchargeVND, Available: true,
		})
		if err != nil {
			return 0, ModifierOptionResponse{}, AuditRecord{}, MapDBError(err)
		}

		res := ModifierOptionResponse{ID: opt.ID, ModifierGroupID: opt.ModifierGroupID, Name: opt.Name, SurchargeVND: opt.SurchargeVnd, Available: opt.Available}
		audit := AuditRecord{
			EventType: EventModifierOptionCreated,
			Details:   modifierOptionCreatedAudit{OptionID: opt.ID, ModifierGroupID: opt.ModifierGroupID, Name: opt.Name, SurchargeVND: opt.SurchargeVnd},
		}
		return 201, res, audit, nil
	})
}
```

- [ ] **Step 4: HTTP handlers and routes**

Append to `internal/catalog/http_structure.go`:

```go
// handleAddSize godoc
//
//	@Summary		Thêm kích cỡ cho món
//	@Description	Thêm kích cỡ mới cho món đang bán theo kích cỡ. Món bán giá đơn không nhận kích cỡ. Yêu cầu quyền catalog.administer_structure, catalog.change_price và PIN quản lý.
//	@Tags			Catalog
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			item_id	path		string			true	"Item ID (UUID)"
//	@Param			request	body		AddSizeRequest	true	"Kích cỡ mới"
//	@Success		201		{object}	response.APIResponse{data=SizeResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Failure		500		{object}	response.APIResponse
//	@Router			/catalog/items/{item_id}/sizes [post]
func (s *Slices) handleAddSize(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	itemID, err := parseUUIDParam(c, "item_id")
	if err != nil {
		return sendError(c, err)
	}
	req, err := bindBody[AddSizeRequest](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(req.RequestID); err != nil {
		return sendError(c, err)
	}
	status, res, err := s.AddSize.Handle(c.Request().Context(), actor, AddSizeCommand{
		RequestID: req.RequestID, ItemID: itemID, Name: req.Name, PriceVND: req.PriceVND, ManagerPIN: req.ManagerPIN,
	})
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
}

// handleAddModifierOption godoc
//
//	@Summary		Thêm tùy chọn vào nhóm topping
//	@Description	Thêm tùy chọn mới vào nhóm topping. Tùy chọn mới không phải mặc định. Yêu cầu quyền catalog.administer_structure, catalog.change_price và PIN quản lý.
//	@Tags			Catalog
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			group_id	path		string						true	"Modifier Group ID (UUID)"
//	@Param			request		body		AddModifierOptionRequest	true	"Tùy chọn mới"
//	@Success		201			{object}	response.APIResponse{data=ModifierOptionResponse}
//	@Failure		400			{object}	response.APIResponse
//	@Failure		401			{object}	response.APIResponse
//	@Failure		403			{object}	response.APIResponse
//	@Failure		404			{object}	response.APIResponse
//	@Failure		409			{object}	response.APIResponse
//	@Failure		500			{object}	response.APIResponse
//	@Router			/catalog/modifier-groups/{group_id}/options [post]
func (s *Slices) handleAddModifierOption(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	groupID, err := parseUUIDParam(c, "group_id")
	if err != nil {
		return sendError(c, err)
	}
	req, err := bindBody[AddModifierOptionRequest](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(req.RequestID); err != nil {
		return sendError(c, err)
	}
	status, res, err := s.AddModifierOption.Handle(c.Request().Context(), actor, AddModifierOptionCommand{
		RequestID: req.RequestID, GroupID: groupID, Name: req.Name, SurchargeVND: req.SurchargeVND, ManagerPIN: req.ManagerPIN,
	})
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
}
```

In `routes.go`: fields `AddSize *AddSizeHandler`, `AddModifierOption *AddModifierOptionHandler`; constructors; routes (same capability pair as `POST /items`):

```go
	catalog.POST("/items/:item_id/sizes", s.handleAddSize, authn.RequireCapability(CapAdministerStructure), authn.RequireCapability(CapChangePrice))
	catalog.POST("/modifier-groups/:group_id/options", s.handleAddModifierOption, authn.RequireCapability(CapAdministerStructure), authn.RequireCapability(CapChangePrice))
```

- [ ] **Step 5: Run the tests**

Run: `go test -tags integration ./internal/catalog/ -run 'TestAddSize|TestAddModifierOption' -count=1 && go build ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/catalog
git commit -m "feat(catalog): add sizes and modifier options to existing entities

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 8: Change a group's selection rule (command 6)

**Files:**
- Modify: `sql/queries/catalog.sql` (1 query), `make sqlc`
- Modify: `internal/catalog/dto.go`, `internal/catalog/structure.go`, `internal/catalog/http_structure.go`, `internal/catalog/routes.go`
- Test: `internal/catalog/structure_integration_test.go`

**Interfaces:**
- Consumes: `ValidateSelectionRule`, `NormalizeIDSet` (Task 2).
- Produces: `type SetSelectionRuleCommand struct { RequestID, GroupID uuid.UUID; MinSelections, MaxSelections int32; DefaultOptionIDs []uuid.UUID }`, `type SelectionRuleResponse struct { GroupID uuid.UUID; MinSelections, MaxSelections int32; DefaultOptionIDs []uuid.UUID }`, `NewSetSelectionRuleHandler(r)`.

- [ ] **Step 1: Add the query**

```sql
-- name: UpdateModifierGroupBounds :one
UPDATE modifier_groups
SET min_selections = $2, max_selections = $3, updated_at = now()
WHERE id = $1
RETURNING id, name, normalized_name, min_selections, max_selections,
          retired_at, retirement_reason, retirement_note,
          created_at, updated_at;
```

Run: `make sqlc`.

- [ ] **Step 2: Write the failing test**

Append to `structure_integration_test.go`:

```go
func TestSetSelectionRule(t *testing.T) {
	db, q := openExecutorTestDB(t)
	handler := catalog.NewSetSelectionRuleHandler(catalog.NewRunner(db, q))
	ctx := context.Background()
	const defaultsOf = `SELECT modifier_option_id FROM modifier_group_default_options WHERE modifier_group_id = $1 ORDER BY modifier_option_id`

	setup := func(t *testing.T) (group, a, b, c uuid.UUID) {
		t.Helper()
		cleanCategoryTestTables(t, db)
		group = createTestModifierGroupDirect(t, db, "Topping", 0, 1, false)
		a = createTestModifierOptionDirect(t, db, group, "A", 0, true, false)
		b = createTestModifierOptionDirect(t, db, group, "B", 0, true, false)
		c = createTestModifierOptionDirect(t, db, group, "C", 0, false, false)
		return group, a, b, c
	}

	t.Run("changes bounds and defaults together", func(t *testing.T) {
		group, a, b, _ := setup(t)
		status, res, err := handler.Handle(ctx, managerActor(t, db, q), catalog.SetSelectionRuleCommand{
			RequestID: uuid.New(), GroupID: group, MinSelections: 2, MaxSelections: 3, DefaultOptionIDs: []uuid.UUID{b, a},
		})
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.Equal(t, int32(2), res.MinSelections)
		assert.Equal(t, int32(3), res.MaxSelections)
		assert.ElementsMatch(t, []uuid.UUID{a, b}, idsFrom(t, db, defaultsOf, group))
		assert.Equal(t, 1, auditCount(t, db, catalog.EventModifierGroupSelectionRuleChanged))
	})

	t.Run("max above active options is INVALID_MODIFIER_CONFIGURATION", func(t *testing.T) {
		group, _, _, _ := setup(t)
		createTestModifierOptionDirect(t, db, group, "Retired", 0, true, true)
		_, _, err := handler.Handle(ctx, managerActor(t, db, q), catalog.SetSelectionRuleCommand{
			RequestID: uuid.New(), GroupID: group, MinSelections: 0, MaxSelections: 4,
		})
		assert.True(t, errors.Is(err, catalog.ErrInvalidModifierConfiguration), "3 active options, max 4: got %v", err)
	})

	t.Run("an unavailable default is INVALID_MODIFIER_CONFIGURATION", func(t *testing.T) {
		group, _, _, c := setup(t)
		_, _, err := handler.Handle(ctx, managerActor(t, db, q), catalog.SetSelectionRuleCommand{
			RequestID: uuid.New(), GroupID: group, MinSelections: 1, MaxSelections: 1, DefaultOptionIDs: []uuid.UUID{c},
		})
		assert.True(t, errors.Is(err, catalog.ErrInvalidModifierConfiguration))
	})

	t.Run("a default from another group is INVALID_MODIFIER_CONFIGURATION", func(t *testing.T) {
		group, _, _, _ := setup(t)
		other := createTestModifierGroupDirect(t, db, "Other", 0, 1, false)
		foreign := createTestModifierOptionDirect(t, db, other, "X", 0, true, false)
		_, _, err := handler.Handle(ctx, managerActor(t, db, q), catalog.SetSelectionRuleCommand{
			RequestID: uuid.New(), GroupID: group, MinSelections: 1, MaxSelections: 1, DefaultOptionIDs: []uuid.UUID{foreign},
		})
		assert.True(t, errors.Is(err, catalog.ErrInvalidModifierConfiguration))
	})

	t.Run("defaults below min are INVALID_MODIFIER_CONFIGURATION", func(t *testing.T) {
		group, _, _, _ := setup(t)
		_, _, err := handler.Handle(ctx, managerActor(t, db, q), catalog.SetSelectionRuleCommand{
			RequestID: uuid.New(), GroupID: group, MinSelections: 1, MaxSelections: 2,
		})
		assert.True(t, errors.Is(err, catalog.ErrInvalidModifierConfiguration))
	})

	t.Run("retired group is ErrEntityRetired", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		group := createTestModifierGroupDirect(t, db, "Old", 0, 1, true)
		_, _, err := handler.Handle(ctx, managerActor(t, db, q), catalog.SetSelectionRuleCommand{
			RequestID: uuid.New(), GroupID: group, MinSelections: 0, MaxSelections: 1,
		})
		assert.True(t, errors.Is(err, catalog.ErrEntityRetired))
	})
}
```

Run: `go test -tags integration ./internal/catalog/ -run TestSetSelectionRule -count=1`
Expected: compile FAIL.

- [ ] **Step 3: DTOs and implementation**

Append to `dto.go`:

```go
// SetSelectionRuleRequest replaces a group's bounds and defaults together.
type SetSelectionRuleRequest struct {
	RequestID        uuid.UUID   `json:"request_id"`
	MinSelections    int32       `json:"min_selections"`
	MaxSelections    int32       `json:"max_selections"`
	DefaultOptionIDs []uuid.UUID `json:"default_option_ids"`
}

// SetSelectionRuleCommand carries the parameters for changing a selection rule.
type SetSelectionRuleCommand struct {
	RequestID        uuid.UUID
	GroupID          uuid.UUID
	MinSelections    int32
	MaxSelections    int32
	DefaultOptionIDs []uuid.UUID
}

// SelectionRuleResponse is a group's bounds and defaults.
type SelectionRuleResponse struct {
	GroupID          uuid.UUID   `json:"group_id"`
	MinSelections    int32       `json:"min_selections"`
	MaxSelections    int32       `json:"max_selections"`
	DefaultOptionIDs []uuid.UUID `json:"default_option_ids"`
}
```

Append to `structure.go`:

```go
type selectionRuleFingerprint struct {
	GroupID          uuid.UUID   `json:"group_id"`
	MinSelections    int32       `json:"min_selections"`
	MaxSelections    int32       `json:"max_selections"`
	DefaultOptionIDs []uuid.UUID `json:"default_option_ids"`
}

type selectionRuleValues struct {
	MinSelections    int32       `json:"min_selections"`
	MaxSelections    int32       `json:"max_selections"`
	DefaultOptionIDs []uuid.UUID `json:"default_option_ids"`
}

type selectionRuleChangedAudit struct {
	GroupID uuid.UUID           `json:"group_id"`
	Before  selectionRuleValues `json:"before"`
	After   selectionRuleValues `json:"after"`
}

// SetSelectionRuleHandler changes a group's min, max, and defaults atomically,
// so no intermediate state is ever invalid.
type SetSelectionRuleHandler struct {
	runner *Runner
}

// NewSetSelectionRuleHandler creates a SetSelectionRuleHandler.
func NewSetSelectionRuleHandler(runner *Runner) *SetSelectionRuleHandler {
	return &SetSelectionRuleHandler{runner: runner}
}

// Handle validates against the group's non-retired options.
func (h *SetSelectionRuleHandler) Handle(ctx context.Context, actor Actor, cmd SetSelectionRuleCommand) (int, SelectionRuleResponse, error) {
	defaults, err := NormalizeIDSet(cmd.DefaultOptionIDs, "default_option_ids")
	if err != nil {
		return 0, SelectionRuleResponse{}, fmt.Errorf("%w: %s", ErrInvalidModifierConfiguration, err.Error())
	}
	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpModifierGroupSetSelectionRule,
		Fingerprint: selectionRuleFingerprint{
			GroupID: cmd.GroupID, MinSelections: cmd.MinSelections, MaxSelections: cmd.MaxSelections, DefaultOptionIDs: defaults,
		},
		Required: []string{CapAdministerStructure},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec, func(q *sqlc.Queries) (int, SelectionRuleResponse, AuditRecord, error) {
		group, err := q.GetModifierGroupForUpdate(ctx, cmd.GroupID)
		if err != nil {
			return 0, SelectionRuleResponse{}, AuditRecord{}, MapDBError(err)
		}
		if group.RetiredAt.Valid {
			return 0, SelectionRuleResponse{}, AuditRecord{}, fmt.Errorf("%w: modifier group is retired", ErrEntityRetired)
		}

		options, err := q.ListModifierOptionsByGroup(ctx, cmd.GroupID)
		if err != nil {
			return 0, SelectionRuleResponse{}, AuditRecord{}, MapDBError(err)
		}
		active := 0
		optByID := make(map[uuid.UUID]sqlc.ModifierOption, len(options))
		for _, o := range options {
			optByID[o.ID] = o
			if !o.RetiredAt.Valid {
				active++
			}
		}
		if err := ValidateSelectionRule(cmd.MinSelections, cmd.MaxSelections, active, len(defaults)); err != nil {
			return 0, SelectionRuleResponse{}, AuditRecord{}, fmt.Errorf("%w: %s", ErrInvalidModifierConfiguration, err.Error())
		}
		for _, id := range defaults {
			o, ok := optByID[id]
			if !ok || o.RetiredAt.Valid || !o.Available {
				return 0, SelectionRuleResponse{}, AuditRecord{}, fmt.Errorf("%w: default option %s must belong to the group and be available", ErrInvalidModifierConfiguration, id)
			}
		}

		beforeRows, err := q.ListModifierGroupDefaultOptionsByGroup(ctx, cmd.GroupID)
		if err != nil {
			return 0, SelectionRuleResponse{}, AuditRecord{}, MapDBError(err)
		}
		beforeDefaults := make([]uuid.UUID, 0, len(beforeRows))
		for _, r := range beforeRows {
			beforeDefaults = append(beforeDefaults, r.ModifierOptionID)
		}

		updated, err := q.UpdateModifierGroupBounds(ctx, sqlc.UpdateModifierGroupBoundsParams{
			ID: cmd.GroupID, MinSelections: cmd.MinSelections, MaxSelections: cmd.MaxSelections,
		})
		if err != nil {
			return 0, SelectionRuleResponse{}, AuditRecord{}, MapDBError(err)
		}
		if err := q.DeleteModifierGroupDefaultOptions(ctx, cmd.GroupID); err != nil {
			return 0, SelectionRuleResponse{}, AuditRecord{}, MapDBError(err)
		}
		if len(defaults) > 0 {
			if err := q.CreateModifierGroupDefaultOptions(ctx, sqlc.CreateModifierGroupDefaultOptionsParams{
				ModifierGroupID: cmd.GroupID, OptionIds: defaults,
			}); err != nil {
				return 0, SelectionRuleResponse{}, AuditRecord{}, MapDBError(err)
			}
		}

		res := SelectionRuleResponse{
			GroupID: updated.ID, MinSelections: updated.MinSelections, MaxSelections: updated.MaxSelections, DefaultOptionIDs: defaults,
		}
		audit := AuditRecord{
			EventType: EventModifierGroupSelectionRuleChanged,
			Details: selectionRuleChangedAudit{
				GroupID: updated.ID,
				Before:  selectionRuleValues{MinSelections: group.MinSelections, MaxSelections: group.MaxSelections, DefaultOptionIDs: UnionIDs(beforeDefaults)},
				After:   selectionRuleValues{MinSelections: res.MinSelections, MaxSelections: res.MaxSelections, DefaultOptionIDs: defaults},
			},
		}
		return 200, res, audit, nil
	})
}
```

- [ ] **Step 4: HTTP handler and route**

Append to `http_structure.go`:

```go
// handleSetSelectionRule godoc
//
//	@Summary		Đổi quy tắc chọn của nhóm topping
//	@Description	Đổi số lựa chọn tối thiểu, tối đa và các tùy chọn mặc định cùng lúc. Yêu cầu quyền catalog.administer_structure.
//	@Tags			Catalog
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			group_id	path		string					true	"Modifier Group ID (UUID)"
//	@Param			request		body		SetSelectionRuleRequest	true	"Quy tắc chọn"
//	@Success		200			{object}	response.APIResponse{data=SelectionRuleResponse}
//	@Failure		400			{object}	response.APIResponse
//	@Failure		401			{object}	response.APIResponse
//	@Failure		403			{object}	response.APIResponse
//	@Failure		404			{object}	response.APIResponse
//	@Failure		409			{object}	response.APIResponse
//	@Failure		500			{object}	response.APIResponse
//	@Router			/catalog/modifier-groups/{group_id}/selection-rule [put]
func (s *Slices) handleSetSelectionRule(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	groupID, err := parseUUIDParam(c, "group_id")
	if err != nil {
		return sendError(c, err)
	}
	req, err := bindBody[SetSelectionRuleRequest](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(req.RequestID); err != nil {
		return sendError(c, err)
	}
	status, res, err := s.SetSelectionRule.Handle(c.Request().Context(), actor, SetSelectionRuleCommand{
		RequestID: req.RequestID, GroupID: groupID, MinSelections: req.MinSelections, MaxSelections: req.MaxSelections, DefaultOptionIDs: req.DefaultOptionIDs,
	})
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
}
```

`routes.go`: field `SetSelectionRule *SetSelectionRuleHandler`, constructor, route
`catalog.PUT("/modifier-groups/:group_id/selection-rule", s.handleSetSelectionRule, authn.RequireCapability(CapAdministerStructure))`.

- [ ] **Step 5: Run the tests**

Run: `go test -tags integration ./internal/catalog/ -run TestSetSelectionRule -count=1 && go build ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add sql/queries/catalog.sql internal/database/sqlc internal/catalog
git commit -m "feat(catalog): change a modifier group's selection rule atomically

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 9: Replace-set item and category modifier groups (commands 7 and 8)

**Files:**
- Modify: `sql/queries/catalog.sql`, `make sqlc`
- Create: `internal/catalog/assignment_sets.go`
- Modify: `internal/catalog/dto.go`, `internal/catalog/http_structure.go`, `internal/catalog/routes.go`
- Test: `internal/catalog/assignment_sets_integration_test.go`

**Interfaces:**
- Consumes: `NormalizeIDSet`, `DiffIDSets`, `UnionIDs`, `ContainsID` (Task 2); test helpers from Tasks 4 and 6.
- Produces:
  - `type ExclusionRef struct { ItemID, ModifierGroupID uuid.UUID }`, `type IDSetChange struct { Added, Removed []uuid.UUID }`
  - `type ReplaceItemModifierGroupsCommand struct { RequestID, ItemID uuid.UUID; DirectGroupIDs, ExcludedGroupIDs []uuid.UUID }` → `ItemModifierGroupsResponse{ItemID, DirectGroupIDs, ExcludedGroupIDs}`, `NewReplaceItemModifierGroupsHandler(r)`
  - `type ReplaceCategoryModifierGroupsCommand struct { RequestID, CategoryID uuid.UUID; GroupIDs []uuid.UUID }` → `CategoryModifierGroupsResponse{CategoryID, GroupIDs, RemovedExclusions []ExclusionRef}`, `NewReplaceCategoryModifierGroupsHandler(r)`
  - `func lockGroupsForSet(ctx, q, current, desired []uuid.UUID) error` (package-private)
  - `func deleteCategoryGroupExclusions(ctx, q, categoryID, groupID uuid.UUID) ([]ExclusionRef, error)` and `func sortExclusionRefs([]ExclusionRef)` (package-private, reused by Task 10)
  - sqlc: `ListItemDirectGroupIDs`, `ListItemExcludedGroupIDs`, `ListCategoryGroupIDs`, `LockModifierGroupsByIDs`, `DeleteItemModifierGroup`, `DeleteItemModifierGroupExclusion`, `DeleteCategoryModifierGroup`, `DeleteCategoryGroupExclusions`

- [ ] **Step 1: Add the queries**

```sql
-- -- Replace-set Assignments (BA-1, ADR-059) --

-- name: ListItemDirectGroupIDs :many
SELECT modifier_group_id FROM item_modifier_groups
WHERE menu_item_id = $1 ORDER BY modifier_group_id;

-- name: ListItemExcludedGroupIDs :many
SELECT modifier_group_id FROM item_modifier_group_exclusions
WHERE menu_item_id = $1 ORDER BY modifier_group_id;

-- name: ListCategoryGroupIDs :many
SELECT modifier_group_id FROM category_modifier_groups
WHERE menu_category_id = $1 ORDER BY modifier_group_id;

-- name: LockModifierGroupsByIDs :many
SELECT id, retired_at FROM modifier_groups
WHERE id = ANY(sqlc.arg(ids)::uuid[])
ORDER BY id
FOR UPDATE;

-- name: DeleteItemModifierGroup :exec
DELETE FROM item_modifier_groups WHERE menu_item_id = $1 AND modifier_group_id = $2;

-- name: DeleteItemModifierGroupExclusion :exec
DELETE FROM item_modifier_group_exclusions WHERE menu_item_id = $1 AND modifier_group_id = $2;

-- name: DeleteCategoryModifierGroup :exec
DELETE FROM category_modifier_groups WHERE menu_category_id = $1 AND modifier_group_id = $2;

-- name: DeleteCategoryGroupExclusions :many
DELETE FROM item_modifier_group_exclusions e
USING menu_items i
WHERE e.menu_item_id = i.id
  AND i.category_id = sqlc.arg(category_id)
  AND e.modifier_group_id = sqlc.arg(group_id)
RETURNING e.menu_item_id;
```

Run: `make sqlc`.

- [ ] **Step 2: Write the failing tests**

Create `internal/catalog/assignment_sets_integration_test.go`:

```go
//go:build integration

package catalog_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/catalog"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	directGroupsOfItem   = `SELECT modifier_group_id FROM item_modifier_groups WHERE menu_item_id = $1 ORDER BY modifier_group_id`
	groupsOfCategory     = `SELECT modifier_group_id FROM category_modifier_groups WHERE menu_category_id = $1 ORDER BY modifier_group_id`
)

func TestReplaceItemModifierGroups(t *testing.T) {
	db, q := openExecutorTestDB(t)
	handler := catalog.NewReplaceItemModifierGroupsHandler(catalog.NewRunner(db, q))
	ctx := context.Background()
	price := int64(30000)

	t.Run("replaces direct and excluded sets in one audit event", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		cat := createTestCategoryDirect(t, db, "Coffee")
		sugar := createTestModifierGroupDirect(t, db, "Sugar", 0, 1, false)
		ice := createTestModifierGroupDirect(t, db, "Ice", 0, 1, false)
		topping := createTestModifierGroupDirect(t, db, "Topping", 0, 1, false)
		cheese := createTestModifierGroupDirect(t, db, "Cheese", 0, 1, false)
		attachCategoryGroupDirect(t, db, cat, sugar)
		attachCategoryGroupDirect(t, db, cat, ice)
		item := createTestItemDirect(t, db, cat, "Latte", &price, false)
		attachItemGroupDirect(t, db, item, topping)
		excludeItemGroupDirect(t, db, item, sugar)

		status, res, err := handler.Handle(ctx, managerActor(t, db, q), catalog.ReplaceItemModifierGroupsCommand{
			RequestID: uuid.New(), ItemID: item, DirectGroupIDs: []uuid.UUID{cheese}, ExcludedGroupIDs: []uuid.UUID{ice},
		})
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.Equal(t, []uuid.UUID{cheese}, res.DirectGroupIDs)
		assert.Equal(t, []uuid.UUID{cheese}, idsFrom(t, db, directGroupsOfItem, item))
		assert.Equal(t, []uuid.UUID{ice}, idsFrom(t, db, exclusionsOfItem, item))
		assert.Equal(t, 1, auditCount(t, db, catalog.EventItemModifierGroupsReplaced))
	})

	t.Run("an identical set is a no-op without audit", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		cat := createTestCategoryDirect(t, db, "Coffee")
		topping := createTestModifierGroupDirect(t, db, "Topping", 0, 1, false)
		item := createTestItemDirect(t, db, cat, "Latte", &price, false)
		attachItemGroupDirect(t, db, item, topping)
		_, _, err := handler.Handle(ctx, managerActor(t, db, q), catalog.ReplaceItemModifierGroupsCommand{
			RequestID: uuid.New(), ItemID: item, DirectGroupIDs: []uuid.UUID{topping},
		})
		require.NoError(t, err)
		assert.Equal(t, 0, auditCount(t, db, catalog.EventItemModifierGroupsReplaced))
	})

	t.Run("excluding a group the category does not provide is INVALID_INHERITANCE", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		cat := createTestCategoryDirect(t, db, "Coffee")
		other := createTestModifierGroupDirect(t, db, "Other", 0, 1, false)
		item := createTestItemDirect(t, db, cat, "Latte", &price, false)
		_, _, err := handler.Handle(ctx, managerActor(t, db, q), catalog.ReplaceItemModifierGroupsCommand{
			RequestID: uuid.New(), ItemID: item, ExcludedGroupIDs: []uuid.UUID{other},
		})
		assert.True(t, errors.Is(err, catalog.ErrInvalidInheritance))
	})

	t.Run("a group both direct and excluded is INVALID_INHERITANCE", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		cat := createTestCategoryDirect(t, db, "Coffee")
		sugar := createTestModifierGroupDirect(t, db, "Sugar", 0, 1, false)
		attachCategoryGroupDirect(t, db, cat, sugar)
		item := createTestItemDirect(t, db, cat, "Latte", &price, false)
		_, _, err := handler.Handle(ctx, managerActor(t, db, q), catalog.ReplaceItemModifierGroupsCommand{
			RequestID: uuid.New(), ItemID: item, DirectGroupIDs: []uuid.UUID{sugar}, ExcludedGroupIDs: []uuid.UUID{sugar},
		})
		assert.True(t, errors.Is(err, catalog.ErrInvalidInheritance))
	})

	t.Run("adding a retired group is ErrEntityRetired; keeping one is fine", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		cat := createTestCategoryDirect(t, db, "Coffee")
		retired := createTestModifierGroupDirect(t, db, "Old", 0, 1, true)
		kept := createTestModifierGroupDirect(t, db, "Kept", 0, 1, true)
		item := createTestItemDirect(t, db, cat, "Latte", &price, false)
		attachItemGroupDirect(t, db, item, kept)

		_, _, err := handler.Handle(ctx, managerActor(t, db, q), catalog.ReplaceItemModifierGroupsCommand{
			RequestID: uuid.New(), ItemID: item, DirectGroupIDs: []uuid.UUID{kept, retired},
		})
		assert.True(t, errors.Is(err, catalog.ErrEntityRetired))

		_, _, err = handler.Handle(ctx, managerActor(t, db, q), catalog.ReplaceItemModifierGroupsCommand{
			RequestID: uuid.New(), ItemID: item, DirectGroupIDs: []uuid.UUID{kept},
		})
		require.NoError(t, err)
	})

	t.Run("unknown group is ErrNotFound; duplicates are INVALID_INPUT", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		cat := createTestCategoryDirect(t, db, "Coffee")
		g := createTestModifierGroupDirect(t, db, "G", 0, 1, false)
		item := createTestItemDirect(t, db, cat, "Latte", &price, false)
		_, _, err := handler.Handle(ctx, managerActor(t, db, q), catalog.ReplaceItemModifierGroupsCommand{
			RequestID: uuid.New(), ItemID: item, DirectGroupIDs: []uuid.UUID{uuid.New()},
		})
		assert.True(t, errors.Is(err, catalog.ErrNotFound))
		_, _, err = handler.Handle(ctx, managerActor(t, db, q), catalog.ReplaceItemModifierGroupsCommand{
			RequestID: uuid.New(), ItemID: item, DirectGroupIDs: []uuid.UUID{g, g},
		})
		assert.Error(t, err)
	})
}

func TestReplaceCategoryModifierGroups(t *testing.T) {
	db, q := openExecutorTestDB(t)
	handler := catalog.NewReplaceCategoryModifierGroupsHandler(catalog.NewRunner(db, q))
	ctx := context.Background()
	price := int64(30000)

	t.Run("removing a group deletes its exclusions in the category", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		cat := createTestCategoryDirect(t, db, "Coffee")
		sugar := createTestModifierGroupDirect(t, db, "Sugar", 0, 1, false)
		ice := createTestModifierGroupDirect(t, db, "Ice", 0, 1, false)
		topping := createTestModifierGroupDirect(t, db, "Topping", 0, 1, false)
		attachCategoryGroupDirect(t, db, cat, sugar)
		attachCategoryGroupDirect(t, db, cat, ice)
		latte := createTestItemDirect(t, db, cat, "Latte", &price, false)
		mocha := createTestItemDirect(t, db, cat, "Mocha", &price, false)
		excludeItemGroupDirect(t, db, latte, sugar)
		excludeItemGroupDirect(t, db, mocha, ice)

		status, res, err := handler.Handle(ctx, managerActor(t, db, q), catalog.ReplaceCategoryModifierGroupsCommand{
			RequestID: uuid.New(), CategoryID: cat, GroupIDs: []uuid.UUID{ice, topping},
		})
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.ElementsMatch(t, []uuid.UUID{ice, topping}, idsFrom(t, db, groupsOfCategory, cat))
		assert.Equal(t, []catalog.ExclusionRef{{ItemID: latte, ModifierGroupID: sugar}}, res.RemovedExclusions)
		assert.Empty(t, idsFrom(t, db, exclusionsOfItem, latte))
		assert.Equal(t, []uuid.UUID{ice}, idsFrom(t, db, exclusionsOfItem, mocha), "exclusions of kept groups stay")
		assert.Equal(t, 1, auditCount(t, db, catalog.EventCategoryModifierGroupsReplaced))
	})

	t.Run("retired category is ErrEntityRetired", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		cat := createTestCategoryDirect(t, db, "Coffee")
		retireCategoryDirect(t, db, cat)
		_, _, err := handler.Handle(ctx, managerActor(t, db, q), catalog.ReplaceCategoryModifierGroupsCommand{RequestID: uuid.New(), CategoryID: cat})
		assert.True(t, errors.Is(err, catalog.ErrEntityRetired))
	})
}
```

Run: `go test -tags integration ./internal/catalog/ -run 'TestReplaceItemModifierGroups|TestReplaceCategoryModifierGroups' -count=1`
Expected: compile FAIL.

- [ ] **Step 3: DTOs**

Append to `dto.go`:

```go
// === Replace-set Assignments (BA-1) ===

// ExclusionRef names one item exclusion of a modifier group.
type ExclusionRef struct {
	ItemID          uuid.UUID `json:"item_id"`
	ModifierGroupID uuid.UUID `json:"modifier_group_id"`
}

// IDSetChange lists what a replace-set command added and removed.
type IDSetChange struct {
	Added   []uuid.UUID `json:"added"`
	Removed []uuid.UUID `json:"removed"`
}

// ReplaceItemModifierGroupsRequest replaces an item's direct and excluded groups.
type ReplaceItemModifierGroupsRequest struct {
	RequestID        uuid.UUID   `json:"request_id"`
	DirectGroupIDs   []uuid.UUID `json:"direct_group_ids"`
	ExcludedGroupIDs []uuid.UUID `json:"excluded_group_ids"`
}

// ReplaceItemModifierGroupsCommand carries the parameters for command 7.
type ReplaceItemModifierGroupsCommand struct {
	RequestID        uuid.UUID
	ItemID           uuid.UUID
	DirectGroupIDs   []uuid.UUID
	ExcludedGroupIDs []uuid.UUID
}

// ItemModifierGroupsResponse is an item's direct and excluded groups.
type ItemModifierGroupsResponse struct {
	ItemID           uuid.UUID   `json:"item_id"`
	DirectGroupIDs   []uuid.UUID `json:"direct_group_ids"`
	ExcludedGroupIDs []uuid.UUID `json:"excluded_group_ids"`
}

// ReplaceCategoryModifierGroupsRequest replaces a category's groups.
type ReplaceCategoryModifierGroupsRequest struct {
	RequestID uuid.UUID   `json:"request_id"`
	GroupIDs  []uuid.UUID `json:"group_ids"`
}

// ReplaceCategoryModifierGroupsCommand carries the parameters for command 8.
type ReplaceCategoryModifierGroupsCommand struct {
	RequestID  uuid.UUID
	CategoryID uuid.UUID
	GroupIDs   []uuid.UUID
}

// CategoryModifierGroupsResponse is a category's groups and the exclusions the change dropped.
type CategoryModifierGroupsResponse struct {
	CategoryID        uuid.UUID      `json:"category_id"`
	GroupIDs          []uuid.UUID    `json:"group_ids"`
	RemovedExclusions []ExclusionRef `json:"removed_exclusions"`
}
```

- [ ] **Step 4: Implement**

Create `internal/catalog/assignment_sets.go`:

```go
package catalog

import (
	"context"
	"fmt"
	"sort"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
)

// lockGroupsForSet locks every group in current ∪ desired in id order and
// rejects retired groups the command would add.
func lockGroupsForSet(ctx context.Context, q *sqlc.Queries, current, desired []uuid.UUID) error {
	all := UnionIDs(current, desired)
	if len(all) == 0 {
		return nil
	}
	rows, err := q.LockModifierGroupsByIDs(ctx, all)
	if err != nil {
		return MapDBError(err)
	}
	if len(rows) != len(all) {
		return fmt.Errorf("%w: modifier group not found", ErrNotFound)
	}
	added, _ := DiffIDSets(current, desired)
	for _, r := range rows {
		if r.RetiredAt.Valid && ContainsID(added, r.ID) {
			return fmt.Errorf("%w: modifier group %s is retired", ErrEntityRetired, r.ID)
		}
	}
	return nil
}

func sortExclusionRefs(refs []ExclusionRef) {
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].ModifierGroupID != refs[j].ModifierGroupID {
			return refs[i].ModifierGroupID.String() < refs[j].ModifierGroupID.String()
		}
		return refs[i].ItemID.String() < refs[j].ItemID.String()
	})
}

// deleteCategoryGroupExclusions enforces ADR-059 when a category stops providing a group.
func deleteCategoryGroupExclusions(ctx context.Context, q *sqlc.Queries, categoryID, groupID uuid.UUID) ([]ExclusionRef, error) {
	itemIDs, err := q.DeleteCategoryGroupExclusions(ctx, sqlc.DeleteCategoryGroupExclusionsParams{CategoryID: categoryID, GroupID: groupID})
	if err != nil {
		return nil, MapDBError(err)
	}
	refs := make([]ExclusionRef, 0, len(itemIDs))
	for _, id := range itemIDs {
		refs = append(refs, ExclusionRef{ItemID: id, ModifierGroupID: groupID})
	}
	return refs, nil
}

// === Command 7 ===

type replaceItemGroupsFingerprint struct {
	ItemID   uuid.UUID   `json:"item_id"`
	Direct   []uuid.UUID `json:"direct_group_ids"`
	Excluded []uuid.UUID `json:"excluded_group_ids"`
}

type itemModifierGroupsReplacedAudit struct {
	ItemID   uuid.UUID   `json:"item_id"`
	Direct   IDSetChange `json:"direct"`
	Excluded IDSetChange `json:"excluded"`
}

// ReplaceItemModifierGroupsHandler replaces an item's direct and excluded groups.
type ReplaceItemModifierGroupsHandler struct {
	runner *Runner
}

// NewReplaceItemModifierGroupsHandler creates a ReplaceItemModifierGroupsHandler.
func NewReplaceItemModifierGroupsHandler(runner *Runner) *ReplaceItemModifierGroupsHandler {
	return &ReplaceItemModifierGroupsHandler{runner: runner}
}

// Handle locks the item, then its groups in id order (the same order as attach).
func (h *ReplaceItemModifierGroupsHandler) Handle(ctx context.Context, actor Actor, cmd ReplaceItemModifierGroupsCommand) (int, ItemModifierGroupsResponse, error) {
	direct, err := NormalizeIDSet(cmd.DirectGroupIDs, "direct_group_ids")
	if err != nil {
		return 0, ItemModifierGroupsResponse{}, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error())
	}
	excluded, err := NormalizeIDSet(cmd.ExcludedGroupIDs, "excluded_group_ids")
	if err != nil {
		return 0, ItemModifierGroupsResponse{}, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error())
	}
	for _, id := range direct {
		if ContainsID(excluded, id) {
			return 0, ItemModifierGroupsResponse{}, fmt.Errorf("%w: group %s cannot be both direct and excluded", ErrInvalidInheritance, id)
		}
	}

	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpItemReplaceModifierGroups,
		Fingerprint: replaceItemGroupsFingerprint{ItemID: cmd.ItemID, Direct: direct, Excluded: excluded},
		Required:    []string{CapAdministerStructure},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec, func(q *sqlc.Queries) (int, ItemModifierGroupsResponse, AuditRecord, error) {
		item, err := q.GetMenuItemForUpdate(ctx, cmd.ItemID)
		if err != nil {
			return 0, ItemModifierGroupsResponse{}, AuditRecord{}, MapDBError(err)
		}
		if item.RetiredAt.Valid {
			return 0, ItemModifierGroupsResponse{}, AuditRecord{}, fmt.Errorf("%w: item is retired", ErrEntityRetired)
		}

		curDirect, err := q.ListItemDirectGroupIDs(ctx, cmd.ItemID)
		if err != nil {
			return 0, ItemModifierGroupsResponse{}, AuditRecord{}, MapDBError(err)
		}
		curExcluded, err := q.ListItemExcludedGroupIDs(ctx, cmd.ItemID)
		if err != nil {
			return 0, ItemModifierGroupsResponse{}, AuditRecord{}, MapDBError(err)
		}
		if err := lockGroupsForSet(ctx, q, UnionIDs(curDirect, curExcluded), UnionIDs(direct, excluded)); err != nil {
			return 0, ItemModifierGroupsResponse{}, AuditRecord{}, err
		}

		provided, err := q.ListCategoryGroupIDs(ctx, item.CategoryID)
		if err != nil {
			return 0, ItemModifierGroupsResponse{}, AuditRecord{}, MapDBError(err)
		}
		for _, id := range excluded {
			if !ContainsID(provided, id) {
				return 0, ItemModifierGroupsResponse{}, AuditRecord{}, fmt.Errorf("%w: group %s is not provided by the item's category", ErrInvalidInheritance, id)
			}
		}

		dAdd, dRem := DiffIDSets(curDirect, direct)
		eAdd, eRem := DiffIDSets(curExcluded, excluded)
		for _, g := range dRem {
			if err := q.DeleteItemModifierGroup(ctx, sqlc.DeleteItemModifierGroupParams{MenuItemID: cmd.ItemID, ModifierGroupID: g}); err != nil {
				return 0, ItemModifierGroupsResponse{}, AuditRecord{}, MapDBError(err)
			}
		}
		for _, g := range eRem {
			if err := q.DeleteItemModifierGroupExclusion(ctx, sqlc.DeleteItemModifierGroupExclusionParams{MenuItemID: cmd.ItemID, ModifierGroupID: g}); err != nil {
				return 0, ItemModifierGroupsResponse{}, AuditRecord{}, MapDBError(err)
			}
		}
		for _, g := range dAdd {
			if err := q.CreateItemModifierGroup(ctx, sqlc.CreateItemModifierGroupParams{MenuItemID: cmd.ItemID, ModifierGroupID: g}); err != nil {
				return 0, ItemModifierGroupsResponse{}, AuditRecord{}, MapDBError(err)
			}
		}
		for _, g := range eAdd {
			if err := q.CreateItemModifierGroupExclusion(ctx, sqlc.CreateItemModifierGroupExclusionParams{MenuItemID: cmd.ItemID, ModifierGroupID: g}); err != nil {
				return 0, ItemModifierGroupsResponse{}, AuditRecord{}, MapDBError(err)
			}
		}

		res := ItemModifierGroupsResponse{ItemID: cmd.ItemID, DirectGroupIDs: direct, ExcludedGroupIDs: excluded}
		if len(dAdd)+len(dRem)+len(eAdd)+len(eRem) == 0 {
			return 200, res, AuditRecord{}, nil
		}
		audit := AuditRecord{
			EventType: EventItemModifierGroupsReplaced,
			Details: itemModifierGroupsReplacedAudit{
				ItemID:   cmd.ItemID,
				Direct:   IDSetChange{Added: dAdd, Removed: dRem},
				Excluded: IDSetChange{Added: eAdd, Removed: eRem},
			},
		}
		return 200, res, audit, nil
	})
}

// === Command 8 ===

type replaceCategoryGroupsFingerprint struct {
	CategoryID uuid.UUID   `json:"category_id"`
	GroupIDs   []uuid.UUID `json:"group_ids"`
}

type categoryModifierGroupsReplacedAudit struct {
	CategoryID        uuid.UUID      `json:"category_id"`
	Added             []uuid.UUID    `json:"added"`
	Removed           []uuid.UUID    `json:"removed"`
	RemovedExclusions []ExclusionRef `json:"removed_exclusions"`
}

// ReplaceCategoryModifierGroupsHandler replaces the groups a category provides.
type ReplaceCategoryModifierGroupsHandler struct {
	runner *Runner
}

// NewReplaceCategoryModifierGroupsHandler creates a ReplaceCategoryModifierGroupsHandler.
func NewReplaceCategoryModifierGroupsHandler(runner *Runner) *ReplaceCategoryModifierGroupsHandler {
	return &ReplaceCategoryModifierGroupsHandler{runner: runner}
}

// Handle locks the category, then its groups in id order.
func (h *ReplaceCategoryModifierGroupsHandler) Handle(ctx context.Context, actor Actor, cmd ReplaceCategoryModifierGroupsCommand) (int, CategoryModifierGroupsResponse, error) {
	groups, err := NormalizeIDSet(cmd.GroupIDs, "group_ids")
	if err != nil {
		return 0, CategoryModifierGroupsResponse{}, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error())
	}
	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpCategoryReplaceModifierGroups,
		Fingerprint: replaceCategoryGroupsFingerprint{CategoryID: cmd.CategoryID, GroupIDs: groups},
		Required:    []string{CapAdministerStructure},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec, func(q *sqlc.Queries) (int, CategoryModifierGroupsResponse, AuditRecord, error) {
		cat, err := q.GetMenuCategoryForUpdate(ctx, cmd.CategoryID)
		if err != nil {
			return 0, CategoryModifierGroupsResponse{}, AuditRecord{}, MapDBError(err)
		}
		if cat.RetiredAt.Valid {
			return 0, CategoryModifierGroupsResponse{}, AuditRecord{}, fmt.Errorf("%w: category is retired", ErrEntityRetired)
		}
		current, err := q.ListCategoryGroupIDs(ctx, cmd.CategoryID)
		if err != nil {
			return 0, CategoryModifierGroupsResponse{}, AuditRecord{}, MapDBError(err)
		}
		if err := lockGroupsForSet(ctx, q, current, groups); err != nil {
			return 0, CategoryModifierGroupsResponse{}, AuditRecord{}, err
		}

		added, removed := DiffIDSets(current, groups)
		removedExcl := []ExclusionRef{}
		for _, g := range removed {
			if err := q.DeleteCategoryModifierGroup(ctx, sqlc.DeleteCategoryModifierGroupParams{MenuCategoryID: cmd.CategoryID, ModifierGroupID: g}); err != nil {
				return 0, CategoryModifierGroupsResponse{}, AuditRecord{}, MapDBError(err)
			}
			refs, err := deleteCategoryGroupExclusions(ctx, q, cmd.CategoryID, g)
			if err != nil {
				return 0, CategoryModifierGroupsResponse{}, AuditRecord{}, err
			}
			removedExcl = append(removedExcl, refs...)
		}
		for _, g := range added {
			if err := q.CreateCategoryModifierGroup(ctx, sqlc.CreateCategoryModifierGroupParams{MenuCategoryID: cmd.CategoryID, ModifierGroupID: g}); err != nil {
				return 0, CategoryModifierGroupsResponse{}, AuditRecord{}, MapDBError(err)
			}
		}
		sortExclusionRefs(removedExcl)

		res := CategoryModifierGroupsResponse{CategoryID: cmd.CategoryID, GroupIDs: groups, RemovedExclusions: removedExcl}
		if len(added)+len(removed) == 0 {
			return 200, res, AuditRecord{}, nil
		}
		audit := AuditRecord{
			EventType: EventCategoryModifierGroupsReplaced,
			Details: categoryModifierGroupsReplacedAudit{
				CategoryID: cmd.CategoryID, Added: added, Removed: removed, RemovedExclusions: removedExcl,
			},
		}
		return 200, res, audit, nil
	})
}
```

- [ ] **Step 5: HTTP handlers and routes**

Append to `http_structure.go`:

```go
// handleReplaceItemModifierGroups godoc
//
//	@Summary		Thay toàn bộ nhóm topping của món
//	@Description	Đặt đúng tập nhóm gán trực tiếp và tập nhóm loại trừ của món. Bỏ một id khỏi danh sách nghĩa là gỡ nó. Yêu cầu quyền catalog.administer_structure.
//	@Tags			Catalog
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			item_id	path		string								true	"Item ID (UUID)"
//	@Param			request	body		ReplaceItemModifierGroupsRequest	true	"Tập nhóm mong muốn"
//	@Success		200		{object}	response.APIResponse{data=ItemModifierGroupsResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Failure		500		{object}	response.APIResponse
//	@Router			/catalog/items/{item_id}/modifier-groups [put]
func (s *Slices) handleReplaceItemModifierGroups(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	itemID, err := parseUUIDParam(c, "item_id")
	if err != nil {
		return sendError(c, err)
	}
	req, err := bindBody[ReplaceItemModifierGroupsRequest](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(req.RequestID); err != nil {
		return sendError(c, err)
	}
	status, res, err := s.ReplaceItemModifierGroups.Handle(c.Request().Context(), actor, ReplaceItemModifierGroupsCommand{
		RequestID: req.RequestID, ItemID: itemID, DirectGroupIDs: req.DirectGroupIDs, ExcludedGroupIDs: req.ExcludedGroupIDs,
	})
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
}

// handleReplaceCategoryModifierGroups godoc
//
//	@Summary		Thay toàn bộ nhóm topping của danh mục
//	@Description	Đặt đúng tập nhóm danh mục cung cấp. Gỡ một nhóm sẽ xóa các loại trừ nhóm đó trên các món trong danh mục. Yêu cầu quyền catalog.administer_structure.
//	@Tags			Catalog
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			category_id	path		string									true	"Category ID (UUID)"
//	@Param			request		body		ReplaceCategoryModifierGroupsRequest	true	"Tập nhóm mong muốn"
//	@Success		200			{object}	response.APIResponse{data=CategoryModifierGroupsResponse}
//	@Failure		400			{object}	response.APIResponse
//	@Failure		401			{object}	response.APIResponse
//	@Failure		403			{object}	response.APIResponse
//	@Failure		404			{object}	response.APIResponse
//	@Failure		409			{object}	response.APIResponse
//	@Failure		500			{object}	response.APIResponse
//	@Router			/catalog/categories/{category_id}/modifier-groups [put]
func (s *Slices) handleReplaceCategoryModifierGroups(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	catID, err := parseUUIDParam(c, "category_id")
	if err != nil {
		return sendError(c, err)
	}
	req, err := bindBody[ReplaceCategoryModifierGroupsRequest](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(req.RequestID); err != nil {
		return sendError(c, err)
	}
	status, res, err := s.ReplaceCategoryModifierGroups.Handle(c.Request().Context(), actor, ReplaceCategoryModifierGroupsCommand{
		RequestID: req.RequestID, CategoryID: catID, GroupIDs: req.GroupIDs,
	})
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
}
```

`routes.go`: fields and constructors for both handlers, then:

```go
	catalog.PUT("/items/:item_id/modifier-groups", s.handleReplaceItemModifierGroups, authn.RequireCapability(CapAdministerStructure))
	catalog.PUT("/categories/:category_id/modifier-groups", s.handleReplaceCategoryModifierGroups, authn.RequireCapability(CapAdministerStructure))
```

These are `PUT` on the collection path; the existing `POST /items/:item_id/modifier-groups/:group_id` routes stay untouched.

- [ ] **Step 6: Run the tests**

Run: `go test -tags integration ./internal/catalog/ -run 'TestReplaceItemModifierGroups|TestReplaceCategoryModifierGroups' -count=1 && go build ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add sql/queries/catalog.sql internal/database/sqlc internal/catalog
git commit -m "feat(catalog): replace-set item and category modifier groups

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 10: Replace a group's assignments for the Batch Linker (command 9)

**Files:**
- Modify: `sql/queries/catalog.sql`, `make sqlc`
- Modify: `internal/catalog/assignment_sets.go`, `internal/catalog/dto.go`, `internal/catalog/http_structure.go`, `internal/catalog/routes.go`
- Test: `internal/catalog/assignment_sets_integration_test.go`

**Interfaces:**
- Consumes: `lockGroupsForSet` is not used here (the group is locked first, alone); `deleteCategoryGroupExclusions`, `sortExclusionRefs`, `ExclusionRef`, `IDSetChange` (Task 9).
- Produces: `type ReplaceGroupAssignmentsCommand struct { RequestID, GroupID uuid.UUID; ItemIDs, CategoryIDs []uuid.UUID }` → `ModifierGroupAssignmentsResponse{GroupID, ItemIDs, CategoryIDs, RemovedExclusions}`, `NewReplaceGroupAssignmentsHandler(r)`.

- [ ] **Step 1: Add the queries**

```sql
-- name: ListItemIDsWithDirectGroup :many
SELECT menu_item_id FROM item_modifier_groups
WHERE modifier_group_id = $1 ORDER BY menu_item_id;

-- name: ListCategoryIDsWithGroup :many
SELECT menu_category_id FROM category_modifier_groups
WHERE modifier_group_id = $1 ORDER BY menu_category_id;

-- name: ListItemIDsExcludingGroup :many
SELECT menu_item_id FROM item_modifier_group_exclusions
WHERE modifier_group_id = $1 ORDER BY menu_item_id;

-- name: LockMenuItemsByIDs :many
SELECT id, retired_at FROM menu_items
WHERE id = ANY(sqlc.arg(ids)::uuid[])
ORDER BY id
FOR UPDATE;

-- name: LockMenuCategoriesByIDs :many
SELECT id, retired_at FROM menu_categories
WHERE id = ANY(sqlc.arg(ids)::uuid[])
ORDER BY id
FOR UPDATE;
```

Run: `make sqlc`.

- [ ] **Step 2: Write the failing test**

Append to `assignment_sets_integration_test.go`:

```go
func TestReplaceGroupAssignments(t *testing.T) {
	db, q := openExecutorTestDB(t)
	handler := catalog.NewReplaceGroupAssignmentsHandler(catalog.NewRunner(db, q))
	ctx := context.Background()
	price := int64(30000)
	const itemsWithGroup = `SELECT menu_item_id FROM item_modifier_groups WHERE modifier_group_id = $1 ORDER BY menu_item_id`
	const categoriesWithGroup = `SELECT menu_category_id FROM category_modifier_groups WHERE modifier_group_id = $1 ORDER BY menu_category_id`

	t.Run("sets exactly the items and categories, one audit event", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		coffee := createTestCategoryDirect(t, db, "Coffee")
		tea := createTestCategoryDirect(t, db, "Tea")
		topping := createTestModifierGroupDirect(t, db, "Topping", 0, 3, false)
		latte := createTestItemDirect(t, db, coffee, "Latte", &price, false)
		mocha := createTestItemDirect(t, db, coffee, "Mocha", &price, false)
		peach := createTestItemDirect(t, db, tea, "Peach tea", &price, false)
		attachItemGroupDirect(t, db, latte, topping)
		attachCategoryGroupDirect(t, db, coffee, topping)
		excludeItemGroupDirect(t, db, mocha, topping)

		status, res, err := handler.Handle(ctx, managerActor(t, db, q), catalog.ReplaceGroupAssignmentsCommand{
			RequestID: uuid.New(), GroupID: topping, ItemIDs: []uuid.UUID{peach}, CategoryIDs: []uuid.UUID{tea},
		})
		require.NoError(t, err)
		assert.Equal(t, 200, status)
		assert.Equal(t, []uuid.UUID{peach}, idsFrom(t, db, itemsWithGroup, topping))
		assert.Equal(t, []uuid.UUID{tea}, idsFrom(t, db, categoriesWithGroup, topping))
		assert.Equal(t, []catalog.ExclusionRef{{ItemID: mocha, ModifierGroupID: topping}}, res.RemovedExclusions,
			"coffee no longer provides the group, so mocha's exclusion goes")
		assert.Equal(t, 1, auditCount(t, db, catalog.EventModifierGroupAssignmentsReplaced))
	})

	t.Run("adding an item that excludes the group is INVALID_INHERITANCE", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		coffee := createTestCategoryDirect(t, db, "Coffee")
		sugar := createTestModifierGroupDirect(t, db, "Sugar", 0, 1, false)
		attachCategoryGroupDirect(t, db, coffee, sugar)
		latte := createTestItemDirect(t, db, coffee, "Latte", &price, false)
		excludeItemGroupDirect(t, db, latte, sugar)
		_, _, err := handler.Handle(ctx, managerActor(t, db, q), catalog.ReplaceGroupAssignmentsCommand{
			RequestID: uuid.New(), GroupID: sugar, ItemIDs: []uuid.UUID{latte}, CategoryIDs: []uuid.UUID{coffee},
		})
		assert.True(t, errors.Is(err, catalog.ErrInvalidInheritance))
	})

	t.Run("adding a retired item is ErrEntityRetired; unknown is ErrNotFound", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		coffee := createTestCategoryDirect(t, db, "Coffee")
		g := createTestModifierGroupDirect(t, db, "G", 0, 1, false)
		retired := createTestItemDirect(t, db, coffee, "Old", &price, true)
		_, _, err := handler.Handle(ctx, managerActor(t, db, q), catalog.ReplaceGroupAssignmentsCommand{
			RequestID: uuid.New(), GroupID: g, ItemIDs: []uuid.UUID{retired},
		})
		assert.True(t, errors.Is(err, catalog.ErrEntityRetired))
		_, _, err = handler.Handle(ctx, managerActor(t, db, q), catalog.ReplaceGroupAssignmentsCommand{
			RequestID: uuid.New(), GroupID: g, CategoryIDs: []uuid.UUID{uuid.New()},
		})
		assert.True(t, errors.Is(err, catalog.ErrNotFound))
	})

	t.Run("retired group is ErrEntityRetired", func(t *testing.T) {
		cleanCategoryTestTables(t, db)
		g := createTestModifierGroupDirect(t, db, "Old", 0, 1, true)
		_, _, err := handler.Handle(ctx, managerActor(t, db, q), catalog.ReplaceGroupAssignmentsCommand{RequestID: uuid.New(), GroupID: g})
		assert.True(t, errors.Is(err, catalog.ErrEntityRetired))
	})
}
```

Run: `go test -tags integration ./internal/catalog/ -run TestReplaceGroupAssignments -count=1`
Expected: compile FAIL.

- [ ] **Step 3: DTOs**

Append to `dto.go`:

```go
// ReplaceGroupAssignmentsRequest sets exactly which items and categories a group is attached to.
type ReplaceGroupAssignmentsRequest struct {
	RequestID   uuid.UUID   `json:"request_id"`
	ItemIDs     []uuid.UUID `json:"item_ids"`
	CategoryIDs []uuid.UUID `json:"category_ids"`
}

// ReplaceGroupAssignmentsCommand carries the parameters for command 9.
type ReplaceGroupAssignmentsCommand struct {
	RequestID   uuid.UUID
	GroupID     uuid.UUID
	ItemIDs     []uuid.UUID
	CategoryIDs []uuid.UUID
}

// ModifierGroupAssignmentsResponse is where a group is directly attached.
type ModifierGroupAssignmentsResponse struct {
	GroupID           uuid.UUID      `json:"group_id"`
	ItemIDs           []uuid.UUID    `json:"item_ids"`
	CategoryIDs       []uuid.UUID    `json:"category_ids"`
	RemovedExclusions []ExclusionRef `json:"removed_exclusions"`
}
```

- [ ] **Step 4: Implement**

Append to `assignment_sets.go`:

```go
// === Command 9 ===

type replaceGroupAssignmentsFingerprint struct {
	GroupID     uuid.UUID   `json:"group_id"`
	ItemIDs     []uuid.UUID `json:"item_ids"`
	CategoryIDs []uuid.UUID `json:"category_ids"`
}

type modifierGroupAssignmentsReplacedAudit struct {
	GroupID           uuid.UUID      `json:"group_id"`
	Items             IDSetChange    `json:"items"`
	Categories        IDSetChange    `json:"categories"`
	RemovedExclusions []ExclusionRef `json:"removed_exclusions"`
}

// ReplaceGroupAssignmentsHandler sets exactly which items and categories a
// group is directly attached to. It serves the 9b Batch Linker.
type ReplaceGroupAssignmentsHandler struct {
	runner *Runner
}

// NewReplaceGroupAssignmentsHandler creates a ReplaceGroupAssignmentsHandler.
func NewReplaceGroupAssignmentsHandler(runner *Runner) *ReplaceGroupAssignmentsHandler {
	return &ReplaceGroupAssignmentsHandler{runner: runner}
}

// lockOwners locks rows in id order and rejects unknown ids and retired added ones.
func lockOwners(ids, added []uuid.UUID, lock func([]uuid.UUID) ([]uuid.UUID, []bool, error), kind string) error {
	if len(ids) == 0 {
		return nil
	}
	locked, retired, err := lock(ids)
	if err != nil {
		return MapDBError(err)
	}
	if len(locked) != len(ids) {
		return fmt.Errorf("%w: %s not found", ErrNotFound, kind)
	}
	for i, id := range locked {
		if retired[i] && ContainsID(added, id) {
			return fmt.Errorf("%w: %s %s is retired", ErrEntityRetired, kind, id)
		}
	}
	return nil
}

// Handle locks the group first, then items and categories in id order.
func (h *ReplaceGroupAssignmentsHandler) Handle(ctx context.Context, actor Actor, cmd ReplaceGroupAssignmentsCommand) (int, ModifierGroupAssignmentsResponse, error) {
	items, err := NormalizeIDSet(cmd.ItemIDs, "item_ids")
	if err != nil {
		return 0, ModifierGroupAssignmentsResponse{}, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error())
	}
	cats, err := NormalizeIDSet(cmd.CategoryIDs, "category_ids")
	if err != nil {
		return 0, ModifierGroupAssignmentsResponse{}, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error())
	}
	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpModifierGroupReplaceAssignments,
		Fingerprint: replaceGroupAssignmentsFingerprint{GroupID: cmd.GroupID, ItemIDs: items, CategoryIDs: cats},
		Required:    []string{CapAdministerStructure},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec, func(q *sqlc.Queries) (int, ModifierGroupAssignmentsResponse, AuditRecord, error) {
		zero := ModifierGroupAssignmentsResponse{}
		group, err := q.GetModifierGroupForUpdate(ctx, cmd.GroupID)
		if err != nil {
			return 0, zero, AuditRecord{}, MapDBError(err)
		}
		if group.RetiredAt.Valid {
			return 0, zero, AuditRecord{}, fmt.Errorf("%w: modifier group is retired", ErrEntityRetired)
		}

		curItems, err := q.ListItemIDsWithDirectGroup(ctx, cmd.GroupID)
		if err != nil {
			return 0, zero, AuditRecord{}, MapDBError(err)
		}
		curCats, err := q.ListCategoryIDsWithGroup(ctx, cmd.GroupID)
		if err != nil {
			return 0, zero, AuditRecord{}, MapDBError(err)
		}
		iAdd, iRem := DiffIDSets(curItems, items)
		cAdd, cRem := DiffIDSets(curCats, cats)

		if err := lockOwners(UnionIDs(curItems, items), iAdd, func(ids []uuid.UUID) ([]uuid.UUID, []bool, error) {
			rows, err := q.LockMenuItemsByIDs(ctx, ids)
			locked, retired := make([]uuid.UUID, len(rows)), make([]bool, len(rows))
			for i, r := range rows {
				locked[i], retired[i] = r.ID, r.RetiredAt.Valid
			}
			return locked, retired, err
		}, "menu item"); err != nil {
			return 0, zero, AuditRecord{}, err
		}
		if err := lockOwners(UnionIDs(curCats, cats), cAdd, func(ids []uuid.UUID) ([]uuid.UUID, []bool, error) {
			rows, err := q.LockMenuCategoriesByIDs(ctx, ids)
			locked, retired := make([]uuid.UUID, len(rows)), make([]bool, len(rows))
			for i, r := range rows {
				locked[i], retired[i] = r.ID, r.RetiredAt.Valid
			}
			return locked, retired, err
		}, "category"); err != nil {
			return 0, zero, AuditRecord{}, err
		}

		excluding, err := q.ListItemIDsExcludingGroup(ctx, cmd.GroupID)
		if err != nil {
			return 0, zero, AuditRecord{}, MapDBError(err)
		}
		for _, id := range iAdd {
			if ContainsID(excluding, id) {
				return 0, zero, AuditRecord{}, fmt.Errorf("%w: item %s excludes this group; lift the exclusion first", ErrInvalidInheritance, id)
			}
		}

		removedExcl := []ExclusionRef{}
		for _, c := range cRem {
			if err := q.DeleteCategoryModifierGroup(ctx, sqlc.DeleteCategoryModifierGroupParams{MenuCategoryID: c, ModifierGroupID: cmd.GroupID}); err != nil {
				return 0, zero, AuditRecord{}, MapDBError(err)
			}
			refs, err := deleteCategoryGroupExclusions(ctx, q, c, cmd.GroupID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			removedExcl = append(removedExcl, refs...)
		}
		for _, c := range cAdd {
			if err := q.CreateCategoryModifierGroup(ctx, sqlc.CreateCategoryModifierGroupParams{MenuCategoryID: c, ModifierGroupID: cmd.GroupID}); err != nil {
				return 0, zero, AuditRecord{}, MapDBError(err)
			}
		}
		for _, i := range iRem {
			if err := q.DeleteItemModifierGroup(ctx, sqlc.DeleteItemModifierGroupParams{MenuItemID: i, ModifierGroupID: cmd.GroupID}); err != nil {
				return 0, zero, AuditRecord{}, MapDBError(err)
			}
		}
		for _, i := range iAdd {
			if err := q.CreateItemModifierGroup(ctx, sqlc.CreateItemModifierGroupParams{MenuItemID: i, ModifierGroupID: cmd.GroupID}); err != nil {
				return 0, zero, AuditRecord{}, MapDBError(err)
			}
		}
		sortExclusionRefs(removedExcl)

		res := ModifierGroupAssignmentsResponse{GroupID: cmd.GroupID, ItemIDs: items, CategoryIDs: cats, RemovedExclusions: removedExcl}
		if len(iAdd)+len(iRem)+len(cAdd)+len(cRem) == 0 {
			return 200, res, AuditRecord{}, nil
		}
		audit := AuditRecord{
			EventType: EventModifierGroupAssignmentsReplaced,
			Details: modifierGroupAssignmentsReplacedAudit{
				GroupID:           cmd.GroupID,
				Items:             IDSetChange{Added: iAdd, Removed: iRem},
				Categories:        IDSetChange{Added: cAdd, Removed: cRem},
				RemovedExclusions: removedExcl,
			},
		}
		return 200, res, audit, nil
	})
}
```

- [ ] **Step 5: HTTP handler and route**

Append to `http_structure.go`:

```go
// handleReplaceGroupAssignments godoc
//
//	@Summary		Gán hàng loạt một nhóm topping
//	@Description	Đặt đúng tập món và danh mục được gán trực tiếp nhóm topping này (Batch Linker). Gỡ nhóm khỏi danh mục sẽ xóa các loại trừ liên quan. Yêu cầu quyền catalog.administer_structure.
//	@Tags			Catalog
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			group_id	path		string							true	"Modifier Group ID (UUID)"
//	@Param			request		body		ReplaceGroupAssignmentsRequest	true	"Tập món và danh mục mong muốn"
//	@Success		200			{object}	response.APIResponse{data=ModifierGroupAssignmentsResponse}
//	@Failure		400			{object}	response.APIResponse
//	@Failure		401			{object}	response.APIResponse
//	@Failure		403			{object}	response.APIResponse
//	@Failure		404			{object}	response.APIResponse
//	@Failure		409			{object}	response.APIResponse
//	@Failure		500			{object}	response.APIResponse
//	@Router			/catalog/modifier-groups/{group_id}/assignments [put]
func (s *Slices) handleReplaceGroupAssignments(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	groupID, err := parseUUIDParam(c, "group_id")
	if err != nil {
		return sendError(c, err)
	}
	req, err := bindBody[ReplaceGroupAssignmentsRequest](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(req.RequestID); err != nil {
		return sendError(c, err)
	}
	status, res, err := s.ReplaceGroupAssignments.Handle(c.Request().Context(), actor, ReplaceGroupAssignmentsCommand{
		RequestID: req.RequestID, GroupID: groupID, ItemIDs: req.ItemIDs, CategoryIDs: req.CategoryIDs,
	})
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
}
```

`routes.go`: field, constructor, route
`catalog.PUT("/modifier-groups/:group_id/assignments", s.handleReplaceGroupAssignments, authn.RequireCapability(CapAdministerStructure))`.

- [ ] **Step 6: Run the tests and lint**

Run: `go test -tags integration ./internal/catalog/ -run 'TestReplace' -count=1 && make lint`
Expected: PASS, no lint findings.

- [ ] **Step 7: Commit**

```bash
git add sql/queries/catalog.sql internal/database/sqlc internal/catalog
git commit -m "feat(catalog): replace a modifier group's assignments in one command

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 11: Projections return the new fields; availability gates prices

**Files:**
- Modify: `internal/catalog/executor.go`, `internal/catalog/dto.go`, `internal/catalog/projections.go`
- Test: `internal/catalog/projections_display_integration_test.go`

**Interfaces:**
- Consumes: `ImageURL`, `nullStringPtr`.
- Produces:
  - `func ExecuteReadWithCapabilities[T any](ctx, r *Runner, actor Actor, requiredCapability string, fn func(q *sqlc.Queries, caps []string) (T, error)) (T, error)`
  - New JSON fields per spec §5.1: sellable item `code, badge, image_url`, category `icon`; management item `code, badge, description, image_url`, category `icon, display_order`; availability item `code, image_url, price_vnd?`, option `surcharge_vnd?`, category `icon`.

- [ ] **Step 1: Write the failing test**

Create `internal/catalog/projections_display_integration_test.go`:

```go
//go:build integration

package catalog_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/catalog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProjectionsCarryDisplayFields(t *testing.T) {
	db, q := openExecutorTestDB(t)
	runner := catalog.NewRunner(db, q)
	ctx := context.Background()
	cleanCategoryTestTables(t, db)

	manager := managerActor(t, db, q)
	baristaIdent := createCatalogTestIdentity(t, db, q, []string{auth.RoleBarista}, true, "")
	barista := catalog.Actor{StaffID: baristaIdent.StaffID, SessionID: baristaIdent.SessionID}

	tea := createTestCategoryDirect(t, db, "Tea")
	coffee := createTestCategoryDirect(t, db, "Coffee")
	_, err := db.Exec(`UPDATE menu_categories SET display_order = 1, icon = 'coffee' WHERE id = $1`, coffee)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE menu_categories SET display_order = 2 WHERE id = $1`, tea)
	require.NoError(t, err)

	price := int64(35000)
	croissant := createTestItemDirect(t, db, coffee, "Croissant", &price, false)
	key := strings.Repeat("a", 64) + ".webp"
	_, err = db.Exec(`UPDATE menu_items SET code = 'CB', normalized_code = 'cb', badge = 'HOT', description = 'Bơ tỏi', image_key = $2 WHERE id = $1`, croissant, key)
	require.NoError(t, err)
	latte := createTestItemDirect(t, db, tea, "Latte", nil, false)
	createTestSizeDirect(t, db, latte, "Size L", 45000, false)
	createTestSizeDirect(t, db, latte, "Size M", 39000, false)
	createTestSizeDirect(t, db, latte, "Size S", 20000, true) // retired: ignored for the card price

	group := createTestModifierGroupDirect(t, db, "Topping", 0, 1, false)
	createTestModifierOptionDirect(t, db, group, "Pearl", 5000, true, false)
	attachCategoryGroupDirect(t, db, tea, group)

	t.Run("management menu", func(t *testing.T) {
		res, err := catalog.NewManagementMenuHandler(runner).Handle(ctx, manager)
		require.NoError(t, err)
		require.Len(t, res.Categories, 2)
		assert.Equal(t, coffee, res.Categories[0].ID, "display_order first")
		assert.Equal(t, "coffee", *res.Categories[0].Icon)
		assert.Equal(t, int32(1), res.Categories[0].DisplayOrder)
		item := res.Categories[0].Items[0]
		assert.Equal(t, "CB", *item.Code)
		assert.Equal(t, "HOT", *item.Badge)
		assert.Equal(t, "Bơ tỏi", *item.Description)
		assert.Equal(t, "/media/catalog/"+key, *item.ImageURL)
	})

	t.Run("sellable menu", func(t *testing.T) {
		res, err := catalog.NewSellableMenuHandler(runner).Handle(ctx, manager)
		require.NoError(t, err)
		require.Equal(t, coffee, res.Categories[0].ID)
		assert.Equal(t, "coffee", *res.Categories[0].Icon)
		item := res.Categories[0].Items[0]
		assert.Equal(t, "CB", *item.Code)
		assert.Equal(t, "HOT", *item.Badge)
		assert.Equal(t, "/media/catalog/"+key, *item.ImageURL)
	})

	t.Run("availability menu shows prices to a manager", func(t *testing.T) {
		res, err := catalog.NewAvailabilityMenuHandler(runner).Handle(ctx, manager)
		require.NoError(t, err)
		require.Len(t, res.Categories, 2)
		c := res.Categories[0].Items[0]
		assert.Equal(t, "CB", *c.Code)
		assert.Equal(t, "/media/catalog/"+key, *c.ImageURL)
		require.NotNil(t, c.PriceVND)
		assert.Equal(t, int64(35000), *c.PriceVND)

		l := res.Categories[1].Items[0]
		require.NotNil(t, l.PriceVND)
		assert.Equal(t, int64(39000), *l.PriceVND, "lowest non-retired size")
		require.NotNil(t, l.ModifierGroups[0].Options[0].SurchargeVND)
		assert.Equal(t, int64(5000), *l.ModifierGroups[0].Options[0].SurchargeVND)
	})

	t.Run("availability menu hides prices from a barista", func(t *testing.T) {
		res, err := catalog.NewAvailabilityMenuHandler(runner).Handle(ctx, barista)
		require.NoError(t, err)
		assert.Nil(t, res.Categories[0].Items[0].PriceVND)
		assert.Nil(t, res.Categories[1].Items[0].PriceVND)
		assert.Nil(t, res.Categories[1].Items[0].ModifierGroups[0].Options[0].SurchargeVND)
		assert.Equal(t, "CB", *res.Categories[0].Items[0].Code, "non-price fields stay")
	})
}
```

Run: `go test -tags integration ./internal/catalog/ -run TestProjectionsCarryDisplayFields -count=1`
Expected: compile FAIL — `res.Categories[0].Icon undefined`.

- [ ] **Step 2: Add `ExecuteReadWithCapabilities`**

In `internal/catalog/executor.go`, replace the body of `ExecuteRead` so it delegates, and add the new function:

```go
// ExecuteRead runs a read-only query inside a repeatable-read transaction
// with current authority verification.
func ExecuteRead[T any](ctx context.Context, r *Runner, actor Actor,
	requiredCapability string,
	fn func(*sqlc.Queries) (T, error),
) (T, error) {
	return ExecuteReadWithCapabilities(ctx, r, actor, requiredCapability, func(q *sqlc.Queries, _ []string) (T, error) {
		return fn(q)
	})
}

// ExecuteReadWithCapabilities is ExecuteRead for projections whose fields
// depend on the caller's capabilities (ADR-061).
func ExecuteReadWithCapabilities[T any](ctx context.Context, r *Runner, actor Actor,
	requiredCapability string,
	fn func(q *sqlc.Queries, caps []string) (T, error),
) (T, error) {
	var zero T

	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return zero, fmt.Errorf("begin read transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	q := r.queries.WithTx(tx)

	_, _, caps, err := reloadAuthority(ctx, q, actor)
	if err != nil {
		return zero, err
	}
	if err := verifyCapabilities([]string{requiredCapability}, caps); err != nil {
		return zero, err
	}

	result, err := fn(q, caps)
	if err != nil {
		return zero, err
	}
	if err := tx.Commit(); err != nil {
		return zero, fmt.Errorf("commit read transaction: %w", err)
	}
	return result, nil
}
```

- [ ] **Step 3: Add the DTO fields**

In `internal/catalog/dto.go`:

- `SellableItemResponse`: add after `Name`
  ```go
	Code     *string `json:"code"`
	Badge    *string `json:"badge"`
	ImageURL *string `json:"image_url"`
  ```
- `SellableCategoryResponse`: add `Icon *string `json:"icon"`` after `Name`.
- `ManagementItemResponse`: add after `Name`
  ```go
	Code        *string `json:"code"`
	Badge       *string `json:"badge"`
	Description *string `json:"description"`
	ImageURL    *string `json:"image_url"`
  ```
- `ManagementCategoryResponse`: add after `Name`
  ```go
	Icon         *string `json:"icon"`
	DisplayOrder int32   `json:"display_order"`
  ```
- `AvailabilityItemResponse`: add after `Name`
  ```go
	Code     *string `json:"code"`
	ImageURL *string `json:"image_url"`
	// PriceVND is the item price, or its lowest non-retired Size price. Present
	// only for callers holding catalog.view_prices (ADR-061).
	PriceVND *int64 `json:"price_vnd,omitempty"`
  ```
- `AvailabilityModifierOptionResponse`: add `SurchargeVND *int64 `json:"surcharge_vnd,omitempty"`` (same comment).
- `AvailabilityCategoryResponse`: add `Icon *string `json:"icon"`` after `Name`.
- Change the comment on `AvailabilityMenuResponse` from "price-free projection" to "availability projection; prices only with catalog.view_prices".

- [ ] **Step 4: Fill the fields in `projections.go`**

Sellable (the literal near line 243):

```go
			itemsByCategory[item.CategoryID] = append(itemsByCategory[item.CategoryID], SellableItemResponse{
				ID:             item.ID,
				CategoryID:     item.CategoryID,
				Name:           item.Name,
				Code:           nullStringPtr(item.Code),
				Badge:          nullStringPtr(item.Badge),
				ImageURL:       ImageURL(item.ImageKey),
				PriceVND:       itemPrice,
				Sizes:          availableSizes,
				ModifierGroups: sellableGroups,
			})
```

and the category literal near line 264: add `Icon: nullStringPtr(cat.Icon),`.

Management (near line 432): add `Code: nullStringPtr(item.Code), Badge: nullStringPtr(item.Badge), Description: nullStringPtr(item.Description), ImageURL: ImageURL(item.ImageKey),`; category literal near line 476: add `Icon: nullStringPtr(cat.Icon), DisplayOrder: cat.DisplayOrder,`.

Availability: change `return ExecuteRead(ctx, h.runner, actor, CapManageAvailability, func(q *sqlc.Queries) (AvailabilityMenuResponse, error) {` to

```go
	return ExecuteReadWithCapabilities(ctx, h.runner, actor, CapManageAvailability, func(q *sqlc.Queries, caps []string) (AvailabilityMenuResponse, error) {
		showPrices := verifyCapabilities([]string{CapViewPrices}, caps) == nil
```

In the options loop, set the surcharge when allowed:

```go
			optResp := AvailabilityModifierOptionResponse{ID: opt.ID, Name: opt.Name, Available: opt.Available}
			if showPrices {
				s := opt.SurchargeVnd
				optResp.SurchargeVND = &s
			}
			optionsByGroup[opt.ModifierGroupID] = append(optionsByGroup[opt.ModifierGroupID], optResp)
```

Before the items loop, compute the lowest non-retired size price per item:

```go
		lowestSizePrice := make(map[uuid.UUID]int64)
		for _, s := range snap.sizes {
			if s.RetiredAt.Valid {
				continue
			}
			if cur, ok := lowestSizePrice[s.MenuItemID]; !ok || s.PriceVnd < cur {
				lowestSizePrice[s.MenuItemID] = s.PriceVnd
			}
		}
```

In the item literal:

```go
			itemResp := AvailabilityItemResponse{
				ID:             item.ID,
				CategoryID:     item.CategoryID,
				Name:           item.Name,
				Code:           nullStringPtr(item.Code),
				ImageURL:       ImageURL(item.ImageKey),
				Available:      item.Available,
				Sizes:          itemSizes,
				ModifierGroups: effGroups,
			}
			if showPrices {
				if item.PriceVnd.Valid {
					p := item.PriceVnd.Int64
					itemResp.PriceVND = &p
				} else if p, ok := lowestSizePrice[item.ID]; ok {
					itemResp.PriceVND = &p
				}
			}
			itemsByCategory[item.CategoryID] = append(itemsByCategory[item.CategoryID], itemResp)
```

And the category literal: add `Icon: nullStringPtr(cat.Icon),`.

Categories already arrive ordered by `display_order` from `ListMenuCategories` (Task 1).

- [ ] **Step 5: Run the projection tests and the whole catalog suite**

Run: `go test -tags integration ./internal/catalog/... -count=1`
Expected: PASS, including the pre-existing projection tests. Existing availability tests that compare whole `AvailabilityItemResponse` or `AvailabilityModifierOptionResponse` values for a Manager or Cashier caller now see `PriceVND` / `SurchargeVND` filled; update those expectations to the real prices (that is the intended behavior change, ADR-061), never gate the field differently to make old assertions pass. If an existing test asserts category order by name, set its categories' `display_order` equal (default 0 after `cleanCategoryTestTables`, so ties already sort by name) — no change should be needed.

- [ ] **Step 6: Commit**

```bash
git add internal/catalog
git commit -m "feat(catalog): menu projections return display fields and gated prices

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 12: Swagger, client regeneration, and real availability cards

**Files:**
- Regenerate: `docs/swagger.yaml` (and siblings) via `make swagger`; `web/src/api/generated/**` via `bun run codegen`
- Modify: `web/src/features/settings/lib/availability.ts`, `availability-cards.ts`, `availability-cards.test.ts`
- Delete: `web/src/features/settings/lib/availability-mock.ts`
- Modify: `web/src/lib/error-messages.ts`, `web/vite.config.ts`

**Interfaces:**
- Consumes: the regenerated `CatalogAvailabilityMenuResponse` with `code`, `image_url`, `price_vnd`, option `surcharge_vnd`.
- Produces: `AvailabilityItemView` gains `code: string | null; imageUrl: string | null; priceVnd: number | null`; `AvailabilityOptionView` gains `priceVnd: number | null`.

- [ ] **Step 1: Regenerate Swagger and the client**

Run: `make swagger && cd web && bun run codegen && cd ..`
Expected: `git diff --stat docs/ web/src/api/generated` shows the eleven new paths and the new fields; `grep -n "image_url" web/src/api/generated/models/catalogAvailabilityItemResponse.ts` finds the field.

- [ ] **Step 2: Update the card test to expect real values**

In `web/src/features/settings/lib/availability-cards.test.ts`, give the fixture real fields:
- `i-den`: add `code: "CFD", image_url: "/media/catalog/abc.webp", price_vnd: 25_000`.
- `i-croissant`: add `price_vnd: 35_000` (no code, no image).
- in `topping.options[0]` (`o-pearl`): add `surcharge_vnd: 5_000`.

Replace the test at lines 75–78 (`"fills mock price and image until the backend provides them"`) with:

```ts
  it("reads price, image, and code from the API", () => {
    const den = cards.find((c) => c.id === "i-den")!;
    expect(den.priceVnd).toBe(25_000);
    expect(den.imageUrl).toBe("/media/catalog/abc.webp");
    expect(den.code).toBe("CFD");

    const croissant = cards.find((c) => c.id === "i-croissant")!;
    expect(croissant.priceVnd).toBe(35_000);
    expect(croissant.imageUrl).toBeNull();
    expect(croissant.code).toBe("CBT"); // derived from the name when the API has none

    expect(cards.find((c) => c.id === "o-pearl")!.priceVnd).toBe(5_000);
  });

  it("shows no price when the API omits it (Barista)", () => {
    const view = toAvailabilityView({ categories: [{ id: "c", name: "C", items: [{ id: "x", name: "Món lạ", available: true }] }] });
    expect(toStockCards(view)[0].priceVnd).toBeNull();
  });
```

If `getAcronym("Croissant bơ tỏi")` produces something other than `"cbt"`, use its actual output in the assertion — check with `bun -e 'import {getAcronym} from "./src/lib/search"; console.log(getAcronym("Croissant bơ tỏi"))'` from `web/`.

Run: `cd web && bun test src/features/settings && cd ..`
Expected: FAIL on the new assertions (the mock still supplies values).

- [ ] **Step 3: Carry the fields through the view**

In `web/src/features/settings/lib/availability.ts`:

Add to `AvailabilityItemView`:

```ts
  /** Stored code, or null; cards derive one from the name when null. */
  code: string | null;
  imageUrl: string | null;
  /** Item price or lowest Size price; null when the caller lacks catalog.view_prices. */
  priceVnd: number | null;
```

Add to `AvailabilityOptionView`: `priceVnd: number | null;`

In `toAvailabilityView`, in `group.options.push({...})` add `priceVnd: o.surcharge_vnd ?? null,`; in `items.push({...})` add

```ts
        code: item.code ?? null,
        imageUrl: item.image_url ?? null,
        priceVnd: item.price_vnd ?? null,
```

- [ ] **Step 4: Remove the mock**

In `web/src/features/settings/lib/availability-cards.ts`:
- delete `import { mockImageUrl, mockPriceVnd } from "./availability-mock";`
- item card: `code: (item.code ?? getAcronym(item.name)).toUpperCase(),`, `priceVnd: item.priceVnd,`, `imageUrl: item.imageUrl,`
- topping card: `priceVnd: option.priceVnd,`
- update the two field comments on `StockCard`:
  ```ts
  /** Short code shown next to the name ("CFSD"): the stored code, else derived from the name. */
  code: string;
  ...
  /** Item price, lowest Size price, or topping surcharge; null when the caller cannot see prices. */
  priceVnd: number | null;
  /** Served from /media/catalog; null when the item has no image. */
  imageUrl: string | null;
  ```

Delete the mock: `git rm web/src/features/settings/lib/availability-mock.ts`.

- [ ] **Step 5: Error texts and the dev proxy**

Add to `ERROR_MESSAGES` in `web/src/lib/error-messages.ts` (next to `CATALOG_NOT_FOUND`):

```ts
  CATALOG_CODE_CONFLICT: "Mã món này đã được dùng cho một món khác.",
  INVALID_IMAGE: "Ảnh không hợp lệ. Chỉ nhận JPEG, PNG hoặc WebP.",
  IMAGE_TOO_LARGE: "Ảnh quá lớn. Dung lượng tối đa là 1 MB.",
```

In `web/vite.config.ts`, add beside `"/swagger"`:

```ts
      "/media": {
        target: process.env.VITE_BACKEND_URL || "http://localhost:8080",
        changeOrigin: true,
      },
```

- [ ] **Step 6: Run the web checks**

Run: `cd web && bun test && bun run build && cd ..`
Expected: all tests PASS; the build (which type-checks) succeeds. `grep -rn "availability-mock\|mockPriceVnd\|mockImageUrl" web/src` returns nothing.

- [ ] **Step 7: Commit**

```bash
git add docs web
git commit -m "feat(web): availability cards read price, image, and code from the API

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 13: Dev seed, ADRs, and backlog updates

**Files:**
- Modify: `scripts/dev-seed.ts`
- Modify: `spec/decisions.md` (ADR-057 to ADR-061)
- Modify: `docs/backlog/phase-12-backup-restore-update-readiness.md`, `docs/backlog/backend-alignment.md`, `docs/backlog/availability-card-fields.md`, `docs/backlog/README.md`, `ROADMAP.md`

- [ ] **Step 1: Seed display fields**

In `scripts/dev-seed.ts`, capture the item ids and set details after the items are created. Change each `await apiRequest("/catalog/items", …)` to assign its result, e.g. `const croissantRes = await apiRequest<IdResponse>("/catalog/items", { … });` (names: `croissantRes`, `denRes`, `suaDaRes`, `bacXiuRes`, `traDaoRes`). Then, before `console.log("Sample catalog successfully seeded.");`, add:

```ts
  // 6. Display fields (BA-1). No images: the seed must run offline.
  const categoryDetails: Array<[string, string, number]> = [
    [catCoffeeId, "coffee", 1],
    [catTeaId, "cup-soda", 2],
    [catPastryId, "croissant", 3],
  ];
  for (const [id, icon, order] of categoryDetails) {
    if (!id) continue;
    await apiRequest(`/catalog/categories/${id}/details`, {
      token,
      method: "PATCH",
      body: { icon, display_order: order },
    });
  }

  const itemDetails: Array<[string | undefined, string, string | null]> = [
    [croissantRes?.data?.id, "CBT", null],
    [denRes?.data?.id, "CFD", null],
    [suaDaRes?.data?.id, "CFSD", "BEST_SELLER"],
    [bacXiuRes?.data?.id, "BX", "SIGNATURE"],
    [traDaoRes?.data?.id, "TDCS", "HOT"],
  ];
  for (const [id, code, badge] of itemDetails) {
    if (!id) continue;
    await apiRequest(`/catalog/items/${id}/details`, {
      token,
      method: "PATCH",
      body: { code, badge, description: null },
    });
  }
```

Verify: `make docker-up`, reset the dev database if the catalog is already seeded (the seed returns early when categories exist), `make run` in one terminal, then `make dev-seed`.
Expected: "Sample catalog successfully seeded." and `curl -s localhost:8080/api/v1/catalog/menu/manage -H "Authorization: Bearer <token>"` shows codes and icons.

- [ ] **Step 2: Record the ADRs**

Append to `spec/decisions.md`, following the format of ADR-055 and ADR-056 (read them first and copy their heading, Status, Context, Decision, and Consequences layout):

- **ADR-057: Catalog images are content-addressed files under `MEDIA_DIR`, served unauthenticated at `/media`.** Context: Phase 11 forbids an Internet dependency; Phase 12 must back everything up. Decision: store files named `sha256 + ext`, written before the transaction; serve at `/media/catalog/{key}` without auth because `<img>` sends no bearer token and the hash cannot be guessed. Rejected: bytes in PostgreSQL (heavier database and backups), external URLs (break offline). Consequence: backups must include `MEDIA_DIR`; unreferenced files accumulate until a cleanup job exists.
- **ADR-058: Menu display fields are non-commercial.** Code, badge, description, image, icon, and display order are edited in place, need no Manager Approval, and are not snapshotted into Committed Items. Code is optional; the web derives one from the name when it is empty. Badge is a closed enum.
- **ADR-059: Modifier assignments have replace-set commands, and an exclusion exists only while the item's category provides the group.** Detaching a group from a category or moving an item deletes orphaned exclusions in the same transaction and lists them in the audit. Adding an item that excludes a group through the group-centric command is rejected.
- **ADR-060: Pricing mode is fixed at item creation.** A sized item gains Sizes; a single-price item never does. Rejected: a conversion command, which needs rules for Order Drafts holding the item.
- **ADR-061: The availability projection returns prices only to callers holding `catalog.view_prices`.** Rejected: prices for everyone, which would hollow out the capability.

Update the ADR count in `ROADMAP.md` ("Fifty-six architecture decisions") to "Sixty-one".

- [ ] **Step 3: Update the backlog**

- `docs/backlog/phase-12-backup-restore-update-readiness.md`: under "Acceptance criteria", add
  `- [ ] Backup and restore include MEDIA_DIR (catalog images, ADR-057) together with the database, and a scheduled job removes image files no Menu Item references.`
- `docs/backlog/availability-card-fields.md`: change the status line to `**Status:** completed by backend alignment BA-1 ([spec](../superpowers/specs/2026-09-29-backend-alignment-ba1-catalog-design.md))` and tick its three acceptance boxes.
- `docs/backlog/README.md`: set that ticket's row status to `completed (BA-1)`.
- `docs/backlog/backend-alignment.md`: under the Sub-projects table, add a line: `BA-1 implemented on branch backend-alignment-ba1; awaiting UAT.` Leave the epic status `in-design (BA-1)` until the operator passes UAT.

- [ ] **Step 4: Commit**

```bash
git add scripts/dev-seed.ts spec/decisions.md docs/backlog ROADMAP.md
git commit -m "docs: record BA-1 decisions and seed display fields

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 14: Full verification and UAT hand-off

**Files:** none new.

- [ ] **Step 1: Run everything CI runs, plus integration and web**

Run:
```bash
make check
make test-integration
cd web && bun test && bun run build && cd ..
```
Expected: all PASS, no lint findings. Paste the tail of each output into the hand-off note.

- [ ] **Step 2: Smoke-test the running binary**

Run `make build`, start `./build/app.exe` with the dev `.env`, and confirm:
- `curl -I localhost:8080/media/catalog/$(printf 'a%.0s' {1..64}).png` → `404`
- `curl -s localhost:8080/swagger/doc.json | grep -c '/catalog/items/{item_id}/details'` → `1`

- [ ] **Step 3: Hand over the UAT script and stop**

Post the eight UAT steps from spec §9 to the operator verbatim, with the branch name `backend-alignment-ba1`. Do not mark BA-1 done, do not merge, and do not open the pull request until the operator confirms UAT. Completion is never self-certified.
