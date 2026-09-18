//go:build integration

package sales_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/preparation"
	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// compEnv drives the Sales Comp command over the Sales env's fixture world and
// reaches into internal/preparation through the package's exported handlers
// for the Waste/Remake/advance steps that make a charged Wasted unit exist. A
// test file may cross the ADR-024 boundary; the handlers under test may not.
type compEnv struct {
	*salesEnv
	prep *prepHandlers
}

func newCompEnv(t *testing.T) *compEnv {
	t.Helper()
	env := &compEnv{salesEnv: newSalesEnv(t)}
	env.prep = newPrepHandlers(env.salesEnv)
	return env
}

// ---------- Preparation fixture helpers ----------

// advanceToReady moves a QUEUED unit along the chain until Waste admits it.
func (e *compEnv) advanceToReady(t *testing.T, unitID uuid.UUID) {
	t.Helper()
	e.prep.advancePrepUnit(t, unitID, preparation.StateInPreparation)
	e.prep.advancePrepUnit(t, unitID, preparation.StateReady)
}

func (e *compEnv) wasteUnit(t *testing.T, unitID uuid.UUID) uuid.UUID {
	t.Helper()
	return e.prep.wastePrepUnit(t, unitID)
}

func (e *compEnv) remakeWaste(t *testing.T, wasteID uuid.UUID) preparation.RemakeResponse {
	t.Helper()
	return e.prep.remakePrepUnit(t, wasteID)
}

// liveWastedUnit submits one unpaid Dine-in unit, advances it to READY, and
// wastes it: the active-Session charged source Comp consumes.
func (e *compEnv) liveWastedUnit(t *testing.T) (sales.ServiceSessionResponse,
	sales.PreparationUnitResponse, uuid.UUID,
) {
	t.Helper()
	session := e.commitDineInDraftWithQuantity(t, 1)
	session = e.Submit(t, session.ID)
	require.Len(t, session.PreparationUnits, 1)
	unit := session.PreparationUnits[0]
	e.advanceToReady(t, unit.ID)
	return session, unit, e.wasteUnit(t, unit.ID)
}

// paidTakeawayWastedUnit pays one Takeaway unit in full before Submit, then
// wastes it: the Check is SETTLED when the Comp arrives.
func (e *compEnv) paidTakeawayWastedUnit(t *testing.T) (sales.ServiceSessionResponse,
	sales.PreparationUnitResponse, uuid.UUID, uuid.UUID,
) {
	t.Helper()
	session := e.StartTakeaway(t)
	e.AddDraftItem(t, session.ID, e.CoffeeID, nil)
	session = e.Commit(t, session.ID)
	checkID := e.soleCheckID(t, session.ID)
	_, status, err := e.payCash(t, checkID, 25000, 25000)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
	session = e.Submit(t, session.ID)
	require.Len(t, session.PreparationUnits, 1)
	unit := session.PreparationUnits[0]
	e.advanceToReady(t, unit.ID)
	return session, unit, e.wasteUnit(t, unit.ID), checkID
}

// ---------- Comp invocation helpers ----------

// loginCode resolves the seeded identity's login code, which a Manager
// Approval must name.
func (e *compEnv) loginCode(t *testing.T, actor sales.Actor) string {
	t.Helper()
	var code string
	require.NoError(t, e.DB.QueryRow(
		`SELECT login_code FROM staff_identities WHERE id = $1`, actor.StaffID).Scan(&code))
	return code
}

// approvalFor builds the inline Manager Approval for one seeded identity. The
// shared fixture hash always authenticates PIN 1234.
func (e *compEnv) approvalFor(t *testing.T, actor sales.Actor) sales.ManagerApprovalInput {
	t.Helper()
	return sales.ManagerApprovalInput{
		ApproverLoginCode: e.loginCode(t, actor),
		ManagerPIN:        "1234",
	}
}

func (e *compEnv) compCommand(t *testing.T, wasteID uuid.UUID, reason string,
	note *string,
) sales.CompWasteCommand {
	t.Helper()
	return sales.CompWasteCommand{
		RequestID:       uuid.New(),
		WasteID:         wasteID,
		Reason:          reason,
		Note:            note,
		ManagerApproval: e.approvalFor(t, e.Actor),
	}
}

// comp runs Comp and returns the mapped HTTP status and raw error.
func (e *compEnv) comp(t *testing.T, cmd sales.CompWasteCommand) (int, sales.CompResult, error) {
	t.Helper()
	status, result, err := sales.NewCompWasteHandler(e.Runner).
		Handle(context.Background(), e.Actor, cmd)
	if err != nil {
		status, err = mapErrorStatus(err)
	}
	return status, result, err
}

// compAs runs Comp as another actor, for authorization tests.
func (e *compEnv) compAs(t *testing.T, actor sales.Actor, cmd sales.CompWasteCommand) (
	int, sales.CompResult, error,
) {
	t.Helper()
	status, result, err := sales.NewCompWasteHandler(e.Runner).
		Handle(context.Background(), actor, cmd)
	if err != nil {
		status, err = mapErrorStatus(err)
	}
	return status, result, err
}

// compOK runs Comp and requires the 201 the route promises.
func (e *compEnv) compOK(t *testing.T, cmd sales.CompWasteCommand) sales.CompResult {
	t.Helper()
	status, result, err := e.comp(t, cmd)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, status)
	return result
}

// ---------- Database assertion helpers ----------

type compAdjustmentRow struct {
	ID                 uuid.UUID
	Kind               string
	Scope              string
	CheckID            uuid.UUID
	ChargeAllocationID uuid.UUID
	CompletedSaleID    uuid.NullUUID
	SalesShiftID       uuid.UUID
	AmountVND          int64
}

func (e *compEnv) compCount(t *testing.T, wasteID uuid.UUID) int {
	t.Helper()
	var n int
	require.NoError(t, e.DB.QueryRow(
		`SELECT count(*) FROM sales_comps WHERE preparation_waste_id = $1`,
		wasteID).Scan(&n))
	return n
}

