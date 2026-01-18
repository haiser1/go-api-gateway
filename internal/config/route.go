package config

import (
	"fmt"

	"github.com/rs/zerolog/log"
)

func (m *Manager) routeNameExistsLocked(name string, excludeId string) bool {
	for _, r := range m.config.Routes {
		if r.Name == name && r.Id != excludeId {
			return true
		}
	}
	return false
}

// ========================================================
// PENGELOLAAN ROUTE (CRUD)
// ========================================================

func (m *Manager) AddRoute(r Route) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.routeNameExistsLocked(r.Name, "") {
		return fmt.Errorf("route dengan nama '%s' sudah ada", r.Name)
	}
	if !m.serviceExistsLocked(r.ServiceId) {
		return fmt.Errorf("service dengan ID '%s' tidak ditemukan", r.ServiceId)
	}
	m.config.Routes = append(m.config.Routes, r)
	err := m.saveAndReloadLocked()
	if err == nil {
		log.Info().Str("route_id", r.Id).Str("route_name", r.Name).Msg("Route added")
	}
	return err
}

func (m *Manager) UpdateRoute(routeId string, updatedRoute Route) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.routeNameExistsLocked(updatedRoute.Name, routeId) {
		return fmt.Errorf("nama route '%s' sudah digunakan", updatedRoute.Name)
	}
	if !m.serviceExistsLocked(updatedRoute.ServiceId) {
		return fmt.Errorf("service dengan ID '%s' tidak ditemukan", updatedRoute.ServiceId)
	}
	found := false
	for i := range m.config.Routes {
		if m.config.Routes[i].Id == routeId {
			m.config.Routes[i] = updatedRoute
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("route dengan ID '%s' tidak ditemukan", routeId)
	}
	err := m.saveAndReloadLocked()
	if err == nil {
		log.Info().Str("route_id", routeId).Str("route_name", updatedRoute.Name).Msg("Route updated")
	}
	return err
}

func (m *Manager) DeleteRoute(routeId string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	found := false
	newRoutes := make([]Route, 0, len(m.config.Routes))
	for _, r := range m.config.Routes {
		if r.Id == routeId {
			found = true
			continue
		}
		newRoutes = append(newRoutes, r)
	}
	if !found {
		return fmt.Errorf("route dengan ID '%s' tidak ditemukan", routeId)
	}
	m.config.Routes = newRoutes
	err := m.saveAndReloadLocked()
	if err == nil {
		log.Info().Str("route_id", routeId).Msg("Route deleted")
	}
	return err
}
