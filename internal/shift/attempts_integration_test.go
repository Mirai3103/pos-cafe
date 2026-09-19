//go:build integration

package shift_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/Mirai3103/pos-cafe/internal/shift"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startReconciliationExact moves the fixture's Shift to CLOSING with an exact
// initial count: the empty fixture's Expected Cash is its Opening Float
// (500000) and it carries no Manual QR activity, so both QR expectations are
// zero. Attempts are then appended from a known all-exact baseline.
func startReconciliationExact(t *testing.T, f shiftFixture) shift.ClosingShiftResponse {
	t.Helper()
	status, res, err := shift.NewStartReconciliationHandler(f.Runner).Handle(
		context.Background(), f.Cashier.actor(), f.startCommand(500_000, uuid.New()))
	require.NoError(t, err)
	require.Equal(t, 201, status)
	return res
}

func (f shiftFixture) cashCountCommand(counted int64, requestID uuid.UUID) shift.RecordCashCountCommand {
	return shift.RecordCashCountCommand{
		RequestID:      requestID,
		ShiftID:        f.Shift.ID,
		CountedCashVND: int64Ptr(counted),
	}
}

func (f shiftFixture) qrObservationCommand(received, refunded int64, requestID uuid.UUID) shift.RecordQRObservationCommand {
	return shift.RecordQRObservationCommand{
		RequestID:           requestID,
		ShiftID:             f.Shift.ID,
		ObservedReceivedVND: int64Ptr(received),
		ObservedRefundedVND: int64Ptr(refunded),
	}
}

// requireCodedError asserts err maps to the expected HTTP status and stable
// error code.
func requireCodedError(t *testing.T, err error, status int, code string) {
	t.Helper()
	mapped := shift.MapHTTPError(err)
	var coded *response.CodedError
	require.True(t, errors.As(mapped, &coded), "expected a *response.CodedError")
	assert.Equal(t, status, coded.Status)
	assert.Equal(t, code, coded.Code)
}

