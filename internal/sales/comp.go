package sales

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// compWasteFingerprint is the normalized, credential-free business input a
// Comp stands for: the Waste, the reason from the Comp catalog, and the
// normalized note. The approver login code and PIN are deliberately absent, so
// rotating credentials cannot change the idempotency key (spec §13).
type compWasteFingerprint struct {
	WasteID uuid.UUID `json:"waste_id"`
	Reason  string    `json:"reason"`
	Note    *string   `json:"note"`
}

// compWasteFingerprintFor builds the fingerprint from the already-validated
// command and the already-normalized note.
func compWasteFingerprintFor(cmd CompWasteCommand, note *string) compWasteFingerprint {
	return compWasteFingerprint{WasteID: cmd.WasteID, Reason: cmd.Reason, Note: note}
}

// Domain values the Comp command writes, restated here because
// internal/preparation's are not importable across the ADR-024 boundary.
const unitPriorityStandard = "STANDARD"

// Database constraint names this command maps to a domain condition. Only the
// constraints that represent a documented one-Comp-per-Waste race are mapped;
// every other violation stays an infrastructure failure.
const (
	constraintChargeAdjustmentKindUnitUnique = "charge_adjustment_kind_unit_unique"
	constraintSalesCompWasteUnique           = "sales_comp_waste_unique"
	constraintSalesCompAdjustmentUnique      = "sales_comp_adjustment_unique"
)

// Post-sale correction history entry kinds, as ListCompletedSalePostSaleCorrections
// discriminates them.
const (
	postSaleEntryKindComp   = "COMP"
	postSaleEntryKindRefund = "REFUND"
)

// mapCompDBError maps exactly the named unique constraints to the typed
// condition they represent: one Waste admits one Comp. A concurrent second
// Comp serializes first on the Waste/unit lock and then fails here, and the
// whole transaction — claim included — rolls back. Every other PostgreSQL
// error is returned untouched.
func mapCompDBError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		switch pgErr.ConstraintName {
		case constraintChargeAdjustmentKindUnitUnique,
			constraintSalesCompWasteUnique,
			constraintSalesCompAdjustmentUnique:
			return fmt.Errorf("%w: the waste already carries a comp", ErrWasteAlreadyComped)
		}
	}
	return err
}

// compWasteSource is the pre-resolved ownership of one Comp source: the Waste's
// unit, the owning Session, the Completed Sale when the Session is closed, and
// the original Charge Allocation whose cumulative quantity range covers the
// unit number.
type compWasteSource struct {
	WasteID            uuid.UUID
	PreparationUnitID  uuid.UUID
	Priority           string
	ServiceSessionID   uuid.UUID
	CompletedSaleID    uuid.NullUUID
	ChargeAllocationID uuid.NullUUID
	CheckID            uuid.NullUUID
	AmountVND          int64
}

// resolveCompWasteSource resolves and validates a Comp source with a
// non-locking read. The Waste must exist, its unit must be WASTED, and the
// unit must be a charged standard unit: a Wasted Remake has no allocation and
// is rejected as COMP_SOURCE_NOT_CHARGED, while a standard unit that resolves
// no allocation or no positive price is a corrupt mapping, not a business
// state.
func resolveCompWasteSource(ctx context.Context, q *sqlc.Queries, wasteID uuid.UUID) (
	compWasteSource, error,
) {
	row, err := q.ResolveCompSource(ctx, wasteID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return compWasteSource{}, fmt.Errorf("%w: %s", ErrWasteNotFound, wasteID)
		}
		return compWasteSource{}, fmt.Errorf("resolve comp source: %w", err)
	}
	source := compWasteSource{
		WasteID:            row.WasteID,
		PreparationUnitID:  row.PreparationUnitID,
		Priority:           row.Priority,
		ServiceSessionID:   row.ServiceSessionID,
		CompletedSaleID:    row.CompletedSaleID,
		ChargeAllocationID: row.ChargeAllocationID,
		CheckID:            row.CheckID,
	}
	if row.UnitState != UnitStateWasted {
		return compWasteSource{}, fmt.Errorf("%w: unit %s is %s",
			ErrWasteNotFound, row.PreparationUnitID, row.UnitState)
	}
	if row.Priority != unitPriorityStandard {
		return compWasteSource{}, fmt.Errorf("%w: unit %s has priority %s",
			ErrCompSourceNotCharged, row.PreparationUnitID, row.Priority)
	}
	if !row.ChargeAllocationID.Valid || !row.CheckID.Valid ||
		!row.AmountVnd.Valid || row.AmountVnd.Int64 <= 0 {
		return compWasteSource{}, fmt.Errorf(
			"%w: standard unit %s resolves allocation %s, check %s, amount %v",
			ErrChargeInvariantViolated, row.PreparationUnitID,
			row.ChargeAllocationID.UUID, row.CheckID.UUID, row.AmountVnd)
	}
	source.AmountVND = row.AmountVnd.Int64
	return source, nil
}

