// Package config_test holds integration tests that require the centralized
// validator to be wired into the config Manager — i.e., package
// internal/config/validator imported. That import is impossible inside
// package config's own test files (import cycle), so they live here.
package manager_integration_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/haiser1/go-api-gateway/internal/config"
	"github.com/haiser1/go-api-gateway/internal/config/validator"
	"gopkg.in/yaml.v3"
)

// mustManager creates a Manager over tmpDir with a valid base config.
func validYAML(t *testing.T) ([]byte, string) {
	t.Helper()
	base := config.Config{
		Server: config.ServerConfig{ProxyPort: 8080, AdminPort: 8081, LogLevel: "info"},
		Upstreams: []config.Upstream{{
			Id:      "up-1",
			Name:    "backend",
			Targets: []config.UpstreamTarget{{Host: "127.0.0.1", Port: 9090, Weight: 100}},
		}},
		Services: []config.Service{{
			Id:         "svc-1",
			Name:       "api",
			UpstreamId: "up-1",
			Protocol:   "http",
		}},
		Routes: []config.Route{{
			Id:        "rt-1",
			Name:      "root",
			Methods:   []string{"GET"},
			Paths:     []string{"/api"},
			ServiceId: "svc-1",
		}},
	}
	data, err := yaml.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	return data, string(data)
}

func TestNewManager_RejectsInvalidConfig(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mgr-validate")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Duplicate route IDs must trip the semantic validator.
	invalid := config.Config{
		Server: config.ServerConfig{ProxyPort: 8080, AdminPort: 8081, LogLevel: "info"},
		Routes: []config.Route{
			{Id: "rt-1", Name: "a", Methods: []string{"GET"}, Paths: []string{"/a"}, ServiceId: "svc-1"},
			{Id: "rt-1", Name: "b", Methods: []string{"GET"}, Paths: []string{"/b"}, ServiceId: "svc-1"},
		},
	}
	data, _ := yaml.Marshal(invalid)
	if err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), data, 0644); err != nil {
		t.Fatal(err)
	}

	_, err = config.NewManager(tmpDir)
	if err == nil {
		t.Fatal("NewManager must reject configs failing semantic validation")
	}
	t.Logf("rejection reason: %v", err)
}

