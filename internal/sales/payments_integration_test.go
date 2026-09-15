//go:build integration

package sales_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPayCash(t *testing.T) {
	env := newSalesEnv(t)
	ctx := context.Background()

	t.Run("paying in full settles the check with complete evidence", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 2)
		checkID := env.soleCheckID(t, session.ID)
		charge := env.checkCharge(t, checkID)

		got, _, err := env.payCash(t, checkID, charge, charge+15_000)
		require.NoError(t, err)

		require.Len(t, got.Checks, 1)
		require.Equal(t, sales.CheckStateSettled, got.Checks[0].State)
		require.Equal(t, charge, got.Checks[0].TotalAppliedVND)
		require.Equal(t, int64(0), got.Checks[0].BalanceVND)
		require.Len(t, got.Checks[0].Payments, 1)
		require.Equal(t, int64(15_000), *got.Checks[0].Payments[0].ChangeDueVND)

		var settledBy, shift, session2 uuid.UUID
		require.NoError(t, env.DB.QueryRowContext(ctx, `
			SELECT settled_by_staff_identity_id, settled_during_sales_shift_id,
			       settled_staff_access_session_id
			FROM checks WHERE id = $1`, checkID).Scan(&settledBy, &shift, &session2))
		require.Equal(t, env.Actor.StaffID, settledBy)
		require.Equal(t, env.ShiftID, shift)
		require.Equal(t, env.Actor.SessionID, session2)
	})

	t.Run("a partial payment leaves the check open", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 2)
		checkID := env.soleCheckID(t, session.ID)
		charge := env.checkCharge(t, checkID)

		got, _, err := env.payCash(t, checkID, charge-1_000, charge-1_000)
		require.NoError(t, err)
		require.Equal(t, sales.CheckStateOpen, got.Checks[0].State)
		require.Equal(t, int64(1_000), got.Checks[0].BalanceVND)
	})

	t.Run("over the balance is rejected and records nothing", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 1)
		checkID := env.soleCheckID(t, session.ID)
		charge := env.checkCharge(t, checkID)

		_, status, err := env.payCash(t, checkID, charge+1, charge+1)
		require.Error(t, err)
		require.Equal(t, http.StatusConflict, status)
		require.ErrorIs(t, err, sales.ErrPaymentExceedsBalance)
		require.Equal(t, 0, env.countPayments(t, checkID))
	})

	t.Run("under-tendered cash is rejected", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 1)
		checkID := env.soleCheckID(t, session.ID)
		charge := env.checkCharge(t, checkID)

		_, _, err := env.payCash(t, checkID, charge, charge-1)
		require.ErrorIs(t, err, sales.ErrInsufficientCashTendered)
		require.Equal(t, 0, env.countPayments(t, checkID))
	})

	t.Run("paying a settled check reports CHECK_NOT_OPEN", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 1)
		checkID := env.soleCheckID(t, session.ID)
		charge := env.checkCharge(t, checkID)

		_, _, err := env.payCash(t, checkID, charge, charge)
		require.NoError(t, err)

		_, _, err = env.payCash(t, checkID, 1_000, 1_000)
		require.ErrorIs(t, err, sales.ErrCheckNotOpen)
	})

	t.Run("an unknown check reports CHECK_NOT_FOUND", func(t *testing.T) {
		_, status, err := env.payCash(t, uuid.New(), 1_000, 1_000)
		require.ErrorIs(t, err, sales.ErrCheckNotFound)
		require.Equal(t, http.StatusNotFound, status)
	})

	t.Run("a settling payment writes two audit events", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 1)
		checkID := env.soleCheckID(t, session.ID)
		charge := env.checkCharge(t, checkID)

		before := env.countAuditEvents(t, sales.EventCheckSettled)
		_, _, err := env.payCash(t, checkID, charge, charge)
		require.NoError(t, err)

		require.Equal(t, before+1, env.countAuditEvents(t, sales.EventCheckSettled))
		require.Equal(t, 1, env.countAuditEventsForCheck(t, sales.EventCashPaymentRecorded, checkID))
	})

	t.Run("an exact replay records only one payment", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 1)
		checkID := env.soleCheckID(t, session.ID)
		charge := env.checkCharge(t, checkID)
		requestID := uuid.New()

		first, _, err := env.payCashWithRequestID(t, requestID, checkID, charge, charge)
		require.NoError(t, err)
		second, _, err := env.payCashWithRequestID(t, requestID, checkID, charge, charge)
		require.NoError(t, err)

		require.Equal(t, first.Checks[0].ID, second.Checks[0].ID)
		require.Equal(t, 1, env.countPayments(t, checkID))
	})

	t.Run("the same request id with a different amount conflicts", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 2)
		checkID := env.soleCheckID(t, session.ID)
		requestID := uuid.New()

		_, _, err := env.payCashWithRequestID(t, requestID, checkID, 1_000, 1_000)
		require.NoError(t, err)
		_, _, err = env.payCashWithRequestID(t, requestID, checkID, 2_000, 2_000)
		require.ErrorIs(t, err, sales.ErrRequestConflict)
	})

	t.Run("a barista is denied", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 1)
		checkID := env.soleCheckID(t, session.ID)

		_, status, err := env.payCashAs(t, env.BaristaActor(), checkID, 1_000, 1_000)
		require.Error(t, err)
		require.Equal(t, http.StatusForbidden, status)
		require.Equal(t, 0, env.countPayments(t, checkID))
	})
}

