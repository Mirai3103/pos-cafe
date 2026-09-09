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

// === Mock Driver for Staff Administration Tests ===

var (
	staffDriverOnce sync.Once
	staffRegistry   = &staffDriverRegistry{
		configs: make(map[string]*staffMockConfig),
	}
)

type staffDriverRegistry struct {
	mu      sync.Mutex
	configs map[string]*staffMockConfig
}

func (r *staffDriverRegistry) set(name string, cfg *staffMockConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.configs[name] = cfg
}

func (r *staffDriverRegistry) get(name string) (*staffMockConfig, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cfg, ok := r.configs[name]
	return cfg, ok
}

type staffMockDriver struct{}

func (d *staffMockDriver) Open(name string) (driver.Conn, error) {
	cfg, ok := staffRegistry.get(name)
	if !ok {
		return nil, fmt.Errorf("mock configuration not found for: %s", name)
	}
	return &staffMockConn{cfg: cfg}, nil
}

type staffMockConfig struct {
	mu sync.Mutex

	// Transaction & Lock controls
	beginErr           error
	commitErr          error
	lockErr            error
	advisoryLockCalled bool
	committed          bool
	rolledBack         bool

	// Actor lookup
	actorID       uuid.UUID
	actorStaff    *sqlc.StaffIdentity
	actorStaffErr error

	// Target lookup
	targetID       uuid.UUID
	targetStaff    *sqlc.StaffIdentity
	targetStaffErr error
	targetRoles    []string
	targetRolesErr error

	// Active managers
	activeManagers    int64
	activeManagersErr error

	// Staff create
	createdStaff   *sqlc.CreateStaffIdentityRow
	createStaffErr error
	rolesAdded     []string
	addRoleErr     error

	// Staff replace roles
	rolesClearedFor uuid.UUID
	clearRolesErr   error

	// Staff set enabled
	setEnabledErr error

	// Staff reset pin
	pinUpdatedFor uuid.UUID
	updatePinErr  error

	// Session revocation
	sessionsRevokedFor uuid.UUID
	revokeSessionsErr  error

	// Staff list
	listAllStaff         []sqlc.ListAllStaffRow
	listAllStaffErr      error
	listAllStaffRoles    []sqlc.StaffOperationalRole
	listAllStaffRolesErr error

	// Idempotency in-memory store
	idempKeys map[string]storedKey
}

func (c *staffMockConfig) getIdempKey(actorID, key uuid.UUID) (storedKey, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	k, ok := c.idempKeys[actorID.String()+":"+key.String()]
	return k, ok
}

func (c *staffMockConfig) setIdempKey(k storedKey) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.idempKeys == nil {
		c.idempKeys = make(map[string]storedKey)
	}
	c.idempKeys[k.ActorID.String()+":"+k.Key.String()] = k
}

type staffMockConn struct {
	cfg *staffMockConfig
}

func (c *staffMockConn) Prepare(query string) (driver.Stmt, error) {
	return &staffMockStmt{conn: c, query: query}, nil
}

func (c *staffMockConn) Close() error { return nil }

func (c *staffMockConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *staffMockConn) BeginTx(_ context.Context, _ driver.TxOptions) (driver.Tx, error) {
	if c.cfg.beginErr != nil {
		return nil, c.cfg.beginErr
	}
	return &staffMockTx{cfg: c.cfg}, nil
}

type staffMockTx struct {
	cfg *staffMockConfig
}

func (t *staffMockTx) Commit() error {
	t.cfg.committed = true
	if t.cfg.commitErr != nil {
		return t.cfg.commitErr
	}
	return nil
}

func (t *staffMockTx) Rollback() error {
	t.cfg.rolledBack = true
	return nil
}

type staffMockStmt struct {
	conn  *staffMockConn
	query string
}

func (s *staffMockStmt) Close() error  { return nil }
func (s *staffMockStmt) NumInput() int { return -1 }

