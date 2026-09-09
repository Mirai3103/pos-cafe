package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

type StaffResetPinHandler struct {
	db      *sql.DB
	queries *sqlc.Queries
}

func NewStaffResetPinHandler(db *sql.DB, queries *sqlc.Queries) *StaffResetPinHandler {
	return &StaffResetPinHandler{db: db, queries: queries}
}

func (h *StaffResetPinHandler) Handle(ctx context.Context, actor *StaffClaims, targetID uuid.UUID, req ResetStaffPinRequest) (int, map[string]string, error) {
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

	return ExecuteWithIdempotency(ctx, h.queries, actor.StaffID, req.RequestID, "staff.reset_pin", idempotencyPayload{TargetID: targetID, Body: req}, func() (int, map[string]string, error) {
		pinHash, err := HashPin(req.Pin)
		if err != nil {
			return 0, nil, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error())
		}

		tx, err := h.db.BeginTx(ctx, nil)
		if err != nil {
			return 0, nil, fmt.Errorf("begin tx: %w", err)
		}
		defer func() { _ = tx.Rollback() }()

		qtx := h.queries.WithTx(tx)

		target, err := qtx.GetStaffByID(ctx, targetID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return 0, nil, fmt.Errorf("%w: không tìm thấy nhân viên", response.ErrNotFound)
			}
			return 0, nil, fmt.Errorf("get target: %w", err)
		}

		if err := qtx.UpdateStaffPin(ctx, sqlc.UpdateStaffPinParams{
			ID:      target.ID,
			PinHash: pinHash,
		}); err != nil {
			return 0, nil, fmt.Errorf("update pin: %w", err)
		}

		if err := qtx.RevokeAllStaffSessions(ctx, targetID); err != nil {
			return 0, nil, fmt.Errorf("revoke sessions: %w", err)
		}

		if err := tx.Commit(); err != nil {
			return 0, nil, fmt.Errorf("commit tx: %w", err)
		}

		return http.StatusOK, map[string]string{"message": "đổi mã PIN thành công"}, nil
	})
}

// HandleHTTP godoc
//
//	@Summary		Đặt lại mã PIN nhân viên
//	@Description	Quản lý đặt lại mã PIN cho nhân viên và thu hồi các phiên đăng nhập hiện tại
//	@Tags			Staff
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string					true	"Staff ID"
//	@Param			request	body		ResetStaffPinRequest	true	"Mã PIN mới"
//	@Success		200		{object}	response.APIResponse{data=map[string]string}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		500		{object}	response.APIResponse
//	@Router			/staff/{id}/reset-pin [post]
func (h *StaffResetPinHandler) HandleHTTP(c echo.Context) error {
	staff := GetStaff(c)
	if staff == nil {
		return response.Error(c, fmt.Errorf("%w: unauthorized", response.ErrForbidden))
	}

	targetID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.Error(c, fmt.Errorf("%w: invalid staff id", response.ErrInvalid))
	}

	var req ResetStaffPinRequest
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
