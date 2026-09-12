package tables

import (
	"context"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
)

type createTableFingerprint struct {
	Name string `json:"name"`
}

type renameTableFingerprint struct {
	TableID uuid.UUID `json:"table_id"`
	Name    string    `json:"name"`
}

type setAvailabilityFingerprint struct {
	TableID   uuid.UUID `json:"table_id"`
	Available bool      `json:"available"`
}

type tableCreatedAuditDetails struct {
	TableID uuid.UUID `json:"table_id"`
	Name    string    `json:"name"`
}

type tableRenamedAuditDetails struct {
	TableID    uuid.UUID `json:"table_id"`
	BeforeName string    `json:"before_name"`
	AfterName  string    `json:"after_name"`
}

type tableAvailabilityAuditDetails struct {
	TableID         uuid.UUID `json:"table_id"`
	BeforeAvailable bool      `json:"before_available"`
	AfterAvailable  bool      `json:"after_available"`
}

func toTableResponse(row sqlc.Table) TableResponse {
	return TableResponse{ID: row.ID, Name: row.Name, Available: row.Available}
}

// CreateTableHandler handles Table creation.
type CreateTableHandler struct{ runner *Runner }

// NewCreateTableHandler creates a new CreateTableHandler.
func NewCreateTableHandler(runner *Runner) *CreateTableHandler {
	return &CreateTableHandler{runner: runner}
}

// Handle executes the Table creation command.
func (h *CreateTableHandler) Handle(ctx context.Context, actor Actor, cmd CreateTableCommand) (int, TableResponse, error) {
	display, key := NormalizeTableName(cmd.Name)
	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpCreateTable,
		Fingerprint: createTableFingerprint{Name: display},
		Required:    []string{CapTablesAdminister},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(q *sqlc.Queries) (int, TableResponse, AuditRecord, error) {
			if err := ValidateTableName(display); err != nil {
				return 0, TableResponse{}, AuditRecord{}, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error())
			}

			row, err := q.CreateTable(ctx, sqlc.CreateTableParams{
				Name:           display,
				NormalizedName: key,
			})
			if err != nil {
				return 0, TableResponse{}, AuditRecord{}, MapDBError(err)
			}

			return 201, toTableResponse(row), AuditRecord{
				EventType: EventTableCreated,
				Details:   tableCreatedAuditDetails{TableID: row.ID, Name: row.Name},
			}, nil
		})
}

// RenameTableHandler handles Table renaming.
type RenameTableHandler struct{ runner *Runner }

// NewRenameTableHandler creates a new RenameTableHandler.
func NewRenameTableHandler(runner *Runner) *RenameTableHandler {
	return &RenameTableHandler{runner: runner}
}

// Handle executes the Table rename command.
func (h *RenameTableHandler) Handle(ctx context.Context, actor Actor, cmd RenameTableCommand) (int, TableResponse, error) {
	display, key := NormalizeTableName(cmd.Name)
	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpRenameTable,
		Fingerprint: renameTableFingerprint{TableID: cmd.TableID, Name: display},
		Required:    []string{CapTablesAdminister},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(q *sqlc.Queries) (int, TableResponse, AuditRecord, error) {
			if err := ValidateTableName(display); err != nil {
				return 0, TableResponse{}, AuditRecord{}, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error())
			}

			before, err := q.GetTableForUpdate(ctx, cmd.TableID)
			if err != nil {
				return 0, TableResponse{}, AuditRecord{}, MapDBError(err)
			}

			after, err := q.RenameTable(ctx, sqlc.RenameTableParams{
				ID:             cmd.TableID,
				Name:           display,
				NormalizedName: key,
			})
			if err != nil {
				return 0, TableResponse{}, AuditRecord{}, MapDBError(err)
			}

			return 200, toTableResponse(after), AuditRecord{
				EventType: EventTableRenamed,
				Details: tableRenamedAuditDetails{
					TableID:    after.ID,
					BeforeName: before.Name,
					AfterName:  after.Name,
				},
			}, nil
		})
}

// SetTableAvailabilityHandler handles Table Availability changes.
type SetTableAvailabilityHandler struct{ runner *Runner }

// NewSetTableAvailabilityHandler creates a new SetTableAvailabilityHandler.
func NewSetTableAvailabilityHandler(runner *Runner) *SetTableAvailabilityHandler {
	return &SetTableAvailabilityHandler{runner: runner}
}

// Handle executes the Availability command. Setting the current value again is
// a successful no-op: it returns current state, leaves updated_at alone, and
// writes no audit event.
func (h *SetTableAvailabilityHandler) Handle(ctx context.Context, actor Actor, cmd SetTableAvailabilityCommand) (int, TableResponse, error) {
	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpSetTableAvailability,
		Fingerprint: setAvailabilityFingerprint{
			TableID:   cmd.TableID,
			Available: cmd.Available,
		},
		Required: []string{CapTablesAdminister},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(q *sqlc.Queries) (int, TableResponse, AuditRecord, error) {
			before, err := q.GetTableForUpdate(ctx, cmd.TableID)
			if err != nil {
				return 0, TableResponse{}, AuditRecord{}, MapDBError(err)
			}

			if before.Available == cmd.Available {
				// Same-state no-op: no row change, no audit event.
				return 200, toTableResponse(before), AuditRecord{}, nil
			}

			after, err := q.SetTableAvailability(ctx, sqlc.SetTableAvailabilityParams{
				ID:        cmd.TableID,
				Available: cmd.Available,
			})
			if err != nil {
				return 0, TableResponse{}, AuditRecord{}, MapDBError(err)
			}

			return 200, toTableResponse(after), AuditRecord{
				EventType: EventTableAvailabilityChanged,
				Details: tableAvailabilityAuditDetails{
					TableID:         after.ID,
					BeforeAvailable: before.Available,
					AfterAvailable:  after.Available,
				},
			}, nil
		})
}
