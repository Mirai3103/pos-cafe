package sales

import (
	"context"

	"github.com/google/uuid"
)

// startTakeawayFingerprint carries no business input: a Takeaway Session has
// nothing to configure. The request_id alone distinguishes two opens, which is
// correct — two deliberate opens are two Sessions.
type startTakeawayFingerprint struct {
	Mode string `json:"mode"`
}

type serviceSessionStartedAudit struct {
	ServiceSessionID uuid.UUID `json:"service_session_id"`
	ServiceNumber    string    `json:"service_number"`
	ServiceMode      string    `json:"service_mode"`
	SalesShiftID     uuid.UUID `json:"sales_shift_id"`
}

// StartTakeawaySessionHandler opens an anonymous Takeaway Service Session.
type StartTakeawaySessionHandler struct{ runner *Runner }

// NewStartTakeawaySessionHandler creates a new StartTakeawaySessionHandler.
func NewStartTakeawaySessionHandler(runner *Runner) *StartTakeawaySessionHandler {
	return &StartTakeawaySessionHandler{runner: runner}
}

// Handle opens a Takeaway Service Session with its editable Order Draft.
func (h *StartTakeawaySessionHandler) Handle(ctx context.Context, actor Actor,
	cmd StartTakeawaySessionCommand,
) (int, ServiceSessionResponse, error) {
	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpStartTakeawaySession,
		Fingerprint: startTakeawayFingerprint{Mode: ModeTakeaway},
		Required:    []string{CapSalesOperate},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, ServiceSessionResponse, AuditRecord, error) {
			shiftID, err := requireOpenSalesShift(ctx, mc.Queries)
			if err != nil {
				return 0, ServiceSessionResponse{}, AuditRecord{}, err
			}

			sequence, number, err := allocateServiceNumber(ctx, h.runner, mc.Queries, shiftID)
			if err != nil {
				return 0, ServiceSessionResponse{}, AuditRecord{}, err
			}

			sessionID, err := insertSessionWithDraft(ctx, mc.Queries, actor,
				ModeTakeaway, shiftID, sequence, number)
			if err != nil {
				return 0, ServiceSessionResponse{}, AuditRecord{}, err
			}

			result, err := LoadServiceSession(ctx, mc.Queries, sessionID)
			if err != nil {
				return 0, ServiceSessionResponse{}, AuditRecord{}, err
			}

			return 201, result, AuditRecord{
				EventType: EventServiceSessionStarted,
				Details: serviceSessionStartedAudit{
					ServiceSessionID: sessionID,
					ServiceNumber:    number,
					ServiceMode:      ModeTakeaway,
					SalesShiftID:     shiftID,
				},
			}, nil
		})
}

// startDineInFingerprint sorts the Table ids: selection order does not change
// the target Table set, so it must not change the idempotency fingerprint. The
// unsorted order is kept for assignment sequence and audit events, which do
// care about the order staff selected Tables in.
type startDineInFingerprint struct {
	Mode     string      `json:"mode"`
	TableIDs []uuid.UUID `json:"table_ids"`
}

type dineInSessionStartedAudit struct {
	serviceSessionStartedAudit
	TableIDs []uuid.UUID `json:"table_ids"`
}

// StartDineInSessionHandler opens a Dine-in Service Session against Tables.
type StartDineInSessionHandler struct{ runner *Runner }

// NewStartDineInSessionHandler creates a new StartDineInSessionHandler.
func NewStartDineInSessionHandler(runner *Runner) *StartDineInSessionHandler {
	return &StartDineInSessionHandler{runner: runner}
}

// Handle opens a Dine-in Service Session and assigns its Tables.
//
// Selection validation happens before the transaction because it needs no
// database state and a bad selection should not consume a request_id.
func (h *StartDineInSessionHandler) Handle(ctx context.Context, actor Actor,
	cmd StartDineInSessionCommand,
) (int, ServiceSessionResponse, error) {
	if len(cmd.TableIDs) == 0 {
		return 0, ServiceSessionResponse{}, ErrTableSelectionRequired
	}
	if HasDuplicateUUIDs(cmd.TableIDs) {
		return 0, ServiceSessionResponse{}, ErrTableSelectionDuplicate
	}

	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpStartDineInSession,
		Fingerprint: startDineInFingerprint{
			Mode:     ModeDineIn,
			TableIDs: SortedUUIDs(cmd.TableIDs),
		},
		Required: []string{CapSalesOperate},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, ServiceSessionResponse, AuditRecord, error) {
			var zero ServiceSessionResponse

			shiftID, err := requireOpenSalesShift(ctx, mc.Queries)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			if err := lockAndValidateTables(ctx, mc.Queries, cmd.TableIDs); err != nil {
				return 0, zero, AuditRecord{}, err
			}

			sequence, number, err := allocateServiceNumber(ctx, h.runner, mc.Queries, shiftID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}

			sessionID, err := insertSessionWithDraft(ctx, mc.Queries, actor,
				ModeDineIn, shiftID, sequence, number)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}

			assignments, err := assignTables(ctx, mc.Queries, actor, sessionID, cmd.TableIDs, 0)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			if err := writeAssignmentAudits(ctx, mc.Queries, actor,
				EventTableAssignmentCreated, assignments); err != nil {
				return 0, zero, AuditRecord{}, err
			}

			result, err := LoadServiceSession(ctx, mc.Queries, sessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}

			return 201, result, AuditRecord{
				EventType: EventDineInServiceSessionStarted,
				Details: dineInSessionStartedAudit{
					serviceSessionStartedAudit: serviceSessionStartedAudit{
						ServiceSessionID: sessionID,
						ServiceNumber:    number,
						ServiceMode:      ModeDineIn,
						SalesShiftID:     shiftID,
					},
					TableIDs: cmd.TableIDs,
				},
			}, nil
		})
}
