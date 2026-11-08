package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/haiser1/go-api-gateway/internal/auth"
	"github.com/haiser1/go-api-gateway/internal/config"
	"github.com/haiser1/go-api-gateway/internal/secrets"
	"github.com/julienschmidt/httprouter"
	"github.com/rs/zerolog/log"
)

type Server struct {
	proxyServer *http.Server
	adminServer *http.Server
	cfgManager  *config.Manager
	proxy       *Proxy
	secretPath  string
	reloadMu    sync.Mutex

	// stopCh is closed at the very start of Shutdown so the config-reload
	// goroutine exits promptly instead of blocking on the reload channel.
	stopCh   chan struct{}
	stopOnce sync.Once
}

// InstallRuntimeBuilder wires the runtime-build gate into the config
// manager: admin CRUD commits and file reloads compile a runtime candidate
// BEFORE their changes are persisted or published. A change the runtime
// cannot build (unknown plugin, invalid target, conflicting routes, ...)
// is rejected outright.
func installRuntimeBuilder(provider secrets.Provider) {
	config.SetRuntimeBuilder(func(cfg *config.Config) error {
		_, err := buildRuntimeSnapshotWithProvider(cfg, provider)
		return err
	})
}

func NewServer(cfgManager *config.Manager) (*Server, error) {
	cfg := cfgManager.GetConfig()

	// Secrets live next to config.yaml unless explicitly overridden. Opening
	// the provider does not create the file when auth is not configured.
	secretPath := os.Getenv(secrets.EnvFileEnvVar)
	if secretPath == "" {
		secretPath = filepath.Join(filepath.Dir(cfgManager.ConfigPath()), ".env")
	}
	provider, err := secrets.OpenEnvFileProvider(secretPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open secret provider: %w", err)
	}

	// Gate the runtime builder in once, before the first build.
	installRuntimeBuilder(provider)

	// === Proxy Server (Data Plane) ===
	proxy, err := NewProxyWithSecrets(cfg, provider)
	if err != nil {
		return nil, fmt.Errorf("failed to build initial runtime: %w", err)
	}

	proxyPort := cfg.Server.ProxyPort
	if proxyPort == 0 {
		proxyPort = 8080
	}

	// === Management Server (Control Plane) ===
	adminAuthenticator, err := loadAdminAuthenticator(provider)
	if err != nil {
		return nil, fmt.Errorf("failed to load admin credentials: %w", err)
	}
	if adminAuthenticator == nil {
		log.Warn().Msg("Management API is disabled: configure GATEWAY_ADMIN_TOKEN or ADMIN_TOKEN/ADMIN_TOKEN_HASH in the secret file")
	}

	adminRouter := httprouter.New()
	RegisterManagementRoutes(adminRouter, cfgManager, provider)
	adminHandler := auth.AdminMiddleware(adminAuthenticator)(adminRouter)

	adminPort := cfg.Server.AdminPort
	if adminPort == 0 {
		adminPort = 8081
	}

	s := &Server{
		cfgManager: cfgManager,
		proxy:      proxy,
		secretPath: secretPath,
		proxyServer: &http.Server{
			Addr:         fmt.Sprintf(":%d", proxyPort),
			Handler:      http.HandlerFunc(proxy.ServeHTTP),
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 10 * time.Second,
		},
		adminServer: &http.Server{
			Addr:         fmt.Sprintf(":%d", adminPort),
			Handler:      adminHandler,
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 10 * time.Second,
		},
		stopCh: make(chan struct{}),
	}
	return s, nil
}

func (s *Server) Start() error {
	// Start config and secret watchers
	go s.watchConfigChanges()
	go s.watchSecretChanges()

	cfg := s.cfgManager.GetConfig()
	proxyPort := cfg.Server.ProxyPort
	adminPort := cfg.Server.AdminPort

	errCh := make(chan error, 2)

	// Start Proxy Server
	go func() {
		log.Info().Int("port", proxyPort).Msg("Proxy server started")
		if err := s.proxyServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("proxy server error: %w", err)
		}
	}()

	// Start Management Server
	go func() {
		log.Info().Int("port", adminPort).Msg("Management server started")
		if err := s.adminServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("management server error: %w", err)
		}
	}()

	// Block until one of them returns an error
	return <-errCh
}

func (s *Server) watchConfigChanges() {
	for {
		select {
		case <-s.stopCh:
			return
		case _, ok := <-s.cfgManager.Reload:
			if !ok {
				return
			}
			log.Info().Msg("Config file changed, reloading proxy routes...")
			s.reloadRoutes()
		}
	}
}

func (s *Server) watchSecretChanges() {
	if s.secretPath == "" {
		return
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Warn().Err(err).Msg("Failed to start secret file watcher")
		return
	}
	defer watcher.Close()

	dir := filepath.Dir(s.secretPath)
	if err := watcher.Add(dir); err != nil {
		log.Warn().Err(err).Str("dir", dir).Msg("Failed to watch secret directory")
		return
	}

	const debounceDelay = 100 * time.Millisecond
	var debounceTimer *time.Timer
	var debounceC <-chan time.Time
	defer func() {
		if debounceTimer != nil {
			debounceTimer.Stop()
		}
	}()

	for {
		select {
		case <-s.stopCh:
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if filepath.Base(event.Name) != filepath.Base(s.secretPath) {
				continue
			}
			if !event.Has(fsnotify.Write) && !event.Has(fsnotify.Create) && !event.Has(fsnotify.Rename) {
				continue
			}
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
		case <-debounceC:
			debounceTimer = nil
			debounceC = nil
			log.Info().Msg("Secret file changed, rebuilding runtime...")
			s.reloadRoutes()
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Warn().Err(err).Msg("Secret watcher error")
		}
	}
}

func (s *Server) reloadRoutes() {
	s.reloadMu.Lock()
	defer s.reloadMu.Unlock()

	cfg := s.cfgManager.GetConfig()
	if err := s.proxy.UpdateRoutes(cfg); err != nil {
		// Build failed — the previously published runtime stays active.
		log.Error().Err(err).Msg("Runtime rebuild failed, keeping previous runtime")
		return
	}
	log.Info().Msg("Proxy routing table updated successfully")
}

func (s *Server) Shutdown(ctx context.Context) error {
	log.Info().Msg("Shutting down API Gateway...")

	// Stop consuming config reloads first so the watcher goroutine does not
	// race the servers' shutdown or rebuild runtimes during termination.
	s.stopOnce.Do(func() { close(s.stopCh) })

	var firstErr error

	if err := s.proxyServer.Shutdown(ctx); err != nil {
		log.Error().Err(err).Msg("Failed to shutdown proxy server")
		firstErr = err
	}

	if err := s.adminServer.Shutdown(ctx); err != nil {
		log.Error().Err(err).Msg("Failed to shutdown management server")
		if firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

func loadAdminAuthenticator(provider secrets.Provider) (*auth.AdminAuthenticator, error) {
	if raw := strings.TrimSpace(os.Getenv("GATEWAY_ADMIN_TOKEN")); raw != "" {
		return auth.NewAdminAuthenticator(raw)
	}
	if provider == nil {
		return nil, nil
	}
	for _, key := range []string{secrets.AdminTokenHashKey, secrets.AdminTokenKey} {
		value, err := provider.Get(key)
		if err != nil {
			if errors.Is(err, secrets.ErrNotFound) {
				continue
			}
			return nil, err
		}
		if strings.TrimSpace(value) == "" {
			continue
		}
		return auth.NewAdminAuthenticator(value)
	}
	return nil, nil
}
