package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/labstack/echo/v4"
)

type UnlockSessionHandler struct {
	queries *sqlc.Queries
}

func NewUnlockSessionHandler(queries *sqlc.Queries) *UnlockSessionHandler {
	return &UnlockSessionHandler{queries: queries}
}

func (h *UnlockSessionHandler) Handle(ctx context.Context, token string, pin string) (*SignInResponse, error) {
	if token == "" {
		return nil, fmt.Errorf("%w: session không tồn tại", response.ErrForbidden)
	}

	tokenHash := HashToken(token)
	sess, err := h.queries.GetSessionByTokenHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: session không hợp lệ", response.ErrForbidden)
		}
		return nil, fmt.Errorf("lookup session: %w", err)
	}

	if sess.RevokedAt.Valid || !sess.IdentityEnabled {
		return nil, fmt.Errorf("%w: session đã hết hạn", response.ErrForbidden)
	}

	if !VerifyPin(sess.PinHash, pin) {
		return nil, fmt.Errorf("%w: mã PIN không đúng", response.ErrInvalid)
	}

	if err := h.queries.UpdateSessionState(ctx, sqlc.UpdateSessionStateParams{
		ID:    sess.SessionID,
		State: SessionStateActive,
	}); err != nil {
		return nil, fmt.Errorf("unlock session: %w", err)
	}

	roles, err := h.queries.GetStaffRoles(ctx, sess.StaffIdentityID)
	if err != nil {
		return nil, fmt.Errorf("lookup roles: %w", err)
	}

	return &SignInResponse{
		Token: token,
		Staff: StaffProfileResponse{
			ID:           sess.StaffIdentityID,
			DisplayName:  sess.DisplayName,
			LoginCode:    sess.LoginCode,
			Enabled:      sess.IdentityEnabled,
			Roles:        roles,
			Capabilities: DeriveCapabilities(roles),
		},
	}, nil
}

func (h *UnlockSessionHandler) HandleHTTP(c echo.Context) error {
	token := extractToken(c)
	var req UnlockRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, fmt.Errorf("%w: invalid payload", response.ErrInvalid))
	}
	if err := c.Validate(&req); err != nil {
		return response.Error(c, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error()))
	}

	res, err := h.Handle(c.Request().Context(), token, req.Pin)
	if err != nil {
		return response.Error(c, err)
	}

	return response.OK(c, res)
}
