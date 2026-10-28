package config

import (
	"fmt"

	"github.com/rs/zerolog/log"
)

func (m *Manager) routeNameExistsLocked(name string, excludeId string) bool {
	if m.routeNameIdx == nil {
		m.rebuildIndexesLocked()
	}
	if idx, ok := m.routeNameIdx[name]; ok {
		return m.config.Routes[idx].Id != excludeId
	}
	return false
}

// routePathConflictsLocked checks if any method+path combination in the new route
// already exists in another route. Returns an error describing the conflict, or nil.
// excludeId allows skipping the route being updated.
func (m *Manager) routePathConflictsLocked(methods, paths []string, excludeId string) error {
	for i := range m.config.Routes {
		existing := &m.config.Routes[i]
		if existing.Id == excludeId {
			continue
		}
		for _, existingPath := range existing.Paths {
			for _, newPath := range paths {
				if existingPath != newPath {
					continue
				}
				// Same path found — check for method overlap
				for _, existingMethod := range existing.Methods {
					for _, newMethod := range methods {
						if existingMethod == newMethod {
							return fmt.Errorf(
								"route conflict: %s %s sudah digunakan oleh route '%s' (id: %s)",
								newMethod, newPath, existing.Name, existing.Id,
							)
						}
					}
				}
			}
		}
	}
	return nil
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
	if err := m.routePathConflictsLocked(r.Methods, r.Paths, ""); err != nil {
		return err
	}

	// Copy-on-Write: deep copy config first
	newCfg := m.config.DeepCopy()
	newCfg.Routes = append(newCfg.Routes, r)

	err := m.commitConfigLocked(newCfg)
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
	if err := m.routePathConflictsLocked(updatedRoute.Methods, updatedRoute.Paths, routeId); err != nil {
		return err
	}

	// Copy-on-Write: deep copy config first
	newCfg := m.config.DeepCopy()
	found := false
	for i := range newCfg.Routes {
		if newCfg.Routes[i].Id == routeId {
			newCfg.Routes[i] = updatedRoute
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("route dengan ID '%s' tidak ditemukan", routeId)
	}

	err := m.commitConfigLocked(newCfg)
	if err == nil {
		log.Info().Str("route_id", routeId).Str("route_name", updatedRoute.Name).Msg("Route updated")
	}
	return err
}

func (m *Manager) DeleteRoute(routeId string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Copy-on-Write: deep copy config first
	newCfg := m.config.DeepCopy()
	found := false
	newRoutes := make([]Route, 0, len(newCfg.Routes))
	for i := range newCfg.Routes {
		if newCfg.Routes[i].Id == routeId {
			found = true
			continue
		}
		newRoutes = append(newRoutes, newCfg.Routes[i])
	}
	if !found {
		return fmt.Errorf("route dengan ID '%s' tidak ditemukan", routeId)
	}
	newCfg.Routes = newRoutes

	err := m.commitConfigLocked(newCfg)
	if err == nil {
		log.Info().Str("route_id", routeId).Msg("Route deleted")
	}
	return err
}