func TestPayManualQR(t *testing.T) {
	env := newSalesEnv(t)

	t.Run("paying in full settles the check and stores no cash fields", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 2)
		checkID := env.soleCheckID(t, session.ID)
		charge := env.checkCharge(t, checkID)
		ref := "FT24012345"

		got, _, err := env.payManualQR(t, checkID, charge, true, &ref)
		require.NoError(t, err)

		require.Equal(t, sales.CheckStateSettled, got.Checks[0].State)
		require.Len(t, got.Checks[0].Payments, 1)
		payment := got.Checks[0].Payments[0]
		require.Equal(t, sales.PaymentMethodManualQR, payment.Method)
		require.Nil(t, payment.CashTenderedVND)
		require.Nil(t, payment.ChangeDueVND)
		require.Equal(t, "FT24012345", *payment.TransactionReference)
	})

	t.Run("a payment without a reference is accepted", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 1)
		checkID := env.soleCheckID(t, session.ID)
		charge := env.checkCharge(t, checkID)

		got, _, err := env.payManualQR(t, checkID, charge, true, nil)
		require.NoError(t, err)
		require.Nil(t, got.Checks[0].Payments[0].TransactionReference)
	})

	t.Run("an unconfirmed receipt is rejected", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 1)
		checkID := env.soleCheckID(t, session.ID)
		charge := env.checkCharge(t, checkID)

		_, status, err := env.payManualQR(t, checkID, charge, false, nil)
		require.ErrorIs(t, err, sales.ErrManualQRReceiptRequired)
		require.Equal(t, http.StatusConflict, status)
		require.Equal(t, 0, env.countPayments(t, checkID))
	})

	t.Run("an over-long reference is a validation error", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 1)
		checkID := env.soleCheckID(t, session.ID)
		long := strings.Repeat("A", 101)

		_, status, err := env.payManualQR(t, checkID, 1_000, true, &long)
		require.Error(t, err)
		require.Equal(t, http.StatusBadRequest, status)
	})

	t.Run("the receipt attestation lives in the audit event, not the row", func(t *testing.T) {
		session := env.commitTakeawayDraft(t, 1)
		checkID := env.soleCheckID(t, session.ID)
		charge := env.checkCharge(t, checkID)

		_, _, err := env.payManualQR(t, checkID, charge, true, nil)
		require.NoError(t, err)

		require.Equal(t, 1,
			env.countAuditEventsForCheck(t, sales.EventManualQRPaymentRecorded, checkID))
		var attested bool
		require.NoError(t, env.DB.QueryRow(`
			SELECT (details->>'receipt_observed_in_bank_app')::boolean
			FROM audit_events
			WHERE event_type = $1 AND details->>'check_id' = $2`,
			sales.EventManualQRPaymentRecorded, checkID).Scan(&attested))
		require.True(t, attested,
			"the receipt attestation must be recorded in the audit event")

		// A MANUAL_QR row stores no cash fields — the attestation is stored
		// nowhere else because a constant-true column stores nothing.
		var rows, cashRows int
		require.NoError(t, env.DB.QueryRow(`
			SELECT count(*), count(cash_tendered_vnd)
			FROM payments WHERE check_id = $1`, checkID).Scan(&rows, &cashRows))
		require.Equal(t, 1, rows)
		require.Equal(t, 0, cashRows)
	})
}

