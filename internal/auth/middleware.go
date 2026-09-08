package auth

import (
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

const StaffContextKey = "auth_staff_claims"

type StaffClaims struct {
	StaffID      uuid.UUID
	SessionID    uuid.UUID
	DisplayName  string
	LoginCode    string
	Roles        []string
	Capabilities []string
	Workspace    *string
}

func GetStaff(c echo.Context) *StaffClaims {
	if val := c.Get(StaffContextKey); val != nil {
		if claims, ok := val.(*StaffClaims); ok {
			return claims
		}
	}
	return nil
}
