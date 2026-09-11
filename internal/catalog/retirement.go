package catalog

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

// Fingerprints and audit details

type retireItemFingerprint struct {
	ItemID uuid.UUID `json:"item_id"`
	Reason string    `json:"reason"`
	Note   string    `json:"note,omitempty"`
}

type itemRetiredAuditDetails struct {
	ItemID uuid.UUID `json:"item_id"`
	Reason string    `json:"reason"`
	Note   string    `json:"note,omitempty"`
}

type retireSizeFingerprint struct {
	SizeID uuid.UUID `json:"size_id"`
	Reason string    `json:"reason"`
	Note   string    `json:"note,omitempty"`
}

type sizeRetiredAuditDetails struct {
	SizeID     uuid.UUID `json:"size_id"`
	MenuItemID uuid.UUID `json:"menu_item_id"`
	Reason     string    `json:"reason"`
	Note       string    `json:"note,omitempty"`
}

type retireModifierGroupFingerprint struct {
	GroupID uuid.UUID `json:"group_id"`
	Reason  string    `json:"reason"`
	Note    string    `json:"note,omitempty"`
}

type modifierGroupRetiredAuditDetails struct {
	GroupID uuid.UUID `json:"group_id"`
	Reason  string    `json:"reason"`
	Note    string    `json:"note,omitempty"`
}

type retireModifierOptionFingerprint struct {
	OptionID uuid.UUID `json:"option_id"`
	Reason   string    `json:"reason"`
	Note     string    `json:"note,omitempty"`
}

type modifierOptionRetiredAuditDetails struct {
	OptionID        uuid.UUID `json:"option_id"`
	ModifierGroupID uuid.UUID `json:"modifier_group_id"`
	Reason          string    `json:"reason"`
	Note            string    `json:"note,omitempty"`
}

// validateRetirementInput performs domain validation for retirement arguments.
func validateRetirementInput(reason, note string) (string, error) {
	trimmedNote := strings.TrimSpace(note)
	if reason == "" {
		return "", fmt.Errorf("%w: retirement reason is required", ErrInvalidRetirement)
	}
	if err := ValidateRetirement(Retirement{Reason: reason, Note: trimmedNote}); err != nil {
		return "", fmt.Errorf("%w: %s", ErrInvalidRetirement, err.Error())
	}
	return trimmedNote, nil
}

// ============================================================================
// RetireItemHandler
// ============================================================================

// RetireItemHandler handles permanent retirement of menu items.
type RetireItemHandler struct {
	runner *Runner
}

// NewRetireItemHandler creates a new RetireItemHandler.
func NewRetireItemHandler(runner *Runner) *RetireItemHandler {
	return &RetireItemHandler{runner: runner}
}

