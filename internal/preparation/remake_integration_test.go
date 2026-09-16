//go:build integration

package preparation_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/preparation"
	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// wastedDressedUnit submits one modifier-and-note round, advances its unit to
// IN_PREPARATION, wastes it, and returns the unit response, the Waste fact,
// and the alert.
func wastedDressedUnit(t *testing.T, env *prepEnv) (
	sales.PreparationUnitResponse, wasteFactRow, alertFactRow,
) {
	t.Helper()
	source := env.SubmittedDressedUnits(t, 1)[0]
	_, _, err := env.Advance(t, source.ID, preparation.StateInPreparation)
	require.NoError(t, err)
	_, _, err = env.Waste(t, source.ID, preparation.ReasonQualityFailure, notePtr("bị lỗi"))
	require.NoError(t, err)
	return source, env.UnitWaste(t, source.ID), env.UnitAlert(t, source.ID)
}

func TestRemakeUnit(t *testing.T) {
	t.Run("creates one linked queued replacement from a wasted source", func(t *testing.T) {
		env := newPrepEnv(t)
		source, waste, alert := wastedDressedUnit(t, env)

		requestID := uuid.New()
		resp, status, err := env.RemakeWithRequestID(t, requestID, waste.ID,
			preparation.ReasonPreparationError, notePtr("  làm lại ly mới  "))
		require.NoError(t, err)
		require.Equal(t, http.StatusCreated, status, "a first remake answers 201")

		// Exact response fields: the Remake fact's identity and meaning plus
		// the replacement unit.
		require.NotEqual(t, uuid.Nil, resp.ID)
		require.Equal(t, waste.ID, resp.WasteID)
		require.Equal(t, source.ID, resp.SourcePreparationUnitID)
		require.Equal(t, preparation.ReasonPreparationError, resp.Reason)
		require.NotNil(t, resp.Note)
		require.Equal(t, "làm lại ly mới", *resp.Note, "the note is stored trimmed")
		require.NotZero(t, resp.CreatedAt)

		// The replacement is one new QUEUED unit of the same Order Item with
		// the next unit number.
		unit := resp.Unit
		require.NotEqual(t, uuid.Nil, unit.ID)
		require.NotEqual(t, source.ID, unit.ID, "the replacement is a fresh unit")
		require.Equal(t, source.OrderItemID, unit.OrderItemID)
		require.Equal(t, int32(2), unit.UnitNumber, "the next number of the same item")
		require.Equal(t, preparation.StateQueued, unit.State)
		require.Equal(t, preparation.StateQueued, env.UnitState(t, unit.ID))
		require.Nil(t, unit.InPreparationAt, "a queued replacement never entered preparation")

		// The immutable preparation snapshot is copied exactly.
		require.Equal(t, source.ServiceNumber, unit.ServiceNumber)
		require.Equal(t, source.CategoryName, unit.CategoryName)
		require.Equal(t, source.ItemName, unit.ItemName)
		require.Equal(t, source.SizeName, unit.SizeName)
		require.Equal(t, source.PreparationNote, unit.PreparationNote)
		require.Len(t, unit.Modifiers, len(source.Modifiers))
		for i := range source.Modifiers {
			require.Equal(t, source.Modifiers[i].GroupName, unit.Modifiers[i].GroupName)
			require.Equal(t, source.Modifiers[i].OptionName, unit.Modifiers[i].OptionName)
		}

		// REMAKE priority, the source link, and a fresh queue time.
		require.Equal(t, preparation.PriorityRemake, unit.Priority)
		require.NotNil(t, unit.RemakeOfPreparationUnitID)
		require.Equal(t, source.ID, *unit.RemakeOfPreparationUnitID)
		require.True(t, unit.QueuedAt.After(source.QueuedAt),
			"the replacement queues at a fresh instant, not the source's")

		// The stored fact carries the actor's identity and shares one instant
		// with the replacement's queue time.
		require.Equal(t, 1, env.CountRemakes(t, waste.ID))
		fact := env.WasteRemake(t, waste.ID)
		require.Equal(t, resp.ID, fact.ID)
		require.Equal(t, waste.ID, fact.WasteID)
		require.Equal(t, unit.ID, fact.ReplacementUnitID)
		require.Equal(t, preparation.ReasonPreparationError, fact.Reason)
		require.NotNil(t, fact.Note)
		require.Equal(t, "làm lại ly mới", *fact.Note)
		require.Equal(t, env.BaristaActor().StaffID, fact.ActorID)
		require.Equal(t, env.BaristaActor().SessionID, fact.SessionID)
		require.True(t, fact.CreatedAt.Equal(unit.QueuedAt),
			"the fact and the queue time share one instant")

		// One REMAKE audit naming the replacement, at that same instant.
		require.Equal(t, 1, env.CountAuditEventsByTypeAndUnit(
			t, preparation.EventPreparationRemakeCreated, unit.ID))
		audits := env.UnitAuditEvents(t, unit.ID, preparation.EventPreparationRemakeCreated)
		require.Len(t, audits, 1)
		require.True(t, audits[0].OccurredAt.Equal(unit.QueuedAt))

		// One stored result: the exact response body, replayable.
		claim, ok := env.IdempotencyClaim(t, env.BaristaActor(), requestID)
		require.True(t, ok, "a success stores its replayable result")
		require.Equal(t, preparation.OpRemakeUnit, claim.Action)
		require.Equal(t, int32(http.StatusCreated), claim.ResponseCode)
		respJSON, err := json.Marshal(resp)
		require.NoError(t, err)
		require.JSONEq(t, string(respJSON), string(claim.ResponseBody))

		// The item now has exactly the original and the replacement.
		require.Equal(t, []int32{1, 2}, env.OrderItemUnitNumbers(t, source.ID))

		// The source's Waste alert is untouched by the remake.
		require.Nil(t, env.AlertAcknowledgment(t, alert.ID).AcknowledgedAt)
	})

	t.Run("changes no financial meaning and keeps closure out", func(t *testing.T) {
		env := newPrepEnv(t)
		source := env.SubmittedTakeawayUnits(t, 1)[0]
		_, _, err := env.Advance(t, source.ID, preparation.StateInPreparation)
		require.NoError(t, err)
		_, _, err = env.Waste(t, source.ID, preparation.ReasonQualityFailure, nil)
		require.NoError(t, err)
		waste := env.UnitWaste(t, source.ID)
		sessionID := env.SessionIDForUnit(t, source.ID)

		// Baseline after the Waste: the takeaway Check is settled and the
		// Wasted unit is terminal, so the session was closure-ready.
		before := env.SessionFinancials(t, sessionID)
		itemsBefore := env.OrderItemCount(t, sessionID)
		require.True(t, before.ClosureEligible,
			"a wasted unit is terminal: the session was closure-ready before the remake")

		resp, status, err := env.Remake(t, waste.ID, preparation.ReasonPreparationError, nil)
		require.NoError(t, err)
		require.Equal(t, http.StatusCreated, status)

		after := env.SessionFinancials(t, sessionID)
		require.Equal(t, before.State, after.State)
		require.Equal(t, before.Checks, after.Checks,
			"a remake changes no Check charge, applied amount, Payment, allocation, Refund, or Comp")
		require.Empty(t, after.UnsettledCheckIDs, "money stays settled")
		require.Equal(t, itemsBefore, env.OrderItemCount(t, sessionID),
			"a remake adds no Order Item")

		// The queued replacement is real nonterminal work: closure readiness
		// now refuses the session, exactly as for an ordinary queued unit.
		require.False(t, after.ClosureEligible)
		require.Len(t, after.NonterminalUnitIDs, 1)
		require.Equal(t, resp.Unit.ID, after.NonterminalUnitIDs[0])

		// The source's Waste alert stays active until separately acknowledged.
		require.Nil(t, env.UnitAlert(t, source.ID).AcknowledgedAt)
	})

	t.Run("an unknown waste is not found before any claim", func(t *testing.T) {
		env := newPrepEnv(t)

		requestID := uuid.New()
		_, status, err := env.RemakeWithRequestID(t, requestID, uuid.New(),
			preparation.ReasonPreparationError, nil)
		require.ErrorIs(t, err, preparation.ErrWasteNotFound)
		require.Equal(t, http.StatusNotFound, status)

		_, ok := env.IdempotencyClaim(t, env.BaristaActor(), requestID)
		require.False(t, ok, "an unknown waste never claims its request id")
	})

	t.Run("a reason outside the Remake catalog is refused before any claim", func(t *testing.T) {
		env := newPrepEnv(t)
		_, waste, _ := wastedDressedUnit(t, env)

		requestID := uuid.New()
		// CUSTOMER_REQUEST is a Waste reason but not a Remake reason.
		_, status, err := env.RemakeWithRequestID(t, requestID, waste.ID,
			preparation.ReasonCustomerRequest, nil)
		require.ErrorIs(t, err, preparation.ErrInvalidReason)
		require.Equal(t, http.StatusBadRequest, status)

		_, ok := env.IdempotencyClaim(t, env.BaristaActor(), requestID)
		require.False(t, ok, "a boundary validation failure never claims its request id")
		require.Equal(t, 0, env.CountRemakes(t, waste.ID))
	})

	t.Run("OTHER without a note is refused and a blank note cannot satisfy it", func(t *testing.T) {
		env := newPrepEnv(t)
		_, waste, _ := wastedDressedUnit(t, env)

		_, _, err := env.Remake(t, waste.ID, preparation.ReasonOther, nil)
		require.ErrorIs(t, err, preparation.ErrInvalidNote)

		// The blank note normalizes to nil before validation, so it cannot
		// satisfy the OTHER reason's note rule.
		_, _, err = env.Remake(t, waste.ID, preparation.ReasonOther, notePtr("   "))
		require.ErrorIs(t, err, preparation.ErrInvalidNote)

		_, _, err = env.Remake(t, waste.ID, preparation.ReasonPreparationError,
			notePtr(strings.Repeat("ể", preparation.MaxNoteRunes+1)))
		require.ErrorIs(t, err, preparation.ErrInvalidNote)

		require.Equal(t, 0, env.CountRemakes(t, waste.ID))
	})

	t.Run("a cashier cannot remake", func(t *testing.T) {
		env := newPrepEnv(t)
		source, waste, _ := wastedDressedUnit(t, env)

		_, status, err := env.RemakeAs(t, env.CashierActor(), waste.ID,
			preparation.ReasonPreparationError, nil)
		require.ErrorIs(t, err, preparation.ErrForbidden)
		require.Equal(t, http.StatusForbidden, status)

		require.Equal(t, 0, env.CountRemakes(t, waste.ID))
		require.Equal(t, []int32{1}, env.OrderItemUnitNumbers(t, source.ID),
			"the denied remake creates no unit")
	})

	t.Run("a replayed request id returns the stored result without new writes", func(t *testing.T) {
		env := newPrepEnv(t)
		source, waste, _ := wastedDressedUnit(t, env)

		requestID := uuid.New()
		first, status, err := env.RemakeWithRequestID(t, requestID, waste.ID,
			preparation.ReasonPreparationError, notePtr("  làm lại  "))
		require.NoError(t, err)
		require.Equal(t, http.StatusCreated, status)

		remakes := env.CountRemakes(t, waste.ID)
		units := env.OrderItemUnitNumbers(t, source.ID)
		audits := env.CountAuditEvents(t, preparation.EventPreparationRemakeCreated)

		second, status, err := env.RemakeWithRequestID(t, requestID, waste.ID,
			preparation.ReasonPreparationError, notePtr("làm lại"))
		require.NoError(t, err)
		require.Equal(t, http.StatusCreated, status, "the replay returns the stored 201")

		firstJSON, err := json.Marshal(first)
		require.NoError(t, err)
		secondJSON, err := json.Marshal(second)
		require.NoError(t, err)
		require.JSONEq(t, string(firstJSON), string(secondJSON),
			"the replay returns the exact stored response")

		require.Equal(t, remakes, env.CountRemakes(t, waste.ID), "a replay records no duplicate fact")
		require.Equal(t, units, env.OrderItemUnitNumbers(t, source.ID), "a replay records no duplicate unit")
		require.Equal(t, audits, env.CountAuditEvents(t, preparation.EventPreparationRemakeCreated),
			"a replay records no duplicate audit")
	})

	t.Run("a conflicting request-id reuse is refused", func(t *testing.T) {
		env := newPrepEnv(t)
		source, waste, _ := wastedDressedUnit(t, env)

		requestID := uuid.New()
		_, _, err := env.RemakeWithRequestID(t, requestID, waste.ID,
			preparation.ReasonPreparationError, nil)
		require.NoError(t, err)

		_, status, err := env.RemakeWithRequestID(t, requestID, waste.ID,
			preparation.ReasonQualityFailure, nil)
		require.ErrorIs(t, err, preparation.ErrRequestConflict)
		require.Equal(t, http.StatusConflict, status)

		require.Equal(t, 1, env.CountRemakes(t, waste.ID))
		require.Equal(t, []int32{1, 2}, env.OrderItemUnitNumbers(t, source.ID))
	})

	t.Run("an already-remade waste is a conflict", func(t *testing.T) {
		env := newPrepEnv(t)
		source, waste, _ := wastedDressedUnit(t, env)

		_, status, err := env.Remake(t, waste.ID, preparation.ReasonPreparationError, nil)
		require.NoError(t, err)
		require.Equal(t, http.StatusCreated, status)

		requestID := uuid.New()
		_, status, err = env.RemakeWithRequestID(t, requestID, waste.ID,
			preparation.ReasonQualityFailure, nil)
		require.ErrorIs(t, err, preparation.ErrWasteAlreadyRemade,
			"a separately requested second remake of one waste is a conflict")
		require.Equal(t, http.StatusConflict, status)

		require.Equal(t, 1, env.CountRemakes(t, waste.ID))
		require.Equal(t, []int32{1, 2}, env.OrderItemUnitNumbers(t, source.ID),
			"the rejected remake leaves no replacement behind")
		_, ok := env.IdempotencyClaim(t, env.BaristaActor(), requestID)
		require.False(t, ok, "the rejected remake leaves no claim behind")
	})

	t.Run("a remake into a closed session is a conflict", func(t *testing.T) {
		env := newPrepEnv(t)
		source := env.SubmittedTakeawayUnits(t, 1)[0]
		_, _, err := env.Advance(t, source.ID, preparation.StateInPreparation)
		require.NoError(t, err)
		_, _, err = env.Waste(t, source.ID, preparation.ReasonQualityFailure, nil)
		require.NoError(t, err)
		waste := env.UnitWaste(t, source.ID)
		sessionID := env.SessionIDForUnit(t, source.ID)

		env.SettleAndCloseSession(t, sessionID)

		requestID := uuid.New()
		_, status, err := env.RemakeWithRequestID(t, requestID, waste.ID,
			preparation.ReasonPreparationError, nil)
		require.ErrorIs(t, err, preparation.ErrServiceSessionClosed,
			"a closed session is a conflict even on a first execution")
		require.Equal(t, http.StatusConflict, status)

		require.Equal(t, []int32{1}, env.OrderItemUnitNumbers(t, source.ID),
			"the rejected remake creates no unit")
		require.Equal(t, 0, env.CountRemakes(t, waste.ID))
		_, ok := env.IdempotencyClaim(t, env.BaristaActor(), requestID)
		require.False(t, ok, "the rejected remake leaves no claim behind")
	})

	t.Run("a replay succeeds after its session closes", func(t *testing.T) {
		env := newPrepEnv(t)
		source := env.SubmittedTakeawayUnits(t, 1)[0]
		_, _, err := env.Advance(t, source.ID, preparation.StateInPreparation)
		require.NoError(t, err)
		_, _, err = env.Waste(t, source.ID, preparation.ReasonQualityFailure, nil)
		require.NoError(t, err)
		waste := env.UnitWaste(t, source.ID)
		sessionID := env.SessionIDForUnit(t, source.ID)

		requestID := uuid.New()
		first, status, err := env.RemakeWithRequestID(t, requestID, waste.ID,
			preparation.ReasonPreparationError, nil)
		require.NoError(t, err)
		require.Equal(t, http.StatusCreated, status)

		// The queued replacement blocks closure like any nonterminal unit,
		// so it is fulfilled before the session can close.
		for _, target := range []string{
			preparation.StateInPreparation, preparation.StateReady, preparation.StateFulfilled,
		} {
			_, _, err = env.Advance(t, first.Unit.ID, target)
			require.NoError(t, err)
		}
		env.SettleAndCloseSession(t, sessionID)

		second, status, err := env.RemakeWithRequestID(t, requestID, waste.ID,
			preparation.ReasonPreparationError, nil)
		require.NoError(t, err, "the exact replay returns its stored result regardless of session state")
		require.Equal(t, http.StatusCreated, status)
		firstJSON, err := json.Marshal(first)
		require.NoError(t, err)
		secondJSON, err := json.Marshal(second)
		require.NoError(t, err)
		require.JSONEq(t, string(firstJSON), string(secondJSON))
	})

	t.Run("a wasted remake can produce the next chain link", func(t *testing.T) {
		env := newPrepEnv(t)
		unit1 := env.SubmittedUnits(t, 1)[0]
		_, _, err := env.Advance(t, unit1.ID, preparation.StateInPreparation)
		require.NoError(t, err)
		_, _, err = env.Waste(t, unit1.ID, preparation.ReasonQualityFailure, nil)
		require.NoError(t, err)
		waste1 := env.UnitWaste(t, unit1.ID)

		first, status, err := env.Remake(t, waste1.ID, preparation.ReasonPreparationError, nil)
		require.NoError(t, err)
		require.Equal(t, http.StatusCreated, status)
		require.Equal(t, int32(2), first.Unit.UnitNumber)
		require.Equal(t, unit1.ID, *first.Unit.RemakeOfPreparationUnitID)

		// The replacement itself can be wasted, and its Waste can produce the
		// next link of the same chain.
		_, _, err = env.Advance(t, first.Unit.ID, preparation.StateInPreparation)
		require.NoError(t, err)
		_, _, err = env.Waste(t, first.Unit.ID, preparation.ReasonQualityFailure, nil)
		require.NoError(t, err)
		waste2 := env.UnitWaste(t, first.Unit.ID)

		second, status, err := env.Remake(t, waste2.ID, preparation.ReasonOther, notePtr("lại hỏng"))
		require.NoError(t, err)
		require.Equal(t, http.StatusCreated, status)
		require.Equal(t, int32(3), second.Unit.UnitNumber)
		require.Equal(t, first.Unit.ID, *second.Unit.RemakeOfPreparationUnitID,
			"the next link points at the wasted replacement, not the original")
		require.Equal(t, preparation.PriorityRemake, second.Unit.Priority)
		require.Equal(t, []int32{1, 2, 3}, env.OrderItemUnitNumbers(t, unit1.ID))
		require.Equal(t, 1, env.CountRemakes(t, waste1.ID))
		require.Equal(t, 1, env.CountRemakes(t, waste2.ID))
	})
}