// CompWasteHandler waives the charge of one charged Wasted unit through one
// Manager-approved append-only correction. An active Session's Comp writes a
// LIVE_CHECK adjustment, updates the Check charge, applies any settlement
// consequence, and returns the updated Service Session; a closed Session's
// Comp writes a POST_SALE adjustment linked to its Completed Sale and returns
// the sale's outstanding correction amount without rewriting any core row.
type CompWasteHandler struct{ runner *Runner }

// NewCompWasteHandler creates a CompWasteHandler.
func NewCompWasteHandler(runner *Runner) *CompWasteHandler {
	return &CompWasteHandler{runner: runner}
}

// Handle executes the Comp.
//
// The command is validated and its note normalized BEFORE the mutation begins,
// so a malformed request never claims its idempotency key. The command
// requires the initiator's sales.operate plus one inline Manager Approval for
// sales.operate; self-approval is permitted and the approver is recorded
// separately. Success answers 201; the route layer maps domain errors through
// ErrorResponse.
func (h *CompWasteHandler) Handle(ctx context.Context, actor Actor,
	cmd CompWasteCommand,
) (int, CompResult, error) {
	note := NormalizeCompNote(cmd.Note)
	if err := ValidateCompWasteCommand(cmd, note); err != nil {
		return 0, CompResult{}, err
	}

	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpCompWaste,
		Fingerprint: compWasteFingerprintFor(cmd, note),
		Required:    []string{CapSalesOperate},
		Approval: &ApprovalSpec{
			ApproverLoginCode:  cmd.ManagerApproval.ApproverLoginCode,
			ManagerPIN:         cmd.ManagerApproval.ManagerPIN,
			RequiredCapability: CapSalesOperate,
		},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, CompResult, AuditRecord, error) {
			result, audit, err := applyCompWaste(ctx, mc.Queries, actor, mc.Approver, cmd, note)
			if err != nil {
				return 0, CompResult{}, AuditRecord{}, err
			}
			return http.StatusCreated, result, audit, nil
		})
}

