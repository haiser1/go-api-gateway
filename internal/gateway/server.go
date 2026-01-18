package gateway

import (
	"context"
	"net/http"
	"time"

	"github.com/haiser1/go-api-gateway/internal/config"
	"github.com/julienschmidt/httprouter"
	"github.com/rs/zerolog/log"
)

type Server struct {
	httpServer *http.Server
	cfgManager *config.Manager
	router     *httprouter.Router
	proxy      *Proxy
}

func NewServer(cfgManager *config.Manager) *Server {
	router := httprouter.New()
	proxy := NewProxy(cfgManager.GetConfig())

	// Management routes (Control Plane)
	RegisterManagementRoutes(router, cfgManager)

	// Catch-all route for Proxy (Data Plane)
	// Router ini menangani request yang tidak match dengan route manajemen
	router.NotFound = http.HandlerFunc(proxy.ServeHTTP)
	router.MethodNotAllowed = http.HandlerFunc(proxy.ServeHTTP)

	s := &Server{
		cfgManager: cfgManager,
		router:     router,
		proxy:      proxy,
		httpServer: &http.Server{
			Addr:         ":8080",
			Handler:      router,
			ReadTimeout:  10 * time.Second, // Best practice: Set timeouts!
			WriteTimeout: 10 * time.Second,
		},
	}
	return s
}

func (s *Server) Start() error {
	// FIX: Panggil watcher hanya SEKALI
	go s.watchConfigChanges()

	log.Info().Msg("Server started on port 8080")
	return s.httpServer.ListenAndServe()
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
	return s.httpServer.Shutdown(ctx)
}
