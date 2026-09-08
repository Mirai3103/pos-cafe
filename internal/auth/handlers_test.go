package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetStaffClaims(t *testing.T) {
	e := echo.New()

	t.Run("returns nil when context has no claims", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		claims := auth.GetStaff(c)
		assert.Nil(t, claims)
	})

	t.Run("returns nil when context value is wrong type", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.Set(auth.StaffContextKey, "invalid-claims")

		claims := auth.GetStaff(c)
		assert.Nil(t, claims)
	})

	t.Run("returns claims when properly set", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		expected := &auth.StaffClaims{
			StaffID:      uuid.New(),
			SessionID:    uuid.New(),
			DisplayName:  "Alice",
			LoginCode:    "ALICE",
			Roles:        []string{auth.RoleCashier},
			Capabilities: []string{"sales.operate"},
		}
		c.Set(auth.StaffContextKey, expected)

		claims := auth.GetStaff(c)
		require.NotNil(t, claims)
		assert.Equal(t, expected.StaffID, claims.StaffID)
		assert.Equal(t, expected.DisplayName, claims.DisplayName)
		assert.Equal(t, expected.LoginCode, claims.LoginCode)
		assert.Equal(t, expected.Roles, claims.Roles)
		assert.Equal(t, expected.Capabilities, claims.Capabilities)
	})
}

func TestConstructors(t *testing.T) {
	assert.NotNil(t, auth.NewSignInHandler(nil))
	assert.NotNil(t, auth.NewGetSessionHandler(nil))
	assert.NotNil(t, auth.NewLockSessionHandler(nil))
	assert.NotNil(t, auth.NewUnlockSessionHandler(nil))
	assert.NotNil(t, auth.NewSignOutHandler(nil))
	assert.NotNil(t, auth.NewDeclareWorkspaceHandler(nil))
	assert.NotNil(t, auth.NewRecordActivityHandler(nil))
	assert.NotNil(t, auth.NewListIdentitiesHandler(nil))
}
