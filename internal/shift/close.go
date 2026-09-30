package shift

import (
	"context"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

// closeDiscrepancyFingerprint is one normalized dimension/reason/note tuple in
// the close idempotency fingerprint. The note is trimmed before hashing, so an
// idempotent replay stays stable across cosmetic whitespace differences.
type closeDiscrepancyFingerprint struct {
	Dimension DiscrepancyDimension `json:"dimension"`
	Reason    DiscrepancyReason    `json:"reason"`
	Note      *string              `json:"note"`
}

// closeShiftFingerprint is the idempotency fingerprint for a Final Close: the
// Shift, the final evidence ids, and the normalized discrepancy tuples.
//
// The approver login and the PIN are deliberately absent: the PIN is a secret and the approver identity is verification input, not business
// input, so neither may make an otherwise identical close conflict — or store
// a PIN-derived value at rest.
type closeShiftFingerprint struct {
	SalesShiftID         uuid.UUID                     `json:"sales_shift_id"`
	FinalCashCountID     uuid.UUID                     `json:"final_cash_count_id"`
	FinalQRObservationID uuid.UUID                     `json:"final_qr_observation_id"`
	Discrepancies        []closeDiscrepancyFingerprint `json:"discrepancies"`
}

// closedDiscrepancyAuditDetails names one stored discrepancy reason.
type closedDiscrepancyAuditDetails struct {
	Dimension DiscrepancyDimension `json:"dimension"`
	Reason    DiscrepancyReason    `json:"reason"`
}

// closedShiftAuditDetails carries the closure facts: the snapshot ids, the final evidence ids, all three differences, the discrepancy reasons,
// and the approver identity when present. No approval secret appears here.
type closedShiftAuditDetails struct {
	SalesShiftID                  uuid.UUID                       `json:"sales_shift_id"`
	ReconciliationID              uuid.UUID                       `json:"reconciliation_id"`
	ClosureID                     uuid.UUID                       `json:"closure_id"`
	FinalCashCountID              uuid.UUID                       `json:"final_cash_count_id"`
	FinalQRObservationID          uuid.UUID                       `json:"final_qr_observation_id"`
	CashDifferenceVND             int64                           `json:"cash_difference_vnd"`
	ManualQRReceivedDifferenceVND int64                           `json:"manual_qr_received_difference_vnd"`
	ManualQRRefundedDifferenceVND int64                           `json:"manual_qr_refunded_difference_vnd"`
	Discrepancies                 []closedDiscrepancyAuditDetails `json:"discrepancies"`
	ApproverStaffIdentityID       *uuid.UUID                      `json:"approver_staff_identity_id"`
}

// normalizedCloseDiscrepancy is one reason entry whose note has been trimmed,
// ready for fingerprinting, validation, and storage.
type normalizedCloseDiscrepancy struct {
	Dimension DiscrepancyDimension
	Reason    DiscrepancyReason
	Note      *string
}

// normalizeCloseDiscrepancies trims every entry's note (an all-whitespace note
// becomes absent), matching the canonical note transform the rest of the slice
// applies before fingerprinting and storage.
func normalizeCloseDiscrepancies(inputs []CloseDiscrepancyInput) []normalizedCloseDiscrepancy {
	normalized := make([]normalizedCloseDiscrepancy, 0, len(inputs))
	for _, input := range inputs {
		normalized = append(normalized, normalizedCloseDiscrepancy{
			Dimension: input.Dimension,
			Reason:    input.Reason,
			Note:      NormalizeNote(input.Note),
		})
	}
	return normalized
}

// CloseShiftHandler closes a reconciled Sales Shift.
type CloseShiftHandler struct{ runner *Runner }

// NewCloseShiftHandler creates a new CloseShiftHandler.
func NewCloseShiftHandler(runner *Runner) *CloseShiftHandler {
	return &CloseShiftHandler{runner: runner}
}

// Handle performs the Final Close of the named CLOSING Shift: the Shift locks
// FOR UPDATE, global blockers re-evaluate, the live source totals re-verify
// against the frozen snapshot, the submitted final attempt ids must be the
// latest evidence, and the three signed differences derive server-side from
// that evidence — never from the request.
//
// The command's discrepancy list selects the operation: a non-null empty list
// closes exactly and any entry closes with a fresh Manager Approval, verified
// inside the transaction before replay. Omitting reasons cannot bypass that
// approval: an exact command meeting a server-derived nonzero difference fails
// on the recount, recheck, or reason rules instead of closing. Exact and
// discrepant closes both return the immutable ClosedShiftDetailResponse,
// including when the initiator is a Cashier.
func (h *CloseShiftHandler) Handle(ctx context.Context, actor Actor, cmd CloseShiftCommand) (int, ClosedShiftDetailResponse, error) {
	// The HTTP boundary rejects a nil (JSON null or omitted) list; a domain
	// caller passing nil closes exactly, which is what an empty list means.
	discrepancies := normalizeCloseDiscrepancies(cmd.Discrepancies)
	spec := closeShiftSpec(cmd, discrepancies)

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, ClosedShiftDetailResponse, AuditRecord, error) {
			detail, audit, err := closeShift(ctx, mc, cmd, spec.Operation, discrepancies)
			if err != nil {
				return 0, ClosedShiftDetailResponse{}, AuditRecord{}, err
			}
			return 200, detail, audit, nil
		})
}

