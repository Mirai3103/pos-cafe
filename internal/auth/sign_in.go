package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/labstack/echo/v4"
)

const SessionCookieName = "staff_session_token"

func setSessionCookie(c echo.Context, token string, maxAge int) {
	//nolint:gosec // G124: Secure flag is dynamically configured based on request scheme (HTTP vs HTTPS in POS local setup)
	c.SetCookie(&http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   c.Scheme() == "https",
	})
}

func clearSessionCookie(c echo.Context) {
	setSessionCookie(c, "", -1)
}

type SignInHandler struct {
	queries sqlc.Querier
}

func NewSignInHandler(queries sqlc.Querier) *SignInHandler {
	return &SignInHandler{queries: queries}
}

func (h *SignInHandler) Handle(ctx context.Context, req SignInRequest) (*SignInResponse, error) {
	staff, err := h.queries.GetStaffByLoginCode(ctx, req.LoginCode)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			VerifyPin("", req.Pin) // Constant time check
			return nil, fmt.Errorf("%w: thông tin đăng nhập không chính xác", response.ErrUnauthorized)
		}
		return nil, fmt.Errorf("lookup staff: %w", err)
	}

	if !VerifyPin(staff.PinHash, req.Pin) {
		return nil, fmt.Errorf("%w: thông tin đăng nhập không chính xác", response.ErrUnauthorized)
	}

	if !staff.Enabled {
		return nil, fmt.Errorf("%w: tài khoản nhân viên đã bị vô hiệu hóa", response.ErrForbidden)
	}

	roles, err := h.queries.GetStaffRoles(ctx, staff.ID)
	if err != nil {
		return nil, fmt.Errorf("lookup staff roles: %w", err)
	}

	token, tokenHash, err := GenerateSessionToken()
	if err != nil {
		return nil, fmt.Errorf("generate session token: %w", err)
	}

	now := time.Now().UTC()
	expiresAt := now.Add(SessionDuration)

	_, err = h.queries.CreateStaffSession(ctx, sqlc.CreateStaffSessionParams{
		TokenHash:           tokenHash,
		StaffIdentityID:     staff.ID,
		State:               SessionStateActive,
		ActiveWorkspace:     sql.NullString{},
		LastAuthenticatedAt: now,
		LastHumanActivityAt: now,
		ExpiresAt:           expiresAt,
	})
	if err != nil {
		return nil, fmt.Errorf("create staff session: %w", err)
	}

	caps := DeriveCapabilities(roles)

	return &SignInResponse{
		Token: token,
		Staff: StaffProfileResponse{
			ID:           staff.ID,
			DisplayName:  staff.DisplayName,
			LoginCode:    staff.LoginCode,
			Enabled:      staff.Enabled,
			Roles:        roles,
			Capabilities: caps,
		},
	}, nil
}

// HandleHTTP godoc
// @Summary Đăng nhập bằng mã PIN
// @Description Xác thực nhân viên bằng login_code và pin 4-8 số
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body SignInRequest true "Sign in credentials"
// @Success 200 {object} response.APIResponse{data=SignInResponse}
// @Failure 400 {object} response.APIResponse
// @Failure 401 {object} response.APIResponse
// @Router /auth/sign-in [post]
func (h *SignInHandler) HandleHTTP(c echo.Context) error {
	var req SignInRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, fmt.Errorf("%w: invalid payload", response.ErrInvalid))
	}
	if err := c.Validate(&req); err != nil {
		return response.Error(c, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error()))
	}

	res, err := h.Handle(c.Request().Context(), req)
	if err != nil {
		return response.Error(c, err)
	}

	setSessionCookie(c, res.Token, int(SessionDuration.Seconds()))
	return response.OK(c, res)
}