// applyCompWaste is the mutation body, ordered so each step's failure leaves
// the transaction — approval, claim, and result included — to the executor's
// rollback:
//
//  1. resolve the Waste and its charge mapping WITHOUT locks, rejecting
//     missing ids, non-WASTED units, and uncharged Remakes before any lock;
//  2. lock the source Check, then its Session, then the current open Sales
//     Shift, then the Waste and its unit (spec §11.1 lock order);
//  3. revalidate the locked Waste and re-resolve the mapping under the locks,
//     refusing if a concurrent restructuring moved the allocation;
//  4. a still-active Session takes the live path, a closed one the post-sale
//     path, decided by the locked Session state rather than a clock.
func applyCompWaste(ctx context.Context, q *sqlc.Queries, actor Actor,
	approver *auth.ApproverSummary, cmd CompWasteCommand, note *string,
) (CompResult, AuditRecord, error) {
	if approver == nil {
		// The executor always supplies an approver for a spec with Approval;
		// a nil here is a wiring defect, not a client condition.
		return CompResult{}, AuditRecord{}, fmt.Errorf(
			"comp waste: executor supplied no approver for a manager-approved command")
	}

	// 1. Lock-free resolution.
	pre, err := resolveCompWasteSource(ctx, q, cmd.WasteID)
	if err != nil {
		return CompResult{}, AuditRecord{}, err
	}

	// 2. Lock order: Check, Service Session, current Shift, source Waste/unit.
	lockedCheck, err := q.LockCheckForPayment(ctx, pre.CheckID.UUID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return CompResult{}, AuditRecord{}, fmt.Errorf(
				"%w: check %s vanished", ErrChargeInvariantViolated, pre.CheckID.UUID)
		}
		return CompResult{}, AuditRecord{}, fmt.Errorf("lock comp check: %w", err)
	}
	sessionRow, err := q.LockServiceSessionForUpdate(ctx, pre.ServiceSessionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return CompResult{}, AuditRecord{}, fmt.Errorf(
				"%w: session %s vanished", ErrChargeInvariantViolated, pre.ServiceSessionID)
		}
		return CompResult{}, AuditRecord{}, fmt.Errorf("lock comp service session: %w", err)
	}
	shiftID, err := lockOpenSalesShift(ctx, q)
	if err != nil {
		return CompResult{}, AuditRecord{}, err
	}
	if shiftID == uuid.Nil {
		return CompResult{}, AuditRecord{}, fmt.Errorf("%w: no sales shift is open",
			ErrOpenShiftRequired)
	}
	lockedWaste, err := q.LockWasteForComp(ctx, cmd.WasteID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return CompResult{}, AuditRecord{}, fmt.Errorf("%w: %s", ErrWasteNotFound, cmd.WasteID)
		}
		return CompResult{}, AuditRecord{}, fmt.Errorf("lock comp waste: %w", err)
	}

	// 3. Revalidate the locked source and its mapping. A Split or Merge that
	// committed between the pre-resolution and the Check lock changed the
	// established attribution, and building an adjustment on a stale mapping
	// would strand it. The loser refuses whole rather than repair.
	if lockedWaste.UnitState != UnitStateWasted {
		return CompResult{}, AuditRecord{}, fmt.Errorf("%w: unit %s is %s",
			ErrWasteNotFound, lockedWaste.PreparationUnitID, lockedWaste.UnitState)
	}
	if lockedWaste.Priority != unitPriorityStandard {
		return CompResult{}, AuditRecord{}, fmt.Errorf("%w: unit %s has priority %s",
			ErrCompSourceNotCharged, lockedWaste.PreparationUnitID, lockedWaste.Priority)
	}
	post, err := resolveCompWasteSource(ctx, q, cmd.WasteID)
	if err != nil {
		return CompResult{}, AuditRecord{}, err
	}
	if pre.PreparationUnitID != post.PreparationUnitID ||
		pre.ServiceSessionID != post.ServiceSessionID ||
		pre.ChargeAllocationID != post.ChargeAllocationID ||
		pre.CheckID != post.CheckID ||
		pre.AmountVND != post.AmountVND ||
		pre.Priority != post.Priority {
		return CompResult{}, AuditRecord{}, fmt.Errorf(
			"%w: the charge mapping of unit %s changed concurrently",
			ErrChargeAdjustmentConflict, post.PreparationUnitID)
	}
	if lockedCheck.ID != post.CheckID.UUID {
		return CompResult{}, AuditRecord{}, fmt.Errorf(
			"%w: check %s resolved after locking %s",
			ErrChargeInvariantViolated, post.CheckID.UUID, lockedCheck.ID)
	}

	occurredAt, err := q.GetSalesOccurredAt(ctx)
	if err != nil {
		return CompResult{}, AuditRecord{}, fmt.Errorf("read comp time: %w", err)
	}
	if sessionRow.State == StateActive {
		if post.CompletedSaleID.Valid {
			return CompResult{}, AuditRecord{}, fmt.Errorf(
				"%w: active session %s carries completed sale %s",
				ErrChargeInvariantViolated, post.ServiceSessionID, post.CompletedSaleID.UUID)
		}
		return applyLiveCompWaste(ctx, q, actor, approver, cmd, note, post,
			lockedCheck, shiftID, occurredAt)
	}
	if sessionRow.State != StateClosed {
		return CompResult{}, AuditRecord{}, fmt.Errorf(
			"%w: session %s is %s", ErrChargeInvariantViolated, post.ServiceSessionID, sessionRow.State)
	}
	if !post.CompletedSaleID.Valid {
		return CompResult{}, AuditRecord{}, fmt.Errorf(
			"%w: closed session %s carries no completed sale",
			ErrChargeInvariantViolated, post.ServiceSessionID)
	}
	return applyPostSaleCompWaste(ctx, q, actor, approver, cmd, note, post, shiftID, occurredAt)
}

