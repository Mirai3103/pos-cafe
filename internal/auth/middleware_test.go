package auth_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockMiddlewareStore struct {
	sqlc.Querier
	getSessionByTokenHashFunc func(ctx context.Context, tokenHash string) (sqlc.GetSessionByTokenHashRow, error)
	updateSessionStateFunc    func(ctx context.Context, arg sqlc.UpdateSessionStateParams) error
	getStaffRolesFunc         func(ctx context.Context, staffIdentityID uuid.UUID) ([]string, error)
}

func (m *mockMiddlewareStore) GetSessionByTokenHash(ctx context.Context, tokenHash string) (sqlc.GetSessionByTokenHashRow, error) {
	if m.getSessionByTokenHashFunc != nil {
		return m.getSessionByTokenHashFunc(ctx, tokenHash)
	}
	return sqlc.GetSessionByTokenHashRow{}, sql.ErrNoRows
}

func (m *mockMiddlewareStore) UpdateSessionState(ctx context.Context, arg sqlc.UpdateSessionStateParams) error {
	if m.updateSessionStateFunc != nil {
		return m.updateSessionStateFunc(ctx, arg)
	}
	return nil
}

func (m *mockMiddlewareStore) GetStaffRoles(ctx context.Context, staffIdentityID uuid.UUID) ([]string, error) {
	if m.getStaffRolesFunc != nil {
		return m.getStaffRolesFunc(ctx, staffIdentityID)
	}
	return nil, nil
}

func makeSessionRow(staffID, sessionID uuid.UUID, workspace *string) sqlc.GetSessionByTokenHashRow {
	ws := sql.NullString{}
	if workspace != nil {
		ws = sql.NullString{String: *workspace, Valid: true}
	}
	return sqlc.GetSessionByTokenHashRow{
		SessionID:           sessionID,
		TokenHash:           auth.HashToken("test-token"),
		StaffIdentityID:     staffID,
		SessionState:        "active",
		ActiveWorkspace:     ws,
		LastAuthenticatedAt: time.Now().UTC().Add(-10 * time.Minute),
		LastHumanActivityAt: time.Now().UTC().Add(-1 * time.Minute),
		ExpiresAt:           time.Now().UTC().Add(12 * time.Hour),
		RevokedAt:           sql.NullTime{Valid: false},
		DisplayName:         "Tester",
		LoginCode:           "TEST01",
		PinHash:             "some-pin-hash",
		IdentityEnabled:     true,
	}
}