// TestRecordCashCountAppendsIndependentSequenceAndPreview drives the append
// protocol: a recount appends the next Cash sequence without touching the QR
// ledger, a QR observation's sequence counts independently, each attempt
// persists its own actor and access session, and the returned preview is built
// from the latest evidence after the append.
func TestRecordCashCountAppendsIndependentSequenceAndPreview(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()
	started := startReconciliationExact(t, f)
	reconID := started.Reconciliation.ID
	counts := shift.NewRecordCashCountHandler(f.Runner)
	observations := shift.NewRecordQRObservationHandler(f.Runner)

	// The blind initial count is sequence 1 by the starter.
	require.Len(t, started.Reconciliation.CashCounts, 1)
	assert.Equal(t, 1, started.Reconciliation.CashCounts[0].Sequence)
	assert.Equal(t, f.Cashier.StaffID, started.Reconciliation.CashCounts[0].CountedBy.ID)

	// A recount by a different staff member and session appends sequence 2.
	status, recount, err := counts.Handle(ctx, f.Manager.actor(), f.cashCountCommand(499_000, uuid.New()))
	require.NoError(t, err)
	assert.Equal(t, 201, status)
	assert.Equal(t, 2, recount.CashCount.Sequence)
	assert.Equal(t, int64(499_000), recount.CashCount.CountedCashVND)
	assert.Equal(t, f.Manager.StaffID, recount.CashCount.CountedBy.ID)
	assert.False(t, recount.CashCount.CountedAt.IsZero())

	// The attempt's own access session is persisted beside the actor's.
	var recountSession uuid.UUID
	require.NoError(t, f.DB.QueryRow(
		`SELECT counted_staff_access_session_id FROM shift_cash_counts
		 WHERE reconciliation_id = $1 AND sequence = 2`, reconID).Scan(&recountSession))
	assert.Equal(t, f.Manager.SessionID, recountSession)

	// The returned preview is built from the latest evidence: the Cash
	// dimension moved to a 1000 shortage requiring a recount outcome, and the
	// Shift cannot close exactly.
	require.Len(t, recount.Preview.Dimensions, 3)
	cash := recount.Preview.Dimensions[0]
	require.NotNil(t, cash.ObservedVND)
	assert.Equal(t, int64(499_000), *cash.ObservedVND)
	require.NotNil(t, cash.DifferenceVND)
	assert.Equal(t, int64(-1_000), *cash.DifferenceVND)
	assert.True(t, cash.RecheckRequired)
	assert.False(t, recount.Preview.CanClose)

	// A QR observation's sequence counts independently of the Cash ledger.
	qrStatus, observation, err := observations.Handle(ctx, f.Manager.actor(),
		f.qrObservationCommand(0, 0, uuid.New()))
	require.NoError(t, err)
	assert.Equal(t, 201, qrStatus)
	assert.Equal(t, 1, observation.QRObservation.Sequence)

	// A third Cash count is sequence 3, not 2: the QR append never consumed a
	// Cash sequence.
	thirdStatus, third, err := counts.Handle(ctx, f.Cashier.actor(), f.cashCountCommand(500_000, uuid.New()))
	require.NoError(t, err)
	assert.Equal(t, 201, thirdStatus)
	assert.Equal(t, 3, third.CashCount.Sequence)

	// The latest Cash evidence is exact again, so only the QR observation's
	// presence decides CanClose now.
	cash = third.Preview.Dimensions[0]
	assert.False(t, cash.RecheckRequired)
	assert.True(t, third.Preview.CanClose,
		"exact latest evidence with a QR observation can close exactly")
	require.NotNil(t, third.Preview.Dimensions[1].ObservedVND)
	assert.Equal(t, int64(0), *third.Preview.Dimensions[1].ObservedVND)

	// Persisted ledgers: three Cash counts and exactly one QR observation.
	var cashRows int
	require.NoError(t, f.DB.QueryRow(
		`SELECT count(*) FROM shift_cash_counts WHERE reconciliation_id = $1`,
		reconID).Scan(&cashRows))
	assert.Equal(t, 3, cashRows)
	var qrRows int
	require.NoError(t, f.DB.QueryRow(
		`SELECT count(*) FROM shift_qr_observations WHERE reconciliation_id = $1`,
		reconID).Scan(&qrRows))
	assert.Equal(t, 1, qrRows)

	// One audit event per attempt: the start's initial count plus two recounts.
	assert.Equal(t, 3, countAuditEvents(t, f.DB, shift.EventCashCountRecorded, f.Shift.ID))
	assert.Equal(t, 1, countAuditEvents(t, f.DB, shift.EventQRObservationRecorded, f.Shift.ID))
}

