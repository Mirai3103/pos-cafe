package sales

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

// newServiceSessionResponse returns a response with every collection
// initialized, so the JSON contract never emits null for an array.
func newServiceSessionResponse() ServiceSessionResponse {
	return ServiceSessionResponse{
		Tables:           make([]SessionTableResponse, 0),
		Checks:           make([]CheckResponse, 0),
		Orders:           make([]OrderResponse, 0),
		PreparationUnits: make([]PreparationUnitResponse, 0),
	}
}

// newDraftItemResponse returns a draft item with its collection initialized,
// so the JSON contract never emits null for selected_modifier_options.
func newDraftItemResponse() DraftItemResponse {
	return DraftItemResponse{
		SelectedModifierOptions: make([]SelectedModifierOptionResponse, 0),
	}
}

// LoadServiceSession assembles the full projection for one Service Session.
//
// Every mutation returns this, so a client never needs a follow-up read and a
// composition merge that changed an id the client was holding is immediately
// visible. Callers must run it inside the same transaction as their mutation.
func LoadServiceSession(ctx context.Context, q *sqlc.Queries, sessionID uuid.UUID) (
	ServiceSessionResponse, error,
) {
	out := newServiceSessionResponse()

	session, err := q.GetServiceSession(ctx, sessionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return out, fmt.Errorf("%w: %s", ErrServiceSessionNotFound, sessionID)
		}
		return out, fmt.Errorf("load service session: %w", err)
	}

	out.ID = session.ID
	out.ServiceNumber = session.ServiceNumber
	out.ServiceMode = session.ServiceMode
	out.State = session.State
	out.SalesShiftID = session.SalesShiftID
	out.CreatedAt = session.CreatedAt

	tableRows, err := q.ListServiceSessionTables(ctx, sessionID)
	if err != nil {
		return out, fmt.Errorf("load session tables: %w", err)
	}
	for _, row := range tableRows {
		out.Tables = append(out.Tables, SessionTableResponse{ID: row.ID, Name: row.Name})
	}

	// Checks load before the draft block: a Session that has committed its
	// only draft has Checks but no EDITABLE draft, so the no-draft path below
	// must still carry them.
	checks, err := loadChecks(ctx, q, sessionID)
	if err != nil {
		return out, err
	}
	out.Checks = checks

	orders, err := loadOrders(ctx, q, sessionID)
	if err != nil {
		return out, err
	}
	out.Orders = orders

	units, err := loadPreparationUnits(ctx, q, sessionID)
	if err != nil {
		return out, err
	}
	out.PreparationUnits = units

	draft, err := q.GetEditableDraft(ctx, sessionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// 5A always creates a draft with its Session, so this is only
			// reachable once 5B can commit one without a successor.
			return out, nil
		}
		return out, fmt.Errorf("load editable draft: %w", err)
	}

	items, err := loadDraftItems(ctx, q, draft.ID)
	if err != nil {
		return out, err
	}
	out.Draft = &OrderDraftResponse{
		ID:          draft.ID,
		State:       draft.State,
		CheckTarget: draft.CheckTarget,
		Items:       items,
	}

	return out, nil
}

