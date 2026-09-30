package preparation

import (
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/platform/httpx"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/labstack/echo/v4"
)

// sendError writes err through the Preparation handler error mapping.
func sendError(c echo.Context, err error) error {
	return httpx.SendError(c, err, handlerErrors.Map)
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
	actor, err := httpx.Actor(c)
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
	actor, err := httpx.Actor(c)
	if err != nil {
		return sendError(c, err)
	}
	body, err := httpx.BindBody[BulkAdvanceCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	status, result, err := s.BulkAdvance.Handle(c.Request().Context(), actor, body)
	if err != nil {
		return sendError(c, err)
	}
	return httpx.SendResult(c, status, result)
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
	actor, err := httpx.Actor(c)
	if err != nil {
		return sendError(c, err)
	}
	unitID, err := httpx.UUIDParam(c, "unit_id")
	if err != nil {
		return sendError(c, err)
	}
	body, err := httpx.BindBody[AdvanceUnitCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := httpx.RequireRequestID(body.RequestID); err != nil {
		return sendError(c, err)
	}
	body.UnitID = unitID

	status, result, err := s.AdvanceUnit.Handle(c.Request().Context(), actor, body)
	if err != nil {
		return sendError(c, err)
	}
	return httpx.SendResult(c, status, result)
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
	actor, err := httpx.Actor(c)
	if err != nil {
		return sendError(c, err)
	}
	alertID, err := httpx.UUIDParam(c, "alert_id")
	if err != nil {
		return sendError(c, err)
	}
	body, err := httpx.BindBody[AcknowledgeAlertCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := httpx.RequireRequestID(body.RequestID); err != nil {
		return sendError(c, err)
	}
	body.AlertID = alertID

	status, result, err := s.AcknowledgeAlert.Handle(c.Request().Context(), actor, body)
	if err != nil {
		return sendError(c, err)
	}
	return httpx.SendResult(c, status, result)
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
	actor, err := httpx.Actor(c)
	if err != nil {
		return sendError(c, err)
	}
	unitID, err := httpx.UUIDParam(c, "unit_id")
	if err != nil {
		return sendError(c, err)
	}
	body, err := httpx.BindBody[WasteUnitCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := httpx.RequireRequestID(body.RequestID); err != nil {
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
	return httpx.SendResult(c, status, result)
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
	actor, err := httpx.Actor(c)
	if err != nil {
		return sendError(c, err)
	}
	wasteID, err := httpx.UUIDParam(c, "waste_id")
	if err != nil {
		return sendError(c, err)
	}
	body, err := httpx.BindBody[RemakeUnitCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := httpx.RequireRequestID(body.RequestID); err != nil {
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
	return httpx.SendResult(c, status, result)
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
	actor, err := httpx.Actor(c)
	if err != nil {
		return sendError(c, err)
	}
	body, err := httpx.BindBody[CorrectStateCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := httpx.RequireRequestID(body.RequestID); err != nil {
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
	return httpx.SendResult(c, status, result)
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
	actor, err := httpx.Actor(c)
	if err != nil {
		return sendError(c, err)
	}
	body, err := httpx.BindBody[CancelUnitsCommand](c)
	if err != nil {
		return sendError(c, err)
	}

	status, result, err := s.CancelUnits.Handle(c.Request().Context(), actor, body)
	if err != nil {
		return sendError(c, err)
	}
	return httpx.SendResult(c, status, result)
}
