//go:build integration

package sales_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// salesEnv is the fluent fixture world the 5B suites share, wrapping the 5A
// primitives (openSalesTestDB, truncateSalesTables, seedActor, seedOpenShift,
// and the raw-SQL seed helpers) into one object. Each env owns a fresh
// database view: openSalesTestDB registers the close, and newSalesEnv
// truncates and reseeds, so tests are order-independent even within one run.
//
// Every handler-driving method runs the handler directly — these suites have
// no HTTP layer — asserting nothing unless the name says OK or Try.
type salesEnv struct {
	// DB and Queries are exported because later tasks' suites reach into them
	// for direct SQL assertions.
	DB      *sql.DB
	Queries *sqlc.Queries
	Runner  *sales.Runner

	// Actor is a MANAGER and holds sales.operate; every env method executes as
	// this actor. AsBarista returns a view that executes as a BARISTA, who
	// holds no sales.operate.
	Actor   sales.Actor
	barista sales.Actor

	// CoffeeID is a directly priced Menu Item (25,000 VND) whose Category
	// carries the Topping group.
	CoffeeID uuid.UUID
	// TeaID is a directly priced Menu Item (20,000 VND) with no modifier
	// groups.
	TeaID uuid.UUID
	// SizedItemID is priced only through its Sizes: its price_vnd is NULL.
	SizedItemID uuid.UUID
	// LargeSizeID is SizedItemID's only Size ("Lớn", 30,000 VND).
	LargeSizeID uuid.UUID
	// TableID is one available dine-in Table.
	TableID uuid.UUID
	// ToppingGroupID is effective for CoffeeID only (min 0, max 1).
	ToppingGroupID uuid.UUID
	// ToppingOptionID is ToppingGroupID's only option ("Trân châu",
	// surcharge 5,000 VND).
	ToppingOptionID uuid.UUID
}

// newSalesEnv opens the test database, truncates every Sales table, and seeds
// the shared world: an open Shift, a manager and a barista identity, and a
// small Catalog (two priced items, one sized item with its size, one Table,
// one Topping group with a surcharged option). Nothing session-side is seeded;
// tests open Sessions through the env.
func newSalesEnv(t *testing.T) *salesEnv {
	t.Helper()
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	env := &salesEnv{DB: db, Queries: q, Runner: runner}
	env.Actor = seedActor(t, q, []string{"MANAGER"})
	env.barista = seedActor(t, q, []string{"BARISTA"})
	seedOpenShift(t, q, env.Actor.StaffID)

	coffeeCategoryID := seedMenuCategory(t, db, "Cà phê")
	env.ToppingGroupID = seedModifierGroup(t, db, "Topping")
	attachGroupToCategory(t, db, coffeeCategoryID, env.ToppingGroupID)
	env.ToppingOptionID = seedModifierOption(t, db, env.ToppingGroupID, "Trân châu")
	_, err := db.Exec(`UPDATE modifier_options SET surcharge_vnd = 5000 WHERE id = $1`,
		env.ToppingOptionID)
	require.NoError(t, err)
	env.CoffeeID = seedMenuItemInCategory(t, db, coffeeCategoryID, "Cà phê sữa", 25000)

	env.TeaID = seedMenuItem(t, db, "Trà đào", 20000)
	env.SizedItemID = seedSizedMenuItem(t, db, "Trà sữa")
	env.LargeSizeID = seedSize(t, db, env.SizedItemID, "Lớn", 30000)
	env.TableID = seedTable(t, db, "Bàn 1")

	return env
}

// AsBarista returns a view of the env that executes every handler as a
// BARISTA. It shares the database connection, queries, and seeded ids; only
// the actor differs.
func (e *salesEnv) AsBarista() *salesEnv {
	view := *e
	view.Actor = e.barista
	return &view
}

// ---------- Session lifecycle ----------

// StartTakeaway opens a Takeaway Session and returns its projection.
func (e *salesEnv) StartTakeaway(t *testing.T) sales.ServiceSessionResponse {
	t.Helper()
	_, resp, err := sales.NewStartTakeawaySessionHandler(e.Runner).
		Handle(context.Background(), e.Actor,
			sales.StartTakeawaySessionCommand{RequestID: uuid.New()})
	require.NoError(t, err)
	return resp
}

