package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"
)

// Add watcher to Manager struct
// (Removed - moved to config.go)

// NewManager creates a new Manager instance.
func NewManager(configDir string) (*Manager, error) {
	configPath := filepath.Join(configDir, "config.yaml")

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create watcher: %w", err)
	}

	m := &Manager{
		configPath: configPath,
		Reload:     make(chan struct{}, 1),
		watcher:    watcher,
		done:       make(chan struct{}),
	}

	// Load initial config
	if err := m.loadConfigFromFile(); err != nil {
		return nil, err
	}

	// Start watcher
	go m.watchConfig()

	return m, nil
}

// loadConfigFromFile reads, parses, and updates atomic config.
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

// watchConfig watches the DIRECTORY for file changes with debouncing.
func (m *Manager) watchConfig() {
	watcher := m.watcher
	defer watcher.Close()

	configDir := filepath.Dir(m.configPath)
	configFile := filepath.Base(m.configPath)

	// OPTIMASI 1: Pantau Direktori, bukan File.
	// Ini menangani "Atomic Save" (Rename/Replace) dari editor modern.
	if err := watcher.Add(configDir); err != nil {
		log.Error().Err(err).Str("dir", configDir).Msg("Failed to watch config directory")
		return
	}

	// Debounce timer - coalesce rapid events
	const debounceDelay = 100 * time.Millisecond
	var debounceTimer *time.Timer

	for {
		select {
		case <-m.done:
			return

		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			// Filter: Hanya peduli jika file target yang berubah
			if filepath.Base(event.Name) == configFile {
				// OPTIMASI 2: Tangkap event Rename/Chmod juga (karena atomic save)
				if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Rename) {

					// Reset debounce timer
					if debounceTimer != nil {
						debounceTimer.Stop()
					}

					debounceTimer = time.AfterFunc(debounceDelay, func() {
						log.Info().Str("file", configFile).Msg("Config changed detected, reloading...")

						if err := m.loadConfigFromFile(); err != nil {
							log.Error().Err(err).Msg("Failed to reload config")
						} else {
							// Notify reload (Non-blocking send)
							select {
							case m.Reload <- struct{}{}:
							default:
							}
							log.Info().Msg("Config reloaded successfully")
						}
					})
				}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Error().Err(err).Msg("Watcher error")
		}
	}
}

// Helper Internal: Backup dan Tulis File (Menghindari Duplikasi Kode)
// Fungsi ini tidak menggunakan Lock sendiri, caller harus hold Lock.
func (m *Manager) backupAndWriteFileLocked(data []byte) error {
	// 1. Backup file lama jika ada
	if _, err := os.Stat(m.configPath); err == nil {
		backup := fmt.Sprintf("%s.bak-%d", m.configPath, time.Now().Unix())
		oldData, err := os.ReadFile(m.configPath)
		if err == nil {
			_ = os.WriteFile(backup, oldData, 0644)
		}
	}

	// 2. Tulis file baru
	if err := os.WriteFile(m.configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}
	return nil
}

// WriteConfigSafe is a public wrapper if needed by other components
// Note: This assumes m.config is already modified by caller.
// But wait, callers (AddRoute, etc) hold the lock.
// This function needs to be careful not to double lock if used internally.
// BETTER: Use saveAndReloadLocked for internal use. This is just for "Save Current State".
func (m *Manager) WriteConfigSafe() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := yaml.Marshal(m.config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	return m.backupAndWriteFileLocked(data)
}

// saveAndReloadLocked saves changes to file and updates atomic config.
// Callers must ALREADY HOLD m.mu.
func (m *Manager) saveAndReloadLocked() error {
	// 1. Marshal current config
	data, err := yaml.Marshal(m.config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// 2. Backup & Write (DRY: Pakai helper)
	if err := m.backupAndWriteFileLocked(data); err != nil {
		return err
	}

	// 3. Create fresh copy specifically for atomic (decodes from bytes to ensure clean state)
	var newCfg Config
	if err := yaml.Unmarshal(data, &newCfg); err != nil {
		return fmt.Errorf("failed to unmarshal just-saved config: %w", err)
	}

	// 4. Update memory pointers
	m.atomicConfig.Store(&newCfg) // Untuk Proxy (Lock-free)
	m.config = &newCfg            // Untuk CRUD selanjutnya

	// 5. Notify
	select {
	case m.Reload <- struct{}{}:
	default:
	}

	return nil
}
