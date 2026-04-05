package config

import (
	"sync"
	"sync/atomic"

	"github.com/fsnotify/fsnotify"
)

// === Server Config ===

type ServerConfig struct {
	ProxyPort int    `yaml:"proxy_port" json:"proxy_port"`
	AdminPort int    `yaml:"admin_port" json:"admin_port"`
	LogLevel  string `yaml:"log_level" json:"log_level"`
}

func (s *ServerConfig) ApplyDefaults() {
	if s.ProxyPort == 0 {
		s.ProxyPort = 8080
	}
	if s.AdminPort == 0 {
		s.AdminPort = 8081
	}
	if s.LogLevel == "" {
		s.LogLevel = "info"
	}
}

// === Plugin Config ===

type PluginConfig struct {
	Name    string                 `yaml:"name" json:"name"`
	Enabled bool                   `yaml:"enabled" json:"enabled"`
	Config  map[string]interface{} `yaml:"config" json:"config"`
}

// === Upstream ===

type HealthCheckConfig struct {
	Path     string `yaml:"path" json:"path"`
	Interval string `yaml:"interval" json:"interval"`
}

type UpstreamTarget struct {
	Host        string             `yaml:"host" json:"host"`
	Port        int                `yaml:"port" json:"port"`
	Weight      int                `yaml:"weight" json:"weight"`
	HealthCheck *HealthCheckConfig `yaml:"health_check,omitempty" json:"health_check,omitempty"`
}

type Upstream struct {
	Id        string           `yaml:"id" json:"id"`
	Name      string           `yaml:"name" json:"name"`
	Algorithm string           `yaml:"algorithm" json:"algorithm"`
	Targets   []UpstreamTarget `yaml:"targets" json:"targets"`
}

func (u *Upstream) ApplyDefaults() {
	if u.Algorithm == "" {
		u.Algorithm = "round-robin"
	}
	for i := range u.Targets {
		if u.Targets[i].Port == 0 {
			u.Targets[i].Port = 80
		}
		if u.Targets[i].Weight == 0 {
			u.Targets[i].Weight = 100
		}
	}
}

// === Service ===

type Service struct {
	Id         string         `yaml:"id" json:"id"`
	Name       string         `yaml:"name" json:"name"`
	UpstreamId string         `yaml:"upstream_id" json:"upstream_id"`
	Protocol   string         `yaml:"protocol" json:"protocol"`
	Plugins    []PluginConfig `yaml:"plugins,omitempty" json:"plugins,omitempty"`

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

// === Route ===

type Route struct {
	Id          string         `yaml:"id" json:"id"`
	Name        string         `yaml:"name" json:"name"`
	Methods     []string       `yaml:"methods" json:"methods"`
	Paths       []string       `yaml:"paths" json:"paths"`
	ServiceId   string         `yaml:"service_id" json:"service_id"`
	StripPrefix bool           `yaml:"strip_prefix,omitempty" json:"strip_prefix,omitempty"`
	Plugins     []PluginConfig `yaml:"plugins,omitempty" json:"plugins,omitempty"`
}

// === Top-Level Config ===

type Config struct {
	Server        ServerConfig   `yaml:"server" json:"server"`
	GlobalPlugins []PluginConfig `yaml:"global_plugins" json:"global_plugins"`
	Upstreams     []Upstream     `yaml:"upstreams" json:"upstreams"`
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
