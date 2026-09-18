package sales

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// refundTestApproval is a well-formed inline Manager Approval. No test ever
// asserts these values appear in a fingerprint, a result, or an audit record.
func refundTestApproval() ManagerApprovalInput {
	return ManagerApprovalInput{ApproverLoginCode: "MGR001", ManagerPIN: "1234"}
}

// refundTestCommand returns a valid Cash Refund command with one Payment and
// one Charge Adjustment allocation of equal amount. A test can mutate it.
func refundTestCommand() RecordRefundCommand {
	return RecordRefundCommand{
		RequestID: uuid.New(),
		CheckID:   uuid.New(),
		Method:    RefundMethodCash,
		AdjustmentAllocations: []RefundAdjustmentAllocationInput{
			{ChargeAdjustmentID: uuid.New(), AmountVND: 48000},
		},
		PaymentAllocations: []RefundPaymentAllocationInput{
			{PaymentID: uuid.New(), AmountVND: 48000},
		},
		Reason:          RefundReasonCustomerRequest,
		ManagerApproval: refundTestApproval(),
	}
}

// TestValidateRefund pins the boundary contract: every malformed allocation,
// method, reason, note, approval, or unequal sum is rejected before a request
// id is consumed, and a guarded sum overflow stays a monetary range failure.
func TestValidateRefund(t *testing.T) {
	validReasons := []struct {
		reason string
		note   *string
	}{
		{RefundReasonCustomerRequest, nil},
		{RefundReasonItemUnavailable, nil},
		{RefundReasonCafeError, nil},
		{RefundReasonOther, strPtr("khách đổi món")},
	}
	for _, tc := range validReasons {
		t.Run("accepts reason "+tc.reason, func(t *testing.T) {
			cmd := refundTestCommand()
			cmd.Reason = tc.reason
			assert.NoError(t, ValidateRecordRefundCommand(cmd, tc.note))
		})
	}

	t.Run("accepts a manual QR refund", func(t *testing.T) {
		cmd := refundTestCommand()
		cmd.Method = RefundMethodManualQR
		assert.NoError(t, ValidateRecordRefundCommand(cmd, nil))
	})

	t.Run("accepts a 500-rune note after normalization", func(t *testing.T) {
		cmd := refundTestCommand()
		note := strings.Repeat("x", MaxCorrectionNoteRunes)
		assert.NoError(t, ValidateRecordRefundCommand(cmd, NormalizeRefundNote(&note)))
	})

	t.Run("requires the OTHER reason to carry a note", func(t *testing.T) {
		cmd := refundTestCommand()
		cmd.Reason = RefundReasonOther
		assert.ErrorIs(t, ValidateRecordRefundCommand(cmd, nil), response.ErrInvalid)

		blank := "   "
		assert.ErrorIs(t, ValidateRecordRefundCommand(cmd, NormalizeRefundNote(&blank)),
			response.ErrInvalid)
	})

	rejections := []struct {
		name   string
		mutate func(*RecordRefundCommand, *string)
	}{
		{"zero request id", func(cmd *RecordRefundCommand, _ *string) {
			cmd.RequestID = uuid.Nil
		}},
		{"zero check id", func(cmd *RecordRefundCommand, _ *string) {
			cmd.CheckID = uuid.Nil
		}},
		{"empty payment allocations", func(cmd *RecordRefundCommand, _ *string) {
			cmd.PaymentAllocations = nil
		}},
		{"empty adjustment allocations", func(cmd *RecordRefundCommand, _ *string) {
			cmd.AdjustmentAllocations = nil
		}},
		{"zero payment id", func(cmd *RecordRefundCommand, _ *string) {
			cmd.PaymentAllocations[0].PaymentID = uuid.Nil
		}},
		{"zero adjustment id", func(cmd *RecordRefundCommand, _ *string) {
			cmd.AdjustmentAllocations[0].ChargeAdjustmentID = uuid.Nil
		}},
		{"duplicate payment ids", func(cmd *RecordRefundCommand, _ *string) {
			cmd.PaymentAllocations = append(cmd.PaymentAllocations, cmd.PaymentAllocations[0])
		}},
		{"duplicate adjustment ids", func(cmd *RecordRefundCommand, _ *string) {
			cmd.AdjustmentAllocations = append(cmd.AdjustmentAllocations,
				cmd.AdjustmentAllocations[0])
		}},
		{"zero payment amount", func(cmd *RecordRefundCommand, _ *string) {
			cmd.PaymentAllocations[0].AmountVND = 0
		}},
		{"negative payment amount", func(cmd *RecordRefundCommand, _ *string) {
			cmd.PaymentAllocations[0].AmountVND = -1
		}},
		{"zero adjustment amount", func(cmd *RecordRefundCommand, _ *string) {
			cmd.AdjustmentAllocations[0].AmountVND = 0
		}},
		{"negative adjustment amount", func(cmd *RecordRefundCommand, _ *string) {
			cmd.AdjustmentAllocations[0].AmountVND = -1
		}},
		{"unequal allocation sums", func(cmd *RecordRefundCommand, _ *string) {
			cmd.AdjustmentAllocations[0].AmountVND = 47000
		}},
		{"unknown method", func(cmd *RecordRefundCommand, _ *string) {
			cmd.Method = "CARD"
		}},
		{"lowercase method", func(cmd *RecordRefundCommand, _ *string) {
			cmd.Method = "cash"
		}},
		{"empty method", func(cmd *RecordRefundCommand, _ *string) {
			cmd.Method = ""
		}},
		{"unknown reason", func(cmd *RecordRefundCommand, _ *string) {
			cmd.Reason = "QUALITY_FAILURE"
		}},
		{"empty reason", func(cmd *RecordRefundCommand, _ *string) {
			cmd.Reason = ""
		}},
		{"note over 500 runes", func(_ *RecordRefundCommand, note *string) {
			*note = strings.Repeat("x", MaxCorrectionNoteRunes+1)
		}},
		{"blank approver login code", func(cmd *RecordRefundCommand, _ *string) {
			cmd.ManagerApproval.ApproverLoginCode = "   "
		}},
		{"malformed manager pin", func(cmd *RecordRefundCommand, _ *string) {
			cmd.ManagerApproval.ManagerPIN = "12"
		}},
		{"non-numeric manager pin", func(cmd *RecordRefundCommand, _ *string) {
			cmd.ManagerApproval.ManagerPIN = "abcd"
		}},
	}
	for _, tc := range rejections {
		t.Run("rejects "+tc.name, func(t *testing.T) {
			cmd := refundTestCommand()
			note := strPtr("một ghi chú")
			tc.mutate(&cmd, note)
			assert.ErrorIs(t, ValidateRecordRefundCommand(cmd, note), response.ErrInvalid)
		})
	}

	t.Run("rejects payment allocation sum overflow as a monetary range failure", func(t *testing.T) {
		cmd := refundTestCommand()
		cmd.PaymentAllocations = []RefundPaymentAllocationInput{
			{PaymentID: uuid.New(), AmountVND: math.MaxInt64},
			{PaymentID: uuid.New(), AmountVND: math.MaxInt64},
		}
		assert.ErrorIs(t, ValidateRecordRefundCommand(cmd, nil), ErrCheckChargeOutOfRange)
	})

	t.Run("rejects adjustment allocation sum overflow", func(t *testing.T) {
		cmd := refundTestCommand()
		cmd.AdjustmentAllocations = []RefundAdjustmentAllocationInput{
			{ChargeAdjustmentID: uuid.New(), AmountVND: math.MaxInt64},
			{ChargeAdjustmentID: uuid.New(), AmountVND: math.MaxInt64},
		}
		cmd.PaymentAllocations = []RefundPaymentAllocationInput{
			{PaymentID: uuid.New(), AmountVND: math.MaxInt64 - 1},
			{PaymentID: uuid.New(), AmountVND: math.MaxInt64 - 1},
		}
		assert.ErrorIs(t, ValidateRecordRefundCommand(cmd, nil), ErrCheckChargeOutOfRange)
	})
}