// applyLiveCompWaste corrects an active Session: one LIVE_CHECK adjustment,
// one stored-charge update, the settlement consequence when the corrected
// balance reaches zero, the Comp fact, the audits, and the updated Service
// Session. A Check already SETTLED keeps its original evidence; a pending
// Refund is a separate obligation that closure, not state, enforces.
func applyLiveCompWaste(ctx context.Context, q *sqlc.Queries, actor Actor,
	approver *auth.ApproverSummary, cmd CompWasteCommand, note *string,
	source compWasteSource, lockedCheck sqlc.LockCheckForPaymentRow,
	shiftID uuid.UUID, occurredAt time.Time,
) (CompResult, AuditRecord, error) {
	checkID := source.CheckID.UUID

	if lockedCheck.State != CheckStateOpen && lockedCheck.State != CheckStateSettled {
		return CompResult{}, AuditRecord{}, fmt.Errorf(
			"%w: check %s is %s", ErrChargeInvariantViolated, checkID, lockedCheck.State)
	}
	if err := assertChargeMatchesAllocations(ctx, q, checkID, lockedCheck.ChargeVnd); err != nil {
		return CompResult{}, AuditRecord{}, err
	}

	// One Waste admits one Comp. The Check lock serializes concurrent Comps of
	// the same source, so a COMMITTED adjustment for this unit seen here is a
	// duplicate rather than a race; the unique constraints remain the backstop.
	existingAdjustments, err := q.ListCheckChargeAdjustments(ctx, checkID)
	if err != nil {
		return CompResult{}, AuditRecord{}, fmt.Errorf("list check charge adjustments: %w", err)
	}
	for _, existing := range existingAdjustments {
		if existing.Kind == ChargeAdjustmentKindComp &&
			existing.PreparationUnitID == source.PreparationUnitID {
			return CompResult{}, AuditRecord{}, fmt.Errorf(
				"%w: unit %s already carries a comp", ErrWasteAlreadyComped, source.PreparationUnitID)
		}
	}

	if source.AmountVND > lockedCheck.ChargeVnd {
		return CompResult{}, AuditRecord{}, fmt.Errorf(
			"%w: comp %d exceeds stored charge %d of check %s",
			ErrChargeInvariantViolated, source.AmountVND, lockedCheck.ChargeVnd, checkID)
	}
	newChargeVND := lockedCheck.ChargeVnd - source.AmountVND

	adjustment, err := q.InsertChargeAdjustment(ctx, sqlc.InsertChargeAdjustmentParams{
		Kind:               ChargeAdjustmentKindComp,
		Scope:              CompScopeLiveCheck,
		PreparationUnitID:  source.PreparationUnitID,
		PreparationWasteID: uuid.NullUUID{UUID: source.WasteID, Valid: true},
		ChargeAllocationID: source.ChargeAllocationID.UUID,
		CheckID:            checkID,
		CompletedSaleID:    uuid.NullUUID{},
		SalesShiftID:       shiftID,
		AmountVnd:          source.AmountVND,
		CreatedAt:          occurredAt,
	})
	if err != nil {
		return CompResult{}, AuditRecord{}, fmt.Errorf("insert comp charge adjustment: %w",
			mapCompDBError(err))
	}
	if err := q.UpdateAdjustedCheckCharge(ctx, sqlc.UpdateAdjustedCheckChargeParams{
		ChargeVnd: newChargeVND,
		ID:        checkID,
	}); err != nil {
		return CompResult{}, AuditRecord{}, fmt.Errorf("update adjusted check charge: %w", err)
	}

	// The balance derivation re-verifies the corrected charge against the
	// allocations and adjustments just written, then derives the receipt side.
	balanceVND, err := checkBalance(ctx, q, checkID, newChargeVND)
	if err != nil {
		return CompResult{}, AuditRecord{}, err
	}
	settled := false
	if lockedCheck.State == CheckStateOpen && SettlesCheck(balanceVND) {
		if err := q.SettleCheck(ctx, sqlc.SettleCheckParams{
			ID:                          checkID,
			SettledAt:                   sql.NullTime{Time: occurredAt, Valid: true},
			SettledByStaffIdentityID:    uuid.NullUUID{UUID: actor.StaffID, Valid: true},
			SettledDuringSalesShiftID:   uuid.NullUUID{UUID: shiftID, Valid: true},
			SettledStaffAccessSessionID: uuid.NullUUID{UUID: actor.SessionID, Valid: true},
		}); err != nil {
			return CompResult{}, AuditRecord{}, fmt.Errorf("settle adjusted check: %w", err)
		}
		settled = true
	}

	comp, err := q.InsertSalesComp(ctx, sqlc.InsertSalesCompParams{
		PreparationWasteID:        source.WasteID,
		ChargeAdjustmentID:        adjustment.ID,
		Reason:                    cmd.Reason,
		Note:                      nullString(note),
		ActorStaffIdentityID:      actor.StaffID,
		StaffAccessSessionID:      actor.SessionID,
		ApprovedByStaffIdentityID: approver.ID,
		OccurredAt:                occurredAt,
	})
	if err != nil {
		return CompResult{}, AuditRecord{}, fmt.Errorf("insert sales comp: %w", mapCompDBError(err))
	}

	chargeBeforeVND, chargeAfterVND := lockedCheck.ChargeVnd, newChargeVND
	if err := writeSalesAudit(ctx, q, actor, occurredAt, EventCheckChargeAdjusted,
		compChargeAdjustedAudit{
			CheckID:            checkID,
			ChargeAdjustmentID: adjustment.ID,
			PreparationWasteID: source.WasteID,
			PreparationUnitID:  source.PreparationUnitID,
			Scope:              CompScopeLiveCheck,
			SalesShiftID:       shiftID,
			AmountVND:          source.AmountVND,
			ChargeBeforeVND:    &chargeBeforeVND,
			ChargeAfterVND:     &chargeAfterVND,
			Reason:             cmd.Reason,
		}); err != nil {
		return CompResult{}, AuditRecord{}, err
	}
	if settled {
		if err := writeSalesAudit(ctx, q, actor, occurredAt, EventCheckSettled,
			compSettledAudit{
				CheckID:            checkID,
				SalesShiftID:       shiftID,
				ChargeBeforeVND:    lockedCheck.ChargeVnd,
				ChargeAfterVND:     newChargeVND,
				ChargeAdjustmentID: adjustment.ID,
			}); err != nil {
			return CompResult{}, AuditRecord{}, err
		}
	}

	session, err := LoadServiceSession(ctx, q, source.ServiceSessionID)
	if err != nil {
		return CompResult{}, AuditRecord{}, err
	}

	return CompResult{
		Scope:          CompScopeLiveCheck,
		Comp:           compResponseFromFact(comp, source),
		ServiceSession: &session,
	}, AuditRecord{
		EventType: EventSalesCompRecorded,
		Details: compRecordedAudit{
			CompID:                    comp.ID,
			PreparationWasteID:        source.WasteID,
			PreparationUnitID:         source.PreparationUnitID,
			ChargeAdjustmentID:        adjustment.ID,
			Scope:                     CompScopeLiveCheck,
			AmountVND:                 source.AmountVND,
			Reason:                    cmd.Reason,
			Note:                      note,
			ActorStaffIdentityID:      actor.StaffID,
			ApprovedByStaffIdentityID: approver.ID,
		},
	}, nil
}

