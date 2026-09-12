package catalog

import (
	"context"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
	"github.com/google/uuid"
)

type createModifierOptionFingerprint struct {
	Name         string `json:"name"`
	SurchargeVND int64  `json:"surcharge_vnd"`
}

type createModifierGroupFingerprint struct {
	Name               string                            `json:"name"`
	MinSelections      int32                             `json:"min_selections"`
	MaxSelections      int32                             `json:"max_selections"`
	Options            []createModifierOptionFingerprint `json:"options"`
	DefaultOptionNames []string                          `json:"default_option_names,omitempty"`
}

type modifierGroupCreatedOptionAudit struct {
	OptionID     uuid.UUID `json:"option_id"`
	Name         string    `json:"name"`
	SurchargeVND int64     `json:"surcharge_vnd"`
}

type modifierGroupCreatedAuditDetails struct {
	GroupID          uuid.UUID                         `json:"group_id"`
	Name             string                            `json:"name"`
	MinSelections    int32                             `json:"min_selections"`
	MaxSelections    int32                             `json:"max_selections"`
	Options          []modifierGroupCreatedOptionAudit `json:"options"`
	DefaultOptionIDs []uuid.UUID                       `json:"default_option_ids,omitempty"`
}

// CreateModifierGroupHandler handles creation of modifier groups, initial options, and defaults.
type CreateModifierGroupHandler struct {
	runner *Runner
}

// NewCreateModifierGroupHandler creates a new CreateModifierGroupHandler.
func NewCreateModifierGroupHandler(runner *Runner) *CreateModifierGroupHandler {
	return &CreateModifierGroupHandler{runner: runner}
}

