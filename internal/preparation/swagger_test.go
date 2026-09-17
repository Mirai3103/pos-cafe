package preparation_test

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The generated-Swagger contract of the Phase 6B correction routes. The test
// reads ../../docs/swagger.json — the file `swag init -g cmd/api/main.go -o
// docs` generates — and asserts each route exposes its POST operation with
// BearerAuth, the documented request body schema, the success status wired to
// the exact response DTO, and the full 400|401|403|404|409|500 error set. It
// then walks every definition reachable from those RESPONSES and proves no
// response field anywhere carries a PIN: no manager_pin, no pin, no pin_hash.
// The Manager PIN is request-only; this is the guard that keeps it that way.
//
// The test runs without the integration tag: it reads a file, not a database.

// swaggerSecurityRequirement is one OAuth2-style security requirement object.
type swaggerSecurityRequirement map[string][]string

// swaggerSchema is the slice of a JSON Schema this contract reads: $ref
// resolution and the allOf/properties/items shapes swag emits for
// response.APIResponse{data=...} envelopes.
type swaggerSchema struct {
	Ref        string                   `json:"$ref"`
	AllOf      []swaggerSchema          `json:"allOf"`
	Properties map[string]swaggerSchema `json:"properties"`
	Items      *swaggerSchema           `json:"items"`
}

// swaggerParam is one operation parameter; only the body parameter carries a
// schema this contract cares about.
type swaggerParam struct {
	Name     string         `json:"name"`
	In       string         `json:"in"`
	Required bool           `json:"required"`
	Schema   *swaggerSchema `json:"schema"`
}

// swaggerResponse is one named response; only its schema is inspected.
type swaggerResponse struct {
	Schema *swaggerSchema `json:"schema"`
}

// swaggerOperation is one path operation.
type swaggerOperation struct {
	Security   []swaggerSecurityRequirement `json:"security"`
	Parameters []swaggerParam               `json:"parameters"`
	Responses  map[string]swaggerResponse   `json:"responses"`
}

// swaggerDoc is the slice of docs/swagger.json the contract reads.
type swaggerDoc struct {
	Paths       map[string]map[string]*swaggerOperation `json:"paths"`
	Definitions map[string]json.RawMessage              `json:"definitions"`
}

// swaggerContractRoute is one Phase 6B route's expected generated contract.
type swaggerContractRoute struct {
	path          string
	successStatus string
	requestRef    string
	dataRef       string
}

// requiredErrorStatuses is the error set every correction route must document.
var requiredErrorStatuses = []string{"400", "401", "403", "404", "409", "500"}

// collectSwaggerResponseRefs resolves every definition the response schemas of
// one operation reach, through $ref, allOf, properties, and items, so the PIN
// sweep covers the whole response closure.
func collectSwaggerResponseRefs(schema *swaggerSchema, doc *swaggerDoc,
	visited map[string]bool, t *testing.T,
) {
	t.Helper()
	if schema == nil {
		return
	}
	if schema.Ref != "" {
		name := strings.TrimPrefix(schema.Ref, "#/definitions/")
		if visited[name] {
			return
		}
		visited[name] = true
		raw, ok := doc.Definitions[name]
		require.True(t, ok, "response schema references unknown definition %q", name)
		var definition swaggerSchema
		require.NoError(t, json.Unmarshal(raw, &definition),
			"definition %q must be a decodable schema", name)
		collectSwaggerResponseRefs(&definition, doc, visited, t)
		return
	}
	for i := range schema.AllOf {
		collectSwaggerResponseRefs(&schema.AllOf[i], doc, visited, t)
	}
	for name := range schema.Properties {
		property := schema.Properties[name]
		collectSwaggerResponseRefs(&property, doc, visited, t)
	}
	if schema.Items != nil {
		collectSwaggerResponseRefs(schema.Items, doc, visited, t)
	}
}

// swaggerDefinitionFieldNames walks a decoded definition and reports every
// object key at any depth — the response's field names, including the names of
// nested schema objects like properties and definitions.
func swaggerDefinitionFieldNames(value any, visit func(string)) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			visit(key)
			swaggerDefinitionFieldNames(child, visit)
		}
	case []any:
		for _, child := range typed {
			swaggerDefinitionFieldNames(child, visit)
		}
	}
}

