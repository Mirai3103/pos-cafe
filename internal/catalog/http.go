package catalog

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

func getActor(c echo.Context) (Actor, error) {
	claims := auth.GetStaff(c)
	if claims == nil {
		return Actor{}, fmt.Errorf("%w: unauthorized", response.ErrUnauthorized)
	}
	return Actor{
		StaffID:   claims.StaffID,
		SessionID: claims.SessionID,
	}, nil
}

func parseUUIDParam(c echo.Context, name string) (uuid.UUID, error) {
	val := c.Param(name)
	id, err := uuid.Parse(val)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: invalid %s UUID: %s", response.ErrInvalid, name, val)
	}
	return id, nil
}

func bindBody[T any](c echo.Context) (T, error) {
	var body T
	if err := c.Bind(&body); err != nil {
		return body, fmt.Errorf("%w: invalid request body: %s", response.ErrInvalid, err.Error())
	}
	return body, nil
}

func checkRequestID(id uuid.UUID) error {
	if id == uuid.Nil {
		return fmt.Errorf("%w: request_id is required", response.ErrInvalid)
	}
	return nil
}

func sendResult[T any](c echo.Context, status int, data T) error {
	if status == http.StatusCreated {
		return response.Created(c, data)
	}
	return response.OK(c, data)
}

func sendError(c echo.Context, err error) error {
	return response.Error(c, MapHTTPError(err))
}