func (e *compEnv) adjustmentForWaste(t *testing.T, wasteID uuid.UUID) compAdjustmentRow {
	t.Helper()
	var row compAdjustmentRow
	require.NoError(t, e.DB.QueryRow(`
		SELECT id, kind, scope, check_id, charge_allocation_id,
		       completed_sale_id, sales_shift_id, amount_vnd
		FROM charge_adjustments
		WHERE preparation_waste_id = $1`, wasteID).Scan(
		&row.ID, &row.Kind, &row.Scope, &row.CheckID, &row.ChargeAllocationID,
		&row.CompletedSaleID, &row.SalesShiftID, &row.AmountVND))
	return row
}

func (e *compEnv) adjustmentCountForWaste(t *testing.T, wasteID uuid.UUID) int {
	t.Helper()
	var n int
	require.NoError(t, e.DB.QueryRow(
		`SELECT count(*) FROM charge_adjustments WHERE preparation_waste_id = $1`,
		wasteID).Scan(&n))
	return n
}

type compCheckRow struct {
	State          string
	ChargeVND      int64
	EvidenceCount  int
	SettledBy      uuid.NullUUID
	SettledShiftID uuid.NullUUID
	SettledAt      sql.NullTime
	SettledSession uuid.NullUUID
}

func (e *compEnv) compCheck(t *testing.T, checkID uuid.UUID) compCheckRow {
	t.Helper()
	var row compCheckRow
	require.NoError(t, e.DB.QueryRow(`
		SELECT state, charge_vnd,
		       (settled_at IS NOT NULL)::int
		     + (settled_by_staff_identity_id IS NOT NULL)::int
		     + (settled_during_sales_shift_id IS NOT NULL)::int
		     + (settled_staff_access_session_id IS NOT NULL)::int,
		       settled_by_staff_identity_id, settled_during_sales_shift_id,
		       settled_at, settled_staff_access_session_id
		FROM checks WHERE id = $1`, checkID).Scan(
		&row.State, &row.ChargeVND, &row.EvidenceCount,
		&row.SettledBy, &row.SettledShiftID, &row.SettledAt, &row.SettledSession))
	return row
}

func (e *compEnv) projectedCheck(t *testing.T, sessionID, checkID uuid.UUID) sales.CheckResponse {
	t.Helper()
	return e.findCheck(t, e.GetSessionOK(t, sessionID), checkID)
}

func (e *compEnv) idempotencyClaimCount(t *testing.T, actor sales.Actor, requestID uuid.UUID) int {
	t.Helper()
	var n int
	require.NoError(t, e.DB.QueryRow(
		`SELECT count(*) FROM idempotency_keys WHERE actor_id = $1 AND key = $2`,
		actor.StaffID, requestID).Scan(&n))
	return n
}

func (e *compEnv) storedResultBody(t *testing.T, actor sales.Actor, requestID uuid.UUID) string {
	t.Helper()
	var body []byte
	require.NoError(t, e.DB.QueryRow(
		`SELECT response_body FROM idempotency_keys WHERE actor_id = $1 AND key = $2`,
		actor.StaffID, requestID).Scan(&body))
	return string(body)
}

// installCompTrigger installs a forced-failure trigger and removes it when the
// test ends, mirroring the package's other failure-injection fixtures.
func installCompTrigger(t *testing.T, env *compEnv, name, ddl, table string) {
	t.Helper()
	_, err := env.DB.Exec(ddl)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = env.DB.Exec("DROP TRIGGER IF EXISTS " + name + " ON " + table)
		_, _ = env.DB.Exec("DROP FUNCTION IF EXISTS " + name + "()")
	})
}

// ---------- Tests ----------

// TestCompWasteLiveChargedUnit comps an active charged Wasted unit: one
// LIVE_CHECK adjustment, the Check's stored charge reduced, the Comp fact and
// audits, and the updated Service Session in the result.
func TestCompWasteLiveChargedUnit(t *testing.T) {
	env := newCompEnv(t)
	session, unit, wasteID := env.liveWastedUnit(t)
	checkID := env.soleCheckID(t, session.ID)

	before := env.compCheck(t, checkID)
	require.EqualValues(t, 25000, before.ChargeVND)

	note := "  khách đổi ý  "
	result := env.compOK(t, env.compCommand(t, wasteID, sales.CompReasonCafeError, &note))

	assert.Equal(t, sales.CompScopeLiveCheck, result.Scope)
	require.NotNil(t, result.ServiceSession, "a live Comp must return the updated Service Session")
	assert.Nil(t, result.CompletedSaleID)
	assert.Nil(t, result.OutstandingPostSaleRefundVND)
	assert.Empty(t, result.PostSaleCorrections)

	comp := result.Comp
	assert.Equal(t, wasteID, comp.WasteID)
	assert.Equal(t, unit.ID, comp.PreparationUnitID)
	assert.NotEqual(t, uuid.Nil, comp.ChargeAdjustmentID)
	assert.EqualValues(t, 25000, comp.AmountVND)
	assert.Equal(t, sales.CompReasonCafeError, comp.Reason)
	require.NotNil(t, comp.Note)
	assert.Equal(t, "khách đổi ý", *comp.Note, "the note must be trimmed")
	assert.Equal(t, env.Actor.StaffID, comp.ActorStaffIdentityID)
	assert.Equal(t, env.Actor.StaffID, comp.ApprovedByStaffIdentityID, "self-approval records both roles")
	assert.False(t, comp.OccurredAt.IsZero())

	adjustment := env.adjustmentForWaste(t, wasteID)
	assert.Equal(t, sales.ChargeAdjustmentKindComp, adjustment.Kind)
	assert.Equal(t, sales.CompScopeLiveCheck, adjustment.Scope)
	assert.Equal(t, checkID, adjustment.CheckID)
	assert.Equal(t, env.ShiftID, adjustment.SalesShiftID)
	assert.False(t, adjustment.CompletedSaleID.Valid)
	assert.EqualValues(t, 25000, adjustment.AmountVND)
	assert.Equal(t, 1, env.compCount(t, wasteID))

	after := env.compCheck(t, checkID)
	assert.EqualValues(t, 0, after.ChargeVND)
	assert.Equal(t, sales.CheckStateSettled, after.State, "a zero corrected balance settles the open Check")
	assert.Equal(t, 4, after.EvidenceCount)
	assert.Equal(t, uuid.NullUUID{UUID: env.Actor.StaffID, Valid: true}, after.SettledBy)
	assert.Equal(t, uuid.NullUUID{UUID: env.ShiftID, Valid: true}, after.SettledShiftID)
	assert.Equal(t, uuid.NullUUID{UUID: env.Actor.SessionID, Valid: true}, after.SettledSession)

	updated := env.projectedCheck(t, session.ID, checkID)
	assert.EqualValues(t, 0, updated.ChargeVND)
	assert.EqualValues(t, 0, updated.BalanceVND)
	require.Len(t, updated.ChargeAdjustments, 1)
	assert.Equal(t, sales.ChargeAdjustmentKindComp, updated.ChargeAdjustments[0].Kind)
	assert.EqualValues(t, 25000, updated.ChargeAdjustments[0].RemainingRefundableVND)

	require.NotNil(t, result.ServiceSession)
	projected := result.ServiceSession
	require.Len(t, projected.Checks, 1)
	assert.EqualValues(t, 0, projected.Checks[0].ChargeVND)
	require.Len(t, projected.Checks[0].ChargeAdjustments, 1)

	assert.Equal(t, 1, env.countAuditEvents(t, sales.EventSalesCompRecorded))
	assert.Equal(t, 1, env.countAuditEvents(t, sales.EventCheckChargeAdjusted))
	assert.Equal(t, 1, env.countAuditEventsForCheck(t, sales.EventCheckSettled, checkID))
}

