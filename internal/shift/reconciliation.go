package shift

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/platform/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
)

// startReconciliationFingerprint is the idempotency fingerprint for a start
// command: every request-relevant field and no secret, because a Start carries
// no approval input.
type startReconciliationFingerprint struct {
	SalesShiftID   uuid.UUID `json:"sales_shift_id"`
	CountedCashVND int64     `json:"counted_cash_vnd"`
}

type reconciliationStartedAuditDetails struct {
	SalesShiftID     uuid.UUID `json:"sales_shift_id"`
	ReconciliationID uuid.UUID `json:"reconciliation_id"`
}

type cashCountRecordedAuditDetails struct {
	SalesShiftID     uuid.UUID `json:"sales_shift_id"`
	ReconciliationID uuid.UUID `json:"reconciliation_id"`
	CashCountID      uuid.UUID `json:"cash_count_id"`
	Sequence         int       `json:"sequence"`
	CountedCashVND   int64     `json:"counted_cash_vnd"`
}

// StartReconciliationHandler starts a Sales Shift's blind reconciliation.
type StartReconciliationHandler struct{ runner *Runner }

// NewStartReconciliationHandler creates a new StartReconciliationHandler.
func NewStartReconciliationHandler(runner *Runner) *StartReconciliationHandler {
	return &StartReconciliationHandler{runner: runner}
}

// validateCountedCash checks a counted Cash amount. Zero is a valid count; the
// bound is the existing non-negative money-input bound the database re-checks.
func validateCountedCash(v int64) error {
	if v < 0 {
		return fmt.Errorf("counted_cash_vnd cannot be negative")
	}
	if v > MaxAmountVND {
		return fmt.Errorf("counted_cash_vnd exceeds the maximum of %d", MaxAmountVND)
	}
	return nil
}

// Handle starts the blind reconciliation of the named OPEN Shift: it freezes
// the financial snapshot, records the initial Cash Count as sequence 1, and
// moves the Shift to CLOSING.
//
// The counted amount crosses the boundary before any expected value is
// revealed, and the response is returned only after the snapshot, the initial
// count, the state transition, and both audit events have committed together.
// If any step fails the Shift remains OPEN and the initial count is not
// stored, so the counted cash of a failed start never survives.
func (h *StartReconciliationHandler) Handle(ctx context.Context, actor Actor, cmd StartReconciliationCommand) (int, ClosingShiftResponse, error) {
	if cmd.CountedCashVND == nil {
		return 0, ClosingShiftResponse{}, fmt.Errorf("%w: counted_cash_vnd is required", response.ErrInvalid)
	}
	countedCash := *cmd.CountedCashVND

	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpStartReconciliation,
		Fingerprint: startReconciliationFingerprint{
			SalesShiftID:   cmd.ShiftID,
			CountedCashVND: countedCash,
		},
		Required: []string{CapSalesShiftOperate},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, ClosingShiftResponse, AuditRecord, error) {
			result, audit, err := startReconciliation(ctx, mc, cmd.ShiftID, countedCash)
			if err != nil {
				return 0, ClosingShiftResponse{}, AuditRecord{}, err
			}
			return 201, result, audit, nil
		})
}

