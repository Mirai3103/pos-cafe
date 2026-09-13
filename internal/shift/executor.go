package shift

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

// Actor identifies the authenticated staff member executing an operation.
type Actor struct {
	StaffID   uuid.UUID
	SessionID uuid.UUID
}

// ApprovalSpec requires a second identity holding the Manager role to
// authenticate inline before the request is claimed.
type ApprovalSpec struct {
	ApproverLoginCode string
	ManagerPIN        string
	// RequiredCapability is the capability the approver must also hold, so a
	// Manager cannot approve an operation outside their own authority.
	RequiredCapability string
}

// MutationSpec carries request-level metadata for a mutation command.
//
// Fingerprint must never contain a PIN. Including a secret would make the
// idempotency key sensitive to it and would store a PIN-derived value at rest.
type MutationSpec struct {
	RequestID   uuid.UUID
	Operation   string
	Fingerprint any
	Required    []string
	// Approval is nil for operations needing no second-party approval.
	Approval *ApprovalSpec
}

// MutationContext carries per-execution facts the mutation body needs.
// Approver is nil when the spec required no approval.
type MutationContext struct {
	Queries  *sqlc.Queries
	Approver *auth.ApproverSummary
}

// AuditRecord describes the audit event to insert after a successful mutation.
// A zero EventType writes no business event.
type AuditRecord struct {
	EventType string
	Details   any
}

// committedDenial carries a resolved authorization denial: err is the
// original denial reason the client must see, and committed reports whether
// its audit event was durably written. committed is false only when the audit
// insert itself failed, which is logged but must never mask err with a
// different, unmapped error.
type committedDenial struct {
	err       error
	committed bool
}

func (e *committedDenial) Error() string { return e.err.Error() }
func (e *committedDenial) Unwrap() error { return e.err }

// Runner holds the database connection and generated queries.
type Runner struct {
	db      *sql.DB
	queries *sqlc.Queries
}

// NewRunner creates a Runner.
func NewRunner(db *sql.DB, queries *sqlc.Queries) *Runner {
	return &Runner{db: db, queries: queries}
}

// idToLockKey converts a UUID to a stable int64 for advisory locking.
func idToLockKey(id uuid.UUID) int64 {
	//nolint:gosec // G115: bit-cast first 8 bytes of UUID to int64 for advisory lock key
	return int64(binary.BigEndian.Uint64(id[:8]))
}

// fpHash computes SHA-256 of the JSON-encoded business fingerprint.
func fpHash(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("marshal mutation fingerprint: %w", err)
	}
	h := sha256.Sum256(b)
	return fmt.Sprintf("%x", h), nil
}

// reloadAuthority loads the current session, roles, and capabilities inside the
// transaction, so authority removed mid-session takes effect immediately.
func reloadAuthority(ctx context.Context, q *sqlc.Queries, actor Actor) (
	sqlc.GetShiftSessionAuthorityRow, []string, error,
) {
	authRow, err := q.GetShiftSessionAuthority(ctx, sqlc.GetShiftSessionAuthorityParams{
		ID:              actor.SessionID,
		StaffIdentityID: actor.StaffID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return authRow, nil, fmt.Errorf("%w: session not found or identity mismatch", ErrUnauthorized)
		}
		return authRow, nil, fmt.Errorf("reload session authority: %w", err)
	}

	if authRow.State == auth.SessionStateLocked {
		return authRow, nil, fmt.Errorf("%w: session locked", ErrUnauthorized)
	}
	if authRow.RevokedAt.Valid {
		return authRow, nil, fmt.Errorf("%w: session revoked", ErrUnauthorized)
	}
	if time.Now().After(authRow.ExpiresAt) {
		return authRow, nil, fmt.Errorf("%w: session expired", ErrUnauthorized)
	}
	if !authRow.IdentityEnabled {
		return authRow, nil, fmt.Errorf("%w: identity disabled", ErrForbidden)
	}
	workspace := ""
	if authRow.ActiveWorkspace.Valid {
		workspace = authRow.ActiveWorkspace.String
	}
	if time.Since(authRow.LastHumanActivityAt) >= auth.GetInactivityTimeout(workspace) {
		return authRow, nil, fmt.Errorf("%w: session inactive", ErrUnauthorized)
	}

	roles, err := q.GetShiftSessionRoles(ctx, authRow.StaffIdentityID)
	if err != nil {
		return authRow, nil, fmt.Errorf("reload session roles: %w", err)
	}

	return authRow, auth.DeriveCapabilities(roles), nil
}

