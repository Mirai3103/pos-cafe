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
