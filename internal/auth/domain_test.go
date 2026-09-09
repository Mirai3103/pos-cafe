package auth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionStates(t *testing.T) {
	assert.Equal(t, "active", SessionStateActive)
	assert.Equal(t, "authenticated", SessionStateAuthenticated)
}

func TestValidatePinFormat(t *testing.T) {
	assert.NoError(t, ValidatePinFormat("1234"))
	assert.NoError(t, ValidatePinFormat("12345678"))
	assert.Error(t, ValidatePinFormat("123"))       // too short
	assert.Error(t, ValidatePinFormat("123456789")) // too long
	assert.Error(t, ValidatePinFormat("123a"))      // non-digit
	assert.Error(t, ValidatePinFormat(""))          // empty
}

func TestHashAndVerifyPin(t *testing.T) {
	pin := "123456"
	hash, err := HashPin(pin)
	require.NoError(t, err)
	assert.NotEmpty(t, hash)

	assert.True(t, VerifyPin(hash, "123456"))
	assert.False(t, VerifyPin(hash, "654321"))
	assert.False(t, VerifyPin("", "123456"))
}

func TestDeriveCapabilities(t *testing.T) {
	// Cashier
	cashierCaps := DeriveCapabilities([]string{RoleCashier})
	assert.Contains(t, cashierCaps, "sales.operate")
	assert.Contains(t, cashierCaps, "catalog.view_prices")
	assert.NotContains(t, cashierCaps, "preparation.operate")
	assert.NotContains(t, cashierCaps, "staff.administer")

	// Barista
	baristaCaps := DeriveCapabilities([]string{RoleBarista})
	assert.Contains(t, baristaCaps, "preparation.operate")
	assert.NotContains(t, baristaCaps, "sales.operate")

	// Manager
	managerCaps := DeriveCapabilities([]string{RoleManager})
	assert.Contains(t, managerCaps, "staff.administer")
	assert.Contains(t, managerCaps, "sales.operate")
	assert.Contains(t, managerCaps, "preparation.operate")

	// Combined multi-role deduplication
	combined := DeriveCapabilities([]string{RoleCashier, RoleBarista})
	assert.Contains(t, combined, "sales.operate")
	assert.Contains(t, combined, "preparation.operate")
}

func TestTokenGenerationAndHashing(t *testing.T) {
	token, hash, err := GenerateSessionToken()
	require.NoError(t, err)
	assert.NotEmpty(t, token)
	assert.Len(t, hash, 64) // SHA-256 hex string is 64 chars

	assert.Equal(t, hash, HashToken(token))
}

func TestInactivityTimeout(t *testing.T) {
	assert.Equal(t, 5*time.Minute, GetInactivityTimeout(WorkspaceCashier))
	assert.Equal(t, 5*time.Minute, GetInactivityTimeout(WorkspaceManager))
	assert.Equal(t, 15*time.Minute, GetInactivityTimeout(WorkspacePreparation))
	assert.Equal(t, 5*time.Minute, GetInactivityTimeout(""))
}
