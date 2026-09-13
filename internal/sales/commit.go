package sales

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

// EffectiveGroup is one Modifier Group that applies to a Menu Item, with the
// selection rules Commit enforces. 5A's draft validation deliberately ignores
// these counts: a draft is a proposal and tolerates incompleteness. Commit is
// where completeness matters, because it is where price is fixed.
type EffectiveGroup struct {
	GroupID       uuid.UUID
	GroupName     string
	MinSelections int32
	MaxSelections int32
	Retired       bool
}

// SelectedOption is one Modifier Option chosen on a draft item.
type SelectedOption struct {
	GroupID      uuid.UUID
	GroupName    string
	OptionID     uuid.UUID
	OptionName   string
	SurchargeVND int64
	Available    bool
	Retired      bool
	GroupRetired bool
}

// CommitSize is the Size a draft item selected, as it stands now.
type CommitSize struct {
	ID         uuid.UUID
	MenuItemID uuid.UUID
	Name       string
	PriceVND   int64
	Available  bool
	Retired    bool
}

// CommitCandidate is one draft item with everything Commit revalidates it
// against, loaded and locked by the handler.
//
// ItemPriceVND is nil exactly when the Menu Item is priced through its Sizes.
type CommitCandidate struct {
	DraftItemID     uuid.UUID
	MenuItemID      uuid.UUID
	ItemName        string
	CategoryName    string
	Quantity        int32
	ItemPriceVND    *int64
	PreparationNote *string
	SizeID          *uuid.UUID
	Size            *CommitSize
	EffectiveGroups []EffectiveGroup
	Selected        []SelectedOption
}

// ItemSnapshot is the immutable commercial record Commit persists.
type ItemSnapshot struct {
	SourceDraftItemID uuid.UUID
	MenuItemID        uuid.UUID
	CategoryName      string
	ItemName          string
	SizeName          *string
	Quantity          int32
	UnitPriceVND      int64
	TotalVND          int64
	PreparationNote   *string
	Modifiers         []CommittedModifierResponse
}

// BuildSnapshot revalidates one draft item and freezes it into a snapshot.
//
// The rule order is load-bearing and reproduces the canonical source: within
// each pair, retirement is reported before unavailability, because retirement
// is permanent and must not tell staff to try again later. Every error names
// the offending item, since a cashier told only that "an item is unavailable"
// cannot act on it.
//
// The canonical duplicate-option check is not migrated: 5A's
// order_draft_item_modifier_options primary key on (draft item, option) makes
// a duplicate unrepresentable, and a check that cannot fire is noise.
func BuildSnapshot(c CommitCandidate) (ItemSnapshot, error) {
	var zero ItemSnapshot

	basePriceVND, sizeName, err := resolveBasePrice(c)
	if err != nil {
		return zero, err
	}

	if err := validateSelections(c); err != nil {
		return zero, err
	}
	if err := validateGroupRules(c); err != nil {
		return zero, err
	}

	unitPriceVND := basePriceVND
	modifiers := make([]CommittedModifierResponse, 0, len(c.Selected))
	for _, opt := range c.Selected {
		unitPriceVND, err = AddCharge(unitPriceVND, opt.SurchargeVND)
		if err != nil {
			return zero, err
		}
		modifiers = append(modifiers, CommittedModifierResponse{
			GroupID:      opt.GroupID,
			GroupName:    opt.GroupName,
			OptionID:     opt.OptionID,
			OptionName:   opt.OptionName,
			SurchargeVND: opt.SurchargeVND,
		})
	}

	totalVND, err := LineTotal(c.Quantity, unitPriceVND)
	if err != nil {
		return zero, err
	}

	return ItemSnapshot{
		SourceDraftItemID: c.DraftItemID,
		MenuItemID:        c.MenuItemID,
		CategoryName:      c.CategoryName,
		ItemName:          c.ItemName,
		SizeName:          sizeName,
		Quantity:          c.Quantity,
		UnitPriceVND:      unitPriceVND,
		TotalVND:          totalVND,
		PreparationNote:   c.PreparationNote,
		Modifiers:         modifiers,
	}, nil
}

