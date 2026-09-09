package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
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
	queries sqlc.Querier
}

func NewGetSessionHandler(queries sqlc.Querier) *GetSessionHandler {
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
	if (sess.SessionState == SessionStateActive || sess.SessionState == "active") && time.Now().UTC().Sub(sess.LastHumanActivityAt) >= timeout {
		if err := h.queries.UpdateSessionState(ctx, sqlc.UpdateSessionStateParams{
			ID:    sess.SessionID,
			State: SessionStateLocked,
		}); err != nil {
			slog.Error("failed to update session state to locked", "session_id", sess.SessionID, "error", err)
		}
		sess.SessionState = SessionStateLocked
	}

	var wsPtr *string
	if workspace != "" {
		wsPtr = &workspace
	}

	caps := DeriveCapabilities(roles)
	apiState := sess.SessionState
	if apiState == "active" {
		apiState = SessionStateActive
	}
	return &SessionStateResponse{
		State:        apiState,
		StaffID:      sess.StaffIdentityID,
		DisplayName:  sess.DisplayName,
		LoginCode:    sess.LoginCode,
		Roles:        roles,
		Capabilities: caps,
		Workspace:    wsPtr,
	}, nil
}

// HandleHTTP godoc
//
//	@Summary		Lấy trạng thái phiên làm việc hiện tại
//	@Description	Kiểm tra và trả về trạng thái phiên làm việc hiện tại (authenticated, locked, signed_out) qua Bearer token hoặc cookie
//	@Tags			Auth
//	@Produce		json
//	@Success		200	{object}	response.APIResponse{data=SessionStateResponse}
//	@Failure		500	{object}	response.APIResponse
//	@Router			/auth/session [get]
func (h *GetSessionHandler) HandleHTTP(c echo.Context) error {
	token := extractToken(c)
	res, err := h.Handle(c.Request().Context(), token)
	if err != nil {
		return response.Error(c, err)
	}
	return response.OK(c, res)
}
