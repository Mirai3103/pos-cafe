package sales

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

type sessionScopedFingerprint struct {
	ServiceSessionID uuid.UUID `json:"service_session_id"`
}

type orderDraftStartedAudit struct {
	ServiceSessionID uuid.UUID `json:"service_session_id"`
	OrderDraftID     uuid.UUID `json:"order_draft_id"`
}

// StartNewOrderDraftHandler opens the Session's next Order Draft.
//
// This is a Session round-lifecycle operation, not a draft-item operation: it
// locks the Session rather than a draft, and decides whether a new round may
// begin at all. It is grouped with the Check-target command for that reason.
type StartNewOrderDraftHandler struct{ runner *Runner }

// NewStartNewOrderDraftHandler creates a new StartNewOrderDraftHandler.
func NewStartNewOrderDraftHandler(runner *Runner) *StartNewOrderDraftHandler {
	return &StartNewOrderDraftHandler{runner: runner}
}

// Handle opens a new EDITABLE draft when no blocking draft stands in the way.
func (h *StartNewOrderDraftHandler) Handle(ctx context.Context, actor Actor,
	cmd StartNewOrderDraftCommand,
) (int, ServiceSessionResponse, error) {
	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpStartNewOrderDraft,
		Fingerprint: sessionScopedFingerprint{ServiceSessionID: cmd.ServiceSessionID},
		Required:    []string{CapSalesOperate},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, ServiceSessionResponse, AuditRecord, error) {
			var zero ServiceSessionResponse
			q := mc.Queries

			shiftID, err := q.GetOpenSalesShiftID(ctx)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return 0, zero, AuditRecord{}, fmt.Errorf(
						"%w: no open sales shift", ErrOpenShiftRequired)
				}
				return 0, zero, AuditRecord{}, fmt.Errorf("load open sales shift: %w", err)
			}

			session, err := q.LockServiceSessionForUpdate(ctx, cmd.ServiceSessionID)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return 0, zero, AuditRecord{}, fmt.Errorf(
						"%w: %s", ErrServiceSessionNotFound, cmd.ServiceSessionID)
				}
				return 0, zero, AuditRecord{}, fmt.Errorf("lock service session: %w", err)
			}
			if session.State != StateActive {
				return 0, zero, AuditRecord{}, fmt.Errorf(
					"%w: %s", ErrServiceSessionClosed, cmd.ServiceSessionID)
			}
			if session.SalesShiftID != shiftID {
				return 0, zero, AuditRecord{}, fmt.Errorf(
					"%w: session belongs to another shift", ErrOpenShiftRequired)
			}

			if _, err := q.FindBlockingDraft(ctx, cmd.ServiceSessionID); err == nil {
				return 0, zero, AuditRecord{}, fmt.Errorf(
					"%w: %s", ErrNewOrderDraftNotAvailable, cmd.ServiceSessionID)
			} else if !errors.Is(err, sql.ErrNoRows) {
				return 0, zero, AuditRecord{}, fmt.Errorf("find blocking draft: %w", err)
			}

			// The new draft takes check_target's column default, so a cashier
			// who directed one round to a new Check does not silently direct
			// the next one there too.
			draft, err := q.InsertOrderDraftForSession(ctx,
				sqlc.InsertOrderDraftForSessionParams{
					ServiceSessionID: cmd.ServiceSessionID,
					CreatedAt:        time.Now(),
				})
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("insert order draft: %w", err)
			}

			result, err := LoadServiceSession(ctx, q, cmd.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}

			return 200, result, AuditRecord{
				EventType: EventOrderDraftStarted,
				Details: orderDraftStartedAudit{
					ServiceSessionID: cmd.ServiceSessionID,
					OrderDraftID:     draft.ID,
				},
			}, nil
		})
}

type setCheckTargetFingerprint struct {
	ServiceSessionID uuid.UUID `json:"service_session_id"`
	CheckTarget      string    `json:"check_target"`
}

type checkTargetAudit struct {
	ServiceSessionID uuid.UUID `json:"service_session_id"`
	OrderDraftID     uuid.UUID `json:"order_draft_id"`
	CheckTarget      string    `json:"check_target"`
}

// SetCheckTargetHandler steers where the next Commit's charges land.
type SetCheckTargetHandler struct{ runner *Runner }

// NewSetCheckTargetHandler creates a new SetCheckTargetHandler.
func NewSetCheckTargetHandler(runner *Runner) *SetCheckTargetHandler {
	return &SetCheckTargetHandler{runner: runner}
}

// Handle writes the draft's Check target.
//
// It acquires the draft through the same lock the six 5A draft commands use,
// so it carries the same preconditions: ACTIVE Session, EDITABLE draft, OPEN
// Shift. Setting a target on a committed draft is not a thing that can happen.
func (h *SetCheckTargetHandler) Handle(ctx context.Context, actor Actor,
	cmd SetCheckTargetCommand,
) (int, ServiceSessionResponse, error) {
	if err := ValidateCheckTarget(cmd.CheckTarget); err != nil {
		return 0, ServiceSessionResponse{}, err
	}

	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpSetOrderDraftCheckTarget,
		Fingerprint: setCheckTargetFingerprint{
			ServiceSessionID: cmd.ServiceSessionID,
			CheckTarget:      cmd.CheckTarget,
		},
		Required: []string{CapSalesOperate},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, ServiceSessionResponse, AuditRecord, error) {
			var zero ServiceSessionResponse
			q := mc.Queries

			draft, err := lockEditableDraft(ctx, q, cmd.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}

			if err := q.SetOrderDraftCheckTarget(ctx, sqlc.SetOrderDraftCheckTargetParams{
				ID:          draft.OrderDraftID,
				CheckTarget: cmd.CheckTarget,
			}); err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("set order draft check target: %w", err)
			}

			result, err := LoadServiceSession(ctx, q, cmd.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}

			return 200, result, AuditRecord{
				EventType: EventOrderDraftCheckTargetSet,
				Details: checkTargetAudit{
					ServiceSessionID: cmd.ServiceSessionID,
					OrderDraftID:     draft.OrderDraftID,
					CheckTarget:      cmd.CheckTarget,
				},
			}, nil
		})
}
