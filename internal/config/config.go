package config

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

type PluginConfig struct {
	Name   string                 `mapstructure:"name" json:"name"`
	Config map[string]interface{} `mapstructure:"config" json:"config"`
}

type Service struct {
	Id       string         `mapstructure:"id" json:"id"`
	Name     string         `mapstructure:"name" json:"name"`
	Host     string         `mapstructure:"host" json:"host"`
	Port     int            `mapstructure:"port" json:"port"`
	Protocol string         `mapstructure:"protocol" json:"protocol"` // Menggunakan 'Protocols' (plural)
	Plugins  []PluginConfig `mapstructure:"plugins,omitempty" json:"plugins,omitempty"`
}

type Route struct {
	Id        string         `mapstructure:"id" json:"id"`
	Name      string         `mapstructure:"name" json:"name"`
	Methods   []string       `mapstructure:"methods" json:"methods"`
	Paths     []string       `mapstructure:"paths" json:"paths"`
	ServiceId string         `mapstructure:"serviceId" json:"serviceId"`
	Plugins   []PluginConfig `mapstructure:"plugins,omitempty" json:"plugins,omitempty"`
}

type Config struct {
	GlobalPlugins []PluginConfig `mapstructure:"global_plugins" json:"global_plugins"` // DITAMBAHKAN
	Services      []Service      `mapstructure:"services" json:"services"`
	Routes        []Route        `mapstructure:"routes" json:"routes"`
}

// Manager is a struct that holds the configuration and reload channel.
type Manager struct {
	mu     sync.RWMutex
	config *Config
	v      *viper.Viper
	Reload chan struct{}
}

// NewManager creates a new Manager instance.
func NewManager(configPath string) (*Manager, error) {
	v := viper.New()
	v.AddConfigPath(configPath)
	v.SetConfigName("config")
	v.SetConfigType("yaml")

	if err := v.ReadInConfig(); err != nil {
		log.Println("Tidak dapat membaca file config:", err)
		return nil, fmt.Errorf("gagal membaca file config: %w", err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("gagal unmarshal config: %w", err)
	}

	m := &Manager{
		config: &cfg,
		v:      v,
		Reload: make(chan struct{}, 1),
	}

	go m.watchConfig()
	return m, nil
}

// watchConfig is a goroutine that watches for changes to the configuration.
func (m *Manager) watchConfig() {
	m.v.WatchConfig()
	m.v.OnConfigChange(func(e fsnotify.Event) {
		log.Println("Config file changed:", e.Name)

		m.mu.Lock()
		defer m.mu.Unlock()

		var cfg Config
		if err := m.v.Unmarshal(&cfg); err == nil {
			m.config = &cfg
			select {
			case m.Reload <- struct{}{}:
			default:
			}
		} else {
			log.Printf("Failed to unmarshal config: %v", err)
		}
	})
}

// GetConfig mengembalikan pointer ke config yang saat ini (thread-safe).
func (m *Manager) GetConfig() *Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}

// WriteConfigSafe is a thread-safe version of WriteConfig that first backs up
// the existing configuration file.
func (m *Manager) WriteConfigSafe() error {
	file := m.v.ConfigFileUsed()

	if _, err := os.Stat(file); err == nil {
		data, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("gagal membaca config untuk backup: %w", err)
		}
		backup := fmt.Sprintf("%s.bak-%d", file, time.Now().Unix())
		_ = os.WriteFile(backup, data, 0644)
	}

	if err := m.v.WriteConfig(); err != nil {
		return fmt.Errorf("gagal menulis config ke file: %w", err)
	}
	return nil
}

// saveAndReload is a thread-safe version of WriteConfig.
func (m *Manager) saveAndReload() error {
	m.mu.RLock()
	globalPlugins := make([]PluginConfig, len(m.config.GlobalPlugins)) // DITAMBAHKAN
	copy(globalPlugins, m.config.GlobalPlugins)                        // DITAMBAHKAN
	services := make([]Service, len(m.config.Services))
	copy(services, m.config.Services)
	routes := make([]Route, len(m.config.Routes))
	copy(routes, m.config.Routes)
	m.mu.RUnlock()

	m.v.Set("global_plugins", globalPlugins) // DITAMBAHKAN
	m.v.Set("services", services)
	m.v.Set("routes", routes)

	if err := m.WriteConfigSafe(); err != nil {
		return err
	}

	select {
	case m.Reload <- struct{}{}:
	default:
	}
	return nil
}

