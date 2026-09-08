package auth

import (
	"context"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/labstack/echo/v4"
)

type ListIdentitiesHandler struct {
	queries *sqlc.Queries
}

func NewListIdentitiesHandler(queries *sqlc.Queries) *ListIdentitiesHandler {
	return &ListIdentitiesHandler{queries: queries}
}

func (h *ListIdentitiesHandler) Handle(ctx context.Context) ([]IdentitySummaryResponse, error) {
	rows, err := h.queries.ListActiveIdentities(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active identities: %w", err)
	}

	res := make([]IdentitySummaryResponse, len(rows))
	for i, r := range rows {
		res[i] = IdentitySummaryResponse{
			DisplayName: r.DisplayName,
			LoginCode:   r.LoginCode,
		}
	}
	return res, nil
}

func (h *ListIdentitiesHandler) HandleHTTP(c echo.Context) error {
	res, err := h.Handle(c.Request().Context())
	if err != nil {
		return response.Error(c, err)
	}
	return response.OK(c, res)
}
