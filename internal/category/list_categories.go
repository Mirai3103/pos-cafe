package category

import (
	"context"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/labstack/echo/v4"
)

// === Query (Input Data) ===

type ListQuery struct {
	ActiveOnly bool
}

// === Query Handler (Read Logic) ===

type ListHandler struct {
	queries *sqlc.Queries
}

func NewListHandler(queries *sqlc.Queries) *ListHandler {
	return &ListHandler{queries: queries}
}

func (h *ListHandler) Handle(ctx context.Context, q ListQuery) ([]Response, error) {
	var categories []sqlc.Category
	var err error

	if q.ActiveOnly {
		categories, err = h.queries.ListActiveCategories(ctx)
	} else {
		categories, err = h.queries.ListCategories(ctx)
	}

	if err != nil {
		return nil, fmt.Errorf("query categories list: %w", err)
	}

	result := make([]Response, len(categories))
	for i, c := range categories {
		result[i] = toResponse(c)
	}
	return result, nil
}

// === HTTP Transport Adapter ===

// HandleHTTP godoc
// @Summary List categories
// @Description Retrieve a list of all categories or active categories only
// @Tags Categories
// @Produce json
// @Param active_only query bool false "Filter active categories only (true/false)"
// @Success 200 {object} response.APIResponse{data=[]Response}
// @Router /categories [get]
func (h *ListHandler) HandleHTTP(c echo.Context) error {
	activeOnly := c.QueryParam("active_only") == "true"

	res, err := h.Handle(c.Request().Context(), ListQuery{ActiveOnly: activeOnly})
	if err != nil {
		return response.Error(c, err)
	}

	return response.OK(c, res)
}
