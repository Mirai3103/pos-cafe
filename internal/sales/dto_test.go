package sales

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The projection ships in its final shape from 5A. Fields owned by 5B and 5D
// are present and empty rather than absent, so integrators never face a
// breaking change when those sub-phases land.
func TestServiceSessionResponseShipsFinalShape(t *testing.T) {
	resp := ServiceSessionResponse{
		ID:            uuid.New(),
		ServiceNumber: "S00001",
		ServiceMode:   ModeTakeaway,
		State:         StateActive,
		SalesShiftID:  uuid.New(),
	}

	raw, err := json.Marshal(resp)
	require.NoError(t, err)

	var decoded map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &decoded))

	for _, field := range []string{
		"id", "service_number", "service_mode", "state", "customer_identity_id",
		"sales_shift_id", "tables", "created_at", "draft", "checks", "orders",
		"preparation_units",
	} {
		assert.Contains(t, decoded, field, "field %q must be in the 5A contract", field)
	}

	// Phase 6 concerns are omitted entirely, not stubbed: no Phase 5 sub-phase
	// will ever fill them.
	assert.NotContains(t, decoded, "preparation_alerts")
	assert.NotContains(t, decoded, "preparation_corrections")
}

// Empty collections serialize as [], never null, so clients can iterate
// without a nil check.
func TestEmptyCollectionsSerializeAsArrays(t *testing.T) {
	raw, err := json.Marshal(newServiceSessionResponse())
	require.NoError(t, err)

	body := string(raw)
	assert.Contains(t, body, `"tables":[]`)
	assert.Contains(t, body, `"checks":[]`)
	assert.Contains(t, body, `"orders":[]`)
	assert.Contains(t, body, `"preparation_units":[]`)
}

// Every Service Session in 5A is anonymous; the field is a constant null.
func TestCustomerIdentityIsAlwaysNull(t *testing.T) {
	raw, err := json.Marshal(ServiceSessionResponse{})
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"customer_identity_id":null`)
}

func TestDraftItemResponseNullables(t *testing.T) {
	raw, err := json.Marshal(newDraftItemResponse())
	require.NoError(t, err)

	body := string(raw)
	assert.Contains(t, body, `"price_vnd":null`)
	assert.Contains(t, body, `"size_id":null`)
	assert.Contains(t, body, `"size_name":null`)
	assert.Contains(t, body, `"preparation_note":null`)
	assert.Contains(t, body, `"selected_modifier_options":[]`)
}

// Absent and empty modifier_option_ids are different requests: absent means
// "apply the menu's defaults", empty means "the customer declined every
// option". Go collapses both to a nil slice unless the field is a pointer.
func TestAddDraftItemCommandDistinguishesAbsentFromEmpty(t *testing.T) {
	var absent AddDraftItemCommand
	require.NoError(t, json.Unmarshal([]byte(`{"menu_item_id":"`+uuid.Nil.String()+`"}`), &absent))
	assert.Nil(t, absent.ModifierOptionIDs, "absent must stay nil")

	var empty AddDraftItemCommand
	require.NoError(t, json.Unmarshal(
		[]byte(`{"menu_item_id":"`+uuid.Nil.String()+`","modifier_option_ids":[]}`), &empty))
	require.NotNil(t, empty.ModifierOptionIDs, "empty must be a non-nil pointer to an empty slice")
	assert.Empty(t, *empty.ModifierOptionIDs)
}
