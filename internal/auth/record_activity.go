package auth

import (
	"fmt"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/labstack/echo/v4"
)

type RecordActivityHandler struct {
	queries sqlc.Querier
}

func NewRecordActivityHandler(queries sqlc.Querier) *RecordActivityHandler {
	return &RecordActivityHandler{queries: queries}
}

// HandleHTTP godoc
//
//	@Summary		Ghi nhận hoạt động người dùng
//	@Description	Cập nhật thời điểm hoạt động gần nhất để gia hạn tự động khóa do không hoạt động (inactivity timeout)
//	@Tags			Auth
//	@Produce		json
//	@Success		200	{object}	response.APIResponse{data=map[string]bool}
//	@Failure		401	{object}	response.APIResponse
//	@Failure		500	{object}	response.APIResponse
//	@Router			/auth/activity [post]
func (h *RecordActivityHandler) HandleHTTP(c echo.Context) error {
	staff := GetStaff(c)
	if staff == nil {
		return response.Error(c, fmt.Errorf("%w: unauthorized", response.ErrUnauthorized))
	}

	err := h.queries.UpdateSessionActivity(c.Request().Context(), sqlc.UpdateSessionActivityParams{
		ID:                  staff.SessionID,
		LastHumanActivityAt: time.Now().UTC(),
	})
	if err != nil {
		return response.Error(c, fmt.Errorf("record activity: %w", err))
	}

	return response.OK(c, map[string]bool{"recorded": true})
}