// Handle executes the item retirement command.
func (h *RetireItemHandler) Handle(ctx context.Context, actor Actor, cmd RetireItemCommand) (int, ItemResponse, error) {
	trimmedNote, valErr := validateRetirementInput(cmd.Reason, cmd.Note)
	if valErr != nil {
		return 0, ItemResponse{}, valErr
	}

	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpItemRetire,
		Fingerprint: retireItemFingerprint{
			ItemID: cmd.ItemID,
			Reason: cmd.Reason,
			Note:   trimmedNote,
		},
		Required: []string{CapAdministerStructure},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec, func(q *sqlc.Queries) (int, ItemResponse, AuditRecord, error) {
		existing, err := q.GetMenuItemForUpdate(ctx, cmd.ItemID)
		if err != nil {
			return 0, ItemResponse{}, AuditRecord{}, MapDBError(err)
		}
		if existing.RetiredAt.Valid {
			return 0, ItemResponse{}, AuditRecord{}, ErrEntityRetired
		}

		var noteNull sql.NullString
		if trimmedNote != "" {
			noteNull = sql.NullString{String: trimmedNote, Valid: true}
		}

		item, err := q.RetireMenuItem(ctx, sqlc.RetireMenuItemParams{
			ID:               cmd.ItemID,
			RetiredAt:        sql.NullTime{Time: time.Now(), Valid: true},
			RetirementReason: sql.NullString{String: cmd.Reason, Valid: true},
			RetirementNote:   noteNull,
		})
		if err != nil {
			return 0, ItemResponse{}, AuditRecord{}, MapDBError(err)
		}

		res := ItemResponse{
			ID:         item.ID,
			CategoryID: item.CategoryID,
			Name:       item.Name,
			Available:  item.Available,
		}
		if item.PriceVnd.Valid {
			v := item.PriceVnd.Int64
			res.PriceVND = &v
		}

		sizes, err := q.ListMenuItemSizesByItem(ctx, item.ID)
		if err != nil {
			return 0, ItemResponse{}, AuditRecord{}, MapDBError(err)
		}
		if len(sizes) > 0 {
			res.Sizes = make([]SizeResponse, len(sizes))
			for i, s := range sizes {
				res.Sizes[i] = SizeResponse{
					ID:        s.ID,
					Name:      s.Name,
					PriceVND:  s.PriceVnd,
					Available: s.Available,
				}
			}
		}

		audit := AuditRecord{
			EventType: EventItemRetired,
			Details: itemRetiredAuditDetails{
				ItemID: item.ID,
				Reason: cmd.Reason,
				Note:   trimmedNote,
			},
		}

		return 200, res, audit, nil
	})
}

// ============================================================================
// RetireSizeHandler
// ============================================================================

// RetireSizeHandler handles permanent retirement of menu item sizes.
type RetireSizeHandler struct {
	runner *Runner
}

// NewRetireSizeHandler creates a new RetireSizeHandler.
func NewRetireSizeHandler(runner *Runner) *RetireSizeHandler {
	return &RetireSizeHandler{runner: runner}
}

// Handle executes the size retirement command.
func (h *RetireSizeHandler) Handle(ctx context.Context, actor Actor, cmd RetireSizeCommand) (int, SizeResponse, error) {
	trimmedNote, valErr := validateRetirementInput(cmd.Reason, cmd.Note)
	if valErr != nil {
		return 0, SizeResponse{}, valErr
	}

	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpSizeRetire,
		Fingerprint: retireSizeFingerprint{
			SizeID: cmd.SizeID,
			Reason: cmd.Reason,
			Note:   trimmedNote,
		},
		Required: []string{CapAdministerStructure},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec, func(q *sqlc.Queries) (int, SizeResponse, AuditRecord, error) {
		// Lock parent before child:
		// 1. Obtain parent MenuItemID
		sizeRow, err := q.GetMenuItemSizeByID(ctx, cmd.SizeID)
		if err != nil {
			return 0, SizeResponse{}, AuditRecord{}, MapDBError(err)
		}

		// 2. Lock parent MenuItem and check retirement
		parent, err := q.GetMenuItemForUpdate(ctx, sizeRow.MenuItemID)
		if err != nil {
			return 0, SizeResponse{}, AuditRecord{}, MapDBError(err)
		}
		if parent.RetiredAt.Valid {
			return 0, SizeResponse{}, AuditRecord{}, ErrEntityRetired
		}

		// 3. Lock child MenuItemSize and check retirement
		existing, err := q.GetMenuItemSizeForUpdate(ctx, cmd.SizeID)
		if err != nil {
			return 0, SizeResponse{}, AuditRecord{}, MapDBError(err)
		}
		if existing.RetiredAt.Valid {
			return 0, SizeResponse{}, AuditRecord{}, ErrEntityRetired
		}

		var noteNull sql.NullString
		if trimmedNote != "" {
			noteNull = sql.NullString{String: trimmedNote, Valid: true}
		}

		size, err := q.RetireMenuItemSize(ctx, sqlc.RetireMenuItemSizeParams{
			ID:               cmd.SizeID,
			RetiredAt:        sql.NullTime{Time: time.Now(), Valid: true},
			RetirementReason: sql.NullString{String: cmd.Reason, Valid: true},
			RetirementNote:   noteNull,
		})
		if err != nil {
			return 0, SizeResponse{}, AuditRecord{}, MapDBError(err)
		}

		res := SizeResponse{
			ID:        size.ID,
			Name:      size.Name,
			PriceVND:  size.PriceVnd,
			Available: size.Available,
		}

		audit := AuditRecord{
			EventType: EventSizeRetired,
			Details: sizeRetiredAuditDetails{
				SizeID:     size.ID,
				MenuItemID: size.MenuItemID,
				Reason:     cmd.Reason,
				Note:       trimmedNote,
			},
		}

		return 200, res, audit, nil
	})
}