// startReconciliation is the start mutation body. The start event flows
// through the returned AuditRecord, which the pipeline writes just before the
// commit.
func startReconciliation(ctx context.Context, mc MutationContext, shiftID uuid.UUID,
	countedCash int64,
) (ClosingShiftResponse, AuditRecord, error) {
	if err := validateCountedCash(countedCash); err != nil {
		return ClosingShiftResponse{}, AuditRecord{}, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error())
	}

	locked, err := lockOpenShiftForStart(ctx, mc.Queries, shiftID)
	if err != nil {
		return ClosingShiftResponse{}, AuditRecord{}, err
	}

	// Global blockers in one MVCC read, rejected in their load-bearing
	// precedence. They never take Check or Session row locks while the Shift
	// lock is held.
	if err := loadClosureBlockers(ctx, mc.Queries); err != nil {
		return ClosingShiftResponse{}, AuditRecord{}, err
	}

	params, err := freezeReconciliationSnapshot(ctx, mc.Queries, mc.Actor, locked)
	if err != nil {
		return ClosingShiftResponse{}, AuditRecord{}, err
	}

	// The immutable snapshot and the blind initial count are born in this
	// order, before any read path can observe one without the other.
	snapshot, err := mc.Queries.InsertShiftReconciliation(ctx, params)
	if err != nil {
		return ClosingShiftResponse{}, AuditRecord{}, MapDBError(err)
	}
	count, err := mc.Queries.InsertShiftCashCount(ctx, sqlc.InsertShiftCashCountParams{
		ReconciliationID:            snapshot.ID,
		Sequence:                    1,
		CountedCashVnd:              countedCash,
		CountedByStaffIdentityID:    mc.Actor.StaffID,
		CountedStaffAccessSessionID: mc.Actor.SessionID,
	})
	if err != nil {
		return ClosingShiftResponse{}, AuditRecord{}, MapDBError(err)
	}

	if err := mc.Queries.TransitionSalesShiftToClosing(ctx, locked.ID); err != nil {
		return ClosingShiftResponse{}, AuditRecord{}, fmt.Errorf("transition sales shift to closing: %w", err)
	}

	// The initial-count event is written here so both start events share this
	// transaction.
	if err := insertInitialCashCountAudit(ctx, mc.Queries, mc.Actor, cashCountRecordedAuditDetails{
		SalesShiftID:     locked.ID,
		ReconciliationID: snapshot.ID,
		CashCountID:      count.ID,
		Sequence:         int(count.Sequence),
		CountedCashVND:   count.CountedCashVnd,
	}); err != nil {
		return ClosingShiftResponse{}, AuditRecord{}, err
	}

	result, err := revealClosingShift(ctx, mc.Queries, locked)
	if err != nil {
		return ClosingShiftResponse{}, AuditRecord{}, err
	}
	return result, AuditRecord{
		EventType: EventReconciliationStarted,
		Details: reconciliationStartedAuditDetails{
			SalesShiftID:     locked.ID,
			ReconciliationID: result.Reconciliation.ID,
		},
	}, nil
}

// lockOpenShiftForStart locks the named Shift FOR UPDATE. The state is read
// rather than filtered, so an unknown Shift, a CLOSING one, and a CLOSED one
// each map to their own error.
func lockOpenShiftForStart(ctx context.Context, q *sqlc.Queries, shiftID uuid.UUID) (sqlc.SalesShift, error) {
	locked, err := q.LockSalesShiftForReconciliation(ctx, shiftID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sqlc.SalesShift{}, ErrSalesShiftNotFound
		}
		return sqlc.SalesShift{}, MapDBError(err)
	}
	switch locked.State {
	case StateClosing:
		// Two independent starts: the loser reads CLOSING under lock and
		// reports the stable conflict.
		return sqlc.SalesShift{}, ErrShiftAlreadyClosing
	case StateClosed:
		// Start requires an OPEN Shift; a CLOSED one fails the same lifecycle
		// rule from the other side.
		return sqlc.SalesShift{}, ErrOpenShiftRequired
	}
	return locked, nil
}

