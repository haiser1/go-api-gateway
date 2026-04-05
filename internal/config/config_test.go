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
