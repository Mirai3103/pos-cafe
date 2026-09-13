//go:build integration

package sales_test

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/database"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testLoginCode generates a high-entropy unique login code for a test identity.
// staff_identities.login_code is VARCHAR(24) and the prefix shares that budget.
// Identities are never deleted between runs, so a per-process counter would
// collide across runs.
func testLoginCode(prefix string) string {
	room := 24 - len(prefix)
	if room < 8 {
		panic("testLoginCode: prefix leaves too little entropy budget")
	}
	return prefix + strings.ReplaceAll(uuid.NewString(), "-", "")[:room]
}

// openSalesTestDB connects to the integration database and runs migrations
// (database.Open applies every pending embedded migration, including 000008).
// Mirrors openShiftTestDB in internal/shift.
func openSalesTestDB(t *testing.T) (*sql.DB, *sqlc.Queries) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	require.Contains(t, dsn, "_test")
	db, err := database.Open(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db, sqlc.New(db)
}

// truncateSalesTables clears every table this slice writes, plus the shared
// tables its fixtures seed. Order matters only for readability; CASCADE does
// the work. The 5B tables (checks, committed_items and their children) lead
// the list.
func truncateSalesTables(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`
		TRUNCATE charge_allocations, committed_item_modifier_options, committed_items,
		         checks, order_draft_item_modifier_options, order_draft_items,
		         order_drafts, table_assignments, service_sessions, cash_movements,
		         sales_shifts, tables, modifier_group_default_options,
		         item_modifier_group_exclusions, item_modifier_groups,
		         category_modifier_groups, modifier_options, modifier_groups,
		         menu_item_sizes, menu_items, menu_categories, idempotency_keys,
		         audit_events, staff_access_sessions, staff_operational_roles,
		         staff_identities
		RESTART IDENTITY CASCADE`)
	require.NoError(t, err)
}

// salesFixture is the minimal world a Sales test needs: an open Shift, a
// Service Session with its draft, and one priced Menu Item.
type salesFixture struct {
	StaffID          uuid.UUID
	SalesShiftID     uuid.UUID
	ServiceSessionID uuid.UUID
	DraftID          uuid.UUID
	MenuCategoryID   uuid.UUID
	MenuItemID       uuid.UUID
}

// seedSalesFixture inserts the fixture with raw SQL rather than through the
// Sales API, because Task 1 has no API yet and later tasks still need a world
// that exists before the command under test runs.
func seedSalesFixture(t *testing.T, db *sql.DB, q *sqlc.Queries) salesFixture {
	t.Helper()
	ctx := context.Background()
	var fx salesFixture

	staff, err := q.CreateStaffIdentity(ctx, sqlc.CreateStaffIdentityParams{
		DisplayName: "Fixture " + testLoginCode("F"),
		Btrim:       testLoginCode("F"),
		PinHash:     "$2a$10$abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKLMNOPQR",
		Enabled:     true,
	})
	require.NoError(t, err)
	fx.StaffID = staff.ID

	shift := seedOpenShift(t, q, fx.StaffID)
	fx.SalesShiftID = shift

	cat, err := q.CreateMenuCategory(ctx, sqlc.CreateMenuCategoryParams{
		Name:           "Cà phê",
		NormalizedName: "cà phê",
	})
	require.NoError(t, err)
	fx.MenuCategoryID = cat.ID

	item, err := q.CreateMenuItem(ctx, sqlc.CreateMenuItemParams{
		CategoryID:     fx.MenuCategoryID,
		Name:           "Cà phê sữa",
		NormalizedName: "cà phê sữa",
		PriceVnd:       sql.NullInt64{Int64: 25000, Valid: true},
		Available:      true,
	})
	require.NoError(t, err)
	fx.MenuItemID = item.ID

	// service_sessions and order_drafts have no sqlc writer until Task 6, so
	// the fixture inserts them directly.
	require.NoError(t, db.QueryRowContext(ctx, `
		INSERT INTO service_sessions
		    (service_number, sequence, service_mode, state, created_by_staff_identity_id, sales_shift_id)
		VALUES ('S00001', 1, 'TAKEAWAY', 'ACTIVE', $1, $2)
		RETURNING id`, fx.StaffID, fx.SalesShiftID).Scan(&fx.ServiceSessionID))

	require.NoError(t, db.QueryRowContext(ctx, `
		INSERT INTO order_drafts (service_session_id) VALUES ($1) RETURNING id`,
		fx.ServiceSessionID).Scan(&fx.DraftID))

	return fx
}

