package catalog

import (
	"database/sql"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/labstack/echo/v4"
)

type Slices struct {
	Runner *Runner
	db     *sql.DB
	q      *sqlc.Queries

	CreateCategory                    *CreateCategoryHandler
	RenameCategory                    *RenameCategoryHandler
	CreateItem                        *CreateItemHandler
	RenameItem                        *RenameItemHandler
	RepriceItem                       *RepriceItemHandler
	SetItemAvailability               *SetItemAvailabilityHandler
	RetireItem                        *RetireItemHandler
	RenameSize                        *RenameSizeHandler
	RepriceSize                       *RepriceSizeHandler
	SetSizeAvailability               *SetSizeAvailabilityHandler
	RetireSize                        *RetireSizeHandler
	CreateModifierGroup               *CreateModifierGroupHandler
	RenameModifierGroup               *RenameModifierGroupHandler
	SetModifierGroupDefaults          *SetModifierGroupDefaultsHandler
	RetireModifierGroup               *RetireModifierGroupHandler
	RenameModifierOption              *RenameModifierOptionHandler
	RepriceModifierOption             *RepriceModifierOptionHandler
	SetModifierOptionAvailability     *SetModifierOptionAvailabilityHandler
	RetireModifierOption              *RetireModifierOptionHandler
	AttachItemModifierGroup           *AttachItemModifierGroupHandler
	AttachCategoryModifierGroup       *AttachCategoryModifierGroupHandler
	ExcludeItemInheritedModifierGroup *ExcludeInheritedModifierGroupHandler
	SellableMenu                      *SellableMenuHandler
	ManagementMenu                    *ManagementMenuHandler
	AvailabilityMenu                  *AvailabilityMenuHandler
	ModifierGroups                    *ModifierGroupsHandler
	AuditEvents                       *AuditEventsHandler
}

func NewSlices(db *sql.DB, queries *sqlc.Queries) *Slices {
	runner := NewRunner(db, queries)
	return &Slices{
		Runner:                            runner,
		db:                                db,
		q:                                 queries,
		CreateCategory:                    NewCreateCategoryHandler(runner),
		RenameCategory:                    NewRenameCategoryHandler(runner),
		CreateItem:                        NewCreateItemHandler(runner),
		RenameItem:                        NewRenameItemHandler(runner),
		RepriceItem:                       NewRepriceItemHandler(runner),
		SetItemAvailability:               NewSetItemAvailabilityHandler(runner),
		RetireItem:                        NewRetireItemHandler(runner),
		RenameSize:                        NewRenameSizeHandler(runner),
		RepriceSize:                       NewRepriceSizeHandler(runner),
		SetSizeAvailability:               NewSetSizeAvailabilityHandler(runner),
		RetireSize:                        NewRetireSizeHandler(runner),
		CreateModifierGroup:               NewCreateModifierGroupHandler(runner),
		RenameModifierGroup:               NewRenameModifierGroupHandler(runner),
		SetModifierGroupDefaults:          NewSetModifierGroupDefaultsHandler(runner),
		RetireModifierGroup:               NewRetireModifierGroupHandler(runner),
		RenameModifierOption:              NewRenameModifierOptionHandler(runner),
		RepriceModifierOption:             NewRepriceModifierOptionHandler(runner),
		SetModifierOptionAvailability:     NewSetModifierOptionAvailabilityHandler(runner),
		RetireModifierOption:              NewRetireModifierOptionHandler(runner),
		AttachItemModifierGroup:           NewAttachItemModifierGroupHandler(runner),
		AttachCategoryModifierGroup:       NewAttachCategoryModifierGroupHandler(runner),
		ExcludeItemInheritedModifierGroup: NewExcludeInheritedModifierGroupHandler(runner),
		SellableMenu:                      NewSellableMenuHandler(runner),
		ManagementMenu:                    NewManagementMenuHandler(runner),
		AvailabilityMenu:                  NewAvailabilityMenuHandler(runner),
		ModifierGroups:                    NewModifierGroupsHandler(runner),
		AuditEvents:                       NewAuditEventsHandler(runner),
	}
}