// applyPostSaleCompWaste corrects a closed sale: one POST_SALE adjustment
// linked to its Completed Sale, the Comp fact, the audits, and the
// outstanding post-sale correction amount. It writes no Check, allocation,
// settlement, Preparation Unit, or Completed Sale core row (spec §8.3).
func applyPostSaleCompWaste(ctx context.Context, q *sqlc.Queries, actor Actor,
	approver *auth.ApproverSummary, cmd CompWasteCommand, note *string,
	source compWasteSource, shiftID uuid.UUID, occurredAt time.Time,
) (CompResult, AuditRecord, error) {
	saleID := source.CompletedSaleID.UUID

	adjustment, err := q.InsertChargeAdjustment(ctx, sqlc.InsertChargeAdjustmentParams{
		Kind:               ChargeAdjustmentKindComp,
		Scope:              CompScopePostSale,
		PreparationUnitID:  source.PreparationUnitID,
		PreparationWasteID: uuid.NullUUID{UUID: source.WasteID, Valid: true},
		ChargeAllocationID: source.ChargeAllocationID.UUID,
		CheckID:            source.CheckID.UUID,
		CompletedSaleID:    uuid.NullUUID{UUID: saleID, Valid: true},
		SalesShiftID:       shiftID,
		AmountVnd:          source.AmountVND,
		CreatedAt:          occurredAt,
	})
	if err != nil {
		return CompResult{}, AuditRecord{}, fmt.Errorf("insert comp charge adjustment: %w",
			mapCompDBError(err))
	}

	comp, err := q.InsertSalesComp(ctx, sqlc.InsertSalesCompParams{
		PreparationWasteID:        source.WasteID,
		ChargeAdjustmentID:        adjustment.ID,
		Reason:                    cmd.Reason,
		Note:                      nullString(note),
		ActorStaffIdentityID:      actor.StaffID,
		StaffAccessSessionID:      actor.SessionID,
		ApprovedByStaffIdentityID: approver.ID,
		OccurredAt:                occurredAt,
	})
	if err != nil {
		return CompResult{}, AuditRecord{}, fmt.Errorf("insert sales comp: %w", mapCompDBError(err))
	}

	if err := writeSalesAudit(ctx, q, actor, occurredAt, EventCheckChargeAdjusted,
		compChargeAdjustedAudit{
			CheckID:            source.CheckID.UUID,
			ChargeAdjustmentID: adjustment.ID,
			PreparationWasteID: source.WasteID,
			PreparationUnitID:  source.PreparationUnitID,
			Scope:              CompScopePostSale,
			CompletedSaleID:    &saleID,
			SalesShiftID:       shiftID,
			AmountVND:          source.AmountVND,
			Reason:             cmd.Reason,
		}); err != nil {
		return CompResult{}, AuditRecord{}, err
	}

	outstandingVND, err := loadOutstandingPostSaleRefundVND(ctx, q, saleID)
	if err != nil {
		return CompResult{}, AuditRecord{}, err
	}

	// The mutation projects the sale's whole additive history, so a later
	// correction never hides an earlier one (spec §12.3).
	history, err := loadCompletedSalePostSaleCorrections(ctx, q, saleID)
	if err != nil {
		return CompResult{}, AuditRecord{}, err
	}
	return CompResult{
		Scope:                        CompScopePostSale,
		Comp:                         compResponseFromFact(comp, source),
		CompletedSaleID:              &saleID,
		OutstandingPostSaleRefundVND: &outstandingVND,
		PostSaleCorrections:          history,
	}, AuditRecord{
		EventType: EventSalesCompRecorded,
		Details: compRecordedAudit{
			CompID:                    comp.ID,
			PreparationWasteID:        source.WasteID,
			PreparationUnitID:         source.PreparationUnitID,
			ChargeAdjustmentID:        adjustment.ID,
			Scope:                     CompScopePostSale,
			CompletedSaleID:           &saleID,
			AmountVND:                 source.AmountVND,
			Reason:                    cmd.Reason,
			Note:                      note,
			ActorStaffIdentityID:      actor.StaffID,
			ApprovedByStaffIdentityID: approver.ID,
		},
	}, nil
}

