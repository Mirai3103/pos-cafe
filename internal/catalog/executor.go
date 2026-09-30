package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/platform/command"
	"github.com/Mirai3103/pos-cafe/internal/platform/database/sqlc"
	"github.com/google/uuid"
)

// Actor identifies the authenticated staff member executing a command.
type Actor = command.Actor

// AuditRecord describes the audit event to insert after a successful
// mutation. A zero EventType means no business event is written, which is
// how a same-state no-op reports itself.
type AuditRecord = command.AuditRecord

// Runner executes Catalog commands and reads under the Catalog policy.
type Runner = command.Runner

// MutationSpec carries request-level metadata for a Catalog mutation.
//
// RequireManagerPIN marks a price-sensitive command: the initiator must also
// hold catalog.change_price and re-enter their own Manager PIN, checked on
// every attempt including an exact replay. ManagerPIN is never part of the
// Fingerprint, so the idempotency key stays independent of the credential.
type MutationSpec struct {
	RequestID         uuid.UUID
	Operation         string
	Fingerprint       any
	Required          []string
	ManagerPIN        string
	RequireManagerPIN bool
}

// policy is how the Catalog pipeline audits denials: the actor is attributed
// only when the reloaded authority confirmed it, a denial whose own audit
// insert fails surfaces as that failure, read denials are not audited, and a
// wrong Manager PIN is recorded as a security denial. Requests are kept in
// catalog_mutation_requests rather than the shared idempotency_keys table.
var policy = command.Policy{
	DenialEventType:      EventAuthorizationDenied,
	LogScope:             "catalog",
	Attribution:          command.AttributeActorIfConfirmed,
	OnDenialAuditFailure: command.ReturnAuditError,
	SecurityDenials:      []error{ErrInvalidManagerPin},
	Idempotency:          catalogRequests{},
}

// NewRunner creates a Runner bound to the Catalog policy.
func NewRunner(db *sql.DB, queries *sqlc.Queries) *Runner {
	return command.NewRunner(db, queries, policy)
}

// ExecuteMutation runs a Catalog mutation through the shared command
// pipeline. On success it returns the HTTP status and result.
func ExecuteMutation[T any](ctx context.Context, r *Runner, actor Actor,
	spec MutationSpec,
	fn func(*sqlc.Queries) (int, T, AuditRecord, error),
) (int, T, error) {
	return command.ExecuteMutation(ctx, r, actor, spec.command(),
		func(mc command.MutationContext) (int, T, AuditRecord, error) {
			return fn(mc.Queries)
		})
}

// ExecuteRead runs a Catalog read in a read-only repeatable-read transaction
// after verifying requiredCapability.
func ExecuteRead[T any](ctx context.Context, r *Runner, actor Actor,
	requiredCapability string,
	fn func(*sqlc.Queries) (T, error),
) (T, error) {
	return ExecuteReadWithCapabilities(ctx, r, actor, requiredCapability, func(q *sqlc.Queries, _ []string) (T, error) {
		return fn(q)
	})
}

// ExecuteReadWithCapabilities is ExecuteRead for projections whose fields
// depend on the caller's capabilities (ADR-061).
func ExecuteReadWithCapabilities[T any](ctx context.Context, r *Runner, actor Actor,
	requiredCapability string,
	fn func(q *sqlc.Queries, caps []string) (T, error),
) (T, error) {
	return command.ExecuteRead(ctx, r, actor, command.ReadSpec{Required: []string{requiredCapability}},
		func(rc command.ReadContext) (T, error) {
			return fn(rc.Queries, rc.Capabilities)
		})
}

func (s MutationSpec) command() command.MutationSpec {
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

// managerPINGate guards a price-sensitive command. A missing
// catalog.change_price capability and a wrong PIN are recorded denials; a
// failure to load the staff row aborts the transaction unrecorded.
func managerPINGate(pin string) command.Gate {
	return func(ctx context.Context, gc command.GateContext) error {
		if err := command.VerifyCapabilities([]string{CapChangePrice}, gc.Capabilities); err != nil {
			return err
		}
		return verifyManagerPIN(ctx, gc.Queries, gc.Authority.StaffIdentityID, pin)
	}
}

// verifyManagerPIN checks the actor's freshly entered Manager PIN.
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

// catalogRequests is the command.IdempotencyStore backed by the
// catalog_mutation_requests table.
type catalogRequests struct{}

var (
	_ command.IdempotencyStore  = catalogRequests{}
	_ command.ConflictDescriber = catalogRequests{}
)

// Find implements command.IdempotencyStore.
func (catalogRequests) Find(ctx context.Context, q *sqlc.Queries, actorID, requestID uuid.UUID) (command.StoredRequest, bool, error) {
	row, err := q.GetCatalogMutationRequest(ctx, sqlc.GetCatalogMutationRequestParams{
		ActorID:   actorID,
		RequestID: requestID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return command.StoredRequest{}, false, nil
	}
	if err != nil {
		return command.StoredRequest{}, false, err
	}
	return storedCatalogRequest(row), true, nil
}

// Claim implements command.IdempotencyStore.
func (catalogRequests) Claim(ctx context.Context, q *sqlc.Queries, actorID, requestID uuid.UUID,
	operation, requestHash string,
) (command.StoredRequest, error) {
	row, err := q.ClaimCatalogRequest(ctx, sqlc.ClaimCatalogRequestParams{
		ActorID:      actorID,
		RequestID:    requestID,
		Operation:    operation,
		RequestHash:  requestHash,
		ResponseCode: 0,
		ResponseBody: json.RawMessage("null"),
	})
	if err != nil {
		return command.StoredRequest{}, err
	}
	return storedCatalogRequest(row), nil
}

// Complete implements command.IdempotencyStore.
func (catalogRequests) Complete(ctx context.Context, q *sqlc.Queries, actorID, requestID uuid.UUID,
	responseCode int, responseBody []byte,
) error {
	return q.StoreCatalogRequestResult(ctx, sqlc.StoreCatalogRequestResultParams{
		ActorID:      actorID,
		RequestID:    requestID,
		ResponseCode: int32(responseCode), //nolint:gosec // G115: HTTP status (100-599) fits int32
		ResponseBody: responseBody,
	})
}

// DescribeConflict implements command.ConflictDescriber.
func (catalogRequests) DescribeConflict(stored command.StoredRequest, _, requestHash string) string {
	return fmt.Sprintf("operation %q hash mismatch (stored: %s, current: %s)",
		stored.Operation, stored.RequestHash, requestHash)
}

func storedCatalogRequest(row sqlc.CatalogMutationRequest) command.StoredRequest {
	return command.StoredRequest{
		Operation:    row.Operation,
		RequestHash:  row.RequestHash,
		ResponseCode: int(row.ResponseCode),
		ResponseBody: row.ResponseBody,
	}
}
