package auth_test

import (
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSlices(t *testing.T) {
	slices := auth.NewSlices(nil, nil)
	require.NotNil(t, slices)

	assert.NotNil(t, slices.Middleware)
	assert.NotNil(t, slices.Bootstrap)
	assert.NotNil(t, slices.SignIn)
	assert.NotNil(t, slices.Unlock)
	assert.NotNil(t, slices.GetSession)
	assert.NotNil(t, slices.Lock)
	assert.NotNil(t, slices.SignOut)
	assert.NotNil(t, slices.DeclareWorkspace)
	assert.NotNil(t, slices.RecordActivity)
	assert.NotNil(t, slices.ListIdentities)
	assert.NotNil(t, slices.StaffMe)
	assert.NotNil(t, slices.StaffList)
	assert.NotNil(t, slices.StaffCreate)
	assert.NotNil(t, slices.StaffSetEnabled)
	assert.NotNil(t, slices.StaffReplaceRole)
	assert.NotNil(t, slices.StaffResetPin)
}

func TestRegisterRoutes(t *testing.T) {
	e := echo.New()
	v1 := e.Group("/api/v1")

	slices := auth.NewSlices(nil, nil)
	slices.RegisterRoutes(v1)

	expectedRoutes := map[string]string{
		"POST /api/v1/auth/bootstrap":      "Bootstrap",
		"GET /api/v1/auth/identities":      "ListIdentities",
		"POST /api/v1/auth/sign-in":        "SignIn",
		"GET /api/v1/auth/session":         "GetSession",
		"POST /api/v1/auth/unlock":         "Unlock",
		"POST /api/v1/auth/lock":           "Lock",
		"POST /api/v1/auth/sign-out":       "SignOut",
		"POST /api/v1/auth/workspace":      "DeclareWorkspace",
		"POST /api/v1/auth/activity":       "RecordActivity",
		"GET /api/v1/staff/me":             "StaffMe",
		"GET /api/v1/staff":                "StaffList",
		"POST /api/v1/staff":               "StaffCreate",
		"PATCH /api/v1/staff/:id/enabled":  "StaffSetEnabled",
		"PUT /api/v1/staff/:id/roles":      "StaffReplaceRole",
		"POST /api/v1/staff/:id/reset-pin": "StaffResetPin",
	}

	routes := e.Routes()
	registeredRoutes := make(map[string]bool)
	validRoutes := 0
	for _, r := range routes {
		if r.Method == "echo_route_not_found" {
			continue
		}
		validRoutes++
		registeredRoutes[r.Method+" "+r.Path] = true
	}

	for routeKey := range expectedRoutes {
		assert.True(t, registeredRoutes[routeKey], "expected route %s to be registered", routeKey)
	}

	assert.Equal(t, len(expectedRoutes), validRoutes, "number of registered routes must match expected")
}
