package auth_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeRequestHash(t *testing.T) {
	t.Run("computes consistent SHA256 hex", func(t *testing.T) {
		type samplePayload struct {
			Name string `json:"name"`
			Age  int    `json:"age"`
		}
		p1 := samplePayload{Name: "Alice", Age: 30}
		p2 := samplePayload{Name: "Alice", Age: 30}

		h1 := auth.ComputeRequestHash("staff.create", p1)
		h2 := auth.ComputeRequestHash("staff.create", p2)

		assert.Len(t, h1, 64)
		assert.Equal(t, h1, h2)
	})

	t.Run("differs on different action", func(t *testing.T) {
		payload := map[string]string{"foo": "bar"}
		h1 := auth.ComputeRequestHash("action.one", payload)
		h2 := auth.ComputeRequestHash("action.two", payload)

		assert.NotEqual(t, h1, h2)
	})

	t.Run("differs on different payload", func(t *testing.T) {
		h1 := auth.ComputeRequestHash("action.one", map[string]string{"foo": "bar"})
		h2 := auth.ComputeRequestHash("action.one", map[string]string{"foo": "baz"})

		assert.NotEqual(t, h1, h2)
	})
}

// === Mock Driver for Idempotency Tests ===

var (
	idempDriverOnce sync.Once
	idempRegistry   = &idempDriverRegistry{
		configs: make(map[string]*idempMockConfig),
	}
)

type idempDriverRegistry struct {
	mu      sync.Mutex
	configs map[string]*idempMockConfig
}

func (r *idempDriverRegistry) set(name string, cfg *idempMockConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.configs[name] = cfg
}

func (r *idempDriverRegistry) get(name string) (*idempMockConfig, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cfg, ok := r.configs[name]
	return cfg, ok
}

type idempMockDriver struct{}

func (d *idempMockDriver) Open(name string) (driver.Conn, error) {
	cfg, ok := idempRegistry.get(name)
	if !ok {
		return nil, fmt.Errorf("mock configuration not found for: %s", name)
	}
	return &idempMockConn{cfg: cfg}, nil
}

type storedKey struct {
	Key          uuid.UUID
	ActorID      uuid.UUID
	Action       string
	RequestHash  string
	ResponseCode int32
	ResponseBody []byte
	CreatedAt    time.Time
}

type idempMockConfig struct {
	mu        sync.Mutex
	keys      map[string]storedKey
	getErr    error
	insertErr error
}

func (c *idempMockConfig) getKey(actorID, key uuid.UUID) (storedKey, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	k, ok := c.keys[actorID.String()+":"+key.String()]
	return k, ok
}

func (c *idempMockConfig) setKey(k storedKey) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.keys == nil {
		c.keys = make(map[string]storedKey)
	}
	c.keys[k.ActorID.String()+":"+k.Key.String()] = k
}

type idempMockConn struct {
	cfg *idempMockConfig
}

func (c *idempMockConn) Prepare(query string) (driver.Stmt, error) {
	return &idempMockStmt{conn: c, query: query}, nil
}

func (c *idempMockConn) Close() error              { return nil }
func (c *idempMockConn) Begin() (driver.Tx, error) { return &idempMockTx{}, nil }

type idempMockTx struct{}

func (t *idempMockTx) Commit() error   { return nil }
func (t *idempMockTx) Rollback() error { return nil }

type idempMockStmt struct {
	conn  *idempMockConn
	query string
}

func (s *idempMockStmt) Close() error  { return nil }
func (s *idempMockStmt) NumInput() int { return -1 }