// closeShiftSpec selects the exact or discrepant close operation and builds
// its mutation spec. The discrepant close requires a Manager approval.
func closeShiftSpec(cmd CloseShiftCommand, discrepancies []normalizedCloseDiscrepancy) MutationSpec {
	operation := OpCloseExact
	var approval *ApprovalSpec
	if len(discrepancies) > 0 {
		operation = OpCloseWithDiscrepancy
		// The approver pair is verification input. The HTTP boundary rejects a
		// pair missing either field with 400; a provided but wrong pair is
		// denied by auth.VerifyManagerApproval inside the transaction and
		// collapses to the one 403 code. The approver must hold the MANAGER
		// role and sales_shift.operate.
		approval = &ApprovalSpec{
			ApproverLoginCode:  auth.NormalizeLoginCode(cmd.ApproverLoginCode),
			ManagerPIN:         cmd.ManagerPIN,
			RequiredCapability: CapSalesShiftOperate,
		}
	}

	tuples := make([]closeDiscrepancyFingerprint, 0, len(discrepancies))
	for _, d := range discrepancies {
		// The two structs share identical fields, so the conversion carries
		// the normalized tuple over directly.
		tuples = append(tuples, closeDiscrepancyFingerprint(d))
	}

	return MutationSpec{
		RequestID: cmd.RequestID,
		Operation: operation,
		Fingerprint: closeShiftFingerprint{
			SalesShiftID:         cmd.ShiftID,
			FinalCashCountID:     cmd.FinalCashCountID,
			FinalQRObservationID: cmd.FinalQRObservationID,
			Discrepancies:        tuples,
		},
		Required: []string{CapSalesShiftOperate},
		Approval: approval,
	}
}

// closureDifferences are the three signed differences of a Final Close, each
// derived server-side from the final evidence and the frozen expectations.
type closureDifferences struct {
	cash       int64
	qrReceived int64
	qrRefunded int64
}

func (d closureDifferences) nonzero() bool {
	return d.cash != 0 || d.qrReceived != 0 || d.qrRefunded != 0
}

// verifiedClose is the Final Close state established under the Shift lock:
// the locked Shift, its source-verified frozen snapshot, the final evidence,
// and the differences derived from them.
type verifiedClose struct {
	shift            sqlc.SalesShift
	recon            ReconciliationResponse
	finalCount       CashCountResponse
	finalObservation QRObservationResponse
	diffs            closureDifferences
}

