package catalog

import (
	"github.com/labstack/echo/v4"
)

// handleAttachItemModifierGroup godoc
//
//	@Summary		Gắn nhóm tùy chọn trực tiếp vào món
//	@Description	Gắn một nhóm modifier trực tiếp vào món thực đơn. Yêu cầu quyền catalog.administer_structure.
//	@Tags			Catalog
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			item_id		path		string			true	"Item ID (UUID)"
//	@Param			group_id	path		string			true	"Group ID (UUID)"
//	@Param			request		body		MutationRequest	true	"Request ID idempotency"
//	@Success		200			{object}	response.APIResponse{data=ItemModifierGroupResponse}
//	@Failure		400			{object}	response.APIResponse
//	@Failure		401			{object}	response.APIResponse
//	@Failure		403			{object}	response.APIResponse
//	@Failure		404			{object}	response.APIResponse
//	@Failure		409			{object}	response.APIResponse
//	@Failure		500			{object}	response.APIResponse
//	@Router			/catalog/items/{item_id}/modifier-groups/{group_id} [post]
func (s *Slices) handleAttachItemModifierGroup(c echo.Context) error {
	actor, ids, req, err := bindCommand[MutationRequest](c, "item_id", "group_id")
	if err != nil {
		return sendError(c, err)
	}
	return sendCommand(c, actor, s.AttachItemModifierGroup.Handle, AttachItemModifierGroupCommand{
		RequestID:       req.RequestID,
		ItemID:          ids[0],
		ModifierGroupID: ids[1],
	})
}

// handleAttachCategoryModifierGroup godoc
//
//	@Summary		Gắn nhóm tùy chọn vào danh mục
//	@Description	Gắn một nhóm modifier vào danh mục để các món con kế thừa. Yêu cầu quyền catalog.administer_structure.
//	@Tags			Catalog
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			category_id	path		string			true	"Category ID (UUID)"
//	@Param			group_id	path		string			true	"Group ID (UUID)"
//	@Param			request		body		MutationRequest	true	"Request ID idempotency"
//	@Success		200			{object}	response.APIResponse{data=CategoryModifierGroupResponse}
//	@Failure		400			{object}	response.APIResponse
//	@Failure		401			{object}	response.APIResponse
//	@Failure		403			{object}	response.APIResponse
//	@Failure		404			{object}	response.APIResponse
//	@Failure		409			{object}	response.APIResponse
//	@Failure		500			{object}	response.APIResponse
//	@Router			/catalog/categories/{category_id}/modifier-groups/{group_id} [post]
func (s *Slices) handleAttachCategoryModifierGroup(c echo.Context) error {
	actor, ids, req, err := bindCommand[MutationRequest](c, "category_id", "group_id")
	if err != nil {
		return sendError(c, err)
	}
	return sendCommand(c, actor, s.AttachCategoryModifierGroup.Handle, AttachCategoryModifierGroupCommand{
		RequestID:       req.RequestID,
		CategoryID:      ids[0],
		ModifierGroupID: ids[1],
	})
}

// handleExcludeInheritedModifierGroup godoc
//
//	@Summary		Loại trừ nhóm tùy chọn kế thừa cho món
//	@Description	Đánh dấu loại trừ một nhóm modifier kế thừa từ danh mục cho món cụ thể. Yêu cầu quyền catalog.administer_structure.
//	@Tags			Catalog
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			item_id		path		string			true	"Item ID (UUID)"
//	@Param			group_id	path		string			true	"Group ID (UUID)"
//	@Param			request		body		MutationRequest	true	"Request ID idempotency"
//	@Success		200			{object}	response.APIResponse{data=ItemModifierGroupExclusionResponse}
//	@Failure		400			{object}	response.APIResponse
//	@Failure		401			{object}	response.APIResponse
//	@Failure		403			{object}	response.APIResponse
//	@Failure		404			{object}	response.APIResponse
//	@Failure		409			{object}	response.APIResponse
//	@Failure		500			{object}	response.APIResponse
//	@Router			/catalog/items/{item_id}/inherited-modifier-group-exclusions/{group_id} [post]
func (s *Slices) handleExcludeInheritedModifierGroup(c echo.Context) error {
	actor, ids, req, err := bindCommand[MutationRequest](c, "item_id", "group_id")
	if err != nil {
		return sendError(c, err)
	}
	return sendCommand(c, actor, s.ExcludeItemInheritedModifierGroup.Handle, ExcludeInheritedModifierGroupCommand{
		RequestID:       req.RequestID,
		ItemID:          ids[0],
		ModifierGroupID: ids[1],
	})
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
	actor, ids, req, err := bindCommand[ReplaceItemModifierGroupsRequest](c, "item_id")
	if err != nil {
		return sendError(c, err)
	}
	return sendCommand(c, actor, s.ReplaceItemModifierGroups.Handle, ReplaceItemModifierGroupsCommand{
		RequestID: req.RequestID, ItemID: ids[0], DirectGroupIDs: req.DirectGroupIDs, ExcludedGroupIDs: req.ExcludedGroupIDs,
	})
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
	actor, ids, req, err := bindCommand[ReplaceCategoryModifierGroupsRequest](c, "category_id")
	if err != nil {
		return sendError(c, err)
	}
	return sendCommand(c, actor, s.ReplaceCategoryModifierGroups.Handle, ReplaceCategoryModifierGroupsCommand{
		RequestID: req.RequestID, CategoryID: ids[0], GroupIDs: req.GroupIDs,
	})
}

// handleReplaceGroupAssignments godoc
//
//	@Summary		Gán hàng loạt một nhóm topping
//	@Description	Đặt đúng tập món và danh mục được gán trực tiếp nhóm topping này (Batch Linker). Gỡ nhóm khỏi danh mục sẽ xóa các loại trừ liên quan. Yêu cầu quyền catalog.administer_structure.
//	@Tags			Catalog
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			group_id	path		string							true	"Modifier Group ID (UUID)"
//	@Param			request		body		ReplaceGroupAssignmentsRequest	true	"Tập món và danh mục mong muốn"
//	@Success		200			{object}	response.APIResponse{data=ModifierGroupAssignmentsResponse}
//	@Failure		400			{object}	response.APIResponse
//	@Failure		401			{object}	response.APIResponse
//	@Failure		403			{object}	response.APIResponse
//	@Failure		404			{object}	response.APIResponse
//	@Failure		409			{object}	response.APIResponse
//	@Failure		500			{object}	response.APIResponse
//	@Router			/catalog/modifier-groups/{group_id}/assignments [put]
func (s *Slices) handleReplaceGroupAssignments(c echo.Context) error {
	actor, ids, req, err := bindCommand[ReplaceGroupAssignmentsRequest](c, "group_id")
	if err != nil {
		return sendError(c, err)
	}
	return sendCommand(c, actor, s.ReplaceGroupAssignments.Handle, ReplaceGroupAssignmentsCommand{
		RequestID: req.RequestID, GroupID: ids[0], ItemIDs: req.ItemIDs, CategoryIDs: req.CategoryIDs,
	})
}
