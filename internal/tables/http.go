package tables

import (
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/platform/httpx"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/labstack/echo/v4"
)

func checkAvailable(available *bool) error {
	if available == nil {
		return fmt.Errorf("%w: available is required", response.ErrInvalid)
	}
	return nil
}

// sendError writes err through the Tables error mapping.
func sendError(c echo.Context, err error) error {
	return httpx.SendError(c, err, MapHTTPError)
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
	actor, err := httpx.Actor(c)
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
	actor, err := httpx.Actor(c)
	if err != nil {
		return sendError(c, err)
	}
	cmd, err := httpx.BindBody[CreateTableCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := httpx.RequireRequestID(cmd.RequestID); err != nil {
		return sendError(c, err)
	}
	status, res, err := s.CreateTable.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return httpx.SendResult(c, status, res)
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
	actor, err := httpx.Actor(c)
	if err != nil {
		return sendError(c, err)
	}
	tableID, err := httpx.UUIDParam(c, "table_id")
	if err != nil {
		return sendError(c, err)
	}
	cmd, err := httpx.BindBody[RenameTableCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := httpx.RequireRequestID(cmd.RequestID); err != nil {
		return sendError(c, err)
	}
	cmd.TableID = tableID

	status, res, err := s.RenameTable.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return httpx.SendResult(c, status, res)
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
	actor, err := httpx.Actor(c)
	if err != nil {
		return sendError(c, err)
	}
	tableID, err := httpx.UUIDParam(c, "table_id")
	if err != nil {
		return sendError(c, err)
	}
	cmd, err := httpx.BindBody[SetTableAvailabilityCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := httpx.RequireRequestID(cmd.RequestID); err != nil {
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
	return httpx.SendResult(c, status, res)
}
