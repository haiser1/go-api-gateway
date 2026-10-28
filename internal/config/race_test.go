package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// TestManager_ConcurrentReadWriteRace tests concurrent reads and writes under go test -race.
// It verifies that readers never see corrupted memory, half-written structs,
// or slice reallocation panics while writers perform copy-on-write mutations.
func TestManager_ConcurrentReadWriteRace(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-race-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "config.yaml")
	initialCfg := Config{
		Server: ServerConfig{ProxyPort: 8080, AdminPort: 8081, LogLevel: "info"},
		Upstreams: []Upstream{
			{Id: "u1", Name: "upstream1", Algorithm: "round-robin", Targets: []UpstreamTarget{{Host: "localhost", Port: 8080}}},
		},
		Services: []Service{
			{Id: "s1", Name: "service1", UpstreamId: "u1"},
		},
		Routes: []Route{
			{Id: "r1", Name: "route1", ServiceId: "s1", Methods: []string{"GET"}, Paths: []string{"/api/v1"}},
		},
	}
	data, _ := yaml.Marshal(initialCfg)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	m, err := NewManager(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	stopCh := make(chan struct{})
	var wg sync.WaitGroup

	// 10 Concurrent Readers: simulating high-throughput proxy requests
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()
			for {
				select {
				case <-stopCh:
					return
				default:
					cfg := m.GetConfig()
					if cfg == nil {
						t.Errorf("reader %d got nil config", readerID)
						return
					}

					// Read and iterate all slices (must not panic from slice reallocation)
					for _, r := range cfg.Routes {
						_ = r.Id
						_ = r.Name
						for _, m := range r.Methods {
							_ = m
						}
						for _, p := range r.Paths {
							_ = p
						}
					}

					for _, u := range cfg.Upstreams {
						_ = u.Id
						_ = u.Name
						for _, tgt := range u.Targets {
							_ = tgt.Host
							_ = tgt.Port
						}
					}

					for _, s := range cfg.Services {
						_ = s.Id
						_ = s.Name
						_ = s.UpstreamId
					}

					for _, p := range cfg.GlobalPlugins {
						_ = p.Name
						_ = p.Enabled
					}
				}
			}
		}(i)
	}

	// 5 Concurrent Writers: simulating admin API operations
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()
			iter := 0
			for {
				select {
				case <-stopCh:
					return
				default:
					iter++
					routeID := fmt.Sprintf("r-dyn-%d-%d", writerID, iter%5)
					routeName := fmt.Sprintf("route-dyn-%d-%d", writerID, iter%5)
					routePath := fmt.Sprintf("/dyn/%d/%d", writerID, iter%5)

					// Add route
					_ = m.AddRoute(Route{
						Id:        routeID,
						Name:      routeName,
						ServiceId: "s1",
						Methods:   []string{"GET"},
						Paths:     []string{routePath},
					})

					// Update route
					_ = m.UpdateRoute(routeID, Route{
						Id:        routeID,
						Name:      routeName,
						ServiceId: "s1",
						Methods:   []string{"POST"},
						Paths:     []string{routePath},
					})

					// Delete route
					_ = m.DeleteRoute(routeID)

					// Sleep briefly to yield CPU
					time.Sleep(1 * time.Millisecond)
				}
			}
		}(i)
	}

	// Run stress test for 600ms
	time.Sleep(600 * time.Millisecond)
	close(stopCh)
	wg.Wait()
}

// TestManager_ConcurrentClose verifies that calling Close() simultaneously
// from multiple goroutines is safe and does NOT panic with "close of closed channel".
func TestManager_ConcurrentClose(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-close-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "config.yaml")
	data, _ := yaml.Marshal(Config{Server: ServerConfig{LogLevel: "info"}})
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	m, err := NewManager(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	// 20 Goroutines simultaneously closing the manager
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = m.Close()
		}()
	}
	wg.Wait()
}

