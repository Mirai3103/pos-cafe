package shift

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
)

// cashCountFingerprint is the idempotency fingerprint for appending a Cash
// Count: the Shift and the attempt's own business value, and nothing else. No
// secret exists on this surface.
type cashCountFingerprint struct {
	SalesShiftID   uuid.UUID `json:"sales_shift_id"`
	CountedCashVND int64     `json:"counted_cash_vnd"`
}

// qrObservationFingerprint is the idempotency fingerprint for appending a QR
// Observation. Both observed values are fingerprinted together, so reusing a
// request id with either value changed conflicts instead of replaying.
type qrObservationFingerprint struct {
	SalesShiftID        uuid.UUID `json:"sales_shift_id"`
	ObservedReceivedVND int64     `json:"observed_received_vnd"`
	ObservedRefundedVND int64     `json:"observed_refunded_vnd"`
}

// qrObservationRecordedAuditDetails names the appended observation. The
// Cash-count twin (cashCountRecordedAuditDetails) lives beside the start
// handler, which writes the initial-count event.
type qrObservationRecordedAuditDetails struct {
	SalesShiftID        uuid.UUID `json:"sales_shift_id"`
	ReconciliationID    uuid.UUID `json:"reconciliation_id"`
	QRObservationID     uuid.UUID `json:"qr_observation_id"`
	Sequence            int       `json:"sequence"`
	ObservedReceivedVND int64     `json:"observed_received_vnd"`
	ObservedRefundedVND int64     `json:"observed_refunded_vnd"`
}

// RecordCashCountHandler appends a Cash Count attempt to a CLOSING Shift.
type RecordCashCountHandler struct{ runner *Runner }

// NewRecordCashCountHandler creates a new RecordCashCountHandler.
func NewRecordCashCountHandler(runner *Runner) *RecordCashCountHandler {
	return &RecordCashCountHandler{runner: runner}
}

// RecordQRObservationHandler appends a QR Observation attempt to a CLOSING
// Shift.
type RecordQRObservationHandler struct{ runner *Runner }

// NewRecordQRObservationHandler creates a new RecordQRObservationHandler.
func NewRecordQRObservationHandler(runner *Runner) *RecordQRObservationHandler {
	return &RecordQRObservationHandler{runner: runner}
}

// validateObservedAmount checks one observed attempt value. Zero is a valid
// observation; the bound is the existing non-negative money-input bound the
// database re-checks.
func validateObservedAmount(v int64, field string) error {
	if v < 0 {
		return fmt.Errorf("%s cannot be negative", field)
	}
	if v > MaxAmountVND {
		return fmt.Errorf("%s exceeds the maximum of %d", field, MaxAmountVND)
	}
	return nil
}

// lockClosingShift locks the named Shift FOR UPDATE (spec 11.1) for the
// attempt commands and returns its row. Every error is narrowed per path
// before the legacy blanket mapping (spec 12): an unknown Shift is 404, and
// every non-CLOSING state is its own 409 — attempts are the inverse of the
// ordinary commands that require OPEN (spec 4.3).
func lockClosingShift(ctx context.Context, q *sqlc.Queries, shiftID uuid.UUID) (sqlc.SalesShift, error) {
	locked, err := q.LockSalesShiftForReconciliation(ctx, shiftID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sqlc.SalesShift{}, ErrSalesShiftNotFound
		}
		return sqlc.SalesShift{}, MapDBError(err)
	}
	switch locked.State {
	case StateClosing:
		return locked, nil
	case StateClosed:
		return sqlc.SalesShift{}, ErrShiftAlreadyClosed
	case StateOpen:
		// No reconciliation exists to append to while the Shift is OPEN.
		return sqlc.SalesShift{}, ErrReconciliationNotStarted
	default:
		return sqlc.SalesShift{}, fmt.Errorf("unknown sales shift state %q", locked.State)
	}
}