// TestNormalizeRefundAllocations proves both collections are ordered by source
// UUID for fingerprinting, locking, and response order, without mutating the
// caller's slices, so a reordered but otherwise identical request replays.
func TestNormalizeRefundAllocations(t *testing.T) {
	first := uuid.MustParse("00000000-0000-0000-0000-00000000000a")
	second := uuid.MustParse("00000000-0000-0000-0000-00000000000b")
	third := uuid.MustParse("00000000-0000-0000-0000-00000000000c")

	cmd := RecordRefundCommand{
		RequestID: uuid.New(),
		CheckID:   uuid.New(),
		Method:    RefundMethodCash,
		AdjustmentAllocations: []RefundAdjustmentAllocationInput{
			{ChargeAdjustmentID: third, AmountVND: 3000},
			{ChargeAdjustmentID: first, AmountVND: 1000},
			{ChargeAdjustmentID: second, AmountVND: 2000},
		},
		PaymentAllocations: []RefundPaymentAllocationInput{
			{PaymentID: second, AmountVND: 2000},
			{PaymentID: third, AmountVND: 3000},
			{PaymentID: first, AmountVND: 1000},
		},
		Reason:          RefundReasonCustomerRequest,
		ManagerApproval: refundTestApproval(),
	}

	normalized := NormalizeRefundAllocations(cmd)

	require.Len(t, normalized.PaymentAllocations, 3)
	assert.Equal(t, first, normalized.PaymentAllocations[0].PaymentID)
	assert.Equal(t, second, normalized.PaymentAllocations[1].PaymentID)
	assert.Equal(t, third, normalized.PaymentAllocations[2].PaymentID)

	require.Len(t, normalized.AdjustmentAllocations, 3)
	assert.Equal(t, first, normalized.AdjustmentAllocations[0].ChargeAdjustmentID)
	assert.Equal(t, second, normalized.AdjustmentAllocations[1].ChargeAdjustmentID)
	assert.Equal(t, third, normalized.AdjustmentAllocations[2].ChargeAdjustmentID)

	// The caller's command keeps its request order; normalization copies.
	assert.Equal(t, second, cmd.PaymentAllocations[0].PaymentID)
	assert.Equal(t, third, cmd.AdjustmentAllocations[0].ChargeAdjustmentID)

	// A reordered duplicate normalizes to the same fingerprint.
	reordered := cmd
	reordered.PaymentAllocations = []RefundPaymentAllocationInput{
		cmd.PaymentAllocations[2], cmd.PaymentAllocations[0], cmd.PaymentAllocations[1],
	}
	reordered.AdjustmentAllocations = []RefundAdjustmentAllocationInput{
		cmd.AdjustmentAllocations[1], cmd.AdjustmentAllocations[2], cmd.AdjustmentAllocations[0],
	}
	assert.Equal(t, refundFingerprintFor(cmd, nil), refundFingerprintFor(reordered, nil))

	// They differ only in method: the fingerprint must not collapse that.
	otherMethod := reordered
	otherMethod.Method = RefundMethodManualQR
	assert.NotEqual(t, refundFingerprintFor(cmd, nil), refundFingerprintFor(otherMethod, nil))
}