// TestCompWasteDuplicateRejects proves the unique one-Comp-per-Waste rule: the
// second command conflicts and writes nothing.
func TestCompWasteDuplicateRejects(t *testing.T) {
	env := newCompEnv(t)
	session, _, wasteID := env.liveWastedUnit(t)
	checkID := env.soleCheckID(t, session.ID)

	env.compOK(t, env.compCommand(t, wasteID, sales.CompReasonCafeError, nil))

	status, _, err := env.comp(t, env.compCommand(t, wasteID, sales.CompReasonServiceRecovery, nil))
	require.ErrorIs(t, err, sales.ErrWasteAlreadyComped)
	assert.Equal(t, http.StatusConflict, status)

	assert.Equal(t, 1, env.compCount(t, wasteID))
	assert.Equal(t, 1, env.adjustmentCountForWaste(t, wasteID))
	assert.EqualValues(t, 0, env.compCheck(t, checkID).ChargeVND)
	assert.Equal(t, 1, env.countAuditEvents(t, sales.EventSalesCompRecorded))
}

// TestCompWasteRemakeRejects proves an uncharged Wasted Remake cannot be
// comped: the replacement has no Charge Allocation.
func TestCompWasteRemakeRejects(t *testing.T) {
	env := newCompEnv(t)
	session, _, wasteID := env.liveWastedUnit(t)
	checkID := env.soleCheckID(t, session.ID)

	remake := env.remakeWaste(t, wasteID)
	env.advanceToReady(t, remake.Unit.ID)
	remakeWasteID := env.wasteUnit(t, remake.Unit.ID)

	status, _, err := env.comp(t, env.compCommand(t, remakeWasteID, sales.CompReasonQualityFailure, nil))
	require.ErrorIs(t, err, sales.ErrCompSourceNotCharged)
	assert.Equal(t, http.StatusConflict, status)

	assert.Equal(t, 0, env.compCount(t, remakeWasteID))
	assert.Equal(t, 0, env.adjustmentCountForWaste(t, remakeWasteID))
	assert.EqualValues(t, 25000, env.compCheck(t, checkID).ChargeVND)
}

// TestCompWastePaidCheckPendingRefund comps a fully paid Check: it stays
// SETTLED, keeps its original evidence, and now owes money back.
func TestCompWastePaidCheckPendingRefund(t *testing.T) {
	env := newCompEnv(t)
	session, _, wasteID, checkID := env.paidTakeawayWastedUnit(t)

	before := env.compCheck(t, checkID)
	require.Equal(t, sales.CheckStateSettled, before.State)
	require.EqualValues(t, 25000, before.ChargeVND)
	settlementAudits := env.countAuditEventsForCheck(t, sales.EventCheckSettled, checkID)

	result := env.compOK(t, env.compCommand(t, wasteID, sales.CompReasonCafeError, nil))
	assert.Equal(t, sales.CompScopeLiveCheck, result.Scope)

	after := env.compCheck(t, checkID)
	assert.Equal(t, sales.CheckStateSettled, after.State, "a settled Check stays settled while a Refund is owed")
	assert.EqualValues(t, 0, after.ChargeVND)
	assert.True(t, before.SettledAt.Time.Equal(after.SettledAt.Time),
		"original settlement evidence is never rewritten")
	assert.Equal(t, before.SettledBy, after.SettledBy)
	assert.Equal(t, before.SettledShiftID, after.SettledShiftID)
	assert.Equal(t, before.SettledSession, after.SettledSession)

	projected := env.projectedCheck(t, session.ID, checkID)
	assert.EqualValues(t, 0, projected.ChargeVND)
	assert.EqualValues(t, 25000, projected.EffectiveReceivedVND)
	assert.EqualValues(t, 25000, projected.PendingRefundVND)
	assert.EqualValues(t, 0, projected.BalanceVND)
	require.Len(t, projected.ChargeAdjustments, 1)
	assert.Equal(t, settlementAudits,
		env.countAuditEventsForCheck(t, sales.EventCheckSettled, checkID),
		"an already-settled Check gains no second settlement event")
}

