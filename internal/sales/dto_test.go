package sales

import (
	"encoding/json"
	"testing"
	"time"

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

// ManagerApprovalInput is the request-only credential block Comp, Refund, and
// Payment Void embed. The JSON tags are the wire contract, and the type
// appears in no response.
func TestManagerApprovalInputIsRequestOnlyCredentials(t *testing.T) {
	raw, err := json.Marshal(ManagerApprovalInput{
		ApproverLoginCode: "MANAGER01",
		ManagerPIN:        "8642",
	})
	require.NoError(t, err)
	assert.JSONEq(t, `{"approver_login_code":"MANAGER01","manager_pin":"8642"}`, string(raw))

	var in ManagerApprovalInput
	require.NoError(t, json.Unmarshal(
		[]byte(`{"approver_login_code":"manager01","manager_pin":"8642"}`), &in))
	assert.Equal(t, "manager01", in.ApproverLoginCode)
	assert.Equal(t, "8642", in.ManagerPIN)
}

func TestCheckResponseSerialization(t *testing.T) {
	check := CheckResponse{
		ID:              uuid.New(),
		State:           "OPEN",
		ChargeVND:       85_000,
		TotalAppliedVND: 0,
		BalanceVND:      85_000,
		Payments:        make([]PaymentResponse, 0),
		Allocations:     make([]ChargeAllocationResponse, 0),
	}
	b, err := json.Marshal(check)
	require.NoError(t, err)
	require.Contains(t, string(b), `"payments":[]`)
	require.Contains(t, string(b), `"allocations":[]`)
	require.Contains(t, string(b), `"total_applied_vnd":0`)
	// pending_refund_vnd is omitted, not stubbed: Refund is outside Phase 5.
	require.NotContains(t, string(b), "pending_refund_vnd")
}

func TestChargeAllocationSerializationMarksUnsubmitted(t *testing.T) {
	alloc := ChargeAllocationResponse{
		ID:                uuid.New(),
		CommittedItemID:   uuid.New(),
		Modifiers:         make([]CommittedModifierResponse, 0),
		AllocatedQuantity: 2,
		AmountVND:         85_000,
	}
	b, err := json.Marshal(alloc)
	require.NoError(t, err)
	require.Contains(t, string(b), `"submitted":false`)
	require.Contains(t, string(b), `"modifiers":[]`)
}

func TestOrderDraftCarriesCheckTarget(t *testing.T) {
	draft := OrderDraftResponse{
		ID:          uuid.New(),
		State:       "EDITABLE",
		CheckTarget: "CURRENT_UNPAID",
		Items:       make([]DraftItemResponse, 0),
	}
	b, err := json.Marshal(draft)
	require.NoError(t, err)
	require.Contains(t, string(b), `"check_target":"CURRENT_UNPAID"`)
}

func TestPaymentResponseSerialization(t *testing.T) {
	t.Run("a cash payment carries tendered and change", func(t *testing.T) {
		tendered, change := int64(100_000), int64(15_000)
		b, err := json.Marshal(PaymentResponse{
			ID:               uuid.MustParse("11111111-1111-1111-1111-111111111111"),
			Method:           PaymentMethodCash,
			AppliedAmountVND: 85_000,
			CashTenderedVND:  &tendered,
			ChangeDueVND:     &change,
			SalesShiftID:     uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		})
		require.NoError(t, err)
		require.Contains(t, string(b), `"cash_tendered_vnd":100000`)
		require.Contains(t, string(b), `"change_due_vnd":15000`)
		require.NotContains(t, string(b), "transaction_reference")
	})

	t.Run("a manual QR payment carries no cash fields", func(t *testing.T) {
		ref := "FT24012345"
		b, err := json.Marshal(PaymentResponse{
			Method:               PaymentMethodManualQR,
			AppliedAmountVND:     85_000,
			TransactionReference: &ref,
		})
		require.NoError(t, err)
		require.Contains(t, string(b), `"transaction_reference":"FT24012345"`)
		require.NotContains(t, string(b), "cash_tendered_vnd")
		require.NotContains(t, string(b), "change_due_vnd")
	})
}

func TestCheckResponseSerialization5C(t *testing.T) {
	t.Run("an open check omits merged_into_check_id", func(t *testing.T) {
		b, err := json.Marshal(CheckResponse{
			State:      CheckStateOpen,
			ChargeVND:  85_000,
			BalanceVND: 85_000,
			Payments:   []PaymentResponse{},
		})
		require.NoError(t, err)
		require.NotContains(t, string(b), "merged_into_check_id")
		require.Contains(t, string(b), `"payments":[]`)
		require.NotContains(t, string(b), "pending_refund_vnd")
	})

	t.Run("a merged check carries merged_into_check_id", func(t *testing.T) {
		into := uuid.MustParse("33333333-3333-3333-3333-333333333333")
		b, err := json.Marshal(CheckResponse{
			State:             CheckStateMerged,
			MergedIntoCheckID: &into,
			Payments:          []PaymentResponse{},
		})
		require.NoError(t, err)
		require.Contains(t, string(b), `"merged_into_check_id":"33333333-3333-3333-3333-333333333333"`)
	})
}

func TestServiceSessionSerializesNullDraftAfterCommit(t *testing.T) {
	session := ServiceSessionResponse{
		Tables:           make([]SessionTableResponse, 0),
		Checks:           make([]CheckResponse, 0),
		Orders:           make([]OrderResponse, 0),
		PreparationUnits: make([]PreparationUnitResponse, 0),
		Draft:            nil,
	}
	b, err := json.Marshal(session)
	require.NoError(t, err)
	require.Contains(t, string(b), `"draft":null`)
	require.Contains(t, string(b), `"checks":[]`)
}

func TestServiceSessionResponseOrdersAndUnitsSerialize(t *testing.T) {
	// The arrays were []struct{} placeholders through 5A, 5B and 5C. Filling
	// them must not change the shape of the contract: same keys, same types
	// for everything that already existed.
	s := ServiceSessionResponse{
		Orders: []OrderResponse{{
			ID:          uuid.New(),
			SubmittedAt: time.Unix(0, 0).UTC(),
			Items:       []OrderItemResponse{{ID: uuid.New()}},
		}},
		PreparationUnits: []PreparationUnitResponse{{
			ID:         uuid.New(),
			UnitNumber: 1,
			State:      UnitStateQueued,
			Modifiers:  []UnitModifierResponse{},
		}},
	}
	b, err := json.Marshal(s)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(b, &got))
	require.Contains(t, got, "orders")
	require.Contains(t, got, "preparation_units")
	require.Len(t, got["orders"], 1)
	require.Len(t, got["preparation_units"], 1)
}

