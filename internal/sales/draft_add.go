package sales

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

// addDraftItemFingerprint sorts the option ids: selection order does not
// change the chosen set. ModifierOptionIDs stays a pointer so an absent list
// and an empty one hash differently — they are different requests.
type addDraftItemFingerprint struct {
	ServiceSessionID  uuid.UUID    `json:"service_session_id"`
	MenuItemID        uuid.UUID    `json:"menu_item_id"`
	SizeID            *uuid.UUID   `json:"size_id"`
	PreparationNote   *string      `json:"preparation_note"`
	ModifierOptionIDs *[]uuid.UUID `json:"modifier_option_ids"`
}

type draftItemAudit struct {
	ServiceSessionID  uuid.UUID   `json:"service_session_id"`
	OrderDraftID      uuid.UUID   `json:"order_draft_id"`
	DraftItemID       uuid.UUID   `json:"draft_item_id"`
	MenuItemID        uuid.UUID   `json:"menu_item_id"`
	Quantity          int32       `json:"quantity"`
	ModifierOptionIDs []uuid.UUID `json:"modifier_option_ids"`
}

// lockEditableDraft acquires the Session's editable draft, checking every
// precondition in the same statement that takes the lock.
func lockEditableDraft(ctx context.Context, q *sqlc.Queries, sessionID uuid.UUID) (
	sqlc.LockEditableDraftRow, error,
) {
	row, err := q.LockEditableDraft(ctx, sessionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return row, fmt.Errorf("%w: %s", ErrEditableDraftNotFound, sessionID)
		}
		return row, fmt.Errorf("lock editable draft: %w", err)
	}
	return row, nil
}

// requireSellableMenuItem locks the Menu Item and rejects it if it is not
// currently choosable.
//
// Retirement is checked before availability: retirement is permanent and
// availability is temporary, so a retired item must not report "try again
// later".
func requireSellableMenuItem(ctx context.Context, q *sqlc.Queries, menuItemID uuid.UUID) (
	sqlc.LockMenuItemForDraftRow, error,
) {
	item, err := q.LockMenuItemForDraft(ctx, menuItemID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return item, fmt.Errorf("%w: %s", ErrMenuItemNotFound, menuItemID)
		}
		return item, fmt.Errorf("lock menu item: %w", err)
	}
	if item.RetiredAt.Valid {
		return item, fmt.Errorf("%w: %s", ErrMenuItemRetired, item.Name)
	}
	if !item.Available {
		return item, fmt.Errorf("%w: %s", ErrMenuItemUnavailable, item.Name)
	}
	return item, nil
}

// requireSizeForItem rejects a Size that does not belong to the Item or is not
// currently choosable. A Size from another Menu Item reports not-found: it is
// not a Size of this item at all.
func requireSizeForItem(ctx context.Context, q *sqlc.Queries,
	menuItemID, sizeID uuid.UUID,
) error {
	size, err := q.GetMenuItemSizeForDraft(ctx, sizeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: %s", ErrSizeNotFound, sizeID)
		}
		return fmt.Errorf("load menu item size: %w", err)
	}
	if size.MenuItemID != menuItemID {
		return fmt.Errorf("%w: %s does not belong to this menu item", ErrSizeNotFound, sizeID)
	}
	if size.RetiredAt.Valid {
		return fmt.Errorf("%w: %s", ErrSizeRetired, size.Name)
	}
	if !size.Available {
		return fmt.Errorf("%w: %s", ErrSizeUnavailable, size.Name)
	}
	return nil
}

// replaceDraftItemOptions rewrites a draft item's selected options.
func replaceDraftItemOptions(ctx context.Context, q *sqlc.Queries, itemID uuid.UUID,
	optionIDs []uuid.UUID,
) error {
	if err := q.DeleteDraftItemModifierOptions(ctx, itemID); err != nil {
		return fmt.Errorf("clear draft item modifier options: %w", err)
	}
	for _, optionID := range optionIDs {
		if err := q.InsertDraftItemModifierOption(ctx, sqlc.InsertDraftItemModifierOptionParams{
			OrderDraftItemID: itemID,
			ModifierOptionID: optionID,
		}); err != nil {
			return fmt.Errorf("insert draft item modifier option: %w", err)
		}
	}
	return nil
}

