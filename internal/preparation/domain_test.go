// Internal domain tests: the Phase 6B fingerprint contract is
// package-private, so this file joins the package under test (matching
// bulk_advance_test.go and queue_test.go).
package preparation

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestNextState(t *testing.T) {
	cases := []struct {
		from string
		next string
		ok   bool
	}{
		{StateQueued, StateInPreparation, true},
		{StateInPreparation, StateReady, true},
		{StateReady, StateFulfilled, true},
		{StateFulfilled, "", false},
		{StateCancelled, "", false},
		{StateWasted, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.from, func(t *testing.T) {
			next, ok := NextState(tc.from)
			require.Equal(t, tc.ok, ok)
			require.Equal(t, tc.next, next)
		})
	}
}

func TestIsLegalAdvance(t *testing.T) {
	require.True(t, IsLegalAdvance(StateQueued, StateInPreparation))
	require.True(t, IsLegalAdvance(StateInPreparation, StateReady))
	require.True(t, IsLegalAdvance(StateReady, StateFulfilled))

	// Skipping a step is not an advance.
	require.False(t, IsLegalAdvance(StateQueued, StateReady))
	require.False(t, IsLegalAdvance(StateQueued, StateFulfilled))
	// Going backwards is a State Correction, which is Phase 6.
	require.False(t, IsLegalAdvance(StateReady, StateInPreparation))
	// Terminal states are terminal.
	require.False(t, IsLegalAdvance(StateFulfilled, StateFulfilled))
	// Cancellation and Waste are Phase 6 commands, not advances.
	require.False(t, IsLegalAdvance(StateQueued, StateCancelled))
	require.False(t, IsLegalAdvance(StateInPreparation, StateWasted))
}

func TestIsAdvanceTarget(t *testing.T) {
	for _, s := range []string{StateInPreparation, StateReady, StateFulfilled} {
		require.True(t, IsAdvanceTarget(s), s)
	}
	for _, s := range []string{StateQueued, StateCancelled, StateWasted, "", "BREWING"} {
		require.False(t, IsAdvanceTarget(s), s)
	}
}

func TestWasteReason(t *testing.T) {
	for _, reason := range []string{
		ReasonPreparationError, ReasonQualityFailure, ReasonCustomerRequest, ReasonOther,
	} {
		require.NoError(t, ValidateWasteReason(reason), reason)
	}
	// Everything outside the Waste catalog is rejected, including the
	// correction-only and reserved reasons.
	for _, reason := range []string{
		"", "SPOILED", ReasonStateRecordedInError,
	} {
		err := ValidateWasteReason(reason)
		require.ErrorIs(t, err, ErrInvalidReason, reason)
	}
}

func TestRemakeReason(t *testing.T) {
	for _, reason := range []string{ReasonPreparationError, ReasonQualityFailure, ReasonOther} {
		require.NoError(t, ValidateRemakeReason(reason), reason)
	}
	// The Remake catalog is narrower than Waste: CUSTOMER_REQUEST is waste-only.
	for _, reason := range []string{
		"", "SPOILED", ReasonCustomerRequest, ReasonStateRecordedInError,
	} {
		err := ValidateRemakeReason(reason)
		require.ErrorIs(t, err, ErrInvalidReason, reason)
	}
}

func TestCorrectionReason(t *testing.T) {
	for _, reason := range []string{ReasonStateRecordedInError, ReasonOther} {
		require.NoError(t, ValidateCorrectionReason(reason), reason)
	}
	for _, reason := range []string{
		"", "SPOILED", ReasonPreparationError, ReasonQualityFailure, ReasonCustomerRequest,
	} {
		err := ValidateCorrectionReason(reason)
		require.ErrorIs(t, err, ErrInvalidReason, reason)
	}
}

func TestNormalizeCorrectionNote(t *testing.T) {
	cases := []struct {
		name string
		in   *string
		want *string
	}{
		{"nil stays nil", nil, nil},
		{"empty becomes nil", notePtr(""), nil},
		{"blank becomes nil", notePtr(" \t\r\n "), nil},
		{"surrounding whitespace is trimmed", notePtr("  ít đá hơn  "), notePtr("ít đá hơn")},
		{"internal whitespace is preserved", notePtr("a  b"), notePtr("a  b")},
		{"unicode whitespace is trimmed", notePtr("\u00a0sua\u3000"), notePtr("sua")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, NormalizeCorrectionNote(tc.in))
		})
	}
}