// closeShift is the Final Close mutation body. It runs inside the mutation
// transaction after the idempotency claim; the pipeline writes the returned
// audit record just before the commit.
func closeShift(ctx context.Context, mc MutationContext, cmd CloseShiftCommand,
	operation string, discrepancies []normalizedCloseDiscrepancy,
) (ClosedShiftDetailResponse, AuditRecord, error) {
	vc, err := verifyClose(ctx, mc.Queries, cmd, discrepancies)
	if err != nil {
		return ClosedShiftDetailResponse{}, AuditRecord{}, err
	}

	// A nonzero difference can only reach this point through the discrepant
	// operation, whose fresh Manager Approval the pipeline verified before
	// replay. The approver is therefore absent exactly when all three
	// differences are zero.
	var approverID uuid.NullUUID
	var approver *StaffSummary
	if mc.Approver != nil {
		approverID = uuid.NullUUID{UUID: mc.Approver.ID, Valid: true}
		summary := staffSummaryFromApprover(*mc.Approver)
		approver = &summary
	}

	// The immutable closure aggregate, then exactly the nonzero discrepancy
	// rows, then the one-way state transition, all in this one transaction.
	closure, err := insertClosure(ctx, mc.Queries, mc.Actor, vc, approverID)
	if err != nil {
		return ClosedShiftDetailResponse{}, AuditRecord{}, err
	}
	if err := insertClosureDiscrepancies(ctx, mc.Queries, closure.ID, vc, discrepancies); err != nil {
		return ClosedShiftDetailResponse{}, AuditRecord{}, err
	}
	// There is no reopen.
	if err := mc.Queries.TransitionSalesShiftToClosed(ctx, vc.shift.ID); err != nil {
		return ClosedShiftDetailResponse{}, AuditRecord{}, fmt.Errorf("transition sales shift to closed: %w", err)
	}

	detail, err := loadClosedShiftDetail(ctx, mc.Queries, mc.Actor, vc, closure, approver)
	if err != nil {
		return ClosedShiftDetailResponse{}, AuditRecord{}, err
	}
	return detail, closeAuditRecord(operation, vc, closure, discrepancies, approverID), nil
}

// verifyClose runs every Final Close precondition under the Shift lock, in
// order: lock the Shift, re-evaluate the global blockers, re-verify the frozen
// snapshot against the live sources, resolve the final evidence, derive the
// differences, and check the recount, recheck, and reason rules.
func verifyClose(ctx context.Context, q *sqlc.Queries, cmd CloseShiftCommand,
	discrepancies []normalizedCloseDiscrepancy,
) (verifiedClose, error) {
	// The state branches map an unknown Shift to 404, a CLOSED one to the
	// second-close conflict, and an OPEN one to not-started.
	locked, err := lockClosingShift(ctx, q, cmd.ShiftID)
	if err != nil {
		return verifiedClose{}, err
	}

	// Every global blocker is re-evaluated in one MVCC read. No Check or
	// Session row lock is taken while the Shift lock is held.
	if err := loadClosureBlockers(ctx, q); err != nil {
		return verifiedClose{}, err
	}

	recon, err := loadSourceVerifiedReconciliation(ctx, q, locked.ID)
	if err != nil {
		return verifiedClose{}, err
	}

	finalCount, err := finalAttempt(recon.CashCounts, cmd.FinalCashCountID,
		func(row CashCountResponse) uuid.UUID { return row.ID }, "final_cash_count_id")
	if err != nil {
		return verifiedClose{}, err
	}
	finalObservation, err := finalAttempt(recon.QRObservations, cmd.FinalQRObservationID,
		func(row QRObservationResponse) uuid.UUID { return row.ID }, "final_qr_observation_id")
	if err != nil {
		return verifiedClose{}, err
	}

	diffs, err := deriveClosureDifferences(recon, finalCount, finalObservation)
	if err != nil {
		return verifiedClose{}, err
	}

	// A nonzero difference requires a second relevant attempt. The exact
	// initial evidence may close directly from sequence 1.
	if diffs.cash != 0 && finalCount.Sequence < 2 {
		return verifiedClose{}, ErrCashRecountRequired
	}
	if (diffs.qrReceived != 0 || diffs.qrRefunded != 0) && finalObservation.Sequence < 2 {
		return verifiedClose{}, ErrQRRecheckRequired
	}

	if err := checkDiscrepancyReasons(diffs, discrepancies); err != nil {
		return verifiedClose{}, err
	}

	return verifiedClose{
		shift:            locked,
		recon:            recon,
		finalCount:       finalCount,
		finalObservation: finalObservation,
		diffs:            diffs,
	}, nil
}

