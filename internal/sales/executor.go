package sales

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

// MutationSpec carries request-level metadata for a mutation command.
//
// No Sales operation in 5A takes a PIN or any other secret, so unlike Shift's
// spec there is no approval field and no secret can reach the fingerprint.
type MutationSpec struct {
	RequestID   uuid.UUID
	Operation   string
	Fingerprint any
	Required    []string
}

// MutationContext carries per-execution facts the mutation body needs.
type MutationContext struct {
	Queries *sqlc.Queries
}

// AuditRecord describes the audit event to insert after a successful mutation.
// A zero EventType writes no business event.
type AuditRecord struct {
	EventType string
	Details   any
}

// committedDenial carries a resolved authorization denial: err is the original
// denial reason the client must see, and committed reports whether its audit
// event was durably written.
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

// IDToLockKey converts a UUID to a stable int64 for advisory locking.
func IDToLockKey(id uuid.UUID) int64 {
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
	sqlc.GetSalesSessionAuthorityRow, []string, error,
) {
	authRow, err := q.GetSalesSessionAuthority(ctx, sqlc.GetSalesSessionAuthorityParams{
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

	roles, err := q.GetSalesSessionRoles(ctx, authRow.StaffIdentityID)
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

// recordDenial writes a denial audit event and returns the resolved outcome.
//
// The session id is attributed only when authority confirms that exact session
// row still exists, because audit_events.session_id carries its own foreign
// key: naming a session this transaction could not find would fail the audit
// insert itself.
func recordDenial(ctx context.Context, q *sqlc.Queries, actor Actor,
	authority sqlc.GetSalesSessionAuthorityRow, operation string, denialErr error,
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
	slog.Warn("sales authorization denied",
		"operation", operation,
		"reason", denialErr.Error(),
		"staff_identity_id", actor.StaffID)
	return &committedDenial{err: denialErr, committed: true}
}

// finishDenial commits the denial's audit event when it was written and always
// returns the original denial reason.
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

// AdvisoryLock takes a transaction-scoped advisory lock on the given key.
// Exposed so Service Number allocation can serialize on the Sales Shift id.
func (r *Runner) AdvisoryLock(ctx context.Context, q *sqlc.Queries, key int64) error {
	if err := q.SalesAdvisoryLock(ctx, key); err != nil {
		return fmt.Errorf("advisory lock: %w", err)
	}
	return nil
}

// ExecuteMutation runs a mutation inside one transaction with authorization,
// idempotency, and audit. On success it returns the HTTP status and result.
//
// The order of steps is load-bearing. Authority is reloaded before the
// idempotency replay, so an actor whose session was revoked or whose role was
// removed cannot replay an earlier success. The open-Sales-Shift precondition
// deliberately lives inside fn, which runs after the claim, so a replay of a
// request that succeeded during a Shift still returns its stored result after
// that Shift closes.
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

	// 2. Verify capabilities.
	if err := verifyCapabilities(spec.Required, caps); err != nil {
		return 0, zero, finishDenial(tx, recordDenial(ctx, q, actor, authRow, spec.Operation, err))
	}

	// 3. Fingerprint the normalized business input.
	reqHash, err := fpHash(spec.Fingerprint)
	if err != nil {
		return 0, zero, err
	}

	// 4. Advisory lock to serialize concurrent duplicates of this request.
	if err := r.AdvisoryLock(ctx, q, IDToLockKey(actor.StaffID)^IDToLockKey(spec.RequestID)); err != nil {
		return 0, zero, err
	}

	// 5. Look for an existing record, now that the lock is held.
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

	// 6. Claim the request before any business mutation.
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

	// 7. Run the mutation.
	resultCode, result, audit, err := fn(MutationContext{Queries: q})
	if err != nil {
		return 0, zero, err
	}

	// 8. Write the business audit event, when there is one.
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

	// 9. Store the replayable result.
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

// auditReadDenial records a read-path denial in a short separate read-write
// transaction, since ExecuteRead's own transaction is read-only.
func (r *Runner) auditReadDenial(ctx context.Context, actor Actor,
	authority sqlc.GetSalesSessionAuthorityRow, operation string, denialErr error,
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