func TestRequireAuth(t *testing.T) {
	e := setupEcho()
	staffID := uuid.New()
	sessionID := uuid.New()
	testToken := "test-token"

	t.Run("missing authorization header and cookie returns 401 Unauthorized", func(t *testing.T) {
		mw := auth.NewMiddleware(&mockMiddlewareStore{})
		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		handler := mw.RequireAuth()(func(c echo.Context) error {
			return response.OK(c, "success")
		})

		err := handler(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)

		var resp response.APIResponse
		err = json.Unmarshal(rec.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.False(t, resp.Success)
		assert.Equal(t, "UNAUTHORIZED", resp.Error.Code)
		assert.Contains(t, resp.Error.Message, "vui lòng đăng nhập")
	})

	t.Run("invalid token returns 401 Unauthorized", func(t *testing.T) {
		mock := &mockMiddlewareStore{
			getSessionByTokenHashFunc: func(_ context.Context, _ string) (sqlc.GetSessionByTokenHashRow, error) {
				return sqlc.GetSessionByTokenHashRow{}, sql.ErrNoRows
			},
		}
		mw := auth.NewMiddleware(mock)

		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set(echo.HeaderAuthorization, "Bearer invalid-token")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		handler := mw.RequireAuth()(func(c echo.Context) error {
			return response.OK(c, "success")
		})

		err := handler(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)

		var resp response.APIResponse
		err = json.Unmarshal(rec.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.False(t, resp.Success)
		assert.Equal(t, "UNAUTHORIZED", resp.Error.Code)
		assert.Contains(t, resp.Error.Message, "phiên đăng nhập không hợp lệ")
	})

	t.Run("session lookup database failure returns 500 Internal Server Error", func(t *testing.T) {
		mock := &mockMiddlewareStore{
			getSessionByTokenHashFunc: func(_ context.Context, _ string) (sqlc.GetSessionByTokenHashRow, error) {
				return sqlc.GetSessionByTokenHashRow{}, errors.New("db disconnect")
			},
		}
		mw := auth.NewMiddleware(mock)

		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set(echo.HeaderAuthorization, "Bearer "+testToken)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		handler := mw.RequireAuth()(func(c echo.Context) error {
			return response.OK(c, "success")
		})

		err := handler(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)

		var resp response.APIResponse
		err = json.Unmarshal(rec.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.False(t, resp.Success)
		assert.Equal(t, "INTERNAL_ERROR", resp.Error.Code)
	})

	t.Run("expired session returns 401 Unauthorized", func(t *testing.T) {
		row := makeSessionRow(staffID, sessionID, nil)
		row.ExpiresAt = time.Now().UTC().Add(-10 * time.Minute)

		mock := &mockMiddlewareStore{
			getSessionByTokenHashFunc: func(_ context.Context, _ string) (sqlc.GetSessionByTokenHashRow, error) {
				return row, nil
			},
		}
		mw := auth.NewMiddleware(mock)

		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set(echo.HeaderAuthorization, "Bearer "+testToken)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		handler := mw.RequireAuth()(func(c echo.Context) error {
			return response.OK(c, "success")
		})

		err := handler(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)

		var resp response.APIResponse
		err = json.Unmarshal(rec.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.False(t, resp.Success)
		assert.Equal(t, "UNAUTHORIZED", resp.Error.Code)
		assert.Contains(t, resp.Error.Message, "phiên đăng nhập đã hết hạn hoặc bị thu hồi")
	})

	t.Run("revoked session returns 401 Unauthorized", func(t *testing.T) {
		row := makeSessionRow(staffID, sessionID, nil)
		row.RevokedAt = sql.NullTime{Time: time.Now().UTC(), Valid: true}

		mock := &mockMiddlewareStore{
			getSessionByTokenHashFunc: func(_ context.Context, _ string) (sqlc.GetSessionByTokenHashRow, error) {
				return row, nil
			},
		}
		mw := auth.NewMiddleware(mock)

		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set(echo.HeaderAuthorization, "Bearer "+testToken)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		handler := mw.RequireAuth()(func(c echo.Context) error {
			return response.OK(c, "success")
		})

		err := handler(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)

		var resp response.APIResponse
		err = json.Unmarshal(rec.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.False(t, resp.Success)
		assert.Equal(t, "UNAUTHORIZED", resp.Error.Code)
		assert.Contains(t, resp.Error.Message, "phiên đăng nhập đã hết hạn hoặc bị thu hồi")
	})

	t.Run("disabled staff identity returns 401 Unauthorized", func(t *testing.T) {
		row := makeSessionRow(staffID, sessionID, nil)
		row.IdentityEnabled = false

		mock := &mockMiddlewareStore{
			getSessionByTokenHashFunc: func(_ context.Context, _ string) (sqlc.GetSessionByTokenHashRow, error) {
				return row, nil
			},
		}
		mw := auth.NewMiddleware(mock)

		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set(echo.HeaderAuthorization, "Bearer "+testToken)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		handler := mw.RequireAuth()(func(c echo.Context) error {
			return response.OK(c, "success")
		})

		err := handler(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)

		var resp response.APIResponse
		err = json.Unmarshal(rec.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.False(t, resp.Success)
		assert.Equal(t, "UNAUTHORIZED", resp.Error.Code)
		assert.Contains(t, resp.Error.Message, "phiên đăng nhập đã hết hạn hoặc bị thu hồi")
	})

	t.Run("already locked session returns 403 Forbidden", func(t *testing.T) {
		row := makeSessionRow(staffID, sessionID, nil)
		row.SessionState = auth.SessionStateLocked

		mock := &mockMiddlewareStore{
			getSessionByTokenHashFunc: func(_ context.Context, _ string) (sqlc.GetSessionByTokenHashRow, error) {
				return row, nil
			},
		}
		mw := auth.NewMiddleware(mock)

		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set(echo.HeaderAuthorization, "Bearer "+testToken)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		handler := mw.RequireAuth()(func(c echo.Context) error {
			return response.OK(c, "success")
		})

		err := handler(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusForbidden, rec.Code)

		var resp response.APIResponse
		err = json.Unmarshal(rec.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.False(t, resp.Success)
		assert.Equal(t, "FORBIDDEN", resp.Error.Code)
		assert.Contains(t, resp.Error.Message, "phiên đang bị khóa, vui lòng mở khóa")
	})

	t.Run("inactivity auto-lock transition updates state to locked and returns 403 Forbidden", func(t *testing.T) {
		row := makeSessionRow(staffID, sessionID, nil)
		row.SessionState = "active"
		row.LastHumanActivityAt = time.Now().UTC().Add(-6 * time.Minute) // default timeout is 5 min

		var updatedParams sqlc.UpdateSessionStateParams
		mock := &mockMiddlewareStore{
			getSessionByTokenHashFunc: func(_ context.Context, _ string) (sqlc.GetSessionByTokenHashRow, error) {
				return row, nil
			},
			updateSessionStateFunc: func(_ context.Context, arg sqlc.UpdateSessionStateParams) error {
				updatedParams = arg
				return nil
			},
		}
		mw := auth.NewMiddleware(mock)

		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set(echo.HeaderAuthorization, "Bearer "+testToken)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		handler := mw.RequireAuth()(func(c echo.Context) error {
			return response.OK(c, "success")
		})

		err := handler(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusForbidden, rec.Code)

		assert.Equal(t, sessionID, updatedParams.ID)
		assert.Equal(t, auth.SessionStateLocked, updatedParams.State)

		var resp response.APIResponse
		err = json.Unmarshal(rec.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, "FORBIDDEN", resp.Error.Code)
		assert.Contains(t, resp.Error.Message, "phiên đang bị khóa, vui lòng mở khóa")
	})

	t.Run("preparation workspace respects 15m inactivity timeout", func(t *testing.T) {
		ws := auth.WorkspacePreparation
		row := makeSessionRow(staffID, sessionID, &ws)
		row.LastHumanActivityAt = time.Now().UTC().Add(-10 * time.Minute) // < 15 min, still active!

		mock := &mockMiddlewareStore{
			getSessionByTokenHashFunc: func(_ context.Context, _ string) (sqlc.GetSessionByTokenHashRow, error) {
				return row, nil
			},
			getStaffRolesFunc: func(_ context.Context, _ uuid.UUID) ([]string, error) {
				return []string{auth.RoleBarista}, nil
			},
		}
		mw := auth.NewMiddleware(mock)

		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set(echo.HeaderAuthorization, "Bearer "+testToken)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		handler := mw.RequireAuth()(func(c echo.Context) error {
			return response.OK(c, "active")
		})

		err := handler(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)

		// Now test >= 15 min -> auto lock triggers
		row.LastHumanActivityAt = time.Now().UTC().Add(-16 * time.Minute)
		rec2 := httptest.NewRecorder()
		c2 := e.NewContext(req, rec2)

		err = handler(c2)
		require.NoError(t, err)
		assert.Equal(t, http.StatusForbidden, rec2.Code)
	})

	t.Run("role check: allowed single role passes", func(t *testing.T) {
		row := makeSessionRow(staffID, sessionID, nil)
		mock := &mockMiddlewareStore{
			getSessionByTokenHashFunc: func(_ context.Context, _ string) (sqlc.GetSessionByTokenHashRow, error) {
				return row, nil
			},
			getStaffRolesFunc: func(_ context.Context, _ uuid.UUID) ([]string, error) {
				return []string{auth.RoleManager}, nil
			},
		}
		mw := auth.NewMiddleware(mock)

		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set(echo.HeaderAuthorization, "Bearer "+testToken)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		handler := mw.RequireAuth(auth.RoleManager)(func(c echo.Context) error {
			return response.OK(c, "allowed")
		})

		err := handler(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("role check: multiple allowed roles passes when staff has one", func(t *testing.T) {
		row := makeSessionRow(staffID, sessionID, nil)
		mock := &mockMiddlewareStore{
			getSessionByTokenHashFunc: func(_ context.Context, _ string) (sqlc.GetSessionByTokenHashRow, error) {
				return row, nil
			},
			getStaffRolesFunc: func(_ context.Context, _ uuid.UUID) ([]string, error) {
				return []string{auth.RoleBarista}, nil
			},
		}
		mw := auth.NewMiddleware(mock)

		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set(echo.HeaderAuthorization, "Bearer "+testToken)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		handler := mw.RequireAuth(auth.RoleManager, auth.RoleBarista)(func(c echo.Context) error {
			return response.OK(c, "allowed")
		})

		err := handler(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("role check: disallowed role returns 403 Forbidden", func(t *testing.T) {
		row := makeSessionRow(staffID, sessionID, nil)
		mock := &mockMiddlewareStore{
			getSessionByTokenHashFunc: func(_ context.Context, _ string) (sqlc.GetSessionByTokenHashRow, error) {
				return row, nil
			},
			getStaffRolesFunc: func(_ context.Context, _ uuid.UUID) ([]string, error) {
				return []string{auth.RoleCashier}, nil
			},
		}
		mw := auth.NewMiddleware(mock)

		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set(echo.HeaderAuthorization, "Bearer "+testToken)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		handler := mw.RequireAuth(auth.RoleManager)(func(c echo.Context) error {
			return response.OK(c, "allowed")
		})

		err := handler(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusForbidden, rec.Code)

		var resp response.APIResponse
		err = json.Unmarshal(rec.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, "FORBIDDEN", resp.Error.Code)
		assert.Contains(t, resp.Error.Message, "bạn không có quyền thực hiện thao tác này")
	})

	t.Run("role check: empty allowed roles passes for any active staff", func(t *testing.T) {
		row := makeSessionRow(staffID, sessionID, nil)
		mock := &mockMiddlewareStore{
			getSessionByTokenHashFunc: func(_ context.Context, _ string) (sqlc.GetSessionByTokenHashRow, error) {
				return row, nil
			},
			getStaffRolesFunc: func(_ context.Context, _ uuid.UUID) ([]string, error) {
				return []string{auth.RoleCashier}, nil
			},
		}
		mw := auth.NewMiddleware(mock)

		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set(echo.HeaderAuthorization, "Bearer "+testToken)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		handler := mw.RequireAuth()(func(c echo.Context) error {
			return response.OK(c, "any-role-allowed")
		})

		err := handler(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("get staff roles database failure returns 500 Internal Server Error", func(t *testing.T) {
		row := makeSessionRow(staffID, sessionID, nil)
		mock := &mockMiddlewareStore{
			getSessionByTokenHashFunc: func(_ context.Context, _ string) (sqlc.GetSessionByTokenHashRow, error) {
				return row, nil
			},
			getStaffRolesFunc: func(_ context.Context, _ uuid.UUID) ([]string, error) {
				return nil, errors.New("roles query error")
			},
		}
		mw := auth.NewMiddleware(mock)

		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set(echo.HeaderAuthorization, "Bearer "+testToken)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		handler := mw.RequireAuth()(func(c echo.Context) error {
			return response.OK(c, "success")
		})

		err := handler(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})

	t.Run("cookie token authentication succeeds", func(t *testing.T) {
		row := makeSessionRow(staffID, sessionID, nil)
		mock := &mockMiddlewareStore{
			getSessionByTokenHashFunc: func(_ context.Context, _ string) (sqlc.GetSessionByTokenHashRow, error) {
				return row, nil
			},
			getStaffRolesFunc: func(_ context.Context, _ uuid.UUID) ([]string, error) {
				return []string{auth.RoleCashier}, nil
			},
		}
		mw := auth.NewMiddleware(mock)

		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		req.AddCookie(&http.Cookie{
			Name:  auth.SessionCookieName,
			Value: testToken,
		})
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		handler := mw.RequireAuth()(func(c echo.Context) error {
			return response.OK(c, "cookie-ok")
		})

		err := handler(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("successful authentication injects StaffClaims into context", func(t *testing.T) {
		ws := auth.WorkspaceCashier
		row := makeSessionRow(staffID, sessionID, &ws)
		row.DisplayName = "Bob Cashier"
		row.LoginCode = "BOB"

		mock := &mockMiddlewareStore{
			getSessionByTokenHashFunc: func(_ context.Context, _ string) (sqlc.GetSessionByTokenHashRow, error) {
				return row, nil
			},
			getStaffRolesFunc: func(_ context.Context, _ uuid.UUID) ([]string, error) {
				return []string{auth.RoleCashier}, nil
			},
		}
		mw := auth.NewMiddleware(mock)

		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set(echo.HeaderAuthorization, "Bearer "+testToken)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		var capturedClaims *auth.StaffClaims
		handler := mw.RequireAuth()(func(c echo.Context) error {
			capturedClaims = auth.GetStaff(c)
			return response.OK(c, capturedClaims)
		})

		err := handler(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)

		require.NotNil(t, capturedClaims)
		assert.Equal(t, staffID, capturedClaims.StaffID)
		assert.Equal(t, sessionID, capturedClaims.SessionID)
		assert.Equal(t, "Bob Cashier", capturedClaims.DisplayName)
		assert.Equal(t, "BOB", capturedClaims.LoginCode)
		assert.Equal(t, []string{auth.RoleCashier}, capturedClaims.Roles)
		assert.Equal(t, auth.DeriveCapabilities([]string{auth.RoleCashier}), capturedClaims.Capabilities)
		require.NotNil(t, capturedClaims.Workspace)
		assert.Equal(t, auth.WorkspaceCashier, *capturedClaims.Workspace)
	})
}

func TestRequireCapability(t *testing.T) {
	e := setupEcho()
	mw := auth.NewMiddleware(&mockMiddlewareStore{})

	t.Run("missing staff claims in context returns 401 Unauthorized", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/capability-test", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		handler := mw.RequireCapability("sales.operate")(func(c echo.Context) error {
			return response.OK(c, "success")
		})

		err := handler(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)

		var resp response.APIResponse
		err = json.Unmarshal(rec.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.False(t, resp.Success)
		assert.Equal(t, "UNAUTHORIZED", resp.Error.Code)
		assert.Contains(t, resp.Error.Message, "vui lòng đăng nhập")
	})

	t.Run("lacking capability returns 403 Forbidden", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/capability-test", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.Set(auth.StaffContextKey, &auth.StaffClaims{
			StaffID:      uuid.New(),
			SessionID:    uuid.New(),
			Capabilities: []string{"sales.operate"},
		})

		handler := mw.RequireCapability("catalog.administer_structure")(func(c echo.Context) error {
			return response.OK(c, "success")
		})

		err := handler(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusForbidden, rec.Code)

		var resp response.APIResponse
		err = json.Unmarshal(rec.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, "FORBIDDEN", resp.Error.Code)
		assert.Contains(t, resp.Error.Message, "thiếu quyền catalog.administer_structure")
	})

	t.Run("possessing capability passes to next handler", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/capability-test", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.Set(auth.StaffContextKey, &auth.StaffClaims{
			StaffID:      uuid.New(),
			SessionID:    uuid.New(),
			Capabilities: []string{"sales.operate", "catalog.view_prices"},
		})

		handler := mw.RequireCapability("sales.operate")(func(c echo.Context) error {
			return response.OK(c, "allowed")
		})

		err := handler(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("package-level RequireCapability behaves identically", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/capability-test", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.Set(auth.StaffContextKey, &auth.StaffClaims{
			StaffID:      uuid.New(),
			SessionID:    uuid.New(),
			Capabilities: []string{"sales.operate"},
		})

		handler := auth.RequireCapability("sales.operate")(func(c echo.Context) error {
			return response.OK(c, "allowed-package")
		})

		err := handler(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)
	})
}

func TestNewMiddleware(t *testing.T) {
	mock := &mockMiddlewareStore{}
	mw := auth.NewMiddleware(mock)
	require.NotNil(t, mw)
}

func TestGetStaffDirect(t *testing.T) {
	e := setupEcho()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	assert.Nil(t, auth.GetStaff(c))

	c.Set(auth.StaffContextKey, "not-a-staff-claim")
	assert.Nil(t, auth.GetStaff(c))

	claims := &auth.StaffClaims{
		DisplayName: "Test",
	}
	c.Set(auth.StaffContextKey, claims)
	got := auth.GetStaff(c)
	require.NotNil(t, got)
	assert.Equal(t, "Test", got.DisplayName)
}
