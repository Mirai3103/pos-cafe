package auth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
)

type idempotencyPayload struct {
	TargetID uuid.UUID `json:"target_id"`
	Body     any       `json:"body"`
}

// ComputeRequestHash computes a deterministic SHA256 hex string for an action and payload.
func ComputeRequestHash(action string, payload any) string {
	b, _ := json.Marshal(payload)
	sum := sha256.Sum256(append([]byte(action+":"), b...))
	return hex.EncodeToString(sum[:])
}

// ComputeRequestHashWithTarget includes a target resource in an idempotency fingerprint.
func ComputeRequestHashWithTarget(action string, targetID uuid.UUID, payload any) string {
	return ComputeRequestHash(action, idempotencyPayload{TargetID: targetID, Body: payload})
}

// ExecuteWithIdempotency wraps a mutating operation in an idempotency check and stores replayable response.
func ExecuteWithIdempotency[T any](
	ctx context.Context,
	db *sql.DB,
	q *sqlc.Queries,
	actorID uuid.UUID,
	key uuid.UUID,
	action string,
	payload any,
	fn func(tx *sql.Tx, qtx *sqlc.Queries) (int, T, error),
) (int, T, error) {
	var zero T
	reqHash := ComputeRequestHash(action, payload)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, zero, fmt.Errorf("begin idempotency tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	qtx := q.WithTx(tx)
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", idempotencyLockID(actorID, key)); err != nil {
		return 0, zero, fmt.Errorf("acquire idempotency lock: %w", err)
	}

	existing, err := qtx.GetIdempotencyKey(ctx, sqlc.GetIdempotencyKeyParams{
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

	code, result, err := fn(tx, qtx)
	if err != nil {
		return code, result, err
	}

	resultBytes, _ := json.Marshal(result)
	if err := qtx.InsertIdempotencyKey(ctx, sqlc.InsertIdempotencyKeyParams{
		Key:         key,
		ActorID:     actorID,
		Action:      action,
		RequestHash: reqHash,
		//nolint:gosec // G115: HTTP status code (100-599) fits within int32
		ResponseCode: int32(code),
		ResponseBody: json.RawMessage(resultBytes),
	}); err != nil {
		return 0, zero, fmt.Errorf("save idempotency key: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, zero, fmt.Errorf("commit idempotency tx: %w", err)
	}

	return code, result, nil
}

// idempotencyLockID derives a stable non-negative PostgreSQL advisory-lock ID.
func idempotencyLockID(actorID, key uuid.UUID) int64 {
	h := sha256.Sum256([]byte(actorID.String() + ":" + key.String()))
	return int64(binary.BigEndian.Uint64(h[:8]) & (1<<63 - 1))
}