func (s *staffMockStmt) Exec(args []driver.Value) (driver.Result, error) {
	cfg := s.conn.cfg

	if strings.Contains(s.query, "pg_advisory_xact_lock") {
		cfg.advisoryLockCalled = true
		if cfg.lockErr != nil {
			return nil, cfg.lockErr
		}
		return driver.RowsAffected(1), nil
	}

	if strings.Contains(s.query, "InsertIdempotencyKey") || strings.Contains(s.query, "idempotency_keys") {
		key, _ := uuid.Parse(fmt.Sprint(args[0]))
		actorID, _ := uuid.Parse(fmt.Sprint(args[1]))
		action := fmt.Sprint(args[2])
		hash := fmt.Sprint(args[3])
		code := int32(args[4].(int64))
		var body []byte
		switch b := args[5].(type) {
		case []byte:
			body = b
		case string:
			body = []byte(b)
		}
		cfg.setIdempKey(storedKey{
			Key:          key,
			ActorID:      actorID,
			Action:       action,
			RequestHash:  hash,
			ResponseCode: code,
			ResponseBody: body,
			CreatedAt:    time.Now().UTC(),
		})
		return driver.RowsAffected(1), nil
	}

	if strings.Contains(s.query, "AddStaffRole") || strings.Contains(s.query, "staff_operational_roles") && strings.Contains(s.query, "INSERT") {
		if cfg.addRoleErr != nil {
			return nil, cfg.addRoleErr
		}
		if len(args) > 1 {
			cfg.rolesAdded = append(cfg.rolesAdded, fmt.Sprint(args[1]))
		}
		return driver.RowsAffected(1), nil
	}

	if strings.Contains(s.query, "ClearStaffRoles") || strings.Contains(s.query, "DELETE FROM staff_operational_roles") {
		if cfg.clearRolesErr != nil {
			return nil, cfg.clearRolesErr
		}
		if len(args) > 0 {
			cfg.rolesClearedFor, _ = uuid.Parse(fmt.Sprint(args[0]))
		}
		return driver.RowsAffected(1), nil
	}

	if strings.Contains(s.query, "UpdateStaffPin") || strings.Contains(s.query, "pin_hash = $2") {
		if cfg.updatePinErr != nil {
			return nil, cfg.updatePinErr
		}
		if len(args) > 0 {
			cfg.pinUpdatedFor, _ = uuid.Parse(fmt.Sprint(args[0]))
		}
		return driver.RowsAffected(1), nil
	}

	if strings.Contains(s.query, "RevokeAllStaffSessions") || strings.Contains(s.query, "staff_access_sessions") {
		if cfg.revokeSessionsErr != nil {
			return nil, cfg.revokeSessionsErr
		}
		if len(args) > 0 {
			cfg.sessionsRevokedFor, _ = uuid.Parse(fmt.Sprint(args[0]))
		}
		return driver.RowsAffected(1), nil
	}

	return driver.RowsAffected(1), nil
}

func (s *staffMockStmt) Query(args []driver.Value) (driver.Rows, error) {
	cfg := s.conn.cfg

	if strings.Contains(s.query, "GetIdempotencyKey") || strings.Contains(s.query, "idempotency_keys") {
		actorID, _ := uuid.Parse(fmt.Sprint(args[0]))
		key, _ := uuid.Parse(fmt.Sprint(args[1]))
		k, found := cfg.getIdempKey(actorID, key)
		if !found {
			return &staffMockRows{cols: []string{"key", "actor_id", "action", "request_hash", "response_code", "response_body", "created_at"}, rows: [][]driver.Value{}}, nil
		}
		return &staffMockRows{
			cols: []string{"key", "actor_id", "action", "request_hash", "response_code", "response_body", "created_at"},
			rows: [][]driver.Value{
				{k.Key.String(), k.ActorID.String(), k.Action, k.RequestHash, k.ResponseCode, k.ResponseBody, k.CreatedAt},
			},
		}, nil
	}

	if strings.Contains(s.query, "GetStaffByID") || strings.Contains(s.query, "FROM staff_identities") && strings.Contains(s.query, "WHERE id = $1") {
		queryID, _ := uuid.Parse(fmt.Sprint(args[0]))
		if cfg.actorStaff != nil && queryID == cfg.actorID {
			if cfg.actorStaffErr != nil {
				return nil, cfg.actorStaffErr
			}
			return &staffMockRows{
				cols: []string{"id", "display_name", "login_code", "pin_hash", "enabled", "created_at"},
				rows: [][]driver.Value{{cfg.actorStaff.ID.String(), cfg.actorStaff.DisplayName, cfg.actorStaff.LoginCode, cfg.actorStaff.PinHash, cfg.actorStaff.Enabled, cfg.actorStaff.CreatedAt}},
			}, nil
		}

		if cfg.targetStaffErr != nil {
			return nil, cfg.targetStaffErr
		}
		if cfg.targetStaff == nil {
			return &staffMockRows{cols: []string{"id", "display_name", "login_code", "pin_hash", "enabled", "created_at"}, rows: [][]driver.Value{}}, nil
		}
		return &staffMockRows{
			cols: []string{"id", "display_name", "login_code", "pin_hash", "enabled", "created_at"},
			rows: [][]driver.Value{{cfg.targetStaff.ID.String(), cfg.targetStaff.DisplayName, cfg.targetStaff.LoginCode, cfg.targetStaff.PinHash, cfg.targetStaff.Enabled, cfg.targetStaff.CreatedAt}},
		}, nil
	}

	if strings.Contains(s.query, "CountActiveManagers") || strings.Contains(s.query, "count(DISTINCT si.id)") {
		if cfg.activeManagersErr != nil {
			return nil, cfg.activeManagersErr
		}
		return &staffMockRows{
			cols: []string{"count"},
			rows: [][]driver.Value{{cfg.activeManagers}},
		}, nil
	}

	if strings.Contains(s.query, "GetStaffRoles") || strings.Contains(s.query, "FROM staff_operational_roles") && strings.Contains(s.query, "staff_identity_id = $1") {
		if cfg.targetRolesErr != nil {
			return nil, cfg.targetRolesErr
		}
		rows := make([][]driver.Value, len(cfg.targetRoles))
		for i, r := range cfg.targetRoles {
			rows[i] = []driver.Value{r}
		}
		return &staffMockRows{
			cols: []string{"role"},
			rows: rows,
		}, nil
	}

	if strings.Contains(s.query, "CreateStaffIdentity") {
		if cfg.createStaffErr != nil {
			return nil, cfg.createStaffErr
		}
		res := cfg.createdStaff
		if res == nil {
			res = &sqlc.CreateStaffIdentityRow{
				ID:          uuid.New(),
				DisplayName: fmt.Sprint(args[0]),
				LoginCode:   fmt.Sprint(args[1]),
				Enabled:     args[3].(bool),
				CreatedAt:   time.Now().UTC(),
			}
		}
		return &staffMockRows{
			cols: []string{"id", "display_name", "login_code", "enabled", "created_at"},
			rows: [][]driver.Value{{res.ID.String(), res.DisplayName, res.LoginCode, res.Enabled, res.CreatedAt}},
		}, nil
	}

	if strings.Contains(s.query, "SetStaffEnabled") {
		if cfg.setEnabledErr != nil {
			return nil, cfg.setEnabledErr
		}
		id := fmt.Sprint(args[0])
		enabled := args[1].(bool)
		dispName := "Updated Name"
		loginCode := "LOGIN"
		if cfg.targetStaff != nil {
			dispName = cfg.targetStaff.DisplayName
			loginCode = cfg.targetStaff.LoginCode
		}
		return &staffMockRows{
			cols: []string{"id", "display_name", "login_code", "enabled"},
			rows: [][]driver.Value{{id, dispName, loginCode, enabled}},
		}, nil
	}

	if strings.Contains(s.query, "ListAllStaffRoles") || strings.Contains(s.query, "FROM staff_operational_roles") && strings.Contains(s.query, "ORDER BY staff_identity_id") {
		if cfg.listAllStaffRolesErr != nil {
			return nil, cfg.listAllStaffRolesErr
		}
		rows := make([][]driver.Value, len(cfg.listAllStaffRoles))
		for i, r := range cfg.listAllStaffRoles {
			rows[i] = []driver.Value{r.StaffIdentityID.String(), r.Role}
		}
		return &staffMockRows{
			cols: []string{"staff_identity_id", "role"},
			rows: rows,
		}, nil
	}

	if strings.Contains(s.query, "ListAllStaff") {
		if cfg.listAllStaffErr != nil {
			return nil, cfg.listAllStaffErr
		}
		rows := make([][]driver.Value, len(cfg.listAllStaff))
		for i, s := range cfg.listAllStaff {
			rows[i] = []driver.Value{s.ID.String(), s.DisplayName, s.LoginCode, s.Enabled, s.CreatedAt}
		}
		return &staffMockRows{
			cols: []string{"id", "display_name", "login_code", "enabled", "created_at"},
			rows: rows,
		}, nil
	}

	return nil, fmt.Errorf("unexpected query: %s", s.query)
}

