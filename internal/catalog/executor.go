package catalog

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
	return int64(binary.BigEndian.Uint64(id[:8]))
}

// fpHash computes SHA-256 of the operation + JSON-encoded fingerprint.
func fpHash(operation string, v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(append([]byte(operation), b...))
	return fmt.Sprintf("%x", h)
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

// commitDenial inserts a denial audit event and returns a committedDenial error.
// The caller must return this error from ExecuteMutation to trigger commit.
func commitDenial(ctx context.Context, q *sqlc.Queries, actor Actor, operation string, denialErr error) error {
	details, _ := json.Marshal(map[string]string{
		"operation": operation,
		"reason":    denialErr.Error(),
	})
	_, err := q.InsertAuditEvent(ctx, sqlc.InsertAuditEventParams{
		EventType:  "catalog.denial",
		ActorID:    uuid.NullUUID{UUID: actor.StaffID, Valid: true},
		SessionID:  uuid.NullUUID{UUID: actor.SessionID, Valid: true},
		Details:    details,
		OccurredAt: time.Now(),
	})
	if err != nil {
		return fmt.Errorf("insert denial audit event: %w", err)
	}
	return &committedDenial{err: denialErr}
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
		if errors.As(err, new(*committedDenial)) {
			if commitErr := tx.Commit(); commitErr != nil {
				return 0, zero, fmt.Errorf("commit denial: %w", commitErr)
			}
			return 0, zero, err
		}
		return 0, zero, err
	}

	// 2. Verify capabilities
	if err := verifyCapabilities(spec.Required, caps); err != nil {
		if commitErr := commitDenial(ctx, q, actor, spec.Operation, err); commitErr != nil {
			if commitErr := tx.Commit(); commitErr != nil {
				return 0, zero, fmt.Errorf("commit denial: %w", commitErr)
			}
			return 0, zero, commitErr
		}
		return 0, zero, err
	}

	// 3. Verify fresh Manager PIN when price-sensitive
	if spec.RequireManagerPIN {
		if err := verifyManagerPIN(ctx, q, authRow.StaffIdentityID, spec.ManagerPIN); err != nil {
			if commitErr := commitDenial(ctx, q, actor, spec.Operation, err); commitErr != nil {
				if commitErr := tx.Commit(); commitErr != nil {
					return 0, zero, fmt.Errorf("commit denial: %w", commitErr)
				}
				return 0, zero, commitErr
			}
			return 0, zero, err
		}
	}

	// 4. Compute request hash (PIN excluded by design)
	reqHash := fpHash(spec.Operation, spec.Fingerprint)

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
		if existing.RequestHash != reqHash {
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

	// 7-9. Execute mutation, insert audit, and store idempotent result
	resultCode, result, audit, err := fn(q)
	if err != nil {
		return 0, zero, err
	}

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
		slog.Error("audit event insert failed, rolling back mutation", "error", err, "operation", spec.Operation)
		return 0, zero, fmt.Errorf("insert audit event: %w", err)
	}

	bodyBytes, err := json.Marshal(result)
	if err != nil {
		return 0, zero, fmt.Errorf("marshal response body: %w", err)
	}

	// Store idempotent result
	_, err = q.ClaimCatalogRequest(ctx, sqlc.ClaimCatalogRequestParams{
		ActorID:      actor.StaffID,
		RequestID:    spec.RequestID,
		Operation:    spec.Operation,
		RequestHash:  reqHash,
		ResponseCode: int32(resultCode),
		ResponseBody: bodyBytes,
	})
	if err != nil {
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
