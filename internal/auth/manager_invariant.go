package auth

import (
	"context"
	"fmt"
	"slices"

	"github.com/Mirai3103/pos-cafe/internal/platform/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/response"
)

// ensureManagerRemains returns ErrManagerInvariant when an enabled Manager would
// stop being one and no other enabled Manager exists. The caller must hold
// EnabledManagerInvariantLockID for the rest of its transaction.
func ensureManagerRemains(ctx context.Context, qtx *sqlc.Queries, targetEnabled bool, currentRoles, nextRoles []string) error {
	if !targetEnabled || !slices.Contains(currentRoles, RoleManager) || slices.Contains(nextRoles, RoleManager) {
		return nil
	}
	activeManagers, err := qtx.CountActiveManagers(ctx)
	if err != nil {
		return fmt.Errorf("count active managers: %w", err)
	}
	if activeManagers <= 1 {
		return fmt.Errorf("%w: phải còn ít nhất một Quản lý đang hoạt động", response.ErrManagerInvariant)
	}
	return nil
}