// loadReconciliationForAppend reads the reconciliation row the Shift's CLOSING
// state promises, narrowing a missing row to the stable not-started conflict.
// A CLOSING Shift always carries its snapshot through the API, so reaching
// that branch means corrupt state, and it maps to the 409 lifecycle error
// rather than a blanket mapping or a 500.
func loadReconciliationForAppend(ctx context.Context, q *sqlc.Queries, shiftID uuid.UUID) (sqlc.ShiftReconciliation, error) {
	snapshot, err := q.GetReconciliationSnapshot(ctx, shiftID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sqlc.ShiftReconciliation{}, ErrReconciliationNotStarted
		}
		return sqlc.ShiftReconciliation{}, MapDBError(err)
	}
	return snapshot, nil
}

// Handle appends one immutable Cash Count attempt to the named CLOSING Shift's
// reconciliation (spec 4.3, 7.2): it locks the Shift FOR UPDATE, takes the
// next Cash sequence through a query under that lock, inserts exactly one
// append-only row, writes one audit event, and returns the new attempt plus
// the full preview built from the latest evidence.
func (h *RecordCashCountHandler) Handle(ctx context.Context, actor Actor, cmd RecordCashCountCommand) (int, CashCountResult, error) {
	if cmd.CountedCashVND == nil {
		return 0, CashCountResult{}, fmt.Errorf("%w: counted_cash_vnd is required", response.ErrInvalid)
	}
	countedCash := *cmd.CountedCashVND

	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpRecordCashCount,
		Fingerprint: cashCountFingerprint{
			SalesShiftID:   cmd.ShiftID,
			CountedCashVND: countedCash,
		},
		Required: []string{CapSalesShiftOperate},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, CashCountResult, AuditRecord, error) {
			var zero CashCountResult

			if err := validateCountedCash(countedCash); err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error())
			}

			locked, err := lockClosingShift(ctx, mc.Queries, cmd.ShiftID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			snapshot, err := loadReconciliationForAppend(ctx, mc.Queries, cmd.ShiftID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}

			// The next sequence is read under the Shift lock every append
			// holds, so the blind initial count and every recount are
			// serialized through it and never share a sequence.
			sequence, err := mc.Queries.GetNextShiftCashCountSequence(ctx, snapshot.ID)
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("next cash count sequence: %w", err)
			}
			count, err := mc.Queries.InsertShiftCashCount(ctx, sqlc.InsertShiftCashCountParams{
				ReconciliationID:            snapshot.ID,
				Sequence:                    sequence,
				CountedCashVnd:              countedCash,
				CountedByStaffIdentityID:    actor.StaffID,
				CountedStaffAccessSessionID: actor.SessionID,
			})
			if err != nil {
				return 0, zero, AuditRecord{}, MapDBError(err)
			}

			countedBy, err := mc.Queries.GetStaffSummary(ctx, actor.StaffID)
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("load cash count actor: %w", err)
			}

			// Reveal: the frozen snapshot is read back through the same load
			// the CLOSING read uses, so the appended attempt is already inside
			// the ledgers the preview is built from (spec 4.3).
			recon, err := loadReconciliationSnapshot(ctx, mc.Queries, locked.ID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}

			return 201, CashCountResult{
				CashCount: CashCountResponse{
					ID:             count.ID,
					Sequence:       int(count.Sequence),
					CountedCashVND: count.CountedCashVnd,
					CountedBy:      staffSummaryFromRow(countedBy),
					CountedAt:      count.CountedAt,
				},
				Preview: latestPreview(recon),
			}, AuditRecord{
				EventType: EventCashCountRecorded,
				Details: cashCountRecordedAuditDetails{
					SalesShiftID:     locked.ID,
					ReconciliationID: snapshot.ID,
					CashCountID:      count.ID,
					Sequence:         int(count.Sequence),
					CountedCashVND:   count.CountedCashVnd,
				},
			}, nil
		})
}

