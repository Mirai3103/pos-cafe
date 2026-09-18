package preparation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// cancelUnitsFingerprint is the normalized, credential-free business input a
// Cancellation/Change stands for. The selection is the UUID-sorted copy, so
// two requests that name the same units in different orders are the same
// request (spec §7.1); a Cancellation/Change requires no Manager Approval, so
// there is no credential to keep out. The note is the normalized one, so
// replays of differently padded input stay equal.
type cancelUnitsFingerprint struct {
	PreparationUnitIDs []uuid.UUID `json:"preparation_unit_ids"`
	Kind               string      `json:"kind"`
	ReplacementOrderID *uuid.UUID  `json:"replacement_order_id"`
	Reason             string      `json:"reason"`
	Note               *string     `json:"note"`
}

// cancelUnitsFingerprintFor builds the credential-free fingerprint of a
// Cancellation/Change from its already-validated command and normalized note.
// The ids are a fresh UUID-sorted copy; the command's own slice keeps request
// order for the response and the per-unit writes.
func cancelUnitsFingerprintFor(cmd CancelUnitsCommand, note *string) cancelUnitsFingerprint {
	return cancelUnitsFingerprint{
		PreparationUnitIDs: uuidSortedCopy(cmd.PreparationUnitIDs),
		Kind:               cmd.Kind,
		ReplacementOrderID: cmd.ReplacementOrderID,
		Reason:             cmd.Reason,
		Note:               note,
	}
}

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

// cancelResolvedUnit is one selected unit's pre-resolved immutable facts: the
// Order Item chain, the owning Session, and — for a charged standard unit —
// the Charge Allocation whose quantity range covers its unit number and the
// immutable per-unit price. A Remake, or a standard unit beyond every
// allocation range, carries a zero CheckID and no charge.
type cancelResolvedUnit struct {
	UnitID             uuid.UUID
	State              string
	Priority           string
	OrderItemID        uuid.UUID
	ServiceSessionID   uuid.UUID
	ChargeAllocationID uuid.UUID
	CheckID            uuid.UUID
	UnitPriceVND       int64
}

// charged reports whether a resolved unit carries customer charge.
func (u cancelResolvedUnit) charged() bool { return u.CheckID != uuid.Nil }

// resolveCancelUnits indexes a ResolveCancellationUnits result and rejects any
// selected id the query did not return: a missing unit is the 404 condition
// every selection member must fail on before anything is locked.
func resolveCancelUnits(rows []sqlc.ResolveCancellationUnitsRow,
	ids []uuid.UUID,
) (map[uuid.UUID]cancelResolvedUnit, error) {
	byID := make(map[uuid.UUID]cancelResolvedUnit, len(rows))
	for _, row := range rows {
		unit := cancelResolvedUnit{
			UnitID:           row.ID,
			State:            row.State,
			Priority:         row.Priority,
			OrderItemID:      row.OrderItemID,
			ServiceSessionID: row.ServiceSessionID,
		}
		if row.ChargeAllocationID.Valid {
			unit.ChargeAllocationID = row.ChargeAllocationID.UUID
		}
		if row.CheckID.Valid {
			unit.CheckID = row.CheckID.UUID
		}
		if row.UnitPriceVnd.Valid {
			unit.UnitPriceVND = row.UnitPriceVnd.Int64
		}
		byID[row.ID] = unit
	}
	for _, id := range ids {
		if _, found := byID[id]; !found {
			return nil, fmt.Errorf("%w: %s", ErrUnitNotFound, id)
		}
	}
	return byID, nil
}