// resolveBasePrice applies the Size rules and returns the pre-surcharge price.
func resolveBasePrice(c CommitCandidate) (int64, *string, error) {
	sized := c.ItemPriceVND == nil

	if sized && c.SizeID == nil {
		return 0, nil, fmt.Errorf("%w: %s", ErrCommitSizeRequired, c.ItemName)
	}
	if !sized && c.SizeID != nil {
		return 0, nil, fmt.Errorf("%w: %s is priced directly", ErrCommitSizeInvalid, c.ItemName)
	}
	if !sized {
		return *c.ItemPriceVND, nil, nil
	}

	size := c.Size
	if size == nil || size.MenuItemID != c.MenuItemID {
		return 0, nil, fmt.Errorf("%w: %s", ErrCommitSizeInvalid, c.ItemName)
	}
	if size.Retired {
		return 0, nil, fmt.Errorf("%w: %s", ErrCommitSizeRetired, c.ItemName)
	}
	if !size.Available {
		return 0, nil, fmt.Errorf("%w: %s", ErrCommitSizeUnavailable, c.ItemName)
	}
	name := size.Name
	return size.PriceVND, &name, nil
}

// validateSelections rejects options that are no longer selectable.
func validateSelections(c CommitCandidate) error {
	effective := make(map[uuid.UUID]struct{}, len(c.EffectiveGroups))
	for _, g := range c.EffectiveGroups {
		effective[g.GroupID] = struct{}{}
	}
	for _, opt := range c.Selected {
		// A selection valid when it was made becomes invalid if the Item's
		// Category attachments changed since.
		if _, ok := effective[opt.GroupID]; !ok {
			return fmt.Errorf("%w: %s", ErrCommitModifierOptionInvalid, c.ItemName)
		}
		if opt.Retired || opt.GroupRetired {
			return fmt.Errorf("%w: %s", ErrCommitModifierOptionRetired, c.ItemName)
		}
		if !opt.Available {
			return fmt.Errorf("%w: %s", ErrCommitModifierOptionUnavailable, c.ItemName)
		}
	}
	return nil
}

// validateGroupRules enforces each effective Group's selection counts.
func validateGroupRules(c CommitCandidate) error {
	counts := make(map[uuid.UUID]int32, len(c.EffectiveGroups))
	for _, opt := range c.Selected {
		counts[opt.GroupID]++
	}
	for _, g := range c.EffectiveGroups {
		if g.Retired {
			// A retired Group that requires nothing is simply skipped; one
			// that requires a selection can no longer be satisfied.
			if g.MinSelections > 0 {
				return fmt.Errorf("%w: %s", ErrCommitModifierGroupRetired, c.ItemName)
			}
			continue
		}
		n := counts[g.GroupID]
		if n < g.MinSelections || n > g.MaxSelections {
			return fmt.Errorf("%w: %s", ErrCommitModifierGroupInvalid, c.ItemName)
		}
	}
	return nil
}

type commitFingerprint struct {
	ServiceSessionID uuid.UUID `json:"service_session_id"`
}

type commitAudit struct {
	ServiceSessionID   uuid.UUID `json:"service_session_id"`
	OrderDraftID       uuid.UUID `json:"order_draft_id"`
	CheckID            uuid.UUID `json:"check_id"`
	CommittedAmountVND int64     `json:"committed_amount_vnd"`
	CommittedItemCount int       `json:"committed_item_count"`
}

// CommitOrderDraftHandler fixes an Order Draft's prices into a Check.
type CommitOrderDraftHandler struct{ runner *Runner }

// NewCommitOrderDraftHandler creates a new CommitOrderDraftHandler.
func NewCommitOrderDraftHandler(runner *Runner) *CommitOrderDraftHandler {
	return &CommitOrderDraftHandler{runner: runner}
}

