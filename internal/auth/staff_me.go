package auth

import (
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/labstack/echo/v4"
)

type StaffMeHandler struct{}

func NewStaffMeHandler() *StaffMeHandler {
	return &StaffMeHandler{}
}

// HandleHTTP godoc
// @Summary Lấy thông tin tài khoản hiện tại
// @Description Trả về thông tin hồ sơ và quyền hạn của nhân viên đang đăng nhập
// @Tags Staff
// @Produce json
// @Success 200 {object} response.APIResponse{data=StaffProfileResponse}
// @Failure 403 {object} response.APIResponse
// @Router /staff/me [get]
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

	return response.OK(c, StaffProfileResponse{
		ID:           staff.StaffID,
		DisplayName:  staff.DisplayName,
		LoginCode:    staff.LoginCode,
		Enabled:      true,
		Roles:        roles,
		Capabilities: caps,
	})
}