// seedActor creates an enabled identity with the given roles and an active
// access session, returning the Actor the executor expects.
func seedActor(t *testing.T, q *sqlc.Queries, roles []string) sales.Actor {
	t.Helper()
	ctx := context.Background()

	code := testLoginCode("A")
	hash, err := auth.HashPin("1234")
	require.NoError(t, err)

	identity, err := q.CreateStaffIdentity(ctx, sqlc.CreateStaffIdentityParams{
		DisplayName: "Actor " + code,
		Btrim:       code,
		PinHash:     hash,
		Enabled:     true,
	})
	require.NoError(t, err)

	for _, role := range roles {
		require.NoError(t, q.AddStaffRole(ctx, sqlc.AddStaffRoleParams{
			StaffIdentityID: identity.ID,
			Role:            role,
		}))
	}

	session, err := q.CreateStaffSession(ctx, sqlc.CreateStaffSessionParams{
		TokenHash:           "tok_" + uuid.NewString()[:16],
		StaffIdentityID:     identity.ID,
		State:               auth.SessionStateActive,
		LastHumanActivityAt: time.Now(),
		ExpiresAt:           time.Now().Add(8 * time.Hour),
	})
	require.NoError(t, err)

	return sales.Actor{StaffID: identity.ID, SessionID: session.ID}
}

// assertAuditEvent asserts the exact number of audit_events rows of a type.
func assertAuditEvent(t *testing.T, db *sql.DB, eventType string, want int) {
	t.Helper()
	var got int
	require.NoError(t, db.QueryRow(
		`SELECT count(*) FROM audit_events WHERE event_type = $1`, eventType).Scan(&got))
	assert.Equal(t, want, got, "audit_events rows of type %s", eventType)
}

// seedOpenShift opens a Sales Shift and returns its id.
func seedOpenShift(t *testing.T, q *sqlc.Queries, openedBy uuid.UUID) uuid.UUID {
	t.Helper()
	shift, err := q.OpenSalesShift(context.Background(), sqlc.OpenSalesShiftParams{
		OpenedByStaffIdentityID: openedBy,
		OpeningFloatVnd:         100000,
	})
	require.NoError(t, err)
	return shift.ID
}

// seedTable creates a Table and returns its id.
func seedTable(t *testing.T, db *sql.DB, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	require.NoError(t, db.QueryRow(`
		INSERT INTO tables (name, normalized_name, available)
		VALUES ($1, lower($1), true) RETURNING id`, name).Scan(&id))
	return id
}

// seedSize adds a Size to a Menu Item and returns its id.
func seedSize(t *testing.T, db *sql.DB, menuItemID uuid.UUID, name string, priceVND int64) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	require.NoError(t, db.QueryRow(`
		INSERT INTO menu_item_sizes (menu_item_id, name, normalized_name, price_vnd, available)
		VALUES ($1, $2, lower($2), $3, true) RETURNING id`,
		menuItemID, name, priceVND).Scan(&id))
	return id
}

// seedMenuCategory creates a Menu Category and returns its id.
func seedMenuCategory(t *testing.T, db *sql.DB, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	require.NoError(t, db.QueryRow(`
		INSERT INTO menu_categories (name, normalized_name)
		VALUES ($1, lower($1)) RETURNING id`, name).Scan(&id))
	return id
}

// seedMenuItem creates a directly priced Menu Item in a fresh Category.
func seedMenuItem(t *testing.T, db *sql.DB, name string, priceVND int64) uuid.UUID {
	t.Helper()
	categoryID := seedMenuCategory(t, db, "Cat "+name)
	var id uuid.UUID
	require.NoError(t, db.QueryRow(`
		INSERT INTO menu_items (category_id, name, normalized_name, price_vnd, available)
		VALUES ($1, $2, lower($2), $3, true) RETURNING id`,
		categoryID, name, priceVND).Scan(&id))
	return id
}

// seedSizedMenuItem creates a Menu Item priced only through its Sizes, so its
// own price_vnd is NULL.
func seedSizedMenuItem(t *testing.T, db *sql.DB, name string) uuid.UUID {
	t.Helper()
	categoryID := seedMenuCategory(t, db, "Cat "+name)
	var id uuid.UUID
	require.NoError(t, db.QueryRow(`
		INSERT INTO menu_items (category_id, name, normalized_name, price_vnd, available)
		VALUES ($1, $2, lower($2), NULL, true) RETURNING id`,
		categoryID, name).Scan(&id))
	return id
}

// seedModifierGroup creates a Modifier Group and returns its id.
func seedModifierGroup(t *testing.T, db *sql.DB, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	require.NoError(t, db.QueryRow(`
		INSERT INTO modifier_groups (name, normalized_name)
		VALUES ($1, lower($1)) RETURNING id`, name).Scan(&id))
	return id
}