// StartDineIn opens a Dine-in Session against one Table and returns its
// projection.
func (e *salesEnv) StartDineIn(t *testing.T, tableID uuid.UUID) sales.ServiceSessionResponse {
	t.Helper()
	_, resp, err := sales.NewStartDineInSessionHandler(e.Runner).
		Handle(context.Background(), e.Actor,
			sales.StartDineInSessionCommand{RequestID: uuid.New(), TableIDs: []uuid.UUID{tableID}})
	require.NoError(t, err)
	return resp
}

// CloseShift closes the open Sales Shift directly: Phase 4 ships no close
// operation, and the seed must not depend on one.
func (e *salesEnv) CloseShift(t *testing.T) {
	t.Helper()
	_, err := e.DB.Exec(`UPDATE sales_shifts SET state = 'CLOSED' WHERE state = 'OPEN'`)
	require.NoError(t, err)
}

// ---------- Draft maintenance ----------

// AddDraftItem adds one unit of the item, letting the menu's defaults apply.
// sizeID may be nil.
func (e *salesEnv) AddDraftItem(t *testing.T, sessionID, itemID uuid.UUID,
	sizeID *uuid.UUID,
) sales.ServiceSessionResponse {
	t.Helper()
	_, resp, err := sales.NewAddDraftItemHandler(e.Runner).
		Handle(context.Background(), e.Actor, sales.AddDraftItemCommand{
			RequestID:        uuid.New(),
			ServiceSessionID: sessionID,
			MenuItemID:       itemID,
			SizeID:           sizeID,
		})
	require.NoError(t, err)
	return resp
}

// AddDraftItemWithOptions adds one unit of the item with exactly the given
// option selected, declining the menu's defaults.
func (e *salesEnv) AddDraftItemWithOptions(t *testing.T, sessionID, itemID,
	optionID uuid.UUID,
) sales.ServiceSessionResponse {
	t.Helper()
	optionIDs := []uuid.UUID{optionID}
	_, resp, err := sales.NewAddDraftItemHandler(e.Runner).
		Handle(context.Background(), e.Actor, sales.AddDraftItemCommand{
			RequestID:         uuid.New(),
			ServiceSessionID:  sessionID,
			MenuItemID:        itemID,
			ModifierOptionIDs: &optionIDs,
		})
	require.NoError(t, err)
	return resp
}

// ---------- Check targeting ----------

// SeedEditableDraft inserts an EDITABLE draft directly. 5B's
// START_NEW_ORDER_DRAFT refuses while a COMMITTED draft has no Order, which
// no phase before 5D can produce, so the reuse path is reachable only this
// way. See the spec's accepted consequences.
func (e *salesEnv) SeedEditableDraft(t *testing.T, sessionID uuid.UUID, target string) {
	t.Helper()
	_, err := e.DB.Exec(
		`INSERT INTO order_drafts (service_session_id, state, check_target)
		 VALUES ($1, 'EDITABLE', $2)`, sessionID, target)
	require.NoError(t, err)
}

// SetCheckTarget sets the check_target of the Session's current EDITABLE draft
// directly with SQL. This is fixture seeding, not a handler call; tests that
// must exercise the SetCheckTargetHandler use TrySetCheckTarget instead.
func (e *salesEnv) SetCheckTarget(t *testing.T, sessionID uuid.UUID, target string) {
	t.Helper()
	res, err := e.DB.Exec(`
		UPDATE order_drafts
		SET check_target = $2
		WHERE id = (
			SELECT id
			FROM order_drafts
			WHERE service_session_id = $1 AND state = 'EDITABLE'
			ORDER BY created_at DESC, id DESC
			LIMIT 1
		)`, sessionID, target)
	require.NoError(t, err)
	n, err := res.RowsAffected()
	require.NoError(t, err)
	require.Equal(t, int64(1), n, "exactly one EDITABLE draft for session %s", sessionID)
}

// TrySetCheckTarget runs SetCheckTarget and returns its error untouched.
func (e *salesEnv) TrySetCheckTarget(t *testing.T, sessionID uuid.UUID, target string) (
	sales.ServiceSessionResponse, error,
) {
	t.Helper()
	_, resp, err := sales.NewSetCheckTargetHandler(e.Runner).
		Handle(context.Background(), e.Actor, sales.SetCheckTargetCommand{
			RequestID:        uuid.New(),
			ServiceSessionID: sessionID,
			CheckTarget:      target,
		})
	return resp, err
}

// ---------- Commit ----------

// Commit runs Commit and fails the test when it errors.
func (e *salesEnv) Commit(t *testing.T, sessionID uuid.UUID) sales.ServiceSessionResponse {
	t.Helper()
	resp, err := e.TryCommitWithRequestID(t, sessionID, uuid.New())
	require.NoError(t, err)
	return resp
}