// loadOutstandingPostSaleRefundVND derives the uncompleted post-sale
// correction amount of one Completed Sale: every POST_SALE adjustment less
// the completed post-sale Refunds allocated against them. A pending Manual QR
// Refund reserves capacity but has not moved money, so it does not reduce the
// amount owed (spec §6.3, §12.5).
func loadOutstandingPostSaleRefundVND(ctx context.Context, q *sqlc.Queries,
	saleID uuid.UUID,
) (int64, error) {
	rows, err := q.ListCompletedSalePostSaleCorrections(ctx, saleID)
	if err != nil {
		return 0, fmt.Errorf("load post-sale corrections: %w", err)
	}
	var outstandingVND int64
	for _, row := range rows {
		if !row.AmountVnd.Valid || row.AmountVnd.Int64 < 0 {
			return 0, fmt.Errorf("%w: post-sale correction amount %v is not a sum",
				ErrFinancialInvariantViolated, row.AmountVnd)
		}
		switch {
		case row.EntryKind.Valid && row.EntryKind.String == postSaleEntryKindComp:
			outstandingVND, err = AddCharge(outstandingVND, row.AmountVnd.Int64)
			if err != nil {
				return 0, fmt.Errorf("%w: post-sale corrections: %v",
					ErrFinancialInvariantViolated, err)
			}
		case row.EntryKind.Valid && row.EntryKind.String == postSaleEntryKindRefund &&
			row.CompletedAt.Valid:
			if row.AmountVnd.Int64 > outstandingVND {
				return 0, fmt.Errorf(
					"%w: completed post-sale refund %d exceeds outstanding %d",
					ErrFinancialInvariantViolated, row.AmountVnd.Int64, outstandingVND)
			}
			outstandingVND -= row.AmountVnd.Int64
		default:
			// A pending Refund has not moved money and consumes no outstanding
			// amount until its completion exists.
			continue
		}
	}
	return outstandingVND, nil
}

