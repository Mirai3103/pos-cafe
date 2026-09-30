package catalog

import (
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/labstack/echo/v4"
)

// handleCreateItem godoc
//
//	@Summary		Tạo món thực đơn
//	@Description	Tạo món có giá trực tiếp hoặc món có các kích thước (size). Yêu cầu quyền catalog.administer_structure và catalog.change_price.
//	@Tags			Catalog
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		CreateItemCommand	true	"Thông tin tạo món"
//	@Success		201		{object}	response.APIResponse{data=ItemResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Failure		500		{object}	response.APIResponse
//	@Router			/catalog/items [post]
func (s *Slices) handleCreateItem(c echo.Context) error {
	actor, _, req, err := bindCommand[CreateItemCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	return sendCommand(c, actor, s.CreateItem.Handle, req)
}

// handleRenameItem godoc
//
//	@Summary		Đổi tên món thực đơn
//	@Description	Đổi tên món hiện có trong thực đơn. Yêu cầu quyền catalog.administer_structure.
//	@Tags			Catalog
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			item_id	path		string			true	"Item ID (UUID)"
//	@Param			request	body		RenameRequest	true	"Thông tin đổi tên món"
//	@Success		200		{object}	response.APIResponse{data=ItemResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Failure		500		{object}	response.APIResponse
//	@Router			/catalog/items/{item_id}/name [patch]
func (s *Slices) handleRenameItem(c echo.Context) error {
	actor, ids, req, err := bindCommand[RenameRequest](c, "item_id")
	if err != nil {
		return sendError(c, err)
	}
	return sendCommand(c, actor, s.RenameItem.Handle, RenameItemCommand{
		RequestID: req.RequestID,
		ItemID:    ids[0],
		Name:      req.Name,
	})
}

// handleRepriceItem godoc
//
//	@Summary		Cập nhật giá món trực tiếp
//	@Description	Thay đổi giá của món bán trực tiếp. Yêu cầu quyền catalog.administer_structure, catalog.change_price và PIN quản lý.
//	@Tags			Catalog
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			item_id	path		string			true	"Item ID (UUID)"
//	@Param			request	body		RepriceRequest	true	"Thông tin đổi giá món"
//	@Success		200		{object}	response.APIResponse{data=ItemResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Failure		500		{object}	response.APIResponse
//	@Router			/catalog/items/{item_id}/price [patch]
func (s *Slices) handleRepriceItem(c echo.Context) error {
	actor, ids, req, err := bindCommand[RepriceRequest](c, "item_id")
	if err != nil {
		return sendError(c, err)
	}
	return sendCommand(c, actor, s.RepriceItem.Handle, RepriceItemCommand{
		RequestID:  req.RequestID,
		ItemID:     ids[0],
		PriceVND:   req.PriceVND,
		ManagerPIN: req.ManagerPIN,
	})
}

// handleRetireItem godoc
//
//	@Summary		Ngừng kinh doanh món thực đơn
//	@Description	Đánh dấu ngừng kinh doanh vĩnh viễn món thực đơn. Yêu cầu quyền catalog.administer_structure.
//	@Tags			Catalog
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			item_id	path		string			true	"Item ID (UUID)"
//	@Param			request	body		RetireRequest	true	"Lý do và ghi chú ngừng kinh doanh"
//	@Success		200		{object}	response.APIResponse{data=ItemResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Failure		500		{object}	response.APIResponse
//	@Router			/catalog/items/{item_id}/retirement [post]
func (s *Slices) handleRetireItem(c echo.Context) error {
	actor, ids, req, err := bindCommand[RetireRequest](c, "item_id")
	if err != nil {
		return sendError(c, err)
	}
	return sendCommand(c, actor, s.RetireItem.Handle, RetireItemCommand{
		RequestID: req.RequestID,
		ItemID:    ids[0],
		Reason:    req.Reason,
		Note:      req.Note,
	})
}

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
	actor, ids, req, err := bindCommand[SetItemDetailsRequest](c, "item_id")
	if err != nil {
		return sendError(c, err)
	}
	if !req.Code.Present || !req.Badge.Present || !req.Description.Present {
		return sendError(c, fmt.Errorf("%w: code, badge, and description are all required (null clears)", response.ErrInvalid))
	}
	return sendCommand(c, actor, s.SetItemDetails.Handle, SetItemDetailsCommand{
		RequestID: req.RequestID, ItemID: ids[0],
		Code: req.Code.Value, Badge: req.Badge.Value, Description: req.Description.Value,
	})
}

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
	actor, ids, req, err := bindCommand[MoveItemCategoryRequest](c, "item_id")
	if err != nil {
		return sendError(c, err)
	}
	return sendCommand(c, actor, s.MoveItemCategory.Handle, MoveItemCategoryCommand{
		RequestID: req.RequestID, ItemID: ids[0], CategoryID: req.CategoryID,
	})
}
