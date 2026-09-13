package sales

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

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

// composition is a draft item's identity: two items with the same composition
// are the same line.
type composition struct {
	SizeID      uuid.NullUUID
	Note        sql.NullString
	ModifierKey string
	OptionIDs   []uuid.UUID
}

// mergeOutcome reports what applyCompositionChange did, so the caller can emit
// the right audit events.
type mergeOutcome struct {
	// SurvivingItemID is the edited row when nothing collided, or the
	// pre-existing row when the two merged.
	SurvivingItemID uuid.UUID
	Quantity        int32
	Merged          bool
	AbsorbedItemID  uuid.UUID
	AbsorbedQty     int32
}

// findDraftItemByCompositionExcluding loads the draft line matching the
// composition other than the row being edited, or returns (nil, nil) when no
// other line matches. It is findDraftItemByComposition plus an id <> $n
// clause, so the row under edit can never match itself.
func findDraftItemByCompositionExcluding(ctx context.Context, q *sqlc.Queries,
	draftID, menuItemID uuid.UUID, comp composition, excludeItemID uuid.UUID,
) (*sqlc.FindDraftItemByCompositionExcludingRow, error) {
	row, err := q.FindDraftItemByCompositionExcluding(ctx, sqlc.FindDraftItemByCompositionExcludingParams{
		OrderDraftID:    draftID,
		MenuItemID:      menuItemID,
		ModifierKey:     comp.ModifierKey,
		ID:              excludeItemID,
		SizeID:          comp.SizeID,
		PreparationNote: comp.Note,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("find draft item by composition excluding: %w", err)
	}
	return &row, nil
}

// applyCompositionChange moves a draft item to a new composition, merging it
// into an existing line when one already has that composition.
//
// This is the behavior most easily lost in a port: it is reached from three
// different commands and produces a result none of their names suggests — the
// edited item's id disappears and another row's quantity grows. Every caller
// returns the reloaded projection so the client sees it immediately.
//
// The caller must already hold the draft row lock, so no competing row can
// appear between the lookup and the write.
func applyCompositionChange(ctx context.Context, q *sqlc.Queries,
	draftID uuid.UUID, item sqlc.LockDraftItemRow, next composition,
) (mergeOutcome, error) {
	existing, err := findDraftItemByCompositionExcluding(ctx, q, draftID,
		item.MenuItemID, next, item.ID)
	if err != nil {
		return mergeOutcome{}, err
	}

	if existing != nil {
		total := existing.Quantity + item.Quantity
		// Reject rather than clamp: silently capping a quantity would charge
		// the customer for fewer units than staff entered.
		if err := ValidateQuantity(total); err != nil {
			return mergeOutcome{}, err
		}
		if _, err := q.SetDraftItemQuantity(ctx, sqlc.SetDraftItemQuantityParams{
			ID: existing.ID, Quantity: total,
		}); err != nil {
			return mergeOutcome{}, fmt.Errorf("merge draft item quantity: %w", err)
		}
		// The absorbed row's option rows cascade away. The survivor's are
		// already correct: the two rows agreed on composition by definition.
		if err := q.DeleteDraftItem(ctx, item.ID); err != nil {
			return mergeOutcome{}, fmt.Errorf("delete absorbed draft item: %w", err)
		}
		return mergeOutcome{
			SurvivingItemID: existing.ID,
			Quantity:        total,
			Merged:          true,
			AbsorbedItemID:  item.ID,
			AbsorbedQty:     item.Quantity,
		}, nil
	}

	if err := q.UpdateDraftItemComposition(ctx, sqlc.UpdateDraftItemCompositionParams{
		ID:              item.ID,
		SizeID:          next.SizeID,
		PreparationNote: next.Note,
		ModifierKey:     next.ModifierKey,
	}); err != nil {
		return mergeOutcome{}, fmt.Errorf("update draft item composition: %w", err)
	}
	if err := replaceDraftItemOptions(ctx, q, item.ID, next.OptionIDs); err != nil {
		return mergeOutcome{}, err
	}
	return mergeOutcome{SurvivingItemID: item.ID, Quantity: item.Quantity}, nil
}

type draftItemsMergedAudit struct {
	ServiceSessionID  uuid.UUID `json:"service_session_id"`
	OrderDraftID      uuid.UUID `json:"order_draft_id"`
	SurvivingItemID   uuid.UUID `json:"surviving_draft_item_id"`
	AbsorbedItemID    uuid.UUID `json:"absorbed_draft_item_id"`
	AbsorbedQuantity  int32     `json:"absorbed_quantity"`
	ResultingQuantity int32     `json:"resulting_quantity"`
}

// writeMergeAudit records a merge alongside the command's own event, because a
// quantity changed that no command asked to change.
func writeMergeAudit(ctx context.Context, q *sqlc.Queries, actor Actor,
	sessionID, draftID uuid.UUID, outcome mergeOutcome,
) error {
	if !outcome.Merged {
		return nil
	}
	details, err := json.Marshal(draftItemsMergedAudit{
		ServiceSessionID:  sessionID,
		OrderDraftID:      draftID,
		SurvivingItemID:   outcome.SurvivingItemID,
		AbsorbedItemID:    outcome.AbsorbedItemID,
		AbsorbedQuantity:  outcome.AbsorbedQty,
		ResultingQuantity: outcome.Quantity,
	})
	if err != nil {
		return fmt.Errorf("marshal merge audit: %w", err)
	}
	if _, err := q.InsertAuditEvent(ctx, sqlc.InsertAuditEventParams{
		EventType:  EventDraftItemsMerged,
		ActorID:    uuid.NullUUID{UUID: actor.StaffID, Valid: true},
		SessionID:  uuid.NullUUID{UUID: actor.SessionID, Valid: true},
		Details:    details,
		OccurredAt: time.Now(),
	}); err != nil {
		return fmt.Errorf("insert merge audit event: %w", err)
	}
	return nil
}

type setSizeFingerprint struct {
	ServiceSessionID uuid.UUID  `json:"service_session_id"`
	DraftItemID      uuid.UUID  `json:"draft_item_id"`
	SizeID           *uuid.UUID `json:"size_id"`
}

// SetDraftItemSizeHandler sets or clears a draft item's Size.
type SetDraftItemSizeHandler struct{ runner *Runner }

// NewSetDraftItemSizeHandler creates a new SetDraftItemSizeHandler.
func NewSetDraftItemSizeHandler(runner *Runner) *SetDraftItemSizeHandler {
	return &SetDraftItemSizeHandler{runner: runner}
}

// Handle changes the Size, merging the item into another line when that makes
// the two compositions identical.
//
// A nil SizeID clears the Size and is permitted: the draft tolerates an
// incomplete configuration, and Commit in 5B is where a required Size is
// enforced.
func (h *SetDraftItemSizeHandler) Handle(ctx context.Context, actor Actor,
	cmd SetDraftItemSizeCommand,
) (int, ServiceSessionResponse, error) {
	var zero ServiceSessionResponse

	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpSetDraftItemSize,
		Fingerprint: setSizeFingerprint{
			ServiceSessionID: cmd.ServiceSessionID,
			DraftItemID:      cmd.DraftItemID,
			SizeID:           cmd.SizeID,
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
			if cmd.SizeID != nil {
				if err := requireSizeForItem(ctx, q, item.MenuItemID, *cmd.SizeID); err != nil {
					return 0, zero, AuditRecord{}, err
				}
			}

			// Carry the untouched components forward. The option ids come from
			// the authoritative rows, not from modifier_key, which is derived.
			optionIDs, err := q.ListDraftItemOptionIDs(ctx, item.ID)
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("load draft item options: %w", err)
			}

			next := composition{
				SizeID:      nullUUID(cmd.SizeID),
				Note:        item.PreparationNote,
				ModifierKey: item.ModifierKey,
				OptionIDs:   optionIDs,
			}

			outcome, err := applyCompositionChange(ctx, q, draft.OrderDraftID, item, next)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			if err := writeMergeAudit(ctx, q, actor,
				cmd.ServiceSessionID, draft.OrderDraftID, outcome); err != nil {
				return 0, zero, AuditRecord{}, err
			}

			result, err := LoadServiceSession(ctx, q, cmd.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			return 200, result, AuditRecord{
				EventType: EventDraftItemSizeSet,
				Details: draftItemAudit{
					ServiceSessionID: cmd.ServiceSessionID,
					OrderDraftID:     draft.OrderDraftID,
					DraftItemID:      outcome.SurvivingItemID,
					MenuItemID:       item.MenuItemID,
					Quantity:         outcome.Quantity,
				},
			}, nil
		})
}