// seedModifierOption creates an available Modifier Option in the Group and
// returns its id.
func seedModifierOption(t *testing.T, db *sql.DB, groupID uuid.UUID, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	require.NoError(t, db.QueryRow(`
		INSERT INTO modifier_options (modifier_group_id, name, normalized_name)
		VALUES ($1, $2, lower($2)) RETURNING id`, groupID, name).Scan(&id))
	return id
}

// attachGroupToCategory makes the Group inherited by every Item in the
// Category, through the same association Catalog writes.
func attachGroupToCategory(t *testing.T, db *sql.DB, categoryID, groupID uuid.UUID) {
	t.Helper()
	_, err := db.Exec(`
		INSERT INTO category_modifier_groups (menu_category_id, modifier_group_id)
		VALUES ($1, $2)`, categoryID, groupID)
	require.NoError(t, err)
}

// seedMenuItemInCategory creates a directly priced Menu Item in an existing
// Category, for fixtures that need the Item inside a specific Category.
func seedMenuItemInCategory(t *testing.T, db *sql.DB, categoryID uuid.UUID, name string, priceVND int64) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	require.NoError(t, db.QueryRow(`
		INSERT INTO menu_items (category_id, name, normalized_name, price_vnd, available)
		VALUES ($1, $2, lower($2), $3, true) RETURNING id`,
		categoryID, name, priceVND).Scan(&id))
	return id
}

// seedItemWithDefaultOption creates a Menu Item whose effective Modifier Group
// declares exactly one default Option, and returns both ids.
func seedItemWithDefaultOption(t *testing.T, db *sql.DB) (itemID, defaultOptionID uuid.UUID) {
	t.Helper()
	categoryID := seedMenuCategory(t, db, "Danh mục mặc định")
	groupID := seedModifierGroup(t, db, "Đá")
	attachGroupToCategory(t, db, categoryID, groupID)
	defaultOptionID = seedModifierOption(t, db, groupID, "Ít đá")
	itemID = seedMenuItemInCategory(t, db, categoryID, "Cà phê mặc định", 30000)
	_, err := db.Exec(`
		INSERT INTO modifier_group_default_options (modifier_group_id, modifier_option_id)
		VALUES ($1, $2)`, groupID, defaultOptionID)
	require.NoError(t, err)
	return itemID, defaultOptionID
}

// seedExclusionFixture creates one Category with an inherited Group, two Items
// in it, and an item_modifier_group_exclusions row excluding the first Item
// from that Group. The Group offers one selectable Option.
func seedExclusionFixture(t *testing.T, db *sql.DB) (excludingItemID, inheritingItemID, optionID uuid.UUID) {
	t.Helper()
	categoryID := seedMenuCategory(t, db, "Danh mục loại trừ")
	groupID := seedModifierGroup(t, db, "Topping")
	attachGroupToCategory(t, db, categoryID, groupID)
	optionID = seedModifierOption(t, db, groupID, "Trân châu")
	excludingItemID = seedMenuItemInCategory(t, db, categoryID, "Món chặn", 20000)
	inheritingItemID = seedMenuItemInCategory(t, db, categoryID, "Món kế thừa", 21000)
	_, err := db.Exec(`
		INSERT INTO item_modifier_group_exclusions (menu_item_id, modifier_group_id)
		VALUES ($1, $2)`, excludingItemID, groupID)
	require.NoError(t, err)
	return excludingItemID, inheritingItemID, optionID
}

// seedItemWithTwoOptions creates a Menu Item whose effective Group offers two
// selectable Options, and returns the item and both option ids.
func seedItemWithTwoOptions(t *testing.T, db *sql.DB) (itemID, optionA, optionB uuid.UUID) {
	t.Helper()
	categoryID := seedMenuCategory(t, db, "Danh mục hai tuỳ chọn")
	groupID := seedModifierGroup(t, db, "Đường")
	attachGroupToCategory(t, db, categoryID, groupID)
	optionA = seedModifierOption(t, db, groupID, "Ít đường")
	optionB = seedModifierOption(t, db, groupID, "Nhiều đường")
	itemID = seedMenuItemInCategory(t, db, categoryID, "Món hai tuỳ chọn", 25000)
	return itemID, optionA, optionB
}

// assertModifierKeyIntegrity proves the denormalized key never drifts from the
// authoritative option rows.
func assertModifierKeyIntegrity(t *testing.T, db *sql.DB) {
	t.Helper()
	var drifted int
	require.NoError(t, db.QueryRow(`
		SELECT count(*)
		FROM order_draft_items di
		WHERE di.modifier_key <> COALESCE((
		    SELECT string_agg(m.modifier_option_id::text, ',' ORDER BY m.modifier_option_id::text)
		    FROM order_draft_item_modifier_options m
		    WHERE m.order_draft_item_id = di.id
		), '')`).Scan(&drifted))
	assert.Equal(t, 0, drifted, "modifier_key must equal the sorted join of the stored options")
}
