package sales

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

// lockDraftItem acquires one draft item, scoped to its draft so a caller
// cannot reach another Session's item by guessing an id.
func lockDraftItem(ctx context.Context, q *sqlc.Queries, draftID, itemID uuid.UUID) (
	sqlc.LockDraftItemRow, error,
) {
	row, err := q.LockDraftItem(ctx, sqlc.LockDraftItemParams{
		ID: itemID, OrderDraftID: draftID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return row, fmt.Errorf("%w: %s", ErrDraftItemNotFound, itemID)
		}
		return row, fmt.Errorf("lock draft item: %w", err)
	}
	return row, nil
}

type setQuantityFingerprint struct {
	ServiceSessionID uuid.UUID `json:"service_session_id"`
	DraftItemID      uuid.UUID `json:"draft_item_id"`
	Quantity         int32     `json:"quantity"`
}

// SetDraftItemQuantityHandler sets an absolute quantity on a draft item.
type SetDraftItemQuantityHandler struct{ runner *Runner }

// NewSetDraftItemQuantityHandler creates a new SetDraftItemQuantityHandler.
func NewSetDraftItemQuantityHandler(runner *Runner) *SetDraftItemQuantityHandler {
	return &SetDraftItemQuantityHandler{runner: runner}
}

// Handle sets the quantity. Zero is rejected rather than treated as removal.
//
// Quantity is not part of the composition, so this command can never trigger a
// merge.
func (h *SetDraftItemQuantityHandler) Handle(ctx context.Context, actor Actor,
	cmd SetDraftItemQuantityCommand,
) (int, ServiceSessionResponse, error) {
	var zero ServiceSessionResponse
	if cmd.Quantity == nil {
		return 0, zero, fmt.Errorf("%w: quantity is required", ErrInvalidQuantity)
	}
	quantity := *cmd.Quantity
	if err := ValidateQuantity(quantity); err != nil {
		return 0, zero, err
	}

	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpSetDraftItemQuantity,
		Fingerprint: setQuantityFingerprint{
			ServiceSessionID: cmd.ServiceSessionID,
			DraftItemID:      cmd.DraftItemID,
			Quantity:         quantity,
		},
		Required: []string{CapSalesOperate},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, ServiceSessionResponse, AuditRecord, error) {
			q := mc.Queries

			draft, err := lockEditableDraft(ctx, q, cmd.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			item, err := lockDraftItem(ctx, q, draft.OrderDraftID, cmd.DraftItemID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			if _, err := q.SetDraftItemQuantity(ctx, sqlc.SetDraftItemQuantityParams{
				ID: item.ID, Quantity: quantity,
			}); err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("set draft item quantity: %w", err)
			}

			result, err := LoadServiceSession(ctx, q, cmd.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			return 200, result, AuditRecord{
				EventType: EventDraftItemQuantitySet,
				Details: draftItemAudit{
					ServiceSessionID: cmd.ServiceSessionID,
					OrderDraftID:     draft.OrderDraftID,
					DraftItemID:      item.ID,
					MenuItemID:       item.MenuItemID,
					Quantity:         quantity,
				},
			}, nil
		})
}
