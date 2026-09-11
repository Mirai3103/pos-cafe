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
