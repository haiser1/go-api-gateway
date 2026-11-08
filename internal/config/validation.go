package config

import (
	"fmt"
	"sync"
)

// Centralized validation wiring. The rules themselves live in
// internal/config/validator (single source of truth); this file only holds
// the dependency inversion: pkg config cannot import pkg config/validator
// without an import cycle (validator needs the config types), so the
// validator package installs its implementation here via SetValidator on
// import. When no validator is installed — e.g. package-internal unit tests
// that build isolated configs without the runtime — validation is lenient.
//
// runtimeBuilder is the "build the runtime candidate before persisting"
// boundary (installed by the gateway server): a candidate config that the
// runtime cannot compile never reaches disk or memory.
var (
	validationMu   sync.RWMutex
	configValidate func(*Config) error
	runtimeBuild   func(*Config) error
)

// SetValidator installs the centralized configuration validator used by all
// internal commit points. Called once by the validator package on import.
func SetValidator(fn func(*Config) error) {
	validationMu.Lock()
	defer validationMu.Unlock()
	configValidate = fn
}

// SetRuntimeBuilder installs the runtime-build gate: a pure function that
// compiles a runtime candidate from cfg (without publishing it). Called by
// the gateway once when wiring the server.
func SetRuntimeBuilder(fn func(*Config) error) {
	validationMu.Lock()
	defer validationMu.Unlock()
	runtimeBuild = fn
}

// validateAndBuildLocked runs the validator and the runtime-build gate for a
// candidate config. Caller must ALREADY HOLD m.mu so commits observe a
// consistent snapshot: no partial validation state can be interleaved with
// an admin update running concurrently.
func (m *Manager) validateAndBuildLocked(cfg *Config) error {
	validationMu.RLock()
	validateFn, buildFn := configValidate, runtimeBuild
	validationMu.RUnlock()

	if validateFn != nil {
		if err := validateFn(cfg); err != nil {
			return fmt.Errorf("config validation failed: %w", err)
		}
	}
	if buildFn != nil {
		if err := buildFn(cfg); err != nil {
			return fmt.Errorf("runtime build failed: %w", err)
		}
	}
	return nil
}