// --- Helper internal (thread-safe) ---

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

func (m *Manager) routeNameExistsLocked(name string, excludeId string) bool {
	for _, r := range m.config.Routes {
		if r.Name == name && r.Id != excludeId {
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
	if m.serviceNameExistsLocked(s.Name, "") {
		m.mu.Unlock()
		return fmt.Errorf("service dengan nama '%s' sudah ada", s.Name)
	}
	m.config.Services = append(m.config.Services, s)
	m.mu.Unlock()
	return m.saveAndReload()
}

func (m *Manager) UpdateService(serviceId string, updatedService Service) error {
	m.mu.Lock()
	if m.serviceNameExistsLocked(updatedService.Name, serviceId) {
		m.mu.Unlock()
		return fmt.Errorf("nama service '%s' sudah digunakan", updatedService.Name)
	}
	found := false
	for i := range m.config.Services {
		if m.config.Services[i].Id == serviceId {
			m.config.Services[i] = updatedService
			found = true
			break
		}
	}
	m.mu.Unlock()
	if !found {
		return fmt.Errorf("service dengan ID '%s' tidak ditemukan", serviceId)
	}
	return m.saveAndReload()
}

func (m *Manager) DeleteService(serviceId string) error {
	m.mu.Lock()
	for _, r := range m.config.Routes {
		if r.ServiceId == serviceId {
			m.mu.Unlock()
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
		m.mu.Unlock()
		return fmt.Errorf("service dengan ID '%s' tidak ditemukan", serviceId)
	}
	m.config.Services = newServices
	m.mu.Unlock()
	return m.saveAndReload()
}

// ========================================================
// PENGELOLAAN ROUTE (CRUD)
// ========================================================

func (m *Manager) AddRoute(r Route) error {
	m.mu.Lock()
	if m.routeNameExistsLocked(r.Name, "") {
		m.mu.Unlock()
		return fmt.Errorf("route dengan nama '%s' sudah ada", r.Name)
	}
	if !m.serviceExistsLocked(r.ServiceId) {
		m.mu.Unlock()
		return fmt.Errorf("service dengan ID '%s' tidak ditemukan", r.ServiceId)
	}
	m.config.Routes = append(m.config.Routes, r)
	m.mu.Unlock()
	return m.saveAndReload()
}

func (m *Manager) UpdateRoute(routeId string, updatedRoute Route) error {
	m.mu.Lock()
	if m.routeNameExistsLocked(updatedRoute.Name, routeId) {
		m.mu.Unlock()
		return fmt.Errorf("nama route '%s' sudah digunakan", updatedRoute.Name)
	}
	if !m.serviceExistsLocked(updatedRoute.ServiceId) {
		m.mu.Unlock()
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
	m.mu.Unlock()
	if !found {
		return fmt.Errorf("route dengan ID '%s' tidak ditemukan", routeId)
	}
	return m.saveAndReload()
}

func (m *Manager) DeleteRoute(routeId string) error {
	m.mu.Lock()
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
		m.mu.Unlock()
		return fmt.Errorf("route dengan ID '%s' tidak ditemukan", routeId)
	}
	m.config.Routes = newRoutes
	m.mu.Unlock()
	return m.saveAndReload()
}

// ========================================================
// PENGELOLAAN PLUGIN (CRUD)
// ========================================================

// --- Plugin Global ---

// AddGlobalPlugin menambahkan plugin global.
func (m *Manager) AddGlobalPlugin(p PluginConfig) error {
	m.mu.Lock()
	for _, existing := range m.config.GlobalPlugins {
		if existing.Name == p.Name {
			m.mu.Unlock()
			return fmt.Errorf("global plugin '%s' sudah ada", p.Name)
		}
	}
	m.config.GlobalPlugins = append(m.config.GlobalPlugins, p)
	m.mu.Unlock()
	return m.saveAndReload()
}

// UpdateGlobalPlugin memperbarui plugin global.
func (m *Manager) UpdateGlobalPlugin(pluginName string, p PluginConfig) error {
	m.mu.Lock()
	found := false
	for i := range m.config.GlobalPlugins {
		if m.config.GlobalPlugins[i].Name == pluginName {
			m.config.GlobalPlugins[i] = p
			found = true
			break
		}
	}
	m.mu.Unlock()
	if !found {
		return fmt.Errorf("global plugin '%s' tidak ditemukan", pluginName)
	}
	return m.saveAndReload()
}

// DeleteGlobalPlugin menghapus plugin global.
func (m *Manager) DeleteGlobalPlugin(pluginName string) error {
	m.mu.Lock()
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
		m.mu.Unlock()
		return fmt.Errorf("global plugin '%s' tidak ditemukan", pluginName)
	}
	m.config.GlobalPlugins = newPlugins
	m.mu.Unlock()
	return m.saveAndReload()
}

// --- Plugin di Service ---

func (m *Manager) AddPluginToService(serviceId string, p PluginConfig) error {
	m.mu.Lock()
	serviceFound := false
	for i := range m.config.Services {
		if m.config.Services[i].Id == serviceId {
			serviceFound = true
			for _, existingPlugin := range m.config.Services[i].Plugins {
				if existingPlugin.Name == p.Name {
					m.mu.Unlock()
					return fmt.Errorf("plugin '%s' sudah ada di service '%s'", p.Name, m.config.Services[i].Name)
				}
			}
			m.config.Services[i].Plugins = append(m.config.Services[i].Plugins, p)
			break
		}
	}
	m.mu.Unlock()
	if !serviceFound {
		return fmt.Errorf("service dengan ID '%s' tidak ditemukan", serviceId)
	}
	return m.saveAndReload()
}

func (m *Manager) UpdatePluginInService(serviceId string, pluginName string, newPluginConfig PluginConfig) error {
	m.mu.Lock()
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
	m.mu.Unlock()
	if !serviceFound {
		return fmt.Errorf("service dengan ID '%s' tidak ditemukan", serviceId)
	}
	if !pluginFound {
		return fmt.Errorf("plugin '%s' tidak ditemukan di service", pluginName)
	}
	return m.saveAndReload()
}

func (m *Manager) DeletePluginFromService(serviceId string, pluginName string) error {
	m.mu.Lock()
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
	m.mu.Unlock()
	if !serviceFound {
		return fmt.Errorf("service dengan ID '%s' tidak ditemukan", serviceId)
	}
	if !pluginFound {
		return fmt.Errorf("plugin '%s' tidak ditemukan di service", pluginName)
	}
	return m.saveAndReload()
}

// --- Plugin di Route ---

func (m *Manager) AddPluginToRoute(routeId string, p PluginConfig) error {
	m.mu.Lock()
	routeFound := false
	for i := range m.config.Routes {
		if m.config.Routes[i].Id == routeId {
			routeFound = true
			for _, existingPlugin := range m.config.Routes[i].Plugins {
				if existingPlugin.Name == p.Name {
					m.mu.Unlock()
					return fmt.Errorf("plugin '%s' sudah ada di route '%s'", p.Name, m.config.Routes[i].Name)
				}
			}
			m.config.Routes[i].Plugins = append(m.config.Routes[i].Plugins, p)
			break
		}
	}
	m.mu.Unlock()
	if !routeFound {
		return fmt.Errorf("route dengan ID '%s' tidak ditemukan", routeId)
	}
	return m.saveAndReload()
}

func (m *Manager) UpdatePluginInRoute(routeId string, pluginName string, newPluginConfig PluginConfig) error {
	m.mu.Lock()
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
	m.mu.Unlock()
	if !routeFound {
		return fmt.Errorf("route dengan ID '%s' tidak ditemukan", routeId)
	}
	if !pluginFound {
		return fmt.Errorf("plugin '%s' tidak ditemukan di route", pluginName)
	}
	return m.saveAndReload()
}

func (m *Manager) DeletePluginFromRoute(routeId string, pluginName string) error {
	m.mu.Lock()
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
	m.mu.Unlock()
	if !routeFound {
		return fmt.Errorf("route dengan ID '%s' tidak ditemukan", routeId)
	}
	if !pluginFound {
		return fmt.Errorf("plugin '%s' tidak ditemukan di route", pluginName)
	}
	return m.saveAndReload()
}
