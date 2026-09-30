// Package httpx holds the Echo handler helpers every vertical slice shares:
// resolving the acting staff member, parsing path parameters and bodies, and
// writing results and mapped errors in the response envelope.
package httpx

import (
	"fmt"
	"net/http"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/platform/command"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

// Actor returns the authenticated staff member from the claims RequireAuth
// placed on the context.
func Actor(c echo.Context) (command.Actor, error) {
	claims := auth.GetStaff(c)
	if claims == nil {
		return command.Actor{}, fmt.Errorf("%w: unauthorized", response.ErrUnauthorized)
	}
	return command.Actor{StaffID: claims.StaffID, SessionID: claims.SessionID}, nil
}

// UUIDParam parses the named path parameter as a UUID.
func UUIDParam(c echo.Context, name string) (uuid.UUID, error) {
	val := c.Param(name)
	id, err := uuid.Parse(val)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: invalid %s UUID: %s", response.ErrInvalid, name, val)
	}
	return id, nil
}

// BindBody binds the request body into a new T.
func BindBody[T any](c echo.Context) (T, error) {
	var body T
	if err := c.Bind(&body); err != nil {
		return body, fmt.Errorf("%w: invalid request body: %s", response.ErrInvalid, err.Error())
	}
	return body, nil
}

// RequireRequestID rejects a missing idempotency request id.
func RequireRequestID(id uuid.UUID) error {
	if id == uuid.Nil {
		return fmt.Errorf("%w: request_id is required", response.ErrInvalid)
	}
	return nil
}

// SendResult writes data as 201 Created when status is 201 and as 200 OK
// otherwise.
func SendResult(c echo.Context, status int, data any) error {
	if status == http.StatusCreated {
		return response.Created(c, data)
	}
	return response.OK(c, data)
}

// SendError writes err after mapping it through the slice's mapper, which
// turns slice sentinels into *response.CodedError and leaves anything else
// for response.Error's shared handling. A nil mapper sends err as is.
func SendError(c echo.Context, err error, mapErr func(error) error) error {
	if mapErr != nil {
		err = mapErr(err)
	}
	return response.Error(c, err)
}
