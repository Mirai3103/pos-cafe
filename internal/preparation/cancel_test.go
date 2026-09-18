// Internal tests for the Cancellation/Change command's pure contracts: the
// credential-free, UUID-sorted fingerprint, the request-order versus
// lock-order split, and the boundary validation the handler performs before
// any transaction. The fingerprint and validators are package-private, so this
// file joins the package under test (matching correct_state_test.go); the
// database-backed behavior lives in cancel_integration_test.go.
package preparation

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// cancelFingerprintHashOf hashes a fingerprint through the executor's own
// fingerprint function, so the tests pin exactly what the idempotency claim
// compares.
func cancelFingerprintHashOf(t *testing.T, fingerprint cancelUnitsFingerprint) string {
	t.Helper()
	hash, err := fpHash(fingerprint)
	require.NoError(t, err)
	return hash
}

func TestValidateCancelUnits(t *testing.T) {
	validCmd := func(ids []uuid.UUID) CancelUnitsCommand {
		return CancelUnitsCommand{
			RequestID:          correctionUnitID(999),
			PreparationUnitIDs: ids,
			Kind:               CancelKindCancellation,
			Reason:             ReasonCustomerRequest,
		}
	}
	one := func() []uuid.UUID { return []uuid.UUID{correctionUnitID(1)} }

	t.Run("accepts one unit", func(t *testing.T) {
		require.NoError(t, ValidateCancelUnitsCommand(validCmd(one()), nil))
	})

	t.Run("accepts fifty units", func(t *testing.T) {
		ids := make([]uuid.UUID, 0, MaxCancellationUnits)
		for i := 1; i <= MaxCancellationUnits; i++ {
			ids = append(ids, correctionUnitID(i))
		}
		require.NoError(t, ValidateCancelUnitsCommand(validCmd(ids), nil))
	})

	t.Run("rejects zero ids", func(t *testing.T) {
		err := ValidateCancelUnitsCommand(validCmd([]uuid.UUID{}), nil)
		require.ErrorIs(t, err, ErrCancellationSelectionInvalid)
	})

	t.Run("rejects more than fifty ids", func(t *testing.T) {
		ids := make([]uuid.UUID, 0, MaxCancellationUnits+1)
		for i := 1; i <= MaxCancellationUnits+1; i++ {
			ids = append(ids, correctionUnitID(i))
		}
		err := ValidateCancelUnitsCommand(validCmd(ids), nil)
		require.ErrorIs(t, err, ErrCancellationSelectionInvalid)
	})

	t.Run("rejects duplicate ids instead of deduplicating", func(t *testing.T) {
		dup := correctionUnitID(7)
		ids := []uuid.UUID{correctionUnitID(1), dup, correctionUnitID(3), dup}
		cmd := validCmd(ids)
		err := ValidateCancelUnitsCommand(cmd, nil)
		require.ErrorIs(t, err, ErrCancellationSelectionInvalid)
		require.Equal(t,
			[]uuid.UUID{correctionUnitID(1), dup, correctionUnitID(3), dup},
			cmd.PreparationUnitIDs,
			"the rejection must not deduplicate or reorder the selection")
	})

	t.Run("rejects a zero UUID in the selection", func(t *testing.T) {
		err := ValidateCancelUnitsCommand(
			validCmd([]uuid.UUID{correctionUnitID(1), uuid.Nil}), nil)
		require.ErrorIs(t, err, ErrCancellationSelectionInvalid)
	})

	t.Run("rejects a nil request_id", func(t *testing.T) {
		cmd := validCmd(one())
		cmd.RequestID = uuid.Nil
		require.Error(t, ValidateCancelUnitsCommand(cmd, nil))
	})

	t.Run("rejects a kind outside the cancellation domain", func(t *testing.T) {
		for _, kind := range []string{"", "WASTE", "cancel", "CHANGE_ORDER"} {
			cmd := validCmd(one())
			cmd.Kind = kind
			require.ErrorIs(t, ValidateCancelUnitsCommand(cmd, nil),
				ErrCancellationSelectionInvalid, kind)
		}
	})

	t.Run("CHANGE requires a replacement order", func(t *testing.T) {
		cmd := validCmd(one())
		cmd.Kind = CancelKindChange
		require.ErrorIs(t, ValidateCancelUnitsCommand(cmd, nil), ErrReplacementOrderRequired)

		replacement := correctionUnitID(500)
		cmd.ReplacementOrderID = &replacement
		require.NoError(t, ValidateCancelUnitsCommand(cmd, nil))
	})

	t.Run("CANCELLATION forbids a replacement order", func(t *testing.T) {
		replacement := correctionUnitID(500)
		cmd := validCmd(one())
		cmd.ReplacementOrderID = &replacement
		require.ErrorIs(t, ValidateCancelUnitsCommand(cmd, nil),
			ErrCancellationSelectionInvalid)
	})

	t.Run("rejects a reason outside the Cancellation catalog", func(t *testing.T) {
		for _, reason := range []string{
			"", "SPOILED", ReasonPreparationError, ReasonQualityFailure,
			ReasonStateRecordedInError,
		} {
			cmd := validCmd(one())
			cmd.Reason = reason
			require.ErrorIs(t, ValidateCancelUnitsCommand(cmd, nil), ErrInvalidReason, reason)
		}
	})

	t.Run("accepts every Cancellation reason", func(t *testing.T) {
		for _, reason := range []string{
			ReasonCustomerRequest, ReasonOrderEntryError, ReasonItemUnavailable, ReasonOther,
		} {
			cmd := validCmd(one())
			cmd.Reason = reason
			if reason == ReasonOther {
				cmd.Note = notePtr("khách đổi món")
			}
			require.NoError(t, ValidateCancelUnitsCommand(cmd, NormalizeCorrectionNote(cmd.Note)), reason)
		}
	})

	t.Run("OTHER requires a note that survives trimming", func(t *testing.T) {
		cmd := validCmd(one())
		cmd.Reason = ReasonOther
		require.ErrorIs(t, ValidateCancelUnitsCommand(cmd, nil), ErrInvalidNote)

		// A blank note normalizes to nil before validation.
		blank := "   "
		require.ErrorIs(t, ValidateCancelUnitsCommand(cmd,
			NormalizeCorrectionNote(&blank)), ErrInvalidNote)
	})

	t.Run("bounds a present note at five hundred code points", func(t *testing.T) {
		cmd := validCmd(one())
		note500 := strings.Repeat("ể", MaxNoteRunes)
		require.NoError(t, ValidateCancelUnitsCommand(cmd, &note500))

		note501 := strings.Repeat("ể", MaxNoteRunes+1)
		require.ErrorIs(t, ValidateCancelUnitsCommand(cmd, &note501), ErrInvalidNote)
	})
}

