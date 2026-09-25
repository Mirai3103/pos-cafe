package catalog

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

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
	ID               uuid.UUID  `json:"id"`
	Name             string     `json:"name"`
	Retired          bool       `json:"retired"`
	RetiredAt        *time.Time `json:"retired_at,omitempty"`
	RetirementReason *string    `json:"retirement_reason,omitempty"`
	RetirementNote   *string    `json:"retirement_note,omitempty"`
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

// RenameItemCommand carries the parameters for renaming a menu item.
type RenameItemCommand struct {
	RequestID uuid.UUID `json:"request_id"`
	ItemID    uuid.UUID `json:"item_id"`
	Name      string    `json:"name"`
}

// RenameSizeCommand carries the parameters for renaming a menu item size.
type RenameSizeCommand struct {
	RequestID uuid.UUID `json:"request_id"`
	SizeID    uuid.UUID `json:"size_id"`
	Name      string    `json:"name"`
}

// RenameModifierGroupCommand carries the parameters for renaming a modifier group.
type RenameModifierGroupCommand struct {
	RequestID uuid.UUID `json:"request_id"`
	GroupID   uuid.UUID `json:"group_id"`
	Name      string    `json:"name"`
}

// RenameModifierOptionCommand carries the parameters for renaming a modifier option.
type RenameModifierOptionCommand struct {
	RequestID uuid.UUID `json:"request_id"`
	OptionID  uuid.UUID `json:"option_id"`
	Name      string    `json:"name"`
}

// RepriceItemCommand carries the parameters for repricing a menu item.
type RepriceItemCommand struct {
	RequestID  uuid.UUID `json:"request_id"`
	ItemID     uuid.UUID `json:"item_id"`
	PriceVND   int64     `json:"price_vnd"`
	ManagerPIN string    `json:"manager_pin"`
}

// RepriceSizeCommand carries the parameters for repricing a menu item size.
type RepriceSizeCommand struct {
	RequestID  uuid.UUID `json:"request_id"`
	SizeID     uuid.UUID `json:"size_id"`
	PriceVND   int64     `json:"price_vnd"`
	ManagerPIN string    `json:"manager_pin"`
}

// RepriceModifierOptionCommand carries the parameters for repricing a modifier option.
type RepriceModifierOptionCommand struct {
	RequestID    uuid.UUID `json:"request_id"`
	OptionID     uuid.UUID `json:"option_id"`
	SurchargeVND int64     `json:"surcharge_vnd"`
	ManagerPIN   string    `json:"manager_pin"`
}

// SetItemAvailabilityCommand carries the parameters for setting a menu item's availability.
type SetItemAvailabilityCommand struct {
	RequestID uuid.UUID `json:"request_id"`
	ItemID    uuid.UUID `json:"item_id"`
	Available bool      `json:"available"`
}

// SetSizeAvailabilityCommand carries the parameters for setting a menu item size's availability.
type SetSizeAvailabilityCommand struct {
	RequestID uuid.UUID `json:"request_id"`
	SizeID    uuid.UUID `json:"size_id"`
	Available bool      `json:"available"`
}

// SetModifierOptionAvailabilityCommand carries the parameters for setting a modifier option's availability.
type SetModifierOptionAvailabilityCommand struct {
	RequestID uuid.UUID `json:"request_id"`
	OptionID  uuid.UUID `json:"option_id"`
	Available bool      `json:"available"`
}

// RetireCategoryCommand carries the parameters for retiring a menu category.
type RetireCategoryCommand struct {
	RequestID  uuid.UUID `json:"request_id"`
	CategoryID uuid.UUID `json:"category_id"`
	Reason     string    `json:"reason"`
	Note       string    `json:"note,omitempty"`
}

// RetireItemCommand carries the parameters for retiring a menu item.
type RetireItemCommand struct {
	RequestID uuid.UUID `json:"request_id"`
	ItemID    uuid.UUID `json:"item_id"`
	Reason    string    `json:"reason"`
	Note      string    `json:"note,omitempty"`
}

