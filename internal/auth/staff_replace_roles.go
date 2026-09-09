package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

type StaffReplaceRolesHandler struct {
	db      *sql.DB
	queries *sqlc.Queries
}

func NewStaffReplaceRolesHandler(db *sql.DB, queries *sqlc.Queries) *StaffReplaceRolesHandler {
	return &StaffReplaceRolesHandler{db: db, queries: queries}
}

func (h *StaffReplaceRolesHandler) Handle(ctx context.Context, actor *StaffClaims, targetID uuid.UUID, req ReplaceStaffRolesRequest) (int, *StaffDetailResponse, error) {
	actorStaff, err := h.queries.GetStaffByID(ctx, actor.StaffID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			VerifyPin("", req.ManagerPin)
			return 0, nil, fmt.Errorf("%w: PIN Quản lý không đúng", response.ErrForbidden)
		}
		return 0, nil, fmt.Errorf("lookup actor: %w", err)
	}
	if !VerifyPin(actorStaff.PinHash, req.ManagerPin) {
		return 0, nil, fmt.Errorf("%w: PIN Quản lý không đúng", response.ErrForbidden)
	}

	return ExecuteWithIdempotency(ctx, h.queries, actor.StaffID, req.RequestID, "staff.replace_roles", req, func() (int, *StaffDetailResponse, error) {
		tx, err := h.db.BeginTx(ctx, nil)
		if err != nil {
			return 0, nil, fmt.Errorf("begin tx: %w", err)
		}
		defer func() { _ = tx.Rollback() }()

		if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", EnabledManagerInvariantLockID); err != nil {
			return 0, nil, fmt.Errorf("acquire invariant lock: %w", err)
		}

		qtx := h.queries.WithTx(tx)

		target, err := qtx.GetStaffByID(ctx, targetID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return 0, nil, fmt.Errorf("%w: không tìm thấy nhân viên", response.ErrNotFound)
			}
			return 0, nil, fmt.Errorf("get target: %w", err)
		}

		currentRoles, err := qtx.GetStaffRoles(ctx, targetID)
		if err != nil {
			return 0, nil, fmt.Errorf("get target roles: %w", err)
		}

		// Invariant check: If removing MANAGER role from an active manager, ensure at least one other active manager remains
		hadManager := slices.Contains(currentRoles, RoleManager)
		willHaveManager := slices.Contains(req.Roles, RoleManager)
		if target.Enabled && hadManager && !willHaveManager {
			activeManagers, err := qtx.CountActiveManagers(ctx)
			if err != nil {
				return 0, nil, fmt.Errorf("count active managers: %w", err)
			}
			if activeManagers <= 1 {
				return 0, nil, fmt.Errorf("%w: phải còn ít nhất một Quản lý đang hoạt động", response.ErrManagerInvariant)
			}
		}

		if err := qtx.ClearStaffRoles(ctx, targetID); err != nil {
			return 0, nil, fmt.Errorf("clear roles: %w", err)
		}

		for _, r := range req.Roles {
			if err := qtx.AddStaffRole(ctx, sqlc.AddStaffRoleParams{
				StaffIdentityID: targetID,
				Role:            r,
			}); err != nil {
				return 0, nil, fmt.Errorf("add role: %w", err)
			}
		}

		if err := tx.Commit(); err != nil {
			return 0, nil, fmt.Errorf("commit tx: %w", err)
		}

		return http.StatusOK, &StaffDetailResponse{
			ID:          target.ID,
			DisplayName: target.DisplayName,
			LoginCode:   target.LoginCode,
			Enabled:     target.Enabled,
			Roles:       req.Roles,
			CreatedAt:   target.CreatedAt,
		}, nil
	})
}

// HandleHTTP godoc
//
//	@Summary		Cập nhật danh sách vai trò nhân viên
//	@Description	Thay thế toàn bộ vai trò của nhân viên bằng danh sách mới
//	@Tags			Staff
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string						true	"Staff ID"
//	@Param			request	body		ReplaceStaffRolesRequest	true	"Danh sách vai trò mới"
//	@Success		200		{object}	response.APIResponse{data=StaffDetailResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Failure		500		{object}	response.APIResponse
//	@Router			/staff/{id}/roles [put]
func (h *StaffReplaceRolesHandler) HandleHTTP(c echo.Context) error {
	staff := GetStaff(c)
	if staff == nil {
		return response.Error(c, fmt.Errorf("%w: unauthorized", response.ErrForbidden))
	}

	targetID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.Error(c, fmt.Errorf("%w: invalid staff id", response.ErrInvalid))
	}

	var req ReplaceStaffRolesRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, fmt.Errorf("%w: invalid payload", response.ErrInvalid))
	}
	if err := c.Validate(&req); err != nil {
		return response.Error(c, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error()))
	}

	_, res, err := h.Handle(c.Request().Context(), staff, targetID, req)
	if err != nil {
		return response.Error(c, err)
	}

	return response.OK(c, res)
}
