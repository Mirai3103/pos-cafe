package auth

import (
	"database/sql"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/labstack/echo/v4"
)

type Slices struct {
	Middleware       *Middleware
	Bootstrap        *BootstrapManagerHandler
	SignIn           *SignInHandler
	Unlock           *UnlockSessionHandler
	GetSession       *GetSessionHandler
	Lock             *LockSessionHandler
	SignOut          *SignOutHandler
	DeclareWorkspace *DeclareWorkspaceHandler
	RecordActivity   *RecordActivityHandler
	ListIdentities   *ListIdentitiesHandler
	StaffMe          *StaffMeHandler
	StaffList        *StaffListHandler
	StaffCreate      *StaffCreateHandler
	StaffSetEnabled  *StaffSetEnabledHandler
	StaffReplaceRole *StaffReplaceRolesHandler
	StaffResetPin    *StaffResetPinHandler
}

func NewSlices(db *sql.DB, queries *sqlc.Queries) *Slices {
	return &Slices{
		Middleware:       NewMiddleware(queries),
		Bootstrap:        NewBootstrapManagerHandler(db, queries),
		SignIn:           NewSignInHandler(queries),
		Unlock:           NewUnlockSessionHandler(queries),
		GetSession:       NewGetSessionHandler(queries),
		Lock:             NewLockSessionHandler(queries),
		SignOut:          NewSignOutHandler(queries),
		DeclareWorkspace: NewDeclareWorkspaceHandler(queries),
		RecordActivity:   NewRecordActivityHandler(queries),
		ListIdentities:   NewListIdentitiesHandler(queries),
		StaffMe:          NewStaffMeHandler(),
		StaffList:        NewStaffListHandler(queries),
		StaffCreate:      NewStaffCreateHandler(db, queries),
		StaffSetEnabled:  NewStaffSetEnabledHandler(db, queries),
		StaffReplaceRole: NewStaffReplaceRolesHandler(db, queries),
		StaffResetPin:    NewStaffResetPinHandler(db, queries),
	}
}

func (s *Slices) RegisterRoutes(v1 *echo.Group) {
	// Auth routes
	authGroup := v1.Group("/auth")
	authGroup.POST("/bootstrap", s.Bootstrap.HandleHTTP)
	authGroup.GET("/identities", s.ListIdentities.HandleHTTP)
	authGroup.POST("/sign-in", s.SignIn.HandleHTTP)
	authGroup.GET("/session", s.GetSession.HandleHTTP)
	authGroup.POST("/unlock", s.Unlock.HandleHTTP)
	authGroup.POST("/lock", s.Lock.HandleHTTP, s.Middleware.RequireAuth())
	authGroup.POST("/sign-out", s.SignOut.HandleHTTP, s.Middleware.RequireAuth())
	authGroup.POST("/workspace", s.DeclareWorkspace.HandleHTTP, s.Middleware.RequireAuth())
	authGroup.POST("/activity", s.RecordActivity.HandleHTTP, s.Middleware.RequireAuth())

	// Staff administration routes (Protected by MANAGER role)
	staffGroup := v1.Group("/staff", s.Middleware.RequireAuth(RoleManager))
	staffGroup.GET("/me", s.StaffMe.HandleHTTP)
	staffGroup.GET("", s.StaffList.HandleHTTP)
	staffGroup.POST("", s.StaffCreate.HandleHTTP)
	staffGroup.PATCH("/:id/enabled", s.StaffSetEnabled.HandleHTTP)
	staffGroup.PUT("/:id/roles", s.StaffReplaceRole.HandleHTTP)
	staffGroup.POST("/:id/reset-pin", s.StaffResetPin.HandleHTTP)
}
