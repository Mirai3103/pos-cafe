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

type DeleteCommand struct {
	ID int64
}

// === Dependency Boundary ===

type categoryDeleter interface {
	GetCategoryByID(ctx context.Context, id int64) (sqlc.Category, error)
	DeleteCategory(ctx context.Context, id int64) error
}

// === Command Handler (Business Logic) ===

type DeleteHandler struct {
	store categoryDeleter
}

func NewDeleteHandler(store categoryDeleter) *DeleteHandler {
	return &DeleteHandler{store: store}
}

func (h *DeleteHandler) Handle(ctx context.Context, cmd DeleteCommand) error {
	// Check existence first so a missing row maps to 404, not a silent no-op
	_, err := h.store.GetCategoryByID(ctx, cmd.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: category id %d", response.ErrNotFound, cmd.ID)
		}
		return fmt.Errorf("find category: %w", err)
	}

	if err := h.store.DeleteCategory(ctx, cmd.ID); err != nil {
		return fmt.Errorf("delete category from database: %w", err)
	}

	return nil
}

// === HTTP Transport Adapter ===

// HandleHTTP godoc
// @Summary Delete category
// @Description Delete a category by its ID
// @Tags Categories
// @Produce json
// @Param id path int true "Category ID"
// @Success 204 "No Content"
// @Failure 400 {object} response.APIResponse
// @Failure 404 {object} response.APIResponse
// @Router /categories/{id} [delete]
func (h *DeleteHandler) HandleHTTP(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		return response.Error(c, fmt.Errorf("%w: invalid category id", response.ErrInvalid))
	}

	if err := h.Handle(c.Request().Context(), DeleteCommand{ID: id}); err != nil {
		return response.Error(c, err)
	}

	return response.NoContent(c)
}
