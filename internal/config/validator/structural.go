package validator

import (
	"errors"
	"fmt"
	"strings"

	v10 "github.com/go-playground/validator/v10"
)

// structValidator enforces the structural (field-level) rules declared with
// `validate:"..."` tags on internal/config and internal/domain types.
var structValidator = v10.New()

// ValidateStruct validates any struct decorated with validate tags (e.g.
// Management API request DTOs) and returns aggregated structured errors.
func ValidateStruct(v interface{}) ValidationErrors {
	if v == nil {
		return ValidationErrors{{Code: CodeInvalidValue, Message: "payload is nil"}}
	}
	err := structValidationErrors(v)
	if len(err) == 0 {
		return nil
	}
	return err
}

func structValidationErrors(v interface{}) ValidationErrors {
	err := structValidator.Struct(v)
	if err == nil {
		return nil
	}
	var tagged v10.ValidationErrors
	if !errors.As(err, &tagged) {
		return ValidationErrors{{Code: CodeInvalidValue, Message: err.Error()}}
	}
	out := make(ValidationErrors, 0, len(tagged))
	for _, fe := range tagged {
		out = append(out, ValidationError{
			Field:   fieldPath(fe),
			Code:    codeForTag(fe.Tag()),
			Message: messageForTag(fe),
		})
	}
	return out
}

// fieldPath renders the struct path of a failed field as a stable, readable
// path, e.g. "Services[1].Plugins[0].Name" — the enclosing type name is
// trimmed so the path can be mapped back onto config/docs fields easily.
func fieldPath(fe v10.FieldError) string {
	ns := fe.Namespace()
	if i := strings.IndexByte(ns, '.'); i >= 0 {
		ns = ns[i+1:]
	}
	return ns
}

func codeForTag(tag string) string {
	switch tag {
	case "required":
		return CodeRequired
	case "oneof", "min", "max":
		return CodeInvalidValue
	case "unique":
		return CodeDuplicate
	default:
		return CodeInvalidFormat
	}
}

func messageForTag(fe v10.FieldError) string {
	field := fe.Field()
	switch fe.Tag() {
	case "required":
		return field + " is required"
	case "oneof":
		return field + " must be one of: " + fe.Param()
	case "min", "max":
		return fmt.Sprintf("%s failed the %s rule (limit: %s)", field, fe.Tag(), fe.Param())
	default:
		return field + " failed validation rule " + fe.Tag() + " " + fe.Param()
	}
}
