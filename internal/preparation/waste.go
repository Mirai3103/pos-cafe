package preparation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// wasteFingerprint is the normalized, credential-free business input a Waste
// stands for: the unit, the reason from the Waste catalog, and the normalized
// note. Waste requires no Manager PIN, so there is no secret to keep out; the
// note is the normalized one, so replays of differently padded input stay
// equal.
type wasteFingerprint struct {
	UnitID uuid.UUID `json:"unit_id"`
	Reason string    `json:"reason"`
	Note   *string   `json:"note"`
}

// WasteUnitHandler records one Waste: the terminal Waste fact, the unit's
// WASTED state and its typed transition, the unacknowledged WASTE alert, and
// the two business audits, all inside the executor's single mutation
// transaction.
type WasteUnitHandler struct{ runner *Runner }

// NewWasteUnitHandler creates a WasteUnitHandler.
func NewWasteUnitHandler(runner *Runner) *WasteUnitHandler {
	return &WasteUnitHandler{runner: runner}
}

// Handle executes the waste.
//
// Reason and note are normalized and validated BEFORE the mutation begins, so
// a malformed request never claims its idempotency key. Success answers 201;
// the route layer maps domain errors through ErrorResponse.
func (h *WasteUnitHandler) Handle(ctx context.Context, actor Actor,
	cmd WasteUnitCommand,
) (int, WasteResponse, error) {
	note := NormalizeCorrectionNote(cmd.Note)
	if err := ValidateWasteReason(cmd.Reason); err != nil {
		return 0, WasteResponse{}, err
	}
	if err := ValidateCorrectionNote(cmd.Reason, note); err != nil {
		return 0, WasteResponse{}, err
	}

	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpWasteUnit,
		Fingerprint: wasteFingerprint{
			UnitID: cmd.UnitID,
			Reason: cmd.Reason,
			Note:   note,
		},
		Required: []string{CapPreparationOperate},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, WasteResponse, AuditRecord, error) {
			response, err := applyWaste(ctx, mc.Queries, actor, cmd.UnitID, cmd.Reason, note)
			if err != nil {
				return 0, WasteResponse{}, AuditRecord{}, err
			}
			// Both business audits were written inside the mutation through
			// writePreparationAudits, so the executor's single-audit step is
			// deliberately given a zero record.
			return http.StatusCreated, response, AuditRecord{}, nil
		})
}

// constraintPreparationWasteUnitUnique is the migration 000013 constraint
// making a Waste a terminal, one-per-unit fact. It is the only database error
// this command maps to a domain outcome.
const constraintPreparationWasteUnitUnique = "preparation_waste_unit_unique"

// mapWasteDBError maps exactly the named unique-Waste constraint to the typed
// lifecycle conflict it represents: a unit that already carries a Waste fact
// cannot be wasted again. Every other PostgreSQL error — including every
// other unique violation — is returned untouched as an infrastructure
// failure.
func mapWasteDBError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) &&
		pgErr.Code == "23505" &&
		pgErr.ConstraintName == constraintPreparationWasteUnitUnique {
		return fmt.Errorf("%w: the unit already carries a waste fact", ErrInvalidTransition)
	}
	return err
}