// ============================================================================
// RetireModifierGroupHandler
// ============================================================================

// RetireModifierGroupHandler handles permanent retirement of modifier groups.
type RetireModifierGroupHandler struct {
	runner *Runner
}

// NewRetireModifierGroupHandler creates a new RetireModifierGroupHandler.
func NewRetireModifierGroupHandler(runner *Runner) *RetireModifierGroupHandler {
	return &RetireModifierGroupHandler{runner: runner}
}

// Handle executes the modifier group retirement command.
func (h *RetireModifierGroupHandler) Handle(ctx context.Context, actor Actor, cmd RetireModifierGroupCommand) (int, ModifierGroupResponse, error) {
	trimmedNote, valErr := validateRetirementInput(cmd.Reason, cmd.Note)
	if valErr != nil {
		return 0, ModifierGroupResponse{}, valErr
	}

	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpModifierGroupRetire,
		Fingerprint: retireModifierGroupFingerprint{
			GroupID: cmd.GroupID,
			Reason:  cmd.Reason,
			Note:    trimmedNote,
		},
		Required: []string{CapAdministerStructure},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec, func(q *sqlc.Queries) (int, ModifierGroupResponse, AuditRecord, error) {
		existing, err := q.GetModifierGroupForUpdate(ctx, cmd.GroupID)
		if err != nil {
			return 0, ModifierGroupResponse{}, AuditRecord{}, MapDBError(err)
		}
		if existing.RetiredAt.Valid {
			return 0, ModifierGroupResponse{}, AuditRecord{}, ErrEntityRetired
		}

		var noteNull sql.NullString
		if trimmedNote != "" {
			noteNull = sql.NullString{String: trimmedNote, Valid: true}
		}

		group, err := q.RetireModifierGroup(ctx, sqlc.RetireModifierGroupParams{
			ID:               cmd.GroupID,
			RetiredAt:        sql.NullTime{Time: time.Now(), Valid: true},
			RetirementReason: sql.NullString{String: cmd.Reason, Valid: true},
			RetirementNote:   noteNull,
		})
		if err != nil {
			return 0, ModifierGroupResponse{}, AuditRecord{}, MapDBError(err)
		}

		options, err := q.ListModifierOptionsByGroup(ctx, group.ID)
		if err != nil {
			return 0, ModifierGroupResponse{}, AuditRecord{}, MapDBError(err)
		}
		defaultRows, err := q.ListModifierGroupDefaultOptionsByGroup(ctx, group.ID)
		if err != nil {
			return 0, ModifierGroupResponse{}, AuditRecord{}, MapDBError(err)
		}
		defaultIDs := make([]uuid.UUID, len(defaultRows))
		for i, d := range defaultRows {
			defaultIDs[i] = d.ModifierOptionID
		}

		res := ModifierGroupResponse{
			ID:               group.ID,
			Name:             group.Name,
			MinSelections:    group.MinSelections,
			MaxSelections:    group.MaxSelections,
			Options:          make([]ModifierOptionResponse, len(options)),
			DefaultOptionIDs: defaultIDs,
		}
		for i, o := range options {
			res.Options[i] = ModifierOptionResponse{
				ID:              o.ID,
				ModifierGroupID: o.ModifierGroupID,
				Name:            o.Name,
				SurchargeVND:    o.SurchargeVnd,
				Available:       o.Available,
			}
		}

		audit := AuditRecord{
			EventType: EventModifierGroupRetired,
			Details: modifierGroupRetiredAuditDetails{
				GroupID: group.ID,
				Reason:  cmd.Reason,
				Note:    trimmedNote,
			},
		}

		return 200, res, audit, nil
	})
}

