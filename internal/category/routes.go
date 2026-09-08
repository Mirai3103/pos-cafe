package category

import (
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/eventbus"
	"github.com/labstack/echo/v4"
)

type Slices struct {
	Create *CreateHandler
	Get    *GetByIDHandler
	List   *ListHandler
	Update *UpdateHandler
	Delete *DeleteHandler
}

func NewSlices(queries *sqlc.Queries, bus *eventbus.Bus) *Slices {
	return &Slices{
		Create: NewCreateHandler(queries, bus),
		Get:    NewGetByIDHandler(queries),
		List:   NewListHandler(queries),
		Update: NewUpdateHandler(queries),
		Delete: NewDeleteHandler(queries),
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
