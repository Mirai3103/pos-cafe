package auth

import (
	"context"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

type StaffListHandler struct {
	queries *sqlc.Queries
}

func NewStaffListHandler(queries *sqlc.Queries) *StaffListHandler {
	return &StaffListHandler{queries: queries}
}

func (h *StaffListHandler) Handle(ctx context.Context) ([]StaffDetailResponse, error) {
	staffRows, err := h.queries.ListAllStaff(ctx)
	if err != nil {
		return nil, fmt.Errorf("list all staff: %w", err)
	}

	roleRows, err := h.queries.ListAllStaffRoles(ctx)
	if err != nil {
		return nil, fmt.Errorf("list all staff roles: %w", err)
	}

	rolesMap := make(map[uuid.UUID][]string)
	for _, r := range roleRows {
		rolesMap[r.StaffIdentityID] = append(rolesMap[r.StaffIdentityID], r.Role)
	}

	res := make([]StaffDetailResponse, len(staffRows))
	for i, s := range staffRows {
		roles := rolesMap[s.ID]
		if roles == nil {
			roles = []string{}
		}
		res[i] = StaffDetailResponse{
			ID:          s.ID,
			DisplayName: s.DisplayName,
			LoginCode:   s.LoginCode,
			Enabled:     s.Enabled,
			Roles:       roles,
			CreatedAt:   s.CreatedAt,
		}
	}

	return res, nil
}

// HandleHTTP godoc
//
//	@Summary		Danh sách tất cả nhân viên
//	@Description	Trả về danh sách tất cả nhân viên kèm vai trò
//	@Tags			Staff
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	response.APIResponse{data=[]StaffDetailResponse}
//	@Failure		401	{object}	response.APIResponse
//	@Failure		403	{object}	response.APIResponse
//	@Failure		500	{object}	response.APIResponse
//	@Router			/staff [get]
func (h *StaffListHandler) HandleHTTP(c echo.Context) error {
	res, err := h.Handle(c.Request().Context())
	if err != nil {
		return response.Error(c, err)
	}
	return response.OK(c, res)
}