// ============================================================================
// RetireModifierOptionHandler
// ============================================================================

// RetireModifierOptionHandler handles permanent retirement of modifier options.
type RetireModifierOptionHandler struct {
	runner *Runner
}

// NewRetireModifierOptionHandler creates a new RetireModifierOptionHandler.
func NewRetireModifierOptionHandler(runner *Runner) *RetireModifierOptionHandler {
	return &RetireModifierOptionHandler{runner: runner}
}

// Handle executes the modifier option retirement command.
func (h *RetireModifierOptionHandler) Handle(ctx context.Context, actor Actor, cmd RetireModifierOptionCommand) (int, ModifierOptionResponse, error) {
	trimmedNote, valErr := validateRetirementInput(cmd.Reason, cmd.Note)
	if valErr != nil {
		return 0, ModifierOptionResponse{}, valErr
	}

	spec := MutationSpec{
		RequestID: cmd.RequestID,
		Operation: OpModifierOptionRetire,
		Fingerprint: retireModifierOptionFingerprint{
			OptionID: cmd.OptionID,
			Reason:   cmd.Reason,
			Note:     trimmedNote,
		},
		Required: []string{CapAdministerStructure},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec, func(q *sqlc.Queries) (int, ModifierOptionResponse, AuditRecord, error) {
		// Lock parent before child:
		// 1. Obtain parent ModifierGroupID
		optRow, err := q.GetModifierOptionByID(ctx, cmd.OptionID)
		if err != nil {
			return 0, ModifierOptionResponse{}, AuditRecord{}, MapDBError(err)
		}

		// 2. Lock parent ModifierGroup and check retirement
		parent, err := q.GetModifierGroupForUpdate(ctx, optRow.ModifierGroupID)
		if err != nil {
			return 0, ModifierOptionResponse{}, AuditRecord{}, MapDBError(err)
		}
		if parent.RetiredAt.Valid {
			return 0, ModifierOptionResponse{}, AuditRecord{}, ErrEntityRetired
		}

		// 3. Lock child ModifierOption and check retirement
		existing, err := q.GetModifierOptionForUpdate(ctx, cmd.OptionID)
		if err != nil {
			return 0, ModifierOptionResponse{}, AuditRecord{}, MapDBError(err)
		}
		if existing.RetiredAt.Valid {
			return 0, ModifierOptionResponse{}, AuditRecord{}, ErrEntityRetired
		}

		var noteNull sql.NullString
		if trimmedNote != "" {
			noteNull = sql.NullString{String: trimmedNote, Valid: true}
		}

		opt, err := q.RetireModifierOption(ctx, sqlc.RetireModifierOptionParams{
			ID:               cmd.OptionID,
			RetiredAt:        sql.NullTime{Time: time.Now(), Valid: true},
			RetirementReason: sql.NullString{String: cmd.Reason, Valid: true},
			RetirementNote:   noteNull,
		})
		if err != nil {
			return 0, ModifierOptionResponse{}, AuditRecord{}, MapDBError(err)
		}

		res := ModifierOptionResponse{
			ID:              opt.ID,
			ModifierGroupID: opt.ModifierGroupID,
			Name:            opt.Name,
			SurchargeVND:    opt.SurchargeVnd,
			Available:       opt.Available,
		}

		audit := AuditRecord{
			EventType: EventModifierOptionRetired,
			Details: modifierOptionRetiredAuditDetails{
				OptionID:        opt.ID,
				ModifierGroupID: opt.ModifierGroupID,
				Reason:          cmd.Reason,
				Note:            trimmedNote,
			},
		}

		return 200, res, audit, nil
	})
}