// TestManager_Debounce_CoalesceRapidEvents verifies that rapid burst writes to config.yaml
// are coalesced into a single reload event.
func TestManager_Debounce_CoalesceRapidEvents(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-debounce-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "config.yaml")
	data, _ := yaml.Marshal(Config{Server: ServerConfig{LogLevel: "info"}})
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	m, err := NewManager(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	// Wait for watcher to start
	time.Sleep(100 * time.Millisecond)

	// Rapidly write 5 changes within 25ms (< 100ms debounce delay)
	for i := 1; i <= 5; i++ {
		newCfg := Config{Server: ServerConfig{LogLevel: fmt.Sprintf("debug-%d", i)}}
		newData, _ := yaml.Marshal(newCfg)
		_ = os.WriteFile(configPath, newData, 0644)
		time.Sleep(5 * time.Millisecond)
	}

	// Should receive exactly 1 reload notification
	reloadCount := 0
	timeout := time.After(400 * time.Millisecond)

loop:
	for {
		select {
		case <-m.Reload:
			reloadCount++
		case <-timeout:
			break loop
		}
	}

	if reloadCount != 1 {
		t.Errorf("expected exactly 1 coalesced reload, got %d", reloadCount)
	}
	if m.GetConfig().Server.LogLevel != "debug-5" {
		t.Errorf("expected latest log level debug-5, got %s", m.GetConfig().Server.LogLevel)
	}
}

// TestManager_EchoReloadPrevention verifies that internal Admin writes (which write to disk)
// do NOT trigger an echo reload from the watcher because the SHA-256 content checksum matches.
func TestManager_EchoReloadPrevention(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-echo-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "config.yaml")
	initialCfg := Config{
		Server: ServerConfig{LogLevel: "info"},
		Upstreams: []Upstream{
			{Id: "u1", Name: "upstream1"},
		},
		Services: []Service{
			{Id: "s1", Name: "service1", UpstreamId: "u1"},
		},
	}
	data, _ := yaml.Marshal(initialCfg)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	m, err := NewManager(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	// Wait for watcher to be ready
	time.Sleep(100 * time.Millisecond)

	// Add route via Admin API / Manager method (this writes to disk)
	err = m.AddRoute(Route{
		Id:        "r-echo",
		Name:      "route-echo",
		ServiceId: "s1",
		Methods:   []string{"GET"},
		Paths:     []string{"/echo"},
	})
	if err != nil {
		t.Fatalf("AddRoute failed: %v", err)
	}

	// 1st reload is expected from in-memory commit
	select {
	case <-m.Reload:
		// OK - commit notification received
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for in-memory commit reload")
	}

	// Now wait for 300ms (3x debounce window).
	// If echo reload prevention works, watchConfig will see identical hash and skip reload!
	select {
	case <-m.Reload:
		t.Error("unexpected echo reload received from watcher!")
	case <-time.After(300 * time.Millisecond):
		// SUCCESS: No second reload fired!
	}
}

// TestManager_Close_StopsDebounceTimer verifies that closing Manager cancels pending
// debounce timers so no orphan reload callback executes after shutdown.
func TestManager_Close_StopsDebounceTimer(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-timer-stop-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "config.yaml")
	data, _ := yaml.Marshal(Config{Server: ServerConfig{LogLevel: "info"}})
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	m, err := NewManager(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(100 * time.Millisecond)

	// Trigger a change
	newData, _ := yaml.Marshal(Config{Server: ServerConfig{LogLevel: "debug"}})
	_ = os.WriteFile(configPath, newData, 0644)

	// Immediately close Manager before 100ms debounce fires (at 15ms)
	time.Sleep(15 * time.Millisecond)
	if err := m.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Wait 250ms past debounce window
	time.Sleep(250 * time.Millisecond)

	// Drain reload channel - should be empty
	select {
	case <-m.Reload:
		t.Error("unexpected reload callback ran after Close()")
	default:
		// SUCCESS
	}
}