func TestCancelFingerprintIsUUIDSorted(t *testing.T) {
	// Deliberately jumbled relative to UUID byte order: the fingerprint is
	// built from the sorted copy, so two requests that name the same
	// selection in different orders are the same request.
	high := uuid.MustParse("ff000000-0000-0000-0000-000000000001")
	mid := uuid.MustParse("0a000000-0000-0000-0000-000000000002")
	low := correctionUnitID(50)

	build := func(ids []uuid.UUID) cancelUnitsFingerprint {
		return cancelUnitsFingerprintFor(CancelUnitsCommand{
			RequestID:          correctionUnitID(999),
			PreparationUnitIDs: ids,
			Kind:               CancelKindCancellation,
			Reason:             ReasonCustomerRequest,
		}, nil)
	}

	forward := build([]uuid.UUID{low, mid, high})
	reverse := build([]uuid.UUID{high, low, mid})

	require.Equal(t, []uuid.UUID{low, mid, high}, forward.PreparationUnitIDs,
		"the fingerprint carries the selection in UUID byte order")
	require.Equal(t, cancelFingerprintHashOf(t, forward), cancelFingerprintHashOf(t, reverse),
		"a differently ordered request fingerprints identically")

	// Fingerprinting must not mutate the request's own slice: request order
	// drives the response, while the sorted copy drives the fingerprint.
	ids := []uuid.UUID{high, low, mid}
	_ = build(ids)
	require.Equal(t, []uuid.UUID{high, low, mid}, ids,
		"building the fingerprint must not reorder the command's selection")
}

