package catalog

import (
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/labstack/echo/v4"
)

// handleSetItemDetails godoc
//
//	@Summary		Cập nhật thông tin hiển thị của món
//	@Description	Ghi đè mã món, huy hiệu và mô tả. Cả ba khóa đều bắt buộc; gửi null để xóa. Yêu cầu quyền catalog.administer_structure.
//	@Tags			Catalog
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			item_id	path		string					true	"Item ID (UUID)"
//	@Param			request	body		SetItemDetailsRequest	true	"Thông tin hiển thị"
//	@Success		200		{object}	response.APIResponse{data=ItemDetailsResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Failure		500		{object}	response.APIResponse
//	@Router			/catalog/items/{item_id}/details [patch]
func (s *Slices) handleSetItemDetails(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	itemID, err := parseUUIDParam(c, "item_id")
	if err != nil {
		return sendError(c, err)
	}
	req, err := bindBody[SetItemDetailsRequest](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(req.RequestID); err != nil {
		return sendError(c, err)
	}
	if !req.Code.Present || !req.Badge.Present || !req.Description.Present {
		return sendError(c, fmt.Errorf("%w: code, badge, and description are all required (null clears)", response.ErrInvalid))
	}

	status, res, err := s.SetItemDetails.Handle(c.Request().Context(), actor, SetItemDetailsCommand{
		RequestID: req.RequestID, ItemID: itemID,
		Code: req.Code.Value, Badge: req.Badge.Value, Description: req.Description.Value,
	})
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
}

// handleSetCategoryDetails godoc
//
//	@Summary		Cập nhật biểu tượng và thứ tự danh mục
//	@Description	Đặt biểu tượng (tên icon Lucide) và thứ tự hiển thị (0 đến 9999). Yêu cầu quyền catalog.administer_structure.
//	@Tags			Catalog
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			category_id	path		string						true	"Category ID (UUID)"
//	@Param			request		body		SetCategoryDetailsRequest	true	"Thông tin hiển thị"
//	@Success		200			{object}	response.APIResponse{data=CategoryDetailsResponse}
//	@Failure		400			{object}	response.APIResponse
//	@Failure		401			{object}	response.APIResponse
//	@Failure		403			{object}	response.APIResponse
//	@Failure		404			{object}	response.APIResponse
//	@Failure		409			{object}	response.APIResponse
//	@Failure		500			{object}	response.APIResponse
//	@Router			/catalog/categories/{category_id}/details [patch]
func (s *Slices) handleSetCategoryDetails(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	catID, err := parseUUIDParam(c, "category_id")
	if err != nil {
		return sendError(c, err)
	}
	req, err := bindBody[SetCategoryDetailsRequest](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(req.RequestID); err != nil {
		return sendError(c, err)
	}

	status, res, err := s.SetCategoryDetails.Handle(c.Request().Context(), actor, SetCategoryDetailsCommand{
		RequestID: req.RequestID, CategoryID: catID, Icon: req.Icon, DisplayOrder: req.DisplayOrder,
	})
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
}
