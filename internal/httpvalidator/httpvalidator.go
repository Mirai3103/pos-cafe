// Package httpvalidator adapts go-playground/validator to Echo's echo.Validator
// interface and turns its field errors into messages that are safe and readable
// to return to an API client.
//
// The package is deliberately not named "validator": that would collide with the
// go-playground import it is built on, forcing an alias at every call site.
package httpvalidator

import (
	"errors"
	"fmt"
	"strings"

	"github.com/go-playground/validator/v10"
)

type CustomValidator struct {
	validator *validator.Validate
}

func New() *CustomValidator {
	return &CustomValidator{
		validator: validator.New(),
	}
}

// Validate implements echo.Validator. Field-level failures are joined into a
// single human-readable message; anything else (notably a non-struct argument,
// which is a programming error) is returned unchanged.
func (cv *CustomValidator) Validate(i any) error {
	err := cv.validator.Struct(i)
	if err == nil {
		return nil
	}

	var validationErrors validator.ValidationErrors
	if !errors.As(err, &validationErrors) {
		return err
	}

	msgs := make([]string, 0, len(validationErrors))
	for _, fe := range validationErrors {
		msgs = append(msgs, formatFieldError(fe))
	}
	return errors.New(strings.Join(msgs, ", "))
}

func formatFieldError(fe validator.FieldError) string {
	field := strings.ToLower(fe.Field())
	switch fe.Tag() {
	case "required":
		return fmt.Sprintf("field '%s' is required", field)
	case "min":
		return fmt.Sprintf("field '%s' must be at least %s characters", field, fe.Param())
	case "max":
		return fmt.Sprintf("field '%s' must be at most %s characters", field, fe.Param())
	case "gte":
		return fmt.Sprintf("field '%s' must be greater than or equal to %s", field, fe.Param())
	case "lte":
		return fmt.Sprintf("field '%s' must be less than or equal to %s", field, fe.Param())
	default:
		return fmt.Sprintf("field '%s' failed validation for rule '%s'", field, fe.Tag())
	}
}
