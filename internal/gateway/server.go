package gateway

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/haiser1/go-api-gateway/internal/config"
	"github.com/julienschmidt/httprouter"
	"github.com/rs/zerolog/log"
)

type Server struct {
	proxyServer *http.Server
	adminServer *http.Server
	cfgManager  *config.Manager
	proxy       *Proxy
}

func NewServer(cfgManager *config.Manager) *Server {
	cfg := cfgManager.GetConfig()

	// === Proxy Server (Data Plane) ===
	proxy := NewProxy(cfg)

	proxyPort := cfg.Server.ProxyPort
	if proxyPort == 0 {
		proxyPort = 8080
	}

	// === Management Server (Control Plane) ===
	adminRouter := httprouter.New()
	RegisterManagementRoutes(adminRouter, cfgManager)

	adminPort := cfg.Server.AdminPort
	if adminPort == 0 {
		adminPort = 8081
	}

	s := &Server{
		cfgManager: cfgManager,
		proxy:      proxy,
		proxyServer: &http.Server{
			Addr:         fmt.Sprintf(":%d", proxyPort),
			Handler:      http.HandlerFunc(proxy.ServeHTTP),
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 10 * time.Second,
		},
		adminServer: &http.Server{
			Addr:         fmt.Sprintf(":%d", adminPort),
			Handler:      adminRouter,
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 10 * time.Second,
		},
	}
	return s
}

func (s *Server) Start() error {
	// Start config watcher
	go s.watchConfigChanges()

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
	for range s.cfgManager.Reload {
		log.Info().Msg("Config file changed, reloading proxy routes...")
		s.reloadRoutes()
	}
}

func (s *Server) reloadRoutes() {
	cfg := s.cfgManager.GetConfig()
	s.proxy.UpdateRoutes(cfg) // Thread-safe swap
	log.Info().Msg("Proxy routing table updated successfully")
}

func (s *Server) Shutdown(ctx context.Context) error {
	log.Info().Msg("Shutting down API Gateway...")

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
