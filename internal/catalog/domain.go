package catalog

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Capabilities required for catalog operations.
const (
	CapAdministerStructure = "catalog.administer_structure"
	CapChangePrice         = "catalog.change_price"
	CapViewPrices          = "catalog.view_prices"
	CapManageAvailability  = "catalog.manage_availability"
	CapAuditInspect        = "audit.inspect"
)

// Operations for catalog mutations.
const (
	OpCategoryCreate                    = "catalog.category.create"
	OpCategoryRename                    = "catalog.category.rename"
	OpItemCreate                        = "catalog.item.create"
	OpItemRename                        = "catalog.item.rename"
	OpSizeRename                        = "catalog.size.rename"
	OpModifierGroupCreate               = "catalog.modifier_group.create"
	OpModifierGroupRename               = "catalog.modifier_group.rename"
	OpModifierOptionRename              = "catalog.modifier_option.rename"
	OpItemAttachModifierGroup           = "catalog.item.attach_modifier_group"
	OpCategoryAttachModifierGroup       = "catalog.category.attach_modifier_group"
	OpItemExcludeInheritedModifierGroup = "catalog.item.exclude_inherited_modifier_group"
	OpModifierGroupSetDefaults          = "catalog.modifier_group.set_defaults"
	OpItemReprice                       = "catalog.item.reprice"
	OpSizeReprice                       = "catalog.size.reprice"
	OpModifierOptionReprice             = "catalog.modifier_option.reprice"
	OpItemSetAvailability               = "catalog.item.set_availability"
	OpSizeSetAvailability               = "catalog.size.set_availability"
	OpModifierOptionSetAvailability     = "catalog.modifier_option.set_availability"
	OpCategoryRetire                    = "catalog.category.retire"
	OpItemRetire                        = "catalog.item.retire"
	OpSizeRetire                        = "catalog.size.retire"
	OpModifierGroupRetire               = "catalog.modifier_group.retire"
	OpModifierOptionRetire              = "catalog.modifier_option.retire"
)

// Audit event types emitted by catalog operations.
const (
	EventCategoryCreated                    = "catalog.category.created"
	EventCategoryRenamed                    = "catalog.category.renamed"
	EventItemCreated                        = "catalog.item.created"
	EventItemRenamed                        = "catalog.item.renamed"
	EventSizeRenamed                        = "catalog.size.renamed"
	EventModifierGroupCreated               = "catalog.modifier_group.created"
	EventModifierGroupRenamed               = "catalog.modifier_group.renamed"
	EventModifierOptionRenamed              = "catalog.modifier_option.renamed"
	EventItemModifierGroupAttached          = "catalog.item.modifier_group_attached"
	EventCategoryModifierGroupAttached      = "catalog.category.modifier_group_attached"
	EventItemInheritedModifierGroupExcluded = "catalog.item.inherited_modifier_group_excluded"
	EventModifierGroupDefaultsChanged       = "catalog.modifier_group.defaults_changed"
	EventItemRepriced                       = "catalog.item.repriced"
	EventSizeRepriced                       = "catalog.size.repriced"
	EventModifierOptionRepriced             = "catalog.modifier_option.repriced"
	EventItemAvailabilityChanged            = "catalog.item.availability_changed"
	EventSizeAvailabilityChanged            = "catalog.size.availability_changed"
	EventModifierOptionAvailabilityChanged  = "catalog.modifier_option.availability_changed"
	EventCategoryRetired                    = "catalog.category.retired"
	EventItemRetired                        = "catalog.item.retired"
	EventSizeRetired                        = "catalog.size.retired"
	EventModifierGroupRetired               = "catalog.modifier_group.retired"
	EventModifierOptionRetired              = "catalog.modifier_option.retired"
	EventAuthorizationDenied                = "catalog.authorization_denied"
	EventPrefixCatalog                      = "catalog."
)

// Retirement holds optional retirement metadata.
type Retirement struct {
	Reason string
	Note   string
}

// EffectiveGroup carries a group ID with its minimum selection requirement.
type EffectiveGroup struct {
	ID            uuid.UUID
	MinSelections int32
}

// ItemState describes the current projection facts needed for sellability.
type ItemState struct {
	Available             bool
	HasDirectPrice        bool
	AvailableSizeCount    int
	EffectiveGroups       []EffectiveGroup
	AvailableOptionCounts map[uuid.UUID]int
}

var validRetirementReasons = map[string]bool{
	"NO_LONGER_OFFERED": true,
	"MENU_RESTRUCTURE":  true,
	"OTHER":             true,
}

// NormalizeName trims surrounding whitespace and returns the display form
// and a Unicode-lowercase key. Internal whitespace is preserved.
func NormalizeName(s string) (display, key string) {
	display = strings.TrimSpace(s)
	key = strings.ToLower(display)
	return display, key
}

// ValidatePrice checks that price is in range [1, 2_147_483_647].
func ValidatePrice(price int64) error {
	if price < 1 || price > 2_147_483_647 {
		return fmt.Errorf("price %d is out of range [1, 2147483647]", price)
	}
	return nil
}

// ValidateSurcharge checks that surcharge is in range [0, 2_147_483_647].
func ValidateSurcharge(surcharge int64) error {
	if surcharge < 0 || surcharge > 2_147_483_647 {
		return fmt.Errorf("surcharge %d is out of range [0, 2147483647]", surcharge)
	}
	return nil
}

// ValidateRetirement checks that the retirement value is valid.
// A nil Retirement is valid (no retirement).
// OTHER requires a non-empty trimmed note; any note is capped at 500 runes.
func ValidateRetirement(r Retirement) error {
	if r.Reason == "" {
		return nil
	}
	if !validRetirementReasons[r.Reason] {
		return fmt.Errorf("unknown retirement reason %q", r.Reason)
	}
	note := strings.TrimSpace(r.Note)
	if r.Reason == "OTHER" && note == "" {
		return fmt.Errorf("OTHER retirement reason requires a non-empty note")
	}
	if utf8.RuneCountInString(note) > 500 {
		return fmt.Errorf("retirement note must be at most 500 characters")
	}
	return nil
}

// EffectiveGroupIDs computes the effective set of modifier groups as
// (inherited - exclusions) + direct, deduplicated.
func EffectiveGroupIDs(inheritedGroups, excludedGroupIDs, directGroups []uuid.UUID) []uuid.UUID {
	excluded := make(map[uuid.UUID]bool, len(excludedGroupIDs))
	for _, id := range excludedGroupIDs {
		excluded[id] = true
	}

	seen := make(map[uuid.UUID]bool, len(inheritedGroups)+len(directGroups))
	var result []uuid.UUID

	for _, id := range inheritedGroups {
		if excluded[id] {
			continue
		}
		if !seen[id] {
			seen[id] = true
			result = append(result, id)
		}
	}

	for _, id := range directGroups {
		if !seen[id] {
			seen[id] = true
			result = append(result, id)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].String() < result[j].String()
	})

	return result
}

// IsSellable determines whether an item can currently be sold based on its
// projection state. The item must be available, have exactly one valid pricing
// form, have at least one available size if sized, and every required group
// must have at least min available options.
func IsSellable(state ItemState) bool {
	if !state.Available {
		return false
	}

	if !state.HasDirectPrice && state.AvailableSizeCount == 0 {
		return false
	}

	if state.HasDirectPrice && state.AvailableSizeCount > 0 {
		return false
	}

	for _, g := range state.EffectiveGroups {
		if g.MinSelections > 0 {
			if state.AvailableOptionCounts[g.ID] < int(g.MinSelections) {
				return false
			}
		}
	}

	return true
}
