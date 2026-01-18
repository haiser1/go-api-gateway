package config

import (
	"sync"
	"sync/atomic"
)

type PluginConfig struct {
	Name   string                 `yaml:"name" json:"name"`
	Config map[string]interface{} `yaml:"config" json:"config"`
}

type Service struct {
	Id       string         `yaml:"id" json:"id"`
	Name     string         `yaml:"name" json:"name"`
	Host     string         `yaml:"host" json:"host"`
	Port     int            `yaml:"port" json:"port"`
	Protocol string         `yaml:"protocol" json:"protocol"`
	Plugins  []PluginConfig `yaml:"plugins,omitempty" json:"plugins,omitempty"`
}

type Route struct {
	Id        string         `yaml:"id" json:"id"`
	Name      string         `yaml:"name" json:"name"`
	Methods   []string       `yaml:"methods" json:"methods"`
	Paths     []string       `yaml:"paths" json:"paths"`
	ServiceId string         `yaml:"serviceId" json:"serviceId"`
	Plugins   []PluginConfig `yaml:"plugins,omitempty" json:"plugins,omitempty"`
}

type Config struct {
	GlobalPlugins []PluginConfig `yaml:"global_plugins" json:"global_plugins"`
	Services      []Service      `yaml:"services" json:"services"`
	Routes        []Route        `yaml:"routes" json:"routes"`
}

// Manager holds the configuration and handles reloading.
type Manager struct {
	mu           sync.Mutex
	configPath   string
	config       *Config // The current "write" source of truth
	atomicConfig atomic.Value
	Reload       chan struct{}
}

// GetConfig returns the current configuration (thread-safe, lock-free).
func (m *Manager) GetConfig() *Config {
	val := m.atomicConfig.Load()
	if val == nil {
		return nil
	}
	return val.(*Config)
}
