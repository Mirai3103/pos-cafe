//go:build integration

package shift_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/Mirai3103/pos-cafe/internal/shift"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Failure injection for the Phase 07 commands (spec 4.2, 4.4, 13, 14.3): each
// durable write is followed by an injected failure, and the test proves the
// whole command rolled back — no partial reconciliation, attempt, closure,
// discrepancy, audit success event, or idempotency result.
//
// The injection mechanism is the package's established test-only PostgreSQL
// trigger (the Sales comp suite's TestCompWasteInjectedFailures pattern): a
// BEFORE trigger raises mid-transaction at exactly the write the subtest
// names. The executor has no fault hook and gains none; the trigger lives in
// the test database only and is dropped by cleanup.

// installReconciliationTrigger creates one test-only fault trigger and drops
// it (and its function) when the test ends.
func installReconciliationTrigger(t *testing.T, db *sql.DB, name, ddl, table string) {
	t.Helper()
	_, err := db.Exec(ddl)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = db.Exec("DROP TRIGGER IF EXISTS " + name + " ON " + table)
		_, _ = db.Exec("DROP FUNCTION IF EXISTS " + name + "()")
	})
}

// requireInjectedFailure asserts the command failed with the raw injected
// PostgreSQL error. It maps through the HTTP boundary as an unmapped
// infrastructure failure — the generic 500 envelope — never a stable
// client-facing code (spec 12).
func requireInjectedFailure(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	mapped := shift.MapHTTPError(err)
	var coded *response.CodedError
	require.False(t, errors.As(mapped, &coded),
		"an injected infrastructure failure must not map to a stable client code, got %v", mapped)
}

// requireNothingPersisted proves a rolled-back START left no partial fact of
// any kind: no reconciliation, no attempt of either ledger, no closure, no
// discrepancy, no success audit event, no idempotency claim, and the Shift
// still OPEN.
func requireNothingPersisted(t *testing.T, f shiftFixture, requestID uuid.UUID) {
	t.Helper()
	requireNoReconciliationRows(t, f)
	for _, event := range []string{
		shift.EventReconciliationStarted,
		shift.EventCashCountRecorded,
		shift.EventQRObservationRecorded,
		shift.EventShiftClosedExact,
		shift.EventShiftClosedWithDiscrepancy,
	} {
		assert.Equal(t, 0, countAuditEvents(t, f.DB, event, f.Shift.ID),
			"no %s success event may survive", event)
	}
	assert.Equal(t, 0, countIdempotencyClaims(t, f.DB, f.Cashier.StaffID, requestID),
		"the rolled-back command must leave no idempotency claim or stored result")
	assert.Equal(t, shift.StateOpen, requireShiftState(t, f))
}

// requireNoReconciliationRows asserts the Shift carries no reconciliation and
// no attempt rows at all.
func requireNoReconciliationRows(t *testing.T, f shiftFixture) {
	t.Helper()
	var reconRows, countRows, observationRows, closureRows, discrepancyRows int
	require.NoError(t, f.DB.QueryRow(
		`SELECT count(*) FROM shift_reconciliations`).Scan(&reconRows))
	require.NoError(t, f.DB.QueryRow(
		`SELECT count(*) FROM shift_cash_counts`).Scan(&countRows))
	require.NoError(t, f.DB.QueryRow(
		`SELECT count(*) FROM shift_qr_observations`).Scan(&observationRows))
	require.NoError(t, f.DB.QueryRow(
		`SELECT count(*) FROM shift_closures`).Scan(&closureRows))
	require.NoError(t, f.DB.QueryRow(
		`SELECT count(*) FROM shift_discrepancies`).Scan(&discrepancyRows))
	assert.Zero(t, reconRows, "no reconciliation row may survive")
	assert.Zero(t, countRows, "no cash count attempt may survive")
	assert.Zero(t, observationRows, "no QR observation attempt may survive")
	assert.Zero(t, closureRows, "no closure row may survive")
	assert.Zero(t, discrepancyRows, "no discrepancy row may survive")
}

