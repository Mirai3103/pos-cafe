package sales

import (
	"fmt"
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
	}

	e := echo.New()
	v1 := e.Group("/api/v1")

	// NewSlices needs no working database to register routes: the Runner only
	// stores the handle.
	slices := NewSlices(nil, nil)
	slices.RegisterRoutes(v1, &auth.Middleware{})

	got := make(map[string]bool)
	for _, route := range e.Routes() {
		got[fmt.Sprintf("%s %s", route.Method, route.Path)] = true
	}

	for route := range want {
		assert.True(t, got[route], "route %q must be registered", route)
	}
	require.Equal(t, len(want), len(got),
		"the router must expose exactly the Sales routes and nothing else; got %v", got)
}
