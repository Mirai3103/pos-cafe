package preparation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
)

type bulkAdvanceFingerprint struct {
	PreparationUnitIDs []uuid.UUID `json:"preparation_unit_ids"`
	TargetState        string      `json:"target_state"`
}

func normalizeBulkSelection(ids []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(ids))
	normalized := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		normalized = append(normalized, id)
	}
	return normalized
}

func sortedBulkSelection(ids []uuid.UUID) []uuid.UUID {
	sorted := append([]uuid.UUID(nil), ids...)
	slices.SortFunc(sorted, func(a, b uuid.UUID) int {
		return bytes.Compare(a[:], b[:])
	})
	return sorted
}

func validateBulkAdvance(cmd BulkAdvanceCommand) error {
	if cmd.RequestID == uuid.Nil {
		return fmt.Errorf("%w: request_id is required", response.ErrInvalid)
	}
	if len(cmd.PreparationUnitIDs) < 1 || len(cmd.PreparationUnitIDs) > 50 {
		return fmt.Errorf("%w: preparation_unit_ids must contain 1 through 50 ids", response.ErrInvalid)
	}
	for _, id := range cmd.PreparationUnitIDs {
		if id == uuid.Nil {
			return fmt.Errorf("%w: preparation_unit_ids must not contain a zero UUID", response.ErrInvalid)
		}
	}
	if !IsAdvanceTarget(cmd.TargetState) {
		return fmt.Errorf("%w: target_state must be IN_PREPARATION, READY, or FULFILLED", response.ErrInvalid)
	}
	return nil
}

// BulkAdvanceHandler moves a batch of Preparation Units to one explicit target
// state in a single transaction. Authority and idempotency are handled once by
// the executor; each unit's own transition runs inside a savepoint so a
// UNIT_NOT_FOUND or INVALID_TRANSITION fails only that unit.
type BulkAdvanceHandler struct{ runner *Runner }

// NewBulkAdvanceHandler creates a BulkAdvanceHandler.
func NewBulkAdvanceHandler(runner *Runner) *BulkAdvanceHandler {
	return &BulkAdvanceHandler{runner: runner}
}

// Handle executes the bulk advance.
//
// Units lock in UUID byte order (sortedBulkSelection) so two overlapping
// commands can never form a lock-order cycle, while the response keeps the
// submitted first-selection order: byID maps each id to its outcome and the
// final slice reads the normalized selection back in order.
func (h *BulkAdvanceHandler) Handle(ctx context.Context, actor Actor,
	cmd BulkAdvanceCommand,
) (int, BulkAdvanceResponse, error) {
	if err := validateBulkAdvance(cmd); err != nil {
		return 0, BulkAdvanceResponse{}, err
	}
	selected := normalizeBulkSelection(cmd.PreparationUnitIDs)
	fingerprintIDs := append([]uuid.UUID(nil), selected...)
	lockOrder := sortedBulkSelection(selected)

	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpBulkAdvance,
		Fingerprint: bulkAdvanceFingerprint{
			PreparationUnitIDs: fingerprintIDs,
			TargetState:        cmd.TargetState,
		},
		Required: []string{CapPreparationOperate},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, BulkAdvanceResponse, AuditRecord, error) {
			byID := make(map[uuid.UUID]BulkAdvanceOutcome, len(selected))
			audits := make([]AuditRecord, 0, len(selected))
			for _, unitID := range lockOrder {
				var transition transitionOutcome
				err := mc.withUnitSavepoint(ctx, func(q *sqlc.Queries) error {
					var err error
					transition, err = applyAdvance(ctx, q, actor, unitID, cmd.TargetState)
					return err
				})
				if err == nil {
					unit := transition.Unit
					byID[unitID] = BulkAdvanceOutcome{
						PreparationUnitID: unitID,
						Status:            BulkStatusAdvanced,
						Unit:              &unit,
					}
					audits = append(audits, transition.Audit)
					continue
				}
				switch {
				case errors.Is(err, ErrUnitNotFound):
					byID[unitID] = failedBulkOutcome(unitID, BulkCodeUnitNotFound)
				case errors.Is(err, ErrInvalidTransition):
					byID[unitID] = failedBulkOutcome(unitID, BulkCodeInvalidTransition)
				default:
					return 0, BulkAdvanceResponse{}, AuditRecord{}, err
				}
			}

			if err := writeAdvanceAudits(ctx, mc.Queries, actor, audits); err != nil {
				return 0, BulkAdvanceResponse{}, AuditRecord{}, err
			}
			outcomes := make([]BulkAdvanceOutcome, 0, len(selected))
			for _, id := range selected {
				outcomes = append(outcomes, byID[id])
			}
			return http.StatusOK, BulkAdvanceResponse{
				TargetState: cmd.TargetState,
				Outcomes:    outcomes,
			}, AuditRecord{}, nil
		})
}

// failedBulkOutcome builds the FAILED outcome for one unit. Unit stays nil so
// its omitempty tag keeps it out of the response, and Code names the reason.
func failedBulkOutcome(unitID uuid.UUID, code string) BulkAdvanceOutcome {
	return BulkAdvanceOutcome{
		PreparationUnitID: unitID,
		Status:            BulkStatusFailed,
		Code:              code,
	}
}

// writeAdvanceAudits batches one PREPARATION_UNIT_ADVANCED audit row per
// successful unit into a single insert. These per-unit events are the complete
// business audit trail of the bulk advance; the executor itself is given a
// zero AuditRecord, so no extra batch-summary event exists.
func writeAdvanceAudits(ctx context.Context, q *sqlc.Queries, actor Actor,
	audits []AuditRecord,
) error {
	if len(audits) == 0 {
		return nil
	}
	detailsBatch := make([]string, len(audits))
	for i, audit := range audits {
		if audit.EventType != EventPreparationUnitAdvanced {
			return fmt.Errorf("unexpected bulk audit type %q", audit.EventType)
		}
		details, err := json.Marshal(audit.Details)
		if err != nil {
			return fmt.Errorf("marshal preparation advance audit: %w", err)
		}
		detailsBatch[i] = string(details)
	}
	if err := q.InsertAuditEventsBatch(ctx, sqlc.InsertAuditEventsBatchParams{
		EventType:    EventPreparationUnitAdvanced,
		ActorID:      actor.StaffID,
		SessionID:    actor.SessionID,
		OccurredAt:   time.Now(),
		DetailsBatch: detailsBatch,
	}); err != nil {
		return fmt.Errorf("insert preparation advance audits: %w", err)
	}
	return nil
}
