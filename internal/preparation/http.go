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

// validateCorrectionReasonAndNote is the Waste and Remake boundary check: it
// normalizes the optional note and validates the reason/note pair against the
// operation's catalog validator before the handler runs, so a malformed
// request never reaches a transaction or claims its idempotency key. Every
// failure wraps response.ErrInvalid, which sendError answers with
// 400 INVALID_INPUT; the handlers keep their own domain validation (the
// INVALID_PREPARATION_REASON and INVALID_PREPARATION_NOTE rows) for direct
// callers and defense in depth. The normalized note is returned so the handler
// executes — and fingerprints — on exactly the input the boundary validated.
func validateCorrectionReasonAndNote(reason string, note *string,
	validateReason func(string) error,
) (*string, error) {
	normalized := NormalizeCorrectionNote(note)
	if err := validateReason(reason); err != nil {
		return nil, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error())
	}
	if err := ValidateCorrectionNote(reason, normalized); err != nil {
		return nil, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error())
	}
	return normalized, nil
}

// validateCorrectStateInput is the State Correction boundary check. It mirrors
// ValidateCorrectStateCommand's ordering and wraps every failure in
// response.ErrInvalid — selection shape (bounds, duplicates, zero ids), the
// correction target, the reason and note catalogs, and the Manager PIN's
// SHAPE only: a well-formed PIN that fails self-authentication is denied
// later, inside the mutation transaction, as the collapsed 403, never here.
func validateCorrectStateInput(cmd *CorrectStateCommand) (*string, error) {
	note := NormalizeCorrectionNote(cmd.Note)
	if err := ValidateCorrectionSelection(cmd.PreparationUnitIDs); err != nil {
		return nil, err // already wraps response.ErrInvalid
	}
	if _, ok := RequiredPriorState(cmd.TargetState); !ok {
		return nil, fmt.Errorf("%w: %q is not a correction target",
			response.ErrInvalid, cmd.TargetState)
	}
	if err := ValidateCorrectionReason(cmd.Reason); err != nil {
		return nil, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error())
	}
	if err := ValidateCorrectionNote(cmd.Reason, note); err != nil {
		return nil, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error())
	}
	if err := auth.ValidatePinFormat(cmd.ManagerPIN); err != nil {
		return nil, fmt.Errorf("%w: manager_pin %s", response.ErrInvalid, err.Error())
	}
	return note, nil
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
//	@Description	Moves one unit along the linear chain QUEUED -> IN_PREPARATION -> READY -> FULFILLED. The target state is explicit, so two baristas acting on a stale display get a conflict rather than a silent double advance. Waste, Remake, and State Correction are Phase 6B; Cancellation remains Phase 6C.
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

// handleAcknowledgeAlert godoc
//
//	@Summary		Acknowledge a Preparation Alert
//	@Description	Fills the alert's acknowledgment tuple — the acking actor's identity, their Staff Access Session, and one timestamp — inside one transaction. It changes no unit state, Waste, or financial meaning, and requires no Manager PIN.
//	@Tags			preparation
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			alert_id	path		string					true	"Preparation Alert ID"
//	@Param			body		body		AcknowledgeAlertCommand	true	"Acknowledgment request"
//	@Success		200			{object}	response.APIResponse{data=AlertResponse}
//	@Failure		400			{object}	response.APIResponse
//	@Failure		401			{object}	response.APIResponse
//	@Failure		403			{object}	response.APIResponse
//	@Failure		404			{object}	response.APIResponse
//	@Failure		409			{object}	response.APIResponse
//	@Failure		500			{object}	response.APIResponse
//	@Router			/preparation/alerts/{alert_id}/acknowledge [post]
func (s *Slices) handleAcknowledgeAlert(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	alertID, err := parseUUIDParam(c, "alert_id")
	if err != nil {
		return sendError(c, err)
	}
	body, err := bindBody[AcknowledgeAlertCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(body.RequestID); err != nil {
		return sendError(c, err)
	}
	body.AlertID = alertID

	status, result, err := s.AcknowledgeAlert.Handle(c.Request().Context(), actor, body)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, result)
}

