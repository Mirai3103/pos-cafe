package category

import (
	"time"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
)

type Response struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	DisplayOrder int64     `json:"display_order"`
	IsActive     bool      `json:"is_active"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func toResponse(c sqlc.Category) Response {
	return Response{
		ID:           c.ID,
		Name:         c.Name,
		Description:  c.Description,
		DisplayOrder: c.DisplayOrder,
		IsActive:     c.IsActive == 1,
		CreatedAt:    c.CreatedAt,
		UpdatedAt:    c.UpdatedAt,
	}
}