func TestRemakeRollbacksAreAtomic(t *testing.T) {
	t.Run("forced audit failure rolls back the new unit and the fact", func(t *testing.T) {
		env := newPrepEnv(t)
		source, waste, _ := wastedDressedUnit(t, env)
		unitsBefore := env.OrderItemUnitNumbers(t, source.ID)

		// The one PREPARATION_REMAKE_CREATED audit is written inside the
		// mutation through writePreparationAudits; raising there forces the
		// whole batch — and with it the mutation — to abort.
		_, err := env.DB.Exec(`
			CREATE OR REPLACE FUNCTION fail_preparation_remake_created_audit() RETURNS trigger AS $$
			BEGIN
				IF NEW.event_type = 'PREPARATION_REMAKE_CREATED' THEN
					RAISE EXCEPTION 'forced remake audit failure';
				END IF;
				RETURN NEW;
			END;
			$$ LANGUAGE plpgsql;
			CREATE TRIGGER fail_preparation_remake_created_audit
			BEFORE INSERT ON audit_events
			FOR EACH ROW EXECUTE FUNCTION fail_preparation_remake_created_audit();`)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = env.DB.Exec(`DROP TRIGGER IF EXISTS fail_preparation_remake_created_audit ON audit_events`)
			_, _ = env.DB.Exec(`DROP FUNCTION IF EXISTS fail_preparation_remake_created_audit()`)
		})

		requestID := uuid.New()
		_, _, err = env.RemakeWithRequestID(t, requestID, waste.ID,
			preparation.ReasonPreparationError, nil)
		require.Error(t, err, "the forced audit failure must abort the whole command")

		require.Equal(t, unitsBefore, env.OrderItemUnitNumbers(t, source.ID),
			"the failed transaction must not leave the replacement unit behind")
		require.Equal(t, preparation.StateWasted, env.UnitState(t, source.ID))
		require.Equal(t, 0, env.CountRemakes(t, waste.ID))
		require.Equal(t, 0, env.CountAuditEvents(t, preparation.EventPreparationRemakeCreated))

		_, ok := env.IdempotencyClaim(t, env.BaristaActor(), requestID)
		require.False(t, ok, "the failed transaction must not leave its claim behind")
	})

	t.Run("forced result-storage failure rolls back the new unit and the fact", func(t *testing.T) {
		env := newPrepEnv(t)
		source, waste, _ := wastedDressedUnit(t, env)
		unitsBefore := env.OrderItemUnitNumbers(t, source.ID)

		// The idempotency claim is written with response_code 0 at INSERT
		// time; the result store is the UPDATE that sets response_code to
		// 201. Raising there forces the executor's last write to fail with
		// the replacement and the fact already applied.
		_, err := env.DB.Exec(`
			CREATE OR REPLACE FUNCTION fail_preparation_remake_result_store() RETURNS trigger AS $$
			BEGIN
				IF NEW.response_code <> 0 AND NEW.action = 'preparation.remake_unit' THEN
					RAISE EXCEPTION 'forced remake result store failure';
				END IF;
				RETURN NEW;
			END;
			$$ LANGUAGE plpgsql;
			CREATE TRIGGER fail_preparation_remake_result_store
			BEFORE UPDATE ON idempotency_keys
			FOR EACH ROW EXECUTE FUNCTION fail_preparation_remake_result_store();`)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = env.DB.Exec(`DROP TRIGGER IF EXISTS fail_preparation_remake_result_store ON idempotency_keys`)
			_, _ = env.DB.Exec(`DROP FUNCTION IF EXISTS fail_preparation_remake_result_store()`)
		})

		requestID := uuid.New()
		_, _, err = env.RemakeWithRequestID(t, requestID, waste.ID,
			preparation.ReasonPreparationError, nil)
		require.Error(t, err, "the forced store failure must abort the whole command")

		require.Equal(t, unitsBefore, env.OrderItemUnitNumbers(t, source.ID),
			"the failed transaction must not leave the replacement unit behind")
		require.Equal(t, preparation.StateWasted, env.UnitState(t, source.ID))
		require.Equal(t, 0, env.CountRemakes(t, waste.ID))
		require.Equal(t, 0, env.CountAuditEvents(t, preparation.EventPreparationRemakeCreated))

		_, ok := env.IdempotencyClaim(t, env.BaristaActor(), requestID)
		require.False(t, ok, "the failed transaction must not leave its claim behind")
	})
}

