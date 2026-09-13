package sales

import (
	"context"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

// effectiveGroupIDs returns the Modifier Groups that apply to a Menu Item:
// those inherited from its Menu Category and not excluded by it, plus those
// attached directly.
func effectiveGroupIDs(ctx context.Context, q *sqlc.Queries, menuItemID uuid.UUID) (
	[]uuid.UUID, error,
) {
	ids, err := q.ListEffectiveModifierGroupIDs(ctx, menuItemID)
	if err != nil {
		return nil, fmt.Errorf("resolve effective modifier groups: %w", err)
	}
	return ids, nil
}

// defaultOptionIDs returns the declared default options of a Menu Item's
// effective Groups, filtered to those currently selectable, along with those
// effective Group ids themselves so a caller that must also validate the
// result (AddDraftItemHandler) does not resolve them a second time.
//
// These apply only when an add request omits modifier_option_ids entirely. An
// explicitly empty list means the customer declined every option and is taken
// literally.
func defaultOptionIDs(ctx context.Context, q *sqlc.Queries, menuItemID uuid.UUID) (
	ids []uuid.UUID, groups []uuid.UUID, err error,
) {
	groups, err = effectiveGroupIDs(ctx, q, menuItemID)
	if err != nil {
		return nil, nil, err
	}
	if len(groups) == 0 {
		return []uuid.UUID{}, groups, nil
	}
	ids, err = q.ListDefaultModifierOptionIDs(ctx, groups)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve default modifier options: %w", err)
	}
	return ids, groups, nil
}

// validateModifierOptions rejects any option that is not currently selectable
// for this Menu Item.
//
// An option belonging to a Group that is not effective for the Item returns
// ErrModifierOptionNotFound rather than a distinct code, matching the
// canonical source: from the caller's position the option is not selectable
// here, and the reason is not the caller's business.
//
// Note the check order. Retirement is permanent and unavailability is
// temporary, so a retired option must report retirement even though it is also
// unavailable; reporting "try again later" for something that will never
// return would be wrong.
func validateModifierOptions(ctx context.Context, q *sqlc.Queries,
	menuItemID uuid.UUID, optionIDs []uuid.UUID,
) error {
	groups, err := effectiveGroupIDs(ctx, q, menuItemID)
	if err != nil {
		return err
	}
	return validateModifierOptionsForGroups(ctx, q, groups, optionIDs)
}

// validateModifierOptionsForGroups is validateModifierOptions for a caller
// that has already resolved the Menu Item's effective Groups (AddDraftItemHandler's
// default-options path), so the resolution query does not run twice.
func validateModifierOptionsForGroups(ctx context.Context, q *sqlc.Queries,
	groups []uuid.UUID, optionIDs []uuid.UUID,
) error {
	if len(optionIDs) == 0 {
		return nil
	}
	if HasDuplicateUUIDs(optionIDs) {
		return fmt.Errorf("%w: the same modifier option was selected twice", ErrModifierOptionNotFound)
	}

	effective := make(map[uuid.UUID]struct{}, len(groups))
	for _, id := range groups {
		effective[id] = struct{}{}
	}

	rows, err := q.ListModifierOptionsForValidation(ctx, optionIDs)
	if err != nil {
		return fmt.Errorf("load modifier options: %w", err)
	}
	byID := make(map[uuid.UUID]sqlc.ListModifierOptionsForValidationRow, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}

	for _, id := range optionIDs {
		row, ok := byID[id]
		if !ok {
			return fmt.Errorf("%w: %s", ErrModifierOptionNotFound, id)
		}
		if _, applies := effective[row.ModifierGroupID]; !applies {
			return fmt.Errorf("%w: %s is not offered for this menu item", ErrModifierOptionNotFound, id)
		}
		// sqlc cannot infer the boolean type of the query's
		// `(retired_at IS NOT NULL)` expressions for the database/sql
		// engine, so the generated fields are interface{}; pgx scans
		// PostgreSQL booleans into bool. A mismatch here must fail closed
		// rather than silently treat a retired row as active.
		optionRetired, ok := row.OptionRetired.(bool)
		if !ok {
			return fmt.Errorf("scan modifier option retirement flag: unexpected type %T", row.OptionRetired)
		}
		groupRetired, ok := row.GroupRetired.(bool)
		if !ok {
			return fmt.Errorf("scan modifier group retirement flag: unexpected type %T", row.GroupRetired)
		}
		if optionRetired || groupRetired {
			return fmt.Errorf("%w: %s", ErrModifierOptionRetired, id)
		}
		if !row.Available {
			return fmt.Errorf("%w: %s", ErrModifierOptionUnavailable, id)
		}
	}
	return nil
}