// TestRefundFingerprintExcludesCredentials proves the idempotency fingerprint
// stands for the normalized business input alone: rotating the approver or
// their PIN leaves it byte-identical, and no credential value can be found in
// its encoding.
func TestRefundFingerprintExcludesCredentials(t *testing.T) {
	cmd := refundTestCommand()
	note := NormalizeRefundNote(strPtr("  trả món  "))

	first := refundFingerprintFor(cmd, note)

	rotated := cmd
	rotated.ManagerApproval = ManagerApprovalInput{
		ApproverLoginCode: "OTHER",
		ManagerPIN:        "987654",
	}
	second := refundFingerprintFor(rotated, note)
	assert.Equal(t, first, second, "credentials must not shape the fingerprint")

	raw, err := json.Marshal(first)
	require.NoError(t, err)
	text := string(raw)
	assert.NotContains(t, text, cmd.ManagerApproval.ApproverLoginCode)
	assert.NotContains(t, text, cmd.ManagerApproval.ManagerPIN)
	assert.Contains(t, text, cmd.CheckID.String())
	assert.Contains(t, text, cmd.PaymentAllocations[0].PaymentID.String())
	assert.Contains(t, text, cmd.AdjustmentAllocations[0].ChargeAdjustmentID.String())
	assert.Contains(t, text, "trả món")
}