// Mixed settlement: cash then manual QR closes the Check.
func TestMixedPaymentSettlement(t *testing.T) {
	env := newSalesEnv(t)

	session := env.commitTakeawayDraft(t, 2)
	checkID := env.soleCheckID(t, session.ID)
	charge := env.checkCharge(t, checkID)
	half := charge / 2

	got, _, err := env.payCash(t, checkID, half, half)
	require.NoError(t, err)
	require.Equal(t, sales.CheckStateOpen, got.Checks[0].State)
	require.Equal(t, charge-half, got.Checks[0].BalanceVND)

	got, _, err = env.payManualQR(t, checkID, charge-half, true, nil)
	require.NoError(t, err)
	require.Equal(t, sales.CheckStateSettled, got.Checks[0].State)
	require.Equal(t, int64(0), got.Checks[0].BalanceVND)
	require.Len(t, got.Checks[0].Payments, 2)
	require.Equal(t, sales.PaymentMethodCash, got.Checks[0].Payments[0].Method)
	require.Equal(t, sales.PaymentMethodManualQR, got.Checks[0].Payments[1].Method)
}

// TestPaymentPreconditionsReportDistinctCodes pins the two parent-state
// payment preconditions the other suites never reach: a closed Service Session
// and a closed Sales Shift are distinct conditions with distinct codes (spec
// §11.2 item 6). No 5C operation closes either parent, so the states are
// seeded the way the fixtures seed every other unreachable state: direct SQL.
func TestPaymentPreconditionsReportDistinctCodes(t *testing.T) {
	t.Run("a closed session reports SERVICE_SESSION_ALREADY_CLOSED", func(t *testing.T) {
		env := newSalesEnv(t)
		session := env.commitTakeawayDraft(t, 1)
		checkID := env.soleCheckID(t, session.ID)

		_, err := env.DB.Exec(
			`UPDATE service_sessions SET state = 'CLOSED' WHERE id = $1`, session.ID)
		require.NoError(t, err)

		_, status, err := env.payCash(t, checkID, 1_000, 1_000)
		require.ErrorIs(t, err, sales.ErrServiceSessionClosed)
		require.Equal(t, http.StatusConflict, status)
		require.Equal(t, "SERVICE_SESSION_ALREADY_CLOSED", paymentErrorCode(t, err))
		require.Equal(t, 0, env.countPayments(t, checkID))
	})

	t.Run("a closed shift reports OPEN_SALES_SHIFT_REQUIRED", func(t *testing.T) {
		env := newSalesEnv(t)
		session := env.commitTakeawayDraft(t, 1)
		checkID := env.soleCheckID(t, session.ID)
		env.CloseShift(t)

		_, status, err := env.payManualQR(t, checkID, 1_000, true, nil)
		require.ErrorIs(t, err, sales.ErrOpenShiftRequired)
		require.Equal(t, http.StatusConflict, status)
		require.Equal(t, "OPEN_SALES_SHIFT_REQUIRED", paymentErrorCode(t, err))
		require.Equal(t, 0, env.countPayments(t, checkID))
	})
}

// TestPaymentIsAttributedToTheShiftItWasReceivedIn pins ADR-019.
//
// A Payment stores its own sales_shift_id because the Shift in which the money
// reached the cashier is an independent fact from the Shift the Session was
// opened in: a Session opened near the end of one Shift can be paid during the
// next. Deriving the Payment's Shift through service_sessions would attribute
// the cash to the wrong drawer and answer the reconciliation question wrongly.
func TestPaymentIsAttributedToTheShiftItWasReceivedIn(t *testing.T) {
	env := newSalesEnv(t)
	session := env.commitTakeawayDraft(t, 1)
	checkID := env.soleCheckID(t, session.ID)

	openedIn := env.ShiftID

	// The Session outlives its Shift. Phase 4 ships no close command, so the
	// seeded Shift is closed by direct SQL, the same way the fixture seeds
	// every other unreachable state.
	env.CloseShift(t)
	paidIn := seedOpenShift(t, env.Queries, env.Actor.StaffID)
	require.NotEqual(t, openedIn, paidIn, "the fixture must open a second Shift")

	got, _, err := env.payCash(t, checkID, 1_000, 1_000)
	require.NoError(t, err)

	payments := env.findCheck(t, got, checkID).Payments
	require.Len(t, payments, 1)
	require.Equal(t, paidIn, payments[0].SalesShiftID,
		"the Payment belongs to the Shift that received it, not the one its Session opened in")
}

