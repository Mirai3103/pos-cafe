package validator

import (
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

func (cv *CustomValidator) Validate(i interface{}) error {
	if err := cv.validator.Struct(i); err != nil {
		if validationErrors, ok := err.(validator.ValidationErrors); ok {
			var errMsgs []string
			for _, fe := range validationErrors {
				errMsgs = append(errMsgs, formatFieldError(fe))
			}
			return fmt.Errorf("%s", strings.Join(errMsgs, ", "))
		}
		return err
	}
	return nil
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
