//go:build integration

package preparation_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/preparation"
	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCancelRaces pins the Cancellation concurrency contract of design spec
// §11: each subtest releases two commands simultaneously at their shared lock
// boundary, accepts only the explicitly documented outcomes, and afterwards
// proves the losing side left no partial adjustment, fact, alert, audit, or
// stored idempotency result behind. Only the Cancel-versus-Submit pairing may
// surface PostgreSQL 40P01 (ADR-031); every other pairing treats that abort as
// a failure. A blocked pair that never resolves fails the test instead of
// hanging it.
func TestCancelRaces(t *testing.T) {
	t.Run("cancel versus advance", func(t *testing.T) {
		env := newCancelEnv(t)
		unit := env.SubmittedUnits(t, 1)[0]
		check := env.CheckForUnit(t, unit.ID)
		require.EqualValues(t, 25000, check.ChargeVND)

		requestID := uuid.New()
		cancelCmd := preparation.CancelUnitsCommand{
			RequestID:          requestID,
			PreparationUnitIDs: []uuid.UUID{unit.ID},
			Kind:               preparation.CancelKindCancellation,
			Reason:             preparation.ReasonCustomerRequest,
		}
		advanceCmd := preparation.AdvanceUnitCommand{
			RequestID:   uuid.New(),
			UnitID:      unit.ID,
			TargetState: preparation.StateInPreparation,
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		var cancelErr, advanceErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, _, cancelErr = preparation.NewCancelUnitsHandler(env.PreparationRunner).
				Handle(context.Background(), env.CashierActor(), cancelCmd)
		}()
		go func() {
			defer wg.Done()
			<-start
			_, _, advanceErr = preparation.NewAdvanceUnitHandler(env.PreparationRunner).
				Handle(context.Background(), env.BaristaActor(), advanceCmd)
		}()
		close(start)
		requireRaceResolved(t, &wg)

		require.False(t, raceDeadlock(cancelErr),
			"Cancel locks Check, Session, then the unit; advance locks only the unit, so no AB-BA exists: %v", cancelErr)
		require.False(t, raceDeadlock(advanceErr),
			"Cancel and advance cannot deadlock: %v", advanceErr)
		require.True(t, cancelErr == nil || advanceErr == nil,
			"exactly one lifecycle result must stand: cancel=%v advance=%v", cancelErr, advanceErr)

		if cancelErr == nil {
			require.ErrorIs(t, advanceErr, preparation.ErrInvalidTransition,
				"the cancelled unit admits no advance")
			assert.Equal(t, preparation.StateCancelled, env.UnitState(t, unit.ID))
			assert.Equal(t, 1, env.CountCancellations(t, unit.ID))
			assert.Equal(t, 1, env.CountAdjustments(t, unit.ID))
			assert.Equal(t, 1, env.CountAlerts(t, unit.ID))
			assert.Equal(t, 1, env.CountTransitionsTo(t, unit.ID, preparation.StateCancelled))
			assert.EqualValues(t, 0, env.CheckForUnit(t, unit.ID).ChargeVND)
		} else {
			require.ErrorIs(t, cancelErr, preparation.ErrCancellationSourceNotQueued)
			require.NoError(t, advanceErr)
			assert.Equal(t, preparation.StateInPreparation, env.UnitState(t, unit.ID))
			assert.Equal(t, 0, env.CountCancellations(t, unit.ID))
			assert.Equal(t, 0, env.CountAdjustments(t, unit.ID))
			assert.Equal(t, 0, env.CountAlerts(t, unit.ID))
			assert.EqualValues(t, check.ChargeVND, env.CheckForUnit(t, unit.ID).ChargeVND)
		}

		// A losing Cancel rolls its whole transaction back, claim included.
		_, claimed := env.IdempotencyClaim(t, env.CashierActor(), requestID)
		assert.Equal(t, cancelErr == nil, claimed,
			"the idempotency claim exists exactly when the Cancellation stood")
	})

	t.Run("cancel versus waste", func(t *testing.T) {
		env := newCancelEnv(t)
		unit := env.SubmittedUnits(t, 1)[0]
		_, _, err := env.Advance(t, unit.ID, preparation.StateInPreparation)
		require.NoError(t, err)
		_, _, err = env.Advance(t, unit.ID, preparation.StateReady)
		require.NoError(t, err)
		check := env.CheckForUnit(t, unit.ID)

		requestID := uuid.New()
		cancelCmd := preparation.CancelUnitsCommand{
			RequestID:          requestID,
			PreparationUnitIDs: []uuid.UUID{unit.ID},
			Kind:               preparation.CancelKindCancellation,
			Reason:             preparation.ReasonCustomerRequest,
		}
		wasteCmd := preparation.WasteUnitCommand{
			RequestID: uuid.New(),
			UnitID:    unit.ID,
			Reason:    preparation.ReasonQualityFailure,
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		var cancelErr, wasteErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, _, cancelErr = preparation.NewCancelUnitsHandler(env.PreparationRunner).
				Handle(context.Background(), env.CashierActor(), cancelCmd)
		}()
		go func() {
			defer wg.Done()
			<-start
			_, _, wasteErr = preparation.NewWasteUnitHandler(env.PreparationRunner).
				Handle(context.Background(), env.BaristaActor(), wasteCmd)
		}()
		close(start)
		requireRaceResolved(t, &wg)

		// Cancellation is admissible only from QUEUED while Waste is
		// admissible only from IN_PREPARATION/READY, so the READY unit has one
		// lifecycle winner and the Cancel side is rejected before touching any
		// lock. It must not deadlock and must write nothing.
		require.NoError(t, wasteErr, "the Wasted lifecycle result must stand")
		require.ErrorIs(t, cancelErr, preparation.ErrCancellationSourceNotQueued)
		require.False(t, raceDeadlock(cancelErr))
		assert.Equal(t, preparation.StateWasted, env.UnitState(t, unit.ID))
		assert.Equal(t, 1, env.CountWastes(t, unit.ID))
		assert.Equal(t, 1, env.CountAlerts(t, unit.ID))
		assert.Equal(t, 1, env.CountTransitionsTo(t, unit.ID, preparation.StateWasted))
		assert.Equal(t, 0, env.CountCancellations(t, unit.ID))
		assert.Equal(t, 0, env.CountAdjustments(t, unit.ID))
		assert.EqualValues(t, check.ChargeVND, env.CheckForUnit(t, unit.ID).ChargeVND,
			"Waste changes no charge")

		_, claimed := env.IdempotencyClaim(t, env.CashierActor(), requestID)
		assert.False(t, claimed, "the rejected Cancellation leaves no claim")
	})

	t.Run("cancel versus payment", func(t *testing.T) {
		env := newCancelEnv(t)
		unit := env.SubmittedUnits(t, 1)[0]
		sessionID := env.SessionIDForUnit(t, unit.ID)
		check := env.CheckForUnit(t, unit.ID)
		require.Equal(t, sales.CheckStateOpen, check.State)

		requestID := uuid.New()
		cancelCmd := preparation.CancelUnitsCommand{
			RequestID:          requestID,
			PreparationUnitIDs: []uuid.UUID{unit.ID},
			Kind:               preparation.CancelKindCancellation,
			Reason:             preparation.ReasonCustomerRequest,
		}
		payCmd := sales.PayCashCommand{
			RequestID:        uuid.New(),
			CheckID:          check.ID,
			AppliedAmountVND: 25000,
			CashTenderedVND:  25000,
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		var cancelErr, payErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, _, cancelErr = preparation.NewCancelUnitsHandler(env.PreparationRunner).
				Handle(context.Background(), env.CashierActor(), cancelCmd)
		}()
		go func() {
			defer wg.Done()
			<-start
			_, _, payErr = sales.NewPayCashHandler(env.SalesRunner).
				Handle(context.Background(), env.salesActor(env.manager), payCmd)
		}()
		close(start)
		requireRaceResolved(t, &wg)

		// Both commands lock the Check first, so they serialize; Cancel always
		// commits and the Payment wins only by committing before it, in which
		// case the reduced-and-settled Check rejects the receipt cleanly.
		require.NoError(t, cancelErr, "Cancellation must not fail against a Payment")
		require.False(t, raceDeadlock(payErr))
		if payErr != nil {
			require.ErrorIs(t, payErr, sales.ErrCheckNotOpen)
		}

		after := env.CheckForUnit(t, unit.ID)
		assert.EqualValues(t, 0, after.ChargeVND)
		assert.Equal(t, sales.CheckStateSettled, after.State)
		assert.Equal(t, 1, env.CountCancellations(t, unit.ID))
		assert.Equal(t, 1, env.CountAdjustments(t, unit.ID))
		assert.Equal(t, 1, env.CountAlerts(t, unit.ID))

		projected := env.ProjectedCheck(t, sessionID, check.ID)
		if payErr == nil {
			assert.EqualValues(t, 25000, projected.EffectiveReceivedVND)
			assert.EqualValues(t, 25000, projected.PendingRefundVND)
		} else {
			assert.EqualValues(t, 0, projected.EffectiveReceivedVND)
			assert.EqualValues(t, 0, projected.PendingRefundVND)
		}
		assert.EqualValues(t, 0, projected.BalanceVND,
			"a cancelled charge leaves no customer balance either way")
	})

	t.Run("cancel versus split", func(t *testing.T) {
		env := newCancelEnv(t)
		units := env.SubmittedUnits(t, 2)
		target := unitByNumber(t, units, 2)
		sessionID := env.SessionIDForUnit(t, target.ID)
		projection := env.Projection(t, sessionID)
		require.Len(t, projection.Checks, 1)
		checkID := projection.Checks[0].ID
		allocation := projection.Checks[0].Allocations[0]

		requestID := uuid.New()
		cancelCmd := preparation.CancelUnitsCommand{
			RequestID:          requestID,
			PreparationUnitIDs: []uuid.UUID{target.ID},
			Kind:               preparation.CancelKindCancellation,
			Reason:             preparation.ReasonCustomerRequest,
		}
		splitCmd := sales.SplitCheckCommand{
			RequestID:     uuid.New(),
			SourceCheckID: checkID,
			Destination:   sales.SplitDestination{Type: sales.SplitDestinationNewCheck},
			Items: []sales.SplitItem{
				{CommittedItemID: allocation.CommittedItemID, Quantity: 1},
			},
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		var cancelErr, splitErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, _, cancelErr = preparation.NewCancelUnitsHandler(env.PreparationRunner).
				Handle(context.Background(), env.CashierActor(), cancelCmd)
		}()
		go func() {
			defer wg.Done()
			<-start
			_, _, splitErr = sales.NewSplitCheckHandler(env.SalesRunner).
				Handle(context.Background(), env.salesActor(env.manager), splitCmd)
		}()
		close(start)
		requireRaceResolved(t, &wg)

		require.False(t, raceDeadlock(cancelErr))
		require.False(t, raceDeadlock(splitErr))
		require.True(t, cancelErr == nil || splitErr == nil,
			"the Check lock serializes Cancel and Split: cancel=%v split=%v", cancelErr, splitErr)

		switch {
		case cancelErr == nil && splitErr == nil:
			// The split committed wholly before the Cancellation resolved its
			// mapping; the Cancellation then corrected the destination Check.
			require.Len(t, env.Projection(t, sessionID).Checks, 2)
			assert.Equal(t, preparation.StateCancelled, env.UnitState(t, target.ID))
			assert.Equal(t, 1, env.CountCancellations(t, target.ID))
			assert.Equal(t, 1, env.CountAdjustments(t, target.ID))
			assertCheckChargeEquation(t, env, sessionID, 25000)
		case cancelErr == nil:
			require.ErrorIs(t, splitErr, sales.ErrCheckHasChargeAdjustment,
				"once an adjustment exists, restructuring must reject")
			assert.Equal(t, preparation.StateCancelled, env.UnitState(t, target.ID))
			assert.Equal(t, 1, env.CountCancellations(t, target.ID))
			assert.Equal(t, 1, env.CountAdjustmentsForCheck(t, checkID))
			assert.EqualValues(t, 25000, env.CheckForUnit(t, target.ID).ChargeVND)
		default:
			require.ErrorIs(t, cancelErr, preparation.ErrChargeAdjustmentConflict,
				"the moved allocation changed the target unit's mapping")
			require.NoError(t, splitErr)
			assert.Equal(t, preparation.StateQueued, env.UnitState(t, target.ID))
			assert.Equal(t, 0, env.CountCancellations(t, target.ID))
			assert.Equal(t, 0, env.CountAdjustments(t, target.ID))
			require.Len(t, env.Projection(t, sessionID).Checks, 2,
				"the split created its destination check")
			assertCheckChargeEquation(t, env, sessionID, 50000)
		}

		_, claimed := env.IdempotencyClaim(t, env.CashierActor(), requestID)
		assert.Equal(t, cancelErr == nil, claimed)
	})

	t.Run("cancel versus merge", func(t *testing.T) {
		env := newCancelEnv(t)
		units := env.SubmittedUnits(t, 2)
		target := unitByNumber(t, units, 2)
		sessionID := env.SessionIDForUnit(t, target.ID)
		projection := env.Projection(t, sessionID)
		require.Len(t, projection.Checks, 1)
		sourceCheckID := projection.Checks[0].ID
		allocation := projection.Checks[0].Allocations[0]

		// Pre-split the second unit onto its own Check, so the race below is
		// between Cancellation and a Merge that absorbs the target's Check.
		_, _, err := sales.NewSplitCheckHandler(env.SalesRunner).Handle(
			context.Background(), env.salesActor(env.manager), sales.SplitCheckCommand{
				RequestID:     uuid.New(),
				SourceCheckID: sourceCheckID,
				Destination:   sales.SplitDestination{Type: sales.SplitDestinationNewCheck},
				Items: []sales.SplitItem{
					{CommittedItemID: allocation.CommittedItemID, Quantity: 1},
				},
			})
		require.NoError(t, err)
		afterSplit := env.Projection(t, sessionID)
		require.Len(t, afterSplit.Checks, 2)
		absorbedCheckID := uuid.Nil
		for _, check := range afterSplit.Checks {
			if check.ID != sourceCheckID {
				absorbedCheckID = check.ID
			}
		}
		require.NotEqual(t, uuid.Nil, absorbedCheckID)

		requestID := uuid.New()
		cancelCmd := preparation.CancelUnitsCommand{
			RequestID:          requestID,
			PreparationUnitIDs: []uuid.UUID{target.ID},
			Kind:               preparation.CancelKindCancellation,
			Reason:             preparation.ReasonCustomerRequest,
		}
		mergeCmd := sales.MergeChecksCommand{
			RequestID:        uuid.New(),
			SurvivingCheckID: sourceCheckID,
			AbsorbedCheckID:  absorbedCheckID,
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		var cancelErr, mergeErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, _, cancelErr = preparation.NewCancelUnitsHandler(env.PreparationRunner).
				Handle(context.Background(), env.CashierActor(), cancelCmd)
		}()
		go func() {
			defer wg.Done()
			<-start
			_, _, mergeErr = sales.NewMergeChecksHandler(env.SalesRunner).
				Handle(context.Background(), env.salesActor(env.manager), mergeCmd)
		}()
		close(start)
		requireRaceResolved(t, &wg)

		require.False(t, raceDeadlock(cancelErr))
		require.False(t, raceDeadlock(mergeErr))
		require.True(t, cancelErr == nil || mergeErr == nil,
			"the Check lock serializes Cancel and Merge: cancel=%v merge=%v", cancelErr, mergeErr)

		switch {
		case cancelErr == nil && mergeErr == nil:
			// The merge committed wholly before the Cancellation resolved its
			// mapping; the Cancellation then corrected the surviving Check.
			merged := env.Projection(t, sessionID)
			require.Len(t, merged.Checks, 2, "a merged Check stays projected as MERGED")
			assert.Equal(t, preparation.StateCancelled, env.UnitState(t, target.ID))
			assert.Equal(t, 1, env.CountCancellations(t, target.ID))
			assert.Equal(t, 1, env.CountAdjustments(t, target.ID))
			assertCheckChargeEquation(t, env, sessionID, 25000)
		case cancelErr == nil:
			// The Cancellation settled the absorbed Check and left a live
			// adjustment, so restructuring rejects on either the OPEN-only
			// precondition or the live-adjustment guard.
			require.True(t,
				errors.Is(mergeErr, sales.ErrCheckNotOpen) ||
					errors.Is(mergeErr, sales.ErrCheckHasChargeAdjustment),
				"a Cancellation must block the Merge, got %v", mergeErr)
			assert.Equal(t, preparation.StateCancelled, env.UnitState(t, target.ID))
			assert.Equal(t, 1, env.CountCancellations(t, target.ID))
			assert.Equal(t, 1, env.CountAdjustmentsForCheck(t, absorbedCheckID))
		default:
			require.ErrorIs(t, cancelErr, preparation.ErrChargeAdjustmentConflict,
				"the merged allocation moved the target unit's mapping")
			require.NoError(t, mergeErr)
			assert.Equal(t, preparation.StateQueued, env.UnitState(t, target.ID))
			assert.Equal(t, 0, env.CountCancellations(t, target.ID))
			assert.Equal(t, 0, env.CountAdjustmentsForCheck(t, absorbedCheckID))
			require.Len(t, env.Projection(t, sessionID).Checks, 2,
				"a merged Check stays projected as MERGED")
			assertCheckChargeEquation(t, env, sessionID, 50000)
		}

		_, claimed := env.IdempotencyClaim(t, env.CashierActor(), requestID)
		assert.Equal(t, cancelErr == nil, claimed)
	})

	t.Run("cancel versus submit", func(t *testing.T) {
		env := newCancelEnv(t)
		unit := env.SubmittedUnits(t, 1)[0]
		sessionID := env.SessionIDForUnit(t, unit.ID)
		check := env.CheckForUnit(t, unit.ID)
		env.commitPendingRound(t, sessionID, 1)
		chargeBefore := env.CheckForUnit(t, unit.ID).ChargeVND
		require.EqualValues(t, check.ChargeVND+25000, chargeBefore,
			"the newly committed round joins the Session's open Check")

		cancelRequestID := uuid.New()
		cancelCmd := preparation.CancelUnitsCommand{
			RequestID:          cancelRequestID,
			PreparationUnitIDs: []uuid.UUID{unit.ID},
			Kind:               preparation.CancelKindCancellation,
			Reason:             preparation.ReasonCustomerRequest,
		}
		submitRequestID := uuid.New()
		submitCmd := sales.SubmitOrderCommand{
			RequestID:        submitRequestID,
			ServiceSessionID: sessionID,
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		var cancelErr, submitErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, _, cancelErr = preparation.NewCancelUnitsHandler(env.PreparationRunner).
				Handle(context.Background(), env.CashierActor(), cancelCmd)
		}()
		go func() {
			defer wg.Done()
			<-start
			_, _, submitErr = sales.NewSubmitOrderHandler(env.SalesRunner).
				Handle(context.Background(), env.salesActor(env.manager), submitCmd)
		}()
		close(start)
		requireRaceResolved(t, &wg)

		// ADR-031's AB-BA window: either side may abort with 40P01, but no
		// business rejection is documented for this pairing and at least one
		// side must stand.
		if cancelErr != nil {
			require.True(t, raceDeadlock(cancelErr),
				"the only accepted Cancel failure here is the 40P01 abort: %v", cancelErr)
		}
		if submitErr != nil {
			require.True(t, raceDeadlock(submitErr),
				"the only accepted Submit failure here is the 40P01 abort: %v", submitErr)
		}
		require.True(t, cancelErr == nil || submitErr == nil,
			"the aborted side must leave the other standing: cancel=%v submit=%v", cancelErr, submitErr)

		orders := env.countOrders(t, sessionID)
		adjustments := env.CountAdjustmentsForCheck(t, check.ID)
		if submitErr == nil {
			assert.Equal(t, 2, orders, "the second round became an Order")
		} else {
			assert.Equal(t, 1, orders, "the aborted Submit left no Order behind")
		}
		if cancelErr == nil {
			assert.Equal(t, 1, adjustments, "the Cancellation adjustment committed")
		} else {
			assert.Equal(t, 0, adjustments, "the aborted Cancellation left no adjustment")
		}
		expectedCharge := chargeBefore
		if cancelErr == nil {
			expectedCharge -= 25000
		}
		assert.EqualValues(t, expectedCharge, env.CheckForUnit(t, unit.ID).ChargeVND,
			"the stored charge reflects exactly the winners' writes")

		_, cancelClaimed := env.IdempotencyClaim(t, env.CashierActor(), cancelRequestID)
		assert.Equal(t, cancelErr == nil, cancelClaimed,
			"the aborted Cancellation rolls its claim back")
		_, submitClaimed := env.IdempotencyClaim(t, env.manager, submitRequestID)
		assert.Equal(t, submitErr == nil, submitClaimed,
			"the aborted Submit rolls its claim back")
	})

	t.Run("cancel versus closure on an unpaid check", func(t *testing.T) {
		env := newCancelEnv(t)
		unit := env.SubmittedUnits(t, 1)[0]
		sessionID := env.SessionIDForUnit(t, unit.ID)
		check := env.CheckForUnit(t, unit.ID)
		require.Equal(t, sales.CheckStateOpen, check.State)

		requestID := uuid.New()
		cancelCmd := preparation.CancelUnitsCommand{
			RequestID:          requestID,
			PreparationUnitIDs: []uuid.UUID{unit.ID},
			Kind:               preparation.CancelKindCancellation,
			Reason:             preparation.ReasonCustomerRequest,
		}
		closeCmd := sales.CloseServiceSessionCommand{
			RequestID:        uuid.New(),
			ServiceSessionID: sessionID,
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		var cancelErr, closeErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, _, cancelErr = preparation.NewCancelUnitsHandler(env.PreparationRunner).
				Handle(context.Background(), env.CashierActor(), cancelCmd)
		}()
		go func() {
			defer wg.Done()
			<-start
			_, _, closeErr = sales.NewCloseServiceSessionHandler(env.SalesRunner).
				Handle(context.Background(), env.salesActor(env.manager), closeCmd)
		}()
		close(start)
		requireRaceResolved(t, &wg)

		// Closure cannot pass while the source unit is QUEUED and its Check is
		// unsettled. Cancellation always commits; closure either observes the
		// pre-cancel open Check (unsettled precedence) or the post-cancel
		// terminal unit and settled Check and commits too.
		require.NoError(t, cancelErr)
		require.False(t, raceDeadlock(closeErr))
		if closeErr != nil {
			require.ErrorIs(t, closeErr, sales.ErrCheckNotSettledForClosure)
			assert.Equal(t, sales.StateActive, env.Projection(t, sessionID).State)
		} else {
			assert.Equal(t, sales.StateClosed, env.Projection(t, sessionID).State)
			_, _, saleErr := sales.NewGetCompletedSaleBySessionHandler(env.SalesRunner).
				Handle(context.Background(), env.salesActor(env.manager), sessionID)
			require.NoError(t, saleErr, "closure must have recorded its Completed Sale")
		}
		assert.Equal(t, preparation.StateCancelled, env.UnitState(t, unit.ID))
		assert.Equal(t, 1, env.CountCancellations(t, unit.ID))
		assert.Equal(t, 1, env.CountAdjustments(t, unit.ID))
		after := env.CheckForUnit(t, unit.ID)
		assert.EqualValues(t, 0, after.ChargeVND)
		assert.Equal(t, sales.CheckStateSettled, after.State)
	})

	t.Run("cancel versus closure on a paid check", func(t *testing.T) {
		env := newCancelEnv(t)
		unit := env.SubmittedTakeawayUnits(t, 1)[0]
		sessionID := env.SessionIDForUnit(t, unit.ID)
		check := env.CheckForUnit(t, unit.ID)
		require.Equal(t, sales.CheckStateSettled, check.State)

		requestID := uuid.New()
		cancelCmd := preparation.CancelUnitsCommand{
			RequestID:          requestID,
			PreparationUnitIDs: []uuid.UUID{unit.ID},
			Kind:               preparation.CancelKindCancellation,
			Reason:             preparation.ReasonCustomerRequest,
		}
		closeCmd := sales.CloseServiceSessionCommand{
			RequestID:        uuid.New(),
			ServiceSessionID: sessionID,
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		var cancelErr, closeErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, _, cancelErr = preparation.NewCancelUnitsHandler(env.PreparationRunner).
				Handle(context.Background(), env.CashierActor(), cancelCmd)
		}()
		go func() {
			defer wg.Done()
			<-start
			_, _, closeErr = sales.NewCloseServiceSessionHandler(env.SalesRunner).
				Handle(context.Background(), env.salesActor(env.manager), closeCmd)
		}()
		close(start)
		requireRaceResolved(t, &wg)

		// A paid Check settles the money question, so closure can only fail
		// before the Cancellation (nonterminal unit) or after it (pending
		// refund); it can never commit. Cancellation always commits.
		require.NoError(t, cancelErr)
		require.False(t, raceDeadlock(closeErr))
		require.Error(t, closeErr, "a paid cancellation creates the pending Refund closure refuses")
		require.True(t,
			errors.Is(closeErr, sales.ErrUnfulfilledPreparationForClosure) ||
				errors.Is(closeErr, sales.ErrPendingRefundForClosure),
			"accepted closure outcomes are the nonterminal-unit and pending-refund rejections, got %v", closeErr)
		assert.Equal(t, sales.StateActive, env.Projection(t, sessionID).State)

		projected := env.ProjectedCheck(t, sessionID, check.ID)
		assert.EqualValues(t, 25000, projected.PendingRefundVND)
		assert.EqualValues(t, 0, projected.ChargeVND)
		assert.Equal(t, sales.CheckStateSettled, projected.State)
	})
}

