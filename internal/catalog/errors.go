package catalog

import "errors"

var (
	ErrNotFound                     = errors.New("catalog entity not found")
	ErrNameConflict                 = errors.New("catalog name conflict")
	ErrRequestConflict              = errors.New("request conflict")
	ErrInvalidPricingConfiguration  = errors.New("invalid pricing configuration")
	ErrInvalidModifierConfiguration = errors.New("invalid modifier configuration")
	ErrInvalidInheritance           = errors.New("invalid inheritance")
	ErrEntityRetired                = errors.New("entity retired")
	ErrInvalidManagerPin            = errors.New("invalid manager pin")
	ErrForbidden                    = errors.New("forbidden")
	ErrUnauthorized                 = errors.New("unauthorized")
	ErrInvalidStoredResult          = errors.New("invalid stored result")
)
