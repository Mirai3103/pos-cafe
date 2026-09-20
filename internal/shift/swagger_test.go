package shift_test

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The generated-Swagger contract of the Phase 07 Shift closure and
// reconciliation routes. The test reads ../../docs/swagger.json — the file
// `make swagger` (`swag init -g cmd/api/main.go -o docs`) generates — and
// asserts every Phase 07 route is documented with its method, BearerAuth
// security, the exact request body schema, the success status wired to the
// exact response DTO, and the error statuses its handler can return: the full
// 400|401|403|404|409 set for the four commands, the window-and-capability
// subset for the Manager history list, the 404 for the history detail, and the
// capability pair for the current read. It then walks every definition
// reachable from those RESPONSES and proves no response field anywhere carries
// a PIN. Finally it pins the documentation carry-forwards: the pre-Phase-07
// current-Shift shapes are gone, and the approval-bearing request DTOs
// document no example values, because the approver login and PIN are
// request-only secrets that must never ship in an example.
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

// swaggerContractRoute is one Phase 07 route's expected generated contract.
type swaggerContractRoute struct {
	path          string
	method        string
	successStatus string
	// requestRef is the body schema the operation must reference. The GET
	// routes document query and path parameters instead and carry none.
	requestRef string
	dataRef    string
	// errorStatuses are the non-2xx codes the operation documents.
	errorStatuses []string
}

// phase07Routes is the Shift closure, reconciliation, and history HTTP
// contract the generated document must carry.
var phase07Routes = []swaggerContractRoute{
	{
		path:          "/shifts/{shift_id}/reconciliation",
		method:        "post",
		successStatus: "201",
		requestRef:    "shift.StartReconciliationCommand",
		dataRef:       "shift.ClosingShiftResponse",
		errorStatuses: []string{"400", "401", "403", "404", "409"},
	},
	{
		path:          "/shifts/{shift_id}/reconciliation/cash-counts",
		method:        "post",
		successStatus: "201",
		requestRef:    "shift.RecordCashCountCommand",
		dataRef:       "shift.CashCountResult",
		errorStatuses: []string{"400", "401", "403", "404", "409"},
	},
	{
		path:          "/shifts/{shift_id}/reconciliation/qr-observations",
		method:        "post",
		successStatus: "201",
		requestRef:    "shift.RecordQRObservationCommand",
		dataRef:       "shift.QRObservationResult",
		errorStatuses: []string{"400", "401", "403", "404", "409"},
	},
	{
		path:          "/shifts/{shift_id}/close",
		method:        "post",
		successStatus: "200",
		requestRef:    "shift.CloseShiftCommand",
		dataRef:       "shift.ClosedShiftDetailResponse",
		errorStatuses: []string{"400", "401", "403", "404", "409"},
	},
	{
		path:          "/shifts",
		method:        "get",
		successStatus: "200",
		dataRef:       "shift.ClosedShiftListResponse",
		errorStatuses: []string{"400", "401", "403"},
	},
	{
		path:          "/shifts/{shift_id}",
		method:        "get",
		successStatus: "200",
		dataRef:       "shift.ClosedShiftDetailResponse",
		errorStatuses: []string{"400", "401", "403", "404"},
	},
	{
		path:          "/shifts/current",
		method:        "get",
		successStatus: "200",
		dataRef:       "shift.CurrentShiftResponse",
		errorStatuses: []string{"401", "403"},
	},
}

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

