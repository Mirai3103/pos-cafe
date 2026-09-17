//go:build integration

package preparation_test

import (
	"bytes"
	"context"
	"sort"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/preparation"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestActiveQueueReturnsEmptySliceAndDatabaseTime(t *testing.T) {
	env := newPrepEnv(t)
	// observed_at is read from PostgreSQL, so its bounds must come from the
	// same clock. Bounding it with the host time.Now() fails whenever the
	// Docker VM clock lags the host under suite load; SELECT now() through
	// env.DB makes the comparison deterministic.
	var before, after time.Time
	require.NoError(t, env.DB.QueryRow(`SELECT now()`).Scan(&before))
	got, err := preparation.NewActiveQueueHandler(env.PreparationRunner).
		Handle(context.Background(), env.BaristaActor())
	require.NoError(t, err)
	require.NoError(t, env.DB.QueryRow(`SELECT now()`).Scan(&after))
	require.NotNil(t, got.Units)
	require.Empty(t, got.Units)
	require.NotNil(t, got.Alerts, "alerts must never be nil so the wire carries []")
	require.Empty(t, got.Alerts)
	require.NotNil(t, got.Corrections, "corrections must never be nil so the wire carries []")
	require.Empty(t, got.Corrections)
	require.False(t, got.ObservedAt.Before(before))
	require.False(t, got.ObservedAt.After(after))
}

func TestPreparationObservedAtUsesCurrentDatabaseTime(t *testing.T) {
	db, q := openPrepTestDB(t)
	tx, err := db.Begin()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, tx.Rollback()) })

	_, err = db.Exec(`SELECT pg_sleep(0.05)`)
	require.NoError(t, err)
	var afterDelay time.Time
	require.NoError(t, db.QueryRow(`SELECT clock_timestamp()`).Scan(&afterDelay))

	observedAt, err := q.WithTx(tx).GetPreparationCurrentTime(t.Context())
	require.NoError(t, err)
	require.False(t, observedAt.Before(afterDelay))
}

func TestActiveQueueObservedAtDoesNotPrecedeVisibleUnitTimestamps(t *testing.T) {
	env := newPrepEnv(t)
	unit := env.SubmittedUnits(t, 1)[0]

	var queuedAt time.Time
	require.NoError(t, env.DB.QueryRow(`
		UPDATE preparation_units
		SET queued_at = clock_timestamp() + interval '1 hour'
		WHERE id = $1
		RETURNING queued_at`, unit.ID).Scan(&queuedAt))

	got, err := preparation.NewActiveQueueHandler(env.PreparationRunner).
		Handle(t.Context(), env.BaristaActor())
	require.NoError(t, err)
	require.Len(t, got.Units, 1)
	require.False(t, got.ObservedAt.Before(queuedAt))
}

func TestActiveQueueOrdersFIFOAndExcludesTerminalUnits(t *testing.T) {
	env := newPrepEnv(t)
	units := env.SubmittedUnits(t, 3)
	_, _, err := env.Advance(t, units[0].ID, preparation.StateInPreparation)
	require.NoError(t, err)
	_, _, err = env.Advance(t, units[0].ID, preparation.StateReady)
	require.NoError(t, err)
	_, _, err = env.Advance(t, units[0].ID, preparation.StateFulfilled)
	require.NoError(t, err)

	got, err := preparation.NewActiveQueueHandler(env.PreparationRunner).
		Handle(context.Background(), env.BaristaActor())
	require.NoError(t, err)
	require.Len(t, got.Units, 2)
	require.Equal(t, units[1].ID, got.Units[0].ID)
	require.Equal(t, units[2].ID, got.Units[1].ID)
	require.Equal(t, int32(3), got.Units[0].OrderItemUnitCount)
}

func TestActiveQueueProjectsCurrentTables(t *testing.T) {
	env := newPrepEnv(t)
	unit := env.SubmittedUnits(t, 1)[0]
	sessionID := env.SessionIDForUnit(t, unit.ID)
	secondTable := env.SeedTable(t, "Bàn 2")

	// Before any reassignment the Session's current unreleased assignment is
	// the table it opened against.
	before, err := preparation.NewActiveQueueHandler(env.PreparationRunner).
		Handle(context.Background(), env.BaristaActor())
	require.NoError(t, err)
	require.Len(t, before.Units, 1)
	require.Equal(t, []string{"Bàn 1"}, before.Units[0].TableNames)

	// SetTables writes only table_assignments; the Preparation Unit snapshot
	// row is untouched, so the after-read proves the projection is computed
	// at read time from current assignments, not frozen on the unit.
	env.SetTables(t, sessionID, secondTable)

	after, err := preparation.NewActiveQueueHandler(env.PreparationRunner).
		Handle(context.Background(), env.BaristaActor())
	require.NoError(t, err)
	require.Len(t, after.Units, 1)
	require.Equal(t, []string{"Bàn 2"}, after.Units[0].TableNames)
}

