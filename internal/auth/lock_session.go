package auth

import (
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/labstack/echo/v4"
)

type LockSessionHandler struct {
	queries *sqlc.Queries
}

func NewLockSessionHandler(queries *sqlc.Queries) *LockSessionHandler {
	return &LockSessionHandler{queries: queries}
}

func (h *LockSessionHandler) HandleHTTP(c echo.Context) error {
	staff := GetStaff(c)
	if staff == nil {
		return response.Error(c, fmt.Errorf("%w: unauthorized", response.ErrForbidden))
	}

	err := h.queries.UpdateSessionState(c.Request().Context(), sqlc.UpdateSessionStateParams{
		ID:    staff.SessionID,
		State: SessionStateLocked,
	})
	if err != nil {
		return response.Error(c, fmt.Errorf("lock session: %w", err))
	}

	return response.OK(c, map[string]string{"state": SessionStateLocked})
}
