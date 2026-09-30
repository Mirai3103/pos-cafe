package command

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

// StoredRequest is a claimed request as its IdempotencyStore holds it. A
// claim that has not completed yet carries ResponseCode 0 and a JSON null
// body.
type StoredRequest struct {
	Operation    string
	RequestHash  string
	ResponseCode int
	ResponseBody []byte
}

// IdempotencyStore persists claimed requests and their replayable results,
// keyed by (actor, request id). Every method runs on the mutation's
// transaction-scoped queries, after the request's advisory lock is held.
type IdempotencyStore interface {
	// Find returns the stored request; found is false when there is none.
	Find(ctx context.Context, q *sqlc.Queries, actorID, requestID uuid.UUID) (rec StoredRequest, found bool, err error)
	// Claim inserts a pending record for the request, or returns the record a
	// concurrent transaction committed first.
	Claim(ctx context.Context, q *sqlc.Queries, actorID, requestID uuid.UUID,
		operation, requestHash string) (StoredRequest, error)
	// Complete stores the replayable result on the claimed record.
	Complete(ctx context.Context, q *sqlc.Queries, actorID, requestID uuid.UUID,
		responseCode int, responseBody []byte) error
}

// ConflictDescriber is implemented by an IdempotencyStore whose conflict
// error must say more than the default "request_id reused for a different
// change". The returned text follows "request conflict: " in the error.
type ConflictDescriber interface {
	DescribeConflict(stored StoredRequest, operation, requestHash string) string
}

// IdempotencyKeys is the IdempotencyStore backed by the shared
// idempotency_keys table (ADR-005).
type IdempotencyKeys struct{}

var _ IdempotencyStore = IdempotencyKeys{}

// Find implements IdempotencyStore.
func (IdempotencyKeys) Find(ctx context.Context, q *sqlc.Queries, actorID, requestID uuid.UUID) (StoredRequest, bool, error) {
	row, err := q.GetIdempotencyRecord(ctx, sqlc.GetIdempotencyRecordParams{
		ActorID: actorID,
		Key:     requestID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return StoredRequest{}, false, nil
	}
	if err != nil {
		return StoredRequest{}, false, err
	}
	return storedFromKey(row), true, nil
}

// Claim implements IdempotencyStore.
func (IdempotencyKeys) Claim(ctx context.Context, q *sqlc.Queries, actorID, requestID uuid.UUID,
	operation, requestHash string,
) (StoredRequest, error) {
	row, err := q.ClaimIdempotencyRecord(ctx, sqlc.ClaimIdempotencyRecordParams{
		Key:          requestID,
		ActorID:      actorID,
		Action:       operation,
		RequestHash:  requestHash,
		ResponseCode: 0,
		ResponseBody: json.RawMessage("null"),
	})
	if err != nil {
		return StoredRequest{}, err
	}
	return storedFromKey(row), nil
}

// Complete implements IdempotencyStore.
func (IdempotencyKeys) Complete(ctx context.Context, q *sqlc.Queries, actorID, requestID uuid.UUID,
	responseCode int, responseBody []byte,
) error {
	return q.StoreIdempotencyResult(ctx, sqlc.StoreIdempotencyResultParams{
		ActorID:      actorID,
		Key:          requestID,
		ResponseCode: int32(responseCode), //nolint:gosec // G115: HTTP status (100-599) fits int32
		ResponseBody: responseBody,
	})
}

func storedFromKey(row sqlc.IdempotencyKey) StoredRequest {
	return StoredRequest{
		Operation:    row.Action,
		RequestHash:  row.RequestHash,
		ResponseCode: int(row.ResponseCode),
		ResponseBody: row.ResponseBody,
	}
}

func (r *Runner) conflictDetail(stored StoredRequest, operation, requestHash string) string {
	if d, ok := r.policy.Idempotency.(ConflictDescriber); ok {
		return d.DescribeConflict(stored, operation, requestHash)
	}
	return "request_id reused for a different change"
}
