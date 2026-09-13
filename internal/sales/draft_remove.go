package sales

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

type removeDraftItemFingerprint struct {
	ServiceSessionID uuid.UUID `json:"service_session_id"`
	DraftItemID      uuid.UUID `json:"draft_item_id"`
}

// RemoveDraftItemHandler removes a draft item outright, with its selected
// options.
type RemoveDraftItemHandler struct{ runner *Runner }

// NewRemoveDraftItemHandler creates a new RemoveDraftItemHandler.
func NewRemoveDraftItemHandler(runner *Runner) *RemoveDraftItemHandler {
	return &RemoveDraftItemHandler{runner: runner}
}

// Handle removes one draft item. The composition is not merged into anything:
// removal deletes the line, and the composition index loses nothing.
func (h *RemoveDraftItemHandler) Handle(ctx context.Context, actor Actor,
	cmd RemoveDraftItemCommand,
) (int, ServiceSessionResponse, error) {
	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpRemoveDraftItem,
		Fingerprint: removeDraftItemFingerprint{
			ServiceSessionID: cmd.ServiceSessionID,
			DraftItemID:      cmd.DraftItemID,
		},
		Required: []string{CapSalesOperate},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, ServiceSessionResponse, AuditRecord, error) {
			var zero ServiceSessionResponse
			q := mc.Queries

			draft, err := lockEditableDraft(ctx, q, cmd.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			item, err := lockDraftItem(ctx, q, draft.OrderDraftID, cmd.DraftItemID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}

			// Read before the delete: the cascade takes the option rows with
			// the item, and the audit event must record what was removed.
			optionIDs, err := q.ListDraftItemOptionIDs(ctx, item.ID)
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("list draft item options: %w", err)
			}

			if err := q.DeleteDraftItem(ctx, item.ID); err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("delete draft item: %w", err)
			}

			result, err := LoadServiceSession(ctx, q, cmd.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			return 200, result, AuditRecord{
				EventType: EventDraftItemRemoved,
				Details: draftItemAudit{
					ServiceSessionID:  cmd.ServiceSessionID,
					OrderDraftID:      draft.OrderDraftID,
					DraftItemID:       item.ID,
					MenuItemID:        item.MenuItemID,
					Quantity:          item.Quantity,
					ModifierOptionIDs: optionIDs,
				},
			}, nil
		})
}
