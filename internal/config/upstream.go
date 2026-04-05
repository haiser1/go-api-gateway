package config

import (
	"fmt"

	"github.com/rs/zerolog/log"
)

// --- Helper internal (locked) ---

func (m *Manager) upstreamExistsLocked(upstreamId string) bool {
	for _, u := range m.config.Upstreams {
		if u.Id == upstreamId {
			return true
		}
	}
	return false
}

func (m *Manager) upstreamNameExistsLocked(name string, excludeId string) bool {
	for _, u := range m.config.Upstreams {
		if u.Name == name && u.Id != excludeId {
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
	u.ApplyDefaults()
	m.config.Upstreams = append(m.config.Upstreams, u)

	err := m.saveAndReloadLocked()
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
	found := false
	for i := range m.config.Upstreams {
		if m.config.Upstreams[i].Id == upstreamId {
			updatedUpstream.ApplyDefaults()
			m.config.Upstreams[i] = updatedUpstream
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("upstream dengan ID '%s' tidak ditemukan", upstreamId)
	}
	err := m.saveAndReloadLocked()
	if err == nil {
		log.Info().Str("upstream_id", upstreamId).Str("upstream_name", updatedUpstream.Name).Msg("Upstream updated")
	}
	return err
}

func (m *Manager) DeleteUpstream(upstreamId string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if upstream is referenced by any service
	for _, s := range m.config.Services {
		if s.UpstreamId == upstreamId {
			return fmt.Errorf("upstream tidak dapat dihapus, masih digunakan oleh service '%s' (ID: %s)", s.Name, s.Id)
		}
	}
	found := false
	newUpstreams := make([]Upstream, 0, len(m.config.Upstreams))
	for _, u := range m.config.Upstreams {
		if u.Id == upstreamId {
			found = true
			continue
		}
		newUpstreams = append(newUpstreams, u)
	}
	if !found {
		return fmt.Errorf("upstream dengan ID '%s' tidak ditemukan", upstreamId)
	}
	m.config.Upstreams = newUpstreams
	err := m.saveAndReloadLocked()
	if err == nil {
		log.Info().Str("upstream_id", upstreamId).Msg("Upstream deleted")
	}
	return err
}
