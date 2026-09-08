package category_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/category"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/Mirai3103/pos-cafe/internal/validator"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// === Mock Store Implementation ===

type mockCategoryStore struct {
	createFunc     func(ctx context.Context, arg sqlc.CreateCategoryParams) (sqlc.Category, error)
	getByIDFunc    func(ctx context.Context, id int64) (sqlc.Category, error)
	getByNameFunc  func(ctx context.Context, name string) (sqlc.Category, error)
	listFunc       func(ctx context.Context) ([]sqlc.Category, error)
	listActiveFunc func(ctx context.Context) ([]sqlc.Category, error)
	updateFunc     func(ctx context.Context, arg sqlc.UpdateCategoryParams) (sqlc.Category, error)
	deleteFunc     func(ctx context.Context, id int64) error
}

func (m *mockCategoryStore) CreateCategory(ctx context.Context, arg sqlc.CreateCategoryParams) (sqlc.Category, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, arg)
	}
	return sqlc.Category{}, errors.New("unimplemented")
}

func (m *mockCategoryStore) GetCategoryByID(ctx context.Context, id int64) (sqlc.Category, error) {
	if m.getByIDFunc != nil {
		return m.getByIDFunc(ctx, id)
	}
	return sqlc.Category{}, sql.ErrNoRows
}

func (m *mockCategoryStore) GetCategoryByName(ctx context.Context, name string) (sqlc.Category, error) {
	if m.getByNameFunc != nil {
		return m.getByNameFunc(ctx, name)
	}
	return sqlc.Category{}, sql.ErrNoRows
}

func (m *mockCategoryStore) ListCategories(ctx context.Context) ([]sqlc.Category, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx)
	}
	return nil, nil
}

func (m *mockCategoryStore) ListActiveCategories(ctx context.Context) ([]sqlc.Category, error) {
	if m.listActiveFunc != nil {
		return m.listActiveFunc(ctx)
	}
	return nil, nil
}

func (m *mockCategoryStore) UpdateCategory(ctx context.Context, arg sqlc.UpdateCategoryParams) (sqlc.Category, error) {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, arg)
	}
	return sqlc.Category{}, errors.New("unimplemented")
}

func (m *mockCategoryStore) DeleteCategory(ctx context.Context, id int64) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, id)
	}
	return nil
}

// === Mock Publisher ===

type mockPublisher struct {
	publishedEvents []any
	publishedTopics []string
	publishErr      error
}

func (m *mockPublisher) Publish(topic string, payload any) error {
	if m.publishErr != nil {
		return m.publishErr
	}
	m.publishedTopics = append(m.publishedTopics, topic)
	m.publishedEvents = append(m.publishedEvents, payload)
	return nil
}

func setupEcho() *echo.Echo {
	e := echo.New()
	e.Validator = validator.New()
	return e
}

// === Unit Tests ===

func TestCreateCategory_Handle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Now()

	t.Run("success with event published", func(t *testing.T) {
		t.Parallel()
		store := &mockCategoryStore{
			getByNameFunc: func(ctx context.Context, name string) (sqlc.Category, error) {
				return sqlc.Category{}, sql.ErrNoRows
			},
			createFunc: func(ctx context.Context, arg sqlc.CreateCategoryParams) (sqlc.Category, error) {
				return sqlc.Category{
					ID:           1,
					Name:         arg.Name,
					Description:  arg.Description,
					DisplayOrder: arg.DisplayOrder,
					IsActive:     arg.IsActive,
					CreatedAt:    now,
					UpdatedAt:    now,
				}, nil
			},
		}
		pub := &mockPublisher{}
		handler := category.NewCreateHandler(store, pub)

		res, err := handler.Handle(ctx, category.CreateCommand{
			Name:         "Espresso",
			Description:  "Single shot",
			DisplayOrder: 1,
		})

		require.NoError(t, err)
		assert.Equal(t, int64(1), res.ID)
		assert.Equal(t, "Espresso", res.Name)
		assert.True(t, res.IsActive)
		require.Len(t, pub.publishedTopics, 1)
		assert.Equal(t, category.TopicCategoryCreated, pub.publishedTopics[0])
	})

	t.Run("conflict on pre-check", func(t *testing.T) {
		t.Parallel()
		store := &mockCategoryStore{
			getByNameFunc: func(ctx context.Context, name string) (sqlc.Category, error) {
				return sqlc.Category{ID: 10, Name: name}, nil
			},
		}
		handler := category.NewCreateHandler(store, nil)

		_, err := handler.Handle(ctx, category.CreateCommand{Name: "Espresso"})
		require.Error(t, err)
		assert.True(t, errors.Is(err, response.ErrConflict))
	})

	t.Run("conflict on database race condition (PostgreSQL 23505)", func(t *testing.T) {
		t.Parallel()
		store := &mockCategoryStore{
			getByNameFunc: func(ctx context.Context, name string) (sqlc.Category, error) {
				return sqlc.Category{}, sql.ErrNoRows
			},
			createFunc: func(ctx context.Context, arg sqlc.CreateCategoryParams) (sqlc.Category, error) {
				return sqlc.Category{}, &pgconn.PgError{Code: "23505"}
			},
		}
		handler := category.NewCreateHandler(store, nil)

		_, err := handler.Handle(ctx, category.CreateCommand{Name: "Espresso"})
		require.Error(t, err)
		assert.True(t, errors.Is(err, response.ErrConflict))
	})

	t.Run("database error on creation", func(t *testing.T) {
		t.Parallel()
		store := &mockCategoryStore{
			getByNameFunc: func(ctx context.Context, name string) (sqlc.Category, error) {
				return sqlc.Category{}, sql.ErrNoRows
			},
			createFunc: func(ctx context.Context, arg sqlc.CreateCategoryParams) (sqlc.Category, error) {
				return sqlc.Category{}, errors.New("db disk failure")
			},
		}
		handler := category.NewCreateHandler(store, nil)

		_, err := handler.Handle(ctx, category.CreateCommand{Name: "Espresso"})
		require.Error(t, err)
		assert.False(t, errors.Is(err, response.ErrConflict))
	})
}

