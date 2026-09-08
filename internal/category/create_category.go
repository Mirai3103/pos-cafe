package category

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/eventbus"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/labstack/echo/v4"
)

// === Command (Input Data) ===

type CreateCommand struct {
	Name         string `json:"name" validate:"required,min=2,max=100"`
	Description  string `json:"description" validate:"max=500"`
	DisplayOrder int64  `json:"display_order" validate:"gte=0"`
	IsActive     *bool  `json:"is_active"`
}

// === Event (Domain Event) ===

const TopicCategoryCreated = "category.created"

type CreatedEvent struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// === Command Handler (Business Logic) ===

type CreateHandler struct {
	queries *sqlc.Queries
	bus     *eventbus.Bus
}

func NewCreateHandler(queries *sqlc.Queries, bus *eventbus.Bus) *CreateHandler {
	return &CreateHandler{queries: queries, bus: bus}
}

func (h *CreateHandler) Handle(ctx context.Context, cmd CreateCommand) (*Response, error) {
	// 1. Business Rule: Tên danh mục không được trùng lặp
	existing, err := h.queries.GetCategoryByName(ctx, cmd.Name)
	if err == nil && existing.ID > 0 {
		return nil, fmt.Errorf("%w: category with name '%s'", response.ErrConflict, cmd.Name)
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("check category uniqueness: %w", err)
	}

	// 2. Defaulting logic
	isActive := true
	if cmd.IsActive != nil {
		isActive = *cmd.IsActive
	}

	// 3. Thực thi lưu trữ (Database Mutation)
	category, err := h.queries.CreateCategory(ctx, sqlc.CreateCategoryParams{
		Name:         cmd.Name,
		Description:  cmd.Description,
		DisplayOrder: cmd.DisplayOrder,
		IsActive:     isActive,
	})
	if err != nil {
		return nil, fmt.Errorf("create category in database: %w", err)
	}

	res := toResponse(category)

	// 4. Bắn Domain Event ra EventBus (nếu bus được truyền vào)
	if h.bus != nil {
		_ = h.bus.Publish(TopicCategoryCreated, CreatedEvent{
			ID:   res.ID,
			Name: res.Name,
		})
	}

	return &res, nil
}

// === HTTP Transport Adapter ===

// HandleHTTP godoc
// @Summary Create a new category
// @Description Create a new drink/food category for POS Cafe
// @Tags Categories
// @Accept json
// @Produce json
// @Param request body CreateCommand true "Category payload"
// @Success 201 {object} response.APIResponse{data=Response}
// @Failure 400 {object} response.APIResponse
// @Failure 409 {object} response.APIResponse
// @Router /categories [post]
func (h *CreateHandler) HandleHTTP(c echo.Context) error {
	var cmd CreateCommand
	if err := c.Bind(&cmd); err != nil {
		return response.Error(c, fmt.Errorf("%w: invalid request body", response.ErrInvalid))
	}

	if err := c.Validate(&cmd); err != nil {
		return response.Error(c, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error()))
	}

	res, err := h.Handle(c.Request().Context(), cmd)
	if err != nil {
		return response.Error(c, err)
	}

	return response.Created(c, res)
}
