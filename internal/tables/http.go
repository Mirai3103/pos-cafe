package tables

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

func checkAvailable(available *bool) error {
	if available == nil {
		return fmt.Errorf("%w: available is required", response.ErrInvalid)
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

// handleGetOverview returns every Table with its current occupancy.
//
//	@Summary		Table overview
//	@Description	Lists every Table with the active Service Sessions occupying it
//	@Tags			tables
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	response.APIResponse{data=[]TableOverviewRow}
//	@Failure		401	{object}	response.APIResponse
//	@Failure		403	{object}	response.APIResponse
//	@Router			/tables/overview [get]
func (s *Slices) handleGetOverview(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	rows, err := s.Overview.Handle(c.Request().Context(), actor)
	if err != nil {
		return sendError(c, err)
	}
	return response.OK(c, rows)
}

// handleCreateTable creates a Table.
//
//	@Summary		Create a Table
//	@Description	Creates a Table with a unique name
//	@Tags			tables
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		CreateTableCommand	true	"Table to create"
//	@Success		201		{object}	response.APIResponse{data=TableResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Router			/tables [post]
func (s *Slices) handleCreateTable(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	cmd, err := bindBody[CreateTableCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(cmd.RequestID); err != nil {
		return sendError(c, err)
	}
	status, res, err := s.CreateTable.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
}

// handleRenameTable renames a Table.
//
//	@Summary		Rename a Table
//	@Description	Renames a Table, preserving its identity and history
//	@Tags			tables
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			table_id	path		string						true	"Table ID"
//	@Param			request		body		RenameTableCommand	true	"New name"
//	@Success		200			{object}	response.APIResponse{data=TableResponse}
//	@Failure		400			{object}	response.APIResponse
//	@Failure		401			{object}	response.APIResponse
//	@Failure		403			{object}	response.APIResponse
//	@Failure		404			{object}	response.APIResponse
//	@Failure		409			{object}	response.APIResponse
//	@Router			/tables/{table_id}/name [patch]
func (s *Slices) handleRenameTable(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	tableID, err := parseUUIDParam(c, "table_id")
	if err != nil {
		return sendError(c, err)
	}
	cmd, err := bindBody[RenameTableCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(cmd.RequestID); err != nil {
		return sendError(c, err)
	}
	cmd.TableID = tableID

	status, res, err := s.RenameTable.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
}

// handleSetTableAvailability changes a Table's Availability.
//
//	@Summary		Set Table Availability
//	@Description	Sets whether a Table is eligible for new work. Setting the current value again is a successful no-op.
//	@Tags			tables
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			table_id	path		string									true	"Table ID"
//	@Param			request		body		SetTableAvailabilityCommand	true	"Availability to set"
//	@Success		200			{object}	response.APIResponse{data=TableResponse}
//	@Failure		400			{object}	response.APIResponse
//	@Failure		401			{object}	response.APIResponse
//	@Failure		403			{object}	response.APIResponse
//	@Failure		404			{object}	response.APIResponse
//	@Router			/tables/{table_id}/availability [patch]
func (s *Slices) handleSetTableAvailability(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	tableID, err := parseUUIDParam(c, "table_id")
	if err != nil {
		return sendError(c, err)
	}
	cmd, err := bindBody[SetTableAvailabilityCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(cmd.RequestID); err != nil {
		return sendError(c, err)
	}
	if err := checkAvailable(cmd.Available); err != nil {
		return sendError(c, err)
	}
	cmd.TableID = tableID

	status, res, err := s.SetTableAvailability.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
}