// requireCloseNothingPersisted proves a rolled-back CLOSE left no closure
// fact, while the reconciliation it closed against remains intact: no
// closure, no discrepancy, no closure audit event, no idempotency claim, and
// the Shift still CLOSING with its frozen snapshot and attempts untouched.
func requireCloseNothingPersisted(t *testing.T, f shiftFixture, requestID uuid.UUID,
	wantCashCounts, wantObservations int,
) {
	t.Helper()
	var closureRows, discrepancyRows int
	require.NoError(t, f.DB.QueryRow(
		`SELECT count(*) FROM shift_closures`).Scan(&closureRows))
	require.NoError(t, f.DB.QueryRow(
		`SELECT count(*) FROM shift_discrepancies`).Scan(&discrepancyRows))
	assert.Zero(t, closureRows, "no closure row may survive")
	assert.Zero(t, discrepancyRows, "no discrepancy row may survive")

	for _, event := range []string{
		shift.EventShiftClosedExact,
		shift.EventShiftClosedWithDiscrepancy,
	} {
		assert.Equal(t, 0, countAuditEvents(t, f.DB, event, f.Shift.ID),
			"no %s success event may survive", event)
	}

	assert.Equal(t, 0, countIdempotencyClaims(t, f.DB, f.Cashier.StaffID, requestID),
		"the rolled-back command must leave no idempotency claim or stored result")
	assert.Equal(t, shift.StateClosing, requireShiftState(t, f),
		"a failed close leaves the Shift CLOSING")

	// The frozen snapshot and its attempts survive the close's rollback.
	var reconRows int
	require.NoError(t, f.DB.QueryRow(
		`SELECT count(*) FROM shift_reconciliations`).Scan(&reconRows))
	assert.Equal(t, 1, reconRows, "the reconciliation the close used remains")
	var countRows, observationRows int
	require.NoError(t, f.DB.QueryRow(
		`SELECT count(*) FROM shift_cash_counts`).Scan(&countRows))
	require.NoError(t, f.DB.QueryRow(
		`SELECT count(*) FROM shift_qr_observations`).Scan(&observationRows))
	assert.Equal(t, wantCashCounts, countRows, "the start's cash attempts remain")
	assert.Equal(t, wantObservations, observationRows, "the start's QR attempts remain")
}

