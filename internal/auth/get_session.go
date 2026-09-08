package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/labstack/echo/v4"
)

func extractToken(c echo.Context) string {
	authHeader := c.Request().Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		return strings.TrimPrefix(authHeader, "Bearer ")
	}
	if cookie, err := c.Cookie(SessionCookieName); err == nil && cookie.Value != "" {
		return cookie.Value
	}
	return ""
}

type GetSessionHandler struct {
	queries *sqlc.Queries
}

func NewGetSessionHandler(queries *sqlc.Queries) *GetSessionHandler {
	return &GetSessionHandler{queries: queries}
}

func (h *GetSessionHandler) Handle(ctx context.Context, token string) (*SessionStateResponse, error) {
	if token == "" {
		return &SessionStateResponse{State: "signed_out"}, nil
	}

	tokenHash := HashToken(token)
	sess, err := h.queries.GetSessionByTokenHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &SessionStateResponse{State: "signed_out"}, nil
		}
		return nil, fmt.Errorf("lookup session: %w", err)
	}

	if sess.RevokedAt.Valid || time.Now().UTC().After(sess.ExpiresAt) || !sess.IdentityEnabled {
		return &SessionStateResponse{State: "signed_out"}, nil
	}

	roles, err := h.queries.GetStaffRoles(ctx, sess.StaffIdentityID)
	if err != nil {
		return nil, fmt.Errorf("lookup roles: %w", err)
	}

	workspace := ""
	if sess.ActiveWorkspace.Valid {
		workspace = sess.ActiveWorkspace.String
	}

	// Inactivity Check
	timeout := GetInactivityTimeout(workspace)
	if sess.SessionState == SessionStateActive && time.Now().UTC().Sub(sess.LastHumanActivityAt) >= timeout {
		_ = h.queries.UpdateSessionState(ctx, sqlc.UpdateSessionStateParams{
			ID:    sess.SessionID,
			State: SessionStateLocked,
		})
		sess.SessionState = SessionStateLocked
	}

	var wsPtr *string
	if workspace != "" {
		wsPtr = &workspace
	}

	caps := DeriveCapabilities(roles)
	return &SessionStateResponse{
		State:        sess.SessionState,
		StaffID:      sess.StaffIdentityID,
		DisplayName:  sess.DisplayName,
		LoginCode:    sess.LoginCode,
		Roles:        roles,
		Capabilities: caps,
		Workspace:    wsPtr,
	}, nil
}

func (h *GetSessionHandler) HandleHTTP(c echo.Context) error {
	token := extractToken(c)
	res, err := h.Handle(c.Request().Context(), token)
	if err != nil {
		return response.Error(c, err)
	}
	return response.OK(c, res)
}
