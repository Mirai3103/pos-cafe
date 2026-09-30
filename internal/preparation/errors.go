package preparation

import (
	"errors"
	"net/http"

	"github.com/Mirai3103/pos-cafe/internal/platform/command"
	"github.com/Mirai3103/pos-cafe/internal/response"
)

var (
	ErrUnitNotFound      = errors.New("preparation unit not found")
	ErrInvalidTransition = errors.New("not a legal advance from the unit's current state")
	ErrSessionExpired    = errors.New("staff access session expired")
	ErrSessionRevoked    = errors.New("staff access session revoked")

	ErrRequestConflict     = command.ErrRequestConflict
	ErrInvalidStoredResult = command.ErrInvalidStoredResult
	ErrForbidden           = command.ErrForbidden
	ErrUnauthorized        = command.ErrUnauthorized

	// Corrections & Recovery. Each expected condition has its own sentinel so
	// HTTP mapping never inspects error strings.
	ErrAlertNotFound            = errors.New("preparation alert not found")
	ErrWasteNotFound            = errors.New("preparation waste not found")
	ErrInvalidReason            = errors.New("invalid preparation reason")
	ErrInvalidNote              = errors.New("invalid preparation note")
	ErrAlertAlreadyAcknowledged = errors.New("preparation alert already acknowledged")
	ErrWasteAlreadyRemade       = errors.New("preparation waste already remade")
	ErrServiceSessionClosed     = errors.New("service session already closed")
	// ErrInvalidManagerPIN denies a well-formed PIN that fails current
	// self-authentication. It collapses into the shared NOT_AUTHORIZED
	// response so the API never discloses which denial condition failed; a
	// missing or malformed PIN shape is instead a response.ErrInvalid
	// boundary error raised before the transaction.
	ErrInvalidManagerPIN = errors.New("manager PIN verification failed")

	// Cancellation & Change. Condition-based sentinels: each maps to one HTTP
	// status in httpErrors, and HTTP mapping never inspects error strings.
	ErrCancellationSelectionInvalid = errors.New("invalid cancellation selection")
	ErrReplacementOrderRequired     = errors.New("replacement_order_id is required for a change")
	ErrReplacementOrderNotFound     = errors.New("replacement order not found")
	ErrReplacementOrderInvalid      = errors.New("invalid replacement order")
	ErrCancellationSourceNotQueued  = errors.New("cancellation source is not queued")
	ErrCancellationSessionMismatch  = errors.New("cancellation units belong to different service sessions")
	ErrChargeAdjustmentConflict     = errors.New("charge adjustment conflicts with an existing correction")
	ErrOpenShiftRequired            = errors.New("an open sales shift is required")
	// ErrCancellationChargeOutOfRange reports a guarded monetary range
	// failure caused by the request's own size, so it is a client-shaped
	// 422 rather than a defect.
	ErrCancellationChargeOutOfRange = errors.New("cancellation charge out of range")
	// ErrChargeInvariantViolated reports that a Check's stored charge_vnd
	// disagrees with base allocations less live adjustments. That is a
	// defect, not a business state, so it is deliberately absent from
	// httpErrors and surfaces as a logged 500.
	ErrChargeInvariantViolated = errors.New("check charge does not match its allocations")
)

// notAuthorized is the collapsed denial every authorization failure answers
// with: the API must never disclose which denial condition failed. The reason
// is in the server log and the audit event.
const notAuthorized = "not authorized"

// fixed maps target to status and code with the sentinel's own stable text as
// the client message, so wrapped detail — including PostgreSQL error detail —
// never reaches the client.
func fixed(target error, status int, code string) response.ErrorRule {
	return response.ErrorRule{Target: target, Spec: response.ErrorSpec{
		Status: status, Code: code, Message: target.Error(),
	}}
}

// denied maps target to the collapsed NOT_AUTHORIZED denial.
func denied(target error, status int) response.ErrorRule {
	return response.ErrorRule{Target: target, Spec: response.ErrorSpec{
		Status: status, Code: "NOT_AUTHORIZED", Message: notAuthorized,
	}}
}