// validateCancelUnitsResolved enforces the batch preconditions that need no
// lock: every unit is QUEUED and the whole selection shares one Service
// Session. A charged unit must resolve its allocation, Check, and positive
// price together; any other combination is a corrupt mapping, not a business
// state.
func validateCancelUnitsResolved(resolved map[uuid.UUID]cancelResolvedUnit,
	ids []uuid.UUID,
) (uuid.UUID, error) {
	var sessionID uuid.UUID
	for index, id := range ids {
		unit := resolved[id]
		if unit.State != StateQueued {
			return uuid.Nil, fmt.Errorf("%w: %s is %s",
				ErrCancellationSourceNotQueued, id, unit.State)
		}
		hasAllocation := unit.ChargeAllocationID != uuid.Nil
		hasCheck := unit.CheckID != uuid.Nil
		if hasAllocation != hasCheck {
			return uuid.Nil, fmt.Errorf("%w: unit %s resolves allocation %s but check %s",
				ErrChargeInvariantViolated, id, unit.ChargeAllocationID, unit.CheckID)
		}
		// Only a Remake is legitimately uncharged; a STANDARD unit beyond
		// every allocation range means its charge cannot be resolved, and
		// silently cancelling it without a reduction would under-refund.
		if unit.Priority != PriorityRemake && !hasCheck {
			return uuid.Nil, fmt.Errorf(
				"%w: standard unit %s resolves no charge allocation",
				ErrChargeInvariantViolated, id)
		}
		if hasCheck && unit.UnitPriceVND <= 0 {
			return uuid.Nil, fmt.Errorf("%w: unit %s resolves a non-positive price %d",
				ErrChargeInvariantViolated, id, unit.UnitPriceVND)
		}
		if index == 0 {
			sessionID = unit.ServiceSessionID
			continue
		}
		if unit.ServiceSessionID != sessionID {
			return uuid.Nil, fmt.Errorf(
				"%w: units span sessions %s and %s",
				ErrCancellationSessionMismatch, sessionID, unit.ServiceSessionID)
		}
	}
	return sessionID, nil
}

// validateCancellationReplacement enforces the CHANGE preconditions with
// non-locking reads: the replacement Order must exist, belong to the same
// active Service Session, differ from every source Order, and have been
// submitted after every source Order (spec §7.2). Source Orders are resolved
// through the immutable Order Item chain; nothing about them can change after
// Submit.
func validateCancellationReplacement(ctx context.Context, q *sqlc.Queries,
	cmd CancelUnitsCommand, sessionID uuid.UUID, resolved map[uuid.UUID]cancelResolvedUnit,
) error {
	if cmd.Kind != CancelKindChange {
		return nil
	}

	orders, err := q.ListSessionOrders(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("list session orders for replacement resolution: %w", err)
	}
	orderIDs := make([]uuid.UUID, 0, len(orders))
	for _, order := range orders {
		orderIDs = append(orderIDs, order.ID)
	}
	items, err := q.ListOrderItems(ctx, orderIDs)
	if err != nil {
		return fmt.Errorf("list order items for replacement resolution: %w", err)
	}
	orderByItem := make(map[uuid.UUID]uuid.UUID, len(items))
	for _, item := range items {
		orderByItem[item.ID] = item.OrderID
	}

	sourceSet := make(map[uuid.UUID]struct{}, len(resolved))
	for _, unit := range resolved {
		orderID, found := orderByItem[unit.OrderItemID]
		if !found {
			return fmt.Errorf("%w: unit %s resolves no source order",
				ErrChargeInvariantViolated, unit.UnitID)
		}
		sourceSet[orderID] = struct{}{}
	}
	sourceOrderIDs := make([]uuid.UUID, 0, len(sourceSet))
	for orderID := range sourceSet {
		sourceOrderIDs = append(sourceOrderIDs, orderID)
	}

	replacement, err := q.GetCancellationReplacementOrder(ctx,
		sqlc.GetCancellationReplacementOrderParams{
			ServiceSessionID:   sessionID,
			SourceOrderIds:     uuidSortedCopy(sourceOrderIDs),
			ReplacementOrderID: *cmd.ReplacementOrderID,
		})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: %s", ErrReplacementOrderNotFound, *cmd.ReplacementOrderID)
		}
		return fmt.Errorf("resolve replacement order: %w", err)
	}
	if !replacement.SameSession {
		return fmt.Errorf("%w: %s belongs to another service session",
			ErrReplacementOrderInvalid, replacement.ID)
	}
	if !replacement.DiffersFromSourceOrders {
		return fmt.Errorf("%w: %s is a source order of the selection",
			ErrReplacementOrderInvalid, replacement.ID)
	}
	if !replacement.SubmittedAfterSourceOrders {
		return fmt.Errorf("%w: %s was not submitted after every source order",
			ErrReplacementOrderInvalid, replacement.ID)
	}
	return nil
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
// corrupt stored result (spec §6.1).
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
// stored charge = base allocations - live adjustments (spec §5.1). A
// disagreement is a defect, not a business state.
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