func TestNewManager_RejectsStrictUnknownField(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mgr-strict")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	typo := `server:
  proxy_port: 8080
  admin_port: 8081
  log_level: info
  log_lvl: info
`
	if err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(typo), 0644); err != nil {
		t.Fatal(err)
	}

	_, err = config.NewManager(tmpDir)
	if err == nil {
		t.Fatal("strict decode must reject unknown fields")
	}
}

func TestNewManager_RejectsEmptyFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mgr-empty")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	if err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := config.NewManager(tmpDir); err == nil {
		t.Fatal("an empty config file must not silently publish a zero config")
	}
}

func TestReload_KeepsActiveConfigWhenFileInvalid(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mgr-reload-guard")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "config.yaml")
	base, _ := validYAML(t)
	if err := os.WriteFile(configPath, []byte(base), 0644); err != nil {
		t.Fatal(err)
	}

	m, err := config.NewManager(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	// Corrupt the file (semantic break).
	broken := config.Config{
		Server: config.ServerConfig{ProxyPort: 8080, AdminPort: 8081, LogLevel: "info"},
		Services: []config.Service{{
			Id:         "svc-1",
			Name:       "api",
			UpstreamId: "does-not-exist",
			Protocol:   "http",
		}},
	}
	brokenData, _ := yaml.Marshal(broken)
	if err := os.WriteFile(configPath, brokenData, 0644); err != nil {
		t.Fatal(err)
	}

	// The watcher reload must reject the broken file: no reload signal and
	// no config change within the observation window.
	select {
	case <-m.Reload:
		t.Fatal("invalid file must not publish nor signal a reload")
	case <-time.After(1500 * time.Millisecond):
	}
	if m.GetConfig().Server.AdminPort != 8081 || len(m.GetConfig().Services) != 1 {
		t.Fatalf("active config must be untouched after a failed reload: %+v", m.GetConfig())
	}
	if m.GetConfig().Services[0].UpstreamId != "up-1" {
		t.Fatalf("active service must still reference the valid upstream")
	}

	// Fix the file: the next watcher cycle must reload cleanly (the failed
	// attempt must not have poisoned the echo-filter hash).
	fixed := config.Config{Server: config.ServerConfig{ProxyPort: 8080, AdminPort: 8081, LogLevel: "debug"}}
	fixedData, _ := yaml.Marshal(fixed)
	if err := os.WriteFile(configPath, fixedData, 0644); err != nil {
		t.Fatal(err)
	}
	select {
	case <-m.Reload:
	case <-time.After(3 * time.Second):
		t.Fatal("corrected file must reload cleanly")
	}
	if m.GetConfig().Server.LogLevel != "debug" {
		t.Fatalf("expected the corrected config to be active, got %s", m.GetConfig().Server.LogLevel)
	}
}

func TestManager_CRUDValidatesCandidates(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mgr-crud-validate")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	base, _ := validYAML(t)
	if err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(base), 0644); err != nil {
		t.Fatal(err)
	}

	m, err := config.NewManager(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	// Add a service with an unknown upstream reference: semantic validation
	// must reject the candidate before persistence.
	err = m.AddService(config.Service{Id: "svc-bad", Name: "bad", UpstreamId: "ghost"})
	if err == nil {
		t.Fatal("AddService with unknown upstream_id must be rejected by centralized validation")
	}

	if len(m.GetConfig().Services) != 1 {
		t.Fatal("rejected service must not be published")
	}

	// plugin-name validation is lenient until the gateway registers any
	// plugin implementation; contract errors still apply/enforced.
	err = m.AddGlobalPlugin(config.PluginConfig{
		Name:    "rate-limiting",
		Enabled: true,
		Config:  map[string]interface{}{"requests_per_minute": "fast"},
	})
	if err == nil {
		t.Fatal("plugin contract violations must be rejected")
	}
	if len(m.GetConfig().GlobalPlugins) != 0 {
		t.Fatal("rejected plugin must not be published")
	}

	// The known name leniency must not reject correct configs.
	if err := m.AddGlobalPlugin(config.PluginConfig{
		Name:    "rate-limiting",
		Enabled: true,
		Config:  map[string]interface{}{"requests_per_minute": 60},
	}); err != nil {
		t.Fatalf("valid plugin config must be accepted: %v", err)
	}
}

func TestFullConfig_ValidatorsExposeNoErrorsForRepoConfig(t *testing.T) {
	// The real repo config must keep passing validation (guards regressions
	// in the strict decode and semantic layers).
	data, err := os.ReadFile(filepath.Join("..", "..", "configs", "config.yaml"))
	if err != nil {
		t.Skip("repo config.yaml not found, skipping")
	}
	// NOTE: decodeStrict is package-private; this guard is covered by the
	// NewManager-based check in TestValidateAgainstLoadersRepoConfig via a
	// temp copy of the file.
	tmpDir, err := os.MkdirTemp("", "mgr-repo")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)
	if err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), data, 0644); err != nil {
		t.Fatal(err)
	}
	m, err := config.NewManager(tmpDir)
	if err != nil {
		t.Fatalf("repo config.yaml must pass strict decode + validation + build: %v", err)
	}
	defer m.Close()

	// Cross-check the validator directly (aggregated, no Manager).
	var typed config.Config
	if err := yaml.Unmarshal(data, &typed); err != nil {
		t.Fatal(err)
	}
	if errs := validator.Validate(&typed); len(errs) != 0 {
		t.Fatalf("validator.Validate disagrees with Manager for repo config: %v", errs)
	}
}

// TestValidatorAggregateErrorRendering documents the aggregated error type on
// the exact code path used by HTTP handlers.
func TestValidatorAggregateErrorRendering(t *testing.T) {
	_, _ = validYAML(t)
	errs := validator.ValidationErrors{
		{Field: "Routes[0].Methods[0]", Code: validator.CodeInvalidValue, Message: "\"brew\" is not a supported HTTP method"},
	}
	if len(errs.Error()) == 0 {
		t.Fatal("aggregate error must render a message")
	}
}

// svcConfigYAML marshals a valid base config (same shape as validYAML) whose
// only GET route serves the given path — used by the concurrency storm to
// generate file reloads.
func svcConfigYAML(t *testing.T, path string) []byte {
	t.Helper()
	base := config.Config{
		Server: config.ServerConfig{ProxyPort: 8080, AdminPort: 8081, LogLevel: "info"},
		Upstreams: []config.Upstream{{
			Id:      "up-1",
			Name:    "backend",
			Targets: []config.UpstreamTarget{{Host: "127.0.0.1", Port: 9090, Weight: 1}},
		}},
		Services: []config.Service{{
			Id:         "svc-1",
			Name:       "api",
			UpstreamId: "up-1",
			Protocol:   "http",
		}},
		Routes: []config.Route{{
			Id:        "rt-1",
			Name:      "root",
			Methods:   []string{"GET"},
			Paths:     []string{path},
			ServiceId: "svc-1",
		}},
	}
	data, err := yaml.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// Phase 9 concurrency coverage: the config watcher (file reloads) and admin
// API mutations (UpdateService) hammer the same Manager at the same time
// while readers sample GetConfig. The gate is -race + no deadlocks +
// GetConfig never nil: individual admin mutations may legitimately conflict
// with concurrent reloads (validator rejections count as handled).
func TestManager_WatcherAndAdminUpdatesConcurrently(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mgr-race")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, svcConfigYAML(t, "/api"), 0644); err != nil {
		t.Fatal(err)
	}

	mgr, err := config.NewManager(tmpDir)
	if err != nil {
		t.Fatalf("manager start failed: %v", err)
	}
	defer mgr.Close()

	stop := make(chan struct{})
	var wg sync.WaitGroup
	var accepted, rejected, readNil atomic.Int64

	// 1. File watcher flanker: periodically rewrite the file with a valid
	//    variant, triggering real reloads.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			variant := svcConfigYAML(t, fmt.Sprintf("/api/variant-%d", i%8))
			if writeErr := os.WriteFile(configPath, variant, 0644); writeErr != nil {
				return
			}
			time.Sleep(15 * time.Millisecond)
		}
	}()

	// 2. Admin API writer: steady UpdateService mutations.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			svc := config.Service{
				Id: "svc-1", Name: fmt.Sprintf("api-%d", i%4), UpstreamId: "up-1", Protocol: "http",
				Retries: i % 3,
			}
			if updErr := mgr.UpdateService("svc-1", svc); updErr == nil {
				accepted.Add(1)
			} else {
				rejected.Add(1) // conflicts with concurrent reloads are acceptable
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()

	// 3. Readers: GetConfig must always return a non-nil, readable snapshot.
	wg.Add(2)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if mgr.GetConfig() == nil {
				readNil.Add(1)
			}
		}
	}()
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if cfg := mgr.GetConfig(); cfg != nil {
				_ = cfg.Services
				_ = cfg.Upstreams
			}
		}
	}()

	time.Sleep(700 * time.Millisecond)
	close(stop)
	wg.Wait()
	mgr.Close()

	if readNil.Load() != 0 {
		t.Fatal("GetConfig returned nil during concurrent watcher/admin activity")
	}
	t.Logf("admin ops: %d accepted, %d rejected under concurrent reloads",
		accepted.Load(), rejected.Load())

	// After the storm the manager must still serve a healthy config.
	final := mgr.GetConfig()
	if final == nil || len(final.Services) == 0 {
		t.Fatal("manager left in an unusable state after concurrent updates")
	}
}