// Handle appends one immutable Manual QR observation attempt to the named
// CLOSING Shift's reconciliation (spec 4.3, 7.3): both observed values are
// explicit and stored together, the sequence counts independently of the Cash
// ledger, and the response is the new attempt plus the full preview built from
// the latest evidence.
func (h *RecordQRObservationHandler) Handle(ctx context.Context, actor Actor, cmd RecordQRObservationCommand) (int, QRObservationResult, error) {
	if cmd.ObservedReceivedVND == nil {
		return 0, QRObservationResult{}, fmt.Errorf("%w: observed_received_vnd is required", response.ErrInvalid)
	}
	if cmd.ObservedRefundedVND == nil {
		return 0, QRObservationResult{}, fmt.Errorf("%w: observed_refunded_vnd is required", response.ErrInvalid)
	}
	received := *cmd.ObservedReceivedVND
	refunded := *cmd.ObservedRefundedVND

	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpRecordQRObservation,
		Fingerprint: qrObservationFingerprint{
			SalesShiftID:        cmd.ShiftID,
			ObservedReceivedVND: received,
			ObservedRefundedVND: refunded,
		},
		Required: []string{CapSalesShiftOperate},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, QRObservationResult, AuditRecord, error) {
			var zero QRObservationResult

			if err := validateObservedAmount(received, "observed_received_vnd"); err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error())
			}
			if err := validateObservedAmount(refunded, "observed_refunded_vnd"); err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error())
			}

			locked, err := lockClosingShift(ctx, mc.Queries, cmd.ShiftID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			snapshot, err := loadReconciliationForAppend(ctx, mc.Queries, cmd.ShiftID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}

			sequence, err := mc.Queries.GetNextShiftQRObservationSequence(ctx, snapshot.ID)
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("next QR observation sequence: %w", err)
			}
			observation, err := mc.Queries.InsertShiftQRObservation(ctx, sqlc.InsertShiftQRObservationParams{
				ReconciliationID:             snapshot.ID,
				Sequence:                     sequence,
				ObservedReceivedVnd:          received,
				ObservedRefundedVnd:          refunded,
				ObservedByStaffIdentityID:    actor.StaffID,
				ObservedStaffAccessSessionID: actor.SessionID,
			})
			if err != nil {
				return 0, zero, AuditRecord{}, MapDBError(err)
			}

			observedBy, err := mc.Queries.GetStaffSummary(ctx, actor.StaffID)
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("load QR observation actor: %w", err)
			}

			recon, err := loadReconciliationSnapshot(ctx, mc.Queries, locked.ID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}

			return 201, QRObservationResult{
				QRObservation: QRObservationResponse{
					ID:                  observation.ID,
					Sequence:            int(observation.Sequence),
					ObservedReceivedVND: observation.ObservedReceivedVnd,
					ObservedRefundedVND: observation.ObservedRefundedVnd,
					ObservedBy:          staffSummaryFromRow(observedBy),
					ObservedAt:          observation.ObservedAt,
				},
				Preview: latestPreview(recon),
			}, AuditRecord{
				EventType: EventQRObservationRecorded,
				Details: qrObservationRecordedAuditDetails{
					SalesShiftID:        locked.ID,
					ReconciliationID:    snapshot.ID,
					QRObservationID:     observation.ID,
					Sequence:            int(observation.Sequence),
					ObservedReceivedVND: observation.ObservedReceivedVnd,
					ObservedRefundedVND: observation.ObservedRefundedVnd,
				},
			}, nil
		})
}

// latestPreview exposes the preview of an already-loaded reconciliation
// snapshot as a pure function over that data (spec 9.6). The preview inside
// ReconciliationResponse is built by the shared loading path from the latest
// attempt of each ledger; the append commands, Final Close (Task 6), and the
// CLOSING current read (Task 7) consume it through this seam so no consumer
// re-derives or re-queries the preview from live data.
func latestPreview(snapshot ReconciliationResponse) ReconciliationPreview {
	return snapshot.Preview
}
