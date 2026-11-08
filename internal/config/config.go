package config

import (
	"sync"
	"sync/atomic"

	"github.com/fsnotify/fsnotify"
)

// === Server Config ===

type ServerConfig struct {
	ProxyPort int    `yaml:"proxy_port" json:"proxy_port" validate:"omitempty,min=1,max=65535"`
	AdminPort int    `yaml:"admin_port" json:"admin_port" validate:"omitempty,min=1,max=65535"`
	LogLevel  string `yaml:"log_level" json:"log_level" validate:"omitempty,oneof=trace debug info warn error fatal panic disabled"`
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
	Name    string                 `yaml:"name" json:"name" validate:"required"`
	Enabled bool                   `yaml:"enabled" json:"enabled"`
	Config  map[string]interface{} `yaml:"config" json:"config"`
}

// DeepCopy creates an independent copy of PluginConfig.
func (p PluginConfig) DeepCopy() PluginConfig {
	cp := PluginConfig{
		Name:    p.Name,
		Enabled: p.Enabled,
	}
	if p.Config != nil {
		cp.Config = deepCopyConfigMap(p.Config)
	}
	return cp
}

func deepCopyConfigMap(in map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = deepCopyConfigValue(v)
	}
	return out
}

func deepCopyConfigValue(v interface{}) interface{} {
	switch value := v.(type) {
	case map[string]interface{}:
		return deepCopyConfigMap(value)
	case map[interface{}]interface{}:
		out := make(map[interface{}]interface{}, len(value))
		for k, item := range value {
			out[k] = deepCopyConfigValue(item)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(value))
		for i := range value {
			out[i] = deepCopyConfigValue(value[i])
		}
		return out
	case []string:
		return append([]string(nil), value...)
	default:
		return v
	}
}

// === Upstream ===

type HealthCheckConfig struct {
	Path     string `yaml:"path" json:"path"`
	Interval string `yaml:"interval" json:"interval"`
}

type UpstreamTarget struct {
	Host        string             `yaml:"host" json:"host" validate:"required"`
	Port        int                `yaml:"port" json:"port" validate:"omitempty,min=1,max=65535"`
	Weight      int                `yaml:"weight" json:"weight" validate:"omitempty,min=1"`
	HealthCheck *HealthCheckConfig `yaml:"health_check,omitempty" json:"health_check,omitempty"`
}

// DeepCopy creates an independent copy of UpstreamTarget.
func (t UpstreamTarget) DeepCopy() UpstreamTarget {
	cp := t
	if t.HealthCheck != nil {
		hc := *t.HealthCheck
		cp.HealthCheck = &hc
	}
	return cp
}