// verifyCapabilities checks that all required capabilities are present.
func verifyCapabilities(required []string, available []string) error {
	avail := make(map[string]struct{}, len(available))
	for _, c := range available {
		avail[c] = struct{}{}
	}
	for _, need := range required {
		if _, ok := avail[need]; !ok {
			return fmt.Errorf("%w: missing capability %q", ErrForbidden, need)
		}
	}
	return nil
}

// recordDenial writes a denial audit event and returns the resolved outcome
// for finishDenial or auditReadDenial to commit.
//
// actor.StaffID is always attributed: staff_identities rows are never
// deleted (ON DELETE RESTRICT), and actor is built only from a session the
// HTTP middleware already validated moments earlier (see
// auth.Middleware.RequireAuth), so the identity is never in doubt even when
// this transaction's own reload denies the request.
//
// actor.SessionID is attributed only when authority confirms that exact
// session row still exists: audit_events.session_id carries its own foreign
// key, so writing a session id this transaction could not find (the reload
// missed with sql.ErrNoRows) would itself fail the audit insert. On every
// other denial — locked, revoked, expired, disabled identity, inactive, or
// an insufficient capability — reloadAuthority already found the row by
// exactly (actor.SessionID, actor.StaffID), so authority.SessionID always
// equals actor.SessionID and the audit event names it.
//
// The details carry the operation and the reason text. The reason may name an
// approval denial such as INVALID_PIN, which is why this value is audited and
// logged but never returned to a client.
func recordDenial(ctx context.Context, q *sqlc.Queries, actor Actor,
	authority sqlc.GetShiftSessionAuthorityRow, operation string, denialErr error,
) *committedDenial {
	details, _ := json.Marshal(map[string]string{
		"operation": operation,
		"reason":    denialErr.Error(),
	})
	sessionID := uuid.NullUUID{}
	if authority.SessionID == actor.SessionID {
		sessionID = uuid.NullUUID{UUID: actor.SessionID, Valid: true}
	}
	if _, err := q.InsertAuditEvent(ctx, sqlc.InsertAuditEventParams{
		EventType:  EventAuthorizationDenied,
		ActorID:    uuid.NullUUID{UUID: actor.StaffID, Valid: true},
		SessionID:  sessionID,
		Details:    details,
		OccurredAt: time.Now(),
	}); err != nil {
		slog.Error("insert denial audit event",
			"operation", operation,
			"reason", denialErr.Error(),
			"staff_identity_id", actor.StaffID,
			"error", err)
		return &committedDenial{err: denialErr, committed: false}
	}
	slog.Warn("shift authorization denied",
		"operation", operation,
		"reason", denialErr.Error(),
		"staff_identity_id", actor.StaffID)
	return &committedDenial{err: denialErr, committed: true}
}

// finishDenial commits the denial's audit event when it was written and
// always returns the original denial reason, so a failed audit insert can
// never surface as an unmapped error in place of the real 401/403/409.
func finishDenial(tx *sql.Tx, outcome *committedDenial) error {
	if outcome.committed {
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit authorization denial: %w", err)
		}
	}
	return outcome.err
}

func isSecurityDenial(err error) bool {
	return errors.Is(err, ErrUnauthorized) || errors.Is(err, ErrForbidden)
}

