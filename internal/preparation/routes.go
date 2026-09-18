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

	ActiveQueue *ActiveQueueHandler
	AdvanceUnit *AdvanceUnitHandler
	BulkAdvance *BulkAdvanceHandler

	// Phase 6B: Corrections & Recovery.
	WasteUnit        *WasteUnitHandler
	RemakeUnit       *RemakeUnitHandler
	AcknowledgeAlert *AcknowledgeAlertHandler
	CorrectState     *CorrectStateHandler

	// Phase 6C: Cancellation & Change.
	CancelUnits *CancelUnitsHandler
}

// NewSlices wires every Preparation handler onto a shared Runner.
func NewSlices(db *sql.DB, queries *sqlc.Queries) *Slices {
	runner := NewRunner(db, queries)
	return &Slices{
		Runner:           runner,
		ActiveQueue:      NewActiveQueueHandler(runner),
		AdvanceUnit:      NewAdvanceUnitHandler(runner),
		BulkAdvance:      NewBulkAdvanceHandler(runner),
		WasteUnit:        NewWasteUnitHandler(runner),
		RemakeUnit:       NewRemakeUnitHandler(runner),
		AcknowledgeAlert: NewAcknowledgeAlertHandler(runner),
		CorrectState:     NewCorrectStateHandler(runner),
		CancelUnits:      NewCancelUnitsHandler(runner),
	}
}

// RegisterRoutes mounts the Preparation routes under /preparation.
//
// Routes are mounted directly on v1 rather than a /preparation sub-group
// carrying RequireAuth, because any echo.Group holding group-level middleware
// also auto-registers two echo_route_not_found catch-all routes. This matches
// internal/sales, internal/tables, and internal/shift.
func (s *Slices) RegisterRoutes(v1 *echo.Group, authn *auth.Middleware) {
	v1.GET("/preparation/queue", s.handleActiveQueue,
		authn.RequireAuth(), authn.RequireCapability(CapPreparationOperate))
	v1.POST("/preparation/units/advance-many", s.handleBulkAdvance,
		authn.RequireAuth(), authn.RequireCapability(CapPreparationOperate))
	// Phase 6C registers its static path before the parameterized unit routes,
	// so /units/cancel can never be read as a unit id.
	v1.POST("/preparation/units/cancel", s.handleCancelUnits,
		authn.RequireAuth(), authn.RequireCapability(CapSalesOperate))
	v1.POST("/preparation/units/:unit_id/advance", s.handleAdvanceUnit,
		authn.RequireAuth(), authn.RequireCapability(CapPreparationOperate))

	// Phase 6B: Corrections & Recovery. Every mutation carries the same
	// capability middleware; State Correction's Manager role and PIN checks
	// live inside the mutation transaction, not in a middleware-only gate.
	v1.POST("/preparation/alerts/:alert_id/acknowledge", s.handleAcknowledgeAlert,
		authn.RequireAuth(), authn.RequireCapability(CapPreparationOperate))
	v1.POST("/preparation/units/:unit_id/waste", s.handleWasteUnit,
		authn.RequireAuth(), authn.RequireCapability(CapPreparationOperate))
	v1.POST("/preparation/wastes/:waste_id/remake", s.handleRemakeUnit,
		authn.RequireAuth(), authn.RequireCapability(CapPreparationOperate))
	v1.POST("/preparation/units/correct-state", s.handleCorrectState,
		authn.RequireAuth(), authn.RequireCapability(CapPreparationOperate))
}