// TestPaymentPreconditionPrecedence pins the order in which the three Check
// preconditions are evaluated, which is the order every Check command shares
// (spec §6.2: Check state, then Session, then Shift). A client in several bad
// states at once must be told about the same one whichever command it calls,
// so the order is observable behaviour rather than an implementation detail.
func TestPaymentPreconditionPrecedence(t *testing.T) {
	// closeSession and closeShift drive the Session and Shift into the states
	// no 5C operation produces, the same way TestPaymentPreconditions-
	// ReportDistinctCodes does.
	closeSession := func(t *testing.T, env *salesEnv, sessionID uuid.UUID) {
		t.Helper()
		_, err := env.DB.Exec(
			`UPDATE service_sessions SET state = 'CLOSED' WHERE id = $1`, sessionID)
		require.NoError(t, err)
	}

	t.Run("the session is reported before the shift", func(t *testing.T) {
		env := newSalesEnv(t)
		session := env.commitTakeawayDraft(t, 1)
		checkID := env.soleCheckID(t, session.ID)

		closeSession(t, env, session.ID)
		env.CloseShift(t)

		_, status, err := env.payCash(t, checkID, 1_000, 1_000)
		require.ErrorIs(t, err, sales.ErrServiceSessionClosed)
		require.Equal(t, http.StatusConflict, status)
		require.Equal(t, "SERVICE_SESSION_ALREADY_CLOSED", paymentErrorCode(t, err))
		require.Equal(t, 0, env.countPayments(t, checkID))
	})

	t.Run("the check is reported before the session and the shift", func(t *testing.T) {
		env := newSalesEnv(t)
		session := env.commitTakeawayDraft(t, 2)
		sourceID := env.soleCheckID(t, session.ID)
		allocations := env.checkAllocations(t, session.ID, sourceID)

		// Splitting creates the second Check, and merging the two leaves that
		// second Check MERGED. Merging is the only 5C operation that produces
		// a non-OPEN Check, so the state is reached through the real path
		// rather than seeded.
		split, _, err := env.splitToNewCheck(t, sourceID, []sales.SplitItem{
			{CommittedItemID: allocations[0].CommittedItemID, Quantity: 1},
		})
		require.NoError(t, err)
		mergedID := env.otherCheckID(t, split, sourceID)

		_, _, err = env.mergeChecks(t, sourceID, mergedID)
		require.NoError(t, err)

		closeSession(t, env, session.ID)
		env.CloseShift(t)

		// The absorbed Check is MERGED, its Session is CLOSED, and no Shift is
		// open: all three preconditions fail, and the Check's own state is the
		// one that must surface.
		_, status, err := env.payCash(t, mergedID, 1_000, 1_000)
		require.ErrorIs(t, err, sales.ErrCheckNotOpen)
		require.Equal(t, http.StatusConflict, status)
		require.Equal(t, "CHECK_NOT_OPEN", paymentErrorCode(t, err))
		require.Equal(t, 0, env.countPayments(t, mergedID))
	})
}

// paymentErrorCode maps an error the way the HTTP layer would and returns its
// stable code string.
func paymentErrorCode(t *testing.T, err error) string {
	t.Helper()
	_, mapped := mapErrorStatus(err)
	var coded *response.CodedError
	require.ErrorAs(t, mapped, &coded, "expected a coded error, got %v", mapped)
	return coded.Code
}

// Every 5C route must deny a BARISTA and an actor whose capability was
// removed mid-session, and must change nothing when it does.
func TestPhase5CAuthorization(t *testing.T) {
	env := newSalesEnv(t)

	session := env.commitTakeawayDraft(t, 2)
	checkID := env.soleCheckID(t, session.ID)
	allocations := env.checkAllocations(t, session.ID, checkID)
	charge := env.checkCharge(t, checkID)

	calls := map[string]func(actor sales.Actor) (int, error){
		"pay cash": func(actor sales.Actor) (int, error) {
			_, status, err := env.payCashAs(t, actor, checkID, 1_000, 1_000)
			return status, err
		},
		"pay manual qr": func(actor sales.Actor) (int, error) {
			_, status, err := env.payManualQRAs(t, actor, checkID, 1_000, true, nil)
			return status, err
		},
		"split check": func(actor sales.Actor) (int, error) {
			_, status, err := env.splitToNewCheckAs(t, actor, checkID, []sales.SplitItem{
				{CommittedItemID: allocations[0].CommittedItemID, Quantity: 1},
			})
			return status, err
		},
		"merge checks": func(actor sales.Actor) (int, error) {
			_, status, err := env.mergeChecksAs(t, actor, checkID, uuid.New())
			return status, err
		},
	}

	for name, call := range calls {
		t.Run(name+" denies a barista", func(t *testing.T) {
			status, err := call(env.BaristaActor())
			require.Error(t, err)
			require.Equal(t, http.StatusForbidden, status)
		})
	}

	t.Run("authority is reloaded inside the transaction", func(t *testing.T) {
		actor := env.newCashierActor(t)
		env.revokeCapability(t, actor, sales.CapSalesOperate)

		_, status, err := env.payCashAs(t, actor, checkID, charge, charge)
		require.Error(t, err)
		require.Equal(t, http.StatusForbidden, status)
		require.Equal(t, 0, env.countPayments(t, checkID))
	})
}
