package preparation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/platform/database/sqlc"
	"github.com/google/uuid"
)

// cancelUnitsFingerprint is the normalized, credential-free business input a
// Cancellation/Change stands for. The selection is the UUID-sorted copy, so
// two requests that name the same units in different orders are the same
// request; a Cancellation/Change requires no Manager Approval, so there is no
// credential to keep out. The note is the normalized one, so
// replays of differently padded input stay equal.
type cancelUnitsFingerprint struct {
	PreparationUnitIDs []uuid.UUID `json:"preparation_unit_ids"`
	Kind               string      `json:"kind"`
	ReplacementOrderID *uuid.UUID  `json:"replacement_order_id"`
	Reason             string      `json:"reason"`
	Note               *string     `json:"note"`
}

// cancelUnitsFingerprintFor builds the credential-free fingerprint of a
// Cancellation/Change from its already-validated command and normalized note.
// The ids are a fresh UUID-sorted copy; the command's own slice keeps request
// order for the response and the per-unit writes.
func cancelUnitsFingerprintFor(cmd CancelUnitsCommand, note *string) cancelUnitsFingerprint {
	return cancelUnitsFingerprint{
		PreparationUnitIDs: uuidSortedCopy(cmd.PreparationUnitIDs),
		Kind:               cmd.Kind,
		ReplacementOrderID: cmd.ReplacementOrderID,
		Reason:             cmd.Reason,
		Note:               note,
	}
}

// CancelUnitsHandler terminates 1 through MaxCancellationUnits queued
// Preparation Units in one all-or-nothing transaction: each selected unit
// becomes CANCELLED with its typed transition, Cancellation fact, and
// CANCELLATION or CHANGE alert, while each charged standard unit reduces its
// Check's live charge through one append-only CANCELLATION Charge Adjustment
// and the Check settles when the corrected balance reaches zero.
type CancelUnitsHandler struct{ runner *Runner }

// NewCancelUnitsHandler creates a CancelUnitsHandler.
func NewCancelUnitsHandler(runner *Runner) *CancelUnitsHandler {
	return &CancelUnitsHandler{runner: runner}
}

// Handle executes the Cancellation/Change.
//
// The command is validated and its note normalized BEFORE the mutation
// begins, so a malformed request never claims its idempotency key. The
// command requires sales.operate and no Manager Approval. Success answers 200;
// the route layer maps domain errors through ErrorResponse.
func (h *CancelUnitsHandler) Handle(ctx context.Context, actor Actor,
	cmd CancelUnitsCommand,
) (int, CancelUnitsResponse, error) {
	note := NormalizeCorrectionNote(cmd.Note)
	if err := ValidateCancelUnitsCommand(cmd, note); err != nil {
		return 0, CancelUnitsResponse{}, err
	}

	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpCancelUnits,
		Fingerprint: cancelUnitsFingerprintFor(cmd, note),
		Required:    []string{CapSalesOperate},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, CancelUnitsResponse, AuditRecord, error) {
			result, err := applyCancelUnits(ctx, mc.Queries, actor, cmd, note)
			if err != nil {
				return 0, CancelUnitsResponse{}, AuditRecord{}, err
			}
			// Every business audit was written inside the mutation through
			// writePreparationAudits, sharing the batch's one timestamp, so
			// the pipeline's single-audit step is deliberately given a zero
			// record.
			return http.StatusOK, result, AuditRecord{}, nil
		})
}