// TestRecordQRObservationStoresPairIncludingZero pins the QR ledger's shape:
// both observed values are explicit including zero, a recheck appends the full
// pair with an independent sequence, and each observation keeps its own actor,
// session, and time.
func TestRecordQRObservationStoresPairIncludingZero(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()
	started := startReconciliationExact(t, f)
	reconID := started.Reconciliation.ID
	observations := shift.NewRecordQRObservationHandler(f.Runner)

	// Explicit zeroes are a meaningful observation, never missing values.
	status, first, err := observations.Handle(ctx, f.Manager.actor(),
		f.qrObservationCommand(0, 0, uuid.New()))
	require.NoError(t, err)
	assert.Equal(t, 201, status)
	assert.Equal(t, 1, first.QRObservation.Sequence)
	assert.Equal(t, int64(0), first.QRObservation.ObservedReceivedVND)
	assert.Equal(t, int64(0), first.QRObservation.ObservedRefundedVND)
	assert.Equal(t, f.Manager.StaffID, first.QRObservation.ObservedBy.ID)
	assert.False(t, first.QRObservation.ObservedAt.IsZero())

	// The zero observation matches the empty fixture's zero expectations.
	for _, dimension := range first.Preview.Dimensions[1:] {
		require.NotNil(t, dimension.DifferenceVND)
		assert.Equal(t, int64(0), *dimension.DifferenceVND)
		assert.False(t, dimension.RecheckRequired)
	}
	// CanClose reflects exact-close readiness, which the exact Cash count and
	// the exact zero observation now satisfy.
	assert.True(t, first.Preview.CanClose)

	// A recheck appends the full received/refunded pair as sequence 2.
	secondStatus, second, err := observations.Handle(ctx, f.Cashier.actor(),
		f.qrObservationCommand(730_000, 50_000, uuid.New()))
	require.NoError(t, err)
	assert.Equal(t, 201, secondStatus)
	assert.Equal(t, 2, second.QRObservation.Sequence)
	assert.Equal(t, int64(730_000), second.QRObservation.ObservedReceivedVND)
	assert.Equal(t, int64(50_000), second.QRObservation.ObservedRefundedVND)
	assert.Equal(t, f.Cashier.StaffID, second.QRObservation.ObservedBy.ID)

	// The preview follows the latest observation, not the first one.
	received := second.Preview.Dimensions[1]
	require.NotNil(t, received.DifferenceVND)
	assert.Equal(t, int64(730_000), *received.DifferenceVND)
	assert.True(t, received.RecheckRequired)
	refunded := second.Preview.Dimensions[2]
	require.NotNil(t, refunded.DifferenceVND)
	assert.Equal(t, int64(50_000), *refunded.DifferenceVND)
	assert.True(t, refunded.RecheckRequired)
	assert.False(t, second.Preview.CanClose)

	// Both rows persist the full pair, including the zero row, each with its
	// own actor and access session.
	var firstReceived, firstRefunded int64
	var firstBy, firstSession uuid.UUID
	require.NoError(t, f.DB.QueryRow(
		`SELECT observed_received_vnd, observed_refunded_vnd,
		        observed_by_staff_identity_id, observed_staff_access_session_id
		 FROM shift_qr_observations WHERE reconciliation_id = $1 AND sequence = 1`,
		reconID).Scan(&firstReceived, &firstRefunded, &firstBy, &firstSession))
	assert.Equal(t, int64(0), firstReceived)
	assert.Equal(t, int64(0), firstRefunded)
	assert.Equal(t, f.Manager.StaffID, firstBy)
	assert.Equal(t, f.Manager.SessionID, firstSession)

	var secondBy, secondSession uuid.UUID
	require.NoError(t, f.DB.QueryRow(
		`SELECT observed_by_staff_identity_id, observed_staff_access_session_id
		 FROM shift_qr_observations WHERE reconciliation_id = $1 AND sequence = 2`,
		reconID).Scan(&secondBy, &secondSession))
	assert.Equal(t, f.Cashier.StaffID, secondBy)
	assert.Equal(t, f.Cashier.SessionID, secondSession)

	// One audit event per observation.
	assert.Equal(t, 2, countAuditEvents(t, f.DB, shift.EventQRObservationRecorded, f.Shift.ID))
}

// TestRecordQRObservationRequiresBothValues rejects one-sided observations:
// both money fields are mandatory, and a rejected command persists nothing
// (spec 14.3: omitted values fail).
func TestRecordQRObservationRequiresBothValues(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()
	started := startReconciliationExact(t, f)
	observations := shift.NewRecordQRObservationHandler(f.Runner)

	cases := []struct {
		name string
		cmd  shift.RecordQRObservationCommand
	}{
		{
			name: "received omitted",
			cmd: shift.RecordQRObservationCommand{
				RequestID: uuid.New(), ShiftID: f.Shift.ID, ObservedRefundedVND: int64Ptr(0),
			},
		},
		{
			name: "refunded omitted",
			cmd: shift.RecordQRObservationCommand{
				RequestID: uuid.New(), ShiftID: f.Shift.ID, ObservedReceivedVND: int64Ptr(0),
			},
		},
		{
			name: "both omitted",
			cmd:  shift.RecordQRObservationCommand{RequestID: uuid.New(), ShiftID: f.Shift.ID},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := observations.Handle(ctx, f.Cashier.actor(), tc.cmd)
			require.Error(t, err)
			assert.ErrorIs(t, err, response.ErrInvalid)
		})
	}

	// No observation row and no audit event was written.
	var rows int
	require.NoError(t, f.DB.QueryRow(
		`SELECT count(*) FROM shift_qr_observations WHERE reconciliation_id = $1`,
		started.Reconciliation.ID).Scan(&rows))
	assert.Equal(t, 0, rows)
	assert.Equal(t, 0, countAuditEvents(t, f.DB, shift.EventQRObservationRecorded, f.Shift.ID))
}

