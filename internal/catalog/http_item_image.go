package catalog

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/Mirai3103/pos-cafe/internal/platform/httpx"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

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
	actor, err := httpx.Actor(c)
	if err != nil {
		return sendError(c, err)
	}
	itemID, err := httpx.UUIDParam(c, "item_id")
	if err != nil {
		return sendError(c, err)
	}
	requestID, data, err := readImageUpload(c)
	if err != nil {
		return sendError(c, err)
	}
	return sendCommand(c, actor, s.SetItemImage.Handle, SetItemImageCommand{RequestID: requestID, ItemID: itemID, Data: data})
}

// readImageUpload reads the request_id and file fields of a multipart image
// upload. The body is capped at MaxImageUploadBody, and the file is read one
// byte past MaxImageBytes so an oversized file is rejected rather than
// silently truncated.
func readImageUpload(c echo.Context) (uuid.UUID, []byte, error) {
	req := c.Request()
	req.Body = http.MaxBytesReader(c.Response(), req.Body, MaxImageUploadBody)
	if err := req.ParseMultipartForm(MaxImageUploadBody); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return uuid.Nil, nil, fmt.Errorf("%w: upload exceeds %d bytes", ErrImageTooLarge, MaxImageBytes)
		}
		return uuid.Nil, nil, fmt.Errorf("%w: invalid multipart body: %s", response.ErrInvalid, err.Error())
	}
	requestID, err := uuid.Parse(req.FormValue("request_id"))
	if err != nil || requestID == uuid.Nil {
		return uuid.Nil, nil, fmt.Errorf("%w: request_id is required", response.ErrInvalid)
	}
	fh, err := c.FormFile("file")
	if err != nil {
		return uuid.Nil, nil, fmt.Errorf("%w: file is required", response.ErrInvalid)
	}
	f, err := fh.Open()
	if err != nil {
		return uuid.Nil, nil, fmt.Errorf("%w: cannot read file: %s", response.ErrInvalid, err.Error())
	}
	defer f.Close() //nolint:errcheck
	data, err := io.ReadAll(io.LimitReader(f, MaxImageBytes+1))
	if err != nil {
		return uuid.Nil, nil, fmt.Errorf("%w: cannot read file: %s", response.ErrInvalid, err.Error())
	}
	return requestID, data, nil
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
	actor, ids, req, err := bindCommand[MutationRequest](c, "item_id")
	if err != nil {
		return sendError(c, err)
	}
	return sendCommand(c, actor, s.ClearItemImage.Handle, ClearItemImageCommand{RequestID: req.RequestID, ItemID: ids[0]})
}
