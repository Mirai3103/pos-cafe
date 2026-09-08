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

func (h *SignOutHandler) HandleHTTP(c echo.Context) error {
	staff := GetStaff(c)
	if staff != nil {
		_ = h.queries.RevokeSession(c.Request().Context(), staff.SessionID)
	}
	clearSessionCookie(c)
	return response.OK(c, map[string]string{"state": "signed_out"})
}
