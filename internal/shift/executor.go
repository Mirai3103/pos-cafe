package shift

import (
	"context"
	"database/sql"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/platform/command"
)

// Actor identifies the authenticated staff member executing an operation.
type Actor = command.Actor

// MutationSpec carries request-level metadata for a mutation command. Its
// Fingerprint must never contain a PIN: a secret would make the idempotency
// key sensitive to it and would store a PIN-derived value at rest.
type MutationSpec = command.MutationSpec

// ApprovalSpec requires a second identity holding the Manager role to
// authenticate inline before the request is claimed.
type ApprovalSpec = command.Approval

// MutationContext carries per-execution facts the mutation body needs. Its
// Approver is nil when the spec required no approval.
type MutationContext = command.MutationContext

// AuditRecord describes the audit event to insert after a successful mutation.
// A zero EventType writes no business event.
type AuditRecord = command.AuditRecord

// Runner executes Shift commands and reads under the Shift policy.
type Runner = command.Runner

// policy is how the Shift pipeline audits denials.
//
// The actor is always attributed: staff identities are never deleted and the
// actor comes from a session the HTTP middleware validated moments earlier,
// so the identity is never in doubt even when the transaction's own reload
// denies the request. A denial whose own audit insert fails is logged and the
// original denial is still returned, so a broken audit write can never
// replace a 401/403 with an unmapped error. Read denials are audited in a
// short separate transaction, since the read's own is read-only. Every failed
// Manager approval collapses to ErrManagerApprovalUnavailable.
var policy = command.Policy{
	DenialEventType:      EventAuthorizationDenied,
	LogScope:             "shift",
	Attribution:          command.AttributeActorAlways,
	OnDenialAuditFailure: command.ReturnDenial,
	AuditReadDenials:     true,
	ApprovalDenied:       ErrManagerApprovalUnavailable,
}

// NewRunner creates a Runner bound to the Shift policy.
func NewRunner(db *sql.DB, queries *sqlc.Queries) *Runner {
	return command.NewRunner(db, queries, policy)
}

// ExecuteMutation runs a Shift mutation through the shared command pipeline:
// one transaction with authorization, optional second-party approval,
// idempotency, and audit. On success it returns the HTTP status and result.
func ExecuteMutation[T any](ctx context.Context, r *Runner, actor Actor,
	spec MutationSpec,
	fn func(MutationContext) (int, T, AuditRecord, error),
) (int, T, error) {
	return command.ExecuteMutation(ctx, r, actor, spec, fn)
}

// ExecuteRead runs a Shift read in a read-only repeatable-read transaction
// after verifying requiredCapability, so the capability check and every query
// observe one snapshot. A denial is audited under operation.
func ExecuteRead[T any](ctx context.Context, r *Runner, actor Actor,
	operation string, requiredCapability string,
	fn func(*sqlc.Queries) (T, error),
) (T, error) {
	return command.ExecuteRead(ctx, r, actor,
		command.ReadSpec{Operation: operation, Required: []string{requiredCapability}},
		func(rc command.ReadContext) (T, error) {
			return fn(rc.Queries)
		})
}