// TestCompWasteOpenCheckSettledByComp reduces a partially paid open Check to a
// zero corrected balance: the Comp itself settles it with complete evidence.
func TestCompWasteOpenCheckSettledByComp(t *testing.T) {
	env := newCompEnv(t)
	session, _, wasteID := env.liveWastedUnit(t)
	checkID := env.soleCheckID(t, session.ID)

	_, status, err := env.payCash(t, checkID, 10000, 10000)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, sales.CheckStateOpen, env.compCheck(t, checkID).State)

	env.compOK(t, env.compCommand(t, wasteID, sales.CompReasonCafeError, nil))

	after := env.compCheck(t, checkID)
	assert.Equal(t, sales.CheckStateSettled, after.State)
	assert.EqualValues(t, 0, after.ChargeVND)
	assert.Equal(t, 4, after.EvidenceCount)
	assert.Equal(t, uuid.NullUUID{UUID: env.Actor.StaffID, Valid: true}, after.SettledBy)
	assert.Equal(t, uuid.NullUUID{UUID: env.ShiftID, Valid: true}, after.SettledShiftID)

	projected := env.projectedCheck(t, session.ID, checkID)
	assert.EqualValues(t, 10000, projected.EffectiveReceivedVND)
	assert.EqualValues(t, 10000, projected.PendingRefundVND)
	assert.EqualValues(t, 0, projected.BalanceVND)
	assert.Equal(t, 1, env.countAuditEventsForCheck(t, sales.EventCheckSettled, checkID))
}

// TestCompWastePostSale comps a Waste after its Service Session closed: a
// POST_SALE adjustment linked to the Completed Sale, with no rewrite of any
// Check, allocation, settlement, or sale core row.
func TestCompWastePostSale(t *testing.T) {
	env := newCompEnv(t)
	session, _, wasteID := env.liveWastedUnit(t)
	checkID := env.soleCheckID(t, session.ID)

	_, status, err := env.payCash(t, checkID, 25000, 25000)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
	sale := env.Close(t, session.ID)

	before := env.compCheck(t, checkID)
	settledAudits := env.countAuditEventsForCheck(t, sales.EventCheckSettled, checkID)

	result := env.compOK(t, env.compCommand(t, wasteID, sales.CompReasonCafeError, nil))

	assert.Equal(t, sales.CompScopePostSale, result.Scope)
	assert.Nil(t, result.ServiceSession, "a post-sale Comp returns no mutable Service Session")
	require.NotNil(t, result.CompletedSaleID)
	assert.Equal(t, sale.ID, *result.CompletedSaleID)
	require.NotNil(t, result.OutstandingPostSaleRefundVND)
	assert.EqualValues(t, 25000, *result.OutstandingPostSaleRefundVND)
	require.Len(t, result.PostSaleCorrections, 1)
	entry := result.PostSaleCorrections[0]
	assert.Equal(t, sales.CompScopePostSale, entry.Adjustment.Scope)
	require.NotNil(t, entry.Adjustment.CompletedSaleID)
	assert.Equal(t, sale.ID, *entry.Adjustment.CompletedSaleID)
	assert.EqualValues(t, 25000, entry.Adjustment.AmountVND)
	assert.Equal(t, wasteID, entry.Comp.WasteID)
	assert.Empty(t, entry.Refunds, "no Refund exists until the Refund command lands")
	assert.NotNil(t, entry.Refunds, "the Refunds collection is non-null")
	assert.EqualValues(t, 25000, entry.OutstandingRefundVND)

	adjustment := env.adjustmentForWaste(t, wasteID)
	assert.Equal(t, sales.CompScopePostSale, adjustment.Scope)
	require.True(t, adjustment.CompletedSaleID.Valid)
	assert.Equal(t, sale.ID, adjustment.CompletedSaleID.UUID)
	assert.Equal(t, env.ShiftID, adjustment.SalesShiftID)

	after := env.compCheck(t, checkID)
	assert.Equal(t, before.State, after.State, "the closed Check is never rewritten")
	assert.EqualValues(t, before.ChargeVND, after.ChargeVND)
	assert.EqualValues(t, 25000, after.ChargeVND)
	assert.Equal(t, before.EvidenceCount, after.EvidenceCount)
	assert.Equal(t, settledAudits, env.countAuditEventsForCheck(t, sales.EventCheckSettled, checkID),
		"a post-sale Comp writes no settlement event")

	// The live Check projection excludes POST_SALE adjustments structurally.
	projected := env.projectedCheck(t, session.ID, checkID)
	assert.Empty(t, projected.ChargeAdjustments)

	var saleRows int
	require.NoError(t, env.DB.QueryRow(
		`SELECT count(*) FROM completed_sales WHERE id = $1`, sale.ID).Scan(&saleRows))
	assert.Equal(t, 1, saleRows)
}

// TestCompWasteRequiresOpenShift proves the command needs a current open
// Sales Shift even though it touches no Payment.
func TestCompWasteRequiresOpenShift(t *testing.T) {
	env := newCompEnv(t)
	session, _, wasteID := env.liveWastedUnit(t)
	checkID := env.soleCheckID(t, session.ID)

	env.CloseShift(t)
	status, _, err := env.comp(t, env.compCommand(t, wasteID, sales.CompReasonCafeError, nil))
	require.ErrorIs(t, err, sales.ErrOpenShiftRequired)
	assert.Equal(t, http.StatusConflict, status)

	assert.Equal(t, 0, env.compCount(t, wasteID))
	assert.Equal(t, 0, env.adjustmentCountForWaste(t, wasteID))
	assert.EqualValues(t, 25000, env.compCheck(t, checkID).ChargeVND)
}