// RetireSizeCommand carries the parameters for retiring a menu item size.
type RetireSizeCommand struct {
	RequestID uuid.UUID `json:"request_id"`
	SizeID    uuid.UUID `json:"size_id"`
	Reason    string    `json:"reason"`
	Note      string    `json:"note,omitempty"`
}

// RetireModifierGroupCommand carries the parameters for retiring a modifier group.
type RetireModifierGroupCommand struct {
	RequestID uuid.UUID `json:"request_id"`
	GroupID   uuid.UUID `json:"group_id"`
	Reason    string    `json:"reason"`
	Note      string    `json:"note,omitempty"`
}

// RetireModifierOptionCommand carries the parameters for retiring a modifier option.
type RetireModifierOptionCommand struct {
	RequestID uuid.UUID `json:"request_id"`
	OptionID  uuid.UUID `json:"option_id"`
	Reason    string    `json:"reason"`
	Note      string    `json:"note,omitempty"`
}

// === Sellable Menu Projection ===

// SellableModifierOptionResponse represents an available modifier option in the sellable menu.
type SellableModifierOptionResponse struct {
	ID           uuid.UUID `json:"id"`
	Name         string    `json:"name"`
	SurchargeVND int64     `json:"surcharge_vnd"`
}

// SellableModifierGroupResponse represents an effective modifier group in the sellable menu.
type SellableModifierGroupResponse struct {
	ID               uuid.UUID                        `json:"id"`
	Name             string                           `json:"name"`
	MinSelections    int32                            `json:"min_selections"`
	MaxSelections    int32                            `json:"max_selections"`
	Options          []SellableModifierOptionResponse `json:"options"`
	DefaultOptionIDs []uuid.UUID                      `json:"default_option_ids"`
}

// SellableSizeResponse represents an available size option in the sellable menu.
type SellableSizeResponse struct {
	ID       uuid.UUID `json:"id"`
	Name     string    `json:"name"`
	PriceVND int64     `json:"price_vnd"`
}

// SellableItemResponse represents a sellable menu item.
type SellableItemResponse struct {
	ID             uuid.UUID                       `json:"id"`
	CategoryID     uuid.UUID                       `json:"category_id"`
	Name           string                          `json:"name"`
	PriceVND       *int64                          `json:"price_vnd,omitempty"`
	Sizes          []SellableSizeResponse          `json:"sizes,omitempty"`
	ModifierGroups []SellableModifierGroupResponse `json:"modifier_groups,omitempty"`
}

// SellableCategoryResponse represents a category with at least one sellable item.
type SellableCategoryResponse struct {
	ID    uuid.UUID              `json:"id"`
	Name  string                 `json:"name"`
	Items []SellableItemResponse `json:"items"`
}

// SellableMenuResponse is the projection of sellable catalog items.
type SellableMenuResponse struct {
	Categories []SellableCategoryResponse `json:"categories"`
}

// === Management Menu Projection ===

// ManagementModifierOptionResponse represents a modifier option in the management projection.
type ManagementModifierOptionResponse struct {
	ID               uuid.UUID  `json:"id"`
	ModifierGroupID  uuid.UUID  `json:"modifier_group_id"`
	Name             string     `json:"name"`
	SurchargeVND     int64      `json:"surcharge_vnd"`
	Available        bool       `json:"available"`
	Retired          bool       `json:"retired"`
	RetiredAt        *time.Time `json:"retired_at,omitempty"`
	RetirementReason *string    `json:"retirement_reason,omitempty"`
	RetirementNote   *string    `json:"retirement_note,omitempty"`
}

