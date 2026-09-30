package sales

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/platform/database/sqlc"
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
			out, audit, err := applyCloseServiceSession(ctx, mc.Queries, actor, cmd.ServiceSessionID)
			if err != nil {
				return 0, CompletedSaleResponse{}, AuditRecord{}, err
			}
			return http.StatusCreated, out, audit, nil
		})
}

// applyCloseServiceSession is the closure mutation body. It locks the Session,
// answers an already-closed Session with its existing sale, checks closure
// readiness, then appends the Completed Sale, releases every held Table, and
// closes the Session.
func applyCloseServiceSession(ctx context.Context, q *sqlc.Queries, actor Actor, sessionID uuid.UUID) (
	CompletedSaleResponse, AuditRecord, error,
) {
	var zero CompletedSaleResponse
	session, err := q.LockServiceSessionForClosure(ctx, sessionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return zero, AuditRecord{}, fmt.Errorf("%w: %s", ErrServiceSessionNotFound, sessionID)
		}
		return zero, AuditRecord{}, fmt.Errorf("lock service session: %w", err)
	}

	// An already-closed Session returns its existing sale rather than an
	// error: closing twice is a duplicate, not a mistake.
	existingID, err := q.FindCompletedSaleByServiceSession(ctx, sessionID)
	switch {
	case err == nil:
		out, err := LoadCompletedSale(ctx, q, existingID)
		if err != nil {
			return zero, AuditRecord{}, err
		}
		return out, AuditRecord{}, nil
	case !errors.Is(err, sql.ErrNoRows):
		return zero, AuditRecord{}, fmt.Errorf("find completed sale: %w", err)
	}
	if session.State != StateActive {
		return zero, AuditRecord{}, fmt.Errorf("%w: %s", ErrServiceSessionClosed, sessionID)
	}

	projection, err := LoadServiceSession(ctx, q, sessionID)
	if err != nil {
		return zero, AuditRecord{}, err
	}
	if err := EvaluateClosureReadiness(projection).Err(); err != nil {
		return zero, AuditRecord{}, err
	}

	completedAt := time.Now()
	saleID, err := q.InsertCompletedSale(ctx, sqlc.InsertCompletedSaleParams{
		ServiceSessionID:              sessionID,
		CompletedByStaffIdentityID:    actor.StaffID,
		CompletedStaffAccessSessionID: actor.SessionID,
		CompletedAt:                   completedAt,
	})
	if err != nil {
		return zero, AuditRecord{}, fmt.Errorf("insert completed sale: %w", err)
	}

	releasedTableIDs, err := releaseHeldTableAssignments(ctx, q, actor, sessionID, completedAt)
	if err != nil {
		return zero, AuditRecord{}, err
	}

	if err := q.CloseServiceSession(ctx, sessionID); err != nil {
		return zero, AuditRecord{}, fmt.Errorf("close service session: %w", err)
	}

	out, err := LoadCompletedSale(ctx, q, saleID)
	if err != nil {
		return zero, AuditRecord{}, err
	}
	return out, AuditRecord{
		EventType: EventServiceSessionClosed,
		Details: map[string]any{
			"service_session_id": sessionID,
			"completed_sale_id":  saleID,
			"released_table_ids": releasedTableIDs,
		},
	}, nil
}

// releaseHeldTableAssignments releases every Table the Session holds, one
// TABLE_ASSIGNMENT_RELEASED Audit Event each, and returns the released Table
// ids. Service Session closure and Abandon Checkout share it.
func releaseHeldTableAssignments(ctx context.Context, q *sqlc.Queries, actor Actor,
	sessionID uuid.UUID, occurredAt time.Time,
) ([]uuid.UUID, error) {
	assignments, err := q.ListHeldTableAssignments(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list held table assignments: %w", err)
	}
	released := make([]uuid.UUID, 0, len(assignments))
	for _, assignment := range assignments {
		// The existing ReleaseTableAssignment query sets released_at = now()
		// server-side: the release and the Session's terminal write happen in
		// one transaction, so the timestamps are the same moment and the
		// evidence constraint stays satisfied without a second query.
		if err := q.ReleaseTableAssignment(ctx, sqlc.ReleaseTableAssignmentParams{
			ID:                        assignment.ID,
			ReleasedByStaffIdentityID: uuid.NullUUID{UUID: actor.StaffID, Valid: true},
		}); err != nil {
			return nil, fmt.Errorf("release table assignment: %w", err)
		}
		// Each released assignment carries its own audit event, the same
		// event type and details shape the Table Assignment release path
		// writes; AuditRecord holds only the one Session-level event.
		details, err := json.Marshal(tableAssignmentAudit{
			TableAssignmentID: assignment.ID,
			TableID:           assignment.TableID,
			ServiceSessionID:  sessionID,
		})
		if err != nil {
			return nil, fmt.Errorf("marshal table assignment audit: %w", err)
		}
		if _, err := q.InsertAuditEvent(ctx, sqlc.InsertAuditEventParams{
			EventType:  EventTableAssignmentReleased,
			ActorID:    uuid.NullUUID{UUID: actor.StaffID, Valid: true},
			SessionID:  uuid.NullUUID{UUID: actor.SessionID, Valid: true},
			Details:    details,
			OccurredAt: occurredAt,
		}); err != nil {
			return nil, fmt.Errorf("insert %s audit event: %w", EventTableAssignmentReleased, err)
		}
		released = append(released, assignment.TableID)
	}
	return released, nil
}
