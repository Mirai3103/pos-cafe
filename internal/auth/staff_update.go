package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/labstack/echo/v4"
)

type StaffUpdateHandler struct {
	db      *sql.DB
	queries *sqlc.Queries
}

func NewStaffUpdateHandler(db *sql.DB, queries *sqlc.Queries) *StaffUpdateHandler {
	return &StaffUpdateHandler{db: db, queries: queries}
}

func (h *StaffUpdateHandler) Handle(ctx context.Context, actor *StaffClaims, targetID uuid.UUID, req UpdateStaffRequest) (int, *StaffDetailResponse, error) {
	displayName := strings.TrimSpace(req.DisplayName)
	loginCode := strings.TrimSpace(req.LoginCode)
	if displayName == "" || loginCode == "" {
		return 0, nil, fmt.Errorf("%w: tên và mã đăng nhập không được để trống", response.ErrInvalid)
	}

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

	return ExecuteWithIdempotency(ctx, h.db, h.queries, actor.StaffID, req.RequestID, "staff.update", idempotencyPayload{TargetID: targetID, Body: req}, func(tx *sql.Tx, qtx *sqlc.Queries) (int, *StaffDetailResponse, error) {
		if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", EnabledManagerInvariantLockID); err != nil {
			return 0, nil, fmt.Errorf("acquire invariant lock: %w", err)
		}

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
		if err := ensureManagerRemains(ctx, qtx, target.Enabled, currentRoles, req.Roles); err != nil {
			return 0, nil, err
		}

		updated, err := qtx.UpdateStaffProfile(ctx, sqlc.UpdateStaffProfileParams{
			ID:          targetID,
			DisplayName: displayName,
			LoginCode:   loginCode,
		})
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return 0, nil, fmt.Errorf("%w: mã đăng nhập đã được sử dụng", response.ErrConflict)
			}
			return 0, nil, fmt.Errorf("update staff: %w", err)
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

		return http.StatusOK, &StaffDetailResponse{
			ID:          updated.ID,
			DisplayName: updated.DisplayName,
			LoginCode:   updated.LoginCode,
			Enabled:     updated.Enabled,
			Roles:       req.Roles,
			CreatedAt:   updated.CreatedAt,
		}, nil
	})
}

// HandleHTTP godoc
//
//	@Summary		Cập nhật thông tin và vai trò nhân viên
//	@Description	Đổi tên hiển thị, mã đăng nhập và toàn bộ vai trò trong một lệnh (ADR-063)
//	@Tags			Staff
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string				true	"Staff ID"
//	@Param			request	body		UpdateStaffRequest	true	"Thông tin mới"
//	@Success		200		{object}	response.APIResponse{data=StaffDetailResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Failure		500		{object}	response.APIResponse
//	@Router			/staff/{id} [patch]
func (h *StaffUpdateHandler) HandleHTTP(c echo.Context) error {
	staff := GetStaff(c)
	if staff == nil {
		return response.Error(c, fmt.Errorf("%w: unauthorized", response.ErrForbidden))
	}

	targetID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.Error(c, fmt.Errorf("%w: invalid staff id", response.ErrInvalid))
	}

	var req UpdateStaffRequest
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
