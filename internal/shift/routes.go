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

	Current             *CurrentShiftHandler
	OpenShift           *OpenShiftHandler
	RecordCashMovement  *RecordCashMovementHandler
	StartReconciliation *StartReconciliationHandler
	RecordCashCount     *RecordCashCountHandler
	RecordQRObservation *RecordQRObservationHandler
	Close               *CloseShiftHandler
	ListClosedShifts    *ListClosedShiftsHandler
	GetClosedShift      *GetClosedShiftHandler
}

// NewSlices wires every Shift handler onto a shared Runner.
func NewSlices(db *sql.DB, queries *sqlc.Queries) *Slices {
	runner := NewRunner(db, queries)
	return &Slices{
		Runner:              runner,
		Current:             NewCurrentShiftHandler(runner),
		OpenShift:           NewOpenShiftHandler(runner),
		RecordCashMovement:  NewRecordCashMovementHandler(runner),
		StartReconciliation: NewStartReconciliationHandler(runner),
		RecordCashCount:     NewRecordCashCountHandler(runner),
		RecordQRObservation: NewRecordQRObservationHandler(runner),
		Close:               NewCloseShiftHandler(runner),
		ListClosedShifts:    NewListClosedShiftsHandler(runner),
		GetClosedShift:      NewGetClosedShiftHandler(runner),
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
	// The history reads require audit.inspect (ADR-052): only Managers hold
	// it, so a Cashier can still receive the close response of the Shift they
	// closed but cannot browse history. The list is registered before the
	// detail route so /shifts/current keeps matching its static segment; echo
	// ranks static segments above params either way, which the history HTTP
	// test pins by hitting all three shapes.
	v1.GET("/shifts", s.handleListClosedShifts,
		authn.RequireAuth(), authn.RequireCapability(CapAuditInspect))
	v1.GET("/shifts/:shift_id", s.handleGetClosedShift,
		authn.RequireAuth(), authn.RequireCapability(CapAuditInspect))
	v1.POST("/shifts", s.handleOpenShift,
		authn.RequireAuth(), authn.RequireCapability(CapSalesShiftOperate))
	v1.POST("/shifts/:shift_id/cash-movements", s.handleRecordCashMovement,
		authn.RequireAuth(), authn.RequireCapability(CapSalesShiftOperate))
	v1.POST("/shifts/:shift_id/reconciliation", s.handleStartReconciliation,
		authn.RequireAuth(), authn.RequireCapability(CapSalesShiftOperate))
	v1.POST("/shifts/:shift_id/reconciliation/cash-counts", s.handleRecordCashCount,
		authn.RequireAuth(), authn.RequireCapability(CapSalesShiftOperate))
	v1.POST("/shifts/:shift_id/reconciliation/qr-observations", s.handleRecordQRObservation,
		authn.RequireAuth(), authn.RequireCapability(CapSalesShiftOperate))
	v1.POST("/shifts/:shift_id/close", s.handleFinalClose,
		authn.RequireAuth(), authn.RequireCapability(CapSalesShiftOperate))
}