// TryCommit runs Commit with a fresh request id and returns its error.
func (e *salesEnv) TryCommit(t *testing.T, sessionID uuid.UUID) (
	sales.ServiceSessionResponse, error,
) {
	t.Helper()
	return e.TryCommitWithRequestID(t, sessionID, uuid.New())
}

// CommitWithRequestID runs Commit under a caller-chosen request id, so a test
// can drive the idempotency replay itself.
func (e *salesEnv) CommitWithRequestID(t *testing.T, sessionID, requestID uuid.UUID,
) sales.ServiceSessionResponse {
	t.Helper()
	resp, err := e.TryCommitWithRequestID(t, sessionID, requestID)
	require.NoError(t, err)
	return resp
}

// TryCommitWithRequestID runs Commit under a caller-chosen request id and
// returns the response and error untouched.
func (e *salesEnv) TryCommitWithRequestID(t *testing.T, sessionID, requestID uuid.UUID) (
	sales.ServiceSessionResponse, error,
) {
	t.Helper()
	_, resp, err := sales.NewCommitOrderDraftHandler(e.Runner).
		Handle(context.Background(), e.Actor, sales.CommitOrderDraftCommand{
			RequestID:        requestID,
			ServiceSessionID: sessionID,
		})
	return resp, err
}

// commitOneItemSession opens a Takeaway Session, adds one item, and commits.
func (e *salesEnv) commitOneItemSession(t *testing.T) sales.ServiceSessionResponse {
	t.Helper()
	session := e.StartTakeaway(t)
	e.AddDraftItem(t, session.ID, e.CoffeeID, nil)
	return e.Commit(t, session.ID)
}

// ---------- Round lifecycle ----------

// TryStartNewDraft runs StartNewOrderDraft and returns its error untouched.
func (e *salesEnv) TryStartNewDraft(t *testing.T, sessionID uuid.UUID) (
	sales.ServiceSessionResponse, error,
) {
	t.Helper()
	_, resp, err := sales.NewStartNewOrderDraftHandler(e.Runner).
		Handle(context.Background(), e.Actor, sales.StartNewOrderDraftCommand{
			RequestID:        uuid.New(),
			ServiceSessionID: sessionID,
		})
	return resp, err
}

// ---------- Reads ----------

// GetSession returns the Session projection and its error untouched, for
// tests that assert on the error.
func (e *salesEnv) GetSession(t *testing.T, sessionID uuid.UUID) (
	sales.ServiceSessionResponse, error,
) {
	t.Helper()
	return sales.NewGetServiceSessionHandler(e.Runner).
		Handle(context.Background(), e.Actor, sessionID)
}

// GetSessionOK returns the Session projection, failing the test on error.
func (e *salesEnv) GetSessionOK(t *testing.T, sessionID uuid.UUID) sales.ServiceSessionResponse {
	t.Helper()
	got, err := e.GetSession(t, sessionID)
	require.NoError(t, err)
	return got
}

// ---------- Catalog mutations ----------

// SetMenuItemAvailable flips a Menu Item's availability flag.
func (e *salesEnv) SetMenuItemAvailable(t *testing.T, itemID uuid.UUID, available bool) {
	t.Helper()
	_, err := e.DB.Exec(`UPDATE menu_items SET available = $2 WHERE id = $1`, itemID, available)
	require.NoError(t, err)
}

// RetireMenuItem retires a Menu Item. The retirement consistency check
// requires a reason, so one is always written.
func (e *salesEnv) RetireMenuItem(t *testing.T, itemID uuid.UUID) {
	t.Helper()
	_, err := e.DB.Exec(`
		UPDATE menu_items
		SET retired_at = now(), retirement_reason = 'test'
		WHERE id = $1`, itemID)
	require.NoError(t, err)
}

// RetireSize retires a Menu Item Size.
func (e *salesEnv) RetireSize(t *testing.T, sizeID uuid.UUID) {
	t.Helper()
	_, err := e.DB.Exec(`
		UPDATE menu_item_sizes
		SET retired_at = now(), retirement_reason = 'test'
		WHERE id = $1`, sizeID)
	require.NoError(t, err)
}

// SetSizeAvailable flips a Size's availability flag.
func (e *salesEnv) SetSizeAvailable(t *testing.T, sizeID uuid.UUID, available bool) {
	t.Helper()
	_, err := e.DB.Exec(`UPDATE menu_item_sizes SET available = $2 WHERE id = $1`,
		sizeID, available)
	require.NoError(t, err)
}

