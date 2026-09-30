package catalog

import (
	"github.com/labstack/echo/v4"
)

// handleCreateCategory godoc
//
//	@Summary		Tạo danh mục thực đơn
//	@Description	Tạo một danh mục mới trong catalog. Yêu cầu quyền catalog.administer_structure.
//	@Tags			Catalog
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		CreateCategoryCommand	true	"Thông tin tạo danh mục"
//	@Success		201		{object}	response.APIResponse{data=CategoryResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Failure		500		{object}	response.APIResponse
//	@Router			/catalog/categories [post]
func (s *Slices) handleCreateCategory(c echo.Context) error {
	actor, _, req, err := bindCommand[CreateCategoryCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	return sendCommand(c, actor, s.CreateCategory.Handle, req)
}

// handleRenameCategory godoc
//
//	@Summary		Đổi tên danh mục thực đơn
//	@Description	Đổi tên danh mục hiện có. Yêu cầu quyền catalog.administer_structure.
//	@Tags			Catalog
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			category_id	path		string			true	"Category ID (UUID)"
//	@Param			request		body		RenameRequest	true	"Thông tin đổi tên danh mục"
//	@Success		200			{object}	response.APIResponse{data=CategoryResponse}
//	@Failure		400			{object}	response.APIResponse
//	@Failure		401			{object}	response.APIResponse
//	@Failure		403			{object}	response.APIResponse
//	@Failure		404			{object}	response.APIResponse
//	@Failure		409			{object}	response.APIResponse
//	@Failure		500			{object}	response.APIResponse
//	@Router			/catalog/categories/{category_id}/name [patch]
func (s *Slices) handleRenameCategory(c echo.Context) error {
	actor, ids, req, err := bindCommand[RenameRequest](c, "category_id")
	if err != nil {
		return sendError(c, err)
	}
	return sendCommand(c, actor, s.RenameCategory.Handle, RenameCategoryCommand{
		RequestID:  req.RequestID,
		CategoryID: ids[0],
		Name:       req.Name,
	})
}

// handleRetireCategory godoc
//
//	@Summary		Ngừng kinh doanh danh mục thực đơn
//	@Description	Đánh dấu ngừng kinh doanh vĩnh viễn danh mục thực đơn. Yêu cầu quyền catalog.administer_structure.
//	@Tags			Catalog
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			category_id	path		string			true	"Category ID (UUID)"
//	@Param			request		body		RetireRequest	true	"Lý do và ghi chú ngừng kinh doanh"
//	@Success		200			{object}	response.APIResponse{data=CategoryResponse}
//	@Failure		400			{object}	response.APIResponse
//	@Failure		401			{object}	response.APIResponse
//	@Failure		403			{object}	response.APIResponse
//	@Failure		404			{object}	response.APIResponse
//	@Failure		409			{object}	response.APIResponse
//	@Failure		500			{object}	response.APIResponse
//	@Router			/catalog/categories/{category_id}/retirement [post]
func (s *Slices) handleRetireCategory(c echo.Context) error {
	actor, ids, req, err := bindCommand[RetireRequest](c, "category_id")
	if err != nil {
		return sendError(c, err)
	}
	return sendCommand(c, actor, s.RetireCategory.Handle, RetireCategoryCommand{
		RequestID:  req.RequestID,
		CategoryID: ids[0],
		Reason:     req.Reason,
		Note:       req.Note,
	})
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
	actor, ids, req, err := bindCommand[SetCategoryDetailsRequest](c, "category_id")
	if err != nil {
		return sendError(c, err)
	}
	return sendCommand(c, actor, s.SetCategoryDetails.Handle, SetCategoryDetailsCommand{
		RequestID: req.RequestID, CategoryID: ids[0], Icon: req.Icon, DisplayOrder: req.DisplayOrder,
	})
}
