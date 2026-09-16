package preparation

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

// EventAuthorizationDenied is the audit event type for an authorization
// denial raised by this package's executor. Each slice names the same
// condition under its own event type so the audit stream keeps the vertical
// slices distinguishable.
const EventAuthorizationDenied = "preparation.authorization_denied"

// Actor identifies the authenticated staff member executing an operation.
type Actor struct {
	StaffID   uuid.UUID
	SessionID uuid.UUID
}

// MutationSpec carries request-level metadata for a mutation command.
//
// Fingerprint stays credential-free: it is the normalized business input and
// must never carry the Manager PIN or any other secret. When RequireManagerPIN
// is set, ManagerPIN carries the actor's own current PIN; the executor
// verifies it inside the mutation transaction, before the fingerprint is
// hashed and before any idempotency replay, and never writes it into the
// fingerprint, an audit row, a log line, or any stored value.
type MutationSpec struct {
	RequestID   uuid.UUID
	Operation   string
	Fingerprint any
	Required    []string

	// RequireManagerPIN turns on the current-Manager self-PIN gate: the actor
	// must still be an enabled Manager holding the required capability and
	// must re-authenticate with their own current PIN, even when the request
	// is an exact replay of an earlier success.
	RequireManagerPIN bool
	// ManagerPIN is the actor's own plaintext PIN, consumed only when
	// RequireManagerPIN is true. It never reaches an error message, the audit
	// trail, or the stored result.
	ManagerPIN string
}

// MutationContext carries per-execution facts the mutation body needs.
type MutationContext struct {
	Queries *sqlc.Queries
	tx      *sql.Tx
}

// withUnitSavepoint runs fn inside a fixed, package-private savepoint so one
// unit's failure can be undone without aborting the surrounding bulk
// transaction: fn's error rolls the savepoint back and is returned for the
// caller to convert (or not) into a per-unit outcome, while a savepoint
// statement's own failure aborts the mutation outright. The name is a
// constant and carries no request data.
func (mc MutationContext) withUnitSavepoint(ctx context.Context,
	fn func(*sqlc.Queries) error,
) error {
	if _, err := mc.tx.ExecContext(ctx, "SAVEPOINT preparation_unit"); err != nil {
		return fmt.Errorf("create preparation unit savepoint: %w", err)
	}
	if err := fn(mc.Queries); err != nil {
		if _, rollbackErr := mc.tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT preparation_unit"); rollbackErr != nil {
			return fmt.Errorf("rollback preparation unit savepoint after callback failure: %w", rollbackErr)
		}
		if _, releaseErr := mc.tx.ExecContext(ctx, "RELEASE SAVEPOINT preparation_unit"); releaseErr != nil {
			return fmt.Errorf("release rolled-back preparation unit savepoint: %w", releaseErr)
		}
		return err
	}
	if _, err := mc.tx.ExecContext(ctx, "RELEASE SAVEPOINT preparation_unit"); err != nil {
		return fmt.Errorf("release preparation unit savepoint: %w", err)
	}
	return nil
}

// AuditRecord describes the audit event to insert after a successful mutation.
// A zero EventType writes no business event.
type AuditRecord struct {
	EventType string
	Details   any
}

