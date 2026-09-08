package category

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/labstack/echo/v4"
)

// === Command (Input Data) ===

type UpdateCommand struct {
	ID           int64  `json:"-"`
	Name         string `json:"name" validate:"required,min=2,max=100"`
	Description  string `json:"description" validate:"max=500"`
	DisplayOrder int64  `json:"display_order" validate:"gte=0"`
	IsActive     *bool  `json:"is_active"`
}

// === Command Handler (Business Logic) ===

type UpdateHandler struct {
	queries *sqlc.Queries
}

func NewUpdateHandler(queries *sqlc.Queries) *UpdateHandler {
	return &UpdateHandler{queries: queries}
}

func (h *UpdateHandler) Handle(ctx context.Context, cmd UpdateCommand) (*Response, error) {
	// 1. Kiểm tra tồn tại
	current, err := h.queries.GetCategoryByID(ctx, cmd.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: category id %d", response.ErrNotFound, cmd.ID)
		}
		return nil, fmt.Errorf("find category: %w", err)
	}

	// 2. Kiểm tra trùng tên với category khác
	if cmd.Name != current.Name {
		existing, err := h.queries.GetCategoryByName(ctx, cmd.Name)
		if err == nil && existing.ID != cmd.ID {
			return nil, fmt.Errorf("%w: category with name '%s'", response.ErrConflict, cmd.Name)
		} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("check uniqueness: %w", err)
		}
	}

	isActive := current.IsActive
	if cmd.IsActive != nil {
		if *cmd.IsActive {
			isActive = 1
		} else {
			isActive = 0
		}
	}

	category, err := h.queries.UpdateCategory(ctx, sqlc.UpdateCategoryParams{
		ID:           cmd.ID,
		Name:         cmd.Name,
		Description:  cmd.Description,
		DisplayOrder: cmd.DisplayOrder,
		IsActive:     isActive,
	})
	if err != nil {
		return nil, fmt.Errorf("update category: %w", err)
	}

	res := toResponse(category)
	return &res, nil
}

// === HTTP Transport Adapter ===

// HandleHTTP godoc
// @Summary Update category
// @Description Update an existing category by its ID
// @Tags Categories
// @Accept json
// @Produce json
// @Param id path int true "Category ID"
// @Param request body UpdateCommand true "Category update payload"
// @Success 200 {object} response.APIResponse{data=Response}
// @Failure 400 {object} response.APIResponse
// @Failure 404 {object} response.APIResponse
// @Failure 409 {object} response.APIResponse
// @Router /categories/{id} [put]
func (h *UpdateHandler) HandleHTTP(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		return response.Error(c, fmt.Errorf("%w: invalid category id", response.ErrInvalid))
	}

	var cmd UpdateCommand
	if err := c.Bind(&cmd); err != nil {
		return response.Error(c, fmt.Errorf("%w: invalid request body", response.ErrInvalid))
	}
	cmd.ID = id

	if err := c.Validate(&cmd); err != nil {
		return response.Error(c, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error()))
	}

	res, err := h.Handle(c.Request().Context(), cmd)
	if err != nil {
		return response.Error(c, err)
	}

	return response.OK(c, res)
}
