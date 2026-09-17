package preparation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

// acknowledgeAlertFingerprint is the normalized, credential-free business
// input an acknowledgment stands for: the path alert id alone. The command
// body carries only the idempotency key, so there is nothing to normalize and
// no secret to keep out.
type acknowledgeAlertFingerprint struct {
	AlertID uuid.UUID `json:"alert_id"`
}

// AcknowledgeAlertHandler acknowledges one Preparation Alert: it fills the
// alert's acknowledgment tuple — the acking actor's identity, their Staff
// Access Session, and one timestamp, together — and writes the
// PREPARATION_ALERT_ACKNOWLEDGED audit, all inside the executor's single
// mutation transaction. It changes no unit state, Waste, charge, or closure
// readiness, and requires no Manager PIN: only preparation.operate.
type AcknowledgeAlertHandler struct{ runner *Runner }

// NewAcknowledgeAlertHandler creates an AcknowledgeAlertHandler.
func NewAcknowledgeAlertHandler(runner *Runner) *AcknowledgeAlertHandler {
	return &AcknowledgeAlertHandler{runner: runner}
}

// Handle executes the acknowledgment.
//
// The command has nothing to validate at the boundary — an unknown alert id is
// a domain outcome, not a malformed request — so no check precedes the
// mutation and a malformed id is answered by the lock's miss. Success answers
// 200; the route layer maps domain errors through ErrorResponse.
func (h *AcknowledgeAlertHandler) Handle(ctx context.Context, actor Actor,
	cmd AcknowledgeAlertCommand,
) (int, AlertResponse, error) {
	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpAcknowledgeAlert,
		Fingerprint: acknowledgeAlertFingerprint{
			AlertID: cmd.AlertID,
		},
		Required: []string{CapPreparationOperate},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, AlertResponse, AuditRecord, error) {
			response, err := applyAlertAcknowledgment(ctx, mc.Queries, actor, cmd.AlertID)
			if err != nil {
				return 0, AlertResponse{}, AuditRecord{}, err
			}
			// The one business event was written inside the mutation through
			// writePreparationAudits, sharing the evidence's timestamp, so the
			// executor's single-audit step is deliberately given a zero
			// record.
			return http.StatusOK, response, AuditRecord{}, nil
		})
}

// applyAlertAcknowledgment is the mutation body, ordered so each step's
// failure leaves the transaction — claim included — to the executor's
// rollback:
//
//  1. lock the alert, mapping a miss to ErrAlertNotFound;
//  2. require an unacknowledged row, else ErrAlertAlreadyAcknowledged — an
//     exact replay never reaches this body, so this condition is always a
//     separately requested second acknowledgment, detected from the locked
//     row's state, not from a constraint race;
//  3. choose one database instant for the evidence and the audit;
//  4. fill the whole acknowledgment tuple together with Valid-pinned
//     parameters, so no zero-value null evidence can be written;
//  5. write the one business audit through writePreparationAudits;
//  6. reload the response from the locked row's creation evidence and the
//     UPDATE's RETURNING evidence.
//
// The Preparation Unit and Service Session are deliberately neither locked
// nor written: acknowledgment controls only the alert's own lifecycle.
func applyAlertAcknowledgment(ctx context.Context, q *sqlc.Queries, actor Actor,
	alertID uuid.UUID,
) (AlertResponse, error) {
	// 1. The row lock serializes concurrent acknowledgments of this alert:
	// every loser of the race re-reads the winner's committed evidence here.
	alert, err := q.LockPreparationAlert(ctx, alertID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AlertResponse{}, fmt.Errorf("%w: %s", ErrAlertNotFound, alertID)
		}
		return AlertResponse{}, fmt.Errorf("lock preparation alert: %w", err)
	}

	// 2. The acknowledgment tuple is all-null or all-present (migration
	// 000013), so the acknowledged time alone discriminates the states.
	if alert.AcknowledgedAt.Valid {
		return AlertResponse{}, fmt.Errorf("%w: %s", ErrAlertAlreadyAcknowledged, alertID)
	}

	// 3. One database clock reading, shared by the evidence and the audit.
	acknowledgedAt, err := q.GetPreparationCurrentTime(ctx)
	if err != nil {
		return AlertResponse{}, fmt.Errorf("read preparation acknowledgment time: %w", err)
	}

	// 4. The columns are nullable, so the generated parameters are nullable
	// types; every one is pinned Valid so the tuple constraint's all-present
	// arm is the only write this command can ever make.
	row, err := q.AcknowledgePreparationAlert(ctx, sqlc.AcknowledgePreparationAlertParams{
		AcknowledgedByStaffIdentityID:    uuid.NullUUID{UUID: actor.StaffID, Valid: true},
		AcknowledgedStaffAccessSessionID: uuid.NullUUID{UUID: actor.SessionID, Valid: true},
		AcknowledgedAt:                   sql.NullTime{Time: acknowledgedAt, Valid: true},
		ID:                               alert.ID,
	})
	if err != nil {
		return AlertResponse{}, fmt.Errorf("acknowledge preparation alert: %w", err)
	}

	// 5. The one business event of this mutation, one actor, one session, one
	// occurrence time — the same instant as the evidence above.
	if err := writePreparationAudits(ctx, q, actor, acknowledgedAt, []AuditRecord{
		{
			EventType: EventPreparationAlertAcknowledged,
			Details: map[string]any{
				"preparation_unit_id": alert.PreparationUnitID,
				"alert_id":            alert.ID,
				"kind":                alert.Kind,
				"reason":              alert.Reason,
			},
		},
	}); err != nil {
		return AlertResponse{}, err
	}

	// 6. The RETURNING row is the load of the acknowledgment evidence the
	// database accepted; merged onto the locked row's immutable creation
	// evidence it is the complete alert as the client reads it back.
	alert.AcknowledgedByStaffIdentityID = row.AcknowledgedByStaffIdentityID
	alert.AcknowledgedStaffAccessSessionID = row.AcknowledgedStaffAccessSessionID
	alert.AcknowledgedAt = row.AcknowledgedAt
	return buildAlertResponse(alert), nil
}

// buildAlertResponse assembles the response from the stored alert row, so the
// client reads back exactly what the database accepted. A fresh alert carries
// nil acknowledgment fields; an acknowledged one carries all three — the
// response exposes identity and time, while the acking session lives in the
// database evidence and the audit.
func buildAlertResponse(alert sqlc.PreparationAlert) AlertResponse {
	var note *string
	if alert.Note.Valid {
		note = &alert.Note.String
	}
	resp := AlertResponse{
		ID:                alert.ID,
		PreparationUnitID: alert.PreparationUnitID,
		Kind:              alert.Kind,
		Reason:            alert.Reason,
		Note:              note,
		CreatedAt:         alert.CreatedAt,
	}
	if alert.AcknowledgedByStaffIdentityID.Valid {
		acknowledgedBy := alert.AcknowledgedByStaffIdentityID.UUID
		resp.AcknowledgedByStaffIdentityID = &acknowledgedBy
	}
	if alert.AcknowledgedAt.Valid {
		acknowledgedAt := alert.AcknowledgedAt.Time
		resp.AcknowledgedAt = &acknowledgedAt
	}
	return resp
}