// writePreparationAudits batches several audit events — possibly of differing
// types — into one insert through InsertPreparationAuditEventsBatch, with one
// actor, one session, and one shared occurrence time. Commands that emit more
// than one business event for a single mutation (Waste writes its fact and its
// alert event; State Correction writes one event per corrected unit) call it
// instead of the executor's single-AuditRecord step, which stays unchanged for
// single-event commands.
//
// The caller chooses occurredAt so every event of one mutation shares one
// moment. Empty input is a no-op. The event and details arrays must be
// equal-length and non-empty: the generated query's parallel unnests zip
// row-wise and pad a shorter array with nulls, so drift fails the target
// columns' NOT NULL constraints — validating in Go keeps that failure out of
// the database.
//
//nolint:unused // produced by this task for the Waste/Correction handlers (Tasks 4-7); exercised by TestWritePreparationAuditsIsAtomic until then
func writePreparationAudits(ctx context.Context, q *sqlc.Queries, actor Actor,
	occurredAt time.Time, audits []AuditRecord,
) error {
	if len(audits) == 0 {
		return nil
	}
	eventTypes := make([]string, len(audits))
	detailsBatch := make([]string, len(audits))
	for i, audit := range audits {
		details, err := json.Marshal(audit.Details)
		if err != nil {
			return fmt.Errorf("marshal preparation audit details for %q: %w", audit.EventType, err)
		}
		eventTypes[i] = audit.EventType
		detailsBatch[i] = string(details)
	}
	if len(eventTypes) != len(detailsBatch) || len(eventTypes) == 0 {
		return fmt.Errorf(
			"preparation audit batch must be non-empty with equal-length event and details arrays: %d events, %d details",
			len(eventTypes), len(detailsBatch))
	}
	if err := q.InsertPreparationAuditEventsBatch(ctx, sqlc.InsertPreparationAuditEventsBatchParams{
		ActorID:      actor.StaffID,
		SessionID:    actor.SessionID,
		OccurredAt:   occurredAt,
		EventTypes:   eventTypes,
		DetailsBatch: detailsBatch,
	}); err != nil {
		return fmt.Errorf("insert preparation audit batch: %w", err)
	}
	return nil
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
// transaction, so authority removed mid-session takes effect immediately. The
// current roles are returned alongside the derived capabilities for callers
// that need the role names themselves (the self-PIN gate re-locks the roles
// through GetStaffRolesForUpdate instead of reusing these).
func reloadAuthority(ctx context.Context, q *sqlc.Queries, actor Actor) (
	sqlc.GetSalesSessionAuthorityRow, []string, []string, error,
) {
	authRow, err := q.GetSalesSessionAuthority(ctx, sqlc.GetSalesSessionAuthorityParams{
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

	roles, err := q.GetSalesSessionRoles(ctx, authRow.StaffIdentityID)
	if err != nil {
		return authRow, nil, nil, fmt.Errorf("reload session roles: %w", err)
	}

	return authRow, roles, auth.DeriveCapabilities(roles), nil
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

// verifyCurrentManagerPIN re-authenticates the actor as a current Manager
// inside the mutation transaction: it locks the actor's own identity row by id
// (so a concurrent disablement or PIN rotation cannot interleave between
// verification and use), locks the identity's roles, re-checks the enabled
// status, the MANAGER role, and the required capability against those locked
// rows, and bcrypt-verifies the supplied PIN against the identity's current
// hash.
//
// Every expected failure wraps ErrForbidden or ErrInvalidManagerPIN — both
// security denials — so denial evidence is committed and the client receives
// the collapsed NOT_AUTHORIZED response. The attempted PIN is never included
// in any error, audit row, or log.
func verifyCurrentManagerPIN(ctx context.Context, q *sqlc.Queries, actor Actor,
	pin string, requiredCapability string,
) error {
	identity, err := q.GetStaffByIDForUpdate(ctx, actor.StaffID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: staff identity not found", ErrForbidden)
		}
		return fmt.Errorf("lock staff identity: %w", err)
	}
	roles, err := q.GetStaffRolesForUpdate(ctx, actor.StaffID)
	if err != nil {
		return fmt.Errorf("lock staff roles: %w", err)
	}

	// Spend the bcrypt comparison before any rejection decision so a denied
	// attempt costs the same regardless of which condition failed — the same
	// rule VerifyManagerApproval follows. The PIN is consumed here and
	// discarded; it never reaches a wrapped error below.
	pinOK := auth.VerifyPin(identity.PinHash, pin)

	if !identity.Enabled {
		return fmt.Errorf("%w: identity disabled", ErrForbidden)
	}
	if !slices.Contains(roles, auth.RoleManager) {
		return fmt.Errorf("%w: manager role required", ErrForbidden)
	}
	if requiredCapability != "" &&
		!slices.Contains(auth.DeriveCapabilities(roles), requiredCapability) {
		return fmt.Errorf("%w: missing capability %q", ErrForbidden, requiredCapability)
	}
	if !pinOK {
		return fmt.Errorf("%w: pin rejected", ErrInvalidManagerPIN)
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
	return errors.Is(err, ErrUnauthorized) ||
		errors.Is(err, ErrForbidden) ||
		errors.Is(err, ErrInvalidManagerPIN)
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
// optional self re-authentication, idempotency, and audit. On success it
// returns the HTTP status and result.
//
// The order of steps is load-bearing. Authority is reloaded before the
// idempotency replay, so an actor whose session was revoked or whose role was
// removed cannot replay an earlier success. The optional Manager self-PIN gate
// runs after capability verification and before the fingerprint and the
// replay, so a rotated PIN or a lost Manager role denies even an exact replay
// of an earlier success. The open-Sales-Shift precondition deliberately lives
// inside fn, which runs after the claim, so a replay of a request that
// succeeded during a Shift still returns its stored result after that Shift
// closes.
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

	// 1. Reload current authority and roles inside the transaction.
	authRow, _, caps, err := reloadAuthority(ctx, q, actor)
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

	// 3. Verify the current-Manager self-PIN, when the command requires it.
	// The gate runs before the fingerprint and the idempotency replay, so a
	// rotated PIN or a lost Manager role denies even an exact replay of an
	// earlier success. Every expected gate failure is a security denial, so
	// its evidence is committed before the denial is returned.
	if spec.RequireManagerPIN {
		gateCapabilities := spec.Required
		if len(gateCapabilities) == 0 {
			// No capability is required of this command, but the gate still
			// demands an enabled current Manager who knows their own PIN; the
			// gate's capability re-check itself is vacuous.
			gateCapabilities = []string{""}
		}
		for _, capability := range gateCapabilities {
			err := verifyCurrentManagerPIN(ctx, q, actor, spec.ManagerPIN, capability)
			if err == nil {
				continue
			}
			if isSecurityDenial(err) {
				return 0, zero, finishDenial(tx, recordDenial(ctx, q, actor, authRow, spec.Operation, err))
			}
			return 0, zero, err
		}
	}

	// 4. Fingerprint the normalized business input. Credential-free by
	// contract: the Manager PIN, when present, was verified in step 3 and
	// never enters this hash.
	reqHash, err := fpHash(spec.Fingerprint)
	if err != nil {
		return 0, zero, err
	}

	// 5. Advisory lock to serialize concurrent duplicates of this request.
	if err := r.AdvisoryLock(ctx, q, IDToLockKey(actor.StaffID)^IDToLockKey(spec.RequestID)); err != nil {
		return 0, zero, err
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
	resultCode, result, audit, err := fn(MutationContext{Queries: q, tx: tx})
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

// auditReadDenial records a read's authorization denial in a separate short
// write transaction. A denied read runs inside a read-only transaction that
// cannot hold its own audit write, and the denial must reach the client even
// when the evidence write fails, so this is best-effort by construction: any
// failure is logged and the original denial is returned untouched.
func (r *Runner) auditReadDenial(ctx context.Context, actor Actor,
	authority sqlc.GetSalesSessionAuthorityRow, operation string, denialErr error,
) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		slog.Error("begin read-denial audit", "operation", operation, "error", err)
		return
	}
	defer tx.Rollback() //nolint:errcheck
	outcome := recordDenial(ctx, r.queries.WithTx(tx), actor, authority, operation, denialErr)
	if !outcome.committed {
		return
	}
	if err := tx.Commit(); err != nil {
		slog.Error("commit read-denial audit", "operation", operation, "error", err)
	}
}

// ExecuteRead runs a read-only REPEATABLE READ transaction with the same
// in-transaction authority reload and capability verification as
// ExecuteMutation, so a revoked session or a stripped role is denied before
// the body runs. Because the transaction is read-only, a denial's audit event
// is written by auditReadDenial in a separate write transaction, best-effort.
//
// The isolation level gives the body one repeatable snapshot for its whole
// run, which is what makes the queue projection internally consistent.
func ExecuteRead[T any](ctx context.Context, r *Runner, actor Actor,
	operation, requiredCapability string,
	fn func(*sqlc.Queries) (T, error),
) (T, error) {
	var zero T
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{
		ReadOnly:  true,
		Isolation: sql.LevelRepeatableRead,
	})
	if err != nil {
		return zero, fmt.Errorf("begin read transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	q := r.queries.WithTx(tx)
	authority, _, caps, err := reloadAuthority(ctx, q, actor)
	if err != nil {
		if isSecurityDenial(err) {
			_ = tx.Rollback()
			r.auditReadDenial(ctx, actor, authority, operation, err)
		}
		return zero, err
	}
	if err := verifyCapabilities([]string{requiredCapability}, caps); err != nil {
		_ = tx.Rollback()
		r.auditReadDenial(ctx, actor, authority, operation, err)
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
