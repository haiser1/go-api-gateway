package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestNewManager(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "config.yaml")
	initialCfg := Config{LogLevel: "info"}
	data, _ := yaml.Marshal(initialCfg)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	m, err := NewManager(tmpDir)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	defer m.Close()
	if m == nil {
		t.Fatal("NewManager returned nil")
	}

	if m.GetConfig().LogLevel != "info" {
		t.Errorf("expected log level info, got %s", m.GetConfig().LogLevel)
	}
}

func TestManager_loadConfigFromFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-test-load")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "config.yaml")
	m := &Manager{configPath: configPath}

	// Test missing file
	err = m.loadConfigFromFile()
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}

	// Test invalid YAML
	if err := os.WriteFile(configPath, []byte("invalid: yaml: :"), 0644); err != nil {
		t.Fatal(err)
	}
	err = m.loadConfigFromFile()
	if err == nil {
		t.Error("expected error for invalid YAML, got nil")
	}

	// Test valid YAML
	validCfg := Config{LogLevel: "warn"}
	data, _ := yaml.Marshal(validCfg)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatal(err)
	}
	err = m.loadConfigFromFile()
	if err != nil {
		t.Errorf("expected no error for valid YAML, got %v", err)
	}
	if m.GetConfig().LogLevel != "warn" {
		t.Errorf("expected log level warn, got %s", m.GetConfig().LogLevel)
	}
}

func TestManager_WriteConfigSafe(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-test-write")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "config.yaml")
	m := &Manager{
		configPath: configPath,
		config:     &Config{LogLevel: "error"},
	}

	err = m.WriteConfigSafe()
	if err != nil {
		t.Fatalf("WriteConfigSafe failed: %v", err)
	}

	// Verify file content
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var gotCfg Config
	yaml.Unmarshal(data, &gotCfg)
	if gotCfg.LogLevel != "error" {
		t.Errorf("expected log level error, got %s", gotCfg.LogLevel)
	}

	// Verify backup
	files, _ := filepath.Glob(configPath + ".bak-*")
	if len(files) == 0 {
		// First write doesn't have backup if file didn't exist, but it existed after first write
	}

	m.config.LogLevel = "fatal"
	err = m.WriteConfigSafe()
	if err != nil {
		t.Fatal(err)
	}
	files, _ = filepath.Glob(configPath + ".bak-*")
	if len(files) == 0 {
		t.Error("expected backup file to be created")
	}
}

func TestManager_watchConfig(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-test-watch")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "config.yaml")
	initialCfg := Config{LogLevel: "info"}
	data, _ := yaml.Marshal(initialCfg)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	m, err := NewManager(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	// Modify file
	newCfg := Config{LogLevel: "debug"}
	newData, _ := yaml.Marshal(newCfg)
	// Small sleep to ensure watcher is ready
	time.Sleep(100 * time.Millisecond)
	if err := os.WriteFile(configPath, newData, 0644); err != nil {
		t.Fatal(err)
	}

	// Wait for reload (with timeout)
	select {
	case <-m.Reload:
		if m.GetConfig().LogLevel != "debug" {
			t.Errorf("expected reloaded log level debug, got %s", m.GetConfig().LogLevel)
		}
	case <-time.After(2 * time.Second):
		t.Error("timed out waiting for config reload")
	}
}
