package preparation

import (
	"errors"
	"fmt"
	"log/slog"
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

// sendError writes the error response for the Preparation handlers.
//
// Handoff from Task 7's review: preparation's ErrorResponse returns
// (status, body) rather than writing, so it can neither log nor see the
// shared validation sentinels — both of which sales gets from response.Error
// via its own mapper. This helper restores them:
//
//   - The sentinels the HTTP helpers raise (parseUUIDParam, bindBody, and
//     checkRequestID wrap response.ErrInvalid; getActor wraps
//     response.ErrUnauthorized) are mapped onto their proper statuses here
//     instead of falling into the generic 500. The bodies mirror sales':
//     response.ErrInvalid carries the INVALID_INPUT code sales' MapHTTPError
//     assigns it, and response.ErrUnauthorized carries the UNAUTHORIZED code
//     response.Error writes for it.
//   - Any error ErrorResponse does not recognize is slog-logged server-side
//     before the generic 500 body is written, exactly as response.Error's
//     fallback does for sales. ErrInvalidStoredResult keeps its own
//     INVALID_STORED_RESULT row and, like sales, is not logged here.
func sendError(c echo.Context, err error) error {
	switch {
	case errors.Is(err, response.ErrInvalid):
		return c.JSON(http.StatusBadRequest, response.APIResponse{
			Success: false,
			Error: &response.APIError{
				Code:    "INVALID_INPUT",
				Message: err.Error(),
			},
		})
	case errors.Is(err, response.ErrUnauthorized):
		return c.JSON(http.StatusUnauthorized, response.APIResponse{
			Success: false,
			Error: &response.APIError{
				Code:    "UNAUTHORIZED",
				Message: err.Error(),
			},
		})
	}

	status, body := ErrorResponse(err)
	if status == http.StatusInternalServerError &&
		body.Error != nil && body.Error.Code == "INTERNAL_ERROR" {
		slog.Error("internal server error", "error", err, "path", c.Path())
	}
	return c.JSON(status, body)
}

// handleActiveQueue godoc
//
//	@Summary		Read the active Preparation Queue
//	@Description	Returns active units in FIFO order with PostgreSQL observed time and current Table names. The projection contains no financial data.
//	@Tags			preparation
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	response.APIResponse{data=QueueResponse}
//	@Failure		401	{object}	response.APIResponse
//	@Failure		403	{object}	response.APIResponse
//	@Failure		500	{object}	response.APIResponse
//	@Router			/preparation/queue [get]
func (s *Slices) handleActiveQueue(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	result, err := s.ActiveQueue.Handle(c.Request().Context(), actor)
	if err != nil {
		return sendError(c, err)
	}
	return response.OK(c, result)
}

// handleBulkAdvance godoc
//
//	@Summary		Advance selected Preparation Units
//	@Description	Advances 1 through 50 selected units. Missing or stale units are per-unit failures; unexpected failures roll back the request.
//	@Tags			preparation
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			body	body		BulkAdvanceCommand	true	"Bulk advance request"
//	@Success		200		{object}	response.APIResponse{data=BulkAdvanceResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Failure		500		{object}	response.APIResponse
//	@Router			/preparation/units/advance-many [post]
func (s *Slices) handleBulkAdvance(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	body, err := bindBody[BulkAdvanceCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	status, result, err := s.BulkAdvance.Handle(c.Request().Context(), actor, body)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, result)
}

// handleAdvanceUnit godoc
//
//	@Summary		Advance a Preparation Unit
//	@Description	Moves one unit along the linear chain QUEUED -> IN_PREPARATION -> READY -> FULFILLED. The target state is explicit, so two baristas acting on a stale display get a conflict rather than a silent double advance. Cancellation, Waste, Remake, and State Correction are Phase 6B/6C.
//	@Tags			preparation
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			unit_id	path		string				true	"Preparation Unit ID"
//	@Param			body	body		AdvanceUnitCommand	true	"Advance request"
//	@Success		200		{object}	response.APIResponse{data=UnitResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Failure		500		{object}	response.APIResponse
//	@Router			/preparation/units/{unit_id}/advance [post]
func (s *Slices) handleAdvanceUnit(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	unitID, err := parseUUIDParam(c, "unit_id")
	if err != nil {
		return sendError(c, err)
	}
	body, err := bindBody[AdvanceUnitCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(body.RequestID); err != nil {
		return sendError(c, err)
	}
	body.UnitID = unitID

	status, result, err := s.AdvanceUnit.Handle(c.Request().Context(), actor, body)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, result)
}
