package tables

import (
	"database/sql"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/labstack/echo/v4"
)

// Slices aggregates the Tables handlers.
type Slices struct {
	Runner *Runner

	Overview             *OverviewHandler
	CreateTable          *CreateTableHandler
	RenameTable          *RenameTableHandler
	SetTableAvailability *SetTableAvailabilityHandler
}

// NewSlices wires every Tables handler onto a shared Runner.
func NewSlices(db *sql.DB, queries *sqlc.Queries) *Slices {
	runner := NewRunner(db, queries)
	return &Slices{
		Runner:               runner,
		Overview:             NewOverviewHandler(runner),
		CreateTable:          NewCreateTableHandler(runner),
		RenameTable:          NewRenameTableHandler(runner),
		SetTableAvailability: NewSetTableAvailabilityHandler(runner),
	}
}

// RegisterRoutes mounts the Tables routes under /tables on the provided group.
//
// Routes are mounted directly on v1 (rather than a /tables sub-group carrying
// RequireAuth) because any echo.Group that holds group-level middleware also
// auto-registers two echo_route_not_found catch-all routes, which would make
// the router expose more than the four Tables routes.
func (s *Slices) RegisterRoutes(v1 *echo.Group, authn *auth.Middleware) {
	v1.GET("/tables/overview", s.handleGetOverview, authn.RequireAuth(), authn.RequireCapability(CapSalesOperate))
	v1.POST("/tables", s.handleCreateTable, authn.RequireAuth(), authn.RequireCapability(CapTablesAdminister))
	v1.PATCH("/tables/:table_id/name", s.handleRenameTable, authn.RequireAuth(), authn.RequireCapability(CapTablesAdminister))
	v1.PATCH("/tables/:table_id/availability", s.handleSetTableAvailability, authn.RequireAuth(), authn.RequireCapability(CapTablesAdminister))
}