type setNoteFingerprint struct {
	ServiceSessionID uuid.UUID `json:"service_session_id"`
	DraftItemID      uuid.UUID `json:"draft_item_id"`
	PreparationNote  *string   `json:"preparation_note"`
}

// SetDraftItemNoteHandler sets or clears a draft item's Preparation Note.
type SetDraftItemNoteHandler struct{ runner *Runner }

// NewSetDraftItemNoteHandler creates a new SetDraftItemNoteHandler.
func NewSetDraftItemNoteHandler(runner *Runner) *SetDraftItemNoteHandler {
	return &SetDraftItemNoteHandler{runner: runner}
}

// Handle sets the Preparation Note, merging the item into another line when
// that makes the two compositions identical. A nil or whitespace-only note
// clears it.
func (h *SetDraftItemNoteHandler) Handle(ctx context.Context, actor Actor,
	cmd SetDraftItemNoteCommand,
) (int, ServiceSessionResponse, error) {
	var zero ServiceSessionResponse

	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpSetDraftItemNote,
		Fingerprint: setNoteFingerprint{
			ServiceSessionID: cmd.ServiceSessionID,
			DraftItemID:      cmd.DraftItemID,
			PreparationNote:  trimmedForFingerprint(cmd.PreparationNote),
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
			note, err := NormalizePreparationNote(cmd.PreparationNote)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}

			// Carry the untouched components forward. The option ids come from
			// the authoritative rows, not from modifier_key, which is derived.
			optionIDs, err := q.ListDraftItemOptionIDs(ctx, item.ID)
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("load draft item options: %w", err)
			}

			next := composition{
				SizeID:      item.SizeID,
				Note:        note,
				ModifierKey: item.ModifierKey,
				OptionIDs:   optionIDs,
			}

			outcome, err := applyCompositionChange(ctx, q, draft.OrderDraftID, item, next)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			if err := writeMergeAudit(ctx, q, actor,
				cmd.ServiceSessionID, draft.OrderDraftID, outcome); err != nil {
				return 0, zero, AuditRecord{}, err
			}

			result, err := LoadServiceSession(ctx, q, cmd.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			return 200, result, AuditRecord{
				EventType: EventDraftItemNoteSet,
				Details: draftItemAudit{
					ServiceSessionID: cmd.ServiceSessionID,
					OrderDraftID:     draft.OrderDraftID,
					DraftItemID:      outcome.SurvivingItemID,
					MenuItemID:       item.MenuItemID,
					Quantity:         outcome.Quantity,
				},
			}, nil
		})
}