// ExecuteMutation runs a mutation inside one transaction with authorization,
// optional second-party approval, idempotency, and audit. On success it returns
// the HTTP status and result.
func ExecuteMutation[T any](ctx context.Context, r *Runner, actor Actor,
	spec MutationSpec,
	fn func(MutationContext) (int, T, AuditRecord, error),
) (int, T, error) {
	var zero T

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, zero, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	q := r.queries.WithTx(tx)

	// 1. Reload current authority inside the transaction.
	authRow, caps, err := reloadAuthority(ctx, q, actor)
	if err != nil {
		if isSecurityDenial(err) {
			return 0, zero, finishDenial(tx, recordDenial(ctx, q, actor, authRow, spec.Operation, err))
		}
		return 0, zero, err
	}

	// 2. Verify the initiator's capabilities.
	if err := verifyCapabilities(spec.Required, caps); err != nil {
		return 0, zero, finishDenial(tx, recordDenial(ctx, q, actor, authRow, spec.Operation, err))
	}

	// 3. Verify second-party Manager approval, when the operation needs one.
	// This runs before the idempotency claim, so a replay cannot succeed using
	// an approver who has since been disabled or demoted. The approver row stays
	// locked for the rest of this transaction.
	var approver *auth.ApproverSummary
	if spec.Approval != nil {
		summary, approvalErr := auth.VerifyManagerApproval(ctx, q,
			spec.Approval.ApproverLoginCode,
			spec.Approval.ManagerPIN,
			spec.Approval.RequiredCapability)
		if approvalErr != nil {
			if errors.Is(approvalErr, auth.ErrManagerApprovalDenied) {
				denial := fmt.Errorf("%w: %s", ErrManagerApprovalUnavailable, approvalErr.Error())
				return 0, zero, finishDenial(tx, recordDenial(ctx, q, actor, authRow, spec.Operation, denial))
			}
			return 0, zero, approvalErr
		}
		approver = &summary
	}

	// 4. Fingerprint the normalized business input. The spec's Fingerprint type
	// has no PIN field, so no secret reaches the hash.
	reqHash, err := fpHash(spec.Fingerprint)
	if err != nil {
		return 0, zero, err
	}

	// 5. Advisory lock to serialize concurrent duplicates of this request.
	lockKey := idToLockKey(actor.StaffID) ^ idToLockKey(spec.RequestID)
	if err := q.ShiftAdvisoryLock(ctx, lockKey); err != nil {
		return 0, zero, fmt.Errorf("advisory lock: %w", err)
	}

	// 6. Look for an existing record, now that the lock is held.
	existing, err := q.GetIdempotencyRecord(ctx, sqlc.GetIdempotencyRecordParams{
		ActorID: actor.StaffID,
		Key:     spec.RequestID,
	})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, zero, fmt.Errorf("lookup idempotency: %w", err)
	}
	if err == nil {
		if existing.Action != spec.Operation || existing.RequestHash != reqHash {
			return 0, zero, fmt.Errorf("%w: request_id reused for a different change", ErrRequestConflict)
		}
		var result T
		if err := json.Unmarshal(existing.ResponseBody, &result); err != nil {
			return 0, zero, fmt.Errorf("%w: stored response body is corrupted", ErrInvalidStoredResult)
		}
		return int(existing.ResponseCode), result, nil
	}

	// 7. Claim the request before any business mutation.
	claimed, err := q.ClaimIdempotencyRecord(ctx, sqlc.ClaimIdempotencyRecordParams{
		Key:          spec.RequestID,
		ActorID:      actor.StaffID,
		Action:       spec.Operation,
		RequestHash:  reqHash,
		ResponseCode: 0,
		ResponseBody: json.RawMessage("null"),
	})
	if err != nil {
		return 0, zero, fmt.Errorf("claim idempotency request: %w", err)
	}
	if claimed.Action != spec.Operation || claimed.RequestHash != reqHash {
		return 0, zero, fmt.Errorf("%w: request was claimed concurrently", ErrRequestConflict)
	}

	// 8. Run the mutation.
	resultCode, result, audit, err := fn(MutationContext{Queries: q, Approver: approver})
	if err != nil {
		return 0, zero, err
	}

	// 9. Write the business audit event, when there is one.
	if audit.EventType != "" {
		detailsBytes, err := json.Marshal(audit.Details)
		if err != nil {
			return 0, zero, fmt.Errorf("marshal audit details: %w", err)
		}
		if _, err := q.InsertAuditEvent(ctx, sqlc.InsertAuditEventParams{
			EventType:  audit.EventType,
			ActorID:    uuid.NullUUID{UUID: actor.StaffID, Valid: true},
			SessionID:  uuid.NullUUID{UUID: actor.SessionID, Valid: true},
			Details:    detailsBytes,
			OccurredAt: time.Now(),
		}); err != nil {
			return 0, zero, fmt.Errorf("insert audit event for %q: %w", spec.Operation, err)
		}
	}

	// 10. Store the replayable result.
	bodyBytes, err := json.Marshal(result)
	if err != nil {
		return 0, zero, fmt.Errorf("marshal response body: %w", err)
	}
	if err := q.StoreIdempotencyResult(ctx, sqlc.StoreIdempotencyResultParams{
		ActorID:      actor.StaffID,
		Key:          spec.RequestID,
		ResponseCode: int32(resultCode), //nolint:gosec // G115: HTTP status (100-599) fits int32
		ResponseBody: bodyBytes,
	}); err != nil {
		return 0, zero, fmt.Errorf("store idempotent result: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, zero, fmt.Errorf("commit transaction: %w", err)
	}
	return resultCode, result, nil
}

