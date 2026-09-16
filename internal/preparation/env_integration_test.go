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

// knownActorPINs records the plaintext PIN of every seeded actor, keyed by the
// identity id. The plaintext stays out of the production preparation.Actor on
// purpose: Actor is what the executor and handlers see, and no production path
// may carry a PIN. The executor's Manager self-PIN gate consumes the plaintext
// only through these test fixtures.
var knownActorPINs sync.Map // staff_identity_id -> plaintext test PIN

// seedKnownPIN registers a seeded actor's plaintext test PIN.
func seedKnownPIN(staffID uuid.UUID, pin string) {
	knownActorPINs.Store(staffID, pin)
}

// knownPIN returns the plaintext test PIN registered for a seeded actor, or ""
// when the actor was not seeded by this package.
func knownPIN(actor preparation.Actor) string {
	pin, _ := knownActorPINs.Load(actor.StaffID)
	pinText, _ := pin.(string)
	return pinText
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
// already migrated, so tests never run migrations themselves. The same pool is
// handed to the package's in-process (internal) integration tests, which
// cannot reach this file's unexported fixtures.
func TestMain(m *testing.M) {
	os.Exit(testdb.Run(m, "preparation", func(db *sql.DB) {
		prepTestDB = db
		preparation.SetInternalTestDB(db)
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
// access session, returning the Actor the Preparation executor expects. The
// identity's plaintext test PIN ("1234") is registered in knownActorPINs so
// the Manager self-PIN gate tests can supply it; it never travels on the
// returned Actor.
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
	seedKnownPIN(identity.ID, "1234")

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

// SubmittedTakeawayUnits returns the Preparation Units of a freshly submitted
// Takeaway round of the given quantity, driven through the same exported
// internal/sales handlers as SubmittedUnits.
//
// Takeaway settles before it submits: ModeRequiresSettlementBeforeSubmit makes
// an unpaid Check block Submit, so the round's Check is paid in cash between
// Commit and Submit, charged at the Check's own stored total.
func (e *prepEnv) SubmittedTakeawayUnits(t *testing.T, quantity int32) []sales.PreparationUnitResponse {
	t.Helper()
	require.GreaterOrEqual(t, quantity, int32(1), "the round needs at least one unit")

	ctx := context.Background()
	actor := e.salesActor(e.manager)

	_, session, err := sales.NewStartTakeawaySessionHandler(e.SalesRunner).
		Handle(ctx, actor, sales.StartTakeawaySessionCommand{RequestID: uuid.New()})
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

	_, committed, err := sales.NewCommitOrderDraftHandler(e.SalesRunner).
		Handle(ctx, actor, sales.CommitOrderDraftCommand{
			RequestID:        uuid.New(),
			ServiceSessionID: session.ID,
		})
	require.NoError(t, err)
	require.Len(t, committed.Checks, 1, "a committed takeaway round has one check")
	check := committed.Checks[0]

	_, _, err = sales.NewPayCashHandler(e.SalesRunner).
		Handle(ctx, actor, sales.PayCashCommand{
			RequestID:        uuid.New(),
			CheckID:          check.ID,
			AppliedAmountVND: check.ChargeVND,
			CashTenderedVND:  check.ChargeVND,
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

// SessionIDForUnit reads the Service Session a Preparation Unit belongs to,
// through the Order Item and Order the unit was made of.
func (e *prepEnv) SessionIDForUnit(t *testing.T, unitID uuid.UUID) uuid.UUID {
	t.Helper()
	var sessionID uuid.UUID
	require.NoError(t, e.DB.QueryRow(`
		SELECT o.service_session_id
		FROM preparation_units pu
		JOIN order_items oi ON oi.id = pu.order_item_id
		JOIN orders o ON o.id = oi.order_id
		WHERE pu.id = $1`, unitID).Scan(&sessionID))
	return sessionID
}

// SeedTable creates a Table in the env's database and returns its id.
func (e *prepEnv) SeedTable(t *testing.T, name string) uuid.UUID {
	t.Helper()
	return seedTable(t, e.DB, name)
}

// SetTables replaces a Dine-in Session's Table set through the real Sales
// handler, so the queue's table projection is tested against the same writes
// production makes.
func (e *prepEnv) SetTables(t *testing.T, sessionID uuid.UUID, tableIDs ...uuid.UUID) {
	t.Helper()
	_, _, err := sales.NewSetSessionTablesHandler(e.SalesRunner).Handle(
		context.Background(), e.salesActor(e.manager), sales.SetSessionTablesCommand{
			RequestID: uuid.New(), ServiceSessionID: sessionID, TableIDs: tableIDs,
		},
	)
	require.NoError(t, err)
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

// BaristaActor returns the seeded BARISTA actor, who holds preparation.operate.
func (e *prepEnv) BaristaActor() preparation.Actor { return e.barista }

// ManagerActor returns the seeded MANAGER actor, who holds preparation.operate
// plus every other Manager capability and is the subject of the executor's
// self-PIN gate tests.
func (e *prepEnv) ManagerActor() preparation.Actor { return e.manager }

// PINOf returns the plaintext test PIN registered for a seeded actor.
func (e *prepEnv) PINOf(actor preparation.Actor) string {
	return knownPIN(actor)
}

// RotatePIN rotates a seeded actor's PIN to the new plaintext through the
// generated UpdateStaffPin query and refreshes the known-plaintext registry,
// so a later PINOf still reports the truth. Only digit PINs of a valid shape
// (4-8 digits) are accepted, mirroring auth.ValidatePinFormat.
func (e *prepEnv) RotatePIN(t *testing.T, actor preparation.Actor, newPIN string) {
	t.Helper()
	require.Regexp(t, `^\d{4,8}$`, newPIN, "test PINs must keep auth's PIN shape")
	hash, err := auth.HashPin(newPIN)
	require.NoError(t, err)
	require.NoError(t, e.Queries.UpdateStaffPin(context.Background(),
		sqlc.UpdateStaffPinParams{ID: actor.StaffID, PinHash: hash}))
	seedKnownPIN(actor.StaffID, newPIN)
}

// ReplaceRoles swaps a seeded actor's operational roles for exactly the given
// set, through the generated ClearStaffRoles and AddStaffRole queries.
func (e *prepEnv) ReplaceRoles(t *testing.T, actor preparation.Actor, roles []string) {
	t.Helper()
	require.NoError(t, e.Queries.ClearStaffRoles(context.Background(), actor.StaffID))
	for _, role := range roles {
		require.NoError(t, e.Queries.AddStaffRole(context.Background(), sqlc.AddStaffRoleParams{
			StaffIdentityID: actor.StaffID,
			Role:            role,
		}))
	}
}

// SetIdentityEnabled enables or disables a seeded actor's identity row.
func (e *prepEnv) SetIdentityEnabled(t *testing.T, actor preparation.Actor, enabled bool) {
	t.Helper()
	_, err := e.Queries.SetStaffEnabled(context.Background(),
		sqlc.SetStaffEnabledParams{ID: actor.StaffID, Enabled: enabled})
	require.NoError(t, err)
}

// CountAuditEvents counts the audit events of the given type. Used to assert
// exactly one denial evidence row per denied call.
func (e *prepEnv) CountAuditEvents(t *testing.T, eventType string) int {
	t.Helper()
	var n int
	require.NoError(t, e.DB.QueryRow(
		`SELECT count(*) FROM audit_events WHERE event_type = $1`, eventType,
	).Scan(&n))
	return n
}

// BulkAdvance runs the bulk advance command as the Barista, who holds
// preparation.operate, returning the status the HTTP layer would have answered
// with and the error untouched, like the other advance helpers.
func (e *prepEnv) BulkAdvance(t *testing.T, cmd preparation.BulkAdvanceCommand) (int, preparation.BulkAdvanceResponse, error) {
	t.Helper()
	return preparation.NewBulkAdvanceHandler(e.PreparationRunner).
		Handle(context.Background(), e.BaristaActor(), cmd)
}

// UnitAuditCount counts the PREPARATION_UNIT_ADVANCED audit events naming one
// Preparation Unit, so a test can prove a unit moved exactly once.
func (e *prepEnv) UnitAuditCount(t *testing.T, unitID uuid.UUID) int {
	t.Helper()
	return e.CountAuditEventsByTypeAndUnit(t, preparation.EventPreparationUnitAdvanced, unitID)
}

// CountAuditEventsByTypeAndUnit counts the audit events of one type whose
// details name one Preparation Unit. The generic form lets suites pin the
// audit trail of any unit-scoped Preparation event.
func (e *prepEnv) CountAuditEventsByTypeAndUnit(t *testing.T, eventType string, unitID uuid.UUID) int {
	t.Helper()
	var n int
	require.NoError(t, e.DB.QueryRow(`
		SELECT count(*)
		FROM audit_events
		WHERE event_type = $1
		  AND details->>'preparation_unit_id' = $2`,
		eventType, unitID.String(),
	).Scan(&n))
	return n
}

// --- Phase 6B: Waste fixtures ---

// waste runs the waste command under a caller-chosen request id as the given
// actor, deriving the status the HTTP layer would have answered with on error
// the way the advance helpers do.
func (e *prepEnv) waste(t *testing.T, requestID uuid.UUID, actor preparation.Actor,
	unitID uuid.UUID, reason string, note *string,
) (preparation.WasteResponse, int, error) {
	t.Helper()
	status, resp, err := preparation.NewWasteUnitHandler(e.PreparationRunner).
		Handle(context.Background(), actor, preparation.WasteUnitCommand{
			RequestID: requestID,
			UnitID:    unitID,
			Reason:    reason,
			Note:      note,
		})
	if err != nil {
		status, _ = preparation.ErrorResponse(err)
	}
	return resp, status, err
}

// Waste runs the waste command as the Barista, who holds preparation.operate.
func (e *prepEnv) Waste(t *testing.T, unitID uuid.UUID, reason string, note *string) (
	preparation.WasteResponse, int, error,
) {
	t.Helper()
	return e.waste(t, uuid.New(), e.barista, unitID, reason, note)
}

// WasteAs runs the waste command as an arbitrary actor, for capability tests.
func (e *prepEnv) WasteAs(t *testing.T, actor preparation.Actor, unitID uuid.UUID,
	reason string, note *string,
) (preparation.WasteResponse, int, error) {
	t.Helper()
	return e.waste(t, uuid.New(), actor, unitID, reason, note)
}

// WasteWithRequestID replays a specific request id, as the Barista.
func (e *prepEnv) WasteWithRequestID(t *testing.T, requestID, unitID uuid.UUID,
	reason string, note *string,
) (preparation.WasteResponse, int, error) {
	t.Helper()
	return e.waste(t, requestID, e.barista, unitID, reason, note)
}

// CountWastes counts the Waste facts recorded for one unit.
func (e *prepEnv) CountWastes(t *testing.T, unitID uuid.UUID) int {
	t.Helper()
	var n int
	require.NoError(t, e.DB.QueryRow(
		`SELECT count(*) FROM preparation_wastes WHERE preparation_unit_id = $1`,
		unitID).Scan(&n))
	return n
}

// CountAlerts counts the alerts created for one unit.
func (e *prepEnv) CountAlerts(t *testing.T, unitID uuid.UUID) int {
	t.Helper()
	var n int
	require.NoError(t, e.DB.QueryRow(
		`SELECT count(*) FROM preparation_alerts WHERE preparation_unit_id = $1`,
		unitID).Scan(&n))
	return n
}

// wasteFactRow is one stored Waste fact row as the fixtures read it back.
type wasteFactRow struct {
	ID         uuid.UUID
	PriorState string
	Reason     string
	Note       *string
	OccurredAt time.Time
	ActorID    uuid.UUID
	SessionID  uuid.UUID
}

// UnitWaste reads the single Waste fact of one unit; the suite requires
// exactly one row to exist before calling.
func (e *prepEnv) UnitWaste(t *testing.T, unitID uuid.UUID) wasteFactRow {
	t.Helper()
	var row wasteFactRow
	var note sql.NullString
	require.NoError(t, e.DB.QueryRow(`
		SELECT id, prior_state, reason, note, occurred_at,
		       actor_staff_identity_id, staff_access_session_id
		FROM preparation_wastes
		WHERE preparation_unit_id = $1`, unitID).
		Scan(&row.ID, &row.PriorState, &row.Reason, &note, &row.OccurredAt,
			&row.ActorID, &row.SessionID))
	if note.Valid {
		row.Note = &note.String
	}
	return row
}

// alertFactRow is one stored alert row as the fixtures read it back.
type alertFactRow struct {
	ID             uuid.UUID
	Kind           string
	Reason         string
	Note           *string
	CreatedAt      time.Time
	AcknowledgedAt *time.Time
}

// UnitAlert reads the single alert of one unit; the suite requires exactly one
// row to exist before calling.
func (e *prepEnv) UnitAlert(t *testing.T, unitID uuid.UUID) alertFactRow {
	t.Helper()
	var row alertFactRow
	var note sql.NullString
	var acknowledgedAt sql.NullTime
	require.NoError(t, e.DB.QueryRow(`
		SELECT id, kind, reason, note, created_at, acknowledged_at
		FROM preparation_alerts
		WHERE preparation_unit_id = $1`, unitID).
		Scan(&row.ID, &row.Kind, &row.Reason, &note, &row.CreatedAt, &acknowledgedAt))
	if note.Valid {
		row.Note = &note.String
	}
	if acknowledgedAt.Valid {
		row.AcknowledgedAt = &acknowledgedAt.Time
	}
	return row
}

// transitionFactRow is one stored unit transition row as the fixtures read it
// back.
type transitionFactRow struct {
	PriorState     string
	ResultingState string
	OccurredAt     time.Time
}

// UnitTransitionTo reads the single transition of one unit into the given
// resulting state; the suite requires exactly one row to exist before calling.
func (e *prepEnv) UnitTransitionTo(t *testing.T, unitID uuid.UUID,
	resulting string,
) transitionFactRow {
	t.Helper()
	var row transitionFactRow
	require.NoError(t, e.DB.QueryRow(`
		SELECT prior_state, resulting_state, occurred_at
		FROM preparation_unit_transitions
		WHERE preparation_unit_id = $1 AND resulting_state = $2`,
		unitID, resulting).
		Scan(&row.PriorState, &row.ResultingState, &row.OccurredAt))
	return row
}

// CountTransitionsTo counts the transitions of one unit into one resulting
// state.
func (e *prepEnv) CountTransitionsTo(t *testing.T, unitID uuid.UUID,
	resulting string,
) int {
	t.Helper()
	var n int
	require.NoError(t, e.DB.QueryRow(`
		SELECT count(*) FROM preparation_unit_transitions
		WHERE preparation_unit_id = $1 AND resulting_state = $2`,
		unitID, resulting).Scan(&n))
	return n
}

// UnitInPreparationAt reads a unit's in_preparation_at straight from the
// database, nil when the unit never entered preparation.
func (e *prepEnv) UnitInPreparationAt(t *testing.T, unitID uuid.UUID) *time.Time {
	t.Helper()
	var value sql.NullTime
	require.NoError(t, e.DB.QueryRow(
		`SELECT in_preparation_at FROM preparation_units WHERE id = $1`, unitID).
		Scan(&value))
	if !value.Valid {
		return nil
	}
	return &value.Time
}

// auditEventRow is one stored audit event naming a unit, as the fixtures read
// it back.
type auditEventRow struct {
	EventType  string
	OccurredAt time.Time
}

// UnitAuditEvents reads the audit events of the given types whose details name
// one unit, ordered deterministically, so a suite can pin the event types and
// their shared occurrence time.
func (e *prepEnv) UnitAuditEvents(t *testing.T, unitID uuid.UUID,
	eventTypes ...string,
) []auditEventRow {
	t.Helper()
	rows, err := e.DB.Query(`
		SELECT event_type, occurred_at
		FROM audit_events
		WHERE event_type = ANY($1)
		  AND details->>'preparation_unit_id' = $2
		ORDER BY event_type ASC, occurred_at ASC, id ASC`,
		eventTypes, unitID.String())
	require.NoError(t, err)
	defer rows.Close()
	events := []auditEventRow{}
	for rows.Next() {
		var event auditEventRow
		require.NoError(t, rows.Scan(&event.EventType, &event.OccurredAt))
		events = append(events, event)
	}
	require.NoError(t, rows.Err())
	return events
}

// idempotencyClaim is the stored idempotency claim of one mutation request.
type idempotencyClaim struct {
	Action       string
	RequestHash  string
	ResponseCode int32
	ResponseBody []byte
}

// IdempotencyClaim reads the idempotency claim an actor stored for a request
// id. The second result reports whether a claim exists, so tests can prove a
// denial left nothing claimed and a success stored its result.
func (e *prepEnv) IdempotencyClaim(t *testing.T, actor preparation.Actor,
	requestID uuid.UUID,
) (idempotencyClaim, bool) {
	t.Helper()
	var claim idempotencyClaim
	err := e.DB.QueryRow(`
		SELECT action, request_hash, response_code, response_body
		FROM idempotency_keys
		WHERE actor_id = $1 AND key = $2`,
		actor.StaffID, requestID,
	).Scan(&claim.Action, &claim.RequestHash, &claim.ResponseCode, &claim.ResponseBody)
	if err == sql.ErrNoRows {
		return idempotencyClaim{}, false
	}
	require.NoError(t, err)
	return claim, true
}
