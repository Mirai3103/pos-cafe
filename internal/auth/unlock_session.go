package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/labstack/echo/v4"
)

type UnlockSessionHandler struct {
	db      *sql.DB
	queries *sqlc.Queries
}

func NewUnlockSessionHandler(db *sql.DB, queries *sqlc.Queries) *UnlockSessionHandler {
	return &UnlockSessionHandler{db: db, queries: queries}
}

func (h *UnlockSessionHandler) Handle(ctx context.Context, token string, pin string) (*SignInResponse, error) {
	if token == "" {
		return nil, fmt.Errorf("%w: session không tồn tại", response.ErrUnauthorized)
	}

	tokenHash := HashToken(token)
	sess, err := h.queries.GetSessionByTokenHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: session không hợp lệ", response.ErrUnauthorized)
		}
		return nil, fmt.Errorf("lookup session: %w", err)
	}

	if sess.RevokedAt.Valid || !sess.IdentityEnabled || time.Now().UTC().After(sess.ExpiresAt) {
		return nil, fmt.Errorf("%w: session đã hết hạn", response.ErrUnauthorized)
	}

	if !VerifyPin(sess.PinHash, pin) {
		return nil, fmt.Errorf("%w: mã PIN không đúng", response.ErrUnauthorized)
	}

	now := time.Now().UTC()
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin unlock transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	qtx := h.queries.WithTx(tx)
	if err := qtx.UpdateSessionState(ctx, sqlc.UpdateSessionStateParams{
		ID:    sess.SessionID,
		State: SessionStateActive,
	}); err != nil {
		return nil, fmt.Errorf("unlock session: %w", err)
	}

	if err := qtx.UpdateSessionActivity(ctx, sqlc.UpdateSessionActivityParams{
		ID:                  sess.SessionID,
		LastHumanActivityAt: now,
	}); err != nil {
		return nil, fmt.Errorf("update session activity: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit unlock transaction: %w", err)
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

// HandleHTTP godoc
//
//	@Summary		Mở khóa phiên làm việc
//	@Description	Mở khóa phiên làm việc đang bị khóa (locked) bằng mã PIN của nhân viên
//	@Tags			Auth
//	@Accept			json
//	@Produce		json
//	@Param			request	body		UnlockRequest	true	"Mã PIN mở khóa"
//	@Success		200		{object}	response.APIResponse{data=SignInResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		500		{object}	response.APIResponse
//	@Router			/auth/unlock [post]
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
