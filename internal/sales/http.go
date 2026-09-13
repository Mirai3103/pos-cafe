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

// handleListActiveSessions returns every ACTIVE Service Session.
//
//	@Summary		List active Service Sessions
//	@Description	Returns every ACTIVE Service Session with its full projection, ordered by creation. This is the cashier's open-tabs view.
//	@Tags			sales
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	response.APIResponse{data=[]ServiceSessionResponse}
//	@Failure		401	{object}	response.APIResponse
//	@Failure		403	{object}	response.APIResponse
//	@Router			/sales/service-sessions [get]
func (s *Slices) handleListActiveSessions(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	result, err := s.ListActiveSessions.Handle(c.Request().Context(), actor)
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

// handleSetDraftItemQuantity sets an absolute quantity on one draft item.
//
//	@Summary		Set a draft item's quantity
//	@Description	Sets an absolute quantity between 1 and 9999 on one Order Draft item. Zero is rejected: removal is its own command. Returns the whole Service Session projection.
//	@Tags			sales
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path	string						true	"Service Session ID"
//	@Param			item_id	path	string						true	"Order Draft Item ID"
//	@Param			request	body	SetDraftItemQuantityCommand	true	"Request"
//	@Success		200		{object}	response.APIResponse{data=ServiceSessionResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Router			/sales/service-sessions/{id}/draft/items/{item_id}/quantity [patch]
func (s *Slices) handleSetDraftItemQuantity(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	cmd, err := bindBody[SetDraftItemQuantityCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(cmd.RequestID); err != nil {
		return sendError(c, err)
	}
	// ServiceSessionID and DraftItemID are json:"-": they come from the path,
	// never the body.
	sessionID, err := parseUUIDParam(c, "id")
	if err != nil {
		return sendError(c, err)
	}
	itemID, err := parseUUIDParam(c, "item_id")
	if err != nil {
		return sendError(c, err)
	}
	cmd.ServiceSessionID = sessionID
	cmd.DraftItemID = itemID
	status, result, err := s.SetItemQuantity.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, result)
}

// handleSetDraftItemSize sets or clears one draft item's Size.
//
//	@Summary		Set a draft item's Size
//	@Description	Sets or clears one Order Draft item's Size. A null size_id clears the Size and is accepted: the draft tolerates an incomplete configuration until Commit in Phase 5B. If the edit makes the item identical in composition to another line of the same draft, the two MERGE: the pre-existing line's quantity grows by this one's, this line's id is deleted, and the returned projection reflects the merge. A merged quantity above 9999 is rejected. Returns the whole Service Session projection.
//	@Tags			sales
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path	string					true	"Service Session ID"
//	@Param			item_id	path	string					true	"Order Draft Item ID"
//	@Param			request	body	SetDraftItemSizeCommand	true	"Request"
//	@Success		200		{object}	response.APIResponse{data=ServiceSessionResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Router			/sales/service-sessions/{id}/draft/items/{item_id}/size [patch]
func (s *Slices) handleSetDraftItemSize(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	cmd, err := bindBody[SetDraftItemSizeCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(cmd.RequestID); err != nil {
		return sendError(c, err)
	}
	// ServiceSessionID and DraftItemID are json:"-": they come from the path,
	// never the body.
	sessionID, err := parseUUIDParam(c, "id")
	if err != nil {
		return sendError(c, err)
	}
	itemID, err := parseUUIDParam(c, "item_id")
	if err != nil {
		return sendError(c, err)
	}
	cmd.ServiceSessionID = sessionID
	cmd.DraftItemID = itemID
	status, result, err := s.SetItemSize.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, result)
}

// handleSetDraftItemNote sets or clears one draft item's Preparation Note.
//
//	@Summary		Set a draft item's Preparation Note
//	@Description	Sets or clears one Order Draft item's Preparation Note. The note is trimmed and may be at most 200 characters; a null or blank preparation_note clears it. If the edit makes the item identical in composition to another line of the same draft, the two MERGE: the pre-existing line's quantity grows by this one's, this line's id is deleted, and the returned projection reflects the merge. A merged quantity above 9999 is rejected. Returns the whole Service Session projection.
//	@Tags			sales
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path	string					true	"Service Session ID"
//	@Param			item_id	path	string					true	"Order Draft Item ID"
//	@Param			request	body	SetDraftItemNoteCommand	true	"Request"
//	@Success		200		{object}	response.APIResponse{data=ServiceSessionResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Router			/sales/service-sessions/{id}/draft/items/{item_id}/preparation-note [patch]
func (s *Slices) handleSetDraftItemNote(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	cmd, err := bindBody[SetDraftItemNoteCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(cmd.RequestID); err != nil {
		return sendError(c, err)
	}
	// ServiceSessionID and DraftItemID are json:"-": they come from the path,
	// never the body.
	sessionID, err := parseUUIDParam(c, "id")
	if err != nil {
		return sendError(c, err)
	}
	itemID, err := parseUUIDParam(c, "item_id")
	if err != nil {
		return sendError(c, err)
	}
	cmd.ServiceSessionID = sessionID
	cmd.DraftItemID = itemID
	status, result, err := s.SetItemNote.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, result)
}

// handleSetDraftItemModifiers replaces one draft item's selected options.
//
//	@Summary		Set a draft item's Modifier Options
//	@Description	Replaces one Order Draft item's selected Modifier Options. Unlike adding an item, the list is taken literally: an empty modifier_option_ids selects no options and no defaults are applied. If the edit makes the item identical in composition to another line of the same draft, the two MERGE: the pre-existing line's quantity grows by this one's, this line's id is deleted, and the returned projection reflects the merge. A merged quantity above 9999 is rejected. Returns the whole Service Session projection.
//	@Tags			sales
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path	string						true	"Service Session ID"
//	@Param			item_id	path	string						true	"Order Draft Item ID"
//	@Param			request	body	SetDraftItemModifiersCommand	true	"Request"
//	@Success		200		{object}	response.APIResponse{data=ServiceSessionResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Router			/sales/service-sessions/{id}/draft/items/{item_id}/modifiers [patch]
func (s *Slices) handleSetDraftItemModifiers(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	cmd, err := bindBody[SetDraftItemModifiersCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(cmd.RequestID); err != nil {
		return sendError(c, err)
	}
	// ServiceSessionID and DraftItemID are json:"-": they come from the path,
	// never the body.
	sessionID, err := parseUUIDParam(c, "id")
	if err != nil {
		return sendError(c, err)
	}
	itemID, err := parseUUIDParam(c, "item_id")
	if err != nil {
		return sendError(c, err)
	}
	cmd.ServiceSessionID = sessionID
	cmd.DraftItemID = itemID
	status, result, err := s.SetItemModifiers.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, result)
}

// handleRemoveDraftItem removes one draft item outright.
//
//	@Summary		Remove a draft item
//	@Description	Removes one Order Draft item and its selected options, returning the whole Service Session projection with 200 rather than 204 so a client sees the resulting draft without a follow-up read. request_id may be sent as a query parameter instead of in the body, since some clients and proxies strip a DELETE request's body.
//	@Tags			sales
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id			path	string					true	"Service Session ID"
//	@Param			item_id		path	string					true	"Order Draft Item ID"
//	@Param			request_id	query	string					false	"Idempotency key, if not sent in the body"
//	@Param			request		body	RemoveDraftItemCommand	false	"Request"
//	@Success		200		{object}	response.APIResponse{data=ServiceSessionResponse}
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Router			/sales/service-sessions/{id}/draft/items/{item_id} [delete]
func (s *Slices) handleRemoveDraftItem(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	cmd, err := bindBody[RemoveDraftItemCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(cmd.RequestID); err != nil {
		return sendError(c, err)
	}
	// ServiceSessionID and DraftItemID are json:"-": they come from the path,
	// never the body.
	sessionID, err := parseUUIDParam(c, "id")
	if err != nil {
		return sendError(c, err)
	}
	itemID, err := parseUUIDParam(c, "item_id")
	if err != nil {
		return sendError(c, err)
	}
	cmd.ServiceSessionID = sessionID
	cmd.DraftItemID = itemID
	status, result, err := s.RemoveDraftItem.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, result)
}

// handleCommitDraft godoc
//
//	@Summary		Commit the Order Draft
//	@Description	Revalidates the draft, freezes prices into immutable Committed Items, and charges a Check. The response's checks[].payments and checks[].total_applied_vnd are filled by Phase 5C; allocations[].submitted is filled by Phase 5D.
//	@Tags			sales
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string					true	"Service Session ID"
//	@Param			body	body		CommitOrderDraftCommand	true	"Commit request"
//	@Success		200		{object}	response.APIResponse{data=ServiceSessionResponse}
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Failure		422		{object}	response.APIResponse
//	@Router			/sales/service-sessions/{id}/draft/commit [post]
func (s *Slices) handleCommitDraft(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	sessionID, err := parseUUIDParam(c, "id")
	if err != nil {
		return sendError(c, err)
	}
	body, err := bindBody[CommitOrderDraftCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(body.RequestID); err != nil {
		return sendError(c, err)
	}
	body.ServiceSessionID = sessionID

	status, result, err := s.CommitDraft.Handle(c.Request().Context(), actor, body)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, result)
}

// handleStartNewDraft godoc
//
//	@Summary		Start a new Order Draft
//	@Description	Opens the Service Session's next Order Draft. Rejected while the Session holds an editable draft, or a committed draft with no Order — in Phase 5B the latter blocks every committed draft, because Submit arrives in 5D.
//	@Tags			sales
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string						true	"Service Session ID"
//	@Param			body	body		StartNewOrderDraftCommand	true	"Start request"
//	@Success		200		{object}	response.APIResponse{data=ServiceSessionResponse}
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		404		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Router			/sales/service-sessions/{id}/draft [post]
func (s *Slices) handleStartNewDraft(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	sessionID, err := parseUUIDParam(c, "id")
	if err != nil {
		return sendError(c, err)
	}
	body, err := bindBody[StartNewOrderDraftCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(body.RequestID); err != nil {
		return sendError(c, err)
	}
	body.ServiceSessionID = sessionID

	status, result, err := s.StartNewDraft.Handle(c.Request().Context(), actor, body)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, result)
}

// handleSetCheckTarget godoc
//
//	@Summary		Set the Order Draft's Check target
//	@Description	Steers where the next Commit's charges land. CURRENT_UNPAID reuses the Session's most recent open Check; NEW_CHECK always opens one. The target belongs to the draft and resets when a new draft opens.
//	@Tags			sales
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string					true	"Service Session ID"
//	@Param			body	body		SetCheckTargetCommand	true	"Check target request"
//	@Success		200		{object}	response.APIResponse{data=ServiceSessionResponse}
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Failure		422		{object}	response.APIResponse
//	@Router			/sales/service-sessions/{id}/draft/check-target [put]
func (s *Slices) handleSetCheckTarget(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	sessionID, err := parseUUIDParam(c, "id")
	if err != nil {
		return sendError(c, err)
	}
	body, err := bindBody[SetCheckTargetCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(body.RequestID); err != nil {
		return sendError(c, err)
	}
	body.ServiceSessionID = sessionID

	status, result, err := s.SetCheckTarget.Handle(c.Request().Context(), actor, body)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, result)
}
