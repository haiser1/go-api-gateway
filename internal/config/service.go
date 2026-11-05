package config

import (
	"fmt"

	"github.com/rs/zerolog/log"
)

// --- Helper internal (locked) ---

func (m *Manager) serviceExistsLocked(serviceId string) bool {
	if m.serviceIdx == nil {
		m.rebuildIndexesLocked()
	}
	_, ok := m.serviceIdx[serviceId]
	return ok
}

func (m *Manager) serviceNameExistsLocked(name string, excludeId string) bool {
	if m.serviceNameIdx == nil {
		m.rebuildIndexesLocked()
	}
	if idx, ok := m.serviceNameIdx[name]; ok {
		return m.config.Services[idx].Id != excludeId
	}
	return false
}

// ========================================================
// PENGELOLAAN SERVICE (CRUD)
// ========================================================

func (m *Manager) AddService(s Service) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.serviceNameExistsLocked(s.Name, "") {
		return fmt.Errorf("service dengan nama '%s' sudah ada", s.Name)
	}
	if s.UpstreamId != "" && !m.upstreamExistsLocked(s.UpstreamId) {
		return fmt.Errorf("upstream dengan ID '%s' tidak ditemukan", s.UpstreamId)
	}

	// Copy-on-Write: deep copy config first
	newCfg := m.config.DeepCopy()
	s.ApplyDefaults()
	newCfg.Services = append(newCfg.Services, s)

	// Provision .env secrets when the service embeds the auth plugin
	m.ensureAuthEnvForPluginsLocked(s.Plugins)

	err := m.commitConfigLocked(newCfg)
	if err == nil {
		log.Info().Str("service_id", s.Id).Str("service_name", s.Name).Msg("Service added")
	}
	return err
}

func (m *Manager) UpdateService(serviceId string, updatedService Service) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.serviceNameExistsLocked(updatedService.Name, serviceId) {
		return fmt.Errorf("nama service '%s' sudah digunakan", updatedService.Name)
	}
	if updatedService.UpstreamId != "" && !m.upstreamExistsLocked(updatedService.UpstreamId) {
		return fmt.Errorf("upstream dengan ID '%s' tidak ditemukan", updatedService.UpstreamId)
	}

	// Copy-on-Write: deep copy config first
	newCfg := m.config.DeepCopy()
	found := false
	for i := range newCfg.Services {
		if newCfg.Services[i].Id == serviceId {
			updatedService.ApplyDefaults()
			newCfg.Services[i] = updatedService
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("service dengan ID '%s' tidak ditemukan", serviceId)
	}

	err := m.commitConfigLocked(newCfg)
	if err == nil {
		log.Info().Str("service_id", serviceId).Str("service_name", updatedService.Name).Msg("Service updated")
	}
	return err
}

func (m *Manager) DeleteService(serviceId string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if service is referenced by any route (index-based to avoid Route copy)
	for i := range m.config.Routes {
		if m.config.Routes[i].ServiceId == serviceId {
			return fmt.Errorf("service tidak dapat dihapus, masih digunakan oleh route '%s' (ID: %s)", m.config.Routes[i].Name, m.config.Routes[i].Id)
		}
	}

	// Copy-on-Write: deep copy config first
	newCfg := m.config.DeepCopy()
	found := false
	newServices := make([]Service, 0, len(newCfg.Services))
	for i := range newCfg.Services {
		if newCfg.Services[i].Id == serviceId {
			found = true
			continue
		}
		newServices = append(newServices, newCfg.Services[i])
	}
	if !found {
		return fmt.Errorf("service dengan ID '%s' tidak ditemukan", serviceId)
	}
	newCfg.Services = newServices

	err := m.commitConfigLocked(newCfg)
	if err == nil {
		log.Info().Str("service_id", serviceId).Msg("Service deleted")
	}
	return err
}
