package shift

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

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

// checkNonNegativeMoney rejects a missing or negative optional-zero money
// field before the handler runs. Zero is meaningful for a counted Cash
// amount, so a nil pointer must not silently become one, and a negative value
// must be rejected rather than flowing into the transaction.
func checkNonNegativeMoney(v *int64, field string) error {
	if err := checkMoney(v, field); err != nil {
		return err
	}
	if *v < 0 {
		return fmt.Errorf("%w: %s cannot be negative", response.ErrInvalid, field)
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
//	@Description	Returns the open Sales Shift with its Expected Cash and Cash Movements, or null when no Shift is open. expected_cash_vnd covers the Opening Float, Cash Payments, and Cash Movements. The Cash Refund term is still outstanding, because Refund is not implemented (ADR-020).
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

// handleStartReconciliation starts a Sales Shift's blind reconciliation.
//
//	@Summary		Start Sales Shift reconciliation
//	@Description	Starts the blind reconciliation of an open Sales Shift: it freezes the financial snapshot, records the submitted cash count as the blind initial count (sequence 1), and moves the Shift to CLOSING. The counted amount is submitted before any expected value is revealed. Returns the CLOSING Shift with its frozen reconciliation.
//	@Tags			shifts
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			shift_id	path		string						true	"Sales Shift ID"
//	@Param			request		body		StartReconciliationCommand	true	"Initial blind cash count"
//	@Success		201			{object}	response.APIResponse{data=ClosingShiftResponse}
//	@Failure		400			{object}	response.APIResponse
//	@Failure		401			{object}	response.APIResponse
//	@Failure		403			{object}	response.APIResponse
//	@Failure		404			{object}	response.APIResponse
//	@Failure		409			{object}	response.APIResponse	SALES_SHIFT_ALREADY_CLOSING, SHIFT_UNSETTLED_CHECK, SHIFT_PENDING_REFUND, SHIFT_UNRESOLVED_CORRECTION, or SHIFT_ACTIVE_SERVICE_SESSION, in that precedence order
//	@Router			/shifts/{shift_id}/reconciliation [post]
func (s *Slices) handleStartReconciliation(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	shiftID, err := parseUUIDParam(c, "shift_id")
	if err != nil {
		return sendError(c, err)
	}
	cmd, err := bindBody[StartReconciliationCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(cmd.RequestID); err != nil {
		return sendError(c, err)
	}
	if err := checkNonNegativeMoney(cmd.CountedCashVND, "counted_cash_vnd"); err != nil {
		return sendError(c, err)
	}
	cmd.ShiftID = shiftID

	status, res, err := s.StartReconciliation.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
}

// handleRecordCashCount appends a Cash Count attempt to a CLOSING Shift.
//
//	@Summary		Append a Cash Count
//	@Description	Appends one immutable Cash Count attempt (a recount) to a CLOSING Sales Shift's reconciliation. The Shift must be CLOSING; an OPEN Shift has no reconciliation to append to. Zero is a valid count. Returns the appended attempt plus the full preview built from the latest evidence.
//	@Tags			shifts
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			shift_id	path		string					true	"Sales Shift ID"
//	@Param			request		body		RecordCashCountCommand	true	"Recount Cash amount"
//	@Success		201			{object}	response.APIResponse{data=CashCountResult}
//	@Failure		400			{object}	response.APIResponse
//	@Failure		401			{object}	response.APIResponse
//	@Failure		403			{object}	response.APIResponse
//	@Failure		404			{object}	response.APIResponse
//	@Failure		409			{object}	response.APIResponse	SHIFT_RECONCILIATION_NOT_STARTED, SALES_SHIFT_ALREADY_CLOSED, or REQUEST_CONFLICT
//	@Router			/shifts/{shift_id}/reconciliation/cash-counts [post]
func (s *Slices) handleRecordCashCount(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	shiftID, err := parseUUIDParam(c, "shift_id")
	if err != nil {
		return sendError(c, err)
	}
	cmd, err := bindBody[RecordCashCountCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(cmd.RequestID); err != nil {
		return sendError(c, err)
	}
	// An omitted or negative amount is rejected before any domain
	// transaction; an explicit zero stays a meaningful count.
	if err := checkNonNegativeMoney(cmd.CountedCashVND, "counted_cash_vnd"); err != nil {
		return sendError(c, err)
	}
	cmd.ShiftID = shiftID

	status, res, err := s.RecordCashCount.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
}

// handleRecordQRObservation appends a Manual QR observation attempt to a
// CLOSING Shift.
//
//	@Summary		Append a Manual QR Observation
//	@Description	Appends one immutable Manual QR observation attempt (a recheck) to a CLOSING Sales Shift's reconciliation. Both observed values are mandatory together and explicit, including zero; one without the other is rejected. Returns the appended observation plus the full preview built from the latest evidence.
//	@Tags			shifts
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			shift_id	path		string							true	"Sales Shift ID"
//	@Param			request		body		RecordQRObservationCommand	true	"Observed received and refunded totals"
//	@Success		201			{object}	response.APIResponse{data=QRObservationResult}
//	@Failure		400			{object}	response.APIResponse
//	@Failure		401			{object}	response.APIResponse
//	@Failure		403			{object}	response.APIResponse
//	@Failure		404			{object}	response.APIResponse
//	@Failure		409			{object}	response.APIResponse	SHIFT_RECONCILIATION_NOT_STARTED, SALES_SHIFT_ALREADY_CLOSED, or REQUEST_CONFLICT
//	@Router			/shifts/{shift_id}/reconciliation/qr-observations [post]
func (s *Slices) handleRecordQRObservation(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	shiftID, err := parseUUIDParam(c, "shift_id")
	if err != nil {
		return sendError(c, err)
	}
	cmd, err := bindBody[RecordQRObservationCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(cmd.RequestID); err != nil {
		return sendError(c, err)
	}
	// Both values are mandatory together and non-negative: one without the
	// other, or a negative one, is rejected before any domain transaction,
	// while explicit zeroes are valid observations (spec 14.3).
	if err := checkNonNegativeMoney(cmd.ObservedReceivedVND, "observed_received_vnd"); err != nil {
		return sendError(c, err)
	}
	if err := checkNonNegativeMoney(cmd.ObservedRefundedVND, "observed_refunded_vnd"); err != nil {
		return sendError(c, err)
	}
	cmd.ShiftID = shiftID

	status, res, err := s.RecordQRObservation.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
}

// parseClosedShiftListQuery parses and validates the history list query
// parameters (spec 9.5): both window bounds are required RFC 3339 instants,
// the window spans at most 31 days, and the limit is a positive integer. The
// cursor is passed through opaquely; its decode and range match are the
// handler's job, where the normalized window is authoritative.
func parseClosedShiftListQuery(c echo.Context) (ListClosedShiftsQuery, error) {
	var query ListClosedShiftsQuery

	closedFromRaw := c.QueryParam("closed_from")
	if closedFromRaw == "" {
		return query, fmt.Errorf("%w: closed_from is required", response.ErrInvalid)
	}
	closedToRaw := c.QueryParam("closed_to")
	if closedToRaw == "" {
		return query, fmt.Errorf("%w: closed_to is required", response.ErrInvalid)
	}
	closedFrom, err := time.Parse(time.RFC3339, closedFromRaw)
	if err != nil {
		return query, fmt.Errorf("%w: closed_from must be an RFC 3339 instant", response.ErrInvalid)
	}
	closedTo, err := time.Parse(time.RFC3339, closedToRaw)
	if err != nil {
		return query, fmt.Errorf("%w: closed_to must be an RFC 3339 instant", response.ErrInvalid)
	}
	if err := validateClosedShiftWindow(closedFrom, closedTo); err != nil {
		return query, err
	}
	query.ClosedFrom = closedFrom
	query.ClosedTo = closedTo

	if raw := c.QueryParam("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil {
			return query, fmt.Errorf("%w: limit must be a positive integer", response.ErrInvalid)
		}
		if limit < 1 {
			return query, fmt.Errorf("%w: limit must be at least 1", response.ErrInvalid)
		}
		query.Limit = limit
	}
	query.Cursor = c.QueryParam("cursor")
	return query, nil
}

// handleListClosedShifts pages through the closed Sales Shift history.
//
//	@Summary		List closed Sales Shifts
//	@Description	Returns Manager-visible closed Sales Shift summaries ordered by closed_at descending (ties by id) inside a half-open [closed_from, closed_to) window of at most 31 days. Pagination is keyset: a full page returns an opaque next_cursor to pass back verbatim; the cursor is exclusive and bound to the window it was minted over. Requires the audit.inspect capability; no fresh PIN is needed.
//	@Tags			shifts
//	@Produce		json
//	@Security		BearerAuth
//	@Param			closed_from	query		string	true	"Inclusive window start, RFC 3339"
//	@Param			closed_to	query		string	true	"Exclusive window end, RFC 3339"
//	@Param			cursor		query		string	false	"Opaque next_cursor from the previous page"
//	@Param			limit		query		int		false	"Page size, default 50, capped at 100"
//	@Success		200			{object}	response.APIResponse{data=ClosedShiftListResponse}
//	@Failure		400			{object}	response.APIResponse	Missing or malformed window, a window over 31 days, a limit below one, or a malformed or mismatched cursor
//	@Failure		401			{object}	response.APIResponse
//	@Failure		403			{object}	response.APIResponse	Requires audit.inspect
//	@Router			/shifts [get]
func (s *Slices) handleListClosedShifts(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	query, err := parseClosedShiftListQuery(c)
	if err != nil {
		return sendError(c, err)
	}

	res, err := s.ListClosedShifts.Handle(c.Request().Context(), actor, query)
	if err != nil {
		return sendError(c, err)
	}
	return response.OK(c, res)
}

// handleGetClosedShift returns one closed Sales Shift's immutable detail.
//
//	@Summary		Read a closed Sales Shift
//	@Description	Returns one closed Sales Shift's immutable detail: the summary, the complete frozen source scalars, the reconciliation starter, every Cash Count and Manual QR observation, the discrepancy rows, and the approving Manager when the close was discrepant. Only closed Shifts are exposed: an unknown id and a Shift that is still OPEN or CLOSING both return 404. Requires the audit.inspect capability; no fresh PIN is needed.
//	@Tags			shifts
//	@Produce		json
//	@Security		BearerAuth
//	@Param			shift_id	path		string	true	"Sales Shift ID"
//	@Success		200			{object}	response.APIResponse{data=ClosedShiftDetailResponse}
//	@Failure		400			{object}	response.APIResponse	Malformed shift_id
//	@Failure		401			{object}	response.APIResponse
//	@Failure		403			{object}	response.APIResponse	Requires audit.inspect
//	@Failure		404			{object}	response.APIResponse	Unknown Shift, or the Shift is not closed
//	@Router			/shifts/{shift_id} [get]
func (s *Slices) handleGetClosedShift(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	shiftID, err := parseUUIDParam(c, "shift_id")
	if err != nil {
		return sendError(c, err)
	}

	res, err := s.GetClosedShift.Handle(c.Request().Context(), actor, shiftID)
	if err != nil {
		return sendError(c, err)
	}
	return response.OK(c, res)
}

// handleFinalClose closes a reconciled Sales Shift.
//
//	@Summary		Close a reconciled Sales Shift
//	@Description	Performs the Final Close of a CLOSING Sales Shift. The request carries the final evidence ids and, for a discrepant close, one reason per nonzero dimension. Amounts are always derived server-side from the frozen snapshot and the final evidence. A non-null empty discrepancies array closes exactly; any entry selects the discrepant close, which requires inline approval by an enabled Manager holding the MANAGER role, who authenticates with their own login code and PIN. Returns the immutable closed-Shift detail for both outcomes.
//	@Tags			shifts
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			shift_id	path		string				true	"Sales Shift ID"
//	@Param			request		body		CloseShiftCommand	true	"Final evidence and discrepancy reasons"
//	@Success		200			{object}	response.APIResponse{data=ClosedShiftDetailResponse}
//	@Failure		400			{object}	response.APIResponse	Malformed body, omitted evidence id, a missing or null discrepancies array, or an invalid reason or note shape
//	@Failure		401			{object}	response.APIResponse
//	@Failure		403			{object}	response.APIResponse	Missing capability or failed Manager approval, collapsed to MANAGER_APPROVAL_UNAVAILABLE
//	@Failure		404			{object}	response.APIResponse	Unknown Sales Shift or final attempt id
//	@Failure		409			{object}	response.APIResponse	Lifecycle, blocker, stale evidence, source mismatch, recount/recheck, or discrepancy reason conflicts
//	@Router			/shifts/{shift_id}/close [post]
func (s *Slices) handleFinalClose(c echo.Context) error {
	actor, err := getActor(c)
	if err != nil {
		return sendError(c, err)
	}
	shiftID, err := parseUUIDParam(c, "shift_id")
	if err != nil {
		return sendError(c, err)
	}
	cmd, err := bindBody[CloseShiftCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := checkRequestID(cmd.RequestID); err != nil {
		return sendError(c, err)
	}
	// Both final evidence ids are required: a zero UUID cannot name an attempt.
	if cmd.FinalCashCountID == uuid.Nil {
		return sendError(c, fmt.Errorf("%w: final_cash_count_id is required", response.ErrInvalid))
	}
	if cmd.FinalQRObservationID == uuid.Nil {
		return sendError(c, fmt.Errorf("%w: final_qr_observation_id is required", response.ErrInvalid))
	}
	// discrepancies must be a non-null array: JSON null and an omitted field
	// both leave the slice nil, and only an explicit [] closes exactly (spec
	// 9.4).
	if cmd.Discrepancies == nil {
		return sendError(c, fmt.Errorf("%w: discrepancies is required and must be a non-null array", response.ErrInvalid))
	}
	// Each reason entry is shape-checked before dispatch: the dimension and
	// reason allowlists and the note rules. A reason-to-dimension pairing that
	// does not match the server-derived differences is a 409 conflict raised
	// inside the transaction instead, where those differences are known.
	seenDimensions := make(map[DiscrepancyDimension]struct{}, len(cmd.Discrepancies))
	for _, entry := range cmd.Discrepancies {
		note := NormalizeNote(entry.Note)
		if err := validateDiscrepancyReasonShape(string(entry.Dimension), string(entry.Reason), note); err != nil {
			return sendError(c, fmt.Errorf("%w: discrepancies: %s", response.ErrInvalid, err.Error()))
		}
		if _, duplicate := seenDimensions[entry.Dimension]; duplicate {
			return sendError(c, fmt.Errorf("%w: discrepancies: duplicate dimension %s",
				response.ErrInvalid, entry.Dimension))
		}
		seenDimensions[entry.Dimension] = struct{}{}
	}
	// The approval pair belongs to the discrepant close only. A non-empty but
	// malformed PIN is rejected on shape alone; a missing or wrong pair is
	// dispatched so the transaction's verification denies it and collapses to
	// the one 403 code without naming the reason (spec 10, 12).
	if len(cmd.Discrepancies) > 0 && cmd.ManagerPIN != "" {
		if err := auth.ValidatePinFormat(cmd.ManagerPIN); err != nil {
			return sendError(c, fmt.Errorf("%w: manager_pin: %s", response.ErrInvalid, err.Error()))
		}
	}
	cmd.ShiftID = shiftID

	status, res, err := s.Close.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return sendResult(c, status, res)
}
