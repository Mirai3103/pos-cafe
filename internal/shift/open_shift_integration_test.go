//go:build integration

package shift_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/shift"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func int64Ptr(v int64) *int64 { return &v }

// countAuditEvents counts business audit events of one type naming a Shift.
func countAuditEvents(t *testing.T, db *sql.DB, eventType string, shiftID uuid.UUID) int {
	t.Helper()
	var n int
	err := db.QueryRow(
		`SELECT count(*) FROM audit_events
		 WHERE event_type = $1 AND details->>'sales_shift_id' = $2`,
		eventType, shiftID.String(),
	).Scan(&n)
	require.NoError(t, err)
	return n
}

func TestOpenShiftRecordsFloatOpenerAndAudit(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	handler := shift.NewOpenShiftHandler(shift.NewRunner(db, q))
	ctx := context.Background()

	cashier := newTestActor(t, q, []string{"CASHIER"}, true)

	status, res, err := handler.Handle(ctx, cashier.actor(), shift.OpenShiftCommand{
		RequestID:       uuid.New(),
		OpeningFloatVND: int64Ptr(500000),
	})
	require.NoError(t, err)
	assert.Equal(t, 201, status)
	assert.Equal(t, shift.StateOpen, res.State)
	assert.Equal(t, int64(500000), res.OpeningFloatVND)
	assert.Equal(t, cashier.StaffID, res.Opener.ID)
	// CreateStaffIdentity stores upper(btrim(login_code)), so the returned
	// login code is the canonical uppercase form, not the generated string.
	assert.Equal(t, strings.ToUpper(cashier.LoginCode), res.Opener.LoginCode)
	assert.False(t, res.OpenedAt.IsZero())

	assert.Equal(t, 1, countAuditEvents(t, db, shift.EventSalesShiftOpened, res.ID))
}

func TestOpenShiftAcceptsZeroFloat(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	handler := shift.NewOpenShiftHandler(shift.NewRunner(db, q))
	ctx := context.Background()

	cashier := newTestActor(t, q, []string{"CASHIER"}, true)

	// A station may legitimately open with an empty fund.
	_, res, err := handler.Handle(ctx, cashier.actor(), shift.OpenShiftCommand{
		RequestID:       uuid.New(),
		OpeningFloatVND: int64Ptr(0),
	})
	require.NoError(t, err)
	assert.Equal(t, int64(0), res.OpeningFloatVND)
}