// handleWasteUnit godoc
//
//	@Summary		Waste a Preparation Unit
//	@Description	Records the terminal Waste fact, sets the unit to WASTED, and creates the unacknowledged WASTE alert in one transaction. The reason must be in the Waste catalog and the optional note is normalized before validation. Requires no Manager PIN.
//	@Tags			preparation
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			unit_id	path		string				true	"Preparation Unit ID"
//	@Param			body	body		WasteUnitCommand	true	"Waste request"
//	@Success		201		{object}	response.APIResponse{data=WasteResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Failure		500		{object}	response.APIResponse
//	@Router			/preparation/units/{unit_id}/waste [post]
func (s *Slices) handleWasteUnit(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	unitID, err := parseUUIDParam(c, "unit_id")
	if err != nil {
		return sendError(c, err)
	}
	body, err := bindBody[WasteUnitCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(body.RequestID); err != nil {
		return sendError(c, err)
	}
	note, err := validateCorrectionReasonAndNote(body.Reason, body.Note, ValidateWasteReason)
	if err != nil {
		return sendError(c, err)
	}
	body.UnitID = unitID
	body.Note = note

	status, result, err := s.WasteUnit.Handle(c.Request().Context(), actor, body)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, result)
}

// handleRemakeUnit godoc
//
//	@Summary		Remake a Waste
//	@Description	Creates the linked replacement Preparation Unit — a fresh QUEUED unit with the next unit number of the same Order Item, the source's immutable preparation snapshot, and REMAKE priority — plus the Remake fact, in one transaction. The reason must be in the Remake catalog and the optional note is normalized before validation. Requires no Manager PIN.
//	@Tags			preparation
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			waste_id	path		string				true	"Preparation Waste ID"
//	@Param			body		body		RemakeUnitCommand	true	"Remake request"
//	@Success		201			{object}	response.APIResponse{data=RemakeResponse}
//	@Failure		400			{object}	response.APIResponse
//	@Failure		401			{object}	response.APIResponse
//	@Failure		403			{object}	response.APIResponse
//	@Failure		404			{object}	response.APIResponse
//	@Failure		409			{object}	response.APIResponse
//	@Failure		500			{object}	response.APIResponse
//	@Router			/preparation/wastes/{waste_id}/remake [post]
func (s *Slices) handleRemakeUnit(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	wasteID, err := parseUUIDParam(c, "waste_id")
	if err != nil {
		return sendError(c, err)
	}
	body, err := bindBody[RemakeUnitCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(body.RequestID); err != nil {
		return sendError(c, err)
	}
	note, err := validateCorrectionReasonAndNote(body.Reason, body.Note, ValidateRemakeReason)
	if err != nil {
		return sendError(c, err)
	}
	body.WasteID = wasteID
	body.Note = note

	status, result, err := s.RemakeUnit.Handle(c.Request().Context(), actor, body)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, result)
}

// handleCorrectState godoc
//
//	@Summary		Correct Preparation Unit states
//	@Description	Reverses 1 through 50 selected units one step along the chain — IN_PREPARATION to QUEUED, READY to IN_PREPARATION, FULFILLED to READY — as one all-or-nothing batch. Requires the Manager's own current PIN: the Manager role and PIN checks run inside the mutation transaction, so a wrong PIN is a collapsed 403 while a malformed PIN shape is 400 before any transaction. The PIN never appears in any response.
//	@Tags			preparation
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			body	body		CorrectStateCommand	true	"State Correction request"
//	@Success		200		{object}	response.APIResponse{data=CorrectStateResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Failure		500		{object}	response.APIResponse
//	@Router			/preparation/units/correct-state [post]
func (s *Slices) handleCorrectState(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	body, err := bindBody[CorrectStateCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(body.RequestID); err != nil {
		return sendError(c, err)
	}
	note, err := validateCorrectStateInput(&body)
	if err != nil {
		return sendError(c, err)
	}
	body.Note = note

	status, result, err := s.CorrectState.Handle(c.Request().Context(), actor, body)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, result)
}

// handleCancelUnits godoc
//
//	@Summary		Cancel or change queued Preparation Units
//	@Description	Cancels 1 through 50 queued units as one all-or-nothing batch. Each unit becomes CANCELLED with its typed transition, Cancellation fact, and CANCELLATION or CHANGE alert; each charged standard unit reduces its Check's live charge by its immutable unit price and the Check settles when the corrected balance reaches zero. A CHANGE links to an already-submitted later Order in the same active Service Session. Requires sales.operate and no Manager approval. The response carries no Check, Payment, or Refund data.
//	@Tags			preparation
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			body	body		CancelUnitsCommand	true	"Cancellation request"
//	@Success		200		{object}	response.APIResponse{data=CancelUnitsResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Failure		422		{object}	response.APIResponse
//	@Failure		500		{object}	response.APIResponse
//	@Router			/preparation/units/cancel [post]
func (s *Slices) handleCancelUnits(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	body, err := bindBody[CancelUnitsCommand](c)
	if err != nil {
		return sendError(c, err)
	}

	status, result, err := s.CancelUnits.Handle(c.Request().Context(), actor, body)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, result)
}