// RegisterRoutes mounts all catalog routes under /catalog on the provided group.
func (s *Slices) RegisterRoutes(v1 *echo.Group, authn *auth.Middleware) {
	catalog := v1.Group("/catalog", authn.RequireAuth())

	// Reads
	catalog.GET("/menu/sellable", s.handleGetSellableMenu, authn.RequireCapability(CapViewPrices))
	catalog.GET("/menu/manage", s.handleGetManagementMenu, authn.RequireCapability(CapViewPrices))
	catalog.GET("/menu/availability", s.handleGetAvailabilityMenu, authn.RequireCapability(CapManageAvailability))
	catalog.GET("/modifier-groups", s.handleGetModifierGroups, authn.RequireCapability(CapViewPrices))
	catalog.GET("/audit-events", s.handleGetAuditEvents, authn.RequireCapability(CapAuditInspect))

	// Categories
	catalog.POST("/categories", s.handleCreateCategory, authn.RequireCapability(CapAdministerStructure))
	catalog.PATCH("/categories/:category_id/name", s.handleRenameCategory, authn.RequireCapability(CapAdministerStructure))

	// Items
	catalog.POST("/items", s.handleCreateItem, authn.RequireCapability(CapAdministerStructure), authn.RequireCapability(CapChangePrice))
	catalog.PATCH("/items/:item_id/name", s.handleRenameItem, authn.RequireCapability(CapAdministerStructure))
	catalog.PATCH("/items/:item_id/price", s.handleRepriceItem, authn.RequireCapability(CapAdministerStructure), authn.RequireCapability(CapChangePrice))
	catalog.PATCH("/items/:item_id/availability", s.handleSetItemAvailability, authn.RequireCapability(CapManageAvailability))
	catalog.POST("/items/:item_id/retirement", s.handleRetireItem, authn.RequireCapability(CapAdministerStructure))

	// Sizes
	catalog.PATCH("/sizes/:size_id/name", s.handleRenameSize, authn.RequireCapability(CapAdministerStructure))
	catalog.PATCH("/sizes/:size_id/price", s.handleRepriceSize, authn.RequireCapability(CapAdministerStructure), authn.RequireCapability(CapChangePrice))
	catalog.PATCH("/sizes/:size_id/availability", s.handleSetSizeAvailability, authn.RequireCapability(CapManageAvailability))
	catalog.POST("/sizes/:size_id/retirement", s.handleRetireSize, authn.RequireCapability(CapAdministerStructure))

	// Modifier Groups
	catalog.POST("/modifier-groups", s.handleCreateModifierGroup, authn.RequireCapability(CapAdministerStructure), authn.RequireCapability(CapChangePrice))
	catalog.PATCH("/modifier-groups/:group_id/name", s.handleRenameModifierGroup, authn.RequireCapability(CapAdministerStructure))
	catalog.PUT("/modifier-groups/:group_id/defaults", s.handleSetModifierGroupDefaults, authn.RequireCapability(CapAdministerStructure))
	catalog.POST("/modifier-groups/:group_id/retirement", s.handleRetireModifierGroup, authn.RequireCapability(CapAdministerStructure))

	// Modifier Options
	catalog.PATCH("/modifier-options/:option_id/name", s.handleRenameModifierOption, authn.RequireCapability(CapAdministerStructure))
	catalog.PATCH("/modifier-options/:option_id/price", s.handleRepriceModifierOption, authn.RequireCapability(CapAdministerStructure), authn.RequireCapability(CapChangePrice))
	catalog.PATCH("/modifier-options/:option_id/availability", s.handleSetModifierOptionAvailability, authn.RequireCapability(CapManageAvailability))
	catalog.POST("/modifier-options/:option_id/retirement", s.handleRetireModifierOption, authn.RequireCapability(CapAdministerStructure))

	// Assignments
	catalog.POST("/items/:item_id/modifier-groups/:group_id", s.handleAttachItemModifierGroup, authn.RequireCapability(CapAdministerStructure))
	catalog.POST("/categories/:category_id/modifier-groups/:group_id", s.handleAttachCategoryModifierGroup, authn.RequireCapability(CapAdministerStructure))
	catalog.POST("/items/:item_id/inherited-modifier-group-exclusions/:group_id", s.handleExcludeInheritedModifierGroup, authn.RequireCapability(CapAdministerStructure))
}