// applyCancelUnits is the mutation body, ordered so each step's failure leaves
// the transaction — claim included — to the pipeline's rollback. There are no
// savepoints: a partial financial correction is not a business state.
//
//  1. resolve every selected unit and its charge mapping WITHOUT locks,
//     rejecting missing ids and stale states before any lock is taken;
//  2. for CHANGE, resolve and validate the already-submitted replacement
//     Order in the same Session, also without locks;
//  3. lock the affected Checks, the one shared Service Session, the one open
//     Sales Shift, and the selected units, in that order;
//  4. re-resolve the unit-to-allocation mapping under those locks and refuse
//     if a concurrent restructuring changed it;
//  5. verify every affected Check's stored charge against base allocations
//     less existing live adjustments, then plan one CANCELLATION adjustment
//     per charged unit;
//  6. insert the adjustments, update each Check's charge once, and settle
//     every Check whose corrected balance reaches zero with all four
//     settlement-evidence columns;
//  7. per unit in request order, append the state, transition, Cancellation
//     fact, and alert; batch all audits under the batch's one timestamp;
//  8. return one outcome and one alert per input unit in request order.
func applyCancelUnits(ctx context.Context, q *sqlc.Queries, actor Actor,
	cmd CancelUnitsCommand, note *string,
) (CancelUnitsResponse, error) {
	ids := cmd.PreparationUnitIDs

	// 1. The lock-free resolution runs on the mutation's own transaction: the
	// reads take no row locks, so the batch rejects its missing ids and stale
	// states before serializing on anything.
	resolved, sessionID, err := resolveCancelSelection(ctx, q, ids)
	if err != nil {
		return CancelUnitsResponse{}, err
	}

	// 2. CHANGE resolves its replacement Order before any lock; Orders are
	// immutable after Submit.
	if err := validateCancellationReplacement(ctx, q, cmd, sessionID, resolved); err != nil {
		return CancelUnitsResponse{}, err
	}

	// 3. Checks, Session, Shift, then units.
	checkIDs := cancelCheckIDs(resolved, ids)
	shiftID, err := lockCancellationScope(ctx, q, checkIDs, sessionID, ids)
	if err != nil {
		return CancelUnitsResponse{}, err
	}

	// 4. Refuse a mapping that changed between the resolution and the locks.
	if err := revalidateCancelMapping(ctx, q, ids, resolved); err != nil {
		return CancelUnitsResponse{}, err
	}

	// 5. One database instant for the whole batch and transaction.
	occurredAt, err := q.GetPreparationCurrentTime(ctx)
	if err != nil {
		return CancelUnitsResponse{}, fmt.Errorf("read cancellation occurrence time: %w", err)
	}
	plans, err := planCancelCheckCharges(ctx, q, checkIDs, sessionID, resolved, ids)
	if err != nil {
		return CancelUnitsResponse{}, err
	}

	// 6. One append-only adjustment per charged unit, then one charge update
	// per Check, then settlement.
	adjustmentByUnit, err := insertCancellationAdjustments(ctx, q, cmd,
		resolved, shiftID, occurredAt)
	if err != nil {
		return CancelUnitsResponse{}, err
	}
	settledChecks, err := applyCancelCheckConsequences(ctx, q, actor, shiftID,
		occurredAt, checkIDs, plans)
	if err != nil {
		return CancelUnitsResponse{}, err
	}

	// 7. Per unit, in request order, then one audit batch, one actor, one
	// session, one occurrence time.
	audits := make([]AuditRecord, 0, len(ids)*3+len(checkIDs))
	audits = appendSettledCheckAudits(audits, checkIDs, settledChecks, plans, shiftID)
	writes := cancelUnitWrites{
		actor:            actor,
		cmd:              cmd,
		note:             nullString(note),
		occurredAt:       occurredAt,
		resolved:         resolved,
		adjustmentByUnit: adjustmentByUnit,
	}
	result, audits, err := writes.apply(ctx, q, plans, audits)
	if err != nil {
		return CancelUnitsResponse{}, err
	}
	if err := writePreparationAudits(ctx, q, actor, occurredAt, audits); err != nil {
		return CancelUnitsResponse{}, err
	}

	// 8.
	return result, nil
}

