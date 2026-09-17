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

// remakeFingerprint is the normalized, credential-free business input a Remake
// stands for: the Waste being replaced, the reason from the Remake catalog,
// and the normalized note. Remake requires no Manager PIN, so there is no
// secret to keep out; the note is the normalized one, so replays of
// differently padded input stay equal.
type remakeFingerprint struct {
	WasteID uuid.UUID `json:"waste_id"`
	Reason  string    `json:"reason"`
	Note    *string   `json:"note"`
}

// stateServiceSessionActive is the Service Session state a Remake requires.
// Service Session states are owned by internal/sales; preparation only reads
// them through its own lock queries, so the value is restated here instead of
// importing across the ADR-024 boundary.
const stateServiceSessionActive = "ACTIVE"

// constraintPreparationRemakeWasteUnique is the migration 000013 constraint
// making a Remake a one-per-Waste fact. It is the only database error this
// command maps to a domain outcome.
const constraintPreparationRemakeWasteUnique = "preparation_remake_waste_unique"

// mapRemakeDBError maps exactly the named unique-Remake constraint to the
// typed conflict it represents: a Waste that already carries its one Remake
// cannot be remade again. Every other PostgreSQL error — including every
// other unique violation, such as preparation_remake_unit_unique — is
// returned untouched as an infrastructure failure.
func mapRemakeDBError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) &&
		pgErr.Code == "23505" &&
		pgErr.ConstraintName == constraintPreparationRemakeWasteUnique {
		return fmt.Errorf("%w: the waste already carries a remake fact", ErrWasteAlreadyRemade)
	}
	return err
}

// remakeSource carries the immutable identifiers a Remake locks: the Waste's
// source Preparation Unit, its Order Item, and the Order Item's Service
// Session. Every reference in the chain is fixed when the Waste is created,
// so the handler resolves them once before the mutation and the callback
// spends them on its session-first locks.
type remakeSource struct {
	WasteID          uuid.UUID
	SourceUnitID     uuid.UUID
	OrderItemID      uuid.UUID
	ServiceSessionID uuid.UUID
}

// RemakeUnitHandler creates one linked Remake: the replacement Preparation
// Unit — a fresh QUEUED unit with the source's immutable preparation snapshot
// under the Order Item's next unit number, at REMAKE priority, linked back to
// its source — plus the Remake fact and the one PREPARATION_REMAKE_CREATED
// audit, all inside the executor's single mutation transaction. It changes no
// Order Item, allocation, Check charge, Payment, Refund, or Comp.
type RemakeUnitHandler struct{ runner *Runner }

// NewRemakeUnitHandler creates a RemakeUnitHandler.
func NewRemakeUnitHandler(runner *Runner) *RemakeUnitHandler {
	return &RemakeUnitHandler{runner: runner}
}

// Handle executes the remake.
//
// Reason and note are normalized and validated BEFORE the mutation begins, so
// a malformed request never claims its idempotency key. The Waste's immutable
// source chain (unit → Order Item → Service Session) is resolved before the
// mutation too, because the mutation's first lock must be the owning Service
// Session and only the chain knows which one that is; the resolution is an
// autocommit read, so no lock outlives it and the mutation re-locks everything
// it needs. Success answers 201; the route layer maps domain errors through
// ErrorResponse.
func (h *RemakeUnitHandler) Handle(ctx context.Context, actor Actor,
	cmd RemakeUnitCommand,
) (int, RemakeResponse, error) {
	note := NormalizeCorrectionNote(cmd.Note)
	if err := ValidateRemakeReason(cmd.Reason); err != nil {
		return 0, RemakeResponse{}, err
	}
	if err := ValidateCorrectionNote(cmd.Reason, note); err != nil {
		return 0, RemakeResponse{}, err
	}

	// The chain read is the one query keyed by Waste id that resolves the
	// source unit, its Order Item, and the Order Item's Service Session. It
	// runs on the pool in autocommit: the row locks its name carries are held
	// only for the statement and are released before the mutation begins —
	// the mutation itself takes every lock again, in the session-first order.
	waste, err := h.runner.queries.LockPreparationWaste(ctx, cmd.WasteID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, RemakeResponse{}, fmt.Errorf("%w: %s", ErrWasteNotFound, cmd.WasteID)
		}
		return 0, RemakeResponse{}, fmt.Errorf("resolve preparation waste chain: %w", err)
	}
	source := remakeSource{
		WasteID:          waste.ID,
		SourceUnitID:     waste.PreparationUnitID,
		OrderItemID:      waste.OrderItemID,
		ServiceSessionID: waste.ServiceSessionID,
	}

	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpRemakeUnit,
		Fingerprint: remakeFingerprint{
			WasteID: cmd.WasteID,
			Reason:  cmd.Reason,
			Note:    note,
		},
		Required: []string{CapPreparationOperate},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, RemakeResponse, AuditRecord, error) {
			response, err := applyRemake(ctx, mc.Queries, actor, source, cmd.Reason, note)
			if err != nil {
				return 0, RemakeResponse{}, AuditRecord{}, err
			}
			// The one business audit was written inside the mutation through
			// writePreparationAudits, sharing the evidence's timestamp, so the
			// executor's single-audit step is deliberately given a zero
			// record.
			return http.StatusCreated, response, AuditRecord{}, nil
		})
}

