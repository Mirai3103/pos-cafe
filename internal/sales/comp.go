package sales

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/platform/database/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// compWasteFingerprint is the normalized, credential-free business input a
// Comp stands for: the Waste, the reason from the Comp catalog, and the
// normalized note. The approver login code and PIN are deliberately absent, so
// rotating credentials cannot change the idempotency key.
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
// MapHTTPError.
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
//     Shift, then the Waste and its unit (the Sales correction lock order);
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
	locks, err := lockCompSource(ctx, q, pre, cmd.WasteID)
	if err != nil {
		return CompResult{}, AuditRecord{}, err
	}

	// 3. Revalidate the locked source and its mapping.
	post, err := revalidateCompSource(ctx, q, pre, locks, cmd.WasteID)
	if err != nil {
		return CompResult{}, AuditRecord{}, err
	}

	occurredAt, err := q.GetSalesOccurredAt(ctx)
	if err != nil {
		return CompResult{}, AuditRecord{}, fmt.Errorf("read comp time: %w", err)
	}
	req := compRequest{
		actor:      actor,
		approverID: approver.ID,
		cmd:        cmd,
		note:       note,
		shiftID:    locks.shiftID,
		occurredAt: occurredAt,
	}

	// 4. Take the path the locked Session state selects.
	sessionState := locks.session.State
	if sessionState == StateActive {
		if post.CompletedSaleID.Valid {
			return CompResult{}, AuditRecord{}, fmt.Errorf(
				"%w: active session %s carries completed sale %s",
				ErrChargeInvariantViolated, post.ServiceSessionID, post.CompletedSaleID.UUID)
		}
		return applyLiveCompWaste(ctx, q, req, post, locks.check)
	}
	if sessionState != StateClosed {
		return CompResult{}, AuditRecord{}, fmt.Errorf(
			"%w: session %s is %s", ErrChargeInvariantViolated, post.ServiceSessionID, sessionState)
	}
	if !post.CompletedSaleID.Valid {
		return CompResult{}, AuditRecord{}, fmt.Errorf(
			"%w: closed session %s carries no completed sale",
			ErrChargeInvariantViolated, post.ServiceSessionID)
	}
	return applyPostSaleCompWaste(ctx, q, req, post)
}

// compLocks is a Comp's locked rows: the source Check, its Service Session,
// the currently open Shift, and the Waste with its unit.
type compLocks struct {
	check   sqlc.LockCheckForPaymentRow
	session sqlc.LockServiceSessionForUpdateRow
	shiftID uuid.UUID
	waste   sqlc.LockWasteForCompRow
}

// lockCompSource takes the Comp's locks in order — Check, Service Session,
// current Shift, Waste and unit — for the pre-resolved source. A Check or
// Session that vanished after resolution is a stored defect.
func lockCompSource(ctx context.Context, q *sqlc.Queries, pre compWasteSource, wasteID uuid.UUID) (
	compLocks, error,
) {
	checkRow, err := q.LockCheckForPayment(ctx, pre.CheckID.UUID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return compLocks{}, fmt.Errorf(
				"%w: check %s vanished", ErrChargeInvariantViolated, pre.CheckID.UUID)
		}
		return compLocks{}, fmt.Errorf("lock comp check: %w", err)
	}
	sessionRow, err := q.LockServiceSessionForUpdate(ctx, pre.ServiceSessionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return compLocks{}, fmt.Errorf(
				"%w: session %s vanished", ErrChargeInvariantViolated, pre.ServiceSessionID)
		}
		return compLocks{}, fmt.Errorf("lock comp service session: %w", err)
	}
	shiftID, err := lockOpenSalesShift(ctx, q)
	if err != nil {
		return compLocks{}, err
	}
	if shiftID == uuid.Nil {
		return compLocks{}, fmt.Errorf("%w: no sales shift is open", ErrOpenShiftRequired)
	}
	wasteRow, err := q.LockWasteForComp(ctx, wasteID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return compLocks{}, fmt.Errorf("%w: %s", ErrWasteNotFound, wasteID)
		}
		return compLocks{}, fmt.Errorf("lock comp waste: %w", err)
	}
	return compLocks{check: checkRow, session: sessionRow, shiftID: shiftID, waste: wasteRow}, nil
}

