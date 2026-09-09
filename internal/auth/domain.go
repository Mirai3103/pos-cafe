package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"regexp"
	"slices"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Operational Roles
const (
	RoleManager = "MANAGER"
	RoleCashier = "CASHIER"
	RoleBarista = "BARISTA"
)

var AllRoles = []string{RoleManager, RoleCashier, RoleBarista}

// Workspaces
const (
	WorkspaceCashier     = "cashier"
	WorkspaceManager     = "manager"
	WorkspacePreparation = "preparation"
)

// Session States
const (
	SessionStateActive        = "active"
	SessionStateAuthenticated = "authenticated"
	SessionStateLocked        = "locked"
)

// Inactivity Timeouts
const (
	CashierManagerInactivityTimeout = 5 * time.Minute
	PreparationInactivityTimeout    = 15 * time.Minute
	SessionDuration                 = 12 * time.Hour
)

// Capabilities
var RoleCapabilities = map[string][]string{
	RoleManager: {
		"catalog.view_prices",
		"catalog.manage_availability",
		"catalog.administer_structure",
		"catalog.change_price",
		"sales.operate",
		"sales_shift.operate",
		"preparation.operate",
		"staff.administer",
		"audit.inspect",
		"tables.administer",
	},
	RoleCashier: {
		"catalog.view_prices",
		"catalog.manage_availability",
		"sales.operate",
		"sales_shift.operate",
	},
	RoleBarista: {
		"catalog.manage_availability",
		"preparation.operate",
	},
}

var pinRegex = regexp.MustCompile(`^\d{4,8}$`)

// Dummy hash for constant-time comparison when loginCode is unknown
const dummyBcryptHash = "$2a$10$92IXUNpkjO0rOQ5byMi.Ye4oKoEa3Ro9llC/.og/at2.uheWG/igi"

func ValidatePinFormat(pin string) error {
	if !pinRegex.MatchString(pin) {
		return errors.New("mã PIN phải có từ 4 đến 8 chữ số")
	}
	return nil
}

func HashPin(pin string) (string, error) {
	if err := ValidatePinFormat(pin); err != nil {
		return "", err
	}
	bytes, err := bcrypt.GenerateFromPassword([]byte(pin), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func VerifyPin(pinHash, attemptedPin string) bool {
	target := pinHash
	if target == "" {
		target = dummyBcryptHash
	}
	err := bcrypt.CompareHashAndPassword([]byte(target), []byte(attemptedPin))
	return err == nil && pinHash != ""
}

func DeriveCapabilities(roles []string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0)
	for _, role := range roles {
		caps, ok := RoleCapabilities[role]
		if !ok {
			continue
		}
		for _, c := range caps {
			if _, exists := seen[c]; !exists {
				seen[c] = struct{}{}
				result = append(result, c)
			}
		}
	}
	slices.Sort(result)
	return result
}

func GenerateSessionToken() (token string, tokenHash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	tokenHash = HashToken(token)
	return token, tokenHash, nil
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func GetInactivityTimeout(workspace string) time.Duration {
	if workspace == WorkspacePreparation {
		return PreparationInactivityTimeout
	}
	return CashierManagerInactivityTimeout
}