// Handle revalidates the draft, freezes its items, and charges a Check.
//
// Any failure aborts the whole Commit: there is no partial commit, and the
// draft stays EDITABLE and editable so staff can fix what was rejected.
func (h *CommitOrderDraftHandler) Handle(ctx context.Context, actor Actor,
	cmd CommitOrderDraftCommand,
) (int, ServiceSessionResponse, error) {
	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpCommitOrderDraft,
		Fingerprint: commitFingerprint{ServiceSessionID: cmd.ServiceSessionID},
		Required:    []string{CapSalesOperate},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(mc MutationContext) (int, ServiceSessionResponse, AuditRecord, error) {
			var zero ServiceSessionResponse
			q := mc.Queries

			draft, err := lockEditableDraft(ctx, q, cmd.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}

			candidates, err := loadCommitCandidates(ctx, q, draft.OrderDraftID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}

			committedAt := time.Now()
			snapshots := make([]ItemSnapshot, 0, len(candidates))
			var committedAmountVND int64
			for _, candidate := range candidates {
				snapshot, err := BuildSnapshot(candidate)
				if err != nil {
					return 0, zero, AuditRecord{}, err
				}
				committedAmountVND, err = AddCharge(committedAmountVND, snapshot.TotalVND)
				if err != nil {
					return 0, zero, AuditRecord{}, err
				}
				snapshots = append(snapshots, snapshot)
			}

			target, err := q.GetOrderDraftCheckTarget(ctx, draft.OrderDraftID)
			if err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("load draft check target: %w", err)
			}
			checkID, chargeVND, err := resolveTargetCheck(ctx, q,
				cmd.ServiceSessionID, target, committedAt)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}
			chargeVND, err = AddCharge(chargeVND, committedAmountVND)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}

			if err := persistSnapshots(ctx, q, draft.OrderDraftID, checkID,
				snapshots, committedAt); err != nil {
				return 0, zero, AuditRecord{}, err
			}
			if err := q.RaiseCheckCharge(ctx, sqlc.RaiseCheckChargeParams{
				ID: checkID, ChargeVnd: chargeVND,
			}); err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("raise check charge: %w", err)
			}
			if err := q.MarkOrderDraftCommitted(ctx, draft.OrderDraftID); err != nil {
				return 0, zero, AuditRecord{}, fmt.Errorf("mark draft committed: %w", err)
			}

			result, err := LoadServiceSession(ctx, q, cmd.ServiceSessionID)
			if err != nil {
				return 0, zero, AuditRecord{}, err
			}

			return 200, result, AuditRecord{
				EventType: EventOrderDraftCommitted,
				Details: commitAudit{
					ServiceSessionID:   cmd.ServiceSessionID,
					OrderDraftID:       draft.OrderDraftID,
					CheckID:            checkID,
					CommittedAmountVND: committedAmountVND,
					CommittedItemCount: len(snapshots),
				},
			}, nil
		})
}

// loadCommitCandidates reads and locks everything Commit revalidates against.
//
// Lock order is Sales rows before Catalog rows, as 5A established. The draft
// items are locked FOR UPDATE by the query; the Catalog rows are locked FOR
// SHARE, which blocks internal/catalog's FOR UPDATE mutations without
// serializing two concurrent Commits that share a menu item. See ADR-015.
func loadCommitCandidates(ctx context.Context, q *sqlc.Queries, draftID uuid.UUID) (
	[]CommitCandidate, error,
) {
	itemRows, err := q.LockDraftItemsForCommit(ctx, draftID)
	if err != nil {
		return nil, fmt.Errorf("lock draft items for commit: %w", err)
	}
	if len(itemRows) == 0 {
		return nil, fmt.Errorf("%w: draft %s", ErrEmptyDraft, draftID)
	}

	// sqlc types the query's `(retired_at IS NOT NULL)` expression as
	// interface{}; see sqlBool. The flags are converted up front so the two
	// whole-draft passes below stay boolean-straight.
	itemRetired := make([]bool, len(itemRows))
	for i, row := range itemRows {
		retired, err := sqlBool(row.ItemRetired, "menu item retirement")
		if err != nil {
			return nil, err
		}
		itemRetired[i] = retired
	}

	// Whole-draft checks run before any per-item rule, and retirement is
	// reported before unavailability across the entire draft. The precedence
	// is observable — a draft holding one of each reports the retired item
	// regardless of draft order — so it is reproduced rather than tidied into
	// per-item ordering.
	for i, row := range itemRows {
		if itemRetired[i] {
			return nil, fmt.Errorf("%w: %s", ErrCommitMenuItemRetired, row.ItemName)
		}
	}
	for _, row := range itemRows {
		if !row.ItemAvailable {
			return nil, fmt.Errorf("%w: %s", ErrCommitMenuItemUnavailable, row.ItemName)
		}
	}

	draftItemIDs := make([]uuid.UUID, 0, len(itemRows))
	menuItemIDs := make([]uuid.UUID, 0, len(itemRows))
	sizeIDs := make([]uuid.UUID, 0, len(itemRows))
	for _, row := range itemRows {
		draftItemIDs = append(draftItemIDs, row.ID)
		menuItemIDs = append(menuItemIDs, row.MenuItemID)
		if row.SizeID.Valid {
			sizeIDs = append(sizeIDs, row.SizeID.UUID)
		}
	}

	sizes, err := loadCommitSizes(ctx, q, sizeIDs)
	if err != nil {
		return nil, err
	}
	groups, err := loadEffectiveGroups(ctx, q, menuItemIDs)
	if err != nil {
		return nil, err
	}
	selected, err := loadSelectedOptions(ctx, q, draftItemIDs)
	if err != nil {
		return nil, err
	}

	out := make([]CommitCandidate, 0, len(itemRows))
	for _, row := range itemRows {
		candidate := CommitCandidate{
			DraftItemID:     row.ID,
			MenuItemID:      row.MenuItemID,
			ItemName:        row.ItemName,
			CategoryName:    row.CategoryName,
			Quantity:        row.Quantity,
			PreparationNote: nullStringPtr(row.PreparationNote),
			EffectiveGroups: groups[row.MenuItemID],
			Selected:        selected[row.ID],
		}
		if row.ItemPriceVnd.Valid {
			price := row.ItemPriceVnd.Int64
			candidate.ItemPriceVND = &price
		}
		if row.SizeID.Valid {
			sizeID := row.SizeID.UUID
			candidate.SizeID = &sizeID
			if size, ok := sizes[sizeID]; ok {
				candidate.Size = &size
			}
		}
		out = append(out, candidate)
	}
	return out, nil
}