// ExecuteRead runs a read inside a read-only repeatable-read transaction so the
// capability check and every query observe one snapshot.
//
// An authorization denial is audited exactly as it is for a mutation. The
// read's own transaction is read-only and cannot itself contain that write,
// so the denial is recorded in a short separate read-write transaction; see
// auditReadDenial.
func ExecuteRead[T any](ctx context.Context, r *Runner, actor Actor,
	operation string, requiredCapability string,
	fn func(*sqlc.Queries) (T, error),
) (T, error) {
	var zero T

	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return zero, fmt.Errorf("begin read transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	q := r.queries.WithTx(tx)

	authRow, caps, err := reloadAuthority(ctx, q, actor)
	if err != nil {
		if isSecurityDenial(err) {
			return zero, r.auditReadDenial(ctx, actor, authRow, operation, err)
		}
		return zero, err
	}
	if err := verifyCapabilities([]string{requiredCapability}, caps); err != nil {
		return zero, r.auditReadDenial(ctx, actor, authRow, operation, err)
	}

	result, err := fn(q)
	if err != nil {
		return zero, err
	}
	if err := tx.Commit(); err != nil {
		return zero, fmt.Errorf("commit read transaction: %w", err)
	}
	return result, nil
}

// auditReadDenial records a read-path authorization denial in a short
// separate read-write transaction, since ExecuteRead's own transaction is
// read-only and PostgreSQL rejects a write inside it. It always returns
// denialErr, whether or not the audit insert itself succeeded, matching
// finishDenial's guarantee for the mutation path.
func (r *Runner) auditReadDenial(ctx context.Context, actor Actor,
	authority sqlc.GetShiftSessionAuthorityRow, operation string, denialErr error,
) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		slog.Error("begin read denial audit transaction", "operation", operation, "error", err)
		return denialErr
	}
	defer tx.Rollback() //nolint:errcheck

	outcome := recordDenial(ctx, r.queries.WithTx(tx), actor, authority, operation, denialErr)
	if outcome.committed {
		if err := tx.Commit(); err != nil {
			slog.Error("commit read denial audit transaction", "operation", operation, "error", err)
		}
	}
	return denialErr
}