// TestRecordAttemptsRejectNonClosingShifts pins the lifecycle boundary:
// attempts are the inverse of the ordinary open-Shift commands — they require
// CLOSING. An OPEN Shift has no reconciliation to append to, a CLOSED one is
// finished, and an unknown Shift is 404.
func TestRecordAttemptsRejectNonClosingShifts(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()
	counts := shift.NewRecordCashCountHandler(f.Runner)
	observations := shift.NewRecordQRObservationHandler(f.Runner)

	// OPEN: the reconciliation has not started, so the append conflicts.
	_, _, err := counts.Handle(ctx, f.Cashier.actor(), f.cashCountCommand(1, uuid.New()))
	require.Error(t, err)
	assert.ErrorIs(t, err, shift.ErrReconciliationNotStarted)
	requireCodedError(t, err, http.StatusConflict, "SHIFT_RECONCILIATION_NOT_STARTED")

	_, _, err = observations.Handle(ctx, f.Cashier.actor(), f.qrObservationCommand(0, 0, uuid.New()))
	require.Error(t, err)
	assert.ErrorIs(t, err, shift.ErrReconciliationNotStarted)
	requireCodedError(t, err, http.StatusConflict, "SHIFT_RECONCILIATION_NOT_STARTED")

	// CLOSED: the workflow is over and stays closed.
	_, err = f.DB.Exec(`UPDATE sales_shifts SET state = 'CLOSED' WHERE id = $1`, f.Shift.ID)
	require.NoError(t, err)

	_, _, err = counts.Handle(ctx, f.Cashier.actor(), f.cashCountCommand(1, uuid.New()))
	require.Error(t, err)
	assert.ErrorIs(t, err, shift.ErrShiftAlreadyClosed)
	requireCodedError(t, err, http.StatusConflict, "SALES_SHIFT_ALREADY_CLOSED")

	_, _, err = observations.Handle(ctx, f.Cashier.actor(), f.qrObservationCommand(0, 0, uuid.New()))
	require.Error(t, err)
	assert.ErrorIs(t, err, shift.ErrShiftAlreadyClosed)
	requireCodedError(t, err, http.StatusConflict, "SALES_SHIFT_ALREADY_CLOSED")

	// Unknown Shift: 404 on both attempt routes.
	unknownCount := f.cashCountCommand(1, uuid.New())
	unknownCount.ShiftID = uuid.New()
	_, _, err = counts.Handle(ctx, f.Cashier.actor(), unknownCount)
	require.Error(t, err)
	assert.ErrorIs(t, err, shift.ErrSalesShiftNotFound)
	requireCodedError(t, err, http.StatusNotFound, "SALES_SHIFT_NOT_FOUND")

	unknownQR := f.qrObservationCommand(0, 0, uuid.New())
	unknownQR.ShiftID = uuid.New()
	_, _, err = observations.Handle(ctx, f.Cashier.actor(), unknownQR)
	require.Error(t, err)
	assert.ErrorIs(t, err, shift.ErrSalesShiftNotFound)
	requireCodedError(t, err, http.StatusNotFound, "SALES_SHIFT_NOT_FOUND")

	// None of the rejected commands persisted anything.
	var countRows int
	require.NoError(t, f.DB.QueryRow(`SELECT count(*) FROM shift_cash_counts`).Scan(&countRows))
	assert.Equal(t, 0, countRows)
	var qrRows int
	require.NoError(t, f.DB.QueryRow(`SELECT count(*) FROM shift_qr_observations`).Scan(&qrRows))
	assert.Equal(t, 0, qrRows)
}

