package config

import "errors"

// GetAuthConfig returns an independent copy of the top-level auth policy.
func (m *Manager) GetAuthConfig() *AuthConfig {
	cfg := m.GetConfig()
	if cfg == nil {
		return nil
	}
	return cfg.Auth.DeepCopy()
}

// UpdateAuthConfig replaces the top-level auth policy. The candidate is
// validated and compiled by the runtime-build gate before it is persisted or
// published, so a bad credential reference never reaches disk.
func (m *Manager) UpdateAuthConfig(authCfg *AuthConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.config == nil {
		return errors.New("manager has no configuration")
	}

	newCfg := m.config.DeepCopy()
	newCfg.Auth = authCfg.DeepCopy()
	return m.commitConfigLocked(newCfg)
}
