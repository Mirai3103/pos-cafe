package preparation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/platform/database/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// Fixed Charge Adjustment and Check literals this command writes. The shared
// sqlc package is a mechanical adapter; these domain values are restated here
// rather than imported from internal/sales across the ADR-024 boundary.
const (
	chargeAdjustmentKindCancellation = "CANCELLATION"
	chargeAdjustmentScopeLiveCheck   = "LIVE_CHECK"
	stateCheckOpen                   = "OPEN"
)

// Database constraint names this command maps to a domain condition. Only the
// constraints that represent a documented concurrent race are mapped; every
// other violation stays an infrastructure failure.
const (
	constraintChargeAdjustmentKindUnitUnique    = "charge_adjustment_kind_unit_unique"
	constraintPreparationCancellationUnitUnique = "preparation_cancellation_unit_unique"
)

// mapCancelDBError maps exactly the named unique constraints to the typed
// conditions they represent: a unit that already carries a CANCELLATION
// adjustment is a correction conflict, and a unit that already carries a
// Cancellation fact is no longer a queued source. Every other PostgreSQL
// error — including every other unique violation — is returned untouched.
func mapCancelDBError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		switch pgErr.ConstraintName {
		case constraintChargeAdjustmentKindUnitUnique:
			return fmt.Errorf("%w: the unit already carries a %s adjustment",
				ErrChargeAdjustmentConflict, chargeAdjustmentKindCancellation)
		case constraintPreparationCancellationUnitUnique:
			return fmt.Errorf("%w: the unit already carries a cancellation",
				ErrCancellationSourceNotQueued)
		}
	}
	return err
}

// sumCancellationAmounts adds the immutable per-unit prices a batch removes
// from one Check with an explicit overflow guard. An amount that is not
// positive is a corrupt resolved price (an invariant), while an overflow is a
// range failure caused by the request's own size.
func sumCancellationAmounts(amounts []int64) (int64, error) {
	var total int64
	for _, amount := range amounts {
		if amount <= 0 {
			return 0, fmt.Errorf("%w: unit price %d is not positive",
				ErrChargeInvariantViolated, amount)
		}
		if total > math.MaxInt64-amount {
			return 0, fmt.Errorf("%w: %d + %d overflows",
				ErrCancellationChargeOutOfRange, total, amount)
		}
		total += amount
	}
	return total, nil
}

// cancellationNewCharge reduces a Check's stored charge by the adjustment
// total planned for it. A removal larger than the stored charge means the
// stored charge never covered the units, which is an invariant, never a
// business conflict.
func cancellationNewCharge(storedChargeVND, removedVND int64) (int64, error) {
	if storedChargeVND < 0 || removedVND < 0 {
		return 0, fmt.Errorf("%w: stored charge %d, removed %d",
			ErrChargeInvariantViolated, storedChargeVND, removedVND)
	}
	if removedVND > storedChargeVND {
		return 0, fmt.Errorf("%w: removing %d from stored charge %d",
			ErrChargeInvariantViolated, removedVND, storedChargeVND)
	}
	return storedChargeVND - removedVND, nil
}

// cancellationEffectiveReceived derives the corrected receipt: valid Payments
// less completed live Refunds. A completed Refund above the valid receipt is a
// corrupt stored result.
func cancellationEffectiveReceived(validPaymentVND, completedRefundVND int64) (int64, error) {
	if validPaymentVND < 0 || completedRefundVND < 0 {
		return 0, fmt.Errorf("%w: valid payment %d, completed refund %d",
			ErrChargeInvariantViolated, validPaymentVND, completedRefundVND)
	}
	if completedRefundVND > validPaymentVND {
		return 0, fmt.Errorf("%w: completed refunds %d exceed valid receipt %d",
			ErrChargeInvariantViolated, completedRefundVND, validPaymentVND)
	}
	return validPaymentVND - completedRefundVND, nil
}