// TestReconciliationRollback proves every injected failure after a durable
// write rolls the whole command back (spec 4.2: "if any step fails the Shift
// remains OPEN and the initial count is not stored").
func TestReconciliationRollback(t *testing.T) {
	ctx := context.Background()

	t.Run("start rolls back after the snapshot insert", func(t *testing.T) {
		f := newShiftFixture(t)
		installReconciliationTrigger(t, f.DB, "fail_shift_cash_count_insert", `
			CREATE FUNCTION fail_shift_cash_count_insert() RETURNS trigger AS $$
			BEGIN
			    RAISE EXCEPTION 'forced shift cash count failure';
			END;
			$$ LANGUAGE plpgsql;
			CREATE TRIGGER fail_shift_cash_count_insert
			BEFORE INSERT ON shift_cash_counts
			FOR EACH ROW EXECUTE FUNCTION fail_shift_cash_count_insert();`, "shift_cash_counts")

		requestID := uuid.New()
		_, _, err := shift.NewStartReconciliationHandler(f.Runner).Handle(
			ctx, f.Cashier.actor(), f.startCommand(500_000, requestID))
		requireInjectedFailure(t, err)
		requireNothingPersisted(t, f, requestID)
	})

	t.Run("start rolls back after the initial count insert", func(t *testing.T) {
		f := newShiftFixture(t)
		installReconciliationTrigger(t, f.DB, "fail_shift_transition_closing", `
			CREATE FUNCTION fail_shift_transition_closing() RETURNS trigger AS $$
			BEGIN
			    IF NEW.state = 'CLOSING' AND OLD.state = 'OPEN' THEN
			        RAISE EXCEPTION 'forced shift closing transition failure';
			    END IF;
			    RETURN NEW;
			END;
			$$ LANGUAGE plpgsql;
			CREATE TRIGGER fail_shift_transition_closing
			BEFORE UPDATE ON sales_shifts
			FOR EACH ROW EXECUTE FUNCTION fail_shift_transition_closing();`, "sales_shifts")

		requestID := uuid.New()
		_, _, err := shift.NewStartReconciliationHandler(f.Runner).Handle(
			ctx, f.Cashier.actor(), f.startCommand(500_000, requestID))
		requireInjectedFailure(t, err)
		requireNothingPersisted(t, f, requestID)
	})

	t.Run("start rolls back after the initial-count audit event", func(t *testing.T) {
		f := newShiftFixture(t)
		// The start event is the executor's last business write, after the
		// handler's own initial-count event: failing it proves the count
		// event written inside the mutation body rolls back with it.
		installReconciliationTrigger(t, f.DB, "fail_shift_start_audit", `
			CREATE FUNCTION fail_shift_start_audit() RETURNS trigger AS $$
			BEGIN
			    IF NEW.event_type = 'SHIFT_RECONCILIATION_STARTED' THEN
			        RAISE EXCEPTION 'forced shift start audit failure';
			    END IF;
			    RETURN NEW;
			END;
			$$ LANGUAGE plpgsql;
			CREATE TRIGGER fail_shift_start_audit
			BEFORE INSERT ON audit_events
			FOR EACH ROW EXECUTE FUNCTION fail_shift_start_audit();`, "audit_events")

		requestID := uuid.New()
		_, _, err := shift.NewStartReconciliationHandler(f.Runner).Handle(
			ctx, f.Cashier.actor(), f.startCommand(500_000, requestID))
		requireInjectedFailure(t, err)
		requireNothingPersisted(t, f, requestID)
	})

	t.Run("start rolls back at result storage", func(t *testing.T) {
		f := newShiftFixture(t)
		installReconciliationTrigger(t, f.DB, "fail_shift_start_result_store", `
			CREATE FUNCTION fail_shift_start_result_store() RETURNS trigger AS $$
			BEGIN
			    IF NEW.action = 'shift.start_reconciliation' AND NEW.response_code <> 0 THEN
			        RAISE EXCEPTION 'forced shift start result store failure';
			    END IF;
			    RETURN NEW;
			END;
			$$ LANGUAGE plpgsql;
			CREATE TRIGGER fail_shift_start_result_store
			BEFORE UPDATE ON idempotency_keys
			FOR EACH ROW EXECUTE FUNCTION fail_shift_start_result_store();`, "idempotency_keys")

		requestID := uuid.New()
		_, _, err := shift.NewStartReconciliationHandler(f.Runner).Handle(
			ctx, f.Cashier.actor(), f.startCommand(500_000, requestID))
		requireInjectedFailure(t, err)
		requireNothingPersisted(t, f, requestID)
	})

	t.Run("close rolls back after the closure insert", func(t *testing.T) {
		f := newShiftFixture(t)
		cashID, qrID := shortageEvidence(t, f)
		installReconciliationTrigger(t, f.DB, "fail_shift_discrepancy_insert", `
			CREATE FUNCTION fail_shift_discrepancy_insert() RETURNS trigger AS $$
			BEGIN
			    RAISE EXCEPTION 'forced shift discrepancy failure';
			END;
			$$ LANGUAGE plpgsql;
			CREATE TRIGGER fail_shift_discrepancy_insert
			BEFORE INSERT ON shift_discrepancies
			FOR EACH ROW EXECUTE FUNCTION fail_shift_discrepancy_insert();`, "shift_discrepancies")

		requestID := uuid.New()
		_, _, err := shift.NewCloseShiftHandler(f.Runner).Handle(ctx, f.Cashier.actor(),
			f.closeCommand(requestID, cashID, qrID,
				[]shift.CloseDiscrepancyInput{reasonInput(shift.DimensionCash, shift.ReasonUnexplained)},
				f.Manager.LoginCode, f.Manager.Pin))
		requireInjectedFailure(t, err)
		requireCloseNothingPersisted(t, f, requestID, 2, 1)
	})

	t.Run("close rolls back after the discrepancy insert", func(t *testing.T) {
		f := newShiftFixture(t)
		cashID, qrID := shortageEvidence(t, f)
		installReconciliationTrigger(t, f.DB, "fail_shift_transition_closed", `
			CREATE FUNCTION fail_shift_transition_closed() RETURNS trigger AS $$
			BEGIN
			    IF NEW.state = 'CLOSED' AND OLD.state = 'CLOSING' THEN
			        RAISE EXCEPTION 'forced shift closed transition failure';
			    END IF;
			    RETURN NEW;
			END;
			$$ LANGUAGE plpgsql;
			CREATE TRIGGER fail_shift_transition_closed
			BEFORE UPDATE ON sales_shifts
			FOR EACH ROW EXECUTE FUNCTION fail_shift_transition_closed();`, "sales_shifts")

		requestID := uuid.New()
		_, _, err := shift.NewCloseShiftHandler(f.Runner).Handle(ctx, f.Cashier.actor(),
			f.closeCommand(requestID, cashID, qrID,
				[]shift.CloseDiscrepancyInput{reasonInput(shift.DimensionCash, shift.ReasonUnexplained)},
				f.Manager.LoginCode, f.Manager.Pin))
		requireInjectedFailure(t, err)
		requireCloseNothingPersisted(t, f, requestID, 2, 1)
	})

	t.Run("close rolls back after the closure audit event", func(t *testing.T) {
		f := newShiftFixture(t)
		startReconciliationExact(t, f)
		observation := appendQRObservation(t, f, 0, 0)
		installReconciliationTrigger(t, f.DB, "fail_shift_close_audit", `
			CREATE FUNCTION fail_shift_close_audit() RETURNS trigger AS $$
			BEGIN
			    IF NEW.event_type = 'SHIFT_CLOSED_EXACT' THEN
			        RAISE EXCEPTION 'forced shift close audit failure';
			    END IF;
			    RETURN NEW;
			END;
			$$ LANGUAGE plpgsql;
			CREATE TRIGGER fail_shift_close_audit
			BEFORE INSERT ON audit_events
			FOR EACH ROW EXECUTE FUNCTION fail_shift_close_audit();`, "audit_events")

		requestID := uuid.New()
		_, _, err := shift.NewCloseShiftHandler(f.Runner).Handle(ctx, f.Cashier.actor(),
			f.closeCommand(requestID, latestCashCountID(t, f), observation.ID,
				[]shift.CloseDiscrepancyInput{}, "", ""))
		requireInjectedFailure(t, err)
		requireCloseNothingPersisted(t, f, requestID, 1, 1)
	})

	t.Run("close rolls back at result storage", func(t *testing.T) {
		f := newShiftFixture(t)
		startReconciliationExact(t, f)
		observation := appendQRObservation(t, f, 0, 0)
		installReconciliationTrigger(t, f.DB, "fail_shift_close_result_store", `
			CREATE FUNCTION fail_shift_close_result_store() RETURNS trigger AS $$
			BEGIN
			    IF NEW.action = 'shift.close_exact' AND NEW.response_code <> 0 THEN
			        RAISE EXCEPTION 'forced shift close result store failure';
			    END IF;
			    RETURN NEW;
			END;
			$$ LANGUAGE plpgsql;
			CREATE TRIGGER fail_shift_close_result_store
			BEFORE UPDATE ON idempotency_keys
			FOR EACH ROW EXECUTE FUNCTION fail_shift_close_result_store();`, "idempotency_keys")

		requestID := uuid.New()
		_, _, err := shift.NewCloseShiftHandler(f.Runner).Handle(ctx, f.Cashier.actor(),
			f.closeCommand(requestID, latestCashCountID(t, f), observation.ID,
				[]shift.CloseDiscrepancyInput{}, "", ""))
		requireInjectedFailure(t, err)
		requireCloseNothingPersisted(t, f, requestID, 1, 1)
	})
}
