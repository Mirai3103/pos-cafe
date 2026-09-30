package command

import "errors"

// Policy declares how one slice's pipeline differs from another's. A slice
// builds one Policy and binds it to its Runner with NewRunner.
type Policy struct {
	// DenialEventType is the audit event type written for an authorization
	// denial, e.g. "tables.authorization_denied". Each slice names the same
	// condition under its own type so the audit stream keeps slices
	// distinguishable. Required.
	DenialEventType string

	// LogScope names the slice in the warning logged for every recorded
	// denial ("<LogScope> authorization denied").
	LogScope string

	// Attribution decides when a denial's audit row names the actor.
	Attribution Attribution

	// OnDenialAuditFailure decides what the caller sees when the denial's
	// own audit insert fails.
	OnDenialAuditFailure DenialAuditFailure

	// AuditReadDenials records read-path denials in a short separate
	// read-write transaction, since the read's own transaction is read-only.
	// The read always returns the original denial, whether or not that
	// best-effort audit succeeded.
	AuditReadDenials bool

	// ApprovalDenied is the sentinel a rejected second-party Manager approval
	// is reported as (wrapped together with the approval's reason). Nil means
	// ErrForbidden.
	ApprovalDenied error

	// SecurityDenials lists slice sentinels, beyond ErrUnauthorized and
	// ErrForbidden, that count as security denials when a Gate returns them,
	// so their evidence is recorded and committed. A wrong self-PIN is the
	// typical member.
	SecurityDenials []error

	// Idempotency stores claimed requests and their replayable results. Nil
	// means IdempotencyKeys.
	Idempotency IdempotencyStore
}

// Attribution selects when a denial audit row names the actor.
type Attribution int

const (
	// AttributeActorAlways names actor.StaffID on every denial. Staff
	// identities are never deleted and the actor comes from a session the
	// HTTP middleware validated moments earlier, so the identity is never in
	// doubt even when this transaction's own reload denies the request.
	AttributeActorAlways Attribution = iota
	// AttributeActorIfConfirmed names the actor only when the reloaded
	// authority row confirmed that identity.
	AttributeActorIfConfirmed
)

// DenialAuditFailure selects the outcome when a denial audit insert fails.
type DenialAuditFailure int

const (
	// ReturnDenial logs the failed insert and still returns the original
	// denial, so a broken audit write can never replace a 401/403 with an
	// unmapped error.
	ReturnDenial DenialAuditFailure = iota
	// ReturnAuditError returns the wrapped insert error instead of the
	// denial, surfacing the lost evidence as a server error.
	ReturnAuditError
)

// IsSecurityDenial reports whether err is a denial whose evidence the
// pipeline records: ErrUnauthorized, ErrForbidden, or one of the policy's
// SecurityDenials.
func (p Policy) IsSecurityDenial(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrUnauthorized) || errors.Is(err, ErrForbidden) {
		return true
	}
	for _, target := range p.SecurityDenials {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}

func (p Policy) approvalDenied() error {
	if p.ApprovalDenied != nil {
		return p.ApprovalDenied
	}
	return ErrForbidden
}