type staffMockRows struct {
	cols []string
	rows [][]driver.Value
	pos  int
}

func (r *staffMockRows) Columns() []string { return r.cols }
func (r *staffMockRows) Close() error      { return nil }
func (r *staffMockRows) Next(dest []driver.Value) error {
	if r.pos >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.pos])
	r.pos++
	return nil
}

func setupStaffDB(t *testing.T, cfg *staffMockConfig) (*sql.DB, *sqlc.Queries) {
	t.Helper()
	staffDriverOnce.Do(func() {
		sql.Register("staff-mock-driver", &staffMockDriver{})
	})

	dsn := fmt.Sprintf("staff-test-%s", uuid.New().String())
	staffRegistry.set(dsn, cfg)

	db, err := sql.Open("staff-mock-driver", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	return db, sqlc.New(db)
}

func createActorManager(pin string) (uuid.UUID, *sqlc.StaffIdentity, *auth.StaffClaims) {
	actorID := uuid.New()
	pinHash, _ := auth.HashPin(pin)
	identity := &sqlc.StaffIdentity{
		ID:          actorID,
		DisplayName: "Admin Manager",
		LoginCode:   "MGR01",
		PinHash:     pinHash,
		Enabled:     true,
		CreatedAt:   time.Now().UTC(),
	}
	claims := &auth.StaffClaims{
		StaffID:      actorID,
		DisplayName:  "Admin Manager",
		LoginCode:    "MGR01",
		Roles:        []string{auth.RoleManager},
		Capabilities: auth.DeriveCapabilities([]string{auth.RoleManager}),
	}
	return actorID, identity, claims
}

// === Staff Me Handler Tests ===

func TestStaffMeHandler(t *testing.T) {
	e := setupEcho()
	mockQ := &mockAuthQuerier{
		getStaffByIDFunc: func(_ context.Context, id uuid.UUID) (sqlc.StaffIdentity, error) {
			return sqlc.StaffIdentity{Enabled: true}, nil
		},
	}
	h := auth.NewStaffMeHandler(mockQ)

	t.Run("returns 403 Forbidden when unauthenticated", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/staff/me", nil)
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

	t.Run("returns profile response for authenticated staff", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/staff/me", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		staffID := uuid.New()
		c.Set(auth.StaffContextKey, &auth.StaffClaims{
			StaffID:      staffID,
			DisplayName:  "Alice Cashier",
			LoginCode:    "ALICE",
			Roles:        []string{auth.RoleCashier},
			Capabilities: []string{"sales.operate"},
		})

		err := h.HandleHTTP(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)

		var resp struct {
			Success bool                      `json:"success"`
			Data    auth.StaffProfileResponse `json:"data"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.True(t, resp.Success)
		assert.Equal(t, staffID, resp.Data.ID)
		assert.Equal(t, "Alice Cashier", resp.Data.DisplayName)
		assert.Equal(t, "ALICE", resp.Data.LoginCode)
		assert.True(t, resp.Data.Enabled)
		assert.Equal(t, []string{auth.RoleCashier}, resp.Data.Roles)
		assert.Equal(t, []string{"sales.operate"}, resp.Data.Capabilities)
	})
}

// === Staff List Handler Tests ===

func TestStaffListHandler(t *testing.T) {
	e := setupEcho()

	t.Run("returns full staff list with aggregated roles", func(t *testing.T) {
		s1 := uuid.New()
		s2 := uuid.New()

		cfg := &staffMockConfig{
			listAllStaff: []sqlc.ListAllStaffRow{
				{ID: s1, DisplayName: "Alice", LoginCode: "ALICE", Enabled: true, CreatedAt: time.Now().UTC()},
				{ID: s2, DisplayName: "Bob", LoginCode: "BOB", Enabled: false, CreatedAt: time.Now().UTC()},
			},
			listAllStaffRoles: []sqlc.StaffOperationalRole{
				{StaffIdentityID: s1, Role: auth.RoleCashier},
				{StaffIdentityID: s1, Role: auth.RoleBarista},
			},
		}
		_, queries := setupStaffDB(t, cfg)
		h := auth.NewStaffListHandler(queries)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/staff", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		err := h.HandleHTTP(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)

		var resp struct {
			Success bool                       `json:"success"`
			Data    []auth.StaffDetailResponse `json:"data"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.True(t, resp.Success)
		require.Len(t, resp.Data, 2)

		assert.Equal(t, s1, resp.Data[0].ID)
		assert.Equal(t, []string{auth.RoleCashier, auth.RoleBarista}, resp.Data[0].Roles)

		assert.Equal(t, s2, resp.Data[1].ID)
		assert.Equal(t, []string{}, resp.Data[1].Roles)
	})

	t.Run("handles empty list without error", func(t *testing.T) {
		cfg := &staffMockConfig{
			listAllStaff:      []sqlc.ListAllStaffRow{},
			listAllStaffRoles: []sqlc.StaffOperationalRole{},
		}
		_, queries := setupStaffDB(t, cfg)
		h := auth.NewStaffListHandler(queries)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/staff", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		err := h.HandleHTTP(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("returns 500 when list all staff fails", func(t *testing.T) {
		cfg := &staffMockConfig{
			listAllStaffErr: errors.New("db disconnect"),
		}
		_, queries := setupStaffDB(t, cfg)
		h := auth.NewStaffListHandler(queries)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/staff", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		err := h.HandleHTTP(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}

// === Staff Create Handler Tests ===

func TestStaffCreateHandler(t *testing.T) {
	e := setupEcho()
	mgrPin := "123456"
	actorID, actorIdentity, actorClaims := createActorManager(mgrPin)

	validReq := auth.CreateStaffRequest{
		RequestID:   uuid.New(),
		DisplayName: "Nguyễn Văn A",
		LoginCode:   "nva",
		Enabled:     true,
		Roles:       []string{auth.RoleCashier},
		Pin:         "4321",
		ManagerPin:  mgrPin,
	}

	t.Run("unauthorized when staff claims missing", func(t *testing.T) {
		h := auth.NewStaffCreateHandler(nil, nil)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/staff", strings.NewReader(`{}`))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		err := h.HandleHTTP(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("validation failure returns 400 Bad Request", func(t *testing.T) {
		h := auth.NewStaffCreateHandler(nil, nil)
		body := `{"display_name":"","pin":"12"}` // missing required fields, invalid pin
		req := httptest.NewRequest(http.MethodPost, "/api/v1/staff", strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.Set(auth.StaffContextKey, actorClaims)

		err := h.HandleHTTP(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusBadRequest, rec.Code)

		var resp response.APIResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.Equal(t, "BAD_REQUEST", resp.Error.Code)
	})

	t.Run("incorrect manager PIN returns 403 Forbidden", func(t *testing.T) {
		cfg := &staffMockConfig{
			actorID:    actorID,
			actorStaff: actorIdentity,
		}
		db, queries := setupStaffDB(t, cfg)
		h := auth.NewStaffCreateHandler(db, queries)

		badReq := validReq
		badReq.ManagerPin = "999999" // wrong manager pin
		b, _ := json.Marshal(badReq)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/staff", bytes.NewReader(b))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.Set(auth.StaffContextKey, actorClaims)

		err := h.HandleHTTP(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusForbidden, rec.Code)

		var resp response.APIResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.Contains(t, resp.Error.Message, "PIN Quản lý không đúng")
	})

	t.Run("login code conflict (unique violation 23505) returns 409 Conflict", func(t *testing.T) {
		cfg := &staffMockConfig{
			actorID:        actorID,
			actorStaff:     actorIdentity,
			createStaffErr: &pgconn.PgError{Code: "23505"},
		}
		db, queries := setupStaffDB(t, cfg)
		h := auth.NewStaffCreateHandler(db, queries)

		b, _ := json.Marshal(validReq)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/staff", bytes.NewReader(b))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.Set(auth.StaffContextKey, actorClaims)

		err := h.HandleHTTP(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusConflict, rec.Code)

		var resp response.APIResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.Equal(t, "CONFLICT", resp.Error.Code)
		assert.Contains(t, resp.Error.Message, "mã đăng nhập đã được sử dụng")
	})

	t.Run("successful staff creation returns 201 Created", func(t *testing.T) {
		newStaffID := uuid.New()
		cfg := &staffMockConfig{
			actorID:    actorID,
			actorStaff: actorIdentity,
			createdStaff: &sqlc.CreateStaffIdentityRow{
				ID:          newStaffID,
				DisplayName: validReq.DisplayName,
				LoginCode:   "NVA",
				Enabled:     true,
				CreatedAt:   time.Now().UTC(),
			},
		}
		db, queries := setupStaffDB(t, cfg)
		h := auth.NewStaffCreateHandler(db, queries)

		b, _ := json.Marshal(validReq)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/staff", bytes.NewReader(b))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.Set(auth.StaffContextKey, actorClaims)

		err := h.HandleHTTP(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusCreated, rec.Code)

		var resp struct {
			Success bool                     `json:"success"`
			Data    auth.StaffDetailResponse `json:"data"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.True(t, resp.Success)
		assert.Equal(t, newStaffID, resp.Data.ID)
		assert.Equal(t, "NVA", resp.Data.LoginCode)
		assert.Equal(t, []string{auth.RoleCashier}, resp.Data.Roles)
		assert.True(t, cfg.committed)

		// Idempotent replay with exact same request returns cached 201
		rec2 := httptest.NewRecorder()
		c2 := e.NewContext(httptest.NewRequest(http.MethodPost, "/api/v1/staff", bytes.NewReader(b)), rec2)
		c2.Request().Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		c2.Set(auth.StaffContextKey, actorClaims)

		err = h.HandleHTTP(c2)
		require.NoError(t, err)
		assert.Equal(t, http.StatusCreated, rec2.Code)

		var resp2 struct {
			Success bool                     `json:"success"`
			Data    auth.StaffDetailResponse `json:"data"`
		}
		require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &resp2))
		assert.Equal(t, newStaffID, resp2.Data.ID)
	})

	t.Run("idempotency conflict returns 409 Conflict", func(t *testing.T) {
		reqID := uuid.New()
		cfg := &staffMockConfig{
			actorID:    actorID,
			actorStaff: actorIdentity,
		}
		db, queries := setupStaffDB(t, cfg)
		h := auth.NewStaffCreateHandler(db, queries)

		req1 := validReq
		req1.RequestID = reqID
		req1.DisplayName = "Original Name"

		b1, _ := json.Marshal(req1)
		rec1 := httptest.NewRecorder()
		c1 := e.NewContext(httptest.NewRequest(http.MethodPost, "/api/v1/staff", bytes.NewReader(b1)), rec1)
		c1.Request().Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		c1.Set(auth.StaffContextKey, actorClaims)

		err := h.HandleHTTP(c1)
		require.NoError(t, err)
		assert.Equal(t, http.StatusCreated, rec1.Code)

		// Same RequestID, different DisplayName
		req2 := validReq
		req2.RequestID = reqID
		req2.DisplayName = "Different Name"

		b2, _ := json.Marshal(req2)
		rec2 := httptest.NewRecorder()
		c2 := e.NewContext(httptest.NewRequest(http.MethodPost, "/api/v1/staff", bytes.NewReader(b2)), rec2)
		c2.Request().Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		c2.Set(auth.StaffContextKey, actorClaims)

		err = h.HandleHTTP(c2)
		require.NoError(t, err)
		assert.Equal(t, http.StatusConflict, rec2.Code)

		var resp response.APIResponse
		require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &resp))
		assert.Contains(t, resp.Error.Message, "mã yêu cầu (request_id) đã được dùng cho payload khác")
	})
}

// === Staff Set Enabled Handler Tests ===

func TestStaffSetEnabledHandler(t *testing.T) {
	e := setupEcho()
	mgrPin := "123456"
	actorID, actorIdentity, actorClaims := createActorManager(mgrPin)

	targetID := uuid.New()
	targetIdentity := &sqlc.StaffIdentity{
		ID:          targetID,
		DisplayName: "Bob Barista",
		LoginCode:   "BOB",
		PinHash:     "hash",
		Enabled:     true,
		CreatedAt:   time.Now().UTC(),
	}

	t.Run("returns 400 when req.Enabled == req.ExpectedEnabled", func(t *testing.T) {
		h := auth.NewStaffSetEnabledHandler(nil, nil)
		body := fmt.Sprintf(`{"request_id":"%s","expected_enabled":true,"enabled":true,"manager_pin":"%s"}`, uuid.New(), mgrPin)
		req := httptest.NewRequest(http.MethodPut, "/api/v1/staff/"+targetID.String()+"/enabled", strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues(targetID.String())
		c.Set(auth.StaffContextKey, actorClaims)

		err := h.HandleHTTP(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusBadRequest, rec.Code)

		var resp response.APIResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.Contains(t, resp.Error.Message, "trạng thái mới phải khác trạng thái hiện tại")
	})

	t.Run("returns 403 when manager PIN incorrect", func(t *testing.T) {
		cfg := &staffMockConfig{
			actorID:    actorID,
			actorStaff: actorIdentity,
		}
		db, queries := setupStaffDB(t, cfg)
		h := auth.NewStaffSetEnabledHandler(db, queries)

		body := fmt.Sprintf(`{"request_id":"%s","expected_enabled":true,"enabled":false,"manager_pin":"000000"}`, uuid.New())
		req := httptest.NewRequest(http.MethodPut, "/api/v1/staff/"+targetID.String()+"/enabled", strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues(targetID.String())
		c.Set(auth.StaffContextKey, actorClaims)

		err := h.HandleHTTP(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusForbidden, rec.Code)

		var resp response.APIResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.Contains(t, resp.Error.Message, "PIN Quản lý không đúng")
	})

	t.Run("returns 404 when target staff not found", func(t *testing.T) {
		cfg := &staffMockConfig{
			actorID:     actorID,
			actorStaff:  actorIdentity,
			targetID:    targetID,
			targetStaff: nil, // Not found
		}
		db, queries := setupStaffDB(t, cfg)
		h := auth.NewStaffSetEnabledHandler(db, queries)

		body := fmt.Sprintf(`{"request_id":"%s","expected_enabled":true,"enabled":false,"manager_pin":"%s"}`, uuid.New(), mgrPin)
		req := httptest.NewRequest(http.MethodPut, "/api/v1/staff/"+targetID.String()+"/enabled", strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues(targetID.String())
		c.Set(auth.StaffContextKey, actorClaims)

		err := h.HandleHTTP(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusNotFound, rec.Code)

		var resp response.APIResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.Contains(t, resp.Error.Message, "không tìm thấy nhân viên")
	})

	t.Run("returns 409 Conflict when target.Enabled != req.ExpectedEnabled (state changed)", func(t *testing.T) {
		targetCopy := *targetIdentity
		targetCopy.Enabled = false // Already disabled in DB
		cfg := &staffMockConfig{
			actorID:     actorID,
			actorStaff:  actorIdentity,
			targetID:    targetID,
			targetStaff: &targetCopy,
		}
		db, queries := setupStaffDB(t, cfg)
		h := auth.NewStaffSetEnabledHandler(db, queries)

		// Expecting true, but DB is false
		body := fmt.Sprintf(`{"request_id":"%s","expected_enabled":true,"enabled":false,"manager_pin":"%s"}`, uuid.New(), mgrPin)
		req := httptest.NewRequest(http.MethodPut, "/api/v1/staff/"+targetID.String()+"/enabled", strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues(targetID.String())
		c.Set(auth.StaffContextKey, actorClaims)

		err := h.HandleHTTP(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusConflict, rec.Code)

		var resp response.APIResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.Contains(t, resp.Error.Message, "trạng thái nhân viên đã thay đổi, vui lòng tải lại")
	})

	t.Run("disabling last active manager returns 409 Conflict", func(t *testing.T) {
		cfg := &staffMockConfig{
			actorID:        actorID,
			actorStaff:     actorIdentity,
			targetID:       targetID,
			targetStaff:    targetIdentity,
			targetRoles:    []string{auth.RoleManager},
			activeManagers: 1, // Only 1 active manager!
		}
		db, queries := setupStaffDB(t, cfg)
		h := auth.NewStaffSetEnabledHandler(db, queries)

		body := fmt.Sprintf(`{"request_id":"%s","expected_enabled":true,"enabled":false,"manager_pin":"%s"}`, uuid.New(), mgrPin)
		req := httptest.NewRequest(http.MethodPut, "/api/v1/staff/"+targetID.String()+"/enabled", strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues(targetID.String())
		c.Set(auth.StaffContextKey, actorClaims)

		err := h.HandleHTTP(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusConflict, rec.Code)

		var resp response.APIResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.Contains(t, resp.Error.Message, "phải còn ít nhất một Quản lý đang hoạt động")
	})

	t.Run("disabling staff revokes active sessions and uses advisory lock", func(t *testing.T) {
		cfg := &staffMockConfig{
			actorID:        actorID,
			actorStaff:     actorIdentity,
			targetID:       targetID,
			targetStaff:    targetIdentity,
			targetRoles:    []string{auth.RoleBarista},
			activeManagers: 2,
		}
		db, queries := setupStaffDB(t, cfg)
		h := auth.NewStaffSetEnabledHandler(db, queries)

		body := fmt.Sprintf(`{"request_id":"%s","expected_enabled":true,"enabled":false,"manager_pin":"%s"}`, uuid.New(), mgrPin)
		req := httptest.NewRequest(http.MethodPut, "/api/v1/staff/"+targetID.String()+"/enabled", strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues(targetID.String())
		c.Set(auth.StaffContextKey, actorClaims)

		err := h.HandleHTTP(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)

		assert.True(t, cfg.advisoryLockCalled, "transactional advisory lock must be acquired")
		assert.True(t, cfg.committed)
		assert.Equal(t, targetID, cfg.sessionsRevokedFor, "sessions must be revoked on disable")
	})

	t.Run("enabling disabled staff does not revoke sessions", func(t *testing.T) {
		disabledTarget := *targetIdentity
		disabledTarget.Enabled = false

		cfg := &staffMockConfig{
			actorID:        actorID,
			actorStaff:     actorIdentity,
			targetID:       targetID,
			targetStaff:    &disabledTarget,
			targetRoles:    []string{auth.RoleBarista},
			activeManagers: 2,
		}
		db, queries := setupStaffDB(t, cfg)
		h := auth.NewStaffSetEnabledHandler(db, queries)

		body := fmt.Sprintf(`{"request_id":"%s","expected_enabled":false,"enabled":true,"manager_pin":"%s"}`, uuid.New(), mgrPin)
		req := httptest.NewRequest(http.MethodPut, "/api/v1/staff/"+targetID.String()+"/enabled", strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues(targetID.String())
		c.Set(auth.StaffContextKey, actorClaims)

		err := h.HandleHTTP(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)

		assert.True(t, cfg.committed)
		assert.Equal(t, uuid.Nil, cfg.sessionsRevokedFor, "no sessions should be revoked on enable")
	})
}

// === Staff Replace Roles Handler Tests ===

func TestStaffReplaceRolesHandler(t *testing.T) {
	e := setupEcho()
	mgrPin := "123456"
	actorID, actorIdentity, actorClaims := createActorManager(mgrPin)

	targetID := uuid.New()
	targetIdentity := &sqlc.StaffIdentity{
		ID:          targetID,
		DisplayName: "Manager Two",
		LoginCode:   "MGR02",
		PinHash:     "hash",
		Enabled:     true,
		CreatedAt:   time.Now().UTC(),
	}

	t.Run("removing MANAGER role when only 1 active manager remains returns 409 Conflict", func(t *testing.T) {
		cfg := &staffMockConfig{
			actorID:        actorID,
			actorStaff:     actorIdentity,
			targetID:       targetID,
			targetStaff:    targetIdentity,
			targetRoles:    []string{auth.RoleManager},
			activeManagers: 1, // Only 1 active manager!
		}
		db, queries := setupStaffDB(t, cfg)
		h := auth.NewStaffReplaceRolesHandler(db, queries)

		body := fmt.Sprintf(`{"request_id":"%s","roles":["CASHIER"],"manager_pin":"%s"}`, uuid.New(), mgrPin)
		req := httptest.NewRequest(http.MethodPut, "/api/v1/staff/"+targetID.String()+"/roles", strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues(targetID.String())
		c.Set(auth.StaffContextKey, actorClaims)

		err := h.HandleHTTP(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusConflict, rec.Code)

		var resp response.APIResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.Contains(t, resp.Error.Message, "phải còn ít nhất một Quản lý đang hoạt động")
	})

	t.Run("removing MANAGER role when multiple active managers exist succeeds", func(t *testing.T) {
		cfg := &staffMockConfig{
			actorID:        actorID,
			actorStaff:     actorIdentity,
			targetID:       targetID,
			targetStaff:    targetIdentity,
			targetRoles:    []string{auth.RoleManager},
			activeManagers: 2, // 2 active managers
		}
		db, queries := setupStaffDB(t, cfg)
		h := auth.NewStaffReplaceRolesHandler(db, queries)

		body := fmt.Sprintf(`{"request_id":"%s","roles":["CASHIER"],"manager_pin":"%s"}`, uuid.New(), mgrPin)
		req := httptest.NewRequest(http.MethodPut, "/api/v1/staff/"+targetID.String()+"/roles", strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues(targetID.String())
		c.Set(auth.StaffContextKey, actorClaims)

		err := h.HandleHTTP(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)

		assert.True(t, cfg.advisoryLockCalled)
		assert.Equal(t, targetID, cfg.rolesClearedFor)
		assert.Contains(t, cfg.rolesAdded, auth.RoleCashier)
		assert.True(t, cfg.committed)
	})

	t.Run("target not found returns 404", func(t *testing.T) {
		cfg := &staffMockConfig{
			actorID:     actorID,
			actorStaff:  actorIdentity,
			targetID:    targetID,
			targetStaff: nil,
		}
		db, queries := setupStaffDB(t, cfg)
		h := auth.NewStaffReplaceRolesHandler(db, queries)

		body := fmt.Sprintf(`{"request_id":"%s","roles":["BARISTA"],"manager_pin":"%s"}`, uuid.New(), mgrPin)
		req := httptest.NewRequest(http.MethodPut, "/api/v1/staff/"+targetID.String()+"/roles", strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues(targetID.String())
		c.Set(auth.StaffContextKey, actorClaims)

		err := h.HandleHTTP(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("manager PIN mismatch returns 403 Forbidden", func(t *testing.T) {
		cfg := &staffMockConfig{
			actorID:    actorID,
			actorStaff: actorIdentity,
		}
		db, queries := setupStaffDB(t, cfg)
		h := auth.NewStaffReplaceRolesHandler(db, queries)

		body := fmt.Sprintf(`{"request_id":"%s","roles":["BARISTA"],"manager_pin":"000000"}`, uuid.New())
		req := httptest.NewRequest(http.MethodPut, "/api/v1/staff/"+targetID.String()+"/roles", strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues(targetID.String())
		c.Set(auth.StaffContextKey, actorClaims)

		err := h.HandleHTTP(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})
}

// === Staff Reset Pin Handler Tests ===

func TestStaffResetPinHandler(t *testing.T) {
	e := setupEcho()
	mgrPin := "123456"
	actorID, actorIdentity, actorClaims := createActorManager(mgrPin)

	targetID := uuid.New()
	targetIdentity := &sqlc.StaffIdentity{
		ID:          targetID,
		DisplayName: "Barista Bob",
		LoginCode:   "BOB",
		PinHash:     "oldhash",
		Enabled:     true,
		CreatedAt:   time.Now().UTC(),
	}

	t.Run("manager PIN mismatch returns 403 Forbidden", func(t *testing.T) {
		cfg := &staffMockConfig{
			actorID:    actorID,
			actorStaff: actorIdentity,
		}
		db, queries := setupStaffDB(t, cfg)
		h := auth.NewStaffResetPinHandler(db, queries)

		body := fmt.Sprintf(`{"request_id":"%s","pin":"9876","manager_pin":"000000"}`, uuid.New())
		req := httptest.NewRequest(http.MethodPut, "/api/v1/staff/"+targetID.String()+"/pin", strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues(targetID.String())
		c.Set(auth.StaffContextKey, actorClaims)

		err := h.HandleHTTP(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("target staff not found returns 404 Not Found", func(t *testing.T) {
		cfg := &staffMockConfig{
			actorID:     actorID,
			actorStaff:  actorIdentity,
			targetID:    targetID,
			targetStaff: nil,
		}
		db, queries := setupStaffDB(t, cfg)
		h := auth.NewStaffResetPinHandler(db, queries)

		body := fmt.Sprintf(`{"request_id":"%s","pin":"9876","manager_pin":"%s"}`, uuid.New(), mgrPin)
		req := httptest.NewRequest(http.MethodPut, "/api/v1/staff/"+targetID.String()+"/pin", strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues(targetID.String())
		c.Set(auth.StaffContextKey, actorClaims)

		err := h.HandleHTTP(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("successful PIN reset updates PIN and revokes active sessions", func(t *testing.T) {
		cfg := &staffMockConfig{
			actorID:     actorID,
			actorStaff:  actorIdentity,
			targetID:    targetID,
			targetStaff: targetIdentity,
		}
		db, queries := setupStaffDB(t, cfg)
		h := auth.NewStaffResetPinHandler(db, queries)

		body := fmt.Sprintf(`{"request_id":"%s","pin":"9876","manager_pin":"%s"}`, uuid.New(), mgrPin)
		req := httptest.NewRequest(http.MethodPut, "/api/v1/staff/"+targetID.String()+"/pin", strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues(targetID.String())
		c.Set(auth.StaffContextKey, actorClaims)

		err := h.HandleHTTP(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)

		var resp struct {
			Success bool              `json:"success"`
			Data    map[string]string `json:"data"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.True(t, resp.Success)
		assert.Equal(t, "đổi mã PIN thành công", resp.Data["message"])

		assert.Equal(t, targetID, cfg.pinUpdatedFor, "pin should be updated for target staff")
		assert.Equal(t, targetID, cfg.sessionsRevokedFor, "sessions must be revoked on PIN reset")
		assert.True(t, cfg.committed)
	})
}

func TestStaffConstructors(t *testing.T) {
	assert.NotNil(t, auth.NewStaffMeHandler(nil))
	assert.NotNil(t, auth.NewStaffListHandler(nil))
	assert.NotNil(t, auth.NewStaffCreateHandler(nil, nil))
	assert.NotNil(t, auth.NewStaffSetEnabledHandler(nil, nil))
	assert.NotNil(t, auth.NewStaffReplaceRolesHandler(nil, nil))
	assert.NotNil(t, auth.NewStaffResetPinHandler(nil, nil))
}
