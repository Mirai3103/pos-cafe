package auth

import (
	"time"

	"github.com/google/uuid"
)

// === Authentication DTOs ===

type BootstrapManagerRequest struct {
	DisplayName string `json:"display_name" validate:"required,min=2,max=120"`
	LoginCode   string `json:"login_code" validate:"required,min=2,max=24,alphanumunicode"`
	Pin         string `json:"pin" validate:"required,min=4,max=8,numeric"`
}

type SignInRequest struct {
	LoginCode string `json:"login_code" validate:"required,min=1,max=24"`
	Pin       string `json:"pin" validate:"required,min=4,max=8,numeric"`
}

type UnlockRequest struct {
	Pin string `json:"pin" validate:"required,min=4,max=8,numeric"`
}

type DeclareWorkspaceRequest struct {
	Workspace string `json:"workspace" validate:"required,oneof=cashier manager preparation"`
}

type IdentitySummaryResponse struct {
	DisplayName string `json:"display_name"`
	LoginCode   string `json:"login_code"`
}

type StaffProfileResponse struct {
	ID           uuid.UUID `json:"id"`
	DisplayName  string    `json:"display_name"`
	LoginCode    string    `json:"login_code"`
	Enabled      bool      `json:"enabled"`
	Roles        []string  `json:"roles"`
	Capabilities []string  `json:"capabilities"`
}

type SignInResponse struct {
	Token string               `json:"token"`
	Staff StaffProfileResponse `json:"staff"`
}

type SessionStateResponse struct {
	State        string    `json:"state"` // "authenticated", "locked", "signed_out"
	StaffID      uuid.UUID `json:"staff_id,omitempty"`
	DisplayName  string    `json:"display_name,omitempty"`
	LoginCode    string    `json:"login_code,omitempty"`
	Roles        []string  `json:"roles,omitempty"`
	Capabilities []string  `json:"capabilities,omitempty"`
	Workspace    *string   `json:"workspace,omitempty"`
}

// === Staff Administration DTOs ===

type CreateStaffRequest struct {
	RequestID   uuid.UUID `json:"request_id" validate:"required"`
	DisplayName string    `json:"display_name" validate:"required,min=1,max=120"`
	LoginCode   string    `json:"login_code" validate:"required,min=1,max=24"`
	Enabled     bool      `json:"enabled"`
	Roles       []string  `json:"roles" validate:"required,min=1,dive,oneof=MANAGER CASHIER BARISTA"`
	Pin         string    `json:"pin" validate:"required,min=4,max=8,numeric"`
	ManagerPin  string    `json:"manager_pin" validate:"required,min=4,max=8,numeric"`
}

type SetStaffEnabledRequest struct {
	RequestID       uuid.UUID `json:"request_id" validate:"required"`
	ExpectedEnabled bool      `json:"expected_enabled"`
	Enabled         bool      `json:"enabled"`
	ManagerPin      string    `json:"manager_pin" validate:"required,min=4,max=8,numeric"`
}

type ReplaceStaffRolesRequest struct {
	RequestID  uuid.UUID `json:"request_id" validate:"required"`
	Roles      []string  `json:"roles" validate:"required,min=1,dive,oneof=MANAGER CASHIER BARISTA"`
	ManagerPin string    `json:"manager_pin" validate:"required,min=4,max=8,numeric"`
}

type ResetStaffPinRequest struct {
	RequestID  uuid.UUID `json:"request_id" validate:"required"`
	Pin        string    `json:"pin" validate:"required,min=4,max=8,numeric"`
	ManagerPin string    `json:"manager_pin" validate:"required,min=4,max=8,numeric"`
}

type StaffDetailResponse struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"display_name"`
	LoginCode   string    `json:"login_code"`
	Enabled     bool      `json:"enabled"`
	Roles       []string  `json:"roles"`
	CreatedAt   time.Time `json:"created_at"`
}
