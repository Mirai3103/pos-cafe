package auth

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/labstack/echo/v4"
)

const BootstrapManagerAdvisoryLockID = 739201

type BootstrapManagerHandler struct {
	db      *sql.DB
	queries *sqlc.Queries
}

func NewBootstrapManagerHandler(db *sql.DB, queries *sqlc.Queries) *BootstrapManagerHandler {
	return &BootstrapManagerHandler{db: db, queries: queries}
}

func (h *BootstrapManagerHandler) Handle(ctx context.Context, req BootstrapManagerRequest) (*StaffProfileResponse, error) {
	pinHash, err := HashPin(req.Pin)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error())
	}

	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Acquire transactional advisory lock to prevent concurrent initialization
	var locked bool
	if err := tx.QueryRowContext(ctx, "SELECT pg_advisory_xact_lock($1)", BootstrapManagerAdvisoryLockID).Scan(&locked); err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("acquire bootstrap advisory lock: %w", err)
	}

	qtx := h.queries.WithTx(tx)

	// Check if any manager already exists
	activeCount, err := qtx.CountActiveManagers(ctx)
	if err != nil {
		return nil, fmt.Errorf("check existing managers: %w", err)
	}
	if activeCount > 0 {
		return nil, fmt.Errorf("%w: hệ thống đã có Quản lý được cài đặt", response.ErrConflict)
	}

	// Create manager identity
	created, err := qtx.CreateStaffIdentity(ctx, sqlc.CreateStaffIdentityParams{
		DisplayName: req.DisplayName,
		Btrim:       strings.ToUpper(strings.TrimSpace(req.LoginCode)),
		PinHash:     pinHash,
		Enabled:     true,
	})
	if err != nil {
		return nil, fmt.Errorf("create manager identity: %w", err)
	}

	// Assign MANAGER role
	if err := qtx.AddStaffRole(ctx, sqlc.AddStaffRoleParams{
		StaffIdentityID: created.ID,
		Role:            RoleManager,
	}); err != nil {
		return nil, fmt.Errorf("assign manager role: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit bootstrap transaction: %w", err)
	}

	return &StaffProfileResponse{
		ID:           created.ID,
		DisplayName:  created.DisplayName,
		LoginCode:    created.LoginCode,
		Enabled:      created.Enabled,
		Roles:        []string{RoleManager},
		Capabilities: DeriveCapabilities([]string{RoleManager}),
	}, nil
}

func (h *BootstrapManagerHandler) HandleHTTP(c echo.Context) error {
	var req BootstrapManagerRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, fmt.Errorf("%w: invalid payload", response.ErrInvalid))
	}
	if err := c.Validate(&req); err != nil {
		return response.Error(c, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error()))
	}

	res, err := h.Handle(c.Request().Context(), req)
	if err != nil {
		return response.Error(c, err)
	}

	return response.Created(c, res)
}
