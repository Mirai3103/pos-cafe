package sales

import (
	"context"
	"database/sql"

	"github.com/Mirai3103/pos-cafe/internal/platform/command"
	"github.com/Mirai3103/pos-cafe/internal/platform/database/sqlc"
	"github.com/google/uuid"
)

// Actor identifies the authenticated staff member executing an operation.
type Actor = command.Actor

// ApprovalSpec requires a second identity holding the Manager role to
// authenticate inline before the request is claimed. Comp, Refund, and Payment
// Void carry one; the approver must also hold the named capability.
type ApprovalSpec = command.Approval

// MutationSpec carries request-level metadata for a mutation command. Its
// Fingerprint must never contain a PIN or an approver login code.
type MutationSpec = command.MutationSpec

// MutationContext carries per-execution facts the mutation body needs.
// Approver is nil when the spec required no approval.
type MutationContext = command.MutationContext

// AuditRecord describes the audit event to insert after a successful mutation.
// A zero EventType writes no business event.
type AuditRecord = command.AuditRecord

// Runner executes Sales commands and reads under the Sales policy.
type Runner = command.Runner

// policy is how the Sales pipeline audits denials. The actor is always
// attributed; a denial whose own audit insert fails is logged and the
// original denial still returned, so a broken audit write never turns a
// 401/403 into a 500; read denials are recorded in a separate short
// transaction; and a rejected Manager approval is reported as ErrForbidden.
var policy = command.Policy{
	DenialEventType:      EventAuthorizationDenied,
	LogScope:             "sales",
	Attribution:          command.AttributeActorAlways,
	OnDenialAuditFailure: command.ReturnDenial,
	AuditReadDenials:     true,
	ApprovalDenied:       ErrForbidden,
}

// NewRunner creates a Runner bound to the Sales policy.
func NewRunner(db *sql.DB, queries *sqlc.Queries) *Runner {
	return command.NewRunner(db, queries, policy)
}

// IDToLockKey converts a UUID to a stable int64 for advisory locking.
func IDToLockKey(id uuid.UUID) int64 {
	return command.IDToLockKey(id)
}

// ExecuteMutation runs a Sales mutation through the shared command pipeline.
// On success it returns the HTTP status and result.
//
// The open-Sales-Shift precondition deliberately lives inside fn, which runs
// after the idempotency claim, so a replay of a request that succeeded during
// a Shift still returns its stored result after that Shift closes.
func ExecuteMutation[T any](ctx context.Context, r *Runner, actor Actor,
	spec MutationSpec,
	fn func(MutationContext) (int, T, AuditRecord, error),
) (int, T, error) {
	return command.ExecuteMutation(ctx, r, actor, spec, fn)
}

// ExecuteRead runs a Sales read in a read-only repeatable-read transaction
// after verifying requiredCapability, so the capability check and every query
// observe one snapshot. operation names the read in a denial's audit event.
func ExecuteRead[T any](ctx context.Context, r *Runner, actor Actor,
	operation string, requiredCapability string,
	fn func(*sqlc.Queries) (T, error),
) (T, error) {
	spec := command.ReadSpec{Operation: operation, Required: []string{requiredCapability}}
	return command.ExecuteRead(ctx, r, actor, spec,
		func(rc command.ReadContext) (T, error) {
			return fn(rc.Queries)
		})
}
