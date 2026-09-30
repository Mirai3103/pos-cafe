package preparation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/platform/command"
	"github.com/Mirai3103/pos-cafe/internal/platform/database/sqlc"
	"github.com/google/uuid"
)

// EventAuthorizationDenied is the audit event type for an authorization
// denial raised by this package's pipeline. Each slice names the same
// condition under its own event type so the audit stream keeps the vertical
// slices distinguishable.
const EventAuthorizationDenied = "preparation.authorization_denied"

// Actor identifies the authenticated staff member executing an operation.
type Actor = command.Actor

// AuditRecord describes the audit event to insert after a successful mutation.
// A zero EventType writes no business event.
type AuditRecord = command.AuditRecord

// MutationContext carries per-execution facts the mutation body needs.
type MutationContext = command.MutationContext

// Runner executes Preparation commands and reads under the Preparation policy.
type Runner = command.Runner

// policy is how the Preparation pipeline audits denials: the actor is always
// attributed, a denial whose own audit insert fails is logged and still
// returned as the denial, read denials are audited in a separate write
// transaction, and a wrong Manager self-PIN is recorded as a security denial.
var policy = command.Policy{
	DenialEventType:      EventAuthorizationDenied,
	LogScope:             "preparation",
	Attribution:          command.AttributeActorAlways,
	OnDenialAuditFailure: command.ReturnDenial,
	AuditReadDenials:     true,
	SecurityDenials:      []error{ErrInvalidManagerPIN},
}

// NewRunner creates a Runner bound to the Preparation policy.
func NewRunner(db *sql.DB, queries *sqlc.Queries) *Runner {
	return command.NewRunner(db, queries, policy)
}

// IDToLockKey converts a UUID to a stable int64 for advisory locking.
func IDToLockKey(id uuid.UUID) int64 {
	return command.IDToLockKey(id)
}

// MutationSpec carries request-level metadata for a mutation command.
//
// Fingerprint stays credential-free: it is the normalized business input and
// must never carry the Manager PIN or any other secret. When RequireManagerPIN
// is set, ManagerPIN carries the actor's own current PIN; the pipeline
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

// commandSpec translates the spec into the shared pipeline's form, turning
// the self-PIN requirement into a Gate.
func (s MutationSpec) commandSpec() command.MutationSpec {
	spec := command.MutationSpec{
		RequestID:   s.RequestID,
		Operation:   s.Operation,
		Fingerprint: s.Fingerprint,
		Required:    s.Required,
	}
	if s.RequireManagerPIN {
		spec.Gate = managerPINGate(s.ManagerPIN)
	}
	return spec
}

// ExecuteMutation runs a Preparation mutation through the shared command
// pipeline. On success it returns the HTTP status and result.
//
// The open-Sales-Shift precondition deliberately lives inside fn, which runs
// after the claim, so a replay of a request that succeeded during a Shift
// still returns its stored result after that Shift closes.
func ExecuteMutation[T any](ctx context.Context, r *Runner, actor Actor,
	spec MutationSpec,
	fn func(MutationContext) (int, T, AuditRecord, error),
) (int, T, error) {
	return command.ExecuteMutation(ctx, r, actor, spec.commandSpec(), fn)
}

// ExecuteRead runs a Preparation read in a read-only REPEATABLE READ
// transaction after verifying requiredCapability. The isolation level gives
// the body one repeatable snapshot for its whole run, which is what makes the
// queue projection internally consistent.
func ExecuteRead[T any](ctx context.Context, r *Runner, actor Actor,
	operation, requiredCapability string,
	fn func(*sqlc.Queries) (T, error),
) (T, error) {
	return command.ExecuteRead(ctx, r, actor,
		command.ReadSpec{Operation: operation, Required: []string{requiredCapability}},
		func(rc command.ReadContext) (T, error) {
			return fn(rc.Queries)
		})
}

// managerPINGate re-authenticates the initiator as a current Manager with
// their own PIN, once per required capability. The pipeline runs it after
// capability verification and before the fingerprint and the replay, so a
// rotated PIN or a lost Manager role denies even an exact replay of an
// earlier success.
func managerPINGate(pin string) command.Gate {
	return func(ctx context.Context, gc command.GateContext) error {
		capabilities := gc.Required
		if len(capabilities) == 0 {
			// No capability is required of this command, but the gate still
			// demands an enabled current Manager who knows their own PIN; the
			// gate's capability re-check itself is vacuous.
			capabilities = []string{""}
		}
		for _, capability := range capabilities {
			if err := verifyCurrentManagerPIN(ctx, gc.Queries, gc.Actor, pin, capability); err != nil {
				return err
			}
		}
		return nil
	}
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

// withUnitSavepoint runs fn inside a fixed, package-private savepoint so one
// unit's failure can be undone without aborting the surrounding bulk
// transaction: fn's error rolls the savepoint back and is returned for the
// caller to convert (or not) into a per-unit outcome, while a savepoint
// statement's own failure aborts the mutation outright. The name is a
// constant and carries no request data.
func withUnitSavepoint(ctx context.Context, mc MutationContext,
	fn func(*sqlc.Queries) error,
) error {
	if _, err := mc.Tx.ExecContext(ctx, "SAVEPOINT preparation_unit"); err != nil {
		return fmt.Errorf("create preparation unit savepoint: %w", err)
	}
	if err := fn(mc.Queries); err != nil {
		if _, rollbackErr := mc.Tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT preparation_unit"); rollbackErr != nil {
			return fmt.Errorf("rollback preparation unit savepoint after callback failure: %w", rollbackErr)
		}
		if _, releaseErr := mc.Tx.ExecContext(ctx, "RELEASE SAVEPOINT preparation_unit"); releaseErr != nil {
			return fmt.Errorf("release rolled-back preparation unit savepoint: %w", releaseErr)
		}
		return err
	}
	if _, err := mc.Tx.ExecContext(ctx, "RELEASE SAVEPOINT preparation_unit"); err != nil {
		return fmt.Errorf("release preparation unit savepoint: %w", err)
	}
	return nil
}

// writePreparationAudits batches several audit events — possibly of differing
// types — into one insert through InsertPreparationAuditEventsBatch, with one
// actor, one session, and one shared occurrence time. Commands that emit more
// than one business event for a single mutation (Waste writes its fact and its
// alert event; State Correction writes one event per corrected unit) call it
// instead of the pipeline's single-AuditRecord step, which stays unchanged for
// single-event commands.
//
// The caller chooses occurredAt so every event of one mutation shares one
// moment. Empty input is a no-op. The event and details arrays must be
// equal-length and non-empty: the generated query's parallel unnests zip
// row-wise and pad a shorter array with nulls, so drift fails the target
// columns' NOT NULL constraints — validating in Go keeps that failure out of
// the database.
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
