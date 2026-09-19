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
// The approver login and the PIN are deliberately absent (spec 9.4): the PIN
// is a secret and the approver identity is verification input, not business
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

// closedShiftAuditDetails carries the closure facts (spec 13): the snapshot
// ids, the final evidence ids, all three differences, the discrepancy reasons,
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

// Handle performs the Final Close of the named CLOSING Shift (spec 4.4): the
// Shift locks FOR UPDATE, global blockers re-evaluate, the live source totals
// re-verify against the frozen snapshot, the submitted final attempt ids must
// be the latest evidence, and the three signed differences derive server-side
// from that evidence — never from the request.
//
// The command's discrepancy list selects the operation: a non-null empty list
// closes exactly and any entry closes with a fresh Manager Approval, verified
// inside the transaction before replay. Omitting reasons cannot bypass that
// approval: an exact command meeting a server-derived nonzero difference fails
// on the recount, recheck, or reason rules instead of closing. Exact and
// discrepant closes both return the immutable ClosedShiftDetailResponse,
// including when the initiator is a Cashier (spec 9.6).
func (h *CloseShiftHandler) Handle(ctx context.Context, actor Actor, cmd CloseShiftCommand) (int, ClosedShiftDetailResponse, error) {
	// The HTTP boundary rejects a nil (JSON null or omitted) list; a domain
	// caller passing nil closes exactly, which is what an empty list means.
	discrepancies := normalizeCloseDiscrepancies(cmd.Discrepancies)

	operation := OpCloseExact
	var approval *ApprovalSpec
	if len(discrepancies) > 0 {
		operation = OpCloseWithDiscrepancy
		// The approver pair is verification input: a missing or wrong pair is
		// denied by auth.VerifyManagerApproval inside the transaction and
		// collapses to the one 403 code (spec 10, 12, ADR-048). The approver
		// must hold the MANAGER role and sales_shift.operate.
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

	spec := MutationSpec{
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

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, ClosedShiftDetailResponse, AuditRecord, error) {
			var zero ClosedShiftDetailResponse

			// Step 2: lock the named Shift FOR UPDATE (spec 11.1). The state
			// branches map an unknown Shift to 404, a CLOSED one to the
			// second-close conflict, and an OPEN one to not-started.
			locked, err := lockClosingShift(ctx, mc.Queries, cmd.ShiftID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}

			// Step 3: re-evaluate every global blocker in one MVCC read,
			// rejected in spec 8's precedence. No Check or Session row lock is
			// taken while the Shift lock is held (spec 11.1).
			if err := loadClosureBlockers(ctx, mc.Queries); err != nil {
				return 0, zero, AuditRecord{}, err
			}

			// Step 4: reload every source total and require exact equality
			// with the frozen snapshot (spec 11.3). Expected values are
			// derived from the source terms, so comparing the sources covers
			// them. A mismatch means an uncoordinated writer or corrupt state,
			// never a closure from unexplained numbers.
			recon, err := loadReconciliationSnapshot(ctx, mc.Queries, locked.ID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			totals, err := mc.Queries.GetShiftReconciliationTotals(ctx, locked.ID)
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("reload reconciliation totals: %w", err)
			}
			movements, err := mc.Queries.SumCashMovements(ctx, locked.ID)
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("reload cash movement sums: %w", err)
			}
			if movements.PayInVnd != recon.PayInVND ||
				movements.PayOutVnd != recon.PayOutVND ||
				totals.CashPaymentVnd != recon.CashPaymentVND ||
				totals.CashPaymentVoidVnd != recon.CashPaymentVoidVND ||
				totals.CashRefundVnd != recon.CashRefundVND ||
				totals.ManualQrPaymentVnd != recon.ManualQRPaymentVND ||
				totals.ManualQrPaymentVoidVnd != recon.ManualQRPaymentVoidVND ||
				totals.ManualQrRefundVnd != recon.ManualQRRefundVND {
				return 0, zero, AuditRecord{}, ErrReconciliationSourceChanged
			}

			// Step 5: the submitted final attempt ids must be the latest rows
			// of their ledgers (spec 7.4). An id absent from the ledger is an
			// unknown attempt id (404, spec 12); a present but superseded one
			// is stale (spec 7.11). The append ledgers order by sequence, so
			// the last row is the latest.
			var finalCount CashCountResponse
			countFound := false
			for _, row := range recon.CashCounts {
				if row.ID == cmd.FinalCashCountID {
					finalCount, countFound = row, true
				}
			}
			if !countFound {
				return 0, zero, AuditRecord{},
					fmt.Errorf("%w: final_cash_count_id", ErrReconciliationAttemptNotFound)
			}
			if finalCount.ID != recon.CashCounts[len(recon.CashCounts)-1].ID {
				return 0, zero, AuditRecord{}, ErrReconciliationStale
			}

			var finalObservation QRObservationResponse
			observationFound := false
			for _, row := range recon.QRObservations {
				if row.ID == cmd.FinalQRObservationID {
					finalObservation, observationFound = row, true
				}
			}
			if !observationFound {
				return 0, zero, AuditRecord{},
					fmt.Errorf("%w: final_qr_observation_id", ErrReconciliationAttemptNotFound)
			}
			if finalObservation.ID != recon.QRObservations[len(recon.QRObservations)-1].ID {
				return 0, zero, AuditRecord{}, ErrReconciliationStale
			}

			// Step 6: derive the three signed differences from the final
			// evidence and the frozen expectations through the guarded
			// arithmetic (spec 6). No amount crosses the boundary from the
			// request, so a wrap is corrupt data, not client input (spec 12).
			cashDifference, err := ComputeDifference(finalCount.CountedCashVND, recon.ExpectedCashVND)
			if err != nil {
				return 0, zero, AuditRecord{},
					fmt.Errorf("compute cash difference: %w: %w", errReconciliationCalculationFailed, err)
			}
			qrReceivedDifference, err := ComputeDifference(
				finalObservation.ObservedReceivedVND, recon.ExpectedManualQRReceivedVND)
			if err != nil {
				return 0, zero, AuditRecord{},
					fmt.Errorf("compute manual QR received difference: %w: %w", errReconciliationCalculationFailed, err)
			}
			qrRefundedDifference, err := ComputeDifference(
				finalObservation.ObservedRefundedVND, recon.ManualQRRefundVND)
			if err != nil {
				return 0, zero, AuditRecord{},
					fmt.Errorf("compute manual QR refunded difference: %w: %w", errReconciliationCalculationFailed, err)
			}

			// Step 7a: a nonzero difference requires a second relevant attempt
			// (spec 5.3, 5.4, 7.5, 7.6). The exact initial evidence may close
			// directly from sequence 1.
			if cashDifference != 0 && finalCount.Sequence < 2 {
				return 0, zero, AuditRecord{}, ErrCashRecountRequired
			}
			if (qrReceivedDifference != 0 || qrRefundedDifference != 0) && finalObservation.Sequence < 2 {
				return 0, zero, AuditRecord{}, ErrQRRecheckRequired
			}

			// Step 7b: the reason set must match the server-derived
			// differences exactly — every nonzero dimension carries exactly one
			// entry and every zero dimension none (spec 7.7, 7.8).
			entryCounts := make(map[DiscrepancyDimension]int, len(discrepancies))
			for _, d := range discrepancies {
				entryCounts[d.Dimension]++
			}
			for _, dimension := range []struct {
				name       DiscrepancyDimension
				difference int64
			}{
				{DimensionCash, cashDifference},
				{DimensionManualQRReceived, qrReceivedDifference},
				{DimensionManualQRRefunded, qrRefundedDifference},
			} {
				if dimension.difference != 0 {
					if entryCounts[dimension.name] != 1 {
						return 0, zero, AuditRecord{},
							fmt.Errorf("%w: dimension %s", ErrDiscrepancyReasonRequired, dimension.name)
					}
				} else if entryCounts[dimension.name] > 0 {
					return 0, zero, AuditRecord{},
						fmt.Errorf("%w: dimension %s", ErrDiscrepancyReasonUnexpected, dimension.name)
				}
			}
			// The HTTP boundary already enforced the reason and note shapes,
			// so a residual failure here is a reason-to-dimension pairing the
			// catalog forbids (spec 5.6) — the stable unexpected-reason
			// conflict rather than a body error.
			for _, d := range discrepancies {
				if err := ValidateDiscrepancyReason(string(d.Dimension), string(d.Reason), d.Note); err != nil {
					return 0, zero, AuditRecord{},
						fmt.Errorf("%w: %s", ErrDiscrepancyReasonUnexpected, err.Error())
				}
			}

			// Step 7c: any nonzero difference can only reach this point through
			// the discrepant operation, whose fresh Manager Approval the
			// executor verified before replay (spec 10, ADR-048, ADR-009). The
			// approver is nullable exactly when all three differences are zero.
			var approverID uuid.NullUUID
			var approver *StaffSummary
			if mc.Approver != nil {
				approverID = uuid.NullUUID{UUID: mc.Approver.ID, Valid: true}
				summary := staffSummaryFromApprover(*mc.Approver)
				approver = &summary
			}

			// Step 8: the immutable closure aggregate, then exactly the nonzero
			// discrepancy rows, all in this one transaction (spec 5.5, 5.6).
			closure, err := mc.Queries.InsertShiftClosure(ctx, sqlc.InsertShiftClosureParams{
				SalesShiftID:                  locked.ID,
				ReconciliationID:              recon.ID,
				InitialCashCountID:            recon.CashCounts[0].ID,
				FinalCashCountID:              finalCount.ID,
				FinalQrObservationID:          finalObservation.ID,
				OpenerStaffIdentityID:         locked.OpenedByStaffIdentityID,
				CloserStaffIdentityID:         actor.StaffID,
				CloserStaffAccessSessionID:    actor.SessionID,
				ApprovedByStaffIdentityID:     approverID,
				OpenedAt:                      locked.OpenedAt,
				OpeningFloatVnd:               recon.OpeningFloatVND,
				PayInVnd:                      recon.PayInVND,
				PayOutVnd:                     recon.PayOutVND,
				CashPaymentVnd:                recon.CashPaymentVND,
				CashPaymentVoidVnd:            recon.CashPaymentVoidVND,
				CashRefundVnd:                 recon.CashRefundVND,
				ExpectedCashVnd:               recon.ExpectedCashVND,
				ManualQrPaymentVnd:            recon.ManualQRPaymentVND,
				ManualQrPaymentVoidVnd:        recon.ManualQRPaymentVoidVND,
				ExpectedManualQrReceivedVnd:   recon.ExpectedManualQRReceivedVND,
				ManualQrRefundVnd:             recon.ManualQRRefundVND,
				ObservedCashVnd:               finalCount.CountedCashVND,
				ObservedManualQrReceivedVnd:   finalObservation.ObservedReceivedVND,
				ObservedManualQrRefundedVnd:   finalObservation.ObservedRefundedVND,
				CashDifferenceVnd:             cashDifference,
				ManualQrReceivedDifferenceVnd: qrReceivedDifference,
				ManualQrRefundedDifferenceVnd: qrRefundedDifference,
			})
			if err != nil {
				return 0, zero, AuditRecord{}, MapDBError(err)
			}

			if len(discrepancies) > 0 {
				dimensions := make([]string, 0, len(discrepancies))
				expecteds := make([]int64, 0, len(discrepancies))
				observeds := make([]int64, 0, len(discrepancies))
				differences := make([]int64, 0, len(discrepancies))
				reasons := make([]string, 0, len(discrepancies))
				notes := make([]string, 0, len(discrepancies))
				// The reason-set check above guarantees exactly one entry per
				// nonzero dimension and none per zero dimension, so each
				// entry's amounts are the amounts of its own dimension.
				for _, entry := range discrepancies {
					var expected, observed, difference int64
					switch entry.Dimension {
					case DimensionCash:
						expected, observed, difference =
							recon.ExpectedCashVND, finalCount.CountedCashVND, cashDifference
					case DimensionManualQRReceived:
						expected, observed, difference =
							recon.ExpectedManualQRReceivedVND,
							finalObservation.ObservedReceivedVND, qrReceivedDifference
					case DimensionManualQRRefunded:
						expected, observed, difference =
							recon.ManualQRRefundVND,
							finalObservation.ObservedRefundedVND, qrRefundedDifference
					}
					dimensions = append(dimensions, string(entry.Dimension))
					expecteds = append(expecteds, expected)
					observeds = append(observeds, observed)
					differences = append(differences, difference)
					reasons = append(reasons, string(entry.Reason))
					if entry.Note != nil {
						notes = append(notes, *entry.Note)
					} else {
						// An empty Go string stores SQL NULL through the
						// query's NULLIF(btrim(note), '') transform.
						notes = append(notes, "")
					}
				}
				if err := mc.Queries.InsertShiftDiscrepancies(ctx, sqlc.InsertShiftDiscrepanciesParams{
					ShiftClosureID: closure.ID,
					Dimensions:     dimensions,
					Expecteds:      expecteds,
					Observeds:      observeds,
					Differences:    differences,
					Reasons:        reasons,
					Notes:          notes,
				}); err != nil {
					return 0, zero, AuditRecord{}, MapDBError(err)
				}
			}

			// Step 9: the one-way state transition. There is no reopen.
			if err := mc.Queries.TransitionSalesShiftToClosed(ctx, locked.ID); err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("transition sales shift to closed: %w", err)
			}

			// Step 10: the exact or discrepant closure audit event, written by
			// the executor just before the commit (spec 13).
			hasDiscrepancy := cashDifference != 0 || qrReceivedDifference != 0 || qrRefundedDifference != 0
			eventType := EventShiftClosedExact
			if operation == OpCloseWithDiscrepancy {
				eventType = EventShiftClosedWithDiscrepancy
			}
			auditReasons := make([]closedDiscrepancyAuditDetails, 0, len(discrepancies))
			for _, d := range discrepancies {
				auditReasons = append(auditReasons, closedDiscrepancyAuditDetails{
					Dimension: d.Dimension, Reason: d.Reason,
				})
			}
			var auditApprover *uuid.UUID
			if approverID.Valid {
				id := approverID.UUID
				auditApprover = &id
			}

			// The immutable detail reuses the frozen snapshot's loaded attempts
			// and starter, plus the stored discrepancy rows with their
			// database-assigned ids and creation times (spec 9.6).
			storedDiscrepancies, err := mc.Queries.ListShiftClosureDiscrepancies(ctx, closure.ID)
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("list closure discrepancies: %w", err)
			}
			discrepancyResponses := make([]DiscrepancyResponse, 0, len(storedDiscrepancies))
			for _, row := range storedDiscrepancies {
				var note *string
				if row.Note.Valid {
					note = &row.Note.String
				}
				discrepancyResponses = append(discrepancyResponses, DiscrepancyResponse{
					Dimension:     DiscrepancyDimension(row.Dimension),
					ExpectedVND:   row.ExpectedVnd,
					ObservedVND:   row.ObservedVnd,
					DifferenceVND: row.DifferenceVnd,
					Reason:        DiscrepancyReason(row.Reason),
					Note:          note,
					CreatedAt:     row.CreatedAt,
				})
			}

			opener, err := mc.Queries.GetStaffSummary(ctx, locked.OpenedByStaffIdentityID)
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("load opener summary: %w", err)
			}
			closer, err := mc.Queries.GetStaffSummary(ctx, actor.StaffID)
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("load closer summary: %w", err)
			}

			detail := ClosedShiftDetailResponse{
				ClosedShiftSummaryResponse: ClosedShiftSummaryResponse{
					ID:       locked.ID,
					Opener:   staffSummaryFromRow(opener),
					Closer:   staffSummaryFromRow(closer),
					OpenedAt: locked.OpenedAt,
					ClosedAt: closure.ClosedAt,

					OpeningFloatVND:   recon.OpeningFloatVND,
					ExpectedCashVND:   recon.ExpectedCashVND,
					ObservedCashVND:   finalCount.CountedCashVND,
					CashDifferenceVND: cashDifference,

					ExpectedManualQRReceivedVND:   recon.ExpectedManualQRReceivedVND,
					ObservedManualQRReceivedVND:   finalObservation.ObservedReceivedVND,
					ManualQRReceivedDifferenceVND: qrReceivedDifference,

					ExpectedManualQRRefundedVND:   recon.ManualQRRefundVND,
					ObservedManualQRRefundedVND:   finalObservation.ObservedRefundedVND,
					ManualQRRefundedDifferenceVND: qrRefundedDifference,

					HasDiscrepancy: hasDiscrepancy,
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
				Discrepancies:                   discrepancyResponses,
				Approver:                        approver,
			}

			return 200, detail, AuditRecord{
				EventType: eventType,
				Details: closedShiftAuditDetails{
					SalesShiftID:                  locked.ID,
					ReconciliationID:              recon.ID,
					ClosureID:                     closure.ID,
					FinalCashCountID:              finalCount.ID,
					FinalQRObservationID:          finalObservation.ID,
					CashDifferenceVND:             cashDifference,
					ManualQRReceivedDifferenceVND: qrReceivedDifference,
					ManualQRRefundedDifferenceVND: qrRefundedDifference,
					Discrepancies:                 auditReasons,
					ApproverStaffIdentityID:       auditApprover,
				},
			}, nil
		})
}
