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
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/labstack/echo/v4"
)

type StaffCreateHandler struct {
	db      *sql.DB
	queries *sqlc.Queries
}

func NewStaffCreateHandler(db *sql.DB, queries *sqlc.Queries) *StaffCreateHandler {
	return &StaffCreateHandler{db: db, queries: queries}
}

func (h *StaffCreateHandler) Handle(ctx context.Context, actor *StaffClaims, req CreateStaffRequest) (int, *StaffDetailResponse, error) {
	// 1. Verify manager pin
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

	// 2. Execute with Idempotency
	return ExecuteWithIdempotency(ctx, h.queries, actor.StaffID, req.RequestID, "staff.create", req, func() (int, *StaffDetailResponse, error) {
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

		created, err := qtx.CreateStaffIdentity(ctx, sqlc.CreateStaffIdentityParams{
			DisplayName: req.DisplayName,
			Btrim:       strings.ToUpper(strings.TrimSpace(req.LoginCode)),
			PinHash:     pinHash,
			Enabled:     req.Enabled,
		})
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return 0, nil, fmt.Errorf("%w: mã đăng nhập đã được sử dụng", response.ErrConflict)
			}
			return 0, nil, fmt.Errorf("insert staff: %w", err)
		}

		for _, role := range req.Roles {
			if err := qtx.AddStaffRole(ctx, sqlc.AddStaffRoleParams{
				StaffIdentityID: created.ID,
				Role:            role,
			}); err != nil {
				return 0, nil, fmt.Errorf("add role: %w", err)
			}
		}

		if err := tx.Commit(); err != nil {
			return 0, nil, fmt.Errorf("commit tx: %w", err)
		}

		return http.StatusCreated, &StaffDetailResponse{
			ID:          created.ID,
			DisplayName: created.DisplayName,
			LoginCode:   created.LoginCode,
			Enabled:     created.Enabled,
			Roles:       req.Roles,
			CreatedAt:   created.CreatedAt,
		}, nil
	})
}

// HandleHTTP godoc
// @Summary Tạo tài khoản nhân viên mới
// @Description Quản lý tạo nhân viên mới với mã đăng nhập, mã PIN và danh sách vai trò
// @Tags Staff
// @Accept json
// @Produce json
// @Param request body CreateStaffRequest true "Thông tin nhân viên mới"
// @Success 201 {object} response.APIResponse{data=StaffDetailResponse}
// @Failure 400 {object} response.APIResponse
// @Failure 403 {object} response.APIResponse
// @Failure 409 {object} response.APIResponse
// @Failure 500 {object} response.APIResponse
// @Router /staff [post]
func (h *StaffCreateHandler) HandleHTTP(c echo.Context) error {
	staff := GetStaff(c)
	if staff == nil {
		return response.Error(c, fmt.Errorf("%w: unauthorized", response.ErrForbidden))
	}

	var req CreateStaffRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, fmt.Errorf("%w: invalid payload", response.ErrInvalid))
	}
	if err := c.Validate(&req); err != nil {
		return response.Error(c, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error()))
	}

	code, res, err := h.Handle(c.Request().Context(), staff, req)
	if err != nil {
		return response.Error(c, err)
	}

	if code == http.StatusCreated {
		return response.Created(c, res)
	}
	return response.OK(c, res)
}
