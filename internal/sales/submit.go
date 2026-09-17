package sales

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

// SubmitOrderHandler crosses the preparation boundary: it turns a committed
// Order Draft's Committed Items into an Order, its Order Items, and the
// Preparation Units the bar works from, repricing nothing.
type SubmitOrderHandler struct{ runner *Runner }

// NewSubmitOrderHandler creates a SubmitOrderHandler.
func NewSubmitOrderHandler(runner *Runner) *SubmitOrderHandler {
	return &SubmitOrderHandler{runner: runner}
}

// Handle executes Submit.
//
// Submit deliberately does not require an open Sales Shift. Only starting a
// new Order Draft does: a Shift can end while drinks are still being prepared,
// and staff must be able to finish what is already in flight. What the Shift
// rule blocks is opening new work.
func (h *SubmitOrderHandler) Handle(ctx context.Context, actor Actor,
	cmd SubmitOrderCommand,
) (int, ServiceSessionResponse, error) {
	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpSubmitOrder,
		Fingerprint: sessionScopedFingerprint{ServiceSessionID: cmd.ServiceSessionID},
		Required:    []string{CapSalesOperate},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, ServiceSessionResponse, AuditRecord, error) {
			var zero ServiceSessionResponse
			q := mc.Queries

			// The Session is locked before the Checks, unchanged: a missing or
			// closed source is told apart from "nothing to submit" here, per
			// spec §9.3, while the draft lock below stays the sole arbiter of
			// whether any committed work awaits submission. Submit's lock
			// order is Session+Draft then Checks; the AB-BA window that leaves
			// against Payment's Check-then-Session order is ADR-031's.
			source, err := q.LockServiceSessionForSubmission(ctx, cmd.ServiceSessionID)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return 0, zero, AuditRecord{}, fmt.Errorf(
						"%w: %s", ErrServiceSessionNotFound, cmd.ServiceSessionID)
				}
				return 0, zero, AuditRecord{}, fmt.Errorf("lock service session: %w", err)
			}
			if source.State != StateActive {
				return 0, zero, AuditRecord{}, fmt.Errorf(
					"%w: %s", ErrServiceSessionClosed, cmd.ServiceSessionID)
			}

			draftID, err := q.LockSubmittableDraft(ctx, cmd.ServiceSessionID)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return 0, zero, AuditRecord{}, fmt.Errorf(
						"%w: %s", ErrNothingToSubmit, cmd.ServiceSessionID)
				}
				return 0, zero, AuditRecord{}, fmt.Errorf("lock submittable draft: %w", err)
			}

			checks, err := q.LockChecksForSubmission(ctx, draftID)
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("lock checks for submission: %w", err)
			}
			if len(checks) == 0 {
				return 0, zero, AuditRecord{}, fmt.Errorf(
					"%w: committed draft has no charge", ErrNothingToSubmit)
			}
			if ModeRequiresSettlementBeforeSubmit(source.ServiceMode) {
				for _, check := range checks {
					if check.State != CheckStateSettled {
						return 0, zero, AuditRecord{}, fmt.Errorf(
							"%w: check %s is %s", ErrCheckNotSettledForSubmission,
							check.ID, check.State)
					}
				}
			}

			occurredAt, err := q.GetSalesOccurredAt(ctx)
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("read submission occurrence time: %w", err)
			}
			orderID, err := q.InsertOrder(ctx, sqlc.InsertOrderParams{
				ServiceSessionID:              cmd.ServiceSessionID,
				OrderDraftID:                  draftID,
				SubmittedByStaffIdentityID:    actor.StaffID,
				SubmittedStaffAccessSessionID: actor.SessionID,
				SubmittedAt:                   occurredAt,
			})
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					// ON CONFLICT DO NOTHING returned no row: another
					// transaction submitted this draft first. The row lock
					// makes this unreachable; the branch is the backstop.
					return 0, zero, AuditRecord{}, fmt.Errorf(
						"%w: %s", ErrNothingToSubmit, draftID)
				}
				return 0, zero, AuditRecord{}, fmt.Errorf("insert order: %w", err)
			}

			items, err := q.ListCommittedItemsForSubmission(ctx, draftID)
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("list committed items: %w", err)
			}
			itemIDs := make([]uuid.UUID, 0, len(items))
			for _, item := range items {
				itemIDs = append(itemIDs, item.ID)
			}
			modifiers, err := loadUnitModifiers(ctx, q, itemIDs)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}

			unitCount := 0
			for _, item := range items {
				orderItemID, err := q.InsertOrderItem(ctx, sqlc.InsertOrderItemParams{
					OrderID:         orderID,
					CommittedItemID: item.ID,
				})
				if err != nil {
					return 0, zero, AuditRecord{}, fmt.Errorf("insert order item: %w", err)
				}
				mods := modifiers[item.ID]
				if mods == nil {
					mods = make([]UnitModifierResponse, 0)
				}
				encoded, err := json.Marshal(mods)
				if err != nil {
					return 0, zero, AuditRecord{}, fmt.Errorf("encode unit modifiers: %w", err)
				}
				// One unit per unit of ordered quantity: a Committed Item of
				// quantity three is three separately prepared drinks.
				for n := int32(1); n <= item.Quantity; n++ {
					if err := q.InsertPreparationUnit(ctx, sqlc.InsertPreparationUnitParams{
						OrderItemID:     orderItemID,
						UnitNumber:      n,
						ServiceNumber:   source.ServiceNumber,
						CategoryName:    item.CategoryName,
						ItemName:        item.ItemName,
						SizeName:        item.SizeName,
						Modifiers:       encoded,
						PreparationNote: item.PreparationNote,
						QueuedAt:        occurredAt,
					}); err != nil {
						return 0, zero, AuditRecord{}, fmt.Errorf("insert preparation unit: %w", err)
					}
					unitCount++
				}
			}

			out, err := LoadServiceSession(ctx, q, cmd.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}

			checkIDs := make([]uuid.UUID, 0, len(checks))
			for _, check := range checks {
				checkIDs = append(checkIDs, check.ID)
			}
			return http.StatusOK, out, AuditRecord{
				EventType: EventOrderSubmitted,
				Details: map[string]any{
					"service_session_id":     cmd.ServiceSessionID,
					"order_draft_id":         draftID,
					"order_id":               orderID,
					"check_ids":              checkIDs,
					"order_item_count":       len(items),
					"preparation_unit_count": unitCount,
				},
			}, nil
		})
}

