package catalog

import "github.com/google/uuid"

// CreateCategoryCommand carries the request ID and name for category creation.
type CreateCategoryCommand struct {
	RequestID uuid.UUID `json:"request_id"`
	Name      string    `json:"name"`
}

// RenameCategoryCommand carries the request ID, category ID, and new name for category rename.
type RenameCategoryCommand struct {
	RequestID  uuid.UUID `json:"request_id"`
	CategoryID uuid.UUID `json:"category_id"`
	Name       string    `json:"name"`
}

// CategoryResponse is the API response representation of a menu category.
type CategoryResponse struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// CreateSizeInput describes a size to create with a sized menu item.
type CreateSizeInput struct {
	Name     string `json:"name"`
	PriceVND int64  `json:"price_vnd"`
}

// CreateItemCommand carries the parameters for direct or sized item creation.
type CreateItemCommand struct {
	RequestID  uuid.UUID         `json:"request_id"`
	CategoryID uuid.UUID         `json:"category_id"`
	Name       string            `json:"name"`
	PriceVND   *int64            `json:"price_vnd,omitempty"`
	Sizes      []CreateSizeInput `json:"sizes,omitempty"`
	ManagerPIN string            `json:"manager_pin"`
}

// SizeResponse represents a size option within an item response.
type SizeResponse struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	PriceVND  int64     `json:"price_vnd"`
	Available bool      `json:"available"`
}

// ItemResponse is the API response representation of a menu item.
type ItemResponse struct {
	ID         uuid.UUID      `json:"id"`
	CategoryID uuid.UUID      `json:"category_id"`
	Name       string         `json:"name"`
	PriceVND   *int64         `json:"price_vnd,omitempty"`
	Available  bool           `json:"available"`
	Sizes      []SizeResponse `json:"sizes,omitempty"`
}

// CreateModifierOptionInput describes an option to create with a modifier group.
type CreateModifierOptionInput struct {
	Name         string `json:"name"`
	SurchargeVND int64  `json:"surcharge_vnd"`
}

// CreateModifierGroupCommand carries the parameters for modifier group creation.
type CreateModifierGroupCommand struct {
	RequestID          uuid.UUID                   `json:"request_id"`
	Name               string                      `json:"name"`
	MinSelections      int32                       `json:"min_selections"`
	MaxSelections      int32                       `json:"max_selections"`
	Options            []CreateModifierOptionInput `json:"options"`
	DefaultOptionNames []string                    `json:"default_option_names,omitempty"`
	ManagerPIN         string                      `json:"manager_pin"`
}

// ModifierOptionResponse represents an option in a modifier group response.
type ModifierOptionResponse struct {
	ID              uuid.UUID `json:"id"`
	ModifierGroupID uuid.UUID `json:"modifier_group_id"`
	Name            string    `json:"name"`
	SurchargeVND    int64     `json:"surcharge_vnd"`
	Available       bool      `json:"available"`
}

// ModifierGroupResponse is the API response representation of a modifier group.
type ModifierGroupResponse struct {
	ID               uuid.UUID                `json:"id"`
	Name             string                   `json:"name"`
	MinSelections    int32                    `json:"min_selections"`
	MaxSelections    int32                    `json:"max_selections"`
	Options          []ModifierOptionResponse `json:"options"`
	DefaultOptionIDs []uuid.UUID              `json:"default_option_ids"`
}