// freezeReconciliationSnapshot reads the authoritative Shift-scoped totals and
// the Cash Movement sums and derives the frozen expectations, all inside the
// Shift lock, so an in-flight financial writer is wholly included or wholly
// rejected.
//
// The checked formulas run before anything is written. A range or overflow
// failure here is corrupt data, not client input: it becomes the private
// calculation-failed sentinel, and only its generic message crosses the
// boundary.
func freezeReconciliationSnapshot(ctx context.Context, q *sqlc.Queries, actor Actor,
	locked sqlc.SalesShift,
) (sqlc.InsertShiftReconciliationParams, error) {
	totals, err := q.GetShiftReconciliationTotals(ctx, locked.ID)
	if err != nil {
		return sqlc.InsertShiftReconciliationParams{}, fmt.Errorf("load reconciliation totals: %w", err)
	}
	movements, err := q.SumCashMovements(ctx, locked.ID)
	if err != nil {
		return sqlc.InsertShiftReconciliationParams{}, fmt.Errorf("sum cash movements: %w", err)
	}

	expectedCash, err := ComputeExpectedCash(locked.OpeningFloatVnd,
		totals.CashPaymentVnd, totals.CashPaymentVoidVnd, totals.CashRefundVnd,
		movements.PayInVnd, movements.PayOutVnd)
	if err != nil {
		return sqlc.InsertShiftReconciliationParams{},
			fmt.Errorf("compute expected cash: %w: %w", errReconciliationCalculationFailed, err)
	}
	expectedQRReceived, err := subtractAmount(totals.ManualQrPaymentVnd, totals.ManualQrPaymentVoidVnd)
	if err != nil {
		return sqlc.InsertShiftReconciliationParams{},
			fmt.Errorf("compute expected manual QR received: %w: %w", errReconciliationCalculationFailed, err)
	}
	// The snapshot table bounds this figure to the same symmetric
	// ±MaxAmountVND window Expected Cash keeps. A breach would otherwise
	// surface as an unmapped check violation, and a start that can never
	// succeed would strand the Shift short of CLOSING permanently.
	if expectedQRReceived > MaxAmountVND || expectedQRReceived < -MaxAmountVND {
		rangeErr := fmt.Errorf("%w: expected manual QR received %d is outside [%d, %d]",
			ErrExpectedCashOutOfRange, expectedQRReceived, -MaxAmountVND, MaxAmountVND)
		return sqlc.InsertShiftReconciliationParams{},
			fmt.Errorf("compute expected manual QR received: %w: %w", errReconciliationCalculationFailed, rangeErr)
	}

	return sqlc.InsertShiftReconciliationParams{
		SalesShiftID:                locked.ID,
		StartedByStaffIdentityID:    actor.StaffID,
		StartedStaffAccessSessionID: actor.SessionID,
		OpeningFloatVnd:             locked.OpeningFloatVnd,
		PayInVnd:                    movements.PayInVnd,
		PayOutVnd:                   movements.PayOutVnd,
		CashPaymentVnd:              totals.CashPaymentVnd,
		CashPaymentVoidVnd:          totals.CashPaymentVoidVnd,
		CashRefundVnd:               totals.CashRefundVnd,
		ExpectedCashVnd:             expectedCash,
		ManualQrPaymentVnd:          totals.ManualQrPaymentVnd,
		ManualQrPaymentVoidVnd:      totals.ManualQrPaymentVoidVnd,
		ExpectedManualQrReceivedVnd: expectedQRReceived,
		ManualQrRefundVnd:           totals.ManualQrRefundVnd,
	}, nil
}

// insertInitialCashCountAudit writes the cash-count event for the blind
// initial count inside the start transaction.
func insertInitialCashCountAudit(ctx context.Context, q *sqlc.Queries, actor Actor,
	details cashCountRecordedAuditDetails,
) error {
	countDetails, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("marshal audit details: %w", err)
	}
	if _, err := q.InsertAuditEvent(ctx, sqlc.InsertAuditEventParams{
		EventType:  EventCashCountRecorded,
		ActorID:    uuid.NullUUID{UUID: actor.StaffID, Valid: true},
		SessionID:  uuid.NullUUID{UUID: actor.SessionID, Valid: true},
		Details:    countDetails,
		OccurredAt: time.Now(),
	}); err != nil {
		return fmt.Errorf("insert %q audit event: %w", EventCashCountRecorded, err)
	}
	return nil
}