// TestRefundNoteNormalization mirrors the Comp note contract for the Refund
// catalog: trim, collapse a blank to nil, and never mutate the caller's value.
func TestRefundNoteNormalization(t *testing.T) {
	assert.Nil(t, NormalizeRefundNote(nil))

	blank := "   "
	assert.Nil(t, NormalizeRefundNote(&blank))

	padded := "  ghi chú  "
	normalized := NormalizeRefundNote(&padded)
	require.NotNil(t, normalized)
	assert.Equal(t, "ghi chú", *normalized)
	assert.Equal(t, "  ghi chú  ", padded, "normalization must not mutate the caller's value")
}

// TestRefundResultJSON pins the discriminated result contract: exactly one of
// the live and post-sale branches serializes, every collection is a real array
// rather than null, and no Manager Approval credential can appear.
func TestRefundResultJSON(t *testing.T) {
	refund := RefundResponse{
		ID:                        uuid.New(),
		CheckID:                   uuid.New(),
		SalesShiftID:              uuid.New(),
		Method:                    RefundMethodCash,
		AmountVND:                 48000,
		State:                     RefundStateCompleted,
		Reason:                    RefundReasonCustomerRequest,
		ActorStaffIdentityID:      uuid.New(),
		ApprovedByStaffIdentityID: uuid.New(),
		CreatedAt:                 time.Now(),
		PaymentAllocations: []RefundAllocationResponse{
			{ID: uuid.New(), AmountVND: 48000},
		},
		AdjustmentAllocations: []RefundAllocationResponse{
			{ID: uuid.New(), AmountVND: 48000},
		},
	}

	t.Run("live branch carries the service session only", func(t *testing.T) {
		sessionID := uuid.New()
		live := RefundResult{
			Scope:          CompScopeLiveCheck,
			Refund:         refund,
			ServiceSession: &ServiceSessionResponse{ID: sessionID},
		}
		raw, err := json.Marshal(live)
		require.NoError(t, err)
		text := string(raw)

		assert.Contains(t, text, `"scope":"LIVE_CHECK"`)
		assert.Contains(t, text, `"service_session":{`)
		assert.Contains(t, text, sessionID.String())
		assert.NotContains(t, text, `"completed_sale_id"`)
		assert.NotContains(t, text, `"post_sale_corrections"`)
		assert.NotContains(t, text, `"service_session":null`)
		assert.NotContains(t, text, `"payment_allocations":null`)
		assert.NotContains(t, text, `"adjustment_allocations":null`)
		assert.NotContains(t, text, `"manager_pin"`)
		assert.NotContains(t, text, `"approver_login_code"`)
	})

	t.Run("post-sale branch carries the sale and non-null history", func(t *testing.T) {
		saleID := uuid.New()
		adjustmentID := uuid.New()
		postSale := RefundResult{
			Scope:           CompScopePostSale,
			Refund:          refund,
			CompletedSaleID: &saleID,
			PostSaleCorrections: []PostSaleCorrectionResponse{{
				Adjustment: ChargeAdjustmentResponse{
					ID:                     adjustmentID,
					Kind:                   ChargeAdjustmentKindComp,
					Scope:                  CompScopePostSale,
					CompletedSaleID:        &saleID,
					AmountVND:              48000,
					RemainingRefundableVND: 0,
					CreatedAt:              refund.CreatedAt,
				},
				Comp:                 CompResponse{ID: uuid.New(), ChargeAdjustmentID: adjustmentID},
				Refunds:              []RefundResponse{refund},
				OutstandingRefundVND: 0,
			}},
		}
		raw, err := json.Marshal(postSale)
		require.NoError(t, err)
		text := string(raw)

		assert.Contains(t, text, `"scope":"POST_SALE"`)
		assert.Contains(t, text, `"completed_sale_id":"`+saleID.String()+`"`)
		assert.Contains(t, text, `"post_sale_corrections":[`)
		assert.NotContains(t, text, `"post_sale_corrections":null`)
		assert.Contains(t, text, `"refunds":[`)
		assert.NotContains(t, text, `"refunds":null`)
		assert.NotContains(t, text, `"service_session"`)
		assert.NotContains(t, text, `"manager_pin"`)
		assert.NotContains(t, text, `"approver_login_code"`)
	})
}