// lockCancellationScope takes every lock a Cancellation/Change holds, in the
// global order: the affected Checks ascending by UUID, then the one shared
// Service Session (which must be ACTIVE), then the one open Sales Shift FOR
// SHARE, and the selected units last, ascending by UUID. It returns the open
// Shift's id.
func lockCancellationScope(ctx context.Context, q *sqlc.Queries,
	checkIDs []uuid.UUID, sessionID uuid.UUID, ids []uuid.UUID,
) (uuid.UUID, error) {
	if err := lockCancellationChecks(ctx, q, checkIDs); err != nil {
		return uuid.Nil, err
	}
	if err := lockCancellationSession(ctx, q, sessionID); err != nil {
		return uuid.Nil, err
	}
	// Settlement evidence names the open Shift, and Shift closure must stay
	// excluded for the whole transaction.
	shift, err := q.LockOpenSalesShiftForCancellation(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return uuid.Nil, fmt.Errorf("%w: no sales shift is open", ErrOpenShiftRequired)
		}
		return uuid.Nil, fmt.Errorf("lock open sales shift: %w", err)
	}
	if err := lockCancellationUnits(ctx, q, ids); err != nil {
		return uuid.Nil, err
	}
	return shift.ID, nil
}

// lockCancellationChecks locks every affected Check, deduplicated and
// ascending by UUID.
func lockCancellationChecks(ctx context.Context, q *sqlc.Queries, checkIDs []uuid.UUID) error {
	lockedChecks, err := q.LockPreparationChecksForCancellation(ctx, checkIDs)
	if err != nil {
		return fmt.Errorf("lock preparation checks for cancellation: %w", err)
	}
	if len(lockedChecks) != len(checkIDs) {
		return fmt.Errorf(
			"lock preparation checks for cancellation: expected %d rows, got %d",
			len(checkIDs), len(lockedChecks))
	}
	return nil
}

// lockCancellationSession locks the one shared Service Session, after its
// Checks, and requires it ACTIVE.
func lockCancellationSession(ctx context.Context, q *sqlc.Queries, sessionID uuid.UUID) error {
	sessions, err := q.LockPreparationSessionsForCancellation(ctx, []uuid.UUID{sessionID})
	if err != nil {
		return fmt.Errorf("lock preparation service session: %w", err)
	}
	if len(sessions) != 1 || sessions[0].ID != sessionID {
		return fmt.Errorf(
			"lock preparation service session: expected %s, got %d rows",
			sessionID, len(sessions))
	}
	if sessions[0].State != stateServiceSessionActive {
		return fmt.Errorf("%w: %s is %s",
			ErrServiceSessionClosed, sessionID, sessions[0].State)
	}
	return nil
}

// lockCancellationUnits locks the selected units in ascending UUID order and
// requires every one of them still QUEUED.
func lockCancellationUnits(ctx context.Context, q *sqlc.Queries, ids []uuid.UUID) error {
	sortedUnitIDs := uuidSortedCopy(ids)
	lockedUnits, err := q.LockPreparationUnitsForCancellation(ctx, sortedUnitIDs)
	if err != nil {
		return fmt.Errorf("lock preparation units for cancellation: %w", err)
	}
	if len(lockedUnits) != len(sortedUnitIDs) {
		return fmt.Errorf(
			"lock preparation units for cancellation: expected %d rows, got %d",
			len(sortedUnitIDs), len(lockedUnits))
	}
	lockedByID := make(map[uuid.UUID]sqlc.LockPreparationUnitsForCancellationRow,
		len(lockedUnits))
	for _, row := range lockedUnits {
		lockedByID[row.ID] = row
	}
	for _, id := range ids {
		if state := lockedByID[id].State; state != StateQueued {
			return fmt.Errorf("%w: %s is %s",
				ErrCancellationSourceNotQueued, id, state)
		}
	}
	return nil
}

// cancelUnitWrites is what every per-unit write of one Cancellation/Change
// shares.
type cancelUnitWrites struct {
	actor            Actor
	cmd              CancelUnitsCommand
	note             sql.NullString
	occurredAt       time.Time
	resolved         map[uuid.UUID]cancelResolvedUnit
	adjustmentByUnit map[uuid.UUID]uuid.UUID
}