// revealClosingShift reads the frozen snapshot back through the same load the
// CLOSING read uses, so the start response and the read response share one
// shape.
func revealClosingShift(ctx context.Context, q *sqlc.Queries, locked sqlc.SalesShift) (ClosingShiftResponse, error) {
	recon, err := loadReconciliationSnapshot(ctx, q, locked.ID)
	if err != nil {
		return ClosingShiftResponse{}, err
	}
	opener, err := q.GetStaffSummary(ctx, locked.OpenedByStaffIdentityID)
	if err != nil {
		return ClosingShiftResponse{}, fmt.Errorf("load opener summary: %w", err)
	}
	return ClosingShiftResponse{
		SalesShiftMetadata: SalesShiftMetadata{
			ID:       locked.ID,
			State:    StateClosing,
			OpenedAt: locked.OpenedAt,
			Opener: StaffSummary{
				ID:          opener.ID,
				DisplayName: opener.DisplayName,
				LoginCode:   opener.LoginCode,
			},
		},
		Reconciliation: recon,
	}, nil
}

// loadClosureBlockers evaluates every global closure blocker in one read and
// returns the first violation in their load-bearing precedence: unsettled
// Checks, pending Refunds, unresolved financial correction obligations, active
// Service Sessions. Start and Final Close both evaluate the set through this
// helper. Blocker reads are MVCC reads; the caller owns whatever row locking
// its protocol requires.
func loadClosureBlockers(ctx context.Context, q *sqlc.Queries) error {
	blockers, err := q.GetGlobalShiftClosureBlockers(ctx)
	if err != nil {
		return fmt.Errorf("load global closure blockers: %w", err)
	}
	switch {
	case blockers.UnsettledCheckCount > 0:
		return ErrUnsettledCheck
	case blockers.PendingRefundCount > 0:
		return ErrPendingRefund
	case blockers.UnresolvedCorrectionVnd > 0:
		return ErrUnresolvedCorrection
	case blockers.ActiveServiceSessionCount > 0:
		return ErrActiveServiceSession
	}
	return nil
}

// loadReconciliationSnapshot reads one Shift's frozen reconciliation back with
// its append-only attempts and the preview built from the latest evidence.
// Start and the attempt commands call it inside their own transactions to
// build the revealed response, Final Close re-verifies it, and the CLOSING
// current read consumes it for its frozen shape.
//
// A missing snapshot or a missing cash count maps to
// ErrReconciliationNotStarted: a reconciliation row always carries count
// sequence 1, because Start inserts both before any read can observe them.
func loadReconciliationSnapshot(ctx context.Context, q *sqlc.Queries, shiftID uuid.UUID) (ReconciliationResponse, error) {
	snapshot, err := q.GetReconciliationSnapshot(ctx, shiftID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ReconciliationResponse{}, ErrReconciliationNotStarted
		}
		return ReconciliationResponse{}, fmt.Errorf("load reconciliation snapshot: %w", err)
	}

	starter, err := q.GetStaffSummary(ctx, snapshot.StartedByStaffIdentityID)
	if err != nil {
		return ReconciliationResponse{}, fmt.Errorf("load starter summary: %w", err)
	}

	cashCounts, err := loadSnapshotCashCounts(ctx, q, snapshot.ID)
	if err != nil {
		return ReconciliationResponse{}, err
	}
	qrObservations, err := loadSnapshotQRObservations(ctx, q, snapshot.ID)
	if err != nil {
		return ReconciliationResponse{}, err
	}

	evidence, err := q.GetLatestReconciliationEvidence(ctx, snapshot.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ReconciliationResponse{}, ErrReconciliationNotStarted
		}
		return ReconciliationResponse{}, fmt.Errorf("load latest reconciliation evidence: %w", err)
	}
	preview, err := buildReconciliationPreview(
		snapshot.ExpectedCashVnd, snapshot.ExpectedManualQrReceivedVnd, snapshot.ManualQrRefundVnd,
		evidence)
	if err != nil {
		return ReconciliationResponse{}, err
	}

	return ReconciliationResponse{
		ID:        snapshot.ID,
		Starter:   staffSummaryFromRow(starter),
		StartedAt: snapshot.StartedAt,

		OpeningFloatVND:                 snapshot.OpeningFloatVnd,
		PayInVND:                        snapshot.PayInVnd,
		PayOutVND:                       snapshot.PayOutVnd,
		CashPaymentVND:                  snapshot.CashPaymentVnd,
		CashPaymentVoidVND:              snapshot.CashPaymentVoidVnd,
		CashRefundVND:                   snapshot.CashRefundVnd,
		ExpectedCashVND:                 snapshot.ExpectedCashVnd,
		ManualQRPaymentVND:              snapshot.ManualQrPaymentVnd,
		ManualQRPaymentVoidVND:          snapshot.ManualQrPaymentVoidVnd,
		ExpectedManualQRReceivedVND:     snapshot.ExpectedManualQrReceivedVnd,
		ManualQRRefundVND:               snapshot.ManualQrRefundVnd,
		PendingManualQRRefundVND:        snapshot.PendingManualQrRefundVnd,
		PendingRefundVND:                snapshot.PendingRefundVnd,
		UnresolvedPostSaleAdjustmentVND: snapshot.UnresolvedPostSaleAdjustmentVnd,

		CashCounts:     cashCounts,
		QRObservations: qrObservations,
		Preview:        preview,
	}, nil
}

