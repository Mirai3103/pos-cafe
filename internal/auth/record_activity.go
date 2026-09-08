package auth

import (
	"fmt"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/labstack/echo/v4"
)

type RecordActivityHandler struct {
	queries *sqlc.Queries
}

func NewRecordActivityHandler(queries *sqlc.Queries) *RecordActivityHandler {
	return &RecordActivityHandler{queries: queries}
}

func (h *RecordActivityHandler) HandleHTTP(c echo.Context) error {
	staff := GetStaff(c)
	if staff == nil {
		return response.Error(c, fmt.Errorf("%w: unauthorized", response.ErrForbidden))
	}

	err := h.queries.UpdateSessionActivity(c.Request().Context(), sqlc.UpdateSessionActivityParams{
		ID:                   staff.SessionID,
		LastHumanActivityAt: time.Now().UTC(),
	})
	if err != nil {
		return response.Error(c, fmt.Errorf("record activity: %w", err))
	}

	return response.OK(c, map[string]bool{"recorded": true})
}
