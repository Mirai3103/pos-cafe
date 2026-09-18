package sales

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// voidPaymentTestApproval is a well-formed inline Manager Approval. No test
// ever asserts these values appear in a fingerprint, a result, or an audit
// record.
func voidPaymentTestApproval() ManagerApprovalInput {
	return ManagerApprovalInput{ApproverLoginCode: "MGR001", ManagerPIN: "1234"}
}

// voidPaymentTestCommand returns a valid Payment Void command a test can
// mutate.
func voidPaymentTestCommand() VoidPaymentCommand {
	return VoidPaymentCommand{
		RequestID:       uuid.New(),
		PaymentID:       uuid.New(),
		Reason:          VoidReasonWrongAmount,
		ManagerApproval: voidPaymentTestApproval(),
	}
}

func TestVoidPaymentValidation(t *testing.T) {
	t.Run("accepts every catalog reason", func(t *testing.T) {
		reasons := []struct {
			reason string
			note   *string
		}{
			{VoidReasonDuplicatePayment, nil},
			{VoidReasonWrongAmount, nil},
			{VoidReasonWrongMethod, nil},
			{VoidReasonPaymentRecordedInError, nil},
			{VoidReasonOther, strPtr("nhập nhầm")},
		}
		for _, tc := range reasons {
			cmd := voidPaymentTestCommand()
			cmd.Reason = tc.reason
			assert.NoError(t, ValidateVoidPaymentCommand(cmd, tc.note),
				"reason %s must be accepted", tc.reason)
		}
	})

	t.Run("accepts a 500-rune note after normalization", func(t *testing.T) {
		cmd := voidPaymentTestCommand()
		note := strings.Repeat("x", MaxCorrectionNoteRunes)
		assert.NoError(t, ValidateVoidPaymentCommand(cmd, NormalizeVoidPaymentNote(&note)))
	})

	t.Run("requires the OTHER reason to carry a note", func(t *testing.T) {
		cmd := voidPaymentTestCommand()
		cmd.Reason = VoidReasonOther
		assert.ErrorIs(t, ValidateVoidPaymentCommand(cmd, nil), response.ErrInvalid)

		blank := "   "
		assert.ErrorIs(t, ValidateVoidPaymentCommand(cmd, NormalizeVoidPaymentNote(&blank)),
			response.ErrInvalid)
	})

	rejections := []struct {
		name   string
		mutate func(*VoidPaymentCommand, *string)
	}{
		{"zero request id", func(cmd *VoidPaymentCommand, _ *string) {
			cmd.RequestID = uuid.Nil
		}},
		{"zero payment id", func(cmd *VoidPaymentCommand, _ *string) {
			cmd.PaymentID = uuid.Nil
		}},
		{"unknown reason", func(cmd *VoidPaymentCommand, _ *string) {
			cmd.Reason = "REFUND_REQUEST"
		}},
		{"lowercase reason", func(cmd *VoidPaymentCommand, _ *string) {
			cmd.Reason = "wrong_amount"
		}},
		{"empty reason", func(cmd *VoidPaymentCommand, _ *string) {
			cmd.Reason = ""
		}},
		{"note over 500 runes", func(_ *VoidPaymentCommand, note *string) {
			*note = strings.Repeat("x", MaxCorrectionNoteRunes+1)
		}},
		{"blank approver login code", func(cmd *VoidPaymentCommand, _ *string) {
			cmd.ManagerApproval.ApproverLoginCode = "   "
		}},
		{"malformed manager pin", func(cmd *VoidPaymentCommand, _ *string) {
			cmd.ManagerApproval.ManagerPIN = "12"
		}},
		{"non-numeric manager pin", func(cmd *VoidPaymentCommand, _ *string) {
			cmd.ManagerApproval.ManagerPIN = "abcd"
		}},
	}
	for _, tc := range rejections {
		t.Run("rejects "+tc.name, func(t *testing.T) {
			cmd := voidPaymentTestCommand()
			note := strPtr("một ghi chú")
			tc.mutate(&cmd, note)
			assert.ErrorIs(t, ValidateVoidPaymentCommand(cmd, note), response.ErrInvalid)
		})
	}
}

// TestVoidPaymentFingerprintExcludesCredentials proves the idempotency
// fingerprint stands for the normalized business input alone: rotating the
// approver or their PIN leaves it byte-identical, and no credential value can
// be found in its encoding.
func TestVoidPaymentFingerprintExcludesCredentials(t *testing.T) {
	cmd := voidPaymentTestCommand()
	note := NormalizeVoidPaymentNote(strPtr("  ghi nhầm máy  "))

	first := voidPaymentFingerprintFor(cmd, note)

	rotated := cmd
	rotated.ManagerApproval = ManagerApprovalInput{
		ApproverLoginCode: "OTHER",
		ManagerPIN:        "987654",
	}
	second := voidPaymentFingerprintFor(rotated, note)
	assert.Equal(t, first, second, "credentials must not shape the fingerprint")

	raw, err := json.Marshal(first)
	require.NoError(t, err)
	text := string(raw)
	assert.NotContains(t, text, cmd.ManagerApproval.ApproverLoginCode)
	assert.NotContains(t, text, cmd.ManagerApproval.ManagerPIN)
	assert.Contains(t, text, cmd.PaymentID.String())
	assert.Contains(t, text, "ghi nhầm máy")
}

func TestVoidPaymentNoteNormalization(t *testing.T) {
	assert.Nil(t, NormalizeVoidPaymentNote(nil))

	blank := "   "
	assert.Nil(t, NormalizeVoidPaymentNote(&blank))

	padded := "  ghi chú  "
	normalized := NormalizeVoidPaymentNote(&padded)
	require.NotNil(t, normalized)
	assert.Equal(t, "ghi chú", *normalized)
	assert.Equal(t, "  ghi chú  ", padded, "normalization must not mutate the caller's value")
}