// loadDraftItems reads the draft's items and their selected options in two
// queries rather than one per item.
func loadDraftItems(ctx context.Context, q *sqlc.Queries, draftID uuid.UUID) (
	[]DraftItemResponse, error,
) {
	itemRows, err := q.ListDraftItems(ctx, draftID)
	if err != nil {
		return nil, fmt.Errorf("load draft items: %w", err)
	}

	optionRows, err := q.ListDraftItemModifierOptions(ctx, draftID)
	if err != nil {
		return nil, fmt.Errorf("load draft item modifier options: %w", err)
	}

	// The query already orders by group name then option name, so appending in
	// scan order preserves the projection's ordering per item.
	byItem := make(map[uuid.UUID][]SelectedModifierOptionResponse, len(itemRows))
	for _, row := range optionRows {
		byItem[row.OrderDraftItemID] = append(byItem[row.OrderDraftItemID],
			SelectedModifierOptionResponse{
				ID:           row.OptionID,
				GroupID:      row.GroupID,
				GroupName:    row.GroupName,
				Name:         row.OptionName,
				SurchargeVND: row.SurchargeVnd,
			})
	}

	items := make([]DraftItemResponse, 0, len(itemRows))
	for _, row := range itemRows {
		options := byItem[row.ID]
		if options == nil {
			options = make([]SelectedModifierOptionResponse, 0)
		}
		item := DraftItemResponse{
			ID:         row.ID,
			MenuItemID: row.MenuItemID,
			Name:       row.MenuItemName,
			Quantity:   row.Quantity,
			// The availability expression is never SQL NULL: mi.available is
			// NOT NULL, IS NULL predicates never yield NULL, and s.available is
			// NULL only when di.size_id IS NULL has already made the OR true.
			Available:               row.Available.Bool,
			SelectedModifierOptions: options,
		}
		if row.PriceVnd.Valid {
			v := row.PriceVnd.Int64
			item.PriceVND = &v
		}
		if row.SizeID.Valid {
			id := row.SizeID.UUID
			item.SizeID = &id
		}
		if row.SizeName.Valid {
			name := row.SizeName.String
			item.SizeName = &name
		}
		if row.PreparationNote.Valid {
			note := row.PreparationNote.String
			item.PreparationNote = &note
		}
		items = append(items, item)
	}
	return items, nil
}

// loadChecks assembles every Check of a Service Session with its allocations.
// It is the live projection's compatibility wrapper around
// loadChecksForSnapshot; callers that must serve the immutable Completed Sale
// core pass SnapshotCompletedSaleCore instead.
func loadChecks(ctx context.Context, q *sqlc.Queries, sessionID uuid.UUID) (
	[]CheckResponse, error,
) {
	return loadChecksForSnapshot(ctx, q, sessionID, SnapshotLive)
}

// loadChecksForSnapshot assembles every Check of a Service Session with its
// Charge Allocations, Charge Adjustments, Payments, and Refunds.
//
// The Check's stored charge_vnd is a denormalization of base allocations less
// live adjustments, in the same spirit as 5A's modifier_key: derived, never
// authoritative. Every read recomputes the full live financial equation and
// compares the resulting charge, and a mismatch fails the read rather than
// serving a wrong total. See the design, sections 6.1 and 12.3.
func loadChecksForSnapshot(ctx context.Context, q *sqlc.Queries, sessionID uuid.UUID,
	mode SnapshotMode,
) ([]CheckResponse, error) {
	checkRows, err := q.ListSessionChecks(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("load session checks: %w", err)
	}

	out := make([]CheckResponse, 0, len(checkRows))
	for _, row := range checkRows {
		check, err := loadCheckForSnapshot(ctx, q, row, mode)
		if err != nil {
			return nil, err
		}
		out = append(out, check)
	}
	return out, nil
}