func TestConcurrentRemake(t *testing.T) {
	t.Run("two requests for one waste produce one remake", func(t *testing.T) {
		env := newPrepEnv(t)
		source, waste, _ := wastedDressedUnit(t, env)

		var wg sync.WaitGroup
		errs := make([]error, 2)
		statuses := make([]int, 2)
		for i := range errs {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_, statuses[i], errs[i] = env.Remake(t, waste.ID, preparation.ReasonPreparationError, nil)
			}(i)
		}
		wg.Wait()

		succeeded := 0
		for i := range errs {
			if errs[i] == nil {
				succeeded++
			} else {
				require.ErrorIs(t, errs[i], preparation.ErrWasteAlreadyRemade,
					"the loser re-reads the winner's committed fact under the locks and loses on the unique waste reference")
				require.Equal(t, http.StatusConflict, statuses[i],
					"the loser maps to a typed conflict, never a 500")
			}
		}
		require.Equal(t, 1, succeeded, "the session, order-item, and waste locks serialize them")

		require.Equal(t, 1, env.CountRemakes(t, waste.ID))
		require.Equal(t, []int32{1, 2}, env.OrderItemUnitNumbers(t, source.ID),
			"exactly one replacement unit exists")
		require.Equal(t, 1, env.CountAuditEvents(t, preparation.EventPreparationRemakeCreated))
	})

	t.Run("two wastes of one order item receive distinct sequential unit numbers", func(t *testing.T) {
		env := newPrepEnv(t)
		units := env.SubmittedUnits(t, 2)
		wasteIDs := make([]uuid.UUID, 0, len(units))
		for _, unit := range units {
			_, _, err := env.Advance(t, unit.ID, preparation.StateInPreparation)
			require.NoError(t, err)
			_, _, err = env.Waste(t, unit.ID, preparation.ReasonQualityFailure, nil)
			require.NoError(t, err)
			wasteIDs = append(wasteIDs, env.UnitWaste(t, unit.ID).ID)
		}

		// Both remakes belong to one Order Item: the item lock must serialize
		// the max(unit_number)+1 allocation so both replacements commit.
		var wg sync.WaitGroup
		resps := make([]preparation.RemakeResponse, 2)
		errs := make([]error, 2)
		for i := range resps {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				resps[i], _, errs[i] = env.Remake(t, wasteIDs[i], preparation.ReasonPreparationError, nil)
			}(i)
		}
		wg.Wait()

		for i := range resps {
			require.NoError(t, errs[i], "different wastes of one item never conflict")
			require.Equal(t, preparation.PriorityRemake, resps[i].Unit.Priority)
		}

		// Distinct, sequential numbers directly after the two originals. The
		// item lock serializes the allocation, but either remake may win it,
		// so only the pair is pinned, not which request drew which number.
		require.Equal(t, []int32{1, 2, 3, 4}, env.OrderItemUnitNumbers(t, units[0].ID))
		require.ElementsMatch(t, []int32{3, 4},
			[]int32{resps[0].Unit.UnitNumber, resps[1].Unit.UnitNumber})
		require.NotEqual(t, resps[0].Unit.ID, resps[1].Unit.ID)
		require.Equal(t, 1, env.CountRemakes(t, wasteIDs[0]))
		require.Equal(t, 1, env.CountRemakes(t, wasteIDs[1]))
	})
}

