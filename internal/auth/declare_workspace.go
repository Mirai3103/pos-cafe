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
