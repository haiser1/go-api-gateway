package config

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
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

	// Register the directory watch SYNCHRONOUSLY before returning: any write
	// landing after NewManager is guaranteed to be observed. (Previously the
	// Add raced with the consumer goroutine start, so a write immediately
	// after construction could be missed.)
	if err := watcher.Add(filepath.Dir(configPath)); err != nil {
		_ = watcher.Close()
		return nil, fmt.Errorf("failed to watch config directory: %w", err)
	}

	// Start watcher
	m.wg.Add(1)
	go m.watchConfig()

	return m, nil
}

// decodeStrict parses configuration data with strict YAML validation:
// unknown fields are rejected, so a typo (e.g. "globa_plugins") fails loudly
// instead of silently dropping a configuration section.
func decodeStrict(data []byte) (*Config, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("config file is empty")
		}
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}
	return &cfg, nil
}

// loadConfigFromFile reads, parses, validates, and publishes the initial config.
func (m *Manager) loadConfigFromFile() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := os.ReadFile(m.configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	cfg, err := decodeStrict(data)
	if err != nil {
		return err
	}

	// Centralized validation (when the validator package is wired): the
	// initial load applies the same rules as reloads and admin commits.
	if err := m.validateAndBuildLocked(cfg); err != nil {
		return err
	}

	m.config = cfg
	m.atomicConfig.Store(cfg)
	m.lastHash = sha256.Sum256(data)
	m.rebuildIndexesLocked()
	return nil
}

// reloadFromFileIfChanged checks if file content actually changed before
// reloading. The file is read WHILE HOLDING the manager lock so a concurrent
// admin commit (marshal + persist + hash update, also under the lock) can
// never interleave: a torn read pairing disk contents with a stale hash —
// and the echo-reload it caused — becomes impossible.
func (m *Manager) reloadFromFileIfChanged() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := os.ReadFile(m.configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// File was removed (editor cleanup / transient atomic-replace
			// state): warn once and keep serving the current configuration
			// instead of throwing the gateway into an unrecoverable state.
			if m.warnedFileRemoved {
				return nil
			}
			m.warnedFileRemoved = true
			log.Warn().Str("path", m.configPath).
				Msg("Config file is currently missing; keeping the active configuration")
			return nil
		}
		return fmt.Errorf("failed to read config file: %w", err)
	}
	m.warnedFileRemoved = false

	newHash := sha256.Sum256(data)

	// Content unchanged -> skip reload (Echo feedback prevention)
	if newHash == m.lastHash {
		log.Debug().Msg("Config file touched but content identical, skipping reload")
		return nil
	}

	cfg, err := decodeStrict(data)
	if err != nil {
		return err
	}

	// Centralized validation + runtime-build gate: an invalid or
	// un-buildable file NEVER replaces the active configuration and never
	// updates lastHash, so the next real correction reloads immediately.
	if err := m.validateAndBuildLocked(cfg); err != nil {
		log.Error().Err(err).Msg("Rejected invalid config file; keeping previous configuration")
		return err
	}

	m.config = cfg
	m.atomicConfig.Store(cfg)
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
			if filepath.Base(event.Name) == filepath.Base(m.configPath) {
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
//
// Semantics:
//  1. Backup of the existing file is MANDATORY: if the old file exists and
//     it cannot be read or the backup cannot be written, the commit is
//     ABORTED so no configuration can be lost.
//  2. The new content is written atomically: temp file in the same
//     directory, fsync, rename over the target, best-effort directory
//     fsync. Readers of the file always observe either the old or the new
//     complete contents, never a torn write.
func (m *Manager) backupAndWriteFileLocked(data []byte) error {
	if old, err := os.ReadFile(m.configPath); err == nil {
		backup := fmt.Sprintf("%s.bak-%d", m.configPath, time.Now().Unix())
		if err := os.WriteFile(backup, old, 0644); err != nil {
			return fmt.Errorf("failed to create config backup %q, aborting commit: %w", backup, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to read existing config for backup, aborting commit: %w", err)
	}

	dir := filepath.Dir(m.configPath)
	tmpFile, err := os.CreateTemp(dir, "config.yaml.tmp-*")
	if err != nil {
		return fmt.Errorf("failed to create temp config file: %w", err)
	}
	tmpPath := tmpFile.Name()
	cleanupOnPanic := true
	defer func() {
		if cleanupOnPanic {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to write temp config: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to flush temp config: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp config: %w", err)
	}
	if err := os.Chmod(tmpPath, 0644); err != nil {
		return fmt.Errorf("failed to set config file permissions: %w", err)
	}
	if err := os.Rename(tmpPath, m.configPath); err != nil {
		return fmt.Errorf("failed to replace config file: %w", err)
	}
	cleanupOnPanic = false

	// Best-effort: flush the directory entry so the rename survives a crash.
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// WriteConfigSafe is a public wrapper if needed by other components.
// It runs the full commit pipeline — validate, runtime-build gate, backup,
// atomic persist, publish — for the CURRENT in-memory configuration, and
// keeps lastHash in sync (previously skipped, which could poison the echo
// filter after direct mutations).
func (m *Manager) WriteConfigSafe() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.commitConfigLocked(m.config)
}

// commitConfigLocked runs the full persistence pipeline for a candidate
// config, in this order:
//  1. validate + build the runtime candidate (an invalid candidate never
//     touches disk or the runtime),
//  2. backup + atomically persist to the config file,
//  3. update the echo-reload hash,
//  4. atomically publish into memory and rebuild indexes,
//  5. notify the proxy.
//
// The old config pointer stays valid for any in-flight readers until GC.
// Callers must ALREADY HOLD m.mu.
func (m *Manager) commitConfigLocked(newCfg *Config) error {
	if err := m.validateAndBuildLocked(newCfg); err != nil {
		return err
	}

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
