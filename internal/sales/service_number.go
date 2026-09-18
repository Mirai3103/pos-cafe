package sales

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

// lockOpenSalesShiftForStart returns the open Sales Shift's id, or
// ErrOpenShiftRequired when none is open. It is Session Start's gate; the
// lockOpenSalesShift twin in payments.go reports a missing Shift through
// checkPreconditions instead, so the two cannot share a name or a contract.
//
// It runs inside the mutation body, after the idempotency claim, so that a
// replay of a request that succeeded during a Shift still returns its stored
// result once that Shift has closed. The precondition guards new work only.
//
// The read takes the Shift row FOR SHARE and holds it through the Session
// insert and the transaction's commit, so a Shift closure that validates its
// blockers under FOR UPDATE cannot miss a Session created concurrently: a
// closure either commits before the start's validation and this read answers
// nothing or re-checks the OPEN predicate against the committed CLOSING row,
// or it waits behind the start and sees the Session as a blocker. Only OPEN
// matches — a CLOSING Shift must not start a Session.
func lockOpenSalesShiftForStart(ctx context.Context, q *sqlc.Queries) (uuid.UUID, error) {
	id, err := q.LockOpenSalesShiftForShare(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, ErrOpenShiftRequired
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("lock open sales shift: %w", err)
	}
	return id, nil
}

// allocateServiceNumber reserves the next Service Number within a Sales Shift.
//
// The canonical implementation derived the number from six hexadecimal
// characters of the Session UUID and retried on collision up to five times,
// against a permanently global unique index. That has an unrecoverable
// exhaustion mode in the system's most frequent operation. ADR-011 replaces it
// with a Shift-scoped sequence serialized by a transaction-scoped advisory
// lock, so there is no retry loop and no exhaustion.
//
// The advisory lock is mandatory: GetNextServiceSequence reads a maximum, and
// two concurrent readers without the lock compute the same next value.
func allocateServiceNumber(ctx context.Context, r *Runner, q *sqlc.Queries, shiftID uuid.UUID) (
	int32, string, error,
) {
	if err := r.AdvisoryLock(ctx, q, IDToLockKey(shiftID)); err != nil {
		return 0, "", err
	}
	next, err := q.GetNextServiceSequence(ctx, shiftID)
	if err != nil {
		return 0, "", fmt.Errorf("compute next service sequence: %w", err)
	}
	number, err := FormatServiceNumber(next)
	if err != nil {
		return 0, "", err
	}
	return next, number, nil
}

// insertSessionWithDraft creates a Service Session and its editable Order
// Draft. Opening a Session always creates its draft, so the projection's draft
// is never null in 5A.
func insertSessionWithDraft(ctx context.Context, q *sqlc.Queries, actor Actor,
	mode string, shiftID uuid.UUID, sequence int32, number string,
) (uuid.UUID, error) {
	session, err := q.InsertServiceSession(ctx, sqlc.InsertServiceSessionParams{
		ServiceNumber:            number,
		Sequence:                 sequence,
		ServiceMode:              mode,
		CreatedByStaffIdentityID: actor.StaffID,
		SalesShiftID:             shiftID,
	})
	if err != nil {
		return uuid.Nil, MapDBError(err)
	}
	if _, err := q.InsertOrderDraft(ctx, session.ID); err != nil {
		return uuid.Nil, fmt.Errorf("create order draft: %w", err)
	}
	return session.ID, nil
}
