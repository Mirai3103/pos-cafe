package auth

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

const StaffContextKey = "auth_staff_claims"

type StaffClaims struct {
	StaffID      uuid.UUID
	SessionID    uuid.UUID
	DisplayName  string
	LoginCode    string
	Roles        []string
	Capabilities []string
	Workspace    *string
}

func GetStaff(c echo.Context) *StaffClaims {
	if val := c.Get(StaffContextKey); val != nil {
		if claims, ok := val.(*StaffClaims); ok {
			return claims
		}
	}
	return nil
}

type Middleware struct {
	queries sqlc.Querier
}

func NewMiddleware(queries sqlc.Querier) *Middleware {
	return &Middleware{queries: queries}
}

func (m *Middleware) RateLimit(limiter *RateLimiter, keyFunc func(echo.Context) string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if !limiter.Allow(keyFunc(c)) {
				return response.Error(c, fmt.Errorf("%w: quá nhiều lần thử, vui lòng thử lại sau", response.ErrTooManyRequests))
			}
			return next(c)
		}
	}
}

func (m *Middleware) RequireAuth(allowedRoles ...string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			token := extractToken(c)
			if token == "" {
				return response.Error(c, fmt.Errorf("%w: vui lòng đăng nhập", response.ErrUnauthorized))
			}

			tokenHash := HashToken(token)
			sess, err := m.queries.GetSessionByTokenHash(c.Request().Context(), tokenHash)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return response.Error(c, fmt.Errorf("%w: phiên đăng nhập không hợp lệ", response.ErrUnauthorized))
				}
				return response.Error(c, fmt.Errorf("lookup session: %w", err))
			}

			if sess.RevokedAt.Valid || time.Now().UTC().After(sess.ExpiresAt) || !sess.IdentityEnabled {
				return response.Error(c, fmt.Errorf("%w: phiên đăng nhập đã hết hạn hoặc bị thu hồi", response.ErrUnauthorized))
			}

			workspace := ""
			if sess.ActiveWorkspace.Valid {
				workspace = sess.ActiveWorkspace.String
			}

			// Inactivity lock check
			timeout := GetInactivityTimeout(workspace)
			if (sess.SessionState == SessionStateActive || sess.SessionState == "active") && time.Now().UTC().Sub(sess.LastHumanActivityAt) >= timeout {
				if err := m.queries.UpdateSessionState(c.Request().Context(), sqlc.UpdateSessionStateParams{
					ID:    sess.SessionID,
					State: SessionStateLocked,
				}); err != nil {
					slog.Error("failed to update session state to locked", "session_id", sess.SessionID, "error", err)
				}
				sess.SessionState = SessionStateLocked
			}

			if sess.SessionState != SessionStateActive && sess.SessionState != "active" {
				return response.Error(c, fmt.Errorf("%w: phiên đang bị khóa, vui lòng mở khóa", response.ErrForbidden))
			}

			roles, err := m.queries.GetStaffRoles(c.Request().Context(), sess.StaffIdentityID)
			if err != nil {
				return response.Error(c, fmt.Errorf("lookup roles: %w", err))
			}
			if roles == nil {
				roles = []string{}
			}

			// Role check
			if len(allowedRoles) > 0 {
				hasRole := false
				for _, r := range allowedRoles {
					if slices.Contains(roles, r) {
						hasRole = true
						break
					}
				}
				if !hasRole {
					return response.Error(c, fmt.Errorf("%w: bạn không có quyền thực hiện thao tác này", response.ErrForbidden))
				}
			}

			var wsPtr *string
			if workspace != "" {
				wsPtr = &workspace
			}

			caps := DeriveCapabilities(roles)
			if caps == nil {
				caps = []string{}
			}

			claims := &StaffClaims{
				StaffID:      sess.StaffIdentityID,
				SessionID:    sess.SessionID,
				DisplayName:  sess.DisplayName,
				LoginCode:    sess.LoginCode,
				Roles:        roles,
				Capabilities: caps,
				Workspace:    wsPtr,
			}
			c.Set(StaffContextKey, claims)

			return next(c)
		}
	}
}

func (m *Middleware) RequireCapability(capability string) echo.MiddlewareFunc {
	return RequireCapability(capability)
}

func RequireCapability(capability string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			staff := GetStaff(c)
			if staff == nil {
				return response.Error(c, fmt.Errorf("%w: vui lòng đăng nhập", response.ErrUnauthorized))
			}
			if !slices.Contains(staff.Capabilities, capability) {
				return response.Error(c, fmt.Errorf("%w: thiếu quyền %s", response.ErrForbidden, capability))
			}
			return next(c)
		}
	}
}