// cancelCheckEquation verifies the live invariant the read path shares:
// stored charge = base allocations - live adjustments. A disagreement is a
// defect, not a business state.
func cancelCheckEquation(baseChargeVND, liveAdjustmentVND, storedChargeVND int64) error {
	if baseChargeVND < 0 || liveAdjustmentVND < 0 || storedChargeVND < 0 {
		return fmt.Errorf("%w: base %d, live adjustments %d, stored %d",
			ErrChargeInvariantViolated, baseChargeVND, liveAdjustmentVND, storedChargeVND)
	}
	if liveAdjustmentVND > baseChargeVND {
		return fmt.Errorf("%w: live adjustments %d exceed base charge %d",
			ErrChargeInvariantViolated, liveAdjustmentVND, baseChargeVND)
	}
	if expected := baseChargeVND - liveAdjustmentVND; expected != storedChargeVND {
		return fmt.Errorf("%w: stored %d, base %d, live adjustments %d",
			ErrChargeInvariantViolated, storedChargeVND, baseChargeVND, liveAdjustmentVND)
	}
	return nil
}

// cancelCheckPlan is one affected Check's planned correction: the stored
// charge before the batch, the corrected charge after it, and the receipt
// side used to decide settlement.
type cancelCheckPlan struct {
	CheckID              uuid.UUID
	State                string
	StoredChargeVND      int64
	NewChargeVND         int64
	EffectiveReceivedVND int64
}

// planCancelCheckCharges verifies every affected Check's live invariant and
// plans its corrected charge from the immutable per-unit prices the batch
// removes.
func planCancelCheckCharges(ctx context.Context, q *sqlc.Queries,
	checkIDs []uuid.UUID, sessionID uuid.UUID,
	resolved map[uuid.UUID]cancelResolvedUnit, ids []uuid.UUID,
) (map[uuid.UUID]cancelCheckPlan, error) {
	amountsByCheck := make(map[uuid.UUID][]int64, len(checkIDs))
	for _, id := range ids {
		unit := resolved[id]
		if !unit.charged() {
			continue
		}
		amountsByCheck[unit.CheckID] = append(amountsByCheck[unit.CheckID], unit.UnitPriceVND)
	}

	plans := make(map[uuid.UUID]cancelCheckPlan, len(checkIDs))
	for _, checkID := range checkIDs {
		financials, err := q.GetPreparationCheckFinancials(ctx, checkID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, fmt.Errorf("%w: check %s does not exist",
					ErrChargeInvariantViolated, checkID)
			}
			return nil, fmt.Errorf("read check financials: %w", err)
		}
		if financials.ID != checkID || financials.ServiceSessionID != sessionID {
			return nil, fmt.Errorf(
				"%w: check %s resolved session %s, expected %s",
				ErrChargeInvariantViolated, checkID, financials.ServiceSessionID, sessionID)
		}
		if err := cancelCheckEquation(financials.BaseChargeVnd,
			financials.LiveAdjustmentVnd, financials.ChargeVnd); err != nil {
			return nil, err
		}
		effectiveReceived, err := cancellationEffectiveReceived(
			financials.ValidPaymentVnd, financials.CompletedRefundVnd)
		if err != nil {
			return nil, err
		}
		removed, err := sumCancellationAmounts(amountsByCheck[checkID])
		if err != nil {
			return nil, err
		}
		newCharge, err := cancellationNewCharge(financials.ChargeVnd, removed)
		if err != nil {
			return nil, err
		}
		plans[checkID] = cancelCheckPlan{
			CheckID:              checkID,
			State:                financials.State,
			StoredChargeVND:      financials.ChargeVnd,
			NewChargeVND:         newCharge,
			EffectiveReceivedVND: effectiveReceived,
		}
	}
	return plans, nil
}