func TestActiveQueueDeniesCashier(t *testing.T) {
	env := newPrepEnv(t)
	_, err := preparation.NewActiveQueueHandler(env.PreparationRunner).
		Handle(context.Background(), env.CashierActor())
	require.ErrorIs(t, err, preparation.ErrForbidden)
}

func TestActiveQueueUsesIDAsEqualTimestampTieBreak(t *testing.T) {
	env := newPrepEnv(t)
	units := env.SubmittedUnits(t, 3)

	// One timestamp for all three rows: the queued_at ordering can no longer
	// separate them, so the stable tie-break must be the unit id.
	tiedAt := time.Now()
	for _, unit := range units {
		_, err := env.DB.Exec(
			`UPDATE preparation_units SET queued_at = $1 WHERE id = $2`, tiedAt, unit.ID)
		require.NoError(t, err)
	}

	ids := []uuid.UUID{units[0].ID, units[1].ID, units[2].ID}
	sort.Slice(ids, func(i, j int) bool { return bytes.Compare(ids[i][:], ids[j][:]) < 0 })

	got, err := preparation.NewActiveQueueHandler(env.PreparationRunner).
		Handle(context.Background(), env.BaristaActor())
	require.NoError(t, err)
	require.Len(t, got.Units, 3)
	gotIDs := []uuid.UUID{got.Units[0].ID, got.Units[1].ID, got.Units[2].ID}
	require.Equal(t, ids, gotIDs)
}

func TestActiveQueueIncludesEveryActiveStateAndStartTime(t *testing.T) {
	env := newPrepEnv(t)
	units := env.SubmittedUnits(t, 3)

	started, _, err := env.Advance(t, units[1].ID, preparation.StateInPreparation)
	require.NoError(t, err)
	readyStarted, _, err := env.Advance(t, units[2].ID, preparation.StateInPreparation)
	require.NoError(t, err)
	_, _, err = env.Advance(t, units[2].ID, preparation.StateReady)
	require.NoError(t, err)

	got, err := preparation.NewActiveQueueHandler(env.PreparationRunner).
		Handle(context.Background(), env.BaristaActor())
	require.NoError(t, err)
	require.Len(t, got.Units, 3)

	byID := make(map[uuid.UUID]preparation.QueueUnitResponse, len(got.Units))
	for _, unit := range got.Units {
		byID[unit.ID] = unit
	}

	require.Equal(t, preparation.StateQueued, byID[units[0].ID].State)
	require.Nil(t, byID[units[0].ID].InPreparationAt)

	require.Equal(t, preparation.StateInPreparation, byID[units[1].ID].State)
	require.NotNil(t, byID[units[1].ID].InPreparationAt)
	require.Equal(t, *started.InPreparationAt, *byID[units[1].ID].InPreparationAt)

	require.Equal(t, preparation.StateReady, byID[units[2].ID].State)
	require.NotNil(t, byID[units[2].ID].InPreparationAt)
	// The start time is stamped once at the IN_PREPARATION transition and
	// retained unchanged through READY.
	require.Equal(t, *readyStarted.InPreparationAt, *byID[units[2].ID].InPreparationAt)
}

func TestActiveQueueTakeawayHasEmptyTables(t *testing.T) {
	env := newPrepEnv(t)
	unit := env.SubmittedTakeawayUnits(t, 1)[0]

	got, err := preparation.NewActiveQueueHandler(env.PreparationRunner).
		Handle(context.Background(), env.BaristaActor())
	require.NoError(t, err)
	require.Len(t, got.Units, 1)
	require.Equal(t, unit.ID, got.Units[0].ID)
	// An empty collection serializes as [], never null: TableNames must be
	// non-nil even with no table to name.
	require.NotNil(t, got.Units[0].TableNames)
	require.Empty(t, got.Units[0].TableNames)
}

