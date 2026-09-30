package catalog

import (
	"context"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/platform/database/sqlc"
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
	spec := MutationSpec{
		RequestID:         cmd.RequestID,
		Operation:         OpModifierGroupCreate,
		Fingerprint:       newCreateModifierGroupFingerprint(cmd, display),
		Required:          []string{CapAdministerStructure, CapChangePrice},
		RequireManagerPIN: true,
		ManagerPIN:        cmd.ManagerPIN,
	}

	return ExecuteMutation(ctx, h.runner, actor, spec, func(q *sqlc.Queries) (int, ModifierGroupResponse, AuditRecord, error) {
		if err := validateNewModifierGroup(cmd, display); err != nil {
			return 0, ModifierGroupResponse{}, AuditRecord{}, err
		}

		group, err := q.CreateModifierGroup(ctx, sqlc.CreateModifierGroupParams{
			Name:           display,
			NormalizedName: key,
			MinSelections:  cmd.MinSelections,
			MaxSelections:  cmd.MaxSelections,
		})
		if err != nil {
			return 0, ModifierGroupResponse{}, AuditRecord{}, MapDBError(err)
		}
		options, optionIDByKey, err := createGroupOptions(ctx, q, group.ID, cmd.Options)
		if err != nil {
			return 0, ModifierGroupResponse{}, AuditRecord{}, err
		}
		defaultIDs, err := createGroupDefaults(ctx, q, group.ID, cmd.DefaultOptionNames, optionIDByKey)
		if err != nil {
			return 0, ModifierGroupResponse{}, AuditRecord{}, err
		}

		audit := AuditRecord{
			EventType: EventModifierGroupCreated,
			Details:   newModifierGroupCreatedAudit(group, options, defaultIDs),
		}
		return 201, newModifierGroupResponse(group, options, defaultIDs), audit, nil
	})
}

// newCreateModifierGroupFingerprint normalizes the business input the
// idempotency key covers. The Manager PIN is deliberately absent.
func newCreateModifierGroupFingerprint(cmd CreateModifierGroupCommand, display string) createModifierGroupFingerprint {
	var options []createModifierOptionFingerprint
	if len(cmd.Options) > 0 {
		options = make([]createModifierOptionFingerprint, len(cmd.Options))
		for i, o := range cmd.Options {
			oDisplay, _ := NormalizeName(o.Name)
			options[i] = createModifierOptionFingerprint{
				Name:         oDisplay,
				SurchargeVND: o.SurchargeVND,
			}
		}
	}

	var defaults []string
	if len(cmd.DefaultOptionNames) > 0 {
		defaults = make([]string, len(cmd.DefaultOptionNames))
		for i, d := range cmd.DefaultOptionNames {
			defaults[i], _ = NormalizeName(d)
		}
	}

	return createModifierGroupFingerprint{
		Name:               display,
		MinSelections:      cmd.MinSelections,
		MaxSelections:      cmd.MaxSelections,
		Options:            options,
		DefaultOptionNames: defaults,
	}
}

// validateNewModifierGroup checks the group name, its selection bounds
// against its options, the options themselves, and the default options.
func validateNewModifierGroup(cmd CreateModifierGroupCommand, display string) error {
	if display == "" {
		return fmt.Errorf("%w: group name cannot be empty", response.ErrInvalid)
	}
	if len(cmd.Options) == 0 {
		return fmt.Errorf("%w: modifier group must have at least one option", ErrInvalidModifierConfiguration)
	}
	if cmd.MinSelections < 0 || cmd.MaxSelections < 1 || cmd.MinSelections > cmd.MaxSelections {
		return fmt.Errorf("%w: invalid min/max selections bounds (%d, %d)", ErrInvalidModifierConfiguration, cmd.MinSelections, cmd.MaxSelections)
	}
	if int(cmd.MaxSelections) > len(cmd.Options) {
		return fmt.Errorf("%w: max selections %d exceeds option count %d", ErrInvalidModifierConfiguration, cmd.MaxSelections, len(cmd.Options))
	}
	optionKeys, err := validateNewOptions(cmd.Options)
	if err != nil {
		return err
	}
	return validateNewDefaults(cmd, optionKeys)
}

// validateNewOptions checks each option's name and surcharge, and returns
// the set of normalized option names.
func validateNewOptions(options []CreateModifierOptionInput) (map[string]bool, error) {
	keys := make(map[string]bool, len(options))
	for _, o := range options {
		oDisplay, oKey := NormalizeName(o.Name)
		if oDisplay == "" {
			return nil, fmt.Errorf("%w: option name cannot be empty", response.ErrInvalid)
		}
		if keys[oKey] {
			return nil, fmt.Errorf("%w: duplicate option name %q", ErrNameConflict, oDisplay)
		}
		keys[oKey] = true

		if err := ValidateSurcharge(o.SurchargeVND); err != nil {
			return nil, fmt.Errorf("%w: option %q surcharge %d: %s", ErrInvalidModifierConfiguration, oDisplay, o.SurchargeVND, err.Error())
		}
	}
	return keys, nil
}

