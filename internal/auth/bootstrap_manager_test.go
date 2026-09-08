package auth_test

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// === Mock SQL Driver for Bootstrap Manager Tests ===

var (
	testDriverOnce sync.Once
	mockRegistry   = &mockDriverRegistry{
		configs: make(map[string]*mockConnConfig),
	}
)

type mockDriverRegistry struct {
	mu      sync.Mutex
	configs map[string]*mockConnConfig
}

func (r *mockDriverRegistry) set(name string, cfg *mockConnConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.configs[name] = cfg
}

func (r *mockDriverRegistry) get(name string) (*mockConnConfig, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cfg, ok := r.configs[name]
	return cfg, ok
}

type testMockDriver struct{}

func (d *testMockDriver) Open(name string) (driver.Conn, error) {
	cfg, ok := mockRegistry.get(name)
	if !ok {
		return nil, fmt.Errorf("mock configuration not found for: %s", name)
	}
	return &testMockConn{cfg: cfg}, nil
}

type mockConnConfig struct {
	beginErr      error
	lockErr       error
	countManagers int64
	countErr      error
	createErr     error
	createdID     uuid.UUID
	addRoleErr    error
	commitErr     error
	rolledBack    bool
	committed     bool
}

type testMockConn struct {
	cfg *mockConnConfig
}

func (c *testMockConn) Prepare(query string) (driver.Stmt, error) {
	return &testMockStmt{conn: c, query: query}, nil
}

func (c *testMockConn) Close() error {
	return nil
}

func (c *testMockConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *testMockConn) BeginTx(_ context.Context, _ driver.TxOptions) (driver.Tx, error) {
	if c.cfg.beginErr != nil {
		return nil, c.cfg.beginErr
	}
	return &testMockTx{cfg: c.cfg}, nil
}

func (c *testMockConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	if strings.Contains(query, "pg_advisory_xact_lock") {
		if c.cfg.lockErr != nil {
			return nil, c.cfg.lockErr
		}
		return driver.RowsAffected(1), nil
	}
	if strings.Contains(query, "AddStaffRole") || strings.Contains(query, "staff_operational_roles") {
		if c.cfg.addRoleErr != nil {
			return nil, c.cfg.addRoleErr
		}
		return driver.RowsAffected(1), nil
	}
	return driver.RowsAffected(1), nil
}

func (c *testMockConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(query, "CountActiveManagers") || strings.Contains(query, "count(DISTINCT si.id)") {
		if c.cfg.countErr != nil {
			return nil, c.cfg.countErr
		}
		return &testMockRows{
			cols: []string{"count"},
			rows: [][]driver.Value{{c.cfg.countManagers}},
		}, nil
	}

	if strings.Contains(query, "CreateStaffIdentity") || strings.Contains(query, "staff_identities") {
		if c.cfg.createErr != nil {
			return nil, c.cfg.createErr
		}
		id := c.cfg.createdID
		if id == uuid.Nil {
			id = uuid.New()
		}
		var dispName, loginCode string
		if len(args) > 0 {
			if s, ok := args[0].Value.(string); ok {
				dispName = s
			}
		}
		if len(args) > 1 {
			if s, ok := args[1].Value.(string); ok {
				loginCode = strings.ToUpper(strings.TrimSpace(s))
			}
		}
		return &testMockRows{
			cols: []string{"id", "display_name", "login_code", "enabled", "created_at"},
			rows: [][]driver.Value{{id.String(), dispName, loginCode, true, time.Now().UTC()}},
		}, nil
	}

	return nil, fmt.Errorf("unexpected query: %s", query)
}

type testMockStmt struct {
	conn  *testMockConn
	query string
}

func (s *testMockStmt) Close() error { return nil }
func (s *testMockStmt) NumInput() int { return -1 }
func (s *testMockStmt) Exec(args []driver.Value) (driver.Result, error) {
	named := make([]driver.NamedValue, len(args))
	for i, v := range args {
		named[i] = driver.NamedValue{Ordinal: i + 1, Value: v}
	}
	return s.conn.ExecContext(context.Background(), s.query, named)
}
func (s *testMockStmt) Query(args []driver.Value) (driver.Rows, error) {
	named := make([]driver.NamedValue, len(args))
	for i, v := range args {
		named[i] = driver.NamedValue{Ordinal: i + 1, Value: v}
	}
	return s.conn.QueryContext(context.Background(), s.query, named)
}

