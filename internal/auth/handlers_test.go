package auth_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/httpvalidator"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// === Mock Auth Store Implementation ===

type mockAuthQuerier struct {
	sqlc.Querier
	getStaffByLoginCodeFunc   func(ctx context.Context, btrim string) (sqlc.StaffIdentity, error)
	getStaffRolesFunc         func(ctx context.Context, staffIdentityID uuid.UUID) ([]string, error)
	createStaffSessionFunc    func(ctx context.Context, arg sqlc.CreateStaffSessionParams) (sqlc.CreateStaffSessionRow, error)
	getSessionByTokenHashFunc func(ctx context.Context, tokenHash string) (sqlc.GetSessionByTokenHashRow, error)
	updateSessionStateFunc    func(ctx context.Context, arg sqlc.UpdateSessionStateParams) error
	updateSessionActivityFunc func(ctx context.Context, arg sqlc.UpdateSessionActivityParams) error
	updateSessionWorkspaceFunc func(ctx context.Context, arg sqlc.UpdateSessionWorkspaceParams) error
	revokeSessionFunc         func(ctx context.Context, id uuid.UUID) error
	listActiveIdentitiesFunc  func(ctx context.Context) ([]sqlc.ListActiveIdentitiesRow, error)
}

func (m *mockAuthQuerier) GetStaffByLoginCode(ctx context.Context, btrim string) (sqlc.StaffIdentity, error) {
	if m.getStaffByLoginCodeFunc != nil {
		return m.getStaffByLoginCodeFunc(ctx, btrim)
	}
	return sqlc.StaffIdentity{}, sql.ErrNoRows
}

func (m *mockAuthQuerier) GetStaffRoles(ctx context.Context, staffIdentityID uuid.UUID) ([]string, error) {
	if m.getStaffRolesFunc != nil {
		return m.getStaffRolesFunc(ctx, staffIdentityID)
	}
	return nil, nil
}

func (m *mockAuthQuerier) CreateStaffSession(ctx context.Context, arg sqlc.CreateStaffSessionParams) (sqlc.CreateStaffSessionRow, error) {
	if m.createStaffSessionFunc != nil {
		return m.createStaffSessionFunc(ctx, arg)
	}
	return sqlc.CreateStaffSessionRow{}, nil
}

func (m *mockAuthQuerier) GetSessionByTokenHash(ctx context.Context, tokenHash string) (sqlc.GetSessionByTokenHashRow, error) {
	if m.getSessionByTokenHashFunc != nil {
		return m.getSessionByTokenHashFunc(ctx, tokenHash)
	}
	return sqlc.GetSessionByTokenHashRow{}, sql.ErrNoRows
}

func (m *mockAuthQuerier) UpdateSessionState(ctx context.Context, arg sqlc.UpdateSessionStateParams) error {
	if m.updateSessionStateFunc != nil {
		return m.updateSessionStateFunc(ctx, arg)
	}
	return nil
}

func (m *mockAuthQuerier) UpdateSessionActivity(ctx context.Context, arg sqlc.UpdateSessionActivityParams) error {
	if m.updateSessionActivityFunc != nil {
		return m.updateSessionActivityFunc(ctx, arg)
	}
	return nil
}

func (m *mockAuthQuerier) UpdateSessionWorkspace(ctx context.Context, arg sqlc.UpdateSessionWorkspaceParams) error {
	if m.updateSessionWorkspaceFunc != nil {
		return m.updateSessionWorkspaceFunc(ctx, arg)
	}
	return nil
}

func (m *mockAuthQuerier) RevokeSession(ctx context.Context, id uuid.UUID) error {
	if m.revokeSessionFunc != nil {
		return m.revokeSessionFunc(ctx, id)
	}
	return nil
}

func (m *mockAuthQuerier) ListActiveIdentities(ctx context.Context) ([]sqlc.ListActiveIdentitiesRow, error) {
	if m.listActiveIdentitiesFunc != nil {
		return m.listActiveIdentitiesFunc(ctx)
	}
	return nil, nil
}