// loadSourceVerifiedReconciliation loads the frozen snapshot, reloads every
// live source total, and requires exact equality between them. Expected
// values are derived from the source terms, so comparing the sources covers
// them. A mismatch means an uncoordinated writer or corrupt state, never a
// closure from unexplained numbers.
func loadSourceVerifiedReconciliation(ctx context.Context, q *sqlc.Queries, shiftID uuid.UUID) (ReconciliationResponse, error) {
	recon, err := loadReconciliationSnapshot(ctx, q, shiftID)
	if err != nil {
		return ReconciliationResponse{}, err
	}
	totals, err := q.GetShiftReconciliationTotals(ctx, shiftID)
	if err != nil {
		return ReconciliationResponse{}, fmt.Errorf("reload reconciliation totals: %w", err)
	}
	movements, err := q.SumCashMovements(ctx, shiftID)
	if err != nil {
		return ReconciliationResponse{}, fmt.Errorf("reload cash movement sums: %w", err)
	}
	if movements.PayInVnd != recon.PayInVND ||
		movements.PayOutVnd != recon.PayOutVND ||
		totals.CashPaymentVnd != recon.CashPaymentVND ||
		totals.CashPaymentVoidVnd != recon.CashPaymentVoidVND ||
		totals.CashRefundVnd != recon.CashRefundVND ||
		totals.ManualQrPaymentVnd != recon.ManualQRPaymentVND ||
		totals.ManualQrPaymentVoidVnd != recon.ManualQRPaymentVoidVND ||
		totals.ManualQrRefundVnd != recon.ManualQRRefundVND {
		return ReconciliationResponse{}, ErrReconciliationSourceChanged
	}
	return recon, nil
}

// finalAttempt returns the ledger row a close submitted as its final attempt,
// which must be the latest row of its ledger. An id absent from the ledger is
// an unknown attempt id (404); a present but superseded one is stale. The
// append ledgers order by sequence, so the last row is the latest.
func finalAttempt[T any](ledger []T, id uuid.UUID, idOf func(T) uuid.UUID, field string) (T, error) {
	var final, zero T
	found := false
	for _, row := range ledger {
		if idOf(row) == id {
			final, found = row, true
		}
	}
	if !found {
		return zero, fmt.Errorf("%w: %s", ErrReconciliationAttemptNotFound, field)
	}
	if idOf(final) != idOf(ledger[len(ledger)-1]) {
		return zero, ErrReconciliationStale
	}
	return final, nil
}

// deriveClosureDifferences derives the three signed differences from the
// final evidence and the frozen expectations through the guarded arithmetic.
// No amount crosses the boundary from the request, so a wrap is corrupt data,
// not client input.
func deriveClosureDifferences(recon ReconciliationResponse, finalCount CashCountResponse,
	finalObservation QRObservationResponse,
) (closureDifferences, error) {
	cash, err := ComputeDifference(finalCount.CountedCashVND, recon.ExpectedCashVND)
	if err != nil {
		return closureDifferences{},
			fmt.Errorf("compute cash difference: %w: %w", errReconciliationCalculationFailed, err)
	}
	qrReceived, err := ComputeDifference(
		finalObservation.ObservedReceivedVND, recon.ExpectedManualQRReceivedVND)
	if err != nil {
		return closureDifferences{},
			fmt.Errorf("compute manual QR received difference: %w: %w", errReconciliationCalculationFailed, err)
	}
	qrRefunded, err := ComputeDifference(
		finalObservation.ObservedRefundedVND, recon.ManualQRRefundVND)
	if err != nil {
		return closureDifferences{},
			fmt.Errorf("compute manual QR refunded difference: %w: %w", errReconciliationCalculationFailed, err)
	}
	return closureDifferences{cash: cash, qrReceived: qrReceived, qrRefunded: qrRefunded}, nil
}

