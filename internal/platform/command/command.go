// Package command is the shared transactional pipeline every vertical slice
// runs its commands and authorized reads through.
//
// A mutation runs in one read-write transaction whose steps are, in order:
// reload the actor's authority, verify capabilities, run the spec's optional
// Gate and second-party Approval, fingerprint the business input, take an
// advisory lock on (actor, request), replay or reject a previously stored
// result, claim the request, run the body, write the business audit event, and
// store the replayable result. The order is load-bearing: authority and gates
// are re-checked before a replay, so a revoked session, a lost role, or a
// rotated PIN cannot replay an earlier success.
//
// A read runs in a read-only REPEATABLE READ transaction so the authority
// check and every query in the body observe one snapshot.
//
// What differs between slices (denial event type, attribution, how a failed
// denial audit is surfaced, read-denial auditing, idempotency storage) is
// declared once per slice in a Policy bound to its Runner.
package command

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

// Sentinel errors produced by the pipeline. Slices alias them
// (var ErrForbidden = command.ErrForbidden) so errors.Is and their HTTP
// mappings keep working unchanged.
var (
	ErrUnauthorized        = errors.New("unauthorized")
	ErrForbidden           = errors.New("forbidden")
	ErrRequestConflict     = errors.New("request conflict")
	ErrInvalidStoredResult = errors.New("invalid stored result")
)

// Actor identifies the authenticated staff member executing an operation.
type Actor struct {
	StaffID   uuid.UUID
	SessionID uuid.UUID
}

// Authority is the session and identity row reloaded inside the transaction.
type Authority = sqlc.GetSessionAuthorityRow

// Runner holds the database connection, the generated queries, and the
// owning slice's Policy.
type Runner struct {
	db      *sql.DB
	queries *sqlc.Queries
	policy  Policy
}

// NewRunner creates a Runner bound to a slice's Policy. It panics when the
// policy names no denial event type, since every denial must be auditable.
func NewRunner(db *sql.DB, queries *sqlc.Queries, policy Policy) *Runner {
	if policy.DenialEventType == "" {
		panic("command: Policy.DenialEventType is required")
	}
	if policy.Idempotency == nil {
		policy.Idempotency = IdempotencyKeys{}
	}
	return &Runner{db: db, queries: queries, policy: policy}
}

// DB returns the underlying connection pool.
func (r *Runner) DB() *sql.DB { return r.db }

// Queries returns the non-transactional generated queries.
func (r *Runner) Queries() *sqlc.Queries { return r.queries }

// AdvisoryLock takes a transaction-scoped advisory lock on key. It is exposed
// for slice code that must serialize on something other than the request,
// such as Service Number allocation on a Sales Shift id.
func (r *Runner) AdvisoryLock(ctx context.Context, q *sqlc.Queries, key int64) error {
	if err := q.AdvisoryXactLock(ctx, key); err != nil {
		return fmt.Errorf("advisory lock: %w", err)
	}
	return nil
}

// IDToLockKey converts a UUID to a stable int64 for advisory locking.
func IDToLockKey(id uuid.UUID) int64 {
	//nolint:gosec // G115: bit-cast first 8 bytes of UUID to int64 for advisory lock key
	return int64(binary.BigEndian.Uint64(id[:8]))
}

// RequestLockKey is the advisory lock key that serializes concurrent
// duplicates of one request by one actor.
func RequestLockKey(staffID, requestID uuid.UUID) int64 {
	return IDToLockKey(staffID) ^ IDToLockKey(requestID)
}

// FingerprintHash computes the SHA-256 of the JSON-encoded business
// fingerprint that the idempotency record is keyed on.
func FingerprintHash(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("marshal mutation fingerprint: %w", err)
	}
	h := sha256.Sum256(b)
	return fmt.Sprintf("%x", h), nil
}

// VerifyCapabilities checks that every required capability is available.
func VerifyCapabilities(required []string, available []string) error {
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