// TestCompWasteAuthorization covers the one Manager Approval, the initiator's
// own capability, self-approval, separate recording, and replay.
func TestCompWasteAuthorization(t *testing.T) {
	t.Run("denies a wrong manager pin with committed denial evidence", func(t *testing.T) {
		env := newCompEnv(t)
		session, _, wasteID := env.liveWastedUnit(t)
		checkID := env.soleCheckID(t, session.ID)

		cmd := env.compCommand(t, wasteID, sales.CompReasonCafeError, nil)
		cmd.ManagerApproval.ManagerPIN = "00000000"
		status, _, err := env.comp(t, cmd)
		require.ErrorIs(t, err, sales.ErrForbidden)
		assert.Equal(t, http.StatusForbidden, status)

		assert.Equal(t, 0, env.compCount(t, wasteID))
		assert.Equal(t, 0, env.adjustmentCountForWaste(t, wasteID))
		assert.EqualValues(t, 25000, env.compCheck(t, checkID).ChargeVND)
		assert.Equal(t, 1, env.countAuditEvents(t, sales.EventAuthorizationDenied))
		assert.Equal(t, 0, env.idempotencyClaimCount(t, env.Actor, cmd.RequestID),
			"a denied approval must not consume the request id")
	})

	t.Run("denies an approver without the manager role", func(t *testing.T) {
		env := newCompEnv(t)
		_, _, wasteID := env.liveWastedUnit(t)
		cashier := env.newCashierActor(t)

		cmd := env.compCommand(t, wasteID, sales.CompReasonCafeError, nil)
		cmd.ManagerApproval = env.approvalFor(t, cashier)
		status, _, err := env.comp(t, cmd)
		require.ErrorIs(t, err, sales.ErrForbidden)
		assert.Equal(t, http.StatusForbidden, status)
		assert.Equal(t, 0, env.compCount(t, wasteID))
	})

	t.Run("denies an initiator without sales.operate", func(t *testing.T) {
		env := newCompEnv(t)
		_, _, wasteID := env.liveWastedUnit(t)

		cmd := env.compCommand(t, wasteID, sales.CompReasonCafeError, nil)
		status, _, err := env.compAs(t, env.BaristaActor(), cmd)
		require.ErrorIs(t, err, sales.ErrForbidden)
		assert.Equal(t, http.StatusForbidden, status)
		assert.Equal(t, 0, env.compCount(t, wasteID))
	})

	t.Run("records a cashier initiator and manager approver separately", func(t *testing.T) {
		env := newCompEnv(t)
		_, unit, wasteID := env.liveWastedUnit(t)
		cashier := env.newCashierActor(t)

		cmd := env.compCommand(t, wasteID, sales.CompReasonCafeError, nil)
		status, result, err := env.compAs(t, cashier, cmd)
		require.NoError(t, err)
		require.Equal(t, http.StatusCreated, status)
		assert.Equal(t, cashier.StaffID, result.Comp.ActorStaffIdentityID)
		assert.Equal(t, env.Actor.StaffID, result.Comp.ApprovedByStaffIdentityID)
		assert.Equal(t, unit.ID, result.Comp.PreparationUnitID)
	})

	t.Run("replays exactly without duplicate work", func(t *testing.T) {
		env := newCompEnv(t)
		_, _, wasteID := env.liveWastedUnit(t)

		cmd := env.compCommand(t, wasteID, sales.CompReasonCafeError, nil)
		first := env.compOK(t, cmd)

		status, second, err := env.comp(t, cmd)
		require.NoError(t, err)
		require.Equal(t, http.StatusCreated, status)
		firstJSON, err := json.Marshal(first)
		require.NoError(t, err)
		secondJSON, err := json.Marshal(second)
		require.NoError(t, err)
		assert.JSONEq(t, string(firstJSON), string(secondJSON),
			"an exact replay returns the stored result")

		assert.Equal(t, 1, env.compCount(t, wasteID))
		assert.Equal(t, 1, env.adjustmentCountForWaste(t, wasteID))
		assert.Equal(t, 1, env.countAuditEvents(t, sales.EventSalesCompRecorded))
		assert.Equal(t, 1, env.idempotencyClaimCount(t, env.Actor, cmd.RequestID))
	})

	t.Run("replay is denied once the approver is disabled", func(t *testing.T) {
		env := newCompEnv(t)
		_, _, wasteID := env.liveWastedUnit(t)
		cashier := env.newCashierActor(t)

		approval := env.approvalFor(t, env.Actor)
		cmd := sales.CompWasteCommand{
			RequestID:       uuid.New(),
			WasteID:         wasteID,
			Reason:          sales.CompReasonCafeError,
			ManagerApproval: approval,
		}
		status, first, err := env.compAs(t, cashier, cmd)
		require.NoError(t, err)
		require.Equal(t, http.StatusCreated, status)

		_, err = env.DB.Exec(
			`UPDATE staff_identities SET enabled = false WHERE id = $1`, env.Actor.StaffID)
		require.NoError(t, err)

		status, _, err = env.compAs(t, cashier, cmd)
		require.ErrorIs(t, err, sales.ErrForbidden)
		assert.Equal(t, http.StatusForbidden, status,
			"a replay still requires a currently valid Manager Approval")

		// The stored result is unchanged and the work is not repeated.
		var storedCode int
		require.NoError(t, env.DB.QueryRow(
			`SELECT response_code FROM idempotency_keys WHERE actor_id = $1 AND key = $2`,
			cashier.StaffID, cmd.RequestID).Scan(&storedCode))
		assert.Equal(t, http.StatusCreated, storedCode)
		assert.Equal(t, 1, env.compCount(t, wasteID))
		assert.Equal(t, first.Comp.ID, env.compIDForWaste(t, wasteID))
	})

	t.Run("keeps credentials out of storage, results, and audits", func(t *testing.T) {
		env := newCompEnv(t)
		_, _, wasteID := env.liveWastedUnit(t)
		code := env.loginCode(t, env.Actor)

		cmd := env.compCommand(t, wasteID, sales.CompReasonCafeError, nil)
		env.compOK(t, cmd)

		stored := env.storedResultBody(t, env.Actor, cmd.RequestID)
		assert.NotContains(t, stored, `"manager_pin"`)
		assert.NotContains(t, stored, `"approver_login_code"`)
		assert.NotContains(t, stored, code)
		assert.NotContains(t, stored, `"1234"`)

		var details []byte
		require.NoError(t, env.DB.QueryRow(`
			SELECT details FROM audit_events
			WHERE event_type = $1
			ORDER BY occurred_at DESC, id DESC LIMIT 1`,
			sales.EventSalesCompRecorded).Scan(&details))
		text := string(details)
		assert.NotContains(t, text, "manager_pin")
		assert.NotContains(t, text, "approver_login_code")
		assert.NotContains(t, text, code)
		assert.NotContains(t, text, `"1234"`)
	})
}