// loadCheckForSnapshot derives one Check's projection from its append-only
// facts and verifies the invariants the database cannot express: the stored
// charge equals base allocations less live adjustments, and the Check's state
// matches its balance. A MERGED Check is exempt from the settlement half only:
// its charge and allocations moved to the survivor, and
// check_settlement_evidence_valid already requires it to point at one.
func loadCheckForSnapshot(ctx context.Context, q *sqlc.Queries, row sqlc.ListSessionChecksRow,
	mode SnapshotMode,
) (CheckResponse, error) {
	allocations, baseChargeVND, err := loadCheckAllocations(ctx, q, row.ID)
	if err != nil {
		return CheckResponse{}, err
	}

	adjustments, liveAdjustmentVND, err := loadCheckAdjustments(ctx, q, row.ID, mode)
	if err != nil {
		return CheckResponse{}, err
	}

	payments, originalPaymentVND, voidedPaymentVND, err := loadCheckPayments(ctx, q, row.ID, mode)
	if err != nil {
		return CheckResponse{}, err
	}

	refunds, completedRefundVND, err := loadCheckRefunds(ctx, q, row.ID)
	if err != nil {
		return CheckResponse{}, err
	}

	financials, err := ComputeCheckFinancials(CheckFinancialInputs{
		BaseChargeVND:      baseChargeVND,
		LiveAdjustmentVND:  liveAdjustmentVND,
		OriginalPaymentVND: originalPaymentVND,
		VoidedPaymentVND:   voidedPaymentVND,
		CompletedRefundVND: completedRefundVND,
	})
	if err != nil {
		return CheckResponse{}, fmt.Errorf("check %s: %w", row.ID, err)
	}

	if financials.ChargeVND != row.ChargeVnd {
		slog.Error("check charge does not match its allocations and adjustments",
			"check_id", row.ID,
			"stored_charge_vnd", row.ChargeVnd,
			"computed_charge_vnd", financials.ChargeVND,
			"base_charge_vnd", baseChargeVND,
			"live_adjustment_vnd", liveAdjustmentVND)
		return CheckResponse{}, fmt.Errorf("%w: check %s stored %d, computed %d",
			ErrChargeInvariantViolated, row.ID, row.ChargeVnd, financials.ChargeVND)
	}

	// The read-path half of the settlement guard. The database constraint
	// guarantees that a SETTLED Check carries complete evidence; this
	// guarantees that its state matches the money. A SETTLED Check carrying a
	// pending Refund still has a zero balance, so it stays settled.
	if row.State != CheckStateMerged &&
		(row.State == CheckStateSettled) != SettlesCheck(financials.BalanceVND) {
		slog.Error("check state does not match its balance",
			"check_id", row.ID, "state", row.State, "balance_vnd", financials.BalanceVND)
		return CheckResponse{}, fmt.Errorf("%w: check %s", ErrSettlementInvariantViolated, row.ID)
	}

	check := CheckResponse{
		ID:                   row.ID,
		State:                row.State,
		BaseChargeVND:        baseChargeVND,
		ChargeVND:            financials.ChargeVND,
		TotalAppliedVND:      originalPaymentVND,
		TotalVoidedVND:       voidedPaymentVND,
		TotalRefundedVND:     completedRefundVND,
		EffectiveReceivedVND: financials.EffectiveReceivedVND,
		BalanceVND:           financials.BalanceVND,
		PendingRefundVND:     financials.PendingRefundVND,
		CreatedAt:            row.CreatedAt,
		Payments:             payments,
		Allocations:          allocations,
		ChargeAdjustments:    adjustments,
		Refunds:              refunds,
	}
	if row.MergedIntoCheckID.Valid {
		into := row.MergedIntoCheckID.UUID
		check.MergedIntoCheckID = &into
	}
	return check, nil
}

