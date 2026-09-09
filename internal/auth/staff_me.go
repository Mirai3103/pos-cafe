package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

type StaffMeHandler struct {
	queries sqlc.Querier
}

func NewStaffMeHandler(queries sqlc.Querier) *StaffMeHandler {
	return &StaffMeHandler{queries: queries}
}

// HandleHTTP godoc
//
//	@Summary		Lấy thông tin tài khoản hiện tại
//	@Description	Trả về thông tin hồ sơ và quyền hạn của nhân viên đang đăng nhập
//	@Tags			Staff
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	response.APIResponse{data=StaffProfileResponse}
//	@Failure		401	{object}	response.APIResponse
//	@Failure		403	{object}	response.APIResponse
//	@Router			/staff/me [get]
func (h *StaffMeHandler) HandleHTTP(c echo.Context) error {
	staff := GetStaff(c)
	if staff == nil {
		return response.Error(c, fmt.Errorf("%w: unauthorized", response.ErrForbidden))
	}

	roles := staff.Roles
	if roles == nil {
		roles = []string{}
	}
	caps := staff.Capabilities
	if caps == nil {
		caps = []string{}
	}

	enabled, err := h.getEnabled(c.Request().Context(), staff.StaffID)
	if err != nil {
		return response.Error(c, fmt.Errorf("lookup enabled: %w", err))
	}

	return response.OK(c, StaffProfileResponse{
		ID:           staff.StaffID,
		DisplayName:  staff.DisplayName,
		LoginCode:    staff.LoginCode,
		Enabled:      enabled,
		Roles:        roles,
		Capabilities: caps,
	})
}

func (h *StaffMeHandler) getEnabled(ctx context.Context, staffID uuid.UUID) (bool, error) {
	row, err := h.queries.GetStaffByID(ctx, staffID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return row.Enabled, nil
}
