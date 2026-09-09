package auth

import (
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/httpvalidator"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestSignInRequestValidation(t *testing.T) {
	v := httpvalidator.New()

	valid := SignInRequest{
		LoginCode: "QL01",
		Pin:       "1234",
	}
	assert.NoError(t, v.Validate(&valid))

	invalid := SignInRequest{
		LoginCode: "",
		Pin:       "12", // too short
	}
	assert.Error(t, v.Validate(&invalid))

	invalidAlpha := SignInRequest{
		LoginCode: "QL01",
		Pin:       "abcd", // non-numeric
	}
	assert.Error(t, v.Validate(&invalidAlpha))
}

func TestBootstrapManagerRequestValidation(t *testing.T) {
	v := httpvalidator.New()

	valid := BootstrapManagerRequest{
		DisplayName: "Quản Lý 1",
		LoginCode:   "QL01",
		Pin:         "1234",
	}
	assert.NoError(t, v.Validate(&valid))

	tooShortName := BootstrapManagerRequest{
		DisplayName: "A",
		LoginCode:   "QL01",
		Pin:         "1234",
	}
	assert.Error(t, v.Validate(&tooShortName))

	invalidPin := BootstrapManagerRequest{
		DisplayName: "Quản Lý 1",
		LoginCode:   "QL01",
		Pin:         "123",
	}
	assert.Error(t, v.Validate(&invalidPin))
}

func TestUnlockRequestValidation(t *testing.T) {
	v := httpvalidator.New()

	valid := UnlockRequest{
		Pin: "1234",
	}
	assert.NoError(t, v.Validate(&valid))

	invalid := UnlockRequest{
		Pin: "12",
	}
	assert.Error(t, v.Validate(&invalid))
}

func TestDeclareWorkspaceRequestValidation(t *testing.T) {
	v := httpvalidator.New()

	validWorkspaces := []string{WorkspaceCashier, WorkspaceManager, WorkspacePreparation}
	for _, ws := range validWorkspaces {
		req := DeclareWorkspaceRequest{Workspace: ws}
		assert.NoError(t, v.Validate(&req))
	}

	invalid := DeclareWorkspaceRequest{Workspace: "kitchen"}
	assert.Error(t, v.Validate(&invalid))
}

func TestCreateStaffRequestValidation(t *testing.T) {
	v := httpvalidator.New()

	valid := CreateStaffRequest{
		RequestID:   uuid.New(),
		DisplayName: "Nguyễn Văn A",
		LoginCode:   "NV01",
		Enabled:     true,
		Roles:       []string{RoleCashier},
		Pin:         "1234",
		ManagerPin:  "9999",
	}
	assert.NoError(t, v.Validate(&valid))

	invalid := CreateStaffRequest{
		RequestID:   uuid.Nil,
		DisplayName: "",
		Roles:       []string{},
		Pin:         "123",
	}
	assert.Error(t, v.Validate(&invalid))

	invalidRole := CreateStaffRequest{
		RequestID:   uuid.New(),
		DisplayName: "Nguyễn Văn B",
		LoginCode:   "NV02",
		Enabled:     true,
		Roles:       []string{"SUPERADMIN"},
		Pin:         "1234",
		ManagerPin:  "9999",
	}
	assert.Error(t, v.Validate(&invalidRole))
}

func TestSetStaffEnabledRequestValidation(t *testing.T) {
	v := httpvalidator.New()

	valid := SetStaffEnabledRequest{
		RequestID:       uuid.New(),
		ExpectedEnabled: true,
		Enabled:         false,
		ManagerPin:      "1234",
	}
	assert.NoError(t, v.Validate(&valid))

	invalidNilUUID := SetStaffEnabledRequest{
		RequestID:       uuid.Nil,
		ExpectedEnabled: true,
		Enabled:         false,
		ManagerPin:      "1234",
	}
	assert.Error(t, v.Validate(&invalidNilUUID))

	invalidPin := SetStaffEnabledRequest{
		RequestID:       uuid.New(),
		ExpectedEnabled: true,
		Enabled:         false,
		ManagerPin:      "12",
	}
	assert.Error(t, v.Validate(&invalidPin))
}

func TestReplaceStaffRolesRequestValidation(t *testing.T) {
	v := httpvalidator.New()

	valid := ReplaceStaffRolesRequest{
		RequestID:  uuid.New(),
		Roles:      []string{RoleManager, RoleCashier},
		ManagerPin: "1234",
	}
	assert.NoError(t, v.Validate(&valid))

	emptyRoles := ReplaceStaffRolesRequest{
		RequestID:  uuid.New(),
		Roles:      []string{},
		ManagerPin: "1234",
	}
	assert.Error(t, v.Validate(&emptyRoles))

	invalidRoles := ReplaceStaffRolesRequest{
		RequestID:  uuid.New(),
		Roles:      []string{"INVALID"},
		ManagerPin: "1234",
	}
	assert.Error(t, v.Validate(&invalidRoles))
}

func TestResetStaffPinRequestValidation(t *testing.T) {
	v := httpvalidator.New()

	valid := ResetStaffPinRequest{
		RequestID:  uuid.New(),
		Pin:        "5678",
		ManagerPin: "1234",
	}
	assert.NoError(t, v.Validate(&valid))

	shortPin := ResetStaffPinRequest{
		RequestID:  uuid.New(),
		Pin:        "56",
		ManagerPin: "1234",
	}
	assert.Error(t, v.Validate(&shortPin))
}
