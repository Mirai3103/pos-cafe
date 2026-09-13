package sales

import (
	"database/sql"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/labstack/echo/v4"
)

// Slices aggregates the Sales handlers.
type Slices struct {
	Runner *Runner

	GetServiceSession *GetServiceSessionHandler
}

// NewSlices wires every Sales handler onto a shared Runner.
func NewSlices(db *sql.DB, queries *sqlc.Queries) *Slices {
	runner := NewRunner(db, queries)
	return &Slices{
		Runner:            runner,
		GetServiceSession: NewGetServiceSessionHandler(runner),
	}
}

// RegisterRoutes mounts the Sales routes under /sales on the provided group.
//
// Routes are mounted directly on v1 rather than a /sales sub-group carrying
// RequireAuth, because any echo.Group holding group-level middleware also
// auto-registers two echo_route_not_found catch-all routes, which would make
// the router expose more than the Sales routes. This matches the reasoning
// already recorded in internal/tables/routes.go and internal/shift/routes.go.
func (s *Slices) RegisterRoutes(v1 *echo.Group, authn *auth.Middleware) {
	v1.GET("/sales/service-sessions/:id", s.handleGetServiceSession,
		authn.RequireAuth(), authn.RequireCapability(CapSalesOperate))
}
