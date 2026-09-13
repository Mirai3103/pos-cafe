package shift

import (
	"database/sql"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/labstack/echo/v4"
)

// Slices aggregates the Shift handlers.
type Slices struct {
	Runner *Runner

	Current            *CurrentShiftHandler
	OpenShift          *OpenShiftHandler
	RecordCashMovement *RecordCashMovementHandler
}

// NewSlices wires every Shift handler onto a shared Runner.
func NewSlices(db *sql.DB, queries *sqlc.Queries) *Slices {
	runner := NewRunner(db, queries)
	return &Slices{
		Runner:             runner,
		Current:            NewCurrentShiftHandler(runner),
		OpenShift:          NewOpenShiftHandler(runner),
		RecordCashMovement: NewRecordCashMovementHandler(runner),
	}
}

// RegisterRoutes mounts the Shift routes under /shifts on the provided group.
//
// Routes are mounted directly on v1 (rather than a /shifts sub-group carrying
// RequireAuth) because any echo.Group holding group-level middleware also
// auto-registers two echo_route_not_found catch-all routes, which would make
// the router expose more than the three Shift routes. This matches the
// reasoning already recorded in internal/tables/routes.go.
func (s *Slices) RegisterRoutes(v1 *echo.Group, authn *auth.Middleware) {
	v1.GET("/shifts/current", s.handleGetCurrent,
		authn.RequireAuth(), authn.RequireCapability(CapSalesShiftOperate))
	v1.POST("/shifts", s.handleOpenShift,
		authn.RequireAuth(), authn.RequireCapability(CapSalesShiftOperate))
	v1.POST("/shifts/:shift_id/cash-movements", s.handleRecordCashMovement,
		authn.RequireAuth(), authn.RequireCapability(CapSalesShiftOperate))
}