// ManagementModifierGroupResponse represents a modifier group in the management projection.
type ManagementModifierGroupResponse struct {
	ID               uuid.UUID                          `json:"id"`
	Name             string                             `json:"name"`
	MinSelections    int32                              `json:"min_selections"`
	MaxSelections    int32                              `json:"max_selections"`
	Retired          bool                               `json:"retired"`
	RetiredAt        *time.Time                         `json:"retired_at,omitempty"`
	RetirementReason *string                            `json:"retirement_reason,omitempty"`
	RetirementNote   *string                            `json:"retirement_note,omitempty"`
	Options          []ManagementModifierOptionResponse `json:"options"`
	DefaultOptionIDs []uuid.UUID                        `json:"default_option_ids"`
}

// ManagementSizeResponse represents a size option in the management projection.
type ManagementSizeResponse struct {
	ID               uuid.UUID  `json:"id"`
	MenuItemID       uuid.UUID  `json:"menu_item_id"`
	Name             string     `json:"name"`
	PriceVND         int64      `json:"price_vnd"`
	Available        bool       `json:"available"`
	Retired          bool       `json:"retired"`
	RetiredAt        *time.Time `json:"retired_at,omitempty"`
	RetirementReason *string    `json:"retirement_reason,omitempty"`
	RetirementNote   *string    `json:"retirement_note,omitempty"`
}

// ManagementItemResponse represents a menu item in the management projection.
type ManagementItemResponse struct {
	ID                       uuid.UUID                         `json:"id"`
	CategoryID               uuid.UUID                         `json:"category_id"`
	Name                     string                            `json:"name"`
	PriceVND                 *int64                            `json:"price_vnd,omitempty"`
	Available                bool                              `json:"available"`
	Retired                  bool                              `json:"retired"`
	RetiredAt                *time.Time                        `json:"retired_at,omitempty"`
	RetirementReason         *string                           `json:"retirement_reason,omitempty"`
	RetirementNote           *string                           `json:"retirement_note,omitempty"`
	Sizes                    []ManagementSizeResponse          `json:"sizes,omitempty"`
	DirectModifierGroupIDs   []uuid.UUID                       `json:"direct_modifier_group_ids"`
	ExcludedModifierGroupIDs []uuid.UUID                       `json:"excluded_modifier_group_ids"`
	ModifierGroups           []ManagementModifierGroupResponse `json:"modifier_groups"`
}

// ManagementCategoryResponse represents a category in the management projection.
type ManagementCategoryResponse struct {
	ID               uuid.UUID                `json:"id"`
	Name             string                   `json:"name"`
	Retired          bool                     `json:"retired"`
	RetiredAt        *time.Time               `json:"retired_at,omitempty"`
	RetirementReason *string                  `json:"retirement_reason,omitempty"`
	RetirementNote   *string                  `json:"retirement_note,omitempty"`
	ModifierGroupIDs []uuid.UUID              `json:"modifier_group_ids"`
	Items            []ManagementItemResponse `json:"items"`
}

// ManagementMenuResponse is the projection of all catalog entities for management.
type ManagementMenuResponse struct {
	Categories []ManagementCategoryResponse `json:"categories"`
}

// === Availability Menu Projection ===

// AvailabilityModifierOptionResponse represents an option in the availability projection.
type AvailabilityModifierOptionResponse struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Available bool      `json:"available"`
}

// AvailabilityModifierGroupResponse represents a modifier group in the availability projection.
type AvailabilityModifierGroupResponse struct {
	ID            uuid.UUID                            `json:"id"`
	Name          string                               `json:"name"`
	MinSelections int32                                `json:"min_selections"`
	MaxSelections int32                                `json:"max_selections"`
	Options       []AvailabilityModifierOptionResponse `json:"options"`
}

// AvailabilitySizeResponse represents a size in the availability projection.
type AvailabilitySizeResponse struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Available bool      `json:"available"`
}

