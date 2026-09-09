package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestRateLimiter(t *testing.T) {
	limiter := auth.NewRateLimiter(3, time.Minute)

	assert.True(t, limiter.Allow("key-1"))
	assert.True(t, limiter.Allow("key-1"))
	assert.True(t, limiter.Allow("key-1"))
	assert.False(t, limiter.Allow("key-1"))
	assert.True(t, limiter.Allow("key-2"), "attempts must be isolated by key")

	limiter.Reset("key-1")
	assert.True(t, limiter.Allow("key-1"))
}

func TestRateLimitMiddleware(t *testing.T) {
	e := setupEcho()
	limiter := auth.NewRateLimiter(1, time.Minute)
	middleware := auth.NewMiddleware(nil).RateLimit(limiter, func(c echo.Context) string {
		return c.Request().Header.Get("X-Rate-Limit-Key")
	})
	handler := middleware(func(c echo.Context) error {
		return response.OK(c, "allowed")
	})

	first := httptest.NewRequest(http.MethodPost, "/", nil)
	first.Header.Set("X-Rate-Limit-Key", "staff")
	firstRecorder := httptest.NewRecorder()
	require.NoError(t, handler(e.NewContext(first, firstRecorder)))
	assert.Equal(t, http.StatusOK, firstRecorder.Code)

	second := httptest.NewRequest(http.MethodPost, "/", nil)
	second.Header.Set("X-Rate-Limit-Key", "staff")
	secondRecorder := httptest.NewRecorder()
	require.NoError(t, handler(e.NewContext(second, secondRecorder)))
	assert.Equal(t, http.StatusTooManyRequests, secondRecorder.Code)

	var res response.APIResponse
	require.NoError(t, json.Unmarshal(secondRecorder.Body.Bytes(), &res))
	require.NotNil(t, res.Error)
	assert.Equal(t, "TOO_MANY_REQUESTS", res.Error.Code)
}

func TestPINRoutesRateLimitInvalidRequests(t *testing.T) {
	t.Run("sign-in normalizes login code and blocks the sixth attempt", func(t *testing.T) {
		e := setupEcho()
		slices := auth.NewSlices(nil, nil)
		slices.RegisterRoutes(e.Group("/api/v1"))

		loginCodes := []string{"RATE-LIMITED", "rate-limited", "Rate-Limited", " RATE-LIMITED", "rate-limited ", " RATE-limited "}
		for attempt, loginCode := range loginCodes {
			body := []byte(`{"login_code":"` + loginCode + `","pin":"1"}`)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sign-in", bytes.NewReader(body))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			rec := httptest.NewRecorder()

			e.ServeHTTP(rec, req)
			if attempt < 5 {
				assert.Equal(t, http.StatusBadRequest, rec.Code, "attempt %d", attempt+1)
			} else {
				assert.Equal(t, http.StatusTooManyRequests, rec.Code)
			}
		}
	})

	t.Run("unlock blocks the fourth attempt for a session token", func(t *testing.T) {
		e := setupEcho()
		slices := auth.NewSlices(nil, nil)
		slices.RegisterRoutes(e.Group("/api/v1"))

		for attempt := 1; attempt <= 4; attempt++ {
			body := []byte(`{"pin":"1"}`)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/unlock", bytes.NewReader(body))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			req.Header.Set(echo.HeaderAuthorization, "Bearer session-token")
			rec := httptest.NewRecorder()

			e.ServeHTTP(rec, req)
			if attempt <= 3 {
				assert.Equal(t, http.StatusBadRequest, rec.Code, "attempt %d", attempt)
			} else {
				assert.Equal(t, http.StatusTooManyRequests, rec.Code)
			}
		}
	})
}

func TestSuccessfulAuthResetsRateLimit(t *testing.T) {
	t.Run("sign-in", func(t *testing.T) {
		pinHash, err := auth.HashPin("1234")
		require.NoError(t, err)
		staffID := uuid.New()
		store := &mockAuthQuerier{
			getStaffByLoginCodeFunc: func(_ context.Context, _ string) (sqlc.StaffIdentity, error) {
				return sqlc.StaffIdentity{
					ID:          staffID,
					DisplayName: "Rate Limited",
					LoginCode:   "RESET-ME",
					PinHash:     pinHash,
					Enabled:     true,
				}, nil
			},
			getStaffRolesFunc: func(_ context.Context, _ uuid.UUID) ([]string, error) {
				return []string{auth.RoleCashier}, nil
			},
			createStaffSessionFunc: func(_ context.Context, _ sqlc.CreateStaffSessionParams) (sqlc.CreateStaffSessionRow, error) {
				return sqlc.CreateStaffSessionRow{ID: uuid.New()}, nil
			},
		}
		limiter := auth.NewRateLimiter(5, time.Minute)
		for range 5 {
			require.True(t, limiter.Allow("sign-in:RESET-ME"))
		}
		require.False(t, limiter.Allow("sign-in:RESET-ME"))

		handler := auth.NewSignInHandler(store, limiter)
		req := httptest.NewRequest(http.MethodPost, "/auth/sign-in", bytes.NewBufferString(`{"login_code":"RESET-ME","pin":"1234"}`))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		require.NoError(t, handler.HandleHTTP(setupEcho().NewContext(req, rec)))
		require.Equal(t, http.StatusOK, rec.Code)
		assert.True(t, limiter.Allow("sign-in:RESET-ME"))
	})

	t.Run("unlock", func(t *testing.T) {
		pinHash, err := auth.HashPin("4321")
		require.NoError(t, err)
		token, _, err := auth.GenerateSessionToken()
		require.NoError(t, err)
		cfg := &mockConnConfig{
			session: sqlc.GetSessionByTokenHashRow{
				SessionID:       uuid.New(),
				StaffIdentityID: uuid.New(),
				SessionState:    auth.SessionStateLocked,
				ExpiresAt:       time.Now().UTC().Add(time.Hour),
				PinHash:         pinHash,
				IdentityEnabled: true,
			},
			roles: []string{auth.RoleCashier},
		}
		db, queries := setupMockDB(t, cfg)
		limiter := auth.NewRateLimiter(3, time.Minute)
		key := "unlock:" + auth.HashToken(token)
		for range 3 {
			require.True(t, limiter.Allow(key))
		}
		require.False(t, limiter.Allow(key))

		handler := auth.NewUnlockSessionHandler(db, queries, limiter)
		req := httptest.NewRequest(http.MethodPost, "/auth/unlock", bytes.NewBufferString(`{"pin":"4321"}`))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		req.Header.Set(echo.HeaderAuthorization, "Bearer "+token)
		rec := httptest.NewRecorder()
		require.NoError(t, handler.HandleHTTP(setupEcho().NewContext(req, rec)))
		require.Equal(t, http.StatusOK, rec.Code)
		assert.True(t, limiter.Allow(key))
	})
}
