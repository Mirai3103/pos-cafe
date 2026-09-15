//go:build integration

package preparation_test

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/preparation"
	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/Mirai3103/pos-cafe/internal/testdb"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// testLoginCode generates a high-entropy unique login code for a test identity.
// staff_identities.login_code is VARCHAR(24) and the prefix shares that budget.
// Identities are never deleted between runs, so a per-process counter would
// collide across runs. Mirrored from internal/sales' test main.
func testLoginCode(prefix string) string {
	room := 24 - len(prefix)
	if room < 8 {
		panic("testLoginCode: prefix leaves too little entropy budget")
	}
	return prefix + strings.ReplaceAll(uuid.NewString(), "-", "")[:room]
}

// prepTestDB is the single pool every Preparation integration test shares,
// bound by TestMain before any test runs.
var prepTestDB *sql.DB

// sharedActorHashOnce hashes the fixture PIN once per process; bcrypt is
// deliberately slow, and every seedActor call in the package can share one.
var (
	sharedActorHashOnce sync.Once
	sharedActorHash     string
)

// TestMain provisions this package's isolated clone of the migrated test
// template and binds one shared pool for the whole package. The clone is
// already migrated, so tests never run migrations themselves.
func TestMain(m *testing.M) {
	os.Exit(testdb.Run(m, "preparation", func(db *sql.DB) {
		prepTestDB = db
	}))
}

// openPrepTestDB returns the shared package pool and its queries. The pool is
// owned by TestMain and closed when the package's clone is dropped, so callers
// must not close it.
func openPrepTestDB(t *testing.T) (*sql.DB, *sqlc.Queries) {
	t.Helper()
	require.NotNil(t, prepTestDB)
	return prepTestDB, sqlc.New(prepTestDB)
}

// truncatePrepTables clears every table the Preparation fixtures touch: the
// Sales tables a submitted round is made of, plus the Preparation tables this
// slice owns. Order matters only for readability; CASCADE does the work.
func truncatePrepTables(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`
		TRUNCATE preparation_unit_transitions, preparation_units, completed_sales,
		         order_items, orders, charge_allocations,
		         committed_item_modifier_options, committed_items, checks,
		         order_draft_item_modifier_options, order_draft_items, order_drafts,
		         table_assignments, service_sessions, cash_movements, sales_shifts,
		         tables, modifier_group_default_options, item_modifier_group_exclusions,
		         item_modifier_groups, category_modifier_groups, modifier_options,
		         modifier_groups, menu_item_sizes, menu_items, menu_categories,
		         idempotency_keys, audit_events, staff_access_sessions,
		         staff_operational_roles, staff_identities
		RESTART IDENTITY CASCADE`)
	require.NoError(t, err)
}

