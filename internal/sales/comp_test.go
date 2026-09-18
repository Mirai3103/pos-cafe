package sales

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// compTestApproval is a well-formed inline Manager Approval. No test ever
// asserts these values appear in a fingerprint, a result, or an audit record.
func compTestApproval() ManagerApprovalInput {
	return ManagerApprovalInput{ApproverLoginCode: "MGR001", ManagerPIN: "1234"}
}

// compTestCommand returns a valid Comp command a test can mutate.
func compTestCommand() CompWasteCommand {
	return CompWasteCommand{
		RequestID:       uuid.New(),
		WasteID:         uuid.New(),
		Reason:          CompReasonCafeError,
		ManagerApproval: compTestApproval(),
	}
}

func TestCompValidation(t *testing.T) {
	t.Run("accepts every catalog reason", func(t *testing.T) {
		reasons := []struct {
			reason string
			note   *string
		}{
			{CompReasonCafeError, nil},
			{CompReasonQualityFailure, nil},
			{CompReasonServiceRecovery, nil},
			{CompReasonOther, strPtr("khách phàn nàn")},
		}
		for _, tc := range reasons {
			cmd := compTestCommand()
			cmd.Reason = tc.reason
			assert.NoError(t, ValidateCompWasteCommand(cmd, tc.note),
				"reason %s must be accepted", tc.reason)
		}
	})

	t.Run("accepts a 500-rune note after normalization", func(t *testing.T) {
		cmd := compTestCommand()
		note := strings.Repeat("x", MaxCorrectionNoteRunes)
		assert.NoError(t, ValidateCompWasteCommand(cmd, NormalizeCompNote(&note)))
	})

	t.Run("requires the OTHER reason to carry a note", func(t *testing.T) {
		cmd := compTestCommand()
		cmd.Reason = CompReasonOther
		assert.ErrorIs(t, ValidateCompWasteCommand(cmd, nil), response.ErrInvalid)

		blank := "   "
		assert.ErrorIs(t, ValidateCompWasteCommand(cmd, NormalizeCompNote(&blank)), response.ErrInvalid)
	})

	rejections := []struct {
		name   string
		mutate func(*CompWasteCommand, *string)
	}{
		{"zero request id", func(cmd *CompWasteCommand, _ *string) {
			cmd.RequestID = uuid.Nil
		}},
		{"zero waste id", func(cmd *CompWasteCommand, _ *string) {
			cmd.WasteID = uuid.Nil
		}},
		{"unknown reason", func(cmd *CompWasteCommand, _ *string) {
			cmd.Reason = "PREPARATION_ERROR"
		}},
		{"lowercase reason", func(cmd *CompWasteCommand, _ *string) {
			cmd.Reason = "cafe_error"
		}},
		{"empty reason", func(cmd *CompWasteCommand, _ *string) {
			cmd.Reason = ""
		}},
		{"note over 500 runes", func(_ *CompWasteCommand, note *string) {
			*note = strings.Repeat("x", MaxCorrectionNoteRunes+1)
		}},
		{"blank approver login code", func(cmd *CompWasteCommand, _ *string) {
			cmd.ManagerApproval.ApproverLoginCode = "   "
		}},
		{"malformed manager pin", func(cmd *CompWasteCommand, _ *string) {
			cmd.ManagerApproval.ManagerPIN = "12"
		}},
		{"non-numeric manager pin", func(cmd *CompWasteCommand, _ *string) {
			cmd.ManagerApproval.ManagerPIN = "abcd"
		}},
	}
	for _, tc := range rejections {
		t.Run("rejects "+tc.name, func(t *testing.T) {
			cmd := compTestCommand()
			note := strPtr("một ghi chú")
			tc.mutate(&cmd, note)
			assert.ErrorIs(t, ValidateCompWasteCommand(cmd, note), response.ErrInvalid)
		})
	}
}

// TestCompFingerprintExcludesCredentials proves the idempotency fingerprint
// stands for the normalized business input alone: rotating the approver or
// their PIN leaves it byte-identical, and no credential value can be found in
// its encoding.
func TestCompFingerprintExcludesCredentials(t *testing.T) {
	cmd := compTestCommand()
	note := NormalizeCompNote(strPtr("  hỏng topping  "))

	first := compWasteFingerprintFor(cmd, note)

	rotated := cmd
	rotated.ManagerApproval = ManagerApprovalInput{
		ApproverLoginCode: "OTHER",
		ManagerPIN:        "987654",
	}
	second := compWasteFingerprintFor(rotated, note)
	assert.Equal(t, first, second, "credentials must not shape the fingerprint")

	raw, err := json.Marshal(first)
	require.NoError(t, err)
	text := string(raw)
	assert.NotContains(t, text, cmd.ManagerApproval.ApproverLoginCode)
	assert.NotContains(t, text, cmd.ManagerApproval.ManagerPIN)
	assert.Contains(t, text, cmd.WasteID.String())
	assert.Contains(t, text, "hỏng topping")
}

