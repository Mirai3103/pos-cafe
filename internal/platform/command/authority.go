package command

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

// reloadAuthority loads the current session, roles, and capabilities inside
// the transaction, so authority removed mid-session takes effect immediately.
// The returned Authority is populated as far as the reload got, which the
// denial audit uses to decide what it may attribute.
func reloadAuthority(ctx context.Context, q *sqlc.Queries, actor Actor) (Authority, []string, error) {
	authority, err := q.GetSessionAuthority(ctx, sqlc.GetSessionAuthorityParams{
		ID:              actor.SessionID,
		StaffIdentityID: actor.StaffID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return authority, nil, fmt.Errorf("%w: session not found or identity mismatch", ErrUnauthorized)
		}
		return authority, nil, fmt.Errorf("reload session authority: %w", err)
	}

	if authority.State == auth.SessionStateLocked {
		return authority, nil, fmt.Errorf("%w: session locked", ErrUnauthorized)
	}
	if authority.RevokedAt.Valid {
		return authority, nil, fmt.Errorf("%w: session revoked", ErrUnauthorized)
	}
	if time.Now().After(authority.ExpiresAt) {
		return authority, nil, fmt.Errorf("%w: session expired", ErrUnauthorized)
	}
	if !authority.IdentityEnabled {
		return authority, nil, fmt.Errorf("%w: identity disabled", ErrForbidden)
	}
	workspace := ""
	if authority.ActiveWorkspace.Valid {
		workspace = authority.ActiveWorkspace.String
	}
	if time.Since(authority.LastHumanActivityAt) >= auth.GetInactivityTimeout(workspace) {
		return authority, nil, fmt.Errorf("%w: session inactive", ErrUnauthorized)
	}

	roles, err := q.GetSessionRoles(ctx, authority.StaffIdentityID)
	if err != nil {
		return authority, nil, fmt.Errorf("reload session roles: %w", err)
	}
	return authority, auth.DeriveCapabilities(roles), nil
}

// denialAttribution returns the actor and session ids a denial audit row may
// name.
//
// The session id is named only when authority confirms that exact session row
// exists: audit_events.session_id carries its own foreign key, so naming a
// session the reload could not find would fail the audit insert itself.
func (p Policy) denialAttribution(actor Actor, authority Authority) (actorID, sessionID uuid.NullUUID) {
	if p.Attribution == AttributeActorAlways || authority.StaffIdentityID == actor.StaffID {
		actorID = uuid.NullUUID{UUID: actor.StaffID, Valid: true}
	}
	if authority.SessionID == actor.SessionID {
		sessionID = uuid.NullUUID{UUID: actor.SessionID, Valid: true}
	}
	return actorID, sessionID
}

// recordDenial inserts the denial audit event. The details carry the
// operation and the reason text; the reason may name an approval failure such
// as INVALID_PIN, which is why it is audited and logged but never returned to
// a client by the slices' error mappings.
func (r *Runner) recordDenial(ctx context.Context, q *sqlc.Queries, actor Actor,
	authority Authority, operation string, denialErr error,
) error {
	details, _ := json.Marshal(map[string]string{
		"operation": operation,
		"reason":    denialErr.Error(),
	})
	actorID, sessionID := r.policy.denialAttribution(actor, authority)
	if _, err := q.InsertAuditEvent(ctx, sqlc.InsertAuditEventParams{
		EventType:  r.policy.DenialEventType,
		ActorID:    actorID,
		SessionID:  sessionID,
		Details:    details,
		OccurredAt: time.Now(),
	}); err != nil {
		return fmt.Errorf("insert denial audit event: %w", err)
	}
	slog.Warn(r.denialLogMessage(),
		"operation", operation,
		"reason", denialErr.Error(),
		"staff_identity_id", actor.StaffID)
	return nil
}

func (r *Runner) denialLogMessage() string {
	if r.policy.LogScope == "" {
		return "authorization denied"
	}
	return r.policy.LogScope + " authorization denied"
}

// deny records denialErr inside the mutation transaction and commits it, so
// the security evidence survives the failed operation, then resolves the
// error the caller returns according to Policy.OnDenialAuditFailure.
func (r *Runner) deny(ctx context.Context, tx *sql.Tx, q *sqlc.Queries, actor Actor,
	authority Authority, operation string, denialErr error,
) error {
	if err := r.recordDenial(ctx, q, actor, authority, operation, denialErr); err != nil {
		if r.policy.OnDenialAuditFailure == ReturnAuditError {
			return err
		}
		slog.Error("insert denial audit event",
			"operation", operation,
			"reason", denialErr.Error(),
			"staff_identity_id", actor.StaffID,
			"error", err)
		return denialErr
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit authorization denial: %w", err)
	}
	return denialErr
}

// auditReadDenial records a read-path denial in a short separate read-write
// transaction, since the read's own transaction is read-only and PostgreSQL
// rejects a write inside it. It is best-effort by construction: any failure is
// logged, and the original denial is always what the read returns.
func (r *Runner) auditReadDenial(ctx context.Context, actor Actor,
	authority Authority, operation string, denialErr error,
) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		slog.Error("begin read denial audit transaction", "operation", operation, "error", err)
		return
	}
	defer tx.Rollback() //nolint:errcheck

	if err := r.recordDenial(ctx, r.queries.WithTx(tx), actor, authority, operation, denialErr); err != nil {
		slog.Error("insert denial audit event",
			"operation", operation,
			"reason", denialErr.Error(),
			"staff_identity_id", actor.StaffID,
			"error", err)
		return
	}
	if err := tx.Commit(); err != nil {
		slog.Error("commit read denial audit transaction", "operation", operation, "error", err)
	}
}
