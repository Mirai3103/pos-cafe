package shift

import (
	"context"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
)

type openShiftFingerprint struct {
	OpeningFloatVND int64 `json:"opening_float_vnd"`
}

type salesShiftOpenedAuditDetails struct {
	SalesShiftID    uuid.UUID `json:"sales_shift_id"`
	OpeningFloatVND int64     `json:"opening_float_vnd"`
}

// OpenShiftHandler opens a Sales Shift.
type OpenShiftHandler struct{ runner *Runner }

// NewOpenShiftHandler creates a new OpenShiftHandler.
func NewOpenShiftHandler(runner *Runner) *OpenShiftHandler {
	return &OpenShiftHandler{runner: runner}
}

// Handle opens a Sales Shift with a counted Opening Float.
//
// At most one Sales Shift may be OPEN across the whole system. The partial
// unique index sales_shift_only_one_open_unique is the sole authority for that
// invariant: a second open receives SALES_SHIFT_ALREADY_OPEN rather than a
// generic 500, and two truly concurrent opens resolve to exactly one success.
func (h *OpenShiftHandler) Handle(ctx context.Context, actor Actor, cmd OpenShiftCommand) (int, SalesShiftResponse, error) {
	if cmd.OpeningFloatVND == nil {
		return 0, SalesShiftResponse{}, fmt.Errorf("%w: opening_float_vnd is required", response.ErrInvalid)
	}
	openingFloat := *cmd.OpeningFloatVND

	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpOpenShift,
		Fingerprint: openShiftFingerprint{OpeningFloatVND: openingFloat},
		Required:    []string{CapSalesShiftOperate},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, SalesShiftResponse, AuditRecord, error) {
			if err := ValidateOpeningFloat(openingFloat); err != nil {
				return 0, SalesShiftResponse{}, AuditRecord{},
					fmt.Errorf("%w: %s", response.ErrInvalid, err.Error())
			}

			row, err := mc.Queries.OpenSalesShift(ctx, sqlc.OpenSalesShiftParams{
				OpenedByStaffIdentityID: actor.StaffID,
				OpeningFloatVnd:         openingFloat,
			})
			if err != nil {
				return 0, SalesShiftResponse{}, AuditRecord{}, MapDBError(err)
			}

			opener, err := mc.Queries.GetStaffSummary(ctx, actor.StaffID)
			if err != nil {
				return 0, SalesShiftResponse{}, AuditRecord{},
					fmt.Errorf("load opener summary: %w", err)
			}

			result := SalesShiftResponse{
				ID:              row.ID,
				State:           row.State,
				OpeningFloatVND: row.OpeningFloatVnd,
				OpenedAt:        row.OpenedAt,
				Opener: StaffSummary{
					ID:          opener.ID,
					DisplayName: opener.DisplayName,
					LoginCode:   opener.LoginCode,
				},
			}

			return 201, result, AuditRecord{
				EventType: EventSalesShiftOpened,
				Details: salesShiftOpenedAuditDetails{
					SalesShiftID:    row.ID,
					OpeningFloatVND: row.OpeningFloatVnd,
				},
			}, nil
		})
}