// TestRecordCashCountIsIdempotent pins the append idempotency contract: an
// exact replay returns the original 201 response without a duplicate attempt
// or audit event, and the same request id with a different amount conflicts.
func TestRecordCashCountIsIdempotent(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()
	started := startReconciliationExact(t, f)
	reconID := started.Reconciliation.ID
	counts := shift.NewRecordCashCountHandler(f.Runner)

	requestID := uuid.New()
	status, first, err := counts.Handle(ctx, f.Cashier.actor(), f.cashCountCommand(499_000, requestID))
	require.NoError(t, err)
	assert.Equal(t, 201, status)

	replayStatus, replay, err := counts.Handle(ctx, f.Cashier.actor(), f.cashCountCommand(499_000, requestID))
	require.NoError(t, err)
	assert.Equal(t, 201, replayStatus, "an exact replay returns the original 201")
	assert.Equal(t, first.CashCount.ID, replay.CashCount.ID)
	assert.Equal(t, first.Preview, replay.Preview)

	// The replay wrote no second attempt and no second audit event: the
	// initial count plus one recount is all there is.
	var countRows int
	require.NoError(t, f.DB.QueryRow(
		`SELECT count(*) FROM shift_cash_counts WHERE reconciliation_id = $1`,
		reconID).Scan(&countRows))
	assert.Equal(t, 2, countRows)
	assert.Equal(t, 2, countAuditEvents(t, f.DB, shift.EventCashCountRecorded, f.Shift.ID))

	// The same request id with a different amount is a conflict, not a replay.
	_, _, err = counts.Handle(ctx, f.Cashier.actor(), f.cashCountCommand(498_000, requestID))
	require.Error(t, err)
	assert.ErrorIs(t, err, shift.ErrRequestConflict)
	requireCodedError(t, err, http.StatusConflict, "REQUEST_CONFLICT")

	// The conflicting request appended nothing.
	require.NoError(t, f.DB.QueryRow(
		`SELECT count(*) FROM shift_cash_counts WHERE reconciliation_id = $1`,
		reconID).Scan(&countRows))
	assert.Equal(t, 2, countRows)
}

// TestRecordQRObservationIsIdempotent pins the same idempotency contract for
// the QR ledger, with both observed values in the fingerprint.
func TestRecordQRObservationIsIdempotent(t *testing.T) {
	f := newShiftFixture(t)
	ctx := context.Background()
	started := startReconciliationExact(t, f)
	reconID := started.Reconciliation.ID
	observations := shift.NewRecordQRObservationHandler(f.Runner)

	requestID := uuid.New()
	status, first, err := observations.Handle(ctx, f.Cashier.actor(),
		f.qrObservationCommand(730_000, 50_000, requestID))
	require.NoError(t, err)
	assert.Equal(t, 201, status)

	replayStatus, replay, err := observations.Handle(ctx, f.Cashier.actor(),
		f.qrObservationCommand(730_000, 50_000, requestID))
	require.NoError(t, err)
	assert.Equal(t, 201, replayStatus, "an exact replay returns the original 201")
	assert.Equal(t, first.QRObservation.ID, replay.QRObservation.ID)
	assert.Equal(t, first.Preview, replay.Preview)

	var qrRows int
	require.NoError(t, f.DB.QueryRow(
		`SELECT count(*) FROM shift_qr_observations WHERE reconciliation_id = $1`,
		reconID).Scan(&qrRows))
	assert.Equal(t, 1, qrRows)
	assert.Equal(t, 1, countAuditEvents(t, f.DB, shift.EventQRObservationRecorded, f.Shift.ID))

	// Changing either observed value breaks the fingerprint.
	_, _, err = observations.Handle(ctx, f.Cashier.actor(),
		f.qrObservationCommand(730_000, 50_001, requestID))
	require.Error(t, err)
	assert.ErrorIs(t, err, shift.ErrRequestConflict)
	requireCodedError(t, err, http.StatusConflict, "REQUEST_CONFLICT")
}

