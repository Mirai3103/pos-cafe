package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
)

// ApproverSummary identifies the Manager who approved a second-party command.
// It carries exactly these three fields; no PIN hash, role list, enablement
// flag, or session detail crosses a slice boundary.
type ApproverSummary struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"display_name"`
	LoginCode   string    `json:"login_code"`
}

// ErrManagerApprovalDenied reports that inline Manager approval failed. The
// wrapped text names the reason for the server log and the denial audit event.
// Callers must collapse every reason to one client-visible code, so that the
// API does not disclose which condition failed.
var ErrManagerApprovalDenied = errors.New("manager approval denied")

// Denial reasons. These are logged and audited, never returned to a client.
const (
	ApprovalDenialInvalidPin          = "INVALID_PIN"
	ApprovalDenialIdentityDisabled    = "IDENTITY_DISABLED"
	ApprovalDenialManagerRoleRequired = "MANAGER_ROLE_REQUIRED"
	ApprovalDenialCapabilityRequired  = "CAPABILITY_REQUIRED"
)

// NormalizeLoginCode trims surrounding whitespace and upper-cases a login code,
// matching the upper(btrim(...)) lookup used by the identity queries.
func NormalizeLoginCode(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
}

// evaluateApprover decides the denial reason from already-gathered facts, or
// returns "" when approval is granted. It is pure so every branch is unit
// tested without a database.
//
// A missing identity and a wrong PIN both report INVALID_PIN: the caller runs
// PIN verification against a dummy hash when no identity matched, so neither
// the response body nor the response timing reveals whether a login code exists.
func evaluateApprover(found, pinOK, enabled bool, roles []string, requiredCapability string) string {
	if !found || !pinOK {
		return ApprovalDenialInvalidPin
	}
	if !enabled {
		return ApprovalDenialIdentityDisabled
	}
	if !slices.Contains(roles, RoleManager) {
		return ApprovalDenialManagerRoleRequired
	}
	if !slices.Contains(DeriveCapabilities(roles), requiredCapability) {
		return ApprovalDenialCapabilityRequired
	}
	return ""
}

// VerifyManagerApproval authenticates a second identity inline and confirms it
// may approve an operation requiring requiredCapability.
//
// It must be called with a transaction-scoped Queries so that the approver row
// lock it takes holds for the rest of the operation: a concurrent disablement
// or role change cannot interleave between verification and use.
//
// Self-approval is permitted. A Manager working alone supplies their own login
// code and PIN; callers record initiator and approver separately, so a
// self-approved command stays distinguishable in the audit trail.
func VerifyManagerApproval(
	ctx context.Context,
	q *sqlc.Queries,
	approverLoginCode string,
	managerPin string,
	requiredCapability string,
) (ApproverSummary, error) {
	normalized := NormalizeLoginCode(approverLoginCode)

	approver, err := q.GetStaffByLoginCodeForUpdate(ctx, normalized)
	found := true
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return ApproverSummary{}, fmt.Errorf("lookup approver: %w", err)
		}
		found = false
	}

	// Always spend the bcrypt comparison, even with no match, so response
	// timing does not distinguish an unknown login code from a wrong PIN.
	pinHash := dummyBcryptHash
	if found {
		pinHash = approver.PinHash
	}
	pinOK := VerifyPin(pinHash, managerPin)

	var roles []string
	if found && pinOK && approver.Enabled {
		roles, err = q.GetStaffRolesForUpdate(ctx, approver.ID)
		if err != nil {
			return ApproverSummary{}, fmt.Errorf("load approver roles: %w", err)
		}
	}

	if reason := evaluateApprover(found, pinOK, approver.Enabled, roles, requiredCapability); reason != "" {
		return ApproverSummary{}, fmt.Errorf("%w: %s", ErrManagerApprovalDenied, reason)
	}

	return ApproverSummary{
		ID:          approver.ID,
		DisplayName: approver.DisplayName,
		LoginCode:   approver.LoginCode,
	}, nil
}
