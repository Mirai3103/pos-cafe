//go:build integration

package sales_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListActiveSessionsOrdersByCreation(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)

	_, first, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)
	_, second, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)

	// now() has microsecond resolution, so two Sessions opened back-to-back
	// can share one created_at, and the ORDER BY created_at, id tiebreak does
	// not preserve insertion order. Force distinct timestamps so the ordering
	// this test pins is deterministic.
	_, err = db.Exec(
		`UPDATE service_sessions SET created_at = now() - interval '1 minute' WHERE id = $1`,
		first.ID)
	require.NoError(t, err)

	got, err := sales.NewListActiveSessionsHandler(runner).Handle(context.Background(), actor)
	require.NoError(t, err)

	require.Len(t, got, 2)
	assert.Equal(t, first.ID, got[0].ID)
	assert.Equal(t, second.ID, got[1].ID)
}

func TestListActiveSessionsExcludesClosed(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)

	_, session, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)

	// 5D owns closure; the test drives the state directly.
	_, err = db.Exec(`UPDATE service_sessions SET state = 'CLOSED' WHERE id = $1`, session.ID)
	require.NoError(t, err)

	got, err := sales.NewListActiveSessionsHandler(runner).Handle(context.Background(), actor)
	require.NoError(t, err)
	assert.Empty(t, got)
}

// Each listed Session carries its full projection, including its draft, so the
// cashier's open-tabs screen needs one request rather than one per Session.
func TestListActiveSessionsIncludesDrafts(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"CASHIER"})
	seedOpenShift(t, q, actor.StaffID)
	menuItemID := seedMenuItem(t, db, "Cà phê sữa", 25000)

	_, session, err := startTakeaway(t, runner, actor, uuid.New())
	require.NoError(t, err)
	_, err = addItem(t, runner, actor, sales.AddDraftItemCommand{
		ServiceSessionID: session.ID, MenuItemID: menuItemID})
	require.NoError(t, err)

	got, err := sales.NewListActiveSessionsHandler(runner).Handle(context.Background(), actor)
	require.NoError(t, err)

	require.Len(t, got, 1)
	require.NotNil(t, got[0].Draft)
	assert.Len(t, got[0].Draft.Items, 1)
}

func TestListActiveSessionsDeniesBarista(t *testing.T) {
	db, q := openSalesTestDB(t)
	truncateSalesTables(t, db)
	runner := sales.NewRunner(db, q)

	actor := seedActor(t, q, []string{"BARISTA"})

	_, err := sales.NewListActiveSessionsHandler(runner).Handle(context.Background(), actor)
	require.ErrorIs(t, err, sales.ErrForbidden)
}

// TestListRouteShadowsGetByID pins behaviorally the registration order that
// RegisterRoutes ships: the static list route before the :id param route.
// Echo resolves static-before-param regardless, and echo.Routes() iterates a
// Go map, so its slice order is random and no index comparison can pin the
// order — behavior can. A request for the bare collection must be answered by
// the list handler: over an empty world that is 200 with "data":[]. If the
// request ever routed into the :id handler instead, the empty id would fail
// UUID parsing with 400 INVALID_INPUT.
func TestListRouteShadowsGetByID(t *testing.T) {
	e, _, q := newTestServer(t)
	cashier := signIn(t, e, q, []string{"CASHIER"}, "2468")
	barista := signIn(t, e, q, []string{"BARISTA"}, "1357")

	rec := doRequest(t, e, http.MethodGet, "/api/v1/sales/service-sessions", cashier, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"data":[]`)

	// The capability gate on the same path answers a BARISTA with 403 from the
	// middleware, before the handler runs.
	rec = doRequest(t, e, http.MethodGet, "/api/v1/sales/service-sessions", barista, nil)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

	var env envelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	require.NotNil(t, env.Error)
	assert.Equal(t, "FORBIDDEN", env.Error.Code)
}
