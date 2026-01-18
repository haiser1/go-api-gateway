package config

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v3"
)

// NewManager creates a new Manager instance.
func NewManager(configDir string) (*Manager, error) {
	configPath := filepath.Join(configDir, "config.yaml")

	m := &Manager{
		configPath: configPath,
		Reload:     make(chan struct{}, 1),
	}

	// Load initial config
	if err := m.loadConfigFromFile(); err != nil {
		return nil, err
	}

	// Start watcher
	go m.watchConfig()

	return m, nil
}

// loadConfigFromFile reads and parses the config file.
// It updates both m.config (locked) and m.atomicConfig.
func (m *Manager) loadConfigFromFile() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := os.ReadFile(m.configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	m.config = &cfg
	m.atomicConfig.Store(&cfg)
	return nil
}

// watchConfig watches for file changes.
func (m *Manager) watchConfig() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("Failed to create file watcher: %v", err)
		return
	}
	defer watcher.Close()

	if err := watcher.Add(m.configPath); err != nil {
		// Fallback: observe directory if file watch fails (rare) or if file doesn't exist initially?
		// But loadConfigFromFile succeeded, so file exists.
		// Some editors delete and recreate files (atomic save), so watching dir is safer?
		// Let's watch the directory instead.
		configDir := filepath.Dir(m.configPath)
		if errDir := watcher.Add(configDir); errDir != nil {
			log.Printf("Failed to watch config directory: %v", errDir)
			return
		}
	}

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			// Check if it's our config file
			if filepath.Base(event.Name) == filepath.Base(m.configPath) {
				if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
					log.Println("Config file changed:", event.Name)
					// Small delay to ensure write is complete (sometimes helpful with some editors)
					time.Sleep(50 * time.Millisecond)

					if err := m.loadConfigFromFile(); err != nil {
						log.Printf("Failed to reload config: %v", err)
					} else {
						// Notify reload
						select {
						case m.Reload <- struct{}{}:
						default:
						}
					}
				}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Printf("Watcher error: %v", err)
		}
	}
}

// WriteConfigSafe backs up the config file and writes the new config.
// Callers must hold m.mu (implied because they edit m.config).
func (m *Manager) WriteConfigSafe() error {
	// Helper to write bytes to file with backup
	data, err := yaml.Marshal(m.config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if _, err := os.Stat(m.configPath); err == nil {
		backup := fmt.Sprintf("%s.bak-%d", m.configPath, time.Now().Unix())
		// Read old file to backup (don't rely on m.config for backup, backup what's on disk)
		oldData, err := os.ReadFile(m.configPath)
		if err == nil {
			_ = os.WriteFile(backup, oldData, 0644)
		}
	}

	if err := os.WriteFile(m.configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}
	return nil
}

// saveAndReloadLocked saves changes to file and updates atomic config.
// Callers must ALREADY HOLD m.mu.
func (m *Manager) saveAndReloadLocked() error {
	// 1. Marshal current config (which was modified by caller)
	data, err := yaml.Marshal(m.config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// 2. Backup
	if _, err := os.Stat(m.configPath); err == nil {
		backup := fmt.Sprintf("%s.bak-%d", m.configPath, time.Now().Unix())
		oldData, _ := os.ReadFile(m.configPath)
		_ = os.WriteFile(backup, oldData, 0644)
	}

	// 3. Write to file
	if err := os.WriteFile(m.configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	// 4. Create fresh copy specifically for atomic (decodes from bytes to ensure clean state)
	var newCfg Config
	if err := yaml.Unmarshal(data, &newCfg); err != nil {
		return fmt.Errorf("failed to unmarshal just-saved config: %w", err)
	}

	// Update atomic
	m.atomicConfig.Store(&newCfg)

	// Update m.config to point to newCfg as well
	m.config = &newCfg

	// 5. Notify
	select {
	case m.Reload <- struct{}{}:
	default:
	}

	return nil
}