// loadUnitModifiers returns each Committed Item's Modifier Options in the
// order the bar display shows them.
//
// Sorting is by group name then option name under the vi-VN collation, so two
// units of the same configuration are byte-identical on the display and a
// barista comparing two tickets is comparing the same text.
func loadUnitModifiers(ctx context.Context, q *sqlc.Queries, itemIDs []uuid.UUID) (
	map[uuid.UUID][]UnitModifierResponse, error,
) {
	out := make(map[uuid.UUID][]UnitModifierResponse)
	if len(itemIDs) == 0 {
		return out, nil
	}
	rows, err := q.ListCommittedItemModifiers(ctx, itemIDs)
	if err != nil {
		return nil, fmt.Errorf("load committed item modifiers: %w", err)
	}
	for _, row := range rows {
		out[row.CommittedItemID] = append(out[row.CommittedItemID], UnitModifierResponse{
			GroupName:  row.ModifierGroupName,
			OptionName: row.ModifierOptionName,
		})
	}
	c := collate.New(language.Vietnamese)
	for id, mods := range out {
		sort.SliceStable(mods, func(i, j int) bool {
			if g := c.CompareString(mods[i].GroupName, mods[j].GroupName); g != 0 {
				return g < 0
			}
			return c.CompareString(mods[i].OptionName, mods[j].OptionName) < 0
		})
		out[id] = mods
	}
	return out, nil
}