func loadCommitSizes(ctx context.Context, q *sqlc.Queries, sizeIDs []uuid.UUID) (
	map[uuid.UUID]CommitSize, error,
) {
	out := make(map[uuid.UUID]CommitSize, len(sizeIDs))
	if len(sizeIDs) == 0 {
		return out, nil
	}
	rows, err := q.LockMenuItemSizesForCommit(ctx, sizeIDs)
	if err != nil {
		return nil, fmt.Errorf("lock menu item sizes for commit: %w", err)
	}
	for _, row := range rows {
		retired, err := sqlBool(row.SizeRetired, "size retirement")
		if err != nil {
			return nil, err
		}
		out[row.ID] = CommitSize{
			ID:         row.ID,
			MenuItemID: row.MenuItemID,
			Name:       row.Name,
			PriceVND:   row.PriceVnd,
			Available:  row.Available,
			Retired:    retired,
		}
	}
	return out, nil
}

func loadEffectiveGroups(ctx context.Context, q *sqlc.Queries, menuItemIDs []uuid.UUID) (
	map[uuid.UUID][]EffectiveGroup, error,
) {
	rows, err := q.ListEffectiveModifierGroupsForCommit(ctx, menuItemIDs)
	if err != nil {
		return nil, fmt.Errorf("resolve effective modifier groups: %w", err)
	}
	out := make(map[uuid.UUID][]EffectiveGroup, len(menuItemIDs))
	for _, row := range rows {
		retired, err := sqlBool(row.GroupRetired, "modifier group retirement")
		if err != nil {
			return nil, err
		}
		out[row.MenuItemID] = append(out[row.MenuItemID], EffectiveGroup{
			GroupID:       row.ModifierGroupID,
			GroupName:     row.GroupName,
			MinSelections: row.MinSelections,
			MaxSelections: row.MaxSelections,
			Retired:       retired,
		})
	}
	return out, nil
}

func loadSelectedOptions(ctx context.Context, q *sqlc.Queries, draftItemIDs []uuid.UUID) (
	map[uuid.UUID][]SelectedOption, error,
) {
	rows, err := q.ListDraftItemOptionsForCommit(ctx, draftItemIDs)
	if err != nil {
		return nil, fmt.Errorf("load selected modifier options: %w", err)
	}
	out := make(map[uuid.UUID][]SelectedOption, len(draftItemIDs))
	for _, row := range rows {
		optionRetired, err := sqlBool(row.OptionRetired, "modifier option retirement")
		if err != nil {
			return nil, err
		}
		groupRetired, err := sqlBool(row.GroupRetired, "modifier group retirement")
		if err != nil {
			return nil, err
		}
		out[row.OrderDraftItemID] = append(out[row.OrderDraftItemID], SelectedOption{
			GroupID:      row.GroupID,
			GroupName:    row.GroupName,
			OptionID:     row.OptionID,
			OptionName:   row.OptionName,
			SurchargeVND: row.SurchargeVnd,
			Available:    row.Available,
			Retired:      optionRetired,
			GroupRetired: groupRetired,
		})
	}
	return out, nil
}