// CancelUnitsHandler terminates 1 through MaxCancellationUnits queued
// Preparation Units in one all-or-nothing transaction: each selected unit
// becomes CANCELLED with its typed transition, Cancellation fact, and
// CANCELLATION or CHANGE alert, while each charged standard unit reduces its
// Check's live charge through one append-only CANCELLATION Charge Adjustment
// and the Check settles when the corrected balance reaches zero.
type CancelUnitsHandler struct{ runner *Runner }

// NewCancelUnitsHandler creates a CancelUnitsHandler.
func NewCancelUnitsHandler(runner *Runner) *CancelUnitsHandler {
	return &CancelUnitsHandler{runner: runner}
}

// Handle executes the Cancellation/Change.
//
// The command is validated and its note normalized BEFORE the mutation
// begins, so a malformed request never claims its idempotency key. The
// command requires sales.operate and no Manager Approval (spec §13). Success
// answers 200; the route layer maps domain errors through ErrorResponse.
func (h *CancelUnitsHandler) Handle(ctx context.Context, actor Actor,
	cmd CancelUnitsCommand,
) (int, CancelUnitsResponse, error) {
	note := NormalizeCorrectionNote(cmd.Note)
	if err := ValidateCancelUnitsCommand(cmd, note); err != nil {
		return 0, CancelUnitsResponse{}, err
	}

	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpCancelUnits,
		Fingerprint: cancelUnitsFingerprintFor(cmd, note),
		Required:    []string{CapSalesOperate},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, CancelUnitsResponse, AuditRecord, error) {
			result, err := applyCancelUnits(ctx, mc.Queries, actor, cmd, note)
			if err != nil {
				return 0, CancelUnitsResponse{}, AuditRecord{}, err
			}
			// Every business audit was written inside the mutation through
			// writePreparationAudits, sharing the batch's one timestamp, so
			// the executor's single-audit step is deliberately given a zero
			// record.
			return http.StatusOK, result, AuditRecord{}, nil
		})
}