// revalidateCompSource revalidates the locked Waste and re-resolves its charge
// mapping under the locks, returning the re-resolved source. A Split or Merge
// that committed between the pre-resolution and the Check lock changed the
// established attribution, and building an adjustment on a stale mapping
// would strand it. The loser refuses whole rather than repair.
func revalidateCompSource(ctx context.Context, q *sqlc.Queries, pre compWasteSource,
	locks compLocks, wasteID uuid.UUID,
) (compWasteSource, error) {
	lockedWaste := locks.waste
	if lockedWaste.UnitState != UnitStateWasted {
		return compWasteSource{}, fmt.Errorf("%w: unit %s is %s",
			ErrWasteNotFound, lockedWaste.PreparationUnitID, lockedWaste.UnitState)
	}
	if lockedWaste.Priority != unitPriorityStandard {
		return compWasteSource{}, fmt.Errorf("%w: unit %s has priority %s",
			ErrCompSourceNotCharged, lockedWaste.PreparationUnitID, lockedWaste.Priority)
	}
	post, err := resolveCompWasteSource(ctx, q, wasteID)
	if err != nil {
		return compWasteSource{}, err
	}
	if pre.PreparationUnitID != post.PreparationUnitID ||
		pre.ServiceSessionID != post.ServiceSessionID ||
		pre.ChargeAllocationID != post.ChargeAllocationID ||
		pre.CheckID != post.CheckID ||
		pre.AmountVND != post.AmountVND ||
		pre.Priority != post.Priority {
		return compWasteSource{}, fmt.Errorf(
			"%w: the charge mapping of unit %s changed concurrently",
			ErrChargeAdjustmentConflict, post.PreparationUnitID)
	}
	if locks.check.ID != post.CheckID.UUID {
		return compWasteSource{}, fmt.Errorf(
			"%w: check %s resolved after locking %s",
			ErrChargeInvariantViolated, post.CheckID.UUID, locks.check.ID)
	}
	return post, nil
}

// compRequest is what both Comp write paths need beyond the source: who acts,
// who approved, the validated command and note, the Shift the correction is
// recorded in, and the time it is recorded at.
type compRequest struct {
	actor      Actor
	approverID uuid.UUID
	cmd        CompWasteCommand
	note       *string
	shiftID    uuid.UUID
	occurredAt time.Time
}