func TestValidateCorrectionNote(t *testing.T) {
	// Exactly 500 code points is accepted, for both single-byte and
	// multi-byte runes — the boundary counts runes, not bytes.
	note500 := strings.Repeat("a", MaxNoteRunes)
	require.Equal(t, MaxNoteRunes, utf8.RuneCountInString(note500))
	require.NoError(t, ValidateCorrectionNote(ReasonPreparationError, &note500))

	note500Unicode := strings.Repeat("ể", MaxNoteRunes)
	require.Equal(t, MaxNoteRunes, utf8.RuneCountInString(note500Unicode))
	require.NoError(t, ValidateCorrectionNote(ReasonOther, &note500Unicode))

	// 501 code points is rejected.
	note501 := strings.Repeat("ể", MaxNoteRunes+1)
	err := ValidateCorrectionNote(ReasonOther, &note501)
	require.ErrorIs(t, err, ErrInvalidNote)

	// OTHER requires a present note.
	err = ValidateCorrectionNote(ReasonOther, nil)
	require.ErrorIs(t, err, ErrInvalidNote)

	// A blank note normalizes to nil before validation, so OTHER rejects it.
	blank := "   "
	err = ValidateCorrectionNote(ReasonOther, NormalizeCorrectionNote(&blank))
	require.ErrorIs(t, err, ErrInvalidNote)

	// Non-OTHER reasons need no note.
	require.NoError(t, ValidateCorrectionNote(ReasonPreparationError, nil))
	require.NoError(t, ValidateCorrectionNote(ReasonStateRecordedInError, nil))
}

func TestRequiredPriorState(t *testing.T) {
	cases := []struct {
		target string
		prior  string
		ok     bool
	}{
		{StateQueued, StateInPreparation, true},
		{StateInPreparation, StateReady, true},
		{StateReady, StateFulfilled, true},
		// Every other target is rejected: terminal and exceptional states
		// are not correction targets.
		{StateFulfilled, "", false},
		{StateCancelled, "", false},
		{StateWasted, "", false},
		{"", "", false},
		{"BREWING", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.target, func(t *testing.T) {
			prior, ok := RequiredPriorState(tc.target)
			require.Equal(t, tc.ok, ok)
			require.Equal(t, tc.prior, prior)
		})
	}
}