func TestOpenShiftRejectsSecondOpenShift(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	handler := shift.NewOpenShiftHandler(shift.NewRunner(db, q))
	ctx := context.Background()

	first := newTestActor(t, q, []string{"CASHIER"}, true)
	second := newTestActor(t, q, []string{"MANAGER"}, true)

	_, _, err := handler.Handle(ctx, first.actor(), shift.OpenShiftCommand{
		RequestID:       uuid.New(),
		OpeningFloatVND: int64Ptr(500000),
	})
	require.NoError(t, err)

	// The invariant is system-wide, not per identity or per station.
	_, _, err = handler.Handle(ctx, second.actor(), shift.OpenShiftCommand{
		RequestID:       uuid.New(),
		OpeningFloatVND: int64Ptr(700000),
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, shift.ErrShiftAlreadyOpen)
}

func TestOpenShiftRejectsInvalidFloat(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	handler := shift.NewOpenShiftHandler(shift.NewRunner(db, q))
	ctx := context.Background()

	cashier := newTestActor(t, q, []string{"CASHIER"}, true)

	for _, amount := range []int64{-1, shift.MaxAmountVND + 1} {
		_, _, err := handler.Handle(ctx, cashier.actor(), shift.OpenShiftCommand{
			RequestID:       uuid.New(),
			OpeningFloatVND: int64Ptr(amount),
		})
		require.Error(t, err, "amount %d", amount)
	}
}

func TestOpenShiftDeniesBarista(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	handler := shift.NewOpenShiftHandler(shift.NewRunner(db, q))
	ctx := context.Background()

	barista := newTestActor(t, q, []string{"BARISTA"}, true)

	_, _, err := handler.Handle(ctx, barista.actor(), shift.OpenShiftCommand{
		RequestID:       uuid.New(),
		OpeningFloatVND: int64Ptr(500000),
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, shift.ErrForbidden)
}

func TestOpenShiftIsIdempotent(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	handler := shift.NewOpenShiftHandler(shift.NewRunner(db, q))
	ctx := context.Background()

	cashier := newTestActor(t, q, []string{"CASHIER"}, true)
	requestID := uuid.New()
	cmd := shift.OpenShiftCommand{RequestID: requestID, OpeningFloatVND: int64Ptr(500000)}

	_, first, err := handler.Handle(ctx, cashier.actor(), cmd)
	require.NoError(t, err)

	status, replay, err := handler.Handle(ctx, cashier.actor(), cmd)
	require.NoError(t, err)
	assert.Equal(t, 201, status)
	assert.Equal(t, first.ID, replay.ID)
	assert.Equal(t, first.OpeningFloatVND, replay.OpeningFloatVND)

	// A replay writes no second business event.
	assert.Equal(t, 1, countAuditEvents(t, db, shift.EventSalesShiftOpened, first.ID))

	// The same request_id with a different float is a conflict, not a replay.
	_, _, err = handler.Handle(ctx, cashier.actor(), shift.OpenShiftCommand{
		RequestID:       requestID,
		OpeningFloatVND: int64Ptr(700000),
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, shift.ErrRequestConflict)
}

func TestOpenShiftConcurrentOpensYieldExactlyOne(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	handler := shift.NewOpenShiftHandler(shift.NewRunner(db, q))
	ctx := context.Background()

	const goroutines = 4
	actors := make([]testActor, goroutines)
	for i := range actors {
		actors[i] = newTestActor(t, q, []string{"CASHIER"}, true)
	}

	var succeeded atomic.Int32
	start := make(chan struct{})
	errs := make(chan error, goroutines)
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(a testActor) {
			defer wg.Done()
			<-start
			_, _, execErr := handler.Handle(ctx, a.actor(), shift.OpenShiftCommand{
				RequestID:       uuid.New(),
				OpeningFloatVND: int64Ptr(500000),
			})
			if execErr == nil {
				succeeded.Add(1)
			}
			errs <- execErr
		}(actors[i])
	}
	close(start)
	wg.Wait()
	close(errs)

	assert.Equal(t, int32(1), succeeded.Load(), "exactly one concurrent open must succeed")
	for execErr := range errs {
		if execErr != nil {
			assert.ErrorIs(t, execErr, shift.ErrShiftAlreadyOpen,
				"a losing open must report the domain conflict, never a generic failure")
		}
	}

	var openCount int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM sales_shifts WHERE state = 'OPEN'`).Scan(&openCount))
	assert.Equal(t, 1, openCount)
}

func TestOpenShiftAuditDetailsCarryNoSecrets(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	handler := shift.NewOpenShiftHandler(shift.NewRunner(db, q))
	ctx := context.Background()

	cashier := newTestActor(t, q, []string{"CASHIER"}, true)
	_, res, err := handler.Handle(ctx, cashier.actor(), shift.OpenShiftCommand{
		RequestID:       uuid.New(),
		OpeningFloatVND: int64Ptr(500000),
	})
	require.NoError(t, err)

	var raw []byte
	require.NoError(t, db.QueryRow(
		`SELECT details FROM audit_events
		 WHERE event_type = $1 AND details->>'sales_shift_id' = $2`,
		shift.EventSalesShiftOpened, res.ID.String(),
	).Scan(&raw))

	var details map[string]any
	require.NoError(t, json.Unmarshal(raw, &details))
	assert.Equal(t, []string{"opening_float_vnd", "sales_shift_id"}, sortedKeys(details))
}

// sortedKeys returns a map's keys in ascending order for stable assertions.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}