// loadCheckPayments returns one Check's Payments, their summed original applied
// amount, and their summed voided amount, in (received_at, id) order. Void
// evidence rides along so a client can present a reversed Payment without a
// second read.
func loadCheckPayments(ctx context.Context, q *sqlc.Queries, checkID uuid.UUID,
	mode SnapshotMode,
) ([]PaymentResponse, int64, int64, error) {
	rows, err := q.ListCheckPayments(ctx, checkID)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("load check payments: %w", err)
	}

	originalVND, voidedVND, err := sumPaymentAmounts(rows)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("check %s: %w", checkID, err)
	}

	paymentIDs := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		paymentIDs = append(paymentIDs, row.ID)
	}
	allocated, err := loadPaymentRefundAllocations(ctx, q, paymentIDs)
	if err != nil {
		return nil, 0, 0, err
	}

	out := make([]PaymentResponse, 0, len(rows))
	for _, row := range rows {
		remainingVND, err := remainingRefundableVND(row.AppliedAmountVnd, allocated[row.ID], mode)
		if err != nil {
			return nil, 0, 0, fmt.Errorf("payment %s: %w", row.ID, err)
		}
		payment := PaymentResponse{
			ID:                     row.ID,
			Method:                 row.Method,
			AppliedAmountVND:       row.AppliedAmountVnd,
			SalesShiftID:           row.SalesShiftID,
			ReceivedAt:             row.ReceivedAt,
			RemainingRefundableVND: remainingVND,
		}
		if row.CashTenderedVnd.Valid {
			v := row.CashTenderedVnd.Int64
			payment.CashTenderedVND = &v
		}
		if row.ChangeDueVnd.Valid {
			v := row.ChangeDueVnd.Int64
			payment.ChangeDueVND = &v
		}
		if row.TransactionReference.Valid {
			v := row.TransactionReference.String
			payment.TransactionReference = &v
		}
		if row.VoidID.Valid {
			payment.Void = &PaymentVoidResponse{
				ID:                        row.VoidID.UUID,
				AmountVND:                 row.VoidAmountVnd.Int64,
				Reason:                    row.VoidReason.String,
				Note:                      nullStringPtr(row.VoidNote),
				ActorStaffIdentityID:      row.VoidActorStaffIdentityID.UUID,
				ApprovedByStaffIdentityID: row.VoidApprovedByStaffIdentityID.UUID,
				OccurredAt:                row.VoidOccurredAt.Time,
			}
		}
		out = append(out, payment)
	}
	return out, originalVND, voidedVND, nil
}

// sumPaymentAmounts totals one Check's Payments and their Voids, enforcing the
// whole-Void rule on the persisted facts.
func sumPaymentAmounts(rows []sqlc.ListCheckPaymentsRow) (int64, int64, error) {
	var originalVND, voidedVND int64
	for _, row := range rows {
		next, err := AddCharge(originalVND, row.AppliedAmountVnd)
		if err != nil {
			return 0, 0, fmt.Errorf("%w: payment amounts: %v",
				ErrFinancialInvariantViolated, err)
		}
		originalVND = next
		if !row.VoidID.Valid {
			continue
		}
		if row.VoidAmountVnd.Int64 != row.AppliedAmountVnd {
			return 0, 0, fmt.Errorf(
				"%w: payment %s void amount %d differs from applied %d",
				ErrFinancialInvariantViolated, row.ID,
				row.VoidAmountVnd.Int64, row.AppliedAmountVnd)
		}
		voidedVND, err = AddCharge(voidedVND, row.VoidAmountVnd.Int64)
		if err != nil {
			return 0, 0, fmt.Errorf("%w: void amounts: %v",
				ErrFinancialInvariantViolated, err)
		}
	}
	return originalVND, voidedVND, nil
}

// loadCheckAdjustments returns one Check's live Charge Adjustments and their
// summed amount. The query excludes POST_SALE adjustments structurally, so a
// closed sale's later history never rewrites this projection.
func loadCheckAdjustments(ctx context.Context, q *sqlc.Queries, checkID uuid.UUID,
	mode SnapshotMode,
) ([]ChargeAdjustmentResponse, int64, error) {
	rows, err := q.ListCheckChargeAdjustments(ctx, checkID)
	if err != nil {
		return nil, 0, fmt.Errorf("load check charge adjustments: %w", err)
	}

	adjustmentIDs := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		adjustmentIDs = append(adjustmentIDs, row.ID)
	}
	allocated, err := loadAdjustmentRefundAllocations(ctx, q, adjustmentIDs)
	if err != nil {
		return nil, 0, err
	}

	out := make([]ChargeAdjustmentResponse, 0, len(rows))
	var totalVND int64
	for _, row := range rows {
		totalVND, err = AddCharge(totalVND, row.AmountVnd)
		if err != nil {
			return nil, 0, fmt.Errorf("%w: check %s adjustments: %v",
				ErrFinancialInvariantViolated, checkID, err)
		}
		remainingVND, err := remainingRefundableVND(row.AmountVnd, allocated[row.ID], mode)
		if err != nil {
			return nil, 0, fmt.Errorf("adjustment %s: %w", row.ID, err)
		}
		out = append(out, ChargeAdjustmentResponse{
			ID:                     row.ID,
			Kind:                   row.Kind,
			Scope:                  row.Scope,
			PreparationUnitID:      row.PreparationUnitID,
			PreparationWasteID:     nullUUIDPtr(row.PreparationWasteID),
			ChargeAllocationID:     row.ChargeAllocationID,
			CompletedSaleID:        nullUUIDPtr(row.CompletedSaleID),
			SalesShiftID:           row.SalesShiftID,
			AmountVND:              row.AmountVnd,
			RemainingRefundableVND: remainingVND,
			CreatedAt:              row.CreatedAt,
		})
	}
	return out, totalVND, nil
}