// lockWaiters counts the sessions of this database currently blocked on a
// lock. The suites run sequentially, so any waiter is one this test parked.
func lockWaiters(t *testing.T, env *prepEnv) int {
	t.Helper()
	var waiters int
	require.NoError(t, env.DB.QueryRow(`
		SELECT count(*) FROM pg_stat_activity
		WHERE datname = current_database()
		  AND wait_event_type = 'Lock'
		  AND pid <> pg_backend_pid()`).Scan(&waiters))
	return waiters
}

// TestRemakeAgainstClosure pins the Remake-versus-closure serialization: both
// commands lock the Service Session first, so exactly one of two outcomes is
// possible and a queued Remake can never be missing from a closed sale.
func TestRemakeAgainstClosure(t *testing.T) {
	t.Run("a remake that commits first makes closure reject its nonterminal work", func(t *testing.T) {
		env := newPrepEnv(t)
		source := env.SubmittedTakeawayUnits(t, 1)[0]
		_, _, err := env.Advance(t, source.ID, preparation.StateInPreparation)
		require.NoError(t, err)
		_, _, err = env.Waste(t, source.ID, preparation.ReasonQualityFailure, nil)
		require.NoError(t, err)
		wasteID := env.UnitWaste(t, source.ID).ID
		sessionID := env.SessionIDForUnit(t, source.ID)

		// Lock barrier on the Order Item row: the remake takes the Session
		// lock — its first — and then parks on the Order Item, still holding
		// the Session.
		barrier, err := env.DB.BeginTx(context.Background(), nil)
		require.NoError(t, err)
		_, err = barrier.ExecContext(context.Background(),
			`SELECT id FROM order_items WHERE id = $1 FOR UPDATE`, source.OrderItemID)
		require.NoError(t, err)

		remakeErrs := make(chan error, 1)
		go func() {
			_, _, err := env.Remake(t, wasteID, preparation.ReasonPreparationError, nil)
			remakeErrs <- err
		}()

		require.Eventually(t, func() bool {
			return lockWaiters(t, env) > 0
		}, 10*time.Second, 10*time.Millisecond,
			"the remake must park on the barrier's order-item lock before it is released")

		// Closure starts only once the remake holds the Session lock, so it
		// must park there until the remake's transaction ends.
		closureErrs := make(chan error, 1)
		go func() {
			_, _, err := sales.NewCloseServiceSessionHandler(env.SalesRunner).Handle(
				context.Background(), env.salesActor(env.ManagerActor()),
				sales.CloseServiceSessionCommand{
					RequestID:        uuid.New(),
					ServiceSessionID: sessionID,
				})
			closureErrs <- err
		}()

		require.NoError(t, barrier.Rollback(), "releasing the barrier must succeed")

		require.NoError(t, <-remakeErrs, "the remake parked behind the barrier must commit")
		require.ErrorIs(t, <-closureErrs, sales.ErrUnfulfilledPreparationForClosure,
			"closure must refuse the committed replacement's nonterminal work")

		// The winner-only outcome: the replacement exists and is queued, and
		// the session is still open.
		require.Equal(t, []int32{1, 2}, env.OrderItemUnitNumbers(t, source.ID))
		require.Equal(t, 1, env.CountRemakes(t, wasteID))
		require.Equal(t, sales.StateActive, env.SessionFinancials(t, sessionID).State)
	})

	t.Run("a closure that commits first makes the remake reject the closed session", func(t *testing.T) {
		env := newPrepEnv(t)
		source := env.SubmittedTakeawayUnits(t, 1)[0]
		_, _, err := env.Advance(t, source.ID, preparation.StateInPreparation)
		require.NoError(t, err)
		_, _, err = env.Waste(t, source.ID, preparation.ReasonQualityFailure, nil)
		require.NoError(t, err)
		wasteID := env.UnitWaste(t, source.ID).ID
		sessionID := env.SessionIDForUnit(t, source.ID)

		// Lock barrier on the remake's own executor advisory lock (the same
		// actor^request-id key the executor takes): the remake parks before
		// its idempotency claim and takes no business lock, so closure can
		// run to completion.
		requestID := uuid.New()
		key := preparation.IDToLockKey(env.BaristaActor().StaffID) ^
			preparation.IDToLockKey(requestID)
		barrier, err := env.DB.BeginTx(context.Background(), nil)
		require.NoError(t, err)
		_, err = barrier.ExecContext(context.Background(),
			`SELECT pg_advisory_xact_lock($1)`, key)
		require.NoError(t, err)

		remakeErrs := make(chan error, 1)
		go func() {
			_, _, err := env.RemakeWithRequestID(t, requestID, wasteID,
				preparation.ReasonPreparationError, nil)
			remakeErrs <- err
		}()

		require.Eventually(t, func() bool {
			return lockWaiters(t, env) > 0
		}, 10*time.Second, 10*time.Millisecond,
			"the remake must park on its advisory lock before closure runs")

		// Closure completes while the remake is parked; the remake's Session
		// lock has not been taken, so nothing blocks it.
		env.SettleAndCloseSession(t, sessionID)
		require.Equal(t, sales.StateClosed, env.SessionFinancials(t, sessionID).State)

		require.NoError(t, barrier.Rollback(), "releasing the barrier must succeed")

		remakeErr := <-remakeErrs
		require.ErrorIs(t, remakeErr, preparation.ErrServiceSessionClosed,
			"the released remake must re-read the session state under the lock and refuse")
		status, _ := preparation.ErrorResponse(remakeErr)
		require.Equal(t, http.StatusConflict, status)

		// The winner-only outcome: no replacement, no fact, no claim.
		require.Equal(t, []int32{1}, env.OrderItemUnitNumbers(t, source.ID))
		require.Equal(t, 0, env.CountRemakes(t, wasteID))
		_, ok := env.IdempotencyClaim(t, env.BaristaActor(), requestID)
		require.False(t, ok, "the rejected remake leaves no claim behind")
	})
}
