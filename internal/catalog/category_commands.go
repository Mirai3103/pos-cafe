package catalog

import (
	"context"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

type createCategoryFingerprint struct {
	Name string `json:"name"`
}

type renameCategoryFingerprint struct {
	CategoryID uuid.UUID `json:"category_id"`
	Name       string    `json:"name"`
}

type categoryCreatedAuditDetails struct {
	CategoryID uuid.UUID `json:"category_id"`
	Name       string    `json:"name"`
}

type categoryRenamedAuditDetails struct {
	CategoryID uuid.UUID `json:"category_id"`
	OldName    string    `json:"old_name"`
	NewName    string    `json:"new_name"`
}

// CreateCategoryHandler handles creation of menu categories.
type CreateCategoryHandler struct {
	runner *Runner
}

// NewCreateCategoryHandler creates a new CreateCategoryHandler.
func NewCreateCategoryHandler(runner *Runner) *CreateCategoryHandler {
	return &CreateCategoryHandler{runner: runner}
}

// Handle executes the category creation command.
func (h *CreateCategoryHandler) Handle(ctx context.Context, actor Actor, cmd CreateCategoryCommand) (int, CategoryResponse, error) {
	display, key := NormalizeName(cmd.Name)
	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpCategoryCreate,
		Fingerprint: createCategoryFingerprint{
			Name: display,
		},
		Required: []string{CapAdministerStructure},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec, func(q *sqlc.Queries) (int, CategoryResponse, AuditRecord, error) {
		category, err := q.CreateMenuCategory(ctx, sqlc.CreateMenuCategoryParams{
			Name:           display,
			NormalizedName: key,
		})
		if err != nil {
			return 0, CategoryResponse{}, AuditRecord{}, MapDBError(err)
		}

		res := CategoryResponse{
			ID:   category.ID,
			Name: category.Name,
		}
		audit := AuditRecord{
			EventType: EventCategoryCreated,
			Details: categoryCreatedAuditDetails{
				CategoryID: category.ID,
				Name:       category.Name,
			},
		}
		return 201, res, audit, nil
	})
}

// RenameCategoryHandler handles renaming of menu categories.
type RenameCategoryHandler struct {
	runner *Runner
}

// NewRenameCategoryHandler creates a new RenameCategoryHandler.
func NewRenameCategoryHandler(runner *Runner) *RenameCategoryHandler {
	return &RenameCategoryHandler{runner: runner}
}

// Handle executes the category rename command.
func (h *RenameCategoryHandler) Handle(ctx context.Context, actor Actor, cmd RenameCategoryCommand) (int, CategoryResponse, error) {
	display, key := NormalizeName(cmd.Name)
	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpCategoryRename,
		Fingerprint: renameCategoryFingerprint{
			CategoryID: cmd.CategoryID,
			Name:       display,
		},
		Required: []string{CapAdministerStructure},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec, func(q *sqlc.Queries) (int, CategoryResponse, AuditRecord, error) {
		existing, err := q.GetMenuCategoryForUpdate(ctx, cmd.CategoryID)
		if err != nil {
			return 0, CategoryResponse{}, AuditRecord{}, MapDBError(err)
		}

		category, err := q.RenameMenuCategory(ctx, sqlc.RenameMenuCategoryParams{
			ID:             cmd.CategoryID,
			Name:           display,
			NormalizedName: key,
		})
		if err != nil {
			return 0, CategoryResponse{}, AuditRecord{}, MapDBError(err)
		}

		res := CategoryResponse{
			ID:   category.ID,
			Name: category.Name,
		}
		audit := AuditRecord{
			EventType: EventCategoryRenamed,
			Details: categoryRenamedAuditDetails{
				CategoryID: category.ID,
				OldName:    existing.Name,
				NewName:    category.Name,
			},
		}
		return 200, res, audit, nil
	})
}
