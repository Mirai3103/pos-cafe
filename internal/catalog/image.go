package catalog

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

type setItemImageFingerprint struct {
	ItemID uuid.UUID `json:"item_id"`
	SHA256 string    `json:"sha256"`
}

type clearItemImageFingerprint struct {
	ItemID uuid.UUID `json:"item_id"`
}

type itemImageAudit struct {
	ItemID           uuid.UUID `json:"item_id"`
	ImageKey         *string   `json:"image_key"`
	PreviousImageKey *string   `json:"previous_image_key"`
}

// SetItemImageHandler stores an uploaded image and points the item at it (ADR-057).
type SetItemImageHandler struct {
	runner *Runner
	media  *MediaStore
}

// NewSetItemImageHandler creates a SetItemImageHandler.
func NewSetItemImageHandler(runner *Runner, media *MediaStore) *SetItemImageHandler {
	return &SetItemImageHandler{runner: runner, media: media}
}

// Handle writes the file before the transaction, so a rollback leaves an
// unreferenced file and never a reference to a missing one.
func (h *SetItemImageHandler) Handle(ctx context.Context, actor Actor, cmd SetItemImageCommand) (int, ItemImageResponse, error) {
	stored, err := h.media.Save(cmd.Data)
	if err != nil {
		return 0, ItemImageResponse{}, err
	}
	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpItemSetImage,
		Fingerprint: setItemImageFingerprint{ItemID: cmd.ItemID, SHA256: stored.SHA256},
		Required:    []string{CapAdministerStructure},
	}
	key := sql.NullString{String: stored.Key, Valid: true}
	return ExecuteMutation(ctx, h.runner, actor, spec, func(q *sqlc.Queries) (int, ItemImageResponse, AuditRecord, error) {
		return setImageKey(ctx, q, cmd.ItemID, key, EventItemImageSet)
	})
}

// ClearItemImageHandler removes an item's image reference. The file stays.
type ClearItemImageHandler struct {
	runner *Runner
}

// NewClearItemImageHandler creates a ClearItemImageHandler.
func NewClearItemImageHandler(runner *Runner) *ClearItemImageHandler {
	return &ClearItemImageHandler{runner: runner}
}

// Handle clears image_key.
func (h *ClearItemImageHandler) Handle(ctx context.Context, actor Actor, cmd ClearItemImageCommand) (int, ItemImageResponse, error) {
	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpItemClearImage,
		Fingerprint: clearItemImageFingerprint{ItemID: cmd.ItemID},
		Required:    []string{CapAdministerStructure},
	}
	return ExecuteMutation(ctx, h.runner, actor, spec, func(q *sqlc.Queries) (int, ItemImageResponse, AuditRecord, error) {
		return setImageKey(ctx, q, cmd.ItemID, sql.NullString{}, EventItemImageCleared)
	})
}

func setImageKey(ctx context.Context, q *sqlc.Queries, itemID uuid.UUID, key sql.NullString, event string) (int, ItemImageResponse, AuditRecord, error) {
	existing, err := q.GetMenuItemForUpdate(ctx, itemID)
	if err != nil {
		return 0, ItemImageResponse{}, AuditRecord{}, MapDBError(err)
	}
	if existing.RetiredAt.Valid {
		return 0, ItemImageResponse{}, AuditRecord{}, fmt.Errorf("%w: item is retired", ErrEntityRetired)
	}
	item, err := q.SetMenuItemImageKey(ctx, sqlc.SetMenuItemImageKeyParams{ID: itemID, ImageKey: key})
	if err != nil {
		return 0, ItemImageResponse{}, AuditRecord{}, MapDBError(err)
	}
	res := ItemImageResponse{ItemID: item.ID, ImageURL: ImageURL(item.ImageKey)}
	audit := AuditRecord{
		EventType: event,
		Details: itemImageAudit{
			ItemID: item.ID, ImageKey: nullStringPtr(item.ImageKey), PreviousImageKey: nullStringPtr(existing.ImageKey),
		},
	}
	return 200, res, audit, nil
}