// Handle executes the modifier group creation command.
func (h *CreateModifierGroupHandler) Handle(ctx context.Context, actor Actor, cmd CreateModifierGroupCommand) (int, ModifierGroupResponse, error) {
	display, key := NormalizeName(cmd.Name)

	var optionsFp []createModifierOptionFingerprint
	if len(cmd.Options) > 0 {
		optionsFp = make([]createModifierOptionFingerprint, len(cmd.Options))
		for i, o := range cmd.Options {
			oDisplay, _ := NormalizeName(o.Name)
			optionsFp[i] = createModifierOptionFingerprint{
				Name:         oDisplay,
				SurchargeVND: o.SurchargeVND,
			}
		}
	}

	var defaultsFp []string
	if len(cmd.DefaultOptionNames) > 0 {
		defaultsFp = make([]string, len(cmd.DefaultOptionNames))
		for i, d := range cmd.DefaultOptionNames {
			dDisplay, _ := NormalizeName(d)
			defaultsFp[i] = dDisplay
		}
	}

	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpModifierGroupCreate,
		Fingerprint: createModifierGroupFingerprint{
			Name:               display,
			MinSelections:      cmd.MinSelections,
			MaxSelections:      cmd.MaxSelections,
			Options:            optionsFp,
			DefaultOptionNames: defaultsFp,
		},
		Required:          []string{CapAdministerStructure, CapChangePrice},
		RequireManagerPIN: true,
		ManagerPIN:        cmd.ManagerPIN,
	}

	return ExecuteMutation(ctx, h.runner, actor, spec, func(q *sqlc.Queries) (int, ModifierGroupResponse, AuditRecord, error) {
		// 1. Validate modifier group name
		if display == "" {
			return 0, ModifierGroupResponse{}, AuditRecord{}, fmt.Errorf("%w: group name cannot be empty", response.ErrInvalid)
		}

		// 2. Validate at least one option is present
		if len(cmd.Options) == 0 {
			return 0, ModifierGroupResponse{}, AuditRecord{}, fmt.Errorf("%w: modifier group must have at least one option", ErrInvalidModifierConfiguration)
		}

		// 3. Validate selection bounds: min >= 0, max >= 1, min <= max
		if cmd.MinSelections < 0 || cmd.MaxSelections < 1 || cmd.MinSelections > cmd.MaxSelections {
			return 0, ModifierGroupResponse{}, AuditRecord{}, fmt.Errorf("%w: invalid min/max selections bounds (%d, %d)", ErrInvalidModifierConfiguration, cmd.MinSelections, cmd.MaxSelections)
		}

		// 4. Validate max selections vs option count
		if int(cmd.MaxSelections) > len(cmd.Options) {
			return 0, ModifierGroupResponse{}, AuditRecord{}, fmt.Errorf("%w: max selections %d exceeds option count %d", ErrInvalidModifierConfiguration, cmd.MaxSelections, len(cmd.Options))
		}

		// 5. Validate options: non-empty, unique normalized names, surcharge bounds
		seenOptions := make(map[string]bool, len(cmd.Options))
		for _, o := range cmd.Options {
			oDisplay, oKey := NormalizeName(o.Name)
			if oDisplay == "" {
				return 0, ModifierGroupResponse{}, AuditRecord{}, fmt.Errorf("%w: option name cannot be empty", response.ErrInvalid)
			}
			if seenOptions[oKey] {
				return 0, ModifierGroupResponse{}, AuditRecord{}, fmt.Errorf("%w: duplicate option name %q", ErrNameConflict, oDisplay)
			}
			seenOptions[oKey] = true

			if err := ValidateSurcharge(o.SurchargeVND); err != nil {
				return 0, ModifierGroupResponse{}, AuditRecord{}, fmt.Errorf("%w: option %q surcharge %d: %s", ErrInvalidModifierConfiguration, oDisplay, o.SurchargeVND, err.Error())
			}
		}

		// 6. Validate default options if provided: cardinality and existence
		if len(cmd.DefaultOptionNames) > 0 {
			defCount := len(cmd.DefaultOptionNames)
			if defCount < int(cmd.MinSelections) || defCount > int(cmd.MaxSelections) {
				return 0, ModifierGroupResponse{}, AuditRecord{}, fmt.Errorf("%w: default options count %d must be between min %d and max %d", ErrInvalidModifierConfiguration, defCount, cmd.MinSelections, cmd.MaxSelections)
			}
			seenDefaults := make(map[string]bool, len(cmd.DefaultOptionNames))
			for _, d := range cmd.DefaultOptionNames {
				dDisplay, dKey := NormalizeName(d)
				if !seenOptions[dKey] {
					return 0, ModifierGroupResponse{}, AuditRecord{}, fmt.Errorf("%w: default option %q does not exist in options", ErrInvalidModifierConfiguration, dDisplay)
				}
				if seenDefaults[dKey] {
					return 0, ModifierGroupResponse{}, AuditRecord{}, fmt.Errorf("%w: duplicate default option %q", ErrInvalidModifierConfiguration, dDisplay)
				}
				seenDefaults[dKey] = true
			}
		}

		// 7. Create modifier group
		group, err := q.CreateModifierGroup(ctx, sqlc.CreateModifierGroupParams{
			Name:           display,
			NormalizedName: key,
			MinSelections:  cmd.MinSelections,
			MaxSelections:  cmd.MaxSelections,
		})
		if err != nil {
			return 0, ModifierGroupResponse{}, AuditRecord{}, MapDBError(err)
		}

		// 8. Create modifier options in a single batch insert.
		names := make([]string, len(cmd.Options))
		normNames := make([]string, len(cmd.Options))
		surcharges := make([]int64, len(cmd.Options))
		for i, o := range cmd.Options {
			oDisplay, oKey := NormalizeName(o.Name)
			names[i] = oDisplay
			normNames[i] = oKey
			surcharges[i] = o.SurchargeVND
		}
		createdOptions, err := q.CreateModifierOptions(ctx, sqlc.CreateModifierOptionsParams{
			ModifierGroupID: group.ID,
			Names:           names,
			NormalizedNames: normNames,
			Surcharges:      surcharges,
		})
		if err != nil {
			return 0, ModifierGroupResponse{}, AuditRecord{}, MapDBError(err)
		}

		// The batch query returns rows in input order (ORDER BY ordinality),
		// so createdOptions[i] corresponds to cmd.Options[i], whose normalized
		// key is normNames[i].
		optMap := make(map[string]uuid.UUID, len(cmd.Options))
		for i, oKey := range normNames {
			optMap[oKey] = createdOptions[i].ID
		}

		// 9. Create default options associations in a single batch insert.
		defaultIDs := make([]uuid.UUID, 0, len(cmd.DefaultOptionNames))
		for _, d := range cmd.DefaultOptionNames {
			_, dKey := NormalizeName(d)
			defaultIDs = append(defaultIDs, optMap[dKey])
		}
		if len(defaultIDs) > 0 {
			if err := q.CreateModifierGroupDefaultOptions(ctx, sqlc.CreateModifierGroupDefaultOptionsParams{
				ModifierGroupID: group.ID,
				OptionIds:       defaultIDs,
			}); err != nil {
				return 0, ModifierGroupResponse{}, AuditRecord{}, MapDBError(err)
			}
		}

		// 10. Assemble ModifierGroupResponse
		res := ModifierGroupResponse{
			ID:               group.ID,
			Name:             group.Name,
			MinSelections:    group.MinSelections,
			MaxSelections:    group.MaxSelections,
			Options:          make([]ModifierOptionResponse, len(createdOptions)),
			DefaultOptionIDs: defaultIDs,
		}
		for i, o := range createdOptions {
			res.Options[i] = ModifierOptionResponse{
				ID:              o.ID,
				ModifierGroupID: o.ModifierGroupID,
				Name:            o.Name,
				SurchargeVND:    o.SurchargeVnd,
				Available:       o.Available,
			}
		}

		// 11. Assemble secret-free AuditRecord
		auditOpts := make([]modifierGroupCreatedOptionAudit, len(createdOptions))
		for i, o := range createdOptions {
			auditOpts[i] = modifierGroupCreatedOptionAudit{
				OptionID:     o.ID,
				Name:         o.Name,
				SurchargeVND: o.SurchargeVnd,
			}
		}

		audit := AuditRecord{
			EventType: EventModifierGroupCreated,
			Details: modifierGroupCreatedAuditDetails{
				GroupID:          group.ID,
				Name:             group.Name,
				MinSelections:    group.MinSelections,
				MaxSelections:    group.MaxSelections,
				Options:          auditOpts,
				DefaultOptionIDs: defaultIDs,
			},
		}

		return 201, res, audit, nil
	})
}
