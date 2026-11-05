package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/haiser1/go-api-gateway/internal/secret"
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
	m.rebuildIndexesLocked()
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

func TestManager_AddAuthPluginProvisionsEnvFile(t *testing.T) {
	// Global-level add provisions the .env file
	m, tmpDir := setupTestManager(t)
	defer os.RemoveAll(tmpDir)
	defer m.Close()

	envPath := filepath.Join(tmpDir, ".env")
	if _, err := os.Stat(envPath); !os.IsNotExist(err) {
		t.Fatalf(".env should not exist before the auth plugin is added")
	}

	if err := m.AddGlobalPlugin(PluginConfig{Name: "auth", Enabled: true}); err != nil {
		t.Fatalf("AddGlobalPlugin failed: %v", err)
	}

	secrets, err := secret.LoadFile(envPath)
	if err != nil {
		t.Fatalf("expected .env provisioned after auth plugin add: %v", err)
	}
	if secrets[secret.APIKeySecret] == "" || secrets[secret.JWTSecret] == "" {
		t.Errorf("expected API_KEY_SECRET and JWT_SECRET in .env, got %v", secrets)
	}

	// Idempotent: re-provisioning must not regenerate existing values
	if err := m.AddPluginToService("s1", PluginConfig{Name: "authorization", Enabled: false}); err != nil {
		t.Fatalf("AddPluginToService failed: %v", err)
	}
	secrets2, err := secret.LoadFile(envPath)
	if err != nil {
		t.Fatalf("failed to re-read .env: %v", err)
	}
	for key, val := range secrets {
		if secrets2[key] != val {
			t.Errorf("existing secret %s was regenerated", key)
		}
	}

	// Non-auth plugins and auth deleting must NOT provision anything
	m2, tmpDir2 := setupTestManager(t)
	defer os.RemoveAll(tmpDir2)
	defer m2.Close()

	if err := m2.AddGlobalPlugin(PluginConfig{Name: "cors", Enabled: true}); err != nil {
		t.Fatalf("AddGlobalPlugin failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmpDir2, ".env")); !os.IsNotExist(err) {
		t.Errorf(".env must not be created for non-auth plugins")
	}

	// Route-level add with embedded auth plugin provisions the .env file
	if err := m2.AddRoute(Route{Id: "r2", Name: "route2", ServiceId: "s1", Methods: []string{"GET"}, Paths: []string{"/r2"}, Plugins: []PluginConfig{{Name: "authorization", Enabled: false}}}); err != nil {
		t.Fatalf("AddRoute failed: %v", err)
	}
	routeEnv := filepath.Join(tmpDir2, ".env")
	if _, err := os.Stat(routeEnv); err != nil {
		t.Fatalf("expected .env provisioned after route embedding auth plugin: %v", err)
	}
}