// httpErrors is the Preparation domain error mapping. The session sentinels
// ride the 401 denial row, so a session-expiry or revocation denial can never
// surface as a generic 500. Rules are tried in order, first match wins.
var httpErrors = response.ErrorMapper{
	fixed(ErrUnitNotFound, http.StatusNotFound, "PREPARATION_UNIT_NOT_FOUND"),
	fixed(ErrInvalidTransition, http.StatusConflict, "INVALID_TRANSITION"),
	{Target: ErrRequestConflict, Spec: response.ErrorSpec{Status: http.StatusConflict, Code: "REQUEST_CONFLICT"}},
	{Target: ErrInvalidStoredResult, Spec: response.ErrorSpec{
		Status: http.StatusInternalServerError, Code: "INVALID_STORED_RESULT", Message: "an unexpected error occurred",
	}},
	fixed(ErrAlertNotFound, http.StatusNotFound, "PREPARATION_ALERT_NOT_FOUND"),
	fixed(ErrWasteNotFound, http.StatusNotFound, "PREPARATION_WASTE_NOT_FOUND"),
	fixed(ErrInvalidReason, http.StatusBadRequest, "INVALID_PREPARATION_REASON"),
	fixed(ErrInvalidNote, http.StatusBadRequest, "INVALID_PREPARATION_NOTE"),
	fixed(ErrAlertAlreadyAcknowledged, http.StatusConflict, "PREPARATION_ALERT_ALREADY_ACKNOWLEDGED"),
	fixed(ErrWasteAlreadyRemade, http.StatusConflict, "PREPARATION_WASTE_ALREADY_REMADE"),
	fixed(ErrServiceSessionClosed, http.StatusConflict, "SERVICE_SESSION_ALREADY_CLOSED"),
	denied(ErrForbidden, http.StatusForbidden),
	denied(ErrInvalidManagerPIN, http.StatusForbidden),
	fixed(ErrCancellationSelectionInvalid, http.StatusBadRequest, "CANCELLATION_SELECTION_INVALID"),
	fixed(ErrReplacementOrderRequired, http.StatusBadRequest, "REPLACEMENT_ORDER_REQUIRED"),
	fixed(ErrReplacementOrderNotFound, http.StatusNotFound, "REPLACEMENT_ORDER_NOT_FOUND"),
	fixed(ErrReplacementOrderInvalid, http.StatusConflict, "REPLACEMENT_ORDER_INVALID"),
	fixed(ErrCancellationSourceNotQueued, http.StatusConflict, "CANCELLATION_SOURCE_NOT_QUEUED"),
	fixed(ErrCancellationSessionMismatch, http.StatusConflict, "CANCELLATION_SESSION_MISMATCH"),
	fixed(ErrChargeAdjustmentConflict, http.StatusConflict, "CHARGE_ADJUSTMENT_CONFLICT"),
	fixed(ErrOpenShiftRequired, http.StatusConflict, "OPEN_SALES_SHIFT_REQUIRED"),
	fixed(ErrCancellationChargeOutOfRange, http.StatusUnprocessableEntity, "CANCELLATION_CHARGE_OUT_OF_RANGE"),
	denied(ErrUnauthorized, http.StatusUnauthorized),
	denied(ErrSessionExpired, http.StatusUnauthorized),
	denied(ErrSessionRevoked, http.StatusUnauthorized),
	// Boundary validation failures (malformed request shape, out-of-range or
	// duplicate correction selections, a malformed PIN shape). The wrapped
	// detail is request input, safe to echo.
	{Target: response.ErrInvalid, Spec: response.ErrorSpec{Status: http.StatusBadRequest, Code: "INVALID_INPUT"}},
}

// handlerErrors is what the HTTP handlers write: the sentinels the shared
// httpx helpers raise come first, then the domain mapping. Anything neither
// matches falls through to response.Error, which logs it and answers the
// generic 500.
var handlerErrors = append(response.ErrorMapper{
	{Target: response.ErrInvalid, Spec: response.ErrorSpec{Status: http.StatusBadRequest, Code: "INVALID_INPUT"}},
	{Target: response.ErrUnauthorized, Spec: response.ErrorSpec{Status: http.StatusUnauthorized, Code: "UNAUTHORIZED"}},
}, httpErrors...)

// ErrorResponse maps a Preparation domain error to its HTTP status and the
// response.APIResponse body the handler writes. An error no rule matches is
// the generic 500.
func ErrorResponse(err error) (int, response.APIResponse) {
	coded, ok := httpErrors.Map(err).(*response.CodedError)
	if !ok {
		return http.StatusInternalServerError, errorBody("INTERNAL_ERROR", "an unexpected error occurred")
	}
	return coded.Status, errorBody(coded.Code, coded.Message)
}

func errorBody(code, message string) response.APIResponse {
	return response.APIResponse{
		Success: false,
		Error:   &response.APIError{Code: code, Message: message},
	}
}
