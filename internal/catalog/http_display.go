package catalog

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
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

// handleSetItemImage godoc
//
//	@Summary		Tải ảnh món
//	@Description	Tải ảnh JPEG, PNG hoặc WebP (tối đa 1 MB) cho món. Trình duyệt nên thu nhỏ ảnh trước khi gửi. Yêu cầu quyền catalog.administer_structure.
//	@Tags			Catalog
//	@Accept			multipart/form-data
//	@Produce		json
//	@Security		BearerAuth
//	@Param			item_id		path		string	true	"Item ID (UUID)"
//	@Param			request_id	formData	string	true	"Request ID (UUID)"
//	@Param			file		formData	file	true	"Ảnh món"
//	@Success		200			{object}	response.APIResponse{data=ItemImageResponse}
//	@Failure		400			{object}	response.APIResponse
//	@Failure		401			{object}	response.APIResponse
//	@Failure		403			{object}	response.APIResponse
//	@Failure		404			{object}	response.APIResponse
//	@Failure		409			{object}	response.APIResponse
//	@Failure		413			{object}	response.APIResponse
//	@Failure		500			{object}	response.APIResponse
//	@Router			/catalog/items/{item_id}/image [put]
func (s *Slices) handleSetItemImage(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	itemID, err := parseUUIDParam(c, "item_id")
	if err != nil {
		return sendError(c, err)
	}

	req := c.Request()
	req.Body = http.MaxBytesReader(c.Response(), req.Body, MaxImageUploadBody)
	if err := req.ParseMultipartForm(MaxImageUploadBody); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return sendError(c, fmt.Errorf("%w: upload exceeds %d bytes", ErrImageTooLarge, MaxImageBytes))
		}
		return sendError(c, fmt.Errorf("%w: invalid multipart body: %s", response.ErrInvalid, err.Error()))
	}
	requestID, err := uuid.Parse(req.FormValue("request_id"))
	if err != nil || requestID == uuid.Nil {
		return sendError(c, fmt.Errorf("%w: request_id is required", response.ErrInvalid))
	}
	fh, err := c.FormFile("file")
	if err != nil {
		return sendError(c, fmt.Errorf("%w: file is required", response.ErrInvalid))
	}
	f, err := fh.Open()
	if err != nil {
		return sendError(c, fmt.Errorf("%w: cannot read file: %s", response.ErrInvalid, err.Error()))
	}
	defer f.Close() //nolint:errcheck
	data, err := io.ReadAll(io.LimitReader(f, MaxImageBytes+1))
	if err != nil {
		return sendError(c, fmt.Errorf("%w: cannot read file: %s", response.ErrInvalid, err.Error()))
	}

	status, res, err := s.SetItemImage.Handle(req.Context(), actor, SetItemImageCommand{RequestID: requestID, ItemID: itemID, Data: data})
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
}

// handleClearItemImage godoc
//
//	@Summary		Gỡ ảnh món
//	@Description	Gỡ ảnh khỏi món. File ảnh vẫn được giữ trên đĩa. Yêu cầu quyền catalog.administer_structure.
//	@Tags			Catalog
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			item_id	path		string			true	"Item ID (UUID)"
//	@Param			request	body		MutationRequest	true	"Request ID"
//	@Success		200		{object}	response.APIResponse{data=ItemImageResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Failure		500		{object}	response.APIResponse
//	@Router			/catalog/items/{item_id}/image [delete]
func (s *Slices) handleClearItemImage(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	itemID, err := parseUUIDParam(c, "item_id")
	if err != nil {
		return sendError(c, err)
	}
	req, err := bindBody[MutationRequest](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(req.RequestID); err != nil {
		return sendError(c, err)
	}
	status, res, err := s.ClearItemImage.Handle(c.Request().Context(), actor, ClearItemImageCommand{RequestID: req.RequestID, ItemID: itemID})
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
}