// checkDiscrepancyReasons requires the reason set to match the server-derived
// differences exactly: every nonzero dimension carries exactly one entry and
// every zero dimension none.
func checkDiscrepancyReasons(diffs closureDifferences, discrepancies []normalizedCloseDiscrepancy) error {
	entryCounts := make(map[DiscrepancyDimension]int, len(discrepancies))
	for _, d := range discrepancies {
		entryCounts[d.Dimension]++
	}
	for _, dimension := range []struct {
		name       DiscrepancyDimension
		difference int64
	}{
		{DimensionCash, diffs.cash},
		{DimensionManualQRReceived, diffs.qrReceived},
		{DimensionManualQRRefunded, diffs.qrRefunded},
	} {
		if dimension.difference != 0 {
			if entryCounts[dimension.name] != 1 {
				return fmt.Errorf("%w: dimension %s", ErrDiscrepancyReasonRequired, dimension.name)
			}
		} else if entryCounts[dimension.name] > 0 {
			return fmt.Errorf("%w: dimension %s", ErrDiscrepancyReasonUnexpected, dimension.name)
		}
	}
	// The HTTP boundary already enforced the reason and note shapes, so a
	// residual failure here is a reason-to-dimension pairing the catalog
	// forbids — the stable unexpected-reason conflict rather than a body
	// error.
	for _, d := range discrepancies {
		if err := ValidateDiscrepancyReason(string(d.Dimension), string(d.Reason), d.Note); err != nil {
			return fmt.Errorf("%w: %s", ErrDiscrepancyReasonUnexpected, err.Error())
		}
	}
	return nil
}

// insertClosure writes the immutable closure aggregate.
func insertClosure(ctx context.Context, q *sqlc.Queries, actor Actor, vc verifiedClose,
	approverID uuid.NullUUID,
) (sqlc.InsertShiftClosureRow, error) {
	closure, err := q.InsertShiftClosure(ctx, sqlc.InsertShiftClosureParams{
		SalesShiftID:                  vc.shift.ID,
		ReconciliationID:              vc.recon.ID,
		InitialCashCountID:            vc.recon.CashCounts[0].ID,
		FinalCashCountID:              vc.finalCount.ID,
		FinalQrObservationID:          vc.finalObservation.ID,
		OpenerStaffIdentityID:         vc.shift.OpenedByStaffIdentityID,
		CloserStaffIdentityID:         actor.StaffID,
		CloserStaffAccessSessionID:    actor.SessionID,
		ApprovedByStaffIdentityID:     approverID,
		OpenedAt:                      vc.shift.OpenedAt,
		OpeningFloatVnd:               vc.recon.OpeningFloatVND,
		PayInVnd:                      vc.recon.PayInVND,
		PayOutVnd:                     vc.recon.PayOutVND,
		CashPaymentVnd:                vc.recon.CashPaymentVND,
		CashPaymentVoidVnd:            vc.recon.CashPaymentVoidVND,
		CashRefundVnd:                 vc.recon.CashRefundVND,
		ExpectedCashVnd:               vc.recon.ExpectedCashVND,
		ManualQrPaymentVnd:            vc.recon.ManualQRPaymentVND,
		ManualQrPaymentVoidVnd:        vc.recon.ManualQRPaymentVoidVND,
		ExpectedManualQrReceivedVnd:   vc.recon.ExpectedManualQRReceivedVND,
		ManualQrRefundVnd:             vc.recon.ManualQRRefundVND,
		ObservedCashVnd:               vc.finalCount.CountedCashVND,
		ObservedManualQrReceivedVnd:   vc.finalObservation.ObservedReceivedVND,
		ObservedManualQrRefundedVnd:   vc.finalObservation.ObservedRefundedVND,
		CashDifferenceVnd:             vc.diffs.cash,
		ManualQrReceivedDifferenceVnd: vc.diffs.qrReceived,
		ManualQrRefundedDifferenceVnd: vc.diffs.qrRefunded,
	})
	if err != nil {
		return sqlc.InsertShiftClosureRow{}, MapDBError(err)
	}
	return closure, nil
}

