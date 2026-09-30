package tables

import (
	"context"
	"database/sql"

	"github.com/Mirai3103/pos-cafe/internal/platform/command"
	"github.com/Mirai3103/pos-cafe/internal/platform/database/sqlc"
)

// Actor identifies the authenticated staff member executing an operation.
type Actor = command.Actor

// MutationSpec carries request-level metadata for a mutation command. Tables
// has no price-sensitive or approval-gated command, so its specs never set a
// Gate or an Approval.
type MutationSpec = command.MutationSpec

// AuditRecord describes the audit event to insert after a successful
// mutation. A zero EventType means no business event is written, which is
// how a same-state Availability no-op reports itself.
type AuditRecord = command.AuditRecord

// Runner executes Tables commands and reads under the Tables policy.
type Runner = command.Runner

// policy is how the Tables pipeline audits denials: the actor is attributed
// only when the reloaded authority confirmed it, a denial whose own audit
// insert fails surfaces as that failure, and read denials are not audited.
var policy = command.Policy{
	DenialEventType:      EventAuthorizationDenied,
	LogScope:             "tables",
	Attribution:          command.AttributeActorIfConfirmed,
	OnDenialAuditFailure: command.ReturnAuditError,
}

// NewRunner creates a Runner bound to the Tables policy.
func NewRunner(db *sql.DB, queries *sqlc.Queries) *Runner {
	return command.NewRunner(db, queries, policy)
}

// ExecuteMutation runs a Tables mutation through the shared command pipeline.
// On success it returns the HTTP status and result.
func ExecuteMutation[T any](ctx context.Context, r *Runner, actor Actor,
	spec MutationSpec,
	fn func(*sqlc.Queries) (int, T, AuditRecord, error),
) (int, T, error) {
	return command.ExecuteMutation(ctx, r, actor, spec,
		func(mc command.MutationContext) (int, T, AuditRecord, error) {
			return fn(mc.Queries)
		})
}

// ExecuteRead runs a Tables read in a read-only repeatable-read transaction
// after verifying requiredCapability.
func ExecuteRead[T any](ctx context.Context, r *Runner, actor Actor,
	requiredCapability string,
	fn func(*sqlc.Queries) (T, error),
) (T, error) {
	return command.ExecuteRead(ctx, r, actor, command.ReadSpec{Required: []string{requiredCapability}},
		func(rc command.ReadContext) (T, error) {
			return fn(rc.Queries)
		})
}
