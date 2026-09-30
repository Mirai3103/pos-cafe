package command

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/platform/database/sqlc"
	"github.com/google/uuid"
)

// MutationSpec carries request-level metadata for a mutation command.
//
// Fingerprint is the normalized business input the idempotency record is
// keyed on. It must never carry a PIN, an approver login code, or any other
// credential: including one would make the key sensitive to it and would
// store a credential-derived value at rest.
type MutationSpec struct {
	RequestID   uuid.UUID
	Operation   string
	Fingerprint any
	Required    []string

	// Gate is an optional extra check on the initiator, such as a self
	// Manager-PIN re-authentication. Nil means none.
	Gate Gate
	// Approval requires a second identity holding the Manager role to
	// authenticate inline. Nil means no second-party approval.
	Approval *Approval
}

// Approval requires a second identity holding the Manager role to
// authenticate inline before the request is claimed.
type Approval struct {
	ApproverLoginCode string
	ManagerPIN        string
	// RequiredCapability is the capability the approver must also hold, so a
	// Manager cannot approve an operation outside their own authority.
	RequiredCapability string
}

// Gate is an extra authorization step run inside the mutation transaction,
// after capability verification and before the fingerprint, the replay
// lookup, and the claim, so a failing gate denies even an exact replay of an
// earlier success.
//
// An error the Policy classifies as a security denial (see
// Policy.IsSecurityDenial) is recorded and committed as denial evidence
// before it is returned; any other error aborts the transaction unchanged.
// A gate must never put a credential into its error.
type Gate func(ctx context.Context, gc GateContext) error

// GateContext is what a Gate may inspect.
type GateContext struct {
	Queries      *sqlc.Queries
	Actor        Actor
	Authority    Authority
	Capabilities []string
	// Required is the spec's required capabilities.
	Required []string
}

// MutationContext carries per-execution facts the mutation body needs.
type MutationContext struct {
	Queries *sqlc.Queries
	// Tx is the mutation transaction, for bodies that need savepoints or
	// statements outside the generated queries. The body must not commit or
	// roll it back.
	Tx    *sql.Tx
	Actor Actor
	// Approver is nil when the spec required no approval.
	Approver *auth.ApproverSummary
}

// AuditRecord describes the business audit event written after a successful
// mutation body. A zero EventType writes none, which is how a same-state
// no-op reports itself.
type AuditRecord struct {
	EventType string
	Details   any
}

// ExecuteMutation runs a mutation inside one transaction with authorization,
// optional gate and second-party approval, idempotency, and audit. On success
// it returns the HTTP status and result; an exact replay returns the stored
// ones without running fn.
//
// Business preconditions that may stop holding over time (an open Sales
// Shift, say) belong inside fn, which runs after the claim, so a replay of a
// request that succeeded earlier still returns its stored result.
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
	deny := func(authority Authority, denialErr error) (int, T, error) {
		return 0, zero, r.deny(ctx, tx, q, actor, authority, spec.Operation, denialErr)
	}

	// 1. Reload current authority inside the transaction.
	authority, caps, err := reloadAuthority(ctx, q, actor)
	if err != nil {
		if r.policy.IsSecurityDenial(err) {
			return deny(authority, err)
		}
		return 0, zero, err
	}

	// 2. Verify the initiator's capabilities.
	if err := VerifyCapabilities(spec.Required, caps); err != nil {
		return deny(authority, err)
	}

	// 3. Run the initiator's own gate, when the spec has one.
	if spec.Gate != nil {
		err := spec.Gate(ctx, GateContext{
			Queries:      q,
			Actor:        actor,
			Authority:    authority,
			Capabilities: caps,
			Required:     spec.Required,
		})
		if err != nil {
			if r.policy.IsSecurityDenial(err) {
				return deny(authority, err)
			}
			return 0, zero, err
		}
	}

	// 4. Verify second-party Manager approval. This runs before the claim, so
	// a replay cannot succeed using an approver who has since been disabled
	// or demoted. The approver row stays locked for the rest of the
	// transaction.
	var approver *auth.ApproverSummary
	if spec.Approval != nil {
		summary, err := auth.VerifyManagerApproval(ctx, q,
			spec.Approval.ApproverLoginCode,
			spec.Approval.ManagerPIN,
			spec.Approval.RequiredCapability)
		if err != nil {
			if errors.Is(err, auth.ErrManagerApprovalDenied) {
				return deny(authority, fmt.Errorf("%w: %s", r.policy.approvalDenied(), err.Error()))
			}
			return 0, zero, err
		}
		approver = &summary
	}

	// 5. Fingerprint the normalized business input.
	reqHash, err := FingerprintHash(spec.Fingerprint)
	if err != nil {
		return 0, zero, err
	}

	// 6. Serialize concurrent duplicates of this request.
	if err := r.AdvisoryLock(ctx, q, RequestLockKey(actor.StaffID, spec.RequestID)); err != nil {
		return 0, zero, err
	}

	// 7. Replay or reject an existing record, now that the lock is held.
	store := r.policy.Idempotency
	existing, found, err := store.Find(ctx, q, actor.StaffID, spec.RequestID)
	if err != nil {
		return 0, zero, fmt.Errorf("lookup idempotency: %w", err)
	}
	if found {
		if existing.Operation != spec.Operation || existing.RequestHash != reqHash {
			return 0, zero, fmt.Errorf("%w: %s", ErrRequestConflict,
				r.conflictDetail(existing, spec.Operation, reqHash))
		}
		var result T
		if err := json.Unmarshal(existing.ResponseBody, &result); err != nil {
			return 0, zero, fmt.Errorf("%w: stored response body is corrupted", ErrInvalidStoredResult)
		}
		return existing.ResponseCode, result, nil
	}

	// 8. Claim the request before any business mutation.
	claimed, err := store.Claim(ctx, q, actor.StaffID, spec.RequestID, spec.Operation, reqHash)
	if err != nil {
		return 0, zero, fmt.Errorf("claim idempotency request: %w", err)
	}
	if claimed.Operation != spec.Operation || claimed.RequestHash != reqHash {
		return 0, zero, fmt.Errorf("%w: request was claimed concurrently", ErrRequestConflict)
	}

	// 9. Run the mutation.
	resultCode, result, audit, err := fn(MutationContext{Queries: q, Tx: tx, Actor: actor, Approver: approver})
	if err != nil {
		return 0, zero, err
	}

	// 10. Write the business audit event, when there is one.
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

	// 11. Store the replayable result.
	bodyBytes, err := json.Marshal(result)
	if err != nil {
		return 0, zero, fmt.Errorf("marshal response body: %w", err)
	}
	if err := store.Complete(ctx, q, actor.StaffID, spec.RequestID, resultCode, bodyBytes); err != nil {
		return 0, zero, fmt.Errorf("store idempotent result: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, zero, fmt.Errorf("commit transaction: %w", err)
	}
	return resultCode, result, nil
}
