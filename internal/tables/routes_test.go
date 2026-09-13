package tables_test

import (
	"net/http"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/tables"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegisterRoutes(t *testing.T) {
	e := echo.New()
	v1 := e.Group("/api/v1")

	slices := tables.NewSlices(nil, nil)
	slices.RegisterRoutes(v1, &auth.Middleware{})

	registered := make(map[string]bool)
	for _, r := range e.Routes() {
		registered[r.Method+" "+r.Path] = true
	}

	expected := []string{
		http.MethodGet + " /api/v1/tables/overview",
		http.MethodPost + " /api/v1/tables",
		http.MethodPatch + " /api/v1/tables/:table_id/name",
		http.MethodPatch + " /api/v1/tables/:table_id/availability",
	}
	for _, route := range expected {
		assert.True(t, registered[route], "route %q must be registered", route)
	}

	assert.Len(t, e.Routes(), len(expected),
		"tables must register exactly four routes and no generic CRUD")
}

func TestNewSlicesWiresEveryHandler(t *testing.T) {
	slices := tables.NewSlices(nil, nil)
	require.NotNil(t, slices.Runner)
	require.NotNil(t, slices.Overview)
	require.NotNil(t, slices.CreateTable)
	require.NotNil(t, slices.RenameTable)
	require.NotNil(t, slices.SetTableAvailability)
}