// apply cancels every selected unit in request order and returns one outcome
// and one alert per unit, with the unit, alert, and charge audits appended to
// audits.
func (w cancelUnitWrites) apply(ctx context.Context, q *sqlc.Queries,
	plans map[uuid.UUID]cancelCheckPlan, audits []AuditRecord,
) (CancelUnitsResponse, []AuditRecord, error) {
	ids := w.cmd.PreparationUnitIDs

	// Per-unit CHECK_CHARGE_ADJUSTED audits report each unit's own incremental
	// step, not the batch's aggregate before/after: a running charge per Check,
	// seeded from its pre-batch stored charge and stepped down by each charged
	// unit's immutable price in request order.
	runningChargeByCheck := make(map[uuid.UUID]int64, len(plans))
	for checkID, plan := range plans {
		runningChargeByCheck[checkID] = plan.StoredChargeVND
	}

	// The bar projection each alert carries (service_number, item_name,
	// unit_number) is immutable across this cancellation, so one batched read
	// replaces a per-unit GetPreparationUnit round trip.
	unitViews, err := loadUnits(ctx, q, ids)
	if err != nil {
		return CancelUnitsResponse{}, nil, err
	}

	outcomes := make([]CancellationOutcome, len(ids))
	alerts := make([]QueueAlertResponse, len(ids))
	for index, id := range ids {
		unit := w.resolved[id]
		fact, alert, err := w.cancelUnit(ctx, q, id)
		if err != nil {
			return CancelUnitsResponse{}, nil, err
		}
		unitView, ok := unitViews[id]
		if !ok {
			return CancelUnitsResponse{}, nil, fmt.Errorf("%w: %s", ErrUnitNotFound, id)
		}

		adjustmentID := w.adjustmentID(id)
		outcomes[index] = CancellationOutcome{
			CancellationID:     fact.ID,
			PreparationUnitID:  id,
			PriorState:         StateQueued,
			ResultingState:     StateCancelled,
			ChargeAdjustmentID: adjustmentID,
			ChargeRemovedVND:   unit.UnitPriceVND,
			OccurredAt:         fact.OccurredAt,
		}
		alerts[index] = buildCancellationAlert(alert, unitView)
		audits = append(audits, w.unitAudits(id, fact.ID, alert.ID, adjustmentID)...)

		if adjustmentID != nil {
			chargeBeforeVND := runningChargeByCheck[unit.CheckID]
			chargeAfterVND, err := cancellationNewCharge(chargeBeforeVND, unit.UnitPriceVND)
			if err != nil {
				return CancelUnitsResponse{}, nil, err
			}
			runningChargeByCheck[unit.CheckID] = chargeAfterVND
			audits = append(audits, AuditRecord{
				EventType: EventCheckChargeAdjusted,
				Details: map[string]any{
					"check_id":             unit.CheckID,
					"charge_adjustment_id": *adjustmentID,
					"preparation_unit_id":  id,
					"amount_vnd":           unit.UnitPriceVND,
					"charge_before_vnd":    chargeBeforeVND,
					"charge_after_vnd":     chargeAfterVND,
				},
			})
		}
	}
	return CancelUnitsResponse{Outcomes: outcomes, Alerts: alerts}, audits, nil
}