// RetireOption retires a Modifier Option.
func (e *salesEnv) RetireOption(t *testing.T, optionID uuid.UUID) {
	t.Helper()
	_, err := e.DB.Exec(`
		UPDATE modifier_options
		SET retired_at = now(), retirement_reason = 'test'
		WHERE id = $1`, optionID)
	require.NoError(t, err)
}

// SetOptionAvailable flips a Modifier Option's availability flag.
func (e *salesEnv) SetOptionAvailable(t *testing.T, optionID uuid.UUID, available bool) {
	t.Helper()
	_, err := e.DB.Exec(`UPDATE modifier_options SET available = $2 WHERE id = $1`,
		optionID, available)
	require.NoError(t, err)
}

// ExcludeGroupFromItem excludes an Item from a Category-inherited Group, the
// row Catalog's own exclusion feature writes.
func (e *salesEnv) ExcludeGroupFromItem(t *testing.T, itemID, groupID uuid.UUID) {
	t.Helper()
	_, err := e.DB.Exec(`
		INSERT INTO item_modifier_group_exclusions (menu_item_id, modifier_group_id)
		VALUES ($1, $2)`, itemID, groupID)
	require.NoError(t, err)
}

// RetireGroup retires a Modifier Group.
func (e *salesEnv) RetireGroup(t *testing.T, groupID uuid.UUID) {
	t.Helper()
	_, err := e.DB.Exec(`
		UPDATE modifier_groups
		SET retired_at = now(), retirement_reason = 'test'
		WHERE id = $1`, groupID)
	require.NoError(t, err)
}

// SetGroupSelectionBounds rewrites a Group's min/max selection counts.
func (e *salesEnv) SetGroupSelectionBounds(t *testing.T, groupID uuid.UUID, min, max int32) {
	t.Helper()
	_, err := e.DB.Exec(`UPDATE modifier_groups SET min_selections = $2, max_selections = $3
		WHERE id = $1`, groupID, min, max)
	require.NoError(t, err)
}

// RenameMenuItem renames a Menu Item, as a later Catalog edit would.
func (e *salesEnv) RenameMenuItem(t *testing.T, itemID uuid.UUID, name string) {
	t.Helper()
	_, err := e.DB.Exec(`UPDATE menu_items SET name = $2, normalized_name = lower($2)
		WHERE id = $1`, itemID, name)
	require.NoError(t, err)
}

// RenameOption renames a Modifier Option, as a later Catalog edit would.
func (e *salesEnv) RenameOption(t *testing.T, optionID uuid.UUID, name string) {
	t.Helper()
	_, err := e.DB.Exec(`UPDATE modifier_options SET name = $2, normalized_name = lower($2)
		WHERE id = $1`, optionID, name)
	require.NoError(t, err)
}

// ---------- Assertions ----------

// RequireNoChecks asserts the Session has no Checks at all.
func (e *salesEnv) RequireNoChecks(t *testing.T, sessionID uuid.UUID) {
	t.Helper()
	e.RequireCheckCount(t, sessionID, 0)
}

// RequireCheckCount asserts the Session's exact Check count.
func (e *salesEnv) RequireCheckCount(t *testing.T, sessionID uuid.UUID, want int) {
	t.Helper()
	var got int
	require.NoError(t, e.DB.QueryRow(
		`SELECT count(*) FROM checks WHERE service_session_id = $1`, sessionID).Scan(&got))
	require.Equal(t, want, got, "checks rows for session %s", sessionID)
}

// RequireNoCommittedItems asserts no Committed Item exists anywhere in the
// Session's drafts.
func (e *salesEnv) RequireNoCommittedItems(t *testing.T, sessionID uuid.UUID) {
	t.Helper()
	var got int
	require.NoError(t, e.DB.QueryRow(`
		SELECT count(*)
		FROM committed_items ci
		JOIN order_drafts d ON d.id = ci.order_draft_id
		WHERE d.service_session_id = $1`, sessionID).Scan(&got))
	require.Equal(t, 0, got, "committed_items rows for session %s", sessionID)
}

// RequireDraftState asserts the Session's latest draft's state.
func (e *salesEnv) RequireDraftState(t *testing.T, sessionID uuid.UUID, state string) {
	t.Helper()
	var got string
	require.NoError(t, e.DB.QueryRow(`
		SELECT state
		FROM order_drafts
		WHERE service_session_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT 1`, sessionID).Scan(&got))
	require.Equal(t, state, got, "draft state for session %s", sessionID)
}