// loadSnapshotCashCounts reads a reconciliation's append-only Cash Count
// ledger, ordered by sequence, with each attempt's actor summary.
func loadSnapshotCashCounts(ctx context.Context, q *sqlc.Queries, reconciliationID uuid.UUID) ([]CashCountResponse, error) {
	rows, err := q.ListShiftCashCounts(ctx, reconciliationID)
	if err != nil {
		return nil, fmt.Errorf("list cash counts: %w", err)
	}
	cashCounts := make([]CashCountResponse, 0, len(rows))
	for _, row := range rows {
		countedBy, err := q.GetStaffSummary(ctx, row.CountedByStaffIdentityID)
		if err != nil {
			return nil, fmt.Errorf("load cash count actor: %w", err)
		}
		cashCounts = append(cashCounts, CashCountResponse{
			ID:             row.ID,
			Sequence:       int(row.Sequence),
			CountedCashVND: row.CountedCashVnd,
			CountedBy:      staffSummaryFromRow(countedBy),
			CountedAt:      row.CountedAt,
		})
	}
	return cashCounts, nil
}

// loadSnapshotQRObservations reads a reconciliation's append-only Manual QR
// observation ledger, ordered by sequence, with each attempt's actor summary.
func loadSnapshotQRObservations(ctx context.Context, q *sqlc.Queries, reconciliationID uuid.UUID) ([]QRObservationResponse, error) {
	rows, err := q.ListShiftQRObservations(ctx, reconciliationID)
	if err != nil {
		return nil, fmt.Errorf("list QR observations: %w", err)
	}
	qrObservations := make([]QRObservationResponse, 0, len(rows))
	for _, row := range rows {
		observedBy, err := q.GetStaffSummary(ctx, row.ObservedByStaffIdentityID)
		if err != nil {
			return nil, fmt.Errorf("load QR observation actor: %w", err)
		}
		qrObservations = append(qrObservations, QRObservationResponse{
			ID:                  row.ID,
			Sequence:            int(row.Sequence),
			ObservedReceivedVND: row.ObservedReceivedVnd,
			ObservedRefundedVND: row.ObservedRefundedVnd,
			ObservedBy:          staffSummaryFromRow(observedBy),
			ObservedAt:          row.ObservedAt,
		})
	}
	return qrObservations, nil
}

