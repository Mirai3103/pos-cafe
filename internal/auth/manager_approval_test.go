package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeLoginCode(t *testing.T) {
	cases := []struct{ in, want string }{
		{"qla", "QLA"},
		{"  qla  ", "QLA"},
		{"QLA", "QLA"},
		{"\tQl A\n", "QL A"},
		{"", ""},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, NormalizeLoginCode(tc.in), "input %q", tc.in)
	}
}

func TestEvaluateApprover(t *testing.T) {
	managerRoles := []string{RoleManager}
	cashierRoles := []string{RoleCashier}

	cases := []struct {
		name       string
		found      bool
		pinOK      bool
		enabled    bool
		roles      []string
		capability string
		want       string
	}{
		{
			name: "approved", found: true, pinOK: true, enabled: true,
			roles: managerRoles, capability: "sales_shift.operate", want: "",
		},
		{
			// An unknown login code must be indistinguishable from a wrong PIN.
			name: "unknown login code", found: false, pinOK: false, enabled: false,
			roles: nil, capability: "sales_shift.operate", want: ApprovalDenialInvalidPin,
		},
		{
			name: "wrong pin", found: true, pinOK: false, enabled: true,
			roles: managerRoles, capability: "sales_shift.operate", want: ApprovalDenialInvalidPin,
		},
		{
			name: "disabled manager", found: true, pinOK: true, enabled: false,
			roles: managerRoles, capability: "sales_shift.operate", want: ApprovalDenialIdentityDisabled,
		},
		{
			name: "cashier cannot approve", found: true, pinOK: true, enabled: true,
			roles: cashierRoles, capability: "sales_shift.operate", want: ApprovalDenialManagerRoleRequired,
		},
		{
			name: "no roles at all", found: true, pinOK: true, enabled: true,
			roles: nil, capability: "sales_shift.operate", want: ApprovalDenialManagerRoleRequired,
		},
		{
			name: "manager lacking the capability", found: true, pinOK: true, enabled: true,
			roles: managerRoles, capability: "nonexistent.capability", want: ApprovalDenialCapabilityRequired,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := evaluateApprover(tc.found, tc.pinOK, tc.enabled, tc.roles, tc.capability)
			assert.Equal(t, tc.want, got)
		})
	}
}