// applyLiveCompWaste corrects an active Session: one LIVE_CHECK adjustment,
// one stored-charge update, the settlement consequence when the corrected
// balance reaches zero, the Comp fact, the audits, and the updated Service
// Session. A Check already SETTLED keeps its original evidence; a pending
// Refund is a separate obligation that closure, not state, enforces.
func applyLiveCompWaste(ctx context.Context, q *sqlc.Queries, req compRequest,
	source compWasteSource, lockedCheck sqlc.LockCheckForPaymentRow,
) (CompResult, AuditRecord, error) {
	checkID := source.CheckID.UUID

	newChargeVND, err := assertLiveCompAdmissible(ctx, q, source, lockedCheck)
	if err != nil {
		return CompResult{}, AuditRecord{}, err
	}

	adjustment, err := insertCompChargeAdjustment(ctx, q, req, source, CompScopeLiveCheck,
		uuid.NullUUID{})
	if err != nil {
		return CompResult{}, AuditRecord{}, err
	}
	settled, err := applyLiveCompCharge(ctx, q, req, checkID, lockedCheck.State, newChargeVND)
	if err != nil {
		return CompResult{}, AuditRecord{}, err
	}
	comp, err := insertSalesComp(ctx, q, req, source, adjustment.ID)
	if err != nil {
		return CompResult{}, AuditRecord{}, err
	}

	chargeBeforeVND, chargeAfterVND := lockedCheck.ChargeVnd, newChargeVND
	if err := writeSalesAudit(ctx, q, req.actor, req.occurredAt, EventCheckChargeAdjusted,
		compChargeAdjustedAudit{
			CheckID:            checkID,
			ChargeAdjustmentID: adjustment.ID,
			PreparationWasteID: source.WasteID,
			PreparationUnitID:  source.PreparationUnitID,
			Scope:              CompScopeLiveCheck,
			SalesShiftID:       req.shiftID,
			AmountVND:          source.AmountVND,
			ChargeBeforeVND:    &chargeBeforeVND,
			ChargeAfterVND:     &chargeAfterVND,
			Reason:             req.cmd.Reason,
		}); err != nil {
		return CompResult{}, AuditRecord{}, err
	}
	if settled {
		if err := writeSalesAudit(ctx, q, req.actor, req.occurredAt, EventCheckSettled,
			compSettledAudit{
				CheckID:            checkID,
				SalesShiftID:       req.shiftID,
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
	}, newCompRecordedAudit(req, comp.ID, source, adjustment.ID, CompScopeLiveCheck, nil), nil
}

// assertLiveCompAdmissible checks the locked Check can take a live Comp of
// source and returns the corrected stored charge. One Waste admits one Comp:
// the Check lock serializes concurrent Comps of the same source, so a
// COMMITTED adjustment for this unit seen here is a duplicate rather than a
// race; the unique constraints remain the backstop.
func assertLiveCompAdmissible(ctx context.Context, q *sqlc.Queries, source compWasteSource,
	lockedCheck sqlc.LockCheckForPaymentRow,
) (int64, error) {
	checkID := source.CheckID.UUID
	if lockedCheck.State != CheckStateOpen && lockedCheck.State != CheckStateSettled {
		return 0, fmt.Errorf(
			"%w: check %s is %s", ErrChargeInvariantViolated, checkID, lockedCheck.State)
	}
	if err := assertChargeMatchesAllocations(ctx, q, checkID, lockedCheck.ChargeVnd); err != nil {
		return 0, err
	}

	existingAdjustments, err := q.ListCheckChargeAdjustments(ctx, checkID)
	if err != nil {
		return 0, fmt.Errorf("list check charge adjustments: %w", err)
	}
	for _, existing := range existingAdjustments {
		if existing.Kind == ChargeAdjustmentKindComp &&
			existing.PreparationUnitID.Valid && existing.PreparationUnitID.UUID == source.PreparationUnitID {
			return 0, fmt.Errorf(
				"%w: unit %s already carries a comp", ErrWasteAlreadyComped, source.PreparationUnitID)
		}
	}

	if source.AmountVND > lockedCheck.ChargeVnd {
		return 0, fmt.Errorf(
			"%w: comp %d exceeds stored charge %d of check %s",
			ErrChargeInvariantViolated, source.AmountVND, lockedCheck.ChargeVnd, checkID)
	}
	return lockedCheck.ChargeVnd - source.AmountVND, nil
}

// applyLiveCompCharge stores the corrected charge and settles an OPEN Check
// whose corrected balance reaches zero. It reports whether it settled the
// Check. The balance derivation re-verifies the corrected charge against the
// allocations and adjustments just written, then derives the receipt side.
func applyLiveCompCharge(ctx context.Context, q *sqlc.Queries, req compRequest, checkID uuid.UUID,
	checkState string, newChargeVND int64,
) (bool, error) {
	if err := q.UpdateAdjustedCheckCharge(ctx, sqlc.UpdateAdjustedCheckChargeParams{
		ChargeVnd: newChargeVND,
		ID:        checkID,
	}); err != nil {
		return false, fmt.Errorf("update adjusted check charge: %w", err)
	}

	balanceVND, err := checkBalance(ctx, q, checkID, newChargeVND)
	if err != nil {
		return false, err
	}
	if checkState != CheckStateOpen || !SettlesCheck(balanceVND) {
		return false, nil
	}
	if err := q.SettleCheck(ctx, sqlc.SettleCheckParams{
		ID:                          checkID,
		SettledAt:                   sql.NullTime{Time: req.occurredAt, Valid: true},
		SettledByStaffIdentityID:    uuid.NullUUID{UUID: req.actor.StaffID, Valid: true},
		SettledDuringSalesShiftID:   uuid.NullUUID{UUID: req.shiftID, Valid: true},
		SettledStaffAccessSessionID: uuid.NullUUID{UUID: req.actor.SessionID, Valid: true},
	}); err != nil {
		return false, fmt.Errorf("settle adjusted check: %w", err)
	}
	return true, nil
}

// applyPostSaleCompWaste corrects a closed sale: one POST_SALE adjustment
// linked to its Completed Sale, the Comp fact, the audits, and the
// outstanding post-sale correction amount. It writes no Check, allocation,
// settlement, Preparation Unit, or Completed Sale core row.
func applyPostSaleCompWaste(ctx context.Context, q *sqlc.Queries, req compRequest,
	source compWasteSource,
) (CompResult, AuditRecord, error) {
	saleID := source.CompletedSaleID.UUID

	adjustment, err := insertCompChargeAdjustment(ctx, q, req, source, CompScopePostSale,
		uuid.NullUUID{UUID: saleID, Valid: true})
	if err != nil {
		return CompResult{}, AuditRecord{}, err
	}
	comp, err := insertSalesComp(ctx, q, req, source, adjustment.ID)
	if err != nil {
		return CompResult{}, AuditRecord{}, err
	}

	if err := writeSalesAudit(ctx, q, req.actor, req.occurredAt, EventCheckChargeAdjusted,
		compChargeAdjustedAudit{
			CheckID:            source.CheckID.UUID,
			ChargeAdjustmentID: adjustment.ID,
			PreparationWasteID: source.WasteID,
			PreparationUnitID:  source.PreparationUnitID,
			Scope:              CompScopePostSale,
			CompletedSaleID:    &saleID,
			SalesShiftID:       req.shiftID,
			AmountVND:          source.AmountVND,
			Reason:             req.cmd.Reason,
		}); err != nil {
		return CompResult{}, AuditRecord{}, err
	}

	outstandingVND, err := loadOutstandingPostSaleRefundVND(ctx, q, saleID)
	if err != nil {
		return CompResult{}, AuditRecord{}, err
	}

	// The mutation projects the sale's whole additive history, so a later
	// correction never hides an earlier one.
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
	}, newCompRecordedAudit(req, comp.ID, source, adjustment.ID, CompScopePostSale, &saleID), nil
}

// insertCompChargeAdjustment appends the Comp's charge adjustment in scope,
// linked to completedSaleID for a post-sale correction.
func insertCompChargeAdjustment(ctx context.Context, q *sqlc.Queries, req compRequest,
	source compWasteSource, scope string, completedSaleID uuid.NullUUID,
) (sqlc.ChargeAdjustment, error) {
	adjustment, err := q.InsertChargeAdjustment(ctx, sqlc.InsertChargeAdjustmentParams{
		Kind:               ChargeAdjustmentKindComp,
		Scope:              scope,
		PreparationUnitID:  uuid.NullUUID{UUID: source.PreparationUnitID, Valid: true},
		PreparationWasteID: uuid.NullUUID{UUID: source.WasteID, Valid: true},
		ChargeAllocationID: source.ChargeAllocationID.UUID,
		CheckID:            source.CheckID.UUID,
		CompletedSaleID:    completedSaleID,
		SalesShiftID:       req.shiftID,
		AmountVnd:          source.AmountVND,
		CreatedAt:          req.occurredAt,
	})
	if err != nil {
		return sqlc.ChargeAdjustment{}, fmt.Errorf("insert comp charge adjustment: %w",
			mapCompDBError(err))
	}
	return adjustment, nil
}

// insertSalesComp appends the Comp fact naming its Waste and adjustment.
func insertSalesComp(ctx context.Context, q *sqlc.Queries, req compRequest,
	source compWasteSource, adjustmentID uuid.UUID,
) (sqlc.SalesComp, error) {
	comp, err := q.InsertSalesComp(ctx, sqlc.InsertSalesCompParams{
		PreparationWasteID:        source.WasteID,
		ChargeAdjustmentID:        adjustmentID,
		Reason:                    req.cmd.Reason,
		Note:                      nullString(req.note),
		ActorStaffIdentityID:      req.actor.StaffID,
		StaffAccessSessionID:      req.actor.SessionID,
		ApprovedByStaffIdentityID: req.approverID,
		OccurredAt:                req.occurredAt,
	})
	if err != nil {
		return sqlc.SalesComp{}, fmt.Errorf("insert sales comp: %w", mapCompDBError(err))
	}
	return comp, nil
}

// newCompRecordedAudit is the business audit record of a Comp. completedSaleID
// is nil for a live Comp.
func newCompRecordedAudit(req compRequest, compID uuid.UUID, source compWasteSource,
	adjustmentID uuid.UUID, scope string, completedSaleID *uuid.UUID,
) AuditRecord {
	return AuditRecord{
		EventType: EventSalesCompRecorded,
		Details: compRecordedAudit{
			CompID:                    compID,
			PreparationWasteID:        source.WasteID,
			PreparationUnitID:         source.PreparationUnitID,
			ChargeAdjustmentID:        adjustmentID,
			Scope:                     scope,
			CompletedSaleID:           completedSaleID,
			AmountVND:                 source.AmountVND,
			Reason:                    req.cmd.Reason,
			Note:                      req.note,
			ActorStaffIdentityID:      req.actor.StaffID,
			ApprovedByStaffIdentityID: req.approverID,
		},
	}
}

// loadOutstandingPostSaleRefundVND derives the uncompleted post-sale
// correction amount of one Completed Sale: every POST_SALE adjustment less
// the completed post-sale Refunds allocated against them. A pending Manual QR
// Refund reserves capacity but has not moved money, so it does not reduce the
// amount owed.
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
// and never a Manager PIN, PIN hash, or login credential.

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