// compIDForWaste reads the surviving Comp fact's id without invoking the
// command again.
func (e *compEnv) compIDForWaste(t *testing.T, wasteID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	require.NoError(t, e.DB.QueryRow(
		`SELECT id FROM sales_comps WHERE preparation_waste_id = $1`, wasteID).Scan(&id))
	return id
}

// TestCompWasteAuditDetails pins the business meaning each audit records.
func TestCompWasteAuditDetails(t *testing.T) {
	env := newCompEnv(t)
	session, unit, wasteID := env.liveWastedUnit(t)
	checkID := env.soleCheckID(t, session.ID)

	note := "rơi ly"
	result := env.compOK(t, env.compCommand(t, wasteID, sales.CompReasonQualityFailure, &note))

	detailsOf := func(eventType string) map[string]any {
		t.Helper()
		var raw []byte
		require.NoError(t, env.DB.QueryRow(`
			SELECT details FROM audit_events
			WHERE event_type = $1 AND details->>'check_id' = $2
			ORDER BY occurred_at ASC, id ASC LIMIT 1`,
			eventType, checkID.String()).Scan(&raw))
		out := map[string]any{}
		require.NoError(t, json.Unmarshal(raw, &out))
		return out
	}

	adjusted := detailsOf(sales.EventCheckChargeAdjusted)
	assert.Equal(t, checkID.String(), adjusted["check_id"])
	assert.Equal(t, result.Comp.ChargeAdjustmentID.String(), adjusted["charge_adjustment_id"])
	assert.Equal(t, wasteID.String(), adjusted["preparation_waste_id"])
	assert.Equal(t, unit.ID.String(), adjusted["preparation_unit_id"])
	assert.Equal(t, sales.CompScopeLiveCheck, adjusted["scope"])
	assert.EqualValues(t, 25000, adjusted["amount_vnd"])
	assert.EqualValues(t, 25000, adjusted["charge_before_vnd"])
	assert.EqualValues(t, 0, adjusted["charge_after_vnd"])

	settled := detailsOf(sales.EventCheckSettled)
	assert.EqualValues(t, 25000, settled["charge_before_vnd"])
	assert.EqualValues(t, 0, settled["charge_after_vnd"])

	var raw []byte
	require.NoError(t, env.DB.QueryRow(`
		SELECT details FROM audit_events
		WHERE event_type = $1 AND details->>'comp_id' = $2`,
		sales.EventSalesCompRecorded, result.Comp.ID.String()).Scan(&raw))
	recorded := map[string]any{}
	require.NoError(t, json.Unmarshal(raw, &recorded))
	assert.Equal(t, wasteID.String(), recorded["preparation_waste_id"])
	assert.Equal(t, unit.ID.String(), recorded["preparation_unit_id"])
	assert.Equal(t, result.Comp.ChargeAdjustmentID.String(), recorded["charge_adjustment_id"])
	assert.Equal(t, sales.CompScopeLiveCheck, recorded["scope"])
	assert.EqualValues(t, 25000, recorded["amount_vnd"])
	assert.Equal(t, sales.CompReasonQualityFailure, recorded["reason"])
	assert.Equal(t, note, recorded["note"])
	assert.Equal(t, env.Actor.StaffID.String(), recorded["actor_staff_identity_id"])
	assert.Equal(t, env.Actor.StaffID.String(), recorded["approved_by_staff_identity_id"])
}

