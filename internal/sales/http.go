package sales

import (
	"fmt"
	"net/http"

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
	return Actor{StaffID: claims.StaffID, SessionID: claims.SessionID}, nil
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

// handleGetServiceSession returns one Service Session.
//
//	@Summary		Get a Service Session
//	@Description	Returns one Service Session with its current Tables and Order Draft. checks is filled by Phase 5B, orders and preparation_units by Phase 5D; each is an empty array until then.
//	@Tags			sales
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string	true	"Service Session ID"
//	@Success		200	{object}	response.APIResponse{data=ServiceSessionResponse}
//	@Failure		401	{object}	response.APIResponse
//	@Failure		403	{object}	response.APIResponse
//	@Failure		404	{object}	response.APIResponse
//	@Router			/sales/service-sessions/{id} [get]
func (s *Slices) handleGetServiceSession(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	sessionID, err := parseUUIDParam(c, "id")
	if err != nil {
		return sendError(c, err)
	}
	result, err := s.GetServiceSession.Handle(c.Request().Context(), actor, sessionID)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, http.StatusOK, result)
}

// handleStartTakeaway opens a Takeaway Service Session.
//
//	@Summary		Open a Takeaway Service Session
//	@Description	Opens an anonymous Takeaway Service Session with an empty editable Order Draft. Requires an open Sales Shift. The Service Number is sequential within that Shift (ADR-011).
//	@Tags			sales
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		StartTakeawaySessionCommand	true	"Request"
//	@Success		201		{object}	response.APIResponse{data=ServiceSessionResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Router			/sales/service-sessions/takeaway [post]
func (s *Slices) handleStartTakeaway(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	cmd, err := bindBody[StartTakeawaySessionCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(cmd.RequestID); err != nil {
		return sendError(c, err)
	}
	status, result, err := s.StartTakeaway.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, result)
}

// handleStartDineIn opens a Dine-in Service Session.
//
//	@Summary		Open a Dine-in Service Session
//	@Description	Opens a Dine-in Service Session assigned to one or more Tables. A Table may carry more than one active Service Session. Requires an open Sales Shift.
//	@Tags			sales
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		StartDineInSessionCommand	true	"Request"
//	@Success		201		{object}	response.APIResponse{data=ServiceSessionResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Router			/sales/service-sessions/dine-in [post]
func (s *Slices) handleStartDineIn(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	cmd, err := bindBody[StartDineInSessionCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(cmd.RequestID); err != nil {
		return sendError(c, err)
	}
	status, result, err := s.StartDineIn.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, result)
}

// handleSetSessionTables sets a Service Session's Tables.
//
//	@Summary		Set a Service Session's Tables
//	@Description	Replaces a Dine-in Session's current Table set. Tables no longer listed are released, preserving history. An empty table_ids releases every Table and is permitted. Rejected for a Takeaway Session.
//	@Tags			sales
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string					true	"Service Session ID"
//	@Param			request	body		SetSessionTablesCommand	true	"Request"
//	@Success		200		{object}	response.APIResponse{data=ServiceSessionResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Router			/sales/service-sessions/{id}/tables [put]
func (s *Slices) handleSetSessionTables(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	cmd, err := bindBody[SetSessionTablesCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(cmd.RequestID); err != nil {
		return sendError(c, err)
	}
	// ServiceSessionID is json:"-": it comes from the path, never the body.
	sessionID, err := parseUUIDParam(c, "id")
	if err != nil {
		return sendError(c, err)
	}
	cmd.ServiceSessionID = sessionID
	status, result, err := s.SetSessionTables.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, result)
}

// handleAddDraftItem adds one unit of a configured Menu Item to the draft.
//
//	@Summary		Add an Order Draft item
//	@Description	Adds one unit of a configured Menu Item, merging into an existing line of the same composition. Omit modifier_option_ids to apply the menu's default options; send an empty array to apply none. The draft accepts an incomplete configuration; completeness is checked at Commit in Phase 5B.
//	@Tags			sales
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string				true	"Service Session ID"
//	@Param			request	body		AddDraftItemCommand	true	"Request"
//	@Success		200		{object}	response.APIResponse{data=ServiceSessionResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Router			/sales/service-sessions/{id}/draft/items [post]
func (s *Slices) handleAddDraftItem(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	cmd, err := bindBody[AddDraftItemCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(cmd.RequestID); err != nil {
		return sendError(c, err)
	}
	// ServiceSessionID is json:"-": it comes from the path, never the body.
	sessionID, err := parseUUIDParam(c, "id")
	if err != nil {
		return sendError(c, err)
	}
	cmd.ServiceSessionID = sessionID
	status, result, err := s.AddDraftItem.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, result)
}