// applyWaste is the mutation body, ordered so each step's failure leaves the
// transaction — claim included — to the executor's rollback:
//
//  1. lock the unit, mapping a miss to ErrUnitNotFound;
//  2. require IN_PREPARATION or READY, else ErrInvalidTransition;
//  3. choose one database instant for the fact, the state write, the
//     transition, the alert, and both audits;
//  4. insert the Waste fact;
//  5. set the unit state to WASTED without touching in_preparation_at;
//  6. insert the typed transition;
//  7. insert the unacknowledged WASTE alert;
//  8. write both business audits through writePreparationAudits;
//  9. return the complete Waste and alert result from the RETURNING rows,
//     leaving the executor's AuditRecord at zero.
func applyWaste(ctx context.Context, q *sqlc.Queries, actor Actor,
	unitID uuid.UUID, reason string, note *string,
) (WasteResponse, error) {
	// 1. The row lock serializes concurrent lifecycles on this unit: every
	// loser of the race re-reads the winner's committed state here.
	unit, err := q.LockPreparationUnit(ctx, unitID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return WasteResponse{}, fmt.Errorf("%w: %s", ErrUnitNotFound, unitID)
		}
		return WasteResponse{}, fmt.Errorf("lock preparation unit: %w", err)
	}

	// 2. Waste admits exactly two prior states; everything else — queued,
	// fulfilled, cancelled, already wasted — is a lifecycle conflict.
	if unit.State != StateInPreparation && unit.State != StateReady {
		return WasteResponse{}, fmt.Errorf("%w: %s cannot be wasted", ErrInvalidTransition, unit.State)
	}

	// 3. One database clock reading, shared by every write below.
	occurredAt, err := q.GetPreparationCurrentTime(ctx)
	if err != nil {
		return WasteResponse{}, fmt.Errorf("read preparation occurrence time: %w", err)
	}

	var noteValue sql.NullString
	if note != nil {
		noteValue = sql.NullString{String: *note, Valid: true}
	}

	// 4. The Waste fact — one per unit, forever.
	waste, err := q.InsertPreparationWaste(ctx, sqlc.InsertPreparationWasteParams{
		PreparationUnitID:    unit.ID,
		PriorState:           unit.State,
		Reason:               reason,
		Note:                 noteValue,
		ActorStaffIdentityID: actor.StaffID,
		StaffAccessSessionID: actor.SessionID,
		OccurredAt:           occurredAt,
	})
	if err != nil {
		return WasteResponse{}, fmt.Errorf("insert preparation waste: %w", mapWasteDBError(err))
	}

	// 5. SetPreparationUnitState only overwrites in_preparation_at when the
	// target is IN_PREPARATION, so the unit's preparation start survives the
	// WASTED write.
	if err := q.SetPreparationUnitState(ctx, sqlc.SetPreparationUnitStateParams{
		ID: unit.ID, State: StateWasted, OccurredAt: occurredAt,
	}); err != nil {
		return WasteResponse{}, fmt.Errorf("set preparation unit state: %w", err)
	}

	// 6. The transition row is business data a Completed Sale is made of
	// (ADR-027); the audits below record the same moment for a different
	// purpose.
	if err := q.InsertPreparationUnitTransition(ctx, sqlc.InsertPreparationUnitTransitionParams{
		PreparationUnitID:    unit.ID,
		PriorState:           unit.State,
		ResultingState:       StateWasted,
		ActorStaffIdentityID: actor.StaffID,
		StaffAccessSessionID: actor.SessionID,
		OccurredAt:           occurredAt,
	}); err != nil {
		return WasteResponse{}, fmt.Errorf("insert preparation unit transition: %w", err)
	}

	// 7. The WASTE alert is born unacknowledged; only the acknowledgment
	// command ever fills its acknowledgment columns.
	alert, err := q.InsertPreparationAlert(ctx, sqlc.InsertPreparationAlertParams{
		PreparationUnitID:           unit.ID,
		Kind:                        AlertKindWaste,
		Reason:                      reason,
		Note:                        noteValue,
		CreatedByStaffIdentityID:    actor.StaffID,
		CreatedStaffAccessSessionID: actor.SessionID,
		CreatedAt:                   occurredAt,
	})
	if err != nil {
		return WasteResponse{}, fmt.Errorf("insert preparation alert: %w", err)
	}

	// 8. Both business events of this mutation, one batch, one actor, one
	// session, one occurrence time.
	if err := writePreparationAudits(ctx, q, actor, occurredAt, []AuditRecord{
		{
			EventType: EventPreparationUnitWasted,
			Details: map[string]any{
				"preparation_unit_id": unit.ID,
				"prior_state":         unit.State,
				"resulting_state":     StateWasted,
				"waste_id":            waste.ID,
				"reason":              reason,
			},
		},
		{
			EventType: EventPreparationAlertCreated,
			Details: map[string]any{
				"preparation_unit_id": unit.ID,
				"alert_id":            alert.ID,
				"kind":                AlertKindWaste,
				"reason":              reason,
			},
		},
	}); err != nil {
		return WasteResponse{}, err
	}

	// 9. The RETURNING rows are the load of the complete result:
	// ResultingState is the handler's constant and a fresh alert carries no
	// acknowledgment.
	return buildWasteResponse(waste, alert), nil
}

// buildWasteResponse assembles the response from the stored fact rows, so the
// client reads back exactly what the database accepted.
func buildWasteResponse(waste sqlc.PreparationWaste, alert sqlc.PreparationAlert) WasteResponse {
	var wasteNote *string
	if waste.Note.Valid {
		wasteNote = &waste.Note.String
	}
	var alertNote *string
	if alert.Note.Valid {
		alertNote = &alert.Note.String
	}
	return WasteResponse{
		ID:                waste.ID,
		PreparationUnitID: waste.PreparationUnitID,
		PriorState:        waste.PriorState,
		ResultingState:    StateWasted,
		Reason:            waste.Reason,
		Note:              wasteNote,
		OccurredAt:        waste.OccurredAt,
		Alert: AlertResponse{
			ID:                alert.ID,
			PreparationUnitID: alert.PreparationUnitID,
			Kind:              alert.Kind,
			Reason:            alert.Reason,
			Note:              alertNote,
			CreatedAt:         alert.CreatedAt,
		},
	}
}