// TestCompWasteInjectedFailures proves every failure inside the transaction —
// including the executor's stored result — rolls back the whole command.
func TestCompWasteInjectedFailures(t *testing.T) {
	assertRolledBack := func(t *testing.T, env *compEnv, wasteID, checkID uuid.UUID,
		before compCheckRow, requestID uuid.UUID,
	) {
		t.Helper()
		assert.Equal(t, 0, env.compCount(t, wasteID))
		assert.Equal(t, 0, env.adjustmentCountForWaste(t, wasteID))
		after := env.compCheck(t, checkID)
		assert.Equal(t, before.State, after.State)
		assert.EqualValues(t, before.ChargeVND, after.ChargeVND)
		assert.Equal(t, before.EvidenceCount, after.EvidenceCount)
		assert.Equal(t, 0, env.countAuditEvents(t, sales.EventSalesCompRecorded))
		assert.Equal(t, 0, env.idempotencyClaimCount(t, env.Actor, requestID))
	}

	t.Run("adjustment insert failure", func(t *testing.T) {
		env := newCompEnv(t)
		session, unit, wasteID := env.liveWastedUnit(t)
		checkID := env.soleCheckID(t, session.ID)
		before := env.compCheck(t, checkID)
		installCompTrigger(t, env, "fail_comp_adjustment", `
			CREATE FUNCTION fail_comp_adjustment() RETURNS trigger AS $$
			BEGIN
			    IF NEW.preparation_unit_id = '`+unit.ID.String()+`'::uuid THEN
			        RAISE EXCEPTION 'forced comp adjustment failure';
			    END IF;
			    RETURN NEW;
			END;
			$$ LANGUAGE plpgsql;
			CREATE TRIGGER fail_comp_adjustment
			BEFORE INSERT ON charge_adjustments
			FOR EACH ROW EXECUTE FUNCTION fail_comp_adjustment();`, "charge_adjustments")

		cmd := env.compCommand(t, wasteID, sales.CompReasonCafeError, nil)
		status, _, err := env.comp(t, cmd)
		require.Error(t, err)
		require.Equal(t, http.StatusInternalServerError, status)
		assertRolledBack(t, env, wasteID, checkID, before, cmd.RequestID)
	})

	t.Run("comp fact failure", func(t *testing.T) {
		env := newCompEnv(t)
		session, _, wasteID := env.liveWastedUnit(t)
		checkID := env.soleCheckID(t, session.ID)
		before := env.compCheck(t, checkID)
		installCompTrigger(t, env, "fail_comp_fact", `
			CREATE FUNCTION fail_comp_fact() RETURNS trigger AS $$
			BEGIN
			    IF NEW.preparation_waste_id = '`+wasteID.String()+`'::uuid THEN
			        RAISE EXCEPTION 'forced comp fact failure';
			    END IF;
			    RETURN NEW;
			END;
			$$ LANGUAGE plpgsql;
			CREATE TRIGGER fail_comp_fact
			BEFORE INSERT ON sales_comps
			FOR EACH ROW EXECUTE FUNCTION fail_comp_fact();`, "sales_comps")

		cmd := env.compCommand(t, wasteID, sales.CompReasonCafeError, nil)
		status, _, err := env.comp(t, cmd)
		require.Error(t, err)
		require.Equal(t, http.StatusInternalServerError, status)
		assertRolledBack(t, env, wasteID, checkID, before, cmd.RequestID)
	})

	t.Run("audit failure", func(t *testing.T) {
		env := newCompEnv(t)
		session, _, wasteID := env.liveWastedUnit(t)
		checkID := env.soleCheckID(t, session.ID)
		before := env.compCheck(t, checkID)
		installCompTrigger(t, env, "fail_comp_audit", `
			CREATE FUNCTION fail_comp_audit() RETURNS trigger AS $$
			BEGIN
			    IF NEW.event_type = 'SALES_COMP_RECORDED' THEN
			        RAISE EXCEPTION 'forced comp audit failure';
			    END IF;
			    RETURN NEW;
			END;
			$$ LANGUAGE plpgsql;
			CREATE TRIGGER fail_comp_audit
			BEFORE INSERT ON audit_events
			FOR EACH ROW EXECUTE FUNCTION fail_comp_audit();`, "audit_events")

		cmd := env.compCommand(t, wasteID, sales.CompReasonCafeError, nil)
		status, _, err := env.comp(t, cmd)
		require.Error(t, err)
		require.Equal(t, http.StatusInternalServerError, status)
		assertRolledBack(t, env, wasteID, checkID, before, cmd.RequestID)
	})

	t.Run("result storage failure", func(t *testing.T) {
		env := newCompEnv(t)
		session, _, wasteID := env.liveWastedUnit(t)
		checkID := env.soleCheckID(t, session.ID)
		before := env.compCheck(t, checkID)
		installCompTrigger(t, env, "fail_comp_result_store", `
			CREATE FUNCTION fail_comp_result_store() RETURNS trigger AS $$
			BEGIN
			    IF NEW.action = 'sales.comp_waste' AND NEW.response_code <> 0 THEN
			        RAISE EXCEPTION 'forced comp result store failure';
			    END IF;
			    RETURN NEW;
			END;
			$$ LANGUAGE plpgsql;
			CREATE TRIGGER fail_comp_result_store
			BEFORE UPDATE ON idempotency_keys
			FOR EACH ROW EXECUTE FUNCTION fail_comp_result_store();`, "idempotency_keys")

		cmd := env.compCommand(t, wasteID, sales.CompReasonCafeError, nil)
		status, _, err := env.comp(t, cmd)
		require.Error(t, err)
		require.Equal(t, http.StatusInternalServerError, status)
		assertRolledBack(t, env, wasteID, checkID, before, cmd.RequestID)
	})
}

// signInReturningLoginCode is signIn plus the generated login code, so an
// HTTP-level test can name the identity in an inline Manager Approval.
func signInReturningLoginCode(t *testing.T, e *echo.Echo, q *sqlc.Queries,
	roles []string, pin string,
) (string, string) {
	t.Helper()
	ctx := context.Background()

	loginCode := testLoginCode("H")
	hash, err := auth.HashPin(pin)
	require.NoError(t, err)

	row, err := q.CreateStaffIdentity(ctx, sqlc.CreateStaffIdentityParams{
		DisplayName: "HTTP Test " + loginCode,
		Btrim:       loginCode,
		PinHash:     hash,
		Enabled:     true,
	})
	require.NoError(t, err)
	for _, role := range roles {
		require.NoError(t, q.AddStaffRole(ctx, sqlc.AddStaffRoleParams{
			StaffIdentityID: row.ID, Role: role,
		}))
	}

	body, _ := json.Marshal(map[string]string{"login_code": loginCode, "pin": pin})
	rec := doRequest(t, e, http.MethodPost, "/api/v1/auth/sign-in", "", body)
	require.Equal(t, http.StatusOK, rec.Code, "sign-in failed: %s", rec.Body.String())

	var env envelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	var payload struct {
		Token string `json:"token"`
	}
	require.NoError(t, json.Unmarshal(env.Data, &payload))
	require.NotEmpty(t, payload.Token)
	return payload.Token, loginCode
}