// persistSnapshots writes the Committed Items, their frozen modifiers, and one
// full-quantity Charge Allocation each.
func persistSnapshots(ctx context.Context, q *sqlc.Queries, draftID, checkID uuid.UUID,
	snapshots []ItemSnapshot, committedAt time.Time,
) error {
	for _, snapshot := range snapshots {
		itemID, err := q.InsertCommittedItem(ctx, sqlc.InsertCommittedItemParams{
			OrderDraftID:      draftID,
			SourceDraftItemID: snapshot.SourceDraftItemID,
			MenuItemID:        snapshot.MenuItemID,
			CategoryName:      snapshot.CategoryName,
			ItemName:          snapshot.ItemName,
			SizeName:          nullString(snapshot.SizeName),
			Quantity:          snapshot.Quantity,
			UnitPriceVnd:      snapshot.UnitPriceVND,
			TotalVnd:          snapshot.TotalVND,
			PreparationNote:   nullString(snapshot.PreparationNote),
			CommittedAt:       committedAt,
		})
		if err != nil {
			return fmt.Errorf("insert committed item: %w", err)
		}

		for _, modifier := range snapshot.Modifiers {
			if err := q.InsertCommittedItemModifierOption(ctx,
				sqlc.InsertCommittedItemModifierOptionParams{
					CommittedItemID:    itemID,
					ModifierGroupID:    modifier.GroupID,
					ModifierGroupName:  modifier.GroupName,
					ModifierOptionID:   modifier.OptionID,
					ModifierOptionName: modifier.OptionName,
					SurchargeVnd:       modifier.SurchargeVND,
				}); err != nil {
				return fmt.Errorf("insert committed item modifier option: %w", err)
			}
		}

		// 5B always allocates the full committed quantity to one Check. 5C's
		// Split is what makes a partial allocation possible.
		if err := q.InsertChargeAllocation(ctx, sqlc.InsertChargeAllocationParams{
			CommittedItemID: itemID,
			CheckID:         checkID,
			Quantity:        snapshot.Quantity,
			CreatedAt:       committedAt,
		}); err != nil {
			return fmt.Errorf("insert charge allocation: %w", err)
		}
	}
	return nil
}

// sqlBool converts one of sqlc's `(retired_at IS NOT NULL)` expression fields
// to a bool. sqlc cannot infer the boolean type of those expressions for the
// database/sql engine, so the generated fields are interface{}; pgx scans
// PostgreSQL booleans into bool. The conversion fails closed, as
// catalog_resolution.go's identical handling does, rather than silently
// treating a retired row as active.
func sqlBool(v any, what string) (bool, error) {
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("scan %s flag: unexpected type %T", what, v)
	}
	return b, nil
}

// nullString converts an optional string to the nullable text type the
// generated queries write.
func nullString(v *string) sql.NullString {
	if v == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *v, Valid: true}
}

// resolveTargetCheck returns the Check the Commit's charges join, creating one
// when needed, and its current charge.
//
// CURRENT_UNPAID reuses the Session's most recent OPEN Check; NEW_CHECK always
// opens one. The name is canonical and "unpaid" is vacuous in 5B, where no
// Check can be paid — the state = 'OPEN' filter is what gives it meaning from
// 5C, when it must skip settled Checks and reuse only one still awaiting
// money.
func resolveTargetCheck(ctx context.Context, q *sqlc.Queries, sessionID uuid.UUID,
	target string, at time.Time,
) (uuid.UUID, int64, error) {
	if target == CheckTargetCurrentUnpaid {
		existing, err := q.LockCurrentOpenCheck(ctx, sessionID)
		if err == nil {
			return existing.ID, existing.ChargeVnd, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return uuid.Nil, 0, fmt.Errorf("lock current open check: %w", err)
		}
	}

	created, err := q.InsertCheck(ctx, sqlc.InsertCheckParams{
		ServiceSessionID: sessionID,
		CreatedAt:        at,
	})
	if err != nil {
		return uuid.Nil, 0, fmt.Errorf("insert check: %w", err)
	}
	return created.ID, created.ChargeVnd, nil
}
