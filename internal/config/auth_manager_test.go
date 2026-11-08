package config

import (
	"os"
	"testing"
)

func TestManagerUpdateAuthConfigPersistsAndPublishes(t *testing.T) {
	m, tmpDir := setupTestManager(t)
	defer os.RemoveAll(tmpDir)
	defer m.Close()

	authCfg := &AuthConfig{
		Mode:   AuthModeAPIKey,
		APIKey: &APIKeyAuthConfig{SecretRef: "API_KEY_HASH"},
	}
	if err := m.UpdateAuthConfig(authCfg); err != nil {
		t.Fatalf("UpdateAuthConfig failed: %v", err)
	}

	got := m.GetAuthConfig()
	if got == nil || got.Mode != AuthModeAPIKey || got.APIKey.SecretRef != "API_KEY_HASH" {
		t.Fatalf("published auth config = %+v", got)
	}
	got.APIKey.SecretRef = "mutated"
	if m.GetAuthConfig().APIKey.SecretRef != "API_KEY_HASH" {
		t.Fatal("GetAuthConfig must return an independent copy")
	}

	data, err := os.ReadFile(m.ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("config file was not persisted")
	}
}