// loadCheckRefunds returns one Check's live Refunds with their derived state
// and both allocation collections, and the summed amount money has actually
// left. The query excludes post-sale Refunds structurally. Every Refund's
// amount must equal each of its allocation sums.
func loadCheckRefunds(ctx context.Context, q *sqlc.Queries, checkID uuid.UUID) (
	[]RefundResponse, int64, error,
) {
	rows, err := q.ListCheckRefunds(ctx, checkID)
	if err != nil {
		return nil, 0, fmt.Errorf("load check refunds: %w", err)
	}
	completedVND, err := sumCompletedRefundsVND(rows)
	if err != nil {
		return nil, 0, fmt.Errorf("check %s: %w", checkID, err)
	}

	out := make([]RefundResponse, 0, len(rows))
	for _, row := range rows {
		paymentRows, err := q.ListRefundPaymentAllocations(ctx, row.ID)
		if err != nil {
			return nil, 0, fmt.Errorf("load refund payment allocations: %w", err)
		}
		adjustmentRows, err := q.ListRefundAdjustmentAllocations(ctx, row.ID)
		if err != nil {
			return nil, 0, fmt.Errorf("load refund adjustment allocations: %w", err)
		}
		if err := assertRefundAllocationSums(row, paymentRows, adjustmentRows); err != nil {
			return nil, 0, err
		}

		refund := RefundResponse{
			ID:                        row.ID,
			CheckID:                   row.CheckID,
			CompletedSaleID:           nullUUIDPtr(row.CompletedSaleID),
			SalesShiftID:              row.SalesShiftID,
			Method:                    row.Method,
			AmountVND:                 row.AmountVnd,
			State:                     RefundStatePending,
			Reason:                    row.Reason,
			Note:                      nullStringPtr(row.Note),
			ActorStaffIdentityID:      row.ActorStaffIdentityID,
			ApprovedByStaffIdentityID: row.ApprovedByStaffIdentityID,
			CreatedAt:                 row.CreatedAt,
			PaymentAllocations:        make([]RefundAllocationResponse, 0, len(paymentRows)),
			AdjustmentAllocations:     make([]RefundAllocationResponse, 0, len(adjustmentRows)),
		}
		for _, allocation := range paymentRows {
			refund.PaymentAllocations = append(refund.PaymentAllocations,
				RefundAllocationResponse{ID: allocation.PaymentID, AmountVND: allocation.AmountVnd})
		}
		for _, allocation := range adjustmentRows {
			refund.AdjustmentAllocations = append(refund.AdjustmentAllocations,
				RefundAllocationResponse{
					ID:        allocation.ChargeAdjustmentID,
					AmountVND: allocation.AmountVnd,
				})
		}
		if row.CompletionID.Valid {
			refund.State = RefundStateCompleted
			refund.Completion = &RefundCompletionResponse{
				ID:                            row.CompletionID.UUID,
				TransactionReference:          nullStringPtr(row.TransactionReference),
				CompletedByStaffIdentityID:    row.CompletedByStaffIdentityID.UUID,
				CompletedStaffAccessSessionID: row.CompletedStaffAccessSessionID.UUID,
				CompletedAt:                   row.CompletedAt.Time,
			}
		}
		out = append(out, refund)
	}
	return out, completedVND, nil
}