// dimensionAmounts returns one closure dimension's expected, observed, and
// difference amounts.
func (vc verifiedClose) dimensionAmounts(dimension DiscrepancyDimension) (expected, observed, difference int64) {
	switch dimension {
	case DimensionCash:
		return vc.recon.ExpectedCashVND, vc.finalCount.CountedCashVND, vc.diffs.cash
	case DimensionManualQRReceived:
		return vc.recon.ExpectedManualQRReceivedVND, vc.finalObservation.ObservedReceivedVND, vc.diffs.qrReceived
	case DimensionManualQRRefunded:
		return vc.recon.ManualQRRefundVND, vc.finalObservation.ObservedRefundedVND, vc.diffs.qrRefunded
	}
	return 0, 0, 0
}

// insertClosureDiscrepancies stores one discrepancy row per reason entry. An
// exact close has no entries and writes nothing.
func insertClosureDiscrepancies(ctx context.Context, q *sqlc.Queries, closureID uuid.UUID,
	vc verifiedClose, discrepancies []normalizedCloseDiscrepancy,
) error {
	if len(discrepancies) == 0 {
		return nil
	}
	dimensions := make([]string, 0, len(discrepancies))
	expecteds := make([]int64, 0, len(discrepancies))
	observeds := make([]int64, 0, len(discrepancies))
	differences := make([]int64, 0, len(discrepancies))
	reasons := make([]string, 0, len(discrepancies))
	notes := make([]string, 0, len(discrepancies))
	// The reason-set check guarantees exactly one entry per nonzero dimension
	// and none per zero dimension, so each entry's amounts are the amounts of
	// its own dimension.
	for _, entry := range discrepancies {
		expected, observed, difference := vc.dimensionAmounts(entry.Dimension)
		dimensions = append(dimensions, string(entry.Dimension))
		expecteds = append(expecteds, expected)
		observeds = append(observeds, observed)
		differences = append(differences, difference)
		reasons = append(reasons, string(entry.Reason))
		if entry.Note != nil {
			notes = append(notes, *entry.Note)
		} else {
			// An empty Go string stores SQL NULL through the query's
			// NULLIF(btrim(note), '') transform.
			notes = append(notes, "")
		}
	}
	if err := q.InsertShiftDiscrepancies(ctx, sqlc.InsertShiftDiscrepanciesParams{
		ShiftClosureID: closureID,
		Dimensions:     dimensions,
		Expecteds:      expecteds,
		Observeds:      observeds,
		Differences:    differences,
		Reasons:        reasons,
		Notes:          notes,
	}); err != nil {
		return MapDBError(err)
	}
	return nil
}