func TestSwaggerPhase07RouteContracts(t *testing.T) {
	raw, err := os.ReadFile("../../docs/swagger.json")
	require.NoError(t, err, "docs/swagger.json must exist; regenerate with `make swagger`")

	var doc swaggerDoc
	require.NoError(t, json.Unmarshal(raw, &doc))
	require.NotEmpty(t, doc.Paths)

	// visited accumulates the response closure of every route for the PIN
	// sweep at the end.
	visited := map[string]bool{}

	for _, route := range phase07Routes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			operations, ok := doc.Paths[route.path]
			require.True(t, ok, "path %q must be documented", route.path)

			operation, ok := operations[route.method]
			require.True(t, ok, "path %q must expose a %s operation", route.path, route.method)

			// Security: BearerAuth on every Shift route.
			require.NotEmpty(t, operation.Security, "%s must declare security", route.path)
			secured := false
			for _, requirement := range operation.Security {
				if _, ok := requirement["BearerAuth"]; ok {
					secured = true
				}
			}
			assert.True(t, secured, "%s must be secured by BearerAuth", route.path)

			// Request body: the POST commands reference exactly their command
			// DTO; the GET reads document none.
			var bodyRefs []string
			for _, parameter := range operation.Parameters {
				if parameter.In != "body" || parameter.Schema == nil {
					continue
				}
				bodyRefs = append(bodyRefs, parameter.Schema.Ref)
			}
			if route.requestRef == "" {
				assert.Empty(t, bodyRefs, "%s must not document a body schema", route.path)
			} else {
				require.Len(t, bodyRefs, 1, "%s must document exactly one body schema", route.path)
				assert.Equal(t, "#/definitions/"+route.requestRef, bodyRefs[0],
					"%s body schema must be %s", route.path, route.requestRef)
			}

			// Responses: the success status plus every documented error status.
			for _, status := range append([]string{route.successStatus}, route.errorStatuses...) {
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
			for _, status := range route.errorStatuses {
				failure, ok := operation.Responses[status]
				require.True(t, ok)
				collectSwaggerResponseRefs(failure.Schema, &doc, visited, t)
			}
		})
	}

	// The PIN sweep: no field of any response-reachable definition — at any
	// depth, including the error envelope — may carry a PIN fragment. "pin"
	// as a substring covers manager_pin and pin_hash too. The Manager PIN is
	// request-only; this is the guard that keeps it that way.
	names := make([]string, 0, len(visited))
	for name := range visited {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, expected := range phase07DataDefinitions() {
		require.Contains(t, names, expected,
			"the response closure must include %s", expected)
	}

	for _, name := range names {
		var decoded any
		require.NoError(t, json.Unmarshal(doc.Definitions[name], &decoded))
		swaggerRejectPinExamples(decoded, name, t)
	}
}

// phase07DataDefinitions names the data DTOs the sweep must have visited, so a
// silently dropped response schema cannot empty the closure and pass.
func phase07DataDefinitions() []string {
	return []string{
		"response.APIResponse",
		"shift.CashCountResult",
		"shift.ClosedShiftDetailResponse",
		"shift.ClosedShiftListResponse",
		"shift.ClosingShiftResponse",
		"shift.CurrentShiftResponse",
		"shift.QRObservationResult",
		"shift.ReconciliationPreview",
		"shift.ReconciliationResponse",
		"shift.StaffSummary",
	}
}

// swaggerRejectPinExamples walks one decoded definition and asserts no field
// anywhere in it is named after a PIN and no field carries an example value:
// swag only emits "example" from an example struct tag, so its absence on the
// approval fields is what proves no secret can ship in a generated example.
func swaggerRejectPinExamples(value any, definition string, t *testing.T) {
	t.Helper()
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			assert.NotEqual(t, "example", key,
				"definition %s must not ship an example value", definition)
			assert.False(t, strings.Contains(strings.ToLower(key), "pin"),
				"response definition %s must not expose a pin field, found %q",
				definition, key)
			swaggerRejectPinExamples(child, definition, t)
		}
	case []any:
		for _, child := range typed {
			swaggerRejectPinExamples(child, definition, t)
		}
	}
}

// TestSwaggerPhase07RequestSecretsAreExampleFree pins the Global Constraint on
// the request side: the two commands that carry the Manager Approval pair must
// document both request-only fields — clients need their names — but neither
// field may carry an example value, so no real PIN can ever appear in the
// generated document. It also pins the removal of the pre-Phase-07 shapes the
// carry-forward review flagged.
func TestSwaggerPhase07RequestSecretsAreExampleFree(t *testing.T) {
	raw, err := os.ReadFile("../../docs/swagger.json")
	require.NoError(t, err, "docs/swagger.json must exist; regenerate with `make swagger`")

	var doc swaggerDoc
	require.NoError(t, json.Unmarshal(raw, &doc))

	for _, name := range []string{"shift.CloseShiftCommand", "shift.RecordCashMovementCommand"} {
		rawDefinition, ok := doc.Definitions[name]
		require.True(t, ok, "request definition %s must be documented", name)
		var definition struct {
			Properties map[string]struct {
				Example any `json:"example"`
			} `json:"properties"`
		}
		require.NoError(t, json.Unmarshal(rawDefinition, &definition))
		for _, field := range []string{"approver_login_code", "manager_pin"} {
			property, ok := definition.Properties[field]
			require.True(t, ok, "%s must document the request-only field %s", name, field)
			assert.Nil(t, property.Example,
				"%s.%s must not ship an example: the approver login and PIN are request-only secrets",
				name, field)
		}
	}

	// The stale pre-Phase-07 definitions: the old current-Shift shape that
	// leaked Expected Cash and Refund rows, and the Refund summary it alone
	// referenced.
	for _, stale := range []string{"shift.CurrentSalesShiftResponse", "shift.RefundSummaryResponse"} {
		_, ok := doc.Definitions[stale]
		assert.False(t, ok, "%s is a pre-Phase-07 shape and must not ship", stale)
	}
}