type Upstream struct {
	Id        string           `yaml:"id" json:"id" validate:"required"`
	Name      string           `yaml:"name" json:"name" validate:"omitempty,max=128"`
	Algorithm string           `yaml:"algorithm" json:"algorithm" validate:"omitempty,oneof=round-robin weighted-round-robin"`
	Targets   []UpstreamTarget `yaml:"targets" json:"targets" validate:"omitempty,min=1,dive"`
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

// DeepCopy creates an independent copy of Upstream.
func (u Upstream) DeepCopy() Upstream {
	cp := Upstream{
		Id:        u.Id,
		Name:      u.Name,
		Algorithm: u.Algorithm,
	}
	if u.Targets != nil {
		cp.Targets = make([]UpstreamTarget, len(u.Targets))
		for i := range u.Targets {
			cp.Targets[i] = u.Targets[i].DeepCopy()
		}
	}
	return cp
}

// === Service ===

type Service struct {
	Id         string         `yaml:"id" json:"id" validate:"required"`
	Name       string         `yaml:"name" json:"name" validate:"omitempty,max=128"`
	UpstreamId string         `yaml:"upstream_id" json:"upstream_id" validate:"required"`
	Protocol   string         `yaml:"protocol" json:"protocol" validate:"omitempty,oneof=http https"`
	Plugins    []PluginConfig `yaml:"plugins,omitempty" json:"plugins,omitempty" validate:"dive"`

	// Timeout settings (in seconds) — zero means "use the documented
	// default" (ApplyDefaults); only negative values are invalid.
	Timeout        int `yaml:"timeout,omitempty" json:"timeout,omitempty" validate:"omitempty,min=0"`                 // Total request timeout (default: 30)
	ConnectTimeout int `yaml:"connect_timeout,omitempty" json:"connect_timeout,omitempty" validate:"omitempty,min=0"` // Connection timeout (default: 10)
	ReadTimeout    int `yaml:"read_timeout,omitempty" json:"read_timeout,omitempty" validate:"omitempty,min=0"`       // Read timeout (default: 30)

	// Retry settings
	Retries      int     `yaml:"retries,omitempty" json:"retries,omitempty" validate:"omitempty,min=0"`             // Number of retries (default: 0)
	RetryBackoff float64 `yaml:"retry_backoff,omitempty" json:"retry_backoff,omitempty" validate:"omitempty,min=0"` // Backoff multiplier (default: 1.5)
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

// DeepCopy creates an independent copy of Service.
func (s Service) DeepCopy() Service {
	cp := s
	if s.Plugins != nil {
		cp.Plugins = make([]PluginConfig, len(s.Plugins))
		for i := range s.Plugins {
			cp.Plugins[i] = s.Plugins[i].DeepCopy()
		}
	}
	return cp
}

// === Route ===

type Route struct {
	Id          string         `yaml:"id" json:"id" validate:"required"`
	Name        string         `yaml:"name" json:"name" validate:"omitempty,max=128"`
	Methods     []string       `yaml:"methods" json:"methods" validate:"required,min=1,unique,dive,oneof=GET POST PUT PATCH DELETE HEAD OPTIONS TRACE CONNECT"`
	Paths       []string       `yaml:"paths" json:"paths" validate:"required,min=1,unique,dive,startswith=/"`
	ServiceId   string         `yaml:"service_id" json:"service_id" validate:"required"`
	StripPrefix bool           `yaml:"strip_prefix,omitempty" json:"strip_prefix,omitempty"`
	Plugins     []PluginConfig `yaml:"plugins,omitempty" json:"plugins,omitempty" validate:"dive"`
}

// DeepCopy creates an independent copy of Route.
func (r Route) DeepCopy() Route {
	cp := Route{
		Id:          r.Id,
		Name:        r.Name,
		ServiceId:   r.ServiceId,
		StripPrefix: r.StripPrefix,
	}
	if r.Methods != nil {
		cp.Methods = make([]string, len(r.Methods))
		copy(cp.Methods, r.Methods)
	}
	if r.Paths != nil {
		cp.Paths = make([]string, len(r.Paths))
		copy(cp.Paths, r.Paths)
	}
	if r.Plugins != nil {
		cp.Plugins = make([]PluginConfig, len(r.Plugins))
		for i := range r.Plugins {
			cp.Plugins[i] = r.Plugins[i].DeepCopy()
		}
	}
	return cp
}

// === Top-Level Config ===

type Config struct {
	Server        ServerConfig   `yaml:"server" json:"server"`
	Auth          *AuthConfig    `yaml:"auth,omitempty" json:"auth,omitempty"`
	GlobalPlugins []PluginConfig `yaml:"global_plugins" json:"global_plugins" validate:"omitempty,dive"`
	Upstreams     []Upstream     `yaml:"upstreams" json:"upstreams" validate:"omitempty,dive"`
	Services      []Service      `yaml:"services" json:"services" validate:"omitempty,dive"`
	Routes        []Route        `yaml:"routes" json:"routes" validate:"omitempty,dive"`
}

// DeepCopy creates a fully independent copy of the Config, safe for mutation.
func (c *Config) DeepCopy() *Config {
	if c == nil {
		return nil
	}

	newCfg := &Config{
		Server: c.Server,
		Auth:   c.Auth.DeepCopy(),
	}

	if c.GlobalPlugins != nil {
		newCfg.GlobalPlugins = make([]PluginConfig, len(c.GlobalPlugins))
		for i := range c.GlobalPlugins {
			newCfg.GlobalPlugins[i] = c.GlobalPlugins[i].DeepCopy()
		}
	}

	if c.Upstreams != nil {
		newCfg.Upstreams = make([]Upstream, len(c.Upstreams))
		for i := range c.Upstreams {
			newCfg.Upstreams[i] = c.Upstreams[i].DeepCopy()
		}
	}

	if c.Services != nil {
		newCfg.Services = make([]Service, len(c.Services))
		for i := range c.Services {
			newCfg.Services[i] = c.Services[i].DeepCopy()
		}
	}

	if c.Routes != nil {
		newCfg.Routes = make([]Route, len(c.Routes))
		for i := range c.Routes {
			newCfg.Routes[i] = c.Routes[i].DeepCopy()
		}
	}

	return newCfg
}

// Manager holds the configuration and handles reloading.
type Manager struct {
	mu           sync.Mutex
	configPath   string
	config       *Config // The current "write" source of truth
	atomicConfig atomic.Pointer[Config]

	// Index maps for O(1) lookups (guarded by mu, rebuilt on commit/load)
	upstreamIdx     map[string]int // id -> index in Config.Upstreams
	upstreamNameIdx map[string]int // name -> index in Config.Upstreams
	serviceIdx      map[string]int // id -> index in Config.Services
	serviceNameIdx  map[string]int // name -> index in Config.Services
	routeIdx        map[string]int // id -> index in Config.Routes
	routeNameIdx    map[string]int // name -> index in Config.Routes
	globalPluginIdx map[string]int // name -> index in Config.GlobalPlugins

	Reload  chan struct{}
	watcher *fsnotify.Watcher
	done    chan struct{}

	lastHash          [32]byte // SHA-256 hash of latest config content
	warnedFileRemoved bool     // file-missing warning already emitted (guarded by mu)
	closeOnce         sync.Once
	wg                sync.WaitGroup
}

// rebuildIndexesLocked rebuilds index maps for O(1) lookup.
// Caller must ALREADY HOLD m.mu.
func (m *Manager) rebuildIndexesLocked() {
	if m.config == nil {
		return
	}

	m.upstreamIdx = make(map[string]int, len(m.config.Upstreams))
	m.upstreamNameIdx = make(map[string]int, len(m.config.Upstreams))
	for i := range m.config.Upstreams {
		m.upstreamIdx[m.config.Upstreams[i].Id] = i
		m.upstreamNameIdx[m.config.Upstreams[i].Name] = i
	}

	m.serviceIdx = make(map[string]int, len(m.config.Services))
	m.serviceNameIdx = make(map[string]int, len(m.config.Services))
	for i := range m.config.Services {
		m.serviceIdx[m.config.Services[i].Id] = i
		m.serviceNameIdx[m.config.Services[i].Name] = i
	}

	m.routeIdx = make(map[string]int, len(m.config.Routes))
	m.routeNameIdx = make(map[string]int, len(m.config.Routes))
	for i := range m.config.Routes {
		m.routeIdx[m.config.Routes[i].Id] = i
		m.routeNameIdx[m.config.Routes[i].Name] = i
	}

	m.globalPluginIdx = make(map[string]int, len(m.config.GlobalPlugins))
	for i := range m.config.GlobalPlugins {
		m.globalPluginIdx[m.config.GlobalPlugins[i].Name] = i
	}
}

// Close stops the configuration watcher and waits for background routines to finish.
func (m *Manager) Close() error {
	var err error
	m.closeOnce.Do(func() {
		if m.done != nil {
			close(m.done)
		}
		if m.watcher != nil {
			err = m.watcher.Close()
		}
		m.wg.Wait()
	})
	return err
}

// GetConfig returns the current configuration (thread-safe, lock-free).
func (m *Manager) GetConfig() *Config {
	return m.atomicConfig.Load()
}

// ConfigPath returns the absolute or relative path of the managed config file.
func (m *Manager) ConfigPath() string {
	return m.configPath
}