func TestValidateCorrectState(t *testing.T) {
	validCmd := func(ids []uuid.UUID) CorrectStateCommand {
		return CorrectStateCommand{
			RequestID:          correctionUnitID(999),
			PreparationUnitIDs: ids,
			TargetState:        StateInPreparation,
			Reason:             ReasonStateRecordedInError,
			ManagerPIN:         "123456",
		}
	}

	t.Run("accepts one unit", func(t *testing.T) {
		require.NoError(t, ValidateCorrectStateCommand(validCmd([]uuid.UUID{correctionUnitID(1)})))
	})

	t.Run("accepts fifty units", func(t *testing.T) {
		ids := make([]uuid.UUID, 0, MaxCorrectionUnits)
		for i := 1; i <= MaxCorrectionUnits; i++ {
			ids = append(ids, correctionUnitID(i))
		}
		require.NoError(t, ValidateCorrectStateCommand(validCmd(ids)))
	})

	t.Run("rejects zero ids", func(t *testing.T) {
		err := ValidateCorrectStateCommand(validCmd([]uuid.UUID{}))
		require.ErrorIs(t, err, response.ErrInvalid)
	})

	t.Run("rejects more than fifty ids", func(t *testing.T) {
		ids := make([]uuid.UUID, 0, MaxCorrectionUnits+1)
		for i := 1; i <= MaxCorrectionUnits+1; i++ {
			ids = append(ids, correctionUnitID(i))
		}
		err := ValidateCorrectStateCommand(validCmd(ids))
		require.ErrorIs(t, err, response.ErrInvalid)
	})

	t.Run("rejects duplicate ids instead of deduplicating", func(t *testing.T) {
		dup := correctionUnitID(7)
		ids := []uuid.UUID{correctionUnitID(1), dup, correctionUnitID(3), dup}
		cmd := validCmd(ids)
		err := ValidateCorrectStateCommand(cmd)
		require.ErrorIs(t, err, response.ErrInvalid)
		// The selection is never silently deduplicated or reordered.
		require.Equal(t, []uuid.UUID{correctionUnitID(1), dup, correctionUnitID(3), dup}, cmd.PreparationUnitIDs)
	})

	t.Run("rejects a zero UUID in the selection", func(t *testing.T) {
		err := ValidateCorrectStateCommand(validCmd([]uuid.UUID{correctionUnitID(1), uuid.Nil}))
		require.ErrorIs(t, err, response.ErrInvalid)
	})

	t.Run("rejects a nil request_id", func(t *testing.T) {
		cmd := validCmd([]uuid.UUID{correctionUnitID(1)})
		cmd.RequestID = uuid.Nil
		err := ValidateCorrectStateCommand(cmd)
		require.ErrorIs(t, err, response.ErrInvalid)
	})

	t.Run("rejects every state that is not a correction target", func(t *testing.T) {
		for _, target := range []string{StateFulfilled, StateCancelled, StateWasted, "", "BREWING"} {
			cmd := validCmd([]uuid.UUID{correctionUnitID(1)})
			cmd.TargetState = target
			err := ValidateCorrectStateCommand(cmd)
			require.ErrorIs(t, err, response.ErrInvalid, target)
		}
	})

	t.Run("rejects a reason outside the State Correction catalog", func(t *testing.T) {
		cmd := validCmd([]uuid.UUID{correctionUnitID(1)})
		cmd.Reason = ReasonPreparationError
		err := ValidateCorrectStateCommand(cmd)
		require.ErrorIs(t, err, ErrInvalidReason)
	})

	t.Run("rejects OTHER without a note", func(t *testing.T) {
		cmd := validCmd([]uuid.UUID{correctionUnitID(1)})
		cmd.Reason = ReasonOther
		err := ValidateCorrectStateCommand(cmd)
		require.ErrorIs(t, err, ErrInvalidNote)
	})

	t.Run("accepts OTHER with a note and rejects an over-long one", func(t *testing.T) {
		cmd := validCmd([]uuid.UUID{correctionUnitID(1)})
		cmd.Reason = ReasonOther
		cmd.Note = notePtr("  chốt sai trạng thái  ")
		require.NoError(t, ValidateCorrectStateCommand(cmd))

		cmd.Note = notePtr(strings.Repeat("ể", MaxNoteRunes+1))
		err := ValidateCorrectStateCommand(cmd)
		require.ErrorIs(t, err, ErrInvalidNote)
	})

	t.Run("rejects a missing or malformed PIN shape at the boundary", func(t *testing.T) {
		for _, pin := range []string{"", "123", "123456789", "12a4", "12 4"} {
			cmd := validCmd([]uuid.UUID{correctionUnitID(1)})
			cmd.ManagerPIN = pin
			err := ValidateCorrectStateCommand(cmd)
			require.ErrorIs(t, err, response.ErrInvalid, pin)
		}
	})
}

func TestCorrectStateFingerprintExcludesCredentials(t *testing.T) {
	note := NormalizeCorrectionNote(notePtr("  chốt nhầm trạng thái  "))
	fingerprint := correctStateFingerprint{
		PreparationUnitIDs: []uuid.UUID{correctionUnitID(1), correctionUnitID(2)},
		TargetState:        StateReady,
		Reason:             ReasonStateRecordedInError,
		Note:               note,
	}
	raw, err := json.Marshal(fingerprint)
	require.NoError(t, err)

	// The fingerprint carries exactly the normalized business input.
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &fields))
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	require.ElementsMatch(t,
		[]string{"preparation_unit_ids", "target_state", "reason", "note"}, keys)
	require.NotContains(t, string(raw), "manager_pin")

	// The PIN value exists only on the request command; the marshaled
	// fingerprint never contains it.
	const samplePin = "43219876"
	cmd := CorrectStateCommand{
		RequestID:          correctionUnitID(999),
		PreparationUnitIDs: []uuid.UUID{correctionUnitID(1), correctionUnitID(2)},
		TargetState:        StateReady,
		Reason:             ReasonStateRecordedInError,
		Note:               note,
		ManagerPIN:         samplePin,
	}
	cmdRaw, err := json.Marshal(cmd)
	require.NoError(t, err)
	require.Contains(t, string(cmdRaw), fmt.Sprintf(`"manager_pin":%q`, samplePin))
	require.NotContains(t, string(raw), samplePin)
}

// notePtr returns a pointer to s; a test helper for optional notes.
func notePtr(s string) *string { return &s }

// correctionUnitID builds a deterministic non-zero UUID for selection tests.
func correctionUnitID(i int) uuid.UUID {
	return uuid.MustParse(fmt.Sprintf("00000000-0000-0000-0000-%012d", i))
}
