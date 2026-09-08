package category

import (
	"github.com/labstack/echo/v4"
)

// CategoryStore bundles the storage operations needed by all category slices.
// sqlc.Queries implements this interface implicitly.
type CategoryStore interface {
	categoryCreator
	categoryGetter
	categoryLister
	categoryUpdater
	categoryDeleter
}

// EventPublisher decouples slices from the concrete event bus implementation.
type EventPublisher interface {
	Publish(topic string, payload any) error
}

// NoopPublisher is a null object pattern implementation of EventPublisher.
type NoopPublisher struct{}

func (NoopPublisher) Publish(topic string, payload any) error {
	return nil
}

type Slices struct {
	Create *CreateHandler
	Get    *GetByIDHandler
	List   *ListHandler
	Update *UpdateHandler
	Delete *DeleteHandler
}

func NewSlices(store CategoryStore, publisher EventPublisher) *Slices {
	if publisher == nil {
		publisher = NoopPublisher{}
	}
	return &Slices{
		Create: NewCreateHandler(store, publisher),
		Get:    NewGetByIDHandler(store),
		List:   NewListHandler(store),
		Update: NewUpdateHandler(store),
		Delete: NewDeleteHandler(store),
	}
}

func (s *Slices) RegisterRoutes(g *echo.Group) {
	categories := g.Group("/categories")
	categories.POST("", s.Create.HandleHTTP)
	categories.GET("", s.List.HandleHTTP)
	categories.GET("/:id", s.Get.HandleHTTP)
	categories.PUT("/:id", s.Update.HandleHTTP)
	categories.DELETE("/:id", s.Delete.HandleHTTP)
}