// insertCancellationAdjustments appends one live CANCELLATION adjustment per
// charged unit for its immutable unit price, in request order, and returns the
// unit-to-adjustment map.
func insertCancellationAdjustments(ctx context.Context, q *sqlc.Queries,
	cmd CancelUnitsCommand, resolved map[uuid.UUID]cancelResolvedUnit,
	shiftID uuid.UUID, occurredAt time.Time,
) (map[uuid.UUID]uuid.UUID, error) {
	adjustmentByUnit := make(map[uuid.UUID]uuid.UUID, len(cmd.PreparationUnitIDs))
	for _, id := range cmd.PreparationUnitIDs {
		unit := resolved[id]
		if !unit.charged() {
			continue
		}
		adjustment, err := q.InsertChargeAdjustment(ctx, sqlc.InsertChargeAdjustmentParams{
			Kind:               chargeAdjustmentKindCancellation,
			Scope:              chargeAdjustmentScopeLiveCheck,
			PreparationUnitID:  uuid.NullUUID{UUID: id, Valid: true},
			ChargeAllocationID: unit.ChargeAllocationID,
			CheckID:            unit.CheckID,
			SalesShiftID:       shiftID,
			AmountVnd:          unit.UnitPriceVND,
			CreatedAt:          occurredAt,
		})
		if err != nil {
			return nil, fmt.Errorf("insert charge adjustment: %w", mapCancelDBError(err))
		}
		adjustmentByUnit[id] = adjustment.ID
	}
	return adjustmentByUnit, nil
}

// applyCancelCheckConsequences updates each affected Check's denormalized
// charge once and settles every Check whose corrected balance reaches zero,
// writing all four settlement-evidence columns from the initiator and the
// current open Shift. A Check already SETTLED stays settled: its original
// evidence is never rewritten, and a pending Refund is a separate obligation
// that closure, not state, enforces.
func applyCancelCheckConsequences(ctx context.Context, q *sqlc.Queries,
	actor Actor, shiftID uuid.UUID, occurredAt time.Time,
	checkIDs []uuid.UUID, plans map[uuid.UUID]cancelCheckPlan,
) (map[uuid.UUID]bool, error) {
	settled := make(map[uuid.UUID]bool, len(checkIDs))
	for _, checkID := range checkIDs {
		plan := plans[checkID]
		if err := q.UpdateAdjustedCheckCharge(ctx, sqlc.UpdateAdjustedCheckChargeParams{
			ChargeVnd: plan.NewChargeVND,
			ID:        checkID,
		}); err != nil {
			return nil, fmt.Errorf("update adjusted check charge: %w", err)
		}
		if plan.State != stateCheckOpen {
			continue
		}
		if plan.NewChargeVND > plan.EffectiveReceivedVND {
			continue
		}
		if err := q.SettleAdjustedCheck(ctx, sqlc.SettleAdjustedCheckParams{
			SettledAt:                   sql.NullTime{Time: occurredAt, Valid: true},
			SettledByStaffIdentityID:    uuid.NullUUID{UUID: actor.StaffID, Valid: true},
			SettledDuringSalesShiftID:   uuid.NullUUID{UUID: shiftID, Valid: true},
			SettledStaffAccessSessionID: uuid.NullUUID{UUID: actor.SessionID, Valid: true},
			ID:                          checkID,
		}); err != nil {
			return nil, fmt.Errorf("settle adjusted check: %w", err)
		}
		settled[checkID] = true
	}
	return settled, nil
}

// appendSettledCheckAudits appends one CHECK_SETTLED audit per Check the batch
// settled, in Check lock order.
func appendSettledCheckAudits(audits []AuditRecord, checkIDs []uuid.UUID,
	settledChecks map[uuid.UUID]bool, plans map[uuid.UUID]cancelCheckPlan, shiftID uuid.UUID,
) []AuditRecord {
	for _, checkID := range checkIDs {
		if !settledChecks[checkID] {
			continue
		}
		// The Check can settle at a non-zero corrected charge: a partial
		// receipt covered by the reduced charge leaves no balance, so the
		// audit records the real before/after financial meaning rather than
		// a hardcoded zero.
		plan := plans[checkID]
		audits = append(audits, AuditRecord{
			EventType: EventCheckSettled,
			Details: map[string]any{
				"check_id":          checkID,
				"sales_shift_id":    shiftID,
				"charge_before_vnd": plan.StoredChargeVND,
				"charge_after_vnd":  plan.NewChargeVND,
			},
		})
	}
	return audits
}