// startReconciliationViaHTTP opens a Shift and starts its reconciliation
// through the HTTP API with an exact initial count, returning the Shift id.
func startReconciliationViaHTTP(t *testing.T, e *echo.Echo, token string) string {
	t.Helper()

	body, _ := json.Marshal(map[string]any{"request_id": uuid.New(), "opening_float_vnd": 500000})
	rec := doRequest(t, e, http.MethodPost, "/api/v1/shifts", token, body)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var env envelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	var opened shift.SalesShiftResponse
	require.NoError(t, json.Unmarshal(env.Data, &opened))

	body, _ = json.Marshal(map[string]any{"request_id": uuid.New(), "counted_cash_vnd": 500000})
	rec = doRequest(t, e, http.MethodPost,
		"/api/v1/shifts/"+opened.ID.String()+"/reconciliation", token, body)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	return opened.ID.String()
}

// TestShiftHTTPRecordQRObservationHappyPath drives the route end to end with
// explicit zeroes and pins the response's field allowlist: the appended
// observation plus the full preview (spec 9.3, 9.6).
func TestShiftHTTPRecordQRObservationHappyPath(t *testing.T) {
	e, q := newTestServer(t)
	token, _ := signIn(t, e, q, []string{"CASHIER"}, "2468")
	shiftID := startReconciliationViaHTTP(t, e, token)

	body, _ := json.Marshal(map[string]any{
		"request_id":            uuid.New(),
		"observed_received_vnd": 0,
		"observed_refunded_vnd": 0,
	})
	rec := doRequest(t, e, http.MethodPost,
		"/api/v1/shifts/"+shiftID+"/reconciliation/qr-observations", token, body)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var env envelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	require.True(t, env.Success)

	var data map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(env.Data, &data))
	assert.ElementsMatch(t, []string{"qr_observation", "preview"}, jsonKeys(data))

	var observation map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data["qr_observation"], &observation))
	assert.ElementsMatch(t,
		[]string{"id", "sequence", "observed_received_vnd", "observed_refunded_vnd", "observed_by", "observed_at"},
		jsonKeys(observation))
	assert.JSONEq(t, "0", string(observation["observed_received_vnd"]))
	assert.JSONEq(t, "0", string(observation["observed_refunded_vnd"]))
	assert.JSONEq(t, "1", string(observation["sequence"]))

	var preview map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data["preview"], &preview))
	assert.ElementsMatch(t, []string{"dimensions", "can_close"}, jsonKeys(preview))
}

