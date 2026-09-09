package auth

import (
	"context"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/labstack/echo/v4"
)

type ListIdentitiesHandler struct {
	queries sqlc.Querier
}

func NewListIdentitiesHandler(queries sqlc.Querier) *ListIdentitiesHandler {
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

// HandleHTTP godoc
//
//	@Summary		Danh sách định danh nhân viên đang hoạt động
//	@Description	Trả về danh sách tên hiển thị và mã đăng nhập của nhân viên đang hoạt động (dùng cho màn hình chọn nhân viên POS)
//	@Tags			Auth
//	@Produce		json
//	@Success		200	{object}	response.APIResponse{data=[]IdentitySummaryResponse}
//	@Failure		500	{object}	response.APIResponse
//	@Router			/auth/identities [get]
func (h *ListIdentitiesHandler) HandleHTTP(c echo.Context) error {
	res, err := h.Handle(c.Request().Context())
	if err != nil {
		return response.Error(c, err)
	}
	return response.OK(c, res)
}