// compResponseFromFact assembles the Comp result from the stored fact and the
// resolved source's immutable unit identity and amount.
func compResponseFromFact(comp sqlc.SalesComp, source compWasteSource) CompResponse {
	return CompResponse{
		ID:                        comp.ID,
		WasteID:                   comp.PreparationWasteID,
		PreparationUnitID:         source.PreparationUnitID,
		ChargeAdjustmentID:        comp.ChargeAdjustmentID,
		AmountVND:                 source.AmountVND,
		Reason:                    comp.Reason,
		Note:                      nullStringPtr(comp.Note),
		ActorStaffIdentityID:      comp.ActorStaffIdentityID,
		ApprovedByStaffIdentityID: comp.ApprovedByStaffIdentityID,
		OccurredAt:                comp.OccurredAt,
	}
}

// audit detail shapes. Each carries stable business ids and financial meaning
// and never a Manager PIN, PIN hash, or login credential (spec §15).

type compChargeAdjustedAudit struct {
	CheckID            uuid.UUID  `json:"check_id"`
	ChargeAdjustmentID uuid.UUID  `json:"charge_adjustment_id"`
	PreparationWasteID uuid.UUID  `json:"preparation_waste_id"`
	PreparationUnitID  uuid.UUID  `json:"preparation_unit_id"`
	Scope              string     `json:"scope"`
	CompletedSaleID    *uuid.UUID `json:"completed_sale_id,omitempty"`
	SalesShiftID       uuid.UUID  `json:"sales_shift_id"`
	AmountVND          int64      `json:"amount_vnd"`
	// ChargeBeforeVND and ChargeAfterVND describe the live Check charge the
	// adjustment corrected; a post-sale adjustment leaves both absent because
	// it rewrites no stored charge.
	ChargeBeforeVND *int64 `json:"charge_before_vnd,omitempty"`
	ChargeAfterVND  *int64 `json:"charge_after_vnd,omitempty"`
	Reason          string `json:"reason"`
}

type compSettledAudit struct {
	CheckID            uuid.UUID `json:"check_id"`
	SalesShiftID       uuid.UUID `json:"sales_shift_id"`
	ChargeBeforeVND    int64     `json:"charge_before_vnd"`
	ChargeAfterVND     int64     `json:"charge_after_vnd"`
	ChargeAdjustmentID uuid.UUID `json:"charge_adjustment_id"`
}

type compRecordedAudit struct {
	CompID                    uuid.UUID  `json:"comp_id"`
	PreparationWasteID        uuid.UUID  `json:"preparation_waste_id"`
	PreparationUnitID         uuid.UUID  `json:"preparation_unit_id"`
	ChargeAdjustmentID        uuid.UUID  `json:"charge_adjustment_id"`
	Scope                     string     `json:"scope"`
	CompletedSaleID           *uuid.UUID `json:"completed_sale_id,omitempty"`
	AmountVND                 int64      `json:"amount_vnd"`
	Reason                    string     `json:"reason"`
	Note                      *string    `json:"note,omitempty"`
	ActorStaffIdentityID      uuid.UUID  `json:"actor_staff_identity_id"`
	ApprovedByStaffIdentityID uuid.UUID  `json:"approved_by_staff_identity_id"`
}
