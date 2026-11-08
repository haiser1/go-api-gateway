package validator

import (
	"github.com/haiser1/go-api-gateway/internal/config"
)

// Validate is the single centralized entry point for whole-configuration
// validation. It aggregates structural (field-level, tag-driven) and semantic
// (cross-entity) errors, returning all problems at once. A nil slice means
// the configuration is valid.
//
// It is used by all configuration entry points:
//   - the Config Manager, via config.SetValidator (wired by this package on
//     import, so file loads, reloads and CRUD commits are all validated),
//   - the Management API handlers via ValidateStruct on request DTOs,
//   - the Runtime Builder (gateway.buildRuntimeSnapshot) as its final safety
//     boundary before compiling a runtime candidate.
func Validate(cfg *config.Config) ValidationErrors {
	if cfg == nil {
		return ValidationErrors{{
			Field:   "Config",
			Code:    CodeInvalidValue,
			Message: "configuration is nil",
		}}
	}
	errs := ValidateStruct(cfg)
	errs = append(errs, validateSemantics(cfg)...)
	if len(errs) == 0 {
		return nil
	}
	return errs
}

// init wires this package into the config manager: importing the validator
// package (directly or transitively, e.g. via the gateway) installs it as the
// centralized validation boundary for file loads, reloads and CRUD commits.
func init() {
	config.SetValidator(func(cfg *config.Config) error {
		errs := Validate(cfg)
		if len(errs) == 0 {
			return nil
		}
		return errs
	})
}
