package config

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"
)

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
		_ = watcher.Close()
		return nil, err
	}

	// Start watcher
	m.wg.Add(1)
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
	m.lastHash = sha256.Sum256(data)
	m.rebuildIndexesLocked()
	return nil
}

// reloadFromFileIfChanged checks if file content actually changed before reloading.
// This prevents redundant echo reloads when Admin API writes to the file.
func (m *Manager) reloadFromFileIfChanged() error {
	data, err := os.ReadFile(m.configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	newHash := sha256.Sum256(data)

	m.mu.Lock()
	defer m.mu.Unlock()

	// Content unchanged -> skip reload (Echo feedback prevention)
	if newHash == m.lastHash {
		log.Debug().Msg("Config file touched but content identical, skipping reload")
		return nil
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	m.config = &cfg
	m.atomicConfig.Store(&cfg)
	m.lastHash = newHash
	m.rebuildIndexesLocked()

	// Notify reload (Non-blocking send)
	select {
	case m.Reload <- struct{}{}:
	default:
	}

	log.Info().Msg("Config reloaded successfully from external file change")
	return nil
}

// watchConfig watches the DIRECTORY for file changes with debouncing.
func (m *Manager) watchConfig() {
	defer m.wg.Done()

	configDir := filepath.Dir(m.configPath)
	configFile := filepath.Base(m.configPath)

	// Pantau Direktori, bukan File (menangani atomic save editor)
	if err := m.watcher.Add(configDir); err != nil {
		log.Error().Err(err).Str("dir", configDir).Msg("Failed to watch config directory")
		return
	}

	const debounceDelay = 100 * time.Millisecond
	var debounceTimer *time.Timer
	var debounceC <-chan time.Time

	// Pastikan timer di-stop saat goroutine selesai untuk mencegah leak
	defer func() {
		if debounceTimer != nil {
			debounceTimer.Stop()
		}
	}()

	for {
		select {
		case <-m.done:
			return

		case event, ok := <-m.watcher.Events:
			if !ok {
				return
			}

			// Filter: Hanya peduli jika file target yang berubah
			if filepath.Base(event.Name) == configFile {
				if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Rename) {
					if debounceTimer == nil {
						debounceTimer = time.NewTimer(debounceDelay)
						debounceC = debounceTimer.C
					} else {
						if !debounceTimer.Stop() {
							select {
							case <-debounceTimer.C:
							default:
							}
						}
						debounceTimer.Reset(debounceDelay)
					}
				}
			}

		case <-debounceC:
			debounceTimer = nil
			debounceC = nil

			if err := m.reloadFromFileIfChanged(); err != nil {
				log.Error().Err(err).Msg("Failed to reload config from file")
			}

		case err, ok := <-m.watcher.Errors:
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
func (m *Manager) WriteConfigSafe() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := yaml.Marshal(m.config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	return m.backupAndWriteFileLocked(data)
}

// commitConfigLocked persists the new config and atomically publishes it.
// The old m.config pointer stays valid for any in-flight readers until GC.
// Callers must ALREADY HOLD m.mu.
func (m *Manager) commitConfigLocked(newCfg *Config) error {
	data, err := yaml.Marshal(newCfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := m.backupAndWriteFileLocked(data); err != nil {
		return err
	}

	// Update hash to prevent watchConfig echo reload
	m.lastHash = sha256.Sum256(data)

	// Atomic publish — old *Config remains valid for existing readers
	m.config = newCfg
	m.atomicConfig.Store(newCfg)
	m.rebuildIndexesLocked()

	// Notify proxy
	select {
	case m.Reload <- struct{}{}:
	default:
	}

	return nil
}

// saveAndReloadLocked persists current m.config. Callers must ALREADY HOLD m.mu.
func (m *Manager) saveAndReloadLocked() error {
	return m.commitConfigLocked(m.config)
}
