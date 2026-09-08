package auth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
)

// ComputeRequestHash computes a deterministic SHA256 hex string for an action and payload.
func ComputeRequestHash(action string, payload any) string {
	b, _ := json.Marshal(payload)
	sum := sha256.Sum256(append([]byte(action+":"), b...))
	return hex.EncodeToString(sum[:])
}

// ExecuteWithIdempotency wraps a mutating operation in an idempotency check and stores replayable response.
func ExecuteWithIdempotency[T any](
	ctx context.Context,
	q *sqlc.Queries,
	actorID uuid.UUID,
	key uuid.UUID,
	action string,
	payload any,
	fn func() (int, T, error),
) (int, T, error) {
	var zero T
	reqHash := ComputeRequestHash(action, payload)

	// Check if already executed
	existing, err := q.GetIdempotencyKey(ctx, sqlc.GetIdempotencyKeyParams{
		ActorID: actorID,
		Key:     key,
	})
	if err == nil {
		if existing.RequestHash != reqHash {
			return 0, zero, fmt.Errorf("%w: mã yêu cầu (request_id) đã được dùng cho payload khác", response.ErrConflict)
		}
		var stored T
		if err := json.Unmarshal(existing.ResponseBody, &stored); err != nil {
			return 0, zero, fmt.Errorf("unmarshal cached response: %w", err)
		}
		return int(existing.ResponseCode), stored, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return 0, zero, fmt.Errorf("check idempotency key: %w", err)
	}

	// Execute operation
	code, result, err := fn()
	if err != nil {
		return code, result, err
	}

	// Save result
	resultBytes, _ := json.Marshal(result)
	_ = q.InsertIdempotencyKey(ctx, sqlc.InsertIdempotencyKeyParams{
		Key:          key,
		ActorID:      actorID,
		Action:       action,
		RequestHash:  reqHash,
		//nolint:gosec // G115: HTTP status code (100-599) fits within int32
		ResponseCode: int32(code),
		ResponseBody: json.RawMessage(resultBytes),
	})

	return code, result, nil
}