// seedActor creates an enabled identity with the given roles and an active
// access session, returning the Actor the Preparation executor expects.
func seedActor(t *testing.T, q *sqlc.Queries, roles []string) preparation.Actor {
	t.Helper()
	ctx := context.Background()

	code := testLoginCode("A")
	sharedActorHashOnce.Do(func() {
		var err error
		sharedActorHash, err = auth.HashPin("1234")
		require.NoError(t, err)
	})
	hash := sharedActorHash

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

	return preparation.Actor{StaffID: identity.ID, SessionID: session.ID}
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

// seedMenuCategory creates a Menu Category and returns its id.
func seedMenuCategory(t *testing.T, db *sql.DB, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	require.NoError(t, db.QueryRow(`
		INSERT INTO menu_categories (name, normalized_name)
		VALUES ($1, lower($1)) RETURNING id`, name).Scan(&id))
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

// seedTable creates a Table and returns its id.
func seedTable(t *testing.T, db *sql.DB, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	require.NoError(t, db.QueryRow(`
		INSERT INTO tables (name, normalized_name, available)
		VALUES ($1, lower($1), true) RETURNING id`, name).Scan(&id))
	return id
}

// prepEnv is the fixture world the Preparation suites share. It wraps the raw
// seeds (openPrepTestDB, truncatePrepTables, seedActor, seedOpenShift, and the
// catalog helpers) plus the two runners the boundary needs: a sales.Runner to
// drive rounds to the bar, and a preparation.Runner over the same pool to
// advance the resulting units.
//
// The advance helpers run the handler directly — these suites have no HTTP
// layer — and return the response, the status the HTTP layer would have
// answered with, and the error untouched, so tests can assert on all three.
type prepEnv struct {
	// DB and Queries are exported so later tasks' suites can reach into them
	// for direct SQL assertions.
	DB      *sql.DB
	Queries *sqlc.Queries

	SalesRunner       *sales.Runner
	PreparationRunner *preparation.Runner

	// manager holds both sales.operate and preparation.operate and drives the
	// Sales path; barista holds preparation.operate and runs the advances;
	// cashier holds sales.operate but not preparation.operate, which is what
	// makes the capability denial test meaningful — a Manager would prove
	// nothing.
	manager preparation.Actor
	barista preparation.Actor
	cashier preparation.Actor

	// ShiftID is the seeded open Sales Shift the fixture rounds are sold in.
	ShiftID uuid.UUID
	// CoffeeID is a directly priced Menu Item (25,000 VND) whose Category
	// carries the Topping group.
	CoffeeID uuid.UUID
	// TableID is one available dine-in Table.
	TableID uuid.UUID
}

// newPrepEnv opens the test database, truncates every Sales and Preparation
// table, and seeds the shared world: a manager, a barista, and a cashier, an
// open Shift, and a small Catalog mirroring the Sales env's (one category with
// a surcharged Topping group, one priced item, one Table). Nothing session-side
// is seeded; tests submit rounds through the env.
func newPrepEnv(t *testing.T) *prepEnv {
	t.Helper()
	db, q := openPrepTestDB(t)
	truncatePrepTables(t, db)

	env := &prepEnv{
		DB:                db,
		Queries:           q,
		SalesRunner:       sales.NewRunner(db, q),
		PreparationRunner: preparation.NewRunner(db, q),
	}
	env.manager = seedActor(t, q, []string{auth.RoleManager})
	env.barista = seedActor(t, q, []string{auth.RoleBarista})
	env.cashier = seedActor(t, q, []string{auth.RoleCashier})
	env.ShiftID = seedOpenShift(t, q, env.manager.StaffID)

	coffeeCategoryID := seedMenuCategory(t, db, "Cà phê")
	toppingGroupID := seedModifierGroup(t, db, "Topping")
	attachGroupToCategory(t, db, coffeeCategoryID, toppingGroupID)
	toppingOptionID := seedModifierOption(t, db, toppingGroupID, "Trân châu")
	_, err := db.Exec(`UPDATE modifier_options SET surcharge_vnd = 5000 WHERE id = $1`,
		toppingOptionID)
	require.NoError(t, err)
	env.CoffeeID = seedMenuItemInCategory(t, db, coffeeCategoryID, "Cà phê sữa", 25000)
	env.TableID = seedTable(t, db, "Bàn 1")

	return env
}

// salesActor converts a Preparation actor into the equivalent Sales actor, so
// the manager can drive internal/sales handlers across the boundary.
func (e *prepEnv) salesActor(actor preparation.Actor) sales.Actor {
	return sales.Actor{StaffID: actor.StaffID, SessionID: actor.SessionID}
}

// SubmittedUnits returns the Preparation Units of a freshly submitted dine-in
// round of the given quantity.
//
// It reaches across the ADR-024 boundary through internal/sales' exported
// handlers — a test file may import sales — because a Service Session that has
// reached Submit is the only way Preparation Units come to exist. The manager
// drives the whole path.
func (e *prepEnv) SubmittedUnits(t *testing.T, quantity int32) []sales.PreparationUnitResponse {
	t.Helper()
	require.GreaterOrEqual(t, quantity, int32(1), "the round needs at least one unit")

	ctx := context.Background()
	actor := e.salesActor(e.manager)

	_, session, err := sales.NewStartDineInSessionHandler(e.SalesRunner).
		Handle(ctx, actor, sales.StartDineInSessionCommand{
			RequestID: uuid.New(),
			TableIDs:  []uuid.UUID{e.TableID},
		})
	require.NoError(t, err)

	_, resp, err := sales.NewAddDraftItemHandler(e.SalesRunner).
		Handle(ctx, actor, sales.AddDraftItemCommand{
			RequestID:        uuid.New(),
			ServiceSessionID: session.ID,
			MenuItemID:       e.CoffeeID,
		})
	require.NoError(t, err)
	require.NotEmpty(t, resp.Draft.Items, "the added item must be in the draft")

	_, _, err = sales.NewSetDraftItemQuantityHandler(e.SalesRunner).
		Handle(ctx, actor, sales.SetDraftItemQuantityCommand{
			RequestID:        uuid.New(),
			ServiceSessionID: session.ID,
			DraftItemID:      resp.Draft.Items[0].ID,
			Quantity:         &quantity,
		})
	require.NoError(t, err)

	_, _, err = sales.NewCommitOrderDraftHandler(e.SalesRunner).
		Handle(ctx, actor, sales.CommitOrderDraftCommand{
			RequestID:        uuid.New(),
			ServiceSessionID: session.ID,
		})
	require.NoError(t, err)

	_, submitted, err := sales.NewSubmitOrderHandler(e.SalesRunner).
		Handle(ctx, actor, sales.SubmitOrderCommand{
			RequestID:        uuid.New(),
			ServiceSessionID: session.ID,
		})
	require.NoError(t, err)

	require.Len(t, submitted.PreparationUnits, int(quantity),
		"one Preparation Unit per unit of ordered quantity")
	return submitted.PreparationUnits
}

// advance runs the advance command under a caller-chosen request id as the
// given actor, deriving the status the HTTP layer would have answered with on
// error the way the Sales suites' mapErrorStatus does.
func (e *prepEnv) advance(t *testing.T, requestID uuid.UUID, actor preparation.Actor,
	unitID uuid.UUID, target string,
) (preparation.UnitResponse, int, error) {
	t.Helper()
	status, resp, err := preparation.NewAdvanceUnitHandler(e.PreparationRunner).
		Handle(context.Background(), actor, preparation.AdvanceUnitCommand{
			RequestID:   requestID,
			UnitID:      unitID,
			TargetState: target,
		})
	if err != nil {
		status, _ = preparation.ErrorResponse(err)
	}
	return resp, status, err
}

// Advance runs the advance command as the Barista, who holds
// preparation.operate.
func (e *prepEnv) Advance(t *testing.T, unitID uuid.UUID, target string) (
	preparation.UnitResponse, int, error,
) {
	t.Helper()
	return e.advance(t, uuid.New(), e.barista, unitID, target)
}

// AdvanceAs runs the advance command as an arbitrary actor, for capability
// tests.
func (e *prepEnv) AdvanceAs(t *testing.T, actor preparation.Actor, unitID uuid.UUID,
	target string,
) (preparation.UnitResponse, int, error) {
	t.Helper()
	return e.advance(t, uuid.New(), actor, unitID, target)
}

// AdvanceWithRequestID replays a specific request id, as the Barista.
func (e *prepEnv) AdvanceWithRequestID(t *testing.T, requestID, unitID uuid.UUID,
	target string,
) (preparation.UnitResponse, int, error) {
	t.Helper()
	return e.advance(t, requestID, e.barista, unitID, target)
}

// UnitState reads a unit's state straight from the database.
func (e *prepEnv) UnitState(t *testing.T, unitID uuid.UUID) string {
	t.Helper()
	var state string
	require.NoError(t, e.DB.QueryRow(
		`SELECT state FROM preparation_units WHERE id = $1`, unitID).Scan(&state))
	return state
}

// CountTransitions counts the transition rows for a unit.
func (e *prepEnv) CountTransitions(t *testing.T, unitID uuid.UUID) int {
	t.Helper()
	var n int
	require.NoError(t, e.DB.QueryRow(
		`SELECT count(*) FROM preparation_unit_transitions
		 WHERE preparation_unit_id = $1`, unitID).Scan(&n))
	return n
}

// CashierActor returns the seeded CASHIER actor, who holds sales.operate but
// not preparation.operate.
func (e *prepEnv) CashierActor() preparation.Actor { return e.cashier }
