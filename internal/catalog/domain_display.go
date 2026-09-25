package catalog

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Menu Item badges (ADR-058). The web maps each code to its label and color.
const (
	BadgeBestSeller = "BEST_SELLER"
	BadgeHot        = "HOT"
	BadgeNew        = "NEW"
	BadgeSignature  = "SIGNATURE"
	BadgeChefPick   = "CHEF_PICK"
)

// Limits on display fields and replace-set commands.
const (
	MaxDescriptionRunes = 300
	MaxDisplayOrder     = 9999
	MaxAssignmentIDs    = 500
)

var (
	itemCodePattern     = regexp.MustCompile(`^[a-z0-9]{1,12}$`)
	categoryIconPattern = regexp.MustCompile(`^[a-z0-9-]{1,40}$`)
	validBadges         = map[string]bool{
		BadgeBestSeller: true, BadgeHot: true, BadgeNew: true, BadgeSignature: true, BadgeChefPick: true,
	}
)

// NormalizeItemCode trims a Menu Item code and returns its display form and its
// lowercase uniqueness key. A nil or blank code clears it.
func NormalizeItemCode(code *string) (display, key sql.NullString, err error) {
	if code == nil {
		return sql.NullString{}, sql.NullString{}, nil
	}
	d := strings.TrimSpace(*code)
	if d == "" {
		return sql.NullString{}, sql.NullString{}, nil
	}
	k := strings.ToLower(d)
	if !itemCodePattern.MatchString(k) {
		return sql.NullString{}, sql.NullString{}, fmt.Errorf("code %q must be 1 to 12 letters or digits", d)
	}
	return sql.NullString{String: d, Valid: true}, sql.NullString{String: k, Valid: true}, nil
}

// NormalizeBadge accepts nil (no badge) or one of the Badge* codes.
func NormalizeBadge(badge *string) (sql.NullString, error) {
	if badge == nil {
		return sql.NullString{}, nil
	}
	if !validBadges[*badge] {
		return sql.NullString{}, fmt.Errorf("unknown badge %q", *badge)
	}
	return sql.NullString{String: *badge, Valid: true}, nil
}

// NormalizeDescription trims a description; blank clears it.
func NormalizeDescription(desc *string) (sql.NullString, error) {
	if desc == nil {
		return sql.NullString{}, nil
	}
	d := strings.TrimSpace(*desc)
	if d == "" {
		return sql.NullString{}, nil
	}
	if utf8.RuneCountInString(d) > MaxDescriptionRunes {
		return sql.NullString{}, fmt.Errorf("description must be at most %d characters", MaxDescriptionRunes)
	}
	return sql.NullString{String: d, Valid: true}, nil
}

// NormalizeCategoryIcon accepts nil (no icon) or a Lucide icon name.
func NormalizeCategoryIcon(icon *string) (sql.NullString, error) {
	if icon == nil {
		return sql.NullString{}, nil
	}
	if !categoryIconPattern.MatchString(*icon) {
		return sql.NullString{}, fmt.Errorf("icon %q must match %s", *icon, categoryIconPattern)
	}
	return sql.NullString{String: *icon, Valid: true}, nil
}

// ValidateDisplayOrder checks a category display order.
func ValidateDisplayOrder(order int32) error {
	if order < 0 || order > MaxDisplayOrder {
		return fmt.Errorf("display_order %d is out of range [0, %d]", order, MaxDisplayOrder)
	}
	return nil
}

// ValidateSelectionRule checks a modifier group's bounds against its active
// (non-retired) option count and its default option count.
func ValidateSelectionRule(minSel, maxSel int32, activeOptions, defaults int) error {
	if minSel < 0 || maxSel < 1 || minSel > maxSel {
		return fmt.Errorf("invalid min/max selections bounds (%d, %d)", minSel, maxSel)
	}
	if int(maxSel) > activeOptions {
		return fmt.Errorf("max selections %d exceeds active option count %d", maxSel, activeOptions)
	}
	if defaults < int(minSel) || defaults > int(maxSel) {
		return fmt.Errorf("default options count %d must be between min %d and max %d", defaults, minSel, maxSel)
	}
	return nil
}

// NormalizeIDSet validates a replace-set list and returns a sorted copy. The
// sort makes the same set fingerprint alike in any order.
func NormalizeIDSet(ids []uuid.UUID, field string) ([]uuid.UUID, error) {
	if len(ids) > MaxAssignmentIDs {
		return nil, fmt.Errorf("%s must hold at most %d ids", field, MaxAssignmentIDs)
	}
	seen := make(map[uuid.UUID]struct{}, len(ids))
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id == uuid.Nil {
			return nil, fmt.Errorf("%s contains an empty id", field)
		}
		if _, dup := seen[id]; dup {
			return nil, fmt.Errorf("%s contains %s twice", field, id)
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sortUUIDs(out)
	return out, nil
}

// DiffIDSets returns what desired adds to and removes from current.
func DiffIDSets(current, desired []uuid.UUID) (added, removed []uuid.UUID) {
	added, removed = []uuid.UUID{}, []uuid.UUID{}
	for _, id := range desired {
		if !ContainsID(current, id) {
			added = append(added, id)
		}
	}
	for _, id := range current {
		if !ContainsID(desired, id) {
			removed = append(removed, id)
		}
	}
	sortUUIDs(added)
	sortUUIDs(removed)
	return added, removed
}

// UnionIDs merges sets into one sorted, deduplicated list.
func UnionIDs(sets ...[]uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]struct{}{}
	out := []uuid.UUID{}
	for _, set := range sets {
		for _, id := range set {
			if _, dup := seen[id]; !dup {
				seen[id] = struct{}{}
				out = append(out, id)
			}
		}
	}
	sortUUIDs(out)
	return out
}

// ContainsID reports whether id is in ids.
func ContainsID(ids []uuid.UUID, id uuid.UUID) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

func nullStringPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	s := ns.String
	return &s
}