// AvailabilityItemResponse represents an item in the availability projection.
type AvailabilityItemResponse struct {
	ID             uuid.UUID                           `json:"id"`
	CategoryID     uuid.UUID                           `json:"category_id"`
	Name           string                              `json:"name"`
	Available      bool                                `json:"available"`
	Sizes          []AvailabilitySizeResponse          `json:"sizes,omitempty"`
	ModifierGroups []AvailabilityModifierGroupResponse `json:"modifier_groups,omitempty"`
}

// AvailabilityCategoryResponse represents a category in the availability projection.
type AvailabilityCategoryResponse struct {
	ID    uuid.UUID                  `json:"id"`
	Name  string                     `json:"name"`
	Items []AvailabilityItemResponse `json:"items"`
}

// AvailabilityMenuResponse is the price-free projection for managing availability.
type AvailabilityMenuResponse struct {
	Categories []AvailabilityCategoryResponse `json:"categories"`
}

// === Modifier Groups Management Projection ===

// ModifierGroupsManagementResponse is a slice of ManagementModifierGroupResponse.
type ModifierGroupsManagementResponse = []ManagementModifierGroupResponse

// === Audit Events Query ===

// AuditEventResponse is the API response representation of an audit event.
type AuditEventResponse struct {
	ID         uuid.UUID       `json:"id"`
	EventType  string          `json:"event_type"`
	ActorID    *uuid.UUID      `json:"actor_id,omitempty"`
	SessionID  *uuid.UUID      `json:"session_id,omitempty"`
	Details    json.RawMessage `json:"details" swaggertype:"object"`
	OccurredAt time.Time       `json:"occurred_at"`
}

// === HTTP Request Envelopes ===

// RenameRequest carries the request ID and new name for entity rename operations.
type RenameRequest struct {
	RequestID uuid.UUID `json:"request_id"`
	Name      string    `json:"name"`
}

// RepriceRequest carries the request ID, new price, and manager PIN for direct items and sizes.
type RepriceRequest struct {
	RequestID  uuid.UUID `json:"request_id"`
	PriceVND   int64     `json:"price_vnd"`
	ManagerPIN string    `json:"manager_pin"`
}

// RepriceModifierOptionRequest carries the request ID, new surcharge, and manager PIN for modifier options.
type RepriceModifierOptionRequest struct {
	RequestID    uuid.UUID `json:"request_id"`
	SurchargeVND int64     `json:"surcharge_vnd"`
	ManagerPIN   string    `json:"manager_pin"`
}

// SetAvailabilityRequest carries the request ID and target availability state.
type SetAvailabilityRequest struct {
	RequestID uuid.UUID `json:"request_id"`
	Available bool      `json:"available"`
}

// SetAvailabilityBatchRequest carries the request ID and a batch of availability changes.
type SetAvailabilityBatchRequest struct {
	RequestID uuid.UUID            `json:"request_id"`
	Changes   []AvailabilityChange `json:"changes"`
}

// SetAvailabilityBatchCommand carries the parameters for one batch availability change.
type SetAvailabilityBatchCommand struct {
	RequestID uuid.UUID            `json:"request_id"`
	Changes   []AvailabilityChange `json:"changes"`
}

// AvailabilityBatchResult reports the outcome for one requested entry.
type AvailabilityBatchResult struct {
	Kind      string    `json:"kind"`
	ID        uuid.UUID `json:"id"`
	Available bool      `json:"available"`
	Changed   bool      `json:"changed"`
}

// AvailabilityBatchResponse lists every requested entry in (kind, id) order.
type AvailabilityBatchResponse struct {
	Results []AvailabilityBatchResult `json:"results"`
}

// RetireRequest carries the request ID, reason, and optional note for permanent retirement.
type RetireRequest struct {
	RequestID uuid.UUID `json:"request_id"`
	Reason    string    `json:"reason"`
	Note      string    `json:"note"`
}

// SetModifierGroupDefaultsRequest carries the request ID and the explicit list of default option IDs.
type SetModifierGroupDefaultsRequest struct {
	RequestID uuid.UUID   `json:"request_id"`
	OptionIDs []uuid.UUID `json:"option_ids"`
}

