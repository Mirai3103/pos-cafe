package command

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/platform/database/sqlc"
)

// ReadSpec carries request-level metadata for an authorized read.
type ReadSpec struct {
	// Operation names the read in its denial audit event. It is only used
	// when the Policy audits read denials.
	Operation string
	Required  []string
}

// ReadContext carries per-execution facts the read body needs.
type ReadContext struct {
	Queries *sqlc.Queries
	Actor   Actor
	// Capabilities are the actor's current capabilities, for projections
	// whose fields depend on them.
	Capabilities []string
}

// ExecuteRead runs a read inside a read-only REPEATABLE READ transaction with
// the same in-transaction authority reload and capability verification as
// ExecuteMutation, so the capability check and every query in fn observe one
// snapshot.
//
// When the Policy audits read denials, the read transaction is ended first
// and the denial is recorded best-effort in a separate write transaction; the
// original denial is returned either way.
func ExecuteRead[T any](ctx context.Context, r *Runner, actor Actor,
	spec ReadSpec,
	fn func(ReadContext) (T, error),
) (T, error) {
	var zero T

	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return zero, fmt.Errorf("begin read transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	q := r.queries.WithTx(tx)
	deny := func(authority Authority, denialErr error) (T, error) {
		if r.policy.AuditReadDenials {
			// Release the read snapshot and its connection before taking a
			// second one for the audit write.
			_ = tx.Rollback()
			r.auditReadDenial(ctx, actor, authority, spec.Operation, denialErr)
		}
		return zero, denialErr
	}

	authority, caps, err := reloadAuthority(ctx, q, actor)
	if err != nil {
		if r.policy.IsSecurityDenial(err) {
			return deny(authority, err)
		}
		return zero, err
	}
	if err := VerifyCapabilities(spec.Required, caps); err != nil {
		return deny(authority, err)
	}

	result, err := fn(ReadContext{Queries: q, Actor: actor, Capabilities: caps})
	if err != nil {
		return zero, err
	}
	if err := tx.Commit(); err != nil {
		return zero, fmt.Errorf("commit read transaction: %w", err)
	}
	return result, nil
}
