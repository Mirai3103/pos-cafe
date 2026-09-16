package preparation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

// AdvanceUnitHandler moves one Preparation Unit along the linear chain from
// queued to fulfilled.
type AdvanceUnitHandler struct{ runner *Runner }

// NewAdvanceUnitHandler creates an AdvanceUnitHandler.
func NewAdvanceUnitHandler(runner *Runner) *AdvanceUnitHandler {
	return &AdvanceUnitHandler{runner: runner}
}

// advanceFingerprint is the normalized business input this request stands for.
type advanceFingerprint struct {
	UnitID      uuid.UUID `json:"unit_id"`
	TargetState string    `json:"target_state"`
}

// Handle executes the advance.
func (h *AdvanceUnitHandler) Handle(ctx context.Context, actor Actor,
	cmd AdvanceUnitCommand,
) (int, UnitResponse, error) {
	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpAdvanceUnit,
		Fingerprint: advanceFingerprint{UnitID: cmd.UnitID, TargetState: cmd.TargetState},
		Required:    []string{CapPreparationOperate},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, UnitResponse, AuditRecord, error) {
			outcome, err := applyAdvance(ctx, mc.Queries, actor, cmd.UnitID, cmd.TargetState)
			if err != nil {
				return 0, UnitResponse{}, AuditRecord{}, err
			}
			return http.StatusOK, outcome.Unit, outcome.Audit, nil
		})
}

// transitionOutcome is the result of one advance: the unit as it now reads,
// plus the audit record the mutation layer should persist.
type transitionOutcome struct {
	Unit  UnitResponse
	Audit AuditRecord
}

// applyAdvance moves one Preparation Unit to an explicit target state and is
// the single transition implementation shared by the single-unit command and
// the bulk queue advance: lock, validate, stamp, insert the transition row.
func applyAdvance(ctx context.Context, q *sqlc.Queries, actor Actor,
	unitID uuid.UUID, target string,
) (transitionOutcome, error) {
	var zero transitionOutcome
	if !IsAdvanceTarget(target) {
		return zero, fmt.Errorf("%w: %q is not an advance target", ErrInvalidTransition, target)
	}

	unit, err := q.LockPreparationUnit(ctx, unitID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return zero, fmt.Errorf("%w: %s", ErrUnitNotFound, unitID)
		}
		return zero, fmt.Errorf("lock preparation unit: %w", err)
	}
	if !IsLegalAdvance(unit.State, target) {
		return zero, fmt.Errorf("%w: %s cannot advance to %s", ErrInvalidTransition, unit.State, target)
	}

	occurredAt := time.Now()
	if err := q.SetPreparationUnitState(ctx, sqlc.SetPreparationUnitStateParams{
		ID: unit.ID, State: target, OccurredAt: occurredAt,
	}); err != nil {
		return zero, fmt.Errorf("set preparation unit state: %w", err)
	}
	// The transition row is business data a Completed Sale is made of,
	// not a derived report (ADR-027). The audit event below records
	// the same moment for a different purpose.
	if err := q.InsertPreparationUnitTransition(ctx, sqlc.InsertPreparationUnitTransitionParams{
		PreparationUnitID:    unit.ID,
		PriorState:           unit.State,
		ResultingState:       target,
		ActorStaffIdentityID: actor.StaffID,
		StaffAccessSessionID: actor.SessionID,
		OccurredAt:           occurredAt,
	}); err != nil {
		return zero, fmt.Errorf("insert preparation unit transition: %w", err)
	}

	out, err := loadUnit(ctx, q, unit.ID)
	if err != nil {
		return zero, err
	}
	return transitionOutcome{
		Unit: out,
		Audit: AuditRecord{
			EventType: EventPreparationUnitAdvanced,
			Details: map[string]any{
				"preparation_unit_id": unit.ID,
				"prior_state":         unit.State,
				"resulting_state":     target,
			},
		},
	}, nil
}

// loadUnit reads one Preparation Unit back after the update.
func loadUnit(ctx context.Context, q *sqlc.Queries, unitID uuid.UUID) (UnitResponse, error) {
	row, err := q.GetPreparationUnit(ctx, unitID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return UnitResponse{}, fmt.Errorf("%w: %s", ErrUnitNotFound, unitID)
		}
		return UnitResponse{}, fmt.Errorf("load preparation unit: %w", err)
	}
	mods := make([]UnitModifierResponse, 0)
	if len(row.Modifiers) > 0 {
		if err := json.Unmarshal(row.Modifiers, &mods); err != nil {
			return UnitResponse{}, fmt.Errorf("decode preparation unit modifiers: %w", err)
		}
	}
	var sizeName, note *string
	if row.SizeName.Valid {
		sizeName = &row.SizeName.String
	}
	if row.PreparationNote.Valid {
		note = &row.PreparationNote.String
	}
	var inPreparationAt *time.Time
	if row.InPreparationAt.Valid {
		value := row.InPreparationAt.Time
		inPreparationAt = &value
	}
	return UnitResponse{
		ID:              row.ID,
		OrderItemID:     row.OrderItemID,
		UnitNumber:      row.UnitNumber,
		State:           row.State,
		ServiceNumber:   row.ServiceNumber,
		CategoryName:    row.CategoryName,
		ItemName:        row.ItemName,
		SizeName:        sizeName,
		Modifiers:       mods,
		PreparationNote: note,
		QueuedAt:        row.QueuedAt,
		InPreparationAt: inPreparationAt,
	}, nil
}