// findDraftItemByComposition loads the draft line matching the composition, or
// returns (nil, nil) when no line matches.
func findDraftItemByComposition(ctx context.Context, q *sqlc.Queries,
	draftID, menuItemID uuid.UUID, sizeID *uuid.UUID,
	note sql.NullString, modifierKey string,
) (*sqlc.FindDraftItemByCompositionRow, error) {
	row, err := q.FindDraftItemByComposition(ctx, sqlc.FindDraftItemByCompositionParams{
		OrderDraftID:    draftID,
		MenuItemID:      menuItemID,
		ModifierKey:     modifierKey,
		SizeID:          nullUUID(sizeID),
		PreparationNote: note,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("find draft item by composition: %w", err)
	}
	return &row, nil
}

// AddDraftItemHandler adds one unit of a configured Menu Item to the draft.
type AddDraftItemHandler struct{ runner *Runner }

// NewAddDraftItemHandler creates a new AddDraftItemHandler.
func NewAddDraftItemHandler(runner *Runner) *AddDraftItemHandler {
	return &AddDraftItemHandler{runner: runner}
}

// Handle adds one unit, merging into an existing line of the same composition.
//
// Validation is deliberately shallow: it checks only that what has been chosen
// is currently choosable, not that the configuration is complete. A sized item
// with no Size and a required Group with no selection are both valid draft
// states. Commit, in 5B, is where completeness is enforced, because that is
// where price is fixed.
func (h *AddDraftItemHandler) Handle(ctx context.Context, actor Actor,
	cmd AddDraftItemCommand,
) (int, ServiceSessionResponse, error) {
	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpAddDraftItem,
		Fingerprint: addDraftItemFingerprint{
			ServiceSessionID:  cmd.ServiceSessionID,
			MenuItemID:        cmd.MenuItemID,
			SizeID:            cmd.SizeID,
			PreparationNote:   trimmedForFingerprint(cmd.PreparationNote),
			ModifierOptionIDs: sortedOptionPointer(cmd.ModifierOptionIDs),
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

			item, err := requireSellableMenuItem(ctx, q, cmd.MenuItemID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			if cmd.SizeID != nil {
				if err := requireSizeForItem(ctx, q, item.ID, *cmd.SizeID); err != nil {
					return 0, zero, AuditRecord{}, err
				}
			}

			// Absent means "use the menu's defaults"; empty means "the
			// customer declined every option".
			var optionIDs []uuid.UUID
			if cmd.ModifierOptionIDs == nil {
				optionIDs, err = defaultOptionIDs(ctx, q, item.ID)
				if err != nil {
					return 0, zero, AuditRecord{}, err
				}
			} else {
				optionIDs = *cmd.ModifierOptionIDs
			}
			if err := validateModifierOptions(ctx, q, item.ID, optionIDs); err != nil {
				return 0, zero, AuditRecord{}, err
			}

			note, err := NormalizePreparationNote(cmd.PreparationNote)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			modifierKey := ModifierKeyFor(optionIDs)

			itemID, quantity, err := upsertDraftItem(ctx, q, draft.OrderDraftID,
				cmd.MenuItemID, cmd.SizeID, note, modifierKey, optionIDs)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}

			result, err := LoadServiceSession(ctx, q, cmd.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}

			return 200, result, AuditRecord{
				EventType: EventDraftItemAdded,
				Details: draftItemAudit{
					ServiceSessionID:  cmd.ServiceSessionID,
					OrderDraftID:      draft.OrderDraftID,
					DraftItemID:       itemID,
					MenuItemID:        cmd.MenuItemID,
					Quantity:          quantity,
					ModifierOptionIDs: SortedUUIDs(optionIDs),
				},
			}, nil
		})
}

// upsertDraftItem increments an existing line of the same composition or
// inserts a new one.
//
// This is a lookup-then-write rather than an ON CONFLICT upsert, because the
// increment must be validated against MaxQuantity: an upsert's DO UPDATE would
// hit the database check constraint and surface as an unmapped 500 instead of
// a business error. The caller already holds the draft row lock, so no other
// transaction can insert a competing composition in between.
func upsertDraftItem(ctx context.Context, q *sqlc.Queries, draftID, menuItemID uuid.UUID,
	sizeID *uuid.UUID, note sql.NullString, modifierKey string, optionIDs []uuid.UUID,
) (uuid.UUID, int32, error) {
	existing, err := findDraftItemByComposition(ctx, q, draftID, menuItemID, sizeID, note, modifierKey)
	if err != nil {
		return uuid.Nil, 0, err
	}
	if existing != nil {
		next := existing.Quantity + 1
		if err := ValidateQuantity(next); err != nil {
			return uuid.Nil, 0, err
		}
		row, err := q.SetDraftItemQuantity(ctx, sqlc.SetDraftItemQuantityParams{
			ID: existing.ID, Quantity: next,
		})
		if err != nil {
			return uuid.Nil, 0, fmt.Errorf("increment draft item quantity: %w", err)
		}
		return row.ID, row.Quantity, nil
	}

	row, err := q.InsertDraftItem(ctx, sqlc.InsertDraftItemParams{
		OrderDraftID:    draftID,
		MenuItemID:      menuItemID,
		SizeID:          nullUUID(sizeID),
		PreparationNote: note,
		ModifierKey:     modifierKey,
	})
	if err != nil {
		return uuid.Nil, 0, fmt.Errorf("insert draft item: %w", err)
	}
	if err := replaceDraftItemOptions(ctx, q, row.ID, optionIDs); err != nil {
		return uuid.Nil, 0, err
	}
	return row.ID, row.Quantity, nil
}

// nullUUID converts an optional id pointer to the nullable UUID type the
// generated queries scan and write.
func nullUUID(id *uuid.UUID) uuid.NullUUID {
	if id == nil {
		return uuid.NullUUID{}
	}
	return uuid.NullUUID{UUID: *id, Valid: true}
}

// trimmedForFingerprint returns a pointer to the trimmed note, or nil when it
// trims to empty, so a replay differing only in surrounding whitespace is a
// replay rather than a conflict.
func trimmedForFingerprint(v *string) *string {
	if v == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*v)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// sortedOptionPointer returns nil for nil, and a pointer to the sorted copy
// otherwise, preserving the absent-versus-empty distinction through the
// fingerprint.
func sortedOptionPointer(ids *[]uuid.UUID) *[]uuid.UUID {
	if ids == nil {
		return nil
	}
	sorted := SortedUUIDs(*ids)
	return &sorted
}
