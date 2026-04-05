package config

import (
	"os"
	"path/filepath"
	"testing"
)

func setupTestManager(t *testing.T) (*Manager, string) {
	tmpDir, err := os.MkdirTemp("", "config-plugin-test")
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(tmpDir, "config.yaml")

	m := &Manager{
		configPath: configPath,
		config: &Config{
			Server: ServerConfig{LogLevel: "info"},
			Upstreams: []Upstream{
				{Id: "u1", Name: "upstream1", Algorithm: "round-robin", Targets: []UpstreamTarget{{Host: "localhost", Port: 9090}}},
			},
			Services: []Service{
				{Id: "s1", Name: "service1", UpstreamId: "u1"},
			},
			Routes: []Route{
				{Id: "r1", Name: "route1", ServiceId: "s1"},
			},
		},
	}
	m.atomicConfig.Store(m.config)
	return m, tmpDir
}

func TestManager_GlobalPluginCRUD(t *testing.T) {
	m, tmpDir := setupTestManager(t)
	defer os.RemoveAll(tmpDir)
	defer m.Close()

	p := PluginConfig{Name: "auth", Config: map[string]interface{}{"key": "val"}}

	// Add
	err := m.AddGlobalPlugin(p)
	if err != nil {
		t.Fatalf("AddGlobalPlugin failed: %v", err)
	}
	if len(m.GetConfig().GlobalPlugins) != 1 {
		t.Error("expected 1 global plugin")
	}

	// Add duplicate
	err = m.AddGlobalPlugin(p)
	if err == nil {
		t.Error("expected error adding duplicate plugin, got nil")
	}

	// Update
	p.Config["key"] = "newval"
	err = m.UpdateGlobalPlugin("auth", p)
	if err != nil {
		t.Fatalf("UpdateGlobalPlugin failed: %v", err)
	}
	if m.GetConfig().GlobalPlugins[0].Config["key"] != "newval" {
		t.Error("update not applied")
	}

	// Update non-existent
	err = m.UpdateGlobalPlugin("non-existent", p)
	if err == nil {
		t.Error("expected error updating non-existent plugin, got nil")
	}

	// Delete
	err = m.DeleteGlobalPlugin("auth")
	if err != nil {
		t.Fatalf("DeleteGlobalPlugin failed: %v", err)
	}
	if len(m.GetConfig().GlobalPlugins) != 0 {
		t.Error("expected 0 global plugins")
	}

	// Delete non-existent
	err = m.DeleteGlobalPlugin("auth")
	if err == nil {
		t.Error("expected error deleting non-existent plugin, got nil")
	}
}

func TestManager_ServicePluginCRUD(t *testing.T) {
	m, tmpDir := setupTestManager(t)
	defer os.RemoveAll(tmpDir)
	defer m.Close()

	p := PluginConfig{Name: "cors"}

	// Add
	err := m.AddPluginToService("s1", p)
	if err != nil {
		t.Fatalf("AddPluginToService failed: %v", err)
	}
	if len(m.GetConfig().Services[0].Plugins) != 1 {
		t.Error("expected 1 plugin in service")
	}

	// Add duplicate
	err = m.AddPluginToService("s1", p)
	if err == nil {
		t.Error("expected error adding duplicate plugin to service, got nil")
	}

	// Add to non-existent service
	err = m.AddPluginToService("s2", p)
	if err == nil {
		t.Error("expected error adding plugin to non-existent service, got nil")
	}

	// Update
	p.Config = map[string]interface{}{"origin": "*"}
	err = m.UpdatePluginInService("s1", "cors", p)
	if err != nil {
		t.Fatalf("UpdatePluginInService failed: %v", err)
	}

	// Update non-existent
	err = m.UpdatePluginInService("s1", "non-existent", p)
	if err == nil {
		t.Error("expected error updating non-existent plugin in service, got nil")
	}

	// Delete
	err = m.DeletePluginFromService("s1", "cors")
	if err != nil {
		t.Fatalf("DeletePluginFromService failed: %v", err)
	}

	// Delete non-existent
	err = m.DeletePluginFromService("s1", "cors")
	if err == nil {
		t.Error("expected error deleting non-existent plugin from service, got nil")
	}
}

func TestManager_RoutePluginCRUD(t *testing.T) {
	m, tmpDir := setupTestManager(t)
	defer os.RemoveAll(tmpDir)
	defer m.Close()

	p := PluginConfig{Name: "ratelimit"}

	// Add
	err := m.AddPluginToRoute("r1", p)
	if err != nil {
		t.Fatalf("AddPluginToRoute failed: %v", err)
	}

	// Duplicate
	err = m.AddPluginToRoute("r1", p)
	if err == nil {
		t.Error("expected error adding duplicate plugin to route")
	}

	// Update
	err = m.UpdatePluginInRoute("r1", "ratelimit", p)
	if err != nil {
		t.Fatal(err)
	}

	// Delete
	err = m.DeletePluginFromRoute("r1", "ratelimit")
	if err != nil {
		t.Fatal(err)
	}
}