// commitPendingRound leaves one more COMMITTED draft in the Session, awaiting
// Submit, so a Cancel-versus-Submit race has real Submit work to do. It drives
// the exported internal/sales handlers, the only cross-boundary way a draft
// reaches COMMITTED.
func (e *cancelEnv) commitPendingRound(t *testing.T, sessionID uuid.UUID, quantity int32) {
	t.Helper()
	ctx := context.Background()
	actor := e.salesActor(e.manager)

	_, _, err := sales.NewStartNewOrderDraftHandler(e.SalesRunner).
		Handle(ctx, actor, sales.StartNewOrderDraftCommand{
			RequestID:        uuid.New(),
			ServiceSessionID: sessionID,
		})
	require.NoError(t, err)

	_, draft, err := sales.NewAddDraftItemHandler(e.SalesRunner).
		Handle(ctx, actor, sales.AddDraftItemCommand{
			RequestID:        uuid.New(),
			ServiceSessionID: sessionID,
			MenuItemID:       e.CoffeeID,
		})
	require.NoError(t, err)
	require.NotEmpty(t, draft.Draft.Items)

	_, _, err = sales.NewSetDraftItemQuantityHandler(e.SalesRunner).
		Handle(ctx, actor, sales.SetDraftItemQuantityCommand{
			RequestID:        uuid.New(),
			ServiceSessionID: sessionID,
			DraftItemID:      draft.Draft.Items[0].ID,
			Quantity:         &quantity,
		})
	require.NoError(t, err)

	_, _, err = sales.NewCommitOrderDraftHandler(e.SalesRunner).
		Handle(ctx, actor, sales.CommitOrderDraftCommand{
			RequestID:        uuid.New(),
			ServiceSessionID: sessionID,
		})
	require.NoError(t, err)
}

