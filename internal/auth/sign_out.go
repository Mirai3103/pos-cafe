package auth

import (
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/labstack/echo/v4"
)

type SignOutHandler struct {
	queries sqlc.Querier
}

func NewSignOutHandler(queries sqlc.Querier) *SignOutHandler {
	return &SignOutHandler{queries: queries}
}

// HandleHTTP godoc
//
//	@Summary		Đăng xuất phiên làm việc
//	@Description	Thu hồi phiên làm việc hiện tại và xóa cookie phiên
//	@Tags			Auth
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	response.APIResponse{data=map[string]string}
//	@Failure		401	{object}	response.APIResponse
//	@Failure		500	{object}	response.APIResponse
//	@Router			/auth/sign-out [post]
func (h *SignOutHandler) HandleHTTP(c echo.Context) error {
	staff := GetStaff(c)
	if staff != nil {
		_ = h.queries.RevokeSession(c.Request().Context(), staff.SessionID)
	}
	clearSessionCookie(c)
	return response.OK(c, map[string]string{"state": "signed_out"})
}