// loadClosedShiftDetail builds the immutable close response. It reuses the
// frozen snapshot's loaded attempts and starter, plus the stored discrepancy
// rows with their database-assigned ids and creation times.
func loadClosedShiftDetail(ctx context.Context, q *sqlc.Queries, actor Actor, vc verifiedClose,
	closure sqlc.InsertShiftClosureRow, approver *StaffSummary,
) (ClosedShiftDetailResponse, error) {
	discrepancies, err := loadClosedDiscrepancies(ctx, q, closure.ID)
	if err != nil {
		return ClosedShiftDetailResponse{}, err
	}
	opener, err := q.GetStaffSummary(ctx, vc.shift.OpenedByStaffIdentityID)
	if err != nil {
		return ClosedShiftDetailResponse{}, fmt.Errorf("load opener summary: %w", err)
	}
	closer, err := q.GetStaffSummary(ctx, actor.StaffID)
	if err != nil {
		return ClosedShiftDetailResponse{}, fmt.Errorf("load closer summary: %w", err)
	}

	recon := vc.recon
	return ClosedShiftDetailResponse{
		ClosedShiftSummaryResponse: ClosedShiftSummaryResponse{
			ID:       vc.shift.ID,
			Opener:   staffSummaryFromRow(opener),
			Closer:   staffSummaryFromRow(closer),
			OpenedAt: vc.shift.OpenedAt,
			ClosedAt: closure.ClosedAt,

			OpeningFloatVND:   recon.OpeningFloatVND,
			ExpectedCashVND:   recon.ExpectedCashVND,
			ObservedCashVND:   vc.finalCount.CountedCashVND,
			CashDifferenceVND: vc.diffs.cash,

			ExpectedManualQRReceivedVND:   recon.ExpectedManualQRReceivedVND,
			ObservedManualQRReceivedVND:   vc.finalObservation.ObservedReceivedVND,
			ManualQRReceivedDifferenceVND: vc.diffs.qrReceived,

			ExpectedManualQRRefundedVND:   recon.ManualQRRefundVND,
			ObservedManualQRRefundedVND:   vc.finalObservation.ObservedRefundedVND,
			ManualQRRefundedDifferenceVND: vc.diffs.qrRefunded,

			HasDiscrepancy: vc.diffs.nonzero(),
		},
		Starter:                         recon.Starter,
		PayInVND:                        recon.PayInVND,
		PayOutVND:                       recon.PayOutVND,
		CashPaymentVND:                  recon.CashPaymentVND,
		CashPaymentVoidVND:              recon.CashPaymentVoidVND,
		CashRefundVND:                   recon.CashRefundVND,
		ManualQRPaymentVND:              recon.ManualQRPaymentVND,
		ManualQRPaymentVoidVND:          recon.ManualQRPaymentVoidVND,
		PendingManualQRRefundVND:        recon.PendingManualQRRefundVND,
		PendingRefundVND:                recon.PendingRefundVND,
		UnresolvedPostSaleAdjustmentVND: recon.UnresolvedPostSaleAdjustmentVND,
		CashCounts:                      recon.CashCounts,
		QRObservations:                  recon.QRObservations,
		Discrepancies:                   discrepancies,
		Approver:                        approver,
	}, nil
}

// closeAuditRecord is the exact or discrepant closure audit event.
func closeAuditRecord(operation string, vc verifiedClose, closure sqlc.InsertShiftClosureRow,
	discrepancies []normalizedCloseDiscrepancy, approverID uuid.NullUUID,
) AuditRecord {
	eventType := EventShiftClosedExact
	if operation == OpCloseWithDiscrepancy {
		eventType = EventShiftClosedWithDiscrepancy
	}
	reasons := make([]closedDiscrepancyAuditDetails, 0, len(discrepancies))
	for _, d := range discrepancies {
		reasons = append(reasons, closedDiscrepancyAuditDetails{
			Dimension: d.Dimension, Reason: d.Reason,
		})
	}
	var approver *uuid.UUID
	if approverID.Valid {
		id := approverID.UUID
		approver = &id
	}
	return AuditRecord{
		EventType: eventType,
		Details: closedShiftAuditDetails{
			SalesShiftID:                  vc.shift.ID,
			ReconciliationID:              vc.recon.ID,
			ClosureID:                     closure.ID,
			FinalCashCountID:              vc.finalCount.ID,
			FinalQRObservationID:          vc.finalObservation.ID,
			CashDifferenceVND:             vc.diffs.cash,
			ManualQRReceivedDifferenceVND: vc.diffs.qrReceived,
			ManualQRRefundedDifferenceVND: vc.diffs.qrRefunded,
			Discrepancies:                 reasons,
			ApproverStaffIdentityID:       approver,
		},
	}
}
