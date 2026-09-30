package httpx_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/platform/command"
	"github.com/Mirai3103/pos-cafe/internal/platform/httpx"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newContext(method, body string) (echo.Context, *httptest.ResponseRecorder) {
	req := httptest.NewRequest(method, "/", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	return echo.New().NewContext(req, rec), rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) response.APIResponse {
	t.Helper()
	var resp response.APIResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	return resp
}

func TestActor(t *testing.T) {
	c, _ := newContext(http.MethodGet, "")
	_, err := httpx.Actor(c)
	require.ErrorIs(t, err, response.ErrUnauthorized)
	assert.EqualError(t, err, "unauthorized: unauthorized")

	claims := &auth.StaffClaims{StaffID: uuid.New(), SessionID: uuid.New()}
	c.Set(auth.StaffContextKey, claims)
	actor, err := httpx.Actor(c)
	require.NoError(t, err)
	assert.Equal(t, command.Actor{StaffID: claims.StaffID, SessionID: claims.SessionID}, actor)
}

func TestUUIDParam(t *testing.T) {
	c, _ := newContext(http.MethodGet, "")
	want := uuid.New()
	c.SetParamNames("table_id")
	c.SetParamValues(want.String())
	got, err := httpx.UUIDParam(c, "table_id")
	require.NoError(t, err)
	assert.Equal(t, want, got)

	c.SetParamValues("nope")
	_, err = httpx.UUIDParam(c, "table_id")
	require.ErrorIs(t, err, response.ErrInvalid)
	assert.EqualError(t, err, "invalid input data: invalid table_id UUID: nope")
}

func TestBindBody(t *testing.T) {
	type body struct {
		Name string `json:"name"`
	}

	c, _ := newContext(http.MethodPost, `{"name":"Ban 1"}`)
	got, err := httpx.BindBody[body](c)
	require.NoError(t, err)
	assert.Equal(t, "Ban 1", got.Name)

	c, _ = newContext(http.MethodPost, `{"name":`)
	_, err = httpx.BindBody[body](c)
	require.ErrorIs(t, err, response.ErrInvalid)
	assert.True(t, strings.HasPrefix(err.Error(), "invalid input data: invalid request body: "), err.Error())
}

func TestRequireRequestID(t *testing.T) {
	require.NoError(t, httpx.RequireRequestID(uuid.New()))

	err := httpx.RequireRequestID(uuid.Nil)
	require.ErrorIs(t, err, response.ErrInvalid)
	assert.EqualError(t, err, "invalid input data: request_id is required")
}

func TestSendResult(t *testing.T) {
	for _, tc := range []struct {
		status, want int
	}{
		{http.StatusCreated, http.StatusCreated},
		{http.StatusOK, http.StatusOK},
		{http.StatusAccepted, http.StatusOK},
	} {
		c, rec := newContext(http.MethodPost, "")
		require.NoError(t, httpx.SendResult(c, tc.status, map[string]int{"n": 1}))
		assert.Equal(t, tc.want, rec.Code)
		assert.True(t, decode(t, rec).Success)
	}
}

func TestSendError(t *testing.T) {
	errTeapot := errors.New("teapot")
	mapper := func(err error) error {
		if errors.Is(err, errTeapot) {
			return response.NewCodedError(http.StatusTeapot, "TEAPOT", "short and stout", err)
		}
		return err
	}

	c, rec := newContext(http.MethodGet, "")
	require.NoError(t, httpx.SendError(c, errTeapot, mapper))
	assert.Equal(t, http.StatusTeapot, rec.Code)
	assert.Equal(t, "TEAPOT", decode(t, rec).Error.Code)

	c, rec = newContext(http.MethodGet, "")
	require.NoError(t, httpx.SendError(c, response.ErrForbidden, nil))
	assert.Equal(t, http.StatusForbidden, rec.Code, "a nil mapper leaves the shared sentinel handling")
}
