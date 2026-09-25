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

// handleAddSize godoc
//
//	@Summary		Thêm kích cỡ cho món
//	@Description	Thêm kích cỡ mới cho món đang bán theo kích cỡ. Món bán giá đơn không nhận kích cỡ. Yêu cầu quyền catalog.administer_structure, catalog.change_price và PIN quản lý.
//	@Tags			Catalog
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			item_id	path		string			true	"Item ID (UUID)"
//	@Param			request	body		AddSizeRequest	true	"Kích cỡ mới"
//	@Success		201		{object}	response.APIResponse{data=SizeResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Failure		500		{object}	response.APIResponse
//	@Router			/catalog/items/{item_id}/sizes [post]
func (s *Slices) handleAddSize(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	itemID, err := parseUUIDParam(c, "item_id")
	if err != nil {
		return sendError(c, err)
	}
	req, err := bindBody[AddSizeRequest](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(req.RequestID); err != nil {
		return sendError(c, err)
	}
	status, res, err := s.AddSize.Handle(c.Request().Context(), actor, AddSizeCommand{
		RequestID: req.RequestID, ItemID: itemID, Name: req.Name, PriceVND: req.PriceVND, ManagerPIN: req.ManagerPIN,
	})
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
}

// handleAddModifierOption godoc
//
//	@Summary		Thêm tùy chọn vào nhóm topping
//	@Description	Thêm tùy chọn mới vào nhóm topping. Tùy chọn mới không phải mặc định. Yêu cầu quyền catalog.administer_structure, catalog.change_price và PIN quản lý.
//	@Tags			Catalog
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			group_id	path		string						true	"Modifier Group ID (UUID)"
//	@Param			request		body		AddModifierOptionRequest	true	"Tùy chọn mới"
//	@Success		201			{object}	response.APIResponse{data=ModifierOptionResponse}
//	@Failure		400			{object}	response.APIResponse
//	@Failure		401			{object}	response.APIResponse
//	@Failure		403			{object}	response.APIResponse
//	@Failure		404			{object}	response.APIResponse
//	@Failure		409			{object}	response.APIResponse
//	@Failure		500			{object}	response.APIResponse
//	@Router			/catalog/modifier-groups/{group_id}/options [post]
func (s *Slices) handleAddModifierOption(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	groupID, err := parseUUIDParam(c, "group_id")
	if err != nil {
		return sendError(c, err)
	}
	req, err := bindBody[AddModifierOptionRequest](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(req.RequestID); err != nil {
		return sendError(c, err)
	}
	status, res, err := s.AddModifierOption.Handle(c.Request().Context(), actor, AddModifierOptionCommand{
		RequestID: req.RequestID, GroupID: groupID, Name: req.Name, SurchargeVND: req.SurchargeVND, ManagerPIN: req.ManagerPIN,
	})
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
}

// handleSetSelectionRule godoc
//
//	@Summary		Đổi quy tắc chọn của nhóm topping
//	@Description	Đổi số lựa chọn tối thiểu, tối đa và các tùy chọn mặc định cùng lúc. Yêu cầu quyền catalog.administer_structure.
//	@Tags			Catalog
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			group_id	path		string					true	"Modifier Group ID (UUID)"
//	@Param			request		body		SetSelectionRuleRequest	true	"Quy tắc chọn"
//	@Success		200			{object}	response.APIResponse{data=SelectionRuleResponse}
//	@Failure		400			{object}	response.APIResponse
//	@Failure		401			{object}	response.APIResponse
//	@Failure		403			{object}	response.APIResponse
//	@Failure		404			{object}	response.APIResponse
//	@Failure		409			{object}	response.APIResponse
//	@Failure		500			{object}	response.APIResponse
//	@Router			/catalog/modifier-groups/{group_id}/selection-rule [put]
func (s *Slices) handleSetSelectionRule(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	groupID, err := parseUUIDParam(c, "group_id")
	if err != nil {
		return sendError(c, err)
	}
	req, err := bindBody[SetSelectionRuleRequest](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(req.RequestID); err != nil {
		return sendError(c, err)
	}
	status, res, err := s.SetSelectionRule.Handle(c.Request().Context(), actor, SetSelectionRuleCommand{
		RequestID: req.RequestID, GroupID: groupID, MinSelections: req.MinSelections, MaxSelections: req.MaxSelections, DefaultOptionIDs: req.DefaultOptionIDs,
	})
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
}

// handleReplaceItemModifierGroups godoc
//
//	@Summary		Thay toàn bộ nhóm topping của món
//	@Description	Đặt đúng tập nhóm gán trực tiếp và tập nhóm loại trừ của món. Bỏ một id khỏi danh sách nghĩa là gỡ nó. Yêu cầu quyền catalog.administer_structure.
//	@Tags			Catalog
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			item_id	path		string								true	"Item ID (UUID)"
//	@Param			request	body		ReplaceItemModifierGroupsRequest	true	"Tập nhóm mong muốn"
//	@Success		200		{object}	response.APIResponse{data=ItemModifierGroupsResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Failure		500		{object}	response.APIResponse
//	@Router			/catalog/items/{item_id}/modifier-groups [put]
func (s *Slices) handleReplaceItemModifierGroups(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	itemID, err := parseUUIDParam(c, "item_id")
	if err != nil {
		return sendError(c, err)
	}
	req, err := bindBody[ReplaceItemModifierGroupsRequest](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(req.RequestID); err != nil {
		return sendError(c, err)
	}
	status, res, err := s.ReplaceItemModifierGroups.Handle(c.Request().Context(), actor, ReplaceItemModifierGroupsCommand{
		RequestID: req.RequestID, ItemID: itemID, DirectGroupIDs: req.DirectGroupIDs, ExcludedGroupIDs: req.ExcludedGroupIDs,
	})
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
}

// handleReplaceCategoryModifierGroups godoc
//
//	@Summary		Thay toàn bộ nhóm topping của danh mục
//	@Description	Đặt đúng tập nhóm danh mục cung cấp. Gỡ một nhóm sẽ xóa các loại trừ nhóm đó trên các món trong danh mục. Yêu cầu quyền catalog.administer_structure.
//	@Tags			Catalog
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			category_id	path		string									true	"Category ID (UUID)"
//	@Param			request		body		ReplaceCategoryModifierGroupsRequest	true	"Tập nhóm mong muốn"
//	@Success		200			{object}	response.APIResponse{data=CategoryModifierGroupsResponse}
//	@Failure		400			{object}	response.APIResponse
//	@Failure		401			{object}	response.APIResponse
//	@Failure		403			{object}	response.APIResponse
//	@Failure		404			{object}	response.APIResponse
//	@Failure		409			{object}	response.APIResponse
//	@Failure		500			{object}	response.APIResponse
//	@Router			/catalog/categories/{category_id}/modifier-groups [put]
func (s *Slices) handleReplaceCategoryModifierGroups(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	catID, err := parseUUIDParam(c, "category_id")
	if err != nil {
		return sendError(c, err)
	}
	req, err := bindBody[ReplaceCategoryModifierGroupsRequest](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(req.RequestID); err != nil {
		return sendError(c, err)
	}
	status, res, err := s.ReplaceCategoryModifierGroups.Handle(c.Request().Context(), actor, ReplaceCategoryModifierGroupsCommand{
		RequestID: req.RequestID, CategoryID: catID, GroupIDs: req.GroupIDs,
	})
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
}
