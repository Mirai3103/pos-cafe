package shift

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

// checkMoney rejects a missing numeric field rather than defaulting it. Zero is
// a meaningful Opening Float, so a nil pointer must not silently become one.
func checkMoney(v *int64, field string) error {
	if v == nil {
		return fmt.Errorf("%w: %s is required", response.ErrInvalid, field)
	}
	return nil
}

func checkRequiredString(v, field string) error {
	if v == "" {
		return fmt.Errorf("%w: %s is required", response.ErrInvalid, field)
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

// handleGetCurrent returns the open Sales Shift, or null when none is open.
//
//	@Summary		Current Sales Shift
//	@Description	Returns the open Sales Shift with its Expected Cash and Cash Movements, or null when no Shift is open. expected_cash_vnd covers the Opening Float and Cash Movements only; Cash Payments and Cash Refunds join the figure in Phase 5 (ADR-008).
//	@Tags			shifts
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	response.APIResponse{data=CurrentSalesShiftResponse}
//	@Failure		401	{object}	response.APIResponse
//	@Failure		403	{object}	response.APIResponse
//	@Router			/shifts/current [get]
func (s *Slices) handleGetCurrent(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	res, err := s.Current.Handle(c.Request().Context(), actor)
	if err != nil {
		return sendError(c, err)
	}
	// A nil result serializes as data: null, which is the canonical
	// "no Shift is open" response, not a 404.
	return response.OK(c, res)
}

// handleOpenShift opens a Sales Shift.
//
//	@Summary		Open a Sales Shift
//	@Description	Opens a Sales Shift with a counted Opening Float. At most one Sales Shift may be open across the whole system.
//	@Tags			shifts
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		OpenShiftCommand	true	"Opening Float"
//	@Success		201		{object}	response.APIResponse{data=SalesShiftResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Router			/shifts [post]
func (s *Slices) handleOpenShift(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	cmd, err := bindBody[OpenShiftCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(cmd.RequestID); err != nil {
		return sendError(c, err)
	}
	if err := checkMoney(cmd.OpeningFloatVND, "opening_float_vnd"); err != nil {
		return sendError(c, err)
	}

	status, res, err := s.OpenShift.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
}

// handleRecordCashMovement records a Pay In or Pay Out.
//
//	@Summary		Record a Cash Movement
//	@Description	Records a Pay In or Pay Out against an open Sales Shift. Requires inline approval by an enabled Manager, who authenticates with their own login code and PIN. Returns the resulting Expected Cash.
//	@Tags			shifts
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			shift_id	path		string						true	"Sales Shift ID"
//	@Param			request		body		RecordCashMovementCommand	true	"Cash Movement to record"
//	@Success		201			{object}	response.APIResponse{data=CashMovementResult}
//	@Failure		400			{object}	response.APIResponse
//	@Failure		401			{object}	response.APIResponse
//	@Failure		403			{object}	response.APIResponse
//	@Failure		409			{object}	response.APIResponse
//	@Router			/shifts/{shift_id}/cash-movements [post]
func (s *Slices) handleRecordCashMovement(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	shiftID, err := parseUUIDParam(c, "shift_id")
	if err != nil {
		return sendError(c, err)
	}
	cmd, err := bindBody[RecordCashMovementCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(cmd.RequestID); err != nil {
		return sendError(c, err)
	}
	if err := checkMoney(cmd.AmountVND, "amount_vnd"); err != nil {
		return sendError(c, err)
	}
	if err := checkRequiredString(cmd.Method, "method"); err != nil {
		return sendError(c, err)
	}
	if err := checkRequiredString(cmd.Reason, "reason"); err != nil {
		return sendError(c, err)
	}
	if err := checkRequiredString(cmd.ApproverLoginCode, "approver_login_code"); err != nil {
		return sendError(c, err)
	}
	// A malformed PIN is rejected on shape alone, before any identity lookup.
	// This leaks nothing: it says the field is wrong, never whose PIN it is.
	if err := auth.ValidatePinFormat(cmd.ManagerPIN); err != nil {
		return sendError(c, fmt.Errorf("%w: manager_pin: %s", response.ErrInvalid, err.Error()))
	}
	cmd.ShiftID = shiftID

	status, res, err := s.RecordCashMovement.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
}
