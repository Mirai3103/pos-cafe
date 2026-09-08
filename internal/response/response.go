package response

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v4"
)

var (
	ErrNotFound = errors.New("resource not found")
	ErrConflict = errors.New("resource already exists")
	ErrInvalid  = errors.New("invalid input data")
)

type APIResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   *APIError   `json:"error,omitempty"`
}

type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// OK sends a 200 OK success response.
func OK(c echo.Context, data interface{}) error {
	return c.JSON(http.StatusOK, APIResponse{
		Success: true,
		Data:    data,
	})
}

// Created sends a 201 Created success response.
func Created(c echo.Context, data interface{}) error {
	return c.JSON(http.StatusCreated, APIResponse{
		Success: true,
		Data:    data,
	})
}

// NoContent sends a 204 No Content response.
func NoContent(c echo.Context) error {
	return c.NoContent(http.StatusNoContent)
}

// Error maps errors to appropriate HTTP status codes and JSON payloads.
func Error(c echo.Context, err error) error {
	if err == nil {
		return nil
	}

	switch {
	case errors.Is(err, ErrNotFound):
		return c.JSON(http.StatusNotFound, APIResponse{
			Success: false,
			Error: &APIError{
				Code:    "NOT_FOUND",
				Message: err.Error(),
			},
		})
	case errors.Is(err, ErrConflict):
		return c.JSON(http.StatusConflict, APIResponse{
			Success: false,
			Error: &APIError{
				Code:    "CONFLICT",
				Message: err.Error(),
			},
		})
	case errors.Is(err, ErrInvalid):
		return c.JSON(http.StatusBadRequest, APIResponse{
			Success: false,
			Error: &APIError{
				Code:    "BAD_REQUEST",
				Message: err.Error(),
			},
		})
	default:
		slog.Error("internal server error", "error", err, "path", c.Path())
		return c.JSON(http.StatusInternalServerError, APIResponse{
			Success: false,
			Error: &APIError{
				Code:    "INTERNAL_ERROR",
				Message: "an unexpected error occurred",
			},
		})
	}
}
