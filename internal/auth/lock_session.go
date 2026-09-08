package auth

import (
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/labstack/echo/v4"
)

type LockSessionHandler struct {
	queries sqlc.Querier
}

func NewLockSessionHandler(queries sqlc.Querier) *LockSessionHandler {
	return &LockSessionHandler{queries: queries}
}

// HandleHTTP godoc
//
//	@Summary		Khóa phiên làm việc
//	@Description	Chuyển trạng thái phiên làm việc hiện tại sang locked
//	@Tags			Auth
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	response.APIResponse{data=map[string]string}
//	@Failure		401	{object}	response.APIResponse
//	@Failure		500	{object}	response.APIResponse
//	@Router			/auth/lock [post]
func (h *LockSessionHandler) HandleHTTP(c echo.Context) error {
	staff := GetStaff(c)
	if staff == nil {
		return response.Error(c, fmt.Errorf("%w: unauthorized", response.ErrUnauthorized))
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
