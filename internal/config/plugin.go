package config

import (
	"fmt"

	"github.com/rs/zerolog/log"
)

// ========================================================
// PENGELOLAAN PLUGIN (CRUD)
// ========================================================

// --- Plugin Global ---

func (m *Manager) AddGlobalPlugin(p PluginConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, existing := range m.config.GlobalPlugins {
		if existing.Name == p.Name {
			return fmt.Errorf("global plugin '%s' sudah ada", p.Name)
		}
	}
	m.config.GlobalPlugins = append(m.config.GlobalPlugins, p)
	err := m.saveAndReloadLocked()
	if err == nil {
		log.Info().Str("plugin", p.Name).Msg("Global plugin added")
	}
	return err
}

func (m *Manager) UpdateGlobalPlugin(pluginName string, p PluginConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	found := false
	for i := range m.config.GlobalPlugins {
		if m.config.GlobalPlugins[i].Name == pluginName {
			m.config.GlobalPlugins[i] = p
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("global plugin '%s' tidak ditemukan", pluginName)
	}
	err := m.saveAndReloadLocked()
	if err == nil {
		log.Info().Str("plugin", pluginName).Msg("Global plugin updated")
	}
	return err
}

func (m *Manager) DeleteGlobalPlugin(pluginName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	found := false
	newPlugins := make([]PluginConfig, 0, len(m.config.GlobalPlugins))
	for _, p := range m.config.GlobalPlugins {
		if p.Name == pluginName {
			found = true
			continue
		}
		newPlugins = append(newPlugins, p)
	}
	if !found {
		return fmt.Errorf("global plugin '%s' tidak ditemukan", pluginName)
	}
	m.config.GlobalPlugins = newPlugins
	err := m.saveAndReloadLocked()
	if err == nil {
		log.Info().Str("plugin", pluginName).Msg("Global plugin deleted")
	}
	return err
}

// --- Plugin di Service ---

func (m *Manager) AddPluginToService(serviceId string, p PluginConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	serviceFound := false
	for i := range m.config.Services {
		if m.config.Services[i].Id == serviceId {
			serviceFound = true
			for _, existingPlugin := range m.config.Services[i].Plugins {
				if existingPlugin.Name == p.Name {
					return fmt.Errorf("plugin '%s' sudah ada di service '%s'", p.Name, m.config.Services[i].Name)
				}
			}
			m.config.Services[i].Plugins = append(m.config.Services[i].Plugins, p)
			break
		}
	}
	if !serviceFound {
		return fmt.Errorf("service dengan ID '%s' tidak ditemukan", serviceId)
	}
	err := m.saveAndReloadLocked()
	if err == nil {
		log.Info().Str("service_id", serviceId).Str("plugin", p.Name).Msg("Service plugin added")
	}
	return err
}

func (m *Manager) UpdatePluginInService(serviceId string, pluginName string, newPluginConfig PluginConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	serviceFound := false
	pluginFound := false
	for i := range m.config.Services {
		if m.config.Services[i].Id == serviceId {
			serviceFound = true
			for j := range m.config.Services[i].Plugins {
				if m.config.Services[i].Plugins[j].Name == pluginName {
					m.config.Services[i].Plugins[j] = newPluginConfig
					pluginFound = true
					break
				}
			}
			break
		}
	}
	if !serviceFound {
		return fmt.Errorf("service dengan ID '%s' tidak ditemukan", serviceId)
	}
	if !pluginFound {
		return fmt.Errorf("plugin '%s' tidak ditemukan di service", pluginName)
	}
	err := m.saveAndReloadLocked()
	if err == nil {
		log.Info().Str("service_id", serviceId).Str("plugin", pluginName).Msg("Service plugin updated")
	}
	return err
}

func (m *Manager) DeletePluginFromService(serviceId string, pluginName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	serviceFound := false
	pluginFound := false
	for i := range m.config.Services {
		if m.config.Services[i].Id == serviceId {
			serviceFound = true
			newPlugins := make([]PluginConfig, 0, len(m.config.Services[i].Plugins))
			for _, p := range m.config.Services[i].Plugins {
				if p.Name == pluginName {
					pluginFound = true
					continue
				}
				newPlugins = append(newPlugins, p)
			}
			if pluginFound {
				m.config.Services[i].Plugins = newPlugins
			}
			break
		}
	}
	if !serviceFound {
		return fmt.Errorf("service dengan ID '%s' tidak ditemukan", serviceId)
	}
	if !pluginFound {
		return fmt.Errorf("plugin '%s' tidak ditemukan di service", pluginName)
	}
	err := m.saveAndReloadLocked()
	if err == nil {
		log.Info().Str("service_id", serviceId).Str("plugin", pluginName).Msg("Service plugin deleted")
	}
	return err
}

// --- Plugin di Route ---

func (m *Manager) AddPluginToRoute(routeId string, p PluginConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	routeFound := false
	for i := range m.config.Routes {
		if m.config.Routes[i].Id == routeId {
			routeFound = true
			for _, existingPlugin := range m.config.Routes[i].Plugins {
				if existingPlugin.Name == p.Name {
					return fmt.Errorf("plugin '%s' sudah ada di route '%s'", p.Name, m.config.Routes[i].Name)
				}
			}
			m.config.Routes[i].Plugins = append(m.config.Routes[i].Plugins, p)
			break
		}
	}
	if !routeFound {
		return fmt.Errorf("route dengan ID '%s' tidak ditemukan", routeId)
	}
	err := m.saveAndReloadLocked()
	if err == nil {
		log.Info().Str("route_id", routeId).Str("plugin", p.Name).Msg("Route plugin added")
	}
	return err
}

func (m *Manager) UpdatePluginInRoute(routeId string, pluginName string, newPluginConfig PluginConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	routeFound := false
	pluginFound := false
	for i := range m.config.Routes {
		if m.config.Routes[i].Id == routeId {
			routeFound = true
			for j := range m.config.Routes[i].Plugins {
				if m.config.Routes[i].Plugins[j].Name == pluginName {
					m.config.Routes[i].Plugins[j] = newPluginConfig
					pluginFound = true
					break
				}
			}
			break
		}
	}
	if !routeFound {
		return fmt.Errorf("route dengan ID '%s' tidak ditemukan", routeId)
	}
	if !pluginFound {
		return fmt.Errorf("plugin '%s' tidak ditemukan di route", pluginName)
	}
	err := m.saveAndReloadLocked()
	if err == nil {
		log.Info().Str("route_id", routeId).Str("plugin", pluginName).Msg("Route plugin updated")
	}
	return err
}

func (m *Manager) DeletePluginFromRoute(routeId string, pluginName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	routeFound := false
	pluginFound := false
	for i := range m.config.Routes {
		if m.config.Routes[i].Id == routeId {
			routeFound = true
			newPlugins := make([]PluginConfig, 0, len(m.config.Routes[i].Plugins))
			for _, p := range m.config.Routes[i].Plugins {
				if p.Name == pluginName {
					pluginFound = true
					continue
				}
				newPlugins = append(newPlugins, p)
			}
			if pluginFound {
				m.config.Routes[i].Plugins = newPlugins
			}
			break
		}
	}
	if !routeFound {
		return fmt.Errorf("route dengan ID '%s' tidak ditemukan", routeId)
	}
	if !pluginFound {
		return fmt.Errorf("plugin '%s' tidak ditemukan di route", pluginName)
	}
	err := m.saveAndReloadLocked()
	if err == nil {
		log.Info().Str("route_id", routeId).Str("plugin", pluginName).Msg("Route plugin deleted")
	}
	return err
}
