//go:build integration

package auth_test

import (
	"net/http"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// updateFixture bootstraps manager QL01/1234, signs in, and creates barista BA01/5555.
func updateFixture(t *testing.T, e *echo.Echo) (map[string]string, uuid.UUID, uuid.UUID) {
	t.Helper()
	boot := doJSONRequest(e, http.MethodPost, "/api/v1/auth/bootstrap", auth.BootstrapManagerRequest{
		DisplayName: "Quản Lý", LoginCode: "QL01", Pin: "1234",
	}, nil)
	require.Equal(t, http.StatusCreated, boot.Code)

	signIn := doJSONRequest(e, http.MethodPost, "/api/v1/auth/sign-in", auth.SignInRequest{LoginCode: "QL01", Pin: "1234"}, nil)
	require.Equal(t, http.StatusOK, signIn.Code)
	headers := map[string]string{"Authorization": "Bearer " + parseResponse[auth.SignInResponse](t, signIn).Data.Token}

	me := doJSONRequest(e, http.MethodGet, "/api/v1/staff/me", nil, headers)
	require.Equal(t, http.StatusOK, me.Code)
	managerID := parseResponse[auth.StaffProfileResponse](t, me).Data.ID

	created := doJSONRequest(e, http.MethodPost, "/api/v1/staff", auth.CreateStaffRequest{
		RequestID: uuid.New(), DisplayName: "Pha Chế", LoginCode: "BA01", Enabled: true,
		Roles: []string{auth.RoleBarista}, Pin: "5555", ManagerPin: "1234",
	}, headers)
	require.Equal(t, http.StatusCreated, created.Code)
	baristaID := parseResponse[auth.StaffDetailResponse](t, created).Data.ID

	return headers, managerID, baristaID
}

func updateBody(name, code string, roles []string, pin string) auth.UpdateStaffRequest {
	return auth.UpdateStaffRequest{RequestID: uuid.New(), DisplayName: name, LoginCode: code, Roles: roles, ManagerPin: pin}
}

func TestStaffUpdate(t *testing.T) {
	t.Run("updates name, upper-cased code, and roles; new code signs in", func(t *testing.T) {
		e, cleanup := setupTestApp(t)
		defer cleanup()
		headers, _, baristaID := updateFixture(t, e)

		rec := doJSONRequest(e, http.MethodPatch, "/api/v1/staff/"+baristaID.String(),
			updateBody("  Minh Mới ", " minh ", []string{auth.RoleCashier, auth.RoleBarista}, "1234"), headers)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		got := parseResponse[auth.StaffDetailResponse](t, rec).Data
		assert.Equal(t, "Minh Mới", got.DisplayName)
		assert.Equal(t, "MINH", got.LoginCode)
		assert.ElementsMatch(t, []string{auth.RoleCashier, auth.RoleBarista}, got.Roles)

		list := parseResponse[[]auth.StaffDetailResponse](t, doJSONRequest(e, http.MethodGet, "/api/v1/staff", nil, headers)).Data
		for _, s := range list {
			if s.ID == baristaID {
				assert.ElementsMatch(t, []string{auth.RoleBarista, auth.RoleCashier}, s.Roles)
			}
		}

		signIn := doJSONRequest(e, http.MethodPost, "/api/v1/auth/sign-in", auth.SignInRequest{LoginCode: "MINH", Pin: "5555"}, nil)
		assert.Equal(t, http.StatusOK, signIn.Code)
	})

	t.Run("duplicate login code is a conflict", func(t *testing.T) {
		e, cleanup := setupTestApp(t)
		defer cleanup()
		headers, _, baristaID := updateFixture(t, e)

		rec := doJSONRequest(e, http.MethodPatch, "/api/v1/staff/"+baristaID.String(),
			updateBody("Pha Chế", "ql01", []string{auth.RoleBarista}, "1234"), headers)
		assert.Equal(t, http.StatusConflict, rec.Code)
		assert.Equal(t, "CONFLICT", parseResponse[any](t, rec).Error.Code)
	})

	t.Run("removing MANAGER from the last enabled manager is refused", func(t *testing.T) {
		e, cleanup := setupTestApp(t)
		defer cleanup()
		headers, managerID, _ := updateFixture(t, e)

		rec := doJSONRequest(e, http.MethodPatch, "/api/v1/staff/"+managerID.String(),
			updateBody("Quản Lý", "QL01", []string{auth.RoleCashier}, "1234"), headers)
		assert.Equal(t, http.StatusConflict, rec.Code)
		assert.Equal(t, "FINAL_ENABLED_MANAGER_REQUIRED", parseResponse[any](t, rec).Error.Code)
	})

	t.Run("wrong manager PIN is forbidden", func(t *testing.T) {
		e, cleanup := setupTestApp(t)
		defer cleanup()
		headers, _, baristaID := updateFixture(t, e)

		rec := doJSONRequest(e, http.MethodPatch, "/api/v1/staff/"+baristaID.String(),
			updateBody("Pha Chế", "BA01", []string{auth.RoleBarista}, "9999"), headers)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("unknown staff is not found", func(t *testing.T) {
		e, cleanup := setupTestApp(t)
		defer cleanup()
		headers, _, _ := updateFixture(t, e)

		rec := doJSONRequest(e, http.MethodPatch, "/api/v1/staff/"+uuid.New().String(),
			updateBody("Ai Đó", "AIDO", []string{auth.RoleBarista}, "1234"), headers)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("same request_id replays the first result", func(t *testing.T) {
		e, cleanup := setupTestApp(t)
		defer cleanup()
		headers, _, baristaID := updateFixture(t, e)

		body := updateBody("Pha Chế 2", "BA02", []string{auth.RoleBarista}, "1234")
		first := doJSONRequest(e, http.MethodPatch, "/api/v1/staff/"+baristaID.String(), body, headers)
		second := doJSONRequest(e, http.MethodPatch, "/api/v1/staff/"+baristaID.String(), body, headers)
		require.Equal(t, http.StatusOK, first.Code)
		assert.Equal(t, http.StatusOK, second.Code)
		assert.JSONEq(t, first.Body.String(), second.Body.String())
	})
}
