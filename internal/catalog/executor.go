package catalog

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

// Actor identifies the authenticated staff member executing a command.
type Actor struct {
	StaffID   uuid.UUID
	SessionID uuid.UUID
}

// MutationSpec carries request-level metadata for a mutation command.
// Fingerprint must be a dedicated struct — never contain ManagerPIN.
type MutationSpec struct {
	RequestID         uuid.UUID
	Operation         string
	Fingerprint       any
	Required          []string
	ManagerPIN        string
	RequireManagerPIN bool
}

// AuditRecord describes the audit event to insert after a successful mutation.
type AuditRecord struct {
	EventType string
	Details   any
}

// committedDenial signals that a denial was committed and should not roll back.
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

// reloadAuthority loads the current session, roles, and capabilities within the transaction.
func reloadAuthority(ctx context.Context, q *sqlc.Queries, actor Actor) (sqlc.GetCatalogSessionAuthorityRow, []string, []string, error) {
	authRow, err := q.GetCatalogSessionAuthority(ctx, sqlc.GetCatalogSessionAuthorityParams{
		ID:              actor.SessionID,
		StaffIdentityID: actor.StaffID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return authRow, nil, nil, fmt.Errorf("%w: session not found or identity mismatch", ErrUnauthorized)
		}
		return authRow, nil, nil, fmt.Errorf("reload session authority: %w", err)
	}

	if authRow.State == auth.SessionStateLocked {
		return authRow, nil, nil, fmt.Errorf("%w: session locked", ErrUnauthorized)
	}
	if authRow.RevokedAt.Valid {
		return authRow, nil, nil, fmt.Errorf("%w: session revoked", ErrUnauthorized)
	}
	if time.Now().After(authRow.ExpiresAt) {
		return authRow, nil, nil, fmt.Errorf("%w: session expired", ErrUnauthorized)
	}
	if !authRow.IdentityEnabled {
		return authRow, nil, nil, fmt.Errorf("%w: identity disabled", ErrForbidden)
	}
	workspace := ""
	if authRow.ActiveWorkspace.Valid {
		workspace = authRow.ActiveWorkspace.String
	}
	if time.Since(authRow.LastHumanActivityAt) >= auth.GetInactivityTimeout(workspace) {
		return authRow, nil, nil, fmt.Errorf("%w: session inactive", ErrUnauthorized)
	}

	roles, err := q.GetCatalogSessionRoles(ctx, authRow.StaffIdentityID)
	if err != nil {
		return authRow, nil, nil, fmt.Errorf("reload session roles: %w", err)
	}

	caps := auth.DeriveCapabilities(roles)
	return authRow, roles, caps, nil
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

// verifyManagerPIN checks a fresh Manager PIN when required.
func verifyManagerPIN(ctx context.Context, q *sqlc.Queries, staffID uuid.UUID, pin string) error {
	staff, err := q.GetStaffByID(ctx, staffID)
	if err != nil {
		return fmt.Errorf("load staff for PIN verification: %w", err)
	}
	if !auth.VerifyPin(staff.PinHash, pin) {
		return fmt.Errorf("%w: manager PIN verification failed", ErrInvalidManagerPin)
	}
	return nil
}

// recordDenial inserts a denial audit event and returns an outcome that must be committed.
func recordDenial(ctx context.Context, q *sqlc.Queries, actor Actor,
	authority sqlc.GetCatalogSessionAuthorityRow, operation string, denialErr error,
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
	_, err := q.InsertAuditEvent(ctx, sqlc.InsertAuditEventParams{
		EventType:  EventAuthorizationDenied,
		ActorID:    actorID,
		SessionID:  sessionID,
		Details:    details,
		OccurredAt: time.Now(),
	})
	if err != nil {
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
	return errors.Is(err, ErrUnauthorized) ||
		errors.Is(err, ErrForbidden) ||
		errors.Is(err, ErrInvalidManagerPin)
}

// ExecuteMutation runs a mutation inside a transaction with authorization,
// idempotency, and audit. On success it returns the HTTP status code and result.
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

	// 1. Reload current authority
	authRow, _, caps, err := reloadAuthority(ctx, q, actor)
	if err != nil {
		if isSecurityDenial(err) {
			outcome := recordDenial(ctx, q, actor, authRow, spec.Operation, err)
			return 0, zero, finishDenial(tx, outcome)
		}
		return 0, zero, err
	}

	// 2. Verify capabilities
	if err := verifyCapabilities(spec.Required, caps); err != nil {
		outcome := recordDenial(ctx, q, actor, authRow, spec.Operation, err)
		return 0, zero, finishDenial(tx, outcome)
	}

	// 3. Verify fresh Manager PIN when price-sensitive
	if spec.RequireManagerPIN {
		if err := verifyCapabilities([]string{"catalog.change_price"}, caps); err != nil {
			outcome := recordDenial(ctx, q, actor, authRow, spec.Operation, err)
			return 0, zero, finishDenial(tx, outcome)
		}
		if err := verifyManagerPIN(ctx, q, authRow.StaffIdentityID, spec.ManagerPIN); err != nil {
			outcome := recordDenial(ctx, q, actor, authRow, spec.Operation, err)
			return 0, zero, finishDenial(tx, outcome)
		}
	}

	// 4. Compute request hash (PIN excluded by design)
	reqHash, err := fpHash(spec.Fingerprint)
	if err != nil {
		return 0, zero, err
	}

	// 5. Advisory lock to serialize concurrent duplicate execution
	lockKey := idToLockKey(actor.StaffID) ^ idToLockKey(spec.RequestID)
	if err := q.CatalogAdvisoryLock(ctx, lockKey); err != nil {
		return 0, zero, fmt.Errorf("advisory lock: %w", err)
	}

	// 6. Check for existing idempotency record (after lock to avoid races)
	existing, err := q.GetCatalogMutationRequest(ctx, sqlc.GetCatalogMutationRequestParams{
		ActorID:   actor.StaffID,
		RequestID: spec.RequestID,
	})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, zero, fmt.Errorf("lookup idempotency: %w", err)
	}

	// If found, check for exact replay or conflict
	if err == nil {
		if existing.Operation != spec.Operation || existing.RequestHash != reqHash {
			return 0, zero, fmt.Errorf("%w: operation %q hash mismatch (stored: %s, current: %s)",
				ErrRequestConflict, existing.Operation, existing.RequestHash, reqHash)
		}
		// Exact replay
		var result T
		if err := json.Unmarshal(existing.ResponseBody, &result); err != nil {
			return 0, zero, fmt.Errorf("%w: stored response body is corrupted", ErrInvalidStoredResult)
		}
		return int(existing.ResponseCode), result, nil
	}

	// 7. Claim the request before any business mutation.
	claimed, err := q.ClaimCatalogRequest(ctx, sqlc.ClaimCatalogRequestParams{
		ActorID:      actor.StaffID,
		RequestID:    spec.RequestID,
		Operation:    spec.Operation,
		RequestHash:  reqHash,
		ResponseCode: 0,
		ResponseBody: json.RawMessage("null"),
	})
	if err != nil {
		return 0, zero, fmt.Errorf("claim idempotency request: %w", err)
	}
	if claimed.Operation != spec.Operation || claimed.RequestHash != reqHash {
		return 0, zero, fmt.Errorf("%w: request was claimed concurrently", ErrRequestConflict)
	}

	// 8-10. Execute mutation, insert audit, and store idempotent result.
	resultCode, result, audit, err := fn(q)
	if err != nil {
		return 0, zero, err
	}

	if audit.EventType != "" {
		detailsBytes, err := json.Marshal(audit.Details)
		if err != nil {
			return 0, zero, fmt.Errorf("marshal audit details: %w", err)
		}

		_, err = q.InsertAuditEvent(ctx, sqlc.InsertAuditEventParams{
			EventType:  audit.EventType,
			ActorID:    uuid.NullUUID{UUID: actor.StaffID, Valid: true},
			SessionID:  uuid.NullUUID{UUID: actor.SessionID, Valid: true},
			Details:    detailsBytes,
			OccurredAt: time.Now(),
		})
		if err != nil {
			return 0, zero, fmt.Errorf("insert audit event for %q: %w", spec.Operation, err)
		}
	}

	bodyBytes, err := json.Marshal(result)
	if err != nil {
		return 0, zero, fmt.Errorf("marshal response body: %w", err)
	}

	if err := q.StoreCatalogRequestResult(ctx, sqlc.StoreCatalogRequestResultParams{
		ActorID:      actor.StaffID,
		RequestID:    spec.RequestID,
		ResponseCode: int32(resultCode), //nolint:gosec // G115: HTTP status code (100-599) fits within int32
		ResponseBody: bodyBytes,
	}); err != nil {
		return 0, zero, fmt.Errorf("store idempotent result: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, zero, fmt.Errorf("commit transaction: %w", err)
	}

	return resultCode, result, nil
}

// ExecuteRead runs a read-only query inside a repeatable-read transaction
// with current authority verification.
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

	_, _, caps, err := reloadAuthority(ctx, q, actor)
	if err != nil {
		return zero, err
	}

	if err := verifyCapabilities([]string{requiredCapability}, caps); err != nil {
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
