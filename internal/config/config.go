package config

import (
	"sync"
	"sync/atomic"

	"github.com/fsnotify/fsnotify"
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

	// Timeout settings (in seconds)
	Timeout        int `yaml:"timeout,omitempty" json:"timeout,omitempty"`                 // Total request timeout (default: 30)
	ConnectTimeout int `yaml:"connect_timeout,omitempty" json:"connect_timeout,omitempty"` // Connection timeout (default: 10)
	ReadTimeout    int `yaml:"read_timeout,omitempty" json:"read_timeout,omitempty"`       // Read timeout (default: 30)

	// Retry settings
	Retries      int     `yaml:"retries,omitempty" json:"retries,omitempty"`             // Number of retries (default: 0)
	RetryBackoff float64 `yaml:"retry_backoff,omitempty" json:"retry_backoff,omitempty"` // Backoff multiplier (default: 1.5)
}

// ApplyDefaults sets sensible defaults for zero-value fields
func (s *Service) ApplyDefaults() {
	if s.Protocol == "" {
		s.Protocol = "http"
	}
	if s.Port == 0 {
		s.Port = 8080
	}
	if s.Plugins == nil {
		s.Plugins = []PluginConfig{}
	}
	if s.Timeout == 0 {
		s.Timeout = 30
	}
	if s.ConnectTimeout == 0 {
		s.ConnectTimeout = 10
	}
	if s.ReadTimeout == 0 {
		s.ReadTimeout = 30
	}
	if s.RetryBackoff == 0 {
		s.RetryBackoff = 1.5
	}
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
	LogLevel      string         `yaml:"log_level" json:"log_level"`
	GlobalPlugins []PluginConfig `yaml:"global_plugins" json:"global_plugins"`
	Services      []Service      `yaml:"services" json:"services"`
	Routes        []Route        `yaml:"routes" json:"routes"`
}

// Manager holds the configuration and handles reloading.
type Manager struct {
	mu           sync.Mutex
	configPath   string
	config       *Config // The current "write" source of truth
	atomicConfig atomic.Pointer[Config]
	Reload       chan struct{}
	watcher      *fsnotify.Watcher
	done         chan struct{}
}

// Close stops the configuration watcher.
func (m *Manager) Close() error {
	if m.done != nil {
		close(m.done)
	}
	if m.watcher != nil {
		return m.watcher.Close()
	}
	return nil
}

// GetConfig returns the current configuration (thread-safe, lock-free).
func (m *Manager) GetConfig() *Config {
	return m.atomicConfig.Load()
}