// applyCancelUnits is the mutation body, ordered so each step's failure leaves
// the transaction — claim included — to the executor's rollback. There are no
// savepoints: a partial financial correction is not a business state.
//
//  1. resolve every selected unit and its charge mapping WITHOUT locks,
//     rejecting missing ids and stale states before any lock is taken;
//  2. for CHANGE, resolve and validate the already-submitted replacement
//     Order in the same Session, also without locks;
//  3. lock the affected Checks in ascending UUID order (spec §11.1 step 1);
//  4. lock the one shared Service Session and require it ACTIVE;
//  5. lock the one open Sales Shift FOR SHARE;
//  6. lock the selected Preparation Units in ascending UUID order;
//  7. re-resolve the unit-to-allocation mapping under those locks and refuse
//     if a concurrent restructuring changed it;
//  8. verify every affected Check's stored charge against base allocations
//     less existing live adjustments, then plan one CANCELLATION adjustment
//     per charged unit;
//  9. insert the adjustments, update each Check's charge once, and settle
//     every Check whose corrected balance reaches zero with all four
//     settlement-evidence columns;
//  10. per unit in request order, append the state, transition, Cancellation
//     fact, and alert; batch all audits under the batch's one timestamp;
//  11. return one outcome and one alert per input unit in request order.
func applyCancelUnits(ctx context.Context, q *sqlc.Queries, actor Actor,
	cmd CancelUnitsCommand, note *string,
) (CancelUnitsResponse, error) {
	// 1. The lock-free resolution runs on the mutation's own transaction: the
	// reads take no row locks, so the batch rejects its missing ids and stale
	// states before serializing on anything.
	rows, err := q.ResolveCancellationUnits(ctx, cmd.PreparationUnitIDs)
	if err != nil {
		return CancelUnitsResponse{}, fmt.Errorf("resolve cancellation units: %w", err)
	}
	resolved, err := resolveCancelUnits(rows, cmd.PreparationUnitIDs)
	if err != nil {
		return CancelUnitsResponse{}, err
	}
	sessionID, err := validateCancelUnitsResolved(resolved, cmd.PreparationUnitIDs)
	if err != nil {
		return CancelUnitsResponse{}, err
	}

	// 2. CHANGE resolves its replacement Order before any lock; Orders are
	// immutable after Submit.
	if err := validateCancellationReplacement(ctx, q, cmd, sessionID, resolved); err != nil {
		return CancelUnitsResponse{}, err
	}

	// 3. Every affected Check, deduplicated and ascending by UUID.
	checkIDs := cancelCheckIDs(resolved, cmd.PreparationUnitIDs)
	lockedChecks, err := q.LockPreparationChecksForCancellation(ctx, checkIDs)
	if err != nil {
		return CancelUnitsResponse{}, fmt.Errorf("lock preparation checks for cancellation: %w", err)
	}
	if len(lockedChecks) != len(checkIDs) {
		return CancelUnitsResponse{}, fmt.Errorf(
			"lock preparation checks for cancellation: expected %d rows, got %d",
			len(checkIDs), len(lockedChecks))
	}

	// 4. The one shared Service Session, after its Checks.
	sessions, err := q.LockPreparationSessionsForCancellation(ctx, []uuid.UUID{sessionID})
	if err != nil {
		return CancelUnitsResponse{}, fmt.Errorf("lock preparation service session: %w", err)
	}
	if len(sessions) != 1 || sessions[0].ID != sessionID {
		return CancelUnitsResponse{}, fmt.Errorf(
			"lock preparation service session: expected %s, got %d rows",
			sessionID, len(sessions))
	}
	if sessions[0].State != stateServiceSessionActive {
		return CancelUnitsResponse{}, fmt.Errorf("%w: %s is %s",
			ErrServiceSessionClosed, sessionID, sessions[0].State)
	}

	// 5. The one open Sales Shift, FOR SHARE: settlement evidence names it and
	// Shift closure must stay excluded for the whole transaction.
	shift, err := q.LockOpenSalesShiftForCancellation(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return CancelUnitsResponse{}, fmt.Errorf("%w: no sales shift is open",
				ErrOpenShiftRequired)
		}
		return CancelUnitsResponse{}, fmt.Errorf("lock open sales shift: %w", err)
	}

	// 6. The selected units last, in ascending UUID order.
	sortedUnitIDs := uuidSortedCopy(cmd.PreparationUnitIDs)
	lockedUnits, err := q.LockPreparationUnitsForCancellation(ctx, sortedUnitIDs)
	if err != nil {
		return CancelUnitsResponse{}, fmt.Errorf("lock preparation units for cancellation: %w", err)
	}
	if len(lockedUnits) != len(sortedUnitIDs) {
		return CancelUnitsResponse{}, fmt.Errorf(
			"lock preparation units for cancellation: expected %d rows, got %d",
			len(sortedUnitIDs), len(lockedUnits))
	}
	lockedByID := make(map[uuid.UUID]sqlc.LockPreparationUnitsForCancellationRow,
		len(lockedUnits))
	for _, row := range lockedUnits {
		lockedByID[row.ID] = row
	}
	for _, id := range cmd.PreparationUnitIDs {
		if state := lockedByID[id].State; state != StateQueued {
			return CancelUnitsResponse{}, fmt.Errorf("%w: %s is %s",
				ErrCancellationSourceNotQueued, id, state)
		}
	}

	// 7. Re-resolve under the locks: a Split or Merge that committed between
	// the pre-resolution and the Check lock changed the established mapping,
	// and building an adjustment on a stale attribution would strand it. The
	// loser refuses whole rather than repair.
	revalidatedRows, err := q.ResolveCancellationUnits(ctx, cmd.PreparationUnitIDs)
	if err != nil {
		return CancelUnitsResponse{}, fmt.Errorf("re-resolve cancellation units: %w", err)
	}
	revalidated, err := resolveCancelUnits(revalidatedRows, cmd.PreparationUnitIDs)
	if err != nil {
		return CancelUnitsResponse{}, err
	}
	for _, id := range cmd.PreparationUnitIDs {
		before, after := resolved[id], revalidated[id]
		if before.ChargeAllocationID != after.ChargeAllocationID ||
			before.CheckID != after.CheckID ||
			before.UnitPriceVND != after.UnitPriceVND ||
			before.Priority != after.Priority ||
			before.ServiceSessionID != after.ServiceSessionID {
			return CancelUnitsResponse{}, fmt.Errorf(
				"%w: the charge mapping of unit %s changed concurrently",
				ErrChargeAdjustmentConflict, id)
		}
	}

	// 8. One database instant for the whole batch and transaction.
	occurredAt, err := q.GetPreparationCurrentTime(ctx)
	if err != nil {
		return CancelUnitsResponse{}, fmt.Errorf("read cancellation occurrence time: %w", err)
	}

	plans, err := planCancelCheckCharges(ctx, q, checkIDs, sessionID, resolved,
		cmd.PreparationUnitIDs)
	if err != nil {
		return CancelUnitsResponse{}, err
	}

	// 9. One append-only adjustment per charged unit, then one charge update
	// per Check, then settlement.
	adjustmentByUnit, err := insertCancellationAdjustments(ctx, q, cmd,
		resolved, shift.ID, occurredAt)
	if err != nil {
		return CancelUnitsResponse{}, err
	}

	settledChecks, err := applyCancelCheckConsequences(ctx, q, actor, shift.ID,
		occurredAt, checkIDs, plans)
	if err != nil {
		return CancelUnitsResponse{}, err
	}

	// 10. Per unit, in request order.
	outcomes := make([]CancellationOutcome, len(cmd.PreparationUnitIDs))
	alerts := make([]QueueAlertResponse, len(cmd.PreparationUnitIDs))
	indexByUnit := make(map[uuid.UUID]int, len(cmd.PreparationUnitIDs))
	for index, id := range cmd.PreparationUnitIDs {
		indexByUnit[id] = index
	}
	audits := make([]AuditRecord, 0, len(cmd.PreparationUnitIDs)*3+len(checkIDs))
	for _, checkID := range checkIDs {
		if settledChecks[checkID] {
			// The Check can settle at a non-zero corrected charge: a partial
			// receipt covered by the reduced charge leaves no balance, so the
			// audit records the real before/after financial meaning rather
			// than a hardcoded zero (spec §15).
			plan := plans[checkID]
			audits = append(audits, AuditRecord{
				EventType: EventCheckSettled,
				Details: map[string]any{
					"check_id":          checkID,
					"sales_shift_id":    shift.ID,
					"charge_before_vnd": plan.StoredChargeVND,
					"charge_after_vnd":  plan.NewChargeVND,
				},
			})
		}
	}
	for _, id := range cmd.PreparationUnitIDs {
		unit := resolved[id]
		index := indexByUnit[id]

		if err := q.SetPreparationUnitState(ctx, sqlc.SetPreparationUnitStateParams{
			ID: id, State: StateCancelled, OccurredAt: occurredAt,
		}); err != nil {
			return CancelUnitsResponse{}, fmt.Errorf("set preparation unit state: %w", err)
		}
		if err := q.InsertPreparationUnitTransition(ctx,
			sqlc.InsertPreparationUnitTransitionParams{
				PreparationUnitID:    id,
				PriorState:           StateQueued,
				ResultingState:       StateCancelled,
				ActorStaffIdentityID: actor.StaffID,
				StaffAccessSessionID: actor.SessionID,
				OccurredAt:           occurredAt,
			}); err != nil {
			return CancelUnitsResponse{}, fmt.Errorf("insert preparation unit transition: %w", err)
		}

		var chargeAdjustmentID uuid.NullUUID
		var outcomeAdjustmentID *uuid.UUID
		if adjustmentID, found := adjustmentByUnit[id]; found {
			chargeAdjustmentID = uuid.NullUUID{UUID: adjustmentID, Valid: true}
			value := adjustmentID
			outcomeAdjustmentID = &value
		}
		var replacementOrderID uuid.NullUUID
		if cmd.ReplacementOrderID != nil {
			replacementOrderID = uuid.NullUUID{UUID: *cmd.ReplacementOrderID, Valid: true}
		}

		noteValue := sql.NullString{}
		if note != nil {
			noteValue = sql.NullString{String: *note, Valid: true}
		}
		fact, err := q.InsertPreparationCancellation(ctx,
			sqlc.InsertPreparationCancellationParams{
				PreparationUnitID:    id,
				Kind:                 cmd.Kind,
				ChargeAdjustmentID:   chargeAdjustmentID,
				ReplacementOrderID:   replacementOrderID,
				Reason:               cmd.Reason,
				Note:                 noteValue,
				ActorStaffIdentityID: actor.StaffID,
				StaffAccessSessionID: actor.SessionID,
				OccurredAt:           occurredAt,
			})
		if err != nil {
			return CancelUnitsResponse{}, fmt.Errorf("insert preparation cancellation: %w",
				mapCancelDBError(err))
		}

		alert, err := q.InsertPreparationAlert(ctx, sqlc.InsertPreparationAlertParams{
			PreparationUnitID:           id,
			Kind:                        cmd.Kind,
			Reason:                      cmd.Reason,
			Note:                        noteValue,
			CreatedByStaffIdentityID:    actor.StaffID,
			CreatedStaffAccessSessionID: actor.SessionID,
			CreatedAt:                   occurredAt,
		})
		if err != nil {
			return CancelUnitsResponse{}, fmt.Errorf("insert preparation alert: %w", err)
		}

		// The bar projection is read back so the alert carries the same
		// service_number, item_name, and unit_number a queue read projects.
		unitView, err := loadUnit(ctx, q, id)
		if err != nil {
			return CancelUnitsResponse{}, err
		}

		outcomes[index] = CancellationOutcome{
			CancellationID:     fact.ID,
			PreparationUnitID:  id,
			PriorState:         StateQueued,
			ResultingState:     StateCancelled,
			ChargeAdjustmentID: outcomeAdjustmentID,
			ChargeRemovedVND:   unit.UnitPriceVND,
			OccurredAt:         fact.OccurredAt,
		}
		alerts[index] = buildCancellationAlert(alert, unitView)

		audits = append(audits,
			AuditRecord{
				EventType: EventPreparationUnitCancelled,
				Details: map[string]any{
					"preparation_unit_id":  id,
					"cancellation_id":      fact.ID,
					"kind":                 cmd.Kind,
					"prior_state":          StateQueued,
					"resulting_state":      StateCancelled,
					"charge_adjustment_id": outcomeAdjustmentID,
					"charge_removed_vnd":   unit.UnitPriceVND,
					"reason":               cmd.Reason,
				},
			},
			AuditRecord{
				EventType: EventPreparationAlertCreated,
				Details: map[string]any{
					"preparation_unit_id": id,
					"alert_id":            alert.ID,
					"kind":                cmd.Kind,
					"reason":              cmd.Reason,
				},
			})
		if outcomeAdjustmentID != nil {
			plan := plans[unit.CheckID]
			audits = append(audits, AuditRecord{
				EventType: EventCheckChargeAdjusted,
				Details: map[string]any{
					"check_id":             unit.CheckID,
					"charge_adjustment_id": *outcomeAdjustmentID,
					"preparation_unit_id":  id,
					"amount_vnd":           unit.UnitPriceVND,
					"charge_before_vnd":    plan.StoredChargeVND,
					"charge_after_vnd":     plan.NewChargeVND,
				},
			})
		}
	}

	// 11. One audit batch, one actor, one session, one occurrence time.
	if err := writePreparationAudits(ctx, q, actor, occurredAt, audits); err != nil {
		return CancelUnitsResponse{}, err
	}

	return CancelUnitsResponse{Outcomes: outcomes, Alerts: alerts}, nil
}

