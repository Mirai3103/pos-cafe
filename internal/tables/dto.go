package tables

import "github.com/google/uuid"

// CreateTableCommand creates a new Table.
type CreateTableCommand struct {
	RequestID uuid.UUID `json:"request_id"`
	Name      string    `json:"name"`
}

// RenameTableCommand renames an existing Table. TableID comes from the route.
type RenameTableCommand struct {
	RequestID uuid.UUID `json:"request_id"`
	TableID   uuid.UUID `json:"-"`
	Name      string    `json:"name"`
}

// SetTableAvailabilityCommand changes a Table's Availability.
// TableID comes from the route.
type SetTableAvailabilityCommand struct {
	RequestID uuid.UUID `json:"request_id"`
	TableID   uuid.UUID `json:"-"`
	Available bool      `json:"available"`
}

// TableResponse is the API representation of a Table. It is the result of all
// three commands and the base of each overview row.
type TableResponse struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Available bool      `json:"available"`
}

// TableOccupant identifies one active Service Session seated at a Table.
// It carries exactly these two fields; no other Sales detail crosses the
// Tables boundary.
type TableOccupant struct {
	ServiceSessionID uuid.UUID `json:"service_session_id"`
	ServiceNumber    string    `json:"service_number"`
}

// TableOverviewRow is one Table plus its current occupants.
type TableOverviewRow struct {
	TableResponse
	CurrentServiceSessions []TableOccupant `json:"current_service_sessions"`
}
