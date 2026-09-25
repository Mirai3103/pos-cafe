package catalog

import (
	"github.com/labstack/echo/v4"
)

// handleMoveItemCategory godoc
//
//	@Summary		Chuyển món sang danh mục khác
//	@Description	Chuyển món sang danh mục khác. Các loại trừ nhóm topping mà danh mục mới không cung cấp sẽ bị xóa. Yêu cầu quyền catalog.administer_structure.
//	@Tags			Catalog
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			item_id	path		string					true	"Item ID (UUID)"
//	@Param			request	body		MoveItemCategoryRequest	true	"Danh mục đích"
//	@Success		200		{object}	response.APIResponse{data=ItemCategoryResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Failure		500		{object}	response.APIResponse
//	@Router			/catalog/items/{item_id}/category [patch]
func (s *Slices) handleMoveItemCategory(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	itemID, err := parseUUIDParam(c, "item_id")
	if err != nil {
		return sendError(c, err)
	}
	req, err := bindBody[MoveItemCategoryRequest](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(req.RequestID); err != nil {
		return sendError(c, err)
	}
	status, res, err := s.MoveItemCategory.Handle(c.Request().Context(), actor, MoveItemCategoryCommand{
		RequestID: req.RequestID, ItemID: itemID, CategoryID: req.CategoryID,
	})
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
}