// sumCompletedRefundsVND totals the Refunds whose money has actually left.
// Pending Manual QR intents contribute nothing until their completion exists.
func sumCompletedRefundsVND(rows []sqlc.ListCheckRefundsRow) (int64, error) {
	var completedVND int64
	for _, row := range rows {
		if !row.CompletionID.Valid {
			continue
		}
		next, err := AddCharge(completedVND, row.AmountVnd)
		if err != nil {
			return 0, fmt.Errorf("%w: completed refunds: %v",
				ErrFinancialInvariantViolated, err)
		}
		completedVND = next
	}
	return completedVND, nil
}

// assertRefundAllocationSums enforces refund.amount_vnd = sum(payment
// allocations) = sum(adjustment allocations) on the persisted facts.
func assertRefundAllocationSums(row sqlc.ListCheckRefundsRow,
	paymentRows []sqlc.ListRefundPaymentAllocationsRow,
	adjustmentRows []sqlc.ListRefundAdjustmentAllocationsRow,
) error {
	var paymentVND, adjustmentVND int64
	var err error
	for _, allocation := range paymentRows {
		paymentVND, err = AddCharge(paymentVND, allocation.AmountVnd)
		if err != nil {
			return fmt.Errorf("%w: refund %s payment allocations: %v",
				ErrFinancialInvariantViolated, row.ID, err)
		}
	}
	for _, allocation := range adjustmentRows {
		adjustmentVND, err = AddCharge(adjustmentVND, allocation.AmountVnd)
		if err != nil {
			return fmt.Errorf("%w: refund %s adjustment allocations: %v",
				ErrFinancialInvariantViolated, row.ID, err)
		}
	}
	if paymentVND != row.AmountVnd || adjustmentVND != row.AmountVnd {
		return fmt.Errorf(
			"%w: refund %s amount %d, payment allocations %d, adjustment allocations %d",
			ErrFinancialInvariantViolated, row.ID, row.AmountVnd, paymentVND, adjustmentVND)
	}
	return nil
}

// loadPaymentRefundAllocations groups every Refund allocation against the given
// Payments by Payment, including pending Manual QR intents: they reserve
// capacity before money moves. The caller applies the structural snapshot rule
// through the SnapshotMode.
func loadPaymentRefundAllocations(ctx context.Context, q *sqlc.Queries, paymentIDs []uuid.UUID) (
	map[uuid.UUID][]sourceAllocation, error,
) {
	byID := make(map[uuid.UUID][]sourceAllocation, len(paymentIDs))
	if len(paymentIDs) == 0 {
		return byID, nil
	}
	rows, err := q.ListPaymentRefundAllocations(ctx, paymentIDs)
	if err != nil {
		return nil, fmt.Errorf("load payment refund allocations: %w", err)
	}
	for _, row := range rows {
		byID[row.PaymentID] = append(byID[row.PaymentID], sourceAllocation{
			AmountVND:             row.AmountVnd,
			RefundCompletedSaleID: row.RefundCompletedSaleID,
		})
	}
	return byID, nil
}

// loadAdjustmentRefundAllocations groups every Refund allocation against the
// given Charge Adjustments by Adjustment, with the same capacity-reservation
// rule as Payments.
func loadAdjustmentRefundAllocations(ctx context.Context, q *sqlc.Queries,
	adjustmentIDs []uuid.UUID,
) (map[uuid.UUID][]sourceAllocation, error) {
	byID := make(map[uuid.UUID][]sourceAllocation, len(adjustmentIDs))
	if len(adjustmentIDs) == 0 {
		return byID, nil
	}
	rows, err := q.ListAdjustmentRefundAllocations(ctx, adjustmentIDs)
	if err != nil {
		return nil, fmt.Errorf("load adjustment refund allocations: %w", err)
	}
	for _, row := range rows {
		byID[row.ChargeAdjustmentID] = append(byID[row.ChargeAdjustmentID], sourceAllocation{
			AmountVND:             row.AmountVnd,
			RefundCompletedSaleID: row.RefundCompletedSaleID,
		})
	}
	return byID, nil
}