// MutationRequest carries only the request ID for parameter-free mutation routes.
type MutationRequest struct {
	RequestID uuid.UUID `json:"request_id"`
}

// === Display Details (BA-1) ===

// PresentString records whether a JSON key was present, so a missing key and
// an explicit null are told apart.
type PresentString struct {
	Present bool
	Value   *string
}

// UnmarshalJSON marks the key present; null leaves Value nil.
func (p *PresentString) UnmarshalJSON(b []byte) error {
	p.Present = true
	if string(b) == "null" {
		p.Value = nil
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	p.Value = &s
	return nil
}

// SetItemDetailsRequest replaces all three display fields. Every key must be
// present; null clears.
type SetItemDetailsRequest struct {
	RequestID   uuid.UUID     `json:"request_id"`
	Code        PresentString `json:"code" swaggertype:"string" extensions:"x-nullable"`
	Badge       PresentString `json:"badge" swaggertype:"string" enums:"BEST_SELLER,HOT,NEW,SIGNATURE,CHEF_PICK" extensions:"x-nullable"`
	Description PresentString `json:"description" swaggertype:"string" extensions:"x-nullable"`
}

// SetItemDetailsCommand carries the parameters for setting item display fields.
type SetItemDetailsCommand struct {
	RequestID   uuid.UUID
	ItemID      uuid.UUID
	Code        *string
	Badge       *string
	Description *string
}

// ItemDetailsResponse is an item's display fields.
type ItemDetailsResponse struct {
	ItemID      uuid.UUID `json:"item_id"`
	Code        *string   `json:"code"`
	Badge       *string   `json:"badge"`
	Description *string   `json:"description"`
}

// SetCategoryDetailsRequest sets a category's icon and display order.
type SetCategoryDetailsRequest struct {
	RequestID    uuid.UUID `json:"request_id"`
	Icon         *string   `json:"icon"`
	DisplayOrder int32     `json:"display_order"`
}

// SetCategoryDetailsCommand carries the parameters for setting category display fields.
type SetCategoryDetailsCommand struct {
	RequestID    uuid.UUID
	CategoryID   uuid.UUID
	Icon         *string
	DisplayOrder int32
}

// CategoryDetailsResponse is a category's display fields.
type CategoryDetailsResponse struct {
	CategoryID   uuid.UUID `json:"category_id"`
	Icon         *string   `json:"icon"`
	DisplayOrder int32     `json:"display_order"`
}

// SetItemImageCommand carries the uploaded bytes for an item image.
type SetItemImageCommand struct {
	RequestID uuid.UUID
	ItemID    uuid.UUID
	Data      []byte
}

// ClearItemImageCommand removes an item's image.
type ClearItemImageCommand struct {
	RequestID uuid.UUID
	ItemID    uuid.UUID
}

// ItemImageResponse is an item's image URL, or null.
type ItemImageResponse struct {
	ItemID   uuid.UUID `json:"item_id"`
	ImageURL *string   `json:"image_url"`
}

// === Structure (BA-1) ===

// MoveItemCategoryRequest moves an item to another category.
type MoveItemCategoryRequest struct {
	RequestID  uuid.UUID `json:"request_id"`
	CategoryID uuid.UUID `json:"category_id"`
}

// MoveItemCategoryCommand carries the parameters for moving an item.
type MoveItemCategoryCommand struct {
	RequestID  uuid.UUID
	ItemID     uuid.UUID
	CategoryID uuid.UUID
}

// ItemCategoryResponse reports an item's category and the exclusions the move dropped.
type ItemCategoryResponse struct {
	ItemID                   uuid.UUID   `json:"item_id"`
	CategoryID               uuid.UUID   `json:"category_id"`
	RemovedExclusionGroupIDs []uuid.UUID `json:"removed_exclusion_group_ids"`
}