func TestGetByID_Handle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		store := &mockCategoryStore{
			getByIDFunc: func(ctx context.Context, id int64) (sqlc.Category, error) {
				return sqlc.Category{ID: id, Name: "Cold Brew"}, nil
			},
		}
		handler := category.NewGetByIDHandler(store)

		res, err := handler.Handle(ctx, category.GetByIDQuery{ID: 5})
		require.NoError(t, err)
		assert.Equal(t, int64(5), res.ID)
		assert.Equal(t, "Cold Brew", res.Name)
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()
		store := &mockCategoryStore{
			getByIDFunc: func(ctx context.Context, id int64) (sqlc.Category, error) {
				return sqlc.Category{}, sql.ErrNoRows
			},
		}
		handler := category.NewGetByIDHandler(store)

		_, err := handler.Handle(ctx, category.GetByIDQuery{ID: 99})
		require.Error(t, err)
		assert.True(t, errors.Is(err, response.ErrNotFound))
	})
}

func TestList_Handle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("list all", func(t *testing.T) {
		t.Parallel()
		store := &mockCategoryStore{
			listFunc: func(ctx context.Context) ([]sqlc.Category, error) {
				return []sqlc.Category{
					{ID: 1, Name: "Tea"},
					{ID: 2, Name: "Coffee"},
				}, nil
			},
		}
		handler := category.NewListHandler(store)

		res, err := handler.Handle(ctx, category.ListQuery{ActiveOnly: false})
		require.NoError(t, err)
		assert.Len(t, res, 2)
	})

	t.Run("list active only", func(t *testing.T) {
		t.Parallel()
		store := &mockCategoryStore{
			listActiveFunc: func(ctx context.Context) ([]sqlc.Category, error) {
				return []sqlc.Category{
					{ID: 1, Name: "Tea", IsActive: true},
				}, nil
			},
		}
		handler := category.NewListHandler(store)

		res, err := handler.Handle(ctx, category.ListQuery{ActiveOnly: true})
		require.NoError(t, err)
		assert.Len(t, res, 1)
	})
}

func TestUpdateCategory_Handle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		store := &mockCategoryStore{
			getByIDFunc: func(ctx context.Context, id int64) (sqlc.Category, error) {
				return sqlc.Category{ID: id, Name: "Old Name"}, nil
			},
			getByNameFunc: func(ctx context.Context, name string) (sqlc.Category, error) {
				return sqlc.Category{}, sql.ErrNoRows
			},
			updateFunc: func(ctx context.Context, arg sqlc.UpdateCategoryParams) (sqlc.Category, error) {
				return sqlc.Category{ID: arg.ID, Name: arg.Name}, nil
			},
		}
		handler := category.NewUpdateHandler(store)

		res, err := handler.Handle(ctx, category.UpdateCommand{
			ID:   1,
			Name: "New Name",
		})
		require.NoError(t, err)
		assert.Equal(t, "New Name", res.Name)
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()
		store := &mockCategoryStore{
			getByIDFunc: func(ctx context.Context, id int64) (sqlc.Category, error) {
				return sqlc.Category{}, sql.ErrNoRows
			},
		}
		handler := category.NewUpdateHandler(store)

		_, err := handler.Handle(ctx, category.UpdateCommand{ID: 999, Name: "New Name"})
		require.Error(t, err)
		assert.True(t, errors.Is(err, response.ErrNotFound))
	})

	t.Run("conflict with another category", func(t *testing.T) {
		t.Parallel()
		store := &mockCategoryStore{
			getByIDFunc: func(ctx context.Context, id int64) (sqlc.Category, error) {
				return sqlc.Category{ID: id, Name: "My Category"}, nil
			},
			getByNameFunc: func(ctx context.Context, name string) (sqlc.Category, error) {
				return sqlc.Category{ID: 2, Name: name}, nil
			},
		}
		handler := category.NewUpdateHandler(store)

		_, err := handler.Handle(ctx, category.UpdateCommand{ID: 1, Name: "Other Category"})
		require.Error(t, err)
		assert.True(t, errors.Is(err, response.ErrConflict))
	})

	t.Run("conflict from postgresql race (23505)", func(t *testing.T) {
		t.Parallel()
		store := &mockCategoryStore{
			getByIDFunc: func(ctx context.Context, id int64) (sqlc.Category, error) {
				return sqlc.Category{ID: id, Name: "Name A"}, nil
			},
			getByNameFunc: func(ctx context.Context, name string) (sqlc.Category, error) {
				return sqlc.Category{}, sql.ErrNoRows
			},
			updateFunc: func(ctx context.Context, arg sqlc.UpdateCategoryParams) (sqlc.Category, error) {
				return sqlc.Category{}, &pgconn.PgError{Code: "23505"}
			},
		}
		handler := category.NewUpdateHandler(store)

		_, err := handler.Handle(ctx, category.UpdateCommand{ID: 1, Name: "Name B"})
		require.Error(t, err)
		assert.True(t, errors.Is(err, response.ErrConflict))
	})
}

