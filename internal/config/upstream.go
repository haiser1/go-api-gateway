package config

import (
	"fmt"

	"github.com/rs/zerolog/log"
)

// --- Helper internal (locked) ---

func (m *Manager) upstreamExistsLocked(upstreamId string) bool {
	for i := range m.config.Upstreams {
		if m.config.Upstreams[i].Id == upstreamId {
			return true
		}
	}
	return false
}

func (m *Manager) upstreamNameExistsLocked(name string, excludeId string) bool {
	for i := range m.config.Upstreams {
		if m.config.Upstreams[i].Name == name && m.config.Upstreams[i].Id != excludeId {
			return true
		}
	}
	return false
}

// ========================================================
// PENGELOLAAN UPSTREAM (CRUD)
// ========================================================

func (m *Manager) AddUpstream(u Upstream) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.upstreamNameExistsLocked(u.Name, "") {
		return fmt.Errorf("upstream dengan nama '%s' sudah ada", u.Name)
	}

	// Copy-on-Write: deep copy config first
	newCfg := m.config.DeepCopy()
	u.ApplyDefaults()
	newCfg.Upstreams = append(newCfg.Upstreams, u)

	err := m.commitConfigLocked(newCfg)
	if err == nil {
		log.Info().Str("upstream_id", u.Id).Str("upstream_name", u.Name).Msg("Upstream added")
	}
	return err
}

func (m *Manager) UpdateUpstream(upstreamId string, updatedUpstream Upstream) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.upstreamNameExistsLocked(updatedUpstream.Name, upstreamId) {
		return fmt.Errorf("nama upstream '%s' sudah digunakan", updatedUpstream.Name)
	}

	// Copy-on-Write: deep copy config first
	newCfg := m.config.DeepCopy()
	found := false
	for i := range newCfg.Upstreams {
		if newCfg.Upstreams[i].Id == upstreamId {
			updatedUpstream.ApplyDefaults()
			newCfg.Upstreams[i] = updatedUpstream
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("upstream dengan ID '%s' tidak ditemukan", upstreamId)
	}

	err := m.commitConfigLocked(newCfg)
	if err == nil {
		log.Info().Str("upstream_id", upstreamId).Str("upstream_name", updatedUpstream.Name).Msg("Upstream updated")
	}
	return err
}

func (m *Manager) DeleteUpstream(upstreamId string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if upstream is referenced by any service (avoid copy of Service struct)
	for i := range m.config.Services {
		if m.config.Services[i].UpstreamId == upstreamId {
			return fmt.Errorf("upstream tidak dapat dihapus, masih digunakan oleh service '%s' (ID: %s)", m.config.Services[i].Name, m.config.Services[i].Id)
		}
	}

	// Copy-on-Write: deep copy config first
	newCfg := m.config.DeepCopy()
	found := false
	newUpstreams := make([]Upstream, 0, len(newCfg.Upstreams))
	for i := range newCfg.Upstreams {
		if newCfg.Upstreams[i].Id == upstreamId {
			found = true
			continue
		}
		newUpstreams = append(newUpstreams, newCfg.Upstreams[i])
	}
	if !found {
		return fmt.Errorf("upstream dengan ID '%s' tidak ditemukan", upstreamId)
	}
	newCfg.Upstreams = newUpstreams

	err := m.commitConfigLocked(newCfg)
	if err == nil {
		log.Info().Str("upstream_id", upstreamId).Msg("Upstream deleted")
	}
	return err
}
