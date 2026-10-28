package config

import (
	"testing"
)

func TestService_ApplyDefaults(t *testing.T) {
	tests := []struct {
		name     string
		s        Service
		expected Service
	}{
		{
			name: "all zero values",
			s:    Service{},
			expected: Service{
				Protocol:       "http",
				Plugins:        []PluginConfig{},
				Timeout:        30,
				ConnectTimeout: 10,
				ReadTimeout:    30,
				RetryBackoff:   1.5,
			},
		},
		{
			name: "some values set",
			s: Service{
				Protocol: "https",
			},
			expected: Service{
				Protocol:       "https",
				Plugins:        []PluginConfig{},
				Timeout:        30,
				ConnectTimeout: 10,
				ReadTimeout:    30,
				RetryBackoff:   1.5,
			},
		},
		{
			name: "already set",
			s: Service{
				Protocol:       "grpc",
				Plugins:        []PluginConfig{{Name: "test"}},
				Timeout:        60,
				ConnectTimeout: 20,
				ReadTimeout:    60,
				RetryBackoff:   2.0,
			},
			expected: Service{
				Protocol:       "grpc",
				Plugins:        []PluginConfig{{Name: "test"}},
				Timeout:        60,
				ConnectTimeout: 20,
				ReadTimeout:    60,
				RetryBackoff:   2.0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.s.ApplyDefaults()
			if tt.s.Protocol != tt.expected.Protocol {
				t.Errorf("expected Protocol %s, got %s", tt.expected.Protocol, tt.s.Protocol)
			}
			if len(tt.s.Plugins) != len(tt.expected.Plugins) {
				t.Errorf("expected %d plugins, got %d", len(tt.expected.Plugins), len(tt.s.Plugins))
			}
			if tt.s.Timeout != tt.expected.Timeout {
				t.Errorf("expected Timeout %d, got %d", tt.expected.Timeout, tt.s.Timeout)
			}
			if tt.s.ConnectTimeout != tt.expected.ConnectTimeout {
				t.Errorf("expected ConnectTimeout %d, got %d", tt.expected.ConnectTimeout, tt.s.ConnectTimeout)
			}
			if tt.s.ReadTimeout != tt.expected.ReadTimeout {
				t.Errorf("expected ReadTimeout %d, got %d", tt.expected.ReadTimeout, tt.s.ReadTimeout)
			}
			if tt.s.RetryBackoff != tt.expected.RetryBackoff {
				t.Errorf("expected RetryBackoff %f, got %f", tt.expected.RetryBackoff, tt.s.RetryBackoff)
			}
		})
	}
}

func TestServerConfig_ApplyDefaults(t *testing.T) {
	s := ServerConfig{}
	s.ApplyDefaults()

	if s.ProxyPort != 8080 {
		t.Errorf("expected ProxyPort 8080, got %d", s.ProxyPort)
	}
	if s.AdminPort != 8081 {
		t.Errorf("expected AdminPort 8081, got %d", s.AdminPort)
	}
	if s.LogLevel != "info" {
		t.Errorf("expected LogLevel info, got %s", s.LogLevel)
	}
}

func TestUpstream_ApplyDefaults(t *testing.T) {
	u := Upstream{
		Targets: []UpstreamTarget{
			{Host: "localhost"},
		},
	}
	u.ApplyDefaults()

	if u.Algorithm != "round-robin" {
		t.Errorf("expected Algorithm round-robin, got %s", u.Algorithm)
	}
	if u.Targets[0].Port != 80 {
		t.Errorf("expected target port 80, got %d", u.Targets[0].Port)
	}
	if u.Targets[0].Weight != 100 {
		t.Errorf("expected target weight 100, got %d", u.Targets[0].Weight)
	}
}

func TestManager_GetConfig(t *testing.T) {
	m := &Manager{}
	cfg := &Config{Server: ServerConfig{LogLevel: "debug"}}
	m.atomicConfig.Store(cfg)

	got := m.GetConfig()
	if got != cfg {
		t.Errorf("expected %v, got %v", cfg, got)
	}
	if got.Server.LogLevel != "debug" {
		t.Errorf("expected log level debug, got %s", got.Server.LogLevel)
	}
}

func TestConfig_DeepCopy(t *testing.T) {
	orig := &Config{
		Server: ServerConfig{ProxyPort: 8080, AdminPort: 8081, LogLevel: "info"},
		GlobalPlugins: []PluginConfig{
			{Name: "cors", Enabled: true, Config: map[string]interface{}{"origin": "*"}},
		},
		Upstreams: []Upstream{
			{
				Id: "u1", Name: "upstream-1", Algorithm: "round-robin",
				Targets: []UpstreamTarget{
					{Host: "127.0.0.1", Port: 8001, Weight: 100, HealthCheck: &HealthCheckConfig{Path: "/health"}},
				},
			},
		},
		Services: []Service{
			{
				Id: "s1", Name: "service-1", UpstreamId: "u1", Protocol: "http",
				Plugins: []PluginConfig{
					{Name: "rate-limit", Enabled: true, Config: map[string]interface{}{"rate": 100}},
				},
			},
		},
		Routes: []Route{
			{
				Id: "r1", Name: "route-1", ServiceId: "s1",
				Methods: []string{"GET", "POST"},
				Paths:   []string{"/api/v1"},
				Plugins: []PluginConfig{
					{Name: "auth", Enabled: true},
				},
			},
		},
	}

	cp := orig.DeepCopy()
	if cp == nil {
		t.Fatal("expected non-nil copy")
	}

	// Mutate the copy
	cp.Server.LogLevel = "error"
	cp.GlobalPlugins[0].Config["origin"] = "https://example.com"
	cp.Upstreams[0].Targets[0].Port = 9999
	cp.Upstreams[0].Targets[0].HealthCheck.Path = "/status"
	cp.Services[0].Plugins[0].Config["rate"] = 500
	cp.Routes[0].Methods[0] = "PUT"
	cp.Routes[0].Paths[0] = "/api/v2"

	// Verify original is untouched
	if orig.Server.LogLevel != "info" {
		t.Errorf("original Server.LogLevel was mutated: %s", orig.Server.LogLevel)
	}
	if orig.GlobalPlugins[0].Config["origin"] != "*" {
		t.Errorf("original GlobalPlugins was mutated: %v", orig.GlobalPlugins[0].Config["origin"])
	}
	if orig.Upstreams[0].Targets[0].Port != 8001 {
		t.Errorf("original UpstreamTarget.Port was mutated: %d", orig.Upstreams[0].Targets[0].Port)
	}
	if orig.Upstreams[0].Targets[0].HealthCheck.Path != "/health" {
		t.Errorf("original HealthCheck.Path was mutated: %s", orig.Upstreams[0].Targets[0].HealthCheck.Path)
	}
	if orig.Services[0].Plugins[0].Config["rate"] != 100 {
		t.Errorf("original Service plugin config was mutated: %v", orig.Services[0].Plugins[0].Config["rate"])
	}
	if orig.Routes[0].Methods[0] != "GET" {
		t.Errorf("original Route.Methods was mutated: %s", orig.Routes[0].Methods[0])
	}
	if orig.Routes[0].Paths[0] != "/api/v1" {
		t.Errorf("original Route.Paths was mutated: %s", orig.Routes[0].Paths[0])
	}

	// Test nil config deep copy
	var nilCfg *Config
	if nilCfg.DeepCopy() != nil {
		t.Error("expected nil for nil config DeepCopy")
	}
}
