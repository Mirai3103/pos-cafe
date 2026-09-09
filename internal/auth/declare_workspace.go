package auth

import (
	"database/sql"
	"fmt"
	"slices"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/labstack/echo/v4"
)

type DeclareWorkspaceHandler struct {
	queries sqlc.Querier
}

func NewDeclareWorkspaceHandler(queries sqlc.Querier) *DeclareWorkspaceHandler {
	return &DeclareWorkspaceHandler{queries: queries}
}

// HandleHTTP godoc
//
//	@Summary		Chọn khu vực làm việc (Workspace)
//	@Description	Khai báo khu vực làm việc (cashier, manager, preparation) cho phiên làm việc hiện tại
//	@Tags			Auth
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		DeclareWorkspaceRequest	true	"Khu vực làm việc"
//	@Success		200		{object}	response.APIResponse{data=map[string]string}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		500		{object}	response.APIResponse
//	@Router			/auth/workspace [post]
func (h *DeclareWorkspaceHandler) HandleHTTP(c echo.Context) error {
	staff := GetStaff(c)
	if staff == nil {
		return response.Error(c, fmt.Errorf("%w: unauthorized", response.ErrUnauthorized))
	}

	var req DeclareWorkspaceRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, fmt.Errorf("%w: invalid payload", response.ErrInvalid))
	}
	if err := c.Validate(&req); err != nil {
		return response.Error(c, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error()))
	}

	// Validate capability for requested workspace
	switch req.Workspace {
	case WorkspaceManager:
		if !slices.Contains(staff.Roles, RoleManager) {
			return response.Error(c, fmt.Errorf("%w: bạn không có quyền khu vực quản lý", response.ErrForbidden))
		}
	case WorkspacePreparation:
		if !slices.Contains(staff.Capabilities, "preparation.operate") {
			return response.Error(c, fmt.Errorf("%w: bạn không có quyền khu vực pha chế", response.ErrForbidden))
		}
	case WorkspaceCashier:
		if !slices.Contains(staff.Capabilities, "sales.operate") {
			return response.Error(c, fmt.Errorf("%w: bạn không có quyền khu vực thu ngân", response.ErrForbidden))
		}
	}

	err := h.queries.UpdateSessionWorkspace(c.Request().Context(), sqlc.UpdateSessionWorkspaceParams{
		ID:              staff.SessionID,
		ActiveWorkspace: sql.NullString{String: req.Workspace, Valid: true},
	})
	if err != nil {
		return response.Error(c, fmt.Errorf("update workspace: %w", err))
	}

	return response.OK(c, map[string]string{"workspace": req.Workspace})
}
