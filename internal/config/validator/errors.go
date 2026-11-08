// Package validator provides the single centralized configuration validation
// entry point for the gateway. Every configuration entry point — the Management
// API, the Config Manager (file load / reload / CRUD commits) and the Runtime
// Builder — validates candidates through this package. Rules are declared once
// here and must never be duplicated in callers.
package validator

import (
	"fmt"
	"strings"
)

// Validation error codes surfaced to API consumers. Comparable constants
// instead of free-form strings so handlers/tooling can switch on them.
const (
	CodeRequired             = "required"
	CodeInvalidFormat        = "invalid_format"
	CodeInvalidValue         = "invalid_value"
	CodeDuplicate            = "duplicate"
	CodeUnknownReference     = "unknown_reference"
	CodeUnknownPlugin        = "unknown_plugin"
	CodeInvalidPluginConfig  = "invalid_plugin_config"
	CodeInvalidLoadBalancing = "invalid_load_balancing"
)

// ValidationError describes one configuration problem in a structured way.
type ValidationError struct {
	// Field is the dotted path to the offending configuration node, using
	// the Go struct names of pkg config/validator, e.g. "Services[1].ConnectTimeout".
	Field string `json:"field"`
	// Code is one of the Code* constants above.
	Code string `json:"code"`
	// Message is a human-readable, self-contained description.
	Message string `json:"message"`
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("[%s] %s: %s", e.Code, e.Field, e.Message)
}

// ValidationErrors aggregates all problems found in one configuration, so a
// user fixes everything in a single attempt. Implements error; nil when valid.
type ValidationErrors []ValidationError

func (ve ValidationErrors) Error() string {
	if len(ve) == 0 {
		return "no validation errors"
	}
	parts := make([]string, 0, len(ve))
	for _, e := range ve {
		parts = append(parts, e.Error())
	}
	return fmt.Sprintf("configuration validation failed with %d error(s): %s", len(ve), strings.Join(parts, "; "))
}