func TestCancelFingerprintExcludesCredentials(t *testing.T) {
	replacement := correctionUnitID(500)
	fingerprint := cancelUnitsFingerprint{
		PreparationUnitIDs: []uuid.UUID{correctionUnitID(1), correctionUnitID(2)},
		Kind:               CancelKindChange,
		ReplacementOrderID: &replacement,
		Reason:             ReasonCustomerRequest,
	}
	raw, err := json.Marshal(fingerprint)
	require.NoError(t, err)

	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &fields))
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	require.ElementsMatch(t,
		[]string{"preparation_unit_ids", "kind", "replacement_order_id", "reason", "note"},
		keys)
	for _, credential := range []string{"pin", "approved_by", "manager", "login_code"} {
		require.NotContains(t, strings.ToLower(string(raw)), credential)
	}
}

func TestCancelFingerprintNormalizesNote(t *testing.T) {
	ids := []uuid.UUID{correctionUnitID(1)}
	build := func(note *string) cancelUnitsFingerprint {
		return cancelUnitsFingerprintFor(CancelUnitsCommand{
			RequestID:          correctionUnitID(999),
			PreparationUnitIDs: ids,
			Kind:               CancelKindCancellation,
			Reason:             ReasonOther,
		}, note)
	}

	// Differently padded input fingerprints identically: the fingerprint
	// stands for the normalized note, so padded replays stay replays.
	padded := build(NormalizeCorrectionNote(notePtr("  khách đổi món  ")))
	trimmed := build(NormalizeCorrectionNote(notePtr("khách đổi món")))
	require.Equal(t, cancelFingerprintHashOf(t, trimmed), cancelFingerprintHashOf(t, padded),
		"a padded note and its trimmed form fingerprint identically")

	// A blank note normalizes to an absent one before fingerprinting.
	blank := build(NormalizeCorrectionNote(notePtr("   ")))
	absent := build(nil)
	require.Equal(t, cancelFingerprintHashOf(t, absent), cancelFingerprintHashOf(t, blank),
		"a blank note and an absent note fingerprint identically")
}

