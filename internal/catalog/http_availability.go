package catalog

import (
	"github.com/labstack/echo/v4"
)

// handleSetItemAvailability godoc
//
//	@Summary		Cập nhật trạng thái khả dụng của món
//	@Description	Bật hoặc tắt trạng thái khả dụng (còn hàng/hết hàng) của món. Yêu cầu quyền catalog.manage_availability.
//	@Tags			Catalog
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			item_id	path		string					true	"Item ID (UUID)"
//	@Param			request	body		SetAvailabilityRequest	true	"Thông tin trạng thái khả dụng"
//	@Success		200		{object}	response.APIResponse{data=ItemResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Failure		500		{object}	response.APIResponse
//	@Router			/catalog/items/{item_id}/availability [patch]
func (s *Slices) handleSetItemAvailability(c echo.Context) error {
	actor, ids, req, err := bindCommand[SetAvailabilityRequest](c, "item_id")
	if err != nil {
		return sendError(c, err)
	}
	return sendCommand(c, actor, s.SetItemAvailability.Handle, SetItemAvailabilityCommand{
		RequestID: req.RequestID,
		ItemID:    ids[0],
		Available: req.Available,
	})
}

// handleSetSizeAvailability godoc
//
//	@Summary		Cập nhật trạng thái khả dụng của kích thước
//	@Description	Bật hoặc tắt trạng thái khả dụng của size món. Yêu cầu quyền catalog.manage_availability.
//	@Tags			Catalog
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			size_id	path		string					true	"Size ID (UUID)"
//	@Param			request	body		SetAvailabilityRequest	true	"Thông tin trạng thái khả dụng"
//	@Success		200		{object}	response.APIResponse{data=SizeResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Failure		500		{object}	response.APIResponse
//	@Router			/catalog/sizes/{size_id}/availability [patch]
func (s *Slices) handleSetSizeAvailability(c echo.Context) error {
	actor, ids, req, err := bindCommand[SetAvailabilityRequest](c, "size_id")
	if err != nil {
		return sendError(c, err)
	}
	return sendCommand(c, actor, s.SetSizeAvailability.Handle, SetSizeAvailabilityCommand{
		RequestID: req.RequestID,
		SizeID:    ids[0],
		Available: req.Available,
	})
}

// handleSetModifierOptionAvailability godoc
//
//	@Summary		Cập nhật trạng thái khả dụng của tùy chọn
//	@Description	Bật hoặc tắt trạng thái khả dụng của tùy chọn modifier. Yêu cầu quyền catalog.manage_availability.
//	@Tags			Catalog
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			option_id	path		string					true	"Option ID (UUID)"
//	@Param			request		body		SetAvailabilityRequest	true	"Thông tin trạng thái khả dụng"
//	@Success		200			{object}	response.APIResponse{data=ModifierOptionResponse}
//	@Failure		400			{object}	response.APIResponse
//	@Failure		401			{object}	response.APIResponse
//	@Failure		403			{object}	response.APIResponse
//	@Failure		404			{object}	response.APIResponse
//	@Failure		409			{object}	response.APIResponse
//	@Failure		500			{object}	response.APIResponse
//	@Router			/catalog/modifier-options/{option_id}/availability [patch]
func (s *Slices) handleSetModifierOptionAvailability(c echo.Context) error {
	actor, ids, req, err := bindCommand[SetAvailabilityRequest](c, "option_id")
	if err != nil {
		return sendError(c, err)
	}
	return sendCommand(c, actor, s.SetModifierOptionAvailability.Handle, SetModifierOptionAvailabilityCommand{
		RequestID: req.RequestID,
		OptionID:  ids[0],
		Available: req.Available,
	})
}

// handleSetAvailabilityBatch godoc
//
//	@Summary		Cập nhật trạng thái khả dụng hàng loạt
//	@Description	Bật hoặc tắt nhiều món, kích cỡ và tùy chọn trong một giao dịch. Thành công toàn bộ hoặc không thay đổi gì. Yêu cầu quyền catalog.manage_availability.
//	@Tags			Catalog
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		SetAvailabilityBatchRequest	true	"Danh sách thay đổi (1 đến 200 mục)"
//	@Success		200		{object}	response.APIResponse{data=AvailabilityBatchResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Failure		500		{object}	response.APIResponse
//	@Router			/catalog/availability/batch [post]
func (s *Slices) handleSetAvailabilityBatch(c echo.Context) error {
	actor, _, req, err := bindCommand[SetAvailabilityBatchRequest](c)
	if err != nil {
		return sendError(c, err)
	}
	return sendCommand(c, actor, s.SetAvailabilityBatch.Handle, SetAvailabilityBatchCommand(req))
}
