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

const EnabledManagerInvariantLockID = 1247091103

type StaffSetEnabledHandler struct {
	db      *sql.DB
	queries *sqlc.Queries
}

func NewStaffSetEnabledHandler(db *sql.DB, queries *sqlc.Queries) *StaffSetEnabledHandler {
	return &StaffSetEnabledHandler{db: db, queries: queries}
}

func (h *StaffSetEnabledHandler) Handle(ctx context.Context, actor *StaffClaims, targetID uuid.UUID, req SetStaffEnabledRequest) (int, *StaffDetailResponse, error) {
	if req.Enabled == req.ExpectedEnabled {
		return 0, nil, fmt.Errorf("%w: trạng thái mới phải khác trạng thái hiện tại", response.ErrInvalid)
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

	return ExecuteWithIdempotency(ctx, h.queries, actor.StaffID, req.RequestID, "staff.set_enabled", req, func() (int, *StaffDetailResponse, error) {
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

		if target.Enabled != req.ExpectedEnabled {
			return 0, nil, fmt.Errorf("%w: trạng thái nhân viên đã thay đổi, vui lòng tải lại", response.ErrConflict)
		}

		roles, err := qtx.GetStaffRoles(ctx, targetID)
		if err != nil {
			return 0, nil, fmt.Errorf("get target roles: %w", err)
		}

		// Invariant check: If disabling a manager, ensure at least one other active manager remains
		if !req.Enabled && slices.Contains(roles, RoleManager) {
			activeManagers, err := qtx.CountActiveManagers(ctx)
			if err != nil {
				return 0, nil, fmt.Errorf("count active managers: %w", err)
			}
			if activeManagers <= 1 {
				return 0, nil, fmt.Errorf("%w: phải còn ít nhất một Quản lý đang hoạt động", response.ErrConflict)
			}
		}

		updated, err := qtx.SetStaffEnabled(ctx, sqlc.SetStaffEnabledParams{
			ID:      targetID,
			Enabled: req.Enabled,
		})
		if err != nil {
			return 0, nil, fmt.Errorf("update enabled: %w", err)
		}

		// If disabled, revoke all active sessions for this staff member
		if !req.Enabled {
			if err := qtx.RevokeAllStaffSessions(ctx, targetID); err != nil {
				return 0, nil, fmt.Errorf("revoke sessions: %w", err)
			}
		}

		if err := tx.Commit(); err != nil {
			return 0, nil, fmt.Errorf("commit tx: %w", err)
		}

		return http.StatusOK, &StaffDetailResponse{
			ID:          updated.ID,
			DisplayName: updated.DisplayName,
			LoginCode:   updated.LoginCode,
			Enabled:     updated.Enabled,
			Roles:       roles,
			CreatedAt:   target.CreatedAt,
		}, nil
	})
}

// HandleHTTP godoc
//
//	@Summary		Bật hoặc vô hiệu hóa tài khoản nhân viên
//	@Description	Kích hoạt hoặc ngưng kích hoạt tài khoản nhân viên
//	@Tags			Staff
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string					true	"Staff ID"
//	@Param			request	body		SetStaffEnabledRequest	true	"Thông tin trạng thái"
//	@Success		200		{object}	response.APIResponse{data=StaffDetailResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Failure		500		{object}	response.APIResponse
//	@Router			/staff/{id}/enabled [patch]
func (h *StaffSetEnabledHandler) HandleHTTP(c echo.Context) error {
	staff := GetStaff(c)
	if staff == nil {
		return response.Error(c, fmt.Errorf("%w: unauthorized", response.ErrForbidden))
	}

	targetID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.Error(c, fmt.Errorf("%w: invalid staff id", response.ErrInvalid))
	}

	var req SetStaffEnabledRequest
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