// applyRemake is the mutation body, ordered so each step's failure leaves the
// transaction — claim included — to the executor's rollback:
//
//  1. lock the owning Service Session first — the global lock contract that
//     serializes Remake with closure, which locks the same row first too —
//     and require ACTIVE: a closed Session is a conflict even on a first
//     execution;
//  2. lock the Order Item, so the unit-number allocation below serializes
//     across concurrent Remakes of the same item, and confirm the item still
//     belongs to the locked Session;
//  3. lock the Waste and its source unit together, revalidating both after
//     the locks: the Waste still exists and its source is still WASTED;
//  4. choose one database instant for the queue time, the fact, and the audit;
//  5. allocate max(unit_number)+1 for the Order Item while its lock is held;
//  6. insert the copied replacement unit — fresh id and queue time, the
//     source's immutable snapshot, REMAKE priority, remake_of link;
//  7. insert the Remake fact, mapping only the one-Remake-per-Waste
//     constraint to ErrWasteAlreadyRemade;
//  8. write the one business audit through writePreparationAudits;
//  9. load the replacement unit and return the complete result, leaving the
//     executor's AuditRecord at zero.
func applyRemake(ctx context.Context, q *sqlc.Queries, actor Actor,
	source remakeSource, reason string, note *string,
) (RemakeResponse, error) {
	// 1. Session-first locking. The row lock serializes this Remake against
	// Service Session closure.
	sessions, err := q.LockPreparationServiceSessions(ctx, []uuid.UUID{source.ServiceSessionID})
	if err != nil {
		return RemakeResponse{}, fmt.Errorf("lock preparation service session: %w", err)
	}
	if len(sessions) != 1 {
		return RemakeResponse{}, fmt.Errorf(
			"lock preparation service session: expected exactly 1 row for %s, got %d",
			source.ServiceSessionID, len(sessions))
	}
	if sessions[0].State != stateServiceSessionActive {
		return RemakeResponse{}, fmt.Errorf("%w: %s is %s",
			ErrServiceSessionClosed, source.ServiceSessionID, sessions[0].State)
	}

	// 2. The Order Item lock guards the unit-number allocation in step 5.
	orderItem, err := q.LockPreparationOrderItem(ctx, source.OrderItemID)
	if err != nil {
		return RemakeResponse{}, fmt.Errorf("lock preparation order item: %w", err)
	}
	if orderItem.ServiceSessionID != source.ServiceSessionID {
		return RemakeResponse{}, fmt.Errorf(
			"lock preparation order item: %s belongs to session %s, expected %s",
			orderItem.ID, orderItem.ServiceSessionID, source.ServiceSessionID)
	}

	// 3. The Waste and its source unit are locked together and revalidated
	// after the locks, so the loser of a concurrent remake re-reads the
	// winner's committed evidence here and loses on the fact insert below.
	waste, err := q.LockPreparationWaste(ctx, source.WasteID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RemakeResponse{}, fmt.Errorf("%w: %s", ErrWasteNotFound, source.WasteID)
		}
		return RemakeResponse{}, fmt.Errorf("lock preparation waste: %w", err)
	}
	sourceUnit, err := q.LockPreparationUnit(ctx, waste.PreparationUnitID)
	if err != nil {
		return RemakeResponse{}, fmt.Errorf("lock remake source unit: %w", err)
	}
	if sourceUnit.State != StateWasted {
		return RemakeResponse{}, fmt.Errorf("%w: the remake source unit is %s, not wasted",
			ErrInvalidTransition, sourceUnit.State)
	}

	// 4. One database clock reading, shared by the queue time, the fact, and
	// the audit.
	occurredAt, err := q.GetPreparationCurrentTime(ctx)
	if err != nil {
		return RemakeResponse{}, fmt.Errorf("read preparation occurrence time: %w", err)
	}

	// 5. max(unit_number)+1 runs under the Order Item lock taken in step 2,
	// so concurrent Remakes of different Wastes of one item cannot collide.
	unitNumber, err := q.GetNextPreparationUnitNumber(ctx, waste.OrderItemID)
	if err != nil {
		return RemakeResponse{}, fmt.Errorf("allocate preparation unit number: %w", err)
	}

	// 6. The copied replacement: the source's immutable preparation snapshot
	// under a fresh id and queue time, REMAKE priority, and the remake_of
	// link. Nothing customer-facing is charged or allocated for it.
	replacement, err := q.InsertPreparationRemakeUnit(ctx, sqlc.InsertPreparationRemakeUnitParams{
		OrderItemID:               waste.OrderItemID,
		UnitNumber:                unitNumber,
		ServiceNumber:             sourceUnit.ServiceNumber,
		CategoryName:              sourceUnit.CategoryName,
		ItemName:                  sourceUnit.ItemName,
		SizeName:                  sourceUnit.SizeName,
		Modifiers:                 sourceUnit.Modifiers,
		PreparationNote:           sourceUnit.PreparationNote,
		QueuedAt:                  occurredAt,
		RemakeOfPreparationUnitID: uuid.NullUUID{UUID: sourceUnit.ID, Valid: true},
	})
	if err != nil {
		return RemakeResponse{}, fmt.Errorf("insert preparation remake unit: %w", err)
	}

	// 7. The Remake fact — one per Waste, forever. A concurrent remake of the
	// same Waste committed while this transaction waited on the locks fails
	// here on the named constraint, and the whole mutation rolls back with
	// it, replacement unit included.
	var noteValue sql.NullString
	if note != nil {
		noteValue = sql.NullString{String: *note, Valid: true}
	}
	fact, err := q.InsertPreparationRemake(ctx, sqlc.InsertPreparationRemakeParams{
		WasteID:              waste.ID,
		PreparationUnitID:    replacement.ID,
		Reason:               reason,
		Note:                 noteValue,
		ActorStaffIdentityID: actor.StaffID,
		StaffAccessSessionID: actor.SessionID,
		CreatedAt:            occurredAt,
	})
	if err != nil {
		return RemakeResponse{}, fmt.Errorf("insert preparation remake: %w", mapRemakeDBError(err))
	}

	// 8. The one business event of this mutation, one actor, one session, one
	// occurrence time — the same instant as the queue time above.
	if err := writePreparationAudits(ctx, q, actor, occurredAt, []AuditRecord{
		{
			EventType: EventPreparationRemakeCreated,
			Details: map[string]any{
				"waste_id":                   waste.ID,
				"source_preparation_unit_id": waste.PreparationUnitID,
				"preparation_unit_id":        replacement.ID,
				"reason":                     reason,
			},
		},
	}); err != nil {
		return RemakeResponse{}, err
	}

	// 9. The replacement is loaded through the same read the bar's unit view
	// uses, so the client reads back exactly what the database accepted.
	unit, err := loadUnit(ctx, q, replacement.ID)
	if err != nil {
		return RemakeResponse{}, err
	}
	var factNote *string
	if fact.Note.Valid {
		factNote = &fact.Note.String
	}
	return RemakeResponse{
		ID:                      fact.ID,
		WasteID:                 fact.WasteID,
		SourcePreparationUnitID: waste.PreparationUnitID,
		Reason:                  fact.Reason,
		Note:                    factNote,
		CreatedAt:               fact.CreatedAt,
		Unit:                    unit,
	}, nil
}
