package sales

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

// CloseServiceSessionHandler completes a Service Session into an immutable
// Completed Sale.
type CloseServiceSessionHandler struct{ runner *Runner }

// NewCloseServiceSessionHandler creates a CloseServiceSessionHandler.
func NewCloseServiceSessionHandler(runner *Runner) *CloseServiceSessionHandler {
	return &CloseServiceSessionHandler{runner: runner}
}

// Handle executes closure.
//
// Idempotency runs on the shared executor with T = CompletedSaleResponse
// (ADR-026). The canonical source maintains a dedicated
// completed_sale_closing_requests table only because its helper is typed to
// the Service Session projection; the Go executor is generic, so closure's
// replay and audit behave identically to every other command's.
//
// Like Submit, closure does not require an open Sales Shift: a Shift can end
// while drinks are still being prepared, and staff must be able to finish what
// is already in flight.
func (h *CloseServiceSessionHandler) Handle(ctx context.Context, actor Actor,
	cmd CloseServiceSessionCommand,
) (int, CompletedSaleResponse, error) {
	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpCloseServiceSession,
		Fingerprint: sessionScopedFingerprint{ServiceSessionID: cmd.ServiceSessionID},
		Required:    []string{CapSalesOperate},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, CompletedSaleResponse, AuditRecord, error) {
			var zero CompletedSaleResponse
			q := mc.Queries

			session, err := q.LockServiceSessionForClosure(ctx, cmd.ServiceSessionID)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return 0, zero, AuditRecord{}, fmt.Errorf(
						"%w: %s", ErrServiceSessionNotFound, cmd.ServiceSessionID)
				}
				return 0, zero, AuditRecord{}, fmt.Errorf("lock service session: %w", err)
			}

			// An already-closed Session returns its existing sale rather than
			// an error: closing twice is a duplicate, not a mistake.
			existingID, err := q.FindCompletedSaleByServiceSession(ctx, cmd.ServiceSessionID)
			switch {
			case err == nil:
				out, err := LoadCompletedSale(ctx, q, existingID)
				if err != nil {
					return 0, zero, AuditRecord{}, err
				}
				return http.StatusCreated, out, AuditRecord{}, nil
			case !errors.Is(err, sql.ErrNoRows):
				return 0, zero, AuditRecord{}, fmt.Errorf("find completed sale: %w", err)
			}
			if session.State != StateActive {
				return 0, zero, AuditRecord{}, fmt.Errorf(
					"%w: %s", ErrServiceSessionClosed, cmd.ServiceSessionID)
			}

			projection, err := LoadServiceSession(ctx, q, cmd.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			if err := EvaluateClosureReadiness(projection).Err(); err != nil {
				return 0, zero, AuditRecord{}, err
			}

			completedAt := time.Now()
			saleID, err := q.InsertCompletedSale(ctx, sqlc.InsertCompletedSaleParams{
				ServiceSessionID:              cmd.ServiceSessionID,
				CompletedByStaffIdentityID:    actor.StaffID,
				CompletedStaffAccessSessionID: actor.SessionID,
				CompletedAt:                   completedAt,
			})
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("insert completed sale: %w", err)
			}

			assignments, err := q.ListHeldTableAssignments(ctx, cmd.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("list held table assignments: %w", err)
			}
			for _, assignment := range assignments {
				// The existing ReleaseTableAssignment query sets released_at =
				// now() server-side: the release and the Completed Sale happen
				// in this one transaction, so the timestamps are the same
				// moment and the evidence constraint stays satisfied without a
				// second, redundant query.
				if err := q.ReleaseTableAssignment(ctx, sqlc.ReleaseTableAssignmentParams{
					ID:                        assignment.ID,
					ReleasedByStaffIdentityID: uuid.NullUUID{UUID: actor.StaffID, Valid: true},
				}); err != nil {
					return 0, zero, AuditRecord{}, fmt.Errorf("release table assignment: %w", err)
				}
				// Each released assignment carries its own audit event, the
				// same event type and details shape the Table Assignment
				// release path writes; AuditRecord holds only the one
				// Session-level event.
				details, err := json.Marshal(tableAssignmentAudit{
					TableAssignmentID: assignment.ID,
					TableID:           assignment.TableID,
					ServiceSessionID:  cmd.ServiceSessionID,
				})
				if err != nil {
					return 0, zero, AuditRecord{},
						fmt.Errorf("marshal table assignment audit: %w", err)
				}
				if _, err := q.InsertAuditEvent(ctx, sqlc.InsertAuditEventParams{
					EventType:  EventTableAssignmentReleased,
					ActorID:    uuid.NullUUID{UUID: actor.StaffID, Valid: true},
					SessionID:  uuid.NullUUID{UUID: actor.SessionID, Valid: true},
					Details:    details,
					OccurredAt: completedAt,
				}); err != nil {
					return 0, zero, AuditRecord{}, fmt.Errorf(
						"insert %s audit event: %w", EventTableAssignmentReleased, err)
				}
			}

			if err := q.CloseServiceSession(ctx, cmd.ServiceSessionID); err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("close service session: %w", err)
			}

			out, err := LoadCompletedSale(ctx, q, saleID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}

			releasedTableIDs := make([]uuid.UUID, 0, len(assignments))
			for _, assignment := range assignments {
				releasedTableIDs = append(releasedTableIDs, assignment.TableID)
			}
			return http.StatusCreated, out, AuditRecord{
				EventType: EventServiceSessionClosed,
				Details: map[string]any{
					"service_session_id": cmd.ServiceSessionID,
					"completed_sale_id":  saleID,
					"released_table_ids": releasedTableIDs,
				},
			}, nil
		})
}