// loadCheckAllocations returns one Check's allocations and their summed amount.
func loadCheckAllocations(ctx context.Context, q *sqlc.Queries, checkID uuid.UUID) (
	[]ChargeAllocationResponse, int64, error,
) {
	rows, err := q.ListCheckAllocations(ctx, checkID)
	if err != nil {
		return nil, 0, fmt.Errorf("load check allocations: %w", err)
	}

	itemIDs := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		itemIDs = append(itemIDs, row.CommittedItemID)
	}
	modifiers, err := loadCommittedModifiers(ctx, q, itemIDs)
	if err != nil {
		return nil, 0, err
	}
	submitted, err := loadSubmittedItems(ctx, q, itemIDs)
	if err != nil {
		return nil, 0, err
	}

	out := make([]ChargeAllocationResponse, 0, len(rows))
	var totalVND int64
	for _, row := range rows {
		amountVND, err := LineTotal(row.AllocatedQuantity, row.UnitPriceVnd)
		if err != nil {
			return nil, 0, err
		}
		totalVND, err = AddCharge(totalVND, amountVND)
		if err != nil {
			return nil, 0, err
		}

		mods := modifiers[row.CommittedItemID]
		if mods == nil {
			mods = make([]CommittedModifierResponse, 0)
		}
		_, isSubmitted := submitted[row.CommittedItemID]
		out = append(out, ChargeAllocationResponse{
			ID:                row.ID,
			CommittedItemID:   row.CommittedItemID,
			MenuItemID:        row.MenuItemID,
			CategoryName:      row.CategoryName,
			Name:              row.ItemName,
			SizeName:          nullStringPtr(row.SizeName),
			PreparationNote:   nullStringPtr(row.PreparationNote),
			Modifiers:         mods,
			CommittedQuantity: row.CommittedQuantity,
			CommittedTotalVND: row.CommittedTotalVnd,
			AllocatedQuantity: row.AllocatedQuantity,
			AmountVND:         amountVND,
			CreatedAt:         row.CreatedAt,
			Submitted:         isSubmitted,
		})
	}
	return out, totalVND, nil
}

// loadCommittedModifiers groups frozen modifier snapshots by Committed Item.
// The query orders by (group name, option name), so presentation order comes
// from the read rather than from insert order — the table carries no ordering
// column, exactly as 5A's selected options do not.
func loadCommittedModifiers(ctx context.Context, q *sqlc.Queries, itemIDs []uuid.UUID) (
	map[uuid.UUID][]CommittedModifierResponse, error,
) {
	out := make(map[uuid.UUID][]CommittedModifierResponse, len(itemIDs))
	if len(itemIDs) == 0 {
		return out, nil
	}
	rows, err := q.ListCommittedItemModifiers(ctx, itemIDs)
	if err != nil {
		return nil, fmt.Errorf("load committed item modifiers: %w", err)
	}
	for _, row := range rows {
		out[row.CommittedItemID] = append(out[row.CommittedItemID], CommittedModifierResponse{
			GroupID:      row.ModifierGroupID,
			GroupName:    row.ModifierGroupName,
			OptionID:     row.ModifierOptionID,
			OptionName:   row.ModifierOptionName,
			SurchargeVND: row.SurchargeVnd,
		})
	}
	return out, nil
}

