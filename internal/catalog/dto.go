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

// AttachItemModifierGroupCommand carries the parameters for attaching a modifier group to an item.
type AttachItemModifierGroupCommand struct {
	RequestID       uuid.UUID `json:"request_id"`
	ItemID          uuid.UUID `json:"item_id"`
	ModifierGroupID uuid.UUID `json:"modifier_group_id"`
}

// ItemModifierGroupResponse represents an item-to-modifier-group attachment response.
type ItemModifierGroupResponse struct {
	ItemID          uuid.UUID `json:"item_id"`
	ModifierGroupID uuid.UUID `json:"modifier_group_id"`
}

// AttachCategoryModifierGroupCommand carries the parameters for attaching a modifier group to a category.
type AttachCategoryModifierGroupCommand struct {
	RequestID       uuid.UUID `json:"request_id"`
	CategoryID      uuid.UUID `json:"category_id"`
	ModifierGroupID uuid.UUID `json:"modifier_group_id"`
}

// CategoryModifierGroupResponse represents a category-to-modifier-group attachment response.
type CategoryModifierGroupResponse struct {
	CategoryID      uuid.UUID `json:"category_id"`
	ModifierGroupID uuid.UUID `json:"modifier_group_id"`
}

// ExcludeInheritedModifierGroupCommand carries the parameters for excluding an inherited group from an item.
type ExcludeInheritedModifierGroupCommand struct {
	RequestID       uuid.UUID `json:"request_id"`
	ItemID          uuid.UUID `json:"item_id"`
	ModifierGroupID uuid.UUID `json:"modifier_group_id"`
}

// ItemModifierGroupExclusionResponse represents an inherited modifier group exclusion response.
type ItemModifierGroupExclusionResponse struct {
	ItemID          uuid.UUID `json:"item_id"`
	ModifierGroupID uuid.UUID `json:"modifier_group_id"`
}

// SetModifierGroupDefaultsCommand carries the parameters for replacing default options of a modifier group.
type SetModifierGroupDefaultsCommand struct {
	RequestID uuid.UUID   `json:"request_id"`
	GroupID   uuid.UUID   `json:"group_id"`
	OptionIDs []uuid.UUID `json:"option_ids"`
}

// ModifierGroupDefaultsResponse represents the default options response for a modifier group.
type ModifierGroupDefaultsResponse struct {
	GroupID   uuid.UUID   `json:"group_id"`
	OptionIDs []uuid.UUID `json:"option_ids"`
}
