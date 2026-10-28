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

	if m.globalPluginIdx == nil {
		m.rebuildIndexesLocked()
	}
	if _, ok := m.globalPluginIdx[p.Name]; ok {
		return fmt.Errorf("global plugin '%s' sudah ada", p.Name)
	}

	newCfg := m.config.DeepCopy()
	newCfg.GlobalPlugins = append(newCfg.GlobalPlugins, p)

	err := m.commitConfigLocked(newCfg)
	if err == nil {
		log.Info().Str("plugin", p.Name).Msg("Global plugin added")
	}
	return err
}

func (m *Manager) UpdateGlobalPlugin(pluginName string, p PluginConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	newCfg := m.config.DeepCopy()
	found := false
	for i := range newCfg.GlobalPlugins {
		if newCfg.GlobalPlugins[i].Name == pluginName {
			newCfg.GlobalPlugins[i] = p
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("global plugin '%s' tidak ditemukan", pluginName)
	}

	err := m.commitConfigLocked(newCfg)
	if err == nil {
		log.Info().Str("plugin", pluginName).Msg("Global plugin updated")
	}
	return err
}

func (m *Manager) DeleteGlobalPlugin(pluginName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	newCfg := m.config.DeepCopy()
	found := false
	newPlugins := make([]PluginConfig, 0, len(newCfg.GlobalPlugins))
	for i := range newCfg.GlobalPlugins {
		if newCfg.GlobalPlugins[i].Name == pluginName {
			found = true
			continue
		}
		newPlugins = append(newPlugins, newCfg.GlobalPlugins[i])
	}
	if !found {
		return fmt.Errorf("global plugin '%s' tidak ditemukan", pluginName)
	}
	newCfg.GlobalPlugins = newPlugins

	err := m.commitConfigLocked(newCfg)
	if err == nil {
		log.Info().Str("plugin", pluginName).Msg("Global plugin deleted")
	}
	return err
}

// --- Plugin di Service ---

func (m *Manager) AddPluginToService(serviceId string, p PluginConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	newCfg := m.config.DeepCopy()
	serviceFound := false
	for i := range newCfg.Services {
		if newCfg.Services[i].Id == serviceId {
			serviceFound = true
			for j := range newCfg.Services[i].Plugins {
				if newCfg.Services[i].Plugins[j].Name == p.Name {
					return fmt.Errorf("plugin '%s' sudah ada di service '%s'", p.Name, newCfg.Services[i].Name)
				}
			}
			newCfg.Services[i].Plugins = append(newCfg.Services[i].Plugins, p)
			break
		}
	}
	if !serviceFound {
		return fmt.Errorf("service dengan ID '%s' tidak ditemukan", serviceId)
	}

	err := m.commitConfigLocked(newCfg)
	if err == nil {
		log.Info().Str("service_id", serviceId).Str("plugin", p.Name).Msg("Service plugin added")
	}
	return err
}

func (m *Manager) UpdatePluginInService(serviceId string, pluginName string, newPluginConfig PluginConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	newCfg := m.config.DeepCopy()
	serviceFound := false
	pluginFound := false
	for i := range newCfg.Services {
		if newCfg.Services[i].Id == serviceId {
			serviceFound = true
			for j := range newCfg.Services[i].Plugins {
				if newCfg.Services[i].Plugins[j].Name == pluginName {
					newCfg.Services[i].Plugins[j] = newPluginConfig
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

	err := m.commitConfigLocked(newCfg)
	if err == nil {
		log.Info().Str("service_id", serviceId).Str("plugin", pluginName).Msg("Service plugin updated")
	}
	return err
}

func (m *Manager) DeletePluginFromService(serviceId string, pluginName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	newCfg := m.config.DeepCopy()
	serviceFound := false
	pluginFound := false
	for i := range newCfg.Services {
		if newCfg.Services[i].Id == serviceId {
			serviceFound = true
			newPlugins := make([]PluginConfig, 0, len(newCfg.Services[i].Plugins))
			for j := range newCfg.Services[i].Plugins {
				if newCfg.Services[i].Plugins[j].Name == pluginName {
					pluginFound = true
					continue
				}
				newPlugins = append(newPlugins, newCfg.Services[i].Plugins[j])
			}
			if pluginFound {
				newCfg.Services[i].Plugins = newPlugins
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

	err := m.commitConfigLocked(newCfg)
	if err == nil {
		log.Info().Str("service_id", serviceId).Str("plugin", pluginName).Msg("Service plugin deleted")
	}
	return err
}

// --- Plugin di Route ---

func (m *Manager) AddPluginToRoute(routeId string, p PluginConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	newCfg := m.config.DeepCopy()
	routeFound := false
	for i := range newCfg.Routes {
		if newCfg.Routes[i].Id == routeId {
			routeFound = true
			for j := range newCfg.Routes[i].Plugins {
				if newCfg.Routes[i].Plugins[j].Name == p.Name {
					return fmt.Errorf("plugin '%s' sudah ada di route '%s'", p.Name, newCfg.Routes[i].Name)
				}
			}
			newCfg.Routes[i].Plugins = append(newCfg.Routes[i].Plugins, p)
			break
		}
	}
	if !routeFound {
		return fmt.Errorf("route dengan ID '%s' tidak ditemukan", routeId)
	}

	err := m.commitConfigLocked(newCfg)
	if err == nil {
		log.Info().Str("route_id", routeId).Str("plugin", p.Name).Msg("Route plugin added")
	}
	return err
}

func (m *Manager) UpdatePluginInRoute(routeId string, pluginName string, newPluginConfig PluginConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	newCfg := m.config.DeepCopy()
	routeFound := false
	pluginFound := false
	for i := range newCfg.Routes {
		if newCfg.Routes[i].Id == routeId {
			routeFound = true
			for j := range newCfg.Routes[i].Plugins {
				if newCfg.Routes[i].Plugins[j].Name == pluginName {
					newCfg.Routes[i].Plugins[j] = newPluginConfig
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

	err := m.commitConfigLocked(newCfg)
	if err == nil {
		log.Info().Str("route_id", routeId).Str("plugin", pluginName).Msg("Route plugin updated")
	}
	return err
}

func (m *Manager) DeletePluginFromRoute(routeId string, pluginName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	newCfg := m.config.DeepCopy()
	routeFound := false
	pluginFound := false
	for i := range newCfg.Routes {
		if newCfg.Routes[i].Id == routeId {
			routeFound = true
			newPlugins := make([]PluginConfig, 0, len(newCfg.Routes[i].Plugins))
			for j := range newCfg.Routes[i].Plugins {
				if newCfg.Routes[i].Plugins[j].Name == pluginName {
					pluginFound = true
					continue
				}
				newPlugins = append(newPlugins, newCfg.Routes[i].Plugins[j])
			}
			if pluginFound {
				newCfg.Routes[i].Plugins = newPlugins
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

	err := m.commitConfigLocked(newCfg)
	if err == nil {
		log.Info().Str("route_id", routeId).Str("plugin", pluginName).Msg("Route plugin deleted")
	}
	return err
}
