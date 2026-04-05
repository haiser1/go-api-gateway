package config

import (
	"fmt"

	"github.com/rs/zerolog/log"
)

// --- Helper internal (locked) ---

func (m *Manager) serviceExistsLocked(serviceId string) bool {
	for _, s := range m.config.Services {
		if s.Id == serviceId {
			return true
		}
	}
	return false
}

func (m *Manager) serviceNameExistsLocked(name string, excludeId string) bool {
	for _, s := range m.config.Services {
		if s.Name == name && s.Id != excludeId {
			return true
		}
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
	s.ApplyDefaults()
	m.config.Services = append(m.config.Services, s)

	err := m.saveAndReloadLocked()
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
	found := false
	for i := range m.config.Services {
		if m.config.Services[i].Id == serviceId {
			updatedService.ApplyDefaults()
			m.config.Services[i] = updatedService
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("service dengan ID '%s' tidak ditemukan", serviceId)
	}
	err := m.saveAndReloadLocked()
	if err == nil {
		log.Info().Str("service_id", serviceId).Str("service_name", updatedService.Name).Msg("Service updated")
	}
	return err
}

func (m *Manager) DeleteService(serviceId string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, r := range m.config.Routes {
		if r.ServiceId == serviceId {
			return fmt.Errorf("service tidak dapat dihapus, masih digunakan oleh route '%s' (ID: %s)", r.Name, r.Id)
		}
	}
	found := false
	newServices := make([]Service, 0, len(m.config.Services))
	for _, s := range m.config.Services {
		if s.Id == serviceId {
			found = true
			continue
		}
		newServices = append(newServices, s)
	}
	if !found {
		return fmt.Errorf("service dengan ID '%s' tidak ditemukan", serviceId)
	}
	m.config.Services = newServices
	err := m.saveAndReloadLocked()
	if err == nil {
		log.Info().Str("service_id", serviceId).Msg("Service deleted")
	}
	return err
}
