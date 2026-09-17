package preparation

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"sort"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

// correctStateFingerprint is the normalized, credential-free business input a
// State Correction stands for. ManagerPIN is deliberately absent — a PIN must
// never enter a fingerprint, so replays stay comparable without ever hashing
// a secret (spec §7.5). The correction command builds it from the normalized
// command, preserving request order.
type correctStateFingerprint struct {
	PreparationUnitIDs []uuid.UUID `json:"preparation_unit_ids"`
	TargetState        string      `json:"target_state"`
	Reason             string      `json:"reason"`
	Note               *string     `json:"note"`
}

// correctStateFingerprintFor builds the credential-free fingerprint of a
// State Correction from its already-validated command and normalized note.
// The ids stay in the request's own order: the fingerprint stands for what
// was asked, while the lock order is a separate sorted copy owned by the
// callback.
func correctStateFingerprintFor(cmd CorrectStateCommand, note *string) correctStateFingerprint {
	return correctStateFingerprint{
		PreparationUnitIDs: cmd.PreparationUnitIDs,
		TargetState:        cmd.TargetState,
		Reason:             cmd.Reason,
		Note:               note,
	}
}

// uuidSortedCopy returns a fresh copy of ids sorted in UUID byte order
// (bytes.Compare over the raw 16 bytes), leaving the caller's slice and its
// backing array untouched: the request order drives the fingerprint and the
// response, while byte order drives every multi-row lock, so concurrent
// corrections of overlapping selections serialize instead of deadlocking.
func uuidSortedCopy(ids []uuid.UUID) []uuid.UUID {
	sorted := make([]uuid.UUID, len(ids))
	copy(sorted, ids)
	sort.Slice(sorted, func(i, j int) bool {
		return bytes.Compare(sorted[i][:], sorted[j][:]) < 0
	})
	return sorted
}

// CorrectStateHandler reverses 1 through MaxCorrectionUnits Preparation Units
// one step along the chain — IN_PREPARATION to QUEUED, READY to
// IN_PREPARATION, FULFILLED to READY — as one all-or-nothing Manager command:
// every unit's state write, reverse transition, correction fact, and
// PREPARATION_STATE_CORRECTED audit commit together or not at all. It changes
// no Order Item, allocation, Check charge, Payment, Refund, or Comp.
type CorrectStateHandler struct{ runner *Runner }

// NewCorrectStateHandler creates a CorrectStateHandler.
func NewCorrectStateHandler(runner *Runner) *CorrectStateHandler {
	return &CorrectStateHandler{runner: runner}
}

// Handle executes the correction.
//
// The command is validated and its note normalized BEFORE the mutation
// begins, so a malformed request never claims its idempotency key. The
// executor's Manager self-PIN gate verifies the actor's own current PIN
// inside the mutation transaction, before the fingerprint is hashed and
// before any replay — this handler never re-verifies it and never lets it
// reach the fingerprint, the stored result, a fact, an audit, or a log.
// Success answers 200; the route layer maps domain errors through
// ErrorResponse.
func (h *CorrectStateHandler) Handle(ctx context.Context, actor Actor,
	cmd CorrectStateCommand,
) (int, CorrectStateResponse, error) {
	note := NormalizeCorrectionNote(cmd.Note)
	if err := ValidateCorrectStateCommand(cmd); err != nil {
		return 0, CorrectStateResponse{}, err
	}

	spec := MutationSpec{
		RequestID:         cmd.RequestID,
		Operation:         OpCorrectState,
		Fingerprint:       correctStateFingerprintFor(cmd, note),
		Required:          []string{CapPreparationOperate},
		RequireManagerPIN: true,
		ManagerPIN:        cmd.ManagerPIN,
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, CorrectStateResponse, AuditRecord, error) {
			response, err := applyCorrection(ctx, mc.Queries, actor, cmd, note)
			if err != nil {
				return 0, CorrectStateResponse{}, AuditRecord{}, err
			}
			// The per-unit business audits were written inside the mutation
			// through writePreparationAudits, sharing the correction's
			// timestamp, so the executor's single-audit step is deliberately
			// given a zero record.
			return http.StatusOK, response, AuditRecord{}, nil
		})
}

