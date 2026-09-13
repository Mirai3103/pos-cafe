//go:build integration

package sales_test

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/database"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
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
// the work.
func truncateSalesTables(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`
		TRUNCATE order_draft_item_modifier_options, order_draft_items, order_drafts,
		         table_assignments, service_sessions, cash_movements, sales_shifts,
		         tables, modifier_group_default_options, item_modifier_group_exclusions,
		         item_modifier_groups, category_modifier_groups, modifier_options,
		         modifier_groups, menu_item_sizes, menu_items, menu_categories,
		         idempotency_keys, audit_events, staff_access_sessions,
		         staff_operational_roles, staff_identities
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

	shift, err := q.OpenSalesShift(ctx, sqlc.OpenSalesShiftParams{
		OpenedByStaffIdentityID: fx.StaffID,
		OpeningFloatVnd:         100000,
	})
	require.NoError(t, err)
	fx.SalesShiftID = shift.ID

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