// TestShiftHTTPRecordQRObservationValidation covers the boundary rejections:
// one-sided or negative observations are 400 before any transaction, a
// malformed Shift id is 400, and an unknown Shift is 404.
func TestShiftHTTPRecordQRObservationValidation(t *testing.T) {
	e, q := newTestServer(t)
	token, _ := signIn(t, e, q, []string{"CASHIER"}, "2468")
	shiftID := startReconciliationViaHTTP(t, e, token)

	cases := []struct {
		name string
		body map[string]any
	}{
		{
			name: "received omitted",
			body: map[string]any{"request_id": uuid.New(), "observed_refunded_vnd": 0},
		},
		{
			name: "refunded omitted",
			body: map[string]any{"request_id": uuid.New(), "observed_received_vnd": 0},
		},
		{
			name: "both omitted",
			body: map[string]any{"request_id": uuid.New()},
		},
		{
			name: "negative received",
			body: map[string]any{"request_id": uuid.New(), "observed_received_vnd": -1, "observed_refunded_vnd": 0},
		},
		{
			name: "missing request_id",
			body: map[string]any{"observed_received_vnd": 0, "observed_refunded_vnd": 0},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(tc.body)
			rec := doRequest(t, e, http.MethodPost,
				"/api/v1/shifts/"+shiftID+"/reconciliation/qr-observations", token, body)
			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())

			var errEnv envelope
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errEnv))
			require.NotNil(t, errEnv.Error)
			assert.Equal(t, "INVALID_INPUT", errEnv.Error.Code)
		})
	}

	t.Run("malformed shift_id", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"request_id": uuid.New(), "observed_received_vnd": 0, "observed_refunded_vnd": 0,
		})
		rec := doRequest(t, e, http.MethodPost,
			"/api/v1/shifts/not-a-uuid/reconciliation/qr-observations", token, body)
		assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	})

	t.Run("unknown shift", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"request_id": uuid.New(), "observed_received_vnd": 0, "observed_refunded_vnd": 0,
		})
		rec := doRequest(t, e, http.MethodPost,
			"/api/v1/shifts/"+uuid.New().String()+"/reconciliation/qr-observations", token, body)
		require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())

		var errEnv envelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errEnv))
		require.NotNil(t, errEnv.Error)
		assert.Equal(t, "SALES_SHIFT_NOT_FOUND", errEnv.Error.Code)
	})
}

// TestShiftHTTPRecordCashCountHappyPath drives the cash-count route end to
// end: 201 with the appended attempt plus the full preview, and an omitted
// counted_cash_vnd is rejected before any transaction.
func TestShiftHTTPRecordCashCountHappyPath(t *testing.T) {
	e, q := newTestServer(t)
	token, _ := signIn(t, e, q, []string{"CASHIER"}, "2468")
	shiftID := startReconciliationViaHTTP(t, e, token)

	t.Run("omitted counted_cash_vnd", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"request_id": uuid.New()})
		rec := doRequest(t, e, http.MethodPost,
			"/api/v1/shifts/"+shiftID+"/reconciliation/cash-counts", token, body)
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())

		var errEnv envelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errEnv))
		require.NotNil(t, errEnv.Error)
		assert.Equal(t, "INVALID_INPUT", errEnv.Error.Code)
	})

	t.Run("appends the recount", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"request_id": uuid.New(), "counted_cash_vnd": 499000})
		rec := doRequest(t, e, http.MethodPost,
			"/api/v1/shifts/"+shiftID+"/reconciliation/cash-counts", token, body)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

		var env envelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
		require.True(t, env.Success)

		var data map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(env.Data, &data))
		assert.ElementsMatch(t, []string{"cash_count", "preview"}, jsonKeys(data))

		var count map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(data["cash_count"], &count))
		assert.ElementsMatch(t,
			[]string{"id", "sequence", "counted_cash_vnd", "counted_by", "counted_at"},
			jsonKeys(count))
		assert.JSONEq(t, "499000", string(count["counted_cash_vnd"]))
		assert.JSONEq(t, "2", string(count["sequence"]))
	})

	t.Run("appends an explicit zero recount", func(t *testing.T) {
		// JSON 0 is a present, meaningful count — an empty drawer — and must
		// be distinguished from an omitted field (spec 7.2): the boundary
		// accepts it and the sequence advances.
		body, _ := json.Marshal(map[string]any{"request_id": uuid.New(), "counted_cash_vnd": 0})
		rec := doRequest(t, e, http.MethodPost,
			"/api/v1/shifts/"+shiftID+"/reconciliation/cash-counts", token, body)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

		var env envelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
		require.True(t, env.Success)

		var data map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(env.Data, &data))
		var count map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(data["cash_count"], &count))
		assert.JSONEq(t, "0", string(count["counted_cash_vnd"]),
			"an explicit zero is stored, not defaulted")
		assert.JSONEq(t, "3", string(count["sequence"]))
	})
}