func TestPreparationSwaggerCorrectionRoutes(t *testing.T) {
	raw, err := os.ReadFile("../../docs/swagger.json")
	require.NoError(t, err, "docs/swagger.json must exist; regenerate with `swag init -g cmd/api/main.go -o docs`")

	var doc swaggerDoc
	require.NoError(t, json.Unmarshal(raw, &doc))
	require.NotEmpty(t, doc.Paths)

	routes := []swaggerContractRoute{
		{
			path:          "/preparation/alerts/{alert_id}/acknowledge",
			successStatus: "200",
			requestRef:    "preparation.AcknowledgeAlertCommand",
			dataRef:       "preparation.AlertResponse",
		},
		{
			path:          "/preparation/units/{unit_id}/waste",
			successStatus: "201",
			requestRef:    "preparation.WasteUnitCommand",
			dataRef:       "preparation.WasteResponse",
		},
		{
			path:          "/preparation/wastes/{waste_id}/remake",
			successStatus: "201",
			requestRef:    "preparation.RemakeUnitCommand",
			dataRef:       "preparation.RemakeResponse",
		},
		{
			path:          "/preparation/units/correct-state",
			successStatus: "200",
			requestRef:    "preparation.CorrectStateCommand",
			dataRef:       "preparation.CorrectStateResponse",
		},
	}

	// visited accumulates the response closure of all four routes for the
	// PIN sweep at the end.
	visited := map[string]bool{}

	for _, route := range routes {
		t.Run(route.path, func(t *testing.T) {
			operations, ok := doc.Paths[route.path]
			require.True(t, ok, "path %q must be documented", route.path)

			operation, ok := operations["post"]
			require.True(t, ok, "path %q must expose a POST operation", route.path)

			// Security: BearerAuth on every correction route.
			require.NotEmpty(t, operation.Security, "%s must declare security", route.path)
			secured := false
			for _, requirement := range operation.Security {
				if _, ok := requirement["BearerAuth"]; ok {
					secured = true
				}
			}
			assert.True(t, secured, "%s must be secured by BearerAuth", route.path)

			// Request body: exactly one body parameter referencing the
			// route's command DTO.
			var bodyRefs []string
			for _, parameter := range operation.Parameters {
				if parameter.In != "body" || parameter.Schema == nil {
					continue
				}
				assert.True(t, parameter.Required,
					"%s body parameter must be required", route.path)
				bodyRefs = append(bodyRefs, parameter.Schema.Ref)
			}
			require.Len(t, bodyRefs, 1, "%s must document exactly one body schema", route.path)
			assert.Equal(t, "#/definitions/"+route.requestRef, bodyRefs[0],
				"%s body schema must be %s", route.path, route.requestRef)

			// Responses: the success status and the full error set.
			for _, status := range append(requiredErrorStatuses, route.successStatus) {
				_, ok := operation.Responses[status]
				assert.True(t, ok, "%s must document a %s response", route.path, status)
			}

			// Success: response.APIResponse wrapped around the exact DTO.
			success := operation.Responses[route.successStatus]
			require.NotNil(t, success.Schema, "%s success must carry a schema", route.path)
			require.Len(t, success.Schema.AllOf, 2,
				"%s success schema must be response.APIResponse{data=...}", route.path)
			assert.Equal(t, "#/definitions/response.APIResponse", success.Schema.AllOf[0].Ref)
			data := success.Schema.AllOf[1].Properties["data"]
			assert.Equal(t, "#/definitions/"+route.dataRef, data.Ref,
				"%s success data must be %s", route.path, route.dataRef)

			// Collect this operation's response closure for the PIN sweep.
			collectSwaggerResponseRefs(success.Schema, &doc, visited, t)
			for _, status := range requiredErrorStatuses {
				failure, ok := operation.Responses[status]
				require.True(t, ok)
				collectSwaggerResponseRefs(failure.Schema, &doc, visited, t)
			}
		})
	}

	// The PIN sweep: no field of any response-reachable definition — at any
	// depth, including the error envelope — may carry a PIN fragment. "pin"
	// as a substring covers manager_pin and pin_hash too.
	names := make([]string, 0, len(visited))
	for name := range visited {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, expected := range routeDataDefinitions() {
		require.Contains(t, names, expected,
			"the response closure must include %s", expected)
	}

	for _, name := range names {
		var decoded any
		require.NoError(t, json.Unmarshal(doc.Definitions[name], &decoded))
		swaggerDefinitionFieldNames(decoded, func(field string) {
			assert.False(t, strings.Contains(strings.ToLower(field), "pin"),
				"response definition %s must not expose a pin field, found %q", name, field)
		})
	}
}

// routeDataDefinitions names the data DTOs the sweep must have visited, so a
// silently dropped response schema cannot empty the closure and pass.
func routeDataDefinitions() []string {
	return []string{
		"preparation.AlertResponse",
		"preparation.CorrectStateResponse",
		"preparation.RemakeResponse",
		"preparation.WasteResponse",
		"response.APIResponse",
	}
}
