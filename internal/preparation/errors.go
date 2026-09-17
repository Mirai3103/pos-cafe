package preparation

import (
	"errors"
	"net/http"

	"github.com/Mirai3103/pos-cafe/internal/response"
)

var (
	ErrUnitNotFound        = errors.New("preparation unit not found")
	ErrInvalidTransition   = errors.New("not a legal advance from the unit's current state")
	ErrRequestConflict     = errors.New("request conflict")
	ErrInvalidStoredResult = errors.New("invalid stored result")
	ErrForbidden           = errors.New("forbidden")
	ErrUnauthorized        = errors.New("unauthorized")
	ErrSessionExpired      = errors.New("staff access session expired")
	ErrSessionRevoked      = errors.New("staff access session revoked")

	// Phase 6B (Preparation Corrections & Recovery). Each expected condition
	// has its own sentinel so HTTP mapping never inspects error strings.
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
)

// ErrorResponse maps a Preparation domain error to its HTTP status and the
// response.APIResponse body the handler writes.
//
// The INVALID_STORED_RESULT and denial rows are copied verbatim from
// internal/sales/errors.go, including the collapsed NOT_AUTHORIZED code: the
// API must never disclose which denial condition failed. The reason is in the
// server log and the audit event. The session sentinels ride the same denial
// row, so a session-expiry or revocation denial can never surface as a
// generic 500.
//
// Phase 6B adds stable rows for the correction conditions (alerts, Waste,
// Remake, State Correction) and folds ErrInvalidManagerPIN into the existing
// 403 denial row. Boundary validation errors ride response.ErrInvalid, which
// sendError maps to 400 INVALID_INPUT; the row is repeated here so this
// mapping stays complete without its caller. Wrapped detail — including
// PostgreSQL error detail — never reaches the client; callers log it
// server-side.
func ErrorResponse(err error) (int, response.APIResponse) {
	// The client message is always the stable sentinel text, never the wrapped
	// PostgreSQL detail, which must stay server-side only.
	coded := func(_ int, code string, sentinel error) response.APIResponse {
		return response.APIResponse{
			Success: false,
			Error: &response.APIError{
				Code:    code,
				Message: sentinel.Error(),
			},
		}
	}

	switch {
	case errors.Is(err, ErrUnitNotFound):
		return http.StatusNotFound, coded(http.StatusNotFound, "PREPARATION_UNIT_NOT_FOUND", ErrUnitNotFound)
	case errors.Is(err, ErrInvalidTransition):
		return http.StatusConflict, coded(http.StatusConflict, "INVALID_TRANSITION", ErrInvalidTransition)
	case errors.Is(err, ErrRequestConflict):
		return http.StatusConflict, response.APIResponse{
			Success: false,
			Error: &response.APIError{
				Code:    "REQUEST_CONFLICT",
				Message: err.Error(),
			},
		}
	case errors.Is(err, ErrInvalidStoredResult):
		return http.StatusInternalServerError, response.APIResponse{
			Success: false,
			Error: &response.APIError{
				Code:    "INVALID_STORED_RESULT",
				Message: "an unexpected error occurred",
			},
		}
	// --- Phase 6B (Preparation Corrections & Recovery) ---
	case errors.Is(err, ErrAlertNotFound):
		return http.StatusNotFound, coded(http.StatusNotFound, "PREPARATION_ALERT_NOT_FOUND", ErrAlertNotFound)
	case errors.Is(err, ErrWasteNotFound):
		return http.StatusNotFound, coded(http.StatusNotFound, "PREPARATION_WASTE_NOT_FOUND", ErrWasteNotFound)
	case errors.Is(err, ErrInvalidReason):
		return http.StatusBadRequest, coded(http.StatusBadRequest, "INVALID_PREPARATION_REASON", ErrInvalidReason)
	case errors.Is(err, ErrInvalidNote):
		return http.StatusBadRequest, coded(http.StatusBadRequest, "INVALID_PREPARATION_NOTE", ErrInvalidNote)
	case errors.Is(err, ErrAlertAlreadyAcknowledged):
		return http.StatusConflict, coded(http.StatusConflict, "PREPARATION_ALERT_ALREADY_ACKNOWLEDGED", ErrAlertAlreadyAcknowledged)
	case errors.Is(err, ErrWasteAlreadyRemade):
		return http.StatusConflict, coded(http.StatusConflict, "PREPARATION_WASTE_ALREADY_REMADE", ErrWasteAlreadyRemade)
	case errors.Is(err, ErrServiceSessionClosed):
		return http.StatusConflict, coded(http.StatusConflict, "SERVICE_SESSION_ALREADY_CLOSED", ErrServiceSessionClosed)
	case errors.Is(err, ErrForbidden), errors.Is(err, ErrInvalidManagerPIN):
		return http.StatusForbidden, response.APIResponse{
			Success: false,
			Error: &response.APIError{
				Code:    "NOT_AUTHORIZED",
				Message: "not authorized",
			},
		}
	case errors.Is(err, ErrUnauthorized), errors.Is(err, ErrSessionExpired), errors.Is(err, ErrSessionRevoked):
		return http.StatusUnauthorized, response.APIResponse{
			Success: false,
			Error: &response.APIError{
				Code:    "NOT_AUTHORIZED",
				Message: "not authorized",
			},
		}
	case errors.Is(err, response.ErrInvalid):
		// Boundary validation failures (malformed UUIDs or request shape,
		// out-of-range or duplicate correction selections, a missing or
		// malformed PIN shape) mirror the INVALID_INPUT row sendError writes
		// for response.ErrInvalid, so this mapping stays complete on its
		// own. The wrapped detail is request input, safe to echo.
		return http.StatusBadRequest, response.APIResponse{
			Success: false,
			Error: &response.APIError{
				Code:    "INVALID_INPUT",
				Message: err.Error(),
			},
		}
	default:
		return http.StatusInternalServerError, response.APIResponse{
			Success: false,
			Error: &response.APIError{
				Code:    "INTERNAL_ERROR",
				Message: "an unexpected error occurred",
			},
		}
	}
}