// === Categories ===

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
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	cmd, err := bindBody[CreateCategoryCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(cmd.RequestID); err != nil {
		return sendError(c, err)
	}

	status, res, err := s.CreateCategory.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
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
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	catID, err := parseUUIDParam(c, "category_id")
	if err != nil {
		return sendError(c, err)
	}
	req, err := bindBody[RenameRequest](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(req.RequestID); err != nil {
		return sendError(c, err)
	}

	cmd := RenameCategoryCommand{
		RequestID:  req.RequestID,
		CategoryID: catID,
		Name:       req.Name,
	}
	status, res, err := s.RenameCategory.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
}

// === Items ===

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
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	cmd, err := bindBody[CreateItemCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(cmd.RequestID); err != nil {
		return sendError(c, err)
	}
	if cmd.CategoryID == uuid.Nil {
		return sendError(c, fmt.Errorf("%w: category_id is required", response.ErrInvalid))
	}

	status, res, err := s.CreateItem.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
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
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	itemID, err := parseUUIDParam(c, "item_id")
	if err != nil {
		return sendError(c, err)
	}
	req, err := bindBody[RenameRequest](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(req.RequestID); err != nil {
		return sendError(c, err)
	}

	cmd := RenameItemCommand{
		RequestID: req.RequestID,
		ItemID:    itemID,
		Name:      req.Name,
	}
	status, res, err := s.RenameItem.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
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
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	itemID, err := parseUUIDParam(c, "item_id")
	if err != nil {
		return sendError(c, err)
	}
	req, err := bindBody[RepriceRequest](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(req.RequestID); err != nil {
		return sendError(c, err)
	}

	cmd := RepriceItemCommand{
		RequestID:  req.RequestID,
		ItemID:     itemID,
		PriceVND:   req.PriceVND,
		ManagerPIN: req.ManagerPIN,
	}
	status, res, err := s.RepriceItem.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
}

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
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	itemID, err := parseUUIDParam(c, "item_id")
	if err != nil {
		return sendError(c, err)
	}
	req, err := bindBody[SetAvailabilityRequest](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(req.RequestID); err != nil {
		return sendError(c, err)
	}

	cmd := SetItemAvailabilityCommand{
		RequestID: req.RequestID,
		ItemID:    itemID,
		Available: req.Available,
	}
	status, res, err := s.SetItemAvailability.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
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
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	itemID, err := parseUUIDParam(c, "item_id")
	if err != nil {
		return sendError(c, err)
	}
	req, err := bindBody[RetireRequest](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(req.RequestID); err != nil {
		return sendError(c, err)
	}

	cmd := RetireItemCommand{
		RequestID: req.RequestID,
		ItemID:    itemID,
		Reason:    req.Reason,
		Note:      req.Note,
	}
	status, res, err := s.RetireItem.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
}

// === Sizes ===

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
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	sizeID, err := parseUUIDParam(c, "size_id")
	if err != nil {
		return sendError(c, err)
	}
	req, err := bindBody[RenameRequest](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(req.RequestID); err != nil {
		return sendError(c, err)
	}

	cmd := RenameSizeCommand{
		RequestID: req.RequestID,
		SizeID:    sizeID,
		Name:      req.Name,
	}
	status, res, err := s.RenameSize.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
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
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	sizeID, err := parseUUIDParam(c, "size_id")
	if err != nil {
		return sendError(c, err)
	}
	req, err := bindBody[RepriceRequest](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(req.RequestID); err != nil {
		return sendError(c, err)
	}

	cmd := RepriceSizeCommand{
		RequestID:  req.RequestID,
		SizeID:     sizeID,
		PriceVND:   req.PriceVND,
		ManagerPIN: req.ManagerPIN,
	}
	status, res, err := s.RepriceSize.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
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
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	sizeID, err := parseUUIDParam(c, "size_id")
	if err != nil {
		return sendError(c, err)
	}
	req, err := bindBody[SetAvailabilityRequest](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(req.RequestID); err != nil {
		return sendError(c, err)
	}

	cmd := SetSizeAvailabilityCommand{
		RequestID: req.RequestID,
		SizeID:    sizeID,
		Available: req.Available,
	}
	status, res, err := s.SetSizeAvailability.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
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
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	sizeID, err := parseUUIDParam(c, "size_id")
	if err != nil {
		return sendError(c, err)
	}
	req, err := bindBody[RetireRequest](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(req.RequestID); err != nil {
		return sendError(c, err)
	}

	cmd := RetireSizeCommand{
		RequestID: req.RequestID,
		SizeID:    sizeID,
		Reason:    req.Reason,
		Note:      req.Note,
	}
	status, res, err := s.RetireSize.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
}

// === Modifier Groups ===

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
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	cmd, err := bindBody[CreateModifierGroupCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(cmd.RequestID); err != nil {
		return sendError(c, err)
	}

	status, res, err := s.CreateModifierGroup.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
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
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	groupID, err := parseUUIDParam(c, "group_id")
	if err != nil {
		return sendError(c, err)
	}
	req, err := bindBody[RenameRequest](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(req.RequestID); err != nil {
		return sendError(c, err)
	}

	cmd := RenameModifierGroupCommand{
		RequestID: req.RequestID,
		GroupID:   groupID,
		Name:      req.Name,
	}
	status, res, err := s.RenameModifierGroup.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
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
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	groupID, err := parseUUIDParam(c, "group_id")
	if err != nil {
		return sendError(c, err)
	}
	req, err := bindBody[SetModifierGroupDefaultsRequest](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(req.RequestID); err != nil {
		return sendError(c, err)
	}

	cmd := SetModifierGroupDefaultsCommand{
		RequestID: req.RequestID,
		GroupID:   groupID,
		OptionIDs: req.OptionIDs,
	}
	status, res, err := s.SetModifierGroupDefaults.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
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
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	groupID, err := parseUUIDParam(c, "group_id")
	if err != nil {
		return sendError(c, err)
	}
	req, err := bindBody[RetireRequest](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(req.RequestID); err != nil {
		return sendError(c, err)
	}

	cmd := RetireModifierGroupCommand{
		RequestID: req.RequestID,
		GroupID:   groupID,
		Reason:    req.Reason,
		Note:      req.Note,
	}
	status, res, err := s.RetireModifierGroup.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
}

// === Modifier Options ===

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
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	optionID, err := parseUUIDParam(c, "option_id")
	if err != nil {
		return sendError(c, err)
	}
	req, err := bindBody[RenameRequest](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(req.RequestID); err != nil {
		return sendError(c, err)
	}

	cmd := RenameModifierOptionCommand{
		RequestID: req.RequestID,
		OptionID:  optionID,
		Name:      req.Name,
	}
	status, res, err := s.RenameModifierOption.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
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
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	optionID, err := parseUUIDParam(c, "option_id")
	if err != nil {
		return sendError(c, err)
	}
	req, err := bindBody[RepriceModifierOptionRequest](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(req.RequestID); err != nil {
		return sendError(c, err)
	}

	cmd := RepriceModifierOptionCommand{
		RequestID:    req.RequestID,
		OptionID:     optionID,
		SurchargeVND: req.SurchargeVND,
		ManagerPIN:   req.ManagerPIN,
	}
	status, res, err := s.RepriceModifierOption.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
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
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	optionID, err := parseUUIDParam(c, "option_id")
	if err != nil {
		return sendError(c, err)
	}
	req, err := bindBody[SetAvailabilityRequest](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(req.RequestID); err != nil {
		return sendError(c, err)
	}

	cmd := SetModifierOptionAvailabilityCommand{
		RequestID: req.RequestID,
		OptionID:  optionID,
		Available: req.Available,
	}
	status, res, err := s.SetModifierOptionAvailability.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
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
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	optionID, err := parseUUIDParam(c, "option_id")
	if err != nil {
		return sendError(c, err)
	}
	req, err := bindBody[RetireRequest](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(req.RequestID); err != nil {
		return sendError(c, err)
	}

	cmd := RetireModifierOptionCommand{
		RequestID: req.RequestID,
		OptionID:  optionID,
		Reason:    req.Reason,
		Note:      req.Note,
	}
	status, res, err := s.RetireModifierOption.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
}

// === Assignments ===

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
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	itemID, err := parseUUIDParam(c, "item_id")
	if err != nil {
		return sendError(c, err)
	}
	groupID, err := parseUUIDParam(c, "group_id")
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

	cmd := AttachItemModifierGroupCommand{
		RequestID:       req.RequestID,
		ItemID:          itemID,
		ModifierGroupID: groupID,
	}
	status, res, err := s.AttachItemModifierGroup.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
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
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	catID, err := parseUUIDParam(c, "category_id")
	if err != nil {
		return sendError(c, err)
	}
	groupID, err := parseUUIDParam(c, "group_id")
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

	cmd := AttachCategoryModifierGroupCommand{
		RequestID:       req.RequestID,
		CategoryID:      catID,
		ModifierGroupID: groupID,
	}
	status, res, err := s.AttachCategoryModifierGroup.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
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
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	itemID, err := parseUUIDParam(c, "item_id")
	if err != nil {
		return sendError(c, err)
	}
	groupID, err := parseUUIDParam(c, "group_id")
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

	cmd := ExcludeInheritedModifierGroupCommand{
		RequestID:       req.RequestID,
		ItemID:          itemID,
		ModifierGroupID: groupID,
	}
	status, res, err := s.ExcludeItemInheritedModifierGroup.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
}

// === Reads ===

// handleGetSellableMenu godoc
//
//	@Summary		Lấy thực đơn bán hàng (Sellable Menu)
//	@Description	Trả về projection thực đơn chỉ bao gồm các món và kích thước còn hàng, sẵn sàng để bán. Yêu cầu quyền catalog.view_prices.
//	@Tags			Catalog
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	response.APIResponse{data=SellableMenuResponse}
//	@Failure		400	{object}	response.APIResponse
//	@Failure		401	{object}	response.APIResponse
//	@Failure		403	{object}	response.APIResponse
//	@Failure		404	{object}	response.APIResponse
//	@Failure		409	{object}	response.APIResponse
//	@Failure		500	{object}	response.APIResponse
//	@Router			/catalog/menu/sellable [get]
func (s *Slices) handleGetSellableMenu(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	res, err := s.SellableMenu.Handle(c.Request().Context(), actor)
	if err != nil {
		return sendError(c, err)
	}
	return response.OK(c, res)
}

// handleGetManagementMenu godoc
//
//	@Summary		Lấy thực đơn quản lý (Management Menu)
//	@Description	Trả về toàn bộ danh mục, món, kích thước (bao gồm cả món đã ngừng bán hoặc hết hàng) phục vụ quản trị. Yêu cầu quyền catalog.view_prices.
//	@Tags			Catalog
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	response.APIResponse{data=ManagementMenuResponse}
//	@Failure		400	{object}	response.APIResponse
//	@Failure		401	{object}	response.APIResponse
//	@Failure		403	{object}	response.APIResponse
//	@Failure		404	{object}	response.APIResponse
//	@Failure		409	{object}	response.APIResponse
//	@Failure		500	{object}	response.APIResponse
//	@Router			/catalog/menu/manage [get]
func (s *Slices) handleGetManagementMenu(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	res, err := s.ManagementMenu.Handle(c.Request().Context(), actor)
	if err != nil {
		return sendError(c, err)
	}
	return response.OK(c, res)
}

// handleGetAvailabilityMenu godoc
//
//	@Summary		Lấy thực đơn trạng thái khả dụng (Availability Menu)
//	@Description	Trả về projection nhẹ danh mục, món, size, tùy chọn và cờ khả dụng để thu ngân/pha chế bật tắt nhanh. Yêu cầu quyền catalog.manage_availability.
//	@Tags			Catalog
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	response.APIResponse{data=AvailabilityMenuResponse}
//	@Failure		400	{object}	response.APIResponse
//	@Failure		401	{object}	response.APIResponse
//	@Failure		403	{object}	response.APIResponse
//	@Failure		404	{object}	response.APIResponse
//	@Failure		409	{object}	response.APIResponse
//	@Failure		500	{object}	response.APIResponse
//	@Router			/catalog/menu/availability [get]
func (s *Slices) handleGetAvailabilityMenu(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	res, err := s.AvailabilityMenu.Handle(c.Request().Context(), actor)
	if err != nil {
		return sendError(c, err)
	}
	return response.OK(c, res)
}

// handleGetModifierGroups godoc
//
//	@Summary		Danh sách nhóm tùy chọn / topping quản lý
//	@Description	Trả về danh sách tất cả các nhóm modifier cùng các tùy chọn và cấu hình mặc định. Yêu cầu quyền catalog.view_prices.
//	@Tags			Catalog
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	response.APIResponse{data=[]ModifierGroupManagementResponse}
//	@Failure		400	{object}	response.APIResponse
//	@Failure		401	{object}	response.APIResponse
//	@Failure		403	{object}	response.APIResponse
//	@Failure		404	{object}	response.APIResponse
//	@Failure		409	{object}	response.APIResponse
//	@Failure		500	{object}	response.APIResponse
//	@Router			/catalog/modifier-groups [get]
func (s *Slices) handleGetModifierGroups(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	res, err := s.ModifierGroups.Handle(c.Request().Context(), actor)
	if err != nil {
		return sendError(c, err)
	}
	return response.OK(c, res)
}

// handleGetAuditEvents godoc
//
//	@Summary		Danh sách nhật ký kiểm toán catalog
//	@Description	Trả về danh sách sự kiện audit của catalog theo thứ tự mới nhất trước. Yêu cầu quyền audit.inspect.
//	@Tags			Catalog
//	@Produce		json
//	@Security		BearerAuth
//	@Param			limit	query		int	false	"Số lượng bản ghi tối đa (mặc định 50, tối đa 100)"
//	@Success		200		{object}	response.APIResponse{data=[]AuditEventResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Failure		500		{object}	response.APIResponse
//	@Router			/catalog/audit-events [get]
func (s *Slices) handleGetAuditEvents(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	limit := int32(50)
	if lStr := c.QueryParam("limit"); lStr != "" {
		if parsed, err := strconv.ParseInt(lStr, 10, 32); err == nil && parsed > 0 {
			limit = int32(parsed)
		}
	}
	res, err := s.AuditEvents.Handle(c.Request().Context(), actor, limit)
	if err != nil {
		return sendError(c, err)
	}
	return response.OK(c, res)
}
