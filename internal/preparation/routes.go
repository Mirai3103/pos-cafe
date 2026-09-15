package preparation

import (
	"database/sql"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/labstack/echo/v4"
)

// Slices aggregates the Preparation handlers.
type Slices struct {
	Runner *Runner

	AdvanceUnit *AdvanceUnitHandler
}

// NewSlices wires every Preparation handler onto a shared Runner.
func NewSlices(db *sql.DB, queries *sqlc.Queries) *Slices {
	runner := NewRunner(db, queries)
	return &Slices{
		Runner:      runner,
		AdvanceUnit: NewAdvanceUnitHandler(runner),
	}
}

// RegisterRoutes mounts the Preparation routes under /preparation.
//
// Routes are mounted directly on v1 rather than a /preparation sub-group
// carrying RequireAuth, because any echo.Group holding group-level middleware
// also auto-registers two echo_route_not_found catch-all routes. This matches
// internal/sales, internal/tables, and internal/shift.
func (s *Slices) RegisterRoutes(v1 *echo.Group, authn *auth.Middleware) {
	v1.POST("/preparation/units/:unit_id/advance", s.handleAdvanceUnit,
		authn.RequireAuth(), authn.RequireCapability(CapPreparationOperate))
}
