//go:build integration

package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/database"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/httpvalidator"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const truncateAuthTablesQuery = "TRUNCATE TABLE staff_access_sessions, idempotency_keys, staff_operational_roles, staff_identities RESTART IDENTITY CASCADE;"

func setupTestApp(t *testing.T) (*echo.Echo, func()) {
	t.Helper()

	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable"
	}

	// Strict safety guard: prevent running against non-test databases
	if !strings.Contains(dbURL, "_test?") && !strings.HasSuffix(dbURL, "_test") {
		t.Fatalf("SAFETY VIOLATION: TEST_DATABASE_URL must target a database name ending with '_test' to protect development data. Got: %s", dbURL)
	}

	ctx := context.Background()
	db, err := database.Open(ctx, dbURL)
	require.NoError(t, err)

	// Clean tables before test suite
	_, err = db.ExecContext(ctx, truncateAuthTablesQuery)
	require.NoError(t, err)

	queries := sqlc.New(db)
	slices := auth.NewSlices(db, queries)

	e := echo.New()
	e.Validator = httpvalidator.New()

	v1 := e.Group("/api/v1")
	slices.RegisterRoutes(v1)

	cleanup := func() {
		_, cleanErr := db.ExecContext(context.Background(), truncateAuthTablesQuery)
		assert.NoError(t, cleanErr)
		_ = db.Close()
	}

	return e, cleanup
}

type genericResponse[T any] struct {
	Success bool               `json:"success"`
	Data    T                  `json:"data"`
	Error   *response.APIError `json:"error"`
}

func parseResponse[T any](t *testing.T, rec *httptest.ResponseRecorder) genericResponse[T] {
	t.Helper()
	var res genericResponse[T]
	err := json.Unmarshal(rec.Body.Bytes(), &res)
	require.NoError(t, err, "failed to unmarshal JSON response (status=%d): %s", rec.Code, rec.Body.String())
	return res
}

