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
	StartTakeaway     *StartTakeawaySessionHandler
	StartDineIn       *StartDineInSessionHandler
	SetSessionTables  *SetSessionTablesHandler
	AddDraftItem      *AddDraftItemHandler
}

// NewSlices wires every Sales handler onto a shared Runner.
func NewSlices(db *sql.DB, queries *sqlc.Queries) *Slices {
	runner := NewRunner(db, queries)
	return &Slices{
		Runner:            runner,
		GetServiceSession: NewGetServiceSessionHandler(runner),
		StartTakeaway:     NewStartTakeawaySessionHandler(runner),
		StartDineIn:       NewStartDineInSessionHandler(runner),
		SetSessionTables:  NewSetSessionTablesHandler(runner),
		AddDraftItem:      NewAddDraftItemHandler(runner),
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
	v1.POST("/sales/service-sessions/takeaway", s.handleStartTakeaway,
		authn.RequireAuth(), authn.RequireCapability(CapSalesOperate))
	v1.POST("/sales/service-sessions/dine-in", s.handleStartDineIn,
		authn.RequireAuth(), authn.RequireCapability(CapSalesOperate))
	v1.PUT("/sales/service-sessions/:id/tables", s.handleSetSessionTables,
		authn.RequireAuth(), authn.RequireCapability(CapSalesOperate))
	v1.POST("/sales/service-sessions/:id/draft/items", s.handleAddDraftItem,
		authn.RequireAuth(), authn.RequireCapability(CapSalesOperate))
}
