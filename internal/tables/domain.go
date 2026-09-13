// Package tables implements the Tables vertical slice: the Table entity,
// its naming and Availability rules, and a read reporting current occupancy.
package tables

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Capabilities required for tables operations.
const (
	CapSalesOperate     = "sales.operate"
	CapTablesAdminister = "tables.administer"
)

// Idempotency action names. Stored in idempotency_keys.action (VARCHAR(50)).
const (
	OpCreateTable          = "tables.create_table"
	OpRenameTable          = "tables.rename_table"
	OpSetTableAvailability = "tables.set_table_availability"
)

// Audit event types.
const (
	EventTableCreated             = "TABLE_CREATED"
	EventTableRenamed             = "TABLE_RENAMED"
	EventTableAvailabilityChanged = "TABLE_AVAILABILITY_CHANGED"
	EventAuthorizationDenied      = "tables.authorization_denied"
)

// MaxTableNameLength is the inclusive upper bound on a normalized Table name,
// counted in Unicode code points to agree with the database char_length check.
const MaxTableNameLength = 60

// NormalizeTableName trims surrounding whitespace, collapses each run of
// internal whitespace to a single space, and returns the display form plus a
// Unicode-lowercase uniqueness key.
//
// This differs from catalog.NormalizeName, which preserves internal
// whitespace. Table names collapse it because "Ban  1" and "Ban 1" name the
// same physical location.
func NormalizeTableName(s string) (display, key string) {
	display = strings.Join(strings.Fields(s), " ")
	key = strings.ToLower(display)
	return display, key
}

// ValidateTableName checks a normalized display name against the canonical
// length bounds. Length is measured in Unicode code points, never bytes.
func ValidateTableName(display string) error {
	n := utf8.RuneCountInString(display)
	if n < 1 {
		return fmt.Errorf("table name cannot be empty")
	}
	if n > MaxTableNameLength {
		return fmt.Errorf("table name is %d characters, maximum is %d", n, MaxTableNameLength)
	}
	return nil
}