func setupEcho() *echo.Echo {
	e := echo.New()
	e.Validator = httpvalidator.New()
	return e
}

func TestGetStaffClaims(t *testing.T) {
	e := setupEcho()

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

func TestSignInHandler(t *testing.T) {
	e := setupEcho()
	validPin := "1234"
	pinHash, err := auth.HashPin(validPin)
	require.NoError(t, err)

	staffID := uuid.New()
	activeStaff := sqlc.StaffIdentity{
		ID:          staffID,
		DisplayName: "Bob Manager",
		LoginCode:   "BOB",
		PinHash:     pinHash,
		Enabled:     true,
	}

	t.Run("invalid payload returns 400 Bad Request", func(t *testing.T) {
		h := auth.NewSignInHandler(&mockAuthQuerier{})
		body := []byte(`{"login_code":"","pin":"123"}`) // pin too short (<4 digits)
		req := httptest.NewRequest(http.MethodPost, "/auth/sign-in", bytes.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		err := h.HandleHTTP(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusBadRequest, rec.Code)

		var resp response.APIResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.False(t, resp.Success)
		assert.Equal(t, "BAD_REQUEST", resp.Error.Code)
	})

	t.Run("unknown login code returns 401 Unauthorized", func(t *testing.T) {
		mockQ := &mockAuthQuerier{
			getStaffByLoginCodeFunc: func(_ context.Context, _ string) (sqlc.StaffIdentity, error) {
				return sqlc.StaffIdentity{}, sql.ErrNoRows
			},
		}
		h := auth.NewSignInHandler(mockQ)
		body := []byte(`{"login_code":"UNKNOWN","pin":"1234"}`)
		req := httptest.NewRequest(http.MethodPost, "/auth/sign-in", bytes.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		err := h.HandleHTTP(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)

		var resp response.APIResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.False(t, resp.Success)
		assert.Equal(t, "UNAUTHORIZED", resp.Error.Code)
	})

	t.Run("incorrect PIN returns 401 Unauthorized", func(t *testing.T) {
		mockQ := &mockAuthQuerier{
			getStaffByLoginCodeFunc: func(_ context.Context, _ string) (sqlc.StaffIdentity, error) {
				return activeStaff, nil
			},
		}
		h := auth.NewSignInHandler(mockQ)
		body := []byte(`{"login_code":"BOB","pin":"9999"}`)
		req := httptest.NewRequest(http.MethodPost, "/auth/sign-in", bytes.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		err := h.HandleHTTP(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)

		var resp response.APIResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.False(t, resp.Success)
		assert.Equal(t, "UNAUTHORIZED", resp.Error.Code)
	})

	t.Run("disabled account returns 403 Forbidden", func(t *testing.T) {
		disabledStaff := activeStaff
		disabledStaff.Enabled = false
		mockQ := &mockAuthQuerier{
			getStaffByLoginCodeFunc: func(_ context.Context, _ string) (sqlc.StaffIdentity, error) {
				return disabledStaff, nil
			},
		}
		h := auth.NewSignInHandler(mockQ)
		body := []byte(`{"login_code":"BOB","pin":"1234"}`)
		req := httptest.NewRequest(http.MethodPost, "/auth/sign-in", bytes.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		err := h.HandleHTTP(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusForbidden, rec.Code)

		var resp response.APIResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.False(t, resp.Success)
		assert.Equal(t, "FORBIDDEN", resp.Error.Code)
	})

	t.Run("successful sign in returns 200 OK with token and cookie", func(t *testing.T) {
		mockQ := &mockAuthQuerier{
			getStaffByLoginCodeFunc: func(_ context.Context, _ string) (sqlc.StaffIdentity, error) {
				return activeStaff, nil
			},
			getStaffRolesFunc: func(_ context.Context, _ uuid.UUID) ([]string, error) {
				return []string{auth.RoleManager}, nil
			},
			createStaffSessionFunc: func(_ context.Context, _ sqlc.CreateStaffSessionParams) (sqlc.CreateStaffSessionRow, error) {
				return sqlc.CreateStaffSessionRow{ID: uuid.New()}, nil
			},
		}
		h := auth.NewSignInHandler(mockQ)
		body := []byte(`{"login_code":"BOB","pin":"1234"}`)
		req := httptest.NewRequest(http.MethodPost, "/auth/sign-in", bytes.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		err := h.HandleHTTP(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)

		// Verify cookie was set
		cookies := rec.Result().Cookies()
		var sessionCookie *http.Cookie
		for _, ck := range cookies {
			if ck.Name == auth.SessionCookieName {
				sessionCookie = ck
				break
			}
		}
		require.NotNil(t, sessionCookie)
		assert.NotEmpty(t, sessionCookie.Value)
		assert.True(t, sessionCookie.HttpOnly)
	})
}

func TestUnlockSessionHandler(t *testing.T) {
	e := setupEcho()
	validPin := "4321"
	pinHash, err := auth.HashPin(validPin)
	require.NoError(t, err)

	validToken, _, err := auth.GenerateSessionToken()
	require.NoError(t, err)

	sessionID := uuid.New()
	staffID := uuid.New()

	baseSession := sqlc.GetSessionByTokenHashRow{
		SessionID:           sessionID,
		StaffIdentityID:     staffID,
		SessionState:        auth.SessionStateLocked,
		LastAuthenticatedAt: time.Now().UTC().Add(-1 * time.Hour),
		LastHumanActivityAt: time.Now().UTC().Add(-10 * time.Minute),
		ExpiresAt:           time.Now().UTC().Add(10 * time.Hour),
		RevokedAt:           sql.NullTime{Valid: false},
		DisplayName:         "Charlie Cashier",
		LoginCode:           "CHARLIE",
		PinHash:             pinHash,
		IdentityEnabled:     true,
	}

	t.Run("missing token returns 401 Unauthorized", func(t *testing.T) {
		h := auth.NewUnlockSessionHandler(&mockAuthQuerier{})
		body := []byte(`{"pin":"4321"}`)
		req := httptest.NewRequest(http.MethodPost, "/auth/unlock", bytes.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		err := h.HandleHTTP(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("expired session returns 401 Unauthorized", func(t *testing.T) {
		expiredSession := baseSession
		expiredSession.ExpiresAt = time.Now().UTC().Add(-1 * time.Minute) // EXPIRED
		mockQ := &mockAuthQuerier{
			getSessionByTokenHashFunc: func(_ context.Context, _ string) (sqlc.GetSessionByTokenHashRow, error) {
				return expiredSession, nil
			},
		}
		h := auth.NewUnlockSessionHandler(mockQ)
		body := []byte(`{"pin":"4321"}`)
		req := httptest.NewRequest(http.MethodPost, "/auth/unlock", bytes.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		req.Header.Set(echo.HeaderAuthorization, "Bearer "+validToken)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		err := h.HandleHTTP(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)

		var resp response.APIResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.Equal(t, "UNAUTHORIZED", resp.Error.Code)
	})

	t.Run("revoked session returns 401 Unauthorized", func(t *testing.T) {
		revokedSession := baseSession
		revokedSession.RevokedAt = sql.NullTime{Time: time.Now().UTC(), Valid: true}
		mockQ := &mockAuthQuerier{
			getSessionByTokenHashFunc: func(_ context.Context, _ string) (sqlc.GetSessionByTokenHashRow, error) {
				return revokedSession, nil
			},
		}
		h := auth.NewUnlockSessionHandler(mockQ)
		body := []byte(`{"pin":"4321"}`)
		req := httptest.NewRequest(http.MethodPost, "/auth/unlock", bytes.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		req.Header.Set(echo.HeaderAuthorization, "Bearer "+validToken)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		err := h.HandleHTTP(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("incorrect PIN returns 401 Unauthorized", func(t *testing.T) {
		mockQ := &mockAuthQuerier{
			getSessionByTokenHashFunc: func(_ context.Context, _ string) (sqlc.GetSessionByTokenHashRow, error) {
				return baseSession, nil
			},
		}
		h := auth.NewUnlockSessionHandler(mockQ)
		body := []byte(`{"pin":"0000"}`)
		req := httptest.NewRequest(http.MethodPost, "/auth/unlock", bytes.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		req.Header.Set(echo.HeaderAuthorization, "Bearer "+validToken)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		err := h.HandleHTTP(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)

		var resp response.APIResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.Equal(t, "UNAUTHORIZED", resp.Error.Code)
	})

	t.Run("successful unlock refreshes LastHumanActivityAt and activates state", func(t *testing.T) {
		var updatedState string
		var activityUpdated bool

		mockQ := &mockAuthQuerier{
			getSessionByTokenHashFunc: func(_ context.Context, _ string) (sqlc.GetSessionByTokenHashRow, error) {
				return baseSession, nil
			},
			updateSessionStateFunc: func(_ context.Context, arg sqlc.UpdateSessionStateParams) error {
				updatedState = arg.State
				return nil
			},
			updateSessionActivityFunc: func(_ context.Context, arg sqlc.UpdateSessionActivityParams) error {
				activityUpdated = true
				assert.WithinDuration(t, time.Now().UTC(), arg.LastHumanActivityAt, 2*time.Second)
				return nil
			},
			getStaffRolesFunc: func(_ context.Context, _ uuid.UUID) ([]string, error) {
				return []string{auth.RoleCashier}, nil
			},
		}

		h := auth.NewUnlockSessionHandler(mockQ)
		body := []byte(`{"pin":"4321"}`)
		req := httptest.NewRequest(http.MethodPost, "/auth/unlock", bytes.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		req.Header.Set(echo.HeaderAuthorization, "Bearer "+validToken)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		err := h.HandleHTTP(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, auth.SessionStateActive, updatedState)
		assert.True(t, activityUpdated, "expected LastHumanActivityAt to be refreshed")
	})
}

func TestUnauthenticatedAndForbiddenRequests(t *testing.T) {
	e := setupEcho()

	t.Run("lock session returns 401 when unauthenticated", func(t *testing.T) {
		h := auth.NewLockSessionHandler(&mockAuthQuerier{})
		req := httptest.NewRequest(http.MethodPost, "/auth/lock", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		err := h.HandleHTTP(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("declare workspace returns 401 when unauthenticated", func(t *testing.T) {
		h := auth.NewDeclareWorkspaceHandler(&mockAuthQuerier{})
		body := []byte(`{"workspace":"cashier"}`)
		req := httptest.NewRequest(http.MethodPost, "/auth/workspace", bytes.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		err := h.HandleHTTP(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("declare workspace returns 403 Forbidden when lacking capability", func(t *testing.T) {
		h := auth.NewDeclareWorkspaceHandler(&mockAuthQuerier{})
		body := []byte(`{"workspace":"manager"}`)
		req := httptest.NewRequest(http.MethodPost, "/auth/workspace", bytes.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		// Staff is a barista without manager role
		c.Set(auth.StaffContextKey, &auth.StaffClaims{
			StaffID:      uuid.New(),
			SessionID:    uuid.New(),
			DisplayName:  "Barista Bob",
			LoginCode:    "BOB",
			Roles:        []string{auth.RoleBarista},
			Capabilities: []string{"preparation.operate"},
		})

		err := h.HandleHTTP(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("record activity returns 401 when unauthenticated", func(t *testing.T) {
		h := auth.NewRecordActivityHandler(&mockAuthQuerier{})
		req := httptest.NewRequest(http.MethodPost, "/auth/activity", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		err := h.HandleHTTP(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
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
