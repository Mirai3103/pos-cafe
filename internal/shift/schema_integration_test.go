//go:build integration

package shift_test

import (
	"database/sql"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// insertShiftRow opens a Sales Shift with raw SQL so constraint behavior is
// tested without going through the slice.
func insertShiftRow(t *testing.T, db *sql.DB, openerID uuid.UUID, floatVND int64) (uuid.UUID, error) {
	t.Helper()
	var id uuid.UUID
	err := db.QueryRow(
		`INSERT INTO sales_shifts (opened_by_staff_identity_id, opening_float_vnd)
		 VALUES ($1, $2) RETURNING id`,
		openerID, floatVND,
	).Scan(&id)
	return id, err
}

func TestSchemaRejectsSecondOpenShift(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	actor := newTestActor(t, q, []string{"CASHIER"}, true)

	_, err := insertShiftRow(t, db, actor.StaffID, 500000)
	require.NoError(t, err)

	_, err = insertShiftRow(t, db, actor.StaffID, 700000)
	require.Error(t, err, "a second OPEN Sales Shift must violate the partial unique index")
	assert.Contains(t, err.Error(), "sales_shift_only_one_open_unique")
}

func TestSchemaAllowsOpenAfterClose(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	actor := newTestActor(t, q, []string{"CASHIER"}, true)

	first, err := insertShiftRow(t, db, actor.StaffID, 500000)
	require.NoError(t, err)

	// CLOSED exists in the check constraint although Phase 4 ships no close
	// command, so Phase 5 adds one without a state-domain migration.
	_, err = db.Exec(`UPDATE sales_shifts SET state = 'CLOSED' WHERE id = $1`, first)
	require.NoError(t, err)

	_, err = insertShiftRow(t, db, actor.StaffID, 700000)
	assert.NoError(t, err, "the partial index must only constrain OPEN rows")
}

func TestSchemaRejectsInvalidShiftValues(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	actor := newTestActor(t, q, []string{"CASHIER"}, true)

	_, err := insertShiftRow(t, db, actor.StaffID, -1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sales_shift_opening_float_vnd_valid")

	_, err = insertShiftRow(t, db, actor.StaffID, 2147483648)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sales_shift_opening_float_vnd_valid")

	// Zero is valid: a station may legitimately open with an empty fund.
	_, err = insertShiftRow(t, db, actor.StaffID, 0)
	assert.NoError(t, err)
}

func TestSchemaRejectsInvalidCashMovements(t *testing.T) {
	db, q := openShiftTestDB(t)
	truncateShiftTables(t, db)
	actor := newTestActor(t, q, []string{"CASHIER"}, true)
	shiftID, err := insertShiftRow(t, db, actor.StaffID, 500000)
	require.NoError(t, err)

	insert := func(method string, amount int64, reason string, note any) error {
		_, execErr := db.Exec(
			`INSERT INTO cash_movements (
				sales_shift_id, method, amount_vnd, reason, note,
				initiated_by_staff_identity_id, initiated_staff_access_session_id,
				approved_by_staff_identity_id)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			shiftID, method, amount, reason, note,
			actor.StaffID, actor.SessionID, actor.StaffID,
		)
		return execErr
	}

	cases := []struct {
		name       string
		method     string
		amount     int64
		reason     string
		note       any
		constraint string
	}{
		{"unknown method", "CASH_DROP", 1000, "SAFE_DROP", nil, "cash_movement_method_valid"},
		{"zero amount", "PAY_IN", 0, "ADD_CHANGE_FUND", nil, "cash_movement_amount_vnd_valid"},
		{"negative amount", "PAY_OUT", -1000, "SAFE_DROP", nil, "cash_movement_amount_vnd_valid"},
		{"over-bound amount", "PAY_IN", 2147483648, "ADD_CHANGE_FUND", nil, "cash_movement_amount_vnd_valid"},
		{"unknown reason", "PAY_IN", 1000, "PETTY_CASH", nil, "cash_movement_reason_valid"},
		{"OTHER without note", "PAY_OUT", 1000, "OTHER", nil, "cash_movement_note_valid"},
		{"untrimmed note", "PAY_OUT", 1000, "OTHER", "  padded  ", "cash_movement_note_valid"},
		{"empty note", "PAY_OUT", 1000, "OTHER", "", "cash_movement_note_valid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			insertErr := insert(tc.method, tc.amount, tc.reason, tc.note)
			require.Error(t, insertErr)
			assert.Contains(t, insertErr.Error(), tc.constraint)
		})
	}

	assert.NoError(t, insert("PAY_OUT", 50000, "SAFE_DROP", nil))
	assert.NoError(t, insert("PAY_OUT", 50000, "OTHER", "mua da cho quay pha che"))
}