func (s *idempMockStmt) Exec(args []driver.Value) (driver.Result, error) {
	if strings.Contains(s.query, "InsertIdempotencyKey") || strings.Contains(s.query, "idempotency_keys") {
		if s.conn.cfg.insertErr != nil {
			return nil, s.conn.cfg.insertErr
		}
		// VALUES ($1, $2, $3, $4, $5, $6)
		// args: key, actor_id, action, request_hash, response_code, response_body
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

		s.conn.cfg.setKey(storedKey{
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
	return driver.RowsAffected(1), nil
}

func (s *idempMockStmt) Query(args []driver.Value) (driver.Rows, error) {
	if strings.Contains(s.query, "GetIdempotencyKey") || strings.Contains(s.query, "idempotency_keys") {
		if s.conn.cfg.getErr != nil {
			return nil, s.conn.cfg.getErr
		}
		// WHERE actor_id = $1 AND key = $2
		actorID, _ := uuid.Parse(fmt.Sprint(args[0]))
		key, _ := uuid.Parse(fmt.Sprint(args[1]))

		k, found := s.conn.cfg.getKey(actorID, key)
		if !found {
			return &idempMockRows{cols: []string{"key", "actor_id", "action", "request_hash", "response_code", "response_body", "created_at"}, rows: [][]driver.Value{}}, nil
		}

		return &idempMockRows{
			cols: []string{"key", "actor_id", "action", "request_hash", "response_code", "response_body", "created_at"},
			rows: [][]driver.Value{
				{k.Key.String(), k.ActorID.String(), k.Action, k.RequestHash, k.ResponseCode, k.ResponseBody, k.CreatedAt},
			},
		}, nil
	}
	return nil, fmt.Errorf("unexpected query: %s", s.query)
}

type idempMockRows struct {
	cols []string
	rows [][]driver.Value
	pos  int
}

func (r *idempMockRows) Columns() []string { return r.cols }
func (r *idempMockRows) Close() error      { return nil }
func (r *idempMockRows) Next(dest []driver.Value) error {
	if r.pos >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.pos])
	r.pos++
	return nil
}

func setupIdempDB(t *testing.T, cfg *idempMockConfig) (*sql.DB, *sqlc.Queries) {
	t.Helper()
	idempDriverOnce.Do(func() {
		sql.Register("idemp-mock-driver", &idempMockDriver{})
	})

	dsn := fmt.Sprintf("idemp-test-%s", uuid.New().String())
	idempRegistry.set(dsn, cfg)

	db, err := sql.Open("idemp-mock-driver", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	return db, sqlc.New(db)
}

func TestExecuteWithIdempotency(t *testing.T) {
	actorID := uuid.New()
	key := uuid.New()
	action := "test.mutation"
	payload := map[string]string{"name": "test"}

	t.Run("initial execution runs fn and caches result", func(t *testing.T) {
		cfg := &idempMockConfig{}
		_, queries := setupIdempDB(t, cfg)

		callCount := 0
		code, res, err := auth.ExecuteWithIdempotency(context.Background(), queries, actorID, key, action, payload, func() (int, map[string]string, error) {
			callCount++
			return 201, map[string]string{"status": "created"}, nil
		})

		require.NoError(t, err)
		assert.Equal(t, 201, code)
		assert.Equal(t, "created", res["status"])
		assert.Equal(t, 1, callCount)

		// Verify stored in mock DB
		stored, found := cfg.getKey(actorID, key)
		assert.True(t, found)
		assert.Equal(t, int32(201), stored.ResponseCode)
		assert.Equal(t, auth.ComputeRequestHash(action, payload), stored.RequestHash)
	})

	t.Run("replay with identical payload returns cached result without calling fn", func(t *testing.T) {
		cfg := &idempMockConfig{}
		_, queries := setupIdempDB(t, cfg)

		callCount := 0
		run := func() (int, map[string]string, error) {
			return auth.ExecuteWithIdempotency(context.Background(), queries, actorID, key, action, payload, func() (int, map[string]string, error) {
				callCount++
				return 201, map[string]string{"status": "created"}, nil
			})
		}

		// First call
		code1, res1, err1 := run()
		require.NoError(t, err1)
		assert.Equal(t, 201, code1)
		assert.Equal(t, "created", res1["status"])
		assert.Equal(t, 1, callCount)

		// Second call (replay)
		code2, res2, err2 := run()
		require.NoError(t, err2)
		assert.Equal(t, 201, code2)
		assert.Equal(t, "created", res2["status"])
		assert.Equal(t, 1, callCount, "fn should not have been called again on replay")
	})

	t.Run("replay with same key but different payload returns ErrConflict", func(t *testing.T) {
		cfg := &idempMockConfig{}
		_, queries := setupIdempDB(t, cfg)

		// First call with payload1
		p1 := map[string]string{"name": "first"}
		_, _, err := auth.ExecuteWithIdempotency(context.Background(), queries, actorID, key, action, p1, func() (int, map[string]string, error) {
			return 201, map[string]string{"status": "ok"}, nil
		})
		require.NoError(t, err)

		// Second call with payload2 (different hash)
		p2 := map[string]string{"name": "second"}
		_, _, err2 := auth.ExecuteWithIdempotency(context.Background(), queries, actorID, key, action, p2, func() (int, map[string]string, error) {
			t.Fatal("fn should not be called")
			return 200, nil, nil
		})

		require.Error(t, err2)
		assert.True(t, errors.Is(err2, response.ErrConflict))
		assert.Contains(t, err2.Error(), "mã yêu cầu (request_id) đã được dùng cho payload khác")
	})

	t.Run("operation error does not save idempotency key", func(t *testing.T) {
		cfg := &idempMockConfig{}
		_, queries := setupIdempDB(t, cfg)

		opErr := errors.New("business failure")
		code, _, err := auth.ExecuteWithIdempotency(context.Background(), queries, actorID, key, action, payload, func() (int, map[string]string, error) {
			return 400, nil, opErr
		})

		require.Error(t, err)
		assert.Equal(t, 400, code)
		assert.True(t, errors.Is(err, opErr))

		_, found := cfg.getKey(actorID, key)
		assert.False(t, found, "failed operation should not be cached in idempotency store")
	})

	t.Run("db check error propagates", func(t *testing.T) {
		cfg := &idempMockConfig{
			getErr: errors.New("connection failed"),
		}
		_, queries := setupIdempDB(t, cfg)

		_, _, err := auth.ExecuteWithIdempotency(context.Background(), queries, actorID, key, action, payload, func() (int, map[string]string, error) {
			t.Fatal("fn should not be called")
			return 200, nil, nil
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "check idempotency key")
	})

	t.Run("corrupted cached response body returns error", func(t *testing.T) {
		cfg := &idempMockConfig{}
		cfg.setKey(storedKey{
			Key:          key,
			ActorID:      actorID,
			Action:       action,
			RequestHash:  auth.ComputeRequestHash(action, payload),
			ResponseCode: 200,
			ResponseBody: []byte("invalid-json{"),
		})
		_, queries := setupIdempDB(t, cfg)

		_, _, err := auth.ExecuteWithIdempotency(context.Background(), queries, actorID, key, action, payload, func() (int, map[string]string, error) {
			t.Fatal("fn should not be called")
			return 200, nil, nil
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "unmarshal cached response")
	})
}

func TestComputeRequestHashWithTarget(t *testing.T) {
	t.Parallel()

	action := "staff.set_enabled"
	body := map[string]bool{"enabled": false}
	targetID := uuid.New()

	sameTargetHash := auth.ComputeRequestHashWithTarget(action, targetID, body)
	assert.Equal(t, sameTargetHash, auth.ComputeRequestHashWithTarget(action, targetID, body))
	assert.NotEqual(t, sameTargetHash, auth.ComputeRequestHashWithTarget(action, uuid.New(), body))
}

func TestIdempotency_TargetSpecific(t *testing.T) {
	type targetPayload struct {
		TargetID uuid.UUID `json:"target_id"`
		Body     any       `json:"body"`
	}

	actorID := uuid.New()
	key := uuid.New()
	action := "staff.set_enabled"
	body := map[string]bool{"enabled": false}
	targetID := uuid.New()
	cfg := &idempMockConfig{}
	_, queries := setupIdempDB(t, cfg)

	callCount := 0
	run := func(targetID uuid.UUID) (int, map[string]string, error) {
		payload := targetPayload{TargetID: targetID, Body: body}
		return auth.ExecuteWithIdempotency(context.Background(), queries, actorID, key, action, payload, func() (int, map[string]string, error) {
			callCount++
			return http.StatusOK, map[string]string{"status": "updated"}, nil
		})
	}

	code, result, err := run(targetID)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "updated", result["status"])

	code, result, err = run(targetID)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "updated", result["status"])
	assert.Equal(t, 1, callCount)

	_, _, err = run(uuid.New())
	require.Error(t, err)
	assert.ErrorIs(t, err, response.ErrConflict)
	assert.Equal(t, 1, callCount)
}