type testMockTx struct {
	cfg *mockConnConfig
}

func (t *testMockTx) Commit() error {
	t.cfg.committed = true
	if t.cfg.commitErr != nil {
		return t.cfg.commitErr
	}
	return nil
}

func (t *testMockTx) Rollback() error {
	t.cfg.rolledBack = true
	return nil
}

type testMockRows struct {
	cols []string
	rows [][]driver.Value
	pos  int
}

func (r *testMockRows) Columns() []string {
	return r.cols
}

func (r *testMockRows) Close() error {
	return nil
}

func (r *testMockRows) Next(dest []driver.Value) error {
	if r.pos >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.pos])
	r.pos++
	return nil
}

func setupMockDB(t *testing.T, cfg *mockConnConfig) (*sql.DB, *sqlc.Queries) {
	t.Helper()
	testDriverOnce.Do(func() {
		sql.Register("bootstrap-mock-driver", &testMockDriver{})
	})

	dsn := fmt.Sprintf("test-%s", uuid.New().String())
	mockRegistry.set(dsn, cfg)

	db, err := sql.Open("bootstrap-mock-driver", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	return db, sqlc.New(db)
}

// === Tests ===

func TestBootstrapManagerHandler_HandleHTTP_Success(t *testing.T) {
	e := setupEcho()
	managerID := uuid.New()
	cfg := &mockConnConfig{
		countManagers: 0,
		createdID:     managerID,
	}
	db, queries := setupMockDB(t, cfg)
	h := auth.NewBootstrapManagerHandler(db, queries)

	reqBody := `{"display_name":"Quản Lý Trưởng","login_code":"ql01","pin":"123456"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/bootstrap", bytes.NewReader([]byte(reqBody)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := h.HandleHTTP(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp struct {
		Success bool                      `json:"success"`
		Data    auth.StaffProfileResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.True(t, resp.Success)
	assert.Equal(t, managerID, resp.Data.ID)
	assert.Equal(t, "Quản Lý Trưởng", resp.Data.DisplayName)
	assert.Equal(t, "QL01", resp.Data.LoginCode)
	assert.True(t, resp.Data.Enabled)
	assert.Equal(t, []string{auth.RoleManager}, resp.Data.Roles)
	assert.Contains(t, resp.Data.Capabilities, "staff.administer")
	assert.Contains(t, resp.Data.Capabilities, "sales.operate")
	assert.Contains(t, resp.Data.Capabilities, "preparation.operate")

	assert.True(t, cfg.committed)
}

func TestBootstrapManagerHandler_HandleHTTP_Conflict_ManagerAlreadyExists(t *testing.T) {
	e := setupEcho()
	cfg := &mockConnConfig{
		countManagers: 1, // Manager already exists
	}
	db, queries := setupMockDB(t, cfg)
	h := auth.NewBootstrapManagerHandler(db, queries)

	reqBody := `{"display_name":"Quản Lý Mới","login_code":"ql02","pin":"123456"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/bootstrap", bytes.NewReader([]byte(reqBody)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := h.HandleHTTP(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusConflict, rec.Code)

	var resp response.APIResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.False(t, resp.Success)
	assert.Equal(t, "CONFLICT", resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "hệ thống đã có Quản lý được cài đặt")

	assert.True(t, cfg.rolledBack)
	assert.False(t, cfg.committed)
}

func TestBootstrapManagerHandler_AdvisoryLockFailure(t *testing.T) {
	e := setupEcho()
	cfg := &mockConnConfig{
		lockErr: errors.New("lock timeout"),
	}
	db, queries := setupMockDB(t, cfg)
	h := auth.NewBootstrapManagerHandler(db, queries)

	reqBody := `{"display_name":"Quản Lý 1","login_code":"ql01","pin":"123456"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/bootstrap", bytes.NewReader([]byte(reqBody)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := h.HandleHTTP(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)

	var resp response.APIResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.False(t, resp.Success)
	assert.Equal(t, "INTERNAL_ERROR", resp.Error.Code)

	assert.True(t, cfg.rolledBack)
}

func TestBootstrapManagerHandler_CountManagersFailure(t *testing.T) {
	e := setupEcho()
	cfg := &mockConnConfig{
		countErr: errors.New("db query failed"),
	}
	db, queries := setupMockDB(t, cfg)
	h := auth.NewBootstrapManagerHandler(db, queries)

	reqBody := `{"display_name":"Quản Lý 1","login_code":"ql01","pin":"123456"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/bootstrap", bytes.NewReader([]byte(reqBody)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := h.HandleHTTP(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)

	assert.True(t, cfg.rolledBack)
}

func TestBootstrapManagerHandler_CreateStaffIdentityFailure(t *testing.T) {
	e := setupEcho()
	cfg := &mockConnConfig{
		countManagers: 0,
		createErr:     errors.New("insert failed"),
	}
	db, queries := setupMockDB(t, cfg)
	h := auth.NewBootstrapManagerHandler(db, queries)

	reqBody := `{"display_name":"Quản Lý 1","login_code":"ql01","pin":"123456"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/bootstrap", bytes.NewReader([]byte(reqBody)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := h.HandleHTTP(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)

	assert.True(t, cfg.rolledBack)
}

func TestBootstrapManagerHandler_CreateStaffIdentityUniqueViolation(t *testing.T) {
	e := setupEcho()
	cfg := &mockConnConfig{
		countManagers: 0,
		createErr:     &pgconn.PgError{Code: "23505"},
	}
	db, queries := setupMockDB(t, cfg)
	h := auth.NewBootstrapManagerHandler(db, queries)

	reqBody := `{"display_name":"Quản Lý Trùng","login_code":"ql01","pin":"123456"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/bootstrap", bytes.NewReader([]byte(reqBody)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := h.HandleHTTP(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusConflict, rec.Code)

	var resp response.APIResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.False(t, resp.Success)
	assert.Equal(t, "CONFLICT", resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "mã đăng nhập đã được sử dụng")

	assert.True(t, cfg.rolledBack)
	assert.False(t, cfg.committed)
}

func TestBootstrapManagerHandler_AddStaffRoleFailure(t *testing.T) {
	e := setupEcho()
	cfg := &mockConnConfig{
		countManagers: 0,
		addRoleErr:    errors.New("add role failed"),
	}
	db, queries := setupMockDB(t, cfg)
	h := auth.NewBootstrapManagerHandler(db, queries)

	reqBody := `{"display_name":"Quản Lý 1","login_code":"ql01","pin":"123456"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/bootstrap", bytes.NewReader([]byte(reqBody)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := h.HandleHTTP(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)

	assert.True(t, cfg.rolledBack)
}

func TestBootstrapManagerHandler_CommitFailure(t *testing.T) {
	e := setupEcho()
	cfg := &mockConnConfig{
		countManagers: 0,
		commitErr:     errors.New("commit failed"),
	}
	db, queries := setupMockDB(t, cfg)
	h := auth.NewBootstrapManagerHandler(db, queries)

	reqBody := `{"display_name":"Quản Lý 1","login_code":"ql01","pin":"123456"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/bootstrap", bytes.NewReader([]byte(reqBody)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := h.HandleHTTP(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestBootstrapManagerHandler_BeginTxFailure(t *testing.T) {
	e := setupEcho()
	cfg := &mockConnConfig{
		beginErr: errors.New("connection refused"),
	}
	db, queries := setupMockDB(t, cfg)
	h := auth.NewBootstrapManagerHandler(db, queries)

	reqBody := `{"display_name":"Quản Lý 1","login_code":"ql01","pin":"123456"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/bootstrap", bytes.NewReader([]byte(reqBody)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := h.HandleHTTP(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestBootstrapManagerHandler_DirectHandle(t *testing.T) {
	t.Run("invalid PIN returns ErrInvalid", func(t *testing.T) {
		h := auth.NewBootstrapManagerHandler(nil, nil)
		_, err := h.Handle(context.Background(), auth.BootstrapManagerRequest{
			DisplayName: "Manager",
			LoginCode:   "MGR01",
			Pin:         "12", // too short
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, response.ErrInvalid))
	})

	t.Run("conflict returns ErrConflict", func(t *testing.T) {
		cfg := &mockConnConfig{countManagers: 1}
		db, queries := setupMockDB(t, cfg)
		h := auth.NewBootstrapManagerHandler(db, queries)

		_, err := h.Handle(context.Background(), auth.BootstrapManagerRequest{
			DisplayName: "Manager",
			LoginCode:   "MGR01",
			Pin:         "123456",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, response.ErrConflict))
		assert.Contains(t, err.Error(), "hệ thống đã có Quản lý được cài đặt")
	})

	t.Run("login code unique violation returns ErrConflict", func(t *testing.T) {
		cfg := &mockConnConfig{
			countManagers: 0,
			createErr:     &pgconn.PgError{Code: "23505"},
		}
		db, queries := setupMockDB(t, cfg)
		h := auth.NewBootstrapManagerHandler(db, queries)

		_, err := h.Handle(context.Background(), auth.BootstrapManagerRequest{
			DisplayName: "Manager",
			LoginCode:   "MGR01",
			Pin:         "123456",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, response.ErrConflict))
		assert.Contains(t, err.Error(), "mã đăng nhập đã được sử dụng")
	})
}

func TestBootstrapManagerHandler_PayloadValidation(t *testing.T) {
	e := setupEcho()
	h := auth.NewBootstrapManagerHandler(nil, nil)

	tests := []struct {
		name        string
		body        string
		expectCode  int
		expectedErr string
	}{
		{
			name:        "malformed json",
			body:        `{invalid-json}`,
			expectCode:  http.StatusBadRequest,
			expectedErr: "invalid payload",
		},
		{
			name:        "empty object",
			body:        `{}`,
			expectCode:  http.StatusBadRequest,
			expectedErr: "displayname",
		},
		{
			name:        "missing display_name",
			body:        `{"login_code":"MGR01","pin":"1234"}`,
			expectCode:  http.StatusBadRequest,
			expectedErr: "displayname",
		},
		{
			name:        "display_name too short (1 char)",
			body:        `{"display_name":"A","login_code":"MGR01","pin":"1234"}`,
			expectCode:  http.StatusBadRequest,
			expectedErr: "displayname",
		},
		{
			name:        "display_name too long (121 chars)",
			body:        fmt.Sprintf(`{"display_name":"%s","login_code":"MGR01","pin":"1234"}`, strings.Repeat("A", 121)),
			expectCode:  http.StatusBadRequest,
			expectedErr: "displayname",
		},
		{
			name:        "missing login_code",
			body:        `{"display_name":"Manager","pin":"1234"}`,
			expectCode:  http.StatusBadRequest,
			expectedErr: "logincode",
		},
		{
			name:        "login_code too short (1 char)",
			body:        `{"display_name":"Manager","login_code":"M","pin":"1234"}`,
			expectCode:  http.StatusBadRequest,
			expectedErr: "logincode",
		},
		{
			name:        "login_code too long (25 chars)",
			body:        `{"display_name":"Manager","login_code":"ABCDEFGHIJKLM1234567890123","pin":"1234"}`,
			expectCode:  http.StatusBadRequest,
			expectedErr: "logincode",
		},
		{
			name:        "login_code non-alphanumeric with space",
			body:        `{"display_name":"Manager","login_code":"MGR 01","pin":"1234"}`,
			expectCode:  http.StatusBadRequest,
			expectedErr: "logincode",
		},
		{
			name:        "login_code non-alphanumeric with hyphen",
			body:        `{"display_name":"Manager","login_code":"MGR-01","pin":"1234"}`,
			expectCode:  http.StatusBadRequest,
			expectedErr: "logincode",
		},
		{
			name:        "missing pin",
			body:        `{"display_name":"Manager","login_code":"MGR01"}`,
			expectCode:  http.StatusBadRequest,
			expectedErr: "pin",
		},
		{
			name:        "pin too short (3 digits)",
			body:        `{"display_name":"Manager","login_code":"MGR01","pin":"123"}`,
			expectCode:  http.StatusBadRequest,
			expectedErr: "pin",
		},
		{
			name:        "pin too long (9 digits)",
			body:        `{"display_name":"Manager","login_code":"MGR01","pin":"123456789"}`,
			expectCode:  http.StatusBadRequest,
			expectedErr: "pin",
		},
		{
			name:        "pin non-numeric",
			body:        `{"display_name":"Manager","login_code":"MGR01","pin":"123a"}`,
			expectCode:  http.StatusBadRequest,
			expectedErr: "pin",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/bootstrap", bytes.NewReader([]byte(tc.body)))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			err := h.HandleHTTP(c)
			require.NoError(t, err)
			assert.Equal(t, tc.expectCode, rec.Code)

			var resp response.APIResponse
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
			assert.False(t, resp.Success)
			assert.Equal(t, "BAD_REQUEST", resp.Error.Code)
			if tc.expectedErr != "" {
				assert.Contains(t, resp.Error.Message, tc.expectedErr)
			}
		})
	}
}
