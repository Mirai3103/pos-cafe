//go:build integration

package auth_test

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/database"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openApprovalTestDB(t *testing.T) (*sql.DB, *sqlc.Queries) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	require.Contains(t, url, "_test")
	db, err := database.Open(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db, sqlc.New(db)
}

// newApprover creates an identity with a known PIN and returns its login code.
func newApprover(t *testing.T, q *sqlc.Queries, roles []string, enabled bool, pin string) (uuid.UUID, string) {
	t.Helper()
	ctx := context.Background()

	loginCode := "A" + strings.ReplaceAll(uuid.NewString(), "-", "")[:23]
	hash, err := auth.HashPin(pin)
	require.NoError(t, err)

	row, err := q.CreateStaffIdentity(ctx, sqlc.CreateStaffIdentityParams{
		DisplayName: "Approval Test " + loginCode,
		Btrim:       loginCode,
		PinHash:     hash,
		Enabled:     enabled,
	})
	require.NoError(t, err)

	for _, role := range roles {
		require.NoError(t, q.AddStaffRole(ctx, sqlc.AddStaffRoleParams{
			StaffIdentityID: row.ID, Role: role,
		}))
	}
	return row.ID, loginCode
}

func TestVerifyManagerApprovalGrantsForEnabledManager(t *testing.T) {
	db, q := openApprovalTestDB(t)
	ctx := context.Background()
	id, loginCode := newApprover(t, q, []string{auth.RoleManager}, true, "8642")

	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback() //nolint:errcheck

	// Lower-cased and padded on purpose: normalization must find the identity.
	approver, err := auth.VerifyManagerApproval(ctx, q.WithTx(tx),
		"  "+strings.ToLower(loginCode)+"  ", "8642", "sales_shift.operate")
	require.NoError(t, err)
	assert.Equal(t, id, approver.ID)
	// CreateStaffIdentity stores upper(btrim(...)), so the canonical login code
	// is the upper-cased form of what the helper generated.
	assert.Equal(t, strings.ToUpper(loginCode), approver.LoginCode)
	assert.NotEmpty(t, approver.DisplayName)
}

func TestVerifyManagerApprovalDenials(t *testing.T) {
	db, q := openApprovalTestDB(t)
	ctx := context.Background()

	_, managerCode := newApprover(t, q, []string{auth.RoleManager}, true, "8642")
	_, disabledCode := newApprover(t, q, []string{auth.RoleManager}, false, "8642")
	_, cashierCode := newApprover(t, q, []string{auth.RoleCashier}, true, "8642")

	cases := []struct {
		name       string
		loginCode  string
		pin        string
		capability string
		reason     string
	}{
		{"wrong pin", managerCode, "0000", "sales_shift.operate", auth.ApprovalDenialInvalidPin},
		{"unknown login code", "ZZUNKNOWN", "8642", "sales_shift.operate", auth.ApprovalDenialInvalidPin},
		{"disabled manager", disabledCode, "8642", "sales_shift.operate", auth.ApprovalDenialIdentityDisabled},
		{"cashier approver", cashierCode, "8642", "sales_shift.operate", auth.ApprovalDenialManagerRoleRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := db.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer tx.Rollback() //nolint:errcheck

			_, err = auth.VerifyManagerApproval(ctx, q.WithTx(tx), tc.loginCode, tc.pin, tc.capability)
			require.Error(t, err)
			assert.ErrorIs(t, err, auth.ErrManagerApprovalDenied)
			assert.Contains(t, err.Error(), tc.reason)
		})
	}
}

func TestVerifyManagerApprovalLocksApproverRow(t *testing.T) {
	db, q := openApprovalTestDB(t)
	ctx := context.Background()
	id, loginCode := newApprover(t, q, []string{auth.RoleManager}, true, "8642")

	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback() //nolint:errcheck

	_, err = auth.VerifyManagerApproval(ctx, q.WithTx(tx), loginCode, "8642", "sales_shift.operate")
	require.NoError(t, err)

	// A concurrent disablement must block while the approval transaction holds
	// the row lock, proving verification and use cannot be interleaved.
	blocked := make(chan error, 1)
	go func() {
		_, execErr := db.Exec(`UPDATE staff_identities SET enabled = false WHERE id = $1`, id)
		blocked <- execErr
	}()

	select {
	case <-blocked:
		t.Fatal("concurrent disablement completed while the approver row was locked")
	case <-time.After(300 * time.Millisecond):
		// Still blocked, which is the expected outcome.
	}

	require.NoError(t, tx.Rollback())
	require.NoError(t, <-blocked)
}