func doJSONRequest(e *echo.Echo, method, path string, body any, headers map[string]string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	var bodyReader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, bodyReader)
	if body != nil {
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func getCookieByName(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestBootstrapManager(t *testing.T) {
	e, cleanup := setupTestApp(t)
	defer cleanup()

	// 1. First call creates manager (201 Created)
	reqBody := auth.BootstrapManagerRequest{
		DisplayName: "Quản Lý Cửa Hàng",
		LoginCode:   "QL01",
		Pin:         "1234",
	}
	rec := doJSONRequest(e, http.MethodPost, "/api/v1/auth/bootstrap", reqBody, nil)
	assert.Equal(t, http.StatusCreated, rec.Code)

	res := parseResponse[auth.StaffProfileResponse](t, rec)
	assert.True(t, res.Success)
	assert.Equal(t, "Quản Lý Cửa Hàng", res.Data.DisplayName)
	assert.Equal(t, "QL01", res.Data.LoginCode)
	assert.True(t, res.Data.Enabled)
	assert.Contains(t, res.Data.Roles, auth.RoleManager)
	assert.Contains(t, res.Data.Capabilities, "staff.administer")
	assert.NotEqual(t, uuid.Nil, res.Data.ID)

	// 2. Second call returns 409 Conflict
	secondReq := auth.BootstrapManagerRequest{
		DisplayName: "Quản Lý Thứ Hai",
		LoginCode:   "QL02",
		Pin:         "5678",
	}
	rec2 := doJSONRequest(e, http.MethodPost, "/api/v1/auth/bootstrap", secondReq, nil)
	assert.Equal(t, http.StatusConflict, rec2.Code)

	res2 := parseResponse[any](t, rec2)
	assert.False(t, res2.Success)
	require.NotNil(t, res2.Error)
	assert.Equal(t, "CONFLICT", res2.Error.Code)
}

func TestSignInAndSession(t *testing.T) {
	e, cleanup := setupTestApp(t)
	defer cleanup()

	// Setup: bootstrap manager
	bootReq := auth.BootstrapManagerRequest{
		DisplayName: "Quản Lý Cửa Hàng",
		LoginCode:   "QL01",
		Pin:         "1234",
	}
	bootRec := doJSONRequest(e, http.MethodPost, "/api/v1/auth/bootstrap", bootReq, nil)
	require.Equal(t, http.StatusCreated, bootRec.Code)

	// 1. Incorrect PIN returns 401 Unauthorized
	badSignIn := auth.SignInRequest{
		LoginCode: "QL01",
		Pin:       "9999",
	}
	recBad := doJSONRequest(e, http.MethodPost, "/api/v1/auth/sign-in", badSignIn, nil)
	assert.Equal(t, http.StatusUnauthorized, recBad.Code)
	badRes := parseResponse[any](t, recBad)
	assert.False(t, badRes.Success)
	require.NotNil(t, badRes.Error)
	assert.Equal(t, "UNAUTHORIZED", badRes.Error.Code)

	// 2. Correct PIN returns 200 with token and staff_session_token cookie
	goodSignIn := auth.SignInRequest{
		LoginCode: "QL01",
		Pin:       "1234",
	}
	recGood := doJSONRequest(e, http.MethodPost, "/api/v1/auth/sign-in", goodSignIn, nil)
	assert.Equal(t, http.StatusOK, recGood.Code)

	goodRes := parseResponse[auth.SignInResponse](t, recGood)
	assert.True(t, goodRes.Success)
	token := goodRes.Data.Token
	assert.NotEmpty(t, token)
	assert.Equal(t, "QL01", goodRes.Data.Staff.LoginCode)

	// Verify cookie
	sessionCookie := getCookieByName(recGood, auth.SessionCookieName)
	require.NotNil(t, sessionCookie, "staff_session_token cookie must be present")
	assert.Equal(t, token, sessionCookie.Value)
	assert.True(t, sessionCookie.HttpOnly)

	// 3. GET /api/v1/auth/session with Bearer token
	recBearer := doJSONRequest(e, http.MethodGet, "/api/v1/auth/session", nil, map[string]string{
		"Authorization": "Bearer " + token,
	})
	assert.Equal(t, http.StatusOK, recBearer.Code)
	sessBearerRes := parseResponse[auth.SessionStateResponse](t, recBearer)
	assert.True(t, sessBearerRes.Success)
	assert.Equal(t, auth.SessionStateAuthenticated, sessBearerRes.Data.State)
	assert.Equal(t, "QL01", sessBearerRes.Data.LoginCode)

	// 4. GET /api/v1/auth/session with cookie
	recCookie := doJSONRequest(e, http.MethodGet, "/api/v1/auth/session", nil, nil, sessionCookie)
	assert.Equal(t, http.StatusOK, recCookie.Code)
	sessCookieRes := parseResponse[auth.SessionStateResponse](t, recCookie)
	assert.True(t, sessCookieRes.Success)
	assert.Equal(t, auth.SessionStateAuthenticated, sessCookieRes.Data.State)
	assert.Equal(t, "QL01", sessCookieRes.Data.LoginCode)

	// 5. GET /api/v1/auth/session without token (returns signed_out)
	recNone := doJSONRequest(e, http.MethodGet, "/api/v1/auth/session", nil, nil)
	assert.Equal(t, http.StatusOK, recNone.Code)
	sessNoneRes := parseResponse[auth.SessionStateResponse](t, recNone)
	assert.True(t, sessNoneRes.Success)
	assert.Equal(t, "signed_out", sessNoneRes.Data.State)

	// 6. GET /api/v1/auth/identities
	recIdentities := doJSONRequest(e, http.MethodGet, "/api/v1/auth/identities", nil, nil)
	assert.Equal(t, http.StatusOK, recIdentities.Code)
	identitiesRes := parseResponse[[]auth.IdentitySummaryResponse](t, recIdentities)
	assert.True(t, identitiesRes.Success)
	require.NotEmpty(t, identitiesRes.Data)
	assert.Equal(t, "QL01", identitiesRes.Data[0].LoginCode)
	assert.Equal(t, "Quản Lý Cửa Hàng", identitiesRes.Data[0].DisplayName)
}

func TestSessionLockAndUnlock(t *testing.T) {
	e, cleanup := setupTestApp(t)
	defer cleanup()

	// Bootstrap & sign in
	bootReq := auth.BootstrapManagerRequest{
		DisplayName: "Quản Lý Cửa Hàng",
		LoginCode:   "QL01",
		Pin:         "1234",
	}
	bootRec := doJSONRequest(e, http.MethodPost, "/api/v1/auth/bootstrap", bootReq, nil)
	require.Equal(t, http.StatusCreated, bootRec.Code)

	signInReq := auth.SignInRequest{
		LoginCode: "QL01",
		Pin:       "1234",
	}
	signInRec := doJSONRequest(e, http.MethodPost, "/api/v1/auth/sign-in", signInReq, nil)
	require.Equal(t, http.StatusOK, signInRec.Code)
	signInRes := parseResponse[auth.SignInResponse](t, signInRec)
	token := signInRes.Data.Token
	sessionCookie := getCookieByName(signInRec, auth.SessionCookieName)
	authHeaders := map[string]string{"Authorization": "Bearer " + token}

	// 1. POST /api/v1/auth/lock
	recLock := doJSONRequest(e, http.MethodPost, "/api/v1/auth/lock", nil, authHeaders)
	assert.Equal(t, http.StatusOK, recLock.Code)
	lockRes := parseResponse[map[string]string](t, recLock)
	assert.True(t, lockRes.Success)
	assert.Equal(t, auth.SessionStateLocked, lockRes.Data["state"])

	// 2. Verify locked state on session
	recSessionLocked := doJSONRequest(e, http.MethodGet, "/api/v1/auth/session", nil, authHeaders)
	assert.Equal(t, http.StatusOK, recSessionLocked.Code)
	sessLockedRes := parseResponse[auth.SessionStateResponse](t, recSessionLocked)
	assert.True(t, sessLockedRes.Success)
	assert.Equal(t, auth.SessionStateLocked, sessLockedRes.Data.State)

	// 3. Protected endpoints reject with 403 Forbidden while session is locked
	recActivityLocked := doJSONRequest(e, http.MethodPost, "/api/v1/auth/activity", nil, authHeaders)
	assert.Equal(t, http.StatusForbidden, recActivityLocked.Code)

	recWorkspaceLocked := doJSONRequest(e, http.MethodPost, "/api/v1/auth/workspace", auth.DeclareWorkspaceRequest{
		Workspace: auth.WorkspaceManager,
	}, authHeaders)
	assert.Equal(t, http.StatusForbidden, recWorkspaceLocked.Code)

	// 4. Unlock with wrong PIN returns 401 Unauthorized
	recUnlockBad := doJSONRequest(e, http.MethodPost, "/api/v1/auth/unlock", auth.UnlockRequest{
		Pin: "9999",
	}, authHeaders)
	assert.Equal(t, http.StatusUnauthorized, recUnlockBad.Code)

	// 5. Unlock with correct PIN succeeds and reactivates session
	recUnlockGood := doJSONRequest(e, http.MethodPost, "/api/v1/auth/unlock", auth.UnlockRequest{
		Pin: "1234",
	}, authHeaders)
	assert.Equal(t, http.StatusOK, recUnlockGood.Code)
	unlockRes := parseResponse[auth.SignInResponse](t, recUnlockGood)
	assert.True(t, unlockRes.Success)
	assert.Equal(t, token, unlockRes.Data.Token)

	recSessionUnlocked := doJSONRequest(e, http.MethodGet, "/api/v1/auth/session", nil, authHeaders)
	assert.Equal(t, http.StatusOK, recSessionUnlocked.Code)
	sessUnlockedRes := parseResponse[auth.SessionStateResponse](t, recSessionUnlocked)
	assert.True(t, sessUnlockedRes.Success)
	assert.Equal(t, auth.SessionStateAuthenticated, sessUnlockedRes.Data.State)

	// 6. Workspace declaration
	recWorkspace := doJSONRequest(e, http.MethodPost, "/api/v1/auth/workspace", auth.DeclareWorkspaceRequest{
		Workspace: auth.WorkspaceManager,
	}, authHeaders)
	assert.Equal(t, http.StatusOK, recWorkspace.Code)
	wsRes := parseResponse[map[string]string](t, recWorkspace)
	assert.True(t, wsRes.Success)
	assert.Equal(t, auth.WorkspaceManager, wsRes.Data["workspace"])

	recSessionWS := doJSONRequest(e, http.MethodGet, "/api/v1/auth/session", nil, authHeaders)
	sessWSRes := parseResponse[auth.SessionStateResponse](t, recSessionWS)
	assert.True(t, sessWSRes.Success)
	require.NotNil(t, sessWSRes.Data.Workspace)
	assert.Equal(t, auth.WorkspaceManager, *sessWSRes.Data.Workspace)

	// 7. Activity recording
	recActivity := doJSONRequest(e, http.MethodPost, "/api/v1/auth/activity", nil, authHeaders)
	assert.Equal(t, http.StatusOK, recActivity.Code)
	actRes := parseResponse[map[string]bool](t, recActivity)
	assert.True(t, actRes.Success)
	assert.True(t, actRes.Data["recorded"])

	// 8. Sign-out clears session and cookie
	recSignOut := doJSONRequest(e, http.MethodPost, "/api/v1/auth/sign-out", nil, authHeaders, sessionCookie)
	assert.Equal(t, http.StatusOK, recSignOut.Code)
	signOutRes := parseResponse[map[string]string](t, recSignOut)
	assert.True(t, signOutRes.Success)
	assert.Equal(t, "signed_out", signOutRes.Data["state"])

	clearedCookie := getCookieByName(recSignOut, auth.SessionCookieName)
	require.NotNil(t, clearedCookie)
	assert.Equal(t, "", clearedCookie.Value)
	assert.True(t, clearedCookie.MaxAge < 0)

	// Verify session is now signed out
	recSessionSignedOut := doJSONRequest(e, http.MethodGet, "/api/v1/auth/session", nil, authHeaders)
	assert.Equal(t, http.StatusOK, recSessionSignedOut.Code)
	sessSignedOutRes := parseResponse[auth.SessionStateResponse](t, recSessionSignedOut)
	assert.True(t, sessSignedOutRes.Success)
	assert.Equal(t, "signed_out", sessSignedOutRes.Data.State)

	// Verify protected endpoint rejects revoked session with 401 Unauthorized
	recActivityAfterSignOut := doJSONRequest(e, http.MethodPost, "/api/v1/auth/activity", nil, authHeaders)
	assert.Equal(t, http.StatusUnauthorized, recActivityAfterSignOut.Code)
}

func TestStaffAdministrationAndIdempotency(t *testing.T) {
	e, cleanup := setupTestApp(t)
	defer cleanup()

	// Setup: bootstrap manager
	bootReq := auth.BootstrapManagerRequest{
		DisplayName: "Quản Lý Cửa Hàng",
		LoginCode:   "QL01",
		Pin:         "1234",
	}
	bootRec := doJSONRequest(e, http.MethodPost, "/api/v1/auth/bootstrap", bootReq, nil)
	require.Equal(t, http.StatusCreated, bootRec.Code)

	// Sign in as manager
	signInReq := auth.SignInRequest{
		LoginCode: "QL01",
		Pin:       "1234",
	}
	signInRec := doJSONRequest(e, http.MethodPost, "/api/v1/auth/sign-in", signInReq, nil)
	require.Equal(t, http.StatusOK, signInRec.Code)
	mgrToken := parseResponse[auth.SignInResponse](t, signInRec).Data.Token
	mgrHeaders := map[string]string{"Authorization": "Bearer " + mgrToken}

	// 1. Manager creates staff with request_id
	createReqID := uuid.New()
	createStaffPayload := auth.CreateStaffRequest{
		RequestID:   createReqID,
		DisplayName: "Thu Ngân 01",
		LoginCode:   "TN01",
		Enabled:     true,
		Roles:       []string{auth.RoleCashier},
		Pin:         "5678",
		ManagerPin:  "1234",
	}
	recCreate := doJSONRequest(e, http.MethodPost, "/api/v1/staff", createStaffPayload, mgrHeaders)
	assert.Equal(t, http.StatusCreated, recCreate.Code)
	createdStaffRes := parseResponse[auth.StaffDetailResponse](t, recCreate)
	assert.True(t, createdStaffRes.Success)
	staff1ID := createdStaffRes.Data.ID
	assert.NotEqual(t, uuid.Nil, staff1ID)
	assert.Equal(t, "TN01", createdStaffRes.Data.LoginCode)
	assert.True(t, createdStaffRes.Data.Enabled)
	assert.Equal(t, []string{auth.RoleCashier}, createdStaffRes.Data.Roles)

	// 2. Duplicate call with same request_id and payload returns cached response
	recDup := doJSONRequest(e, http.MethodPost, "/api/v1/staff", createStaffPayload, mgrHeaders)
	assert.Equal(t, http.StatusCreated, recDup.Code)
	dupRes := parseResponse[auth.StaffDetailResponse](t, recDup)
	assert.True(t, dupRes.Success)
	assert.Equal(t, staff1ID, dupRes.Data.ID)
	assert.Equal(t, "TN01", dupRes.Data.LoginCode)

	// 3. Duplicate call with same request_id but different payload returns 409 Conflict
	differentPayload := createStaffPayload
	differentPayload.DisplayName = "Thu Ngân Sửa Tên"
	recConflict := doJSONRequest(e, http.MethodPost, "/api/v1/staff", differentPayload, mgrHeaders)
	assert.Equal(t, http.StatusConflict, recConflict.Code)
	conflictRes := parseResponse[any](t, recConflict)
	assert.False(t, conflictRes.Success)
	require.NotNil(t, conflictRes.Error)
	assert.Equal(t, "CONFLICT", conflictRes.Error.Code)

	// 4. Duplicate login code with different request_id returns 409 Conflict
	dupLoginCodePayload := auth.CreateStaffRequest{
		RequestID:   uuid.New(),
		DisplayName: "Thu Ngân Trùng Code",
		LoginCode:   "TN01",
		Enabled:     true,
		Roles:       []string{auth.RoleCashier},
		Pin:         "9999",
		ManagerPin:  "1234",
	}
	recDupLogin := doJSONRequest(e, http.MethodPost, "/api/v1/staff", dupLoginCodePayload, mgrHeaders)
	assert.Equal(t, http.StatusConflict, recDupLogin.Code)

	// 5. Non-manager caller gets 403 Forbidden
	// Sign in as created cashier TN01
	cashierSignIn := auth.SignInRequest{
		LoginCode: "TN01",
		Pin:       "5678",
	}
	cashierSignInRec := doJSONRequest(e, http.MethodPost, "/api/v1/auth/sign-in", cashierSignIn, nil)
	require.Equal(t, http.StatusOK, cashierSignInRec.Code)
	cashierToken := parseResponse[auth.SignInResponse](t, cashierSignInRec).Data.Token
	cashierHeaders := map[string]string{"Authorization": "Bearer " + cashierToken}

	// Non-manager tries staff routes
	recNonMgrMe := doJSONRequest(e, http.MethodGet, "/api/v1/staff/me", nil, cashierHeaders)
	assert.Equal(t, http.StatusForbidden, recNonMgrMe.Code)

	recNonMgrList := doJSONRequest(e, http.MethodGet, "/api/v1/staff", nil, cashierHeaders)
	assert.Equal(t, http.StatusForbidden, recNonMgrList.Code)

	recNonMgrCreate := doJSONRequest(e, http.MethodPost, "/api/v1/staff", createStaffPayload, cashierHeaders)
	assert.Equal(t, http.StatusForbidden, recNonMgrCreate.Code)

	recNonMgrRoles := doJSONRequest(e, http.MethodPut, "/api/v1/staff/"+staff1ID.String()+"/roles", auth.ReplaceStaffRolesRequest{
		RequestID:  uuid.New(),
		Roles:      []string{auth.RoleBarista},
		ManagerPin: "1234",
	}, cashierHeaders)
	assert.Equal(t, http.StatusForbidden, recNonMgrRoles.Code)

	recNonMgrReset := doJSONRequest(e, http.MethodPost, "/api/v1/staff/"+staff1ID.String()+"/reset-pin", auth.ResetStaffPinRequest{
		RequestID:  uuid.New(),
		Pin:        "8888",
		ManagerPin: "1234",
	}, cashierHeaders)
	assert.Equal(t, http.StatusForbidden, recNonMgrReset.Code)

	// 6. Manager calls GET /api/v1/staff/me
	recMgrMe := doJSONRequest(e, http.MethodGet, "/api/v1/staff/me", nil, mgrHeaders)
	assert.Equal(t, http.StatusOK, recMgrMe.Code)
	mgrMeRes := parseResponse[auth.StaffProfileResponse](t, recMgrMe)
	assert.True(t, mgrMeRes.Success)
	assert.Equal(t, "QL01", mgrMeRes.Data.LoginCode)
	assert.Contains(t, mgrMeRes.Data.Roles, auth.RoleManager)

	// 7. Manager calls GET /api/v1/staff
	recMgrList := doJSONRequest(e, http.MethodGet, "/api/v1/staff", nil, mgrHeaders)
	assert.Equal(t, http.StatusOK, recMgrList.Code)
	mgrListRes := parseResponse[[]auth.StaffDetailResponse](t, recMgrList)
	assert.True(t, mgrListRes.Success)
	assert.GreaterOrEqual(t, len(mgrListRes.Data), 2)

	// 8. Manager calls PUT /api/v1/staff/:id/roles
	replaceRolesPayload := auth.ReplaceStaffRolesRequest{
		RequestID:  uuid.New(),
		Roles:      []string{auth.RoleCashier, auth.RoleBarista},
		ManagerPin: "1234",
	}
	recReplace := doJSONRequest(e, http.MethodPut, "/api/v1/staff/"+staff1ID.String()+"/roles", replaceRolesPayload, mgrHeaders)
	assert.Equal(t, http.StatusOK, recReplace.Code)
	replaceRes := parseResponse[auth.StaffDetailResponse](t, recReplace)
	assert.True(t, replaceRes.Success)
	assert.ElementsMatch(t, []string{auth.RoleCashier, auth.RoleBarista}, replaceRes.Data.Roles)

	// 9. Manager calls POST /api/v1/staff/:id/reset-pin
	resetPinPayload := auth.ResetStaffPinRequest{
		RequestID:  uuid.New(),
		Pin:        "8888",
		ManagerPin: "1234",
	}
	recReset := doJSONRequest(e, http.MethodPost, "/api/v1/staff/"+staff1ID.String()+"/reset-pin", resetPinPayload, mgrHeaders)
	assert.Equal(t, http.StatusOK, recReset.Code)
	resetRes := parseResponse[map[string]string](t, recReset)
	assert.True(t, resetRes.Success)

	// Verify TN01 can sign in with new PIN 8888
	recNewPinSignIn := doJSONRequest(e, http.MethodPost, "/api/v1/auth/sign-in", auth.SignInRequest{
		LoginCode: "TN01",
		Pin:       "8888",
	}, nil)
	assert.Equal(t, http.StatusOK, recNewPinSignIn.Code)

	// Verify TN01 cannot sign in with old PIN 5678
	recOldPinSignIn := doJSONRequest(e, http.MethodPost, "/api/v1/auth/sign-in", auth.SignInRequest{
		LoginCode: "TN01",
		Pin:       "5678",
	}, nil)
	assert.Equal(t, http.StatusUnauthorized, recOldPinSignIn.Code)
}

func TestManagerInvariant(t *testing.T) {
	e, cleanup := setupTestApp(t)
	defer cleanup()

	// Setup: bootstrap manager
	bootReq := auth.BootstrapManagerRequest{
		DisplayName: "Quản Lý Cửa Hàng",
		LoginCode:   "QL01",
		Pin:         "1234",
	}
	bootRec := doJSONRequest(e, http.MethodPost, "/api/v1/auth/bootstrap", bootReq, nil)
	require.Equal(t, http.StatusCreated, bootRec.Code)

	// Sign in as manager
	signInReq := auth.SignInRequest{
		LoginCode: "QL01",
		Pin:       "1234",
	}
	signInRec := doJSONRequest(e, http.MethodPost, "/api/v1/auth/sign-in", signInReq, nil)
	require.Equal(t, http.StatusOK, signInRec.Code)
	mgrToken := parseResponse[auth.SignInResponse](t, signInRec).Data.Token
	mgrHeaders := map[string]string{"Authorization": "Bearer " + mgrToken}

	// Get manager ID via /staff/me
	meRec := doJSONRequest(e, http.MethodGet, "/api/v1/staff/me", nil, mgrHeaders)
	require.Equal(t, http.StatusOK, meRec.Code)
	mgr1ID := parseResponse[auth.StaffProfileResponse](t, meRec).Data.ID
	require.NotEqual(t, uuid.Nil, mgr1ID)

	// 1. Attempting to deactivate the only active manager returns 409 Conflict
	deactReq := auth.SetStaffEnabledRequest{
		RequestID:       uuid.New(),
		ExpectedEnabled: true,
		Enabled:         false,
		ManagerPin:      "1234",
	}
	recDeact := doJSONRequest(e, http.MethodPatch, "/api/v1/staff/"+mgr1ID.String()+"/enabled", deactReq, mgrHeaders)
	assert.Equal(t, http.StatusConflict, recDeact.Code)
	deactRes := parseResponse[any](t, recDeact)
	assert.False(t, deactRes.Success)
	require.NotNil(t, deactRes.Error)
	assert.Equal(t, "FINAL_ENABLED_MANAGER_REQUIRED", deactRes.Error.Code)

	// 2. Removing MANAGER role from the only active manager returns 409 Conflict
	removeMgrRoleReq := auth.ReplaceStaffRolesRequest{
		RequestID:  uuid.New(),
		Roles:      []string{auth.RoleCashier},
		ManagerPin: "1234",
	}
	recRemoveRole := doJSONRequest(e, http.MethodPut, "/api/v1/staff/"+mgr1ID.String()+"/roles", removeMgrRoleReq, mgrHeaders)
	assert.Equal(t, http.StatusConflict, recRemoveRole.Code)
	removeRoleRes := parseResponse[any](t, recRemoveRole)
	assert.False(t, removeRoleRes.Success)
	require.NotNil(t, removeRoleRes.Error)
	assert.Equal(t, "FINAL_ENABLED_MANAGER_REQUIRED", removeRoleRes.Error.Code)

	// 3. Adding a second manager allows deactivating the first
	createMgr2Req := auth.CreateStaffRequest{
		RequestID:   uuid.New(),
		DisplayName: "Quản Lý 02",
		LoginCode:   "QL02",
		Enabled:     true,
		Roles:       []string{auth.RoleManager},
		Pin:         "2222",
		ManagerPin:  "1234",
	}
	recCreateMgr2 := doJSONRequest(e, http.MethodPost, "/api/v1/staff", createMgr2Req, mgrHeaders)
	require.Equal(t, http.StatusCreated, recCreateMgr2.Code)

	// Now deactivating the first manager succeeds
	deactFirstMgrReq := auth.SetStaffEnabledRequest{
		RequestID:       uuid.New(),
		ExpectedEnabled: true,
		Enabled:         false,
		ManagerPin:      "1234",
	}
	recDeactSuccess := doJSONRequest(e, http.MethodPatch, "/api/v1/staff/"+mgr1ID.String()+"/enabled", deactFirstMgrReq, mgrHeaders)
	assert.Equal(t, http.StatusOK, recDeactSuccess.Code)
	deactSuccessRes := parseResponse[auth.StaffDetailResponse](t, recDeactSuccess)
	assert.True(t, deactSuccessRes.Success)
	assert.False(t, deactSuccessRes.Data.Enabled)
}

func TestSessionRevocationOnDeactivation(t *testing.T) {
	e, cleanup := setupTestApp(t)
	defer cleanup()

	// Setup: bootstrap manager
	bootReq := auth.BootstrapManagerRequest{
		DisplayName: "Quản Lý Cửa Hàng",
		LoginCode:   "QL01",
		Pin:         "1234",
	}
	bootRec := doJSONRequest(e, http.MethodPost, "/api/v1/auth/bootstrap", bootReq, nil)
	require.Equal(t, http.StatusCreated, bootRec.Code)

	// Sign in as manager
	signInReq := auth.SignInRequest{
		LoginCode: "QL01",
		Pin:       "1234",
	}
	signInRec := doJSONRequest(e, http.MethodPost, "/api/v1/auth/sign-in", signInReq, nil)
	require.Equal(t, http.StatusOK, signInRec.Code)
	mgrToken := parseResponse[auth.SignInResponse](t, signInRec).Data.Token
	mgrHeaders := map[string]string{"Authorization": "Bearer " + mgrToken}

	// Create cashier staff
	createStaffPayload := auth.CreateStaffRequest{
		RequestID:   uuid.New(),
		DisplayName: "Thu Ngân Viên",
		LoginCode:   "TN99",
		Enabled:     true,
		Roles:       []string{auth.RoleCashier},
		Pin:         "1111",
		ManagerPin:  "1234",
	}
	recCreate := doJSONRequest(e, http.MethodPost, "/api/v1/staff", createStaffPayload, mgrHeaders)
	require.Equal(t, http.StatusCreated, recCreate.Code)
	cashierID := parseResponse[auth.StaffDetailResponse](t, recCreate).Data.ID

	// Cashier signs in to get active session
	cashierSignInRec := doJSONRequest(e, http.MethodPost, "/api/v1/auth/sign-in", auth.SignInRequest{
		LoginCode: "TN99",
		Pin:       "1111",
	}, nil)
	require.Equal(t, http.StatusOK, cashierSignInRec.Code)
	cashierToken := parseResponse[auth.SignInResponse](t, cashierSignInRec).Data.Token
	cashierHeaders := map[string]string{"Authorization": "Bearer " + cashierToken}

	// Verify cashier session is active
	recSessBefore := doJSONRequest(e, http.MethodGet, "/api/v1/auth/session", nil, cashierHeaders)
	assert.Equal(t, http.StatusOK, recSessBefore.Code)
	sessBeforeRes := parseResponse[auth.SessionStateResponse](t, recSessBefore)
	assert.True(t, sessBeforeRes.Success)
	assert.Equal(t, auth.SessionStateAuthenticated, sessBeforeRes.Data.State)

	// Manager deactivates cashier
	deactReq := auth.SetStaffEnabledRequest{
		RequestID:       uuid.New(),
		ExpectedEnabled: true,
		Enabled:         false,
		ManagerPin:      "1234",
	}
	recDeact := doJSONRequest(e, http.MethodPatch, "/api/v1/staff/"+cashierID.String()+"/enabled", deactReq, mgrHeaders)
	assert.Equal(t, http.StatusOK, recDeact.Code)
	deactRes := parseResponse[auth.StaffDetailResponse](t, recDeact)
	assert.True(t, deactRes.Success)
	assert.False(t, deactRes.Data.Enabled)

	// Verify cashier session is immediately revoked (GetSession returns signed_out)
	recSessAfter := doJSONRequest(e, http.MethodGet, "/api/v1/auth/session", nil, cashierHeaders)
	assert.Equal(t, http.StatusOK, recSessAfter.Code)
	sessAfterRes := parseResponse[auth.SessionStateResponse](t, recSessAfter)
	assert.True(t, sessAfterRes.Success)
	assert.Equal(t, "signed_out", sessAfterRes.Data.State)

	// Verify protected endpoint immediately rejects cashier token with 401 Unauthorized
	recProtected := doJSONRequest(e, http.MethodPost, "/api/v1/auth/activity", nil, cashierHeaders)
	assert.Equal(t, http.StatusUnauthorized, recProtected.Code)

	// Verify cashier cannot sign in while deactivated (403 Forbidden)
	recDeactivatedSignIn := doJSONRequest(e, http.MethodPost, "/api/v1/auth/sign-in", auth.SignInRequest{
		LoginCode: "TN99",
		Pin:       "1111",
	}, nil)
	assert.Equal(t, http.StatusForbidden, recDeactivatedSignIn.Code)
}
