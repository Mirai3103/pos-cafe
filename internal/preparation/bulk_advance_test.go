package preparation

import (
	"errors"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeBulkSelectionKeepsFirstOccurrence(t *testing.T) {
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	assert.Equal(t, []uuid.UUID{b, a, c}, normalizeBulkSelection(
		[]uuid.UUID{b, a, b, c, a},
	))
}

func TestSortedBulkSelectionUsesUUIDByteOrderWithoutMutatingInput(t *testing.T) {
	a := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	b := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	c := uuid.MustParse("10000000-0000-0000-0000-000000000000")
	input := []uuid.UUID{c, a, b}

	require.Equal(t, []uuid.UUID{b, a, c}, sortedBulkSelection(input))
	require.Equal(t, []uuid.UUID{c, a, b}, input)
}

func TestValidateBulkAdvance(t *testing.T) {
	ids50 := make([]uuid.UUID, 50)
	for i := range ids50 {
		ids50[i] = uuid.New()
	}

	tests := []struct {
		name string
		cmd  BulkAdvanceCommand
		want error
	}{
		{"one id", BulkAdvanceCommand{RequestID: uuid.New(), PreparationUnitIDs: ids50[:1], TargetState: StateReady}, nil},
		{"fifty ids", BulkAdvanceCommand{RequestID: uuid.New(), PreparationUnitIDs: ids50, TargetState: StateReady}, nil},
		{"zero request id", BulkAdvanceCommand{PreparationUnitIDs: ids50[:1], TargetState: StateReady}, response.ErrInvalid},
		{"empty ids", BulkAdvanceCommand{RequestID: uuid.New(), TargetState: StateReady}, response.ErrInvalid},
		{"fifty one ids", BulkAdvanceCommand{RequestID: uuid.New(), PreparationUnitIDs: append(ids50, uuid.New()), TargetState: StateReady}, response.ErrInvalid},
		{"zero unit id", BulkAdvanceCommand{RequestID: uuid.New(), PreparationUnitIDs: []uuid.UUID{uuid.Nil}, TargetState: StateReady}, response.ErrInvalid},
		{"invalid target", BulkAdvanceCommand{RequestID: uuid.New(), PreparationUnitIDs: ids50[:1], TargetState: StateQueued}, response.ErrInvalid},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBulkAdvance(tt.cmd)
			if tt.want == nil {
				require.NoError(t, err)
				return
			}
			require.True(t, errors.Is(err, tt.want), err)
		})
	}
}