func TestDeleteCategory_Handle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		store := &mockCategoryStore{
			getByIDFunc: func(ctx context.Context, id int64) (sqlc.Category, error) {
				return sqlc.Category{ID: id, Name: "To Delete"}, nil
			},
			deleteFunc: func(ctx context.Context, id int64) error {
				return nil
			},
		}
		handler := category.NewDeleteHandler(store)

		err := handler.Handle(ctx, category.DeleteCommand{ID: 1})
		require.NoError(t, err)
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()
		store := &mockCategoryStore{
			getByIDFunc: func(ctx context.Context, id int64) (sqlc.Category, error) {
				return sqlc.Category{}, sql.ErrNoRows
			},
		}
		handler := category.NewDeleteHandler(store)

		err := handler.Handle(ctx, category.DeleteCommand{ID: 999})
		require.Error(t, err)
		assert.True(t, errors.Is(err, response.ErrNotFound))
	})
}

// === HTTP Transport Unit Tests ===

func TestCategoryHTTP_Endpoints(t *testing.T) {
	t.Parallel()

	t.Run("POST /categories - validation failure", func(t *testing.T) {
		t.Parallel()
		e := setupEcho()
		store := &mockCategoryStore{}
		handler := category.NewCreateHandler(store, nil)
		e.POST("/categories", handler.HandleHTTP)

		body := []byte(`{"name": "A"}`) // min 2 characters
		req := httptest.NewRequest(http.MethodPost, "/categories", bytes.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("POST /categories - 201 Created", func(t *testing.T) {
		t.Parallel()
		e := setupEcho()
		store := &mockCategoryStore{
			getByNameFunc: func(ctx context.Context, name string) (sqlc.Category, error) {
				return sqlc.Category{}, sql.ErrNoRows
			},
			createFunc: func(ctx context.Context, arg sqlc.CreateCategoryParams) (sqlc.Category, error) {
				return sqlc.Category{ID: 1, Name: arg.Name}, nil
			},
		}
		handler := category.NewCreateHandler(store, nil)
		e.POST("/categories", handler.HandleHTTP)

		body := []byte(`{"name": "Smoothies"}`)
		req := httptest.NewRequest(http.MethodPost, "/categories", bytes.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusCreated, rec.Code)
		var res response.APIResponse
		err := json.Unmarshal(rec.Body.Bytes(), &res)
		require.NoError(t, err)
		assert.True(t, res.Success)
	})

	t.Run("GET /categories/:id - invalid ID", func(t *testing.T) {
		t.Parallel()
		e := setupEcho()
		store := &mockCategoryStore{}
		handler := category.NewGetByIDHandler(store)
		e.GET("/categories/:id", handler.HandleHTTP)

		req := httptest.NewRequest(http.MethodGet, "/categories/abc", nil)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("DELETE /categories/:id - 204 NoContent", func(t *testing.T) {
		t.Parallel()
		e := setupEcho()
		store := &mockCategoryStore{
			getByIDFunc: func(ctx context.Context, id int64) (sqlc.Category, error) {
				return sqlc.Category{ID: id}, nil
			},
			deleteFunc: func(ctx context.Context, id int64) error {
				return nil
			},
		}
		handler := category.NewDeleteHandler(store)
		e.DELETE("/categories/:id", handler.HandleHTTP)

		req := httptest.NewRequest(http.MethodDelete, "/categories/5", nil)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNoContent, rec.Code)
	})
}
