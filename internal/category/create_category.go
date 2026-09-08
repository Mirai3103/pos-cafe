package category

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/labstack/echo/v4"
)

// === Command (Input Data) ===

type CreateCommand struct {
	Name         string `json:"name" validate:"required,min=2,max=100"`
	Description  string `json:"description" validate:"max=500"`
	DisplayOrder int64  `json:"display_order" validate:"gte=0"`
	IsActive     *bool  `json:"is_active"`
}

// === Event (Domain Event) ===

const TopicCategoryCreated = "category.created"

type CreatedEvent struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// === Dependency Boundary (Consumer-defined interface) ===

type categoryCreator interface {
	GetCategoryByName(ctx context.Context, name string) (sqlc.Category, error)
	CreateCategory(ctx context.Context, arg sqlc.CreateCategoryParams) (sqlc.Category, error)
}

// === Command Handler (Business Logic) ===

type CreateHandler struct {
	store     categoryCreator
	publisher EventPublisher
}

func NewCreateHandler(store categoryCreator, publisher EventPublisher) *CreateHandler {
	if publisher == nil {
		publisher = NoopPublisher{}
	}
	return &CreateHandler{store: store, publisher: publisher}
}

func (h *CreateHandler) Handle(ctx context.Context, cmd CreateCommand) (*Response, error) {
	// 1. Business rule: category names are unique (pre-check for a friendly 409)
	switch _, err := h.store.GetCategoryByName(ctx, cmd.Name); {
	case err == nil:
		return nil, fmt.Errorf("%w: category with name '%s'", response.ErrConflict, cmd.Name)
	case !errors.Is(err, sql.ErrNoRows):
		return nil, fmt.Errorf("check category uniqueness: %w", err)
	}

	// 2. Defaulting logic
	isActive := true
	if cmd.IsActive != nil {
		isActive = *cmd.IsActive
	}

	// 3. Persist (database mutation)
	category, err := h.store.CreateCategory(ctx, sqlc.CreateCategoryParams{
		Name:         cmd.Name,
		Description:  cmd.Description,
		DisplayOrder: cmd.DisplayOrder,
		IsActive:     isActive,
	})
	if err != nil {
		// The pre-check above can lose a race, so the unique violation
		// (SQLSTATE 23505) is the authoritative conflict signal.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, fmt.Errorf("%w: category with name '%s'", response.ErrConflict, cmd.Name)
		}
		return nil, fmt.Errorf("create category in database: %w", err)
	}

	res := toResponse(category)

	// 4. Emit the domain event; a failed publish must not fail the request
	if err := h.publisher.Publish(TopicCategoryCreated, CreatedEvent{
		ID:   res.ID,
		Name: res.Name,
	}); err != nil {
		slog.ErrorContext(ctx, "failed to publish category created event",
			"error", err,
			"category_id", res.ID,
			"topic", TopicCategoryCreated,
		)
	}

	return &res, nil
}

// === HTTP Transport Adapter ===

// HandleHTTP godoc
// @Summary Create a new category
// @Description Create a new drink/food category for POS Cafe
// @Tags Categories
// @Accept json
// @Produce json
// @Param request body CreateCommand true "Category payload"
// @Success 201 {object} response.APIResponse{data=Response}
// @Failure 400 {object} response.APIResponse
// @Failure 409 {object} response.APIResponse
// @Router /categories [post]
func (h *CreateHandler) HandleHTTP(c echo.Context) error {
	var cmd CreateCommand
	if err := c.Bind(&cmd); err != nil {
		return response.Error(c, fmt.Errorf("%w: invalid request body", response.ErrInvalid))
	}

	if err := c.Validate(&cmd); err != nil {
		return response.Error(c, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error()))
	}

	res, err := h.Handle(c.Request().Context(), cmd)
	if err != nil {
		return response.Error(c, err)
	}

	return response.Created(c, res)
}
