package tables

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
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
// Tables has no price-sensitive command, so there is no Manager PIN field.
type MutationSpec struct {
	RequestID   uuid.UUID
	Operation   string
	Fingerprint any
	Required    []string
}

// AuditRecord describes the audit event to insert after a successful mutation.
// A zero EventType means no business event is written, which is how a
// same-state Availability no-op reports itself.
type AuditRecord struct {
	EventType string
	Details   any
}

// committedDenial signals that a denial audit event was written and must be
// committed even though the operation itself failed.
type committedDenial struct {
	err error
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

// reloadAuthority loads the current session, roles, and capabilities inside
// the transaction, so authority removed mid-session takes effect immediately.
func reloadAuthority(ctx context.Context, q *sqlc.Queries, actor Actor) (
	sqlc.GetTablesSessionAuthorityRow, []string, error,
) {
	authRow, err := q.GetTablesSessionAuthority(ctx, sqlc.GetTablesSessionAuthorityParams{
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

	roles, err := q.GetTablesSessionRoles(ctx, authRow.StaffIdentityID)
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

// recordDenial writes a denial audit event and returns an outcome that must be
// committed so the security evidence survives the failed operation.
func recordDenial(ctx context.Context, q *sqlc.Queries, actor Actor,
	authority sqlc.GetTablesSessionAuthorityRow, operation string, denialErr error,
) error {
	details, _ := json.Marshal(map[string]string{
		"operation": operation,
		"reason":    denialErr.Error(),
	})
	actorID := uuid.NullUUID{}
	sessionID := uuid.NullUUID{}
	if authority.StaffIdentityID == actor.StaffID {
		actorID = uuid.NullUUID{UUID: actor.StaffID, Valid: true}
	}
	if authority.SessionID == actor.SessionID {
		sessionID = uuid.NullUUID{UUID: actor.SessionID, Valid: true}
	}
	if _, err := q.InsertAuditEvent(ctx, sqlc.InsertAuditEventParams{
		EventType:  EventAuthorizationDenied,
		ActorID:    actorID,
		SessionID:  sessionID,
		Details:    details,
		OccurredAt: time.Now(),
	}); err != nil {
		return fmt.Errorf("insert denial audit event: %w", err)
	}
	return &committedDenial{err: denialErr}
}

func finishDenial(tx *sql.Tx, outcome error) error {
	var denial *committedDenial
	if !errors.As(outcome, &denial) {
		return outcome
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit authorization denial: %w", err)
	}
	return denial.err
}

func isSecurityDenial(err error) bool {
	return errors.Is(err, ErrUnauthorized) || errors.Is(err, ErrForbidden)
}

// ExecuteMutation runs a mutation inside one transaction with authorization,
// idempotency, and audit. On success it returns the HTTP status and result.
func ExecuteMutation[T any](ctx context.Context, r *Runner, actor Actor,
	spec MutationSpec,
	fn func(*sqlc.Queries) (int, T, AuditRecord, error),
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
	lockKey := idToLockKey(actor.StaffID) ^ idToLockKey(spec.RequestID)
	if err := q.TablesAdvisoryLock(ctx, lockKey); err != nil {
		return 0, zero, fmt.Errorf("advisory lock: %w", err)
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
	resultCode, result, audit, err := fn(q)
	if err != nil {
		return 0, zero, err
	}

	// 8. Write the business audit event, when there is one. A same-state
	// Availability no-op returns a zero EventType and writes none.
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

// ExecuteRead runs a read inside a read-only repeatable-read transaction so
// the capability check and every query observe one snapshot.
func ExecuteRead[T any](ctx context.Context, r *Runner, actor Actor,
	requiredCapability string,
	fn func(*sqlc.Queries) (T, error),
) (T, error) {
	var zero T

	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return zero, fmt.Errorf("begin read transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	q := r.queries.WithTx(tx)

	if _, caps, err := reloadAuthority(ctx, q, actor); err != nil {
		return zero, err
	} else if err := verifyCapabilities([]string{requiredCapability}, caps); err != nil {
		return zero, err
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
