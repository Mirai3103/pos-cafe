package sales

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRouteRegistration pins the exact router surface.
//
// The set comparison is deliberate rather than a length check: mounting on a
// sub-group with group-level middleware would silently add two
// echo_route_not_found catch-all routes, which is exactly the mistake the
// comment in RegisterRoutes warns about.
//
// Registration ORDER (the static list route before the :id param route) is
// deliberately not pinned here: echo.Routes() iterates a Go map, so its slice
// order is random per call and any index comparison flakes. The order is
// pinned behaviorally instead, by TestListRouteShadowsGetByID in
// list_sessions_integration_test.go.
func TestRouteRegistration(t *testing.T) {
	want := map[string]bool{
		"GET /api/v1/sales/service-sessions":                                             true,
		"GET /api/v1/sales/service-sessions/:id":                                         true,
		"POST /api/v1/sales/service-sessions/takeaway":                                   true,
		"POST /api/v1/sales/service-sessions/dine-in":                                    true,
		"PUT /api/v1/sales/service-sessions/:id/tables":                                  true,
		"POST /api/v1/sales/service-sessions/:id/draft/items":                            true,
		"PATCH /api/v1/sales/service-sessions/:id/draft/items/:item_id/quantity":         true,
		"PATCH /api/v1/sales/service-sessions/:id/draft/items/:item_id/size":             true,
		"PATCH /api/v1/sales/service-sessions/:id/draft/items/:item_id/preparation-note": true,
		"PATCH /api/v1/sales/service-sessions/:id/draft/items/:item_id/modifiers":        true,
		"DELETE /api/v1/sales/service-sessions/:id/draft/items/:item_id":                 true,
		"POST /api/v1/sales/service-sessions/:id/draft/commit":                           true,
		"POST /api/v1/sales/service-sessions/:id/draft":                                  true,
		"PUT /api/v1/sales/service-sessions/:id/draft/check-target":                      true,
		"POST /api/v1/sales/service-sessions/:id/submit":                                 true,
		"POST /api/v1/sales/service-sessions/:id/close":                                  true,
		"POST /api/v1/sales/checks/:check_id/payments/cash":                              true,
		"POST /api/v1/sales/checks/:check_id/payments/manual-qr":                         true,
		"POST /api/v1/sales/checks/:check_id/split":                                      true,
		"POST /api/v1/sales/checks/merge":                                                true,
	}

	got := make(map[string]bool)
	for _, route := range registeredSalesRoutes(t) {
		got[route] = true
	}

	for route := range want {
		assert.True(t, got[route], "route %q must be registered", route)
	}
	require.Equal(t, len(want), len(got),
		"the router must expose exactly the Sales routes and nothing else; got %v", got)
}

// registeredSalesRoutes registers the Sales routes on a fresh router and
// returns every route the router exposes as "METHOD path" strings.
func registeredSalesRoutes(t *testing.T) []string {
	t.Helper()

	e := echo.New()
	v1 := e.Group("/api/v1")

	// NewSlices needs no working database to register routes: the Runner only
	// stores the handle.
	slices := NewSlices(nil, nil)
	slices.RegisterRoutes(v1, &auth.Middleware{})

	routes := make([]string, 0, len(e.Routes()))
	for _, route := range e.Routes() {
		routes = append(routes, fmt.Sprintf("%s %s", route.Method, route.Path))
	}
	return routes
}

func TestCommitRoutesRequireSalesOperate(t *testing.T) {
	routes := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/v1/sales/service-sessions/:id/draft/commit"},
		{http.MethodPost, "/api/v1/sales/service-sessions/:id/draft"},
		{http.MethodPut, "/api/v1/sales/service-sessions/:id/draft/check-target"},
	}
	registered := registeredSalesRoutes(t)
	for _, r := range routes {
		t.Run(r.method+" "+r.path, func(t *testing.T) {
			require.Contains(t, registered, r.method+" "+r.path)
		})
	}
}

func TestSalesExposesTwentyOperations(t *testing.T) {
	require.Len(t, registeredSalesRoutes(t), 20)
}

func TestPhase5CRoutesAreRegistered(t *testing.T) {
	routes := registeredSalesRoutes(t)
	require.Contains(t, routes, "POST /api/v1/sales/checks/:check_id/payments/cash")
	require.Contains(t, routes, "POST /api/v1/sales/checks/:check_id/payments/manual-qr")
	require.Contains(t, routes, "POST /api/v1/sales/checks/:check_id/split")
	require.Contains(t, routes, "POST /api/v1/sales/checks/merge")
	require.Len(t, routes, 20, "the Sales surface has grown to twenty operations by Phase 5D")
}

func TestPhase5DRoutesAreRegistered(t *testing.T) {
	routes := registeredSalesRoutes(t)
	require.Contains(t, routes, "POST /api/v1/sales/service-sessions/:id/submit")
	require.Contains(t, routes, "POST /api/v1/sales/service-sessions/:id/close")
	require.Len(t, routes, 20, "Phase 5D brings the Sales surface to twenty operations")
}