type setModifiersFingerprint struct {
	ServiceSessionID  uuid.UUID   `json:"service_session_id"`
	DraftItemID       uuid.UUID   `json:"draft_item_id"`
	ModifierOptionIDs []uuid.UUID `json:"modifier_option_ids"`
}

// SetDraftItemModifiersHandler replaces a draft item's selected options.
type SetDraftItemModifiersHandler struct{ runner *Runner }

// NewSetDraftItemModifiersHandler creates a new SetDraftItemModifiersHandler.
func NewSetDraftItemModifiersHandler(runner *Runner) *SetDraftItemModifiersHandler {
	return &SetDraftItemModifiersHandler{runner: runner}
}

// Handle replaces the selection, merging the item into another line when that
// makes the two compositions identical.
//
// Unlike the add command this list is required and taken literally: an empty
// list means the customer declined every option, and no defaults are applied.
func (h *SetDraftItemModifiersHandler) Handle(ctx context.Context, actor Actor,
	cmd SetDraftItemModifiersCommand,
) (int, ServiceSessionResponse, error) {
	var zero ServiceSessionResponse

	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpSetDraftItemModifiers,
		Fingerprint: setModifiersFingerprint{
			ServiceSessionID:  cmd.ServiceSessionID,
			DraftItemID:       cmd.DraftItemID,
			ModifierOptionIDs: SortedUUIDs(cmd.ModifierOptionIDs),
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
			if err := validateModifierOptions(ctx, q, item.MenuItemID, cmd.ModifierOptionIDs); err != nil {
				return 0, zero, AuditRecord{}, err
			}

			next := composition{
				SizeID:      item.SizeID,
				Note:        item.PreparationNote,
				ModifierKey: ModifierKeyFor(cmd.ModifierOptionIDs),
				OptionIDs:   cmd.ModifierOptionIDs,
			}

			outcome, err := applyCompositionChange(ctx, q, draft.OrderDraftID, item, next)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			if err := writeMergeAudit(ctx, q, actor,
				cmd.ServiceSessionID, draft.OrderDraftID, outcome); err != nil {
				return 0, zero, AuditRecord{}, err
			}

			result, err := LoadServiceSession(ctx, q, cmd.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			return 200, result, AuditRecord{
				EventType: EventDraftItemModifiersSet,
				Details: draftItemAudit{
					ServiceSessionID: cmd.ServiceSessionID,
					OrderDraftID:     draft.OrderDraftID,
					DraftItemID:      outcome.SurvivingItemID,
					MenuItemID:       item.MenuItemID,
					Quantity:         outcome.Quantity,
				},
			}, nil
		})
}
