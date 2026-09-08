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

// === Query (Input Data) ===

type GetByIDQuery struct {
	ID int64
}

// === Dependency Boundary ===

type categoryGetter interface {
	GetCategoryByID(ctx context.Context, id int64) (sqlc.Category, error)
}

// === Query Handler (Read Logic) ===

type GetByIDHandler struct {
	store categoryGetter
}

func NewGetByIDHandler(store categoryGetter) *GetByIDHandler {
	return &GetByIDHandler{store: store}
}

func (h *GetByIDHandler) Handle(ctx context.Context, q GetByIDQuery) (*Response, error) {
	category, err := h.store.GetCategoryByID(ctx, q.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: category id %d", response.ErrNotFound, q.ID)
		}
		return nil, fmt.Errorf("query category by id: %w", err)
	}

	res := toResponse(category)
	return &res, nil
}

// === HTTP Transport Adapter ===

// HandleHTTP godoc
// @Summary Get category by ID
// @Description Retrieve details of a specific category by its ID
// @Tags Categories
// @Produce json
// @Param id path int true "Category ID"
// @Success 200 {object} response.APIResponse{data=Response}
// @Failure 400 {object} response.APIResponse
// @Failure 404 {object} response.APIResponse
// @Router /categories/{id} [get]
func (h *GetByIDHandler) HandleHTTP(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		return response.Error(c, fmt.Errorf("%w: invalid category id", response.ErrInvalid))
	}

	res, err := h.Handle(c.Request().Context(), GetByIDQuery{ID: id})
	if err != nil {
		return response.Error(c, err)
	}

	return response.OK(c, res)
}