func TestEmptyOrdersAndUnitsSerializeAsArrays(t *testing.T) {
	b, err := json.Marshal(ServiceSessionResponse{
		Orders:           []OrderResponse{},
		PreparationUnits: []PreparationUnitResponse{},
	})
	require.NoError(t, err)
	require.Contains(t, string(b), `"orders":[]`)
	require.Contains(t, string(b), `"preparation_units":[]`)
}

// Phase 6B adds the Remake metadata to the unit object. The additions are
// additive — priority and remake_of_preparation_unit_id ride alongside the 5D
// fields — and no Preparation alert, credential, or financial field ever
// reaches a unit object: the bar display and the receipt read the same shape
// from both projections, and neither may grow keys owned by another slice.
func TestPreparationUnitResponseRemakeMetadata(t *testing.T) {
	t.Run("an original unit serializes STANDARD with a null remake link", func(t *testing.T) {
		raw, err := json.Marshal(PreparationUnitResponse{
			ID:        uuid.New(),
			State:     UnitStateQueued,
			Modifiers: make([]UnitModifierResponse, 0),
		})
		require.NoError(t, err)

		var decoded map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(raw, &decoded))
		assert.Contains(t, decoded, "priority", "priority must be in the unit contract")
		assert.Contains(t, decoded, "remake_of_preparation_unit_id",
			"the remake link must be in the unit contract")
		assert.JSONEq(t, "null", string(decoded["remake_of_preparation_unit_id"]),
			"an original unit's remake link is a constant null")
	})

	t.Run("a remake unit serializes REMAKE with its exact source link", func(t *testing.T) {
		source := uuid.MustParse("44444444-4444-4444-4444-444444444444")
		raw, err := json.Marshal(PreparationUnitResponse{
			ID:                        uuid.New(),
			State:                     UnitStateQueued,
			Modifiers:                 make([]UnitModifierResponse, 0),
			Priority:                  "REMAKE",
			RemakeOfPreparationUnitID: &source,
		})
		require.NoError(t, err)

		var decoded map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(raw, &decoded))
		assert.JSONEq(t, `"REMAKE"`, string(decoded["priority"]))
		assert.JSONEq(t, `"44444444-4444-4444-4444-444444444444"`,
			string(decoded["remake_of_preparation_unit_id"]))
	})

	t.Run("empty and populated unit objects never leak alerts, credentials, or financial fields", func(t *testing.T) {
		source := uuid.New()
		populated := PreparationUnitResponse{
			ID:                        uuid.New(),
			OrderItemID:               uuid.New(),
			UnitNumber:                2,
			State:                     UnitStateQueued,
			ServiceNumber:             "S00001",
			CategoryName:              "Cà phê",
			ItemName:                  "Cà phê sữa",
			Modifiers:                 make([]UnitModifierResponse, 0),
			Priority:                  "REMAKE",
			RemakeOfPreparationUnitID: &source,
		}

		emptyRaw, err := json.Marshal(PreparationUnitResponse{
			Modifiers: make([]UnitModifierResponse, 0),
		})
		require.NoError(t, err)
		populatedRaw, err := json.Marshal(populated)
		require.NoError(t, err)

		for name, body := range map[string]string{
			"empty unit object":     string(emptyRaw),
			"populated unit object": string(populatedRaw),
		} {
			assert.NotContains(t, body, "alert", "%s must not carry Preparation alert fields", name)
			assert.NotContains(t, body, "acknowledged", "%s must not carry acknowledgment fields", name)
			assert.NotContains(t, body, "pin", "%s must not carry credential fields", name)
			assert.NotContains(t, body, "waste_id", "%s must not carry Waste fact fields", name)
			assert.NotContains(t, body, "charge", "%s must not carry financial fields", name)
			assert.NotContains(t, body, "payment", "%s must not carry financial fields", name)
			assert.NotContains(t, body, "applied_amount", "%s must not carry financial fields", name)
			assert.NotContains(t, body, "balance", "%s must not carry financial fields", name)
			assert.NotContains(t, body, "correction", "%s must not carry correction-history fields", name)
		}
	})
}