// loadOrders assembles the Session's Orders with their items.
func loadOrders(ctx context.Context, q *sqlc.Queries, sessionID uuid.UUID) (
	[]OrderResponse, error,
) {
	rows, err := q.ListSessionOrders(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("load session orders: %w", err)
	}
	out := make([]OrderResponse, 0, len(rows))
	if len(rows) == 0 {
		return out, nil
	}

	orderIDs := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		orderIDs = append(orderIDs, row.ID)
	}
	itemRows, err := q.ListOrderItems(ctx, orderIDs)
	if err != nil {
		return nil, fmt.Errorf("load order items: %w", err)
	}
	itemsByOrder := make(map[uuid.UUID][]OrderItemResponse, len(rows))
	for _, item := range itemRows {
		itemsByOrder[item.OrderID] = append(itemsByOrder[item.OrderID], OrderItemResponse{
			ID:              item.ID,
			CommittedItemID: item.CommittedItemID,
		})
	}

	for _, row := range rows {
		items := itemsByOrder[row.ID]
		if items == nil {
			items = make([]OrderItemResponse, 0)
		}
		out = append(out, OrderResponse{
			ID:                 row.ID,
			OrderDraftID:       row.OrderDraftID,
			SubmittedByStaffID: row.SubmittedByStaffIdentityID,
			SubmittedSessionID: row.SubmittedStaffAccessSessionID,
			SubmittedAt:        row.SubmittedAt,
			Items:              items,
		})
	}
	return out, nil
}

// loadPreparationUnits assembles the Session's Preparation Units.
//
// internal/sales reads unit state here and creates units at Submit; every
// state transition belongs to internal/preparation (ADR-024). The read is the
// one query feeding both the live Service Session and the Completed Sale unit
// projections, so the Phase 6B Remake metadata maps once: originals are
// STANDARD with a null link, a linked replacement is REMAKE pointing at its
// wasted source.
func loadPreparationUnits(ctx context.Context, q *sqlc.Queries, sessionID uuid.UUID) (
	[]PreparationUnitResponse, error,
) {
	rows, err := q.ListSessionPreparationUnits(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("load preparation units: %w", err)
	}
	out := make([]PreparationUnitResponse, 0, len(rows))
	for _, row := range rows {
		mods := make([]UnitModifierResponse, 0)
		if len(row.Modifiers) > 0 {
			if err := json.Unmarshal(row.Modifiers, &mods); err != nil {
				return nil, fmt.Errorf("decode preparation unit modifiers: %w", err)
			}
			// A literal JSON null in the column unmarshals to nil; restore the
			// non-nil empty slice the projection contract promises.
			if mods == nil {
				mods = make([]UnitModifierResponse, 0)
			}
		}
		out = append(out, PreparationUnitResponse{
			ID:                        row.ID,
			OrderItemID:               row.OrderItemID,
			UnitNumber:                row.UnitNumber,
			State:                     row.State,
			ServiceNumber:             row.ServiceNumber,
			CategoryName:              row.CategoryName,
			ItemName:                  row.ItemName,
			SizeName:                  nullStringPtr(row.SizeName),
			Modifiers:                 mods,
			PreparationNote:           nullStringPtr(row.PreparationNote),
			QueuedAt:                  row.QueuedAt,
			Priority:                  row.Priority,
			RemakeOfPreparationUnitID: nullUUIDPtr(row.RemakeOfPreparationUnitID),
		})
	}
	return out, nil
}

// nullUUIDPtr converts a nullable UUID column to an optional id pointer.
func nullUUIDPtr(v uuid.NullUUID) *uuid.UUID {
	if !v.Valid {
		return nil
	}
	id := v.UUID
	return &id
}

// loadSubmittedItems returns the set of Committed Items that have entered an
// Order, which is what a Charge Allocation's `submitted` flag reports.
func loadSubmittedItems(ctx context.Context, q *sqlc.Queries, itemIDs []uuid.UUID) (
	map[uuid.UUID]struct{}, error,
) {
	out := make(map[uuid.UUID]struct{})
	if len(itemIDs) == 0 {
		return out, nil
	}
	rows, err := q.ListSubmittedCommittedItems(ctx, itemIDs)
	if err != nil {
		return nil, fmt.Errorf("load submitted committed items: %w", err)
	}
	for _, id := range rows {
		out[id] = struct{}{}
	}
	return out, nil
}