// validateNewDefaults checks that the defaults, when given, fit the selection
// bounds and name distinct options of the group.
func validateNewDefaults(cmd CreateModifierGroupCommand, optionKeys map[string]bool) error {
	if len(cmd.DefaultOptionNames) == 0 {
		return nil
	}
	defCount := len(cmd.DefaultOptionNames)
	if defCount < int(cmd.MinSelections) || defCount > int(cmd.MaxSelections) {
		return fmt.Errorf("%w: default options count %d must be between min %d and max %d", ErrInvalidModifierConfiguration, defCount, cmd.MinSelections, cmd.MaxSelections)
	}
	seen := make(map[string]bool, len(cmd.DefaultOptionNames))
	for _, d := range cmd.DefaultOptionNames {
		dDisplay, dKey := NormalizeName(d)
		if !optionKeys[dKey] {
			return fmt.Errorf("%w: default option %q does not exist in options", ErrInvalidModifierConfiguration, dDisplay)
		}
		if seen[dKey] {
			return fmt.Errorf("%w: duplicate default option %q", ErrInvalidModifierConfiguration, dDisplay)
		}
		seen[dKey] = true
	}
	return nil
}

// createGroupOptions inserts the group's options in one batch and returns
// them with their ids keyed by normalized name.
func createGroupOptions(ctx context.Context, q *sqlc.Queries, groupID uuid.UUID, options []CreateModifierOptionInput) ([]sqlc.ModifierOption, map[string]uuid.UUID, error) {
	names := make([]string, len(options))
	normalizedNames := make([]string, len(options))
	surcharges := make([]int64, len(options))
	for i, o := range options {
		names[i], normalizedNames[i] = NormalizeName(o.Name)
		surcharges[i] = o.SurchargeVND
	}
	created, err := q.CreateModifierOptions(ctx, sqlc.CreateModifierOptionsParams{
		ModifierGroupID: groupID,
		Names:           names,
		NormalizedNames: normalizedNames,
		Surcharges:      surcharges,
	})
	if err != nil {
		return nil, nil, MapDBError(err)
	}

	// The batch query returns rows in input order (ORDER BY ordinality),
	// so created[i] corresponds to options[i], whose normalized key is
	// normalizedNames[i].
	idByKey := make(map[string]uuid.UUID, len(options))
	for i, key := range normalizedNames {
		idByKey[key] = created[i].ID
	}
	return created, idByKey, nil
}

// createGroupDefaults marks the named options as the group's defaults in one
// batch and returns their ids in request order, never nil.
func createGroupDefaults(ctx context.Context, q *sqlc.Queries, groupID uuid.UUID, defaultNames []string, optionIDByKey map[string]uuid.UUID) ([]uuid.UUID, error) {
	defaultIDs := make([]uuid.UUID, 0, len(defaultNames))
	for _, d := range defaultNames {
		_, dKey := NormalizeName(d)
		defaultIDs = append(defaultIDs, optionIDByKey[dKey])
	}
	if len(defaultIDs) > 0 {
		if err := q.CreateModifierGroupDefaultOptions(ctx, sqlc.CreateModifierGroupDefaultOptionsParams{
			ModifierGroupID: groupID,
			OptionIds:       defaultIDs,
		}); err != nil {
			return nil, MapDBError(err)
		}
	}
	return defaultIDs, nil
}

// newModifierGroupCreatedAudit assembles the audit details, which never
// carry the Manager PIN.
func newModifierGroupCreatedAudit(group sqlc.ModifierGroup, options []sqlc.ModifierOption, defaultIDs []uuid.UUID) modifierGroupCreatedAuditDetails {
	auditOptions := make([]modifierGroupCreatedOptionAudit, len(options))
	for i, o := range options {
		auditOptions[i] = modifierGroupCreatedOptionAudit{
			OptionID:     o.ID,
			Name:         o.Name,
			SurchargeVND: o.SurchargeVnd,
		}
	}
	return modifierGroupCreatedAuditDetails{
		GroupID:          group.ID,
		Name:             group.Name,
		MinSelections:    group.MinSelections,
		MaxSelections:    group.MaxSelections,
		Options:          auditOptions,
		DefaultOptionIDs: defaultIDs,
	}
}
