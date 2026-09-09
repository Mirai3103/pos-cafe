package auth

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/labstack/echo/v4"
)

type Slices struct {
	Middleware       *Middleware
	SignInLimiter    *RateLimiter
	UnlockLimiter    *RateLimiter
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
	signInLimiter := NewRateLimiter(5, 15*time.Minute)
	unlockLimiter := NewRateLimiter(3, 5*time.Minute)

	return &Slices{
		Middleware:       NewMiddleware(queries),
		SignInLimiter:    signInLimiter,
		UnlockLimiter:    unlockLimiter,
		Bootstrap:        NewBootstrapManagerHandler(db, queries),
		SignIn:           NewSignInHandler(queries, signInLimiter),
		Unlock:           NewUnlockSessionHandler(db, queries, unlockLimiter),
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
	authGroup.POST("/sign-in", s.SignIn.HandleHTTP, s.Middleware.RateLimit(s.SignInLimiter, signInRequestRateLimitKey))
	authGroup.GET("/session", s.GetSession.HandleHTTP)
	authGroup.POST("/unlock", s.Unlock.HandleHTTP, s.Middleware.RateLimit(s.UnlockLimiter, func(c echo.Context) string {
		return unlockRateLimitKey(extractToken(c))
	}))
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

func signInRequestRateLimitKey(c echo.Context) string {
	request := c.Request()
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return signInRateLimitKey(c.RealIP())
	}
	_ = request.Body.Close()
	request.Body = io.NopCloser(bytes.NewReader(body))

	var req SignInRequest
	if err := json.Unmarshal(body, &req); err != nil || strings.TrimSpace(req.LoginCode) == "" {
		return signInRateLimitKey(c.RealIP())
	}
	return signInRateLimitKey(req.LoginCode)
}

func signInRateLimitKey(loginCode string) string {
	return "sign-in:" + strings.ToUpper(strings.TrimSpace(loginCode))
}

func unlockRateLimitKey(token string) string {
	return "unlock:" + HashToken(token)
}