// applyCorrection is the mutation body, ordered so each step's failure leaves
// the transaction — claim included — to the executor's rollback. There are no
// savepoints: one missing id, one stale state, or one closed Session rejects
// the complete batch, because a half-reversed selection is exactly the
// inconsistency the command exists to prevent.
//
//  1. resolve every selected unit and its owning Session WITHOUT locks,
//     rejecting missing ids before any lock is taken;
//  2. deduplicate the Session ids and lock the Sessions in UUID byte order —
//     the session-first serialization shared with Service Session closure;
//  3. require every locked Session ACTIVE, else the whole batch is refused;
//  4. lock the units in UUID byte order;
//  5. revalidate every current state against RequiredPriorState(target) —
//     the loser of a concurrent advance or correction re-reads the winner's
//     committed state here and refuses the entire batch;
//  6. choose one database instant for every write of the batch;
//  7. for each unit in request order, set the corrected state, insert the
//     reverse transition, insert the correction fact, and queue the audit;
//  8. batch-write all audits through writePreparationAudits;
//  9. return the outcomes in original request order, leaving the executor's
//     AuditRecord at zero.
func applyCorrection(ctx context.Context, q *sqlc.Queries, actor Actor,
	cmd CorrectStateCommand, note *string,
) (CorrectStateResponse, error) {
	prior, ok := RequiredPriorState(cmd.TargetState)
	if !ok {
		// Unreachable — the boundary validated the target — but the callback
		// refuses rather than invent a prior state.
		return CorrectStateResponse{}, fmt.Errorf("%w: %q is not a correction target",
			ErrInvalidTransition, cmd.TargetState)
	}

	// 1. The lock-free resolution runs on the mutation's own transaction: the
	// reads take no row locks, so the batch rejects its missing ids before
	// serializing on anything.
	resolved, err := q.ListPreparationUnitsForCorrection(ctx, cmd.PreparationUnitIDs)
	if err != nil {
		return CorrectStateResponse{}, fmt.Errorf("resolve preparation units for correction: %w", err)
	}
	byID := make(map[uuid.UUID]sqlc.ListPreparationUnitsForCorrectionRow, len(resolved))
	for _, row := range resolved {
		byID[row.ID] = row
	}
	for _, id := range cmd.PreparationUnitIDs {
		if _, found := byID[id]; !found {
			return CorrectStateResponse{}, fmt.Errorf("%w: %s", ErrUnitNotFound, id)
		}
	}

	// 2. Session-first locking in byte order, deduplicated so a batch spread
	// over one Session still takes exactly one lock.
	sessionIDs := make([]uuid.UUID, 0, len(resolved))
	seenSessions := make(map[uuid.UUID]struct{}, len(resolved))
	for _, row := range resolved {
		if _, dup := seenSessions[row.ServiceSessionID]; dup {
			continue
		}
		seenSessions[row.ServiceSessionID] = struct{}{}
		sessionIDs = append(sessionIDs, row.ServiceSessionID)
	}
	sessions, err := q.LockPreparationServiceSessions(ctx, uuidSortedCopy(sessionIDs))
	if err != nil {
		return CorrectStateResponse{}, fmt.Errorf("lock preparation service sessions: %w", err)
	}
	if len(sessions) != len(sessionIDs) {
		return CorrectStateResponse{}, fmt.Errorf(
			"lock preparation service sessions: expected %d rows, got %d",
			len(sessionIDs), len(sessions))
	}

	// 3. Every owning Session must still be ACTIVE: one closed Session
	// refuses the whole batch before any unit is touched.
	for _, session := range sessions {
		if session.State != stateServiceSessionActive {
			return CorrectStateResponse{}, fmt.Errorf("%w: %s is %s",
				ErrServiceSessionClosed, session.ID, session.State)
		}
	}

	// 4. The units lock in byte order after their Sessions.
	locked, err := q.LockPreparationUnitsForCorrection(ctx, uuidSortedCopy(cmd.PreparationUnitIDs))
	if err != nil {
		return CorrectStateResponse{}, fmt.Errorf("lock preparation units for correction: %w", err)
	}
	if len(locked) != len(cmd.PreparationUnitIDs) {
		return CorrectStateResponse{}, fmt.Errorf(
			"lock preparation units for correction: expected %d rows, got %d",
			len(cmd.PreparationUnitIDs), len(locked))
	}
	currentState := make(map[uuid.UUID]string, len(locked))
	for _, row := range locked {
		currentState[row.ID] = row.State
	}

	// 5. Every unit must sit exactly one step ahead of the target. This is
	// the revalidation the whole design hangs on: whoever holds the locks
	// last reads the winner's committed state and refuses.
	for _, id := range cmd.PreparationUnitIDs {
		if state := currentState[id]; state != prior {
			return CorrectStateResponse{}, fmt.Errorf(
				"%w: %s is %s, a correction to %s requires %s",
				ErrInvalidTransition, id, state, cmd.TargetState, prior)
		}
	}

	// 6. One database clock reading, shared by every write below.
	occurredAt, err := q.GetPreparationCurrentTime(ctx)
	if err != nil {
		return CorrectStateResponse{}, fmt.Errorf("read preparation occurrence time: %w", err)
	}

	var noteValue sql.NullString
	if note != nil {
		noteValue = sql.NullString{String: *note, Valid: true}
	}

	// 7. Per unit, in request order so the writes, the outcomes, and the
	// audit batch all follow the selection the client sent.
	outcomes := make([]CorrectStateOutcome, 0, len(cmd.PreparationUnitIDs))
	audits := make([]AuditRecord, 0, len(cmd.PreparationUnitIDs))
	for _, id := range cmd.PreparationUnitIDs {
		// SetPreparationUnitCorrectedState clears in_preparation_at only
		// when the target is QUEUED; the later targets keep the recorded
		// preparation start.
		if err := q.SetPreparationUnitCorrectedState(ctx, sqlc.SetPreparationUnitCorrectedStateParams{
			ResultingState: cmd.TargetState,
			ID:             id,
		}); err != nil {
			return CorrectStateResponse{}, fmt.Errorf("set preparation unit corrected state: %w", err)
		}

		if err := q.InsertPreparationUnitTransition(ctx, sqlc.InsertPreparationUnitTransitionParams{
			PreparationUnitID:    id,
			PriorState:           currentState[id],
			ResultingState:       cmd.TargetState,
			ActorStaffIdentityID: actor.StaffID,
			StaffAccessSessionID: actor.SessionID,
			OccurredAt:           occurredAt,
		}); err != nil {
			return CorrectStateResponse{}, fmt.Errorf("insert preparation unit transition: %w", err)
		}

		fact, err := q.InsertPreparationStateCorrection(ctx, sqlc.InsertPreparationStateCorrectionParams{
			PreparationUnitID:    id,
			PriorState:           currentState[id],
			ResultingState:       cmd.TargetState,
			Reason:               cmd.Reason,
			Note:                 noteValue,
			ActorStaffIdentityID: actor.StaffID,
			StaffAccessSessionID: actor.SessionID,
			OccurredAt:           occurredAt,
		})
		if err != nil {
			return CorrectStateResponse{}, fmt.Errorf("insert preparation state correction: %w", err)
		}

		outcomes = append(outcomes, CorrectStateOutcome{
			CorrectionID:      fact.ID,
			PreparationUnitID: id,
			PriorState:        currentState[id],
			ResultingState:    cmd.TargetState,
			CorrectedAt:       fact.OccurredAt,
		})
		audits = append(audits, AuditRecord{
			EventType: EventPreparationStateCorrected,
			Details: map[string]any{
				"preparation_unit_id": id,
				"correction_id":       fact.ID,
				"prior_state":         currentState[id],
				"resulting_state":     cmd.TargetState,
				"reason":              cmd.Reason,
			},
		})
	}

	// 8. One batch, one actor, one session, one occurrence time — the same
	// instant as the correction above.
	if err := writePreparationAudits(ctx, q, actor, occurredAt, audits); err != nil {
		return CorrectStateResponse{}, err
	}

	// 9. The outcomes follow the original request order; the executor's own
	// audit record stays at zero.
	return CorrectStateResponse{Outcomes: outcomes}, nil
}