// cancelUnit writes one unit's CANCELLED state, its typed transition, its
// Cancellation fact, and its CANCELLATION or CHANGE alert.
func (w cancelUnitWrites) cancelUnit(ctx context.Context, q *sqlc.Queries,
	id uuid.UUID,
) (sqlc.PreparationCancellation, sqlc.PreparationAlert, error) {
	var noFact sqlc.PreparationCancellation
	var noAlert sqlc.PreparationAlert
	actor := w.actor

	if err := q.SetPreparationUnitState(ctx, sqlc.SetPreparationUnitStateParams{
		ID: id, State: StateCancelled, OccurredAt: w.occurredAt,
	}); err != nil {
		return noFact, noAlert, fmt.Errorf("set preparation unit state: %w", err)
	}
	if err := q.InsertPreparationUnitTransition(ctx,
		sqlc.InsertPreparationUnitTransitionParams{
			PreparationUnitID:    id,
			PriorState:           StateQueued,
			ResultingState:       StateCancelled,
			ActorStaffIdentityID: actor.StaffID,
			StaffAccessSessionID: actor.SessionID,
			OccurredAt:           w.occurredAt,
		}); err != nil {
		return noFact, noAlert, fmt.Errorf("insert preparation unit transition: %w", err)
	}

	var chargeAdjustmentID uuid.NullUUID
	if adjustmentID, found := w.adjustmentByUnit[id]; found {
		chargeAdjustmentID = uuid.NullUUID{UUID: adjustmentID, Valid: true}
	}
	var replacementOrderID uuid.NullUUID
	if w.cmd.ReplacementOrderID != nil {
		replacementOrderID = uuid.NullUUID{UUID: *w.cmd.ReplacementOrderID, Valid: true}
	}
	fact, err := q.InsertPreparationCancellation(ctx,
		sqlc.InsertPreparationCancellationParams{
			PreparationUnitID:    id,
			Kind:                 w.cmd.Kind,
			ChargeAdjustmentID:   chargeAdjustmentID,
			ReplacementOrderID:   replacementOrderID,
			Reason:               w.cmd.Reason,
			Note:                 w.note,
			ActorStaffIdentityID: actor.StaffID,
			StaffAccessSessionID: actor.SessionID,
			OccurredAt:           w.occurredAt,
		})
	if err != nil {
		return noFact, noAlert, fmt.Errorf("insert preparation cancellation: %w",
			mapCancelDBError(err))
	}

	alert, err := q.InsertPreparationAlert(ctx, sqlc.InsertPreparationAlertParams{
		PreparationUnitID:           id,
		Kind:                        w.cmd.Kind,
		Reason:                      w.cmd.Reason,
		Note:                        w.note,
		CreatedByStaffIdentityID:    actor.StaffID,
		CreatedStaffAccessSessionID: actor.SessionID,
		CreatedAt:                   w.occurredAt,
	})
	if err != nil {
		return noFact, noAlert, fmt.Errorf("insert preparation alert: %w", err)
	}
	return fact, alert, nil
}

// adjustmentID returns the CANCELLATION adjustment written for a charged
// unit, or nil for an uncharged one.
func (w cancelUnitWrites) adjustmentID(id uuid.UUID) *uuid.UUID {
	adjustmentID, found := w.adjustmentByUnit[id]
	if !found {
		return nil
	}
	return &adjustmentID
}

// unitAudits builds one cancelled unit's PREPARATION_UNIT_CANCELLED and
// PREPARATION_ALERT_CREATED audits.
func (w cancelUnitWrites) unitAudits(id, cancellationID, alertID uuid.UUID,
	adjustmentID *uuid.UUID,
) []AuditRecord {
	return []AuditRecord{
		{
			EventType: EventPreparationUnitCancelled,
			Details: map[string]any{
				"preparation_unit_id":  id,
				"cancellation_id":      cancellationID,
				"kind":                 w.cmd.Kind,
				"prior_state":          StateQueued,
				"resulting_state":      StateCancelled,
				"charge_adjustment_id": adjustmentID,
				"charge_removed_vnd":   w.resolved[id].UnitPriceVND,
				"reason":               w.cmd.Reason,
			},
		},
		{
			EventType: EventPreparationAlertCreated,
			Details: map[string]any{
				"preparation_unit_id": id,
				"alert_id":            alertID,
				"kind":                w.cmd.Kind,
				"reason":              w.cmd.Reason,
			},
		},
	}
}

// buildCancellationAlert projects one stored alert with the unit identity the
// queue reads at read time. A Cancellation/Change alert resolves no Waste
// fact, so WasteID stays nil.
func buildCancellationAlert(alert sqlc.PreparationAlert,
	unit UnitResponse,
) QueueAlertResponse {
	var note *string
	if alert.Note.Valid {
		value := alert.Note.String
		note = &value
	}
	return QueueAlertResponse{
		ID:                alert.ID,
		Kind:              alert.Kind,
		PreparationUnitID: alert.PreparationUnitID,
		ServiceNumber:     unit.ServiceNumber,
		ItemName:          unit.ItemName,
		UnitNumber:        unit.UnitNumber,
		Reason:            alert.Reason,
		Note:              note,
		CreatedAt:         alert.CreatedAt,
	}
}
