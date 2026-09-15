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

	GetServiceSession  *GetServiceSessionHandler
	ListActiveSessions *ListActiveSessionsHandler
	StartTakeaway      *StartTakeawaySessionHandler
	StartDineIn        *StartDineInSessionHandler
	SetSessionTables   *SetSessionTablesHandler
	AddDraftItem       *AddDraftItemHandler
	SetItemQuantity    *SetDraftItemQuantityHandler
	SetItemSize        *SetDraftItemSizeHandler
	SetItemNote        *SetDraftItemNoteHandler
	SetItemModifiers   *SetDraftItemModifiersHandler
	RemoveDraftItem    *RemoveDraftItemHandler
	CommitDraft        *CommitOrderDraftHandler
	StartNewDraft      *StartNewOrderDraftHandler
	SetCheckTarget     *SetCheckTargetHandler
	SubmitOrder        *SubmitOrderHandler
	CloseSession       *CloseServiceSessionHandler
	PayCash            *PayCashHandler
	PayManualQR        *PayManualQRHandler
	SplitCheck         *SplitCheckHandler
	MergeChecks        *MergeChecksHandler
}

// NewSlices wires every Sales handler onto a shared Runner.
func NewSlices(db *sql.DB, queries *sqlc.Queries) *Slices {
	runner := NewRunner(db, queries)
	return &Slices{
		Runner:             runner,
		GetServiceSession:  NewGetServiceSessionHandler(runner),
		ListActiveSessions: NewListActiveSessionsHandler(runner),
		StartTakeaway:      NewStartTakeawaySessionHandler(runner),
		StartDineIn:        NewStartDineInSessionHandler(runner),
		SetSessionTables:   NewSetSessionTablesHandler(runner),
		AddDraftItem:       NewAddDraftItemHandler(runner),
		SetItemQuantity:    NewSetDraftItemQuantityHandler(runner),
		SetItemSize:        NewSetDraftItemSizeHandler(runner),
		SetItemNote:        NewSetDraftItemNoteHandler(runner),
		SetItemModifiers:   NewSetDraftItemModifiersHandler(runner),
		RemoveDraftItem:    NewRemoveDraftItemHandler(runner),
		CommitDraft:        NewCommitOrderDraftHandler(runner),
		StartNewDraft:      NewStartNewOrderDraftHandler(runner),
		SetCheckTarget:     NewSetCheckTargetHandler(runner),
		SubmitOrder:        NewSubmitOrderHandler(runner),
		CloseSession:       NewCloseServiceSessionHandler(runner),
		PayCash:            NewPayCashHandler(runner),
		PayManualQR:        NewPayManualQRHandler(runner),
		SplitCheck:         NewSplitCheckHandler(runner),
		MergeChecks:        NewMergeChecksHandler(runner),
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
	v1.GET("/sales/service-sessions", s.handleListActiveSessions,
		authn.RequireAuth(), authn.RequireCapability(CapSalesOperate))
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
	v1.PATCH("/sales/service-sessions/:id/draft/items/:item_id/quantity", s.handleSetDraftItemQuantity,
		authn.RequireAuth(), authn.RequireCapability(CapSalesOperate))
	v1.PATCH("/sales/service-sessions/:id/draft/items/:item_id/size", s.handleSetDraftItemSize,
		authn.RequireAuth(), authn.RequireCapability(CapSalesOperate))
	v1.PATCH("/sales/service-sessions/:id/draft/items/:item_id/preparation-note", s.handleSetDraftItemNote,
		authn.RequireAuth(), authn.RequireCapability(CapSalesOperate))
	v1.PATCH("/sales/service-sessions/:id/draft/items/:item_id/modifiers", s.handleSetDraftItemModifiers,
		authn.RequireAuth(), authn.RequireCapability(CapSalesOperate))
	v1.DELETE("/sales/service-sessions/:id/draft/items/:item_id", s.handleRemoveDraftItem,
		authn.RequireAuth(), authn.RequireCapability(CapSalesOperate))
	v1.POST("/sales/service-sessions/:id/draft/commit", s.handleCommitDraft,
		authn.RequireAuth(), authn.RequireCapability(CapSalesOperate))
	v1.POST("/sales/service-sessions/:id/draft", s.handleStartNewDraft,
		authn.RequireAuth(), authn.RequireCapability(CapSalesOperate))
	v1.PUT("/sales/service-sessions/:id/draft/check-target", s.handleSetCheckTarget,
		authn.RequireAuth(), authn.RequireCapability(CapSalesOperate))
	v1.POST("/sales/service-sessions/:id/submit", s.handleSubmitOrder,
		authn.RequireAuth(), authn.RequireCapability(CapSalesOperate))
	v1.POST("/sales/service-sessions/:id/close", s.handleCloseSession,
		authn.RequireAuth(), authn.RequireCapability(CapSalesOperate))
	// The flat merge route is registered before the /sales/checks/:check_id
	// routes so a literal segment is never shadowed by the parameter route.
	v1.POST("/sales/checks/merge", s.handleMergeChecks,
		authn.RequireAuth(), authn.RequireCapability(CapSalesOperate))
	v1.POST("/sales/checks/:check_id/payments/cash", s.handlePayCash,
		authn.RequireAuth(), authn.RequireCapability(CapSalesOperate))
	v1.POST("/sales/checks/:check_id/payments/manual-qr", s.handlePayManualQR,
		authn.RequireAuth(), authn.RequireCapability(CapSalesOperate))
	v1.POST("/sales/checks/:check_id/split", s.handleSplitCheck,
		authn.RequireAuth(), authn.RequireCapability(CapSalesOperate))
}