// TestSalesHTTPCompWaste drives the route end to end and pins its statuses,
// envelope, authorization, and credential-free body.
func TestSalesHTTPCompWaste(t *testing.T) {
	e, db, q := newTestServer(t)
	managerToken, managerCode := signInReturningLoginCode(t, e, q, []string{"MANAGER"}, "1357")
	baristaToken, _ := signInReturningLoginCode(t, e, q, []string{"BARISTA"}, "2468")
	_ = openShiftOverHTTP(t, e, managerToken)
	tableID := seedTable(t, db, "Bàn Comp")
	itemID := seedMenuItem(t, db, "Cà phê comp", 25000)

	// A Dine-in round needs no Payment before Submit, so the live Comp runs on
	// an OPEN Check.
	body, _ := json.Marshal(map[string]any{
		"request_id": uuid.New(), "table_ids": []uuid.UUID{tableID},
	})
	rec := doRequest(t, e, http.MethodPost,
		"/api/v1/sales/service-sessions/dine-in", managerToken, body)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	session := decodeSession(t, rec)

	body, _ = json.Marshal(map[string]any{"request_id": uuid.New(), "menu_item_id": itemID})
	rec = doRequest(t, e, http.MethodPost,
		"/api/v1/sales/service-sessions/"+session.ID.String()+"/draft/items", managerToken, body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	session = decodeSession(t, rec)

	body, _ = json.Marshal(map[string]any{"request_id": uuid.New()})
	rec = doRequest(t, e, http.MethodPost,
		"/api/v1/sales/service-sessions/"+session.ID.String()+"/draft/commit", managerToken, body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	session = decodeSession(t, rec)

	body, _ = json.Marshal(map[string]any{"request_id": uuid.New()})
	rec = doRequest(t, e, http.MethodPost,
		"/api/v1/sales/service-sessions/"+session.ID.String()+"/submit", managerToken, body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	session = decodeSession(t, rec)
	require.Len(t, session.PreparationUnits, 1)
	unitID := session.PreparationUnits[0].ID

	advance := func(target string) {
		t.Helper()
		body, _ := json.Marshal(map[string]any{
			"request_id": uuid.New(), "target_state": target,
		})
		rec := doRequest(t, e, http.MethodPost,
			"/api/v1/preparation/units/"+unitID.String()+"/advance", baristaToken, body)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	}
	advance("IN_PREPARATION")
	advance("READY")

	body, _ = json.Marshal(map[string]any{
		"request_id": uuid.New(), "reason": "QUALITY_FAILURE",
	})
	rec = doRequest(t, e, http.MethodPost,
		"/api/v1/preparation/units/"+unitID.String()+"/waste", baristaToken, body)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var wasteEnvelope envelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &wasteEnvelope))
	var waste struct {
		ID uuid.UUID `json:"id"`
	}
	require.NoError(t, json.Unmarshal(wasteEnvelope.Data, &waste))
	require.NotEqual(t, uuid.Nil, waste.ID)

	wastePath := "/api/v1/sales/wastes/" + waste.ID.String() + "/comp"
	compBody := func(requestID uuid.UUID, reason string, approval map[string]any) []byte {
		body, _ := json.Marshal(map[string]any{
			"request_id": requestID, "reason": reason, "manager_approval": approval,
		})
		return body
	}
	approval := map[string]any{"approver_login_code": managerCode, "manager_pin": "1357"}

	t.Run("comps and returns 201", func(t *testing.T) {
		requestID := uuid.New()
		rec := doRequest(t, e, http.MethodPost, wastePath, managerToken,
			compBody(requestID, "CAFE_ERROR", approval))
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

		var env envelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
		require.True(t, env.Success)
		var result sales.CompResult
		require.NoError(t, json.Unmarshal(env.Data, &result))
		assert.Equal(t, sales.CompScopeLiveCheck, result.Scope)
		assert.EqualValues(t, 25000, result.Comp.AmountVND)
		assert.Equal(t, waste.ID, result.Comp.WasteID)
		require.NotNil(t, result.ServiceSession)
		assert.Nil(t, result.CompletedSaleID)
		assert.Nil(t, result.OutstandingPostSaleRefundVND)

		raw := rec.Body.String()
		assert.Contains(t, raw, `"service_session"`)
		assert.NotContains(t, raw, `"completed_sale_id"`)
		assert.NotContains(t, raw, `"outstanding_post_sale_refund_vnd"`)
		assert.NotContains(t, raw, `"post_sale_corrections"`)
		assert.NotContains(t, raw, `"manager_pin"`)
		assert.NotContains(t, raw, `"approver_login_code"`)
		assert.NotContains(t, raw, `"1357"`)
	})

	t.Run("duplicate comp answers 409", func(t *testing.T) {
		rec := doRequest(t, e, http.MethodPost, wastePath, managerToken,
			compBody(uuid.New(), "CAFE_ERROR", approval))
		require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
		var env envelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
		require.NotNil(t, env.Error)
		assert.Equal(t, "WASTE_ALREADY_COMPED", env.Error.Code)
	})

	t.Run("unknown waste answers 404", func(t *testing.T) {
		rec := doRequest(t, e, http.MethodPost,
			"/api/v1/sales/wastes/"+uuid.NewString()+"/comp", managerToken,
			compBody(uuid.New(), "CAFE_ERROR", approval))
		require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
		var env envelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
		require.NotNil(t, env.Error)
		assert.Equal(t, "WASTE_NOT_FOUND", env.Error.Code)
	})

	t.Run("invalid reason answers 400", func(t *testing.T) {
		rec := doRequest(t, e, http.MethodPost, wastePath, managerToken,
			compBody(uuid.New(), "PREPARATION_ERROR", approval))
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		var env envelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
		require.NotNil(t, env.Error)
		assert.Equal(t, "INVALID_INPUT", env.Error.Code)
	})

	t.Run("missing request id answers 400", func(t *testing.T) {
		rec := doRequest(t, e, http.MethodPost, wastePath, managerToken,
			[]byte(`{"reason":"CAFE_ERROR","manager_approval":{"approver_login_code":"`+
				managerCode+`","manager_pin":"1357"}}`))
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	})

	t.Run("malformed manager approval answers 400", func(t *testing.T) {
		rec := doRequest(t, e, http.MethodPost, wastePath, managerToken,
			compBody(uuid.New(), "CAFE_ERROR", map[string]any{}))
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		var env envelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
		require.NotNil(t, env.Error)
		assert.Equal(t, "INVALID_INPUT", env.Error.Code)
	})

	t.Run("wrong manager pin answers 403", func(t *testing.T) {
		rec := doRequest(t, e, http.MethodPost, wastePath, managerToken,
			compBody(uuid.New(), "CAFE_ERROR", map[string]any{
				"approver_login_code": managerCode,
				"manager_pin":         "00000000",
			}))
		require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
		var env envelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
		require.NotNil(t, env.Error)
		assert.Equal(t, "NOT_AUTHORIZED", env.Error.Code)
	})

	t.Run("barista is denied by capability", func(t *testing.T) {
		rec := doRequest(t, e, http.MethodPost, wastePath, baristaToken,
			compBody(uuid.New(), "CAFE_ERROR", approval))
		require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
		var env envelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
		require.NotNil(t, env.Error)
		assert.Equal(t, "FORBIDDEN", env.Error.Code)
	})

	t.Run("anonymous is denied by the middleware", func(t *testing.T) {
		rec := doRequest(t, e, http.MethodPost, wastePath, "",
			compBody(uuid.New(), "CAFE_ERROR", approval))
		require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
	})
}
