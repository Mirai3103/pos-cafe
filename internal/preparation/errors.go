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
	case errors.Is(err, ErrForbidden):
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
