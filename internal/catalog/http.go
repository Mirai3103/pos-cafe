package catalog

import (
	"context"

	"github.com/Mirai3103/pos-cafe/internal/platform/httpx"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

// commandBody is a JSON command body. Every one carries the idempotency
// request id the command pipeline keys on.
type commandBody interface {
	idempotencyKey() uuid.UUID
}

func (b CreateCategoryCommand) idempotencyKey() uuid.UUID                { return b.RequestID }
func (b CreateItemCommand) idempotencyKey() uuid.UUID                    { return b.RequestID }
func (b CreateModifierGroupCommand) idempotencyKey() uuid.UUID           { return b.RequestID }
func (b MutationRequest) idempotencyKey() uuid.UUID                      { return b.RequestID }
func (b RenameRequest) idempotencyKey() uuid.UUID                        { return b.RequestID }
func (b RetireRequest) idempotencyKey() uuid.UUID                        { return b.RequestID }
func (b RepriceRequest) idempotencyKey() uuid.UUID                       { return b.RequestID }
func (b RepriceModifierOptionRequest) idempotencyKey() uuid.UUID         { return b.RequestID }
func (b SetAvailabilityRequest) idempotencyKey() uuid.UUID               { return b.RequestID }
func (b SetAvailabilityBatchRequest) idempotencyKey() uuid.UUID          { return b.RequestID }
func (b SetModifierGroupDefaultsRequest) idempotencyKey() uuid.UUID      { return b.RequestID }
func (b SetItemDetailsRequest) idempotencyKey() uuid.UUID                { return b.RequestID }
func (b SetCategoryDetailsRequest) idempotencyKey() uuid.UUID            { return b.RequestID }
func (b MoveItemCategoryRequest) idempotencyKey() uuid.UUID              { return b.RequestID }
func (b AddSizeRequest) idempotencyKey() uuid.UUID                       { return b.RequestID }
func (b AddModifierOptionRequest) idempotencyKey() uuid.UUID             { return b.RequestID }
func (b SetSelectionRuleRequest) idempotencyKey() uuid.UUID              { return b.RequestID }
func (b ReplaceItemModifierGroupsRequest) idempotencyKey() uuid.UUID     { return b.RequestID }
func (b ReplaceCategoryModifierGroupsRequest) idempotencyKey() uuid.UUID { return b.RequestID }
func (b ReplaceGroupAssignmentsRequest) idempotencyKey() uuid.UUID       { return b.RequestID }

// sendError writes err through the Catalog error mapping.
func sendError(c echo.Context, err error) error {
	return httpx.SendError(c, err, MapHTTPError)
}

// bindCommand resolves the actor, parses the named UUID path parameters, binds
// the JSON body, and requires its request id. The steps run in that order, so
// a request with several faults always reports the same one.
func bindCommand[B commandBody](c echo.Context, params ...string) (Actor, []uuid.UUID, B, error) {
	var body B
	actor, err := httpx.Actor(c)
	if err != nil {
		return actor, nil, body, err
	}
	ids := make([]uuid.UUID, len(params))
	for i, name := range params {
		if ids[i], err = httpx.UUIDParam(c, name); err != nil {
			return actor, nil, body, err
		}
	}
	if body, err = httpx.BindBody[B](c); err != nil {
		return actor, nil, body, err
	}
	if err := httpx.RequireRequestID(body.idempotencyKey()); err != nil {
		return actor, nil, body, err
	}
	return actor, ids, body, nil
}

// sendCommand runs a command handler and writes its result with the status
// the handler chose (201 for a creation, 200 otherwise).
func sendCommand[C, R any](c echo.Context, actor Actor,
	handle func(context.Context, Actor, C) (int, R, error), cmd C,
) error {
	status, res, err := handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return httpx.SendResult(c, status, res)
}

// sendRead resolves the actor, runs a read handler, and writes its result.
func sendRead[R any](c echo.Context, handle func(context.Context, Actor) (R, error)) error {
	actor, err := httpx.Actor(c)
	if err != nil {
		return sendError(c, err)
	}
	res, err := handle(c.Request().Context(), actor)
	if err != nil {
		return sendError(c, err)
	}
	return response.OK(c, res)
}
