package catalog

import (
	"github.com/labstack/echo/v4"
)

// handleCreateModifierGroup godoc
//
//	@Summary		Tạo nhóm tùy chọn / topping
//	@Description	Tạo nhóm modifier mới kèm danh sách tùy chọn. Yêu cầu quyền catalog.administer_structure, catalog.change_price và PIN quản lý.
//	@Tags			Catalog
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		CreateModifierGroupCommand	true	"Thông tin tạo nhóm tùy chọn"
//	@Success		201		{object}	response.APIResponse{data=ModifierGroupResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Failure		500		{object}	response.APIResponse
//	@Router			/catalog/modifier-groups [post]
func (s *Slices) handleCreateModifierGroup(c echo.Context) error {
	actor, _, req, err := bindCommand[CreateModifierGroupCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	return sendCommand(c, actor, s.CreateModifierGroup.Handle, req)
}

// handleRenameModifierGroup godoc
//
//	@Summary		Đổi tên nhóm tùy chọn
//	@Description	Đổi tên nhóm modifier hiện có. Yêu cầu quyền catalog.administer_structure.
//	@Tags			Catalog
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			group_id	path		string			true	"Group ID (UUID)"
//	@Param			request		body		RenameRequest	true	"Thông tin đổi tên nhóm"
//	@Success		200			{object}	response.APIResponse{data=ModifierGroupResponse}
//	@Failure		400			{object}	response.APIResponse
//	@Failure		401			{object}	response.APIResponse
//	@Failure		403			{object}	response.APIResponse
//	@Failure		404			{object}	response.APIResponse
//	@Failure		409			{object}	response.APIResponse
//	@Failure		500			{object}	response.APIResponse
//	@Router			/catalog/modifier-groups/{group_id}/name [patch]
func (s *Slices) handleRenameModifierGroup(c echo.Context) error {
	actor, ids, req, err := bindCommand[RenameRequest](c, "group_id")
	if err != nil {
		return sendError(c, err)
	}
	return sendCommand(c, actor, s.RenameModifierGroup.Handle, RenameModifierGroupCommand{
		RequestID: req.RequestID,
		GroupID:   ids[0],
		Name:      req.Name,
	})
}

// handleSetModifierGroupDefaults godoc
//
//	@Summary		Cập nhật tùy chọn mặc định của nhóm
//	@Description	Thiết lập danh sách tùy chọn mặc định cho nhóm modifier. Yêu cầu quyền catalog.administer_structure.
//	@Tags			Catalog
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			group_id	path		string							true	"Group ID (UUID)"
//	@Param			request		body		SetModifierGroupDefaultsRequest	true	"Danh sách ID tùy chọn mặc định"
//	@Success		200			{object}	response.APIResponse{data=ModifierGroupDefaultsResponse}
//	@Failure		400			{object}	response.APIResponse
//	@Failure		401			{object}	response.APIResponse
//	@Failure		403			{object}	response.APIResponse
//	@Failure		404			{object}	response.APIResponse
//	@Failure		409			{object}	response.APIResponse
//	@Failure		500			{object}	response.APIResponse
//	@Router			/catalog/modifier-groups/{group_id}/defaults [put]
func (s *Slices) handleSetModifierGroupDefaults(c echo.Context) error {
	actor, ids, req, err := bindCommand[SetModifierGroupDefaultsRequest](c, "group_id")
	if err != nil {
		return sendError(c, err)
	}
	return sendCommand(c, actor, s.SetModifierGroupDefaults.Handle, SetModifierGroupDefaultsCommand{
		RequestID: req.RequestID,
		GroupID:   ids[0],
		OptionIDs: req.OptionIDs,
	})
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
	actor, ids, req, err := bindCommand[SetSelectionRuleRequest](c, "group_id")
	if err != nil {
		return sendError(c, err)
	}
	return sendCommand(c, actor, s.SetSelectionRule.Handle, SetSelectionRuleCommand{
		RequestID: req.RequestID, GroupID: ids[0], MinSelections: req.MinSelections, MaxSelections: req.MaxSelections, DefaultOptionIDs: req.DefaultOptionIDs,
	})
}

// handleRetireModifierGroup godoc
//
//	@Summary		Ngừng kinh doanh nhóm tùy chọn
//	@Description	Đánh dấu ngừng kinh doanh vĩnh viễn nhóm modifier. Yêu cầu quyền catalog.administer_structure.
//	@Tags			Catalog
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			group_id	path		string			true	"Group ID (UUID)"
//	@Param			request		body		RetireRequest	true	"Lý do và ghi chú ngừng kinh doanh"
//	@Success		200		{object}	response.APIResponse{data=ModifierGroupResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Failure		500		{object}	response.APIResponse
//	@Router			/catalog/modifier-groups/{group_id}/retirement [post]
func (s *Slices) handleRetireModifierGroup(c echo.Context) error {
	actor, ids, req, err := bindCommand[RetireRequest](c, "group_id")
	if err != nil {
		return sendError(c, err)
	}
	return sendCommand(c, actor, s.RetireModifierGroup.Handle, RetireModifierGroupCommand{
		RequestID: req.RequestID,
		GroupID:   ids[0],
		Reason:    req.Reason,
		Note:      req.Note,
	})
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
	actor, ids, req, err := bindCommand[AddModifierOptionRequest](c, "group_id")
	if err != nil {
		return sendError(c, err)
	}
	return sendCommand(c, actor, s.AddModifierOption.Handle, AddModifierOptionCommand{
		RequestID: req.RequestID, GroupID: ids[0], Name: req.Name, SurchargeVND: req.SurchargeVND, ManagerPIN: req.ManagerPIN,
	})
}

// handleRenameModifierOption godoc
//
//	@Summary		Đổi tên tùy chọn modifier
//	@Description	Đổi tên tùy chọn hiện có. Yêu cầu quyền catalog.administer_structure.
//	@Tags			Catalog
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			option_id	path		string			true	"Option ID (UUID)"
//	@Param			request		body		RenameRequest	true	"Thông tin đổi tên tùy chọn"
//	@Success		200			{object}	response.APIResponse{data=ModifierOptionResponse}
//	@Failure		400			{object}	response.APIResponse
//	@Failure		401			{object}	response.APIResponse
//	@Failure		403			{object}	response.APIResponse
//	@Failure		404			{object}	response.APIResponse
//	@Failure		409			{object}	response.APIResponse
//	@Failure		500			{object}	response.APIResponse
//	@Router			/catalog/modifier-options/{option_id}/name [patch]
func (s *Slices) handleRenameModifierOption(c echo.Context) error {
	actor, ids, req, err := bindCommand[RenameRequest](c, "option_id")
	if err != nil {
		return sendError(c, err)
	}
	return sendCommand(c, actor, s.RenameModifierOption.Handle, RenameModifierOptionCommand{
		RequestID: req.RequestID,
		OptionID:  ids[0],
		Name:      req.Name,
	})
}

// handleRepriceModifierOption godoc
//
//	@Summary		Cập nhật phụ thu tùy chọn modifier
//	@Description	Thay đổi số tiền phụ thu của tùy chọn. Yêu cầu quyền catalog.administer_structure, catalog.change_price và PIN quản lý.
//	@Tags			Catalog
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			option_id	path		string							true	"Option ID (UUID)"
//	@Param			request		body		RepriceModifierOptionRequest	true	"Thông tin đổi phụ thu"
//	@Success		200			{object}	response.APIResponse{data=ModifierOptionResponse}
//	@Failure		400			{object}	response.APIResponse
//	@Failure		401			{object}	response.APIResponse
//	@Failure		403			{object}	response.APIResponse
//	@Failure		404			{object}	response.APIResponse
//	@Failure		409			{object}	response.APIResponse
//	@Failure		500			{object}	response.APIResponse
//	@Router			/catalog/modifier-options/{option_id}/price [patch]
func (s *Slices) handleRepriceModifierOption(c echo.Context) error {
	actor, ids, req, err := bindCommand[RepriceModifierOptionRequest](c, "option_id")
	if err != nil {
		return sendError(c, err)
	}
	return sendCommand(c, actor, s.RepriceModifierOption.Handle, RepriceModifierOptionCommand{
		RequestID:    req.RequestID,
		OptionID:     ids[0],
		SurchargeVND: req.SurchargeVND,
		ManagerPIN:   req.ManagerPIN,
	})
}

// handleRetireModifierOption godoc
//
//	@Summary		Ngừng kinh doanh tùy chọn modifier
//	@Description	Đánh dấu ngừng kinh doanh vĩnh viễn tùy chọn modifier. Yêu cầu quyền catalog.administer_structure.
//	@Tags			Catalog
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			option_id	path		string			true	"Option ID (UUID)"
//	@Param			request		body		RetireRequest	true	"Lý do và ghi chú ngừng kinh doanh"
//	@Success		200		{object}	response.APIResponse{data=ModifierOptionResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Failure		500		{object}	response.APIResponse
//	@Router			/catalog/modifier-options/{option_id}/retirement [post]
func (s *Slices) handleRetireModifierOption(c echo.Context) error {
	actor, ids, req, err := bindCommand[RetireRequest](c, "option_id")
	if err != nil {
		return sendError(c, err)
	}
	return sendCommand(c, actor, s.RetireModifierOption.Handle, RetireModifierOptionCommand{
		RequestID: req.RequestID,
		OptionID:  ids[0],
		Reason:    req.Reason,
		Note:      req.Note,
	})
}
