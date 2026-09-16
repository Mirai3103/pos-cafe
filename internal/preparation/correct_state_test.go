// Internal tests for the State Correction command's pure contracts: the
// credential-free fingerprint, the request-order versus lock-order split, and
// the boundary validation the handler performs before any transaction. The
// fingerprint and helpers are package-private, so this file joins the package
// under test (matching domain_test.go); the database-backed behavior lives in
// correct_state_integration_test.go.
package preparation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCorrectStateFingerprintPreservesRequestOrder(t *testing.T) {
	// Deliberately jumbled relative to UUID byte order: the request order
	// drives the fingerprint (and the response), while a separate sorted copy
	// drives the locks.
	requestOrder := []uuid.UUID{correctionUnitID(3), correctionUnitID(1), correctionUnitID(2)}
	cmd := CorrectStateCommand{
		RequestID:          correctionUnitID(999),
		PreparationUnitIDs: requestOrder,
		TargetState:        StateReady,
		Reason:             ReasonStateRecordedInError,
	}
	note := NormalizeCorrectionNote(notePtr("  chốt nhầm trạng thái  "))

	fingerprint := correctStateFingerprintFor(cmd, note)
	require.Equal(t, requestOrder, fingerprint.PreparationUnitIDs,
		"the fingerprint keeps the request order, never the lock order")
	require.Equal(t, StateReady, fingerprint.TargetState)
	require.Equal(t, ReasonStateRecordedInError, fingerprint.Reason)
	require.Equal(t, note, fingerprint.Note)

	// Fingerprinting must not reorder or otherwise mutate the request.
	require.Equal(t, requestOrder, cmd.PreparationUnitIDs,
		"building the fingerprint must not mutate the command's selection")
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

func TestCorrectStateFingerprintNormalizesNote(t *testing.T) {
	ids := []uuid.UUID{correctionUnitID(1)}
	build := func(note *string) correctStateFingerprint {
		return correctStateFingerprintFor(CorrectStateCommand{
			RequestID:          correctionUnitID(999),
			PreparationUnitIDs: ids,
			TargetState:        StateInPreparation,
			Reason:             ReasonStateRecordedInError,
		}, note)
	}
	hashOf := func(fingerprint correctStateFingerprint) string {
		t.Helper()
		hash, err := fpHash(fingerprint)
		require.NoError(t, err)
		return hash
	}

	// Differently padded input fingerprints identically: the fingerprint
	// stands for the normalized note, so padded replays stay replays.
	padded := build(NormalizeCorrectionNote(notePtr("  sai trạng thái  ")))
	trimmed := build(NormalizeCorrectionNote(notePtr("sai trạng thái")))
	require.Equal(t, hashOf(trimmed), hashOf(padded),
		"a padded note and its trimmed form fingerprint identically")

	// A blank note normalizes to an absent one before fingerprinting.
	blank := build(NormalizeCorrectionNote(notePtr("   ")))
	absent := build(nil)
	require.Equal(t, hashOf(absent), hashOf(blank),
		"a blank note and an absent note fingerprint identically")
}

func TestUUIDSortedCopyDrivesLocks(t *testing.T) {
	// First-byte variety proves byte order, not string suffix order:
	// 00000000-…-050 < 0a000000-… < ff000000-….
	high := uuid.MustParse("ff000000-0000-0000-0000-000000000001")
	mid := uuid.MustParse("0a000000-0000-0000-0000-000000000002")
	low := correctionUnitID(50)
	input := []uuid.UUID{high, low, mid}

	sorted := uuidSortedCopy(input)
	require.Equal(t, []uuid.UUID{low, mid, high}, sorted,
		"the copy is sorted in UUID byte order, not request order")
	for i := 1; i < len(sorted); i++ {
		require.Negative(t, bytes.Compare(sorted[i-1][:], sorted[i][:]),
			"the lock order is strictly ascending in UUID byte order")
	}

	// The input keeps its request order: the sorted copy is a separate slice.
	require.Equal(t, []uuid.UUID{high, low, mid}, input,
		"sorting for the locks must not reorder the caller's slice")

	// The copy owns its backing array: mutating it cannot touch the input.
	sorted[0] = uuid.Nil
	require.Equal(t, []uuid.UUID{high, low, mid}, input,
		"the sorted copy must not alias the caller's backing array")
}

func TestCorrectStateBoundary(t *testing.T) {
	// The handler validates before any transaction, so a handler whose runner
	// is nil never reaches the database on a boundary rejection — the nil
	// runner proves the ordering.
	h := NewCorrectStateHandler(nil)

	cmd := func(ids []uuid.UUID) CorrectStateCommand {
		return CorrectStateCommand{
			RequestID:          correctionUnitID(999),
			PreparationUnitIDs: ids,
			TargetState:        StateInPreparation,
			Reason:             ReasonStateRecordedInError,
			ManagerPIN:         "123456",
		}
	}

	t.Run("duplicate ids are rejected and never deduplicated or reordered", func(t *testing.T) {
		dup := correctionUnitID(7)
		ids := []uuid.UUID{correctionUnitID(3), dup, correctionUnitID(1), dup}
		request := cmd(ids)
		_, _, err := h.Handle(context.Background(), Actor{}, request)
		require.ErrorIs(t, err, response.ErrInvalid)
		require.Equal(t, ids, request.PreparationUnitIDs,
			"the rejection must not deduplicate or reorder the selection")
	})

	t.Run("zero and fifty-one ids are rejected", func(t *testing.T) {
		_, _, err := h.Handle(context.Background(), Actor{}, cmd([]uuid.UUID{}))
		require.ErrorIs(t, err, response.ErrInvalid)

		tooMany := make([]uuid.UUID, 0, MaxCorrectionUnits+1)
		for i := 1; i <= MaxCorrectionUnits+1; i++ {
			tooMany = append(tooMany, correctionUnitID(i))
		}
		_, _, err = h.Handle(context.Background(), Actor{}, cmd(tooMany))
		require.ErrorIs(t, err, response.ErrInvalid)
	})

	t.Run("a zero UUID in the selection is rejected", func(t *testing.T) {
		_, _, err := h.Handle(context.Background(), Actor{},
			cmd([]uuid.UUID{correctionUnitID(1), uuid.Nil}))
		require.ErrorIs(t, err, response.ErrInvalid)
	})

	t.Run("a state that is not a correction target is rejected", func(t *testing.T) {
		for _, target := range []string{StateFulfilled, StateCancelled, StateWasted, "", "BREWING"} {
			request := cmd([]uuid.UUID{correctionUnitID(1)})
			request.TargetState = target
			_, _, err := h.Handle(context.Background(), Actor{}, request)
			require.ErrorIs(t, err, response.ErrInvalid, target)
		}
	})

	t.Run("a reason outside the correction catalog is rejected", func(t *testing.T) {
		request := cmd([]uuid.UUID{correctionUnitID(1)})
		request.Reason = ReasonQualityFailure
		_, _, err := h.Handle(context.Background(), Actor{}, request)
		require.ErrorIs(t, err, ErrInvalidReason)
	})

	t.Run("OTHER without a note is rejected", func(t *testing.T) {
		request := cmd([]uuid.UUID{correctionUnitID(1)})
		request.Reason = ReasonOther
		_, _, err := h.Handle(context.Background(), Actor{}, request)
		require.ErrorIs(t, err, ErrInvalidNote)
	})

	t.Run("a missing or malformed PIN shape is rejected", func(t *testing.T) {
		for _, pin := range []string{"", "123", "123456789", "12a4"} {
			request := cmd([]uuid.UUID{correctionUnitID(1)})
			request.ManagerPIN = pin
			_, _, err := h.Handle(context.Background(), Actor{}, request)
			require.ErrorIs(t, err, response.ErrInvalid, pin)
		}
	})
}
