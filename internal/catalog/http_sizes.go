package catalog

import (
	"github.com/labstack/echo/v4"
)

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
	actor, ids, req, err := bindCommand[AddSizeRequest](c, "item_id")
	if err != nil {
		return sendError(c, err)
	}
	return sendCommand(c, actor, s.AddSize.Handle, AddSizeCommand{
		RequestID: req.RequestID, ItemID: ids[0], Name: req.Name, PriceVND: req.PriceVND, ManagerPIN: req.ManagerPIN,
	})
}

// handleRenameSize godoc
//
//	@Summary		Đổi tên kích thước món
//	@Description	Đổi tên size của món. Yêu cầu quyền catalog.administer_structure.
//	@Tags			Catalog
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			size_id	path		string			true	"Size ID (UUID)"
//	@Param			request	body		RenameRequest	true	"Thông tin đổi tên size"
//	@Success		200		{object}	response.APIResponse{data=SizeResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Failure		500		{object}	response.APIResponse
//	@Router			/catalog/sizes/{size_id}/name [patch]
func (s *Slices) handleRenameSize(c echo.Context) error {
	actor, ids, req, err := bindCommand[RenameRequest](c, "size_id")
	if err != nil {
		return sendError(c, err)
	}
	return sendCommand(c, actor, s.RenameSize.Handle, RenameSizeCommand{
		RequestID: req.RequestID,
		SizeID:    ids[0],
		Name:      req.Name,
	})
}

// handleRepriceSize godoc
//
//	@Summary		Cập nhật giá kích thước món
//	@Description	Thay đổi giá tuyệt đối của size món. Yêu cầu quyền catalog.administer_structure, catalog.change_price và PIN quản lý.
//	@Tags			Catalog
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			size_id	path		string			true	"Size ID (UUID)"
//	@Param			request	body		RepriceRequest	true	"Thông tin đổi giá size"
//	@Success		200		{object}	response.APIResponse{data=SizeResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Failure		500		{object}	response.APIResponse
//	@Router			/catalog/sizes/{size_id}/price [patch]
func (s *Slices) handleRepriceSize(c echo.Context) error {
	actor, ids, req, err := bindCommand[RepriceRequest](c, "size_id")
	if err != nil {
		return sendError(c, err)
	}
	return sendCommand(c, actor, s.RepriceSize.Handle, RepriceSizeCommand{
		RequestID:  req.RequestID,
		SizeID:     ids[0],
		PriceVND:   req.PriceVND,
		ManagerPIN: req.ManagerPIN,
	})
}

// handleRetireSize godoc
//
//	@Summary		Ngừng kinh doanh kích thước món
//	@Description	Đánh dấu ngừng kinh doanh vĩnh viễn size món. Yêu cầu quyền catalog.administer_structure.
//	@Tags			Catalog
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			size_id	path		string			true	"Size ID (UUID)"
//	@Param			request	body		RetireRequest	true	"Lý do và ghi chú ngừng kinh doanh"
//	@Success		200		{object}	response.APIResponse{data=SizeResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Failure		500		{object}	response.APIResponse
//	@Router			/catalog/sizes/{size_id}/retirement [post]
func (s *Slices) handleRetireSize(c echo.Context) error {
	actor, ids, req, err := bindCommand[RetireRequest](c, "size_id")
	if err != nil {
		return sendError(c, err)
	}
	return sendCommand(c, actor, s.RetireSize.Handle, RetireSizeCommand{
		RequestID: req.RequestID,
		SizeID:    ids[0],
		Reason:    req.Reason,
		Note:      req.Note,
	})
}