func TestCancelUnitsBoundary(t *testing.T) {
	// The handler validates before any transaction, so a handler whose runner
	// is nil never reaches the database on a boundary rejection — the nil
	// runner proves the ordering.
	h := NewCancelUnitsHandler(nil)

	cmd := func(ids []uuid.UUID) CancelUnitsCommand {
		return CancelUnitsCommand{
			RequestID:          correctionUnitID(999),
			PreparationUnitIDs: ids,
			Kind:               CancelKindCancellation,
			Reason:             ReasonCustomerRequest,
		}
	}

	t.Run("duplicate ids are rejected and never deduplicated or reordered", func(t *testing.T) {
		dup := correctionUnitID(7)
		ids := []uuid.UUID{correctionUnitID(3), dup, correctionUnitID(1), dup}
		request := cmd(ids)
		_, _, err := h.Handle(context.Background(), Actor{}, request)
		require.ErrorIs(t, err, ErrCancellationSelectionInvalid)
		require.Equal(t, ids, request.PreparationUnitIDs,
			"the rejection must not deduplicate or reorder the selection")
	})

	t.Run("zero and fifty-one ids are rejected", func(t *testing.T) {
		_, _, err := h.Handle(context.Background(), Actor{}, cmd([]uuid.UUID{}))
		require.ErrorIs(t, err, ErrCancellationSelectionInvalid)

		tooMany := make([]uuid.UUID, 0, MaxCancellationUnits+1)
		for i := 1; i <= MaxCancellationUnits+1; i++ {
			tooMany = append(tooMany, correctionUnitID(i))
		}
		_, _, err = h.Handle(context.Background(), Actor{}, cmd(tooMany))
		require.ErrorIs(t, err, ErrCancellationSelectionInvalid)
	})

	t.Run("a zero UUID in the selection is rejected", func(t *testing.T) {
		_, _, err := h.Handle(context.Background(), Actor{},
			cmd([]uuid.UUID{correctionUnitID(1), uuid.Nil}))
		require.ErrorIs(t, err, ErrCancellationSelectionInvalid)
	})

	t.Run("an invalid kind is rejected", func(t *testing.T) {
		request := cmd([]uuid.UUID{correctionUnitID(1)})
		request.Kind = "BREW"
		_, _, err := h.Handle(context.Background(), Actor{}, request)
		require.ErrorIs(t, err, ErrCancellationSelectionInvalid)
	})

	t.Run("the kind and replacement pairing is enforced before the transaction", func(t *testing.T) {
		request := cmd([]uuid.UUID{correctionUnitID(1)})
		request.Kind = CancelKindChange
		_, _, err := h.Handle(context.Background(), Actor{}, request)
		require.ErrorIs(t, err, ErrReplacementOrderRequired)

		replacement := correctionUnitID(500)
		request.Kind = CancelKindCancellation
		request.ReplacementOrderID = &replacement
		_, _, err = h.Handle(context.Background(), Actor{}, request)
		require.ErrorIs(t, err, ErrCancellationSelectionInvalid)
	})

	t.Run("a reason outside the Cancellation catalog is rejected", func(t *testing.T) {
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
}

func TestCancelChargeArithmetic(t *testing.T) {
	t.Run("an adjustment sum that overflows is a request range failure", func(t *testing.T) {
		total, err := sumCancellationAmounts([]int64{math.MaxInt64, 1})
		require.ErrorIs(t, err, ErrCancellationChargeOutOfRange)
		require.Zero(t, total)

		total, err = sumCancellationAmounts([]int64{25000, 25000, 25000})
		require.NoError(t, err)
		require.EqualValues(t, 75000, total)
	})

	t.Run("a non-positive unit price is an invariant", func(t *testing.T) {
		_, err := sumCancellationAmounts([]int64{25000, 0})
		require.ErrorIs(t, err, ErrChargeInvariantViolated)
	})

	t.Run("removing more than the stored charge is an invariant", func(t *testing.T) {
		newCharge, err := cancellationNewCharge(25000, 50000)
		require.ErrorIs(t, err, ErrChargeInvariantViolated)
		require.Zero(t, newCharge)

		newCharge, err = cancellationNewCharge(50000, 25000)
		require.NoError(t, err)
		require.EqualValues(t, 25000, newCharge)

		newCharge, err = cancellationNewCharge(25000, 25000)
		require.NoError(t, err)
		require.Zero(t, newCharge)
	})

	t.Run("effective receipt rejects refunds above valid payments", func(t *testing.T) {
		_, err := cancellationEffectiveReceived(10000, 20000)
		require.ErrorIs(t, err, ErrChargeInvariantViolated)

		effective, err := cancellationEffectiveReceived(10000, 4000)
		require.NoError(t, err)
		require.EqualValues(t, 6000, effective)
	})

	t.Run("the stored charge must equal base allocations less live adjustments", func(t *testing.T) {
		require.NoError(t, cancelCheckEquation(50000, 20000, 30000))

		require.ErrorIs(t, cancelCheckEquation(50000, 20000, 40000),
			ErrChargeInvariantViolated)
		require.ErrorIs(t, cancelCheckEquation(50000, 60000, 0),
			ErrChargeInvariantViolated)
		require.ErrorIs(t, cancelCheckEquation(-1, 0, 0),
			ErrChargeInvariantViolated)
	})
}