// buildReconciliationPreview compares the three frozen expected values with
// the latest attempt of each ledger. The Cash evidence always exists (Start
// inserts count sequence 1 before any preview is built); the QR side is
// nullable until the first observation lands. A dimension requires a recheck
// when its latest evidence disagrees with the frozen expectation, and the
// Shift can close exactly only when every dimension is exact and a QR
// observation exists.
//
// Its ComputeDifference failures carry no calculation-failed sentinel wrap,
// unlike the start path: this preview is built only after the reveal, both
// sides of each comparison are already bounded (the frozen expectations were
// range-checked at start and attempt amounts carry database CHECK bounds), and
// MaxAmountVND keeps the subtraction far from int64 wrap. An error here is
// therefore corrupt state that surfaces as the generic 500, not a secrecy
// boundary.
func buildReconciliationPreview(expectedCashVND, expectedQRReceivedVND, expectedQRRefundedVND int64,
	evidence sqlc.GetLatestReconciliationEvidenceRow,
) (ReconciliationPreview, error) {
	preview := ReconciliationPreview{Dimensions: make([]ReconciliationPreviewEntry, 0, 3)}

	cashDifference, err := ComputeDifference(evidence.CountedCashVnd, expectedCashVND)
	if err != nil {
		return ReconciliationPreview{}, fmt.Errorf("compute cash difference: %w", err)
	}
	observedCash := evidence.CountedCashVnd
	preview.Dimensions = append(preview.Dimensions, ReconciliationPreviewEntry{
		Dimension:       DimensionCash,
		ExpectedVND:     expectedCashVND,
		ObservedVND:     &observedCash,
		DifferenceVND:   &cashDifference,
		RecheckRequired: cashDifference != 0,
	})

	anyRecheck := cashDifference != 0
	hasObservation := evidence.QrObservationID.Valid

	var receivedDifference, refundedDifference int64
	if hasObservation {
		receivedDifference, err = ComputeDifference(evidence.ObservedReceivedVnd.Int64, expectedQRReceivedVND)
		if err != nil {
			return ReconciliationPreview{}, fmt.Errorf("compute QR received difference: %w", err)
		}
		refundedDifference, err = ComputeDifference(evidence.ObservedRefundedVnd.Int64, expectedQRRefundedVND)
		if err != nil {
			return ReconciliationPreview{}, fmt.Errorf("compute QR refunded difference: %w", err)
		}
		anyRecheck = anyRecheck || receivedDifference != 0 || refundedDifference != 0
	}

	observedReceived := evidence.ObservedReceivedVnd.Int64
	observedRefunded := evidence.ObservedRefundedVnd.Int64
	preview.Dimensions = append(preview.Dimensions,
		ReconciliationPreviewEntry{
			Dimension:       DimensionManualQRReceived,
			ExpectedVND:     expectedQRReceivedVND,
			ObservedVND:     nilIfAbsent(hasObservation, &observedReceived),
			DifferenceVND:   nilIfAbsent(hasObservation, &receivedDifference),
			RecheckRequired: hasObservation && receivedDifference != 0,
		},
		ReconciliationPreviewEntry{
			Dimension:       DimensionManualQRRefunded,
			ExpectedVND:     expectedQRRefundedVND,
			ObservedVND:     nilIfAbsent(hasObservation, &observedRefunded),
			DifferenceVND:   nilIfAbsent(hasObservation, &refundedDifference),
			RecheckRequired: hasObservation && refundedDifference != 0,
		})

	preview.CanClose = hasObservation && !anyRecheck
	return preview, nil
}

// nilIfAbsent maps missing evidence to a nil pointer, since a present zero is
// a meaningful observed value.
func nilIfAbsent(present bool, v *int64) *int64 {
	if !present {
		return nil
	}
	return v
}

// staffSummaryFromRow builds the boundary summary from a GetStaffSummaryRow.
func staffSummaryFromRow(row sqlc.GetStaffSummaryRow) StaffSummary {
	return StaffSummary{ID: row.ID, DisplayName: row.DisplayName, LoginCode: row.LoginCode}
}
