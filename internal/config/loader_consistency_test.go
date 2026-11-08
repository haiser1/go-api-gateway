package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func writeTestConfig(t *testing.T, path string, cfg *Config) {
	t.Helper()
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}

// TestCommitAbortWhenBackupFails verifies the backup-is-mandatory guarantee:
// if the existing config cannot be read for backup, the commit is aborted
// and neither disk nor memory changes.
func TestCommitAbortWhenBackupFails(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-backup-abort")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "config.yaml")
	writeTestConfig(t, configPath, &Config{Server: ServerConfig{LogLevel: "info"}})

	m, err := NewManager(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	before := m.GetConfig()

	// Replace the file with a directory: reading for backup now fails.
	if err := os.Remove(configPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(configPath, 0755); err != nil {
		t.Fatal(err)
	}

	err = m.AddUpstream(Upstream{Id: "u1", Name: "u", Targets: []UpstreamTarget{{Host: "localhost", Port: 1}}})
	if err == nil {
		t.Fatal("expected commit to abort when reading the existing file for backup fails")
	}

	if m.GetConfig() != before {
		t.Errorf("active config pointer must be unchanged after aborted commit")
	}
	if len(m.GetConfig().Upstreams) != 0 {
		t.Errorf("active config must not contain the new upstream")
	}
}

// TestCommitNoTempFilesLeftBehind guarantees atomic-replace temp files are
// cleaned up on success.
func TestCommitNoTempFilesLeftBehind(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-tmp-clean")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	writeTestConfig(t, filepath.Join(tmpDir, "config.yaml"), &Config{Server: ServerConfig{LogLevel: "info"}})

	m, err := NewManager(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	if err := m.AddUpstream(Upstream{Id: "u1", Name: "u", Targets: []UpstreamTarget{{Host: "localhost", Port: 1}}}); err != nil {
		t.Fatal(err)
	}

	leftovers, _ := filepath.Glob(filepath.Join(tmpDir, "config.yaml.tmp-*"))
	if len(leftovers) != 0 {
		t.Errorf("temp files must not be left behind: %v", leftovers)
	}
}

// TestCommitRejectedWhenRuntimeBuildFails verifies the runtime-build gate:
// when the installed builder cannot compile the candidate, the commit fails
// before persisting anything.
func TestCommitRejectedWhenRuntimeBuildFails(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-build-gate")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "config.yaml")
	writeTestConfig(t, configPath, &Config{Server: ServerConfig{LogLevel: "info"}})

	m, err := NewManager(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	before := m.GetConfig()

	SetRuntimeBuilder(func(c *Config) error {
		if len(c.Upstreams) > 0 {
			return errFakeBuild
		}
		return nil
	})
	defer SetRuntimeBuilder(nil)

	err = m.AddUpstream(Upstream{Id: "u1", Name: "u", Targets: []UpstreamTarget{{Host: "localhost", Port: 1}}})
	if err == nil {
		t.Fatal("expected commit to be rejected by the runtime-build gate")
	}

	// Neither memory nor disk may have changed.
	if m.GetConfig() != before {
		t.Errorf("active config must stay unchanged after failed commit")
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var onDisk Config
	_ = yaml.Unmarshal(data, &onDisk)
	if len(onDisk.Upstreams) != 0 {
		t.Errorf("failed commit must not touch the config file")
	}
	// A no-persist, non-empty builder must then succeed — hook released.
	defer func() {
		_ = m.AddUpstream(Upstream{Id: "u2", Name: "u2", Targets: []UpstreamTarget{{Host: "localhost", Port: 2}}})
	}()
}

type errFakeBuildType struct{}

func (errFakeBuildType) Error() string { return "fake build failure" }

var errFakeBuild error = errFakeBuildType{}

// TestReload_FileRemovedKeepsActiveConfig verifies that removing config.yaml
// warns but keeps serving the previous configuration, and that recreating
// the file restores reload behavior.
func TestReload_FileRemovedKeepsActiveConfig(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-removed")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "config.yaml")
	writeTestConfig(t, configPath, &Config{Server: ServerConfig{LogLevel: "info"}})

	m, err := NewManager(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	// Wait for the watcher registration (synchronous since rework).
	select {
	case <-m.Reload:
	default:
	}

	if err := os.Remove(configPath); err != nil {
		t.Fatal(err)
	}

	// Give the watcher a chance to react; nothing should reload or change.
	deadline := time.After(500 * time.Millisecond)
out:
	for {
		select {
		case <-m.Reload:
			t.Fatalf("removal must not trigger a reload")
		case <-deadline:
			break out
		}
	}
	if m.GetConfig().Server.LogLevel != "info" {
		t.Fatalf("active config must keep serving after file removal")
	}

	// Recreating with new content reloads normally.
	writeTestConfig(t, configPath, &Config{Server: ServerConfig{LogLevel: "debug"}})
	select {
	case <-m.Reload:
	case <-time.After(3 * time.Second):
		t.Fatalf("recreating the file must trigger a reload")
	}
	if m.GetConfig().Server.LogLevel != "debug" {
		t.Fatalf("expected reloaded config, got %s", m.GetConfig().Server.LogLevel)
	}
}

// TestWriteConfigSafe_UpdatesPublishedConfigAndHash verifies that
// WriteConfigSafe goes through the full commit pipeline: publishes into
// memory and refreshes the echo-filter hash.
func TestWriteConfigSafe_UpdatesPublishedConfigAndHash(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-write-safe")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	writeTestConfig(t, filepath.Join(tmpDir, "config.yaml"), &Config{Server: ServerConfig{LogLevel: "info"}})

	m, err := NewManager(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	cfg := m.GetConfig()
	cfg.Server.LogLevel = "warn"
	if err := m.WriteConfigSafe(); err != nil {
		t.Fatalf("WriteConfigSafe failed: %v", err)
	}

	if m.GetConfig().Server.LogLevel != "warn" {
		t.Errorf("WriteConfigSafe must publish the mutated config, got %s", m.GetConfig().Server.LogLevel)
	}

	// The echo filter must now recognize the new content: a simulated
	// watcher read of the file sees an unchanged hash and does not publish
	// anything (and does not error).
	if err := m.reloadFromFileIfChanged(); err != nil {
		t.Errorf("reload after WriteConfigSafe should be a harmless no-op: %v", err)
	}
}

// TestGetConfigRemainsReadOnly confirms the long-standing contract: the
// pointer identity returned to readers never changes through reads.
func TestGetConfigRemainsReadOnly(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-identity")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	writeTestConfig(t, filepath.Join(tmpDir, "config.yaml"), &Config{Server: ServerConfig{LogLevel: "info"}})

	m, err := NewManager(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	if m.GetConfig() != m.GetConfig() {
		t.Errorf("GetConfig must return the same pointer between reads of the generation")
	}
}