func TestCompNoteNormalization(t *testing.T) {
	assert.Nil(t, NormalizeCompNote(nil))

	blank := "   "
	assert.Nil(t, NormalizeCompNote(&blank))

	padded := "  ghi chú  "
	normalized := NormalizeCompNote(&padded)
	require.NotNil(t, normalized)
	assert.Equal(t, "ghi chú", *normalized)
	assert.Equal(t, "  ghi chú  ", padded, "normalization must not mutate the caller's value")
}

// TestCompResultJSON pins the discriminated result contract: exactly one of
// the live and post-sale branches serializes, the post-sale history list is a
// real array rather than null, and no Manager Approval credential can appear.
func TestCompResultJSON(t *testing.T) {
	comp := CompResponse{
		ID:                        uuid.New(),
		WasteID:                   uuid.New(),
		PreparationUnitID:         uuid.New(),
		ChargeAdjustmentID:        uuid.New(),
		AmountVND:                 25000,
		Reason:                    CompReasonCafeError,
		ActorStaffIdentityID:      uuid.New(),
		ApprovedByStaffIdentityID: uuid.New(),
		OccurredAt:                time.Now(),
	}

	t.Run("live branch carries the service session only", func(t *testing.T) {
		sessionID := uuid.New()
		live := CompResult{
			Scope:          CompScopeLiveCheck,
			Comp:           comp,
			ServiceSession: &ServiceSessionResponse{ID: sessionID},
		}
		raw, err := json.Marshal(live)
		require.NoError(t, err)
		text := string(raw)

		assert.Contains(t, text, `"scope":"LIVE_CHECK"`)
		assert.Contains(t, text, `"service_session":{`)
		assert.Contains(t, text, sessionID.String())
		assert.NotContains(t, text, `"completed_sale_id"`)
		assert.NotContains(t, text, `"outstanding_post_sale_refund_vnd"`)
		assert.NotContains(t, text, `"post_sale_corrections"`)
		assert.NotContains(t, text, `"service_session":null`)
		assert.NotContains(t, text, `"manager_pin"`)
		assert.NotContains(t, text, `"approver_login_code"`)
	})

	t.Run("post-sale branch carries the sale and non-null history", func(t *testing.T) {
		saleID := uuid.New()
		outstanding := int64(25000)
		postSale := CompResult{
			Scope:                        CompScopePostSale,
			Comp:                         comp,
			CompletedSaleID:              &saleID,
			OutstandingPostSaleRefundVND: &outstanding,
			PostSaleCorrections: []PostSaleCorrectionResponse{{
				Adjustment: ChargeAdjustmentResponse{
					ID:                     comp.ChargeAdjustmentID,
					Kind:                   ChargeAdjustmentKindComp,
					Scope:                  CompScopePostSale,
					PreparationUnitID:      comp.PreparationUnitID,
					PreparationWasteID:     &comp.WasteID,
					ChargeAllocationID:     uuid.New(),
					CompletedSaleID:        &saleID,
					SalesShiftID:           uuid.New(),
					AmountVND:              25000,
					RemainingRefundableVND: 25000,
					CreatedAt:              comp.OccurredAt,
				},
				Comp:                 comp,
				Refunds:              []RefundResponse{},
				OutstandingRefundVND: 25000,
			}},
		}
		raw, err := json.Marshal(postSale)
		require.NoError(t, err)
		text := string(raw)

		assert.Contains(t, text, `"scope":"POST_SALE"`)
		assert.Contains(t, text, `"completed_sale_id":"`+saleID.String()+`"`)
		assert.Contains(t, text, `"outstanding_post_sale_refund_vnd":25000`)
		assert.Contains(t, text, `"post_sale_corrections":[`)
		assert.NotContains(t, text, `"post_sale_corrections":null`)
		assert.Contains(t, text, `"refunds":[]`, "the entry's refunds must be [] not null")
		assert.Contains(t, text, `"outstanding_refund_vnd":25000`)
		assert.NotContains(t, text, `"service_session"`)
		assert.NotContains(t, text, `"manager_pin"`)
		assert.NotContains(t, text, `"approver_login_code"`)
	})
}

// strPtr returns a pointer to v, for optional command fields.
func strPtr(v string) *string { return &v }