// cancelCheckIDs collects the affected Checks of a selection, deduplicated and
// ascending in UUID byte order so every multi-row Check lock is deterministic.
func cancelCheckIDs(resolved map[uuid.UUID]cancelResolvedUnit,
	ids []uuid.UUID,
) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(ids))
	checkIDs := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		unit := resolved[id]
		if !unit.charged() {
			continue
		}
		if _, duplicate := seen[unit.CheckID]; duplicate {
			continue
		}
		seen[unit.CheckID] = struct{}{}
		checkIDs = append(checkIDs, unit.CheckID)
	}
	return uuidSortedCopy(checkIDs)
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
			PreparationUnitID:  id,
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
// current open Shift (spec §6.2). A Check already SETTLED stays settled: its
// original evidence is never rewritten, and a pending Refund is a separate
// obligation that closure, not state, enforces.
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

// buildCancellationAlert projects one stored alert with the unit identity the
// queue reads at read time. A Cancellation/Change alert resolves no Waste
// fact, so WasteID stays nil.
func buildCancellationAlert(alert sqlc.PreparationAlert,
	unit UnitResponse,
) QueueAlertResponse {
	var note *string
	if alert.Note.Valid {
		value := alert.Note.String
		note = &value
	}
	return QueueAlertResponse{
		ID:                alert.ID,
		Kind:              alert.Kind,
		PreparationUnitID: alert.PreparationUnitID,
		ServiceNumber:     unit.ServiceNumber,
		ItemName:          unit.ItemName,
		UnitNumber:        unit.UnitNumber,
		Reason:            alert.Reason,
		Note:              note,
		CreatedAt:         alert.CreatedAt,
	}
}