// insertReservedKindAlert creates a Phase 6C reserved-kind (CANCELLATION or
// CHANGE) alert straight in the database. TEST-ONLY arrangement: Phase 6B has
// no writer for those kinds, so the queue's reserved-kind projection cannot be
// arranged through exported handlers; Phase 6C must arrive without changing
// what this test proves.
func insertReservedKindAlert(t *testing.T, env *prepEnv, unitID uuid.UUID,
	kind, reason string, createdAt time.Time,
) uuid.UUID {
	t.Helper()
	actor := env.BaristaActor()
	var id uuid.UUID
	require.NoError(t, env.DB.QueryRow(`
		INSERT INTO preparation_alerts (
			preparation_unit_id, kind, reason,
			created_by_staff_identity_id, created_staff_access_session_id, created_at
		) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		unitID, kind, reason, actor.StaffID, actor.SessionID, createdAt).Scan(&id))
	return id
}

// unitServiceNumber reads a unit's service number straight from the database,
// as the independent source the alert and history identity projections are
// asserted against.
func unitServiceNumber(t *testing.T, env *prepEnv, unitID uuid.UUID) string {
	t.Helper()
	var serviceNumber string
	require.NoError(t, env.DB.QueryRow(
		`SELECT service_number FROM preparation_units WHERE id = $1`, unitID).
		Scan(&serviceNumber))
	return serviceNumber
}

func TestActiveQueueOrdersRemakesBeforeStandardUnits(t *testing.T) {
	env := newPrepEnv(t)
	units := env.SubmittedUnits(t, 3)

	// The remake is queued strictly later than its standard siblings, so a
	// pure queued_at ordering would put it last. The REMAKE lane must put it
	// first, and the alert-retained wasted source must follow all active
	// work even though it was queued first of all.
	_, _, err := env.Advance(t, units[0].ID, preparation.StateInPreparation)
	require.NoError(t, err)
	_, _, err = env.Waste(t, units[0].ID, preparation.ReasonQualityFailure, nil)
	require.NoError(t, err)
	remake, _, err := env.Remake(t, env.UnitWaste(t, units[0].ID).ID,
		preparation.ReasonPreparationError, nil)
	require.NoError(t, err)

	got, err := preparation.NewActiveQueueHandler(env.PreparationRunner).
		Handle(context.Background(), env.BaristaActor())
	require.NoError(t, err)
	require.Len(t, got.Units, 4, "two active standards, the remake, and the alert-retained source")

	require.Equal(t, remake.Unit.ID, got.Units[0].ID, "the queued REMAKE precedes all standard work")
	require.Equal(t, preparation.StateQueued, got.Units[0].State)
	require.Equal(t, preparation.PriorityRemake, got.Units[0].Priority)

	require.Equal(t, units[1].ID, got.Units[1].ID, "active STANDARD units keep their FIFO lane")
	require.Equal(t, units[2].ID, got.Units[2].ID)
	require.Equal(t, preparation.StateQueued, got.Units[1].State)
	require.Equal(t, preparation.PriorityStandard, got.Units[1].Priority)

	require.Equal(t, units[0].ID, got.Units[3].ID,
		"the alert-retained WASTED source follows all active work")
	require.Equal(t, preparation.StateWasted, got.Units[3].State)
	require.Equal(t, preparation.PriorityStandard, got.Units[3].Priority)
}

func TestActiveQueueOrdersRemakeLaneFIFOByIDOnEqualTimestamps(t *testing.T) {
	env := newPrepEnv(t)
	units := env.SubmittedUnits(t, 2)

	var remakeIDs []uuid.UUID
	for _, unit := range units {
		_, _, err := env.Advance(t, unit.ID, preparation.StateInPreparation)
		require.NoError(t, err)
		_, _, err = env.Waste(t, unit.ID, preparation.ReasonQualityFailure, nil)
		require.NoError(t, err)
		remake, _, err := env.Remake(t, env.UnitWaste(t, unit.ID).ID,
			preparation.ReasonPreparationError, nil)
		require.NoError(t, err)
		remakeIDs = append(remakeIDs, remake.Unit.ID)
	}

	// One timestamp for both remakes: the lane's ordering can no longer use
	// queued_at, so the id tie-break must decide (test-only arrangement,
	// mirroring the standard lane's tie-break test).
	tiedAt := time.Now()
	for _, id := range remakeIDs {
		_, err := env.DB.Exec(
			`UPDATE preparation_units SET queued_at = $1 WHERE id = $2`, tiedAt, id)
		require.NoError(t, err)
	}
	sort.Slice(remakeIDs, func(i, j int) bool { return bytes.Compare(remakeIDs[i][:], remakeIDs[j][:]) < 0 })

	got, err := preparation.NewActiveQueueHandler(env.PreparationRunner).
		Handle(context.Background(), env.BaristaActor())
	require.NoError(t, err)
	require.Len(t, got.Units, 4, "two remakes then two alert-retained sources")
	require.Equal(t, remakeIDs, []uuid.UUID{got.Units[0].ID, got.Units[1].ID},
		"the REMAKE lane stays FIFO with the id tie-break")
	require.Equal(t, preparation.PriorityRemake, got.Units[0].Priority)
	require.Equal(t, preparation.PriorityRemake, got.Units[1].Priority)
	require.Equal(t, units[0].ID, got.Units[2].ID, "the wasted sources follow in queued_at order")
	require.Equal(t, units[1].ID, got.Units[3].ID)
}

func TestActiveQueueAlertRetainedWastedUnitFollowsActiveWorkEvenAsRemake(t *testing.T) {
	env := newPrepEnv(t)
	source := env.SubmittedUnits(t, 1)[0]
	_, _, err := env.Advance(t, source.ID, preparation.StateInPreparation)
	require.NoError(t, err)
	_, _, err = env.Waste(t, source.ID, preparation.ReasonQualityFailure, nil)
	require.NoError(t, err)
	remake, _, err := env.Remake(t, env.UnitWaste(t, source.ID).ID,
		preparation.ReasonPreparationError, nil)
	require.NoError(t, err)

	// The source's alert is acknowledged so only the remake's own waste
	// retains a unit; the next round is submitted after the remake, so its
	// queued_at sorts after the remake's.
	_, _, err = env.Acknowledge(t, env.UnitAlert(t, source.ID).ID)
	require.NoError(t, err)
	later := env.SubmittedUnits(t, 1)[0]

	// Wasting the remake leaves it WASTED with the REMAKE priority and an
	// active alert: recovery work no longer, so it leaves the priority lane.
	_, _, err = env.Advance(t, remake.Unit.ID, preparation.StateInPreparation)
	require.NoError(t, err)
	_, _, err = env.Waste(t, remake.Unit.ID, preparation.ReasonQualityFailure, nil)
	require.NoError(t, err)

	got, err := preparation.NewActiveQueueHandler(env.PreparationRunner).
		Handle(context.Background(), env.BaristaActor())
	require.NoError(t, err)
	require.Len(t, got.Units, 2)
	require.Equal(t, later.ID, got.Units[0].ID,
		"active standard work precedes the alert-retained unit even when it was queued later")
	require.Equal(t, remake.Unit.ID, got.Units[1].ID,
		"the wasted REMAKE is retained by its alert in the terminal lane")
	require.Equal(t, preparation.StateWasted, got.Units[1].State)
	require.Equal(t, preparation.PriorityRemake, got.Units[1].Priority)
	require.Len(t, got.Alerts, 1)
	require.Equal(t, env.UnitWaste(t, remake.Unit.ID).ID, *got.Alerts[0].WasteID)
}

func TestActiveQueueWastedUnitAndAlertVisibleUntilAcknowledgment(t *testing.T) {
	env := newPrepEnv(t)
	unit := env.SubmittedUnits(t, 1)[0]
	_, _, err := env.Advance(t, unit.ID, preparation.StateInPreparation)
	require.NoError(t, err)
	waste, _, err := env.Waste(t, unit.ID, preparation.ReasonQualityFailure, nil)
	require.NoError(t, err)

	before, err := preparation.NewActiveQueueHandler(env.PreparationRunner).
		Handle(context.Background(), env.BaristaActor())
	require.NoError(t, err)
	require.Len(t, before.Units, 1, "the wasted unit stays visible while its alert is active")
	require.Equal(t, unit.ID, before.Units[0].ID)
	require.Len(t, before.Alerts, 1)
	require.Equal(t, waste.Alert.ID, before.Alerts[0].ID)

	_, _, err = env.Acknowledge(t, waste.Alert.ID)
	require.NoError(t, err)

	after, err := preparation.NewActiveQueueHandler(env.PreparationRunner).
		Handle(context.Background(), env.BaristaActor())
	require.NoError(t, err)
	require.Empty(t, after.Units, "acknowledgment removes the retained unit")
	require.Empty(t, after.Alerts, "acknowledged alerts never appear")
	require.Len(t, after.Corrections, 1, "the Waste fact stays in the active-session history")
	require.Equal(t, "WASTE", after.Corrections[0].EntryKind)
	require.Equal(t, waste.ID, after.Corrections[0].ID)
}

func TestActiveQueueAlertsAreOldestFirstWithProjectedIdentity(t *testing.T) {
	env := newPrepEnv(t)

	wasteWithNote := "Đánh rơi ly"
	first := env.SubmittedUnits(t, 1)[0]
	_, _, err := env.Advance(t, first.ID, preparation.StateInPreparation)
	require.NoError(t, err)
	firstWaste, _, err := env.Waste(t, first.ID, preparation.ReasonQualityFailure, &wasteWithNote)
	require.NoError(t, err)

	second := env.SubmittedUnits(t, 1)[0]
	_, _, err = env.Advance(t, second.ID, preparation.StateInPreparation)
	require.NoError(t, err)
	secondWaste, _, err := env.Waste(t, second.ID, preparation.ReasonOther, &wasteWithNote)
	require.NoError(t, err)

	got, err := preparation.NewActiveQueueHandler(env.PreparationRunner).
		Handle(context.Background(), env.BaristaActor())
	require.NoError(t, err)
	require.Len(t, got.Alerts, 2)
	require.Equal(t, firstWaste.Alert.ID, got.Alerts[0].ID,
		"alerts are ordered oldest first")
	require.Equal(t, secondWaste.Alert.ID, got.Alerts[1].ID)

	oldest := got.Alerts[0]
	require.Equal(t, "WASTE", oldest.Kind)
	require.Equal(t, first.ID, oldest.PreparationUnitID)
	require.Equal(t, unitServiceNumber(t, env, first.ID), oldest.ServiceNumber,
		"the alert projects its own unit's identity")
	require.Equal(t, "Cà phê sữa", oldest.ItemName)
	require.Equal(t, int32(1), oldest.UnitNumber)
	require.Equal(t, preparation.ReasonQualityFailure, oldest.Reason)
	require.NotNil(t, oldest.Note)
	require.Equal(t, wasteWithNote, *oldest.Note)
	require.NotNil(t, oldest.WasteID)
	require.Equal(t, firstWaste.ID, *oldest.WasteID, "only the WASTE alert resolves the Waste fact")
	require.Equal(t, firstWaste.Alert.CreatedAt, oldest.CreatedAt)
	require.Nil(t, oldest.AcknowledgedByStaffIdentityID,
		"an active alert carries null acknowledgment evidence")
	require.Nil(t, oldest.AcknowledgedAt)

	newest := got.Alerts[1]
	require.Equal(t, unitServiceNumber(t, env, second.ID), newest.ServiceNumber,
		"each alert projects its own unit, never a shared identity")
	require.Equal(t, secondWaste.ID, *newest.WasteID)
	require.Nil(t, newest.AcknowledgedByStaffIdentityID)
	require.Nil(t, newest.AcknowledgedAt)
}

func TestActiveQueueTwoAlertsOnOneTerminalUnitProduceOneUnitRow(t *testing.T) {
	env := newPrepEnv(t)
	unit := env.SubmittedUnits(t, 1)[0]
	_, _, err := env.Advance(t, unit.ID, preparation.StateInPreparation)
	require.NoError(t, err)
	waste, _, err := env.Waste(t, unit.ID, preparation.ReasonQualityFailure, nil)
	require.NoError(t, err)

	// TEST-ONLY arrangement (see insertReservedKindAlert): the reserved
	// Cancellation kinds have no Phase 6B writer. Explicit timestamps keep
	// the created_at ordering deterministic against the Waste alert.
	cancellationID := insertReservedKindAlert(t, env, unit.ID,
		"CANCELLATION", "CUSTOMER_REQUEST", time.Now().Add(time.Minute))
	changeID := insertReservedKindAlert(t, env, unit.ID,
		"CHANGE", "ITEM_UNAVAILABLE", time.Now().Add(2*time.Minute))

	got, err := preparation.NewActiveQueueHandler(env.PreparationRunner).
		Handle(context.Background(), env.BaristaActor())
	require.NoError(t, err)
	require.Len(t, got.Units, 1,
		"the EXISTS guard keeps several alerts on one unit from duplicating its row")
	require.Equal(t, unit.ID, got.Units[0].ID)

	require.Len(t, got.Alerts, 3)
	require.Equal(t, waste.Alert.ID, got.Alerts[0].ID, "alerts stay oldest first")
	require.Equal(t, "WASTE", got.Alerts[0].Kind)
	require.NotNil(t, got.Alerts[0].WasteID)
	require.Equal(t, waste.ID, *got.Alerts[0].WasteID,
		"only the WASTE alert resolves the unit's Waste fact")

	require.Equal(t, cancellationID, got.Alerts[1].ID)
	require.Equal(t, "CANCELLATION", got.Alerts[1].Kind)
	require.Nil(t, got.Alerts[1].WasteID, "reserved kinds never resolve a waste_id")

	require.Equal(t, changeID, got.Alerts[2].ID)
	require.Equal(t, "CHANGE", got.Alerts[2].Kind)
	require.Nil(t, got.Alerts[2].WasteID)
}

func TestActiveQueueCorrectionsAreNewestFirst(t *testing.T) {
	env := newPrepEnv(t)

	first := env.SubmittedUnits(t, 1)[0]
	_, _, err := env.Advance(t, first.ID, preparation.StateInPreparation)
	require.NoError(t, err)
	firstWaste, _, err := env.Waste(t, first.ID, preparation.ReasonQualityFailure, nil)
	require.NoError(t, err)

	second := env.SubmittedUnits(t, 1)[0]
	_, _, err = env.Advance(t, second.ID, preparation.StateInPreparation)
	require.NoError(t, err)
	secondWaste, _, err := env.Waste(t, second.ID, preparation.ReasonPreparationError, nil)
	require.NoError(t, err)

	// The remake happens last, so its created_at sorts newest of all.
	remake, _, err := env.Remake(t, firstWaste.ID, preparation.ReasonPreparationError, nil)
	require.NoError(t, err)

	got, err := preparation.NewActiveQueueHandler(env.PreparationRunner).
		Handle(context.Background(), env.BaristaActor())
	require.NoError(t, err)
	require.Len(t, got.Corrections, 3)
	require.Equal(t, "REMAKE", got.Corrections[0].EntryKind)
	require.Equal(t, remake.ID, got.Corrections[0].ID)
	require.Equal(t, "WASTE", got.Corrections[1].EntryKind)
	require.Equal(t, secondWaste.ID, got.Corrections[1].ID, "history is newest first")
	require.Equal(t, "WASTE", got.Corrections[2].EntryKind)
	require.Equal(t, firstWaste.ID, got.Corrections[2].ID)
}

func TestActiveQueueCorrectionEntriesMapWasteAndRemakeFacts(t *testing.T) {
	env := newPrepEnv(t)
	units := env.SubmittedUnits(t, 2)

	wasteNote := "Sai công thức"
	_, _, err := env.Advance(t, units[0].ID, preparation.StateInPreparation)
	require.NoError(t, err)
	waste, _, err := env.Waste(t, units[0].ID, preparation.ReasonQualityFailure, &wasteNote)
	require.NoError(t, err)

	remakeNote := "Làm lại ly mới"
	remake, _, err := env.Remake(t, waste.ID, preparation.ReasonPreparationError, &remakeNote)
	require.NoError(t, err)

	got, err := preparation.NewActiveQueueHandler(env.PreparationRunner).
		Handle(context.Background(), env.BaristaActor())
	require.NoError(t, err)
	require.Len(t, got.Corrections, 2)

	entry := got.Corrections[0]
	require.Equal(t, "REMAKE", entry.EntryKind)
	require.Equal(t, remake.ID, entry.ID, "a REMAKE entry's id is the Remake fact id")
	require.Equal(t, remake.Unit.ID, entry.PreparationUnitID,
		"the REMAKE entry names the replacement unit")
	require.NotNil(t, entry.WasteID)
	require.Equal(t, waste.ID, *entry.WasteID)
	require.NotNil(t, entry.SourcePreparationUnitID)
	require.Equal(t, units[0].ID, *entry.SourcePreparationUnitID)
	require.NotNil(t, entry.SourceUnitNumber)
	require.Equal(t, units[0].UnitNumber, *entry.SourceUnitNumber)
	require.Equal(t, remake.Unit.UnitNumber, entry.UnitNumber)
	require.Equal(t, remake.Unit.ServiceNumber, entry.ServiceNumber)
	require.Equal(t, remake.Unit.ItemName, entry.ItemName)
	require.Equal(t, preparation.ReasonPreparationError, entry.Reason)
	require.NotNil(t, entry.Note)
	require.Equal(t, remakeNote, *entry.Note)
	require.Equal(t, remake.CreatedAt, entry.OccurredAt, "a REMAKE entry occurs at its creation")

	wasteEntry := got.Corrections[1]
	require.Equal(t, "WASTE", wasteEntry.EntryKind)
	require.Equal(t, waste.ID, wasteEntry.ID, "a WASTE entry's id is the Waste fact id")
	require.Equal(t, units[0].ID, wasteEntry.PreparationUnitID,
		"the WASTE entry names the wasted source unit")
	require.Nil(t, wasteEntry.WasteID)
	require.Nil(t, wasteEntry.SourcePreparationUnitID,
		"a WASTE entry carries no remake linkage")
	require.Nil(t, wasteEntry.SourceUnitNumber)
	// The unit number comes from the wasted unit itself: same-instant
	// originals tie on queued_at and fall to the id tie-break, so the
	// submitted order (and thus units[0]'s number) is random across runs.
	require.Equal(t, units[0].UnitNumber, wasteEntry.UnitNumber)
	require.Equal(t, unitServiceNumber(t, env, units[0].ID), wasteEntry.ServiceNumber)
	require.Equal(t, "Cà phê sữa", wasteEntry.ItemName)
	require.Equal(t, preparation.ReasonQualityFailure, wasteEntry.Reason)
	require.NotNil(t, wasteEntry.Note)
	require.Equal(t, wasteNote, *wasteEntry.Note)
	require.Equal(t, waste.OccurredAt, wasteEntry.OccurredAt)
}

func TestActiveQueueCorrectionsIncludeOnlyActiveSessions(t *testing.T) {
	env := newPrepEnv(t)

	closed := env.SubmittedUnits(t, 1)[0]
	_, _, err := env.Advance(t, closed.ID, preparation.StateInPreparation)
	require.NoError(t, err)
	closedWaste, _, err := env.Waste(t, closed.ID, preparation.ReasonQualityFailure, nil)
	require.NoError(t, err)
	env.SettleAndCloseSession(t, env.SessionIDForUnit(t, closed.ID))

	active := env.SubmittedUnits(t, 1)[0]
	_, _, err = env.Advance(t, active.ID, preparation.StateInPreparation)
	require.NoError(t, err)
	activeWaste, _, err := env.Waste(t, active.ID, preparation.ReasonPreparationError, nil)
	require.NoError(t, err)

	got, err := preparation.NewActiveQueueHandler(env.PreparationRunner).
		Handle(context.Background(), env.BaristaActor())
	require.NoError(t, err)
	require.Len(t, got.Corrections, 1, "a closed Session's Waste leaves the history")
	require.Equal(t, "WASTE", got.Corrections[0].EntryKind)
	require.Equal(t, activeWaste.ID, got.Corrections[0].ID)
	require.NotEqual(t, closedWaste.ID, got.Corrections[0].ID)
}

func TestActiveQueueCorrectionsCapAtFifty(t *testing.T) {
	env := newPrepEnv(t)
	units := env.SubmittedUnits(t, 55)

	// Bulk advance keeps the setup to two calls; the cap needs more facts
	// than one handler command may touch in a batch.
	allIDs := make([]uuid.UUID, 0, len(units))
	for _, unit := range units {
		allIDs = append(allIDs, unit.ID)
	}
	half := len(allIDs) / 2
	for _, group := range [][]uuid.UUID{allIDs[:half], allIDs[half:]} {
		_, resp, err := env.BulkAdvance(t, preparation.BulkAdvanceCommand{
			RequestID:          uuid.New(),
			PreparationUnitIDs: group,
			TargetState:        preparation.StateInPreparation,
		})
		require.NoError(t, err)
		for _, outcome := range resp.Outcomes {
			require.Equal(t, preparation.BulkStatusAdvanced, outcome.Status, outcome.Code)
		}
	}
	for _, unit := range units {
		_, _, err := env.Waste(t, unit.ID, preparation.ReasonQualityFailure, nil)
		require.NoError(t, err)
	}

	// The independent newest-first ordering, straight from the facts.
	rows, err := env.DB.Query(`SELECT id FROM preparation_wastes ORDER BY occurred_at DESC, id DESC`)
	require.NoError(t, err)
	allWasteIDs := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		require.NoError(t, rows.Scan(&id))
		allWasteIDs = append(allWasteIDs, id)
	}
	require.NoError(t, rows.Close())
	require.Len(t, allWasteIDs, 55)

	got, err := preparation.NewActiveQueueHandler(env.PreparationRunner).
		Handle(context.Background(), env.BaristaActor())
	require.NoError(t, err)
	require.Len(t, got.Corrections, 50, "history is capped at 50 entries")

	returned := make([]uuid.UUID, 0, len(got.Corrections))
	for _, entry := range got.Corrections {
		returned = append(returned, entry.ID)
	}
	require.Equal(t, allWasteIDs[:50], returned,
		"the cap keeps exactly the newest entries in their newest-first order")
	excluded := make(map[uuid.UUID]struct{}, 5)
	for _, id := range allWasteIDs[50:] {
		excluded[id] = struct{}{}
	}
	for _, id := range returned {
		require.NotContains(t, excluded, id, "the oldest facts must be the ones dropped")
	}
}

func TestActiveQueueReadIsOneRepeatableSnapshot(t *testing.T) {
	t.Run("a concurrent acknowledgment is invisible to the in-flight read", func(t *testing.T) {
		env := newPrepEnv(t)
		unit := env.SubmittedUnits(t, 1)[0]
		_, _, err := env.Advance(t, unit.ID, preparation.StateInPreparation)
		require.NoError(t, err)
		waste, _, err := env.Waste(t, unit.ID, preparation.ReasonQualityFailure, nil)
		require.NoError(t, err)

		// Barrier: an ACCESS EXCLUSIVE lock on tables parks the queue read
		// at its table-assignments query — after its units read but before
		// its alerts read — while touching nothing the acknowledgment
		// writes. A read-only REPEATABLE READ transaction takes no row
		// locks, so this table lock is the only way to pin it mid-read.
		barrier, err := env.DB.BeginTx(context.Background(), nil)
		require.NoError(t, err)
		_, err = barrier.ExecContext(context.Background(),
			`LOCK TABLE tables IN ACCESS EXCLUSIVE MODE`)
		require.NoError(t, err)

		type readResult struct {
			queue preparation.QueueResponse
			err   error
		}
		results := make(chan readResult, 1)
		go func() {
			queue, err := preparation.NewActiveQueueHandler(env.PreparationRunner).
				Handle(context.Background(), env.BaristaActor())
			results <- readResult{queue: queue, err: err}
		}()

		require.Eventually(t, func() bool { return lockWaiters(t, env) > 0 },
			10*time.Second, 10*time.Millisecond,
			"the queue read must park on the barrier's tables lock")

		// The acknowledgment commits entirely while the read is parked.
		_, _, err = env.Acknowledge(t, waste.Alert.ID)
		require.NoError(t, err)
		require.NotNil(t, env.AlertAcknowledgment(t, waste.Alert.ID).AcknowledgedAt,
			"the acknowledgment must be committed before the read resumes")

		require.NoError(t, barrier.Rollback(), "releasing the barrier must succeed")
		result := <-results
		require.NoError(t, result.err)

		// The read's snapshot predates the acknowledgment: the wasted unit
		// and its alert still agree, though the acknowledgment committed
		// between the read's units query and its alerts query.
		require.Len(t, result.queue.Units, 1)
		require.Equal(t, unit.ID, result.queue.Units[0].ID)
		require.Len(t, result.queue.Alerts, 1)
		require.Equal(t, waste.Alert.ID, result.queue.Alerts[0].ID)

		fresh, err := preparation.NewActiveQueueHandler(env.PreparationRunner).
			Handle(context.Background(), env.BaristaActor())
		require.NoError(t, err)
		require.Empty(t, fresh.Units, "a fresh read sees the acknowledgment")
		require.Empty(t, fresh.Alerts)
		require.Len(t, fresh.Corrections, 1, "the Waste fact outlives its alert")
	})

	t.Run("a concurrent remake is invisible to the in-flight read", func(t *testing.T) {
		env := newPrepEnv(t)
		source := env.SubmittedUnits(t, 1)[0]
		_, _, err := env.Advance(t, source.ID, preparation.StateInPreparation)
		require.NoError(t, err)
		waste, _, err := env.Waste(t, source.ID, preparation.ReasonQualityFailure, nil)
		require.NoError(t, err)

		barrier, err := env.DB.BeginTx(context.Background(), nil)
		require.NoError(t, err)
		_, err = barrier.ExecContext(context.Background(),
			`LOCK TABLE tables IN ACCESS EXCLUSIVE MODE`)
		require.NoError(t, err)

		type readResult struct {
			queue preparation.QueueResponse
			err   error
		}
		results := make(chan readResult, 1)
		go func() {
			queue, err := preparation.NewActiveQueueHandler(env.PreparationRunner).
				Handle(context.Background(), env.BaristaActor())
			results <- readResult{queue: queue, err: err}
		}()

		require.Eventually(t, func() bool { return lockWaiters(t, env) > 0 },
			10*time.Second, 10*time.Millisecond,
			"the queue read must park on the barrier's tables lock")

		// The remake commits entirely while the read is parked: it writes
		// replacement units, a Remake fact, and its own locks, none of
		// which collide with the read's held snapshot.
		remake, _, err := env.Remake(t, waste.ID, preparation.ReasonPreparationError, nil)
		require.NoError(t, err)

		require.NoError(t, barrier.Rollback(), "releasing the barrier must succeed")
		result := <-results
		require.NoError(t, result.err)

		// The read's snapshot predates the remake: no replacement unit in
		// the queue and no REMAKE entry in the history, while the WASTE
		// facts read before the park stay consistent.
		require.Len(t, result.queue.Units, 1)
		require.Equal(t, source.ID, result.queue.Units[0].ID)
		require.Len(t, result.queue.Alerts, 1)
		require.Len(t, result.queue.Corrections, 1)
		require.Equal(t, "WASTE", result.queue.Corrections[0].EntryKind)
		require.Equal(t, waste.ID, result.queue.Corrections[0].ID)

		fresh, err := preparation.NewActiveQueueHandler(env.PreparationRunner).
			Handle(context.Background(), env.BaristaActor())
		require.NoError(t, err)
		require.Len(t, fresh.Units, 2, "a fresh read sees the replacement")
		require.Equal(t, remake.Unit.ID, fresh.Units[0].ID, "the replacement takes the REMAKE lane")
		require.Equal(t, source.ID, fresh.Units[1].ID)
		require.Len(t, fresh.Corrections, 2)
		require.Equal(t, "REMAKE", fresh.Corrections[0].EntryKind)
		require.Equal(t, remake.ID, fresh.Corrections[0].ID)
		require.Equal(t, "WASTE", fresh.Corrections[1].EntryKind)
	})
}