// unitByNumber selects one Preparation Unit by its per-Order-Item number; the
// submitted projection's order is not guaranteed, so suites must not index it.
func unitByNumber(t *testing.T, units []sales.PreparationUnitResponse, number int32) sales.PreparationUnitResponse {
	t.Helper()
	for _, unit := range units {
		if unit.UnitNumber == number {
			return unit
		}
	}
	t.Fatalf("no preparation unit numbered %d in the submission", number)
	return sales.PreparationUnitResponse{}
}

// countOrders counts the submitted Orders of one Service Session.
func (e *cancelEnv) countOrders(t *testing.T, sessionID uuid.UUID) int {
	t.Helper()
	var n int
	require.NoError(t, e.DB.QueryRow(
		`SELECT count(*) FROM orders WHERE service_session_id = $1`, sessionID).Scan(&n))
	return n
}

// assertCheckChargeEquation proves every Check of a Session satisfies the live
// financial invariant after a race: stored charge = base allocations less live
// adjustments, with no negative balance or pending obligation, and the Check
// charges sum to wantTotal.
func assertCheckChargeEquation(t *testing.T, env *cancelEnv, sessionID uuid.UUID, wantTotal int64) {
	t.Helper()
	projection := env.Projection(t, sessionID)
	var total int64
	for _, check := range projection.Checks {
		assert.GreaterOrEqual(t, check.BalanceVND, int64(0),
			"check %s balance must never go negative", check.ID)
		assert.GreaterOrEqual(t, check.PendingRefundVND, int64(0),
			"check %s pending refund must never go negative", check.ID)
		var liveAdjustments int64
		for _, adjustment := range check.ChargeAdjustments {
			if adjustment.Scope == sales.CompScopeLiveCheck {
				liveAdjustments += adjustment.AmountVND
			}
		}
		assert.EqualValues(t, check.BaseChargeVND-liveAdjustments, check.ChargeVND,
			"check %s stored charge must equal base allocations less live adjustments", check.ID)
		total += check.ChargeVND
	}
	assert.EqualValues(t, wantTotal, total, "the Check charges must sum to wantTotal")
}

// raceDeadlock reports whether err is PostgreSQL's retryable 40P01 abort, the
// only failure ADR-031 accepts for a Submit pairing.
func raceDeadlock(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "40P01"
}

// requireRaceResolved waits for a synchronized race with a hard upper bound, so
// a blocked pair fails the test rather than hanging it.
func requireRaceResolved(t *testing.T, wg *sync.WaitGroup) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the synchronized race never resolved: a transaction is still blocked")
	}
}
