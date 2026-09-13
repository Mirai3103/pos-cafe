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
