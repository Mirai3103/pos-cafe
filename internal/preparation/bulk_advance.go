package preparation

import (
	"bytes"
	"fmt"
	"slices"

	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
)

type bulkAdvanceFingerprint struct {
	PreparationUnitIDs []uuid.UUID `json:"preparation_unit_ids"`
	TargetState        string      `json:"target_state"`
}

func normalizeBulkSelection(ids []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(ids))
	normalized := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		normalized = append(normalized, id)
	}
	return normalized
}

func sortedBulkSelection(ids []uuid.UUID) []uuid.UUID {
	sorted := append([]uuid.UUID(nil), ids...)
	slices.SortFunc(sorted, func(a, b uuid.UUID) int {
		return bytes.Compare(a[:], b[:])
	})
	return sorted
}

func validateBulkAdvance(cmd BulkAdvanceCommand) error {
	if cmd.RequestID == uuid.Nil {
		return fmt.Errorf("%w: request_id is required", response.ErrInvalid)
	}
	if len(cmd.PreparationUnitIDs) < 1 || len(cmd.PreparationUnitIDs) > 50 {
		return fmt.Errorf("%w: preparation_unit_ids must contain 1 through 50 ids", response.ErrInvalid)
	}
	for _, id := range cmd.PreparationUnitIDs {
		if id == uuid.Nil {
			return fmt.Errorf("%w: preparation_unit_ids must not contain a zero UUID", response.ErrInvalid)
		}
	}
	if !IsAdvanceTarget(cmd.TargetState) {
		return fmt.Errorf("%w: target_state must be IN_PREPARATION, READY, or FULFILLED", response.ErrInvalid)
	}
	return nil
}
